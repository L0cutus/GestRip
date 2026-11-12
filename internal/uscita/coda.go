// Package uscita è la coda durevole dei messaggi in uscita (CLI-51, slice 068), come Outgoing.enqueue/3,
// dispatch_once/1, Outgoing.Provider e Outgoing.Dispatcher dello standalone a go-baseline.
//
// Email e Telegram passano dalla tabella messaggi_in_uscita: una riga nasce «queued» (Accoda) e un giro
// della coda (Giro) la passa al provider del suo canale quando il suo momento è venuto. L'esito è
// «accepted» (il provider l'ha presa: mai «consegnato», e il database non ha quello stato) o, dopo
// MaxTentativi rifiuti, «failed»; fra un rifiuto e l'altro le attese dello standalone. WhatsApp non passa
// di qui: la riga nasce «accepted», consegnata all'operatore (buste.registraConsegna). La coda è nel
// database, quindi sopravvive a un riavvio: Lavoro, in gestrip serve, fa un giro all'avvio e poi ogni
// OgniQuanto, e riprende le righe rimaste in coda con i tentativi già fatti. Una riga che stava andando al
// provider quando il processo è morto resta «queued» e si riprova (come lo standalone).
//
// I provider veri implementano Provider: SMTP (smtp.go, slice 069), il bot Telegram con la T4.7. Le regole di Telegram dello standalone (il 429 che
// fa aspettare le altre righe Telegram del giro, il 400 e il 403 che fanno fallire subito, le notifiche ai
// clienti spente) arrivano con la T4.7, in esitoDelRifiuto.
package uscita

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// I canali della coda (@queue_channels). WhatsApp è un canale «a mano» (@handoff_channels).
const (
	CanaleEmail    = "email"
	CanaleTelegram = "telegram"
	canaleWhatsApp = "whatsapp"
)

const (
	// MaxTentativi è @max_attempts: il quinto rifiuto fa fallire la riga.
	MaxTentativi = 5
	// OgniQuanto è :dispatch_interval_ms dello standalone: un giro ogni 30 secondi.
	OgniQuanto = 30 * time.Second
	// PerGiro è il :batch del Dispatcher: al più 50 righe per giro.
	PerGiro = 50
	// lunghezzaErrore è truncate/1: l'errore salvato tiene al più 500 caratteri.
	lunghezzaErrore = 500
	// lunghezzaIndirizzo è validate_length(:address, max: 320).
	lunghezzaIndirizzo = 320
)

// attese è @backoff_seconds: l'attesa dopo il 1°, 2°, 3° e 4° rifiuto.
var attese = []time.Duration{10 * time.Second, time.Minute, 5 * time.Minute, 30 * time.Minute}

// Le parole dell'errore salvato quando il provider non ne dichiara uno (le stesse dello standalone dove
// non portano dati).
const (
	erroreNonConfigurato = "not_configured" // il canale non ha un provider
	errorePanico         = "raised"         // il provider è andato in panico (lo standalone aggiunge il messaggio: qui no)
	erroreProvider       = "provider_error" // un errore che non è un Rifiuto: potrebbe portare indirizzo o testo
)

// I rifiuti di Accoda: niente è scritto.
var (
	// ErrSoloAMano è {:not_a_queue_channel, :whatsapp}: nessun provider la prenderebbe mai.
	ErrSoloAMano = errors.New("uscita: canale consegnato a mano, non si accoda")
	// ErrNonConfigurato è {:not_configured, channel}: il canale non ha un provider.
	ErrNonConfigurato = errors.New("uscita: canale non configurato")
	// ErrMessaggioNonValido è il changeset rifiutato (Message.changeset/2): canale o parte sconosciuti,
	// indirizzo vuoto, con spazi o a capo (un'intestazione iniettata) o troppo lungo, testo vuoto.
	ErrMessaggioNonValido = errors.New("uscita: messaggio non valido")
)

// Rifiuto è il motivo di un rifiuto che il provider dichiara sicuro da salvare e da mostrare: breve, senza
// credenziali, token, indirizzi né testi (il contratto di Outgoing.Provider). Ogni altro errore si salva
// come «provider_error».
type Rifiuto string

func (r Rifiuto) Error() string { return string(r) }

