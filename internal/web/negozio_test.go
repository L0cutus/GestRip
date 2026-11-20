package web

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/db"
)

// La pagina «Impostazioni del negozio», sezione del profilo (negozio.go, slice 087, ADM-02), come ShopLive dello
// standalone (go-baseline) per l'evento save: i casi di shop_live_test.exs e shop_tax_code_test.exs, senza LiveView.
// Le altre sezioni della pagina (operatività, stima, logo, colori, accessi) sono di altre voci. Dati inventati.

const rottaNegozio = "/settings/shop"

// campiProva sono il nome del campo, l'etichetta, il tipo e gli attributi del campo nella pagina, nell'ordine dello
// standalone.
var campiProva = []struct{ chiave, etichetta, tipo, extra string }{
	{"shop_name", "Nome negozio", "text", " required"}, {"subtitle", "Sottotitolo", "text", ""},
	{"legal_name", "Ragione sociale", "text", ""}, {"fiscal_id", "Identificativo fiscale", "text", ""},
	{"tax_code", "Codice fiscale (vuoto = uguale alla P.IVA)", "text", ` autocomplete="off"`},
	{"address", "Indirizzo", "text", ""}, {"postal_code", "CAP", "text", ""}, {"city", "Città", "text", ""},
	{"province", "Provincia", "text", ""}, {"phone", "Telefono", "text", ""}, {"email", "Email", "email", ""},
	{"sales_email", "Email vendite", "email", ""}, {"opening_hours", "Orari (nei messaggi al cliente)", "text", ""},
	{"instagram", "Instagram", "text", ""}, {"facebook", "Facebook", "text", ""}, {"website", "Blog o sito", "text", ""},
	{"warranty_terms", "Condizioni di garanzia", "textarea", " required"}}

// moduloNegozio è il modulo con i campi dati (gli altri non arrivano).
func moduloNegozio(b *browser, kv ...string) url.Values {
	v := url.Values{"_csrf_token": {b.token}}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set("shop_profile["+kv[i]+"]", kv[i+1])
	}
	return v
}

// moduloNegozioBase è il minimo valido.
func moduloNegozioBase(b *browser, extra ...string) url.Values {
	return moduloNegozio(b, append([]string{"shop_name", "Bottega Sintetica", "warranty_terms", "Dodici mesi"}, extra...)...)
}

func campoDelProfilo(t *testing.T, d *db.DB, colonna string) string {
	t.Helper()
	var v *string
	if err := d.Lettura.QueryRow(`SELECT ` + colonna + ` FROM profilo_negozio WHERE ambito = 'default'`).Scan(&v); err != nil {
		return "(assente)"
	}
	if v == nil {
		return "∅"
	}
	return *v
}

