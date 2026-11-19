package web

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/jobs"
	"github.com/L0cutus/gestrip/internal/ratelimit"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// La pagina «Promemoria e job» (promemoria.go, slice 098, JOB-05, JOB-06), come JobsLive dello standalone
// (go-baseline, jobs_live_test.exs) senza LiveView, con la resa della D-16 (senza canali niente «▶ Esegui adesso» né
// righe «Saltato») e i giorni di apertura della D-102. Database vero, pianificatore vero con i tre lavori, coda con
// i provider veri (non vanno in rete). Dati inventati.

const rottaPromemoria = "/settings/promemoria"

// martedì 17/11/2026, 10:00 in negozio.
var martediPromemoria = time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)

type provaPromemoria struct {
	d *db.DB
	o *clock.Finto
	p *jobs.Pianificatore
}

func facciaConPromemoria(t *testing.T) (*provaPromemoria, *browser) {
	t.Helper()
	pp, h := facciaPromemoria(t)
	b := titolareDentro(t, h)
	b.vai("GET", "/", nil, nil) // il token della sessione nuova
	return pp, b
}

func facciaPromemoria(t *testing.T) (*provaPromemoria, http.Handler) {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(martediPromemoria)
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	inserisciConti(t, d, contiBase)
	coda := uscita.Nuova(d, o, map[string]uscita.Provider{uscita.CanaleEmail: uscita.NuovoSMTP(d, o),
		uscita.CanaleTelegram: uscita.NuovoTelegram(d, "http://bot.invalid")})
	p := jobs.Nuovo(d, o, coda, map[string]jobs.Lavoro{jobs.Ritardi: jobs.NuoviRitardi(d, coda), jobs.Solleciti: jobs.NuoviSolleciti(d, coda),
		jobs.Briefing: jobs.NuovoBriefing(d, coda)})
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti = o, ratelimit.Nuovo(o)
	dd.Buste = buste.Nuove(d, o)
	dd.Uscita = coda
	dd.Lavori = p
	h := mustInterna(t, dd)
	return &provaPromemoria{d: d, o: o, p: p}, h
}

