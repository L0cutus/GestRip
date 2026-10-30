package web

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
)

// I test di questo file chiudono ACC-17 (CSRF e cookie), ACC-18 (intestazioni di sicurezza e
// HTTPS) e ACC-19 (nessun registro degli indirizzi con un token) sulle rotte che il Go ha oggi,
// prese dall'elenco registrato: una rotta nuova entra da sola nei controlli.

// hostVero è l'host di una faccia dietro Tailscale: con 127.0.0.1 (il server di prova) il Go non
// manda HSTS, come lo standalone (force_ssl esclude localhost e 127.0.0.1).
const hostVero = "negozio.esempio.ts.net:8443"

// conHost passa alla faccia le richieste con un Host scelto: con il client si perderebbero i
// cookie, che il cookiejar lega all'Host della richiesta.
func conHost(host string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = host
		h.ServeHTTP(w, r)
	})
}

// postDellaFaccia sono i pattern POST registrati dalla faccia interna.
func postDellaFaccia(f *facciaWeb) []string {
	var post []string
	for _, r := range f.rotte {
		if strings.HasPrefix(r.pattern, "POST ") {
			post = append(post, r.pattern)
		}
	}
	return post
}

// inventario: ACC-17
// Ogni POST della faccia interna (l'elenco registrato, quindi anche le rotte che arriveranno)
// vuole il token CSRF, da fuori e da dentro: senza token, con un token sbagliato, con il token di
// un altro browser, con il token solo nella query o con un x-csrf-token sbagliato è il 403
// «Forbidden» dello standalone (protect_from_forgery prima di ogni altro plug della rotta), senza
// le intestazioni delle pagine, e non cambia niente: nessuna riga d'audit, la sessione resta. Con
// il token giusto la stessa richiesta passa il controllo (il test non è vuoto).
func TestOgniPostVuoleIlTokenCSRF(t *testing.T) {
	u := &utentiFinti{attivo: true}
	reg := &registroFinto{}
	d := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, u)
	d.Audit = reg
	h, f, err := costruisciInterna(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	post := postDellaFaccia(f)
	for _, atteso := range []string{"POST /login", "POST /logout", "POST /reset-request", "POST /reset/{token}",
		"POST /invito/{token}", "POST /configura/{codice}", "POST /settings/utenti/invita", "POST /settings/utenti/{id}/disattiva"} {
		if !slices.Contains(post, atteso) {
			t.Fatalf("manca %s fra le rotte POST: %v", atteso, post)
		}
	}
	fuori := nuovoBrowser(t, h)
	fuori.vai("GET", "/login", nil, nil)
	dentro := nuovoBrowser(t, h)
	if r := dentro.entra(emailProva, passwordProva); r.codice != http.StatusFound {
		t.Fatalf("login: %d", r.codice)
	}
	dentro.vai("GET", "/", nil, nil)
	if fuori.token == "" || dentro.token == "" || fuori.token == dentro.token {
		t.Fatal("token delle pagine mancanti")
	}
	righe := len(reg.righe)
	for _, b := range []*browser{fuori, dentro} {
		altro := fuori.token
		if b == fuori {
			altro = dentro.token
		}
		chi := map[*browser]string{fuori: "fuori", dentro: "dentro"}[b]
		for _, p := range post {
			metodo, percorso := percorsoDi(p)
			if p == "POST /login" && b == dentro {
				// Con una sessione valida il Go porta a / prima del controllo (008b, seconda scheda):
				// lo standalone dà 403 a un POST senza token. Differenza segnalata nella slice 017.
				if r := b.vai(metodo, percorso, url.Values{}, nil); !redirectA(r, "/") {
					t.Errorf("%s %s senza token: %d %q", chi, p, r.codice, r.location)
				}
				continue
			}
			casi := map[string]struct {
				percorso string
				form     url.Values
				h        map[string]string
			}{
				"senza token":               {percorso, url.Values{"nome": {"x"}}, nil},
				"token sbagliato":           {percorso, url.Values{"_csrf_token": {"sbagliato"}}, nil},
				"token di un altro browser": {percorso, url.Values{"_csrf_token": {altro}}, nil},
				"token solo nella query":    {percorso + "?_csrf_token=" + url.QueryEscape(b.token), url.Values{}, nil},
				"x-csrf-token sbagliato":    {percorso, url.Values{}, map[string]string{"X-Csrf-Token": altro}},
			}
			for nome, c := range casi {
				r := b.vai(metodo, c.percorso, c.form, c.h)
				if r.codice != http.StatusForbidden || r.corpo != "Forbidden" || r.h.Get("Content-Type") != "text/html; charset=utf-8" ||
					r.h.Get("Content-Security-Policy") != "" || r.h.Get("Set-Cookie") != "" {
					t.Errorf("%s %s, %s: %d %q %v", chi, p, nome, r.codice, r.corpo, r.h)
				}
			}
		}
	}
	if len(reg.righe) != righe {
		t.Errorf("righe d'audit scritte da POST rifiutati: %v", reg.righe[righe:])
	}
	if r := dentro.vai("GET", "/", nil, nil); r.codice != http.StatusOK {
		t.Errorf("la sessione non è sopravvissuta ai POST rifiutati: %d", r.codice)
	}
	// Con il token giusto il controllo passa: la risposta è quella della rotta, mai il 403.
	for _, p := range post {
		metodo, percorso := percorsoDi(p)
		b := nuovoBrowser(t, h)
		b.vai("GET", "/login", nil, nil)
		if r := b.vai(metodo, percorso, url.Values{"_csrf_token": {b.token}}, nil); r.codice == http.StatusForbidden {
			t.Errorf("%s con il token giusto: 403", p)
		}
		b = nuovoBrowser(t, h) // l'uscita rinnova il segreto: un browser nuovo per l'intestazione
		b.vai("GET", "/login", nil, nil)
		if r := b.vai(metodo, percorso, url.Values{}, map[string]string{"X-Csrf-Token": b.token}); r.codice == http.StatusForbidden {
			t.Errorf("%s con x-csrf-token giusto: 403", p)
		}
	}
}

