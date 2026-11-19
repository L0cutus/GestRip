package main

import (
	"strings"
	"testing"
)

// Il passo dei lavori spenti (slice 100) gira come gli altri: immagine dello standalone, senza rete, con le
// protezioni e i dati dell'istanza, e solo con le funzioni sue.
func TestComandoLavori(t *testing.T) {
	n, _ := istanzaDiProva(t)
	c := comandoLavori(n, "img:1", []string{"--env", "DATABASE_PATH=/data/gestrip.db"})
	s := strings.Join(c, " ")
	if valoreDopo(c, "--name") != n.base+"-"+passoLavori || valoreDopo(c, "--network") != "none" || indice(c, "--rm") < 0 {
		t.Errorf("nome, rete o --rm: %s", s)
	}
	if !strings.Contains(s, strings.Join(protezioniStandalone(n), " ")) {
		t.Errorf("protezioni: %s", s)
	}
	if !strings.Contains(s, strings.Join(montaggiStandalone(n), " ")) || !strings.Contains(s, "--env DATABASE_PATH=/data/gestrip.db") {
		t.Errorf("dati dell'istanza o ambiente: %s", s)
	}
	i := indice(c, "img:1")
	if i < 0 || len(c) != i+4 || c[i+1] != "bin/gestrip_standalone" || c[i+2] != "eval" || c[i+3] != scriptLavori() {
		t.Fatalf("comando = %v", c)
	}
	found := false
	for _, x := range suffissiContainer() {
		found = found || x == "-"+passoLavori
	}
	if !found {
		t.Error("banco giu non toglie il container dei lavori")
	}
}

// Lo script spegne i tre lavori con la funzione e le chiavi che scrive la pagina dello standalone, e controlla che
// niente sia dovuto; non fa girare il giro (Jobs.run_due/1 scriverebbe una riga) e non scrive direttamente.
func TestScriptLavori(t *testing.T) {
	s := scriptLavori()
	if len(chiaviLavoriBanco) != 3 {
		t.Fatalf("chiavi = %v", chiaviLavoriBanco)
	}
	for _, chiave := range []string{"jobs.overdue_enabled", "jobs.chases_enabled", "jobs.briefing_enabled"} {
		if !strings.Contains(s, `"`+chiave+`"`) {
			t.Errorf("lo script non spegne %s", chiave)
		}
	}
	for _, voluto := range []string{`Settings.put(Repo, chiave, "0")`, "Config.get()", "Config.enabled?(config, &1)", "Jobs.due?(&1, adesso)",
		`{[], [], []} -> IO.puts("GR_LAVORI:ok")`, "System.halt(1)", "LegacyImport.Release.quiet!()"} {
		if !strings.Contains(s, voluto) {
			t.Errorf("lo script non contiene %q", voluto)
		}
	}
	for _, vietato := range []string{"run_due", "Jobs.run(", "Repo.insert", "Repo.update_all", `"1"`} {
		if strings.Contains(s, vietato) {
			t.Errorf("lo script contiene %q", vietato)
		}
	}
}

func TestLeggiEsitoLavori(t *testing.T) {
	if err := leggiEsitoLavori("altro\nGR_LAVORI:ok\n"); err != nil {
		t.Error(err)
	}
	for _, out := range []string{"", "GR_LAVORI:ok\nGR_LAVORI:ok\n", "GR_LAVORI:errore {[], [:overdue], [:overdue]}\n"} {
		if leggiEsitoLavori(out) == nil {
			t.Errorf("%q accettato", out)
		}
	}
}
