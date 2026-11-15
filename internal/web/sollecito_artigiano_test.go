package web

import (
	"html"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/buste"
)

// «🔧 Sollecita artigiano» della scheda (slice 085), come il purpose :artisan di NoticePopup e
// RepairLive dello standalone (go-baseline; repair_notice_live_test.exs). Dati inventati.

var reSollecitaScheda = regexp.MustCompile(`<a id="repair-artisan-notify" href="(/repairs/\d+/avviso\?scopo=artigiano&amp;mostra=[A-Za-z0-9_-]{22})" class="pulsante-contorno" data-avviso>🔧 Sollecita artigiano</a>`)

// inventario: ART-23, CLI-29
// La scheda di una busta aperta con l'artigiano che ha il numero: «🔧 Sollecita artigiano» con il segno.
// Il popup: «🔧 Sollecita l'artigiano», «Busta 0045/26», WhatsApp al numero dell'artigiano con il suo
// nome, il testo wa_artigiano (numero senza «N.», D-82), il suggerimento del link dell'artigiano, i campi
// nascosti con l'artigiano. «Invia»: wa.me con il link /artigiano/<id>?t=, la riga verso l'artigiano, che
// la scheda mostra in «📨 Invii registrati» con « · all'artigiano».
func TestSollecitaArtigianoDallaScheda(t *testing.T) {
	p, c, _, _ := facceArtigiano(t)
	b := titolareDentro(t, p.interna)
	scheda := b.vai("GET", percorsoScheda(c.ID), nil, nil).corpo
	m := reSollecitaScheda.FindStringSubmatch(scheda)
	if m == nil {
		t.Fatalf("manca «🔧 Sollecita artigiano»:\n%s", scheda)
	}
	prima := contaRighe(t, p.d, `SELECT (SELECT count(*) FROM eventi_audit) * 1000 + (SELECT count(*) FROM messaggi_in_uscita)`)
	popup := b.vai("GET", html.UnescapeString(m[1]), nil, nil)
	if popup.codice != 200 {
		t.Fatalf("popup %d", popup.codice)
	}
	contiene(t, "popup", popup.corpo, `<h2 id="notice-title">🔧 Sollecita l&#39;artigiano</h2>`, `<p class="sotto">Busta 0045/26</p>`,
		`<span class="grassetto">WhatsApp</span> <span>a &#43;393331112222 (Laboratorio Esempio)</span>`,
		`<input name="scopo" type="hidden" value="artigiano"><input name="tipo" type="hidden" value="artisan">`,
		`<input name="artigiano" type="hidden" value="1">`,
		"Ciao Laboratorio Esempio, sollecito riparazione n° 0045/26\n",
		"{link} diventa il link dell&#39;artigiano all&#39;invio. Invia registra il messaggio in lista e apre WhatsApp.",
		"<title>Sollecita l&#39;artigiano")
	nonContiene(t, "popup", popup.corpo, "notice-link-missing", "notice-problem", "Ada Inventata", "333 987 6543")
	if contaRighe(t, p.d, `SELECT (SELECT count(*) FROM eventi_audit) * 1000 + (SELECT count(*) FROM messaggi_in_uscita)`) != prima {
		t.Error("aprire il popup ha scritto")
	}
	f := moduloAvviso(t, popup.corpo)
	f.Set("_csrf_token", b.token)
	f.Set("notice[body]", "Sollecito: {link}")
	r := b.vai("POST", percorsoScheda(c.ID)+"/avviso", f, nil)
	var testo string
	if err := p.d.Lettura.QueryRow(`SELECT testo FROM messaggi_in_uscita WHERE destinatario = 'artisan' AND indirizzo = '+393331112222'`).Scan(&testo); err != nil {
		t.Fatal(err)
	}
	pref := "Sollecito: " + indirizzoPubblicoProva + "/artigiano/" + strconv.FormatInt(c.ID, 10) + "?t=g1."
	if !strings.HasPrefix(testo, pref) || r.codice != 302 || r.location != buste.IndirizzoWaMe("+393331112222", testo) {
		t.Fatalf("invio %d %q %q", r.codice, r.location, testo)
	}
	contiene(t, "invii", b.vai("GET", percorsoScheda(c.ID), nil, nil).corpo, "<span> · all'artigiano</span>")
}

