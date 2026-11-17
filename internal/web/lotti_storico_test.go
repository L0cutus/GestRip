package web

import (
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// Lo storico dei lotti inviati, il dettaglio e il reinvio (lotti_storico.go, slice 093, ORE-11, ORE-12, ORE-13), come
// ReadyLotHistoryLive dello standalone (/ready-lots/history e /ready-lots/history/:id): i casi di
// ready_lot_live_test.exs («the history of sent lots», «resending a sent lot», «the batch send and the bag links (slice
// 104)»), senza LiveView. Nomi inventati; l'oreficeria 2 «Alfa Orafi» ha il telefono 0490000000.

const (
	rottaStoricoLottiProva = "/ready-lots/history"
	gettoneLotto093        = "gettone-lotto-093"
	msgPronto093           = "Messaggio pronto: apri WhatsApp per reinviarlo e registrarlo."
)

func percorsoStoricoLotto(id int64) string { return rottaStoricoLottiProva + "/" + itoa(id) }

// inventario: ORE-11
// La lista dello storico: il ritorno a /ready-lots, il titolo, «Nessun lotto inviato.» senza lotti; poi solo i lotti
// inviati (mai un aperto), dal più recente per id, con l'oreficeria, «lotto #N · N buste · inviato <giorno ora del
// negozio>», «Completato il …» per un lotto chiuso o «N da ritirare» (buste fuori dal cestino non consegnate), e
// «Apri» verso il dettaglio. La lista dei lotti ha il link allo storico.
func TestPaginaStoricoLotti(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	contiene(t, "link", b.vai("GET", rottaLotti, nil, nil).corpo,
		`<p class="consegne-link"><a id="ready-lot-history-link" href="/ready-lots/history">Storico dei lotti inviati</a></p>`)
	r := b.vai("GET", rottaStoricoLottiProva, nil, nil)
	if r.codice != 200 {
		t.Fatalf("vuota: %d", r.codice)
	}
	contiene(t, "vuota", r.corpo, "<title>Storico lotti · ",
		`<nav class="gs-back-bar" aria-label="Ritorno"><a id="ready-lot-history-back" href="/ready-lots" class="gs-back-btn">← Indietro</a></nav>`+"\n"+
			`<div>`+"\n"+`<h1>📦 Storico lotti</h1>`+"\n"+`<p class="sotto">I lotti già inviati, dal più recente.</p>`+"\n"+`</div>`+"\n"+
			`<section id="ready-lot-history" class="riquadro">`+"\n"+`<p id="ready-lot-history-empty" class="sotto">Nessun lotto inviato.</p>`+"\n"+
			`<ul class="consegne-elenco">`+"\n"+`</ul>`+"\n"+`</section>`)
	aperto := lottoConOreficeria(t, d)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Beta <Oro>')`)
	inviato := lottoProva(t, d, 2, "2026-10-31T08:10:00.000000Z")
	chiuso := lottoProva(t, d, 3, "2026-10-31T08:20:00.000000Z")
	var ids []int64
	for _, s := range []string{"pronta", "consegnata", "pronta", "pronta"} {
		id := prontaDelLotto(t, d, o, "", "Anello")
		eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ?, stato = ? WHERE id = ?`, inviato, s, id)
		ids = append(ids, id)
	}
	aperta := prontaDelLotto(t, d, o, "", "Anello")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, aperto, aperta)
	eseguiSQL(t, d, `UPDATE buste SET cancellata_il = '2026-10-31T08:00:00.000000Z' WHERE id = ?`, ids[3])
	eseguiSQL(t, d, `UPDATE lotti_avviso SET inviato_il = '2026-10-31T08:30:00.000000Z' WHERE id = ?`, inviato)
	eseguiSQL(t, d, `UPDATE lotti_avviso SET inviato_il = '2026-07-15T22:30:00.000000Z', chiuso_il = '2026-12-15T08:05:00.000000Z' WHERE id = ?`, chiuso)
	r = b.vai("GET", rottaStoricoLottiProva, nil, nil)
	contiene(t, "lotti", r.corpo,
		`<section id="ready-lot-history" class="riquadro">`+"\n"+`<ul class="consegne-elenco">`+"\n"+
			`<li id="ready-lot-history-`+itoa(chiuso)+`" class="lotti-riga"><div><strong>Beta &lt;Oro&gt;</strong> <span class="sotto-in-linea">— lotto #`+itoa(chiuso)+` · 0 buste · inviato 16/07/2026 00:30</span>`+"\n"+
			`<span id="ready-lot-history-`+itoa(chiuso)+`-closed" class="lotto-completato">Completato il 15/12/2026 09:05</span></div> <a href="/ready-lots/history/`+itoa(chiuso)+`" class="pulsante-contorno">Apri</a></li>`+"\n"+
			`<li id="ready-lot-history-`+itoa(inviato)+`" class="lotti-riga"><div><strong>Alfa Orafi</strong> <span class="sotto-in-linea">— lotto #`+itoa(inviato)+` · 3 buste · inviato 31/10/2026 09:30</span>`+"\n"+
			`<span id="ready-lot-history-`+itoa(inviato)+`-open" class="lotto-da-ritirare">2 da ritirare</span></div> <a href="/ready-lots/history/`+itoa(inviato)+`" class="pulsante-contorno">Apri</a></li>`+"\n"+
			`</ul>`+"\n"+`</section>`)
	nonContiene(t, "lotti", r.corpo, "ready-lot-history-empty", `id="ready-lot-history-`+itoa(aperto)+`"`, "15/07/2026 22:30")
	// Anche il titolare.
	tit, _ := dentroCome(t, accounts.Titolare, nil)
	if r := tit.vai("GET", rottaStoricoLottiProva, nil, nil); r.codice != 200 {
		t.Fatalf("titolare: %d", r.codice)
	}
}

// inventario: ORE-12
// Il dettaglio: il ritorno allo storico, «📦 Lotto <oreficeria>», «Lotto #N · inviato … · completato …», il link
// pubblico (lo stesso della pagina /lotto e dell'invio: indirizzo_pubblico + /lotto/<gettone>), le buste del lotto
// fuori dal cestino (anche passate a un'altra oreficeria) dalla più recente con il numero visibile (D-64), la
// descrizione oltre 60 grafemi tagliata, «Ritirata» e «Apri busta». Un lotto con il link spento all'importazione dice
// «Link storico non riattivato durante l'importazione.» al posto del link. Un lotto aperto, che non c'è o un id non
// valido torna allo storico con «Lotto non trovato nello storico.».
func TestPaginaLottoStorico(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	l := lottoConOreficeria(t, d)
	for _, p := range []string{percorsoStoricoLotto(l), percorsoStoricoLotto(9999), rottaStoricoLottiProva + "/abc", rottaStoricoLottiProva + "/0"} {
		r := b.vai("GET", p, nil, nil)
		if !redirectA(r, rottaStoricoLottiProva) {
			t.Fatalf("%s: %d %q", p, r.codice, r.location)
		}
		contiene(t, p, seguiRedirect(t, b, r).corpo, `<div role="alert" class="flash flash-errore">Lotto non trovato nello storico.</div>`)
	}
	uno := prontaDelLotto(t, d, o, "", "Uno")
	lunga := strings.Repeat("è", 61)
	due := prontaDelLotto(t, d, o, "RIP-2026-0045", lunga)
	cestino := prontaDelLotto(t, d, o, "", "Cestino")
	giusta := prontaDelLotto(t, d, o, "", strings.Repeat("ò", 60)) // esattamente 60 grafemi: resta intera
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id IN (?, ?, ?, ?)`, l, uno, due, cestino, giusta)
	eseguiSQL(t, d, `UPDATE buste SET stato = 'consegnata', oreficeria_id = NULL WHERE id = ?`, uno)
	eseguiSQL(t, d, `UPDATE buste SET cancellata_il = '2026-10-31T08:00:00.000000Z' WHERE id = ?`, cestino)
	eseguiSQL(t, d, `UPDATE lotti_avviso SET inviato_il = '2026-10-31T08:30:00.000000Z', token = ? WHERE id = ?`, gettoneLotto093, l)
	link := indirizzoPubblicoProva + "/lotto/" + gettoneLotto093
	r := b.vai("GET", percorsoStoricoLotto(l), nil, nil)
	if r.codice != 200 {
		t.Fatalf("dettaglio: %d", r.codice)
	}
	contiene(t, "dettaglio", r.corpo, "<title>Storico lotti · ",
		`<nav class="gs-back-bar" aria-label="Ritorno"><a id="ready-lot-history-back" href="/ready-lots/history" class="gs-back-btn">← Indietro</a></nav>`+"\n"+
			`<div>`+"\n"+`<h1>📦 Lotto Alfa Orafi</h1>`+"\n"+`<p class="sotto">Lotto #`+itoa(l)+` · inviato 31/10/2026 09:30</p>`+"\n"+
			`<p class="lotto-link">Link pubblico: <a id="ready-lot-history-link" href="`+link+`" class="lotto-link">`+link+`</a></p>`+"\n"+`</div>`,
		`<section id="ready-lot-history-bags" class="riquadro">`+"\n"+`<h2>Buste del lotto</h2>`+"\n"+`<ul class="consegne-elenco">`+"\n"+
			`<li id="ready-lot-history-bag-`+itoa(giusta)+`" class="consegna-busta"><div><strong>N. 4/26</strong> <span class="sotto-in-linea">— `+strings.Repeat("ò", 60)+`</span></div> <a href="/repairs/`+itoa(giusta)+`" class="pulsante-leggero">Apri busta</a></li>`+"\n"+
			`<li id="ready-lot-history-bag-`+itoa(due)+`" class="consegna-busta"><div><strong>#0045/26</strong> <span class="sotto-in-linea">— `+strings.Repeat("è", 60)+`…</span></div> <a href="/repairs/`+itoa(due)+`" class="pulsante-leggero">Apri busta</a></li>`+"\n"+
			`<li id="ready-lot-history-bag-`+itoa(uno)+`" class="consegna-busta"><div><strong>N. 1/26</strong> <span class="sotto-in-linea">— Uno</span> <span id="ready-lot-history-bag-`+itoa(uno)+`-collected" class="lotto-ritirata">Ritirata</span></div> <a href="/repairs/`+itoa(uno)+`" class="pulsante-leggero">Apri busta</a></li>`+"\n"+
			`</ul>`+"\n"+`</section>`)
	nonContiene(t, "dettaglio", r.corpo, "Cestino", "ready-lot-history-disabled", "ready-lot-history-bags-empty", "completato", "flash-errore")
	// Il link è quello della pagina del lotto e dell'invio.
	contiene(t, "pagina del lotto", b.vai("GET", percorsoLotto(l), nil, nil).corpo, `<a id="ready-lot-gone-link" href="`+link+`"`)
	// Chiuso: la data del completamento.
	eseguiSQL(t, d, `UPDATE lotti_avviso SET chiuso_il = '2026-11-02T16:45:00.000000Z' WHERE id = ?`, l)
	contiene(t, "chiuso", b.vai("GET", percorsoStoricoLotto(l), nil, nil).corpo,
		`<p class="sotto">Lotto #`+itoa(l)+` · inviato 31/10/2026 09:30 <span>· completato 02/11/2026 17:45</span></p>`)
	// Il link spento all'importazione.
	eseguiSQL(t, d, `UPDATE lotti_avviso SET chiuso_il = NULL, accesso_pubblico_spento_il = '2026-10-31T08:40:00.000000Z', token = 'disabled-import-7' WHERE id = ?`, l)
	r = b.vai("GET", percorsoStoricoLotto(l), nil, nil)
	contiene(t, "spento", r.corpo, `<p class="sotto">Lotto #`+itoa(l)+` · inviato 31/10/2026 09:30</p>`+"\n"+
		`<p id="ready-lot-history-disabled" class="sotto">Link storico non riattivato durante l&#39;importazione.</p>`+"\n"+`</div>`)
	nonContiene(t, "spento", r.corpo, "ready-lot-history-link", "disabled-import-7", "ready-lot-resend")
	// Nessuna busta.
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = NULL`)
	contiene(t, "vuoto", b.vai("GET", percorsoStoricoLotto(l), nil, nil).corpo,
		`<h2>Buste del lotto</h2>`+"\n"+`<p id="ready-lot-history-bags-empty" class="sotto">Nessuna busta.</p>`)
}

var reChiave093 = regexp.MustCompile(`name="reinvio" value="([A-Za-z0-9_-]{22})"`)

// chiaveReinvio è la chiave del messaggio preparato nella pagina (la stessa nei due moduli).
func chiaveReinvio(t *testing.T, corpo string) string {
	t.Helper()
	m := reChiave093.FindAllStringSubmatch(corpo, -1)
	if len(m) == 0 {
		t.Fatalf("nessuna chiave del reinvio:\n%s", corpo)
	}
	for _, x := range m {
		if x[1] != m[0][1] {
			t.Fatal("due chiavi diverse")
		}
	}
	return m[0][1]
}

// inventario: ORE-13
// Il reinvio: il riquadro «Reinvia all'oreficeria» (solo su un lotto inviato, non chiuso e con il link) con
// «Prepara di nuovo il messaggio»; prepararlo (200, «Messaggio pronto: …») mostra il testo con lo stesso link, «↗ Apri
// WhatsApp» e «👥 Scegli contatto» (moduli data-avviso-invio con la chiave del messaggio, D-74) e «Pronto, non ancora
// registrato…», e non scrive niente. Il tocco su «Apri WhatsApp» registra la riga all'oreficeria con il testo preparato
// e ready_lot.resent, poi wa.me (JSON con JavaScript, redirect senza); un secondo tocco sullo stesso messaggio
// riapre WhatsApp e non registra; un messaggio preparato di nuovo si registra di nuovo, e «Scegli contatto» registra il
// contatto scelto. Il lotto e gli avvisi delle buste non cambiano.
func TestReinvioLotto(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	l := lottoConOreficeria(t, d)
	uno := prontaDelLotto(t, d, o, "", "Uno")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ?, avviso_pronta_il = '2026-10-31T08:30:00.000000Z' WHERE id = ?`, l, uno)
	eseguiSQL(t, d, `UPDATE lotti_avviso SET inviato_il = '2026-10-31T08:30:00.000000Z', token = ? WHERE id = ?`, gettoneLotto093, l)
	link := indirizzoPubblicoProva + "/lotto/" + gettoneLotto093
	testo := "Buongiorno Alfa Orafi,\nho 1 riparazioni pronte da farvi ritirare.\nDettagli e foto: " + link + "\n\nNegozio"
	riquadro := func() string {
		return `<section id="ready-lot-resend" class="riquadro">` + "\n" + `<h2>Reinvia all&#39;oreficeria</h2>` + "\n" +
			`<p class="sotto">Stesso link del lotto, messaggio nuovo. Prepararlo non lo registra: finisce tra gli invii solo quando apri WhatsApp.</p>` + "\n" +
			`<form id="ready-lot-resend-prepare-form" action="/ready-lots/history/` + itoa(l) + `/prepara" method="post" data-invio-unico>` + "\n" +
			`<input name="_csrf_token" type="hidden" value="` + b.token + `">` + "\n" +
			`<button id="ready-lot-resend-prepare" type="submit">Prepara di nuovo il messaggio</button>` + "\n" + `</form>` + "\n"
	}
	r := b.vai("GET", percorsoStoricoLotto(l), nil, nil)
	contiene(t, "riquadro", r.corpo, riquadro()+`</section>`)
	nonContiene(t, "riquadro", r.corpo, "ready-lot-resend-message", "ready-lot-resend-opening", "ready-lot-resend-pending")
	if i, j := strings.Index(r.corpo, `id="ready-lot-resend"`), strings.Index(r.corpo, `id="ready-lot-history-bags"`); i < 0 || j < i {
		t.Fatal("il reinvio sta sopra le buste")
	}
	conta := func() string {
		return itoa(int64(contaRighe(t, d, `SELECT count(*) FROM messaggi_in_uscita`))) + "/" +
			itoa(int64(contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'ready_lot.resent'`)))
	}
	if r := b.vai("POST", percorsoStoricoLotto(l)+"/prepara", nil, nil); r.codice != 403 {
		t.Fatalf("senza token: %d", r.codice)
	}
	r = b.vai("POST", percorsoStoricoLotto(l)+"/prepara", modulo(b), nil)
	if r.codice != 200 {
		t.Fatalf("prepara: %d %q", r.codice, r.location)
	}
	k := chiaveReinvio(t, r.corpo)
	contiene(t, "preparato", r.corpo, `<div role="status" class="flash flash-info">`+msgPronto093+`</div>`,
		riquadro()+`<pre id="ready-lot-resend-message" class="lotto-testo">`+testo+`</pre>`+"\n"+
			`<div class="lotto-pulsanti"><form id="ready-lot-resend-opening-form" action="/ready-lots/history/`+itoa(l)+`/reinvia" method="post" data-channel="whatsapp" data-avviso-invio><input name="_csrf_token" type="hidden" value="`+b.token+`"><input type="hidden" name="reinvio" value="`+k+`"><input type="hidden" name="via" value="numero"><button id="ready-lot-resend-opening" type="submit" class="pulsante">↗ Apri WhatsApp</button></form>`+"\n"+
			`<form id="ready-lot-resend-choose-form" action="/ready-lots/history/`+itoa(l)+`/reinvia" method="post" data-channel="whatsapp" data-avviso-invio><input name="_csrf_token" type="hidden" value="`+b.token+`"><input type="hidden" name="reinvio" value="`+k+`"><input type="hidden" name="via" value="scegli"><button id="ready-lot-resend-choose" type="submit" class="pulsante-contorno"><span aria-hidden="true">👥</span> Scegli contatto</button></form>`+"\n"+
			`</div>`+"\n"+`<p id="ready-lot-resend-pending" class="sotto">Pronto, non ancora registrato: lo sarà quando apri WhatsApp.</p>`+"\n"+`</section>`)
	nonContiene(t, "preparato", r.corpo, "ready-lot-resend-no-number", "flash-errore")
	if c := conta(); c != "0/0" {
		t.Fatalf("prepara ha scritto: %s", c)
	}
	lotto := contaRighe(t, d, `SELECT count(*) FROM lotti_avviso WHERE token = '`+gettoneLotto093+`' AND inviato_il = '2026-10-31T08:30:00.000000Z' AND aggiornato_il = '2026-10-31T08:05:00.000000Z'`)
	// Il tocco su «Apri WhatsApp», con JavaScript.
	voluto := buste.IndirizzoWaMe("+390490000000", testo)
	json1 := func(via, chiave string) risposta {
		t.Helper()
		return b.vai("POST", percorsoStoricoLotto(l)+"/reinvia", modulo(b, "reinvio", chiave, "via", via), map[string]string{"Accept": "application/json"})
	}
	urlDi := func(r risposta) string {
		t.Helper()
		var j map[string]string
		if err := json.Unmarshal([]byte(r.corpo), &j); err != nil || len(j) != 1 || r.codice != 200 ||
			r.h.Get("Cache-Control") != "no-store" || r.h.Get("Content-Type") != "application/json" {
			t.Fatalf("JSON: %d %s %v %v", r.codice, r.corpo, r.h, err)
		}
		return j["url"]
	}
	if u := urlDi(json1("numero", k)); u != voluto {
		t.Fatalf("url: %s", u)
	}
	if c := conta(); c != "1/1" {
		t.Fatalf("tocco: %s", c)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE busta_id IS NULL AND destinatario = 'oreficeria' AND indirizzo = '+390490000000' AND stato = 'accepted'`); n != 1 {
		t.Fatal("riga", n)
	}
	if v := contaRighe(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE testo = '`+strings.ReplaceAll(testo, "'", "''")+`'`); v != 1 {
		t.Fatal("testo della riga")
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'ready_lot.resent' AND attore = 'banco@esempio.it' AND id_risorsa = '`+itoa(l)+`' AND dati LIKE '{"bags":1,"message_id":%'`); n != 1 {
		t.Fatal("audit", n)
	}
	// Un secondo tocco: WhatsApp di nuovo, niente registrato; anche senza JavaScript e da «Scegli contatto».
	if u := urlDi(json1("numero", k)); u != voluto {
		t.Fatalf("secondo tocco: %s", u)
	}
	if r := b.vai("POST", percorsoStoricoLotto(l)+"/reinvia", modulo(b, "reinvio", k, "via", "numero"), nil); r.codice != 302 || r.location != voluto {
		t.Fatalf("secondo tocco senza JavaScript: %d %s", r.codice, r.location)
	}
	if u := urlDi(json1("scegli", k)); u != buste.IndirizzoWaMe("", testo) {
		t.Fatalf("secondo tocco da Scegli: %s", u)
	}
	if c := conta(); c != "1/1" {
		t.Fatalf("secondo tocco ha scritto: %s", c)
	}
	if contaRighe(t, d, `SELECT count(*) FROM lotti_avviso WHERE token = '`+gettoneLotto093+`' AND inviato_il = '2026-10-31T08:30:00.000000Z' AND aggiornato_il = '2026-10-31T08:05:00.000000Z'`) != lotto ||
		contaRighe(t, d, `SELECT count(*) FROM buste WHERE avviso_pronta_il = '2026-10-31T08:30:00.000000Z'`) != 1 {
		t.Fatal("lotto o avvisi cambiati")
	}
	// Preparato di nuovo: «Scegli contatto» senza JavaScript registra il contatto scelto e va a wa.me/?text=.
	k2 := chiaveReinvio(t, b.vai("POST", percorsoStoricoLotto(l)+"/prepara", modulo(b), nil).corpo)
	if k2 == k {
		t.Fatal("stessa chiave")
	}
	if r := b.vai("POST", percorsoStoricoLotto(l)+"/reinvia", modulo(b, "reinvio", k2, "via", "scegli"), nil); r.codice != 302 || r.location != buste.IndirizzoWaMe("", testo) {
		t.Fatalf("scegli: %d %s", r.codice, r.location)
	}
	if c := conta(); c != "2/2" {
		t.Fatalf("scegli: %s", c)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE indirizzo = 'contatto-scelto' AND busta_id IS NULL`); n != 1 {
		t.Fatal("contatto scelto", n)
	}
	// La riaperta della pagina non ha più il testo.
	nonContiene(t, "riaperta", b.vai("GET", percorsoStoricoLotto(l), nil, nil).corpo, "ready-lot-resend-message", "ready-lot-resend-pending")
}

// inventario: ORE-13
// Un'oreficeria senza numero: «Prepara» mostra solo «👥 Scegli contatto» e l'avviso «Nessun numero WhatsApp …»; il
// tocco registra il contatto scelto.
func TestReinvioSenzaNumero(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	l := lottoConOreficeria(t, d)
	eseguiSQL(t, d, `UPDATE oreficerie SET telefono = NULL WHERE id = 2`)
	uno := prontaDelLotto(t, d, o, "", "Uno")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, l, uno)
	eseguiSQL(t, d, `UPDATE lotti_avviso SET inviato_il = '2026-10-31T08:30:00.000000Z', token = ? WHERE id = ?`, gettoneLotto093, l)
	b.vai("GET", percorsoStoricoLotto(l), nil, nil)
	r := b.vai("POST", percorsoStoricoLotto(l)+"/prepara", modulo(b), nil)
	k := chiaveReinvio(t, r.corpo)
	contiene(t, "senza numero", r.corpo, `<div class="lotto-pulsanti"><form id="ready-lot-resend-choose-form"`,
		`</div>`+"\n"+`<p id="ready-lot-resend-no-number" class="lotto-avviso">Nessun numero WhatsApp per questa oreficeria: scegli tu il contatto. Salva il numero in Anagrafiche per la prossima volta.</p>`+"\n"+`<p id="ready-lot-resend-pending"`)
	nonContiene(t, "senza numero", r.corpo, "ready-lot-resend-opening", "Apri WhatsApp")
	// Anche un «numero» forgiato: il contatto scelto, wa.me/?text=.
	r = b.vai("POST", percorsoStoricoLotto(l)+"/reinvia", modulo(b, "reinvio", k, "via", "numero"), nil)
	if r.codice != 302 || !strings.HasPrefix(r.location, "https://wa.me/?text=Buongiorno%20Alfa%20Orafi") {
		t.Fatalf("tocco: %d %s", r.codice, r.location)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE indirizzo = 'contatto-scelto' AND busta_id IS NULL`); n != 1 {
		t.Fatal("contatto scelto", n)
	}
}

// inventario: ORE-13
// I rifiuti di «Prepara» ridisegnano il dettaglio (200) con il messaggio, senza il testo né i tocchi e senza scrivere:
// tutte le buste ritirate, nessuna busta ancora dell'oreficeria, un testo del lotto non valido, l'indirizzo pubblico che
// manca (D-46 estesa), un lotto chiuso nel frattempo («Lotto non disponibile.», senza più il riquadro). Un lotto che
// non è nello storico torna alla lista con «Lotto non trovato nello storico.».
func TestReinvioRifiuti(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	l := lottoConOreficeria(t, d)
	uno := prontaDelLotto(t, d, o, "", "Uno")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, l, uno)
	eseguiSQL(t, d, `UPDATE lotti_avviso SET inviato_il = '2026-10-31T08:30:00.000000Z', token = ? WHERE id = ?`, gettoneLotto093, l)
	b.vai("GET", percorsoStoricoLotto(l), nil, nil)
	prepara := func(cosa, errore string, riquadro bool) {
		t.Helper()
		r := b.vai("POST", percorsoStoricoLotto(l)+"/prepara", modulo(b), nil)
		if r.codice != 200 {
			t.Fatalf("%s: %d %q", cosa, r.codice, r.location)
		}
		contiene(t, cosa, r.corpo, `<div role="alert" class="flash flash-errore">`+errore+`</div>`, `<h1>📦 Lotto Alfa Orafi</h1>`)
		nonContiene(t, cosa, r.corpo, "ready-lot-resend-message", "ready-lot-resend-choose", "flash-info")
		if strings.Contains(r.corpo, `id="ready-lot-resend"`) != riquadro {
			t.Fatalf("%s: riquadro %v", cosa, !riquadro)
		}
	}
	eseguiSQL(t, d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, uno)
	prepara("ritirate", "Tutte le buste del lotto sono state ritirate.", true)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Beta')`)
	eseguiSQL(t, d, `UPDATE buste SET stato = 'pronta', oreficeria_id = 3 WHERE id = ?`, uno)
	prepara("spostate", "Nessuna busta del lotto è ancora di questa oreficeria: niente da reinviare.", true)
	eseguiSQL(t, d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, uno)
	eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.template.wa_oreficeria_lotto', 'Lotto {password}', '2026-10-31T09:00:00.000000Z')`)
	prepara("testo", "Il testo «Avviso lotto pronte oreficeria» non è valido: Segnaposto non supportato: {password}. Niente è partito.", true)
	eseguiSQL(t, d, `DELETE FROM impostazioni WHERE chiave = 'outgoing.template.wa_oreficeria_lotto'`)
	eseguiSQL(t, d, `UPDATE lotti_avviso SET chiuso_il = '2026-10-31T08:50:00.000000Z' WHERE id = ?`, l)
	prepara("chiuso", "Lotto non disponibile.", false)
	if n := contaRighe(t, d, `SELECT count(*) FROM messaggi_in_uscita`) + contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'ready_lot.%'`); n != 0 {
		t.Fatalf("un rifiuto ha scritto: %d", n)
	}
	aperto := lottoProva(t, d, 2, "2026-10-31T08:40:00.000000Z")
	for _, p := range []string{percorsoStoricoLotto(aperto), percorsoStoricoLotto(9999), rottaStoricoLottiProva + "/abc"} {
		for _, azione := range []string{"/prepara", "/reinvia"} {
			r := b.vai("POST", p+azione, modulo(b, "reinvio", "x", "via", "numero"), nil)
			if !redirectA(r, rottaStoricoLottiProva) {
				t.Fatalf("%s%s: %d %q", p, azione, r.codice, r.location)
			}
			contiene(t, p, seguiRedirect(t, b, r).corpo, "Lotto non trovato nello storico.")
		}
	}
}

// inventario: ORE-13
// Senza indirizzo pubblico (D-46 estesa ai testi con {link}): il messaggio non si prepara e la pagina dice cosa
// configurare, con la frase del popup (D-46: «Messaggio non inviato: manca …»); il dettaglio mostra il link come solo percorso, come la pagina del lotto.
func TestReinvioSenzaIndirizzoPubblico(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, "")
	b := dipendenteDentro(t, h)
	l := lottoConOreficeria(t, d)
	uno := prontaDelLotto(t, d, o, "", "Uno")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, l, uno)
	eseguiSQL(t, d, `UPDATE lotti_avviso SET inviato_il = '2026-10-31T08:30:00.000000Z', token = ? WHERE id = ?`, gettoneLotto093, l)
	contiene(t, "dettaglio", b.vai("GET", percorsoStoricoLotto(l), nil, nil).corpo,
		`Link pubblico: <a id="ready-lot-history-link" href="/lotto/`+gettoneLotto093+`" class="lotto-link">/lotto/`+gettoneLotto093+`</a>`)
	b.vai("GET", percorsoStoricoLotto(l), nil, nil)
	r := b.vai("POST", percorsoStoricoLotto(l)+"/prepara", modulo(b), nil)
	contiene(t, "senza indirizzo", r.corpo, `<div role="alert" class="flash flash-errore">Messaggio non inviato: manca «indirizzo_pubblico = https://host[:porta]» in `,
		`».</div>`, `id="ready-lot-resend-prepare"`)
	nonContiene(t, "senza indirizzo", r.corpo, "ready-lot-resend-message", "Niente è partito")
}

