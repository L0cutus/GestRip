package web

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

// Lo scanner continuo della sessione di consegna (slice 079, ART-09): l'evento add_scanned di
// HandoverSessionLive, i casi di «the camera scanner» di handover_session_live_test.exs. Il codice letto
// arriva con POST /handovers/{id}/scansione e la risposta è il JSON della reply dello standalone, con il
// tipo del messaggio per la pagina. Il comportamento della finestra (fotocamera, stato, suoni) lo prova il
// banco browser (lettore-qr, prova consegna). Nomi e codici inventati.

// esitoScansione è la risposta di POST /handovers/{id}/scansione.
type esitoScansione struct {
	Ok      bool   `json:"ok"`
	Message string `json:"message"`
	Count   int    `json:"count"`
	Info    bool   `json:"info"`
}

// inventario: ART-09
// Una sessione aperta ha «📷 Scansiona» nel modulo del campo, accanto ad «Aggiungi», legato allo scanner
// continuo (l'indirizzo della scansione, la pagina da rileggere alla chiusura, «in consegna» per il conto);
// non riempie un campo e non manda un modulo. Una sessione confermata non lo ha.
func TestPulsanteScansiona(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Alfa Orafi')`)
	c := consegnaProva(t, d, 1, "2026-10-31T07:05:00.000000Z", "")
	r := b.vai("GET", percorsoConsegna(c), nil, nil)
	pulsante := `<button id="handover-scan-button" type="button" class="pulsante-contorno" data-scansione="/handovers/` + itoa(c) +
		`/scansione" data-ricarica="/handovers/` + itoa(c) + `" data-count-label="in consegna">📷 Scansiona</button>`
	contiene(t, "aperta", r.corpo, `<button type="submit">Aggiungi</button>`+"\n"+pulsante+"\n"+`</form>`)
	i := strings.Index(r.corpo, `id="handover-scan-button"`)
	if i < 0 || strings.Contains(r.corpo[i:strings.Index(r.corpo[i:], ">")+i], "data-target") ||
		strings.Contains(r.corpo[i:strings.Index(r.corpo[i:], ">")+i], "data-submit") {
		t.Fatal("lo scanner continuo non riempie un campo e non manda un modulo")
	}
	busta, _ := bustaConsegna(t, d, o, "", "Anello")
	eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ?, consegnata_artigiano_il = ? WHERE id = ?`, c, "2026-10-31T09:00:00.000000Z", busta)
	nonContiene(t, "confermata", b.vai("GET", percorsoConsegna(c), nil, nil).corpo, "handover-scan-button", "Scansiona", "/scansione")
}

// inventario: ART-09
// Ogni codice letto si aggiunge subito, prima come QR e poi come numero (l'ordine opposto al campo): la
// risposta dice l'esito (ok solo per una busta appena aggiunta: è il suono di successo), il messaggio della
// pagina e quante buste sono in consegna adesso; info distingue i messaggi d'informazione (aggiunta, «già in
// lista») dagli errori. «Già in lista» si decide sulla consegna riletta: una busta aggiunta da un altro
// telefono è una ripetizione. Un rifiuto della prima lettura è la risposta. Nessuna riga d'audit, nessun
// messaggio lasciato nella sessione e il campo scritto non si tocca.
func TestScansioneDellaConsegna(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Alfa Orafi'), (2, 'Cesello')`)
	c := consegnaProva(t, d, 1, "2026-10-31T07:05:00.000000Z", "")
	b.vai("GET", percorsoConsegna(c), nil, nil)
	scansiona := func(id string, form url.Values) esitoScansione {
		t.Helper()
		r := b.vai("POST", rottaConsegne+"/"+id+"/scansione", form, nil)
		if r.codice != 200 || r.h.Get("Content-Type") != "application/json; charset=utf-8" {
			t.Fatalf("%v: %d %q %s", form, r.codice, r.h.Get("Content-Type"), r.corpo)
		}
		var e esitoScansione
		dec := json.NewDecoder(strings.NewReader(r.corpo))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&e); err != nil {
			t.Fatalf("%s: %v", r.corpo, err)
		}
		return e
	}
	leggi := func(codice string) esitoScansione {
		t.Helper()
		return scansiona(itoa(c), url.Values{"_csrf_token": {b.token}, "qr": {codice}})
	}
	anello, na1 := bustaConsegna(t, d, o, "ANT-001", "Anello")
	collana, _ := bustaConsegna(t, d, o, "RIP-2026-0045", "Collana")
	if e := leggi(" ANT-001 "); e != (esitoScansione{true, "✓ Aggiunta busta N. " + na1, 1, true}) {
		t.Fatalf("prima busta: %+v", e)
	}
	if e := leggi("RIP-2026-0045"); e != (esitoScansione{true, "✓ Aggiunta busta #0045/26", 2, true}) {
		t.Fatalf("seconda busta: %+v", e)
	}
	if giroBusta(t, d, anello) != itoa(c)+"|-|1" || giroBusta(t, d, collana) != itoa(c)+"|-|1" {
		t.Fatal("buste non aggiunte")
	}
	if e := leggi("RIP-2026-0045"); e != (esitoScansione{false, "↻ Busta #0045/26 già in lista", 2, true}) {
		t.Fatalf("ripetuta: %+v", e)
	}
	// Prima il QR: «0046/26» è il codice di una busta e il numero dell'etichetta di un'altra.
	conEtichetta, _ := bustaConsegna(t, d, o, "RIP-2026-0046", "Bracciale")
	conCodice, nc := bustaConsegna(t, d, o, "0046/26", "Spilla")
	if e := leggi("0046/26"); e != (esitoScansione{true, "✓ Aggiunta busta N. " + nc, 3, true}) {
		t.Fatalf("prima il QR: %+v", e)
	}
	if giroBusta(t, d, conCodice) != itoa(c)+"|-|1" || giroBusta(t, d, conEtichetta) != "-|-|-" {
		t.Fatal("la scansione non ha letto prima il QR")
	}
	// Un numero letto (nessun QR con quel testo) vale anche lui.
	if e := leggi("0046"); e != (esitoScansione{true, "✓ Aggiunta busta #0046/26", 4, true}) {
		t.Fatalf("numero letto: %+v", e)
	}
	// Una busta aggiunta da un altro telefono nel frattempo: ripetizione, non successo.
	altro, na := bustaConsegna(t, d, o, "ANT-002", "Orecchini")
	eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ?, artigiano_id = 1 WHERE id = ?`, c, altro)
	if e := leggi("ANT-002"); e != (esitoScansione{false, "↻ Busta N. " + na + " già in lista", 5, true}) {
		t.Fatalf("altro telefono: %+v", e)
	}
	// I rifiuti: il testo della pagina con il codice, ok e info falsi, il conto della consegna.
	oro, _ := bustaConsegna(t, d, o, "ANT-ORO", "Catenina")
	eseguiSQL(t, d, `UPDATE buste SET compro_oro = 1, stato = 'in_valutazione' WHERE id = ?`, oro)
	fuori, _ := bustaConsegna(t, d, o, "ANT-OUT", "Spilla")
	eseguiSQL(t, d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, fuori)
	altrove, _ := bustaConsegna(t, d, o, "ANT-ALTRA", "Bracciale")
	altra := consegnaProva(t, d, 2, "2026-10-31T07:05:00.000000Z", "")
	eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ?, artigiano_id = 2 WHERE id = ?`, altra, altrove)
	for _, x := range []struct{ codice, messaggio string }{
		{"ANT-999", "Riparazione non trovata: nessuna busta con numero o QR «ANT-999»"},
		{"ANT-ORO", "Buste compro oro non possono essere date ad un artigiano («ANT-ORO»)"},
		{"ANT-OUT", "Stato 'consegnata' non adatto («ANT-OUT»)"},
		{"ANT-ALTRA", "Busta già in raccolta in altra consegna («ANT-ALTRA»)"},
		{"   ", "Numero busta o QR richiesto"},
		{"<b>", "Riparazione non trovata: nessuna busta con numero o QR «<b>»"},
	} {
		if e := leggi(x.codice); e != (esitoScansione{false, x.messaggio, 5, false}) {
			t.Fatalf("%q: %+v", x.codice, e)
		}
	}
	// Un rifiuto della prima lettura (il QR) è la risposta: il numero non si prova.
	sei, _ := bustaConsegna(t, d, o, "RIP-2026-0006", "Anello")
	oro6, _ := bustaConsegna(t, d, o, "0006/26", "Catenina")
	eseguiSQL(t, d, `UPDATE buste SET compro_oro = 1, stato = 'in_valutazione' WHERE id = ?`, oro6)
	if e := leggi("0006/26"); e != (esitoScansione{false, "Buste compro oro non possono essere date ad un artigiano («0006/26»)", 5, false}) {
		t.Fatalf("rifiuto della prima lettura: %+v", e)
	}
	if giroBusta(t, d, sei) != "-|-|-" {
		t.Fatal("dopo il rifiuto del QR si è provato il numero")
	}
	// Un numero che vale per più buste (dopo un QR non trovato): il rifiuto solo del Go della 077, senza aggiungere.
	g1, _ := bustaConsegna(t, d, o, "RIP-2026-0047", "Gemella")
	g2, _ := bustaConsegna(t, d, o, "RIP-2026-0047", "Gemella bis")
	if e := leggi("0047/26"); e != (esitoScansione{false, "Più buste corrispondono a «0047/26»: scrivi il numero con l'anno o usa il QR.", 5, false}) {
		t.Fatalf("più buste: %+v", e)
	}
	if giroBusta(t, d, g1) != "-|-|-" || giroBusta(t, d, g2) != "-|-|-" {
		t.Fatal("più buste: aggiunta una gemella")
	}
	// Senza qr, o con un qr che non è un testo (una mappa o una lista per Plug): il campo vuoto.
	for _, f := range []url.Values{{}, {"qr[x]": {"ANT-001"}}, {"qr[]": {"ANT-001"}}} {
		f.Set("_csrf_token", b.token)
		if e := scansiona(itoa(c), f); e != (esitoScansione{false, "Numero busta o QR richiesto", 5, false}) {
			t.Fatalf("%v: %+v", f, e)
		}
	}
	// Nessun messaggio resta nella sessione (la pagina lo mostra da sé) e il registro non cambia.
	nonContiene(t, "dopo le scansioni", b.vai("GET", percorsoConsegna(c), nil, nil).corpo, "flash-info", "flash-errore")
	if contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'handover.%'`) != 0 {
		t.Fatal("la scansione ha scritto nel registro")
	}
	// Sessione chiusa nel frattempo, o che non c'è: il rifiuto del dominio, con il suo conto.
	nuova, _ := bustaConsegna(t, d, o, "ANT-003", "Ciondolo")
	eseguiSQL(t, d, `UPDATE consegne_artigiani SET chiusa_il = ? WHERE id = ?`, "2026-10-31T09:00:00.000000Z", c)
	if e := leggi("ANT-003"); e != (esitoScansione{false, "Consegna non disponibile («ANT-003»)", 5, false}) {
		t.Fatalf("chiusa: %+v", e)
	}
	if e := scansiona("999", url.Values{"_csrf_token": {b.token}, "qr": {"ANT-003"}}); e != (esitoScansione{false, "Consegna non disponibile («ANT-003»)", 0, false}) {
		t.Fatalf("che non c'è: %+v", e)
	}
	if giroBusta(t, d, nuova) != "-|-|-" {
		t.Fatal("aggiunta a una consegna chiusa")
	}
	// Un id che non è un numero: 404; senza token: 403.
	for _, p := range []string{"/handovers/x/scansione", "/handovers/0/scansione"} {
		if r := b.vai("POST", p, url.Values{"_csrf_token": {b.token}, "qr": {"ANT-003"}}, nil); r.codice != 404 {
			t.Fatalf("%s: %d", p, r.codice)
		}
	}
	if r := b.vai("POST", percorsoConsegna(c)+"/scansione", url.Values{"qr": {"ANT-003"}}, nil); r.codice != 403 {
		t.Fatalf("senza token: %d", r.codice)
	}
}

// inventario: ART-09
// Il titolare usa lo scanner come il dipendente (la tabella dei permessi fissa la rotta del personale).
func TestScansioneDelTitolare(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Alfa Orafi')`)
	c := consegnaProva(t, d, 1, "2026-10-31T07:05:00.000000Z", "")
	busta, _ := bustaConsegna(t, d, o, "ANT-001", "Anello")
	b := titolareDentro(t, h)
	b.vai("GET", percorsoConsegna(c), nil, nil)
	r := b.vai("POST", percorsoConsegna(c)+"/scansione", url.Values{"_csrf_token": {b.token}, "qr": {"ANT-001"}}, nil)
	if r.codice != 200 || !strings.Contains(r.corpo, `"ok":true`) || giroBusta(t, d, busta) != itoa(c)+"|-|1" {
		t.Fatalf("titolare: %d %s", r.codice, r.corpo)
	}
}

