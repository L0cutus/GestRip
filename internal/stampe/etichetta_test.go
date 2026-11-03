package stampe

import (
	"os"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/pdf"
	"github.com/L0cutus/gestrip/internal/pdf/lettura"
)

func etichetta(t *testing.T, e buste.DatiEtichetta) (*lettura.Documento, []string) {
	t.Helper()
	b, err := Etichetta(e)
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
	var righe []string
	for _, p := range l.Pagine {
		for _, r := range p.Righe {
			righe = append(righe, r.Testo)
		}
	}
	return l, righe
}

// I dati della busta 5/26 dei dati sintetici del banco, come li dà buste.Etichetta il 03/11/2026.
func etichetta5() buste.DatiEtichetta {
	return buste.DatiEtichetta{ID: 5, Numero: "5/26", QR: "SINT-QR-0005", Oggi: "03/11/2026", Scadenza: "fine mese",
		Stato: "in lavorazione", Descrizione: "Lavoro sintetico"}
}

// inventario: RIP-S-43
// L'etichetta è una pagina di 76×127 mm con, nell'ordine dello standalone: «→ ARTIGIANO» (maiuscolo,
// text-transform: uppercase), «#<numero>», «Consegna: <oggi>» e «Scad.: …», lo stato in maiuscolo nel
// riquadro nero, la descrizione, « FATTO» e «Note:»; il QR contiene il valore dato. Senza artigiano e
// senza scadenza quelle righe mancano.
func TestEtichettaContenutiEOrdine(t *testing.T) {
	e := etichetta5()
	e.Artigiano = "Bulino & C. <b>"
	e.Descrizione = "Riparazione d'oro & pietre"
	l, righe := etichetta(t, e)
	if lettura.Formato(l) != "76x127" || len(l.Pagine) != 1 {
		t.Fatalf("formato %q", lettura.Formato(l))
	}
	uguali(t, "righe", righe, []string{"→ BULINO & C. <B>", "#5/26", "Consegna: 03/11/2026", "Scad.: fine mese", "IN LAVORAZIONE",
		"Riparazione d'oro & pietre", " FATTO", "Note:"})
	qq, err := lettura.CodiciQR(l.Pagine[0])
	if err != nil || len(qq) != 1 || qq[0].Testo != "SINT-QR-0005" || qq[0].Specchiato {
		t.Errorf("QR %v %+v", err, qq)
	}
	// Le tre righe per le note, i filetti di intestazione e descrizione e il fondo del riquadro dello
	// stato; la casella FATTO è l'unico contorno.
	if c := l.Pagine[0].Contorni; len(c) != 1 {
		t.Errorf("contorni %+v", c)
	}
	e.Artigiano, e.Scadenza = "", ""
	_, righe = etichetta(t, e)
	uguali(t, "senza", righe, []string{"#5/26", "Consegna: 03/11/2026", "IN LAVORAZIONE", "Riparazione d'oro & pietre", " FATTO", "Note:"})
}

// inventario: RIP-S-43
// Una descrizione lunga (già tagliata dal dominio) va a capo dentro i 70 mm, spezzando anche una
// parola troppo lunga (word-wrap: break-word); un nome d'artigiano lungo va a capo nei 46 mm accanto
// al QR e l'intestazione si allunga; una scadenza lunga va a capo nella sua parte della riga .meta.
// Le righe restano nella pagina.
func TestEtichettaTestiLunghi(t *testing.T) {
	e := etichetta5()
	e.Descrizione = buste.DescrizioneEtichetta(strings.Repeat("parola ", 40) + strings.Repeat("x", 120))
	e.Artigiano = strings.Repeat("Laboratorio orafo ", 6)
	e.Scadenza = "entro la fine del mese di dicembre, dopo il rientro dal laboratorio " + strings.Repeat("s", 90)
	l, righe := etichetta(t, e)
	if len(l.Pagine) != 1 {
		t.Fatalf("pagine %d", len(l.Pagine))
	}
	if got, atteso := lettura.Normalizza(strings.Join(righe, " ")), lettura.Normalizza("→ "+strings.ToUpper(e.Artigiano)); !strings.HasPrefix(got, atteso) {
		t.Errorf("artigiano: %q", got)
	}
	for _, r := range l.Pagine[0].Righe {
		if r.X < 2.9 || r.Y > 127 {
			t.Errorf("riga fuori: %+v", r)
		}
	}
	if !strings.HasSuffix(strings.Join(righe, ""), "…"+" FATTO"+"Note:") {
		t.Error("taglio perso")
	}
	// La scadenza lunga va a capo nella sua parte della riga; la parola più larga della pagina resta
	// intera (esce dal bordo, come in Chromium), quindi il testo è tutto.
	if got := lettura.TestoNormalizzato(l); !strings.Contains(got, "Consegna: 03/11/2026 Scad.: "+e.Scadenza+" IN LAVORAZIONE") {
		t.Errorf("scadenza: %q", got)
	}
}

