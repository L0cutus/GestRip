package lettura

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// Gli oggetti di un PDF (ISO 32000-1, §7.3) e la loro lettura dal file: solo ciò che scrivono i PDF
// che il lettore deve capire, cioè quelli del motore (gopdf) e quelli di Chromium (Skia/PDF). Tutti e
// due hanno la tabella xref classica; uno stream xref (PDF 1.5) o un filtro diverso da FlateDecode è
// un errore: il lettore rifiuta ciò che non sa leggere, non indovina.

// Limiti contro un PDF ostile (review della 042): i nostri PDF e quelli di Chromium stanno molto
// sotto.
const (
	profonditaMassima = 64        // array e dizionari annidati
	profonditaPagine  = 32        // livelli dell'albero delle pagine
	profonditaForme   = 8         // form XObject annidati
	maxPagine         = 1000      // pagine di un documento
	maxStream         = 64 << 20  // byte di uno stream decompresso
	maxDecompressi    = 128 << 20 // byte decompressi in tutto il documento
	maxVociCMap       = 100_000   // voci di tutte le CMap ToUnicode
)

type nome string

type dizionario map[nome]any

type riferimento struct{ n, g int64 }

type flusso struct {
	diz  dizionario
	dati []byte // come sta nel file, prima dei filtri
}

type operatore string // una parola chiave (obj, R, true, …) o un operatore del contenuto

// ErrNonPDF è l'errore di ogni lettura non riuscita: il resto dice il perché.
var ErrNonPDF = errors.New("lettura: PDF non valido")

func guasto(formato string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrNonPDF, fmt.Sprintf(formato, a...))
}

// lessico scorre i byte di un file o di uno stream di contenuto.
type lessico struct {
	b          []byte
	pos        int
	profondita int // array e dizionari aperti adesso
}

func spazio(c byte) bool {
	return c == 0 || c == '\t' || c == '\n' || c == '\f' || c == '\r' || c == ' '
}

func delimitatore(c byte) bool {
	return c == '(' || c == ')' || c == '<' || c == '>' || c == '[' || c == ']' || c == '{' || c == '}' || c == '/' || c == '%'
}

func (l *lessico) salta() {
	for l.pos < len(l.b) {
		c := l.b[l.pos]
		switch {
		case spazio(c):
			l.pos++
		case c == '%':
			for l.pos < len(l.b) && l.b[l.pos] != '\n' && l.b[l.pos] != '\r' {
				l.pos++
			}
		default:
			return
		}
	}
}

// segno di fine degli elementi composti.
type chiusura byte

// valore legge il prossimo oggetto. I riferimenti «n g R» si riconoscono dentro array e dizionari
// e al livello più alto (riconosci); un operatore torna come operatore.
func (l *lessico) valore() (any, error) {
	l.salta()
	if l.pos >= len(l.b) {
		return nil, io.EOF
	}
	c := l.b[l.pos]
	switch {
	case c == '/':
		l.pos++
		inizio := l.pos
		for l.pos < len(l.b) && !spazio(l.b[l.pos]) && !delimitatore(l.b[l.pos]) {
			l.pos++
		}
		return nome(decodificaNome(l.b[inizio:l.pos])), nil
	case c == '(':
		return l.letterale()
	case c == '<' && l.pos+1 < len(l.b) && l.b[l.pos+1] == '<':
		l.pos += 2
		if err := l.apri(); err != nil {
			return nil, err
		}
		defer l.chiudi()
		return l.dizionario()
	case c == '<':
		return l.esadecimale()
	case c == '>' && l.pos+1 < len(l.b) && l.b[l.pos+1] == '>':
		l.pos += 2
		return chiusura('>'), nil
	case c == '[':
		l.pos++
		if err := l.apri(); err != nil {
			return nil, err
		}
		defer l.chiudi()
		return l.array()
	case c == ']':
		l.pos++
		return chiusura(']'), nil
	case c == '{' || c == '}':
		l.pos++
		return operatore(string(c)), nil
	case c == ')' || c == '>':
		return nil, guasto("%q fuori posto a %d", c, l.pos)
	}
	inizio := l.pos
	for l.pos < len(l.b) && !spazio(l.b[l.pos]) && !delimitatore(l.b[l.pos]) {
		l.pos++
	}
	parola := string(l.b[inizio:l.pos])
	if n, ok := numero(parola); ok {
		return n, nil
	}
	return operatore(parola), nil
}

