package buste

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/audit"
)

// La riapertura di una busta chiusa (RIP-S-14), come Workshop.reopen_repair/3,
// Repair.reopen_statuses/1 e Repair.reopen_changeset/2 dello standalone (go-baseline): la sola
// porta da uno stato chiuso verso il lavoro, con un motivo che va nell'audit e non sulla busta.
// Non scrive date (pronta e consegna restano, anche dopo una seconda consegna) e azzera la
// conservazione delle foto. Il contatore delle versioni lo conta il trigger, come ogni cambio.

// I messaggi della riapertura sui campi, con i testi dello standalone.
const (
	MsgMotivoObbligatorio = "Motivo obbligatorio."
	MsgMotivoLungo        = "Motivo troppo lungo: massimo 255 caratteri."
	MsgNonRiapre          = "non riapre la busta"
	MsgNonChiusa          = "la busta non è chiusa"
)

// motivoMassimo è @max_reason_text, in grafemi come validate_length di Ecto.
const motivoMassimo = 255

// StatiDiRiapertura sono gli stati in cui una busta chiusa può riaprirsi: quelli della sua famiglia
// meno i chiusi, nell'ordine dello standalone; nessuno per una busta aperta.
func StatiDiRiapertura(stato string, compraOro bool) []string {
	if !Chiuso(stato) {
		return []string{}
	}
	var s []string
	for _, x := range Stati(compraOro) {
		if !Chiuso(x) {
			s = append(s, x)
		}
	}
	return s
}

// Riapri riporta la busta id dallo stato chiuso a stato, con il motivo motivo. Gli errori sui
// campi tutti insieme (Campi, «stato» e «motivo»), come il changeset: motivo vuoto o di soli spazi,
// oltre 255 grafemi dopo aver tolto gli spazi ai bordi, stato che non è una riapertura della
// busta. Audit repair.reopened con gli stati da/a e il motivo. Una busta che non c'è o sta nel
// cestino: ErrBustaNonTrovata. Ogni rifiuto non scrive niente.
func (b *Buste) Riapri(ctx context.Context, id int64, stato, motivo string, c audit.Contesto) (Cambio, error) {
	if id < 1 {
		return Cambio{}, ErrBustaNonTrovata
	}
	_, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return Cambio{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var (
		prima string
		oro   bool
	)
	err = tx.QueryRowContext(ctx, `SELECT stato, compro_oro FROM buste WHERE id = ? AND cancellata_il IS NULL`, id).Scan(&prima, &oro)
	if errors.Is(err, sql.ErrNoRows) {
		return Cambio{}, ErrBustaNonTrovata
	}
	if err != nil {
		return Cambio{}, err
	}
	campi := Campi{}
	// cast/3 rende nil un motivo vuoto o di soli spazi; poi update_change(&String.trim/1).
	m := strings.TrimSpace(motivo)
	switch {
	case m == "":
		campi.aggiungi("motivo", MsgMotivoObbligatorio)
	case uniseg.GraphemeClusterCount(m) > motivoMassimo:
		campi.aggiungi("motivo", MsgMotivoLungo)
	}
	if !slices.Contains(StatiDiRiapertura(prima, oro), stato) {
		if Chiuso(prima) {
			campi.aggiungi("stato", MsgNonRiapre)
		} else {
			campi.aggiungi("stato", MsgNonChiusa)
		}
	}
	if len(campi) > 0 {
		return Cambio{}, campi
	}
	if _, err := tx.ExecContext(ctx, `UPDATE buste SET stato = ?, conservazione_iniziata_il = NULL, aggiornato_il = ? WHERE id = ?`,
		stato, ora, id); err != nil {
		return Cambio{}, err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "repair.reopened", "repair", id, "ok",
		map[string]any{"status_from": prima, "status_to": stato, "reason": m}); err != nil {
		return Cambio{}, err
	}
	cambio := Cambio{Prima: prima, Dopo: stato}
	if cambio.Versione, err = versione(ctx, tx, id); err != nil {
		return Cambio{}, err
	}
	if err := tx.Commit(); err != nil {
		return Cambio{}, err
	}
	// Dopo il commit lo standalone accoda l'email di pronta se la busta si riapre in «pronta»
	// (announce_ready_by_email/4, area email): chi chiama lo sa da cambio.Dopo.
	return cambio, nil
}
