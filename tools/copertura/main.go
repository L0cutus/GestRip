package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	home, _ := os.UserHomeDir()
	def := os.Getenv("GESTRIP_INVENTARIO")
	if def == "" {
		def = filepath.Join(home, "Condivisi", "GestRip-Go", "inventario-funzioni.md")
	}
	inv := flag.String("inventario", def, "percorso di inventario-funzioni.md (non sta nel repo)")
	ris := flag.String("risultati", "", "uscita di `go test -json ./...` (obbligatoria con -verifica e -rilascio)")
	bancoEsito := flag.String("banco", "", "esito del banco (`banco confronta -esito`); senza, le voci con scenari restano parziali")
	tab := flag.Bool("tabella", false, "stampa una riga per voce")
	verifica := flag.Bool("verifica", false, "esce con errore se una voce v1 o sostituita ha test rossi")
	rilascio := flag.Bool("rilascio", false, "esce con errore se una voce v1 o sostituita non è chiusa")
	flag.Parse()
	if err := esegui(*inv, *ris, *bancoEsito, *tab, *verifica, *rilascio); err != nil {
		fmt.Fprintln(os.Stderr, "copertura:", err)
		os.Exit(1)
	}
}

func esegui(inv, ris, bancoEsito string, tab, verifica, rilascio bool) error {
	if (verifica || rilascio) && ris == "" {
		return fmt.Errorf("-verifica e -rilascio vogliono -risultati (uscita di go test -json)")
	}
	modulo, err := moduloDa("go.mod")
	if err != nil {
		return fmt.Errorf("va lanciato dalla radice del repo: %w", err)
	}
	f, err := os.Open(inv)
	if err != nil {
		return err
	}
	defer f.Close()
	voci, err := leggiInventario(f)
	if err != nil {
		return err
	}
	var segni []segno
	err = filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (strings.HasPrefix(d.Name(), ".") && p != "." || d.Name() == "to-be-deleted-after-use" || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, "_test.go") {
			return nil
		}
		g, err := os.Open(p)
		if err != nil {
			return err
		}
		defer g.Close()
		s, err := leggiSegni(filepath.ToSlash(p), g)
		segni = append(segni, s...)
		return err
	})
	if err != nil {
		return err
	}
	banco := map[string][]string{}
	scenari, _ := filepath.Glob(filepath.Join("bench", "scenari", "*.json"))
	for _, s := range scenari {
		g, err := os.Open(s)
		if err != nil {
			return err
		}
		ids, err := leggiScenarioBanco(s, g)
		g.Close()
		if err != nil {
			return err
		}
		for _, id := range ids {
			banco[id] = append(banco[id], strings.TrimSuffix(filepath.Base(s), ".json"))
		}
	}
	var e esiti
	if ris != "" {
		r, err := os.Open(ris)
		if err != nil {
			return err
		}
		defer r.Close()
		if e, err = leggiRisultati(r); err != nil {
			return err
		}
	}
	var esitoBanco map[string]bool
	if bancoEsito != "" {
		b, err := os.ReadFile(bancoEsito)
		if err != nil {
			return err
		}
		var x struct {
			Scenari map[string]bool `json:"scenari"`
		}
		if err := json.Unmarshal(b, &x); err != nil {
			return fmt.Errorf("esito del banco: %w", err)
		}
		esitoBanco = x.Scenari
	}
	rep, err := calcolaConBanco(voci, segni, e, modulo, banco, esitoBanco)
	if err != nil {
		return err
	}
	if tab {
		for _, r := range rep.Righe {
			b := ""
			if len(r.Banco) > 0 {
				b = " banco:" + strings.Join(r.Banco, ",")
			}
			fmt.Printf("%-9s %-10s %-9s %s%s\n", r.ID, r.Destino, r.Stato, strings.Join(r.Test, " "), b)
		}
	}
	fmt.Printf("voci %d: chiuse %d, rosse %d, parziali %d, aperte %d, fuori v1 %d\n", len(rep.Righe),
		rep.Conteggi[statoChiusa], rep.Conteggi[statoRossa], rep.Conteggi[statoParziale],
		rep.Conteggi[statoAperta], rep.Conteggi[statoFuoriV1])
	if (verifica || rilascio) && rep.Conteggi[statoRossa] > 0 {
		return fmt.Errorf("%d voci con test rossi", rep.Conteggi[statoRossa])
	}
	if rilascio && rep.Conteggi[statoAperta]+rep.Conteggi[statoParziale] > 0 {
		return fmt.Errorf("rilascio bloccato: %d voci v1 non chiuse", rep.Conteggi[statoAperta]+rep.Conteggi[statoParziale])
	}
	return nil
}

// moduloDa legge il percorso del modulo da go.mod.
func moduloDa(gomod string) (string, error) {
	b, err := os.ReadFile(gomod)
	if err != nil {
		return "", err
	}
	for _, l := range strings.Split(string(b), "\n") {
		if m, ok := strings.CutPrefix(strings.TrimSpace(l), "module "); ok {
			return strings.TrimSpace(m), nil
		}
	}
	return "", fmt.Errorf("%s senza riga module", gomod)
}
