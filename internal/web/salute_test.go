package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type sondaFinta struct {
	pronto   bool
	chiamate int
}

func (s *sondaFinta) Pronto(context.Context) bool { s.chiamate++; return s.pronto }

// inventario: ADM-20
// Come lo standalone (health_controller_test.exs): corpo esatto, text/plain, no-store,
// nessun cookie, nessun redirect; la query string non conta.
func TestSalute(t *testing.T) {
	casi := []struct {
		nome   string
		pronto bool
		codice int
		corpo  string
	}{
		{"pronto", true, http.StatusOK, "ok"},
		{"non pronto", false, http.StatusServiceUnavailable, "unavailable"},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			s := &sondaFinta{pronto: c.pronto}
			rec := httptest.NewRecorder()
			Salute(s).ServeHTTP(rec, httptest.NewRequest("GET", "/healthz?ignored=private-shop-value", nil))
			if rec.Code != c.codice || rec.Body.String() != c.corpo {
				t.Errorf("risposta %d %q, attesa %d %q", rec.Code, rec.Body.String(), c.codice, c.corpo)
			}
			h := rec.Header()
			if got := h.Get("Content-Type"); got != "text/plain; charset=utf-8" {
				t.Errorf("content-type = %q", got)
			}
			if got := h.Values("Cache-Control"); len(got) != 1 || got[0] != "no-store" {
				t.Errorf("cache-control = %q", got)
			}
			for _, vietata := range []string{"Set-Cookie", "Location"} {
				if h.Get(vietata) != "" {
					t.Errorf("%s presente: %q", vietata, h.Get(vietata))
				}
			}
			if s.chiamate != 1 {
				t.Errorf("sonda chiamata %d volte", s.chiamate)
			}
		})
	}
}

// inventario: ADM-20
// /healthz risponde su tutte e due le facce: lo usano il controllo d'avvio e il banco.
func TestSaluteSuTutteLeFacce(t *testing.T) {
	s := &sondaFinta{pronto: true}
	for nome, h := range map[string]http.Handler{"interna": Interna(s), "pubblica": Pubblica(s)} {
		for _, metodo := range []string{"GET", "HEAD"} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(metodo, "/healthz", nil))
			if rec.Code != http.StatusOK {
				t.Errorf("%s %s /healthz = %d", nome, metodo, rec.Code)
			}
		}
	}
}
