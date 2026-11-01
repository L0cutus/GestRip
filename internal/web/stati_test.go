package web

import (
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/buste"
)

// Stati, riapertura e attesa dalla scheda (RIP-S-13, RIP-S-14, RIP-S-15), come gli eventi
// transition, reopen e waiting di RepairLive dello standalone (go-baseline), su account e buste
// veri. I casi da repair_live_test.exs: «status uses validated transition and general save cannot
// override it», «a delivered bag reopens from the detail page, with a confirmation and a reason»,
// «staff waiting form survives validation, updates detail and list, and clears on reply» e
// «waiting refuses a deactivated live session…».

func azioneStato(id int64) string  { return percorsoScheda(id) + "/stato" }
func azioneRiapri(id int64) string { return percorsoScheda(id) + "/riapri" }
func azioneAttesa(id int64) string { return percorsoScheda(id) + "/attesa" }
func formStato(b *browser, s string) url.Values {
	return url.Values{"_csrf_token": {b.token}, "transition[status]": {s}}
}

func formRiapri(b *browser, stato, motivo string) url.Values {
	return url.Values{"_csrf_token": {b.token}, "repair[status]": {stato}, "repair[reopen_reason]": {motivo}}
}

func formAttesa(b *browser, azione, motivo string) url.Values {
	return url.Values{"_csrf_token": {b.token}, "waiting[azione]": {azione}, "waiting[motivo]": {motivo}}
}

// cambia apre la scheda (il token CSRF cambia all'accesso), manda il cambio di stato e vuole il
// ritorno alla scheda.
func cambia(t *testing.T, b *browser, id int64, s string) {
	t.Helper()
	b.vai("GET", percorsoScheda(id), nil, nil)
	if r := b.vai("POST", azioneStato(id), formStato(b, s), nil); !redirectA(r, percorsoScheda(id)) {
		t.Fatalf("%s: %d %q", s, r.codice, r.location)
	}
}

// inventario: RIP-S-13
// «Cambia stato» propone solo gli stati raggiungibili da quello attuale, con «Seleziona» davanti;
// «Aggiorna stato» li applica e torna alla scheda con «Stato aggiornato.». Uno stato fuori dal
// grafo dice «Transizione non consentita.» e non cambia niente; il modulo generale non cambia lo
// stato né le date; una busta consegnata non ha più il modulo, ha quello della riapertura.
func TestPaginaCambioStato(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := dipendenteDentro(t, h)
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "modulo", r.corpo,
		`<form id="repair-status-form" action="/repairs/`+strconv.FormatInt(id, 10)+`/stato" method="post" class="modulo-in-riga" data-invio-unico>`,
		`<label for="transition_status">`+"\n"+`<span class="label">Cambia stato</span>`+"\n"+`<select id="transition_status" name="transition[status]" required>`+"\n"+
			`<option value="">Seleziona</option>`+"\n"+`<option value="preventivo">Preventivo</option>`+"\n"+`<option value="pronta">Pronta</option>`+"\n"+
			`<option value="in_attesa_materiali">In attesa materiali</option>`+"\n"+`<option value="annullata">Annullata</option>`+"\n"+`</select>`,
		`<button type="submit">Aggiorna stato</button>`)
	nonContiene(t, "modulo", r.corpo, `<option value="in_lavorazione">`, `<option value="consegnata">`, `id="repair-reopen-form"`)
	if strings.Index(r.corpo, `id="edit-repair"`) > strings.Index(r.corpo, `id="repair-status-form"`) {
		t.Error("il modulo degli stati viene prima di «Modifica»")
	}

	cambia(t, b, id, "pronta")
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "pronta", r.corpo, `<div role="status" class="flash flash-info">Stato aggiornato.</div>`,
		`<dd id="repair-status" class="forte">Pronta</dd>`, `<dd id="repair-ready-on">31/10/2026</dd>`,
		`<option value="in_lavorazione">In lavorazione</option>`+"\n"+`<option value="consegnata">Consegnata</option>`+"\n"+`<option value="annullata">Annullata</option>`)
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed' AND attore = 'banco@esempio.it' AND dati = '{"status":"pronta"}'`); n != 1 {
		t.Errorf("audit %d", n)
	}

	for _, s := range []string{"preventivo", "", "valutata", "boh"} {
		cambia(t, b, id, s)
		contiene(t, s, b.vai("GET", percorsoScheda(id), nil, nil).corpo,
			`<div role="alert" class="flash flash-errore">Transizione non consentita.</div>`, `<dd id="repair-status" class="forte">Pronta</dd>`)
	}

	// Il modulo generale non cambia stato né date.
	r = salva(t, b, id, map[string]string{"repair[description]": "Aggiornata", "repair[status]": "consegnata", "repair[ready_on]": "2000-01-01"})
	if !redirectA(r, percorsoScheda(id)) {
		t.Fatalf("modifica: %d %q", r.codice, r.location)
	}
	if got := colonna(t, d, id, "stato") + "|" + colonna(t, d, id, "data_pronta"); got != "pronta|2026-10-31" {
		t.Errorf("dopo la modifica %s", got)
	}

	cambia(t, b, id, "consegnata")
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "consegnata", r.corpo, `<dd id="repair-status" class="forte">Consegnata</dd>`, `id="repair-reopen-form"`)
	nonContiene(t, "consegnata", r.corpo, `id="repair-status-form"`)
	cambia(t, b, id, "in_lavorazione")
	contiene(t, "dopo la consegna", b.vai("GET", percorsoScheda(id), nil, nil).corpo, "Transizione non consentita.")
	if got := colonna(t, d, id, "stato"); got != "consegnata" {
		t.Errorf("stato %s", got)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed'`); n != 2 {
		t.Errorf("repair.status_changed %d", n)
	}
}

