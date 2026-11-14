package web

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/db"
)

// La pagina «Anagrafiche», parte degli artigiani (anagrafiche.go, slice 076), come OreficerieLive
// dello standalone per gli eventi create_artisan, update_artisan e delete_artisan: i casi di
// artisans_live_test.exs, senza LiveView. Nomi e numeri inventati.

const rottaAnagrafiche = "/settings/anagrafiche"

func artigianoProva(t *testing.T, d *db.DB, nome, telefono string) int64 {
	t.Helper()
	var tel any
	if telefono != "" {
		tel = telefono
	}
	res, err := d.Scrittura.Exec(`INSERT INTO artigiani (nome, telefono) VALUES (?, ?)`, nome, tel)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func artigianoScritto(t *testing.T, d *db.DB, id int64) string {
	t.Helper()
	var s string
	if err := d.Lettura.QueryRow(`SELECT nome || ':' || coalesce(telefono, '-') FROM artigiani WHERE id = ?`, id).Scan(&s); err != nil {
		return "(assente)"
	}
	return s
}

func moduloArtigiano(b *browser, nome, telefono string) url.Values {
	return url.Values{"_csrf_token": {b.token}, "artisan[name]": {nome}, "artisan[phone]": {telefono}}
}

func dopoRedirect(t *testing.T, b *browser, r risposta) risposta {
	t.Helper()
	if !redirectA(r, rottaAnagrafiche) {
		t.Fatalf("atteso il ritorno alla pagina, avuto %d %q", r.codice, r.location)
	}
	return b.vai("GET", rottaAnagrafiche, nil, nil)
}

// inventario: ART-01
// La pagina del personale (titolare e dipendente): intestazione «Impostazioni» / «Anagrafiche», il
// riquadro #artigiani con la frase sui nomi, una riga per artigiano (nome e telefono nel modulo di
// modifica, «Salva», «Elimina» con la domanda), per nome e id, il modulo «Nuovo artigiano *» e
// «Nessun artigiano registrato.» nascosto quando l'elenco non è vuoto. «Profilo negozio» solo al
// titolare, e solo quando la sua pagina c'è.
func TestPaginaAnagrafiche(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	r := b.vai("GET", rottaAnagrafiche, nil, nil)
	if r.codice != 200 {
		t.Fatalf("pagina vuota: %d", r.codice)
	}
	contiene(t, "vuota", r.corpo, `<p class="sotto">Impostazioni</p>`+"\n"+`<h1>Anagrafiche</h1>`,
		`<section id="artigiani" class="riquadro">`+"\n"+`<h2>Artigiani</h2>`+"\n"+
			`<p class="sotto">Modificare il nome aggiorna anche le riparazioni passate associate (sono collegate per ID, non per nome).</p>`,
		`<div id="artisans-list">`+"\n"+`<p id="artisans-empty" class="sotto">Nessun artigiano registrato.</p>`,
		`<form id="new-artisan-form" action="/settings/anagrafiche/artigiani" method="post" class="anagrafica-nuovo" data-invio-unico>`+"\n"+`<input name="_csrf_token" type="hidden" value="`+b.token+`">`,
		`<label for="new-artisan-name">Nuovo artigiano *</label>`+"\n"+`<input id="new-artisan-name" name="artisan[name]" type="text" value="" placeholder="Nome e cognome" required autocomplete="off">`,
		`<label for="new-artisan-phone">Telefono WhatsApp</label>`+"\n"+`<input id="new-artisan-phone" name="artisan[phone]" type="tel" value="" placeholder="Es.: 3391234567" inputmode="tel" autocomplete="off">`,
		`<button type="submit">+ Aggiungi</button>`, "<title>Anagrafiche")
	nonContiene(t, "vuota", r.corpo, "Profilo negozio", "Oreficerie clienti", `id="artisan-`)
	zeta := artigianoProva(t, d, "Zeta", "")
	alfa := artigianoProva(t, d, "alfa", "0490000001")
	beta := artigianoProva(t, d, "Beta <&>", "")
	r = b.vai("GET", rottaAnagrafiche, nil, nil)
	contiene(t, "piena", r.corpo, `<p id="artisans-empty" class="sotto" hidden>Nessun artigiano registrato.</p>`,
		`<div id="artisan-`+itoa(alfa)+`" class="anagrafica-riga">`+"\n"+
			`<form id="edit-artisan-`+itoa(alfa)+`" action="/settings/anagrafiche/artigiani/`+itoa(alfa)+`" method="post" data-invio-unico>`+"\n"+
			`<input name="_csrf_token" type="hidden" value="`,
		`<label for="artisan-name-`+itoa(alfa)+`">Nome</label>`+"\n"+`<input id="artisan-name-`+itoa(alfa)+`" name="artisan[name]" type="text" value="alfa" placeholder="Nome" required>`,
		`<label for="artisan-phone-`+itoa(alfa)+`">Telefono</label>`+"\n"+`<input id="artisan-phone-`+itoa(alfa)+`" name="artisan[phone]" type="tel" value="0490000001" placeholder="Telefono" inputmode="tel">`,
		`<button type="submit">Salva</button>`+"\n"+
			`<button id="delete-artisan-`+itoa(alfa)+`" type="submit" formaction="/settings/anagrafiche/artigiani/`+itoa(alfa)+`/elimina" formnovalidate class="pulsante-pericolo" data-confirm="Eliminare alfa?">Elimina</button>`,
		`value="Beta &lt;&amp;&gt;"`, `data-confirm="Eliminare Beta &lt;&amp;&gt;?"`,
		`id="artisan-phone-`+itoa(zeta)+`" name="artisan[phone]" type="tel" value=""`)
	iBeta, iZeta, iAlfa := strings.Index(r.corpo, `id="artisan-`+itoa(beta)+`"`), strings.Index(r.corpo, `id="artisan-`+itoa(zeta)+`"`), strings.Index(r.corpo, `id="artisan-`+itoa(alfa)+`"`)
	if iBeta < 0 || iBeta > iZeta || iZeta > iAlfa {
		t.Fatalf("ordine: %d %d %d", iBeta, iZeta, iAlfa)
	}
	if strings.Index(r.corpo, `id="artisans-list"`) > strings.Index(r.corpo, `id="new-artisan-form"`) {
		t.Fatal("il modulo nuovo va dopo l'elenco")
	}
	// Il titolare non vede «Profilo negozio» finché /settings/shop non c'è nel Go (ADM-02).
	tit := titolareDentro(t, h)
	nonContiene(t, "titolare", tit.vai("GET", rottaAnagrafiche, nil, nil).corpo, "Profilo negozio")
}

// inventario: ART-01
// Con la pagina del profilo del negozio registrata, il link «Profilo negozio» c'è per il titolare
// e non per il dipendente.
func TestAnagraficheProfiloNegozio(t *testing.T) {
	for _, c := range []struct {
		ruolo string
		vede  bool
	}{{accounts.Titolare, true}, {accounts.Dipendente, false}} {
		b, _ := dentroCome(t, c.ruolo, func(f *facciaWeb, mux *http.ServeMux) {
			f.rottaConGuscio(mux, "GET /settings/shop", soloTitolare, f.home)
		})
		r := b.vai("GET", rottaAnagrafiche, nil, nil)
		if r.codice != 200 {
			t.Fatalf("%s: %d", c.ruolo, r.codice)
		}
		if got := strings.Contains(r.corpo, `<a id="shop-profile-link" href="/settings/shop" class="pulsante-contorno">Profilo negozio</a>`); got != c.vede {
			t.Errorf("%s: link %v", c.ruolo, got)
		}
	}
}

// inventario: ART-02
// «+ Aggiungi»: il nome ripulito, il telefono normalizzato, «Artigiano "X" aggiunto.» e la pagina
// con il modulo vuoto; il nome vuoto ridisegna con «Il nome è obbligatorio.», l'errore sotto il
// campo e i valori ripuliti; un doppione nomina il nome ripulito e lascia i valori scritti; un
// modulo senza la mappa artisan è «Artigiano non aggiunto.». Il dipendente può.
func TestNuovoArtigianoDallaPagina(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	b.vai("GET", rottaAnagrafiche, nil, nil)
	r := dopoRedirect(t, b, b.vai("POST", "/settings/anagrafiche/artigiani", moduloArtigiano(b, "  Mario Esempio  ", "+39 333-111"), nil))
	contiene(t, "creato", r.corpo, `<div role="status" class="flash flash-info">Artigiano &#34;Mario Esempio&#34; aggiunto.</div>`,
		`value="Mario Esempio"`, `value="&#43;39333111"`,
		// D-88: dopo «+ Aggiungi» riuscito il modulo nuovo è vuoto (lo standalone lascia nel campo il nome scritto).
		`<input id="new-artisan-name" name="artisan[name]" type="text" value=""`, `<input id="new-artisan-phone" name="artisan[phone]" type="tel" value=""`)
	if got := contaRighe(t, d, `SELECT count(*) FROM artigiani WHERE nome = 'Mario Esempio' AND telefono = '+39333111'`); got != 1 {
		t.Fatalf("righe %d", got)
	}
	if righeAudit(t, d, "artisan.created") != 1 {
		t.Fatal("audit")
	}
	if contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'artisan.created' AND attore = 'banco@esempio.it'`) != 1 {
		t.Fatal("attore")
	}
	// Nome vuoto.
	r = b.vai("POST", "/settings/anagrafiche/artigiani", moduloArtigiano(b, "   ", " 333 (0) "), nil)
	if r.codice != 200 {
		t.Fatalf("vuoto: %d", r.codice)
	}
	contiene(t, "vuoto", r.corpo, `<div role="alert" class="flash flash-errore">Il nome è obbligatorio.</div>`,
		`<input id="new-artisan-name" name="artisan[name]" type="text" value="" placeholder="Nome e cognome" required autocomplete="off">`+"\n"+`<p class="field-error">Campo obbligatorio.</p>`,
		`<input id="new-artisan-phone" name="artisan[phone]" type="tel" value="3330"`)
	// Doppione, con gli spazi: il messaggio dice il nome ripulito, il modulo tiene quello scritto.
	r = b.vai("POST", "/settings/anagrafiche/artigiani", moduloArtigiano(b, "   Mario Esempio   ", " +39 333-1 "), nil)
	contiene(t, "doppione", r.corpo, `<div role="alert" class="flash flash-errore">Esiste già un artigiano con il nome &#34;Mario Esempio&#34;.</div>`,
		`<input id="new-artisan-name" name="artisan[name]" type="text" value="   Mario Esempio   "`, `<input id="new-artisan-phone" name="artisan[phone]" type="tel" value=" &#43;39 333-1 "`)
	nonContiene(t, "doppione", r.corpo, "field-error")
	// Moduli forgiati.
	for _, v := range []url.Values{
		{"artisan": {"boom"}}, {"artisan[]": {"x"}}, {"nope": {"1"}}, {},
		{"artisan": {"boom"}, "artisan[name]": {"Forgiato"}},
	} {
		v.Set("_csrf_token", b.token)
		r = b.vai("POST", "/settings/anagrafiche/artigiani", v, nil)
		if r.codice != 200 {
			t.Fatalf("%v: %d", v, r.codice)
		}
		contiene(t, "forgiato", r.corpo, `<div role="alert" class="flash flash-errore">Artigiano non aggiunto.</div>`)
	}
	// Un campo ripetuto vale l'ultimo valore, come Plug.
	v2 := url.Values{"_csrf_token": {b.token}, "artisan[name]": {"Primo Ripetuto", "Secondo Ripetuto"}}
	dopoRedirect(t, b, b.vai("POST", "/settings/anagrafiche/artigiani", v2, nil))
	if contaRighe(t, d, `SELECT count(*) FROM artigiani WHERE nome = 'Secondo Ripetuto'`) != 1 || contaRighe(t, d, `SELECT count(*) FROM artigiani WHERE nome = 'Primo Ripetuto'`) != 0 {
		t.Fatal("campo ripetuto: non vale l'ultimo")
	}
	// Un modulo con la mappa ma senza il nome è un nome vuoto.
	v := url.Values{"_csrf_token": {b.token}, "artisan[phone]": {"1"}}
	contiene(t, "senza nome", b.vai("POST", "/settings/anagrafiche/artigiani", v, nil).corpo, "Il nome è obbligatorio.")
	if contaRighe(t, d, `SELECT count(*) FROM artigiani`) != 2 || righeAudit(t, d, "artisan.created") != 2 {
		t.Fatal("un rifiuto ha scritto")
	}
}

