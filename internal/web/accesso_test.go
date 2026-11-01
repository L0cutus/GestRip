package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// utentiFinti: un account con password nota, titolare se ruolo è vuoto; attivo, versione e
// ruolo si cambiano a mano (il ruolo si rilegge a ogni richiesta, come DiSessione).
type utentiFinti struct {
	mu       sync.Mutex
	attivo   bool
	versione int64
	ruolo    string
	accessi  int
	// autentiche conta le verifiche della password: un login bloccato non deve arrivarci.
	autentiche int
	// ritardo rallenta la verifica come PBKDF2, per i tentativi simultanei.
	ritardo time.Duration
}

func (u *utentiFinti) ruoloAttuale() string {
	if u.ruolo == "" {
		return accounts.Titolare
	}
	return u.ruolo
}

func (u *utentiFinti) cambiaRuolo(r string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.ruolo = r
}

const (
	emailProva    = "titolare@esempio.it"
	passwordProva = "password-sintetica-di-prova"
)

func (u *utentiFinti) Autentica(_ context.Context, email, password string) (accounts.Utente, bool, error) {
	u.mu.Lock()
	u.autentiche++
	r := u.ritardo
	u.mu.Unlock()
	time.Sleep(r)
	u.mu.Lock()
	defer u.mu.Unlock()
	if accounts.NormalizzaEmail(email) == emailProva && password == passwordProva && u.attivo {
		return accounts.Utente{ID: 1, Ruolo: u.ruoloAttuale(), Versione: u.versione}, true, nil
	}
	return accounts.Utente{}, false, nil
}

func (u *utentiFinti) DiSessione(_ context.Context, id, versione int64) (accounts.Utente, bool, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if id == 1 && u.attivo && versione == u.versione {
		return accounts.Utente{ID: 1, Ruolo: u.ruoloAttuale(), Versione: versione}, true, nil
	}
	return accounts.Utente{}, false, nil
}

func (u *utentiFinti) ElencoUtenti(context.Context) ([]accounts.VoceUtente, error) { return nil, nil }

func (u *utentiFinti) Disattiva(context.Context, int64, audit.Contesto) error {
	return errors.New("non nei test finti")
}

func (u *utentiFinti) CambiaRuolo(context.Context, int64, string, audit.Contesto) error {
	return errors.New("non nei test finti")
}

// recuperoFinto: nessun link vale (le pagine del reset si provano su account veri).
type recuperoFinto struct{}

func (recuperoFinto) RichiestaDallaPagina(context.Context, string) (string, error) {
	return "no_channel", nil
}
func (recuperoFinto) AccountDel(context.Context, string) (string, bool, error) { return "", false, nil }
func (recuperoFinto) Reimposta(context.Context, string, string, string, string) error {
	return accounts.ErrLinkNonValido
}

func (u *utentiFinti) RegistraAccesso(context.Context, int64) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.accessi++
	return nil
}

func dipendenzeProva(s Sonda, l *slog.Logger, u Utenti) Dipendenze {
	o := clock.NuovoFinto(time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC))
	return Dipendenze{Sonda: s, Registro: l, Utenti: u, Orologio: o, Limiti: ratelimit.Nuovo(o), Audit: &registroFinto{}, Recupero: recuperoFinto{}, Inviti: invitiFinti{}, PrimoTitolare: primoFinto{}, Consultazione: consultazioneFinta{}, Buste: busteFinte{},
		ChiaveSessione: []byte("sottochiave-sessione-di-prova-lunga-32-byte"),
		ChiaveCSRF:     []byte("sottochiave-csrf-di-prova-lunga-almeno-32-b"),
		ChiaveGalleria: []byte("sottochiave-galleria-di-prova-lunga-32-byte")}
}

func internaProva(s Sonda, l *slog.Logger) http.Handler {
	h, err := Interna(dipendenzeProva(s, l, &utentiFinti{attivo: true}))
	if err != nil {
		panic(err)
	}
	return h
}

func pubblicaProva(s Sonda, l *slog.Logger) http.Handler {
	h, err := Pubblica(dipendenzeProva(s, l, &utentiFinti{attivo: true}))
	if err != nil {
		panic(err)
	}
	return h
}

