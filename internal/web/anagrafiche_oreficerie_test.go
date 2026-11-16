package web

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/db"
)

// La pagina «Anagrafiche», parte delle oreficerie clienti (anagrafiche.go, slice 086), come OreficerieLive
// dello standalone per gli eventi create e update: i casi di oreficerie_live_test.exs, senza LiveView. Le
// oreficerie non si eliminano. Nomi e numeri inventati. La riga 1 («Negozio», il negozio stesso) la semina la
// migrazione 0020, come quella dello standalone: non va messa a mano.

const rottaOreficerie = "/settings/anagrafiche/oreficerie"

func oreficeriaProva(t *testing.T, d *db.DB, nome, telefono string) int64 {
	t.Helper()
	var tel any
	if telefono != "" {
		tel = telefono
	}
	res, err := d.Scrittura.Exec(`INSERT INTO oreficerie (nome, telefono) VALUES (?, ?)`, nome, tel)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func oreficeriaScritta(t *testing.T, d *db.DB, id int64) string {
	t.Helper()
	var s string
	if err := d.Lettura.QueryRow(`SELECT nome || ':' || coalesce(telefono, '-') FROM oreficerie WHERE id = ?`, id).Scan(&s); err != nil {
		return "(assente)"
	}
	return s
}

func moduloOreficeria(b *browser, nome, telefono string) url.Values {
	return url.Values{"_csrf_token": {b.token}, "oreficeria[name]": {nome}, "oreficeria[phone]": {telefono}}
}

// inventario: ORE-01
// La pagina del personale (titolare e dipendente) ha il riquadro #oreficerie prima degli artigiani:
// «Oreficerie clienti» con la frase sui nomi, una riga per oreficeria esterna (nome e telefono nel modulo di
// modifica e «Salva», senza «Elimina»), per nome e id, il modulo «Nuova oreficeria *» e «Nessuna oreficeria
// registrata.» nascosto quando l'elenco non è vuoto. La riga 1, il negozio stesso, non c'è mai.
func TestPaginaAnagraficheOreficerie(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	r := b.vai("GET", rottaAnagrafiche, nil, nil)
	if r.codice != 200 {
		t.Fatalf("pagina vuota: %d", r.codice)
	}
	contiene(t, "vuota", r.corpo,
		`<section id="oreficerie" class="riquadro">`+"\n"+`<h2>Oreficerie clienti</h2>`+"\n"+
			`<p class="sotto">Modificare il nome aggiorna anche le riparazioni passate associate (sono collegate per ID, non per nome).</p>`,
		`<div id="oreficerie-list">`+"\n"+`<p id="oreficerie-empty" class="sotto">Nessuna oreficeria registrata.</p>`,
		`<form id="new-oreficeria-form" action="/settings/anagrafiche/oreficerie" method="post" class="anagrafica-nuovo" data-invio-unico>`+"\n"+`<input name="_csrf_token" type="hidden" value="`+b.token+`">`,
		`<label for="new-oreficeria-name">Nuova oreficeria *</label>`+"\n"+`<input id="new-oreficeria-name" name="oreficeria[name]" type="text" value="" placeholder="Es.: Gioielleria Rossi" required autocomplete="off">`,
		`<label for="new-oreficeria-phone">Telefono WhatsApp</label>`+"\n"+`<input id="new-oreficeria-phone" name="oreficeria[phone]" type="tel" value="" placeholder="Es.: 3391234567" inputmode="tel" autocomplete="off">`)
	nonContiene(t, "vuota", r.corpo, `value="Negozio"`, `id="oreficerie-1"`)
	if strings.Index(r.corpo, `<section id="oreficerie"`) > strings.Index(r.corpo, `<section id="artigiani"`) {
		t.Fatal("le oreficerie vanno prima degli artigiani")
	}
	zeta := oreficeriaProva(t, d, "Zeta", "")
	alfa := oreficeriaProva(t, d, "alfa", "0490000001")
	beta := oreficeriaProva(t, d, "Beta <&>", "")
	r = b.vai("GET", rottaAnagrafiche, nil, nil)
	contiene(t, "piena", r.corpo, `<p id="oreficerie-empty" class="sotto" hidden>Nessuna oreficeria registrata.</p>`,
		`<div id="oreficerie-`+itoa(alfa)+`" class="anagrafica-riga anagrafica-riga-senza-elimina">`+"\n"+
			`<form id="edit-oreficeria-`+itoa(alfa)+`" action="/settings/anagrafiche/oreficerie/`+itoa(alfa)+`" method="post" data-invio-unico>`+"\n"+
			`<input name="_csrf_token" type="hidden" value="`,
		`<label for="oreficeria-name-`+itoa(alfa)+`">Nome</label>`+"\n"+`<input id="oreficeria-name-`+itoa(alfa)+`" name="oreficeria[name]" type="text" value="alfa" placeholder="Nome" required>`,
		`<label for="oreficeria-phone-`+itoa(alfa)+`">Telefono</label>`+"\n"+`<input id="oreficeria-phone-`+itoa(alfa)+`" name="oreficeria[phone]" type="tel" value="0490000001" placeholder="Telefono" inputmode="tel">`,
		`<button type="submit">Salva</button>`+"\n</form>",
		`value="Beta &lt;&amp;&gt;"`,
		`id="oreficeria-phone-`+itoa(zeta)+`" name="oreficeria[phone]" type="tel" value=""`)
	nonContiene(t, "piena", r.corpo, `value="Negozio"`, `id="oreficerie-1"`, `id="delete-oreficeria`, `/oreficerie/`+itoa(alfa)+`/elimina`)
	da, a := strings.Index(r.corpo, `<section id="oreficerie"`), strings.Index(r.corpo, `<section id="artigiani"`)
	if da < 0 || a < da {
		t.Fatalf("riquadri: %d %d", da, a)
	}
	sez := r.corpo[da:a]
	nonContiene(t, "riquadro", sez, "Elimina", "pulsante-pericolo", "data-confirm")
	iBeta, iZeta, iAlfa := strings.Index(r.corpo, `id="oreficerie-`+itoa(beta)+`"`), strings.Index(r.corpo, `id="oreficerie-`+itoa(zeta)+`"`), strings.Index(r.corpo, `id="oreficerie-`+itoa(alfa)+`"`)
	if iBeta < 0 || iBeta > iZeta || iZeta > iAlfa {
		t.Fatalf("ordine: %d %d %d", iBeta, iZeta, iAlfa)
	}
	if strings.Index(r.corpo, `id="oreficerie-list"`) > strings.Index(r.corpo, `id="new-oreficeria-form"`) {
		t.Fatal("il modulo nuovo va dopo l'elenco")
	}
	// Lo stesso per il titolare.
	tit := titolareDentro(t, h)
	contiene(t, "titolare", tit.vai("GET", rottaAnagrafiche, nil, nil).corpo, `id="oreficeria-name-`+itoa(alfa)+`"`)
}

// inventario: ORE-02
// «+ Aggiungi»: il nome ripulito, il telefono normalizzato, «Oreficeria "X" aggiunta.» e la pagina con il
// modulo vuoto; il nome vuoto ridisegna con «Il nome è obbligatorio.», l'errore sotto il campo e i valori
// ripuliti; un doppione a maiuscole esatte, anche del negozio, nomina il nome ripulito e lascia i valori
// scritti; con altre maiuscole è un nome nuovo; un modulo senza la mappa oreficeria è «Oreficeria non
// aggiunta.». Il dipendente può.
func TestNuovaOreficeriaDallaPagina(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	b.vai("GET", rottaAnagrafiche, nil, nil)
	r := dopoRedirect(t, b, b.vai("POST", rottaOreficerie, moduloOreficeria(b, "  Gioielleria Rossi  ", "+39 333-111"), nil))
	contiene(t, "creata", r.corpo, `<div role="status" class="flash flash-info">Oreficeria &#34;Gioielleria Rossi&#34; aggiunta.</div>`,
		`value="Gioielleria Rossi"`, `value="&#43;39333111"`,
		`<input id="new-oreficeria-name" name="oreficeria[name]" type="text" value=""`, `<input id="new-oreficeria-phone" name="oreficeria[phone]" type="tel" value=""`)
	if got := contaRighe(t, d, `SELECT count(*) FROM oreficerie WHERE nome = 'Gioielleria Rossi' AND telefono = '+39333111' AND id > 1`); got != 1 {
		t.Fatalf("righe %d", got)
	}
	if righeAudit(t, d, "oreficeria.created") != 1 {
		t.Fatal("audit")
	}
	if contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'oreficeria.created' AND attore = 'banco@esempio.it'`) != 1 {
		t.Fatal("attore")
	}
	// Nome vuoto.
	r = b.vai("POST", rottaOreficerie, moduloOreficeria(b, "   ", " 333 (0) "), nil)
	if r.codice != 200 {
		t.Fatalf("vuoto: %d", r.codice)
	}
	contiene(t, "vuoto", r.corpo, `<div role="alert" class="flash flash-errore">Il nome è obbligatorio.</div>`,
		`<input id="new-oreficeria-name" name="oreficeria[name]" type="text" value="" placeholder="Es.: Gioielleria Rossi" required autocomplete="off">`+"\n"+`<p class="field-error">Campo obbligatorio.</p>`,
		`<input id="new-oreficeria-phone" name="oreficeria[phone]" type="tel" value="3330"`)
	// Doppione, con gli spazi: il messaggio dice il nome ripulito, il modulo tiene quello scritto.
	r = b.vai("POST", rottaOreficerie, moduloOreficeria(b, "   Gioielleria Rossi   ", " +39 333-1 "), nil)
	contiene(t, "doppione", r.corpo, `<div role="alert" class="flash flash-errore">Esiste già un&#39;oreficeria con il nome &#34;Gioielleria Rossi&#34;.</div>`,
		`<input id="new-oreficeria-name" name="oreficeria[name]" type="text" value="   Gioielleria Rossi   "`, `<input id="new-oreficeria-phone" name="oreficeria[phone]" type="tel" value=" &#43;39 333-1 "`)
	nonContiene(t, "doppione", r.corpo, "field-error")
	// Il nome del negozio (riga 1) a maiuscole esatte è un doppione; con altre maiuscole no.
	r = b.vai("POST", rottaOreficerie, moduloOreficeria(b, "Negozio", ""), nil)
	contiene(t, "negozio", r.corpo, `Esiste già un&#39;oreficeria con il nome &#34;Negozio&#34;.`)
	dopoRedirect(t, b, b.vai("POST", rottaOreficerie, moduloOreficeria(b, "GIOIELLERIA ROSSI", ""), nil))
	// Moduli forgiati.
	for _, v := range []url.Values{
		{"oreficeria": {"boom"}}, {"oreficeria[]": {"x"}}, {"nope": {"1"}}, {},
		{"oreficeria": {"boom"}, "oreficeria[name]": {"Forgiata"}},
		// Il modulo di un'altra anagrafica non è la mappa oreficeria.
		{"artisan[name]": {"Forgiata"}},
	} {
		v.Set("_csrf_token", b.token)
		r = b.vai("POST", rottaOreficerie, v, nil)
		if r.codice != 200 {
			t.Fatalf("%v: %d", v, r.codice)
		}
		contiene(t, "forgiato", r.corpo, `<div role="alert" class="flash flash-errore">Oreficeria non aggiunta.</div>`)
	}
	// Un campo ripetuto vale l'ultimo valore, come Plug.
	v2 := url.Values{"_csrf_token": {b.token}, "oreficeria[name]": {"Prima Ripetuta", "Seconda Ripetuta"}}
	dopoRedirect(t, b, b.vai("POST", rottaOreficerie, v2, nil))
	if contaRighe(t, d, `SELECT count(*) FROM oreficerie WHERE nome = 'Seconda Ripetuta'`) != 1 || contaRighe(t, d, `SELECT count(*) FROM oreficerie WHERE nome = 'Prima Ripetuta'`) != 0 {
		t.Fatal("campo ripetuto: non vale l'ultimo")
	}
	// Un modulo con la mappa ma senza il nome è un nome vuoto.
	v := url.Values{"_csrf_token": {b.token}, "oreficeria[phone]": {"1"}}
	contiene(t, "senza nome", b.vai("POST", rottaOreficerie, v, nil).corpo, "Il nome è obbligatorio.")
	if contaRighe(t, d, `SELECT count(*) FROM oreficerie WHERE id > 1`) != 3 || righeAudit(t, d, "oreficeria.created") != 3 {
		t.Fatal("un rifiuto ha scritto")
	}
}

