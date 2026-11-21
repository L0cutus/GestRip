package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/verifica"
	"github.com/L0cutus/gestrip/tools/banco/modellofinto"
)

// gestrip verifica-modello (slice 112). Dettati, nomi e telefoni inventati; server del modello finto.

// Un dettato inventato con parole che non compaiono altrove: nessuna deve arrivare all'uscita.
const (
	dettatoSegreto = "Cliente Ermenegildo Fittizzi 300 987 6543 orologio Zaffirotto da revisionare, preventivo 70 euro"
	telSegreto     = "3009876543"
)

var paroleSegrete = []string{"Ermenegildo", "Fittizzi", "987", "6543", telSegreto, "Zaffirotto", "revisionare", "orologio"}

func rigaSegreta(id string) string {
	return `{"id": "` + id + `", "oggi": "2026-10-23", "testo": "` + dettatoSegreto + `", "attesi": {"data": ["23/10/2026"], ` +
		`"nome": ["Ermenegildo Fittizzi"], "telefono": ["` + telSegreto + `"], "preventivo": ["70"], "data_consegna": [""], ` +
		`"oreficeria": [""], "compro_oro": [0]}, "busta": null, "parola": "revision"}`
}

func fileDiCasi(t *testing.T, righe ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "casi.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(righe, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// cronometroDiProva dà 100, 200, 300… ms, uno per caso.
func cronometroDiProva() verifica.Cronometro {
	n := 0
	return func() func() time.Duration {
		n++
		d := time.Duration(n*100) * time.Millisecond
		return func() time.Duration { return d }
	}
}

// inventario: RIP-H-54, OPS-08
// Il comando con il modello finto: punteggio per campo, tempi, una riga per caso sbagliato; il dettato,
// la risposta del modello e il nome che il server dice di sé non arrivano mai all'uscita, né al registro,
// nemmeno quando il server li ripete in un errore o in una risposta illeggibile.
func TestVerificaModelloNonStampaIlDettato(t *testing.T) {
	var registro bytes.Buffer
	log.SetOutput(&registro)
	vecchio := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&registro, nil)))
	t.Cleanup(func() { log.SetOutput(os.Stderr); slog.SetDefault(vecchio) })

	f := modellofinto.Nuovo()
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	eco := modellofinto.Testo(dettatoSegreto)
	f.Programma(
		// c1: giusta, con il nome del modello che ripete il dettato.
		modellofinto.Risposta{Contenuto: modellofinto.Testo(`{"n": "ermenegildo fittizzi", "d": "orologio Zaffirotto da revisionare", "p": 70}`), Modello: modellofinto.Testo("Fittizzi Zaffirotto")},
		// c2: il preventivo sbagliato.
		modellofinto.Buona(`{"n": "Ermenegildo Fittizzi", "d": "orologio Zaffirotto da revisionare", "p": "70 euro"}`),
		// c3: due risposte illeggibili che ripetono il dettato.
		modellofinto.Risposta{Contenuto: eco}, modellofinto.Risposta{Contenuto: eco},
		// c4: due 500 con il dettato nel corpo.
		modellofinto.Risposta{Stato: 500, Grezzo: eco}, modellofinto.Risposta{Stato: 500, Grezzo: eco},
	)
	var out, avanz bytes.Buffer
	o := opzioniVerifica{Indirizzo: s.URL, Modello: " candidato-prova ", Casi: fileDiCasi(t, rigaSegreta("c-1"), rigaSegreta("c-2"), rigaSegreta("c-3"), rigaSegreta("c-4")),
		Oreficerie: oreficerieDiDefault, cronometro: cronometroDiProva()}
	if err := verificaModello(context.Background(), o, &out, &avanz); err != nil {
		t.Fatal(err)
	}
	atteso := `banco del modello: candidato-prova, 4 casi su 4
schede tutte giuste: 1/4
  casi.jsonl: 1/4
per campo:
  data: 2/4
  nome: 2/4
  telefono: 2/4
  oreficeria: 2/4
  parola: 2/4
  preventivo: 1/4
  data_consegna: 2/4
  compro_oro: 2/4 (dal testo, non dal modello)
acconti inventati: 0
buste inventate: 0
risposte illeggibili: 1
rifiuti: 1 (ai:http:500 1)
casi con un nuovo tentativo: 2
tempo per scheda (ms): mediana 200, p90 400, max 400
c-2: preventivo
c-3: risposta illeggibile
c-4: rifiutato (ai:http:500)
`
	if out.String() != atteso {
		t.Errorf("uscita:\n%s", out.String())
	}
	if avanz.String() != "gestrip: verifica-modello: 4/4\n" {
		t.Errorf("avanzamento: %q", avanz.String())
	}
	tutto := out.String() + avanz.String() + registro.String()
	for _, p := range append(paroleSegrete, s.URL, "127.0.0.1") {
		if strings.Contains(tutto, p) {
			t.Errorf("l'uscita contiene %q", p)
		}
	}
	// Nessuna chiave: il comando non ne prende e non ne manda.
	for i, r := range f.Richieste() {
		if r.ConAutorizz {
			t.Errorf("richiesta %d con Authorization", i)
		}
	}
}

