package web

import (
	"net/url"
	"strings"
	"testing"
)

// La domanda della busta senza foto (D-117, slice 133, solo Go): «Questa riparazione non ha foto. Vuoi salvarla lo
// stesso?» con «Aggiungi foto» e «Salva senza foto», lato server, così vale senza JavaScript e per ogni invio del
// modulo (anche quello che app.js manda con fetch quando ci sono foto in memoria: la risposta è la stessa pagina).

const (
	testoSenzaFoto   = `<p>Questa riparazione non ha foto. Vuoi salvarla lo stesso?</p>`
	pulsanteAggiungi = `<button id="intake-no-photos-add" type="submit" form="new-repair-form" name="aggiungi_foto" value="1" class="pulsante-contorno">Aggiungi foto</button>`
	pulsanteSalva    = `<button id="intake-no-photos-save" type="submit" form="new-repair-form" name="senza_foto" value="1">Salva senza foto</button>`
	campoRicordato   = `<input name="senza_foto" type="hidden" value="1">`
)

// moduloSenzaFoto è bustaProva senza la risposta «Salva senza foto».
func moduloSenzaFoto(b *browser) url.Values {
	v := bustaProva(b)
	v.Del("senza_foto")
	return v
}

// inventario: RIP-H-29, RIP-H-40
// Una busta nuova senza foto (senza JavaScript: intake_photos manca; con JavaScript: 0) non si crea: la pagina torna
// con tutto quello che era scritto e la domanda, sotto «Genera busta» (che resta l'unico pulsante dentro il modulo,
// quello di Invio). Niente buste, niente repair.created, niente numero consumato. «Salva senza foto» (senza_foto=1,
// lo stesso invio) crea la busta una volta sola: lo stesso invio ripetuto porta allo stesso esito.
func TestNuovaSenzaFotoChiedeConferma(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	pagina := b.vai("GET", "/nuova", nil, nil)
	nonContiene(t, "pagina nuova", pagina.corpo, `id="intake-no-photos"`, campoRicordato)
	invio, chiave := invioDi(t, pagina.corpo), chiaveDi(t, pagina.corpo)
	for _, conteggio := range []string{"", "0", "+0", "000"} {
		v := con(con(con(moduloSenzaFoto(b), "invio", invio), "intake_key", chiave), "repair[public_notes]", "Nota <per> il cliente")
		if conteggio != "" {
			v.Set("intake_photos", conteggio)
		}
		r := b.vai("POST", "/nuova", v, nil)
		if r.codice != 200 || contaBuste(t, d) != 0 {
			t.Fatalf("conteggio %q: %d, %d buste", conteggio, r.codice, contaBuste(t, d))
		}
		contiene(t, "domanda", r.corpo, `<div id="intake-no-photos" role="alert" class="domanda-senza-foto" tabindex="-1">`, `<article class="riquadro con-domanda-foto">`, testoSenzaFoto,
			pulsanteAggiungi, pulsanteSalva, `value="Ada Esempio"`, "Ridurre anello</textarea>", "Nota &lt;per&gt; il cliente",
			`value="`+invio+`"`)
		if chiaveDi(t, r.corpo) != chiave {
			t.Fatal("la chiave delle foto è cambiata: le foto scelte dopo non andrebbero alla busta")
		}
		nonContiene(t, "domanda", r.corpo, campoRicordato)
		// I pulsanti della domanda stanno dopo il modulo: dentro c'è solo «Genera busta».
		inizio := strings.Index(r.corpo, `<form id="new-repair-form"`)
		chiusura := strings.Index(r.corpo[inizio:], `</form>`)
		if inizio < 0 || chiusura < 0 {
			t.Fatal("il modulo della busta non si chiude")
		}
		fine := inizio + chiusura
		if strings.Count(r.corpo[inizio:fine], `type="submit"`) != 1 || strings.Index(r.corpo, `id="intake-no-photos"`) < fine {
			t.Fatal("la domanda non è dopo il modulo, o il modulo ha più di un pulsante")
		}
	}
	// Senza QR, con l'avviso del QR già risposto: la domanda delle foto, senza ripetere l'avviso del QR.
	r := b.vai("POST", "/nuova", con(con(moduloSenzaFoto(b), "repair[qr_code]", ""), "qr_avvisato", "1"), nil)
	contiene(t, "domanda dopo il QR", r.corpo, testoSenzaFoto, `<input name="qr_avvisato" type="hidden" value="1">`)
	nonContiene(t, "domanda dopo il QR", r.corpo, "Genera busta senza QR", `id="intake-qr-warning"`, "intake-qr-missing")
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.created'`); n != 0 || contaBuste(t, d) != 0 {
		t.Fatalf("repair.created %d prima della risposta", n)
	}
	salva := con(con(con(moduloSenzaFoto(b), "invio", invio), "intake_key", chiave), "senza_foto", "1")
	r = b.vai("POST", "/nuova", salva, nil)
	if !creata(r) || contaBuste(t, d) != 1 {
		t.Fatalf("«Salva senza foto»: %d %q, %d buste", r.codice, r.location, contaBuste(t, d))
	}
	if numero := colonna(t, d, 1, "numero"); numero != "1/26" {
		t.Errorf("numero %q: la domanda ha consumato un numero", numero)
	}
	if r2 := b.vai("POST", "/nuova", salva, nil); r2.location != r.location || contaBuste(t, d) != 1 {
		t.Fatalf("lo stesso invio ripetuto: %q, %d buste", r2.location, contaBuste(t, d))
	}
	// L'esito non chiama la coda delle foto: non ce ne sono.
	nonContiene(t, "esito", b.vai("GET", r.location, nil, nil).corpo, `id="intake-photos-saved"`)
}

