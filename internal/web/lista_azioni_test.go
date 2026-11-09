package web

import (
	"net/url"
	"strings"
	"testing"
)

// Le azioni del riquadro della lista (lista_azioni.go, slice 056) e il ritorno alla lista filtrata,
// come gli eventi quick-status, transition e toggle_pin di WorkshopLive e #list-return dello
// standalone; i casi da workshop_live_test.exs: «the quick buttons of a card (slice 198)», «the
// status select of a card (slice 197)», «pin control toggles in place and crafted ids fail without
// mutation» e «the list states its own address…».

func formLista(b *browser, lista string, campi ...string) url.Values {
	f := url.Values{"_csrf_token": {b.token}, "lista": {lista}}
	for i := 0; i+1 < len(campi); i += 2 {
		f.Set(campi[i], campi[i+1])
	}
	return f
}

// inventario: RIP-H-14, RIP-H-15, RIP-H-16
// Il riquadro: «✓ Pronta» solo su una busta in lavorazione, «✓ Consegnata» solo su una pronta (con la
// conferma e il numero visibile), nessuno su preventivi, attesa materiali e compro oro; il menu con lo
// stato di adesso e i soli passi del grafo, nell'ordine della famiglia, spento e senza «Cambia stato» su una
// busta chiusa; il 📌 «Pinna in cima» o «Togli pin». Ogni modulo ha il CSRF e la lista a cui tornare.
func TestRiquadroAzioni(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	lav := bustaHome(t, d, o, "1/26", "RIP-2026-0045", "")
	pronta := bustaHome(t, d, o, "2/26", "", "stato = 'pronta', fissata_il = ?", istanteHome)
	prev := bustaHome(t, d, o, "3/26", "", "stato = 'preventivo'")
	attesa := bustaHome(t, d, o, "4/26", "", "stato = 'in_attesa_materiali'")
	oro := bustaHome(t, d, o, "5/26", "", "compro_oro = 1, stato = 'valutata'")
	cons := bustaHome(t, d, o, "6/26", "", "stato = 'consegnata', data_consegna = '2026-10-31'")
	b := dipendenteDentro(t, h)
	corpo := b.vai("GET", "/?stato=&p=1", nil, nil).corpo
	tok := b.token

	r := riquadroBusta(t, corpo, lav)
	contiene(t, "in lavorazione", r, `<div class="rep-card-azioni">`+"\n"+
		`<form action="/repairs/`+idTesto(lav)+`/rapido" method="post" data-azione-lista><input name="_csrf_token" type="hidden" value="`+tok+
		`"><input name="lista" type="hidden" value=""><input name="status" type="hidden" value="pronta"><button id="quick-ready-`+idTesto(lav)+
		`" type="submit" title="Segna come pronta" class="gs-rapido gs-rapido-pronta">✓ Pronta</button></form>`,
		`<button id="pin-repair-`+idTesto(lav)+`" type="submit" aria-label="Pinna in cima" title="Pinna in cima" class="pin-btn">📌</button>`,
		`<form id="transition-`+idTesto(lav)+`" action="/repairs/`+idTesto(lav)+`/stato-lista" method="post" class="gs-stato-lista" data-azione-lista data-stato-lista>`,
		`<select id="transition-status-`+idTesto(lav)+`" name="status" class="gs-campo" aria-label="Stato 0045/26">`+"\n"+
			`<option value="in_lavorazione" selected>In lavorazione</option>`+"\n"+`<option value="preventivo">Preventivo</option>`+"\n"+
			`<option value="pronta">Pronta</option>`+"\n"+`<option value="in_attesa_materiali">In attesa materiali</option>`+"\n"+
			`<option value="annullata">Annullata</option>`+"\n"+`</select>`+"\n"+
			`<button type="submit" class="pulsante-contorno" data-solo-senza-js>Cambia stato</button>`)
	nonContiene(t, "in lavorazione", r, "quick-delivered", `<option value="consegnata"`)

	r = riquadroBusta(t, corpo, pronta)
	contiene(t, "pronta", r, `<input name="status" type="hidden" value="consegnata"><input name="conferma" type="hidden" value="" data-conferma-js><button id="quick-delivered-`+idTesto(pronta)+
		`" type="submit" data-confirm="Segnare la busta N. 2/26 come consegnata?" title="Segna come consegnata" class="gs-rapido gs-rapido-consegnata">✓ Consegnata</button>`,
		`<button id="pin-repair-`+idTesto(pronta)+`" type="submit" aria-label="Togli pin" title="Togli pin" class="pin-btn pinned">📌</button>`,
		`aria-label="Stato N. 2/26">`+"\n"+`<option value="in_lavorazione">In lavorazione</option>`+"\n"+`<option value="pronta" selected>Pronta</option>`+"\n"+
			`<option value="consegnata">Consegnata</option>`+"\n"+`<option value="annullata">Annullata</option>`+"\n"+`</select>`)
	nonContiene(t, "pronta", r, "quick-ready", `<option value="preventivo"`)

	for nome, c := range map[string]struct {
		id      int64
		opzioni []string
	}{
		"preventivo": {prev, []string{"in_lavorazione", "preventivo", "annullata"}},
		"attesa":     {attesa, []string{"in_lavorazione", "preventivo", "pronta", "in_attesa_materiali", "annullata"}},
		"compro oro": {oro, []string{"in_valutazione", "valutata", "perfezionata", "annullata"}},
	} {
		r := riquadroBusta(t, corpo, c.id)
		nonContiene(t, nome, r, "quick-ready", "quick-delivered", " disabled")
		var visti []string
		for _, m := range strings.Split(r, `<option value="`)[1:] {
			visti = append(visti, m[:strings.Index(m, `"`)])
		}
		if strings.Join(visti, ",") != strings.Join(c.opzioni, ",") {
			t.Errorf("%s: opzioni %v, attese %v", nome, visti, c.opzioni)
		}
	}

	// Una busta chiusa compare con un filtro sullo stato: il menu spento con il solo stato, niente
	// «Cambia».
	corpo = b.vai("GET", "/?stato=consegnata", nil, nil).corpo
	r = riquadroBusta(t, corpo, cons)
	contiene(t, "consegnata", r, `<input name="lista" type="hidden" value="stato=consegnata">`,
		`aria-label="Stato N. 6/26" disabled>`+"\n"+`<option value="consegnata" selected>Consegnata</option>`+"\n"+`</select>`+"\n"+`</form>`)
	nonContiene(t, "consegnata", r, "quick-ready", "quick-delivered", "data-solo-senza-js")
}

