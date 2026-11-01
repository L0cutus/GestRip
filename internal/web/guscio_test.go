package web

import (
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/accounts"
)

// Il guscio delle pagine interne: menu e navigazione (ADM-22), ingresso «Impostazioni» con le
// schede (ADM-01), tavolozze (ADM-08) e la parte di PWA che serve al guscio (ADM-07). Portano
// l'intento di navigation_consistency_test.exs, activity_navigation_test.exs,
// settings_navigation_test.exs, floating_buttons_test.exs, pwa_test.exs e pwa_surface_test.exs
// dello standalone (go-baseline), e di close_outside_live_test.exs la parte dei menu: la chiusura
// automatica dei messaggi verdi ([data-autoclose], FlashAutoClose) è rimandata, e anche la parte
// della ricerca QR, che arriva con la lista delle buste.

// menuStandalone è @owner_links di navigation_consistency_test.exs, senza «Esci» (che nel Go è un
// modulo, non un link): tutte le voci, nell'ordine e con le etichette dello standalone.
var menuStandalone = [][2]string{
	{"/", "Riparazioni"},
	{"/sales", "Vendite"},
	{"/settings", "Impostazioni"},
	{"/audit", "Registro attività"},
	{"/handovers", "📤 Consegne"},
	{"/handovers/history", "📋 Storico consegne"},
	{"/handovers/reminders", "⏰ Solleciti pendenti"},
	{"/ready-lots", "📦 Avvisi oreficeria"},
	{"/emergenza", "🚨 Registro Emergenza"},
	{"/settings/anagrafiche", "Anagrafiche"},
	{"/labels", "🏷️ Etichette QR"},
	{"/statistiche", "📊 Statistiche"},
	{"/cestino", "🗑 Cestino"},
}

// schedeStandalone sono le schede di Layouts.settings/1 (e @sections di
// settings_navigation_test.exs), nell'ordine dello standalone.
var schedeStandalone = [][3]string{
	{"shop", "Negozio", "/settings/shop"},
	{"templates", "Messaggi WhatsApp", "/settings/templates"},
	{"providers", "Provider", "/settings/providers"},
	{"assistente", "Assistente", "/settings/assistente"},
	{"promemoria", "Promemoria", "/settings/promemoria"},
	{"utenti", "Utenti", "/settings/utenti"},
	{"cestino", "Cestino", "/settings/cestino"},
	{"retention-foto", "Foto", "/settings/retention-foto"},
}

var (
	reBloccoMenu  = regexp.MustCompile(`(?s)<ul id="(app|workshop)-mobile-menu"[^>]*>(.*?)</ul>`)
	reLinkMenu    = regexp.MustCompile(`(?s)<a href="([^"]*)"[^>]*>(.*?)</a>`)
	reNavigazione = regexp.MustCompile(`<details id="(app|workshop)-mobile-navigation"[^>]*>`)
	reFab         = regexp.MustCompile(`class="gs-fab[ "]`)
	reIDFab       = regexp.MustCompile(`id="(fab-[a-z]+)"[^>]*class="gs-fab[ "]`)
	reSchede      = regexp.MustCompile(`(?s)<nav id="settings-tabs"[^>]*>(.*?)</nav>`)
	reScheda      = regexp.MustCompile(`(?s)<a id="settings-tab-([a-z-]+)" href="([^"]*)"([^>]*)>(.*?)</a>`)
)

// comandoTema è il bottone «🎨 Cambia tema» di theme_picker_button/1, disegnato hidden e rivelato
// dal foglio delle tavolozze.
const comandoTema = `<button type="button" data-theme-picker-open aria-haspopup="dialog" hidden>🎨 <span>Cambia tema</span></button>`

// menuDi è la variante del menu e le sue voci (indirizzo, etichetta), in ordine.
func menuDi(t *testing.T, corpo string) (string, [][2]string, string) {
	t.Helper()
	m := reBloccoMenu.FindStringSubmatch(corpo)
	if m == nil {
		t.Fatal("la pagina non ha il menu ☰")
	}
	var voci [][2]string
	for _, l := range reLinkMenu.FindAllStringSubmatch(m[2], -1) {
		voci = append(voci, [2]string{l[1], strings.TrimSpace(l[2])})
	}
	return m[1], voci, m[2]
}

// testaDi è la parte della pagina fino a </head>.
func testaDi(corpo string) string {
	i := strings.Index(corpo, "</head>")
	if i < 0 {
		return ""
	}
	return corpo[:i]
}

func fabDi(corpo string) []string {
	var ids []string
	for _, m := range reIDFab.FindAllStringSubmatch(corpo, -1) {
		ids = append(ids, m[1])
	}
	slices.Sort(ids)
	return ids
}

// dentroCome è un browser entrato con l'account finto, nel ruolo dato, su una faccia interna con
// le rotte in più di extra.
func dentroCome(t *testing.T, ruolo string, extra func(*facciaWeb, *http.ServeMux)) (*browser, *utentiFinti) {
	t.Helper()
	u := &utentiFinti{attivo: true, ruolo: ruolo}
	h, _, err := costruisciInterna(dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, u), extra)
	if err != nil {
		t.Fatal(err)
	}
	b := nuovoBrowser(t, h)
	if r := b.entra(emailProva, passwordProva); !redirectA(r, "/") {
		t.Fatalf("accesso: %d %q", r.codice, r.location)
	}
	return b, u
}

// inventario: ADM-22
// La tabella del menu è quella dello standalone: tutte le voci, nell'ordine e con le etichette di
// navigation/1; «Impostazioni» e «Registro attività» solo del titolare; ogni voce porta la rotta
// Go che la fa comparire (GET dell'indirizzo); «Statistiche» è rimandata dopo la v1.
func TestTabellaMenuComeLoStandalone(t *testing.T) {
	var voci [][2]string
	for _, v := range vociMenu {
		voci = append(voci, [2]string{v.Href, v.Etichetta})
		rotta := "GET " + v.Href
		if v.Href == "/" {
			rotta = "GET /{$}"
		}
		if v.Rotta != rotta {
			t.Errorf("%s: rotta %q, attesa %q", v.Href, v.Rotta, rotta)
		}
		if titolare := v.Href == "/settings" || v.Href == "/audit"; v.SoloTitolare != titolare {
			t.Errorf("%s: solo titolare = %v", v.Href, v.SoloTitolare)
		}
		if v.Rimandata != (v.Href == "/statistiche") {
			t.Errorf("%s: rimandata = %v", v.Href, v.Rimandata)
		}
	}
	if !slices.Equal(voci, menuStandalone) {
		t.Errorf("voci del menu:\n%v\nattese:\n%v", voci, menuStandalone)
	}
}

