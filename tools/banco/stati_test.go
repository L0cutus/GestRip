package main

import (
	"strings"
	"testing"
)

// Il passo degli stati gira come le modifiche: immagine dello standalone, senza rete, con le
// protezioni e i dati dell'istanza, e nient'altro.
func TestComandoStati(t *testing.T) {
	n, _ := istanzaDiProva(t)
	c := comandoStati(n, "img:1", []string{"--env", "DATABASE_PATH=/data/gestrip.db"})
	s := strings.Join(c, " ")
	if valoreDopo(c, "--name") != n.base+"-"+passoStati || valoreDopo(c, "--network") != "none" || indice(c, "--rm") < 0 {
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
	if src, ro := montaggio(c, "/run/secrets"); src != n.dir+"/secrets" || !ro {
		t.Errorf("segreti da %q ro=%v", src, ro)
	}
	i := indice(c, "img:1")
	if i < 0 || len(c) != i+4 || c[i+1] != "bin/gestrip_standalone" || c[i+2] != "eval" || c[i+3] != statiStandalone {
		t.Fatalf("comando = %v", c)
	}
	found := false
	for _, x := range suffissiContainer() {
		found = found || x == "-"+passoStati
	}
	if !found {
		t.Error("banco giu non toglie il container degli stati")
	}
}

// Lo script usa le funzioni delle pagine con i loro contesti: l'evento transition della scheda
// (TelegramNotices.transition/3), la riapertura e l'attesa con il contesto della scheda (actor e
// session_user), i lotti con quello delle pagine dei lotti (actor). Nessun dato vero.
func TestScriptStatiComeLePagine(t *testing.T) {
	for _, voluto := range []string{
		"GestripStandalone.LegacyImport.Release.quiet!()",
		"Application.ensure_all_started(:gestrip_standalone)",
		"\nemail = \"/run/secrets/admin_email\" |> File.read!() |> String.trim_trailing()\n",
		"audit_scheda = %{actor: email, session_user: {titolare.id, titolare.session_version}}",
		"\"GR_STATI:\"",
		"System.halt(1)",
		"buste = Enum.map([b11, b12, b13, b14, b15], riletta)",
		"versioni: Enum.map(buste, &Workshop.status_version(&1.id))",
		"lotto_chiuso: not is_nil(Repo.get!(ReadyLot, lotto.id).closed_at)",
		"totale_buste: Repo.aggregate(Repair, :count)",
		// Le aperture della scheda per la paginazione del registro (scenario registro).
		"Enum.reduce_while(1..aperture_attese, {:ok, 0}",
		"Workshop.record_repair_event(riletta.(b11), \"repair.viewed\", audit_scheda)",
		"aperture: Enum.count(Workshop.list_audit_events(), &(&1.action == \"repair.viewed\" and &1.resource_id == Integer.to_string(b11.id)))",
	} {
		if !strings.Contains(statiStandalone, voluto) {
			t.Errorf("lo script non contiene %q", voluto)
		}
	}
	conta := map[string]int{
		"TelegramNotices.transition(":   1,
		"stato.(":                       9,
		"Workshop.reopen_repair(":       1,
		"Workshop.set_repair_waiting(":  4,
		"ReadyLots.open_lot(2, audit)":  1,
		"ReadyLots.add_bag(":            1,
		"ReadyLots.send_lot(lotto.id, ": 1,
		"nuova.(":                       5,
		"Workshop.transition_repair(":   0,
		"Workshop.record_repair_event(": 1,
	}
	for f, n := range conta {
		if got := strings.Count(statiStandalone, f); got != n {
			t.Errorf("%s chiamata %d volte, attese %d", f, got, n)
		}
	}
	if got := strings.Count(statiStandalone, ", audit_scheda)"); got != 9+1+4+1 {
		t.Errorf("%d azioni della scheda con il contesto della scheda", got)
	}
	for _, vietato := range []string{"Repo.update", "Repo.insert", "Repo.query", "Ecto.Changeset.change", "SQL"} {
		if strings.Contains(statiStandalone, vietato) {
			t.Errorf("lo script scrive senza le funzioni delle pagine: %q", vietato)
		}
	}
}

func rigaStati(json string) string { return "GR_STATI:" + json + "\n" }

const esitoStatiBuono = `{"esito":"ok","buste":[11,12,13,14,15],"stati":["in_attesa_materiali","annullata","chiusa","consegnata","pronta"],` +
	`"compro_oro":[false,false,true,false,false],"attesa":[true,true,false,false,false],"versioni":[1,1,3,2,3],"riaperture":1,` +
	`"lotto_chiuso":true,"lotti":1,"aperture":105,"totale_buste":15}`

func TestLeggiEsitoStati(t *testing.T) {
	ok := "avvio\n" + rigaStati(esitoStatiBuono)
	e, err := leggiEsitoStati(ok)
	if err != nil {
		t.Fatalf("esito buono rifiutato: %v", err)
	}
	if len(e.Buste) != 5 || e.TotaleBuste != 15 || e.Lotti != 1 || !e.LottoChiuso {
		t.Errorf("esito = %+v", e)
	}
	sostituisci := func(vecchio, nuovo string) string {
		if !strings.Contains(esitoStatiBuono, vecchio) {
			t.Fatalf("%q non c'è", vecchio)
		}
		return rigaStati(strings.Replace(esitoStatiBuono, vecchio, nuovo, 1))
	}
	for nome, out := range map[string]string{
		"senza riga":         "fatto\n",
		"esito errore":       rigaStati(`{"esito":"errore","motivo":"{:error, :not_ready}"}`),
		"due righe":          ok + ok,
		"json rotto":         "GR_STATI:{esito\n",
		"dati in coda":       strings.TrimSuffix(rigaStati(esitoStatiBuono), "\n") + "{}\n",
		"campo sconosciuto":  sostituisci(`"esito":"ok"`, `"esito":"ok","altro":1`),
		"altri id":           sostituisci(`[11,12,13,14,15]`, `[12,13,14,15,16]`),
		"una busta in meno":  sostituisci(`[11,12,13,14,15]`, `[11,12,13,14]`),
		"stato diverso":      sostituisci(`"consegnata"`, `"pronta"`),
		"tipo diverso":       sostituisci(`[false,false,true,`, `[false,false,false,`),
		"attesa diversa":     sostituisci(`[true,true,false,`, `[true,false,false,`),
		"versione diversa":   sostituisci(`[1,1,3,2,3]`, `[1,1,3,2,2]`),
		"nessuna riapertura": sostituisci(`"riaperture":1`, `"riaperture":0`),
		"due riaperture":     sostituisci(`"riaperture":1`, `"riaperture":2`),
		"lotto aperto":       sostituisci(`"lotto_chiuso":true`, `"lotto_chiuso":false`),
		"nessun lotto":       sostituisci(`"lotti":1`, `"lotti":0`),
		"aperture in meno":   sostituisci(`"aperture":105`, `"aperture":100`),
		"aperture in più":    sostituisci(`"aperture":105`, `"aperture":106`),
		"buste in più":       sostituisci(`"totale_buste":15`, `"totale_buste":16`),
		"totale mancante":    sostituisci(`,"totale_buste":15`, ``),
	} {
		if _, err := leggiEsitoStati(out); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
	if _, err := leggiEsitoStati(rigaStati(`{"esito":"errore","motivo":"not_ready"}`)); err == nil || !strings.Contains(err.Error(), "not_ready") {
		t.Errorf("errore senza il motivo: %v", err)
	}
}

// Il report dell'import Go deve avere letti e scritti tutti i lotti dello standalone; senza il passo
// non si controlla.
func TestControllaStati(t *testing.T) {
	e, err := leggiEsitoStati(rigaStati(esitoStatiBuono))
	if err != nil {
		t.Fatal(err)
	}
	if err := controllaStati(map[string]int64{"ready_lots": 1}, map[string]int64{"lotti_avviso": 1}, e); err != nil {
		t.Errorf("tutti: %v", err)
	}
	for nome, c := range map[string][2]int64{"letti in meno": {0, 1}, "scritti in meno": {1, 0}, "in più": {2, 2}} {
		if err := controllaStati(map[string]int64{"ready_lots": c[0]}, map[string]int64{"lotti_avviso": c[1]}, e); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
	if err := controllaStati(map[string]int64{}, map[string]int64{}, esitoStati{}); err != nil {
		t.Errorf("senza il passo: %v", err)
	}
}
