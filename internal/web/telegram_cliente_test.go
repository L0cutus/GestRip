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

// Il riquadro Telegram della pagina del cliente (CLI-44), il link dei messaggi WhatsApp (CLI-45) e le due
// azioni dalla pagina storica (CLI-19), come public_repair_telegram_test.exs e
// public_repair_telegram_link_test.exs dello standalone (go-baseline), con la regola del canale disponibile
// del Go (D-16 come per l'email).

const botWebProva = "bottega_prova_bot"

// bottinoFinto è il provider finto di Telegram: il canale (invio acceso e bot configurato) acceso finché
// spento è falso.
type bottinoFinto struct{ spento bool }

func (p *bottinoFinto) Consegna(context.Context, uscita.Messaggio) (string, error) { return "1", nil }
func (p *bottinoFinto) Attivo(context.Context) (bool, error)                       { return !p.spento, nil }

type provaTelegramWeb struct {
	provaCliente
	posta *postinoFinto
	bot   *bottinoFinto
}

// telegramWebProva è la faccia pubblica con email e Telegram disponibili: le due code accese, le notifiche
// ai clienti accese, il nome del bot e l'indirizzo pubblico.
func telegramWebProva(t *testing.T) *provaTelegramWeb {
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
	eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, '1', ?), (?, ?, ?)`,
		uscita.ChiaveNotificheClienti, istanteHome, uscita.ChiaveNomeBot, botWebProva, istanteHome)
	p := &provaTelegramWeb{posta: &postinoFinto{}, bot: &bottinoFinto{}}
	coda := uscita.Nuova(d, o, map[string]uscita.Provider{uscita.CanaleEmail: p.posta, uscita.CanaleTelegram: p.bot})
	b := buste.Nuove(d, o).ConPosta(coda, []byte("chiave-email-di-prova-di-32-byte")).ConIndirizzoPubblico(pubblicoEmail)
	reg := &bytes.Buffer{}
	dd := dipendenzeProva(&sondaFinta{pronto: true}, slog.New(slog.NewTextHandler(reg, nil)), accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti, dd.Buste, dd.Audit, dd.IndirizzoPubblico = o, ratelimit.Nuovo(o), b, audit.Nuovo(d, o), pubblicoEmail
	h, err := Pubblica(dd)
	if err != nil {
		t.Fatal(err)
	}
	p.provaCliente = provaCliente{h: h, d: d, b: b, o: o, reg: reg, dir: dir}
	return p
}

var (
	reRiquadroTelegram = regexp.MustCompile(`(?s)<section id="public-telegram".*?</section>`)
	reTMe              = regexp.MustCompile(`\Ahttps://t\.me/` + botWebProva + `\?start=[A-Za-z0-9_-]{32}\z`)
)

// iscrittaWeb mette un'iscrizione con il consenso della chat alla busta.
func iscrittaWeb(t *testing.T, d *db.DB, busta int64) {
	t.Helper()
	eseguiSQL(t, d, `INSERT INTO iscrizioni_telegram (busta_id, chat_id, consenso, collegata_il, creato_il, aggiornato_il)
		VALUES (?, 4503599627370497, 1, ?, ?, ?)`, busta, istanteHome, istanteHome, istanteHome)
}

