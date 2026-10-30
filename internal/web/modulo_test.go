package web

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// I test di questo file chiudono ACC-17 solo con le sessioni (slice 008): per ora niente segno.

var chiaveProva = []byte("chiave-di-prova-del-server-lunga-almeno-32-byte")

func csrfProva(t *testing.T, faccia string) *CSRF {
	t.Helper()
	c, err := NuovoCSRF(chiaveProva, faccia)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// paginaConToken dà un token CSRF e il cookie del segreto, come una pagina con un modulo.
func paginaConToken(t *testing.T, c *CSRF) (token string, cookie *http.Cookie) {
	t.Helper()
	rec := httptest.NewRecorder()
	token = c.Token(rec, httptest.NewRequest("GET", "/login", nil))
	cs := rec.Result().Cookies()
	if len(cs) != 1 {
		t.Fatalf("cookie impostati: %d", len(cs))
	}
	return token, cs[0]
}

// gestoreProva ricorda se è stato chiamato e il campo «nome» del modulo.
type gestoreProva struct {
	chiamato bool
	nome     string
}

func (g *gestoreProva) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.chiamato, g.nome = true, r.PostForm.Get("nome")
}

type invio struct {
	percorso, corpo, tipo, host string
	cookie                      *http.Cookie
	intestazioni                map[string]string
}