// inventario: RIP-H-14
// «✓ Pronta» porta la busta a pronta (audit del cambio di stato) e alla scheda sul prezzo;
// «✓ Consegnata» a consegnata e alla lista di prima con «Stato aggiornato.». Uno stato di partenza
// sbagliato, un passo che non è rapido, un id falsificato o malformato: la lista con «Transizione non
// consentita.», niente scritto. Vale anche per il dipendente.
func TestPulsantiRapidi(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaHome(t, d, o, "1/26", "", "")
	prev := bustaHome(t, d, o, "2/26", "", "stato = 'preventivo'")
	attesa := bustaHome(t, d, o, "3/26", "", "stato = 'in_attesa_materiali'")
	b := dipendenteDentro(t, h)
	b.vai("GET", "/", nil, nil)

	r := b.vai("POST", "/repairs/"+idTesto(id)+"/rapido", formLista(b, "stato=in_lavorazione&p=2", "status", "pronta"), nil)
	if r.codice != 302 || r.location != "/repairs/"+idTesto(id)+"?edit=prezzo" {
		t.Fatalf("pronta: %d %q", r.codice, r.location)
	}
	if s := colonna(t, d, id, "stato"); s != "pronta" {
		t.Errorf("dopo «✓ Pronta»: %s", s)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed' AND attore = 'banco@esempio.it' AND dati = '{"status":"pronta"}'`); n != 1 {
		t.Errorf("audit pronta %d", n)
	}
	contiene(t, "scheda sul prezzo", b.vai("GET", r.location, nil, nil).corpo, `id="repair-form"`)

	b.vai("GET", "/", nil, nil)
	r = b.vai("POST", "/repairs/"+idTesto(id)+"/rapido", formLista(b, "stato=pronta&cerca=Cliente", "status", "consegnata", "conferma", "1"), nil)
	if r.codice != 302 || r.location != "/?cerca=Cliente&stato=pronta" {
		t.Fatalf("consegnata: %d %q", r.codice, r.location)
	}
	if s := colonna(t, d, id, "stato"); s != "consegnata" {
		t.Errorf("dopo «✓ Consegnata»: %s", s)
	}
	contiene(t, "consegnata", b.vai("GET", r.location, nil, nil).corpo, `<div role="status" class="flash flash-info">Stato aggiornato.</div>`)

	prima := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed'`)
	for _, c := range []struct{ id, stato string }{
		{idTesto(prev), "pronta"}, {idTesto(attesa), "pronta"}, {idTesto(prev), "consegnata"}, {idTesto(id), "consegnata"},
		{idTesto(prev), "annullata"}, {idTesto(prev), "in_lavorazione"}, {idTesto(prev), ""}, {"abc", "consegnata"},
		{"99999", "pronta"}, {"0", "pronta"}, {"99999999999999999999999999999", "pronta"},
	} {
		b.vai("GET", "/", nil, nil)
		r := b.vai("POST", "/repairs/"+c.id+"/rapido", formLista(b, "stato=preventivo", "status", c.stato), nil)
		if r.codice != 302 || r.location != "/?stato=preventivo" {
			t.Errorf("%s → %q: %d %q", c.id, c.stato, r.codice, r.location)
			continue
		}
		contiene(t, c.id+" → "+c.stato, b.vai("GET", r.location, nil, nil).corpo,
			`<div role="alert" class="flash flash-errore">Transizione non consentita.</div>`)
	}
	if got := colonna(t, d, prev, "stato") + "|" + colonna(t, d, attesa, "stato") + "|" + colonna(t, d, id, "stato"); got != "preventivo|in_attesa_materiali|consegnata" {
		t.Errorf("stati dopo i rifiuti: %s", got)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed'`); n != prima {
		t.Errorf("audit dopo i rifiuti: %d, prima %d", n, prima)
	}
}

