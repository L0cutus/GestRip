package web

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var reIDRichiesta = regexp.MustCompile(`^[A-Za-z0-9_-]{20}$`)

// x-request-id (deciso il 30/10/2026): generato dal Go su ogni risposta, 20 caratteri come
// Plug.RequestId, mai quello mandato dal client; nel registro una riga per richiesta con lo stesso
// id, il metodo, la rotta (il pattern, mai il percorso né la query, che possono contenere token)
// e il codice. /healthz non si registra, come lo standalone (log: false).
func TestIDRichiestaERegistro(t *testing.T) {
	var reg bytes.Buffer
	l := slog.New(slog.NewTextHandler(&reg, nil))
	s := &sondaFinta{pronto: true}
	h := Pubblica(s, l)
	visti := map[string]bool{}
	for _, c := range []struct{ metodo, percorso string }{
		{"GET", "/healthz"},
		{"GET", "/r/token-segreto-inventato?t=altro-segreto"},
		{"POST", "/healthz"},
		{"GET", "/non-esiste?x=%FF"},
	} {
		req := httptest.NewRequest(c.metodo, "/", nil)
		req.URL.Path, req.URL.RawQuery, _ = strings.Cut(c.percorso, "?")
		req.Header.Set("X-Request-Id", "id-del-cliente-lungo-abbastanza-1234")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		id := rec.Header().Get("X-Request-Id")
		if rec.Code == http.StatusBadRequest {
			// Il 400 dello standalone non ha x-request-id (misurato): la riga di registro c'è.
			if id != "" || !strings.Contains(reg.String(), "codice=400") {
				t.Errorf("400: x-request-id %q, registro %q", id, reg.String())
			}
			continue
		}
		if !reIDRichiesta.MatchString(id) || visti[id] {
			t.Errorf("%s %s: x-request-id %q (deve essere nuovo, 20 caratteri)", c.metodo, c.percorso, id)
		}
		visti[id] = true
		riga := ""
		for _, r := range strings.Split(reg.String(), "\n") {
			if strings.Contains(r, id) {
				riga = r
			}
		}
		if c.percorso == "/healthz" && c.metodo == "GET" {
			if riga != "" {
				t.Errorf("/healthz registrata: %q", riga)
			}
			continue
		}
		if riga == "" || !strings.Contains(riga, "metodo="+c.metodo) || !strings.Contains(riga, "codice="+strconv.Itoa(rec.Code)) {
			t.Errorf("%s %s: riga di registro %q", c.metodo, c.percorso, riga)
		}
	}
	for _, vietato := range []string{"segreto", "altro", "%FF", "id-del-cliente"} {
		if strings.Contains(reg.String(), vietato) {
			t.Errorf("il registro contiene %q:\n%s", vietato, reg.String())
		}
	}
}

// Nel registro la rotta è il pattern del mux: dice quale pagina senza scrivere il token.
func TestRegistroScriveLaRotta(t *testing.T) {
	var reg bytes.Buffer
	mux := http.NewServeMux()
	mux.HandleFunc("GET /r/{token}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.WriteHeader(http.StatusInternalServerError) // ignorato da net/http: conta il primo
	})
	f := faccia(mux, slog.New(slog.NewTextHandler(&reg, nil)))
	f.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/r/abc123segreto", nil))
	if !strings.Contains(reg.String(), `rotta="GET /r/{token}" codice=201`) || strings.Contains(reg.String(), "abc123segreto") {
		t.Errorf("registro: %s", reg.String())
	}
	reg.Reset()
	f.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/altro", nil))
	if !strings.Contains(reg.String(), "rotta=nessuna codice=404") {
		t.Errorf("registro del 404: %s", reg.String())
	}
}

// Un panic nel gestore: nel registro codice=500 con la rotta e l'id (lo stack di net/http non ha
// l'id); il panic prosegue verso net/http, che chiude la connessione.
func TestRegistroDelPanic(t *testing.T) {
	var reg bytes.Buffer
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rotto/{x}", func(w http.ResponseWriter, r *http.Request) { panic("prova") })
	f := faccia(mux, slog.New(slog.NewTextHandler(&reg, nil)))
	func() {
		defer func() {
			if p := recover(); p != "prova" {
				t.Errorf("panic non rilanciato: %v", p)
			}
		}()
		f.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/rotto/abc", nil))
	}()
	if !strings.Contains(reg.String(), `rotta="GET /rotto/{x}" codice=500 panic=true`) {
		t.Errorf("registro: %s", reg.String())
	}
}

// Nel registro il metodo è uno di quelli noti, o «altro»: un metodo lunghissimo scelto dal
// client non riempie il registro.
func TestRegistroMetodo(t *testing.T) {
	var reg bytes.Buffer
	f := faccia(http.NewServeMux(), slog.New(slog.NewTextHandler(&reg, nil)))
	req := httptest.NewRequest("GET", "/x", nil)
	req.Method = strings.Repeat("A", 10000)
	f.ServeHTTP(httptest.NewRecorder(), req)
	if !strings.Contains(reg.String(), "metodo=altro") || len(reg.String()) > 500 {
		t.Errorf("registro (%d byte): %.200s", len(reg.String()), reg.String())
	}
}

// Una risposta 1xx (103 Early Hints) non è il codice finale.
func TestRegistroIgnora1xx(t *testing.T) {
	var reg bytes.Buffer
	mux := http.NewServeMux()
	mux.HandleFunc("GET /x", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusEarlyHints)
		w.WriteHeader(http.StatusAccepted)
	})
	faccia(mux, slog.New(slog.NewTextHandler(&reg, nil))).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	if !strings.Contains(reg.String(), "codice=202") {
		t.Errorf("registro: %s", reg.String())
	}
}
