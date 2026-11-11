package web

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/buste"
)

// La pagina «Testi dei messaggi» (testi.go, slice 067), come TemplatesLive dello standalone: i casi di
// templates_live_test.exs, senza LiveView. Dati inventati.

// casellaTesto è il contenuto del textarea della famiglia f.
func casellaTesto(t *testing.T, corpo string, f buste.Famiglia) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<textarea id="template-text-` + string(f) + `" name="template\[text\]" rows="\d+">\n(.*?)</textarea>`).FindStringSubmatch(corpo)
	if m == nil {
		t.Fatalf("manca la casella di %s", f)
	}
	return htmlNonScappato(m[1])
}

func salvaTesto(b *browser, f buste.Famiglia, testo string) risposta {
	return b.vai("POST", "/settings/templates", url.Values{"_csrf_token": {b.token}, "template[family]": {string(f)}, "template[text]": {testo}}, nil)
}

// inventario: CLI-38, ADM-13
// La pagina del titolare: il titolo, la frase, le tre sezioni (WhatsApp con il riquadro di
// {telegram_optin}, Telegram con la sua frase e il suggerimento, email) e per ogni famiglia il riquadro
// con il nome, l'uso dove la sorgente lo scrive, i segnaposto (senza {telegram_optin}, con le note e gli
// «avanzati»), la casella con il testo in vigore e le righe della sorgente, «Salva il testo»,
// «Ripristina predefinito» con la conferma e l'anteprima sull'esempio.
func TestPaginaTestiDeiMessaggi(t *testing.T) {
	h, _, _ := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := titolareDentro(t, h)
	r := b.vai("GET", "/settings/templates", nil, nil)
	if r.codice != 200 {
		t.Fatalf("pagina %d", r.codice)
	}
	c := r.corpo
	contiene(t, "pagina", c, `<a id="settings-tab-templates" href="/settings/templates" aria-current="page" class="settings-tab">Messaggi WhatsApp</a>`,
		`<h1>Testi dei messaggi</h1>`+"\n"+`<p class="sotto">Ogni testo vale per una famiglia di messaggi. Lasciando la casella vuota, o con «Ripristina predefinito», torna il testo predefinito. Un testo con un segnaposto non supportato non si salva.</p>`,
		`<section id="templates-whatsapp">`+"\n"+`<h2>Template messaggi WhatsApp</h2>`+"\n"+
			`<p class="sotto">Personalizza i testi inviati via WhatsApp. I segnaposto tra <code>{}</code> vengono sostituiti automaticamente con i dati della riparazione.</p>`+"\n"+
			`<p id="templates-telegram-optin" class="testi-suggerimento">📲 <b>Nuovo:</b> aggiungi <code>{telegram_optin}</code> in qualsiasi template cliente per invitarlo ad attivare le notifiche Telegram in tempo reale (stato &#43; messaggi dal negozio). Il cliente potrà disattivarle con <code>/stop</code>.</p>`,
		`<section id="templates-telegram">`+"\n"+`<h2>Template notifiche Telegram cliente</h2>`+"\n"+
			`<p id="templates-telegram-intro" class="sotto">Personalizza i messaggi inviati ai clienti che hanno attivato le notifiche Telegram. Tag HTML supportati: <code>&lt;b&gt;</code> <code>&lt;i&gt;</code> <code>&lt;u&gt;</code> <code>&lt;code&gt;</code> <code>&lt;blockquote&gt;</code>. Il bottone <i>📂 Apri scheda riparazione</i> viene aggiunto automaticamente sotto il messaggio (non serve metterlo nel testo).</p>`+"\n"+
			`<p id="templates-telegram-hint" class="testi-suggerimento">💡 <b>Suggerimento:</b> aggiungi <code>{telegram_optin}</code> ai template WhatsApp per invitare il cliente ad attivare le notifiche; il link nella pagina pubblica della riparazione mostra sempre il pulsante.</p>`,
		`<section id="templates-email">`+"\n"+`<h2>Avvisi email cliente</h2>`,
		// Un riquadro intero: wa_cliente_aggiornamento ha il titolo e l'uso suoi, le note dei campi.
		`<section id="template-section-wa_cliente_aggiornamento" class="testo-famiglia">`+"\n"+`<h3>Aggiornamento link al cliente (re-invio)</h3>`+"\n"+
			`<p id="template-usage-wa_cliente_aggiornamento" class="sotto">Usato quando si preme &#34;Aggiorna cliente&#34; su una scheda a cui il link è già stato inviato in passato. Per default non include i link social (re-invio più sobrio). Qui lo usa anche l&#39;avviso al cliente di una risposta nella conversazione, in qualunque stato.</p>`+"\n"+
			`<p id="template-fields-wa_cliente_aggiornamento" class="sotto">Segnaposto: <span><code>{nome}</code></span> <span><code>{numero}</code></span> <span><code>{link}</code></span> <span><code>{negozio}</code></span> <span><code>{citta}</code></span> <span><code>{orari}</code></span> <span><code>{social}</code> <em>(ammesso ma non usato di default)</em></span> <span><code>{stima}</code> <em>(ammesso ma non usato di default)</em></span></p>`+"\n"+
			`<form id="template-wa_cliente_aggiornamento" action="/settings/templates" method="post">`+"\n"+`<input name="_csrf_token" type="hidden" value="`,
		`"><input type="hidden" name="template[family]" value="wa_cliente_aggiornamento">`+"\n"+
			`<textarea id="template-text-wa_cliente_aggiornamento" name="template[text]" rows="7">`+"\n"+`Buongiorno {nome}, c&#39;è stato un aggiornamento sulla Sua riparazione n° {numero}.`,
		`</textarea>`+"\n"+`<div class="testi-pulsanti"><button type="submit">Salva il testo</button> <button id="template-restore-wa_cliente_aggiornamento" type="submit" formaction="/settings/templates/ripristina" class="pulsante-contorno" data-confirm="Rimettere il testo predefinito al posto di quello salvato?">Ripristina predefinito</button></div>`+"\n"+`</form>`+"\n"+
			`<div class="testi-anteprima">`+"\n"+`<h4>Anteprima</h4>`+"\n"+`<p class="sotto">Con dati di esempio: nessuna busta reale, nessun invio.</p>`+"\n"+
			`<pre id="template-preview-wa_cliente_aggiornamento">Buongiorno Maria Rossi, c&#39;è stato un aggiornamento sulla Sua riparazione n° 12/26.`,
		`<p id="template-fields-wa_preventivo_con_importo" class="sotto">Segnaposto: <span><code>{nome}</code></span> <span><code>{numero}</code></span> <span><code>{link}</code></span> <span><code>{negozio}</code></span> <span><code>{citta}</code></span> <span><code>{orari}</code></span> <span><code>{social}</code></span> <span class="testi-codici">— avanzato: <code>{preventivo}</code><code>{descrizione}</code></span></p>`,
		`<p id="template-fields-wa_link_stato" class="sotto">Segnaposto: <span><code>{nome}</code></span> <span><code>{numero}</code></span> <span><code>{link}</code></span> <span><code>{negozio}</code></span> <span><code>{citta}</code></span> <span><code>{orari}</code></span> <span><code>{social}</code></span> <span><code>{stima}</code> <em>(tempo indicativo; vuoto se la stima è spenta o manca)</em></span></p>`,
		`<p id="template-note-wa_oreficeria" class="sotto testi-codici">Nota: <code>{lavorazione}</code><code>{importo}</code><code>{consegna}</code><code>{note_riga}</code> includono già il ritorno a capo se valorizzati (o sono vuoti).</p>`,
		`<p id="template-usage-wa_oreficeria_lotto" class="sotto">Inviato come messaggio unico quando si chiude un lotto di pronte da ritirare per un&#39;oreficeria B2B.</p>`,
		`<textarea id="template-text-wa_oreficeria" name="template[text]" rows="9">`,
		`<textarea id="template-text-email_pronta_testo" name="template[text]" rows="8">`,
		`<h3>Email di conferma — testo</h3>`)
	nonContiene(t, "pagina", c, "template-error-", "<code>{telegram_optin}</code></span>", "flash-errore")
	// L'ordine delle famiglie.
	ultimo := -1
	for _, f := range buste.Famiglie {
		i := strings.Index(c, `<section id="template-section-`+string(f)+`"`)
		if i <= ultimo {
			t.Fatalf("ordine: %s", f)
		}
		ultimo = i
		if casellaTesto(t, c, f) != f.Predefinito() {
			t.Errorf("%s: casella", f)
		}
	}
}

