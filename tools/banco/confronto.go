package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"image"
	_ "image/jpeg" // dimensioni: le foto JPEG
	_ "image/png"  // dimensioni: le foto salvate come PNG
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	_ "golang.org/x/image/webp" // dimensioni: le miniature
)

// dimensioniImmagine è «<larghezza>x<altezza>» dell'immagine nel corpo (dalla sola intestazione), o
// «(non un'immagine)». Per le foto (slice 037): le miniature del Go e di Pillow hanno byte diversi
// per scelta (tema H) e le stesse dimensioni.
func dimensioniImmagine(corpo string) string {
	c, _, err := image.DecodeConfig(strings.NewReader(corpo))
	if err != nil {
		return "(non un'immagine)"
	}
	return strconv.Itoa(c.Width) + "x" + strconv.Itoa(c.Height)
}

// scenario è una batteria di passi HTTP che gira uguale sullo standalone e sul Go. Solo dati
// inventati (PIANO §4.6).
type scenario struct {
	Nome       string   `json:"nome"`
	Inventario []string `json:"inventario"`
	Passi      []passo  `json:"passi"`
}

type passo struct {
	Metodo       string            `json:"metodo"`
	Percorso     string            `json:"percorso"` // arriva al server così com'è (//, /./, %74)
	Intestazioni map[string]string `json:"intestazioni,omitempty"`
	// Corpo è un modulo urlencoded; {csrf} è il token dell'ultima pagina di quell'istanza,
	// {email} e {password} le credenziali del titolare sintetico, {email_dipendente} e
	// {password_dipendente} quelle del dipendente (già codificati per il modulo).
	Corpo string `json:"corpo,omitempty"`
	// Atteso sono valori che **tutte e due** le istanze devono dare (campo → valore): così un
	// passo non è verde solo perché le due sbagliano allo stesso modo (per esempio un login che
	// fallisce su entrambe).
	Atteso map[string]string `json:"atteso,omitempty"`
	// AttesoDati sono le attese che dipendono dai dati da cui è partito il banco (`banco su
	// -dati`, scritto in runtime.json e passato a confronta con -dati): per ogni modalità, i
	// campi che lì valgono al posto di quelli di Atteso o in più. Atteso resta l'attesa del
	// database con i soli due account seminati; una modalità qui dice esattamente cosa implicano
	// i suoi dati (slice 029), mai «indifferente».
	AttesoDati map[string]map[string]string `json:"atteso_dati,omitempty"`
	Confronta  []string                     `json:"confronta"`
}

// attesePer dà le attese del passo con i dati della modalità dati ("" = account seminati),
// senza toccare il passo.
func attesePer(p passo, dati string) map[string]string {
	out := make(map[string]string, len(p.Atteso)+len(p.AttesoDati[dati]))
	for c, v := range p.Atteso {
		out[c] = v
	}
	if dati != "" {
		for c, v := range p.AttesoDati[dati] {
			out[c] = v
		}
	}
	return out
}

// intestazioniIgnorate sono quelle che il client Go ignora o riscrive in una richiesta.
var intestazioniIgnorate = []string{"Host", "Content-Length", "Connection", "Transfer-Encoding"}

var (
	reVoce = regexp.MustCompile(`^[A-Z]+(?:-[A-Z])?-\d+$`)
	reDev  = regexp.MustCompile(`^D-\d{2,}$`)
)

func campoValido(c string) bool {
	if c == "codice" || c == "corpo" || c == "impronta" || c == "lunghezza" || c == "dimensioni" {
		return true
	}
	if f, ok := strings.CutPrefix(c, "testo:"); ok {
		return fraseValida(f)
	}
	if f, ok := strings.CutPrefix(c, "conteggio:"); ok {
		return fraseValida(f)
	}
	if loc, ok := strings.CutPrefix(c, "visibile:"); ok {
		return reLocalizzatore.MatchString(loc)
	}
	if loc, ok := strings.CutPrefix(c, "grezzo:"); ok {
		return reLocalizzatore.MatchString(loc)
	}
	if resto, ok := strings.CutPrefix(c, "attributo:"); ok {
		loc, nome, ok := tagliaAttributo(resto)
		return ok && reLocalizzatore.MatchString(loc) && reNomeAttributo.MatchString(nome)
	}
	n, ok := strings.CutPrefix(c, "intestazione:")
	if !ok {
		n, ok = strings.CutPrefix(c, "presenza:")
	}
	return ok && n != "" && n == strings.ToLower(n)
}

