package web

import (
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Le regole comuni alle due facce, copiate dallo standalone (Phoenix 1.8.13, Plug 1.20.3) e
// misurate sul banco contro go-baseline il 30/10/2026 (ricevuta della slice 006).

const (
	// cacheBase è il Cache-Control che Plug mette su ogni risposta che non ne sceglie uno.
	cacheBase = "max-age=0, private, must-revalidate"
	// hsts è quello di force_ssl (Plug.SSL) nello standalone.
	hsts = "max-age=31536000"
)

// hostSenzaHSTS sono gli host esclusi da force_ssl in config/prod.exs.
var hostSenzaHSTS = []string{"localhost", "127.0.0.1"}

// faccia avvolge il mux di una faccia:
//   - il percorso si legge come Phoenix: le barre in più non contano, un segmento codificato
//     vale come quello in chiaro, «.» e «..» non sono speciali (nessuna rotta li ha);
//   - nessun redirect di pulizia del percorso e nessun 405: quello che il mux non serve è il 404
//     dello standalone;
//   - una query con UTF-8 non valido è il 400 dello standalone (Plug.Parsers);
//   - HSTS su ogni risposta, Cache-Control di Plug se il gestore non ne mette uno;
//   - x-request-id nuovo su ogni risposta e una riga di registro per richiesta.
func faccia(mux *http.ServeMux, registro *slog.Logger) http.Handler {
	return facciaConFiltro(mux, registro, nil)
}

// filtro sta fra la lettura del percorso e la scelta della rotta, come un plug dell'endpoint
// prima del router: vede ogni richiesta con il percorso canonico, comprese quelle che il mux non
// serve, e decide se passarla avanti (il limite della faccia pubblica, limiti.go).
type filtro func(w http.ResponseWriter, r *http.Request, avanti http.HandlerFunc)

func facciaConFiltro(mux *http.ServeMux, registro *slog.Logger, prima filtro) http.Handler {
	return http.HandlerFunc(func(w0 http.ResponseWriter, r *http.Request) {
		id := nuovoIDRichiesta()
		w := &conCodice{ResponseWriter: w0}
		var r2 *http.Request // la richiesta passata al mux, che vi scrive il pattern
		defer func() {
			// Mai percorso né query: la rotta è il pattern del mux, che non contiene token.
			rotta := "nessuna"
			if r2 != nil && r2.Pattern != "" {
				rotta = r2.Pattern
			}
			p := recover()
			if p != nil {
				// Lo stack di net/http non ha l'id: la riga del registro lo collega alla rotta.
				registro.Error("richiesta", "id", id, "metodo", metodoPerRegistro(r.Method), "rotta", rotta, "codice", http.StatusInternalServerError, "panic", true)
				panic(p)
			}
			// /healthz non si registra (log: false nello standalone).
			if rotta != rottaSalute {
				registro.Info("richiesta", "id", id, "metodo", metodoPerRegistro(r.Method), "rotta", rotta, "codice", w.codice())
			}
		}()
		h := w.Header()
		h.Set("X-Request-Id", id)
		h.Set("Cache-Control", cacheBase)
		host := r.Host
		if solo, _, err := net.SplitHostPort(r.Host); err == nil {
			host = solo
		}
		if !slices.Contains(hostSenzaHSTS, host) {
			h.Set("Strict-Transport-Security", hsts)
		}
		if _, _, ok := formatoQuery(r.URL.RawQuery); !ok {
			NonValida(w, r)
			return
		}
		var ok bool
		r2, ok = percorsoPhoenix(r)
		if !ok {
			NonTrovata(w, r)
			return
		}
		smista := func(w http.ResponseWriter, r2 *http.Request) {
			// Un pattern a sottoalbero («/r/») farebbe redirect del mux: Phoenix non ne ha,
			// quindi le rotte non li usano e qui sono un 404.
			if _, pattern := mux.Handler(r2); pattern == "" || strings.HasSuffix(pattern, "/") {
				NonTrovata(w, r2)
				return
			}
			// mux.ServeHTTP, non il gestore trovato: è lui che compila r.PathValue e r.Pattern.
			mux.ServeHTTP(w, r2)
		}
		if prima != nil {
			prima(w, r2, smista)
			return
		}
		smista(w, r2)
	})
}

// metodiNoti sono i metodi che il registro scrive per nome: gli altri sono «altro», così un
// metodo lunghissimo scelto dal client non riempie il registro.
var metodiNoti = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}

