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

// D-80 (slice 060): le date AAAA-MM-GG di «ACCETTATA» e «PREVISTA» della ricevuta e di «Scad.:»
// dell'etichetta dello standalone diventano gg/mm/aaaa; i testi e le altre date restano.
func TestNormalizzaD80(t *testing.T) {
	for in, out := range map[string]string{
		"ACCETTATA 2026-03-01 PREVISTA 2026-04-15 LAVORAZIONE": "ACCETTATA 01/03/2026 PREVISTA 15/04/2026 LAVORAZIONE",
		"ACCETTATA 2026-01-05 PREVISTA Da concordare":          "ACCETTATA 05/01/2026 PREVISTA Da concordare",
		"ACCETTATA\n2026-01-05":                                "ACCETTATA\n05/01/2026",
		"All'artigiano: 08/11/2026 Scad.: 2026-11-20 IN":       "All'artigiano: 08/11/2026 Scad.: 20/11/2026 IN",
		"Scad.: fine mese":                                     "Scad.: fine mese",
		"DATA CONSEGNA PREVISTA CLIENTE":                       "DATA CONSEGNA PREVISTA CLIENTE",
		"DATA RICEZIONE 2026-01-05":                            "DATA RICEZIONE 2026-01-05",
		"PREVISTA 2026-04-150":                                 "PREVISTA 2026-04-150",
	} {
		if g := normalizzaD80(in); g != out {
			t.Errorf("%q: %q, voluto %q", in, g, out)
		}
	}
	if normalizzatore("D-64+D-73+D-80")("#22/26 Consegna: 08/11/2026 Scad.: 2026-11-20") != "#0045/26 All'artigiano: 08/11/2026 Scad.: 20/11/2026" {
		t.Error("D-64+D-73+D-80")
	}
}

// D-84 (slice 074c): sulla pagina del cliente le date AAAA-MM-GG di «Accettata», «Consegna prevista»,
// «Pronta dal» e «Consegnata» dello standalone diventano gg/mm/aaaa; un testo scritto al posto della
// data, le altre righe e le date fuori da un <dd> della pagina restano.
func TestNormalizzaD84(t *testing.T) {
	dd := func(dt, v string) string {
		return `<div><dt class="text-stone-500">` + dt + `</dt><dd class="mt-1 font-semibold">` + "\n            " + v + "\n          </dd></div>"
	}
	for in, out := range map[string]string{
		dd("Accettata", "2026-10-01"):               dd("Accettata", "01/10/2026"),
		dd("Consegna prevista", "2026-11-05"):       dd("Consegna prevista", "05/11/2026"),
		dd("Pronta dal", "2026-11-03"):              dd("Pronta dal", "03/11/2026"),
		dd("Consegnata", "2026-11-04"):              dd("Consegnata", "04/11/2026"),
		"<dt>Accettata</dt><dd>2026-01-09</dd>":     "<dt>Accettata</dt><dd>09/01/2026</dd>",
		dd("Consegna prevista", "fine mese"):        dd("Consegna prevista", "fine mese"),
		dd("Consegna prevista", "entro 2026-11-05"): dd("Consegna prevista", "entro 2026-11-05"),
		dd("Consegnata il", "2026-11-04"):           dd("Consegnata il", "2026-11-04"),
		dd("Tempo indicativo", "2026-11-05"):        dd("Tempo indicativo", "2026-11-05"),
		dd("Accettata", "2026-10-011"):              dd("Accettata", "2026-10-011"),
		`<p>Accettata 2026-10-01</p>`:               `<p>Accettata 2026-10-01</p>`,
		`data-last-ts="2026-10-01T09:00:00Z"`:       `data-last-ts="2026-10-01T09:00:00Z"`,
	} {
		if g := normalizzaD84(in); g != out {
			t.Errorf("%q: %q, voluto %q", in, g, out)
		}
	}
	if normalizzatore("D-84") == nil {
		t.Error("D-84 senza normalizzatore")
	}
}
