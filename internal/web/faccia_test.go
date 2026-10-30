package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	corpoHTML = "Not Found"
	corpoJSON = `{"errors":{"detail":"Not Found"}}`
	tipoHTML  = "text/html; charset=utf-8"
	tipoJSON  = "application/json; charset=utf-8"
)

// inventario: ADM-20
// Il 404 è quello dello standalone (Phoenix ErrorHTML/ErrorJSON), misurato sul banco contro
// go-baseline il 30/10/2026: «Not Found» in HTML, o in JSON se il client lo chiede; un metodo
// diverso da GET e HEAD su /healthz è un 404 come una rotta inesistente (niente 405).
func TestNonTrovataComeLoStandalone(t *testing.T) {
	casi := []struct{ metodo, percorso, accept, tipo, corpo string }{
		{"GET", "/non-esiste-inventato", "", tipoHTML, corpoHTML},
		{"GET", "/non-esiste-inventato", "text/html", tipoHTML, corpoHTML},
		{"GET", "/non-esiste-inventato", "application/json", tipoJSON, corpoJSON},
		{"GET", "/non-esiste-inventato.png", "image/png", tipoHTML, corpoHTML},
		{"POST", "/healthz", "", tipoHTML, corpoHTML},
		{"PUT", "/healthz", "application/json", tipoJSON, corpoJSON},
		{"DELETE", "/non-esiste-inventato", "", tipoHTML, corpoHTML},
		{"OPTIONS", "/healthz", "", tipoHTML, corpoHTML},
		{"GET", "/HEALTHZ", "", tipoHTML, corpoHTML},
		{"GET", "/healthz/x", "", tipoHTML, corpoHTML},
		{"GET", "/healthz;x", "", tipoHTML, corpoHTML},
		{"GET", "/./healthz", "", tipoHTML, corpoHTML},
		{"GET", "/x/../healthz", "", tipoHTML, corpoHTML},
		{"GET", "/non-esiste-inventato?_format=json", "", tipoJSON, corpoJSON},
		{"GET", "/non-esiste-inventato?_format=json", "text/html", tipoJSON, corpoJSON},
		{"GET", "/non-esiste-inventato?_format=xml", "", tipoHTML, corpoHTML},
	}
	s := &sondaFinta{pronto: true}
	for nome, h := range map[string]http.Handler{"interna": Interna(s), "pubblica": Pubblica(s)} {
		for _, c := range casi {
			req := httptest.NewRequest(c.metodo, c.percorso, nil)
			req.Host = "negozio.esempio.ts.net"
			if c.accept != "" {
				req.Header.Set("Accept", c.accept)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			r := rec.Result()
			if r.StatusCode != http.StatusNotFound || rec.Body.String() != c.corpo {
				t.Errorf("%s %s %s accept %q: %d %q, atteso 404 %q", nome, c.metodo, c.percorso, c.accept, r.StatusCode, rec.Body.String(), c.corpo)
			}
			if got := r.Header.Get("Content-Type"); got != c.tipo {
				t.Errorf("%s %s %s: content-type %q, atteso %q", nome, c.metodo, c.percorso, got, c.tipo)
			}
			if got := r.Header.Values("Cache-Control"); len(got) != 1 || got[0] != "max-age=0, private, must-revalidate" {
				t.Errorf("%s %s %s: cache-control %q", nome, c.metodo, c.percorso, got)
			}
			if got := r.Header.Get("Strict-Transport-Security"); got != "max-age=31536000" {
				t.Errorf("%s %s %s: hsts %q", nome, c.metodo, c.percorso, got)
			}
			for _, k := range []string{"Allow", "X-Content-Type-Options", "Content-Security-Policy", "Set-Cookie", "Location"} {
				if r.Header.Get(k) != "" {
					t.Errorf("%s %s %s: %s presente nel 404", nome, c.metodo, c.percorso, k)
				}
			}
		}
	}
}

// inventario: ADM-20
// Come Phoenix, le barre in più non contano e un segmento codificato vale come quello in chiaro.
func TestPercorsiEquivalentiAHealthz(t *testing.T) {
	s := &sondaFinta{pronto: true}
	for _, p := range []string{"/healthz/", "//healthz", "/healthz//", "/heal%74hz", "/healthz?x=%zz"} {
		for nome, h := range map[string]http.Handler{"interna": Interna(s), "pubblica": Pubblica(s)} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
			if rec.Code != http.StatusOK || rec.Body.String() != "ok" || rec.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("%s GET %s = %d %q (cache-control %q), atteso 200 ok no-store", nome, p, rec.Code, rec.Body.String(), rec.Header().Get("Cache-Control"))
			}
			if rec.Header().Get("Location") != "" {
				t.Errorf("%s GET %s: redirect a %q", nome, p, rec.Header().Get("Location"))
			}
		}
	}
}

