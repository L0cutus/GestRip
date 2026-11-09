package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// La conversazione della pagina del cliente e della scheda (CLI-10…15, RIP-S-19, RIP-S-20), come
// public_repair_thread_test.exs, public_repair_message_test.exs e repair_live_test.exs dello
// standalone (go-baseline).

func (p provaCliente) riga(t *testing.T, id int64, autore string, canale any, testo, il string) {
	t.Helper()
	eseguiSQL(t, p.d, `INSERT INTO messaggi_buste (busta_id, autore, canale, testo, inviato_il, creato_il, aggiornato_il)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, id, autore, canale, testo, il, il, il)
}

// inventario: CLI-10
// La conversazione sulla pagina: dal più vecchio, il lato di ogni riga, l'ora del negozio, il testo
// scappato; vuota lo dice; mai su una busta chiusa, mai le righe di un'altra busta.
func TestPaginaClienteConversazione(t *testing.T) {
	p := paginaClienteProva(t)
	b := nuovoBrowser(t, p.h)
	c := p.busta(t, nil)
	altra := p.busta(t, nil)
	r := b.vai("GET", "/r/"+c.Token, nil, nil)
	contiene(t, "vuota", r.corpo, `<section id="public-thread" class="cliente-conversazione" data-poll-url="/r/`+c.Token+`/poll" data-shop-name="Bottega di Prova" data-logo-url="/branding/icon-192.png">`,
		"💬 Lascia un messaggio", `<li id="public-thread-empty" class="cliente-conv-vuota">Ancora nessun messaggio. Scrivi qui sotto per contattarci.</li>`,
		`<form id="public-thread-form" action="/r/`+c.Token+`/messaggio" method="post" class="cliente-conv-modulo">`+"\n"+`<input name="_csrf_token" type="hidden" value="`+b.token+`">`,
		`<textarea id="public-thread-text" name="testo" rows="3" maxlength="1000"`, `<button type="submit" id="public-thread-send">Invia messaggio</button>`,
		`<p id="public-thread-feedback" role="status" data-kind="info" class="cliente-conv-esito"></p>`,
		`<template id="public-thread-line"><li class="cliente-conv-riga">`, `<template data-thread-icon="wa">`)
	nonContiene(t, "vuota", r.corpo, "data-last-ts")
	p.riga(t, c.ID, "shop", nil, "Pronta <domani>", "2026-10-31T10:00:00.000000Z")
	p.riga(t, c.ID, "customer", nil, "Grazie & ciao", "2026-10-31T09:00:00.000000Z")
	p.riga(t, c.ID, "customer", "whatsapp", "Via WhatsApp", "2026-10-31T10:00:00.000000Z")
	p.riga(t, altra.ID, "customer", nil, "Segreto di un'altra busta", "2026-10-31T09:30:00.000000Z")
	r = b.vai("GET", "/r/"+c.Token, nil, nil)
	contiene(t, "righe", r.corpo, `data-last-ts="2026-10-31T10:00:00.000000Z"`,
		`<li id="public-thread-message-2" data-party="customer" data-side="cliente" class="cliente-conv-riga">`,
		`<span data-slot="at" class="cliente-conv-ora">31/10/2026 10:00</span><br>`+"\n"+`<span data-slot="body" class="cliente-conv-testo">Grazie &amp; ciao</span>`,
		`<li id="public-thread-message-1" data-party="shop" data-side="negozio" class="cliente-conv-riga">`,
		`<img src="/branding/icon-192.png" alt="Bottega di Prova" class="cliente-conv-logo">`, "Pronta &lt;domani&gt;",
		`<li id="public-thread-message-3" data-party="customer" data-side="wa"`, `aria-label="via WhatsApp"`)
	if a, z := strings.Index(r.corpo, "Grazie &amp;"), strings.Index(r.corpo, "Via WhatsApp"); a < 0 || z < a || strings.Index(r.corpo, "Pronta &lt;") > z {
		t.Error("ordine delle righe")
	}
	nonContiene(t, "righe", r.corpo, "Segreto di un'altra busta", "public-thread-empty")
	// Sulla busta B2B il lato è l'oreficeria.
	eseguiSQL(t, p.d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, c.ID)
	contiene(t, "b2b", b.vai("GET", "/r/"+c.Token, nil, nil).corpo, `id="public-thread-message-2" data-party="oreficeria"`)
	for _, stato := range []string{"consegnata", "annullata"} {
		eseguiSQL(t, p.d, `UPDATE buste SET stato = ? WHERE id = ?`, stato, c.ID)
		nonContiene(t, stato, b.vai("GET", "/r/"+c.Token, nil, nil).corpo, "public-thread", "Pronta &lt;domani&gt;", "/messaggio", "/poll")
	}
}

// inventario: CLI-11
// Il poll: il JSON dello standalone (chiavi in ordine, nessun nome né numero), no-store; link morto,
// conversazione nascosta e busta nel cestino lo stesso 404 della pagina, contato dal limite.
func TestPaginaClientePoll(t *testing.T) {
	p := paginaClienteProva(t)
	b := nuovoBrowser(t, p.h)
	c := p.busta(t, nil)
	r := b.vai("GET", "/r/"+c.Token+"/poll", nil, nil)
	if r.codice != http.StatusOK || r.corpo != `{"messages":[],"ts":null}` || r.h.Get("Content-Type") != "application/json; charset=utf-8" ||
		r.h.Get("Cache-Control") != "no-store" || r.h.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("vuoto: %d %q %v", r.codice, r.corpo, r.h)
	}
	p.riga(t, c.ID, "shop", nil, "Pronta <domani> & \"ok\"", "2026-10-31T10:00:00.123456Z")
	p.riga(t, c.ID, "customer", "whatsapp", "Sì", "2026-10-31T11:00:00.000000Z")
	r = b.vai("GET", "/r/"+c.Token+"/poll", nil, nil)
	atteso := `{"messages":[{"id":1,"at":"31/10/2026 11:00","body":"Pronta <domani> & \"ok\"","party":"shop","side":"negozio"},` +
		`{"id":2,"at":"31/10/2026 12:00","body":"Sì","party":"customer","side":"wa"}],"ts":"2026-10-31T11:00:00.000000Z"}`
	if r.corpo != atteso {
		t.Errorf("poll\n%s\natteso\n%s", r.corpo, atteso)
	}
	nonContiene(t, "poll", r.corpo, "Mario", "0045", c.Numero)
	audit0 := contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit`)
	morto := func(nome, percorso string) {
		t.Helper()
		x := b.vai("GET", percorso, nil, nil)
		if x.codice != http.StatusNotFound || x.corpo != "Riparazione non disponibile" || x.h.Get("Content-Type") != "" {
			t.Errorf("%s: %d %q", nome, x.codice, x.corpo)
		}
	}
	morto("inventato", "/r/inventato/poll")
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, c.ID)
	morto("consegnata", "/r/"+c.Token+"/poll")
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'pronta', token_pubblico_revocato_il = ? WHERE id = ?`, istanteHome, c.ID)
	morto("revocato", "/r/"+c.Token+"/poll")
	if contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit`) != audit0 {
		t.Error("il poll ha scritto nel registro")
	}
	if strings.Contains(p.reg.String(), c.Token) {
		t.Error("il token nel registro")
	}
	// Il limite conta i 404 del poll come quelli della pagina.
	viva := p.busta(t, nil)
	for i := 0; i < 30; i++ {
		b.vai("GET", "/r/inventato-"+strconv.Itoa(i)+"/poll", nil, nil)
	}
	if x := b.vai("GET", "/r/"+viva.Token+"/poll", nil, nil); x.codice != http.StatusNotFound {
		t.Errorf("dopo 30 tentativi: %d", x.codice)
	}
}

