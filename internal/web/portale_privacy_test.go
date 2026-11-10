package web

import (
	"encoding/base64"
	"html"
	"image/color"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/boombuler/barcode/qr"
)

// Il portale ritirato delle oreficerie /m/ (CLI-53) e l'informativa /privacy (CLI-54) sulla faccia
// pubblica, come retired_portal_controller_test.exs e privacy_controller_test.exs dello standalone
// (go-baseline).

// profiloCompleto riscrive il profilo del negozio di prova con tutti i campi dell'intestazione.
func profiloCompleto(t *testing.T, p *provaAnteprima) {
	t.Helper()
	eseguiSQL(t, p.d, `UPDATE profilo_negozio SET nome = 'Bottega di Prova', ragione_sociale = 'Bottega Inventata S.r.l.',
		partita_iva = '01234567890', indirizzo = 'Via Finta 1', citta = 'Paese Finto', telefono = '0423 000111',
		email = 'info@bottega.example'`)
}

// inventario: CLI-53
// Ogni indirizzo /m/<codice>/… è il 410 «Portale non più attivo» con il nome e il telefono del
// negozio, le intestazioni delle pagine e nient'altro; il codice non si legge e non va nel registro.
func TestPortaleRitirato(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	profiloCompleto(t, p)
	b := nuovoBrowser(t, p.pubblica)
	for _, q := range []string{"/m/abc123", "/m/abc123/r/12", "/m/abc123/pair", "/m/abc123/r/12/foto/x.jpg", "/m/codice%2Fstrano/x"} {
		r := b.vai("GET", q, nil, nil)
		if r.codice != http.StatusGone {
			t.Fatalf("%s: %d", q, r.codice)
		}
		contiene(t, q, r.corpo, `<article id="retired-portal" class="ritirato-scheda">`, "<h1>Bottega di Prova</h1>",
			"<p>Il portale non è più attivo.</p>",
			"Per sapere a che punto sono le vostre buste chiedete al negozio: vi manderà il link\naggiornato quando sono pronte.",
			`<p id="retired-portal-phone">`, `Telefono: <a href="tel:0423 000111">0423 000111</a>`,
			"<title>Portale non più attivo · Laboratorio</title>")
		nonContiene(t, q, r.corpo, "abc123", "info@bottega.example", "Via Finta", `rel="manifest"`)
		for k, v := range map[string]string{"Content-Type": "text/html; charset=utf-8", "Cache-Control": cacheBase,
			"Content-Security-Policy": "base-uri 'self'; frame-ancestors 'self';", "Referrer-Policy": "strict-origin-when-cross-origin",
			"X-Content-Type-Options": "nosniff"} {
			if g := r.h.Get(k); g != v {
				t.Errorf("%s: %s = %q", q, k, g)
			}
		}
	}
	// La faccia interna non ha il portale.
	if r := nuovoBrowser(t, p.interna).vai("GET", "/m/abc123", nil, nil); r.codice != http.StatusNotFound {
		t.Errorf("interna: %d", r.codice)
	}
}

// inventario: CLI-53
// Il telefono si mostra se il profilo ne ha uno scritto, anche vuoto: profile && profile.phone dello
// standalone, dove una stringa vuota è vera.
func TestPortaleRitiratoTelefonoVuoto(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	eseguiSQL(t, p.d, `UPDATE profilo_negozio SET telefono = ''`)
	r := nuovoBrowser(t, p.pubblica).vai("GET", "/m/abc123", nil, nil)
	contiene(t, "telefono vuoto", r.corpo, `<p id="retired-portal-phone">`, `Telefono: <a href="tel:"></a>`)
	eseguiSQL(t, p.d, `UPDATE profilo_negozio SET telefono = NULL`)
	r = nuovoBrowser(t, p.pubblica).vai("GET", "/m/abc123", nil, nil)
	nonContiene(t, "telefono NULL", r.corpo, "retired-portal-phone")
}

// inventario: CLI-53
// Senza profilo del negozio la pagina c'è lo stesso, con «Negozio» e senza telefono.
func TestPortaleRitiratoSenzaProfilo(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	eseguiSQL(t, p.d, `DELETE FROM profilo_negozio`)
	r := nuovoBrowser(t, p.pubblica).vai("GET", "/m/abc123", nil, nil)
	if r.codice != http.StatusGone {
		t.Fatalf("%d", r.codice)
	}
	contiene(t, "senza profilo", r.corpo, "<h1>Negozio</h1>", "Il portale non è più attivo.")
	nonContiene(t, "senza profilo", r.corpo, "retired-portal-phone", "Telefono:")
}