// apri conta un array o un dizionario aperto: oltre profonditaMassima è un errore, non una
// ricorsione senza fondo.
func (l *lessico) apri() error {
	l.profondita++
	if l.profondita > profonditaMassima {
		return guasto("array o dizionari annidati oltre %d livelli", profonditaMassima)
	}
	return nil
}

func (l *lessico) chiudi() { l.profondita-- }

// dentro dà l'errore di un elemento dentro un array o un dizionario: quello vero, o la chiusura che
// manca se il file è finito.
func dentro(err error, mancante string) error {
	if errors.Is(err, io.EOF) {
		return guasto("%s senza chiusura", mancante)
	}
	return err
}

// numero legge un intero (int64) o un reale (float64).
func numero(s string) (any, bool) {
	if s == "" {
		return nil, false
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i, true
	}
	for _, c := range s {
		if (c < '0' || c > '9') && c != '.' && c != '-' && c != '+' {
			return nil, false
		}
	}
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}

func decodificaNome(b []byte) string {
	var out []byte
	for i := 0; i < len(b); i++ {
		if b[i] == '#' && i+2 < len(b) {
			if v, err := strconv.ParseUint(string(b[i+1:i+3]), 16, 8); err == nil {
				out = append(out, byte(v))
				i += 2
				continue
			}
		}
		out = append(out, b[i])
	}
	return string(out)
}

func (l *lessico) letterale() (any, error) {
	l.pos++ // (
	var out []byte
	livello := 1
	for l.pos < len(l.b) {
		c := l.b[l.pos]
		l.pos++
		switch c {
		case '(':
			livello++
		case ')':
			livello--
			if livello == 0 {
				return []byte(out), nil
			}
		case '\\':
			if l.pos >= len(l.b) {
				return nil, guasto("stringa troncata")
			}
			e := l.b[l.pos]
			l.pos++
			switch e {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case '\r':
				if l.pos < len(l.b) && l.b[l.pos] == '\n' {
					l.pos++
				}
			case '\n':
			default:
				if e >= '0' && e <= '7' {
					v := int(e - '0')
					for k := 0; k < 2 && l.pos < len(l.b) && l.b[l.pos] >= '0' && l.b[l.pos] <= '7'; k++ {
						v = v*8 + int(l.b[l.pos]-'0')
						l.pos++
					}
					out = append(out, byte(v))
				} else {
					out = append(out, e)
				}
			}
			continue
		}
		out = append(out, c)
	}
	return nil, guasto("stringa senza chiusura")
}

func (l *lessico) esadecimale() (any, error) {
	l.pos++ // <
	var cifre []byte
	for l.pos < len(l.b) {
		c := l.b[l.pos]
		l.pos++
		switch {
		case c == '>':
			if len(cifre)%2 == 1 {
				cifre = append(cifre, '0')
			}
			out := make([]byte, len(cifre)/2)
			for i := range out {
				v, err := strconv.ParseUint(string(cifre[2*i:2*i+2]), 16, 8)
				if err != nil {
					return nil, guasto("stringa esadecimale non valida")
				}
				out[i] = byte(v)
			}
			return out, nil
		case spazio(c):
		default:
			cifre = append(cifre, c)
		}
	}
	return nil, guasto("stringa esadecimale senza chiusura")
}

// riconosci sostituisce le terne «n g R» con un riferimento.
func riconosci(vv []any) []any {
	out := vv[:0]
	for i := 0; i < len(vv); i++ {
		if i+2 < len(vv) {
			n, ok1 := vv[i].(int64)
			g, ok2 := vv[i+1].(int64)
			if op, ok3 := vv[i+2].(operatore); ok1 && ok2 && ok3 && op == "R" {
				out = append(out, riferimento{n, g})
				i += 2
				continue
			}
		}
		out = append(out, vv[i])
	}
	return out
}

func (l *lessico) array() (any, error) {
	var vv []any
	for {
		v, err := l.valore()
		if err != nil {
			return nil, dentro(err, "array")
		}
		if c, ok := v.(chiusura); ok {
			if c != ']' {
				return nil, guasto("array chiuso male")
			}
			return riconosci(vv), nil
		}
		vv = append(vv, v)
	}
}

func (l *lessico) dizionario() (any, error) {
	var vv []any
	for {
		v, err := l.valore()
		if err != nil {
			return nil, dentro(err, "dizionario")
		}
		if c, ok := v.(chiusura); ok {
			if c != '>' {
				return nil, guasto("dizionario chiuso male")
			}
			break
		}
		vv = append(vv, v)
	}
	vv = riconosci(vv)
	if len(vv)%2 != 0 {
		return nil, guasto("dizionario con una chiave senza valore")
	}
	d := dizionario{}
	for i := 0; i < len(vv); i += 2 {
		k, ok := vv[i].(nome)
		if !ok {
			return nil, guasto("chiave di dizionario che non è un nome")
		}
		d[k] = vv[i+1]
	}
	return d, nil
}

