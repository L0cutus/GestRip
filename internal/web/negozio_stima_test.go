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

// La sezione «Stima tempi» della pagina «Impostazioni del negozio» (negozio.go, slice 102, ADM-04, RIP-H-55), come
// l'evento save_estimate di ShopLive dello standalone (go-baseline) e Settings.Estimate, senza LiveView:
//   - POST /settings/shop/stima con estimate[enabled], estimate[artisan_min] e estimate[artisan_max];
//   - riuscito, ritorno alla pagina (302) con «Stima tempi salvata.» (push_navigate: la pagina mostra quello che è
//     stato tenuto, non quello che si è scritto); un modulo senza la mappa estimate è «Richiesta non riconosciuta:
//     nessuna impostazione salvata.»; un guasto del database «Stima tempi non salvata: riprova.».
// Solo il titolare. Dati inventati.

const rottaStima = "/settings/shop/stima"

// moduloStimaWeb è il modulo della stima con i campi dati (gli altri non arrivano).
func moduloStimaWeb(b *browser, kv ...string) url.Values {
	v := url.Values{"_csrf_token": {b.token}}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set("estimate["+kv[i]+"]", kv[i+1])
	}
	return v
}

// chiaviStima dà le tre chiavi come sono salvate: «1|7|8», «∅» per una riga che non c'è.
func chiaviStima(t *testing.T, d *db.DB) string {
	t.Helper()
	var out []string
	for _, k := range []string{"stima_tempi_attiva", "stima_tempi_artigiano_min", "stima_tempi_artigiano_max"} {
		var v string
		if err := d.Lettura.QueryRow(`SELECT valore FROM impostazioni WHERE chiave = ?`, k).Scan(&v); err != nil {
			v = "∅"
		}
		out = append(out, v)
	}
	return strings.Join(out, "|")
}

func righeAuditStima(t *testing.T, d *db.DB) int {
	t.Helper()
	return contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'settings.estimate_updated'`)
}

const (
	campoAttiva = `<input type="hidden" name="estimate[enabled]" value="false"><label class="gs-spunta"><input type="checkbox" id="estimate_enabled" name="estimate[enabled]" value="true"%s><span>Mostra la stima ai clienti</span></label>`
	campoMinimo = `<label for="estimate_artisan_min"><span class="label">Supplemento artigiano: giorni minimi</span>` + "\n" +
		`<input type="number" id="estimate_artisan_min" name="estimate[artisan_min]" value="%s" min="0" max="90"></label>`
	campoMassimo = `<label for="estimate_artisan_max"><span class="label">Supplemento artigiano: giorni massimi</span>` + "\n" +
		`<input type="number" id="estimate_artisan_max" name="estimate[artisan_max]" value="%s" min="0" max="90"></label>`
)

func campiStima(acceso bool, min, max string) []string {
	spunta := ""
	if acceso {
		spunta = " checked"
	}
	return []string{strings.Replace(campoAttiva, "%s", spunta, 1), strings.Replace(campoMinimo, "%s", min, 1), strings.Replace(campoMassimo, "%s", max, 1)}
}

// inventario: ADM-04
// La pagina del titolare ha, dopo il profilo, la sezione «Stima tempi» con i testi dello standalone: la frase, «Oggi»
// (con meno di venti buste pronte «Dati insufficienti» e i conti), il modulo #estimate-form (POST con il token, un solo
// invio) con l'interruttore (il nascosto «false» come core_components, spento), i giorni minimi e massimi (7 e 8, da 0 a
// 90) e «Salva stima tempi». Il profilo resta com'è.
func TestPaginaNegozioStima(t *testing.T) {
	h, _ := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	r := b.vai("GET", rottaNegozio, nil, nil)
	if r.codice != 200 {
		t.Fatalf("pagina %d", r.codice)
	}
	contiene(t, "sezione", r.corpo, `<section id="shop-estimate" class="negozio-sezione">`+"\n"+`<h2>Stima tempi</h2>`,
		"Mostra al cliente, sulla sua pagina e nel primo messaggio WhatsApp, in quanti giorni dal ritiro una riparazione è di solito pronta, "+
			"calcolato sulle buste pronte degli ultimi 12 mesi. Non compare se la busta ha una data di consegna. Il supplemento vale per le buste "+
			"consegnate all'artigiano finché non ce ne sono almeno 10 da misurare. Se il testo «Invia link stato cliente» è stato personalizzato o "+
			"importato, aggiungi <code>{stima}</code> in Testi perché la stima arrivi anche su WhatsApp.",
		`<p id="estimate-today" class="blocco-piccolo">Dati insufficienti: servono almeno 20 buste pronte (ora 0) — 0 buste in bottega, 0 passate dall'artigiano; `+
			`supplemento artigiano 7-8 giorni (dalle impostazioni).</p>`,
		`<form id="estimate-form" action="/settings/shop/stima" method="post" data-invio-unico>`+"\n"+`<input name="_csrf_token" type="hidden" value="`+b.token+`">`,
		`<button type="submit">Salva stima tempi</button>`)
	contiene(t, "campi", r.corpo, campiStima(false, "7", "8")...)
	// Dopo il profilo (e il suo «Salva impostazioni»), prima della chiusura del pannello.
	if i, j, k := strings.Index(r.corpo, `id="shop-form"`), strings.Index(r.corpo, `id="shop-estimate"`), strings.Index(r.corpo, `id="estimate-form"`); !(0 < i && i < j && j < k) {
		t.Errorf("ordine: profilo %d, sezione %d, modulo %d", i, j, k)
	}
	nonContiene(t, "pagina", r.corpo, `class="field-error"`, "flash-errore", "flash-info", "Operativita", "Logo e icone", "Colori della barra")
}