// inventario: ADM-22
// Come «owner sees the same complete ordered menu in every shell»: il titolare vede lo stesso menu
// su ogni pagina interna, una sola volta, con le voci delle rotte che il Go ha, «Cambia tema» ed
// «Esci» (un modulo POST con il token, che funziona senza JavaScript). La pagina iniziale ha il
// menu della bottega (workshop-…), le altre quello dell'app (app-…), come lo standalone.
func TestMenuDelTitolare(t *testing.T) {
	b, _ := dentroCome(t, accounts.Titolare, nil)
	atteso := [][2]string{{"/", "Riparazioni"}, {"/settings", "Impostazioni"}, {"/audit", "Registro attività"}}
	for percorso, variante := range map[string]string{"/": "workshop", "/audit": "app", "/settings/utenti": "app"} {
		r := b.vai("GET", percorso, nil, nil)
		if r.codice != 200 {
			t.Fatalf("%s: %d", percorso, r.codice)
		}
		v, voci, blocco := menuDi(t, r.corpo)
		if v != variante {
			t.Errorf("%s: variante %q, attesa %q", percorso, v, variante)
		}
		if !slices.Equal(voci, atteso) {
			t.Errorf("%s: menu %v, atteso %v", percorso, voci, atteso)
		}
		if n := len(reNavigazione.FindAllString(r.corpo, -1)); n != 1 {
			t.Errorf("%s: %d menu ☰", percorso, n)
		}
		for _, s := range []string{`<details id="` + v + `-mobile-navigation"`, `<summary id="` + v + `-mobile-menu-button" aria-label="Menu"`} {
			if !strings.Contains(r.corpo, s) {
				t.Errorf("%s: manca %q", percorso, s)
			}
		}
		// Esci: modulo POST /logout con il token, dentro il menu, dopo «Cambia tema».
		esci := regexp.MustCompile(`(?s)<form action="/logout" method="post">\s*<input name="_csrf_token" type="hidden" value="` + regexp.QuoteMeta(b.token) + `">\s*<button[^>]*>Esci</button>`)
		tema := strings.Index(blocco, comandoTema)
		if loc := esci.FindStringIndex(blocco); loc == nil || tema < 0 || tema > loc[0] {
			t.Errorf("%s: nel menu manca «Cambia tema» seguito da «Esci» con il token", percorso)
		}
		if strings.Count(r.corpo, comandoTema) != 1 {
			t.Errorf("%s: il comando del tema non c'è una volta sola", percorso)
		}
		if strings.Count(r.corpo, `action="/logout"`) != 1 {
			t.Errorf("%s: «Esci» non c'è una volta sola", percorso)
		}
	}
}

// inventario: ADM-22
// Come «staff sees the same ordinary menu and cannot force owner pages» e «staff has neither menu
// entry nor forced URL access»: il dipendente non vede «Impostazioni» né «Registro attività», e
// aprendo a mano /settings o /audit torna a / con «Operazione riservata al titolare.».
func TestMenuDelDipendente(t *testing.T) {
	b, _ := dentroCome(t, accounts.Dipendente, nil)
	r := b.vai("GET", "/", nil, nil)
	v, voci, _ := menuDi(t, r.corpo)
	if v != "workshop" || !slices.Equal(voci, [][2]string{{"/", "Riparazioni"}}) {
		t.Errorf("menu del dipendente: %s %v", v, voci)
	}
	if strings.Contains(r.corpo, `href="/settings"`) || strings.Contains(r.corpo, `href="/audit"`) {
		t.Error("il dipendente vede un link del titolare")
	}
	if !strings.Contains(r.corpo, comandoTema) || !strings.Contains(r.corpo, `action="/logout"`) {
		t.Error("il dipendente non ha «Cambia tema» o «Esci»")
	}
	for _, p := range []string{"/settings", "/audit", "/settings/utenti"} {
		if r := b.vai("GET", p, nil, nil); !redirectA(r, "/") {
			t.Errorf("%s: %d %q", p, r.codice, r.location)
		}
		if r := b.vai("GET", "/", nil, nil); !strings.Contains(r.corpo, msgSoloTitolare) {
			t.Errorf("%s: manca «%s»", p, msgSoloTitolare)
		}
	}
}

// inventario: ADM-22
// Il menu cresce con le aree: una rotta che arriva fa comparire la sua voce al suo posto, per il
// ruolo giusto, senza toccare il menu; «Statistiche» (dopo la v1) resta fuori anche se la rotta
// c'è. Nessuna voce porta a una rotta che il Go non ha.
func TestMenuCresceConLeRotte(t *testing.T) {
	extra := func(f *facciaWeb, mux *http.ServeMux) {
		for _, p := range []string{"GET /cestino", "GET /sales", "GET /statistiche", "GET /handovers"} {
			f.rottaConGuscio(mux, p, personale, f.home)
		}
	}
	atteso := map[string][][2]string{
		accounts.Titolare: {{"/", "Riparazioni"}, {"/sales", "Vendite"}, {"/settings", "Impostazioni"}, {"/audit", "Registro attività"},
			{"/handovers", "📤 Consegne"}, {"/cestino", "🗑 Cestino"}},
		accounts.Dipendente: {{"/", "Riparazioni"}, {"/sales", "Vendite"}, {"/handovers", "📤 Consegne"}, {"/cestino", "🗑 Cestino"}},
	}
	for ruolo, voci := range atteso {
		b, _ := dentroCome(t, ruolo, extra)
		for _, p := range []string{"/", "/sales"} {
			r := b.vai("GET", p, nil, nil)
			if _, v, _ := menuDi(t, r.corpo); !slices.Equal(v, voci) {
				t.Errorf("%s su %s: %v, atteso %v", ruolo, p, v, voci)
			}
		}
	}
	// Senza rotte in più, ogni link del menu del titolare è una rotta registrata.
	_, f, err := costruisciInterna(dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, &utentiFinti{attivo: true}), nil)
	if err != nil {
		t.Fatal(err)
	}
	registrate := map[string]bool{}
	for _, r := range f.rotte {
		registrate[r.pattern] = true
	}
	for _, v := range vociMenu {
		if registrate[v.Rotta] && !v.Rimandata && !slices.ContainsFunc(f.menu, func(m voceMenu) bool { return m.Href == v.Href }) {
			t.Errorf("la rotta %s c'è ma la voce %q no", v.Rotta, v.Etichetta)
		}
	}
	for _, m := range f.menu {
		if !registrate[m.Rotta] {
			t.Errorf("la voce %q porta a %s, che non c'è", m.Etichetta, m.Rotta)
		}
	}
}

