// Package lettura legge i PDF che il Go confronta: formato delle pagine, testo con la sua posizione
// e rettangoli pieni (D-24: «uguale» vuol dire stesso formato della pagina, stesso testo estratto
// normalizzato, stesso contenuto dei QR, posizioni chiave entro ±0,5 mm dove la misura è una regola).
//
// Non è un lettore di PDF qualsiasi: capisce i PDF del motore (internal/pdf, sopra gopdf) e quelli di
// Chromium (Skia/PDF) dello standalone, cioè tabella xref classica, stream FlateDecode, font Type0
// Identity-H con ToUnicode e font semplici WinAnsi. Il resto è un errore, mai un testo indovinato. Lo
// usano i test, il controllo del motore prima di consegnare un PDF (pdf.Controlla) e il banco.
//
// Le misure sono in millimetri, con l'origine in alto a sinistra della pagina, come nel motore.
package lettura

import (
	"bytes"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
)

// Riga è un pezzo di testo scritto di seguito sulla stessa linea di base: un blocco BT…ET, o una
// sua parte se dentro il blocco il testo va a capo. X e Y sono il punto d'inizio della linea di base
// in mm dall'angolo in alto a sinistra; Corpo è la grandezza del carattere in pt.
type Riga struct {
	Testo       string
	X, Y, Corpo float64
}

// Rettangolo è un rettangolo pieno (re seguito da f): angolo in alto a sinistra, larghezza e altezza
// in mm.
type Rettangolo struct{ X, Y, L, A float64 }

// Pagina è una pagina letta: formato (MediaBox) in mm, testo e rettangoli nell'ordine del contenuto.
type Pagina struct {
	Larghezza, Altezza float64
	Righe              []Riga
	Rettangoli         []Rettangolo
	Contorni           []Rettangolo // rettangoli disegnati solo con il tratto (re seguito da S)
	Immagini           []Rettangolo // dove sono disegnate le immagini (XObject /Image con Do)
}

// Documento è il PDF letto.
type Documento struct{ Pagine []Pagina }

const ptInMM = 25.4 / 72

// Leggi legge b. Un errore vuol dire che b non è un PDF valido, o non è uno di quelli che il lettore
// sa leggere: in tutti e due i casi non va consegnato né confrontato.
func Leggi(b []byte) (*Documento, error) {
	f, err := apri(b)
	if err != nil {
		return nil, err
	}
	cat, err := f.diz(f.trailer["Root"])
	if err != nil {
		return nil, err
	}
	if cat == nil || cat["Type"] != nome("Catalog") {
		return nil, guasto("manca il catalogo (/Root)")
	}
	radice, err := f.diz(cat["Pages"])
	if err != nil {
		return nil, err
	}
	if radice == nil {
		return nil, guasto("il catalogo non ha /Pages")
	}
	d := &Documento{}
	if err := f.pagine(d, radice, ereditati{}, 0, map[int64]bool{}); err != nil {
		return nil, err
	}
	if len(d.Pagine) == 0 {
		return nil, guasto("nessuna pagina")
	}
	return d, nil
}

// ereditati sono gli attributi che una pagina prende dall'albero (§7.7.3.4).
type ereditati struct {
	formato any
	risorse any
}

// pagine visita l'albero delle pagine. Un nodo già visitato (lo stesso oggetto due volte) è un
// errore, come più di maxPagine pagine: un albero così non lo scrive nessuno dei due generatori.
func (f *file) pagine(d *Documento, n dizionario, e ereditati, livello int, visti map[int64]bool) error {
	if livello > profonditaPagine {
		return guasto("albero delle pagine troppo profondo")
	}
	if v, c := n["MediaBox"]; c {
		e.formato = v
	}
	if v, c := n["Resources"]; c {
		e.risorse = v
	}
	switch n["Type"] {
	case nome("Pages"):
		kids, err := f.risolvi(n["Kids"])
		if err != nil {
			return err
		}
		figli, ok := kids.([]any)
		if !ok {
			return guasto("/Kids non valido")
		}
		for _, k := range figli {
			if r, ok := k.(riferimento); ok {
				if visti[r.n] {
					return guasto("l'oggetto %d compare due volte nell'albero delle pagine", r.n)
				}
				visti[r.n] = true
			}
			fn, err := f.diz(k)
			if err != nil {
				return err
			}
			if fn == nil {
				return guasto("pagina che non c'è")
			}
			if err := f.pagine(d, fn, e, livello+1, visti); err != nil {
				return err
			}
		}
		return nil
	case nome("Page"):
		if len(d.Pagine) >= maxPagine {
			return guasto("più di %d pagine", maxPagine)
		}
		return f.pagina(d, n, e)
	}
	return guasto("nodo dell'albero delle pagine senza /Type")
}

