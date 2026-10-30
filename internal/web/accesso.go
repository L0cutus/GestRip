package web

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"html/template"
	"net/http"
	"regexp"
	"strings"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clientip"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// Accesso, uscita e controllo della sessione (ACC-01…04), sulla faccia interna (D-28).

// Utenti è quello che le facce chiedono agli account (internal/accounts).
type Utenti interface {
	Autentica(ctx context.Context, email, password string) (accounts.Utente, bool, error)
	DiSessione(ctx context.Context, id, versione int64) (accounts.Utente, bool, error)
	RegistraAccesso(ctx context.Context, id int64) error
	ElencoUtenti(ctx context.Context) ([]accounts.VoceUtente, error)
	Disattiva(ctx context.Context, id int64, c audit.Contesto) error
	CambiaRuolo(ctx context.Context, id int64, ruolo string, c audit.Contesto) error
}

// Registro scrive una riga d'audit fuori da una transazione (internal/audit).
type Registro interface {
	Registra(ctx context.Context, c audit.Contesto, azione, risorsa string, id any, esito string, dati map[string]any) error
}

//go:embed pagine/*.html
var filePagine embed.FS

var pagine = template.Must(template.New("").Funcs(template.FuncMap{"asset": percorsoAsset}).ParseFS(filePagine, "pagine/*.html"))

const (
	msgCredenziali = "Email o password non validi."
	msgAccedi      = "Devi accedere per continuare."
)

// datiPagina è quello che le pagine ricevono.
type datiPagina struct {
	Token  string
	Errore string
	Info   string
	Dati   any    // quello che serve alla pagina
	Guscio guscio // testa, menu e coda secondo la superficie della rotta (guscio.go)
}

// mostra scrive una pagina: token CSRF, messaggio una volta sola (quello della sessione dopo un
// redirect, o quello di questa risposta), cookie di sessione se è cambiata.
func (f *facciaWeb) mostra(w http.ResponseWriter, r *http.Request, nome string, codice int, errore string) {
	f.mostraCon(w, r, nome, codice, errore, nil)
}

// mostraCon è mostra con i dati della pagina; anche il messaggio di esito si mostra una volta.
func (f *facciaWeb) mostraCon(w http.ResponseWriter, r *http.Request, nome string, codice int, errore string, dati any) {
	st := statoDi(r)
	if st.dati.Errore != "" {
		if errore == "" {
			errore = st.dati.Errore
		}
		st.dati.Errore, st.cambiata = "", true
	}
	info := st.dati.Info
	if info != "" {
		st.dati.Info, st.cambiata = "", true
	}
	d := datiPagina{Token: f.csrf.Token(w, r), Errore: errore, Info: info, Dati: dati, Guscio: f.guscioPer(r, nome)}
	f.salva(w, st)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(codice)
	_ = pagine.ExecuteTemplate(w, nome, d) // un errore qui è un client andato via: niente da fare
}

// vaiA è il redirect di Phoenix: 302, Location e il suo corpo HTML, con l'indirizzo escapato
// come Plug.HTML.html_escape.
func (f *facciaWeb) vaiA(w http.ResponseWriter, r *http.Request, dove string) {
	f.salva(w, statoDi(r))
	w.Header().Set("Location", dove)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusFound)
	_, _ = w.Write([]byte(`<html><body>You are being <a href="` + escapePlug(dove) + `">redirected</a>.</body></html>`))
}

var sostituzioniPlug = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")

func escapePlug(s string) string { return sostituzioniPlug.Replace(s) }

// richiedeAccesso: senza un account valido si va a /login con «Devi accedere per continuare.»,
// ricordando la pagina se è una lettura (GET) navigata (Sec-Fetch-Dest assente o document)
// e sicura; una pagina non valida cancella quella ricordata prima (ACC-02).
func (f *facciaWeb) richiedeAccesso(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := statoDi(r)
		if st.utente != nil {
			h.ServeHTTP(w, r)
			return
		}
		if dest := r.Header.Get("Sec-Fetch-Dest"); r.Method == http.MethodGet && (dest == "" || dest == "document") {
			st.dati.Ritorno = ""
			if ritornoSicuro(r.RequestURI) {
				st.dati.Ritorno = r.RequestURI
			}
		}
		st.dati.Errore, st.cambiata = msgAccedi, true
		f.vaiA(w, r, "/login")
	})
}

// seFuori: chi è già dentro e apre /login va a /.
func (f *facciaWeb) seFuori(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if statoDi(r).utente != nil {
			f.vaiA(w, r, "/")
			return
		}
		h.ServeHTTP(w, r)
	})
}

var (
	reRitorno        = regexp.MustCompile(`\A/[A-Za-z0-9\-._~!$&'()*+,;=:@%/?#]*\z`)
	reRitornoVietato = regexp.MustCompile(`%(?:[01][0-9A-Fa-f]|7[Ff]|5[Cc])`)
)

