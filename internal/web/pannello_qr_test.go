package web

import (
	"bytes"
	"net/url"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/db"
)

// Il pannello delle azioni dopo la scansione (pannello_qr.go, slice 054): i pulsanti per stato, la
// conferma (dal server senza JavaScript, D-70), l'avanzamento, «💰 Preventivo», il rientro
// dall'artigiano, il compro oro, i rifiuti, il CSRF, i filtri che restano e il codice che non va mai
// nell'indirizzo né nel registro.

// azioneQR manda il modulo del pannello della busta id dopo la lista: il token, lo stato mostrato e i
// campi in più.
func azioneQR(b *browser, id int64, azione, stato string, altri ...string) risposta {
	b.t.Helper()
	b.vai("GET", "/", nil, nil) // il token della sessione, come scansione
	v := url.Values{"_csrf_token": {b.token}, "stato_visto": {stato}}
	for i := 0; i+1 < len(altri); i += 2 {
		v.Add(altri[i], altri[i+1])
	}
	return b.vai("POST", "/repairs/"+idTesto(id)+"/"+azione, v, nil)
}

// statoBusta è lo stato della busta id, con le date di pronta e consegna e il rientro.
func statoBusta(t *testing.T, d *db.DB, id int64) string {
	t.Helper()
	var s string
	if err := d.Lettura.QueryRow(`SELECT stato || '|' || coalesce(data_pronta, '-') || '|' || coalesce(data_consegna, '-') || '|' ||
		coalesce(rientrata_artigiano_il, '-') FROM buste WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// pannelloDellaPagina è il riquadro #qr-action della pagina, fino alla sua chiusura.
func pannelloDellaPagina(t *testing.T, corpo string) string {
	t.Helper()
	i := strings.Index(corpo, `<div id="qr-action"`)
	if i < 0 {
		t.Fatalf("manca il pannello")
	}
	return corpo[i : i+strings.Index(corpo[i:], "</section>")]
}

// inventario: RIP-H-23, RIP-H-25, RIP-H-26
// I pulsanti del pannello per ogni stato, come @qr_actions e qr_select/2: il testo, la conferma
// nascosta per app.js (data-qr-chiedi), «💰 Preventivo» solo per in lavorazione e in attesa
// materiali, l'avviso e solo «👁 Consultazione» per le chiuse; il modulo ha il token, lo stato
// mostrato e i filtri, mai il codice letto.
func TestPannelloQRPerStato(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	casi := []struct {
		qr, colonne, avanza, conferma string
		preventivo                    bool
		avviso                        string
	}{
		{"QR-LAV", ``, "✅ Pronta", "", true, ""},
		{"QR-MAT", `stato = 'in_attesa_materiali'`, "✅ Pronta", "", true, ""},
		{"QR-PREV", `stato = 'preventivo'`, "▶ In lavorazione", "", false, ""},
		{"QR-PRONTA", `stato = 'pronta'`, "✅ Consegnata", "Marchio come consegnata? Verrà impostata data oggi.", false, ""},
		{"QR-CONS", `stato = 'consegnata'`, "", "", false, "Stato consegnata — non avanzabile."},
		{"QR-ANN", `stato = 'annullata'`, "", "", false, "Stato annullata — non avanzabile."},
		{"QR-VAL", `stato = 'in_valutazione', compro_oro = 1`, "✅ Valutata", "Conferma valutazione completata? Verrà aperto WA &#34;scheda pronta firma&#34;.", false, ""},
		{"QR-VALUTATA", `stato = 'valutata', compro_oro = 1`, "✅ Perfezionata", "Confermi firma scheda? Movimento di cassa registrato.", false, ""},
		{"QR-PERF", `stato = 'perfezionata', compro_oro = 1`, "✅ Chiusa", "", false, ""},
		{"QR-CHIUSA", `stato = 'chiusa', compro_oro = 1`, "", "", false, "Stato chiusa — non avanzabile."},
		{"QR-FUORI", `consegnata_artigiano_il = '2026-10-01T09:00:00.000000Z'`, "✅ Pronta",
			"La busta risulta fuori bottega: viene segnata anche rientrata dall&#39;artigiano.", true, ""},
		{"QR-FUORI-PRONTA", `stato = 'pronta', consegnata_artigiano_il = '2026-10-01T09:00:00.000000Z'`, "✅ Consegnata",
			"Marchio come consegnata? Verrà impostata data oggi. La busta risulta fuori bottega: viene segnata anche rientrata dall&#39;artigiano.", false, ""},
	}
	for k, b := range []*browser{titolareDentro(t, h), dipendenteDentro(t, h)} {
		for i, c := range casi {
			id := bustaHome(t, d, o, idTesto(int64(60+i+k*len(casi)))+"/26", c.qr, c.colonne)
			p := pannelloDellaPagina(t, scansione(b, c.qr, "stato", "", "scadenza", "1").corpo)
			nonContiene(t, c.qr, p, c.qr, `name="cerca"`)
			if c.avanza == "" {
				contiene(t, c.qr, p, `<p id="qr-action-notice" class="gs-qr-avviso">ℹ `+c.avviso+`</p>
<div class="gs-qr-pulsanti">
<a id="qr-btn-consulta" href="/repairs/`+idTesto(id)+`" class="pulsante-contorno">👁 Consultazione</a>
</div>`)
				nonContiene(t, c.qr, p, "<form", "qr-btn-avanza", "qr-btn-prev")
				continue
			}
			contiene(t, c.qr, p, `<form id="qr-action-form" method="post" action="/repairs/`+idTesto(id)+`/qr-avanza">
<input name="_csrf_token" type="hidden" value="`+b.token+`">
<input name="stato_visto" type="hidden" value="`)
			contiene(t, c.qr, p, `<input type="hidden" name="scadenza" value="1">`, `<a id="qr-btn-consulta" href="/repairs/`+idTesto(id)+`" class="pulsante-contorno">👁 Consultazione</a>`)
			nonContiene(t, c.qr, p, "qr-action-notice")
			if c.conferma == "" {
				contiene(t, c.qr, p, `<button id="qr-btn-avanza" type="submit" class="pulsante">`+c.avanza+`</button>`)
				nonContiene(t, c.qr, p, "qr-action-confirm", "qr-btn-si", "data-qr-chiedi")
			} else {
				contiene(t, c.qr, p, `<p id="qr-action-confirm" class="gs-qr-conferma" hidden>`+c.conferma+`</p>`,
					`<button id="qr-btn-avanza" type="submit" data-qr-chiedi class="pulsante">`+c.avanza+`</button>`,
					`<div class="gs-qr-pulsanti" data-qr-conferma hidden>
<button id="qr-btn-si" type="submit" name="conferma" value="1" class="pulsante">Sì</button>
<button id="qr-btn-no" type="submit" name="annulla" value="1" class="pulsante-contorno">Annulla</button>
</div>`)
			}
			if c.preventivo {
				contiene(t, c.qr, p, `<button id="qr-btn-prev" type="submit" formaction="/repairs/`+idTesto(id)+`/qr-preventivo" class="pulsante gs-qr-preventivo">💰 Preventivo</button>`)
			} else {
				nonContiene(t, c.qr, p, "qr-btn-prev")
			}
		}
		// Il secondo giro (il dipendente) con buste nuove: le stesse etichette darebbero gemelle.
		for i := range casi {
			casi[i].qr += "-2"
		}
	}
}

