package main

import (
	"context"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// providerServe è un provider finto per serve: annota gli id e accetta. Nessun provider vero.
type providerServe struct {
	mu  sync.Mutex
	ids []int64
	via chan int64
}

func (p *providerServe) Consegna(_ context.Context, m uscita.Messaggio) (string, error) {
	p.mu.Lock()
	p.ids = append(p.ids, m.ID)
	p.mu.Unlock()
	p.via <- m.ID
	return "finto", nil
}

// inventario: CLI-51
// gestrip serve con un provider: la riga rimasta in coda prima dell'avvio (un riavvio) parte al primo
// giro, poi si aspetta 30 secondi (:dispatch_interval_ms). Senza provider (oggi, prima della slice 069)
// la coda non gira: le righe importate restano in coda, senza tentativi.
func TestServeCodaInUscita(t *testing.T) {
	vera := aspettaPer
	t.Cleanup(func() { aspettaPer = vera })
	durate := make(chan time.Duration, 8)
	aspettaPer = func(ctx context.Context, d time.Duration) bool {
		durate <- d
		<-ctx.Done()
		return false
	}
	riga := func(t *testing.T, percorso string) int64 {
		t.Helper()
		d, err := db.Apri(percorso)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = d.Chiudi() }()
		if _, err := d.Migra(context.Background(), clock.Sistema{}); err != nil {
			t.Fatal(err)
		}
		res, err := d.Scrittura.Exec(`INSERT INTO messaggi_in_uscita (canale, destinatario, indirizzo, testo, stato, tentativi,
			prossimo_tentativo_il, accodato_il) VALUES ('email', 'customer', 'cliente@esempio.it', 'testo', 'queued', 2, ?1, ?1)`,
			"2026-01-01T09:00:00.000000Z")
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	stato := func(t *testing.T, percorso string, id int64) string {
		t.Helper()
		d, err := db.Apri(percorso)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = d.Chiudi() }()
		var s string
		if err := d.Lettura.QueryRow(`SELECT stato || '|' || tentativi FROM messaggi_in_uscita WHERE id = ?`, id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	for _, conProvider := range []bool{true, false} {
		percorso := filepath.Join(t.TempDir(), "g.db")
		id := riga(t, percorso)
		p := &providerServe{via: make(chan int64, 4)}
		o := opzioniServe{SenzaVolumeCifrato: true, DB: percorso, Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0", avvisi: io.Discard}
		if conProvider {
			o.provider = map[string]uscita.Provider{uscita.CanaleEmail: p}
		}
		ctx, ferma := context.WithCancel(context.Background())
		finito := make(chan error, 1)
		go func() { finito <- serve(ctx, o, nil) }()
		viste := map[time.Duration]bool{}
		for len(viste) < 2 {
			select {
			case d := <-durate:
				viste[d] = true
			case err := <-finito:
				t.Fatal(err)
			case <-time.After(10 * time.Second):
				t.Fatalf("attese viste %v", viste)
			}
		}
		if conProvider {
			select {
			case got := <-p.via:
				if got != id {
					t.Errorf("riga %d, attesa %d", got, id)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("la riga rimasta in coda non è partita")
			}
			for len(viste) < 3 {
				select {
				case d := <-durate:
					viste[d] = true
				case <-time.After(10 * time.Second):
					t.Fatalf("attese viste %v", viste)
				}
			}
			if !viste[uscita.OgniQuanto] {
				t.Errorf("attese %v, manca quella della coda", viste)
			}
		}
		ferma()
		if err := <-finito; err != nil {
			t.Fatal(err)
		}
		for len(durate) > 0 {
			viste[<-durate] = true
		}
		want := map[bool]string{true: "accepted|3", false: "queued|2"}[conProvider]
		if got := stato(t, percorso, id); got != want {
			t.Errorf("provider %v: %s, atteso %s", conProvider, got, want)
		}
		if !conProvider && viste[uscita.OgniQuanto] {
			t.Error("senza provider la coda gira")
		}
	}
}