// inventario: RIP-H-14
// «✓ Consegnata» senza JavaScript (slice 060, Stefano 09/11/2026): senza conferma=1 il server disegna la
// conferma, con la frase e il numero visibile, «Sì» che rimanda il modulo con conferma=1 e «Annulla»
// verso la lista, e non scrive niente; con conferma=1 consegna. Una busta che non è pronta o non c'è
// (anche un id malformato): la lista con «Transizione non consentita.», senza la pagina della conferma.
func TestConsegnataConfermaSenzaJS(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	pronta := bustaHome(t, d, o, "1/26", "RIP-2026-0045", "stato = 'pronta'")
	lav := bustaHome(t, d, o, "2/26", "", "")
	cestino := bustaHome(t, d, o, "3/26", "RIP-2026-0046", "stato = 'pronta', cancellata_il = '2026-10-30T09:00:00.000000Z'")
	b := dipendenteDentro(t, h)
	audit := func() int {
		return contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed'`)
	}
	prima := audit()
	for _, conferma := range []string{"", "0", "sì"} {
		b.vai("GET", "/", nil, nil)
		f := formLista(b, "stato=pronta&cerca=Cliente&boh=1", "status", "consegnata")
		if conferma != "" {
			f.Set("conferma", conferma)
		}
		r := b.vai("POST", "/repairs/"+idTesto(pronta)+"/rapido", f, nil)
		if r.codice != 200 {
			t.Fatalf("senza conferma %q: %d %q", conferma, r.codice, r.location)
		}
		contiene(t, "conferma "+conferma, r.corpo,
			`<p id="quick-delivered-confirm-text" class="gs-qr-conferma">Segnare la busta 0045/26 come consegnata?</p>`,
			`<form action="/repairs/`+idTesto(pronta)+`/rapido" method="post">`,
			`<input name="lista" type="hidden" value="cerca=Cliente&amp;stato=pronta">`,
			`<input name="status" type="hidden" value="consegnata">`,
			`<button id="quick-delivered-yes" type="submit" name="conferma" value="1" class="pulsante">Sì</button>`,
			`<a id="quick-delivered-no" href="/?cerca=Cliente&amp;stato=pronta" class="pulsante-contorno">Annulla</a>`,
			"<title>Riparazioni · Laboratorio</title>")
		if s := colonna(t, d, pronta, "stato"); s != "pronta" || audit() != prima {
			t.Fatalf("la conferma ha scritto: %s, audit %d prima %d", s, audit(), prima)
		}
	}
	for _, id := range []string{idTesto(lav), idTesto(cestino), "99999", "abc"} {
		b.vai("GET", "/", nil, nil)
		r := b.vai("POST", "/repairs/"+id+"/rapido", formLista(b, "stato=pronta", "status", "consegnata"), nil)
		if r.codice != 302 || r.location != "/?stato=pronta" {
			t.Fatalf("%s: %d %q", id, r.codice, r.location)
		}
		contiene(t, id, b.vai("GET", r.location, nil, nil).corpo, `<div role="alert" class="flash flash-errore">Transizione non consentita.</div>`)
	}
	// «Sì»: il modulo della pagina con conferma=1.
	b.vai("GET", "/", nil, nil)
	r := b.vai("POST", "/repairs/"+idTesto(pronta)+"/rapido", formLista(b, "cerca=Cliente&stato=pronta", "status", "consegnata", "conferma", "1"), nil)
	if r.codice != 302 || r.location != "/?cerca=Cliente&stato=pronta" || colonna(t, d, pronta, "stato") != "consegnata" || audit() != prima+1 {
		t.Errorf("Sì: %d %q %s", r.codice, r.location, colonna(t, d, pronta, "stato"))
	}
}

