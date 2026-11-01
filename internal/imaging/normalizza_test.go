package imaging

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/L0cutus/gestrip/internal/imaging/sintetiche"
)

// I casi di media_store_normalize_test.exs dello standalone (slice 191), con le immagini fatte in
// Go invece che con Pillow.

func normalizzata(t *testing.T, in []byte) sintetiche.InfoJPEG {
	t.Helper()
	out, err := Normalizza(in)
	if err != nil {
		t.Fatalf("Normalizza: %v", err)
	}
	i, ok := sintetiche.LeggiJPEG(out)
	if !ok {
		t.Fatalf("non è un JPEG: % x", out[:min(len(out), 8)])
	}
	return i
}

func rifiutata(t *testing.T, cosa string, in []byte) {
	t.Helper()
	if out, err := Normalizza(in); !errors.Is(err, ErrImmagine) || out != nil {
		t.Errorf("%s: %d byte, %v; atteso ErrImmagine", cosa, len(out), err)
	}
}

func dimensioni(t *testing.T, cosa string, i sintetiche.InfoJPEG, l, a int) {
	t.Helper()
	if i.Larghezza != l || i.Altezza != a {
		t.Errorf("%s: %d×%d, attese %d×%d", cosa, i.Larghezza, i.Altezza, l, a)
	}
}

// inventario: RIP-S-30
// Una foto da 12 MP del telefono tiene la sua risoluzione e diventa un JPEG di base senza
// metadati (niente EXIF né ICC), qualità 90 (primo valore della luminanza 3).
func TestNormalizzaFotoDelTelefono(t *testing.T) {
	for _, d := range [][2]int{{4032, 3024}, {4080, 3060}} {
		i := normalizzata(t, sintetiche.JPEG(sintetiche.Liscio(d[0], d[1]), sintetiche.OpzioniJPEG{}))
		dimensioni(t, "telefono", i, d[0], d[1])
		if !i.Base || i.EXIF || i.ICC || i.LumaQ0 != 3 || i.Componenti != 3 {
			t.Errorf("%v: %+v; attesi base, senza EXIF né ICC, luma 3, 3 componenti", d, i)
		}
	}
}

// inventario: RIP-S-30
// Un PNG o un WebP fermo esce JPEG alla sua dimensione.
func TestNormalizzaPNGEWebPDiventanoJPEG(t *testing.T) {
	i := normalizzata(t, sintetiche.PNG(4000, 3000, 1))
	dimensioni(t, "PNG", i, 4000, 3000)
	if !i.Base {
		t.Errorf("PNG: non di base")
	}
	i = normalizzata(t, sintetiche.WebP(1200, 900, 1))
	dimensioni(t, "WebP", i, 1200, 900)
	if !i.Base {
		t.Errorf("WebP: non di base")
	}
}

// inventario: RIP-S-30
// L'orientamento EXIF si applica ai pixel, poi il tag sparisce.
func TestNormalizzaApplicaLOrientamento(t *testing.T) {
	i := normalizzata(t, sintetiche.JPEG(sintetiche.Gradiente(400, 300, 1), sintetiche.OpzioniJPEG{Orientamento: 6}))
	dimensioni(t, "orientamento 6", i, 300, 400)
	if i.EXIF {
		t.Errorf("l'EXIF è rimasto")
	}
}

// riquadri è un'immagine 64×32 nera con un quadrato rosso in alto a sinistra e uno verde in alto
// a destra: dopo una rotazione o un ribaltamento si vede dove sono finiti.
func riquadri() *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			c := color.RGBA{0, 0, 0, 255}
			switch {
			case x < 16 && y < 16:
				c = color.RGBA{255, 0, 0, 255}
			case x >= 48 && y < 16:
				c = color.RGBA{0, 255, 0, 255}
			}
			m.Set(x, y, c)
		}
	}
	return m
}

