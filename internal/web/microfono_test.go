package web

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// Il «🎤 Detta a voce» della pagina «Nuova riparazione» (slice 110; RIP-H-44, D-25), come il pulsante #assist-mic,
// l'upload :dictation e il hook Dictation (assets/js/dictation.js) di WorkshopLive dello standalone a go-baseline,
// senza LiveView: il pulsante c'è con la trascrizione configurata (Assist.configured?(:stt)), dentro il riquadro del
// dettato (che vuole l'estrazione); app.js registra WAV mono a 16 kHz (D-25) e manda la clip a POST /nuova/trascrivi
// (slice 109) dal campo file nascosto #assist-clip, come lo standalone la mette nel campo dell'upload. Qui la parte
// del server e le righe di app.js che fanno le regole; il comportamento nel browser lo provano gli scenari
// dettato-microfono (il microfono finto di Chromium) e dettato-clip del banco.

// bloccoMicrofono è il pezzo del modulo del dettato con il microfono: il pulsante prima di «✦ Estrai con AI» e il
// campo della clip, senza nome (il modulo non lo manda a /nuova/estrai), con l'indirizzo della trascrizione.
const bloccoMicrofono = `<div class="pulsanti-dettato">
<button id="assist-mic" type="button" aria-label="Detta a voce">🎤 Detta a voce</button>
<button id="assist-estrai" type="submit">✦ Estrai con AI</button>
</div>
<input id="assist-clip" type="file" hidden data-trascrivi="/nuova/trascrivi">
</form>`

// configuraTrascrizione salva la trascrizione sul trascrittore finto della prova.
func configuraTrascrizione(t *testing.T, p *dettatoProva) {
	t.Helper()
	b := titolareNellAssistente(t, p.h)
	if r := b.vai("POST", "/settings/assistente/trascrizione", moduloAssistente(b, "stt", "endpoint", p.url), nil); r.codice != 302 {
		t.Fatalf("configurazione della trascrizione %d", r.codice)
	}
}

// inventario: RIP-H-44
// Il pulsante si vede solo con la trascrizione configurata e dentro il riquadro del dettato: con la sola estrazione il
// riquadro senza microfono; con la sola trascrizione «🔒 Dettato AI» e nessun microfono (lo standalone lo disegna dentro
// #assist-card). Con tutte e due il pulsante dello standalone (type="button", aria-label «Detta a voce») prima di
// «✦ Estrai con AI», e il campo della clip nel modulo del dettato, per il titolare e il dipendente, anche nelle pagine
// che tornano dopo un'estrazione rifiutata o riuscita. Aprire la pagina non chiede niente al trascrittore.
func TestMicrofonoPulsante(t *testing.T) {
	p := nuovoDettato(t, true)
	r := p.b.vai("GET", "/nuova", nil, nil)
	contiene(t, "sola estrazione", r.corpo, `<article id="assist-card" class="riquadro">`,
		"<div class=\"pulsanti-dettato\">\n<button id=\"assist-estrai\" type=\"submit\">✦ Estrai con AI</button>\n</div>\n</form>")
	nonContiene(t, "sola estrazione", r.corpo, "assist-mic", "Detta a voce", "assist-clip", "/nuova/trascrivi")

	configuraTrascrizione(t, p)
	dip := dipendenteDentro(t, p.h)
	for _, b := range []*browser{p.b, dip} {
		r = b.vai("GET", "/nuova", nil, nil)
		contiene(t, "con la trascrizione", r.corpo, bloccoMicrofono)
		if m := regexp.MustCompile(`(?s)<form id="assist-form"[^>]*>(.*?)</form>`).FindStringSubmatch(r.corpo); m == nil ||
			!strings.Contains(m[1], `<input id="assist-clip" type="file" hidden data-trascrivi="/nuova/trascrivi">`) {
			t.Error("il campo della clip non è nel modulo del dettato")
		}
		if n := strings.Count(r.corpo, `id="assist-mic"`); n != 1 {
			t.Errorf("%d pulsanti del microfono", n)
		}
	}
	// Le pagine che tornano dal dettato hanno ancora il microfono.
	if r = p.estrai("corto", nil); !strings.Contains(r.corpo, "Testo troppo corto") {
		t.Fatalf("rifiuto: %d", r.codice)
	}
	contiene(t, "dopo un rifiuto", r.corpo, bloccoMicrofono)
	if r = p.estrai("Cliente Ida Prova, anello da lucidare", nil); !strings.Contains(r.corpo, "Dati estratti") {
		t.Fatalf("estrazione: %d", r.codice)
	}
	contiene(t, "dopo un'estrazione", r.corpo, bloccoMicrofono)
	if n := len(p.m.RichiesteSTT()); n != 0 {
		t.Errorf("la pagina ha chiesto %d volte al trascrittore", n)
	}

	// Solo la trascrizione: niente riquadro, niente microfono.
	q := nuovoDettato(t, false)
	configuraTrascrizione(t, q)
	r = q.b.vai("GET", "/nuova", nil, nil)
	contiene(t, "sola trascrizione", r.corpo, "🔒 Dettato AI")
	nonContiene(t, "sola trascrizione", r.corpo, "assist-mic", "Detta a voce", "assist-clip")
}