// attributiCookie controlla un Set-Cookie come quello dello standalone (misurato: path=/; secure;
// HttpOnly; SameSite=Lax; senza scadenza né dominio), con il prefisso __Host-.
func attributiCookie(t *testing.T, h http.Header, nome string) {
	t.Helper()
	for _, v := range h.Values("Set-Cookie") {
		k, err := http.ParseSetCookie(v)
		if err != nil || k.Name != nome {
			continue
		}
		if k.Path != "/" || !k.Secure || !k.HttpOnly || k.SameSite != http.SameSiteLaxMode || k.Domain != "" ||
			k.MaxAge != 0 || !k.Expires.IsZero() || !strings.HasPrefix(k.Name, "__Host-") {
			t.Errorf("cookie %s: %q", nome, v)
		}
		return
	}
	t.Errorf("nessun Set-Cookie %s in %q", nome, h.Values("Set-Cookie"))
}

// inventario: ACC-17
// I due cookie della faccia (sessione e segreto CSRF) hanno gli attributi del cookie dello
// standalone: SameSite=Lax, Secure (sempre: il Go non ha un modo sviluppo in chiaro), HttpOnly,
// Path=/, nessuna scadenza nel browser, nessun dominio. Al login sono nuovi tutti e due; il
// cookie di sessione si legge solo con la firma della sua faccia.
func TestCookieDellaFacciaComeLoStandalone(t *testing.T) {
	h := internaProva(&sondaFinta{pronto: true}, nessunRegistro)
	b := nuovoBrowser(t, h)
	r := b.vai("GET", "/login", nil, nil)
	attributiCookie(t, r.h, "__Host-gestrip_csrf_interna")
	r = b.vai("POST", "/login", url.Values{"_csrf_token": {b.token}, "user[email]": {emailProva}, "user[password]": {passwordProva}}, nil)
	if r.codice != http.StatusFound {
		t.Fatalf("login: %d", r.codice)
	}
	attributiCookie(t, r.h, "__Host-gestrip_sessione_interna")
	attributiCookie(t, r.h, "__Host-gestrip_csrf_interna")
	// Il contenuto è leggibile (firmato, non cifrato, come lo standalone) ma una modifica di un
	// solo carattere del contenuto o della firma lo rende senza valore.
	v := cookie(b, "__Host-gestrip_sessione_interna")
	dati, firma, ok := strings.Cut(v, ".")
	if !ok || dati == "" || firma == "" {
		t.Fatalf("cookie di sessione senza firma: %q", v)
	}
	for _, manomesso := range []string{altroPrimo(dati) + "." + firma, dati + "." + altroPrimo(firma), dati, dati + "."} {
		imposta(b, "__Host-gestrip_sessione_interna", manomesso)
		if r := b.vai("GET", "/", nil, nil); !redirectA(r, "/login") {
			t.Errorf("cookie manomesso %q accettato: %d", manomesso, r.codice)
		}
	}
	imposta(b, "__Host-gestrip_sessione_interna", v)
	if r := b.vai("GET", "/", nil, nil); r.codice != http.StatusOK {
		t.Errorf("cookie vero rifiutato: %d", r.codice)
	}
}

