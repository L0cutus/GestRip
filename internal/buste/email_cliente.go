package buste

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// L'avviso email della pagina del cliente (CLI-39, CLI-40, CLI-41, CLI-19 per il link storico), come
// EmailNotice e Workshop.EmailConsent dello standalone (go-baseline), slice 124 e 125 di lì:
//   - le tre colonne della busta: email_cliente (l'indirizzo chiesto, già pulito e in minuscolo),
//     email_confermata_il (scritta solo dal link di conferma, una volta) e email_pronta_inviata_il
//     (l'avviso di pronta, CLI-42: slice 070b). Stato: nessuno senza indirizzo, «in attesa» con
//     l'indirizzo e senza conferma, «confermato» con le due;
//   - chi chiede è chi ha il link (Recipient): sul /r/ di una busta conto terzi l'oreficeria; il
//     link storico /stato di una busta conto terzi non ha il riquadro (CLI-17, CLI-28); una busta
//     chiusa nemmeno. L'indirizzo è di chi l'ha chiesto per ultimo (address_party/1, dalle righe
//     repair.email_optin_requested): quello dell'altra parte non si mostra e non si tocca da qui;
//   - la richiesta: consenso («1», «on», «true» senza spazi ai bordi), indirizzo (NormalizzaEmail),
//     poi in una transazione la busta riletta, il limite di 6 all'ora per busta (righe d'audit), le
//     email in coda per l'indirizzo sostituito che falliscono, le colonne e la riga
//     repair.email_optin_requested; dopo il commit l'email di conferma va in coda (Posta) con i testi
//     dell'editor (email_conferma_*) e il piè con il link per disattivare. Una coda che rifiuta lascia
//     l'indirizzo in attesa e la riga repair.email_optin_not_queued: ErrEmailNonInviata;
//   - D-16 (solo Go): il riquadro e la richiesta solo con l'email attiva (EmailDisponibile: canale
//     acceso e configurato, uscita.EmailAttiva, e un indirizzo pubblico su cui scrivere i link).
//     Senza, la richiesta è ErrEmailNonDisponibile prima di scrivere: lo standalone scrive
//     l'indirizzo e poi dice che l'email non è partita;
//   - i link di conferma (48 ore) e di disattivazione (10 anni) sono firmati dal Go (firmaLink) con
//     una sottochiave della chiave del server, come i gettoni della galleria; portano la busta,
//     un'impronta con chiave dell'indirizzo (mai l'indirizzo) e, la conferma, l'id della riga della
//     richiesta. I vecchi link Phoenix.Token dello standalone non si accettano (D-02): se ne
//     riconosce la forma (ErrLinkEmailVecchio) e la pagina offre il ripiego;
//   - il link apre una pagina con un pulsante e solo il pulsante scrive (uno scanner della posta che
//     apre i link non conferma e non disattiva niente);
//   - l'audit non porta mai l'indirizzo.

// Posta è la parte della coda in uscita che le buste usano per le email del cliente: la coda vera
// (uscita.Coda) in gestrip serve, una finta nei test.
type Posta interface {
	Accoda(ctx context.Context, dest uscita.Destinazione, cont uscita.Contenuto, a audit.Contesto) (int64, error)
	AccodaIn(ctx context.Context, tx *sql.Tx, dest uscita.Destinazione, cont uscita.Contenuto, a audit.Contesto) (int64, error)
	CanaleAttivo(ctx context.Context, canale string) (bool, error)
}

// ConPosta dà alle buste la coda per le email del cliente e la chiave dei loro link
// (db.SottoChiave(chiave, "email")). Senza, l'email del cliente non è mai disponibile e ogni link
// è rifiutato.
func (b *Buste) ConPosta(p Posta, chiave []byte) *Buste {
	b.posta, b.chiaveEmail = p, chiave
	return b
}

// Le azioni d'audit dell'avviso email, quelle dello standalone; stopInviato è solo del Go (D-02).
const (
	azioneRichiesta     = "repair.email_optin_requested"
	azioneConferma      = "repair.email_optin_confirmed"
	azioneNonAccodata   = "repair.email_optin_not_queued"
	azioneDisattivata   = "repair.email_optout"
	azioneStopInviato   = "repair.email_stop_link_sent"
	azioneAccodata      = "repair.email_optin_queued" // solo Go: la riga della coda dell'email di conferma
	limiteStopIndirizzo = 3                           // al più tante email di ripiego al giorno per indirizzo
	limiteEmail         = 6
	finestraEmail       = time.Hour
	durataConferma      = 48 * time.Hour
	durataStop          = 10 * 365 * 24 * time.Hour
	massimoIndirizzo    = 254
	erroreDisattivata   = "Avvisi email disattivati dal destinatario" // @withdrawn
	scopoConferma       = "conf"
	scopoStop           = "stop"
	busteRipiego        = 10 // al più tante buste per un invio della pagina di ripiego
	limiteStopPerBusta  = 3  // al più tanti link di ripiego all'ora per busta
)

// I rifiuti dell'avviso email.
var (
	ErrConsensoEmail       = errors.New("buste: serve il consenso per l'avviso email")
	ErrIndirizzoEmail      = errors.New("buste: indirizzo email non valido")
	ErrEmailChiusa         = errors.New("buste: la busta non riceve più avvisi")
	ErrTroppeRichiesteMail = errors.New("buste: troppe richieste email nell'ultima ora")
	ErrEmailNonInviata     = errors.New("buste: email di conferma non accodata")
	ErrNessunaAttesa       = errors.New("buste: nessun indirizzo in attesa di conferma")
	// ErrEmailNonDisponibile è la D-16: l'email non è attiva (o manca l'indirizzo pubblico) e la
	// pagina non ha il riquadro; niente è stato scritto.
	ErrEmailNonDisponibile = errors.New("buste: avviso email non disponibile")
	ErrLinkEmailNonValido  = errors.New("buste: link email non valido o scaduto")
	// ErrLinkEmailVecchio è un link dello standalone (Phoenix.Token), che il Go non verifica (D-02).
	ErrLinkEmailVecchio   = errors.New("buste: link email dello standalone")
	ErrIndirizzoCambiato  = errors.New("buste: l'indirizzo email della busta è cambiato")
	ErrRichiestaSostituta = errors.New("buste: una richiesta più recente ha sostituito il link")
	// errEmailNascosta è {:error, :hidden}: il link storico di una busta conto terzi.
	errEmailNascosta = errors.New("buste: avviso email nascosto per questo link")
)