// inventario: RIP-H-40
// Con delle foto scelte (intake_photos > 0) la busta si salva subito, senza domanda, e l'esito le manda alla coda.
func TestNuovaConFotoNessunaDomanda(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	chiave := chiaveDi(t, b.vai("GET", "/nuova", nil, nil).corpo)
	r := b.vai("POST", "/nuova", con(con(moduloSenzaFoto(b), "intake_key", chiave), "intake_photos", "2"), nil)
	if !creata(r) || contaBuste(t, d) != 1 {
		t.Fatalf("con foto: %d %q", r.codice, r.location)
	}
	contiene(t, "esito", b.vai("GET", r.location, nil, nil).corpo, `id="intake-photos-saved"`)
}

// inventario: RIP-H-40
// «Aggiungi foto» senza JavaScript (aggiungi_foto=1) ridisegna il modulo com'è, senza la domanda e senza salvare,
// anche con senza_foto nello stesso invio; l'avviso del QR già dato resta. Con JavaScript (app.js) la domanda si
// toglie nel browser e si va alle foto.
func TestNuovaAggiungiFoto(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	b.vai("GET", "/nuova", nil, nil)
	for _, v := range []url.Values{
		con(moduloSenzaFoto(b), "aggiungi_foto", "1"),
		con(con(moduloSenzaFoto(b), "aggiungi_foto", "1"), "senza_foto", "1"),
		con(con(con(moduloSenzaFoto(b), "aggiungi_foto", "1"), "repair[qr_code]", ""), "qr_avvisato", "1"),
	} {
		r := b.vai("POST", "/nuova", v, nil)
		if r.codice != 200 || contaBuste(t, d) != 0 {
			t.Fatalf("«Aggiungi foto»: %d, %d buste", r.codice, contaBuste(t, d))
		}
		contiene(t, "aggiungi", r.corpo, `value="Ada Esempio"`, `id="intake-photos"`, `<article class="riquadro">`)
		// L'avviso del QR già risposto non si ripete (D-117).
		nonContiene(t, "aggiungi", r.corpo, `id="intake-no-photos"`, "Genera busta senza QR", `id="intake-qr-warning"`, "intake-qr-missing")
		if v["qr_avvisato"] != nil {
			contiene(t, "aggiungi con il QR già avvisato", r.corpo, `<input name="qr_avvisato" type="hidden" value="1">`)
		}
	}
	js := string(leggiStatico("app.js"))
	contiene(t, "app.js", js, `event.target.closest("#intake-no-photos-add")`, `document.getElementById("intake-no-photos")`,
		`domanda.remove()`, `document.getElementById("intake-photos")`)
	contiene(t, "app.css", string(leggiStatico("app.css")), ".domanda-senza-foto {", ".pulsanti-senza-foto {",
		".con-domanda-foto { display: flex; flex-direction: column; }", ".con-domanda-foto > h2 { order: -2; }",
		".con-domanda-foto > #intake-no-photos { order: -1;")
	contiene(t, "app.js", js, `domanda.scrollIntoView({block: "center"})`, `domanda.focus({preventScroll: true})`)
}

