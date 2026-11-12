package buste

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
)

// Gli stati della busta (RIP-S-13, RIP-H-38): due famiglie, riparazioni e compro oro, ognuna con
// il suo grafo (@transitions e @gold_transitions di workshop.ex). «annullata» è l'unico stato
// delle due. Il CHECK di buste lega lo stato al tipo; il tipo non cambia con lo stato.

var (
	statiRiparazioni = []string{"in_lavorazione", "preventivo", "pronta", "consegnata", "in_attesa_materiali", "annullata"}
	statiCompraOro   = []string{"in_valutazione", "valutata", "perfezionata", "chiusa", "annullata"}

	grafoRiparazioni = map[string][]string{
		"in_lavorazione":      {"preventivo", "pronta", "in_attesa_materiali", "annullata"},
		"preventivo":          {"in_lavorazione", "annullata"},
		"in_attesa_materiali": {"in_lavorazione", "preventivo", "pronta", "annullata"},
		"pronta":              {"in_lavorazione", "consegnata", "annullata"},
		"consegnata":          {},
		"annullata":           {},
	}
	grafoCompraOro = map[string][]string{
		"in_valutazione": {"valutata", "annullata"},
		"valutata":       {"in_valutazione", "perfezionata", "annullata"},
		"perfezionata":   {"chiusa", "annullata"},
		"chiusa":         {},
		"annullata":      {},
	}

	// statiChiusi sono dove una busta smette di muoversi (@closed_statuses): entrarci avvia
	// l'orologio della conservazione delle foto.
	statiChiusi = []string{"consegnata", "annullata", "chiusa"}
)

// Stati sono gli stati di una famiglia, nell'ordine dello standalone (Repair.statuses/1).
func Stati(compraOro bool) []string {
	if compraOro {
		return slices.Clone(statiCompraOro)
	}
	return slices.Clone(statiRiparazioni)
}

