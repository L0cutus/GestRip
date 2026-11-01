package imaging

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Quello che Pillow 9.4 (Debian bookworm, l'immagine dello standalone) legge di una foto prima di
// decodificarla: il formato dalla firma (Image.open con formats=["JPEG", "PNG", "WEBP"], mai
// dall'estensione), quanti fotogrammi ha (n_frames) e l'orientamento EXIF (getexif, che
// exif_transpose usa).

type formato int

const (
	formatoJPEG formato = iota + 1
	formatoPNG
	formatoWebP
)

// errFormato è un rifiuto della lettura: si traduce sempre in ErrImmagine.
var errFormato = errors.New("imaging: formato")

// testa è quello che si sa della foto prima di decodificarla.
type testa struct {
	formato      formato
	fotogrammi   int
	orientamento int // 0 o 1: niente da fare
}

// formatoDi riconosce la firma, come gli accept dei tre plugin di Pillow.
func formatoDi(b []byte) formato {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return formatoJPEG
	case len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return formatoPNG
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return formatoWebP
	}
	return 0
}

// leggiTesta legge formato, fotogrammi e orientamento; errFormato per ciò che Pillow non apre o
// che fa fallire exif_transpose.
func leggiTesta(b []byte) (testa, error) {
	t := testa{formato: formatoDi(b), fotogrammi: 1}
	var exif []byte
	var err error
	switch t.formato {
	case formatoJPEG:
		exif, t.fotogrammi, err = testaJPEG(b)
	case formatoPNG:
		var xmp string
		exif, xmp, t.fotogrammi, err = testaPNG(b)
		if err == nil {
			t.orientamento, err = orientamentoConXMP(exif, xmp)
			return t, err
		}
	case formatoWebP:
		exif, t.fotogrammi, err = testaWebP(b)
	default:
		return t, errFormato
	}
	if err != nil {
		return t, err
	}
	t.orientamento, err = orientamentoConXMP(exif, "")
	return t, err
}

// --- JPEG ---

// testaJPEG legge i segmenti fino alla scansione: il primo APP1 «Exif\0» (info["exif"]) e
// l'ultimo APP2 «MPF\0» (info["mp"]), da cui jpeg_factory decide se è un MPO con più immagini.
func testaJPEG(b []byte) (exif []byte, fotogrammi int, err error) {
	var mp []byte
	r := b[2:]
	for len(r) >= 2 {
		if r[0] != 0xFF {
			break
		}
		m := r[1]
		if m == 0xFF {
			r = r[1:]
			continue
		}
		if m == 0xDA || m == 0xD9 || len(r) < 4 {
			break
		}
		n := int(binary.BigEndian.Uint16(r[2:]))
		if n < 2 || len(r) < 2+n {
			break
		}
		p := r[4 : 2+n]
		switch {
		case m == 0xE1 && exif == nil && bytes.HasPrefix(p, []byte("Exif\x00")):
			exif = p
		case m == 0xE2 && bytes.HasPrefix(p, []byte("MPF\x00")):
			mp = p[4:]
		}
		r = r[2+n:]
	}
	fotogrammi, err = immaginiMPO(mp)
	return exif, fotogrammi, err
}

// immaginiMPO è jpeg_factory/_getmp di Pillow 9.4: un indice MP leggibile, con il numero delle
// immagini (0xB001) oltre 1 e le voci (0xB002) tutte con ImageDataFormat JPEG (i bit 24-26 di
// Attribute a 0; il tipo MP, bit 0-23, non conta), fa un MPO con più fotogrammi; voci troppo
// corte fanno fallire l'apertura (struct.error); ogni altra forma (indice illeggibile, senza
// numero, senza voci, una voce con un altro formato) resta un JPEG normale.
func immaginiMPO(mp []byte) (int, error) {
	if mp == nil {
		return 1, nil
	}
	d, err := leggiIFD(mp)
	if err != nil {
		return 1, nil
	}
	quante, ok := d.intero(0xB001)
	if !ok || quante <= 1 {
		return 1, nil
	}
	voci, ok := d.ifd[0xB002]
	if !ok {
		return 1, nil
	}
	for i := 0; i < quante; i++ {
		if len(voci.dati) < (i+1)*16 {
			return 0, errFormato
		}
		if d.ordine.Uint32(voci.dati[i*16:])>>24&7 != 0 {
			return 1, nil
		}
	}
	return quante, nil
}

