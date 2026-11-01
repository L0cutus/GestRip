package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/imaging/sintetiche"
)

func TestLeggiScenario(t *testing.T) {
	s, err := leggiScenario("healthz.json", strings.NewReader(`{
		"nome": "healthz",
		"inventario": ["ADM-20"],
		"passi": [{"metodo": "GET", "percorso": "/healthz?ignored=x",
		           "confronta": ["codice", "corpo", "intestazione:content-type", "intestazione:set-cookie"]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Nome != "healthz" || len(s.Passi) != 1 || s.Passi[0].Percorso != "/healthz?ignored=x" {
		t.Errorf("scenario = %+v", s)
	}
}

func TestLeggiScenarioRifiuta(t *testing.T) {
	casi := map[string]string{
		"campo ignoto":       `{"nome":"x","passi":[{"metodo":"GET","percorso":"/","confronta":["colore"]}]}`,
		"senza passi":        `{"nome":"x","passi":[]}`,
		"percorso assoluto":  `{"nome":"x","passi":[{"metodo":"GET","percorso":"https://altro/","confronta":["codice"]}]}`,
		"chiave sconosciuta": `{"nome":"x","pasi":[]}`,
		"inventario errato":  `{"nome":"x","inventario":["adm20"],"passi":[{"metodo":"GET","percorso":"/","confronta":["codice"]}]}`,
		// Il client Go le ignora o le riscrive: lo scenario sarebbe verde senza provare niente.
		"intestazione Host":       `{"nome":"x","passi":[{"metodo":"GET","percorso":"/","intestazioni":{"host":"a"},"confronta":["codice"]}]}`,
		"intestazione lunghezza":  `{"nome":"x","passi":[{"metodo":"GET","percorso":"/","intestazioni":{"Content-Length":"1"},"confronta":["codice"]}]}`,
		"intestazione Connection": `{"nome":"x","passi":[{"metodo":"GET","percorso":"/","intestazioni":{"Connection":"close"},"confronta":["codice"]}]}`,
		"intestazione Transfer":   `{"nome":"x","passi":[{"metodo":"GET","percorso":"/","intestazioni":{"Transfer-Encoding":"chunked"},"confronta":["codice"]}]}`,
	}
	for nome, src := range casi {
		if _, err := leggiScenario("x.json", strings.NewReader(src)); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
}

func esito(codice int, corpo string, h map[string][]string) risposta {
	return risposta{Codice: codice, Corpo: corpo, Intestazioni: h}
}

func TestConfrontaUguali(t *testing.T) {
	p := passo{Confronta: []string{"codice", "corpo", "intestazione:cache-control", "intestazione:set-cookie"}}
	a := esito(200, "ok", map[string][]string{"Cache-Control": {"no-store"}, "Server": {"Caddy"}})
	b := esito(200, "ok", map[string][]string{"Cache-Control": {"no-store"}})
	if d := confronta(p, a, b); len(d) != 0 {
		t.Errorf("differenze inattese: %v", d)
	}
}

func TestConfrontaTrovaLeDifferenze(t *testing.T) {
	p := passo{Confronta: []string{"codice", "corpo", "intestazione:cache-control", "intestazione:set-cookie"}}
	a := esito(200, "ok", map[string][]string{"Cache-Control": {"no-store"}})
	b := esito(503, "unavailable", map[string][]string{"Cache-Control": {"private"}, "Set-Cookie": {"x=1"}})
	d := confronta(p, a, b)
	campi := map[string]bool{}
	for _, x := range d {
		campi[x.Campo] = true
	}
	for _, c := range []string{"codice", "corpo", "intestazione:cache-control", "intestazione:set-cookie"} {
		if !campi[c] {
			t.Errorf("differenza su %s non trovata: %v", c, d)
		}
	}
}

func TestDeviazioniCopronoSoloLaDifferenzaDichiarata(t *testing.T) {
	devs, err := leggiDeviazioni(strings.NewReader(`[
		{"id": "D-05", "scenario": "blocco", "passo": 0, "campo": "intestazione:cache-control",
		 "standalone": "no-store", "go": "no-store, private", "motivo": "esempio"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	attesa := differenza{Scenario: "blocco", Passo: 0, Campo: "intestazione:cache-control", Standalone: "no-store", Go: "no-store, private"}
	if id, ok := devs.copre(attesa); !ok || id != "D-05" {
		t.Errorf("deviazione dichiarata non riconosciuta: %q %v", id, ok)
	}
	altro := attesa
	altro.Go = "public"
	if _, ok := devs.copre(altro); ok {
		t.Error("una deviazione con un valore diverso da quello dichiarato è stata accettata")
	}
	altro = attesa
	altro.Scenario = "altro"
	if _, ok := devs.copre(altro); ok {
		t.Error("deviazione applicata allo scenario sbagliato")
	}
}

func TestLeggiDeviazioniRifiutaIDNonValidi(t *testing.T) {
	if _, err := leggiDeviazioni(strings.NewReader(`[{"id":"X-1","scenario":"a","passo":0,"campo":"codice","standalone":"1","go":"2","motivo":"m"}]`)); err == nil {
		t.Error("ID di deviazione non valido accettato")
	}
	if _, err := leggiDeviazioni(strings.NewReader(`[{"id":"D-01","scenario":"a","passo":0,"campo":"codice","standalone":"1","go":"2"}]`)); err == nil {
		t.Error("deviazione senza motivo accettata")
	}
}

func TestDeviazioniApprovateNellInventario(t *testing.T) {
	inv := "| ID | Voce |\n|---|---|\n| D-01 | RIP-S-46 | x | y | Stefano |\n| D-05 | CLI-21 | x | y | Stefano |\n"
	ok, err := deviazioniApprovate(strings.NewReader(inv))
	if err != nil {
		t.Fatal(err)
	}
	if !ok["D-01"] || !ok["D-05"] || ok["D-02"] {
		t.Errorf("approvate = %v", ok)
	}
}

// «presenza:nome» confronta solo se l'intestazione c'è, non il valore (x-request-id).
func TestValorePresenza(t *testing.T) {
	r := risposta{Intestazioni: http.Header{"X-Request-Id": {"abc"}}}
	if r.valore("presenza:x-request-id") != "presente" || (risposta{}).valore("presenza:x-request-id") != "assente" {
		t.Errorf("presenza: %q", r.valore("presenza:x-request-id"))
	}
	if !campoValido("presenza:x-request-id") || campoValido("presenza:") || campoValido("presenza:X-Request-Id") {
		t.Error("campoValido sbagliato per presenza:")
	}
}

// «visibile:id» è il testo visibile dell'elemento con quell'id: i tag tolti (quelli di blocco e
// <br> valgono uno spazio, quelli in linea niente), le entità decodificate, gli spazi compattati. Così due pagine con markup diverso
// (Phoenix e html/template) si confrontano sul testo che legge chi lavora, «+» e «'» compresi
// (html/template li scrive &#43; e &#39;).
func TestValoreVisibile(t *testing.T) {
	corpo := `<html><body>
<div id="fuori"><dd id="repair-recipient">
  <a href="tel:+39">&#43;39 000</a>
  <span class="x">Oreficeria <b>Rossi</b></span>
</dd><div id="annidato"><div>uno <div>due</div></div> tre</div><div>dopo</div></div>
<span id='apice'>niente</span><input id="vuoto" value="x"><p id="ultimo">l&#39;oreficeria &amp; co.<!-- nota --> fine</p>
<dd id="repair-recipient-bis">non io</dd>
</body></html>`
	r := risposta{Corpo: corpo}
	casi := map[string]string{
		"visibile:repair-recipient": "+39 000 Oreficeria Rossi",
		"visibile:annidato":         "uno due tre",
		"visibile:fuori":            "+39 000 Oreficeria Rossi uno due tre dopo",
		"visibile:vuoto":            "",
		"visibile:ultimo":           "l'oreficeria & co. fine",
		"visibile:manca":            "(assente)",
		"visibile:apice":            "(assente)", // solo id="…" con le virgolette doppie, come scrivono le due app
	}
	for campo, atteso := range casi {
		if got := r.valore(campo); got != atteso {
			t.Errorf("%s = %q, atteso %q", campo, got, atteso)
		}
	}
	// Un tag uguale chiuso da sé non apre un livello; un data-id non è l'id; un commento con «>»
	// dentro sparisce intero.
	for _, c := range []struct{ corpo, id, atteso string }{
		{`<div id="s">a<div/>b</div>c`, "s", "a b"},
		{`<i data-id="x">no</i><i id="x">sì</i>`, "x", "sì"},
		{`<p id="c">prima<!-- a > b -->dopo</p>`, "c", "primadopo"},
	} {
		if got := (risposta{Corpo: c.corpo}).valore("visibile:" + c.id); got != c.atteso {
			t.Errorf("%s: %q, atteso %q", c.corpo, got, c.atteso)
		}
	}
	// Un elemento non chiuso: tutto il resto del corpo, senza bloccarsi.
	if got := (risposta{Corpo: `<p id="a">resto <b>qui`}).valore("visibile:a"); got != "resto qui" {
		t.Errorf("non chiuso: %q", got)
	}
	// Due id uguali: conta il primo.
	if got := (risposta{Corpo: `<i id="d">primo</i><i id="d">secondo</i>`}).valore("visibile:d"); got != "primo" {
		t.Errorf("doppio: %q", got)
	}
	for _, c := range []string{"visibile:repair-detail", "visibile:a_b", "visibile:A1"} {
		if !campoValido(c) {
			t.Errorf("%s rifiutato", c)
		}
	}
	for _, c := range []string{"visibile:", "visibile:a b", `visibile:a"`, "visibile:-a", "visibile:a>"} {
		if campoValido(c) {
			t.Errorf("%s accettato", c)
		}
	}
	// Un'attesa su visibile: si confronta con il testo, anche «(assente)».
	if err := attesaValida(map[string]string{"visibile:repair-status": "In lavorazione"}); err != nil {
		t.Error(err)
	}
	p := passo{Confronta: []string{"visibile:x"}}
	a := risposta{Corpo: `<b id="x">€ 1,00</b>`}
	b := risposta{Corpo: `<span class="y" id="x">€  1,00 </span>`}
	if d := confronta(p, a, b); len(d) != 0 {
		t.Errorf("stesso testo visibile: %v", d)
	}
	b.Corpo = `<span id="x">€ 1,01</span>`
	if d := confronta(p, a, b); len(d) != 1 {
		t.Errorf("testo diverso non visto: %v", d)
	}
}

// Review della 032: un tag in linea non aggiunge spazio («333<span>111</span>» non è «333 111»),
// uno di blocco e <br> sì; i commenti spariscono senza spazio.
func TestVisibileTagInLinea(t *testing.T) {
	for _, c := range []struct{ corpo, atteso string }{
		{`<dd id="t">333<span>111</span></dd>`, "333111"},
		{`<dd id="t">333<div>111</div></dd>`, "333 111"},
		{`<dd id="t">uno<br>due<br/>tre</dd>`, "uno due tre"},
		{`<p id="t">a<strong>b</strong><em>c</em><a href="x">d</a><small>e</small><code>f</code><i>g</i><b>h</b></p>`, "abcdefgh"},
		{`<div id="t"><dt>Data</dt><dd>05/01/2026</dd></div>`, "Data 05/01/2026"},
		{`<p id="t">a<!-- x -->b</p>`, "ab"},
	} {
		if got := (risposta{Corpo: c.corpo}).valore("visibile:t"); got != c.atteso {
			t.Errorf("%s: %q, atteso %q", c.corpo, got, c.atteso)
		}
	}
	a := risposta{Corpo: `<dd id="t">333<span>111</span></dd>`}
	b := risposta{Corpo: `<dd id="t">333 <span>111</span></dd>`}
	if d := confronta(passo{Confronta: []string{"visibile:t"}}, a, b); len(d) != 1 {
		t.Errorf("«333111» e «333 111» uguali: %v", d)
	}
}

// «grezzo:» è il testo con gli a capo e gli spazi intatti (tag tolti senza spazio, entità
// decodificate): una riga vuota in testa a un riquadro pre-wrap è una differenza.
func TestValoreGrezzo(t *testing.T) {
	r := risposta{Corpo: "<dd id=\"g\" class=\"whitespace-pre-wrap\">Anello\nSaldatura &amp; <b>oro</b>  </dd>"}
	if got := r.valore("grezzo:g"); got != "Anello\nSaldatura & oro  " {
		t.Errorf("grezzo %q", got)
	}
	if got := r.valore("visibile:g"); got != "Anello Saldatura & oro" {
		t.Errorf("visibile %q", got)
	}
	a := risposta{Corpo: `<dd id="g">Nota</dd>`}
	b := risposta{Corpo: "<dd id=\"g\">\nNota</dd>"}
	if d := confronta(passo{Confronta: []string{"grezzo:g"}}, a, b); len(d) != 1 {
		t.Errorf("riga vuota in testa non vista: %v", d)
	}
	if d := confronta(passo{Confronta: []string{"visibile:g"}}, a, b); len(d) != 0 {
		t.Errorf("visibile vede la riga vuota: %v", d)
	}
	if got := (risposta{Corpo: `<p>x</p>`}).valore("grezzo:g"); got != "(assente)" {
		t.Errorf("assente %q", got)
	}
}

// I localizzatori: un id, oppure «.classe@n» (l'n-esimo elemento con quella classe), seguiti
// facoltativamente da «/tag» o «/tag@n» (il primo o l'n-esimo tag con quel nome dentro).
func TestLocalizzatori(t *testing.T) {
	corpo := `<section id="sez"><dl><div><dt>Data</dt><dd>05/01</dd></div><div><dt>Cliente</dt><dd>Ada</dd></div></dl>
<dd class="x whitespace-pre-wrap">uno</dd><dd class="whitespace-pre-wrap y">due</dd><dd class="whitespace-pre-wrapper">no</dd>
<dd id="rec"><a href="tel:333 111" class="u">333 111</a></dd></section>`
	r := risposta{Corpo: corpo}
	for campo, atteso := range map[string]string{
		"visibile:sez/dd":                        "05/01",
		"visibile:sez/dd@2":                      "Ada",
		"visibile:sez/dd@9":                      "(assente)",
		"visibile:sez/dl":                        "Data 05/01 Cliente Ada",
		"grezzo:.whitespace-pre-wrap@1":          "uno",
		"grezzo:.whitespace-pre-wrap@2":          "due",
		"grezzo:.whitespace-pre-wrap@3":          "(assente)",
		"visibile:.u@1":                          "333 111",
		"attributo:rec/a:href":                   "tel:333 111",
		"attributo:.whitespace-pre-wrap@2:class": "whitespace-pre-wrap y",
		"attributo:rec/a:title":                  "(senza attributo)",
		"attributo:manca/a:href":                 "(assente)",
	} {
		if got := r.valore(campo); got != atteso {
			t.Errorf("%s = %q, atteso %q", campo, got, atteso)
		}
	}
}

// «attributo:loc:nome» è il valore dell'attributo nel tag di apertura, entità decodificate (le
// due app scappano diversamente: &quot; e &#34;); un attributo senza valore è «».
func TestValoreAttributo(t *testing.T) {
	a := risposta{Corpo: `<a id="t" href="tel:33&quot;3&lt;x&gt; 1">x</a><input id="q" required>`}
	b := risposta{Corpo: `<a href="tel:33&#34;3&lt;x&gt; 1" id="t">y</a>`}
	if got := a.valore("attributo:t:href"); got != `tel:33"3<x> 1` {
		t.Errorf("href %q", got)
	}
	if d := confronta(passo{Confronta: []string{"attributo:t:href"}}, a, b); len(d) != 0 {
		t.Errorf("stesso href: %v", d)
	}
	if got := a.valore("attributo:q:required"); got != "" {
		t.Errorf("booleano %q", got)
	}
	b.Corpo = `<a id="t" href="tel:333%20111">x</a>`
	a.Corpo = `<a id="t" href="tel:333 111">x</a>`
	if d := confronta(passo{Confronta: []string{"attributo:t:href"}}, a, b); len(d) != 1 {
		t.Errorf("%%20 e spazio uguali: %v", d)
	}
	// data-href non è href.
	if got := (risposta{Corpo: `<a id="t" data-href="x">y</a>`}).valore("attributo:t:href"); got != "(senza attributo)" {
		t.Errorf("data-href %q", got)
	}
}

func TestCampiNuoviValidi(t *testing.T) {
	for _, c := range []string{"grezzo:a", "grezzo:.whitespace-pre-wrap@1", "visibile:sez/dd@2", "visibile:sez/dl",
		"attributo:rec/a:href", "attributo:t:data-x", "attributo:.c@1:class"} {
		if !campoValido(c) {
			t.Errorf("%s rifiutato", c)
		}
	}
	for _, c := range []string{"grezzo:", "grezzo:.c", "grezzo:.c@0", "visibile:a/", "visibile:a/dd@0", "visibile:a/DD",
		"attributo:a", "attributo:a:", "attributo::href", "attributo:a:Href", "attributo:a:h ref", "visibile:a/b/c"} {
		if campoValido(c) {
			t.Errorf("%s accettato", c)
		}
	}
}

// Per le foto (slice 037): «impronta» è lo SHA-256 esadecimale del corpo, «lunghezza» i suoi byte,
// «dimensioni» larghezza×altezza dell'immagine che il corpo contiene (JPEG, PNG o WebP), o «(non
// un'immagine)». Così si confrontano i byte di una foto senza metterli nell'esito, e una miniatura
// fatta in Go con una fatta da Pillow (byte diversi per scelta, tema H) dalle dimensioni.
func TestValoreImprontaLunghezzaDimensioni(t *testing.T) {
	for _, c := range []string{"impronta", "lunghezza", "dimensioni"} {
		if !campoValido(c) {
			t.Errorf("%s non valido", c)
		}
	}
	r := risposta{Corpo: "abc"}
	if got := r.valore("impronta"); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Errorf("impronta %s", got)
	}
	if got := r.valore("lunghezza"); got != "3" {
		t.Errorf("lunghezza %s", got)
	}
	if got := r.valore("dimensioni"); got != "(non un'immagine)" {
		t.Errorf("dimensioni %s", got)
	}
	j, err := fotoSintetica()
	if err != nil {
		t.Fatal(err)
	}
	if got := (risposta{Corpo: string(j)}).valore("dimensioni"); got != "64x48" {
		t.Errorf("dimensioni del JPEG %s", got)
	}
	webp := sintetiche.WebP(30, 20, 1)
	if got := (risposta{Corpo: string(webp)}).valore("dimensioni"); got != "30x20" {
		t.Errorf("dimensioni del WebP %s", got)
	}
}
