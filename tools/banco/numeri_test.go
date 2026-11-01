package main

import (
	"os"
	"strings"
	"testing"
)

// Il passo dei numeri gira come gli stati: immagine dello standalone, senza rete, con le protezioni
// e i dati dell'istanza, e nient'altro.
func TestComandoNumeri(t *testing.T) {
	n, _ := istanzaDiProva(t)
	c := comandoNumeri(n, "img:1", []string{"--env", "DATABASE_PATH=/data/gestrip.db"})
	s := strings.Join(c, " ")
	if valoreDopo(c, "--name") != n.base+"-"+passoNumeri || valoreDopo(c, "--network") != "none" || indice(c, "--rm") < 0 {
		t.Errorf("nome, rete o --rm: %s", s)
	}
	if !strings.Contains(s, strings.Join(protezioniStandalone(n), " ")) || !strings.Contains(s, "--env DATABASE_PATH=/data/gestrip.db") {
		t.Errorf("protezioni o ambiente: %s", s)
	}
	for _, vietato := range []string{"GESTRIP_IMPORT", "/import/", "/catena/", "PHX_SERVER", "-d "} {
		if strings.Contains(s, vietato) {
			t.Errorf("contiene %q: %s", vietato, s)
		}
	}
	montaggi := 0
	for _, a := range c {
		if strings.HasPrefix(a, "type=bind,") {
			montaggi++
		}
	}
	if montaggi != 3 {
		t.Errorf("%d montaggi, voluti i soli 3 dell'istanza dello standalone: %s", montaggi, s)
	}
	i := indice(c, "img:1")
	if i < 0 || len(c) != i+4 || c[i+1] != "bin/gestrip_standalone" || c[i+2] != "eval" || c[i+3] != numeriStandalone {
		t.Fatalf("comando = %v", c)
	}
	found := false
	for _, x := range suffissiContainer() {
		found = found || x == "-"+passoNumeri
	}
	if !found {
		t.Error("banco giu non toglie il container dei numeri")
	}
}

// Lo script usa le funzioni della nuova busta e della modifica, con il contesto della scheda per
// la modifica, e legge gli errori come la pagina (translate_error/1, il catalogo); nessuna
// scrittura diretta.
func TestScriptNumeriComeLePagine(t *testing.T) {
	for _, voluto := range []string{
		"GestripStandalone.LegacyImport.Release.quiet!()",
		"Application.ensure_all_started(:gestrip_standalone)",
		"\nemail = \"/run/secrets/admin_email\" |> File.read!() |> String.trim_trailing()\n",
		"audit_scheda = %{actor: email, session_user: {titolare.id, titolare.session_version}}",
		"Ecto.Changeset.traverse_errors(cs, &GestripStandaloneWeb.CoreComponents.translate_error/1)",
		"Ecto.Changeset.get_change(cs, :bag_number)",
		`"bag_number" => "019/26"`,
		`"bag_number" => " 0019/26 "`,
		`"bag_number" => "0/26"`,
		`"bag_number" => "00000/26"`,
		`"bag_number" => "000020/26"`,
		"Workshop.peek_next_bag_number(2026)",
		"totale_buste: Repo.aggregate(Repair, :count)",
		"\"GR_NUMERI:\"",
		"System.halt(1)",
	} {
		if !strings.Contains(numeriStandalone, voluto) {
			t.Errorf("lo script non contiene %q", voluto)
		}
	}
	conta := map[string]int{
		"Workshop.create_repair(cliente,":  7,
		"Workshop.update_repair(riletta.(": 3,
		"audit_scheda, compro_oro: nil)":   3,
		"rifiuto.(":                        8,
		"Workshop.create_customer(%{name:": 1,
	}
	for f, n := range conta {
		if got := strings.Count(numeriStandalone, f); got != n {
			t.Errorf("%s %d volte, attese %d", f, got, n)
		}
	}
	for _, vietato := range []string{"Repo.update", "Repo.insert", "Repo.query", "Ecto.Changeset.change", "SQL", "IO.inspect", "Gettext.put_locale"} {
		if strings.Contains(numeriStandalone, vietato) {
			t.Errorf("lo script contiene %q", vietato)
		}
	}
}

func rigaNumeri(json string) string { return "GR_NUMERI:" + json + "\n" }

// esitoNumeriBuono è la riga che lo standalone a go-baseline (slice 220) deve dare: i testi del
// catalogo it e i numeri senza zeri davanti.
const esitoNumeriBuono = `{"esito":"ok","buste":[19,20],"numeri":["19/26","20/26"],"prossimo":"21/26",` +
	`"rifiuti":{"doppio":{"errori":{"bag_number":["Numero busta già esistente."]},"numero":"19/26"},` +
	`"zero":{"errori":{"bag_number":["formato richiesto N/AA (es. 12/26)"]},"numero":"0/26"},` +
	`"zeri":{"errori":{"bag_number":["formato richiesto N/AA (es. 12/26)"]},"numero":"0/26"},` +
	`"vuoti":{"errori":{"accepted_on":["Campo obbligatorio."],"description":["Campo obbligatorio."]},"numero":"21/26"},` +
	`"data":{"errori":{"accepted_on":["Valore non valido."]},"numero":"21/26"},` +
	`"modifica_vuota":{"errori":{"accepted_on":["Campo obbligatorio."],"description":["Campo obbligatorio."],` +
	`"customer_name_snapshot":["Campo obbligatorio."]},"numero":null},` +
	`"modifica_lunghi":{"errori":{"customer_name_snapshot":["Troppo lungo (massimo 160 caratteri)."],` +
	`"customer_phone_snapshot":["Troppo lungo (massimo 40 caratteri)."]},"numero":null},` +
	`"modifica_non_validi":{"errori":{"accepted_on":["Valore non valido."],"extra_days":["Valore non valido."]},"numero":null}},` +
	`"totale_buste":20}`