// swPortale è @service_worker di RetiredPortalController, byte per byte.
const swPortale = `self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((key) => key.startsWith("portale-")).map((key) => caches.delete(key))))
      .then(() => self.registration.unregister())
  );
});
`

// inventario: CLI-53
// Il worker del portale installato: svuota le cache portale-* e si toglie; fuori dal pipeline delle
// pagine (niente sessione, niente intestazioni delle pagine), text/javascript e no-cache. Il codice
// del percorso non va nel registro.
func TestPortaleRitiratoServiceWorker(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	r := nuovoBrowser(t, p.pubblica).vai("GET", "/m/abc123/sw.js", nil, nil)
	if r.codice != 200 || r.corpo != swPortale {
		t.Fatalf("%d %q", r.codice, r.corpo)
	}
	if r.h.Get("Content-Type") != "text/javascript; charset=utf-8" || r.h.Get("Cache-Control") != "no-cache" {
		t.Errorf("tipo %q, cache %q", r.h.Get("Content-Type"), r.h.Get("Cache-Control"))
	}
	for _, k := range []string{"Set-Cookie", "Content-Security-Policy", "Referrer-Policy", "X-Content-Type-Options"} {
		if g := r.h.Values(k); len(g) != 0 {
			t.Errorf("%s: %q", k, g)
		}
	}
}

// inventario: CLI-53
// Il codice nel percorso non finisce nel registro, e le pagine /m/ non contano nel limite ai
// tentativi (PublicAttempts.family/1 non le ha): dopo 40 visite il link buono si apre.
func TestPortaleRitiratoRegistroELimite(t *testing.T) {
	p := paginaClienteProva(t)
	c := p.busta(t, nil)
	b := nuovoBrowser(t, p.h)
	b.ip = "203.0.113.50"
	for i := range 40 {
		r := b.vai("GET", "/m/codicesegretoportale"+strconv.Itoa(i)+"/r/1", nil, nil)
		if r.codice != http.StatusGone {
			t.Fatalf("%d", r.codice)
		}
	}
	if r := b.vai("GET", "/m/codicesegretoportale/sw.js", nil, nil); r.codice != 200 {
		t.Fatalf("sw.js %d", r.codice)
	}
	if r := b.vai("GET", "/r/"+c.Token, nil, nil); r.codice != 200 {
		t.Errorf("link buono dopo /m/: %d", r.codice)
	}
	if strings.Contains(p.reg.String(), "codicesegretoportale") {
		t.Error("il codice del portale nel registro")
	}
	contiene(t, "registro", p.reg.String(), `rotta="GET /m/{codice}/{resto...}"`, `rotta="GET /m/{codice}/sw.js"`)
}

// inventario: CLI-54
// L'intestazione del titolare dal profilo: la ragione sociale (non il nome), P.IVA, sede, contatti;
// il titolo della finestra con il nome del negozio. Nessuna riga d'audit.
func TestPrivacyIntestazione(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	profiloCompleto(t, p)
	audit0 := contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit`)
	r := nuovoBrowser(t, p.pubblica).vai("GET", "/privacy", nil, nil)
	if r.codice != 200 {
		t.Fatalf("%d", r.codice)
	}
	contiene(t, "privacy", r.corpo, "<title>Privacy — Bottega di Prova · Laboratorio</title>", "<h1>Informativa Privacy</h1>",
		`<p id="privacy-titolare"><b>Titolare del trattamento:</b>`+"\nBottega Inventata S.r.l.<span> — Identificativo fiscale 01234567890</span><br>\n"+
			"<span>Sede: Via Finta 1, Paese Finto<br></span>\n<span>Contatti per l'esercizio dei diritti: tel. 0423 000111 · email info@bottega.example</span>\n</p>",
		"contattando il titolare\n<span>ai recapiti sopra indicati</span>.")
	nonContiene(t, "privacy", testoDi(r.corpo, "privacy-titolare"), "Bottega di Prova")
	nonContiene(t, "privacy", r.corpo, "direttamente in negozio")
	if n := contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit`); n != audit0 {
		t.Errorf("righe d'audit: %d", n-audit0)
	}
	for k, v := range map[string]string{"Content-Type": "text/html; charset=utf-8", "Cache-Control": cacheBase,
		"Content-Security-Policy": "base-uri 'self'; frame-ancestors 'self';", "Referrer-Policy": "strict-origin-when-cross-origin"} {
		if g := r.h.Get(k); g != v {
			t.Errorf("%s = %q", k, g)
		}
	}
}

