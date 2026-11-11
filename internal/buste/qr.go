package buste

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/L0cutus/gestrip/internal/audit"
)

// Il pannello delle azioni dopo una scansione (slice 054; RIP-H-23, RIP-H-24, RIP-H-25, RIP-H-26),
// come lo standalone a go-baseline: la tabella @qr_actions di WorkshopLive (TRANSIZIONI_RIP e
// TRANSIZIONI_CO della sorgente), qr_select/2 (l'avviso delle buste che non si avanzano e
// «💰 Preventivo»), qr_confirm_text/2 (la frase del rientro) e Workshop.qr_advance_repair/3,
// qr_set_estimate/3, qr_next_status/1 e qr_auto_return/4. I due cambi passano da passaStato, lo
// stesso della scheda: grafo, date, conservazione, lotto e audit sono quelli di CambiaStato, con
// «via»: «qr» nei metadati.

// DopoQR è cosa segue un avanzamento riuscito (il terzo elemento della riga di @qr_actions).
type DopoQR string

const (
	// DopoPrezzo è :edit_price: la scheda con il modulo aperto sul prezzo finale (?edit=prezzo).
	DopoPrezzo DopoQR = "prezzo"
	// DopoLista è :reload: «✓ <nuovo stato>» e la lista, con i filtri di prima.
	DopoLista DopoQR = "lista"
	// DopoAvviso è :whatsapp, la valutazione del compro oro: l'avviso di ritiro su WhatsApp, offerto
	// come link (qr_announce/2, AnnunciaRitiro; slice 068).
	DopoAvviso DopoQR = "avviso"
)

// AzioneQR è il pulsante #qr-btn-avanza del pannello: il testo, la frase della conferma (vuota:
// nessuna conferma) e cosa segue.
type AzioneQR struct {
	Etichetta string
	Conferma  string
	Dopo      DopoQR
}

type chiaveAzioneQR struct {
	oro   bool
	stato string
}

// azioniQR è @qr_actions.
var azioniQR = map[chiaveAzioneQR]AzioneQR{
	{false, "in_lavorazione"}:      {"✅ Pronta", "", DopoPrezzo},
	{false, "in_attesa_materiali"}: {"✅ Pronta", "", DopoPrezzo},
	{false, "preventivo"}:          {"▶ In lavorazione", "", DopoLista},
	{false, "pronta"}:              {"✅ Consegnata", "Marchio come consegnata? Verrà impostata data oggi.", DopoLista},
	{true, "in_valutazione"}:       {"✅ Valutata", `Conferma valutazione completata? Verrà aperto WA "scheda pronta firma".`, DopoAvviso},
	{true, "valutata"}:             {"✅ Perfezionata", "Confermi firma scheda? Movimento di cassa registrato.", DopoLista},
	{true, "perfezionata"}:         {"✅ Chiusa", "", DopoLista},
}

// confermaRientro è @qr_auto_return_confirm: si aggiunge alla frase della riga (o è la sola frase)
// quando l'avanzamento riporta anche la busta dall'artigiano.
const confermaRientro = "La busta risulta fuori bottega: viene segnata anche rientrata dall'artigiano."

// Azione è il pulsante del pannello per la busta, con la conferma del rientro (qr_confirm_text/2);
// false se lo stato non ha un'azione (le buste chiuse).
func (r RigaQR) Azione() (AzioneQR, bool) {
	a, ok := azioniQR[chiaveAzioneQR{r.CompraOro, r.Stato}]
	if !ok {
		return AzioneQR{}, false
	}
	if r.RientroAutomatico() {
		a.Conferma = strings.TrimSpace(a.Conferma + " " + confermaRientro)
	}
	return a, true
}

// RientroAutomatico è qr_auto_return?/1: la busta è ancora dall'artigiano e il prossimo stato è
// «pronta» o «consegnata» (app_qr_bp.py:126 della sorgente).
func (r RigaQR) RientroAutomatico() bool {
	dopo, err := prossimoDaQR(r.Stato, r.CompraOro)
	return r.FuoriBottega && err == nil && slices.Contains(statiDelRientro, dopo)
}

// AvvisoDelPannello è il riquadro #qr-action-notice di qr_select/2: «Stato X — non avanzabile.» per
// una busta chiusa, «Stato non avanzabile.» per uno stato senza azione; "" se si può avanzare.
func (r RigaQR) AvvisoDelPannello() string {
	if Chiuso(r.Stato) {
		return "Stato " + ParolaDelloStato(r.Stato) + " — non avanzabile."
	}
	if _, ok := r.Azione(); !ok {
		return "Stato non avanzabile."
	}
	return ""
}