// inventario: ART-03
// «Salva» su una riga: «Artigiano aggiornato.» con i valori nuovi; il doppione senza maiuscole
// nomina il nome scritto e la riga tiene i valori scritti; il nome vuoto è «Il nome è
// obbligatorio.»; un id forgiato, o un modulo senza la mappa artisan, «Artigiano non trovato.»;
// niente cambia.
func TestModificaArtigianoDallaPagina(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	rossi := artigianoProva(t, d, "Rossi", "")
	bianchi := artigianoProva(t, d, "Bianchi", "")
	b.vai("GET", rottaAnagrafiche, nil, nil)
	dove := "/settings/anagrafiche/artigiani/" + itoa(bianchi)
	r := b.vai("POST", dove, moduloArtigiano(b, "ROSSI", " +39 333-1 "), nil)
	if r.codice != 200 {
		t.Fatalf("doppione: %d", r.codice)
	}
	contiene(t, "doppione", r.corpo, `<div role="alert" class="flash flash-errore">Esiste già un artigiano con il nome &#34;ROSSI&#34;.</div>`,
		`<input id="artisan-name-`+itoa(bianchi)+`" name="artisan[name]" type="text" value="ROSSI"`,
		`<input id="artisan-phone-`+itoa(bianchi)+`" name="artisan[phone]" type="tel" value=" &#43;39 333-1 "`,
		`<input id="artisan-name-`+itoa(rossi)+`" name="artisan[name]" type="text" value="Rossi"`,
		// La domanda di «Elimina» nomina il nome salvato, non quello scritto.
		`id="delete-artisan-`+itoa(bianchi)+`" type="submit" formaction="/settings/anagrafiche/artigiani/`+itoa(bianchi)+`/elimina" formnovalidate class="pulsante-pericolo" data-confirm="Eliminare Bianchi?"`)
	r = b.vai("POST", dove, moduloArtigiano(b, "  ", "5"), nil)
	contiene(t, "vuoto", r.corpo, `<div role="alert" class="flash flash-errore">Il nome è obbligatorio.</div>`,
		`<input id="artisan-name-`+itoa(bianchi)+`" name="artisan[name]" type="text" value="  "`)
	nonContiene(t, "vuoto", r.corpo, "field-error")
	for _, id := range []string{"invalid", "0", "-1", "99999999999999999999999999999", "999"} {
		r = b.vai("POST", "/settings/anagrafiche/artigiani/"+id, moduloArtigiano(b, "Alterata", "222"), nil)
		contiene(t, "id "+id, r.corpo, `<div role="alert" class="flash flash-errore">Artigiano non trovato.</div>`)
	}
	for _, v := range []url.Values{{"artisan": {"boom"}}, {"nome": {"x"}}, {"artisan[]": {"x"}}} {
		v.Set("_csrf_token", b.token)
		contiene(t, "senza mappa", b.vai("POST", dove, v, nil).corpo, "Artigiano non trovato.")
	}
	if artigianoScritto(t, d, bianchi) != "Bianchi:-" || righeAudit(t, d, "artisan.updated") != 0 {
		t.Fatal("un rifiuto ha scritto")
	}
	r = dopoRedirect(t, b, b.vai("POST", dove, moduloArtigiano(b, "Bianchi Srl", "(02) 12.34"), nil))
	contiene(t, "aggiornato", r.corpo, `<div role="status" class="flash flash-info">Artigiano aggiornato.</div>`,
		`<input id="artisan-name-`+itoa(bianchi)+`" name="artisan[name]" type="text" value="Bianchi Srl"`,
		`<input id="artisan-phone-`+itoa(bianchi)+`" name="artisan[phone]" type="tel" value="021234"`)
	if artigianoScritto(t, d, bianchi) != "Bianchi Srl:021234" || righeAudit(t, d, "artisan.updated") != 1 {
		t.Fatal("non aggiornato")
	}
}

