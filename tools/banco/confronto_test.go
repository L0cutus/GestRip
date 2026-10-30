package main

import (
	"strings"
	"testing"
)

func TestLeggiScenario(t *testing.T) {
	s, err := leggiScenario("healthz.json", strings.NewReader(`{
		"nome": "healthz",
		"inventario": ["ADM-20"],
		"passi": [{"metodo": "GET", "percorso": "/healthz?ignored=x",
		           "confronta": ["codice", "corpo", "intestazione:content-type", "intestazione:set-cookie"]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Nome != "healthz" || len(s.Passi) != 1 || s.Passi[0].Percorso != "/healthz?ignored=x" {
		t.Errorf("scenario = %+v", s)
	}
}

func TestLeggiScenarioRifiuta(t *testing.T) {
	casi := map[string]string{
		"campo ignoto":       `{"nome":"x","passi":[{"metodo":"GET","percorso":"/","confronta":["colore"]}]}`,
		"senza passi":        `{"nome":"x","passi":[]}`,
		"percorso assoluto":  `{"nome":"x","passi":[{"metodo":"GET","percorso":"https://altro/","confronta":["codice"]}]}`,
		"chiave sconosciuta": `{"nome":"x","pasi":[]}`,
		"inventario errato":  `{"nome":"x","inventario":["adm20"],"passi":[{"metodo":"GET","percorso":"/","confronta":["codice"]}]}`,
	}
	for nome, src := range casi {
		if _, err := leggiScenario("x.json", strings.NewReader(src)); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
}

func esito(codice int, corpo string, h map[string][]string) risposta {
	return risposta{Codice: codice, Corpo: corpo, Intestazioni: h}
}

func TestConfrontaUguali(t *testing.T) {
	p := passo{Confronta: []string{"codice", "corpo", "intestazione:cache-control", "intestazione:set-cookie"}}
	a := esito(200, "ok", map[string][]string{"Cache-Control": {"no-store"}, "Server": {"Caddy"}})
	b := esito(200, "ok", map[string][]string{"Cache-Control": {"no-store"}})
	if d := confronta(p, a, b); len(d) != 0 {
		t.Errorf("differenze inattese: %v", d)
	}
}

func TestConfrontaTrovaLeDifferenze(t *testing.T) {
	p := passo{Confronta: []string{"codice", "corpo", "intestazione:cache-control", "intestazione:set-cookie"}}
	a := esito(200, "ok", map[string][]string{"Cache-Control": {"no-store"}})
	b := esito(503, "unavailable", map[string][]string{"Cache-Control": {"private"}, "Set-Cookie": {"x=1"}})
	d := confronta(p, a, b)
	campi := map[string]bool{}
	for _, x := range d {
		campi[x.Campo] = true
	}
	for _, c := range []string{"codice", "corpo", "intestazione:cache-control", "intestazione:set-cookie"} {
		if !campi[c] {
			t.Errorf("differenza su %s non trovata: %v", c, d)
		}
	}
}

func TestDeviazioniCopronoSoloLaDifferenzaDichiarata(t *testing.T) {
	devs, err := leggiDeviazioni(strings.NewReader(`[
		{"id": "D-05", "scenario": "blocco", "passo": 0, "campo": "intestazione:cache-control",
		 "standalone": "no-store", "go": "no-store, private", "motivo": "esempio"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	attesa := differenza{Scenario: "blocco", Passo: 0, Campo: "intestazione:cache-control", Standalone: "no-store", Go: "no-store, private"}
	if id, ok := devs.copre(attesa); !ok || id != "D-05" {
		t.Errorf("deviazione dichiarata non riconosciuta: %q %v", id, ok)
	}
	altro := attesa
	altro.Go = "public"
	if _, ok := devs.copre(altro); ok {
		t.Error("una deviazione con un valore diverso da quello dichiarato è stata accettata")
	}
	altro = attesa
	altro.Scenario = "altro"
	if _, ok := devs.copre(altro); ok {
		t.Error("deviazione applicata allo scenario sbagliato")
	}
}

func TestLeggiDeviazioniRifiutaIDNonValidi(t *testing.T) {
	if _, err := leggiDeviazioni(strings.NewReader(`[{"id":"X-1","scenario":"a","passo":0,"campo":"codice","standalone":"1","go":"2","motivo":"m"}]`)); err == nil {
		t.Error("ID di deviazione non valido accettato")
	}
	if _, err := leggiDeviazioni(strings.NewReader(`[{"id":"D-01","scenario":"a","passo":0,"campo":"codice","standalone":"1","go":"2"}]`)); err == nil {
		t.Error("deviazione senza motivo accettata")
	}
}

func TestDeviazioniApprovateNellInventario(t *testing.T) {
	inv := "| ID | Voce |\n|---|---|\n| D-01 | RIP-S-46 | x | y | Stefano |\n| D-05 | CLI-21 | x | y | Stefano |\n"
	ok, err := deviazioniApprovate(strings.NewReader(inv))
	if err != nil {
		t.Fatal(err)
	}
	if !ok["D-01"] || !ok["D-05"] || ok["D-02"] {
		t.Errorf("approvate = %v", ok)
	}
}
