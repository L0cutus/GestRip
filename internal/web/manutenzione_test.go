package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Voce OPS-19, in parte (la conferma della 131 manca): niente segno «inventario:».
// La faccia pubblica di una copia ripristinata ancora isolata (slice 130, risposta I): ogni pagina, con ogni
// metodo, risponde 503 «in manutenzione» in testo semplice, senza cookie né redirect e senza cache; le pagine dei
// clienti (buste /r/, garanzie /g/, link storici, artigiani) non si aprono. Resta /healthz.
func TestPubblicaInManutenzione(t *testing.T) {
	h, err := PubblicaInManutenzione(dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, &utentiFinti{attivo: true}))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ metodo, percorso string }{
		{"GET", "/"}, {"GET", "/r/TOKENINVENTATO"}, {"GET", "/r/TOKENINVENTATO/telegram"}, {"POST", "/r/TOKENINVENTATO/email"},
		{"GET", "/g/qualcosa"}, {"GET", "/stato/1?pw=abc"}, {"GET", "/artigiano/1?t=g1.x.y"}, {"HEAD", "/r/x"},
		{"GET", "/static/app.css"}, {"GET", "/login"}, {"GET", "/foto/1/abc?sig=x"}, {"GET", "/thumb/1/abc?sig=x"},
		{"GET", "/reset/CODICEINVENTATO"}, {"GET", "/r/TOKEN%2Finventato"}, {"GET", "/healthz/altro"}, {"GET", "/%2Fhealthz"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c.metodo, c.percorso, nil))
		corpo := rec.Body.String()
		if rec.Code != http.StatusServiceUnavailable || (c.metodo != "HEAD" && corpo != testoManutenzione) {
			t.Errorf("%s %s: %d %q", c.metodo, c.percorso, rec.Code, corpo)
		}
		hd := rec.Header()
		if hd.Get("Content-Type") != "text/plain; charset=utf-8" || hd.Get("Cache-Control") != "no-store" || hd.Get("Retry-After") == "" ||
			hd.Get("Set-Cookie") != "" || hd.Get("Location") != "" {
			t.Errorf("%s %s: intestazioni %v", c.metodo, c.percorso, hd)
		}
	}
	if !strings.Contains(testoManutenzione, "manutenzione") {
		t.Errorf("testo %q", testoManutenzione)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Errorf("/healthz: %d %q", rec.Code, rec.Body.String())
	}
}
