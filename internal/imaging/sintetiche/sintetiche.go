// Package sintetiche fa le immagini inventate dei test delle foto, come PhotoFixture dello
// standalone (test/support/photo_fixture.ex): gradienti con un po' di rumore deterministico, e per
// il JPEG i metadati che scrive un telefono (EXIF con il produttore e, se chiesto, l'orientamento,
// e un profilo ICC). Mai foto vere nel repo. Lo usano solo i test.
package sintetiche

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"

	"github.com/gen2brain/webp"
)

// Gradiente è un'immagine RGB di l × a: un gradiente con un rumore che dipende da seme, così due
// semi danno byte diversi e la miniatura pesa molto meno dell'originale.
func Gradiente(l, a int, seme uint32) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, l, a))
	s := seme*2654435761 + 1
	for y := 0; y < a; y++ {
		for x := 0; x < l; x++ {
			s ^= s << 13
			s ^= s >> 17
			s ^= s << 5
			r := byte(s % 40)
			i := m.PixOffset(x, y)
			m.Pix[i] = byte(x*5+y) + r
			m.Pix[i+1] = byte(x+y*3) + r/2
			m.Pix[i+2] = byte(x*2+y*2) + r/3
			m.Pix[i+3] = 0xff
		}
	}
	return m
}

// Liscio è un gradiente senza rumore, che si codifica in fretta anche a 60 MP (come il gradiente
// lineare di Pillow nella fixture dello standalone).
func Liscio(l, a int) *image.YCbCr {
	m := image.NewYCbCr(image.Rect(0, 0, l, a), image.YCbCrSubsampleRatio420)
	for y := 0; y < a; y++ {
		for x := 0; x < l; x++ {
			m.Y[m.YOffset(x, y)] = byte(x * 255 / l)
		}
	}
	for i := range m.Cb {
		m.Cb[i], m.Cr[i] = 100, 160
	}
	return m
}

// PNG è Gradiente in PNG.
func PNG(l, a int, seme uint32) []byte { return CodificaPNG(Gradiente(l, a, seme)) }