// inventario: ADM-04
// Salvare: ritorno alla pagina (302) con «Stima tempi salvata.» una volta sola, la pagina con l'interruttore acceso e i
// giorni salvati, le tre chiavi nella tabella («1», «10», «12»), una riga «settings.estimate_updated» con l'attore e i
// nomi delle chiavi cambiate (mai i valori), e il profilo del negozio non toccato.
func TestSalvaStimaNegozio(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	b.vai("GET", rottaNegozio, nil, nil)
	r := b.vai("POST", rottaStima, moduloStimaWeb(b, "enabled", "true", "artisan_min", "10", "artisan_max", "12"), nil)
	if !redirectA(r, rottaNegozio) {
		t.Fatalf("atteso il ritorno alla pagina, avuto %d %q", r.codice, r.location)
	}
	r = b.vai("GET", rottaNegozio, nil, nil)
	contiene(t, "dopo", r.corpo, `<div role="status" class="flash flash-info">Stima tempi salvata.</div>`)
	contiene(t, "campi", r.corpo, campiStima(true, "10", "12")...)
	contiene(t, "oggi", r.corpo, "supplemento artigiano 10-12 giorni (dalle impostazioni).</p>")
	nonContiene(t, "dopo", r.corpo, "field-error", "flash-errore")
	if r2 := b.vai("GET", rottaNegozio, nil, nil); strings.Contains(r2.corpo, "Stima tempi salvata") {
		t.Error("il messaggio resta alla pagina dopo")
	}
	if got := chiaviStima(t, d); got != "1|10|12" {
		t.Fatalf("chiavi %s", got)
	}
	var azione, risorsa, id, esito, dati, attore string
	if err := d.Lettura.QueryRow(`SELECT azione, risorsa, id_risorsa, esito, dati, attore FROM eventi_audit WHERE azione = 'settings.estimate_updated'`).
		Scan(&azione, &risorsa, &id, &esito, &dati, &attore); err != nil {
		t.Fatalf("audit: %v", err)
	}
	if risorsa != "settings" || id != "estimate" || esito != "ok" || attore != "titolare@esempio.it" ||
		dati != `{"changed":["stima_tempi_attiva","stima_tempi_artigiano_min","stima_tempi_artigiano_max"]}` {
		t.Errorf("audit %s|%s|%s|%s|%s|%s", azione, risorsa, id, esito, dati, attore)
	}
	if contaRighe(t, d, `SELECT count(*) FROM profilo_negozio`)+contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'shop_profile.updated'`) != 0 {
		t.Fatal("la stima ha scritto il profilo")
	}
	// Spegnerla: il nascosto «false» arriva prima della casella; l'ultimo valore vale (Plug).
	f := moduloStimaWeb(b, "artisan_min", "10", "artisan_max", "12")
	f["estimate[enabled]"] = []string{"false"}
	if !redirectA(b.vai("POST", rottaStima, f, nil), rottaNegozio) || chiaviStima(t, d) != "0|10|12" {
		t.Fatalf("spenta: %s", chiaviStima(t, d))
	}
	f["estimate[enabled]"] = []string{"false", "true"}
	if !redirectA(b.vai("POST", rottaStima, f, nil), rottaNegozio) || chiaviStima(t, d) != "1|10|12" {
		t.Fatalf("accesa (nascosto e casella): %s", chiaviStima(t, d))
	}
}

// inventario: ADM-04
// Giorni fuori campo o illeggibili non si rifiutano: si riportano (−5 a 0, 120 a 90, un massimo sotto il minimo alzato a
// quello, un testo che non è un intero al valore di partenza 7 o 8) e la pagina mostra quello che è stato tenuto, non
// quello che si è scritto.
func TestStimaNegozioGiorniRiportati(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	b.vai("GET", rottaNegozio, nil, nil)
	for _, c := range []struct{ min, max, salvate, attesoMin, attesoMax string }{
		{"-5", "120", "1|0|90", "0", "90"},
		{"30", "10", "1|30|30", "30", "30"},
		{"abc", "", "1|7|8", "7", "8"},
		{"4,5", " 6", "1|7|8", "7", "8"},
		{"+6", "007", "1|6|7", "6", "7"},
		{"99999999999999999999999", "1", "1|90|90", "90", "90"},
	} {
		r := b.vai("POST", rottaStima, moduloStimaWeb(b, "enabled", "true", "artisan_min", c.min, "artisan_max", c.max), nil)
		if !redirectA(r, rottaNegozio) {
			t.Errorf("%q, %q: %d %q", c.min, c.max, r.codice, r.location)
			continue
		}
		if got := chiaviStima(t, d); got != c.salvate {
			t.Errorf("%q, %q: salvate %s, attese %s", c.min, c.max, got, c.salvate)
		}
		contiene(t, c.min+","+c.max, b.vai("GET", rottaNegozio, nil, nil).corpo, campiStima(true, c.attesoMin, c.attesoMax)[1:]...)
	}
	// Un invio senza uno dei due campi tiene quello che c'è; un campo che non è un testo vale il valore di partenza.
	b.vai("POST", rottaStima, moduloStimaWeb(b, "enabled", "true", "artisan_min", "20", "artisan_max", "30"), nil)
	b.vai("POST", rottaStima, moduloStimaWeb(b, "enabled", "true", "artisan_min", "21"), nil)
	if got := chiaviStima(t, d); got != "1|21|30" {
		t.Errorf("massimo mancante: %s", got)
	}
	f := moduloStimaWeb(b, "enabled", "true", "artisan_min", "25")
	f.Set("estimate[artisan_max][]", "40")
	b.vai("POST", rottaStima, f, nil)
	if got := chiaviStima(t, d); got != "1|25|25" {
		t.Errorf("massimo come lista: %s", got)
	}
}

// inventario: ADM-04
// Un modulo senza la mappa estimate (un invio forgiato) non salva niente e dice «Richiesta non riconosciuta: nessuna
// impostazione salvata.» (pagina ridisegnata, 200); senza token il modulo non passa (403).
func TestStimaNegozioModuliForgiati(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	b.vai("GET", rottaNegozio, nil, nil)
	for nome, f := range map[string]url.Values{
		"vuoto":       {"_csrf_token": {b.token}},
		"altro":       {"_csrf_token": {b.token}, "sciocchezze": {"1"}},
		"testo":       {"_csrf_token": {b.token}, "estimate": {"x"}},
		"lista":       {"_csrf_token": {b.token}, "estimate[]": {"x"}},
		"del profilo": {"_csrf_token": {b.token}, "shop_profile[shop_name]": {"x"}},
		"prefisso":    {"_csrf_token": {b.token}, "xestimate[enabled]": {"true"}, "estimate_x[enabled]": {"true"}},
	} {
		r := b.vai("POST", rottaStima, f, nil)
		if r.codice != 200 {
			t.Errorf("%s: %d %q", nome, r.codice, r.location)
			continue
		}
		contiene(t, nome, r.corpo, `<div role="alert" class="flash flash-errore">Richiesta non riconosciuta: nessuna impostazione salvata.</div>`, `id="estimate-form"`)
	}
	// Una lista «estimate[]» insieme ai campi: non è la mappa estimate (Plug non la farebbe), come per il profilo.
	f := moduloStimaWeb(b, "enabled", "true", "artisan_min", "20")
	f.Set("estimate[]", "x")
	contiene(t, "lista e campi", b.vai("POST", rottaStima, f, nil).corpo, "Richiesta non riconosciuta: nessuna impostazione salvata.")
	if chiaviStima(t, d) != "∅|∅|∅" || righeAuditStima(t, d) != 0 {
		t.Fatal("un invio forgiato ha scritto")
	}
	// Il modulo del profilo, a sua volta, non salva la stima: «estimate[…]» su /settings/shop non è la mappa del profilo.
	r := b.vai("POST", rottaNegozio, moduloStimaWeb(b, "enabled", "true"), nil)
	contiene(t, "stima sul profilo", r.corpo, "Richiesta non riconosciuta: nessuna impostazione salvata.")
	if chiaviStima(t, d) != "∅|∅|∅" {
		t.Fatal("il modulo del profilo ha scritto la stima")
	}
	senza := moduloStimaWeb(b, "enabled", "true")
	senza.Del("_csrf_token")
	if r := b.vai("POST", rottaStima, senza, nil); r.codice != http.StatusForbidden || chiaviStima(t, d) != "∅|∅|∅" {
		t.Fatalf("POST senza token: %d", r.codice)
	}
}

// inventario: ADM-04
// Solo il titolare: il dipendente che forza un salvataggio va alla home e non scrive niente; chi non è entrato va al
// login. La pagina del dipendente è già rifiutata (TestNegozioSoloTitolare).
func TestStimaNegozioSoloTitolare(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	dip := dipendenteDentro(t, h)
	dip.vai("GET", "/", nil, nil)
	if r := dip.vai("POST", rottaStima, moduloStimaWeb(dip, "enabled", "true", "artisan_min", "20", "artisan_max", "30"), nil); !redirectA(r, "/") {
		t.Errorf("POST del dipendente: %d %q", r.codice, r.location)
	}
	contiene(t, "home", dip.vai("GET", "/", nil, nil).corpo, `Operazione riservata al titolare.`)
	if chiaviStima(t, d) != "∅|∅|∅" || righeAuditStima(t, d) != 0 {
		t.Fatal("il dipendente ha scritto la stima")
	}
	anon := nuovoBrowser(t, h)
	if r := anon.vai("POST", rottaStima, url.Values{"estimate[enabled]": {"true"}}, nil); r.codice == http.StatusOK || chiaviStima(t, d) != "∅|∅|∅" {
		t.Errorf("senza accesso: %d", r.codice)
	}
}

// stimaGuasta avvolge le buste di una prova: la lettura o la scrittura delle impostazioni della stima falliscono come
// un database guasto.
type stimaGuasta struct {
	Buste
	lettura, scrittura bool
}

func (p stimaGuasta) PaginaStima(ctx context.Context) (buste.PaginaStima, error) {
	if p.lettura {
		return buste.PaginaStima{}, errors.New("guasto della lettura")
	}
	return p.Buste.PaginaStima(ctx)
}

func (p stimaGuasta) SalvaStima(ctx context.Context, m buste.ModuloStima, c audit.Contesto) error {
	if p.scrittura {
		return errors.New("guasto della scrittura")
	}
	return p.Buste.SalvaStima(ctx, m, c)
}

// inventario: ADM-04
// Un guasto del database: se la stima non si legge la pagina è quella d'errore (500) con la riga nel registro; se non si
// scrive, la pagina dice «Stima tempi non salvata: riprova.» con i valori salvati (non quelli scritti: come assign_estimate)
// e una riga nel registro, senza i valori.
func TestStimaNegozioGuasto(t *testing.T) {
	var reg bytes.Buffer
	h, _ := facciaConBusteCon(t, &reg, func(b Buste) Buste { return stimaGuasta{Buste: b, lettura: true} })
	b := titolareDentro(t, h)
	if r := b.vai("GET", rottaNegozio, nil, nil); r.codice != 500 {
		t.Errorf("lettura guasta: %d", r.codice)
	}
	contiene(t, "registro", reg.String(), "stima dei tempi non letta")
	reg.Reset()
	h, d := facciaConBusteCon(t, &reg, func(b Buste) Buste { return stimaGuasta{Buste: b, scrittura: true} })
	b = titolareDentro(t, h)
	b.vai("GET", rottaNegozio, nil, nil)
	r := b.vai("POST", rottaStima, moduloStimaWeb(b, "enabled", "true", "artisan_min", "61", "artisan_max", "73"), nil)
	if r.codice != 200 {
		t.Fatalf("scrittura guasta: %d", r.codice)
	}
	contiene(t, "scrittura", r.corpo, `<div role="alert" class="flash flash-errore">Stima tempi non salvata: riprova.</div>`)
	contiene(t, "valori salvati", r.corpo, campiStima(false, "7", "8")...)
	contiene(t, "registro", reg.String(), "stima dei tempi non salvata")
	nonContiene(t, "registro", reg.String(), "=61", "=73", "artisan", "estimate")
	if chiaviStima(t, d) != "∅|∅|∅" || righeAuditStima(t, d) != 0 {
		t.Fatal("il guasto ha scritto")
	}
}

// inventario: ADM-04
// Con poche buste «ora N» conta quelle di bottega e «passate dall'artigiano» le altre (la scheda dice «ora N» uguale).
func TestStimaNegozioPocheBuste(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := titolareDentro(t, h)
	pronteProva(t, d, o, 3, 6, false)
	pronteProva(t, d, o, 2, 9, true)
	contiene(t, "poche", b.vai("GET", rottaNegozio, nil, nil).corpo,
		`<p id="estimate-today" class="blocco-piccolo">Dati insufficienti: servono almeno 20 buste pronte (ora 3) — 3 buste in bottega, 2 passate dall'artigiano; `+
			`supplemento artigiano 7-8 giorni (dalle impostazioni).</p>`)
}

