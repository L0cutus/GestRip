package uscita

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/db"
)

// La configurazione del canale email (slice 069, CLI-52 parte email, ADM-09, D-03, D-16): come
// Outgoing.Config.get(:email), usable?/1 e secrets/1 dello standalone, sulle chiavi outgoing.smtp.* che
// l'import porta in impostazioni e la password in segreti_provider. Dati inventati.

func scriviConfigSMTP(t *testing.T, d *db.DB, valori map[string]string, password string) {
	t.Helper()
	for k, v := range valori {
		if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, '2026-11-11T09:00:00.000000Z')
			ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore`, k, v); err != nil {
			t.Fatal(err)
		}
	}
	if password != "" {
		if _, err := d.Scrittura.Exec(`INSERT INTO segreti_provider (nome, valore, aggiornato_il) VALUES ('outgoing.smtp.password', ?, '2026-11-11T09:00:00.000000Z')
			ON CONFLICT (nome) DO UPDATE SET valore = excluded.valore`, password); err != nil {
			t.Fatal(err)
		}
	}
}

func accendiEmail(t *testing.T, d *db.DB) {
	t.Helper()
	if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, '1', '2026-11-11T09:00:00.000000Z')
		ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore`, ChiaveEmailAcceso); err != nil {
		t.Fatal(err)
	}
}

// inventario: CLI-43
// Niente configurato: i predefiniti dello standalone (porta 587, starttls) e il canale non usabile.
// Un valore di soli spazi è assente, una porta che non è un intero torna a 587 (integer_setting/2), la
// password viene da segreti_provider e un valore non vuoto resta com'è.
func TestLeggiSMTP(t *testing.T) {
	d, _ := apriDB(t)
	ctx := context.Background()
	c, err := LeggiSMTP(ctx, d.Lettura)
	if err != nil {
		t.Fatal(err)
	}
	if c.Server != "" || c.Porta != 587 || c.Sicurezza != SicurezzaStartTLS || c.Utente != "" || c.Password.Presente() ||
		c.Mittente != "" || c.NomeMittente != "" || c.Usabile() {
		t.Errorf("vuota: %+v usabile %v", c, c.Usabile())
	}
	scriviConfigSMTP(t, d, map[string]string{
		"outgoing.smtp.host": "smtp.example.invalid", "outgoing.smtp.port": "porta", "outgoing.smtp.security": "   ",
		"outgoing.smtp.username": " ", "outgoing.smtp.from_address": "negozio@example.invalid", "outgoing.smtp.from_name": " Negozio ",
	}, parolaDiProva)
	c, err = LeggiSMTP(ctx, d.Lettura)
	if err != nil {
		t.Fatal(err)
	}
	if c.Server != "smtp.example.invalid" || c.Porta != 587 || c.Sicurezza != SicurezzaStartTLS || c.Utente != "" ||
		c.Password.valore() != parolaDiProva || c.Mittente != "negozio@example.invalid" || c.NomeMittente != " Negozio " || !c.Usabile() {
		t.Errorf("letta: %+v", c)
	}
	scriviConfigSMTP(t, d, map[string]string{"outgoing.smtp.port": "465", "outgoing.smtp.security": "tls", "outgoing.smtp.username": "u"}, "")
	if c, _ = LeggiSMTP(ctx, d.Lettura); c.Porta != 465 || c.Sicurezza != SicurezzaTLS || c.Utente != "u" {
		t.Errorf("porta e sicurezza: %+v", c)
	}
	// Server e mittente servono tutti e due; la password no (il relay locale).
	for _, k := range []string{"outgoing.smtp.host", "outgoing.smtp.from_address"} {
		scriviConfigSMTP(t, d, map[string]string{k: ""}, "")
		if c, _ = LeggiSMTP(ctx, d.Lettura); c.Usabile() {
			t.Errorf("senza %s usabile", k)
		}
		scriviConfigSMTP(t, d, map[string]string{"outgoing.smtp.host": "smtp.example.invalid", "outgoing.smtp.from_address": "negozio@example.invalid"}, "")
	}
	if _, err := d.Scrittura.Exec(`DELETE FROM segreti_provider`); err != nil {
		t.Fatal(err)
	}
	if c, _ = LeggiSMTP(ctx, d.Lettura); !c.Usabile() || c.Password.Presente() {
		t.Errorf("senza password: %+v", c)
	}
}