// inventario: ART-23
// Senza il pulsante: un artigiano senza numero (con la riga «Nessun telefono per l'artigiano…»), una busta
// chiusa, una busta senza artigiano. Un popup forzato su una busta chiusa dice «La busta è chiusa:
// l'artigiano non si sollecita.» con «Chiudi»; su un artigiano senza numero «Nessun telefono…». Un «Invia»
// dopo un cambio d'artigiano torna alla scheda con il messaggio dello standalone e non scrive.
func TestSollecitaArtigianoRifiuti(t *testing.T) {
	p, c, _, _ := facceArtigiano(t)
	b := titolareDentro(t, p.interna)
	scheda := b.vai("GET", percorsoScheda(c.ID), nil, nil).corpo
	popup := b.vai("GET", html.UnescapeString(reSollecitaScheda.FindStringSubmatch(scheda)[1]), nil, nil).corpo
	f := moduloAvviso(t, popup)
	f.Set("_csrf_token", b.token)
	eseguiSQL(t, p.d, `UPDATE buste SET artigiano_id = 2 WHERE id = ?`, c.ID)
	scheda = b.vai("GET", percorsoScheda(c.ID), nil, nil).corpo
	nonContiene(t, "senza numero", scheda, "repair-artisan-notify")
	contiene(t, "senza numero", scheda, `id="repair-artisan-send-missing"`)
	contiene(t, "popup senza numero", b.vai("GET", percorsoScheda(c.ID)+"/avviso?scopo=artigiano", nil, nil).corpo,
		`<div id="notice-problem" class="avviso-problema">Nessun telefono per l&#39;artigiano: il sollecito su WhatsApp non può partire.</div>`)
	prima := contaRighe(t, p.d, `SELECT (SELECT count(*) FROM eventi_audit) * 1000 + (SELECT count(*) FROM messaggi_in_uscita)`)
	r := b.vai("POST", percorsoScheda(c.ID)+"/avviso", f, nil)
	if !redirectAllaScheda(r, c.ID) {
		t.Fatalf("rifiuto %d %q", r.codice, r.location)
	}
	contiene(t, "rifiuto", b.vai("GET", r.location, nil, nil).corpo,
		`<div role="alert" class="flash flash-errore">L&#39;artigiano della busta è cambiato mentre il messaggio era aperto: niente è stato inviato. Riaprilo.</div>`)
	if contaRighe(t, p.d, `SELECT (SELECT count(*) FROM eventi_audit) * 1000 + (SELECT count(*) FROM messaggi_in_uscita)`) != prima {
		t.Error("il rifiuto ha scritto")
	}
	eseguiSQL(t, p.d, `UPDATE buste SET artigiano_id = 1, stato = 'consegnata' WHERE id = ?`, c.ID)
	nonContiene(t, "chiusa", b.vai("GET", percorsoScheda(c.ID), nil, nil).corpo, "repair-artisan-notify", "repair-artisan-send-missing")
	chiusa := b.vai("GET", percorsoScheda(c.ID)+"/avviso?scopo=artigiano", nil, nil).corpo
	contiene(t, "popup chiusa", chiusa, `<div id="notice-problem" class="avviso-problema">La busta è chiusa: l&#39;artigiano non si sollecita.</div>`,
		`>Chiudi</a>`)
	nonContiene(t, "popup chiusa", chiusa, "notice-form")
	// Il segno del modulo è già stato usato dal primo rifiuto: si torna alla scheda senza segno.
	r = b.vai("POST", percorsoScheda(c.ID)+"/avviso", f, nil)
	if r.codice != 302 || r.location != percorsoScheda(c.ID) {
		t.Fatalf("chiusa %d %q", r.codice, r.location)
	}
	contiene(t, "rifiuto chiusa", b.vai("GET", r.location, nil, nil).corpo,
		`<div role="alert" class="flash flash-errore">La busta è chiusa: niente è stato inviato.</div>`)
	eseguiSQL(t, p.d, `UPDATE buste SET artigiano_id = NULL, stato = 'in_lavorazione' WHERE id = ?`, c.ID)
	nonContiene(t, "senza artigiano", b.vai("GET", percorsoScheda(c.ID), nil, nil).corpo, "repair-artisan-notify")
	// Un modulo forzato senza artigiano su una busta senza artigiano: refusal(:no_address), niente scritto.
	f.Del("artigiano")
	prima = contaRighe(t, p.d, `SELECT (SELECT count(*) FROM eventi_audit) * 1000 + (SELECT count(*) FROM messaggi_in_uscita)`)
	r = b.vai("POST", percorsoScheda(c.ID)+"/avviso", f, nil)
	if contaRighe(t, p.d, `SELECT (SELECT count(*) FROM eventi_audit) * 1000 + (SELECT count(*) FROM messaggi_in_uscita)`) != prima {
		t.Error("il modulo forzato ha scritto")
	}
	contiene(t, "senza artigiano", b.vai("GET", r.location, nil, nil).corpo,
		`<div role="alert" class="flash flash-errore">Nessun recapito per questo messaggio: niente è stato inviato.</div>`)
}