// inventario: ADM-04, RIP-H-55
// «Oggi» legge il modello della stima: con venti buste di bottega pronte «Pronta di solito in 9-20 giorni dal ritiro»
// e «20 buste in bottega, 0 passate dall'artigiano», supplemento «dalle impostazioni»; da dieci buste passate
// dall'artigiano il supplemento è «misurato sui dati». Sono le cifre che la scheda e la pagina del cliente leggono.
func TestStimaNegozioOggi(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := titolareDentro(t, h)
	ventiProntePerLaScheda(t, d, o)
	contiene(t, "venti", b.vai("GET", rottaNegozio, nil, nil).corpo,
		`<p id="estimate-today" class="blocco-piccolo">Oggi: Pronta di solito in 9-20 giorni dal ritiro — 20 buste in bottega, 0 passate dall'artigiano; `+
			`supplemento artigiano 7-8 giorni (dalle impostazioni).</p>`)
	pronteProva(t, d, o, 10, 30, true)
	contiene(t, "dieci", b.vai("GET", rottaNegozio, nil, nil).corpo,
		`Oggi: Pronta di solito in 9-20 giorni dal ritiro — 20 buste in bottega, 10 passate dall'artigiano; supplemento artigiano 21-11 giorni (misurato sui dati).</p>`)
}

