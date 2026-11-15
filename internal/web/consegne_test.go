package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Le pagine delle consegne all'artigiano (consegne.go, slice 077), come HandoverLive e HandoverSessionLive
// dello standalone per gli eventi open, add, remove e confirm: i casi di handover_live_test.exs e
// handover_session_live_test.exs, senza LiveView, con la D-64 per il numero. Nomi e numeri inventati;
// l'orologio è fermo al 31/10/2026, le 10:00 in negozio.

const rottaConsegne = "/handovers"

func consegnaProva(t *testing.T, d *db.DB, artigiano int64, creata, chiusa string) int64 {
	t.Helper()
	var c any
	if chiusa != "" {
		c = chiusa
	}
	res, err := d.Scrittura.Exec(`INSERT INTO consegne_artigiani (artigiano_id, chiusa_il, creato_il, aggiornato_il) VALUES (?, ?, ?, ?)`, artigiano, c, creata, creata)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func percorsoConsegna(id int64) string { return rottaConsegne + "/" + itoa(id) }

// bustaConsegna crea una busta con il codice dato (vuoto: nessuno) e la descrizione; dà id e numero.
func bustaConsegna(t *testing.T, d *db.DB, o clock.Orologio, codice, descrizione string) (int64, string) {
	t.Helper()
	id := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.Descrizione = descrizione; a.DataAccettazione = "2026-10-31" })
	var qr any
	if codice != "" {
		qr = codice
	}
	eseguiSQL(t, d, `UPDATE buste SET codice_qr = ? WHERE id = ?`, qr, id)
	var n string
	if err := d.Lettura.QueryRow(`SELECT numero FROM buste WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return id, n
}

func giroBusta(t *testing.T, d *db.DB, id int64) string {
	t.Helper()
	var s string
	if err := d.Lettura.QueryRow(`SELECT coalesce(consegna_artigiano_id, '-') || '|' || CASE WHEN consegnata_artigiano_il IS NULL THEN '-' ELSE 'si' END || '|' || coalesce(artigiano_id, '-') FROM buste WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// inventario: ART-05, ART-07
// La lista delle consegne per il personale: titolo, frase, le sessioni di oggi in raccolta (artigiano, «1
// busta» / «N buste», l'ora del negozio) con il link alla sessione, il menu con «— scegli —» e «(sessione
// aperta)» per chi ha una consegna non chiusa di qualunque giorno; senza artigiani «Nessun artigiano in
// anagrafica.» con il link all'anagrafica. Aprire la pagina cancella le consegne vuote dei giorni prima.
// I link a storico e solleciti compaiono quando le loro pagine ci sono (082, 083).
func TestPaginaConsegne(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	r := b.vai("GET", rottaConsegne, nil, nil)
	if r.codice != 200 {
		t.Fatalf("vuota: %d", r.codice)
	}
	contiene(t, "vuota", r.corpo, "<title>Consegna ad artigiano", `<h1>📤 Consegna ad artigiano</h1>`+"\n"+
		`<p class="sotto">Crea una sessione di consegna e aggiungi le buste da portare all'artigiano.</p>`,
		`<section id="handover-new" class="riquadro">`+"\n"+`<h2>Nuova consegna</h2>`,
		`<form id="new-handover-form" action="/handovers" method="post" class="consegne-nuova" data-invio-unico>`+"\n"+`<input name="_csrf_token" type="hidden" value="`+b.token+`">`,
		`<label for="handover-artisan-id">Artigiano</label>`+"\n"+`<select id="handover-artisan-id" name="handover[artisan_id]">`+"\n"+`<option value="">— scegli —</option>`+"\n"+`</select>`,
		`<p class="sotto">Se per oggi esiste già una sessione in raccolta per quell'artigiano, viene riaperta.</p>`+"\n"+`<button type="submit">Apri sessione</button>`,
		`<p id="handover-no-artisans" class="sotto">Nessun artigiano in anagrafica. <a href="/settings/anagrafiche">Aggiungine uno</a></p>`)
	nonContiene(t, "vuota", r.corpo, "handovers-open-today", "handover-history-link", "artisan-reminders-link")
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Zeta <Bottega>'), (2, 'Alfa Orafi'), (3, 'Beta')`)
	ieri := consegnaProva(t, d, 3, "2026-10-29T08:00:00.000000Z", "")     // vuota di ieri: la pagina la cancella
	conBusta := consegnaProva(t, d, 1, "2026-10-29T08:00:00.000000Z", "") // di ieri con una busta: resta aperta
	oggi := consegnaProva(t, d, 2, "2026-10-31T07:05:00.000000Z", "")
	una := consegnaProva(t, d, 1, "2026-10-31T08:30:00.000000Z", "")
	busta, _ := bustaConsegna(t, d, o, "", "Anello")
	eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, conBusta, busta)
	for range 2 {
		id, _ := bustaConsegna(t, d, o, "", "Collana")
		eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, oggi, id)
	}
	id, _ := bustaConsegna(t, d, o, "", "Bracciale")
	eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, una, id)
	r = b.vai("GET", rottaConsegne, nil, nil)
	contiene(t, "piena", r.corpo, `<section id="handovers-open-today" class="riquadro">`+"\n"+`<h2>Sessioni aperte oggi</h2>`,
		`<li id="handover-row-`+itoa(una)+`"><a href="/handovers/`+itoa(una)+`" class="consegne-riga"><span><strong>Zeta &lt;Bottega&gt;</strong> — 1 busta</span> <span class="sotto-in-linea">09:30</span></a></li>`,
		`<li id="handover-row-`+itoa(oggi)+`"><a href="/handovers/`+itoa(oggi)+`" class="consegne-riga"><span><strong>Alfa Orafi</strong> — 2 buste</span> <span class="sotto-in-linea">08:05</span></a></li>`,
		`<option value="">— scegli —</option>`+"\n"+`<option value="2">Alfa Orafi (sessione aperta)</option>`+"\n"+`<option value="3">Beta</option>`+"\n"+`<option value="1">Zeta &lt;Bottega&gt; (sessione aperta)</option>`+"\n"+`</select>`)
	nonContiene(t, "piena", r.corpo, "handover-no-artisans", `id="handover-row-`+itoa(conBusta)+`"`)
	if strings.Index(r.corpo, `id="handover-row-`+itoa(una)+`"`) > strings.Index(r.corpo, `id="handover-row-`+itoa(oggi)+`"`) {
		t.Fatal("le sessioni di oggi vanno dalla più recente")
	}
	if contaRighe(t, d, `SELECT count(*) FROM consegne_artigiani WHERE id = `+itoa(ieri)) != 0 || contaRighe(t, d, `SELECT count(*) FROM consegne_artigiani`) != 3 {
		t.Fatal("la pagina non ha pulito le consegne vuote di ieri")
	}
	// Con le pagine dello storico e dei solleciti, i loro link.
	b2, _ := dentroCome(t, accounts.Dipendente, func(f *facciaWeb, mux *http.ServeMux) {
		f.rottaConGuscio(mux, "GET /handovers/history", personale, f.home)
		f.rottaConGuscio(mux, "GET /handovers/reminders", personale, f.home)
	})
	contiene(t, "con i link", b2.vai("GET", rottaConsegne, nil, nil).corpo, `<p class="consegne-link"><a id="handover-history-link" href="/handovers/history">📋 Storico consegne</a> <a id="artisan-reminders-link" href="/handovers/reminders">⏰ Solleciti pendenti</a></p>`)
}

