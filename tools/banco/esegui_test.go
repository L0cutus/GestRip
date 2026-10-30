package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func server(corpo string, extra map[string]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range extra {
			w.Header().Set(k, v)
		}
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/vai" {
			http.Redirect(w, r, "/arrivo", http.StatusFound)
			return
		}
		w.Write([]byte(corpo))
	}))
}

var scenarioProva = scenario{Nome: "prova", Passi: []passo{
	{Metodo: "GET", Percorso: "/healthz", Confronta: []string{"codice", "corpo", "intestazione:cache-control"}},
	{Metodo: "GET", Percorso: "/vai", Confronta: []string{"codice", "intestazione:location"}},
}}

func TestEseguiUguali(t *testing.T) {
	a, b := server("ok", map[string]string{"Server": "Caddy"}), server("ok", nil)
	defer a.Close()
	defer b.Close()
	rap := eseguiScenari([]scenario{scenarioProva}, nil, istanza{"standalone", a.URL, a.Client()}, istanza{"go", b.URL, b.Client()})
	if rap.Inattese != 0 || rap.Passi != 2 || len(rap.Errori) != 0 {
		t.Errorf("rapporto = %+v", rap)
	}
}

func TestEseguiDifferenzaInattesaEDeviazione(t *testing.T) {
	a, b := server("ok", nil), server("OK", nil)
	defer a.Close()
	defer b.Close()
	rap := eseguiScenari([]scenario{scenarioProva}, nil, istanza{"standalone", a.URL, a.Client()}, istanza{"go", b.URL, b.Client()})
	if rap.Inattese != 1 {
		t.Fatalf("inattese = %d, attesa 1: %+v", rap.Inattese, rap)
	}
	devs := deviazioni{{ID: "D-99", Scenario: "prova", Passo: 0, Campo: "corpo", Standalone: "ok", Go: "OK", Motivo: "prova"}}
	rap = eseguiScenari([]scenario{scenarioProva}, devs, istanza{"standalone", a.URL, a.Client()}, istanza{"go", b.URL, b.Client()})
	if rap.Inattese != 0 || rap.Attese != 1 {
		t.Errorf("con la deviazione: %+v", rap)
	}
}

func TestEseguiNonSegueIRedirect(t *testing.T) {
	a := server("ok", nil)
	defer a.Close()
	r, err := chiedi(istanza{"x", a.URL, a.Client()}, passo{Metodo: "GET", Percorso: "/vai"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Codice != http.StatusFound || r.valore("intestazione:location") != "/arrivo" {
		t.Errorf("risposta = %+v", r)
	}
}

func TestEseguiIstanzaIrraggiungibile(t *testing.T) {
	a := server("ok", nil)
	defer a.Close()
	rap := eseguiScenari([]scenario{scenarioProva}, nil, istanza{"standalone", a.URL, a.Client()}, istanza{"go", "http://127.0.0.1:1", http.DefaultClient})
	if len(rap.Errori) == 0 || rap.ok() {
		t.Errorf("istanza irraggiungibile non segnalata: %+v", rap)
	}
}

func TestEsitoPerScenario(t *testing.T) {
	a, b := server("ok", nil), server("OK", nil)
	defer a.Close()
	defer b.Close()
	altro := scenario{Nome: "altro", Passi: []passo{{Metodo: "GET", Percorso: "/vai", Confronta: []string{"codice"}}}}
	rap := eseguiScenari([]scenario{scenarioProva, altro}, nil, istanza{"standalone", a.URL, a.Client()}, istanza{"go", b.URL, b.Client()})
	if rap.Verdi["prova"] || !rap.Verdi["altro"] {
		t.Errorf("verdi = %v, attesi prova rosso e altro verde", rap.Verdi)
	}
}