// reAttributo è un attributo HTML intero (href="/settings"): le uniche virgolette ammesse in una
// frase, perché testo: e conteggio: cercano nel corpo grezzo e gli scenari dei gusci controllano
// così il markup.
var reAttributo = regexp.MustCompile(`^[a-z][a-z-]*="[^"]*"$`)

// fraseValida rifiuta le frasi vuote e quelle con un carattere che l'HTML scappa nel testo
// (' & < > e le virgolette fuori da un attributo intero): nel corpo non comparirebbero mai, e
// un «assente» o uno «0» passerebbero sempre su tutte e due le parti.
func fraseValida(f string) bool {
	if f == "" || strings.ContainsAny(f, "'&<>") {
		return false
	}
	return !strings.Contains(f, `"`) || reAttributo.MatchString(f)
}

func leggiScenario(file string, r io.Reader) (scenario, error) {
	var s scenario
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return s, fmt.Errorf("%s: %w", file, err)
	}
	if s.Nome == "" || len(s.Passi) == 0 {
		return s, fmt.Errorf("%s: servono nome e almeno un passo", file)
	}
	for _, id := range s.Inventario {
		if !reVoce.MatchString(id) {
			return s, fmt.Errorf("%s: voce d'inventario malformata %q", file, id)
		}
	}
	for i, p := range s.Passi {
		if p.Metodo == "" || !strings.HasPrefix(p.Percorso, "/") {
			return s, fmt.Errorf("%s: passo %d: metodo e percorso relativo (/…) obbligatori", file, i)
		}
		for k := range p.Intestazioni {
			if slices.Contains(intestazioniIgnorate, http.CanonicalHeaderKey(k)) {
				return s, fmt.Errorf("%s: passo %d: l'intestazione %s la decide il client Go, non lo scenario", file, i, k)
			}
		}
		if len(p.Confronta) == 0 {
			return s, fmt.Errorf("%s: passo %d: niente da confrontare", file, i)
		}
		for _, c := range p.Confronta {
			if !campoValido(c) {
				return s, fmt.Errorf("%s: passo %d: campo %q non valido", file, i, c)
			}
		}
		if err := attesaValida(p.Atteso); err != nil {
			return s, fmt.Errorf("%s: passo %d: %w", file, i, err)
		}
		for dati, att := range p.AttesoDati {
			// La modalità di base è Atteso: qui solo le modalità con dati diversi.
			if con, err := modalitaDati(dati); err != nil || !con {
				return s, fmt.Errorf("%s: passo %d: atteso_dati per la modalità %q: vale solo %s", file, i, dati, datiSinteticiImport)
			}
			if len(att) == 0 {
				return s, fmt.Errorf("%s: passo %d: atteso_dati %s senza campi", file, i, dati)
			}
			if err := attesaValida(att); err != nil {
				return s, fmt.Errorf("%s: passo %d: atteso_dati %s: %w", file, i, dati, err)
			}
		}
	}
	return s, nil
}

// attesaValida vuole campi validi e, per un conteggio, un numero intero non negativo.
func attesaValida(att map[string]string) error {
	for c, v := range att {
		if !campoValido(c) {
			return fmt.Errorf("campo atteso %q non valido", c)
		}
		if strings.HasPrefix(c, "conteggio:") {
			if n, err := strconv.Atoi(v); err != nil || n < 0 || strconv.Itoa(n) != v {
				return fmt.Errorf("conteggio atteso %q non è un numero: %q", c, v)
			}
		}
	}
	return nil
}

// risposta è ciò che il banco ha visto da un'istanza per un passo.
type risposta struct {
	Codice       int
	Corpo        string
	Intestazioni map[string][]string
}

