package lettura

import (
	"fmt"
	"image/color"
	"os"
	"strings"
	"testing"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/qr"
)

// paginaDelCodice disegna il codice come lo disegnerebbe un PDF: un rettangolo pieno per ogni modulo
// scuro (come Chromium con l'SVG dello standalone) o per ogni tratto scuro di una riga (come il motore
// Go), con l'angolo dei moduli in (x, y) e moduli larghi l e alti a mm. Ci aggiunge rettangoli che
// non sono del codice: un filetto e un fondo bianco più grande, come nell'etichetta di Chromium.
func paginaDelCodice(c barcode.Barcode, x, y, l, a float64, tratti bool) Pagina {
	n := c.Bounds().Dx()
	p := Pagina{Larghezza: 76, Altezza: 127, Rettangoli: []Rettangolo{
		{X: 2.9, Y: 25.9, L: 70.1, A: 0.529},
		{X: x - 4*l, Y: y - 4*a, L: float64(n+8) * l, A: float64(n+8) * a},
	}}
	for r := range n {
		for k := 0; k < n; {
			if c.At(k, r) != color.Black {
				k++
				continue
			}
			inizio := k
			for k < n && c.At(k, r) == color.Black {
				k++
				if !tratti {
					break
				}
			}
			p.Rettangoli = append(p.Rettangoli, Rettangolo{X: x + float64(inizio)*l, Y: y + float64(r)*a, L: float64(k-inizio) * l, A: a})
		}
	}
	return p
}