// Messaggio è una riga della coda come la riceve il provider.
type Messaggio struct {
	ID           int64
	BustaID      *int64
	Canale       string
	Destinatario string // shop, customer, oreficeria, artisan
	Indirizzo    string
	Oggetto      *string
	Testo        string
	Tentativi    int // quelli già fatti, prima di questo
}

// Provider è Outgoing.Provider: Consegna dice se il provider ha preso il messaggio (il riferimento che
// dà, mai la prova che è arrivato) o perché no. Non deve andare in rete in un test.
type Provider interface {
	Consegna(ctx context.Context, m Messaggio) (riferimento string, err error)
}

// conAccensione è un provider il cui canale può essere spento senza togliere il provider (SMTP: la
// configurazione si legge dal database a ogni giro, e la D-03 tiene spento il canale finché il titolare
// non lo accende). Un canale spento è come un canale senza provider: Accoda lo rifiuta e il giro non
// legge le sue righe.
type conAccensione interface {
	Attivo(ctx context.Context) (bool, error)
}

// conInvioAMano è un provider che sa mandare una riga anche col suo canale spento: «Manda» di un'email
// rimasta in coda, che il titolare guarda prima di accendere l'email (D-27, pagina_smtp.go).
type conInvioAMano interface {
	ConsegnaAMano(ctx context.Context, m Messaggio) (riferimento string, err error)
}

// conSegreti è un provider che ha dei segreti (password, token): non finiscono mai nell'errore salvato
// (scrub/2).
type conSegreti interface{ Segreti() []string }

// Destinazione è a chi va il messaggio (Outgoing.Destination).
type Destinazione struct {
	BustaID      *int64
	Canale       string
	Destinatario string
	Indirizzo    string
}

// Contenuto è cosa si manda: l'oggetto (solo email) e il testo.
type Contenuto struct {
	Oggetto *string
	Testo   string
}

// Esito è quante righe un giro ha portato ad «accepted», rimesso in coda e fatto fallire.
type Esito struct{ Accettati, Riprovati, Falliti int }

// Coda è la coda in uscita su un database, con i provider dei canali configurati.
type Coda struct {
	d        *db.DB
	o        clock.Orologio
	reg      *audit.Registro
	provider map[string]Provider
	log      *slog.Logger
	// aMano mette in fila, nel processo, ogni riga del giro (dalla rilettura all'esito), l'interruttore
	// (D-03) e le azioni del titolare sulle email in coda (D-27): una email non parte due volte (giro e
	// «Manda adesso», o due clic), e l'email non si accende né si spegne a metà di un invio.
	aMano sync.Mutex
}

// Nuova è la coda con i provider dati, per canale: un canale senza provider non è configurato.
func Nuova(d *db.DB, o clock.Orologio, provider map[string]Provider) *Coda {
	return &Coda{d: d, o: o, reg: audit.Nuovo(d, o), provider: provider, log: slog.New(slog.DiscardHandler)}
}

// ConRegistro dà alla coda le righe del server (mai indirizzi, testi né segreti).
func (c *Coda) ConRegistro(l *slog.Logger) *Coda {
	if l != nil {
		c.log = l
	}
	return c
}

var (
	canaliCoda     = []string{CanaleEmail, CanaleTelegram}
	destinatari    = []string{"shop", "customer", "oreficeria", "artisan"}
	reIndirizzoUno = regexp.MustCompile(`\A\S+\z`)
)

