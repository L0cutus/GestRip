package assist

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// La riga del file di addestramento (slice 117; RIP-H-52, OPS-09), come TrainingExport.export/5 con Training.current/1 e
// Training.corrected_fields/2 dello standalone a go-baseline (training_export_test.exs). Nomi, telefoni (333 000 …) e
// dettati inventati.

func bustaAttualeProva(f ...func(*BustaAttuale)) BustaAttuale {
	b := BustaAttuale{
		Data: "23/10/2026", Nome: "Mario Rossi", Telefono: "333 000 1234", Descrizione: "Orologio da revisionare",
	}
	for _, g := range f {
		g(&b)
	}
	return b
}

func dettatoRaccoltoProva(proposta string) DettatoRaccolto {
	modello := "modello-di-prova"
	return DettatoRaccolto{
		ID: 7, Testo: "Cliente Mario Rossi 333 000 1234 orologio da revisionare", Proposta: proposta, Modello: &modello,
		// 23:30 UTC del 26/10/2026 è già il 27/10 (martedì) a Roma.
		CreatoIl: time.Date(2026, 10, 26, 23, 30, 0, 0, time.UTC),
	}
}

const propostaBase = `{"data":"23/10/2026","nome":"Mario Rossi","telefono":"333 000 1234","descrizione":"Orologio da revisionare",` +
	`"note":"","preventivo":"","data_consegna":"","oreficeria":"","oreficeria_id":"","compro_oro":0}`

// conProposta è propostaBase con le chiavi di cambi (in JSON grezzo) al posto delle sue.
func conProposta(t *testing.T, cambi map[string]string) string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(propostaBase), &m); err != nil {
		t.Fatal(err)
	}
	for k, v := range cambi {
		m[k] = json.RawMessage(v)
	}
	j, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(j)
}

func rigaAddestramento(t *testing.T, d DettatoRaccolto, b BustaAttuale) map[string]any {
	t.Helper()
	j, m := RigaPerAddestramento(d, b, ParoleDiAltriNomi([]string{"Mario Rossi"}, nil), chiaveProva)
	if m != "" {
		t.Fatalf("scartata: %s", m)
	}
	if !strings.HasSuffix(string(j), "}\n") || strings.Count(string(j), "\n") != 1 {
		t.Fatalf("una riga sola che finisce con a capo: %q", j)
	}
	dec := json.NewDecoder(strings.NewReader(string(j)))
	dec.UseNumber()
	var r map[string]any
	if err := dec.Decode(&r); err != nil {
		t.Fatal(err)
	}
	return r
}

