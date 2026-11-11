package web

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/accounts"
)

// Il lettore QR con la fotocamera, il suono dell'esito e il pannello sopra la lista (slice 055). Il
// comportamento nel browser lo prova il banco (scenario browser lettore-qr, con una fotocamera finta
// di Chromium che mostra un QR diritto, uno trasposto e quello di un'etichetta dello standalone);
// qui si fissano jsQR nel binario e le righe di app.js che fanno le regole, come
// qr_scanner_asset_test.exs e qr_sound_asset_test.exs dello standalone.

// inventario: RIP-H-28
// jsQR 1.4.0 è nel binario, uguale byte per byte al file dello standalone (e a dist/jsQR.js di
// jsqr@1.4.0), con la licenza Apache-2.0 accanto; si serve come asset con l'hash nel nome, senza
// sessione, e app.js lo carica da quell'indirizzo (nessun CDN: il negozio lavora anche senza
// internet).
func TestJsQRNelBinario(t *testing.T) {
	b, _ := dentroCome(t, accounts.Titolare, nil)
	js := assetDellaPagina(t, b, b.vai("GET", "/", nil, nil).corpo, "js", "text/javascript; charset=utf-8")
	m := regexp.MustCompile(`const PERCORSO_JSQR = "(/assets/jsqr\.min-[0-9a-f]{12}\.js)"`).FindStringSubmatch(js)
	if m == nil || strings.Contains(js, "__JSQR__") {
		t.Fatal("app.js senza l'indirizzo di jsQR")
	}
	nb := nuovoBrowser(t, b.srv.Config.Handler)
	r := nb.vai("GET", m[1], nil, nil)
	if r.codice != 200 || r.h.Get("Content-Type") != "text/javascript; charset=utf-8" ||
		r.h.Get("Cache-Control") != "public, max-age=31536000, immutable" || r.h.Get("Set-Cookie") != "" {
		t.Fatalf("%s: %d %q %q", m[1], r.codice, r.h.Get("Content-Type"), r.h.Get("Cache-Control"))
	}
	h := sha256.Sum256([]byte(r.corpo))
	const impronta = "bc40c8a15196236b2314db0856f72ca0b49980cd5413b8c852a7349f5fee0859"
	if hex.EncodeToString(h[:]) != impronta {
		t.Errorf("jsqr.min.js cambiato: sha256 %x", h)
	}
	if !strings.Contains(r.corpo, `root["jsQR"] = factory();`) {
		t.Error("jsQR non si mette in window.jsQR")
	}
	// D-52: la riprova sul QR a specchio sulla diagonale (decode di jsQR 1.4.0).
	if !strings.Contains(r.corpo, "    // Decoding didn't work, try mirroring the QR across the topLeft -> bottomRight line.\n    for (var x = 0; x < matrix.width; x++) {\n        for (var y = x + 1; y < matrix.height; y++) {") {
		t.Error("jsQR senza la lettura dei QR a specchio")
	}
	lic, err := os.ReadFile("statici/LICENSE-jsqr.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"jsQR 1.4.0", "https://github.com/cozmo/jsQR", "Apache License, Version 2.0", impronta,
		"Apache License\n                           Version 2.0, January 2004"} {
		if !strings.Contains(string(lic), s) {
			t.Errorf("LICENSE-jsqr.txt senza %q", s)
		}
	}
	// Nessun'altra origine: niente CDN nelle pagine e nello script.
	for _, s := range []string{"https://", "http://", "//cdn", "unpkg", "jsdelivr"} {
		if strings.Contains(js, s) {
			t.Errorf("app.js nomina %q", s)
		}
	}
}

