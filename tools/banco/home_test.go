package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/buste"
)

func rigaHome(json string) string { return "GR_HOME:" + json + "\n" }

// esitoHomeBuono è la riga che lo standalone a go-baseline deve dare per il passo della home.
const esitoHomeBuono = `{"esito":"ok","buste":[21,22,23,24,25,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40],` +
	`"numeri":["21/26","22/26","23/26","24/26","25/26","26/26","27/26","28/26","29/26","30/26","31/26","32/26","33/26","34/26",` +
	`"35/26","36/26","37/26","38/26","9999/25","10000/25"],` +
	`"stati":["consegnata","in_lavorazione","in_lavorazione","in_attesa_materiali","in_lavorazione","in_lavorazione",` +
	`"in_lavorazione","in_lavorazione","in_lavorazione","in_lavorazione","in_lavorazione","pronta","in_lavorazione",` +
	`"in_valutazione","in_lavorazione","in_lavorazione","consegnata","annullata","in_lavorazione","in_lavorazione"],` +
	`"totale_buste":40}`

func TestLeggiEsitoHome(t *testing.T) {
	ok := "avvio\n" + rigaHome(esitoHomeBuono)
	e, err := leggiEsitoHome(ok)
	if err != nil {
		t.Fatalf("esito buono rifiutato: %v", err)
	}
	if len(e.Buste) != 20 || e.TotaleBuste != 40 {
		t.Errorf("esito = %+v", e)
	}
	sostituisci := func(vecchio, nuovo string) string {
		if !strings.Contains(esitoHomeBuono, vecchio) {
			t.Fatalf("%q non c'è", vecchio)
		}
		return rigaHome(strings.Replace(esitoHomeBuono, vecchio, nuovo, 1))
	}
	for nome, out := range map[string]string{
		"senza riga":        "fatto\n",
		"esito errore":      rigaHome(`{"esito":"errore","motivo":"{:error, :not_found}"}`),
		"due righe":         ok + ok,
		"json rotto":        "GR_HOME:{esito\n",
		"dati in coda":      strings.TrimSuffix(rigaHome(esitoHomeBuono), "\n") + "{}\n",
		"campo sconosciuto": sostituisci(`"esito":"ok"`, `"esito":"ok","altro":1`),
		"altri id":          sostituisci(`[21,22,`, `[21,23,`),
		"altro numero":      sostituisci(`"10000/25"`, `"1000/25"`),
		"altro stato":       sostituisci(`"pronta"`, `"in_lavorazione"`),
		"buste in più":      sostituisci(`"totale_buste":40`, `"totale_buste":41`),
	} {
		if _, err := leggiEsitoHome(out); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
}

// Il passo della home gira come gli altri: immagine dello standalone, senza rete, con le protezioni e
// i dati dell'istanza; usa solo le funzioni delle pagine, mai scritture dirette.
func TestComandoHome(t *testing.T) {
	n, _ := istanzaDiProva(t)
	c := comandoHome(n, "img:1", []string{"--env", "DATABASE_PATH=/data/gestrip.db"})
	if valoreDopo(c, "--name") != n.base+"-"+passoHome || valoreDopo(c, "--network") != "none" || indice(c, "--rm") < 0 {
		t.Errorf("nome, rete o --rm: %v", c)
	}
	i := indice(c, "img:1")
	if i < 0 || len(c) != i+4 || c[i+3] != homeStandalone {
		t.Fatalf("comando = %v", c)
	}
	found := false
	for _, x := range suffissiContainer() {
		found = found || x == "-"+passoHome
	}
	if !found {
		t.Error("banco giu non toglie il container della home")
	}
	for _, voluto := range []string{"Workshop.create_repair(cliente,", "TelegramNotices.transition(", "Workshop.toggle_repair_pin(b26, audit_scheda)",
		`RepairMessages.append(riletta.(busta), campi, audit)`, `author: "customer"`, `author: "shop"`, `"show_delivered" => "false"`,
		"Workshop.set_repair_waiting(", `"GR_HOME:"`, "System.halt(1)", "totale_buste: Repo.aggregate(Repair, :count)"} {
		if !strings.Contains(homeStandalone, voluto) {
			t.Errorf("lo script non contiene %q", voluto)
		}
	}
	for _, vietato := range []string{"Repo.update", "Repo.insert", "Repo.query", "Ecto.Changeset.change", "SQL", "IO.inspect"} {
		if strings.Contains(homeStandalone, vietato) {
			t.Errorf("lo script contiene %q", vietato)
		}
	}
}

// La tabella del normalizzatore D-64 è scritta a mano: qui si ricontrolla con la regola del Go
// (buste.NumeroVisibile) sui codici QR che la cattura e i passi danno alle buste, così una tabella
// sbagliata non nasconde un Go sbagliato e viceversa.
func TestNumeriVisibiliBanco(t *testing.T) {
	qr := map[string]string{"21/26": "RIP-2026-0045", "22/26": "RIP-2026-0045", "23/26": "RIP-2026-0046", "24/26": "RIP-2026-0046",
		"25/26": "RIP-2025-0045", "26/26": "RIP-2026-0047", "27/26": "RIP-2026-0048", "28/26": "RIP-2026-0049", "29/26": "RIP-2026-0050",
		"30/26": "RIP-2026-0051", "31/26": "RIP-2026-0052", "32/26": "RIP-2026-0053", "33/26": "RIP-2026-0054", "34/26": "RIP-2026-0055",
		"35/26": "", "36/26": "SINT-ALTRO-36", "37/26": "RIP-2026-0056", "38/26": "RIP-2026-0057", "9999/25": "RIP-2025-0012", "10000/25": ""}
	if len(numeriVisibiliBanco) != 40 {
		t.Errorf("%d buste nella tabella", len(numeriVisibiliBanco))
	}
	for numero, visibile := range numeriVisibiliBanco {
		codice := qr[numero] // le 1–20: nessuna etichetta RIP (SINT-QR-… o nessun QR)
		testo, etichetta := buste.NumeroVisibile(codice, numero)
		if etichetta {
			testo = "#" + testo
		}
		if testo != visibile {
			t.Errorf("%s (%q): tabella %q, Go %q", numero, codice, visibile, testo)
		}
	}
	// La forma vera dell'aria-label dello standalone, «Apri busta 21/26» (interpolata, senza «#»),
	// per ogni busta della tabella: diventa quella del Go, «Apri busta 0045/26» o «Apri busta N. 1/26».
	for numero, visibile := range numeriVisibiliBanco {
		if g, v := normalizzaD64("Apri busta "+numero), "Apri busta "+strings.TrimPrefix(visibile, "#"); g != v {
			t.Errorf("aria-label %s: %q, voluto %q", numero, g, v)
		}
	}
	for in, out := range map[string]string{
		"Apri busta #21/26":         "Apri busta #0045/26",
		"#1/26 Cliente":             "N. 1/26 Cliente",
		"#99/26 e #10000/25":        "#99/26 e N. 10000/25",
		"Busta #21/26x":             "Busta #21/26x",
		"#21/26#22/26":              "#0045/26#0045/26",
		"senza numeri":              "senza numeri",
		"#9999/25, #25/26 · #35/26": "#0012/25, #0045/25 · N. 35/26",
	} {
		if g := normalizzaD64(in); g != out {
			t.Errorf("%q: %q, voluto %q", in, g, out)
		}
	}
}

// Un passo con "normalizza" confronta il valore dello standalone normalizzato: uguale al Go è una
// differenza attesa della D-xx, diverso resta inatteso; le attese guardano lo standalone normalizzato.
// Il normalizzatore deve esistere e il passo deve girare sulle due app.
func TestNormalizzaNelConfronto(t *testing.T) {
	p := passo{Metodo: "GET", Percorso: "/", Confronta: []string{"corpo"}, Normalizza: "D-64"}
	a, b := risposta{Corpo: "Apri busta #21/26"}, risposta{Corpo: "Apri busta #0045/26"}
	d := confrontaCon(p, "", a, b)
	if len(d) != 1 || d[0].Normalizzata != "D-64" {
		t.Fatalf("normalizzata: %+v", d)
	}
	if d := confrontaCon(p, "", a, risposta{Corpo: "Apri busta #0046/26"}); len(d) != 1 || d[0].Normalizzata != "" {
		t.Errorf("diversa anche normalizzata: %+v", d)
	}
	if d := confrontaCon(p, "", b, b); len(d) != 0 {
		t.Errorf("uguali: %+v", d)
	}
	// Il Go uguale allo standalone grezzo dove la D-64 cambia il valore: non l'ha applicata, inattesa.
	if d := confrontaCon(p, "", a, a); len(d) != 1 || d[0].Normalizzata != "" || d[0].Campo != "corpo (D-64 non applicata dal Go)" ||
		d[0].Standalone != "Apri busta #0045/26" || d[0].Go != "Apri busta #21/26" {
		t.Errorf("D-64 non applicata: %+v", d)
	}
	// Uguali e senza numeri da normalizzare: nessuna differenza.
	if d := confrontaCon(p, "", risposta{Corpo: "niente"}, risposta{Corpo: "niente"}); len(d) != 0 {
		t.Errorf("senza numeri: %+v", d)
	}
	p.Normalizza = ""
	if d := confrontaCon(p, "", a, b); len(d) != 1 || d[0].Normalizzata != "" {
		t.Errorf("senza normalizzatore: %+v", d)
	}
	p.Normalizza, p.Atteso = "D-64", map[string]string{"testo:#0045/26": "presente"}
	if d := confrontaCon(p, "", a, b); len(d) != 1 || d[0].Normalizzata != "D-64" {
		t.Errorf("attesa sullo standalone normalizzato: %+v", d)
	}
	for nome, js := range map[string]string{
		"sconosciuto": `{"nome":"x","passi":[{"metodo":"GET","percorso":"/","confronta":["codice"],"normalizza":"D-99"}]}`,
		"solo go":     `{"nome":"x","passi":[{"metodo":"GET","percorso":"/","atteso":{"codice":"200"},"solo_go":"D-44","normalizza":"D-64"}]}`,
	} {
		if _, err := leggiScenario(nome, strings.NewReader(js)); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
	s, err := leggiScenario("buono", strings.NewReader(`{"nome":"x","passi":[{"metodo":"GET","percorso":"/","confronta":["codice"],"normalizza":"D-64"}]}`))
	if err != nil || !slices.Contains(deviazioniSoloGo([]scenario{s}), "D-64") {
		t.Errorf("la D-64 del normalizzatore si controlla nell'inventario: %v %v", err, deviazioniSoloGo([]scenario{s}))
	}
}

// Gli scenari della home girano nell'ordine dei nomi dei file: home-cerca (slice 052), home-contatori,
// home-dipendente e home-filtri prima di home, perché l'ultimo passo di home apre la 27/26 e segna letto il suo messaggio («💬 Messaggi:
// 1» negli altri due, 0 dopo; la campanella dello standalone a zero per il livello browser).
func TestOrdineScenariHome(t *testing.T) {
	ss, err := leggiScenari("../../bench/scenari")
	if err != nil {
		t.Fatal(err)
	}
	var nomi []string
	for _, s := range ss {
		if s.Nome == "home" || strings.HasPrefix(s.Nome, "home-") {
			nomi = append(nomi, s.Nome)
		}
	}
	if strings.Join(nomi, " ") != "home-cerca home-contatori home-dipendente home-filtri home" {
		t.Errorf("ordine %v", nomi)
	}
	h := ss[slices.IndexFunc(ss, func(s scenario) bool { return s.Nome == "home" })]
	if p := h.Passi[len(h.Passi)-2]; p.Percorso != "/repairs/27" {
		t.Errorf("il penultimo passo di home apre %s, non la 27/26", p.Percorso)
	}
}