// inventario: CLI-44
// Stato A con le parole dello standalone e un modulo POST con il CSRF, sopra il riquadro email; stato B
// con «Modifica preferenze» e «Disattiva» dopo il consenso. Nessun riquadro con il canale non disponibile,
// sulla busta conto terzi né su quella chiusa.
func TestRiquadroTelegramPagina(t *testing.T) {
	p := telegramWebProva(t)
	c := p.busta(t, nil)
	b := nuovoBrowser(t, p.h)
	corpo := b.vai("GET", "/r/"+c.Token, nil, nil).corpo
	riq := reRiquadroTelegram.FindString(corpo)
	contiene(t, "stato A", riq, "Vuoi essere avvisato in tempo reale?",
		"Attiva le notifiche Telegram: ti avviseremo non appena la riparazione cambia stato o ti scriviamo un messaggio. Gratuito, disattivabile in qualsiasi momento con <code>/stop</code>.",
		`<form id="public-telegram-form" action="/r/`+c.Token+`/telegram-optin" method="post">`, `<input name="_csrf_token" type="hidden" value="`,
		`id="public-telegram-optin"`, "Attiva su Telegram →", `id="public-telegram-feedback"`)
	nonContiene(t, "stato A", riq, "public-telegram-optout", "<script")
	if i, j := strings.Index(corpo, `id="public-telegram"`), strings.Index(corpo, `id="public-email"`); i < 0 || j < 0 || i > j {
		t.Errorf("il riquadro Telegram non sta sopra quello email: %d %d", i, j)
	}
	iscrittaWeb(t, p.d, c.ID)
	riq = reRiquadroTelegram.FindString(b.vai("GET", "/r/"+c.Token, nil, nil).corpo)
	contiene(t, "stato B", riq, "Notifiche Telegram attive", "Ti avviseremo qui non appena ci sono aggiornamenti.", "⚙️ Modifica preferenze",
		`<form id="public-telegram-form" action="/r/`+c.Token+`/telegram-optin" method="post">`,
		`<form id="public-telegram-optout-form" action="/r/`+c.Token+`/telegram-optout" method="post">`,
		`data-confirm="Disattivare le notifiche Telegram per questa riparazione?"`)
	if strings.Count(riq, `name="_csrf_token"`) != 2 {
		t.Error("stato B: i due moduli senza CSRF")
	}
	nonContiene(t, "stato B", riq, "Vuoi essere avvisato")
	p.bot.spento = true
	nonContiene(t, "invio spento", b.vai("GET", "/r/"+c.Token, nil, nil).corpo, "public-telegram")
	p.bot.spento = false
	eseguiSQL(t, p.d, `UPDATE impostazioni SET valore = '' WHERE chiave = ?`, uscita.ChiaveNomeBot)
	nonContiene(t, "nome del bot ignoto", b.vai("GET", "/r/"+c.Token, nil, nil).corpo, "public-telegram")
	eseguiSQL(t, p.d, `UPDATE impostazioni SET valore = ? WHERE chiave = ?`, botWebProva, uscita.ChiaveNomeBot)
	oref := p.busta(t, func(a *buste.Accettazione) { a.OreficeriaID = "2"; a.CodiceQR = "RIP-2026-0046" })
	nonContiene(t, "conto terzi", b.vai("GET", "/r/"+oref.Token, nil, nil).corpo, "public-telegram")
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'consegnata', data_consegna = '2026-10-31' WHERE id = ?`, c.ID)
	nonContiene(t, "chiusa", b.vai("GET", "/r/"+c.Token, nil, nil).corpo, "public-telegram")
}

// inventario: CLI-44
// Il pulsante: un 302 verso t.me con un token nuovo e no-store; la settima pressione dell'ora torna al
// riquadro con «Troppe richieste»; la busta chiusa torna alla pagina con la frase e senza riquadro; link
// morto e busta conto terzi il 404 della pagina; il canale spento (una pagina disegnata prima) «Servizio
// non disponibile» in cima. Senza CSRF niente.
func TestIscrizioneTelegramPagina(t *testing.T) {
	p := telegramWebProva(t)
	c := p.busta(t, nil)
	b := nuovoBrowser(t, p.h)
	pagina := "/r/" + c.Token
	if r := b.vai("POST", pagina+"/telegram-optin", url.Values{}, nil); r.codice != http.StatusForbidden {
		t.Errorf("senza CSRF: %d", r.codice)
	}
	r := mandaModulo(b, pagina, pagina+"/telegram-optin", url.Values{})
	if r.codice != http.StatusFound || !reTMe.MatchString(r.location) || r.h.Get("Cache-Control") != "no-store" {
		t.Fatalf("pulsante: %d %q %v", r.codice, r.location, r.h)
	}
	if contaSQL(t, p.d, `SELECT count(*) FROM token_telegram WHERE busta_id = ?`, c.ID) != 1 {
		t.Error("nessun token")
	}
	for range 5 {
		mandaModulo(b, pagina, pagina+"/telegram-optin", url.Values{})
	}
	r = mandaModulo(b, pagina, pagina+"/telegram-optin", url.Values{})
	if !redirectA(r, pagina+"#public-telegram") {
		t.Fatalf("settima: %d %q", r.codice, r.location)
	}
	contiene(t, "settima", reRiquadroTelegram.FindString(b.vai("GET", r.location, nil, nil).corpo),
		`<p id="public-telegram-feedback" role="status" data-kind="error"`, "⚠️ Troppe richieste nell&#39;ultima ora: riprova più tardi o contatta il negozio.")
	p.bot.spento = true
	r = mandaModulo(b, pagina, pagina+"/telegram-optin", url.Values{})
	if !redirectA(r, pagina) {
		t.Fatalf("spento: %d %q", r.codice, r.location)
	}
	contiene(t, "spento", b.vai("GET", r.location, nil, nil).corpo, `<div id="public-flash-error" class="cliente-esito cliente-esito-errore">⚠️ Servizio non disponibile. Riprova più tardi.</div>`)
	p.bot.spento = false
	altra := p.busta(t, func(a *buste.Accettazione) { a.CodiceQR = "RIP-2026-0047" })
	b.vai("GET", "/r/"+altra.Token, nil, nil)
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'consegnata', data_consegna = '2026-10-31' WHERE id = ?`, altra.ID)
	r = b.vai("POST", "/r/"+altra.Token+"/telegram-optin", url.Values{"_csrf_token": {b.token}}, nil)
	if !redirectA(r, "/r/"+altra.Token) {
		t.Fatalf("chiusa: %d %q", r.codice, r.location)
	}
	corpo := b.vai("GET", r.location, nil, nil).corpo
	contiene(t, "chiusa", corpo, `<div id="public-flash-error" class="cliente-esito cliente-esito-errore">Questa riparazione non riceve più avvisi.</div>`)
	nonContiene(t, "chiusa", corpo, "public-telegram")
	oref := p.busta(t, func(a *buste.Accettazione) { a.OreficeriaID = "2"; a.CodiceQR = "RIP-2026-0046" })
	for nome, tok := range map[string]string{"conto terzi": oref.Token, "link morto": "QUESTO-TOKEN-NON-ESISTE-0000000000000000000"} {
		if r := b.vai("POST", "/r/"+tok+"/telegram-optin", url.Values{"_csrf_token": {b.token}}, nil); r.codice != http.StatusNotFound || r.corpo != "Riparazione non disponibile" {
			t.Errorf("%s: %d %q", nome, r.codice, r.corpo)
		}
	}
	if n := contaSQL(t, p.d, `SELECT count(*) FROM token_telegram`); n != 6 {
		t.Errorf("token: %d", n)
	}
}

