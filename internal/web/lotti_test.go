package web

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/db"
)

// La lista dei lotti d'avviso alle oreficerie (lotti.go, slice 088), come ReadyLotsLive dello standalone (mount
// ed evento open): i casi di ready_lot_live_test.exs («the list page»), senza LiveView. Nomi inventati;
// l'orologio è fermo al 31/10/2026, le 10:00 in negozio (mezzanotte del negozio: il 30/10 alle 23:00 UTC).

const rottaLotti = "/ready-lots"

func lottoProva(t *testing.T, d *db.DB, oreficeria int64, creato string) int64 {
	t.Helper()
	res, err := d.Scrittura.Exec(`INSERT INTO lotti_avviso (oreficeria_id, token, creato_il, aggiornato_il) VALUES (?, ?, ?, ?)`,
		oreficeria, "gettone-prova-"+creato+"-"+itoa(oreficeria), creato, creato)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

var reLotto = regexp.MustCompile(`^/ready-lots/([0-9]+)$`)

// inventario: ORE-04
// La lista per il personale: titolo, frase, il menu delle oreficerie clienti per nome (senza la riga 1) con
// «Scegli un'oreficeria», «Apri lotto»; senza oreficerie «Nessuna oreficeria in anagrafica.» con il link
// all'anagrafica; «Lotti aperti oggi» con «Nessun lotto aperto oggi.» o le righe dal più recente (oreficeria,
// «lotto #N · 1 busta / N buste · aperto gg/mm/aaaa hh:mm» sull'orologio del negozio, «Apri»). Aprire la pagina
// cancella i lotti vuoti dei giorni prima. Il link allo storico arriva con la sua pagina (093).
func TestPaginaLotti(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	r := b.vai("GET", rottaLotti, nil, nil)
	if r.codice != 200 {
		t.Fatalf("vuota: %d", r.codice)
	}
	contiene(t, "vuota", r.corpo, "<title>Avviso oreficeria", `<h1>📦 Avviso oreficeria</h1>`+"\n"+
		`<p class="sotto">Un lotto per oreficeria, con le buste pronte da ritirare.</p>`,
		`<section id="ready-lot-open" class="riquadro">`+"\n"+
			`<form id="ready-lot-open-form" action="/ready-lots" method="post" class="consegne-nuova" data-invio-unico>`+"\n"+`<input name="_csrf_token" type="hidden" value="`+b.token+`">`,
		`<label for="ready-lot-goldsmith" class="lotti-oreficeria"><span>Oreficeria</span>`+"\n"+`<select id="ready-lot-goldsmith" name="lot[oreficeria_id]">`+"\n"+`<option value="">Scegli un&#39;oreficeria</option>`+"\n"+`</select></label>`+"\n"+`<button type="submit">Apri lotto</button>`+"\n"+`</form>`,
		`<p id="ready-lot-no-goldsmith" class="sotto">Nessuna oreficeria in anagrafica. <a href="/settings/anagrafiche">Aggiungine una</a>.</p>`,
		`<section id="ready-lot-list" class="riquadro">`+"\n"+`<h2>Lotti aperti oggi</h2>`+"\n"+`<p id="ready-lot-list-empty" class="sotto">Nessun lotto aperto oggi.</p>`)
	nonContiene(t, "vuota", r.corpo, "ready-lot-history-link", "Storico dei lotti inviati", `id="ready-lot-1"`, ">Negozio<")
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (2, 'Zeta <Oro>'), (3, 'Alfa Orafi'), (4, 'Beta')`)
	vuotoIeri := lottoProva(t, d, 4, "2026-10-29T08:00:00.000000Z")  // la pagina lo cancella
	pienoIeri := lottoProva(t, d, 4, "2026-10-30T22:59:59.999999Z")  // resta, ma non è di oggi
	mezzanotte := lottoProva(t, d, 3, "2026-10-30T23:00:00.000000Z") // di oggi: 00:00 in negozio
	recente := lottoProva(t, d, 2, "2026-10-31T08:05:00.000000Z")    // 09:05 in negozio
	inviato := lottoProva(t, d, 3, "2026-10-31T08:30:00.000000Z")    // inviato: non è in lista
	eseguiSQL(t, d, `UPDATE lotti_avviso SET inviato_il = ? WHERE id = ?`, "2026-10-31T08:40:00.000000Z", inviato)
	for _, l := range []int64{pienoIeri, recente} {
		id, _ := bustaConsegna(t, d, o, "", "Anello")
		eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, l, id)
	}
	cestinata, _ := bustaConsegna(t, d, o, "", "Collana")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ?, cancellata_il = ? WHERE id = ?`, mezzanotte, "2026-10-31T08:00:00.000000Z", cestinata)
	r = b.vai("GET", rottaLotti, nil, nil)
	contiene(t, "piena", r.corpo,
		`<option value="">Scegli un&#39;oreficeria</option>`+"\n"+`<option value="3">Alfa Orafi</option>`+"\n"+`<option value="4">Beta</option>`+"\n"+`<option value="2">Zeta &lt;Oro&gt;</option>`+"\n"+`</select></label>`,
		`<h2>Lotti aperti oggi</h2>`+"\n"+`<ul class="consegne-elenco">`+"\n"+
			`<li id="ready-lot-`+itoa(recente)+`" class="lotti-riga"><div><strong>Zeta &lt;Oro&gt;</strong> <span class="sotto-in-linea">— lotto #`+itoa(recente)+` · 1 busta · aperto 31/10/2026 09:05</span></div> <a href="/ready-lots/`+itoa(recente)+`" class="pulsante-contorno">Apri</a></li>`+"\n"+
			`<li id="ready-lot-`+itoa(mezzanotte)+`" class="lotti-riga"><div><strong>Alfa Orafi</strong> <span class="sotto-in-linea">— lotto #`+itoa(mezzanotte)+` · 0 buste · aperto 31/10/2026 00:00</span></div> <a href="/ready-lots/`+itoa(mezzanotte)+`" class="pulsante-contorno">Apri</a></li>`+"\n"+`</ul>`)
	nonContiene(t, "piena", r.corpo, "ready-lot-no-goldsmith", "ready-lot-list-empty", `id="ready-lot-`+itoa(pienoIeri)+`"`, `id="ready-lot-`+itoa(inviato)+`"`, `id="ready-lot-`+itoa(vuotoIeri)+`"`)
	if contaRighe(t, d, `SELECT count(*) FROM lotti_avviso WHERE id = `+itoa(vuotoIeri)) != 0 || contaRighe(t, d, `SELECT count(*) FROM lotti_avviso`) != 4 {
		t.Fatal("la pagina non ha pulito i lotti vuoti dei giorni prima")
	}
}

