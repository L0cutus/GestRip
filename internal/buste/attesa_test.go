package buste

import (
	"errors"
	"testing"
	"time"
)

// L'attesa della risposta del cliente (RIP-S-15), come Workshop.set_repair_waiting/4 dello
// standalone (go-baseline). I casi da workshop_test.exs: «waiting is independent of status, trims
// optional reason, clears all fields and audits atomically».

func rigaAttesa(t *testing.T, b *Buste, id int64) string {
	t.Helper()
	return valore(t, b.d, `SELECT attesa_cliente || '|' || coalesce(attesa_dal, '∅') || '|' || coalesce(attesa_motivo, '∅') || '|' || stato
		FROM buste WHERE id = ?`, id).(string)
}

// inventario: RIP-S-15
// «Metti in attesa» non tocca lo stato: azione senza maiuscole né spazi ai bordi, motivo tolto
// dagli spazi (vuoto = nessuno), «dal» con l'ora del negozio in «AAAA-MM-GG HH:MM:SS»; togliere
// l'attesa svuota i tre campi; l'audit repair.waiting_started / repair.waiting_ended non ha dati
// (il motivo non ci va). Un'azione sconosciuta non scrive niente. L'attesa resta anche su una
// busta consegnata, e si rimette.
func TestAttesaIndipendenteDalloStato(t *testing.T) {
	b, d, o := prova(t)
	busta := crea(t, b, modulo())
	// 25/10/2026 alle 00:30 UTC: in negozio sono le 02:30, ancora l'ora legale.
	o.Imposta(time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC))
	in, err := b.Attesa(ctx, busta.ID, " ON ", "  Quale pietra?  ", chi)
	if err != nil || !in {
		t.Fatalf("attesa: %v %v", in, err)
	}
	if got := rigaAttesa(t, b, busta.ID); got != "1|2026-10-25 02:30:00|Quale pietra?|in_lavorazione" {
		t.Errorf("in attesa: %s", got)
	}
	if s := ultimoAudit(t, d, "repair.waiting_started"); s != "repair.waiting_started|repair|"+itoa(busta.ID)+"|ok|{}" {
		t.Errorf("audit %q", s)
	}
	if valore(t, d, `SELECT attore || '|' || ip FROM eventi_audit WHERE azione = 'repair.waiting_started'`) != "titolare@esempio.test|203.0.113.7" {
		t.Error("attore dell'audit")
	}

	prima := scatta(t, d)
	for _, a := range []string{"toggle", "", "o n", "1"} {
		if _, err := b.Attesa(ctx, busta.ID, a, "", chi); !errors.Is(err, ErrAzioneNonValida) || err.Error() != "Azione non valida." {
			t.Errorf("azione %q: %v", a, err)
		}
	}
	uguali(t, "azione sconosciuta", prima, scatta(t, d))

	if in, err := b.Attesa(ctx, busta.ID, "off", "ignorato", chi); err != nil || in {
		t.Fatalf("tolta: %v %v", in, err)
	}
	if got := rigaAttesa(t, b, busta.ID); got != "0|∅|∅|in_lavorazione" {
		t.Errorf("tolta: %s", got)
	}
	if s := ultimoAudit(t, d, "repair.waiting_ended"); s != "repair.waiting_ended|repair|"+itoa(busta.ID)+"|ok|{}" {
		t.Errorf("audit %q", s)
	}

	// Un'ora dopo in UTC, alle 01:30: in negozio sono di nuovo le 02:30, già l'ora solare.
	o.Imposta(time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC))
	if _, err := b.Attesa(ctx, busta.ID, "on", "  ", chi); err != nil {
		t.Fatal(err)
	}
	if got := rigaAttesa(t, b, busta.ID); got != "1|2026-10-25 02:30:00|∅|in_lavorazione" {
		t.Errorf("senza motivo: %s", got)
	}
	for _, s := range []string{"pronta", "consegnata"} {
		if _, err := b.CambiaStato(ctx, busta.ID, s, chi); err != nil {
			t.Fatal(err)
		}
	}
	if got := rigaAttesa(t, b, busta.ID); got != "1|2026-10-25 02:30:00|∅|consegnata" {
		t.Errorf("dopo la consegna: %s", got)
	}
	o.Imposta(time.Date(2026, 11, 2, 16, 5, 9, 0, time.UTC))
	if _, err := b.Attesa(ctx, busta.ID, "On", "Seconda domanda", chi); err != nil {
		t.Fatal(err)
	}
	if got := rigaAttesa(t, b, busta.ID); got != "1|2026-11-02 17:05:09|Seconda domanda|consegnata" {
		t.Errorf("rimessa su una busta consegnata: %s", got)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione IN ('repair.waiting_started', 'repair.waiting_ended')`); n != 4 {
		t.Errorf("righe d'audit %d", n)
	}
}

// inventario: RIP-S-15
// Si scrivono solo i tre campi, e solo se cambiano: togliere un'attesa che non c'è non tocca la
// busta (aggiornato_il compreso, come un changeset senza cambi) ma scrive lo stesso
// repair.waiting_ended. Una busta che non c'è o sta nel cestino: non trovata. Se l'audit non si
// scrive, l'attesa non cambia.
func TestAttesaScritturaEAudit(t *testing.T) {
	b, d, o := prova(t)
	busta := crea(t, b, modulo())
	o.Sposta(time.Hour)
	riga := valore(t, d, `SELECT aggiornato_il FROM buste WHERE id = ?`, busta.ID)
	if _, err := b.Attesa(ctx, busta.ID, "off", "", chi); err != nil {
		t.Fatal(err)
	}
	if valore(t, d, `SELECT aggiornato_il FROM buste WHERE id = ?`, busta.ID) != riga {
		t.Error("togliere un'attesa che non c'è ha toccato la busta")
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.waiting_ended'`) != 1 {
		t.Error("manca repair.waiting_ended")
	}
	if _, err := b.Attesa(ctx, busta.ID, "on", "Colore?", chi); err != nil {
		t.Fatal(err)
	}
	ora := o.Adesso().Format("2006-01-02T15:04:05.000000Z")
	if valore(t, d, `SELECT aggiornato_il FROM buste WHERE id = ?`, busta.ID) != ora {
		t.Error("aggiornato_il non segue l'attesa")
	}

	esegui(t, d, `CREATE TEMP TRIGGER blocca BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.waiting_ended' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if _, err := b.Attesa(ctx, busta.ID, "off", "", chi); err == nil || errors.Is(err, ErrAzioneNonValida) || errors.Is(err, ErrBustaNonTrovata) {
		t.Fatalf("audit rifiutato: %v", err)
	}
	if got := rigaAttesa(t, b, busta.ID); got != "1|2026-10-31 11:00:00|Colore?|in_lavorazione" {
		t.Errorf("attesa cambiata senza audit: %s", got)
	}
	esegui(t, d, `DROP TRIGGER blocca`)

	for _, id := range []int64{busta.ID + 100, 0} {
		if _, err := b.Attesa(ctx, id, "on", "", chi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("busta %d: %v", id, err)
		}
	}
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, busta.ID)
	if _, err := b.Attesa(ctx, busta.ID, "off", "", chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("busta nel cestino: %v", err)
	}
	if got := rigaAttesa(t, b, busta.ID); got != "1|2026-10-31 11:00:00|Colore?|in_lavorazione" {
		t.Errorf("busta nel cestino cambiata: %s", got)
	}
}