// inventario: RIP-S-30
// Gli otto orientamenti EXIF come ImageOps.exif_transpose di Pillow: dove finiscono il rosso
// (in alto a sinistra) e il verde (in alto a destra) dell'immagine 64×32.
func TestNormalizzaGliOttoOrientamenti(t *testing.T) {
	// angoli: 0 alto sinistra, 1 alto destra, 2 basso sinistra, 3 basso destra.
	casi := []struct {
		o, l, a      int
		rosso, verde int
	}{
		{1, 64, 32, 0, 1},
		{2, 64, 32, 1, 0}, // specchio orizzontale
		{3, 64, 32, 3, 2}, // 180°
		{4, 64, 32, 2, 3}, // specchio verticale
		{5, 32, 64, 0, 2}, // trasposta
		{6, 32, 64, 1, 3}, // 90° in senso orario
		{7, 32, 64, 3, 1}, // trasversa
		{8, 32, 64, 2, 0}, // 90° in senso antiorario
	}
	for _, c := range casi {
		out, err := Normalizza(sintetiche.JPEG(riquadri(), sintetiche.OpzioniJPEG{Orientamento: c.o}))
		if err != nil {
			t.Fatalf("orientamento %d: %v", c.o, err)
		}
		m, err := jpeg.Decode(bytes.NewReader(out))
		if err != nil {
			t.Fatal(err)
		}
		b := m.Bounds()
		if b.Dx() != c.l || b.Dy() != c.a {
			t.Errorf("orientamento %d: %d×%d, attese %d×%d", c.o, b.Dx(), b.Dy(), c.l, c.a)
			continue
		}
		angolo := func(n int) (int, int) {
			x, y := 4, 4
			if n%2 == 1 {
				x = b.Dx() - 5
			}
			if n >= 2 {
				y = b.Dy() - 5
			}
			return x, y
		}
		x, y := angolo(c.rosso)
		if r, g, _ := sintetiche.Colore(m, x, y); r < 200 || g > 60 {
			t.Errorf("orientamento %d: il rosso non è nell'angolo %d (%d,%d)", c.o, c.rosso, r, g)
		}
		x, y = angolo(c.verde)
		if r, g, _ := sintetiche.Colore(m, x, y); g < 200 || r > 60 {
			t.Errorf("orientamento %d: il verde non è nell'angolo %d (%d,%d)", c.o, c.verde, r, g)
		}
	}
}

// inventario: RIP-S-30
// Il lato lungo si ferma a 4096 px e una foto piccola non si ingrandisce.
func TestNormalizzaLatoMassimo(t *testing.T) {
	dimensioni(t, "largo", normalizzata(t, sintetiche.JPEG(sintetiche.Liscio(6000, 4500), sintetiche.OpzioniJPEG{})), 4096, 3072)
	dimensioni(t, "alto", normalizzata(t, sintetiche.CodificaPNG(sintetiche.Liscio(3750, 5000))), 3072, 4096)
	dimensioni(t, "piccolo", normalizzata(t, sintetiche.PNG(640, 480, 1)), 640, 480)
}

// inventario: RIP-S-30
// Il modo da 50 MP (8160×6120) esce come la riduzione a metà di Pillow (draft), 4080×3060, non
// rifiutato; appena sotto i 60 MP, ridotto a metà e poi al lato massimo.
func TestNormalizzaCinquantaMegapixel(t *testing.T) {
	if testing.Short() {
		t.Skip("immagini da 50 MP")
	}
	dimensioni(t, "50 MP", normalizzata(t, sintetiche.JPEG(sintetiche.Liscio(8160, 6120), sintetiche.OpzioniJPEG{})), 4080, 3060)
	dimensioni(t, "bordo", normalizzata(t, sintetiche.JPEG(sintetiche.Liscio(8944, 6708), sintetiche.OpzioniJPEG{})), 4096, 3072)
}

// inventario: RIP-S-30
// Oltre i 60 megapixel è una bomba di decompressione ed è rifiutata, in ogni formato.
func TestNormalizzaOltreSessantaMegapixel(t *testing.T) {
	rifiutata(t, "JPEG 9000×6800", sintetiche.JPEG(sintetiche.Liscio(9000, 6800), sintetiche.OpzioniJPEG{}))
	rifiutata(t, "PNG 7900×7800", sintetiche.CodificaPNG(image.NewGray(image.Rect(0, 0, 7900, 7800))))
}

// inventario: RIP-S-30
// La foto in arrivo può pesare 25 MiB e non un byte di più.
func TestNormalizzaVenticinqueMiB(t *testing.T) {
	pesante := sintetiche.JPEG(sintetiche.Gradiente(1600, 1200, 1), sintetiche.OpzioniJPEG{Pad: MaxByteIngresso})
	if len(pesante) != MaxByteIngresso {
		t.Fatalf("fixture: %d byte", len(pesante))
	}
	dimensioni(t, "25 MiB", normalizzata(t, pesante), 1600, 1200)
	rifiutata(t, "25 MiB + 1", sintetiche.JPEG(sintetiche.Gradiente(1600, 1200, 1), sintetiche.OpzioniJPEG{Pad: MaxByteIngresso + 1}))
	if MaxByteIngresso != 25*1024*1024 {
		t.Errorf("MaxByteIngresso = %d", MaxByteIngresso)
	}
}