// inventario: ART-06
// «Apri sessione»: nessuna scelta è «Seleziona un artigiano.», un id non numerico «Artigiano non valido.», un
// artigiano che non c'è «Artigiano non trovato.», con la pagina ridisegnata e la scelta tenuta; un artigiano
// valido porta alla sua consegna di oggi in raccolta, la stessa una seconda volta. Senza token il 403.
func TestApriConsegnaDallaPagina(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Alfa Orafi')`)
	b.vai("GET", rottaConsegne, nil, nil)
	for _, c := range []struct {
		valori    url.Values
		messaggio string
	}{
		{url.Values{"handover[artisan_id]": {""}}, "Seleziona un artigiano."},
		{url.Values{"handover[artisan_id]": {"  "}}, "Seleziona un artigiano."},
		{url.Values{}, "Seleziona un artigiano."},
		{url.Values{"handover": {"1"}}, "Seleziona un artigiano."},
		{url.Values{"handover": {"1"}, "handover[artisan_id]": {"1"}}, "Seleziona un artigiano."},
		{url.Values{"handover[artisan_id]": {"abc"}}, "Artigiano non valido."},
		{url.Values{"handover[artisan_id]": {"0"}}, "Artigiano non valido."},
		{url.Values{"handover[artisan_id]": {"-1"}}, "Artigiano non valido."},
		{url.Values{"handover[artisan_id]": {"999"}}, "Artigiano non trovato."},
	} {
		c.valori.Set("_csrf_token", b.token)
		r := b.vai("POST", rottaConsegne, c.valori, nil)
		if r.codice != 200 {
			t.Fatalf("%v: %d", c.valori, r.codice)
		}
		contiene(t, c.messaggio, r.corpo, `<div role="alert" class="flash flash-errore">`+c.messaggio+`</div>`, `<h2>Nuova consegna</h2>`)
	}
	if contaRighe(t, d, `SELECT count(*) FROM consegne_artigiani`) != 0 {
		t.Fatal("un rifiuto ha aperto una consegna")
	}
	// Una scrittura che non riesce è «Impossibile aprire la sessione.», con la scelta tenuta.
	eseguiSQL(t, d, `CREATE TRIGGER niente_consegne BEFORE INSERT ON consegne_artigiani BEGIN SELECT RAISE(ABORT, 'rotto'); END`)
	r := b.vai("POST", rottaConsegne, url.Values{"_csrf_token": {b.token}, "handover[artisan_id]": {"1"}}, nil)
	contiene(t, "guasto", r.corpo, `<div role="alert" class="flash flash-errore">Impossibile aprire la sessione.</div>`, `<option value="1" selected>Alfa Orafi</option>`)
	eseguiSQL(t, d, `DROP TRIGGER niente_consegne`)
	r = b.vai("POST", rottaConsegne, url.Values{"_csrf_token": {b.token}, "handover[artisan_id]": {" 1 "}}, nil)
	if r.codice != 302 || !strings.HasPrefix(r.location, "/handovers/") {
		t.Fatalf("apertura: %d %q", r.codice, r.location)
	}
	prima := r.location
	if r := b.vai("POST", rottaConsegne, url.Values{"_csrf_token": {b.token}, "handover[artisan_id]": {"1"}}, nil); !redirectA(r, prima) {
		t.Fatalf("ripresa: %d %q, attesa %q", r.codice, r.location, prima)
	}
	if contaRighe(t, d, `SELECT count(*) FROM consegne_artigiani WHERE artigiano_id = 1`) != 1 {
		t.Fatal("aperte due consegne")
	}
	if r := b.vai("POST", rottaConsegne, url.Values{"handover[artisan_id]": {"1"}}, nil); r.codice != 403 {
		t.Fatalf("senza token: %d", r.codice)
	}
}

