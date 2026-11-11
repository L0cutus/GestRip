package web

import (
	"regexp"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/buste"
)

// L'avviso WhatsApp di ritiro dopo «✅ Valutata» dal pannello del QR (slice 068, RIP-H-26), come
// qr_announce/2 di WorkshopLive e workshop_qr_notice_live_test.exs dello standalone a go-baseline. Dati
// inventati.

var reAperturaQR = regexp.MustCompile(`<a id="qr-send-opening" href="([^"]+)"`)

// valutaDaQR porta la busta id da in_valutazione a valutata dal pannello, con la conferma.
func valutaDaQR(t *testing.T, b *browser, id int64) risposta {
	t.Helper()
	return azioneQR(b, id, "qr-avanza", "in_valutazione", "conferma", "1")
}

// inventario: RIP-H-26
// «✅ Valutata» del compro oro: la conferma con la frase di WhatsApp; confermata, lo stato avanza
// (risposta 10 del 05/11/2026) e l'avviso di ritiro è registrato (blocco, riga WhatsApp con il link vero
// al posto di {link} e senza spazi ai bordi, evento «notified», ready_notice.sent); la pagina della
// scansione offre «↗ Apri WhatsApp» (#qr-opening, fuori dal pannello delle azioni, che non c'è più) con
// «👁 Consultazione», «✓ valutata» e il bip di successo. La scansione dopo non ha più il link; il secondo
// invio dello stesso modulo (doppio tocco senza JavaScript) non avvisa di nuovo e torna alla lista.
func TestPannelloQRAvvisoWhatsApp(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaHome(t, d, o, "34/26", "RIP-2026-0055", `stato = 'in_valutazione', compro_oro = 1`)
	bustaHome(t, d, o, "35/26", "QR-ALTRA", ``)
	b := titolareDentro(t, h)
	scansione(b, "RIP-2026-0055", "compro_oro", "1")
	r := azioneQR(b, id, "qr-avanza", "in_valutazione", "compro_oro", "1")
	contiene(t, "conferma", r.corpo, `<p id="qr-action-confirm" class="gs-qr-conferma">Conferma valutazione completata? Verrà aperto WA &#34;scheda pronta firma&#34;.</p>`)
	if !strings.HasPrefix(statoBusta(t, d, id), "in_valutazione|") || contaRighe(t, d, `SELECT count(*) FROM messaggi_in_uscita`) != 0 {
		t.Fatal("la conferma ha scritto")
	}
	r = azioneQR(b, id, "qr-avanza", "in_valutazione", "compro_oro", "1", "conferma", "1")
	if r.codice != 200 || r.h.Get("Cache-Control") != "no-store" || !strings.HasPrefix(statoBusta(t, d, id), "valutata|") {
		t.Fatalf("valutata: %d %q %s", r.codice, r.location, statoBusta(t, d, id))
	}
	var testo string
	if err := d.Lettura.QueryRow(`SELECT testo FROM messaggi_in_uscita WHERE busta_id = ? AND canale = 'whatsapp' AND stato = 'accepted'
		AND destinatario = 'customer' AND indirizzo = '+39333111'`, id).Scan(&testo); err != nil {
		t.Fatalf("messaggio: %v", err)
	}
	token := colonna(t, d, id, "token_pubblico")
	if !strings.Contains(testo, indirizzoPubblicoProva+"/r/"+token) || strings.Contains(testo, "{link}") || testo != strings.TrimSpace(testo) ||
		!strings.Contains(testo, "34/26") {
		t.Errorf("testo %q", testo)
	}
	if colonna(t, d, id, "avviso_pronta_il") == "∅" || righeAudit(t, d, "ready_notice.sent") != 1 ||
		contaSQL(t, d, `SELECT count(*) FROM eventi_avviso_pronta WHERE busta_id = ? AND evento = 'notified' AND attore = 'titolare@esempio.it'`, id) != 1 {
		t.Error("blocco, audit o evento dell'avviso")
	}
	contiene(t, "apertura", r.corpo, `<div role="status" class="flash flash-info">✓ valutata</div>`,
		`<section id="qr-lookup" class="gs-qr" data-qr-esito="ok">`,
		`<div id="qr-opening" class="gs-qr-azione">`+"\n"+`<p class="gs-qr-titolo">#0055/26 — Cliente 34/26</p>`+"\n"+
			`<p class="gs-qr-stato">Stato: <strong>Valutata</strong> · avviso registrato</p>`+"\n"+`<div class="gs-qr-pulsanti">`+"\n"+
			`<a id="qr-send-opening" href="`,
		`" target="_blank" rel="noopener" class="pulsante" data-qr-apertura>↗ Apri WhatsApp</a>`+"\n"+
			`<a id="qr-opening-consulta" href="/repairs/`+idTesto(id)+`" class="pulsante-contorno">👁 Consultazione</a>`,
		`<a id="qr-lookup-close" href="/?compro_oro=1" class="pulsante-contorno">Chiudi</a>`)
	nonContiene(t, "apertura", r.corpo, `id="qr-action"`, "qr-btn-avanza", "qr-btn-si")
	m := reAperturaQR.FindStringSubmatch(r.corpo)
	if m == nil || unescapeHTML(m[1]) != buste.IndirizzoWaMe("+39333111", testo) {
		t.Errorf("wa.me %v, atteso %q", m, buste.IndirizzoWaMe("+39333111", testo))
	}
	nonContiene(t, "dopo", scansione(b, "QR-ALTRA").corpo, "qr-opening", "qr-send-opening")
	r = azioneQR(b, id, "qr-avanza", "in_valutazione", "compro_oro", "1", "conferma", "1")
	if r.codice != 302 || r.location != "/?compro_oro=1" || contaSQL(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE busta_id = ?`, id) != 1 ||
		righeAudit(t, d, "ready_notice.sent") != 1 {
		t.Fatalf("secondo invio: %d %q", r.codice, r.location)
	}
	contiene(t, "secondo invio", b.vai("GET", r.location, nil, nil).corpo, `<div role="status" class="flash flash-info">✓ valutata</div>`)
}

// inventario: RIP-H-26
// Quando l'avviso non parte lo stato avanza lo stesso, il pannello si chiude e la lista dice perché,
// con le parole dello standalone: senza telefono o senza un numero da comporre «✓ valutata (no WA,
// telefono mancante)» (info); già avvisata o in un lotto aperto l'errore. Un testo salvato che non si
// compone dice l'etichetta e l'avviso, senza emettere il link. Niente scritto oltre al cambio di stato.
func TestPannelloQRAvvisoNonParte(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome, telefono) VALUES (1, 'Il negozio', NULL), (2, 'Oreficeria Finta', '049 000')`)
	eseguiSQL(t, d, `INSERT INTO lotti_avviso (id, oreficeria_id, token, creato_il, aggiornato_il) VALUES (7, 2, 'lotto-sintetico', ?1, ?1)`, istanteHome)
	b := dipendenteDentro(t, h)
	for i, c := range []struct{ colonne, classe, frase string }{
		{`telefono_cliente = ''`, "flash-info", "✓ valutata (no WA, telefono mancante)"},
		{`telefono_cliente = 'n/d'`, "flash-info", "✓ valutata (no WA, telefono mancante)"},
		{`avviso_pronta_il = '2026-10-01T09:00:00.000000Z'`, "flash-errore", "✓ valutata — già stata avvisata: azzera l&#39;avviso dal dettaglio per rifarlo."},
		{`oreficeria_id = 2, lotto_avviso_id = 7`, "flash-errore", "✓ valutata — è in un lotto ancora aperto: l&#39;avviso parte col lotto."},
	} {
		id := bustaHome(t, d, o, idTesto(int64(40+i))+"/26", "QR-NO-"+idTesto(int64(i)), `stato = 'in_valutazione', compro_oro = 1, `+c.colonne)
		r := valutaDaQR(t, b, id)
		if r.codice != 302 || r.location != "/" || !strings.HasPrefix(statoBusta(t, d, id), "valutata|") {
			t.Fatalf("%s: %d %q %s", c.frase, r.codice, r.location, statoBusta(t, d, id))
		}
		contiene(t, c.frase, b.vai("GET", "/", nil, nil).corpo, `class="flash `+c.classe+`">`+c.frase+`</div>`)
		if n := contaSQL(t, d, `SELECT (SELECT count(*) FROM messaggi_in_uscita WHERE busta_id = ?1) + (SELECT count(*) FROM eventi_avviso_pronta WHERE busta_id = ?1)`, id); n != 0 {
			t.Errorf("%s: %d righe", c.frase, n)
		}
	}
	if righeAudit(t, d, "ready_notice.sent") != 0 {
		t.Error("audit dell'avviso")
	}
	eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.template.wa_pronta_compro_oro', 'Valutata {password} {link}', ?)`, istanteHome)
	id := bustaHome(t, d, o, "50/26", "QR-ROTTO", `stato = 'in_valutazione', compro_oro = 1`)
	link := colonna(t, d, id, "token_pubblico") + colonna(t, d, id, "hash_token_pubblico")
	if r := valutaDaQR(t, b, id); r.codice != 302 || !strings.HasPrefix(statoBusta(t, d, id), "valutata|") {
		t.Fatalf("testo rotto: %d", r.codice)
	}
	contiene(t, "testo rotto", b.vai("GET", "/", nil, nil).corpo,
		`<div role="alert" class="flash flash-errore">✓ valutata — testo «Pronta — Compro oro» non valido: Segnaposto non supportato: {password}. Avviso non registrato.</div>`)
	if colonna(t, d, id, "avviso_pronta_il") != "∅" || colonna(t, d, id, "token_pubblico")+colonna(t, d, id, "hash_token_pubblico") != link ||
		contaSQL(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE busta_id = ?`, id) != 0 || righeAudit(t, d, "repair.public_link_issued") != 0 {
		t.Error("il testo rotto ha scritto")
	}
}

// inventario: RIP-H-26
// Una busta conto terzi avvisa l'oreficeria con il testo per le oreficerie («Stato: VALUTATA») e il link
// della busta; il link wa.me è sul numero dell'oreficeria.
func TestPannelloQRAvvisoOreficeria(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome, telefono) VALUES (1, 'Il negozio', NULL), (2, 'Oreficeria Finta', '049 000')`)
	id := bustaHome(t, d, o, "34/26", "QR-B2B", `stato = 'in_valutazione', compro_oro = 1, oreficeria_id = 2`)
	b := titolareDentro(t, h)
	r := valutaDaQR(t, b, id)
	var testo string
	if err := d.Lettura.QueryRow(`SELECT testo FROM messaggi_in_uscita WHERE busta_id = ? AND destinatario = 'oreficeria' AND indirizzo = '+39049000'`, id).Scan(&testo); err != nil {
		t.Fatalf("messaggio: %v (%d)", err, r.codice)
	}
	if !strings.Contains(testo, "Stato: VALUTATA") || !strings.Contains(testo, indirizzoPubblicoProva+"/r/"+colonna(t, d, id, "token_pubblico")) {
		t.Errorf("testo %q", testo)
	}
	if m := reAperturaQR.FindStringSubmatch(r.corpo); m == nil || unescapeHTML(m[1]) != buste.IndirizzoWaMe("+39049000", testo) {
		t.Errorf("wa.me %v", m)
	}
}

