package web

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/firmeflask"
	"github.com/L0cutus/gestrip/internal/ratelimit"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// L'avviso email della pagina del cliente sulla faccia pubblica (CLI-39, CLI-40, CLI-41, CLI-19), come
// public_repair_email_test.exs e public_repair_email_stop_test.exs dello standalone (go-baseline), con
// la D-16 e la pagina di ripiego della D-02 (solo Go).

const pubblicoEmail = "https://negozio.example.invalid"

// postinoFinto è il provider finto dell'email (prende tutto); spento è l'email spenta (D-03, D-16).
type postinoFinto struct{ spento bool }

func (p *postinoFinto) Consegna(context.Context, uscita.Messaggio) (string, error) {
	return "250 ok", nil
}
func (p *postinoFinto) Attivo(context.Context) (bool, error) { return !p.spento, nil }

type provaEmailWeb struct {
	provaCliente
	posta  *postinoFinto
	lavori []func()
}

// emailWebProva è la faccia pubblica con la coda dell'email accesa, l'indirizzo pubblico e i lavori in
// sottofondo tenuti da parte (girano quando il test li chiama).
func emailWebProva(t *testing.T) *provaEmailWeb {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Apri(filepath.Join(dir, "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (1, 'Il negozio'), (2, 'Oreficeria Esempio')`)
	eseguiSQL(t, d, `INSERT INTO profilo_negozio (nome, condizioni_garanzia, creato_il, aggiornato_il) VALUES ('Bottega di Prova', '', ?, ?)`,
		istanteHome, istanteHome)
	p := &provaEmailWeb{posta: &postinoFinto{}}
	b := buste.Nuove(d, o).ConPosta(uscita.Nuova(d, o, map[string]uscita.Provider{uscita.CanaleEmail: p.posta}), []byte("chiave-email-di-prova-di-32-byte"))
	reg := &bytes.Buffer{}
	dd := dipendenzeProva(&sondaFinta{pronto: true}, slog.New(slog.NewTextHandler(reg, nil)), accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti, dd.Buste, dd.Audit, dd.IndirizzoPubblico = o, ratelimit.Nuovo(o), b, audit.Nuovo(d, o), pubblicoEmail
	dd.Sottofondo = func(l func()) { p.lavori = append(p.lavori, l) }
	h, err := Pubblica(dd)
	if err != nil {
		t.Fatal(err)
	}
	p.provaCliente = provaCliente{h: h, d: d, b: b, o: o, reg: reg, dir: dir}
	return p
}

// fai fa girare i lavori in sottofondo tenuti da parte.
func (p *provaEmailWeb) fai() {
	for _, l := range p.lavori {
		l()
	}
	p.lavori = nil
}

// manda è il modulo del riquadro dalla pagina: prima la pagina (il token CSRF), poi il POST.
func mandaModulo(b *browser, pagina, azione string, campi url.Values) risposta {
	b.t.Helper()
	b.vai("GET", pagina, nil, nil)
	campi = cloneValues(campi)
	campi.Set("_csrf_token", b.token)
	return b.vai("POST", azione, campi, nil)
}

// senzaToken è la pagina senza il token CSRF, che cambia a ogni risposta (mascherato) e non dice niente.
func senzaToken(corpo string) string { return reTokenCSRF.ReplaceAllString(corpo, "") }

var (
	reTokenCSRF       = regexp.MustCompile(`<meta name="csrf-token" content="[^"]*">`)
	reLinkConfermaWeb = regexp.MustCompile(`/stato/email-conferma\?t=(\S+)`)
	reLinkStopWeb     = regexp.MustCompile(`/stato/email-disattiva\?t=(\S+)`)
	reRiquadro        = regexp.MustCompile(`(?s)<section id="public-email".*?</section>`)
)

func (p *provaEmailWeb) linkEmail(t *testing.T, re *regexp.Regexp) string {
	t.Helper()
	var testo string
	if err := p.d.Lettura.QueryRow(`SELECT testo FROM messaggi_in_uscita ORDER BY id DESC LIMIT 1`).Scan(&testo); err != nil {
		t.Fatal(err)
	}
	m := re.FindStringSubmatch(testo)
	if m == nil {
		t.Fatalf("nessun link in %q", testo)
	}
	return m[1]
}

// inventario: CLI-39
// Il riquadro della richiesta, con le parole dello standalone, il consenso, l'informativa e un modulo
// vero; solo con l'email disponibile (D-16).
func TestRiquadroEmailPagina(t *testing.T) {
	p := emailWebProva(t)
	c := p.busta(t, nil)
	b := nuovoBrowser(t, p.h)
	r := b.vai("GET", "/r/"+c.Token, nil, nil)
	riq := reRiquadro.FindString(r.corpo)
	contiene(t, "richiesta", riq, `<section id="public-email" class="cliente-email">`, "📧</span> Preferisci un avviso via email?",
		"Lascia la tua email: ti avviseremo quando la riparazione sarà pronta per il ritiro. Solo quell'avviso, niente pubblicità.",
		`<form id="public-email-form" action="/r/`+c.Token+`/email-optin" method="post" class="cliente-email-modulo">`+"\n"+
			`<input name="_csrf_token" type="hidden" value="`+b.token+`">`,
		`<input type="email" id="public-email-address" name="email" inputmode="email" autocomplete="email" placeholder="la-tua-email@esempio.it" maxlength="254" aria-label="La tua email">`,
		`<input type="checkbox" id="public-email-consent" name="consenso" value="1">`,
		`Acconsento a ricevere l'avviso via email per questa riparazione (<a href="/privacy">informativa privacy</a>)`,
		`<button type="submit" id="public-email-request" class="cliente-email-pulsante">Attiva avviso email →</button>`,
		`<p id="public-email-feedback" role="status" data-kind="info" class="cliente-email-esito"></p>`)
	// Il riquadro sta fra lo stato e le date, come nello standalone.
	if i, j, k := strings.Index(r.corpo, "Stato attuale"), strings.Index(r.corpo, `id="public-email"`), strings.Index(r.corpo, `class="cliente-date"`); !(i < j && j < k) {
		t.Error("posto del riquadro")
	}
	// D-16: email spenta, niente riquadro.
	p.posta.spento = true
	nonContiene(t, "spenta", b.vai("GET", "/r/"+c.Token, nil, nil).corpo, "public-email", "email-optin")
}

// inventario: CLI-39
// La richiesta: CSRF, redirect al riquadro con «in invio» una volta, il riquadro «Conferma la tua
// email» con la frase della consegna, «Reinvia conferma»; i rifiuti nel riquadro come errori.
func TestRichiestaEmailPagina(t *testing.T) {
	p := emailWebProva(t)
	c := p.busta(t, nil)
	b := nuovoBrowser(t, p.h)
	pagina := "/r/" + c.Token
	if r := b.vai("POST", pagina+"/email-optin", url.Values{"email": {"a@b.co"}, "consenso": {"1"}}, nil); r.codice != http.StatusForbidden {
		t.Fatalf("senza CSRF: %d", r.codice)
	}
	rifiuto := func(nome string, campi url.Values, frase string) {
		t.Helper()
		r := mandaModulo(b, pagina, pagina+"/email-optin", campi)
		if !redirectA(r, pagina+"#public-email") || r.h.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %q", nome, r.codice, r.location)
		}
		contiene(t, nome, b.vai("GET", pagina, nil, nil).corpo,
			`<p id="public-email-feedback" role="status" data-kind="error" class="cliente-email-esito cliente-email-errore">`+frase+`</p>`)
	}
	rifiuto("consenso", url.Values{"email": {"a@b.co"}}, "⚠️ Serve il consenso per attivare l&#39;avviso.")
	rifiuto("indirizzo", url.Values{"email": {"a@b"}, "consenso": {"1"}}, "⚠️ Controlla l&#39;indirizzo email.")
	rifiuto("reinvia", url.Values{"reinvia": {"1"}}, "⚠️ Nessun indirizzo in attesa di conferma.")
	if contaSQL(t, p.d, `SELECT count(*) FROM messaggi_in_uscita`) != 0 {
		t.Fatal("un rifiuto ha accodato")
	}
	r := mandaModulo(b, pagina, pagina+"/email-optin", url.Values{"email": {" A@B.co "}, "consenso": {"on"}})
	if !redirectA(r, pagina+"#public-email") {
		t.Fatalf("richiesta: %d %q", r.codice, r.location)
	}
	corpo := b.vai("GET", pagina, nil, nil).corpo
	contiene(t, "in attesa", reRiquadro.FindString(corpo), "📬</span> Conferma la tua email",
		"Stiamo inviando l'email di conferma a <b>a@b.co</b>: apri il link che contiene per attivare l'avviso. Non arriva? Controlla lo spam oppure",
		`<form id="public-email-resend-form" action="/r/`+c.Token+`/email-optin" method="post">`, `<input type="hidden" name="reinvia" value="1">`,
		`<button type="submit" id="public-email-resend" class="cliente-email-pulsante">Reinvia conferma</button>`,
		`data-kind="info" class="cliente-email-esito">Ti stiamo inviando l&#39;email di conferma: apri il link che contiene per attivare l&#39;avviso.</p>`)
	// L'esito si vede una volta.
	contiene(t, "una volta", b.vai("GET", pagina, nil, nil).corpo, `data-kind="info" class="cliente-email-esito"></p>`)
	for stato, frase := range map[string]string{
		"accepted": "Ti abbiamo scritto a <b>a@b.co</b>: apri il link nell'email per attivare l'avviso. Non è arrivata? Controlla lo spam oppure",
		"failed":   "Non siamo riusciti a inviare l'email di conferma a <b>a@b.co</b>. Controlla l'indirizzo oppure",
	} {
		eseguiSQL(t, p.d, `UPDATE messaggi_in_uscita SET stato = ?`, stato)
		contiene(t, stato, b.vai("GET", pagina, nil, nil).corpo, frase)
	}
	r = mandaModulo(b, pagina, pagina+"/email-optin", url.Values{"reinvia": {"1"}})
	if !redirectA(r, pagina+"#public-email") {
		t.Fatalf("reinvia: %d", r.codice)
	}
	contiene(t, "reinvia", b.vai("GET", pagina, nil, nil).corpo, ">✉️ Email di conferma rimessa in invio.</p>")
	if contaSQL(t, p.d, `SELECT count(*) FROM messaggi_in_uscita WHERE indirizzo = 'a@b.co'`) != 2 {
		t.Error("email accodate")
	}
	// Una coda che non accoda: l'email non è partita.
	eseguiSQL(t, p.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, '{sconosciuto} {link_conferma}', ?)`,
		buste.FamEmailConfermaTesto.Chiave(), istanteHome)
	p.o.Sposta(time.Second) // la richiesta dopo le email di prima
	rifiuto("non accodata", url.Values{"reinvia": {"1"}}, "⚠️ L&#39;invio dell&#39;email non è al momento disponibile. Riprova più tardi.")
	contiene(t, "non partita", b.vai("GET", pagina, nil, nil).corpo,
		"L'email di conferma per <b>a@b.co</b> non è partita: l'invio non è al momento disponibile. Riprova più tardi con")
	// Il limite: sei all'ora.
	eseguiSQL(t, p.d, `DELETE FROM impostazioni`)
	for range 3 {
		mandaModulo(b, pagina, pagina+"/email-optin", url.Values{"reinvia": {"1"}})
	}
	rifiuto("limite", url.Values{"reinvia": {"1"}}, "⚠️ Troppe richieste nell&#39;ultima ora: riprova più tardi o contatta il negozio.")
}

// inventario: CLI-39
// Busta chiusa: niente riquadro e il post torna in cima alla pagina con la frase dello standalone; link
// morto: il 404 della pagina, che il limite conta; D-16: una pagina disegnata prima che l'email si
// spegnesse torna in cima con «non disponibile», niente scritto.
func TestRichiestaEmailPaginaRifiuti(t *testing.T) {
	p := emailWebProva(t)
	c := p.busta(t, nil)
	b := nuovoBrowser(t, p.h)
	pagina := "/r/" + c.Token
	b.vai("GET", pagina, nil, nil)
	p.posta.spento = true
	r := b.vai("POST", pagina+"/email-optin", url.Values{"_csrf_token": {b.token}, "email": {"a@b.co"}, "consenso": {"1"}}, nil)
	if !redirectA(r, pagina) {
		t.Fatalf("spenta: %d %q", r.codice, r.location)
	}
	contiene(t, "spenta", b.vai("GET", pagina, nil, nil).corpo,
		`<div id="public-flash-error" class="cliente-esito cliente-esito-errore">⚠️ L&#39;invio dell&#39;email non è al momento disponibile. Riprova più tardi.</div>`)
	p.posta.spento = false
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, c.ID)
	corpo := b.vai("GET", pagina, nil, nil).corpo
	nonContiene(t, "chiusa", corpo, "public-email")
	r = b.vai("POST", pagina+"/email-optin", url.Values{"_csrf_token": {b.token}, "email": {"a@b.co"}, "consenso": {"1"}}, nil)
	if !redirectA(r, pagina) {
		t.Fatalf("chiusa: %d %q", r.codice, r.location)
	}
	contiene(t, "chiusa", b.vai("GET", pagina, nil, nil).corpo, `id="public-flash-error" class="cliente-esito cliente-esito-errore">Questa riparazione non riceve più avvisi.</div>`)
	for _, x := range []string{"/r/inventato/email-optin", "/r/inventato/email-optout"} {
		r := b.vai("POST", x, url.Values{"_csrf_token": {b.token}, "email": {"a@b.co"}, "consenso": {"1"}}, nil)
		if r.codice != http.StatusNotFound || r.corpo != "Riparazione non disponibile" {
			t.Errorf("%s: %d %q", x, r.codice, r.corpo)
		}
	}
	if contaSQL(t, p.d, `SELECT count(*) FROM buste WHERE email_cliente IS NOT NULL`) != 0 {
		t.Error("scritto un indirizzo")
	}
}

// inventario: CLI-40, CLI-41
// Il giro intero: il link di conferma apre la pagina col pulsante (niente scritto), il pulsante conferma e
// porta alla busta; il riquadro attivo con «Disattiva» e la domanda; «Disattiva» dalla pagina; il link
// nel piè apre la pagina col pulsante e il pulsante spegne.
func TestEmailConfermaEDisattiva(t *testing.T) {
	p := emailWebProva(t)
	c := p.busta(t, nil)
	b := nuovoBrowser(t, p.h)
	pagina := "/r/" + c.Token
	mandaModulo(b, pagina, pagina+"/email-optin", url.Values{"email": {"a@b.co"}, "consenso": {"1"}})
	tok := p.linkEmail(t, reLinkConfermaWeb)
	r := b.vai("GET", "/stato/email-conferma?t="+tok, nil, nil)
	if r.codice != http.StatusOK || r.h.Get("Cache-Control") != "no-store" || r.h.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("pagina di conferma: %d %v", r.codice, r.h)
	}
	contiene(t, "pagina di conferma", r.corpo, "<h1>Conferma la tua email</h1>",
		`<p id="email-action-message">Premi «Conferma» per ricevere l&#39;avviso a questo indirizzo quando la riparazione sarà pronta per il ritiro.</p>`,
		`<form id="email-action-form" action="/stato/email-conferma" method="post" class="email-azione-modulo">`,
		`<input type="hidden" name="t" value="`+tok+`">`, `<button type="submit" id="email-confirm-button" class="email-azione-pulsante">Conferma</button>`)
	if contaSQL(t, p.d, `SELECT count(*) FROM buste WHERE email_confermata_il IS NOT NULL`) != 0 {
		t.Fatal("aprire il link ha confermato")
	}
	if r := b.vai("POST", "/stato/email-conferma", url.Values{"t": {tok}}, nil); r.codice != http.StatusForbidden {
		t.Fatalf("conferma senza CSRF: %d", r.codice)
	}
	r = b.vai("POST", "/stato/email-conferma", url.Values{"_csrf_token": {b.token}, "t": {tok}}, nil)
	contiene(t, "confermata", r.corpo, "<h1>Email confermata ✅</h1>", "Ti avviseremo a questo indirizzo quando la riparazione sarà pronta per il ritiro.",
		`<a id="email-outcome-link" href="`+pubblicoEmail+`/r/`+c.Token+`" class="email-azione-pulsante">Torna alla tua riparazione</a>`)
	if r.codice != http.StatusOK || contaSQL(t, p.d, `SELECT count(*) FROM buste WHERE email_confermata_il IS NOT NULL`) != 1 {
		t.Fatalf("conferma: %d", r.codice)
	}
	// Un link già usato risponde subito la pagina della conferma.
	contiene(t, "già usato", b.vai("GET", "/stato/email-conferma?t="+tok, nil, nil).corpo, "<h1>Email confermata ✅</h1>")
	riq := reRiquadro.FindString(b.vai("GET", pagina, nil, nil).corpo)
	contiene(t, "attivo", riq, "✅</span> Avviso email attivo", "Ti scriveremo a <b>a@b.co</b> quando la riparazione sarà pronta.",
		`<form id="public-email-optout-form" action="/r/`+c.Token+`/email-optout" method="post">`,
		`<button type="submit" id="public-email-optout" data-confirm="Disattivare l'avviso email per questa riparazione?" class="cliente-email-secondario">Disattiva</button>`)
	nonContiene(t, "attivo", riq, "public-email-resend", "public-email-form")
	r = mandaModulo(b, pagina, pagina+"/email-optout", nil)
	if !redirectA(r, pagina+"#public-email") {
		t.Fatalf("disattiva: %d %q", r.codice, r.location)
	}
	corpo := b.vai("GET", pagina, nil, nil).corpo
	contiene(t, "disattivato", corpo, ">Avviso email disattivato.</p>", "Preferisci un avviso via email?")
	// Il link del piè: pagina con «Disattiva», poi il pulsante.
	mandaModulo(b, pagina, pagina+"/email-optin", url.Values{"email": {"a@b.co"}, "consenso": {"1"}})
	stop := p.linkEmail(t, reLinkStopWeb)
	r = b.vai("GET", "/stato/email-disattiva?t="+stop, nil, nil)
	contiene(t, "pagina di stop", r.corpo, "<h1>Disattivare gli avvisi email?</h1>", "Premi «Disattiva» per non ricevere più email per questa riparazione.",
		`action="/stato/email-disattiva"`, `<button type="submit" id="email-stop-button" class="email-azione-pulsante">Disattiva</button>`)
	if contaSQL(t, p.d, `SELECT count(*) FROM buste WHERE email_cliente IS NOT NULL`) != 1 {
		t.Fatal("aprire il link ha disattivato")
	}
	r = b.vai("POST", "/stato/email-disattiva", url.Values{"_csrf_token": {b.token}, "t": {stop}}, nil)
	contiene(t, "stop", r.corpo, "<h1>Avvisi email disattivati</h1>", "Non riceverai più email per questa riparazione.")
	if r.codice != http.StatusOK || contaSQL(t, p.d, `SELECT count(*) FROM buste WHERE email_cliente IS NOT NULL`) != 0 {
		t.Fatalf("stop: %d", r.codice)
	}
	nonContiene(t, "stop", r.corpo, "email-outcome-link")
}

