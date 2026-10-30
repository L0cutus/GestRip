// Package web contiene le due facce HTTP di GestRip: interna (Tailscale) e pubblica (Funnel).
// Ognuna ha il suo elenco di rotte (PIANO §1.5).
package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// Sonda dice se l'app è pronta a servire (il database risponde).
type Sonda interface {
	Pronto(ctx context.Context) bool
}

// Salute è `GET /healthz` (ADM-20): 200 «ok» o 503 «unavailable», sempre no-store, nessuna
// sessione, nessun log.
func Salute(s Sonda) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("Cache-Control", "no-store")
		if s.Pronto(r.Context()) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok")) // se il client se n'è andato non c'è niente da fare
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("unavailable"))
	})
}

// Dipendenze sono quello che le facce ricevono da chi le avvia. Le chiavi sono sottochiavi
// della chiave del server (db.SottoChiave), mai la chiave stessa.
type Dipendenze struct {
	Sonda          Sonda
	Registro       *slog.Logger
	Utenti         Utenti
	ChiaveSessione []byte                // db.SottoChiave(chiave, "sessione")
	ChiaveCSRF     []byte                // db.SottoChiave(chiave, "csrf")
	Orologio       clock.Orologio        // scadenza delle sessioni (D-31)
	Limiti         *ratelimit.Limitatore // tentativi falliti (login e rotte pubbliche), uno per processo
	Audit          Registro              // righe d'audit fuori da una transazione (inizio di un blocco)
	Recupero       RecuperoPassword      // password dimenticata del titolare (faccia interna)
}

// facciaWeb tiene quello che serve alle rotte di una faccia.
type facciaWeb struct {
	utenti   Utenti
	sessioni *sessioni
	csrf     *CSRF
	orologio clock.Orologio
	limiti   *ratelimit.Limitatore
	audit    Registro
	recupero RecuperoPassword
	registro *slog.Logger
	rotte    []rottaDichiarata // le rotte registrate con il loro permesso (ruoli.go)
}

func nuovaFaccia(d Dipendenze, nome string) (*facciaWeb, error) {
	if len(d.ChiaveSessione) < 32 {
		return nil, errors.New("web: chiave di sessione più corta di 32 byte")
	}
	if d.Orologio == nil {
		return nil, errors.New("web: manca l'orologio")
	}
	if d.Limiti == nil {
		return nil, errors.New("web: manca il limitatore dei tentativi")
	}
	if d.Audit == nil {
		return nil, errors.New("web: manca il registro delle azioni")
	}
	registro := d.Registro
	if registro == nil {
		registro = slog.New(slog.DiscardHandler)
	}
	c, err := NuovoCSRF(d.ChiaveCSRF, nome)
	if err != nil {
		return nil, err
	}
	return &facciaWeb{utenti: d.Utenti, sessioni: sessioniPer(d.ChiaveSessione, nome), csrf: c, orologio: d.Orologio, limiti: d.Limiti,
		audit: d.Audit, recupero: d.Recupero, registro: registro}, nil
}

// Interna è la faccia interna (gestione), solo nella tailnet: /login e il reset stanno solo qui
// (D-28). Ogni rotta si registra con chi la può usare (ruoli.go).
func Interna(d Dipendenze) (http.Handler, error) {
	h, _, err := costruisciInterna(d, nil)
	return h, err
}

// costruisciInterna fa la faccia interna; extra aggiunge rotte (solo i test, per provare i
// permessi su rotte che il Go non ha ancora).
func costruisciInterna(d Dipendenze, extra func(*facciaWeb, *http.ServeMux)) (http.Handler, *facciaWeb, error) {
	f, err := nuovaFaccia(d, "interna")
	if err != nil {
		return nil, nil, err
	}
	if d.Recupero == nil {
		return nil, nil, errors.New("web: manca il recupero della password")
	}
	mux := http.NewServeMux()
	f.speciale(mux, rottaSalute, aperta, Salute(d.Sonda))
	f.rotta(mux, "GET /login", aperta, func(w http.ResponseWriter, r *http.Request) {
		f.seFuori(http.HandlerFunc(f.paginaLogin)).ServeHTTP(w, r)
	})
	// Chi ha già una sessione valida va a / prima del controllo CSRF, come lo standalone (dove il
	// token non cambia al login): una seconda scheda con il modulo di prima non dà 403. È solo un
	// redirect, nessuna azione.
	f.speciale(mux, "POST /login", aperta, f.conSessione(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if statoDi(r).utente != nil {
			Pagina(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.vaiA(w, r, "/") })).ServeHTTP(w, r)
			return
		}
		f.csrf.Modulo(Pagina(http.HandlerFunc(f.entra))).ServeHTTP(w, r)
	})))
	f.rotta(mux, "POST /logout", aperta, f.esci)
	f.rotta(mux, "GET /{$}", personale, f.home)
	f.rotta(mux, "GET /reset-request", aperta, f.paginaRichiestaReset)
	f.rotta(mux, "POST /reset-request", aperta, f.richiediReset)
	f.rotta(mux, "GET /reset/{token}", aperta, f.paginaNuovaPassword)
	f.rotta(mux, "POST /reset/{token}", aperta, f.reimposta)
	f.rotta(mux, "GET /settings/utenti", soloTitolare, f.paginaUtenti)
	f.rotta(mux, "POST /settings/utenti/{id}/disattiva", soloTitolare, f.disattivaUtente)
	f.rotta(mux, "POST /settings/utenti/{id}/ruolo", soloTitolare, f.ruoloUtente)
	if extra != nil {
		extra(f, mux)
	}
	return faccia(mux, d.Registro), f, nil
}

// Pubblica è la faccia pubblica (clienti, artigiani, acquirenti), dietro Tailscale Funnel. Ogni
// richiesta passa dal limite ai tentativi falliti (limiti.go).
func Pubblica(d Dipendenze) (http.Handler, error) {
	return costruisciPubblica(d, nil)
}

// costruisciPubblica fa la faccia pubblica; extra aggiunge rotte (solo i test, finché le rotte
// pubbliche vere non ci sono).
func costruisciPubblica(d Dipendenze, extra func(*http.ServeMux)) (http.Handler, error) {
	f, err := nuovaFaccia(d, "pubblica")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle(rottaSalute, Salute(d.Sonda)) // faccia pubblica: nessuna sessione né permesso
	if extra != nil {
		extra(mux)
	}
	return facciaConFiltro(mux, d.Registro, limitePubblico(d.Limiti, f.registraBlocco)), nil
}