// RichiestaEmail è quello che il modulo del riquadro manda: «Reinvia conferma» (reinvia=1) o
// l'indirizzo e il consenso, ognuno con il suo «c'è» (un campo che manca non è un testo vuoto).
type RichiestaEmail struct {
	Reinvia                   bool
	Indirizzo, Consenso       string
	ConIndirizzo, ConConsenso bool
}

// RiquadroEmail è il riquadro della pagina (EmailNotice.box/2): Stato «none», «pending» o
// «confirmed»; l'indirizzo solo se c'è; Consegna solo in attesa: «queued», «accepted», «failed»
// della riga dell'ultima email, «not_queued» se l'ultima richiesta non ha accodato niente,
// «unknown» per un indirizzo che nessuna richiesta del Go ha scritto (importato).
type RiquadroEmail struct {
	Stato, Indirizzo, Consegna string
}

// EsitoConferma è la conferma riuscita: Gia se era già confermata, Link il token del link stabile
// della busta se è vivo e si può rileggere (stable_link/1), "" altrimenti.
type EsitoConferma struct {
	Gia  bool
	Link string
}

// reIndirizzo è _EMAIL_RE di EmailConsent, con «\s» letto come lo spazio Unicode (unicode.IsSpace).
var reIndirizzo = regexp.MustCompile(`\A[^@]+@[^@]+\.[^@]{2,}\z`)

// NormalizzaEmail è EmailConsent.normalize_address/1: senza spazi ai bordi e in minuscolo, al più
// 254 caratteri, una sola «@», niente spazi, un punto nel dominio con almeno due caratteri dopo.
func NormalizzaEmail(s string) (string, bool) {
	a := strings.ToLower(strings.TrimFunc(s, unicode.IsSpace))
	if utf8.RuneCountInString(a) > massimoIndirizzo || strings.IndexFunc(a, unicode.IsSpace) >= 0 || !reIndirizzo.MatchString(a) {
		return "", false
	}
	return a, true
}

// consentito è consented?/1: «1», «on» o «true», senza spazi ai bordi.
func consentito(r RichiestaEmail) bool {
	if !r.ConConsenso {
		return false
	}
	switch strings.TrimFunc(r.Consenso, unicode.IsSpace) {
	case "1", "on", "true":
		return true
	}
	return false
}

// EmailDisponibile è la domanda della D-16 per la pagina del cliente: la coda ha l'email accesa e
// configurata (uscita.EmailAttiva, tramite CanaleAttivo) e c'è l'indirizzo pubblico base su cui
// scrivere i link delle email.
func (b *Buste) EmailDisponibile(ctx context.Context, base string) (bool, error) {
	if base == "" || b.posta == nil || len(b.chiaveEmail) == 0 {
		return false, nil
	}
	return b.posta.CanaleAttivo(ctx, uscita.CanaleEmail)
}

// bustaEmail è quello che l'avviso email guarda della busta.
type bustaEmail struct {
	id                    int64
	numero, codiceQR      string
	stato                 string
	oreficeriaID          *int64
	storicoRevocato       bool
	nomeCliente           string
	descrizione           string
	indirizzo, confermata *string
}

const colonneBustaEmail = `id, numero, coalesce(codice_qr, ''), stato, oreficeria_id, codice_flask_revocato_il IS NOT NULL,
	nome_cliente, coalesce(descrizione, ''), email_cliente, email_confermata_il FROM buste`

func leggiBustaEmail(ctx context.Context, q lettore, dove string, args ...any) (bustaEmail, error) {
	var be bustaEmail
	var oref sql.NullInt64
	var revocato int
	var indirizzo, confermata sql.NullString
	err := q.QueryRowContext(ctx, `SELECT `+colonneBustaEmail+` WHERE `+dove, args...).Scan(&be.id, &be.numero, &be.codiceQR,
		&be.stato, &oref, &revocato, &be.nomeCliente, &be.descrizione, &indirizzo, &confermata)
	if errors.Is(err, sql.ErrNoRows) {
		return bustaEmail{}, ErrBustaNonTrovata
	}
	if err != nil {
		return bustaEmail{}, err
	}
	be.oreficeriaID, be.storicoRevocato = nullaIntero(oref), revocato == 1
	be.indirizzo, be.confermata = nullaTesto(indirizzo), nullaTesto(confermata)
	return be, nil
}

// b2b è Recipient.party/1 == :oreficeria.
func (be bustaEmail) b2b() bool { return Classificazione(be.oreficeriaID) == ClasseEsterna }

// parte è la controparte della busta: «oreficeria» o «customer».
func (be bustaEmail) parte() string {
	if be.b2b() {
		return "oreficeria"
	}
	return "customer"
}

// chiTieneIlLink è holder/1: il cliente, o l'oreficeria con il suo id (ognuna la sua).
func (be bustaEmail) chiTieneIlLink() string {
	if be.b2b() {
		return "oreficeria:" + strconv.FormatInt(*be.oreficeriaID, 10)
	}
	return "customer"
}

// datiDiChi è holder_metadata/1.
func (be bustaEmail) datiDiChi() map[string]any {
	if be.b2b() {
		return map[string]any{"party": "oreficeria", "oreficeria_id": *be.oreficeriaID}
	}
	return map[string]any{"party": "customer"}
}

// statoConsenso è EmailConsent.state/1.
func (be bustaEmail) statoConsenso() string {
	switch {
	case be.indirizzo == nil:
		return "none"
	case be.confermata == nil:
		return "pending"
	}
	return "confirmed"
}

// chiusa è Repair.closed?/1.
func (be bustaEmail) chiusa() bool { return ConversazioneChiusa(be.stato) }