// inventario: CLI-40, CLI-41
// I 410 dello standalone sulla pagina e sul pulsante; un vecchio link di conferma dello standalone è
// «non valido o scaduto» (D-02).
func TestEmailLinkRifiutati(t *testing.T) {
	p := emailWebProva(t)
	c := p.busta(t, nil)
	b := nuovoBrowser(t, p.h)
	pagina := "/r/" + c.Token
	mandaModulo(b, pagina, pagina+"/email-optin", url.Values{"email": {"a@b.co"}, "consenso": {"1"}})
	vecchio := p.linkEmail(t, reLinkConfermaWeb)
	p.o.Sposta(11 * time.Minute)
	mandaModulo(b, pagina, pagina+"/email-optin", url.Values{"email": {"b@b.co"}, "consenso": {"1"}})
	nuovo := p.linkEmail(t, reLinkConfermaWeb)
	eseguiSQL(t, p.d, `UPDATE buste SET email_cliente = 'c@b.co' WHERE id = ?`, c.ID)
	casi := []struct {
		metodo, t, titolo, frase string
	}{
		{"GET", "inventato", "Link non valido o scaduto", "Richiedi un nuovo link dalla pagina della tua riparazione (bottone «Reinvia conferma»)."},
		{"POST", "", "Link non valido o scaduto", "Richiedi un nuovo link"},
		{"GET", "SFMyNTY.g2gDdAAAAAJ.Xj3_k-9", "Link non valido o scaduto", "Richiedi un nuovo link"},
		{"POST", "SFMyNTY.g2gDdAAAAAJ.Xj3_k-9", "Link non valido o scaduto", "Richiedi un nuovo link"},
		{"GET", nuovo, "Link non più valido", "L&#39;indirizzo email di questa riparazione è cambiato."},
	}
	for _, x := range casi {
		var r risposta
		if x.metodo == "GET" {
			r = b.vai("GET", "/stato/email-conferma?t="+url.QueryEscape(x.t), nil, nil)
		} else {
			b.vai("GET", pagina, nil, nil)
			r = b.vai("POST", "/stato/email-conferma", url.Values{"_csrf_token": {b.token}, "t": {x.t}}, nil)
		}
		if r.codice != http.StatusGone || r.h.Get("Cache-Control") != "no-store" {
			t.Errorf("%s %q: %d", x.metodo, x.t, r.codice)
		}
		contiene(t, x.metodo+" "+x.t, r.corpo, "<h1>"+x.titolo+"</h1>", x.frase, "⚠️")
	}
	eseguiSQL(t, p.d, `UPDATE buste SET email_cliente = 'a@b.co' WHERE id = ?`, c.ID)
	contiene(t, "sostituito", b.vai("GET", "/stato/email-conferma?t="+vecchio, nil, nil).corpo, "<h1>Link non più valido</h1>",
		"Per questa riparazione è stata fatta una richiesta più recente: usa il link dell&#39;ultima email ricevuta.")
	for _, metodo := range []string{"GET", "POST"} {
		var r risposta
		if metodo == "GET" {
			r = b.vai("GET", "/stato/email-disattiva?t=inventato", nil, nil)
		} else {
			r = b.vai("POST", "/stato/email-disattiva", url.Values{"_csrf_token": {b.token}, "t": {"inventato"}}, nil)
		}
		if r.codice != http.StatusGone {
			t.Errorf("stop %s: %d", metodo, r.codice)
		}
		contiene(t, "stop "+metodo, r.corpo, "<h1>Link non valido</h1>", "Il link di disattivazione non è valido.")
	}
	if r := b.vai("POST", "/stato/email-disattiva", url.Values{"t": {"inventato"}}, nil); r.codice != http.StatusForbidden {
		t.Errorf("stop senza CSRF: %d", r.codice)
	}
	// /stato/email-… non conta nel limite dei tentativi (due segmenti).
	for range 12 {
		b.vai("GET", "/stato/email-conferma?t=x", nil, nil)
	}
	if r := b.vai("GET", pagina, nil, nil); r.codice != http.StatusOK {
		t.Errorf("dopo i 410: %d", r.codice)
	}
}

