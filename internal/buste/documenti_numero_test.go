package buste

import (
	"testing"
)

// inventario: RIP-S-42, RIP-S-43, RIP-S-45, RIP-S-46, RIP-S-37
// D-64 nei documenti (slice 058): ricevuta, etichetta 76×127, scheda PDF, report e pagina della firma
// portano lo stesso numero visibile della lista e della scheda (NumeroVisibile sul codice salvato):
// l'etichetta «0045/26» (anche di un altro anno, anche salvata con gli spazi o «rip-» minuscolo, come
// la legge NormalizzaCodice), altrimenti «N. 45/26» (nessun codice, vuoto, di altro formato, un'etichetta
// senza le quattro cifre). Il numero interno resta in Numero (nome del file, registro), il QR
// dell'etichetta resta il codice salvato com'è (ValoreQR).
func TestNumeroVisibileDeiDocumenti(t *testing.T) {
	p := conFirma(t)
	numero := ""
	if err := p.d.Lettura.QueryRowContext(ctx, `SELECT numero FROM buste WHERE id = ?`, p.busta).Scan(&numero); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		qr     any
		atteso Mostrato
		valore string // il QR dell'etichetta
	}{
		{"RIP-2026-0045", Mostrato{"0045/26", true}, "RIP-2026-0045"},
		{"RIP-2025-0045", Mostrato{"0045/25", true}, "RIP-2025-0045"},
		{" rip-2026- 0046 ", Mostrato{"0046/26", true}, " rip-2026- 0046 "},
		{nil, Mostrato{"N. " + numero, false}, numero},
		{"", Mostrato{"N. " + numero, false}, numero},
		{"SINT-ALTRO-36", Mostrato{"N. " + numero, false}, "SINT-ALTRO-36"},
		{"RIP-2026-45", Mostrato{"N. " + numero, false}, "RIP-2026-45"},
	} {
		esegui(t, p.d, `UPDATE buste SET codice_qr = ? WHERE id = ?`, c.qr, p.busta)
		id := itoa(p.busta)
		_, r, err := p.b.Ricevuta(ctx, id, chi, faiFinto([]byte("%PDF-x"), nil))
		if err != nil || r.Visibile != c.atteso || r.Numero != numero {
			t.Errorf("%v: ricevuta %+v %q %v", c.qr, r.Visibile, r.Numero, err)
		}
		_, e, err := p.b.Etichetta(ctx, id, chi, etichettaFinta([]byte("%PDF-x"), nil))
		if err != nil || e.Visibile != c.atteso || e.Numero != numero || e.QR != c.valore {
			t.Errorf("%v: etichetta %+v %q %q %v", c.qr, e.Visibile, e.Numero, e.QR, err)
		}
		_, e, err = p.b.EtichettaDaStampare(ctx, id, etichettaFinta([]byte("%PDF-x"), nil))
		if err != nil || e.Visibile != c.atteso || e.QR != c.valore {
			t.Errorf("%v: etichetta da stampare %+v %q %v", c.qr, e.Visibile, e.QR, err)
		}
		_, s, err := p.b.SchedaPDF(ctx, id, chi, schedaFinta([]byte("%PDF-x"), nil))
		if err != nil || s.Visibile != c.atteso || s.Numero != numero {
			t.Errorf("%v: scheda PDF %+v %q %v", c.qr, s.Visibile, s.Numero, err)
		}
		_, dr, err := p.b.Report(ctx, []int64{p.busta}, chi, reportFinto([]byte("%PDF-x"), nil))
		if err != nil || len(dr.Schede) != 1 || dr.Schede[0].Visibile != c.atteso || dr.Schede[0].Numero != numero {
			t.Errorf("%v: report %+v %v", c.qr, dr.Schede, err)
		}
		pg, err := p.b.ApriFirma(ctx, id)
		if err != nil || pg.Visibile != c.atteso || pg.Numero != numero {
			t.Errorf("%v: firma %+v %q %v", c.qr, pg.Visibile, pg.Numero, err)
		}
	}
	// Il registro resta sul numero interno.
	if n := conta(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione IN ('repair.receipt_generated', 'repair.label_generated',
		'repair.sheet_generated', 'repair.report_generated') AND dati = ?`, `{"bag_number":"`+numero+`"}`); n != 4*7 {
		t.Errorf("righe con il numero interno: %d", n)
	}
}

// Mostrato.Titolo: il «#» solo davanti a un'etichetta.
func TestTitoloDelNumeroMostrato(t *testing.T) {
	for _, c := range []struct {
		qr, numero, titolo string
	}{
		{"RIP-2026-0045", "21/26", "#0045/26"}, {"RIP-2025-0012", "9999/25", "#0012/25"}, {"", "35/26", "N. 35/26"},
		{"SINT-QR-0005", "5/26", "N. 5/26"}, {"", "10000/25", "N. 10000/25"},
	} {
		if got := NumeroMostrato(c.qr, c.numero).Titolo(); got != c.titolo {
			t.Errorf("%q %q: %q", c.qr, c.numero, got)
		}
	}
}

// inventario: RIP-S-37, RIP-S-45
// Review 058: una scheda Compro Oro ha lo stesso numero visibile delle riparazioni, nei documenti e
// nella pagina della firma (stato «perfezionata», da cui si firma): la regola non guarda compro_oro.
func TestNumeroVisibileCompraOro(t *testing.T) {
	p := conFirma(t)
	oro := crea(t, p.b, con(modulo(), func(a *Accettazione) { a.CompraOro = "1" })).ID
	numero := ""
	if err := p.d.Lettura.QueryRowContext(ctx, `SELECT numero FROM buste WHERE id = ?`, oro).Scan(&numero); err != nil {
		t.Fatal(err)
	}
	esegui(t, p.d, `UPDATE buste SET stato = 'perfezionata', codice_qr = 'RIP-2026-0071' WHERE id = ?`, oro)
	atteso := Mostrato{"0071/26", true}
	pg, err := p.b.ApriFirma(ctx, itoa(oro))
	if err != nil || pg.Visibile != atteso || pg.Numero != numero {
		t.Errorf("firma %+v %q %v", pg.Visibile, pg.Numero, err)
	}
	_, s, err := p.b.SchedaPDF(ctx, itoa(oro), chi, schedaFinta([]byte("%PDF-x"), nil))
	if err != nil || !s.CompraOro || s.Visibile != atteso || s.Numero != numero {
		t.Errorf("scheda PDF %+v %q %v", s.Visibile, s.Numero, err)
	}
	_, r, err := p.b.Ricevuta(ctx, itoa(oro), chi, faiFinto([]byte("%PDF-x"), nil))
	if err != nil || r.Visibile != atteso {
		t.Errorf("ricevuta %+v %v", r.Visibile, err)
	}
	_, e, err := p.b.Etichetta(ctx, itoa(oro), chi, etichettaFinta([]byte("%PDF-x"), nil))
	if err != nil || e.Visibile != atteso || e.QR != "RIP-2026-0071" {
		t.Errorf("etichetta %+v %q %v", e.Visibile, e.QR, err)
	}
	esegui(t, p.d, `UPDATE buste SET codice_qr = NULL WHERE id = ?`, oro)
	if pg, err := p.b.ApriFirma(ctx, itoa(oro)); err != nil || pg.Visibile != (Mostrato{"N. " + numero, false}) {
		t.Errorf("firma senza QR %+v %v", pg.Visibile, err)
	}
}