// visibile è visible/2: il link storico di una busta revocata è una busta che non c'è, una busta
// chiusa non riceve avvisi, il link storico di una busta conto terzi non ha il riquadro.
func (be bustaEmail) visibile(storico bool) error {
	switch {
	case storico && be.storicoRevocato:
		return ErrBustaNonTrovata
	case be.chiusa():
		return ErrEmailChiusa
	case storico && be.b2b():
		return errEmailNascosta
	}
	return nil
}

// richiedente è address_party/1: chi ha scritto l'indirizzo con l'ultima richiesta che non è un
// «Reinvia» («oreficeria:<id>», «oreficeria:?» per una riga di prima che nominasse l'oreficeria, di
// nessuno), o il cliente per un indirizzo che nessuna richiesta ha scritto (importato).
func richiedente(ctx context.Context, q lettore, id int64) (string, error) {
	var parte sql.NullString
	var oref sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT json_extract(dati, '$.party'), json_extract(dati, '$.oreficeria_id') FROM eventi_audit
		WHERE azione = ? AND risorsa = 'repair' AND id_risorsa = ? AND json_extract(dati, '$.resend') = 0
		ORDER BY id DESC LIMIT 1`, azioneRichiesta, strconv.FormatInt(id, 10)).Scan(&parte, &oref)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "customer", nil
	case err != nil:
		return "", err
	case parte.String != "oreficeria":
		return "customer", nil
	case !oref.Valid:
		return "oreficeria:?", nil
	}
	return "oreficeria:" + strconv.FormatInt(oref.Int64, 10), nil
}

// proprio è own_address?/1: senza indirizzo sì; altrimenti l'indirizzo è di chi ha il link adesso.
func proprio(ctx context.Context, q lettore, be bustaEmail) (bool, error) {
	if be.indirizzo == nil {
		return true, nil
	}
	chi, err := richiedente(ctx, q, be.id)
	return chi == be.chiTieneIlLink(), err
}

// RiquadroEmail è EmailNotice.box/2 della busta id: nil dove il link non mostra il riquadro (busta
// chiusa, link storico di una busta conto terzi o revocata). Non scrive niente. Chi la chiama ha
// già deciso che l'email è disponibile (D-16).
func (b *Buste) RiquadroEmail(ctx context.Context, id int64, storico bool) (*RiquadroEmail, error) {
	be, err := leggiBustaEmail(ctx, b.d.Lettura, `id = ? AND cancellata_il IS NULL`, id)
	if errors.Is(err, ErrBustaNonTrovata) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if be.visibile(storico) != nil {
		return nil, nil
	}
	mio, err := proprio(ctx, b.d.Lettura, be)
	if err != nil {
		return nil, err
	}
	r := &RiquadroEmail{Stato: "none"}
	if mio {
		r.Stato = be.statoConsenso()
	}
	if r.Stato != "none" {
		r.Indirizzo = *be.indirizzo
	}
	if r.Stato == "pending" {
		if r.Consegna, err = b.consegnaConferma(ctx, be); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// consegnaConferma è delivery/1: l'ultima richiesta della busta e l'email di conferma che quella
// richiesta ha accodato, per id (la riga repair.email_optin_queued del Go): un'altra email
// all'indirizzo, come quella della pagina di ripiego, non parla per lei (review della 070). Una
// richiesta che nessuna riga del Go segue (importata dallo standalone, o di prima) si legge come
// delivery/1, ma solo fra le email di conferma (con il loro link) dell'indirizzo, dalla richiesta in poi.
func (b *Buste) consegnaConferma(ctx context.Context, be bustaEmail) (string, error) {
	var richiesta int64
	var il string
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT id, creato_il FROM eventi_audit WHERE azione = ? AND risorsa = 'repair' AND id_risorsa = ?
		ORDER BY id DESC LIMIT 1`, azioneRichiesta, strconv.FormatInt(be.id, 10)).Scan(&richiesta, &il)
	if errors.Is(err, sql.ErrNoRows) {
		return "unknown", nil
	}
	if err != nil {
		return "", err
	}
	var messaggio sql.NullInt64
	err = b.d.Lettura.QueryRowContext(ctx, `SELECT json_extract(dati, '$.message_id') FROM eventi_audit WHERE azione = ? AND risorsa = 'repair'
		AND id_risorsa = ? AND id > ? ORDER BY id DESC LIMIT 1`, azioneAccodata, strconv.FormatInt(be.id, 10), richiesta).Scan(&messaggio)
	var stato string
	switch {
	case err == nil && messaggio.Valid:
		err = b.d.Lettura.QueryRowContext(ctx, `SELECT stato FROM messaggi_in_uscita WHERE id = ? AND busta_id = ?`, messaggio.Int64, be.id).Scan(&stato)
	case err == nil, errors.Is(err, sql.ErrNoRows):
		err = b.d.Lettura.QueryRowContext(ctx, `SELECT stato FROM messaggi_in_uscita WHERE busta_id = ? AND canale = 'email' AND indirizzo = ?
			AND accodato_il >= ? AND instr(testo, '/stato/email-conferma?t=') > 0 ORDER BY id DESC LIMIT 1`, be.id, *be.indirizzo, il).Scan(&stato)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "not_queued", nil
	}
	return stato, err
}

// sottoLimite è under_limit/3: le righe azione della busta nell'ultima ora (soloPagina: solo quelle
// con via «page») sono meno di limiteEmail.
func (b *Buste) sottoLimite(ctx context.Context, tx *sql.Tx, id int64, azione string, soloPagina bool) error {
	dal := b.o.Adesso().UTC().Add(-finestraEmail).Format(db.FormatoIstante)
	q := `SELECT count(*) FROM eventi_audit WHERE azione = ? AND risorsa = 'repair' AND id_risorsa = ? AND creato_il >= ?`
	if soloPagina {
		q += ` AND json_extract(dati, '$.via') = 'page'`
	}
	var n int
	if err := tx.QueryRowContext(ctx, q, azione, strconv.FormatInt(id, 10), dal).Scan(&n); err != nil {
		return err
	}
	if n >= limiteEmail {
		return ErrTroppeRichiesteMail
	}
	return nil
}

