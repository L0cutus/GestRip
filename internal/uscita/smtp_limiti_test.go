package uscita

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// I limiti della conversazione SMTP (review della 069): tempo complessivo, contesto, righe di
// continuazione, lunghezza della riga, tempo di scrittura; e la seconda serratura di Usabile. Server
// grezzi su 127.0.0.1, dati inventati.

// serverGrezzo ascolta su 127.0.0.1 e passa ogni connessione a f.
func serverGrezzo(t *testing.T, f func(c net.Conn)) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				f(c)
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func configGrezza(porta int) ConfigSMTP {
	return ConfigSMTP{Server: "127.0.0.1", Porta: porta, Sicurezza: SicurezzaNessuna, Mittente: "negozio@example.invalid"}
}

// consegnaEntro fa la consegna e fallisce se non finisce entro limite.
func consegnaEntro(t *testing.T, p *SMTP, ctx context.Context, c ConfigSMTP, limite time.Duration) error {
	t.Helper()
	fatto := make(chan error, 1)
	go func() {
		_, err := p.consegnaCon(ctx, messaggioProva(), c)
		fatto <- err
	}()
	select {
	case err := <-fatto:
		return err
	case <-time.After(limite):
		t.Fatalf("la consegna non è finita entro %v", limite)
		return nil
	}
}