// inventario: ART-08, ART-10, ART-11
// La pagina della sessione: «← Indietro» verso la lista, «📤 Consegna a <artigiano>», «Sessione #N aperta il
// gg/mm/aaaa hh:mm», il campo «Numero busta» (D-64: «es. 0045/26»), le buste dalla più recente con il numero
// visibile e la descrizione troncata, ✕ con la domanda, «✓ Conferma senza stampare» con il conto. Una
// sessione con una busta uscita è «· Confermata», senza campo, senza ✕ e senza conferma. Un id che non c'è
// torna alla lista con «Consegna non trovata.».
func TestPaginaSessione(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Alfa <Orafi>')`)
	c := consegnaProva(t, d, 1, "2026-10-31T07:05:00.000000Z", "")
	r := b.vai("GET", percorsoConsegna(c), nil, nil)
	if r.codice != 200 {
		t.Fatalf("%d", r.codice)
	}
	contiene(t, "vuota", r.corpo, "<title>Consegna a Alfa &lt;Orafi&gt;",
		`<nav class="gs-back-bar" aria-label="Ritorno"><a id="handover-back" href="/handovers" class="gs-back-btn">← Indietro</a></nav>`,
		`<h1>📤 Consegna a Alfa &lt;Orafi&gt;</h1>`+"\n"+`<p class="sotto">Sessione #`+itoa(c)+` aperta il 31/10/2026 08:05</p>`,
		`<section id="handover-add" class="riquadro">`+"\n"+`<form id="handover-add-form" action="/handovers/`+itoa(c)+`/aggiungi" method="post" class="consegna-aggiungi" data-invio-unico>`+"\n"+`<input name="_csrf_token" type="hidden" value="`+b.token+`">`,
		`<label for="handover-bag-number">Numero busta</label>`+"\n"+`<input id="handover-bag-number" name="bag[numero_busta]" type="text" value="" placeholder="es. 0045/26" autocomplete="off">`,
		`<button type="submit">Aggiungi</button>`,
		`<h2 class="consegna-titolo"><span>Buste in consegna</span> <span id="handover-bag-count" class="sotto-in-linea">0</span></h2>`,
		`<p id="handover-empty" class="sotto">Nessuna busta. Aggiungi il numero busta.</p>`,
		`<form id="handover-confirm-form" action="/handovers/`+itoa(c)+`/conferma" method="post" class="consegna-azioni" data-invio-unico>`,
		`<button id="handover-confirm" type="submit" class="pulsante-contorno" data-confirm="Confermare la consegna di 0 busta/e senza stampare etichette?">✓ Conferma senza stampare</button>`)
	nonContiene(t, "vuota", r.corpo, "handover-confirmed-badge", "Anteprima", "Stampa e conferma", "Scansiona")
	lunga := strings.Repeat("é", 59) + "1234567" // 66 grafemi: 59 e «…»
	b1, _ := bustaConsegna(t, d, o, "RIP-2026-0045", lunga)
	b2, n2 := bustaConsegna(t, d, o, "", strings.Repeat("a", 65))
	eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id IN (?, ?)`, c, b1, b2)
	r = b.vai("GET", percorsoConsegna(c), nil, nil)
	contiene(t, "piena", r.corpo, `<span id="handover-bag-count" class="sotto-in-linea">2</span>`,
		`<li id="handover-bag-`+itoa(b2)+`" class="consegna-busta"><div><strong>N. `+n2+`</strong> <span class="sotto-in-linea">— `+strings.Repeat("a", 65)+`</span></div>`,
		`<li id="handover-bag-`+itoa(b1)+`" class="consegna-busta"><div><strong>#0045/26</strong> <span class="sotto-in-linea">— `+strings.Repeat("é", 59)+`…</span></div>`,
		`<form action="/handovers/`+itoa(c)+`/buste/`+itoa(b1)+`/togli" method="post" data-invio-unico><input name="_csrf_token" type="hidden" value="`+b.token+`"><button id="handover-remove-`+itoa(b1)+`" type="submit" class="pulsante-leggero" data-confirm="Rimuovere la busta dalla consegna?" title="Rimuovi">✕</button></form>`,
		`data-confirm="Confermare la consegna di 2 busta/e senza stampare etichette?"`)
	nonContiene(t, "piena", r.corpo, "handover-empty")
	if strings.Index(r.corpo, `id="handover-bag-`+itoa(b2)+`"`) > strings.Index(r.corpo, `id="handover-bag-`+itoa(b1)+`"`) {
		t.Fatal("le buste vanno dalla più recente")
	}
	// Uscita la più recente (la prima della lista), non l'ultima: «· Confermata» vale per una busta qualunque.
	eseguiSQL(t, d, `UPDATE buste SET consegnata_artigiano_il = ? WHERE id = ?`, "2026-10-31T09:00:00.000000Z", b2)
	r = b.vai("GET", percorsoConsegna(c), nil, nil)
	contiene(t, "confermata", r.corpo, `aperta il 31/10/2026 08:05 <span id="handover-confirmed-badge" class="consegna-confermata">· Confermata</span></p>`)
	nonContiene(t, "confermata", r.corpo, "handover-add", "handover-remove-", "handover-confirm-form", "handover-bag-number")
	for _, p := range []string{"/handovers/999", "/handovers/abc", "/handovers/0"} {
		r := b.vai("GET", p, nil, nil)
		if !redirectA(r, rottaConsegne) {
			t.Fatalf("%s: %d %q", p, r.codice, r.location)
		}
		contiene(t, p, b.vai("GET", rottaConsegne, nil, nil).corpo, `<div role="alert" class="flash flash-errore">Consegna non trovata.</div>`)
	}
}