// ritiraInCoda è withdraw_queued/2: le email della busta ancora in coda per indirizzo falliscono
// con motivo, così il giro della coda non le prende più.
func ritiraInCoda(ctx context.Context, tx *sql.Tx, id int64, indirizzo, motivo, ora string) error {
	_, err := tx.ExecContext(ctx, `UPDATE messaggi_in_uscita SET stato = 'failed', fallito_il = ?1, ultimo_errore = ?2
		WHERE busta_id = ?3 AND canale = 'email' AND indirizzo = ?4 AND stato = 'queued'`, ora, motivo, id, indirizzo)
	return err
}

// ChiediEmailCliente è email_optin/2: la richiesta (o «Reinvia conferma») dal riquadro della pagina
// del link token. ErrBustaNonTrovata per un link morto.
func (b *Buste) ChiediEmailCliente(ctx context.Context, token string, rich RichiestaEmail, base string, c audit.Contesto) error {
	t, err := b.bustaDelToken(ctx, token)
	if err != nil {
		return err
	}
	err = b.chiediEmail(ctx, t.id, false, rich, base, c)
	if errors.Is(err, errEmailNascosta) {
		return ErrBustaNonTrovata
	}
	return err
}

// ChiediEmailStorica è legacy_email_optin/2: la stessa richiesta dalla pagina storica, con la
// credenziale. Ogni rifiuto della credenziale, e una busta conto terzi, è ErrBustaNonTrovata con la
// riga repair.legacy_public_denied.
func (b *Buste) ChiediEmailStorica(ctx context.Context, scritto string, cred CredenzialeStorica, rich RichiestaEmail, base string, c audit.Contesto) error {
	s, ok, err := b.cercaStorica(ctx, scritto)
	if err != nil {
		return err
	}
	if !ok || !b.autorizzata(s, scritto, cred) {
		b.rifiutoStorico(ctx, c)
		return ErrBustaNonTrovata
	}
	err = b.chiediEmail(ctx, s.id, true, rich, base, c)
	if errors.Is(err, errEmailNascosta) {
		b.rifiutoStorico(ctx, c)
		return ErrBustaNonTrovata
	}
	return err
}