// inventario: RIP-H-52
// La risposta attesa è la busta com'è adesso, anche corretta giorni dopo il dettato: descrizione e preventivo nuovi nella
// risposta, quelli del modello nella proposta, i campi corretti contati; oggi e giorno sono quelli del negozio; il modello
// com'è; solo le chiavi dello standalone.
func TestAddestramentoRispostaAttuale(t *testing.T) {
	b := bustaAttualeProva(func(b *BustaAttuale) {
		b.Descrizione, b.Preventivo = "Orologio da revisionare, cambio pila", "35"
	})
	r := rigaAddestramento(t, dettatoRaccoltoProva(propostaBase), b)
	chiavi := []string{}
	for k := range r {
		chiavi = append(chiavi, k)
	}
	slices.Sort(chiavi)
	if strings.Join(chiavi, ",") != "campi_corretti,giorno,modello,nome_corretto_a_mano,nome_dall_anagrafica,oggi,proposta,risposta,testo" {
		t.Errorf("chiavi %v", chiavi)
	}
	risposta, proposta := r["risposta"].(map[string]any), r["proposta"].(map[string]any)
	if risposta["descrizione"] != "Orologio da revisionare, cambio pila" || risposta["preventivo"] != "35" {
		t.Errorf("risposta %v", risposta)
	}
	if proposta["descrizione"] != "Orologio da revisionare" || proposta["preventivo"] != "" {
		t.Errorf("proposta %v", proposta)
	}
	if got := r["campi_corretti"].([]any); len(got) != 2 || got[0] != "descrizione" || got[1] != "preventivo" {
		t.Errorf("campi corretti %v", got)
	}
	if r["oggi"] != "2026-10-27" || r["giorno"] != "martedì" || r["modello"] != "modello-di-prova" {
		t.Errorf("oggi %v giorno %v modello %v", r["oggi"], r["giorno"], r["modello"])
	}
	if r["nome_corretto_a_mano"] != false || r["nome_dall_anagrafica"] != false {
		t.Errorf("nome %v %v", r["nome_corretto_a_mano"], r["nome_dall_anagrafica"])
	}
	// Prompt.fields: niente oreficeria_id né nome_corretto_da, nella risposta e nella proposta.
	for _, m := range []map[string]any{risposta, proposta} {
		if len(m) != 9 || m["oreficeria_id"] != nil || m["nome_corretto_da"] != nil {
			t.Errorf("campi %v", m)
		}
	}
	if risposta["compro_oro"] != json.Number("0") {
		t.Errorf("compro_oro %#v", risposta["compro_oro"])
	}
	// Jason non scrive «<», «>» e «&» come \u003c…
	b.Note = "anello <oro> & argento"
	if j, _ := RigaPerAddestramento(dettatoRaccoltoProva(propostaBase), b, nil, chiaveProva); !strings.Contains(string(j), `"note":"anello <oro> & argento"`) {
		t.Errorf("segni riscritti: %s", j)
	}
	// Il modello che manca è null.
	d := dettatoRaccoltoProva(propostaBase)
	d.Modello = nil
	if r := rigaAddestramento(t, d, bustaAttualeProva()); r["modello"] != nil {
		t.Errorf("modello %v", r["modello"])
	}
}

// inventario: RIP-H-52
// Il JSON della proposta si legge con UseNumber (nota 14): un preventivo dettato come numero decimale si esporta com'è
// (con float64 sarebbe valore_illeggibile) e un id di oreficeria oltre 2^53 resta esatto nel confronto.
func TestAddestramentoNumeriConUseNumber(t *testing.T) {
	grande := int64(9007199254740993) // 2^53 + 1: in float64 diventerebbe …992
	b := bustaAttualeProva(func(b *BustaAttuale) { b.OreficeriaID, b.Oreficeria, b.Preventivo = &grande, "Esempio", "35.50" })
	d := dettatoRaccoltoProva(conProposta(t, map[string]string{"preventivo": "35.50", "oreficeria_id": "9007199254740993", "oreficeria": `"Esempio"`}))
	j, m := RigaPerAddestramento(d, b, nil, chiaveProva)
	if m != "" {
		t.Fatalf("scartata: %s", m)
	}
	if !strings.Contains(string(j), `"preventivo":35.50`) {
		t.Errorf("il numero non è com'era: %s", j)
	}
	r := rigaAddestramento(t, d, b)
	if c := r["campi_corretti"].([]any); len(c) != 0 {
		t.Errorf("campi corretti %v", c)
	}
	// Un id diverso di uno è una correzione dell'oreficeria.
	d = dettatoRaccoltoProva(conProposta(t, map[string]string{"oreficeria_id": "9007199254740992", "oreficeria": `"Esempio"`, "preventivo": `"35.50"`}))
	if c := rigaAddestramento(t, d, b)["campi_corretti"].([]any); len(c) != 1 || c[0] != "oreficeria" {
		t.Errorf("campi corretti %v", c)
	}
}