// inventario: ART-08
// «Aggiungi»: il numero dell'etichetta (D-64) o il QR; una busta nuova torna alla pagina con «✓ Aggiunta
// busta #0045/26» (il numero visibile) e il campo vuoto, la stessa di nuovo con «↻ Busta #0045/26 già in
// lista»; un rifiuto ridisegna la pagina con il messaggio e il codice scritto (senza spazi ai bordi) nel campo.
// Una sessione che non c'è torna alla lista.
func TestAggiungiDallaPagina(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Alfa Orafi')`)
	c := consegnaProva(t, d, 1, "2026-10-31T07:05:00.000000Z", "")
	etichetta, _ := bustaConsegna(t, d, o, "RIP-2026-0045", "Anello")
	senza, numero := bustaConsegna(t, d, o, "", "Collana")
	b.vai("GET", percorsoConsegna(c), nil, nil)
	dove := percorsoConsegna(c) + "/aggiungi"
	aggiungi := func(codice string) risposta {
		return b.vai("POST", dove, url.Values{"_csrf_token": {b.token}, "bag[numero_busta]": {codice}}, nil)
	}
	r := aggiungi(" 0045/26 ")
	if !redirectA(r, percorsoConsegna(c)) {
		t.Fatalf("aggiunta: %d %q", r.codice, r.location)
	}
	r = b.vai("GET", percorsoConsegna(c), nil, nil)
	contiene(t, "aggiunta", r.corpo, `<div role="status" class="flash flash-info">✓ Aggiunta busta #0045/26</div>`, `name="bag[numero_busta]" type="text" value=""`, `id="handover-bag-`+itoa(etichetta)+`"`)
	if giroBusta(t, d, etichetta) != itoa(c)+"|-|1" {
		t.Fatalf("giro %s", giroBusta(t, d, etichetta))
	}
	aggiungi("RIP-2026-0045")
	contiene(t, "già", b.vai("GET", percorsoConsegna(c), nil, nil).corpo, `<div role="status" class="flash flash-info">↻ Busta #0045/26 già in lista</div>`)
	aggiungi(numero)
	contiene(t, "senza etichetta", b.vai("GET", percorsoConsegna(c), nil, nil).corpo, `<div role="status" class="flash flash-info">✓ Aggiunta busta N. `+numero+`</div>`)
	if giroBusta(t, d, senza) != itoa(c)+"|-|1" {
		t.Fatal("busta senza etichetta non aggiunta")
	}
	for _, x := range []struct{ scritto, messaggio, campo string }{
		{"  RIP-2026-0099 ", "Riparazione non trovata: nessuna busta con numero o QR «RIP-2026-0099»", "RIP-2026-0099"},
		{"   ", "Numero busta o QR richiesto", ""},
		{"<b>", "Riparazione non trovata: nessuna busta con numero o QR «&lt;b&gt;»", "&lt;b&gt;"},
	} {
		r := aggiungi(x.scritto)
		if r.codice != 200 {
			t.Fatalf("%q: %d", x.scritto, r.codice)
		}
		contiene(t, x.scritto, r.corpo, `<div role="alert" class="flash flash-errore">`+x.messaggio+`</div>`,
			`<input id="handover-bag-number" name="bag[numero_busta]" type="text" value="`+x.campo+`"`, `<h1>📤 Consegna a Alfa Orafi</h1>`)
	}
	eseguiSQL(t, d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, senza)
	eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = NULL WHERE id = ?`, senza)
	contiene(t, "stato", aggiungi(numero).corpo, `Stato &#39;consegnata&#39; non adatto («`+numero+`»)`)
	// Un modulo senza la mappa bag è un campo vuoto.
	r = b.vai("POST", dove, url.Values{"_csrf_token": {b.token}, "bag": {"x"}}, nil)
	contiene(t, "senza mappa", r.corpo, "Numero busta o QR richiesto")
	// Sessione chiusa: il rifiuto del dominio; sessione che non c'è: la lista.
	eseguiSQL(t, d, `UPDATE consegne_artigiani SET chiusa_il = ? WHERE id = ?`, "2026-10-31T09:00:00.000000Z", c)
	contiene(t, "chiusa", aggiungi("0045/26").corpo, "Consegna non disponibile («0045/26»)")
	for _, p := range []string{"/handovers/999/aggiungi", "/handovers/x/aggiungi"} {
		r := b.vai("POST", p, url.Values{"_csrf_token": {b.token}, "bag[numero_busta]": {"0045/26"}}, nil)
		if !redirectA(r, rottaConsegne) {
			t.Fatalf("%s: %d %q", p, r.codice, r.location)
		}
	}
	if righeAudit(t, d, "handover.confirmed") != 0 {
		t.Fatal("l'aggiunta ha scritto nel registro")
	}
}