// --- PNG ---

// maxTesto è MAX_TEXT_CHUNK di Pillow: un testo compresso che si apre oltre fa fallire l'apertura.
const maxTesto = 1 << 20

// maxTestoTotale è MAX_TEXT_MEMORY (64 × MAX_TEXT_CHUNK): il testo di tutti i chunk tenuti, contato
// in caratteri (check_text_memory), oltre fa fallire l'apertura.
const maxTestoTotale = 64 * maxTesto

// testaPNG legge i chunk: acTL prima dei dati (n_frames di un PNG animato), eXIf e il tEXt
// «exif» (info["exif"], vince l'ultimo), «Raw profile type exif» e l'XMP «XML:com.adobe.xmp» nei
// chunk di testo (tEXt, zTXt, iTXt), letti anche dopo i dati perché la normalizzazione carica
// l'immagine prima di exif_transpose.
func testaPNG(b []byte) (exif []byte, xmp string, fotogrammi int, err error) {
	fotogrammi = 1
	var raw *string
	animati, dopoIDAT, testo := 0, false, 0
	r := b[8:]
	for len(r) >= 12 {
		n := int(binary.BigEndian.Uint32(r))
		if n < 0 || n > len(r)-12 {
			break
		}
		tipo, d := string(r[4:8]), r[8:8+n]
		r = r[12+n:]
		switch tipo {
		case "IDAT":
			dopoIDAT = true
		case "IEND":
			r = nil
		case "acTL":
			if dopoIDAT {
				continue
			}
			if n < 8 {
				return nil, "", 0, errFormato
			}
			if animati > 0 {
				animati = 0 // un secondo acTL: «Invalid APNG», immagine normale
				continue
			}
			if f := binary.BigEndian.Uint32(d); f != 0 && f <= 0x80000000 {
				animati = int(f)
			}
		case "eXIf":
			exif = append([]byte("Exif\x00\x00"), d...)
		case "tEXt", "zTXt", "iTXt":
			k, v, ok, e := testoPNG(tipo, d)
			if e != nil {
				return nil, "", 0, e
			}
			if !ok {
				continue
			}
			if testo += utf8.RuneCountInString(v); testo > maxTestoTotale {
				return nil, "", 0, errFormato
			}
			switch k {
			case "exif":
				if tipo == "tEXt" {
					exif = []byte(v)
				}
			case "Raw profile type exif":
				s := v
				raw = &s
			case "XML:com.adobe.xmp":
				xmp = v
			}
		}
	}
	if animati > 0 {
		fotogrammi = animati
	}
	if exif == nil && raw != nil {
		righe := strings.Split(*raw, "\n")
		if len(righe) > 3 {
			h := strings.Map(func(c rune) rune {
				if strings.ContainsRune(" \t\n\r\v\f", c) {
					return -1
				}
				return c
			}, strings.Join(righe[3:], ""))
			if exif, err = hex.DecodeString(h); err != nil {
				return nil, "", 0, errFormato
			}
		}
	}
	return exif, xmp, fotogrammi, nil
}

// testoPNG è chunk_tEXt, chunk_zTXt e chunk_iTXt di Pillow 9.4: chiave e valore (latin-1, utf-8
// per iTXt), false se Pillow scarta il chunk (tEXt e zTXt con la chiave vuota, iTXt con un
// testo che non è utf-8), errore se ne fa fallire l'apertura.
func testoPNG(tipo string, d []byte) (string, string, bool, error) {
	k, v, _ := bytes.Cut(d, []byte{0})
	switch tipo {
	case "tEXt":
		return latino(k), latino(v), len(k) > 0, nil
	case "zTXt":
		if len(v) == 0 {
			return latino(k), "", true, nil
		}
		if v[0] != 0 {
			return "", "", false, errFormato
		}
		s, err := apriTesto(v[1:])
		if errors.Is(err, errFormato) {
			return "", "", false, err
		}
		if err != nil {
			s = nil
		}
		return latino(k), latino(s), len(k) > 0, nil
	}
	if len(v) < 2 {
		return "", "", false, nil
	}
	compresso, metodo, resto := v[0], v[1], v[2:]
	parti := bytes.SplitN(resto, []byte{0}, 3)
	if len(parti) < 3 {
		return "", "", false, nil
	}
	s := parti[2]
	if !utf8Valido(parti[0]) || !utf8Valido(parti[1]) {
		return "", "", false, nil
	}
	if compresso != 0 {
		if metodo != 0 {
			return "", "", false, nil
		}
		var err error
		if s, err = apriTesto(s); errors.Is(err, errFormato) {
			return "", "", false, err
		} else if err != nil {
			return "", "", false, nil
		}
	}
	if !utf8Valido(s) {
		return "", "", false, nil
	}
	return latino(k), string(s), true, nil
}

