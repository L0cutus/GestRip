package web

import (
	"errors"
	"net/http"

	"github.com/L0cutus/gestrip/internal/buste"
)

// La pagina del cliente GET /r/{token} sulla faccia pubblica (CLI-01, CLI-02, CLI-03, CLI-21), come
// show/2 e with_public_repair/3 di PublicRepairController dello standalone (go-baseline):
//   - il token è tutta l'autorizzazione: niente sessione né accesso (rottaPubblica, pipeline
//     :browser con le intestazioni delle pagine);
//   - token sbagliato, scaduto, revocato o busta nel cestino: lo stesso 404 «Riparazione non
//     disponibile» (RifiutoPubblico), che il limite ai tentativi falliti conta (limiti.go: 30 in un
//     minuto, poi 15 minuti di blocco con lo stesso 404, D-05);
//   - riuscita: no-store e no-referrer (public_headers/1), la pagina con quello che mostra lo
//     standalone (buste.PaginaCliente) e nient'altro; aprirla non scrive righe d'audit, come
//     show/2.
//
// Le parti della pagina delle slice dopo (062: foto e preventivo; 063: conversazione; T4.6:
// riquadri Telegram ed email) hanno il loro posto segnato in pagina_cliente.html.

// datiPaginaCliente è quello che pagina_cliente.html riceve: il token non c'è.
type datiPaginaCliente struct {
	buste.PaginaCliente
}

// paginaCliente è GET /r/{token}.
func (f *facciaWeb) paginaCliente(w http.ResponseWriter, r *http.Request) {
	p, err := f.buste.PaginaCliente(r.Context(), r.PathValue("token"))
	switch {
	case errors.Is(err, buste.ErrBustaNonTrovata):
		RifiutoPubblico(w, r, rifiutoRiparazione, false)
		return
	case err != nil:
		f.registro.Error("pagina del cliente non letta")
		Errore(w, r)
		return
	}
	intestazioniPubbliche(w)
	f.mostraPubblica(w, r, "pagina_cliente", http.StatusOK, datiPaginaCliente{p})
}

// intestazioniPubbliche è public_headers/1: la pagina non resta in nessuna cache e non manda
// l'indirizzo (con il token) a nessun altro sito.
func intestazioniPubbliche(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

// mostraPubblica disegna una pagina della faccia pubblica: nessuna sessione, nessun messaggio di
// esito del personale, il token CSRF della faccia pubblica nella testa (get_csrf_token/0 del
// layout radice).
func (f *facciaWeb) mostraPubblica(w http.ResponseWriter, r *http.Request, nome string, codice int, dati any) {
	d := datiPagina{Token: f.csrf.Token(w, r), Dati: dati, Guscio: f.guscioPer(r, nome)}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(codice)
	_ = pagine.ExecuteTemplate(w, nome, d) // un errore qui è un client andato via: niente da fare
}

// registraCliente registra sulla faccia pubblica la pagina del cliente e quello che la sua testa
// chiede (asset con l'hash e icona), come lo standalone che li serve su ogni indirizzo.
func (f *facciaWeb) registraCliente(mux *http.ServeMux) {
	f.rottaPubblica(mux, "GET /r/{token}", f.paginaCliente)
	f.speciale(mux, "GET "+indirizzoIcona192, aperta, serviIcona("icon-192.png"))
	f.speciale(mux, "GET /assets/{nome}", aperta, http.HandlerFunc(serviAsset))
}
