// Comando copertura: incrocia le voci dell'inventario delle funzioni con i test Go che le
// coprono. Ogni test porta sopra di sé un segno `// inventario: ID[, ID…]` (PIANO §5).
//
//	go run ./tools/copertura [-inventario FILE] [-risultati go-test.json] [-tabella] [-verifica]
//
// L'inventario non sta nel repo (PIANO §8.3, A2): di default si legge da ~/Condivisi.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
)

// Stati di una voce nel report.
const (
	statoChiusa   = "chiusa"   // test Go tutti verdi e, se ha scenari, banco verde
	statoRossa    = "rossa"    // almeno un suo test Go è rosso
	statoParziale = "parziale" // ha test, ma manca un esito, un test è saltato o il banco non è verde
	statoAperta   = "aperta"   // voce v1 o sostituita senza test
	statoFuoriV1  = "fuori v1" // dopo-v1 o mai
)

var destini = []string{"dopo-v1", "sostituita", "mai", "v1"}

type voce struct {
	ID, Titolo, Destino string
}

type segno struct {
	File string
	Riga int
	Test string
	Voci []string
}

var (
	reTitolo = regexp.MustCompile(`^### ([A-Z]+(?:-[A-Z])?-\d+) — (.+)$`)
	reID     = regexp.MustCompile(`^[A-Z]+(?:-[A-Z])?-\d+$`)
	reSegno  = regexp.MustCompile(`^// inventario:\s*(.+)$`)
	// reQuasi trova i segni scritti male (`//inventario:`, `Inventario:`, commenti a blocco).
	reQuasi = regexp.MustCompile(`(?i)inventario\s*:`)
	// reTest: go test esegue TestXxx solo se dopo «Test» non c'è una minuscola.
	reTest = regexp.MustCompile(`^func (Test(?:[^a-z(]\w*)?)\(`)
)

// leggiInventario ricava ID, titolo e destino di ogni voce. Un destino che non inizia con
// v1, dopo-v1, sostituita o mai (per esempio «Da decidere») è un errore: l'inventario va
// riallineato prima.
func leggiInventario(r io.Reader) (map[string]voce, error) {
	voci := map[string]voce{}
	var cur *voce
	chiudi := func() error {
		if cur == nil {
			return nil
		}
		if cur.Destino == "" {
			return fmt.Errorf("voce %s senza «Destino Go»", cur.ID)
		}
		voci[cur.ID] = *cur
		cur = nil
		return nil
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "### ") {
			if err := chiudi(); err != nil {
				return nil, err
			}
			if m := reTitolo.FindStringSubmatch(l); m != nil {
				if _, dup := voci[m[1]]; dup {
					return nil, fmt.Errorf("voce %s duplicata", m[1])
				}
				cur = &voce{ID: m[1], Titolo: m[2]}
			}
			continue
		}
		if strings.HasPrefix(l, "## ") {
			if err := chiudi(); err != nil {
				return nil, err
			}
			continue
		}
		if cur != nil && cur.Destino == "" && strings.HasPrefix(l, "- **Destino Go:** ") {
			d := strings.TrimLeft(strings.TrimPrefix(l, "- **Destino Go:** "), "`")
			for _, k := range destini {
				if strings.HasPrefix(strings.ToLower(d), k) {
					cur.Destino = k
					break
				}
			}
			if cur.Destino == "" {
				return nil, fmt.Errorf("voce %s: destino non riconosciuto %q", cur.ID, d)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if err := chiudi(); err != nil {
		return nil, err
	}
	return voci, nil
}

// leggiSegni trova i segni `// inventario: ID, …` nei commenti di un file di test. Un segno
// vale solo nel commento di documentazione di un func TestXxx; un segno in qualunque altro
// commento è un errore, così nessun segno resta appeso a niente.
func leggiSegni(file string, r io.Reader) ([]segno, error) {
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	usati := map[*ast.Comment]bool{}
	var out []segno
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Doc == nil {
			continue
		}
		var ids []string
		riga := 0
		for _, c := range fn.Doc.List {
			m := reSegno.FindStringSubmatch(c.Text)
			if m == nil {
				continue
			}
			usati[c] = true
			if riga == 0 {
				riga = fset.Position(c.Pos()).Line
			}
			for _, id := range strings.Split(m[1], ",") {
				id = strings.TrimSpace(id)
				if !reID.MatchString(id) {
					return nil, fmt.Errorf("%s:%d: ID malformato %q", file, fset.Position(c.Pos()).Line, id)
				}
				ids = append(ids, id)
			}
		}
		if ids == nil {
			continue
		}
		if fn.Recv != nil || !reTest.MatchString("func "+fn.Name.Name+"(") {
			return nil, fmt.Errorf("%s:%d: il segno «inventario» deve stare sopra un func TestXxx", file, riga)
		}
		out = append(out, segno{File: file, Riga: riga, Test: fn.Name.Name, Voci: ids})
	}
	for _, g := range f.Comments {
		for _, c := range g.List {
			if usati[c] {
				continue
			}
			if reSegno.MatchString(c.Text) {
				return nil, fmt.Errorf("%s:%d: segno «inventario» senza test", file, fset.Position(c.Pos()).Line)
			}
			if sembraSegno(c.Text) {
				return nil, fmt.Errorf("%s:%d: segno «inventario» scritto male: usa «// inventario: ID»", file, fset.Position(c.Pos()).Line)
			}
		}
	}
	return out, nil
}

// sembraSegno dice se un commento voleva essere un segno (`//inventario:`, `// Inventario:`,
// `/* inventario: … */`): meglio un errore che una voce che sembra coperta e non lo è.
func sembraSegno(c string) bool {
	s := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(c, "//"), "/*"))
	return reQuasi.MatchString(s) && strings.HasPrefix(strings.ToLower(s), "inventario")
}

