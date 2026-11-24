package assist

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"
)

// I pseudonimi dell'esportazione (slice 116; RIP-H-52, parte dei pseudonimi), come pseudonym_test.exs dello standalone a
// go-baseline, più i casi limite chiesti dal piano. Nomi, numeri, oreficerie e dettati inventati; i telefoni hanno la
// forma 333 000 …, mai assegnata.

var chiaveProva = Chiave{b: [32]byte{7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7}}

func rispostaProva() map[string]any {
	return map[string]any{
		"nome": "Mario Rossi", "telefono": "3330001234", "descrizione": "Anello da incidere Anna 10/10/2026",
		"note": "Busta 118, richiamare Rossi", "preventivo": "40", "data_consegna": "30/10/2026",
		"oreficeria": "Esempio", "data": "23/10/2026", "compro_oro": 0,
	}
}

func propostaProva() map[string]any {
	return map[string]any{
		"nome": "Mario Rossi", "telefono": "3330001234", "descrizione": "Anello da incidere Anna 10/10/2026",
		"note": "Busta 118", "preventivo": "", "data_consegna": "", "oreficeria": "Esempio", "data": "23/10/2026",
		"compro_oro": 0,
	}
}

// rigaProva è row/1 dello standalone, con le modifiche di f.
func rigaProva(f ...func(*RigaDaEsportare)) RigaDaEsportare {
	r := RigaDaEsportare{
		ID:       1,
		Testo:    "Cliente Mario Rossi 333 000 1234 oreficeria Esempio busta 118 anello da incidere Anna 10/10/2026",
		Nomi:     []string{"Mario Rossi", "", "Mario Rossi"},
		Telefoni: []string{"3330001234", "3330001234"},
		Risposta: rispostaProva(),
		Proposta: propostaProva(),
	}
	for _, g := range f {
		g(&r)
	}
	return r
}

func conTesto(t string) func(*RigaDaEsportare) { return func(r *RigaDaEsportare) { r.Testo = t } }

func conNomi(n ...string) func(*RigaDaEsportare) { return func(r *RigaDaEsportare) { r.Nomi = n } }

func conTelefoni(n ...string) func(*RigaDaEsportare) {
	return func(r *RigaDaEsportare) { r.Telefoni = n }
}

func inRisposta(k string, v any) func(*RigaDaEsportare) {
	return func(r *RigaDaEsportare) { r.Risposta[k] = v }
}

func inProposta(k string, v any) func(*RigaDaEsportare) {
	return func(r *RigaDaEsportare) { r.Proposta[k] = v }
}

// tuttiITesti sono tutti i testi della riga, come li scrive l'esportazione.
func tuttiITesti(o RigaPseudonima) []string {
	return append(append([]string{o.Testo}, testi(o.Risposta)...), testi(o.Proposta)...)
}

func esporta(t *testing.T, r RigaDaEsportare) RigaPseudonima {
	t.Helper()
	o, m := Pseudonimizza(r, chiaveProva)
	if m != "" {
		t.Fatalf("scartata: %s", m)
	}
	return o
}

func scarta(t *testing.T, r RigaDaEsportare, atteso MotivoScarto) {
	t.Helper()
	o, m := Pseudonimizza(r, chiaveProva)
	if m != atteso {
		t.Errorf("motivo %q, atteso %q", m, atteso)
	}
	if m != "" && !reflect.DeepEqual(o, RigaPseudonima{}) {
		t.Errorf("scartata ma con un risultato: %+v", o)
	}
}

func nessunTesto(t *testing.T, o RigaPseudonima, re string) {
	t.Helper()
	r := regexp.MustCompile(re)
	for _, s := range tuttiITesti(o) {
		if r.MatchString(s) {
			t.Errorf("%s ancora in %q", re, s)
		}
	}
}

func nessunNumero(t *testing.T, o RigaPseudonima, numero string) {
	t.Helper()
	for _, s := range tuttiITesti(o) {
		if strings.Contains(soloCifre(s), numero) {
			t.Errorf("%s ancora in %q", numero, s)
		}
	}
}

// ---- nome e telefono del cliente ----

// inventario: RIP-H-52
// Nessun originale da nessuna parte; il resto resta come dettato (date, oggetti, lavori, incisioni, oreficerie, buste).
func TestPseudonimiNessunOriginaleIlRestoResta(t *testing.T) {
	o := esporta(t, rigaProva())
	nessunTesto(t, o, `(?i)\bMario\b`)
	nessunTesto(t, o, `(?i)\bRossi\b`)
	nessunNumero(t, o, "3330001234")
	if !strings.Contains(o.Testo, "oreficeria Esempio busta 118 anello da incidere Anna 10/10/2026") {
		t.Errorf("testo %q", o.Testo)
	}
	for k, v := range map[string]any{"descrizione": "Anello da incidere Anna 10/10/2026", "oreficeria": "Esempio",
		"data_consegna": "30/10/2026", "preventivo": "40", "compro_oro": 0} {
		if o.Risposta[k] != v {
			t.Errorf("%s: %v", k, o.Risposta[k])
		}
	}
}

// inventario: RIP-H-52
// Il nome atteso è quello inventato, scritto dove c'era quello vero; lo stesso cognome nelle note diventa lo stesso
// cognome inventato.
func TestPseudonimiNomeAttesoInventato(t *testing.T) {
	o := esporta(t, rigaProva())
	parti := strings.Fields(o.Risposta["nome"].(string))
	if len(parti) != 2 || !slices.Contains(NomiInventati(), parti[0]) || !slices.Contains(CognomiInventati(), parti[1]) {
		t.Fatalf("nome %q", o.Risposta["nome"])
	}
	if !strings.HasPrefix(o.Testo, "Cliente "+parti[0]+" "+parti[1]+" ") {
		t.Errorf("testo %q", o.Testo)
	}
	if o.Proposta["nome"] != o.Risposta["nome"] {
		t.Errorf("proposta %q", o.Proposta["nome"])
	}
	if o.Risposta["note"] != "Busta 118, richiamare "+parti[1] {
		t.Errorf("note %q", o.Risposta["note"])
	}
}

// inventario: RIP-H-52
// Il telefono tiene la forma e la prima cifra, ed è il telefono atteso.
func TestPseudonimiTelefonoTieneLaForma(t *testing.T) {
	o := esporta(t, rigaProva())
	m := regexp.MustCompile(` (\d{3}) (\d{3}) (\d{4}) `).FindStringSubmatch(o.Testo)
	if m == nil || m[1][0] != '3' {
		t.Fatalf("testo %q", o.Testo)
	}
	if o.Risposta["telefono"] != m[1]+m[2]+m[3] || o.Proposta["telefono"] != m[1]+m[2]+m[3] {
		t.Errorf("telefoni %v %v", o.Risposta["telefono"], o.Proposta["telefono"])
	}
}

// inventario: RIP-H-52
// Il nome del cliente dentro la descrizione (l'incisione) si sostituisce anche lì.
func TestPseudonimiNomeNellaDescrizione(t *testing.T) {
	o := esporta(t, rigaProva(conNomi("Anna Rossi", "", "Anna Rossi"),
		conTesto("Cliente Anna Rossi 333 000 1234 anello da incidere Anna 10/10/2026"),
		inRisposta("nome", "Anna Rossi"), inProposta("nome", "Anna Rossi")))
	primo := strings.Fields(o.Risposta["nome"].(string))[0]
	if o.Risposta["descrizione"] != "Anello da incidere "+primo+" 10/10/2026" {
		t.Errorf("descrizione %q", o.Risposta["descrizione"])
	}
	if !strings.Contains(o.Testo, "da incidere "+primo+" 10/10/2026") {
		t.Errorf("testo %q", o.Testo)
	}
	nessunTesto(t, o, `\bAnna\b`)
}

// inventario: RIP-H-52
// Stessa riga e stessa chiave: stesso risultato; un'altra chiave o un'altra riga: un altro.
func TestPseudonimiDeterminatoDaRigaEChiave(t *testing.T) {
	a, _ := Pseudonimizza(rigaProva(), chiaveProva)
	b, _ := Pseudonimizza(rigaProva(), chiaveProva)
	if !reflect.DeepEqual(a, b) {
		t.Error("stessa riga e chiave, risultati diversi")
	}
	altra := chiaveProva
	altra.b[0] = 8
	c, _ := Pseudonimizza(rigaProva(), altra)
	d, _ := Pseudonimizza(rigaProva(func(r *RigaDaEsportare) { r.ID = 2 }), chiaveProva)
	if reflect.DeepEqual(a, c) || reflect.DeepEqual(a, d) {
		t.Error("un'altra chiave o un'altra riga danno lo stesso risultato")
	}
	// Anche con più numeri nel testo (l'ordine dei campi non cambia le cifre inventate).
	r := func() RigaDaEsportare {
		return rigaProva(conTesto("Cliente Mario Rossi 333 000 1234 o il marito 0400 123456 anello"),
			inRisposta("note", "anche 0400 654321"), inRisposta("descrizione", "o 0400 777777"), inRisposta("oreficeria", "0400 888888"),
			inProposta("note", "o 333 000 9876"))
	}
	e, _ := Pseudonimizza(r(), chiaveProva)
	for range 20 {
		if f, _ := Pseudonimizza(r(), chiaveProva); !reflect.DeepEqual(e, f) {
			t.Fatal("con più numeri il risultato cambia fra due giri")
		}
	}
}

