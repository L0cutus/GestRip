package web

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/buste"
)

// Il popup «📲 Avvisa cliente» (avvisi.go, slice 066), come NoticePopup e gli eventi notice-open,
// notice-cancel, notice-send e reset-notice dello standalone: i casi di notice_popup_live_test.exs e
// repair_notice_live_test.exs, senza LiveView. Dati inventati.

var reNotifyScheda = regexp.MustCompile(`<a id="repair-notify" href="(/repairs/\d+/avviso\?mostra=[A-Za-z0-9_-]{22})" class="pulsante-avvisa" data-avviso>([^<]+)</a>`)

// modulo dell'«Invia» ricavato dalla pagina del popup.
func moduloAvviso(t *testing.T, corpo string) url.Values {
	t.Helper()
	i := strings.Index(corpo, `<form id="notice-form"`)
	if i < 0 {
		t.Fatalf("manca il modulo del popup:\n%s", corpo)
	}
	f := corpo[i:]
	f = f[:strings.Index(f, "</form>")]
	v := url.Values{}
	for _, m := range regexp.MustCompile(`<input name="([^"]+)" type="hidden" value="([^"]*)">`).FindAllStringSubmatch(f, -1) {
		v.Set(m[1], htmlNonScappato(m[2]))
	}
	if m := regexp.MustCompile(`(?s)<textarea id="notice-body" name="notice\[body\]" rows="8">\n(.*)</textarea>`).FindStringSubmatch(f); m != nil {
		v.Set("notice[body]", htmlNonScappato(m[1]))
	}
	return v
}

func htmlNonScappato(s string) string {
	return strings.NewReplacer("&#39;", "'", "&quot;", `"`, "&lt;", "<", "&gt;", ">", "&#43;", "+", "&amp;", "&").Replace(s)
}

