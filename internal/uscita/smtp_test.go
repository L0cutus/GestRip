package uscita

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// Il provider SMTP (slice 069, CLI-43) come Providers.Smtp e Providers.Smtp.Tcp dello standalone a
// go-baseline (smtp_test.exs): una conversazione per riga della coda, contro il server finto di
// smtp_finto_test.go. Dati inventati.

const parolaDiProva = "PAROLA-FINTA-di-prova"

func configProva(s *serverFinto, sicurezza string) ConfigSMTP {
	return ConfigSMTP{
		Server:       "127.0.0.1",
		Porta:        s.porta(),
		Sicurezza:    sicurezza,
		Utente:       "negozio-prova",
		Password:     NuovaPassword(parolaDiProva),
		Mittente:     "negozio@example.invalid",
		NomeMittente: "Negozio di Prova",
	}
}

func messaggioProva() Messaggio {
	oggetto := "Prova invio"
	return Messaggio{ID: 7, Canale: CanaleEmail, Destinatario: "customer", Indirizzo: "cliente@example.com",
		Oggetto: &oggetto, Testo: "Riga uno.\nRiga due."}
}

// smtpProva è il provider puntato al server finto: le radici del suo certificato e un tempo breve.
func smtpProva(s *serverFinto) *SMTP {
	p := NuovoSMTP(nil, clock.NuovoFinto(time.Date(2026, 11, 11, 9, 5, 7, 0, time.UTC)))
	p.radici = s.Radici
	p.tempo = 3 * time.Second
	return p
}

// rifiuto dice il motivo di un Rifiuto, o fallisce se l'errore non lo è.
func rifiuto(t *testing.T, err error) string {
	t.Helper()
	var r Rifiuto
	if !errors.As(err, &r) {
		t.Fatalf("errore %v (%T), atteso un Rifiuto", err, err)
	}
	return string(r)
}

// inventario: CLI-43
// La conversazione dello standalone: EHLO col dominio del mittente, STARTTLS, di nuovo EHLO, AUTH LOGIN
// con utente e password in base64 solo dentro il TLS, MAIL, RCPT, DATA, QUIT; il riferimento è la riga
// del 250 dopo il corpo.
func TestSMTPConversazioneStartTLS(t *testing.T) {
	s := avviaServerFinto(t, &serverFinto{Utente: "negozio-prova", Password: parolaDiProva})
	rif, err := smtpProva(s).consegnaCon(context.Background(), messaggioProva(), configProva(s, SicurezzaStartTLS))
	if err != nil {
		t.Fatal(err)
	}
	if rif != "250 2.0.0 Ok: queued as ABC123" {
		t.Errorf("riferimento %q", rif)
	}
	s.fine(t)
	got := s.ricevuti()
	want := []string{
		"EHLO example.invalid", "STARTTLS", "EHLO example.invalid", "AUTH LOGIN",
		base64.StdEncoding.EncodeToString([]byte("negozio-prova")), base64.StdEncoding.EncodeToString([]byte(parolaDiProva)),
		"MAIL FROM:<negozio@example.invalid>", "RCPT TO:<cliente@example.com>", "DATA", "QUIT",
	}
	if !slices.Equal(got, want) {
		t.Errorf("comandi\n%q\natteso\n%q", got, want)
	}
	if !slices.Equal(s.authInTLS, []bool{true}) {
		t.Errorf("AUTH nel TLS: %v", s.authInTLS)
	}
}

// inventario: CLI-43
// Saluto ed EHLO su più righe, arrivate una per volta: la risposta finisce alla prima riga senza «-».
func TestSMTPRispostaSuPiuRighe(t *testing.T) {
	s := avviaServerFinto(t, &serverFinto{Utente: "negozio-prova", Password: parolaDiProva, Spezzato: true})
	rif, err := smtpProva(s).consegnaCon(context.Background(), messaggioProva(), configProva(s, SicurezzaStartTLS))
	if err != nil || rif != "250 2.0.0 Ok: queued as ABC123" {
		t.Fatalf("%q %v", rif, err)
	}
	s.fine(t)
	if !slices.Equal(s.authInTLS, []bool{true}) {
		t.Errorf("AUTH nel TLS: %v", s.authInTLS)
	}
}

