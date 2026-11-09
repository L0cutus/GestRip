package buste

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
)

// La risposta al preventivo (CLI-05, CLI-06, CLI-07, CLI-09, RIP-S-22), come QuoteDecision dello
// standalone (go-baseline, lib/gestrip_standalone/quote_decision.ex):
//   - una busta in «preventivo» riceve **una** risposta dalla sua pagina pubblica, con il token che
//     ha già: l'approvazione la porta a «in_lavorazione», il rifiuto non cambia lo stato;
//   - l'unica autorità è un UPDATE protetto (stato «preventivo», nessuna risposta già data, fuori
//     dal cestino): due tocchi o due telefoni con lo stesso link danno una risposta e un rifiuto,
//     mai due risposte. Il perché del rifiuto si chiede solo dopo, e una risposta già data vince
//     sullo stato (claim_refusal/2);
//   - la revoca la chiede il negozio dalla scheda: toglie il segno (e il motivo), non cancella
//     niente, e aggiunge una riga alla storia (eventi_preventivo, solo aggiunte). La revoca
//     dell'approvazione non cambia lo stato; quella del rifiuto riporta a «preventivo» solo una
//     busta «annullata», nella stessa scrittura (e la conservazione riparte da zero, come
//     retention_started_at). Revocare quello che non c'è è un errore, mai un falso «fatto»;
//   - l'approvazione e la riapertura dopo la revoca del rifiuto non passano dal cambio di stato e
//     non scrivono repair.status_changed, come lo standalone (divergenza dichiarata lì): lo stato
//     che lasciano è nei dati di quote.approved e quote.rejection_revoked. I due passaggi sono
//     comunque nel grafo del Go: preventivo → in_lavorazione è consentito (Consentiti), annullata →
//     preventivo è una riapertura (StatiDiRiapertura);
//   - ogni risposta lascia vuoto il suo segno «letto», così la scheda e il filtro «notifiche» la
//     vedono come nuova (slice 144 dello standalone);
//   - l'audit dice **se** c'è un motivo, mai il testo.
// L'avviso Telegram al negozio dopo la risposta (CLI-08) arriva con il canale Telegram (T4.6); la
// campanella che lo standalone avvisa (Notifications.announce/0) nel Go non c'è ancora (ADM-05):
// il filtro «notifiche» della home legge i segni dal database.

// Gli esiti della risposta e della revoca, con i nomi dello standalone.
var (
	ErrGiaApprovato     = errors.New("buste: preventivo già approvato")              // :already_approved
	ErrGiaRifiutato     = errors.New("buste: preventivo già rifiutato")              // :already_rejected
	ErrNonInPreventivo  = errors.New("buste: busta non in attesa di preventivo")     // :not_quoting
	ErrNonApprovato     = errors.New("buste: nessuna approvazione da revocare")      // :not_approved
	ErrNonRifiutato     = errors.New("buste: nessun rifiuto da revocare")            // :not_rejected
	ErrRispostaNonValid = errors.New("buste: risposta da revocare non riconosciuta") // revoke-quote senza answer
)

const (
	statoInPreventivo = "preventivo"
	statoApprovato    = "in_lavorazione"
	statoAnnullata    = "annullata"
	// motivoRifiutoMassimo è @max_reason: i grafemi del motivo che si tengono.
	motivoRifiutoMassimo = 300
)

// FaseRisposta è dove sta una busta con il suo preventivo (QuoteDecision.state/1).
type FaseRisposta int

// Le fasi, nell'ordine di state/1: risposta data, poi niente da decidere, poi senza importo, poi
// in attesa.
const (
	RispostaNonApplicabile FaseRisposta = iota // :not_applicable
	RispostaSenzaImporto                       // :awaiting_amount
	RispostaInAttesa                           // :pending
	RispostaApprovata                          // {:approved, at}
	RispostaRifiutata                          // {:rejected, at, reason}
)

// StatoRisposta è state/1 o public_state/1: la fase, e per una risposta data l'istante e il motivo.
type StatoRisposta struct {
	Fase   FaseRisposta
	Il     time.Time
	Motivo *string
}

