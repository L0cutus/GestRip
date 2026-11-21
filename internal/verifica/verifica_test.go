package verifica

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/assist"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/tools/banco/modellofinto"
)

// Il banco del modello (slice 112). Casi, dettati, nomi e telefoni inventati; il server è il modello
// finto (tools/banco/modellofinto) e i tempi vengono da un cronometro finto: nessun test aspetta.

// scrivi mette le righe in un file di casi nuovo e dà il percorso.
func scrivi(t *testing.T, nome string, righe ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), nome)
	if err := os.WriteFile(p, []byte(strings.Join(righe, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const (
	rigaStandalone = `{"id": "prova-01", "oggi": "2026-10-23", "testo": "Cliente Ugo Esempio 300 111 2222 anello da allargare, preventivo 400-500 euro, pronto sabato", "attesi": {"data": ["23/10/2026"], "nome": ["Ugo Esempio"], "telefono": ["300 111 2222"], "preventivo": ["400/500"], "data_consegna": ["24/10/2026"], "oreficeria": [""], "compro_oro": [0]}, "busta": "45", "parola": "allarg"}`
	rigaBanco      = `{"cat": "acconto", "frase": "ha dato 20 euro", "oggi": "venerdì 23/10/2026", "dettato": "Cliente Ada Prova catenina da saldare, ha dato 20 euro", "risposta": {"n": "Ada Prova", "d": "catenina da saldare", "a": "20"}}`
)

func TestCaricaIDueFormati(t *testing.T) {
	casi, err := CaricaTutti([]string{scrivi(t, "A.jsonl", rigaStandalone), scrivi(t, "B-prova.jsonl", "", rigaBanco, "  ")})
	if err != nil || len(casi) != 2 {
		t.Fatalf("%v %d", err, len(casi))
	}
	a, b := casi[0], casi[1]
	if a.ID != "prova-01" || a.File != "A.jsonl" || a.Categoria != "prova" || !a.Oggi.Equal(time.Date(2026, 10, 23, 12, 0, 0, 0, clock.Roma)) ||
		!strings.HasPrefix(a.Dettato, "Cliente Ugo") {
		t.Errorf("standalone: %+v", a)
	}
	attesiA := map[string][]string{"data": {"23/10/2026"}, "nome": {"Ugo Esempio"}, "telefono": {"300 111 2222"},
		"preventivo": {"400/500"}, "data_consegna": {"24/10/2026"}, "oreficeria": {""}, "compro_oro": {"0"},
		"busta": {"45"}, "parola": {"allarg"}}
	if !uguali(a.Attesi, attesiA) {
		t.Errorf("attesi standalone: %v", a.Attesi)
	}
	// Banco: l'id dalla riga (contando le righe vuote), ogni chiave corta contata, vuota se manca.
	if b.ID != "B-prova-2" || b.Categoria != "acconto" || !b.Oggi.Equal(time.Date(2026, 10, 23, 12, 0, 0, 0, clock.Roma)) {
		t.Errorf("banco: %+v", b)
	}
	attesiB := map[string][]string{"nome": {"Ada Prova"}, "oreficeria": {""}, "busta": {""}, "descrizione": {"catenina da saldare"},
		"data_consegna": {""}, "preventivo": {""}, "acconto": {"20"}, "altre_note": {""}}
	if !uguali(b.Attesi, attesiB) {
		t.Errorf("attesi banco: %v", b.Attesi)
	}
}

func uguali(a, b map[string][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !slices.Equal(v, b[k]) {
			return false
		}
	}
	return true
}

// Una riga sbagliata rifiuta il file con il suo numero e senza ripeterla; anche un file vuoto o due id
// uguali.
func TestCaricaRifiuta(t *testing.T) {
	segreto := "Zebedeo Inventatissimo"
	for _, c := range []struct{ nome, riga string }{
		{"non JSON", "{" + segreto},
		{"né attesi né risposta", `{"id": "x-1", "testo": "` + segreto + `"}`},
		{"id vuoto", strings.Replace(rigaStandalone, `"prova-01"`, `""`, 1)},
		{"oggi non ISO", strings.Replace(rigaStandalone, `"2026-10-23"`, `"23/10/2026"`, 1)},
		{"testo non testo", strings.Replace(rigaStandalone, `"testo": "Cliente Ugo Esempio`, `"testo": 5, "x": "`, 1)},
		{"campo che manca", strings.Replace(rigaStandalone, `"compro_oro": [0]`, `"altro": [0]`, 1)},
		{"lista vuota", strings.Replace(rigaStandalone, `"compro_oro": [0]`, `"compro_oro": []`, 1)},
		{"decimale", strings.Replace(rigaStandalone, `"compro_oro": [0]`, `"compro_oro": [0.5]`, 1)},
		{"busta vuota", strings.Replace(rigaStandalone, `"busta": "45"`, `"busta": ""`, 1)},
		{"parola numero", strings.Replace(rigaStandalone, `"parola": "allarg"`, `"parola": 3`, 1)},
		{"giorno sbagliato", strings.Replace(rigaBanco, "venerdì", "sabato", 1)},
		{"oggi senza giorno", strings.Replace(rigaBanco, "venerdì 23/10/2026", "23/10/2026", 1)},
		{"chiave sconosciuta", strings.Replace(rigaBanco, `"a": "20"`, `"z": "20"`, 1)},
		{"valore non testo", strings.Replace(rigaBanco, `"a": "20"`, `"a": 20`, 1)},
		{"cat che manca", strings.Replace(rigaBanco, `"cat": "acconto", `, "", 1)},
	} {
		p := scrivi(t, "casi.jsonl", rigaBanco, c.riga)
		_, err := Carica(p)
		if err == nil || err.Error() != "casi.jsonl, riga 2: caso non valido" {
			t.Errorf("%s: %v", c.nome, err)
		}
		if err != nil && strings.Contains(err.Error(), "Zebedeo") {
			t.Errorf("%s: l'errore ripete la riga", c.nome)
		}
	}
	if _, err := Carica(scrivi(t, "vuoto.jsonl", "", " ")); err == nil || err.Error() != "vuoto.jsonl: nessun caso" {
		t.Errorf("vuoto: %v", err)
	}
	if _, err := Carica(filepath.Join(t.TempDir(), "manca.jsonl")); err == nil {
		t.Error("file che manca: nessun errore")
	}
	if _, err := CaricaTutti([]string{scrivi(t, "a.jsonl", rigaStandalone), scrivi(t, "b.jsonl", rigaStandalone)}); err == nil || err.Error() != "id ripetuto: prova-01" {
		t.Errorf("id ripetuti: %v", err)
	}
}

// inventario: RIP-H-54, OPS-08
// I banchi del repo: 1720 casi, tutti leggibili, id diversi. Gli 84 casi dello standalone non sono nel
// repo (si rigenerano in locale, bench/modello/README.md).
func TestCasiDelRepo(t *testing.T) {
	cartella := filepath.Join("..", "..", "bench", "modello")
	conti := map[string]int{"B-difficili.jsonl": 720, "C-acconto-test.jsonl": 600,
		"D-prezzi-verbo.jsonl": 200, "D2-prezzi-verbo-nuovi.jsonl": 200}
	var percorsi []string
	for f := range conti {
		percorsi = append(percorsi, filepath.Join(cartella, f))
	}
	slices.Sort(percorsi)
	casi, err := CaricaTutti(percorsi)
	if err != nil || len(casi) != 1720 {
		t.Fatalf("%v %d", err, len(casi))
	}
	visti := map[string]int{}
	for _, c := range casi {
		visti[c.File]++
	}
	for f, n := range conti {
		if visti[f] != n {
			t.Errorf("%s: %d casi, attesi %d", f, visti[f], n)
		}
	}
	// Il campione per la corsa su CPU: 10 per categoria.
	if n := len(Campione(casi, 10)); n != 300 {
		t.Errorf("campione: %d", n)
	}
}

func TestCampione(t *testing.T) {
	var casi []Caso
	for i := range 10 {
		casi = append(casi, Caso{ID: "a" + string(rune('0'+i)), File: "f", Categoria: "uno"})
	}
	casi = append(casi, Caso{ID: "b0", File: "f", Categoria: "due"}, Caso{ID: "c0", File: "g", Categoria: "uno"})
	ids := func(cc []Caso) string {
		var s []string
		for _, c := range cc {
			s = append(s, c.ID)
		}
		return strings.Join(s, " ")
	}
	if g := ids(Campione(casi, 4)); g != "a0 a2 a5 a7 b0 c0" {
		t.Errorf("4: %s", g)
	}
	if g := ids(Campione(casi, 1)); g != "a0 b0 c0" {
		t.Errorf("1: %s", g)
	}
	if g := Campione(casi, 0); len(g) != len(casi) {
		t.Errorf("0: %d", len(g))
	}
	if g := Campione(casi, 10); len(g) != len(casi) {
		t.Errorf("10: %d", len(g))
	}
}

// Il punteggio: si perdonano solo maiuscole e spazi, e «400-500» = «400/500» nel preventivo.
func TestConfrontabile(t *testing.T) {
	for _, c := range []struct {
		campo, a, b string
		uguali      bool
	}{
		{"nome", "Ugo Esempio", "  ugo   ESEMPIO ", true},
		{"nome", "Ugo Esempio", "Ugo\tEsempio\n", true},
		{"nome", "Ugo Esempio", "Ugo Esempio.", false},
		{"nome", "Ugo Esempio", "Ugo Esempi", false},
		{"preventivo", "400/500", "400-500", true},
		{"preventivo", "400/500", "400 - 500", false}, // gli spazi interni restano uno
		{"preventivo", "400 / 500", "400 - 500", true},
		{"preventivo", "35", "35 €", false},
		{"preventivo", "35", "35 euro", false},
		{"preventivo", "35", "35.0", false},
		{"acconto", "400/500", "400-500", false}, // solo il preventivo
		{"descrizione", "a-b", "a/b", false},
		{"telefono", "300 111 2222", "3001112222", true},
		{"telefono", "+39 300 111 2222", "393001112222", false}, // il «+» resta, come Phone.normalize/1
		{"telefono", "", "nessuno", true},                       // senza cifre è vuoto
		{"data_consegna", "24/10/2026", "24/10/2026 ", true},
		{"data_consegna", "24/10/2026", "24-10-2026", false},
	} {
		if g := confrontabile(c.campo, c.a) == confrontabile(c.campo, c.b); g != c.uguali {
			t.Errorf("%s: %q e %q: %v", c.campo, c.a, c.b, g)
		}
	}
}

// valoriDi legge la proposta come la vede il negozio: note «Busta N — Acconto N € — x», date GG/MM/AAAA.
func TestValoriDellaProposta(t *testing.T) {
	for _, c := range []struct{ note, busta, acconto, altre string }{
		{"", "", "", ""},
		{"Busta 241", "241", "", ""},
		{"Acconto 20 €", "", "20", ""},
		{"Busta 7 — Acconto 20.5 € — da ritirare in mattinata", "7", "20.5", "da ritirare in mattinata"},
		{"Busta 7 — da ritirare — Acconto 20 €", "7", "", "da ritirare — Acconto 20 €"},
		{"da ritirare", "", "", "da ritirare"},
		{"Acconto 20", "", "", "Acconto 20"},
		{"busta 7 da ritirare", "", "", "busta 7 da ritirare"}, // la proposta scrive «Busta»: un'altra nota non è la busta
	} {
		b, a, x := separaNote(c.note)
		if b != c.busta || a != c.acconto || x != c.altre {
			t.Errorf("%q: %q %q %q", c.note, b, a, x)
		}
	}
	// La parola si cerca senza guardare le maiuscole, da tutte e due le parti.
	if !giustoIl("parola", map[string]string{"descrizione": "Anello da Allargare"}, []string{"xx", "ALLARG"}) ||
		giustoIl("parola", map[string]string{"descrizione": "anello da stringere"}, []string{"allarg"}) {
		t.Error("parola")
	}
	v := valoriDi(assist.Proposta{DataAccettazione: "2026-03-05", DataPrevista: "2026-10-24", CompraOro: true})
	if v["data"] != "05/03/2026" || v["data_consegna"] != "24/10/2026" || v["compro_oro"] != "1" {
		t.Errorf("%v", v)
	}
	if v := valoriDi(assist.Proposta{}); v["data_consegna"] != "" || v["compro_oro"] != "0" {
		t.Errorf("vuota: %v", v)
	}
}

func TestValuta(t *testing.T) {
	casi, err := CaricaTutti([]string{scrivi(t, "A.jsonl", rigaStandalone), scrivi(t, "B.jsonl", rigaBanco)})
	if err != nil {
		t.Fatal(err)
	}
	a, b := casi[0], casi[1]
	giusta := assist.Proposta{DataAccettazione: "2026-10-23", NomeCliente: "UGO esempio", TelefonoCliente: "3001112222",
		Preventivo: "400-500", DataPrevista: "2026-10-24", Descrizione: "Anello da ALLARGARE", NotePubbliche: "Busta 45"}
	e := Valuta(a, assist.Estrazione{Proposta: giusta, Tentativi: 1}, nil, 1500*time.Millisecond)
	if !e.TutteGiuste || e.Esito != EsitoOK || len(e.Controlli) != 9 || e.Durata != 1500*time.Millisecond || e.Tentativi != 1 {
		t.Errorf("giusta: %+v", e)
	}
	for campo, cambia := range map[string]func(*assist.Proposta){
		"data":          func(p *assist.Proposta) { p.DataAccettazione = "2026-10-22" },
		"nome":          func(p *assist.Proposta) { p.NomeCliente = "Ugo" },
		"telefono":      func(p *assist.Proposta) { p.TelefonoCliente = "" },
		"preventivo":    func(p *assist.Proposta) { p.Preventivo = "400" },
		"data_consegna": func(p *assist.Proposta) { p.DataPrevista = "" },
		"oreficeria":    func(p *assist.Proposta) { p.Oreficeria = "Bottari" },
		"compro_oro":    func(p *assist.Proposta) { p.CompraOro = true },
		"busta":         func(p *assist.Proposta) { p.NotePubbliche = "Busta 46" },
		"parola":        func(p *assist.Proposta) { p.Descrizione = "anello da stringere" },
	} {
		p := giusta
		cambia(&p)
		e := Valuta(a, assist.Estrazione{Proposta: p}, nil, 0)
		if e.TutteGiuste || e.Controlli[campo] || RigaDelCaso(e) != "prova-01: "+campo {
			t.Errorf("%s: %+v %q", campo, e.Controlli, RigaDelCaso(e))
		}
	}
	// Casi dello standalone: la busta si cerca come right?("busta"), «busta N» come parola in tutte le note,
	// senza badare a maiuscole e posizione (M1 della revisione).
	for note, ok := range map[string]bool{"Busta 45": true, "Acconto 5 € — busta 45": true, "riferimento BUSTA\u00a045.": true,
		"Busta 451": false, "Busta 4": false, "ribusta 45": false, "": false} {
		p := giusta
		p.NotePubbliche = note
		if e := Valuta(a, assist.Estrazione{Proposta: p}, nil, 0); e.Controlli["busta"] != ok {
			t.Errorf("busta in %q: %v", note, e.Controlli["busta"])
		}
	}
	// Il banco: la risposta corta contro la proposta; un acconto inventato o una busta in più sono errori.
	pb := assist.Proposta{NomeCliente: "Ada Prova", Descrizione: "catenina da saldare", NotePubbliche: "Acconto 20 €"}
	if e := Valuta(b, assist.Estrazione{Proposta: pb}, nil, 0); !e.TutteGiuste || len(e.Controlli) != 8 {
		t.Errorf("banco giusto: %+v", e)
	}
	pb.NotePubbliche = "Busta 3 — Acconto 20 €"
	pb.Preventivo = "20"
	if e := Valuta(b, assist.Estrazione{Proposta: pb}, nil, 0); RigaDelCaso(e) != "B-1: busta, preventivo" || !e.BustaInventata || e.AccontoInventato {
		t.Errorf("banco sbagliato: %q %+v", RigaDelCaso(e), e)
	}
	// Acconto inventato (M2): atteso vuoto, proposta con l'acconto.
	cb, err := Carica(scrivi(t, "B.jsonl", strings.Replace(rigaBanco, `, "a": "20"`, "", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if e := Valuta(cb[0], assist.Estrazione{Proposta: pb}, nil, 0); !e.AccontoInventato || !e.BustaInventata {
		t.Errorf("inventati: %+v", e)
	}
	pb.NotePubbliche = ""
	if e := Valuta(cb[0], assist.Estrazione{Proposta: pb}, nil, 0); e.AccontoInventato || e.BustaInventata {
		t.Errorf("niente inventato: %+v", e)
	}
	if e := Valuta(cb[0], assist.Estrazione{Proposta: assist.Proposta{NotePubbliche: "Acconto 20 €"}}, assist.ErrRispostaIllegibile, 0); e.AccontoInventato {
		t.Errorf("senza proposta niente inventato: %+v", e)
	}
	// Senza proposta ogni campo è sbagliato; l'esito dice perché, con un motivo fisso.
	for _, c := range []struct {
		err         error
		esito, riga string
	}{
		{assist.ErrRispostaIllegibile, EsitoIllegibile, "prova-01: risposta illeggibile"},
		{assist.ErrModello{Motivo: "ai:http:500"}, EsitoRifiutato, "prova-01: rifiutato (ai:http:500)"},
		{assist.ErrTestoCorto, EsitoNonMandato, "prova-01: non mandato (testo troppo corto)"},
		{assist.ErrTestoLungo, EsitoNonMandato, "prova-01: non mandato (testo troppo lungo)"},
		{errors.New("Ugo Esempio 300 111 2222"), EsitoRifiutato, "prova-01: rifiutato (errore)"},
	} {
		e := Valuta(a, assist.Estrazione{Proposta: giusta, Tentativi: 2}, c.err, 0)
		if e.Esito != c.esito || e.TutteGiuste || RigaDelCaso(e) != c.riga || e.Tentativi != 2 {
			t.Errorf("%v: %+v %q", c.err, e, RigaDelCaso(e))
		}
		for campo, ok := range e.Controlli {
			if ok {
				t.Errorf("%v: %s giusto", c.err, campo)
			}
		}
	}
}

func TestRiassunto(t *testing.T) {
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	var esiti []Esito
	for i := 1; i <= 10; i++ {
		esiti = append(esiti, Esito{ID: "x", File: "A", Esito: EsitoOK, TutteGiuste: i != 3,
			Controlli: map[string]bool{"nome": i != 3, "busta": true}, Durata: ms(i * 100), Tentativi: 1})
	}
	esiti = append(esiti,
		Esito{File: "B", Esito: EsitoIllegibile, Controlli: map[string]bool{"nome": false}, Durata: ms(5000), Tentativi: 2},
		Esito{File: "B", Esito: EsitoRifiutato, Motivo: "ai:http:500", Controlli: map[string]bool{"nome": false}, Durata: ms(50), Tentativi: 2},
		Esito{File: "B", Esito: EsitoNonMandato, Motivo: "testo troppo corto", Controlli: map[string]bool{"compro_oro": false}},
	)
	r := Riassumi(esiti)
	// 13 tempi ordinati: 0 50 100 200 … 1000 5000; mediana al rango 7, p90 al rango 12 (ceil(11,7)).
	if r.Casi != 13 || r.TutteGiuste != 9 || r.Mediana != ms(500) || r.P90 != ms(1000) || r.Max != ms(5000) ||
		r.Illeggibili != 1 || r.NuoviTentativi != 2 || r.PerCampo["nome"] != (Conto{9, 12}) || r.PerCampo["busta"] != (Conto{10, 10}) ||
		r.PerFile["A"] != (Conto{9, 10}) || r.PerFile["B"] != (Conto{0, 3}) || !slices.Equal(r.File, []string{"A", "B"}) {
		t.Errorf("%+v", r)
	}
	atteso := []string{
		"schede tutte giuste: 9/13", "  A: 9/10", "  B: 0/3", "per campo:", "  nome: 9/12", "  busta: 10/10",
		"  compro_oro: 0/1 (dal testo, non dal modello)", "acconti inventati: 0", "buste inventate: 0", "risposte illeggibili: 1",
		"rifiuti: 2 (ai:http:500 1, testo troppo corto 1)", "casi con un nuovo tentativo: 2",
		"tempo per scheda (ms): mediana 500, p90 1000, max 5000",
	}
	if g := Rapporto(r); !slices.Equal(g, atteso) {
		t.Errorf("rapporto:\n%s", strings.Join(g, "\n"))
	}
	for _, c := range []struct {
		tempi       []int
		med, p90, m int
	}{
		{[]int{7}, 7, 7, 7},
		{[]int{1, 2}, 1, 2, 2},
		{[]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 5, 9, 10},
	} {
		var ee []Esito
		for _, x := range c.tempi {
			ee = append(ee, Esito{Durata: ms(x)})
		}
		r := Riassumi(ee)
		if r.Mediana != ms(c.med) || r.P90 != ms(c.p90) || r.Max != ms(c.m) {
			t.Errorf("%v: %v %v %v", c.tempi, r.Mediana, r.P90, r.Max)
		}
	}
	if r := Riassumi(nil); r.Mediana != 0 || r.Max != 0 || Rapporto(r)[0] != "schede tutte giuste: 0/0" {
		t.Errorf("vuoto: %+v", r)
	}
}

// cronometroFinto dà, a ogni misura, il tempo dopo nella lista.
func cronometroFinto(tempi ...time.Duration) Cronometro {
	i := 0
	return func() func() time.Duration {
		d := tempi[i%len(tempi)]
		i++
		return func() time.Duration { return d }
	}
}

// inventario: RIP-H-54, OPS-08
// La corsa con il modello finto: ogni caso è una domanda della nuova busta (solo il messaggio
// dell'utente «Oggi: …\nDettato: …», temperature 0, nessuna chiave), con il nuovo tentativo dopo una
// risposta illeggibile o un errore, e la lettura della proposta (nome dall'apertura, oreficerie note).
func TestGiraConIlModelloFinto(t *testing.T) {
	f := modellofinto.Nuovo()
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	casi, err := CaricaTutti([]string{scrivi(t, "A.jsonl", rigaStandalone,
		strings.NewReplacer(`"prova-01"`, `"prova-02"`, `"busta": "45"`, `"busta": null`).Replace(rigaStandalone)),
		scrivi(t, "B.jsonl", rigaBanco, rigaBanco, rigaBanco)})
	if err != nil {
		t.Fatal(err)
	}
	f.Programma(
		// prova-01: giusta; il nome lo legge dall'apertura anche se il modello non lo dà.
		modellofinto.Buona(`{"b": 45, "d": "anello da allargare", "c": "sabato", "p": "400-500"}`),
		// prova-02: prima illeggibile, poi una scheda con il preventivo sbagliato.
		modellofinto.Buona("non ho capito"), modellofinto.Buona(`{"n": "Ugo Esempio", "d": "anello da allargare", "c": "sabato", "p": "450"}`),
		// B-1: giusta al secondo tentativo dopo un 500.
		modellofinto.Risposta{Stato: 500}, modellofinto.Buona("```json\n{\"n\": \"ada prova\", \"d\": \"catenina da saldare\", \"a\": 20}\n```"),
		// B-2: due 500.
		modellofinto.Risposta{Stato: 500}, modellofinto.Risposta{Stato: 503},
		// B-3: l'acconto letto come preventivo.
		modellofinto.Buona(`{"n": "Ada Prova", "d": "catenina da saldare", "p": "20"}`),
	)
	var avanzamento [][2]int
	c := Corsa{
		Estrai:      DallaBusta(&assist.Collegamento{Config: assist.Configurazione{Indirizzo: s.URL + "/", Modello: "candidato"}, Attesa: 5 * time.Second}),
		Negozio:     assist.Negozio{Oreficerie: []assist.Oreficeria{{ID: 2, Nome: "Bottari"}}},
		Cronometro:  cronometroFinto(100*time.Millisecond, 300*time.Millisecond, 200*time.Millisecond, 900*time.Millisecond, 400*time.Millisecond),
		Avanzamento: func(fatti, totale int) { avanzamento = append(avanzamento, [2]int{fatti, totale}) },
	}
	esiti, err := c.Gira(context.Background(), casi)
	if err != nil || len(esiti) != 5 {
		t.Fatalf("%v %d", err, len(esiti))
	}
	var righe []string
	for _, e := range esiti {
		righe = append(righe, RigaDelCaso(e))
	}
	if atteso := []string{"", "prova-02: preventivo", "", "B-2: rifiutato (ai:http:503)", "B-3: preventivo, acconto"}; !slices.Equal(righe, atteso) {
		t.Errorf("righe: %q", righe)
	}
	if g := []int{esiti[0].Tentativi, esiti[1].Tentativi, esiti[2].Tentativi, esiti[3].Tentativi, esiti[4].Tentativi}; !slices.Equal(g, []int{1, 2, 2, 2, 1}) {
		t.Errorf("tentativi: %v", g)
	}
	r := Riassumi(esiti)
	if r.TutteGiuste != 2 || r.Mediana != 300*time.Millisecond || r.P90 != 900*time.Millisecond || r.Max != 900*time.Millisecond || r.NuoviTentativi != 3 {
		t.Errorf("riassunto: %+v", r)
	}
	if !slices.Equal(avanzamento, [][2]int{{1, 5}, {2, 5}, {3, 5}, {4, 5}, {5, 5}}) {
		t.Errorf("avanzamento: %v", avanzamento)
	}
	// Le richieste: 8, tutte come la nuova busta.
	ric := f.Richieste()
	if len(ric) != 8 {
		t.Fatalf("%d richieste", len(ric))
	}
	domande := []string{
		"Oggi: venerdì 23/10/2026\nDettato: " + casi[0].Dettato,
		"Oggi: venerdì 23/10/2026\nDettato: " + casi[2].Dettato,
	}
	for i, r := range ric {
		corpo, _ := json.Marshal(r.Corpo)
		var c struct {
			Model       string           `json:"model"`
			Temperature *float64         `json:"temperature"`
			Stream      *bool            `json:"stream"`
			Messages    []map[string]any `json:"messages"`
		}
		if json.Unmarshal(corpo, &c) != nil || c.Model != "candidato" || c.Temperature == nil || *c.Temperature != 0 ||
			c.Stream == nil || *c.Stream || len(c.Messages) != 1 || c.Messages[0]["role"] != "user" || r.ConAutorizz ||
			r.Percorso != "/v1/chat/completions" || len(r.Corpo) != 4 {
			t.Errorf("richiesta %d: %s", i, corpo)
			continue
		}
		atteso := domande[0]
		if i >= 3 {
			atteso = domande[1]
		}
		if c.Messages[0]["content"] != atteso {
			t.Errorf("richiesta %d: %q", i, c.Messages[0]["content"])
		}
	}
}

// Un contesto finito ferma la corsa prima del caso dopo e dà gli esiti fatti; il caso a metà non conta.
func TestGiraInterrotta(t *testing.T) {
	casi, err := Carica(scrivi(t, "B.jsonl", rigaBanco, rigaBanco, rigaBanco))
	if err != nil {
		t.Fatal(err)
	}
	ctx, ferma := context.WithCancel(context.Background())
	chiamate := 0
	c := Corsa{Estrai: func(ctx context.Context, _ string, _ time.Time, _ assist.Negozio) (assist.Estrazione, error) {
		chiamate++
		if chiamate == 2 {
			ferma()
			return assist.Estrazione{}, assist.ErrModello{Motivo: "ai:transport::closed"}
		}
		return assist.Estrazione{Tentativi: 1}, nil
	}, Cronometro: cronometroFinto(time.Millisecond)}
	esiti, err := c.Gira(ctx, casi)
	if !errors.Is(err, context.Canceled) || len(esiti) != 1 || chiamate != 2 {
		t.Errorf("%v %d %d", err, len(esiti), chiamate)
	}
	if esiti, err := c.Gira(ctx, casi); !errors.Is(err, context.Canceled) || len(esiti) != 0 || chiamate != 2 {
		t.Errorf("già finito: %v %d %d", err, len(esiti), chiamate)
	}
}

// Il cronometro vero misura qualcosa che non è negativo.
func TestCronometroVero(t *testing.T) {
	if d := CronometroVero()(); d < 0 {
		t.Errorf("%v", d)
	}
}

// Le oreficerie del negozio arrivano alla lettura del nome (slice 105): un nome uguale a un'oreficeria
// nota non è una persona.
func TestGiraConLeOreficerieDelNegozio(t *testing.T) {
	f := modellofinto.Nuovo()
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	casi, err := Carica(scrivi(t, "B.jsonl", `{"cat": "oreficeria", "oggi": "venerdì 23/10/2026", "dettato": "da Bottari, catenina da saldare", "risposta": {"o": "Bottari", "d": "catenina da saldare"}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		negozio assist.Negozio
		nome    bool
	}{
		{assist.Negozio{Oreficerie: []assist.Oreficeria{{ID: 2, Nome: "Bottari"}}}, true},
		{assist.Negozio{}, false},
	} {
		f.Programma(modellofinto.Buona(`{"n": "Bottari", "d": "catenina da saldare"}`))
		esiti, err := Corsa{Estrai: DallaBusta(&assist.Collegamento{Config: assist.Configurazione{Indirizzo: s.URL, Modello: "m"}, Attesa: 5 * time.Second}),
			Negozio: c.negozio}.Gira(context.Background(), casi)
		if err != nil || len(esiti) != 1 || esiti[0].Controlli["nome"] != c.nome || esiti[0].Controlli["oreficeria"] || !esiti[0].Controlli["descrizione"] {
			t.Errorf("%+v: %v %+v", c.negozio, err, esiti)
		}
	}
}

// I motivi dei rifiuti in ordine, qualunque sia l'ordine dei casi; acconti e buste inventati contati (M2).
func TestRapportoMotiviEInventati(t *testing.T) {
	var esiti []Esito
	for _, m := range []string{"ai:transport::timeout", "ai:http:503", "testo troppo lungo", "ai:http:500", "ai:transport::econnrefused", "ai:http:503"} {
		esiti = append(esiti, Esito{Esito: EsitoRifiutato, Motivo: m})
	}
	esiti = append(esiti, Esito{Esito: EsitoOK, AccontoInventato: true, BustaInventata: true}, Esito{Esito: EsitoOK, AccontoInventato: true})
	r := Rapporto(Riassumi(esiti))
	for _, atteso := range []string{"rifiuti: 6 (ai:http:500 1, ai:http:503 2, ai:transport::econnrefused 1, ai:transport::timeout 1, testo troppo lungo 1)",
		"acconti inventati: 2", "buste inventate: 1"} {
		if !slices.Contains(r, atteso) {
			t.Errorf("manca %q:\n%s", atteso, strings.Join(r, "\n"))
		}
	}
}

// Date con giorno e mese a una cifra (B2 della revisione): «giovedì 05/03/2026» e «2026-03-05» sono il 5 marzo,
// e la data di oggi della proposta torna «05/03/2026».
func TestDateAUnaCifra(t *testing.T) {
	casi, err := CaricaTutti([]string{
		scrivi(t, "B.jsonl", strings.Replace(rigaBanco, "venerdì 23/10/2026", "giovedì 05/03/2026", 1)),
		scrivi(t, "A.jsonl", strings.NewReplacer(`"2026-10-23"`, `"2026-03-05"`, `"23/10/2026"`, `"05/03/2026"`).Replace(rigaStandalone)),
	})
	if err != nil {
		t.Fatal(err)
	}
	marzo := time.Date(2026, 3, 5, 12, 0, 0, 0, clock.Roma)
	if !casi[0].Oggi.Equal(marzo) || !casi[1].Oggi.Equal(marzo) {
		t.Errorf("%v %v", casi[0].Oggi, casi[1].Oggi)
	}
	e := Valuta(casi[1], assist.Estrazione{Proposta: assist.Proposta{DataAccettazione: "2026-03-05"}}, nil, 0)
	if !e.Controlli["data"] {
		t.Errorf("%+v", e.Controlli)
	}
}