// inventario: ADM-22
// Come «the ☰ menu … closes outside» e «only the menus close outside»: il solo menu porta
// data-close-outside; lo script della pagina lo chiude toccando fuori e con Esc, e rimpicciolisce
// i pulsanti flottanti scorrendo (gs-fabs-shrunk su <html>). Il menu è un <details>: si apre e si
// chiude anche senza JavaScript.
func TestMenuSiChiudeFuori(t *testing.T) {
	b, _ := dentroCome(t, accounts.Titolare, nil)
	r := b.vai("GET", "/", nil, nil)
	if n := strings.Count(r.corpo, "data-close-outside"); n != 1 || !regexp.MustCompile(`<details id="workshop-mobile-navigation"[^>]*data-close-outside`).MatchString(r.corpo) {
		t.Errorf("data-close-outside: %d, o non sul menu", n)
	}
	js := assetDellaPagina(t, b, r.corpo, "js", "text/javascript; charset=utf-8")
	for _, s := range []string{`details[data-close-outside][open]`, `addEventListener("click"`, `addEventListener("keydown"`,
		`event.key !== "Escape"`, `gs-fabs-shrunk`, `addEventListener("scroll"`} {
		if !strings.Contains(js, s) {
			t.Errorf("lo script non contiene %q", s)
		}
	}
	// Il menu sta sopra la pagina e scorre sui telefoni (max-height con 100dvh, overflow-y auto).
	css := assetDellaPagina(t, b, r.corpo, "app-css", "text/css; charset=utf-8")
	if !regexp.MustCompile(`(?s)-mobile-menu[^{]*\{[^}]*max-height:\s*calc\(100dvh - 10rem\)[^}]*overflow-y:\s*auto`).MatchString(css) {
		t.Error("il menu non ha l'altezza limitata con lo scorrimento")
	}
}

// assetDellaPagina scarica il foglio o lo script che la pagina collega: tipo è "js", "app-css" o
// "schede-css". Controlla tipo, cache e che non apra una sessione.
func assetDellaPagina(t *testing.T, b *browser, corpo, tipo, contentType string) string {
	t.Helper()
	re := map[string]*regexp.Regexp{
		"js":         regexp.MustCompile(`<script defer src="(/assets/app-[0-9a-f]{12}\.js)"></script>`),
		"app-css":    regexp.MustCompile(`<link rel="stylesheet" href="(/assets/app-[0-9a-f]{12}\.css)">`),
		"schede-css": regexp.MustCompile(`<link rel="stylesheet" href="(/assets/settings-tabs-[0-9a-f]{12}\.css)">`),
	}[tipo]
	m := re.FindStringSubmatch(testaDi(corpo))
	if m == nil {
		t.Fatalf("la testa della pagina non collega %s", tipo)
	}
	nb := nuovoBrowser(t, b.srv.Config.Handler) // senza cookie: gli asset si servono a chiunque
	r := nb.vai("GET", m[1], nil, nil)
	if r.codice != 200 || r.h.Get("Content-Type") != contentType || r.h.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Errorf("%s: %d %q %q", m[1], r.codice, r.h.Get("Content-Type"), r.h.Get("Cache-Control"))
	}
	if r.h.Get("Set-Cookie") != "" {
		t.Errorf("%s apre una sessione", m[1])
	}
	// Anche con una sessione aperta l'asset non tocca il cookie.
	if rs := b.vai("GET", m[1], nil, nil); rs.codice != 200 || rs.h.Get("Set-Cookie") != "" {
		t.Errorf("%s con la sessione: %d, Set-Cookie %q", m[1], rs.codice, rs.h.Get("Set-Cookie"))
	}
	return r.corpo
}

// inventario: ADM-22
// Gli asset sono nel binario, con il nome che cambia con il contenuto (cache di un anno); un nome
// che non c'è è il 404 dello standalone.
func TestAssetStatici(t *testing.T) {
	b, _ := dentroCome(t, accounts.Titolare, nil)
	r := b.vai("GET", "/", nil, nil)
	css := assetDellaPagina(t, b, r.corpo, "app-css", "text/css; charset=utf-8")
	assetDellaPagina(t, b, r.corpo, "js", "text/javascript; charset=utf-8")
	assetDellaPagina(t, b, r.corpo, "schede-css", "text/css; charset=utf-8")
	// floating_buttons_test.exs: pillole fisse che si rimpiccioliscono, sotto il visore delle foto.
	for _, s := range []string{".gs-fabs-shrunk .gs-fab", "safe-area-inset-bottom"} {
		if !strings.Contains(css, s) {
			t.Errorf("il foglio non contiene %q", s)
		}
	}
	if strings.Contains(css, ".gs-fab.shrunk") || !regexp.MustCompile(`\.gs-fab\s*\{[^}]*position:\s*fixed`).MatchString(css) {
		t.Error("le pillole non sono fisse o usano la classe sbagliata")
	}
	if m := regexp.MustCompile(`\.gs-fab\s*\{[^}]*z-index:\s*(\d+)`).FindStringSubmatch(css); m == nil {
		t.Error("le pillole non hanno z-index")
	} else if z, _ := strconv.Atoi(m[1]); z >= 60 {
		t.Errorf("z-index %d copre il visore delle foto (60)", z)
	}
	for _, p := range []string{"/assets/app-000000000000.css", "/assets/app.css", "/assets/nessuno.js"} {
		if r := b.vai("GET", p, nil, nil); r.codice != 404 {
			t.Errorf("%s = %d", p, r.codice)
		}
	}
}

// inventario: ADM-22
// Come floating_buttons_test.exs: «🏠 Lista» su ogni pagina interna (porta a /, che c'è), con lo
// spaziatore in fondo; nessun pulsante sul login, sul reset e sull'invito. «➕», «✏️ Edit» e
// «🔍 Filtri» non ci sono: le loro pagine non ci sono ancora.
func TestPulsantiFlottanti(t *testing.T) {
	b, _ := dentroCome(t, accounts.Titolare, nil)
	for _, p := range []string{"/", "/audit", "/settings/utenti"} {
		r := b.vai("GET", p, nil, nil)
		if f := fabDi(r.corpo); !slices.Equal(f, []string{"fab-home"}) {
			t.Errorf("%s: %v", p, f)
		}
		if !regexp.MustCompile(`<a href="/" id="fab-home" class="gs-fab gs-fab-right" title="Lista riparazioni">\s*<span class="gs-fab-icon" aria-hidden="true">🏠</span><span class="gs-fab-label">Lista</span>\s*</a>`).MatchString(r.corpo) ||
			!strings.Contains(r.corpo, `<div class="gs-fab-spacer" aria-hidden="true"></div>`) {
			t.Errorf("%s: «🏠 Lista» o lo spaziatore diversi dallo standalone", p)
		}
		if strings.Contains(r.corpo, `class="fab`) || strings.Contains(r.corpo, ` fab"`) {
			t.Errorf("%s: un pulsante con la classe fab di daisyUI", p)
		}
	}
	a := nuovoBrowser(t, internaProva(&sondaFinta{pronto: true}, nessunRegistro))
	for _, p := range []string{"/login", "/reset-request", "/invito/non-esiste", "/configura/non-esiste"} {
		if r := a.vai("GET", p, nil, nil); reFab.MatchString(r.corpo) {
			t.Errorf("%s ha pulsanti flottanti", p)
		}
	}
}