// testoDi è il pezzo di corpo dall'elemento con quell'id fino alla sua prima chiusura </p>.
func testoDi(corpo, id string) string {
	i := strings.Index(corpo, `id="`+id+`"`)
	if i < 0 {
		return ""
	}
	j := strings.Index(corpo[i:], "</p>")
	if j < 0 {
		return corpo[i:]
	}
	return corpo[i : i+j]
}

// inventario: CLI-54
// Ogni campo vuoto o assente toglie la sua etichetta: con il solo nome «Bottega di Prova» e nessun
// contatto i diritti si esercitano «direttamente in negozio»; senza profilo il titolare è «Il negozio»
// e il titolo «Privacy — Negozio».
func TestPrivacyCampiAssenti(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	eseguiSQL(t, p.d, `UPDATE profilo_negozio SET ragione_sociale = '', partita_iva = '', indirizzo = NULL, citta = '', telefono = '', email = NULL`)
	r := nuovoBrowser(t, p.pubblica).vai("GET", "/privacy", nil, nil)
	contiene(t, "solo il nome", r.corpo, `<p id="privacy-titolare"><b>Titolare del trattamento:</b>`+"\nBottega di Prova<br>\n</p>",
		"contattando il titolare\n<span>direttamente in negozio</span>.")
	nonContiene(t, "solo il nome", r.corpo, "Identificativo fiscale", "Sede:", "Contatti per l", "ai recapiti sopra indicati")
	// La sede senza telefono né email: niente contatti, quindi «direttamente in negozio».
	eseguiSQL(t, p.d, `UPDATE profilo_negozio SET indirizzo = 'Via Finta 1'`)
	r = nuovoBrowser(t, p.pubblica).vai("GET", "/privacy", nil, nil)
	contiene(t, "sede senza contatti", r.corpo, "<span>Sede: Via Finta 1<br></span>", "contattando il titolare\n<span>direttamente in negozio</span>.")
	nonContiene(t, "sede senza contatti", r.corpo, "ai recapiti sopra indicati", "Contatti per l")
	eseguiSQL(t, p.d, `UPDATE profilo_negozio SET indirizzo = NULL`)
	// Solo la città: la sede è la città da sola; solo l'email: i contatti sono l'email.
	eseguiSQL(t, p.d, `UPDATE profilo_negozio SET citta = 'Paese Finto', email = 'info@bottega.example'`)
	r = nuovoBrowser(t, p.pubblica).vai("GET", "/privacy", nil, nil)
	contiene(t, "città ed email", r.corpo, "<span>Sede: Paese Finto<br></span>", "Contatti per l'esercizio dei diritti: email info@bottega.example</span>")
	// Il nome vuoto non è un titolare: «Il negozio».
	eseguiSQL(t, p.d, `UPDATE profilo_negozio SET nome = '', citta = NULL, email = NULL`)
	r = nuovoBrowser(t, p.pubblica).vai("GET", "/privacy", nil, nil)
	contiene(t, "nome vuoto", r.corpo, "<b>Titolare del trattamento:</b>\nIl negozio<br>", "<title>Privacy — Negozio · Laboratorio</title>")
	eseguiSQL(t, p.d, `DELETE FROM profilo_negozio`)
	r = nuovoBrowser(t, p.pubblica).vai("GET", "/privacy", nil, nil)
	if r.codice != 200 {
		t.Fatalf("%d", r.codice)
	}
	contiene(t, "senza profilo", r.corpo, "<b>Titolare del trattamento:</b>\nIl negozio<br>", "<title>Privacy — Negozio · Laboratorio</title>",
		"direttamente in negozio")
}