func TestLeggiEsitoNumeri(t *testing.T) {
	ok := "avvio\n" + rigaNumeri(esitoNumeriBuono)
	e, err := leggiEsitoNumeri(ok)
	if err != nil {
		t.Fatalf("esito buono rifiutato: %v", err)
	}
	if len(e.Buste) != 2 || e.TotaleBuste != 20 || len(e.Rifiuti) != 8 || e.Rifiuti["zero"].Numero == nil || e.Rifiuti["modifica_vuota"].Numero != nil {
		t.Errorf("esito = %+v", e)
	}
	sostituisci := func(vecchio, nuovo string) string {
		if !strings.Contains(esitoNumeriBuono, vecchio) {
			t.Fatalf("%q non c'è", vecchio)
		}
		return rigaNumeri(strings.Replace(esitoNumeriBuono, vecchio, nuovo, 1))
	}
	for nome, out := range map[string]string{
		"senza riga":          "fatto\n",
		"esito errore":        rigaNumeri(`{"esito":"errore","motivo":"{:error, :not_found}"}`),
		"due righe":           ok + ok,
		"json rotto":          "GR_NUMERI:{esito\n",
		"dati in coda":        strings.TrimSuffix(rigaNumeri(esitoNumeriBuono), "\n") + "{}\n",
		"campo sconosciuto":   sostituisci(`"esito":"ok"`, `"esito":"ok","altro":1`),
		"altri id":            sostituisci(`[19,20]`, `[19,21]`),
		"numero con gli zeri": sostituisci(`"numeri":["19/26"`, `"numeri":["019/26"`),
		"contatore fermo":     sostituisci(`"prossimo":"21/26"`, `"prossimo":"20/26"`),
		"buste in più":        sostituisci(`"totale_buste":20`, `"totale_buste":21`),
		"totale mancante":     sostituisci(`,"totale_buste":20`, ``),
	} {
		if _, err := leggiEsitoNumeri(out); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
	if _, err := leggiEsitoNumeri(rigaNumeri(`{"esito":"errore","motivo":"not_found"}`)); err == nil || !strings.Contains(err.Error(), "not_found") {
		t.Errorf("errore senza il motivo: %v", err)
	}
}

// inventario: RIP-H-31, RIP-H-30, RIP-S-04, RIP-S-07
// Il dominio Go, con gli stessi dati del passo, dà esattamente quello che deve dare lo standalone
// della slice 220: numeri senza zeri, contatore mosso da «000020/26», «0/26» rifiutato, gli stessi
// testi italiani per campo e lo stesso numero rimostrato. Il database di prova non resta.
func TestNumeriNelGoComeLoStandalone(t *testing.T) {
	dir := t.TempDir()
	gogo, err := numeriNelGo(dir)
	if err != nil {
		t.Fatal(err)
	}
	sa, err := leggiEsitoNumeri(rigaNumeri(esitoNumeriBuono))
	if err != nil {
		t.Fatal(err)
	}
	if err := confrontaNumeri(sa, gogo); err != nil {
		t.Error(err)
	}
	if voci, err := os.ReadDir(dir); err != nil || len(voci) != 0 {
		t.Errorf("resti del database di prova: %v %v", voci, err)
	}
}

// Ogni differenza fra standalone e Go si vede: un testo, un numero rimostrato, un rifiuto in meno,
// il prossimo numero.
func TestConfrontaNumeriTrovaLeDifferenze(t *testing.T) {
	buono, err := leggiEsitoNumeri(rigaNumeri(esitoNumeriBuono))
	if err != nil {
		t.Fatal(err)
	}
	for nome, guasta := range map[string]func(*esitoNumeri){
		"testo inglese": func(e *esitoNumeri) {
			e.Rifiuti["vuoti"] = rifiutoNumeri{Errori: map[string][]string{"accepted_on": {"can't be blank"}, "description": {"Campo obbligatorio."}}, Numero: e.Rifiuti["vuoti"].Numero}
		},
		"numero rimostrato con gli zeri": func(e *esitoNumeri) {
			n := "019/26"
			e.Rifiuti["doppio"] = rifiutoNumeri{Errori: e.Rifiuti["doppio"].Errori, Numero: &n}
		},
		"rifiuto in meno":  func(e *esitoNumeri) { delete(e.Rifiuti, "zeri") },
		"rifiuto in più":   func(e *esitoNumeri) { e.Rifiuti["altro"] = rifiutoNumeri{} },
		"prossimo diverso": func(e *esitoNumeri) { e.Prossimo = "20/26" },
		"numero salvato":   func(e *esitoNumeri) { e.Numeri = []string{"019/26", "20/26"} },
	} {
		altro, _ := leggiEsitoNumeri(rigaNumeri(esitoNumeriBuono))
		guasta(&altro)
		if err := confrontaNumeri(buono, altro); err == nil {
			t.Errorf("%s: nessuna differenza", nome)
		}
	}
}