// inventario: ORE-13
// Il tocco senza un messaggio preparato in questa sessione (chiave inventata, di un'altra sessione, persa con un
// riavvio, scaduta) non registra niente e chiede di preparare di nuovo (D-nuova, da approvare); un rifiuto al tocco
// (il lotto completato fra la preparazione e il tocco) ridisegna il dettaglio con il messaggio e consuma la chiave.
func TestReinvioToccoSenzaMessaggio(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	l := lottoConOreficeria(t, d)
	uno := prontaDelLotto(t, d, o, "", "Uno")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, l, uno)
	eseguiSQL(t, d, `UPDATE lotti_avviso SET inviato_il = '2026-10-31T08:30:00.000000Z', token = ? WHERE id = ?`, gettoneLotto093, l)
	b.vai("GET", percorsoStoricoLotto(l), nil, nil)
	k := chiaveReinvio(t, b.vai("POST", percorsoStoricoLotto(l)+"/prepara", modulo(b), nil).corpo)
	senza := func(cosa string, br *browser, chiave string) {
		t.Helper()
		r := br.vai("POST", percorsoStoricoLotto(l)+"/reinvia", modulo(br, "reinvio", chiave, "via", "numero"), map[string]string{"Accept": "application/json"})
		if r.codice != 200 || !strings.HasPrefix(r.h.Get("Content-Type"), "text/html") {
			t.Fatalf("%s: %d %s", cosa, r.codice, r.h.Get("Content-Type"))
		}
		contiene(t, cosa, r.corpo, `<div role="alert" class="flash flash-errore">Prepara di nuovo il messaggio: niente è stato registrato.</div>`, `id="ready-lot-resend-prepare"`)
		nonContiene(t, cosa, r.corpo, "ready-lot-resend-message")
	}
	senza("inventata", b, "AAAAAAAAAAAAAAAAAAAAAA")
	senza("vuota", b, "")
	altro := dipendenteDentro(t, h) // un altro accesso dello stesso utente: un'altra sessione
	altro.vai("GET", percorsoStoricoLotto(l), nil, nil)
	senza("altra sessione", altro, k)
	if n := contaRighe(t, d, `SELECT count(*) FROM messaggi_in_uscita`); n != 0 {
		t.Fatal("scritto", n)
	}
	// Il lotto completato fra «Prepara» e il tocco: il rifiuto, poi la chiave non vale più.
	eseguiSQL(t, d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, uno)
	r := b.vai("POST", percorsoStoricoLotto(l)+"/reinvia", modulo(b, "reinvio", k, "via", "numero"), map[string]string{"Accept": "application/json"})
	if r.codice != 200 {
		t.Fatalf("rifiuto: %d", r.codice)
	}
	contiene(t, "rifiuto", r.corpo, `<div role="alert" class="flash flash-errore">Tutte le buste del lotto sono state ritirate.</div>`)
	eseguiSQL(t, d, `UPDATE buste SET stato = 'pronta' WHERE id = ?`, uno)
	senza("dopo il rifiuto", b, k)
	if n := contaRighe(t, d, `SELECT count(*) FROM messaggi_in_uscita`); n != 0 {
		t.Fatal("scritto", n)
	}
}