// browser è un client con cookie verso una faccia vera (TLS, perché i cookie sono Secure).
type browser struct {
	t     *testing.T
	srv   *httptest.Server
	c     *http.Client
	token string
	ip    string // se c'è, l'indirizzo del cliente in X-Forwarded-For (il server di prova è su 127.0.0.1)
}

var reToken = regexp.MustCompile(`name="csrf-token" content="([^"]+)"`)

func nuovoBrowser(t *testing.T, h http.Handler) *browser {
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	c := srv.Client()
	c.Jar = jar
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &browser{t: t, srv: srv, c: c}
}

type risposta struct {
	codice   int
	location string
	corpo    string
	h        http.Header
}

func (b *browser) vai(metodo, percorso string, form url.Values, h map[string]string) risposta {
	b.t.Helper()
	if b.ip != "" {
		if h == nil {
			h = map[string]string{}
		}
		if _, c := h["X-Forwarded-For"]; !c {
			h["X-Forwarded-For"] = b.ip
		}
	}
	return b.vaiSenzaIP(metodo, percorso, form, h)
}

func (b *browser) vaiSenzaIP(metodo, percorso string, form url.Values, h map[string]string) risposta {
	b.t.Helper()
	var corpo *strings.Reader
	if form != nil {
		corpo = strings.NewReader(form.Encode())
	} else {
		corpo = strings.NewReader("")
	}
	req, _ := http.NewRequest(metodo, b.srv.URL+percorso, corpo)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range h {
		req.Header.Set(k, v)
	}
	r, err := b.c.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if m := reToken.FindStringSubmatch(sb.String()); m != nil {
		b.token = m[1]
	}
	return risposta{r.StatusCode, r.Header.Get("Location"), sb.String(), r.Header}
}

func (b *browser) entra(email, password string) risposta {
	b.t.Helper()
	b.vai("GET", "/login", nil, nil)
	return b.vai("POST", "/login", url.Values{"_csrf_token": {b.token}, "user[email]": {email}, "user[password]": {password}}, nil)
}

const corpoRedirect = `<html><body>You are being <a href="%s">redirected</a>.</body></html>`

func redirectA(r risposta, dove string) bool {
	// Nel corpo l'indirizzo è escapato come Plug.HTML.html_escape: qui basta la «&».
	return r.codice == http.StatusFound && r.location == dove && r.corpo == strings.Replace(corpoRedirect, "%s", strings.ReplaceAll(dove, "&", "&amp;"), 1)
}

// inventario: ACC-01
// La pagina di accesso ha i campi, i testi e le intestazioni dello standalone (misurati il
// 30/10/2026), il token CSRF nel meta e nel modulo; solo sulla faccia interna (D-28).
func TestPaginaLogin(t *testing.T) {
	b := nuovoBrowser(t, internaProva(&sondaFinta{pronto: true}, nessunRegistro))
	r := b.vai("GET", "/login", nil, nil)
	if r.codice != 200 || r.h.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("GET /login = %d %q", r.codice, r.h.Get("Content-Type"))
	}
	for _, s := range []string{"Accesso laboratorio", "Area riservata al personale autorizzato.", ">Accedi<", "Password dimenticata?",
		`action="/login" method="post"`, `name="_csrf_token"`, `name="user[email]"`, `type="email"`, `autocomplete="username"`,
		`name="user[password]"`, `type="password"`, `autocomplete="current-password"`, `href="/reset-request"`, `name="csrf-token"`, `lang="it"`} {
		if !strings.Contains(r.corpo, s) {
			t.Errorf("la pagina non contiene %q", s)
		}
	}
	for k, v := range map[string]string{"Content-Security-Policy": "base-uri 'self'; frame-ancestors 'self';", "Referrer-Policy": "strict-origin-when-cross-origin", "X-Content-Type-Options": "nosniff"} {
		if r.h.Get(k) != v {
			t.Errorf("%s = %q", k, r.h.Get(k))
		}
	}
	p := nuovoBrowser(t, pubblicaProva(&sondaFinta{pronto: true}, nessunRegistro))
	if r := p.vai("GET", "/login", nil, nil); r.codice != 404 {
		t.Errorf("/login sulla faccia pubblica = %d, atteso 404 (D-28)", r.codice)
	}
}