// inventario: RIP-H-23
// Senza JavaScript (D-70): «✅ Consegnata» senza conferma=1 non scrive e risponde con il pannello in
// conferma (la frase, «Sì» e «Annulla», senza gli altri pulsanti, no-store); «Annulla» ridisegna i
// pulsanti; «Sì» consegna, dice «✓ consegnata» e torna alla lista con i filtri del modulo. L'audit è
// quello della scheda con «via»: «qr»; il codice non va nell'indirizzo, nel registro né nelle righe
// del server.
func TestPannelloQRConferma(t *testing.T) {
	var log bytes.Buffer
	h, d, o := facciaConBusteE(t, &log, indirizzoPubblicoProva)
	id := bustaHome(t, d, o, "32/26", "RIP-2026-0053", `stato = 'pronta', data_pronta = '2026-10-30'`)
	b := dipendenteDentro(t, h)
	scansione(b, "RIP-2026-0053", "stato", "pronta", "compro_oro", "")
	prima := righeAudit(t, d, "repair.status_changed")
	r := azioneQR(b, id, "qr-avanza", "pronta", "stato", "pronta", "scadenza", "1")
	if r.codice != 200 || r.h.Get("Cache-Control") != "no-store" || statoBusta(t, d, id) != "pronta|2026-10-30|-|-" {
		t.Fatalf("senza conferma: %d %q %s", r.codice, r.h.Get("Cache-Control"), statoBusta(t, d, id))
	}
	p := pannelloDellaPagina(t, r.corpo)
	contiene(t, "conferma", p, `<p class="gs-qr-titolo">#0053/26 — Cliente 32/26</p>`,
		`<input name="stato_visto" type="hidden" value="pronta">
<input type="hidden" name="stato" value="pronta">
<input type="hidden" name="scadenza" value="1">
<p id="qr-action-confirm" class="gs-qr-conferma">Marchio come consegnata? Verrà impostata data oggi.</p>
<div class="gs-qr-pulsanti" data-qr-conferma>
<button id="qr-btn-si" type="submit" name="conferma" value="1" class="pulsante">Sì</button>
<button id="qr-btn-no" type="submit" name="annulla" value="1" class="pulsante-contorno">Annulla</button>
</div>
</form>`)
	nonContiene(t, "conferma", p, "qr-btn-avanza", "qr-btn-consulta", "data-qr-scelte", "hidden>")
	// La conferma disegnata dal server non suona (RIP-H-27): niente è ancora successo.
	nonContiene(t, "conferma", r.corpo, "data-qr-esito")
	contiene(t, "conferma", r.corpo, `<a id="qr-lookup-close" href="/?stato=pronta&amp;scadenza=1" class="pulsante-contorno">Chiudi</a>`)
	// «Annulla» dal server: i pulsanti di nuovo, niente scritto.
	r = azioneQR(b, id, "qr-avanza", "pronta", "annulla", "1")
	contiene(t, "annulla", pannelloDellaPagina(t, r.corpo), `<button id="qr-btn-avanza" type="submit" data-qr-chiedi class="pulsante">✅ Consegnata</button>`,
		`<p id="qr-action-confirm" class="gs-qr-conferma" hidden>`)
	// Un valore diverso da «1» non conferma.
	r = azioneQR(b, id, "qr-avanza", "pronta", "conferma", "si")
	contiene(t, "conferma si", r.corpo, `<p id="qr-action-confirm" class="gs-qr-conferma">`)
	if statoBusta(t, d, id) != "pronta|2026-10-30|-|-" || righeAudit(t, d, "repair.status_changed") != prima {
		t.Fatalf("la conferma ha scritto: %s", statoBusta(t, d, id))
	}
	// «Sì».
	r = azioneQR(b, id, "qr-avanza", "pronta", "conferma", "1", "stato", "pronta", "scadenza", "1")
	if r.codice != 302 || r.location != "/?stato=pronta&scadenza=1" {
		t.Fatalf("sì: %d %q", r.codice, r.location)
	}
	if got := statoBusta(t, d, id); got != "consegnata|2026-10-30|2026-10-31|-" {
		t.Errorf("dopo sì: %s", got)
	}
	// Il secondo «Sì» (doppio tocco, review M1): la stessa risposta, niente scritto.
	audit := righeAudit(t, d, "repair.status_changed")
	r = azioneQR(b, id, "qr-avanza", "pronta", "conferma", "1", "stato", "pronta", "scadenza", "1")
	if r.codice != 302 || r.location != "/?stato=pronta&scadenza=1" || righeAudit(t, d, "repair.status_changed") != audit {
		t.Fatalf("secondo sì: %d %q", r.codice, r.location)
	}
	l := b.vai("GET", r.location, nil, nil)
	contiene(t, "lista", l.corpo, `<div role="status" class="flash flash-info">✓ consegnata</div>`)
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed' AND dati = '{"status":"consegnata","via":"qr"}'`); n != 1 {
		t.Errorf("audit: %d", n)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE dati LIKE '%RIP-2026%' OR id_risorsa LIKE '%RIP-2026%'`); n != 0 {
		t.Errorf("il codice nel registro: %d righe", n)
	}
	if strings.Contains(log.String(), "RIP-2026") {
		t.Errorf("il codice nelle righe del server: %s", log.String())
	}
	// Dopo: la busta è chiusa, il pannello dice solo l'avviso.
	contiene(t, "chiusa", scansione(b, "RIP-2026-0053").corpo, `ℹ Stato consegnata — non avanzabile.`)
}

// inventario: RIP-H-23
// «✅ Pronta» porta alla scheda con il modulo sul prezzo (?edit=prezzo), «▶ In lavorazione» e
// «✅ Chiusa» dicono «✓ <stato>» e tornano alla lista (senza filtri: «/»); la busta cambiata nel
// frattempo dice «Stato cambiato nel frattempo: ricontrolla.» con il pannello com'è adesso, senza
// scrivere; una chiusa nel frattempo il rifiuto e solo «👁 Consultazione»; una busta che non c'è o
// sta nel cestino «Nessuna riparazione trovata», senza conferma.
func TestPannelloQRAvanza(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	lav := bustaHome(t, d, o, "29/26", "QR-29", ``)
	prev := bustaHome(t, d, o, "30/26", "QR-30", `stato = 'preventivo'`)
	perf := bustaHome(t, d, o, "31/26", "QR-31", `stato = 'perfezionata', compro_oro = 1`)
	b := titolareDentro(t, h)
	r := azioneQR(b, lav, "qr-avanza", "in_lavorazione")
	if r.codice != 302 || r.location != "/repairs/"+idTesto(lav)+"?edit=prezzo" || !strings.HasPrefix(statoBusta(t, d, lav), "pronta|2026-10-31|") {
		t.Fatalf("pronta: %d %q %s", r.codice, r.location, statoBusta(t, d, lav))
	}
	contiene(t, "scheda", b.vai("GET", r.location, nil, nil).corpo, `id="repair-form"`)
	for _, c := range []struct {
		id           int64
		visto, flash string
	}{{prev, "preventivo", "✓ in lavorazione"}, {perf, "perfezionata", "✓ chiusa"}} {
		r = azioneQR(b, c.id, "qr-avanza", c.visto)
		if r.codice != 302 || r.location != "/" {
			t.Fatalf("%s: %d %q", c.visto, r.codice, r.location)
		}
		contiene(t, c.visto, b.vai("GET", "/", nil, nil).corpo, `<div role="status" class="flash flash-info">`+c.flash+`</div>`)
	}
	// Il secondo invio di «✅ Pronta» (doppio tocco, review M1): lav è già pronta, si va dove sarebbe
	// andato il primo, senza scrivere niente.
	audit := righeAudit(t, d, "repair.status_changed")
	r = azioneQR(b, lav, "qr-avanza", "in_lavorazione")
	if r.codice != 302 || r.location != "/repairs/"+idTesto(lav)+"?edit=prezzo" || righeAudit(t, d, "repair.status_changed") != audit {
		t.Fatalf("secondo invio: %d %q", r.codice, r.location)
	}
	// La busta cambiata nel frattempo (lav è pronta, il pannello mostrava preventivo), senza conferma
	// (review B2: la conferma si chiede solo se la busta è ancora come la mostrava il pannello):
	// niente scritto, «Stato cambiato» e il pannello vero.
	prima := statoBusta(t, d, lav)
	r = azioneQR(b, lav, "qr-avanza", "preventivo")
	if r.codice != 200 || statoBusta(t, d, lav) != prima || righeAudit(t, d, "repair.status_changed") != audit {
		t.Fatalf("cambiata: %d %s", r.codice, statoBusta(t, d, lav))
	}
	contiene(t, "cambiata", r.corpo, `<div role="alert" class="flash flash-errore">Stato cambiato nel frattempo: ricontrolla.</div>`,
		`<section id="qr-lookup" class="gs-qr" data-qr-esito="ko">`,
		`<button id="qr-btn-avanza" type="submit" data-qr-chiedi class="pulsante">✅ Consegnata</button>`, `<input name="stato_visto" type="hidden" value="pronta">`)
	nonContiene(t, "cambiata", r.corpo, `<p id="qr-action-confirm" class="gs-qr-conferma">`)
	// Dal pannello di perfezionata, perf già chiusa è il secondo invio di «✅ Chiusa»: riuscito.
	r = azioneQR(b, perf, "qr-avanza", "perfezionata")
	if r.codice != 302 || r.location != "/" {
		t.Fatalf("secondo invio di «✅ Chiusa»: %d %q", r.codice, r.location)
	}
	contiene(t, "secondo invio", b.vai("GET", "/", nil, nil).corpo, `<div role="status" class="flash flash-info">✓ chiusa</div>`)
	// Chiusa nel frattempo (perf è chiusa, il pannello mostrava valutata): il rifiuto della
	// sorgente, solo «👁 Consultazione».
	r = azioneQR(b, perf, "qr-avanza", "valutata", "conferma", "1")
	p := pannelloDellaPagina(t, r.corpo)
	contiene(t, "chiusa", p, `<p id="qr-action-notice" class="gs-qr-avviso">ℹ Stato &#34;chiusa&#34; non avanzabile</p>`, `id="qr-btn-consulta"`)
	contiene(t, "chiusa", r.corpo, `data-qr-esito="ko"`)
	nonContiene(t, "chiusa", p, "qr-btn-avanza")
	// Nel cestino: nemmeno la conferma.
	cestino := bustaHome(t, d, o, "32/26", "QR-32", `stato = 'pronta', cancellata_il = ?`, istanteHome)
	for _, x := range []string{"999", "abc", "0", idTesto(cestino)} {
		r = b.vai("POST", "/repairs/"+x+"/qr-avanza", url.Values{"_csrf_token": {b.token}, "stato_visto": {"pronta"}}, nil)
		if r.codice != 200 {
			t.Fatalf("%s: %d", x, r.codice)
		}
		contiene(t, x, r.corpo, `<p id="qr-lookup-error" class="gs-qr-errore">⚠️ Nessuna riparazione trovata</p>`, `data-qr-esito="ko"`)
		nonContiene(t, x, r.corpo, `id="qr-action"`)
	}
	if righeAudit(t, d, "repair.qr_lookup") != 0 {
		t.Error("le azioni non sono scansioni")
	}
}

