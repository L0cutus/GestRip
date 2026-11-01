// Comando banco: il banco a scatola chiusa (PIANO §4). Gli stessi scenari girano sullo
// standalone (istanza isolata e sintetica) e sul Go, e si confrontano le risposte.
//
// Sull'host (docker senza sudo: lanciare sotto `sg docker -c '…'`):
//
//	banco su       -nome N -immagine gestrip-standalone:fab-220 -gestrip BIN [-dati sintetici-import]
//	banco esegui   -nome N -banco BIN [-scenari DIR] [-deviazioni FILE]
//	banco giu      -nome N
//	banco controlla                 # avanzi con il prefisso grgo-, rc=1 se ce ne sono
//
// Con -dati sintetici-import le due istanze partono dagli stessi dati sintetici (catena.go):
// cattura Flask sintetica → legacy import dello standalone → decifratura → gestrip import.
//
// Dentro il container del banco, sulla rete interna:
//
//	banco confronta -standalone URL -go URL -ca FILE -scenari DIR -deviazioni FILE [-inventario FILE] [-dati sintetici-import]
//	banco attendi URL…
package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "uso: banco su|esegui|giu|controlla|confronta|attendi …")
		os.Exit(2)
	}
	var err error
	args := os.Args[2:]
	switch os.Args[1] {
	case "su":
		err = cmdSu(args)
	case "esegui":
		err = cmdEsegui(args)
	case "giu":
		err = cmdGiu(args)
	case "controlla":
		err = cmdControlla()
	case "confronta":
		err = cmdConfronta(args)
	case "attendi":
		err = cmdAttendi(args)
	default:
		err = fmt.Errorf("comando sconosciuto %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "banco:", err)
		os.Exit(1)
	}
}

func clientCon(ca string) (*http.Client, error) {
	pool := x509.NewCertPool()
	if ca != "" {
		pem, err := os.ReadFile(ca)
		if err != nil {
			return nil, err
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("CA non valida")
		}
	}
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
	}, nil
}

func cmdConfronta(args []string) error {
	fs := flag.NewFlagSet("confronta", flag.ExitOnError)
	su := fs.String("standalone", "", "URL di base dello standalone isolato")
	gx := fs.String("go", "", "URL di base del Go")
	ca := fs.String("ca", "", "CA dell'istanza sintetica")
	dir := fs.String("scenari", "/banco/scenari", "cartella degli scenari")
	devf := fs.String("deviazioni", "/banco/deviazioni.json", "deviazioni attese")
	inv := fs.String("inventario", "", "inventario (per controllare che ogni D-xx sia approvata)")
	esito := fs.String("esito", "", "file JSON con l'esito per scenario, per tools/copertura -banco")
	credDir := fs.String("credenziali", "", "cartella con email e password del titolare e del dipendente sintetici")
	dati := fs.String("dati", "", "i dati da cui sono partite le istanze, come in banco su: vuoto (account seminati) o "+
		datiSinteticiImport+"; sceglie le attese atteso_dati degli scenari")
	_ = fs.Parse(args) // ExitOnError: con un errore esce da sola
	if fs.NArg() > 0 {
		return fmt.Errorf("argomenti in più non riconosciuti: %v", fs.Args())
	}
	if _, err := modalitaDati(*dati); err != nil {
		return err
	}
	c, err := clientCon(*ca)
	if err != nil {
		return err
	}
	ss, err := leggiScenari(*dir)
	if err != nil {
		return err
	}
	f, err := leggiFile(*devf)
	if err != nil {
		return err
	}
	devs, err := leggiDeviazioni(f)
	if err != nil {
		return err
	}
	soloGo := deviazioniSoloGo(ss)
	if *inv == "" && (len(devs) > 0 || len(soloGo) > 0) {
		return errors.New("ci sono deviazioni: serve -inventario per controllare che siano approvate")
	}
	if *inv != "" {
		g, err := leggiFile(*inv)
		if err != nil {
			return err
		}
		ok, err := deviazioniApprovate(g)
		if err != nil {
			return err
		}
		for _, d := range devs {
			if !ok[d.ID] {
				return fmt.Errorf("deviazione %s usata dal banco ma non approvata nell'inventario", d.ID)
			}
		}
		for _, id := range soloGo {
			if !ok[id] {
				return fmt.Errorf("deviazione %s di un passo solo_go ma non approvata nell'inventario", id)
			}
		}
	}
	var cred credenziali
	if *credDir != "" {
		for nome, dest := range map[string]*string{"email": &cred.Email, "password": &cred.Password,
			"dipendente_email": &cred.EmailDipendente, "dipendente_password": &cred.PasswordDipendente} {
			b, err := os.ReadFile(filepath.Join(*credDir, nome))
			if err != nil {
				return err
			}
			*dest = strings.TrimSpace(string(b))
		}
	}
	fmt.Println("dati:", datiDi(*dati != ""))
	rap := eseguiScenariCon(ss, devs, istanza{"standalone", *su, c}, istanza{"go", *gx, c}, cred, *dati)
	for _, r := range rap.Righe {
		fmt.Println(r)
	}
	for _, e := range rap.Errori {
		fmt.Println("ERRORE", e)
	}
	fmt.Printf("scenari %d, passi %d, differenze attese %d, inattese %d, errori %d\n",
		rap.Scenari, rap.Passi, rap.Attese, rap.Inattese, len(rap.Errori))
	if *esito != "" {
		b, _ := json.MarshalIndent(map[string]any{"verde": rap.ok(), "scenari": rap.Verdi,
			"inventario_controllato": *inv != "", "quando": time.Now().UTC().Format(time.RFC3339)}, "", "  ")
		if err := os.WriteFile(*esito, append(b, '\n'), 0o644); err != nil {
			return err
		}
	}
	if !rap.ok() {
		return errors.New("banco rosso")
	}
	fmt.Println("BANCO VERDE")
	return nil
}

