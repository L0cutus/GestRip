package uscita

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La coda durevole dei messaggi in uscita (slice 068, CLI-51), come Outgoing.enqueue/3,
// dispatch_once/1 e Outgoing.Dispatcher dello standalone a go-baseline (outgoing_test.exs,
// message_test.exs). Nessun provider vero: solo providerFinto. Dati inventati.

var inizio = time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)

const segretoFinto = "SEGRETO-FINTO-del-provider"

// providerFinto è Providers.Fake: non apre niente, risponde quello che il test dice e annota le chiamate.
type providerFinto struct {
	mu        sync.Mutex
	chiamate  []Messaggio
	rispondi  func(Messaggio) (string, error)
	conPanico bool
}

func (p *providerFinto) Consegna(_ context.Context, m Messaggio) (string, error) {
	p.mu.Lock()
	p.chiamate = append(p.chiamate, m)
	p.mu.Unlock()
	if p.conPanico {
		panic("il provider è caduto con " + m.Indirizzo)
	}
	if p.rispondi != nil {
		return p.rispondi(m)
	}
	return "finto-" + m.Indirizzo, nil
}

func (p *providerFinto) Segreti() []string { return []string{segretoFinto} }

func (p *providerFinto) quante() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.chiamate)
}

func apriDB(t *testing.T) (*db.DB, *clock.Finto) {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(inizio)
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	return d, o
}

var contesto = audit.Contesto{Attore: "titolare@esempio.it"}

func email(indirizzo, testo string) (Destinazione, Contenuto) {
	oggetto := "Oggetto di prova"
	return Destinazione{Canale: CanaleEmail, Destinatario: "customer", Indirizzo: indirizzo}, Contenuto{Oggetto: &oggetto, Testo: testo}
}

