package web

import (
	"bytes"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Il campo unico della home (cerca.go, D-63 e D-64, slice 052): la scansione con POST /cerca-qr, la
// ricerca scritta con il suo smistamento (anche da GET /cerca), i filtri che restano o che la
// ricerca per etichetta toglie, le righe del registro.

// busteRicerca semina le buste delle prove della ricerca, come la semina del banco: le gemelle di
// un'etichetta (una consegnata), due gemelle aperte, l'etichetta dell'anno prima, un codice da solo,
// due gemelle chiuse, una busta 12/26 senza etichetta e una del 2025 con l'etichetta 0012.
type busteRicerca struct {
	consegnata, aperta, gemellaA, gemellaB, vecchia, sola, chiusaA, chiusaB, dodici, dodici25 int64
}

func seminaRicerca(t *testing.T, d *db.DB, o clock.Orologio) busteRicerca {
	t.Helper()
	var s busteRicerca
	s.consegnata = bustaHome(t, d, o, "21/26", "RIP-2026-0045", `stato = 'consegnata', descrizione = 'Collana'`)
	s.aperta = bustaHome(t, d, o, "22/26", "RIP-2026-0045", `descrizione = 'Anello unico da cercare'`)
	s.gemellaA = bustaHome(t, d, o, "23/26", "RIP-2026-0046", ``)
	s.gemellaB = bustaHome(t, d, o, "24/26", "RIP-2026-0046", `stato = 'in_valutazione', compro_oro = 1`)
	s.vecchia = bustaHome(t, d, o, "25/26", "RIP-2025-0045", `stato = 'pronta'`)
	s.sola = bustaHome(t, d, o, "26/26", "QR-SOLO", ``)
	s.chiusaA = bustaHome(t, d, o, "27/26", "QR-CHIUSE", `stato = 'consegnata'`)
	s.chiusaB = bustaHome(t, d, o, "28/26", "QR-CHIUSE", `stato = 'chiusa', compro_oro = 1`)
	s.dodici = bustaHome(t, d, o, "12/26", "", ``)
	s.dodici25 = bustaHome(t, d, o, "9999/25", "RIP-2025-0012", ``)
	return s
}

func idTesto(id int64) string { return strconv.FormatInt(id, 10) }

// scansione manda il modulo del campo unico com'è nella pagina: il token, cerca, e i campi in più.
func scansione(b *browser, cerca string, altri ...string) risposta {
	b.t.Helper()
	b.vai("GET", "/", nil, nil)
	v := url.Values{"_csrf_token": {b.token}, "cerca": {cerca}}
	for i := 0; i+1 < len(altri); i += 2 {
		v.Add(altri[i], altri[i+1])
	}
	return b.vai("POST", "/cerca-qr", v, nil)
}

// inventario: RIP-H-22
// La scansione (POST /cerca-qr): l'unica busta col codice (spazi ai bordi tolti) apre il pannello
// #qr-action, con il numero D-64, il cliente, lo stato, 🪙 e le azioni (slice 054, provate in
// pannello_qr_test.go); con le gemelle si apre l'unica aperta (D-64), altrimenti la scelta «N riparazioni con
// questo QR — scegli:», dalla più recente, le consegnate attenuate; la scelta di una gemella apre il
// suo pannello. Nessuna riga repair.viewed, la risposta non resta in cache, e c'è il campo unico per
// la prossima ricerca.
func TestCercaQRScansione(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	s := seminaRicerca(t, d, o)
	for _, b := range []*browser{titolareDentro(t, h), dipendenteDentro(t, h)} {
		viste := righeAudit(t, d, "repair.viewed")
		r := scansione(b, " \tQR-SOLO ")
		if r.codice != 200 || r.h.Get("Cache-Control") != "no-store" {
			t.Fatalf("%d %q", r.codice, r.h.Get("Cache-Control"))
		}
		contiene(t, "sola", r.corpo, "<title>Riparazioni · Laboratorio</title>", `<section id="qr-lookup" class="gs-qr" data-qr-esito="ok">`,
			`<h2>📷 Cerca per QR busta</h2>`, `<div class="gs-qr-comandi">
<button id="qr-sound-toggle" type="button" title="Suoni del QR" aria-label="Suoni del QR" class="pulsante-contorno gs-qr-suono" hidden></button>
<a id="qr-lookup-close" href="/" class="pulsante-contorno">Chiudi</a>
</div>`,
			`<form id="repair-search-form" method="post" action="/cerca-qr" class="gs-ricerca">`,
			`<div id="qr-matches" class="gs-qr-esito">
<div id="qr-action" class="gs-qr-azione">
<p class="gs-qr-titolo">N. 26/26 — Cliente 26/26</p>
<p class="gs-qr-stato">Stato: <strong>In lavorazione</strong></p>
<form id="qr-action-form" method="post" action="/repairs/`+idTesto(s.sola)+`/qr-avanza">
<input name="_csrf_token" type="hidden" value="`+b.token+`">
<input name="stato_visto" type="hidden" value="in_lavorazione">
<div class="gs-qr-pulsanti" data-qr-scelte>
<button id="qr-btn-avanza" type="submit" class="pulsante">✅ Pronta</button>
<button id="qr-btn-prev" type="submit" formaction="/repairs/`+idTesto(s.sola)+`/qr-preventivo" class="pulsante gs-qr-preventivo">💰 Preventivo</button>
<a id="qr-btn-consulta" href="/repairs/`+idTesto(s.sola)+`" class="pulsante-contorno">👁 Consultazione</a>
</div>
</form>`)
		nonContiene(t, "sola", r.corpo, "qr-match-list", "qr-lookup-error", "QR-SOLO", "phx-")
		// Le gemelle di un'etichetta riusata: si apre l'unica aperta.
		r = scansione(b, "RIP-2026-0045")
		contiene(t, "riusata", r.corpo, `<p class="gs-qr-titolo">#0045/26 — Cliente 22/26</p>`, `href="/repairs/`+idTesto(s.aperta)+`"`)
		nonContiene(t, "riusata", r.corpo, "qr-match-list", `href="/repairs/`+idTesto(s.consegnata)+`"`)
		// Due gemelle aperte: la scelta, dalla più recente, con 🪙 e lo stato.
		r = scansione(b, "RIP-2026-0046", "via", "qr", "stato", "pronta")
		contiene(t, "gemelle", r.corpo, `<section id="qr-lookup" class="gs-qr" data-qr-esito="ok">`, `<p id="qr-matches-count"><strong>2</strong> riparazioni con questo QR — scegli:</p>`,
			`<button id="qr-match-`+idTesto(s.gemellaB)+`" type="submit" name="scelta" value="`+idTesto(s.gemellaB)+`" class="gs-qr-gemella">
<span class="gs-qr-titolo">#0046/26 — Cliente 24/26</span>
<span class="gs-qr-stato">📅 05/09/2026 · Stato: <strong>In valutazione</strong> <span>· 🪙 Compro Oro</span></span>
<span class="gs-qr-descrizione">Anello
Saldatura</span>`, `<form method="post" action="/cerca-qr">
<input name="_csrf_token" type="hidden" value="`+b.token+`">
<input name="cerca" type="hidden" value="RIP-2026-0046">
<input name="via" type="hidden" value="qr">
<input type="hidden" name="stato" value="pronta">`, `<a id="qr-lookup-close" href="/?stato=pronta" class="pulsante-contorno">Chiudi</a>`)
		elenco := r.corpo[strings.Index(r.corpo, `<div id="qr-match-list"`):]
		if n := strings.Count(elenco, `<input name="_csrf_token" type="hidden" value="`+b.token+`">`); n != 2 {
			t.Errorf("il token in ogni modulo delle gemelle: %d", n)
		}
		if a, c := strings.Index(r.corpo, "qr-match-"+idTesto(s.gemellaB)), strings.Index(r.corpo, "qr-match-"+idTesto(s.gemellaA)); a < 0 || c < a {
			t.Errorf("ordine delle gemelle: %d %d", a, c)
		}
		nonContiene(t, "gemelle", r.corpo, `id="qr-action"`)
		// Il campo unico di questa pagina tiene i filtri, vuoto.
		contiene(t, "gemelle", r.corpo, `<form id="repair-search-form" method="post" action="/cerca-qr" class="gs-ricerca">
<input type="hidden" name="stato" value="pronta">
<div class="gs-ricerca-riga">`, `<input id="repair-search-token" name="_csrf_token" type="hidden" value="`+b.token+`" form="repair-search-form">`, `name="cerca" value="" placeholder="Cerca nome, n° busta o QR…"`)
		// Nessuna aperta: la scelta, le consegnate attenuate (le chiuse no, come lo standalone).
		r = scansione(b, "QR-CHIUSE")
		contiene(t, "chiuse", r.corpo, `<strong>2</strong> riparazioni con questo QR`,
			`id="qr-match-`+idTesto(s.chiusaA)+`" type="submit" name="scelta" value="`+idTesto(s.chiusaA)+`" class="gs-qr-gemella gs-qr-attenuata">`,
			`id="qr-match-`+idTesto(s.chiusaB)+`" type="submit" name="scelta" value="`+idTesto(s.chiusaB)+`" class="gs-qr-gemella">`)
		// La scelta di una gemella (qr_select); un id che non è fra le trovate no.
		r = scansione(b, "RIP-2026-0046", "via", "qr", "scelta", idTesto(s.gemellaA))
		// La scelta di una gemella non suona, come qr_select dello standalone (RIP-H-27).
		contiene(t, "scelta", r.corpo, `<p class="gs-qr-titolo">#0046/26 — Cliente 23/26</p>`, `href="/repairs/`+idTesto(s.gemellaA)+`"`,
			`<section id="qr-lookup" class="gs-qr">`)
		nonContiene(t, "scelta", r.corpo, "qr-match-list", "data-qr-esito")
		for _, x := range []string{idTesto(s.sola), "abc", "0", ""} {
			r = scansione(b, "RIP-2026-0046", "via", "qr", "scelta", x)
			contiene(t, "scelta "+x, r.corpo, `<p id="qr-lookup-error" class="gs-qr-errore">⚠️ Riparazione non trovata.</p>`,
				`<section id="qr-lookup" class="gs-qr" data-qr-esito="ko">`)
			nonContiene(t, "scelta "+x, r.corpo, `id="qr-action"`, "qr-match-list")
		}
		if n := righeAudit(t, d, "repair.viewed"); n != viste {
			t.Errorf("la scansione non apre la scheda: %d righe repair.viewed", n-viste)
		}
	}
}

// inventario: RIP-H-22
// I messaggi: «📷» (via=qr) con un codice sconosciuto dice «Nessuna riparazione trovata per questo
// QR» e scrive la riga con zero buste; vuoto «QR non fornito» senza riga. Lo stesso codice dal
// pulsante «🔍» (o dal lettore a pistola) è una ricerca scritta: la lista vuota, senza riga
// repair.qr_lookup (risposta 1 del 05/11/2026).
func TestCercaQRMessaggi(t *testing.T) {
	var log bytes.Buffer
	h, d, o := facciaConBusteE(t, &log, indirizzoPubblicoProva)
	seminaRicerca(t, d, o)
	b := titolareDentro(t, h)
	r := scansione(b, "RIP-2026-0999", "via", "qr")
	// L'esito per il suono (RIP-H-27) lo decide il server: un codice letto ma sconosciuto è un
	// fallimento, come un codice vuoto e la ricerca non riuscita.
	contiene(t, "sconosciuto", r.corpo, `<p id="qr-lookup-error" class="gs-qr-errore">⚠️ Nessuna riparazione trovata per questo QR</p>`,
		`<section id="qr-lookup" class="gs-qr" data-qr-esito="ko">`)
	nonContiene(t, "sconosciuto", r.corpo, "qr-matches", "RIP-2026-0999")
	if n := righeAudit(t, d, "repair.qr_lookup"); n != 1 {
		t.Errorf("righe: %d", n)
	}
	for _, vuoto := range []string{"", "   ", " "} {
		r = scansione(b, vuoto, "via", "qr")
		contiene(t, "vuoto", r.corpo, `<p id="qr-lookup-error" class="gs-qr-errore">⚠️ QR non fornito</p>`, `data-qr-esito="ko"`)
	}
	if n := righeAudit(t, d, "repair.qr_lookup"); n != 1 {
		t.Errorf("il codice vuoto non scrive: %d", n)
	}
	// Dalla pistola un codice che non è di nessuna busta: la lista vuota disegnata nella risposta al
	// POST (no-store, nessun redirect), con una riga «lista consultata»; il codice non va in nessun
	// link (report, cassetto), solo nel campo e nella pillola (review della 052, punto 1).
	lista := righeAudit(t, d, "repair.index_viewed")
	r = scansione(b, "RIP-2026-0999", "stato", "pronta")
	if r.codice != 200 || r.location != "" || r.h.Get("Cache-Control") != "no-store" {
		t.Fatalf("dalla pistola: %d %q %q", r.codice, r.location, r.h.Get("Cache-Control"))
	}
	contiene(t, "lista", r.corpo, `<div id="repairs-empty" class="gs-lista-vuota">Nessuna riparazione trovata.</div>`,
		`<span class="filter-pill pill-indaco">🔍 RIP-2026-0999</span>`, `<a id="repair-report-pdf" href="/repairs/report.pdf?stato=pronta" target="_blank"`,
		`name="cerca" value="RIP-2026-0999" placeholder="Cerca nome, n° busta o QR…"`, `<input class="gs-campo" type="search" name="cerca" value="" placeholder="Nome cliente o n° busta…">`)
	nonContiene(t, "lista", r.corpo, "data-qr-esito", `id="qr-lookup"`)
	if n := strings.Count(r.corpo, "RIP-2026-0999"); n != 2 {
		t.Errorf("il codice nella pagina %d volte, volute 2 (pillola e campo)", n)
	}
	if n := righeAudit(t, d, "repair.qr_lookup"); n != 1 {
		t.Errorf("una ricerca scritta non scrive repair.qr_lookup: %d", n)
	}
	if n := righeAudit(t, d, "repair.index_viewed"); n != lista+2 { // la GET della pagina e la lista del POST
		t.Errorf("righe della lista: %d", n-lista)
	}
	var dati string
	if err := d.Lettura.QueryRow(`SELECT dati FROM eventi_audit WHERE azione = 'repair.index_viewed' ORDER BY id DESC LIMIT 1`).Scan(&dati); err != nil || dati != `{"filter_keys":["cerca","stato"]}` {
		t.Errorf("chiavi: %s %v", dati, err)
	}
	// Vuoto senza 📷: la lista con i filtri del modulo, o la home.
	if r = scansione(b, "  ", "stato", "pronta", "p", "3"); r.location != "/?stato=pronta" {
		t.Errorf("vuoto con lo stato: %q", r.location)
	}
	if r = scansione(b, ""); r.location != "/" {
		t.Errorf("vuoto: %q", r.location)
	}
	// Senza token il modulo non passa (403 del CSRF).
	if r = b.vai("POST", "/cerca-qr", url.Values{"cerca": {"QR-SOLO"}, "via": {"qr"}}, nil); r.codice != 403 {
		t.Errorf("senza token: %d", r.codice)
	}
	// Se la riga del registro non si scrive: «Ricerca non riuscita.», 500, nessuna busta.
	eseguiSQL(t, d, `CREATE TEMP TRIGGER no_qr BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.qr_lookup' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	r = scansione(b, "QR-SOLO")
	if r.codice != 500 || !strings.Contains(r.corpo, "⚠️ Ricerca non riuscita.") || strings.Contains(r.corpo, "qr-action") ||
		!strings.Contains(r.corpo, `data-qr-esito="ko"`) {
		t.Errorf("senza riga: %d", r.codice)
	}
	// Nemmeno il guasto mette il codice nelle righe del server.
	if !strings.Contains(log.String(), "ricerca per QR non riuscita") || strings.Contains(log.String(), "QR-SOLO") || strings.Contains(log.String(), "RIP-2026") {
		t.Errorf("righe del server:\n%s", log.String())
	}
}

// inventario: RIP-H-22
// Il codice letto non va mai nell'indirizzo né nel registro (righe dell'audit e righe del server):
// solo nel corpo del POST e nella pagina di chi l'ha letto (le scelte fra le gemelle).
func TestCercaQRCodiceMaiFuori(t *testing.T) {
	var log bytes.Buffer
	h, d, o := facciaConBusteE(t, &log, indirizzoPubblicoProva)
	s := seminaRicerca(t, d, o)
	b := titolareDentro(t, h)
	prima := contaRighe(t, d, `SELECT count(*) FROM eventi_audit`)
	for _, x := range [][]string{
		{"QR-SOLO"}, {"RIP-2026-0045"}, {"RIP-2026-0046", "via", "qr"}, {"RIP-2026-0046", "via", "qr", "scelta", idTesto(s.gemellaA)},
		{"QR-CHIUSE"}, {"RIP-2026-0777", "via", "qr"},
	} {
		r := scansione(b, x[0], x[1:]...)
		if r.codice != 200 || r.location != "" {
			t.Errorf("%v: %d %q", x, r.codice, r.location)
		}
	}
	var dati []string
	righe, err := d.Lettura.Query(`SELECT azione || ' ' || risorsa || ' ' || id_risorsa || ' ' || dati FROM eventi_audit WHERE id > ? ORDER BY id`, prima)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = righe.Close() }()
	for righe.Next() {
		var x string
		if err := righe.Scan(&x); err != nil {
			t.Fatal(err)
		}
		dati = append(dati, x)
	}
	audit := strings.Join(dati, "\n")
	for _, codice := range []string{"QR-SOLO", "RIP-", "0045", "0046", "QR-CHIUSE", "0777"} {
		if strings.Contains(audit, codice) {
			t.Errorf("%q nel registro:\n%s", codice, audit)
		}
	}
	for _, codice := range []string{"QR-SOLO", "RIP-20", "QR-CHIUSE"} {
		if strings.Contains(log.String(), codice) {
			t.Errorf("%q nelle righe del server:\n%s", codice, log.String())
		}
	}
	voluto := "repair.qr_lookup repair_index qr {\"matches\":1,\"repair_ids\":[" + idTesto(s.sola) + "]}"
	if !strings.Contains(strings.Join(dati, "\n"), voluto) {
		t.Errorf("righe:\n%s", strings.Join(dati, "\n"))
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.qr_lookup' AND id > `+strconv.Itoa(prima)); n != 6 {
		t.Errorf("una riga per scansione (scelta compresa): %d", n)
	}
}

// inventario: RIP-H-03, RIP-H-21
// La ricerca scritta: una busta trovata apre la scheda (e la sola riga «scheda vista», nessuna
// «lista consultata»: D-68), più buste la lista con la ricerca e i filtri (una riga «lista
// consultata»), nessuna la lista vuota. Dal modulo e da GET /cerca è lo stesso; GET /cerca non è mai
// una scansione e non scrive niente da sé.
func TestCercaSmista(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	s := seminaRicerca(t, d, o)
	b := titolareDentro(t, h)
	lista := func() int { return righeAudit(t, d, "repair.index_viewed") }
	viste := func() int { return righeAudit(t, d, "repair.viewed") }
	b.vai("GET", "/", nil, nil)
	l0, v0 := lista(), viste()
	r := b.vai("POST", "/cerca-qr", url.Values{"_csrf_token": {b.token}, "cerca": {" unico da "}, "stato": {"in_lavorazione"}}, nil)
	if r.codice != 302 || r.location != "/repairs/"+idTesto(s.aperta) {
		t.Fatalf("una busta: %d %q", r.codice, r.location)
	}
	if lista() != l0 || viste() != v0 {
		t.Errorf("lo smistamento non scrive: %d %d", lista()-l0, viste()-v0)
	}
	b.vai("GET", r.location, nil, nil)
	if lista() != l0 || viste() != v0+1 {
		t.Errorf("D-68, solo la scheda vista: lista %d, scheda %d", lista()-l0, viste()-v0)
	}
	// Più buste: la lista con la ricerca e gli altri filtri, nell'ordine di filter_query/3.
	b.vai("GET", "/", nil, nil)
	l0 = lista()
	r = b.vai("POST", "/cerca-qr", url.Values{"_csrf_token": {b.token}, "cerca": {"Cliente 2"}, "stato": {"in_lavorazione"},
		"compro_oro": {""}, "p": {"4"}, "data_campo": {"pronta"}}, nil)
	// Dal POST la lista si disegna nella risposta (no-store, nessun redirect), dalla pagina 1, con la
	// ricerca nei link perché non è un codice.
	if r.codice != 200 || r.location != "" || r.h.Get("Cache-Control") != "no-store" {
		t.Fatalf("più buste: %d %q", r.codice, r.location)
	}
	corpo := r.corpo
	contiene(t, "più buste", corpo, `<a id="repair-report-pdf" href="/repairs/report.pdf?cerca=Cliente&#43;2&amp;stato=in_lavorazione"`)
	if got := ordineRiquadri(corpo); strings.Join(got, ",") != idTesto(s.sola)+","+idTesto(s.gemellaA)+","+idTesto(s.aperta) {
		t.Errorf("lista: %v", got)
	}
	if lista() != l0+1 {
		t.Errorf("una riga «lista consultata»: %d", lista()-l0)
	}
	for q, voluto := range map[string]string{
		"cerca=+unico+da+&stato=in_lavorazione":       "/repairs/" + idTesto(s.aperta),
		"cerca=nessuna-busta&scadenza=1":              "/?cerca=nessuna-busta&scadenza=1",
		"cerca=Cliente+2&stato=consegnata":            "/?cerca=Cliente+2&stato=consegnata",
		"cerca=Cliente+2&stato=pronta":                "/repairs/" + idTesto(s.vecchia),
		"cerca=":                                      "/",
		"stato=pronta":                                "/?stato=pronta",
		"cerca=RIP-2026-0046":                         "/?cerca=RIP-2026-0046", // contiene, mai una scansione
		"cerca=QR-SOLO":                               "/repairs/" + idTesto(s.sola),
		"cerca[]=Collana":                             "/",
		"cerca=Collana":                               "/repairs/" + idTesto(s.consegnata),
		"cerca=Collana&stato=pronta":                  "/?cerca=Collana&stato=pronta",
		"cerca=0012":                                  "/?cerca=0012",
		"cerca=0012/26":                               "/repairs/" + idTesto(s.dodici),
		"cerca=12/25&stato=chiusa&messaggi=1":         "/repairs/" + idTesto(s.dodici25),
		"cerca=0045/25&stato=in_lavorazione&p=2":      "/repairs/" + idTesto(s.vecchia),
		"cerca=0045&stato=pronta&data_da=2026-01-01":  "/?cerca=0045",
		"cerca=0046%2F26&compro_oro=1&senza_qr=1&p=9": "/?cerca=0046%2F26",
		// Il cassetto (GET a /cerca, review della 052): con il testo smista, senza testo («Applica»)
		// torna alla lista con i suoi filtri, come prima.
		"cerca=+unico+da+&stato=&data_campo=accettazione&data_da=&data_a=":              "/repairs/" + idTesto(s.aperta),
		"cerca=&stato=pronta&data_campo=consegna&data_da=2026-01-01&data_a=&senza_qr=1": "/?stato=pronta&senza_qr=1&data_da=2026-01-01&data_campo=consegna",
		"attesa=1&cerca=&stato=&data_campo=accettazione&data_da=&data_a=":               "/?attesa=1",
		"cerca=&stato=&data_campo=accettazione&data_da=&data_a=":                        "/",
	} {
		l0, v0 = lista(), viste()
		r := b.vai("GET", "/cerca?"+q, nil, nil)
		if r.codice != 302 || r.location != voluto {
			t.Errorf("GET /cerca?%s: %d %q, voluto %q", q, r.codice, r.location, voluto)
		}
		if lista() != l0 || viste() != v0 {
			t.Errorf("GET /cerca?%s scrive", q)
		}
	}
	if n := righeAudit(t, d, "repair.qr_lookup"); n != 0 {
		t.Errorf("la ricerca scritta non scrive repair.qr_lookup: %d", n)
	}
}

// inventario: RIP-H-03, RIP-H-06, RIP-H-07
// D-64 nella lista: un numero d'etichetta mostra tutte le buste con quell'etichetta, anche consegnate
// e nascoste dalle impostazioni, qualunque filtro ci sia; le pillole, i campi nascosti, il cassetto e
// il report dicono solo la ricerca. «0012» trova anche la 12/26 senza etichetta.
func TestHomeRicercaEtichetta(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	s := seminaRicerca(t, d, o)
	eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('home_mostra_consegnate', '0', ?), ('home_mostra_pronte', '0', ?)`, istanteHome, istanteHome)
	b := titolareDentro(t, h)
	corpo := b.vai("GET", "/?cerca=0045&stato=in_lavorazione&compro_oro=1&scadenza=1", nil, nil).corpo
	if got, voluti := ordineRiquadri(corpo), []string{idTesto(s.vecchia), idTesto(s.aperta), idTesto(s.consegnata)}; strings.Join(got, ",") != strings.Join(voluti, ",") {
		t.Errorf("0045: %v, voluti %v", got, voluti)
	}
	contiene(t, "0045", corpo, `<span class="filter-pill pill-indaco">🔍 0045</span>
<a id="clear-repair-filters"`, `<a id="repair-report-pdf" href="/repairs/report.pdf?cerca=0045" target="_blank"`)
	nonContiene(t, "0045", corpo, "pill-rossa", "pill-ambra", `<input type="hidden" name="stato"`, `<input type="hidden" name="scadenza"`,
		`<option value="in_lavorazione" selected>`, `name="compro_oro" value="1" checked`)
	if got := ordineRiquadri(b.vai("GET", "/?cerca=0012", nil, nil).corpo); strings.Join(got, ",") != idTesto(s.dodici)+","+idTesto(s.dodici25) {
		t.Errorf("0012: %v", got)
	}
	// Un testo qualunque tiene i filtri (e il nascondimento non c'è perché c'è un filtro).
	if got := ordineRiquadri(b.vai("GET", "/?cerca=Cliente+2&stato=consegnata", nil, nil).corpo); strings.Join(got, ",") != idTesto(s.chiusaA)+","+idTesto(s.consegnata) {
		t.Errorf("testo con lo stato: %v", got)
	}
}

// Il pannello e la scelta per il numero D-64: «#0045/26» con l'etichetta, «N. 26/26» senza; la
// descrizione ai primi 80 caratteri (grafemi).
func TestRigaDellaScansione(t *testing.T) {
	q := "RIP-2025-0045"
	r := rigaDellaScansione(buste.RigaQR{ID: 7, Numero: "25/26", CodiceQR: &q, NomeCliente: "Ada", DataAccettazione: "2026-03-02",
		Descrizione: strings.Repeat("é", 79) + "👍🏽x", Stato: "annullata"})
	if r.Titolo != "#0045/25 — Ada" || r.Href != "/repairs/7" || r.Data != "02/03/2026" || !r.Attenuata || r.Stato != "Annullata" ||
		r.Descrizione != strings.Repeat("é", 79)+"👍🏽" {
		t.Errorf("%+v", r)
	}
	r = rigaDellaScansione(buste.RigaQR{ID: 8, Numero: "26/26", NomeCliente: "Bo", DataAccettazione: "2026-03-02", Stato: "chiusa"})
	if r.Titolo != "N. 26/26 — Bo" || r.Attenuata {
		t.Errorf("%+v", r)
	}
}