// inventario: ADM-20
// HSTS su ogni risposta, tranne per gli host esclusi dallo standalone (force_ssl, prod.exs).
func TestHSTS(t *testing.T) {
	s := &sondaFinta{pronto: true}
	casi := map[string]string{
		"negozio.esempio.ts.net":      "max-age=31536000",
		"negozio.esempio.ts.net:8443": "max-age=31536000",
		"localhost":                   "",
		"localhost:47101":             "",
		"127.0.0.1:47102":             "",
		"[::1]:47101":                 "max-age=31536000", // lo standalone esclude solo localhost e 127.0.0.1
	}
	for host, atteso := range casi {
		for _, p := range []string{"/healthz", "/non-esiste-inventato"} {
			req := httptest.NewRequest("GET", p, nil)
			req.Host = host
			rec := httptest.NewRecorder()
			Pubblica(s).ServeHTTP(rec, req)
			if got := rec.Header().Get("Strict-Transport-Security"); got != atteso {
				t.Errorf("host %s %s: hsts %q, atteso %q", host, p, got, atteso)
			}
		}
	}
}

// inventario: ADM-20
// HEAD su una rotta inesistente: 404 senza corpo, con la lunghezza del corpo di GET (come lo
// standalone: content-length 9). Su un server vero, perché è net/http a scartare il corpo.
func TestNonTrovataHEAD(t *testing.T) {
	s := httptest.NewServer(Interna(&sondaFinta{pronto: true}))
	defer s.Close()
	req, _ := http.NewRequest("HEAD", s.URL+"/non-esiste-inventato", nil)
	req.Header.Set("Accept", "text/html")
	r, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if r.StatusCode != http.StatusNotFound || r.Header.Get("Content-Type") != tipoHTML || len(b) != 0 || r.ContentLength != 9 {
		t.Errorf("HEAD = %d %q, corpo %q, content-length %d", r.StatusCode, r.Header.Get("Content-Type"), b, r.ContentLength)
	}
}

// La scelta tra HTML e JSON segue Phoenix.Controller.accepts/2 (Phoenix 1.8.13): un «*/*» in
// qualsiasi punto dà HTML; una voce con q=1 che corrisponde vince subito; le altre si ordinano
// per q decrescente e poi per il testo grezzo della voce (spazi compresi); nessuna
// corrispondenza dà HTML. Tutti i casi sono misurati sullo standalone il 30/10/2026.
func TestFormatoErrore(t *testing.T) {
	casi := map[string]string{
		"":                                  "html",
		"text/html":                         "html",
		"application/json":                  "json",
		"APPLICATION/JSON":                  "json",
		" application/json ":                "json",
		"application/json; charset=utf-8":   "json",
		"application/json, text/html":       "json",
		"text/html;q=0.5, application/json": "json",
		"application/json;q=0.5, text/html": "html",
		"application/json,text/html;q=0.9":  "json",
		"application/json, */*":             "html",
		"application/*":                     "json",
		"text/*":                            "html",
		"application/json;q=0":              "json",
		"application/vnd.api+json":          "html",
		"text/plain":                        "html",
		"image/png":                         "html",
		"json":                              "html",
		"text/html;q=0.5, application/json;q=0.5":   "json", // a pari q vince il testo minore
		" text/html;q=0.5, application/json;q=0.5":  "json",
		"application/json;q=0.5, text/html;q=0.5":   "html", // lo spazio dopo la virgola fa parte del testo
		"application/json;q=0.5,text/html;q=0.5":    "json",
		"text/html;q=0.5,application/json;q=0.5":    "json",
		"application/json;q=1.0, text/html":         "json",
		"text/html;q=1, application/json":           "html",
		"application/json;q=0.5, text/html;q=0.4":   "json",
		"application/json;q=0.4, text/html;q=0.5":   "html",
		"application/json;q=abc":                    "json", // q illeggibile vale 1
		"text/plain, application/json;q=0.1":        "json",
		"application/json;q=\"0.5\", text/html":     "html", // virgolette tolte
		"application/json;q= 0.5, text/html;q=0.6":  "json", // spazio nel valore: q illeggibile, vale 1
		"application/json;q =0.5, text/html;q=0.6":  "json", // spazio dopo la chiave: non è q
		"application/json;q=\t0.5, text/html;q=0.6": "json",
		"application/json; q=0.5, text/html;q=0.6":  "html", // spazio prima della chiave: conta
		"application/json;q=0.5 , text/html;q=0.4":  "json",
	}
	for accept, atteso := range casi {
		req := httptest.NewRequest("GET", "/x", nil)
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		if got := formatoErrore(req); got != atteso {
			t.Errorf("Accept %q: %s, atteso %s", accept, got, atteso)
		}
	}
	// Solo la prima intestazione Accept conta, come in Phoenix.
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Add("Accept", "text/html")
	req.Header.Add("Accept", "application/json")
	if got := formatoErrore(req); got != "html" {
		t.Errorf("due Accept: %s", got)
	}
	// _format nella query vince sull'intestazione; l'ultimo valore conta.
	for q, atteso := range map[string]string{"_format=json": "json", "_format=html": "html", "_format=xml": "html", "_format=html&_format=json": "json"} {
		req := httptest.NewRequest("GET", "/x?"+q, nil)
		req.Header.Set("Accept", "text/html")
		if got := formatoErrore(req); got != atteso {
			t.Errorf("?%s: %s, atteso %s", q, got, atteso)
		}
	}
}