// inventario: CLI-43
// Un server che manda righe di continuazione senza fine, ognuna entro il tempo di una lettura: la
// conversazione finisce lo stesso alla sua scadenza complessiva, con «:timeout».
func TestSMTPScadenzaComplessiva(t *testing.T) {
	porta := serverGrezzo(t, func(c net.Conn) {
		for i := 0; ; i++ {
			if _, err := fmt.Fprintf(c, "220-riga %d\r\n", i); err != nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
	p := NuovoSMTP(nil, clock.Sistema{})
	p.tempo, p.durata = 300*time.Millisecond, 800*time.Millisecond
	err := consegnaEntro(t, p, context.Background(), configGrezza(porta), 3*time.Second)
	if got := rifiuto(t, err); got != "smtp:transport::timeout" {
		t.Errorf("motivo %q", got)
	}
	if NuovoSMTP(nil, nil).durata != 2*time.Minute {
		t.Error("la scadenza complessiva non è di 2 minuti")
	}
}

// inventario: CLI-43
// Più di 512 righe di continuazione in una risposta: errore di protocollo.
func TestSMTPTroppeContinuazioni(t *testing.T) {
	for _, n := range []int{512, 513} {
		porta := serverGrezzo(t, func(c net.Conn) {
			var b strings.Builder
			for i := range n {
				fmt.Fprintf(&b, "220-riga %d\r\n", i)
			}
			b.WriteString("220 pronto\r\n")
			_, _ = c.Write([]byte(b.String()))
			_, _ = c.Read(make([]byte, 1024)) // l'EHLO
			_, _ = c.Write([]byte("554 basta\r\n"))
			time.Sleep(time.Second)
		})
		p := NuovoSMTP(nil, clock.Sistema{})
		p.tempo = 2 * time.Second
		got := rifiuto(t, consegnaEntro(t, p, context.Background(), configGrezza(porta), 5*time.Second))
		want := map[int]string{512: "smtp:554", 513: "smtp:transport::protocol"}[n]
		if got != want {
			t.Errorf("%d continuazioni: %q, atteso %q", n, got, want)
		}
	}
}

// inventario: CLI-43
// Il contesto annullato (serve che si ferma) chiude la conversazione subito, senza aspettare il tempo
// della lettura.
func TestSMTPContestoAnnullato(t *testing.T) {
	porta := serverGrezzo(t, func(c net.Conn) {
		_, _ = c.Write([]byte("220 ciao\r\n"))
		_, _ = c.Read(make([]byte, 1024))
		time.Sleep(5 * time.Second)
	})
	p := NuovoSMTP(nil, clock.Sistema{})
	p.tempo = 3 * time.Second
	ctx, ferma := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, ferma)
	err := consegnaEntro(t, p, ctx, configGrezza(porta), time.Second)
	var r Rifiuto
	if !errors.As(err, &r) || !strings.HasPrefix(string(r), "smtp:transport:") {
		t.Errorf("errore %v", err)
	}
}

// inventario: CLI-43
// Una riga di risposta oltre i 64 KiB è un errore di protocollo, non memoria senza fine; una appena
// sotto si legge.
func TestSMTPRigaLunga(t *testing.T) {
	for _, n := range []int{lunghezzaRiga - 10, lunghezzaRiga + 10} {
		porta := serverGrezzo(t, func(c net.Conn) {
			_, _ = c.Write([]byte("554 " + strings.Repeat("x", n) + "\r\n"))
			time.Sleep(time.Second)
		})
		p := NuovoSMTP(nil, clock.Sistema{})
		p.tempo = 2 * time.Second
		got := rifiuto(t, consegnaEntro(t, p, context.Background(), configGrezza(porta), 5*time.Second))
		want := "smtp:554"
		if n > lunghezzaRiga {
			want = "smtp:transport::protocol"
		}
		if got != want {
			t.Errorf("riga di %d: %q, atteso %q", n, got, want)
		}
	}
	if lunghezzaRiga != 64*1024 {
		t.Errorf("lunghezzaRiga %d", lunghezzaRiga)
	}
}

// inventario: CLI-43
// Un server che dopo il 354 smette di leggere: la scrittura del corpo finisce col tempo massimo
// (:timeout), non resta appesa.
func TestSMTPTempoDiScrittura(t *testing.T) {
	porta := serverGrezzo(t, func(c net.Conn) {
		_, _ = c.Write([]byte("220 ciao\r\n"))
		buf := make([]byte, 4096)
		for _, r := range []string{"250 ok\r\n", "250 ok\r\n", "250 ok\r\n", "354 vai\r\n"} {
			if _, err := c.Read(buf); err != nil {
				return
			}
			_, _ = c.Write([]byte(r))
		}
		time.Sleep(10 * time.Second) // non legge più
	})
	p := NuovoSMTP(nil, clock.Sistema{})
	p.tempo = 500 * time.Millisecond
	m := messaggioProva()
	m.Testo = strings.Repeat("testo lungo di prova ", 1_000_000) // ~20 MB: riempie i buffer
	fatto := make(chan error, 1)
	go func() {
		_, err := p.consegnaCon(context.Background(), m, configGrezza(porta))
		fatto <- err
	}()
	select {
	case err := <-fatto:
		if got := rifiuto(t, err); got != "smtp:transport::timeout" {
			t.Errorf("motivo %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("la scrittura del corpo è rimasta appesa")
	}
}

// inventario: CLI-43
// Seconda serratura contro l'iniezione nei comandi: server, mittente o utente con spazi, CR, LF, «<» o
// «>», o una sicurezza fuori da starttls, tls e none, rendono la configurazione non usabile (fallisce
// chiusa: lo standalone accetta solo quei valori dal suo modulo).
func TestSMTPUsabileSecondaSerratura(t *testing.T) {
	buona := ConfigSMTP{Server: "smtp.example.invalid", Porta: 587, Sicurezza: SicurezzaStartTLS, Utente: "negozio-prova",
		Mittente: "negozio@example.invalid"}
	if !buona.Usabile() {
		t.Fatal("la configurazione buona non è usabile")
	}
	for _, sic := range []string{SicurezzaStartTLS, SicurezzaTLS, SicurezzaNessuna} {
		c := buona
		c.Sicurezza = sic
		if !c.Usabile() {
			t.Errorf("sicurezza %q non usabile", sic)
		}
	}
	for _, sic := range []string{"STARTTLS", "ssl", "starttls ", ""} {
		c := buona
		c.Sicurezza = sic
		if c.Usabile() {
			t.Errorf("sicurezza %q usabile", sic)
		}
	}
	for _, cattivo := range []string{" ", "\r", "\n", "\t", "<", ">"} {
		for i, metti := range []func(*ConfigSMTP){
			func(c *ConfigSMTP) { c.Server = "smtp" + cattivo + ".example.invalid" },
			func(c *ConfigSMTP) { c.Mittente = "negozio@example.invalid" + cattivo + "x" },
			func(c *ConfigSMTP) { c.Utente = "negozio" + cattivo + "prova" },
		} {
			c := buona
			metti(&c)
			if c.Usabile() {
				t.Errorf("campo %d con %q usabile", i, cattivo)
			}
		}
	}
	senzaUtente := buona
	senzaUtente.Utente = ""
	if !senzaUtente.Usabile() {
		t.Error("senza utente (relay) non usabile")
	}
}

// inventario: CLI-43
// La sonda della review: un mittente con «\r\n» nel database non apre un comando in più. Consegna dà
// «not_configured» e il server non riceve niente.
func TestSMTPMittenteConACapo(t *testing.T) {
	d, o := apriDB(t)
	s := avviaServerFinto(t, &serverFinto{})
	scriviConfigSMTP(t, d, map[string]string{
		"outgoing.smtp.host": "127.0.0.1", "outgoing.smtp.port": fmt.Sprint(s.porta()), "outgoing.smtp.security": "none",
		"outgoing.smtp.from_address": "negozio@example.invalid>\r\nRCPT TO:<intruso@example.com",
	}, "")
	accendiEmail(t, d)
	p := NuovoSMTP(d, o)
	p.tempo = 2 * time.Second
	_, err := p.Consegna(context.Background(), messaggioProva())
	if got := rifiuto(t, err); got != "not_configured" {
		t.Errorf("motivo %q", got)
	}
	if s.haRicevuto("") {
		t.Errorf("il server ha ricevuto %q", s.ricevuti())
	}
	if a, err := EmailAttiva(context.Background(), d.Lettura); err != nil || a {
		t.Errorf("EmailAttiva %v %v", a, err)
	}
}

// inventario: CLI-51
// Un canale che si spegne durante il giro: la riga non va al provider, non si scrive niente e non si
// consuma un tentativo (resta «queued» com'era).
func TestGiroCanaleSpentoDuranteIlGiro(t *testing.T) {
	d, o := apriDB(t)
	ctx := context.Background()
	p := &providerAMano{risposte: []bool{true, true, false}}
	c := Nuova(d, o, map[string]Provider{CanaleEmail: p})
	var ids []int64
	for range 2 {
		res, err := d.Scrittura.Exec(`INSERT INTO messaggi_in_uscita (canale, destinatario, indirizzo, testo, stato, tentativi,
			prossimo_tentativo_il, accodato_il) VALUES ('email', 'customer', 'cliente@example.com', 'testo', 'queued', 1, ?1, ?1)`,
			"2026-01-01T09:00:00.000000Z")
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		ids = append(ids, id)
	}
	e, err := c.Giro(ctx, PerGiro)
	if err != nil || e != (Esito{Accettati: 1}) {
		t.Errorf("giro %+v %v", e, err)
	}
	if p.quante() != 1 {
		t.Errorf("chiamate %d", p.quante())
	}
	if got := riga(t, d, ids[1]); got != "queued|1|2026-01-01T09:00:00.000000Z|-|-|-|-" {
		t.Errorf("riga spenta %s", got)
	}
	// Un errore nel chiedere per la riga: il giro si ferma, la riga resta com'era.
	p.risposte, p.chiamate, p.guastoDopo, p.guasto = []bool{true}, 0, 1, errors.New("guasto")
	if _, err := c.Giro(ctx, PerGiro); err == nil {
		t.Error("nessun errore")
	}
	if got := riga(t, d, ids[1]); got != "queued|1|2026-01-01T09:00:00.000000Z|-|-|-|-" || p.quante() != 1 {
		t.Errorf("dopo l'errore %s, chiamate %d", got, p.quante())
	}
}

// providerAMano risponde ad Attivo con le risposte date, una per chiamata (poi spento).
type providerAMano struct {
	providerFinto
	risposte   []bool
	guasto     error
	guastoDopo int
	chiamate   int
}

func (p *providerAMano) Attivo(context.Context) (bool, error) {
	p.chiamate++
	if p.guasto != nil && p.chiamate > p.guastoDopo {
		return false, p.guasto
	}
	if len(p.risposte) == 0 {
		return false, nil
	}
	a := p.risposte[0]
	p.risposte = p.risposte[1:]
	return a, nil
}

// inventario: CLI-43
// Un server che dice «220» a STARTTLS (o accetta la connessione TLS diretta) e poi non fa la stretta di
// mano del TLS: la consegna finisce col tempo massimo, non resta appesa.
func TestSMTPTempoDelTLS(t *testing.T) {
	for _, sic := range []string{SicurezzaStartTLS, SicurezzaTLS} {
		porta := serverGrezzo(t, func(c net.Conn) {
			buf := make([]byte, 1024)
			if sic == SicurezzaStartTLS {
				_, _ = c.Write([]byte("220 ciao\r\n"))
				_, _ = c.Read(buf)
				_, _ = c.Write([]byte("250 STARTTLS\r\n"))
				_, _ = c.Read(buf)
				_, _ = c.Write([]byte("220 vai\r\n"))
			}
			time.Sleep(10 * time.Second) // nessuna stretta di mano
		})
		p := NuovoSMTP(nil, clock.Sistema{})
		p.tempo = 300 * time.Millisecond
		c := configGrezza(porta)
		c.Sicurezza = sic
		got := rifiuto(t, consegnaEntro(t, p, context.Background(), c, 3*time.Second))
		if got != "smtp:transport::timeout" {
			t.Errorf("%s: motivo %q", sic, got)
		}
	}
}
