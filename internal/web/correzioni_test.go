package web

import (
	"strconv"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/printing"
)

// Le correzioni decise da Stefano il 06/11/2026 (slice 055d): la busta senza etichetta nella ricerca
// per numero, il suggerimento senza «telefono», l'avviso della forma non riconosciuta, il messaggio
// della firma Compro Oro (D-75), i segnaposto delle opzioni di stampa.

// inventario: RIP-H-03, RIP-H-56
// D-64 punto a: «0012», «12» e «12/26» trovano l'etichetta 0012/25 e la busta senza etichetta 12/26,
// che porta «⚠ senza QR» con il suo titolo anche se è chiusa o ha un codice di altro formato; la busta
// con l'etichetta no. Fuori dalla ricerca per numero una busta chiusa non ha il segno; una aperta con un
// altro codice sì, con il titolo della D-79 (slice 059).
func TestHomeRicercaNumeroInterno(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	s := seminaRicerca(t, d, o)
	chiusa := bustaHome(t, d, o, "13/26", "", `stato = 'consegnata'`)
	altroCodice := bustaHome(t, d, o, "14/26", "QR-ALTRO", ``)
	b := titolareDentro(t, h)
	segno := func(id int64, titolo string) string {
		return `<span id="repair-noqr-` + idTesto(id) + `" class="segno segno-noqr" title="` + titolo + `">⚠ senza QR</span>`
	}
	const interno = "Busta senza etichetta: trovata con il numero interno"
	for _, cerca := range []string{"0012", "12", "12%2F26"} {
		corpo := b.vai("GET", "/?cerca="+cerca, nil, nil).corpo
		contiene(t, cerca, corpo, segno(s.dodici, interno))
		nonContiene(t, cerca, corpo, `id="repair-noqr-`+idTesto(s.dodici25)+`"`)
	}
	contiene(t, "13", b.vai("GET", "/?cerca=13", nil, nil).corpo, segno(chiusa, interno))
	contiene(t, "14", b.vai("GET", "/?cerca=0014", nil, nil).corpo, segno(altroCodice, interno))
	corpo := b.vai("GET", "/?cerca=Cliente", nil, nil).corpo
	// D-79 (slice 059): fuori dalla ricerca per numero la busta aperta con un codice di altro formato
	// porta il segno con il suo titolo; quella chiusa no.
	contiene(t, "testo", corpo, segno(s.dodici, "Busta senza codice QR: aggiungilo dalla scheda"),
		segno(altroCodice, "Busta senza etichetta RIP: il suo QR è di un altro formato"))
	nonContiene(t, "testo", corpo, `id="repair-noqr-`+idTesto(chiusa)+`"`, interno)
}

// inventario: RIP-H-03
// D-63: con un numero d'etichetta (da 1 a 4 cifre) il suggerimento sotto il campo non dice «telefono»;
// con ogni altro testo è quello dello standalone. I due testi stanno anche negli attributi per app.js.
func TestSuggerimentoRicerca(t *testing.T) {
	for _, c := range []struct {
		cerca string
		cifre bool
	}{{"", false}, {"45", true}, {"0045/26", true}, {"3331", true}, {"Rossi", false}, {"33312", false}, {"0", false}, {"45/2026", false}} {
		got := campoRicerca{Cerca: c.cerca}.Suggerimento()
		if (got == suggerimentoCifre) != c.cifre || strings.Contains(got, "telefono") == c.cifre {
			t.Errorf("%q: %q", c.cerca, got)
		}
	}
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	seminaRicerca(t, d, o)
	b := titolareDentro(t, h)
	contiene(t, "45", b.vai("GET", "/?cerca=45", nil, nil).corpo,
		`<small id="repair-search-hint" data-cifre="`+suggerimentoCifre+`" data-testo="`+suggerimentoTesto+`">`+suggerimentoCifre+`</small>`)
	js := string(leggiStatico("app.js"))
	contiene(t, "app.js", js, `campo.matches("#repair-search-code")`, `document.getElementById("repair-search-hint")`,
		`/^([0-9]{1,4})(?:\/[0-9]{2})?$/`, `aiuto.dataset[cifre ? "cifre" : "testo"]`)
}