// inventario: ART-10
// ✕ toglie la busta e torna alla pagina con «Busta rimossa dalla consegna.», anche quando non cambia niente
// (busta uscita o di un'altra consegna); un id di busta non valido torna senza messaggio.
func TestTogliDallaPagina(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Alfa Orafi')`)
	c := consegnaProva(t, d, 1, "2026-10-31T07:05:00.000000Z", "")
	busta, _ := bustaConsegna(t, d, o, "", "Anello")
	eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ?, artigiano_id = 1 WHERE id = ?`, c, busta)
	b.vai("GET", percorsoConsegna(c), nil, nil)
	togli := func(id string) risposta {
		return b.vai("POST", percorsoConsegna(c)+"/buste/"+id+"/togli", url.Values{"_csrf_token": {b.token}}, nil)
	}
	if r := togli(itoa(busta)); !redirectA(r, percorsoConsegna(c)) {
		t.Fatalf("togli: %d %q", r.codice, r.location)
	}
	contiene(t, "tolta", b.vai("GET", percorsoConsegna(c), nil, nil).corpo, `<div role="status" class="flash flash-info">Busta rimossa dalla consegna.</div>`, `id="handover-empty"`)
	if giroBusta(t, d, busta) != "-|-|1" {
		t.Fatalf("giro %s", giroBusta(t, d, busta))
	}
	togli("999")
	contiene(t, "nessuna", b.vai("GET", percorsoConsegna(c), nil, nil).corpo, "Busta rimossa dalla consegna.")
	r := togli("abc")
	if !redirectA(r, percorsoConsegna(c)) {
		t.Fatalf("id non valido: %d", r.codice)
	}
	nonContiene(t, "id non valido", b.vai("GET", percorsoConsegna(c), nil, nil).corpo, "flash-info")
	if r := b.vai("POST", "/handovers/999/buste/1/togli", url.Values{"_csrf_token": {b.token}}, nil); !redirectA(r, rottaConsegne) {
		t.Fatalf("sessione che non c'è: %d %q", r.codice, r.location)
	}
}

// inventario: ART-11
// «✓ Conferma senza stampare»: senza buste «Aggiungi almeno una busta.» sulla pagina; con le buste le timbra,
// scrive handover.confirmed con gli id e porta a /?fuori_bottega=1 con «Consegna confermata: N buste fuori
// bottega.» («1 busta»). Una sessione chiusa dice «Consegna non disponibile».
func TestConfermaDallaPagina(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Alfa Orafi')`)
	c := consegnaProva(t, d, 1, "2026-10-31T07:05:00.000000Z", "")
	b.vai("GET", percorsoConsegna(c), nil, nil)
	conferma := func(id int64) risposta {
		return b.vai("POST", percorsoConsegna(id)+"/conferma", url.Values{"_csrf_token": {b.token}}, nil)
	}
	if r := conferma(c); !redirectA(r, percorsoConsegna(c)) {
		t.Fatalf("vuota: %d %q", r.codice, r.location)
	}
	contiene(t, "vuota", b.vai("GET", percorsoConsegna(c), nil, nil).corpo, `<div role="alert" class="flash flash-errore">Aggiungi almeno una busta.</div>`)
	if righeAudit(t, d, "handover.confirmed") != 0 {
		t.Fatal("conferma vuota scritta")
	}
	busta, _ := bustaConsegna(t, d, o, "", "Anello")
	eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, c, busta)
	if r := conferma(c); !redirectA(r, "/?fuori_bottega=1") {
		t.Fatalf("conferma: %d %q", r.codice, r.location)
	}
	contiene(t, "home", b.vai("GET", "/?fuori_bottega=1", nil, nil).corpo, `<div role="status" class="flash flash-info">Consegna confermata: 1 busta fuori bottega.</div>`)
	if giroBusta(t, d, busta) != itoa(c)+"|si|-" || contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'handover.confirmed' AND attore = 'banco@esempio.it' AND dati = '{"handed_over":1,"repair_ids":[`+itoa(busta)+`]}'`) != 1 {
		t.Fatal("conferma non scritta")
	}
	altra := consegnaProva(t, d, 1, "2026-10-31T07:05:00.000000Z", "")
	for range 2 {
		id, _ := bustaConsegna(t, d, o, "", "Collana")
		eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, altra, id)
	}
	conferma(altra)
	contiene(t, "due", b.vai("GET", "/?fuori_bottega=1", nil, nil).corpo, "Consegna confermata: 2 buste fuori bottega.")
	chiusa := consegnaProva(t, d, 1, "2026-10-31T07:05:00.000000Z", "2026-10-31T08:00:00.000000Z")
	id, _ := bustaConsegna(t, d, o, "", "Spilla")
	eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, chiusa, id)
	if r := conferma(chiusa); !redirectA(r, percorsoConsegna(chiusa)) {
		t.Fatalf("chiusa: %d %q", r.codice, r.location)
	}
	contiene(t, "chiusa", b.vai("GET", percorsoConsegna(chiusa), nil, nil).corpo, `<div role="alert" class="flash flash-errore">Consegna non disponibile</div>`)
	if r := b.vai("POST", "/handovers/999/conferma", url.Values{"_csrf_token": {b.token}}, nil); !redirectA(r, rottaConsegne) {
		t.Fatalf("sessione che non c'è: %d %q", r.codice, r.location)
	}
}

// inventario: ART-08
// Dopo «Aggiungi» il cursore torna nel campo del numero (in LiveView non si ricarica la pagina): app.js
// annota l'invio di #handover-add-form nella scheda e la pagina dopo mette il fuoco su #handover-bag-number,
// una volta. Il comportamento lo prova il banco browser (sessioni-consegna, etichetta.fuoco).
func TestScriptDelFuocoDellaConsegna(t *testing.T) {
	b, _ := dentroCome(t, accounts.Dipendente, nil)
	js := assetDellaPagina(t, b, b.vai("GET", "/", nil, nil).corpo, "js", "text/javascript; charset=utf-8")
	for _, s := range []string{
		`if (event.target && event.target.id === "handover-add-form") {` + "\n" + `    try { sessionStorage.setItem("gestrip:handover-focus", "1") }`,
		`fuoco = sessionStorage.getItem("gestrip:handover-focus")` + "\n" + `    sessionStorage.removeItem("gestrip:handover-focus")`,
		`const campo = document.getElementById("handover-bag-number")` + "\n" + `  if (fuoco && campo) campo.focus()`,
	} {
		if !strings.Contains(js, s) {
			t.Errorf("app.js non contiene %q", s)
		}
	}
}