// Accoda è Outgoing.enqueue/3: la riga «queued» con zero tentativi e il prossimo tentativo adesso, il testo
// senza spazi ai bordi (base_changeset/2), e outgoing.queued con canale, parte e busta (mai testo né
// indirizzo), in una transazione. WhatsApp è ErrSoloAMano, un canale senza provider ErrNonConfigurato.
func (c *Coda) Accoda(ctx context.Context, dest Destinazione, cont Contenuto, a audit.Contesto) (int64, error) {
	if err := c.accodabile(ctx, dest, cont); err != nil {
		return 0, err
	}
	tx, err := c.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	id, err := c.scrivi(ctx, tx, dest, cont, a)
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// AccodaIn è Accoda nella transazione tx di chi chiama (slice 070b): la riga e outgoing.queued si
// scrivono con quello che chi chiama scrive (l'email di pronta con il cambio di stato della busta), o non
// si scrivono. Gli stessi rifiuti, senza scrivere niente in tx. Il canale si chiede al provider fuori da
// tx (SMTP lo legge con il collegamento di lettura): niente qui usa il collegamento di scrittura, che è
// uno solo e tiene già tx.
func (c *Coda) AccodaIn(ctx context.Context, tx *sql.Tx, dest Destinazione, cont Contenuto, a audit.Contesto) (int64, error) {
	if err := c.accodabile(ctx, dest, cont); err != nil {
		return 0, err
	}
	return c.scrivi(ctx, tx, dest, cont, a)
}

// accodabile sono i rifiuti di Accoda, prima di scrivere: WhatsApp, il messaggio non valido, il canale
// senza provider o spento.
func (c *Coda) accodabile(ctx context.Context, dest Destinazione, cont Contenuto) error {
	if dest.Canale == canaleWhatsApp {
		return ErrSoloAMano
	}
	testo := strings.TrimFunc(cont.Testo, unicode.IsSpace)
	switch {
	case !slices.Contains(canaliCoda, dest.Canale), !slices.Contains(destinatari, dest.Destinatario),
		!reIndirizzoUno.MatchString(dest.Indirizzo), len([]rune(dest.Indirizzo)) > lunghezzaIndirizzo, testo == "":
		return ErrMessaggioNonValido
	}
	switch attivo, err := c.CanaleAttivo(ctx, dest.Canale); {
	case err != nil:
		return err
	case !attivo:
		return ErrNonConfigurato
	}
	return nil
}

// scrivi è la riga e la sua riga d'audit, in tx.
func (c *Coda) scrivi(ctx context.Context, tx *sql.Tx, dest Destinazione, cont Contenuto, a audit.Contesto) (int64, error) {
	ora := c.o.Adesso().UTC().Format(db.FormatoIstante)
	testo := strings.TrimFunc(cont.Testo, unicode.IsSpace)
	res, err := tx.ExecContext(ctx, `INSERT INTO messaggi_in_uscita (busta_id, canale, destinatario, indirizzo, oggetto, testo, stato,
		tentativi, prossimo_tentativo_il, accodato_il) VALUES (?1, ?2, ?3, ?4, ?5, ?6, 'queued', 0, ?7, ?7)`,
		dest.BustaID, dest.Canale, dest.Destinatario, dest.Indirizzo, cont.Oggetto, testo, ora)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	var busta any
	if dest.BustaID != nil {
		busta = *dest.BustaID
	}
	if err := c.reg.Scrivi(ctx, tx, a, "outgoing.queued", "outgoing_message", id, "ok",
		map[string]any{"channel": dest.Canale, "party": dest.Destinatario, "repair_id": busta}); err != nil {
		return 0, err
	}
	return id, nil
}

// Giro è dispatch_once/1: le righe «queued» il cui momento è venuto, dalla più vecchia, al più limite.
// Una riga uscita dalla coda mentre il giro lavorava su quelle prima non va al provider. Un errore del
// database ferma il giro (le righe restano in coda); un rifiuto del provider no.
func (c *Coda) Giro(ctx context.Context, limite int) (Esito, error) {
	adesso := c.o.Adesso().UTC().Format(db.FormatoIstante)
	righe, err := c.daMandare(ctx, adesso, limite)
	if err != nil {
		return Esito{}, err
	}
	var e Esito
	for _, m := range righe {
		if err := ctx.Err(); err != nil {
			return e, err
		}
		if err := c.unaRiga(ctx, m, &e); err != nil {
			return e, err
		}
	}
	return e, nil
}

// unaRiga è il giro su una riga, sotto aMano (review della 069b): rileggere se è ancora in coda, se il
// canale è acceso, il tentativo e il suo esito non si mescolano con «Spegni» o «Manda adesso», così la
// stessa email non parte due volte.
func (c *Coda) unaRiga(ctx context.Context, m Messaggio, e *Esito) error {
	c.aMano.Lock()
	defer c.aMano.Unlock()
	ancora, err := c.ancoraInCoda(ctx, m.ID)
	if err != nil || !ancora {
		return err
	}
	// Un canale che si è spento durante il giro (review della 069): la riga resta com'è, senza
	// tentativo. Solo per un provider che sa dirlo: uno che manca resta «not_configured» in prova.
	if pa, ok := c.provider[m.Canale].(conAccensione); ok {
		attivo, err := pa.Attivo(ctx)
		if err != nil || !attivo {
			return err
		}
	}
	rif, rifiuto := c.prova(ctx, m)
	return c.salva(ctx, m, rif, rifiuto, e)
}

// daMandare legge le righe da passare ai provider: solo dei canali che hanno un provider (review della
// 068) acceso adesso (CanaleAttivo, slice 069: l'email spenta lascia ferme le sue righe, anche quelle
// importate, D-03 e D-27). Le righe di un canale senza provider restano «queued» senza tentativi né scritture: dopo la 069,
// con il solo SMTP, le righe Telegram importate dallo standalone aspettano la T4.7 invece di fallire per
// «not_configured» (lo standalone, dove ogni canale si configura, le prova e le fa fallire).
func (c *Coda) daMandare(ctx context.Context, adesso string, limite int) ([]Messaggio, error) {
	canali := make([]string, 0, len(c.provider))
	for k := range c.provider {
		attivo, err := c.CanaleAttivo(ctx, k)
		if err != nil {
			return nil, err
		}
		if attivo {
			canali = append(canali, k)
		}
	}
	if len(canali) == 0 {
		return nil, nil
	}
	slices.Sort(canali)
	arg := []any{adesso}
	for _, k := range canali {
		arg = append(arg, k)
	}
	arg = append(arg, limite)
	rr, err := c.d.Lettura.QueryContext(ctx, `SELECT id, busta_id, canale, destinatario, indirizzo, oggetto, testo, tentativi
		FROM messaggi_in_uscita WHERE stato = 'queued' AND prossimo_tentativo_il <= ? AND canale IN (?`+strings.Repeat(", ?", len(canali)-1)+`)
		ORDER BY id LIMIT ?`, arg...)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	var righe []Messaggio
	for rr.Next() {
		var m Messaggio
		if err := rr.Scan(&m.ID, &m.BustaID, &m.Canale, &m.Destinatario, &m.Indirizzo, &m.Oggetto, &m.Testo, &m.Tentativi); err != nil {
			return nil, err
		}
		righe = append(righe, m)
	}
	return righe, rr.Err()
}

// ancoraInCoda è still_queued?/1.
func (c *Coda) ancoraInCoda(ctx context.Context, id int64) (bool, error) {
	var n int
	err := c.d.Lettura.QueryRowContext(ctx, `SELECT count(*) FROM messaggi_in_uscita WHERE id = ? AND stato = 'queued'`, id).Scan(&n)
	return n == 1, err
}

// prova è attempt/1 e deliver/3: il provider del canale, o «not_configured»; un panico del provider
// costa il tentativo della sua riga e basta (nel registro solo il canale e l'id, mai il valore del panico,
// che può avere il testo o un segreto). Il secondo valore è l'errore da salvare, "" se accettato.
func (c *Coda) prova(ctx context.Context, m Messaggio) (rif, errore string) {
	p, ok := c.provider[m.Canale]
	if !ok {
		return "", erroreNonConfigurato
	}
	return c.provaCon(ctx, m, p, p.Consegna)
}

// provaCon è prova con la consegna data (Consegna, o ConsegnaAMano per «Manda» della D-27).
func (c *Coda) provaCon(ctx context.Context, m Messaggio, p Provider, consegna func(context.Context, Messaggio) (string, error)) (rif, errore string) {
	defer func() {
		if v := recover(); v != nil {
			c.log.Error("coda in uscita: il provider è andato in panico", "canale", m.Canale, "messaggio", m.ID, "tipo", fmt.Sprintf("%T", v))
			rif, errore = "", errorePanico
		}
	}()
	r, err := consegna(ctx, m)
	if err == nil {
		return pulisci(r, p), ""
	}
	var motivo Rifiuto
	if errors.As(err, &motivo) {
		e := pulisci(string(motivo), p)
		if e == "" {
			e = erroreProvider
		}
		return "", e
	}
	return "", erroreProvider
}

// pulisci è text/2, scrub/2 e truncate/1: i segreti del provider diventano «***» e il testo si taglia a
// 500 caratteri.
func pulisci(s string, p Provider) string {
	if ps, ok := p.(conSegreti); ok {
		for _, segreto := range ps.Segreti() {
			if segreto != "" {
				s = strings.ReplaceAll(s, segreto, "***")
			}
		}
	}
	if r := []rune(s); len(r) > lunghezzaErrore {
		s = string(r[:lunghezzaErrore])
	}
	return s
}

// salva è record/2 con store/2: l'esito si scrive solo su una riga ancora «queued» (un secondo giro, o
// una riga che altri hanno già chiuso, non cambia niente). Accettata: «accepted», l'ora, il riferimento,
// nessun errore. Rifiutata: il tentativo e l'errore, e il prossimo momento dopo l'attesa del suo
// gradino; al quinto «failed» con l'ora.
func (c *Coda) salva(ctx context.Context, m Messaggio, rif, errore string, e *Esito) error {
	adesso := c.o.Adesso().UTC()
	ora := adesso.Format(db.FormatoIstante)
	tentativi := m.Tentativi + 1
	var err error
	switch {
	case errore == "":
		_, err = c.d.Scrittura.ExecContext(ctx, `UPDATE messaggi_in_uscita SET stato = 'accepted', accettato_il = ?1, tentativi = ?2,
			riferimento_provider = ?3, ultimo_errore = NULL WHERE id = ?4 AND stato = 'queued'`, ora, tentativi, rif, m.ID)
		e.Accettati++
	case tentativi >= MaxTentativi:
		_, err = c.d.Scrittura.ExecContext(ctx, `UPDATE messaggi_in_uscita SET stato = 'failed', fallito_il = ?1, tentativi = ?2,
			ultimo_errore = ?3 WHERE id = ?4 AND stato = 'queued'`, ora, tentativi, errore, m.ID)
		e.Falliti++
	default:
		prossimo := adesso.Add(attesa(tentativi)).Format(db.FormatoIstante)
		_, err = c.d.Scrittura.ExecContext(ctx, `UPDATE messaggi_in_uscita SET tentativi = ?1, prossimo_tentativo_il = ?2,
			ultimo_errore = ?3 WHERE id = ?4 AND stato = 'queued'`, tentativi, prossimo, errore, m.ID)
		e.Riprovati++
	}
	return err
}

// attesa è backoff_seconds/1: il gradino del tentativo fatto, l'ultimo oltre la scala.
func attesa(tentativi int) time.Duration {
	return attese[min(tentativi-1, len(attese)-1)]
}

// Lavoro è Outgoing.Dispatcher: un giro subito, poi uno dopo ogni aspetta (OgniQuanto in gestrip serve),
// finché aspetta dice di smettere. Un giro che va in panico o non riesce non ferma il lavoro: nel registro
// solo il genere dell'errore, e le righe restano in coda per il giro dopo.
func (c *Coda) Lavoro(ctx context.Context, aspetta func(context.Context) bool) {
	for {
		c.unGiro(ctx)
		if !aspetta(ctx) {
			return
		}
	}
}

func (c *Coda) unGiro(ctx context.Context) {
	defer func() {
		if v := recover(); v != nil {
			c.log.Error("coda in uscita: giro interrotto da un panico, si riprova al prossimo giro", "tipo", fmt.Sprintf("%T", v))
		}
	}()
	e, err := c.Giro(ctx, PerGiro)
	if err != nil && ctx.Err() == nil {
		c.log.Warn("coda in uscita: giro non finito, le righe restano in coda", "tipo", fmt.Sprintf("%T", err))
	}
	if e.Falliti > 0 {
		c.log.Warn("coda in uscita: messaggi falliti", "quanti", e.Falliti)
	}
}

// CanaleAttivo dice se un canale ha un provider e, se il provider lo sa dire (conAccensione: l'email con
// la configurazione e l'interruttore della D-03), se è acceso adesso. È configured?/1 dello standalone.
func (c *Coda) CanaleAttivo(ctx context.Context, canale string) (bool, error) {
	p, ok := c.provider[canale]
	if !ok {
		return false, nil
	}
	if pa, ok := p.(conAccensione); ok {
		return pa.Attivo(ctx)
	}
	return true, nil
}