// file è un PDF letto: la tabella xref (anche con gli aggiornamenti incrementali, /Prev) e gli
// oggetti già letti.
type file struct {
	b       []byte
	xref    map[int64]int64 // numero → offset, solo gli oggetti in uso
	trailer dizionario
	letti   map[int64]any
	// decompressi sono i byte usciti dai filtri in tutto il documento, vociCMap le voci di tutte le
	// CMap: hanno un tetto (maxDecompressi, maxVociCMap).
	decompressi int64
	vociCMap    int
}

func apri(b []byte) (*file, error) {
	if !bytes.HasPrefix(b, []byte("%PDF-1.")) && !bytes.HasPrefix(b, []byte("%PDF-2.")) {
		return nil, guasto("manca l'intestazione %%PDF-")
	}
	coda := b
	if len(coda) > 1024 {
		coda = coda[len(coda)-1024:]
	}
	fine := bytes.TrimRight(coda, "\r\n\t \x00")
	if !bytes.HasSuffix(fine, []byte("%%EOF")) {
		return nil, guasto("manca %%%%EOF alla fine")
	}
	i := bytes.LastIndex(coda, []byte("startxref"))
	if i < 0 {
		return nil, guasto("manca startxref")
	}
	l := &lessico{b: coda, pos: i + len("startxref")}
	v, err := l.valore()
	off, ok := v.(int64)
	if err != nil || !ok || off <= 0 || off >= int64(len(b)) {
		return nil, guasto("startxref non valido")
	}
	f := &file{b: b, xref: map[int64]int64{}, letti: map[int64]any{}}
	visti := map[int64]bool{}
	for off != 0 {
		if visti[off] {
			return nil, guasto("catena /Prev circolare")
		}
		visti[off] = true
		t, err := f.leggiXref(off)
		if err != nil {
			return nil, err
		}
		if f.trailer == nil {
			f.trailer = t
		}
		off = 0
		if p, ok := t["Prev"].(int64); ok {
			off = p
		}
	}
	for n, o := range f.xref {
		if _, err := f.oggetto(n, o); err != nil {
			return nil, err
		}
	}
	return f, nil
}

// leggiXref legge una sezione xref classica e il suo trailer; le voci già viste (di un aggiornamento
// più recente) restano.
func (f *file) leggiXref(off int64) (dizionario, error) {
	l := &lessico{b: f.b, pos: int(off)}
	if v, err := l.valore(); err != nil || v != operatore("xref") {
		return nil, guasto("a startxref non c'è una tabella xref (uno stream xref non è supportato)")
	}
	for {
		v, err := l.valore()
		if err != nil {
			return nil, guasto("xref troncata")
		}
		if v == operatore("trailer") {
			break
		}
		inizio, ok1 := v.(int64)
		w, err := l.valore()
		quanti, ok2 := w.(int64)
		if err != nil || !ok1 || !ok2 || inizio < 0 || quanti < 0 || quanti > 1<<22 {
			return nil, guasto("sottosezione xref non valida")
		}
		for k := int64(0); k < quanti; k++ {
			a, e1 := l.valore()
			_, e2 := l.valore()
			t, e3 := l.valore()
			o, ok := a.(int64)
			if e1 != nil || e2 != nil || e3 != nil || !ok {
				return nil, guasto("voce xref non valida")
			}
			n := inizio + k
			switch t {
			case operatore("n"):
				if _, c := f.xref[n]; !c {
					f.xref[n] = o
				}
			case operatore("f"):
			default:
				return nil, guasto("voce xref di tipo sconosciuto")
			}
		}
	}
	v, err := l.valore()
	t, ok := v.(dizionario)
	if err != nil || !ok {
		return nil, guasto("trailer non valido")
	}
	return t, nil
}