// inventario: RIP-H-52
// I campi corretti come Training.corrected_fields/2: il nome senza accenti, spazi e maiuscole; il telefono per cifre
// (senza +39) e solo se il modello ne aveva dettato uno; la consegna come data se tutte e due lo sono, altrimenti come
// testo; l'oreficeria per id ("" e un id che non è un intero valgono nessuna); gli altri campi tolti gli spazi ai bordi.
func TestAddestramentoCampiCorretti(t *testing.T) {
	casi := []struct {
		nome      string
		proposta  map[string]string
		busta     func(*BustaAttuale)
		corretti  string
		aMano     bool
		anagrafe  bool
		sostituto string
	}{
		{nome: "uguale", corretti: ""},
		{nome: "nome con accenti e maiuscole", proposta: map[string]string{"nome": `"mario  rossì"`}, corretti: ""},
		{nome: "nome corretto", busta: func(b *BustaAttuale) { b.Nome = "Mario Rossini" }, corretti: "nome", aMano: true},
		{nome: "telefono +39", proposta: map[string]string{"telefono": `"+39 333 000 1234"`}, corretti: ""},
		{nome: "telefono 0039", proposta: map[string]string{"telefono": `"0039 3330001234"`}, corretti: ""},
		{nome: "telefono non dettato", proposta: map[string]string{"telefono": `""`}, busta: func(b *BustaAttuale) { b.Telefono = "333 000 9999" }, corretti: ""},
		{nome: "telefono corretto", busta: func(b *BustaAttuale) { b.Telefono = "333 000 9999" }, corretti: "telefono"},
		{nome: "telefono 39 corto", proposta: map[string]string{"telefono": `"39 1234567"`}, busta: func(b *BustaAttuale) { b.Telefono = "1234567" }, corretti: "telefono"},
		{nome: "telefono numero", proposta: map[string]string{"telefono": `3330001234`}, corretti: ""},
		{nome: "consegna date uguali", proposta: map[string]string{"data_consegna": `"05/11/2026"`}, busta: func(b *BustaAttuale) { b.Consegna = "05/11/2026" }},
		{nome: "consegna date diverse", proposta: map[string]string{"data_consegna": `"05/11/2026"`}, busta: func(b *BustaAttuale) { b.Consegna = "06/11/2026" }, corretti: "data_consegna"},
		{nome: "consegna data impossibile", proposta: map[string]string{"data_consegna": `"31/11/2026"`}, busta: func(b *BustaAttuale) { b.Consegna = "01/12/2026" }, corretti: "data_consegna"},
		{nome: "consegna a parole", proposta: map[string]string{"data_consegna": `" fine mese "`}, busta: func(b *BustaAttuale) { b.Consegna = "fine mese" }},
		{nome: "oreficeria vuota e nessuna", corretti: ""},
		{nome: "oreficeria aggiunta", busta: func(b *BustaAttuale) { b.OreficeriaID, b.Oreficeria = new(int64(2)), "Esempio" }, corretti: "oreficeria"},
		{nome: "oreficeria id testo", proposta: map[string]string{"oreficeria_id": `" 2 "`}, busta: func(b *BustaAttuale) { b.OreficeriaID = new(int64(2)) }},
		{nome: "oreficeria id decimale", proposta: map[string]string{"oreficeria_id": `2.0`}, busta: func(b *BustaAttuale) { b.OreficeriaID = new(int64(2)) }, corretti: "oreficeria"},
		{nome: "note con spazi", proposta: map[string]string{"note": `"  da lucidare "`}, busta: func(b *BustaAttuale) { b.Note = "da lucidare" }},
		{nome: "note corrette", busta: func(b *BustaAttuale) { b.Note = "da lucidare" }, corretti: "note"},
		{nome: "nome dall'anagrafica", proposta: map[string]string{"nome_corretto_da": `"Mario Rosi"`}, anagrafe: true},
		{nome: "nome dall'anagrafica vuoto", proposta: map[string]string{"nome_corretto_da": `"  "`}},
		{nome: "nome dall'anagrafica numero", proposta: map[string]string{"nome_corretto_da": `5`}},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			b := bustaAttualeProva()
			if c.busta != nil {
				c.busta(&b)
			}
			d := dettatoRaccoltoProva(conProposta(t, c.proposta))
			d.Testo = "Cliente Mario Rossi 333 000 1234 orologio da revisionare"
			if b.Nome == "Mario Rossini" {
				d.Testo = "Cliente Mario Rossini 333 000 1234 orologio da revisionare"
			}
			if c.nome == "telefono non dettato" || c.nome == "telefono 39 corto" {
				d.Testo = "Cliente Mario Rossi orologio da revisionare"
			}
			r := rigaAddestramento(t, d, b)
			var got []string
			for _, v := range r["campi_corretti"].([]any) {
				got = append(got, v.(string))
			}
			if strings.Join(got, ",") != c.corretti {
				t.Errorf("campi corretti %v, attesi %q", got, c.corretti)
			}
			if r["nome_corretto_a_mano"] != c.aMano || r["nome_dall_anagrafica"] != c.anagrafe {
				t.Errorf("nome %v %v", r["nome_corretto_a_mano"], r["nome_dall_anagrafica"])
			}
		})
	}
}