// Le opzioni sbagliate sono un errore d'uso (uscita 2); un file di casi sbagliato un errore che non
// ripete la riga; il comando non chiede niente al server prima di aver letto tutti i casi.
func TestVerificaModelloRifiuti(t *testing.T) {
	f := modellofinto.Nuovo()
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	casi := fileDiCasi(t, rigaSegreta("c-1"))
	for _, o := range []opzioniVerifica{
		{Indirizzo: s.URL, Modello: "", Casi: casi},
		{Indirizzo: s.URL, Modello: "  ", Casi: casi},
		{Indirizzo: "", Modello: "m", Casi: casi},
		{Indirizzo: "127.0.0.1:8080", Modello: "m", Casi: casi},
		{Indirizzo: "ftp://127.0.0.1", Modello: "m", Casi: casi},
		{Indirizzo: "http://", Modello: "m", Casi: casi},
		{Indirizzo: "http://utente:segreto@127.0.0.1:1", Modello: "m", Casi: casi}, // credenziali nell'indirizzo (B6)
		{Indirizzo: "http://utente@127.0.0.1:1", Modello: "m", Casi: casi},
		{Indirizzo: s.URL, Modello: "m", Casi: casi, PerCategoria: -1},
	} {
		var out bytes.Buffer
		if err := verificaModello(context.Background(), o, &out, &out); !errors.Is(err, errUsoVerifica) || out.Len() != 0 {
			t.Errorf("%+v: %v %q", o, err, out.String())
		}
	}
	rotto := fileDiCasi(t, rigaSegreta("c-1"), `{"id": "c-2", "testo": "`+dettatoSegreto+`"}`)
	var out bytes.Buffer
	err := verificaModello(context.Background(), opzioniVerifica{Indirizzo: s.URL, Modello: "m", Casi: casi + "," + rotto}, &out, &out)
	if err == nil || err.Error() != "casi.jsonl, riga 2: caso non valido" || out.Len() != 0 {
		t.Errorf("file rotto: %v %q", err, out.String())
	}
	err = verificaModello(context.Background(), opzioniVerifica{Indirizzo: s.URL, Modello: "m", Casi: casi + " , " + casi}, &out, &out)
	if err == nil || err.Error() != "id ripetuto: c-1" {
		t.Errorf("id ripetuti: %v", err)
	}
	if n := len(f.Richieste()); n != 0 {
		t.Errorf("%d richieste", n)
	}
}

// Fermato (Ctrl-C, timeout esterno): stampa quello che ha fatto e dà l'errore.
func TestVerificaModelloInterrotta(t *testing.T) {
	s := httptest.NewServer(modellofinto.Nuovo())
	t.Cleanup(s.Close)
	ctx, ferma := context.WithCancel(context.Background())
	ferma()
	var out bytes.Buffer
	err := verificaModello(ctx, opzioniVerifica{Indirizzo: s.URL, Modello: "m", Casi: fileDiCasi(t, rigaSegreta("c-1"), rigaSegreta("c-2"))}, &out, &out)
	if err == nil || err.Error() != "verifica interrotta dopo 0 casi su 2" || !strings.HasPrefix(out.String(), "banco del modello: m, 0 casi su 2\nschede tutte giuste: 0/0\n") {
		t.Errorf("%v\n%s", err, out.String())
	}
}

// Senza -casi legge i quattro banchi di bench/modello/ dalla cartella del repo; -per-categoria ne tiene
// uno per categoria (9 di B, 3 di C, 10 di D, 8 di D2); -oreficerie vuoto non ne conosce nessuna.
func TestVerificaModelloCasiDelRepo(t *testing.T) {
	f := modellofinto.Nuovo()
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	t.Chdir(filepath.Join("..", ".."))
	var out, avanz bytes.Buffer
	o := opzioniVerifica{Indirizzo: s.URL, Modello: "m", PerCategoria: 1, Oreficerie: " , ", cronometro: cronometroDiProva()}
	if err := verificaModello(context.Background(), o, &out, &avanz); err != nil {
		t.Fatal(err)
	}
	for _, r := range []string{"banco del modello: m, 30 casi su 30\n", "  B-difficili.jsonl: ", "  C-acconto-test.jsonl: ",
		"  D-prezzi-verbo.jsonl: ", "  D2-prezzi-verbo-nuovi.jsonl: "} {
		if !strings.Contains(out.String(), r) {
			t.Errorf("manca %q:\n%s", r, out.String())
		}
	}
	if avanz.String() != "gestrip: verifica-modello: 25/30\ngestrip: verifica-modello: 30/30\n" || len(f.Richieste()) != 30 ||
		strings.Contains(out.String(), "A-standalone") {
		t.Errorf("avanzamento %q, %d richieste", avanz.String(), len(f.Richieste()))
	}
}

// -oreficerie arriva alla lettura del nome: di default Bottari e Bianchi Gioielli, vuoto nessuna.
func TestVerificaModelloOreficerie(t *testing.T) {
	f := modellofinto.Nuovo()
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	casi := fileDiCasi(t, `{"cat": "oreficeria", "oggi": "venerdì 23/10/2026", "dettato": "da Bianchi Gioielli, catenina da saldare", "risposta": {"o": "Bianchi Gioielli", "d": "catenina da saldare"}}`)
	for _, c := range []struct{ oreficerie, riga string }{
		{oreficerieDiDefault, "casi-1: oreficeria\n"},
		{"Altra, Bianchi Gioielli ", "casi-1: oreficeria\n"},
		{"", "casi-1: nome, oreficeria\n"},
	} {
		f.Programma(modellofinto.Buona(`{"n": "bianchi gioielli", "d": "catenina da saldare"}`))
		var out bytes.Buffer
		err := verificaModello(context.Background(), opzioniVerifica{Indirizzo: s.URL, Modello: "m", Casi: casi, Oreficerie: c.oreficerie}, &out, io.Discard)
		if err != nil || !strings.HasSuffix(out.String(), c.riga) {
			t.Errorf("%q: %v\n%s", c.oreficerie, err, out.String())
		}
	}
}
