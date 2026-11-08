package web

import (
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/accounts"
)

// I tre pezzi della slice 059 sul lato del server e dello script: il lettore a pistola con
// un'etichetta che non è di nessuna busta (RIP-H-27), il 📷 di «Etichetta» quando la fotocamera non
// si apre (RIP-H-28), «⚠ senza QR» sulle buste con un QR di altro formato (D-79). Il comportamento
// nel browser lo provano gli scenari lettore-qr del banco (prove pistola, senza-fotocamera, cambia).

const esitoListaKO = `<div id="repair-search-outcome" data-qr-esito="ko" hidden></div>`

// inventario: RIP-H-27
// Dal campo unico (il lettore a pistola scrive e preme Invio: 🔍, senza via=qr) un testo con la forma
// esatta di un'etichetta, anche minuscolo o con gli spazi che NormalizzaCodice toglie, che non trova
// nessuna busta disegna la lista vuota con l'esito ko per i due bip gravi; il riquadro del QR non c'è.
// Ogni altra ricerca scritta resta senza esito: un nome, un numero d'etichetta, un «RIP-2026-45» (non
// è un'etichetta), un'etichetta che la ricerca testuale trova in più buste; un'etichetta di una busta
// apre il riquadro col suo esito ok, come prima. Le GET della lista non hanno mai l'esito.
func TestPistolaEtichettaSconosciuta(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	s := seminaRicerca(t, d, o)
	// Due buste con lo stesso codice di altro formato scritto nelle note: la ricerca testuale le trova.
	bustaHome(t, d, o, "31/26", "", `note_interne = 'vecchia etichetta RIP-2026-0777'`)
	bustaHome(t, d, o, "32/26", "", `note_interne = 'vecchia etichetta RIP-2026-0777'`)
	for _, b := range []*browser{titolareDentro(t, h), dipendenteDentro(t, h)} {
		for _, codice := range []string{"RIP-2026-0999", "rip-2026-0999", " RIP-2026- 0999 ", "RIP - 2026 - 0999", "RIP-2025-12345"} {
			r := scansione(b, codice)
			if r.codice != 200 || r.location != "" || r.h.Get("Cache-Control") != "no-store" {
				t.Fatalf("%q: %d %q %q", codice, r.codice, r.location, r.h.Get("Cache-Control"))
			}
			contiene(t, codice, r.corpo, esitoListaKO, `<div id="repairs-empty" class="gs-lista-vuota">Nessuna riparazione trovata.</div>`)
			nonContiene(t, codice, r.corpo, `id="qr-lookup"`)
		}
		for _, testo := range []string{"Cliente inesistente", "RIP-2026-45", "RIP-2026-0000", "RIP 2026 0999", "0999", "9999/26", "RIP-2026-0777"} {
			r := scansione(b, testo)
			if r.codice != 200 {
				t.Fatalf("%q: %d", testo, r.codice)
			}
			nonContiene(t, testo, r.corpo, "data-qr-esito")
		}
		// L'etichetta scritta nelle note di due buste: la lista con le due, senza esito.
		contiene(t, "note", scansione(b, "RIP-2026-0777").corpo, `id="repair-`)
		// Un'etichetta che è di una busta: il riquadro, con l'esito ok del server.
		r := scansione(b, "RIP-2025-0045")
		contiene(t, "trovata", r.corpo, `<section id="qr-lookup" class="gs-qr" data-qr-esito="ok">`)
		nonContiene(t, "trovata", r.corpo, "repair-search-outcome")
		// Una ricerca testuale che trova una sola busta apre la scheda (redirect), senza esito.
		if r := scansione(b, "Anello unico da cercare"); r.location != "/repairs/"+idTesto(s.aperta) {
			t.Errorf("una busta: %q", r.location)
		}
		for _, p := range []string{"/?cerca=RIP-2026-0999", "/", "/?cerca=rip-2026-0999"} {
			nonContiene(t, p, b.vai("GET", p, nil, nil).corpo, "data-qr-esito", "repair-search-outcome")
		}
	}
}

// inventario: RIP-H-27
// La regola dell'esito della ricerca scritta, da sola.
func TestEsitoRicercaScritta(t *testing.T) {
	for _, c := range []struct {
		cerca   string
		trovate int64
		esito   string
	}{
		{"RIP-2026-0999", 0, esitoQRKO}, {"rip-2026-0999", 0, esitoQRKO}, {" rip - 2026 - 0999 ", 0, esitoQRKO},
		{"RIP-2026-0999", 2, ""}, {"RIP-2026-0999", 1, ""}, {"RIP-2026-45", 0, ""}, {"RIP-2026-0000", 0, ""},
		{"SINT-QR-0045", 0, ""}, {"0045/26", 0, ""}, {"Rossi", 0, ""}, {"", 0, ""},
	} {
		if got := esitoRicercaScritta(c.cerca, c.trovate); got != c.esito {
			t.Errorf("%q, %d: %q, voluto %q", c.cerca, c.trovate, got, c.esito)
		}
	}
}

