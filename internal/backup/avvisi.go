package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// Gli avvisi dell'allarme dei backup al negozio, su email e Telegram (slice 127, T5.2, tema E, risposta H del piano:
// «un messaggio quando nasce il problema, uno al giorno finché resta, uno quando si risolve; con canali spenti solo
// il banner»; D-16). Solo Go: lo standalone non ha l'allarme (lo fa otto-vps, fuori dal pacchetto, OPS-28).
//
//   - **Quali problemi:** quelli del banner del titolare, Stato.Allarme (slice 126), con la stessa regola di
//     -senza-lavori-automatici. In più, con l'opzione il lavoro degli avvisi non parte proprio (è un lavoro
//     automatico, come il backup notturno): nel banco e nello sviluppo non si accoda niente.
//   - **Un problema per codice:** ogni Problema.Codice è un problema a sé, che nasce, resta e si risolve. Un
//     cambiamento (la chiavetta tolta che dopo 26 ore porta anche «backup vecchio», o la chiavetta rimessa con il
//     backup ancora vecchio) è un codice che nasce o si risolve: lo dice il messaggio di quel giro, con gli altri
//     problemi ancora aperti. Lo stesso codice con un testo diverso (la chiavetta assente e poi in sola lettura)
//     resta lo stesso problema: il promemoria del giorno dopo ha il testo nuovo.
//   - **Quando:** un giro ogni OgniQuantoAvvisi, il primo AttesaAvvisiDopoAvvio dopo l'avvio. Un giro manda al più
//     un messaggio per canale: «nuovo problema» se nasce almeno un codice (con quelli risolti e quelli ancora
//     aperti), «risolto» se se ne risolve almeno uno e nessuno nasce, altrimenti il promemoria del giorno.
//   - **Uno al giorno:** il promemoria parte al primo giro dalle OraPromemoria (09:00 del negozio, Europe/Rome)
//     di un giorno del negozio in cui non è ancora partito nessun messaggio che elenchi problemi aperti (nascita,
//     risoluzione di uno con altri ancora aperti, promemoria). Tutti i giorni, anche quelli di chiusura, come il backup (D-115). I messaggi di nascita e di
//     risoluzione partono subito, a qualunque ora.
//   - **Isteresi (revisione, MEDIA 1):** un codice aperto che manca dall'allarme è «risolto» solo dopo AttesaRisolto
//     (30 minuti) di fila; se torna prima non parte niente e resta aperto con la sua data di nascita. L'inizio
//     dell'assenza sta in statoAvvisi.Assenti (durevole). Senza configurazione gli altri codici sono nascosti, non
//     risolti.
//   - **Testi:** solo Problema.Testo (codici fissi e date, mai percorsi né chiavi) e i titoli fissi di titoliAvviso;
//     mai il motivo grezzo delle esecuzioni (nota 11(b)).
//   - **Durevole:** lo stato degli avvisi (ChiaveAvvisi in impostazioni: i codici avvisati e il giorno dell'ultimo
//     messaggio) si scrive nella stessa transazione delle righe della coda in uscita (uscita.Coda.AccodaIn): o
//     tutti e due o niente, così un riavvio non rimanda e non perde messaggi.
//   - **Canali spenti (D-16):** nessun canale attivo, nessun messaggio, e lo stato non si tocca (non si legge
//     nemmeno lo stato dei backup): riaccesi i canali, quello che è nato intanto si avvisa come nuovo e quello che
//     si è risolto intanto come risolto.
//   - **Stato illeggibile:** nessun messaggio e lo stato non si tocca (un errore non è mai «tutto risolto»). Il
//     banner del titolare lo mostra (slice 126); un errore di lettura del database è quasi sempre un database che
//     non scrive nemmeno la coda, e un avviso per un errore passeggero sarebbe un falso allarme seguito da un
//     «risolto». Nel registro del server solo il tipo dell'errore.

// ChiaveAvvisi è la chiave di impostazioni, solo del Go, dello stato degli avvisi (JSON di statoAvvisi).
const ChiaveAvvisi = "backup.avvisi"