// inventario: CLI-44
// «Disattiva» spegne l'iscrizione e torna al riquadro nello stato A con «Notifiche Telegram disattivate.»;
// sulla busta conto terzi è il 404.
func TestDisattivaTelegramPagina(t *testing.T) {
	p := telegramWebProva(t)
	c := p.busta(t, nil)
	iscrittaWeb(t, p.d, c.ID)
	b := nuovoBrowser(t, p.h)
	pagina := "/r/" + c.Token
	r := mandaModulo(b, pagina, pagina+"/telegram-optout", url.Values{})
	if !redirectA(r, pagina+"#public-telegram") {
		t.Fatalf("disattiva: %d %q", r.codice, r.location)
	}
	riq := reRiquadroTelegram.FindString(b.vai("GET", r.location, nil, nil).corpo)
	contiene(t, "dopo", riq, "Vuoi essere avvisato in tempo reale?", `data-kind="info"`, "Notifiche Telegram disattivate.")
	if contaSQL(t, p.d, `SELECT count(*) FROM iscrizioni_telegram WHERE consenso = 0 AND motivo_fermata = 'page' AND fermata_il IS NOT NULL`) != 1 {
		t.Error("iscrizione non spenta")
	}
	oref := p.busta(t, func(a *buste.Accettazione) { a.OreficeriaID = "2"; a.CodiceQR = "RIP-2026-0046" })
	if r := b.vai("POST", "/r/"+oref.Token+"/telegram-optout", url.Values{"_csrf_token": {b.token}}, nil); r.codice != http.StatusNotFound {
		t.Errorf("conto terzi: %d", r.codice)
	}
}