// oggetto legge l'oggetto n all'offset o, controllando che lì ci sia proprio «n g obj … endobj».
func (f *file) oggetto(n, o int64) (any, error) {
	if v, ok := f.letti[n]; ok {
		return v, nil
	}
	if o <= 0 || o >= int64(len(f.b)) {
		return nil, guasto("oggetto %d fuori dal file", n)
	}
	l := &lessico{b: f.b, pos: int(o)}
	a, e1 := l.valore()
	_, e2 := l.valore()
	k, e3 := l.valore()
	if e1 != nil || e2 != nil || e3 != nil || a != n || k != operatore("obj") {
		return nil, guasto("all'offset dell'oggetto %d non c'è l'oggetto", n)
	}
	v, err := l.valore()
	if err != nil {
		return nil, guasto("oggetto %d: %v", n, err)
	}
	f.letti[n] = nil // un /Length che punta all'oggetto stesso non gira in tondo
	dopo, err := l.valore()
	if err != nil {
		return nil, guasto("oggetto %d senza endobj", n)
	}
	if dopo == operatore("stream") {
		d, ok := v.(dizionario)
		if !ok {
			return nil, guasto("stream dell'oggetto %d senza dizionario", n)
		}
		s, err := f.flusso(l, d)
		if err != nil {
			return nil, fmt.Errorf("oggetto %d: %w", n, err)
		}
		v = s
		if dopo, err = l.valore(); err != nil {
			return nil, guasto("oggetto %d senza endobj", n)
		}
	}
	if dopo != operatore("endobj") {
		return nil, guasto("oggetto %d senza endobj", n)
	}
	f.letti[n] = v
	return v, nil
}

func (f *file) flusso(l *lessico, d dizionario) (*flusso, error) {
	// Dopo «stream» un fine riga, CRLF o LF (§7.3.8.1).
	if l.pos < len(l.b) && l.b[l.pos] == '\r' {
		l.pos++
	}
	if l.pos >= len(l.b) || l.b[l.pos] != '\n' {
		return nil, guasto("dopo stream manca il fine riga")
	}
	l.pos++
	lv := d["Length"]
	if r, ok := lv.(riferimento); ok {
		v, err := f.risolvi(r)
		if err != nil {
			return nil, err
		}
		lv = v
	}
	n, ok := lv.(int64)
	if !ok || n < 0 || int64(l.pos)+n > int64(len(l.b)) {
		return nil, guasto("/Length non valido")
	}
	dati := l.b[l.pos : l.pos+int(n)]
	l.pos += int(n)
	k, err := l.valore()
	if err != nil || k != operatore("endstream") {
		return nil, guasto("lo stream non finisce dove dice /Length")
	}
	return &flusso{diz: d, dati: dati}, nil
}

// risolvi segue un riferimento; un oggetto che non c'è è null (§7.3.10).
func (f *file) risolvi(v any) (any, error) {
	for range 32 {
		r, ok := v.(riferimento)
		if !ok {
			return v, nil
		}
		o, c := f.xref[r.n]
		if !c {
			return nil, nil
		}
		var err error
		if v, err = f.oggetto(r.n, o); err != nil {
			return nil, err
		}
	}
	return nil, guasto("catena di riferimenti troppo lunga")
}

// diz risolve v e lo vuole dizionario (o lo stream con il suo dizionario).
func (f *file) diz(v any) (dizionario, error) {
	v, err := f.risolvi(v)
	if err != nil {
		return nil, err
	}
	switch d := v.(type) {
	case dizionario:
		return d, nil
	case *flusso:
		return d.diz, nil
	}
	return nil, nil
}

// decodifica dà i byte di uno stream dopo i filtri: nessuno o FlateDecode, senza predittori.
func (f *file) decodifica(s *flusso) ([]byte, error) {
	filtro, err := f.risolvi(s.diz["Filter"])
	if err != nil {
		return nil, err
	}
	var filtri []any
	switch v := filtro.(type) {
	case nil:
	case nome:
		filtri = []any{v}
	case []any:
		filtri = v
	default:
		return nil, guasto("/Filter non valido")
	}
	if _, c := s.diz["DecodeParms"]; c {
		return nil, guasto("/DecodeParms non supportato")
	}
	dati := s.dati
	for _, x := range filtri {
		if x != nome("FlateDecode") {
			return nil, guasto("filtro %v non supportato", x)
		}
		r, err := zlib.NewReader(bytes.NewReader(dati))
		if err != nil {
			return nil, guasto("FlateDecode: %v", err)
		}
		out, err := io.ReadAll(io.LimitReader(r, maxStream+1))
		if err != nil {
			return nil, guasto("FlateDecode: %v", err)
		}
		if len(out) > maxStream {
			return nil, guasto("stream oltre %d byte decompressi", maxStream)
		}
		f.decompressi += int64(len(out))
		if f.decompressi > maxDecompressi {
			return nil, guasto("oltre %d byte decompressi nel documento", maxDecompressi)
		}
		dati = out
	}
	return dati, nil
}