// inventario: CLI-41
// La pagina di ripiego della D-02: un vecchio link di disattivazione dello standalone apre il modulo
// «scrivi la tua email»; la risposta è la stessa pagina per un indirizzo iscritto, uno che non c'è e
// uno non valido, e il lavoro parte solo dopo la risposta; il link nuovo spegne davvero; oltre cinque
// invii in 15 minuti dallo stesso indirizzo IP la stessa risposta e niente lavoro.
func TestEmailRipiegoD02(t *testing.T) {
	p := emailWebProva(t)
	c := p.busta(t, nil)
	eseguiSQL(t, p.d, `UPDATE buste SET email_cliente = 'a@b.co', email_confermata_il = ? WHERE id = ?`, istanteHome, c.ID)
	b := nuovoBrowser(t, p.h)
	const vecchio = "SFMyNTY.g2gDdAAAAAJtAAAAAWE.Xj3_k-9AbC"
	for _, metodo := range []string{"GET", "POST"} {
		var r risposta
		if metodo == "GET" {
			r = b.vai("GET", "/stato/email-disattiva?t="+vecchio, nil, nil)
		} else {
			r = b.vai("POST", "/stato/email-disattiva", url.Values{"_csrf_token": {b.token}, "t": {vecchio}}, nil)
		}
		if r.codice != http.StatusOK {
			t.Fatalf("%s: %d", metodo, r.codice)
		}
		contiene(t, metodo, r.corpo, "<h1>Disattiva gli avvisi email</h1>", "Questo link è di una versione precedente del sistema e non vale più.",
			`<form id="email-action-form" action="/stato/email-ripiego" method="post" class="email-azione-modulo">`,
			`<input type="email" id="email-fallback-address" name="email"`, `id="email-fallback-button"`, `name="_csrf_token"`)
		nonContiene(t, metodo, r.corpo, `name="t"`, vecchio)
	}
	if contaSQL(t, p.d, `SELECT count(*) FROM buste WHERE email_cliente IS NOT NULL`) != 1 {
		t.Fatal("il vecchio link ha cambiato qualcosa")
	}
	manda := func(indirizzo string) risposta {
		t.Helper()
		return b.vai("POST", "/stato/email-ripiego", url.Values{"_csrf_token": {b.token}, "email": {indirizzo}}, nil)
	}
	iscritto := manda("A@B.co")
	if iscritto.codice != http.StatusOK || iscritto.h.Get("Cache-Control") != "no-store" {
		t.Fatalf("ripiego: %d", iscritto.codice)
	}
	contiene(t, "ripiego", iscritto.corpo, "<h1>Controlla la tua email</h1>",
		"Se l&#39;indirizzo che hai scritto riceve gli avvisi di una riparazione, tra poco ti arriva un&#39;email con il link per disattivarli.")
	// La risposta c'è prima del lavoro: niente è ancora in coda.
	if contaSQL(t, p.d, `SELECT count(*) FROM messaggi_in_uscita`) != 0 || len(p.lavori) != 1 {
		t.Fatalf("lavoro prima della risposta: %d lavori", len(p.lavori))
	}
	nessuno, nonValido := manda("nessuno@b.co"), manda("non valido")
	for nome, r := range map[string]risposta{"nessuno": nessuno, "non valido": nonValido} {
		if r.codice != iscritto.codice || senzaToken(r.corpo) != senzaToken(iscritto.corpo) {
			t.Errorf("%s: risposta diversa", nome)
		}
	}
	p.fai()
	if contaSQL(t, p.d, `SELECT count(*) FROM messaggi_in_uscita WHERE indirizzo = 'a@b.co'`) != 1 ||
		contaSQL(t, p.d, `SELECT count(*) FROM messaggi_in_uscita`) != 1 {
		t.Fatal("email del ripiego")
	}
	stop := p.linkEmail(t, reLinkStopWeb)
	r := b.vai("POST", "/stato/email-disattiva", url.Values{"_csrf_token": {b.token}, "t": {stop}}, nil)
	contiene(t, "link nuovo", r.corpo, "<h1>Avvisi email disattivati</h1>")
	if contaSQL(t, p.d, `SELECT count(*) FROM buste WHERE email_cliente IS NOT NULL`) != 0 {
		t.Error("il link nuovo non spegne")
	}
	// Due invii ancora passano (cinque in tutto), il sesto no: stessa risposta, nessun lavoro.
	manda("x@b.co")
	manda("y@b.co")
	if len(p.lavori) != 2 {
		t.Fatalf("lavori: %d", len(p.lavori))
	}
	sesto := manda("a@b.co")
	if len(p.lavori) != 2 || senzaToken(sesto.corpo) != senzaToken(iscritto.corpo) || sesto.codice != http.StatusOK {
		t.Errorf("sesto: %d lavori, %d", len(p.lavori), sesto.codice)
	}
	if r := b.vai("POST", "/stato/email-ripiego", url.Values{"email": {"a@b.co"}}, nil); r.codice != http.StatusForbidden {
		t.Errorf("senza CSRF: %d", r.codice)
	}
	if strings.Contains(p.reg.String(), "a@b.co") {
		t.Error("indirizzo nel registro del server")
	}
}