// OraPromemoria è l'ora del negozio del promemoria giornaliero: le 9, la stessa dell'avviso dei ritardi (jobs),
// quando il negozio c'è; mai di notte, quando il backup fallisce e nessuno può fare niente.
const OraPromemoria = 9 * time.Hour

// OgniQuantoAvvisi è l'intervallo fra due giri degli avvisi: un problema nato si avvisa entro 5 minuti.
const OgniQuantoAvvisi = 5 * time.Minute

// AttesaAvvisiDopoAvvio è quanto aspetta il primo giro dopo l'avvio di serve: dopo il recupero del backup notturno
// (AttesaDopoAvvio, 10 minuti) e il tempo di farlo, così un apparecchio rimasto spento la notte non manda «backup
// vecchio» seguito, pochi minuti dopo, da «risolto».
const AttesaAvvisiDopoAvvio = 30 * time.Minute

// attoreAvvisi firma le righe della coda in uscita (outgoing.queued), come i lavori automatici della 094.
const attoreAvvisi = "scheduler"

// titoliAvviso sono i nomi brevi dei problemi, per dire quale si è risolto.
var titoliAvviso = map[string]string{
	ProblemaNonConfigurato: "backup automatico non configurato",
	ProblemaChiavetta:      "chiavetta dei backup",
	ProblemaBackupFallito:  "ultimo backup non riuscito",
	ProblemaBackupVecchio:  "backup mancante o troppo vecchio",
	ProblemaOrologio:       "ora nel futuro nel registro dei backup",
	ProblemaProvaFallita:   "prova di ripristino non riuscita",
	ProblemaProvaMancante:  "prova di ripristino non fatta da più di 8 giorni",
	ProblemaMediaRovinati:  "foto o file rovinati sul disco",
}

func titoloAvviso(codice string) string {
	if t, ok := titoliAvviso[codice]; ok {
		return t
	}
	return "un problema dei backup"
}

// statoAvvisi è quello che è già stato detto al negozio.
type statoAvvisi struct {
	// Aperti sono i codici avvisati e non ancora risolti, con l'istante (UTC) del messaggio che li ha detti nati.
	Aperti map[string]time.Time `json:"aperti"`
	// Giorno è il giorno del negozio (aaaa-mm-gg) dell'ultimo messaggio che elencava problemi aperti.
	Giorno string `json:"giorno,omitempty"`
	// Assenti sono i codici aperti che adesso mancano dall'allarme, con l'istante (UTC) del primo giro in cui sono
	// mancati: diventano «risolti» solo dopo AttesaRisolto di fila (revisione della 127, MEDIA 1).
	Assenti map[string]time.Time `json:"assenti,omitempty"`
}

// AttesaRisolto è quanto un problema aperto deve mancare dall'allarme, di fila, per essere detto risolto: una
// chiavetta con un contatto lento che va e viene ogni pochi minuti non fa «nuovo» e «risolto» a ogni giro
// (scelta del coordinatore, revisione della 127). Se il codice torna prima, il problema resta aperto con la sua
// data di nascita e non parte niente.
const AttesaRisolto = 30 * time.Minute

// TempoStatoAvvisi è il tempo massimo per leggere lo stato dei backup in un giro (una chiavetta appesa, il database
// occupato), come tempoStatoBackup della pagina (126): scaduto, lo stato vale «illeggibile» e il giro non manda
// niente.
var TempoStatoAvvisi = 5 * time.Second

// ErrStatoAvvisiScaduto: la lettura dello stato dei backup non è finita entro TempoStatoAvvisi.
var ErrStatoAvvisiScaduto = errors.New("backup: lo stato dei backup non è arrivato in tempo per gli avvisi")

// letturaAvvisi è una lettura dello stato in corso: fatto si chiude quando s ed err sono scritti.
type letturaAvvisi struct {
	fatto chan struct{}
	s     Stato
	err   error
}

// PostaDelNegozio è la coda in uscita (uscita.Coda): i canali attivi e le righe accodate in una transazione.
type PostaDelNegozio interface {
	CanaleAttivo(ctx context.Context, canale string) (bool, error)
	AccodaIn(ctx context.Context, tx *sql.Tx, dest uscita.Destinazione, cont uscita.Contenuto, a audit.Contesto) (int64, error)
}