// inventario: RIP-H-52
// Un nome inventato non è mai uno di quelli veri, per nessuna riga.
func TestPseudonimiInventatoMaiVero(t *testing.T) {
	re := regexp.MustCompile(`Mario|Rossi`)
	for id := int64(1); id <= 200; id++ {
		o := esporta(t, rigaProva(func(r *RigaDaEsportare) { r.ID = id }))
		if re.MatchString(o.Risposta["nome"].(string)) {
			t.Fatalf("riga %d: %q", id, o.Risposta["nome"])
		}
	}
}

// ---- il nome trascritto, intero ----

// inventario: RIP-H-52
// Un nome corretto dall'operatore esporta quello trascritto (la regola «Pelizari»).
func TestPseudonimiNomeCorrettoEsportaIlTrascritto(t *testing.T) {
	o := esporta(t, rigaProva(conTesto("Cliente Pelizari 333 000 1234 orologio da revisionare"),
		conNomi("Pellizzari", "Pelizari", "Pellizzari"),
		inRisposta("nome", "Pellizzari"), inRisposta("note", "Pellizzari"), inProposta("nome", "Pellizzari")))
	finto := strings.Fields(o.Risposta["nome"].(string))
	if len(finto) != 1 || !strings.HasPrefix(o.Testo, "Cliente "+finto[0]+" 3") {
		t.Errorf("nome %q, testo %q", o.Risposta["nome"], o.Testo)
	}
	nessunTesto(t, o, `(?i)Pel+iz+ari`)
}

// inventario: RIP-H-52
// Una grafia vicina nel testo vale quando è l'unica.
func TestPseudonimiGrafiaVicinaUnica(t *testing.T) {
	o := esporta(t, rigaProva(conTesto("Cliente Pelizari 333 000 1234 orologio da revisionare"),
		conNomi("Pellizzari", "", ""), inRisposta("nome", "Pellizzari"), inProposta("nome", "")))
	if len(strings.Fields(o.Risposta["nome"].(string))) != 1 {
		t.Errorf("nome %q", o.Risposta["nome"])
	}
	nessunTesto(t, o, `(?i)Pel+iz+ari`)
}

// inventario: RIP-H-52
// Una seconda grafia del nome altrove (con la maiuscola) è un dubbio: la rete la prende.
func TestPseudonimiSecondaGrafiaAltrove(t *testing.T) {
	scarta(t, rigaProva(conTesto("Cliente Mario Rossi 333 000 1234 anello, ritira Mario Rosi")), ScartoNomeResiduo)
	scarta(t, rigaProva(inRisposta("note", "Richiamare il signor Rosi")), ScartoNomeResiduo)
}

// inventario: RIP-H-52
// Un nome eliso si sostituisce; in minuscolo è un dubbio.
func TestPseudonimiNomeEliso(t *testing.T) {
	r := rigaProva(conTesto("Cliente Elisa Bianchi 333 000 1234 l'anello dell'Elisa"), conNomi("Elisa Bianchi", "", "Elisa Bianchi"))
	o := esporta(t, r)
	primo := strings.Fields(o.Risposta["nome"].(string))[0]
	if !strings.HasSuffix(o.Testo, "l'anello dell'"+primo) {
		t.Errorf("testo %q", o.Testo)
	}
	nessunTesto(t, o, `(?i)Elisa`)
	// Con l'apostrofo tipografico è lo stesso.
	o = esporta(t, rigaProva(conTesto("Cliente Elisa Bianchi 333 000 1234 l’anello dell’Elisa"), conNomi("Elisa Bianchi", "", "Elisa Bianchi")))
	nessunTesto(t, o, `(?i)Elisa`)
	scarta(t, rigaProva(conTesto("Cliente Elisa Bianchi 333 000 1234 l'anello dell'elisa"), conNomi("Elisa Bianchi", "", "Elisa Bianchi")), ScartoNomeAmbiguo)
}

// inventario: RIP-H-52
// Un nome di sole particelle è un dubbio; un nome senza lettere non è un nome da cercare.
func TestPseudonimiNomeDiParticelleOSenzaLettere(t *testing.T) {
	scarta(t, rigaProva(conTesto("Cliente Li 333 000 1234 anello"), conNomi("Li", "", "Li")), ScartoNomeAmbiguo)
	for _, n := range []string{".", "-", "??", "3330005678"} {
		scarta(t, rigaProva(conTesto("Busta 45 anello 333 000 1234"), conNomi(n, "", n)), ScartoNomeNonTrovato)
	}
}

// inventario: RIP-H-52
// Più parole di nome che nomi inventati liberi: un dubbio, non un crollo.
func TestPseudonimiTroppeParoleDiNome(t *testing.T) {
	var v []string
	for i := 1; i <= 45; i++ {
		v = append(v, "Nome"+string(rune('a'+i/26))+string(rune('a'+i%26)))
	}
	lungo := strings.Join(v, " ")
	scarta(t, rigaProva(inProposta("nome", lungo), conNomi("Mario Rossi", "", lungo)), ScartoNomeAmbiguo)
}

// inventario: RIP-H-52
// Un valore che non è testo, numero o booleano è un dubbio; numeri e booleani passano e restano.
func TestPseudonimiValoreIlleggibile(t *testing.T) {
	scarta(t, rigaProva(inRisposta("note", []string{"Mario Rossi"})), ScartoValoreIlleggibile)
	scarta(t, rigaProva(inProposta("note", map[string]any{"x": "Mario Rossi"})), ScartoValoreIlleggibile)
	o := esporta(t, rigaProva(inRisposta("compro_oro", true), inProposta("preventivo", 40.0), inProposta("oreficeria_id", nil)))
	if o.Risposta["compro_oro"] != true || o.Proposta["preventivo"] != 40.0 || o.Proposta["oreficeria_id"] != nil {
		t.Errorf("valori %v %v", o.Risposta, o.Proposta)
	}
}

// inventario: RIP-H-52
// Un nome scritto in una parola, o diviso in due, è lo stesso nome.
func TestPseudonimiNomeInUnaParola(t *testing.T) {
	o := esporta(t, rigaProva(conTesto("Cliente Dallalana 333 000 1234 orologio"), conNomi("Dalla Lana", "", "Dalla Lana")))
	if len(strings.Fields(o.Risposta["nome"].(string))) != 1 || regexp.MustCompile(`(?i)lana`).MatchString(o.Testo) {
		t.Errorf("nome %q, testo %q", o.Risposta["nome"], o.Testo)
	}
}

// inventario: RIP-H-52
// Una parola che somiglia soltanto al nome non è il nome.
func TestPseudonimiSoloSomiglianza(t *testing.T) {
	for _, c := range []struct{ nome, testo string }{
		{"Rossi", "Cliente 333 000 1234 anello in oro Rosso"},
		{"Pellizzari", "Cliente Bellizzari 333 000 1234 orologio"},
		{"Mario Rossi", "Cliente Mario. Rossi 333 000 1234 anello"},
	} {
		t.Run(c.testo, func(t *testing.T) {
			scarta(t, rigaProva(conTesto(c.testo), conNomi(c.nome, "", "")), ScartoNomeNonTrovato)
		})
	}
}

// inventario: RIP-H-52
// Un nome vero che è anche uno degli inventati non resta mai, per nessuna riga.
func TestPseudonimiNomeVeroCheEAncheInventato(t *testing.T) {
	re := regexp.MustCompile(`Carla|Pavan`)
	for id := int64(1); id <= 60; id++ {
		o := esporta(t, rigaProva(func(r *RigaDaEsportare) { r.ID = id }, conTesto("Cliente Carla Pavan 333 000 1234 anello"),
			conNomi("Carla Pavan", "", "Carla Pavan"), inRisposta("nome", "Carla Pavan"), inRisposta("note", ""),
			inProposta("nome", "Carla Pavan")))
		for _, s := range tuttiITesti(o) {
			if re.MatchString(s) {
				t.Fatalf("riga %d: %q", id, s)
			}
		}
	}
}

// inventario: RIP-H-52
// Un nome in nessuna forma nel testo si butta.
func TestPseudonimiNomeNonTrovato(t *testing.T) {
	scarta(t, rigaProva(conTesto("Cliente 333 000 1234 orologio da revisionare"), conNomi("Giacomo Ferretti", "", "")), ScartoNomeNonTrovato)
	// Nome in parte: «Rossi» da solo non è «Mario Rossi» (manca una parola, e nessuna grafia vicina) se nessun candidato
	// è il solo cognome.
	scarta(t, rigaProva(conTesto("Cliente Rossi 333 000 1234 anello"), conNomi("Mario Rossi", "", "")), ScartoNomeNonTrovato)
}

// inventario: RIP-H-52
// Nome in parte, ma il modello aveva proposto proprio quello: il cognome trascritto è il nome atteso, e il nome proprio
// del cliente, che il testo non dice, non compare da nessuna parte.
func TestPseudonimiNomeInParteDalModello(t *testing.T) {
	o := esporta(t, rigaProva(conTesto("Cliente Rossi 333 000 1234 anello"), conNomi("Mario Rossi", "", "Rossi"),
		inProposta("nome", "Rossi")))
	if len(strings.Fields(o.Risposta["nome"].(string))) != 1 || !slices.Contains(CognomiInventati(), o.Risposta["nome"].(string)) {
		t.Errorf("nome %q", o.Risposta["nome"])
	}
	nessunTesto(t, o, `(?i)mario|rossi`)
}

// inventario: RIP-H-52
// Due grafie vicine diverse: un dubbio.
func TestPseudonimiDueGrafieVicine(t *testing.T) {
	scarta(t, rigaProva(conTesto("Cliente Pelizari 333 000 1234 da Pellizari"), conNomi("Pellizzari", "", ""),
		inRisposta("nome", "Pellizzari"), inRisposta("note", ""), inProposta("nome", "")), ScartoNomeAmbiguo)
}

