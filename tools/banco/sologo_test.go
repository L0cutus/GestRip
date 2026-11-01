package main

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// I passi solo_go (slice 037b): un'azione che nello standalone è un evento LiveView e nel Go un
// POST con un redirect a un indirizzo con un segno casuale. Il passo gira solo sul Go, con attese
// esatte, e può seguire la Location o un link della risposta di prima del Go.

// goConSegno è un Go finto: POST /azione porta a /scheda?mostra=<segno>, la scheda ha un link
// «Modifica» con il segno, e il link porta al modulo. Conta le richieste.
func goConSegno(richieste *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		richieste.Add(1)
		switch {
		case r.Method == "POST" && r.URL.Path == "/azione":
			http.Redirect(w, r, "/scheda?mostra=SEGNO1", http.StatusFound)
		case r.URL.Path == "/scheda" && r.URL.RawQuery == "mostra=SEGNO1":
			_, _ = w.Write([]byte(`<a id="edit-repair" href="/scheda?modifica=1&amp;mostra=SEGNO2">Modifica</a>`))
		case r.URL.Path == "/scheda" && r.URL.RawQuery == "modifica=1&mostra=SEGNO2":
			_, _ = w.Write([]byte(`<form id="repair-form"></form>`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

var scenarioSoloGo = scenario{Nome: "viste", Passi: []passo{
	{Metodo: "POST", Percorso: "/azione", Corpo: "x=1", SoloGo: "D-44", Atteso: map[string]string{"codice": "302"}},
	{Metodo: "GET", Percorso: "{location}", SoloGo: "D-44", Atteso: map[string]string{"codice": "200", "visibile:edit-repair": "Modifica"}},
	{Metodo: "GET", Percorso: "{href:edit-repair}", SoloGo: "D-44", Atteso: map[string]string{"codice": "200", "presenza:x-niente": "assente", "testo:repair-form": "presente"}},
}}

// inventario: RIP-S-01
// Un passo solo_go va solo al Go, segue la Location e l'href della risposta di prima del Go, e
// vuole le sue attese; lo standalone non riceve niente.
func TestPassiSoloGo(t *testing.T) {
	var a, b atomic.Int32
	sa, sb := goConSegno(&a), goConSegno(&b)
	defer sa.Close()
	defer sb.Close()
	rap := eseguiScenariCon([]scenario{scenarioSoloGo}, nil, istanza{"standalone", sa.URL, sa.Client()}, istanza{"go", sb.URL, sb.Client()}, credenziali{}, "")
	if !rap.ok() || rap.Passi != 3 || rap.Inattese != 0 {
		t.Fatalf("rapporto = %+v", rap)
	}
	if a.Load() != 0 || b.Load() != 3 {
		t.Errorf("richieste: standalone %d, go %d", a.Load(), b.Load())
	}
	// Dopo un passo comune, {location} è la Location del Go di quel passo.
	misto := scenario{Nome: "misto", Passi: []passo{
		{Metodo: "POST", Percorso: "/azione", Corpo: "x=1", Confronta: []string{"codice"}},
		{Metodo: "GET", Percorso: "{location}", SoloGo: "D-44", Atteso: map[string]string{"codice": "200", "visibile:edit-repair": "Modifica"}},
	}}
	rap = eseguiScenariCon([]scenario{misto}, nil, istanza{"standalone", sa.URL, sa.Client()}, istanza{"go", sb.URL, sb.Client()}, credenziali{}, "")
	if !rap.ok() || rap.Passi != 2 {
		t.Errorf("dopo un passo comune: %+v", rap)
	}
}

// inventario: RIP-S-01
// Un'attesa sbagliata di un passo solo_go è inattesa e nessuna deviazione la copre; un segnaposto
// che non risponde (niente Location, niente link) è un errore, e così una Location che non è un
// percorso relativo sull'istanza.
func TestPassiSoloGoRossi(t *testing.T) {
	var a, b atomic.Int32
	sa, sb := goConSegno(&a), goConSegno(&b)
	defer sa.Close()
	defer sb.Close()
	sbagliato := scenario{Nome: "viste", Passi: slices.Clone(scenarioSoloGo.Passi)}
	sbagliato.Passi[1] = passo{Metodo: "GET", Percorso: "{location}", SoloGo: "D-44", Atteso: map[string]string{"codice": "200", "visibile:edit-repair": "Altro"}}
	devs := deviazioni{{ID: "D-44", Scenario: "viste", Passo: 1, Campo: "atteso visibile:edit-repair=Altro (solo Go, D-44)", Standalone: "(non eseguito)", Go: "Modifica", Motivo: "prova"}}
	rap := eseguiScenariCon([]scenario{sbagliato}, devs, istanza{"standalone", sa.URL, sa.Client()}, istanza{"go", sb.URL, sb.Client()}, credenziali{}, "")
	if rap.ok() || rap.Inattese != 1 || rap.Attese != 0 {
		t.Fatalf("attesa sbagliata: %+v", rap)
	}
	if !strings.Contains(strings.Join(rap.Righe, "\n"), `INATTESA viste[1] atteso visibile:edit-repair=Altro (solo Go, D-44): standalone "(non eseguito)", go "Modifica"`) {
		t.Errorf("righe: %v", rap.Righe)
	}
	senzaLocation := scenario{Nome: "x", Passi: []passo{
		{Metodo: "GET", Percorso: "/scheda?mostra=SEGNO1", SoloGo: "D-44", Atteso: map[string]string{"codice": "200"}},
		{Metodo: "GET", Percorso: "{location}", SoloGo: "D-44", Atteso: map[string]string{"codice": "200"}},
		{Metodo: "GET", Percorso: "{href:altro-link}", SoloGo: "D-44", Atteso: map[string]string{"codice": "200"}},
	}}
	rap = eseguiScenariCon([]scenario{senzaLocation}, nil, istanza{"standalone", sa.URL, sa.Client()}, istanza{"go", sb.URL, sb.Client()}, credenziali{}, "")
	if len(rap.Errori) != 2 || rap.ok() {
		t.Errorf("segnaposti senza risposta: %+v", rap)
	}
	for _, loc := range []string{"https://altro.esempio/x", "//altro.esempio/x", "x", ""} {
		if _, err := percorsoDopo("{location}", risposta{Intestazioni: map[string][]string{"Location": {loc}}}); err == nil {
			t.Errorf("Location %q accettata", loc)
		}
	}
	if p, err := percorsoDopo("{location}&x=1", risposta{Intestazioni: map[string][]string{"Location": {"/a?b=c"}}}); err != nil || p != "/a?b=c&x=1" {
		t.Errorf("Location relativa: %q %v", p, err)
	}
}

// inventario: RIP-S-01
// Un passo solo_go vuole una D-xx, nessun campo da confrontare e almeno un'attesa; i segnaposti
// valgono solo nei passi solo_go. deviazioniSoloGo dà le D-xx da controllare nell'inventario.
func TestLeggiScenarioSoloGo(t *testing.T) {
	buono := `{"nome":"x","passi":[{"metodo":"GET","percorso":"{location}","solo_go":"D-44","atteso":{"codice":"200"}}]}`
	s, err := leggiScenario("x.json", strings.NewReader(buono))
	if err != nil || s.Passi[0].SoloGo != "D-44" {
		t.Fatalf("%v %+v", err, s)
	}
	for nome, src := range map[string]string{
		"senza D-xx":        `{"nome":"x","passi":[{"metodo":"GET","percorso":"/","solo_go":"si","atteso":{"codice":"200"}}]}`,
		"con confronta":     `{"nome":"x","passi":[{"metodo":"GET","percorso":"/","solo_go":"D-44","confronta":["codice"],"atteso":{"codice":"200"}}]}`,
		"senza attese":      `{"nome":"x","passi":[{"metodo":"GET","percorso":"/","solo_go":"D-44"}]}`,
		"solo atteso_dati":  `{"nome":"x","passi":[{"metodo":"GET","percorso":"/","solo_go":"D-44","atteso_dati":{"sintetici-import":{"codice":"200"}}}]}`,
		"segnaposto comune": `{"nome":"x","passi":[{"metodo":"GET","percorso":"{location}","confronta":["codice"]}]}`,
		"href comune":       `{"nome":"x","passi":[{"metodo":"GET","percorso":"/a{href:b}","confronta":["codice"]}]}`,
		"attesa non valida": `{"nome":"x","passi":[{"metodo":"GET","percorso":"/","solo_go":"D-44","atteso":{"colore":"blu"}}]}`,
	} {
		if _, err := leggiScenario("x.json", strings.NewReader(src)); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
	ss := []scenario{scenarioSoloGo, {Nome: "y", Passi: []passo{{Metodo: "GET", Percorso: "/", SoloGo: "D-45", Atteso: map[string]string{"codice": "200"}},
		{Metodo: "GET", Percorso: "/", Confronta: []string{"codice"}}}}}
	if d := deviazioniSoloGo(ss); !slices.Equal(d, []string{"D-44", "D-45"}) {
		t.Errorf("deviazioni solo_go %v", d)
	}
}

// inventario: RIP-S-01
// Un passo solo_standalone va solo allo standalone, con le sue attese; {mostra} nel corpo di un
// passo solo_go è il segno dei moduli della pagina di prima del Go, vuoto se la pagina non lo ha.
func TestPassiSoloStandaloneESegnoDelModulo(t *testing.T) {
	var a, b atomic.Int32
	var corpo atomic.Value
	sa := goConSegno(&a)
	defer sa.Close()
	sb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.Add(1)
		_ = r.ParseForm()
		if r.Method == "POST" {
			corpo.Store(r.PostForm.Encode())
			return
		}
		_, _ = w.Write([]byte(`<form><input name="mostra" type="hidden" value="SEGNO-3_x"></form>`))
	}))
	defer sb.Close()
	sc := scenario{Nome: "x", Passi: []passo{
		{Metodo: "GET", Percorso: "/scheda?mostra=SEGNO1", SoloStandalone: "D-44", Atteso: map[string]string{"codice": "200", "visibile:edit-repair": "Modifica"}},
		{Metodo: "GET", Percorso: "/pagina", SoloGo: "D-44", Atteso: map[string]string{"codice": "200"}},
		{Metodo: "POST", Percorso: "/azione", Corpo: "a=1&mostra={mostra}", SoloGo: "D-44", Atteso: map[string]string{"codice": "200"}},
	}}
	rap := eseguiScenariCon([]scenario{sc}, nil, istanza{"standalone", sa.URL, sa.Client()}, istanza{"go", sb.URL, sb.Client()}, credenziali{}, "")
	if !rap.ok() || rap.Passi != 3 || a.Load() != 1 || b.Load() != 2 {
		t.Fatalf("rapporto = %+v, richieste %d %d", rap, a.Load(), b.Load())
	}
	if c, _ := corpo.Load().(string); c != "a=1&mostra=SEGNO-3_x" {
		t.Errorf("corpo %q", c)
	}
	sc.Passi[0].Atteso = map[string]string{"codice": "404"}
	sc.Passi = append(sc.Passi, sc.Passi[2]) // dopo un POST senza pagina: niente segno
	rap = eseguiScenariCon([]scenario{sc}, nil, istanza{"standalone", sa.URL, sa.Client()}, istanza{"go", sb.URL, sb.Client()}, credenziali{}, "")
	if c, _ := corpo.Load().(string); c != "a=1&mostra=" {
		t.Errorf("corpo senza segno %q", c)
	}
	if rap.Inattese != 1 || len(rap.Errori) != 0 ||
		!strings.Contains(strings.Join(rap.Righe, "\n"), `atteso codice=404 (solo standalone, D-44): standalone "200", go "(non eseguito)"`) {
		t.Errorf("rossi: %+v", rap)
	}
	for nome, src := range map[string]string{
		"solo_standalone senza D-xx":    `{"nome":"x","passi":[{"metodo":"GET","percorso":"/","solo_standalone":"x","atteso":{"codice":"200"}}]}`,
		"solo_standalone con confronta": `{"nome":"x","passi":[{"metodo":"GET","percorso":"/","solo_standalone":"D-44","confronta":["codice"],"atteso":{"codice":"200"}}]}`,
		"solo_standalone senza attese":  `{"nome":"x","passi":[{"metodo":"GET","percorso":"/","solo_standalone":"D-44"}]}`,
		"solo_standalone con location":  `{"nome":"x","passi":[{"metodo":"GET","percorso":"{location}","solo_standalone":"D-44","atteso":{"codice":"200"}}]}`,
		"solo_standalone con mostra":    `{"nome":"x","passi":[{"metodo":"POST","percorso":"/","corpo":"m={mostra}","solo_standalone":"D-44","atteso":{"codice":"200"}}]}`,
		"tutti e due":                   `{"nome":"x","passi":[{"metodo":"GET","percorso":"/","solo_go":"D-44","solo_standalone":"D-44","atteso":{"codice":"200"}}]}`,
		"mostra in un passo comune":     `{"nome":"x","passi":[{"metodo":"POST","percorso":"/","corpo":"m={mostra}","confronta":["codice"]}]}`,
	} {
		if _, err := leggiScenario("x.json", strings.NewReader(src)); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
	if d := deviazioniSoloGo([]scenario{{Passi: []passo{{SoloStandalone: "D-46"}}}}); !slices.Equal(d, []string{"D-46"}) {
		t.Errorf("deviazioni %v", d)
	}
}

// inventario: RIP-S-01
// Dopo un passo solo_go in errore i segnaposti non vengono da una risposta vecchia: il passo dopo
// con {location} è un errore anche se un passo prima del guasto aveva una Location (review 037b).
func TestSoloGoDopoUnErrore(t *testing.T) {
	var a, b atomic.Int32
	sa, sb := goConSegno(&a), goConSegno(&b)
	defer sa.Close()
	defer sb.Close()
	sc := scenario{Nome: "x", Passi: []passo{
		{Metodo: "POST", Percorso: "/azione", Corpo: "x=1", SoloGo: "D-44", Atteso: map[string]string{"codice": "302"}},
		{Metodo: "GET", Percorso: "{href:nessun-link}", SoloGo: "D-44", Atteso: map[string]string{"codice": "200"}},
		{Metodo: "GET", Percorso: "{location}", SoloGo: "D-44", Atteso: map[string]string{"codice": "200"}},
	}}
	rap := eseguiScenariCon([]scenario{sc}, nil, istanza{"standalone", sa.URL, sa.Client()}, istanza{"go", sb.URL, sb.Client()}, credenziali{}, "")
	if len(rap.Errori) != 2 || b.Load() != 1 {
		t.Errorf("errori %v, richieste al Go %d", rap.Errori, b.Load())
	}
}