// statoRisposta è QuoteDecision.state/1: l'approvazione, poi il rifiuto (la risposta sopravvive
// allo stato), poi lo stato, poi l'importo (conImporto: c'è un preventivo, testo o centesimi).
func statoRisposta(stato string, approvato, rifiutato *time.Time, motivo *string, conImporto bool) StatoRisposta {
	switch {
	case approvato != nil:
		return StatoRisposta{Fase: RispostaApprovata, Il: *approvato}
	case rifiutato != nil:
		return StatoRisposta{Fase: RispostaRifiutata, Il: *rifiutato, Motivo: motivo}
	case stato != statoInPreventivo:
		return StatoRisposta{}
	case !conImporto:
		return StatoRisposta{Fase: RispostaSenzaImporto}
	}
	return StatoRisposta{Fase: RispostaInAttesa}
}

// statoRispostaPubblico è public_state/1: fuori da «preventivo» la pagina del cliente non dice
// niente del preventivo, nemmeno di una risposta data.
func statoRispostaPubblico(stato string, approvato, rifiutato *time.Time, motivo *string, conImporto bool) StatoRisposta {
	if stato != statoInPreventivo {
		return StatoRisposta{}
	}
	return statoRisposta(stato, approvato, rifiutato, motivo, conImporto)
}

// EventoPreventivo è una riga della storia del preventivo.
type EventoPreventivo struct {
	ID     int64
	Evento string // approved, rejected, approval_revoked, rejection_revoked
	Parte  string // customer, oreficeria, shop
	Il     time.Time
}

// Etichetta è event_label/1.
func (e EventoPreventivo) Etichetta() string {
	switch e.Evento {
	case "approved":
		return "Preventivo approvato"
	case "rejected":
		return "Preventivo rifiutato"
	case "approval_revoked":
		return "Approvazione revocata"
	case "rejection_revoked":
		return "Rifiuto revocato"
	}
	return e.Evento
}

// NomeParte è party_label/1.
func (e EventoPreventivo) NomeParte() string {
	switch e.Parte {
	case "customer":
		return "cliente"
	case "oreficeria":
		return "oreficeria"
	case "shop":
		return "negozio"
	}
	return e.Parte
}

// StoriaPreventivo è history/1: la storia del preventivo della busta, dalla più vecchia.
func (b *Buste) StoriaPreventivo(ctx context.Context, id int64) ([]EventoPreventivo, error) {
	return storiaPreventivo(ctx, b.d.Lettura, id)
}

func storiaPreventivo(ctx context.Context, q interroga, id int64) ([]EventoPreventivo, error) {
	righe, err := q.QueryContext(ctx, `SELECT id, evento, parte, avvenuto_il FROM eventi_preventivo
		WHERE busta_id = ? ORDER BY avvenuto_il, id`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = righe.Close() }()
	var v []EventoPreventivo
	for righe.Next() {
		var e EventoPreventivo
		var il string
		if err := righe.Scan(&e.ID, &e.Evento, &e.Parte, &il); err != nil {
			return nil, err
		}
		if e.Il, err = time.Parse(db.FormatoIstante, il); err != nil {
			return nil, err
		}
		v = append(v, e)
	}
	return v, righe.Err()
}

// bustaDelToken è get_public_repair/1 ridotto a quello che la risposta e le foto guardano.
type bustaDelToken struct {
	id           int64
	oreficeriaID *int64
}