// inventario: RIP-H-52
// Le particelle restano e ogni altra parola di un nome lungo si sostituisce.
func TestPseudonimiNomeLungoConParticelle(t *testing.T) {
	o := esporta(t, rigaProva(conTesto("Cliente Maria Bianchi in Rossi 333 000 1234 collana da accorciare"),
		conNomi("Maria Bianchi in Rossi", "", "Maria Bianchi")))
	parti := strings.Fields(o.Risposta["nome"].(string))
	if len(parti) != 4 || parti[2] != "in" || !strings.Contains(o.Testo, o.Risposta["nome"].(string)) {
		t.Errorf("nome %q, testo %q", o.Risposta["nome"], o.Testo)
	}
	nessunTesto(t, o, `Maria|Bianchi|Rossi`)
}

// inventario: RIP-H-52
// Una trascrizione in minuscolo resta in minuscolo, una gridata in maiuscolo; il campo nome in minuscolo non è un dubbio.
func TestPseudonimiMaiuscoleEMinuscole(t *testing.T) {
	o := esporta(t, rigaProva(conTesto("cliente dalla lana 333 000 1234 due collane da sistemare"),
		conNomi("Dalla Lana", "", "dalla lana"), inProposta("nome", "dalla lana")))
	parti := strings.Fields(o.Risposta["nome"].(string))
	if len(parti) != 2 || parti[0] != "dalla" || parti[1] != strings.ToLower(parti[1]) {
		t.Fatalf("nome %q", o.Risposta["nome"])
	}
	if o.Proposta["nome"] != "dalla "+parti[1] || !strings.HasPrefix(o.Testo, "cliente dalla "+parti[1]+" ") {
		t.Errorf("proposta %q, testo %q", o.Proposta["nome"], o.Testo)
	}
	o = esporta(t, rigaProva(conTesto("CLIENTE MARIO ROSSI 333 000 1234 ANELLO")))
	if n := o.Risposta["nome"].(string); n != strings.ToUpper(n) {
		t.Errorf("gridato: %q", n)
	}
	nessunTesto(t, o, `(?i)mario|rossi`)
	// Con la sola iniziale maiuscola il finto è com'è (iniziale maiuscola).
	o = esporta(t, rigaProva(conTesto("Cliente MARIO rossi 333 000 1234 anello")))
	parti = strings.Fields(o.Risposta["nome"].(string))
	if parti[0] != strings.ToUpper(parti[0]) || parti[1] != strings.ToLower(parti[1]) {
		t.Errorf("misto: %q", o.Risposta["nome"])
	}
}

// inventario: RIP-H-52
// Accenti: il nome con o senza accenti è lo stesso nome, in tutte e due le direzioni, e nessuna forma resta.
func TestPseudonimiAccenti(t *testing.T) {
	o := esporta(t, rigaProva(conTesto("Cliente Nicolo Zane 333 000 1234 anello"), conNomi("Nicolò Zanè", "", ""),
		inRisposta("nome", "Nicolò Zanè"), inRisposta("note", "Avvisare Zanè"), inProposta("nome", "")))
	nessunTesto(t, o, `(?i)nicol|zan[eè]\b`)
	o = esporta(t, rigaProva(conTesto("Cliente Nicolò Zanè 333 000 1234 anello"), conNomi("Nicolo Zane", "", ""),
		inRisposta("nome", "Nicolo Zane"), inProposta("nome", "")))
	nessunTesto(t, o, `(?i)nicol|zan[eè]\b`)
	if !strings.HasPrefix(o.Testo, "Cliente "+o.Risposta["nome"].(string)+" ") {
		t.Errorf("testo %q", o.Testo)
	}
	// La forma decomposta (e + accento combinante) è la stessa parola.
	o = esporta(t, rigaProva(conTesto("Cliente Nicolò Zanè 333 000 1234 anello"), conNomi("Nicolò Zanè", "", ""),
		inRisposta("nome", "Nicolò Zanè"), inProposta("nome", "")))
	nessunTesto(t, o, `(?i)nicol|zane`)
}

// inventario: RIP-H-52
// Più occorrenze: il nome intero due volte nel testo si sostituisce tutte e due le volte con lo stesso finto; il cognome
// da solo con la maiuscola anche; lo stesso cognome in minuscolo altrove è un dubbio.
func TestPseudonimiPiuOccorrenze(t *testing.T) {
	o := esporta(t, rigaProva(conTesto("Cliente Mario Rossi 333 000 1234 anello, ritira Mario Rossi o la moglie del Rossi")))
	n := o.Risposta["nome"].(string)
	cognome := strings.Fields(n)[1]
	if strings.Count(o.Testo, n) != 2 || !strings.HasSuffix(o.Testo, "la moglie del "+cognome) {
		t.Errorf("testo %q", o.Testo)
	}
	nessunTesto(t, o, `(?i)mario|rossi`)
	scarta(t, rigaProva(conTesto("Cliente Mario Rossi 333 000 1234 anello, ritira il rossi")), ScartoNomeAmbiguo)
	scarta(t, rigaProva(inRisposta("note", "richiamare mario")), ScartoNomeAmbiguo)
}

// inventario: RIP-H-52
// Un nome che è anche una parola comune altrove in minuscolo è un dubbio.
func TestPseudonimiNomeCheEParolaComune(t *testing.T) {
	scarta(t, rigaProva(conTesto("Cliente Rosa Bianco 333 000 1234 anello in oro rosa"), conNomi("Rosa Bianco", "", "Rosa Bianco")), ScartoNomeAmbiguo)
}

// inventario: RIP-H-52
// Nessun nome da nessuna parte: nessun nome, e va bene.
func TestPseudonimiNessunNome(t *testing.T) {
	o := esporta(t, rigaProva(conTesto("Busta 45 anello da stringere, preventivo 40"), conNomi("", "", ""), conTelefoni("", ""),
		inRisposta("nome", ""), inRisposta("telefono", ""), inRisposta("note", ""), inProposta("nome", ""), inProposta("telefono", "")))
	if o.Testo != "Busta 45 anello da stringere, preventivo 40" || o.Risposta["nome"] != "" || o.Risposta["telefono"] != "" {
		t.Errorf("%+v", o)
	}
	// Anche con candidati nil-equivalenti (spazi soltanto).
	o = esporta(t, rigaProva(conTesto("Busta 45 anello"), conNomi("  ", "\t"), conTelefoni(),
		inRisposta("nome", ""), inRisposta("telefono", ""), inRisposta("note", ""), inProposta("nome", ""), inProposta("telefono", "")))
	if o.Risposta["nome"] != "" {
		t.Errorf("%+v", o)
	}
}

// ---- telefoni ----

// inventario: RIP-H-52
// Con il prefisso del paese (+39 o 0039) il prefisso resta e il numero cambia; il telefono atteso è quello del testo.
func TestPseudonimiTelefonoConPrefisso(t *testing.T) {
	o := esporta(t, rigaProva(conTesto("Cliente Mario Rossi +39 333 000 1234 anello"), conTelefoni("+393330001234", ""),
		inRisposta("telefono", "+39 333 000 1234")))
	m := regexp.MustCompile(`\+39 (\d{3} \d{3} \d{4})`).FindStringSubmatch(o.Testo)
	if m == nil || o.Risposta["telefono"] != strings.ReplaceAll(m[1], " ", "") {
		t.Errorf("testo %q, telefono %v", o.Testo, o.Risposta["telefono"])
	}
	nessunNumero(t, o, "3330001234")
	o = esporta(t, rigaProva(conTesto("Cliente Mario Rossi 0039 333 000 1234 anello"), conTelefoni("00393330001234", "")))
	m = regexp.MustCompile(` 0039 (\d{3} \d{3} \d{4}) `).FindStringSubmatch(o.Testo)
	if m == nil || o.Risposta["telefono"] != strings.ReplaceAll(m[1], " ", "") {
		t.Errorf("testo %q, telefono %v", o.Testo, o.Risposta["telefono"])
	}
}

// inventario: RIP-H-52
// Lo stesso numero scritto in forme diverse (punti, trattini, gruppi diversi, attaccato) si trova e si sostituisce,
// tenendo la forma.
func TestPseudonimiTelefonoInFormeDiverse(t *testing.T) {
	for _, c := range []struct{ testo, forma string }{
		{"Cliente Mario Rossi 333.000.1234 anello", `^Cliente \S+ \S+ 3\d{2}\.\d{3}\.\d{4} anello$`},
		{"Cliente Mario Rossi 333-000-1234 anello", `^Cliente \S+ \S+ 3\d{2}-\d{3}-\d{4} anello$`},
		{"Cliente Mario Rossi 3330001234 anello", `^Cliente \S+ \S+ 3\d{9} anello$`},
		{"Cliente Mario Rossi 333 0001234 anello", `^Cliente \S+ \S+ 3\d{2} \d{7} anello$`},
		{"Cliente Mario Rossi 33 30 00 12 34 anello", `^Cliente \S+ \S+ 3\d \d{2} \d{2} \d{2} \d{2} anello$`},
	} {
		t.Run(c.testo, func(t *testing.T) {
			o := esporta(t, rigaProva(conTesto(c.testo), conTelefoni("+39 333 000 1234", "")))
			if !regexp.MustCompile(c.forma).MatchString(o.Testo) {
				t.Errorf("testo %q", o.Testo)
			}
			nessunNumero(t, o, "3330001234")
			if o.Risposta["telefono"] != soloCifre(regexp.MustCompile(`3[\d .\-]+\d`).FindString(o.Testo)) {
				t.Errorf("telefono %v, testo %q", o.Risposta["telefono"], o.Testo)
			}
		})
	}
}