// inventario: CLI-12
// Il messaggio del cliente: CSRF, redirect all'ancora con l'esito nel riquadro una volta, i rifiuti con
// i testi dello standalone, la busta chiusa con la frase in cima, il link morto 404; la riga non letta
// per il negozio e il filtro «notifiche» della home la trova.
func TestPaginaClienteMessaggio(t *testing.T) {
	p := paginaClienteProva(t)
	b := nuovoBrowser(t, p.h)
	c := p.busta(t, nil)
	pagina := "/r/" + c.Token
	b.vai("GET", pagina, nil, nil)
	if r := b.vai("POST", pagina+"/messaggio", url.Values{"testo": {"ciao"}}, nil); r.codice != http.StatusForbidden {
		t.Fatalf("senza CSRF: %d", r.codice)
	}
	manda := func(testo string) risposta {
		t.Helper()
		b.vai("GET", pagina, nil, nil)
		return b.vai("POST", pagina+"/messaggio", url.Values{"_csrf_token": {b.token}, "testo": {testo}}, nil)
	}
	esito := func(nome, testo, kind, frase string) {
		t.Helper()
		r := manda(testo)
		if !redirectA(r, pagina+"#public-thread") || r.h.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %q", nome, r.codice, r.location)
		}
		classe := "cliente-conv-esito"
		if kind == "error" {
			classe += " cliente-conv-errore"
		}
		g := b.vai("GET", pagina, nil, nil)
		contiene(t, nome, g.corpo, `<p id="public-thread-feedback" role="status" data-kind="`+kind+`" class="`+classe+`">`+frase+`</p>`)
		nonContiene(t, nome, g.corpo, "public-flash")
		contiene(t, nome+" una volta", b.vai("GET", pagina, nil, nil).corpo, `data-kind="info" class="cliente-conv-esito"></p>`)
	}
	esito("vuoto", " \n ", "error", "Scrivi un messaggio prima di inviare.")
	esito("lungo", strings.Repeat("x", 1001), "error", "Messaggio troppo lungo (max 1000 caratteri)")
	if contaSQL(t, p.d, `SELECT count(*) FROM messaggi_buste`) != 0 {
		t.Fatal("scritto un rifiuto")
	}
	p.o.Sposta(time.Minute)
	esito("inviato", "  Quando è pronta? <b>  ", "info", "Messaggio inviato.")
	contiene(t, "riga", b.vai("GET", pagina, nil, nil).corpo, `data-party="customer" data-side="cliente"`, "Quando è pronta? &lt;b&gt;")
	if contaSQL(t, p.d, `SELECT count(*) FROM buste WHERE id = ? AND messaggi_letti_il IS NULL AND ultimo_messaggio_il IS NOT NULL`, c.ID) != 1 {
		t.Error("non segnato come non letto")
	}
	if strings.Contains(p.reg.String(), "Quando") || contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit WHERE dati LIKE '%Quando%'`) != 0 {
		t.Error("il testo nel registro o nell'audit")
	}
	// Il filtro «notifiche» della home la trova (indice.go, nonLettiSQL).
	ls, err := p.b.Lista(context.Background(), buste.Filtri{Notifiche: true})
	if err != nil || len(ls.Righe) != 1 || ls.Righe[0].ID != c.ID {
		t.Errorf("notifiche: %+v %v", ls.Righe, err)
	}
	for i := 0; i < 9; i++ {
		manda("ancora")
	}
	esito("limite", "undicesimo", "error", "Ha già inviato molti messaggi nell&#39;ultima ora: riprovi più tardi o contatti il negozio.")
	// Chiusa nel frattempo: in cima alla pagina, che non ha più il riquadro.
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, c.ID)
	if r := manda("ciao"); !redirectA(r, pagina) {
		t.Fatalf("chiusa: %d %q", r.codice, r.location)
	}
	contiene(t, "chiusa", b.vai("GET", pagina, nil, nil).corpo, `<div id="public-flash-error" class="cliente-esito cliente-esito-errore">Questa riparazione non riceve più messaggi.</div>`)
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'pronta', token_pubblico_revocato_il = ? WHERE id = ?`, istanteHome, c.ID)
	b.vai("GET", "/r/"+p.busta(t, nil).Token, nil, nil)
	for _, perc := range []string{pagina + "/messaggio", "/r/inventato/messaggio", "/r/a%2Fb/messaggio"} {
		// Anche con il testo vuoto: il link si guarda prima del testo (with_public_repair/3).
		for _, testo := range []string{"ciao", ""} {
			if r := b.vai("POST", perc, url.Values{"_csrf_token": {b.token}, "testo": {testo}}, nil); r.codice != http.StatusNotFound || r.corpo != "Riparazione non disponibile" {
				t.Errorf("%s %q: %d %q", perc, testo, r.codice, r.corpo)
			}
		}
	}
	if n := contaSQL(t, p.d, `SELECT count(*) FROM messaggi_buste`); n != 10 {
		t.Errorf("righe %d", n)
	}
}

