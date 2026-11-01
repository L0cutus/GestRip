package main

import (
	"encoding/pem"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Slice 029 (T3b): le attese di un passo possono dipendere dai dati con cui è partito il banco.
// Senza modalità valgono quelle di «atteso» (database con i soli due account seminati); con
// `-dati sintetici-import` quelle di «atteso_dati» le sostituiscono o si aggiungono.

func TestAttesoDatiLetto(t *testing.T) {
	s, err := leggiScenario("x.json", strings.NewReader(`{"nome":"x","passi":[{"metodo":"GET","percorso":"/",
		"confronta":["codice","testo:Invito"],"atteso":{"codice":"200","testo:Invito":"assente"},
		"atteso_dati":{"sintetici-import":{"testo:Invito":"presente","conteggio:Invito":"2"}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	p := s.Passi[0]
	if a := attesePer(p, ""); len(a) != 2 || a["codice"] != "200" || a["testo:Invito"] != "assente" {
		t.Errorf("attese senza dati = %v", a)
	}
	a := attesePer(p, datiSinteticiImport)
	if len(a) != 3 || a["codice"] != "200" || a["testo:Invito"] != "presente" || a["conteggio:Invito"] != "2" {
		t.Errorf("attese con i dati importati = %v", a)
	}
	// attesePer non tocca il passo: il giro dopo, senza dati, ritrova le attese di prima.
	if p.Atteso["testo:Invito"] != "assente" || len(p.Atteso) != 2 {
		t.Errorf("atteso modificato: %v", p.Atteso)
	}
}

func TestAttesoDatiRifiutato(t *testing.T) {
	casi := map[string]string{
		// La modalità di base è «atteso»: un doppione la renderebbe ambigua.
		"modalità vuota":     `{"":{"codice":"200"}}`,
		"account seminati":   `{"account-seminati":{"codice":"200"}}`,
		"modalità ignota":    `{"sintetici":{"codice":"200"}}`,
		"campo non valido":   `{"sintetici-import":{"colore":"x"}}`,
		"senza campi":        `{"sintetici-import":{}}`,
		"conteggio negativo": `{"sintetici-import":{"conteggio:Invito":"-1"}}`,
		"conteggio parola":   `{"sintetici-import":{"conteggio:Invito":"due"}}`,
	}
	for nome, ad := range casi {
		src := `{"nome":"x","passi":[{"metodo":"GET","percorso":"/","confronta":["codice"],"atteso_dati":` + ad + `}]}`
		if _, err := leggiScenario("x.json", strings.NewReader(src)); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
	if _, err := leggiScenario("x.json", strings.NewReader(`{"nome":"x","passi":[{"metodo":"GET","percorso":"/",
		"confronta":["codice"],"atteso":{"conteggio:x":"tre"}}]}`)); err == nil {
		t.Error("conteggio atteso non numerico accettato in atteso")
	}
}

func TestValoreConteggio(t *testing.T) {
	r := risposta{Corpo: "<td>Invito da consegnare</td><td>Attivo</td><td>Invito da consegnare</td>"}
	if v := r.valore("conteggio:Invito da consegnare"); v != "2" {
		t.Errorf("conteggio = %q", v)
	}
	if v := r.valore("conteggio:Disattivato"); v != "0" {
		t.Errorf("conteggio assente = %q", v)
	}
	if !campoValido("conteggio:Invito") || campoValido("conteggio:") {
		t.Error("campoValido conteggio:")
	}
}

// Le attese per i dati valgono su tutte e due le istanze, come «atteso», e solo con quei dati.
func TestEseguiConAttesoDati(t *testing.T) {
	corpo := "<td>Invito da consegnare</td><td>Invito da consegnare</td>"
	a, b := server(corpo, nil), server(corpo, nil)
	defer a.Close()
	defer b.Close()
	sc := scenario{Nome: "utenti", Passi: []passo{{Metodo: "GET", Percorso: "/", Confronta: []string{"codice", "testo:Invito da consegnare"},
		Atteso: map[string]string{"testo:Invito da consegnare": "assente"},
		AttesoDati: map[string]map[string]string{datiSinteticiImport: {"testo:Invito da consegnare": "presente",
			"conteggio:Invito da consegnare": "2"}}}}}
	ia, ib := istanza{"standalone", a.URL, a.Client()}, istanza{"go", b.URL, b.Client()}
	if rap := eseguiScenariCon([]scenario{sc}, nil, ia, ib, credenziali{}, ""); rap.Inattese != 1 ||
		!strings.Contains(strings.Join(rap.Righe, "\n"), "atteso testo:Invito da consegnare=assente") {
		t.Errorf("senza dati l'attesa resta «assente»: %+v", rap)
	}
	if rap := eseguiScenariCon([]scenario{sc}, nil, ia, ib, credenziali{}, datiSinteticiImport); rap.Inattese != 0 || rap.Passi != 1 {
		t.Errorf("con i dati importati: %+v", rap)
	}
	// Il conteggio è esatto: tre inviti da consegnare dove i dati ne implicano due è rosso.
	tre := server(corpo+"<td>Invito da consegnare</td>", nil)
	defer tre.Close()
	if rap := eseguiScenariCon([]scenario{sc}, nil, istanza{"standalone", tre.URL, tre.Client()}, istanza{"go", tre.URL, tre.Client()},
		credenziali{}, datiSinteticiImport); rap.Inattese != 1 ||
		!strings.Contains(strings.Join(rap.Righe, "\n"), `atteso conteggio:Invito da consegnare=2: standalone "3", go "3"`) {
		t.Errorf("conteggio diverso non trovato: %+v", rap)
	}
}

// Lo scenario utenti del repo: sul database vuoto nessun account in attesa; sui dati importati
// esattamente i due account attivi della cattura Flask sintetica (synthetic_capture.py a
// go-baseline: operatore.uno e operatore.due), che il legacy import scrive senza password.
func TestScenarioUtentiSuiDueDati(t *testing.T) {
	ss, err := leggiScenari(filepath.Join("..", "..", "bench", "scenari"))
	if err != nil {
		t.Fatal(err)
	}
	var p *passo
	for _, s := range ss {
		if s.Nome == "utenti" && len(s.Passi) > 2 {
			p = &s.Passi[2]
		}
	}
	if p == nil || p.Percorso != "/settings/utenti" {
		t.Fatalf("passo utenti[2] non trovato: %+v", p)
	}
	for _, c := range []string{"testo:Invito da consegnare", "conteggio:Invito da consegnare"} {
		if !strings.Contains(strings.Join(p.Confronta, "\n"), c) {
			t.Errorf("utenti[2] non confronta %s", c)
		}
	}
	vuoto := attesePer(*p, "")
	if vuoto["testo:Invito da consegnare"] != "assente" || vuoto["conteggio:Invito da consegnare"] != "0" || vuoto["codice"] != "200" ||
		vuoto["testo:Disattiva"] != "presente" {
		t.Errorf("attese sul database vuoto = %v", vuoto)
	}
	imp := attesePer(*p, datiSinteticiImport)
	if imp["testo:Invito da consegnare"] != "presente" || imp["conteggio:Invito da consegnare"] != "2" || imp["codice"] != "200" ||
		imp["testo:Disattiva"] != "presente" {
		t.Errorf("attese sui dati importati = %v", imp)
	}
	// Nessun altro passo del repo cambia attese con i dati, salvo le schede delle buste importate
	// (TestScenarioSchedaSuiDueDati), quelle del passo delle modifiche, con il loro modulo
	// (scenario modifica, slice 033), quelle del passo degli stati (scenario stati, slice 034,
	// TestScenarioStatiSuiDueDati), quelle del passo del link (scenario link, slice 035,
	// TestScenarioLinkSuiDueDati), le foto delle buste (scenari foto e foto-dipendente, slice 037,
	// TestScenarioFotoSuiDueDati), le schede mostrate dopo un'azione (scenario viste, slice 037b,
	// TestScenarioVisteSuiDueDati) e le due buste del passo dei numeri (scenario numeri, slice 037d,
	// TestScenarioNumeriSuiDueDati): cambia solo ciò che serve.
	for _, s := range ss {
		for i, q := range s.Passi {
			if len(q.AttesoDati) > 0 && (s.Nome != "utenti" || i != 2) &&
				(s.Nome != "scheda" && s.Nome != "modifica" && s.Nome != "stati" && s.Nome != "link" && s.Nome != "foto" && s.Nome != "foto-dipendente" && s.Nome != "viste" && s.Nome != "numeri" ||
					!strings.HasPrefix(q.Percorso, "/repairs/") && (s.Nome != "viste" || q.Percorso != "{location}")) &&
				(s.Nome != "registro" || !strings.HasPrefix(q.Percorso, "/audit")) {
				t.Errorf("%s[%d] ha atteso_dati", s.Nome, i)
			}
		}
	}
}

// Lo scenario scheda (slice 032): sul database vuoto ogni /repairs/N torna alla home (busta non
// trovata); sui dati importati la scheda si apre, con il numero e il cliente della busta della
// cattura sintetica (N/26, «Cliente sintetico N»: gli id sono gli stessi nei due lati) e lo
// stato che il legacy import e i passi della catena le danno.
func TestScenarioSchedaSuiDueDati(t *testing.T) {
	ss, err := leggiScenari(filepath.Join("..", "..", "bench", "scenari"))
	if err != nil {
		t.Fatal(err)
	}
	stati := map[string]string{"/repairs/1": "In lavorazione", "/repairs/2": "Consegnata", "/repairs/3": "Consegnata", "/repairs/4": "In lavorazione"}
	visti := map[string]int{}
	for _, s := range ss {
		if s.Nome != "scheda" {
			continue
		}
		for i, q := range s.Passi {
			if len(q.AttesoDati) == 0 {
				continue
			}
			n := strings.TrimPrefix(q.Percorso, "/repairs/")
			vuoto, imp := attesePer(q, ""), attesePer(q, datiSinteticiImport)
			if vuoto["codice"] != "302" || vuoto["intestazione:location"] != "/" {
				t.Errorf("scheda[%d] sul database vuoto = %v", i, vuoto)
			}
			if imp["codice"] != "200" || imp["intestazione:location"] != "" || imp["testo:Busta #"+n+"/26 · Laboratorio"] != "presente" ||
				imp["testo:Cliente sintetico "+n] != "presente" || imp["visibile:repair-status"] != stati[q.Percorso] {
				t.Errorf("scheda[%d] sui dati importati = %v", i, imp)
			}
			for c := range imp {
				if !slices.Contains(q.Confronta, c) && c != "codice" {
					t.Errorf("scheda[%d]: attesa su %s che il passo non confronta", i, c)
				}
			}
			visti[q.Percorso]++
		}
	}
	for p := range stati {
		if visti[p] == 0 {
			t.Errorf("nessun passo apre %s", p)
		}
	}
}

// Lo scenario stati (slice 034): sul database vuoto ogni /repairs/N torna alla home; sui dati
// importati la scheda si apre con lo stato che le hanno dato i passi della catena, e le date che
// dipendono dal giorno del giro (pronta, consegna, «dal» dell'attesa) non hanno un valore atteso.
// Ogni busta del passo degli stati ha almeno un passo, e ogni attesa è su un campo confrontato.
func TestScenarioStatiSuiDueDati(t *testing.T) {
	ss, err := leggiScenari(filepath.Join("..", "..", "bench", "scenari"))
	if err != nil {
		t.Fatal(err)
	}
	stati := map[string]string{"/repairs/11": "In attesa materiali", "/repairs/12": "Annullata", "/repairs/13": "Chiusa",
		"/repairs/14": "Consegnata", "/repairs/15": "Pronta", "/repairs/2": "Consegnata", "/repairs/6": "Pronta",
		"/repairs/7": "Valutata", "/repairs/10": "Preventivo"}
	visti := map[string]int{}
	for _, s := range ss {
		if s.Nome != "stati" {
			continue
		}
		for i, q := range s.Passi {
			if len(q.AttesoDati) == 0 {
				continue
			}
			vuoto, imp := attesePer(q, ""), attesePer(q, datiSinteticiImport)
			if vuoto["codice"] != "302" || vuoto["intestazione:location"] != "/" {
				t.Errorf("stati[%d] sul database vuoto = %v", i, vuoto)
			}
			if imp["codice"] != "200" || imp["intestazione:location"] != "" {
				t.Errorf("stati[%d] sui dati importati = %v", i, imp)
			}
			if st, ok := imp["visibile:repair-status"]; ok && st != stati[q.Percorso] {
				t.Errorf("stati[%d]: stato %q, atteso %q", i, st, stati[q.Percorso])
			}
			for c := range imp {
				if !slices.Contains(q.Confronta, c) && c != "codice" {
					t.Errorf("stati[%d]: attesa su %s che il passo non confronta", i, c)
				}
				for _, d := range []string{"visibile:repair-ready-on", "visibile:repair-delivered-on", "visibile:repair-waiting-since", "visibile:repair-waiting"} {
					if c == d {
						t.Errorf("stati[%d]: attesa su %s, che dipende dal giorno del giro", i, c)
					}
				}
			}
			visti[q.Percorso]++
		}
	}
	for p := range stati {
		if visti[p] == 0 {
			t.Errorf("nessun passo apre %s", p)
		}
	}
}

// Lo scenario registro (slice 034) prova la paginazione davvero: sui dati importati le aperture
// della scheda scritte dal passo degli stati sono più di 100 righe repair.viewed, uguali dalle due
// parti, quindi pagina 1 ha «Successiva» e l'ultima «Precedente»; sul database vuoto nessuna delle
// due. Anche il registro intero, sui dati importati, ha «Successiva» a pagina 1 e «Precedente»
// sull'ultima.
func TestScenarioRegistroPaginazione(t *testing.T) {
	ss, err := leggiScenari(filepath.Join("..", "..", "bench", "scenari"))
	if err != nil {
		t.Fatal(err)
	}
	voluti := map[string][2]string{
		"/audit": {"testo:Successiva", "testo:Precedente"},
		"/audit?giorni=7&pagina=99999999999999999999&fino_a=abc":                      {"testo:Precedente", "testo:Successiva"},
		"/audit?action=repair.viewed":                                                 {"testo:Successiva", "testo:Precedente"},
		"/audit?giorni=7&pagina=99999999999999999999&fino_a=abc&action=repair.viewed": {"testo:Precedente", "testo:Successiva"},
	}
	visti := map[string]bool{}
	for _, s := range ss {
		if s.Nome != "registro" {
			continue
		}
		for i, q := range s.Passi {
			v, ok := voluti[q.Percorso]
			if !ok || visti[q.Percorso] {
				continue
			}
			visti[q.Percorso] = true
			presente, assente := v[0], v[1]
			imp, vuoto := attesePer(q, datiSinteticiImport), attesePer(q, "")
			if imp[presente] != "presente" || imp[assente] != "assente" || vuoto[presente] != "assente" || vuoto[assente] != "assente" {
				t.Errorf("registro[%d] %s: importati %v, vuoto %v", i, q.Percorso, imp, vuoto)
			}
			for _, c := range []string{presente, assente} {
				if !slices.Contains(q.Confronta, c) {
					t.Errorf("registro[%d] non confronta %s", i, c)
				}
			}
			if strings.Contains(q.Percorso, "repair.viewed") && !slices.Contains(q.Confronta, "visibile:audit-total") {
				t.Errorf("registro[%d] non confronta il totale", i)
			}
		}
	}
	for p := range voluti {
		if !visti[p] {
			t.Errorf("nessun passo %s", p)
		}
	}
}

// esegui legge la modalità dei dati dal runtime.json dell'istanza, scritto da su.
func TestDatiIstanza(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GESTRIP_BANCO_RUN", dir)
	n, _ := nomiPer("t0")
	if _, err := datiIstanza(n); err == nil {
		t.Error("istanza senza runtime.json accettata")
	}
	if err := os.Mkdir(n.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for rt, voluto := range map[string]string{
		`{"sintetica":true,"dati":"account-seminati"}`: "",
		`{"sintetica":true,"dati":"sintetici-import"}`: datiSinteticiImport,
	} {
		if err := os.WriteFile(filepath.Join(n.dir, "runtime.json"), []byte(rt), 0o600); err != nil {
			t.Fatal(err)
		}
		if d, err := datiIstanza(n); err != nil || d != voluto {
			t.Errorf("%s: dati %q (%v), voluti %q", rt, d, err, voluto)
		}
	}
	for _, rt := range []string{`{"sintetica":true}`, `{"sintetica":true,"dati":"altro"}`, `{"sintetica":false,"dati":"account-seminati"}`, `non json`} {
		if err := os.WriteFile(filepath.Join(n.dir, "runtime.json"), []byte(rt), 0o600); err != nil {
			t.Fatal(err)
		}
		if d, err := datiIstanza(n); err == nil {
			t.Errorf("%s accettato: %q", rt, d)
		}
	}
}

// Con i dati importati confronta riceve -dati sintetici-import; senza, nessun -dati.
func TestComandiEseguiPassanoIDati(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GESTRIP_BANCO_RUN", dir)
	n, _ := nomiPer("t0")
	for _, f := range []string{"banco", "dev.json"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("[]"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "scen"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, dati := range []string{"", datiSinteticiImport} {
		cmds, err := comandiEsegui(n, filepath.Join(dir, "banco"), filepath.Join(dir, "scen"), filepath.Join(dir, "dev.json"), "", dati)
		if err != nil {
			t.Fatal(err)
		}
		conf := cmds[1]
		i := indice(conf, immagineBase)
		fs := flag.NewFlagSet("x", flag.ContinueOnError)
		for _, k := range []string{"standalone", "go", "ca", "scenari", "deviazioni", "inventario", "esito", "credenziali"} {
			fs.String(k, "", "")
		}
		d := fs.String("dati", "", "")
		if err := fs.Parse(conf[i+3:]); err != nil || fs.NArg() != 0 {
			t.Fatalf("argomenti di confronta non validi: %v (avanzano %v)", err, fs.Args())
		}
		if *d != dati {
			t.Errorf("dati %q: confronta riceve -dati %q", dati, *d)
		}
		if dati == "" && strings.Contains(strings.Join(conf, " "), "-dati") {
			t.Errorf("senza dati c'è -dati: %v", conf)
		}
	}
	if _, err := comandiEsegui(n, filepath.Join(dir, "banco"), filepath.Join(dir, "scen"), filepath.Join(dir, "dev.json"), "", "altro"); err == nil {
		t.Error("modalità dei dati ignota accettata")
	}
}

// Correzioni della review (029).

// Un'attesa rispettata da una parte sola è rossa anche se il campo non è in «confronta»: con
// «atteso» e con «atteso_dati», sia con lo standalone conforme e il Go no, sia il contrario.
func TestAttesaRispettataDaUnaParteSola(t *testing.T) {
	con, senza := esito(200, "<p>Frase attesa</p>", nil), esito(200, "<p>altro</p>", nil)
	passi := map[string]struct {
		p    passo
		dati string
	}{
		"atteso": {passo{Confronta: []string{"codice"}, Atteso: map[string]string{"testo:Frase attesa": "presente"}}, ""},
		"atteso_dati": {passo{Confronta: []string{"codice"}, Atteso: map[string]string{"testo:Frase attesa": "assente"},
			AttesoDati: map[string]map[string]string{datiSinteticiImport: {"testo:Frase attesa": "presente"}}}, datiSinteticiImport},
	}
	for nome, c := range passi {
		for verso, ab := range map[string][2]risposta{"standalone conforme": {con, senza}, "go conforme": {senza, con}} {
			d := confrontaCon(c.p, c.dati, ab[0], ab[1])
			if len(d) != 1 || d[0].Campo != "atteso testo:Frase attesa=presente" {
				t.Errorf("%s, %s: differenze %v", nome, verso, d)
			}
		}
		if d := confrontaCon(c.p, c.dati, con, con); len(d) != 0 {
			t.Errorf("%s: tutte e due conformi, differenze %v", nome, d)
		}
	}
}

func TestConteggioDistingueLeMaiuscole(t *testing.T) {
	r := risposta{Corpo: "Invito da consegnare; invito da consegnare; INVITO DA CONSEGNARE"}
	if v := r.valore("conteggio:Invito da consegnare"); v != "1" {
		t.Errorf("conteggio = %q, voluto 1", v)
	}
}

// Una frase con un carattere che l'HTML scappa non compare mai nel corpo: «assente» o «0»
// passerebbero sempre. Le virgolette valgono solo in un attributo intero (href="/x"), come negli
// scenari dei gusci.
func TestFrasiConCaratteriScappati(t *testing.T) {
	for _, c := range []string{"testo:l'invito", "testo:A & B", "testo:<td>", "testo:a > b", `testo:clicca "Salva"`,
		"conteggio:l'invito", "conteggio:A & B", "conteggio:<td>", `conteggio:"x"`, `testo:href="/x" id="y"`} {
		if campoValido(c) {
			t.Errorf("%s accettato", c)
		}
	}
	for _, c := range []string{`testo:href="/settings"`, `testo:data-theme-choose="notte"`, `testo:content="#1c1409"`,
		`testo:aria-labelledby="settings-tab-utenti"`, "testo:Il link può essere scaduto o già usato.", "testo:@", "conteggio:Invito da consegnare"} {
		if !campoValido(c) {
			t.Errorf("%s rifiutato", c)
		}
	}
	if _, err := leggiScenari(filepath.Join("..", "..", "bench", "scenari")); err != nil {
		t.Errorf("scenari del repo: %v", err)
	}
}

func TestConteggioNonCanonico(t *testing.T) {
	for _, v := range []string{"02", "+2", " 2", "2.0"} {
		for nome, campo := range map[string]string{
			"atteso":      `"atteso":{"conteggio:x":"` + v + `"}`,
			"atteso_dati": `"atteso_dati":{"sintetici-import":{"conteggio:x":"` + v + `"}}`,
		} {
			src := `{"nome":"x","passi":[{"metodo":"GET","percorso":"/","confronta":["codice"],` + campo + `}]}`
			if _, err := leggiScenario("x.json", strings.NewReader(src)); err == nil {
				t.Errorf("%s: conteggio %q accettato", nome, v)
			}
		}
	}
}

// eseguiScenariCon non ripiega sulle attese di base con una modalità sconosciuta: è un errore e
// non parte nessuna richiesta.
func TestEseguiConDatiSconosciuti(t *testing.T) {
	richieste := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { richieste++ }))
	defer s.Close()
	sc := scenario{Nome: "x", Passi: []passo{{Metodo: "GET", Percorso: "/", Confronta: []string{"codice"}}}}
	for _, dati := range []string{"altro", "account-seminati"} {
		rap := eseguiScenariCon([]scenario{sc}, nil, istanza{"standalone", s.URL, s.Client()}, istanza{"go", s.URL, s.Client()}, credenziali{}, dati)
		if rap.ok() || len(rap.Errori) != 1 || !strings.Contains(rap.Errori[0], dati) || rap.Passi != 0 || richieste != 0 {
			t.Errorf("dati %q: %+v, richieste %d", dati, rap, richieste)
		}
	}
}

// esegui: la modalità viene dal runtime.json dell'istanza e arriva a confronta; senza runtime.json
// non parte niente.
func TestPreparaEseguiLeggeIDatiDellIstanza(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GESTRIP_BANCO_RUN", dir)
	n, _ := nomiPer("t0")
	for _, f := range []string{"banco", "dev.json"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("[]"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "scen"), 0o700); err != nil {
		t.Fatal(err)
	}
	prepara := func() ([][]string, error) {
		return preparaEsegui(n, filepath.Join(dir, "banco"), filepath.Join(dir, "scen"), filepath.Join(dir, "dev.json"), "")
	}
	if _, err := prepara(); err == nil {
		t.Error("istanza senza runtime.json accettata")
	}
	if err := os.Mkdir(n.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for dati, voluto := range map[string]bool{"sintetici-import": true, "account-seminati": false} {
		if err := os.WriteFile(filepath.Join(n.dir, "runtime.json"), []byte(`{"sintetica":true,"dati":"`+dati+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		cmds, err := prepara()
		if err != nil {
			t.Fatal(err)
		}
		if c := strings.Join(cmds[1], " "); strings.HasSuffix(c, " -dati sintetici-import") != voluto || strings.Contains(c, "-dati account") {
			t.Errorf("dati %s: confronta = %s", dati, c)
		}
	}
	if err := os.WriteFile(filepath.Join(n.dir, "runtime.json"), []byte(`{"sintetica":true,"dati":"altro"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepara(); err == nil {
		t.Error("runtime.json con dati sconosciuti accettato")
	}
}

// confronta da riga di comando, contro due server TLS: -dati sceglie le attese, un -dati
// sconosciuto si ferma prima di qualunque richiesta.
func TestCmdConfrontaConDati(t *testing.T) {
	richieste := 0
	corpo := "<td>Invito da consegnare</td><td>Invito da consegnare</td>"
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { richieste++; _, _ = w.Write([]byte(corpo)) })
	a, b := httptest.NewTLSServer(h), httptest.NewTLSServer(h)
	defer a.Close()
	defer b.Close()
	dir := t.TempDir()
	var ca []byte
	for _, s := range []*httptest.Server{a, b} {
		ca = append(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Certificate().Raw})...)
	}
	scen := filepath.Join(dir, "scen")
	if err := os.Mkdir(scen, 0o700); err != nil {
		t.Fatal(err)
	}
	for f, s := range map[string]string{
		"ca.crt":   string(ca),
		"dev.json": "[]",
		"scen/utenti.json": `{"nome":"utenti","passi":[{"metodo":"GET","percorso":"/","confronta":["codice"],
			"atteso":{"conteggio:Invito da consegnare":"0"},
			"atteso_dati":{"sintetici-import":{"conteggio:Invito da consegnare":"2"}}}]}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	arg := func(dati ...string) []string {
		return append([]string{"-standalone", a.URL, "-go", b.URL, "-ca", filepath.Join(dir, "ca.crt"),
			"-scenari", scen, "-deviazioni", filepath.Join(dir, "dev.json")}, dati...)
	}
	if err := cmdConfronta(arg("-dati", datiSinteticiImport)); err != nil {
		t.Errorf("con i dati importati: %v", err)
	}
	if err := cmdConfronta(arg()); err == nil {
		t.Error("senza -dati valgono le attese del database vuoto: doveva essere rosso")
	}
	prima := richieste
	err := cmdConfronta(arg("-dati", "altro"))
	if err == nil || !strings.Contains(err.Error(), `-dati "altro" non valido`) || richieste != prima {
		t.Errorf("-dati sconosciuto: %v, richieste %d", err, richieste-prima)
	}
}

// Lo scenario link (slice 035): sul database vuoto ogni /repairs/N torna alla home; sui dati
// importati ogni scheda ha un'attesa esplicita sullo stato del link: il testo intero dove non
// dipende dal giorno del giro, altrimenti il conteggio di «attivo fino al» (una volta) con la riga
// del vecchio link; il menu c'è per il titolare e manca al dipendente; il nuovo link mostrato una
// volta non c'è mai (lo mostra solo la risposta a un POST, che il banco non confronta). Le buste del
// passo del link hanno tutte un passo.
func TestScenarioLinkSuiDueDati(t *testing.T) {
	ss, err := leggiScenari(filepath.Join("..", "..", "bench", "scenari"))
	if err != nil {
		t.Fatal(err)
	}
	visti := map[string]int{}
	trovato := false
	for _, s := range ss {
		if s.Nome != "link" {
			continue
		}
		trovato = true
		dipendente := false
		for i, q := range s.Passi {
			if strings.Contains(q.Corpo, "{email_dipendente}") {
				dipendente = true
			}
			if len(q.AttesoDati) == 0 {
				continue
			}
			vuoto, imp := attesePer(q, ""), attesePer(q, datiSinteticiImport)
			if vuoto["codice"] != "302" || vuoto["intestazione:location"] != "/" {
				t.Errorf("link[%d] sul database vuoto = %v", i, vuoto)
			}
			if imp["codice"] != "200" || imp["intestazione:location"] != "" || imp["visibile:repair-public-link-new"] != "(assente)" ||
				imp["conteggio:mostrato una volta"] != "0" {
				t.Errorf("link[%d] sui dati importati = %v", i, imp)
			}
			if st, ok := imp["visibile:repair-public-link-state"]; ok {
				if !strings.HasPrefix(st, "🔗 Link ") || strings.Contains(st, "attivo fino al") {
					t.Errorf("link[%d]: stato %q", i, st)
				}
			} else if imp["conteggio:attivo fino al"] != "1" {
				t.Errorf("link[%d]: né lo stato né il conteggio di «attivo fino al»: %v", i, imp)
			}
			menu := imp["visibile:repair-public-link-menu/summary"]
			if dipendente && menu != "(assente)" || !dipendente && menu != "⋯" {
				t.Errorf("link[%d]: menu %q per il dipendente=%v", i, menu, dipendente)
			}
			for c := range imp {
				if !slices.Contains(q.Confronta, c) && c != "codice" {
					t.Errorf("link[%d]: attesa su %s che il passo non confronta", i, c)
				}
			}
			visti[q.Percorso]++
		}
		if !dipendente {
			t.Error("lo scenario link non prova il dipendente")
		}
	}
	if !trovato {
		t.Fatal("manca lo scenario link")
	}
	for _, n := range busteLink {
		if visti["/repairs/"+strconv.FormatInt(n, 10)] == 0 {
			t.Errorf("nessun passo apre la busta %d del passo del link", n)
		}
	}
}

// Gli scenari delle foto (slice 037): sul database vuoto ogni foto dopo l'accesso è il rifiuto
// «Foto non disponibile» (la busta non c'è); sui dati importati le foto della cattura (byte che non
// si decodificano) si servono intere con la loro impronta anche chiedendo la miniatura, e la foto
// del passo delle foto (la 4 della 1/26) intera 1000×750 e in miniatura WebP 400×300; i rifiuti
// restano rifiuti. Nessun passo dopo l'accesso è senza attesa.
func TestScenarioFotoSuiDueDati(t *testing.T) {
	ss, err := leggiScenari(filepath.Join("..", "..", "bench", "scenari"))
	if err != nil {
		t.Fatal(err)
	}
	visti := 0
	for _, s := range ss {
		if s.Nome != "foto" && s.Nome != "foto-dipendente" {
			continue
		}
		visti++
		dentro := false
		miniature := 0
		for i, p := range s.Passi {
			if p.Metodo == "POST" && p.Percorso == "/login" {
				dentro = true
				continue
			}
			if !strings.Contains(p.Percorso, "/photos/") {
				continue
			}
			vuoto, imp := attesePer(p, ""), attesePer(p, datiSinteticiImport)
			if !dentro {
				if vuoto["codice"] != "302" || vuoto["intestazione:location"] != "/login" {
					t.Errorf("%s[%d] senza accesso: %v", s.Nome, i, vuoto)
				}
				continue
			}
			if vuoto["codice"] != "404" || vuoto["intestazione:cache-control"] != "no-store" || vuoto["intestazione:content-type"] != "" {
				t.Errorf("%s[%d] sul database vuoto: %v", s.Nome, i, vuoto)
			}
			switch {
			case strings.HasPrefix(p.Percorso, "/repairs/1/photos/4"):
				if imp["codice"] != "200" || imp["intestazione:referrer-policy"] != "no-referrer" {
					t.Errorf("%s[%d] foto nuova: %v", s.Nome, i, imp)
				}
				if imp["intestazione:x-photo-variant"] == "thumb" {
					miniature++
					if imp["dimensioni"] != "400x300" || imp["intestazione:content-type"] != "image/webp" ||
						imp["intestazione:cache-control"] != "private, max-age=86400" || slices.Contains(p.Confronta, "impronta") {
						t.Errorf("%s[%d] miniatura: %v", s.Nome, i, imp)
					}
				} else if imp["dimensioni"] != "1000x750" || imp["intestazione:content-type"] != "image/jpeg" || !slices.Contains(p.Confronta, "impronta") {
					t.Errorf("%s[%d] foto intera: %v", s.Nome, i, imp)
				}
			case imp["codice"] == "200":
				if imp["intestazione:x-photo-variant"] != "full" || imp["intestazione:content-type"] != "image/jpeg" ||
					imp["impronta"] == "" || imp["dimensioni"] != "(non un'immagine)" {
					t.Errorf("%s[%d] foto della cattura: %v", s.Nome, i, imp)
				}
			default:
				if imp["codice"] != "404" || imp["corpo"] != "Foto non disponibile" {
					t.Errorf("%s[%d] rifiuto: %v", s.Nome, i, imp)
				}
			}
		}
		if miniature == 0 {
			t.Errorf("%s: nessuna miniatura vera", s.Nome)
		}
	}
	if visti != 2 {
		t.Errorf("%d scenari delle foto", visti)
	}
}

// inventario: RIP-S-01
// Lo scenario viste (slice 037b, D-44 come applicata): sui dati importati il titolare apre la 5/26
// (una riga da tutte e due le parti); poi solo sul Go manda un'azione rifiutata con il segno della
// pagina e segue il redirect (nessuna riga), e un rifiuto forgiato senza segno (token dalla home):
// il Go apre la scheda con la riga, e lo standalone apre la scheda (solo standalone) per la stessa
// riga. Il dipendente rifiutato da un'azione del titolare con il segno: nessuna riga. Alla fine il
// totale di repair.viewed si confronta. Una riga in più o in meno lo fa rosso.
func TestScenarioVisteSuiDueDati(t *testing.T) {
	ss, err := leggiScenari(filepath.Join("..", "..", "bench", "scenari"))
	if err != nil {
		t.Fatal(err)
	}
	var v *scenario
	for i := range ss {
		if ss[i].Nome == "viste" {
			v = &ss[i]
		}
	}
	if v == nil {
		t.Fatal("scenario viste non trovato")
	}
	var conSegno, forgiati, mostre, aperture, conteggi []int
	for i, p := range v.Passi {
		switch {
		case p.SoloGo != "" && p.Metodo == "POST" && strings.Contains(p.Corpo, "mostra={mostra}"):
			conSegno = append(conSegno, i)
		case p.SoloGo != "" && p.Metodo == "POST":
			if p.Atteso["intestazione:location"] != "/repairs/5" {
				t.Errorf("viste[%d]: il forgiato deve tornare alla scheda senza segno: %v", i, p.Atteso)
			}
			forgiati = append(forgiati, i)
		case p.SoloGo != "":
			if p.SoloGo != "D-44" || p.Percorso != "{location}" || attesePer(p, datiSinteticiImport)["codice"] != "200" {
				t.Errorf("viste[%d]: %+v", i, p)
			}
			mostre = append(mostre, i)
		case p.SoloStandalone != "":
			if p.SoloStandalone != "D-44" || p.Percorso != "/repairs/5" || attesePer(p, datiSinteticiImport)["codice"] != "200" {
				t.Errorf("viste[%d]: %+v", i, p)
			}
			aperture = append(aperture, i)
		case p.Percorso == "/audit?action=repair.viewed":
			if !slices.Contains(p.Confronta, "visibile:audit-total") {
				t.Errorf("viste[%d] non confronta il totale", i)
			}
			conteggi = append(conteggi, i)
		}
	}
	if len(conSegno) != 2 || len(forgiati) != 1 || len(mostre) != 3 || len(aperture) != 1 ||
		mostre[0] != conSegno[0]+1 || mostre[1] != forgiati[0]+1 || aperture[0] != forgiati[0]+2 || mostre[2] != conSegno[1]+1 {
		t.Errorf("con segno %v, forgiati %v, mostre %v, aperture %v", conSegno, forgiati, mostre, aperture)
	}
	if len(conteggi) != 2 || conteggi[0] > conSegno[0] || conteggi[1] < mostre[2] {
		t.Errorf("conteggi %v", conteggi)
	}
	for i, frase := range map[int]string{mostre[0]: "Azione non valida.", mostre[1]: "Azione non valida.", mostre[2]: "Solo il titolare può revocare il link."} {
		if attesePer(v.Passi[i], datiSinteticiImport)["testo:"+frase] != "presente" {
			t.Errorf("viste[%d] non pretende %q", i, frase)
		}
	}
}

// Lo scenario numeri (slice 037d): sul database vuoto le due schede tornano alla home; sui dati
// importati sono le buste del passo dei numeri, con il numero senza gli zeri che lo standalone ha
// tolto («019/26» e «000020/26» scritti), mai con gli zeri.
func TestScenarioNumeriSuiDueDati(t *testing.T) {
	ss, err := leggiScenari(filepath.Join("..", "..", "bench", "scenari"))
	if err != nil {
		t.Fatal(err)
	}
	var visti []string
	for _, s := range ss {
		if s.Nome != "numeri" {
			continue
		}
		for i, q := range s.Passi {
			if len(q.AttesoDati) == 0 {
				continue
			}
			vuoto, imp := attesePer(q, ""), attesePer(q, datiSinteticiImport)
			if vuoto["codice"] != "302" || vuoto["intestazione:location"] != "/" {
				t.Errorf("numeri[%d] sul database vuoto = %v", i, vuoto)
			}
			id, _ := strconv.ParseInt(strings.TrimPrefix(q.Percorso, "/repairs/"), 10, 64)
			k := slices.Index(busteNumeri, id)
			if k < 0 {
				t.Fatalf("numeri[%d]: %s non è una busta del passo", i, q.Percorso)
			}
			titolo := "testo:Busta #" + numeriNumeri[k] + " · Laboratorio"
			if imp["codice"] != "200" || imp[titolo] != "presente" || imp["visibile:repair-detail/dd@2"] != "Cliente Sintetico Numeri" {
				t.Errorf("numeri[%d] sui dati importati = %v", i, imp)
			}
			zeri := 0
			for c, v := range imp {
				if !slices.Contains(q.Confronta, c) && c != "codice" {
					t.Errorf("numeri[%d]: attesa su %s che il passo non confronta", i, c)
				}
				if strings.HasPrefix(c, "testo:Busta #0") && v == "assente" {
					zeri++
				}
			}
			if zeri != 1 {
				t.Errorf("numeri[%d]: %d attese sul numero con gli zeri, ne serve una assente", i, zeri)
			}
			visti = append(visti, q.Percorso)
		}
	}
	if !slices.Equal(visti, []string{"/repairs/19", "/repairs/20"}) {
		t.Errorf("schede del passo dei numeri: %v", visti)
	}
}
