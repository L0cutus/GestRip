package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
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
		_, _ = w.Write([]byte(corpo))
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

// Un passo può mandare intestazioni (per esempio Accept) e un percorso grezzo, che arriva al
// server così com'è (//, /./, %74).
func TestEseguiMandaIntestazioniEPercorsoGrezzo(t *testing.T) {
	var visto struct{ accept, uri string }
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		visto.accept, visto.uri = r.Header.Get("Accept"), r.RequestURI
	}))
	defer s.Close()
	for _, p := range []string{"//healthz", "//altro.esempio/x", "/./healthz", "/heal%74hz?x=%zz"} {
		if _, err := chiedi(istanza{"x", s.URL, s.Client()}, passo{Metodo: "GET", Percorso: p, Intestazioni: map[string]string{"Accept": "application/json"}}); err != nil {
			t.Fatal(err)
		}
		if visto.accept != "application/json" || visto.uri != p {
			t.Errorf("%s: arrivati accept %q, uri %q", p, visto.accept, visto.uri)
		}
	}
}

// Un passo può mandare un corpo di modulo con i segnaposto {csrf} (dall'ultima pagina di quella
// istanza), {email} e {password} (le credenziali sintetiche); i cookie restano tra i passi dello
// stesso scenario e ripartono vuoti allo scenario dopo.
func TestEseguiModuliECookie(t *testing.T) {
	var visti []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		c, _ := r.Cookie("s")
		valore := ""
		if c != nil {
			valore = c.Value
		}
		visti = append(visti, r.URL.Path+"|"+r.PostForm.Encode()+"|"+valore+"|"+r.Header.Get("Content-Type"))
		if r.URL.Path == "/pagina" {
			http.SetCookie(w, &http.Cookie{Name: "s", Value: "uno"})
			_, _ = w.Write([]byte(`<meta name="csrf-token" content="TOKEN-1"><p>Accesso laboratorio</p>`))
		}
	}))
	defer s.Close()
	sc := []scenario{
		{Nome: "a", Passi: []passo{
			{Metodo: "GET", Percorso: "/pagina", Confronta: []string{"testo:Accesso laboratorio", "testo:Assente qui"}},
			{Metodo: "POST", Percorso: "/invia", Corpo: "_csrf_token={csrf}&user%5Bemail%5D={email}&user%5Bpassword%5D={password}", Confronta: []string{"codice"}},
		}},
		{Nome: "b", Passi: []passo{{Metodo: "GET", Percorso: "/altro", Confronta: []string{"codice"}}}},
	}
	cred := credenziali{Email: "titolare@example.invalid", Password: "p&ss=x"}
	rap := eseguiScenariCon(sc, nil, istanza{"standalone", s.URL, s.Client()}, istanza{"go", s.URL, s.Client()}, cred)
	if !rap.ok() || rap.Passi != 3 {
		t.Fatalf("rapporto = %+v", rap)
	}
	attese := []string{
		"/pagina|||", "/pagina|||",
		"/invia|_csrf_token=TOKEN-1&user%5Bemail%5D=titolare%40example.invalid&user%5Bpassword%5D=p%26ss%3Dx|uno|application/x-www-form-urlencoded",
		"/invia|_csrf_token=TOKEN-1&user%5Bemail%5D=titolare%40example.invalid&user%5Bpassword%5D=p%26ss%3Dx|uno|application/x-www-form-urlencoded",
		"/altro|||", "/altro|||",
	}
	if strings.Join(visti, "\n") != strings.Join(attese, "\n") {
		t.Errorf("richieste:\n%s\nattese:\n%s", strings.Join(visti, "\n"), strings.Join(attese, "\n"))
	}
	// {csrf} senza una pagina che lo dia è un errore, non una richiesta senza token.
	rap = eseguiScenariCon([]scenario{{Nome: "c", Passi: []passo{{Metodo: "POST", Percorso: "/x", Corpo: "_csrf_token={csrf}", Confronta: []string{"codice"}}}}},
		nil, istanza{"standalone", s.URL, s.Client()}, istanza{"go", s.URL, s.Client()}, cred)
	if len(rap.Errori) == 0 {
		t.Error("{csrf} senza token non segnalato")
	}
}

func TestValoreTesto(t *testing.T) {
	r := risposta{Corpo: "<p>Email o password non validi.</p>"}
	if r.valore("testo:Email o password non validi.") != "presente" || r.valore("testo:Accedi") != "assente" {
		t.Error("testo:")
	}
	if !campoValido("testo:Accedi") || campoValido("testo:") {
		t.Error("campoValido testo:")
	}
}

// Un valore atteso che nessuna delle due istanze dà è una differenza, anche se le due sono
// uguali tra loro.
func TestAttesoSuTutteEDue(t *testing.T) {
	a, b := server("ok", nil), server("ok", nil)
	defer a.Close()
	defer b.Close()
	sc := scenario{Nome: "x", Passi: []passo{{Metodo: "GET", Percorso: "/", Confronta: []string{"codice"}, Atteso: map[string]string{"codice": "302"}}}}
	rap := eseguiScenari([]scenario{sc}, nil, istanza{"standalone", a.URL, a.Client()}, istanza{"go", b.URL, b.Client()})
	if rap.Inattese != 1 {
		t.Errorf("atteso non rispettato da nessuna: %+v", rap)
	}
	sc.Passi[0].Atteso = map[string]string{"codice": "200"}
	if rap := eseguiScenari([]scenario{sc}, nil, istanza{"standalone", a.URL, a.Client()}, istanza{"go", b.URL, b.Client()}); rap.Inattese != 0 {
		t.Errorf("atteso rispettato: %+v", rap)
	}
	if _, err := leggiScenario("x.json", strings.NewReader(`{"nome":"x","passi":[{"metodo":"GET","percorso":"/","confronta":["codice"],"atteso":{"colore":"x"}}]}`)); err == nil {
		t.Error("campo atteso non valido accettato")
	}
}
