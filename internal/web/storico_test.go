package web

import (
	"net/url"
	"strings"
	"testing"
)

// Lo storico delle consegne (storico.go, slice 082, ART-15…18) e il riquadro dei giri sulla scheda (ART-19,
// RIP-S-17), come HandoverHistoryLive, HandoverHistoryController e #repair-artisan-cycles di RepairLive
// (302439d): i casi di handover_history_live_test.exs e handover_history_controller_test.exs, senza LiveView,
// con la D-64 per il numero. Nomi e numeri inventati; l'orologio è fermo al 31/10/2026, le 10:00 in negozio.

const rottaStoricoProva = "/handovers/history"

func percorsoStorico(id int64) string { return rottaStoricoProva + "/" + itoa(id) }

// seguiRedirect segue i redirect di r (al più tre) e dà la pagina dove finiscono.
func seguiRedirect(t *testing.T, b *browser, r risposta) risposta {
	t.Helper()
	for i := 0; i < 3 && r.codice == 302; i++ {
		u, err := url.Parse(r.location)
		if err != nil {
			t.Fatal(err)
		}
		r = b.vai("GET", u.RequestURI(), nil, nil)
	}
	if r.codice != 200 {
		t.Fatalf("dopo i redirect: %d %s", r.codice, r.location)
	}
	return r
}

// storicoDiProva scrive due artigiani, una consegna chiusa (con due buste rientrate, una nel cestino e una
// riaperta altrove), una in corso (una busta fuori) e una in raccolta; dà le consegne e le buste.
type storicoDiProva struct {
	chiusa, inCorso, inRaccolta int64
	a, b, fuori, riaperta       int64
}

