package main

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// I normalizzatori della slice 055a: D-71 («Data ricezione») e D-72 («Registrata il» solo se di un
// altro giorno), sui pezzi di pagina dello standalone (repair_live.ex, workshop_live.ex a go-baseline)
// e sul testo dei PDF.

func TestNormalizzaD71(t *testing.T) {
	for in, out := range map[string]string{
		`<dt class="text-sm opacity-70">Data</dt><dd>05/01/2026</dd>`: `<dt class="text-sm opacity-70">Data ricezione</dt><dd>05/01/2026</dd>`,
		`<dt>Data</dt>`: `<dt>Data ricezione</dt>`,
		`<label for="repair_accepted_on">` + "\n" + `<span class="label">Data</span>`:            `<label for="repair_accepted_on">` + "\n" + `<span class="label">Data ricezione</span>`,
		`<label class="x" for="repair_accepted_on"><span class="label">Data accettazione</span>`: `<label class="x" for="repair_accepted_on"><span class="label">Data ricezione</span>`,
		"N° BUSTA 1/26 DATA ACCETTAZIONE 05/01/2026 DATA CONSEGNA PREVISTA":                      "N° BUSTA 1/26 DATA RICEZIONE 05/01/2026 DATA CONSEGNA PREVISTA",
		"IN LAVORAZIONE DATA RIPARAZIONE 05/01/2026 CONSEGNA ENTRO":                              "IN LAVORAZIONE DATA RICEZIONE 05/01/2026 CONSEGNA ENTRO",
		// La home dalla slice 056: la scelta del periodo e la pillola.
		`<option value="accettazione" selected>` + "\n          Accettazione\n        </option>": `<option value="accettazione" selected>` + "\n          Ricezione\n        </option>",
		`<option value="accettazione">Accettazione</option>`:                                     `<option value="accettazione">Ricezione</option>`,
		`<span class="filter-pill">📅 Accettazione: 2026-01-01 → …</span>`:                        `<span class="filter-pill">📅 Ricezione: 2026-01-01 → …</span>`,
		// Restano: i campi delle date della home, le altre scelte, altre righe, un'altra etichetta «Data».
		`<option value="pronta">Pronta</option>`:                          `<option value="pronta">Pronta</option>`,
		"📅 Pronta: 2026-01-01 → …":                                        "📅 Pronta: 2026-01-01 → …",
		"Accettazione":                                                    "Accettazione",
		`<span class="gs-etichetta">Data da</span>`:                       `<span class="gs-etichetta">Data da</span>`,
		`<dt>Data consegna</dt>`:                                          `<dt>Data consegna</dt>`,
		`<label for="repair_expected_on"><span class="label">Data</span>`: `<label for="repair_expected_on"><span class="label">Data</span>`,
		"DATA CONSEGNA PREVISTA 20/09/2026":                               "DATA CONSEGNA PREVISTA 20/09/2026",
	} {
		if g := normalizzaD71(in); g != out {
			t.Errorf("%q: %q, voluto %q", in, g, out)
		}
	}
}

func TestNormalizzaD72(t *testing.T) {
	riga := func(quando string) string {
		return "<div>\n  <dt class=\"text-sm opacity-70\">Registrata il</dt><dd>\n    " + quando + "\n  </dd>\n</div>\n"
	}
	data := `<div><dt class="text-sm opacity-70">Data</dt><dd>05/01/2026</dd></div>`
	codice := `<div><dt>Codice QR busta</dt><dd id="repair-qr-code">—</dd></div>`
	for _, c := range []struct{ nome, in, out string }{
		{"stesso giorno", data + riga("05/01/2026 10:00") + codice, data + codice},
		{"stesso giorno dopo la D-71", normalizzaD71(data) + riga("05/01/2026 23:59") + codice, normalizzaD71(data) + codice},
		{"altro giorno", data + riga("06/01/2026 00:00") + codice, data + riga("06/01/2026 00:00") + codice},
		{"senza data", riga("05/01/2026 10:00"), riga("05/01/2026 10:00")},
	} {
		if g := normalizzaD72(c.in); g != c.out {
			t.Errorf("%s: %q, voluto %q", c.nome, g, c.out)
		}
	}
}

// I normalizzatori si compongono con «+», nell'ordine; uno sconosciuto rende nullo tutto. Le D-xx
// di un passo composto si controllano ognuna nell'inventario.
func TestNormalizzatoriComposti(t *testing.T) {
	n := normalizzatore("D-64+D-71+D-72")
	if n == nil {
		t.Fatal("composizione mancante")
	}
	in := `<h1>Busta #21/26</h1><dt>Data</dt><dd>05/01/2026</dd><div><dt>Registrata il</dt><dd>05/01/2026 10:00</dd></div>`
	if g, voluto := n(in), `<h1>Busta #0045/26</h1><dt>Data ricezione</dt><dd>05/01/2026</dd>`; g != voluto {
		t.Errorf("composta: %q, voluto %q", g, voluto)
	}
	for _, nome := range []string{"", "D-99", "D-64+D-99", "D-64+"} {
		if normalizzatore(nome) != nil {
			t.Errorf("%q: normalizzatore non nullo", nome)
		}
	}
	if _, err := leggiScenario("x.json", strings.NewReader(`{"nome":"x","passi":[{"metodo":"GET","percorso":"/","confronta":["codice"],"normalizza":"D-64+D-99"}]}`)); err == nil {
		t.Error("D-99 accettata")
	}
	s := scenario{Passi: []passo{{Normalizza: "D-64+D-71"}, {Normalizza: "D-71"}, {SoloGo: "D-72"}}}
	if d := deviazioniSoloGo([]scenario{s}); !slices.Equal(d, []string{"D-64", "D-71", "D-72"}) {
		t.Errorf("deviazioni %v", d)
	}
}