func (f *file) pagina(d *Documento, n dizionario, e ereditati) error {
	mb, err := f.numeri(e.formato)
	if err != nil || len(mb) != 4 || mb[2] <= mb[0] || mb[3] <= mb[1] {
		return guasto("pagina senza /MediaBox valido")
	}
	risorse, err := f.diz(e.risorse)
	if err != nil {
		return err
	}
	var contenuto []byte
	cv, err := f.risolvi(n["Contents"])
	if err != nil {
		return err
	}
	switch c := cv.(type) {
	case nil:
	case *flusso:
		if contenuto, err = f.decodifica(c); err != nil {
			return err
		}
	case []any:
		for _, x := range c {
			s, err := f.risolvi(x)
			if err != nil {
				return err
			}
			fl, ok := s.(*flusso)
			if !ok {
				return guasto("/Contents con un elemento che non è uno stream")
			}
			b, err := f.decodifica(fl)
			if err != nil {
				return err
			}
			contenuto = append(append(contenuto, b...), '\n')
		}
	default:
		return guasto("/Contents non valido")
	}
	p := &pittore{f: f, x0: mb[0], alto: mb[3], ctm: identita}
	if err := p.esegui(contenuto, risorse, 0); err != nil {
		return err
	}
	p.chiudiRiga()
	d.Pagine = append(d.Pagine, Pagina{
		Larghezza: (mb[2] - mb[0]) * ptInMM, Altezza: (mb[3] - mb[1]) * ptInMM,
		Righe: p.righe, Rettangoli: p.rettangoli, Contorni: p.contorni, Immagini: p.immagini,
	})
	return nil
}

func (f *file) numeri(v any) ([]float64, error) {
	v, err := f.risolvi(v)
	if err != nil {
		return nil, err
	}
	a, ok := v.([]any)
	if !ok {
		return nil, guasto("array di numeri atteso")
	}
	out := make([]float64, len(a))
	for i, x := range a {
		x, err := f.risolvi(x)
		if err != nil {
			return nil, err
		}
		n, ok := comeNumero(x)
		if !ok {
			return nil, guasto("array di numeri atteso")
		}
		out[i] = n
	}
	return out, nil
}