// inventario: RIP-S-30
// Ciò che non è un'immagine ferma è rifiutato: byte a caso, niente, un GIF, un PNG troncato.
func TestNormalizzaRifiutaCioCheNonEUnImmagine(t *testing.T) {
	caso := make([]byte, 4096)
	if _, err := rand.Read(caso); err != nil {
		t.Fatal(err)
	}
	rifiutata(t, "byte a caso", caso)
	rifiutata(t, "vuoto", nil)
	rifiutata(t, "GIF", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"))
	p := sintetiche.PNG(64, 64, 1)
	rifiutata(t, "PNG troncato", p[:len(p)/2])
}

// inventario: RIP-S-30
// Più fotogrammi sono rifiutati come in Pillow 9.4 (n_frames != 1): un PNG animato con due
// fotogrammi e un JPEG multi-immagine (MPO, il formato di alcuni telefoni) con due immagini. Con
// un solo fotogramma dichiarato passano. Un indice MP che Pillow non sa leggere (senza le voci,
// con una voce non JPEG) lascia un JPEG normale; voci più corte del numero delle immagini fanno
// fallire l'apertura.
func TestNormalizzaRifiutaPiuFotogrammi(t *testing.T) {
	actl := func(n uint32) []byte {
		return binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, n), 0)
	}
	p := sintetiche.PNG(64, 64, 1)
	rifiutata(t, "APNG con 2 fotogrammi", sintetiche.ConChunkPNG(p, "acTL", actl(2)))
	dimensioni(t, "APNG con 1 fotogramma", normalizzata(t, sintetiche.ConChunkPNG(p, "acTL", actl(1))), 64, 64)
	dimensioni(t, "APNG con 0 fotogrammi (non valido)", normalizzata(t, sintetiche.ConChunkPNG(p, "acTL", actl(0))), 64, 64)
	rifiutata(t, "acTL troncato", sintetiche.ConChunkPNG(p, "acTL", []byte{0, 0, 0, 2}))
	due := sintetiche.ConChunkPNG(sintetiche.ConChunkPNG(p, "acTL", actl(2)), "acTL", actl(3))
	dimensioni(t, "due acTL (non valido)", normalizzata(t, due), 64, 64)

	j := sintetiche.JPEG(sintetiche.Gradiente(64, 64, 1), sintetiche.OpzioniJPEG{SenzaMetadati: true})
	rifiutata(t, "MPO con 2 immagini", sintetiche.SegmentoJPEG(j, 0xE2, mpf(2, 0x030000, 0x030000)))
	dimensioni(t, "MPO con 1 immagine", normalizzata(t, sintetiche.SegmentoJPEG(j, 0xE2, mpf(1, 0x030000))), 64, 64)
	dimensioni(t, "MPO senza voci", normalizzata(t, sintetiche.SegmentoJPEG(j, 0xE2, mpfSenzaVoci(2))), 64, 64)
	// Casi misurati con Pillow dalla review: iPhone (primaria 0x20030000, seconda 0), disparità
	// (0x020002 ×2) rifiutate; ImageDataFormat 1 (0x01030000) accettata come JPEG.
	rifiutata(t, "MPO dell'iPhone", sintetiche.SegmentoJPEG(j, 0xE2, mpf(2, 0x20030000, 0)))
	rifiutata(t, "MPO di disparità", sintetiche.SegmentoJPEG(j, 0xE2, mpf(2, 0x020002, 0x020002)))
	rifiutata(t, "MPO con un tipo MP qualsiasi", sintetiche.SegmentoJPEG(j, 0xE2, mpf(2, 0x020000, 0x020000)))
	dimensioni(t, "MPO di formato 1", normalizzata(t, sintetiche.SegmentoJPEG(j, 0xE2, mpf(2, 0x01030000, 0x01030000))), 64, 64)
	dimensioni(t, "MPO con la seconda di formato 1", normalizzata(t, sintetiche.SegmentoJPEG(j, 0xE2, mpf(2, 0x030000, 0x01000000))), 64, 64)
	rifiutata(t, "MPO con le voci corte", sintetiche.SegmentoJPEG(j, 0xE2, mpfCon(3, true, 0x030000, 0x030000)))
}

// mpf è il carico di un segmento APP2 «MPF» (CIPA DC-007, big endian) con NumberOfImages = n e
// una voce MP Entry per ogni attributo.
func mpf(n uint32, attributi ...uint32) []byte { return mpfCon(n, true, attributi...) }

func mpfSenzaVoci(n uint32) []byte { return mpfCon(n, false) }