// altroPrimo cambia il primo carattere di s con un altro carattere base64url.
func altroPrimo(s string) string {
	if s[0] == 'A' {
		return "B" + s[1:]
	}
	return "A" + s[1:]
}

// rotteSenzaPagina sono le rotte della faccia interna che lo standalone serve fuori dal pipeline
// :browser (HealthController, PwaController, BrandingController, Plug.Static): niente
// put_secure_browser_headers.
var rotteSenzaPagina = []string{"GET /healthz", "GET /manifest.webmanifest", "GET /sw.js",
	"GET /branding/icon-192.png", "GET /branding/icon-512.png", "GET /assets/{nome}"}

// intestazioniPagina sono quelle di put_secure_browser_headers (Phoenix 1.8.13), misurate sullo
// standalone (slice 006): nessuna CSP propria oltre questa.
var intestazioniPagina = map[string]string{
	"Content-Security-Policy":           "base-uri 'self'; frame-ancestors 'self';",
	"Referrer-Policy":                   "strict-origin-when-cross-origin",
	"X-Content-Type-Options":            "nosniff",
	"X-Permitted-Cross-Domain-Policies": "none",
}

// intestazioniMai sono intestazioni di sicurezza che lo standalone non manda su nessuna risposta
// (né Phoenix né Caddy): il Go non ne aggiunge.
var intestazioniMai = []string{"X-Frame-Options", "Permissions-Policy", "Cross-Origin-Opener-Policy",
	"Cross-Origin-Embedder-Policy", "Cross-Origin-Resource-Policy", "X-Xss-Protection", "X-Download-Options",
	"Content-Security-Policy-Report-Only"}