// ritornoSicuro sono le regole di UserAuth.safe_return_to/1 dello standalone: un percorso di
// questo sito (un solo «/» non seguito da «/» o «\»), solo caratteri ammessi, al massimo 2000
// byte, nessun carattere di controllo né la barra rovesciata codificati.
func ritornoSicuro(p string) bool {
	return len(p) <= 2000 && reRitorno.MatchString(p) && !strings.HasPrefix(p, "//") && !reRitornoVietato.MatchString(p)
}

func (f *facciaWeb) paginaLogin(w http.ResponseWriter, r *http.Request) {
	f.mostra(w, r, "login", http.StatusOK, "")
}

// entra è POST /login (ACC-01): email normalizzata, solo account attivi con password; ogni
// rifiuto è lo stesso 422. Dopo l'accesso sessione e segreto CSRF sono nuovi, l'accesso è
// registrato e si torna alla pagina chiesta prima, o a /.
func (f *facciaWeb) entra(w http.ResponseWriter, r *http.Request) {
	email, password := r.PostForm["user[email]"], r.PostForm["user[password]"]
	if len(email) == 0 || len(password) == 0 {
		f.mostra(w, r, "login", http.StatusUnprocessableEntity, msgCredenziali)
		return
	}
	// Il limite si guarda prima di cercare l'account e di verificare la password (ACC-20): si
	// prenotano i due posti, account e indirizzo (slice 219), così sessanta login mandati insieme
	// verificano cinque password, non sessanta. Se uno è rifiutato l'altro si restituisce, e la
	// password non si controlla nemmeno.
	ip := clientip.Di(r)
	account := chiaveAccount(ip, email[len(email)-1])
	bAcc, attesaAcc := f.limiti.Prenota(ratelimit.LoginAccount, account)
	bIP, attesaIP := f.limiti.Prenota(ratelimit.LoginIndirizzo, ip)
	if attesaAcc > 0 || attesaIP > 0 {
		f.limiti.Rilascia(ratelimit.LoginAccount, account, bAcc)
		f.limiti.Rilascia(ratelimit.LoginIndirizzo, ip, bIP)
		f.mostra(w, r, "login", http.StatusTooManyRequests, msgTroppi(max(attesaAcc, attesaIP)))
		return
	}
	u, ok, err := f.utenti.Autentica(r.Context(), email[len(email)-1], password[len(password)-1])
	if err != nil {
		// Come lo standalone, dove un'eccezione nel controller non sistema i posti: restano
		// occupati fino a fine finestra, senza mai contare per un blocco (review della 009b).
		Errore(w, r)
		return
	}
	if !ok {
		// I due posti diventano falliti; il rifiuto che fa partire il blocco è già il 429
		// (misurato). Ogni blocco che parte scrive la sua riga d'audit.
		if f.limiti.Fallito(ratelimit.LoginAccount, account, bAcc) {
			f.registraBlocco(r.Context(), ip, ratelimit.LoginAccount)
		}
		if f.limiti.Fallito(ratelimit.LoginIndirizzo, ip, bIP) {
			f.registraBlocco(r.Context(), ip, ratelimit.LoginIndirizzo)
		}
		if attesa := f.attesaLogin(ip, account); attesa > 0 {
			f.mostra(w, r, "login", http.StatusTooManyRequests, msgTroppi(attesa))
			return
		}
		f.mostra(w, r, "login", http.StatusUnprocessableEntity, msgCredenziali)
		return
	}
	// Login riuscito: il posto dell'indirizzo torna libero, l'account si azzera.
	f.limiti.Rilascia(ratelimit.LoginIndirizzo, ip, bIP)
	f.limiti.Azzera(ratelimit.LoginAccount, account)
	if err := f.utenti.RegistraAccesso(r.Context(), u.ID); err != nil {
		Errore(w, r)
		return
	}
	st := statoDi(r)
	dove := "/"
	if ritornoSicuro(st.dati.Ritorno) {
		dove = st.dati.Ritorno
	}
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce) // crypto/rand.Read non fallisce (in caso estremo il processo si ferma)
	st.dati, st.cambiata = datiSessione{Utente: u.ID, Versione: u.Versione, Nonce: base64.RawURLEncoding.EncodeToString(nonce),
		Accesso: f.orologio.Adesso().Unix()}, true
	f.csrf.Rinnova(w)
	f.vaiA(w, r, dove)
}

// esci è l'uscita (ACC-03): cancella la sessione di questo browser, rinnova il segreto CSRF e
// porta a /login. Le altre sessioni restano (per quelle c'è «chiudi tutte»).
func (f *facciaWeb) esci(w http.ResponseWriter, r *http.Request) {
	f.sessioni.cancella(w)
	statoDi(r).cambiata = false
	f.csrf.Rinnova(w)
	f.vaiA(w, r, "/login")
}

// home è la pagina iniziale provvisoria: la lista delle buste arriva con la T3.
func (f *facciaWeb) home(w http.ResponseWriter, r *http.Request) {
	f.mostra(w, r, "home", http.StatusOK, "")
}