func (i invio) manda(t *testing.T, c *CSRF, g http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	p := i.percorso
	if p == "" {
		p = "/login"
	}
	req := httptest.NewRequest("POST", p, strings.NewReader(i.corpo))
	if i.tipo != "" {
		req.Header.Set("Content-Type", i.tipo)
	}
	if i.host != "" {
		req.Host = i.host
	}
	if i.cookie != nil {
		req.AddCookie(i.cookie)
	}
	for k, v := range i.intestazioni {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	c.Modulo(g).ServeHTTP(rec, req)
	return rec
}

const form = "application/x-www-form-urlencoded"

func conToken(tok, resto string) string { return "_csrf_token=" + url.QueryEscape(tok) + resto }

// Il cookie del segreto ha gli attributi di quello dello standalone (misurati: path=/; secure;
// HttpOnly; SameSite=Lax; senza scadenza), più il prefisso __Host-; un nome per faccia.
func TestCookieCSRF(t *testing.T) {
	c := csrfProva(t, "interna")
	t1, k := paginaConToken(t, c)
	if k.Name != "__Host-gestrip_csrf_interna" || k.Path != "/" || !k.Secure || !k.HttpOnly || k.SameSite != http.SameSiteLaxMode || k.MaxAge != 0 || !k.Expires.IsZero() || k.Domain != "" {
		t.Errorf("cookie = %+v", k)
	}
	if _, kp := paginaConToken(t, csrfProva(t, "pubblica")); kp.Name != "__Host-gestrip_csrf_pubblica" {
		t.Errorf("cookie della faccia pubblica: %s", kp.Name)
	}
	// Con il cookie già presente non se ne imposta un altro, e il token cambia (mascherato) ma
	// vale lo stesso.
	req := httptest.NewRequest("GET", "/login", nil)
	req.AddCookie(k)
	rec := httptest.NewRecorder()
	t2 := c.Token(rec, req)
	if len(rec.Result().Cookies()) != 0 {
		t.Error("cookie reimpostato anche se c'era")
	}
	if t1 == t2 {
		t.Error("due pagine hanno lo stesso token: non è mascherato")
	}
	for _, tok := range []string{t1, t2} {
		g := &gestoreProva{}
		if rec := (invio{corpo: conToken(tok, "&nome=Anna"), tipo: form, cookie: k}).manda(t, c, g); rec.Code != 200 || !g.chiamato || g.nome != "Anna" {
			t.Errorf("token valido rifiutato: %d", rec.Code)
		}
	}
}

// Due token nella stessa pagina (meta e campo nascosto) alla prima visita: un solo cookie, e
// tutti e due i token valgono.
func TestDueTokenNellaStessaPagina(t *testing.T) {
	c := csrfProva(t, "interna")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/login", nil)
	t1, t2 := c.Token(rec, req), c.Token(rec, req)
	cs := rec.Result().Cookies()
	if len(cs) != 1 {
		t.Fatalf("cookie impostati: %d", len(cs))
	}
	for _, tok := range []string{t1, t2} {
		if r := (invio{corpo: conToken(tok, ""), tipo: form, cookie: cs[0]}).manda(t, c, &gestoreProva{}); r.Code != 200 {
			t.Errorf("token della stessa pagina rifiutato: %d", r.Code)
		}
	}
}

// Un POST passa solo con il token giusto, nel corpo o nell'intestazione x-csrf-token, e dalla
// stessa origine; altrimenti 403 «Forbidden» come lo standalone, e il gestore non viene chiamato.
func TestModuloRifiuta(t *testing.T) {
	c := csrfProva(t, "interna")
	tok, k := paginaConToken(t, c)
	altro, _ := paginaConToken(t, c)
	_, kAltro := paginaConToken(t, c)
	tokPub, kPub := paginaConToken(t, csrfProva(t, "pubblica"))
	// Un cookie impostato da uno script: segreto scelto da chi attacca, senza la firma giusta.
	segAttacco := make([]byte, lunghezzaSegreto)
	falsi := []string{
		base64.RawURLEncoding.EncodeToString(segAttacco),
		base64.RawURLEncoding.EncodeToString(append(segAttacco, make([]byte, lunghezzaSegreto)...)),
		kPub.Value, // il cookie della faccia pubblica sotto il nome di quella interna
		"corto",
	}
	// La firma vera di un altro cookie con il segreto scelto da chi attacca.
	if b, err := base64.RawURLEncoding.DecodeString(k.Value); err == nil {
		falsi = append(falsi, base64.RawURLEncoding.EncodeToString(append(append([]byte{}, segAttacco...), b[lunghezzaSegreto:]...)))
	}
	casi := map[string]invio{
		"senza token":               {corpo: "nome=Anna", tipo: form, cookie: k},
		"token vuoto":               {corpo: "_csrf_token=&nome=Anna", tipo: form, cookie: k},
		"token falso":               {corpo: "_csrf_token=falso", tipo: form, cookie: k},
		"token di un altro segreto": {corpo: conToken(altro, ""), tipo: form, cookie: k},
		"cookie di un altro":        {corpo: conToken(tok, ""), tipo: form, cookie: kAltro},
		"senza cookie":              {corpo: conToken(tok, ""), tipo: form},
		"token troncato":            {corpo: conToken(tok[:len(tok)-2], ""), tipo: form, cookie: k},
		"intestazione falsa":        {corpo: "nome=Anna", tipo: form, cookie: k, intestazioni: map[string]string{"X-Csrf-Token": "falso"}},
		"JSON senza token":          {corpo: `{"nome":"Anna"}`, tipo: "application/json", cookie: k},
		"multipart senza token":     {corpo: "--XX--\r\n", tipo: "multipart/form-data; boundary=XX", cookie: k},
		"senza tipo":                {corpo: conToken(tok, ""), cookie: k},
		"token nella query":         {percorso: "/login?" + conToken(tok, ""), corpo: "nome=Anna", tipo: form, cookie: k},
		"token e cookie pubblici":   {corpo: conToken(tokPub, ""), tipo: form, cookie: &http.Cookie{Name: "__Host-gestrip_csrf_interna", Value: kPub.Value}},
		"da un altro sito":          {corpo: conToken(tok, ""), tipo: form, cookie: k, intestazioni: map[string]string{"Sec-Fetch-Site": "cross-site"}},
		"dalla faccia pubblica 443": {corpo: conToken(tok, ""), tipo: form, cookie: k, host: "negozio.esempio.ts.net:8443", intestazioni: map[string]string{"Origin": "https://negozio.esempio.ts.net"}},
	}
	for i, v := range falsi {
		seg := make([]byte, lunghezzaSegreto)
		casi["cookie impostato da uno script "+string(rune('a'+i))] = invio{corpo: conToken(maschera(seg), ""), tipo: form, cookie: &http.Cookie{Name: k.Name, Value: v}}
	}
	for nome, i := range casi {
		g := &gestoreProva{}
		rec := i.manda(t, c, g)
		if rec.Code != http.StatusForbidden || rec.Body.String() != "Forbidden" || g.chiamato {
			t.Errorf("%s: %d %q, gestore chiamato %v", nome, rec.Code, rec.Body.String(), g.chiamato)
		}
	}
	// Basta uno dei due valido, come Plug; l'intestazione basta anche senza modulo (fetch).
	for nome, i := range map[string]invio{
		"corpo giusto, intestazione falsa": {corpo: conToken(tok, ""), tipo: form, cookie: k, intestazioni: map[string]string{"X-Csrf-Token": "falso"}},
		"corpo falso, intestazione giusta": {corpo: "_csrf_token=falso", tipo: form, cookie: k, intestazioni: map[string]string{"X-Csrf-Token": tok}},
		"JSON con intestazione":            {corpo: `{"x":1}`, tipo: "application/json", cookie: k, intestazioni: map[string]string{"X-Csrf-Token": tok}},
		"stessa origine":                   {corpo: conToken(tok, ""), tipo: form, cookie: k, intestazioni: map[string]string{"Sec-Fetch-Site": "same-origin"}},
		"stessa origine, solo Origin":      {corpo: conToken(tok, ""), tipo: form, cookie: k, host: "negozio.esempio.ts.net:8443", intestazioni: map[string]string{"Origin": "https://negozio.esempio.ts.net:8443"}},
		// Un modulo senza action si invia all'indirizzo corrente: la query non conta.
		"query con ; e %": {percorso: "/login?a=1;b=2&c=%zz&pw=100%", corpo: conToken(tok, ""), tipo: form, cookie: k},
	} {
		g := &gestoreProva{}
		if rec := i.manda(t, c, g); rec.Code != 200 || !g.chiamato {
			t.Errorf("%s: %d", nome, rec.Code)
		}
	}
	// Un cookie non valido fa creare un segreto nuovo alla pagina successiva.
	req := httptest.NewRequest("GET", "/login", nil)
	req.AddCookie(&http.Cookie{Name: k.Name, Value: falsi[0]})
	rec := httptest.NewRecorder()
	c.Token(rec, req)
	if len(rec.Result().Cookies()) != 1 {
		t.Error("cookie non firmato accettato dalla pagina")
	}
}

// Un corpo di modulo che non si legge: 400; troppo grande: 413 (Plug.Parsers, 1 000 000 di byte,
// misurato). Si controlla prima del token, come Plug (i parser girano prima del router).
func TestModuloCorpo(t *testing.T) {
	c := csrfProva(t, "interna")
	tok, k := paginaConToken(t, c)
	t0 := conToken(tok, "&")
	casi := []struct {
		nome, corpo string
		codice      int
		testo       string
	}{
		{"UTF-8 non valido", t0 + "nome=%FF", 400, "Bad Request"},
		{"chiave non valida", t0 + "%FF=1", 400, "Bad Request"},
		{"percento rotto", t0 + "nome=%zz", 400, "Bad Request"}, // Plug darebbe 403: D-30, un browser codifica sempre
		{"troppo grande", t0 + "nome=" + strings.Repeat("a", LimiteModulo), 413, "Request Entity Too Large"},
		{"un byte oltre il limite", "nome=" + strings.Repeat("a", LimiteModulo+1-len("nome=")), 413, "Request Entity Too Large"},
		{"al limite", t0 + "nome=" + strings.Repeat("a", LimiteModulo-len(t0)-len("nome=")), 200, ""},
	}
	for _, k2 := range casi {
		g := &gestoreProva{}
		rec := (invio{corpo: k2.corpo, tipo: form, cookie: k}).manda(t, c, g)
		if rec.Code != k2.codice || (k2.testo != "" && rec.Body.String() != k2.testo) || g.chiamato != (k2.codice == 200) {
			t.Errorf("%s: %d %q, gestore chiamato %v", k2.nome, rec.Code, rec.Body.String(), g.chiamato)
		}
	}
}

// Attraverso la faccia, le intestazioni degli errori sono quelle misurate sullo standalone: il
// 403 ha HSTS e x-request-id, il 400 e il 413 no.
func TestModuloAttraversoLaFaccia(t *testing.T) {
	c := csrfProva(t, "interna")
	tok, k := paginaConToken(t, c)
	mux := http.NewServeMux()
	mux.Handle("POST /modulo", c.Modulo(&gestoreProva{}))
	f := faccia(mux, nessunRegistro)
	for _, caso := range []struct {
		corpo  string
		codice int
		hsts   bool
	}{
		{"nome=Anna", 403, true},
		{conToken(tok, "&nome=%FF"), 400, false},
		{"nome=" + strings.Repeat("a", LimiteModulo+1), 413, false},
		{conToken(tok, ""), 200, true},
	} {
		req := httptest.NewRequest("POST", "/modulo", strings.NewReader(caso.corpo))
		req.Host = "negozio.esempio.ts.net"
		req.Header.Set("Content-Type", form)
		req.AddCookie(k)
		rec := httptest.NewRecorder()
		f.ServeHTTP(rec, req)
		h := rec.Header()
		if rec.Code != caso.codice || (h.Get("Strict-Transport-Security") != "") != caso.hsts || (h.Get("X-Request-Id") != "") != caso.hsts {
			t.Errorf("%d: codice %d, hsts %q, id %q", caso.codice, rec.Code, h.Get("Strict-Transport-Security"), h.Get("X-Request-Id"))
		}
		if h.Get("Content-Security-Policy") != "" {
			t.Errorf("%d: intestazioni delle pagine sull'errore", caso.codice)
		}
	}
}

func TestNuovoCSRF(t *testing.T) {
	if _, err := NuovoCSRF([]byte("corta"), "interna"); err == nil {
		t.Error("chiave corta accettata")
	}
	if _, err := NuovoCSRF(chiaveProva, "altra"); err == nil {
		t.Error("faccia sconosciuta accettata")
	}
}

// Il limite è quello di Plug.Parsers per i moduli (misurato): fissato qui perché gli altri test
// lo usano come costante.
func TestLimiteComePlug(t *testing.T) {
	if LimiteModulo != 1_000_000 {
		t.Errorf("LimiteModulo = %d", LimiteModulo)
	}
}

// Il token si legge e si scrive solo con i byte giusti.
func TestTokenMascherato(t *testing.T) {
	seg := make([]byte, lunghezzaSegreto)
	for i := range seg {
		seg[i] = byte(i)
	}
	tok := maschera(seg)
	if got, ok := smaschera(tok); !ok || string(got) != string(seg) {
		t.Errorf("smaschera(maschera) = %v %v", got, ok)
	}
	for _, rotto := range []string{"", "a", tok + "a", tok[1:], strings.Repeat("!", len(tok))} {
		if _, ok := smaschera(rotto); ok {
			t.Errorf("smaschera(%q) accettato", rotto)
		}
	}
}

type letturaRotta struct{ fatto bool }

func (l *letturaRotta) Read(p []byte) (int, error) {
	if !l.fatto {
		l.fatto = true
		return copy(p, "_csrf_token="), nil
	}
	return 0, errors.New("connessione caduta")
}

// Un corpo che si interrompe a metà è un 400, mai un modulo letto a pezzi.
func TestModuloCorpoInterrotto(t *testing.T) {
	c := csrfProva(t, "interna")
	_, k := paginaConToken(t, c)
	req := httptest.NewRequest("POST", "/login", &letturaRotta{})
	req.Header.Set("Content-Type", form)
	req.AddCookie(k)
	rec := httptest.NewRecorder()
	g := &gestoreProva{}
	c.Modulo(g).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || g.chiamato {
		t.Errorf("corpo interrotto: %d, gestore chiamato %v", rec.Code, g.chiamato)
	}
}
