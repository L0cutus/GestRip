package web

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La pagina di un lotto d'avviso (lotto.go, slice 089), come ReadyLotLive dello standalone: i casi di
// ready_lot_live_test.exs («the lot page», «the interlock seen from the bag's own page»), senza LiveView. Nomi
// inventati; l'orologio è fermo al 31/10/2026, le 10:00 in negozio. Le buste sono dell'oreficeria 2, accettate il
// 31/10/2026, senza etichetta: il numero visibile è «N. 1/26».

func percorsoLotto(id int64) string { return rottaLotti + "/" + itoa(id) }

// prontaDelLotto crea una busta pronta dell'oreficeria 2 con il codice (vuoto: nessuno) e la descrizione.
func prontaDelLotto(t *testing.T, d *db.DB, o clock.Orologio, codice, descrizione string) int64 {
	t.Helper()
	id, _ := bustaConsegna(t, d, o, codice, descrizione)
	eseguiSQL(t, d, `UPDATE buste SET oreficeria_id = 2, stato = 'pronta', data_pronta = '2026-10-31' WHERE id = ?`, id)
	return id
}

// lottoConOreficeria mette l'oreficeria 2 «Alfa Orafi» e il suo lotto aperto alle 09:05 del negozio.
func lottoConOreficeria(t *testing.T, d *db.DB) int64 {
	t.Helper()
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome, telefono) VALUES (2, 'Alfa Orafi', '0490000000')`)
	return lottoProva(t, d, 2, "2026-10-31T08:05:00.000000Z")
}

func modulo(b *browser, coppie ...string) url.Values {
	v := url.Values{"_csrf_token": {b.token}}
	for i := 0; i+1 < len(coppie); i += 2 {
		v.Add(coppie[i], coppie[i+1])
	}
	return v
}

func lottoDellaBusta(t *testing.T, d *db.DB, id int64) string {
	t.Helper()
	var s string
	if err := d.Lettura.QueryRow(`SELECT coalesce(lotto_avviso_id, '-') FROM buste WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// inventario: ORE-06, ORE-08
