package uscita

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"unicode"

	"github.com/L0cutus/gestrip/internal/db"
)

// La configurazione del canale email (slice 069, CLI-52 parte email, ADM-09), come Outgoing.Config dello
// standalone a go-baseline: le chiavi outgoing.smtp.* di settings, che l'import porta in impostazioni
// come sono, e la password in segreti_provider (D-03, migrazione 0006), mai tra le impostazioni. Si
// legge a ogni tentativo (attempt/1), così una configurazione salvata vale dal giro dopo.
//
// Solo del Go (D-03): il canale è spento finché il titolare non lo accende (ChiaveEmailAcceso). Una
// configurazione importata dallo standalone resta quindi ferma, e con lei le righe email importate
// (D-27). Chi accende è la pagina dei provider (slice dopo la 069).

// Le sicurezze del trasporto che il provider sa parlare (@security).
const (
	SicurezzaStartTLS = "starttls"
	SicurezzaTLS      = "tls"
	SicurezzaNessuna  = "none"
)

// ChiaveEmailAcceso è la chiave di impostazioni, solo del Go, dell'interruttore del canale email: «1»
// acceso, ogni altro valore o nessuna riga spento (D-03).
const ChiaveEmailAcceso = "canale_email_acceso"

// Le chiavi dello standalone (Outgoing.Config.keys(:email) con il prefisso outgoing.smtp.).
const (
	chiaveServer       = "outgoing.smtp.host"
	chiavePorta        = "outgoing.smtp.port"
	chiaveSicurezza    = "outgoing.smtp.security"
	chiaveUtente       = "outgoing.smtp.username"
	chiaveMittente     = "outgoing.smtp.from_address"
	chiaveNomeMittente = "outgoing.smtp.from_name"
	// chiavePassword sta in segreti_provider (importer.ChiaviSegreteProvider).
	chiavePassword = "outgoing.smtp.password"
	// portaPredefinita è il default: 587 dello schema Smtp.
	portaPredefinita = 587
)

// Password è la password SMTP: non si stampa mai (String, GoString, Format con ogni verbo, LogValue e
// MarshalText danno «***», come redact/1), e il valore sta dentro una funzione, così nemmeno %+v di
// una struttura che la contiene in un campo non esportato lo mostra.
type Password struct{ v func() string }

const passwordNascosta = "***"

// NuovaPassword fa una Password dal valore; vuoto è l'assenza.
func NuovaPassword(valore string) Password {
	if valore == "" {
		return Password{}
	}
	return Password{v: func() string { return valore }}
}

// Presente dice se c'è una password.
func (p Password) Presente() bool { return p.v != nil }

func (p Password) valore() string {
	if p.v == nil {
		return ""
	}
	return p.v()
}

func (Password) String() string               { return passwordNascosta }
func (Password) GoString() string             { return passwordNascosta }
func (Password) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(passwordNascosta)) }
func (Password) LogValue() slog.Value         { return slog.StringValue(passwordNascosta) }
func (Password) MarshalText() ([]byte, error) { return []byte(passwordNascosta), nil }

// ConfigSMTP è Outgoing.Config.Smtp: un valore assente è "" (setting/1 dà nil per una chiave che manca
// o di soli spazi; un valore presente resta com'è, spazi compresi).
type ConfigSMTP struct {
	Server       string // host
	Porta        int    // port, 587 se manca o non è un intero
	Sicurezza    string // security, starttls se manca
	Utente       string // username
	Password     Password
	Mittente     string // from_address
	NomeMittente string // from_name
}

// Usabile è usable?/1: server e mittente. La password no: un relay sulla stessa macchina prende la
// posta senza login (per questo c'è la sicurezza «none»).
//
// In più, solo nel Go (review della 069), fallisce chiusa: una sicurezza fuori da starttls, tls e none
// (lo standalone non la può salvare: il suo changeset la rifiuta) e un server, un mittente o un utente
// con spazi, a capo, «<» o «>» (finirebbero nei comandi SMTP: seconda serratura dopo il modulo) danno un
// canale non configurato.
func (c ConfigSMTP) Usabile() bool {
	switch c.Sicurezza {
	case SicurezzaStartTLS, SicurezzaTLS, SicurezzaNessuna:
	default:
		return false
	}
	return presente(c.Server) && presente(c.Mittente) && pulito(c.Server) && pulito(c.Mittente) && pulito(c.Utente)
}

// pulito dice se un valore non ha spazi (né a capo) né «<» o «>».
func pulito(s string) bool {
	return !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == '<' || r == '>' })
}

func presente(s string) bool { return strings.TrimSpace(s) != "" }

// LeggiSMTP è Config.get(:email).
func LeggiSMTP(ctx context.Context, q db.Interrogatore) (ConfigSMTP, error) {
	var c ConfigSMTP
	var err error
	leggi := func(dove, chiave string) string {
		if err != nil {
			return ""
		}
		var v string
		e := q.QueryRowContext(ctx, `SELECT valore FROM `+dove, chiave).Scan(&v)
		switch {
		case errors.Is(e, sql.ErrNoRows):
			return ""
		case e != nil:
			err = e
			return ""
		case !presente(v):
			return ""
		}
		return v
	}
	const impostazione = `impostazioni WHERE chiave = ?`
	c.Server = leggi(impostazione, chiaveServer)
	c.Porta = portaPredefinita
	if p := leggi(impostazione, chiavePorta); p != "" {
		if n, e := strconv.Atoi(p); e == nil {
			c.Porta = n
		}
	}
	c.Sicurezza = leggi(impostazione, chiaveSicurezza)
	if c.Sicurezza == "" {
		c.Sicurezza = SicurezzaStartTLS
	}
	c.Utente = leggi(impostazione, chiaveUtente)
	c.Password = NuovaPassword(leggi(`segreti_provider WHERE nome = ?`, chiavePassword))
	c.Mittente = leggi(impostazione, chiaveMittente)
	c.NomeMittente = leggi(impostazione, chiaveNomeMittente)
	if err != nil {
		return ConfigSMTP{}, err
	}
	return c, nil
}

// emailAccesa legge l'interruttore della D-03.
func emailAccesa(ctx context.Context, q db.Interrogatore) (bool, error) {
	var v string
	err := q.QueryRowContext(ctx, `SELECT valore FROM impostazioni WHERE chiave = ?`, ChiaveEmailAcceso).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return v == "1", err
}

// EmailAttiva dice se il canale email c'è: configurazione usabile e canale acceso. È la domanda della
// D-16 (senza SMTP i riquadri email della pagina del cliente non compaiono) e di Accoda.
func EmailAttiva(ctx context.Context, q db.Interrogatore) (bool, error) {
	c, err := LeggiSMTP(ctx, q)
	if err != nil || !c.Usabile() {
		return false, err
	}
	return emailAccesa(ctx, q)
}
