package main

import (
	"strings"
	"testing"
)

// Il passo del link gira come gli stati: immagine dello standalone, senza rete, con le protezioni e
// i dati dell'istanza, e nient'altro.
func TestComandoLink(t *testing.T) {
	n, _ := istanzaDiProva(t)
	c := comandoLink(n, "img:1", []string{"--env", "DATABASE_PATH=/data/gestrip.db"})
	s := strings.Join(c, " ")
	if valoreDopo(c, "--name") != n.base+"-"+passoLink || valoreDopo(c, "--network") != "none" || indice(c, "--rm") < 0 {
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
	if i < 0 || len(c) != i+4 || c[i+1] != "bin/gestrip_standalone" || c[i+2] != "eval" || c[i+3] != linkStandalone {
		t.Fatalf("comando = %v", c)
	}
	found := false
	for _, x := range suffissiContainer() {
		found = found || x == "-"+passoLink
	}
	if !found {
		t.Error("banco giu non toglie il container del link")
	}
}

// Lo script usa le funzioni degli eventi revoke-link e regenerate-link della scheda con il contesto
// della scheda (actor e session_user), le buste nuove come la nuova busta; il token non esce mai
// (né stampato, né nell'esito); nessuna scrittura diretta.
func TestScriptLinkComeLePagine(t *testing.T) {
	for _, voluto := range []string{
		"GestripStandalone.LegacyImport.Release.quiet!()",
		"Application.ensure_all_started(:gestrip_standalone)",
		"\nemail = \"/run/secrets/admin_email\" |> File.read!() |> String.trim_trailing()\n",
		"audit_scheda = %{actor: email, session_user: {titolare.id, titolare.session_version}}",
		"Workshop.regenerate_public_link(riletta.(busta), contesto)",
		"{:ok, rinnovata, _token} -> {:ok, rinnovata}",
		"Workshop.revoke_public_access(riletta.(busta), contesto)",
		"%Repair{} = b1 <- Repo.get_by(Repair, bag_number: \"1/26\")",
		"%Repair{} = b3 <- Repo.get_by(Repair, bag_number: \"3/26\")",
		"buste = Enum.map([b1, b3, b16, b17, b18], riletta)",
		"Workshop.public_link_status()",
		"Workshop.legacy_link_status()",
		"totale_buste: Repo.aggregate(Repair, :count)",
		"\"GR_LINK:\"",
		"System.halt(1)",
	} {
		if !strings.Contains(linkStandalone, voluto) {
			t.Errorf("lo script non contiene %q", voluto)
		}
	}
	conta := map[string]int{
		"nuovo_link.(":                     3,
		"revoca.(":                         3,
		"nuova.(":                          3,
		", audit_scheda)":                  6,
		"Workshop.ensure_public_link(":     0,
		"public_token":                     0,
		"\"oreficeria_id\" => \"2\"":       1,
		"Workshop.regenerate_public_link(": 1,
		"Workshop.revoke_public_access(":   1,
		"Workshop.create_repair(cliente,":  1,
		"Workshop.create_customer(%{name:": 1,
	}
	for f, n := range conta {
		if got := strings.Count(linkStandalone, f); got != n {
			t.Errorf("%s %d volte, attese %d", f, got, n)
		}
	}
	for _, vietato := range []string{"Repo.update", "Repo.insert", "Repo.query", "Ecto.Changeset.change", "SQL", "IO.inspect", "_token}\n"} {
		if strings.Contains(linkStandalone, vietato) {
			t.Errorf("lo script contiene %q", vietato)
		}
	}
}

func rigaLink(json string) string { return "GR_LINK:" + json + "\n" }

const esitoLinkBuono = `{"esito":"ok","buste":[1,3,16,17,18],"link":["usable","revoked","revoked","usable","usable"],` +
	`"storico":["revoked","revoked","none","none","none"],"emessi":3,"emessi_storico":1,"revocati":3,"revocati_storico":1,"totale_buste":18}`

func TestLeggiEsitoLink(t *testing.T) {
	ok := "avvio\n" + rigaLink(esitoLinkBuono)
	e, err := leggiEsitoLink(ok)
	if err != nil {
		t.Fatalf("esito buono rifiutato: %v", err)
	}
	if len(e.Buste) != 5 || e.TotaleBuste != 18 || e.Emessi != 3 || e.Revocati != 3 {
		t.Errorf("esito = %+v", e)
	}
	sostituisci := func(vecchio, nuovo string) string {
		if !strings.Contains(esitoLinkBuono, vecchio) {
			t.Fatalf("%q non c'è", vecchio)
		}
		return rigaLink(strings.Replace(esitoLinkBuono, vecchio, nuovo, 1))
	}
	for nome, out := range map[string]string{
		"senza riga":                  "fatto\n",
		"esito errore":                rigaLink(`{"esito":"errore","motivo":"{:error, :not_found}"}`),
		"due righe":                   ok + ok,
		"json rotto":                  "GR_LINK:{esito\n",
		"dati in coda":                strings.TrimSuffix(rigaLink(esitoLinkBuono), "\n") + "{}\n",
		"campo sconosciuto":           sostituisci(`"esito":"ok"`, `"esito":"ok","token":"x"`),
		"altri id":                    sostituisci(`[1,3,16,17,18]`, `[1,3,16,17,19]`),
		"una busta in meno":           sostituisci(`[1,3,16,17,18]`, `[1,3,16,17]`),
		"link diverso":                sostituisci(`"revoked","revoked","usable"`, `"revoked","usable","usable"`),
		"vecchio link vivo":           sostituisci(`["revoked","revoked","none"`, `["live","revoked","none"`),
		"un nuovo link in meno":       sostituisci(`"emessi":3`, `"emessi":2`),
		"vecchio link nel nuovo link": sostituisci(`"emessi_storico":1`, `"emessi_storico":0`),
		"una revoca in più":           sostituisci(`"revocati":3`, `"revocati":4`),
		"vecchio link non revocato":   sostituisci(`"revocati_storico":1`, `"revocati_storico":0`),
		"buste in più":                sostituisci(`"totale_buste":18`, `"totale_buste":19`),
		"totale mancante":             sostituisci(`,"totale_buste":18`, ``),
	} {
		if _, err := leggiEsitoLink(out); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
	if _, err := leggiEsitoLink(rigaLink(`{"esito":"errore","motivo":"not_found"}`)); err == nil || !strings.Contains(err.Error(), "not_found") {
		t.Errorf("errore senza il motivo: %v", err)
	}
}
