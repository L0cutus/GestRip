package stampe

import (
	"fmt"
	"math"
	"testing"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/pdf"
	"github.com/L0cutus/gestrip/internal/pdf/lettura"
)

func fogliQR(t *testing.T, f buste.FoglioEtichette) *lettura.Documento {
	t.Helper()
	b, err := EtichetteQR(f)
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
	return l
}

// inventario: RIP-S-48
// Una pagina per etichetta, del formato chiesto; il QR contiene «RIP-<anno>-NNNN» (letto diritto) e
// sotto c'è il numero «0045/26» (D-21, D-64), tutto dentro l'etichetta con 2 mm di margine, anche al
// formato minimo di 16 mm e con un numero oltre 9999.
func TestEtichetteQRNumeroSottoIlQR(t *testing.T) {
	for _, c := range []struct {
		l, h  float64
		da, a int64
	}{
		{57, 32, 44, 46}, {16, 16, 45, 45}, {40, 16, 1, 2}, {200, 16, 9999, 10000}, {16, 200, 10000, 10000},
		{20, 57.5, 7, 7}, {200, 200, 3, 3}, {57.03125, 32.5, 1, 1},
	} {
		nome := fmt.Sprintf("%vx%v", c.l, c.h)
		l := fogliQR(t, buste.FoglioEtichette{Da: c.da, A: c.a, Anno: 2026, L: c.l, H: c.h})
		if len(l.Pagine) != int(c.a-c.da+1) {
			t.Fatalf("%s: %d pagine", nome, len(l.Pagine))
		}
		for i, p := range l.Pagine {
			n := c.da + int64(i)
			if math.Abs(p.Larghezza-c.l) > 0.01 || math.Abs(p.Altezza-c.h) > 0.01 {
				t.Errorf("%s: pagina %v×%v", nome, p.Larghezza, p.Altezza)
			}
			qq, err := lettura.CodiciQR(p)
			if err != nil || len(qq) != 1 || qq[0].Testo != buste.CodiceEtichetta(2026, n) || qq[0].Specchiato {
				t.Fatalf("%s %d: QR %v %+v", nome, n, err, qq)
			}
			q := qq[0]
			if len(p.Righe) != 1 || p.Righe[0].Testo != buste.NumeroEtichetta(2026, n) {
				t.Fatalf("%s %d: righe %+v", nome, n, p.Righe)
			}
			r := p.Righe[0]
			d, _ := pdf.Nuovo(10, 10)
			w, _ := d.Larghezza(r.Testo, r.Corpo, true)
			switch {
			case q.X < 2 || q.Y < 2 || q.X+q.L > c.l-2 || q.Y+q.A > c.h-2:
				t.Errorf("%s %d: QR fuori dal margine: %+v", nome, n, q)
			case r.Y-r.Corpo*0.3528*0.76 < q.Y+q.A:
				t.Errorf("%s %d: il numero (linea di base %.2f, corpo %v) non è sotto il QR (fondo %.2f)", nome, n, r.Y, r.Corpo, q.Y+q.A)
			case r.Y > c.h-2:
				t.Errorf("%s %d: il numero esce in basso: %.2f", nome, n, r.Y)
			case r.X < 2-0.01 || r.X+w > c.l-2+0.01:
				t.Errorf("%s %d: il numero esce di lato: %.2f + %.2f", nome, n, r.X, w)
			case math.Abs(r.X+w/2-c.l/2) > 0.05 || math.Abs(q.X+q.L/2-c.l/2) > 0.05:
				t.Errorf("%s %d: non centrati: numero %.2f, QR %.2f", nome, n, r.X+w/2, q.X+q.L/2)
			}
		}
	}
}

// inventario: RIP-S-48
// La disposizione documentata: a 57×32 mm numero 17 pt e QR di 22 mm circa; a 16×16 mm numero 7,5 pt e
// QR di circa 9,35 mm; il corpo scende finché il numero non sta nella larghezza.
func TestDisposizioneEtichettaQR(t *testing.T) {
	d, err := pdf.Nuovo(10, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		l, h, corpo, lato float64
		testo             string
	}{
		{57, 32, 17, 22.0, "0045/26"},
		{16, 16, 7.5, 9.35, "0045/26"},
		{16, 16, 6.5, 9.71, "10000/26"},
		{20, 57.5, 10, 16, "0007/26"},
	} {
		p, err := DisponiEtichettaQR(d, c.l, c.h, c.testo)
		if err != nil {
			t.Fatal(err)
		}
		if p.Corpo != c.corpo || math.Abs(p.Lato-c.lato) > 0.01 {
			t.Errorf("%vx%v %s: corpo %v lato %.2f, attesi %v %.2f", c.l, c.h, c.testo, p.Corpo, p.Lato, c.corpo, c.lato)
		}
	}
}
