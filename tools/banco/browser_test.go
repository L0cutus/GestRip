package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const scenarioBrowserProva = `{
  "nome": "prova",
  "inventario": ["RIP-S-28"],
  "prova_con": ["sintetici-import"],
  "atteso": {"desktop": {"contatore": "(assente)"}},
  "atteso_dati": {"sintetici-import": {"desktop": {"contatore": "1 / 3"}, "telefono": {"contatore": "1 / 3"}}},
  "da_decidere": [{"campo": "anteprima", "standalone": "true", "go": "false", "motivo": "il Go non ha l'anteprima: domanda aperta"}]
}`

func TestLeggiScenarioBrowser(t *testing.T) {
	s, err := leggiScenarioBrowser("prova.json", strings.NewReader(scenarioBrowserProva))
	if err != nil {
		t.Fatal(err)
	}
	if s.Nome != "prova" || !slices.Equal(s.Inventario, []string{"RIP-S-28"}) || len(s.DaDecidere) != 1 ||
		!slices.Equal(s.ProvaCon, []string{datiSinteticiImport}) || s.DaDecidere[0].Standalone != "true" || s.DaDecidere[0].Go != "false" {
		t.Errorf("scenario = %+v", s)
	}
	if got := attesePerBrowser(s, "desktop", ""); got["contatore"] != "(assente)" || len(got) != 1 {
		t.Errorf("attese senza dati = %v", got)
	}
	if got := attesePerBrowser(s, "desktop", datiSinteticiImport); got["contatore"] != "1 / 3" {
		t.Errorf("attese con i dati = %v", got)
	}
	if got := attesePerBrowser(s, "telefono", ""); len(got) != 0 {
		t.Errorf("attese del telefono senza dati = %v", got)
	}
}

func TestLeggiScenarioBrowserRifiuta(t *testing.T) {
	casi := map[string]string{
		"campo sconosciuto":                    `{"nome":"prova","inventario":[],"passi":[]}`,
		"nome diverso":                         `{"nome":"altro","inventario":[]}`,
		"senza nome":                           `{"inventario":[]}`,
		"vista sconosciuta":                    `{"nome":"prova","atteso":{"tablet":{"a":"b"}}}`,
		"voce malformata":                      `{"nome":"prova","inventario":["rip-s-28"]}`,
		"dati sconosciuti":                     `{"nome":"prova","atteso_dati":{"altri":{"desktop":{"a":"b"}}}}`,
		"campo non valido":                     `{"nome":"prova","atteso":{"desktop":{"A B":"b"}}}`,
		"da decidere vuoto":                    `{"nome":"prova","da_decidere":[{"campo":"anteprima","standalone":"a","go":"b","motivo":""}]}`,
		"da decidere doppio":                   `{"nome":"prova","da_decidere":[{"campo":"a","standalone":"a","go":"b","motivo":"x"},{"campo":"a","standalone":"a","go":"b","motivo":"y"}]}`,
		"atteso da decidere":                   `{"nome":"prova","atteso":{"desktop":{"a":"b"}},"da_decidere":[{"campo":"a","standalone":"a","go":"b","motivo":"x"}]}`,
		"da decidere con attesa solo nei dati": `{"nome":"prova","atteso_dati":{"sintetici-import":{"telefono":{"a":"b"}}},"da_decidere":[{"campo":"a","standalone":"a","go":"b","motivo":"x"}]}`,
		"da decidere senza coppia":             `{"nome":"prova","da_decidere":[{"campo":"a","motivo":"x"}]}`,
		"da decidere coppia uguale":            `{"nome":"prova","da_decidere":[{"campo":"a","standalone":"b","go":"b","motivo":"x"}]}`,
		"prova_con sconosciuto":                `{"nome":"prova","prova_con":["altri"]}`,
		"prova_con vuoto":                      `{"nome":"prova","prova_con":[]}`,
	}
	for nome, j := range casi {
		if _, err := leggiScenarioBrowser("prova.json", strings.NewReader(j)); err == nil {
			t.Errorf("%s: accettato %s", nome, j)
		}
	}
}

func osservazioniProva(sa, gox map[string]map[string]string) uscitaBrowser {
	return uscitaBrowser{Osservazioni: map[string]map[string]map[string]map[string]string{
		"standalone": {"prova": sa}, "go": {"prova": gox}}}
}

func vistaUguale(campi map[string]string) map[string]map[string]string {
	return map[string]map[string]string{"desktop": campi, "telefono": campi}
}