func (r risposta) valore(campo string) string {
	switch campo {
	case "codice":
		return fmt.Sprint(r.Codice)
	case "corpo":
		return r.Corpo
	case "impronta":
		h := sha256.Sum256([]byte(r.Corpo))
		return hex.EncodeToString(h[:])
	case "lunghezza":
		return strconv.Itoa(len(r.Corpo))
	case "dimensioni":
		return dimensioniImmagine(r.Corpo)
	}
	if frase, ok := strings.CutPrefix(campo, "testo:"); ok {
		if strings.Contains(r.Corpo, frase) {
			return "presente"
		}
		return "assente"
	}
	// conteggio: quante volte la frase compare nel corpo, per le attese esatte sui dati.
	if frase, ok := strings.CutPrefix(campo, "conteggio:"); ok {
		return strconv.Itoa(strings.Count(r.Corpo, frase))
	}
	if loc, ok := strings.CutPrefix(campo, "visibile:"); ok {
		return testoVisibile(r.Corpo, loc)
	}
	if loc, ok := strings.CutPrefix(campo, "grezzo:"); ok {
		return testoGrezzo(r.Corpo, loc)
	}
	if resto, ok := strings.CutPrefix(campo, "attributo:"); ok {
		loc, nome, _ := tagliaAttributo(resto)
		return valoreAttributo(r.Corpo, loc, nome)
	}
	if nome, ok := strings.CutPrefix(campo, "presenza:"); ok {
		if len(http.Header(r.Intestazioni).Values(nome)) > 0 {
			return "presente"
		}
		return "assente"
	}
	nome := strings.TrimPrefix(campo, "intestazione:")
	return strings.Join(http.Header(r.Intestazioni).Values(nome), ", ")
}