// La pagina di un lotto aperto: titolo «Lotto <oreficeria>», «← Indietro» verso la lista, «📦 Lotto <oreficeria>»,
// «Lotto #N aperto il gg/mm/aaaa hh:mm» sull'orologio del negozio, le due porte dell'aggiunta («Numero busta» e il
// QR), «Buste nel lotto» con il conto e «Nessuna busta. …» o le buste dalla più recente (numero visibile,
// descrizione tagliata a 50 grafemi, «Rimuovi»), le candidate «Pronte di <oreficeria> non ancora in lotto» con
// «Aggiungi», o la segnalazione e «Apri busta» per una già avvisata. Senza candidate niente riquadro.
func TestPaginaLotto(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	l := lottoConOreficeria(t, d)
	r := b.vai("GET", percorsoLotto(l), nil, nil)
	if r.codice != 200 {
		t.Fatalf("vuota: %d", r.codice)
	}
	contiene(t, "vuota", r.corpo, "<title>Lotto Alfa Orafi",
		`<nav class="gs-back-bar" aria-label="Ritorno"><a id="ready-lot-back" href="/ready-lots" class="gs-back-btn">← Indietro</a></nav>`,
		`<h1>📦 Lotto Alfa Orafi</h1>`+"\n"+`<p class="sotto">Lotto #`+itoa(l)+` aperto il 31/10/2026 09:05</p>`,
		`<form id="ready-lot-add-form" action="/ready-lots/`+itoa(l)+`/aggiungi" method="post" class="consegna-aggiungi" data-invio-unico>`+"\n"+`<input name="_csrf_token" type="hidden" value="`+b.token+`">`,
		`<label for="ready-lot-bag-number">Numero busta</label>`+"\n"+`<input id="ready-lot-bag-number" name="bag[numero_busta]" type="text" value="" placeholder="es. 0045/26" autocomplete="off">`+"\n"+`</div>`+"\n"+`<button type="submit">Aggiungi</button>`,
		`<form id="ready-lot-scan-form" action="/ready-lots/`+itoa(l)+`/qr" method="post"`,
		`<input id="ready-lot-scan-code" name="qr" type="text" value="" autocomplete="off" placeholder="Scansiona o inserisci il codice QR" class="mono">`+"\n"+`<button id="ready-lot-scan-button" type="button" class="pulsante-contorno" data-scansione="/ready-lots/`+itoa(l)+`/scansione" data-ricarica="/ready-lots/`+itoa(l)+`" data-count-label="nel lotto">📷 Scansiona</button>`+"\n"+`<button type="submit" class="pulsante-contorno">Aggiungi dal QR</button>`,
		`<span>Buste nel lotto</span> <span id="ready-lot-bag-count" class="sotto-in-linea">0</span>`,
		`<p id="ready-lot-bags-empty" class="sotto">Nessuna busta. Scansiona o aggiungi a mano.</p>`)
	nonContiene(t, "vuota", r.corpo, "ready-lot-closed-badge", "ready-lot-candidates", "ready-lot-gone", "ready-lot-import-disabled", "Chiudi e invia")
	membro := prontaDelLotto(t, d, o, "", "Anello & catena")
	lunga := prontaDelLotto(t, d, o, "", strings.Repeat("é", 50)+"x")
	giusta := prontaDelLotto(t, d, o, "", strings.Repeat("a", 50))
	avvisata := prontaDelLotto(t, d, o, "", "Collana")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, l, membro)
	eseguiSQL(t, d, `UPDATE buste SET avviso_pronta_il = '2026-10-31T08:00:00.000000Z' WHERE id = ?`, avvisata)
	eseguiSQL(t, d, `UPDATE buste SET data_pronta = '2026-10-01' WHERE id = ?`, lunga)
	r = b.vai("GET", percorsoLotto(l), nil, nil)
	contiene(t, "piena", r.corpo,
		`<span id="ready-lot-bag-count" class="sotto-in-linea">1</span>`,
		`<li id="ready-lot-bag-`+itoa(membro)+`" class="consegna-busta"><div><strong>N. 1/26</strong> <span class="sotto-in-linea">— Anello &amp; catena</span></div>`+"\n"+
			`<form action="/ready-lots/`+itoa(l)+`/buste/`+itoa(membro)+`/togli" method="post" data-invio-unico><input name="_csrf_token" type="hidden" value="`+b.token+`"><button id="ready-lot-remove-`+itoa(membro)+`" type="submit" class="pulsante-leggero">Rimuovi</button></form>`,
		`<h2>Pronte di Alfa Orafi non ancora in lotto</h2>`,
		// Per data di pronta e id decrescenti: l'avvisata, la giusta, poi la lunga (pronta il 01/10).
		`<li id="ready-lot-candidate-`+itoa(avvisata)+`" class="consegna-busta"><div><strong>N. 4/26</strong> <span class="sotto-in-linea">— Collana</span>`+"\n"+
			`<span id="ready-lot-candidate-`+itoa(avvisata)+`-told" class="lotto-avvisata">Già avvisata singolarmente — azzera l&#39;avviso dal dettaglio per metterla in lotto.</span></div>`+"\n"+
			`<a href="/repairs/`+itoa(avvisata)+`" class="pulsante-leggero">Apri busta</a>`+"\n"+`</li>`+"\n"+
			`<li id="ready-lot-candidate-`+itoa(giusta)+`" class="consegna-busta"><div><strong>N. 3/26</strong> <span class="sotto-in-linea">— `+strings.Repeat("a", 50)+`</span></div>`+"\n"+
			`<form action="/ready-lots/`+itoa(l)+`/buste/`+itoa(giusta)+`/aggiungi" method="post" data-invio-unico><input name="_csrf_token" type="hidden" value="`+b.token+`"><button id="ready-lot-candidate-add-`+itoa(giusta)+`" type="submit" class="pulsante-contorno">Aggiungi</button></form>`+"\n"+`</li>`+"\n"+
			`<li id="ready-lot-candidate-`+itoa(lunga)+`" class="consegna-busta"><div><strong>N. 2/26</strong> <span class="sotto-in-linea">— `+strings.Repeat("é", 50)+`…</span></div>`)
	nonContiene(t, "piena", r.corpo, "ready-lot-bags-empty", `id="ready-lot-candidate-`+itoa(membro)+`"`, `ready-lot-candidate-add-`+itoa(avvisata))
}