// inventario: RIP-S-19, RIP-S-20, CLI-14
// La conversazione nella scheda (dal più vecchio, il negozio a destra, segnata letta all'apertura) e
// «Invia 🏪»: la risposta nella conversazione, visibile sulla pagina del cliente.
func TestSchedaConversazione(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := dipendenteDentro(t, h)
	sid := strconv.FormatInt(id, 10)
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	nonContiene(t, "senza messaggi", r.corpo, "repair-thread")
	contiene(t, "risposta", r.corpo, `<form id="repair-reply" action="/repairs/`+sid+`/risposta" method="post" class="riquadro-risposta" data-invio-unico>`,
		`<span class="label">Rispondi al cliente</span>`, `<textarea id="repair-reply-body" name="reply[body]" rows="2" maxlength="1000" placeholder="Scrivi la tua risposta…"></textarea>`,
		"Invia 🏪")
	nonContiene(t, "aperta", r.corpo, "La busta è chiusa: la risposta resta")
	eseguiSQL(t, d, `INSERT INTO messaggi_buste (busta_id, autore, canale, testo, inviato_il, creato_il, aggiornato_il) VALUES
		(?, 'customer', NULL, 'Domanda <prima>', '2026-10-31T08:00:00.000000Z', '2026-10-31T08:00:00.000000Z', '2026-10-31T08:00:00.000000Z')`, id)
	eseguiSQL(t, d, `UPDATE buste SET ultimo_messaggio_il = '2026-10-31T08:00:00.000000Z' WHERE id = ?`, id)
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "con messaggi", r.corpo, `<div id="repair-thread" class="riquadro-conversazione">`, "<strong>💬 Conversazione con il cliente</strong>",
		`<li id="repair-thread-message-1" data-party="customer" class="conv-controparte">`+"\n"+`<div class="conv-ora">31/10/2026 09:00</div>`+"\n"+`<div class="conv-testo">Domanda &lt;prima&gt;</div>`)
	if contaSQL(t, d, `SELECT count(*) FROM buste WHERE id = ? AND messaggi_letti_il = ultimo_messaggio_il`, id) != 1 {
		t.Error("aprire la scheda non segna letto")
	}
	form := func(testo string) url.Values {
		return url.Values{"_csrf_token": {b.token}, "reply[body]": {testo}}
	}
	for testo, frase := range map[string]string{"  ": "La risposta è vuota: niente è stato inviato.",
		strings.Repeat("y", 1001): "La risposta supera i 1000 caratteri: niente è stato inviato."} {
		x := b.vai("POST", percorsoScheda(id)+"/risposta", form(testo), nil)
		if !redirectAllaScheda(x, id) {
			t.Fatalf("%q: %d %q", frase, x.codice, x.location)
		}
		contiene(t, frase, b.vai("GET", x.location, nil, nil).corpo, `<div role="alert" class="flash flash-errore">`+frase+`</div>`)
	}
	o.Sposta(time.Hour)
	x := b.vai("POST", percorsoScheda(id)+"/risposta", form(" Pronta da lunedì "), nil)
	if !redirectAllaScheda(x, id) {
		t.Fatalf("risposta: %d %q", x.codice, x.location)
	}
	contiene(t, "inviata", b.vai("GET", x.location, nil, nil).corpo, `<div role="status" class="flash flash-info">Risposta inviata.</div>`,
		`data-party="shop" class="conv-negozio">`+"\n"+`<div class="conv-ora">🏪 31/10/2026 11:00</div>`+"\n"+`<div class="conv-testo">Pronta da lunedì</div>`)
	if contaSQL(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.message_appended' AND attore = 'banco@esempio.it'`) != 1 {
		t.Error("audit della risposta")
	}
	// La risposta è sulla pagina del cliente.
	const tok = "SINTETICO-token-della-conversazione-xxxxxxxx"
	eseguiSQL(t, d, `UPDATE buste SET hash_token_pubblico = ?, token_pubblico_scade_il = '2027-01-01T00:00:00.000000Z',
		token_pubblico_revocato_il = NULL WHERE id = ?`, buste.ImprontaToken(tok), id)
	ms, err := buste.Nuove(d, o).ConversazioneCliente(context.Background(), tok)
	if err != nil || len(ms) != 2 || ms[1].Testo != "Pronta da lunedì" || ms[1].Parte != "shop" {
		t.Errorf("sulla pagina: %+v %v", ms, err)
	}
	// Busta chiusa: la nota; B2B: «Rispondi all'oreficeria».
	eseguiSQL(t, d, `INSERT OR IGNORE INTO oreficerie (id, nome) VALUES (1, 'Il negozio'), (2, 'Oreficeria Esempio')`)
	eseguiSQL(t, d, `UPDATE buste SET stato = 'consegnata', oreficeria_id = 2 WHERE id = ?`, id)
	contiene(t, "chiusa", b.vai("GET", percorsoScheda(id), nil, nil).corpo, "La busta è chiusa: la risposta resta nella conversazione del negozio",
		"Rispondi all&#39;oreficeria", "💬 Conversazione con l&#39;oreficeria", `data-party="oreficeria"`)
	for nome, perc := range map[string]string{"id malformato": "/repairs/abc/risposta", "busta che non c'è": "/repairs/999/risposta"} {
		if x := b.vai("POST", perc, form("ciao"), nil); !redirectA(x, "/") {
			t.Errorf("%s: %d %q", nome, x.codice, x.location)
		}
	}
}