// inventario: RIP-H-27
// Lo script: la lista di una ricerca scritta suona i due bip gravi solo con l'esito ko del server, dopo
// aver messo la lista al suo posto (mostraPagina), e mai per le risposte dei moduli del pannello; il
// suono passa da suonoQR.suona, quindi rispetta lo sblocco nel tocco su 📷, l'interruttore gs-qr-sound
// e la vibrazione come la 055.
func TestScriptDellaPistola(t *testing.T) {
	js := string(leggiStatico("app.js"))
	contiene(t, "app.js", js,
		`const esitoLista = doc.getElementById("repair-search-outcome")`,
		`const koLista = !dalPannello && esitoLista !== null && esitoLista.getAttribute("data-qr-esito") === "ko"`,
		"    mostraPagina(doc, r.url, r.redirected ? \"aggiungi\" : null)\n    if (koLista) suonoQR.suona(false)\n",
	)
	if n := strings.Count(js, "suonoQR.suona(false)"); n != 1 {
		t.Errorf("suonoQR.suona(false) %d volte", n)
	}
	// Nessun suono nuovo fuori da suonoQR: niente AudioContext né vibrazioni dirette.
	if n := strings.Count(js, "navigator.vibrate("); n != 1 {
		t.Errorf("vibrazioni chieste in %d punti", n)
	}
}

// inventario: RIP-H-28
// Il 📷 di «Etichetta» quando la fotocamera non si apre (contesto non sicuro, browser senza fotocamera,
// nessuna fotocamera o permesso negato): il messaggio nella finestra come il lettore della home, e
// alla chiusura («Chiudi» o Esc) il cursore nel campo dell'etichetta per il lettore a pistola. Il 📷
// della home (con data-submit) non ha ripiego; una chiusura dopo una lettura o un cambio di pagina
// non sposta il cursore.
func TestScriptDelRipiegoEtichetta(t *testing.T) {
	js := string(leggiStatico("app.js"))
	contiene(t, "app.js", js,
		"  function senzaFotocamera(messaggio) {\n    s.stato.textContent = messaggio\n",
		`s.ripiego = p && !p.hasAttribute("data-submit") ? document.getElementById(p.getAttribute("data-target") || "") : null`,
		"      senzaFotocamera(\n        \"⚠ Fotocamera non disponibile: contesto non sicuro",
		`senzaFotocamera("⚠ Fotocamera non disponibile su questo dispositivo o browser.")`,
		`if (viva()) senzaFotocamera("⚠ " + err.message)`,
		"  function chiudiLettore() {\n    s.apertura = null\n    s.ripiego = null\n",
		"    const campo = s.ripiego\n    chiudiLettore()\n    if (campo && document.body.contains(campo)) campo.focus()\n",
		`chiudi.addEventListener("click", chiudiDaUtente)`,
		"return {aperto, chiudi: chiudiDaUtente}",
	)
	if n := strings.Count(js, "senzaFotocamera("); n != 4 { // la definizione e i tre casi
		t.Errorf("senzaFotocamera in %d punti", n)
	}
	if n := strings.Count(js, "s.ripiego = "); n != 2 {
		t.Errorf("s.ripiego scritto in %d punti", n)
	}
	// Il 📷 di «Nuova riparazione» è senza data-submit (quello della modifica, uguale, lo prova
	// TestModificaApreIlModulo in modifica_test.go).
	b, _ := dentroCome(t, accounts.Titolare, nil)
	n := b.vai("GET", "/nuova", nil, nil).corpo
	contiene(t, "nuova", n, `id="repair_qr_code"`, `<button id="repair-qr-code-scan" type="button" data-target="repair_qr_code"`)
}

// inventario: RIP-H-28
// D-78 (Stefano 07/11/2026, slice 059): la linea di mira c'è nella finestra (rossa, a metà del video) e
// si vede solo sopra un video acceso: nascosta all'apertura, accesa da mostra, spenta alla chiusura e
// quando la fotocamera si spegne per un cambio non riuscito. Non tocca la lettura (pointer-events: none).
func TestScriptDellaMira(t *testing.T) {
	js := string(leggiStatico("app.js"))
	contiene(t, "app.js", js,
		"    const mira = s.mira = document.createElement(\"div\")\n    mira.className = \"gs-lettore-mira\"\n    mira.setAttribute(\"aria-hidden\", \"true\")\n    mira.hidden = true\n",
		"    s.flusso = flusso\n    s.mira.hidden = false\n",
		"    if (s.mira) s.mira.hidden = true\n",
		"        s.flusso = null\n        s.mira.hidden = true\n",
	)
	if n := strings.Count(js, "s.mira.hidden = false"); n != 1 {
		t.Errorf("mira accesa in %d punti", n)
	}
	css := string(leggiStatico("app.css"))
	contiene(t, "app.css", css, ".gs-lettore-mira[hidden] { display: none; }", "background: #ef4444;", "pointer-events: none;")
}