// inventario: RIP-H-25
// «💰 Preventivo»: la busta in preventivo e la scheda con il modulo sul preventivo
// (?edit=preventivo); il compro oro e una busta pronta (moduli forgiati) hanno il rifiuto nel
// pannello, senza scrivere.
func TestPannelloQRPreventivo(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	lav := bustaHome(t, d, o, "29/26", "QR-29", `stato = 'in_attesa_materiali'`)
	oro := bustaHome(t, d, o, "30/26", "QR-30", `stato = 'in_valutazione', compro_oro = 1`)
	pronta := bustaHome(t, d, o, "31/26", "QR-31", `stato = 'pronta'`)
	b := dipendenteDentro(t, h)
	r := azioneQR(b, lav, "qr-preventivo", "in_attesa_materiali")
	if r.codice != 302 || r.location != "/repairs/"+idTesto(lav)+"?edit=preventivo" || !strings.HasPrefix(statoBusta(t, d, lav), "preventivo|") {
		t.Fatalf("preventivo: %d %q %s", r.codice, r.location, statoBusta(t, d, lav))
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed' AND dati = '{"status":"preventivo","via":"qr"}'`); n != 1 {
		t.Errorf("audit: %d", n)
	}
	for _, c := range []struct {
		id      int64
		rifiuto string
	}{{oro, "QR preventivo non supportato per buste compro oro"}, {pronta, "Transizione non consentita."}} {
		r = azioneQR(b, c.id, "qr-preventivo", "")
		contiene(t, c.rifiuto, pannelloDellaPagina(t, r.corpo), `ℹ `+c.rifiuto+`</p>`)
		contiene(t, c.rifiuto, r.corpo, `data-qr-esito="ko"`)
	}
	if !strings.HasPrefix(statoBusta(t, d, oro), "in_valutazione|") || !strings.HasPrefix(statoBusta(t, d, pronta), "pronta|") {
		t.Error("il rifiuto ha scritto")
	}
}

