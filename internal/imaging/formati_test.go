package imaging

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/L0cutus/gestrip/internal/imaging/sintetiche"
)

func orientamentoDi(t *testing.T, cosa string, in []byte) int {
	t.Helper()
	te, err := leggiTesta(in)
	if err != nil {
		t.Fatalf("%s: %v", cosa, err)
	}
	return te.orientamento
}

func comprimi(b []byte) []byte {
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	_, _ = w.Write(b) // in memoria: non fallisce
	_ = w.Close()
	return z.Bytes()
}

// webpConEXIF è un WebP esteso (VP8X) con il chunk EXIF e i dati di un WebP semplice.
func webpConEXIF(exif []byte, flag byte) []byte {
	semplice := sintetiche.WebP(16, 16, 1)
	chunk := func(tipo string, d []byte) []byte {
		c := append([]byte(tipo), binary.LittleEndian.AppendUint32(nil, uint32(len(d)))...)
		c = append(c, d...)
		if len(d)%2 == 1 {
			c = append(c, 0)
		}
		return c
	}
	vp8x := chunk("VP8X", []byte{flag | 0x08, 0, 0, 0, 15, 0, 0, 15, 0, 0})
	corpo := append([]byte("WEBP"), vp8x...)
	corpo = append(corpo, semplice[12:]...)
	corpo = append(corpo, chunk("EXIF", exif)...)
	return append(append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(len(corpo)))...), corpo...)
}

// inventario: RIP-S-30
// Dove Pillow 9.4 legge l'orientamento: APP1 Exif del JPEG; eXIf, il tEXt «exif» e «Raw profile
// type exif» del PNG, con l'XMP (tEXt, zTXt, iTXt) se il tag manca; il chunk EXIF del WebP.
func TestOrientamentoComePillow(t *testing.T) {
	j := sintetiche.JPEG(sintetiche.Gradiente(16, 16, 1), sintetiche.OpzioniJPEG{Orientamento: 6})
	if o := orientamentoDi(t, "JPEG", j); o != 6 {
		t.Errorf("JPEG: %d", o)
	}
	// Vale il primo APP1 Exif.
	doppio := sintetiche.SegmentoJPEG(j, 0xE1, append([]byte("Exif\x00\x00"), sintetiche.TIFF(3)...))
	if o := orientamentoDi(t, "JPEG con due EXIF", doppio); o != 3 {
		t.Errorf("JPEG con due EXIF: %d, atteso il primo (3)", o)
	}
	p := sintetiche.PNG(16, 16, 1)
	xmp := func(o string) string { return `<x:xmpmeta><rdf:Description tiff:Orientation="` + o + `"/></x:xmpmeta>` }
	for _, c := range []struct {
		cosa string
		in   []byte
		o    int
	}{
		{"PNG senza niente", p, 0},
		{"PNG eXIf", sintetiche.ConChunkPNG(p, "eXIf", sintetiche.TIFF(8)), 8},
		{"PNG tEXt exif", sintetiche.ConChunkPNG(p, "tEXt", append([]byte("exif\x00"), sintetiche.TIFF(5)...)), 5},
		{"PNG Raw profile", sintetiche.ConChunkPNG(p, "tEXt", []byte("Raw profile type exif\x00\nexif\n      44\n"+hex.EncodeToString(sintetiche.TIFF(7)))), 7},
		{"PNG XMP tEXt", sintetiche.ConChunkPNG(p, "tEXt", []byte("XML:com.adobe.xmp\x00"+xmp("6"))), 6},
		{"PNG XMP iTXt", sintetiche.ConChunkPNG(p, "iTXt", []byte("XML:com.adobe.xmp\x00\x00\x00\x00\x00"+xmp("3"))), 3},
		{"PNG XMP iTXt compresso", sintetiche.ConChunkPNG(p, "iTXt", append([]byte("XML:com.adobe.xmp\x00\x01\x00\x00\x00"), comprimi([]byte(xmp("2")))...)), 2},
		{"PNG XMP zTXt", sintetiche.ConChunkPNG(p, "zTXt", append([]byte("XML:com.adobe.xmp\x00\x00"), comprimi([]byte(xmp("4")))...)), 4},
		{"PNG XMP con EXIF senza orientamento", sintetiche.ConChunkPNG(sintetiche.ConChunkPNG(p, "eXIf", sintetiche.TIFF(0)), "tEXt", []byte("XML:com.adobe.xmp\x00"+xmp("6"))), 6},
		{"PNG EXIF batte XMP", sintetiche.ConChunkPNG(sintetiche.ConChunkPNG(p, "eXIf", sintetiche.TIFF(2)), "tEXt", []byte("XML:com.adobe.xmp\x00"+xmp("6"))), 2},
		{"WebP EXIF", webpConEXIF(sintetiche.TIFF(6), 0), 6},
		{"WebP EXIF con prefisso", webpConEXIF(append([]byte("Exif\x00\x00"), sintetiche.TIFF(8)...), 0), 8},
		{"EXIF troncato", sintetiche.ConChunkPNG(p, "eXIf", sintetiche.TIFF(6)[:20]), 0},
	} {
		if o := orientamentoDi(t, c.cosa, c.in); o != c.o {
			t.Errorf("%s: %d, atteso %d", c.cosa, o, c.o)
		}
	}
	if o := orientamentoDi(t, "JPEG", sintetiche.JPEG(sintetiche.Gradiente(16, 16, 1), sintetiche.OpzioniJPEG{})); o != 0 {
		t.Errorf("JPEG senza orientamento: %d", o)
	}
}

// inventario: RIP-S-30
// Un blocco EXIF che non comincia come un TIFF fa fallire getexif, e nello standalone la foto è
// rifiutata; un WebP animato con due fotogrammi è rifiutato.
func TestEXIFGuastoEAnimazioneRifiutati(t *testing.T) {
	j := sintetiche.JPEG(sintetiche.Gradiente(16, 16, 1), sintetiche.OpzioniJPEG{SenzaMetadati: true})
	guasto := sintetiche.SegmentoJPEG(j, 0xE1, []byte("Exif\x00\x00non un tiff"))
	if _, err := Normalizza(guasto); !errors.Is(err, ErrImmagine) {
		t.Errorf("EXIF guasto: %v", err)
	}
	vuoto := sintetiche.SegmentoJPEG(j, 0xE1, []byte("Exif\x00\x00"))
	if _, err := Normalizza(vuoto); err != nil {
		t.Errorf("EXIF vuoto: %v", err)
	}
	if _, err := Normalizza(webpConEXIF(sintetiche.TIFF(0), 0)); err != nil {
		t.Errorf("WebP esteso fermo: %v", err)
	}
	anim := webpConEXIF(nil, 0x02)
	anim = append(anim, []byte("ANMF\x00\x00\x00\x00ANMF\x00\x00\x00\x00")...)
	binary.LittleEndian.PutUint32(anim[4:], uint32(len(anim)-8))
	if te, err := leggiTesta(anim); err != nil || te.fotogrammi != 2 {
		t.Errorf("WebP animato: %+v %v", te, err)
	}
	if _, err := Normalizza(anim); !errors.Is(err, ErrImmagine) {
		t.Errorf("WebP animato: %v", err)
	}
}