// inventario: ADM-01
// La tabella delle schede è quella di Layouts.settings/1, ognuna con la sua rotta Go.
func TestTabellaSchedeComeLoStandalone(t *testing.T) {
	var s [][3]string
	for _, x := range schedeImpostazioni {
		s = append(s, [3]string{x.Chiave, x.Etichetta, x.Href})
		if x.Rotta != "GET "+x.Href {
			t.Errorf("%s: rotta %q", x.Chiave, x.Rotta)
		}
	}
	if !slices.Equal(s, schedeStandalone) {
		t.Errorf("schede:\n%v\nattese:\n%v", s, schedeStandalone)
	}
}

// schedeDi sono le schede della pagina: chiave, indirizzo, etichetta e se è quella aperta.
func schedeDi(t *testing.T, corpo string) [][4]string {
	t.Helper()
	m := reSchede.FindStringSubmatch(corpo)
	if m == nil {
		t.Fatal("la pagina non ha le schede delle impostazioni")
	}
	var s [][4]string
	for _, x := range reScheda.FindAllStringSubmatch(m[1], -1) {
		aperta := ""
		if strings.Contains(x[3], `aria-current="page"`) {
			aperta = "aperta"
		}
		s = append(s, [4]string{x[1], x[2], strings.TrimSpace(x[4]), aperta})
	}
	return s
}

// inventario: ADM-01
// Come «both menus have one named entrance» e «every direct route carries the same complete
// navigation»: una sola voce «Impostazioni» (verso /settings, mai /settings/shop); /settings va
// alla prima scheda che il Go ha (oggi Utenti); la pagina della scheda ha il titolo, la barra con
// le schede che esistono e quella aperta segnata una volta, e il pannello legato alla scheda.
func TestIngressoImpostazioni(t *testing.T) {
	b, _ := dentroCome(t, accounts.Titolare, nil)
	r := b.vai("GET", "/", nil, nil)
	if strings.Count(r.corpo, `href="/settings"`) != 1 || strings.Contains(r.corpo, `href="/settings/shop"`) {
		t.Error("il menu non ha una sola voce «Impostazioni» verso /settings")
	}
	if r := b.vai("GET", "/settings", nil, nil); !redirectA(r, "/settings/utenti") {
		t.Errorf("/settings: %d %q", r.codice, r.location)
	}
	r = b.vai("GET", "/settings/utenti", nil, nil)
	if !regexp.MustCompile(`<p id="settings-title"[^>]*>Impostazioni</p>`).MatchString(r.corpo) ||
		!strings.Contains(r.corpo, `<nav id="settings-tabs" aria-label="Sezioni delle impostazioni" class="settings-tabs">`) ||
		!regexp.MustCompile(`<section id="settings-panel" aria-labelledby="settings-tab-utenti">(?s:.*)id="user-invite-form"(?s:.*)</section>`).MatchString(r.corpo) {
		t.Error("titolo, barra o pannello delle impostazioni diversi dallo standalone")
	}
	if s := schedeDi(t, r.corpo); !slices.Equal(s, [][4]string{{"utenti", "/settings/utenti", "Utenti", "aperta"}}) {
		t.Errorf("schede: %v", s)
	}
	if strings.Count(r.corpo, `aria-current="page"`) != 1 {
		t.Error("aria-current non è su una sola scheda")
	}
}

// inventario: ADM-01
// Le schede crescono con le rotte, nell'ordine dello standalone, e /settings va alla prima.
func TestSchedeCresconoConLeRotte(t *testing.T) {
	b, _ := dentroCome(t, accounts.Titolare, func(f *facciaWeb, mux *http.ServeMux) {
		f.rottaConGuscio(mux, "GET /settings/cestino", soloTitolare, f.home)
		f.rottaConGuscio(mux, "GET /settings/shop", soloTitolare, f.home)
	})
	if r := b.vai("GET", "/settings", nil, nil); !redirectA(r, "/settings/shop") {
		t.Errorf("/settings: %d %q", r.codice, r.location)
	}
	r := b.vai("GET", "/settings/utenti", nil, nil)
	atteso := [][4]string{{"shop", "/settings/shop", "Negozio", ""}, {"utenti", "/settings/utenti", "Utenti", "aperta"},
		{"cestino", "/settings/cestino", "Cestino", ""}}
	if s := schedeDi(t, r.corpo); !slices.Equal(s, atteso) {
		t.Errorf("schede: %v", s)
	}
}

// inventario: ADM-01
// Come «staff cannot enter settings even by forcing every URL» e «new entrance rechecks owner»:
// il ruolo si ricontrolla a ogni richiesta; un titolare retrocesso è fuori al clic dopo.
func TestImpostazioniSoloTitolare(t *testing.T) {
	b, u := dentroCome(t, accounts.Titolare, nil)
	if r := b.vai("GET", "/settings", nil, nil); r.codice != http.StatusFound {
		t.Fatalf("/settings del titolare: %d", r.codice)
	}
	u.cambiaRuolo(accounts.Dipendente)
	for _, p := range []string{"/settings", "/settings/utenti"} {
		if r := b.vai("GET", p, nil, nil); !redirectA(r, "/") {
			t.Errorf("%s dopo la retrocessione: %d %q", p, r.codice, r.location)
		}
	}
	a := nuovoBrowser(t, internaProva(&sondaFinta{pronto: true}, nessunRegistro))
	if r := a.vai("GET", "/settings", nil, nil); !redirectA(r, "/login") {
		t.Errorf("/settings senza accesso: %d %q", r.codice, r.location)
	}
}

// inventario: ADM-01
// Come «settings stylesheet is served and linked» e «each settings tab has its own colour»: il
// foglio delle schede (lo stesso dello standalone) ha la scheda aperta colorata, 44px di altezza e
// un colore diverso per ogni scheda.
func TestFoglioDelleSchede(t *testing.T) {
	b, _ := dentroCome(t, accounts.Titolare, nil)
	r := b.vai("GET", "/settings/utenti", nil, nil)
	css := assetDellaPagina(t, b, r.corpo, "schede-css", "text/css; charset=utf-8")
	if !strings.Contains(css, `.settings-tab[aria-current="page"]`) || !strings.Contains(css, "min-height: 44px") {
		t.Error("manca la scheda aperta o l'altezza di 44px")
	}
	colori := map[string]bool{}
	for _, s := range schedeStandalone {
		m := regexp.MustCompile(`#settings-tab-` + regexp.QuoteMeta(s[0]) + `\s*\{([^}]*)\}`).FindStringSubmatch(css)
		if m == nil {
			t.Errorf("nessun colore per %s", s[0])
			continue
		}
		c := regexp.MustCompile(`--tab-colour:\s*([^;]+);`).FindStringSubmatch(m[1])
		if c == nil || !regexp.MustCompile(`--tab-tint:\s*[^;]+;`).MatchString(m[1]) {
			t.Errorf("%s senza colore o tinta", s[0])
			continue
		}
		colori[c[1]] = true
	}
	if len(colori) != len(schedeStandalone) {
		t.Errorf("%d colori diversi per %d schede", len(colori), len(schedeStandalone))
	}
	if !regexp.MustCompile(`\.settings-tab\s*\{[^}]*border-top:[^;]*var\(--tab-colour\)`).MatchString(css) ||
		!regexp.MustCompile(`\.settings-tab\[aria-current="page"\]\s*\{[^}]*background:\s*var\(--tab-tint\)`).MatchString(css) {
		t.Error("la striscia o la tinta della scheda aperta mancano")
	}
}

