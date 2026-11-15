package buste

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
)

// Il rientro di una busta dall'artigiano dalla scheda (slice 080, ART-13, RIP-S-16), come
// Workshop.return_from_artisan/4, applicable_outcomes/1 e return_error_message/1 dello standalone
// (302439d): l'atto intero in una transazione, timbro del rientro, nota dell'artigiano, esito del lavoro,
// chiusura della consegna se era l'ultima busta fuori e una riga repair.returned_from_artisan. Quando
// l'esito porta la busta a «pronta» da un altro stato, l'email di pronta parte nella stessa transazione
// (avvisaPronta, sezione 070b di SVILUPPO): lo standalone la accoda dopo il commit
// (announce_ready_by_email(:returned, …), workshop.ex:509-510); nel Go è la scelta della 070b, un'email che
// non parte non toglie mai il rientro. Nessun controllo di ruolo (titolare e dipendente).

// Gli esiti del lavoro del modulo di rientro («Stato lavoro»): "" è «Nessun cambio di stato».
const (
	EsitoFatto    = "fatto"     // → «pronta», con la data di pronta se vuota
	EsitoDaRifare = "da_rifare" // → «in lavorazione»
)

// I rifiuti del rientro, con i testi di return_error_message/1.
const (
	ErrNonFuoriBottega Rifiuto = "La riparazione non risulta fuori bottega."
	ErrEsitoNonValido  Rifiuto = "Esito lavoro non valido."
)

// EsitoNonConsentito è {:outcome_not_allowed, current, target}: il grafo della famiglia non ha il passaggio
// dallo stato della busta a quello dell'esito; tutto il rientro è rifiutato.
type EsitoNonConsentito struct{ Da, A string }

func (e *EsitoNonConsentito) Error() string {
	return "Da «" + ParolaDelloStato(e.Da) + "» non si passa a «" + ParolaDelloStato(e.A) + "»: scegli un altro esito lavoro."
}

// Rientro è un rientro riuscito: lo stato prima e dopo, la consegna della busta (nil per una busta importata
// uscita senza consegna) e se il rientro l'ha chiusa.
type Rientro struct {
	Prima, Dopo    string
	Consegna       *int64
	ConsegnaChiusa bool
}

// statoDellEsito è outcome_status/1.
func statoDellEsito(esito string) string {
	if esito == EsitoFatto {
		return "pronta"
	}
	return "in_lavorazione"
}

// EsitiApplicabili è applicable_outcomes/1: gli esiti che la busta può prendere, nell'ordine del modulo, con
// quello vuoto sempre per ultimo. Lo stesso stato non è un passaggio (un «fatto» su una busta già pronta).
func EsitiApplicabili(stato string, oro bool) []string {
	var out []string
	for _, esito := range []string{EsitoFatto, EsitoDaRifare} {
		if s := statoDellEsito(esito); s == stato || slices.Contains(Consentiti(stato, oro), s) {
			out = append(out, esito)
		}
	}
	return append(out, "")
}