// inventario: ORE-03
// «Salva» su una riga: «Oreficeria aggiornata.» con i valori nuovi; il doppione senza maiuscole nomina il
// nome scritto e la riga tiene i valori scritti, ma il negozio (riga 1) non conta; il nome vuoto è «Il nome
// è obbligatorio.»; un id forgiato, la riga 1 o un modulo senza la mappa oreficeria sono «Oreficeria non
// trovata o non modificabile.»; niente cambia e la riga 1 resta com'è.
func TestModificaOreficeriaDallaPagina(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	rossi := oreficeriaProva(t, d, "Rossi", "")
	bianchi := oreficeriaProva(t, d, "Bianchi", "")
	b.vai("GET", rottaAnagrafiche, nil, nil)
	dove := rottaOreficerie + "/" + itoa(bianchi)
	r := b.vai("POST", dove, moduloOreficeria(b, "ROSSI", " +39 333-1 "), nil)
	if r.codice != 200 {
		t.Fatalf("doppione: %d", r.codice)
	}
	contiene(t, "doppione", r.corpo, `<div role="alert" class="flash flash-errore">Esiste già un&#39;oreficeria con il nome &#34;ROSSI&#34;.</div>`,
		`<input id="oreficeria-name-`+itoa(bianchi)+`" name="oreficeria[name]" type="text" value="ROSSI"`,
		`<input id="oreficeria-phone-`+itoa(bianchi)+`" name="oreficeria[phone]" type="tel" value=" &#43;39 333-1 "`,
		`<input id="oreficeria-name-`+itoa(rossi)+`" name="oreficeria[name]" type="text" value="Rossi"`)
	r = b.vai("POST", dove, moduloOreficeria(b, "  ", "5"), nil)
	contiene(t, "vuoto", r.corpo, `<div role="alert" class="flash flash-errore">Il nome è obbligatorio.</div>`,
		`<input id="oreficeria-name-`+itoa(bianchi)+`" name="oreficeria[name]" type="text" value="  "`)
	nonContiene(t, "vuoto", r.corpo, "field-error")
	for _, id := range []string{"invalid", "0", "-1", "99999999999999999999999999999", "999", "1"} {
		r = b.vai("POST", rottaOreficerie+"/"+id, moduloOreficeria(b, "Alterata", "222"), nil)
		contiene(t, "id "+id, r.corpo, `<div role="alert" class="flash flash-errore">Oreficeria non trovata o non modificabile.</div>`)
	}
	for _, v := range []url.Values{{"oreficeria": {"boom"}}, {"nome": {"x"}}, {"oreficeria[]": {"x"}}, {"artisan[name]": {"x"}}} {
		v.Set("_csrf_token", b.token)
		contiene(t, "senza mappa", b.vai("POST", dove, v, nil).corpo, "Oreficeria non trovata o non modificabile.")
	}
	if oreficeriaScritta(t, d, bianchi) != "Bianchi:-" || oreficeriaScritta(t, d, 1) != "Negozio:-" || righeAudit(t, d, "oreficeria.updated") != 0 {
		t.Fatal("un rifiuto ha scritto")
	}
	// Il nome del negozio con altre maiuscole non è un doppione (la riga 1 non conta), il nome esatto sì.
	contiene(t, "negozio esatto", b.vai("POST", dove, moduloOreficeria(b, "Negozio", ""), nil).corpo,
		`Esiste già un&#39;oreficeria con il nome &#34;Negozio&#34;.`)
	dopoRedirect(t, b, b.vai("POST", dove, moduloOreficeria(b, "NEGOZIO", ""), nil))
	r = dopoRedirect(t, b, b.vai("POST", dove, moduloOreficeria(b, "Bianchi Srl", "(02) 12.34"), nil))
	contiene(t, "aggiornata", r.corpo, `<div role="status" class="flash flash-info">Oreficeria aggiornata.</div>`,
		`<input id="oreficeria-name-`+itoa(bianchi)+`" name="oreficeria[name]" type="text" value="Bianchi Srl"`,
		`<input id="oreficeria-phone-`+itoa(bianchi)+`" name="oreficeria[phone]" type="tel" value="021234"`)
	if oreficeriaScritta(t, d, bianchi) != "Bianchi Srl:021234" || righeAudit(t, d, "oreficeria.updated") != 2 || oreficeriaScritta(t, d, 1) != "Negozio:-" {
		t.Fatal("non aggiornata")
	}
}

