package imaging

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"github.com/L0cutus/gestrip/internal/imaging/sintetiche"
	"github.com/gen2brain/webp"
)

// I casi del giro di correzioni della review della 036, verificati sul codice di Pillow 9.4
// (preso dall'immagine dello standalone) o con il suo script nell'immagine.

// tiffUno è un blocco EXIF (TIFF) con il solo tag Orientation, del tipo e con i byte dati; i byte
// oltre 4 stanno dopo l'IFD.
func tiffUno(o binary.AppendByteOrder, prefisso string, tipo uint16, quanti uint32, dati []byte) []byte {
	t := []byte(prefisso)
	t = o.AppendUint32(t, 8)
	t = o.AppendUint16(t, 1)
	t = o.AppendUint16(t, 0x0112)
	t = o.AppendUint16(t, tipo)
	t = o.AppendUint32(t, quanti)
	if len(dati) > 4 {
		t = o.AppendUint32(t, 8+2+12+4)
	} else {
		t = append(t, append(append([]byte{}, dati...), make([]byte, 4-len(dati))...)...)
	}
	t = o.AppendUint32(t, 0)
	if len(dati) > 4 {
		t = append(t, dati...)
	}
	return t
}

func conEXIF(t *testing.T, tiff []byte) int {
	t.Helper()
	p := sintetiche.ConChunkPNG(sintetiche.PNG(16, 16, 1), "eXIf", tiff)
	te, err := leggiTesta(p)
	if err != nil {
		t.Fatalf("leggiTesta: %v", err)
	}
	return te.orientamento
}

// inventario: RIP-S-30
// Il tipo del tag d'orientamento come in Pillow 9.4: BYTE dà bytes e non ruota; SHORT, LONG,
// SBYTE, SSHORT, SLONG, IFD e LONG8 (letto dall'offset, 8 byte) ruotano; FLOAT, DOUBLE e i
// razionali solo con un valore intero (6.5 no, 12/2 sì); con più valori vale il primo; l'intestazione
// «MM\x2A\x00», sbagliata ma accettata, è big endian.
func TestTipiDellOrientamento(t *testing.T) {
	le, be := binary.LittleEndian, binary.BigEndian
	f32 := le.AppendUint32(nil, math.Float32bits(6))
	f32m := le.AppendUint32(nil, math.Float32bits(6.5))
	f64 := le.AppendUint64(nil, math.Float64bits(6))
	f64m := le.AppendUint64(nil, math.Float64bits(6.5))
	for _, c := range []struct {
		cosa string
		tiff []byte
		o    int
	}{
		{"BYTE", tiffUno(le, "II*\x00", 1, 1, []byte{6}), 0},
		{"SBYTE", tiffUno(le, "II*\x00", 6, 1, []byte{6}), 6},
		{"SHORT", tiffUno(le, "II*\x00", 3, 1, le.AppendUint16(nil, 6)), 6},
		{"SHORT con due valori", tiffUno(le, "II*\x00", 3, 2, le.AppendUint16(le.AppendUint16(nil, 3), 6)), 3},
		{"SSHORT", tiffUno(le, "II*\x00", 8, 1, le.AppendUint16(nil, 6)), 6},
		{"LONG", tiffUno(le, "II*\x00", 4, 1, le.AppendUint32(nil, 6)), 6},
		{"SLONG", tiffUno(le, "II*\x00", 9, 1, le.AppendUint32(nil, 6)), 6},
		{"IFD", tiffUno(le, "II*\x00", 13, 1, le.AppendUint32(nil, 6)), 6},
		{"LONG8", tiffUno(le, "II*\x00", 16, 1, le.AppendUint64(nil, 6)), 6},
		{"FLOAT 6", tiffUno(le, "II*\x00", 11, 1, f32), 6},
		{"FLOAT 6.5", tiffUno(le, "II*\x00", 11, 1, f32m), 0},
		{"DOUBLE 6", tiffUno(le, "II*\x00", 12, 1, f64), 6},
		{"DOUBLE 6.5", tiffUno(le, "II*\x00", 12, 1, f64m), 0},
		{"RATIONAL 12/2", tiffUno(le, "II*\x00", 5, 1, le.AppendUint32(le.AppendUint32(nil, 12), 2)), 6},
		{"RATIONAL 13/2", tiffUno(le, "II*\x00", 5, 1, le.AppendUint32(le.AppendUint32(nil, 13), 2)), 0},
		{"UNDEFINED", tiffUno(le, "II*\x00", 7, 1, []byte{6}), 0},
		{"ASCII", tiffUno(le, "II*\x00", 2, 2, []byte("6\x00")), 0},
		{"MM\\x00\\x2A", tiffUno(be, "MM\x00*", 3, 1, be.AppendUint16(nil, 8)), 8},
		{"MM\\x2A\\x00", tiffUno(be, "MM*\x00", 3, 1, be.AppendUint16(nil, 8)), 8},
		{"II\\x00\\x2A", tiffUno(le, "II\x00*", 3, 1, le.AppendUint16(nil, 5)), 5},
	} {
		if o := conEXIF(t, c.tiff); o != c.o {
			t.Errorf("%s: %d, atteso %d", c.cosa, o, c.o)
		}
	}
	// BigTIFF: Pillow lo accetta come intestazione ma con 8 byte non legge l'offset
	// (struct.error), quindi getexif fallisce e la foto è rifiutata.
	p := sintetiche.ConChunkPNG(sintetiche.PNG(16, 16, 1), "eXIf", []byte("II+\x00\x08\x00\x00\x00\x00\x00\x00\x00"))
	if _, err := Normalizza(p); !errors.Is(err, ErrImmagine) {
		t.Errorf("BigTIFF: %v", err)
	}
	// Due eXIf: vale l'ultimo.
	due := sintetiche.ConChunkPNG(sintetiche.ConChunkPNG(sintetiche.PNG(16, 16, 1), "eXIf", sintetiche.TIFF(3)), "eXIf", sintetiche.TIFF(8))
	if te, _ := leggiTesta(due); te.orientamento != 3 {
		// ConChunkPNG inserisce subito dopo IHDR: l'ultimo nel file è TIFF(3).
		t.Errorf("due eXIf: %d, atteso l'ultimo (3)", te.orientamento)
	}
}