// inventario: RIP-H-24
// Il rientro automatico dalla pagina: una busta ancora dall'artigiano chiede sempre la conferma (anche
// «✅ Pronta», che di suo non la chiede), con la frase del rientro; confermata va pronta, rientra e
// porta alla scheda sul prezzo.
func TestPannelloQRRientro(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (5, 'Artigiano del pannello')`)
	eseguiSQL(t, d, `INSERT INTO consegne_artigiani (id, artigiano_id, creato_il, aggiornato_il) VALUES (3, 5, ?1, ?1)`, istanteHome)
	id := bustaHome(t, d, o, "4/26", "QR-FUORI", `consegna_artigiano_id = 3, consegnata_artigiano_il = ?`, istanteHome)
	b := titolareDentro(t, h)
	r := azioneQR(b, id, "qr-avanza", "in_lavorazione")
	if r.codice != 200 || statoBusta(t, d, id) != "in_lavorazione|-|-|-" {
		t.Fatalf("senza conferma: %d %s", r.codice, statoBusta(t, d, id))
	}
	contiene(t, "conferma", r.corpo, `<p id="qr-action-confirm" class="gs-qr-conferma">La busta risulta fuori bottega: viene segnata anche rientrata dall&#39;artigiano.</p>`)
	r = azioneQR(b, id, "qr-avanza", "in_lavorazione", "conferma", "1")
	if r.codice != 302 || r.location != "/repairs/"+idTesto(id)+"?edit=prezzo" {
		t.Fatalf("sì: %d %q", r.codice, r.location)
	}
	if got := statoBusta(t, d, id); !strings.HasPrefix(got, "pronta|2026-10-31|-|2026-10-31T") {
		t.Errorf("rientro: %s", got)
	}
	if righeAudit(t, d, "repair.returned_from_artisan_auto") != 1 || contaRighe(t, d, `SELECT count(*) FROM consegne_artigiani WHERE id = 3 AND chiusa_il IS NOT NULL`) != 1 {
		t.Error("riga del rientro o consegna chiusa")
	}
}