// riga è la riga della coda come stringa: stato|tentativi|prossimo|accettato|fallito|riferimento|errore.
func riga(t *testing.T, d *db.DB, id int64) string {
	t.Helper()
	var s string
	if err := d.Lettura.QueryRow(`SELECT stato || '|' || tentativi || '|' || prossimo_tentativo_il || '|' || coalesce(accettato_il, '-') || '|' ||
		coalesce(fallito_il, '-') || '|' || coalesce(riferimento_provider, '-') || '|' || coalesce(ultimo_errore, '-') FROM messaggi_in_uscita WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func conta(t *testing.T, d *db.DB, q string, arg ...any) int {
	t.Helper()
	var n int
	if err := d.Lettura.QueryRow(q, arg...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// inventario: CLI-51
// Accoda scrive la riga «queued» con zero tentativi, il prossimo tentativo adesso e il testo senza spazi
// ai bordi, e outgoing.queued (canale, parte, busta; mai testo né indirizzo) nella stessa transazione.
// Rifiuti all'ingresso, senza scrivere niente: WhatsApp (si consegna a mano), un canale senza provider
// (non configurato), un indirizzo vuoto, con spazi o a capo (potrebbe iniettare un'intestazione) o più
// lungo di 320, un testo vuoto, una parte o un canale sconosciuti.
func TestAccoda(t *testing.T) {
	d, o := apriDB(t)
	c := Nuova(d, o, map[string]Provider{CanaleEmail: &providerFinto{}})
	dest, cont := email("cliente@esempio.it", "  Ciao\nseconda riga  \n")
	id, err := c.Accoda(context.Background(), dest, cont, contesto)
	if err != nil {
		t.Fatal(err)
	}
	if got := riga(t, d, id); got != "queued|0|2026-11-11T09:00:00.000000Z|-|-|-|-" {
		t.Errorf("riga %s", got)
	}
	if conta(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE id = ? AND testo = 'Ciao
seconda riga' AND accodato_il = '2026-11-11T09:00:00.000000Z' AND oggetto = 'Oggetto di prova' AND canale = 'email'
		AND destinatario = 'customer' AND busta_id IS NULL`, id) != 1 {
		t.Error("colonne della riga")
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.queued' AND risorsa = 'outgoing_message' AND id_risorsa = ?
		AND dati = '{"channel":"email","party":"customer","repair_id":null}' AND attore = 'titolare@esempio.it'`, id) != 1 {
		t.Errorf("audit: %d", conta(t, d, `SELECT count(*) FROM eventi_audit`))
	}
	prima := conta(t, d, `SELECT (SELECT count(*) FROM messaggi_in_uscita) * 1000 + (SELECT count(*) FROM eventi_audit)`)
	for nome, caso := range map[string]struct {
		dest Destinazione
		cont Contenuto
		err  error
	}{
		"whatsapp":        {Destinazione{Canale: "whatsapp", Destinatario: "customer", Indirizzo: "+39333111"}, Contenuto{Testo: "x"}, ErrSoloAMano},
		"telegram spento": {Destinazione{Canale: CanaleTelegram, Destinatario: "shop", Indirizzo: "12345"}, Contenuto{Testo: "x"}, ErrNonConfigurato},
		"vuoto":           {Destinazione{Canale: CanaleEmail, Destinatario: "customer", Indirizzo: ""}, Contenuto{Testo: "x"}, ErrMessaggioNonValido},
		"a capo":          {Destinazione{Canale: CanaleEmail, Destinatario: "customer", Indirizzo: "a@esempio.it\r\nBcc: b@esempio.it"}, Contenuto{Testo: "x"}, ErrMessaggioNonValido},
		"spazio":          {Destinazione{Canale: CanaleEmail, Destinatario: "customer", Indirizzo: "a @esempio.it"}, Contenuto{Testo: "x"}, ErrMessaggioNonValido},
		"lungo":           {Destinazione{Canale: CanaleEmail, Destinatario: "customer", Indirizzo: strings.Repeat("a", 321)}, Contenuto{Testo: "x"}, ErrMessaggioNonValido},
		"testo vuoto":     {Destinazione{Canale: CanaleEmail, Destinatario: "customer", Indirizzo: "a@esempio.it"}, Contenuto{Testo: " \n "}, ErrMessaggioNonValido},
		"parte":           {Destinazione{Canale: CanaleEmail, Destinatario: "vicino", Indirizzo: "a@esempio.it"}, Contenuto{Testo: "x"}, ErrMessaggioNonValido},
		"canale":          {Destinazione{Canale: "piccione", Destinatario: "customer", Indirizzo: "a@esempio.it"}, Contenuto{Testo: "x"}, ErrMessaggioNonValido},
	} {
		if _, err := c.Accoda(context.Background(), caso.dest, caso.cont, contesto); !errors.Is(err, caso.err) {
			t.Errorf("%s: %v, atteso %v", nome, err, caso.err)
		}
	}
	if dopo := conta(t, d, `SELECT (SELECT count(*) FROM messaggi_in_uscita) * 1000 + (SELECT count(*) FROM eventi_audit)`); dopo != prima {
		t.Errorf("un rifiuto ha scritto: %d → %d", prima, dopo)
	}
	// Un indirizzo di 320 caratteri va.
	if _, err := c.Accoda(context.Background(), Destinazione{Canale: CanaleEmail, Destinatario: "shop", Indirizzo: strings.Repeat("a", 320)}, Contenuto{Testo: "x"}, contesto); err != nil {
		t.Errorf("320 caratteri: %v", err)
	}
}

// inventario: CLI-51
// Un giro passa al provider le righe in coda il cui momento è venuto, dalla più vecchia, al più quante
// ne dice il limite; accettata: stato «accepted», un tentativo, l'ora e il riferimento del provider,
// nessun errore. Le righe del futuro, quelle già accettate o fallite e quelle WhatsApp non si toccano.
func TestGiroAccetta(t *testing.T) {
	d, o := apriDB(t)
	p := &providerFinto{}
	c := Nuova(d, o, map[string]Provider{CanaleEmail: p})
	var ids []int64
	for _, a := range []string{"uno@esempio.it", "due@esempio.it", "tre@esempio.it"} {
		dest, cont := email(a, "testo")
		id, err := c.Accoda(context.Background(), dest, cont, contesto)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if _, err := d.Scrittura.Exec(`UPDATE messaggi_in_uscita SET prossimo_tentativo_il = '2026-11-11T09:00:01.000000Z' WHERE id = ?`, ids[2]); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Scrittura.Exec(`INSERT INTO messaggi_in_uscita (canale, destinatario, indirizzo, testo, stato, tentativi, prossimo_tentativo_il,
		accodato_il, accettato_il) VALUES ('whatsapp', 'customer', '+39333', 'a mano', 'accepted', 1, ?1, ?1, ?1)`, "2026-11-10T09:00:00.000000Z"); err != nil {
		t.Fatal(err)
	}
	o.Sposta(time.Millisecond)
	e, err := c.Giro(context.Background(), 1)
	if err != nil || e != (Esito{Accettati: 1}) || p.quante() != 1 || p.chiamate[0].ID != ids[0] || p.chiamate[0].Indirizzo != "uno@esempio.it" ||
		p.chiamate[0].Testo != "testo" || p.chiamate[0].Oggetto == nil || *p.chiamate[0].Oggetto != "Oggetto di prova" {
		t.Fatalf("primo giro: %+v %v %+v", e, err, p.chiamate)
	}
	if got := riga(t, d, ids[0]); got != "accepted|1|2026-11-11T09:00:00.000000Z|2026-11-11T09:00:00.001000Z|-|finto-uno@esempio.it|-" {
		t.Errorf("accettata: %s", got)
	}
	e, err = c.Giro(context.Background(), 50)
	if err != nil || e != (Esito{Accettati: 1}) || p.quante() != 2 || p.chiamate[1].ID != ids[1] {
		t.Fatalf("secondo giro: %+v %v", e, err)
	}
	if !strings.HasPrefix(riga(t, d, ids[2]), "queued|0|") {
		t.Error("la riga del futuro è partita")
	}
	o.Sposta(time.Second)
	if e, _ := c.Giro(context.Background(), 50); e != (Esito{Accettati: 1}) || p.quante() != 3 {
		t.Errorf("terzo giro: %+v", e)
	}
	if e, _ := c.Giro(context.Background(), 50); e != (Esito{}) || p.quante() != 3 {
		t.Errorf("giro vuoto: %+v", e)
	}
}

// inventario: CLI-51
// Un rifiuto del provider costa un tentativo e la riga resta in coda con le attese dello standalone (10 s,
// 60 s, 5 min, 30 min dopo il 1°, 2°, 3° e 4° rifiuto); il quinto la fa fallire («failed», l'ora, nessun
// altro tentativo). Prima del suo momento la riga non riparte. Dopo un'accettazione l'errore si toglie.
func TestGiroTentativi(t *testing.T) {
	d, o := apriDB(t)
	p := &providerFinto{rispondi: func(Messaggio) (string, error) { return "", Rifiuto("smtp:451 riprova") }}
	c := Nuova(d, o, map[string]Provider{CanaleEmail: p})
	dest, cont := email("cliente@esempio.it", "testo")
	id, err := c.Accoda(context.Background(), dest, cont, contesto)
	if err != nil {
		t.Fatal(err)
	}
	for i, attesa := range []time.Duration{10 * time.Second, time.Minute, 5 * time.Minute, 30 * time.Minute} {
		if e, err := c.Giro(context.Background(), 50); err != nil || e != (Esito{Riprovati: 1}) {
			t.Fatalf("tentativo %d: %+v %v", i+1, e, err)
		}
		prossimo := o.Adesso().Add(attesa).Format(db.FormatoIstante)
		if got := riga(t, d, id); got != "queued|"+string(rune('1'+i))+"|"+prossimo+"|-|-|-|smtp:451 riprova" {
			t.Fatalf("dopo il tentativo %d: %s", i+1, got)
		}
		o.Sposta(attesa - time.Millisecond)
		if e, _ := c.Giro(context.Background(), 50); e != (Esito{}) {
			t.Fatalf("partita prima del momento (%d): %+v", i+1, e)
		}
		o.Sposta(time.Millisecond)
	}
	if e, _ := c.Giro(context.Background(), 50); e != (Esito{Falliti: 1}) {
		t.Fatalf("quinto: %+v", e)
	}
	ora := o.Adesso().Format(db.FormatoIstante)
	if got := riga(t, d, id); !strings.HasPrefix(got, "failed|5|") || !strings.Contains(got, "|-|"+ora+"|-|smtp:451 riprova") {
		t.Errorf("fallita: %s", got)
	}
	o.Sposta(48 * time.Hour)
	if e, _ := c.Giro(context.Background(), 50); e != (Esito{}) || p.quante() != 5 {
		t.Errorf("una fallita è ripartita: %+v %d", e, p.quante())
	}
	// Rifiutata una volta, poi accettata: l'errore si toglie.
	p.rispondi = func(m Messaggio) (string, error) {
		if m.Tentativi == 0 {
			return "", Rifiuto("smtp:421")
		}
		return "rif-2", nil
	}
	id, _ = c.Accoda(context.Background(), dest, cont, contesto)
	_, _ = c.Giro(context.Background(), 50)
	o.Sposta(10 * time.Second)
	if e, _ := c.Giro(context.Background(), 50); e != (Esito{Accettati: 1}) || !strings.HasSuffix(riga(t, d, id), "|rif-2|-") || !strings.HasPrefix(riga(t, d, id), "accepted|2|") {
		t.Errorf("accettata dopo un rifiuto: %+v %s", e, riga(t, d, id))
	}
}

// inventario: CLI-51
// L'errore salvato non porta dati: il motivo che il provider dichiara (Rifiuto), senza i suoi segreti e
// tagliato a 500 caratteri; un altro errore (che può portare l'indirizzo o il testo) diventa
// «provider_error»; un canale senza provider «not_configured» (costa il tentativo, come lo
// standalone); un provider che va in panico «raised», e il giro va avanti con le altre righe. Nel
// registro del server niente indirizzi, testi né segreti.
func TestGiroErroreSenzaDati(t *testing.T) {
	d, o := apriDB(t)
	var log bytes.Buffer
	p := &providerFinto{}
	c := Nuova(d, o, map[string]Provider{CanaleEmail: p})
	c.log = slog.New(slog.NewTextHandler(&log, nil))
	casi := []struct {
		risposta error
		errore   string
	}{
		{Rifiuto("auth:" + segretoFinto + " rifiutato"), "auth:*** rifiutato"},
		{Rifiuto(strings.Repeat("è", 600)), strings.Repeat("è", 500)},
		{errors.New("550 cliente@esempio.it: testo segreto"), "provider_error"},
	}
	// Il primo tentativo di ciascuna riga: l'errore salvato.
	for _, x := range casi {
		dest, cont := email("cliente@esempio.it", "testo segreto")
		id, _ := c.Accoda(context.Background(), dest, cont, contesto)
		risposta := x.risposta
		p.rispondi = func(Messaggio) (string, error) { return "", risposta }
		_, _ = c.Giro(context.Background(), 50)
		var errore string
		if err := d.Lettura.QueryRow(`SELECT ultimo_errore FROM messaggi_in_uscita WHERE id = ?`, id).Scan(&errore); err != nil || errore != x.errore {
			t.Errorf("%v: errore %q, atteso %q (%v)", x.risposta, errore, x.errore, err)
		}
		if _, err := d.Scrittura.Exec(`UPDATE messaggi_in_uscita SET stato = 'failed', fallito_il = prossimo_tentativo_il WHERE id = ?`, id); err != nil {
			t.Fatal(err)
		}
	}
	// Un provider tolto fra la lettura e il tentativo (non succede in serve, dove i provider non cambiano):
	// «not_configured», un tentativo.
	senza := Nuova(d, o, map[string]Provider{CanaleEmail: p})
	dest, cont := email("cliente@esempio.it", "testo segreto")
	id, _ := senza.Accoda(context.Background(), dest, cont, contesto)
	m, err := senza.daMandare(context.Background(), o.Adesso().Format(db.FormatoIstante), 50)
	if err != nil || len(m) != 1 {
		t.Fatalf("righe %d %v", len(m), err)
	}
	senza.provider = map[string]Provider{}
	rif, errore := senza.prova(context.Background(), m[0])
	var e Esito
	if err := senza.salva(context.Background(), m[0], rif, errore, &e); err != nil || e != (Esito{Riprovati: 1}) ||
		!strings.HasPrefix(riga(t, d, id), "queued|1|") || !strings.HasSuffix(riga(t, d, id), "|not_configured") {
		t.Errorf("senza provider: %+v %v %s", e, err, riga(t, d, id))
	}
	if _, err := d.Scrittura.Exec(`UPDATE messaggi_in_uscita SET stato = 'failed', fallito_il = prossimo_tentativo_il WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	// Il panico: «raised» per quella riga, l'altra va.
	p.rispondi = nil
	a, _ := c.Accoda(context.Background(), dest, cont, contesto)
	dest2, cont2 := email("altro@esempio.it", "testo segreto")
	b, _ := c.Accoda(context.Background(), dest2, cont2, contesto)
	p.rispondi = func(m Messaggio) (string, error) {
		if m.ID == a {
			panic("caduto con " + m.Indirizzo + " " + m.Testo + " " + segretoFinto)
		}
		return "ok", nil
	}
	if e, _ := c.Giro(context.Background(), 50); e != (Esito{Accettati: 1, Riprovati: 1}) || !strings.HasSuffix(riga(t, d, a), "|raised") || !strings.HasPrefix(riga(t, d, b), "accepted|") {
		t.Errorf("panico: %+v %s %s", e, riga(t, d, a), riga(t, d, b))
	}
	for _, s := range []string{"esempio.it", "testo segreto", segretoFinto} {
		if strings.Contains(log.String(), s) {
			t.Errorf("il registro contiene %q: %s", s, log.String())
		}
	}
	if conta(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE ultimo_errore LIKE '%esempio.it%' OR ultimo_errore LIKE '%segreto%' OR riferimento_provider LIKE '%`+segretoFinto+`%'`) != 0 {
		t.Error("un errore salvato porta dati")
	}
}

// inventario: CLI-51
// Una riga che esce dalla coda mentre il giro lavora su quelle prima (un'altra istanza l'ha già mandata,
// o è stata spenta) non va al provider; il salvataggio dell'esito vale solo su una riga ancora in coda.
func TestGiroRigaUscitaDallaCoda(t *testing.T) {
	d, o := apriDB(t)
	p := &providerFinto{}
	c := Nuova(d, o, map[string]Provider{CanaleEmail: p})
	dest, cont := email("uno@esempio.it", "testo")
	a, _ := c.Accoda(context.Background(), dest, cont, contesto)
	dest, cont = email("due@esempio.it", "testo")
	b, _ := c.Accoda(context.Background(), dest, cont, contesto)
	p.rispondi = func(m Messaggio) (string, error) {
		if m.ID == a {
			if _, err := d.Scrittura.Exec(`UPDATE messaggi_in_uscita SET stato = 'failed', fallito_il = accodato_il, ultimo_errore = 'spenta' WHERE id = ?`, b); err != nil {
				t.Error(err)
			}
			if _, err := d.Scrittura.Exec(`UPDATE messaggi_in_uscita SET stato = 'accepted', accettato_il = accodato_il, riferimento_provider = 'altro' WHERE id = ?`, a); err != nil {
				t.Error(err)
			}
		}
		return "mio", nil
	}
	if e, _ := c.Giro(context.Background(), 50); e != (Esito{Accettati: 1}) || p.quante() != 1 {
		t.Fatalf("%+v %d", e, p.quante())
	}
	if !strings.HasPrefix(riga(t, d, a), "accepted|0|") || !strings.HasSuffix(riga(t, d, a), "|altro|-") || !strings.HasPrefix(riga(t, d, b), "failed|0|") {
		t.Errorf("righe riscritte: %s %s", riga(t, d, a), riga(t, d, b))
	}
}

// inventario: CLI-51
// La coda è durevole: una riga rifiutata prima di un riavvio riparte dopo, al suo momento, da una coda
// nuova sullo stesso database (il Lavoro di gestrip serve), con i tentativi già fatti; il Lavoro fa un
// giro subito, poi aspetta, e si ferma con il contesto; un panico in un giro non lo ferma.
func TestCodaRipresaDopoUnRiavvio(t *testing.T) {
	d, o := apriDB(t)
	p := &providerFinto{rispondi: func(Messaggio) (string, error) { return "", Rifiuto("smtp:421") }}
	prima := Nuova(d, o, map[string]Provider{CanaleEmail: p})
	dest, cont := email("cliente@esempio.it", "testo")
	id, _ := prima.Accoda(context.Background(), dest, cont, contesto)
	_, _ = prima.Giro(context.Background(), 50)
	// Il riavvio: una coda nuova, un provider che accetta.
	o.Sposta(11 * time.Second)
	nuovo := &providerFinto{}
	dopo := Nuova(d, o, map[string]Provider{CanaleEmail: nuovo})
	ctx, ferma := context.WithCancel(context.Background())
	giri := 0
	fatto := make(chan struct{})
	go func() {
		dopo.Lavoro(ctx, func(context.Context) bool {
			giri++
			if giri == 1 {
				// Al secondo giro un provider che va in panico non ferma il lavoro.
				nuovo.conPanico = true
				if _, err := dopo.Accoda(context.Background(), dest, cont, contesto); err != nil {
					t.Error(err)
				}
				return true
			}
			ferma()
			return false
		})
		close(fatto)
	}()
	select {
	case <-fatto:
	case <-time.After(10 * time.Second):
		t.Fatal("il lavoro non si ferma")
	}
	if got := riga(t, d, id); !strings.HasPrefix(got, "accepted|2|") {
		t.Errorf("dopo il riavvio: %s", got)
	}
	if giri != 2 || nuovo.quante() != 2 {
		t.Errorf("giri %d, chiamate %d", giri, nuovo.quante())
	}
	if conta(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE ultimo_errore = 'raised' AND stato = 'queued'`) != 1 {
		t.Error("il panico non è stato registrato sulla sua riga")
	}
}

// inventario: CLI-51
// L'indice per busta (Dubbio 5 della 067): la lista degli invii della scheda e il giro della coda usano
// un indice, non tutta la tabella.
func TestIndiciDellaCoda(t *testing.T) {
	d, _ := apriDB(t)
	for q, indice := range map[string]string{
		`SELECT id FROM messaggi_in_uscita WHERE busta_id = 1 ORDER BY id DESC`:                                             "messaggi_in_uscita_busta",
		`SELECT id FROM messaggi_in_uscita WHERE stato = 'queued' AND prossimo_tentativo_il <= '2026' ORDER BY id LIMIT 50`: "messaggi_in_uscita_in_attesa",
	} {
		rr, err := d.Lettura.Query(`EXPLAIN QUERY PLAN ` + q)
		if err != nil {
			t.Fatal(err)
		}
		var piano strings.Builder
		for rr.Next() {
			var a, b, c int
			var s string
			if err := rr.Scan(&a, &b, &c, &s); err != nil {
				t.Fatal(err)
			}
			piano.WriteString(s + "\n")
		}
		_ = rr.Close()
		if !strings.Contains(piano.String(), indice) {
			t.Errorf("%s: %s", q, piano.String())
		}
	}
}

// inventario: CLI-51
// Un giro prende solo le righe dei canali che hanno un provider (review della 068): con il solo provider
// email una riga Telegram in coda (per esempio importata dallo standalone prima della T4.7) resta com'è,
// senza tentativi, errore né scritture, giro dopo giro; la riga email accanto parte.
func TestGiroSoloCanaliConProvider(t *testing.T) {
	d, o := apriDB(t)
	p := &providerFinto{}
	c := Nuova(d, o, map[string]Provider{CanaleEmail: p})
	res, err := d.Scrittura.Exec(`INSERT INTO messaggi_in_uscita (canale, destinatario, indirizzo, testo, stato, tentativi,
		prossimo_tentativo_il, accodato_il) VALUES ('telegram', 'customer', '12345', 'importata', 'queued', 0, ?1, ?1)`, "2026-11-10T09:00:00.000000Z")
	if err != nil {
		t.Fatal(err)
	}
	tg, _ := res.LastInsertId()
	prima := riga(t, d, tg)
	dest, cont := email("cliente@esempio.it", "testo")
	em, _ := c.Accoda(context.Background(), dest, cont, contesto)
	for range 8 {
		if _, err := c.Giro(context.Background(), 50); err != nil {
			t.Fatal(err)
		}
		o.Sposta(time.Hour)
	}
	if got := riga(t, d, tg); got != prima || got != "queued|0|2026-11-10T09:00:00.000000Z|-|-|-|-" {
		t.Errorf("la riga Telegram è cambiata: %s", got)
	}
	if !strings.HasPrefix(riga(t, d, em), "accepted|1|") || p.quante() != 1 {
		t.Errorf("la riga email: %s, chiamate %d", riga(t, d, em), p.quante())
	}
	// Senza nessun provider il giro non tocca niente.
	vuota := Nuova(d, o, nil)
	if e, err := vuota.Giro(context.Background(), 50); err != nil || e != (Esito{}) || riga(t, d, tg) != prima {
		t.Errorf("senza provider: %+v %v %s", e, err, riga(t, d, tg))
	}
}

// inventario: CLI-51
// Il rifiuto e il quinto tentativo scrivono solo su una riga ancora in coda: una riga chiusa da altri fra
// la lettura e il salvataggio (qui dal provider stesso) resta com'era, senza tentativo né errore in più.
func TestGiroRifiutoSuRigaChiusaNelFrattempo(t *testing.T) {
	d, o := apriDB(t)
	p := &providerFinto{}
	c := Nuova(d, o, map[string]Provider{CanaleEmail: p})
	chiudi := func(id int64) {
		if _, err := d.Scrittura.Exec(`UPDATE messaggi_in_uscita SET stato = 'failed', fallito_il = accodato_il, ultimo_errore = 'altrove' WHERE id = ?`, id); err != nil {
			t.Error(err)
		}
	}
	p.rispondi = func(m Messaggio) (string, error) { chiudi(m.ID); return "", Rifiuto("smtp:451") }
	for _, gia := range []int{0, MaxTentativi - 1} {
		dest, cont := email("cliente@esempio.it", "testo")
		id, _ := c.Accoda(context.Background(), dest, cont, contesto)
		if _, err := d.Scrittura.Exec(`UPDATE messaggi_in_uscita SET tentativi = ? WHERE id = ?`, gia, id); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Giro(context.Background(), 50); err != nil {
			t.Fatal(err)
		}
		want := "failed|" + string(rune('0'+gia)) + "|2026-11-11T09:00:00.000000Z|-|2026-11-11T09:00:00.000000Z|-|altrove"
		if got := riga(t, d, id); got != want {
			t.Errorf("già %d tentativi: %s, atteso %s", gia, got, want)
		}
	}
}

// inventario: CLI-51
// Anche il riferimento del provider passa dalla pulizia: un segreto lì dentro diventa «***».
func TestGiroRiferimentoSenzaSegreti(t *testing.T) {
	d, o := apriDB(t)
	p := &providerFinto{rispondi: func(Messaggio) (string, error) { return "ok " + segretoFinto + " 250", nil }}
	c := Nuova(d, o, map[string]Provider{CanaleEmail: p})
	dest, cont := email("cliente@esempio.it", "testo")
	id, _ := c.Accoda(context.Background(), dest, cont, contesto)
	if _, err := c.Giro(context.Background(), 50); err != nil {
		t.Fatal(err)
	}
	if got := riga(t, d, id); !strings.HasSuffix(got, "|ok *** 250|-") {
		t.Errorf("riferimento: %s", got)
	}
}
