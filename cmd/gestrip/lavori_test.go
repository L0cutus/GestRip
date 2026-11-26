package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/backup"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/jobs"
	"github.com/L0cutus/gestrip/internal/uscita"
	"github.com/L0cutus/gestrip/internal/web"
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
		var lavoriPagina web.LavoriAutomatici
		var paginaPromemoria jobs.Pagina
		var errPagina error
		o := opzioniServe{SenzaVolumeCifrato: true, DB: filepath.Join(t.TempDir(), "g.db"), Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0",
			avvisi: &avvisi, SenzaLavoriAutomatici: spento,
			conDipendenze: func(d web.Dipendenze) {
				lavoriPagina = d.Lavori
				if d.Lavori != nil {
					paginaPromemoria, errPagina = d.Lavori.Pagina(context.Background()) // il database è aperto e migrato
					// I lavori sono quelli veri: senza canali «Esegui adesso» dice ErrSenzaCanali, non «sconosciuto».
					if _, err := d.Lavori.EseguiAMano(context.Background(), jobs.Ritardi, audit.Contesto{Attore: "titolare@esempio.test"}); !errors.Is(err, jobs.ErrSenzaCanali) {
						errPagina = err
					}
				}
			},
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
		// La pagina «Promemoria e job» riceve il pianificatore con i tre lavori anche con l'opzione (slice 098).
		if p, ok := lavoriPagina.(*jobs.Pianificatore); !ok || p == nil {
			t.Errorf("spento=%v: la pagina dei promemoria non ha il pianificatore (%T)", spento, lavoriPagina)
		} else if errPagina != nil || len(paginaPromemoria.Lavori) != 3 || paginaPromemoria.Campi["overdue_hour"] != "09:00" {
			t.Errorf("spento=%v: pagina %+v %v", spento, paginaPromemoria, errPagina)
		}
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

// inventario: JOB-07, JOB-09, JOB-10
// serve registra nel pianificatore il lavoro dei ritardi (slice 095), quello dei solleciti (096) e il briefing
// (097), sulla sua coda.
func TestLavoriAutomatici(t *testing.T) {
	l := lavoriAutomatici(nil, nil, nil)
	_, ritardi := l[jobs.Ritardi].(*jobs.LavoroRitardi)
	_, solleciti := l[jobs.Solleciti].(*jobs.LavoroSolleciti)
	_, briefing := l[jobs.Briefing].(*jobs.LavoroBriefing)
	if !ritardi || !solleciti || !briefing || len(l) != 3 {
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

// inventario: JOB-10
// Il briefing scrive nel registro di serve con lavoro=briefing: un briefing rifiutato dalla coda (Telegram senza
// provider) lascia la riga del motivo con il suo nome, non quello degli altri lavori.
func TestLavoriAutomaticiRegistroBriefing(t *testing.T) {
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
	var registro bytes.Buffer
	l := lavoriAutomatici(d, uscita.Nuova(d, o, nil), slog.New(slog.NewTextHandler(&registro, nil)))
	r, err := l[jobs.Briefing].Esegui(ctx, jobs.Config{}, o.Adesso(),
		[]uscita.Destinazione{{Canale: uscita.CanaleTelegram, Destinatario: "shop", Indirizzo: "-1004242"}}, audit.Contesto{Attore: "titolare@esempio.test"})
	if err != nil || !r.Fallito || r.Dettaglio != "Il bot Telegram non è più configurato." {
		t.Fatalf("esecuzione %+v %v", r, err)
	}
	if out := registro.String(); !strings.Contains(out, "lavoro=briefing") || strings.Contains(out, "lavoro=solleciti") ||
		!strings.Contains(out, "briefing non accodato") {
		t.Errorf("registro %q", out)
	}
}

// Voce OPS-28, in parte: niente segno «inventario:» (la voce chiude con la copia fuori sede della 128).
// gestrip serve fa partire il backup notturno (slice 124) con la chiavetta del file dell'apparecchio: la prima
// attesa è quella dopo l'avvio (10 minuti), allo stop lo ferma e lo aspetta; e gli avvisi dell'allarme al negozio
// (slice 127), la prima attesa di 30 minuti. Con -senza-lavori-automatici non partono né l'uno né gli altri, ma la
// faccia ha lo stesso «Esegui adesso». All'avvio toglie le cartelle temporanee rimaste dai backup,
// tranne con un backup in corso (il lucchetto preso da un altro).
func TestServeBackupNotturno(t *testing.T) {
	for _, spento := range []bool{false, true} {
		dir := t.TempDir()
		conf := filepath.Join(dir, "apparecchio.conf")
		if err := os.WriteFile(conf, []byte("backup_chiavetta = "+filepath.Join(dir, "chiavetta")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		dbp := filepath.Join(dir, "dati", "g.db")
		for _, c := range []string{".backup-17", ".verifica-backup-18"} {
			if err := os.MkdirAll(filepath.Join(dir, "dati", c), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		var mu sync.Mutex
		var attese []time.Duration
		partito := make(chan struct{}, 1)
		var notturno *backup.Notturno
		var backupFaccia web.BackupDelNegozio
		// Gli avvisi dell'allarme al negozio (slice 127) sono un lavoro automatico: con l'opzione non partono.
		var attesaAvvisi []time.Duration
		partitiAvvisi := make(chan struct{}, 1)
		o := opzioniServe{SenzaVolumeCifrato: true, DB: dbp, Config: conf, Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0",
			avvisi: io.Discard, SenzaLavoriAutomatici: spento,
			conDipendenze:        func(d web.Dipendenze) { backupFaccia = d.Backup },
			conBackup:            func(n *backup.Notturno) { notturno = n },
			aspettaConservazione: func(ctx context.Context) bool { <-ctx.Done(); return false },
			aspettaPulizia:       func(ctx context.Context) bool { <-ctx.Done(); return false },
			aspettaLavori:        func(ctx context.Context) bool { <-ctx.Done(); return false },
			aspettaBackup: func(ctx context.Context, d time.Duration) bool {
				mu.Lock()
				attese = append(attese, d)
				mu.Unlock()
				partito <- struct{}{}
				<-ctx.Done()
				return false
			},
			aspettaAvvisi: func(ctx context.Context, d time.Duration) bool {
				mu.Lock()
				attesaAvvisi = append(attesaAvvisi, d)
				mu.Unlock()
				partitiAvvisi <- struct{}{}
				<-ctx.Done()
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
				t.Fatal("il backup notturno non è partito")
			}
			select {
			case <-partitiAvvisi:
			case <-time.After(10 * time.Second):
				t.Fatal("gli avvisi dei backup non sono partiti")
			}
		}
		// Il banner del titolare (slice 126) segue l'opzione: con -senza-lavori-automatici tace «nessun backup», la
		// chiavetta non montata suona comunque; senza l'opzione suonano tutti e due.
		s, err := backupFaccia.Stato(context.Background())
		var codici []string
		for _, p := range s.Allarme() {
			codici = append(codici, p.Codice)
		}
		atteso := []string{backup.ProblemaChiavetta, backup.ProblemaBackupVecchio}
		if spento {
			atteso = atteso[:1]
		}
		if err != nil || s.LavoroSpento != spento || !slices.Equal(codici, atteso) {
			t.Errorf("spento=%v: lavoro spento %v, allarme %v, errore %v", spento, s.LavoroSpento, codici, err)
		}
		for _, c := range []string{".backup-17", ".verifica-backup-18"} {
			if _, err := os.Lstat(filepath.Join(dir, "dati", c)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("spento=%v: %s rimasta: %v", spento, c, err)
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
		if notturno == nil || !notturno.Configurato() || backupFaccia != web.BackupDelNegozio(notturno) {
			t.Errorf("spento=%v: backup %v, faccia %v", spento, notturno, backupFaccia)
		}
		mu.Lock()
		got := attese
		mu.Unlock()
		if spento && len(got) != 0 || !spento && (len(got) != 1 || got[0] != backup.AttesaDopoAvvio) {
			t.Errorf("spento=%v: attese %v", spento, got)
		}
		mu.Lock()
		gotAvvisi := attesaAvvisi
		mu.Unlock()
		if spento && len(gotAvvisi) != 0 || !spento && (len(gotAvvisi) != 1 || gotAvvisi[0] != backup.AttesaAvvisiDopoAvvio) {
			t.Errorf("spento=%v: attese degli avvisi %v", spento, gotAvvisi)
		}
	}
	// Con un backup in corso (il lucchetto preso qui, come un gestrip backup in un altro processo) la cartella
	// temporanea resta.
	dir := t.TempDir()
	dati := filepath.Join(dir, "dati")
	if err := os.MkdirAll(filepath.Join(dati, ".backup-19"), 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := backup.PrendiLucchetto(dati)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Rilascia() }()
	var registro bytes.Buffer
	pulisciAllAvvio(dati, slog.New(slog.NewTextHandler(&registro, nil)))
	if _, err := os.Lstat(filepath.Join(dati, ".backup-19")); err != nil {
		t.Errorf("con il backup in corso: %v", err)
	}
	if !strings.Contains(registro.String(), "in corso") {
		t.Errorf("registro: %q", registro.String())
	}
}