// inventario: RIP-H-52
// D-111 (2): un numero noto diviso da una barra, una virgola o più spazi si trova e si sostituisce, separatori compresi
// al loro posto; diviso da un segno che non è un separatore (il punto e virgola) non esce mai com'era: la rete lo prende.
func TestPseudonimiTelefonoDiviso(t *testing.T) {
	for _, c := range []struct{ testo, tel, forma string }{
		{"Cliente Mario Rossi 0400/00 1234 anello", "0400001234", `^Cliente \S+ \S+ 0\d{3}/\d{2} \d{4} anello$`},
		{"Cliente Mario Rossi 333, 0001234 anello", "3330001234", `^Cliente \S+ \S+ 3\d{2}, \d{7} anello$`},
		{"Cliente Mario Rossi 333  0001234 anello", "3330001234", `^Cliente \S+ \S+ 3\d{2}  \d{7} anello$`},
		{"Cliente Mario Rossi 333/000/1234 anello", "3330001234", `^Cliente \S+ \S+ 3\d{2}/\d{3}/\d{4} anello$`},
	} {
		t.Run(c.testo, func(t *testing.T) {
			o := esporta(t, rigaProva(conTesto(c.testo), conTelefoni(c.tel, c.tel)))
			nessunNumero(t, o, c.tel)
			if !regexp.MustCompile(c.forma).MatchString(o.Testo) {
				t.Errorf("testo %q", o.Testo)
			}
		})
	}
	o := esporta(t, rigaProva(inRisposta("note", "richiamare il 333/000/1234")))
	nessunNumero(t, o, "3330001234")
	scarta(t, rigaProva(conTesto("Cliente Mario Rossi 333000;1234 anello")), ScartoTelefonoResiduo)
	scarta(t, rigaProva(inRisposta("note", "richiamare il 333000;1234")), ScartoTelefonoResiduo)
}

// inventario: RIP-H-52
// Un secondo numero nel testo si sostituisce anche lui, con la sua prima cifra.
func TestPseudonimiSecondoNumero(t *testing.T) {
	o := esporta(t, rigaProva(conTesto("Cliente Mario Rossi 333 000 1234 o il marito 0400 123456 anello")))
	m := regexp.MustCompile(`marito (0\d{3} \d{6}) anello`).FindStringSubmatch(o.Testo)
	if m == nil || m[1] == "0400 123456" {
		t.Errorf("testo %q", o.Testo)
	}
	nessunNumero(t, o, "0400123456")
	// Lo stesso secondo numero in un campo diventa lo stesso numero inventato.
	o = esporta(t, rigaProva(conTesto("Cliente Mario Rossi 333 000 1234 o il marito 0400 123456 anello"),
		inRisposta("note", "marito 0400123456")))
	m = regexp.MustCompile(`marito (0\d{3} \d{6}) anello`).FindStringSubmatch(o.Testo)
	if m == nil || o.Risposta["note"] != "marito "+strings.ReplaceAll(m[1], " ", "") {
		t.Errorf("testo %q, note %q", o.Testo, o.Risposta["note"])
	}
}

// inventario: RIP-H-52
// Un telefono attaccato a un numero dopo si trova e si sostituisce lo stesso.
func TestPseudonimiTelefonoAttaccato(t *testing.T) {
	o := esporta(t, rigaProva(conTesto("Cliente Mario Rossi 333 000 1234 10 anelli")))
	nessunNumero(t, o, "3330001234")
	if !strings.Contains(o.Testo, " 10 anelli") {
		t.Errorf("testo %q", o.Testo)
	}
}

// inventario: RIP-H-52
// Un numero nel testo che non è di nessuno dei noti è un dubbio (telefoni sconosciuti), anche con la forma di un fisso.
func TestPseudonimiTelefonoSconosciuto(t *testing.T) {
	scarta(t, rigaProva(conTelefoni("", ""), inRisposta("telefono", ""), inProposta("telefono", "")), ScartoTelefonoDubbio)
	scarta(t, rigaProva(conTesto("Cliente Mario Rossi 0400 123456 anello"), conTelefoni("", ""), inRisposta("telefono", ""),
		inProposta("telefono", "")), ScartoTelefonoDubbio)
	// Un noto diverso da quello nel testo non basta.
	scarta(t, rigaProva(conTelefoni("3330009999", "")), ScartoTelefonoDubbio)
	// Un telefono noto troppo corto non si cerca: il numero nel testo resta di nessuno.
	scarta(t, rigaProva(conTelefoni("12345", "")), ScartoTelefonoDubbio)
}

// inventario: RIP-H-52
// Date e numeri corti non sono telefoni.
func TestPseudonimiDateENumeriCorti(t *testing.T) {
	o := esporta(t, rigaProva(conTesto("Cliente Mario Rossi 333 000 1234 consegna 10.10.2026 busta 1234 preventivo 50 matricola 123456789 ritiro 03.10.2026"),
		inRisposta("note", "Busta 1234 03.10.2026")))
	if !strings.Contains(o.Testo, "consegna 10.10.2026 busta 1234 preventivo 50 matricola 123456789 ritiro 03.10.2026") ||
		o.Risposta["note"] != "Busta 1234 03.10.2026" {
		t.Errorf("testo %q, note %q", o.Testo, o.Risposta["note"])
	}
}

// inventario: RIP-H-52
// I nomi inventati sono nomi (una parola, iniziale maiuscola), e nessuno è una parola dei nomi comuni o del mestiere.
func TestPseudonimiNomiInventatiSonoNomi(t *testing.T) {
	for _, n := range append(NomiInventati(), CognomiInventati()...) {
		if w := reParola.FindAllString(n, -1); len(w) != 1 || w[0] != n || titolo(n) != n {
			t.Errorf("%q", n)
		}
	}
}

// ---- i nomi degli altri ----

// inventario: RIP-H-52
// Le parole dei nomi: niente particelle, parole corte, titoli o parole del negozio.
func TestPseudonimiParoleDeiNomi(t *testing.T) {
	w := ParoleDeiNomi([]string{"Sig.ra De Rossi", "Cliente Bianchi in Neri", "D'Angelo Nicolò", "Li Po", "Dell' Orto", "Maria Della Valle"})
	for _, k := range []string{"valle", "orto", "rossi", "bianchi", "neri", "dangelo", "nicolo"} {
		if !w[k] {
			t.Errorf("manca %q", k)
		}
	}
	for _, k := range []string{"dell", "della", "po", "sig", "ra", "de", "cliente", "in", "li"} {
		if w[k] {
			t.Errorf("c'è %q", k)
		}
	}
}

// inventario: RIP-H-52
// I nomi propri comuni sono una lista a sé, senza le parole del mestiere.
func TestPseudonimiNomiPropriComuni(t *testing.T) {
	w := ParoleDeiNomi(NomiPropriComuni())
	if !w["giovanni"] || !w["francesca"] {
		t.Error("mancano nomi comuni")
	}
	for _, k := range []string{"rosa", "gemma", "perla", "ambra", "stella", "oro"} {
		if w[k] {
			t.Errorf("c'è %q", k)
		}
	}
}

// inventario: RIP-H-52
// Una parola intera del nome di un altro, ovunque (in qualunque maiuscola, o elisa), butta la riga.
func TestPseudonimiAltroNome(t *testing.T) {
	altri := ParoleDeiNomi([]string{"Teresa Zanchetta"})
	for _, testo := range []string{
		"Cliente Mario Rossi 333 000 1234 anello, ritira la zanchetta",
		"Cliente Mario Rossi 333 000 1234 anello, ritira ZANCHETTA",
		"Cliente Mario Rossi 333 000 1234 anello dell'Teresa",
		"Cliente Mario Rossi 333 000 1234 anello per Zanchettà",
	} {
		scarta(t, rigaProva(conTesto(testo), func(r *RigaDaEsportare) { r.Altri = altri }), ScartoAltroNome)
	}
	// Anche nei campi, le note della busta comprese.
	scarta(t, rigaProva(inRisposta("note", "Avvisare Teresa"), func(r *RigaDaEsportare) { r.Altri = altri }), ScartoAltroNome)
	scarta(t, rigaProva(inProposta("descrizione", "per teresa"), func(r *RigaDaEsportare) { r.Altri = altri }), ScartoAltroNome)
}

// senzaAnna toglie «Anna» dall'incisione della riga di prova.
func senzaAnna(testo string) func(*RigaDaEsportare) {
	return func(r *RigaDaEsportare) {
		r.Testo = testo
		r.Risposta["descrizione"] = "Anello"
		r.Proposta["descrizione"] = "Anello"
	}
}

// inventario: RIP-H-52
// Solo parole intere: una parola più lunga o più corta è un altro.
func TestPseudonimiAltroNomeSoloParoleIntere(t *testing.T) {
	esporta(t, rigaProva(senzaAnna("Cliente Mario Rossi 333 000 1234 oreficeria Esempio busta 118 anello Annamaria Annina 10/10/2026"),
		func(r *RigaDaEsportare) { r.Altri = ParoleDeiNomi([]string{"Anna Rossetti"}) }))
}

// inventario: RIP-H-52
// Le parole del cliente non sono un altro nome, anche se un altro cliente le porta.
func TestPseudonimiAltroNomeParoleDelCliente(t *testing.T) {
	esporta(t, rigaProva(senzaAnna("Cliente Mario Rossi 333 000 1234 oreficeria Esempio busta 118 anello"),
		func(r *RigaDaEsportare) { r.Altri = ParoleDeiNomi([]string{"Mario Bianchi", "Giulia Rossi"}) }))
}

