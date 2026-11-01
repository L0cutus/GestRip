package buste

import (
	"errors"
	"testing"
	"time"
)

// La chiusura del lotto d'avviso alla consegna (slice 212 dello standalone,
// ReadyLots.close_after_delivery/2 dentro transition/5): nella transazione della consegna e con il
// suo istante, la regola di close_if_complete/2. I casi da ready_lot_close_on_delivery_test.exs.
// La chiusura all'apertura della pagina pubblica del lotto è dell'area oreficerie e lotti.

// lotto mette nel database un lotto dell'oreficeria 2, inviato se inviato, e ci aggiunge le buste.
func lotto(t *testing.T, b *Buste, inviato bool, buste ...int64) int64 {
	t.Helper()
	var inv any
	if inviato {
		inv = istanteProva
	}
	r, err := b.d.Scrittura.Exec(`INSERT INTO lotti_avviso (oreficeria_id, token, inviato_il, creato_il, aggiornato_il) VALUES (2, ?, ?, ?, ?)`,
		"gettone-"+time.Now().Format("150405.000000000"), inv, istanteProva, istanteProva)
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	for _, busta := range buste {
		esegui(t, b.d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, id, busta)
	}
	return id
}

// prontaDiOreficeria è una busta dell'oreficeria 2, pronta.
func prontaDiOreficeria(t *testing.T, b *Buste) int64 {
	t.Helper()
	c := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = "2" }))
	if _, err := b.CambiaStato(ctx, c.ID, "pronta", chi); err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func chiusoIl(t *testing.T, b *Buste, lotto int64) any {
	t.Helper()
	return valore(t, b.d, `SELECT chiuso_il FROM lotti_avviso WHERE id = ?`, lotto)
}

// inventario: RIP-S-13
// L'ultima consegna chiude il lotto inviato, con l'istante della consegna (anche l'ultima modifica
// del lotto); quella prima no. Una consegna dopo, a lotto chiuso, non cambia la data.
func TestConsegnaChiudeIlLotto(t *testing.T) {
	b, d, o := prova(t)
	uno, due := prontaDiOreficeria(t, b), prontaDiOreficeria(t, b)
	l := lotto(t, b, true, uno, due)
	altro := lotto(t, b, true, prontaDiOreficeria(t, b))
	if _, err := b.CambiaStato(ctx, uno, "consegnata", chi); err != nil {
		t.Fatal(err)
	}
	if chiusoIl(t, b, l) != nil {
		t.Fatal("chiuso con una busta ancora da ritirare")
	}
	o.Sposta(90 * time.Minute)
	ora := o.Adesso().Format("2006-01-02T15:04:05.000000Z")
	if _, err := b.CambiaStato(ctx, due, "consegnata", chi); err != nil {
		t.Fatal(err)
	}
	if v := valore(t, d, `SELECT chiuso_il || '|' || aggiornato_il FROM lotti_avviso WHERE id = ?`, l); v != ora+"|"+ora {
		t.Errorf("lotto %v, atteso chiuso alle %s", v, ora)
	}
	if v := valore(t, d, `SELECT conservazione_iniziata_il FROM buste WHERE id = ?`, due); v != ora {
		t.Errorf("istante della consegna %v", v)
	}
	if chiusoIl(t, b, altro) != nil {
		t.Error("chiuso un altro lotto")
	}

	// Riaperta e consegnata di nuovo: la chiusura resta quella di prima.
	if _, err := b.Riapri(ctx, due, "pronta", "Prova di riapertura", chi); err != nil {
		t.Fatal(err)
	}
	o.Sposta(time.Hour)
	if _, err := b.CambiaStato(ctx, due, "consegnata", chi); err != nil {
		t.Fatal(err)
	}
	if v := valore(t, d, `SELECT chiuso_il || '|' || aggiornato_il FROM lotti_avviso WHERE id = ?`, l); v != ora+"|"+ora {
		t.Errorf("lotto dopo la seconda consegna %v", v)
	}
}

