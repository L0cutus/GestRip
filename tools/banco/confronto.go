package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

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
	if c == "codice" || c == "corpo" {
		return true
	}
	if f, ok := strings.CutPrefix(c, "testo:"); ok {
		return fraseValida(f)
	}
	if f, ok := strings.CutPrefix(c, "conteggio:"); ok {
		return fraseValida(f)
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
	if nome, ok := strings.CutPrefix(campo, "presenza:"); ok {
		if len(http.Header(r.Intestazioni).Values(nome)) > 0 {
			return "presente"
		}
		return "assente"
	}
	nome := strings.TrimPrefix(campo, "intestazione:")
	return strings.Join(http.Header(r.Intestazioni).Values(nome), ", ")
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