// inventario: CLI-43
// Il messaggio: From col nome, To, Subject, Date dall'orologio, MIME-Version, testo utf-8 in base64 a
// righe di 76; nessun Message-ID (lo standalone non lo mette); un punto da solo nel testo non chiude
// il DATA.
func TestSMTPIntestazioniECorpo(t *testing.T) {
	s := avviaServerFinto(t, &serverFinto{Utente: "negozio-prova", Password: parolaDiProva})
	m := messaggioProva()
	m.Testo = "\n.\nUn punto solo\n" + strings.Repeat("riga lunga ", 30)
	if _, err := smtpProva(s).consegnaCon(context.Background(), m, configProva(s, SicurezzaStartTLS)); err != nil {
		t.Fatal(err)
	}
	s.fine(t)
	dati := s.corpo()
	testa, corpo, ok := strings.Cut(dati, "\r\n\r\n")
	if !ok {
		t.Fatalf("niente riga vuota fra intestazioni e corpo: %q", dati)
	}
	wantTesta := strings.Join([]string{
		"From: Negozio di Prova <negozio@example.invalid>",
		"To: cliente@example.com",
		"Subject: Prova invio",
		"Date: Wed, 11 Nov 2026 09:05:07 +0000",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: base64",
	}, "\r\n")
	if testa != wantTesta {
		t.Errorf("intestazioni\n%q\natteso\n%q", testa, wantTesta)
	}
	if !strings.HasSuffix(corpo, "\r\n.\r\n") {
		t.Fatalf("corpo senza terminatore: %q", corpo)
	}
	righe := strings.Split(strings.TrimSuffix(corpo, "\r\n.\r\n"), "\r\n")
	for i, r := range righe {
		if len(r) > 76 || (i < len(righe)-1 && len(r) != 76) {
			t.Errorf("riga %d lunga %d", i, len(r))
		}
	}
	testo, err := base64.StdEncoding.DecodeString(strings.Join(righe, ""))
	if err != nil || string(testo) != m.Testo {
		t.Errorf("testo %q (%v)", testo, err)
	}
}

// inventario: CLI-43
// Il nome del mittente che non è semplice e l'oggetto non ASCII viaggiano come parole codificate (RFC
// 2047); un nome vuoto lascia il solo indirizzo, un oggetto assente un Subject vuoto.
func TestSMTPParoleCodificate(t *testing.T) {
	casi := []struct {
		nome, oggetto      string
		senzaOggetto       bool
		wantFrom, wantSubj string
	}{
		{"Negozio, Òtto <capo>", "Riparazione pronta però", false,
			"From: =?utf-8?B?" + base64.StdEncoding.EncodeToString([]byte("Negozio, Òtto <capo>")) + "?= <negozio@example.invalid>",
			"Subject: =?utf-8?B?" + base64.StdEncoding.EncodeToString([]byte("Riparazione pronta però")) + "?="},
		{"  ", "", true, "From: negozio@example.invalid", "Subject: "},
		{" Negozio d'Arte - 2 ", "Pronta", false, "From: Negozio d'Arte - 2 <negozio@example.invalid>", "Subject: Pronta"},
		// Lo standalone: un nome non «semplice» ma tutto ASCII stampabile passa com'è (encode_word/1).
		{"Negozio, Otto", "Pronta", false, "From: Negozio, Otto <negozio@example.invalid>", "Subject: Pronta"},
		{"Nome\r\nBcc: altro@example.com", "Riga\r\nBcc: altro@example.com", false,
			"From: =?utf-8?B?" + base64.StdEncoding.EncodeToString([]byte("Nome\r\nBcc: altro@example.com")) + "?= <negozio@example.invalid>",
			"Subject: =?utf-8?B?" + base64.StdEncoding.EncodeToString([]byte("Riga\r\nBcc: altro@example.com")) + "?="},
	}
	for _, c := range casi {
		s := avviaServerFinto(t, &serverFinto{Utente: "negozio-prova", Password: parolaDiProva})
		cfg := configProva(s, SicurezzaStartTLS)
		cfg.NomeMittente = c.nome
		m := messaggioProva()
		m.Oggetto = &c.oggetto
		if c.senzaOggetto {
			m.Oggetto = nil
		}
		if _, err := smtpProva(s).consegnaCon(context.Background(), m, cfg); err != nil {
			t.Fatal(err)
		}
		s.fine(t)
		dati := s.corpo()
		if !strings.Contains(dati, c.wantFrom+"\r\n") || !strings.Contains(dati, "\r\n"+c.wantSubj+"\r\n") {
			t.Errorf("nome %q, oggetto %q:\n%q", c.nome, c.oggetto, dati)
		}
		if strings.Contains(dati, "\r\nBcc:") {
			t.Errorf("intestazione iniettata: %q", dati)
		}
	}
}

