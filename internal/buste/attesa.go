package buste

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
)

// L'attesa della risposta del cliente (RIP-S-15), come Workshop.set_repair_waiting/4 dello
// standalone (go-baseline): indipendente dallo stato, scrive solo attesa_cliente, attesa_dal e
// attesa_motivo, e il motivo (che è del cliente) non va nell'audit.

// ErrAzioneNonValida: un'azione diversa da «on» e «off».
const ErrAzioneNonValida Rifiuto = "Azione non valida."

// Attesa mette («on») o toglie («off») la busta id in attesa della risposta del cliente.
// L'azione vale senza spazi ai bordi e senza maiuscole; il motivo si toglie dagli spazi e vuoto
// vale nessuno; «dal» è l'ora del negozio in «AAAA-MM-GG HH:MM:SS» (Repair.local_now/0). Togliere
// l'attesa svuota i tre campi. Le colonne si scrivono solo se cambiano (un changeset senza cambi
// non tocca la riga, aggiornato_il compreso), l'audit repair.waiting_started o
// repair.waiting_ended si scrive sempre, senza dati. Dà se la busta è in attesa dopo.
// ErrAzioneNonValida senza leggere la busta; ErrBustaNonTrovata per una busta che non c'è o sta
// nel cestino.
func (b *Buste) Attesa(ctx context.Context, id int64, azione, motivo string, c audit.Contesto) (bool, error) {
	azione = minuscole(strings.TrimSpace(azione))
	if azione != "on" && azione != "off" {
		return false, ErrAzioneNonValida
	}
	if id < 1 {
		return false, ErrBustaNonTrovata
	}
	t, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var (
		in       bool
		dal, mot sql.NullString
	)
	err = tx.QueryRowContext(ctx, `SELECT attesa_cliente, attesa_dal, attesa_motivo FROM buste WHERE id = ? AND cancellata_il IS NULL`, id).
		Scan(&in, &dal, &mot)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrBustaNonTrovata
	}
	if err != nil {
		return false, err
	}
	dopo, nuovoDal, nuovoMotivo := false, sql.NullString{}, sql.NullString{}
	evento := "repair.waiting_ended"
	if azione == "on" {
		dopo, evento = true, "repair.waiting_started"
		nuovoDal = sql.NullString{String: clock.Formatta(t, "2006-01-02 15:04:05"), Valid: true}
		if m := strings.TrimSpace(motivo); m != "" {
			nuovoMotivo = sql.NullString{String: m, Valid: true}
		}
	}
	if dopo != in || nuovoDal != dal || nuovoMotivo != mot {
		if _, err := tx.ExecContext(ctx, `UPDATE buste SET attesa_cliente = ?, attesa_dal = ?, attesa_motivo = ?, aggiornato_il = ? WHERE id = ?`,
			dopo, nuovoDal, nuovoMotivo, ora, id); err != nil {
			return false, err
		}
	}
	if err := b.reg.Scrivi(ctx, tx, c, evento, "repair", id, "ok", nil); err != nil {
		return false, err
	}
	return dopo, tx.Commit()
}