// inventario: ORE-06
// Un lotto che non c'è, o un id non valido, torna alla lista con «Lotto non trovato.».
func TestPaginaLottoNonTrovato(t *testing.T) {
	h, _ := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	for _, p := range []string{"/ready-lots/9999", "/ready-lots/abc", "/ready-lots/0", "/ready-lots/99999999999999999999"} {
		r := b.vai("GET", p, nil, nil)
		if !redirectA(r, rottaLotti) {
			t.Fatalf("%s: %d %q", p, r.codice, r.location)
		}
		contiene(t, p, seguiRedirect(t, b, r).corpo, `<div role="alert" class="flash flash-errore">Lotto non trovato.</div>`, `<h1>📦 Avviso oreficeria</h1>`)
	}
	b.vai("GET", rottaLotti, nil, nil)
	if r := b.vai("POST", "/ready-lots/9999/aggiungi", modulo(b, "bag[numero_busta]", "1/26"), nil); !redirectA(r, rottaLotti) {
		t.Fatalf("aggiunta a un lotto che non c'è: %d", r.codice)
	}
}

// inventario: ORE-06
// Le tre porte dell'aggiunta: il numero scritto (con spazi), il QR e «Aggiungi» di una candidata; ognuna torna alla
// pagina con «✓ Aggiunta busta <numero visibile>», la busta fra quelle del lotto e il campo vuoto; alla fine niente
// candidate. Ogni aggiunta scrive ready_lot.bag_added con l'utente. Senza token il 403.
func TestAggiungiDallaPaginaDelLotto(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	l := lottoConOreficeria(t, d)
	scritta, letta, scelta := prontaDelLotto(t, d, o, "", "Uno"), prontaDelLotto(t, d, o, "QR-LETTA", "Due"), prontaDelLotto(t, d, o, "", "Tre")
	b.vai("GET", percorsoLotto(l), nil, nil)
	for _, c := range []struct {
		percorso string
		form     url.Values
		busta    int64
		numero   string
	}{
		{"/aggiungi", modulo(b, "bag[numero_busta]", "  01/26 "), scritta, "N. 1/26"},
		{"/qr", modulo(b, "qr", " QR-LETTA "), letta, "N. 2/26"},
		{"/buste/" + itoa(scelta) + "/aggiungi", modulo(b), scelta, "N. 3/26"},
	} {
		r := b.vai("POST", percorsoLotto(l)+c.percorso, c.form, nil)
		if !redirectA(r, percorsoLotto(l)) {
			t.Fatalf("%s: %d %q", c.percorso, r.codice, r.location)
		}
		p := seguiRedirect(t, b, r)
		contiene(t, c.percorso, p.corpo, `<div role="status" class="flash flash-info">✓ Aggiunta busta `+c.numero+`</div>`, `id="ready-lot-bag-`+itoa(c.busta)+`"`,
			`name="bag[numero_busta]" type="text" value=""`, `name="qr" type="text" value=""`)
		if lottoDellaBusta(t, d, c.busta) != itoa(l) {
			t.Fatalf("%s: busta non nel lotto", c.percorso)
		}
	}
	p := b.vai("GET", percorsoLotto(l), nil, nil)
	contiene(t, "fine", p.corpo, `<span id="ready-lot-bag-count" class="sotto-in-linea">3</span>`)
	nonContiene(t, "fine", p.corpo, "ready-lot-candidates")
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'ready_lot.bag_added' AND attore = 'banco@esempio.it' AND id_risorsa = '`+itoa(l)+`'`); n != 3 {
		t.Fatalf("audit: %d", n)
	}
	altra := prontaDelLotto(t, d, o, "", "Quattro")
	if r := b.vai("POST", percorsoLotto(l)+"/buste/"+itoa(altra)+"/aggiungi", url.Values{}, nil); r.codice != 403 || lottoDellaBusta(t, d, altra) != "-" {
		t.Fatalf("senza token: %d", r.codice)
	}
}

// inventario: ORE-06
// I rifiuti ridisegnano la pagina (200) con il messaggio d'errore e il numero scritto nel campo: una busta già
// avvisata (che resta fra le candidate, segnata), un numero che non c'è, i campi vuoti o forgiati. «Busta già in
// questo lotto.» è un esito, non un errore. Dal QR il codice resta nel suo campo; da una candidata il campo del numero
// è vuoto; un id di candidata non valido torna alla pagina senza messaggio. Niente si scrive.
func TestRifiutiDellaPaginaDelLotto(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	l := lottoConOreficeria(t, d)
	avvisata, dentro := prontaDelLotto(t, d, o, "QR-AVVISATA", "Uno"), prontaDelLotto(t, d, o, "", "Due")
	eseguiSQL(t, d, `UPDATE buste SET avviso_pronta_il = '2026-10-31T08:00:00.000000Z' WHERE id = ?`, avvisata)
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, l, dentro)
	b.vai("GET", percorsoLotto(l), nil, nil)
	avvisataMsg := "Busta già avvisata singolarmente: azzera l&#39;avviso dal dettaglio, poi riprova."
	for _, c := range []struct {
		percorso        string
		form            url.Values
		errore, scritto string
		scrittoQR       string
	}{
		{"/aggiungi", modulo(b, "bag[numero_busta]", " 1/26 "), avvisataMsg, "1/26", ""},
		{"/qr", modulo(b, "qr", "QR-AVVISATA"), avvisataMsg, "", "QR-AVVISATA"},
		{"/buste/" + itoa(avvisata) + "/aggiungi", modulo(b), avvisataMsg, "", ""},
		{"/aggiungi", modulo(b, "bag[numero_busta]", "99/26"), "Riparazione non trovata.", "99/26", ""},
		{"/qr", modulo(b, "qr", "QR-NESSUNO"), "Riparazione non trovata.", "", "QR-NESSUNO"},
		{"/buste/99999/aggiungi", modulo(b), "Riparazione non trovata.", "", ""},
		{"/aggiungi", modulo(b, "bag[numero_busta]", "  "), "Numero busta, QR o id richiesto.", "", ""},
		{"/aggiungi", modulo(b), "Numero busta, QR o id richiesto.", "", ""},
		{"/aggiungi", modulo(b, "bag", "1/26", "bag[numero_busta]", "1/26"), "Numero busta, QR o id richiesto.", "", ""},
		{"/aggiungi", modulo(b, "bag[numero_busta][x]", "1/26"), "Numero busta, QR o id richiesto.", "", ""},
		{"/aggiungi", modulo(b, "bag[numero_busta][x]", "1/26", "bag[numero_busta]", "1/26"), "Numero busta, QR o id richiesto.", "", ""}, // mutazione 32
		{"/qr", modulo(b, "qr[]", "QR-AVVISATA", "qr", "QR-AVVISATA"), "Numero busta, QR o id richiesto.", "", ""},
		{"/qr", modulo(b, "bag[numero_busta]", "1/26"), "Numero busta, QR o id richiesto.", "", ""},
	} {
		r := b.vai("POST", percorsoLotto(l)+c.percorso, c.form, nil)
		if r.codice != 200 {
			t.Fatalf("%s %v: %d", c.percorso, c.form, r.codice)
		}
		contiene(t, c.percorso+" "+c.errore, r.corpo, `<div role="alert" class="flash flash-errore">`+c.errore+`</div>`, `<h1>📦 Lotto Alfa Orafi</h1>`,
			`name="bag[numero_busta]" type="text" value="`+c.scritto+`"`, `name="qr" type="text" value="`+c.scrittoQR+`"`,
			`id="ready-lot-candidate-`+itoa(avvisata)+`-told"`)
		nonContiene(t, c.percorso, r.corpo, `class="flash flash-info"`)
	}
	// In un altro lotto ancora aperto (D-64: «45» è la busta con l'etichetta 0045/26) e un numero che vale per due buste.
	altroLotto := lottoProva(t, d, 2, "2026-10-31T08:06:00.000000Z")
	inAltro := prontaDelLotto(t, d, o, "RIP-2026-0045", "Tre")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, altroLotto, inAltro)
	r := b.vai("POST", percorsoLotto(l)+"/aggiungi", modulo(b, "bag[numero_busta]", "45"), nil)
	contiene(t, "altro lotto", r.corpo, `<div role="alert" class="flash flash-errore">Busta già nel lotto #`+itoa(altroLotto)+`: toglila da lì, poi riprova.</div>`,
		`name="bag[numero_busta]" type="text" value="45"`)
	gemella := prontaDelLotto(t, d, o, "RIP-2026-0046", "Quattro")
	sorella := prontaDelLotto(t, d, o, "RIP-2026-0046", "Cinque")
	r = b.vai("POST", percorsoLotto(l)+"/aggiungi", modulo(b, "bag[numero_busta]", "46"), nil)
	contiene(t, "più buste", r.corpo, `<div role="alert" class="flash flash-errore">Più buste corrispondono a «46»: scrivi il numero con l&#39;anno o usa il QR.</div>`,
		`name="bag[numero_busta]" type="text" value="46"`)
	if lottoDellaBusta(t, d, inAltro) != itoa(altroLotto) || lottoDellaBusta(t, d, gemella) != "-" || lottoDellaBusta(t, d, sorella) != "-" {
		t.Fatal("un rifiuto ha spostato una busta")
	}
	// La stessa busta di nuovo: un esito, con il numero scritto.
	r = b.vai("POST", percorsoLotto(l)+"/aggiungi", modulo(b, "bag[numero_busta]", "2/26"), nil)
	contiene(t, "già nel lotto", r.corpo, `<div role="status" class="flash flash-info">Busta già in questo lotto.</div>`, `name="bag[numero_busta]" type="text" value="2/26"`)
	nonContiene(t, "già nel lotto", r.corpo, "flash-errore")
	// Un id di candidata non valido: la pagina, senza messaggio.
	for _, p := range []string{"/buste/abc/aggiungi", "/buste/0/aggiungi"} {
		r := b.vai("POST", percorsoLotto(l)+p, modulo(b), nil)
		if !redirectA(r, percorsoLotto(l)) {
			t.Fatalf("%s: %d", p, r.codice)
		}
		nonContiene(t, p, seguiRedirect(t, b, r).corpo, "flash-errore", "flash-info")
	}
	if lottoDellaBusta(t, d, avvisata) != "-" || contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'ready_lot.bag_added'`) != 0 {
		t.Fatal("un rifiuto ha scritto")
	}
}

// inventario: ORE-09
// «Rimuovi» torna alla pagina con «Busta <numero visibile> rimossa dal lotto.» e la busta di nuovo fra le candidate
// (ready_lot.bag_removed con l'utente); di nuovo, «Busta non in questo lotto.»; un id non valido torna senza messaggio.
func TestTogliDallaPaginaDelLotto(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	l := lottoConOreficeria(t, d)
	uno := prontaDelLotto(t, d, o, "", "Uno")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, l, uno)
	b.vai("GET", percorsoLotto(l), nil, nil)
	togli := percorsoLotto(l) + "/buste/" + itoa(uno) + "/togli"
	r := b.vai("POST", togli, modulo(b), nil)
	if !redirectA(r, percorsoLotto(l)) {
		t.Fatalf("togli: %d", r.codice)
	}
	contiene(t, "tolta", seguiRedirect(t, b, r).corpo, `<div role="status" class="flash flash-info">Busta N. 1/26 rimossa dal lotto.</div>`,
		`id="ready-lot-candidate-`+itoa(uno)+`"`, `<p id="ready-lot-bags-empty"`)
	if lottoDellaBusta(t, d, uno) != "-" || contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'ready_lot.bag_removed' AND attore = 'banco@esempio.it'`) != 1 {
		t.Fatal("non tolta")
	}
	r = b.vai("POST", togli, modulo(b), nil)
	contiene(t, "di nuovo", seguiRedirect(t, b, r).corpo, `<div role="alert" class="flash flash-errore">Busta non in questo lotto.</div>`)
	for _, p := range []string{"/buste/abc/togli", "/buste/-1/togli"} {
		r := b.vai("POST", percorsoLotto(l)+p, modulo(b), nil)
		if !redirectA(r, percorsoLotto(l)) {
			t.Fatalf("%s: %d", p, r.codice)
		}
		nonContiene(t, p, seguiRedirect(t, b, r).corpo, "flash-errore", "flash-info")
	}
	if r := b.vai("POST", togli, url.Values{}, nil); r.codice != 403 {
		t.Fatalf("senza token: %d", r.codice)
	}
}

