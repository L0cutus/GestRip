package sintetiche

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
)

// Le foto grandi dei test sui limiti del figlio, fatte senza tenere l'immagine in memoria: un JPEG
// scritto a mano (piatto: ogni blocco ha solo il coefficiente DC a zero, che la libreria standard
// di Go non sa scrivere in progressivo né in 4:4:4) e un PNG scritto riga per riga.

// OpzioniJPEGPiatto: Progressivo scrive un solo passaggio DC (SOF2), altrimenti un JPEG di base
// (SOF0); Campioni è il campionamento della luminanza (1: 4:4:4, 2: 4:2:0); Componenti 3 (YCbCr)
// o 4 (CMYK, con il segmento Adobe).
type OpzioniJPEGPiatto struct {
	Progressivo bool
	Campioni    int
	Componenti  int
}

// JPEGPiatto è un JPEG grigio uniforme di l × a, valido per Go e per libjpeg.
func JPEGPiatto(l, a int, o OpzioniJPEGPiatto) []byte {
	if o.Campioni == 0 {
		o.Campioni = 1
	}
	if o.Componenti == 0 {
		o.Componenti = 3
	}
	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xD8})
	if o.Componenti == 4 {
		b.Write(segmento(0xEE, []byte("Adobe\x00\x64\x00\x00\x00\x00\x00")))
	}
	q := make([]byte, 65)
	for i := 1; i < 65; i++ {
		q[i] = 1
	}
	b.Write(segmento(0xDB, q))
	sof := []byte{8, byte(a >> 8), byte(a), byte(l >> 8), byte(l), byte(o.Componenti)}
	for c := 1; c <= o.Componenti; c++ {
		s := byte(0x11)
		if c == 1 {
			s = byte(o.Campioni<<4 | o.Campioni)
		}
		sof = append(sof, byte(c), s, 0)
	}
	m := byte(0xC0)
	if o.Progressivo {
		m = 0xC2
	}
	b.Write(segmento(m, sof))
	// DC: un solo codice («0», lunghezza 1) per la categoria 0. AC: un solo codice per EOB.
	dht := append([]byte{0x00, 1}, make([]byte, 15)...)
	dht = append(dht, 0)
	b.Write(segmento(0xC4, dht))
	if !o.Progressivo {
		ac := append([]byte{0x10, 1}, make([]byte, 15)...)
		ac = append(ac, 0)
		b.Write(segmento(0xC4, ac))
	}
	sos := []byte{byte(o.Componenti)}
	for c := 1; c <= o.Componenti; c++ {
		sos = append(sos, byte(c), 0x00)
	}
	if o.Progressivo {
		sos = append(sos, 0, 0, 0)
	} else {
		sos = append(sos, 0, 63, 0)
	}
	b.Write(segmento(0xDA, sos))
	mcu := 8 * o.Campioni
	blocchi := o.Campioni*o.Campioni + o.Componenti - 1
	bit := ((l + mcu - 1) / mcu) * ((a + mcu - 1) / mcu) * blocchi
	if !o.Progressivo {
		bit *= 2 // DC e EOB
	}
	b.Write(make([]byte, bit/8))
	if r := bit % 8; r != 0 {
		b.WriteByte(byte(0xFF >> r)) // gli ultimi bit a 1 come riempimento
	}
	b.Write([]byte{0xFF, 0xD9})
	return b.Bytes()
}

// PNGRighe è un PNG di l × a con il tipo di colore e la profondità dati (IHDR), scritto riga per
// riga: riga(y, r) riempie i byte della riga y senza il filtro.
func PNGRighe(l, a int, colore, profondita byte, riga func(y int, r []byte)) []byte {
	canali := map[byte]int{0: 1, 2: 3, 3: 1, 4: 2, 6: 4}[colore]
	n := (l*canali*int(profondita) + 7) / 8
	var z bytes.Buffer
	w, err := zlib.NewWriterLevel(&z, zlib.BestSpeed)
	if err != nil {
		panic(err)
	}
	r := make([]byte, n+1)
	for y := 0; y < a; y++ {
		r[0] = 0
		riga(y, r[1:])
		if _, err := w.Write(r); err != nil {
			panic(err)
		}
	}
	if err := w.Close(); err != nil {
		panic(err)
	}
	out := []byte("\x89PNG\r\n\x1a\n")
	ihdr := binary.BigEndian.AppendUint32(nil, uint32(l))
	ihdr = binary.BigEndian.AppendUint32(ihdr, uint32(a))
	ihdr = append(ihdr, profondita, colore, 0, 0, 0)
	out = chunkPNG(out, "IHDR", ihdr)
	if colore == 3 {
		pal := make([]byte, 256*3)
		for i := range pal {
			pal[i] = byte(i / 3)
		}
		out = chunkPNG(out, "PLTE", pal)
	}
	out = chunkPNG(out, "IDAT", z.Bytes())
	return chunkPNG(out, "IEND", nil)
}

func chunkPNG(out []byte, tipo string, d []byte) []byte {
	out = binary.BigEndian.AppendUint32(out, uint32(len(d)))
	out = append(out, tipo...)
	out = append(out, d...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(append([]byte(tipo), d...)))
}

// Sfumata è una riga per PNGRighe: byte che crescono con x e y (si comprimono bene).
func Sfumata(y int, r []byte) {
	for i := range r {
		r[i] = byte(i/97 + y/89)
	}
}
