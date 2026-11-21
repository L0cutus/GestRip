package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/tools/banco/modellofinto"
)

// inventario: RIP-H-54, OPS-08
// gestrip verifica-modello, processo vero: esce con 0 dopo la corsa e scrive il resoconto su stdout,
// con 2 per un'opzione sbagliata o un argomento in più, con 1 per un file di casi sbagliato; il dettato
// non compare mai, né su stdout né su stderr.
func TestVerificaModelloProcesso(t *testing.T) {
	f := modellofinto.Nuovo()
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	casi := fileDiCasi(t, rigaSegreta("c-1"))
	for _, c := range []struct {
		nome    string
		args    []string
		uscita  int
		stdout  string // prefisso
		stderr  string // contenuto
		domande int
	}{
		{"corsa", []string{"-indirizzo", s.URL, "-modello", "m", "-casi", casi}, 0, "banco del modello: m, 1 casi su 1\nschede tutte giuste: 0/1\n", "gestrip: verifica-modello: 1/1\n", 1},
		{"senza modello", []string{"-indirizzo", s.URL, "-casi", casi}, 2, "", "gestrip: uso: gestrip verifica-modello -indirizzo URL", 0},
		{"argomento in più", []string{"-indirizzo", s.URL, "-modello", "m", "-casi", casi, "altro"}, 2, "", "gestrip: ", 0},
		{"opzione sconosciuta", []string{"-chiave", "x"}, 2, "", "flag provided but not defined: -chiave", 0},
		{"casi sbagliati", []string{"-indirizzo", s.URL, "-modello", "m", "-casi", fileDiCasi(t, `{"testo": "`+dettatoSegreto+`"}`)}, 1, "", "gestrip: casi.jsonl, riga 1: caso non valido\n", 0},
	} {
		f.Programma()
		prima := len(f.Richieste())
		var stdout bytes.Buffer
		cmd, righe := figlioCon(t, func(c *exec.Cmd) { c.Stdout = &stdout }, nil, append([]string{"verifica-modello"}, c.args...)...)
		var stderr strings.Builder
		for r := range righe {
			stderr.WriteString(r + "\n")
		}
		err := cmd.Wait()
		var ue *exec.ExitError
		codice := 0
		if errors.As(err, &ue) {
			codice = ue.ExitCode()
		} else if err != nil {
			t.Fatalf("%s: %v", c.nome, err)
		}
		if codice != c.uscita || !strings.HasPrefix(stdout.String(), c.stdout) || !strings.Contains(stderr.String(), c.stderr) ||
			len(f.Richieste())-prima != c.domande {
			t.Errorf("%s: uscita %d, %d domande\nstdout:\n%s\nstderr:\n%s", c.nome, codice, len(f.Richieste())-prima, stdout.String(), stderr.String())
		}
		for _, p := range paroleSegrete {
			if strings.Contains(stdout.String()+stderr.String(), p) {
				t.Errorf("%s: l'uscita contiene %q", c.nome, p)
			}
		}
	}
}

// gestrip verifica-modello, processo vero, fermato con SIGTERM mentre il server tiene ferma la seconda
// domanda (su un canale, nessuna attesa a tempo): esce con 1 e stampa il resoconto del caso fatto.
func TestVerificaModelloProcessoFermato(t *testing.T) {
	arrivata := make(chan struct{})
	var mu sync.Mutex
	n := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		quale := n
		mu.Unlock()
		// Il corpo letto tutto: solo allora net/http si accorge della connessione chiusa e chiude r.Context().
		_, _ = io.Copy(io.Discard, r.Body)
		if quale == 1 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices": [{"message": {"content": "{\"d\": \"x\"}"}}]}`)) // la risposta è già partita
			return
		}
		close(arrivata)
		<-r.Context().Done() // ferma finché il figlio chiude la connessione
	}))
	t.Cleanup(s.Close)
	var stdout bytes.Buffer
	cmd, righe := figlioCon(t, func(c *exec.Cmd) { c.Stdout = &stdout }, nil,
		"verifica-modello", "-indirizzo", s.URL, "-modello", "m", "-casi", fileDiCasi(t, rigaSegreta("c-1"), rigaSegreta("c-2"), rigaSegreta("c-3")))
	select {
	case <-arrivata:
	case <-time.After(30 * time.Second):
		t.Fatal("la seconda domanda non è arrivata")
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	for r := range righe {
		stderr.WriteString(r + "\n")
	}
	err := cmd.Wait()
	var ue *exec.ExitError
	if !errors.As(err, &ue) || ue.ExitCode() != 1 || !strings.HasPrefix(stdout.String(), "banco del modello: m, 1 casi su 3\nschede tutte giuste: 0/1\n") ||
		!strings.Contains(stderr.String(), "gestrip: verifica interrotta dopo 1 casi su 3\n") {
		t.Errorf("%v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
}