func (pp *provaPromemoria) imposta(t *testing.T, chiave, valore string) {
	t.Helper()
	eseguiSQL(t, pp.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, '2026-11-01T08:00:00.000000Z')
		ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore`, chiave, valore)
}

func (pp *provaPromemoria) accendiEmail(t *testing.T) {
	t.Helper()
	pp.imposta(t, "outgoing.smtp.host", "smtp.esempio.it")
	pp.imposta(t, "outgoing.smtp.from_address", "negozio@esempio.it")
	pp.imposta(t, uscita.ChiaveEmailAcceso, "1")
}

func (pp *provaPromemoria) accendiTelegram(t *testing.T) {
	t.Helper()
	eseguiSQL(t, pp.d, `INSERT INTO segreti_provider (nome, valore, aggiornato_il) VALUES ('outgoing.telegram.bot_token', '4242:finto-SINTETICO', '2026-11-01T08:00:00.000000Z')`)
	pp.imposta(t, "outgoing.telegram.chat_id", "-1004242")
	pp.imposta(t, uscita.ChiaveTelegramAcceso, "1")
}

// bustaInRitardo crea una busta con la data promessa giorni fa.
func (pp *provaPromemoria) bustaInRitardo(t *testing.T, numero string, giorni int) {
	t.Helper()
	b := buste.Nuove(pp.d, pp.o)
	a := buste.Accettazione{NomeCliente: "Cliente " + numero, TelefonoCliente: "333 000 0001", Numero: numero, DataAccettazione: "2026-09-01",
		DataPrevista: clock.Locale(pp.o.Adesso()).AddDate(0, 0, -giorni).Format(time.DateOnly), Descrizione: "Anello da stringere"}
	if _, err := b.Crea(context.Background(), a, audit.Contesto{Attore: "titolare@esempio.it"}); err != nil {
		t.Fatal(err)
	}
}

func modPromemoria(b *browser, kv ...string) url.Values {
	v := url.Values{"_csrf_token": {b.token}}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Add(kv[i], kv[i+1])
	}
	return v
}

func righeEsecuzioni(t *testing.T, d *db.DB) int {
	return contaRighe(t, d, `SELECT count(*) FROM esecuzioni_lavori`)
}

// inventario: JOB-05
// La pagina del titolare senza canali: scheda «Promemoria» aperta, titolo, link ai solleciti, i tre lavori con i
// loro campi e i valori predefiniti (attivo, ora, soglia con min e max), «Ultima esecuzione: mai», le sette caselle dei
// giorni (martedì–sabato spuntati, D-102), «Salva cadenze e soglie», lo storico vuoto. D-16: la riga «partono solo con
// email o Telegram attivi» e nessun «▶ Esegui adesso».
func TestPaginaPromemoria(t *testing.T) {
	_, b := facciaConPromemoria(t)
	r := b.vai("GET", rottaPromemoria, nil, nil)
	if r.codice != 200 {
		t.Fatalf("pagina %d", r.codice)
	}
	contiene(t, "pagina", r.corpo, `<a id="settings-tab-promemoria" href="/settings/promemoria" aria-current="page" class="settings-tab">Promemoria</a>`,
		"<title>Promemoria e job · Laboratorio</title>", "<h1>Promemoria e job</h1>",
		`<p class="sotto">I promemoria girano dentro questa installazione: nessun servizio esterno, nessun timer di sistema. Se non hai configurato nessun canale di invio, un job non manda niente e lo dice qui sotto invece di provarci.</p>`,
		`<a href="/handovers/reminders" class="pulsante-contorno">Solleciti</a>`,
		`<p id="jobs-no-channels" class="flash flash-errore">`+msgPromemoriaSenzaCanali+`</p>`,
		`<form id="jobs-form" action="/settings/promemoria" method="post" data-invio-unico>`,
		`<section id="job-overdue" class="promemoria-lavoro">`, `<h2>Buste in ritardo</h2>`, `<h2>Solleciti artigiano</h2>`, `<h2>Briefing del mattino</h2>`,
		`<p id="job-last-overdue" class="sotto">Ultima esecuzione: mai</p>`,
		`<input type="hidden" name="jobs[overdue_enabled]" value="false"><label class="gs-spunta"><input type="checkbox" id="jobs_overdue_enabled" name="jobs[overdue_enabled]" value="true" checked><span>Attivo</span></label>`,
		`<input type="checkbox" id="jobs_briefing_enabled" name="jobs[briefing_enabled]" value="true"><span>Attivo</span>`,
		`<span class="label">Ora (Europe/Rome)</span>`+"\n"+`<input type="time" id="jobs_overdue_hour" name="jobs[overdue_hour]" value="09:00"></label>`,
		`<input type="time" id="jobs_chases_hour" name="jobs[chases_hour]" value="10:00">`, `<input type="time" id="jobs_briefing_hour" name="jobs[briefing_hour]" value="08:00">`,
		`<span class="label">Giorni di ritardo</span>`+"\n"+`<input type="number" id="jobs_overdue_days" name="jobs[overdue_days]" value="1" min="1" max="365"></label>`,
		`<span class="label">Giorni fuori bottega</span>`+"\n"+`<input type="number" id="jobs_chases_days" name="jobs[chases_days]" value="14" min="1"></label>`,
		`<span class="label">Fuori bottega (0 = escludi)</span>`+"\n"+`<input type="number" id="jobs_briefing_days" name="jobs[briefing_days]" value="14" min="0" max="365"></label>`,
		`<input type="hidden" name="jobs[giorni_apertura][]" value="">`,
		`<input type="checkbox" id="jobs_giorni_apertura_1" name="jobs[giorni_apertura][]" value="1"><span>Lunedì</span>`,
		`<input type="checkbox" id="jobs_giorni_apertura_2" name="jobs[giorni_apertura][]" value="2" checked><span>Martedì</span>`,
		`<input type="checkbox" id="jobs_giorni_apertura_6" name="jobs[giorni_apertura][]" value="6" checked><span>Sabato</span>`,
		`<input type="checkbox" id="jobs_giorni_apertura_7" name="jobs[giorni_apertura][]" value="7"><span>Domenica</span>`,
		`<button type="submit">Salva cadenze e soglie</button>`, "<h2>Storico delle esecuzioni</h2>", "Nessuna esecuzione registrata.")
	nonContiene(t, "pagina", r.corpo, "▶ Esegui adesso", "job-unreachable-", `class="field-error"`)
}

// inventario: JOB-05
// Il dipendente non raggiunge la pagina e non salva né esegue niente (rimandato a / come le altre pagine del
// titolare); senza sessione il login.
func TestPromemoriaDipendente(t *testing.T) {
	pp, h := facciaPromemoria(t)
	pp.accendiTelegram(t)
	b := dipendenteDentro(t, h)
	b.vai("GET", "/", nil, nil)
	for _, c := range []struct{ metodo, percorso string }{{"GET", rottaPromemoria}, {"POST", rottaPromemoria}, {"POST", rottaPromemoria + "/esegui"}} {
		r := b.vai(c.metodo, c.percorso, modPromemoria(b, "jobs[overdue_hour]", "07:00", "job", "overdue"), nil)
		if r.codice != 302 || r.location != "/" {
			t.Errorf("%s %s: %d %q", c.metodo, c.percorso, r.codice, r.location)
		}
	}
	if righeEsecuzioni(t, pp.d) != 0 || contaRighe(t, pp.d, `SELECT count(*) FROM impostazioni WHERE chiave LIKE 'jobs.%'`) != 0 {
		t.Error("il dipendente ha cambiato qualcosa")
	}
}

// inventario: JOB-05
// Il titolare sposta un'ora e una soglia: ritorno alla pagina con «Impostazioni salvate.» e i valori nuovi nei
// campi; la soglia dei solleciti è la riga della pagina dei solleciti. Lo spento si legge dal modulo (il nascosto
// «false» e la casella non spuntata). I giorni di apertura spuntati si salvano.
func TestPromemoriaSalva(t *testing.T) {
	pp, b := facciaConPromemoria(t)
	r := b.vai("POST", rottaPromemoria, modPromemoria(b, "jobs[overdue_hour]", "07:45", "jobs[chases_days]", "21",
		"jobs[overdue_enabled]", "false", "jobs[briefing_enabled]", "false", "jobs[briefing_enabled]", "true",
		"jobs[giorni_apertura][]", "", "jobs[giorni_apertura][]", "1", "jobs[giorni_apertura][]", "3"), nil)
	if !redirectA(r, rottaPromemoria) {
		t.Fatalf("salva: %d %q", r.codice, r.corpo)
	}
	r = b.vai("GET", rottaPromemoria, nil, nil)
	contiene(t, "dopo", r.corpo, "Impostazioni salvate.", `name="jobs[overdue_hour]" value="07:45"`, `name="jobs[chases_days]" value="21"`,
		`<input type="checkbox" id="jobs_overdue_enabled" name="jobs[overdue_enabled]" value="true"><span>`,
		`<input type="checkbox" id="jobs_briefing_enabled" name="jobs[briefing_enabled]" value="true" checked><span>`,
		`value="1" checked><span>Lunedì`, `value="2"><span>Martedì`, `value="3" checked><span>Mercoledì`)
	for k, voluto := range map[string]string{"jobs.overdue_hour": "07:45", buste.ChiaveSollecitiGiorni: "21", "jobs.overdue_enabled": "0",
		"jobs.briefing_enabled": "1", jobs.ChiaveGiorniApertura: "1,3"} {
		var v string
		if err := pp.d.Lettura.QueryRow(`SELECT valore FROM impostazioni WHERE chiave = ?`, k).Scan(&v); err != nil || v != voluto {
			t.Errorf("%s = %q (%v), voluto %q", k, v, err, voluto)
		}
	}
	if n := contaRighe(t, pp.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'jobs.configured' AND attore = 'titolare@esempio.it'`); n != 1 {
		t.Errorf("jobs.configured: %d", n)
	}
}