// inventario: ACC-18
// Ogni rotta della faccia interna, da fuori e da dentro, GET e POST con il token: le pagine (tutto
// quello che lo standalone serve nel pipeline :browser, redirect e pagine d'errore delle rotte
// comprese) hanno le quattro intestazioni di put_secure_browser_headers, una volta e con i valori
// dello standalone; le rotte fuori da quel pipeline non le hanno. Su ogni risposta HSTS con un
// host vero, e nessuna intestazione di sicurezza che lo standalone non manda.
func TestIntestazioniDiSicurezzaSuOgniRotta(t *testing.T) {
	h, f, err := costruisciInterna(dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, &utentiFinti{attivo: true}), nil)
	if err != nil {
		t.Fatal(err)
	}
	fuori := nuovoBrowser(t, conHost(hostVero, h))
	fuori.vai("GET", "/login", nil, nil)
	dentro := nuovoBrowser(t, conHost(hostVero, h))
	dentro.entra(emailProva, passwordProva)
	dentro.vai("GET", "/", nil, nil)
	pagine := 0
	for _, b := range []*browser{fuori, dentro} {
		for _, rd := range f.rotte {
			if rd.pattern == "POST /logout" {
				continue // dopo, per non chiudere la sessione a metà
			}
			metodo, percorso := percorsoDi(rd.pattern)
			var form url.Values
			if metodo == "POST" {
				form = url.Values{"_csrf_token": {b.token}}
			}
			r := b.vai(metodo, percorso, form, nil)
			if r.codice == http.StatusForbidden {
				t.Errorf("%s: 403 con il token giusto", rd.pattern)
			}
			pagina := !slices.Contains(rotteSenzaPagina, rd.pattern)
			for k, v := range intestazioniPagina {
				got := r.h.Values(k)
				switch {
				case pagina && (len(got) != 1 || got[0] != v):
					t.Errorf("%s (%d): %s = %q, atteso %q", rd.pattern, r.codice, k, got, v)
				case !pagina && len(got) != 0:
					t.Errorf("%s (%d): %s = %q su una rotta fuori da :browser", rd.pattern, r.codice, k, got)
				}
			}
			if pagina {
				pagine++
			}
			if got := r.h.Get("Strict-Transport-Security"); got != hsts {
				t.Errorf("%s (%d): HSTS %q", rd.pattern, r.codice, got)
			}
			for _, k := range intestazioniMai {
				if r.h.Get(k) != "" {
					t.Errorf("%s: %s = %q, lo standalone non la manda", rd.pattern, k, r.h.Get(k))
				}
			}
		}
	}
	r := dentro.vai("POST", "/logout", url.Values{"_csrf_token": {dentro.token}}, nil)
	if !redirectA(r, "/login") || r.h.Get("Referrer-Policy") != "strict-origin-when-cross-origin" || r.h.Get("Strict-Transport-Security") != hsts {
		t.Errorf("uscita: %d %v", r.codice, r.h)
	}
	if pagine < 2*12 {
		t.Errorf("solo %d risposte di pagina controllate", pagine)
	}
}