// inventario: CLI-43
// Credenziali rifiutate: «smtp:535» e basta. Il server ripete utente e password nel suo testo, ma
// nell'errore non c'è niente di loro né dell'indirizzo; la connessione si chiude.
func TestSMTPCredenzialiSbagliate(t *testing.T) {
	s := avviaServerFinto(t, &serverFinto{Utente: "negozio-prova", Password: "altra-parola-finta"})
	_, err := smtpProva(s).consegnaCon(context.Background(), messaggioProva(), configProva(s, SicurezzaStartTLS))
	motivo := rifiuto(t, err)
	if motivo != "smtp:535" {
		t.Errorf("motivo %q", motivo)
	}
	for _, vietato := range []string{parolaDiProva, base64.StdEncoding.EncodeToString([]byte(parolaDiProva)), "negozio-prova", "cliente@example.com"} {
		if strings.Contains(err.Error(), vietato) {
			t.Errorf("l'errore contiene %q", vietato)
		}
	}
	s.fine(t)
	if s.haRicevuto("MAIL") {
		t.Error("MAIL dopo un AUTH rifiutato")
	}
}

// inventario: CLI-43, CLI-51
// Ogni rifiuto del server è «smtp:<codice>», mai un successo e mai il testo del server (che può
// ripetere l'indirizzo): il destinatario rifiutato, il mittente, il DATA, il corpo. Lo standalone
// non separa i 4xx dai 5xx: la coda li riprova tutti allo stesso modo.
func TestSMTPRifiutiDelServer(t *testing.T) {
	casi := []struct {
		srv  *serverFinto
		want string
	}{
		{&serverFinto{Risposte: map[string]string{"RCPT": "550 5.1.1 <cliente@example.com>: No such user"}}, "smtp:550"},
		{&serverFinto{Risposte: map[string]string{"RCPT": "450 4.2.0 <cliente@example.com>: greylisted"}}, "smtp:450"},
		{&serverFinto{Risposte: map[string]string{"MAIL": "553 5.7.1 <negozio@example.invalid>: Sender rejected"}}, "smtp:553"},
		{&serverFinto{Risposte: map[string]string{"DATA": "554 5.5.1 Error: no valid recipients"}}, "smtp:554"},
		{&serverFinto{Accettato: "552 5.3.4 Message size exceeds fixed limit"}, "smtp:552"},
		{&serverFinto{Risposte: map[string]string{"saluto": "421 4.3.2 Service not available"}}, "smtp:421"},
		{&serverFinto{Risposte: map[string]string{"EHLO": "501 5.5.4 Syntax error"}}, "smtp:501"},
	}
	for _, c := range casi {
		c.srv.Utente, c.srv.Password = "negozio-prova", parolaDiProva
		s := avviaServerFinto(t, c.srv)
		_, err := smtpProva(s).consegnaCon(context.Background(), messaggioProva(), configProva(s, SicurezzaStartTLS))
		if got := rifiuto(t, err); got != c.want {
			t.Errorf("motivo %q, atteso %q", got, c.want)
		}
		s.fine(t)
	}
}

// inventario: CLI-43
// Un server che rifiuta STARTTLS: si ferma lì, mai in chiaro, niente AUTH né MAIL.
func TestSMTPStartTLSRifiutatoMaiInChiaro(t *testing.T) {
	s := avviaServerFinto(t, &serverFinto{SenzaStartTLS: true, Utente: "negozio-prova", Password: parolaDiProva})
	_, err := smtpProva(s).consegnaCon(context.Background(), messaggioProva(), configProva(s, SicurezzaStartTLS))
	if got := rifiuto(t, err); got != "smtp:454" {
		t.Errorf("motivo %q", got)
	}
	s.fine(t)
	if s.haRicevuto("AUTH") || s.haRicevuto("MAIL") {
		t.Errorf("dopo il 454: %q", s.ricevuti())
	}
}

// inventario: CLI-43
// Un certificato di cui il provider non si fida (le radici del sistema, non quelle del test): errore
// di trasporto, nessuna credenziale, nessun ripiego in chiaro.
func TestSMTPCertificatoNonFidato(t *testing.T) {
	for _, sic := range []string{SicurezzaStartTLS, SicurezzaTLS} {
		s := avviaServerFinto(t, &serverFinto{TLSImplicito: sic == SicurezzaTLS, Utente: "negozio-prova", Password: parolaDiProva})
		p := smtpProva(s)
		p.radici = nil // le radici del sistema: il certificato del test non c'è
		_, err := p.consegnaCon(context.Background(), messaggioProva(), configProva(s, sic))
		if got := rifiuto(t, err); got != "smtp:transport::tls" {
			t.Errorf("%s: motivo %q", sic, got)
		}
		s.fine(t)
		if s.haRicevuto("AUTH") || s.haRicevuto("MAIL") || s.haRicevuto(base64.StdEncoding.EncodeToString([]byte(parolaDiProva))) {
			t.Errorf("%s: %q", sic, s.ricevuti())
		}
	}
}

