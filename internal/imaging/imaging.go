// Package imaging normalizza le foto e ne fa le miniature, senza Python e senza CGO (tema H,
// PIANO §2.2; RIP-S-30, RIP-S-33). Copia gli script dello standalone a go-baseline:
// priv/normalize_image.py (MediaStore.normalize/1) e priv/thumbnail_image.py
// (MediaStore.thumbnail/1), che girano con Pillow 9.4 (Debian bookworm).
package imaging

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"

	"github.com/gen2brain/webp"
	xwebp "golang.org/x/image/webp"
)

// I limiti degli script dello standalone.
const (
	// MaxByteIngresso è quanto può pesare una foto in arrivo (max_input_bytes/0, slice 191).
	MaxByteIngresso = 25 * 1024 * 1024
	// MaxByteUscita è il massimo di una foto normalizzata, e di quella che si legge per la
	// miniatura (lo stesso limite di MediaStore.read_object).
	MaxByteUscita = 16 * 1024 * 1024
	// MaxPixel: oltre, l'intestazione basta a rifiutare la foto (Image.MAX_IMAGE_PIXELS).
	MaxPixel = 60_000_000
	// LatoMassimo è il lato lungo che si tiene: i 4032/4000/4080 px di un telefono restano.
	LatoMassimo = 4096
	// Pavimento è il lato lungo minimo che un JPEG oltre LatoMassimo tiene dopo la riduzione
	// della decodifica (draft di Pillow, DRAFT_FLOOR).
	Pavimento = 4000
	// QualitaJPEG è la qualità del JPEG normalizzato.
	QualitaJPEG = 90
	// LatoMiniatura è il riquadro della miniatura.
	LatoMiniatura = 400
	// QualitaMiniatura e MetodoMiniatura sono le opzioni WebP della miniatura.
	QualitaMiniatura = 70
	MetodoMiniatura  = 4
	// MaxPixelMiniatura: la foto più grande che la normalizzazione salva è 4096 × 4096.
	MaxPixelMiniatura = LatoMassimo * LatoMassimo
	// MaxByteMiniatura è il massimo di una miniatura.
	MaxByteMiniatura = 4 * 1024 * 1024
)

// ErrImmagine è ogni rifiuto: formato, dimensioni, pixel, fotogrammi, byte (:invalid_image dello
// standalone, e :thumbnail_failed per la miniatura). Non dice perché, come lo standalone.
var ErrImmagine = errors.New("imaging: immagine non valida")

// Normalizza decodifica una foto ferma (JPEG, PNG o WebP, fino a MaxByteIngresso) e la riscrive
// come JPEG di base senza metadati (niente EXIF, GPS, ICC né commenti), raddrizzata secondo
// l'EXIF, alla sua risoluzione fino a un lato lungo di LatoMassimo, qualità QualitaJPEG; il
// risultato pesa al più MaxByteUscita. Come normalize_image.py: un JPEG oltre il lato massimo
// prende le dimensioni della riduzione della decodifica di Pillow (dopoIlDraft) prima di stare nel
// riquadro, così un 50 MP (8160×6120) esce 4080×3060.
//
// In produzione si chiama sempre attraverso Lavoratore (il processo figlio con i limiti), mai
// direttamente in un processo che serve le pagine: questa funzione decodifica foto di chiunque.
//
// Differenze dichiarate (tema H): il ridimensionamento è CatmullRom e non Lanczos con la riduzione
// del DCT, e il JPEG non ha le tabelle di Huffman ottimizzate: cambiano un po' i pixel e i byte,
// non le dimensioni.
func Normalizza(in []byte) ([]byte, error) {
	if len(in) == 0 || len(in) > MaxByteIngresso {
		return nil, ErrImmagine
	}
	t, cfg, err := apri(in, MaxPixel)
	if err != nil {
		return nil, ErrImmagine
	}
	l, a := cfg.Width, cfg.Height
	if t.formato == formatoJPEG && max(l, a) > LatoMassimo {
		l, a = dopoIlDraft(l, a)
	}
	// Il riquadro è quadrato: stare nel riquadro prima di raddrizzare dà le stesse dimensioni.
	l, a = dentroDelRiquadro(l, a, LatoMassimo)
	src, err := decodifica(t.formato, in)
	if err != nil {
		return nil, ErrImmagine
	}
	m := orienta(scala(src, l, a), t.orientamento)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, m, &jpeg.Options{Quality: QualitaJPEG}); err != nil {
		return nil, ErrImmagine
	}
	if out.Len() == 0 || out.Len() > MaxByteUscita {
		return nil, ErrImmagine
	}
	return out.Bytes(), nil
}

