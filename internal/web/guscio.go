package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/L0cutus/gestrip/internal/accounts"
)

// Il guscio delle pagine (ADM-22, ADM-01, ADM-08, e di ADM-07 quanto serve al guscio), come
// Layouts.app/1, Layouts.settings/1, root.html.heex e GestripStandalone.Pwa dello standalone:
//   - ogni pagina del personale ha la stessa barra con il menu ☰ (le voci delle aree che il Go ha,
//     «Cambia tema», «Esci»), «🏠 Lista» e il selettore delle tavolozze;
//   - login e reset collegano manifest e worker ma restano chiari e senza menu;
//   - le altre pagine aperte (l'invito) non hanno né manifest né tavolozza.
//
// La superficie la decide la rotta, come i pipeline :staff_shell e :login_page: il guscio interno
// solo per le rotte registrate con rottaConGuscio, l'ingresso per quelle in rotteIngresso, il
// resto è pubblico. Una pagina dimenticata, anche del personale (come l'anteprima del cliente
// dello standalone), riceve meno, mai di più.

const (
	chiaveTema = "app-tema"
	// nomeNegozio è il ripiego di OnlineSales.shop_name/0: il profilo del negozio (ADM-02) non c'è
	// ancora, quindi il manifest e il titolo sotto l'icona dicono sempre «Negozio».
	nomeNegozio = "Negozio"
	oro         = "#b8960c"
	inchiostro  = "#1c1409" // la barra del browser sul login e sulle pagine pubbliche
	// Gli indirizzi del manifest e delle icone, scritti una volta (Pwa.precache/0).
	indirizzoManifest = "/manifest.webmanifest"
	indirizzoIcona192 = "/branding/icon-192.png"
	indirizzoIcona512 = "/branding/icon-512.png"
)

// tema è una tavolozza di Pwa.themes/0: colore della pagina, delle superfici, accento,
// inchiostro, linee, e se è scura.
type tema struct {
	ID, Etichetta                                  string
	Pagina, Superficie, Accento, Inchiostro, Linea string
	Scuro                                          bool
}

// Campioni sono i quattro colori che il bottone della tavolozza dipinge, nell'ordine dello
// standalone (pagina, superficie, accento, inchiostro).
func (t tema) Campioni() []string { return []string{t.Pagina, t.Superficie, t.Accento, t.Inchiostro} }

// temi sono le otto tavolozze dello standalone, nel suo ordine; la prima, senza id, è quella di
// partenza e sta sul :root nudo.
var temi = []tema{
	{"", "Oro gestrip (default)", "#faf9f5", "#ffffff", oro, inchiostro, "#e2d9c0", false},
	{"sabbia", "Sabbia", "#faf6f0", "#fffaf2", "#92400e", "#1c1917", "#e7d6bf", false},
	{"chiaro", "Chiaro", "#f8fafc", "#ffffff", "#2563eb", "#0f172a", "#e2e8f0", false},
	{"marina", "Marina", "#f0f6fc", "#ffffff", "#1e40af", "#1e293b", "#cbd5e1", false},
	{"smeraldo", "Smeraldo", "#f0fdf4", "#ffffff", "#065f46", "#064e3b", "#bbf7d0", false},
	{"crepuscolo", "Crepuscolo", "#3a4253", "#4a5468", "#fbbf24", "#f1f5f9", "#5a6478", true},
	{"notte", "Notte", "#0f172a", "#1e293b", "#d4a017", "#e5e7eb", "#334155", true},
	{"ambra", "Ambra", "#1c1c1c", "#2a2a2a", "#ff9900", "#f5f5f5", "#3f3f3f", true},
}

// temiScuri sono gli id delle tre tavolozze scure: lo script mette data-theme="dark" accanto.
func temiScuri() []string {
	var s []string
	for _, t := range temi {
		if t.Scuro {
			s = append(s, t.ID)
		}
	}
	return s
}

// cssTemi è il foglio delle tavolozze, scritto dalla tabella come Pwa.theme_css/0 (stesso testo):
// un blocco per tavolozza, il corpo dipinto con il colore della pagina, la scelta attiva segnata e
// il comando «Cambia tema» rivelato (è disegnato hidden, così dove non c'è la tavolozza non c'è
// nemmeno il comando).
func cssTemi() string {
	var b strings.Builder
	for _, t := range temi {
		sel := `:root[data-tema="` + t.ID + `"]`
		if t.ID == "" {
			sel = ":root"
		}
		schema := "light"
		if t.Scuro {
			schema = "dark"
		}
		b.WriteString(sel + " {\n  --color-base-100: " + t.Superficie + ";\n  --color-base-200: " + t.Pagina +
			";\n  --color-base-300: " + t.Linea + ";\n  --color-base-content: " + t.Inchiostro + ";\n  --color-primary: " + t.Accento +
			";\n  color-scheme: " + schema + ";\n}\n")
	}
	b.WriteString("body { background-color: var(--color-base-200); }\n")
	b.WriteString("[data-theme-choose][data-theme-active] { border-color: var(--color-primary); border-width: 3px; }\n")
	b.WriteString("@layer base { [data-theme-picker-open][hidden] { display: inline-flex !important; } }\n")
	return b.String()
}

