package main

import (
	"bytes"
	"context"
	"flag"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/jobs"
)

// inventario: JOB-02
// gestrip serve fa partire il pianificatore dei lavori automatici (la prima attesa, poi un giro) e allo stop
// lo ferma e lo aspetta prima di tornare: nessuna goroutine resta viva. Con -senza-lavori-automatici
// (sviluppo e banco) non parte e serve lo dice forte.
func TestServePianificatore(t *testing.T) {
	for _, spento := range []bool{false, true} {
		var mu sync.Mutex
		attese := 0
		partito := make(chan struct{}, 1)
		uscito := make(chan struct{})
		var avvisi bytes.Buffer
		o := opzioniServe{SenzaVolumeCifrato: true, DB: filepath.Join(t.TempDir(), "g.db"), Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0",
			avvisi: &avvisi, SenzaLavoriAutomatici: spento,
			aspettaConservazione: func(ctx context.Context) bool { <-ctx.Done(); return false },
			aspettaPulizia:       func(ctx context.Context) bool { <-ctx.Done(); return false },
			aspettaLavori: func(ctx context.Context) bool {
				mu.Lock()
				attese++
				mu.Unlock()
				partito <- struct{}{}
				<-ctx.Done()
				time.Sleep(200 * time.Millisecond) // serve torna solo dopo: lo aspetta davvero
				close(uscito)
				return false
			}}
		ctx, ferma := context.WithCancel(context.Background())
		finito := make(chan error, 1)
		pronto := make(chan struct{})
		go func() { finito <- serve(ctx, o, func(net.Addr, net.Addr) { close(pronto) }) }()
		select {
		case <-pronto:
		case err := <-finito:
			t.Fatal(err)
		case <-time.After(10 * time.Second):
			t.Fatal("serve non parte")
		}
		if !spento {
			select {
			case <-partito:
			case <-time.After(10 * time.Second):
				t.Fatal("il pianificatore non è partito")
			}
		}
		ferma()
		select {
		case err := <-finito:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("serve non si ferma")
		}
		mu.Lock()
		n := attese
		mu.Unlock()
		if spento {
			if n != 0 {
				t.Errorf("con %s il pianificatore è partito", opzioneSenzaLavoriAutomatici)
			}
			if !strings.Contains(avvisi.String(), "ATTENZIONE: lavori automatici spenti ("+opzioneSenzaLavoriAutomatici+")") {
				t.Errorf("avviso: %q", avvisi.String())
			}
			continue
		}
		select {
		case <-uscito:
		default:
			t.Error("serve è tornato prima che il pianificatore uscisse")
		}
		if n != 1 || strings.Contains(avvisi.String(), "lavori automatici") {
			t.Errorf("%d attese; avvisi %q", n, avvisi.String())
		}
	}
}

// inventario: JOB-02
// -senza-lavori-automatici è un'opzione di serve, spenta di default.
func TestOpzioneSenzaLavoriAutomatici(t *testing.T) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := opzioniDiServe(fs)
	if err := fs.Parse(nil); err != nil || o.SenzaLavoriAutomatici {
		t.Errorf("di default: %v %v", o.SenzaLavoriAutomatici, err)
	}
	fs = flag.NewFlagSet("serve", flag.ContinueOnError)
	o = opzioniDiServe(fs)
	if err := fs.Parse([]string{opzioneSenzaLavoriAutomatici}); err != nil || !o.SenzaLavoriAutomatici {
		t.Errorf("con l'opzione: %v %v", o.SenzaLavoriAutomatici, err)
	}
}

// inventario: JOB-07
// serve registra nel pianificatore il lavoro dei ritardi (slice 095), sulla sua coda; solleciti e briefing non
// ancora (096, 097).
func TestLavoriAutomaticiRitardi(t *testing.T) {
	l := lavoriAutomatici(nil, nil, nil)
	if _, ok := l[jobs.Ritardi].(*jobs.LavoroRitardi); !ok || len(l) != 1 {
		t.Errorf("lavori %#v", l)
	}
}