// Le intestazioni di sicurezza delle pagine sono quelle di put_secure_browser_headers dello
// standalone (Phoenix 1.8.13), misurate su /login. Non ci sono ancora pagine nel Go: il pezzo
// è pronto per la 008 e si chiude con il banco su /login.
func TestIntestazioniPagina(t *testing.T) {
	rec := httptest.NewRecorder()
	Pagina(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("x")) })).
		ServeHTTP(rec, httptest.NewRequest("GET", "/login", nil))
	attese := map[string]string{
		"Content-Security-Policy":           "base-uri 'self'; frame-ancestors 'self';",
		"Referrer-Policy":                   "strict-origin-when-cross-origin",
		"X-Content-Type-Options":            "nosniff",
		"X-Permitted-Cross-Domain-Policies": "none",
	}
	for k, v := range attese {
		if got := rec.Header().Values(k); len(got) != 1 || got[0] != v {
			t.Errorf("%s = %q, atteso %q", k, got, v)
		}
	}
	if strings.Contains(rec.Header().Get("Content-Security-Policy"), "script-src") {
		t.Error("CSP più stretta di quella dello standalone")
	}
}

// inventario: ADM-20
// Il Cache-Control di Plug vale per ogni risposta che non ne sceglie uno; il 404 lo rimette
// anche se un gestore aveva già scelto altro (il 404 di blocco del limitatore è identico, D-05).
func TestCacheControlDiBase(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /muta", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("x")) })
	rec := httptest.NewRecorder()
	faccia(mux).ServeHTTP(rec, httptest.NewRequest("GET", "/muta", nil))
	if got := rec.Header().Get("Cache-Control"); got != "max-age=0, private, must-revalidate" {
		t.Errorf("cache-control di base = %q", got)
	}
	rec = httptest.NewRecorder()
	rec.Header().Set("Cache-Control", "no-store")
	NonTrovata(rec, httptest.NewRequest("GET", "/x", nil))
	if got := rec.Header().Values("Cache-Control"); len(got) != 1 || got[0] != "max-age=0, private, must-revalidate" {
		t.Errorf("cache-control del 404 = %q", got)
	}
}

// inventario: ADM-20
// Le rotte con parametri ricevono i valori anche dopo la riscrittura del percorso.
func TestFacciaPassaIParametri(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /foto/{nome}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.PathValue("nome") + "|" + r.Pattern))
	})
	for _, p := range []string{"/foto/abc", "//foto//abc/", "/fo%74o/abc"} {
		rec := httptest.NewRecorder()
		faccia(mux).ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != 200 || rec.Body.String() != "abc|GET /foto/{nome}" {
			t.Errorf("%s = %d %q", p, rec.Code, rec.Body.String())
		}
	}
}