// facciaPubblicaDiProva è la faccia pubblica vera più due rotte registrate con rottaPubblica,
// finché il Go non ha rotte pubbliche sue: una pagina /r/{token} (con il token CSRF se il token è
// «buono», altrimenti il rifiuto dello standalone) e un modulo /r/{token}/messaggio, che conta le
// volte che il gestore lavora.
func facciaPubblicaDiProva(t *testing.T, l *slog.Logger) (http.Handler, *facciaWeb, *int) {
	t.Helper()
	lavori := 0
	h, f, err := costruisciPubblica(dipendenzeProva(&sondaFinta{pronto: true}, l, &utentiFinti{attivo: true}), func(f *facciaWeb, mux *http.ServeMux) {
		f.rottaPubblica(mux, "GET /r/{token}", func(w http.ResponseWriter, r *http.Request) {
			if r.PathValue("token") != "buono" {
				RifiutoPubblico(w, r, rifiutoRiparazione, false)
				return
			}
			tok := f.csrf.Token(w, r)
			_, _ = w.Write([]byte(`<meta name="csrf-token" content="` + tok + `">`))
		})
		f.rottaPubblica(mux, "POST /r/{token}/messaggio", func(w http.ResponseWriter, r *http.Request) {
			lavori++
			_, _ = w.Write([]byte("ricevuto"))
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	return h, f, &lavori
}

// inventario: ACC-17
// Ogni POST della faccia pubblica (l'elenco registrato: oggi solo quello di prova, domani i moduli
// del cliente) vuole il token CSRF della faccia pubblica per costruzione: senza token, con un token
// sbagliato, con quello della faccia interna, solo nella query o con x-csrf-token sbagliato è il
// 403 «Forbidden» e il gestore non lavora; con il token giusto lavora.
func TestOgniPostPubblicoVuoleIlTokenCSRF(t *testing.T) {
	h, f, lavori := facciaPubblicaDiProva(t, nessunRegistro)
	post := postDellaFaccia(f)
	if !slices.Contains(post, "POST /r/{token}/messaggio") {
		t.Fatalf("rotte POST della faccia pubblica: %v", post)
	}
	interna := nuovoBrowser(t, internaProva(&sondaFinta{pronto: true}, nessunRegistro))
	interna.vai("GET", "/login", nil, nil)
	b := nuovoBrowser(t, h)
	if r := b.vai("GET", "/r/buono", nil, nil); r.codice != http.StatusOK || b.token == "" {
		t.Fatalf("pagina di prova: %d", r.codice)
	}
	for _, p := range post {
		metodo, percorso := percorsoDi(p)
		for nome, c := range map[string]struct {
			percorso string
			form     url.Values
			h        map[string]string
		}{
			"senza token":            {percorso, url.Values{"testo": {"x"}}, nil},
			"token sbagliato":        {percorso, url.Values{"_csrf_token": {"sbagliato"}}, nil},
			"token della interna":    {percorso, url.Values{"_csrf_token": {interna.token}}, nil},
			"token solo nella query": {percorso + "?_csrf_token=" + url.QueryEscape(b.token), url.Values{}, nil},
			"x-csrf-token sbagliato": {percorso, url.Values{}, map[string]string{"X-Csrf-Token": interna.token}},
		} {
			prima := *lavori
			r := b.vai(metodo, c.percorso, c.form, c.h)
			if r.codice != http.StatusForbidden || r.corpo != "Forbidden" || r.h.Get("Content-Security-Policy") != "" || *lavori != prima {
				t.Errorf("%s, %s: %d %q", p, nome, r.codice, r.corpo)
			}
		}
		prima := *lavori
		if r := b.vai(metodo, percorso, url.Values{"_csrf_token": {b.token}}, nil); r.codice == http.StatusForbidden || *lavori != prima+1 {
			t.Errorf("%s con il token giusto: %d", p, r.codice)
		}
	}
}

// inventario: ACC-18
// La faccia pubblica, rotta per rotta dall'elenco registrato: HSTS su ogni risposta con un host
// vero; /healthz no-store e senza le intestazioni delle pagine (fuori da :browser); le pagine e i
// moduli con le quattro intestazioni dello standalone; il rifiuto di una rotta pubblica con
// no-referrer e no-store (RifiutoPubblico); il 404 di una rotta che non c'è senza intestazioni di
// pagina; nessuna intestazione di sicurezza che lo standalone non manda.
func TestIntestazioniDiSicurezzaFacciaPubblica(t *testing.T) {
	h, f, _ := facciaPubblicaDiProva(t, nessunRegistro)
	b := nuovoBrowser(t, conHost(hostVero, h))
	b.vai("GET", "/r/buono", nil, nil)
	controlla := func(nome string, r risposta, pagina bool) {
		t.Helper()
		if r.h.Get("Strict-Transport-Security") != hsts {
			t.Errorf("%s (%d): HSTS %q", nome, r.codice, r.h.Get("Strict-Transport-Security"))
		}
		for k, v := range intestazioniPagina {
			got := r.h.Values(k)
			if k == "Referrer-Policy" && r.h.Get("Cache-Control") == "no-store" && pagina {
				v = "no-referrer" // il rifiuto delle rotte pubbliche (RifiutoPubblico)
			}
			switch {
			case pagina && (len(got) != 1 || got[0] != v):
				t.Errorf("%s (%d): %s = %q, atteso %q", nome, r.codice, k, got, v)
			case !pagina && len(got) != 0:
				t.Errorf("%s (%d): %s = %q fuori da :browser", nome, r.codice, k, got)
			}
		}
		for _, k := range intestazioniMai {
			if r.h.Get(k) != "" {
				t.Errorf("%s: %s = %q", nome, k, r.h.Get(k))
			}
		}
	}
	for _, rd := range f.rotte {
		metodo, percorso := percorsoDi(rd.pattern)
		var form url.Values
		if metodo == "POST" {
			form = url.Values{"_csrf_token": {b.token}}
		}
		r := b.vai(metodo, percorso, form, nil)
		controlla(rd.pattern, r, rd.pattern != rottaSalute)
		if rd.pattern == rottaSalute && r.h.Get("Cache-Control") != "no-store" {
			t.Errorf("/healthz: cache-control %q", r.h.Get("Cache-Control"))
		}
	}
	r := b.vai("GET", "/r/nessuno", nil, nil)
	if r.h.Get("Cache-Control") != "no-store" || r.h.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("rifiuto pubblico: %v", r.h)
	}
	controlla("rifiuto pubblico", r, true)
	r = b.vai("GET", "/non-esiste-inventato", nil, nil)
	if r.h.Get("Cache-Control") != cacheBase {
		t.Errorf("404: cache-control %q", r.h.Get("Cache-Control"))
	}
	controlla("404", r, false)
}

// Le due facce hanno elenchi di rotte separati (PIANO §1.5): nessuna rotta registrata su tutte e
// due, a parte /healthz.
func TestFacceConElenchiSeparati(t *testing.T) {
	_, fi, err := costruisciInterna(dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, &utentiFinti{attivo: true}), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, fp, err := costruisciPubblica(dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, &utentiFinti{attivo: true}), nil)
	if err != nil {
		t.Fatal(err)
	}
	interne := map[string]bool{}
	for _, r := range fi.rotte {
		interne[r.pattern] = true
	}
	pubbliche := 0
	for _, r := range fp.rotte {
		pubbliche++
		if interne[r.pattern] && r.pattern != rottaSalute {
			t.Errorf("%s registrata su tutte e due le facce", r.pattern)
		}
	}
	if pubbliche == 0 {
		t.Error("la faccia pubblica non dichiara le sue rotte")
	}
}

// segretoNelPercorso è il pezzo di token che non deve comparire in nessun registro né risposta.
const segretoNelPercorso = "tokensegretoinventato"

// registroCondiviso raccoglie il registro dell'app e quello di net/http (ErrorLog) da più
// connessioni.
type registroCondiviso struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (r *registroCondiviso) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.b.Write(p)
}

func (r *registroCondiviso) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.b.String()
}