// inventario: JOB-05
// Un'ora che non è un'ora torna con il suo errore sotto il campo e il valore scritto, senza messaggio in alto e
// senza salvare; così un modulo senza nessun giorno di apertura (D-102). Un modulo senza la mappa jobs non salva.
func TestPromemoriaRifiuti(t *testing.T) {
	pp, b := facciaConPromemoria(t)
	r := b.vai("POST", rottaPromemoria, modPromemoria(b, "jobs[overdue_hour]", "alle nove", "jobs[briefing_days]", "366"), nil)
	if r.codice != 200 {
		t.Fatalf("ora non valida: %d", r.codice)
	}
	contiene(t, "ora non valida", r.corpo, `name="jobs[overdue_hour]" value="alle nove"></label>`+"\n"+`<p class="field-error">Orario non valido: usare HH:MM.</p>`,
		`<p class="field-error">Soglia non valida: da 0 a 365 giorni (0 la disattiva).</p>`, `name="jobs[briefing_days]" value="366"`,
		`<a id="settings-tab-promemoria"`)
	nonContiene(t, "ora non valida", r.corpo, "Impostazioni salvate.", "flash-info")
	r = b.vai("POST", rottaPromemoria, modPromemoria(b, "jobs[overdue_hour]", "07:00", "jobs[giorni_apertura][]", ""), nil)
	contiene(t, "nessun giorno", r.corpo, `<p class="field-error">`+jobs.MsgNessunGiorno+`</p>`, `name="jobs[overdue_hour]" value="07:00"`,
		`value="2"><span>Martedì`)
	// I giorni scritti come testo (non come la lista delle caselle) non sono validi.
	r = b.vai("POST", rottaPromemoria, modPromemoria(b, "jobs[giorni_apertura]", "3"), nil)
	contiene(t, "giorni come testo", r.corpo, `</div>`+"\n"+`<p class="field-error">`+jobs.MsgNonValido+`</p>`+"\n"+`</fieldset>`)
	// Un campo scritto come lista non è un testo.
	r = b.vai("POST", rottaPromemoria, modPromemoria(b, "jobs[overdue_days][]", "3"), nil)
	contiene(t, "lista", r.corpo, `name="jobs[overdue_days]" value="" min="1" max="365"></label>`+"\n"+`<p class="field-error">`+jobs.MsgNonValido+`</p>`)
	// «jobs[]» accanto ai campi: Plug ne farebbe una lista, non è la mappa.
	r = b.vai("POST", rottaPromemoria, modPromemoria(b, "jobs[]", "x", "jobs[overdue_hour]", "07:00"), nil)
	contiene(t, "lista e mappa", r.corpo, msgPromemoriaNonRiconosc)
	// «Attivo» dopo un rifiuto con «forse»: la casella non spuntata e l'errore sotto, come normalize_value("checkbox").
	r = b.vai("POST", rottaPromemoria, modPromemoria(b, "jobs[overdue_enabled]", "forse"), nil)
	contiene(t, "forse", r.corpo, `<input type="checkbox" id="jobs_overdue_enabled" name="jobs[overdue_enabled]" value="true"><span>Attivo</span></label>`+"\n"+
		`<p class="field-error">`+jobs.MsgNonValido+`</p>`)
	// «jobs» come testo accanto ai campi: non è la mappa, niente salvato.
	r = b.vai("POST", rottaPromemoria, modPromemoria(b, "jobs", "x", "jobs[overdue_hour]", "07:00"), nil)
	contiene(t, "testo e mappa", r.corpo, msgPromemoriaNonRiconosc)
	r = b.vai("POST", rottaPromemoria, modPromemoria(b, "jobs", "non-una-mappa"), nil)
	contiene(t, "senza mappa", r.corpo, msgPromemoriaNonRiconosc)
	if n := contaRighe(t, pp.d, `SELECT count(*) FROM impostazioni WHERE chiave LIKE 'jobs.%'`); n != 0 {
		t.Errorf("rifiuti salvati: %d", n)
	}
	if n := contaRighe(t, pp.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'jobs.configured'`); n != 0 {
		t.Errorf("audit dei rifiuti: %d", n)
	}
}

// inventario: JOB-06, JOB-04
// Con Telegram il titolare esegue i ritardi a mano: ritorno alla pagina con «Job eseguito.», la riga nello storico
// («Buste in ritardo», «Eseguito», ora del negozio, «A mano», buste 1, messaggi 1, «1 busta in ritardo.») e
// l'ultima esecuzione sotto il lavoro. Un nome che non è un lavoro non esegue niente.
func TestPromemoriaEseguiAdesso(t *testing.T) {
	pp, b := facciaConPromemoria(t)
	pp.accendiTelegram(t)
	pp.bustaInRitardo(t, "40/26", 3)
	r := b.vai("GET", rottaPromemoria, nil, nil)
	contiene(t, "con Telegram", r.corpo, `<form id="jobs-run" action="/settings/promemoria/esegui" method="post" data-invio-unico>`+"\n"+
		`<input name="_csrf_token" type="hidden" value="`+b.token+`">`+"\n"+`</form>`,
		`<button type="submit" form="jobs-run" name="job" value="overdue" class="pulsante-contorno">▶ Esegui adesso</button>`,
		`value="chases" class="pulsante-contorno">▶ Esegui adesso`, `value="briefing" class="pulsante-contorno">▶ Esegui adesso`)
	nonContiene(t, "con Telegram", r.corpo, "jobs-no-channels")
	r = b.vai("POST", rottaPromemoria+"/esegui", modPromemoria(b, "job", "overdue"), nil)
	if !redirectA(r, rottaPromemoria) {
		t.Fatalf("esegui: %d %q", r.codice, r.corpo)
	}
	r = b.vai("GET", rottaPromemoria, nil, nil)
	contiene(t, "dopo", r.corpo, "Job eseguito.", `<li id="job-run-1">`, `<span class="nome">Buste in ritardo</span> <span class="badge badge-ok">Eseguito</span>`,
		`<p class="sotto">17/11/2026 10:00 · A mano · buste 1 · messaggi 1</p>`, `<p>1 busta in ritardo.</p>`,
		`<p id="job-last-overdue" class="sotto">Ultima esecuzione: 17/11/2026 10:00 · Eseguito</p>`,
		`<p id="job-last-chases" class="sotto">Ultima esecuzione: mai</p>`)
	nonContiene(t, "dopo", r.corpo, "Nessuna esecuzione registrata.")
	// Il briefing spento a mano: «Saltato» con la frase dello standalone, che non conta per il giorno.
	b.vai("POST", rottaPromemoria+"/esegui", modPromemoria(b, "job", "briefing"), nil)
	r = b.vai("GET", rottaPromemoria, nil, nil)
	contiene(t, "briefing spento", r.corpo, `<span class="nome">Briefing del mattino</span> <span class="badge badge-attesa">Saltato</span>`,
		`<p>Job disattivato dal titolare.</p>`)
	for _, nome := range []string{"qualunque", "", "Overdue"} {
		r = b.vai("POST", rottaPromemoria+"/esegui", modPromemoria(b, "job", nome), nil)
		if !redirectA(r, rottaPromemoria) {
			t.Errorf("nome %q: %d", nome, r.codice)
		}
	}
	if n := righeEsecuzioni(t, pp.d); n != 2 {
		t.Errorf("esecuzioni: %d", n)
	}
	if n := contaRighe(t, pp.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'jobs.ran' AND attore = 'titolare@esempio.it'`); n != 2 {
		t.Errorf("jobs.ran del titolare: %d", n)
	}
}

