package main

import (
	"strings"
	"testing"
)

// Il passo delle modifiche gira come il registro d'emergenza: immagine dello standalone, senza
// rete, con le protezioni e i dati dell'istanza, e nient'altro.
func TestComandoModifiche(t *testing.T) {
	n, _ := istanzaDiProva(t)
	c := comandoModifiche(n, "img:1", []string{"--env", "DATABASE_PATH=/data/gestrip.db"})
	s := strings.Join(c, " ")
	if valoreDopo(c, "--name") != n.base+"-"+passoModifiche || valoreDopo(c, "--network") != "none" || indice(c, "--rm") < 0 {
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
	if i < 0 || len(c) != i+4 || c[i+1] != "bin/gestrip_standalone" || c[i+2] != "eval" || c[i+3] != modificheStandalone {
		t.Fatalf("comando = %v", c)
	}
}

// Lo script usa le funzioni delle pagine con i loro contesti: la scheda (actor e session_user) per
// update_repair, clear_expected_on e transition_repair; la nuova busta per clienti e buste; la
// pagina pubblica (actor «public») per le risposte al preventivo. Nessun dato vero: solo nomi
// «sintetici» e numeri 0000.
func TestScriptModificheComeLePagine(t *testing.T) {
	for _, voluto := range []string{
		"GestripStandalone.LegacyImport.Release.quiet!()",
		"Application.ensure_all_started(:gestrip_standalone)",
		"\nemail = \"/run/secrets/admin_email\" |> File.read!() |> String.trim_trailing()\n",
		"audit_scheda = %{actor: email, session_user: {titolare.id, titolare.session_version}}",
		`pubblico = %{actor: "public", ip: "192.0.2.10"}`,
		"\"GR_MODIFICHE:\"",
		"System.halt(1)",
		// I valori riletti dallo standalone, non scritti nello script.
		"buste = Enum.map([b5, b6, b7, b8, b9, b10], riletta)",
		"totale_buste: Repo.aggregate(Repair, :count)",
	} {
		if !strings.Contains(modificheStandalone, voluto) {
			t.Errorf("lo script non contiene %q", voluto)
		}
	}
	conta := map[string]int{
		"Workshop.update_repair(":     6,
		"Workshop.clear_expected_on(": 1,
		"Workshop.transition_repair(": 4,
		"QuoteDecision.approve(":      1,
		"QuoteDecision.reject(":       1,
		"Workshop.create_repair(":     1,
		"nuova.(":                     6,
	}
	for f, n := range conta {
		if got := strings.Count(modificheStandalone, f); got != n {
			t.Errorf("%s chiamata %d volte, attese %d", f, got, n)
		}
	}
	// Ogni scrittura della scheda con il contesto della scheda; il cambio di tipo solo dove serve.
	if got := strings.Count(modificheStandalone, ", audit_scheda, compro_oro: nil)") + strings.Count(modificheStandalone, ", audit_scheda, compro_oro: true)"); got != 6 {
		t.Errorf("%d update_repair con il contesto della scheda", got)
	}
	if got := strings.Count(modificheStandalone, "compro_oro: true)"); got != 1 {
		t.Errorf("%d cambi di tipo", got)
	}
	if got := strings.Count(modificheStandalone, ", audit_scheda)"); got != 5 {
		t.Errorf("%d transizioni e Togli data con il contesto della scheda", got)
	}
	if got := strings.Count(modificheStandalone, ", pubblico)"); got != 2 {
		t.Errorf("%d risposte con il contesto pubblico", got)
	}
	for _, vietato := range []string{"Repo.update", "Repo.insert", "Repo.query", "Ecto.Changeset.change", "SQL"} {
		if strings.Contains(modificheStandalone, vietato) {
			t.Errorf("lo script scrive senza le funzioni delle pagine: %q", vietato)
		}
	}
}

func rigaModifiche(json string) string { return "GR_MODIFICHE:" + json + "\n" }

const esitoModificheBuono = `{"esito":"ok","buste":[5,6,7,8,9,10],"stati":["in_lavorazione","pronta","valutata","in_lavorazione","in_lavorazione","preventivo"],` +
	`"compro_oro":[false,false,true,false,false,false],"link_revocati":1,"accettato":true,"rifiutato":true,"totale_buste":10}`

func TestLeggiEsitoModifiche(t *testing.T) {
	ok := "avvio\n" + rigaModifiche(esitoModificheBuono)
	e, err := leggiEsitoModifiche(ok)
	if err != nil {
		t.Fatalf("esito buono rifiutato: %v", err)
	}
	if len(e.Buste) != 6 || e.TotaleBuste != 10 || e.LinkRevocati != 1 {
		t.Errorf("esito = %+v", e)
	}
	sostituisci := func(vecchio, nuovo string) string {
		if !strings.Contains(esitoModificheBuono, vecchio) {
			t.Fatalf("%q non c'è", vecchio)
		}
		return rigaModifiche(strings.Replace(esitoModificheBuono, vecchio, nuovo, 1))
	}
	for nome, out := range map[string]string{
		"senza riga":           "fatto\n",
		"esito errore":         rigaModifiche(`{"esito":"errore","motivo":"{:error, :invalid_artisan}"}`),
		"due righe":            ok + ok,
		"json rotto":           "GR_MODIFICHE:{esito\n",
		"dati in coda":         strings.TrimSuffix(rigaModifiche(esitoModificheBuono), "\n") + "{}\n",
		"campo sconosciuto":    sostituisci(`"esito":"ok"`, `"esito":"ok","altro":1`),
		"altri id":             sostituisci(`[5,6,7,8,9,10]`, `[6,7,8,9,10,11]`),
		"una busta in meno":    sostituisci(`[5,6,7,8,9,10]`, `[5,6,7,8,9]`),
		"stato diverso":        sostituisci(`"pronta"`, `"in_lavorazione"`),
		"tipo diverso":         sostituisci(`[false,false,true,`, `[false,false,false,`),
		"nessun link revocato": sostituisci(`"link_revocati":1`, `"link_revocati":0`),
		"due link revocati":    sostituisci(`"link_revocati":1`, `"link_revocati":2`),
		"senza accettato":      sostituisci(`"accettato":true`, `"accettato":false`),
		"senza rifiutato":      sostituisci(`"rifiutato":true`, `"rifiutato":false`),
		"buste in più":         sostituisci(`"totale_buste":10`, `"totale_buste":11`),
		"totale mancante":      sostituisci(`,"totale_buste":10`, ``),
		"id come stringhe":     sostituisci(`[5,6,7,8,9,10]`, `["5","6","7","8","9","10"]`),
	} {
		if _, err := leggiEsitoModifiche(out); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
	if _, err := leggiEsitoModifiche(rigaModifiche(`{"esito":"errore","motivo":"invalid_artisan"}`)); err == nil ||
		!strings.Contains(err.Error(), "invalid_artisan") {
		t.Errorf("errore senza il motivo: %v", err)
	}
}

// esitoModificheDiProva è l'esito del passo delle modifiche che i report di prova rispecchiano.
func esitoModificheDiProva() esitoModifiche {
	e, err := leggiEsitoModifiche(rigaModifiche(esitoModificheBuono))
	if err != nil {
		panic(err)
	}
	return e
}

// Il report dell'import Go deve avere lette e scritte tutte le buste dello standalone, quelle del
// passo comprese; senza il passo non si controlla.
func TestControllaModifiche(t *testing.T) {
	e := esitoModificheDiProva()
	if err := controllaModifiche(map[string]int64{"repairs": 10}, map[string]int64{"buste": 10}, e); err != nil {
		t.Errorf("tutte: %v", err)
	}
	for nome, c := range map[string][2]int64{"lette in meno": {9, 10}, "scritte in meno": {10, 9}, "le sole della cattura": {4, 4}} {
		if err := controllaModifiche(map[string]int64{"repairs": c[0]}, map[string]int64{"buste": c[1]}, e); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
	if err := controllaModifiche(map[string]int64{"repairs": 4}, map[string]int64{"buste": 4}, esitoModifiche{}); err != nil {
		t.Errorf("senza il passo: %v", err)
	}
}