// inventario: ORE-03
// Le oreficerie non si eliminano: non c'è nessuna rotta per farlo (404), e niente cambia.
func TestOreficerieSenzaEliminazione(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	id := oreficeriaProva(t, d, "Resta", "1")
	b.vai("GET", rottaAnagrafiche, nil, nil)
	forma := url.Values{"_csrf_token": {b.token}}
	for _, p := range []string{rottaOreficerie + "/" + itoa(id) + "/elimina", rottaOreficerie + "/" + itoa(id) + "/delete"} {
		if r := b.vai("POST", p, forma, nil); r.codice != http.StatusNotFound {
			t.Fatalf("%s: %d", p, r.codice)
		}
	}
	if r := b.vai("DELETE", rottaOreficerie+"/"+itoa(id), nil, nil); r.codice == http.StatusOK || r.codice == http.StatusFound {
		t.Fatalf("DELETE: %d", r.codice)
	}
	if oreficeriaScritta(t, d, id) != "Resta:1" || righeAudit(t, d, "oreficeria.deleted") != 0 {
		t.Fatal("l'oreficeria è cambiata")
	}
}

// inventario: ORE-02, ORE-03
// Le due azioni vogliono il token del modulo: senza, 403 e niente cambia.
func TestOreficerieSenzaToken(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	id := oreficeriaProva(t, d, "Ferma", "1")
	for _, p := range []string{rottaOreficerie, rottaOreficerie + "/" + itoa(id)} {
		r := b.vai("POST", p, url.Values{"oreficeria[name]": {"Nuova"}}, nil)
		if r.codice != http.StatusForbidden {
			t.Fatalf("%s: %d", p, r.codice)
		}
	}
	if oreficeriaScritta(t, d, id) != "Ferma:1" || contaRighe(t, d, `SELECT count(*) FROM oreficerie WHERE id > 1`) != 1 {
		t.Fatal("scritto senza token")
	}
}