// inventario: ACC-01
// Email o password sbagliate, account sconosciuto, modulo malformato: lo stesso 422 con «Email o
// password non validi.»; senza token CSRF 403. Il messaggio non resta per la pagina dopo.
func TestLoginSbagliato(t *testing.T) {
	u := &utentiFinti{attivo: true}
	h, _ := Interna(dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, u))
	b := nuovoBrowser(t, h)
	b.vai("GET", "/login", nil, nil)
	for nome, f := range map[string]url.Values{
		"password sbagliata": {"user[email]": {emailProva}, "user[password]": {"sbagliata"}},
		"email sconosciuta":  {"user[email]": {"nessuno@esempio.it"}, "user[password]": {passwordProva}},
		"senza password":     {"user[email]": {emailProva}},
		"senza email":        {"user[password]": {passwordProva}},
		"campi sbagliati":    {"email": {emailProva}, "password": {passwordProva}},
	} {
		f.Set("_csrf_token", b.token)
		r := b.vai("POST", "/login", f, nil)
		if r.codice != 422 || !strings.Contains(r.corpo, "Email o password non validi.") || !strings.Contains(r.corpo, "Accesso laboratorio") {
			t.Errorf("%s: %d", nome, r.codice)
		}
	}
	if r := b.vai("GET", "/login", nil, nil); strings.Contains(r.corpo, "Email o password non validi.") {
		t.Error("il messaggio d'errore è rimasto per la pagina dopo")
	}
	r := b.vai("POST", "/login", url.Values{"user[email]": {emailProva}, "user[password]": {passwordProva}}, nil)
	if r.codice != 403 || r.corpo != "Forbidden" {
		t.Errorf("senza token: %d %q", r.codice, r.corpo)
	}
	if u.accessi != 0 {
		t.Errorf("accessi registrati: %d", u.accessi)
	}
	u.attivo = false
	if r := b.entra(emailProva, passwordProva); r.codice != 422 {
		t.Errorf("account disattivato: %d", r.codice)
	}
}

// inventario: ACC-01, ACC-02
// Senza sessione una pagina interna porta a /login con «Devi accedere per continuare.» (una
// volta sola); dopo la password si torna alla pagina chiesta, o a /. Sessione e segreto CSRF
// sono nuovi dopo il login; l'accesso è registrato; da dentro /login porta a /.
func TestLoginGiusto(t *testing.T) {
	u := &utentiFinti{attivo: true}
	h, _ := Interna(dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, u))
	b := nuovoBrowser(t, h)
	if r := b.vai("GET", "/?cerca=Anna&x=1", nil, map[string]string{"Sec-Fetch-Dest": "document"}); !redirectA(r, "/login") {
		t.Fatalf("home senza sessione: %d %q %q", r.codice, r.location, r.corpo)
	}
	r := b.vai("GET", "/login", nil, nil)
	if !strings.Contains(r.corpo, "Devi accedere per continuare.") {
		t.Error("manca «Devi accedere per continuare.»")
	}
	if r := b.vai("GET", "/login", nil, nil); strings.Contains(r.corpo, "Devi accedere per continuare.") {
		t.Error("il messaggio resta alla seconda pagina")
	}
	csrfPrima := cookie(b, "__Host-gestrip_csrf_interna")
	sessPrima := cookie(b, "__Host-gestrip_sessione_interna")
	r = b.vai("POST", "/login", url.Values{"_csrf_token": {b.token}, "user[email]": {"  " + strings.ToUpper(emailProva) + " "}, "user[password]": {passwordProva}}, nil)
	if !redirectA(r, "/?cerca=Anna&x=1") {
		t.Fatalf("login: %d %q", r.codice, r.location)
	}
	if u.accessi != 1 {
		t.Errorf("accessi registrati: %d", u.accessi)
	}
	if c := cookie(b, "__Host-gestrip_csrf_interna"); c == "" || c == csrfPrima {
		t.Error("il segreto CSRF non è stato rinnovato al login")
	}
	if c := cookie(b, "__Host-gestrip_sessione_interna"); c == "" || c == sessPrima {
		t.Error("la sessione non è nuova dopo il login")
	}
	sc := setCookie(r.h, "__Host-gestrip_sessione_interna")
	if sc == nil || !sc.Secure || !sc.HttpOnly || sc.Path != "/" || sc.SameSite != http.SameSiteLaxMode || sc.MaxAge != 0 {
		t.Errorf("cookie di sessione: %+v", sc)
	}
	if r := b.vai("GET", "/", nil, nil); r.codice != 200 || !strings.Contains(r.corpo, "Esci") {
		t.Errorf("home da dentro: %d", r.codice)
	}
	if r := b.vai("GET", "/login", nil, nil); !redirectA(r, "/") {
		t.Errorf("/login da dentro: %d %q", r.codice, r.location)
	}
	// Un secondo login senza pagina chiesta porta a /.
	b2 := nuovoBrowser(t, h)
	if r := b2.entra(emailProva, passwordProva); !redirectA(r, "/") {
		t.Errorf("login senza pagina chiesta: %d %q", r.codice, r.location)
	}
}