// inventario: ORE-13
// Il messaggio preparato scade (depositoReinvii): dopo la durata il tocco chiede di preparare di nuovo.
func TestDepositoReinviiScade(t *testing.T) {
	o := clock.NuovoFinto(time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
	dr := nuovoDepositoReinvii(o)
	k := dr.metti(5, "nonce", 7, buste.ReinvioLotto{Messaggio: "Testo"})
	if _, stato := dr.prendi(k, 5, "nonce", 8); stato != reinvioAssente {
		t.Fatal("altro lotto")
	}
	if _, stato := dr.prendi(k, 6, "nonce", 7); stato != reinvioAssente {
		t.Fatal("altro utente")
	}
	o.Sposta(durataReinvio)
	if _, stato := dr.prendi(k, 5, "nonce", 7); stato != reinvioAssente {
		t.Fatal("scaduto")
	}
}

// inventario: ORE-13
// Due tocchi insieme sullo stesso messaggio: uno solo lo registra (il secondo tocco non registra di nuovo).
func TestDepositoReinviiUnaVolta(t *testing.T) {
	dr := nuovoDepositoReinvii(clock.NuovoFinto(time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)))
	k := dr.metti(5, "nonce", 7, buste.ReinvioLotto{Messaggio: "Testo"})
	var wg sync.WaitGroup
	var mu sync.Mutex
	primi, gia := 0, 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			x, stato := dr.prendi(k, 5, "nonce", 7)
			mu.Lock()
			defer mu.Unlock()
			switch stato {
			case reinvioDaRegistrare:
				primi++
			case reinvioGiaRegistrato:
				gia++
			}
			if x.Messaggio != "Testo" {
				t.Error("messaggio")
			}
		}()
	}
	wg.Wait()
	if primi != 1 || gia != 19 {
		t.Fatalf("primi %d, già %d", primi, gia)
	}
	// Un rifiuto toglie la chiave.
	dr.togli(k)
	if _, stato := dr.prendi(k, 5, "nonce", 7); stato != reinvioAssente {
		t.Fatal("tolta")
	}
}

