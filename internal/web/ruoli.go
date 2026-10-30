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
func (f *facciaWeb) rotta(mux *http.ServeMux, pattern string, chi permesso, h http.HandlerFunc) {
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
	f.speciale(mux, pattern, chi, g)
}

// speciale registra una rotta con una catena sua (/healthz, POST /login), dichiarandone il
// permesso per la tabella.
func (f *facciaWeb) speciale(mux *http.ServeMux, pattern string, chi permesso, h http.Handler) {
	f.rotte = append(f.rotte, rottaDichiarata{pattern, chi})
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