// inventario: RIP-H-39
// «Nuova riparazione»: un codice simile a un'etichetta («45/2026») si avvisa una volta, con il campo
// nascosto forma_avvisata, senza salvare; premendo di nuovo si salva com'è. «rip-2026-0045» si salva
// in maiuscolo senza avviso.
func TestNuovaFormaNonRiconosciuta(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	b.vai("GET", "/nuova", nil, nil)
	prima := contaBuste(t, d)
	r := b.vai("POST", "/nuova", con(bustaProva(b), "repair[qr_code]", " 45/2026 "), nil)
	if r.codice != 200 {
		t.Fatalf("avviso: %d %q", r.codice, r.location)
	}
	contiene(t, "avviso", r.corpo, `<div id="intake-qr-code-row" class="intake-qr-row intake-qr-missing">`,
		`<p id="intake-qr-format" role="alert">`+"\n⚠ «45/2026» non è un'etichetta riconosciuta: scrivila come 0045/26 o 45, oppure premi di nuovo\n«Genera busta» per salvarla così com'è.\n</p>",
		`<input name="forma_avvisata" type="hidden" value="45/2026">`, `<button type="submit">Genera busta</button>`)
	nonContiene(t, "avviso", r.corpo, `id="intake-qr-warning"`, `id="intake-qr-twins"`)
	if n := contaBuste(t, d); n != prima {
		t.Fatalf("l'avviso ha salvato: %d", n-prima)
	}
	// Un errore dei campi dopo l'avviso tiene il codice avvisato.
	r = b.vai("POST", "/nuova", con(con(con(bustaProva(b), "repair[qr_code]", "45/2026"), "forma_avvisata", "45/2026"), "repair[description]", ""), nil)
	contiene(t, "errore dopo l'avviso", r.corpo, `<input name="forma_avvisata" type="hidden" value="45/2026">`, "Campo obbligatorio.")
	nonContiene(t, "errore dopo l'avviso", r.corpo, `id="intake-qr-format"`)
	if r := b.vai("POST", "/nuova", con(con(bustaProva(b), "repair[qr_code]", "45/2026"), "forma_avvisata", "45/2026"), nil); !creata(r) {
		t.Fatalf("salva com'è: %d %s", r.codice, r.corpo)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM buste WHERE codice_qr = '45/2026'`); n != 1 {
		t.Errorf("buste con 45/2026: %d", n)
	}
	b.vai("GET", "/nuova", nil, nil)
	if r := b.vai("POST", "/nuova", con(bustaProva(b), "repair[qr_code]", "rip-2026-0045"), nil); !creata(r) {
		t.Fatalf("minuscolo: %d %s", r.codice, r.corpo)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM buste WHERE codice_qr = 'RIP-2026-0045'`); n != 1 {
		t.Errorf("RIP-2026-0045: %d", n)
	}
}

// inventario: RIP-S-04, RIP-H-39
// La modifica avvisa allo stesso modo sotto il campo «Etichetta»; il modulo rimandato com'è salva.
func TestModificaFormaNonRiconosciutaWeb(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.CodiceQR = "RIP-2026-0045" })
	b := titolareDentro(t, h)
	r := salva(t, b, id, map[string]string{"repair[qr_code]": "0045-26"})
	if r.codice != 200 {
		t.Fatalf("avviso: %d %q", r.codice, r.location)
	}
	contiene(t, "avviso", r.corpo, `<p id="repair-qr-format" role="alert">`+"\n⚠ «0045-26» non è un'etichetta riconosciuta: scrivila come 0045/26 o 45, oppure premi di nuovo\n«Salva modifiche» per salvarla così com'è.\n</p>",
		`<input name="forma_avvisata" type="hidden" value="0045-26">`, `id="repair_qr_code" value="0045-26"`)
	if got := colonna(t, d, id, "codice_qr"); got != "RIP-2026-0045" {
		t.Fatalf("salvato dopo l'avviso: %q", got)
	}
	if r := b.vai("POST", azioneModifica(id), moduloDi(t, r.corpo), nil); !redirectAllaScheda(r, id) {
		t.Fatalf("rimandato: %d %q", r.codice, r.location)
	}
	if got := colonna(t, d, id, "codice_qr"); got != "0045-26" {
		t.Errorf("salvato com'è: %q", got)
	}
}

