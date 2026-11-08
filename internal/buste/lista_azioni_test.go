package buste

import (
	"errors"
	"testing"
	"time"
)

// Le azioni del riquadro della lista (slice 056): i pulsanti rapidi (quick-status, @quick_steps di
// workshop_live.ex) e il 📌 (toggle_repair_pin/3 di workshop.ex).

// inventario: RIP-H-14
// «✓ Pronta» porta a pronta solo una busta in lavorazione, «✓ Consegnata» a consegnata solo una
// pronta, con lo stesso cambio di stato della scheda (date, audit). Ogni altra partenza, un arrivo
// che non è un passo rapido, un compro oro, una busta che non c'è o nel cestino: «Transizione non
// consentita.» e niente scritto.
func TestPassoRapido(t *testing.T) {
	b, d, o := prova(t)
	busta := crea(t, b, modulo())
	o.Sposta(time.Hour)

	if c, err := b.PassoRapido(ctx, busta.ID, "consegnata", chi); !errors.Is(err, ErrTransizione) || err.Error() != "Transizione non consentita." || c != (Cambio{}) {
		t.Fatalf("consegnata da in lavorazione: %+v %v", c, err)
	}
	c, err := b.PassoRapido(ctx, busta.ID, "pronta", chi)
	if err != nil || c.Prima != "in_lavorazione" || c.Dopo != "pronta" || c.Versione != 1 {
		t.Fatalf("pronta: %+v %v", c, err)
	}
	if got := valore(t, d, `SELECT stato || '|' || data_pronta FROM buste WHERE id = ?`, busta.ID); got != "pronta|2026-10-31" {
		t.Errorf("dopo pronta: %v", got)
	}
	if s := ultimoAudit(t, d, "repair.status_changed"); s != "repair.status_changed|repair|"+itoa(busta.ID)+`|ok|{"status":"pronta"}` {
		t.Errorf("audit %q", s)
	}
	// Un secondo «✓ Pronta» (la pagina vecchia, un doppio tocco): rifiutato, niente in più.
	if _, err := b.PassoRapido(ctx, busta.ID, "pronta", chi); !errors.Is(err, ErrTransizione) {
		t.Errorf("pronta due volte: %v", err)
	}
	if c, err := b.PassoRapido(ctx, busta.ID, "consegnata", chi); err != nil || c.Prima != "pronta" || c.Dopo != "consegnata" {
		t.Fatalf("consegnata: %+v %v", c, err)
	}
	if got := valore(t, d, `SELECT stato || '|' || data_consegna || '|' || (conservazione_iniziata_il IS NOT NULL) FROM buste WHERE id = ?`, busta.ID); got != "consegnata|2026-10-31|1" {
		t.Errorf("dopo consegnata: %v", got)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed'`); n != 2 {
		t.Errorf("righe d'audit %d", n)
	}

	// Partenze sbagliate: preventivo e attesa materiali (da cui il grafo arriva a pronta), compro oro.
	prev := crea(t, b, modulo())
	attesa := crea(t, b, modulo())
	oro := crea(t, b, con(modulo(), func(a *Accettazione) { a.CompraOro = "1" }))
	if _, err := b.CambiaStato(ctx, prev.ID, "preventivo", chi); err != nil {
		t.Fatal(err)
	}
	if _, err := b.CambiaStato(ctx, attesa.ID, "in_attesa_materiali", chi); err != nil {
		t.Fatal(err)
	}
	cestinata := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestinata.ID)
	prima := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	for _, x := range []struct {
		id    int64
		stato string
	}{{prev.ID, "pronta"}, {prev.ID, "consegnata"}, {attesa.ID, "pronta"}, {oro.ID, "pronta"}, {oro.ID, "consegnata"},
		{prev.ID, "annullata"}, {prev.ID, "in_lavorazione"}, {prev.ID, ""}, {busta.ID, "pronta"},
		{cestinata.ID, "pronta"}, {99999, "pronta"}, {0, "consegnata"}} {
		if _, err := b.PassoRapido(ctx, x.id, x.stato, chi); !errors.Is(err, ErrTransizione) {
			t.Errorf("%d → %q: %v", x.id, x.stato, err)
		}
	}
	if got := valore(t, d, `SELECT group_concat(stato, ',') FROM (SELECT stato FROM buste WHERE id IN (?, ?, ?, ?) ORDER BY id)`,
		prev.ID, attesa.ID, oro.ID, cestinata.ID); got != "preventivo,in_attesa_materiali,in_valutazione,in_lavorazione" {
		t.Errorf("stati dopo i rifiuti: %v", got)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit`); n != prima {
		t.Errorf("audit dopo i rifiuti: %d righe, prima %d", n, prima)
	}
}

// inventario: RIP-H-16
// Il 📌: fissa adesso e scrive repair.pinned, il secondo tocco toglie e scrive repair.unpinned; le
// fissate vanno in cima alla lista. Una busta che non c'è o nel cestino: ErrBustaNonTrovata, niente
// scritto.
func TestCambiaFissata(t *testing.T) {
	b, d, o := prova(t)
	vecchia := crea(t, b, modulo())
	nuova := crea(t, b, modulo())
	o.Sposta(time.Hour)
	ora := o.Adesso().Format("2006-01-02T15:04:05.000000Z")

	primo := func() int64 {
		l, err := b.Lista(ctx, Filtri{Pagina: 1})
		if err != nil || len(l.Righe) == 0 {
			t.Fatalf("lista: %v", err)
		}
		return l.Righe[0].ID
	}
	if primo() != nuova.ID {
		t.Fatalf("senza fissate la prima è la più recente")
	}
	fissata, err := b.CambiaFissata(ctx, vecchia.ID, chi)
	if err != nil || !fissata {
		t.Fatalf("fissa: %v %v", fissata, err)
	}
	if got := valore(t, d, `SELECT fissata_il || '|' || aggiornato_il FROM buste WHERE id = ?`, vecchia.ID); got != ora+"|"+ora {
		t.Errorf("dopo il 📌: %v", got)
	}
	if s := ultimoAudit(t, d, "repair.pinned"); s != "repair.pinned|repair|"+itoa(vecchia.ID)+"|ok|{}" {
		t.Errorf("audit %q", s)
	}
	if primo() != vecchia.ID {
		t.Error("la fissata non è in cima")
	}
	fissata, err = b.CambiaFissata(ctx, vecchia.ID, chi)
	if err != nil || fissata {
		t.Fatalf("togli: %v %v", fissata, err)
	}
	if got := valore(t, d, `SELECT coalesce(fissata_il, '-') FROM buste WHERE id = ?`, vecchia.ID); got != "-" {
		t.Errorf("dopo «Togli pin»: %v", got)
	}
	if s := ultimoAudit(t, d, "repair.unpinned"); s != "repair.unpinned|repair|"+itoa(vecchia.ID)+"|ok|{}" {
		t.Errorf("audit %q", s)
	}
	if primo() != nuova.ID {
		t.Error("tolto il pin, la busta resta in cima")
	}

	cestinata := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestinata.ID)
	prima := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	for _, id := range []int64{cestinata.ID, 99999, 0, -1} {
		if _, err := b.CambiaFissata(ctx, id, chi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("%d: %v", id, err)
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit`); n != prima {
		t.Errorf("audit dopo i rifiuti: %d righe, prima %d", n, prima)
	}
	if n := conta(t, d, `SELECT count(*) FROM buste WHERE fissata_il IS NOT NULL`); n != 0 {
		t.Errorf("fissate: %d", n)
	}
}