// chiediEmail è request/5 e resend/3 (write_and_send/4).
func (b *Buste) chiediEmail(ctx context.Context, id int64, storico bool, rich RichiestaEmail, base string, c audit.Contesto) error {
	disponibile, err := b.EmailDisponibile(ctx, base)
	if err != nil {
		return err
	}
	// Con l'email disponibile i controlli del modulo nell'ordine dello standalone; senza (D-16) la
	// pagina non aveva il riquadro, e la risposta è «non disponibile» qualunque cosa sia stata scritta.
	var indirizzo string
	if !rich.Reinvia && disponibile {
		if !consentito(rich) {
			return ErrConsensoEmail
		}
		var ok bool
		if indirizzo, ok = NormalizzaEmail(rich.Indirizzo); !ok || !rich.ConIndirizzo {
			return ErrIndirizzoEmail
		}
	}
	_, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	// Riletta nella transazione: la busta può essere stata chiusa, cestinata o passata a un'altra
	// controparte dopo che la pagina è stata disegnata.
	be, err := leggiBustaEmail(ctx, tx, `id = ? AND cancellata_il IS NULL`, id)
	if err != nil {
		return err
	}
	if err := be.visibile(storico); err != nil {
		return err
	}
	if !disponibile {
		return ErrEmailNonDisponibile
	}
	if rich.Reinvia {
		mio, err := proprio(ctx, tx, be)
		if err != nil {
			return err
		}
		if be.statoConsenso() != "pending" || !mio {
			return ErrNessunaAttesa
		}
		indirizzo = *be.indirizzo
	}
	if err := b.sottoLimite(ctx, tx, id, azioneRichiesta, false); err != nil {
		return err
	}
	if be.indirizzo != nil && *be.indirizzo != indirizzo {
		if err := ritiraInCoda(ctx, tx, id, *be.indirizzo, erroreDisattivata, ora); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE buste SET email_cliente = ?1, email_confermata_il = NULL, email_pronta_inviata_il = NULL,
		aggiornato_il = ?2 WHERE id = ?3 AND (email_cliente IS NOT ?1 OR email_confermata_il IS NOT NULL OR email_pronta_inviata_il IS NOT NULL)`,
		indirizzo, ora, id); err != nil {
		return err
	}
	dati := be.datiDiChi()
	dati["bag_number"], dati["resend"] = be.numero, rich.Reinvia
	richiesta, err := b.reg.ScriviConID(ctx, tx, c, azioneRichiesta, "repair", id, "ok", dati)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	be.indirizzo, be.confermata = &indirizzo, nil
	return b.mandaConferma(ctx, be, richiesta, base, c)
}

// mandaConferma è send_confirmation/3, dopo il commit: l'email di conferma in coda; una coda che
// rifiuta costa l'email, mai la richiesta, e il mancato invio si scrive (record_miss/3).
func (b *Buste) mandaConferma(ctx context.Context, be bustaEmail, richiesta int64, base string, c audit.Contesto) error {
	// La richiesta è già scritta: un browser che chiude la connessione adesso non lascia l'indirizzo in
	// attesa senza l'email e senza la riga del mancato invio.
	ctx = context.WithoutCancel(ctx)
	motivo := "error"
	err := func() error {
		valori, err := b.valoriEmail(ctx, be)
		if err != nil {
			return err
		}
		tok, err := b.firmaLink(scopoConferma, strconv.FormatInt(be.id, 10), b.improntaIndirizzo(*be.indirizzo), strconv.FormatInt(richiesta, 10))
		if err != nil {
			return err
		}
		valori["link_conferma"] = base + "/stato/email-conferma?t=" + tok
		cont, modello, err := b.contenutoEmail(ctx, FamEmailConfermaOggetto, FamEmailConfermaTesto, valori, be, base)
		if err != nil {
			return err
		}
		if modello {
			motivo = "invalid_template"
			return ErrEmailNonInviata
		}
		id, err := b.posta.Accoda(ctx, uscita.Destinazione{BustaID: &be.id, Canale: uscita.CanaleEmail, Destinatario: be.parte(),
			Indirizzo: *be.indirizzo}, cont, c)
		if errors.Is(err, uscita.ErrNonConfigurato) {
			motivo = "not_configured"
		}
		if err == nil {
			// Solo Go: quale riga della coda è l'email di questa richiesta (consegnaConferma). Se non si
			// scrive, l'email è partita lo stesso; il riquadro la cerca fra le email di conferma.
			_ = b.reg.Registra(ctx, c, azioneAccodata, "repair", be.id, "ok", map[string]any{"bag_number": be.numero, "message_id": id})
		}
		return err
	}()
	if err == nil {
		return nil
	}
	// record_miss/3: una riga che non si scrive non cambia la risposta.
	_ = b.reg.Registra(ctx, c, azioneNonAccodata, "repair", be.id, "ok", map[string]any{"bag_number": be.numero, "reason": motivo})
	return ErrEmailNonInviata
}

// valoriEmail è il contesto comune dei testi dell'email (content/3 e ready_content/3): {nome} di chi
// ha il link («cliente» senza), {numero} come nei testi (D-82), {descrizione}, {negozio}.
func (b *Buste) valoriEmail(ctx context.Context, be bustaEmail) (map[string]string, error) {
	nome := be.nomeCliente
	if be.b2b() {
		if err := b.d.Lettura.QueryRowContext(ctx, `SELECT nome FROM oreficerie WHERE id = ?`, *be.oreficeriaID).Scan(&nome); err != nil {
			return nil, err
		}
	}
	if nome = strings.TrimFunc(nome, unicode.IsSpace); nome == "" {
		nome = "cliente"
	}
	negozio, err := b.nomeNegozio(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]string{"nome": nome, "numero": NumeroNeiTesti(be.codiceQR, be.numero), "descrizione": be.descrizione,
		"negozio": negozio}, nil
}

// reAcapi sono gli a capo che one_line/1 fa diventare uno spazio.
var reAcapi = regexp.MustCompile(`[\r\n]+`)

// contenutoEmail compone oggetto e testo delle due famiglie (render/2: un testo salvato che non si
// compone dà modello vero e nessun contenuto, mai il predefinito); l'oggetto su una riga
// (one_line/1), il testo con il piè della busta.
func (b *Buste) contenutoEmail(ctx context.Context, fo, ft Famiglia, valori map[string]string, be bustaEmail, base string) (uscita.Contenuto, bool, error) {
	oggetto, m1, err := b.componiFamiglia(ctx, fo, valori)
	if err != nil {
		return uscita.Contenuto{}, false, err
	}
	testo, m2, err := b.componiFamiglia(ctx, ft, valori)
	if err != nil {
		return uscita.Contenuto{}, false, err
	}
	if m1 != nil || m2 != nil {
		return uscita.Contenuto{}, true, nil
	}
	piede, err := b.piedeEmail(be, valori["negozio"], base)
	if err != nil {
		return uscita.Contenuto{}, false, err
	}
	oggetto = strings.TrimFunc(reAcapi.ReplaceAllString(oggetto, " "), unicode.IsSpace)
	return uscita.Contenuto{Oggetto: &oggetto, Testo: testo + piede}, false, nil
}

// piedeEmail è footer/1: le parole dello standalone e il link per disattivare, firmato per l'indirizzo
// della busta.
func (b *Buste) piedeEmail(be bustaEmail, negozio, base string) (string, error) {
	tok, err := b.firmaLink(scopoStop, strconv.FormatInt(be.id, 10), b.improntaIndirizzo(*be.indirizzo))
	if err != nil {
		return "", err
	}
	return "\n\n---\nRicevi questa email perché hai attivato l'avviso dalla pagina stato della tua riparazione presso " + negozio +
		".\nPer disattivare gli avvisi: " + base + "/stato/email-disattiva?t=" + tok, nil
}

// DisattivaEmailCliente è email_optout/2: «Disattiva» dal riquadro della pagina del link token.
func (b *Buste) DisattivaEmailCliente(ctx context.Context, token string, c audit.Contesto) error {
	t, err := b.bustaDelToken(ctx, token)
	if err != nil {
		return err
	}
	return b.disattivaEmail(ctx, t.id, false, c)
}

// DisattivaEmailStorica è legacy_email_optout/2: lo stesso dalla pagina storica, con la credenziale;
// ogni rifiuto della credenziale, e una busta conto terzi, è ErrBustaNonTrovata con la riga
// repair.legacy_public_denied.
func (b *Buste) DisattivaEmailStorica(ctx context.Context, scritto string, cred CredenzialeStorica, c audit.Contesto) error {
	s, ok, err := b.cercaStorica(ctx, scritto)
	if err != nil {
		return err
	}
	if !ok || !b.autorizzata(s, scritto, cred) {
		b.rifiutoStorico(ctx, c)
		return ErrBustaNonTrovata
	}
	err = b.disattivaEmail(ctx, s.id, true, c)
	if errors.Is(err, errEmailNascosta) {
		b.rifiutoStorico(ctx, c)
		return ErrBustaNonTrovata
	}
	return err
}

// disattivaEmail è revoke/3: in una transazione la busta riletta, il link storico solo per una busta
// del cliente, il limite di 6 all'ora delle disattivazioni dalla pagina, poi solo l'indirizzo di chi
// ha il link (clear_own/2). Anche su una busta chiusa, come la rotta dello standalone. Senza un
// indirizzo proprio niente si scrive e la risposta è la stessa.
func (b *Buste) disattivaEmail(ctx context.Context, id int64, storico bool, c audit.Contesto) error {
	_, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	be, err := leggiBustaEmail(ctx, tx, `id = ? AND cancellata_il IS NULL`, id)
	if err != nil {
		return err
	}
	if storico && be.b2b() {
		return errEmailNascosta
	}
	if err := b.sottoLimite(ctx, tx, id, azioneDisattivata, true); err != nil {
		return err
	}
	mio, err := proprio(ctx, tx, be)
	if err != nil {
		return err
	}
	if !mio {
		return nil
	}
	if err := b.togliEmail(ctx, tx, be, "page", ora, c); err != nil {
		return err
	}
	return tx.Commit()
}

// togliEmail è clear/4: le email in coda per l'indirizzo falliscono, le tre colonne si vuotano e la
// riga repair.email_optout con via, mai l'indirizzo. Senza indirizzo niente.
func (b *Buste) togliEmail(ctx context.Context, tx *sql.Tx, be bustaEmail, via, ora string, c audit.Contesto) error {
	if be.indirizzo == nil {
		return nil
	}
	if err := ritiraInCoda(ctx, tx, be.id, *be.indirizzo, erroreDisattivata, ora); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE buste SET email_cliente = NULL, email_confermata_il = NULL, email_pronta_inviata_il = NULL,
		aggiornato_il = ?1 WHERE id = ?2`, ora, be.id); err != nil {
		return err
	}
	return b.reg.Scrivi(ctx, tx, c, azioneDisattivata, "repair", be.id, "ok", map[string]any{"bag_number": be.numero, "via": via})
}