// esito di un test di primo livello.
type esitoTest int

const (
	verde esitoTest = iota + 1
	rosso
	saltato
)

// esiti: package → test di primo livello → esito. Un rosso non si sovrascrive mai (un test
// instabile con -count=N resta rosso).
type esiti map[string]map[string]esitoTest

// leggiRisultati legge l'uscita di `go test -json`.
func leggiRisultati(r io.Reader) (esiti, error) {
	e := esiti{}
	dec := json.NewDecoder(r)
	for {
		var ev struct{ Action, Package, Test string }
		if err := dec.Decode(&ev); errors.Is(err, io.EOF) {
			return e, nil
		} else if err != nil {
			return nil, fmt.Errorf("risultati di go test: %w", err)
		}
		if ev.Test == "" || strings.Contains(ev.Test, "/") {
			continue
		}
		if ev.Action != "pass" && ev.Action != "fail" && ev.Action != "skip" {
			continue
		}
		if e[ev.Package] == nil {
			e[ev.Package] = map[string]esitoTest{}
		}
		nuovo := map[string]esitoTest{"pass": verde, "fail": rosso, "skip": saltato}[ev.Action]
		if e[ev.Package][ev.Test] != rosso {
			e[ev.Package][ev.Test] = nuovo
		}
	}
}

// esito cerca il risultato del test di un segno nel package della cartella del file
// (modulo + cartella). Un test saltato conta come non trovato: la voce resta parziale.
func (e esiti) esito(s segno, modulo string) (ok, trovato bool) {
	pkg := modulo
	if dir := path.Dir(s.File); dir != "." {
		pkg = modulo + "/" + dir
	}
	switch e[pkg][s.Test] {
	case verde:
		return true, true
	case rosso:
		return false, true
	}
	return false, false
}

type riga struct {
	ID, Titolo, Destino, Stato string
	Test                       []string
	Banco                      []string // scenari del banco a scatola chiusa
}

type report struct {
	Righe    []riga
	Conteggi map[string]int
}

func calcola(voci map[string]voce, segni []segno, e esiti, modulo string) (report, error) {
	return calcolaConBanco(voci, segni, e, modulo, nil, nil)
}

// leggiScenarioBanco legge il campo «inventario» di uno scenario del banco (bench/scenari).
func leggiScenarioBanco(file string, r io.Reader) ([]string, error) {
	var s struct {
		Inventario []string `json:"inventario"`
	}
	if err := json.NewDecoder(r).Decode(&s); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	for _, id := range s.Inventario {
		if !reID.MatchString(id) {
			return nil, fmt.Errorf("%s: ID malformato %q", file, id)
		}
	}
	return s.Inventario, nil
}

// calcolaConBanco è calcola più il banco: banco dice quali scenari coprono una voce,
// esitoBanco se ogni scenario è verde nell'ultimo giro (`banco confronta -esito`). Una voce
// con scenari è chiusa solo se anche tutti i suoi scenari sono verdi (PIANO §5).
func calcolaConBanco(voci map[string]voce, segni []segno, e esiti, modulo string, banco map[string][]string, esitoBanco map[string]bool) (report, error) {
	for id, ss := range banco {
		if _, ok := voci[id]; !ok {
			return report{}, fmt.Errorf("scenario %v: %s non è nell'inventario", ss, id)
		}
	}
	perVoce := map[string][]segno{}
	for _, s := range segni {
		for _, id := range s.Voci {
			if _, ok := voci[id]; !ok {
				return report{}, fmt.Errorf("%s:%d: %s non è nell'inventario", s.File, s.Riga, id)
			}
			perVoce[id] = append(perVoce[id], s)
		}
	}
	rep := report{Conteggi: map[string]int{}}
	for _, v := range voci {
		r := riga{ID: v.ID, Titolo: v.Titolo, Destino: v.Destino, Banco: banco[v.ID]}
		ss := perVoce[v.ID]
		for _, s := range ss {
			r.Test = append(r.Test, fmt.Sprintf("%s:%s", s.File, s.Test))
		}
		switch {
		case v.Destino == "dopo-v1" || v.Destino == "mai":
			r.Stato = statoFuoriV1
		case len(ss) == 0:
			r.Stato = statoAperta
		case e == nil:
			r.Stato = statoParziale
		default:
			r.Stato = statoChiusa
			for _, s := range ss {
				ok, trovato := e.esito(s, modulo)
				if !trovato {
					r.Stato = statoParziale
				}
				if trovato && !ok {
					r.Stato = statoRossa
					break
				}
			}
			if r.Stato == statoChiusa {
				for _, sc := range r.Banco {
					if !esitoBanco[sc] {
						r.Stato = statoParziale
					}
				}
			}
		}
		rep.Conteggi[r.Stato]++
		rep.Righe = append(rep.Righe, r)
	}
	sort.Slice(rep.Righe, func(i, j int) bool { return rep.Righe[i].ID < rep.Righe[j].ID })
	return rep, nil
}