// inventario: RIP-S-13
// Lo stato che la busta ha già (una pagina vecchia o un modulo rimandato) sulla scheda è un rifiuto,
// come lo standalone (repair_live.ex:212-223 non confronta, il dominio rifiuta): ritorno alla
// scheda con «Transizione non consentita.», nessuna scrittura. Il silenzio è solo della lista
// (workshop_live.ex:222-229, T4.2). Una compro oro propone i suoi stati.
func TestPaginaStatoUguale(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	cambia(t, b, id, "in_lavorazione")
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "stesso stato", r.corpo, `<div role="alert" class="flash flash-errore">Transizione non consentita.</div>`,
		`<dd id="repair-status" class="forte">In lavorazione</dd>`)
	nonContiene(t, "stesso stato", r.corpo, "flash-info")
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed'`); n != 0 {
		t.Errorf("repair.status_changed %d", n)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM versioni_stato_buste`); n != 0 {
		t.Errorf("versioni %d", n)
	}
	oro := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.CompraOro = "1"; a.CodiceQR = "QR-ORO" })
	r = b.vai("GET", percorsoScheda(oro), nil, nil)
	contiene(t, "compro oro", r.corpo, `<option value="">Seleziona</option>`+"\n"+`<option value="valutata">Valutata</option>`+"\n"+`<option value="annullata">Annullata</option>`+"\n"+`</select>`)
}

// inventario: RIP-S-13, RIP-S-14, RIP-S-15
// Senza il token CSRF 403; senza accesso o con l'account disattivato si va al login; un id
// malformato, una busta che non c'è o nel cestino tornano alla home con «Riparazione non
// trovata.». Niente cambia.
func TestPaginaStatiSenzaAccesso(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := dipendenteDentro(t, h)
	b.vai("GET", percorsoScheda(id), nil, nil)
	moduli := map[string]url.Values{azioneStato(id): formStato(b, "pronta"), azioneRiapri(id): formRiapri(b, "pronta", "Motivo"),
		azioneAttesa(id): formAttesa(b, "on", "Motivo")}
	for p, v := range moduli {
		senza := url.Values{}
		for k, vv := range v {
			if k != "_csrf_token" {
				senza[k] = vv
			}
		}
		if r := b.vai("POST", p, senza, nil); r.codice != 403 {
			t.Errorf("%s senza CSRF: %d", p, r.codice)
		}
		if r := nuovoBrowser(t, h).vai("POST", p, senza, nil); r.codice != 403 {
			t.Errorf("%s anonimo: %d", p, r.codice)
		}
	}
	eseguiSQL(t, d, `UPDATE utenti SET attivo = 0 WHERE email = 'banco@esempio.it'`)
	for p, v := range moduli {
		if r := b.vai("POST", p, v, nil); !redirectA(r, "/login") {
			t.Errorf("%s disattivato: %d %q", p, r.codice, r.location)
		}
	}
	tit := titolareDentro(t, h)
	tit.vai("GET", "/", nil, nil)
	cestinata := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.CodiceQR = "QR-CESTINO" })
	eseguiSQL(t, d, `UPDATE buste SET cancellata_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, cestinata)
	for _, p := range []string{"/repairs/999/stato", "/repairs/abc/stato", "/repairs/0/riapri", "/repairs/x/riapri", "/repairs/999/riapri",
		"/repairs/-1/attesa", "/repairs/999/attesa", azioneStato(cestinata), azioneRiapri(cestinata), azioneAttesa(cestinata)} {
		v := url.Values{"_csrf_token": {tit.token}, "transition[status]": {"pronta"}, "repair[status]": {"pronta"},
			"repair[reopen_reason]": {"Motivo"}, "waiting[azione]": {"on"}}
		r := tit.vai("POST", p, v, nil)
		if !redirectA(r, "/") {
			t.Errorf("%s: %d %q", p, r.codice, r.location)
			continue
		}
		contiene(t, p, tit.vai("GET", "/", nil, nil).corpo, "Riparazione non trovata.")
	}
	if got := colonna(t, d, id, "stato") + "|" + colonna(t, d, id, "attesa_cliente"); got != "in_lavorazione|0" {
		t.Errorf("busta cambiata: %s", got)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione IN ('repair.status_changed', 'repair.reopened', 'repair.waiting_started')`); n != 0 {
		t.Errorf("audit %d", n)
	}
}

// inventario: RIP-S-14
// Una busta consegnata ha «Riapri la busta» al posto di «Cambia stato»: gli stati di lavoro della
// sua famiglia (mai i chiusi né l'altra famiglia), il motivo e il pulsante con la conferma. Un
// motivo vuoto ridisegna la scheda con «Motivo obbligatorio.» sotto il campo e i valori scritti;
// con il motivo la busta si riapre, «Busta riaperta.», le date restano e torna «Cambia stato». Una
// riapertura forgiata su una busta aperta non cambia niente.
func TestPaginaRiapertura(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := dipendenteDentro(t, h)
	cambia(t, b, id, "pronta")
	cambia(t, b, id, "consegnata")
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "modulo", r.corpo,
		`<form id="repair-reopen-form" action="/repairs/`+strconv.FormatInt(id, 10)+`/riapri" method="post" class="riquadro-riapertura" data-invio-unico>`,
		"<h3>Riapri la busta</h3>",
		"La busta è chiusa. Riaprirla non tocca le date già registrate: restano come sono, e restano anche dopo una seconda consegna. Il motivo finisce nel registro insieme a chi ha riaperto.",
		`<label for="repair_status">`+"\n"+`<span class="label">Riapri in stato</span>`+"\n"+`<select id="repair_status" name="repair[status]" required>`+"\n"+
			`<option value="">Seleziona</option>`+"\n"+`<option value="in_lavorazione">In lavorazione</option>`+"\n"+`<option value="preventivo">Preventivo</option>`+"\n"+
			`<option value="pronta">Pronta</option>`+"\n"+`<option value="in_attesa_materiali">In attesa materiali</option>`+"\n"+`</select>`,
		`<label for="repair_reopen_reason">`+"\n"+`<span class="label">Motivo</span>`+"\n"+`<input type="text" name="repair[reopen_reason]" id="repair_reopen_reason" required>`,
		`<button id="reopen-repair" type="submit" data-confirm="Riaprire questa busta chiusa? Il motivo resta nel registro.">Riapri busta</button>`)
	for _, s := range []string{"consegnata", "annullata", "chiusa", "in_valutazione", "valutata"} {
		nonContiene(t, s, r.corpo, `<option value="`+s+`">`)
	}

	r = b.vai("POST", azioneRiapri(id), formRiapri(b, "in_lavorazione", "  "), nil)
	if r.codice != 200 {
		t.Fatalf("motivo vuoto: %d %q", r.codice, r.location)
	}
	contiene(t, "motivo vuoto", r.corpo, `<option value="in_lavorazione" selected>In lavorazione</option>`,
		`<input type="text" name="repair[reopen_reason]" id="repair_reopen_reason" value="  " required>`+"\n"+`</label>`+"\n"+`<p class="field-error">Motivo obbligatorio.</p>`,
		`<dd id="repair-status" class="forte">Consegnata</dd>`, `id="app-mobile-navigation"`)
	r = b.vai("POST", azioneRiapri(id), formRiapri(b, "annullata", "Ripensamento"), nil)
	contiene(t, "stato chiuso", r.corpo, `</select>`+"\n"+`</label>`+"\n"+`<p class="field-error">non riapre la busta</p>`,
		`id="repair_reopen_reason" value="Ripensamento" required>`)
	if got := colonna(t, d, id, "stato"); got != "consegnata" {
		t.Fatalf("stato %s", got)
	}

	if r := b.vai("POST", azioneRiapri(id), formRiapri(b, "in_lavorazione", "Pietra da rifissare"), nil); !redirectA(r, percorsoScheda(id)) {
		t.Fatalf("riapertura: %d %q", r.codice, r.location)
	}
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "riaperta", r.corpo, `<div role="status" class="flash flash-info">Busta riaperta.</div>`,
		`<dd id="repair-status" class="forte">In lavorazione</dd>`, `<dd id="repair-ready-on">31/10/2026</dd>`,
		`<dd id="repair-delivered-on">31/10/2026</dd>`, `id="repair-status-form"`)
	nonContiene(t, "riaperta", r.corpo, `id="repair-reopen-form"`)
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.reopened' AND attore = 'banco@esempio.it' AND
		dati = '{"reason":"Pietra da rifissare","status_from":"consegnata","status_to":"in_lavorazione"}'`); n != 1 {
		t.Errorf("audit %d", n)
	}

	// Forgiata su una busta aperta: la scheda senza il modulo, niente cambia.
	r = b.vai("POST", azioneRiapri(id), formRiapri(b, "pronta", "Forzatura"), nil)
	if r.codice != 200 {
		t.Fatalf("forgiata: %d %q", r.codice, r.location)
	}
	nonContiene(t, "forgiata", r.corpo, `id="repair-reopen-form"`, "flash-errore")
	if got := colonna(t, d, id, "stato"); got != "in_lavorazione" {
		t.Errorf("stato %s", got)
	}
}