// ConPreventivo dice se il pannello offre «💰 Preventivo» (qr_estimate?): solo una riparazione fuori
// da preventivo, pronta e dagli stati chiusi.
func (r RigaQR) ConPreventivo() bool {
	return !r.CompraOro && !slices.Contains([]string{"preventivo", "pronta", "consegnata", "annullata"}, r.Stato)
}

// ParolaDelloStato è source_word/1: lo stato con gli spazi della sorgente («in lavorazione»).
func ParolaDelloStato(stato string) string { return strings.ReplaceAll(stato, "_", " ") }

// I rifiuti del pannello, con le parole di qr_error/1.
type (
	// ErrNonAvanzabile è {:not_advanceable, status}: una busta chiusa.
	ErrNonAvanzabile struct{ Stato string }
	// ErrOroNonGestito è {:unhandled_gold, status}: uno stato del compro oro senza seguito.
	ErrOroNonGestito struct{ Stato string }
	// ErrStatoCambiato è {:stale, current}: la busta non ha più lo stato che il pannello mostrava.
	ErrStatoCambiato struct{ Attuale string }
)

func (e ErrNonAvanzabile) Error() string {
	return `Stato "` + ParolaDelloStato(e.Stato) + `" non avanzabile`
}

func (e ErrOroNonGestito) Error() string {
	return `Stato CO "` + ParolaDelloStato(e.Stato) + `" non gestito`
}

func (e ErrStatoCambiato) Error() string { return "Stato cambiato nel frattempo: ricontrolla." }

// ErrPreventivoOro è {:error, :compro_oro} di qr_set_estimate/3.
const ErrPreventivoOro Rifiuto = "QR preventivo non supportato per buste compro oro"

// statiDelRientro sono @qr_returning_statuses.
var statiDelRientro = []string{"pronta", "consegnata"}

// prossimoDaQR è qr_next_status/1.
func prossimoDaQR(stato string, oro bool) (string, error) {
	switch {
	case Chiuso(stato):
		return "", ErrNonAvanzabile{stato}
	case oro:
		switch stato {
		case "in_valutazione":
			return "valutata", nil
		case "valutata":
			return "perfezionata", nil
		case "perfezionata":
			return "chiusa", nil
		}
		return "", ErrOroNonGestito{stato}
	case stato == "pronta":
		return "consegnata", nil
	case stato == "preventivo":
		return "in_lavorazione", nil
	}
	return "pronta", nil
}

// giaFatto dice se la busta è già nello stato che l'azione del pannello mostrata per statoVisto
// avrebbe prodotto: un secondo invio dello stesso modulo (doppio tocco, review della 054, punto M1).
// Solo per uno stato vero della famiglia: un modulo forgiato senza stato non vale.
func giaFatto(statoVisto, attuale string, oro bool) bool {
	if !slices.Contains(Stati(oro), statoVisto) {
		return false
	}
	dopo, err := prossimoDaQR(statoVisto, oro)
	return err == nil && dopo == attuale
}

// AzioneDiStato è il pulsante del pannello per uno stato mostrato (la riga di @qr_actions), senza la
// frase del rientro: dice cosa segue un'azione riuscita anche quando la busta è già andata avanti.
func AzioneDiStato(statoVisto string, oro bool) (AzioneQR, bool) {
	a, ok := azioniQR[chiaveAzioneQR{oro, statoVisto}]
	return a, ok
}

// metadatiQR sono i metadati in più delle righe d'audit del pannello (qr_audit/1).
var metadatiQR = map[string]any{"via": "qr"}

// AvanzaDaQR è Workshop.qr_advance_repair/3 con :expected_status: la busta, riletta in transazione,
// deve avere ancora statoVisto (lo stato che il pannello mostrava), altrimenti ErrStatoCambiato e
// niente scritto; una busta chiusa nel frattempo dà prima ErrNonAvanzabile. Una busta già nello
// stato che l'azione avrebbe prodotto (il secondo invio di un doppio tocco) è riuscita senza
// scrivere niente: Cambio con Prima e Dopo uguali, nessun rientro (è già avvenuto o no al primo). Il prossimo stato è
// prossimoDaQR, poi tutto come CambiaStato (grafo, date solo se vuote, conservazione, lotto) con
// l'audit repair.status_changed e «via»: «qr». Una busta ancora dall'artigiano che diventa pronta o
// consegnata rientra nella stessa transazione (rientroAutomatico, RIP-H-24). Una busta che non c'è
// o sta nel cestino: ErrBustaNonTrovata.
func (b *Buste) AvanzaDaQR(ctx context.Context, id int64, statoVisto string, c audit.Contesto) (Cambio, error) {
	return b.passaStato(ctx, id, c, passaggio{
		scegli: func(prima string, oro bool) (string, error) {
			if prima != statoVisto && giaFatto(statoVisto, prima, oro) {
				return "", nil // un secondo invio della stessa azione: com'è, riuscita
			}
			if prima != statoVisto && !Chiuso(prima) {
				return "", ErrStatoCambiato{prima}
			}
			return prossimoDaQR(prima, oro)
		},
		metadati: metadatiQR,
		rientro:  true,
	})
}

