package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/importer"
)

// Il passo del registro d'emergenza gira come il legacy import e come la vendita: immagine dello
// standalone, senza rete, con le protezioni e i dati dell'istanza, e nient'altro.
func TestComandoEmergenza(t *testing.T) {
	n, _ := istanzaDiProva(t)
	c := comandoEmergenza(n, "img:1", []string{"--env", "DATABASE_PATH=/data/gestrip.db"})
	s := strings.Join(c, " ")
	if valoreDopo(c, "--name") != n.base+"-"+passoEmergenza || valoreDopo(c, "--network") != "none" {
		t.Errorf("nome o rete: %s", s)
	}
	if !strings.Contains(s, strings.Join(protezioniStandalone(n), " ")) {
		t.Errorf("protezioni diverse da quelle dei passi vicini: %s", s)
	}
	if !strings.Contains(s, "--env DATABASE_PATH=/data/gestrip.db") {
		t.Errorf("ambiente dell'app perso: %s", s)
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
	for dst, voluto := range map[string]struct {
		src string
		ro  bool
	}{
		"/data":        {n.dir + "/data", false},
		"/data/media":  {n.dir + "/media", false},
		"/run/secrets": {n.dir + "/secrets", true},
	} {
		if src, ro := montaggio(c, dst); src != voluto.src || ro != voluto.ro {
			t.Errorf("%s montato da %q ro=%v, voluto %q ro=%v", dst, src, ro, voluto.src, voluto.ro)
		}
	}
	i := indice(c, "img:1")
	if i < 0 || len(c) != i+4 || c[i+1] != "bin/gestrip_standalone" || c[i+2] != "eval" || c[i+3] != emergenzaStandalone {
		t.Fatalf("comando = %v", c)
	}
}

// Lo script chiama le funzioni pubbliche del registro come la pagina (emergency_live.ex), con il
// contesto d'audit della pagina (solo actor), e avvia l'applicazione come il legacy import. Il QR
// della busta si mette dalla scheda (repair_live.ex: Workshop.update_repair/4), perché la cattura
// sintetica non ne ha.
func TestScriptEmergenzaComeLaPagina(t *testing.T) {
	for _, voluto := range []string{
		"GestripStandalone.LegacyImport.Release.quiet!()",
		"Application.ensure_all_started(:gestrip_standalone)",
		"\nemail = \"/run/secrets/admin_email\" |> File.read!() |> String.trim_trailing()\n",
		"\naudit = %{actor: email}\n",
		"notify_owner: false",
		"\"GR_EMERGENZA:\"",
		"System.halt(1)",
	} {
		if !strings.Contains(emergenzaStandalone, voluto) {
			t.Errorf("lo script del registro non contiene %q", voluto)
		}
	}
	// Le funzioni della pagina, nell'ordine della sequenza: QR dalla scheda, apertura, riga, storno,
	// scansione, chiusura.
	var pos []int
	for _, f := range []string{"Workshop.update_repair(", "EmergencyRegister.open_event(", "EmergencyRegister.create_entry(",
		"EmergencyRegister.reverse_entry(", "EmergencyRegister.scan(", "EmergencyRegister.close_event("} {
		if strings.Count(emergenzaStandalone, f) != 1 {
			t.Errorf("%s chiamata %d volte, ne serve una", f, strings.Count(emergenzaStandalone, f))
		}
		pos = append(pos, strings.Index(emergenzaStandalone, f))
	}
	for i := 1; i < len(pos); i++ {
		if pos[i-1] < 0 || pos[i] < pos[i-1] {
			t.Errorf("ordine delle chiamate: %v", pos)
		}
	}
	// Il contesto della pagina del registro va alle sue cinque funzioni; la scheda ha il suo.
	if n := strings.Count(emergenzaStandalone, ", audit)") + strings.Count(emergenzaStandalone, ", audit,"); n != 5 {
		t.Errorf("%d chiamate con il contesto d'audit del registro, ne servono 5", n)
	}
	if !strings.Contains(emergenzaStandalone, ", audit_scheda") {
		t.Error("la scheda senza il suo contesto d'audit")
	}
	// Il QR che la scheda mette sulla busta è lo stesso che la scansione cerca: un solo valore.
	if strings.Count(emergenzaStandalone, "\nqr = \"") != 1 || strings.Count(emergenzaStandalone, "qr =") != 1 {
		t.Error("il QR della busta non è definito una volta sola")
	}
	for _, voluto := range []string{
		`Workshop.update_repair(busta, %{"qr_code" => qr}, audit_scheda,`,
		"EmergencyRegister.scan(evento.id, qr, audit)",
		// Il contesto della scheda è quello di repair_live.ex: actor e session_user.
		"audit_scheda = %{actor: email, session_user: {titolare.id, titolare.session_version}}",
		// righe è una lettura dello standalone, non un numero scritto nello script.
		"righe: length(EmergencyRegister.entries(evento.id))",
		// Stato e conservazione dalla busta riletta dopo la scansione.
		"stato_busta: dopo.status",
		"conservazione: not is_nil(dopo.retention_started_at)",
	} {
		if !strings.Contains(emergenzaStandalone, voluto) {
			t.Errorf("lo script del registro non contiene %q", voluto)
		}
	}
	rilettura := strings.Index(emergenzaStandalone, "dopo = Repo.get!(Repair, scansionata)")
	if rilettura < 0 || rilettura < strings.Index(emergenzaStandalone, "EmergencyRegister.scan(") {
		t.Errorf("la busta non è riletta dopo la scansione (%d)", rilettura)
	}
	for _, vietato := range []string{"busta.status", "busta.retention_started_at", "righe: 2"} {
		if strings.Contains(emergenzaStandalone, vietato) {
			t.Errorf("lo script del registro contiene %q", vietato)
		}
	}
	for _, vietato := range []string{"INSERT", "UPDATE ", "DELETE", "%{})", ", %{}", "Repo.query", "SQL.query", "notify_owner: true"} {
		if strings.Contains(strings.ToUpper(emergenzaStandalone), strings.ToUpper(vietato)) {
			t.Errorf("lo script del registro contiene %q", vietato)
		}
	}
}

func rigaEmergenza(json string) string { return "GR_EMERGENZA:" + json + "\n" }

const esitoEmergenzaBuono = `{"esito":"ok","evento":3,"chiuso":true,"riga":5,"storno_di":5,"righe":2,"busta":2,"stato_busta":"consegnata","conservazione":true}`

func TestLeggiEsitoEmergenza(t *testing.T) {
	ok := "avvio\n" + rigaEmergenza(esitoEmergenzaBuono)
	e, err := leggiEsitoEmergenza(ok)
	if err != nil {
		t.Fatalf("esito buono rifiutato: %v", err)
	}
	if e.Righe != 2 || e.Evento != 3 || e.Busta != 2 || !e.Chiuso {
		t.Errorf("esito = %+v", e)
	}
	sostituisci := func(vecchio, nuovo string) string {
		return rigaEmergenza(strings.Replace(esitoEmergenzaBuono, vecchio, nuovo, 1))
	}
	for nome, out := range map[string]string{
		"senza riga":           "fatto\n",
		"esito errore":         rigaEmergenza(`{"esito":"errore","motivo":"{:error, :not_found}"}`),
		"due righe":            ok + ok,
		"json rotto":           "GR_EMERGENZA:{esito\n",
		"dati in coda":         strings.TrimSuffix(rigaEmergenza(esitoEmergenzaBuono), "\n") + "{}\n",
		"riga nel mezzo":       "x " + rigaEmergenza(esitoEmergenzaBuono),
		"evento non chiuso":    sostituisci(`"chiuso":true`, `"chiuso":false`),
		"senza evento":         sostituisci(`"evento":3`, `"evento":0`),
		"una riga sola":        sostituisci(`"righe":2`, `"righe":1`),
		"tre righe":            sostituisci(`"righe":2`, `"righe":3`),
		"storno di un'altra":   sostituisci(`"storno_di":5`, `"storno_di":4`),
		"senza riga stornata":  sostituisci(`"riga":5,`, ``),
		"riga e storno a zero": sostituisci(`"riga":5,"storno_di":5`, `"riga":0,"storno_di":0`),
		"busta non scansionata": sostituisci(`"stato_busta":"consegnata"`,
			`"stato_busta":"pronta"`),
		"senza busta":            sostituisci(`"busta":2`, `"busta":0`),
		"senza conservazione":    sostituisci(`"conservazione":true`, `"conservazione":false`),
		"campo sconosciuto":      sostituisci(`"esito":"ok"`, `"esito":"ok","altro":1`),
		"righe come stringa":     sostituisci(`"righe":2`, `"righe":"2"`),
		"conservazione mancante": sostituisci(`,"conservazione":true`, ``),
	} {
		if _, err := leggiEsitoEmergenza(out); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
	// L'errore porta il motivo dello standalone, per la diagnosi.
	if _, err := leggiEsitoEmergenza(rigaEmergenza(`{"esito":"errore","motivo":"already_open"}`)); err == nil ||
		!strings.Contains(err.Error(), "already_open") {
		t.Errorf("errore senza il motivo: %v", err)
	}
}

// esitoEmergenzaDiProva è l'esito del passo del registro che i report di prova rispecchiano.
func esitoEmergenzaDiProva() esitoEmergenza {
	return esitoEmergenza{Esito: "ok", Evento: 3, Chiuso: true, Riga: 5, StornoDi: 5, Righe: 2, Busta: 2,
		StatoBusta: "consegnata", Conservazione: true}
}

// avvisiEmergenza sono gli avvisi sugli istanti senza zona che il passo del registro fa nascere:
// righe e storno, l'evento chiuso, la busta scansionata.
func avvisiEmergenza(righe, eventi, buste int64) []importer.Voce {
	return []importer.Voce{
		avvisoIstante("emergency_entries", "inserted_at", righe),
		avvisoIstante("emergency_events", "closed_at", eventi),
		avvisoIstante("emergency_events", "updated_at", eventi),
		avvisoIstante("repairs", "retention_started_at", buste),
		avvisoIstante("repairs", "updated_at", buste),
	}
}

// Dopo il registro d'emergenza nella catena l'avviso traduzione/istante_senza_zona si pretende su
// tutte e sei le colonne decise, con le righe esatte dell'esito dei passi: foto di revisione della
// vendita, riga e storno, un evento chiuso (closed_at e updated_at), una busta scansionata
// (retention_started_at e updated_at).
func TestIstantiSenzaZonaTutteLeColonne(t *testing.T) {
	sha := strings.Repeat("c", 64)
	att := sorgenteGo{sha: sha, conteggiVendita: conteggiVendita(), emergenza: esitoEmergenzaDiProva()}
	want := map[string]int64{"sale_revision_photos.inserted_at": 1, "emergency_entries.inserted_at": 2,
		"emergency_events.closed_at": 1, "emergency_events.updated_at": 1,
		"repairs.retention_started_at": 1, "repairs.updated_at": 1}
	if _, n, err := controllaVendita(reportDiProva(sha), att); err != nil || !reflect.DeepEqual(n, want) {
		t.Fatalf("report buono: righe %v, %v", n, err)
	}
	// L'ordine degli avvisi nel report non conta.
	rovescio := reportDiProva(sha)
	for i, j := 0, len(rovescio.Avvisi)-1; i < j; i, j = i+1, j-1 {
		rovescio.Avvisi[i], rovescio.Avvisi[j] = rovescio.Avvisi[j], rovescio.Avvisi[i]
	}
	if _, _, err := controllaVendita(rovescio, att); err != nil {
		t.Errorf("avvisi in un altro ordine: %v", err)
	}
	// Le righe attese vengono dall'esito del passo, non da un numero fisso.
	quattro := reportDiProva(sha)
	quattro.Avvisi[5] = avvisoIstante("emergency_entries", "inserted_at", 4)
	att4 := att
	att4.emergenza.Righe = 4
	if _, n, err := controllaVendita(quattro, att4); err != nil || n["emergency_entries.inserted_at"] != 4 {
		t.Errorf("quattro righe del registro attese e trovate: %v %v", n, err)
	}
	if _, _, err := controllaVendita(quattro, att); err == nil {
		t.Error("quattro righe del registro nel report, due nel passo: accettato")
	}
	// Senza l'esito del registro (passo non fatto) la catena non passa per «zero righe uguali».
	attSenza := att
	attSenza.emergenza = esitoEmergenza{}
	if _, _, err := controllaVendita(reportDiProva(sha), attSenza); err == nil {
		t.Error("report con gli avvisi del registro e nessun passo del registro: accettato")
	}
	for i, k := range []string{"emergency_entries.inserted_at", "emergency_events.closed_at", "emergency_events.updated_at",
		"repairs.retention_started_at", "repairs.updated_at"} {
		manca := reportDiProva(sha)
		manca.Avvisi = append(append([]importer.Voce{}, manca.Avvisi[:5+i]...), manca.Avvisi[6+i:]...)
		if _, _, err := controllaVendita(manca, att); err == nil || !strings.Contains(err.Error(), k) {
			t.Errorf("senza l'avviso su %s: %v", k, err)
		}
		uno := reportDiProva(sha)
		*uno.Avvisi[5+i].Righe++
		if _, _, err := controllaVendita(uno, att); err == nil {
			t.Errorf("avviso su %s con una riga in più: accettato", k)
		}
		zero := reportDiProva(sha)
		*zero.Avvisi[5+i].Righe = 0
		if _, _, err := controllaVendita(zero, att); err == nil {
			t.Errorf("avviso su %s a zero righe: accettato", k)
		}
	}
	// Un avviso su una colonna non decisa resta un errore, anche con tutte e sei a posto.
	altra := reportDiProva(sha)
	altra.Avvisi = append(altra.Avvisi, avvisoIstante("emergency_events", "started_at", 1))
	if _, _, err := controllaVendita(altra, att); err == nil {
		t.Error("avviso su emergency_events.started_at: accettato")
	}
}

// Il riassunto dice che cosa ha fatto il passo del registro.
func TestRigheRiassuntoEmergenza(t *testing.T) {
	r := riassuntoImport{Verdetto: "riconciliato", Emergenza: riassuntoEmergenza{Righe: 2, EventiChiusi: 1, BusteScansionate: 1}}
	s := strings.Join(righeRiassunto(r), "\n")
	if !strings.Contains(s, "catena: registro d'emergenza: 2 righe (riga e storno), 1 evento chiuso, 1 busta scansionata") {
		t.Errorf("riassunto senza il registro d'emergenza:\n%s", s)
	}
}
