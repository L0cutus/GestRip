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

// matriceDelFotogramma rilegge il QR di un fotogramma della sequenza (luma w×h, ppm pixel per modulo,
// moduli per lato) al centro di ogni modulo; nil se il fotogramma è tutto bianco.
func matriceDelFotogramma(luma []byte, w, h, ppm, moduli int) [][]bool {
	if !bytes.ContainsRune(luma, 0) {
		return nil
	}
	lato := (moduli + 2*margineModuli) * ppm
	x0, y0 := (w-lato)/2+margineModuli*ppm, (h-lato)/2+margineModuli*ppm
	m := make([][]bool, moduli)
	for r := range m {
		m[r] = make([]bool, moduli)
		for c := range m[r] {
			m[r][c] = luma[(y0+r*ppm+ppm/2)*w+x0+c*ppm+ppm/2] == 0
		}
	}
	return m
}

// inventario: ART-09
// La fotocamera finta della prova scanner di lettore-qr (slice 079): un video 320×240 a 10 fotogrammi al
// secondo, con il bianco all'inizio (2,5 s, la fotocamera parte prima della busta), il QR di una busta (1,5 s), il bianco (3 s, più dei 2 s dopo i quali lo scanner continuo
// riprende lo stesso codice), il QR di un codice di nessuna busta (1,5 s), il bianco, di nuovo la busta e il
// bianco: la busta si aggiunge, il codice sconosciuto si rifiuta e la busta di nuovo è «già in lista». Ogni QR si rilegge dal fotogramma e si decodifica con il suo testo.
func TestFotocameraDellaConsegna(t *testing.T) {
	controllaSequenzaScanner(t, "consegna.y4m", codiceConsegna)
}

// inventario: ORE-07
// La fotocamera finta dello scanner del lotto (slice 090): la stessa sequenza della consegna, con il codice della
// busta propria dello scenario (la prova lotto di lettore-qr la crea con quel QR), che non è quello della consegna.
func TestFotocameraDelLotto(t *testing.T) {
	if codiceLotto == codiceConsegna || codiceLotto == codiceSconosciuto {
		t.Fatal("il codice del lotto deve essere suo")
	}
	controllaSequenzaScanner(t, "lotto.y4m", codiceLotto)
	// La busta che la prova crea con quel QR è scritta in banco.mjs (CODICE_LOTTO): i due devono dire lo stesso testo.
	js, err := os.ReadFile(filepath.Join("browser", "banco.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(js), "const CODICE_LOTTO = '"+codiceLotto+"';") {
		t.Fatalf("banco.mjs non ha CODICE_LOTTO = %q", codiceLotto)
	}
}

// controllaSequenzaScanner legge il video nome scritto da scriviFotocamere e controlla ogni fotogramma: bianco
// dove serve, il QR della busta (codiceBusta) ai due passaggi e quello sconosciuto in mezzo.
func controllaSequenzaScanner(t *testing.T, nome, codiceBusta string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "fotocamere")
	if err := scriviFotocamere(dir, filepath.Join("..", "..", etichettaStandalone)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, nome))
	if err != nil {
		t.Fatal(err)
	}
	const testa = "YUV4MPEG2 W320 H240 F10:1 Ip A1:1 C420jpeg\n"
	quadro := 320*240 + 2*160*120
	if !strings.HasPrefix(string(b), testa) || len(b) != len(testa)+160*(len("FRAME\n")+quadro) {
		t.Fatalf("%s: %d byte", nome, len(b))
	}
	busta, _ := matriceQR(codiceBusta)
	sconosciuto, _ := matriceQR(codiceSconosciuto)
	for i := range 160 {
		f := b[len(testa)+i*(len("FRAME\n")+quadro):]
		if !strings.HasPrefix(string(f), "FRAME\n") {
			t.Fatalf("fotogramma %d senza FRAME", i)
		}
		luma := f[len("FRAME\n"):][:320*240]
		var atteso [][]bool
		testo := ""
		switch {
		case i >= 25 && i < 40, i >= 115 && i < 130:
			atteso, testo = busta, codiceBusta
		case i >= 70 && i < 85:
			atteso, testo = sconosciuto, codiceSconosciuto
		}
		got := matriceDelFotogramma(luma, 320, 240, pixelPerModuloSequenza, len(atteso))
		if atteso == nil {
			if got != nil {
				t.Fatalf("fotogramma %d: atteso bianco", i)
			}
			continue
		}
		if q := letto(t, got); q.Testo != testo || q.Specchiato {
			t.Fatalf("fotogramma %d: %+v", i, q)
		}
	}
}