// inventario: CLI-54
// Pagina pubblica senza busta: niente manifest, worker, tavolozze o menu; nessun dato di una busta;
// solo sulla faccia pubblica.
func TestPrivacyPubblicaSenzaBuste(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	c := p.busta(t, nil)
	r := nuovoBrowser(t, p.pubblica).vai("GET", "/privacy", nil, nil)
	nonContiene(t, "privacy", r.corpo, `rel="manifest"`, "/sw.js", "app-tema", "data-theme-picker", "navbar", "gs-fab",
		"Ada Inventata", "333 987 6543", "1/26", "0045/26", "Ridurre anello", c.Token)
	if r := nuovoBrowser(t, p.interna).vai("GET", "/privacy", nil, nil); r.codice != http.StatusNotFound {
		t.Errorf("faccia interna: %d", r.codice)
	}
}

// Le frasi dell'informativa dello standalone (privacy_html/show.html.heex a go-baseline), a spazi
// compattati, che i test dello standalone fissano.
var frasiPrivacy = []string{
	"Dati raccolti per la riparazione", "art. 6.1.b GDPR", "art. 6.1.c GDPR", "foto", "firma", "WhatsApp", "indirizzo IP",
	"Vendite e garanzie", "artt. 15-22 GDPR", "Garante per la protezione dei dati personali", "cookie tecnici",
	"il numero della busta", "la descrizione", "non mostra il nome né il telefono",
	"Ogni visualizzazione di una foto e ogni scelta sul preventivo",
	"Registrazione assistita (dettatura)", "Miglioramento della dettatura", "Whisper", "L'audio non viene conservato",
	"ogni 500 buste", "altri negozi che usano lo stesso gestionale", "art. 6.1.f GDPR", "art. 21 GDPR",
	"sul server del gestionale, in Germania", "privati di nome, cognome e numeri di telefono",
	"sono cancellati 90 giorni dopo la disattivazione", "cancellato dopo l'uso e comunque entro 12 mesi",
	"i dettati vengono cancellati", "Ultimo aggiornamento: 2026-10-27",
}

// testoCompatto è il testo della pagina senza tag e con gli spazi compattati.
func testoCompatto(corpo string) string {
	return strings.Join(strings.Fields(htmlTesto(reTag.ReplaceAllString(corpo, " "))), " ")
}

