package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Lo scanner continuo del lotto d'avviso (slice 090, ORE-07): l'evento scan_continuous di ReadyLotLive, i casi di
// «the continuous scanner of the lot (slice 200)» di ready_lot_live_test.exs. Il codice letto arriva con
// POST /ready-lots/{id}/scansione e la risposta è il JSON della reply dello standalone, {ok, message, count}, con il
// tipo del messaggio per la pagina (info), come lo scanner della consegna (079, ART-09). Il comportamento della
// finestra (fotocamera, stato, suoni, chiusura) è lo stesso lettore di app.js, provato dai test della 079 e dal banco
// browser (lettore-qr, prova lotto). La pagina sotto la finestra si rilegge solo alla chiusura
// (D-98); il campo «Numero busta» del lotto non suona in nessuna delle due app (nessuna D-91 gemella). Nomi e codici inventati.

func scansionaIlLotto(t *testing.T, b *browser, id string, form url.Values) esitoScansione {
	t.Helper()
	r := b.vai("POST", rottaLotti+"/"+id+"/scansione", form, nil)
	if r.codice != 200 || r.h.Get("Content-Type") != "application/json; charset=utf-8" || r.h.Get("Cache-Control") != "no-store" {
		t.Fatalf("%v: %d %q %q %s", form, r.codice, r.h.Get("Content-Type"), r.h.Get("Cache-Control"), r.corpo)
	}
	var e esitoScansione
	dec := json.NewDecoder(strings.NewReader(r.corpo))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		t.Fatalf("%s: %v", r.corpo, err)
	}
	return e
}

// inventario: ORE-07
// Un lotto aperto ha «📷 Scansiona» nel modulo del QR, fra il campo del codice e «Aggiungi dal QR» (come nello
// standalone), legato allo scanner continuo di app.js (l'indirizzo della scansione, la pagina da rileggere alla
// chiusura, «nel lotto» per il conto); non riempie un campo e non manda un modulo. Un lotto uscito non lo ha.
func TestPulsanteScansionaDelLotto(t *testing.T) {
	h, d, _ := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	l := lottoConOreficeria(t, d)
	r := b.vai("GET", percorsoLotto(l), nil, nil)
	pulsante := `<button id="ready-lot-scan-button" type="button" class="pulsante-contorno" data-scansione="/ready-lots/` + itoa(l) +
		`/scansione" data-ricarica="/ready-lots/` + itoa(l) + `" data-count-label="nel lotto">📷 Scansiona</button>`
	contiene(t, "aperto", r.corpo, `placeholder="Scansiona o inserisci il codice QR" class="mono">`+"\n"+pulsante+"\n"+
		`<button type="submit" class="pulsante-contorno">Aggiungi dal QR</button>`)
	i := strings.Index(r.corpo, `id="ready-lot-scan-button"`)
	if i < 0 || strings.Contains(r.corpo[i:strings.Index(r.corpo[i:], ">")+i], "data-target") ||
		strings.Contains(r.corpo[i:strings.Index(r.corpo[i:], ">")+i], "data-submit") {
		t.Fatal("lo scanner continuo non riempie un campo e non manda un modulo")
	}
	eseguiSQL(t, d, `UPDATE lotti_avviso SET inviato_il = '2026-10-31T08:30:00.000000Z' WHERE id = ?`, l)
	nonContiene(t, "uscito", b.vai("GET", percorsoLotto(l), nil, nil).corpo, "ready-lot-scan-button", "Scansiona</button>", "/scansione")
}

