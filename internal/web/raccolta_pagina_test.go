package web

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/assist"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// La raccolta dei dettati nella pagina «Assistente e dettatura» (slice 114; RIP-H-49, RIP-H-50, ADM-12), come la sezione
// #training di AssistLive e training_collection_live_test.exs («the owner's switch», «slice 184 on the pages») dello
// standalone a go-baseline, senza LiveView: ogni evento è un POST con CSRF che torna alla pagina con il messaggio. Dettati,
// nomi e telefoni inventati.

const (
	percorsoAccendi        = "/settings/assistente/raccolta/accendi"
	percorsoSpegni         = "/settings/assistente/raccolta/spegni"
	percorsoNuovoLotto     = "/settings/assistente/raccolta/lotto"
	percorsoEscludi        = "/settings/assistente/raccolta/escludi"
	percorsoEscludiCliente = "/settings/assistente/raccolta/escludi-cliente"
	dettatoPagina          = "Cliente Ofelia Pagina 333 000 0114, collana da saldare, preventivo 30"
)

type provaRaccolta struct {
	h http.Handler
	d *db.DB
	b *buste.Buste
}

// facciaConRaccolta è la faccia interna con le buste e l'assistente veri sul database; reg riceve il registro; avvolgi,
// se c'è, sta davanti alle buste della faccia (i guasti).
func facciaConRaccolta(t *testing.T, reg *bytes.Buffer, avvolgi func(Buste) Buste) provaRaccolta {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC))
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	inserisciConti(t, d, contiBase)
	l := nessunRegistro
	if reg != nil {
		l = slog.New(slog.NewTextHandler(reg, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	dd := dipendenzeProva(&sondaFinta{pronto: true}, l, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti = o, ratelimit.Nuovo(o)
	b := buste.Nuove(d, o)
	dd.Buste = b
	if avvolgi != nil {
		dd.Buste = avvolgi(b)
	}
	dd.Assistente = assist.NuoveImpostazioni(d, o)
	return provaRaccolta{h: mustInterna(t, dd), d: d, b: b}
}

// busta salva una busta del cliente nome; con dettato la cattura di un'estrazione (raccolta se è accesa).
func (p provaRaccolta) busta(t *testing.T, nome, qr string, dettato bool) buste.Creata {
	t.Helper()
	telefono := "333 000 0114"
	if nome != "Ofelia Pagina" {
		telefono = "333 000 0999"
	}
	a := buste.Accettazione{NomeCliente: nome, TelefonoCliente: telefono, DataAccettazione: "2026-11-20", Descrizione: "Collana da saldare",
		CodiceQR: qr}
	a.GemelleConfermate = qr
	if dettato {
		a.Dettato = &buste.Dettato{Testo: dettatoPagina, Proposta: []byte(`{"nome":"Ofelia Pagina"}`), Modello: "modello-di-prova-v1"}
	}
	c, err := p.b.Crea(context.Background(), a, audit.Contesto{Attore: "titolare@esempio.it"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (p provaRaccolta) impostazione(t *testing.T, chiave string) string {
	t.Helper()
	var v string
	if err := p.d.Lettura.QueryRow(`SELECT coalesce((SELECT valore FROM impostazioni WHERE chiave = ?), '(assente)')`, chiave).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func moduloRaccolta(b *browser, kv ...string) url.Values {
	v := url.Values{"_csrf_token": {b.token}}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return v
}

// azione manda il POST, vuole il ritorno alla pagina e dà la pagina dopo, con il suo messaggio.
func azioneRaccolta(t *testing.T, b *browser, percorso string, kv ...string) string {
	t.Helper()
	r := b.vai("POST", percorso, moduloRaccolta(b, kv...), nil)
	if !redirectA(r, paginaAssistentePercorso) {
		t.Fatalf("%s: %d %q", percorso, r.codice, r.location)
	}
	return b.vai("GET", paginaAssistentePercorso, nil, nil).corpo
}

var reSezioneRaccolta = regexp.MustCompile(`(?s)<section id="training".*?</section>`)

func sezioneRaccolta(t *testing.T, corpo string) string {
	t.Helper()
	s := reSezioneRaccolta.FindString(corpo)
	if s == "" {
		t.Fatal("sezione #training assente")
	}
	return s
}

func conteggio(rimaste, raccolti, esclusi string) string {
	return `<p id="training-remaining">Restano <strong>` + rimaste + `</strong> buste su 500 · dettati raccolti: ` + raccolti +
		` · clienti esclusi: ` + esclusi + `</p>`
}

// inventario: ADM-12
// La sezione «Dettati per l'addestramento»: spenta all'installazione con il lotto intero, il testo dello standalone e le tre
// frasi della D-07, il conteggio (soli numeri), «Accendi la raccolta», i due moduli di esclusione con il CSRF e la conferma
// sul cliente; niente nuovo lotto finché il lotto ha buste. Una busta raccolta fa salire il conteggio, e il suo dettato non
// compare mai.
func TestAssistenteRaccoltaPagina(t *testing.T) {
	p := facciaConRaccolta(t, nil, nil)
	b := titolareDentro(t, p.h)
	s := sezioneRaccolta(t, b.vai("GET", paginaAssistentePercorso, nil, nil).corpo)
	contiene(t, "sezione", s,
		`<h2>Dettati per l'addestramento</h2> <span id="training-state" class="badge">Spenta</span>`,
		template.HTMLEscapeString(testoRaccolta), `<p id="training-privacy" class="sotto">`+template.HTMLEscapeString(testoRaccoltaD07)+`</p>`,
		conteggio("500", "0", "0"),
		`<button id="training-on" type="submit">Accendi la raccolta</button>`,
		`<form id="training-exclude" action="/settings/assistente/raccolta/escludi" method="post"`,
		`<label for="exclude_bag_number">Escludi una busta dalla raccolta (numero della busta)</label> <input type="text" id="exclude_bag_number" name="exclude[bag_number]" placeholder="118/26">`,
		`<button type="submit" class="pulsante-contorno">Escludi</button>`,
		`<form id="training-exclude-customer" action="/settings/assistente/raccolta/escludi-cliente" method="post"`,
		`<label for="exclude_customer_bag_number">Escludi un cliente (opposizione): numero di una sua busta</label> <input type="text" id="exclude_customer_bag_number" name="exclude_customer[bag_number]" placeholder="118/26">`,
		`<input name="conferma" type="hidden" value="" data-conferma-js>`,
		`data-confirm="Cancellare i dettati di tutte le buste di questo cliente ed escludere le sue buste future?">Escludi il cliente</button>`)
	nonContiene(t, "sezione", s, "training-off", "training-refill", "Budget esaurito", "training-confirm")
	for _, id := range []string{"training-on-form", "training-exclude", "training-exclude-customer"} {
		if !regexp.MustCompile(`(?s)<form id="` + id + `"[^>]*>\s*<input name="_csrf_token" type="hidden" value="[^"]+">`).MatchString(s) {
			t.Errorf("%s senza _csrf_token", id)
		}
	}
	eseguiSQL(t, p.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.training.collect', '1', '2026-11-20T09:00:00.000000Z'),
		('assist.training.remaining', '7', '2026-11-20T09:00:00.000000Z')`)
	p.busta(t, "Ofelia Pagina", "QR-114-P", true)
	s = sezioneRaccolta(t, b.vai("GET", paginaAssistentePercorso, nil, nil).corpo)
	contiene(t, "dopo una busta", s, `<span id="training-state" class="badge badge-ok">Accesa</span>`, conteggio("6", "1", "0"),
		`<button id="training-off" type="submit" class="pulsante-contorno">Spegni la raccolta</button>`)
	nonContiene(t, "dopo una busta", b.vai("GET", paginaAssistentePercorso, nil, nil).corpo, "Ofelia", "saldare", "0114", "modello-di-prova", "training-on\"")
}

// inventario: RIP-H-49
// L'interruttore dalla pagina (training_on, training_off): accesa e spenta, la prima accensione scrive il lotto; a lotto
// esaurito niente «Accendi», «Budget esaurito: la raccolta è finita.» e il nuovo lotto con la conferma, e un'accensione
// mandata lo stesso è rifiutata con il messaggio dello standalone. Senza CSRF niente.
func TestAssistenteRaccoltaInterruttore(t *testing.T) {
	p := facciaConRaccolta(t, nil, nil)
	b := titolareNellAssistente(t, p.h)
	pagina := azioneRaccolta(t, b, percorsoAccendi)
	contiene(t, "accesa", sezioneRaccolta(t, pagina), `id="training-state" class="badge badge-ok">Accesa</span>`, `id="training-off"`, conteggio("500", "0", "0"))
	nonContiene(t, "accesa", pagina, `id="training-on"`, "flash-errore")
	if a, r := p.impostazione(t, buste.ChiaveRaccoltaAccesa), p.impostazione(t, buste.ChiaveRaccoltaRimaste); a != "1" || r != "500" {
		t.Errorf("accesa %s, rimaste %s", a, r)
	}
	pagina = azioneRaccolta(t, b, percorsoSpegni)
	contiene(t, "spenta", sezioneRaccolta(t, pagina), `id="training-state" class="badge">Spenta</span>`, `id="training-on"`)
	if a := p.impostazione(t, buste.ChiaveRaccoltaAccesa); a != "0" {
		t.Errorf("spenta: %s", a)
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.training_collect' AND attore = 'titolare@esempio.it'`); n != 2 {
		t.Errorf("audit %d", n)
	}
	// Senza CSRF: 403 e niente.
	if r := b.vai("POST", percorsoAccendi, url.Values{}, nil); r.codice != http.StatusForbidden {
		t.Errorf("senza CSRF: %d", r.codice)
	}
	if a := p.impostazione(t, buste.ChiaveRaccoltaAccesa); a != "0" {
		t.Errorf("senza CSRF accesa: %s", a)
	}
	// Lotto esaurito.
	eseguiSQL(t, p.d, `UPDATE impostazioni SET valore = '0' WHERE chiave = 'assist.training.remaining'`)
	s := sezioneRaccolta(t, b.vai("GET", paginaAssistentePercorso, nil, nil).corpo)
	contiene(t, "esaurito", s, conteggio("0", "0", "0"), `<p class="sotto">Budget esaurito: la raccolta è finita.</p>`,
		`<form id="training-refill-form" action="/settings/assistente/raccolta/lotto" method="post" data-invio-unico>`,
		`<button id="training-refill" type="submit" data-confirm="Avviare un nuovo lotto di 500 buste e riaccendere la raccolta?">Avvia un nuovo lotto di 500 buste</button>`)
	nonContiene(t, "esaurito", s, `id="training-on"`)
	pagina = azioneRaccolta(t, b, percorsoAccendi)
	contiene(t, "rifiuto", pagina, `<div role="alert" class="flash flash-errore">Budget esaurito: la raccolta non si riaccende.</div>`)
	if a := p.impostazione(t, buste.ChiaveRaccoltaAccesa); a != "0" {
		t.Errorf("accesa a lotto esaurito: %s", a)
	}
}

// inventario: RIP-H-49
// Il nuovo lotto (training_refill): rifiutato finché il lotto ha buste, anche prima del primo; a lotto finito, senza la
// conferma (senza JavaScript) la pagina chiede «Sì» o «Annulla» e non scrive niente; con la conferma 500 buste e la raccolta
// accesa, «Nuovo lotto avviato: la raccolta è accesa.».
func TestAssistenteRaccoltaNuovoLotto(t *testing.T) {
	p := facciaConRaccolta(t, nil, nil)
	b := titolareNellAssistente(t, p.h)
	pagina := azioneRaccolta(t, b, percorsoNuovoLotto, "conferma", "1")
	contiene(t, "prima del primo lotto", pagina, `<div role="alert" class="flash flash-errore">Il lotto in corso ha ancora buste: niente da ricaricare.</div>`)
	if r := p.impostazione(t, buste.ChiaveRaccoltaRimaste); r != "(assente)" {
		t.Errorf("rimaste %s", r)
	}
	eseguiSQL(t, p.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.training.remaining', '0', '2026-11-20T09:00:00.000000Z')`)
	b.vai("GET", paginaAssistentePercorso, nil, nil)
	r := b.vai("POST", percorsoNuovoLotto, moduloRaccolta(b), nil)
	if r.codice != http.StatusOK {
		t.Fatalf("conferma: %d", r.codice)
	}
	contiene(t, "conferma", sezioneRaccolta(t, r.corpo), `<div id="training-confirm" class="assistente-conferma">`,
		`<p id="training-confirm-text">Avviare un nuovo lotto di 500 buste e riaccendere la raccolta?</p>`,
		`<form action="/settings/assistente/raccolta/lotto" method="post">`,
		`<button id="training-confirm-yes" type="submit" name="conferma" value="1">Sì</button>`,
		`<a id="training-confirm-no" href="/settings/assistente" class="pulsante-contorno">Annulla</a>`)
	if r := p.impostazione(t, buste.ChiaveRaccoltaRimaste); r != "0" {
		t.Errorf("la conferma ha scritto: %s", r)
	}
	pagina = azioneRaccolta(t, b, percorsoNuovoLotto, "conferma", "1")
	contiene(t, "nuovo lotto", pagina, `<div role="status" class="flash flash-info">Nuovo lotto avviato: la raccolta è accesa.</div>`, conteggio("500", "0", "0"),
		`id="training-state" class="badge badge-ok">Accesa</span>`)
	nonContiene(t, "nuovo lotto", pagina, `id="training-refill"`)
	if a := p.impostazione(t, buste.ChiaveRaccoltaAccesa); a != "1" {
		t.Errorf("accesa %s", a)
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.training_refill' AND dati = '{"on":true,"remaining":500}'`); n != 1 {
		t.Errorf("audit %d", n)
	}
}

// inventario: RIP-H-50
// L'esclusione di una busta per numero (training_exclude): una che non c'è, una raccolta (con spazi e zeri davanti), di
// nuovo la stessa, una nel cestino e una senza dettato, con i messaggi dello standalone; la busta resta. Un modulo senza il
// campo non fa niente.
func TestAssistenteRaccoltaEscludiBusta(t *testing.T) {
	p := facciaConRaccolta(t, nil, nil)
	eseguiSQL(t, p.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.training.collect', '1', '2026-11-20T09:00:00.000000Z'),
		('assist.training.remaining', '500', '2026-11-20T09:00:00.000000Z')`)
	una := p.busta(t, "Ofelia Pagina", "QR-114-A", true)
	cestino := p.busta(t, "Ofelia Pagina", "QR-114-B", true)
	amano := p.busta(t, "Ofelia Pagina", "QR-114-C", false)
	eseguiSQL(t, p.d, `UPDATE buste SET cancellata_il = '2026-11-21T09:00:00.000000Z' WHERE id = ?`, cestino.ID)
	b := titolareNellAssistente(t, p.h)
	for _, c := range []struct{ numero, messaggio string }{
		{"9999/99", `<div role="alert" class="flash flash-errore">Nessuna busta con questo numero.</div>`},
		{" 0" + una.Numero + " ", `<div role="status" class="flash flash-info">Busta N. ` + una.Numero + ` esclusa: il suo dettato è stato cancellato.</div>`},
		{una.Numero, `<div role="alert" class="flash flash-errore">La busta N. ` + una.Numero + ` non ha un dettato raccolto.</div>`},
		{cestino.Numero, `<div role="status" class="flash flash-info">Busta N. ` + cestino.Numero + ` esclusa: il suo dettato è stato cancellato.</div>`},
		{amano.Numero, `<div role="alert" class="flash flash-errore">La busta N. ` + amano.Numero + ` non ha un dettato raccolto.</div>`},
	} {
		contiene(t, c.numero, azioneRaccolta(t, b, percorsoEscludi, "exclude[bag_number]", c.numero), c.messaggio)
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM dettati_addestramento`); n != 0 {
		t.Errorf("righe %d", n)
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM buste`); n != 3 {
		t.Errorf("buste %d", n)
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.training_excluded'`); n != 2 {
		t.Errorf("audit %d", n)
	}
	pagina := azioneRaccolta(t, b, percorsoEscludi, "altro", una.Numero)
	nonContiene(t, "senza campo", pagina, "flash-errore", "flash-info")
}

// inventario: RIP-H-50
// L'opposizione di un cliente (training_exclude_customer), dal numero di una sua busta: senza la conferma la pagina la chiede
// e non cancella; con la conferma «Cliente escluso: cancellati 2 dettati; …», il conteggio dei clienti esclusi sale, l'altro
// cliente resta; un numero che non c'è è «Nessuna busta con questo numero.».
func TestAssistenteRaccoltaEscludiCliente(t *testing.T) {
	p := facciaConRaccolta(t, nil, nil)
	eseguiSQL(t, p.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.training.collect', '1', '2026-11-20T09:00:00.000000Z'),
		('assist.training.remaining', '500', '2026-11-20T09:00:00.000000Z')`)
	una := p.busta(t, "Ofelia Pagina", "QR-114-D", true)
	p.busta(t, "Ofelia Pagina", "QR-114-E", true)
	p.busta(t, "Altra Inventata", "QR-114-F", true)
	b := titolareNellAssistente(t, p.h)
	r := b.vai("POST", percorsoEscludiCliente, moduloRaccolta(b, "exclude_customer[bag_number]", " "+una.Numero), nil)
	if r.codice != http.StatusOK {
		t.Fatalf("conferma: %d", r.codice)
	}
	contiene(t, "conferma", sezioneRaccolta(t, r.corpo),
		`<p id="training-confirm-text">Cancellare i dettati di tutte le buste di questo cliente ed escludere le sue buste future?</p>`,
		`<form action="/settings/assistente/raccolta/escludi-cliente" method="post">`,
		`<input name="exclude_customer[bag_number]" type="hidden" value=" `+una.Numero+`">`,
		`<button id="training-confirm-yes" type="submit" name="conferma" value="1">Sì</button>`)
	if n := contaRighe(t, p.d, `SELECT count(*) FROM dettati_addestramento`); n != 3 {
		t.Fatalf("la conferma ha cancellato: %d", n)
	}
	pagina := azioneRaccolta(t, b, percorsoEscludiCliente, "exclude_customer[bag_number]", " "+una.Numero, "conferma", "1")
	contiene(t, "esclusione", pagina,
		`<div role="status" class="flash flash-info">Cliente della busta N. `+una.Numero+` escluso: cancellati 2 dettati; le sue buste future non saranno raccolte.</div>`,
		conteggio("497", "1", "1"))
	nonContiene(t, "esclusione", pagina, "Ofelia", "Altra Inventata", "training-confirm")
	pagina = azioneRaccolta(t, b, percorsoEscludiCliente, "exclude_customer[bag_number]", "9999/99", "conferma", "1")
	contiene(t, "non trovata", pagina, `<div role="alert" class="flash flash-errore">Nessuna busta con questo numero.</div>`)
	if n := contaRighe(t, p.d, `SELECT count(*) FROM dettati_esclusioni`); n != 1 {
		t.Errorf("esclusioni %d", n)
	}
}

// inventario: ADM-12
// Solo il titolare: il dipendente torna alla home con «Operazione riservata al titolare.» da ogni rotta della raccolta, e
// niente si scrive (nessuna impostazione, nessuna cancellazione, nessun audit).
func TestAssistenteRaccoltaSoloTitolare(t *testing.T) {
	p := facciaConRaccolta(t, nil, nil)
	eseguiSQL(t, p.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.training.collect', '1', '2026-11-20T09:00:00.000000Z'),
		('assist.training.remaining', '0', '2026-11-20T09:00:00.000000Z')`)
	esclusi := p.busta(t, "Ofelia Pagina", "QR-114-G", false)
	eseguiSQL(t, p.d, `UPDATE impostazioni SET valore = '5' WHERE chiave = 'assist.training.remaining'`)
	p.busta(t, "Ofelia Pagina", "QR-114-H", true)
	eseguiSQL(t, p.d, `UPDATE impostazioni SET valore = '0' WHERE chiave = 'assist.training.remaining'`)
	prima := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'assist.%'`)
	b := dipendenteDentro(t, p.h)
	b.vai("GET", "/", nil, nil)
	for _, x := range []struct {
		percorso string
		kv       []string
	}{
		{percorsoAccendi, nil}, {percorsoSpegni, nil}, {percorsoNuovoLotto, []string{"conferma", "1"}},
		{percorsoEscludi, []string{"exclude[bag_number]", esclusi.Numero}},
		{percorsoEscludiCliente, []string{"exclude_customer[bag_number]", esclusi.Numero, "conferma", "1"}},
	} {
		r := b.vai("POST", x.percorso, moduloRaccolta(b, x.kv...), nil)
		if !redirectA(r, "/") {
			t.Errorf("%s: %d %q", x.percorso, r.codice, r.location)
		}
		contiene(t, x.percorso, b.vai("GET", "/", nil, nil).corpo, "Operazione riservata al titolare.")
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'assist.%'`); n != prima {
		t.Errorf("il dipendente ha scritto %d righe d'audit", n-prima)
	}
	if a, r := p.impostazione(t, buste.ChiaveRaccoltaAccesa), p.impostazione(t, buste.ChiaveRaccoltaRimaste); a != "1" || r != "0" {
		t.Errorf("accesa %s, rimaste %s", a, r)
	}
	if contaRighe(t, p.d, `SELECT count(*) FROM dettati_addestramento`) != 1 || contaRighe(t, p.d, `SELECT count(*) FROM dettati_esclusioni`) != 0 {
		t.Error("il dipendente ha cancellato")
	}
}

// raccoltaGuasta sono le buste con la raccolta che non risponde: lo stato (per la pagina) o le azioni.
type raccoltaGuasta struct {
	Buste
	stato bool
}

var errRaccoltaGuasta = errors.New("guasto con il numero 1/26 e Ofelia")

func (g raccoltaGuasta) StatoRaccolta(ctx context.Context) (buste.StatoRaccolta, error) {
	if g.stato {
		return buste.StatoRaccolta{}, errRaccoltaGuasta
	}
	return g.Buste.StatoRaccolta(ctx)
}

func (raccoltaGuasta) ImpostaRaccolta(context.Context, bool, audit.Contesto) error {
	return errRaccoltaGuasta
}
func (raccoltaGuasta) NuovoLottoRaccolta(context.Context, audit.Contesto) error {
	return errRaccoltaGuasta
}
func (raccoltaGuasta) EscludiBustaDallaRaccolta(context.Context, string, audit.Contesto) (string, error) {
	return "", errRaccoltaGuasta
}
func (raccoltaGuasta) EscludiClienteDallaRaccolta(context.Context, string, audit.Contesto) (string, int64, error) {
	return "", 0, errRaccoltaGuasta
}

// inventario: ADM-12
// Un guasto è la pagina d'errore, con una riga fissa nel registro: mai il numero scritto, mai il testo dell'errore.
func TestAssistenteRaccoltaGuasto(t *testing.T) {
	for _, stato := range []bool{false, true} {
		var reg bytes.Buffer
		p := facciaConRaccolta(t, &reg, func(b Buste) Buste { return raccoltaGuasta{b, stato} })
		b := titolareDentro(t, p.h)
		if stato {
			if r := b.vai("GET", paginaAssistentePercorso, nil, nil); r.codice != http.StatusInternalServerError {
				t.Errorf("pagina: %d", r.codice)
			}
			contiene(t, "registro", reg.String(), "raccolta dei dettati non letta")
			continue
		}
		b.vai("GET", paginaAssistentePercorso, nil, nil)
		for _, x := range []struct {
			percorso string
			kv       []string
		}{
			{percorsoAccendi, nil}, {percorsoSpegni, nil}, {percorsoNuovoLotto, []string{"conferma", "1"}},
			{percorsoEscludi, []string{"exclude[bag_number]", "12/26"}},
			{percorsoEscludiCliente, []string{"exclude_customer[bag_number]", "12/26", "conferma", "1"}},
		} {
			if r := b.vai("POST", x.percorso, moduloRaccolta(b, x.kv...), nil); r.codice != http.StatusInternalServerError {
				t.Errorf("%s: %d", x.percorso, r.codice)
			}
		}
		contiene(t, "registro", reg.String(), "raccolta dei dettati non cambiata")
		nonContiene(t, "registro", reg.String(), "12/26", "Ofelia", "1/26")
	}
}

// inventario: RIP-H-50
// D-109 dalla pagina: le gemelle danno il rifiuto della D-89 e non cancellano; gli esiti nominano il numero visibile
// (l'etichetta, o «N.» e il numero interno per una busta senza etichetta).
func TestAssistenteRaccoltaNumeroEtichetta(t *testing.T) {
	p := facciaConRaccolta(t, nil, nil)
	eseguiSQL(t, p.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.training.collect', '1', '2026-11-20T09:00:00.000000Z'),
		('assist.training.remaining', '500', '2026-11-20T09:00:00.000000Z')`)
	gemella := func(numero, nome string) {
		a := buste.Accettazione{NomeCliente: nome, TelefonoCliente: "333 000 0" + numero[:1] + "11", Numero: numero, DataAccettazione: "2026-11-20",
			Descrizione: "Collana da saldare", CodiceQR: "RIP-2026-0060",
			Dettato: &buste.Dettato{Testo: dettatoPagina, Proposta: []byte(`{}`), Modello: "modello-di-prova-v1"}}
		a.GemelleConfermate = buste.EtichettaScritta(a.CodiceQR, buste.AnnoDiAccettazione(a.DataAccettazione, time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)))
		if _, err := p.b.Crea(context.Background(), a, audit.Contesto{Attore: "titolare@esempio.it"}); err != nil {
			t.Fatal(err)
		}
	}
	gemella("6/26", "Prima Gemella")
	gemella("7/26", "Seconda Gemella")
	senza := p.busta(t, "Ofelia Pagina", "QR-114-N", true)
	b := titolareNellAssistente(t, p.h)
	piu := `<div role="alert" class="flash flash-errore">Più buste corrispondono a «0060»: scrivi il numero con l&#39;anno o usa il QR.</div>`
	contiene(t, "gemelle", azioneRaccolta(t, b, percorsoEscludi, "exclude[bag_number]", " 0060 "), piu)
	contiene(t, "gemelle cliente", azioneRaccolta(t, b, percorsoEscludiCliente, "exclude_customer[bag_number]", "0060", "conferma", "1"), piu)
	if n := contaRighe(t, p.d, `SELECT count(*) FROM dettati_addestramento`); n != 3 {
		t.Fatalf("le gemelle hanno cancellato: %d", n)
	}
	contiene(t, "busta", azioneRaccolta(t, b, percorsoEscludi, "exclude[bag_number]", senza.Numero),
		`<div role="status" class="flash flash-info">Busta N. `+senza.Numero+` esclusa: il suo dettato è stato cancellato.</div>`)
	contiene(t, "di nuovo", azioneRaccolta(t, b, percorsoEscludi, "exclude[bag_number]", senza.Numero),
		`<div role="alert" class="flash flash-errore">La busta N. `+senza.Numero+` non ha un dettato raccolto.</div>`)
	contiene(t, "cliente", azioneRaccolta(t, b, percorsoEscludiCliente, "exclude_customer[bag_number]", "0060/26", "conferma", "1"),
		strings.Replace(piu, "«0060»", "«0060/26»", 1))
	contiene(t, "cliente senza etichetta", azioneRaccolta(t, b, percorsoEscludiCliente, "exclude_customer[bag_number]", senza.Numero, "conferma", "1"),
		`<div role="status" class="flash flash-info">Cliente della busta N. `+senza.Numero+` escluso: cancellati 0 dettati; le sue buste future non saranno raccolte.</div>`)
}