// inventario: CLI-29, CLI-36, RIP-S-23
// Dalla scheda di una busta pronta: «📲 Avvisa cliente» con il segno della scheda, il riquadro «🔔 Avviso
// di ritiro» da avvisare; il popup (una pagina senza JavaScript) con il titolo, «Busta N. 1/26», WhatsApp
// e il numero per wa.me con il nome, il testo dell'avviso di ritiro già scritto, i campi nascosti e
// «Annulla» verso la scheda con lo stesso segno. Aprire e annullare non scrivono niente (nemmeno
// repair.viewed: la scheda torna mostrata).
func TestPopupDallaScheda(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaHome(t, d, o, "1/26", "", "stato = 'pronta', link_inviato_il = ?", istanteHome)
	b := dipendenteDentro(t, h)
	corpo := b.vai("GET", percorsoScheda(id), nil, nil).corpo
	m := reNotifyScheda.FindStringSubmatch(corpo)
	if m == nil || m[2] != "📲 Avvisa cliente" {
		t.Fatalf("manca il pulsante:\n%s", corpo)
	}
	contiene(t, "riquadro", corpo, `<div id="repair-notice" class="riquadro-avviso">`+"\n"+`<strong>🔔 Avviso di ritiro</strong>`+"\n"+
		`<p id="repair-notice-pending" class="sotto">Non ancora avvisata. L'avviso singolo si manda una volta sola.</p>`+"\n"+`</div>`)
	segno := strings.TrimPrefix(m[1], percorsoScheda(id)+"/avviso?mostra=")
	prima := contaRighe(t, d, `SELECT (SELECT count(*) FROM eventi_audit) * 1000 + (SELECT count(*) FROM messaggi_in_uscita)`)
	r := b.vai("GET", m[1], nil, nil)
	if r.codice != 200 || r.h.Get("Cache-Control") != "no-store" {
		t.Fatalf("popup %d %v", r.codice, r.h)
	}
	contiene(t, "popup", r.corpo, `<div id="notice-popup" class="avviso-sfondo" role="dialog" aria-modal="true" aria-labelledby="notice-title">`,
		`<h2 id="notice-title">📲 Avviso di ritiro</h2>`+"\n"+`<p class="sotto">Busta N. 1/26</p>`,
		`<p id="notice-platform" data-channel="whatsapp" class="avviso-piattaforma avviso-whatsapp"><span class="grassetto">WhatsApp</span> <span>a &#43;39333111 (Cliente 1/26)</span></p>`,
		`<form id="notice-form" action="/repairs/`+idTesto(id)+`/avviso" method="post" data-channel="whatsapp" data-avviso-invio>`+"\n"+
			`<input name="_csrf_token" type="hidden" value="`+b.token+`">`,
		`<input name="mostra" type="hidden" value="`+segno+`">`,
		`<input name="scopo" type="hidden" value="controparte"><input name="tipo" type="hidden" value="ready"><input name="canale" type="hidden" value="whatsapp">`,
		`<input name="indirizzo" type="hidden" value="&#43;39333111"><input name="controparte" type="hidden" value="">`,
		`<textarea id="notice-body" name="notice[body]" rows="8">`+"\n"+`Buongiorno Cliente 1/26, la Sua riparazione n° N. 1/26 è pronta per il ritiro! 🎉`,
		`<p class="sotto">{link} diventa il link della busta all&#39;invio, e {telegram_optin} il suo link per le notifiche Telegram. Invia registra il messaggio in lista e apre WhatsApp.</p>`,
		`<button id="notice-send" type="submit">Invia</button>`+"\n"+`<a id="notice-cancel" href="/repairs/`+idTesto(id)+`?mostra=`+segno+`" class="pulsante-contorno" data-avviso-chiudi>Annulla</a>`)
	nonContiene(t, "popup", r.corpo, "notice-problem", "notice-template-error", `id="notice-telegram-off" class="avviso-attenzione">`)
	viste := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`)
	if x := b.vai("GET", percorsoScheda(id)+"?mostra="+segno, nil, nil); x.codice != 200 {
		t.Fatalf("annulla %d", x.codice)
	}
	if contaRighe(t, d, `SELECT (SELECT count(*) FROM eventi_audit) * 1000 + (SELECT count(*) FROM messaggi_in_uscita)`) != prima {
		t.Error("aprire e annullare hanno scritto")
	}
	if contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`) != viste {
		t.Error("«Annulla» ha riaperto la scheda")
	}
}

