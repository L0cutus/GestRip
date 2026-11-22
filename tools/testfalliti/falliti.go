package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// evento è una riga di `go test -json` (test2json, più gli eventi di build dalla Go 1.24).
type evento struct {
	Action     string
	Package    string
	ImportPath string
	Test       string
	Output     string
}

// caduto è un test (o un pacchetto, Test vuoto) con Action "fail".
type caduto struct{ pacchetto, test string }

// stampaFalliti legge r e scrive su w, nell'ordine in cui sono caduti: i pacchetti che non si compilano con le righe del
// compilatore, poi ogni test fallito (sottotest compresi) e ogni pacchetto fallito con le loro righe di output, le righe
// che non sono JSON e il conto. Un file senza caduti dà solo il conto.
func stampaFalliti(r io.Reader, w io.Writer) error {
	righe := map[caduto][]string{}
	build := map[string][]string{}
	var caduti []caduto
	var nonCompilati, nonJSON []string
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for s.Scan() {
		riga := s.Text()
		if strings.TrimSpace(riga) == "" {
			continue
		}
		var e evento
		if err := json.Unmarshal([]byte(riga), &e); err != nil {
			nonJSON = append(nonJSON, riga)
			continue
		}
		switch e.Action {
		case "build-output":
			build[e.ImportPath] = append(build[e.ImportPath], e.Output)
		case "build-fail":
			nonCompilati = append(nonCompilati, e.ImportPath)
		case "output":
			k := caduto{e.Package, e.Test}
			righe[k] = append(righe[k], e.Output)
		case "fail":
			caduti = append(caduti, caduto{e.Package, e.Test})
		}
	}
	if err := s.Err(); err != nil {
		return err
	}
	var b strings.Builder
	for _, p := range nonCompilati {
		fmt.Fprintf(&b, "== non compilato: %s\n", p)
		scriviRighe(&b, build[p])
	}
	test, pacchetti := 0, 0
	for _, c := range caduti {
		if c.test == "" {
			pacchetti++
			fmt.Fprintf(&b, "== FAIL %s (pacchetto)\n", c.pacchetto)
		} else {
			test++
			fmt.Fprintf(&b, "== FAIL %s %s\n", c.pacchetto, c.test)
		}
		scriviRighe(&b, righe[c])
	}
	if len(nonJSON) > 0 {
		fmt.Fprintf(&b, "== righe non JSON: %d\n", len(nonJSON))
		for _, r := range nonJSON {
			b.WriteString(r + "\n")
		}
	}
	fmt.Fprintf(&b, "== falliti: %d test, %d pacchetti\n", test, pacchetti)
	_, err := io.WriteString(w, b.String())
	return err
}

// scriviRighe scrive le righe di output come sono, ognuna chiusa da un a capo.
func scriviRighe(b *strings.Builder, rr []string) {
	for _, r := range rr {
		b.WriteString(r)
		if !strings.HasSuffix(r, "\n") {
			b.WriteString("\n")
		}
	}
}