// inventario: JOB-06, JOB-03
// La resa della D-16: senza canali un «Esegui adesso» mandato lo stesso non scrive righe e lo dice; le righe
// importate restano nello storico (anche «Saltato»). Con la sola email il briefing non ha il pulsante e una riga dice
// che parte solo con Telegram; mandato lo stesso, niente righe.
func TestPromemoriaSenzaCanali(t *testing.T) {
	pp, b := facciaConPromemoria(t)
	eseguiSQL(t, pp.d, `INSERT INTO esecuzioni_lavori (lavoro, avvio, esito, eseguito_il, giorno, avvisi, messaggi, dettaglio)
		VALUES ('overdue', 'scheduled', 'skipped', '2026-07-15T08:05:00.000000Z', '2026-07-15', 0, 0, 'Nessun canale di invio configurato.')`)
	r := b.vai("POST", rottaPromemoria+"/esegui", modPromemoria(b, "job", "overdue"), nil)
	if !redirectA(r, rottaPromemoria) {
		t.Fatalf("senza canali: %d", r.codice)
	}
	r = b.vai("GET", rottaPromemoria, nil, nil)
	contiene(t, "senza canali", r.corpo, msgEseguiSenzaCanali, `<span class="badge badge-attesa">Saltato</span>`,
		`<p class="sotto">15/07/2026 10:05 · Automatico · buste 0 · messaggi 0</p>`, `<p>Nessun canale di invio configurato.</p>`,
		`<p id="job-last-overdue" class="sotto">Ultima esecuzione: 15/07/2026 10:05 · Saltato</p>`)
	nonContiene(t, "senza canali", r.corpo, "Job eseguito.")
	pp.accendiEmail(t)
	r = b.vai("GET", rottaPromemoria, nil, nil)
	contiene(t, "sola email", r.corpo, `value="overdue" class="pulsante-contorno">▶ Esegui adesso`,
		`<p id="job-unreachable-briefing" class="sotto">`+msgBriefingSoloTelegram+`</p>`)
	nonContiene(t, "sola email", r.corpo, `value="briefing" class="pulsante-contorno">`, "jobs-no-channels")
	b.vai("POST", rottaPromemoria+"/esegui", modPromemoria(b, "job", "briefing"), nil)
	contiene(t, "briefing con sola email", b.vai("GET", rottaPromemoria, nil, nil).corpo, msgBriefingNonEseguito)
	if n := righeEsecuzioni(t, pp.d); n != 1 {
		t.Errorf("esecuzioni: %d", n)
	}
}

// inventario: JOB-04, JOB-05
// Lo storico mostra le ultime 20 righe, la più recente prima, con l'ora del negozio (luglio: UTC+2).
func TestPromemoriaStorico(t *testing.T) {
	pp, b := facciaConPromemoria(t)
	for i := 0; i < 22; i++ {
		eseguiSQL(t, pp.d, `INSERT INTO esecuzioni_lavori (lavoro, avvio, esito, eseguito_il, giorno, avvisi, messaggi, dettaglio)
			VALUES ('chases', 'manual', 'failed', '2026-07-15T08:05:00.000000Z', '2026-07-15', 2, 3, NULL)`)
	}
	r := b.vai("GET", rottaPromemoria, nil, nil)
	if n := strings.Count(r.corpo, `<li id="job-run-`); n != 20 {
		t.Errorf("righe: %d", n)
	}
	contiene(t, "storico", r.corpo, `<li id="job-run-22">`, `<li id="job-run-3">`, `<span class="badge badge-errore">Fallito</span>`,
		`15/07/2026 10:05 · A mano · buste 2 · messaggi 3`)
	nonContiene(t, "storico", r.corpo, `<li id="job-run-2">`, "08:05")
}