// inventario: ORE-07
// Ogni codice letto si aggiunge subito al lotto, prima come QR e poi come numero: la risposta dice l'esito (ok solo
// per una busta appena aggiunta: è il suono di successo), il messaggio, quante buste sono nel lotto adesso (anche
// dopo un rifiuto) e info (solo l'aggiunta: nello standalone «già in questo lotto» di scan_continuous è un flash
// d'errore, a differenza del campo). Un rifiuto della prima lettura (il QR) è la risposta, e «già avvisata» vince
// come nel campo. Ogni aggiunta scrive ready_lot.bag_added con l'utente; i rifiuti niente; nessun messaggio resta
// nella sessione; i campi scritti non si toccano.
func TestScansioneDelLotto(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	l := lottoConOreficeria(t, d)
	uno := prontaDelLotto(t, d, o, "QR-LOTTO-70", "Uno") // N. 1/26
	due := prontaDelLotto(t, d, o, "", "Due")            // N. 2/26, senza codice
	b.vai("GET", percorsoLotto(l), nil, nil)
	leggi := func(codice string) esitoScansione {
		t.Helper()
		return scansionaIlLotto(t, b, itoa(l), modulo(b, "qr", codice))
	}
	if e := leggi(" QR-LOTTO-70 "); e != (esitoScansione{true, "✓ Aggiunta busta N. 1/26", 1, true}) {
		t.Fatalf("per QR: %+v", e)
	}
	if lottoDellaBusta(t, d, uno) != itoa(l) {
		t.Fatal("la busta non è nel lotto")
	}
	// La stessa di nuovo: un fallimento (suono e messaggio senza il numero), non un successo né un messaggio verde.
	if e := leggi("QR-LOTTO-70"); e != (esitoScansione{false, "↻ Busta già in questo lotto", 1, false}) {
		t.Fatalf("ripetuta: %+v", e)
	}
	// Un'etichetta senza QR porta il suo numero: il codice si prova come numero.
	if e := leggi("2/26"); e != (esitoScansione{true, "✓ Aggiunta busta N. 2/26", 2, true}) {
		t.Fatalf("per numero: %+v", e)
	}
	if lottoDellaBusta(t, d, due) != itoa(l) {
		t.Fatal("la busta per numero non è nel lotto")
	}
	// Prima il QR: «0046/26» è il codice di una busta e il numero dell'etichetta di un'altra.
	conCodice := prontaDelLotto(t, d, o, "0046/26", "Tre")
	conEtichetta := prontaDelLotto(t, d, o, "RIP-2026-0046", "Quattro")
	if e := leggi("0046/26"); e != (esitoScansione{true, "✓ Aggiunta busta N. 3/26", 3, true}) {
		t.Fatalf("prima il QR: %+v", e)
	}
	if lottoDellaBusta(t, d, conCodice) != itoa(l) || lottoDellaBusta(t, d, conEtichetta) != "-" {
		t.Fatal("la scansione non ha letto prima il QR")
	}
	// Un codice di nessuna busta (anche con caratteri speciali): il testo con il codice, il conto invariato.
	for _, x := range []struct{ codice, messaggio string }{
		{"NESSUNO", "Riparazione non trovata. («NESSUNO»)"},
		{"<b>", "Riparazione non trovata. («<b>»)"},
		{" NESSUNO ", "Riparazione non trovata. («NESSUNO»)"}, // senza spazi ai bordi, anche nel messaggio
	} {
		if e := leggi(x.codice); e != (esitoScansione{false, x.messaggio, 3, false}) {
			t.Fatalf("%q: %+v", x.codice, e)
		}
	}
	// Un qr ripetuto: vale l'ultimo valore, come Plug (il primo è una busta del lotto).
	if e := scansionaIlLotto(t, b, itoa(l), modulo(b, "qr", "QR-LOTTO-70", "qr", "NESSUNO")); e != (esitoScansione{false, "Riparazione non trovata. («NESSUNO»)", 3, false}) {
		t.Fatalf("qr ripetuto: %+v", e)
	}
	// Una busta cestinata non si trova.
	cestinata := prontaDelLotto(t, d, o, "QR-CESTINO", "Cestino")
	eseguiSQL(t, d, `UPDATE buste SET cancellata_il = '2026-10-31T08:00:00.000000Z' WHERE id = ?`, cestinata)
	if e := leggi("QR-CESTINO"); e != (esitoScansione{false, "Riparazione non trovata. («QR-CESTINO»)", 3, false}) {
		t.Fatalf("cestinata: %+v", e)
	}
	// I rifiuti del dominio (gli stessi del campo, senza il codice), ognuno con il suo conto e niente aggiunto.
	avvisata := prontaDelLotto(t, d, o, "QR-AVVISATA", "Avvisata")
	eseguiSQL(t, d, `UPDATE buste SET avviso_pronta_il = '2026-10-31T08:00:00.000000Z' WHERE id = ?`, avvisata)
	altra := prontaDelLotto(t, d, o, "QR-ALTRA", "Altra")
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Beta Orafi')`)
	eseguiSQL(t, d, `UPDATE buste SET oreficeria_id = 3 WHERE id = ?`, altra)
	oro := prontaDelLotto(t, d, o, "QR-ORO", "Oro")
	eseguiSQL(t, d, `UPDATE buste SET compro_oro = 1, stato = 'in_valutazione' WHERE id = ?`, oro)
	lavorazione := prontaDelLotto(t, d, o, "QR-LAVORO", "In lavorazione")
	eseguiSQL(t, d, `UPDATE buste SET stato = 'in_lavorazione' WHERE id = ?`, lavorazione)
	altroLotto := lottoProva(t, d, 2, "2026-10-31T08:06:00.000000Z")
	inAltro := prontaDelLotto(t, d, o, "QR-ALTRO-LOTTO", "In altro lotto")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, altroLotto, inAltro)
	uscito := lottoProva(t, d, 2, "2026-10-31T08:07:00.000000Z")
	eseguiSQL(t, d, `UPDATE lotti_avviso SET inviato_il = '2026-10-31T08:30:00.000000Z' WHERE id = ?`, uscito)
	inUscito := prontaDelLotto(t, d, o, "QR-USCITO", "Uscita")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, uscito, inUscito)
	for _, x := range []struct{ codice, messaggio string }{
		{"QR-AVVISATA", "Busta già avvisata singolarmente: azzera l'avviso dal dettaglio, poi riprova."},
		{"QR-ALTRA", "Busta di altra oreficeria."},
		{"QR-ORO", "Le schede compro oro non entrano in un lotto."},
		{"QR-LAVORO", "Busta non pronta."},
		{"QR-ALTRO-LOTTO", "Busta già nel lotto #" + itoa(altroLotto) + ": toglila da lì, poi riprova."},
		{"QR-USCITO", "Busta già uscita col lotto #" + itoa(uscito) + ": resta sua. Per riavvisare il cliente usa l'avviso singolo dal dettaglio."},
		{"   ", "Numero busta, QR o id richiesto."},
	} {
		if e := leggi(x.codice); e != (esitoScansione{false, x.messaggio, 3, false}) {
			t.Fatalf("%q: %+v", x.codice, e)
		}
	}
	// Un rifiuto della prima lettura (il QR) è la risposta: il numero non si prova.
	quattro := prontaDelLotto(t, d, o, "", "Numero 77/26 libero")
	eseguiSQL(t, d, `UPDATE buste SET numero = '77/26' WHERE id = ?`, quattro)
	avvisataQuattro := prontaDelLotto(t, d, o, "77/26", "Avvisata con il codice 77/26")
	eseguiSQL(t, d, `UPDATE buste SET avviso_pronta_il = '2026-10-31T08:00:00.000000Z' WHERE id = ?`, avvisataQuattro)
	if e := leggi("77/26"); e != (esitoScansione{false, "Busta già avvisata singolarmente: azzera l'avviso dal dettaglio, poi riprova.", 3, false}) {
		t.Fatalf("rifiuto della prima lettura: %+v", e)
	}
	if lottoDellaBusta(t, d, quattro) != "-" {
		t.Fatal("dopo il rifiuto del QR si è provato il numero")
	}
	// Un numero che vale per più buste (dopo un QR non trovato): il rifiuto solo del Go (D-95), senza aggiungere.
	g1 := prontaDelLotto(t, d, o, "RIP-2026-0047", "Gemella")
	g2 := prontaDelLotto(t, d, o, "RIP-2026-0047", "Gemella bis")
	if e := leggi("0047/26"); e != (esitoScansione{false, "Più buste corrispondono a «0047/26»: scrivi il numero con l'anno o usa il QR.", 3, false}) {
		t.Fatalf("più buste: %+v", e)
	}
	if lottoDellaBusta(t, d, g1) != "-" || lottoDellaBusta(t, d, g2) != "-" {
		t.Fatal("più buste: aggiunta una gemella")
	}
	// Senza qr, o con un qr che non è un testo (una mappa o una lista per Plug): il campo vuoto.
	for _, f := range []url.Values{{}, {"qr[x]": {"QR-LOTTO-70"}}, {"qr[]": {"QR-LOTTO-70"}}} {
		f.Set("_csrf_token", b.token)
		if e := scansionaIlLotto(t, b, itoa(l), f); e != (esitoScansione{false, "Numero busta, QR o id richiesto.", 3, false}) {
			t.Fatalf("%v: %+v", f, e)
		}
	}
	// Tre aggiunte, tre righe nel registro con l'utente; i rifiuti niente; nessun messaggio lasciato nella sessione.
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'ready_lot.bag_added' AND attore = 'banco@esempio.it' AND id_risorsa = '`+itoa(l)+`'`); n != 3 {
		t.Fatalf("audit: %d", n)
	}
	p := b.vai("GET", percorsoLotto(l), nil, nil)
	nonContiene(t, "dopo le scansioni", p.corpo, "flash-info", "flash-errore")
	contiene(t, "dopo le scansioni", p.corpo, `<span id="ready-lot-bag-count" class="sotto-in-linea">3</span>`)
	// Un lotto uscito nel frattempo, o che non c'è: il rifiuto del dominio, con il suo conto.
	nuova := prontaDelLotto(t, d, o, "QR-NUOVA", "Nuova")
	eseguiSQL(t, d, `UPDATE lotti_avviso SET inviato_il = '2026-10-31T08:40:00.000000Z' WHERE id = ?`, l)
	if e := leggi("QR-NUOVA"); e != (esitoScansione{false, "Lotto non disponibile.", 3, false}) {
		t.Fatalf("uscito: %+v", e)
	}
	if e := scansionaIlLotto(t, b, "999999", modulo(b, "qr", "QR-NUOVA")); e != (esitoScansione{false, "Lotto non disponibile.", 0, false}) {
		t.Fatalf("che non c'è: %+v", e)
	}
	if lottoDellaBusta(t, d, nuova) != "-" {
		t.Fatal("aggiunta a un lotto che non c'è più")
	}
	// Un id che non è un numero: 404; senza token: 403.
	for _, p := range []string{"/ready-lots/x/scansione", "/ready-lots/0/scansione", "/ready-lots/-1/scansione"} {
		if r := b.vai("POST", p, modulo(b, "qr", "QR-NUOVA"), nil); r.codice != 404 {
			t.Fatalf("%s: %d", p, r.codice)
		}
	}
	if r := b.vai("POST", percorsoLotto(l)+"/scansione", url.Values{"qr": {"QR-NUOVA"}}, nil); r.codice != 403 {
		t.Fatalf("senza token: %d", r.codice)
	}
}

// inventario: ORE-07
// Il titolare usa lo scanner come il dipendente (la tabella dei permessi fissa la rotta del personale).
func TestScansioneDelLottoDelTitolare(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	l := lottoConOreficeria(t, d)
	busta := prontaDelLotto(t, d, o, "QR-TITOLARE", "Anello")
	b := titolareDentro(t, h)
	b.vai("GET", percorsoLotto(l), nil, nil)
	r := b.vai("POST", percorsoLotto(l)+"/scansione", modulo(b, "qr", "QR-TITOLARE"), nil)
	if r.codice != 200 || !strings.Contains(r.corpo, `"ok":true`) || lottoDellaBusta(t, d, busta) != itoa(l) {
		t.Fatalf("titolare: %d %s", r.codice, r.corpo)
	}
}

// inventario: ORE-07
// Un guasto del database nella scansione è la pagina d'errore con la riga nel registro (come l'aggiunta dal campo);
// un guasto nel conto dopo l'aggiunta lo stesso.
func TestScansioneDelLottoGuasto(t *testing.T) {
	for _, c := range []struct {
		rotti    lottoRotto
		registro string
	}{
		{lottoRotto{aggiunta: true}, "busta scansionata non aggiunta al lotto"},
		{lottoRotto{membri: true}, "buste del lotto non lette"},
	} {
		var reg bytes.Buffer
		h, d := facciaConBusteCon(t, &reg, func(b Buste) Buste { c.rotti.Buste = b; return c.rotti })
		l := lottoConOreficeria(t, d)
		b := dipendenteDentro(t, h)
		b.vai("GET", rottaLotti, nil, nil)
		r := b.vai("POST", percorsoLotto(l)+"/scansione", modulo(b, "qr", "QR-QUALUNQUE"), nil)
		if r.codice != http.StatusInternalServerError {
			t.Fatalf("%s: %d", c.registro, r.codice)
		}
		contiene(t, c.registro, reg.String(), c.registro)
	}
}