// TuttiGliStati sono gli stati delle due famiglie, ognuno una volta, nell'ordine di Repair.statuses/0:
// le riparazioni, poi i compro oro che non lo sono già («annullata» è di tutte e due).
func TuttiGliStati() []string {
	out := slices.Clone(statiRiparazioni)
	for _, s := range statiCompraOro {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// Consentiti sono gli stati raggiungibili da stato nella sua famiglia (allowed_transitions/1);
// nessuno per uno stato chiuso o di un'altra famiglia.
func Consentiti(stato string, compraOro bool) []string {
	g := grafoRiparazioni
	if compraOro {
		g = grafoCompraOro
	}
	return append([]string{}, g[stato]...)
}

// StatoIniziale è lo stato di una busta nuova (Repair.default_status/1).
func StatoIniziale(compraOro bool) string {
	if compraOro {
		return "in_valutazione"
	}
	return "in_lavorazione"
}

// CompraOroDalModulo è compro_oro?/1 per un valore del modulo: «sì» solo "1", "on" e "true"
// (lo standalone accetta anche il booleano e l'intero 1 da chi lo chiama nel codice); tutto il
// resto, anche il campo assente, è una riparazione.
func CompraOroDalModulo(valore string) bool {
	return valore == "1" || valore == "on" || valore == "true"
}

// Chiuso dice se lo stato chiude la busta.
func Chiuso(stato string) bool { return slices.Contains(statiChiusi, stato) }

// Cambio è un cambio di stato riuscito: da dove, a dove, e la versione dopo il cambio (quante
// volte è cambiato lo stato della busta, per il popup Telegram della T4.6).
type Cambio struct {
	Prima, Dopo string
	Versione    int
	Rientrata   bool // il pannello del QR ha segnato anche il rientro dall'artigiano (RIP-H-24)
}

// CambiaStato porta la busta id a stato, se il grafo della sua famiglia lo permette
// (transition_repair/4): legge la riga in transazione, mai la copia di chi chiama. Pronta scrive
// la data di pronta solo se vuota, consegnata la data di consegna solo se vuota (oggi in negozio);
// uno stato chiuso avvia la conservazione; una consegna chiude il lotto d'avviso della busta, se
// era l'ultima da ritirare (chiudiLottoCompleto). Audit repair.status_changed con il nuovo stato; la
// versione la conta il trigger versioni_stato_buste_conta. Una busta che non c'è o sta nel
// cestino, uno stato dell'altra famiglia o fuori dal grafo: ErrTransizione e niente scritto.
func (b *Buste) CambiaStato(ctx context.Context, id int64, stato string, c audit.Contesto) (Cambio, error) {
	cambio, err := b.passaStato(ctx, id, c, passaggio{scegli: func(string, bool) (string, error) {
		if stato == "" { // "" in passaggio vuol dire «lascia com'è»: qui è uno stato che non esiste
			return "", ErrTransizione
		}
		return stato, nil
	}})
	if errors.Is(err, ErrBustaNonTrovata) {
		return Cambio{}, ErrTransizione
	}
	return cambio, err
}

// passaggio è quello che distingue un cambio di stato dall'altro dentro passaStato (run_transition/4
// e transact_transition/5 dello standalone): scegli decide lo stato dalla riga riletta in transazione
// (prima, compro oro), "" per lasciarla com'è (:unchanged, senza scrivere né audit) o un errore;
// metadati va nell'audit accanto allo stato («via»: «qr» per il pannello); rientro è l'opzione
// :auto_return del pannello QR (rientroAutomatico).
type passaggio struct {
	scegli   func(prima string, oro bool) (string, error)
	metadati map[string]any
	rientro  bool
}

// passaStato è l'unico cambio di stato di una busta, per la scheda (CambiaStato) e per il pannello
// del QR (AvanzaDaQR, PreventivoDaQR). Una busta che non c'è o sta nel cestino: ErrBustaNonTrovata.
func (b *Buste) passaStato(ctx context.Context, id int64, c audit.Contesto, p passaggio) (Cambio, error) {
	t, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return Cambio{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var (
		prima         string
		oro           bool
		pronta, conse sql.NullString
		lotto         sql.NullInt64
	)
	err = tx.QueryRowContext(ctx, `SELECT stato, compro_oro, data_pronta, data_consegna, lotto_avviso_id FROM buste
		WHERE id = ? AND cancellata_il IS NULL`, id).Scan(&prima, &oro, &pronta, &conse, &lotto)
	if errors.Is(err, sql.ErrNoRows) {
		return Cambio{}, ErrBustaNonTrovata
	}
	if err != nil {
		return Cambio{}, err
	}
	stato, err := p.scegli(prima, oro)
	if err != nil {
		return Cambio{}, err
	}
	if stato == "" {
		return Cambio{Prima: prima, Dopo: prima}, nil
	}
	if !slices.Contains(Stati(oro), stato) || !slices.Contains(Consentiti(prima, oro), stato) {
		return Cambio{}, ErrTransizione
	}
	q, args := `UPDATE buste SET stato = ?, aggiornato_il = ?`, []any{stato, ora}
	oggi := clock.DataDi(t).String()
	if stato == "pronta" && !pronta.Valid {
		q, args = q+`, data_pronta = ?`, append(args, oggi)
	}
	if stato == "consegnata" && !conse.Valid {
		q, args = q+`, data_consegna = ?`, append(args, oggi)
	}
	if Chiuso(stato) {
		q, args = q+`, conservazione_iniziata_il = ?`, append(args, ora)
	}
	if _, err := tx.ExecContext(ctx, q+` WHERE id = ?`, append(args, id)...); err != nil {
		return Cambio{}, err
	}
	if stato == "consegnata" && lotto.Valid {
		if err := chiudiLottoCompleto(ctx, tx, lotto.Int64, ora); err != nil {
			return Cambio{}, err
		}
	}
	meta := map[string]any{"status": stato}
	for k, v := range p.metadati {
		meta[k] = v
	}
	if err := b.reg.Scrivi(ctx, tx, c, "repair.status_changed", "repair", id, "ok", meta); err != nil {
		return Cambio{}, err
	}
	cambio := Cambio{Prima: prima, Dopo: stato}
	if p.rientro {
		if cambio.Rientrata, err = b.rientroAutomatico(ctx, tx, id, stato, ora, c, p.metadati); err != nil {
			return Cambio{}, err
		}
	}
	// L'email di pronta quando la busta arriva a «pronta» da un altro stato (announce_ready_by_email/4,
	// CLI-42, slice 070b), dalla scheda, dalla lista e dal pannello del QR («✅ Pronta», AvanzaDaQR):
	// lo standalone la accoda dopo il commit in una transazione sua; qui nella stessa transazione, dentro
	// un SAVEPOINT, e un'email che non parte non toglie mai il nuovo stato (avvisaPronta).
	if prima != "pronta" && stato == "pronta" {
		if err := b.avvisaPronta(ctx, tx, id, c); err != nil {
			return Cambio{}, err
		}
	}
	if cambio.Versione, err = versione(ctx, tx, id); err != nil {
		return Cambio{}, err
	}
	if err := b.provaPrimaDelCommit(tx); err != nil {
		return Cambio{}, err
	}
	if err := tx.Commit(); err != nil {
		return Cambio{}, err
	}
	// Dopo il commit la scheda apre il popup della notifica Telegram (TelegramNotices.transition, T4.7;
	// il pannello del QR no): chi chiama lo sa da cambio.Prima e cambio.Dopo.
	return cambio, nil
}

// provaPrimaDelCommit è primaDelCommit, solo nei test; nil: niente.
func (b *Buste) provaPrimaDelCommit(tx *sql.Tx) error {
	if b.primaDelCommit == nil {
		return nil
	}
	return b.primaDelCommit(tx)
}

// Versione è quante volte è cambiato lo stato della busta (status_version/1); 0 se mai.
func (b *Buste) Versione(ctx context.Context, id int64) (int, error) {
	return versione(ctx, b.d.Lettura, id)
}

type lettore interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

func versione(ctx context.Context, q lettore, id int64) (int, error) {
	var v int
	err := q.QueryRowContext(ctx, `SELECT versione FROM versioni_stato_buste WHERE busta_id = ?`, id).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return v, err
}

// chiudiLottoCompleto è ReadyLots.close_after_delivery/2 (slice 212 dello standalone) con la regola
// di close_if_complete/2: dentro la transazione della consegna e con il suo istante, il lotto si
// chiude una volta sola, se è stato inviato, non è vuoto e tutte le sue buste sono consegnate. Le
// buste nel cestino contano: una cestinata non ritirata lo tiene aperto. Nessun audit.
func chiudiLottoCompleto(ctx context.Context, tx *sql.Tx, lotto int64, ora string) error {
	_, err := tx.ExecContext(ctx, `UPDATE lotti_avviso SET chiuso_il = ?1, aggiornato_il = ?1
		WHERE id = ?2 AND inviato_il IS NOT NULL AND chiuso_il IS NULL
		AND EXISTS (SELECT 1 FROM buste WHERE lotto_avviso_id = ?2)
		AND NOT EXISTS (SELECT 1 FROM buste WHERE lotto_avviso_id = ?2 AND stato != 'consegnata')`, ora, lotto)
	return err
}

// PassiRapidi sono i pulsanti rapidi del riquadro della lista (RIP-H-14, @quick_steps di
// workshop_live.ex, slice 198 dello standalone): lo stato d'arrivo e il solo stato da cui parte.
// Niente pulsanti per preventivi, attesa materiali e compro oro: nessuno di quegli stati è una partenza.
var PassiRapidi = map[string]string{"pronta": "in_lavorazione", "consegnata": "pronta"}

// PassoRapido è «✓ Pronta» o «✓ Consegnata» del riquadro (quick-status): un passo solo, dallo stato
// di partenza di PassiRapidi, con lo stesso cambio di stato della scheda e del menu (passaStato). Lo
// stato di partenza si guarda sulla riga riletta in transazione: una busta cambiata nel frattempo
// (anche in uno stato da cui il grafo permetterebbe lo stesso passo, come «in attesa materiali» →
// «pronta») è rifiutata. Uno stato d'arrivo che non è un passo rapido, una busta che non c'è o sta
// nel cestino: ErrTransizione, come lo standalone, e niente scritto.
func (b *Buste) PassoRapido(ctx context.Context, id int64, stato string, c audit.Contesto) (Cambio, error) {
	partenza, ok := PassiRapidi[stato]
	if !ok {
		return Cambio{}, ErrTransizione
	}
	cambio, err := b.passaStato(ctx, id, c, passaggio{scegli: func(prima string, oro bool) (string, error) {
		if oro || prima != partenza {
			return "", ErrTransizione
		}
		return stato, nil
	}})
	if errors.Is(err, ErrBustaNonTrovata) {
		return Cambio{}, ErrTransizione
	}
	return cambio, err
}

// CambiaStatoDallaLista è il menu dello stato del riquadro della lista (RIP-H-15, l'evento transition
// di WorkshopLive): CambiaStato, ma lo stato che la busta ha già (riletto in transazione) non è un
// rifiuto: niente scritto, niente audit, e il Cambio ha Prima uguale a Dopo (slice 185 dello
// standalone: un modulo rimandato da un telefono che si riconnette non è un passo rifiutato).
func (b *Buste) CambiaStatoDallaLista(ctx context.Context, id int64, stato string, c audit.Contesto) (Cambio, error) {
	cambio, err := b.passaStato(ctx, id, c, passaggio{scegli: func(prima string, _ bool) (string, error) {
		switch stato {
		case "":
			return "", ErrTransizione
		case prima:
			return "", nil
		}
		return stato, nil
	}})
	if errors.Is(err, ErrBustaNonTrovata) {
		return Cambio{}, ErrTransizione
	}
	return cambio, err
}