// inventario: ORE-05
// «Apri lotto»: nessuna scelta (o un lot che non è la mappa) è «Seleziona un'oreficeria.», un id non numerico o
// non positivo «Oreficeria non valida.», la riga 1 «Non si può creare un avviso per l'oreficeria principale.», un
// id che non c'è «Oreficeria non trovata.», con la lista ridisegnata e il menu su «Scegli un'oreficeria» (ogni rifiuto viene da un valore che il menu non offre); un'oreficeria valida
// porta al suo lotto di oggi (ready_lot.opened con l'utente), lo stesso la seconda volta. Senza token il 403.
func TestApriLottoDallaPagina(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (2, 'Alfa Orafi')`)
	b.vai("GET", rottaLotti, nil, nil)
	for _, c := range []struct {
		valori    url.Values
		messaggio string
	}{
		{url.Values{"lot[oreficeria_id]": {""}}, "Seleziona un&#39;oreficeria."},
		{url.Values{"lot[oreficeria_id]": {"  "}}, "Seleziona un&#39;oreficeria."},
		{url.Values{}, "Seleziona un&#39;oreficeria."},
		{url.Values{"lot": {"2"}}, "Seleziona un&#39;oreficeria."},
		{url.Values{"lot": {"2"}, "lot[oreficeria_id]": {"2"}}, "Seleziona un&#39;oreficeria."},
		{url.Values{"lot[]": {"2"}, "lot[oreficeria_id]": {"2"}}, "Seleziona un&#39;oreficeria."},
		{url.Values{"lot[oreficeria_id][x]": {"2"}}, "Seleziona un&#39;oreficeria."},
		{url.Values{"lot[oreficeria_id][]": {"2"}, "lot[oreficeria_id]": {"2"}}, "Seleziona un&#39;oreficeria."},
		{url.Values{"lot[oreficeria_id]": {"abc"}}, "Oreficeria non valida."},
		{url.Values{"lot[oreficeria_id]": {"0"}}, "Oreficeria non valida."},
		{url.Values{"lot[oreficeria_id]": {"-2"}}, "Oreficeria non valida."},
		{url.Values{"lot[oreficeria_id]": {"2.0"}}, "Oreficeria non valida."},
		{url.Values{"lot[oreficeria_id]": {"1"}}, "Non si può creare un avviso per l&#39;oreficeria principale."},
		{url.Values{"lot[oreficeria_id]": {"9999"}}, "Oreficeria non trovata."},
	} {
		c.valori.Set("_csrf_token", b.token)
		r := b.vai("POST", rottaLotti, c.valori, nil)
		if r.codice != 200 {
			t.Fatalf("%v: %d", c.valori, r.codice)
		}
		contiene(t, c.messaggio, r.corpo, `<div role="alert" class="flash flash-errore">`+c.messaggio+`</div>`, `<h2>Lotti aperti oggi</h2>`, `<option value="2">Alfa Orafi</option>`)
	}
	if contaRighe(t, d, `SELECT count(*) FROM lotti_avviso`) != 0 {
		t.Fatal("un rifiuto ha aperto un lotto")
	}
	// Dopo un rifiuto nessuna opzione è scelta: il valore mandato non è nel menu.
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Beta'), (4, 'Gamma')`)
	r := b.vai("POST", rottaLotti, url.Values{"_csrf_token": {b.token}, "lot[oreficeria_id]": {"1"}}, nil)
	contiene(t, "menu", r.corpo, `<option value="">Scegli un&#39;oreficeria</option>`+"\n"+`<option value="2">Alfa Orafi</option>`+"\n"+`<option value="3">Beta</option>`+"\n"+`<option value="4">Gamma</option>`)
	nonContiene(t, "menu", r.corpo, " selected")
	r = b.vai("POST", rottaLotti, url.Values{"_csrf_token": {b.token}, "lot[oreficeria_id]": {" 4 "}}, nil)
	m := reLotto.FindStringSubmatch(r.location)
	if m == nil || !redirectA(r, r.location) {
		t.Fatalf("apertura: %d %q", r.codice, r.location)
	}
	if r := b.vai("POST", rottaLotti, url.Values{"_csrf_token": {b.token}, "lot[oreficeria_id]": {"4"}}, nil); !redirectA(r, "/ready-lots/"+m[1]) {
		t.Fatalf("ripresa: %d %q", r.codice, r.location)
	}
	if contaRighe(t, d, `SELECT count(*) FROM lotti_avviso WHERE oreficeria_id = 4`) != 1 || contaRighe(t, d, `SELECT count(*) FROM lotti_avviso`) != 1 {
		t.Fatal("aperti due lotti")
	}
	var a string
	if err := d.Lettura.QueryRow(`SELECT attore || '|' || risorsa || '|' || id_risorsa || '|' || dati FROM eventi_audit WHERE azione = 'ready_lot.opened'`).Scan(&a); err != nil {
		t.Fatal(err)
	}
	if a != "banco@esempio.it|ready_lot|"+m[1]+`|{"oreficeria_id":4}` {
		t.Fatalf("audit: %s", a)
	}
	if r := b.vai("POST", rottaLotti, url.Values{"lot[oreficeria_id]": {"3"}}, nil); r.codice != 403 {
		t.Fatalf("senza token: %d", r.codice)
	}
	if contaRighe(t, d, `SELECT count(*) FROM lotti_avviso WHERE oreficeria_id = 3`) != 0 {
		t.Fatal("aperto senza token")
	}
}

