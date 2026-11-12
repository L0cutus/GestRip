package uscita

import (
	"context"
	"errors"
	"testing"
)

// AccodaIn (slice 070b): Accoda dentro la transazione di chi chiama, per l'email di pronta che si scrive
// insieme al cambio di stato della busta. Dati inventati.

// inventario: CLI-51, CLI-42
// La riga «queued» e outgoing.queued stanno nella transazione di chi chiama: fuori non si vedono prima del
// suo commit, il suo rollback le toglie tutte e due, il suo commit le scrive come Accoda. Gli stessi
// rifiuti di Accoda (WhatsApp, canale senza provider o spento, messaggio non valido), senza scrivere
// niente nella transazione.
func TestAccodaIn(t *testing.T) {
	d, o := apriDB(t)
	ctx := context.Background()
	c := Nuova(d, o, map[string]Provider{CanaleEmail: &providerFinto{}})
	dest, cont := email("cliente@esempio.it", "  Ciao  ")
	quante := func() int {
		return conta(t, d, `SELECT (SELECT count(*) FROM messaggi_in_uscita) * 1000 + (SELECT count(*) FROM eventi_audit)`)
	}
	// Rollback: niente resta.
	tx, err := d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AccodaIn(ctx, tx, dest, cont, contesto); err != nil {
		t.Fatal(err)
	}
	if n := quante(); n != 0 {
		t.Errorf("visibile prima del commit: %d", n)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if n := quante(); n != 0 {
		t.Errorf("dopo il rollback: %d", n)
	}
	// Commit: la riga e la sua riga d'audit, come Accoda.
	tx, err = d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := c.AccodaIn(ctx, tx, dest, cont, contesto)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := riga(t, d, id); got != "queued|0|2026-11-11T09:00:00.000000Z|-|-|-|-" {
		t.Errorf("riga %s", got)
	}
	if conta(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE id = ? AND testo = 'Ciao' AND indirizzo = 'cliente@esempio.it'`, id) != 1 {
		t.Error("colonne della riga")
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.queued' AND id_risorsa = ?
		AND dati = '{"channel":"email","party":"customer","repair_id":null}'`, id) != 1 {
		t.Error("audit")
	}
	// I rifiuti, dentro una transazione che poi si conferma: niente scritto.
	spento := Nuova(d, o, map[string]Provider{CanaleEmail: &providerAccendibile{}})
	prima := quante()
	tx, err = d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for nome, caso := range map[string]struct {
		c    *Coda
		dest Destinazione
		err  error
	}{
		"whatsapp":       {c, Destinazione{Canale: "whatsapp", Destinatario: "customer", Indirizzo: "+39333111"}, ErrSoloAMano},
		"senza provider": {c, Destinazione{Canale: CanaleTelegram, Destinatario: "customer", Indirizzo: "123"}, ErrNonConfigurato},
		"spento":         {spento, dest, ErrNonConfigurato},
		"a capo":         {c, Destinazione{Canale: CanaleEmail, Destinatario: "customer", Indirizzo: "a@esempio.it\r\nBcc: b@esempio.it"}, ErrMessaggioNonValido},
	} {
		if _, err := caso.c.AccodaIn(ctx, tx, caso.dest, cont, contesto); !errors.Is(err, caso.err) {
			t.Errorf("%s: %v", nome, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if dopo := quante(); dopo != prima {
		t.Errorf("un rifiuto ha scritto: %d → %d", prima, dopo)
	}
	// Accoda resta com'era: una transazione sua.
	if _, err := c.Accoda(ctx, dest, cont, contesto); err != nil || quante() != prima+1001 {
		t.Errorf("Accoda: %v %d", err, quante())
	}
}