// inventario: ART-23
// Senza indirizzo_pubblico (D-46 estesa): il popup dice che {link} non si può usare e cosa configurare;
// «Invia» con {link} ridisegna il popup con il testo e «Messaggio non inviato: manca …», niente scritto;
// senza {link} parte.
func TestSollecitaArtigianoSenzaIndirizzoPubblico(t *testing.T) {
	p := facceAnteprima(t, "")
	p.b.ConLinkArtigiano([]byte("sottochiave-artigiano-SINTETICA-di-prova-32b"), standaloneArtigiano)
	eseguiSQL(t, p.d, `INSERT INTO artigiani (id, nome, telefono) VALUES (1, 'Laboratorio Esempio', '333 111 2222')`)
	c := p.busta(t, nil)
	eseguiSQL(t, p.d, `UPDATE buste SET artigiano_id = 1 WHERE id = ?`, c.ID)
	b := titolareDentro(t, p.interna)
	b.vai("GET", percorsoScheda(c.ID), nil, nil)
	popup := b.vai("GET", percorsoScheda(c.ID)+"/avviso?scopo=artigiano&mostra="+b.segno, nil, nil).corpo
	contiene(t, "popup", popup, `<p id="notice-link-missing" class="avviso-attenzione">{link} non si può usare: manca «indirizzo_pubblico`,
		`. Togli {link} dal testo per mandarlo senza link.</p>`)
	nonContiene(t, "popup", popup, "{telegram_optin} non")
	f := moduloAvviso(t, popup)
	f.Set("_csrf_token", b.token)
	prima := contaRighe(t, p.d, `SELECT (SELECT count(*) FROM eventi_audit) * 1000 + (SELECT count(*) FROM messaggi_in_uscita)`)
	r := b.vai("POST", percorsoScheda(c.ID)+"/avviso", f, nil)
	if r.codice != 200 {
		t.Fatalf("senza indirizzo %d %q", r.codice, r.location)
	}
	contiene(t, "ridisegno", r.corpo, `flash-errore">Messaggio non inviato: manca «indirizzo_pubblico`, `<input name="artigiano" type="hidden" value="1">`,
		`<input name="tipo" type="hidden" value="artisan">`, "Dettagli e foto: {link}")
	if contaRighe(t, p.d, `SELECT (SELECT count(*) FROM eventi_audit) * 1000 + (SELECT count(*) FROM messaggi_in_uscita)`) != prima {
		t.Error("ha scritto")
	}
	f = moduloAvviso(t, r.corpo)
	f.Set("_csrf_token", b.token)
	f.Set("notice[body]", "Solo testo")
	if r := b.vai("POST", percorsoScheda(c.ID)+"/avviso", f, nil); r.codice != 302 || r.location != buste.IndirizzoWaMe("+393331112222", "Solo testo") {
		t.Errorf("senza link %d %q", r.codice, r.location)
	}
}