// inventario: ART-04
// «Elimina» (con la domanda «Eliminare <nome>?»): «Artigiano eliminato.»; con delle buste
// «Impossibile eliminare: questo artigiano ha 1 riparazione associata.»; un id forgiato
// «Artigiano non trovato.». Torna sempre alla pagina.
func TestEliminaArtigianoDallaPagina(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	libero := artigianoProva(t, d, "Libero", "")
	occupato := artigianoProva(t, d, "Occupato", "")
	b.vai("GET", rottaAnagrafiche, nil, nil)
	v := con(bustaProva(b), "repair[artisan_id]", itoa(occupato))
	if r := b.vai("POST", "/nuova", v, nil); r.codice != http.StatusFound {
		t.Fatalf("busta: %d", r.codice)
	}
	b.vai("GET", rottaAnagrafiche, nil, nil)
	forma := url.Values{"_csrf_token": {b.token}}
	r := dopoRedirect(t, b, b.vai("POST", "/settings/anagrafiche/artigiani/"+itoa(occupato)+"/elimina", forma, nil))
	contiene(t, "occupato", r.corpo, `<div role="alert" class="flash flash-errore">Impossibile eliminare: questo artigiano ha 1 riparazione associata.</div>`)
	for _, id := range []string{"invalid", "0", "-1", "99999999999999999999999999999", "999"} {
		forma.Set("_csrf_token", b.token)
		r = dopoRedirect(t, b, b.vai("POST", "/settings/anagrafiche/artigiani/"+id+"/elimina", forma, nil))
		contiene(t, "id "+id, r.corpo, `<div role="alert" class="flash flash-errore">Artigiano non trovato.</div>`)
	}
	if artigianoScritto(t, d, occupato) != "Occupato:-" || righeAudit(t, d, "artisan.deleted") != 0 {
		t.Fatal("un rifiuto ha scritto")
	}
	forma.Set("_csrf_token", b.token)
	// Il pulsante sta nel modulo della riga: arrivano anche nome e telefono, che non contano.
	forma.Set("artisan[name]", "Altro")
	r = dopoRedirect(t, b, b.vai("POST", "/settings/anagrafiche/artigiani/"+itoa(libero)+"/elimina", forma, nil))
	contiene(t, "eliminato", r.corpo, `<div role="status" class="flash flash-info">Artigiano eliminato.</div>`)
	nonContiene(t, "eliminato", r.corpo, `id="artisan-`+itoa(libero)+`"`)
	if artigianoScritto(t, d, libero) != "(assente)" || righeAudit(t, d, "artisan.deleted") != 1 {
		t.Fatal("non eliminato")
	}
	if contaRighe(t, d, `SELECT count(*) FROM artigiani WHERE nome = 'Altro'`) != 0 {
		t.Fatal("l'eliminazione ha scritto il nome")
	}
}