// inventario: CLI-15
// La campanella su ogni pagina del personale: il totale, il titolo, le voci con il link alla scheda e
// «Vedi tutte →»; a zero non c'è il pulsante.
func TestCampanellaNelGuscio(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := dipendenteDentro(t, h)
	r := b.vai("GET", "/", nil, nil)
	contiene(t, "zero", r.corpo, `<div id="nav-bell-wrap" data-count="0"></div>`)
	nonContiene(t, "zero", r.corpo, "nav-bell-menu")
	eseguiSQL(t, d, `UPDATE buste SET ultimo_messaggio_il = '2026-10-31T08:00:00.000000Z', preventivo_rifiutato_il = '2026-10-30T08:00:00.000000Z' WHERE id = ?`, id)
	sid := strconv.FormatInt(id, 10)
	r = b.vai("GET", "/", nil, nil)
	contiene(t, "due", r.corpo, `<div id="nav-bell-wrap" data-count="2">`, `title="1 msg · 1 rifiutati" aria-label="Notifiche non lette: 2"`,
		`<span id="nav-bell-count" class="bell-count">2</span>`,
		`<a id="nav-bell-item-message-`+sid+`" href="/repairs/`+sid+`" role="menuitem"><span class="bell-riga"><span class="bell-message">💬 Messaggio</span><time datetime="2026-10-31T08:00:00.000000Z" class="bell-quando">oggi 09:00</time>`,
		`<span class="bell-rejected">🤝 Rifiutato</span><time datetime="2026-10-30T08:00:00.000000Z" class="bell-quando">ieri 09:00</time>`,
		`<a id="nav-bell-all" href="/?notifiche=1" role="menuitem">Vedi tutte →</a>`)
	if !strings.Contains(r.corpo, `<a id="nav-bell-item-message-`+sid) || strings.Index(r.corpo, "nav-bell-item-message") > strings.Index(r.corpo, "nav-bell-item-rejected") {
		t.Error("ordine delle voci")
	}
	if q := quandoCampanella(time.Date(2026, 10, 1, 8, 5, 0, 0, time.UTC), clock.Oggi(o)); q != "01/10 10:05" {
		t.Errorf("giorno: %q", q)
	}
}
