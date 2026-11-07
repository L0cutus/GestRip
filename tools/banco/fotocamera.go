package main

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/L0cutus/gestrip/internal/pdf/lettura"
	"github.com/boombuler/barcode/qr"
)

// Le fotocamere finte del livello browser (slice 055, scenario lettore-qr: RIP-H-27, RIP-H-28,
// D-52). Chromium le legge da un file Y4M (--use-file-for-fake-video-capture), che `banco esegui`
// scrive prima del giro nella cartella fotocamere dell'istanza, montata in sola lettura nel
// container del browser:
//   - diritto.y4m: il QR di codiceFotocamera, come lo stampa il Go;
//   - trasposto.y4m: lo stesso QR a specchio sulla diagonale (righe e colonne scambiate), come le
//     etichette stampate dallo standalone (D-52, misurato nella slice 043);
//   - etichetta.y4m: il QR vero di un'etichetta di Chromium dello standalone (la fixture della slice
//     043, trasposta), ridisegnato dai suoi rettangoli.
// Solo dati sintetici: i codici sono quelli dei dati del banco.

const (
	// codiceFotocamera è l'etichetta della 37/26 consegnata dei dati importati (pannello-qr la usa
	// per l'avviso della busta chiusa); nel database vuoto non è di nessuna busta.
	codiceFotocamera = "RIP-2026-0056"
	// etichettaStandalone è la fixture, dalla radice del repo (l'opzione -etichetta-qr di esegui).
	etichettaStandalone = "internal/pdf/lettura/testdata/chromium-etichetta-5.pdf"
	// Il fotogramma: 640×480, 10 pixel per modulo, 4 moduli di margine chiaro.
	larghezzaFotogramma, altezzaFotogramma = 640, 480
	pixelPerModulo                         = 10
	margineModuli                          = 4
	fotogrammiY4M                          = 3
)

// cartellaFotocamere è la cartella dei file Y4M dell'istanza.
func cartellaFotocamere(n nomi) string { return filepath.Join(n.dir, "fotocamere") }

// matriceQR è il QR di testo come lo fa il motore dei PDF del Go (internal/pdf: livello M), riga
// per riga: true è un modulo scuro.
func matriceQR(testo string) ([][]bool, error) {
	c, err := qr.Encode(testo, qr.M, qr.Unicode)
	if err != nil {
		return nil, err
	}
	b := c.Bounds()
	m := make([][]bool, b.Dy())
	for y := range m {
		m[y] = make([]bool, b.Dx())
		for x := range m[y] {
			r, _, _, _ := c.At(b.Min.X+x, b.Min.Y+y).RGBA()
			m[y][x] = r < 0x8000
		}
	}
	return m, nil
}

// trasposta scambia righe e colonne: lo specchio sulla diagonale (D-52).
func trasposta(m [][]bool) [][]bool {
	t := make([][]bool, len(m))
	for i := range m {
		t[i] = make([]bool, len(m))
		for j := range m {
			t[i][j] = m[j][i]
		}
	}
	return t
}

// matriceEtichetta ridisegna il QR della prima pagina del PDF dai suoi rettangoli: i moduli scuri
// dentro il riquadro che lettura.CodiciQR ha trovato. Dà anche il testo e se il QR è specchiato.
func matriceEtichetta(pdf []byte) ([][]bool, lettura.CodiceQR, error) {
	d, err := lettura.Leggi(pdf)
	if err != nil {
		return nil, lettura.CodiceQR{}, err
	}
	if len(d.Pagine) == 0 {
		return nil, lettura.CodiceQR{}, fmt.Errorf("etichetta senza pagine")
	}
	p := d.Pagine[0]
	qq, err := lettura.CodiciQR(p)
	if err != nil || len(qq) != 1 {
		return nil, lettura.CodiceQR{}, fmt.Errorf("etichetta: %d QR (%v)", len(qq), err)
	}
	q := qq[0]
	lm, am := q.L/float64(q.Moduli), q.A/float64(q.Moduli)
	const eps = 0.01
	m := make([][]bool, q.Moduli)
	for i := range m {
		m[i] = make([]bool, q.Moduli)
	}
	for _, r := range p.Rettangoli {
		if r.X < q.X-eps || r.Y < q.Y-eps || r.X+r.L > q.X+q.L+eps || r.Y+r.A > q.Y+q.A+eps {
			continue
		}
		c0, r0 := int(math.Round((r.X-q.X)/lm)), int(math.Round((r.Y-q.Y)/am))
		c1, r1 := int(math.Round((r.X+r.L-q.X)/lm)), int(math.Round((r.Y+r.A-q.Y)/am))
		for y := r0; y < r1 && y < q.Moduli; y++ {
			for x := c0; x < c1 && x < q.Moduli; x++ {
				m[y][x] = true
			}
		}
	}
	return m, q, nil
}

// fileY4M è un video Y4M 4:2:0 di fotogrammiY4M fotogrammi uguali: la matrice nera su bianco al
// centro, con il margine chiaro.
func fileY4M(m [][]bool) ([]byte, error) {
	w, h := larghezzaFotogramma, altezzaFotogramma
	lato := (len(m) + 2*margineModuli) * pixelPerModulo
	if lato > w || lato > h {
		return nil, fmt.Errorf("QR di %d moduli troppo grande per il fotogramma", len(m))
	}
	luma := bytes.Repeat([]byte{255}, w*h)
	x0 := (w-lato)/2 + margineModuli*pixelPerModulo
	y0 := (h-lato)/2 + margineModuli*pixelPerModulo
	for r, riga := range m {
		for c, scuro := range riga {
			if !scuro {
				continue
			}
			for dy := range pixelPerModulo {
				o := (y0+r*pixelPerModulo+dy)*w + x0 + c*pixelPerModulo
				for dx := range pixelPerModulo {
					luma[o+dx] = 0
				}
			}
		}
	}
	croma := bytes.Repeat([]byte{128}, 2*(w/2)*(h/2))
	var b bytes.Buffer
	fmt.Fprintf(&b, "YUV4MPEG2 W%d H%d F10:1 Ip A1:1 C420jpeg\n", w, h)
	for range fotogrammiY4M {
		b.WriteString("FRAME\n")
		b.Write(luma)
		b.Write(croma)
	}
	return b.Bytes(), nil
}

// scriviFotocamere scrive i tre file nella cartella dir (rifatta da capo); pdfEtichetta è la fixture
// dell'etichetta di Chromium dello standalone.
func scriviFotocamere(dir, pdfEtichetta string) error {
	diritto, err := matriceQR(codiceFotocamera)
	if err != nil {
		return err
	}
	pdf, err := leggiPiccolo(pdfEtichetta, 1<<20)
	if err != nil {
		return fmt.Errorf("etichetta dello standalone: %w", err)
	}
	etichetta, q, err := matriceEtichetta(pdf)
	if err != nil {
		return err
	}
	if !q.Specchiato {
		return fmt.Errorf("l'etichetta dello standalone non è trasposta: non prova la D-52")
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	for nome, m := range map[string][][]bool{"diritto": diritto, "trasposto": trasposta(diritto), "etichetta": etichetta} {
		b, err := fileY4M(m)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, nome+".y4m"), b, 0o600); err != nil {
			return err
		}
	}
	return nil
}
