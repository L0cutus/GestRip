package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Gli script che banco.mjs e manuale.mjs mettono nelle pagine (OSSERVATORE, SUONI, STATI_LETTORE…) sono
// stringhe template di JavaScript: dentro, una barra rovescia sola è un escape della stringa e sparisce prima
// che lo script arrivi alla pagina («\s» diventa «s», banco s079p: le righe di stato della prova scanner
// arrivavano senza le «s»). Ogni barra rovescia che lo script deve vedere va scritta doppia.
func TestScriptDellePagineSenzaEscapePersi(t *testing.T) {
	blocco := regexp.MustCompile("(?s)const ([A-Z_]+) = `(\\(\\(\\) => \\{.*?\\}\\)\\(\\))`")
	for _, f := range []string{"banco.mjs", "manuale.mjs"} {
		b, err := os.ReadFile(filepath.Join("browser", f))
		if err != nil {
			t.Fatal(err)
		}
		trovati := 0
		for _, m := range blocco.FindAllStringSubmatch(string(b), -1) {
			trovati++
			s := m[2]
			for i := 0; i < len(s); i++ {
				if s[i] != '\\' {
					continue
				}
				if i+1 < len(s) && s[i+1] == '\\' {
					i++
					continue
				}
				fine := min(len(s), i+12)
				t.Errorf("%s, %s: barra rovescia sola in %q", f, m[1], s[i:fine])
			}
		}
		if f == "banco.mjs" && trovati < 3 {
			t.Errorf("%s: %d script delle pagine trovati, almeno 3 (OSSERVATORE, SUONI, STATI_LETTORE)", f, trovati)
		}
	}
}
