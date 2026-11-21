package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/sidecar"
)

// argSidecarFinto: con «sidecar-finto <porta>» il binario dei test fa da sidecar (/health 200); a
// SIGTERM esce dopo 300 ms.
const argSidecarFinto = "sidecar-finto"

func sidecarFinto(porta string) int {
	l, err := net.Listen("tcp", "127.0.0.1:"+porta)
	if err != nil {
		return 7
	}
	go func() { time.Sleep(time.Minute); os.Exit(8) }() // un test caduto non lo lascia acceso
	// Un arresto che prende un po' di tempo (come un server che chiude): serve deve aspettarlo.
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	go func() { <-term; time.Sleep(300 * time.Millisecond); os.Exit(0) }()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") })
	_ = http.Serve(l, mux)
	return 9
}

// portaLiberaServe è una porta libera a caso fra 20000 e 32000, sotto l'intervallo effimero
// (revisione della 111, M1), senza le porte di gestrip-switch e llm-switch.
func portaLiberaServe(t *testing.T) int {
	t.Helper()
	vietate := map[int]bool{43651: true, 43653: true, 43660: true, 43661: true, 43662: true, 43671: true}
	for range 200 {
		p := 20000 + rand.IntN(12000)
		if vietate[p] {
			continue
		}
		l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p))
		if err != nil {
			continue
		}
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Fatal("nessuna porta libera fra 20000 e 32000")
	return 0
}

// serveConSidecar avvia serve con il file dell'apparecchio dato e dà i supervisori, l'indirizzo
// della faccia interna e la funzione che ferma serve e ne dà l'esito.
func serveConSidecar(t *testing.T, conf string) ([]*sidecar.Supervisore, string, func() error) {
	t.Helper()
	ctx, ferma := context.WithCancel(context.Background())
	pronto := make(chan string, 1)
	finito := make(chan error, 1)
	var sup []*sidecar.Supervisore
	go func() {
		finito <- serve(ctx, opzioniServe{SenzaVolumeCifrato: true, DB: filepath.Join(t.TempDir(), "gestrip.db"),
			Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0", Config: configurazione(t, conf),
			conSidecar: func(s *sidecar.Supervisore) { sup = append(sup, s) }},
			func(i, _ net.Addr) { pronto <- i.String() })
	}()
	var interna string
	select {
	case interna = <-pronto:
	case err := <-finito:
		ferma()
		t.Fatal(err)
	case <-time.After(20 * time.Second):
		ferma()
		t.Fatal("serve non parte")
	}
	var una sync.Once
	var esito error
	chiudi := func() error {
		una.Do(func() {
			ferma()
			select {
			case esito = <-finito:
			case <-time.After(30 * time.Second):
				esito = errors.New("serve non si ferma")
			}
		})
		return esito
	}
	t.Cleanup(func() { _ = chiudi() })
	return sup, interna, chiudi
}