// inventario: ORE-06, ORE-09
// Un lotto inviato è in sola lettura: «· Non più modificabile», niente aggiunte, niente «Rimuovi», niente candidate
// (anche se ce n'è una), «Questo lotto è partito il …» con il link pubblico; apertura e partenza sull'orologio del
// negozio (inverno UTC+1, estate UTC+2). Una pagina vecchia che prova ad aggiungere o togliere ha «Lotto non
// disponibile.» e la busta resta. Il link storico non riattivato all'importazione lo dice al posto del link.
func TestLottoInviatoSolaLettura(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	l := lottoConOreficeria(t, d)
	uno, candidata := prontaDelLotto(t, d, o, "", "Uno"), prontaDelLotto(t, d, o, "", "Due")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, l, uno)
	b.vai("GET", percorsoLotto(l), nil, nil)
	eseguiSQL(t, d, `UPDATE lotti_avviso SET creato_il = '2026-01-15T08:05:00.000000Z', inviato_il = '2026-07-15T08:05:00.000000Z', token = 'lotto-gettone-089' WHERE id = ?`, l)
	r := b.vai("GET", percorsoLotto(l), nil, nil)
	contiene(t, "inviato", r.corpo,
		`<p class="sotto">Lotto #`+itoa(l)+` aperto il 15/01/2026 09:05 <span id="ready-lot-closed-badge" class="consegna-confermata">· Non più modificabile</span></p>`,
		`<li id="ready-lot-bag-`+itoa(uno)+`" class="consegna-busta"><div><strong>N. 1/26</strong> <span class="sotto-in-linea">— Uno</span></div>`+"\n"+`</li>`,
		`<section id="ready-lot-gone" class="riquadro sotto">Questo lotto è partito il 15/07/2026 10:05. Il link pubblico è <a id="ready-lot-gone-link" href="`+indirizzoPubblicoProva+`/lotto/lotto-gettone-089" class="lotto-link">`+indirizzoPubblicoProva+`/lotto/lotto-gettone-089</a>.</section>`)
	nonContiene(t, "inviato", r.corpo, "ready-lot-add-form", "ready-lot-scan-form", "ready-lot-candidates", "Rimuovi", "ready-lot-import-disabled", "15/01/2026 08:05", "15/07/2026 08:05")
	r = b.vai("POST", percorsoLotto(l)+"/buste/"+itoa(candidata)+"/aggiungi", modulo(b), nil)
	contiene(t, "aggiunta vecchia", r.corpo, `<div role="alert" class="flash flash-errore">Lotto non disponibile.</div>`, "ready-lot-closed-badge")
	nonContiene(t, "aggiunta vecchia", r.corpo, "ready-lot-add-form")
	r = b.vai("POST", percorsoLotto(l)+"/buste/"+itoa(uno)+"/togli", modulo(b), nil)
	contiene(t, "togli vecchio", seguiRedirect(t, b, r).corpo, `<div role="alert" class="flash flash-errore">Lotto non disponibile.</div>`)
	if lottoDellaBusta(t, d, uno) != itoa(l) || lottoDellaBusta(t, d, candidata) != "-" {
		t.Fatal("lotto inviato cambiato")
	}
	eseguiSQL(t, d, `UPDATE lotti_avviso SET chiuso_il = '2026-07-16T08:05:00.000000Z', accesso_pubblico_spento_il = '2026-07-16T08:05:00.000000Z' WHERE id = ?`, l)
	r = b.vai("GET", percorsoLotto(l), nil, nil)
	contiene(t, "spento", r.corpo, `<section id="ready-lot-import-disabled" class="riquadro sotto">Link storico non riattivato durante l&#39;importazione.</section>`, "ready-lot-closed-badge")
	nonContiene(t, "spento", r.corpo, "ready-lot-gone", "lotto-gettone-089")
}