// inventario: ADM-08
// Come «the table of themes» di pwa_test.exs: le otto tavolozze dello standalone nel suo ordine,
// le tre scure, quattro campioni per tavolozza nell'ordine pagina, superficie, accento, inchiostro.
func TestTavolozze(t *testing.T) {
	var ids []string
	for _, x := range temi {
		ids = append(ids, x.ID)
		if strings.TrimSpace(x.Etichetta) == "" {
			t.Errorf("%q senza etichetta", x.ID)
		}
		c := x.Campioni()
		if len(c) != 4 {
			t.Errorf("%q: %d campioni", x.ID, len(c))
		}
		for _, v := range c {
			if !regexp.MustCompile(`^#[0-9a-f]{6}$`).MatchString(v) {
				t.Errorf("%q: campione %q", x.ID, v)
			}
		}
	}
	if !slices.Equal(ids, []string{"", "sabbia", "chiaro", "marina", "smeraldo", "crepuscolo", "notte", "ambra"}) {
		t.Errorf("tavolozze: %q", ids)
	}
	scuri := temiScuri()
	slices.Sort(scuri)
	if !slices.Equal(scuri, []string{"ambra", "crepuscolo", "notte"}) {
		t.Errorf("scure: %q", scuri)
	}
	for _, x := range temi {
		if x.ID == "notte" && !slices.Equal(x.Campioni(), []string{"#0f172a", "#1e293b", "#d4a017", "#e5e7eb"}) {
			t.Errorf("campioni di notte: %q", x.Campioni())
		}
		if x.ID == "" && x.Etichetta != "Oro gestrip (default)" {
			t.Errorf("etichetta della predefinita: %q", x.Etichetta)
		}
	}
	if chiaveTema != "app-tema" {
		t.Errorf("chiave: %q", chiaveTema)
	}
}

// inventario: ADM-08
// Come «the stylesheet the table writes»: un blocco per ogni tavolozza con nome, la predefinita sul
// :root nudo, i valori dello standalone, il corpo dipinto con il colore della pagina, la scelta
// attiva segnata, il comando rivelato, color-scheme scuro sulle tre scure.
func TestFoglioDelleTavolozze(t *testing.T) {
	css := cssTemi()
	var dichiarate []string
	for _, m := range regexp.MustCompile(`:root\[data-tema="([^"]+)"\]`).FindAllStringSubmatch(css, -1) {
		if !slices.Contains(dichiarate, m[1]) {
			dichiarate = append(dichiarate, m[1])
		}
	}
	slices.Sort(dichiarate)
	if !slices.Equal(dichiarate, []string{"ambra", "chiaro", "crepuscolo", "marina", "notte", "sabbia", "smeraldo"}) {
		t.Errorf("blocchi: %q", dichiarate)
	}
	if !regexp.MustCompile(`:root\s*\{[^}]*--color-primary:\s*#b8960c`).MatchString(css) {
		t.Error("la predefinita non è sul :root nudo")
	}
	blocco := func(id string) string {
		m := regexp.MustCompile(`:root\[data-tema="` + id + `"\]\s*\{([^}]*)\}`).FindStringSubmatch(css)
		if m == nil {
			return ""
		}
		return m[1]
	}
	notte := blocco("notte")
	for _, s := range []string{"--color-base-200: #0f172a", "--color-base-100: #1e293b", "--color-base-content: #e5e7eb",
		"--color-base-300: #334155", "--color-primary: #d4a017", "color-scheme: dark"} {
		if !strings.Contains(notte, s) {
			t.Errorf("notte senza %q", s)
		}
	}
	if !strings.Contains(blocco("chiaro"), "color-scheme: light") {
		t.Error("chiaro non è color-scheme: light")
	}
	if !regexp.MustCompile(`body\s*\{[^}]*background-color:\s*var\(--color-base-200\)`).MatchString(css) {
		t.Error("il corpo non ha il colore della pagina")
	}
	for _, s := range []string{"[data-theme-choose][data-theme-active]", "border-width: 3px",
		"@layer base { [data-theme-picker-open][hidden] { display: inline-flex !important; } }"} {
		if !strings.Contains(css, s) {
			t.Errorf("manca %q", s)
		}
	}
	if strings.Contains(css, `data-tema=""`) {
		t.Error("la predefinita scritta come attributo vuoto")
	}
}

// inventario: ADM-08
// Come «the signed-in shell» di pwa_surface_test.exs: la pagina interna è chiara di partenza
// (data-theme="light" sul tag), la testa collega il manifest, l'icona, registra il worker, chiede
// l'oro per la barra, dà il nome del negozio a iOS, e uno script nella testa applica la tavolozza
// salvata (app-tema, data-tema) prima del disegno; un solo selettore con otto scelte.
func TestGuscioInterno(t *testing.T) {
	b, _ := dentroCome(t, accounts.Titolare, nil)
	for _, p := range []string{"/", "/audit", "/settings/utenti"} {
		r := b.vai("GET", p, nil, nil)
		testa := testaDi(r.corpo)
		for _, s := range []string{`<html lang="it" data-theme="light">`, `<meta name="theme-color" content="#b8960c">`,
			`<link rel="manifest" href="/manifest.webmanifest">`, `<link rel="apple-touch-icon" href="/branding/icon-192.png">`,
			`<meta name="apple-mobile-web-app-title" content="Negozio">`, `<link rel="icon" type="image/png" href="/branding/icon-192.png">`,
			`<meta name="mobile-web-app-capable" content="yes">`, `<meta name="apple-mobile-web-app-capable" content="yes">`,
			`<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">`,
			`navigator.serviceWorker.register("/sw.js", { scope: "/" })`, `"app-tema"`, "data-tema",
			`["crepuscolo","notte","ambra"]`, `--color-primary: #b8960c`} {
			if !strings.Contains(testa, s) {
				t.Errorf("%s: la testa non contiene %q", p, s)
			}
		}
		if n := strings.Count(r.corpo, "data-theme-picker\n") + strings.Count(r.corpo, "data-theme-picker "); n != 1 {
			t.Errorf("%s: %d selettori del tema", p, n)
		}
		if !strings.Contains(r.corpo, `<div id="theme-picker" role="dialog" aria-modal="true" aria-label="Scegli tema" hidden data-theme-picker`) {
			t.Errorf("%s: il selettore non è come lo standalone", p)
		}
		if n := strings.Count(r.corpo, "data-theme-choose="); n != 8 {
			t.Errorf("%s: %d scelte", p, n)
		}
		for _, x := range temi {
			if !strings.Contains(r.corpo, `data-theme-choose="`+x.ID+`"`) || !strings.Contains(r.corpo, "<span>"+x.Etichetta+"</span>") {
				t.Errorf("%s: manca la scelta %q", p, x.ID)
			}
		}
	}
}