// inventario: CLI-43
// La password non si stampa mai: né con fmt (ogni verbo, anche dentro una struttura), né nel registro,
// né in JSON.
func TestPasswordNonSiStampa(t *testing.T) {
	c := ConfigSMTP{Server: "smtp.example.invalid", Password: NuovaPassword(parolaDiProva)}
	contenitore := struct {
		C ConfigSMTP
		c ConfigSMTP
	}{c, c}
	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("prova", "config", c, "password", c.Password)
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("prova", "config", c, "password", c.Password)
	j, err := json.Marshal(contenitore)
	if err != nil {
		t.Fatal(err)
	}
	uscite := []string{
		fmt.Sprint(c), fmt.Sprintf("%v %+v %#v %s %q %x", c, c, c, c.Password, c.Password, c.Password),
		fmt.Sprintf("%v %+v %#v", contenitore, contenitore, contenitore), c.Password.String(), buf.String(), string(j),
	}
	for _, u := range uscite {
		if strings.Contains(u, parolaDiProva) || strings.Contains(u, fmt.Sprintf("%x", parolaDiProva)) {
			t.Errorf("la password si legge in %q", u)
		}
	}
	if !NuovaPassword(parolaDiProva).Presente() || NuovaPassword("").Presente() || (Password{}).Presente() {
		t.Error("Presente")
	}
}