// inventario: RIP-H-23, RIP-H-25
// Il CSRF: senza token le due azioni danno 403 e non scrivono.
func TestPannelloQRCSRF(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaHome(t, d, o, "29/26", "QR-29", ``)
	b := titolareDentro(t, h)
	for _, azione := range []string{"qr-avanza", "qr-preventivo"} {
		for _, token := range []string{"", "sbagliato"} {
			v := url.Values{"stato_visto": {"in_lavorazione"}, "conferma": {"1"}}
			if token != "" {
				v.Set("_csrf_token", token)
			}
			if r := b.vai("POST", "/repairs/"+idTesto(id)+"/"+azione, v, nil); r.codice != 403 {
				t.Errorf("%s %q: %d", azione, token, r.codice)
			}
		}
	}
	if statoBusta(t, d, id) != "in_lavorazione|-|-|-" || righeAudit(t, d, "repair.status_changed") != 0 {
		t.Error("senza token ha scritto")
	}
}

// inventario: RIP-H-23
// D-70: app.js apre la conferma del pannello senza passare dal server (data-qr-chiedi al posto dei
// pulsanti, «Annulla» torna indietro), e solo dove ci sono tutti e due i blocchi: sulla conferma
// disegnata dal server «Annulla» va al server; un secondo invio dello stesso modulo si ferma
// (data-qr-inviato), senza spegnere il pulsante premuto. Qui si controlla solo che le righe ci
// siano: il comportamento JavaScript lo prova solo il banco, con lo scenario browser pannello-qr.
func TestScriptDelPannelloQR(t *testing.T) {
	b, _ := dentroCome(t, accounts.Titolare, nil)
	js := assetDellaPagina(t, b, b.vai("GET", "/", nil, nil).corpo, "js", "text/javascript; charset=utf-8")
	for _, s := range []string{
		`event.target.closest("#qr-btn-avanza[data-qr-chiedi], #qr-btn-no")`,
		"  if (!scelte || !conferma) return\n  event.preventDefault()\n  const chiedi = button.id === \"qr-btn-avanza\"",
		"  if (frase) frase.hidden = !chiedi\n  conferma.hidden = !chiedi\n  scelte.hidden = chiedi",
		"  const form = event.target.closest && event.target.closest(\"#qr-action-form\")\n  if (!form) return\n  if (form.dataset.qrInviato) { event.preventDefault(); return }\n  form.dataset.qrInviato = \"1\"",
	} {
		if !strings.Contains(js, s) {
			t.Errorf("app.js non contiene %q", s)
		}
	}
}