// inventario: ADM-04, RIP-H-55
// L'interruttore salvato dalla pagina è quello che la scheda legge: spenta (la partenza) la scheda dice «stima spenta
// nelle impostazioni»; accesa dalla pagina la nota sparisce; rispenta, torna. La stima resta quella del negozio in tutti
// e due i casi.
func TestStimaNegozioInterruttoreSullaScheda(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.CodiceQR, a.DataPrevista = "", "" })
	ventiProntePerLaScheda(t, d, o)
	b := titolareDentro(t, h)
	contiene(t, "spenta", schedaStima(t, b, id), testoSpenta)
	b.vai("GET", rottaNegozio, nil, nil)
	if !redirectA(b.vai("POST", rottaStima, moduloStimaWeb(b, "enabled", "true", "artisan_min", "7", "artisan_max", "8"), nil), rottaNegozio) {
		t.Fatal("salvataggio")
	}
	corpo := schedaStima(t, b, id)
	contiene(t, "accesa", corpo, "Pronta di solito in 9-20 giorni dal ritiro · su 20 buste")
	nonContiene(t, "accesa", corpo, testoSpenta)
	if !redirectA(b.vai("POST", rottaStima, moduloStimaWeb(b, "enabled", "false", "artisan_min", "7", "artisan_max", "8"), nil), rottaNegozio) {
		t.Fatal("spegnimento")
	}
	contiene(t, "rispenta", schedaStima(t, b, id), testoSpenta)
}