func TestConfrontaBrowser(t *testing.T) {
	s, err := leggiScenarioBrowser("prova.json", strings.NewReader(scenarioBrowserProva))
	if err != nil {
		t.Fatal(err)
	}
	ss := []scenarioBrowser{s}
	base := map[string]string{"contatore": "1 / 3", "anteprima": "true", "esc": "#repair-photos"}
	conGo := func(c, v string) map[string]string {
		m := map[string]string{}
		for k, x := range base {
			m[k] = x
		}
		m[c] = v
		return m
	}

	t.Run("uguali con le attese, solo la differenza da decidere", func(t *testing.T) {
		rap := confrontaBrowser(ss, osservazioniProva(vistaUguale(base), vistaUguale(conGo("anteprima", "false"))), nil, datiSinteticiImport)
		if !rap.ok() || rap.Inattese != 0 || rap.DaDecidere != 2 || rap.Passi != 2 || rap.Scenari != 1 {
			t.Fatalf("rapporto = %+v", rap)
		}
		if rap.Verdi["prova"] {
			t.Error("uno scenario con una differenza da decidere non è verde per la copertura")
		}
		if !strings.Contains(strings.Join(rap.Righe, "\n"), "DA DECIDERE") {
			t.Errorf("righe = %v", rap.Righe)
		}
	})
	t.Run("differenza inattesa", func(t *testing.T) {
		gox := vistaUguale(conGo("anteprima", "false"))
		gox["telefono"] = conGo("esc", "#altro")
		rap := confrontaBrowser(ss, osservazioniProva(vistaUguale(base), gox), nil, datiSinteticiImport)
		if rap.ok() || rap.Inattese != 1 || rap.DaDecidere != 1 || rap.Verdi["prova"] {
			t.Fatalf("rapporto = %+v", rap)
		}
		righe := strings.Join(rap.Righe, "\n")
		if !strings.Contains(righe, `INATTESA prova[1] esc: standalone "#repair-photos", go "#altro"`) {
			t.Errorf("righe = %s", righe)
		}
	})
	t.Run("un campo che manca da una parte", func(t *testing.T) {
		gox := vistaUguale(conGo("anteprima", "false"))
		gox["desktop"] = map[string]string{"contatore": "1 / 3", "anteprima": "false"}
		rap := confrontaBrowser(ss, osservazioniProva(vistaUguale(base), gox), nil, datiSinteticiImport)
		if rap.ok() || !strings.Contains(strings.Join(rap.Righe, "\n"), `esc: standalone "#repair-photos", go "(assente)"`) {
			t.Fatalf("rapporto = %+v", rap)
		}
	})
	t.Run("deviazione approvata", func(t *testing.T) {
		gox := vistaUguale(conGo("anteprima", "false"))
		gox["desktop"] = conGo("esc", "#altro")
		gox["desktop"]["anteprima"] = "false"
		devs := deviazioni{{ID: "D-99", Livello: "browser", Scenario: "prova", Passo: 0, Campo: "esc", Standalone: "#repair-photos", Go: "#altro", Motivo: "prova"}}
		rap := confrontaBrowser(ss, osservazioniProva(vistaUguale(base), gox), devs, datiSinteticiImport)
		if !rap.ok() || rap.Attese != 1 || rap.Inattese != 0 {
			t.Fatalf("rapporto = %+v", rap)
		}
		// Una D-xx del livello HTTP non copre il browser.
		devs[0].Livello = ""
		if rap := confrontaBrowser(ss, osservazioniProva(vistaUguale(base), gox), devs, datiSinteticiImport); rap.ok() {
			t.Fatalf("deviazione HTTP usata dal browser: %+v", rap)
		}
	})
	t.Run("attesa mancata dalle due parti", func(t *testing.T) {
		m := conGo("contatore", "2 / 3")
		rap := confrontaBrowser(ss, osservazioniProva(vistaUguale(m), vistaUguale(conGo("contatore", "2 / 3"))), nil, datiSinteticiImport)
		if rap.ok() || rap.Inattese != 2 {
			t.Fatalf("rapporto = %+v", rap)
		}
	})
	t.Run("attesa mancata da una parte sola", func(t *testing.T) {
		gox := vistaUguale(conGo("anteprima", "false"))
		gox["desktop"] = conGo("contatore", "2 / 3")
		gox["desktop"]["anteprima"] = "false"
		rap := confrontaBrowser(ss, osservazioniProva(vistaUguale(base), gox), nil, datiSinteticiImport)
		righe := strings.Join(rap.Righe, "\n")
		if rap.ok() || rap.Inattese != 2 || !strings.Contains(righe, `INATTESA prova[0] atteso contatore=1 / 3: standalone "1 / 3", go "2 / 3"`) {
			t.Fatalf("l'attesa va controllata su ognuna delle due app: %+v", rap)
		}
	})
	t.Run("le attese seguono i dati", func(t *testing.T) {
		vuoto := map[string]string{"busta-1.errore": "Riparazione non trovata."}
		rap := confrontaBrowser(ss, osservazioniProva(vistaUguale(vuoto), vistaUguale(vuoto)), nil, "")
		// Sul database vuoto lo scenario non prova niente (prova_con): né verde né rosso.
		if !rap.ok() || rap.Verdi["prova"] || !slices.Equal(rap.NonProvati, []string{"prova"}) {
			t.Fatalf("senza dati: %+v", rap)
		}
		if _, c := rap.Verdi["prova"]; c {
			t.Errorf("uno scenario non provato non ha un esito: %+v", rap.Verdi)
		}
		rap = confrontaBrowser(ss, osservazioniProva(vistaUguale(vuoto), vistaUguale(vuoto)), nil, datiSinteticiImport)
		if rap.ok() {
			t.Fatalf("con i dati l'attesa del contatore manca: %+v", rap)
		}
	})
	t.Run("da decidere con un'altra coppia", func(t *testing.T) {
		rap := confrontaBrowser(ss, osservazioniProva(vistaUguale(base), vistaUguale(conGo("anteprima", "forse"))), nil, datiSinteticiImport)
		if rap.ok() || rap.Inattese != 2 || rap.DaDecidere != 0 ||
			!strings.Contains(strings.Join(rap.Righe, "\n"), `INATTESA prova[0] anteprima: standalone "true", go "forse"`) {
			t.Fatalf("una coppia diversa da quella fissata è inattesa: %+v", rap)
		}
	})
	t.Run("da decidere osservato da una parte sola", func(t *testing.T) {
		gox := vistaUguale(map[string]string{"contatore": "1 / 3", "esc": "#repair-photos"})
		rap := confrontaBrowser(ss, osservazioniProva(vistaUguale(base), gox), nil, datiSinteticiImport)
		if rap.ok() || rap.Inattese != 2 || len(rap.Errori) != 0 {
			t.Fatalf("un campo mancante da una parte è inatteso, e non è una voce da togliere: %+v", rap)
		}
	})
	t.Run("osservazioni vuote da una parte", func(t *testing.T) {
		gox := vistaUguale(conGo("anteprima", "false"))
		gox["telefono"] = map[string]string{}
		rap := confrontaBrowser(ss, osservazioniProva(vistaUguale(base), gox), nil, datiSinteticiImport)
		if rap.ok() || rap.Verdi["prova"] || rap.Passi != 1 {
			t.Fatalf("rapporto = %+v", rap)
		}
	})
	t.Run("errore generale", func(t *testing.T) {
		semplice := []scenarioBrowser{{Nome: "prova"}}
		u := osservazioniProva(vistaUguale(base), vistaUguale(base))
		if rap := confrontaBrowser(semplice, u, nil, datiSinteticiImport); !rap.ok() || !rap.Verdi["prova"] {
			t.Fatalf("senza errori: %+v", rap)
		}
		u.Errori = []string{"accesso desktop go: TimeoutError"}
		rap := confrontaBrowser(semplice, u, nil, datiSinteticiImport)
		if rap.ok() || rap.Verdi["prova"] || !strings.Contains(strings.Join(rap.Errori, "\n"), "accesso desktop go") {
			t.Fatalf("un errore senza scenario è del giro e nessuno scenario è verde: %+v", rap)
		}
	})
	t.Run("da decidere ormai uguale", func(t *testing.T) {
		rap := confrontaBrowser(ss, osservazioniProva(vistaUguale(base), vistaUguale(base)), nil, datiSinteticiImport)
		if rap.ok() || !strings.Contains(strings.Join(rap.Errori, "\n"), "anteprima") {
			t.Fatalf("una voce da decidere che non differisce più va tolta: %+v", rap)
		}
	})
	t.Run("vista mancante", func(t *testing.T) {
		sa := vistaUguale(base)
		delete(sa, "telefono")
		rap := confrontaBrowser(ss, osservazioniProva(sa, vistaUguale(conGo("anteprima", "false"))), nil, datiSinteticiImport)
		if rap.ok() || rap.Verdi["prova"] || len(rap.Errori) == 0 {
			t.Fatalf("rapporto = %+v", rap)
		}
	})
	t.Run("errore del browser", func(t *testing.T) {
		u := osservazioniProva(vistaUguale(base), vistaUguale(conGo("anteprima", "false")))
		u.Errori = []string{"prova telefono go (fase tre): TimeoutError: locator.waitFor"}
		rap := confrontaBrowser(ss, u, nil, datiSinteticiImport)
		if rap.ok() || rap.Verdi["prova"] {
			t.Fatalf("rapporto = %+v", rap)
		}
	})
	t.Run("scenario osservato ma sconosciuto", func(t *testing.T) {
		u := osservazioniProva(vistaUguale(base), vistaUguale(conGo("anteprima", "false")))
		u.Osservazioni["go"]["altro"] = vistaUguale(base)
		if rap := confrontaBrowser(ss, u, nil, datiSinteticiImport); rap.ok() {
			t.Fatalf("rapporto = %+v", rap)
		}
	})
	t.Run("dati sconosciuti", func(t *testing.T) {
		rap := confrontaBrowser(ss, osservazioniProva(vistaUguale(base), vistaUguale(base)), nil, "altri")
		if rap.ok() || rap.Passi != 0 || len(rap.Errori) != 1 {
			t.Fatalf("rapporto = %+v", rap)
		}
	})
}