// inventario: CLI-19, CLI-17, CLI-39, CLI-41
// La pagina storica di una busta del cliente ha il riquadro, che manda la credenziale nei campi
// nascosti e torna alla pagina con la credenziale; quella di una busta conto terzi no, e il suo post è
// il 404 del link morto.
func TestRiquadroEmailPaginaStorica(t *testing.T) {
	p := emailWebProva(t)
	c := p.busta(t, nil)
	eseguiSQL(t, p.d, `UPDATE buste SET hash_codice_flask = ? WHERE id = ?`, firmeflask.ImprontaCodice(c.Numero, "segreto1"), c.ID)
	b := nuovoBrowser(t, p.h)
	base := "/stato/" + strings.Replace(c.Numero, "/", "/", 1)
	pagina := base + "?pw=segreto1"
	riq := reRiquadro.FindString(b.vai("GET", pagina, nil, nil).corpo)
	contiene(t, "storica", riq, `<form id="public-email-form" action="`+base+`/email-optin" method="post"`, `<input type="hidden" name="pw" value="segreto1">`)
	r := b.vai("POST", base+"/email-optin", url.Values{"_csrf_token": {b.token}, "pw": {"segreto1"}, "email": {"a@b.co"}, "consenso": {"1"}}, nil)
	if !redirectA(r, pagina+"#public-email") {
		t.Fatalf("richiesta storica: %d %q", r.codice, r.location)
	}
	contiene(t, "storica in attesa", b.vai("GET", pagina, nil, nil).corpo, "Conferma la tua email")
	eseguiSQL(t, p.d, `UPDATE buste SET email_confermata_il = ? WHERE id = ?`, istanteHome, c.ID)
	r = b.vai("POST", base+"/email-optout", url.Values{"_csrf_token": {b.token}, "pw": {"segreto1"}}, nil)
	if !redirectA(r, pagina+"#public-email") || contaSQL(t, p.d, `SELECT count(*) FROM buste WHERE email_cliente IS NOT NULL`) != 0 {
		t.Fatalf("disattiva storica: %d %q", r.codice, r.location)
	}
	for _, x := range []url.Values{{"pw": {"no"}}, {}} {
		x.Set("_csrf_token", b.token)
		x.Set("email", "a@b.co")
		x.Set("consenso", "1")
		if r := b.vai("POST", base+"/email-optin", x, nil); r.codice != http.StatusNotFound || r.corpo != "Riparazione non disponibile" {
			t.Errorf("credenziale %v: %d", x["pw"], r.codice)
		}
	}
	eseguiSQL(t, p.d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, c.ID)
	nonContiene(t, "conto terzi", b.vai("GET", pagina, nil, nil).corpo, "public-email")
	for _, azione := range []string{"/email-optin", "/email-optout"} {
		r := b.vai("POST", base+azione, url.Values{"_csrf_token": {b.token}, "pw": {"segreto1"}, "email": {"a@b.co"}, "consenso": {"1"}}, nil)
		if r.codice != http.StatusNotFound {
			t.Errorf("conto terzi %s: %d", azione, r.codice)
		}
	}
	if contaSQL(t, p.d, `SELECT count(*) FROM buste WHERE email_cliente IS NOT NULL`) != 0 {
		t.Error("scritto sulla busta conto terzi")
	}
}