// Avvisi sono gli avvisi dell'allarme dei backup al negozio: uno per processo (gestrip serve).
type Avvisi struct {
	d     *db.DB
	posta PostaDelNegozio
	log   *slog.Logger
	// stato è Notturno.Stato; una variabile per i test.
	stato func(ctx context.Context) (Stato, error)
	// mu e inCorso: una sola lettura dello stato alla volta (leggiStatoBackup).
	mu      sync.Mutex
	inCorso *letturaAvvisi
}

// NuoviAvvisi sono gli avvisi dei problemi del backup n, accodati su posta.
func NuoviAvvisi(n *Notturno, posta PostaDelNegozio) *Avvisi {
	return &Avvisi{d: n.d, posta: posta, log: slog.New(slog.DiscardHandler), stato: n.Stato}
}

// ConRegistro dà agli avvisi le righe del server (mai testi, indirizzi né errori: solo il tipo).
func (a *Avvisi) ConRegistro(l *slog.Logger) *Avvisi {
	if l != nil {
		a.log = l
	}
	return a
}

// Avviso è il messaggio che un giro accoda, uguale su ogni canale.
type Avviso struct {
	Oggetto, Testo string
}

// Giro guarda l'allarme adesso e, se c'è da dire qualcosa, accoda il messaggio su ogni canale attivo e scrive lo
// stato degli avvisi, in una transazione. m è il messaggio accodato (zero se nessuno).
func (a *Avvisi) Giro(ctx context.Context) (m Avviso, accodato bool, err error) {
	dest, err := a.destinazioni(ctx)
	if err != nil || len(dest) == 0 {
		return Avviso{}, false, err
	}
	s, err := a.leggiStatoBackup(ctx)
	if err != nil {
		return Avviso{}, false, err
	}
	prima, err := a.leggiStato(ctx)
	if err != nil {
		return Avviso{}, false, err
	}
	// Senza configurazione Problemi dà solo «non configurato»: gli altri sono nascosti, non risolti.
	m, manda, dopo, scrivi := decidiAvviso(prima, s.Allarme(), !s.Configurato, s.Adesso)
	if !scrivi {
		return Avviso{}, false, nil
	}
	if !manda {
		dest = nil // cambia solo lo stato delle assenze
	}
	if err := a.accoda(ctx, dest, m, dopo, s.Adesso); err != nil {
		return Avviso{}, false, err
	}
	if !manda {
		return Avviso{}, false, nil
	}
	return m, true, nil
}

// leggiStatoBackup legge lo stato dei backup in una goroutine e lo aspetta al più TempoStatoAvvisi (revisione della
// 127, MEDIA 2): ControllaChiavetta non ha un contesto, quindi una chiavetta appesa terrebbe fermo il giro e lo stop
// di serve. Scaduto il tempo, o finito ctx, ErrStatoAvvisiScaduto o l'errore del contesto: il giro non manda niente
// e la goroutine appesa resta da sola (non è fra i lavori che serve aspetta). Una sola lettura alla volta: con una
// lettura ancora appesa, il giro dopo aspetta quella e non ne fa partire un'altra. Come avviaLettura della pagina
// (126), che però scrive nella cache della faccia: lo schema è lo stesso, il codice no.
func (a *Avvisi) leggiStatoBackup(ctx context.Context) (Stato, error) {
	a.mu.Lock()
	l := a.inCorso
	if l == nil {
		l = &letturaAvvisi{fatto: make(chan struct{})}
		a.inCorso = l
		go func() {
			defer func() {
				if v := recover(); v != nil {
					a.log.Error("avvisi dei backup: lettura dello stato interrotta da un panico", "tipo", fmt.Sprintf("%T", v),
						"stack", string(debug.Stack()))
					l.s, l.err = Stato{}, fmt.Errorf("backup: lettura dello stato interrotta da un panico")
				}
				a.mu.Lock()
				a.inCorso = nil
				a.mu.Unlock()
				close(l.fatto)
			}()
			c, annulla := context.WithTimeout(context.WithoutCancel(ctx), TempoStatoAvvisi)
			defer annulla()
			l.s, l.err = a.stato(c)
		}()
	}
	a.mu.Unlock()
	attesa, annulla := context.WithTimeout(ctx, TempoStatoAvvisi)
	defer annulla()
	select {
	case <-l.fatto:
		return l.s, l.err
	case <-attesa.Done():
		if ctx.Err() != nil {
			return Stato{}, ctx.Err()
		}
		return Stato{}, ErrStatoAvvisiScaduto
	}
}