// assistenteSenzaTrascrizione è l'assistente vero con la lettura della trascrizione guasta.
type assistenteSenzaTrascrizione struct{ Assistente }

func (assistenteSenzaTrascrizione) TrascrizioneConfigurata(context.Context) (bool, error) {
	return false, errGuasto
}

// Una lettura della configurazione della trascrizione che non riesce è la pagina d'errore, come quella
// dell'estrazione (la pagina non sa se mostrare il microfono); senza l'estrazione non si legge nemmeno.
func TestMicrofonoConfigurazioneGuasta(t *testing.T) {
	avvolgiAssistente = func(a Assistente) Assistente { return assistenteSenzaTrascrizione{a} }
	t.Cleanup(func() { avvolgiAssistente = nil })
	p := nuovoDettato(t, false)
	if r := p.b.vai("GET", "/nuova", nil, nil); r.codice != 200 || !strings.Contains(r.corpo, "🔒 Dettato AI") {
		t.Errorf("senza estrazione: %d", r.codice)
	}
	b := titolareNellAssistente(t, p.h)
	if r := b.vai("POST", "/settings/assistente/estrazione", moduloAssistente(b, "ai", "endpoint", p.url, "model", "gestrip-prova"), nil); r.codice != 302 {
		t.Fatalf("configurazione %d", r.codice)
	}
	if r := p.b.vai("GET", "/nuova", nil, nil); r.codice != 500 {
		t.Errorf("con l'estrazione e la trascrizione guasta: %d", r.codice)
	}
	if strings.Contains(p.reg.String(), chiaveAssistenteProva) {
		t.Errorf("registro %q", p.reg.String())
	}
}

