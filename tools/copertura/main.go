package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func main() {
	home, _ := os.UserHomeDir()
	def := os.Getenv("GESTRIP_INVENTARIO")
	if def == "" {
		def = filepath.Join(home, "Condivisi", "GestRip-Go", "inventario-funzioni.md")
	}
	inv := flag.String("inventario", def, "percorso di inventario-funzioni.md (privato, non sta nel repo); se manca si legge -elenco")
	elenco := flag.String("elenco", filepath.Join("docs", "inventario-elenco.tsv"), "elenco pubblico ID, titolo e destino (nel repo)")
	scrivi := flag.Bool("scrivi-elenco", false, "rigenera -elenco dall'inventario privato, con il controllo delle parole vietate")
	defParole := os.Getenv("GESTRIP_PAROLE_VIETATE")
	if defParole == "" {
		defParole = filepath.Join(home, ".config", "gestrip-go", "parole-vietate.txt")
	}
	parole := flag.String("parole-vietate", defParole, "elenco locale delle parole vietate (fuori dal repo)")
	ris := flag.String("risultati", "", "uscita di `go test -json ./...` (obbligatoria con -verifica e -rilascio)")
	bancoEsito := flag.String("banco", "", "esito del banco (`banco confronta -esito`); senza, le voci con scenari restano parziali")
	tab := flag.Bool("tabella", false, "stampa una riga per voce")
	verifica := flag.Bool("verifica", false, "esce con errore se una voce v1 o sostituita ha test rossi")
	rilascio := flag.Bool("rilascio", false, "esce con errore se una voce v1 o sostituita non è chiusa")
	flag.Parse()
	invEsplicito := false
	flag.Visit(func(f *flag.Flag) { invEsplicito = invEsplicito || f.Name == "inventario" })
	voci, err := caricaVoci(*inv, invEsplicito || *scrivi, *elenco, *scrivi, *parole)
	if err == nil && !*scrivi {
		err = esegui(voci, *ris, *bancoEsito, *tab, *verifica, *rilascio)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "copertura:", err)
		os.Exit(1)
	}
}

// caricaVoci legge le voci dall'inventario privato se c'è, e in quel caso controlla che l'elenco
// pubblico sia aggiornato (o lo riscrive, con scrivi). Senza inventario, e se non è stato chiesto
// esplicitamente, legge l'elenco pubblico: è il caso della CI.
func caricaVoci(inv string, invObbligatorio bool, elenco string, scrivi bool, parole string) (map[string]voce, error) {
	f, err := leggiFile(inv)
	if errors.Is(err, fs.ErrNotExist) && !invObbligatorio {
		g, err := leggiFile(elenco)
		if err != nil {
			return nil, err
		}
		fmt.Printf("inventario privato non disponibile: leggo %s\n", elenco)
		return leggiElenco(g)
	}
	if err != nil {
		return nil, err
	}
	voci, err := leggiInventario(f)
	if err != nil {
		return nil, err
	}
	pf, err := leggiFile(parole)
	if err != nil {
		return nil, fmt.Errorf("parole vietate: %w", err)
	}
	vietate, err := leggiParoleVietate(pf)
	if err != nil {
		return nil, err
	}
	var nuovo bytes.Buffer
	if err := scriviElenco(&nuovo, voci, vietate); err != nil {
		return nil, err
	}
	vecchio, err := os.ReadFile(elenco)
	if err != nil && !(scrivi && errors.Is(err, fs.ErrNotExist)) {
		return nil, err
	}
	if bytes.Equal(vecchio, nuovo.Bytes()) {
		return voci, nil
	}
	if !scrivi {
		return nil, fmt.Errorf("%s non è allineato all'inventario: rigeneralo con `go run ./tools/copertura -scrivi-elenco`", elenco)
	}
	if err := os.WriteFile(elenco, nuovo.Bytes(), 0o644); err != nil {
		return nil, err
	}
	fmt.Printf("scritto %s (%d voci)\n", elenco, len(voci))
	return voci, nil
}