func TestLeggiUscitaBrowserStretta(t *testing.T) {
	if _, err := leggiUscitaBrowser(strings.NewReader(`{"osservazioni":{},"errori":[],"altro":1}`)); err == nil {
		t.Error("campo sconosciuto accettato")
	}
	if _, err := leggiUscitaBrowser(strings.NewReader(`{"osservazioni":{"safari":{}},"errori":[]}`)); err == nil {
		t.Error("app sconosciuta accettata")
	}
	u, err := leggiUscitaBrowser(strings.NewReader(`{"osservazioni":{"standalone":{"prova":{"desktop":{"a":"b"}}}},"errori":["x"]}`))
	if err != nil || u.Osservazioni["standalone"]["prova"]["desktop"]["a"] != "b" || len(u.Errori) != 1 {
		t.Errorf("uscita = %+v, %v", u, err)
	}
}

type esitoProva struct {
	Verde                 bool            `json:"verde"`
	Dati                  string          `json:"dati"`
	Scenari               map[string]bool `json:"scenari"`
	NonProvati            []string        `json:"non_provati"`
	InventarioControllato bool            `json:"inventario_controllato"`
	Browser               struct {
		Verde      bool   `json:"verde"`
		VerdeHTTP  bool   `json:"verde_http"`
		DaDecidere int    `json:"da_decidere"`
		Errore     string `json:"errore"`
	} `json:"browser"`
}

func leggiEsitoProva(t *testing.T, p string) (esitoProva, string) {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var e esitoProva
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	return e, string(b)
}