// Dopo un rifiuto la pagina non ripulisce: solo la GET della lista (mount) cancella i lotti vuoti dei giorni
// prima, come lo standalone che li cancella una volta per caricamento e non a ogni evento.
func TestRifiutoLottoNonPulisce(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	b.vai("GET", rottaLotti, nil, nil)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (2, 'Alfa Orafi')`)
	vecchio := lottoProva(t, d, 2, "2026-10-29T08:00:00.000000Z")
	b.vai("POST", rottaLotti, url.Values{"_csrf_token": {b.token}, "lot[oreficeria_id]": {""}}, nil)
	if contaRighe(t, d, `SELECT count(*) FROM lotti_avviso WHERE id = `+itoa(vecchio)) != 1 {
		t.Fatal("il rifiuto ha pulito")
	}
}

// lottiRotti fa fallire le letture e le scritture dei lotti.
type lottiRotti struct {
	Buste
	pulizia, lista, apertura bool
}

var errLottiRotti = errors.New("database rotto")

func (l lottiRotti) PulisciLottiVuoti(ctx context.Context) (int64, error) {
	if l.pulizia {
		return 0, errLottiRotti
	}
	return l.Buste.PulisciLottiVuoti(ctx)
}

func (l lottiRotti) LottiApertiOggi(ctx context.Context) ([]buste.LottoAperto, error) {
	if l.lista {
		return nil, errLottiRotti
	}
	return l.Buste.LottiApertiOggi(ctx)
}

func (l lottiRotti) ApriLotto(ctx context.Context, oreficeria int64, c audit.Contesto) (int64, error) {
	if l.apertura {
		return 0, errLottiRotti
	}
	return l.Buste.ApriLotto(ctx, oreficeria, c)
}

// Un guasto del database (pulizia, lista o apertura) è la pagina d'errore con la riga nel registro: nello
// standalone la LiveView cade, senza un messaggio suo.
func TestLottiGuasto(t *testing.T) {
	for _, c := range []struct {
		rotti    lottiRotti
		metodo   string
		registro string
	}{
		{lottiRotti{pulizia: true}, "GET", "lotti vuoti non puliti"},
		{lottiRotti{lista: true}, "GET", "lotti aperti non letti"},
		{lottiRotti{apertura: true}, "POST", "lotto non aperto"},
	} {
		var reg bytes.Buffer
		h, d := facciaConBusteCon(t, &reg, func(b Buste) Buste { c.rotti.Buste = b; return c.rotti })
		eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (2, 'Alfa Orafi')`)
		b := dipendenteDentro(t, h)
		var form url.Values
		if c.metodo == "POST" {
			b.vai("GET", rottaLotti, nil, nil)
			form = url.Values{"_csrf_token": {b.token}, "lot[oreficeria_id]": {"2"}}
		}
		r := b.vai(c.metodo, rottaLotti, form, nil)
		if r.codice != http.StatusInternalServerError {
			t.Fatalf("%s: %d", c.registro, r.codice)
		}
		nonContiene(t, c.registro, r.corpo, `id="ready-lot-list"`)
		contiene(t, c.registro, reg.String(), c.registro)
	}
}
