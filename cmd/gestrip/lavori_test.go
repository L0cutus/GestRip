package main

import (
	"bytes"
	"context"
	"flag"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/jobs"
	"github.com/L0cutus/gestrip/internal/uscita"
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

// inventario: JOB-07, JOB-09
// serve registra nel pianificatore il lavoro dei ritardi (slice 095) e quello dei solleciti (096), sulla sua coda;
// il briefing non ancora (097).
func TestLavoriAutomatici(t *testing.T) {
	l := lavoriAutomatici(nil, nil, nil)
	_, ritardi := l[jobs.Ritardi].(*jobs.LavoroRitardi)
	_, solleciti := l[jobs.Solleciti].(*jobs.LavoroSolleciti)
	if !ritardi || !solleciti || len(l) != 2 {
		t.Errorf("lavori %#v", l)
	}
}

// inventario: JOB-09
// Ogni lavoro scrive nel registro di serve con il suo nome: un riepilogo dei solleciti rifiutato dalla coda
// (Telegram senza provider) lascia una riga con lavoro=solleciti, non lavoro=ritardi.
func TestLavoriAutomaticiRegistro(t *testing.T) {
	ctx := context.Background()
	d, err := db.Apri(filepath.Join(t.TempDir(), "g.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC))
	if _, err := d.Migra(ctx, o); err != nil {
		t.Fatal(err)
	}
	c := audit.Contesto{Attore: "titolare@esempio.test"}
	b := buste.Nuove(d, o)
	nome := "Bulino"
	art, err := b.CreaArtigiano(ctx, buste.DatiArtigiano{Nome: &nome}, c)
	if err != nil {
		t.Fatal(err)
	}
	a := buste.Accettazione{NomeCliente: "Cliente 1/26", TelefonoCliente: "333 000 0003", Numero: "1/26",
		DataAccettazione: "2026-09-01", Descrizione: "Anello", CodiceQR: "QR-1-26"}
	a.GemelleConfermate = buste.EtichettaScritta(a.CodiceQR, buste.AnnoDiAccettazione(a.DataAccettazione, o.Adesso()))
	if _, err := b.Crea(ctx, a, c); err != nil {
		t.Fatal(err)
	}
	cons, err := b.ApriConsegna(ctx, art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.AggiungiAllaConsegna(ctx, cons, "QR-1-26", buste.LetturaScansione); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ConfermaConsegna(ctx, cons, c); err != nil {
		t.Fatal(err)
	}
	o.Sposta(20 * 24 * time.Hour)
	var registro bytes.Buffer
	l := lavoriAutomatici(d, uscita.Nuova(d, o, nil), slog.New(slog.NewTextHandler(&registro, nil)))
	r, err := l[jobs.Solleciti].Esegui(ctx, jobs.Config{}, o.Adesso(),
		[]uscita.Destinazione{{Canale: uscita.CanaleTelegram, Destinatario: "shop", Indirizzo: "-1004242"}}, c)
	if err != nil || !r.Fallito {
		t.Fatalf("esecuzione %+v %v", r, err)
	}
	if out := registro.String(); !strings.Contains(out, "lavoro=solleciti") || strings.Contains(out, "lavoro=ritardi") ||
		!strings.Contains(out, "riepilogo non accodato") {
		t.Errorf("registro %q", out)
	}
}