// inventario: CLI-30, CLI-36
// «Invia» senza JavaScript: il testo corretto registrato (il link vero al posto di {link}), il blocco
// dell'avviso di ritiro, e il redirect a wa.me con il numero e il testo; la scheda poi mostra «📲
// Avvisato il …» con «Azzera l'avviso» e la storia. Con JavaScript (Accept: application/json) lo stesso
// invio risponde con il JSON dell'indirizzo, senza cache.
func TestInviaDalPopup(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaHome(t, d, o, "1/26", "", "stato = 'pronta'")
	altra := bustaHome(t, d, o, "2/26", "RIP-2026-0045", "link_inviato_il = ?", istanteHome)
	b := dipendenteDentro(t, h)
	b.vai("GET", percorsoScheda(id), nil, nil)
	f := moduloAvviso(t, b.vai("GET", percorsoScheda(id)+"/avviso?mostra="+b.segno, nil, nil).corpo)
	f.Set("_csrf_token", b.token)
	f.Set("notice[body]", "Pronta! {link}")
	r := b.vai("POST", percorsoScheda(id)+"/avviso", f, nil)
	token := colonna(t, d, id, "token_pubblico")
	testo := "Pronta! " + indirizzoPubblicoProva + "/r/" + token
	if r.codice != 302 || r.location != buste.IndirizzoWaMe("+39333111", testo) {
		t.Fatalf("invio %d %q", r.codice, r.location)
	}
	if colonna(t, d, id, "avviso_pronta_il") != istanteHome {
		t.Error("blocco")
	}
	if v := contaSQL(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE testo = ? AND indirizzo = '+39333111' AND canale = 'whatsapp'`, testo); v != 1 {
		t.Errorf("messaggio %d", v)
	}
	if v := contaRighe(t, d, `SELECT count(*) FROM eventi_avviso_pronta WHERE evento = 'notified' AND attore = 'banco@esempio.it'`); v != 1 {
		t.Errorf("evento %d", v)
	}
	corpo := b.vai("GET", percorsoScheda(id), nil, nil).corpo
	contiene(t, "avvisata", corpo, `<span id="repair-notice-badge" class="segno-avvisato">📲 Avvisato il 31/10/2026 10:00</span>`,
		`<button id="repair-notice-reset" type="submit" class="pulsante-piccolo" data-confirm="Azzerare l&#39;avviso? La busta potrà essere avvisata di nuovo e lo storico resta.">Azzera l'avviso</button>`,
		`<ul id="repair-notice-history" class="storia-avviso">`+"\n"+`<li id="repair-notice-event-1">Avviso inviato · 31/10/2026 10:00<span> · banco@esempio.it</span></li>`)
	nonContiene(t, "avvisata", corpo, "repair-notice-pending")

	// Con JavaScript: il JSON, dalla lista.
	f = moduloAvviso(t, b.vai("GET", percorsoScheda(altra)+"/avviso?da=lista&lista="+url.QueryEscape("stato=in_lavorazione"), nil, nil).corpo)
	if f.Get("da") != "lista" || f.Get("lista") != "stato=in_lavorazione" || f.Get("tipo") != "update" {
		t.Fatalf("modulo dalla lista %v", f)
	}
	f.Set("_csrf_token", b.token)
	r = b.vai("POST", percorsoScheda(altra)+"/avviso", f, map[string]string{"Accept": "application/json"})
	var j map[string]string
	if err := json.Unmarshal([]byte(r.corpo), &j); err != nil || r.codice != 200 || r.h.Get("Content-Type") != "application/json" ||
		r.h.Get("Cache-Control") != "no-store" || !strings.HasPrefix(j["url"], "https://wa.me/%2B39333111?text=Buongiorno%20Cliente%202%2F26%2C%20c%27%C3%A8%20stato%20un%20aggiornamento%20sulla%20Sua%20riparazione%20n%C2%B0%200045%2F26.") || len(j) != 1 {
		t.Errorf("JSON %d %v %q", r.codice, r.h, r.corpo)
	}
}

// inventario: CLI-30, CLI-36
// I rifiuti: un avviso già mandato torna alla scheda con il messaggio dello standalone e niente
// scritto; dalla lista torna alla lista con i filtri; un testo vuoto ridisegna il popup con il testo e
// «Il messaggio è vuoto: niente è stato inviato.».
func TestRifiutiDelPopup(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaHome(t, d, o, "1/26", "", "stato = 'pronta', avviso_pronta_il = ?", istanteHome)
	b := dipendenteDentro(t, h)
	b.vai("GET", percorsoScheda(id), nil, nil)
	f := moduloAvviso(t, b.vai("GET", percorsoScheda(id)+"/avviso?mostra="+b.segno, nil, nil).corpo)
	f.Set("_csrf_token", b.token)
	f.Set("tipo", "ready") // un popup disegnato prima dell'avviso di un collega
	prima := contaRighe(t, d, `SELECT (SELECT count(*) FROM eventi_audit) * 1000 + (SELECT count(*) FROM messaggi_in_uscita)`)
	r := b.vai("POST", percorsoScheda(id)+"/avviso", f, nil)
	if !redirectAllaScheda(r, id) {
		t.Fatalf("rifiuto %d %q", r.codice, r.location)
	}
	contiene(t, "rifiuto", b.vai("GET", r.location, nil, nil).corpo, `<div role="alert" class="flash flash-errore">Questa busta è già stata avvisata: azzera l&#39;avviso per rifarlo.</div>`)
	if contaRighe(t, d, `SELECT (SELECT count(*) FROM eventi_audit) * 1000 + (SELECT count(*) FROM messaggi_in_uscita)`) != prima {
		t.Error("il rifiuto ha scritto")
	}
	// Dalla lista: torna alla lista dei filtri.
	f.Set("da", "lista")
	f.Set("lista", "stato=pronta&p=1")
	r = b.vai("POST", percorsoScheda(id)+"/avviso", f, nil)
	if r.codice != 302 || r.location != "/?stato=pronta" {
		t.Fatalf("dalla lista %d %q", r.codice, r.location)
	}
	// Testo vuoto: il popup con il testo, l'errore e il segno nuovo per tornare.
	b.vai("GET", percorsoScheda(id), nil, nil)
	f = moduloAvviso(t, b.vai("GET", percorsoScheda(id)+"/avviso?mostra="+b.segno, nil, nil).corpo)
	f.Set("_csrf_token", b.token)
	f.Set("notice[body]", "  \n ")
	r = b.vai("POST", percorsoScheda(id)+"/avviso", f, nil)
	if r.codice != 200 {
		t.Fatalf("vuoto %d", r.codice)
	}
	contiene(t, "vuoto", r.corpo, `<div role="alert" class="flash flash-errore">Il messaggio è vuoto: niente è stato inviato.</div>`,
		`<textarea id="notice-body" name="notice[body]" rows="8">`+"\n"+"  \n </textarea>", `<input name="tipo" type="hidden" value="update">`)
	if !regexp.MustCompile(`<a id="notice-cancel" href="/repairs/` + idTesto(id) + `\?mostra=[A-Za-z0-9_-]{22}"`).MatchString(r.corpo) {
		t.Error("«Annulla» senza segno nuovo")
	}
}

