package stampe

import (
	"testing"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/pdf"
	"github.com/L0cutus/gestrip/internal/pdf/lettura"
)

// inventario: ART-12
// Le etichette di una consegna (HandoverLabels.render_html/3 dello standalone): una pagina 76×127 per
// busta, nell'ordine dato, ognuna disegnata come l'etichetta della busta singola (stesse righe, «→
// ARTIGIANO» di chi la porta, «All'artigiano:» della D-73) e con il suo QR. Senza buste niente PDF.
func TestEtichetteConsegna(t *testing.T) {
	prima := etichetta5()
	prima.Artigiano = "Alfa Orafi"
	seconda := etichetta5()
	seconda.ID, seconda.Numero, seconda.QR, seconda.Artigiano = 9, "9/26", "RIP-2026-0045", "Alfa Orafi"
	seconda.Visibile = buste.NumeroMostrato("RIP-2026-0045", "9/26")
	seconda.Scadenza, seconda.Descrizione = "", "Collana"
	b, err := EtichetteConsegna([]buste.DatiEtichetta{prima, seconda})
	if err != nil {
		t.Fatal(err)
	}
	if err := pdf.Controlla(b); err != nil {
		t.Fatal(err)
	}
	l, err := lettura.Leggi(b)
	if err != nil {
		t.Fatal(err)
	}
	if lettura.Formato(l) != "76x127,76x127" || len(l.Pagine) != 2 {
		t.Fatalf("formato %q, %d pagine", lettura.Formato(l), len(l.Pagine))
	}
	attese := [][]string{
		{"→ ALFA ORAFI", "N. 5/26", "All'artigiano: 03/11/2026", "Scad.: fine mese", "IN LAVORAZIONE", "Lavoro sintetico", " FATTO", "Note:"},
		{"→ ALFA ORAFI", "#0045/26", "All'artigiano: 03/11/2026", "IN LAVORAZIONE", "Collana", " FATTO", "Note:"},
	}
	qr := []string{"SINT-QR-0005", "RIP-2026-0045"}
	for i, p := range l.Pagine {
		var righe []string
		for _, r := range p.Righe {
			righe = append(righe, r.Testo)
		}
		uguali(t, "pagina", righe, attese[i])
		if qq, err := lettura.CodiciQR(p); err != nil || len(qq) != 1 || qq[0].Testo != qr[i] {
			t.Errorf("pagina %d: QR %v %+v", i+1, err, qq)
		}
	}
	// Una pagina sola è la stessa dell'etichetta della busta.
	una, err := EtichetteConsegna([]buste.DatiEtichetta{prima})
	if err != nil {
		t.Fatal(err)
	}
	sola, err := Etichetta(prima)
	if err != nil {
		t.Fatal(err)
	}
	lu, _ := lettura.Leggi(una)
	ls, _ := lettura.Leggi(sola)
	if len(lu.Pagine) != 1 || lettura.TestoNormalizzato(lu) != lettura.TestoNormalizzato(ls) {
		t.Errorf("una busta: %q contro %q", lettura.TestoNormalizzato(lu), lettura.TestoNormalizzato(ls))
	}
	if _, err := EtichetteConsegna(nil); err == nil {
		t.Error("PDF senza buste")
	}
}