// inventario: CLI-38, ADM-13
// «Salva il testo»: la pagina torna con «Testo salvato: <etichetta>.» e la casella con il testo, che il
// popup poi propone (con il numero della D-82). Un segnaposto non ammesso: la pagina con «Testo non
// salvato: Segnaposto non supportato: {password}», il testo lasciato nella casella, il riquadro
// dell'errore e niente anteprima; niente scritto. «Ripristina predefinito»: «Testo predefinito
// ripristinato: <etichetta>.» e il predefinito nella casella. Le righe del browser (\r\n) diventano \n.
func TestSalvaERipristinaTesti(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaHome(t, d, o, "1/26", "", "")
	b := titolareDentro(t, h)
	b.vai("GET", "/settings/templates", nil, nil)
	r := salvaTesto(b, buste.FamLinkStato, "Ciao {nome},\r\nbusta {numero}: {link}")
	if r.codice != 302 || r.location != "/settings/templates#template-section-wa_link_stato" {
		t.Fatalf("salva %d %q", r.codice, r.location)
	}
	c := b.vai("GET", "/settings/templates", nil, nil).corpo
	contiene(t, "salvato", c, `<div role="status" class="flash flash-info">Testo salvato: Invia link stato cliente.</div>`)
	if v := casellaTesto(t, c, buste.FamLinkStato); v != "Ciao {nome},\nbusta {numero}: {link}" {
		t.Errorf("casella %q", v)
	}
	if n := contaSQL(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.template_saved' AND attore = 'titolare@esempio.it' AND id_risorsa = 'wa_link_stato'`); n != 1 {
		t.Errorf("audit %d", n)
	}
	// Il popup propone il testo salvato.
	dip := dipendenteDentro(t, h)
	dip.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "popup", dip.vai("GET", percorsoScheda(id)+"/avviso?mostra="+dip.segno, nil, nil).corpo,
		`<textarea id="notice-body" name="notice[body]" rows="8">`+"\n"+`Ciao Cliente 1/26,`+"\n"+`busta 1/26: {link}</textarea>`)

	// Un segnaposto non ammesso.
	prima := contaRighe(t, d, `SELECT (SELECT count(*) FROM eventi_audit) * 1000 + (SELECT count(*) FROM impostazioni)`)
	r = salvaTesto(b, buste.FamPronta, "Frase privata {password}")
	if r.codice != 200 {
		t.Fatalf("rifiuto %d", r.codice)
	}
	contiene(t, "rifiuto", r.corpo, `<div role="alert" class="flash flash-errore">Testo non salvato: Segnaposto non supportato: {password}</div>`,
		`<div id="template-error-wa_pronta" class="avviso-problema">`+"\n"+`<strong>⚠️ Questo testo non è valido.</strong> Segnaposto non supportato: {password}`+"\n"+
			`<span class="sotto">Non si salva; se è già quello salvato, non parte nessun messaggio di questa famiglia finché non lo correggi o ripristini il predefinito.</span>`+"\n"+`</div>`,
		`<pre id="template-preview-wa_pronta">Nessuna anteprima: il testo non è valido.</pre>`)
	if casellaTesto(t, r.corpo, buste.FamPronta) != "Frase privata {password}" {
		t.Error("il testo non resta nella casella")
	}
	if casellaTesto(t, r.corpo, buste.FamLinkStato) != "Ciao {nome},\nbusta {numero}: {link}" {
		t.Error("le altre caselle non hanno il testo in vigore")
	}
	if contaRighe(t, d, `SELECT (SELECT count(*) FROM eventi_audit) * 1000 + (SELECT count(*) FROM impostazioni)`) != prima {
		t.Error("il rifiuto ha scritto")
	}
	// Troppo lungo, una regola delle email.
	r = salvaTesto(b, buste.FamPronta, strings.Repeat("a", buste.ModelloLunghezzaMassima+1))
	contiene(t, "lungo", r.corpo, `<div role="alert" class="flash flash-errore">Testo troppo lungo: al massimo 4000 caratteri.</div>`)
	r = salvaTesto(b, buste.FamEmailConfermaOggetto, "Riga uno\r\nriga due")
	contiene(t, "oggetto", r.corpo, `<div role="alert" class="flash flash-errore">Testo non salvato: L&#39;oggetto deve stare su una riga</div>`)

	// Ripristina predefinito.
	r = b.vai("POST", "/settings/templates/ripristina", url.Values{"_csrf_token": {b.token}, "template[family]": {"wa_link_stato"}, "template[text]": {"ignorato {password}"}}, nil)
	if r.codice != 302 || r.location != "/settings/templates#template-section-wa_link_stato" {
		t.Fatalf("ripristina %d %q", r.codice, r.location)
	}
	c = b.vai("GET", "/settings/templates", nil, nil).corpo
	contiene(t, "ripristinato", c, `<div role="status" class="flash flash-info">Testo predefinito ripristinato: Invia link stato cliente.</div>`)
	if casellaTesto(t, c, buste.FamLinkStato) != buste.FamLinkStato.Predefinito() {
		t.Error("predefinito non rimesso")
	}
	// Una casella vuota è un ripristino.
	salvaTesto(b, buste.FamLinkStato, "Altro {nome}")
	salvaTesto(b, buste.FamLinkStato, "  ")
	contiene(t, "vuota", b.vai("GET", "/settings/templates", nil, nil).corpo, `Testo predefinito ripristinato: Invia link stato cliente.`)
	// Una famiglia che non c'è: la pagina, niente scritto.
	prima = contaRighe(t, d, `SELECT (SELECT count(*) FROM eventi_audit) * 1000 + (SELECT count(*) FROM impostazioni)`)
	for _, p := range []string{"/settings/templates", "/settings/templates/ripristina"} {
		r = b.vai("POST", p, url.Values{"_csrf_token": {b.token}, "template[family]": {"wa_portale_orefice_invito"}, "template[text]": {"x"}}, nil)
		if r.codice != 302 || r.location != "/settings/templates" {
			t.Errorf("%s: famiglia sconosciuta %d %q", p, r.codice, r.location)
		}
	}
	if contaRighe(t, d, `SELECT (SELECT count(*) FROM eventi_audit) * 1000 + (SELECT count(*) FROM impostazioni)`) != prima {
		t.Error("una famiglia sconosciuta ha scritto")
	}
}