// inventario: RIP-S-43
// D-24 senza banco: l'etichetta del Go per i dati delle buste 5/26 (codice QR, scadenza a testo), 4/26
// (artigiano su due righe, QR = numero busta) e 1/26 (QR = numero busta) ha lo stesso formato, lo
// stesso testo normalizzato, lo stesso contenuto del QR e le posizioni chiave entro ±0,5 mm del PDF
// che Chromium ha fatto nello standalone (fab-220, internal/pdf/lettura/testdata).
func TestEtichettaComeQuellaDiChromium(t *testing.T) {
	e4 := etichetta5()
	e4.ID, e4.Numero, e4.QR, e4.Scadenza, e4.Descrizione, e4.Artigiano = 4, "4/26", "4/26", "", "Anello sintetico", "Laboratorio Sintetico"
	e1 := e4
	e1.ID, e1.Numero, e1.QR, e1.Artigiano = 1, "1/26", "1/26", ""
	for file, e := range map[string]buste.DatiEtichetta{
		"chromium-etichetta-5.pdf": etichetta5(), "chromium-etichetta-4.pdf": e4, "chromium-etichetta-1.pdf": e1,
	} {
		b, err := os.ReadFile("../pdf/lettura/testdata/" + file)
		if err != nil {
			t.Fatal(err)
		}
		chromium, err := lettura.Leggi(b)
		if err != nil {
			t.Fatal(err)
		}
		l, _ := etichetta(t, e)
		if lettura.Formato(l) != lettura.Formato(chromium) {
			t.Errorf("%s: formato %q, Chromium %q", file, lettura.Formato(l), lettura.Formato(chromium))
		}
		if got, atteso := lettura.TestoNormalizzato(l), lettura.TestoNormalizzato(chromium); got != atteso {
			t.Errorf("%s: testo:\n%q\nChromium:\n%q", file, got, atteso)
		}
		qg, err1 := lettura.CodiciQR(l.Pagine[0])
		qc, err2 := lettura.CodiciQR(chromium.Pagine[0])
		if err1 != nil || err2 != nil || len(qg) != 1 || len(qc) != 1 || qg[0].Testo != qc[0].Testo {
			t.Errorf("%s: QR %v %v %+v %+v", file, err1, err2, qg, qc)
		}
		pg, err1 := lettura.PosizioniEtichetta(l.Pagine[0])
		pc, err2 := lettura.PosizioniEtichetta(chromium.Pagine[0])
		if err1 != nil || err2 != nil || !lettura.PosizioniVicine(lettura.TestoPosizioni(pg), lettura.TestoPosizioni(pc), 0.5) {
			t.Errorf("%s: posizioni\n%s\nChromium\n%s", file, lettura.TestoPosizioni(pg), lettura.TestoPosizioni(pc))
		}
		// Anche le altre righe stanno dove le mette Chromium (non è una regola del banco: qui i testi
		// sono gli stessi).
		if len(l.Pagine[0].Righe) != len(chromium.Pagine[0].Righe) {
			t.Errorf("%s: %d righe, Chromium %d", file, len(l.Pagine[0].Righe), len(chromium.Pagine[0].Righe))
		} else {
			for i, r := range l.Pagine[0].Righe {
				c := chromium.Pagine[0].Righe[i]
				if !vicinoMM(r.Y, c.Y, 0.5) || !vicinoMM(r.X, c.X, 0.5) {
					t.Errorf("%s: riga %q a %.2f,%.2f, Chromium %.2f,%.2f", file, r.Testo, r.X, r.Y, c.X, c.Y)
				}
			}
		}
	}
}

func vicinoMM(a, b, t float64) bool { return a-b <= t && b-a <= t }

// inventario: RIP-S-43
// Il layout serve anche il foglio della consegna all'artigiano (T4.8): PaginaEtichetta disegna
// un'etichetta sulla pagina corrente di un documento 76×127, una pagina per busta, con il nome
// dell'artigiano che dà chi chiama.
func TestPaginaEtichettaSuPiuPagine(t *testing.T) {
	d, err := pdf.Nuovo(LarghezzaEtichetta, AltezzaEtichetta)
	if err != nil {
		t.Fatal(err)
	}
	for i, n := range []string{"1/26", "2/26", "3/26"} {
		if i > 0 {
			d.Pagina()
		}
		e := etichetta5()
		e.Numero, e.QR, e.Artigiano = n, n, "Cesellatore"
		if err := PaginaEtichetta(d, e); err != nil {
			t.Fatal(err)
		}
	}
	b, err := d.Byte()
	if err != nil {
		t.Fatal(err)
	}
	l, err := lettura.Leggi(b)
	if err != nil {
		t.Fatal(err)
	}
	if lettura.Formato(l) != "76x127,76x127,76x127" {
		t.Fatalf("%q", lettura.Formato(l))
	}
	for i, n := range []string{"1/26", "2/26", "3/26"} {
		qq, err := lettura.CodiciQR(l.Pagine[i])
		if err != nil || len(qq) != 1 || qq[0].Testo != n || l.Pagine[i].Righe[1].Testo != "#"+n {
			t.Errorf("pagina %d: %v %+v", i, err, qq)
		}
	}
}

// inventario: RIP-S-43
// Le tre righe per le note: filetti di 1 px alla base di righe alte 6 mm, con 0,8 mm fra l'una e
// l'altra (6,8 mm da un filetto al successivo), larghe quanto il blocco di 70 mm.
func TestRigheDelleNote(t *testing.T) {
	l, _ := etichetta(t, etichetta5())
	var filetti []lettura.Rettangolo
	for _, r := range l.Pagine[0].Rettangoli {
		if vicinoMM(r.L, 70, 0.01) && vicinoMM(r.A, px, 0.001) && r.Y > 60 {
			filetti = append(filetti, r)
		}
	}
	if len(filetti) != 3 || !vicinoMM(filetti[1].Y-filetti[0].Y, 6.8, 0.005) || !vicinoMM(filetti[2].Y-filetti[1].Y, 6.8, 0.005) {
		t.Errorf("filetti %+v", filetti)
	}
}