// inventario: RIP-H-17, CLI-29
// Dalla lista: 📲 su ogni busta aperta (aria-label con il numero visibile, i filtri della lista
// nell'indirizzo), mai su una chiusa; il popup dalla lista ha «Annulla» verso la lista. Un 📲 forzato
// su una busta chiusa torna alla lista con «La busta è chiusa: niente è stato inviato.» e non scrive.
// Una busta senza numero apre il popup con il solo problema e «Chiudi»; nella scheda non ha il
// pulsante.
func TestPopupDallaLista(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	aperta := bustaHome(t, d, o, "1/26", "RIP-2026-0045", "")
	chiusa := bustaHome(t, d, o, "2/26", "", "stato = 'consegnata', data_consegna = '2026-10-31'")
	senza := bustaHome(t, d, o, "3/26", "", "telefono_cliente = ''")
	b := dipendenteDentro(t, h)
	corpo := b.vai("GET", "/?stato=&p=1", nil, nil).corpo
	contiene(t, "lista", riquadroBusta(t, corpo, aperta), `<a id="notify-repair-`+idTesto(aperta)+`" href="/repairs/`+idTesto(aperta)+
		`/avviso?da=lista&amp;lista=" aria-label="Avvisa per la busta 0045/26" title="Avvisa cliente" class="gs-avvisa" data-avviso>📲</a>`+"\n"+
		`<form action="/repairs/`+idTesto(aperta)+`/pin"`)
	corpo = b.vai("GET", "/?stato=consegnata", nil, nil).corpo
	nonContiene(t, "chiusa", riquadroBusta(t, corpo, chiusa), "notify-repair")
	contiene(t, "filtri", b.vai("GET", "/?stato=in_lavorazione", nil, nil).corpo, `href="/repairs/`+idTesto(aperta)+`/avviso?da=lista&amp;lista=stato%3din_lavorazione"`)

	prima := contaRighe(t, d, `SELECT (SELECT count(*) FROM eventi_audit) * 1000 + (SELECT count(*) FROM messaggi_in_uscita)`)
	r := b.vai("GET", percorsoScheda(chiusa)+"/avviso?da=lista&lista=stato%3Dconsegnata", nil, nil)
	if r.codice != 302 || r.location != "/?stato=consegnata" {
		t.Fatalf("chiusa %d %q", r.codice, r.location)
	}
	contiene(t, "chiusa", b.vai("GET", r.location, nil, nil).corpo, `<div role="alert" class="flash flash-errore">La busta è chiusa: niente è stato inviato.</div>`)
	r = b.vai("GET", percorsoScheda(aperta)+"/avviso?da=lista&lista=stato%3Din_lavorazione", nil, nil)
	contiene(t, "dalla lista", r.corpo, `<input name="da" type="hidden" value="lista"><input name="lista" type="hidden" value="stato=in_lavorazione">`,
		`<a id="notice-cancel" href="/?stato=in_lavorazione" class="pulsante-contorno" data-avviso-chiudi>Annulla</a>`)
	nonContiene(t, "dalla lista", r.corpo, `name="mostra"`)
	r = b.vai("GET", percorsoScheda(senza)+"/avviso?da=lista&lista=", nil, nil)
	contiene(t, "senza numero", r.corpo, `<span>nessun recapito</span>`,
		`<div id="notice-problem" class="avviso-problema">Nessun recapito per il cliente: avvisalo in un altro modo.</div>`+"\n"+
			`<div class="avviso-pulsanti"><a id="notice-cancel" href="/" class="pulsante-contorno" data-avviso-chiudi>Chiudi</a></div>`)
	nonContiene(t, "senza numero", r.corpo, "notice-form")
	nonContiene(t, "scheda senza numero", b.vai("GET", percorsoScheda(senza), nil, nil).corpo, `id="repair-notify"`)
	senzaCifre := bustaHome(t, d, o, "4/26", "", "telefono_cliente = 'nessuno'")
	nonContiene(t, "scheda senza cifre", b.vai("GET", percorsoScheda(senzaCifre), nil, nil).corpo, `id="repair-notify"`)
	if contaRighe(t, d, `SELECT count(*) FROM messaggi_in_uscita`) != prima%1000 {
		t.Error("il popup ha scritto")
	}
	if r := b.vai("GET", "/repairs/abc/avviso?da=lista&lista=stato%3Dpronta", nil, nil); r.codice != 302 || r.location != "/?stato=pronta" {
		t.Errorf("id malformato %d %q", r.codice, r.location)
	}
	if r := b.vai("GET", "/repairs/999/avviso", nil, nil); r.codice != 302 || r.location != "/" {
		t.Errorf("busta assente %d %q", r.codice, r.location)
	}
}