func comeNumero(v any) (float64, bool) {
	switch n := v.(type) {
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

// matrice è [a b c d e f] (§8.3.3).
type matrice [6]float64

var identita = matrice{1, 0, 0, 1, 0, 0}

// per dà m×n (prima m, poi n).
func (m matrice) per(n matrice) matrice {
	return matrice{
		m[0]*n[0] + m[1]*n[2], m[0]*n[1] + m[1]*n[3],
		m[2]*n[0] + m[3]*n[2], m[2]*n[1] + m[3]*n[3],
		m[4]*n[0] + m[5]*n[2] + n[4], m[4]*n[1] + m[5]*n[3] + n[5],
	}
}

func (m matrice) punto(x, y float64) (float64, float64) {
	return x*m[0] + y*m[2] + m[4], x*m[1] + y*m[3] + m[5]
}

// font è un font della pagina, per decodificare le stringhe in testo.
type font struct {
	doppio   bool           // codici di due byte (Type0 Identity-H)
	unicode  map[int][]rune // ToUnicode
	semplice bool           // font semplice senza ToUnicode: WinAnsi
}

// pittore interpreta lo stream del contenuto quanto basta: stato grafico (q, Q, cm), testo
// (BT, ET, Tf, Tm, Td, TD, T*, TL, Tj, TJ, ', ") e rettangoli pieni (re, f).
type pittore struct {
	f          *file
	x0, alto   float64
	ctm        matrice
	pila       []matrice
	tm, tlm    matrice
	interlinea float64
	corrente   *font
	corpo      float64
	font       map[string]*font
	righe      []Riga
	contorni   []Rettangolo
	riga       *Riga
	rigaY      float64 // la linea di base della riga aperta, in pt dello spazio della pagina
	percorso   []Rettangolo
	rettangoli []Rettangolo
	immagini   []Rettangolo
}

func (p *pittore) esegui(b []byte, risorse dizionario, livello int) error {
	if livello > profonditaForme {
		return guasto("form XObject annidati troppo")
	}
	l := &lessico{b: b}
	var operandi []any
	for {
		v, err := l.valore()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		op, ok := v.(operatore)
		if !ok {
			operandi = append(operandi, v)
			continue
		}
		if op == "BI" {
			if err := saltaImmagine(l); err != nil {
				return err
			}
			operandi = operandi[:0]
			continue
		}
		if err := p.operatore(string(op), operandi, risorse, livello); err != nil {
			return err
		}
		operandi = operandi[:0]
	}
}

// saltaImmagine salta un'immagine in linea fino a EI.
func saltaImmagine(l *lessico) error {
	i := bytes.Index(l.b[l.pos:], []byte(" ID"))
	if i < 0 {
		return guasto("immagine in linea senza ID")
	}
	l.pos += i + 3
	for l.pos+2 < len(l.b) {
		if spazio(l.b[l.pos]) && l.b[l.pos+1] == 'E' && l.b[l.pos+2] == 'I' && (l.pos+3 == len(l.b) || spazio(l.b[l.pos+3])) {
			l.pos += 3
			return nil
		}
		l.pos++
	}
	return guasto("immagine in linea senza EI")
}

func numeriDi(vv []any, n int) ([]float64, bool) {
	if len(vv) < n {
		return nil, false
	}
	out := make([]float64, n)
	for i, v := range vv[len(vv)-n:] {
		x, ok := comeNumero(v)
		if !ok {
			return nil, false
		}
		out[i] = x
	}
	return out, true
}

func (p *pittore) operatore(op string, vv []any, risorse dizionario, livello int) error {
	switch op {
	case "q":
		p.pila = append(p.pila, p.ctm)
	case "Q":
		if len(p.pila) > 0 {
			p.ctm = p.pila[len(p.pila)-1]
			p.pila = p.pila[:len(p.pila)-1]
		}
	case "cm":
		n, ok := numeriDi(vv, 6)
		if !ok {
			return guasto("cm senza sei numeri")
		}
		p.ctm = matrice(n).per(p.ctm)
	case "BT":
		p.chiudiRiga()
		p.tm, p.tlm = identita, identita
	case "ET":
		p.chiudiRiga()
	case "Tf":
		if len(vv) < 2 {
			return guasto("Tf senza font e corpo")
		}
		n, ok1 := vv[len(vv)-2].(nome)
		c, ok2 := comeNumero(vv[len(vv)-1])
		if !ok1 || !ok2 {
			return guasto("Tf non valido")
		}
		f, err := p.fontDi(string(n), risorse)
		if err != nil {
			return err
		}
		p.corrente, p.corpo = f, c
	case "TL":
		if n, ok := numeriDi(vv, 1); ok {
			p.interlinea = n[0]
		}
	case "Tm":
		n, ok := numeriDi(vv, 6)
		if !ok {
			return guasto("Tm senza sei numeri")
		}
		p.tm, p.tlm = matrice(n), matrice(n)
	case "Td", "TD":
		n, ok := numeriDi(vv, 2)
		if !ok {
			return guasto("%s senza due numeri", op)
		}
		if op == "TD" {
			p.interlinea = -n[1]
		}
		p.tlm = matrice{1, 0, 0, 1, n[0], n[1]}.per(p.tlm)
		p.tm = p.tlm
	case "T*":
		p.aCapo()
	case "Tj":
		return p.mostra(vv)
	case "'":
		p.aCapo()
		return p.mostra(vv)
	case "\"":
		p.aCapo()
		return p.mostra(vv)
	case "TJ":
		if len(vv) == 0 {
			return guasto("TJ senza array")
		}
		a, ok := vv[len(vv)-1].([]any)
		if !ok {
			return guasto("TJ senza array")
		}
		for _, x := range a {
			if s, ok := x.([]byte); ok {
				if err := p.mostra([]any{s}); err != nil {
					return err
				}
			}
		}
	case "re":
		n, ok := numeriDi(vv, 4)
		if !ok {
			return guasto("re senza quattro numeri")
		}
		x1, y1 := p.ctm.punto(n[0], n[1])
		x2, y2 := p.ctm.punto(n[0]+n[2], n[1]+n[3])
		p.percorso = append(p.percorso, Rettangolo{
			X: (math.Min(x1, x2) - p.x0) * ptInMM, Y: (p.alto - math.Max(y1, y2)) * ptInMM,
			L: math.Abs(x2-x1) * ptInMM, A: math.Abs(y2-y1) * ptInMM,
		})
	case "f", "F", "f*", "B", "B*", "b", "b*":
		p.rettangoli = append(p.rettangoli, p.percorso...)
		p.percorso = nil
	case "S", "s":
		p.contorni = append(p.contorni, p.percorso...)
		p.percorso = nil
	case "n":
		p.percorso = nil
	case "Do":
		if len(vv) == 0 {
			return guasto("Do senza nome")
		}
		n, ok := vv[len(vv)-1].(nome)
		if !ok {
			return guasto("Do senza nome")
		}
		return p.forma(string(n), risorse, livello)
	}
	return nil
}

func (p *pittore) aCapo() {
	p.tlm = matrice{1, 0, 0, 1, 0, -p.interlinea}.per(p.tlm)
	p.tm = p.tlm
	p.chiudiRiga()
}

// forma esegue un form XObject (le immagini non hanno testo).
func (p *pittore) forma(n string, risorse dizionario, livello int) error {
	xo, err := p.f.diz(risorse["XObject"])
	if err != nil || xo == nil {
		return err
	}
	v, err := p.f.risolvi(xo[nome(n)])
	if err != nil {
		return err
	}
	s, ok := v.(*flusso)
	if ok && s.diz["Subtype"] == nome("Image") {
		// Un'immagine riempie il quadrato unitario dello spazio corrente.
		x1, y1 := p.ctm.punto(0, 0)
		x2, y2 := p.ctm.punto(1, 1)
		p.immagini = append(p.immagini, Rettangolo{
			X: (math.Min(x1, x2) - p.x0) * ptInMM, Y: (p.alto - math.Max(y1, y2)) * ptInMM,
			L: math.Abs(x2-x1) * ptInMM, A: math.Abs(y2-y1) * ptInMM,
		})
		return nil
	}
	if !ok || s.diz["Subtype"] != nome("Form") {
		return nil
	}
	b, err := p.f.decodifica(s)
	if err != nil {
		return err
	}
	r, err := p.f.diz(s.diz["Resources"])
	if err != nil {
		return err
	}
	if r == nil {
		r = risorse
	}
	salvato, salvati, tm, tlm, f, c := p.ctm, p.pila, p.tm, p.tlm, p.corrente, p.corpo
	if m, err := p.f.numeri(s.diz["Matrix"]); err == nil && len(m) == 6 {
		p.ctm = matrice(m).per(p.ctm)
	}
	p.pila = nil
	vecchi := p.font
	p.font = nil
	err = p.esegui(b, r, livello+1)
	p.ctm, p.pila, p.tm, p.tlm, p.corrente, p.corpo, p.font = salvato, salvati, tm, tlm, f, c, vecchi
	return err
}

// mostra aggiunge il testo della stringa alla riga aperta, o ne apre una nuova se la linea di base è
// cambiata.
func (p *pittore) mostra(vv []any) error {
	if len(vv) == 0 {
		return guasto("testo senza stringa")
	}
	s, ok := vv[len(vv)-1].([]byte)
	if !ok {
		return guasto("testo senza stringa")
	}
	if p.corrente == nil {
		return guasto("testo senza font")
	}
	testo, err := p.corrente.testo(s)
	if err != nil {
		return err
	}
	m := p.tm.per(p.ctm)
	x, y := m.punto(0, 0)
	if p.riga != nil && math.Abs(y-p.rigaY) > 0.5 {
		p.chiudiRiga()
	}
	if p.riga == nil {
		corpo := p.corpo * math.Sqrt(math.Abs(m[0]*m[3]-m[1]*m[2]))
		p.riga = &Riga{X: (x - p.x0) * ptInMM, Y: (p.alto - y) * ptInMM, Corpo: corpo}
		p.rigaY = y
	}
	p.riga.Testo += testo
	// Lo spostamento dopo i glifi non serve: le righe si separano sulla linea di base, e Chromium
	// mette ogni glifo con il suo Td.
	return nil
}

func (p *pittore) chiudiRiga() {
	if p.riga != nil {
		p.righe = append(p.righe, *p.riga)
		p.riga = nil
	}
}

func (p *pittore) fontDi(n string, risorse dizionario) (*font, error) {
	if f, ok := p.font[n]; ok {
		return f, nil
	}
	ff, err := p.f.diz(risorse["Font"])
	if err != nil {
		return nil, err
	}
	if ff == nil {
		return nil, guasto("font %s senza risorse", n)
	}
	d, err := p.f.diz(ff[nome(n)])
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, guasto("font %s che non c'è", n)
	}
	f := &font{}
	switch d["Subtype"] {
	case nome("Type0"):
		if d["Encoding"] != nome("Identity-H") {
			return nil, guasto("font %s: codifica %v non supportata", n, d["Encoding"])
		}
		f.doppio = true
	case nome("Type1"), nome("TrueType"), nome("Type3"), nome("MMType1"):
	default:
		return nil, guasto("font %s di tipo %v non supportato", n, d["Subtype"])
	}
	tu, err := p.f.risolvi(d["ToUnicode"])
	if err != nil {
		return nil, err
	}
	switch s := tu.(type) {
	case *flusso:
		b, err := p.f.decodifica(s)
		if err != nil {
			return nil, err
		}
		if f.unicode, err = leggiCMap(b, &p.f.vociCMap); err != nil {
			return nil, err
		}
	case nil:
		if f.doppio {
			return nil, guasto("font %s senza ToUnicode", n)
		}
		f.semplice = true
	default:
		return nil, guasto("font %s: ToUnicode non valido", n)
	}
	if p.font == nil {
		p.font = map[string]*font{}
	}
	p.font[n] = f
	return f, nil
}

func (f *font) testo(s []byte) (string, error) {
	var b strings.Builder
	passo := 1
	if f.doppio {
		passo = 2
		if len(s)%2 != 0 {
			return "", guasto("stringa di un font a due byte con lunghezza dispari")
		}
	}
	for i := 0; i < len(s); i += passo {
		c := int(s[i])
		if f.doppio {
			c = c<<8 | int(s[i+1])
		}
		switch {
		case f.semplice:
			b.WriteRune(winAnsi(byte(c)))
		default:
			r, ok := f.unicode[c]
			if !ok {
				return "", guasto("glifo %04X senza testo nel ToUnicode", c)
			}
			b.WriteString(string(r))
		}
	}
	return b.String(), nil
}

// winAnsi è la codifica WinAnsiEncoding (§D.2): Latin-1, con i segni di 0x80–0x9F di Windows-1252.
func winAnsi(c byte) rune {
	alti := [32]rune{'€', 0, '‚', 'ƒ', '„', '…', '†', '‡', 'ˆ', '‰', 'Š', '‹', 'Œ', 0, 'Ž', 0,
		0, '‘', '’', '“', '”', '•', '–', '—', '˜', '™', 'š', '›', 'œ', 0, 'ž', 'Ÿ'}
	if c >= 0x80 && c < 0xa0 {
		if r := alti[c-0x80]; r != 0 {
			return r
		}
		return unicode.ReplacementChar
	}
	return rune(c)
}

// leggiCMap legge un CMap ToUnicode: bfchar e bfrange (anche con l'array di destinazioni). voci
// conta le voci di tutte le CMap del documento: oltre maxVociCMap è un errore (un bfrange di 10 byte
// può valere 65 536 voci).
func leggiCMap(b []byte, voci *int) (map[int][]rune, error) {
	l := &lessico{b: b}
	out := map[int][]rune{}
	var vv []any
	for {
		v, err := l.valore()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			return nil, err
		}
		op, ok := v.(operatore)
		if !ok {
			vv = append(vv, v)
			continue
		}
		switch op {
		case "endbfchar":
			for i := 0; i+1 < len(vv); i += 2 {
				c, ok1 := vv[i].([]byte)
				d, ok2 := vv[i+1].([]byte)
				if !ok1 || !ok2 {
					return nil, guasto("bfchar non valido")
				}
				out[codice(c)] = utf16be(d)
				if *voci++; *voci > maxVociCMap {
					return nil, guasto("CMap oltre %d voci", maxVociCMap)
				}
			}
		case "endbfrange":
			for i := 0; i+2 < len(vv); i += 3 {
				a, ok1 := vv[i].([]byte)
				z, ok2 := vv[i+1].([]byte)
				if !ok1 || !ok2 || codice(z) < codice(a) || codice(z)-codice(a) > 1<<16 {
					return nil, guasto("bfrange non valido")
				}
				*voci += codice(z) - codice(a) + 1
				if *voci > maxVociCMap {
					return nil, guasto("CMap oltre %d voci", maxVociCMap)
				}
				switch d := vv[i+2].(type) {
				case []byte:
					base := utf16be(d)
					if len(base) == 0 {
						return nil, guasto("bfrange vuoto")
					}
					for k := 0; k <= codice(z)-codice(a); k++ {
						r := append([]rune{}, base...)
						r[len(r)-1] += rune(k)
						out[codice(a)+k] = r
					}
				case []any:
					for k, x := range d {
						s, ok := x.([]byte)
						if !ok || k > codice(z)-codice(a) {
							return nil, guasto("bfrange non valido")
						}
						out[codice(a)+k] = utf16be(s)
					}
				default:
					return nil, guasto("bfrange non valido")
				}
			}
		}
		vv = vv[:0]
	}
}

