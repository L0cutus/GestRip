package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/L0cutus/gestrip/internal/assist"
	"github.com/L0cutus/gestrip/internal/verifica"
)

// I file del banco del modello nel repo (bench/modello/): i banchi B, C, D, D2 del 03/11/2026, tutti
// inventati. Gli 84 casi dello standalone restano fuori dal repo (si rigenerano in locale e si passano
// con -casi: bench/modello/README.md).
var casiDiDefault = []string{
	"bench/modello/B-difficili.jsonl",
	"bench/modello/C-acconto-test.jsonl",
	"bench/modello/D-prezzi-verbo.jsonl",
	"bench/modello/D2-prezzi-verbo-nuovi.jsonl",
}

// Le due oreficerie che nominano gli esempi del prompt dello standalone e i suoi casi (@default_oreficerie).
const oreficerieDiDefault = "Bottari,Bianchi Gioielli"

// opzioniVerifica sono le opzioni di `gestrip verifica-modello`.
type opzioniVerifica struct {
	Indirizzo, Modello string
	Casi               string // file separati da virgole; vuoto: casiDiDefault
	Oreficerie         string // nomi separati da virgole
	PerCategoria       int    // 0: tutti i casi
	// cronometro sostituisce quello vero nei test (orologio finto).
	cronometro verifica.Cronometro
}

func opzioniDiVerifica(fs *flag.FlagSet) *opzioniVerifica {
	o := &opzioniVerifica{}
	fs.StringVar(&o.Indirizzo, "indirizzo", "", "indirizzo del server compatibile OpenAI (http:// o https://); le misure della slice 112 sono con llama-server --reasoning off -n 256 --cache-ram 0")
	fs.StringVar(&o.Modello, "modello", "", "nome del modello da mandare nel campo model")
	fs.StringVar(&o.Casi, "casi", "", "file di casi separati da virgole (di default i quattro banchi di bench/modello/)")
	fs.StringVar(&o.Oreficerie, "oreficerie", oreficerieDiDefault, "oreficerie che il negozio conosce, separate da virgole (vuoto: nessuna)")
	fs.IntVar(&o.PerCategoria, "per-categoria", 0, "al più N casi per categoria di ogni file, distribuiti uniformi (0: tutti)")
	return o
}

// errUsoVerifica è un'opzione sbagliata: main esce con 2.
var errUsoVerifica = errors.New("uso: gestrip verifica-modello -indirizzo URL -modello NOME [-casi FILE,…] [-oreficerie NOMI] [-per-categoria N]")

// verificaModello è il banco del modello (slice 112; RIP-H-54, OPS-08, come `mix assist.bench`): manda
// ogni caso al server come la nuova busta e scrive su out il punteggio per campo, i tempi e una riga
// per caso sbagliato; su avanzamento i casi fatti, ogni 25. Non stampa mai il dettato né la risposta,
// non manda chiavi (il comando non ne prende: una chiave sulla riga di comando finisce nella storia
// della shell), non apre database e non scrive audit. Un ctx finito (Ctrl-C, timeout) stampa quello
// che ha fatto e dà l'errore.
func verificaModello(ctx context.Context, o opzioniVerifica, out, avanzamento io.Writer) error {
	if strings.TrimSpace(o.Modello) == "" || !indirizzoHTTP(o.Indirizzo) || o.PerCategoria < 0 {
		return errUsoVerifica
	}
	percorsi := casiDiDefault
	if strings.TrimSpace(o.Casi) != "" {
		percorsi = elenco(o.Casi)
	}
	casi, err := verifica.CaricaTutti(percorsi)
	if err != nil {
		return err
	}
	casi = verifica.Campione(casi, o.PerCategoria)
	var negozio assist.Negozio
	for i, nome := range elenco(o.Oreficerie) {
		negozio.Oreficerie = append(negozio.Oreficerie, assist.Oreficeria{ID: int64(i + 2), Nome: nome})
	}
	c := &assist.Collegamento{Config: assist.Configurazione{Indirizzo: o.Indirizzo, Modello: o.Modello}}
	corsa := verifica.Corsa{Estrai: verifica.DallaBusta(c), Negozio: negozio, Cronometro: o.cronometro,
		Avanzamento: func(fatti, totale int) {
			if fatti%25 == 0 || fatti == totale {
				_, _ = fmt.Fprintf(avanzamento, "gestrip: verifica-modello: %d/%d\n", fatti, totale) // solo avanzamento
			}
		}}
	esiti, errCorsa := corsa.Gira(ctx, casi)
	if _, err := fmt.Fprintf(out, "banco del modello: %s, %d casi su %d\n", strings.TrimSpace(o.Modello), len(esiti), len(casi)); err != nil {
		return err
	}
	righe := verifica.Rapporto(verifica.Riassumi(esiti))
	for _, e := range esiti {
		if riga := verifica.RigaDelCaso(e); riga != "" {
			righe = append(righe, riga)
		}
	}
	for _, riga := range righe {
		if _, err := fmt.Fprintln(out, riga); err != nil {
			return err
		}
	}
	if errCorsa != nil {
		return fmt.Errorf("verifica interrotta dopo %d casi su %d", len(esiti), len(casi))
	}
	return nil
}

// indirizzoHTTP dice se l'indirizzo è http:// o https:// con un host e senza utente né password. Il resto lo controlla il client.
func indirizzoHTTP(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	// Niente credenziali nell'indirizzo: non si manda niente che somigli a una chiave (B6 della revisione).
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}

// elenco sono i pezzi non vuoti, senza spazi ai bordi, di un testo separato da virgole.
func elenco(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