// inventario: CLI-28, CLI-29
// Busta di un'oreficeria: «📲 Avvisa oreficeria» nella scheda, il popup «📲 Avvisa l'oreficeria» al
// numero dell'oreficeria (mai il cliente finale), il suggerimento senza Telegram e senza l'avviso
// delle notifiche spente. Un testo salvato che non si compone dice quale nel popup, con il testo vuoto.
func TestPopupOreficeriaEModello(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome, telefono) VALUES (1, 'Il negozio', NULL), (2, 'Oreficeria Finta', '049 000')`)
	id := bustaHome(t, d, o, "1/26", "", "oreficeria_id = 2")
	b := dipendenteDentro(t, h)
	corpo := b.vai("GET", percorsoScheda(id), nil, nil).corpo
	m := reNotifyScheda.FindStringSubmatch(corpo)
	if m == nil || m[2] != "📲 Avvisa oreficeria" {
		t.Fatalf("pulsante %v", m)
	}
	r := b.vai("GET", m[1], nil, nil)
	contiene(t, "oreficeria", r.corpo, `<h2 id="notice-title">📲 Avvisa l&#39;oreficeria</h2>`, `<span>a &#43;39049000 (Oreficeria Finta)</span>`,
		`<input name="controparte" type="hidden" value="2">`, "Ciao Oreficeria Finta, aggiornamento sulla lavorazione:",
		`<p class="sotto">{link} diventa il link della busta all&#39;invio. Invia registra il messaggio in lista e apre WhatsApp.</p>`)
	nonContiene(t, "oreficeria", r.corpo, "notice-telegram-off", "333111")

	eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.template.wa_link_stato', 'Ciao {password} {telegram_optin}', ?)`, istanteHome)
	diretta := bustaHome(t, d, o, "2/26", "", "")
	b.vai("GET", percorsoScheda(diretta), nil, nil)
	r = b.vai("GET", percorsoScheda(diretta)+"/avviso?mostra="+b.segno, nil, nil)
	contiene(t, "modello", r.corpo, `<div id="notice-template-error" class="avviso-problema">`+"\n"+
		`<strong>⚠️ Il testo «Invia link stato cliente» non è valido.</strong>`+"\n"+`Segnaposto non supportato: {password}`,
		`<textarea id="notice-body" name="notice[body]" rows="8">`+"\n"+`</textarea>`,
		`<p id="notice-telegram-off" class="avviso-attenzione" hidden data-telegram-spento>⚠️ Notifiche Telegram ai clienti spente:`)
	// Un testo con il link Telegram mostra l'avviso delle notifiche spente.
	eseguiSQL(t, d, `UPDATE impostazioni SET valore = 'Iscriviti: {telegram_optin}' WHERE chiave = 'outgoing.template.wa_link_stato'`)
	b.vai("GET", percorsoScheda(diretta), nil, nil)
	r = b.vai("GET", percorsoScheda(diretta)+"/avviso?mostra="+b.segno, nil, nil)
	contiene(t, "telegram", r.corpo, `<p id="notice-telegram-off" class="avviso-attenzione" data-telegram-spento>⚠️ Notifiche Telegram ai clienti spente: il link Telegram porterà il cliente alla frase «Servizio notifiche temporaneamente non disponibile.» finché non le accendi in Impostazioni › Provider di invio.</p>`)
}

