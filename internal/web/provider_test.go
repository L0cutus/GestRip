package web

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/ratelimit"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// La pagina «Provider di invio», parte email (provider.go, slice 069b, ADM-09, CLI-52 parte email), come
// ProvidersLive dello standalone a go-baseline (providers_live_test.exs) senza LiveView, più
// l'accensione dell'email con la revisione delle email rimaste in coda (D-03, D-27), solo del Go. Nessun
// server di posta: un provider finto che non apre niente. Dati inventati.

const pwSMTPProva = "PAROLA-FINTA-smtp-di-prova"

// providerWeb è il provider email finto: acceso come quello vero (EmailAttiva sul database), risponde
// quello che il test dice e annota le chiamate.
type providerWeb struct {
	d        *db.DB
	mu       sync.Mutex
	chiamate []uscita.Messaggio
	rispondi func(uscita.Messaggio) (string, error)
}

func (p *providerWeb) Consegna(ctx context.Context, m uscita.Messaggio) (string, error) {
	return p.ConsegnaAMano(ctx, m)
}

func (p *providerWeb) Attivo(ctx context.Context) (bool, error) {
	return uscita.EmailAttiva(ctx, p.d.Lettura)
}

func (p *providerWeb) ConsegnaAMano(_ context.Context, m uscita.Messaggio) (string, error) {
	p.mu.Lock()
	p.chiamate = append(p.chiamate, m)
	p.mu.Unlock()
	if p.rispondi != nil {
		return p.rispondi(m)
	}
	return "250 finto", nil
}

func (p *providerWeb) quante() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.chiamate)
}