// Miniatura fa da una foto salvata (fino a MaxByteUscita e MaxPixelMiniatura) un WebP con perdita
// senza metadati che sta in un riquadro di LatoMiniatura, qualità QualitaMiniatura, metodo
// MetodoMiniatura, come thumbnail_image.py: prima raddrizzata secondo l'EXIF (una foto importata
// può averlo), poi ridotta; una foto piccola non si ingrandisce.
//
// In produzione si chiama sempre attraverso Lavoratore, come Normalizza.
func Miniatura(in []byte) ([]byte, error) {
	if len(in) == 0 || len(in) > MaxByteUscita {
		return nil, ErrImmagine
	}
	t, cfg, err := apri(in, MaxPixelMiniatura)
	if err != nil {
		return nil, ErrImmagine
	}
	l, a := cfg.Width, cfg.Height
	scambia := t.orientamento >= 5 && t.orientamento <= 8
	if scambia {
		l, a = a, l
	}
	l, a = dentroDelRiquadro(l, a, LatoMiniatura)
	if scambia {
		l, a = a, l
	}
	src, err := decodifica(t.formato, in)
	if err != nil {
		return nil, ErrImmagine
	}
	m := orienta(scala(src, l, a), t.orientamento)
	var out bytes.Buffer
	if err := webp.Encode(&out, m, webp.Options{Quality: QualitaMiniatura, Method: MetodoMiniatura}); err != nil {
		return nil, ErrImmagine
	}
	if out.Len() == 0 || out.Len() > MaxByteMiniatura || !WebPIntero(out.Bytes()) {
		return nil, ErrImmagine
	}
	return out.Bytes(), nil
}

// WebPIntero dice se b è un WebP intero: l'intestazione RIFF dichiara proprio la sua lunghezza
// (webp?/1 della cache dello standalone).
func WebPIntero(b []byte) bool {
	return len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP" &&
		int(uint32(b[4])|uint32(b[5])<<8|uint32(b[6])<<16|uint32(b[7])<<24) == len(b)-8
}

// apri legge la testa e le dimensioni senza decodificare: un fotogramma solo e al più maxPixel
// pixel (la bomba di decompressione di Pillow), poi verifica quello che verify() verifica.
func apri(in []byte, maxPixel int) (testa, image.Config, error) {
	t, err := leggiTesta(in)
	if err != nil || t.fotogrammi != 1 {
		return t, image.Config{}, errFormato
	}
	var cfg image.Config
	r := bytes.NewReader(in)
	switch t.formato {
	case formatoJPEG:
		cfg, err = jpeg.DecodeConfig(r)
	case formatoPNG:
		cfg, err = png.DecodeConfig(r)
	case formatoWebP:
		cfg, err = xwebp.DecodeConfig(r)
	}
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > int64(maxPixel) {
		return t, image.Config{}, errFormato
	}
	return t, cfg, nil
}

// decodifica decodifica la foto con il decoder del suo formato (mai image.Decode: niente formati
// registrati da altri pacchetti).
func decodifica(f formato, in []byte) (image.Image, error) {
	r := bytes.NewReader(in)
	switch f {
	case formatoJPEG:
		return jpeg.Decode(r)
	case formatoPNG:
		return png.Decode(r)
	case formatoWebP:
		return xwebp.Decode(r)
	}
	return nil, errFormato
}

// opaca toglie il canale alfa come convert("RGB") di Pillow: i colori restano quelli scritti,
// anche dove la foto è trasparente (niente sfondo nero o bianco). Dove può lavora sul posto, per
// non raddoppiare la memoria di una foto grande.
func opaca(m image.Image) image.Image {
	switch x := m.(type) {
	case *image.NRGBA:
		for i := 3; i < len(x.Pix); i += 4 {
			x.Pix[i] = 0xff
		}
		return x
	case *image.NRGBA64:
		for i := 6; i < len(x.Pix); i += 8 {
			x.Pix[i], x.Pix[i+1] = 0xff, 0xff
		}
		return x
	case *image.NYCbCrA:
		return &x.YCbCr
	case *image.Paletted:
		p := make(color.Palette, len(x.Palette))
		for i, c := range x.Palette {
			n := color.NRGBAModel.Convert(c).(color.NRGBA)
			n.A = 0xff
			p[i] = n
		}
		q := *x
		q.Palette = p
		return &q
	case *image.RGBA, *image.RGBA64:
		// Premoltiplicati: in PNG non capitano (il decoder dà NRGBA per i colori con alfa).
		return x
	}
	return m
}

// scala porta la foto a l × a in un RGBA nuovo (tre componenti nel JPEG anche da una foto in
// scala di grigi, come convert("RGB")): una copia se le dimensioni non cambiano, altrimenti
// CatmullRom (tema H).
func scala(src image.Image, l, a int) *image.RGBA {
	src = opaca(src)
	b := src.Bounds()
	if b.Dx() == l && b.Dy() == a {
		dst := image.NewRGBA(image.Rect(0, 0, l, a))
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
		return dst
	}
	return ridimensiona(src, l, a)
}