func zTXt(chiave string, n int) []byte {
	return append([]byte(chiave+"\x00\x00"), comprimi(make([]byte, n))...)
}

// inventario: RIP-S-30
// Testo del PNG come Pillow 9.4: uno zTXt che si apre oltre 1 MiB fa fallire l'apertura, 1 MiB
// esatto no; il testo di tutti i chunk oltre 64 MiB (MAX_TEXT_MEMORY) la fa fallire, 64 MiB no.
func TestTestoDelPNG(t *testing.T) {
	p := sintetiche.PNG(16, 16, 1)
	if _, err := Normalizza(sintetiche.ConChunkPNG(p, "zTXt", zTXt("Comment", maxTesto))); err != nil {
		t.Errorf("1 MiB: %v", err)
	}
	if _, err := Normalizza(sintetiche.ConChunkPNG(p, "zTXt", zTXt("Comment", maxTesto+1))); !errors.Is(err, ErrImmagine) {
		t.Errorf("1 MiB + 1: %v", err)
	}
	molti := func(n int) []byte {
		q := p
		for i := 0; i < n; i++ {
			q = sintetiche.ConChunkPNG(q, "zTXt", zTXt("Comment", maxTesto))
		}
		return q
	}
	if _, err := Normalizza(molti(64)); err != nil {
		t.Errorf("64 MiB di testo: %v", err)
	}
	if _, err := Normalizza(molti(65)); !errors.Is(err, ErrImmagine) {
		t.Errorf("65 MiB di testo: %v", err)
	}
	// Un tEXt con la chiave vuota Pillow lo scarta (anche dal conto).
	if te, err := leggiTesta(sintetiche.ConChunkPNG(p, "tEXt", []byte("\x00tiff:Orientation=\"6\""))); err != nil || te.orientamento != 0 {
		t.Errorf("chiave vuota: %+v %v", te, err)
	}
}

// inventario: RIP-S-30
// acTL: 2^31 fotogrammi è un PNG animato (rifiutato), oltre 2^31 «Invalid APNG» e immagine normale.
func TestAcTLAlConfine(t *testing.T) {
	actl := func(n uint32) []byte {
		return binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, n), 0)
	}
	p := sintetiche.PNG(16, 16, 1)
	rifiutata(t, "acTL 2^31", sintetiche.ConChunkPNG(p, "acTL", actl(0x80000000)))
	dimensioni(t, "acTL 2^31+1", normalizzata(t, sintetiche.ConChunkPNG(p, "acTL", actl(0x80000001))), 16, 16)
}