// inventario: ACC-02
// Si ricorda solo una pagina letta (GET) e navigata (Sec-Fetch-Dest assente o document); una
// pagina non valida cancella quella ricordata prima.
func TestRitornoSoloNavigazioni(t *testing.T) {
	h := internaProva(&sondaFinta{pronto: true}, nessunRegistro)
	casi := []struct {
		passi  []map[string]string // percorso e Sec-Fetch-Dest
		atteso string
	}{
		{[]map[string]string{{"p": "/?a=1", "d": "image"}}, "/"},
		{[]map[string]string{{"p": "/?a=1"}}, "/?a=1"},
		{[]map[string]string{{"p": "/?a=1", "d": "document"}, {"p": "/?b=2", "d": "iframe"}}, "/?a=1"},
	}
	for i, c := range casi {
		b := nuovoBrowser(t, h)
		for _, p := range c.passi {
			hh := map[string]string{}
			if p["d"] != "" {
				hh["Sec-Fetch-Dest"] = p["d"]
			}
			b.vai("GET", p["p"], nil, hh)
		}
		if r := b.entra(emailProva, passwordProva); r.location != c.atteso {
			t.Errorf("caso %d: torna a %q, atteso %q", i, r.location, c.atteso)
		}
	}
}

// inventario: ACC-02
// Anti open-redirect: le regole di UserAuth.safe_return_to/1 dello standalone.
func TestRitornoSicuro(t *testing.T) {
	for p, ok := range map[string]bool{
		"/":                             true,
		"/repairs/7?tab=foto":           true,
		"/a/b-c._~!$&'()*+,;=:@%/?#":    true,
		"//evil.example":                false,
		"/\\evil":                       false,
		"https://evil":                  false,
		"javascript:alert(1)":           false,
		"relativo":                      false,
		"/a\\b":                         false,
		"/a b":                          false,
		"/a\tb":                         false,
		"/a%09b":                        false,
		"/a%0Ab":                        false,
		"/a%1fb":                        false,
		"/a%7Fb":                        false,
		"/a%5cb":                        false,
		"/a%20b":                        true,
		"/è":                            false,
		"/" + strings.Repeat("a", 1999): true,
		"/" + strings.Repeat("a", 2000): false,
		"":                              false,
	} {
		if got := ritornoSicuro(p); got != ok {
			t.Errorf("ritornoSicuro(%q) = %v, atteso %v", p, got, ok)
		}
	}
}