// inventario: RIP-S-13
// Non si chiude: un lotto mai inviato, un lotto con una busta nel cestino non ritirata, una busta
// annullata del lotto. Una consegna fuori da ogni lotto non tocca i lotti.
func TestConsegnaNonChiudeIlLotto(t *testing.T) {
	b, d, _ := prova(t)
	maiInviato := prontaDiOreficeria(t, b)
	lMai := lotto(t, b, false, maiInviato)
	conCestino, cestinata := prontaDiOreficeria(t, b), prontaDiOreficeria(t, b)
	lCestino := lotto(t, b, true, conCestino, cestinata)
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestinata)
	conAnnullata, annullata := prontaDiOreficeria(t, b), prontaDiOreficeria(t, b)
	lAnnullata := lotto(t, b, true, conAnnullata, annullata)
	fuori := prontaDiOreficeria(t, b)
	prima := valore(t, d, `SELECT group_concat(id || ':' || coalesce(chiuso_il, '-') || ':' || aggiornato_il) FROM lotti_avviso`)
	if _, err := b.CambiaStato(ctx, annullata, "annullata", chi); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{maiInviato, conCestino, conAnnullata, fuori} {
		if _, err := b.CambiaStato(ctx, id, "consegnata", chi); err != nil {
			t.Fatal(err)
		}
	}
	for _, l := range []int64{lMai, lCestino, lAnnullata} {
		if chiusoIl(t, b, l) != nil {
			t.Errorf("lotto %d chiuso", l)
		}
	}
	if valore(t, d, `SELECT group_concat(id || ':' || coalesce(chiuso_il, '-') || ':' || aggiornato_il) FROM lotti_avviso`) != prima {
		t.Error("i lotti sono cambiati")
	}
}

// inventario: RIP-S-13
// La chiusura sta nella transazione della consegna: se la consegna non si salva (l'audit
// rifiutato), il lotto resta aperto. Un passaggio a uno stato diverso da «consegnata» non chiude
// niente, anche con tutte le altre buste ritirate.
func TestChiusuraDelLottoNellaTransazione(t *testing.T) {
	b, d, _ := prova(t)
	uno, due := prontaDiOreficeria(t, b), prontaDiOreficeria(t, b)
	l := lotto(t, b, true, uno, due)
	if _, err := b.CambiaStato(ctx, uno, "consegnata", chi); err != nil {
		t.Fatal(err)
	}
	if _, err := b.CambiaStato(ctx, due, "in_lavorazione", chi); err != nil {
		t.Fatal(err)
	}
	if _, err := b.CambiaStato(ctx, due, "pronta", chi); err != nil {
		t.Fatal(err)
	}
	if chiusoIl(t, b, l) != nil {
		t.Fatal("chiuso senza consegna")
	}
	esegui(t, d, `CREATE TEMP TRIGGER blocca BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.status_changed' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if _, err := b.CambiaStato(ctx, due, "consegnata", chi); err == nil || errors.Is(err, ErrTransizione) {
		t.Fatalf("consegna con l'audit rifiutato: %v", err)
	}
	if chiusoIl(t, b, l) != nil || valore(t, d, `SELECT stato FROM buste WHERE id = ?`, due) != "pronta" {
		t.Error("la consegna rifiutata ha chiuso il lotto")
	}
	esegui(t, d, `DROP TRIGGER blocca`)
	if _, err := b.CambiaStato(ctx, due, "consegnata", chi); err != nil {
		t.Fatal(err)
	}
	if chiusoIl(t, b, l) == nil {
		t.Error("lotto non chiuso")
	}
}

// inventario: RIP-S-13
// Un guasto nella chiusura del lotto (qui un trigger di prova che la rifiuta) fa fallire la
// consegna: la busta resta pronta, il lotto aperto, nessuna riga d'audit del cambio.
func TestGuastoNellaChiusuraDelLotto(t *testing.T) {
	b, d, _ := prova(t)
	uno := prontaDiOreficeria(t, b)
	l := lotto(t, b, true, uno)
	esegui(t, d, `CREATE TEMP TRIGGER blocca_lotto BEFORE UPDATE ON lotti_avviso BEGIN SELECT RAISE(ABORT, 'no'); END`)
	prima := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed'`)
	if _, err := b.CambiaStato(ctx, uno, "consegnata", chi); err == nil || errors.Is(err, ErrTransizione) {
		t.Fatalf("consegna con la chiusura del lotto rifiutata: %v", err)
	}
	if valore(t, d, `SELECT stato FROM buste WHERE id = ?`, uno) != "pronta" || chiusoIl(t, b, l) != nil {
		t.Error("la consegna fallita ha lasciato qualcosa")
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed'`) != prima {
		t.Error("audit del cambio scritto")
	}
}