// voceMenu è una voce del menu ☰ di Layouts.navigation/1. Rotta è il pattern Go che la fa
// comparire: finché l'area non c'è nel Go, la voce non si mostra (niente link verso un 404).
type voceMenu struct {
	Href, Etichetta, Rotta  string
	SoloTitolare, Rimandata bool
}

// vociMenu sono tutte le voci dello standalone, nel suo ordine e con le sue etichette. «Esci» e
// «Cambia tema» non sono qui: ci sono sempre. «Statistiche» è dopo la v1 (inventario, ADM-22).
var vociMenu = []voceMenu{
	{"/", "Riparazioni", "GET /{$}", false, false},
	{"/sales", "Vendite", "GET /sales", false, false},
	{"/settings", "Impostazioni", "GET /settings", true, false},
	{"/audit", "Registro attività", "GET /audit", true, false},
	{"/handovers", "📤 Consegne", "GET /handovers", false, false},
	{"/handovers/history", "📋 Storico consegne", "GET /handovers/history", false, false},
	{"/handovers/reminders", "⏰ Solleciti pendenti", "GET /handovers/reminders", false, false},
	{"/ready-lots", "📦 Avvisi oreficeria", "GET /ready-lots", false, false},
	{"/emergenza", "🚨 Registro Emergenza", "GET /emergenza", false, false},
	{"/settings/anagrafiche", "Anagrafiche", "GET /settings/anagrafiche", false, false},
	{"/labels", "🏷️ Etichette QR", "GET /labels", false, false},
	{"/statistiche", "📊 Statistiche", "GET /statistiche", false, true},
	{"/cestino", "🗑 Cestino", "GET /cestino", false, false},
}

// schedaImpostazioni è una scheda di Layouts.settings/1; Rotta la fa comparire, come per il menu.
type schedaImpostazioni struct {
	Chiave, Etichetta, Href, Rotta string
}

var schedeImpostazioni = []schedaImpostazioni{
	{"shop", "Negozio", "/settings/shop", "GET /settings/shop"},
	{"templates", "Messaggi WhatsApp", "/settings/templates", "GET /settings/templates"},
	{"providers", "Provider", "/settings/providers", "GET /settings/providers"},
	{"assistente", "Assistente", "/settings/assistente", "GET /settings/assistente"},
	{"promemoria", "Promemoria", "/settings/promemoria", "GET /settings/promemoria"},
	{"utenti", "Utenti", "/settings/utenti", "GET /settings/utenti"},
	{"cestino", "Cestino", "/settings/cestino", "GET /settings/cestino"},
	{"retention-foto", "Foto", "/settings/retention-foto", "GET /settings/retention-foto"},
}

// schedaDelModello dice quale scheda è aperta per ogni pagina delle impostazioni.
var schedaDelModello = map[string]string{"utenti": "utenti"}

// varianteDelModello: la pagina iniziale ha il menu della bottega (variant="workshop" di
// WorkshopLive), le altre quello dell'app.
var varianteDelModello = map[string]string{"home": "workshop"}

// superficie è il segno che la rotta mette sulla pagina (page_surface dello standalone).
type superficie int

const (
	superficiePubblica superficie = iota // nessun manifest, nessuna tavolozza
	superficieIngresso                   // login e reset: manifest sì, tavolozza no
	superficieInterna                    // le pagine del personale: tutto
)

// rotteIngresso sono le rotte del pipeline :login_page dello standalone.
var rotteIngresso = []string{"GET /login", "POST /login", "GET /reset-request", "POST /reset-request", "GET /reset/{token}", "POST /reset/{token}"}

// superficieDi: il guscio interno solo se la rotta lo chiede (rottaConGuscio); login e reset sono
// l'ingresso; tutto il resto, anche una pagina del personale non marcata, è pubblico.
func superficieDi(pattern string, guscio bool) superficie {
	switch {
	case guscio:
		return superficieInterna
	case slices.Contains(rotteIngresso, pattern):
		return superficieIngresso
	}
	return superficiePubblica
}

type chiaveSuperficie struct{}