// bustaDelToken cerca la busta del link come PaginaCliente: impronta, scadenza dopo adesso, senza
// revoca, fuori dal cestino; ErrBustaNonTrovata altrimenti.
func (b *Buste) bustaDelToken(ctx context.Context, token string) (bustaDelToken, error) {
	if token == "" {
		return bustaDelToken{}, ErrBustaNonTrovata
	}
	_, ora := b.adesso()
	var t bustaDelToken
	var oref sql.NullInt64
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT id, oreficeria_id FROM buste WHERE hash_token_pubblico = ?
		AND token_pubblico_revocato_il IS NULL AND token_pubblico_scade_il > ? AND cancellata_il IS NULL`,
		ImprontaToken(token), ora).Scan(&t.id, &oref)
	if errors.Is(err, sql.ErrNoRows) {
		return bustaDelToken{}, ErrBustaNonTrovata
	}
	if err != nil {
		return bustaDelToken{}, err
	}
	t.oreficeriaID = nullaIntero(oref)
	return t, nil
}

// parte è Recipient.party/1: chi ha il link di una busta B2B è l'oreficeria.
func (t bustaDelToken) parte() string {
	if Classificazione(t.oreficeriaID) == ClasseEsterna {
		return "oreficeria"
	}
	return "customer"
}

// ApprovaPreventivo è approve/2 dalla pagina del link token: ErrBustaNonTrovata se il link non
// apre niente (o la busta è sparita nel frattempo), ErrGiaApprovato, ErrGiaRifiutato o
// ErrNonInPreventivo se l'UPDATE protetto non tocca niente.
func (b *Buste) ApprovaPreventivo(ctx context.Context, token string, c audit.Contesto) error {
	t, err := b.bustaDelToken(ctx, token)
	if err != nil {
		return err
	}
	return b.rispondi(ctx, t, "approved", nil, c)
}

// RifiutaPreventivo è reject/3 dalla pagina del link token, con il motivo scritto (anche vuoto):
// gli spazi ai bordi tolti, i primi 300 grafemi, vuoto è nessuno.
func (b *Buste) RifiutaPreventivo(ctx context.Context, token, motivo string, c audit.Contesto) error {
	t, err := b.bustaDelToken(ctx, token)
	if err != nil {
		return err
	}
	return b.rispondi(ctx, t, "rejected", motivoRifiuto(motivo), c)
}

// motivoRifiuto è trim_reason/1.
func motivoRifiuto(s string) *string {
	s = strings.TrimFunc(s, unicode.IsSpace)
	g := uniseg.NewGraphemes(s)
	fine := 0
	for i := 0; i < motivoRifiutoMassimo && g.Next(); i++ {
		_, fine = g.Positions()
	}
	if s = s[:fine]; s == "" {
		return nil
	}
	return &s
}

// rispondi è decide/5: l'UPDATE protetto, la riga della storia e l'audit in una transazione.
func (b *Buste) rispondi(ctx context.Context, t bustaDelToken, evento string, motivo *string, c audit.Contesto) error {
	_, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	set := `preventivo_accettato_il = ?, preventivo_accettato_letto_il = NULL, stato = '` + statoApprovato + `'`
	args := []any{ora}
	if evento == "rejected" {
		set, args = `preventivo_rifiutato_il = ?, preventivo_rifiutato_letto_il = NULL, preventivo_rifiuto_motivo = ?`, []any{ora, motivo}
	}
	r, err := tx.ExecContext(ctx, `UPDATE buste SET `+set+`, aggiornato_il = ? WHERE id = ? AND stato = '`+statoInPreventivo+`'
		AND preventivo_accettato_il IS NULL AND preventivo_rifiutato_il IS NULL AND cancellata_il IS NULL`,
		append(args, ora, t.id)...)
	if err != nil {
		return err
	}
	if n, err := r.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return motivoDelNo(ctx, tx, t.id)
	}
	parte := t.parte()
	if _, err := tx.ExecContext(ctx, `INSERT INTO eventi_preventivo (busta_id, evento, parte, motivo, attore, avvenuto_il,
		creato_il, aggiornato_il) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, t.id, evento, parte, motivo, attoreEvento(c), ora, ora, ora); err != nil {
		return err
	}
	var stato string
	if err := tx.QueryRowContext(ctx, `SELECT stato FROM buste WHERE id = ?`, t.id).Scan(&stato); err != nil {
		return err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "quote."+evento, "repair", t.id, "ok", map[string]any{
		"party": parte, "status": stato, "reason_given": motivo != nil}); err != nil {
		return err
	}
	return tx.Commit()
}

