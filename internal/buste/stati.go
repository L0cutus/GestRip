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
}

// CambiaStato porta la busta id a stato, se il grafo della sua famiglia lo permette
// (transition_repair/4): legge la riga in transazione, mai la copia di chi chiama. Pronta scrive
// la data di pronta solo se vuota, consegnata la data di consegna solo se vuota (oggi in negozio);
// uno stato chiuso avvia la conservazione. Audit repair.status_changed con il nuovo stato; la
// versione la conta il trigger versioni_stato_buste_conta. Una busta che non c'è o sta nel
// cestino, uno stato dell'altra famiglia o fuori dal grafo: ErrTransizione e niente scritto.
func (b *Buste) CambiaStato(ctx context.Context, id int64, stato string, c audit.Contesto) (Cambio, error) {
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
	)
	err = tx.QueryRowContext(ctx, `SELECT stato, compro_oro, data_pronta, data_consegna FROM buste WHERE id = ? AND cancellata_il IS NULL`, id).
		Scan(&prima, &oro, &pronta, &conse)
	if errors.Is(err, sql.ErrNoRows) {
		return Cambio{}, ErrTransizione
	}
	if err != nil {
		return Cambio{}, err
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
	// Qui lo standalone chiude anche il lotto di avviso dopo una consegna
	// (ReadyLots.close_after_delivery/2): arriva con i lotti.
	if err := b.reg.Scrivi(ctx, tx, c, "repair.status_changed", "repair", id, "ok", map[string]any{"status": stato}); err != nil {
		return Cambio{}, err
	}
	cambio := Cambio{Prima: prima, Dopo: stato}
	if cambio.Versione, err = versione(ctx, tx, id); err != nil {
		return Cambio{}, err
	}
	if err := tx.Commit(); err != nil {
		return Cambio{}, err
	}
	// Dopo il commit, in transazioni loro e senza mai togliere il nuovo stato, lo standalone
	// accoda l'email di pronta quando la busta arriva a «pronta» da un altro stato
	// (announce_ready_by_email/4, T4.7) e la pagina apre il popup della notifica Telegram
	// (TelegramNotices.transition, T4.6). Chi chiama lo sa da cambio.Prima e cambio.Dopo.
	return cambio, nil
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