// ControllaConferma è check_confirmation/1: cosa farebbe il link di conferma, senza scrivere
// niente. Gia dice se l'indirizzo è già confermato.
func (b *Buste) ControllaConferma(ctx context.Context, tok string) (EsitoConferma, error) {
	id, impronta, richiesta, err := b.leggiConferma(tok)
	if err != nil {
		return EsitoConferma{}, err
	}
	be, err := b.attuale(ctx, b.d.Lettura, id, impronta, richiesta)
	if err != nil {
		return EsitoConferma{}, err
	}
	e := EsitoConferma{Gia: be.confermata != nil}
	if e.Gia {
		e.Link, err = b.linkStabile(ctx, b.d.Lettura, id)
	}
	return e, err
}

// linkStabile è stable_link/1: il token del link della busta solo se ha il testo, non è revocato e non
// è scaduto ("" altrimenti). Una conferma è aperta a chiunque abbia l'email, e un link nuovo
// ucciderebbe quello di chi ha la busta: qui non se ne emette mai uno.
func (b *Buste) linkStabile(ctx context.Context, q lettore, id int64) (string, error) {
	_, ora := b.adesso()
	var testo sql.NullString
	err := q.QueryRowContext(ctx, `SELECT token_pubblico FROM buste WHERE id = ? AND token_pubblico IS NOT NULL
		AND token_pubblico_revocato_il IS NULL AND token_pubblico_scade_il > ?`, id, ora).Scan(&testo)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return testo.String, err
}

