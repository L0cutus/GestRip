package web

import (
	"context"
	"encoding/base64"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/media"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// La firma di consegna (RIP-S-36…40): le quattro rotte di DeliverySignatureController e la sezione
// #repair-signature della scheda, come delivery_signature_controller_test.exs e repair_live_test.exs
// («the delivery signature on the detail») dello standalone a go-baseline.

// firmaPNG è il PNG di 1×1 dei test dello standalone.
const firmaPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGNgYGBgAAAABQABh6FO1AAAAABJRU5ErkJggg=="

func firmaByte(t *testing.T) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(firmaPNG)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type provaFirmaWeb struct {
	d       *db.DB
	o       *clock.Finto
	b       *buste.Buste
	a       *media.Archivio
	handler func() *browser
	busta   int64
}

// facciaConFirma è la faccia interna con le buste vere e l'archivio dei media, e una busta pronta.
func facciaConFirma(t *testing.T) *provaFirmaWeb {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Apri(filepath.Join(dir, "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	inserisciConti(t, d, contiBase)
	p := &provaFirmaWeb{d: d, o: o}
	p.a = media.Nuovo(media.CartellaDi(filepath.Join(dir, "gestrip.db")))
	p.b = buste.Nuove(d, o).ConFoto(p.a, normalizzaQui{})
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti, dd.Buste = o, ratelimit.Nuovo(o), p.b
	h := mustInterna(t, dd)
	p.handler = func() *browser { return nuovoBrowser(t, h) }
	p.busta = p.pronta(t)
	return p
}

func (p *provaFirmaWeb) pronta(t *testing.T) int64 {
	t.Helper()
	id := bustaScheda(t, p.d, p.o, nil)
	if _, err := p.b.CambiaStato(context.Background(), id, "pronta", audit.Contesto{Attore: "titolare@esempio.it"}); err != nil {
		t.Fatal(err)
	}
	return id
}

func (p *provaFirmaWeb) titolare(t *testing.T) *browser {
	t.Helper()
	b := p.handler()
	if r := b.prova("titolare@esempio.it", pwProva); !redirectA(r, "/") {
		t.Fatalf("accesso del titolare: %d", r.codice)
	}
	return b
}

func (p *provaFirmaWeb) dipendente(t *testing.T) *browser {
	t.Helper()
	b := p.handler()
	if r := b.prova("banco@esempio.it", pwProva); !redirectA(r, "/") {
		t.Fatalf("accesso del dipendente: %d", r.codice)
	}
	return b
}

func (p *provaFirmaWeb) firma(t *testing.T, id int64, ritirata string) {
	t.Helper()
	if err := p.b.CatturaFirma(context.Background(), strconv.FormatInt(id, 10), "data:image/png;base64,"+firmaPNG, ritirata,
		audit.Contesto{Attore: "titolare@esempio.it"}); err != nil {
		t.Fatal(err)
	}
}

func percorsoFirma(id int64) string { return percorsoScheda(id) + "/firma" }

// postFirma manda il modulo della pagina di firma, con il token della pagina aperta prima.
func postFirma(b *browser, id int64, png, ritirata string) risposta {
	b.vai("GET", percorsoFirma(id), nil, nil)
	f := url.Values{"_csrf_token": {b.token}, "ritirato_da": {ritirata}}
	if png != "-" {
		f.Set("firma_png", png)
	}
	return b.vai("POST", percorsoFirma(id), f, nil)
}

// inventario: RIP-S-37
// La pagina di firma per titolare e dipendente: titolo, cliente e descrizione, la dichiarazione, il
// riquadro col canvas, «Ritirato da» (al più 120), il campo nascosto, «Conferma firma», «Pulisci» e
// «Annulla» verso la scheda, il guscio senza «🏠 Lista». Aprirla non scrive niente.
func TestPaginaFirma(t *testing.T) {
	p := facciaConFirma(t)
	sid := strconv.FormatInt(p.busta, 10)
	for _, b := range []*browser{p.titolare(t), p.dipendente(t)} {
		prima := contaCon(t, p.d, `SELECT count(*) FROM eventi_audit`)
		r := b.vai("GET", percorsoFirma(p.busta), nil, nil)
		if r.codice != 200 || !unaVolta(r, "Content-Type", "text/html; charset=utf-8") {
			t.Fatalf("%d %q", r.codice, r.location)
		}
		contiene(t, "pagina", r.corpo,
			"<title>Firma busta #1/26 · Laboratorio</title>",
			`<h1 class="titolo-firma">✍️ Consegna busta #1/26</h1>`,
			`<p id="signature-bag" class="sotto">Cliente storico — Anello`+"\n"+`Saldatura</p>`,
			`<p id="signature-declaration" class="forte">Dichiaro di aver ritirato l&#39;oggetto sopra indicato e di accettarne lo stato.</p>`,
			`<div id="signature-pad-frame" style="height:45vh; min-height:14rem;"`,
			`<canvas id="signature-pad" style="width:100%; height:100%; touch-action:none;"></canvas>`,
			`<form id="signature-form" action="/repairs/`+sid+`/firma" method="post"`,
			`<input name="_csrf_token" type="hidden" value="`+b.token+`">`,
			`<span>Ritirato da (solo se diverso dall'intestatario)</span>`,
			`<input type="text" id="signature-collected-by" name="ritirato_da" maxlength="120"`,
			`<input type="hidden" name="firma_png" id="signature-png">`,
			`<button id="signature-save" type="submit"`, ">Conferma firma</button>",
			`<button id="signature-clear" type="button"`, ">Pulisci</button>",
			`<a href="/repairs/`+sid+`" style="min-height:48px; line-height:48px;" class="pulsante-secondario">Annulla</a>`,
			`<p id="signature-no-script" class="sotto">La firma si disegna col dito o col mouse sul riquadro bianco qui sopra.</p>`,
			`id="app-mobile-navigation"`)
		if strings.Contains(r.corpo, `id="fab-home"`) {
			t.Error("la pagina di firma ha «🏠 Lista»")
		}
		if n := contaCon(t, p.d, `SELECT count(*) FROM eventi_audit`); n != prima {
			t.Errorf("la pagina ha scritto %d righe", n-prima)
		}
	}
	// La scheda ha ancora «🏠 Lista»: SenzaPulsanti vale solo per la firma.
	if r := p.titolare(t).vai("GET", percorsoScheda(p.busta), nil, nil); !strings.Contains(r.corpo, `id="fab-home"`) {
		t.Error("scheda senza «🏠 Lista»")
	}
}

// inventario: RIP-S-37
// Busta già firmata → la scheda, senza messaggio; che non può essere consegnata → la scheda con il
// messaggio; che non c'è (anche nel cestino o con un id non valido) → la home con «Riparazione non
// trovata.»; senza accesso → /login.
func TestPaginaFirmaRifiuti(t *testing.T) {
	p := facciaConFirma(t)
	b := p.titolare(t)
	firmata := p.pronta(t)
	p.firma(t, firmata, "")
	r := b.vai("GET", percorsoFirma(firmata), nil, nil)
	if !redirectA(r, percorsoScheda(firmata)) {
		t.Fatalf("firmata: %d %q", r.codice, r.location)
	}
	if r := b.vai("GET", percorsoScheda(firmata), nil, nil); strings.Contains(r.corpo, `role="alert"`) {
		t.Errorf("firmata: messaggio %q", r.corpo)
	}
	lavoro := bustaScheda(t, p.d, p.o, nil)
	if r := b.vai("GET", percorsoFirma(lavoro), nil, nil); !redirectA(r, percorsoScheda(lavoro)) {
		t.Fatalf("in lavorazione: %d %q", r.codice, r.location)
	}
	contiene(t, "in lavorazione", b.vai("GET", percorsoScheda(lavoro), nil, nil).corpo,
		`<div role="alert" class="flash flash-errore">Questa busta non può essere consegnata da dove si trova: firma non disponibile.</div>`)
	cestino := p.pronta(t)
	eseguiSQL(t, p.d, `UPDATE buste SET cancellata_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, cestino)
	for _, x := range []string{"999", "0", "abc", strconv.FormatInt(cestino, 10)} {
		if r := b.vai("GET", "/repairs/"+x+"/firma", nil, nil); !redirectA(r, "/") {
			t.Errorf("%s: %d %q", x, r.codice, r.location)
		}
		contiene(t, x, b.vai("GET", "/", nil, nil).corpo, `<div role="alert" class="flash flash-errore">Riparazione non trovata.</div>`)
	}
	if r := p.handler().vai("GET", percorsoFirma(p.busta), nil, nil); !redirectA(r, "/login") {
		t.Errorf("senza accesso: %d %q", r.codice, r.location)
	}
	// Il redirect tiene l'id com'era nel percorso, scritto come ~p.
	if r := b.vai("GET", "/repairs/0"+strconv.FormatInt(firmata, 10)+"/firma", nil, nil); !redirectA(r, "/repairs/0"+strconv.FormatInt(firmata, 10)) {
		t.Errorf("con lo zero davanti: %q", r.location)
	}
	if r := b.vai("GET", "/repairs/%2B"+strconv.FormatInt(firmata, 10)+"/firma", nil, nil); !redirectA(r, "/repairs/%2B"+strconv.FormatInt(firmata, 10)) {
		t.Errorf("con il segno: %q", r.location)
	}
}

// inventario: RIP-S-38
// Il salvataggio è la consegna: la scheda con «Firma registrata: riparazione consegnata.», lo stato
// «Consegnata», il timbro con chi ha ritirato e l'immagine; la busta nel database consegnata con la
// firma. Lo fa anche il dipendente. Una seconda conferma è rifiutata e la prima resta.
func TestSalvaFirma(t *testing.T) {
	p := facciaConFirma(t)
	for i, b := range []*browser{p.titolare(t), p.dipendente(t)} {
		id := p.busta
		if i == 1 {
			id = p.pronta(t)
		}
		sid := strconv.FormatInt(id, 10)
		r := postFirma(b, id, "data:image/png;base64,"+firmaPNG, "  Mario Rossi  ")
		if !redirectA(r, percorsoScheda(id)) {
			t.Fatalf("%d %q %q", r.codice, r.location, r.corpo)
		}
		r = b.vai("GET", percorsoScheda(id), nil, nil)
		contiene(t, "scheda", r.corpo,
			`<div role="status" class="flash flash-info">Firma registrata: riparazione consegnata.</div>`,
			`<section id="repair-signature" class="riquadro riquadro-firma">`, "<h2>Firma di consegna</h2>",
			`<p id="repair-signature-stamp" class="sotto">Firmata il 31/10/2026 10:00 — ritirata da Mario Rossi</p>`,
			`<img id="repair-signature-image" src="/repairs/`+sid+`/firma.png" alt="Firma del cliente" class="immagine-firma">`)
		if !strings.Contains(r.corpo, ">Consegnata<") {
			t.Error("stato non consegnata")
		}
		if strings.Contains(r.corpo, `id="repair-signature-link"`) {
			t.Error("firmata con il link alla pagina di firma")
		}
		if v := contaCon(t, p.d, `SELECT count(*) FROM buste WHERE id = ? AND stato = 'consegnata' AND firma_ritirata_da = 'Mario Rossi'
			AND firma_byte = ?`, id, len(firmaByte(t))); v != 1 {
			t.Errorf("busta: %d", v)
		}
		r = postFirma(b, id, "data:image/png;base64,"+firmaPNG, "Altro")
		if !redirectA(r, percorsoScheda(id)) {
			t.Fatalf("seconda: %d %q", r.codice, r.location)
		}
		contiene(t, "seconda", b.vai("GET", percorsoScheda(id), nil, nil).corpo,
			`<div role="alert" class="flash flash-errore">La firma è già stata registrata su questa busta.</div>`)
		if v := contaCon(t, p.d, `SELECT count(*) FROM buste WHERE id = ? AND firma_ritirata_da = 'Mario Rossi'`, id); v != 1 {
			t.Error("la seconda firma ha cambiato la prima")
		}
	}
}

// inventario: RIP-S-38
// Ogni cosa che non è il data URL del riquadro (vuoto, non un PNG, base64 senza prefisso, campo
// assente, oltre 500 KiB) torna alla pagina di firma con «Firma mancante o non valida.» e non scrive
// niente; una busta che non può essere consegnata torna alla scheda con il suo messaggio; una che non
// c'è alla home; senza accesso a /login; senza token il 403.
func TestSalvaFirmaRifiuti(t *testing.T) {
	p := facciaConFirma(t)
	b := p.titolare(t)
	grande := "data:image/png;base64," + base64.StdEncoding.EncodeToString(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 500*1024)...))
	for _, v := range []string{"", "data:image/png;base64,bm9uLXBuZw==", firmaPNG, "-", grande} {
		r := postFirma(b, p.busta, v, "")
		if !redirectA(r, percorsoFirma(p.busta)) {
			t.Fatalf("%.30q: %d %q", v, r.codice, r.location)
		}
		contiene(t, "rifiuto", b.vai("GET", percorsoFirma(p.busta), nil, nil).corpo,
			`<div role="alert" class="flash flash-errore">Firma mancante o non valida.</div>`)
	}
	if n := contaCon(t, p.d, `SELECT count(*) FROM buste WHERE firma_chiave IS NOT NULL OR stato = 'consegnata'`); n != 0 {
		t.Errorf("scritte %d buste", n)
	}
	lavoro := bustaScheda(t, p.d, p.o, nil)
	b.vai("GET", percorsoScheda(lavoro), nil, nil)
	r := b.vai("POST", percorsoFirma(lavoro), url.Values{"_csrf_token": {b.token}, "firma_png": {"data:image/png;base64," + firmaPNG}}, nil)
	if !redirectA(r, percorsoScheda(lavoro)) {
		t.Fatalf("in lavorazione: %d %q", r.codice, r.location)
	}
	contiene(t, "in lavorazione", b.vai("GET", percorsoScheda(lavoro), nil, nil).corpo,
		"Questa busta non può essere consegnata da dove si trova: firma non disponibile.")
	for _, x := range []string{"999", "abc"} {
		r := b.vai("POST", "/repairs/"+x+"/firma", url.Values{"_csrf_token": {b.token}, "firma_png": {"data:image/png;base64," + firmaPNG}}, nil)
		if !redirectA(r, "/") {
			t.Errorf("%s: %d %q", x, r.codice, r.location)
		}
	}
	fuori := p.handler()
	fuori.vai("GET", "/login", nil, nil)
	if r := fuori.vai("POST", percorsoFirma(p.busta), url.Values{"_csrf_token": {fuori.token}, "firma_png": {"data:image/png;base64," + firmaPNG}}, nil); !redirectA(r, "/login") {
		t.Errorf("senza accesso: %d %q", r.codice, r.location)
	}
	if r := b.vai("POST", percorsoFirma(p.busta), url.Values{"firma_png": {"data:image/png;base64," + firmaPNG}}, nil); r.codice != 403 {
		t.Errorf("senza token: %d", r.codice)
	}
	if n := contaCon(t, p.d, `SELECT count(*) FROM buste WHERE firma_chiave IS NOT NULL`); n != 0 {
		t.Errorf("scritte %d firme", n)
	}
}

// inventario: RIP-S-39
// firma.png: i byte originali, image/png senza charset, no-store e no-referrer; una busta non
// firmata, una che non c'è e un id che non è un numero danno la stessa risposta: 404 «Firma non
// disponibile», senza tipo, con no-store e no-referrer. Senza accesso: /login.
func TestImmagineFirma(t *testing.T) {
	p := facciaConFirma(t)
	firmata := p.pronta(t)
	p.firma(t, firmata, "")
	for _, b := range []*browser{p.titolare(t), p.dipendente(t)} {
		r := b.vai("GET", percorsoFirma(firmata)+".png", nil, nil)
		if r.codice != 200 || r.corpo != string(firmaByte(t)) || !unaVolta(r, "Content-Type", "image/png") ||
			!unaVolta(r, "Cache-Control", "no-store") || !unaVolta(r, "Referrer-Policy", "no-referrer") ||
			!unaVolta(r, "X-Content-Type-Options", "nosniff") || r.h.Get("Content-Length") != strconv.Itoa(len(r.corpo)) {
			t.Fatalf("%d %v", r.codice, r.h)
		}
		var rifiuti []risposta
		for _, x := range []string{strconv.FormatInt(p.busta, 10), "999999", "not-a-number"} {
			rifiuti = append(rifiuti, b.vai("GET", "/repairs/"+x+"/firma.png", nil, nil))
		}
		for i, r := range rifiuti {
			if r.codice != 404 || r.corpo != "Firma non disponibile" || len(r.h.Values("Content-Type")) != 0 ||
				!unaVolta(r, "Cache-Control", "no-store") || !unaVolta(r, "Referrer-Policy", "no-referrer") {
				t.Errorf("%d: %d %q %v", i, r.codice, r.corpo, r.h)
			}
		}
	}
	if n := contaCon(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.signature_viewed' AND esito = 'ok'`); n != 2 {
		t.Errorf("ok: %d", n)
	}
	if n := contaCon(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.signature_viewed' AND esito = 'denied'`); n != 2 {
		t.Errorf("denied: %d (solo la busta non firmata, due volte)", n)
	}
	if r := p.handler().vai("GET", percorsoFirma(firmata)+".png", nil, nil); !redirectA(r, "/login") {
		t.Errorf("senza accesso: %d %q", r.codice, r.location)
	}
}

// inventario: RIP-S-39
// Un file della firma cambiato sul disco (impronta diversa) non si serve: il 404 di sempre.
func TestImmagineFirmaAlterata(t *testing.T) {
	p := facciaConFirma(t)
	p.firma(t, p.busta, "")
	k := ""
	if err := p.d.Lettura.QueryRow(`SELECT firma_chiave FROM buste WHERE id = ?`, p.busta).Scan(&k); err != nil {
		t.Fatal(err)
	}
	scriviSopra(t, filepath.Join(p.a.Cartella(), k), append(firmaByte(t), 0))
	r := p.titolare(t).vai("GET", percorsoFirma(p.busta)+".png", nil, nil)
	if r.codice != 404 || r.corpo != "Firma non disponibile" {
		t.Errorf("%d %q", r.codice, r.corpo)
	}
}

// scriviSopra cambia i byte di un file dell'archivio (0400), come un disco guasto o una manomissione.
func scriviSopra(t *testing.T, file string, b []byte) {
	t.Helper()
	if err := os.Chmod(file, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// inventario: RIP-S-40
// «↺ Rifai firma» del titolare: la pagina di firma con «Firma azzerata: puoi rifirmare.», la consegna
// resta; il dipendente va a / con «Operazione riservata al titolare.» e la firma resta; senza firma:
// la scheda con «Su questa busta non c'è nessuna firma da azzerare.».
func TestAzzeraFirmaWeb(t *testing.T) {
	p := facciaConFirma(t)
	p.firma(t, p.busta, "Ada")
	dip := p.dipendente(t)
	dip.vai("GET", percorsoScheda(p.busta), nil, nil)
	if r := dip.vai("POST", percorsoFirma(p.busta)+"/reset", url.Values{"_csrf_token": {dip.token}}, nil); !redirectA(r, "/") {
		t.Fatalf("dipendente: %d %q", r.codice, r.location)
	}
	contiene(t, "dipendente", dip.vai("GET", "/", nil, nil).corpo, `<div role="alert" class="flash flash-errore">Operazione riservata al titolare.</div>`)
	if n := contaCon(t, p.d, `SELECT count(*) FROM buste WHERE firma_chiave IS NOT NULL`); n != 1 {
		t.Fatal("il dipendente ha azzerato la firma")
	}
	b := p.titolare(t)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	r := b.vai("POST", percorsoFirma(p.busta)+"/reset", url.Values{"_csrf_token": {b.token}}, nil)
	if !redirectA(r, percorsoFirma(p.busta)) {
		t.Fatalf("titolare: %d %q", r.codice, r.location)
	}
	r = b.vai("GET", percorsoFirma(p.busta), nil, nil)
	if r.codice != 200 {
		t.Fatalf("pagina di firma dopo l'azzeramento: %d %q", r.codice, r.location)
	}
	contiene(t, "azzerata", r.corpo, `<div role="status" class="flash flash-info">Firma azzerata: puoi rifirmare.</div>`)
	if n := contaCon(t, p.d, `SELECT count(*) FROM buste WHERE id = ? AND firma_chiave IS NULL AND stato = 'consegnata'`, p.busta); n != 1 {
		t.Error("azzeramento")
	}
	r = b.vai("POST", percorsoFirma(p.busta)+"/reset", url.Values{"_csrf_token": {b.token}}, nil)
	if !redirectA(r, percorsoScheda(p.busta)) {
		t.Fatalf("senza firma: %d %q", r.codice, r.location)
	}
	contiene(t, "senza firma", b.vai("GET", percorsoScheda(p.busta), nil, nil).corpo,
		`<div role="alert" class="flash flash-errore">Su questa busta non c&#39;è nessuna firma da azzerare.</div>`)
	if r := b.vai("POST", "/repairs/999/firma/reset", url.Values{"_csrf_token": {b.token}}, nil); !redirectA(r, "/") {
		t.Errorf("busta che non c'è: %d %q", r.codice, r.location)
	}
}

// inventario: RIP-S-36
// La sezione «Firma di consegna» della scheda: una busta pronta offre «✍️ Consegna con firma», una
// consegnata «✍️ Aggiungi firma», una in lavorazione nessun pulsante; una firmata il timbro,
// l'immagine e, solo al titolare, «↺ Rifai firma» con la conferma.
func TestSezioneFirmaDellaScheda(t *testing.T) {
	p := facciaConFirma(t)
	b := p.titolare(t)
	sezione := func(t *testing.T, b *browser, id int64) string {
		t.Helper()
		r := b.vai("GET", percorsoScheda(id), nil, nil)
		i := strings.Index(r.corpo, `<section id="repair-signature"`)
		if i < 0 {
			t.Fatalf("%d: nessuna sezione", id)
		}
		j := strings.Index(r.corpo[i:], "</section>")
		// Dopo le foto e prima del link pubblico, come nello standalone.
		if f, l := strings.Index(r.corpo, `id="repair-photos"`), strings.Index(r.corpo, `id="repair-public-link"`); f > i || (l >= 0 && l < i) {
			t.Errorf("ordine delle sezioni: foto %d firma %d link %d", f, i, l)
		}
		return r.corpo[i : i+j]
	}
	sid := strconv.FormatInt(p.busta, 10)
	s := sezione(t, b, p.busta)
	contiene(t, "pronta", s, `<p class="sotto">Nessuna firma registrata su questa busta.</p>`,
		`<a id="repair-signature-link" href="/repairs/`+sid+`/firma" class="pulsante-contorno">✍️ Consegna con firma</a>`)
	consegnata := p.pronta(t)
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, consegnata)
	contiene(t, "consegnata", sezione(t, b, consegnata), `>✍️ Aggiungi firma</a>`)
	oro := bustaScheda(t, p.d, p.o, func(a *buste.Accettazione) { a.CompraOro = "1" })
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'chiusa' WHERE id = ?`, oro)
	contiene(t, "chiusa", sezione(t, b, oro), `>✍️ Aggiungi firma</a>`)
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'perfezionata' WHERE id = ?`, oro)
	contiene(t, "perfezionata", sezione(t, b, oro), `>✍️ Consegna con firma</a>`)
	lavoro := bustaScheda(t, p.d, p.o, nil)
	if s := sezione(t, b, lavoro); strings.Contains(s, "repair-signature-link") || !strings.Contains(s, "Nessuna firma registrata su questa busta.") {
		t.Errorf("in lavorazione: %s", s)
	}
	p.firma(t, p.busta, "")
	s = sezione(t, b, p.busta)
	contiene(t, "firmata", s, `<p id="repair-signature-stamp" class="sotto">Firmata il 31/10/2026 10:00</p>`,
		`<img id="repair-signature-image" src="/repairs/`+sid+`/firma.png"`,
		`<form action="/repairs/`+sid+`/firma/reset" method="post" data-confirm="Cancellare la firma e rifarla? La consegna resta registrata.">`,
		`<input name="_csrf_token" type="hidden" value="`+b.token+`">`,
		`<button id="repair-signature-reset" type="submit" class="pulsante-rifai-firma">↺ Rifai firma</button>`)
	if strings.Contains(s, "Nessuna firma") || strings.Contains(s, "repair-signature-link") {
		t.Errorf("firmata: %s", s)
	}
	if s := sezione(t, p.dipendente(t), p.busta); strings.Contains(s, "repair-signature-reset") || !strings.Contains(s, "repair-signature-image") {
		t.Errorf("dipendente: %s", s)
	}
	// Firma importata senza istante: «(data non registrata)».
	eseguiSQL(t, p.d, `UPDATE buste SET firma_il = NULL, firma_ritirata_da = 'Cliente <3>' WHERE id = ?`, p.busta)
	contiene(t, "importata", sezione(t, b, p.busta), `>Firmata (data non registrata) — ritirata da Cliente &lt;3&gt;</p>`)
}

// inventario: RIP-S-39
// Nessuna pagina pubblica ha una porta verso la firma: la faccia pubblica non ha rotte della firma.
func TestFirmaSoloInterna(t *testing.T) {
	_, f, err := costruisciPubblica(dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, &utentiFinti{attivo: true}), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range f.rotte {
		if strings.Contains(r.pattern, "firma") {
			t.Errorf("rotta pubblica %s", r.pattern)
		}
	}
}