// inventario: RIP-S-14
// Una scheda compro oro annullata si riapre solo nei suoi stati; una riapertura che il database
// rifiuta (qui l'audit) torna alla scheda con «Riapertura non riuscita.» e la busta resta chiusa.
func TestPaginaRiaperturaCompraOroERifiuto(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	oro := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.CompraOro = "1" })
	b := titolareDentro(t, h)
	cambia(t, b, oro, "annullata")
	r := b.vai("GET", percorsoScheda(oro), nil, nil)
	contiene(t, "compro oro", r.corpo, `<option value="">Seleziona</option>`+"\n"+`<option value="in_valutazione">In valutazione</option>`+"\n"+
		`<option value="valutata">Valutata</option>`+"\n"+`<option value="perfezionata">Perfezionata</option>`+"\n"+`</select>`)
	eseguiSQL(t, d, `CREATE TEMP TRIGGER no_riapri BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.reopened' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if r := b.vai("POST", azioneRiapri(oro), formRiapri(b, "valutata", "Cliente tornato"), nil); !redirectA(r, percorsoScheda(oro)) {
		t.Fatalf("rifiutata: %d %q", r.codice, r.location)
	}
	contiene(t, "rifiutata", b.vai("GET", percorsoScheda(oro), nil, nil).corpo,
		`<div role="alert" class="flash flash-errore">Riapertura non riuscita.</div>`, `<dd id="repair-status" class="forte">Annullata</dd>`)
}

// inventario: RIP-S-14
// La conferma «Riaprire questa busta chiusa?…» la chiede app.js prima del clic (data-confirm,
// come phoenix_html dello standalone): qui si controlla che lo script ci sia, non il browser.
func TestPaginaRiaperturaConferma(t *testing.T) {
	b, _ := dentroCome(t, accounts.Dipendente, nil)
	js := assetDellaPagina(t, b, b.vai("GET", "/", nil, nil).corpo, "js", "text/javascript; charset=utf-8")
	for _, s := range []string{`closest("[data-confirm]")`, `window.confirm(el.getAttribute("data-confirm"))`, "event.preventDefault()"} {
		if !strings.Contains(js, s) {
			t.Errorf("app.js non contiene %q", s)
		}
	}
}

// inventario: RIP-S-15
// «⏳ Metti in attesa risposta cliente» apre un modulo con il motivo facoltativo; la busta in attesa
// mostra il riquadro con «dal» (i primi dieci caratteri), il motivo fra «» e scappato, e «✓
// Risposta arrivata», che la toglie. Un'azione sconosciuta dice «Azione non valida.». Lo stato non
// cambia.
func TestPaginaAttesa(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := dipendenteDentro(t, h)
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	sid := strconv.FormatInt(id, 10)
	contiene(t, "modulo", r.corpo, `<details id="repair-waiting-control" class="attesa">`+"\n"+`<summary>⏳ Metti in attesa risposta cliente</summary>`,
		`<form id="repair-waiting-form" action="/repairs/`+sid+`/attesa" method="post" class="modulo-in-riga" data-invio-unico>`,
		`<input type="hidden" name="waiting[azione]" value="on">`,
		`<label for="waiting_motivo">`+"\n"+`<span class="label">Motivo</span>`+"\n"+`<input type="text" name="waiting[motivo]" id="waiting_motivo" value="" placeholder="Motivo (es. quale colore pietra?)">`,
		`<button type="submit">⏳ Metti in attesa</button>`)
	nonContiene(t, "modulo", r.corpo, `id="repair-waiting"`)
	if strings.Index(r.corpo, `id="repair-waiting-control"`) > strings.Index(r.corpo, `id="edit-repair"`) {
		t.Error("l'attesa viene dopo «Modifica»")
	}

	if r := b.vai("POST", azioneAttesa(id), formAttesa(b, "on", "  <b>Pietra?</b>  "), nil); !redirectA(r, percorsoScheda(id)) {
		t.Fatalf("attesa: %d %q", r.codice, r.location)
	}
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "in attesa", r.corpo, `<div role="status" class="flash flash-info">Busta messa in attesa risposta cliente.</div>`,
		`<div id="repair-waiting" class="riquadro-attesa">`+"\n"+`<strong>⏳ In attesa risposta cliente</strong>`+"\n"+`<span id="repair-waiting-since" class="attesa-dal">dal 2026-10-31</span>`+"\n"+
			`<p id="repair-waiting-reason" class="motivo-attesa">«&lt;b&gt;Pietra?&lt;/b&gt;»</p>`,
		`<form id="repair-waiting-reply-form" action="/repairs/`+sid+`/attesa" method="post" class="in-linea" data-invio-unico>`,
		`<input type="hidden" name="waiting[azione]" value="off">`, `<button type="submit" class="pulsante-secondario">✓ Risposta arrivata</button>`,
		`<dd id="repair-status" class="forte">In lavorazione</dd>`)
	nonContiene(t, "in attesa", r.corpo, `id="repair-waiting-control"`, "<b>Pietra?</b>")
	if got := colonna(t, d, id, "attesa_motivo"); got != "<b>Pietra?</b>" {
		t.Errorf("motivo %q", got)
	}

	// Un «dal» importato si mostra per i primi dieci caratteri; senza «dal» né motivo solo il titolo.
	eseguiSQL(t, d, `UPDATE buste SET attesa_dal = '2026-01-02T03:04:05', attesa_motivo = '' WHERE id = ?`, id)
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "importata", r.corpo, `<span id="repair-waiting-since" class="attesa-dal">dal 2026-01-02</span>`)
	nonContiene(t, "importata", r.corpo, `id="repair-waiting-reason"`)
	eseguiSQL(t, d, `UPDATE buste SET attesa_dal = NULL WHERE id = ?`, id)
	nonContiene(t, "senza dal", b.vai("GET", percorsoScheda(id), nil, nil).corpo, `id="repair-waiting-since"`)

	for _, a := range []string{"toggle", ""} {
		if r := b.vai("POST", azioneAttesa(id), formAttesa(b, a, ""), nil); !redirectA(r, percorsoScheda(id)) {
			t.Fatalf("%q: %d %q", a, r.codice, r.location)
		}
		contiene(t, a, b.vai("GET", percorsoScheda(id), nil, nil).corpo, `<div role="alert" class="flash flash-errore">Azione non valida.</div>`)
	}

	if r := b.vai("POST", azioneAttesa(id), url.Values{"_csrf_token": {b.token}, "waiting[azione]": {"off"}}, nil); !redirectA(r, percorsoScheda(id)) {
		t.Fatalf("risposta arrivata: %d %q", r.codice, r.location)
	}
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "tolta", r.corpo, `<div role="status" class="flash flash-info">Attesa rimossa.</div>`, `id="repair-waiting-control"`)
	nonContiene(t, "tolta", r.corpo, `id="repair-waiting"`)
	if got := colonna(t, d, id, "attesa_cliente") + "|" + colonna(t, d, id, "attesa_dal") + "|" + colonna(t, d, id, "attesa_motivo"); got != "0|∅|∅" {
		t.Errorf("attesa %s", got)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione IN ('repair.waiting_started', 'repair.waiting_ended') AND attore = 'banco@esempio.it' AND dati = '{}'`); n != 2 {
		t.Errorf("audit %d", n)
	}
}