// inventario: CLI-43
// Una password su una sessione che non è mai stata cifrata (sicurezza «none»): «smtp:auth_requires_tls»,
// nessun AUTH e nessuna password sul filo.
func TestSMTPCredenzialiMaiSuSessioneInChiaro(t *testing.T) {
	s := avviaServerFinto(t, &serverFinto{Utente: "negozio-prova", Password: parolaDiProva})
	_, err := smtpProva(s).consegnaCon(context.Background(), messaggioProva(), configProva(s, SicurezzaNessuna))
	if got := rifiuto(t, err); got != "smtp:auth_requires_tls" {
		t.Errorf("motivo %q", got)
	}
	s.fine(t)
	if s.haRicevuto("AUTH") || s.haRicevuto("STARTTLS") || s.haRicevuto(base64.StdEncoding.EncodeToString([]byte(parolaDiProva))) {
		t.Errorf("comandi %q", s.ricevuti())
	}
}

// inventario: CLI-43
// Il relay anonimo (sicurezza «none», senza credenziali): né STARTTLS né AUTH.
func TestSMTPRelayAnonimo(t *testing.T) {
	s := avviaServerFinto(t, &serverFinto{})
	cfg := configProva(s, SicurezzaNessuna)
	cfg.Utente, cfg.Password = "", Password{}
	rif, err := smtpProva(s).consegnaCon(context.Background(), messaggioProva(), cfg)
	if err != nil || !strings.Contains(rif, "ABC123") {
		t.Fatalf("%q %v", rif, err)
	}
	s.fine(t)
	want := []string{"EHLO example.invalid", "MAIL FROM:<negozio@example.invalid>", "RCPT TO:<cliente@example.com>", "DATA", "QUIT"}
	if got := s.ricevuti(); !slices.Equal(got, want) {
		t.Errorf("comandi %q", got)
	}
}

// inventario: CLI-43
// Il TLS implicito (porta 465): cifrato dal primo byte, niente STARTTLS, AUTH dentro il TLS.
func TestSMTPTLSImplicito(t *testing.T) {
	s := avviaServerFinto(t, &serverFinto{TLSImplicito: true, Utente: "negozio-prova", Password: parolaDiProva})
	if _, err := smtpProva(s).consegnaCon(context.Background(), messaggioProva(), configProva(s, SicurezzaTLS)); err != nil {
		t.Fatal(err)
	}
	s.fine(t)
	if s.haRicevuto("STARTTLS") || !slices.Equal(s.authInTLS, []bool{true}) {
		t.Errorf("comandi %q, auth %v", s.ricevuti(), s.authInTLS)
	}
}

// inventario: CLI-43
// Il TLS con un utente e senza password, o con la password e senza utente, va senza AUTH come lo
// standalone (authenticate/3 vuole tutti e due).
func TestSMTPCredenzialiAMeta(t *testing.T) {
	for _, soloUtente := range []bool{true, false} {
		s := avviaServerFinto(t, &serverFinto{})
		cfg := configProva(s, SicurezzaStartTLS)
		if soloUtente {
			cfg.Password = Password{}
		} else {
			cfg.Utente = ""
		}
		if _, err := smtpProva(s).consegnaCon(context.Background(), messaggioProva(), cfg); err != nil {
			t.Fatal(err)
		}
		s.fine(t)
		if s.haRicevuto("AUTH") {
			t.Errorf("solo utente %v: AUTH", soloUtente)
		}
	}
}

// inventario: CLI-43
// Un saluto che non arriva è un errore di trasporto dopo il tempo massimo, non un invio.
func TestSMTPTempoScaduto(t *testing.T) {
	s := avviaServerFinto(t, &serverFinto{Muto: true})
	p := smtpProva(s)
	p.tempo = 300 * time.Millisecond
	inizio := time.Now()
	_, err := p.consegnaCon(context.Background(), messaggioProva(), configProva(s, SicurezzaStartTLS))
	if got := rifiuto(t, err); got != "smtp:transport::timeout" {
		t.Errorf("motivo %q", got)
	}
	if d := time.Since(inizio); d > 3*time.Second {
		t.Errorf("durato %v", d)
	}
}

