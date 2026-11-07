package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/pdf/lettura"
)

// paginaDi è la matrice come la disegnerebbe un PDF, un rettangolo di 1 mm per modulo scuro, così
// lettura.CodiciQR la decodifica (ISO/IEC 18004) e dice se si legge solo trasposta.
func paginaDi(m [][]bool) lettura.Pagina {
	var p lettura.Pagina
	for y, riga := range m {
		for x, scuro := range riga {
			if scuro {
				p.Rettangoli = append(p.Rettangoli, lettura.Rettangolo{X: 10 + float64(x), Y: 10 + float64(y), L: 1, A: 1})
			}
		}
	}
	return p
}

func letto(t *testing.T, m [][]bool) lettura.CodiceQR {
	t.Helper()
	qq, err := lettura.CodiciQR(paginaDi(m))
	if err != nil || len(qq) != 1 {
		t.Fatalf("%d QR, %v", len(qq), err)
	}
	return qq[0]
}

// inventario: RIP-H-28
// Le tre fotocamere finte dello scenario browser lettore-qr: il QR diritto si legge diritto, il
// trasposto solo trasposto (D-52), l'etichetta di Chromium dello standalone (la fixture della slice
// 043) è trasposta e porta SINT-QR-0005; ogni file Y4M ha l'intestazione che Chromium legge, tre
// fotogrammi 640×480 4:2:0 e il QR disegnato modulo per modulo.
func TestFotocamereFinte(t *testing.T) {
	d, err := matriceQR(codiceFotocamera)
	if err != nil {
		t.Fatal(err)
	}
	if q := letto(t, d); q.Testo != codiceFotocamera || q.Specchiato {
		t.Errorf("diritto: %+v", q)
	}
	tr := trasposta(d)
	if q := letto(t, tr); q.Testo != codiceFotocamera || !q.Specchiato {
		t.Errorf("trasposto: %+v", q)
	}
	pdf, err := os.ReadFile(filepath.Join("..", "..", etichettaStandalone))
	if err != nil {
		t.Fatal(err)
	}
	e, q, err := matriceEtichetta(pdf)
	if err != nil || q.Testo != "SINT-QR-0005" || !q.Specchiato || len(e) != 21 {
		t.Fatalf("etichetta: %+v %v", q, err)
	}
	if r := letto(t, e); r.Testo != "SINT-QR-0005" || !r.Specchiato {
		t.Errorf("etichetta ridisegnata: %+v", r)
	}
	dir := filepath.Join(t.TempDir(), "fotocamere")
	if err := scriviFotocamere(dir, filepath.Join("..", "..", etichettaStandalone)); err != nil {
		t.Fatal(err)
	}
	for nome, m := range map[string][][]bool{"diritto": d, "trasposto": tr, "etichetta": e} {
		b, err := os.ReadFile(filepath.Join(dir, nome+".y4m"))
		if err != nil {
			t.Fatal(err)
		}
		const testa = "YUV4MPEG2 W640 H480 F10:1 Ip A1:1 C420jpeg\n"
		quadro := 640*480 + 2*320*240
		if !strings.HasPrefix(string(b), testa) || len(b) != len(testa)+3*(len("FRAME\n")+quadro) {
			t.Fatalf("%s: %d byte", nome, len(b))
		}
		// Il fotogramma riletto al centro di ogni modulo è la matrice.
		luma := b[len(testa)+len("FRAME\n"):][:640*480]
		lato := (len(m) + 2*margineModuli) * pixelPerModulo
		x0, y0 := (640-lato)/2+margineModuli*pixelPerModulo, (480-lato)/2+margineModuli*pixelPerModulo
		for r := range m {
			for c := range m[r] {
				v := luma[(y0+r*pixelPerModulo+pixelPerModulo/2)*640+x0+c*pixelPerModulo+pixelPerModulo/2]
				if (v == 0) != m[r][c] {
					t.Fatalf("%s: modulo %d,%d", nome, r, c)
				}
			}
		}
		if bytes.Count(b, []byte("FRAME\n")) < 3 {
			t.Errorf("%s: fotogrammi", nome)
		}
	}
	if st, err := os.Stat(dir); err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("cartella: %v %v", st, err)
	}
}