// inventario: ART-02, ART-03, ART-04
// Le tre azioni vogliono il token del modulo: senza, 403 e niente cambia.
func TestAnagraficheSenzaToken(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	id := artigianoProva(t, d, "Fermo", "1")
	for _, p := range []string{"/settings/anagrafiche/artigiani", "/settings/anagrafiche/artigiani/" + itoa(id), "/settings/anagrafiche/artigiani/" + itoa(id) + "/elimina"} {
		r := b.vai("POST", p, url.Values{"artisan[name]": {"Nuovo"}}, nil)
		if r.codice != http.StatusForbidden {
			t.Fatalf("%s: %d", p, r.codice)
		}
	}
	if artigianoScritto(t, d, id) != "Fermo:1" || contaRighe(t, d, `SELECT count(*) FROM artigiani`) != 1 {
		t.Fatal("scritto senza token")
	}
}

// inventario: ART-04, ART-03
// Un guasto vero del database: l'eliminazione di un artigiano che ha solo un sollecito (nessuna busta,
// nessuna consegna) cade sul vincolo RESTRICT di solleciti_artigiani: pagina d'errore 500, niente
// cambia, «artigiano non eliminato» nel registro (Dubbio 2). Un guasto della modifica (trigger) dà
// «Artigiano non trovato.» e «artigiano non modificato» nel registro.
func TestArtigianiGuastoDelDatabase(t *testing.T) {
	var reg bytes.Buffer
	h, d := facciaConBuste(t, &reg)
	b := dipendenteDentro(t, h)
	solo := artigianoProva(t, d, "Solo Solleciti", "")
	altro := artigianoProva(t, d, "Altro", "")
	b.vai("GET", rottaAnagrafiche, nil, nil)
	v := con(bustaProva(b), "repair[artisan_id]", itoa(altro))
	b.vai("GET", "/nuova", nil, nil)
	if r := b.vai("POST", "/nuova", con(v, "_csrf_token", b.token), nil); r.codice != http.StatusFound {
		t.Fatalf("busta: %d", r.codice)
	}
	eseguiSQL(t, d, `INSERT INTO solleciti_artigiani (busta_id, artigiano_id, giorni_fuori, creato_il) VALUES ((SELECT max(id) FROM buste), ?, 15, '2026-10-31T09:00:00.000000Z')`, solo)
	b.vai("GET", rottaAnagrafiche, nil, nil)
	r := b.vai("POST", "/settings/anagrafiche/artigiani/"+itoa(solo)+"/elimina", url.Values{"_csrf_token": {b.token}}, nil)
	if r.codice != http.StatusInternalServerError {
		t.Fatalf("eliminazione: %d", r.codice)
	}
	if artigianoScritto(t, d, solo) != "Solo Solleciti:-" || righeAudit(t, d, "artisan.deleted") != 0 {
		t.Fatal("il guasto ha cambiato qualcosa")
	}
	contiene(t, "registro", reg.String(), "artigiano non eliminato")
	eseguiSQL(t, d, `CREATE TRIGGER modifica_rotta BEFORE UPDATE ON artigiani BEGIN SELECT RAISE(ABORT, 'guasto di prova'); END`)
	b.vai("GET", rottaAnagrafiche, nil, nil)
	r = b.vai("POST", "/settings/anagrafiche/artigiani/"+itoa(solo), moduloArtigiano(b, "Nuovo Nome", "1"), nil)
	contiene(t, "modifica", r.corpo, `<div role="alert" class="flash flash-errore">Artigiano non trovato.</div>`)
	if artigianoScritto(t, d, solo) != "Solo Solleciti:-" || righeAudit(t, d, "artisan.updated") != 0 {
		t.Fatal("il guasto della modifica ha scritto")
	}
	contiene(t, "registro", reg.String(), "artigiano non modificato")
}

// inventario: ART-01
// «Nessun artigiano registrato.» con hidden resta nascosto: .sotto (display: block) viene dopo [hidden] con la
// stessa specificità e lo mostrerebbe; serve la regola sull'id, dopo .sotto (banco s076p, pagina.vuoto).
func TestAnagraficheVuotoNascostoDalCSS(t *testing.T) {
	css := string(leggiStatico("app.css"))
	sotto := strings.Index(css, "\n.sotto {")
	regola := strings.Index(css, "\n#artisans-empty[hidden] { display: none; }")
	if sotto < 0 || regola < 0 || regola < sotto {
		t.Fatalf("regola di #artisans-empty[hidden] mancante o prima di .sotto: %d %d", sotto, regola)
	}
	if strings.Contains(css[regola+1:], "\n#artisans-empty {") {
		t.Fatal("una regola di #artisans-empty dopo quella di [hidden]")
	}
}