// inventario: ACC-03
// L'uscita (POST con token) cancella la sessione e rinnova il segreto CSRF, poi porta a /login;
// senza token 403. Dopo, la home non si apre.
func TestLogout(t *testing.T) {
	b := nuovoBrowser(t, internaProva(&sondaFinta{pronto: true}, nessunRegistro))
	b.entra(emailProva, passwordProva)
	b.vai("GET", "/", nil, nil)
	if r := b.vai("POST", "/logout", url.Values{"_method": {"delete"}}, nil); r.codice != 403 {
		t.Errorf("logout senza token: %d", r.codice)
	}
	csrfPrima := cookie(b, "__Host-gestrip_csrf_interna")
	r := b.vai("POST", "/logout", url.Values{"_method": {"delete"}, "_csrf_token": {b.token}}, nil)
	if !redirectA(r, "/login") {
		t.Fatalf("logout: %d %q", r.codice, r.location)
	}
	if sc := setCookie(r.h, "__Host-gestrip_sessione_interna"); sc == nil || sc.MaxAge >= 0 {
		t.Errorf("il cookie di sessione non è cancellato: %+v", sc)
	}
	if c := cookie(b, "__Host-gestrip_csrf_interna"); c == csrfPrima {
		t.Error("il segreto CSRF non è stato rinnovato all'uscita")
	}
	if r := b.vai("GET", "/", nil, nil); !redirectA(r, "/login") {
		t.Errorf("home dopo l'uscita: %d", r.codice)
	}
}

// inventario: ACC-04, ACC-17
// A ogni richiesta la sessione si ricontrolla: account disattivato o «chiudi tutte» (versione
// cambiata) → fuori al primo clic; un cookie manomesso o dell'altra faccia non vale.
func TestSessioneRicontrollata(t *testing.T) {
	u := &utentiFinti{attivo: true}
	h, _ := Interna(dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, u))
	b := nuovoBrowser(t, h)
	b.entra(emailProva, passwordProva)
	if r := b.vai("GET", "/", nil, nil); r.codice != 200 {
		t.Fatalf("home: %d", r.codice)
	}
	u.versione = 1
	if r := b.vai("GET", "/", nil, nil); !redirectA(r, "/login") {
		t.Errorf("dopo «chiudi tutte»: %d", r.codice)
	}
	u.versione = 0
	b.entra(emailProva, passwordProva)
	u.attivo = false
	if r := b.vai("GET", "/", nil, nil); !redirectA(r, "/login") {
		t.Errorf("account disattivato: %d", r.codice)
	}
	u.attivo = true
	b.entra(emailProva, passwordProva)
	v := cookie(b, "__Host-gestrip_sessione_interna")
	manomesso := []byte(v)
	manomesso[3] ^= 1
	imposta(b, "__Host-gestrip_sessione_interna", string(manomesso))
	if r := b.vai("GET", "/", nil, nil); !redirectA(r, "/login") {
		t.Errorf("cookie manomesso: %d", r.codice)
	}
	// Il cookie della faccia interna messo sotto il nome della pubblica (e viceversa) non vale:
	// la firma include la faccia.
	s := sessioniPer(dipendenzeProva(nil, nil, nil).ChiaveSessione, "pubblica")
	rec := httptest.NewRecorder()
	// Accesso valido (D-31): l'unica ragione del rifiuto è la faccia.
	s.scrivi(rec, datiSessione{Utente: 1, Accesso: dipendenzeProva(nil, nil, nil).Orologio.Adesso().Unix()})
	imposta(b, "__Host-gestrip_sessione_interna", rec.Result().Cookies()[0].Value)
	if r := b.vai("GET", "/", nil, nil); !redirectA(r, "/login") {
		t.Errorf("cookie della faccia pubblica accettato dall'interna: %d", r.codice)
	}
}

func cookie(b *browser, nome string) string {
	u, _ := url.Parse(b.srv.URL)
	for _, c := range b.c.Jar.Cookies(u) {
		if c.Name == nome {
			return c.Value
		}
	}
	return ""
}

func imposta(b *browser, nome, valore string) {
	u, _ := url.Parse(b.srv.URL)
	b.c.Jar.SetCookies(u, []*http.Cookie{{Name: nome, Value: valore, Path: "/", Secure: true}})
}

func setCookie(h http.Header, nome string) *http.Cookie {
	for _, v := range h.Values("Set-Cookie") {
		if c, err := http.ParseSetCookie(v); err == nil && c.Name == nome {
			return c
		}
	}
	return nil
}