// inventario: RIP-H-44
// Il microfono in app.js (dictation.js dello standalone, con la D-25): i testi copiati (contesto non sicuro, niente
// microfono, microfono negato con il messaggio del browser, «⚠ Troppo breve, riprova.» sotto i 600 ms, «⏳ Trascrivo…»,
// «🔴 Sto ascoltando… (tocca per fermare)»), un secondo tocco mentre si chiede il permesso non apre un secondo flusso,
// getUserMedia({audio: true}) come lo standalone; la registrazione con un AudioWorklet (mai MediaRecorder: niente
// webm), ricampionata a 16 kHz e scritta come WAV PCM mono a 16 bit (RIFF, fmt di 16 byte, data), al più 25 MiB; la
// clip nel campo #assist-clip (DataTransfer, come lo standalone la mette nel campo dell'upload) con il nome «dettato»
// e il tipo audio/wav, poi POST /nuova/trascrivi con il CSRF in testa e il campo dictation; il redirect a /login porta
// a /login (la sessione revocata, handle_dictation/3); il testo in fondo alla casella come append_dictated/2; una
// risposta che non è il JSON della 109 (413, pagina d'errore, rete giù) è «⚠ Trascrizione non disponibile…». Niente
// HTML scritto, niente audio tenuto (né in IndexedDB né in localStorage). Il rilascio del microfono nel browser (tracce
// ferme e contesti chiusi dopo ogni caso) lo guarda lo scenario dettato-microfono.
func TestMicrofonoNelloScript(t *testing.T) {
	js := string(leggiStatico("app.js"))
	i := strings.Index(js, "// «🎤 Detta a voce» (slice 110")
	if i < 0 {
		t.Fatal("app.js senza il microfono")
	}
	blocco := js[i:]
	if j := strings.Index(blocco, "\n})()\n"); j > 0 {
		blocco = blocco[:j]
	}
	contiene(t, "microfono", blocco,
		"const MIN_MS = 600", "const FREQUENZA = 16000", "const MASSIMO_CAMPIONI = Math.floor((25 * 1024 * 1024 - 44) / 2)",
		`"⚠ Microfono non disponibile: contesto non sicuro (HTTP o certificato non valido), il browser non espone il microfono."`,
		`"⚠ Microfono non disponibile su questo dispositivo o browser."`,
		"`⚠ Microfono negato: ${error.message} — scrivi a mano.`",
		`"⚠ Troppo breve, riprova."`, `"⏳ Trascrivo…"`, `"🔴 Sto ascoltando… (tocca per fermare)"`,
		`"⚠ Trascrizione non disponibile, scrivi a mano e premi Estrai."`,
		"if (!window.isSecureContext)", "if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia)",
		"if (avvio) return", "avvio = pulsante", "navigator.mediaDevices.getUserMedia({audio: true})",
		"new ContestoAudio()", "registerProcessor(", "new AudioWorkletNode(", `channelCount: 1, channelCountMode: "explicit"`,
		"new Ricampionatore(contesto.sampleRate)", "this.passo = da / FREQUENZA",
		`scrivi(0, "RIFF")`, "vista.setUint32(4, 36 + dati, true)", `scrivi(8, "WAVE")`, `scrivi(12, "fmt ")`,
		"vista.setUint32(16, 16, true)", "vista.setUint16(20, 1, true)", "vista.setUint16(22, 1, true)",
		"vista.setUint32(24, FREQUENZA, true)", "vista.setUint32(28, FREQUENZA * 2, true)", "vista.setUint16(32, 2, true)",
		"vista.setUint16(34, 16, true)", `scrivi(36, "data")`, "vista.setUint32(40, dati, true)",
		`new File([wav(campioni)], "dettato", {type: "audio/wav"})`, "new DataTransfer()",
		`input.dispatchEvent(new Event("change", {bubbles: true}))`,
		`dati.append("_csrf_token", token ? token.value : "")`, `dati.append("dictation", file, file.name || "dettato")`,
		`if (new URL(r.url).pathname === "/login") { window.location.assign("/login"); return }`,
		`casella.value = casella.value === "" ? testo : casella.value.trim() + " " + testo`,
		`stato.classList.toggle("text-error", tipo === "error")`,
		// Revisione della 110: una pagina cambiata senza ricaricare (gs:pagina) butta la registrazione del pulsante che non
		// c'è più, senza mandarla (destroyed() → release() dello standalone), e libera l'avvio; il microfono staccato o il
		// permesso tolto (ended) ferma e manda la clip; mentre una clip si trascrive il pulsante e il campo non fanno niente.
		`document.addEventListener("gs:pagina", () => {`,
		"if (registrazione && !registrazione.pulsante.isConnected) scarta(registrazione)",
		"if (avvio && !avvio.isConnected) avvio = null",
		"if (!pulsante.isConnected) {",
		`track.addEventListener("ended", () => { if (registrazione === r) ferma() })`,
		"if (trascrivendo) return", "if (trascrivendo) { input.value = \"\"; return }", "trascrivendo = true", "trascrivendo = false")
	nonContiene(t, "microfono", blocco, "innerHTML", "insertAdjacentHTML", "MediaRecorder", "indexedDB", "localStorage", "sessionStorage")
	if strings.Index(blocco, `dati.append("_csrf_token"`) > strings.Index(blocco, `dati.append("dictation"`) {
		t.Error("il token deve venire prima della clip (csrf.ModuloConFile)")
	}
	nonContiene(t, "app.js", js, "MediaRecorder")
}