// inventario: ORE-02, ORE-03
// Un guasto vero del database: la creazione che non si scrive è «Oreficeria non aggiunta.» e la modifica
// «Oreficeria non trovata o non modificabile.» (il ramo di tutti gli errori che non sono il nome), con la riga nel
// registro; niente cambia.
func TestOreficerieGuastoDelDatabase(t *testing.T) {
	var reg bytes.Buffer
	h, d := facciaConBuste(t, &reg)
	b := dipendenteDentro(t, h)
	id := oreficeriaProva(t, d, "Guasta", "")
	b.vai("GET", rottaAnagrafiche, nil, nil)
	eseguiSQL(t, d, `CREATE TRIGGER no_audit_oreficerie BEFORE INSERT ON eventi_audit BEGIN SELECT RAISE(ABORT, 'guasto di prova'); END`)
	r := b.vai("POST", rottaOreficerie, moduloOreficeria(b, "Nuova Guasta", "1"), nil)
	contiene(t, "creazione", r.corpo, `<div role="alert" class="flash flash-errore">Oreficeria non aggiunta.</div>`)
	contiene(t, "registro", reg.String(), "oreficeria non aggiunta")
	b.vai("GET", rottaAnagrafiche, nil, nil)
	r = b.vai("POST", rottaOreficerie+"/"+itoa(id), moduloOreficeria(b, "Cambiata", "2"), nil)
	contiene(t, "modifica", r.corpo, `<div role="alert" class="flash flash-errore">Oreficeria non trovata o non modificabile.</div>`)
	contiene(t, "registro", reg.String(), "oreficeria non modificata")
	if oreficeriaScritta(t, d, id) != "Guasta:-" || contaRighe(t, d, `SELECT count(*) FROM oreficerie WHERE nome = 'Nuova Guasta'`) != 0 {
		t.Fatal("il guasto ha scritto")
	}
}