// inventario: RIP-S-30
// WebP: chunk ANMF senza il segno d'animazione, animato con i fotogrammi, troncato: Pillow 9.4 non
// lo apre (misurato con lo script nell'immagine dello standalone: «could not create decoder
// object»); un VP8X fermo sì.
func TestWebPCheNonSiApre(t *testing.T) {
	anmf := func(flag byte) []byte {
		a := webpConEXIF(nil, flag)
		a = append(a, []byte("ANMF\x00\x00\x00\x00ANMF\x00\x00\x00\x00")...)
		binary.LittleEndian.PutUint32(a[4:], uint32(len(a)-8))
		return a
	}
	rifiutata(t, "ANMF senza segno", anmf(0))
	rifiutata(t, "ANMF con il segno", anmf(0x02))
	w := webpConEXIF(nil, 0)
	rifiutata(t, "troncato", w[:len(w)-30])
	if _, err := Normalizza(w); err != nil {
		t.Errorf("VP8X fermo: %v", err)
	}
	if TipoRIFF := formatoDi([]byte("RIFF\x04\x00\x00\x00WAVEfmt ")); TipoRIFF != 0 {
		t.Errorf("RIFF non WEBP: %v", TipoRIFF)
	}
}

// inventario: RIP-S-30
// Il confine dei 60 MP: 8000×7500 (60 000 000 esatti) passa, 7750×7742 (60 000 500) no
// (60 000 001 non si scompone in due lati da JPEG).
func TestConfineSessantaMegapixel(t *testing.T) {
	if testing.Short() {
		t.Skip("60 MP")
	}
	// Oltre 4096 px il JPEG passa dal draft: 8000×7500 → 4000×3750.
	dimensioni(t, "60 MP esatti", normalizzata(t, sintetiche.JPEGPiatto(8000, 7500, sintetiche.OpzioniJPEGPiatto{Campioni: 2})), 4000, 3750)
	rifiutata(t, "60 MP + 500", sintetiche.JPEGPiatto(7750, 7742, sintetiche.OpzioniJPEGPiatto{Campioni: 2}))
}

// inventario: RIP-S-30
// Il draft è solo dei JPEG: un PNG 8160×6120 esce 4096×3072, non 4080×3060.
func TestNienteDraftSulPNG(t *testing.T) {
	if testing.Short() {
		t.Skip("50 MP")
	}
	p := sintetiche.PNGRighe(8160, 6120, 0, 8, sintetiche.Sfumata)
	dimensioni(t, "PNG 50 MP", normalizzata(t, p), 4096, 3072)
}

// inventario: RIP-S-33
// La miniatura si calcola sulla foto già raddrizzata, come exif_transpose prima di thumbnail: un
// 401×201 con l'orientamento 5 diventa 201×401 e poi 200×400 (non 201×400).
func TestMiniaturaRaddrizzataPrimaDelRiquadro(t *testing.T) {
	j := sintetiche.JPEG(sintetiche.Gradiente(401, 201, 1), sintetiche.OpzioniJPEG{Orientamento: 5})
	if l, a, _ := miniatura(t, j); l != 200 || a != 400 {
		t.Errorf("%d×%d, attese 200×400", l, a)
	}
}

// inventario: RIP-S-33
// La miniatura è il WebP con qualità 70 e metodo 4 dei pixel ridotti: stessi byte.
func TestMiniaturaQualitaEMetodo(t *testing.T) {
	in := salvata(t, 1000, 750, 4)
	m, err := Miniatura(in)
	if err != nil {
		t.Fatal(err)
	}
	src, err := decodifica(formatoJPEG, in)
	if err != nil {
		t.Fatal(err)
	}
	var atteso bytes.Buffer
	if err := webp.Encode(&atteso, scala(src, 400, 300), webp.Options{Quality: 70, Method: 4}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(m, atteso.Bytes()) {
		t.Errorf("miniatura di %d byte, attesi %d (qualità 70, metodo 4)", len(m), atteso.Len())
	}
}