func htmlTesto(s string) string {
	return strings.NewReplacer("&#39;", "'", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&#34;", `"`, "&quot;", `"`).Replace(s)
}

// inventario: CLI-54
// Il testo dello standalone: i trattamenti, i diritti, la data; niente fornitori di IA nel cloud né
// assistente; nessuna cancellazione automatica oltre quelle scritte (Telegram e dettati).
func TestPrivacyTesto(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	r := nuovoBrowser(t, p.pubblica).vai("GET", "/privacy", nil, nil)
	testo := testoCompatto(r.corpo)
	for _, f := range frasiPrivacy {
		if !strings.Contains(testo, f) {
			t.Errorf("manca %q", f)
		}
	}
	for _, x := range []string{"double opt-in", "Anthropic", "Clausole Contrattuali", "SCC", "Google", "Assistente FAQ",
		"Ogni apertura", "10 anni", "onclick", "window.print"} {
		if strings.Contains(r.corpo, x) {
			t.Errorf("c'è %q", x)
		}
	}
	telegram := testoCompatto(sezione(r.corpo, "privacy-telegram-notice"))
	dettati := testoCompatto(sezione(r.corpo, "privacy-dettati"))
	resto := strings.ReplaceAll(strings.ReplaceAll(testoCompatto(sezione(r.corpo, "informativa-print")), telegram, ""), dettati, "")
	for _, x := range []string{"sono cancellati", "è cancellato", "vengono cancellati"} {
		if strings.Contains(resto, x) {
			t.Errorf("cancellazione promessa fuori da Telegram e dettati: %q", x)
		}
	}
	for _, id := range []string{"privacy-email-notice", "privacy-telegram-notice", "privacy-dettatura", "privacy-dettati", "privacy-updated"} {
		contiene(t, id, r.corpo, `id="`+id+`"`)
	}
}

// sezione è l'elemento con quell'id fino alla chiusura del suo tag (stesso nome, contati i livelli).
func sezione(corpo, id string) string {
	i := strings.Index(corpo, `id="`+id+`"`)
	if i < 0 {
		return ""
	}
	inizio := strings.LastIndex(corpo[:i], "<")
	nome := regexp.MustCompile(`^<([a-z0-9]+)`).FindStringSubmatch(corpo[inizio:])[1]
	re := regexp.MustCompile(`<(/?)` + nome + `\b[^>]*>`)
	livello := 0
	for _, m := range re.FindAllStringSubmatchIndex(corpo[inizio:], -1) {
		if m[3] > m[2] {
			livello--
		} else {
			livello++
		}
		if livello == 0 {
			return corpo[inizio : inizio+m[1]]
		}
	}
	return corpo[inizio:]
}

var (
	reQRPrivacy  = regexp.MustCompile(`<img src="([^"]+)" alt="QR informativa privacy" width="120" height="120">`)
	reRettangolo = regexp.MustCompile(`<rect x="(\d+)" y="(\d+)" width="(\d+)" height="1"/>`)
)

// moduliQR legge i moduli scuri dall'SVG del QR (un rettangolo per tratto di riga, margine di
// MargineQR moduli) e li confronta con il QR di contenuto: stessa matrice, stessa correzione M.
func moduliQR(t *testing.T, svg, contenuto string) bool {
	t.Helper()
	codice, err := qr.Encode(contenuto, qr.M, qr.Unicode)
	if err != nil {
		t.Fatal(err)
	}
	n := codice.Bounds().Dx()
	scuri := map[[2]int]bool{}
	for _, m := range reRettangolo.FindAllStringSubmatch(svg, -1) {
		x, _ := strconv.Atoi(m[1])
		y, _ := strconv.Atoi(m[2])
		l, _ := strconv.Atoi(m[3])
		for i := range l {
			scuri[[2]int{x + i - 4, y - 4}] = true
		}
	}
	for r := range n {
		for c := range n {
			if (codice.At(c, r) == color.Black) != scuri[[2]int{c, r}] {
				return false
			}
		}
	}
	return len(scuri) > 0 && strings.Contains(svg, `viewBox="0 0 `+strconv.Itoa(n+8)+" "+strconv.Itoa(n+8)+`"`)
}

// inventario: CLI-54
// Il QR è quello dell'indirizzo pubblico configurato più /privacy, mai dell'Host della richiesta, con
// la sua didascalia; senza indirizzo configurato niente QR. La pagina si stampa su un A4: @page,
// il blocco isolato, il QR fuori dal flusso e il pulsante che non si stampa, con un ascoltatore
// delegato in app.js e non in linea.
func TestPrivacyQREStampa(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	r := nuovoBrowser(t, p.pubblica).vai("GET", "/privacy", nil, nil) // l'Host è 127.0.0.1:<porta>, non l'indirizzo configurato
	m := reQRPrivacy.FindStringSubmatch(r.corpo)
	if m == nil {
		t.Fatal("niente QR")
	}
	dati, ok := strings.CutPrefix(html.UnescapeString(m[1]), "data:image/svg+xml;base64,")
	if !ok {
		t.Fatalf("src %q", m[1])
	}
	svg, err := base64.StdEncoding.DecodeString(dati)
	if err != nil {
		t.Fatal(err)
	}
	if !moduliQR(t, string(svg), indirizzoPubblicoProva+"/privacy") {
		t.Errorf("il QR non è quello di %s/privacy", indirizzoPubblicoProva)
	}
	contiene(t, "privacy", r.corpo, `<div id="privacy-qr" class="solo-print">`, "<small>Inquadra per l'informativa completa online</small>",
		`<div id="informativa-print">`, "@page { size: A4; margin: 10mm; }", "@media print",
		`<button type="button" data-print-page class="no-print">`+"\n🖨 Stampa per esporre in negozio\n</button>")
	if strings.Count(r.corpo, `id="informativa-print"`) != 1 {
		t.Error("blocco della stampa")
	}
	q := facceAnteprima(t, "")
	r = nuovoBrowser(t, q.pubblica).vai("GET", "/privacy", nil, nil)
	nonContiene(t, "senza indirizzo", r.corpo, "privacy-qr", "data:image/svg")
	contiene(t, "senza indirizzo", r.corpo, "Informativa Privacy")
	b := nuovoBrowser(t, q.pubblica)
	js := assetDellaPagina(t, b, r.corpo, "js", "text/javascript; charset=utf-8")
	contiene(t, "app.js", js, `closest("[data-print-page]")`, "window.print()")
}