// inventario: ART-09
// Lo scanner continuo in app.js (il modo continuo di qr_scanner.js dello standalone, slice 186), nello stesso
// lettore della home e di «Nuova riparazione» (nessuna seconda finestra né un secondo caricamento di jsQR):
// con data-scansione la finestra resta aperta e ogni codice letto va al server; la risposta (messaggio e
// conto con data-count-label) è la riga di stato; il suono è l'esito del server (successo solo per una busta
// appena aggiunta); lo stesso codice non riparte finché resta inquadrato (2 s senza vederlo lo rendono di
// nuovo nuovo); un codice alla volta (la risposta, o 5 s); una risposta arrivata dopo la chiusura si scarta;
// una rete che non risponde dice «⚠ Errore di rete…», suona il fallimento e riprova dopo 2 s. Alla chiusura,
// dopo almeno una scansione, la pagina della sessione si rilegge (la lista e il conto di adesso) con gli
// ultimi messaggi sopra e i campi di testo come li si è lasciati.
func TestScriptDelloScannerContinuo(t *testing.T) {
	js := string(leggiStatico("app.js"))
	contiene(t, "app.js", js,
		`event.target.closest("#qr-lookup-scan, #repair-qr-code-scan, [data-scansione]")`,
		"const RIPETIZIONE_MS = 2000", "const ATTESA_RISPOSTA_MS = 5000",
		`const continuo = pulsante => !!(pulsante && pulsante.hasAttribute("data-scansione"))`,
		"      if (testo !== null && continuo(s.pulsante)) {\n        scansionato(testo)\n      } else if (testo !== null) {",
		"    if (s.pausaFino && ora < s.pausaFino) return\n    if (s.inAttesa && ora - s.inAttesa.il < ATTESA_RISPOSTA_MS) {\n      if (testo === s.ultimo) s.vistoIl = ora\n      return\n    }",
		"    const ripetuto = testo === s.ultimo && ora - s.vistoIl < RIPETIZIONE_MS\n    s.ultimo = testo\n    s.vistoIl = ora\n    if (ripetuto) return",
		"const vivo = () => s.apertura === apertura && s.inAttesa === invio",
		`s.stato.textContent = "Lettura…"`,
		"const conto = etichetta && Number.isInteger(risposta.count) ? ` · ${risposta.count} ${etichetta}` : \"\"",
		"      s.stato.textContent = messaggio + conto",
		// Dopo la chiusura resta solo l'errore (il flash verde dello standalone si chiude da sé, FlashAutoClose).
		"if (messaggio && risposta.info !== true) esiti.errore = messaggio",
		// Il suono anche di una risposta arrivata dopo «Chiudi», come push_event(\"qr-outcome\") dello standalone.
		"      suonoQR.suona(risposta.ok === true)\n      if (!vivo()) return",
		"      s.pausaFino = Date.now() + RIPETIZIONE_MS\n      s.stato.textContent = \"⚠ Errore di rete: la busta non è stata aggiunta, riprovo…\"\n      suonoQR.suona(false)",
		`corpo.set("qr", testo)`, `headers: {Accept: "application/json"}`,
		// «Chiudi» con una scansione in corso aspetta la risposta (al più 5 s) prima di rileggere la pagina.
		"Promise.race([attesa || Promise.resolve(), new Promise(ok => setTimeout(ok, ATTESA_RISPOSTA_MS))])",
		".then(() => rileggiPagina(indirizzo, esiti, pagina))",
		`mostraPagina(doc, indirizzo, "sostituisci", true)`,
		`flash.className = "flash flash-errore"`,
		// L'errore che la pagina aveva già resta, se le scansioni non ne hanno uno nuovo.
		`const errore = esiti.errore || erroreDiPrima`,
		// La pagina cambiata nel frattempo non si sostituisce.
		"if (document.body !== pagina) return",
		// I campi di testo come li si è lasciati.
		`if (nuovo && nuovo.matches("input[type=text]")) nuovo.setAttribute("value", campo.value)`,
		// Una risposta che non è la pagina della sessione: una navigazione normale.
		"if (!r.ok || r.redirected || !main || doc.getElementById(\"photo-queue\") === null) {\n        window.location.assign(indirizzo)",
		// La sessione scaduta (il login) e il token scaduto (403): la pagina della sessione, con una navigazione.
		"if (r.status === 403 || new URL(r.url).pathname === \"/login\") {\n      window.location.assign(ricarica)\n      return new Promise(() => {})",
		// Ogni altra risposta che non è l'esito è un errore, come la rete.
		`if (!r.ok || !(r.headers.get("content-type") || "").startsWith("application/json")) throw new Error("risposta non valida")`,
	)
	nonContiene(t, "app.js", js, "esiti.info", `"flash flash-" + tipo`)
	// Un solo lettore: una finestra, un caricamento di jsQR, un ciclo dei fotogrammi.
	for s, n := range map[string]int{`el.id = "qr-scanner-modal"`: 1, "function caricaJsQR()": 1, "function fotogramma()": 1, "jsQR(img.data": 1} {
		if c := strings.Count(js, s); c != n {
			t.Errorf("%q compare %d volte", s, c)
		}
	}
	// La chiusura spegne anche l'attesa: una risposta tardiva non scrive più niente.
	i := strings.Index(js, "  function chiudiLettore() {")
	if i < 0 || !strings.Contains(js[i:i+300], "s.inAttesa = null") || !strings.Contains(js[i:i+300], "s.esiti = null") {
		t.Error("chiudiLettore non azzera l'attesa e gli esiti")
	}
}