func scriviStorico(t *testing.T, h *provaRientro) storicoDiProva {
	t.Helper()
	eseguiSQL(t, h.d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Orafo <Uno>'), (2, 'Bottega Due')`)
	var s storicoDiProva
	s.chiusa = consegnaProva(t, h.d, 1, "2026-10-20T08:00:00.000000Z", "2026-10-22T09:30:00.000000Z")
	s.inCorso = consegnaProva(t, h.d, 2, "2026-10-29T23:00:00.000000Z", "")
	s.inRaccolta = consegnaProva(t, h.d, 1, "2026-10-31T07:00:00.000000Z", "")
	s.a, _ = bustaConsegna(t, h.d, h.o, "RIP-2026-0046", "Anello con una descrizione molto lunga che supera di parecchio i cinquantacinque caratteri")
	s.b, _ = bustaConsegna(t, h.d, h.o, "RIP-2026-0045", "Collana")
	cestino, _ := bustaConsegna(t, h.d, h.o, "", "Nel cestino")
	s.fuori, _ = bustaConsegna(t, h.d, h.o, "", "Bracciale")
	s.riaperta, _ = bustaConsegna(t, h.d, h.o, "RIP-2026-0047", "Spilla")
	eseguiSQL(t, h.d, `UPDATE buste SET consegna_artigiano_id = ?, artigiano_id = 1, consegnata_artigiano_il = '2026-10-20T09:00:00.000000Z',
		rientrata_artigiano_il = '2026-10-22T09:30:00.000000Z' WHERE id IN (?, ?, ?)`, s.chiusa, s.a, s.b, cestino)
	eseguiSQL(t, h.d, `UPDATE buste SET note_artigiano = 'Pietra <rotta>' WHERE id = ?`, s.a)
	eseguiSQL(t, h.d, `UPDATE buste SET cancellata_il = '2026-10-30T08:00:00.000000Z' WHERE id = ?`, cestino)
	eseguiSQL(t, h.d, `UPDATE buste SET consegna_artigiano_id = ?, artigiano_id = 2, consegnata_artigiano_il = '2026-10-30T08:00:00.000000Z' WHERE id = ?`, s.inCorso, s.fuori)
	eseguiSQL(t, h.d, `UPDATE buste SET consegna_artigiano_id = ?, artigiano_id = 2, consegnata_artigiano_il = '2026-10-30T08:00:00.000000Z' WHERE id = ?`, s.inCorso, s.riaperta)
	// La spilla era della consegna chiusa (un giro passato con la nota) e ora è nella consegna in corso.
	eseguiSQL(t, h.d, `INSERT INTO giri_artigiani (busta_id, consegna_artigiano_id, uscita_il, rientro_il, nota_presente) VALUES
		(?, ?, '2026-10-20T09:00:00.000000Z', '2026-10-21T15:05:00.000000Z', 1)`, s.riaperta, s.chiusa)
	return s
}

// inventario: ART-15
// L'elenco per il personale: ritorno alle consegne, il modulo GET dei filtri (artigiani per nome, stato, date),
// le consegne dalla più recente con il giorno d'apertura, l'artigiano, lo stato («chiusa», «in corso», «in
// raccolta»), le buste fuori dal cestino, la chiusura e «N ancora fuori»; «✕ Elimina dallo storico» solo al
// titolare e solo sulle chiuse, con la domanda. Ogni visita scrive handover.history_viewed con i soli nomi dei
// filtri; vuoto «Nessuna consegna trovata.».
func TestElencoStorico(t *testing.T) {
	h := nuovaProvaBuste(t)
	dip := dipendenteDentro(t, h.h)
	r := dip.vai("GET", rottaStoricoProva, nil, nil)
	if r.codice != 200 {
		t.Fatalf("vuoto: %d", r.codice)
	}
	contiene(t, "vuoto", r.corpo, "<title>Storico consegne", `<a id="handover-history-back" href="/handovers" class="gs-back-btn">← Indietro</a>`,
		`<h1>📋 Storico consegne</h1>`, `<form id="handover-history-filter" method="get" action="/handovers/history" class="riquadro storico-filtri">`,
		`<select id="handover-history-artisan" name="artigiano">`+"\n"+`<option value="">— tutti —</option>`+"\n"+`</select>`,
		`<select id="handover-history-state" name="stato">`+"\n"+`<option value="">— tutti —</option>`+"\n"+`<option value="aperte">Aperte</option>`+"\n"+`<option value="chiuse">Chiuse</option>`+"\n"+`</select>`,
		`<input type="date" name="data_da" value="">`, `<input type="date" name="data_a" value="">`,
		`<button type="submit">Filtra</button> <a href="/handovers/history">× Azzera</a>`,
		`<p id="handover-history-empty" class="sotto">Nessuna consegna trovata.</p>`)
	nonContiene(t, "vuoto", r.corpo, "handover-history-prev", "handover-history-next")
	// Il link ai solleciti nella barra di ritorno compare da solo con la rotta della 083 (haRotta).
	contiene(t, "vuoto", r.corpo, `<nav class="gs-back-bar" aria-label="Ritorno"><a id="handover-history-back" href="/handovers" class="gs-back-btn">← Indietro</a> <a href="/handovers/reminders">⏰ Solleciti pendenti</a></nav>`)
	if got := ultimaRiga(t, h.d); got != `banco@esempio.it|handover.history_viewed|index|ok|{"filter_keys":[]}` {
		t.Fatal(got)
	}
	s := scriviStorico(t, h)
	tit := titolareDentro(t, h.h)
	for _, b := range []*browser{dip, tit} {
		r = b.vai("GET", rottaStoricoProva, nil, nil)
		contiene(t, "piena", r.corpo, `<option value="">— tutti —</option>`+"\n"+`<option value="2">Bottega Due</option>`+"\n"+`<option value="1">Orafo &lt;Uno&gt;</option>`+"\n"+`</select>`,
			`<li id="handover-history-`+itoa(s.inRaccolta)+`">`+"\n"+`<div class="storico-riga"><a href="/handovers/history/`+itoa(s.inRaccolta)+`"><strong>Consegna del 31/10/2026</strong></a> <span class="sotto-in-linea">Orafo &lt;Uno&gt;</span></div>`+"\n"+
				`<div class="sotto"><span class="handover-history-state stato-raccolta">in raccolta</span> 0 buste</div>`+"\n"+`</li>`,
			`<li id="handover-history-`+itoa(s.inCorso)+`">`+"\n"+`<div class="storico-riga"><a href="/handovers/history/`+itoa(s.inCorso)+`"><strong>Consegna del 30/10/2026</strong></a> <span class="sotto-in-linea">Bottega Due</span></div>`+"\n"+
				`<div class="sotto"><span class="handover-history-state stato-corso">in corso</span> 2 buste <strong class="ancora-fuori">· 2 ancora fuori</strong></div>`+"\n"+`</li>`,
			`<div class="sotto"><span class="handover-history-state stato-chiusa">chiusa</span> 2 buste · chiusa il 22/10/2026</div>`)
		if strings.Index(r.corpo, `id="handover-history-`+itoa(s.inRaccolta)+`"`) > strings.Index(r.corpo, `id="handover-history-`+itoa(s.chiusa)+`"`) {
			t.Fatal("dalla più recente")
		}
		nonContiene(t, "piena", r.corpo, "handover-history-empty", `id="handover-history-delete-`+itoa(s.inCorso)+`"`, `id="handover-history-delete-`+itoa(s.inRaccolta)+`"`)
	}
	elimina := `<form id="handover-history-delete-` + itoa(s.chiusa) + `" action="/handovers/history/` + itoa(s.chiusa) + `/delete" method="post" class="storico-elimina" data-invio-unico>` +
		`<input name="_csrf_token" type="hidden" value="` + tit.token + `"><button type="submit" class="pulsante-leggero testo-errore" title="Elimina consegna dallo storico" ` +
		`data-confirm="Eliminare questa consegna dallo storico? Le 2 buste verranno sganciate ma restano in archivio.">✕ Elimina dallo storico</button></form>`
	contiene(t, "titolare", r.corpo, elimina)
	nonContiene(t, "dipendente", dip.vai("GET", rottaStoricoProva, nil, nil).corpo, "handover-history-delete-")

	// I filtri: scelti nel modulo, i nomi nella riga d'audit (non i valori, non i vuoti, non le chiavi d'altro).
	r = dip.vai("GET", rottaStoricoProva+"?artigiano=1&stato=aperte&data_da=+2026-10-31+&data_a=&cerca=x&p=abc", nil, nil)
	contiene(t, "filtri", r.corpo, `<option value="1" selected>Orafo &lt;Uno&gt;</option>`, `<option value="aperte" selected>Aperte</option>`,
		`<input type="date" name="data_da" value="2026-10-31">`, `id="handover-history-`+itoa(s.inRaccolta)+`"`)
	nonContiene(t, "filtri", r.corpo, `id="handover-history-`+itoa(s.chiusa)+`"`, `id="handover-history-`+itoa(s.inCorso)+`"`)
	if got := ultimaRiga(t, h.d); got != `banco@esempio.it|handover.history_viewed|index|ok|{"filter_keys":["artigiano","data_da","p","stato"]}` {
		t.Fatal(got)
	}
	r = dip.vai("GET", rottaStoricoProva+"?stato[]=chiuse&artigiano=0", nil, nil)
	if got := ultimaRiga(t, h.d); got != `banco@esempio.it|handover.history_viewed|index|ok|{"filter_keys":["artigiano"]}` || !strings.Contains(r.corpo, `id="handover-history-`+itoa(s.inCorso)+`"`) {
		t.Fatal(got)
	}
}

// inventario: ART-15
// 30 consegne per pagina: «← Precedenti» e «Successive →» con i filtri, «Pagina N di M»; una pagina oltre la
// fine è l'ultima.
func TestPagineStorico(t *testing.T) {
	h := nuovaProvaBuste(t)
	eseguiSQL(t, h.d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Orafo')`)
	for i := range 31 {
		consegnaProva(t, h.d, 1, "2026-10-"+itoa(int64(10+i%20))+"T08:00:00.000000Z", "2026-10-31T08:00:00.000000Z")
	}
	b := dipendenteDentro(t, h.h)
	r := b.vai("GET", rottaStoricoProva+"?stato=chiuse&data_a=2026-10-31", nil, nil)
	contiene(t, "prima", r.corpo, `<span class="sotto-in-linea">Pagina 1 di 2</span> <a id="handover-history-next" href="/handovers/history?stato=chiuse&amp;data_a=2026-10-31&amp;p=2" class="pulsante-contorno">Successive →</a>`)
	nonContiene(t, "prima", r.corpo, "handover-history-prev")
	if n := strings.Count(r.corpo, `<li id="handover-history-`); n != 30 {
		t.Fatalf("righe: %d", n)
	}
	r = b.vai("GET", rottaStoricoProva+"?stato=chiuse&data_a=2026-10-31&p=9", nil, nil)
	contiene(t, "ultima", r.corpo, `<a id="handover-history-prev" href="/handovers/history?stato=chiuse&amp;data_a=2026-10-31&amp;p=1" class="pulsante-contorno">← Precedenti</a> <span class="sotto-in-linea">Pagina 2 di 2</span>`)
	nonContiene(t, "ultima", r.corpo, "handover-history-next")
	if n := strings.Count(r.corpo, `<li id="handover-history-`); n != 1 {
		t.Fatalf("righe: %d", n)
	}
}

// inventario: ART-16
// Il dettaglio: titolo, apertura e chiusura nell'ora del negozio, «🖨 Ristampa» e «📤 Condividi» solo con delle
// buste, le buste per numero (stato, descrizione tagliata a 50, uscita, rientro, nota), «✕ Sgancia» e
// «✕ Elimina dallo storico» solo al titolare su una consegna chiusa; le riaperte dalla tabella dei giri. Un id
// che non c'è torna all'elenco con «Consegna non trovata.», con la riga d'audit; un id illeggibile senza.
func TestDettaglioStorico(t *testing.T) {
	h := nuovaProvaBuste(t)
	s := scriviStorico(t, h)
	tit := titolareDentro(t, h.h)
	dip := dipendenteDentro(t, h.h)
	r := dip.vai("GET", percorsoStorico(s.chiusa), nil, nil)
	if r.codice != 200 {
		t.Fatalf("%d", r.codice)
	}
	c := itoa(s.chiusa)
	contiene(t, "chiusa", r.corpo, "<title>Consegna #"+c, `<a id="handover-history-back" href="/handovers/history" class="gs-back-btn">← Indietro</a>`,
		`<h1>Consegna #`+c+` — Orafo &lt;Uno&gt;</h1>`+"\n"+`<p class="sotto">Sessione aperta il 20/10/2026 10:00 · chiusa il 22/10/2026 11:30</p>`,
		`<a id="handover-history-reprint" href="/handovers/`+c+`/labels.pdf" target="_blank" rel="noopener" class="pulsante-contorno">🖨 Ristampa</a>`,
		`<a id="handover-history-share" href="/handovers/`+c+`/labels.pdf?download=1" data-share-pdf="/handovers/`+c+`/labels.pdf" class="pulsante-contorno">📤 Condividi</a>`,
		`<h2 class="consegna-titolo"><span>Buste</span> <span id="handover-history-bag-count" class="sotto-in-linea">2</span></h2>`,
		`<li id="handover-history-bag-`+itoa(s.a)+`">`+"\n"+`<div class="storico-riga"><a href="/repairs/`+itoa(s.a)+`"><strong>#0046/26</strong></a> <span>✓ rientrata</span></div>`+"\n"+
			`<div class="sotto">Anello con una descrizione molto lunga che supera…</div>`+"\n"+
			`<div class="sotto">Uscita 20/10/2026 11:00 · rientro 22/10/2026 11:30</div>`+"\n"+`<div class="storico-nota">📝 Pietra &lt;rotta&gt;</div>`,
		`<section id="handover-history-reopened" class="riquadro riquadro-grigio">`+"\n"+`<h2>Buste riaperte in un ciclo nuovo</h2>`,
		`<li id="handover-history-reopened-`+itoa(s.riaperta)+`-0">`+"\n"+`<div class="storico-riga"><a href="/repairs/`+itoa(s.riaperta)+`"><strong>#0047/26</strong></a> <span class="sotto-in-linea">📝 nota registrata</span></div>`+"\n"+
			`<div class="sotto">Spilla</div>`+"\n"+`<div class="sotto">Uscita 20/10/2026 11:00 · rientro 21/10/2026 17:05</div>`)
	if strings.Index(r.corpo, `handover-history-bag-`+itoa(s.a)) > strings.Index(r.corpo, `handover-history-bag-`+itoa(s.b)) {
		t.Fatal("per numero (interno) come testo")
	}
	nonContiene(t, "dipendente", r.corpo, "handover-history-detach-", "handover-history-delete-", "handover-history-bags-empty")
	if got := ultimaRiga(t, h.d); got != "banco@esempio.it|handover.history_detail_viewed|"+c+"|ok|{}" {
		t.Fatal(got)
	}
	r = tit.vai("GET", percorsoStorico(s.chiusa), nil, nil)
	contiene(t, "titolare", r.corpo, `id="handover-history-delete-`+c+`"`,
		`<form id="handover-history-detach-`+itoa(s.b)+`" action="/handovers/history/`+c+`/detach" method="post" class="storico-sgancia" data-invio-unico>`+
			`<input name="_csrf_token" type="hidden" value="`+tit.token+`"><input type="hidden" name="riparazione_id" value="`+itoa(s.b)+`">`+
			`<button type="submit" class="pulsante-leggero testo-errore" title="Sgancia busta dalla consegna" data-confirm="Sganciare la busta #0045/26 da questa consegna? La busta resta in archivio.">✕ Sgancia</button></form>`)
	// In corso: le buste fuori, niente correzioni neanche al titolare; in raccolta: nessuna busta né stampa.
	r = tit.vai("GET", percorsoStorico(s.inCorso), nil, nil)
	contiene(t, "in corso", r.corpo, `<p class="sotto">Sessione aperta il 30/10/2026 00:00</p>`, `<span>📤 fuori bottega</span>`, `Uscita 30/10/2026 09:00 · rientro —`)
	nonContiene(t, "in corso", r.corpo, "handover-history-detach-", "handover-history-delete-", "handover-history-reopened")
	r = tit.vai("GET", percorsoStorico(s.inRaccolta), nil, nil)
	contiene(t, "in raccolta", r.corpo, `<p id="handover-history-bags-empty" class="sotto">Nessuna busta in questa consegna.</p>`)
	nonContiene(t, "in raccolta", r.corpo, "handover-history-reprint", "handover-history-share")
	eseguiSQL(t, h.d, `UPDATE buste SET consegna_artigiano_id = ?, consegnata_artigiano_il = NULL, rientrata_artigiano_il = NULL WHERE id = ?`, s.inRaccolta, s.b)
	contiene(t, "busta in raccolta", tit.vai("GET", percorsoStorico(s.inRaccolta), nil, nil).corpo, `<span>in raccolta</span>`, `Uscita — · rientro —`)

	// Che non c'è: all'elenco con il messaggio, la riga d'audit scritta; illeggibile: nessuna riga.
	if r = dip.vai("GET", percorsoStorico(999), nil, nil); !redirectA(r, rottaStoricoProva) {
		t.Fatalf("999: %d %s", r.codice, r.location)
	}
	if got := ultimaRiga(t, h.d); got != "banco@esempio.it|handover.history_detail_viewed|999|ok|{}" {
		t.Fatal(got)
	}
	contiene(t, "non trovata", dip.vai("GET", rottaStoricoProva, nil, nil).corpo, `<div role="alert" class="flash flash-errore">Consegna non trovata.</div>`)
	prima := righeAudit(t, h.d, "handover.history_detail_viewed")
	for _, p := range []string{"abc", "0", "-1"} {
		if r = dip.vai("GET", rottaStoricoProva+"/"+p, nil, nil); !redirectA(r, rottaStoricoProva) {
			t.Fatalf("%s: %d", p, r.codice)
		}
	}
	if righeAudit(t, h.d, "handover.history_detail_viewed") != prima {
		t.Fatal("un id illeggibile non si registra")
	}
	// Un primo segmento che non è «history» è il 404 di una rotta che non c'è, anche per chi non è entrato
	// (prima della catena della rotta, come il router dello standalone).
	for _, b := range []*browser{dip, nuovoBrowser(t, h.h)} {
		if r = b.vai("GET", "/handovers/abc/1", nil, nil); r.codice != 404 {
			t.Fatalf("/handovers/abc/1: %d %s", r.codice, r.location)
		}
	}
	// /handovers/history/labels.pdf è il PDF della consegna «history»: il 404 dello standalone.
	if r = dip.vai("GET", rottaStoricoProva+"/labels.pdf", nil, nil); r.codice != 404 {
		t.Fatalf("labels.pdf: %d", r.codice)
	}
}

// inventario: ART-17, ART-18, ART-22
// Le due correzioni del titolare: «Elimina dallo storico» torna all'elenco con «Consegna #N eliminata dallo
// storico (N buste sganciate).», una consegna aperta con il rifiuto, una che non c'è con «Consegna non
// trovata.»; «Sgancia» torna al dettaglio con «Busta sganciata dalla consegna.» o il rifiuto, un id di busta
// illeggibile con «ID busta non valido.» (anche per una consegna che non c'è), una consegna illeggibile o che
// non c'è all'elenco. Il dipendente va a / con «Operazione riservata al titolare.» e niente cambia; senza
// token il 403.
func TestCorrezioniDelloStorico(t *testing.T) {
	h := nuovaProvaBuste(t)
	s := scriviStorico(t, h)
	tit := titolareDentro(t, h.h)
	dip := dipendenteDentro(t, h.h)
	c := itoa(s.chiusa)
	tit.vai("GET", percorsoStorico(s.chiusa), nil, nil)
	dip.vai("GET", percorsoStorico(s.chiusa), nil, nil)
	sgancia := func(b *browser, consegna, busta string) risposta {
		return b.vai("POST", rottaStoricoProva+"/"+consegna+"/detach", url.Values{"_csrf_token": {b.token}, "riparazione_id": {busta}}, nil)
	}
	elimina := func(b *browser, consegna string) risposta {
		return b.vai("POST", rottaStoricoProva+"/"+consegna+"/delete", url.Values{"_csrf_token": {b.token}}, nil)
	}
	// Il dipendente, e senza token.
	for _, r := range []risposta{sgancia(dip, c, itoa(s.b)), elimina(dip, c)} {
		if !redirectA(r, "/") {
			t.Fatalf("dipendente: %d %s", r.codice, r.location)
		}
	}
	contiene(t, "dipendente", dip.vai("GET", "/", nil, nil).corpo, "Operazione riservata al titolare.")
	if r := tit.vai("POST", rottaStoricoProva+"/"+c+"/delete", url.Values{}, nil); r.codice != 403 {
		t.Fatalf("senza token: %d", r.codice)
	}
	if contaRighe(t, h.d, `SELECT count(*) FROM consegne_artigiani WHERE id = `+c) != 1 || giroBusta(t, h.d, s.b) != c+"|si|1" ||
		righeAudit(t, h.d, "handover.bag_detached")+righeAudit(t, h.d, "handover.deleted") != 0 {
		t.Fatal("il dipendente ha cambiato qualcosa")
	}
	// Sgancia: i rifiuti.
	for _, x := range []struct{ consegna, busta, dove, msg string }{
		{c, "abc", percorsoStorico(s.chiusa), "ID busta non valido."},
		{"999", "", "/handovers/history/999", "Consegna non trovata."}, // il dettaglio che non c'è ripete il redirect
		{"abc", itoa(s.b), rottaStoricoProva, "Consegna non trovata."},
		{"999", itoa(s.b), rottaStoricoProva, "Consegna non trovata."},
		{itoa(s.inCorso), itoa(s.fuori), percorsoStorico(s.inCorso), "Solo le consegne chiuse permettono questa rimozione."},
		{c, itoa(s.fuori), percorsoStorico(s.chiusa), "Busta non trovata in questa consegna."},
	} {
		r := sgancia(tit, x.consegna, x.busta)
		if !redirectA(r, x.dove) {
			t.Fatalf("%+v: %d %s", x, r.codice, r.location)
		}
		contiene(t, x.msg, seguiRedirect(t, tit, r).corpo, `<div role="alert" class="flash flash-errore">`+x.msg+`</div>`)
	}
	if r := sgancia(tit, c, itoa(s.b)); !redirectA(r, percorsoStorico(s.chiusa)) {
		t.Fatalf("sgancia: %d %s", r.codice, r.location)
	}
	r := tit.vai("GET", percorsoStorico(s.chiusa), nil, nil)
	contiene(t, "sganciata", r.corpo, `<div role="status" class="flash flash-info">Busta sganciata dalla consegna.</div>`, `id="handover-history-bag-count" class="sotto-in-linea">1</span>`)
	if giroBusta(t, h.d, s.b) != "-|si|1" || righeAudit(t, h.d, "handover.bag_detached") != 1 {
		t.Fatal("la busta non è sganciata")
	}
	// Elimina: i rifiuti, poi la consegna chiusa (la busta nel cestino non si conta).
	for _, x := range []struct{ consegna, msg string }{
		{itoa(s.inCorso), "Solo le consegne chiuse possono essere eliminate dallo storico."},
		{"999", "Consegna non trovata."},
		{"abc", "Consegna non trovata."},
	} {
		r := elimina(tit, x.consegna)
		if !redirectA(r, rottaStoricoProva) {
			t.Fatalf("%+v: %d %s", x, r.codice, r.location)
		}
		contiene(t, x.msg, seguiRedirect(t, tit, r).corpo, `<div role="alert" class="flash flash-errore">`+x.msg+`</div>`)
	}
	if r := elimina(tit, c); !redirectA(r, rottaStoricoProva) {
		t.Fatalf("elimina: %d %s", r.codice, r.location)
	}
	r = tit.vai("GET", rottaStoricoProva, nil, nil)
	contiene(t, "eliminata", r.corpo, `<div role="status" class="flash flash-info">Consegna #`+c+` eliminata dallo storico (1 buste sganciate).</div>`)
	nonContiene(t, "eliminata", r.corpo, `id="handover-history-`+c+`"`)
	if giroBusta(t, h.d, s.a) != "-|si|1" || contaRighe(t, h.d, `SELECT count(*) FROM buste WHERE consegna_artigiano_id = `+c) != 0 {
		t.Fatal("buste non sganciate")
	}
}

// inventario: ART-19, RIP-S-17
// Il riquadro «🔁 Cicli dall'artigiano» della scheda, solo con un giro passato: la consegna («Consegna #N» o
// «Consegna senza sessione») e l'artigiano («artigiano» senza), «ciclo corrente» sul giro delle colonne,
// uscita e rientro nell'ora del negozio («—» se sconosciuti), «📝 nota registrata» solo su un giro passato.
func TestRiquadroGiriSullaScheda(t *testing.T) {
	h := nuovaProvaBuste(t)
	eseguiSQL(t, h.d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Orafo <Uno>')`)
	vecchia := consegnaProva(t, h.d, 1, "2026-10-01T08:00:00.000000Z", "2026-10-05T08:00:00.000000Z")
	attuale := consegnaProva(t, h.d, 1, "2026-10-20T08:00:00.000000Z", "")
	id := bustaFuori(t, h, attuale, "note_artigiano = 'Testo'")
	b := dipendenteDentro(t, h.h)
	nonContiene(t, "solo il giro attuale", b.vai("GET", percorsoScheda(id), nil, nil).corpo, "repair-artisan-cycles")
	eseguiSQL(t, h.d, `INSERT INTO giri_artigiani (busta_id, consegna_artigiano_id, uscita_il, rientro_il, nota_presente) VALUES
		(?, ?, '2026-10-01T09:00:00.000000Z', '2026-10-04T16:00:00.000000Z', 1), (?, NULL, NULL, '2026-10-06T08:00:00.000000Z', 0),
		(?, 77, NULL, '2026-10-07T08:00:00.000000Z', 0)`, id, vecchia, id, id)
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "giri", r.corpo, `<div id="repair-artisan-cycles" class="riquadro-giri">`+"\n"+`<strong>🔁 Cicli dall'artigiano</strong>`+"\n"+`<ul>`+"\n"+
		`<li><div class="giro-testa"><span class="forte">Consegna #`+itoa(attuale)+` — Orafo &lt;Uno&gt;</span> <span class="sotto-in-linea">ciclo corrente</span></div>`+"\n"+
		`<div class="sotto">Uscita 20/10/2026 10:00 · rientro —</div></li>`+"\n"+
		`<li><div class="giro-testa"><span class="forte">Consegna #77 — artigiano</span></div>`+"\n"+`<div class="sotto">Uscita — · rientro 07/10/2026 10:00</div></li>`+"\n"+
		`<li><div class="giro-testa"><span class="forte">Consegna senza sessione — artigiano</span></div>`+"\n"+`<div class="sotto">Uscita — · rientro 06/10/2026 10:00</div></li>`+"\n"+
		`<li><div class="giro-testa"><span class="forte">Consegna #`+itoa(vecchia)+` — Orafo &lt;Uno&gt;</span></div>`+"\n"+
		`<div class="sotto">Uscita 01/10/2026 11:00 · rientro 04/10/2026 18:00 · 📝 nota registrata</div></li>`+"\n"+`</ul>`+"\n"+`</div>`)
	if strings.Index(r.corpo, "repair-handed-over") > strings.Index(r.corpo, "repair-artisan-cycles") {
		t.Fatal("il riquadro va dopo quello della busta fuori")
	}
}