// inventario: CLI-36, RIP-S-23
// «Azzera l'avviso»: riuscito torna alla scheda con «Fatto: la busta può essere avvisata di nuovo.» e
// il riquadro di nuovo «da avvisare» con la storia; su una busta non avvisata «Questa busta non risulta
// avvisata.» e niente scritto; senza il token CSRF 403 e niente scritto; vale per il dipendente.
func TestAzzeraAvvisoDallaScheda(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaHome(t, d, o, "1/26", "", "stato = 'pronta', avviso_pronta_il = ?", istanteHome)
	b := dipendenteDentro(t, h)
	b.vai("GET", percorsoScheda(id), nil, nil)
	if r := b.vai("POST", percorsoScheda(id)+"/azzera-avviso", url.Values{"mostra": {b.segno}}, nil); r.codice != 403 || colonna(t, d, id, "avviso_pronta_il") != istanteHome {
		t.Fatalf("senza CSRF %d", r.codice)
	}
	r := b.vai("POST", percorsoScheda(id)+"/azzera-avviso", url.Values{"_csrf_token": {b.token}}, nil)
	if !redirectAllaScheda(r, id) || colonna(t, d, id, "avviso_pronta_il") != "∅" {
		t.Fatalf("azzera %d %q", r.codice, r.location)
	}
	contiene(t, "azzerato", b.vai("GET", r.location, nil, nil).corpo, `<div role="status" class="flash flash-info">Fatto: la busta può essere avvisata di nuovo.</div>`,
		`<p id="repair-notice-pending" class="sotto">`, `<li id="repair-notice-event-1">Avviso azzerato · 31/10/2026 10:00<span> · banco@esempio.it</span></li>`)
	if v := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'ready_notice.reset' AND attore = 'banco@esempio.it'`); v != 1 {
		t.Errorf("audit %d", v)
	}
	r = b.vai("POST", percorsoScheda(id)+"/azzera-avviso", url.Values{"_csrf_token": {b.token}}, nil)
	contiene(t, "non avvisata", b.vai("GET", r.location, nil, nil).corpo, `<div role="alert" class="flash flash-errore">Questa busta non risulta avvisata.</div>`)
	if v := contaRighe(t, d, `SELECT count(*) FROM eventi_avviso_pronta`); v != 1 {
		t.Errorf("eventi %d", v)
	}
}

// inventario: CLI-30
// Il POST dell'«Invia» senza il token CSRF è il 403 e non scrive; senza accesso si va al login.
func TestInviaAvvisoCSRFEAccesso(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaHome(t, d, o, "1/26", "", "")
	b := dipendenteDentro(t, h)
	b.vai("GET", percorsoScheda(id), nil, nil)
	f := moduloAvviso(t, b.vai("GET", percorsoScheda(id)+"/avviso?mostra="+b.segno, nil, nil).corpo)
	f.Del("_csrf_token")
	if r := b.vai("POST", percorsoScheda(id)+"/avviso", f, nil); r.codice != 403 {
		t.Errorf("senza CSRF %d", r.codice)
	}
	if contaRighe(t, d, `SELECT count(*) FROM messaggi_in_uscita`) != 0 || colonna(t, d, id, "link_inviato_il") != "∅" {
		t.Error("senza CSRF ha scritto")
	}
	fuori := nuovoBrowser(t, h)
	if r := fuori.vai("GET", percorsoScheda(id)+"/avviso", nil, nil); r.codice != 302 || r.location != "/login" {
		t.Errorf("senza accesso %d %q", r.codice, r.location)
	}
}

// inventario: RIP-S-20
// Dopo una risposta su una busta chiusa il popup non si propone: si torna alla scheda con «Risposta
// inviata.».
func TestRispostaSuBustaChiusa(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaHome(t, d, o, "1/26", "", "stato = 'consegnata', data_consegna = '2026-10-31'")
	b := dipendenteDentro(t, h)
	b.vai("GET", percorsoScheda(id), nil, nil)
	r := b.vai("POST", percorsoScheda(id)+"/risposta", url.Values{"_csrf_token": {b.token}, "reply[body]": {"Grazie"}}, nil)
	r = b.vai("GET", r.location, nil, nil)
	if !redirectAllaScheda(r, id) {
		t.Fatalf("chiusa %d %q", r.codice, r.location)
	}
	corpo := b.vai("GET", r.location, nil, nil).corpo
	contiene(t, "chiusa", corpo, `<div role="status" class="flash flash-info">Risposta inviata.</div>`)
	nonContiene(t, "chiusa", corpo, "notice-popup")
}

// inventario: CLI-30
// Lo script del popup (app.js): la scheda vuota si apre al tocco, prima del fetch; il fetch chiede il
// JSON; solo con il JSON (cioè dopo che il server ha registrato) la scheda vuota va a wa.me e questa
// pagina alla lista ricordata (#list-return); un rifiuto chiude la scheda vuota; se la scheda vuota è
// stata bloccata la pagina stessa va a wa.me.
func TestScriptDelPopup(t *testing.T) {
	js := string(leggiStatico("app.js"))
	inizio := strings.Index(js, "// Il popup «📲 Avvisa cliente» (slice 066")
	fine := strings.Index(js, "// Una miniatura della galleria")
	if inizio < 0 || fine < inizio {
		t.Fatal("pezzo del popup non trovato")
	}
	pezzo := js[inizio:fine]
	contiene(t, "app.js", pezzo,
		`const CHIAVE_LISTA = "gestrip:list-return"`,
		`scheda = window.open("", "_blank")`,
		`headers: {accept: "application/json"}})`,
		`if ((r.headers.get("content-type") || "").startsWith("application/json")) {`,
		"              scheda.location.href = url\n              scheda = null\n              window.location.assign(lista())",
		"              scheda = null\n              window.location.assign(url)",
		"        chiudiScheda()\n        return r.text().then(html => mostra(r, html, \"aggiungi\"))",
	)
	aperta, invio, json := strings.Index(pezzo, `window.open("", "_blank")`), strings.Index(pezzo, "fetch(form.action"), strings.Index(pezzo, "r.json().then(")
	casa := strings.Index(pezzo, "window.location.assign(lista())")
	if aperta < 0 || invio < aperta || json < invio || casa < json {
		t.Errorf("ordine: apri %d, invio %d, json %d, lista %d", aperta, invio, json, casa)
	}
	if strings.Count(pezzo, "window.location.assign(lista())") != 1 {
		t.Error("la lista va raggiunta solo dopo il JSON")
	}
}

// inventario: CLI-32
// Il link che {telegram_optin} diventa (GET /r/<token>/telegram, telegram_link/2 dello standalone) con
// il canale Telegram spento: un cliente diretto con la busta aperta riceve il 503 in testo semplice
// «Servizio notifiche temporaneamente non disponibile.»; una busta dell'oreficeria e un link che non
// apre niente il 404 di sempre; una busta chiusa torna alla sua pagina con «Questa riparazione non
// riceve più avvisi.». Niente si scrive.
func TestLinkTelegramSpento(t *testing.T) {
	p := paginaClienteProva(t)
	diretta := p.busta(t, nil)
	oref := p.busta(t, func(a *buste.Accettazione) { a.OreficeriaID = "2"; a.CodiceQR = "RIP-2026-0046" })
	chiusa := p.busta(t, func(a *buste.Accettazione) { a.CodiceQR = "RIP-2026-0047" })
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'consegnata', data_consegna = '2026-10-31' WHERE id = ?`, chiusa.ID)
	prima := contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit`)
	b := nuovoBrowser(t, p.h)
	r := b.vai("GET", "/r/"+diretta.Token+"/telegram", nil, nil)
	if r.codice != 503 || r.corpo != "Servizio notifiche temporaneamente non disponibile." || r.h.Get("Content-Type") != "text/plain; charset=utf-8" ||
		r.h.Get("Cache-Control") != "no-store" || r.h.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("diretta: %d %q %v", r.codice, r.corpo, r.h)
	}
	for nome, token := range map[string]string{"oreficeria": oref.Token, "inesistente": "QUESTO-TOKEN-NON-ESISTE-0000000000000000000"} {
		if r := b.vai("GET", "/r/"+token+"/telegram", nil, nil); r.codice != 404 || r.corpo != "Riparazione non disponibile" {
			t.Errorf("%s: %d %q", nome, r.codice, r.corpo)
		}
	}
	r = b.vai("GET", "/r/"+chiusa.Token+"/telegram", nil, nil)
	if r.codice != 302 || r.location != "/r/"+chiusa.Token {
		t.Fatalf("chiusa: %d %q", r.codice, r.location)
	}
	contiene(t, "chiusa", b.vai("GET", r.location, nil, nil).corpo, "Questa riparazione non riceve più avvisi.")
	if contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione != 'repair.public_viewed'`) != prima {
		t.Error("il link Telegram ha scritto")
	}
}