// Il testo e gli attributi di un elemento della pagina (slice 032), per confrontare due pagine con
// markup diverso (Phoenix e html/template). Non è un parser HTML: gli elementi si trovano con
// espressioni regolari sul corpo, e la chiusura contando solo i tag con lo stesso nome.
//
// Il localizzatore è un id («repair-status») oppure «.classe@n», l'n-esimo elemento (da 1) che ha
// quella classe fra le sue; dopo, facoltativo, «/tag» o «/tag@n»: il primo o l'n-esimo tag con
// quel nome dentro l'elemento. Id e classi si leggono solo scritti con le virgolette doppie, come
// li scrivono le due app.
var (
	reLocalizzatore = regexp.MustCompile(`^(?:[A-Za-z][A-Za-z0-9_-]*|\.[A-Za-z0-9_-]+@[1-9][0-9]*)(?:/[a-z][a-z0-9]*(?:@[1-9][0-9]*)?)?$`)
	reNomeAttributo = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	reApertura      = regexp.MustCompile(`<([A-Za-z][A-Za-z0-9-]*)\b[^>]*>`)
	reClasse        = regexp.MustCompile(`\sclass="([^"]*)"`)
	reCommentoHTML  = regexp.MustCompile(`(?s)<!--.*?-->`)
	reTagHTML       = regexp.MustCompile(`</?([A-Za-z][A-Za-z0-9-]*)?[^>]*>`)
	// tagVuoti sono gli elementi senza contenuto né chiusura.
	tagVuoti = map[string]bool{"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
		"input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true}
	// tagInLinea sono gli elementi che non vanno a capo: togliendoli non si aggiunge spazio.
	tagInLinea = map[string]bool{"a": true, "abbr": true, "b": true, "bdi": true, "bdo": true, "button": true, "cite": true,
		"code": true, "data": true, "dfn": true, "em": true, "i": true, "img": true, "input": true, "kbd": true, "label": true,
		"mark": true, "q": true, "s": true, "samp": true, "small": true, "span": true, "strong": true, "sub": true, "sup": true,
		"time": true, "u": true, "var": true, "wbr": true}
)

const (
	// assenteVisibile: nessun elemento risponde al localizzatore.
	assenteVisibile = "(assente)"
	// senzaAttributo: l'elemento c'è, l'attributo no.
	senzaAttributo = "(senza attributo)"
)

// tagliaAttributo divide «localizzatore:nome» sull'ultimo «:».
func tagliaAttributo(s string) (loc, nome string, ok bool) {
	i := strings.LastIndex(s, ":")
	if i <= 0 || i == len(s)-1 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

// elemento trova l'elemento del localizzatore: il tag di apertura e il contenuto fino al tag che lo
// chiude (tutto il resto del corpo se non si chiude; vuoto per un elemento vuoto).
func elemento(corpo, loc string) (apertura, dentro string, ok bool) {
	base, sotto, _ := strings.Cut(loc, "/")
	var m []int
	if classe, n, conClasse := strings.Cut(strings.TrimPrefix(base, "."), "@"); strings.HasPrefix(base, ".") && conClasse {
		m = ennesimo(corpo, n, func(tag, apertura string) bool {
			c := reClasse.FindStringSubmatch(apertura)
			return c != nil && slices.Contains(strings.Fields(c[1]), classe)
		})
	} else {
		m = regexp.MustCompile(`<([A-Za-z][A-Za-z0-9-]*)\b[^>]*\sid="` + regexp.QuoteMeta(base) + `"[^>]*>`).FindStringSubmatchIndex(corpo)
	}
	if m == nil {
		return "", "", false
	}
	apertura, dentro = corpo[m[0]:m[1]], contenuto(corpo[m[1]:], strings.ToLower(corpo[m[2]:m[3]]))
	if sotto == "" {
		return apertura, dentro, true
	}
	nome, n, conN := strings.Cut(sotto, "@")
	if !conN {
		n = "1"
	}
	m = ennesimo(dentro, n, func(tag, _ string) bool { return tag == nome })
	if m == nil {
		return "", "", false
	}
	return dentro[m[0]:m[1]], contenuto(dentro[m[1]:], nome), true
}

// ennesimo è l'n-esimo tag di apertura che piace a scegli: gli indici del tag e del nome.
func ennesimo(corpo, n string, scegli func(tag, apertura string) bool) []int {
	k, err := strconv.Atoi(n)
	if err != nil {
		return nil
	}
	for _, m := range reApertura.FindAllStringSubmatchIndex(corpo, -1) {
		if scegli(strings.ToLower(corpo[m[2]:m[3]]), corpo[m[0]:m[1]]) {
			if k--; k == 0 {
				return m
			}
		}
	}
	return nil
}

// contenuto è resto fino al tag che chiude tag, contando quelli con lo stesso nome aperti dentro.
func contenuto(resto, tag string) string {
	if tagVuoti[tag] {
		return ""
	}
	livello := 1
	for _, t := range regexp.MustCompile(`(?i)<(/?)`+regexp.QuoteMeta(tag)+`\b[^>]*>`).FindAllStringSubmatchIndex(resto, -1) {
		switch {
		case t[3] > t[2]: // chiusura
			livello--
		case !strings.HasSuffix(resto[t[0]:t[1]], "/>"):
			livello++
		}
		if livello == 0 {
			return resto[:t[0]]
		}
	}
	return resto
}

// testoVisibile è il testo che si legge: commenti tolti, tag tolti (quelli di blocco e <br>
// valgono uno spazio, quelli in linea niente), entità decodificate, spazi compattati. Non sa
// niente di CSS: un elemento in linea reso a blocco dal foglio di stile conta come in linea.
func testoVisibile(corpo, loc string) string {
	_, dentro, ok := elemento(corpo, loc)
	if !ok {
		return assenteVisibile
	}
	dentro = reCommentoHTML.ReplaceAllString(dentro, "")
	dentro = reTagHTML.ReplaceAllStringFunc(dentro, func(t string) string {
		if m := reTagHTML.FindStringSubmatch(t); m != nil && tagInLinea[strings.ToLower(m[1])] {
			return ""
		}
		return " "
	})
	return strings.Join(strings.Fields(html.UnescapeString(dentro)), " ")
}

// testoGrezzo è il testo con a capo e spazi intatti: commenti e tag tolti senza spazio, entità
// decodificate. Per i riquadri pre-wrap, dove una riga vuota in più si vede.
func testoGrezzo(corpo, loc string) string {
	_, dentro, ok := elemento(corpo, loc)
	if !ok {
		return assenteVisibile
	}
	return html.UnescapeString(reTagHTML.ReplaceAllString(reCommentoHTML.ReplaceAllString(dentro, ""), ""))
}

// valoreAttributo è il valore dell'attributo nome nel tag di apertura, con le entità decodificate;
// «» per un attributo senza valore. Solo valori fra virgolette doppie.
func valoreAttributo(corpo, loc, nome string) string {
	apertura, _, ok := elemento(corpo, loc)
	if !ok {
		return assenteVisibile
	}
	m := regexp.MustCompile(`\s` + regexp.QuoteMeta(nome) + `(?:="([^"]*)"|[\s/>])`).FindStringSubmatch(apertura)
	if m == nil {
		return senzaAttributo
	}
	return html.UnescapeString(m[1])
}

type differenza struct {
	Scenario       string
	Passo          int
	Campo          string
	Standalone, Go string
}

func (d differenza) String() string {
	return fmt.Sprintf("%s[%d] %s: standalone %q, go %q", d.Scenario, d.Passo, d.Campo, d.Standalone, d.Go)
}

// confronta restituisce i campi del passo che differiscono fra standalone (a) e Go (b), e quelli
// in cui una delle due non dà l'atteso del database con i soli account seminati.
func confronta(p passo, a, b risposta) []differenza {
	return confrontaCon(p, "", a, b)
}

// confrontaCon è confronta con le attese dei dati della modalità dati (attesePer).
func confrontaCon(p passo, dati string, a, b risposta) []differenza {
	var out []differenza
	for c, v := range attesePer(p, dati) {
		if va, vb := a.valore(c), b.valore(c); va != v || vb != v {
			out = append(out, differenza{Campo: "atteso " + c + "=" + v, Standalone: va, Go: vb})
		}
	}
	for _, c := range p.Confronta {
		if va, vb := a.valore(c), b.valore(c); va != vb {
			out = append(out, differenza{Campo: c, Standalone: va, Go: vb})
		}
	}
	return out
}

// deviazione è una differenza attesa, legata a una riga D-xx approvata dell'inventario.
type deviazione struct {
	ID         string `json:"id"`
	Scenario   string `json:"scenario"`
	Passo      int    `json:"passo"`
	Campo      string `json:"campo"`
	Standalone string `json:"standalone"`
	Go         string `json:"go"`
	Motivo     string `json:"motivo"`
}

type deviazioni []deviazione

func leggiDeviazioni(r io.Reader) (deviazioni, error) {
	var ds deviazioni
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ds); err != nil {
		return nil, fmt.Errorf("deviazioni: %w", err)
	}
	for _, d := range ds {
		if !reDev.MatchString(d.ID) || d.Scenario == "" || !campoValido(d.Campo) || d.Motivo == "" {
			return nil, fmt.Errorf("deviazione %+v: servono id D-xx, scenario, campo valido e motivo", d)
		}
	}
	return ds, nil
}

// copre dice se la differenza è esattamente una deviazione dichiarata (stesso scenario, passo,
// campo e gli stessi due valori).
func (ds deviazioni) copre(x differenza) (string, bool) {
	for _, d := range ds {
		if d.Scenario == x.Scenario && d.Passo == x.Passo && d.Campo == x.Campo &&
			d.Standalone == x.Standalone && d.Go == x.Go {
			return d.ID, true
		}
	}
	return "", false
}

// deviazioniApprovate legge gli ID della tabella «Deviazioni volute» dell'inventario.
func deviazioniApprovate(r io.Reader) (map[string]bool, error) {
	ok := map[string]bool{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		campi := strings.Split(sc.Text(), "|")
		if len(campi) > 2 {
			if id := strings.TrimSpace(campi[1]); reDev.MatchString(id) {
				ok[id] = true
			}
		}
	}
	if len(ok) == 0 {
		return nil, errors.New("nessuna deviazione D-xx trovata nell'inventario")
	}
	return ok, sc.Err()
}