// inventario: ADM-02
// La pagina del titolare, sezione del profilo: la scheda «Negozio» aperta, il titolo, il link «Anagrafiche», «Profilo
// negozio» con la frase, il modulo #shop-form (POST con il token) con i diciassette campi nell'ordine e con le etichette
// dello standalone, ognuno con i suoi attributi (nome e garanzia obbligatori, le email di tipo email, il codice fiscale
// senza completamento, la garanzia in un textarea), vuoti senza un profilo, e «Salva impostazioni».
func TestPaginaNegozio(t *testing.T) {
	h, _ := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	r := b.vai("GET", rottaNegozio, nil, nil)
	if r.codice != 200 {
		t.Fatalf("pagina %d", r.codice)
	}
	contiene(t, "pagina", r.corpo, `<a id="settings-tab-shop" href="/settings/shop" aria-current="page" class="settings-tab">Negozio</a>`,
		`<section id="settings-panel" aria-labelledby="settings-tab-shop">`,
		`<h1>Impostazioni del negozio</h1>`, `<a href="/settings/anagrafiche" class="pulsante-contorno">Anagrafiche</a>`,
		`<h2>Profilo negozio</h2>`+"\n"+`<p class="sotto">I dati sono congelati in ogni documento al momento dell&#39;emissione. Nome e sottotitolo sono la testata della ricevuta che il cliente porta a casa.</p>`,
		`<form id="shop-form" action="/settings/shop" method="post" data-invio-unico>`+"\n"+`<input name="_csrf_token" type="hidden" value="`+b.token+`">`,
		`<button type="submit">Salva impostazioni</button>`, "<title>GestRip</title>")
	if strings.Count(r.corpo, `aria-current="page"`) != 1 {
		t.Error("aria-current non è su una sola scheda")
	}
	ultimo := -1
	for _, c := range campiProva {
		id, nome := "shop_profile_"+c.chiave, "shop_profile["+c.chiave+"]"
		var voluto string
		if c.tipo == "textarea" {
			voluto = `<div class="fieldset">` + "\n" + `<label for="` + id + `"><span class="label">` + c.etichetta + `</span>` + "\n" +
				`<textarea id="` + id + `" name="` + nome + `" rows="4"` + c.extra + `>` + "\n" + `</textarea></label>` + "\n" + `</div>`
		} else {
			voluto = `<div class="fieldset">` + "\n" + `<label for="` + id + `"><span class="label">` + c.etichetta + `</span>` + "\n" +
				`<input id="` + id + `" name="` + nome + `" type="` + c.tipo + `" value=""` + c.extra + `></label>` + "\n" + `</div>`
		}
		i := strings.Index(r.corpo, voluto)
		if i < 0 {
			t.Errorf("%s: manca %q", c.chiave, voluto)
			continue
		}
		if i < ultimo {
			t.Errorf("%s: fuori ordine", c.chiave)
		}
		ultimo = i
	}
	nonContiene(t, "pagina", r.corpo, `class="field-error"`, "flash-errore", "flash-info")
	// La pagina non porta le sezioni delle altre voci (operatività, logo, colori, accessi): non ci sono ancora. La stima
	// dei tempi (slice 102, ADM-04) ha il suo test (negozio_stima_test.go).
	nonContiene(t, "pagina", r.corpo, "Operativita", "Logo e icone", "Colori della barra", "Accessi all&#39;installazione")
}

// inventario: ADM-02
// Il modulo mostra il profilo salvato: valori scappati come nel markup (virgolette, «<», «&»), il testo della
// garanzia dentro il textarea con a capo tenuti, NULL come campo vuoto.
func TestPaginaNegozioMostraIlSalvato(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	eseguiSQL(t, d, `INSERT INTO profilo_negozio (nome, condizioni_garanzia, citta, instagram, creato_il, aggiornato_il)
		VALUES ('Bottega "Rossi" & <Figli>', 'Riga uno'||char(10)||'Riga <due>', 'Paese Finto', NULL, ?, ?)`, istanteHome, istanteHome)
	r := b.vai("GET", rottaNegozio, nil, nil)
	contiene(t, "salvato", r.corpo, `id="shop_profile_shop_name" name="shop_profile[shop_name]" type="text" value="Bottega &#34;Rossi&#34; &amp; &lt;Figli&gt;" required>`,
		`id="shop_profile_city" name="shop_profile[city]" type="text" value="Paese Finto">`,
		`id="shop_profile_instagram" name="shop_profile[instagram]" type="text" value="">`,
		`<textarea id="shop_profile_warranty_terms" name="shop_profile[warranty_terms]" rows="4" required>`+"\n"+`Riga uno`+"\n"+`Riga &lt;due&gt;</textarea>`)
}