// inventario: ORE-06
// Dalla scheda di una busta in un lotto aperto: «In un lotto ancora aperto: l'avviso parte con il lotto.» con «Apri
// il lotto» verso la pagina del lotto; uscito il lotto, la riga non c'è più.
func TestSchedaApriIlLotto(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	l := lottoConOreficeria(t, d)
	uno := prontaDelLotto(t, d, o, "", "Uno")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, l, uno)
	r := seguiRedirect(t, b, b.vai("GET", percorsoScheda(uno), nil, nil))
	contiene(t, "lotto aperto", r.corpo, `<p id="repair-notice-in-lot" class="sotto">In un lotto ancora aperto: l'avviso parte con il lotto. <a id="repair-notice-lot-link" href="/ready-lots/`+itoa(l)+`">Apri il lotto</a></p>`)
	nonContiene(t, "lotto aperto", r.corpo, "repair-notice-pending")
	eseguiSQL(t, d, `UPDATE lotti_avviso SET inviato_il = '2026-10-31T08:30:00.000000Z' WHERE id = ?`, l)
	r = seguiRedirect(t, b, b.vai("GET", percorsoScheda(uno), nil, nil))
	nonContiene(t, "lotto uscito", r.corpo, "repair-notice-in-lot", "Apri il lotto")
}