func codiceDiProva(t *testing.T, s string, livello qr.ErrorCorrectionLevel, modo qr.Encoding) barcode.Barcode {
	t.Helper()
	c, err := qr.Encode(s, livello, modo)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// I QR di tutte le versioni (1-40) e di tutti i livelli di correzione, in byte, alfanumerico e numerico,
// si rileggono dai rettangoli con il loro contenuto, disegnati modulo per modulo o a tratti, anche con
// i moduli non quadrati (Chromium arrotonda il riquadro dell'immagine ai px). Il riquadro dei moduli
// letto è quello disegnato.
func TestQRDaiRettangoli(t *testing.T) {
	livelli := map[qr.ErrorCorrectionLevel]string{qr.L: "L", qr.M: "M", qr.Q: "Q", qr.H: "H"}
	for livello, nome := range livelli {
		t.Run(nome, func(t *testing.T) {
			t.Parallel()
			tutteLeVersioni(t, livello)
		})
	}
	for _, c := range []struct {
		s    string
		modo qr.Encoding
	}{{"0123456789012", qr.Numeric}, {"12/26", qr.AlphaNumeric}, {"SINT-QR-0005 $%*+-./:", qr.AlphaNumeric}, {"7", qr.Numeric}} {
		p := paginaDelCodice(codiceDiProva(t, c.s, qr.M, c.modo), 10, 10, 0.5, 0.5, true)
		if letti, err := CodiciQR(p); err != nil || len(letti) != 1 || letti[0].Testo != c.s || letti[0].Specchiato {
			t.Errorf("%q: %v %+v", c.s, err, letti)
		}
	}
}

func tutteLeVersioni(t *testing.T, livello qr.ErrorCorrectionLevel) {
	viste := map[int]bool{}
	for l := 1; l <= 2953; l += l/40 + 1 {
		s := strings.Repeat("rip-è/", l/7+1)[:l]
		if strings.HasSuffix(s, "\xc3") {
			s = s[:len(s)-1] + "x"
		}
		c, err := qr.Encode(s, livello, qr.Unicode)
		if err != nil {
			break // oltre la capacità del livello
		}
		n := c.Bounds().Dx()
		viste[(n-17)/4] = true
		p := paginaDelCodice(c, 53.866, 5.939, 0.766, 0.757, l%2 == 0)
		letti, err := CodiciQR(p)
		if err != nil || len(letti) != 1 || letti[0].Testo != s || letti[0].Specchiato {
			t.Fatalf("%d byte, versione %d: %v %+v", l, (n-17)/4, err, letti)
		}
		q := letti[0]
		if q.Moduli != n || !vicino(q.X, 53.866, 1e-6) || !vicino(q.Y, 5.939, 1e-6) ||
			!vicino(q.L, float64(n)*0.766, 1e-6) || !vicino(q.A, float64(n)*0.757, 1e-6) {
			t.Fatalf("versione %d: riquadro %+v", (n-17)/4, q)
		}
	}
	if len(viste) != 40 {
		t.Errorf("versioni provate %d su 40", len(viste))
	}
}

// Un QR con un modulo dei dati sbagliato non si legge (la correzione d'errore non si applica: i
// rettangoli sono esatti, e un errore vuol dire un codice diverso); una pagina senza QR non ne ha.
func TestQRSbagliatoONessuno(t *testing.T) {
	c := codiceDiProva(t, "SINT-QR-0005", qr.M, qr.Unicode)
	p := paginaDelCodice(c, 10, 10, 0.5, 0.5, false)
	// Il modulo in basso a destra è sempre dei dati: lo si toglie se è scuro, o lo si aggiunge.
	n := c.Bounds().Dx()
	ultimo := Rettangolo{X: 10 + float64(n-1)*0.5, Y: 10 + float64(n-1)*0.5, L: 0.5, A: 0.5}
	tolto := false
	for i, r := range p.Rettangoli {
		if r == ultimo {
			p.Rettangoli = append(p.Rettangoli[:i], p.Rettangoli[i+1:]...)
			tolto = true
			break
		}
	}
	if !tolto {
		p.Rettangoli = append(p.Rettangoli, ultimo)
	}
	if letti, err := CodiciQR(p); err == nil {
		t.Errorf("letto un QR sbagliato: %+v", letti)
	}
	if letti, err := CodiciQR(Pagina{Rettangoli: []Rettangolo{{X: 1, Y: 1, L: 70, A: 0.3}}}); err != nil || len(letti) != 0 {
		t.Errorf("senza QR: %v %+v", err, letti)
	}
}

// I QR delle etichette di Chromium dello standalone (fab-220, dati sintetici del banco, 03/11/2026):
// l'SVG di QRCode.create(:medium) esce come un rettangolo pieno per modulo, in byte, versione 1, e si
// legge: il codice salvato sulla busta, o il numero busta quando il codice manca (RIP-S-43).
func TestQRDelleEtichetteDiChromium(t *testing.T) {
	for file, atteso := range map[string]string{
		"testdata/chromium-etichetta-5.pdf": "SINT-QR-0005",
		"testdata/chromium-etichetta-1.pdf": "1/26",
		"testdata/chromium-etichetta-4.pdf": "4/26",
	} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		d, err := Leggi(b)
		if err != nil {
			t.Fatal(err)
		}
		letti, err := CodiciQR(d.Pagine[0])
		if err != nil || len(letti) != 1 {
			t.Fatalf("%s: %v %+v", file, err, letti)
		}
		q := letti[0]
		if q.Testo != atteso || q.Moduli != 21 || q.Modo != "byte" || !q.Specchiato || !vicino(q.X, 53.866, 0.01) || !vicino(q.Y, 5.939, 0.01) {
			t.Errorf("%s: %+v", file, q)
		}
		if got := fmt.Sprintf("%.2f %.2f", q.L, q.A); got != "16.09 15.90" {
			t.Errorf("%s: lato %s", file, got)
		}
	}
}