// inventario: RIP-H-52
// Gli scarti prima dei pseudonimi, nell'ordine di export/5: il cliente escluso prima della busta nel cestino, poi le
// regole della 116. Una proposta che non è un oggetto è un errore; null vale una proposta vuota.
func TestAddestramentoScarti(t *testing.T) {
	escluso := bustaAttualeProva(func(b *BustaAttuale) { b.ClienteEscluso, b.NelCestino = true, true })
	if _, m := RigaPerAddestramento(dettatoRaccoltoProva(propostaBase), escluso, nil, chiaveProva); m != ScartoClienteEscluso {
		t.Errorf("escluso: %q", m)
	}
	cestino := bustaAttualeProva(func(b *BustaAttuale) { b.NelCestino = true })
	if _, m := RigaPerAddestramento(dettatoRaccoltoProva(propostaBase), cestino, nil, chiaveProva); m != ScartoBustaNelCestino {
		t.Errorf("cestino: %q", m)
	}
	d := dettatoRaccoltoProva(propostaBase)
	d.Testo = "Busta 45 anello da stringere"
	if _, m := RigaPerAddestramento(d, bustaAttualeProva(), nil, chiaveProva); m != ScartoNomeNonTrovato {
		t.Errorf("nome non trovato: %q", m)
	}
	for _, p := range []string{`[1]`, `"testo"`, `3`, `{"nome":{"a":1}}`, `{"telefono":[1]}`, `{"oreficeria_id":{"a":1}}`} {
		if j, m := RigaPerAddestramento(dettatoRaccoltoProva(p), bustaAttualeProva(), nil, chiaveProva); j != nil || (m != ScartoErrore && m != ScartoValoreIlleggibile) {
			t.Errorf("proposta %s: %q %q", p, j, m)
		}
	}
	if _, m := RigaPerAddestramento(dettatoRaccoltoProva(`{"oreficeria_id":{"a":1}}`), bustaAttualeProva(), nil, chiaveProva); m != ScartoErrore {
		t.Errorf("oreficeria_id illeggibile: %q", m)
	}
	r := rigaAddestramento(t, dettatoRaccoltoProva(`null`), bustaAttualeProva())
	if p := r["proposta"].(map[string]any); len(p) != 0 {
		t.Errorf("proposta null: %v", p)
	}
	// Nella stessa riga, nessun dato vero: né nome né telefono, in nessuna forma.
	j, _ := RigaPerAddestramento(dettatoRaccoltoProva(propostaBase), bustaAttualeProva(), nil, chiaveProva)
	if s := strings.ToLower(string(j)); strings.Contains(s, "rossi") || strings.Contains(s, "mario") || strings.Contains(strings.NewReplacer(" ", "").Replace(s), "3330001234") {
		t.Errorf("dato vero nel file: %s", j)
	}
}

// Il dettato raccolto e la busta non si stampano.
func TestAddestramentoNascosti(t *testing.T) {
	for _, v := range []any{dettatoRaccoltoProva(propostaBase), bustaAttualeProva()} {
		for _, f := range []string{"%v", "%+v", "%#v", "%s", "%q", "%d", "%x"} {
			if s := fmt.Sprintf(f, v); strings.Contains(s, "Rossi") || strings.Contains(s, "333") {
				t.Errorf("%s stampa i dati: %s", f, s)
			}
		}
	}
}