// inventario: ADM-20
// Un pattern a sottoalbero («/r/») non porta mai a un redirect del mux: Phoenix non ne ha.
func TestFacciaNessunRedirectDelMux(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /r/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("r")) })
	for _, p := range []string{"/r", "/r/", "/r/x"} {
		rec := httptest.NewRecorder()
		faccia(mux).ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != http.StatusNotFound || rec.Header().Get("Location") != "" {
			t.Errorf("%s = %d (location %q), atteso 404", p, rec.Code, rec.Header().Get("Location"))
		}
	}
}

// inventario: ADM-20
// Query come Plug: _format dall'ultima assegnazione (una chiave con parentesi la rende non
// testo), «;» non separa, un % illeggibile resta com'è, un UTF-8 non valido dà 400 «Bad Request»
// senza HSTS, con la scelta HTML/JSON dalla sola intestazione Accept. Misure del 30/10/2026.
func TestQueryComePlug(t *testing.T) {
	casi := []struct {
		percorso, accept string
		codice           int
		tipo, corpo      string
		hsts             bool
	}{
		{"/non-esiste?_format=json&x=%zz", "", 404, tipoJSON, corpoJSON, true},
		{"/non-esiste?_format=%zz", "application/json", 404, tipoHTML, corpoHTML, true},
		{"/non-esiste?_format[]=json", "application/json", 404, tipoHTML, corpoHTML, true},
		{"/non-esiste?_format=json&_format[]=x", "application/json", 404, tipoHTML, corpoHTML, true},
		{"/non-esiste?_format[]=", "application/json", 404, tipoHTML, corpoHTML, true},
		{"/non-esiste?_format=", "application/json", 404, tipoHTML, corpoHTML, true},
		{"/non-esiste?_format=json;x", "application/json", 404, tipoHTML, corpoHTML, true},
		{"/non-esiste?x;y=1&_format=json", "", 404, tipoJSON, corpoJSON, true},
		{"/non-esiste?%5Fformat=json", "", 404, tipoJSON, corpoJSON, true},
		{"/non-esiste?_format=js%6Fn", "", 404, tipoJSON, corpoJSON, true},
		{"/healthz?%", "", 200, "text/plain; charset=utf-8", "ok", true},
		{"/healthz?x=%F", "", 200, "text/plain; charset=utf-8", "ok", true},
		{"/non-esiste?_format=%6a%73on", "", 404, tipoJSON, corpoJSON, true},
		{"/non-esiste?&&_format=json&", "", 404, tipoJSON, corpoJSON, true},
		{"/healthz?a=%FF", "", 400, tipoHTML, "Bad Request", false},
		{"/non-esiste?a=%FF", "", 400, tipoHTML, "Bad Request", false},
		{"/non-esiste?%FF=1", "", 400, tipoHTML, "Bad Request", false},
		{"/non-esiste?_format=json&x=%FF", "", 400, tipoHTML, "Bad Request", false},
		{"/non-esiste?x=%FF", "application/json", 400, tipoJSON, `{"errors":{"detail":"Bad Request"}}`, false},
	}
	for _, c := range casi {
		req := httptest.NewRequest("GET", "/", nil)
		req.URL.Path, req.URL.RawQuery = c.percorso[:strings.Index(c.percorso, "?")], c.percorso[strings.Index(c.percorso, "?")+1:]
		req.Host = "negozio.esempio.ts.net"
		if c.accept != "" {
			req.Header.Set("Accept", c.accept)
		}
		rec := httptest.NewRecorder()
		Pubblica(&sondaFinta{pronto: true}).ServeHTTP(rec, req)
		if rec.Code != c.codice || rec.Body.String() != c.corpo || rec.Header().Get("Content-Type") != c.tipo {
			t.Errorf("%s accept %q = %d %q %q, atteso %d %q %q", c.percorso, c.accept, rec.Code, rec.Header().Get("Content-Type"), rec.Body.String(), c.codice, c.tipo, c.corpo)
		}
		if got := rec.Header().Get("Strict-Transport-Security") != ""; got != c.hsts {
			t.Errorf("%s: hsts presente = %v, atteso %v", c.percorso, got, c.hsts)
		}
		if c.codice == 400 && rec.Header().Get("Cache-Control") != "max-age=0, private, must-revalidate" {
			t.Errorf("%s: cache-control %q", c.percorso, rec.Header().Get("Cache-Control"))
		}
	}
}