// inventario: RIP-H-28
// Le regole del lettore (qr_scanner.js dello standalone): fotocamera posteriore con la risoluzione
// della sorgente, la principale per numero camera2, la scelta ricordata in gs-camera-qr dentro try,
// «🔄 Cambia fotocamera» e «Una sola fotocamera disponibile.», il contesto non sicuro prima della
// fotocamera assente con i due messaggi della sorgente, jsQR senza inversione, il codice nel campo
// di data-target; con data-submit il modulo parte con il 📷 come mittente (via=qr), senza riempie
// solo il campo e suona il successo. D-52: il QR a specchio sulla diagonale lo legge jsQR stesso
// (decode riprova la griglia trasposta), e app.js non trasforma il fotogramma.
func TestScriptDelLettoreQR(t *testing.T) {
	js := string(leggiStatico("app.js"))
	contiene(t, "app.js", js,
		`const CHIAVE_CAMERA = "gs-camera-qr"`,
		`try { return localStorage.getItem(CHIAVE_CAMERA) } catch (_e) { return null }`,
		`try { localStorage.setItem(CHIAVE_CAMERA, id) } catch (_e) {`,
		`const RISOLUZIONE = {width: {ideal: 1280}, height: {ideal: 720}}`,
		`id ? {deviceId: {exact: id}} : {facingMode: "environment"}`,
		`device.kind === "videoinput"`, `/back|rear|environment|posterior/i`, `/camera2? (\d+)/i`, `match ? +match[1] : 999`,
		`"🔄 Cambia fotocamera"`, `"Una sola fotocamera disponibile."`, `cambia.id = "qr-scanner-switch"`, `chiudi.id = "qr-scanner-close"`,
		`mira.className = "gs-lettore-mira"`,
		"\"⚠ Fotocamera non disponibile: contesto non sicuro (HTTP o certificato non valido), il browser non espone la fotocamera.\"",
		`"⚠ Fotocamera non disponibile su questo dispositivo o browser."`,
		`const OPZIONI = {inversionAttempts: "dontInvert"}`,
		// D-52: jsQR legge da sé il QR a specchio, un fotogramma una volta sola.
		"    const codice = jsQR(img.data, img.width, img.height, OPZIONI)\n    return codice ? codice.data : null",
		// La consegna del codice.
		"    campo.value = testo\n    campo.dispatchEvent(new Event(\"input\", {bubbles: true}))",
		"      if (pulsante.form === modulo) modulo.requestSubmit(pulsante)",
		"    } else if (!pulsante.hasAttribute(\"data-submit\")) {\n      suonoQR.suona(true)\n    }",
		// Il tocco su 📷 sblocca l'audio, poi apre.
		`event.target.closest("#qr-lookup-scan, #repair-qr-code-scan")`,
		"    pulsante.blur()\n    suonoQR.sblocca()\n    apri(pulsante)",
	)
	i, j := strings.Index(js, "if (!window.isSecureContext)"), strings.Index(js, "if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia)")
	if i < 0 || j < 0 || i > j {
		t.Errorf("il contesto sicuro va controllato prima della fotocamera: %d %d", i, j)
	}
	// Il 📷 della home ha il campo e il modulo; quello dell'etichetta solo il campo.
	b, _ := dentroCome(t, accounts.Titolare, nil)
	contiene(t, "home", b.vai("GET", "/", nil, nil).corpo,
		`<button id="qr-lookup-scan" type="submit" name="via" value="qr" data-target="repair-search-code" data-submit="repair-search-form"`)
	n := b.vai("GET", "/nuova", nil, nil).corpo
	contiene(t, "nuova", n, `<button id="repair-qr-code-scan" type="button" data-target="repair_qr_code" class="pulsante-contorno">📷 Scansiona</button>`)
	nonContiene(t, "nuova", n, "data-submit")
}