// inventario: ADM-08
// Come «the login page» e «a page a customer is given by link»: login e reset collegano il manifest
// e il worker ma restano chiari, senza tavolozza, senza menu, con l'inchiostro scuro per la barra e
// senza nome del negozio; l'invito (pubblico nello standalone) non ha nemmeno manifest e worker, e
// così la pagina del primo titolare (solo Go, ACC-16), che viene prima di ogni account.
// Tutte hanno l'icona del negozio nella scheda del browser.
func TestGuscioIngressoEPubblico(t *testing.T) {
	a := nuovoBrowser(t, internaProva(&sondaFinta{pronto: true}, nessunRegistro))
	for p, installabile := range map[string]bool{"/login": true, "/reset-request": true, "/reset/non-esiste": true, "/invito/non-esiste": false, "/configura/non-esiste": false} {
		r := a.vai("GET", p, nil, nil)
		testa := testaDi(r.corpo)
		for _, s := range []string{`<html lang="it" data-theme="light">`, `<meta name="theme-color" content="#1c1409">`,
			`<link rel="icon" type="image/png" href="/branding/icon-192.png">`} {
			if !strings.Contains(testa, s) {
				t.Errorf("%s: la testa non contiene %q", p, s)
			}
		}
		if strings.Contains(testa, `rel="manifest" href="/manifest.webmanifest"`) != installabile || strings.Contains(testa, `"/sw.js"`) != installabile {
			t.Errorf("%s: manifest e worker dovrebbero esserci = %v", p, installabile)
		}
		for _, s := range []string{"app-tema", "data-theme-picker", "mobile-navigation", "apple-mobile-web-app-title", "data-close-outside"} {
			if strings.Contains(r.corpo, s) {
				t.Errorf("%s contiene %q", p, s)
			}
		}
	}
}

// Il manifest dello standalone (Pwa.manifest/0): nome lungo del negozio (oggi «Negozio», il
// ripiego di OnlineSales.shop_name/0, finché il profilo del negozio non c'è), nome corto
// «GestRip», solo le chiavi dello standalone; servito senza sessione. Il worker ha la lista del
// precache scritta dentro, le due intestazioni di PwaController e nessuna sessione; le icone
// sono quelle di ripiego dello standalone, con la sua cache.
func TestManifestWorkerEIcone(t *testing.T) {
	a := nuovoBrowser(t, internaProva(&sondaFinta{pronto: true}, nessunRegistro))
	r := a.vai("GET", "/manifest.webmanifest", nil, nil)
	if r.codice != 200 || r.h.Get("Content-Type") != "application/manifest+json; charset=utf-8" || r.h.Get("Set-Cookie") != "" {
		t.Fatalf("manifest: %d %q %q", r.codice, r.h.Get("Content-Type"), r.h.Get("Set-Cookie"))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(r.corpo), &m); err != nil {
		t.Fatal(err)
	}
	chiavi := []string{}
	for k := range m {
		chiavi = append(chiavi, k)
	}
	slices.Sort(chiavi)
	if !slices.Equal(chiavi, []string{"background_color", "description", "display", "icons", "name", "orientation", "scope", "short_name", "start_url", "theme_color"}) {
		t.Errorf("chiavi: %q", chiavi)
	}
	for k, v := range map[string]string{"name": "Negozio", "short_name": "GestRip", "description": "Gestione riparazioni oreficeria",
		"start_url": "/", "scope": "/", "display": "standalone", "orientation": "portrait", "background_color": "#b8960c", "theme_color": "#b8960c"} {
		if m[k] != v {
			t.Errorf("%s = %v", k, m[k])
		}
	}
	// Jason scrive le chiavi di una mappa piccola in ordine: lo stesso testo di encoding/json.
	atteso := `{"background_color":"#b8960c","description":"Gestione riparazioni oreficeria","display":"standalone","icons":[{"purpose":"any","sizes":"192x192","src":"/branding/icon-192.png","type":"image/png"},{"purpose":"any maskable","sizes":"512x512","src":"/branding/icon-512.png","type":"image/png"}],"name":"Negozio","orientation":"portrait","scope":"/","short_name":"GestRip","start_url":"/","theme_color":"#b8960c"}`
	if r.corpo != atteso {
		t.Errorf("manifest:\n%s\natteso:\n%s", r.corpo, atteso)
	}
	if !slices.Equal(precache(), []string{"/manifest.webmanifest", "/branding/icon-192.png", "/branding/icon-512.png"}) {
		t.Errorf("precache: %q", precache())
	}

	r = a.vai("GET", "/sw.js", nil, nil)
	if r.codice != 200 || r.h.Get("Content-Type") != "text/javascript; charset=utf-8" || r.h.Get("Service-Worker-Allowed") != "/" ||
		r.h.Get("Cache-Control") != "no-cache" || r.h.Get("Set-Cookie") != "" {
		t.Errorf("worker: %d %q %q %q", r.codice, r.h.Get("Content-Type"), r.h.Get("Service-Worker-Allowed"), r.h.Get("Cache-Control"))
	}
	for _, s := range []string{`const PRECACHE = ["/manifest.webmanifest","/branding/icon-192.png","/branding/icon-512.png"];`,
		`if (url.search) return false;`, `url.pathname.startsWith("/assets/")`, `key !== CACHE`} {
		if !strings.Contains(r.corpo, s) {
			t.Errorf("il worker non contiene %q", s)
		}
	}
	if strings.Contains(r.corpo, "__PRECACHE__") {
		t.Error("il segnaposto del precache è rimasto")
	}

	for p, lato := range map[string]int{"/branding/icon-192.png": 192, "/branding/icon-512.png": 512} {
		r := a.vai("GET", p, nil, nil)
		if r.codice != 200 || r.h.Get("Content-Type") != "image/png; charset=utf-8" || r.h.Get("Cache-Control") != "public, max-age=300, must-revalidate" ||
			r.h.Get("Set-Cookie") != "" || !strings.HasPrefix(r.corpo, "\x89PNG") {
			t.Errorf("%s: %d %q %q", p, r.codice, r.h.Get("Content-Type"), r.h.Get("Cache-Control"))
			continue
		}
		// Larghezza e altezza dall'intestazione IHDR.
		w := int(r.corpo[16])<<24 | int(r.corpo[17])<<16 | int(r.corpo[18])<<8 | int(r.corpo[19])
		if w != lato {
			t.Errorf("%s largo %d", p, w)
		}
	}
	// Sulla faccia pubblica non ci sono: il manifest compare solo sulla shell interna e sul login.
	p := nuovoBrowser(t, pubblicaProva(&sondaFinta{pronto: true}, nessunRegistro))
	// L'indirizzo vero di un asset, preso dalla testa di una pagina interna.
	css := regexp.MustCompile(`/assets/app-[0-9a-f]{12}\.css`).FindString(a.vai("GET", "/login", nil, nil).corpo)
	if css == "" {
		t.Fatal("il login non collega il foglio")
	}
	for _, x := range []string{"/manifest.webmanifest", "/sw.js", "/branding/icon-192.png", "/branding/icon-512.png", css} {
		if r := p.vai("GET", x, nil, nil); r.codice != 404 {
			t.Errorf("%s sulla faccia pubblica = %d", x, r.codice)
		}
	}
}