// lottoRotto fa fallire le letture e le scritture della pagina del lotto.
type lottoRotto struct {
	Buste
	lotto, membri, candidate, aggiunta, tolta bool
}

func (l lottoRotto) LottoAvviso(ctx context.Context, id int64) (buste.LottoAvviso, error) {
	if l.lotto {
		return buste.LottoAvviso{}, errLottiRotti
	}
	return l.Buste.LottoAvviso(ctx, id)
}

func (l lottoRotto) BusteDelLotto(ctx context.Context, id int64) ([]buste.BustaDelLotto, error) {
	if l.membri {
		return nil, errLottiRotti
	}
	return l.Buste.BusteDelLotto(ctx, id)
}

func (l lottoRotto) CandidateDelLotto(ctx context.Context, x buste.LottoAvviso) ([]buste.BustaDelLotto, error) {
	if l.candidate {
		return nil, errLottiRotti
	}
	return l.Buste.CandidateDelLotto(ctx, x)
}

func (l lottoRotto) AggiungiAlLotto(ctx context.Context, id int64, q buste.RichiestaLotto, c audit.Contesto) (buste.BustaDelLotto, error) {
	if l.aggiunta {
		return buste.BustaDelLotto{}, errLottiRotti
	}
	return l.Buste.AggiungiAlLotto(ctx, id, q, c)
}