// inventario: CLI-30
// Dopo un testo vuoto il popup si ridisegna con i campi nascosti del popup di prima, anche se nel
// frattempo la busta è diventata da avvisare: il tipo resta «update», e il prossimo «Invia» lo rifiuta.
func TestRidisegnoTieneIlPopupDiPrima(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaHome(t, d, o, "1/26", "", "")
	b := dipendenteDentro(t, h)
	b.vai("GET", percorsoScheda(id), nil, nil)
	f := moduloAvviso(t, b.vai("GET", percorsoScheda(id)+"/avviso?mostra="+b.segno, nil, nil).corpo)
	eseguiSQL(t, d, `UPDATE buste SET stato = 'pronta' WHERE id = ?`, id)
	f.Set("_csrf_token", b.token)
	f.Set("notice[body]", " ")
	r := b.vai("POST", percorsoScheda(id)+"/avviso", f, nil)
	contiene(t, "ridisegno", r.corpo, `<input name="tipo" type="hidden" value="update">`, `<h2 id="notice-title">📲 Avviso di ritiro</h2>`)
	f = moduloAvviso(t, r.corpo)
	f.Set("_csrf_token", b.token)
	f.Set("notice[body]", "Ciao")
	r = b.vai("POST", percorsoScheda(id)+"/avviso", f, nil)
	contiene(t, "rifiuto", b.vai("GET", r.location, nil, nil).corpo, "La busta è diventata pronta mentre il messaggio era aperto")
}
