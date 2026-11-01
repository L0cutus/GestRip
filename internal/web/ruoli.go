package web

import (
	"net/http"
	"strings"

	"github.com/L0cutus/gestrip/internal/accounts"
)

// Ruoli titolare e dipendente (ACC-05), come lo standalone a go-baseline: ogni rotta della faccia
// interna si registra con chi la può usare, e il controllo sta sul server, a ogni richiesta, non
// nel pulsante nascosto. La tabella dei permessi dello standalone è un test (ruoli_test.go): una
// rotta che non c'è, o con un permesso diverso, lo fa fallire.

// permesso dice chi può usare una rotta.
type permesso int

const (
	aperta       permesso = iota // anche senza accesso (/login, /healthz)
	personale                    // chiunque abbia fatto l'accesso
	soloTitolare                 // solo il titolare
)

func (p permesso) String() string {
	return [...]string{"aperta", "personale", "solo titolare"}[p]
}

// msgSoloTitolare è il messaggio di require_owner/2 e on_mount(:ensure_owner) dello standalone.
const msgSoloTitolare = "Operazione riservata al titolare."

type rottaDichiarata struct {
	pattern string
	chi     permesso
	guscio  bool
	rifiuto string // un'azione del titolare dentro una pagina comune: il suo messaggio al dipendente
}

// rotta registra una rotta con il suo permesso e la catena dello standalone:
//   - le intestazioni delle pagine e la sessione, ricontrollata a ogni richiesta (ACC-04);
//   - un POST passa prima dal CSRF, come protect_from_forgery nella pipeline :browser, che viene
//     prima di :require_authenticated_user e di :require_owner_user;
//   - senza accesso si va a /login (personale e solo titolare);
//   - un dipendente su una rotta del titolare va a / con «Operazione riservata al titolare.»,
//     pagina o modulo che sia (non un 403), e il gestore non parte.
//
// Il pattern ha sempre il metodo (GET o POST): una rotta senza è un errore di programmazione.
// Le sue pagine non hanno il guscio interno: per quello c'è rottaConGuscio.
func (f *facciaWeb) rotta(mux *http.ServeMux, pattern string, chi permesso, h http.HandlerFunc) {
	f.catena(mux, pattern, chi, false, h)
}

// rottaConGuscio è rotta per una pagina del personale che porta il guscio interno (menu ☰,
// tavolozze, «🏠 Lista», manifest e worker), come il pipeline :staff_shell dello standalone. Va
// usata per ogni rotta che disegna una pagina interna, anche un POST che la ridisegna; una rotta
// aperta non può averlo.
func (f *facciaWeb) rottaConGuscio(mux *http.ServeMux, pattern string, chi permesso, h http.HandlerFunc) {
	if chi == aperta {
		panic("web: guscio interno su una rotta aperta: " + pattern)
	}
	f.catena(mux, pattern, chi, true, h)
}

func (f *facciaWeb) catena(mux *http.ServeMux, pattern string, chi permesso, guscio bool, h http.HandlerFunc) {
	var g http.Handler = h
	switch chi {
	case soloTitolare:
		g = f.richiedeAccesso(f.richiedeTitolare(g))
	case personale:
		g = f.richiedeAccesso(g)
	}
	g = Pagina(f.conSessione(g))
	switch {
	case strings.HasPrefix(pattern, "POST "):
		g = f.csrf.Modulo(g)
	case strings.HasPrefix(pattern, "GET "):
	default:
		panic("web: rotta senza metodo GET o POST: " + pattern)
	}
	f.registra(mux, pattern, chi, guscio, g)
}

// azioneDelTitolare registra un'azione del titolare dentro una pagina comune (POST), come gli eventi
// di una LiveView di tutti che chiedono Accounts.owner?/1 (CLI-24, CLI-25): la catena di rotta per
// il permesso soloTitolare, ma un dipendente non va a / con «Operazione riservata al titolare.»:
// torna alla pagina (torna) con il messaggio dell'azione (rifiuto), e il gestore non parte. Il ruolo
// è quello riletto a questa richiesta. Le sue pagine non hanno il guscio interno: l'azione finisce
// sempre con un redirect.
func (f *facciaWeb) azioneDelTitolare(mux *http.ServeMux, pattern, rifiuto string, torna func(*http.Request) string, h http.HandlerFunc) {
	if !strings.HasPrefix(pattern, "POST ") || rifiuto == "" {
		panic("web: azione del titolare senza POST o senza messaggio: " + pattern)
	}
	g := f.csrf.Modulo(Pagina(f.conSessione(f.richiedeAccesso(f.titolareOppure(rifiuto, torna, h)))))
	f.registra(mux, pattern, soloTitolare, false, g)
	f.rotte[len(f.rotte)-1].rifiuto = rifiuto
}

// titolareOppure fa partire h solo per il titolare; il dipendente torna alla pagina con rifiuto.
func (f *facciaWeb) titolareOppure(rifiuto string, torna func(*http.Request) string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := statoDi(r)
		if st.utente != nil && st.utente.Ruolo == accounts.Titolare {
			h.ServeHTTP(w, r)
			return
		}
		st.dati.Errore, st.cambiata = rifiuto, true
		f.vaiA(w, r, torna(r))
	})
}

// rottaPubblica registra una rotta della faccia pubblica con la catena del pipeline :browser dello
// standalone, per costruzione: le intestazioni delle pagine e, per un POST, prima il CSRF della
// faccia pubblica (protect_from_forgery), con il 403 senza intestazioni di pagina come lo
// standalone. Nessuna sessione né permesso: il token nel percorso è tutta l'autorizzazione. Il
// pattern ha sempre il metodo (GET o POST).
func (f *facciaWeb) rottaPubblica(mux *http.ServeMux, pattern string, h http.HandlerFunc) {
	g := Pagina(h)
	switch {
	case strings.HasPrefix(pattern, "POST "):
		g = f.csrf.Modulo(g)
	case strings.HasPrefix(pattern, "GET "):
	default:
		panic("web: rotta pubblica senza metodo GET o POST: " + pattern)
	}
	f.registra(mux, pattern, aperta, false, g)
}

// speciale registra una rotta con una catena sua (/healthz, POST /login), dichiarandone il
// permesso per la tabella; le sue pagine non hanno il guscio interno.
func (f *facciaWeb) speciale(mux *http.ServeMux, pattern string, chi permesso, h http.Handler) {
	f.registra(mux, pattern, chi, false, h)
}

// registra dichiara la rotta per la tabella e segna la superficie delle sue pagine (guscio.go).
func (f *facciaWeb) registra(mux *http.ServeMux, pattern string, chi permesso, guscio bool, h http.Handler) {
	f.rotte = append(f.rotte, rottaDichiarata{pattern: pattern, chi: chi, guscio: guscio})
	h = conSuperficie(superficieDi(pattern, guscio), h)
	mux.Handle(pattern, h)
}

// richiedeTitolare viene dopo richiedeAccesso: l'account c'è, ed è quello riletto adesso dal
// database, quindi un titolare declassato è fuori al clic dopo.
func (f *facciaWeb) richiedeTitolare(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := statoDi(r)
		if st.utente != nil && st.utente.Ruolo == accounts.Titolare {
			h.ServeHTTP(w, r)
			return
		}
		st.dati.Errore, st.cambiata = msgSoloTitolare, true
		f.vaiA(w, r, "/")
	})
}