// deviazioniSoloGo sono le deviazioni dei passi solo_go e solo_standalone degli scenari, senza
// ripetizioni.
func deviazioniSoloGo(ss []scenario) []string {
	var out []string
	for _, s := range ss {
		for _, p := range s.Passi {
			for _, id := range []string{p.SoloGo, p.SoloStandalone} {
				if id != "" && !slices.Contains(out, id) {
					out = append(out, id)
				}
			}
		}
	}
	return out
}

func leggiScenari(dir string) ([]scenario, error) {
	nomi, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(nomi)
	if len(nomi) == 0 {
		return nil, fmt.Errorf("nessuno scenario in %s", dir)
	}
	var ss []scenario
	visti := map[string]bool{}
	for _, n := range nomi {
		f, err := leggiFile(n)
		if err != nil {
			return nil, err
		}
		s, err := leggiScenario(filepath.Base(n), f)
		if err != nil {
			return nil, err
		}
		if visti[s.Nome] {
			return nil, fmt.Errorf("scenario %q duplicato", s.Nome)
		}
		visti[s.Nome] = true
		ss = append(ss, s)
	}
	return ss, nil
}

// cmdAttendi aspetta che ogni URL risponda 200 (al massimo 3 minuti).
func cmdAttendi(args []string) error {
	fs := flag.NewFlagSet("attendi", flag.ExitOnError)
	ca := fs.String("ca", "", "CA dell'istanza sintetica")
	_ = fs.Parse(args) // ExitOnError: con un errore esce da sola
	c, err := clientCon(*ca)
	if err != nil {
		return err
	}
	c.Timeout = 3 * time.Second
	fine := time.Now().Add(3 * time.Minute)
	for _, u := range fs.Args() {
		for {
			res, err := c.Get(u)
			if err == nil {
				_ = res.Body.Close() // risposta letta solo per il codice
				if res.StatusCode == 200 {
					break
				}
			}
			if time.Now().After(fine) {
				return fmt.Errorf("%s non risponde 200 (ultimo errore: %v)", u, err)
			}
			time.Sleep(2 * time.Second)
		}
	}
	return nil
}

// leggiFile legge tutto il file: niente da chiudere, quindi nessun errore di chiusura ignorato.
func leggiFile(p string) (*bytes.Reader, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(b), nil
}