func conSuperficie(s superficie, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), chiaveSuperficie{}, s)))
	})
}

func superficieRichiesta(r *http.Request) superficie {
	s, _ := r.Context().Value(chiaveSuperficie{}).(superficie)
	return s
}

// schedaVista è una scheda come la pagina la disegna.
type schedaVista struct {
	schedaImpostazioni
	Aperta bool
}

// guscio è quello che base.html riceve per la testa, la barra e la coda.
type guscio struct {
	Interna, Installabile bool
	ColoreBarra           string
	NomeNegozio           string
	Variante              string     // "app" o "workshop"
	Menu                  []voceMenu // le voci per questo ruolo
	Schede                []schedaVista
	SchedaAperta          string
	Temi                  []tema
	CSSTemi               template.CSS
	Scuri                 []string
	Chiave                string
}

// preparaGuscio sceglie, dopo la registrazione delle rotte, le voci del menu e le schede che hanno
// la loro rotta. «🏠 Lista» porta a /, che c'è sempre: si mostra su ogni pagina interna.
func (f *facciaWeb) preparaGuscio() {
	registrate := map[string]bool{}
	for _, r := range f.rotte {
		registrate[r.pattern] = true
	}
	f.menu = nil
	for _, v := range vociMenu {
		if registrate[v.Rotta] && !v.Rimandata {
			f.menu = append(f.menu, v)
		}
	}
	f.schede = nil
	for _, s := range schedeImpostazioni {
		if registrate[s.Rotta] {
			f.schede = append(f.schede, s)
		}
	}
}

// guscioPer è il guscio della pagina nome per questa richiesta.
func (f *facciaWeb) guscioPer(r *http.Request, nome string) guscio {
	s := superficieRichiesta(r)
	g := guscio{ColoreBarra: inchiostro, Installabile: s != superficiePubblica}
	if s != superficieInterna {
		return g
	}
	titolare := false
	if u := statoDi(r).utente; u != nil {
		titolare = u.Ruolo == accounts.Titolare
	}
	g.Interna, g.ColoreBarra, g.NomeNegozio = true, oro, nomeNegozio
	g.Variante = "app"
	if v, ok := varianteDelModello[nome]; ok {
		g.Variante = v
	}
	for _, v := range f.menu {
		if !v.SoloTitolare || titolare {
			g.Menu = append(g.Menu, v)
		}
	}
	if aperta, ok := schedaDelModello[nome]; ok {
		g.SchedaAperta = aperta
		for _, x := range f.schede {
			g.Schede = append(g.Schede, schedaVista{x, x.Chiave == aperta})
		}
	}
	g.Temi, g.CSSTemi, g.Scuri, g.Chiave = temi, template.CSS(cssTemi()), temiScuri(), chiaveTema
	return g
}

// impostazioni è /settings: nello standalone è la scheda Negozio (ADM-02), che il Go non ha
// ancora; fino ad allora va alla prima scheda che c'è.
func (f *facciaWeb) impostazioni(w http.ResponseWriter, r *http.Request) {
	dove := "/"
	if len(f.schede) > 0 {
		dove = f.schede[0].Href
	}
	f.vaiA(w, r, dove)
}

// ---- PWA: manifest, worker, icone (ADM-07, quanto serve al guscio) ----

// manifesto è Pwa.manifest/0.
func manifesto() map[string]any {
	return map[string]any{
		"name":             nomeNegozio,
		"short_name":       "GestRip",
		"description":      "Gestione riparazioni oreficeria",
		"start_url":        "/",
		"scope":            "/",
		"display":          "standalone",
		"orientation":      "portrait",
		"background_color": oro,
		"theme_color":      oro,
		"icons": []map[string]string{
			{"src": indirizzoIcona192, "sizes": "192x192", "type": "image/png", "purpose": "any"},
			{"src": indirizzoIcona512, "sizes": "512x512", "type": "image/png", "purpose": "any maskable"},
		},
	}
}

// precache sono gli indirizzi che il worker mette in cache all'installazione (Pwa.precache/0).
func precache() []string { return []string{indirizzoManifest, indirizzoIcona192, indirizzoIcona512} }

// jsonComeJason scrive come Jason: chiavi di una mappa in ordine, nessun escape HTML, senza a capo.
func jsonComeJason(v any) []byte {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		panic(err) // solo mappe e stringhe: non succede
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}

// serviManifest è PwaController.manifest/2: niente sessione, la cache di Plug.
func serviManifest(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
	_, _ = w.Write(jsonComeJason(manifesto())) // un client andato via: niente da fare
}