// motivoDelNo è claim_refusal/2: chiesto solo dopo che l'UPDATE non ha toccato niente; una
// risposta data si nomina anche se pure lo stato non va.
func motivoDelNo(ctx context.Context, tx *sql.Tx, id int64) error {
	var approvato, rifiutato sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT preventivo_accettato_il, preventivo_rifiutato_il FROM buste
		WHERE id = ? AND cancellata_il IS NULL`, id).Scan(&approvato, &rifiutato)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrBustaNonTrovata
	case err != nil:
		return err
	case approvato.Valid:
		return ErrGiaApprovato
	case rifiutato.Valid:
		return ErrGiaRifiutato
	}
	return ErrNonInPreventivo
}

// attoreEvento è actor/1: chi ha agito, o nessuno.
func attoreEvento(c audit.Contesto) any {
	if c.Attore == "" {
		return nil
	}
	return c.Attore
}

// RevocaRisposta è l'evento revoke-quote della scheda: risposta è quella che la pagina mostrava
// («approved» o «rejected», ErrRispostaNonValid altrimenti), mai una rilettura. ErrNonApprovato o
// ErrNonRifiutato se nel frattempo non c'è più (nessuna scrittura), ErrBustaNonTrovata per una
// busta che non c'è o sta nel cestino.
func (b *Buste) RevocaRisposta(ctx context.Context, id int64, risposta string, c audit.Contesto) error {
	var evento, segno string
	var assente error
	switch risposta {
	case "approved":
		evento, segno, assente = "approval_revoked", "preventivo_accettato_il", ErrNonApprovato
	case "rejected":
		evento, segno, assente = "rejection_revoked", "preventivo_rifiutato_il", ErrNonRifiutato
	default:
		return ErrRispostaNonValid
	}
	_, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// answered_at/2: l'istante della risposta revocata, per l'audit. Letto nella transazione di
	// scrittura, quindi è quello che l'UPDATE toglie.
	var risposta0 sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT `+segno+` FROM buste WHERE id = ? AND cancellata_il IS NULL`, id).Scan(&risposta0)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrBustaNonTrovata
	}
	if err != nil {
		return err
	}
	q := `UPDATE buste SET preventivo_accettato_il = NULL, preventivo_accettato_letto_il = NULL, aggiornato_il = ?
		WHERE id = ? AND preventivo_accettato_il IS NOT NULL AND cancellata_il IS NULL`
	args := []any{ora, id}
	if evento == "rejection_revoked" {
		// Una sola scrittura: la riapertura da «annullata» è un CASE dentro lo stesso UPDATE protetto.
		q = `UPDATE buste SET preventivo_rifiutato_il = NULL, preventivo_rifiutato_letto_il = NULL,
			preventivo_rifiuto_motivo = NULL, aggiornato_il = ?,
			conservazione_iniziata_il = CASE WHEN stato = '` + statoAnnullata + `' THEN NULL ELSE conservazione_iniziata_il END,
			stato = CASE WHEN stato = '` + statoAnnullata + `' THEN '` + statoInPreventivo + `' ELSE stato END
			WHERE id = ? AND preventivo_rifiutato_il IS NOT NULL AND cancellata_il IS NULL`
	}
	r, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, err := r.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return assente
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO eventi_preventivo (busta_id, evento, parte, motivo, attore, avvenuto_il,
		creato_il, aggiornato_il) VALUES (?, ?, 'shop', NULL, ?, ?, ?, ?)`, id, evento, attoreEvento(c), ora, ora, ora); err != nil {
		return err
	}
	var stato string
	if err := tx.QueryRowContext(ctx, `SELECT stato FROM buste WHERE id = ?`, id).Scan(&stato); err != nil {
		return err
	}
	var risposta1 any
	if risposta0.Valid {
		risposta1 = risposta0.String
	}
	if err := b.reg.Scrivi(ctx, tx, c, "quote."+evento, "repair", id, "ok", map[string]any{
		"status": stato, "answered_at": risposta1}); err != nil {
		return err
	}
	return tx.Commit()
}