// inventario: ADM-22
// Il guscio lo chiede la rotta, come :staff_shell nello standalone: solo le rotte registrate con
// rottaConGuscio lo hanno. Sono le rotte che oggi disegnano una pagina interna, compresi i POST
// che ridisegnano la pagina utenti; /settings è solo un redirect.
func TestRotteConGuscio(t *testing.T) {
	_, f, err := costruisciInterna(dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, &utentiFinti{attivo: true}), nil)
	if err != nil {
		t.Fatal(err)
	}
	var con []string
	for _, r := range f.rotte {
		if r.guscio {
			con = append(con, r.pattern)
		}
	}
	slices.Sort(con)
	atteso := []string{"GET /audit", "GET /nuova", "GET /repairs/{id}", "GET /settings/utenti", "GET /{$}", "POST /nuova", "POST /repairs/{id}/modifica", "POST /repairs/{id}/riapri", "POST /settings/utenti/invita", "POST /settings/utenti/{id}/nuovo-link"}
	if !slices.Equal(con, atteso) {
		t.Errorf("rotte con il guscio: %q, attese %q", con, atteso)
	}
}

// inventario: ADM-22
// Una pagina del personale senza la marcatura (come /repairs/:id/anteprima-cliente dello
// standalone, la vista del cliente) non riceve il guscio: né menu, né tavolozze, né Lista, né
// manifest, né worker. Il ripiego dà meno, mai di più. Lo stesso per una POST non marcata.
func TestRottaSenzaGuscio(t *testing.T) {
	b, _ := dentroCome(t, accounts.Titolare, func(f *facciaWeb, mux *http.ServeMux) {
		f.rotta(mux, "GET /anteprima", personale, f.home)
		f.rotta(mux, "GET /solo-titolare", soloTitolare, f.home)
	})
	for _, p := range []string{"/anteprima", "/solo-titolare"} {
		r := b.vai("GET", p, nil, nil)
		if r.codice != 200 {
			t.Fatalf("%s: %d", p, r.codice)
		}
		for _, s := range []string{"mobile-navigation", "app-tema", "data-theme-picker", "fab-home", "manifest", "/sw.js", "apple-mobile-web-app-title", "#b8960c"} {
			if strings.Contains(r.corpo, s) {
				t.Errorf("%s contiene %q", p, s)
			}
		}
		if !strings.Contains(testaDi(r.corpo), `<meta name="theme-color" content="#1c1409">`) || !strings.Contains(r.corpo, `<html lang="it" data-theme="light">`) {
			t.Errorf("%s non è chiara con l'inchiostro scuro", p)
		}
	}
}

// inventario: ADM-22
// Un asset si serve solo dal suo indirizzo esatto: con barre in più, una barra finale o un
// carattere codificato è un 404, così il worker non tiene due copie degli stessi byte.
func TestAssetSoloDalPercorsoEsatto(t *testing.T) {
	b, _ := dentroCome(t, accounts.Titolare, nil)
	css := regexp.MustCompile(`/assets/(app-[0-9a-f]{12}\.css)`).FindStringSubmatch(b.vai("GET", "/", nil, nil).corpo)
	if css == nil {
		t.Fatal("la pagina non collega il foglio")
	}
	if r := b.vai("GET", css[0], nil, nil); r.codice != 200 {
		t.Fatalf("%s: %d", css[0], r.codice)
	}
	for _, p := range []string{css[0] + "/", "/assets//" + css[1], "//assets/" + css[1], "/assets/" + css[1] + "//",
		"/%61ssets/" + css[1], "/assets/%61" + css[1][1:], "/./assets/" + css[1]} {
		if r := b.vai("GET", p, nil, nil); r.codice != 404 {
			t.Errorf("%s = %d, atteso 404", p, r.codice)
		}
	}
}

// inventario: ADM-22
// Manifest, worker e icone non toccano la sessione nemmeno con un accesso aperto.
func TestPWASenzaCookieConSessione(t *testing.T) {
	b, _ := dentroCome(t, accounts.Titolare, nil)
	for _, p := range []string{"/manifest.webmanifest", "/sw.js", "/branding/icon-192.png", "/branding/icon-512.png"} {
		if r := b.vai("GET", p, nil, nil); r.codice != 200 || r.h.Get("Set-Cookie") != "" {
			t.Errorf("%s: %d, Set-Cookie %q", p, r.codice, r.h.Get("Set-Cookie"))
		}
	}
}

// inventario: ADM-22, ADM-01
// «Solo titolare» del menu e delle schede coincide con il permesso: con la rotta registrata nel
// Go, con il suo permesso; senza, con la riga dello standalone nella tabella dei permessi, se c'è.
// Tutte le schede delle impostazioni sono del titolare.
func TestMenuESchedeComeIPermessi(t *testing.T) {
	_, f, err := costruisciInterna(dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, &utentiFinti{attivo: true}), nil)
	if err != nil {
		t.Fatal(err)
	}
	registrate := map[string]permesso{}
	for _, r := range f.rotte {
		registrate[r.pattern] = r.chi
	}
	standalone := map[string]permesso{}
	for _, r := range tabellaPermessi {
		standalone[r.standalone] = r.chi
	}
	permessoDi := func(rotta, href string) (permesso, bool) {
		if chi, ok := registrate[rotta]; ok {
			return chi, true
		}
		chi, ok := standalone["live "+href]
		return chi, ok
	}
	for _, v := range vociMenu {
		if chi, ok := permessoDi(v.Rotta, v.Href); ok && (chi == soloTitolare) != v.SoloTitolare {
			t.Errorf("voce %q: solo titolare %v, permesso %v", v.Etichetta, v.SoloTitolare, chi)
		}
	}
	for _, s := range schedeImpostazioni {
		chi, ok := permessoDi(s.Rotta, s.Href)
		if !ok || chi != soloTitolare {
			t.Errorf("scheda %q: permesso %v (trovato %v), atteso solo titolare", s.Chiave, chi, ok)
		}
	}
	if chi, ok := registrate["GET /settings"]; !ok || chi != soloTitolare {
		t.Errorf("/settings: %v %v", chi, ok)
	}
}

// memorizzabile è storable() di sw.js: stessa origine, niente query, e un indirizzo sotto /assets/
// o nella lista del precache. TestManifestWorkerEIcone controlla che il worker abbia queste righe.
func memorizzabile(percorso string) bool {
	return !strings.Contains(percorso, "?") && (strings.HasPrefix(percorso, "/assets/") || slices.Contains(precache(), percorso))
}