// PreventivoDaQR è Workshop.qr_set_estimate/3: la busta va in «preventivo», con l'audit e «via»:
// «qr»; una già in preventivo resta com'è, senza audit; un compro oro è ErrPreventivoOro. Il grafo
// vale anche qui: da pronta o da una busta chiusa ErrTransizione.
func (b *Buste) PreventivoDaQR(ctx context.Context, id int64, c audit.Contesto) (Cambio, error) {
	return b.passaStato(ctx, id, c, passaggio{
		scegli: func(prima string, oro bool) (string, error) {
			switch {
			case oro:
				return "", ErrPreventivoOro
			case prima == "preventivo":
				return "", nil
			}
			return "preventivo", nil
		},
		metadati: metadatiQR,
	})
}

// RigaPerQR rilegge una busta come la mostra il pannello, fuori dal cestino: dopo un'azione rifiutata
// il pannello si ridisegna con la busta com'è adesso. ErrBustaNonTrovata se non c'è.
func (b *Buste) RigaPerQR(ctx context.Context, id int64) (RigaQR, error) {
	r, err := scandisciRigaQR(b.d.Lettura.QueryRowContext(ctx, `SELECT `+colonneRigaQR+` FROM buste b
		WHERE b.id = ? AND b.cancellata_il IS NULL`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return RigaQR{}, ErrBustaNonTrovata
	}
	return r, err
}

// rientroAutomatico è qr_auto_return/4 e audit_auto_return/4, dentro la transazione del cambio: se
// il nuovo stato è pronta o consegnata, l'UPDATE con la condizione di fuori bottega (l'unica
// autorità, come return_write/4: due rientri insieme non segnano due volte) scrive solo l'istante del
// rientro (né esito né note, come l'UPDATE automatico della sorgente). Se ha segnato qualcosa, la
// consegna all'artigiano si chiude quando non ha più buste fuori (close_handover_if_complete/3) e
// la riga repair.returned_from_artisan_auto dice numero, consegna e chiusura. Dice se ha segnato.
func (b *Buste) rientroAutomatico(ctx context.Context, tx *sql.Tx, id int64, stato, ora string, c audit.Contesto, meta map[string]any) (bool, error) {
	if !slices.Contains(statiDelRientro, stato) {
		return false, nil
	}
	res, err := tx.ExecContext(ctx, `UPDATE buste SET rientrata_artigiano_il = ?1, aggiornato_il = ?1
		WHERE id = ?2 AND consegnata_artigiano_il IS NOT NULL AND rientrata_artigiano_il IS NULL`, ora, id)
	if err != nil {
		return false, err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return false, err
	}
	var (
		numero   string
		consegna sql.NullInt64
	)
	if err := tx.QueryRowContext(ctx, `SELECT numero, consegna_artigiano_id FROM buste WHERE id = ?`, id).Scan(&numero, &consegna); err != nil {
		return false, err
	}
	chiusa := false
	if consegna.Valid {
		res, err := tx.ExecContext(ctx, `UPDATE consegne_artigiani SET chiusa_il = ?1, aggiornato_il = ?1
			WHERE id = ?2 AND chiusa_il IS NULL AND NOT EXISTS (SELECT 1 FROM buste b
			WHERE b.consegna_artigiano_id = ?2 AND `+fuoriBottegaSQL+`)`, ora, consegna.Int64)
		if err != nil {
			return false, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return false, err
		}
		chiusa = n > 0
	}
	dati := map[string]any{"bag_number": numero, "handover_id": nil, "handover_closed": chiusa}
	if consegna.Valid {
		dati["handover_id"] = consegna.Int64
	}
	for k, v := range meta {
		dati[k] = v
	}
	if err := b.reg.Scrivi(ctx, tx, c, "repair.returned_from_artisan_auto", "repair", id, "ok", dati); err != nil {
		return false, err
	}
	return true, nil
}