// inventario: RIP-H-26
// Senza indirizzo_pubblico un testo con {link} non parte (D-46 estesa): lo stato avanza, nessun link
// emesso, nessun avviso, e la lista dice cosa configurare. Un testo senza {link} parte lo stesso.
func TestPannelloQRAvvisoSenzaIndirizzoPubblico(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, "")
	id := bustaHome(t, d, o, "34/26", "QR-SENZA", `stato = 'in_valutazione', compro_oro = 1`)
	link := colonna(t, d, id, "token_pubblico") + colonna(t, d, id, "hash_token_pubblico")
	b := titolareDentro(t, h)
	r := valutaDaQR(t, b, id)
	if r.codice != 302 || r.location != "/" || !strings.HasPrefix(statoBusta(t, d, id), "valutata|") {
		t.Fatalf("%d %q", r.codice, r.location)
	}
	contiene(t, "lista", b.vai("GET", "/", nil, nil).corpo,
		`<div role="alert" class="flash flash-errore">✓ valutata — avviso non registrato: manca «indirizzo_pubblico = https://host[:porta]» in `)
	if colonna(t, d, id, "token_pubblico")+colonna(t, d, id, "hash_token_pubblico") != link || colonna(t, d, id, "avviso_pronta_il") != "∅" ||
		contaSQL(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE busta_id = ?`, id) != 0 || righeAudit(t, d, "repair.public_link_issued") != 0 {
		t.Error("ha scritto")
	}
	eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.template.wa_pronta_compro_oro', 'Valutata, n° {numero}', ?)`, istanteHome)
	id = bustaHome(t, d, o, "35/26", "QR-SENZA-2", `stato = 'in_valutazione', compro_oro = 1`)
	if r = valutaDaQR(t, b, id); r.codice != 200 || !strings.Contains(r.corpo, `id="qr-send-opening"`) {
		t.Fatalf("senza link: %d", r.codice)
	}
}

