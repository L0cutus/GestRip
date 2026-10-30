// Package web contiene le due facce HTTP di GestRip: interna (Tailscale) e pubblica (Funnel).
// Ognuna ha il suo elenco di rotte (PIANO §1.5).
package web

import (
	"context"
	"net/http"
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
			w.Write([]byte("ok"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("unavailable"))
	})
}

// Interna è la faccia interna (gestione), solo nella tailnet.
func Interna(s Sonda) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", Salute(s))
	return mux
}

// Pubblica è la faccia pubblica (clienti, artigiani, acquirenti), dietro Tailscale Funnel.
func Pubblica(s Sonda) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", Salute(s))
	return mux
}