// apriTesto è _safe_zlib_decompress: oltre maxTesto è errFormato.
func apriTesto(z []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(z))
	if err != nil {
		return nil, err
	}
	s, err := io.ReadAll(io.LimitReader(r, maxTesto+1))
	if len(s) > maxTesto {
		return nil, errFormato
	}
	return s, err
}

func latino(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

func utf8Valido(b []byte) bool { return strings.ToValidUTF8(string(b), "�") == string(b) }

// --- WebP ---

// testaWebP legge i chunk della forma estesa: i fotogrammi ANMF di un'animazione (frame_count
// del demuxer; una foto ferma ne ha uno) e il chunk EXIF.
func testaWebP(b []byte) (exif []byte, fotogrammi int, err error) {
	animata, fotogrammi := false, 0
	r := b[12:]
	for len(r) >= 8 {
		n := int(binary.LittleEndian.Uint32(r[4:]))
		if n < 0 || n > len(r)-8 {
			break
		}
		tipo, d := string(r[:4]), r[8:8+n]
		if salta := 8 + n + n%2; salta <= len(r) {
			r = r[salta:]
		} else {
			r = nil
		}
		switch tipo {
		case "VP8X":
			animata = len(d) > 0 && d[0]&0x02 != 0
		case "ANMF":
			fotogrammi++
		case "EXIF":
			if exif == nil {
				exif = d
			}
		}
	}
	// Un'animazione con più fotogrammi è rifiutata come in Pillow (n_frames); ogni altro uso di
	// ANMF o del segno d'animazione il demuxer di libwebp non lo apre (misurato con Pillow 9.4:
	// ANMF senza ANIM), o è un'animazione di un fotogramma che x/image/webp non decodifica.
	switch {
	case animata && fotogrammi >= 2:
		return exif, fotogrammi, nil
	case animata || fotogrammi > 0:
		return nil, 0, errFormato
	}
	return exif, 1, nil
}

// --- EXIF ---

var reXMP = regexp.MustCompile(`tiff:Orientation(="|>)([0-9])`)

// orientamentoConXMP è Image.getexif di Pillow 9.4 per exif_transpose: l'orientamento (0x0112)
// del blocco EXIF e, se il tag non c'è, quello dell'XMP del PNG. Un blocco EXIF che non è TIFF fa
// fallire getexif (SyntaxError), e con lui la normalizzazione.
func orientamentoConXMP(exif []byte, xmp string) (int, error) {
	if bytes.HasPrefix(exif, []byte("Exif\x00\x00")) {
		exif = exif[6:]
	}
	if len(exif) > 0 {
		d, err := leggiIFD(exif)
		if err != nil {
			return 0, errFormato
		}
		if _, c := d.ifd[0x0112]; c {
			o, _ := d.numero(0x0112)
			return o, nil
		}
	}
	if m := reXMP.FindStringSubmatch(xmp); m != nil {
		return int(m[2][0] - '0'), nil
	}
	return 0, nil
}

// voceIFD è un tag letto: tipo, numero di valori e byte.
type voceIFD struct {
	tipo, quanti int
	dati         []byte
}

// ifd sono i tag del primo IFD, con l'ordine dei byte del blocco.
type ifd map[uint16]voceIFD

// dimensioneTipo sono i byte di un valore per tipo TIFF (TYPES di TiffTags).
var dimensioneTipo = map[int]int{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 1, 8: 2, 9: 4, 10: 8, 11: 4, 12: 8, 13: 4, 16: 8}

// leggiIFD è ImageFileDirectory_v2 di Pillow 9.4 sul primo IFD: l'intestazione deve essere TIFF
// (MM o II, anche nelle forme sbagliate che Pillow accetta), altrimenti errore; un IFD troncato
// tiene i tag letti fino a lì; un tag di tipo sconosciuto, vuoto o con i dati fuori dal blocco si
// salta; un tag ripetuto vale l'ultima volta.
func leggiIFD(b []byte) (ifdOrdinato, error) {
	if len(b) < 8 {
		return ifdOrdinato{}, errFormato
	}
	var o binary.ByteOrder
	switch string(b[:4]) {
	case "MM\x00\x2A", "MM\x2A\x00":
		o = binary.BigEndian
	case "II\x2A\x00", "II\x00\x2A":
		o = binary.LittleEndian
	default:
		return ifdOrdinato{}, errFormato
	}
	d := ifdOrdinato{ifd: ifd{}, ordine: o}
	p := int64(o.Uint32(b[4:]))
	if p+2 > int64(len(b)) {
		return d, nil
	}
	n := int(o.Uint16(b[p:]))
	p += 2
	for i := 0; i < n && p+12 <= int64(len(b)); i, p = i+1, p+12 {
		e := b[p : p+12]
		tag, tipo, quanti := o.Uint16(e), int(o.Uint16(e[2:])), int64(o.Uint32(e[4:]))
		dim, ok := dimensioneTipo[tipo]
		if !ok {
			continue
		}
		size := quanti * int64(dim)
		var dati []byte
		if size > 4 {
			da := int64(o.Uint32(e[8:]))
			if da+size > int64(len(b)) {
				continue
			}
			dati = b[da : da+size]
		} else {
			dati = e[8 : 8+size]
		}
		if len(dati) == 0 {
			continue
		}
		d.ifd[tag] = voceIFD{tipo: tipo, quanti: int(quanti), dati: dati}
	}
	return d, nil
}

// ifdOrdinato è un IFD con il suo ordine dei byte.
type ifdOrdinato struct {
	ifd
	ordine binary.ByteOrder
}

// numero è il primo valore numerico del tag come lo vede exif_transpose (un tag con un valore
// atteso ne prende il primo): un intero da 1 a 8 per i tipi interi (SHORT, LONG, i segnati, IFD,
// LONG8), razionali e decimali; 0 per gli altri, BYTE compreso (TiffImagePlugin 9.4: load_byte dà
// bytes).
func (d ifdOrdinato) numero(tag uint16) (int, bool) {
	v, ok := d.ifd[tag]
	if !ok {
		return 0, false
	}
	o := d.ordine
	var f float64
	switch v.tipo {
	case 1:
		// BYTE: load_byte dà bytes, che exif_transpose non riconosce.
		return 0, true
	case 6:
		f = float64(int8(v.dati[0]))
	case 3:
		f = float64(o.Uint16(v.dati))
	case 8:
		f = float64(int16(o.Uint16(v.dati)))
	case 4, 13:
		f = float64(o.Uint32(v.dati))
	case 9:
		f = float64(int32(o.Uint32(v.dati)))
	case 16:
		f = float64(o.Uint64(v.dati))
	case 5, 10:
		n, q := float64(o.Uint32(v.dati)), float64(o.Uint32(v.dati[4:]))
		if v.tipo == 10 {
			n, q = float64(int32(o.Uint32(v.dati))), float64(int32(o.Uint32(v.dati[4:])))
		}
		if q == 0 {
			return 0, true
		}
		f = n / q
	case 11:
		f = float64(math.Float32frombits(o.Uint32(v.dati)))
	case 12:
		f = math.Float64frombits(o.Uint64(v.dati))
	default:
		return 0, true
	}
	if f != math.Trunc(f) || f < 1 || f > 8 {
		return 0, true
	}
	return int(f), true
}

// intero è il valore di un tag LONG o SHORT con un valore solo (come lo usa _getmp).
func (d ifdOrdinato) intero(tag uint16) (int, bool) {
	v, ok := d.ifd[tag]
	if !ok || v.quanti != 1 {
		return 0, false
	}
	switch v.tipo {
	case 3:
		return int(d.ordine.Uint16(v.dati)), true
	case 4:
		return int(d.ordine.Uint32(v.dati)), true
	}
	return 0, false
}