func esegui(voci map[string]voce, ris, bancoEsito string, tab, verifica, rilascio bool) error {
	if (verifica || rilascio) && ris == "" {
		return fmt.Errorf("-verifica e -rilascio vogliono -risultati (uscita di go test -json)")
	}
	modulo, err := moduloDa("go.mod")
	if err != nil {
		return fmt.Errorf("va lanciato dalla radice del repo: %w", err)
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
		g, err := leggiFile(p)
		if err != nil {
			return err
		}
		s, err := leggiSegni(filepath.ToSlash(p), g)
		segni = append(segni, s...)
		return err
	})
	if err != nil {
		return err
	}
	banco, provaCon, err := scenariDelBanco(".")
	if err != nil {
		return err
	}
	var e esiti
	if ris != "" {
		r, err := leggiFile(ris)
		if err != nil {
			return err
		}
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
		if esitoBanco, err = esitoDelBanco(b, provaCon); err != nil {
			return err
		}
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

// scenariDelBanco dice quali scenari del banco coprono ogni voce: quelli HTTP (bench/scenari) e
// quelli del livello browser (bench/browser, slice 040a), per nome del file, che è anche il nome
// nell'esito del banco. Un nome usato in tutte e due le cartelle è un errore. Dà anche, per ogni
// scenario, i dati con cui prova davvero qualcosa (prova_con; vuoto: tutti).
func scenariDelBanco(radice string) (map[string][]string, map[string][]string, error) {
	banco, provaCon := map[string][]string{}, map[string][]string{}
	visti := map[string]string{}
	for _, cartella := range []string{"scenari", "browser"} {
		scenari, err := filepath.Glob(filepath.Join(radice, "bench", cartella, "*.json"))
		if err != nil {
			return nil, nil, err
		}
		for _, s := range scenari {
			nome := strings.TrimSuffix(filepath.Base(s), ".json")
			if c, ok := visti[nome]; ok {
				return nil, nil, fmt.Errorf("scenario %q sia in bench/%s sia in bench/%s", nome, c, cartella)
			}
			visti[nome] = cartella
			g, err := leggiFile(s)
			if err != nil {
				return nil, nil, err
			}
			ids, pc, err := leggiScenarioBanco(s, g)
			if err != nil {
				return nil, nil, err
			}
			provaCon[nome] = pc
			for _, id := range ids {
				banco[id] = append(banco[id], nome)
			}
		}
	}
	return banco, provaCon, nil
}

// esitoDelBanco legge l'esito del banco (`banco esegui`): per ogni scenario verde o no. Uno scenario
// è verde solo se l'esito lo dice e se i dati del giro sono fra quelli con cui prova qualcosa
// (prova_con): un giro su database vuoto non chiude le voci che si provano solo con i dati
// importati. Gli scenari non provati non sono verdi; un esito senza dati non si accetta.
func esitoDelBanco(b []byte, provaCon map[string][]string) (map[string]bool, error) {
	var x struct {
		Dati       string          `json:"dati"`
		Scenari    map[string]bool `json:"scenari"`
		NonProvati []string        `json:"non_provati"`
	}
	if err := json.Unmarshal(b, &x); err != nil {
		return nil, fmt.Errorf("esito del banco: %w", err)
	}
	if x.Dati != "account-seminati" && x.Dati != "sintetici-import" {
		return nil, fmt.Errorf("esito del banco con dati %q: rifare il giro con un banco che li scrive", x.Dati)
	}
	out := map[string]bool{}
	for nome, v := range x.Scenari {
		pc := provaCon[nome]
		out[nome] = v && (len(pc) == 0 || slices.Contains(pc, x.Dati))
	}
	for _, nome := range x.NonProvati {
		out[nome] = false
	}
	return out, nil
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

// leggiFile legge tutto il file: niente da chiudere, quindi nessun errore di chiusura ignorato.
func leggiFile(p string) (*bytes.Reader, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(b), nil
}