// inventario: RIP-H-52
// Le parole del mestiere non sono nomi, nemmeno come cognome di un cliente.
func TestPseudonimiParoleDelMestiereNonSonoNomi(t *testing.T) {
	w := ParoleDeiNomi([]string{"Maria Oro", "Luca Argento", "Anna Perla", "Paolo Rubino", "Sara Costa", "Rosa Bianco",
		"Gino Smeraldo", "Ivo Zaffiro", "Ada Diamante", "Eva Corallo", "Ugo Platino", "Lia Fede", "Rino Croce",
		"Elio Catena", "Dino Corona", "Lina Pila", "Nino Giallo"})
	for _, k := range strings.Fields("oro argento perla rubino costa rosa bianco smeraldo zaffiro diamante corallo platino fede croce catena corona pila giallo") {
		if w[k] {
			t.Errorf("%q contato come nome", k)
		}
	}
	for _, k := range strings.Fields("maria luca anna paolo sara") {
		if !w[k] {
			t.Errorf("manca %q", k)
		}
	}
}

// inventario: RIP-H-52
// Un dettato pieno di parole del mestiere resta; un altro cognome vero lo butta.
func TestPseudonimiDettatoDelMestiere(t *testing.T) {
	altri := ParoleDeiNomi([]string{"Sara Costa", "Rosa Bianco", "Maria Oro", "Teresa Zanchetta"})
	testo := "Cliente Mario Rossi 333 000 1234 anello in oro rosa, quanto costa? fede in argento"
	r := func(t string) RigaDaEsportare {
		return rigaProva(conTesto(t), inRisposta("descrizione", "Anello in oro rosa"), inProposta("descrizione", "Anello in oro rosa"),
			func(r *RigaDaEsportare) { r.Altri = altri })
	}
	esporta(t, r(testo))
	scarta(t, r(testo+" per Zanchetta"), ScartoAltroNome)
}

// inventario: RIP-H-52
// ParoleDiAltriNomi (other_names/0 dell'esportazione): le parole di tutti i clienti e dei nomi comuni, meno quelle delle
// oreficerie, che restano nel file.
func TestPseudonimiParoleDiAltriNomi(t *testing.T) {
	w := ParoleDiAltriNomi([]string{"Teresa Zanchetta", "Ugo Esempietto"}, []string{"Oreficeria Esempietto"})
	if !w["zanchetta"] || !w["teresa"] || !w["giovanni"] || w["esempietto"] || w["oreficeria"] {
		t.Errorf("%v", slices.Sorted(maps.Keys(w)))
	}
	// L'oreficeria nel testo non butta la riga; un nome comune di nessun cliente sì.
	altri := ParoleDiAltriNomi([]string{"Mario Rossi", "Teresa Zanchetta"}, []string{"Oreficeria Esempio"})
	esporta(t, rigaProva(senzaAnna("Cliente Mario Rossi 333 000 1234 oreficeria Esempio busta 118 anello di Rossi"),
		func(r *RigaDaEsportare) { r.Altri = altri }))
	scarta(t, rigaProva(senzaAnna("Cliente Mario Rossi 333 000 1234 anello per il figlio Giovanni"),
		func(r *RigaDaEsportare) { r.Altri = altri }), ScartoAltroNome)
}

// ---- cliente escluso, chiave, riservatezza ----

// inventario: RIP-H-52
// Il cliente della busta si è opposto: la riga si butta prima di tutto, qualunque cosa contenga.
func TestPseudonimiClienteEscluso(t *testing.T) {
	escluso := func(r *RigaDaEsportare) { r.ClienteEscluso = true }
	scarta(t, rigaProva(escluso), ScartoClienteEscluso)
	scarta(t, rigaProva(escluso, inRisposta("note", []string{"x"}), func(r *RigaDaEsportare) { r.Altri = ParoleDeiNomi([]string{"Mario"}) }),
		ScartoClienteEscluso)
}

// inventario: RIP-H-52
// La chiave viene da crypto/rand (due chiavi nuove sono diverse e non vuote) e non si vede mai: né con fmt, né nel
// registro slog, né nel risultato; anche la riga con i dati veri si nasconde.
func TestPseudonimiChiaveNascosta(t *testing.T) {
	a, err := NuovaChiave()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NuovaChiave()
	if a == b || a == (Chiave{}) {
		t.Fatal("chiavi uguali o vuote")
	}
	esa := hex.EncodeToString(a.b[:])
	var reg bytes.Buffer
	l := slog.New(slog.NewTextHandler(&reg, nil))
	l.Info("x", "chiave", a, "riga", rigaProva())
	l.Info("x", slog.Any("chiave", a), slog.Any("riga", rigaProva()))
	s := fmt.Sprintf("%v %+v %#v %s %v %+v %#v %x", a, a, a, a, rigaProva(), rigaProva(), rigaProva(), a) + reg.String()
	o, _ := Pseudonimizza(rigaProva(), a)
	s += fmt.Sprintf("%#v", o)
	for _, vietato := range []string{esa, fmt.Sprint(a.b[:]), "Mario", "Rossi", "3330001234", "333 000 1234"} {
		if strings.Contains(s, vietato) {
			t.Errorf("%q si vede: %s", vietato, s)
		}
	}
}

// inventario: RIP-H-52
// La rete finale non dipende dall'ordine: un nome vero nascosto in un campo che le regole lasciano (il campo «nome» di
// una proposta con un altro cliente, per esempio) butta la riga.
func TestPseudonimiReteFinale(t *testing.T) {
	// Il nome proposto è un cognome vero che il testo non dice: si sostituisce nel campo nome (tabella dei candidati).
	o := esporta(t, rigaProva(conNomi("Mario Rossi", "", "Mario Rossetto"), inProposta("nome", "Mario Rossetto")))
	nessunTesto(t, o, `(?i)rossetto`)
	// Un pezzo eliso di un nome vero in minuscolo nelle note è un dubbio.
	scarta(t, rigaProva(inRisposta("note", "l'anello dell'rossi")), ScartoNomeAmbiguo)
}

// inventario: RIP-H-52
// Fra le parole di un nome valgono gli spazi del \s di PCRE con /u: anche lo spazio non separabile (misurato con Elixir
// 1.18). U+180E e U+200B sono caratteri di formato: dalla D-111 (1) si tolgono prima di tutto, e il nome scritto attaccato
// si trova come una parola sola (nello standalone U+200B lo divideva e il nome non si trovava).
func TestPseudonimiSpaziFraLeParole(t *testing.T) {
	for _, sp := range []string{"\u00a0", "\u2028", "\t"} {
		o := esporta(t, rigaProva(conTesto("Cliente Mario"+sp+"Rossi 333 000 1234 anello")))
		nessunTesto(t, o, `(?i)mario|rossi`)
		if !strings.HasPrefix(o.Testo, "Cliente "+reParola.FindString(o.Risposta["nome"].(string))+sp) {
			t.Errorf("%q: testo %q", sp, o.Testo)
		}
	}
	for _, f := range []string{"\u200b", "\u180e"} {
		o := esporta(t, rigaProva(conTesto("Cliente Mario"+f+"Rossi 333 000 1234 anello"), conNomi("Mario Rossi", "", "")))
		nessunTesto(t, o, `(?i)mario|rossi`)
	}
}

// inventario: RIP-H-52
// Un cellulare che comincia con 39 (10 cifre) non perde il «39» come se fosse il prefisso del paese: il telefono atteso
// ha 10 cifre.
func TestPseudonimiCellulareCheIniziaCon39(t *testing.T) {
	o := esporta(t, rigaProva(conTesto("Cliente Mario Rossi 393 300 0123 anello"), conTelefoni("3933000123", "")))
	if tel := o.Risposta["telefono"].(string); len(tel) != 10 || tel[0] != '3' {
		t.Errorf("telefono %q", tel)
	}
	nessunNumero(t, o, "3933000123")
}

// inventario: RIP-H-52
// Un cognome con l'apostrofo (D'Angelo) è una parola della tabella intera: con la maiuscola fuori dal nome si sostituisce,
// in minuscolo è un dubbio.
func TestPseudonimiCognomeConApostrofo(t *testing.T) {
	r := func(testo string) RigaDaEsportare {
		return rigaProva(conTesto(testo), conNomi("Luca D'Angelo", "", "Luca D'Angelo"), inRisposta("nome", "Luca D'Angelo"),
			inRisposta("note", ""), inProposta("nome", "Luca D'Angelo"))
	}
	o := esporta(t, r("Cliente Luca D'Angelo 333 000 1234 anello per D'Angelo"))
	nessunTesto(t, o, `(?i)angelo`)
	scarta(t, r("Cliente Luca D'Angelo 333 000 1234 anello per d'angelo"), ScartoNomeAmbiguo)
}

// inventario: RIP-H-52
// Anche con il registro JSON di slog la chiave e la riga con i dati veri non si vedono.
func TestPseudonimiRegistroJSON(t *testing.T) {
	var reg bytes.Buffer
	slog.New(slog.NewJSONHandler(&reg, nil)).Info("x", "chiave", chiaveProva, "riga", rigaProva())
	if s := reg.String(); strings.Contains(s, "Mario") || strings.Contains(s, "3330001234") || strings.Contains(s, "[7 7") ||
		!strings.Contains(s, "redacted") {
		t.Errorf("registro %s", s)
	}
}

// ---- correzioni dopo la revisione ----

