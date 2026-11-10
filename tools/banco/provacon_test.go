package main

import (
	"slices"
	"strings"
	"testing"
)

// prova_con (gli scenari HTTP come quelli browser): con dati che non sono nell'elenco lo scenario
// gira e si confronta (una differenza resta rossa), ma non prova la sua voce: non ha un esito, è fra
// i non provati, e la copertura non lo conta come verde.
func TestProvaConScenariHTTP(t *testing.T) {
	s, err := leggiScenario("x.json", strings.NewReader(`{"nome":"x","prova_con":["sintetici-import"],"passi":[{"metodo":"GET","percorso":"/healthz","confronta":["codice"]}]}`))
	if err != nil || !slices.Equal(s.ProvaCon, []string{datiSinteticiImport}) {
		t.Fatalf("scenario = %+v, %v", s, err)
	}
	for _, j := range []string{
		`{"nome":"x","prova_con":["altri"],"passi":[{"metodo":"GET","percorso":"/"}]}`,
		`{"nome":"x","prova_con":[],"passi":[{"metodo":"GET","percorso":"/"}]}`,
	} {
		if _, err := leggiScenario("x.json", strings.NewReader(j)); err == nil {
			t.Errorf("accettato %s", j)
		}
	}
	a, b := server("ok", nil), server("ok", nil)
	defer a.Close()
	defer b.Close()
	ia, ib := istanza{"standalone", a.URL, a.Client()}, istanza{"go", b.URL, b.Client()}
	rap := eseguiScenariCon([]scenario{s, scenarioProva}, nil, ia, ib, credenziali{}, "")
	if !rap.ok() || !slices.Equal(rap.NonProvati, []string{"x"}) || !rap.Verdi["prova"] {
		t.Fatalf("database vuoto: %+v", rap)
	}
	if _, c := rap.Verdi["x"]; c {
		t.Errorf("scenario non provato con un esito: %v", rap.Verdi)
	}
	rap = eseguiScenariCon([]scenario{s}, nil, ia, ib, credenziali{}, datiSinteticiImport)
	if !rap.Verdi["x"] || len(rap.NonProvati) != 0 {
		t.Fatalf("dati importati: %+v", rap)
	}
	// Non provato ma con una differenza: il banco è rosso lo stesso.
	c := server("KO", nil)
	defer c.Close()
	s.Passi[0].Confronta = []string{"corpo"}
	rap = eseguiScenariCon([]scenario{s}, nil, ia, istanza{"go", c.URL, c.Client()}, credenziali{}, "")
	if rap.ok() || rap.Inattese != 1 {
		t.Fatalf("differenza su uno scenario non provato: %+v", rap)
	}
}

// Gli scenari HTTP che sul database vuoto vedono solo «non trovata» lo dicono.
func TestProvaConDegliScenariDelRepo(t *testing.T) {
	ss, err := leggiScenari("../../bench/scenari")
	if err != nil {
		t.Fatal(err)
	}
	soloDati := []string{"anteprima-cliente", "anteprima-cliente-dipendente", "busta-etichetta", "cerca-qr", "coda-foto", "etichetta", "etichetta-scheda-dipendente", "firma", "foto", "foto-dipendente", "foto-ultra-hdr", "galleria", "home", "home-cerca", "home-contatori", "home-dipendente", "home-filtri", "home-galleria", "link", "modifica", "numeri", "pagina-cliente", "pagina-cliente-conversazione", "pagina-cliente-foto", "pagina-cliente-limite", "pagina-cliente-preventivo", "pagina-storica", "pagina-storica-rifiuti", "qr-pannello",
		"report", "report-dipendente", "report-filtri", "ricevuta", "ricevuta-dipendente", "scheda", "scheda-pdf", "stampa-etichetta-busta", "stati", "viste"}
	for _, s := range ss {
		voluto := []string(nil)
		if slices.Contains(soloDati, s.Nome) {
			voluto = []string{datiSinteticiImport}
		}
		if !slices.Equal(s.ProvaCon, voluto) {
			t.Errorf("%s: prova_con = %v, atteso %v", s.Nome, s.ProvaCon, voluto)
		}
	}
}

// Una D-xx del livello browser non copre una differenza del livello HTTP.
func TestDeviazioneDelBrowserNonCopreLHTTP(t *testing.T) {
	a, b := server("ok", nil), server("OK", nil)
	defer a.Close()
	defer b.Close()
	devs := deviazioni{{ID: "D-99", Livello: livelloBrowser, Scenario: "prova", Passo: 0, Campo: "corpo", Standalone: "ok", Go: "OK", Motivo: "prova"}}
	rap := eseguiScenari([]scenario{scenarioProva}, devs, istanza{"standalone", a.URL, a.Client()}, istanza{"go", b.URL, b.Client()})
	if rap.Inattese != 1 || rap.Attese != 0 {
		t.Errorf("rapporto = %+v", rap)
	}
}