// inventario: CLI-38, ADM-13
// Un testo salvato che non vale più (scritto da altri o importato) resta nella sua casella con il
// riquadro dell'errore, e si ripristina.
func TestTestoSalvatoNonValido(t *testing.T) {
	h, d, _ := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.template.wa_artigiano', 'Ciao {password}', '2026-10-31T09:00:00.000000Z')`)
	b := titolareDentro(t, h)
	c := b.vai("GET", "/settings/templates", nil, nil).corpo
	contiene(t, "non valido", c, `<div id="template-error-wa_artigiano" class="avviso-problema">`, `<pre id="template-preview-wa_artigiano">Nessuna anteprima: il testo non è valido.</pre>`)
	if casellaTesto(t, c, buste.FamArtigiano) != "Ciao {password}" {
		t.Error("casella")
	}
	b.vai("POST", "/settings/templates/ripristina", url.Values{"_csrf_token": {b.token}, "template[family]": {"wa_artigiano"}}, nil)
	nonContiene(t, "ripristinato", b.vai("GET", "/settings/templates", nil, nil).corpo, "template-error-wa_artigiano")
}

// inventario: CLI-38, ADM-13
// Solo il titolare: il dipendente che apre la pagina o forza un salvataggio va alla home con «Operazione
// riservata al titolare.» e niente si scrive; senza CSRF 403.
func TestTestiSoloTitolare(t *testing.T) {
	h, d, _ := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	dip := dipendenteDentro(t, h)
	dip.vai("GET", "/", nil, nil)
	prima := contaRighe(t, d, `SELECT count(*) FROM impostazioni`)
	for _, r := range []risposta{dip.vai("GET", "/settings/templates", nil, nil), salvaTesto(dip, buste.FamPronta, "Ciao"),
		dip.vai("POST", "/settings/templates/ripristina", url.Values{"_csrf_token": {dip.token}, "template[family]": {"wa_pronta"}}, nil)} {
		if r.codice != 302 || r.location != "/" {
			t.Errorf("dipendente %d %q", r.codice, r.location)
		}
	}
	contiene(t, "home", dip.vai("GET", "/", nil, nil).corpo, `Operazione riservata al titolare.`)
	tit := titolareDentro(t, h)
	if r := tit.vai("POST", "/settings/templates", url.Values{"template[family]": {"wa_pronta"}, "template[text]": {"Ciao"}}, nil); r.codice != 403 {
		t.Errorf("senza CSRF %d", r.codice)
	}
	if contaRighe(t, d, `SELECT count(*) FROM impostazioni`) != prima {
		t.Error("scritto")
	}
}