// inventario: CLI-43
// Il tempo massimo dello standalone è 15 secondi (@timeout).
func TestSMTPTempoPredefinito(t *testing.T) {
	if p := NuovoSMTP(nil, clock.Sistema{}); p.tempo != 15*time.Second {
		t.Errorf("tempo %v", p.tempo)
	}
}

// inventario: CLI-43
// Una porta chiusa e una risposta che non è SMTP: errori di trasporto senza dati.
func TestSMTPErroriDiTrasporto(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	porta := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	p := NuovoSMTP(nil, clock.Sistema{})
	p.tempo = 2 * time.Second
	cfg := ConfigSMTP{Server: "127.0.0.1", Porta: porta, Sicurezza: SicurezzaNessuna, Mittente: "negozio@example.invalid"}
	_, err = p.consegnaCon(context.Background(), messaggioProva(), cfg)
	if got := rifiuto(t, err); got != "smtp:transport::econnrefused" {
		t.Errorf("porta chiusa: %q", got)
	}

	s := avviaServerFinto(t, &serverFinto{Risposte: map[string]string{"saluto": "ciao, non sono un server SMTP"}})
	_, err = smtpProva(s).consegnaCon(context.Background(), messaggioProva(), configProva(s, SicurezzaNessuna))
	if got := rifiuto(t, err); got != "smtp:transport::protocol" {
		t.Errorf("risposta non SMTP: %q", got)
	}
}

// inventario: CLI-43
// Consegna legge la configurazione dal database a ogni tentativo (attempt/1): senza configurazione
// usabile, o con il canale spento (D-03), «not_configured» senza aprire niente; con la configurazione
// e il canale acceso parla col server, e la password che il server ripete nel riferimento diventa «***».
func TestSMTPConsegnaDalDatabase(t *testing.T) {
	d, o := apriDB(t)
	s := avviaServerFinto(t, &serverFinto{Utente: "negozio-prova", Password: parolaDiProva,
		Accettato: "250 2.0.0 Ok: queued as XYZ " + parolaDiProva})
	p := NuovoSMTP(d, o)
	p.radici, p.tempo = s.Radici, 3*time.Second
	ctx := context.Background()

	_, err := p.Consegna(ctx, messaggioProva())
	if got := rifiuto(t, err); got != "not_configured" {
		t.Errorf("senza configurazione: %q", got)
	}
	scriviConfigSMTP(t, d, map[string]string{
		"outgoing.smtp.host": "127.0.0.1", "outgoing.smtp.port": fmt.Sprint(s.porta()), "outgoing.smtp.security": "starttls",
		"outgoing.smtp.username": "negozio-prova", "outgoing.smtp.from_address": "negozio@example.invalid",
		"outgoing.smtp.from_name": "Negozio di Prova",
	}, parolaDiProva)
	_, err = p.Consegna(ctx, messaggioProva())
	if got := rifiuto(t, err); got != "not_configured" {
		t.Errorf("configurata ma spenta (D-03): %q", got)
	}
	if s.haRicevuto("EHLO") {
		t.Error("spenta, ma ha parlato col server")
	}
	accendiEmail(t, d)
	rif, err := p.Consegna(ctx, messaggioProva())
	if err != nil {
		t.Fatal(err)
	}
	if rif != "250 2.0.0 Ok: queued as XYZ ***" {
		t.Errorf("riferimento %q", rif)
	}
	s.fine(t)
	if !s.haRicevuto(base64.StdEncoding.EncodeToString([]byte(parolaDiProva))) {
		t.Error("la password del database non è arrivata al server")
	}
}

// inventario: CLI-43
// Anche il motivo passa dalla pulizia della password (scrub/2 su last_error), come il riferimento: una
// password che compare nel motivo, qui per caso, diventa «***».
func TestSMTPMotivoSenzaPassword(t *testing.T) {
	d, o := apriDB(t)
	s := avviaServerFinto(t, &serverFinto{})
	scriviConfigSMTP(t, d, map[string]string{
		"outgoing.smtp.host": "127.0.0.1", "outgoing.smtp.port": fmt.Sprint(s.porta()), "outgoing.smtp.security": "none",
		"outgoing.smtp.from_address": "negozio@example.invalid",
	}, "requires")
	accendiEmail(t, d)
	p := NuovoSMTP(d, o)
	p.tempo = 3 * time.Second
	_, err := p.Consegna(context.Background(), messaggioProva())
	if got := rifiuto(t, err); got != "smtp:auth_***_tls" {
		t.Errorf("motivo %q", got)
	}
}