// inventario: CLI-27, CLI-39
// Nell'anteprima del negozio il riquadro c'è, come lo vede chi ha il link, ma spento: nessun indirizzo
// né token nei suoi moduli, dentro il fieldset spento; senza l'email disponibile non c'è (D-16).
func TestRiquadroEmailAnteprima(t *testing.T) {
	p := facceAnteprima(t, pubblicoEmail)
	posta := &postinoFinto{}
	p.b.ConPosta(uscita.Nuova(p.d, p.o, map[string]uscita.Provider{uscita.CanaleEmail: posta}), []byte("chiave-email-di-prova-di-32-byte"))
	c := p.busta(t, nil)
	eseguiSQL(t, p.d, `UPDATE buste SET email_cliente = 'a@b.co', email_confermata_il = ? WHERE id = ?`, istanteHome, c.ID)
	b := titolareDentro(t, p.interna)
	corpo := b.vai("GET", percorsoAnteprima(c.ID), nil, nil).corpo
	riq := reRiquadro.FindString(corpo)
	contiene(t, "anteprima", riq, "Avviso email attivo", `<form id="public-email-optout-form" method="post">`, `id="public-email-optout"`)
	nonContiene(t, "anteprima", riq, "action=", "_csrf_token")
	if i, j := strings.Index(corpo, `<fieldset class="cliente-corpo space-y-7" disabled>`), strings.Index(corpo, `id="public-email"`); i < 0 || j < i {
		t.Error("riquadro fuori dal fieldset spento")
	}
	posta.spento = true
	nonContiene(t, "spenta", b.vai("GET", percorsoAnteprima(c.ID), nil, nil).corpo, "public-email")
}