// inventario: CLI-45, CLI-32
// Il link dei messaggi WhatsApp: ogni apertura un 302 verso t.me con un token nuovo, no-store e
// no-referrer; la settima dell'ora il 429 in testo semplice, mentre il pulsante della pagina funziona
// ancora; canale spento o nome del bot ignoto il 503 dello standalone, senza token; link morto, busta
// conto terzi (anche chiusa) il 404; busta diretta chiusa la sua pagina con la frase.
func TestLinkTelegramDaWhatsApp(t *testing.T) {
	p := telegramWebProva(t)
	c := p.busta(t, nil)
	b := nuovoBrowser(t, p.h)
	link := "/r/" + c.Token + "/telegram"
	uno, due := b.vai("GET", link, nil, nil), b.vai("GET", link, nil, nil)
	for _, r := range []risposta{uno, due} {
		if r.codice != http.StatusFound || !reTMe.MatchString(r.location) || r.h.Get("Cache-Control") != "no-store" ||
			r.h.Get("Referrer-Policy") != "no-referrer" {
			t.Fatalf("apertura: %d %q %v", r.codice, r.location, r.h)
		}
	}
	if uno.location == due.location {
		t.Error("due aperture, lo stesso token")
	}
	for range 4 {
		b.vai("GET", link, nil, nil)
	}
	r := b.vai("GET", link, nil, nil)
	if r.codice != http.StatusTooManyRequests || r.corpo != "Troppe richieste nell'ultima ora: riprova più tardi o contatta il negozio." ||
		r.h.Get("Content-Type") != "text/plain; charset=utf-8" || r.h.Get("Cache-Control") != "no-store" {
		t.Errorf("settima: %d %q %v", r.codice, r.corpo, r.h)
	}
	if contaSQL(t, p.d, `SELECT count(*) FROM token_telegram`) != 6 || contaSQL(t, p.d, `SELECT count(*) FROM iscrizioni_telegram`) != 0 {
		t.Error("token o iscrizioni sbagliati")
	}
	if r := mandaModulo(b, "/r/"+c.Token, "/r/"+c.Token+"/telegram-optin", url.Values{}); r.codice != http.StatusFound || !reTMe.MatchString(r.location) {
		t.Errorf("il pulsante dopo i sei link: %d %q", r.codice, r.location)
	}
	altra := p.busta(t, func(a *buste.Accettazione) { a.CodiceQR = "RIP-2026-0048" })
	spento := func(nome string) {
		t.Helper()
		r := b.vai("GET", "/r/"+altra.Token+"/telegram", nil, nil)
		if r.codice != http.StatusServiceUnavailable || r.corpo != "Servizio notifiche temporaneamente non disponibile." ||
			r.h.Get("Content-Type") != "text/plain; charset=utf-8" {
			t.Errorf("%s: %d %q", nome, r.codice, r.corpo)
		}
	}
	p.bot.spento = true
	spento("invio spento")
	p.bot.spento = false
	eseguiSQL(t, p.d, `UPDATE impostazioni SET valore = '' WHERE chiave = ?`, uscita.ChiaveNotificheClienti)
	spento("notifiche spente")
	eseguiSQL(t, p.d, `UPDATE impostazioni SET valore = '1' WHERE chiave = ?`, uscita.ChiaveNotificheClienti)
	eseguiSQL(t, p.d, `DELETE FROM impostazioni WHERE chiave = ?`, uscita.ChiaveNomeBot)
	spento("nome del bot ignoto")
	if contaSQL(t, p.d, `SELECT count(*) FROM token_telegram WHERE busta_id = ?`, altra.ID) != 0 {
		t.Error("token a canale spento")
	}
	eseguiSQL(t, p.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, ?)`, uscita.ChiaveNomeBot, botWebProva, istanteHome)
	oref := p.busta(t, func(a *buste.Accettazione) { a.OreficeriaID = "2"; a.CodiceQR = "RIP-2026-0046" })
	orefChiusa := p.busta(t, func(a *buste.Accettazione) { a.OreficeriaID = "2"; a.CodiceQR = "RIP-2026-0049" })
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'consegnata', data_consegna = '2026-10-31' WHERE id = ?`, orefChiusa.ID)
	for nome, tok := range map[string]string{"conto terzi": oref.Token, "conto terzi chiusa": orefChiusa.Token, "link morto": "QUESTO-TOKEN-NON-ESISTE-0000000000000000000"} {
		if r := b.vai("GET", "/r/"+tok+"/telegram", nil, nil); r.codice != http.StatusNotFound || r.corpo != "Riparazione non disponibile" {
			t.Errorf("%s: %d %q", nome, r.codice, r.corpo)
		}
	}
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'consegnata', data_consegna = '2026-10-31' WHERE id = ?`, altra.ID)
	r = b.vai("GET", "/r/"+altra.Token+"/telegram", nil, nil)
	if !redirectA(r, "/r/"+altra.Token) {
		t.Fatalf("chiusa: %d %q", r.codice, r.location)
	}
	contiene(t, "chiusa", b.vai("GET", r.location, nil, nil).corpo, "Questa riparazione non riceve più avvisi.")
}

// inventario: CLI-19, CLI-44
// La pagina storica di una busta del cliente ha il riquadro, che manda la credenziale nei campi nascosti;
// il pulsante va a t.me, «Disattiva» torna alla pagina con la credenziale; un codice sbagliato e la busta
// conto terzi sono il 404 senza niente scritto.
func TestRiquadroTelegramPaginaStorica(t *testing.T) {
	p := telegramWebProva(t)
	c := p.busta(t, nil)
	eseguiSQL(t, p.d, `UPDATE buste SET hash_codice_flask = ? WHERE id = ?`, firmeflask.ImprontaCodice(c.Numero, "segreto1"), c.ID)
	b := nuovoBrowser(t, p.h)
	base := "/stato/" + c.Numero
	pagina := base + "?pw=segreto1"
	riq := reRiquadroTelegram.FindString(b.vai("GET", pagina, nil, nil).corpo)
	contiene(t, "storica", riq, `<form id="public-telegram-form" action="`+base+`/telegram-optin" method="post">`, `<input type="hidden" name="pw" value="segreto1">`)
	r := b.vai("POST", base+"/telegram-optin", url.Values{"_csrf_token": {b.token}, "pw": {"segreto1"}}, nil)
	if r.codice != http.StatusFound || !reTMe.MatchString(r.location) {
		t.Fatalf("pulsante storico: %d %q", r.codice, r.location)
	}
	iscrittaWeb(t, p.d, c.ID)
	corpo := b.vai("GET", pagina, nil, nil).corpo
	// I due moduli dello stato B rimandano la credenziale (review della 072): si mandano come la pagina li
	// ha scritti.
	azione, campi := moduloDellaPagina(t, corpo, "public-telegram-form")
	if azione != base+"/telegram-optin" || campi.Get("pw") != "segreto1" {
		t.Errorf("«Modifica preferenze» storico: %q %v", azione, campi)
	}
	if r := b.vai("POST", azione, campi, nil); r.codice != http.StatusFound || !reTMe.MatchString(r.location) {
		t.Errorf("«Modifica preferenze» storico: %d %q", r.codice, r.location)
	}
	azione, campi = moduloDellaPagina(t, corpo, "public-telegram-optout-form")
	if azione != base+"/telegram-optout" || campi.Get("pw") != "segreto1" {
		t.Errorf("«Disattiva» storico: %q %v", azione, campi)
	}
	r = b.vai("POST", azione, campi, nil)
	if !redirectA(r, pagina+"#public-telegram") || contaSQL(t, p.d, `SELECT count(*) FROM iscrizioni_telegram WHERE fermata_il IS NULL`) != 0 {
		t.Fatalf("disattiva storica: %d %q", r.codice, r.location)
	}
	for _, azione := range []string{"/telegram-optin", "/telegram-optout"} {
		if r := b.vai("POST", base+azione, url.Values{"_csrf_token": {b.token}, "pw": {"no"}}, nil); r.codice != http.StatusNotFound || r.corpo != "Riparazione non disponibile" {
			t.Errorf("codice sbagliato %s: %d", azione, r.codice)
		}
	}
	if contaSQL(t, p.d, `SELECT count(*) FROM token_telegram`) != 2 {
		t.Error("token dopo un codice sbagliato")
	}
	// Passata a un'oreficeria: il trigger della 0014 toglie i token della busta.
	eseguiSQL(t, p.d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, c.ID)
	nonContiene(t, "conto terzi", b.vai("GET", pagina, nil, nil).corpo, "public-telegram")
	for _, azione := range []string{"/telegram-optin", "/telegram-optout"} {
		if r := b.vai("POST", base+azione, url.Values{"_csrf_token": {b.token}, "pw": {"segreto1"}}, nil); r.codice != http.StatusNotFound {
			t.Errorf("conto terzi %s: %d", azione, r.codice)
		}
	}
	if contaSQL(t, p.d, `SELECT count(*) FROM token_telegram`) != 0 {
		t.Error("token sulla busta conto terzi")
	}
}

// inventario: CLI-27, CLI-44
// Nell'anteprima del negozio il riquadro c'è, come lo vede chi ha il link, ma spento: nessun indirizzo
// né token nei suoi moduli, dentro il fieldset spento; con il canale non disponibile non c'è.
func TestRiquadroTelegramAnteprima(t *testing.T) {
	p := facceAnteprima(t, pubblicoEmail)
	bot := &bottinoFinto{}
	p.b.ConPosta(uscita.Nuova(p.d, p.o, map[string]uscita.Provider{uscita.CanaleTelegram: bot}), []byte("chiave-email-di-prova-di-32-byte"))
	eseguiSQL(t, p.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, '1', ?), (?, ?, ?)`,
		uscita.ChiaveNotificheClienti, istanteHome, uscita.ChiaveNomeBot, botWebProva, istanteHome)
	c := p.busta(t, nil)
	iscrittaWeb(t, p.d, c.ID)
	b := titolareDentro(t, p.interna)
	corpo := b.vai("GET", percorsoAnteprima(c.ID), nil, nil).corpo
	riq := reRiquadroTelegram.FindString(corpo)
	contiene(t, "anteprima", riq, "Notifiche Telegram attive", `<form id="public-telegram-form" method="post">`, `<form id="public-telegram-optout-form" method="post">`)
	nonContiene(t, "anteprima", riq, "action=", "_csrf_token")
	if i, j := strings.Index(corpo, `<fieldset class="cliente-corpo space-y-7" disabled>`), strings.Index(corpo, `id="public-telegram"`); i < 0 || j < i {
		t.Error("riquadro fuori dal fieldset spento")
	}
	bot.spento = true
	nonContiene(t, "spento", b.vai("GET", percorsoAnteprima(c.ID), nil, nil).corpo, "public-telegram")
}