func codice(b []byte) int {
	n := 0
	for _, c := range b {
		n = n<<8 | int(c)
	}
	return n
}

func utf16be(b []byte) []rune {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
	}
	return utf16.Decode(u)
}

// legature sono quelle che Chromium mette per le coppie di lettere (DejaVu ha «fi», «fl», …), con il
// testo che vogliono dire.
var legature = strings.NewReplacer("ﬀ", "ff", "ﬁ", "fi", "ﬂ", "fl", "ﬃ", "ffi", "ﬄ", "ffl", "ﬅ", "st", "ﬆ", "st")

// Normalizza è la normalizzazione del confronto del testo (D-24): legature sciolte, ogni sequenza di
// spazi (anche a capo, tabulazioni, spazio non separabile) ridotta a uno spazio, niente spazi ai
// bordi. A capo e impaginazione non contano: Chromium e il motore vanno a capo in punti diversi.
func Normalizza(s string) string {
	return strings.Join(strings.FieldsFunc(legature.Replace(s), unicode.IsSpace), " ")
}

// TestoNormalizzato è il testo di tutte le pagine, riga dopo riga, normalizzato.
func TestoNormalizzato(d *Documento) string {
	var parti []string
	for _, p := range d.Pagine {
		for _, r := range p.Righe {
			parti = append(parti, r.Testo)
		}
	}
	return Normalizza(strings.Join(parti, "\n"))
}

// Formato è il formato delle pagine in millimetri interi («80x297»), una voce per pagina separata da
// virgole. Chromium arrotonda il formato della carta (80,09×297,01 mm per 80×297): il millimetro
// intero è il confronto di D-24 per il formato.
func Formato(d *Documento) string {
	parti := make([]string, len(d.Pagine))
	for i, p := range d.Pagine {
		parti[i] = strconv.Itoa(int(math.Round(p.Larghezza))) + "x" + strconv.Itoa(int(math.Round(p.Altezza)))
	}
	return strings.Join(parti, ",")
}
