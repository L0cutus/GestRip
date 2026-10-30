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

// inventario: ACC-19
// x-request-id (deciso il 30/10/2026): generato dal Go su ogni risposta, 20 caratteri come
// Plug.RequestId, mai quello mandato dal client; nel registro una riga per richiesta con lo stesso
// id, il metodo, la rotta (il pattern, mai il percorso né la query, che possono contenere token)
// e il codice. /healthz non si registra, come lo standalone (log: false).
func TestIDRichiestaERegistro(t *testing.T) {
	var reg bytes.Buffer
	l := slog.New(slog.NewTextHandler(&reg, nil))
	s := &sondaFinta{pronto: true}
	h := pubblicaProva(s, l)
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

// inventario: ACC-19
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

// inventario: ACC-19
// Il registro non scrive mai il percorso, nemmeno quando è in codice (/%72/<token>, /r%2F…): lo
// standalone filtra il log con un elenco di prefissi letto sul percorso grezzo, che la 219 ha
// segnalato come aggirabile; nel Go non c'è un elenco da aggirare (Stefano, 30/10/2026).
func TestRegistroSenzaPercorsoAncheInCodice(t *testing.T) {
	var reg bytes.Buffer
	mux := http.NewServeMux()
	mux.HandleFunc("GET /r/{token}", func(w http.ResponseWriter, r *http.Request) {})
	f := faccia(mux, slog.New(slog.NewTextHandler(&reg, nil)))
	for _, p := range []string{"/%72/tok1segreto", "/r/tok2%73egreto", "/%72%2Ftok3segreto", "/x/tok4segreto?t=tok5segreto", "//r//tok6segreto/"} {
		f.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", p, nil))
	}
	for _, x := range []string{"segreto", "%73egreto", "%72"} {
		if strings.Contains(reg.String(), x) {
			t.Errorf("registro con %q: %s", x, reg.String())
		}
	}
	if !strings.Contains(reg.String(), `rotta="GET /r/{token}"`) {
		t.Errorf("la rotta in codice non è stata riconosciuta: %s", reg.String())
	}
}

// inventario: ACC-19
// Un panic nel gestore: nel registro codice=0 (non era partito niente) con la rotta, l'id, il tipo del valore e lo stack,
// mai il valore (può contenere l'indirizzo). Verso net/http prosegue http.ErrAbortHandler, che
// chiude la connessione senza scrivere niente nel suo ErrorLog; un ErrAbortHandler del gestore
// passa com'è, senza stack.
func TestRegistroDelPanic(t *testing.T) {
	var reg bytes.Buffer
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rotto/{x}", func(w http.ResponseWriter, r *http.Request) { panic("prova-" + r.PathValue("x")) })
	mux.HandleFunc("GET /interrotto", func(w http.ResponseWriter, r *http.Request) { panic(http.ErrAbortHandler) })
	f := faccia(mux, slog.New(slog.NewTextHandler(&reg, nil)))
	for _, p := range []string{"/rotto/abcsegreto", "/interrotto"} {
		func() {
			defer func() {
				if p := recover(); p != http.ErrAbortHandler {
					t.Errorf("panic verso net/http: %v", p)
				}
			}()
			f.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", p, nil))
		}()
	}
	righe := strings.Split(strings.TrimSpace(reg.String()), "\n")
	if len(righe) != 2 || !strings.Contains(righe[0], `rotta="GET /rotto/{x}" codice=0 panic=true tipo=string stack="goroutine `) ||
		!strings.Contains(righe[0], "TestRegistroDelPanic") || strings.Contains(reg.String(), "abcsegreto") || strings.Contains(reg.String(), "prova-") {
		t.Errorf("registro del panic: %s", reg.String())
	}
	if !strings.HasSuffix(righe[1], `rotta="GET /interrotto" codice=0 panic=true`) {
		t.Errorf("registro dell'interruzione: %s", righe[1])
	}
}

// inventario: ACC-19
// La riga del panic scrive il codice che era già partito verso il client (200 se il gestore aveva
// scritto il corpo senza WriteHeader), o 0 se non era partito niente: mai un 500 che il client non
// ha visto.
func TestRegistroDelPanicConIlCodiceVero(t *testing.T) {
	var reg bytes.Buffer
	mux := http.NewServeMux()
	mux.HandleFunc("GET /niente", func(w http.ResponseWriter, r *http.Request) { panic("prova") })
	mux.HandleFunc("GET /corpo", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("mezza pagina"))
		panic("prova")
	})
	mux.HandleFunc("GET /creato", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		panic("prova")
	})
	f := faccia(mux, slog.New(slog.NewTextHandler(&reg, nil)))
	for p, atteso := range map[string]string{"/niente": "codice=0", "/corpo": "codice=200", "/creato": "codice=201"} {
		reg.Reset()
		func() {
			defer func() { _ = recover() }() // http.ErrAbortHandler, provato da TestRegistroDelPanic
			f.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", p, nil))
		}()
		if !strings.Contains(reg.String(), `rotta="GET `+p+`" `+atteso+" panic=true") {
			t.Errorf("%s: registro %s", p, reg.String())
		}
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