var (
	reModuloPagina  = regexp.MustCompile(`(?s)<form id="([^"]+)"([^>]*)>(.*?)</form>`)
	reAzione        = regexp.MustCompile(` action="([^"]*)"`)
	reCampoNascosto = regexp.MustCompile(`<input (?:name="([^"]*)" type="hidden" value="([^"]*)"|type="hidden" name="([^"]*)" value="([^"]*)")>`)
)

// moduloDellaPagina è il modulo con quell'id come la pagina l'ha scritto: l'indirizzo e i campi nascosti.
func moduloDellaPagina(t *testing.T, corpo, id string) (string, url.Values) {
	t.Helper()
	for _, m := range reModuloPagina.FindAllStringSubmatch(corpo, -1) {
		if m[1] != id {
			continue
		}
		var azione string
		if a := reAzione.FindStringSubmatch(m[2]); a != nil {
			azione = a[1]
		}
		v := url.Values{}
		for _, c := range reCampoNascosto.FindAllStringSubmatch(m[3], -1) {
			if c[1] != "" {
				v.Add(c[1], c[2])
			} else {
				v.Add(c[3], c[4])
			}
		}
		return azione, v
	}
	t.Fatalf("nessun modulo %s", id)
	return "", nil
}

// inventario: CLI-19, CLI-44
// La settima pressione dell'ora dalla pagina storica torna al riquadro con la credenziale e la frase del
// limite (review della 072).
func TestLimiteTelegramPaginaStorica(t *testing.T) {
	p := telegramWebProva(t)
	c := p.busta(t, nil)
	eseguiSQL(t, p.d, `UPDATE buste SET hash_codice_flask = ? WHERE id = ?`, firmeflask.ImprontaCodice(c.Numero, "segreto1"), c.ID)
	b := nuovoBrowser(t, p.h)
	base := "/stato/" + c.Numero
	pagina := base + "?pw=segreto1"
	var r risposta
	for range 7 {
		azione, campi := moduloDellaPagina(t, b.vai("GET", pagina, nil, nil).corpo, "public-telegram-form")
		r = b.vai("POST", azione, campi, nil)
	}
	if !redirectA(r, pagina+"#public-telegram") {
		t.Fatalf("settima storica: %d %q", r.codice, r.location)
	}
	contiene(t, "settima storica", reRiquadroTelegram.FindString(b.vai("GET", r.location, nil, nil).corpo),
		"⚠️ Troppe richieste nell&#39;ultima ora: riprova più tardi o contatta il negozio.")
}