// Le posizioni chiave dell'etichetta di Chromium (fab-220, busta 5/26): il riquadro dei moduli del QR,
// la riga «#5/26», «Consegna:» e la casella «FATTO»; il confronto del banco le vuole entro ±0,5 mm e
// con le stesse voci.
func TestPosizioniDellEtichetta(t *testing.T) {
	b, err := os.ReadFile("testdata/chromium-etichetta-5.pdf")
	if err != nil {
		t.Fatal(err)
	}
	d, err := Leggi(b)
	if err != nil {
		t.Fatal(err)
	}
	pp, err := PosizioniEtichetta(d.Pagine[0])
	if err != nil {
		t.Fatal(err)
	}
	s := TestoPosizioni(pp)
	if s != "qr 53.87 5.94 16.09 15.90; numero 13.14 16.67; consegna 2.91 30.96; fatto 3.04 53.58 3.70 3.70" {
		t.Errorf("%s", s)
	}
	for altra, vicina := range map[string]bool{
		s: true,
		"qr 54.37 5.94 16.09 15.90; numero 13.14 16.67; consegna 2.91 30.96; fatto 3.04 53.58 3.70 3.70": true,
		"qr 54.38 5.94 16.09 15.90; numero 13.14 16.67; consegna 2.91 30.96; fatto 3.04 53.58 3.70 3.70": false,
		"qr 53.87 5.94 16.09 15.90; numero 13.14 17.68; consegna 2.91 30.96; fatto 3.04 53.58 3.70 3.70": false,
		"qr assente; numero 13.14 16.67; consegna 2.91 30.96; fatto 3.04 53.58 3.70 3.70":                false,
		"qr 53.87 5.94 16.09 15.90; numero 13.14 16.67; consegna 2.91 30.96; fatto assente":              false,
		"(non un PDF)": false,
	} {
		if PosizioniVicine(s, altra, 0.5) != vicina || PosizioniVicine(altra, s, 0.5) != vicina {
			t.Errorf("%q: vicina %v", altra, !vicina)
		}
	}
	if !PosizioniVicine("(non un PDF)", "(non un PDF)", 0.5) {
		t.Error("due non PDF")
	}
	vuota, err := PosizioniEtichetta(Pagina{})
	if err != nil || TestoPosizioni(vuota) != "qr assente; numero assente; consegna assente; fatto assente" {
		t.Errorf("%v %q", err, TestoPosizioni(vuota))
	}
}

// I bit di formato non si correggono: con un bit cambiato in una sola copia il QR si legge con
// l'altra, esatta; con un bit cambiato in tutte e due non si legge.
func TestQRFormatoSenzaCorrezione(t *testing.T) {
	c := codiceDiProva(t, "SINT-QR-0005", qr.M, qr.Unicode)
	n := c.Bounds().Dx()
	griglia := func() [][]bool {
		m := make([][]bool, n)
		for y := range m {
			m[y] = make([]bool, n)
			for x := range m[y] {
				m[y][x] = c.At(x, y) == color.Black
			}
		}
		return m
	}
	m := griglia()
	m[0][8] = !m[0][8] // bit 0 della prima copia, (x=8, y=0)
	if testo, _, err := decodifica(m); err != nil || testo != "SINT-QR-0005" {
		t.Errorf("una copia guasta: %q %v", testo, err)
	}
	m[8][n-1] = !m[8][n-1] // bit 0 della seconda copia, (x=n-1, y=8)
	if testo, _, err := decodifica(m); err == nil {
		t.Errorf("due copie guaste lette: %q", testo)
	}
}

// PosizioniVicine vuole le stesse voci nello stesso ordine: lunghezze diverse, nomi diversi o un
// separatore diverso non sono vicini (e non vanno in panic); i numeri entro la tolleranza sì.
func TestPosizioniVicineStruttura(t *testing.T) {
	for _, c := range []struct {
		a, b   string
		vicine bool
	}{
		{"numero 1 2", "consegna 1 2", false},
		{"numero 1 2", "numero 1", false},
		{"numero 1", "numero 1 2", false},
		{"qr 1 2 3 4; numero 1 2", "qr 1 2 3 4", false},
		{"qr 1 2 3 4; numero 1 2", "qr 1 2 3 4 numero 1 2", false},
		{"numero 1 2", "numero 1.4 2.5", true},
		{"numero 1 2", "numero assente", false},
		{"", "numero 1 2", false},
	} {
		if got := PosizioniVicine(c.a, c.b, 0.5); got != c.vicine {
			t.Errorf("%q / %q: %v", c.a, c.b, got)
		}
		if got := PosizioniVicine(c.b, c.a, 0.5); got != c.vicine {
			t.Errorf("%q / %q: %v", c.b, c.a, got)
		}
	}
}

// Con l'artigiano sopra (busta 4/26 di Chromium) la riga del numero è quella con «#», non la prima.
func TestPosizioniConLArtigiano(t *testing.T) {
	b, err := os.ReadFile("testdata/chromium-etichetta-4.pdf")
	if err != nil {
		t.Fatal(err)
	}
	d, err := Leggi(b)
	if err != nil {
		t.Fatal(err)
	}
	pp, err := PosizioniEtichetta(d.Pagine[0])
	if err != nil {
		t.Fatal(err)
	}
	if s := TestoPosizioni(pp); s != "qr 53.87 5.94 16.09 15.90; numero 13.14 20.37; consegna 2.91 30.96; fatto 3.04 53.58 3.70 3.70" {
		t.Errorf("%s", s)
	}
}