// inventario: CLI-43
// D-16 e D-03: l'email è attiva solo con la configurazione usabile e il canale acceso. Una
// configurazione importata (o scritta senza accendere) resta spenta.
func TestEmailAttiva(t *testing.T) {
	d, _ := apriDB(t)
	ctx := context.Background()
	attiva := func() bool {
		t.Helper()
		a, err := EmailAttiva(ctx, d.Lettura)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	if attiva() {
		t.Error("vuota: attiva")
	}
	accendiEmail(t, d)
	if attiva() {
		t.Error("accesa senza configurazione: attiva")
	}
	if _, err := d.Scrittura.Exec(`DELETE FROM impostazioni`); err != nil {
		t.Fatal(err)
	}
	scriviConfigSMTP(t, d, map[string]string{"outgoing.smtp.host": "smtp.example.invalid", "outgoing.smtp.from_address": "negozio@example.invalid"}, parolaDiProva)
	if attiva() {
		t.Error("configurata ma spenta (D-03): attiva")
	}
	for _, v := range []string{"0", "", "si", " 1"} {
		if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, '2026-11-11T09:00:00.000000Z')
			ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore`, ChiaveEmailAcceso, v); err != nil {
			t.Fatal(err)
		}
		if attiva() {
			t.Errorf("interruttore %q: attiva", v)
		}
	}
	accendiEmail(t, d)
	if !attiva() {
		t.Error("configurata e accesa: spenta")
	}
	p := NuovoSMTP(d, nil)
	if a, err := p.Attivo(ctx); err != nil || !a {
		t.Errorf("Attivo %v %v", a, err)
	}
}

// inventario: CLI-51, CLI-43
// Un canale col provider ma spento (l'email senza configurazione o non accesa): Accoda lo rifiuta come
// non configurato e il giro non legge le sue righe (restano «queued», senza tentativi); acceso, le legge.
func TestCodaCanaleSpento(t *testing.T) {
	d, o := apriDB(t)
	ctx := context.Background()
	p := &providerAccendibile{}
	c := Nuova(d, o, map[string]Provider{CanaleEmail: p})
	dest, cont := email("cliente@example.com", "testo")
	if _, err := c.Accoda(ctx, dest, cont, contesto); !errors.Is(err, ErrNonConfigurato) {
		t.Errorf("Accoda spento: %v", err)
	}
	if a, err := c.CanaleAttivo(ctx, CanaleEmail); err != nil || a {
		t.Errorf("CanaleAttivo spento: %v %v", a, err)
	}
	if a, err := c.CanaleAttivo(ctx, CanaleTelegram); err != nil || a {
		t.Errorf("CanaleAttivo senza provider: %v %v", a, err)
	}
	res, err := d.Scrittura.Exec(`INSERT INTO messaggi_in_uscita (canale, destinatario, indirizzo, testo, stato, tentativi,
		prossimo_tentativo_il, accodato_il) VALUES ('email', 'customer', 'cliente@example.com', 'importata', 'queued', 1, ?1, ?1)`,
		"2026-01-01T09:00:00.000000Z")
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	if e, err := c.Giro(ctx, PerGiro); err != nil || e != (Esito{}) || p.quante() != 0 {
		t.Errorf("giro spento: %+v %v, chiamate %d", e, err, p.quante())
	}
	if got := riga(t, d, id); !strings.HasPrefix(got, "queued|1|") {
		t.Errorf("riga %s", got)
	}
	p.acceso = true
	if a, err := c.CanaleAttivo(ctx, CanaleEmail); err != nil || !a {
		t.Errorf("CanaleAttivo acceso: %v %v", a, err)
	}
	if e, err := c.Giro(ctx, PerGiro); err != nil || e.Accettati != 1 {
		t.Errorf("giro acceso: %+v %v", e, err)
	}
	if _, err := c.Accoda(ctx, dest, cont, contesto); err != nil {
		t.Errorf("Accoda acceso: %v", err)
	}
	// Un errore nel chiedere se il canale è acceso ferma il giro senza toccare le righe.
	p.guasto = errors.New("database chiuso")
	if _, err := c.Giro(ctx, PerGiro); err == nil {
		t.Error("giro con l'errore di Attivo: nessun errore")
	}
	if _, err := c.Accoda(ctx, dest, cont, contesto); err == nil {
		t.Error("Accoda con l'errore di Attivo: nessun errore")
	}
}

// providerAccendibile è un provider finto che dice se il suo canale è acceso.
type providerAccendibile struct {
	providerFinto
	acceso bool
	guasto error
}

func (p *providerAccendibile) Attivo(context.Context) (bool, error) { return p.acceso, p.guasto }

// inventario: CLI-43, CLI-51
// La coda col provider SMTP vero contro il server finto: un rifiuto del server (anche un 5xx) costa un
// tentativo e si riprova dopo 10 secondi, come lo standalone; al tentativo dopo l'email parte.
func TestCodaConSMTP(t *testing.T) {
	d, o := apriDB(t)
	ctx := context.Background()
	s1 := avviaServerFinto(t, &serverFinto{Utente: "negozio-prova", Password: parolaDiProva,
		Risposte: map[string]string{"RCPT": "550 5.1.1 <cliente@example.com>: No such user"}})
	scriviConfigSMTP(t, d, map[string]string{
		"outgoing.smtp.host": "127.0.0.1", "outgoing.smtp.port": fmt.Sprint(s1.porta()), "outgoing.smtp.security": "starttls",
		"outgoing.smtp.username": "negozio-prova", "outgoing.smtp.from_address": "negozio@example.invalid",
	}, parolaDiProva)
	accendiEmail(t, d)
	p := NuovoSMTP(d, o)
	p.radici, p.tempo = s1.Radici, 3*time.Second
	c := Nuova(d, o, map[string]Provider{CanaleEmail: p})
	dest, cont := email("cliente@example.com", "Ciao")
	id, err := c.Accoda(ctx, dest, cont, contesto)
	if err != nil {
		t.Fatal(err)
	}
	if e, err := c.Giro(ctx, PerGiro); err != nil || e.Riprovati != 1 {
		t.Fatalf("primo giro %+v %v", e, err)
	}
	if got := riga(t, d, id); !strings.HasPrefix(got, "queued|1|2026-11-11T09:00:10") || !strings.HasSuffix(got, "|smtp:550") {
		t.Errorf("dopo il 550: %s", got)
	}
	s2 := avviaServerFinto(t, &serverFinto{Utente: "negozio-prova", Password: parolaDiProva})
	scriviConfigSMTP(t, d, map[string]string{"outgoing.smtp.port": fmt.Sprint(s2.porta())}, "")
	p.radici = s2.Radici
	o.Sposta(10 * time.Second)
	if e, err := c.Giro(ctx, PerGiro); err != nil || e.Accettati != 1 {
		t.Fatalf("secondo giro %+v %v", e, err)
	}
	if got := riga(t, d, id); !strings.HasPrefix(got, "accepted|2|") || !strings.Contains(got, "ABC123") {
		t.Errorf("dopo l'invio: %s", got)
	}
}