// codaRottaWeb è una coda che non sa dire se il canale è acceso.
type codaRottaWeb struct{ buste.Posta }

func (codaRottaWeb) CanaleAttivo(context.Context, string) (bool, error) {
	return false, errors.New("database guasto")
}

// inventario: CLI-30, CLI-45
// Un errore nel leggere lo stato di Telegram non toglie il popup WhatsApp: si apre con l'avviso del link
// Telegram spento e il registro lo dice, senza dati (review della 072).
func TestPopupConStatoTelegramNonLetto(t *testing.T) {
	reg := &bytes.Buffer{}
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	inserisciConti(t, d, contiBase)
	dd := dipendenzeProva(&sondaFinta{pronto: true}, slog.New(slog.NewTextHandler(reg, nil)), accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti = o, ratelimit.Nuovo(o)
	dd.Buste = buste.Nuove(d, o).ConPosta(codaRottaWeb{}, []byte("chiave-email-di-prova-di-32-byte")).ConIndirizzoPubblico(indirizzoPubblicoProva)
	dd.IndirizzoPubblico, dd.Configurazione = indirizzoPubblicoProva, configProva
	h := mustInterna(t, dd)
	id := bustaHome(t, d, o, "1/26", "", "")
	b := dipendenteDentro(t, h)
	b.vai("GET", percorsoScheda(id), nil, nil)
	r := b.vai("GET", percorsoScheda(id)+"/avviso?mostra="+b.segno, nil, nil)
	if r.codice != http.StatusOK {
		t.Fatalf("popup: %d", r.codice)
	}
	contiene(t, "popup", r.corpo, `id="notice-popup"`, `id="notice-telegram-off"`)
	contiene(t, "registro", reg.String(), "stato di Telegram non letto per il popup")
	nonContiene(t, "registro", reg.String(), "database guasto")
}