// inventario: RIP-H-52
// Jaro come String.jaro_distance/2 di Elixir 1.18 (valori misurati nell'immagine dello standalone): (M - T/2)/M in
// virgola, anche con un numero dispari di caratteri fuori posto.
func TestPseudonimiJaroComeElixir(t *testing.T) {
	for _, c := range []struct {
		a, b string
		v    float64
	}{
		{"dwayne", "duane", 0.8222222222222223},
		{"martha", "marhta", 0.9444444444444445},
		{"bniahci", "bianchi", 0.8809523809523809},
		{"pellizzari", "pelizari", 0.9333333333333332},
	} {
		if got := jaro(c.a, c.b); got < c.v-1e-12 || got > c.v+1e-12 {
			t.Errorf("%s/%s: %v, Elixir %v", c.a, c.b, got, c.v)
		}
	}
}

// valoriInChiaro dice se un valore qualsiasi della riga (anche un numero) contiene le cifre n.
func valoriInChiaro(o RigaPseudonima, n string) bool {
	for _, m := range []map[string]any{o.Risposta, o.Proposta} {
		for _, v := range m {
			if strings.Contains(soloCifre(fmt.Sprint(v)), n) {
				return true
			}
		}
	}
	return strings.Contains(soloCifre(o.Testo), n)
}

// inventario: RIP-H-52
// Un telefono scritto come numero (json.Number o intero) si tratta come testo: si sostituisce, e se restasse la rete lo
// prende.
func TestPseudonimiTelefonoComeNumero(t *testing.T) {
	for _, c := range []struct {
		nome string
		r    RigaDaEsportare
	}{
		{"json.Number nella proposta", rigaProva(inProposta("telefono", json.Number("3330001234")))},
		{"int64 nelle note", rigaProva(inRisposta("note", int64(3330001234)))},
		{"int nella descrizione della proposta", rigaProva(inProposta("descrizione", 3330001234))},
	} {
		t.Run(c.nome, func(t *testing.T) {
			o, m := Pseudonimizza(c.r, chiaveProva)
			if m == "" && valoriInChiaro(o, "3330001234") {
				t.Errorf("numero in chiaro: %v %v", o.Risposta, o.Proposta)
			}
		})
	}
	// Un numero sconosciuto con la forma di un telefono, scritto come numero in un campo, si sostituisce anche lui.
	o, m := Pseudonimizza(rigaProva(inRisposta("note", json.Number("0400123456"))), chiaveProva)
	if m == "" && valoriInChiaro(o, "0400123456") {
		t.Errorf("numero sconosciuto in chiaro: %v", o.Risposta)
	}
	// Numeri che non sono telefoni restano come sono, dello stesso tipo.
	o = esporta(t, rigaProva(inRisposta("preventivo", json.Number("40")), inRisposta("compro_oro", 0)))
	if o.Risposta["preventivo"] != json.Number("40") || o.Risposta["compro_oro"] != 0 {
		t.Errorf("numeri cambiati: %#v %#v", o.Risposta["preventivo"], o.Risposta["compro_oro"])
	}
}

// inventario: RIP-H-52
// Chiave e riga con i dati veri restano nascoste con ogni verbo di fmt.
func TestPseudonimiNascosteConOgniVerbo(t *testing.T) {
	r := rigaProva()
	for _, verbo := range []string{"%d", "%x", "%X", "%v", "%+v", "%#v", "%s", "%q", "%o", "%b"} {
		s := fmt.Sprintf(verbo, chiaveProva) + fmt.Sprintf(verbo, &chiaveProva) + fmt.Sprintf(verbo, r) + fmt.Sprintf(verbo, &r)
		for _, vietato := range []string{"7 7 7", "[7", "0707", "Mario", "Rossi", "3330001234", "333 000 1234", "Esempio"} {
			if strings.Contains(s, vietato) {
				t.Errorf("%s: %q si vede in %s", verbo, vietato, s)
			}
		}
	}
}

// inventario: RIP-H-52
// La rete finale, da sola: una parola o un pezzo eliso di un nome vero rimasto, una seconda grafia con la maiuscola, un
// telefono vero fra le cifre messe in fila.
func TestPseudonimiControllaResidui(t *testing.T) {
	tab := map[string]string{"mario": "Ugo", "rossi": "Pavan"}
	nomi := []string{"Mario Rossi"}
	tel := []string{"3330001234"}
	riga := func(testo string, note any) RigaPseudonima {
		return RigaPseudonima{Testo: testo, Risposta: map[string]any{"note": note}, Proposta: map[string]any{}}
	}
	for _, c := range []struct {
		r RigaPseudonima
		m MotivoScarto
	}{
		{riga("Cliente Ugo Pavan anello", ""), ""},
		{riga("Cliente Ugo Pavan anello", "ritira rossi"), ScartoNomeResiduo},
		{riga("Cliente Ugo Pavan dell'Mario", ""), ScartoNomeResiduo},
		{riga("Cliente Ugo Pavan dell'mario", ""), ScartoNomeResiduo},
		{riga("Cliente Ugo Pavan anello", "ritira Rosi"), ScartoNomeResiduo},
		{riga("Cliente Ugo Pavan anello", "ritira rosi"), ""},
		{riga("Cliente Ugo Pavan 333/000/1234", ""), ScartoTelefonoResiduo},
	} {
		if m := controllaResidui(c.r, nomi, tab, tel); m != c.m {
			t.Errorf("%q %v: %q, atteso %q", c.r.Testo, c.r.Risposta["note"], m, c.m)
		}
	}
	// Una parola vera che c'è solo fra i candidati (non nella tabella) conta lo stesso.
	if m := controllaResidui(riga("Cliente Ugo Pavan Bianchi", ""), []string{"Mario Rossi", "Anna Bianchi"}, tab, tel); m != ScartoNomeResiduo {
		t.Errorf("candidato fuori tabella: %q", m)
	}
}

// inventario: RIP-H-52
// Un numero sconosciuto di 9 cifre (il minimo di un telefono) è un dubbio.
func TestPseudonimiTelefonoSconosciutoNoveCifre(t *testing.T) {
	scarta(t, rigaProva(conTesto("Cliente Mario Rossi 0400 12345 anello")), ScartoTelefonoDubbio)
	// Otto cifre no.
	esporta(t, rigaProva(conTesto("Cliente Mario Rossi 333 000 1234 matricola 04001234")))
}

// inventario: RIP-H-52
// Un nome di un altro di tre lettere (Eva, Ida) conta: la riga si butta.
func TestPseudonimiAltroNomeDiTreLettere(t *testing.T) {
	altri := ParoleDeiNomi([]string{"Eva Zanchetta", "Ida Esempietto"})
	for _, testo := range []string{"Cliente Mario Rossi 333 000 1234 anello per Eva", "Cliente Mario Rossi 333 000 1234 anello per ida"} {
		scarta(t, rigaProva(conTesto(testo), func(r *RigaDaEsportare) { r.Altri = altri }), ScartoAltroNome)
	}
}

// inventario: RIP-H-52
// Una parola di due lettere nel nome (Bo) identifica: si sostituisce e non resta da nessuna parte.
func TestPseudonimiParolaDiDueLettere(t *testing.T) {
	o := esporta(t, rigaProva(conTesto("Cliente Mario Bo 333 000 1234 anello per Bo"), conNomi("Mario Bo", "", "Mario Bo"),
		inRisposta("nome", "Mario Bo"), inRisposta("note", ""), inProposta("nome", "Mario Bo")))
	nessunTesto(t, o, `(?i)\bbo\b`)
	if len(strings.Fields(o.Risposta["nome"].(string))) != 2 {
		t.Errorf("nome %q", o.Risposta["nome"])
	}
}

// ---- D-111 ----

// inventario: RIP-H-52
// D-111 (1): i caratteri di formato (\p{Cf}: ZWJ, ZWNJ, trattino morbido, BOM…) si tolgono prima di tutto, e U+02BC,
// U+02B9, U+2032 valgono come apostrofi: un nome scritto con uno di questi non passa in chiaro.
func TestPseudonimiD111CaratteriDiFormato(t *testing.T) {
	for _, testo := range []string{
		"Cliente Mario Rossi 333 000 1234 anello dell\u02bcRossi",
		"Cliente Mario Rossi 333 000 1234 anello dell\u02b9Rossi",
		"Cliente Mario Rossi 333 000 1234 anello dell\u2032Rossi",
		"Cliente Mario Rossi 333 000 1234 anello per Ros\u200dsi",
		"Cliente Mario Rossi 333 000 1234 anello per Ros\u200csi",
		"Cliente Mario Rossi 333 000 1234 anello per Ros\u00adsi",
		"Cliente Mario Rossi 333 000 1234 anello per \ufeffRossi",
		"Cliente Ma\u00adrio Ros\u200dsi 333 000 1234 anello",
	} {
		t.Run(testo, func(t *testing.T) {
			o, m := Pseudonimizza(rigaProva(conTesto(testo)), chiaveProva)
			if m != "" {
				return // scartata: va bene
			}
			for _, s := range tuttiITesti(o) {
				pulito := strings.Map(func(r rune) rune {
					if unicode.Is(unicode.Cf, r) {
						return -1
					}
					return r
				}, s)
				if regexp.MustCompile(`(?i)rossi|mario`).MatchString(pulito) {
					t.Errorf("nome in chiaro: %q", s)
				}
			}
		})
	}
	// Anche nei campi e nei candidati.
	o, m := Pseudonimizza(rigaProva(inRisposta("note", "richiamare Ros\u200dsi")), chiaveProva)
	if m == "" && strings.Contains(fmt.Sprint(o.Risposta["note"]), "Ros") {
		t.Errorf("note %q", o.Risposta["note"])
	}
	o = esporta(t, rigaProva(conNomi("Mario Ros\u00adsi", "", ""), inRisposta("nome", "Mario Ros\u00adsi"), inProposta("nome", "")))
	nessunTesto(t, o, `(?i)rossi|mario`)
}