// Il corpo del redirect escapa come Plug.HTML.html_escape (con &quot; e &#39;, non &#34;).
func TestEscapePlug(t *testing.T) {
	if got := escapePlug(`/a?b=1&c='x'"<>`); got != "/a?b=1&amp;c=&#39;x&#39;&quot;&lt;&gt;" {
		t.Errorf("escapePlug = %q", got)
	}
}

// inventario: ACC-01, ACC-02
// Al login la sessione riparte da capo: la pagina ricordata si usa una volta e non resta; un
// errore di questa risposta prevale su quello lasciato in sessione da un redirect.
func TestLoginRinnovaLaSessione(t *testing.T) {
	u := &utentiFinti{attivo: true}
	h, _ := Interna(dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, u))
	b := nuovoBrowser(t, h)
	b.vai("GET", "/login", nil, nil)
	b.vai("GET", "/?pagina=1", nil, nil) // ricorda /?pagina=1 e lascia «Devi accedere…»
	r := b.vai("POST", "/login", url.Values{"_csrf_token": {b.token}, "user[email]": {emailProva}, "user[password]": {"sbagliata"}}, nil)
	if !strings.Contains(r.corpo, "Email o password non validi.") || strings.Contains(r.corpo, "Devi accedere per continuare.") {
		t.Error("il 422 non mostra il suo messaggio al posto di quello in sessione")
	}
	if r := b.entra(emailProva, passwordProva); r.location != "/?pagina=1" {
		t.Fatalf("primo login: %q", r.location)
	}
	u.versione = 1 // «chiudi tutte»
	b.vai("GET", "/login", nil, nil)
	if r := b.vai("POST", "/login", url.Values{"_csrf_token": {b.token}, "user[email]": {emailProva}, "user[password]": {passwordProva}}, nil); r.location != "/" {
		t.Errorf("la pagina ricordata è rimasta dopo il login: %q", r.location)
	}
}

// inventario: ACC-02
// Una pagina non valida cancella quella ricordata prima; un ritorno pericoloso anche dentro un
// cookie firmato non si segue (doppio controllo, come lo standalone).
func TestRitornoCancellatoERicontrollato(t *testing.T) {
	h := internaProva(&sondaFinta{pronto: true}, nessunRegistro)
	b := nuovoBrowser(t, h)
	b.vai("GET", "/?a=1", nil, nil)
	b.vai("GET", "/?b=%01", nil, nil)
	if r := b.entra(emailProva, passwordProva); r.location != "/" {
		t.Errorf("pagina non valida: torna a %q", r.location)
	}
	b2 := nuovoBrowser(t, h)
	b2.vai("GET", "/login", nil, nil)
	s := sessioniPer(dipendenzeProva(nil, nil, nil).ChiaveSessione, "interna")
	rec := httptest.NewRecorder()
	s.scrivi(rec, datiSessione{Ritorno: "//evil.example"})
	imposta(b2, "__Host-gestrip_sessione_interna", rec.Result().Cookies()[0].Value)
	if r := b2.vai("POST", "/login", url.Values{"_csrf_token": {b2.token}, "user[email]": {emailProva}, "user[password]": {passwordProva}}, nil); r.location != "/" {
		t.Errorf("ritorno pericoloso seguito: %q", r.location)
	}
}

// inventario: ACC-02
// Si ricorda solo una lettura (GET): un POST senza accesso va a /login senza ricordare nulla.
func TestRitornoSoloGET(t *testing.T) {
	f, err := nuovaFaccia(dipendenzeProva(nil, nil, &utentiFinti{attivo: true}), "interna")
	if err != nil {
		t.Fatal(err)
	}
	h := f.conSessione(f.richiedeAccesso(http.NotFoundHandler()))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/qualcosa?x=1", nil))
	if rec.Code != 302 {
		t.Fatalf("POST senza accesso: %d", rec.Code)
	}
	req := httptest.NewRequest("GET", "/login", nil)
	req.AddCookie(rec.Result().Cookies()[0])
	if d := f.sessioni.leggi(req); d.Ritorno != "" || d.Errore != msgAccedi {
		t.Errorf("sessione dopo un POST: %+v", d)
	}
}