// destinazioni sono il negozio su ogni canale attivo, email poi Telegram, come i lavori automatici (jobs,
// destination_for_shop/1): l'email al mittente dell'SMTP, Telegram alla chat del negozio (D-16).
func (a *Avvisi) destinazioni(ctx context.Context) ([]uscita.Destinazione, error) {
	var dest []uscita.Destinazione
	for _, c := range []string{uscita.CanaleEmail, uscita.CanaleTelegram} {
		attivo, err := a.posta.CanaleAttivo(ctx, c)
		if err != nil {
			return nil, err
		}
		if !attivo {
			continue
		}
		var indirizzo string
		switch c {
		case uscita.CanaleEmail:
			conf, err := uscita.LeggiSMTP(ctx, a.d.Lettura)
			if err != nil {
				return nil, err
			}
			if conf.Usabile() {
				indirizzo = conf.Mittente
			}
		case uscita.CanaleTelegram:
			conf, err := uscita.LeggiTelegram(ctx, a.d.Lettura)
			if err != nil {
				return nil, err
			}
			if conf.Usabile() {
				indirizzo = conf.Chat
			}
		}
		if indirizzo != "" {
			dest = append(dest, uscita.Destinazione{Canale: c, Destinatario: "shop", Indirizzo: indirizzo})
		}
	}
	return dest, nil
}

// leggiStato legge lo stato degli avvisi. Nessuna riga: niente avvisato. Un valore illeggibile vale come niente
// avvisato (e si dice nel registro): meglio un problema detto due volte che un problema taciuto.
func (a *Avvisi) leggiStato(ctx context.Context) (statoAvvisi, error) {
	var v string
	err := a.d.Lettura.QueryRowContext(ctx, `SELECT valore FROM impostazioni WHERE chiave = ?`, ChiaveAvvisi).Scan(&v)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return statoAvvisi{}, nil
	case err != nil:
		return statoAvvisi{}, fmt.Errorf("backup: stato degli avvisi: %w", err)
	}
	var s statoAvvisi
	if err := json.Unmarshal([]byte(v), &s); err != nil {
		a.log.Warn("avvisi dei backup: stato illeggibile, si riparte da niente avvisato", "tipo", fmt.Sprintf("%T", err))
		return statoAvvisi{}, nil
	}
	return s, nil
}