// inventario: ADM-22
// Nessuna rotta GET della faccia interna, a parte manifest, icone e asset, ricade nella regola del
// worker: una pagina o un dato di un cliente non finiscono mai nella sua cache.
func TestNessunaRottaNellaCacheDelWorker(t *testing.T) {
	_, f, err := costruisciInterna(dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, &utentiFinti{attivo: true}), nil)
	if err != nil {
		t.Fatal(err)
	}
	ammesse := []string{"GET /manifest.webmanifest", "GET /branding/icon-192.png", "GET /branding/icon-512.png", "GET /assets/{nome}"}
	for _, r := range f.rotte {
		metodo, percorso := percorsoDi(r.pattern)
		if metodo == "GET" && memorizzabile(percorso) != slices.Contains(ammesse, r.pattern) {
			t.Errorf("%s: memorizzabile dal worker = %v", r.pattern, memorizzabile(percorso))
		}
	}
	if !strings.Contains(corpoWorker, `return url.pathname.startsWith("/assets/") || PRECACHE.indexOf(url.pathname) !== -1;`) {
		t.Error("la regola del worker non è quella che il test ricopia")
	}
}

// inventario: ADM-22, ADM-08
// Il markup del guscio che i test di comportamento non vedono: la barra scura della bottega solo
// sulla pagina iniziale, il suo contenitore, e i quattro campioni di ogni tavolozza dipinti
// nell'ordine di Campioni().
func TestMarkupDelGuscio(t *testing.T) {
	b, _ := dentroCome(t, accounts.Titolare, nil)
	casa := b.vai("GET", "/", nil, nil).corpo
	if !strings.Contains(casa, `<header class="navbar workshop-header">`) || !strings.Contains(casa, `<main class="gs-page workshop-content">`) {
		t.Error("la pagina iniziale non ha la barra o il contenitore della bottega")
	}
	for _, p := range []string{"/audit", "/settings/utenti"} {
		r := b.vai("GET", p, nil, nil)
		if !strings.Contains(r.corpo, `<header class="navbar">`) || strings.Contains(r.corpo, "workshop-header") || strings.Contains(r.corpo, "workshop-content") {
			t.Errorf("%s: barra della bottega dove non va", p)
		}
	}
	for _, x := range temi {
		var campioni strings.Builder
		for _, c := range x.Campioni() {
			campioni.WriteString(`<span style="background: ` + c + `"></span>`)
		}
		atteso := `<button type="button" data-theme-choose="` + x.ID + "\">\n<span class=\"theme-swatches\">" + campioni.String() + "</span>\n<span>" + x.Etichetta + "</span>"
		if !strings.Contains(casa, atteso) {
			t.Errorf("la scelta %q non ha i campioni nell'ordine di Campioni():\n%s", x.ID, atteso)
		}
	}
}

// inventario: ADM-08
// Lo script del tema nella testa ha le righe che decidono il comportamento, nella loro forma
// esatta. Il comportamento vero (tavolozza salvata applicata prima del disegno, scura → data-theme
// dark, scelta salvata e segnata con aria-pressed, Oro che toglie la chiave) è stato provato il
// 30/10/2026 in Chrome senza testa su una copia statica della pagina generata (slice 015); qui
// non c'è un motore JavaScript.
func TestScriptDelTema(t *testing.T) {
	b, _ := dentroCome(t, accounts.Titolare, nil)
	testa := testaDi(b.vai("GET", "/", nil, nil).corpo)
	for _, s := range []string{
		`const DARK = ["crepuscolo","notte","ambra"];`,
		`const KEY = "app-tema";`,
		`if (id) { root.setAttribute("data-tema", id); } else { root.removeAttribute("data-tema"); }`,
		`root.setAttribute("data-theme", DARK.indexOf(id) !== -1 ? "dark" : "light");`,
		`const mine = button.getAttribute("data-theme-choose") === id;`,
		`if (mine) { button.setAttribute("data-theme-active", ""); }`,
		`button.setAttribute("aria-pressed", mine ? "true" : "false");`,
		`try { stored = localStorage.getItem(KEY) || ""; } catch (e) {}`,
		"apply(stored);",
		`if (target.closest("[data-theme-picker-open]")) { open(true); return; }`,
		`if (target.closest("[data-theme-picker-close]")) { open(false); return; }`,
		`try { if (id) { localStorage.setItem(KEY, id); } else { localStorage.removeItem(KEY); } } catch (e) {}`,
		`if (event.key === "Escape") { open(false); }`,
		`if (event.key === KEY) { apply(event.newValue || ""); }`,
	} {
		if !strings.Contains(testa, s) {
			t.Errorf("lo script del tema non contiene %q", s)
		}
	}
	// La tavolozza salvata si applica nella testa, prima del selettore: apply(stored) prima di </head>.
	if i, j := strings.Index(testa, "apply(stored);"), strings.Index(testa, "const modal"); i < 0 || j < 0 || i > j {
		t.Error("apply(stored) non è dove lo standalone lo mette")
	}
}

// inventario: ADM-22
// app.js ha le righe che decidono il comportamento del menu e delle pillole, nella forma esatta:
// un tocco fuori chiude (mai apre) il menu, Esc lo chiude, le pillole si riducono oltre 80px in
// giù e tornano risalendo o sotto 40px. Provato il 30/10/2026 in Chrome senza testa (slice 015:
// tocco fuori chiude, tocco dentro no, Esc chiude); lo scorrimento non è stato provato dal vivo.
func TestScriptDelMenu(t *testing.T) {
	b, _ := dentroCome(t, accounts.Titolare, nil)
	js := assetDellaPagina(t, b, b.vai("GET", "/", nil, nil).corpo, "js", "text/javascript; charset=utf-8")
	for _, s := range []string{
		"document.addEventListener(\"click\", event => {\n  document.querySelectorAll(\"details[data-close-outside][open]\").forEach(menu => {\n    if (!menu.contains(event.target)) menu.open = false\n  })\n})",
		"  if (event.key !== \"Escape\") return\n  document.querySelectorAll(\"details[data-close-outside][open]\").forEach(menu => {\n    if (menu.contains(document.activeElement)) menu.querySelector(\":scope > summary\").focus()\n    menu.open = false\n  })",
		`if (y > 80 && y > lastY) root.classList.add("gs-fabs-shrunk")`,
		`else if (y < lastY || y < 40) root.classList.remove("gs-fabs-shrunk")`,
		"lastY = y",
	} {
		if !strings.Contains(js, s) {
			t.Errorf("app.js non contiene %q", s)
		}
	}
}

// Il JSON del manifest esce come Jason: «&», «<» e «>» non si escapano (il nome del negozio, quando
// arriva, può contenerli), chiavi in ordine, niente a capo in fondo.
func TestJSONComeJason(t *testing.T) {
	got := string(jsonComeJason(map[string]any{"short_name": "GestRip", "name": "Rossi & figli <oro> 'x'"}))
	if want := `{"name":"Rossi & figli <oro> 'x'","short_name":"GestRip"}`; got != want {
		t.Errorf("jsonComeJason = %s, atteso %s", got, want)
	}
}