func mpfCon(n uint32, conVoci bool, attributi ...uint32) []byte {
	p := []byte("MPF\x00MM\x00\x2A\x00\x00\x00\x08")
	tag := uint16(2)
	if conVoci {
		tag = 3
	}
	p = binary.BigEndian.AppendUint16(p, tag)
	// MPFVersion 0xB000 UNDEFINED 4 «0100»
	p = binary.BigEndian.AppendUint16(p, 0xB000)
	p = binary.BigEndian.AppendUint16(p, 7)
	p = binary.BigEndian.AppendUint32(p, 4)
	p = append(p, "0100"...)
	// NumberOfImages 0xB001 LONG 1
	p = binary.BigEndian.AppendUint16(p, 0xB001)
	p = binary.BigEndian.AppendUint16(p, 4)
	p = binary.BigEndian.AppendUint32(p, 1)
	p = binary.BigEndian.AppendUint32(p, n)
	if conVoci {
		// MPEntry 0xB002 UNDEFINED 16 × voci, subito dopo l'IFD
		fine := 8 + 2 + 12*int(tag) + 4
		p = binary.BigEndian.AppendUint16(p, 0xB002)
		p = binary.BigEndian.AppendUint16(p, 7)
		p = binary.BigEndian.AppendUint32(p, uint32(16*len(attributi)))
		p = binary.BigEndian.AppendUint32(p, uint32(fine))
	}
	p = binary.BigEndian.AppendUint32(p, 0)
	for _, a := range attributi {
		p = binary.BigEndian.AppendUint32(p, a)    // Attribute
		p = binary.BigEndian.AppendUint32(p, 1000) // Size
		p = binary.BigEndian.AppendUint32(p, 0)    // DataOffset
		p = binary.BigEndian.AppendUint32(p, 0)    // dipendenze
	}
	return p
}

// inventario: RIP-S-30
// Un PNG con trasparenza perde il canale alfa come convert("RGB") di Pillow: i colori restano
// quelli scritti, non diventano neri. Un PNG in scala di grigi esce comunque a tre componenti.
func TestNormalizzaAlfaEGrigi(t *testing.T) {
	m := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for i := 0; i < len(m.Pix); i += 4 {
		m.Pix[i], m.Pix[i+1], m.Pix[i+2], m.Pix[i+3] = 200, 30, 30, 0
	}
	out, err := Normalizza(sintetiche.CodificaPNG(m))
	if err != nil {
		t.Fatal(err)
	}
	d, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b := sintetiche.Colore(d, 16, 16); r < 180 || g > 60 || b > 60 {
		t.Errorf("trasparente: (%d,%d,%d), atteso circa (200,30,30)", r, g, b)
	}
	g := image.NewGray(image.Rect(0, 0, 32, 32))
	if i := normalizzata(t, sintetiche.CodificaPNG(g)); i.Componenti != 3 {
		t.Errorf("grigi: %d componenti, attese 3", i.Componenti)
	}
}

// inventario: RIP-S-30
// Le dimensioni come thumbnail() di Pillow 9.4 (preserve_aspect_ratio, con round_aspect), e la
// riduzione del draft per un JPEG oltre il lato massimo.
func TestDimensioniComePillow(t *testing.T) {
	casi := []struct{ l, a, lato, el, ea int }{
		{6000, 4500, 4096, 4096, 3072},
		{3750, 5000, 4096, 3072, 4096},
		{640, 480, 4096, 640, 480},
		{4096, 4096, 4096, 4096, 4096},
		{4097, 1, 4096, 4096, 1},
		{1000, 750, 400, 400, 300},
		{120, 80, 400, 120, 80},
		{401, 3, 400, 400, 3},
		{1001, 333, 400, 400, 133},
		{333, 1001, 400, 133, 400},
		{10000, 3, 400, 400, 1},
	}
	for _, c := range casi {
		if l, a := dentroDelRiquadro(c.l, c.a, c.lato); l != c.el || a != c.ea {
			t.Errorf("%d×%d in %d: %d×%d, attese %d×%d", c.l, c.a, c.lato, l, a, c.el, c.ea)
		}
	}
	draft := []struct{ l, a, el, ea int }{
		{8160, 6120, 4080, 3060},
		{8944, 6708, 4472, 3354},
		{6000, 4500, 6000, 4500},
		{4097, 100, 4097, 100},
		{16001, 400, 4001, 100},
		{9001, 9001, 4501, 4501},
	}
	for _, c := range draft {
		if l, a := dopoIlDraft(c.l, c.a); l != c.el || a != c.ea {
			t.Errorf("draft %d×%d: %d×%d, attese %d×%d", c.l, c.a, l, a, c.el, c.ea)
		}
	}
}
