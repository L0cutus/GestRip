package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"unicode/utf8"
)

// I moduli del Go (decisione del 30/10/2026, PIANO §8.4): solo POST, solo
// application/x-www-form-urlencoded, token CSRF come lo standalone (ACC-17, Plug.CSRFProtection):
// nel campo _csrf_token del corpo se c'è, altrimenti nell'intestazione x-csrf-token. Le differenze
// di Phoenix su richieste che un browser o un modulo non producono sono la deviazione D-30.
//
// Le due facce stanno sullo stesso host (porte 443 e 8443) e i cookie non distinguono le porte:
// ogni faccia ha il suo cookie, con il segreto firmato da una chiave del server e dal nome della
// faccia. Un cookie impostato da uno script (per esempio un XSS sulla faccia pubblica) non ha la
// firma e non vale. In più, CrossOriginProtection di net/http rifiuta i POST da un'altra origine
// (443 → 8443 compreso).

const (
	// LimiteModulo è il limite del corpo di un modulo, quello di Plug.Parsers (misurato).
	LimiteModulo = 1_000_000
	// lunghezzaSegreto è la lunghezza del segreto, della maschera e della firma, in byte.
	lunghezzaSegreto = 32
)

// CSRF è la protezione dei moduli di una faccia.
type CSRF struct {
	chiave  []byte
	faccia  string
	cookie  string
	origine *http.CrossOriginProtection
}

// NuovoCSRF prepara la protezione di una faccia («interna» o «pubblica») con la chiave del
// server, almeno 32 byte, che deve restare la stessa tra un riavvio e l'altro (altrimenti i
// moduli aperti non valgono più).
func NuovoCSRF(chiave []byte, faccia string) (*CSRF, error) {
	if len(chiave) < 32 {
		return nil, errors.New("csrf: chiave del server più corta di 32 byte")
	}
	if faccia != "interna" && faccia != "pubblica" {
		return nil, fmt.Errorf("csrf: faccia sconosciuta %q", faccia)
	}
	o := http.NewCrossOriginProtection() // si usa Check: il rifiuto è Proibita, come il token
	// __Host- obbliga Secure, Path=/ e nessun Domain: un altro host non può impostarlo.
	return &CSRF{chiave: chiave, faccia: faccia, cookie: "__Host-gestrip_csrf_" + faccia, origine: o}, nil
}

// firma lega il segreto alla chiave del server e alla faccia.
func (c *CSRF) firma(seg []byte) []byte {
	m := hmac.New(sha256.New, c.chiave)
	m.Write([]byte("gestrip csrf " + c.faccia + "\x00"))
	m.Write(seg)
	return m.Sum(nil)
}

// Token dà il token da mettere in un modulo o nel meta csrf-token della pagina. Se il browser
// non ha ancora un segreto valido, lo crea e imposta il cookie (attributi come il cookie dello
// standalone: Path=/, Secure, HttpOnly, SameSite=Lax, senza scadenza): va chiamato prima di
// scrivere la risposta. Più chiamate nella stessa richiesta usano lo stesso segreto. Ogni token
// è diverso (mascherato, come Phoenix), tutti validi con lo stesso segreto.
func (c *CSRF) Token(w http.ResponseWriter, r *http.Request) string {
	seg, ok := c.daValore(c.valoreGiaImpostato(w))
	if !ok {
		seg, ok = c.segreto(r)
	}
	if !ok {
		seg = make([]byte, lunghezzaSegreto)
		_, _ = rand.Read(seg) // crypto/rand.Read non fallisce (in caso estremo il processo si ferma)
		c.impostaCookie(w, seg)
	}
	return maschera(seg)
}

// Rinnova dà al browser un segreto nuovo (al login e all'uscita, insieme alla sessione): i
// token delle pagine aperte prima non valgono più.
func (c *CSRF) Rinnova(w http.ResponseWriter) {
	seg := make([]byte, lunghezzaSegreto)
	_, _ = rand.Read(seg) // crypto/rand.Read non fallisce (in caso estremo il processo si ferma)
	c.impostaCookie(w, seg)
}