// inventario: RIP-H-15
// Il menu dello stato: un passo del grafo cambia lo stato (date e audit come la scheda) e torna alla
// lista con «Stato aggiornato.»; lo stato che la busta ha già torna in silenzio, senza audit; un
// passo fuori dal grafo, di un'altra famiglia, vuoto, un id falsificato o malformato: «Transizione non
// consentita.» e niente scritto.
func TestMenuStatoLista(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaHome(t, d, o, "1/26", "", "")
	oro := bustaHome(t, d, o, "2/26", "", "compro_oro = 1, stato = 'in_valutazione'")
	b := titolareDentro(t, h)
	audit := func() int {
		return contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed'`)
	}

	b.vai("GET", "/", nil, nil)
	r := b.vai("POST", "/repairs/"+idTesto(id)+"/stato-lista", formLista(b, "p=3", "status", "pronta"), nil)
	if r.codice != 302 || r.location != "/?p=3" {
		t.Fatalf("pronta: %d %q", r.codice, r.location)
	}
	if got := colonna(t, d, id, "stato") + "|" + colonna(t, d, id, "data_pronta"); got != "pronta|2026-10-31" {
		t.Errorf("dopo il menu: %s", got)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.status_changed' AND id_risorsa = '`+idTesto(id)+`' AND dati = '{"status":"pronta"}'`); n != 1 {
		t.Errorf("audit %d", n)
	}
	contiene(t, "pronta", b.vai("GET", r.location, nil, nil).corpo, `<div role="status" class="flash flash-info">Stato aggiornato.</div>`)

	// Lo stato uguale: niente audit, niente messaggio, la lista.
	prima := audit()
	r = b.vai("POST", "/repairs/"+idTesto(id)+"/stato-lista", formLista(b, "", "status", "pronta"), nil)
	if r.codice != 302 || r.location != "/" {
		t.Fatalf("uguale: %d %q", r.codice, r.location)
	}
	nonContiene(t, "uguale", b.vai("GET", "/", nil, nil).corpo, "flash-info", "flash-errore")
	if audit() != prima {
		t.Error("lo stato uguale ha scritto l'audit")
	}

	for _, c := range []struct{ id, stato string }{
		{idTesto(id), "preventivo"}, {idTesto(id), "valutata"}, {idTesto(id), ""}, {idTesto(id), "boh"},
		{idTesto(oro), "pronta"}, {idTesto(oro), "chiusa"}, {"99999", "pronta"}, {"abc", "pronta"},
		{"99999999999999999999999999999", "pronta"},
	} {
		b.vai("GET", "/", nil, nil)
		r := b.vai("POST", "/repairs/"+c.id+"/stato-lista", formLista(b, "", "status", c.stato), nil)
		if r.codice != 302 || r.location != "/" {
			t.Errorf("%s → %q: %d %q", c.id, c.stato, r.codice, r.location)
			continue
		}
		contiene(t, c.id+" → "+c.stato, b.vai("GET", "/", nil, nil).corpo,
			`<div role="alert" class="flash flash-errore">Transizione non consentita.</div>`)
	}
	if got := colonna(t, d, id, "stato") + "|" + colonna(t, d, oro, "stato"); got != "pronta|in_valutazione" {
		t.Errorf("stati dopo i rifiuti: %s", got)
	}
	if audit() != prima {
		t.Errorf("audit dopo i rifiuti: %d, prima %d", audit(), prima)
	}

	// Una busta nel cestino (review della 056): né un passo del grafo né lo stato che ha già passano
	// in silenzio; «Transizione non consentita.» e niente scritto.
	cestinata := bustaHome(t, d, o, "3/26", "", "cancellata_il = ?", istanteHome)
	for _, stato := range []string{"pronta", "in_lavorazione"} {
		b.vai("GET", "/", nil, nil)
		r := b.vai("POST", "/repairs/"+idTesto(cestinata)+"/stato-lista", formLista(b, "", "status", stato), nil)
		if r.codice != 302 || r.location != "/" {
			t.Errorf("cestino → %q: %d %q", stato, r.codice, r.location)
			continue
		}
		contiene(t, "cestino → "+stato, b.vai("GET", "/", nil, nil).corpo,
			`<div role="alert" class="flash flash-errore">Transizione non consentita.</div>`)
	}
	if s := colonna(t, d, cestinata, "stato"); s != "in_lavorazione" || audit() != prima {
		t.Errorf("cestino: stato %s, audit %d (prima %d)", s, audit(), prima)
	}

	// Il compro oro segue il suo grafo.
	b.vai("GET", "/", nil, nil)
	if r := b.vai("POST", "/repairs/"+idTesto(oro)+"/stato-lista", formLista(b, "", "status", "valutata"), nil); r.location != "/" {
		t.Fatalf("valutata: %q", r.location)
	}
	if s := colonna(t, d, oro, "stato"); s != "valutata" {
		t.Errorf("compro oro: %s", s)
	}
}