// inventario: ACC-03, ACC-04
// Uscire con una sessione già scaduta cancella il cookie e basta: nessun cookie riscritto.
// Un account riattivato non ritrova la sessione vecchia: il cookie è stato ripulito.
func TestUscitaESessioneRipulita(t *testing.T) {
	u := &utentiFinti{attivo: true}
	h, _ := Interna(dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, u))
	b := nuovoBrowser(t, h)
	b.entra(emailProva, passwordProva)
	b.vai("GET", "/", nil, nil)
	u.versione = 1
	r := b.vai("POST", "/logout", url.Values{"_csrf_token": {b.token}}, nil)
	n := 0
	for _, v := range r.h.Values("Set-Cookie") {
		if c, err := http.ParseSetCookie(v); err == nil && c.Name == "__Host-gestrip_sessione_interna" {
			n++
			if c.MaxAge >= 0 {
				t.Errorf("cookie di sessione riscritto all'uscita: %+v", c)
			}
		}
	}
	if n != 1 {
		t.Errorf("Set-Cookie della sessione all'uscita: %d", n)
	}
	u.versione = 0
	b.entra(emailProva, passwordProva)
	u.attivo = false
	b.vai("GET", "/", nil, nil) // fuori: il cookie perde l'account
	u.attivo = true
	if r := b.vai("GET", "/", nil, nil); r.codice != 302 {
		t.Errorf("riattivato: la sessione vecchia vale di nuovo (%d)", r.codice)
	}
}

// inventario: ACC-01
// Due schede su /login: si entra dalla prima, poi «Accedi» nella seconda (token di prima del
// rinnovo). Come lo standalone (misurato dalla review): si va a /, non 403. Chi non ha una
// sessione valida passa sempre dal controllo CSRF.
func TestLoginDaUnaSecondaScheda(t *testing.T) {
	b := nuovoBrowser(t, internaProva(&sondaFinta{pronto: true}, nessunRegistro))
	b.vai("GET", "/login", nil, nil)
	vecchio := b.token
	b.vai("POST", "/login", url.Values{"_csrf_token": {vecchio}, "user[email]": {emailProva}, "user[password]": {passwordProva}}, nil)
	r := b.vai("POST", "/login", url.Values{"_csrf_token": {vecchio}, "user[email]": {emailProva}, "user[password]": {passwordProva}}, nil)
	if !redirectA(r, "/") || r.h.Get("Content-Security-Policy") == "" {
		t.Errorf("seconda scheda: %d %q", r.codice, r.location)
	}
	fuori := nuovoBrowser(t, internaProva(&sondaFinta{pronto: true}, nessunRegistro))
	if r := fuori.vai("POST", "/login", url.Values{"_csrf_token": {vecchio}, "user[email]": {emailProva}, "user[password]": {passwordProva}}, nil); r.codice != 403 {
		t.Errorf("senza sessione, token di un altro browser: %d", r.codice)
	}
}

// Ogni accesso dà un cookie di sessione diverso, anche per lo stesso account.
func TestSessioneDiversaAOgniAccesso(t *testing.T) {
	h := internaProva(&sondaFinta{pronto: true}, nessunRegistro)
	a, b := nuovoBrowser(t, h), nuovoBrowser(t, h)
	a.entra(emailProva, passwordProva)
	b.entra(emailProva, passwordProva)
	if cookie(a, "__Host-gestrip_sessione_interna") == cookie(b, "__Host-gestrip_sessione_interna") {
		t.Error("due accessi con lo stesso cookie di sessione")
	}
}

// Il cookie resta piccolo anche con un ritorno pieno di «&» (json non le deve escapare: il
// browser scarterebbe un cookie oltre 4096 byte).
func TestCookieSessioneCompatto(t *testing.T) {
	s := sessioniPer(dipendenzeProva(nil, nil, nil).ChiaveSessione, "interna")
	rec := httptest.NewRecorder()
	s.scrivi(rec, datiSessione{Ritorno: "/?" + strings.Repeat("&", 1500)})
	if n := len(rec.Result().Cookies()[0].Value); n > 2200 {
		t.Errorf("cookie di %d byte", n)
	}
}
