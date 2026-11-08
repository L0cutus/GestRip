package buste

import (
	"context"
	"database/sql"
	"errors"

	"github.com/L0cutus/gestrip/internal/audit"
)

// CambiaFissata è toggle_repair_pin/3 dello standalone (RIP-H-16, il 📌 del riquadro della lista):
// una busta non fissata si fissa in cima adesso, una fissata si toglie. Legge e scrive nella stessa
// transazione; riga d'audit repair.pinned o repair.unpinned, senza dati. Dice se la busta ora è
// fissata. Una busta che non c'è o sta nel cestino: ErrBustaNonTrovata e niente scritto, come lo
// standalone (Workshop.get_repair e repo.get passano da Repo.prepare_query, che toglie le righe con
// deleted_at senza with_discarded: true).
func (b *Buste) CambiaFissata(ctx context.Context, id int64, c audit.Contesto) (bool, error) {
	_, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var fissata sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT fissata_il FROM buste WHERE id = ? AND cancellata_il IS NULL`, id).Scan(&fissata)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrBustaNonTrovata
	}
	if err != nil {
		return false, err
	}
	var nuova any
	azione := "repair.unpinned"
	if !fissata.Valid {
		nuova, azione = ora, "repair.pinned"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE buste SET fissata_il = ?, aggiornato_il = ? WHERE id = ?`, nuova, ora, id); err != nil {
		return false, err
	}
	if err := b.reg.Scrivi(ctx, tx, c, azione, "repair", id, "ok", nil); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return !fissata.Valid, nil
}