// serverConErrorLog è un server di prova in chiaro con l'ErrorLog di net/http su reg, dove
// net/http scrive i panic dei gestori.
func serverConErrorLog(t *testing.T, h http.Handler, reg io.Writer) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(h)
	srv.Config.ErrorLog = log.New(reg, "", 0)
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

// manda fa una richiesta grezza; un errore (connessione chiusa da un panic) dà codice 0.
func manda(t *testing.T, srv *httptest.Server, metodo, percorso, corpo string, h map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(metodo, srv.URL+"/", strings.NewReader(corpo))
	if err != nil {
		t.Fatal(err)
	}
	// Percorso grezzo, così com'è scritto (anche con %72 e %2F).
	req.URL.Opaque = ""
	p, q, _ := strings.Cut(percorso, "?")
	req.URL.RawQuery = q
	if u, err := url.PathUnescape(p); err == nil {
		req.URL.Path, req.URL.RawPath = u, p
	}
	if corpo != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range h {
		req.Header.Set(k, v)
	}
	c := srv.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.Transport.(*http.Transport).DisableKeepAlives = true
	res, err := c.Do(req)
	if err != nil {
		return 0, ""
	}
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

// prefissiConToken sono i primi segmenti delle rotte pubbliche con una credenziale nel percorso
// (@unlogged_path_prefixes dell'endpoint dello standalone), più /m del portale ritirato e
// /configura del primo titolare (solo Go, ACC-16).
var prefissiConToken = []string{"r", "w", "lotto", "invito", "reset", "artigiano", "g", "stato", "foto", "thumb", "m", "configura"}

// inventario: ACC-19
// Le richieste con un token nel percorso o nella query, su tutti i prefissi a credenziale dello
// standalone e su ogni rotta registrata delle due facce, in chiaro e in codice, con ogni metodo,
// con gli errori (404, 400, 403 del CSRF, 413, 500 di una pagina) e con un gestore che va in
// panic con l'indirizzo nel valore: il token non compare mai nel registro dell'app, né
// nell'ErrorLog di net/http (dove net/http scrive i panic, su stderr in produzione), né nel
// corpo delle risposte d'errore. Il panic resta nel registro dell'app, con la rotta, il tipo del
// valore e lo stack (che ha gli argomenti in esadecimale, mai il contenuto delle stringhe).
func TestNessunTokenNeiRegistri(t *testing.T) {
	reg := &registroCondiviso{}
	l := slog.New(slog.NewTextHandler(reg, &slog.HandlerOptions{Level: slog.LevelDebug}))
	panico := func(w http.ResponseWriter, r *http.Request) {
		panic(fmt.Sprintf("guasto su %s con %s", r.URL.String(), r.PathValue("token")))
	}
	panicoErrore := func(w http.ResponseWriter, r *http.Request) {
		panic(&url.Error{Op: "Get", URL: r.URL.String(), Err: io.ErrUnexpectedEOF})
	}
	errore := func(w http.ResponseWriter, r *http.Request) { Errore(w, r) }
	di := dipendenzeProva(&sondaFinta{pronto: true}, l, &utentiFinti{attivo: true})
	hi, fi, err := costruisciInterna(di, func(f *facciaWeb, mux *http.ServeMux) {
		f.rotta(mux, "GET /prova/panico/{token}", aperta, panico)
		f.rotta(mux, "GET /prova/errore/{token}", aperta, errore)
	})
	if err != nil {
		t.Fatal(err)
	}
	dp := dipendenzeProva(&sondaFinta{pronto: true}, l, &utentiFinti{attivo: true})
	hp, fp, err := costruisciPubblica(dp, func(f *facciaWeb, mux *http.ServeMux) {
		f.rottaPubblica(mux, "GET /r/{token}", func(w http.ResponseWriter, r *http.Request) {
			RifiutoPubblico(w, r, rifiutoRiparazione, false)
		})
		f.rottaPubblica(mux, "POST /r/{token}/messaggio", func(w http.ResponseWriter, r *http.Request) {})
		f.rottaPubblica(mux, "GET /r/{token}/panico", panico)
		f.rottaPubblica(mux, "GET /w/{token}/panico", panicoErrore)
		f.rottaPubblica(mux, "GET /lotto/{token}/errore", errore)
	})
	if err != nil {
		t.Fatal(err)
	}
	si, sp := serverConErrorLog(t, hi, reg), serverConErrorLog(t, hp, reg)
	tok := segretoNelPercorso
	var percorsi []string
	for _, p := range prefissiConToken {
		percorsi = append(percorsi,
			"/"+p+"/"+tok, "/"+p+"/"+tok+"/x", "/"+p+"/1/"+tok, "/"+p+"/"+tok+"?t="+tok+"&pw="+tok,
			"/%"+fmt.Sprintf("%02x", p[0])+p[1:]+"/"+tok, "/"+p+"%2F"+tok, "//"+p+"//"+tok+"/",
			"/"+p+"/"+tok+"?x=%FF"+tok) // 400 di Plug
	}
	for _, rd := range slices.Concat(fi.rotte, fp.rotte) {
		_, percorso := percorsoDi(rd.pattern)
		percorsi = append(percorsi, strings.ReplaceAll(percorso, "/1", "/"+tok)+"?q="+tok)
	}
	percorsi = append(percorsi, "/prova/panico/"+tok, "/prova/errore/"+tok, "/r/"+tok+"/panico", "/w/"+tok+"/panico", "/lotto/"+tok+"/errore")
	panic0, n := 0, 0
	for _, srv := range []*httptest.Server{si, sp} {
		for _, p := range percorsi {
			for _, m := range []string{"GET", "HEAD", "POST", "DELETE", "PROPFIND"} {
				corpo := ""
				if m == "POST" {
					corpo = "_csrf_token=" + tok + "&password=" + tok
				}
				// Un indirizzo nuovo per ogni richiesta: il limite della faccia pubblica non
				// blocca, così ogni richiesta arriva alla sua rotta.
				n++
				codice, testo := manda(t, srv, m, p, corpo, map[string]string{"X-Forwarded-For": fmt.Sprintf("2001:db8:%x::1", n)})
				if codice == 0 {
					panic0++
				}
				if strings.Contains(testo, tok) {
					t.Errorf("%s %s: risposta %d con il token: %q", m, p, codice, testo)
				}
			}
		}
		// Un modulo troppo grande (413) su una rotta con il token.
		if c, _ := manda(t, srv, "POST", "/invito/"+tok, strings.Repeat("x", LimiteModulo+1), nil); srv == si && c != http.StatusRequestEntityTooLarge {
			t.Errorf("413 atteso: %d", c)
		}
	}
	if panic0 == 0 {
		t.Fatal("nessun panic arrivato a net/http: il test non prova lo stack")
	}
	testo := reg.String()
	for _, vietato := range []string{tok, "guasto su", "%FF", "%72", "PROPFIND"} {
		if strings.Contains(testo, vietato) {
			i := strings.Index(testo, vietato)
			t.Errorf("registro con %q: …%s…", vietato, testo[max(0, i-200):min(len(testo), i+200)])
		}
	}
	for _, atteso := range []string{`rotta="GET /prova/panico/{token}" codice=0 panic=true`, `rotta="GET /r/{token}/panico" codice=0 panic=true`,
		"tipo=string", "tipo=*url.Error", "stack=", "codice=403", "codice=413", "codice=400", `rotta="GET /prova/errore/{token}" codice=500`} {
		if !strings.Contains(testo, atteso) {
			t.Errorf("nel registro manca %q", atteso)
		}
	}
	if strings.Contains(testo, "http: panic serving") {
		t.Error("net/http ha scritto il panic nell'ErrorLog")
	}
}

// inventario: ACC-19
// Ogni rotta registrata delle due facce, con i segnaposto e la query pieni di un segreto, lascia
// nel registro una sola riga con il pattern della rotta: mai il percorso né la query.
func TestRegistroSenzaPercorsiSuOgniRotta(t *testing.T) {
	var reg bytes.Buffer
	l := slog.New(slog.NewTextHandler(&reg, nil))
	hi, fi, err := costruisciInterna(dipendenzeProva(&sondaFinta{pronto: true}, l, &utentiFinti{attivo: true}), nil)
	if err != nil {
		t.Fatal(err)
	}
	hp, fp, _ := facciaPubblicaDiProva(t, l)
	for _, c := range []struct {
		h      http.Handler
		f      *facciaWeb
		pagina string // dà il token CSRF della faccia
	}{{hi, fi, "/login"}, {hp, fp, "/r/buono"}} {
		b := nuovoBrowser(t, c.h)
		b.vai("GET", c.pagina, nil, nil)
		for _, rd := range c.f.rotte {
			metodo, percorso := percorsoDi(rd.pattern)
			percorso = strings.ReplaceAll(percorso, "/1", "/"+segretoNelPercorso) + "?q=" + segretoNelPercorso
			reg.Reset()
			var form url.Values
			if metodo == "POST" {
				form = url.Values{"_csrf_token": {b.token}}
			}
			b.vai(metodo, percorso, form, nil)
			if rd.pattern == rottaSalute {
				if reg.Len() != 0 {
					t.Errorf("/healthz registrata: %s", reg.String())
				}
				continue
			}
			righe := strings.Split(strings.TrimSpace(reg.String()), "\n")
			if len(righe) != 1 || !strings.Contains(righe[0], `rotta="`+rd.pattern+`"`) || strings.Contains(reg.String(), segretoNelPercorso) {
				t.Errorf("%s: registro %q", rd.pattern, reg.String())
			}
		}
	}
}