// inventario: ORE-13
// Il deposito pieno: con 500 messaggi preparati il 501° fa uscire quello che scade prima (mai quello che scade per
// ultimo); i messaggi scaduti si tolgono al metti dopo, prima di sfrattare.
func TestDepositoReinviiPieno(t *testing.T) {
	o := clock.NuovoFinto(time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
	dr := nuovoDepositoReinvii(o)
	var kk []string
	for i := 0; i < maxReinvii; i++ {
		kk = append(kk, dr.metti(5, "nonce", 7, buste.ReinvioLotto{Messaggio: "Testo"}))
		o.Sposta(time.Second)
	}
	if len(dr.voci) != maxReinvii || maxReinvii != 500 {
		t.Fatalf("voci: %d, tetto %d", len(dr.voci), maxReinvii)
	}
	ultima := dr.metti(5, "nonce", 7, buste.ReinvioLotto{Messaggio: "Testo"})
	if len(dr.voci) != 500 {
		t.Fatalf("dopo la 501ª: %d", len(dr.voci))
	}
	if _, ok := dr.voci[kk[0]]; ok {
		t.Fatal("la prima a scadere è rimasta")
	}
	for _, k := range []string{kk[1], kk[499], ultima} {
		if _, ok := dr.voci[k]; !ok {
			t.Fatal("sfrattata la voce sbagliata")
		}
	}
	// Scadono la seconda, la terza e la quarta: il metti dopo le toglie, senza sfrattare altro.
	o.Imposta(time.Date(2026, 10, 31, 9, 0, 3, 0, time.UTC).Add(durataReinvio))
	dr.metti(5, "nonce", 7, buste.ReinvioLotto{Messaggio: "Testo"})
	if len(dr.voci) != 498 {
		t.Fatalf("dopo le scadute: %d", len(dr.voci))
	}
	if _, ok := dr.voci[kk[4]]; !ok {
		t.Fatal("tolta una voce non scaduta")
	}
}

// inventario: ORE-13
// Un «via» vuoto o strano è il numero, come nello standalone (solo «scegli» è il contatto scelto): la riga ha il numero
// e WhatsApp si apre sulla stessa chat.
func TestReinvioViaNonScegli(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	l := lottoConOreficeria(t, d)
	uno := prontaDelLotto(t, d, o, "", "Uno")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, l, uno)
	eseguiSQL(t, d, `UPDATE lotti_avviso SET inviato_il = '2026-10-31T08:30:00.000000Z', token = ? WHERE id = ?`, gettoneLotto093, l)
	b.vai("GET", percorsoStoricoLotto(l), nil, nil)
	for i, via := range []string{"", "strano"} {
		k := chiaveReinvio(t, b.vai("POST", percorsoStoricoLotto(l)+"/prepara", modulo(b), nil).corpo)
		r := b.vai("POST", percorsoStoricoLotto(l)+"/reinvia", modulo(b, "reinvio", k, "via", via), nil)
		if r.codice != 302 || !strings.HasPrefix(r.location, "https://wa.me/%2B390490000000?text=") {
			t.Fatalf("via %q: %d %s", via, r.codice, r.location)
		}
		if n := contaRighe(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE indirizzo = '+390490000000' AND busta_id IS NULL`); n != i+1 {
			t.Fatalf("via %q: righe %d", via, n)
		}
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE indirizzo = 'contatto-scelto'`); n != 0 {
		t.Fatal("contatto scelto", n)
	}
}