// inventario: ADM-02
// Salvare con nome e garanzia: ripulisce gli spazi, ritorna alla pagina (302) con «Impostazioni salvate. I documenti
// emessi restano invariati.» una volta sola, i valori salvati nei campi, la riga del database e una riga
// shop_profile.updated con l'attore; con tutti gli altri campi riscritti dal modulo.
func TestSalvaNegozio(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	b.vai("GET", rottaNegozio, nil, nil)
	r := b.vai("POST", rottaNegozio, moduloNegozio(b, "shop_name", "  Otto Gioielli  ", "subtitle", " Oreficeria dal 1952 ", "legal_name", "Otto S.r.l.",
		"fiscal_id", "01234567890", "tax_code", " zmp sfn70a01l407x ", "address", "Via Finta 1", "postal_code", "00000", "city", "Paese Finto",
		"province", "XX", "phone", "0423 000111", "email", "info@otto.example", "sales_email", "vendite@otto.example",
		"opening_hours", "  Lun-Sab 9-19  ", "instagram", " @otto ", "facebook", "otto.gioielli", "website", "otto.example",
		"warranty_terms", "  Due anni\nsu tutto  "), nil)
	if !redirectA(r, rottaNegozio) {
		t.Fatalf("atteso il ritorno alla pagina, avuto %d %q", r.codice, r.location)
	}
	r = b.vai("GET", rottaNegozio, nil, nil)
	contiene(t, "dopo", r.corpo, `<div role="status" class="flash flash-info">Impostazioni salvate. I documenti emessi restano invariati.</div>`,
		`name="shop_profile[shop_name]" type="text" value="Otto Gioielli" required>`, `name="shop_profile[subtitle]" type="text" value="Oreficeria dal 1952">`,
		`name="shop_profile[tax_code]" type="text" value="ZMPSFN70A01L407X" autocomplete="off">`, `name="shop_profile[opening_hours]" type="text" value="Lun-Sab 9-19">`,
		`name="shop_profile[email]" type="email" value="info@otto.example">`, `required>`+"\n"+"Due anni\nsu tutto</textarea>")
	nonContiene(t, "dopo", r.corpo, "field-error")
	if r2 := b.vai("GET", rottaNegozio, nil, nil); strings.Contains(r2.corpo, "Impostazioni salvate") {
		t.Error("il messaggio resta alla pagina dopo")
	}
	for col, v := range map[string]string{"nome": "Otto Gioielli", "sottotitolo": "Oreficeria dal 1952", "ragione_sociale": "Otto S.r.l.", "partita_iva": "01234567890",
		"codice_fiscale": "ZMPSFN70A01L407X", "indirizzo": "Via Finta 1", "cap": "00000", "citta": "Paese Finto", "provincia": "XX", "telefono": "0423 000111",
		"email": "info@otto.example", "email_vendite": "vendite@otto.example", "orari": "Lun-Sab 9-19", "instagram": "@otto", "facebook": "otto.gioielli",
		"sito": "otto.example", "condizioni_garanzia": "Due anni\nsu tutto"} {
		if got := campoDelProfilo(t, d, col); got != v {
			t.Errorf("%s = %q, atteso %q", col, got, v)
		}
	}
	if got := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'shop_profile.updated' AND risorsa = 'shop_profile' AND attore = 'titolare@esempio.it'`); got != 1 {
		t.Fatalf("righe d'audit %d", got)
	}
}

// inventario: ADM-02
// Nome e garanzia vuoti: la pagina ridisegnata (200, nessun messaggio in alto) con «Campo obbligatorio.» sotto il
// nome e sotto la garanzia, i valori scritti negli altri campi (ripuliti dove cambiano) e niente scritto: né il
// profilo né l'audit; sul profilo già salvato resta com'era.
func TestNegozioObbligatori(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	b.vai("GET", rottaNegozio, nil, nil)
	r := b.vai("POST", rottaNegozio, moduloNegozio(b, "shop_name", "   ", "warranty_terms", "", "city", "  Paese Finto  ", "instagram", " "), nil)
	if r.codice != 200 {
		t.Fatalf("rifiuto %d %q", r.codice, r.location)
	}
	contiene(t, "rifiuto", r.corpo, `<h1>Impostazioni del negozio</h1>`,
		`id="shop_profile_shop_name" name="shop_profile[shop_name]" type="text" value="   " required></label>`+"\n"+`<p class="field-error">Campo obbligatorio.</p>`,
		`</textarea></label>`+"\n"+`<p class="field-error">Campo obbligatorio.</p>`,
		`name="shop_profile[city]" type="text" value="Paese Finto">`,
		// Senza profilo un campo di soli spazi non cambia (nil come il salvato): resta com'è scritto, come input_value/3.
		`name="shop_profile[instagram]" type="text" value=" ">`)
	if strings.Count(r.corpo, `class="field-error"`) != 2 {
		t.Errorf("errori sotto i campi: %d", strings.Count(r.corpo, `class="field-error"`))
	}
	nonContiene(t, "rifiuto", r.corpo, "flash-info", "flash-errore")
	if contaRighe(t, d, `SELECT count(*) FROM profilo_negozio`)+contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'shop_profile.updated'`) != 0 {
		t.Fatal("un rifiuto ha scritto")
	}
	// Con un profilo già salvato il rifiuto non lo tocca, e i campi non cambiati mostrano quello che si è scritto.
	if !redirectA(b.vai("POST", rottaNegozio, moduloNegozioBase(b, "city", "Paese Finto"), nil), rottaNegozio) {
		t.Fatal("salvataggio")
	}
	r = b.vai("POST", rottaNegozio, moduloNegozio(b, "shop_name", "", "warranty_terms", "Altro testo", "city", "Altro paese"), nil)
	if r.codice != 200 || strings.Count(r.corpo, `class="field-error"`) != 1 {
		t.Fatalf("secondo rifiuto: %d, errori %d", r.codice, strings.Count(r.corpo, `class="field-error"`))
	}
	contiene(t, "secondo", r.corpo, `name="shop_profile[city]" type="text" value="Altro paese">`, "Altro testo</textarea>")
	if campoDelProfilo(t, d, "nome") != "Bottega Sintetica" || campoDelProfilo(t, d, "citta") != "Paese Finto" || campoDelProfilo(t, d, "condizioni_garanzia") != "Dodici mesi" {
		t.Fatal("il rifiuto ha cambiato il profilo salvato")
	}
	if contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'shop_profile.updated'`) != 1 {
		t.Fatal("il rifiuto ha scritto l'audit")
	}
}

// inventario: ADM-02
// Le righe del testo della garanzia arrivano con «\r\n» dal modulo e si salvano con «\n» (come le caselle di
// LiveView, decisione dell'11/11); si rimostrano con «\n».
func TestNegozioGaranziaConRighe(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	b.vai("GET", rottaNegozio, nil, nil)
	if !redirectA(b.vai("POST", rottaNegozio, moduloNegozio(b, "shop_name", "Bottega Sintetica", "warranty_terms", "Due anni.\r\n\r\nNon copre l'usura.\r\n"), nil), rottaNegozio) {
		t.Fatal("salvataggio")
	}
	if got := campoDelProfilo(t, d, "condizioni_garanzia"); got != "Due anni.\n\nNon copre l'usura." {
		t.Fatalf("garanzia %q", got)
	}
}

// inventario: ADM-02
// Un campo troppo lungo è rifiutato con «Troppo lungo (massimo N caratteri).» sotto il campo, i valori scritti
// restano nel modulo e non si salva niente; un codice fiscale che non lo è, con il suo messaggio.
func TestNegozioLunghezzeECodiceFiscale(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	b.vai("GET", rottaNegozio, nil, nil)
	lungo := strings.Repeat("a", 501)
	r := b.vai("POST", rottaNegozio, moduloNegozioBase(b, "opening_hours", lungo, "city", "Paese Finto"), nil)
	if r.codice != 200 {
		t.Fatalf("rifiuto %d", r.codice)
	}
	contiene(t, "lungo", r.corpo, `value="`+lungo+`"></label>`+"\n"+`<p class="field-error">Troppo lungo (massimo 500 caratteri).</p>`,
		`name="shop_profile[city]" type="text" value="Paese Finto">`)
	if contaRighe(t, d, `SELECT count(*) FROM profilo_negozio`) != 0 {
		t.Fatal("un campo troppo lungo è stato salvato")
	}
	r = b.vai("POST", rottaNegozio, moduloNegozioBase(b, "tax_code", "ABC123"), nil)
	contiene(t, "codice fiscale", r.corpo, `name="shop_profile[tax_code]" type="text" value="ABC123" autocomplete="off"></label>`+"\n"+
		`<p class="field-error">codice fiscale non valido: 16 caratteri, o 11 cifre per una società</p>`)
	if contaRighe(t, d, `SELECT count(*) FROM profilo_negozio`) != 0 {
		t.Fatal("un codice fiscale non valido è stato salvato")
	}
	// Svuotare il codice fiscale lo toglie.
	if !redirectA(b.vai("POST", rottaNegozio, moduloNegozioBase(b, "tax_code", "01234567891"), nil), rottaNegozio) || campoDelProfilo(t, d, "codice_fiscale") != "01234567891" {
		t.Fatal("codice fiscale di una società non salvato")
	}
	if !redirectA(b.vai("POST", rottaNegozio, moduloNegozioBase(b, "tax_code", ""), nil), rottaNegozio) || campoDelProfilo(t, d, "codice_fiscale") != "∅" {
		t.Fatalf("codice fiscale svuotato: %q", campoDelProfilo(t, d, "codice_fiscale"))
	}
}

// inventario: ADM-02
// Un modulo senza la mappa shop_profile (un invio forgiato) non salva niente e dice «Richiesta non riconosciuta:
// nessuna impostazione salvata.»; un campo forgiato come lista è «Valore non valido.» sul campo.
func TestNegozioModuliForgiati(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	b.vai("GET", rottaNegozio, nil, nil)
	for nome, f := range map[string]url.Values{
		"vuoto":    {"_csrf_token": {b.token}},
		"altro":    {"_csrf_token": {b.token}, "sciocchezze": {"1"}},
		"testo":    {"_csrf_token": {b.token}, "shop_profile": {"x"}},
		"lista":    {"_csrf_token": {b.token}, "shop_profile[]": {"x"}},
		"estraneo": {"_csrf_token": {b.token}, "shop_profil": {"x"}},
	} {
		r := b.vai("POST", rottaNegozio, f, nil)
		if r.codice != 200 {
			t.Errorf("%s: %d %q", nome, r.codice, r.location)
			continue
		}
		contiene(t, nome, r.corpo, `<div role="alert" class="flash flash-errore">Richiesta non riconosciuta: nessuna impostazione salvata.</div>`)
	}
	f := moduloNegozioBase(b)
	f.Set("shop_profile[city][]", "b")
	r := b.vai("POST", rottaNegozio, f, nil)
	contiene(t, "lista", r.corpo, `name="shop_profile[city]" type="text" value=""></label>`+"\n"+`<p class="field-error">Valore non valido.</p>`)
	if contaRighe(t, d, `SELECT count(*) FROM profilo_negozio`)+contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'shop_profile.updated'`) != 0 {
		t.Fatal("un invio forgiato ha scritto")
	}
	// Lo stesso campo come testo e come mappa o lista insieme: vince sempre la forma che non è un testo, in qualunque ordine
	// arrivino le chiavi.
	for _, forma := range []string{"shop_profile[city][x]", "shop_profile[city][]"} {
		f = moduloNegozioBase(b)
		f.Set("shop_profile[city]", "Paese Testo")
		f.Set(forma, "altro")
		r = b.vai("POST", rottaNegozio, f, nil)
		contiene(t, forma, r.corpo, `<p class="field-error">Valore non valido.</p>`)
		if contaRighe(t, d, `SELECT count(*) FROM profilo_negozio`) != 0 {
			t.Fatalf("%s: un invio forgiato ha scritto", forma)
		}
	}
	// Un campo ripetuto vale l'ultimo (Plug).
	f = moduloNegozioBase(b)
	f["shop_profile[city]"] = []string{"Primo", "Ultimo"}
	if !redirectA(b.vai("POST", rottaNegozio, f, nil), rottaNegozio) || campoDelProfilo(t, d, "citta") != "Ultimo" {
		t.Fatalf("campo ripetuto: %q", campoDelProfilo(t, d, "citta"))
	}
	// Senza il token il modulo non passa (403) e non scrive.
	prima := campoDelProfilo(t, d, "citta")
	senza := moduloNegozioBase(b, "city", "Senza token")
	senza.Del("_csrf_token")
	if r := b.vai("POST", rottaNegozio, senza, nil); r.codice != http.StatusForbidden || campoDelProfilo(t, d, "citta") != prima {
		t.Fatalf("POST senza token: %d", r.codice)
	}
}