// inventario: RIP-S-15
// Un'attesa che il database rifiuta (qui l'audit) torna alla scheda con «Azione non valida.»;
// l'attesa resta com'era. Anche una busta chiusa si mette in attesa.
func TestPaginaAttesaRifiutata(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	cambia(t, b, id, "annullata")
	eseguiSQL(t, d, `CREATE TEMP TRIGGER no_attesa BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.waiting_started' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if r := b.vai("POST", azioneAttesa(id), formAttesa(b, "on", "Motivo"), nil); !redirectA(r, percorsoScheda(id)) {
		t.Fatalf("%d %q", r.codice, r.location)
	}
	contiene(t, "rifiutata", b.vai("GET", percorsoScheda(id), nil, nil).corpo, `<div role="alert" class="flash flash-errore">Azione non valida.</div>`)
	if got := colonna(t, d, id, "attesa_cliente"); got != "0" {
		t.Errorf("attesa %s", got)
	}
	eseguiSQL(t, d, `DROP TRIGGER no_attesa`)
	if r := b.vai("POST", azioneAttesa(id), formAttesa(b, "on", "Motivo"), nil); !redirectA(r, percorsoScheda(id)) {
		t.Fatalf("%d %q", r.codice, r.location)
	}
	contiene(t, "chiusa", b.vai("GET", percorsoScheda(id), nil, nil).corpo, `id="repair-waiting"`, `id="repair-reopen-form"`)
}

// inventario: RIP-S-14
// Come used_input?/1 dello standalone (core_components.ex: gli errori di un campo solo se il campo
// è arrivato): una riapertura senza il motivo ridisegna la scheda senza errore sotto il motivo e
// con il campo senza value; una senza lo stato non segna errori né voci sotto il menu. Niente cambia.
func TestPaginaRiaperturaCampiMancanti(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := dipendenteDentro(t, h)
	cambia(t, b, id, "annullata")
	r := b.vai("POST", azioneRiapri(id), url.Values{"_csrf_token": {b.token}, "repair[status]": {"pronta"}}, nil)
	if r.codice != 200 {
		t.Fatalf("senza motivo: %d %q", r.codice, r.location)
	}
	contiene(t, "senza motivo", r.corpo, `<option value="pronta" selected>Pronta</option>`,
		`<input type="text" name="repair[reopen_reason]" id="repair_reopen_reason" required>`+"\n"+`</label>`+"\n"+`</div>`)
	nonContiene(t, "senza motivo", r.corpo, "field-error", "flash-errore")
	r = b.vai("POST", azioneRiapri(id), url.Values{"_csrf_token": {b.token}, "repair[reopen_reason]": {""}}, nil)
	if r.codice != 200 {
		t.Fatalf("senza stato: %d %q", r.codice, r.location)
	}
	contiene(t, "senza stato", r.corpo, `id="repair_reopen_reason" value="" required>`+"\n"+`</label>`+"\n"+`<p class="field-error">Motivo obbligatorio.</p>`,
		`</select>`+"\n"+`</label>`+"\n"+`</div>`)
	nonContiene(t, "senza stato", r.corpo, " selected>", "non riapre la busta")
	if got := colonna(t, d, id, "stato"); got != "annullata" {
		t.Errorf("stato %s", got)
	}
}