// corpoWorker è priv/sw.js dello standalone con la lista del precache scritta dentro.
var corpoWorker = strings.Replace(string(leggiStatico("sw.js")), "__PRECACHE__", string(jsonComeJason(precache())), 1)

// serviWorker è PwaController.service_worker/2: Service-Worker-Allowed e no-cache, così un worker
// nuovo si scarica davvero.
func serviWorker(w http.ResponseWriter, _ *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/javascript; charset=utf-8")
	h.Set("Service-Worker-Allowed", "/")
	h.Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(corpoWorker))
}

// serviIcona è BrandingController senza logo caricato: l'icona di ripiego, con la cache e il tipo
// dello standalone (put_resp_content_type aggiunge il charset anche a un'immagine).
func serviIcona(nome string) http.HandlerFunc {
	corpo := leggiStatico(nome)
	return func(w http.ResponseWriter, _ *http.Request) {
		h := w.Header()
		h.Set("Content-Type", "image/png; charset=utf-8")
		h.Set("Cache-Control", "public, max-age=300, must-revalidate")
		_, _ = w.Write(corpo)
	}
}

// ---- Asset: foglio e script delle pagine, nel binario ----

//go:embed statici
var fileStatici embed.FS

func leggiStatico(nome string) []byte {
	b, err := fileStatici.ReadFile("statici/" + nome)
	if err != nil {
		panic(err) // un file incorporato che manca è un errore di compilazione
	}
	return b
}

type fileAsset struct {
	tipo  string
	corpo []byte
}

// Gli asset delle pagine: ognuno servito sotto /assets/ con un pezzo dell'hash nel nome, così si
// tiene in cache un anno e cambia indirizzo quando cambia (come phx.digest). Niente query: il
// worker non mette in cache un indirizzo con la query.
var assetPerNome, percorsiAsset = caricaAsset("app.css", "app.js", "settings-tabs.css")

func caricaAsset(nomi ...string) (map[string]fileAsset, map[string]string) {
	perNome, percorsi := map[string]fileAsset{}, map[string]string{}
	tipi := map[string]string{".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8"}
	for _, n := range nomi {
		corpo := leggiStatico(n)
		h := sha256.Sum256(corpo)
		ext := path.Ext(n)
		conHash := strings.TrimSuffix(n, ext) + "-" + hex.EncodeToString(h[:6]) + ext
		perNome[conHash] = fileAsset{tipi[ext], corpo}
		percorsi[n] = "/assets/" + conHash
	}
	return perNome, percorsi
}

// percorsoAsset è l'indirizzo di un asset per le pagine ({{asset "app.css"}}).
func percorsoAsset(nome string) string {
	p, ok := percorsiAsset[nome]
	if !ok {
		panic("web: asset sconosciuto " + nome)
	}
	return p
}

// serviAsset è GET /assets/{nome}: un nome che non c'è è il 404 dello standalone. L'asset si
// serve solo dal suo indirizzo esatto, come arriva (senza barre in più, senza caratteri
// codificati): faccia ripulisce il percorso, e il worker terrebbe in cache due copie degli stessi
// byte sotto due chiavi.
func serviAsset(w http.ResponseWriter, r *http.Request) {
	nome := r.PathValue("nome")
	a, ok := assetPerNome[nome]
	if !ok || percorsoArrivato(r) != "/assets/"+nome {
		NonTrovata(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", a.tipo)
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(a.corpo)
}

// percorsoArrivato è il percorso della richiesta come l'ha scritto il client, prima della pulizia
// di faccia (la query non conta).
func percorsoArrivato(r *http.Request) string {
	p, _, _ := strings.Cut(r.RequestURI, "?")
	if !strings.HasPrefix(p, "/") {
		// forma assoluta (http://host/percorso): conta il percorso, così com'è scritto
		if u, err := url.ParseRequestURI(p); err == nil {
			return u.EscapedPath()
		}
		return ""
	}
	return p
}

// registraGuscio registra /settings, manifest, worker, icone e asset sulla faccia interna.
func (f *facciaWeb) registraGuscio(mux *http.ServeMux) {
	f.rotta(mux, "GET /settings", soloTitolare, f.impostazioni)
	f.speciale(mux, "GET "+indirizzoManifest, aperta, http.HandlerFunc(serviManifest))
	f.speciale(mux, "GET /sw.js", aperta, http.HandlerFunc(serviWorker))
	f.speciale(mux, "GET "+indirizzoIcona192, aperta, serviIcona("icon-192.png"))
	f.speciale(mux, "GET "+indirizzoIcona512, aperta, serviIcona("icon-512.png"))
	f.speciale(mux, "GET /assets/{nome}", aperta, http.HandlerFunc(serviAsset))
}