// inventario: ADM-02
// Solo il titolare: il dipendente che apre la pagina o forza un salvataggio va alla home e non scrive niente; chi non è
// entrato va al login.
func TestNegozioSoloTitolare(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	dip := dipendenteDentro(t, h)
	dip.vai("GET", "/", nil, nil)
	if r := dip.vai("GET", rottaNegozio, nil, nil); !redirectA(r, "/") {
		t.Errorf("pagina del dipendente: %d %q", r.codice, r.location)
	}
	if r := dip.vai("POST", rottaNegozio, moduloNegozioBase(dip), nil); !redirectA(r, "/") {
		t.Errorf("POST del dipendente: %d %q", r.codice, r.location)
	}
	contiene(t, "home", dip.vai("GET", "/", nil, nil).corpo, `Operazione riservata al titolare.`)
	if contaRighe(t, d, `SELECT count(*) FROM profilo_negozio`)+contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'shop_profile.updated'`) != 0 {
		t.Fatal("il dipendente ha scritto il profilo")
	}
	anon := nuovoBrowser(t, h)
	if r := anon.vai("GET", rottaNegozio, nil, nil); !redirectA(r, "/login") {
		t.Errorf("senza accesso: %d %q", r.codice, r.location)
	}
}

// inventario: ADM-02
// Con la pagina registrata, «Negozio» è la prima scheda delle impostazioni, /settings porta lì e l'anagrafica ha il link
// «Profilo negozio» per il titolare (e non per il dipendente).
func TestNegozioSchedaELink(t *testing.T) {
	h, _ := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	if r := b.vai("GET", "/settings", nil, nil); !redirectA(r, rottaNegozio) {
		t.Errorf("/settings: %d %q", r.codice, r.location)
	}
	r := b.vai("GET", "/settings/utenti", nil, nil)
	if s := schedeDi(t, r.corpo); len(s) == 0 || s[0] != [4]string{"shop", "/settings/shop", "Negozio", ""} {
		t.Errorf("schede: %v", s)
	}
	contiene(t, "link", b.vai("GET", rottaAnagrafiche, nil, nil).corpo, `<a id="shop-profile-link" href="/settings/shop" class="pulsante-contorno">Profilo negozio</a>`)
	nonContiene(t, "dipendente", dipendenteDentro(t, h).vai("GET", rottaAnagrafiche, nil, nil).corpo, "Profilo negozio")
}

// inventario: ADM-02
// Un salvataggio dalla pagina lo leggono subito gli altri: il nome della lista e dell'intestazione, l'informativa
// pubblica (titolo, ragione sociale, P.IVA, sede, contatti). La ricevuta la prova il dominio (il PDF non si legge qui).
func TestNegozioSalvatoLoLeggonoGliAltri(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	b := titolareDentro(t, p.interna)
	b.vai("GET", rottaNegozio, nil, nil)
	r := b.vai("POST", rottaNegozio, moduloNegozio(b, "shop_name", "Bottega dalla Pagina", "warranty_terms", "Dodici mesi", "legal_name", "Bottega dalla Pagina S.r.l.",
		"fiscal_id", "01234567890", "address", "Via Nuova 2", "city", "Paese Nuovo", "phone", "0423 999888", "email", "nuova@bottega.example"), nil)
	if !redirectA(r, rottaNegozio) {
		t.Fatalf("salvataggio %d %q", r.codice, r.location)
	}
	contiene(t, "home", b.vai("GET", "/", nil, nil).corpo, `<h1 class="bottega-nome">Bottega dalla Pagina</h1>`)
	pr := nuovoBrowser(t, p.pubblica).vai("GET", "/privacy", nil, nil)
	contiene(t, "privacy", pr.corpo, "<title>Privacy — Bottega dalla Pagina · Laboratorio</title>",
		"Bottega dalla Pagina S.r.l.<span> — Identificativo fiscale 01234567890</span>", "<span>Sede: Via Nuova 2, Paese Nuovo<br></span>",
		"tel. 0423 999888 · email nuova@bottega.example")
	nonContiene(t, "privacy", pr.corpo, "Bottega di Prova")
}

// profiloGuasto avvolge le buste di una prova: la lettura o la scrittura del profilo falliscono come un
// database guasto.
type profiloGuasto struct {
	Buste
	lettura, scrittura bool
}

func (p profiloGuasto) ProfiloPerModulo(ctx context.Context) (map[string]string, error) {
	if p.lettura {
		return nil, errors.New("guasto della lettura")
	}
	return p.Buste.ProfiloPerModulo(ctx)
}

func (p profiloGuasto) SalvaProfilo(ctx context.Context, m buste.ModuloProfilo, c audit.Contesto) error {
	if p.scrittura {
		return errors.New("guasto della scrittura")
	}
	return p.Buste.SalvaProfilo(ctx, m, c)
}

// inventario: ADM-02
// Un guasto del database: se il profilo non si legge la pagina è quella d'errore (500) con la riga nel registro; se non
// si scrive, la pagina dice «Impostazioni non salvate: riprova.» con i valori scritti e una riga nel registro, senza il
// testo del profilo.
func TestNegozioGuasto(t *testing.T) {
	var reg bytes.Buffer
	h, _ := facciaConBusteCon(t, &reg, func(b Buste) Buste { return profiloGuasto{Buste: b, lettura: true} })
	b := titolareDentro(t, h)
	if r := b.vai("GET", rottaNegozio, nil, nil); r.codice != 500 {
		t.Errorf("lettura guasta: %d", r.codice)
	}
	contiene(t, "registro", reg.String(), "profilo del negozio non letto")
	reg.Reset()
	h, d := facciaConBusteCon(t, &reg, func(b Buste) Buste { return profiloGuasto{Buste: b, scrittura: true} })
	b = titolareDentro(t, h)
	b.vai("GET", rottaNegozio, nil, nil)
	r := b.vai("POST", rottaNegozio, moduloNegozioBase(b, "city", "Paese Segreto"), nil)
	if r.codice != 200 {
		t.Fatalf("scrittura guasta: %d", r.codice)
	}
	contiene(t, "scrittura", r.corpo, `<div role="alert" class="flash flash-errore">Impostazioni non salvate: riprova.</div>`,
		`name="shop_profile[city]" type="text" value="Paese Segreto">`)
	contiene(t, "registro", reg.String(), "profilo del negozio non salvato")
	nonContiene(t, "registro", reg.String(), "Paese Segreto", "Bottega Sintetica", "Dodici mesi")
	if contaRighe(t, d, `SELECT count(*) FROM profilo_negozio`) != 0 {
		t.Fatal("il guasto ha scritto")
	}
}