// facciaConProvider è la faccia interna con le buste vere e la coda in uscita sul provider finto.
func facciaConProvider(t *testing.T, reg *bytes.Buffer) (http.Handler, *db.DB, *clock.Finto, *providerWeb) {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC))
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	inserisciConti(t, d, contiBase)
	l := nessunRegistro
	if reg != nil {
		l = slog.New(slog.NewTextHandler(reg, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	p := &providerWeb{d: d}
	dd := dipendenzeProva(&sondaFinta{pronto: true}, l, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti = o, ratelimit.Nuovo(o)
	dd.Buste = buste.Nuove(d, o)
	dd.Uscita = uscita.Nuova(d, o, map[string]uscita.Provider{uscita.CanaleEmail: p})
	return mustInterna(t, dd), d, o, p
}

func moduloSMTP(b *browser, kv ...string) url.Values {
	v := url.Values{"_csrf_token": {b.token}, "smtp[host]": {"smtp.example.invalid"}, "smtp[port]": {"587"}, "smtp[security]": {"starttls"},
		"smtp[username]": {"negozio-prova"}, "smtp[password]": {pwSMTPProva}, "smtp[from_address]": {"negozio@example.invalid"},
		"smtp[from_name]": {"Negozio di Prova"}}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return v
}

// titolareNeiProvider entra come titolare e apre la pagina, come chi poi manda i moduli (il token è
// quello della pagina).
func titolareNeiProvider(t *testing.T, h http.Handler) *browser {
	t.Helper()
	b := titolareDentro(t, h)
	if r := b.vai("GET", "/settings/providers", nil, nil); r.codice != 200 {
		t.Fatalf("pagina %d", r.codice)
	}
	return b
}

func postaProva(b *browser, kv ...string) risposta {
	return b.vai("POST", "/settings/providers/smtp", moduloSMTP(b, kv...), nil)
}

func soloToken(b *browser, kv ...string) url.Values {
	v := url.Values{"_csrf_token": {b.token}}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return v
}

// inventario: ADM-09, CLI-52
// La pagina del titolare senza niente configurato: la scheda «Provider» delle impostazioni, il titolo e
// la frase dello standalone, il riquadro «Posta SMTP» con «Non configurato», il modulo (campi, etichette,
// valori predefiniti 587 e STARTTLS, le tre sicurezze con le loro etichette, la password vuota e senza
// segnaposto, la spunta «Cancella…», «Salva provider posta»), «✉ Invia messaggio di prova», il
// riquadro dell'accensione (D-03) spento, e la «Coda uscente» vuota con i conteggi. Niente Telegram
// (T4.7). Aprirla non scrive niente.
func TestPaginaProvider(t *testing.T) {
	h, d, _, _ := facciaConProvider(t, nil)
	b := titolareDentro(t, h)
	prima := contaRighe(t, d, `SELECT count(*) FROM eventi_audit`)
	r := b.vai("GET", "/settings/providers", nil, nil)
	if r.codice != 200 {
		t.Fatalf("pagina %d", r.codice)
	}
	contiene(t, "pagina", r.corpo,
		`<a id="settings-tab-providers" href="/settings/providers" aria-current="page" class="settings-tab">Provider</a>`,
		"<h1>Provider di invio</h1>",
		"Questa installazione manda i messaggi con account propri: nessun servizio centrale, nessun bot condiviso. La coda in fondo dice fin dove è arrivato ogni messaggio, e si ferma ad «accettato dal provider»: nessuno qui può sapere se è stato letto.",
		`<h2>Posta SMTP</h2> <span id="smtp-state" class="badge">Non configurato</span>`,
		`<form id="smtp-form" action="/settings/providers/smtp" method="post" class="provider-modulo">`,
		`<label for="smtp_host">Server</label> <input type="text" id="smtp_host" name="smtp[host]" placeholder="smtp.esempio.it">`,
		`<label for="smtp_port">Porta</label> <input type="number" id="smtp_port" name="smtp[port]" value="587">`,
		`<label for="smtp_security">Sicurezza</label> <select id="smtp_security" name="smtp[security]">`,
		`<option value="starttls" selected>STARTTLS (consigliato)</option>`,
		`<option value="tls">TLS diretto</option>`,
		`<option value="none">Nessuna (relay locale, senza credenziali)</option>`,
		`<label for="smtp_username">Utente</label> <input type="text" id="smtp_username" name="smtp[username]">`,
		`<label for="smtp_password">Password</label> <input type="password" id="smtp_password" name="smtp[password]" value="" autocomplete="new-password" placeholder="">`,
		`<label for="smtp_from_address">Indirizzo mittente</label> <input type="text" id="smtp_from_address" name="smtp[from_address]">`,
		`<label for="smtp_from_name">Nome mittente</label> <input type="text" id="smtp_from_name" name="smtp[from_name]">`,
		`<input type="checkbox" name="smtp[clear_secret]" value="1"> Cancella le credenziali salvate (utente e password)</label>`,
		`<button type="submit">Salva provider posta</button>`,
		`<form id="smtp-test-form" action="/settings/providers/prova" method="post">`,
		`<button id="smtp-test" type="submit" class="pulsante-contorno">✉ Invia messaggio di prova</button>`,
		`<span id="smtp-switch-state" class="badge">Spento</span>`,
		`<input type="hidden" name="acceso" value="1"><button id="smtp-switch-toggle" type="submit">Accendi l'invio delle email</button>`,
		`<h2>Coda uscente</h2>`, `href="/settings/providers">↻ Aggiorna</a>`,
		"In coda 0 · accettati 0 · falliti 0. «Accettato dal provider» vuol dire che il provider ha preso il messaggio, non che sia stato consegnato o letto.",
		"<p>La coda è vuota.</p>")
	nonContiene(t, "pagina", r.corpo, "Telegram", "telegram-form", "salvata — lascia vuoto", `id="smtp-pending"`)
	if contaRighe(t, d, `SELECT count(*) FROM eventi_audit`) != prima {
		t.Error("aprire la pagina scrive l'audit")
	}
}

// inventario: ADM-09, CLI-52
// Il dipendente non entra (on_mount :ensure_owner): la pagina e ogni POST della pagina, anche forgiato
// con il token, vanno a / con «Operazione riservata al titolare.» e non scrivono niente.
func TestProviderSoloTitolare(t *testing.T) {
	h, d, _, p := facciaConProvider(t, nil)
	inserisciCoda(t, d)
	b := dipendenteDentro(t, h)
	b.vai("GET", "/", nil, nil)
	for _, x := range []struct {
		metodo, percorso string
		form             url.Values
	}{
		{"GET", "/settings/providers", nil},
		{"POST", "/settings/providers/smtp", moduloSMTP(b)},
		{"POST", "/settings/providers/prova", soloToken(b)},
		{"POST", "/settings/providers/accensione", soloToken(b, "acceso", "1")},
		{"POST", "/settings/providers/sospese/2/scarta", soloToken(b)},
		{"POST", "/settings/providers/sospese/2/manda", soloToken(b)},
	} {
		r := b.vai(x.metodo, x.percorso, x.form, nil)
		if !redirectA(r, "/") {
			t.Errorf("%s %s: %d %q", x.metodo, x.percorso, r.codice, r.location)
		}
		contiene(t, x.percorso, b.vai("GET", "/", nil, nil).corpo, "Operazione riservata al titolare.")
	}
	if n := contaRighe(t, d, `SELECT (SELECT count(*) FROM impostazioni WHERE chiave LIKE 'outgoing.%' OR chiave = 'canale_email_acceso') + (SELECT count(*) FROM segreti_provider) + (SELECT count(*) FROM eventi_audit WHERE azione LIKE 'outgoing.%')`); n != 0 {
		t.Errorf("il dipendente ha scritto %d righe", n)
	}
	if contaRighe(t, d, `SELECT count(*) FROM messaggi_in_uscita`) != 6 || p.quante() != 0 {
		t.Error("il dipendente ha toccato la coda")
	}
}

// inventario: ADM-09, CLI-52
// Ogni POST della pagina senza il token CSRF: 403, niente scritto.
func TestProviderCSRF(t *testing.T) {
	h, d, _, _ := facciaConProvider(t, nil)
	inserisciCoda(t, d)
	b := titolareNeiProvider(t, h)
	for _, p := range []string{"/settings/providers/smtp", "/settings/providers/prova", "/settings/providers/accensione",
		"/settings/providers/sospese/2/scarta", "/settings/providers/sospese/2/manda"} {
		v := moduloSMTP(b, "acceso", "1")
		v.Del("_csrf_token")
		if r := b.vai("POST", p, v, nil); r.codice != 403 {
			t.Errorf("%s senza token: %d", p, r.codice)
		}
	}
	if n := contaRighe(t, d, `SELECT (SELECT count(*) FROM impostazioni WHERE chiave LIKE 'outgoing.%' OR chiave = 'canale_email_acceso') + (SELECT count(*) FROM eventi_audit WHERE azione LIKE 'outgoing.%') + (SELECT count(*) FROM messaggi_in_uscita)`); n != 6 {
		t.Errorf("righe %d", n)
	}
}

// inventario: ADM-09, CLI-52
// «Salva provider posta»: la pagina con «Provider salvato.» e «Configurato», i valori salvati nel modulo,
// la password mai (casella vuota, con il segnaposto «salvata — lascia vuoto per non cambiarla»), nemmeno
// nel registro del server né nell'audit; poi con la casella vuota e un altro utente la password resta.
func TestSalvaProviderPosta(t *testing.T) {
	var reg bytes.Buffer
	h, d, _, _ := facciaConProvider(t, &reg)
	b := titolareNeiProvider(t, h)
	r := postaProva(b)
	if !redirectA(r, "/settings/providers") {
		t.Fatalf("salva %d %q", r.codice, r.location)
	}
	c := b.vai("GET", "/settings/providers", nil, nil).corpo
	contiene(t, "salvato", c, `<div role="status" class="flash flash-info">Provider salvato.</div>`,
		`<span id="smtp-state" class="badge badge-ok">Configurato</span>`,
		`id="smtp_host" name="smtp[host]" value="smtp.example.invalid"`, `id="smtp_username" name="smtp[username]" value="negozio-prova"`,
		`<input type="password" id="smtp_password" name="smtp[password]" value="" autocomplete="new-password" placeholder="salvata — lascia vuoto per non cambiarla">`,
		`id="smtp_from_name" name="smtp[from_name]" value="Negozio di Prova"`)
	r = postaProva(b, "smtp[password]", "", "smtp[username]", "altro")
	if !redirectA(r, "/settings/providers") {
		t.Fatalf("salva senza password %d", r.codice)
	}
	if v := contaSQL(t, d, `SELECT count(*) FROM segreti_provider WHERE nome = 'outgoing.smtp.password' AND valore = ?`, pwSMTPProva); v != 1 {
		t.Error("la password non è rimasta")
	}
	if n := contaSQL(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.provider_configured' AND attore = 'titolare@esempio.it'`); n != 2 {
		t.Errorf("audit %d", n)
	}
	for _, testo := range []string{c, reg.String(), b.vai("GET", "/settings/providers", nil, nil).corpo} {
		if strings.Contains(testo, pwSMTPProva) {
			t.Error("la password è uscita")
		}
	}
	if n := contaSQL(t, d, `SELECT count(*) FROM eventi_audit WHERE dati LIKE ? OR dati LIKE '%smtp.example%'`, "%"+pwSMTPProva+"%"); n != 0 {
		t.Error("valori nell'audit")
	}
}

// inventario: ADM-09, CLI-52
// Un modulo rifiutato ridisegna la pagina (200) con gli errori sotto i campi, i valori mandati e la casella
// della password vuota, «Non configurato», e non salva niente. Il testo mandato è scappato.
func TestSalvaProviderPostaRifiutato(t *testing.T) {
	h, d, _, _ := facciaConProvider(t, nil)
	b := titolareNeiProvider(t, h)
	r := postaProva(b, "smtp[port]", "70000", "smtp[from_address]", "<b>senza-chiocciola", "smtp[password]", "")
	if r.codice != 200 {
		t.Fatalf("rifiuto %d", r.codice)
	}
	contiene(t, "rifiuto", r.corpo, `<span id="smtp-state" class="badge">Non configurato</span>`,
		`<input type="number" id="smtp_port" name="smtp[port]" value="70000">`+"\n"+`<p class="field-error">Al massimo 65535.</p>`,
		`<input type="text" id="smtp_from_address" name="smtp[from_address]" value="&lt;b&gt;senza-chiocciola">`+"\n"+`<p class="field-error">Formato non valido.</p>`,
		`<input type="password" id="smtp_password" name="smtp[password]" value="" autocomplete="new-password" placeholder="">`+"\n"+`<p class="field-error">serve una password per questo utente</p>`)
	nonContiene(t, "rifiuto", r.corpo, "<b>senza", "Provider salvato.")
	if n := contaRighe(t, d, `SELECT (SELECT count(*) FROM impostazioni WHERE chiave LIKE 'outgoing.%') + (SELECT count(*) FROM segreti_provider) + (SELECT count(*) FROM eventi_audit WHERE azione LIKE 'outgoing.%')`); n != 0 {
		t.Errorf("salvato qualcosa: %d", n)
	}
	// Con una password salvata, un rifiuto non la rimette nella pagina.
	if r := postaProva(b); !redirectA(r, "/settings/providers") {
		t.Fatal("salva")
	}
	r = postaProva(b, "smtp[port]", "abc", "smtp[security]", "boh")
	contiene(t, "rifiuto con la password salvata", r.corpo, `<p class="field-error">Valore non valido.</p>`,
		`<option value="starttls">STARTTLS (consigliato)</option>`)
	if strings.Contains(r.corpo, pwSMTPProva) {
		t.Error("la password nel rifiuto")
	}
}

// inventario: ADM-09, CLI-52
// «Cancella le credenziali salvate»: via password e utente, il server resta.
func TestCancellaCredenzialiPosta(t *testing.T) {
	h, d, _, _ := facciaConProvider(t, nil)
	b := titolareNeiProvider(t, h)
	postaProva(b)
	if r := postaProva(b, "smtp[password]", "", "smtp[clear_secret]", "1"); !redirectA(r, "/settings/providers") {
		t.Fatalf("cancella %d", r.codice)
	}
	if contaRighe(t, d, `SELECT count(*) FROM segreti_provider`) != 0 ||
		contaRighe(t, d, `SELECT count(*) FROM impostazioni WHERE chiave = 'outgoing.smtp.username' AND valore = ''`) != 1 ||
		contaRighe(t, d, `SELECT count(*) FROM impostazioni WHERE chiave = 'outgoing.smtp.host' AND valore = 'smtp.example.invalid'`) != 1 {
		t.Error("credenziali non cancellate")
	}
}

// inventario: ADM-09, CLI-52, OPS-02
// «✉ Invia messaggio di prova» subito (069c): senza configurazione «Posta SMTP non è configurato: niente è
// stato accodato.» (come lo standalone) e nessun invio; configurata, anche con l'invio delle email spento
// (D-03), la prova parte dentro la richiesta al mittente stesso, senza riga nella coda, e la pagina dice
// che il server l'ha accettata; rifiutata, solo il genere dell'errore. La frase di prima («accendilo qui
// sotto, poi manda la prova») non c'è più. Con l'invio acceso lo stesso, sempre senza riga.
func TestProvaProviderPosta(t *testing.T) {
	h, d, _, p := facciaConProvider(t, nil)
	b := titolareNeiProvider(t, h)
	prova := func() string {
		t.Helper()
		if r := b.vai("POST", "/settings/providers/prova", soloToken(b), nil); !redirectA(r, "/settings/providers") {
			t.Fatalf("prova %d %q", r.codice, r.location)
		}
		return b.vai("GET", "/settings/providers", nil, nil).corpo
	}
	contiene(t, "senza configurazione", prova(), `<div role="alert" class="flash flash-errore">Posta SMTP non è configurato: niente è stato accodato.</div>`)
	if p.quante() != 0 {
		t.Fatal("prova senza configurazione")
	}
	postaProva(b, "smtp[security]", "none", "smtp[username]", "", "smtp[password]", "")
	c := prova()
	contiene(t, "spenta", c, `<div role="status" class="flash flash-info">Messaggio di prova accettato da Posta SMTP: guarda se è arrivato nella casella del mittente.</div>`,
		`<span id="smtp-switch-state" class="badge">Spento</span>`, "<p>La coda è vuota.</p>")
	nonContiene(t, "spenta", c, "accendilo qui sotto", "accodato su Posta SMTP")
	if p.quante() != 1 || p.chiamate[0].Indirizzo != "negozio@example.invalid" || p.chiamate[0].Destinatario != "shop" || p.chiamate[0].BustaID != nil ||
		p.chiamate[0].Oggetto == nil || *p.chiamate[0].Oggetto != "GestRip — prova del provider di posta" {
		t.Fatalf("invio %+v", p.chiamate)
	}
	if contaRighe(t, d, `SELECT count(*) FROM messaggi_in_uscita`) != 0 {
		t.Fatal("prova messa in coda")
	}
	p.rispondi = func(uscita.Messaggio) (string, error) {
		return "", uscita.Rifiuto("smtp:535")
	}
	c = prova()
	contiene(t, "rifiutata", c, `<div role="alert" class="flash flash-errore">Messaggio di prova non partito (smtp:535): controlla i dati del server e riprova.</div>`)
	p.rispondi = nil
	if r := b.vai("POST", "/settings/providers/accensione", soloToken(b, "acceso", "1"), nil); !redirectA(r, "/settings/providers") {
		t.Fatalf("accendi %d", r.codice)
	}
	contiene(t, "accesa", prova(), `<div role="status" class="flash flash-info">Messaggio di prova accettato da Posta SMTP: guarda se è arrivato nella casella del mittente.</div>`,
		"In coda 0 · accettati 0 · falliti 0.")
	if p.quante() != 3 || contaRighe(t, d, `SELECT count(*) FROM messaggi_in_uscita`) != 0 {
		t.Errorf("invii %d", p.quante())
	}
	if contaSQL(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.test_sent' AND esito = 'ok'`) != 2 ||
		contaSQL(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.test_sent' AND esito = 'error'`) != 1 ||
		contaSQL(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.test_sent' AND dati LIKE '%example%'`) != 0 {
		t.Error("audit della prova")
	}
}

// inventario: ADM-09, CLI-52
// La prova che non si può mandare per un errore del database: la pagina dice «Non è stato possibile
// mandare la prova.» e il registro solo una riga senza dati.
func TestProvaProviderPostaErrore(t *testing.T) {
	var reg bytes.Buffer
	h, d, _, p := facciaConProvider(t, &reg)
	b := titolareNeiProvider(t, h)
	postaProva(b, "smtp[security]", "none", "smtp[username]", "", "smtp[password]", "")
	eseguiSQL(t, d, `DROP TABLE segreti_provider`)
	if r := b.vai("POST", "/settings/providers/prova", soloToken(b), nil); !redirectA(r, "/settings/providers") {
		t.Fatalf("prova %d", r.codice)
	}
	if p.quante() != 0 {
		t.Error("prova mandata")
	}
	if !strings.Contains(reg.String(), "prova della posta non mandata") || strings.Contains(reg.String(), "negozio@example.invalid") {
		t.Errorf("registro %q", reg.String())
	}
}

// inventario: ADM-09, CLI-52, OPS-02
// Le email da guardare (D-27) col numero di busta visibile della D-64 (069c): «0045/26» per una busta con
// l'etichetta, «N. 7/26» senza; nessuna riga «Busta» per un'email senza busta.
func TestSospeseConNumeroBusta(t *testing.T) {
	h, d, _, _ := facciaConProvider(t, nil)
	inserisciCoda(t, d)
	eseguiSQL(t, d, `INSERT INTO clienti (id, nome, telefono, creato_il, aggiornato_il) VALUES (1, 'Cliente Finto', '+390000000009',
		'2026-01-15T08:05:00.000000Z', '2026-01-15T08:05:00.000000Z')`)
	for _, x := range [][]any{{1, "45/26", "RIP-2026-0045"}, {2, "7/26", nil}} {
		eseguiSQL(t, d, `INSERT INTO buste (id, cliente_id, nome_cliente, telefono_cliente, numero, data_accettazione, descrizione, hash_token_pubblico,
			token_pubblico_scade_il, creato_il, aggiornato_il, codice_qr) VALUES (?, 1, 'Cliente Finto', '+390000000009', ?, '2026-01-15', 'Anello',
			randomblob(32), '2027-01-15T08:05:00.000000Z', '2026-01-15T08:05:00.000000Z', '2026-01-15T08:05:00.000000Z', ?)`, x...)
	}
	eseguiSQL(t, d, `UPDATE messaggi_in_uscita SET busta_id = 1 WHERE id = 2`)
	eseguiSQL(t, d, `UPDATE messaggi_in_uscita SET busta_id = 2 WHERE id = 5`)
	eseguiSQL(t, d, `INSERT INTO messaggi_in_uscita (id, canale, destinatario, indirizzo, oggetto, testo, stato, tentativi, prossimo_tentativo_il, accodato_il)
		VALUES (7, 'email', 'shop', 'negozio@example.invalid', 'Prova', 'Prova', 'queued', 0, '2026-01-15T08:05:00.000000Z', '2026-01-15T08:05:00.000000Z')`)
	b := titolareDentro(t, h)
	c := b.vai("GET", "/settings/providers", nil, nil).corpo
	contiene(t, "busta", c, `<li id="smtp-pending-2">`+"\n<p>Busta 0045/26</p>\n<p>Destinatario: cliente-due@example.com</p>",
		`<li id="smtp-pending-5">`+"\n<p>Busta N. 7/26</p>\n<p>Destinatario: cliente-cinque@example.com</p>",
		`<li id="smtp-pending-7">`+"\n<p>Destinatario: negozio@example.invalid</p>")
}

// inventario: ADM-09, CLI-52
// L'invio acceso con una configurazione non più usabile (069c): la pagina lo dice con una riga; acceso e
// configurato, o spento, la riga non c'è.
func TestAccesoMaNonConfigurato(t *testing.T) {
	h, d, _, _ := facciaConProvider(t, nil)
	b := titolareNeiProvider(t, h)
	riga := `<p id="smtp-switch-unusable" class="flash flash-errore">L&#39;invio delle email è acceso ma Posta SMTP non è configurato: nessuna email parte finché non completi i dati del server qui sopra.</p>`
	nonContiene(t, "spento", b.vai("GET", "/settings/providers", nil, nil).corpo, "smtp-switch-unusable")
	postaProva(b)
	b.vai("POST", "/settings/providers/accensione", soloToken(b, "acceso", "1"), nil)
	nonContiene(t, "configurato", b.vai("GET", "/settings/providers", nil, nil).corpo, "smtp-switch-unusable")
	eseguiSQL(t, d, `UPDATE impostazioni SET valore = '' WHERE chiave = 'outgoing.smtp.host'`)
	c := b.vai("GET", "/settings/providers", nil, nil).corpo
	contiene(t, "non configurato", c, riga, `<span id="smtp-switch-state" class="badge badge-ok">Acceso</span>`,
		`<span id="smtp-state" class="badge">Non configurato</span>`)
}

// inserisciCoda mette nella coda, come le porta l'import: 1 WhatsApp (mai nella pagina), 2 email in coda
// con un link di conferma, 3 email accettata, 4 email fallita, 5 email in coda con dell'HTML, 6 Telegram.
func inserisciCoda(t *testing.T, d *db.DB) {
	t.Helper()
	q := `INSERT INTO messaggi_in_uscita (id, canale, destinatario, indirizzo, oggetto, testo, stato, tentativi, prossimo_tentativo_il, accodato_il,
		accettato_il, fallito_il, riferimento_provider, ultimo_errore) VALUES (?, ?, ?, ?, ?, ?, ?, ?, '2026-01-15T08:05:00.000000Z', '2026-01-15T08:05:00.000000Z', ?, ?, ?, ?)`
	for _, r := range [][]any{
		{1, "whatsapp", "customer", "+390000000001", nil, "Ciao", "accepted", 0, "2026-01-15T08:05:00.000000Z", nil, nil, nil},
		{2, "email", "customer", "cliente-due@example.com", "Conferma", "Conferma: https://negozio.example.invalid/stato/email-conferma?t=SEGNO-FINTO", "queued", 1, nil, nil, nil, "smtp:451"},
		{3, "email", "shop", "negozio@example.invalid", "Prova", "Prova", "accepted", 1, "2026-07-15T08:05:00.000000Z", nil, "250 ok", nil},
		{4, "email", "customer", "cliente-quattro@example.com", "Pronta", "Pronta", "failed", 5, nil, "2026-07-15T22:30:00.000000Z", nil, "smtp:550"},
		{5, "email", "customer", "cliente-cinque@example.com", "<i>Pronta</i>", "<script>Pronta</script>", "queued", 0, nil, nil, nil, nil},
		{6, "telegram", "customer", "12345", nil, "Pronta", "queued", 0, nil, nil, nil, nil},
	} {
		eseguiSQL(t, d, q, r...)
	}
}

// inventario: ADM-09, CLI-52, CLI-51
// La «Coda uscente»: i conteggi delle righe email e Telegram (mai WhatsApp), le righe dalla più recente con
// canale, stato, destinatario, l'ora del negozio (non UTC: 15/01 09:05 d'inverno, 15/07 10:05 d'estate,
// 16/07 00:30 dopo mezzanotte), i tentativi, il riferimento, l'errore.
func TestCodaUscente(t *testing.T) {
	h, d, _, _ := facciaConProvider(t, nil)
	inserisciCoda(t, d)
	b := titolareDentro(t, h)
	c := b.vai("GET", "/settings/providers", nil, nil).corpo
	contiene(t, "coda", c, "In coda 3 · accettati 1 · falliti 1.",
		`<li id="outgoing-message-4" class="provider-riga">`+"\n"+`<div class="provider-testa"><span class="forte">Posta SMTP</span> <span class="badge badge-errore">Fallito</span></div>`+"\n"+
			"<p>Destinatario: cliente-quattro@example.com</p>\n<p>Accodato 15/01/2026 09:05 · tentativi 5 di 5</p>\n<p>Fallito 16/07/2026 00:30</p>\n"+
			`<p class="sotto">Ultimo errore: smtp:550</p>`,
		"<p>Accettato 15/07/2026 10:05 <span>· riferimento 250 ok</span></p>",
		`<li id="outgoing-message-6" class="provider-riga">`+"\n"+`<div class="provider-testa"><span class="forte">Telegram</span> <span class="badge badge-attesa">In coda</span></div>`)
	nonContiene(t, "coda", c, `id="outgoing-message-1"`, "La coda è vuota.", "<script>Pronta")
	if strings.Index(c, `id="outgoing-message-6"`) > strings.Index(c, `id="outgoing-message-2"`) {
		t.Error("ordine della coda")
	}
}

// inventario: OPS-02
// L'accensione dopo l'import: con delle email in coda «Accendi» non accende e dice di guardarle; la pagina
// le mostra una per una (destinatario, oggetto, testo con il link di conferma mascherato e scappato, ora,
// tentativi, errore) con «Manda adesso» e «Scarta» (con la conferma). Scartata una («Email scartata: non
// partirà.») e mandata l'altra («Email accettata dal provider.», un solo invio), si accende («Invio delle
// email acceso: da adesso la coda le manda da sola.»); accesa, la lista sparisce e scarta e manda non fanno
// niente. Le righe Telegram non contano. «Spegni» la rispegne.
func TestAccensioneConRevisione(t *testing.T) {
	h, d, _, p := facciaConProvider(t, nil)
	inserisciCoda(t, d)
	b := titolareNeiProvider(t, h)
	postaProva(b)
	accendi := func(valore string) string {
		t.Helper()
		if r := b.vai("POST", "/settings/providers/accensione", soloToken(b, "acceso", valore), nil); !redirectA(r, "/settings/providers") {
			t.Fatalf("accensione %d %q", r.codice, r.location)
		}
		return b.vai("GET", "/settings/providers", nil, nil).corpo
	}
	c := accendi("1")
	contiene(t, "rifiuto", c, `<div role="alert" class="flash flash-errore">Prima di accendere guarda le email rimaste in coda: mandale o scartale una per una.</div>`,
		`<span id="smtp-switch-state" class="badge">Spento</span>`,
		`<h3>Email rimaste in coda (2)</h3>`,
		`<li id="smtp-pending-2">`,
		"<p>Destinatario: cliente-due@example.com</p>", "<p>Oggetto: Conferma</p>",
		`<pre>Conferma: [link di conferma: solo nell&#39;email]</pre>`,
		"<p>Accodato 15/01/2026 09:05 · tentativi 1 di 5</p>", `<p class="sotto">Ultimo errore: smtp:451</p>`,
		`<form action="/settings/providers/sospese/2/manda" method="post">`,
		`<button id="smtp-pending-send-2" type="submit">Manda adesso</button>`,
		`<form action="/settings/providers/sospese/2/scarta" method="post">`,
		`<button id="smtp-pending-discard-2" type="submit" class="pulsante-contorno" data-confirm="Scartare questa email? Non partirà più.">Scarta</button>`,
		`<li id="smtp-pending-5">`, "<p>Oggetto: &lt;i&gt;Pronta&lt;/i&gt;</p>", "<pre>&lt;script&gt;Pronta&lt;/script&gt;</pre>")
	nonContiene(t, "rifiuto", c, "SEGNO-FINTO", `id="smtp-pending-6"`, `id="smtp-pending-4"`, `id="smtp-pending-3"`)
	if contaRighe(t, d, `SELECT count(*) FROM impostazioni WHERE chiave = 'canale_email_acceso'`) != 0 {
		t.Fatal("acceso con le email in coda")
	}
	if r := b.vai("POST", "/settings/providers/sospese/2/scarta", soloToken(b), nil); !redirectA(r, "/settings/providers") {
		t.Fatalf("scarta %d", r.codice)
	}
	c = b.vai("GET", "/settings/providers", nil, nil).corpo
	contiene(t, "scartata", c, `<div role="status" class="flash flash-info">Email scartata: non partirà.</div>`, `<h3>Email rimaste in coda (1)</h3>`)
	nonContiene(t, "scartata", c, `id="smtp-pending-2"`)
	if r := b.vai("POST", "/settings/providers/sospese/5/manda", soloToken(b), nil); !redirectA(r, "/settings/providers") {
		t.Fatalf("manda %d", r.codice)
	}
	contiene(t, "mandata", b.vai("GET", "/settings/providers", nil, nil).corpo, `<div role="status" class="flash flash-info">Email accettata dal provider.</div>`)
	if p.quante() != 1 || p.chiamate[0].ID != 5 {
		t.Fatalf("invii %d", p.quante())
	}
	// Una riga che non è un'email in coda.
	for _, id := range []string{"6", "4", "2", "0", "x"} {
		if r := b.vai("POST", "/settings/providers/sospese/"+id+"/manda", soloToken(b), nil); !redirectA(r, "/settings/providers") {
			t.Fatalf("manda %s: %d", id, r.codice)
		}
		contiene(t, "manda "+id, b.vai("GET", "/settings/providers", nil, nil).corpo, `<div role="alert" class="flash flash-errore">Questa email non è più in coda.</div>`)
	}
	c = accendi("1")
	contiene(t, "acceso", c, `<div role="status" class="flash flash-info">Invio delle email acceso: da adesso la coda le manda da sola.</div>`,
		`<span id="smtp-switch-state" class="badge badge-ok">Acceso</span>`,
		`<input type="hidden" name="acceso" value="0"><button id="smtp-switch-toggle" type="submit" class="pulsante-contorno">Spegni l'invio delle email</button>`)
	nonContiene(t, "acceso", c, `id="smtp-pending"`)
	if contaSQL(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.channel_enabled'`) != 1 ||
		contaSQL(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.queued_discarded'`) != 1 ||
		contaSQL(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.queued_sent_by_hand'`) != 1 {
		t.Error("audit")
	}
	// Un secondo «Accendi» (un'altra scheda) non spegne: il pulsante porta lo stato che chiede.
	accendi("1")
	if contaSQL(t, d, `SELECT count(*) FROM impostazioni WHERE chiave = 'canale_email_acceso' AND valore = '1'`) != 1 {
		t.Error("il secondo «Accendi» ha cambiato l'interruttore")
	}
	eseguiSQL(t, d, `UPDATE messaggi_in_uscita SET stato = 'queued', accettato_il = NULL WHERE id = 3`)
	b.vai("POST", "/settings/providers/sospese/3/scarta", soloToken(b), nil)
	contiene(t, "scarta da accesa", b.vai("GET", "/settings/providers", nil, nil).corpo,
		`<div role="alert" class="flash flash-errore">L&#39;invio delle email è acceso: le email in coda le manda la coda.</div>`)
	if contaSQL(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE id = 3`) != 1 {
		t.Error("scartata con l'email accesa")
	}
	c = accendi("0")
	contiene(t, "spento", c, `<div role="status" class="flash flash-info">Invio delle email spento: le email restano in coda finché non lo riaccendi.</div>`,
		`<span id="smtp-switch-state" class="badge">Spento</span>`, `<li id="smtp-pending-3">`)
}

// inventario: OPS-02
// «Manda adesso» rifiutato dal provider: la riga resta in coda con il tentativo e l'errore senza dati, la
// pagina lo dice; senza configurazione nessun tentativo.
func TestMandaAdessoRifiutato(t *testing.T) {
	h, d, _, p := facciaConProvider(t, nil)
	inserisciCoda(t, d)
	b := titolareNeiProvider(t, h)
	b.vai("POST", "/settings/providers/sospese/5/manda", soloToken(b), nil)
	contiene(t, "senza configurazione", b.vai("GET", "/settings/providers", nil, nil).corpo,
		`<div role="alert" class="flash flash-errore">Posta SMTP non è configurato: niente è stato inviato.</div>`)
	if p.quante() != 0 {
		t.Fatal("provato senza configurazione")
	}
	postaProva(b)
	p.rispondi = func(uscita.Messaggio) (string, error) { return "", uscita.Rifiuto("smtp:550") }
	b.vai("POST", "/settings/providers/sospese/5/manda", soloToken(b), nil)
	c := b.vai("GET", "/settings/providers", nil, nil).corpo
	contiene(t, "rifiutata", c, `<div role="alert" class="flash flash-errore">Email non partita (smtp:550): resta in coda, tentativi 1 di 5.</div>`,
		`<li id="smtp-pending-5">`)
	eseguiSQL(t, d, `UPDATE messaggi_in_uscita SET tentativi = 4 WHERE id = 5`)
	b.vai("POST", "/settings/providers/sospese/5/manda", soloToken(b), nil)
	contiene(t, "fallita", b.vai("GET", "/settings/providers", nil, nil).corpo,
		`<div role="alert" class="flash flash-errore">Email non partita (smtp:550): segnata come fallita.</div>`)
}