// Con un sidecar nel file dell'apparecchio serve lo avvia, lo tiene pronto e alla chiusura lo
// ferma prima di tornare: nessun figlio rimane.
func TestServeAvviaIlSidecar(t *testing.T) {
	porta := portaLiberaServe(t)
	sup, _, chiudi := serveConSidecar(t, fmt.Sprintf("sidecar_estrazione = %s %s {porta}\nsidecar_estrazione_porta = %d\n",
		os.Args[0], argSidecarFinto, porta))
	if len(sup) != 1 {
		t.Fatalf("supervisori: %d", len(sup))
	}
	var pid int
	fine := time.Now().Add(20 * time.Second)
	for {
		st := sup[0].Stato()
		if st.Fase == sidecar.FasePronto {
			pid = st.Pid
			break
		}
		if time.Now().After(fine) {
			t.Fatalf("sidecar mai pronto: %+v", st)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if sup[0].Indirizzo() != fmt.Sprintf("http://127.0.0.1:%d", porta) {
		t.Errorf("indirizzo %s", sup[0].Indirizzo())
	}
	if err := chiudi(); err != nil {
		t.Fatalf("chiusura: %v", err)
	}
	// serve è tornato: il figlio è già raccolto e il suo gruppo vuoto.
	if !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) || !errors.Is(syscall.Kill(-pid, 0), syscall.ESRCH) {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		t.Errorf("sidecar %d ancora vivo dopo la fine di serve", pid)
	}
	if st := sup[0].Stato(); st.Fase != sidecar.FaseSpento {
		t.Errorf("dopo serve: %+v", st)
	}
}

// Un sidecar che non parte non ferma serve: le facce rispondono lo stesso.
func TestServeSenzaSidecarCheParte(t *testing.T) {
	conf := fmt.Sprintf("sidecar_trascrizione = %s {porta}\nsidecar_trascrizione_porta = %d\n",
		filepath.Join(t.TempDir(), "non-c-e"), portaLiberaServe(t))
	sup, interna, chiudi := serveConSidecar(t, conf)
	if len(sup) != 1 {
		t.Fatalf("supervisori: %d", len(sup))
	}
	fine := time.Now().Add(20 * time.Second)
	for len(sup[0].Stato().Attese) < 2 {
		if time.Now().After(fine) {
			t.Fatalf("nessun nuovo tentativo: %+v", sup[0].Stato())
		}
		time.Sleep(5 * time.Millisecond)
	}
	r, err := http.Get("http://" + interna + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Body.Close()
	if r.StatusCode != 200 {
		t.Errorf("/healthz %d", r.StatusCode)
	}
	if err := chiudi(); err != nil {
		t.Errorf("chiusura: %v", err)
	}
}

// Senza righe dei sidecar (il default) serve non avvia niente.
func TestServeSenzaSidecarDiDefault(t *testing.T) {
	sup, _, chiudi := serveConSidecar(t, "indirizzo_interno = https://negozio.esempio.ts.net:8443\n")
	if len(sup) != 0 {
		t.Errorf("supervisori senza configurazione: %d", len(sup))
	}
	if err := chiudi(); err != nil {
		t.Errorf("chiusura: %v", err)
	}
}

// Una faccia che cade fa tornare serve con un errore anche con un sidecar acceso: il supervisore
// segue il contesto dei lavori (non quello di serve, che qui non finisce) e il figlio è fermato
// prima che serve torni (revisione della 111, B7).
func TestServeFacciaCadutaFermaIlSidecar(t *testing.T) {
	var interna net.Listener
	var sup []*sidecar.Supervisore
	conf := configurazione(t, fmt.Sprintf("sidecar_estrazione = %s %s {porta}\nsidecar_estrazione_porta = %d\n",
		os.Args[0], argSidecarFinto, portaLiberaServe(t)))
	o := opzioniServe{SenzaVolumeCifrato: true, DB: filepath.Join(t.TempDir(), "g.db"), Interna: "127.0.0.1:0",
		Pubblica: "127.0.0.1:0", Config: conf, avvisi: io.Discard,
		ascolta: func(rete, ind string) (net.Listener, error) {
			l, err := net.Listen(rete, ind)
			if interna == nil {
				interna = l
			}
			return l, err
		},
		conSidecar: func(s *sidecar.Supervisore) { sup = append(sup, s) }}
	finito := make(chan error, 1)
	pronto := make(chan struct{})
	go func() { finito <- serve(context.Background(), o, func(net.Addr, net.Addr) { close(pronto) }) }()
	select {
	case <-pronto:
	case err := <-finito:
		t.Fatal(err)
	}
	var pid int
	fine := time.Now().Add(20 * time.Second)
	for {
		if st := sup[0].Stato(); st.Fase == sidecar.FasePronto {
			pid = st.Pid
			break
		}
		if time.Now().After(fine) {
			t.Fatalf("sidecar mai pronto: %+v", sup[0].Stato())
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = interna.Close() // la faccia interna cade
	select {
	case err := <-finito:
		if err == nil {
			t.Error("faccia caduta senza errore")
		}
	case <-time.After(30 * time.Second):
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		t.Fatal("serve appeso dopo la caduta di una faccia, con il sidecar acceso")
	}
	if !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		t.Errorf("sidecar %d ancora vivo dopo la fine di serve", pid)
	}
}