func (c *CSRF) impostaCookie(w http.ResponseWriter, seg []byte) {
	h := w.Header()
	vecchi := h.Values("Set-Cookie")
	h.Del("Set-Cookie")
	for _, v := range vecchi { // un solo Set-Cookie per questo nome nella stessa risposta
		if k, err := http.ParseSetCookie(v); err != nil || k.Name != c.cookie {
			h.Add("Set-Cookie", v)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name: c.cookie, Value: base64.RawURLEncoding.EncodeToString(append(append([]byte{}, seg...), c.firma(seg)...)),
		Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

// valoreGiaImpostato è il valore del cookie se questa risposta lo ha già impostato.
func (c *CSRF) valoreGiaImpostato(w http.ResponseWriter) string {
	for _, v := range w.Header().Values("Set-Cookie") {
		if k, err := http.ParseSetCookie(v); err == nil && k.Name == c.cookie {
			return k.Value
		}
	}
	return ""
}

// segreto legge il segreto dal cookie della richiesta; no se manca, non ha la forma giusta o la
// firma non è della chiave del server per questa faccia.
func (c *CSRF) segreto(r *http.Request) ([]byte, bool) {
	k, err := r.Cookie(c.cookie)
	if err != nil {
		return nil, false
	}
	return c.daValore(k.Value)
}

func (c *CSRF) daValore(v string) ([]byte, bool) {
	b, err := base64.RawURLEncoding.DecodeString(v)
	if err != nil || len(b) != 2*lunghezzaSegreto {
		return nil, false
	}
	seg := b[:lunghezzaSegreto]
	if !hmac.Equal(b[lunghezzaSegreto:], c.firma(seg)) {
		return nil, false
	}
	return seg, true
}

// maschera dà base64url(maschera || segreto XOR maschera), con una maschera nuova ogni volta:
// il token in pagina cambia sempre e non rivela il segreto (BREACH).
func maschera(seg []byte) string {
	b := make([]byte, 2*lunghezzaSegreto)
	_, _ = rand.Read(b[:lunghezzaSegreto])
	subtle.XORBytes(b[lunghezzaSegreto:], seg, b[:lunghezzaSegreto])
	return base64.RawURLEncoding.EncodeToString(b)
}

// smaschera è l'inverso di maschera; no se il token non ha la forma giusta.
func smaschera(tok string) ([]byte, bool) {
	b, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil || len(b) != 2*lunghezzaSegreto {
		return nil, false
	}
	seg := make([]byte, lunghezzaSegreto)
	subtle.XORBytes(seg, b[lunghezzaSegreto:], b[:lunghezzaSegreto])
	return seg, true
}

// tokenValido dice se tok, smascherato, è il segreto del browser.
func tokenValido(seg []byte, tok string) bool {
	atteso, ok := smaschera(tok)
	return ok && subtle.ConstantTimeCompare(seg, atteso) == 1
}

// Modulo protegge una rotta POST. Come Plug, prima legge il corpo, se è un modulo: troppo grande
// → 413, illeggibile o UTF-8 non valido → 400. Si legge solo il corpo: la query dell'indirizzo
// non conta (un modulo senza action si invia all'indirizzo corrente, query compresa). Poi: una
// richiesta da un'altra origine, o senza un token valido (il campo _csrf_token del corpo se c'è,
// anche vuoto, altrimenti l'intestazione x-csrf-token, come Plug) → 403 «Forbidden», e il gestore
// non viene chiamato. Il gestore trova i campi in r.PostForm. Ordine con le intestazioni delle
// pagine: c.Modulo(Pagina(h)), perché il 403 dello standalone non le ha.
func (c *CSRF) Modulo(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		campi := url.Values{}
		if tipo, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); tipo == "application/x-www-form-urlencoded" {
			corpo, err := io.ReadAll(http.MaxBytesReader(w, r.Body, LimiteModulo))
			if _, grande := errors.AsType[*http.MaxBytesError](err); grande {
				TroppoGrande(w, r)
				return
			}
			if err == nil {
				campi, err = url.ParseQuery(string(corpo))
			}
			if err != nil || !campiUTF8(campi) {
				NonValida(w, r)
				return
			}
		}
		r.PostForm = campi
		seg, ok := c.segreto(r)
		// Come Plug: il campo del corpo se c'è, anche vuoto; solo senza, l'intestazione.
		tok := r.Header.Get("X-Csrf-Token")
		if campi.Has("_csrf_token") {
			tok = campi.Get("_csrf_token")
		}
		if c.origine.Check(r) != nil || !ok || !tokenValido(seg, tok) {
			Proibita(w, r)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// campiUTF8 dice se chiavi e valori sono UTF-8 valido (Plug risponde 400 altrimenti).
func campiUTF8(campi url.Values) bool {
	for k, vs := range campi {
		if !utf8.ValidString(k) {
			return false
		}
		for _, v := range vs {
			if !utf8.ValidString(v) {
				return false
			}
		}
	}
	return true
}

// Proibita è il 403 dello standalone (token CSRF mancante o sbagliato): «Forbidden».
func Proibita(w http.ResponseWriter, r *http.Request) {
	paginaErrore(w, r, http.StatusForbidden, "Forbidden")
}

// TroppoGrande è il 413 di Plug.Parsers: «Request Entity Too Large», senza HSTS né
// x-request-id come il 400 (misurato: è un errore dei parser, prima degli altri plug).
func TroppoGrande(w http.ResponseWriter, r *http.Request) {
	w.Header().Del("Strict-Transport-Security")
	w.Header().Del("X-Request-Id")
	paginaErrore(w, r, http.StatusRequestEntityTooLarge, "Request Entity Too Large")
}

// paginaErrore scrive una pagina d'errore come ErrorHTML ed ErrorJSON dello standalone.
func paginaErrore(w http.ResponseWriter, r *http.Request, codice int, testo string) {
	h := w.Header()
	h.Set("Cache-Control", cacheBase)
	if formatoErrore(r) == "json" {
		h.Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(codice)
		_, _ = w.Write([]byte(`{"errors":{"detail":"` + testo + `"}}`)) // client andato: niente da fare
		return
	}
	h.Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(codice)
	_, _ = w.Write([]byte(testo))
}
