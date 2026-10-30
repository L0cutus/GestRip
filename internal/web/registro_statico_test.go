package web

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// metodiDelRegistro sono i metodi di *slog.Logger (e di log.Logger) che scrivono o preparano una
// riga di registro.
var metodiDelRegistro = []string{"Debug", "Info", "Warn", "Error", "Log", "LogAttrs", "With", "WithGroup",
	"DebugContext", "InfoContext", "WarnContext", "ErrorContext", "Print", "Printf", "Println",
	"Fatal", "Fatalf", "Fatalln", "Panic", "Panicf", "Panicln"}

// campiConIlPercorso sono i campi di una richiesta che portano percorso, query o provenienza.
var campiConIlPercorso = []string{"URL", "RequestURI", "RawQuery", "RawPath"}

// chiamateConIlPercorso sono i metodi e le funzioni che restituiscono un pezzo dell'indirizzo.
var chiamateConIlPercorso = []string{"PathValue", "Referer", "EscapedPath", "percorsoArrivato", "percorsoPhoenix"}

// richieste sono i nomi che il codice dà a una *http.Request: passata intera a un registro, il
// suo formato contiene l'indirizzo.
var richieste = []string{"r", "r2", "req"}

// scriveNelRegistro dice se la chiamata scrive (o può far scrivere) qualcosa in un registro o
// sullo stderr: un metodo del registro, fmt, log, slog o panic.
func scriveNelRegistro(c *ast.CallExpr) bool {
	switch fn := c.Fun.(type) {
	case *ast.Ident:
		return fn.Name == "panic"
	case *ast.SelectorExpr:
		if x, ok := fn.X.(*ast.Ident); ok && (x.Name == "fmt" || x.Name == "log" || x.Name == "slog") {
			return true
		}
		return slices.Contains(metodiDelRegistro, fn.Sel.Name)
	}
	return false
}

// conIlPercorso dice se un argomento contiene un pezzo dell'indirizzo della richiesta.
func conIlPercorso(arg ast.Expr) (string, bool) {
	if id, ok := arg.(*ast.Ident); ok && slices.Contains(richieste, id.Name) {
		return id.Name, true
	}
	trovato := ""
	ast.Inspect(arg, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if slices.Contains(campiConIlPercorso, x.Sel.Name) {
				trovato = "." + x.Sel.Name
			}
		case *ast.CallExpr:
			switch fn := x.Fun.(type) {
			case *ast.SelectorExpr:
				if slices.Contains(chiamateConIlPercorso, fn.Sel.Name) {
					trovato = fn.Sel.Name + "(…)"
				}
			case *ast.Ident:
				if slices.Contains(chiamateConIlPercorso, fn.Name) {
					trovato = fn.Name + "(…)"
				}
			}
		}
		return trovato == ""
	})
	return trovato, trovato != ""
}

// inventario: ACC-19
// Controllo statico dei sorgenti (non i test) di internal/web e cmd/gestrip: nessuna chiamata che
// scrive in un registro (metodi di *slog.Logger e log.Logger, fmt, log, slog, panic) riceve
// l'indirizzo della richiesta (r.URL e i suoi campi, RequestURI, RawQuery, RawPath, PathValue,
// Referer, EscapedPath, la richiesta intera), nemmeno in un ramo d'errore che i test non
// percorrono.
func TestNessunPercorsoNeiRegistriDelCodice(t *testing.T) {
	var file []string
	for _, cartella := range []string{".", filepath.Join("..", "..", "cmd", "gestrip")} {
		f, err := filepath.Glob(filepath.Join(cartella, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		file = append(file, f...)
	}
	fset := token.NewFileSet()
	letti := 0
	for _, nome := range file {
		if strings.HasSuffix(nome, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, nome, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		letti++
		ast.Inspect(f, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok || !scriveNelRegistro(c) {
				return true
			}
			for _, a := range c.Args {
				if cosa, ok := conIlPercorso(a); ok {
					t.Errorf("%s: %s passa al registro un pezzo dell'indirizzo (%s)", fset.Position(c.Pos()), nomeChiamata(c), cosa)
				}
			}
			return true
		})
	}
	if letti < 10 {
		t.Errorf("letti solo %d sorgenti", letti)
	}
}

func nomeChiamata(c *ast.CallExpr) string {
	switch fn := c.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		if x, ok := fn.X.(*ast.Ident); ok {
			return x.Name + "." + fn.Sel.Name
		}
		return "…." + fn.Sel.Name
	}
	return "?"
}