func (l lottoRotto) TogliDalLotto(ctx context.Context, id, busta int64, c audit.Contesto) (buste.BustaDelLotto, error) {
	if l.tolta {
		return buste.BustaDelLotto{}, errLottiRotti
	}
	return l.Buste.TogliDalLotto(ctx, id, busta, c)
}

// Un guasto del database è la pagina d'errore con la riga nel registro: nello standalone la LiveView cade.
func TestLottoGuasto(t *testing.T) {
	for _, c := range []struct {
		rotti            lottoRotto
		metodo, percorso string
		registro         string
	}{
		{lottoRotto{lotto: true}, "GET", "", "lotto non letto"},
		{lottoRotto{membri: true}, "GET", "", "buste del lotto non lette"},
		{lottoRotto{candidate: true}, "GET", "", "candidate del lotto non lette"},
		{lottoRotto{aggiunta: true}, "POST", "/aggiungi", "busta non aggiunta al lotto"},
		{lottoRotto{tolta: true}, "POST", "/buste/1/togli", "busta non tolta dal lotto"},
	} {
		var reg bytes.Buffer
		h, d := facciaConBusteCon(t, &reg, func(b Buste) Buste { c.rotti.Buste = b; return c.rotti })
		l := lottoConOreficeria(t, d)
		b := dipendenteDentro(t, h)
		var form url.Values
		if c.metodo == "POST" {
			b.vai("GET", rottaLotti, nil, nil)
			form = modulo(b, "bag[numero_busta]", "1/26")
		}
		r := b.vai(c.metodo, percorsoLotto(l)+c.percorso, form, nil)
		if r.codice != http.StatusInternalServerError {
			t.Fatalf("%s: %d", c.registro, r.codice)
		}
		nonContiene(t, c.registro, r.corpo, `id="ready-lot-bags"`)
		contiene(t, c.registro, reg.String(), c.registro)
	}
}

// Il campo del QR della pagina del lotto (slice 100): sul telefono va a capo sopra i due pulsanti invece di
// schiacciarsi accanto a «📷 Scansiona» e «Aggiungi dal QR» (min-width di 12rem, come il campo del numero), e la pagina
// delle consegne, che condivide solo .consegna-aggiungi, non prende la regola. L'effetto sullo schermo lo vede il
// manuale (09-lotto-pagina-telefono.png).
func TestCampoQRDelLottoVaACapo(t *testing.T) {
	css := string(leggiStatico("app.css"))
	contiene(t, "app.css", css, ".lotto-qr input { flex: 1; min-width: 12rem; min-height: 2.75rem; }")
	nonContiene(t, "app.css", css, ".lotto-qr input { flex: 1; min-width: 0;")
	if !strings.Contains(css, ".consegna-aggiungi { display: flex; flex-wrap: wrap;") {
		t.Error("il modulo del QR non va più a capo: .consegna-aggiungi senza flex-wrap")
	}
}