// inventario: ORE-01
// «Nessuna oreficeria registrata.» con hidden resta nascosto: .sotto (display: block) viene dopo [hidden] con la
// stessa specificità e lo mostrerebbe; serve la regola sull'id, dopo .sotto, come per gli artigiani.
func TestAnagraficheOreficerieVuotoNascostoDalCSS(t *testing.T) {
	css := string(leggiStatico("app.css"))
	sotto := strings.Index(css, "\n.sotto {")
	regola := strings.Index(css, "\n#oreficerie-empty[hidden] { display: none; }")
	if sotto < 0 || regola < 0 || regola < sotto {
		t.Fatalf("regola di #oreficerie-empty[hidden] mancante o prima di .sotto: %d %d", sotto, regola)
	}
	if strings.Contains(css[regola+1:], "\n#oreficerie-empty {") {
		t.Fatal("una regola di #oreficerie-empty dopo quella di [hidden]")
	}
}

// oreficerieRotte è la Buste vera con la lettura delle oreficerie che non riesce.
type oreficerieRotte struct{ Buste }

func (oreficerieRotte) Oreficerie(context.Context) ([]buste.OreficeriaCliente, error) {
	return nil, errors.New("lettura di prova")
}

// inventario: ORE-01
// Se l'anagrafica delle oreficerie non si legge la pagina è la pagina d'errore (500) con la riga «anagrafica delle
// oreficerie non letta» nel registro, e niente si disegna a metà.
func TestAnagraficheOreficerieNonLetta(t *testing.T) {
	var reg bytes.Buffer
	h, _ := facciaConBusteCon(t, &reg, func(b Buste) Buste { return oreficerieRotte{b} })
	b := dipendenteDentro(t, h)
	r := b.vai("GET", rottaAnagrafiche, nil, nil)
	if r.codice != http.StatusInternalServerError {
		t.Fatalf("pagina: %d", r.codice)
	}
	nonContiene(t, "errore", r.corpo, `id="oreficerie"`, `id="artigiani"`)
	contiene(t, "registro", reg.String(), "anagrafica delle oreficerie non letta")
}