// inventario: RIP-H-52
// D-111 (2): un numero sconosciuto scritto con barra, spazio non separabile, spazi multipli o con un prefisso estero si
// vede lo stesso: telefono_dubbio.
func TestPseudonimiD111TelefoniSconosciuti(t *testing.T) {
	for _, n := range []string{"0400/121234", "333\u00a0123\u00a04567", "333  123  4567", "+41 79 123 45 67", "0041 79 123 45 67",
		"333\u2009123\u20094567", "333, 123 4567"} {
		t.Run(n, func(t *testing.T) {
			scarta(t, rigaProva(conTesto("Cliente Mario Rossi o il marito "+n+" anello")), ScartoTelefonoDubbio)
			// D-111 (1b), coordinatore 24/11/2026: con il numero del cliente accanto, un secondo numero che solo le
			// forme nuove riconoscono non si riscrive: la riga si scarta.
			scarta(t, rigaProva(conTesto("Cliente Mario Rossi 333 000 1234 o il marito "+n+" anello")), ScartoTelefonoDubbio)
			// Anche in un campo.
			scarta(t, rigaProva(inRisposta("note", "o il marito "+n)), ScartoTelefonoDubbio)
		})
	}
}

// inventario: RIP-H-52
// D-111 (2), che cosa non è un telefono: importi, date, numeri di busta e pesi da soli o in fila come nei dettati.
// Misurato: qui sotto i casi che passano; quelli che la regola butterebbe sono in TestPseudonimiD111FalsiPositivi.
func TestPseudonimiD111NonTelefoni(t *testing.T) {
	for _, n := range []string{
		"preventivo 1.250,00 euro", "consegna 24/11/2026", "busta 0045/26", "peso 12,5 g", "oro 18 kt 3,45 grammi",
		"consegna 24/11/2026 preventivo 1.250,00", "busta 0045/26 del 24/11/2026", "matricola 123456789",
		"busta 1234 03.10.2026",
	} {
		t.Run(n, func(t *testing.T) {
			o := esporta(t, rigaProva(conTesto("Cliente Mario Rossi 333 000 1234 "+n)))
			if !strings.HasSuffix(o.Testo, " "+n) {
				t.Errorf("testo %q", o.Testo)
			}
		})
	}
}

// inventario: RIP-H-52
// D-111 (1) anche sui candidati: un cognome scritto con un trattino morbido in un nome candidato che il testo non dice è
// lo stesso cognome; detto altrove con la maiuscola si sostituisce.
func TestPseudonimiD111CandidatoConFormato(t *testing.T) {
	o := esporta(t, rigaProva(conTesto("Cliente Anna Bianchi 333 000 1234 anello, ritira Rossi"),
		conNomi("Anna Bianchi", "", "Anna Ros\u00adsi"), inRisposta("nome", "Anna Bianchi"), inRisposta("note", ""),
		inProposta("nome", "Anna Ros\u00adsi")))
	nessunTesto(t, o, `(?i)rossi|bianchi|anna`)
}

// inventario: RIP-H-52
// D-111 (2), correzione del coordinatore (24/11): una fila che comincia con «00» si unisce solo attraverso gli spazi, così
// numeri di busta e date in fila non sembrano un prefisso estero. Senza il telefono del cliente nel testo la riga non si
// scarta; con il telefono nel testo busta e date restano intatte. «0041 79 123 45 67» resta un telefono.
func TestPseudonimiD111FilaCon00(t *testing.T) {
	for _, n := range []string{"busta 0045/26 24/11/2026", "buste 0045/26, 0046/26", "0045/26 0046/26 0047/26"} {
		t.Run(n, func(t *testing.T) {
			o := esporta(t, rigaProva(conTesto("Cliente Mario Rossi "+n), conTelefoni("", ""), inRisposta("telefono", ""),
				inProposta("telefono", "")))
			if !strings.HasSuffix(o.Testo, " "+n) {
				t.Errorf("senza telefono: %q", o.Testo)
			}
			o = esporta(t, rigaProva(conTesto("Cliente Mario Rossi 333 000 1234 "+n)))
			if !strings.HasSuffix(o.Testo, " "+n) {
				t.Errorf("con telefono: %q", o.Testo)
			}
			nessunNumero(t, o, "3330001234")
		})
	}
	scarta(t, rigaProva(conTesto("Cliente Mario Rossi 0041 79 123 45 67 anello")), ScartoTelefonoDubbio)
	scarta(t, rigaProva(conTesto("Cliente Mario Rossi 0041\u00a079\u00a0123\u00a045\u00a067 anello")), ScartoTelefonoDubbio)
}

// ---- revisione mirata e D-111 aggiornata (24/11/2026) ----

// conESenza esegue f con il testo t senza il telefono del cliente e con il telefono del cliente davanti.
func conESenza(t *testing.T, n string, f func(t *testing.T, r RigaDaEsportare)) {
	t.Run("senza:"+n, func(t *testing.T) {
		f(t, rigaProva(conTesto("Cliente Mario Rossi "+n), conTelefoni("", ""), inRisposta("telefono", ""), inProposta("telefono", "")))
	})
	t.Run("con:"+n, func(t *testing.T) {
		f(t, rigaProva(conTesto("Cliente Mario Rossi 333 000 1234 "+n)))
	})
}

// inventario: RIP-H-52
// «00» tagliato a un separatore che non è uno spazio: «00NN» si rilegge con la fila che segue (unita dagli spazi o dallo
// stesso separatore); con almeno 9 cifre dopo il «00» è un telefono estero. Con o senza il telefono del cliente:
// telefono_dubbio (D-111 (1b)), mai in chiaro. Busta e data restano fuori.
func TestPseudonimi00TagliatoAlSeparatore(t *testing.T) {
	for _, n := range []string{"0041/79 123 45 67 anello", "0041-79-123-45-67 anello", "0041.79.123.45.67 anello", "0041/79/123/45/67 anello"} {
		conESenza(t, n, func(t *testing.T, r RigaDaEsportare) { scarta(t, r, ScartoTelefonoDubbio) })
	}
	scarta(t, rigaProva(inRisposta("note", "svizzera 0041-79-123-45-67")), ScartoTelefonoDubbio)
	for _, n := range []string{"busta 0045/26 24/11/2026", "buste 0045/26, 0046/26", "0045/26 0046/26 0047/26"} {
		conESenza(t, n, func(t *testing.T, r RigaDaEsportare) {
			o := esporta(t, r)
			if !strings.HasSuffix(o.Testo, " "+n) {
				t.Errorf("testo %q", o.Testo)
			}
		})
	}
}

// inventario: RIP-H-52
// Un float64 intero (json.Unmarshal in map[string]any) si legge come testo: un telefono così non esce in chiaro. Un
// float64 non intero, o troppo grande per essere esatto, butta la riga (valore_illeggibile).
func TestPseudonimiFloat(t *testing.T) {
	o, m := Pseudonimizza(rigaProva(inProposta("telefono", float64(3330001234))), chiaveProva)
	if m == "" && valoriInChiaro(o, "3330001234") {
		t.Errorf("telefono float in chiaro: %v", o.Proposta)
	}
	o, m = Pseudonimizza(rigaProva(inRisposta("note", float64(3330009876))), chiaveProva)
	if m == "" && valoriInChiaro(o, "3330009876") {
		t.Errorf("numero float in chiaro: %v", o.Risposta)
	}
	o = esporta(t, rigaProva(inRisposta("preventivo", float64(40))))
	if o.Risposta["preventivo"] != float64(40) {
		t.Errorf("preventivo %#v", o.Risposta["preventivo"])
	}
	for _, v := range []any{40.5, float64(1 << 60), math.Inf(1), math.NaN(), float32(0.5)} {
		scarta(t, rigaProva(inProposta("preventivo", v)), ScartoValoreIlleggibile)
	}
}

// inventario: RIP-H-52
// Il confine del prefisso «00»: «0039» seguito da 7 cifre (9 dopo il «00») è un telefono, con 6 (8 dopo il «00») no.
// (Con 0 o 3 davanti e 9-11 cifre in tutto vale comunque la forma dello standalone: il confine si vede solo con 0039.)
func TestPseudonimiConfineDel00(t *testing.T) {
	scarta(t, rigaProva(conTesto("Cliente Mario Rossi 0039 6123456 anello"), conTelefoni("", ""), inRisposta("telefono", ""),
		inProposta("telefono", "")), ScartoTelefonoDubbio)
	esporta(t, rigaProva(conTesto("Cliente Mario Rossi 0039 612345 anello"), conTelefoni("", ""), inRisposta("telefono", ""),
		inProposta("telefono", "")))
	if !formaDiTelefono("0033 6123456") || formaDiTelefono("0033 612345") && !formaDiTelefono("0033612345") {
		t.Error("confine")
	}
}

// inventario: RIP-H-52
// D-111 (1a): un gruppo con la forma di una data non si unisce ad altre cifre; date e importi restano intatti, con e
// senza il telefono del cliente.
func TestPseudonimiD111Date(t *testing.T) {
	for _, n := range []string{"consegna 30/11/2026 3 anelli", "03/12/2026 40 euro", "consegna 3.12.26 40 euro", "ritiro 3-12-2026 12 pezzi",
		"40 euro 03/12/2026", "busta 45 03/12/2026 0046/26"} {
		conESenza(t, n, func(t *testing.T, r RigaDaEsportare) {
			o := esporta(t, r)
			if !strings.HasSuffix(o.Testo, " "+n) {
				t.Errorf("testo %q", o.Testo)
			}
		})
	}
}