// inventario: RIP-S-38
// D-75: dopo la firma di una scheda Compro Oro «Firma registrata: pratica chiusa.»; per una busta
// normale il messaggio dello standalone (TestSalvaFirma).
func TestSalvaFirmaCompraOro(t *testing.T) {
	p := facciaConFirma(t)
	id := bustaScheda(t, p.d, p.o, func(a *buste.Accettazione) { a.CompraOro = "true" })
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'perfezionata' WHERE id = ?`, id)
	b := p.titolare(t)
	if r := postFirma(b, id, "data:image/png;base64,"+firmaPNG, "Ada"); !redirectA(r, percorsoScheda(id)) {
		t.Fatalf("%d %q", r.codice, r.location)
	}
	corpo := b.vai("GET", percorsoScheda(id), nil, nil).corpo
	contiene(t, "compro oro", corpo, `<div role="status" class="flash flash-info">Firma registrata: pratica chiusa.</div>`)
	nonContiene(t, "compro oro", corpo, "riparazione consegnata.")
	if v := contaCon(t, p.d, `SELECT count(*) FROM buste WHERE id = ? AND stato = 'chiusa' AND firma_chiave IS NOT NULL`, id); v != 1 {
		t.Errorf("busta: %d", v)
	}
	// La busta che non si rilegge (un id non valido non arriva qui): il messaggio dello standalone.
	f := &facciaWeb{buste: p.b}
	if m := f.messaggioFirma(t.Context(), "x"); m != msgFirmaRegistrata {
		t.Errorf("id non valido: %q", m)
	}
	if m := f.messaggioFirma(t.Context(), strconv.FormatInt(p.busta+1000, 10)); m != msgFirmaRegistrata {
		t.Errorf("busta che non c'è: %q", m)
	}
}

// inventario: RIP-S-48
// Il segnaposto delle opzioni: le opzioni dichiarate dalla stampante scelta (le prime due che si
// possono scrivere, con il loro valore di adesso), altrimenti l'esempio del tipo di stampante.
func TestSegnapostoOpzioni(t *testing.T) {
	for _, c := range []struct {
		id         string
		dichiarate []printing.OpzioneCoda
		voluto     string
	}{
		{"qr", nil, "MediaTracking=Gap Darkness=8"},
		{"busta", nil, "Darkness=12 PrintSpeed=3"},
		{"busta", []printing.OpzioneCoda{{Chiave: "PageSize", Scelte: []string{"A4"}, Predefinita: "A4"},
			{Chiave: "Resolution", Scelte: []string{"203dpi", "300dpi"}, Predefinita: "300dpi"},
			{Chiave: "Vuota"}, {Chiave: "zeMediaTracking", Scelte: []string{"Web", "Mark"}},
			{Chiave: "Darkness", Scelte: []string{"1"}, Predefinita: "1"}}, "Resolution=300dpi zeMediaTracking=Web"},
		{"qr", []printing.OpzioneCoda{{Chiave: "PageRegion", Predefinita: "w162h90"}}, "MediaTracking=Gap Darkness=8"},
	} {
		if got := segnapostoOpzioni(c.id, c.dichiarate); got != c.voluto {
			t.Errorf("%s %v: %q, voluto %q", c.id, c.dichiarate, got, c.voluto)
		}
	}
	h, d, _ := facciaConStampante(t, fintaProva())
	b := titolareDentro(t, h)
	scegliStampanti(t, d, "Etichette_QR", "", "", "")
	corpo := b.vai("GET", "/settings/stampanti", nil, nil).corpo
	contiene(t, "pagina", corpo, `id="printers-qr-options" name="opzioni_qr" value="" placeholder="MediaTracking=Gap Darkness=8"`,
		`id="printers-busta-options" name="opzioni_busta" value="" placeholder="Darkness=12 PrintSpeed=3"`)
	nonContiene(t, "pagina", corpo, "CUPS")
}

// inventario: RIP-H-40
// La coda delle foto (punti 2 e 3 della 049): accanto a «Riprova» il conto alla rovescia del prossimo
// tentativo automatico, solo finché la coda non si è fermata; la riga della foto scelta non sborda
// a 390 px (colonne minmax(0, 1fr), «Rimuovi» che non si stringe). Il comportamento lo provano il
// banco browser (scenario coda) e le schermate del manuale.
func TestCodaContoAllaRovescia(t *testing.T) {
	js := string(leggiStatico("app.js"))
	contiene(t, "app.js", js, "if (!stopped(failure) && (failure.retryAt || 0) > Date.now()) {",
		`element("span", {class: "attesa-coda", "data-retry-at": String(failure.retryAt)}, "")`,
		"`nuovo tentativo tra ${left} s…`", `"nuovo tentativo…"`, `document.querySelectorAll("#photo-queue .attesa-coda")`,
		`if (ticker === null && strip.querySelector(".attesa-coda")) ticker = setInterval(tick, 1000)`)
	css := string(leggiStatico("app.css"))
	contiene(t, "app.css", css, ".avanzamento-foto, .stato-foto, .foto-accettazione { grid-template-columns: minmax(0, 1fr); }",
		".riga-foto-scelta > button { flex-shrink: 0; }", ".attesa-coda {")
}
