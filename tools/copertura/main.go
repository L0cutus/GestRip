package main

import (
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
	ris := flag.String("risultati", "", "uscita di `go test -json ./...` (facoltativa)")
	tab := flag.Bool("tabella", false, "stampa una riga per voce")
	verifica := flag.Bool("verifica", false, "esce con errore se una voce v1 o sostituita ha test rossi")
	rilascio := flag.Bool("rilascio", false, "esce con errore se una voce v1 o sostituita non è chiusa")
	flag.Parse()
	if err := esegui(*inv, *ris, *tab, *verifica, *rilascio); err != nil {
		fmt.Fprintln(os.Stderr, "copertura:", err)
		os.Exit(1)
	}
}

func esegui(inv, ris string, tab, verifica, rilascio bool) error {
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
	rep, err := calcola(voci, segni, e)
	if err != nil {
		return err
	}
	if tab {
		for _, r := range rep.Righe {
			fmt.Printf("%-9s %-10s %-9s %s\n", r.ID, r.Destino, r.Stato, strings.Join(r.Test, " "))
		}
	}
	fmt.Printf("voci %d: chiuse %d, rosse %d, con test %d, aperte %d, fuori v1 %d\n", len(rep.Righe),
		rep.Conteggi[statoChiusa], rep.Conteggi[statoRossa], rep.Conteggi[statoConTest],
		rep.Conteggi[statoAperta], rep.Conteggi[statoFuoriV1])
	if (verifica || rilascio) && rep.Conteggi[statoRossa] > 0 {
		return fmt.Errorf("%d voci con test rossi", rep.Conteggi[statoRossa])
	}
	if rilascio && rep.Conteggi[statoAperta]+rep.Conteggi[statoConTest] > 0 {
		return fmt.Errorf("rilascio bloccato: %d voci v1 non chiuse", rep.Conteggi[statoAperta]+rep.Conteggi[statoConTest])
	}
	return nil
}
