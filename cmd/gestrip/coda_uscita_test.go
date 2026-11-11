package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
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
// giro, poi si aspetta 30 secondi (:dispatch_interval_ms). Con i soli provider di serve (SMTP, slice
// 069) e l'email non configurata né accesa (D-03) le righe importate restano in coda, senza tentativi.
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
	}
}

// relayFinto è un relay SMTP finto su 127.0.0.1 (sicurezza «none», senza credenziali): accetta un
// messaggio e manda il suo DATA sul canale. Nessun server vero.
func relayFinto(t *testing.T) (porta int, dati <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	ch := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		_ = c.SetDeadline(time.Now().Add(20 * time.Second))
		r := bufio.NewReader(c)
		scrivi := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
		scrivi("220 relay.example.invalid ESMTP")
		for {
			riga, err := r.ReadString('\n')
			if err != nil {
				return
			}
			switch v := strings.ToUpper(strings.TrimSpace(riga)); {
			case strings.HasPrefix(v, "EHLO"), strings.HasPrefix(v, "MAIL"), strings.HasPrefix(v, "RCPT"):
				scrivi("250 Ok")
			case v == "DATA":
				scrivi("354 Go ahead")
				var b strings.Builder
				for !strings.HasSuffix(b.String(), "\r\n.\r\n") {
					pezzo, err := r.ReadString('\n')
					b.WriteString(pezzo)
					if err != nil {
						return
					}
				}
				ch <- b.String()
				scrivi("250 2.0.0 Ok: queued as SERVE1")
			case v == "QUIT":
				scrivi("221 Bye")
				return
			default:
				scrivi("502 no")
			}
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, ch
}

// inventario: CLI-43, CLI-51
// gestrip serve con l'email configurata nel database e accesa: la coda parte col provider SMTP e la
// riga email rimasta in coda va al server del negozio (qui un relay finto); le righe Telegram restano
// in coda, senza tentativi (nessun provider Telegram prima della T4.7).
func TestServeEmailSMTP(t *testing.T) {
	vera := aspettaPer
	t.Cleanup(func() { aspettaPer = vera })
	aspettaPer = func(ctx context.Context, _ time.Duration) bool {
		<-ctx.Done()
		return false
	}
	porta, dati := relayFinto(t)
	percorso := filepath.Join(t.TempDir(), "g.db")
	d, err := db.Apri(percorso)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Migra(context.Background(), clock.Sistema{}); err != nil {
		t.Fatal(err)
	}
	ora := "2026-01-01T09:00:00.000000Z"
	for k, v := range map[string]string{
		"outgoing.smtp.host": "127.0.0.1", "outgoing.smtp.port": fmt.Sprint(porta), "outgoing.smtp.security": "none",
		"outgoing.smtp.from_address": "negozio@example.invalid", uscita.ChiaveEmailAcceso: "1",
	} {
		if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, ?)`, k, v, ora); err != nil {
			t.Fatal(err)
		}
	}
	ids := map[string]int64{}
	for _, canale := range []string{"email", "telegram"} {
		res, err := d.Scrittura.Exec(`INSERT INTO messaggi_in_uscita (canale, destinatario, indirizzo, testo, stato, tentativi,
			prossimo_tentativo_il, accodato_il) VALUES (?1, 'customer', 'cliente@example.com', 'testo di prova', 'queued', 0, ?2, ?2)`, canale, ora)
		if err != nil {
			t.Fatal(err)
		}
		ids[canale], _ = res.LastInsertId()
	}
	if err := d.Chiudi(); err != nil {
		t.Fatal(err)
	}
	o := opzioniServe{SenzaVolumeCifrato: true, DB: percorso, Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0", avvisi: io.Discard}
	ctx, ferma := context.WithCancel(context.Background())
	finito := make(chan error, 1)
	go func() { finito <- serve(ctx, o, nil) }()
	select {
	case got := <-dati:
		if !strings.Contains(got, "To: cliente@example.com\r\n") {
			t.Errorf("DATA %q", got)
		}
	case err := <-finito:
		t.Fatal(err)
	case <-time.After(15 * time.Second):
		t.Fatal("l'email rimasta in coda non è arrivata al relay")
	}
	// L'esito si scrive subito dopo il 250: si aspetta che la riga sia chiusa, poi si ferma serve.
	var stati map[string]string
	for range 100 {
		stati = statiRighe(t, percorso, ids)
		if stati["email"] != "queued|0" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	ferma()
	if err := <-finito; err != nil {
		t.Fatal(err)
	}
	stati = statiRighe(t, percorso, ids)
	if stati["email"] != "accepted|1" || stati["telegram"] != "queued|0" {
		t.Errorf("stati %v", stati)
	}
}

func statiRighe(t *testing.T, percorso string, ids map[string]int64) map[string]string {
	t.Helper()
	d, err := db.Apri(percorso)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Chiudi() }()
	out := map[string]string{}
	for k, id := range ids {
		var s string
		if err := d.Lettura.QueryRow(`SELECT stato || '|' || tentativi FROM messaggi_in_uscita WHERE id = ?`, id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		out[k] = s
	}
	return out
}