// ConfermaEmail è confirm/2: il pulsante «Conferma». La prima volta scrive l'istante della conferma
// e repair.email_optin_confirmed; dopo risponde lo stesso senza scrivere. Il link stabile della
// busta, se è vivo e rileggibile, torna in Link.
func (b *Buste) ConfermaEmail(ctx context.Context, tok string, c audit.Contesto) (EsitoConferma, error) {
	id, impronta, richiesta, err := b.leggiConferma(tok)
	if err != nil {
		return EsitoConferma{}, err
	}
	_, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return EsitoConferma{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	be, err := b.attuale(ctx, tx, id, impronta, richiesta)
	if err != nil {
		return EsitoConferma{}, err
	}
	e := EsitoConferma{Gia: be.confermata != nil}
	if !e.Gia {
		if _, err := tx.ExecContext(ctx, `UPDATE buste SET email_confermata_il = ?1, aggiornato_il = ?1 WHERE id = ?2`, ora, id); err != nil {
			return EsitoConferma{}, err
		}
		if err := b.reg.Scrivi(ctx, tx, c, azioneConferma, "repair", id, "ok", map[string]any{"bag_number": be.numero}); err != nil {
			return EsitoConferma{}, err
		}
	}
	if e.Link, err = b.linkStabile(ctx, tx, id); err != nil {
		return EsitoConferma{}, err
	}
	return e, tx.Commit()
}

// attuale è current/3: la busta (fuori dal cestino) porta ancora l'indirizzo per cui il link è
// firmato, e nessuna richiesta o disattivazione è venuta dopo quella del link.
func (b *Buste) attuale(ctx context.Context, q lettore, id int64, impronta string, richiesta int64) (bustaEmail, error) {
	be, err := leggiBustaEmail(ctx, q, `id = ? AND cancellata_il IS NULL`, id)
	if errors.Is(err, ErrBustaNonTrovata) || err == nil && (be.indirizzo == nil || !hmac.Equal([]byte(b.improntaIndirizzo(*be.indirizzo)), []byte(impronta))) {
		return bustaEmail{}, ErrIndirizzoCambiato
	}
	if err != nil {
		return bustaEmail{}, err
	}
	var dopo int
	if err := q.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM eventi_audit WHERE risorsa = 'repair' AND id_risorsa = ? AND id > ?
		AND (azione = ? OR (azione = ? AND json_extract(dati, '$.resend') = 0)))`,
		strconv.FormatInt(id, 10), richiesta, azioneDisattivata, azioneRichiesta).Scan(&dopo); err != nil {
		return bustaEmail{}, err
	}
	if dopo == 1 {
		return bustaEmail{}, ErrRichiestaSostituta
	}
	return be, nil
}

// ControllaStop è check_stop/1: se il link di disattivazione è firmato da questa installazione,
// senza scrivere niente.
func (b *Buste) ControllaStop(tok string) error {
	_, _, err := b.leggiStop(tok)
	return err
}

// ErrDisattivazioneNonRiuscita è {:error, :unavailable} di stop/2: la scrittura non è riuscita.
var ErrDisattivazioneNonRiuscita = errors.New("buste: disattivazione dell'email non riuscita")

// DisattivaConLink è stop/2: il pulsante «Disattiva» della pagina del link nel piè. Se la busta
// porta ancora l'indirizzo per cui il link è firmato (anche nel cestino: un ripristino non riporta
// un consenso ritirato), via «footer»; altrimenti lo stesso successo senza cambiare niente.
func (b *Buste) DisattivaConLink(ctx context.Context, tok string, c audit.Contesto) error {
	id, impronta, err := b.leggiStop(tok)
	if err != nil {
		return err
	}
	_, ora := b.adesso()
	err = func() error {
		tx, err := b.d.Scrittura.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
		be, err := leggiBustaEmail(ctx, tx, `id = ?`, id)
		if errors.Is(err, ErrBustaNonTrovata) {
			return nil
		}
		if err != nil {
			return err
		}
		if be.indirizzo == nil || !hmac.Equal([]byte(b.improntaIndirizzo(*be.indirizzo)), []byte(impronta)) {
			return nil
		}
		if err := b.togliEmail(ctx, tx, be, "footer", ora, c); err != nil {
			return err
		}
		return tx.Commit()
	}()
	if err != nil {
		return ErrDisattivazioneNonRiuscita
	}
	return nil
}

// Il testo dell'email della pagina di ripiego (D-02, solo Go): non è nell'editor dei testi.
const (
	oggettoRipiego = "Disattiva gli avvisi email — busta {numero}"
	testoRipiego   = "Ciao {nome},\n\nci è arrivata una richiesta di disattivare gli avvisi email della tua riparazione (busta {numero}). " +
		"Per disattivarli apri questo link e premi «Disattiva»:\n\n{link}\n\nSe non l'hai chiesto tu, ignora questa email: " +
		"gli avvisi restano attivi.\n\n— {negozio}"
)

// MandaLinkDisattivazione è il modulo della pagina di ripiego (D-02, solo Go: Stefano, 30/10/2026):
// chi ha un vecchio link di disattivazione dello standalone scrive il suo indirizzo e, se una busta lo
// porta (anche in attesa, anche nel cestino), per ognuna delle ultime busteRipiego gli arriva
// un'email con un link di disattivazione nuovo. Non dice mai a chi chiama se l'indirizzo c'è: il
// numero di email accodate serve solo ai test. Al più limiteStopPerBusta link all'ora per busta
// (riga repair.email_stop_link_sent, mai l'indirizzo); senza email disponibile niente parte.
func (b *Buste) MandaLinkDisattivazione(ctx context.Context, indirizzo, base string, c audit.Contesto) (int, error) {
	a, ok := NormalizzaEmail(indirizzo)
	if !ok {
		return 0, nil
	}
	if disponibile, err := b.EmailDisponibile(ctx, base); err != nil || !disponibile {
		return 0, err
	}
	// Anche un indirizzo importato con maiuscole o spazi (lower e trim di SQLite, ASCII); mandaRipiego
	// ricontrolla sull'impronta dell'indirizzo salvato e normalizzato.
	righe, err := b.d.Lettura.QueryContext(ctx, `SELECT id FROM buste WHERE lower(trim(email_cliente)) = ? ORDER BY id DESC LIMIT ?`,
		strings.ToLower(a), busteRipiego)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for righe.Next() {
		var id int64
		if err := righe.Scan(&id); err != nil {
			_ = righe.Close() // l'errore che conta è quello della lettura
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := righe.Err(); err != nil {
		return 0, err
	}
	accodate := 0
	for _, id := range ids {
		fatto, err := b.mandaRipiego(ctx, id, a, base, c)
		if err != nil {
			return accodate, err
		}
		if fatto {
			accodate++
		}
	}
	return accodate, nil
}

// mandaRipiego manda il link di disattivazione nuovo della busta id, se porta ancora l'indirizzo a
// (salvato anche con maiuscole o spazi), se la busta non ne ha già avuti limiteStopPerBusta
// nell'ultima ora e l'indirizzo limiteStopIndirizzo nell'ultimo giorno. I due conteggi e la riga
// repair.email_stop_link_sent (con l'impronta con chiave dell'indirizzo, mai l'indirizzo) stanno in
// una transazione IMMEDIATE, prima dell'accodamento: due invii insieme non passano il tetto, e una coda
// che rifiuta lo consuma lo stesso. L'email va a chi l'indirizzo l'ha chiesto: se non è di chi ha il
// link adesso, «cliente» e la sua parte (mai il nome dell'oreficeria al cliente finale, CLI-28).
func (b *Buste) mandaRipiego(ctx context.Context, id int64, a, base string, c audit.Contesto) (bool, error) {
	impronta := b.improntaIndirizzo(a)
	adesso, _ := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	be, err := leggiBustaEmail(ctx, tx, `id = ?`, id)
	if errors.Is(err, ErrBustaNonTrovata) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if be.indirizzo == nil {
		return false, nil
	}
	if salvato, ok := NormalizzaEmail(*be.indirizzo); !ok || !hmac.Equal([]byte(b.improntaIndirizzo(salvato)), []byte(impronta)) {
		return false, nil
	}
	var perBusta, perIndirizzo int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM eventi_audit WHERE azione = ? AND risorsa = 'repair' AND id_risorsa = ?
		AND creato_il >= ?`, azioneStopInviato, strconv.FormatInt(id, 10), adesso.Add(-finestraEmail).Format(db.FormatoIstante)).Scan(&perBusta); err != nil {
		return false, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM eventi_audit WHERE azione = ? AND json_extract(dati, '$.address_digest') = ?
		AND creato_il >= ?`, azioneStopInviato, impronta, adesso.Add(-24*time.Hour).Format(db.FormatoIstante)).Scan(&perIndirizzo); err != nil {
		return false, err
	}
	if perBusta >= limiteStopPerBusta || perIndirizzo >= limiteStopIndirizzo {
		return false, nil
	}
	mio, err := proprio(ctx, tx, be)
	if err != nil {
		return false, err
	}
	parte := be.parte()
	if !mio {
		chi, err := richiedente(ctx, tx, be.id)
		if err != nil {
			return false, err
		}
		parte, _, _ = strings.Cut(chi, ":")
	}
	if err := b.reg.Scrivi(ctx, tx, c, azioneStopInviato, "repair", id, "ok",
		map[string]any{"bag_number": be.numero, "party": parte, "address_digest": impronta}); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	valori, err := b.valoriEmail(ctx, be)
	if err != nil {
		return false, err
	}
	if !mio {
		valori["nome"] = "cliente"
	}
	// Il link è firmato per l'indirizzo com'è salvato: DisattivaConLink lo confronta con quello.
	tok, err := b.firmaLink(scopoStop, strconv.FormatInt(id, 10), b.improntaIndirizzo(*be.indirizzo))
	if err != nil {
		return false, err
	}
	valori["link"] = base + "/stato/email-disattiva?t=" + tok
	oggetto, err := componiTesto(oggettoRipiego, valori)
	if err != nil {
		return false, err
	}
	testo, err := componiTesto(testoRipiego, valori)
	if err != nil {
		return false, err
	}
	oggetto = strings.TrimFunc(reAcapi.ReplaceAllString(oggetto, " "), unicode.IsSpace)
	if _, err := b.posta.Accoda(ctx, uscita.Destinazione{BustaID: &be.id, Canale: uscita.CanaleEmail, Destinatario: parte, Indirizzo: a},
		uscita.Contenuto{Oggetto: &oggetto, Testo: testo}, c); err != nil {
		if errors.Is(err, uscita.ErrNonConfigurato) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// I link delle email firmati dal Go: base64url(dati) «.» base64url(HMAC-SHA256), con dati =
// scopo.campi….istante della firma in millisecondi. Lo scopo dentro i dati tiene separati i due
// link (una conferma non è una disattivazione e viceversa); l'impronta dell'indirizzo è un HMAC con
// la stessa chiave e un prefisso suo, quindi nessuno fuori dall'installazione la calcola da un
// indirizzo indovinato (address_digest/1).

var b64 = base64.RawURLEncoding

// errSenzaChiave: le buste non hanno la chiave dei link (ConPosta), niente si firma.
var errSenzaChiave = errors.New("buste: chiave dei link email assente")

func (b *Buste) improntaIndirizzo(a string) string {
	m := hmac.New(sha256.New, b.chiaveEmail)
	m.Write([]byte("repair email address:" + a)) // hash.Hash.Write non fallisce
	return b64.EncodeToString(m.Sum(nil))
}

func (b *Buste) macLink(dati string) []byte {
	m := hmac.New(sha256.New, b.chiaveEmail)
	m.Write([]byte("gestrip email link\x00" + dati)) // hash.Hash.Write non fallisce
	return m.Sum(nil)
}

// firmaLink dà il link dello scopo con i campi, firmato adesso.
func (b *Buste) firmaLink(scopo string, campi ...string) (string, error) {
	if len(b.chiaveEmail) == 0 {
		return "", errSenzaChiave
	}
	dati := scopo + "." + strings.Join(campi, ".") + "." + strconv.FormatInt(b.o.Adesso().UnixMilli(), 10)
	return b64.EncodeToString([]byte(dati)) + "." + b64.EncodeToString(b.macLink(dati)), nil
}

// FormaPhoenix dice se tok ha la forma di un Phoenix.Token dello standalone («SFMyNTY.» e due parti in
// base64url): il Go non lo verifica (D-02) ma lo riconosce, per offrire la pagina di ripiego.
func FormaPhoenix(tok string) bool {
	parti := strings.Split(tok, ".")
	if len(parti) != 3 || parti[0] != "SFMyNTY" {
		return false
	}
	for _, p := range parti[1:] {
		if p == "" || strings.Trim(p, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_") != "" {
			return false
		}
	}
	return true
}

// leggiLink verifica tok per lo scopo, con n campi e la durata: i campi, o ErrLinkEmailVecchio per un
// Phoenix.Token, ErrLinkEmailNonValido per tutto il resto (firma, forma, scopo, scaduto).
func (b *Buste) leggiLink(tok, scopo string, n int, durata time.Duration) ([]string, error) {
	if FormaPhoenix(tok) {
		return nil, ErrLinkEmailVecchio
	}
	if len(b.chiaveEmail) == 0 {
		return nil, ErrLinkEmailNonValido
	}
	d64, m64, ok := strings.Cut(tok, ".")
	if !ok {
		return nil, ErrLinkEmailNonValido
	}
	dati, err1 := b64.DecodeString(d64)
	mac, err2 := b64.DecodeString(m64)
	if err1 != nil || err2 != nil || !hmac.Equal(mac, b.macLink(string(dati))) {
		return nil, ErrLinkEmailNonValido
	}
	parti := strings.Split(string(dati), ".")
	if len(parti) != n+2 || parti[0] != scopo {
		return nil, ErrLinkEmailNonValido
	}
	firmato, err := strconv.ParseInt(parti[n+1], 10, 64)
	if err != nil || b.o.Adesso().UnixMilli() > firmato+durata.Milliseconds() {
		return nil, ErrLinkEmailNonValido
	}
	return parti[1 : n+1], nil
}

// idDelLink è l'id positivo di un campo del link.
func idDelLink(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n > 0
}

// leggiConferma è verify_confirmation/1: busta, impronta dell'indirizzo e id della richiesta.
func (b *Buste) leggiConferma(tok string) (int64, string, int64, error) {
	c, err := b.leggiLink(tok, scopoConferma, 3, durataConferma)
	if err != nil {
		return 0, "", 0, err
	}
	id, ok1 := idDelLink(c[0])
	richiesta, ok2 := idDelLink(c[2])
	if !ok1 || !ok2 || c[1] == "" {
		return 0, "", 0, ErrLinkEmailNonValido
	}
	return id, c[1], richiesta, nil
}

// leggiStop è verify_stop/1: busta e impronta dell'indirizzo.
func (b *Buste) leggiStop(tok string) (int64, string, error) {
	c, err := b.leggiLink(tok, scopoStop, 2, durataStop)
	if err != nil {
		return 0, "", err
	}
	id, ok := idDelLink(c[0])
	if !ok || c[1] == "" {
		return 0, "", ErrLinkEmailNonValido
	}
	return id, c[1], nil
}
