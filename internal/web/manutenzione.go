package web

import (
	"net/http"
)

// testoManutenzione è la risposta della faccia pubblica di una copia ripristinata ancora isolata (slice 130).
const testoManutenzione = "GestRip è in manutenzione. Riprova più tardi.\n"

// PubblicaInManutenzione è la faccia pubblica di una copia ripristinata ancora isolata (slice 130, OPS-19, D-12,
// risposta I): /healthz com'è sulla faccia pubblica (con il suo filtro); ogni altra richiesta, con ogni metodo,
// riceve un 503 «in manutenzione» in testo semplice, senza cache, prima di ogni filtro. Le pagine dei clienti non
// si registrano nemmeno: una copia di prova raggiunta dal tunnel pubblico (Funnel è fuori dal programma, «Rischi»
// del piano) non apre buste, garanzie né link degli artigiani.
func PubblicaInManutenzione(d Dipendenze) (http.Handler, error) {
	salute, _, err := costruisciPubblica(d, func(*facciaWeb, *http.ServeMux) {})
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			salute.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(testoManutenzione)) // il client può essere già andato via
	}), nil
}