// busteRipiego registra il contesto del lavoro del ripiego, o va in panico.
type busteRipiego struct {
	*buste.Buste
	scadenza time.Time
	conTetto bool
	panico   bool
}

func (b *busteRipiego) MandaLinkDisattivazione(ctx context.Context, indirizzo, base string, c audit.Contesto) (int, error) {
	if b.panico {
		panic(errors.New("guasto con a@b.co dentro"))
	}
	b.scadenza, b.conTetto = ctx.Deadline()
	return b.Buste.MandaLinkDisattivazione(ctx, indirizzo, base, c)
}

// inventario: CLI-41
// Il sottofondo predefinito torna subito (il lavoro gira dopo la risposta).
func TestSottofondoPredefinito(t *testing.T) {
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nil, nil)
	f, err := nuovaFaccia(dd, "pubblica")
	if err != nil {
		t.Fatal(err)
	}
	ferma, fatto := make(chan struct{}), make(chan struct{})
	tornato := make(chan struct{})
	go func() {
		f.sottofondo(func() { <-ferma; close(fatto) })
		close(tornato)
	}()
	select {
	case <-tornato:
	case <-time.After(5 * time.Second):
		t.Fatal("il sottofondo aspetta il lavoro")
	}
	close(ferma)
	select {
	case <-fatto:
	case <-time.After(5 * time.Second):
		t.Fatal("il lavoro non è partito")
	}
}