// accoda scrive le righe della coda (nessuna se dest è vuoto) e lo stato nuovo in una transazione.
func (a *Avvisi) accoda(ctx context.Context, dest []uscita.Destinazione, m Avviso, s statoAvvisi, adesso time.Time) error {
	v, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tx, err := a.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	for _, d := range dest {
		cont := uscita.Contenuto{Testo: m.Testo}
		if d.Canale == uscita.CanaleEmail {
			oggetto := m.Oggetto
			cont.Oggetto = &oggetto
		}
		if _, err := a.posta.AccodaIn(ctx, tx, d, cont, audit.Contesto{Attore: attoreAvvisi}); err != nil {
			return fmt.Errorf("backup: avviso sul canale %s: %w", d.Canale, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, ?)
		ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore, aggiornata_il = excluded.aggiornata_il`,
		ChiaveAvvisi, string(v), adesso.UTC().Format(db.FormatoIstante)); err != nil {
		return fmt.Errorf("backup: stato degli avvisi: %w", err)
	}
	return tx.Commit()
}

// decidiAvviso è la regola degli avvisi: dallo stato di prima e dall'allarme di adesso, il messaggio (manda) e lo
// stato dopo (scrivi: è cambiato, anche senza messaggio, per le assenze). nascosti: senza configurazione (Problemi dà
// solo «non configurato»), i codici aperti che mancano non sono risolti né assenti, restano aperti e basta.
func decidiAvviso(prima statoAvvisi, allarme []Problema, nascosti bool, adesso time.Time) (m Avviso, manda bool, dopo statoAvvisi, scrivi bool) {
	oggi := clock.DataDi(adesso).String()
	var nuovi, restano []Problema
	attivi := map[string]bool{}
	for _, p := range allarme {
		if attivi[p.Codice] {
			continue
		}
		attivi[p.Codice] = true
		if _, ok := prima.Aperti[p.Codice]; ok {
			restano = append(restano, p)
		} else {
			nuovi = append(nuovi, p)
		}
	}
	dopo = statoAvvisi{Aperti: map[string]time.Time{}, Assenti: map[string]time.Time{}, Giorno: prima.Giorno}
	var risolti, inAttesa []string
	for c, nato := range prima.Aperti {
		switch da, assente := prima.Assenti[c]; {
		case attivi[c]: // c'è ancora, o è tornato prima di AttesaRisolto: resta aperto, l'assenza si dimentica
			dopo.Aperti[c] = nato
		case nascosti && c != ProblemaNonConfigurato:
			dopo.Aperti[c] = nato
		case assente && adesso.Sub(da) >= AttesaRisolto:
			risolti = append(risolti, c)
		default:
			if !assente {
				da = adesso.UTC()
			}
			dopo.Aperti[c], dopo.Assenti[c] = nato, da
			inAttesa = append(inAttesa, c)
		}
	}
	for _, p := range nuovi {
		dopo.Aperti[p.Codice] = adesso.UTC()
	}
	ordina := func(v []string) {
		slices.SortFunc(v, func(x, y string) int { return strings.Compare(ordineCodice(x), ordineCodice(y)) })
	}
	ordina(risolti)
	ordina(inAttesa)
	ancora := append(conData(restano, prima.Aperti), inVerifica(inAttesa, prima.Aperti)...)
	loc := clock.Locale(adesso)
	oraLocale := time.Duration(loc.Hour())*time.Hour + time.Duration(loc.Minute())*time.Minute + time.Duration(loc.Second())*time.Second
	switch {
	case len(nuovi) > 0:
		dopo.Giorno = oggi
		return testoNascita(nuovi, ancora, risolti), true, dopo, true
	case len(risolti) > 0:
		if len(ancora) > 0 {
			dopo.Giorno = oggi // il messaggio elenca anche quelli aperti: vale come il messaggio del giorno
		}
		return testoRisolti(ancora, risolti), true, dopo, true
	case len(restano) > 0 && prima.Giorno != oggi && oraLocale >= OraPromemoria:
		dopo.Giorno = oggi
		return testoPromemoria(ancora), true, dopo, true
	}
	return Avviso{}, false, dopo, !uguali(prima, dopo)
}

// uguali dice se due stati degli avvisi dicono la stessa cosa (una mappa vuota vale una mappa assente).
func uguali(x, y statoAvvisi) bool {
	return x.Giorno == y.Giorno && mappeUguali(x.Aperti, y.Aperti) && mappeUguali(x.Assenti, y.Assenti)
}

func mappeUguali(x, y map[string]time.Time) bool {
	if len(x) != len(y) {
		return false
	}
	for k, v := range x {
		if w, ok := y[k]; !ok || !w.Equal(v) {
			return false
		}
	}
	return true
}

// inVerifica sono i problemi aperti che adesso mancano dall'allarme ma non ancora da AttesaRisolto.
func inVerifica(cc []string, aperti map[string]time.Time) []string {
	var v []string
	for _, c := range cc {
		v = append(v, "(dal "+clock.Timbro(aperti[c])+") "+titoloAvviso(c)+": sembra sistemato, si aspetta la conferma.")
	}
	return v
}

// ordineCodice mette i codici risolti nell'ordine dei Problemi (quello del banner); uno sconosciuto in fondo.
func ordineCodice(c string) string {
	for i, x := range []string{ProblemaNonConfigurato, ProblemaChiavetta, ProblemaBackupFallito, ProblemaBackupVecchio,
		ProblemaOrologio, ProblemaProvaFallita, ProblemaProvaMancante, ProblemaMediaRovinati} {
		if x == c {
			return fmt.Sprintf("%02d", i)
		}
	}
	return "99" + c
}

const piedeAvviso = "Il titolare vede il riquadro rosso in GestRip finché il problema non è sistemato; fino ad allora " +
	"arriva un promemoria al giorno, alle 9."

func elenco(b *strings.Builder, titolo string, righe []string) {
	if len(righe) == 0 {
		return
	}
	b.WriteString("\n\n" + titolo)
	for _, r := range righe {
		b.WriteString("\n• " + r)
	}
}

func testi(pp []Problema) []string {
	var v []string
	for _, p := range pp {
		v = append(v, p.Testo)
	}
	return v
}

func titoli(cc []string) []string {
	var v []string
	for _, c := range cc {
		v = append(v, titoloAvviso(c))
	}
	return v
}

// conData sono i testi dei problemi aperti con il giorno e l'ora in cui sono stati avvisati la prima volta.
func conData(pp []Problema, aperti map[string]time.Time) []string {
	var v []string
	for _, p := range pp {
		v = append(v, "(dal "+clock.Timbro(aperti[p.Codice])+") "+p.Testo)
	}
	return v
}

func testoNascita(nuovi []Problema, ancora []string, risolti []string) Avviso {
	var b strings.Builder
	b.WriteString("⚠ Backup del negozio: c'è un problema nuovo.")
	elenco(&b, "Nuovo:", testi(nuovi))
	elenco(&b, "Ancora da sistemare:", ancora)
	elenco(&b, "Risolto:", titoli(risolti))
	b.WriteString("\n\n" + piedeAvviso)
	return Avviso{Oggetto: "GestRip: problema con i backup", Testo: b.String()}
}

func testoRisolti(ancora []string, risolti []string) Avviso {
	var b strings.Builder
	if len(ancora) == 0 {
		b.WriteString("✅ Backup del negozio: di nuovo tutto a posto.")
		elenco(&b, "Risolto:", titoli(risolti))
		b.WriteString("\n\nNon ci sono altri problemi con i backup.")
		return Avviso{Oggetto: "GestRip: backup di nuovo a posto", Testo: b.String()}
	}
	b.WriteString("✅ Backup del negozio: un problema è risolto, ma non tutti.")
	elenco(&b, "Risolto:", titoli(risolti))
	elenco(&b, "Ancora da sistemare:", ancora)
	b.WriteString("\n\n" + piedeAvviso)
	return Avviso{Oggetto: "GestRip: backup, un problema risolto", Testo: b.String()}
}

func testoPromemoria(ancora []string) Avviso {
	var b strings.Builder
	b.WriteString("⚠ Backup del negozio: promemoria, c'è ancora un problema da sistemare.")
	elenco(&b, "Ancora da sistemare:", ancora)
	b.WriteString("\n\n" + piedeAvviso)
	return Avviso{Oggetto: "GestRip: i backup hanno ancora un problema", Testo: b.String()}
}

// Lavoro sono gli avvisi in serve: aspetta AttesaAvvisiDopoAvvio, poi fa un giro ogni OgniQuantoAvvisi finché
// aspetta dice di smettere (il contesto finito). Un giro che va in panico o non riesce non ferma il lavoro: lo
// stato degli avvisi resta com'era e il giro dopo riprova.
func (a *Avvisi) Lavoro(ctx context.Context, aspetta func(context.Context, time.Duration) bool) {
	if !aspetta(ctx, AttesaAvvisiDopoAvvio) {
		return
	}
	for ctx.Err() == nil {
		a.giroProtetto(ctx)
		if !aspetta(ctx, OgniQuantoAvvisi) {
			return
		}
	}
}

func (a *Avvisi) giroProtetto(ctx context.Context) {
	defer func() {
		if v := recover(); v != nil {
			a.log.Error("avvisi dei backup: giro interrotto da un panico, si riprova al prossimo giro", "tipo", fmt.Sprintf("%T", v),
				"stack", string(debug.Stack()))
		}
	}()
	if _, _, err := a.Giro(ctx); err != nil && ctx.Err() == nil {
		a.log.Warn("avvisi dei backup: giro non finito, si riprova al prossimo giro", "tipo", fmt.Sprintf("%T", err))
	}
}