// inventario: ADM-04
// La stima non cambia il profilo del negozio e viceversa: salvare il profilo non tocca le chiavi della stima, e salvare la
// stima lascia il profilo salvato com'è (compresa la riga d'audit del profilo, una sola).
func TestStimaNegozioIndipendenteDalProfilo(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	b.vai("GET", rottaNegozio, nil, nil)
	b.vai("POST", rottaStima, moduloStimaWeb(b, "enabled", "true", "artisan_min", "15", "artisan_max", "16"), nil)
	if !redirectA(b.vai("POST", rottaNegozio, moduloNegozioBase(b, "city", "Paese Finto"), nil), rottaNegozio) {
		t.Fatal("salvataggio del profilo")
	}
	if got := chiaviStima(t, d); got != "1|15|16" || righeAuditStima(t, d) != 1 {
		t.Fatalf("il profilo ha toccato la stima: %s, righe %d", got, righeAuditStima(t, d))
	}
	contiene(t, "dopo", b.vai("GET", rottaNegozio, nil, nil).corpo, campiStima(true, "15", "16")...)
	b.vai("POST", rottaStima, moduloStimaWeb(b, "enabled", "false", "artisan_min", "15", "artisan_max", "16"), nil)
	if campoDelProfilo(t, d, "citta") != "Paese Finto" || contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'shop_profile.updated'`) != 1 {
		t.Fatal("la stima ha cambiato il profilo")
	}
}