// inventario: RIP-H-26
// Solo la valutazione avvisa: una riparazione che diventa pronta dal pannello va alla scheda sul prezzo
// e non avvisa nessuno (l'avviso parte dalla scheda).
func TestPannelloQRProntaNonAvvisa(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaHome(t, d, o, "34/26", "QR-RIP", ``)
	b := titolareDentro(t, h)
	r := azioneQR(b, id, "qr-avanza", "in_lavorazione")
	if r.codice != 302 || r.location != "/repairs/"+idTesto(id)+"?edit=prezzo" || !strings.HasPrefix(statoBusta(t, d, id), "pronta|") {
		t.Fatalf("%d %q", r.codice, r.location)
	}
	if colonna(t, d, id, "avviso_pronta_il") != "∅" || contaSQL(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE busta_id = ?`, id) != 0 {
		t.Error("ha avvisato")
	}
}

// inventario: RIP-H-26
// D-74: con JavaScript «↗ Apri WhatsApp» apre WhatsApp nella scheda nuova (target=_blank, il tocco non si
// ferma) e porta questa pagina alla lista di «Chiudi» (i filtri di prima); «Chiudi» ed Esc su un pannello
// con l'avviso registrato vanno alla lista vera (ricaricata), non a quella rimasta sotto.
func TestScriptAperturaWhatsAppDalPannello(t *testing.T) {
	b, _ := dentroCome(t, accounts.Titolare, nil)
	js := assetDellaPagina(t, b, b.vai("GET", "/", nil, nil).corpo, "js", "text/javascript; charset=utf-8")
	for _, s := range []string{
		"  const apertura = event.target.closest && event.target.closest(\"a[data-qr-apertura]\")\n  if (!apertura) return\n" +
			"  const chiudi = document.getElementById(\"qr-lookup-close\")\n  const lista = chiudi ? chiudi.getAttribute(\"href\") : \"/\"\n" +
			"  setTimeout(() => window.location.assign(lista), 0)",
		"    if (el.querySelector(\"#qr-opening\")) {\n      const chiudi = el.querySelector(\"#qr-lookup-close\")\n" +
			"      window.location.assign(chiudi ? chiudi.getAttribute(\"href\") : \"/\")\n      return true\n    }",
	} {
		if !strings.Contains(js, s) {
			t.Errorf("app.js non contiene %q", s)
		}
	}
}