func metodoPerRegistro(m string) string {
	if slices.Contains(metodiNoti, m) {
		return m
	}
	return "altro"
}

// rottaSalute è il pattern di /healthz, che non si registra.
const rottaSalute = "GET /healthz"

// nuovoIDRichiesta è un id casuale di 20 caratteri, come Plug.RequestId (15 byte in base64
// per URL). Quello mandato dal client non si usa (deciso il 30/10/2026).
func nuovoIDRichiesta() string {
	b := make([]byte, 15)
	_, _ = rand.Read(b) // crypto/rand.Read non fallisce (documentato: in caso estremo il processo si ferma)
	return base64.RawURLEncoding.EncodeToString(b)
}

// conCodice ricorda il codice di risposta per il registro: il primo WriteHeader finale (non 1xx),
// come net/http; senza WriteHeader è 200. Flush e simili passano da http.ResponseController
// (Unwrap); un'asserzione diretta a http.Flusher su w non funziona.
type conCodice struct {
	http.ResponseWriter
	scritto int
}

func (c *conCodice) WriteHeader(n int) {
	if c.scritto == 0 && n >= 200 { // un 1xx (103 Early Hints) non è il codice finale
		c.scritto = n
	}
	c.ResponseWriter.WriteHeader(n)
}

// Unwrap lascia a http.ResponseController le funzioni del ResponseWriter vero (Flush…).
func (c *conCodice) Unwrap() http.ResponseWriter { return c.ResponseWriter }

func (c *conCodice) codice() int {
	if c.scritto == 0 {
		return http.StatusOK
	}
	return c.scritto
}