// inventario: RIP-H-16
// Il 📌: «Pinna in cima» fissa la busta, che va in testa alla lista, e scrive repair.pinned; «Togli
// pin» la toglie e scrive repair.unpinned; la lista di prima, senza messaggio. Un id falsificato o
// malformato: «Riparazione non trovata.» e niente scritto.
func TestPinLista(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	vecchia := bustaHome(t, d, o, "1/26", "", "")
	nuova := bustaHome(t, d, o, "2/26", "", "")
	b := dipendenteDentro(t, h)
	corpo := b.vai("GET", "/", nil, nil).corpo
	if got := ordineRiquadri(corpo); strings.Join(got, ",") != idTesto(nuova)+","+idTesto(vecchia) {
		t.Fatalf("ordine prima: %v", got)
	}

	r := b.vai("POST", "/repairs/"+idTesto(vecchia)+"/pin", formLista(b, "stato=in_lavorazione"), nil)
	if r.codice != 302 || r.location != "/?stato=in_lavorazione" {
		t.Fatalf("pin: %d %q", r.codice, r.location)
	}
	corpo = b.vai("GET", r.location, nil, nil).corpo
	if got := ordineRiquadri(corpo); strings.Join(got, ",") != idTesto(vecchia)+","+idTesto(nuova) {
		t.Errorf("ordine dopo il pin: %v", got)
	}
	contiene(t, "fissata", riquadroBusta(t, corpo, vecchia), `class="rep-card rep-card-pinned"`, `aria-label="Togli pin" title="Togli pin" class="pin-btn pinned"`)
	nonContiene(t, "fissata", corpo, "flash-info", "flash-errore")
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.pinned' AND id_risorsa = '`+idTesto(vecchia)+`' AND attore = 'banco@esempio.it'`); n != 1 {
		t.Errorf("audit pinned %d", n)
	}

	r = b.vai("POST", "/repairs/"+idTesto(vecchia)+"/pin", formLista(b, ""), nil)
	if r.location != "/" {
		t.Fatalf("togli: %q", r.location)
	}
	corpo = b.vai("GET", "/", nil, nil).corpo
	if got := ordineRiquadri(corpo); strings.Join(got, ",") != idTesto(nuova)+","+idTesto(vecchia) {
		t.Errorf("ordine dopo «Togli pin»: %v", got)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.unpinned' AND id_risorsa = '`+idTesto(vecchia)+`'`); n != 1 {
		t.Errorf("audit unpinned %d", n)
	}

	prima := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione IN ('repair.pinned', 'repair.unpinned')`)
	eseguiSQL(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteHome, nuova)
	for _, id := range []string{"99999999999999999999999999999", "99999", "abc", "0", idTesto(nuova)} {
		b.vai("GET", "/", nil, nil)
		r := b.vai("POST", "/repairs/"+id+"/pin", formLista(b, ""), nil)
		if r.codice != 302 || r.location != "/" {
			t.Errorf("%s: %d %q", id, r.codice, r.location)
			continue
		}
		contiene(t, id, b.vai("GET", "/", nil, nil).corpo, `<div role="alert" class="flash flash-errore">Riparazione non trovata.</div>`)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione IN ('repair.pinned', 'repair.unpinned')`); n != prima {
		t.Errorf("audit dopo i rifiuti: %d, prima %d", n, prima)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM buste WHERE fissata_il IS NOT NULL`); n != 0 {
		t.Errorf("fissate: %d", n)
	}
}

// inventario: RIP-H-14, RIP-H-15, RIP-H-16, RIP-H-20
// La lista a cui si torna: il server non segue «lista» com'è, la rilegge come i filtri della home e
// ne riscrive l'indirizzo (filtri validi in ordine di nome, la pagina dalla 2, mai un codice
// d'etichetta): un valore falsificato porta al più a un'altra lista, mai fuori.
func TestListaDelModulo(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaHome(t, d, o, "1/26", "", "")
	b := titolareDentro(t, h)
	for lista, voluto := range map[string]string{
		"":                                    "/",
		"p=1":                                 "/",
		"stato=pronta&p=2":                    "/?p=2&stato=pronta",
		"cerca=RIP-2026-0045&stato=pronta":    "/?stato=pronta",
		"cerca=0045":                          "/?cerca=0045",
		"cerca=Rossi&scadenza=1&boh=2":        "/?cerca=Rossi&scadenza=1",
		"//altro.esempio/x":                   "/",
		"https://altro.esempio/":              "/",
		"stato=/../x&oreficeria=abc":          "/?stato=%2F..%2Fx",
		"data_da=2026-01-01&data_campo=boh":   "/?data_campo=accettazione&data_da=2026-01-01",
		"%0d%0aLocation:%20https://x.esempio": "/",
	} {
		b.vai("GET", "/", nil, nil)
		r := b.vai("POST", "/repairs/"+idTesto(id)+"/pin", formLista(b, lista), nil)
		if r.codice != 302 || r.location != voluto {
			t.Errorf("%q: %d %q, voluto %q", lista, r.codice, r.location, voluto)
		}
	}
}

// inventario: RIP-H-20
// La lista scrive il suo indirizzo in #list-return (filtri e pagina, in ordine di nome come lo
// standalone, mai un codice d'etichetta), e «← Indietro» della scheda e di «Nuova riparazione» porta
// a «/» senza JavaScript e, con app.js, all'indirizzo tenuto in sessionStorage gestrip:list-return,
// solo se è quello di una lista.
func TestRitornoAllaLista(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaHome(t, d, o, "1/26", "RIP-2026-0045", "")
	for i := 2; i <= 35; i++ {
		bustaHome(t, d, o, idTesto(int64(i))+"/26", "", "")
	}
	b := titolareDentro(t, h)
	for q, voluto := range map[string]string{
		"/":                                    "/",
		"/?p=2":                                "/?p=2",
		"/?p=1":                                "/",
		"/?stato=in_lavorazione&cerca=Cliente": "/?cerca=Cliente&stato=in_lavorazione",
		"/?esito=abc":                          "/",
		"/?cerca=RIP-2026-0045":                "/",
		"/?compro_oro=1&scadenza=1":            "/?compro_oro=1&scadenza=1",
	} {
		contiene(t, q, b.vai("GET", q, nil, nil).corpo, `<div id="list-return" data-path="`+strings.ReplaceAll(voluto, "&", "&amp;")+`" hidden></div>`)
	}
	// La lista disegnata dal POST del campo unico (una ricerca scritta con più risultati).
	b.vai("GET", "/", nil, nil)
	r := b.vai("POST", "/cerca-qr", url.Values{"_csrf_token": {b.token}, "cerca": {"Cliente"}, "stato": {"in_lavorazione"}}, nil)
	contiene(t, "cerca-qr", r.corpo, `<div id="list-return" data-path="/?cerca=Cliente&amp;stato=in_lavorazione" hidden></div>`,
		`<input name="lista" type="hidden" value="cerca=Cliente&amp;stato=in_lavorazione">`)

	contiene(t, "scheda", b.vai("GET", percorsoScheda(id), nil, nil).corpo,
		`<a id="repair-back-list" href="/" class="gs-back-btn" data-list-return>← Indietro</a>`)
	contiene(t, "nuova", b.vai("GET", "/nuova", nil, nil).corpo,
		`<a id="new-repair-back" href="/" class="gs-back-btn" data-list-return>← Indietro</a>`)
	nonContiene(t, "nuova", b.vai("GET", "/nuova", nil, nil).corpo, `id="list-return"`)

	js := string(leggiStatico("app.js"))
	contiene(t, "app.js", js, `const CHIAVE = "gestrip:list-return"`,
		`const LISTA = /^\/(\?[A-Za-z0-9\-._~!$&'()*+,;=:@%\/?]*)?$/`,
		`sessionStorage.setItem(CHIAVE, qui.dataset.path)`, `percorso = sessionStorage.getItem(CHIAVE)`,
		`if (percorso && LISTA.test(percorso)) indietro.forEach(a => a.setAttribute("href", percorso))`,
		`document.querySelectorAll("a[data-list-return]")`)
}

// inventario: RIP-H-14, RIP-H-15, RIP-H-16
// Lo script delle azioni del riquadro: il menu parte alla scelta, «Cambia stato» si nasconde, i moduli
// partono con fetch una volta sola, la lista di ritorno resta allo stesso punto con il messaggio
// sospeso, «✓ Pronta» va alla scheda con la cronologia.
func TestScriptAzioniDellaLista(t *testing.T) {
	js := string(leggiStatico("app.js"))
	contiene(t, "app.js", js,
		`document.querySelectorAll("[data-solo-senza-js]").forEach(el => { el.hidden = true })`,
		`if (!(menu instanceof HTMLSelectElement) || !menu.form || !menu.form.hasAttribute("data-stato-lista")) return`,
		`menu.form.requestSubmit()`,
		`!form.hasAttribute("data-azione-lista")) return`,
		`if (form.dataset.inviato) return`,
		`mostraPagina(doc, r.url, sullaLista ? "sostituisci" : "aggiungi", sullaLista)`,
		`flash.classList.add("flash-sospesa")`)
	// La conferma di «✓ Consegnata» è data-confirm, che sta prima e ferma il clic; dopo «OK» mette
	// conferma=1 nel modulo, così il server non disegna la sua conferma (slice 060).
	contiene(t, "app.js conferma", js, `const campo = el.form && el.form.querySelector("input[data-conferma-js]")`, `if (campo) campo.value = "1"`)
	if strings.Index(js, `event.target.closest("[data-confirm]")`) > strings.Index(js, `hasAttribute("data-azione-lista")`) {
		t.Error("data-confirm dopo l'invio delle azioni della lista")
	}
	css := string(leggiStatico("app.css"))
	contiene(t, "app.css", css, ".rep-card-azioni {", ".pin-btn.pinned {", "[data-solo-senza-js][hidden] { display: none !important; }", ".flash.flash-sospesa {",
		".rep-card { grid-template-columns: minmax(5rem, 7rem) minmax(0, 1fr) auto;")
}