// inventario: RIP-H-52
// D-111 (1b) e (3): «300 - 350 - 400 euro» unito dai nuovi separatori ha la forma di un telefono: senza il telefono del
// cliente è un dubbio; con il telefono del cliente lo stesso (forma nuova), mai riscritto. Le cifre si uniscono anche
// attraverso un a capo: «347 123⏎4567» è un dubbio nei due casi.
func TestPseudonimiD111FormeNuoveSempreDubbio(t *testing.T) {
	for _, n := range []string{"300 - 350 - 400 euro", "347 123\n4567 anello", "347 123\r\n4567 anello"} {
		conESenza(t, n, func(t *testing.T, r RigaDaEsportare) { scarta(t, r, ScartoTelefonoDubbio) })
	}
}

// inventario: RIP-H-52
// D-111 (2) aggiornata: U+2800 e U+00B7 si tolgono come i \p{Cf}: il nome non passa in chiaro.
func TestPseudonimiD111BrailleEPuntoMedio(t *testing.T) {
	for _, n := range []string{"anello per Ros\u2800si", "anello per Ros\u00b7si", "anello per Ros\u00b7\u2800si"} {
		t.Run(n, func(t *testing.T) {
			o, m := Pseudonimizza(rigaProva(conTesto("Cliente Mario Rossi 333 000 1234 "+n)), chiaveProva)
			if m != "" {
				return
			}
			for _, s := range tuttiITesti(o) {
				pulito := strings.NewReplacer("\u2800", "", "\u00b7", "").Replace(s)
				if regexp.MustCompile(`(?i)rossi|mario`).MatchString(pulito) {
					t.Errorf("in chiaro: %q", s)
				}
			}
		})
	}
}

// inventario: RIP-H-52
// Una «data» con due separatori diversi non è una data (D-111 (1a)): si unisce alle cifre intorno come ogni altra fila.
func TestPseudonimiD111DataConSeparatoriDiversi(t *testing.T) {
	scarta(t, rigaProva(conTesto("Cliente Mario Rossi ritiro 3/12-2026 12 pezzi"), conTelefoni("", ""), inRisposta("telefono", ""),
		inProposta("telefono", "")), ScartoTelefonoDubbio)
}

// inventario: RIP-H-52
// Un telefono che le file nuove uniscono a un numero che non è un telefono («busta 45, 0400 123456») si vede lo stesso,
// con le file dello standalone: senza il telefono del cliente è un dubbio, con il telefono del cliente si sostituisce.
func TestPseudonimiTelefonoUnitoAUnAltroNumero(t *testing.T) {
	scarta(t, rigaProva(conTesto("Cliente Mario Rossi busta 45, 0400 123456 anello"), conTelefoni("", ""), inRisposta("telefono", ""),
		inProposta("telefono", "")), ScartoTelefonoDubbio)
	o := esporta(t, rigaProva(conTesto("Cliente Mario Rossi 333 000 1234 busta 45, 0400 123456 anello"),
		inRisposta("note", "busta 45, 0400 123456")))
	nessunNumero(t, o, "0400123456")
	if !strings.Contains(o.Testo, "busta 45, 0") {
		t.Errorf("testo %q", o.Testo)
	}
}

// ---- ultimo giro della D-111 (coordinatore, 24/11/2026) ----

// inventario: RIP-H-52
// D-111 (i)-(iii): un gruppo è una data, e resta separato dalle altre cifre, solo se è plausibile (giorno 1-31, mese
// 1-12, stesso separatore) e ha l'anno a 4 cifre; le date si cercano a ogni inizio possibile, non nell'ordine della regex.
// Questi sono telefoni: telefono_dubbio con e senza il telefono del cliente nel testo. Nelle note non escono mai in
// chiaro (si scartano, o si sostituiscono se la forma è dello standalone, D-111 (1b)).
func TestPseudonimiD111DatePlausibili(t *testing.T) {
	for _, n := range []string{"0423 52/12/34", "0423 52.12.34", "347 12.12.26", "0423.52.12.34"} {
		conESenza(t, n+" anello", func(t *testing.T, r RigaDaEsportare) { scarta(t, r, ScartoTelefonoDubbio) })
		t.Run("note:"+n, func(t *testing.T) {
			o, m := Pseudonimizza(rigaProva(inRisposta("note", "richiamare "+n)), chiaveProva)
			if m == "" && strings.Contains(soloCifre(fmt.Sprint(o.Risposta["note"])), soloCifre(n)) {
				t.Errorf("in chiaro: %q", o.Risposta["note"])
			}
			o, m = Pseudonimizza(rigaProva(conTesto("Cliente Mario Rossi anello"), conTelefoni("", ""), inRisposta("telefono", ""),
				inProposta("telefono", ""), inRisposta("note", "richiamare "+n)), chiaveProva)
			if m == "" && strings.Contains(soloCifre(fmt.Sprint(o.Risposta["note"])), soloCifre(n)) {
				t.Errorf("in chiaro senza telefono: %q", o.Risposta["note"])
			}
		})
	}
	for _, n := range []string{"consegna 30/11/2026 3 anelli", "03/12/2026 40 euro", "consegna 31.12.2026 2 anelli"} {
		conESenza(t, n, func(t *testing.T, r RigaDaEsportare) {
			if o := esporta(t, r); !strings.HasSuffix(o.Testo, " "+n) {
				t.Errorf("testo %q", o.Testo)
			}
		})
	}
	// Una «data» impossibile non separa: «32/11/2026 3» o «31/13/2026 3» si uniscono (9 cifre che cominciano con 3).
	for _, n := range []string{"32/11/2026 3 anelli", "31/13/2026 3 anelli", "3/12/2026 33 anelli"} {
		esportaOScarta := n != "3/12/2026 33 anelli"
		t.Run(n, func(t *testing.T) {
			_, m := Pseudonimizza(rigaProva(conTesto("Cliente Mario Rossi "+n), conTelefoni("", ""), inRisposta("telefono", ""),
				inProposta("telefono", "")), chiaveProva)
			if esportaOScarta && m != ScartoTelefonoDubbio || !esportaOScarta && m != "" {
				t.Errorf("motivo %q", m)
			}
		})
	}
	if v := date("x 1/1/2026 y 01.01.2026 z 31-12-2026 w 1/1/26 0/1/2026 1/0/2026 1/1-2026 11/11/2026/1"); len(v) != 3 {
		t.Errorf("date %v", v)
	}
}

// inventario: RIP-H-52
// D-111 (iv): un segno non ASCII (\p{P} o \p{S}) fra due lettere non nasconde un nome: «Ros‧si», «Ros・si», «Ros⠀si»,
// «Ros·si» non escono mai in chiaro. La pulizia vale solo per la copia in cui si cercano nomi e numeri: il testo
// esportato tiene i suoi segni e i suoi apostrofi («l’anello», «dell’»).
func TestPseudonimiD111SegniFraLettere(t *testing.T) {
	for _, n := range []string{"Ros\u2027si", "Ros\u30fbsi", "Ros\u2800si", "Ros\u00b7si", "Ros\u00b7\u2027si"} {
		t.Run(n, func(t *testing.T) {
			for _, r := range []RigaDaEsportare{rigaProva(conTesto("Cliente Mario Rossi 333 000 1234 anello per " + n)),
				rigaProva(inRisposta("note", "ritira "+n))} {
				o, m := Pseudonimizza(r, chiaveProva)
				if m != "" {
					continue
				}
				for _, s := range tuttiITesti(o) {
					if strings.Contains(s, n) || strings.Contains(s, "Rossi") {
						t.Errorf("in chiaro: %q", s)
					}
				}
			}
		})
	}
	o := esporta(t, rigaProva(conTesto("Cliente Mario Rossi 333 000 1234 l\u2019anello dell\u2019Rossi \u00b7 collana \u2022 orologio")))
	cognome := strings.Fields(o.Risposta["nome"].(string))[1]
	if !strings.HasSuffix(o.Testo, " l\u2019anello dell\u2019"+cognome+" \u00b7 collana \u2022 orologio") {
		t.Errorf("testo %q", o.Testo)
	}
	o = esporta(t, rigaProva(inRisposta("note", "l\u2019anello \u00b7 a\u00b7b")))
	if o.Risposta["note"] != "l\u2019anello \u00b7 a\u00b7b" {
		t.Errorf("note %q", o.Risposta["note"])
	}
}

// inventario: RIP-H-52
// D-111 (iv): il nome di un altro nascosto da un segno fra le lettere butta la riga (altro_nome), e l'apostrofo resta
// apostrofo nella copia di ricerca: «dell’Ros‧si» si legge «dell'Rossi», non «dellRossi».
func TestPseudonimiD111SegniAltroNomeEApostrofo(t *testing.T) {
	altri := ParoleDeiNomi([]string{"Teresa Zanchetta"})
	scarta(t, rigaProva(conTesto("Cliente Mario Rossi 333 000 1234 anello per Zan\u2027chetta"), func(r *RigaDaEsportare) { r.Altri = altri }),
		ScartoAltroNome)
	scarta(t, rigaProva(conTesto("Cliente Mario Rossi 333 000 1234 anello dell\u2019Ros\u2027si")), ScartoNomeResiduo)
	scarta(t, rigaProva(conTesto("Cliente Mario Rossi 333 000 1234 anello dell\u2019Zan\u2027chetta"), func(r *RigaDaEsportare) { r.Altri = altri }),
		ScartoAltroNome)
}
