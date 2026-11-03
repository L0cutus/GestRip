package lettura

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
)

// La ricevuta della busta 5/26 dei dati sintetici del banco, scaricata dallo standalone
// (gestrip-standalone:fab-220, ChromicPDF con Chromium 156, Skia/PDF m156) il 03/11/2026: il
// lettore deve leggere i PDF di Chromium oltre ai nostri, perché il banco li confronta (D-24).
func chromium(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/chromium-ricevuta-5.pdf")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func vicino(a, b, tolleranza float64) bool { return math.Abs(a-b) <= tolleranza }

// Il testo e il formato della ricevuta di Chromium: una pagina di 80×297 mm (227,04×841,92 pt), i
// pezzi di testo nell'ordine del contenuto (la legatura «fi» di Chromium resta nel testo grezzo e si
// scioglie con Normalizza).
func TestLeggeLaRicevutaDiChromium(t *testing.T) {
	d, err := Leggi(chromium(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Pagine) != 1 {
		t.Fatalf("pagine: %d", len(d.Pagine))
	}
	p := d.Pagine[0]
	if !vicino(p.Larghezza, 80.095, 0.01) || !vicino(p.Altezza, 297.01, 0.01) {
		t.Errorf("formato %.3f×%.3f mm", p.Larghezza, p.Altezza)
	}
	if got := Formato(d); got != "80x297" {
		t.Errorf("Formato: %q", got)
	}
	attese := []string{
		"Negozio", "Busta 5/26", "CLIENTE", "Cliente", "<Sintetico> & 5", "TELEFONO", `+39 000 <5> & "5"`,
		"ACCETTATA", "2026-03-02", "PREVISTA", "fine mese", "LAVORAZIONE", "Lavoro sintetico", "NOTE",
		"Prima riga", `Seconda <b>riga</b> & "virgolette"`, "IMPORTI", "Preventivo: 400/500",
		"Prezzo finale: da definire", "Conservare questa ricevuta per il ritiro.",
	}
	var visti []string
	for _, r := range p.Righe {
		visti = append(visti, Normalizza(r.Testo))
	}
	if strings.Join(visti, "|") != strings.Join(attese, "|") {
		t.Errorf("righe:\n%q\nattese:\n%q", visti, attese)
	}
	if got := TestoNormalizzato(d); got != strings.Join(attese, " ") {
		t.Errorf("testo normalizzato: %q", got)
	}
	// Il nome del negozio: 18 pt in grassetto, la linea di base a 22 px (5,82 mm) dall'alto, a sinistra.
	n := p.Righe[0]
	if !vicino(n.X, 0, 0.01) || !vicino(n.Y, 22*25.4/96, 0.01) || !vicino(n.Corpo, 18, 0.01) {
		t.Errorf("nome del negozio a %.3f,%.3f corpo %.3f", n.X, n.Y, n.Corpo)
	}
	// Il filetto sotto l'intestazione: un rettangolo pieno di tutta la larghezza, alto 2 px.
	trovato := false
	for _, r := range p.Rettangoli {
		if vicino(r.Y, 82*25.4/96, 0.01) && vicino(r.A, 2*25.4/96, 0.01) && vicino(r.L, 303*25.4/96, 0.01) {
			trovato = true
		}
	}
	if !trovato {
		t.Errorf("filetto dell'intestazione non trovato: %v", p.Rettangoli)
	}
}

// pdfAMano costruisce un PDF piccolo con gli offset giusti nella xref: oggetti dati come testo
// (stream già scritti), il primo è il catalogo.
func pdfAMano(oggetti ...string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	off := make([]int, len(oggetti))
	for i, o := range oggetti {
		off[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(oggetti)+1)
	for _, o := range off {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(oggetti)+1, x)
	return b.Bytes()
}

func streamDi(contenuto string) string {
	return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(contenuto), contenuto)
}

// Un font semplice senza ToUnicode (Helvetica, WinAnsi): i byte si leggono come WinAnsi, «€» compreso;
// le stringhe letterali con le loro sequenze di escape; un TJ con gli spazi fra i pezzi.
func TestFontSempliceEStringheLetterali(t *testing.T) {
	contenuto := `BT /F1 12 Tf 1 0 0 1 10 800 Tm (Prezzo \(finale\): \200 12,50\\) Tj ET
BT /F1 9 Tf 20 700 Td [(A) -120 (B)] TJ T* (dopo) Tj ET`
	b := pdfAMano(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 /MediaBox [0 0 161.57 90.71] >>",
		"<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		streamDi(contenuto),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	)
	d, err := Leggi(b)
	if err != nil {
		t.Fatal(err)
	}
	p := d.Pagine[0]
	if Formato(d) != "57x32" {
		t.Errorf("formato %q (%.3f×%.3f)", Formato(d), p.Larghezza, p.Altezza)
	}
	var testi []string
	for _, r := range p.Righe {
		testi = append(testi, r.Testo)
	}
	if strings.Join(testi, "|") != `Prezzo (finale): € 12,50\|AB|dopo` {
		t.Errorf("righe %q", testi)
	}
	if !vicino(p.Righe[0].X, 10*25.4/72, 0.001) || !vicino(p.Righe[0].Y, (90.71-800)*25.4/72, 0.001) {
		t.Errorf("posizione %v", p.Righe[0])
	}
}

// Un PDF rovinato non si legge: senza intestazione, senza %%EOF, con la xref che punta fuori da un
// oggetto, con uno stream lungo più di quanto dice /Length, senza pagine, o con un filtro che non
// conosciamo. Così Controlla (nel motore) non lascia passare un PDF guasto.
func TestPDFRovinati(t *testing.T) {
	buono := pdfAMano(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R >>",
		streamDi("0 0 10 10 re f"),
	)
	if _, err := Leggi(buono); err != nil {
		t.Fatalf("il PDF buono: %v", err)
	}
	casi := map[string][]byte{
		"vuoto":                 nil,
		"senza intestazione":    bytes.Replace(buono, []byte("%PDF-1.4"), []byte("%XYZ-1.4"), 1),
		"senza %%EOF":           bytes.TrimSuffix(buono, []byte("%%EOF\n")),
		"troncato":              buono[:len(buono)/2],
		"startxref sbagliato":   bytes.Replace(buono, []byte("startxref\n"), []byte("startxref\n1"), 1),
		"oggetto spostato":      bytes.Replace(buono, []byte("2 0 obj"), []byte("2 0 obi"), 1),
		"stream lungo":          bytes.Replace(buono, []byte("re f\nendstream"), []byte("re f xx\nendstream"), 1),
		"filtro sconosciuto":    bytes.Replace(buono, []byte("<< /Length 14 >>"), []byte("<< /Length 14 /Filter /LZWDecode >>"), 1),
		"flate guasto":          bytes.Replace(buono, []byte("<< /Length 14 >>"), []byte("<< /Length 14 /Filter /FlateDecode >>"), 1),
		"nessuna pagina":        bytes.Replace(buono, []byte("/Kids [3 0 R] /Count 1"), []byte("/Kids [] /Count 0"), 1),
		"pagina senza formato":  bytes.Replace(buono, []byte("/MediaBox [0 0 100 100] "), []byte("                        "), 1),
		"catalogo senza pagine": bytes.Replace(buono, []byte("/Pages 2 0 R"), []byte("/Pagez 2 0 R"), 1),
	}
	for nome, b := range casi {
		if _, err := Leggi(b); err == nil {
			t.Errorf("%s: letto senza errore", nome)
		}
	}
}

// La normalizzazione del confronto (D-24): spazi di ogni tipo ridotti a uno, legature sciolte,
// niente spazi ai bordi; il resto resta com'è (maiuscole, punteggiatura, «€»).
func TestNormalizza(t *testing.T) {
	for in, atteso := range map[string]string{
		"  a\n\tb  ":            "a b",
		"ﬁne mese":              "fine mese",
		"€ 12,50\n\nPrezzo ﬀ ﬂ": "€ 12,50 Prezzo ff fl",
		"":                      "",
	} {
		if got := Normalizza(in); got != atteso {
			t.Errorf("Normalizza(%q) = %q, atteso %q", in, got, atteso)
		}
	}
}