// percorsoPhoenix riscrive il percorso nella forma canonica dei segmenti di Phoenix
// (split su «/», segmenti vuoti tolti). Dice no se un segmento è «.» o «..», che il mux
// ripulirebbe con un redirect e che in Phoenix non corrispondono a nessuna rotta.
func percorsoPhoenix(r *http.Request) (*http.Request, bool) {
	var segni []string
	for _, s := range strings.Split(r.URL.EscapedPath(), "/") {
		if s == "" {
			continue
		}
		if s == "." || s == ".." {
			return nil, false
		}
		segni = append(segni, s)
	}
	grezzo := "/" + strings.Join(segni, "/")
	chiaro, err := url.PathUnescape(grezzo)
	if err != nil {
		return nil, false
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path, r2.URL.RawPath = chiaro, grezzo
	return r2, true
}

// NonTrovata è il 404 dello standalone: «Not Found» in HTML, o in JSON se il client lo
// chiede (formatoErrore). Nessuna intestazione di sicurezza delle pagine, come lo standalone.
func NonTrovata(w http.ResponseWriter, r *http.Request) {
	paginaErrore(w, r, http.StatusNotFound, "Not Found")
}

// NonValida è il 400 dello standalone per una richiesta che Plug non sa leggere: «Bad Request»
// in HTML o JSON secondo la sola intestazione Accept (i parametri non si sono potuti leggere), e
// senza HSTS né x-request-id, come misurato.
func NonValida(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Del("Strict-Transport-Security")
	h.Del("X-Request-Id")
	h.Set("Cache-Control", cacheBase)
	if formatoAccept(r) == "json" {
		h.Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":{"detail":"Bad Request"}}`))
		return
	}
	h.Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write([]byte("Bad Request"))
}

type statoFormato int

const (
	formatoAssente  statoFormato = iota
	formatoTesto                 // _format=valore
	formatoNonTesto              // _format[]=…, _format[x]=…: Phoenix non lo accetta
)

// formatoQuery legge _format dalla query come Plug.Conn.Query: coppie separate solo da «&»
// (il «;» fa parte del valore), chiave e valore decodificati con un «%» illeggibile lasciato
// com'è; vale l'ultima assegnazione, e una chiave con parentesi
// («_format[]», «_format[x]») la rende non testo. ok è falso se una chiave o un valore non è
// UTF-8 valido: Plug risponde 400.
func formatoQuery(grezza string) (valore string, stato statoFormato, ok bool) {
	for _, coppia := range strings.Split(grezza, "&") {
		k, v, _ := strings.Cut(coppia, "=")
		k, v = decodificaForm(k), decodificaForm(v)
		if !utf8.ValidString(k) || !utf8.ValidString(v) {
			return "", formatoAssente, false
		}
		radice, _, conParentesi := strings.Cut(k, "[")
		if radice != "_format" {
			continue
		}
		valore, stato = v, formatoTesto
		if conParentesi {
			stato = formatoNonTesto
		}
	}
	return valore, stato, true
}

// decodificaForm decodifica «%XX» in un byte; un «%» non seguito da due cifre esadecimali resta
// com'è. Il «+» (uno spazio, per Plug) qui non serve: non cambia né la validità UTF-8 né il nome
// «_format».
func decodificaForm(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '%' && i+2 < len(s) && esadecimale(s[i+1]) && esadecimale(s[i+2]):
			n, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
			b.WriteByte(byte(n))
			i += 2
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func esadecimale(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// formati sono i formati delle pagine d'errore dello standalone, nell'ordine di render_errors:
// il primo è quello di ripiego.
var formati = []struct{ nome, tipo, sotto string }{
	{"html", "text", "html"},
	{"json", "application", "json"},
}

// formatoErrore sceglie html o json come Phoenix.Controller.accepts/2: _format nella query
// vince; un «*/*» nella prima intestazione Accept dà il primo formato; una voce con q=1 che
// corrisponde vince subito; le altre si ordinano per q decrescente e poi per il testo grezzo
// della voce; senza corrispondenze, il primo formato.
func formatoErrore(r *http.Request) string {
	if v, stato, _ := formatoQuery(r.URL.RawQuery); stato != formatoAssente {
		for _, f := range formati {
			if stato == formatoTesto && v == f.nome {
				return f.nome
			}
		}
		return formati[0].nome
	}
	return formatoAccept(r)
}

// formatoAccept è la scelta dalla sola intestazione Accept.
func formatoAccept(r *http.Request) string {
	accept := r.Header.Values("Accept")
	if len(accept) == 0 || strings.Contains(accept[0], "*/*") {
		return formati[0].nome
	}
	type voce struct {
		q     float64
		testo string
		trova string
	}
	var resto []voce
	for _, testo := range strings.Split(accept[0], ",") {
		tipo, sotto, q, ok := tipoMedia(testo)
		if !ok {
			continue
		}
		trova := ""
		for _, f := range formati {
			if f.tipo == tipo && (sotto == "*" || sotto == f.sotto) {
				trova = f.nome
				break
			}
		}
		if q == 1 && trova != "" {
			return trova
		}
		resto = append(resto, voce{q, testo, trova})
	}
	slices.SortStableFunc(resto, func(a, b voce) int {
		switch {
		case a.q > b.q:
			return -1
		case a.q < b.q:
			return 1
		}
		return strings.Compare(a.testo, b.testo)
	})
	for _, v := range resto {
		if v.trova != "" {
			return v.trova
		}
	}
	return formati[0].nome
}

var reFloat = regexp.MustCompile(`^[+-]?[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]+)?`)

// tipoMedia legge «tipo/sotto;parametri» (in minuscolo, come Plug.Conn.Utils.media_type/1) e q come
// Phoenix (Float.parse: il prefisso numerico; illeggibile vale 1).
func tipoMedia(s string) (tipo, sotto string, q float64, ok bool) {
	parti := strings.Split(s, ";")
	ts := strings.SplitN(strings.ToLower(strings.TrimSpace(parti[0])), "/", 2)
	// Plug rifiuta anche i token non validi: qui non serve, perché un token non valido non
	// corrisponde comunque a text/html o application/json.
	if len(ts) != 2 {
		return "", "", 0, false
	}
	q = 1
	for _, p := range parti[1:] {
		// Come Plug.Conn.Utils.params: spazi tolti solo prima della chiave, virgolette tolte
		// dal valore; uno spazio dopo la chiave o all'inizio del valore rende q illeggibile.
		k, v, _ := strings.Cut(p, "=")
		if strings.ToLower(strings.TrimLeft(k, " \t")) != "q" {
			continue
		}
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			v = v[1 : len(v)-1]
		}
		if m := reFloat.FindString(v); m != "" {
			if f, err := strconv.ParseFloat(m, 64); err == nil {
				q = f
			}
		}
	}
	return ts[0], ts[1], q, true
}

// Pagina aggiunge le intestazioni di sicurezza delle pagine (put_secure_browser_headers dello
// standalone, misurate su /login): solo pagine e JSON pubblico, non il 404.
func Pagina(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		k := w.Header()
		k.Set("Content-Security-Policy", "base-uri 'self'; frame-ancestors 'self';")
		k.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		k.Set("X-Content-Type-Options", "nosniff")
		k.Set("X-Permitted-Cross-Domain-Policies", "none")
		h.ServeHTTP(w, r)
	})
}