// Per un campo del PDF il normalizzatore passa sul testo letto, non sul corpo (che è il PDF): il Go
// uguale allo standalone grezzo vuol dire D-71 non applicata, e lo standalone normalizzato ha «DATA
// RICEZIONE»; senza il normalizzatore i due PDF uguali non danno differenze.
func TestNormalizzaSulTestoDelPDF(t *testing.T) {
	pdf, err := os.ReadFile("../../internal/pdf/lettura/testdata/chromium-scheda-2.pdf")
	if err != nil {
		t.Fatal(err)
	}
	a := risposta{Corpo: string(pdf)}
	p := passo{Metodo: "GET", Percorso: "/repairs/2/scheda.pdf", Confronta: []string{campoPDFTesto}, Normalizza: "D-71"}
	d := confrontaCon(p, "", a, a)
	if len(d) != 1 || d[0].Campo != "pdf:testo (D-71 non applicata dal Go)" || !strings.Contains(d[0].Standalone, "DATA RICEZIONE 05/01/2026") ||
		!strings.Contains(d[0].Go, "DATA ACCETTAZIONE") {
		t.Errorf("D-71 sul PDF: %+v", d)
	}
	p.Atteso = map[string]string{campoPDFTesto: d[0].Standalone}
	p.Confronta = nil
	if d := confrontaCon(p, "", a, risposta{Corpo: "niente"}); len(d) != 1 || !strings.HasPrefix(d[0].Campo, "atteso pdf:testo=") || d[0].Standalone != p.Atteso[campoPDFTesto] {
		t.Errorf("attesa sul PDF normalizzato: %+v", d)
	}
	p.Normalizza, p.Atteso, p.Confronta = "", nil, []string{campoPDFTesto}
	if d := confrontaCon(p, "", a, a); len(d) != 0 {
		t.Errorf("senza normalizzatore: %+v", d)
	}
}

// Con un normalizzatore, una differenza che resta anche normalizzata porta lo standalone
// normalizzato: così una riga «togli» (D-01) copre il report anche sopra la D-71.
func TestNormalizzataEPoiTogli(t *testing.T) {
	p := passo{Metodo: "GET", Percorso: "/", Confronta: []string{"corpo"}, Normalizza: "D-71"}
	a := risposta{Corpo: "#1/26 DATA RIPARAZIONE 05/01/2026 📷 FOTO Nessuna foto allegata. fine"}
	b := risposta{Corpo: "#1/26 DATA RICEZIONE 05/01/2026 fine"}
	d := confrontaCon(p, "", a, b)
	if len(d) != 1 || d[0].Standalone != "#1/26 DATA RICEZIONE 05/01/2026 📷 FOTO Nessuna foto allegata. fine" || d[0].Normalizzata != "" {
		t.Fatalf("differenza: %+v", d)
	}
	ds := deviazioni{{ID: "D-01", Scenario: "s", Passo: 1, Campo: "corpo", Togli: "📷 FOTO Nessuna foto allegata.", Copie: 1, Motivo: "prova"}}
	x := d[0]
	x.Scenario, x.Passo = "s", 1
	if id, ok := ds.copre(x); !ok || id != "D-01" {
		t.Errorf("togli dopo la D-71: %q %v", id, ok)
	}
}

// D-73 (slice 058): «Consegna: gg/mm/aaaa» dell'etichetta 76×127 dello standalone diventa
// «All'artigiano: gg/mm/aaaa»; le altre «Consegna» (scheda PDF, report, pagina della firma) restano.
func TestNormalizzaD73(t *testing.T) {
	for in, out := range map[string]string{
		"#1/26 Consegna: 08/11/2026 Scad.: fine mese":       "#1/26 All'artigiano: 08/11/2026 Scad.: fine mese",
		"N. 1/26 Consegna: 08/11/2026 IN LAVORAZIONE":       "N. 1/26 All'artigiano: 08/11/2026 IN LAVORAZIONE",
		"Consegna — Firma del cliente":                      "Consegna — Firma del cliente",
		"CONSEGNA ENTRO 20/09/2026":                         "CONSEGNA ENTRO 20/09/2026",
		"✍️ Consegna busta #15/26":                          "✍️ Consegna busta #15/26",
		"Consegna: fine mese":                               "Consegna: fine mese",
		"qr 53.87 5.94 16.09 15.90; numero 16.67; consegna": "qr 53.87 5.94 16.09 15.90; numero 16.67; consegna",
	} {
		if g := normalizzaD73(in); g != out {
			t.Errorf("%q: %q, voluto %q", in, g, out)
		}
	}
	if normalizzatore("D-64+D-73")("#22/26 Consegna: 08/11/2026") != "#0045/26 All'artigiano: 08/11/2026" {
		t.Error("D-64+D-73")
	}
}