// inventario: CLI-41
// Il lavoro del ripiego: al più 2 minuti; un panico resta nel lavoro e nel registro va una frase fissa
// con il tipo (mai il valore); con quattro lavori in attesa il quinto si scarta, con la stessa risposta.
func TestRipiegoLavoroProtetto(t *testing.T) {
	p := emailWebProva(t)
	c := p.busta(t, nil)
	eseguiSQL(t, p.d, `UPDATE buste SET email_cliente = 'a@b.co' WHERE id = ?`, c.ID)
	br := &busteRipiego{Buste: p.b}
	dd := dipendenzeProva(&sondaFinta{pronto: true}, slog.New(slog.NewTextHandler(p.reg, nil)), nil)
	dd.Orologio, dd.Limiti, dd.Buste, dd.Audit, dd.IndirizzoPubblico = p.o, ratelimit.Nuovo(p.o), br, audit.Nuovo(p.d, p.o), pubblicoEmail
	dd.Sottofondo = func(l func()) { p.lavori = append(p.lavori, l) }
	h, err := Pubblica(dd)
	if err != nil {
		t.Fatal(err)
	}
	b := nuovoBrowser(t, h)
	b.vai("GET", "/stato/email-disattiva?t=SFMyNTY.abc.def", nil, nil)
	manda := func() risposta {
		return b.vai("POST", "/stato/email-ripiego", url.Values{"_csrf_token": {b.token}, "email": {"a@b.co"}}, nil)
	}
	primo := manda()
	prima := time.Now()
	p.fai()
	if !br.conTetto || br.scadenza.Before(prima.Add(2*time.Minute-10*time.Second)) || br.scadenza.After(time.Now().Add(2*time.Minute)) {
		t.Errorf("tetto: %v %v", br.conTetto, br.scadenza)
	}
	br.panico = true
	manda()
	p.fai() // non deve propagare il panico
	if !strings.Contains(p.reg.String(), "lavoro del ripiego andato in panico") || !strings.Contains(p.reg.String(), "*errors.errorString") ||
		strings.Contains(p.reg.String(), "a@b.co") {
		t.Errorf("registro: %s", p.reg.String())
	}
	// Quattro posti: i lavori tenuti da parte li occupano, il quinto si scarta.
	for range 3 {
		manda()
	}
	if len(p.lavori) != 3 {
		t.Fatalf("lavori: %d", len(p.lavori))
	}
	// Il limite per indirizzo IP è a 5: un altro indirizzo IP per arrivare al quinto posto.
	b.ip = "198.51.100.77"
	manda()
	quinto := b.vai("POST", "/stato/email-ripiego", url.Values{"_csrf_token": {b.token}, "email": {"a@b.co"}}, nil)
	if len(p.lavori) != 4 || senzaToken(quinto.corpo) != senzaToken(primo.corpo) || quinto.codice != http.StatusOK {
		t.Errorf("quinto: %d lavori, %d", len(p.lavori), quinto.codice)
	}
	br.panico = false
	p.fai()
	manda()
	if len(p.lavori) != 1 {
		t.Errorf("posti non liberati: %d", len(p.lavori))
	}
}

// inventario: CLI-27, CLI-39
// Nell'anteprima nessun modulo del riquadro ha il token CSRF o un indirizzo, in nessuno stato.
func TestRiquadroEmailAnteprimaStati(t *testing.T) {
	p := facceAnteprima(t, pubblicoEmail)
	p.b.ConPosta(uscita.Nuova(p.d, p.o, map[string]uscita.Provider{uscita.CanaleEmail: &postinoFinto{}}), []byte("chiave-email-di-prova-di-32-byte"))
	c := p.busta(t, nil)
	b := titolareDentro(t, p.interna)
	for stato, q := range map[string]string{
		"richiesta": `UPDATE buste SET email_cliente = NULL WHERE id = ?`,
		"in attesa": `UPDATE buste SET email_cliente = 'a@b.co', email_confermata_il = NULL WHERE id = ?`,
	} {
		eseguiSQL(t, p.d, q, c.ID)
		riq := reRiquadro.FindString(b.vai("GET", percorsoAnteprima(c.ID), nil, nil).corpo)
		if riq == "" {
			t.Fatalf("%s: niente riquadro", stato)
		}
		nonContiene(t, stato, riq, "_csrf_token", "action=")
	}
}