// inventario: RIP-H-30
// La domanda arriva per ultima, solo per una busta che si salverebbe: prima l'avviso del QR mancante, gli errori dei
// campi, la forma non riconosciuta e le etichette già usate. Dopo «Salva senza foto» la risposta resta nel modulo
// (campo nascosto senza_foto) se un altro avviso o un errore lo ridisegnano, così non si richiede.
func TestNuovaSenzaFotoPerUltima(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	b.vai("GET", "/nuova", nil, nil)
	base := moduloSenzaFoto(b)
	base.Del("gemelle_avvisate")
	prima := []struct {
		nome   string
		v      url.Values
		atteso string
	}{
		{"QR mancante", con(base, "repair[qr_code]", ""), `id="intake-qr-warning"`},
		{"campo obbligatorio", con(base, "repair[description]", ""), "Campo obbligatorio."},
		{"forma", con(base, "repair[qr_code]", "45/2026"), `id="intake-qr-format"`},
	}
	for _, c := range prima {
		r := b.vai("POST", "/nuova", c.v, nil)
		contiene(t, c.nome, r.corpo, c.atteso)
		nonContiene(t, c.nome, r.corpo, `id="intake-no-photos"`)
	}
	// Una busta aperta con l'etichetta 45: la seconda riceve prima l'avviso delle gemelle.
	if r := b.vai("POST", "/nuova", con(con(base, "repair[qr_code]", "45"), "senza_foto", "1"), nil); !creata(r) {
		t.Fatalf("prima busta: %d", r.codice)
	}
	b.vai("GET", "/nuova", nil, nil)
	r := b.vai("POST", "/nuova", con(con(moduloSenzaFoto(b), "repair[qr_code]", "45"), "gemelle_avvisate", ""), nil)
	contiene(t, "gemelle", r.corpo, `id="intake-qr-twins"`)
	nonContiene(t, "gemelle", r.corpo, `id="intake-no-photos"`)
	// Gemelle confermate: ora la domanda.
	r = b.vai("POST", "/nuova", con(con(moduloSenzaFoto(b), "repair[qr_code]", "45"), "gemelle_avvisate", "RIP-2026-0045"), nil)
	contiene(t, "dopo le gemelle", r.corpo, testoSenzaFoto)
	// «Salva senza foto» con un errore dei campi: l'errore, e la risposta ricordata.
	r = b.vai("POST", "/nuova", con(con(base, "repair[description]", ""), "senza_foto", "1"), nil)
	contiene(t, "errore dopo la risposta", r.corpo, "Campo obbligatorio.", campoRicordato)
	nonContiene(t, "errore dopo la risposta", r.corpo, `id="intake-no-photos"`)
	if contaBuste(t, d) != 1 {
		t.Fatalf("%d buste", contaBuste(t, d))
	}
}

// inventario: RIP-H-30
// La modifica di una scheda esistente senza foto non chiede niente (D-117: solo la creazione).
func TestModificaSenzaFotoNessunaDomanda(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	b.vai("GET", "/nuova", nil, nil)
	if r := b.vai("POST", "/nuova", bustaProva(b), nil); !creata(r) {
		t.Fatalf("busta: %d", r.codice)
	}
	r := salva(t, b, 1, map[string]string{"repair[description]": "Allargare anello"})
	if r.codice != 302 || strings.Contains(r.corpo, "intake-no-photos") {
		t.Fatalf("modifica: %d", r.codice)
	}
	if got := colonna(t, d, 1, "descrizione"); got != "Allargare anello" {
		t.Fatalf("descrizione %q", got)
	}
}