// CodificaPNG scrive m in PNG.
func CodificaPNG(m image.Image) []byte {
	var b bytes.Buffer
	e := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := e.Encode(&b, m); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// WebP è Gradiente in WebP con perdita.
func WebP(l, a int, seme uint32) []byte {
	var b bytes.Buffer
	if err := webp.Encode(&b, Gradiente(l, a, seme), webp.Options{Quality: 80, Method: 4}); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// OpzioniJPEG: Orientamento è il tag EXIF (0: nessuno); Pad, se non è 0, porta il file a
// esattamente Pad byte con segmenti di commento, sempre un'immagine valida (per stare sul limite
// d'ingresso); SenzaMetadati toglie EXIF e ICC.
type OpzioniJPEG struct {
	Orientamento  int
	Pad           int
	SenzaMetadati bool
}

// JPEG è m in JPEG qualità 92, come la fixture dello standalone, con EXIF (produttore «Synthetic
// phone» e l'orientamento) e un profilo ICC.
func JPEG(m image.Image, o OpzioniJPEG) []byte {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, m, &jpeg.Options{Quality: 92}); err != nil {
		panic(err)
	}
	dati := b.Bytes()
	var testa []byte
	if !o.SenzaMetadati {
		testa = append(testa, segmento(0xE1, append([]byte("Exif\x00\x00"), TIFF(o.Orientamento)...))...)
		testa = append(testa, segmento(0xE2, append([]byte("ICC_PROFILE\x00\x01\x01"), "synthetic icc profile"...))...)
	}
	if o.Pad != 0 {
		manca := o.Pad - len(dati) - len(testa)
		if manca < 4 {
			panic("sintetiche: pad troppo piccolo")
		}
		for manca > 0 {
			prendi := min(manca, 65537)
			if r := manca - prendi; r > 0 && r < 4 {
				prendi = manca - 4
			}
			testa = append(testa, segmento(0xFE, make([]byte, prendi-4))...)
			manca -= prendi
		}
	}
	return append(append(append([]byte{}, dati[:2]...), testa...), dati[2:]...)
}

// Segmento è un segmento JPEG con il marcatore 0xFF m e il carico p.
func segmento(m byte, p []byte) []byte {
	s := []byte{0xFF, m, 0, 0}
	binary.BigEndian.PutUint16(s[2:], uint16(len(p)+2))
	return append(s, p...)
}

// SegmentoJPEG inserisce dopo l'inizio del JPEG j un segmento con il marcatore 0xFF m.
func SegmentoJPEG(j []byte, m byte, p []byte) []byte {
	return append(append(append([]byte{}, j[:2]...), segmento(m, p)...), j[2:]...)
}

// TIFF è un blocco EXIF (TIFF little endian) con il produttore e, se non è 0, l'orientamento.
func TIFF(orientamento int) []byte {
	produttore := []byte("Synthetic phone\x00")
	n := uint16(1)
	if orientamento != 0 {
		n = 2
	}
	t := []byte("II*\x00\x08\x00\x00\x00")
	t = binary.LittleEndian.AppendUint16(t, n)
	fine := 8 + 2 + 12*int(n) + 4
	// 0x010F Make, ASCII
	t = binary.LittleEndian.AppendUint16(t, 0x010F)
	t = binary.LittleEndian.AppendUint16(t, 2)
	t = binary.LittleEndian.AppendUint32(t, uint32(len(produttore)))
	t = binary.LittleEndian.AppendUint32(t, uint32(fine))
	if orientamento != 0 {
		// 0x0112 Orientation, SHORT
		t = binary.LittleEndian.AppendUint16(t, 0x0112)
		t = binary.LittleEndian.AppendUint16(t, 3)
		t = binary.LittleEndian.AppendUint32(t, 1)
		t = binary.LittleEndian.AppendUint16(t, uint16(orientamento))
		t = binary.LittleEndian.AppendUint16(t, 0)
	}
	t = binary.LittleEndian.AppendUint32(t, 0)
	return append(t, produttore...)
}

// ConChunkPNG aggiunge a p, subito dopo IHDR, un chunk di tipo tipo con dati d.
func ConChunkPNG(p []byte, tipo string, d []byte) []byte {
	c := binary.BigEndian.AppendUint32(nil, uint32(len(d)))
	c = append(c, tipo...)
	c = append(c, d...)
	c = binary.BigEndian.AppendUint32(c, crc32.ChecksumIEEE(append([]byte(tipo), d...)))
	dopoIHDR := 8 + 8 + 13 + 4
	return append(append(append([]byte{}, p[:dopoIHDR]...), c...), p[dopoIHDR:]...)
}

// InfoJPEG è quello che dicono le intestazioni di un JPEG fino alla scansione, come
// PhotoFixture.jpeg_info: dimensioni dal frame, Base (un frame SOF0 e nessun progressivo),
// presenza di EXIF e ICC, il primo valore della tabella di quantizzazione della luminanza (3 per
// la qualità 89-92 contro il 16 della tabella standard) e il numero di componenti.
type InfoJPEG struct {
	Larghezza, Altezza int
	Base               bool
	EXIF, ICC          bool
	LumaQ0             int
	Componenti         int
}

// LeggiJPEG dà InfoJPEG, o false se j non è un JPEG.
func LeggiJPEG(j []byte) (InfoJPEG, bool) {
	var i InfoJPEG
	if len(j) < 2 || j[0] != 0xFF || j[1] != 0xD8 {
		return i, false
	}
	sof0, sof2 := false, false
	q0 := -1
	r := j[2:]
	for len(r) >= 4 && r[0] == 0xFF && r[1] != 0xDA {
		m := r[1]
		n := int(binary.BigEndian.Uint16(r[2:]))
		if n < 2 || len(r) < 2+n {
			break
		}
		p := r[4 : 2+n]
		switch {
		case m == 0xC0 || m == 0xC2:
			if m == 0xC0 {
				sof0 = true
			} else {
				sof2 = true
			}
			if len(p) >= 6 {
				i.Altezza = int(binary.BigEndian.Uint16(p[1:]))
				i.Larghezza = int(binary.BigEndian.Uint16(p[3:]))
				i.Componenti = int(p[5])
			}
		case m == 0xE1 && bytes.HasPrefix(p, []byte("Exif")):
			i.EXIF = true
		case m == 0xE2 && bytes.HasPrefix(p, []byte("ICC_PROFILE")):
			i.ICC = true
		case m == 0xDB && q0 < 0 && len(p) >= 2 && p[0] == 0:
			q0 = int(p[1])
		}
		r = r[2+n:]
	}
	i.Base = sof0 && !sof2
	i.LumaQ0 = q0
	return i, true
}

// DimensioniWebP sono le dimensioni che dichiara un WebP con perdita semplice (chunk «VP8 »
// subito dopo l'intestazione), come PhotoFixture.webp_size; false per ogni altra forma.
func DimensioniWebP(w []byte) (int, int, bool) {
	if len(w) < 30 || string(w[:4]) != "RIFF" || string(w[8:16]) != "WEBPVP8 " {
		return 0, 0, false
	}
	if w[23] != 0x9D || w[24] != 0x01 || w[25] != 0x2A {
		return 0, 0, false
	}
	return int(binary.LittleEndian.Uint16(w[26:]) & 0x3FFF), int(binary.LittleEndian.Uint16(w[28:]) & 0x3FFF), true
}

// Colore è il colore del punto (x, y) di un'immagine decodificata, in RGB a 8 bit.
func Colore(m image.Image, x, y int) (r, g, b uint8) {
	c := color.RGBAModel.Convert(m.At(x, y)).(color.RGBA)
	return c.R, c.G, c.B
}