func TestUnisciEsito(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "esito.json")
	if err := os.WriteFile(p, []byte(`{"verde":true,"dati":"sintetici-import","scenari":{"healthz":true},"non_provati":["foto"],"inventario_controllato":true,"quando":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rap := rapporto{Passi: 2, Verdi: map[string]bool{"visualizzatore": true, "caricamento": false}, DaDecidere: 3, NonProvati: []string{"conferme"}}
	if err := unisciEsito(p, rap, datiSinteticiImport); err != nil {
		t.Fatal(err)
	}
	e, b := leggiEsitoProva(t, p)
	if !e.Verde || !e.Scenari["healthz"] || !e.Scenari["visualizzatore"] || e.Scenari["caricamento"] ||
		!e.InventarioControllato || !e.Browser.Verde || e.Browser.DaDecidere != 3 || !slices.Equal(e.NonProvati, []string{"conferme", "foto"}) {
		t.Errorf("esito = %s", b)
	}
	if _, c := e.Scenari["conferme"]; c {
		t.Errorf("scenario non provato con un esito: %s", b)
	}
	// Un browser rosso fa rosso tutto il banco; ripetere un giro con gli stessi nomi va bene.
	rap.Inattese = 1
	if err := unisciEsito(p, rap, datiSinteticiImport); err != nil {
		t.Fatalf("giro ripetuto con gli stessi nomi: %v", err)
	}
	if e, b := leggiEsitoProva(t, p); e.Verde || e.Browser.Verde || !e.Browser.VerdeHTTP {
		t.Errorf("esito = %s", b)
	}
	// Dopo un browser rosso, un browser verde rilegge il verdetto del livello HTTP, non quello di prima.
	rap.Inattese = 0
	if err := unisciEsito(p, rap, datiSinteticiImport); err != nil {
		t.Fatal(err)
	}
	if e, b := leggiEsitoProva(t, p); !e.Verde || !e.Browser.Verde {
		t.Errorf("esito = %s", b)
	}
	// Un nome già usato da uno scenario HTTP non si sovrascrive.
	if err := unisciEsito(p, rapporto{Passi: 1, Verdi: map[string]bool{"healthz": true}}, datiSinteticiImport); err == nil {
		t.Error("nome dello scenario HTTP sovrascritto")
	}
	if err := unisciEsito(p, rapporto{Passi: 1, NonProvati: []string{"foto"}}, datiSinteticiImport); err == nil {
		t.Error("nome di uno scenario HTTP non provato sovrascritto")
	}
	// Dati diversi da quelli del livello HTTP: i due livelli non hanno girato sulla stessa istanza.
	if err := unisciEsito(p, rap, ""); err == nil {
		t.Error("dati diversi accettati")
	}
	// Un livello HTTP rosso resta rosso anche con il browser verde.
	q := filepath.Join(dir, "rosso.json")
	if err := os.WriteFile(q, []byte(`{"verde":false,"dati":"account-seminati","scenari":{"healthz":false}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := unisciEsito(q, rapporto{Passi: 1, Verdi: map[string]bool{"visualizzatore": true}}, ""); err != nil {
		t.Fatal(err)
	}
	if e, b := leggiEsitoProva(t, q); e.Verde || e.Browser.VerdeHTTP {
		t.Errorf("esito = %s", b)
	}
	// Senza l'esito del livello HTTP non si inventa niente.
	if err := unisciEsito(filepath.Join(dir, "manca.json"), rap, datiSinteticiImport); err == nil {
		t.Error("esito HTTP mancante accettato")
	}
}

// Appena parte il browser l'esito dice «non eseguito» e non è verde: se il giro si ferma a metà,
// non resta un esito verde del solo livello HTTP.
func TestSegnaBrowserNonEseguito(t *testing.T) {
	p := filepath.Join(t.TempDir(), "esito.json")
	if err := os.WriteFile(p, []byte(`{"verde":true,"dati":"sintetici-import","scenari":{"healthz":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := segnaBrowserNonEseguito(p); err != nil {
		t.Fatal(err)
	}
	e, b := leggiEsitoProva(t, p)
	if e.Verde || e.Browser.Verde || e.Browser.Errore != "non eseguito" || !e.Browser.VerdeHTTP || !e.Scenari["healthz"] {
		t.Errorf("esito = %s", b)
	}
	// Il risultato vero lo sovrascrive.
	if err := unisciEsito(p, rapporto{Passi: 1, Verdi: map[string]bool{"visualizzatore": true}}, datiSinteticiImport); err != nil {
		t.Fatal(err)
	}
	if e, b := leggiEsitoProva(t, p); !e.Verde || !e.Browser.Verde || e.Browser.Errore != "" {
		t.Errorf("esito = %s", b)
	}
	if err := segnaBrowserNonEseguito(filepath.Join(t.TempDir(), "manca.json")); err == nil {
		t.Error("esito mancante accettato")
	}
}

func TestComandoBrowser(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GESTRIP_BANCO_RUN", dir)
	n, _ := nomiPer("t0")
	script := filepath.Join(dir, "banco.mjs")
	if err := os.WriteFile(script, []byte("//"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := comandoBrowser(n, script, []string{"visualizzatore", "conferme"}, datiSinteticiImport)
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Join(c, " ")
	for _, v := range []string{"run --rm", "--name grgo-t0-browser", "--network grgo-t0-net", "--label grgo=1",
		"--user 1000:1000", "--cap-drop ALL", "--security-opt no-new-privileges:true", "--read-only",
		"--security-opt seccomp=" + filepath.Join(n.dir, "chromium-seccomp.json"),
		"src=" + script + ",dst=/gate/banco.mjs,readonly",
		",dst=/banco/ca.crt,readonly", ",dst=/banco/cred/email,readonly", ",dst=/banco/cred/password,readonly",
		",dst=/banco/cred/dipendente_email,readonly", ",dst=/banco/cred/dipendente_password,readonly",
		"src=" + filepath.Join(n.dir, "esito", "browser") + ",dst=/banco/esito",
		"src=" + filepath.Join(n.dir, "fotocamere") + ",dst=/banco/fotocamere,readonly",
		"--pull never",
		"--env TEST_CA_FILE=/banco/ca.crt", "--env STANDALONE_URL=https://synthetic-t0:8443",
		"--env GO_URL=https://synthetic-t0-go:8443", "--env DATI=sintetici-import",
		"--env SCENARI=visualizzatore,conferme", "--env USCITA=/banco/esito/browser.json"} {
		if !strings.Contains(s, v) {
			t.Errorf("manca %q in %s", v, s)
		}
	}
	if !slices.Contains(suffissiContainer(), "-browser") {
		t.Error("banco giu non toglie il container del browser")
	}
	if c[len(c)-1] != immagineBrowser {
		t.Errorf("l'immagine non è l'ultimo argomento: %v", c)
	}
	for _, v := range []string{" -p ", "--publish", "DEBUG", "sh -c", "--privileged"} {
		if strings.Contains(s, v) {
			t.Errorf("comando non ammesso (%s): %s", v, s)
		}
	}
	// Dei segreti dell'istanza solo le credenziali sintetiche del titolare e del dipendente (lo
	// scenario sfondo, slice 044b): mai le chiavi dello standalone né altro.
	if n := strings.Count(s, "/secrets/"); n != 4 {
		t.Errorf("segreti montati %d, attesi 4 (email e password del titolare e del dipendente): %s", n, s)
	}
	if c, _ := comandoBrowser(n, script, nil, ""); !strings.Contains(strings.Join(c, " "), "--env DATI= ") {
		t.Errorf("senza dati: %v", c)
	}
	if _, err := comandoBrowser(n, "relativo.mjs", []string{"x"}, ""); err == nil {
		t.Error("script relativo accettato")
	}
	if _, err := comandoBrowser(n, script, []string{"x"}, "altri"); err == nil {
		t.Error("dati sconosciuti accettati")
	}
	if _, err := comandoBrowser(n, script, []string{"a,b"}, ""); err == nil {
		t.Error("nome di scenario con la virgola accettato")
	}
}

// inventario: RIP-S-14, RIP-S-28, RIP-S-29, RIP-S-32, RIP-H-40
// Gli scenari del livello browser (slice 040a e 040b): si leggono, i nomi non si sovrappongono a quelli
// HTTP e ogni voce che toccano ha le sue attese sui dati importati, scritte qui come per gli scenari
// HTTP. Riapri (RIP-S-14) ed Elimina foto (RIP-S-32): «Annulla» nella finestra di data-confirm non
// cambia niente, «OK» fa l'azione. Visualizzatore (RIP-S-28): miniatura, barra, frecce, Esc, dito sul
// telefono, una voce sola nella cronologia. Caricamento (RIP-S-29): invio automatico, «Foto caricata.»
// per 3 s, 3 foto, foto oltre 25 MiB, anteprima e barra di avanzamento (040b). Coda delle foto di
// «Nuova riparazione» (RIP-H-40): scelta, formato, 3 foto, «Rimuovi», due buste di fila con le foto
// che salgono mentre si compila la seconda, la barra, un 422 definitivo, una connessione caduta e
// «Riprova», la conferma prima di ricaricare, le foto di un modulo lasciato che non vanno a un'altra
// busta, l'avviso del QR e un salvataggio rifiutato con le foto che aspettano la busta giusta, l'avviso
// manomesso (chiave diversa: nessuna foto mandata), la prima foto come foto della busta. Dopo un
// caricamento dalla scheda nessuna GET della scheda come documento, nemmeno aprendo e chiudendo il
// visualizzatore e tornando indietro. Su database vuoto ogni busta è «non trovata» dalle due
// parti.
func TestScenariBrowserDelRepo(t *testing.T) {
	ss, err := leggiScenariBrowser(filepath.Join("..", "..", "bench", "browser"))
	if err != nil {
		t.Fatal(err)
	}
	http, err := leggiScenari(filepath.Join("..", "..", "bench", "scenari"))
	if err != nil {
		t.Fatal(err)
	}
	per := map[string]scenarioBrowser{}
	for _, s := range ss {
		per[s.Nome] = s
		for _, h := range http {
			if h.Nome == s.Nome {
				t.Errorf("scenario %q sia HTTP sia browser", s.Nome)
			}
		}
	}
	voci := map[string][]string{"visualizzatore": {"RIP-S-28"}, "conferme": {"RIP-S-14", "RIP-S-32"}, "caricamento": {"RIP-S-29"},
		"coda": {"RIP-H-40"}, "sfondo": {"RIP-H-40"}, "schede": {"RIP-H-40"}, "tentativi": {"RIP-H-40"},
		"firma-riquadro": {"RIP-S-36", "RIP-S-37", "RIP-S-38", "RIP-S-40"}, "pannello-qr": {"RIP-H-23", "RIP-H-24", "RIP-H-25", "RIP-H-26"},
		"lettore-qr": {"RIP-H-22", "RIP-H-27", "RIP-H-28"}, "riquadro-azioni": {"RIP-H-14", "RIP-H-15", "RIP-H-16", "RIP-H-20"},
		"galleria-home": {"RIP-H-13"}}
	if len(per) != len(voci) {
		t.Errorf("scenari browser = %d, attesi %d", len(per), len(voci))
	}
	for nome, v := range voci {
		if !slices.Equal(per[nome].Inventario, v) {
			t.Errorf("%s: inventario %v, atteso %v", nome, per[nome].Inventario, v)
		}
	}
	attese := map[string]map[string]string{
		"visualizzatore": {"apri.contatore": "1 / 3", "apri.cronologia": "1", "barra-avanti.contatore": "2 / 3",
			"tasto-destra-ultima.contatore": "3 / 3", "tasto-sinistra.contatore": "2 / 3", "esc.hash": "#repair-photos",
			"esc.aperte": "0", "esc.cronologia": "1", "indietro.percorso": "/repairs/1",
			"apri.foto-caricata": "false", "tasto-destra-2.foto-caricata": "true", "tasto-destra-2.foto": "/repairs/1/photos/4"},
		"conferme": {"riapri.annulla.finestra": "confirm: Riaprire questa busta chiusa? Il motivo resta nel registro.",
			"riapri.annulla.modulo": "1", "riapri.annulla.messaggio": "(nessuno)", "riapri.ok.stato": "In lavorazione",
			"riapri.ok.messaggio": "Busta riaperta.", "elimina.annulla.finestra": "confirm: Eliminare la foto?",
			"elimina.annulla.pulsante": "1", "elimina.ok.pulsante": "0", "elimina.ok.messaggio": "Foto eliminata."},
		"caricamento": {"una.messaggio": "Foto caricata.", "seconda.durata": "circa 3 s",
			"seconda.dopo.messaggio": "(nessuno)", "altro.dopo-4s.messaggio": "Foto eliminata.", "altro.sparito": "false",
			"finestre":              "confirm: Eliminare la foto?",
			"cinque.messaggi-visti": "Foto caricata.",
			"grande.errori":         "Foto oltre il limite di 25 MiB.", "grande.foto-in-piu": "0",
			"anteprima": "true", "avanzamento": "true", "una.get-scheda": "0", "seconda.get-scheda": "0",
			"visore.get-scheda": "0"},
		"coda": {"apri.sezione": "true", "formato.errori": "nota-040b.txt: Formato non supportato.",
			"cinque.conteggio": "0", "scelte.conteggio": "3",
			"scelte.anteprime": "Anteprima di uno-040b.png (blob) | Anteprima di rotta-040b.png (blob) | Anteprima di due-040b.png (blob)",
			"salva-1.striscia": "📷 Busta B1: 0/3 foto, N%", "salva-1.barre": "1",
			"esci.striscia":         "📷 Busta B1: 0/3 foto, N%",
			"seconda.striscia-dopo": "❌ Busta B1: 2/3 foto caricate, 1 rifiutata (foto rifiutata). ✕",
			"seconda.percorso-dopo": "/nuova",
			"riprova.striscia":      "❌ Busta B1: 2/3 foto caricate, 1 rifiutata (foto rifiutata). ✕",
			"chiudi.nascosta":       "true",
			"chiave.errori":         "Le foto scelte prima sono state tolte: scegli di nuovo quelle di questa busta.",
			"chiave.striscia":       "(nessuno)",
			"scheda-1.foto":         "Foto (2)", "scheda-1.artigiano": "Artigiano ⛔ | Artigiano ✓",
			"scheda-2.foto": "Foto (1)", "scheda-2.artigiano": "Artigiano ⛔",
			"rifiutate-dal-server": "1", "connessioni-cadute": "1",
			"rifiuto-qr.stato": "1 foto pronta per la busta.", "rifiuto-qr.pulsante": "Genera busta senza QR",
			"rifiuto-nome.errore": "Inserisci il nome del cliente.", "rifiuto-nome.stato": "1 foto pronta per la busta.",
			"rifiuto-nome.foto-mandate": "0", "rifiuto.striscia": "📷 Busta B4: 0/1 foto, N%", "rifiuto.foto-mandate": "1",
			"scheda-4.foto": "Foto (1)", "manomesso.foto-mandate": "0", "manomesso.striscia": "(nessuno)",
			"manomesso-dopo.errori": "(nessuno)", "manomesso-dopo.conteggio": "0", "numeri": "5"},
		// La coda in background (slice 044b, D-58): le foto salgono mentre le pagine cambiano, dopo una
		// ricarica a metà invio senza doppioni, ognuna alla sua busta, e non per un altro utente.
		"sfondo": {"naviga-scelte.conteggio": "4", "naviga.striscia": "📷 Busta B1: 0/4 foto, N%", "naviga.menu": "/audit",
			"naviga.fine": "(nessuno)", "naviga.foto": "Foto (4)", "ricarica.server": "201", "ricarica.fine": "(nessuno)",
			"ricarica.foto": "Foto (2)", "due-buste.foto-prima": "Foto (1)", "due-buste.foto-seconda": "Foto (2)",
			"altro-utente.dipendente-inviate": "0", "altro-utente.dipendente-striscia": "(nessuno)", "altro-utente.foto": "Foto (1)",
			"finestre": "(nessuno)", "numeri": "5", "naviga.server": "201", "scadenza.voci": "0", "scadenza.inviate": "0"},
		// Più schede: una duplicata non toglie le foto della prima; due schede, una sola manda.
		"schede": {"duplicata.seconda.errori": "(nessuno)", "duplicata.prima.stato": "2 foto pronte per la busta.",
			"duplicata.foto": "Foto (2)", "due-schede.invii": "2", "due-schede.doppi": "0", "due-schede.foto": "Foto (2)"},
		// Un errore fisso: 5 tentativi alle loro attese, poi fermo anche cambiando pagina, e «Riprova».
		// Le azioni del riquadro della lista (slice 056): menu, 📌, rapidi con la conferma, «← Indietro».
		"riquadro-azioni": {"menu.stato": "Preventivo", "menu.messaggio": "Stato aggiornato.", "menu.cambia": "0",
			"menu.ritorno.stato": "In lavorazione", "pin.etichetta": "Togli pin", "pin.messaggio": "(nessuno)",
			"pin.tolto.etichetta": "Pinna in cima", "pronta.modulo": "1", "pronta.indietro": "/?stato=in_lavorazione",
			"consegna.annulla.finestra": "confirm: Segnare la busta N come consegnata?", "consegna.annulla.pulsante": "1",
			"consegna.ok.dopo": "/?stato=pronta", "consegna.ok.messaggio": "Stato aggiornato.", "nuova.indietro": "/?stato=pronta"},
		// La galleria della home (slice 057): la foto apre il visualizzatore, «← Indietro» torna alla
		// galleria, le linguette passano dalla lista alla galleria.
		"galleria-home": {"apri.lista": "0", "apri.scelta": "page", "apri.memoria": "/?vista=galleria", "foto.aperte": "1",
			"indietro.dopo": "/?vista=galleria", "linguette.lista.dopo": "/", "linguette.galleria.dopo": "/?vista=galleria"},
		"tentativi": {"errore.tentativi": "5", "errore.attese": "rispettate", "errore.dopo-fermo": "0",
			"errore.striscia": "❌ Busta B1: 0/1 foto caricate, errore 503. Riprova ✕", "riprova.foto": "Foto (1)"},
	}
	for nome, campi := range attese {
		if !slices.Equal(per[nome].ProvaCon, []string{datiSinteticiImport}) {
			t.Errorf("%s: prova_con = %v: sul database vuoto non prova niente", nome, per[nome].ProvaCon)
		}
		for _, vista := range visteBrowser {
			// Nessun errore JavaScript, nessuna risposta 4xx o 5xx, niente fuori dall'origine, con i
			// due dati.
			for _, d := range []string{"", datiSinteticiImport} {
				got := attesePerBrowser(per[nome], vista, d)
				if got["pagina.errori-js"] != "(nessuno)" || got["pagina.risposte-errore"] != "(nessuno)" || got["pagina.fuori-origine"] != "0" {
					t.Errorf("%s %s %q: attese della pagina = %q %q %q", nome, vista, d,
						got["pagina.errori-js"], got["pagina.risposte-errore"], got["pagina.fuori-origine"])
				}
			}
			got := attesePerBrowser(per[nome], vista, datiSinteticiImport)
			for c, v := range campi {
				if got[c] != v {
					t.Errorf("%s %s: %s = %q, atteso %q", nome, vista, c, got[c], v)
				}
			}
			// Su database vuoto la busta non c'è dalle due parti.
			vuoto := attesePerBrowser(per[nome], vista, "")
			trovato := false
			for c, v := range vuoto {
				if strings.HasSuffix(c, ".errore") && v == "Riparazione non trovata." {
					trovato = true
				}
			}
			if !trovato {
				t.Errorf("%s %s: senza dati manca «Riparazione non trovata.»: %v", nome, vista, vuoto)
			}
		}
	}
	if got := attesePerBrowser(per["visualizzatore"], "telefono", datiSinteticiImport); got["dito-sinistra.contatore"] == "" || got["dito-corto.contatore"] == "" {
		t.Errorf("visualizzatore sul telefono senza le attese dello scorrimento: %v", got)
	}
	if len(per["caricamento"].SoloGo) != 1 || per["caricamento"].SoloGo[0].Campo != "pagina.risposte-errore" ||
		len(per["visualizzatore"].SoloGo)+len(per["conferme"].SoloGo)+len(per["coda"].SoloGo)+len(per["schede"].SoloGo) != 0 ||
		len(per["tentativi"].SoloGo) != 3 {
		t.Errorf("solo_go: solo pagina.risposte-errore di caricamento: %+v %+v %+v %+v", per["caricamento"].SoloGo,
			per["visualizzatore"].SoloGo, per["conferme"].SoloGo, per["coda"].SoloGo)
	}
	// Nello scenario sfondo sono solo del Go le foto salite e le finestre: lo standalone perde la coda
	// a ogni pagina caricata da capo (D-58).
	var soloSfondo []string
	for _, g := range per["sfondo"].SoloGo {
		soloSfondo = append(soloSfondo, g.Campo)
		if !strings.Contains(g.Motivo, "D-58") {
			t.Errorf("sfondo: solo_go %s senza D-58 nel motivo", g.Campo)
		}
	}
	if !slices.Equal(soloSfondo, []string{"naviga.foto", "ricarica.foto", "due-buste.foto-prima", "due-buste.foto-seconda", "altro-utente.foto", "finestre", "scadenza.voci"}) {
		t.Errorf("sfondo: solo_go %q", soloSfondo)
	}
	// Il «da decidere» di caricamento (domanda 18) è approvato come D-54 (Stefano, 03/11/2026) ed è
	// diventato una deviazione del livello browser, per desktop e telefono: nessun «da decidere» resta.
	for nome, sc := range per {
		if len(sc.DaDecidere) != 0 {
			t.Errorf("%s: da decidere %+v", nome, sc.DaDecidere)
		}
	}
	f, err := os.Open("../../bench/deviazioni.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }() // solo lettura
	devs, err := leggiDeviazioni(f)
	if err != nil {
		t.Fatal(err)
	}
	// D-58 e D-59 (slice 044b), uguali per desktop e telefono. D-54 (l'avviso che nello standalone
	// sparisce dopo 0,2 s con 3 foto) non si vede più: con 5 foto l'avviso resta dalle due parti.
	var browser []string
	for _, d := range devs.delLivello(livelloBrowser) {
		browser = append(browser, fmt.Sprintf("%s %s[%d] %s=%s/%s", d.ID, d.Scenario, d.Passo, d.Campo, d.Standalone, d.Go))
	}
	var deviazioniAttese []string
	// In «Nuova riparazione» (coda e sfondo) il Go dice «per pulsante» (slice 055d, punto 1 della 049).
	m2, m4, mp := "Massimo 2 foto per volta.", "Massimo 4 foto per volta.", "Massimo 4 foto per pulsante."
	for _, x := range [][5]string{
		{"D-59", "caricamento", "cinque.foto-in-piu", "2", "4"}, {"D-59", "caricamento", "cinque.errori-visti", m2, m4},
		{"D-59", "caricamento", "cinque.errori-finali", m2, m4}, {"D-59", "coda", "cinque.errori", m2, mp},
		// D-58 (slice 055d): il conto alla rovescia accanto a «Riprova».
		{"D-58", "coda", "salva-2.striscia", "❌ Busta B1: 2/3 foto caricate, 1 rifiutata (foto rifiutata). ✕ | ❌ Busta B2: 0/1 foto caricate, connessione assente. Riprova ✕",
			"❌ Busta B1: 2/3 foto caricate, 1 rifiutata (foto rifiutata). ✕ | ❌ Busta B2: 0/1 foto caricate, connessione assente. Riprova nuovo tentativo tra N s… ✕"},
		{"D-58", "coda", "esci.finestre", "beforeunload", "(nessuno)"}, {"D-58", "coda", "finestre", "beforeunload", "(nessuno)"},
		{"D-59", "sfondo", "cinque.errori", m2, mp}, {"D-59", "sfondo", "quattro.stato", "(nessuno)", "4 foto pronte per la busta."},
		{"D-59", "sfondo", "quattro.errori", m2, "(nessuno)"}, {"D-59", "sfondo", "quattro.conteggio", "0", "4"},
		{"D-59", "sfondo", "limite-tolte.errori", m2, "(nessuno)"},
		// D-64 (slice 053): il messaggio dopo il salvataggio, «Busta 0045/26 salvata».
		{"D-64", "coda", "salva-1.messaggio", "Riparazione B1 creata.", "Busta B1 salvata"},
		{"D-64", "coda", "chiave.messaggio", "Riparazione B3 creata.", "Busta B3 salvata"},
	} {
		for p := range 2 {
			deviazioniAttese = append(deviazioniAttese, fmt.Sprintf("%s %s[%d] %s=%s/%s", x[0], x[1], p, x[2], x[3], x[4]))
		}
	}
	if !slices.Equal(browser, deviazioniAttese) {
		t.Errorf("deviazioni del browser:\n%q\nattese\n%q", browser, deviazioniAttese)
	}
}

// Le D-xx usate dalle deviazioni e dai passi solo_go devono essere approvate nell'inventario; senza
// inventario nessuna è ammessa (la regola di confronta, condivisa con confronta-browser).
func TestControllaApprovate(t *testing.T) {
	inv := filepath.Join(t.TempDir(), "inv.md")
	if err := os.WriteFile(inv, []byte("| D-01 | approvata | x |\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ok, err := deviazioniApprovate(strings.NewReader("| D-01 | approvata | x |\n"))
	if err != nil || !ok["D-01"] {
		t.Fatalf("formato dell'inventario di prova non riconosciuto: %v %v", ok, err)
	}
	d01 := deviazioni{{ID: "D-01"}}
	d02 := deviazioni{{ID: "D-02"}}
	if err := controllaApprovate(d01, []string{"D-01"}, inv); err != nil {
		t.Errorf("approvate rifiutate: %v", err)
	}
	if err := controllaApprovate(d02, nil, inv); err == nil {
		t.Error("deviazione non approvata accettata")
	}
	if err := controllaApprovate(nil, []string{"D-02"}, inv); err == nil {
		t.Error("solo_go non approvato accettato")
	}
	if err := controllaApprovate(d01, nil, ""); err == nil {
		t.Error("deviazione senza inventario accettata")
	}
	if err := controllaApprovate(nil, []string{"D-01"}, ""); err == nil {
		t.Error("solo_go senza inventario accettato")
	}
	if err := controllaApprovate(nil, nil, ""); err != nil {
		t.Errorf("niente deviazioni e niente inventario: %v", err)
	}
}

// Una D-xx del livello browser lo dice («livello»: «browser»), nomina un campo del browser e la vista
// come passo (0 o 1); una D-xx del livello HTTP non accetta un campo in formato browser.
func TestDeviazioneDelBrowser(t *testing.T) {
	ds, err := leggiDeviazioni(strings.NewReader(`[{"id":"D-99","livello":"browser","scenario":"caricamento","passo":1,"campo":"tre.errori-finali","standalone":"(nessuno)","go":"x","motivo":"m"}]`))
	if err != nil || len(ds) != 1 || ds[0].Livello != "browser" {
		t.Fatalf("deviazione del browser rifiutata: %v", err)
	}
	for nome, j := range map[string]string{
		"campo non valido":        `[{"id":"D-99","livello":"browser","scenario":"x","passo":0,"campo":"Non Valido","standalone":"a","go":"b","motivo":"m"}]`,
		"vista oltre il telefono": `[{"id":"D-99","livello":"browser","scenario":"x","passo":2,"campo":"a","standalone":"a","go":"b","motivo":"m"}]`,
		"vista negativa":          `[{"id":"D-99","livello":"browser","scenario":"x","passo":-1,"campo":"a","standalone":"a","go":"b","motivo":"m"}]`,
		"campo browser in HTTP":   `[{"id":"D-99","scenario":"x","passo":0,"campo":"tre.errori-finali","standalone":"a","go":"b","motivo":"m"}]`,
		"livello sconosciuto":     `[{"id":"D-99","livello":"pdf","scenario":"x","passo":0,"campo":"codice","standalone":"a","go":"b","motivo":"m"}]`,
	} {
		if _, err := leggiDeviazioni(strings.NewReader(j)); err == nil {
			t.Errorf("%s: accettata", nome)
		}
	}
	if _, err := leggiDeviazioni(strings.NewReader(`[{"id":"D-99","scenario":"x","passo":7,"campo":"codice","standalone":"a","go":"b","motivo":"m"}]`)); err != nil {
		t.Errorf("deviazione HTTP rifiutata: %v", err)
	}
}

// L'impronta dell'immagine del browser: Dockerfile, package.json, lock ed entrypoint.sh, non lo
// script degli scenari (montato a ogni giro).
func TestImprontaImmagineBrowser(t *testing.T) {
	dir := t.TempDir()
	for _, f := range append(slices.Clone(fileImmagineBrowser), "banco.mjs") {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(f), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := improntaImmagineBrowser(dir)
	if err != nil || len(a) != 64 {
		t.Fatalf("impronta = %q, %v", a, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "banco.mjs"), []byte("altro"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, _ := improntaImmagineBrowser(dir); b != a {
		t.Error("lo script degli scenari cambia l'impronta dell'immagine")
	}
	for _, f := range fileImmagineBrowser {
		// Stessa lunghezza, contenuto diverso.
		if err := os.WriteFile(filepath.Join(dir, f), []byte(strings.ToUpper(f)), 0o600); err != nil {
			t.Fatal(err)
		}
		b, _ := improntaImmagineBrowser(dir)
		if b == a {
			t.Errorf("%s non cambia l'impronta", f)
		}
		a = b
	}
	if !slices.Equal(fileImmagineBrowser, []string{"Dockerfile", "package.json", "package-lock.json", "entrypoint.sh"}) {
		t.Errorf("file dell'immagine = %v", fileImmagineBrowser)
	}
	if err := os.Remove(filepath.Join(dir, "Dockerfile")); err != nil {
		t.Fatal(err)
	}
	if _, err := improntaImmagineBrowser(dir); err == nil {
		t.Error("Dockerfile mancante accettato")
	}
}

func TestLeggiScenariBrowserCartellaVuota(t *testing.T) {
	if _, err := leggiScenariBrowser(t.TempDir()); err == nil {
		t.Error("cartella senza scenari accettata")
	}
}

// solo_go: per un campo dello scenario l'attesa vale solo per il Go; il valore dello standalone si
// stampa come NOTA e non fa rosso né il banco né lo scenario. Altrove resta severo.
func TestCampoSoloGo(t *testing.T) {
	j := `{"nome":"prova","atteso":{"desktop":{"pagina.risposte-errore":"(nessuno)","a":"1"}},
	  "solo_go":[{"campo":"pagina.risposte-errore","motivo":"difetto dello standalone"}]}`
	s, err := leggiScenarioBrowser("prova.json", strings.NewReader(j))
	if err != nil {
		t.Fatal(err)
	}
	ss := []scenarioBrowser{s}
	sa := map[string]string{"pagina.risposte-errore": "500 GET /repairs/6/photos/16", "a": "1"}
	gox := map[string]string{"pagina.risposte-errore": "(nessuno)", "a": "1"}
	u := osservazioniProva(map[string]map[string]string{"desktop": sa, "telefono": gox}, vistaUguale(gox))
	rap := confrontaBrowser(ss, u, nil, "")
	righe := strings.Join(rap.Righe, "\n")
	if !rap.ok() || !rap.Verdi["prova"] || rap.Inattese != 0 ||
		!strings.Contains(righe, `NOTA standalone prova[0] pagina.risposte-errore: "500 GET /repairs/6/photos/16" (difetto dello standalone)`) {
		t.Fatalf("rapporto = %+v", rap)
	}
	// Il Go resta severo.
	gox2 := map[string]string{"pagina.risposte-errore": "500 GET /x", "a": "1"}
	rap = confrontaBrowser(ss, osservazioniProva(vistaUguale(gox), map[string]map[string]string{"desktop": gox2, "telefono": gox}), nil, "")
	if rap.ok() || rap.Verdi["prova"] {
		t.Fatalf("Go fuori attesa accettato: %+v", rap)
	}
	// Gli altri campi restano severi anche dalla parte dello standalone.
	sa2 := map[string]string{"pagina.risposte-errore": "(nessuno)", "a": "2"}
	if rap := confrontaBrowser(ss, osservazioniProva(map[string]map[string]string{"desktop": sa2, "telefono": gox}, vistaUguale(gox)), nil, ""); rap.ok() {
		t.Fatalf("altro campo dello standalone non controllato: %+v", rap)
	}
	for _, j := range []string{
		`{"nome":"prova","solo_go":[{"campo":"x","motivo":"m"}]}`,
		`{"nome":"prova","atteso":{"desktop":{"x":"1"}},"solo_go":[{"campo":"x","motivo":""}]}`,
		`{"nome":"prova","atteso":{"desktop":{"x":"1"}},"solo_go":[{"campo":"x","motivo":"m"}],"da_decidere":[{"campo":"x","standalone":"a","go":"b","motivo":"m"}]}`,
	} {
		if _, err := leggiScenarioBrowser("prova.json", strings.NewReader(j)); err == nil {
			t.Errorf("accettato %s", j)
		}
	}
}
