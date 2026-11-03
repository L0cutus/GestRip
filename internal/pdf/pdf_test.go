package pdf

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/boombuler/barcode/qr"

	"github.com/L0cutus/gestrip/internal/pdf/lettura"
)

func vicino(a, b, tolleranza float64) bool { return math.Abs(a-b) <= tolleranza }

func nuovo(t *testing.T, l, a float64) *Documento {
	t.Helper()
	d, err := Nuovo(l, a)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func leggi(t *testing.T, d *Documento) (*lettura.Documento, []byte) {
	t.Helper()
	b, err := d.Byte()
	if err != nil {
		t.Fatal(err)
	}
	l, err := lettura.Leggi(b)
	if err != nil {
		t.Fatal(err)
	}
	return l, b
}

func testi(p lettura.Pagina) []string {
	var out []string
	for _, r := range p.Righe {
		out = append(out, r.Testo)
	}
	return out
}

// inventario: RIP-S-41
// Il formato della pagina è quello chiesto, in millimetri esatti: 80×297 mm (la ricevuta) e 57×32 mm
// (le etichette, 161,57×90,71 pt), anche su più pagine.
func TestFormatoDellaPagina(t *testing.T) {
	for _, c := range []struct{ l, a, lpt, apt float64 }{{80, 297, 226.7717, 841.8898}, {57, 32, 161.5748, 90.7087}} {
		d := nuovo(t, c.l, c.a)
		d.Pagina()
		l, b := leggi(t, d)
		if len(l.Pagine) != 2 {
			t.Fatalf("%v×%v: pagine %d", c.l, c.a, len(l.Pagine))
		}
		for _, p := range l.Pagine {
			// gopdf scrive il MediaBox in pt con due decimali: 0,01 pt è meno di 0,004 mm.
			if !vicino(p.Larghezza, c.l, 0.004) || !vicino(p.Altezza, c.a, 0.004) {
				t.Errorf("%v×%v: %.4f×%.4f mm", c.l, c.a, p.Larghezza, p.Altezza)
			}
		}
		if !bytes.Contains(b, []byte(fmt.Sprintf("/MediaBox [ 0 0 %.2f %.2f ]", c.lpt, c.apt))) {
			t.Errorf("%v×%v: MediaBox in pt diverso da %.2f×%.2f", c.l, c.a, c.lpt, c.apt)
		}
	}
	if _, err := Nuovo(0, 10); err == nil {
		t.Error("formato vuoto accettato")
	}
}

// inventario: RIP-S-41
// Il testo esce con «€», accenti e virgolette così come sono scritti, in DejaVu Sans normale e
// grassetto incorporato (sottoinsieme TrueType), con corpo e posizione della linea di base chiesti;
// nessun font standard del PDF.
func TestTestoConEuroEAccenti(t *testing.T) {
	d := nuovo(t, 80, 297)
	frase := "Prezzo finale: € 12,50 — àèéìòù ÀÈÉÌÒÙ «così» ‘a’ “b”"
	if _, _, err := d.Testo(Casella{X: 5, Y: 10, Larghezza: 200, Testo: frase, Corpo: 10}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.Testo(Casella{X: 5, Y: 20, Larghezza: 70, Testo: "Grassetto è", Corpo: 18, Grassetto: true}); err != nil {
		t.Fatal(err)
	}
	l, b := leggi(t, d)
	p := l.Pagine[0]
	if got := testi(p); len(got) != 2 || got[0] != frase || got[1] != "Grassetto è" {
		t.Fatalf("testo %q", got)
	}
	// La linea di base: sotto il bordo alto della casella di ascesa × corpo (interlinea normale).
	if r := p.Righe[0]; !vicino(r.X, 5, 0.01) || !vicino(r.Y, 10+Ascesa*10*mmPerPt, 0.01) || !vicino(r.Corpo, 10, 0.01) {
		t.Errorf("prima riga %+v", r)
	}
	if r := p.Righe[1]; !vicino(r.Corpo, 18, 0.01) {
		t.Errorf("seconda riga %+v", r)
	}
	for _, s := range []string{"/BaseFont /DejaVuSans\n", "/BaseFont /DejaVuSans-Bold\n", "/FontFile2"} {
		if !bytes.Contains(b, []byte(s)) {
			t.Errorf("manca %s nel PDF", s)
		}
	}
	if bytes.Contains(b, []byte("Helvetica")) {
		t.Error("un font standard nel PDF")
	}
}

// inventario: RIP-S-41
// Un carattere che DejaVu Sans non ha esce come «�» (U+FFFD), non sparisce in silenzio; i caratteri
// di controllo non si stampano.
func TestCarattereCheIlFontNonHa(t *testing.T) {
	d := nuovo(t, 80, 297)
	if _, _, err := d.Testo(Casella{X: 1, Y: 1, Larghezza: 70, Testo: "a😀b\x00\x07c漢", Corpo: 10}); err != nil {
		t.Fatal(err)
	}
	l, _ := leggi(t, d)
	if got := testi(l.Pagine[0]); len(got) != 1 || got[0] != "a\uFFFDbc\uFFFD" {
		t.Errorf("testo %q", got)
	}
}

// inventario: RIP-S-41
// La casella va a capo sulle parole, senza superare la sua larghezza; ogni riga scende di
// un'interlinea; con l'interlinea 1,45 (il p della ricevuta) le righe sono più distanti. L'altezza
// restituita è righe × interlinea.
func TestACapoSulleParole(t *testing.T) {
	d := nuovo(t, 80, 297)
	frase := "Riparazione collana d'oro con chiusura nuova e pulizia completa"
	c := Casella{X: 4, Y: 30, Larghezza: 30, Testo: frase, Corpo: 10}
	alt, tagliato, err := d.Testo(c)
	if err != nil || tagliato {
		t.Fatal(err, tagliato)
	}
	c.Y, c.Interlinea = 150, 1.45
	alt145, _, err := d.Testo(c)
	if err != nil {
		t.Fatal(err)
	}
	l, _ := leggi(t, d)
	p := l.Pagine[0]
	righe := len(p.Righe) / 2
	if righe < 3 || len(p.Righe) != 2*righe {
		t.Fatalf("righe %q", testi(p))
	}
	normale := (Ascesa + Discesa) * 10 * mmPerPt
	if !vicino(alt, float64(righe)*normale, 0.001) || !vicino(alt145, float64(righe)*1.45*10*mmPerPt, 0.001) {
		t.Errorf("altezze %.3f %.3f su %d righe", alt, alt145, righe)
	}
	if got := lettura.Normalizza(strings.Join(testi(p)[:righe], " ")); got != frase {
		t.Errorf("testo %q", got)
	}
	for i, r := range p.Righe[:righe] {
		if !vicino(r.X, 4, 0.01) || !vicino(r.Y, 30+Ascesa*10*mmPerPt+float64(i)*normale, 0.01) {
			t.Errorf("riga %d a %.3f,%.3f", i, r.X, r.Y)
		}
		w, err := d.Larghezza(r.Testo, 10, false)
		if err != nil || w > 30+1e-6 {
			t.Errorf("riga %q larga %.3f mm", r.Testo, w)
		}
		if strings.HasPrefix(r.Testo, " ") || strings.HasSuffix(r.Testo, " ") {
			t.Errorf("riga %q con spazi ai bordi", r.Testo)
		}
	}
	if s := p.Righe[righe]; !vicino(s.Y, 150+(1.45*10-(Ascesa+Discesa)*10)/2*mmPerPt+Ascesa*10*mmPerPt, 0.01) {
		t.Errorf("prima riga con interlinea 1,45 a %.3f", s.Y)
	}
}

// inventario: RIP-S-41
// Spazi come l'HTML (white-space: normal): ogni sequenza di spazi e a capo è uno spazio, niente ai
// bordi. Spazi conservati (pre-wrap, il p della ricevuta): gli a capo restano, anche le righe vuote,
// e gli spazi dentro la riga.
func TestSpaziCollassatiEConservati(t *testing.T) {
	d := nuovo(t, 80, 297)
	testo := "  Prima\r\n\r\nseconda   riga\rterza\t "
	altC, _, err := d.Testo(Casella{X: 1, Y: 1, Larghezza: 75, Testo: testo, Corpo: 10})
	if err != nil {
		t.Fatal(err)
	}
	altP, _, err := d.Testo(Casella{X: 1, Y: 100, Larghezza: 75, Testo: testo, Corpo: 10, Spazi: Conservati})
	if err != nil {
		t.Fatal(err)
	}
	l, _ := leggi(t, d)
	got := testi(l.Pagine[0])
	atteso := []string{"Prima seconda riga terza", "  Prima", "seconda   riga", "terza"}
	if strings.Join(got, "|") != strings.Join(atteso, "|") {
		t.Errorf("righe %q", got)
	}
	riga := (Ascesa + Discesa) * 10 * mmPerPt
	if !vicino(altC, riga, 0.001) || !vicino(altP, 4*riga, 0.001) {
		t.Errorf("altezze %.3f %.3f", altC, altP)
	}
	// La riga vuota occupa il suo posto: «seconda» scende di due righe sotto «Prima».
	if !vicino(l.Pagine[0].Righe[2].Y-l.Pagine[0].Righe[1].Y, 2*riga, 0.01) {
		t.Errorf("riga vuota non contata")
	}
}

// inventario: RIP-S-41
// Una parola più larga della casella: con SpezzaParole (overflow-wrap: anywhere) si spezza dove serve,
// ogni pezzo nella larghezza; senza, resta intera e sborda (come l'HTML senza a capo).
func TestParolaLunga(t *testing.T) {
	d := nuovo(t, 80, 297)
	parola := strings.Repeat("Supercalifragilistico", 3)
	if _, _, err := d.Testo(Casella{X: 1, Y: 1, Larghezza: 20, Testo: "a " + parola, Corpo: 10, SpezzaParole: true}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.Testo(Casella{X: 1, Y: 200, Larghezza: 20, Testo: "a " + parola, Corpo: 10}); err != nil {
		t.Fatal(err)
	}
	l, _ := leggi(t, d)
	got := testi(l.Pagine[0])
	if len(got) < 5 || got[0] != "a" || got[len(got)-2] != "a" || got[len(got)-1] != parola {
		t.Fatalf("righe %q", got)
	}
	if strings.Join(got[1:len(got)-2], "") != parola {
		t.Errorf("pezzi %q", got[1:len(got)-2])
	}
	for _, s := range got[1 : len(got)-2] {
		if w, _ := d.Larghezza(s, 10, false); w > 20+1e-6 {
			t.Errorf("pezzo %q largo %.3f", s, w)
		}
	}
}

// inventario: RIP-S-41
// Il taglio: con MaxRighe la casella scrive al più quelle righe e dice che ha tagliato.
func TestTaglio(t *testing.T) {
	d := nuovo(t, 76, 127)
	alt, tagliato, err := d.Testo(Casella{X: 3, Y: 3, Larghezza: 20, Testo: "uno due tre quattro cinque sei sette otto", Corpo: 10, MaxRighe: 2})
	if err != nil || !tagliato {
		t.Fatal(err, tagliato)
	}
	l, _ := leggi(t, d)
	if n := len(l.Pagine[0].Righe); n != 2 || !vicino(alt, 2*(Ascesa+Discesa)*10*mmPerPt, 0.001) {
		t.Errorf("righe %d, altezza %.3f", n, alt)
	}
	// Restano le prime righe, quelle della casella senza taglio.
	tutte, _, err := d.Righe(Casella{Larghezza: 20, Testo: "uno due tre quattro cinque sei sette otto", Corpo: 10})
	if err != nil || len(tutte) <= 2 {
		t.Fatal(err, tutte)
	}
	if got := testi(l.Pagine[0]); strings.Join(got, "|") != strings.Join(tutte[:2], "|") {
		t.Errorf("righe rimaste %q, attese %q", got, tutte[:2])
	}
	_, tagliato, err = d.Testo(Casella{X: 3, Y: 50, Larghezza: 70, Testo: "corto", Corpo: 10, MaxRighe: 2})
	if err != nil || tagliato {
		t.Error("tagliato senza motivo", err)
	}
}

// inventario: RIP-S-41
// Allineamento al centro e a destra dentro la casella.
func TestAllineamento(t *testing.T) {
	d := nuovo(t, 80, 297)
	for i, a := range []Allineamento{Centro, Destra} {
		if _, _, err := d.Testo(Casella{X: 10, Y: float64(10 + 20*i), Larghezza: 60, Testo: "Conservare", Corpo: 8, Allinea: a}); err != nil {
			t.Fatal(err)
		}
	}
	w, err := d.Larghezza("Conservare", 8, false)
	if err != nil {
		t.Fatal(err)
	}
	l, _ := leggi(t, d)
	r := l.Pagine[0].Righe
	if !vicino(r[0].X, 10+(60-w)/2, 0.01) || !vicino(r[1].X, 70-w, 0.01) {
		t.Errorf("x %.3f %.3f (larghezza %.3f)", r[0].X, r[1].X, w)
	}
}

// inventario: RIP-S-41
// Rettangoli pieni (i filetti della ricevuta) e riquadri: le misure lette tornano in mm.
func TestRettangoliERiquadri(t *testing.T) {
	d := nuovo(t, 80, 297)
	d.Rettangolo(0, 21.7, 80, 0.53, Colore{23, 20, 15})
	d.Riquadro(5, 50, 20, 10, 0.3, Nero)
	l, _ := leggi(t, d)
	rr := l.Pagine[0].Rettangoli
	if len(rr) != 1 || !vicino(rr[0].X, 0, 0.01) || !vicino(rr[0].Y, 21.7, 0.01) || !vicino(rr[0].L, 80, 0.01) || !vicino(rr[0].A, 0.53, 0.01) {
		t.Errorf("rettangoli %+v", rr)
	}
	cc := l.Pagine[0].Contorni
	if len(cc) != 1 || !vicino(cc[0].X, 5, 0.01) || !vicino(cc[0].Y, 50, 0.01) || !vicino(cc[0].L, 20, 0.01) || !vicino(cc[0].A, 10, 0.01) {
		t.Errorf("riquadri %+v", cc)
	}
}

// inventario: RIP-S-41
// Con gli spazi conservati, una riga di soli spazi seguita da una parola che da sola sta nella
// casella: la riga di spazi si chiude e la parola va intera nella riga dopo, come pre-wrap di
// Chromium (prima: la parola si spezzava). Una tabulazione fra due parole è uno spazio.
func TestSpaziInTestaEtabulazione(t *testing.T) {
	d := nuovo(t, 80, 297)
	parola := strings.Repeat("a", 30)
	w, err := d.Larghezza(parola, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	c := Casella{Larghezza: w + 1, Testo: strings.Repeat(" ", 16) + parola, Corpo: 10, Spazi: Conservati, SpezzaParole: true}
	righe, _, err := d.Righe(c)
	if err != nil || strings.Join(righe, "|") != "|"+parola {
		t.Errorf("righe %q", righe)
	}
	for _, s := range []Spazi{Collassati, Conservati} {
		righe, _, err := d.Righe(Casella{Larghezza: 70, Testo: "uno\tdue", Corpo: 10, Spazi: s})
		if err != nil || len(righe) != 1 || righe[0] != "uno due" {
			t.Errorf("tabulazione (%d): %q", s, righe)
		}
	}
}

// inventario: RIP-S-41
// Il QR è vettoriale: un rettangolo per ogni tratto di moduli scuri di una riga, nel quadrato chiesto,
// con il margine di 4 moduli dentro il lato (come qrcode.make e la LabelSheet dello standalone),
// correzione M come QRCode.create(:medium). I moduli letti dal PDF sono quelli del codice.
func TestQRVettoriale(t *testing.T) {
	d := nuovo(t, 57, 32)
	if err := d.QR(10, 2, 28, "000123"); err != nil {
		t.Fatal(err)
	}
	l, _ := leggi(t, d)
	codice, err := qr.Encode("000123", qr.M, qr.Unicode)
	if err != nil {
		t.Fatal(err)
	}
	n := codice.Bounds().Dx()
	modulo := 28 / float64(n+8)
	letti := make([][]bool, n)
	for i := range letti {
		letti[i] = make([]bool, n)
	}
	for _, r := range l.Pagine[0].Rettangoli {
		riga := int(math.Round((r.Y-2)/modulo)) - 4
		col := int(math.Round((r.X-10)/modulo)) - 4
		lung := int(math.Round(r.L / modulo))
		if !vicino(r.A, modulo, 0.005) || riga < 0 || riga >= n || col < 0 || col+lung > n {
			t.Fatalf("rettangolo fuori dal codice: %+v", r)
		}
		for k := range lung {
			letti[riga][col+k] = true
		}
	}
	for y := range n {
		for x := range n {
			scuro := codice.At(x, y) == color.Black
			if letti[y][x] != scuro {
				t.Fatalf("modulo %d,%d diverso", x, y)
			}
		}
	}
	if err := d.QR(1, 1, 10, ""); err == nil {
		t.Error("QR vuoto accettato")
	}
	// In byte come lo standalone: 19 caratteri che in alfanumerico starebbero nella versione 1 (21
	// moduli) vanno nella 2 (25), e il testo si rilegge.
	d = nuovo(t, 76, 127)
	if err := d.QR(51, 3, 22, "RIP-2026-0042-ABCDE"); err != nil {
		t.Fatal(err)
	}
	l, _ = leggi(t, d)
	if qq, err := lettura.CodiciQR(l.Pagine[0]); err != nil || len(qq) != 1 || qq[0].Moduli != 25 || qq[0].Modo != "byte" ||
		qq[0].Testo != "RIP-2026-0042-ABCDE" {
		t.Errorf("modo byte: %v %+v", err, qq)
	}
}

// inventario: RIP-S-41
// Un PDF del motore passa Controlla; un PDF guasto no (così non si consegna). Lo stesso documento
// dà sempre gli stessi byte: niente date né nomi casuali.
func TestControllaEByteStabili(t *testing.T) {
	fai := func() []byte {
		d := nuovo(t, 80, 297)
		if _, _, err := d.Testo(Casella{X: 1, Y: 1, Larghezza: 70, Testo: "Busta 1/26", Corpo: 20, Grassetto: true}); err != nil {
			t.Fatal(err)
		}
		b, err := d.Byte()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	a, b := fai(), fai()
	if !bytes.Equal(a, b) {
		t.Error("byte diversi per lo stesso documento")
	}
	if err := Controlla(a); err != nil {
		t.Errorf("il PDF del motore non passa: %v", err)
	}
	for _, guasto := range [][]byte{nil, []byte("%PDF-1.7 test"), a[:len(a)-200], bytes.Replace(a, []byte("endstream"), []byte("endstrean"), 1)} {
		if err := Controlla(guasto); err == nil {
			t.Errorf("PDF guasto accettato (%d byte)", len(guasto))
		}
	}
}

// I font incorporati sono i file di DejaVu Sans 2.37 dell'immagine dello standalone, con la licenza
// accanto (caratteri/LICENSE): cambiarli è una scelta, non un incidente.
func TestFontIncorporati(t *testing.T) {
	for nome, impronta := range map[string]string{
		"caratteri/DejaVuSans.ttf":      "abdc775b21b1bc470d50c97e790d276f2054b7504e56e5bd3e64f48d68582322",
		"caratteri/DejaVuSans-Bold.ttf": "0d977336a6d5fba34eab8e3199eb218327161b5143749f802982c2bc34df0c96",
	} {
		b, err := caratteri.ReadFile(nome)
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != impronta {
			t.Errorf("%s cambiato", nome)
		}
	}
	lic, err := os.ReadFile("caratteri/LICENSE")
	if err != nil || !bytes.Contains(lic, []byte("Bitstream Vera")) || !bytes.Contains(lic, []byte("Arev Fonts Copyright")) {
		t.Errorf("licenza dei font: %v", err)
	}
}

// pngDiProva è un PNG RGBA w×h con il bordo nero e il centro trasparente, come una firma.
func pngDiProva(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		img.Set(x, 0, color.Black)
		img.Set(x, h-1, color.Black)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// inventario: RIP-S-45
// Un'immagine PNG (la firma della consegna sulla scheda) si disegna nel riquadro chiesto, con la
// trasparenza; il PDF si rilegge e il lettore la trova lì. Le dimensioni del PNG si leggono senza
// disegnarlo. Un PNG che non si decodifica è un errore e non disegna niente; lo stesso documento dà
// gli stessi byte.
func TestImmaginePNG(t *testing.T) {
	firma := pngDiProva(t, 300, 120)
	if w, h, err := DimensioniPNG(firma); err != nil || w != 300 || h != 120 {
		t.Fatalf("dimensioni %d×%d %v", w, h, err)
	}
	fai := func() []byte {
		d := nuovo(t, 100, 230)
		if err := d.Immagine(5, 100, 62.5, 25, firma); err != nil {
			t.Fatal(err)
		}
		_, b := leggi(t, d)
		return b
	}
	b := fai()
	if !bytes.Equal(b, fai()) {
		t.Error("byte diversi fra due giri")
	}
	l, err := lettura.Leggi(b)
	if err != nil {
		t.Fatal(err)
	}
	im := l.Pagine[0].Immagini
	if len(im) != 1 || !vicino(im[0].X, 5, 0.01) || !vicino(im[0].Y, 100, 0.01) || !vicino(im[0].L, 62.5, 0.01) || !vicino(im[0].A, 25, 0.01) {
		t.Errorf("immagini %+v", im)
	}
	if !bytes.Contains(b, []byte("/SMask")) {
		t.Error("trasparenza persa")
	}
	d := nuovo(t, 100, 230)
	finto := append([]byte("\x89PNG\r\n\x1a\n"), "synthetic-signature"...)
	if err := d.Immagine(5, 100, 10, 10, finto); err == nil {
		t.Error("PNG finto disegnato")
	}
	if _, _, err := DimensioniPNG(finto); err == nil {
		t.Error("dimensioni di un PNG finto")
	}
	l, _ = leggi(t, d)
	if len(l.Pagine[0].Immagini) != 0 {
		t.Error("immagine disegnata dopo l'errore")
	}
}

// inventario: RIP-S-43
// La spaziatura fra le lettere (letter-spacing del CSS, in pt) allarga ogni carattere e conta per
// l'a capo e per l'allineamento; dopo, una casella senza spaziatura torna stretta.
func TestSpaziaturaDelleLettere(t *testing.T) {
	d := nuovo(t, 76, 127)
	senza, err := d.Larghezza("LABORATORIO", 9, false)
	if err != nil {
		t.Fatal(err)
	}
	con := Casella{Larghezza: 46, Testo: "→ LABORATORIO SINTETICO", Corpo: 9, Spaziatura: 0.5}
	w, err := d.LarghezzaCasella(Casella{Testo: "LABORATORIO", Corpo: 9, Spaziatura: 0.5})
	if err != nil || !vicino(w-senza, 11*0.5*25.4/72, 0.02) {
		t.Errorf("spaziatura: %v %v (%v)", w, senza, err)
	}
	righe, _, err := d.Righe(con)
	if err != nil || strings.Join(righe, "|") != "→ LABORATORIO|SINTETICO" {
		t.Errorf("righe %q %v", righe, err)
	}
	con.Spaziatura = 0
	if righe, _, _ := d.Righe(con); len(righe) != 1 {
		t.Errorf("senza spaziatura %q", righe)
	}
	if w2, _ := d.Larghezza("LABORATORIO", 9, false); !vicino(w2, senza, 1e-9) {
		t.Errorf("la spaziatura resta: %v %v", w2, senza)
	}
	c := Casella{X: 3, Y: 10, Larghezza: 46, Testo: "#5/26", Corpo: 9, Spaziatura: 0.5, Allinea: Centro}
	if _, _, err := d.Testo(c); err != nil {
		t.Fatal(err)
	}
	l, _ := leggi(t, d)
	wc, _ := d.LarghezzaCasella(c)
	if r := l.Pagine[0].Righe[0]; r.Testo != "#5/26" || !vicino(r.X, 3+(46-wc)/2, 0.01) {
		t.Errorf("riga %+v", r)
	}
}

// inventario: RIP-S-45
// Oltre MaxPixelPNG (16 milioni di pixel) un PNG non si disegna e non se ne danno le misure, anche se è
// valido; un'immagine con un lato nullo o negativo è un errore; una spaziatura negativa fra le lettere
// è un errore (nessun documento dello standalone la usa: meglio fermarsi che indovinare).
func TestLimitiDelMotore(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 4001, 4000))
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	d := nuovo(t, 100, 230)
	if _, _, err := DimensioniPNG(b.Bytes()); err == nil {
		t.Error("misure oltre il limite")
	}
	if _, err := LeggiPNG(b.Bytes()); err == nil {
		t.Error("letto oltre il limite")
	}
	if err := d.Immagine(5, 5, 10, 10, b.Bytes()); err == nil {
		t.Error("disegnato oltre il limite")
	}
	firma := pngDiProva(t, 30, 10)
	for _, m := range [][2]float64{{10, 0}, {0, 10}, {-1, 10}} {
		if err := d.Immagine(5, 5, m[0], m[1], firma); err == nil {
			t.Errorf("immagine %v accettata", m)
		}
	}
	if _, err := d.LarghezzaCasella(Casella{Testo: "x", Corpo: 9, Spaziatura: -0.5}); err == nil {
		t.Error("spaziatura negativa accettata")
	}
	l, _ := leggi(t, d)
	if len(l.Pagine[0].Immagini) != 0 {
		t.Error("immagine disegnata")
	}
}