// RientraDallArtigiano è return_from_artisan/4. L'esito (senza spazi ai bordi) deve essere "fatto",
// "da_rifare" o "": un'altra parola è ErrEsitoNonValido prima di ogni scrittura. La nota senza spazi ai bordi
// sostituisce quella del giro, e una vuota la cancella. Il timbro è un UPDATE con la condizione di fuori
// bottega (consegnata e non rientrata, fuori dal cestino), l'unica autorità come return_write/4: due rientri
// insieme non timbrano due volte. Se non timbra niente: ErrBustaNonTrovata per una busta che non c'è o sta
// nel cestino, ErrNonFuoriBottega altrimenti. Poi l'esito: lo stesso stato scrive solo la data di pronta
// vuota (un «fatto» su una busta già pronta), uno stato che il grafo non permette è *EsitoNonConsentito e
// tutto torna indietro. Uscita e consegna restano come sono (il giro finito resta sulla busta). Nessuna
// repair.status_changed, come lo standalone: la riga è una sola.
func (b *Buste) RientraDallArtigiano(ctx context.Context, id int64, esito, nota string, c audit.Contesto) (Rientro, error) {
	esito = strings.TrimSpace(esito)
	if esito != EsitoFatto && esito != EsitoDaRifare && esito != "" {
		return Rientro{}, ErrEsitoNonValido
	}
	var note any
	if n := strings.TrimSpace(nota); n != "" {
		note = n
	}
	t, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return Rientro{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	res, err := tx.ExecContext(ctx, `UPDATE buste SET rientrata_artigiano_il = ?1, note_artigiano = ?2, aggiornato_il = ?1
		WHERE id = ?3 AND cancellata_il IS NULL AND consegnata_artigiano_il IS NOT NULL AND rientrata_artigiano_il IS NULL`, ora, note, id)
	if err != nil {
		return Rientro{}, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return Rientro{}, err
	} else if n == 0 {
		return Rientro{}, rifiutoDelRientro(ctx, tx, id)
	}
	var (
		r        Rientro
		oro      bool
		pronta   sql.NullString
		consegna sql.NullInt64
	)
	if err := tx.QueryRowContext(ctx, `SELECT stato, compro_oro, data_pronta, consegna_artigiano_id FROM buste WHERE id = ?`, id).
		Scan(&r.Prima, &oro, &pronta, &consegna); err != nil {
		return Rientro{}, err
	}
	r.Dopo = r.Prima
	if esito != "" {
		verso := statoDellEsito(esito)
		if verso != r.Prima && !slices.Contains(Consentiti(r.Prima, oro), verso) {
			return Rientro{}, &EsitoNonConsentito{Da: r.Prima, A: verso}
		}
		// milestone_changes/3 per «pronta» e «in lavorazione»: lo stato e, per «pronta», la data se vuota.
		q, args := `UPDATE buste SET stato = ?`, []any{verso}
		if verso == "pronta" && !pronta.Valid {
			q, args = q+`, data_pronta = ?`, append(args, clock.DataDi(t).String())
		}
		if _, err := tx.ExecContext(ctx, q+` WHERE id = ?`, append(args, id)...); err != nil {
			return Rientro{}, err
		}
		r.Dopo = verso
	}
	if consegna.Valid {
		r.Consegna = &consegna.Int64
		if r.ConsegnaChiusa, err = chiudiConsegnaSeCompleta(ctx, tx, consegna.Int64, ora); err != nil {
			return Rientro{}, err
		}
	}
	dati := map[string]any{"handover_id": nil, "handover_closed": r.ConsegnaChiusa, "outcome": esito, "status": r.Dopo,
		"artisan_notes": note != nil}
	if r.Consegna != nil {
		dati["handover_id"] = *r.Consegna
	}
	evento, err := b.reg.ScriviConID(ctx, tx, c, "repair.returned_from_artisan", "repair", id, "ok", dati)
	if err != nil {
		return Rientro{}, err
	}
	if err := rientroNeiGiri(ctx, tx, evento, id); err != nil { // il giro chiuso (081)
		return Rientro{}, err
	}
	// L'email di pronta solo se la busta non era già «pronta» prima del rientro (announce_ready_by_email/4).
	if r.Prima != "pronta" && r.Dopo == "pronta" {
		if err := b.avvisaPronta(ctx, tx, id, c); err != nil {
			return Rientro{}, err
		}
	}
	if err := b.provaPrimaDelCommit(tx); err != nil {
		return Rientro{}, err
	}
	if err := tx.Commit(); err != nil {
		return Rientro{}, err
	}
	return r, nil
}

// rifiutoDelRientro è return_refusal/2: perché il timbro non ha trovato la busta.
func rifiutoDelRientro(ctx context.Context, tx *sql.Tx, id int64) error {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM buste WHERE id = ? AND cancellata_il IS NULL`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrBustaNonTrovata
	}
	return ErrNonFuoriBottega
}

// chiudiConsegnaSeCompleta è close_handover_if_complete/3 (il _chiudi_consegna_se_completa della sorgente),
// per il rientro dalla scheda e per quello automatico dal QR: la consegna si chiude quando nessuna sua busta
// è ancora fuori bottega. Una busta aggiunta e mai confermata non la tiene aperta, come il conto della
// sorgente; nemmeno una nel cestino, come lo standalone (Repo.prepare_query/3 sull'exists?). Una consegna già
// chiusa tiene la sua prima chiusura. Dice se l'ha chiusa.
func chiudiConsegnaSeCompleta(ctx context.Context, tx *sql.Tx, consegna int64, ora string) (bool, error) {
	res, err := tx.ExecContext(ctx, `UPDATE consegne_artigiani SET chiusa_il = ?1, aggiornato_il = ?1
		WHERE id = ?2 AND chiusa_il IS NULL AND NOT EXISTS (SELECT 1 FROM buste b
		WHERE b.consegna_artigiano_id = ?2 AND b.cancellata_il IS NULL AND `+fuoriBottegaSQL+`)`, ora, consegna)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ErroreDelRientro dice se err è un rifiuto del rientro da mostrare com'è (il testo di return_error_message/1).
func ErroreDelRientro(err error) bool {
	var vietato *EsitoNonConsentito
	return errors.Is(err, ErrNonFuoriBottega) || errors.Is(err, ErrEsitoNonValido) || errors.As(err, &vietato)
}