// inventario: RIP-H-27
// Il suono (qr_sound.js dello standalone): un bip acuto per il successo, due gravi per il
// fallimento, la vibrazione dove c'è; l'interruttore gs-qr-sound con ogni accesso allo storage
// protetto (uno storage che non va lascia il suono acceso); l'audio si sblocca solo nel tocco; l'esito
// è quello del server (data-qr-esito della risposta, o il redirect di un'azione riuscita), mai la
// sola lettura: nella lettura che manda un modulo il lettore non suona.
func TestScriptDelSuonoQR(t *testing.T) {
	js := string(leggiStatico("app.js"))
	contiene(t, "app.js", js,
		"const SUCCESSO = [[1760, 0.0, 0.12]]", "const FALLIMENTO = [[330, 0.0, 0.18], [330, 0.28, 0.18]]",
		`const CHIAVE = "gs-qr-sound"`,
		`try { return localStorage.getItem(CHIAVE) !== "off" } catch (_e) { return true }`,
		`try { localStorage.setItem(CHIAVE, si ? "on" : "off") } catch (_e) {`,
		`if (navigator.audioSession) navigator.audioSession.type = "playback"`,
		"if (!contesto || !acceso()) return",
		"if (navigator.vibrate) navigator.vibrate(ok ? 60 : [120, 80, 120])",
		`el.textContent = si ? "🔊" : "🔇"`,
		`const esito = nuovo.getAttribute("data-qr-esito")`,
		`if (esito === "ok" || esito === "ko") suonoQR.suona(esito === "ok")`,
		"if (dalPannello && r.redirected) suonoQR.suona(true)",
	)
	// Un contesto audio nuovo solo dentro sblocca(), che chiamano solo i tocchi.
	if n := strings.Count(js, "new Contesto()"); n != 1 {
		t.Errorf("AudioContext creato %d volte", n)
	}
	if n := strings.Count(js, "suonoQR.sblocca()"); n != 1 {
		t.Errorf("sblocca chiamato %d volte fuori dal modulo del suono", n)
	}
	// sblocca() c'è tre volte: la definizione, l'interruttore 🔊 e il tocco su 📷 (sopra), mai al
	// caricamento della pagina.
	if n := strings.Count(js, "sblocca()"); n != 3 {
		t.Errorf("sblocca() compare %d volte, attese 3", n)
	}
}

// inventario: RIP-H-22
// D-70, il pannello sopra la lista: con JavaScript, solo dove c'è la lista, il campo unico e i moduli
// del pannello partono con fetch (POST, con il mittente e il suo formaction), il riquadro della
// risposta va sopra la lista senza il campo unico della risposta, «Chiudi» lo toglie; il codice non va
// mai nell'indirizzo (nessuna pushState o replaceState in questo pezzo: le fa solo mostraPagina, con
// l'indirizzo del redirect). Il comportamento lo prova il banco (lettore-qr e pannello-qr).
func TestScriptDelPannelloSopraLaLista(t *testing.T) {
	js := string(leggiStatico("app.js"))
	contiene(t, "app.js", js,
		`const nellaLista = () => document.querySelector("#repairs, #repair-gallery") !== null`,
		`const ricerca = form.id === "repair-search-form"`,
		`const dalPannello = !ricerca && form.closest("#qr-lookup") !== null`,
		`const azione = mittente && mittente.hasAttribute("formaction") ? mittente.formAction : form.action`,
		"if (mittente && mittente.name) corpo.append(mittente.name, mittente.value)",
		`fetch(azione, {method: "POST", body: corpo, credentials: "same-origin", headers: {accept: "text/html"}})`,
		`nuovo.querySelectorAll("#repair-search-form, #repair-search-token").forEach(el => el.remove())`,
		`mostraPagina(doc, r.url, r.redirected ? "aggiungi" : null)`,
		`if (!(event.target.closest && event.target.closest("#qr-lookup-close")) || !nellaLista()) return`,
		"        delete form.dataset.qrInviato\n        connessioneAssente()",
	)
	inizio := strings.Index(js, "// Il pannello del QR sopra la lista (D-70")
	fine := strings.Index(js, "// La coda delle foto scelte prima di generare la busta")
	if inizio < 0 || fine < inizio {
		t.Fatal("pezzo del pannello non trovato")
	}
	// L'unico indirizzo dove il pezzo porta la pagina è «Chiudi» del riquadro dopo l'avviso di ritiro
	// (slice 068): la lista con i filtri che il server ha scritto, mai il codice (paginaQR toglie «cerca»).
	pezzo := strings.Replace(js[inizio:fine], `window.location.assign(chiudi ? chiudi.getAttribute("href") : "/")`, "", 1)
	for _, s := range []string{"pushState", "replaceState", "location.assign", "location.href ="} {
		if strings.Contains(pezzo, s) {
			t.Errorf("il pannello sopra la lista usa %s", s)
		}
	}
	// L'ascoltatore del doppio invio (054) viene prima: il suo preventDefault ferma anche il fetch.
	if a, c := strings.Index(js, `if (form.dataset.qrInviato) { event.preventDefault(); return }`), strings.Index(js, "if (event.defaultPrevented || !(form instanceof HTMLFormElement) || !nellaLista()) return"); a < 0 || c < a {
		t.Errorf("ordine degli ascoltatori: %d %d", a, c)
	}
}
