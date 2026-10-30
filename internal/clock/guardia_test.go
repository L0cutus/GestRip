package clock

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// vietatiFuori sono le funzioni del pacchetto time che leggono l'orologio o il fuso del sistema:
// fuori da internal/clock il codice del prodotto passa da qui (PIANO §1.7). Unix, UnixMilli e
// UnixMicro danno istanti in time.Local; After, Tick, NewTicker e NewTimer consegnano l'ora del
// sistema sul canale (lo schema tipico di uno scheduler dei job).
var vietatiFuori = map[string]bool{
	"Now": true, "Since": true, "Until": true, "Local": true, "LoadLocation": true,
	"Unix": true, "UnixMilli": true, "UnixMicro": true,
	"After": true, "Tick": true, "NewTicker": true, "NewTimer": true,
}

// usiVietati trova in un file gli usi di time.Now e simili, anche con l'import rinominato, e le
// chiamate al metodo Local() di un istante.
func usiVietati(nome string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, nome, src, 0)
	if err != nil {
		return nil, err
	}
	alias := map[string]bool{}
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if p != "time" {
			continue
		}
		switch {
		case imp.Name == nil:
			alias["time"] = true
		default:
			alias[imp.Name.Name] = true
		}
	}
	var trovati []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && alias[id.Name] && vietatiFuori[x.Sel.Name] {
				trovati = append(trovati, fset.Position(x.Pos()).String()+" time."+x.Sel.Name)
			} else if x.Sel.Name == "Local" {
				trovati = append(trovati, fset.Position(x.Pos()).String()+" .Local()")
			}
		case *ast.Ident:
			if alias["."] && vietatiFuori[x.Name] {
				trovati = append(trovati, fset.Position(x.Pos()).String()+" "+x.Name)
			}
		}
		return true
	})
	return trovati, nil
}

// inventario: JOB-01
// Nessun pacchetto del prodotto fuori da internal/clock legge l'orologio o il fuso del sistema.
func TestNessunOrologioFuoriDaClock(t *testing.T) {
	radice := filepath.Join("..", "..")
	var trovati []string
	file := 0
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(radice, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" || p == filepath.Join(radice, "internal", "clock") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			file++
			u, err := usiVietati(p, src)
			trovati = append(trovati, u...)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if file == 0 {
		t.Fatal("nessun file controllato: percorso sbagliato?")
	}
	for _, u := range trovati {
		t.Errorf("orologio di sistema fuori da internal/clock: %s", u)
	}
}

// inventario: JOB-01
func TestUsiVietati(t *testing.T) {
	casi := map[string]int{
		"package x\nimport \"time\"\nvar a = time.Now()\n":                                        1,
		"package x\nimport t \"time\"\nvar a = t.Since(t.Time{})\n":                               1,
		"package x\nimport . \"time\"\nvar a = Now()\n":                                           1,
		"package x\nimport \"time\"\nvar a = time.Time{}.Local()\n":                               1,
		"package x\nimport \"time\"\nvar a = time.Local\nvar b, _ = time.LoadLocation(\"UTC\")\n": 2,
		"package x\nimport \"time\"\nvar a = time.Duration(1) * time.Second\n":                    0,
		"package x\nimport \"time\"\nvar a = time.Unix(0, 0).Format(\"15:04\")\n":                 1,
		"package x\nimport \"time\"\nvar a = time.UnixMilli(0)\nvar b = time.UnixMicro(0)\n":      2,
		"package x\nimport \"time\"\nvar a = <-time.After(0)\nvar b = time.Tick(1)\n":             2,
		"package x\nimport \"time\"\nvar a = time.NewTicker(1)\nvar b = time.NewTimer(1)\n":       2,
		"package x\nimport \"time\"\nvar a = time.Time{}.Unix()\n":                                0,
		"package x\ntype o struct{}\nfunc (o) Now() int { return 0 }\nvar a = o{}.Now()\n":        0,
	}
	for src, n := range casi {
		u, err := usiVietati("x.go", []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		if len(u) != n {
			t.Errorf("%q: trovati %v, attesi %d", src, u, n)
		}
	}
}
