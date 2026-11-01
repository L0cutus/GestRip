package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// La pagina «Nuova riparazione» (/nuova, RIP-H-29…39, 41), come render(%{live_action: :new}) e
// save_repair/3 di WorkshopLive (go-baseline), su account e buste veri. I casi vengono da
// workshop_live_test.exs, workshop_qr_nudge_live_test.exs e workshop_assist_test.exs.

// busteFinte: nessuna busta (le pagine delle buste si provano sul database vero).
type busteFinte struct{}

func (busteFinte) ProssimoNumero(context.Context) (string, error) { return "1/26", nil }
func (busteFinte) Scelte(context.Context) (buste.Scelte, error) {
	return buste.Scelte{NomeNegozio: "Negozio"}, nil
}
func (busteFinte) Crea(context.Context, buste.Accettazione, audit.Contesto) (buste.Creata, error) {
	return buste.Creata{}, buste.Rifiuto(buste.MessaggioNonSalvata)
}
func (busteFinte) ApriScheda(context.Context, string, audit.Contesto) (buste.Scheda, error) {
	return buste.Scheda{}, buste.ErrBustaNonTrovata
}
func (busteFinte) Scheda(context.Context, int64) (buste.Scheda, error) {
	return buste.Scheda{}, buste.ErrBustaNonTrovata
}
func (busteFinte) Modifica(context.Context, int64, buste.Modifica, *bool, audit.Contesto) (buste.Modificata, error) {
	return buste.Modificata{}, buste.ErrBustaNonTrovata
}
func (busteFinte) TogliData(context.Context, int64, audit.Contesto) error {
	return buste.ErrBustaNonTrovata
}
func (busteFinte) CambiaStato(context.Context, int64, string, audit.Contesto) (buste.Cambio, error) {
	return buste.Cambio{}, buste.ErrTransizione
}
func (busteFinte) Riapri(context.Context, int64, string, string, audit.Contesto) (buste.Cambio, error) {
	return buste.Cambio{}, buste.ErrBustaNonTrovata
}
func (busteFinte) Attesa(context.Context, int64, string, string, audit.Contesto) (bool, error) {
	return false, buste.ErrBustaNonTrovata
}

// facciaConBuste è la faccia interna con account e buste veri sullo stesso database, l'orologio
// fermo al 31/10/2026 alle 9:00 UTC e il registro in reg.
// indirizzoPubblicoProva è l'indirizzo della faccia pubblica dei test (inventato).
const indirizzoPubblicoProva = "https://negozio.esempio.ts.net"

func facciaConBuste(t *testing.T, reg *bytes.Buffer) (http.Handler, *db.DB) {
	t.Helper()
	h, d, _ := facciaConBusteE(t, reg, indirizzoPubblicoProva)
	return h, d
}

// facciaConBusteE è facciaConBuste con l'indirizzo pubblico dato (vuoto: non configurato) e
// l'orologio finto.
func facciaConBusteE(t *testing.T, reg *bytes.Buffer, pubblico string) (http.Handler, *db.DB, *clock.Finto) {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
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
	dd.Buste = buste.Nuove(d, o)
	dd.IndirizzoPubblico, dd.Configurazione = pubblico, configProva
	return mustInterna(t, dd), d, o
}

func dipendenteDentro(t *testing.T, h http.Handler) *browser {
	t.Helper()
	b := nuovoBrowser(t, h)
	if r := b.prova("banco@esempio.it", pwProva); !redirectA(r, "/") {
		t.Fatalf("accesso del dipendente: %d", r.codice)
	}
	return b
}

func eseguiSQL(t *testing.T, d *db.DB, q string, args ...any) {
	t.Helper()
	if _, err := d.Scrittura.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}

// bustaProva è un modulo valido, come lo manda il browser: la coppia nome e telefono, la data di
// oggi, il numero proposto, la spunta compro oro con il suo campo nascosto «false».
func bustaProva(b *browser) url.Values {
	return url.Values{
		"_csrf_token":            {b.token},
		"repair[customer_name]":  {"Ada Esempio"},
		"repair[customer_phone]": {"333 111"},
		"repair[artisan_id]":     {""},
		"repair[bag_number]":     {"1/26"},
		"repair[accepted_on]":    {"2026-10-31"},
		"repair[expected_on]":    {""},
		"repair[qr_code]":        {"QR-PROVA"},
		"repair[description]":    {"Ridurre anello"},
		"repair[public_notes]":   {""},
		"repair[internal_notes]": {""},
		"repair[estimate_euros]": {""},
		"repair[compro_oro]":     {"false"},
	}
}

func con(v url.Values, campo, valore string) url.Values {
	n := url.Values{}
	for k, vv := range v {
		n[k] = slices.Clone(vv)
	}
	n.Set(campo, valore)
	return n
}

func contaBuste(t *testing.T, d *db.DB) int {
	t.Helper()
	return contaRighe(t, d, `SELECT count(*) FROM buste`)
}

func contiene(t *testing.T, cosa, corpo string, attesi ...string) {
	t.Helper()
	for _, s := range attesi {
		if !strings.Contains(corpo, s) {
			t.Errorf("%s: manca %q", cosa, s)
		}
	}
}

func nonContiene(t *testing.T, cosa, corpo string, vietati ...string) {
	t.Helper()
	for _, s := range vietati {
		if strings.Contains(corpo, s) {
			t.Errorf("%s: non doveva esserci %q", cosa, s)
		}
	}
}

// inventario: RIP-H-29
// «+ Nuova» dalla home porta a /nuova, una pagina sua per titolare e dipendente: «← Indietro» verso
// /, il titolo «Nuova riparazione» con «busta #N», il ramo «AI non configurata» (il riquadro «🔒
// Dettato AI» e il modulo «Inserimento riparazione» già aperto, nessun comando dell'AI), il menu
// della bottega. Aprirla non scrive nessuna riga d'audit (nessuna lettura della lista). Senza
// accesso si va al login.
func TestPaginaNuova(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	for _, b := range []*browser{titolareDentro(t, h), dipendenteDentro(t, h)} {
		r := b.vai("GET", "/", nil, nil)
		contiene(t, "home", r.corpo, `<a id="new-repair-link" href="/nuova"`, ">+ Nuova</a>")
		prima := contaRighe(t, d, `SELECT count(*) FROM eventi_audit`)
		r = b.vai("GET", "/nuova", nil, nil)
		if r.codice != 200 {
			t.Fatalf("/nuova: %d", r.codice)
		}
		if dopo := contaRighe(t, d, `SELECT count(*) FROM eventi_audit`); dopo != prima {
			t.Errorf("aprire /nuova ha scritto %d righe d'audit", dopo-prima)
		}
		contiene(t, "/nuova", r.corpo,
			"<title>Nuova riparazione · Laboratorio</title>",
			`id="new-bag-page"`,
			`<a id="new-repair-back" href="/" class="gs-back-btn">← Indietro</a>`,
			"Nuova riparazione\n<small>busta #1/26</small>",
			`<article id="assist-disabled"`, "<h2>🔒 Dettato AI</h2>",
			"La compilazione automatica della scheda dal testo dettato non è attualmente disponibile.",
			"Compila la riparazione usando il modulo qui sotto.",
			`<details id="manual-intake" open>`, "<span>Inserimento riparazione</span>",
			"<h2>Accetta riparazione</h2>",
			`<form id="new-repair-form" action="/nuova" method="post" data-invio-unico>`,
			`id="workshop-mobile-navigation"`, `id="fab-home"`)
		nonContiene(t, "/nuova", r.corpo, "Incolla il testo dettato", "Inserimento manuale senza AI", "Estrai con AI",
			"Detta a voce", `id="assist-form"`, `id="repairs"`, `id="public-link"`, `id="intake-qr-warning"`)
	}
	anonimo := nuovoBrowser(t, h)
	if r := anonimo.vai("GET", "/nuova", nil, nil); !redirectA(r, "/login") {
		t.Errorf("anonimo su /nuova: %d %q", r.codice, r.location)
	}
}

// reCampo trova i nomi dei campi del modulo della busta.
var reCampo = regexp.MustCompile(`name="repair\[([a-z_]+)\]"`)

// inventario: RIP-H-30
// I campi, le etichette, l'ordine e i testi dello standalone (oreficeria esclusa: non ci sono
// oreficerie esterne), con gli obbligatori del browser, la data di oggi in negozio e solo i nomi
// dell'elenco @intake_fields. Le foto non ci sono ancora (T4.3).
func TestModuloNuovaCampi(t *testing.T) {
	h, _ := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	r := b.vai("GET", "/nuova", nil, nil)
	etichette := []string{">Nome cliente<", ">Telefono<", ">Artigiano<", ">N° Busta<", ">Data accettazione<",
		">Consegna prevista<", ">Togli data<", ">Codice QR busta<", ">Lavorazione<", ">Note visibili al cliente<",
		">Note interne (non visibili)<", ">Preventivo<", "🪙 Scheda Compro Oro", ">Genera busta<"}
	ultimo := -1
	for _, e := range etichette {
		i := strings.Index(r.corpo, e)
		if i < 0 {
			t.Errorf("manca %q", e)
			continue
		}
		if i < ultimo {
			t.Errorf("%q fuori ordine", e)
		}
		ultimo = i
	}
	contiene(t, "campi", r.corpo,
		`<input id="customer-name" type="text" name="repair[customer_name]" value="" autocomplete="off">`,
		`<input id="customer-phone" type="tel" name="repair[customer_phone]" value="" autocomplete="off">`,
		`<select id="repair_artisan_id" name="repair[artisan_id]">`, `<option value="">— Nessuno —</option>`,
		`<input type="text" name="repair[bag_number]" id="repair_bag_number" value="1/26" inputmode="numeric" autocomplete="off" required>`,
		`<input type="date" name="repair[accepted_on]" id="repair_accepted_on" value="2026-10-31" required>`,
		`<input type="date" name="repair[expected_on]" id="repair_expected_on" value="">`,
		`<button id="intake-expected-clear" type="button" data-clear-field="repair_expected_on">Togli data</button>`,
		`<input type="text" name="repair[qr_code]" id="repair_qr_code" value="" placeholder="Scansiona o inserisci manualmente" autocomplete="off">`,
		`<textarea id="repair_description" name="repair[description]" required>`+"\n"+`</textarea>`,
		`<textarea id="repair_public_notes" name="repair[public_notes]">`+"\n"+`</textarea>`,
		`<textarea id="repair_internal_notes" name="repair[internal_notes]">`+"\n"+`</textarea>`,
		`<input type="text" name="repair[estimate_euros]" id="repair_estimate_euros" value="" placeholder="es. 120 oppure 400/500">`,
		`<input type="hidden" name="repair[compro_oro]" value="false">`,
		`<input type="checkbox" id="repair_compro_oro" name="repair[compro_oro]" value="true">🪙 Scheda Compro Oro`,
		`<button type="submit">Genera busta</button>`)
	nonContiene(t, "campi", r.corpo, "Oreficeria cliente", `name="repair[oreficeria_id]"`, "📷 Scansiona", "Fotocamera", "Galleria")
	ammessi := []string{"customer_name", "customer_phone", "oreficeria_id", "artisan_id", "bag_number", "accepted_on",
		"expected_on", "description", "public_notes", "internal_notes", "estimate_euros", "compro_oro", "qr_code"}
	for _, m := range reCampo.FindAllStringSubmatch(r.corpo, -1) {
		if !slices.Contains(ammessi, m[1]) {
			t.Errorf("campo %q fuori dall'elenco @intake_fields", m[1])
		}
	}
}

// inventario: RIP-H-30, RIP-H-33, RIP-H-41
// «Genera busta» salva la busta con il suo cliente (nuovo) e torna alla home con «Riparazione N
// creata.», una volta. I campi forzati fuori dall'elenco (prezzo finale, testo di consegna,
// stato) si scartano. L'audit repair.created ha come attore chi ha salvato.
func TestNuovaCreaBusta(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	b.vai("GET", "/nuova", nil, nil)
	v := bustaProva(b)
	v.Set("repair[estimate_euros]", "10,00")
	v.Set("repair[final_price_cents]", "99900")
	v.Set("repair[final_price_euros]", "999,00")
	v.Set("repair[expected_on_text]", "fine mese")
	v.Set("repair[status]", "consegnata")
	v.Set("stato", "consegnata")
	r := b.vai("POST", "/nuova", v, nil)
	if !creata(r) {
		t.Fatalf("salvataggio: %d %q %s", r.codice, r.location, r.corpo)
	}
	var riga string
	if err := d.Lettura.QueryRow(`SELECT numero || '|' || nome_cliente || '|' || telefono_cliente || '|' || descrizione || '|' ||
		stato || '|' || preventivo_centesimi || '|' || coalesce(prezzo_centesimi, prezzo_testo, '-') || '|' || coalesce(data_prevista_testo, '-') || '|' ||
		codice_qr FROM buste`).Scan(&riga); err != nil {
		t.Fatal(err)
	}
	if riga != "1/26|Ada Esempio|333 111|Ridurre anello|in_lavorazione|1000|-|-|QR-PROVA" {
		t.Errorf("busta %q", riga)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM clienti WHERE nome = 'Ada Esempio' AND telefono = '333 111'`); n != 1 {
		t.Errorf("clienti %d", n)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.created' AND attore = 'banco@esempio.it'`); n != 1 {
		t.Errorf("audit repair.created %d", n)
	}
	esito := r.location
	if r = b.vai("GET", "/", nil, nil); strings.Contains(r.corpo, "creata.") {
		t.Error("una home senza ?esito= mostra l'esito")
	}
	r = b.vai("GET", esito, nil, nil)
	contiene(t, "home dopo il salvataggio", r.corpo, `<div role="status" class="flash flash-info">Riparazione 1/26 creata.</div>`)
	if r = b.vai("GET", esito, nil, nil); strings.Contains(r.corpo, "creata.") {
		t.Error("l'esito si vede due volte")
	}
	if r = b.vai("GET", "/nuova", nil, nil); !strings.Contains(r.corpo, `value="2/26"`) || !strings.Contains(r.corpo, "busta #2/26") {
		t.Error("la pagina dopo non propone 2/26")
	}
}

// inventario: RIP-H-30, RIP-H-33
// Gli errori restano sul modulo con i valori scritti: quelli dei campi sotto il campo (i messaggi
// del changeset), gli altri in cima (i flash dello standalone). Nessuna busta e nessun cliente.
func TestNuovaErroriSulModulo(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (1, 'Il negozio di prova')`)
	b := titolareDentro(t, h)
	b.vai("GET", "/nuova", nil, nil)
	scritto := con(con(con(bustaProva(b), "repair[description]", ""), "repair[public_notes]", "Ritiro <sabato>"), "repair[internal_notes]", "Oro 750")
	r := b.vai("POST", "/nuova", scritto, nil)
	if r.codice != 200 {
		t.Fatalf("errore dei campi: %d", r.codice)
	}
	contiene(t, "errore dei campi", r.corpo,
		`<textarea id="repair_description" name="repair[description]" required>`+"\n"+`</textarea>`+"\n"+`</label>`+"\n"+`<p class="field-error">can&#39;t be blank</p>`,
		`value="Ada Esempio"`, `value="333 111"`, `value="QR-PROVA"`, ">\nRitiro &lt;sabato&gt;</textarea>", ">\nOro 750</textarea>",
		`name="repair[compro_oro]" value="true">🪙 Scheda Compro Oro`)
	casi := []struct{ campo, valore, messaggio string }{
		{"repair[customer_name]", "  ", "Inserisci il nome del cliente."},
		{"repair[customer_phone]", strings.Repeat("3", 41), "Controlla nome e telefono del cliente."},
		{"repair[oreficeria_id]", "1", "Seleziona un&#39;oreficeria valida."},
		{"repair[artisan_id]", "99", "Seleziona un artigiano valido."},
	}
	for _, c := range casi {
		r := b.vai("POST", "/nuova", con(bustaProva(b), c.campo, c.valore), nil)
		if r.codice != 200 || !strings.Contains(r.corpo, `<div role="alert" class="flash flash-errore">`+c.messaggio+`</div>`) {
			t.Errorf("%s=%q: %d, manca %q", c.campo, c.valore, r.codice, c.messaggio)
		}
		contiene(t, c.campo, r.corpo, ">\nRidurre anello</textarea>", `value="QR-PROVA"`)
	}
	// Come used_input?/1 di Phoenix: l'errore di un campo che il modulo non ha mandato non si vede.
	senzaCampo := bustaProva(b)
	senzaCampo.Del("repair[description]")
	r = b.vai("POST", "/nuova", senzaCampo, nil)
	if r.codice != 200 || strings.Contains(r.corpo, "field-error") {
		t.Errorf("campo non mandato: %d, errore visibile %v", r.codice, strings.Contains(r.corpo, "field-error"))
	}
	r = b.vai("POST", "/nuova", con(bustaProva(b), "repair[estimate_euros]", strings.Repeat("9", 256)), nil)
	contiene(t, "preventivo lungo", r.corpo, `<p class="field-error">Importo troppo lungo: massimo 255 caratteri.</p>`)
	if n := contaBuste(t, d); n != 0 {
		t.Errorf("buste salvate: %d", n)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM clienti`); n != 0 {
		t.Errorf("clienti salvati: %d", n)
	}
}

// inventario: RIP-H-31
// Il numero proposto è nel campo e nel titolo; un numero già usato (anche perché un collega ha
// salvato prima) dà «Numero busta già esistente.» sul modulo, mai un altro numero; un formato
// sbagliato dà il messaggio del formato; un numero scritto si usa e il prossimo proposto lo segue.
func TestNuovaNumero(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	collega := dipendenteDentro(t, h)
	b.vai("GET", "/nuova", nil, nil)
	collega.vai("GET", "/nuova", nil, nil)
	if r := collega.vai("POST", "/nuova", bustaProva(collega), nil); !creata(r) {
		t.Fatalf("collega: %d", r.codice)
	}
	r := b.vai("POST", "/nuova", con(bustaProva(b), "repair[description]", "Non creare"), nil)
	contiene(t, "numero vecchio", r.corpo, `<p class="field-error">Numero busta già esistente.</p>`, `value="1/26"`, "busta #1/26")
	r = b.vai("POST", "/nuova", con(bustaProva(b), "repair[bag_number]", "12-26"), nil)
	contiene(t, "formato", r.corpo, `<p class="field-error">formato richiesto N/AA (es. 12/26)</p>`, `value="12-26"`)
	if n := contaBuste(t, d); n != 1 {
		t.Errorf("buste %d", n)
	}
	// Dopo un errore il modulo mostra il numero risolto, come il changeset dello standalone: il
	// prossimo con il campo vuoto (senza consumarlo), quello scritto senza spazi.
	r = b.vai("POST", "/nuova", con(con(bustaProva(b), "repair[bag_number]", ""), "repair[description]", ""), nil)
	contiene(t, "numero vuoto", r.corpo, `id="repair_bag_number" value="2/26"`, "busta #2/26")
	r = b.vai("POST", "/nuova", con(con(bustaProva(b), "repair[bag_number]", " 7/26 "), "repair[description]", ""), nil)
	contiene(t, "numero con spazi", r.corpo, `id="repair_bag_number" value="7/26"`)
	// Con l'avviso del QR, che non passa dalla creazione, il numero resta com'è scritto.
	r = b.vai("POST", "/nuova", con(con(bustaProva(b), "repair[bag_number]", ""), "repair[qr_code]", ""), nil)
	contiene(t, "avviso", r.corpo, `id="repair_bag_number" value=""`)
	if r := b.vai("POST", "/nuova", con(bustaProva(b), "repair[bag_number]", ""), nil); !creata(r) || contaRighe(t, d, `SELECT count(*) FROM buste WHERE numero = '2/26'`) != 1 {
		t.Error("il numero mostrato dopo l'errore non è quello assegnato dopo")
	}
	if r := b.vai("POST", "/nuova", con(bustaProva(b), "repair[bag_number]", "9/26"), nil); !creata(r) {
		t.Fatalf("numero scritto: %d", r.codice)
	}
	if r := b.vai("GET", "/nuova", nil, nil); !strings.Contains(r.corpo, `id="repair_bag_number" value="10/26"`) {
		t.Error("dopo 9/26 non propone 10/26")
	}
}

// inventario: RIP-H-34
// «Oreficeria cliente» c'è solo con almeno un'oreficeria esterna: «— Diretta <negozio>» (il nome
// del profilo, o «Negozio») e le esterne per nome; la scelta resta dopo un errore e va sulla busta.
func TestNuovaOreficeria(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (1, 'Il negozio di prova')`)
	b := titolareDentro(t, h)
	if r := b.vai("GET", "/nuova", nil, nil); strings.Contains(r.corpo, "Oreficeria cliente") {
		t.Error("menu dell'oreficeria senza oreficerie esterne")
	}
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (2, 'Oreficeria Esempio'), (3, 'Alfa Gioielli')`)
	r := b.vai("GET", "/nuova", nil, nil)
	contiene(t, "menu", r.corpo, `<span class="label">Oreficeria cliente</span>`,
		`<select id="repair_oreficeria_id" name="repair[oreficeria_id]">`+"\n"+`<option value="">— Diretta Negozio</option>`+"\n"+
			`<option value="3">Alfa Gioielli</option>`+"\n"+`<option value="2">Oreficeria Esempio</option>`+"\n"+`</select>`)
	if i, j := strings.Index(r.corpo, "Oreficeria cliente"), strings.Index(r.corpo, ">Artigiano<"); i < 0 || i > j {
		t.Error("l'oreficeria non viene prima dell'artigiano")
	}
	eseguiSQL(t, d, `INSERT INTO profilo_negozio (nome, condizioni_garanzia, creato_il, aggiornato_il) VALUES ('Bottega Esempio', 'G', ?, ?)`,
		"2026-10-31T09:00:00.000000Z", "2026-10-31T09:00:00.000000Z")
	r = b.vai("GET", "/nuova", nil, nil)
	contiene(t, "nome del negozio", r.corpo, `<option value="">— Diretta Bottega Esempio</option>`)
	r = b.vai("POST", "/nuova", con(con(bustaProva(b), "repair[oreficeria_id]", "2"), "repair[description]", ""), nil)
	contiene(t, "scelta dopo l'errore", r.corpo, `<option value="2" selected>Oreficeria Esempio</option>`)
	// options_for_select: con l'avviso del QR (il modulo com'è scritto) la voce vuota scelta è
	// segnata; dopo un errore della creazione l'oreficeria è quella risolta, nil per la vuota, e
	// nessuna voce è segnata. Al primo GET nessuna voce è segnata.
	r = b.vai("POST", "/nuova", con(con(bustaProva(b), "repair[oreficeria_id]", ""), "repair[qr_code]", ""), nil)
	contiene(t, "vuota con l'avviso", r.corpo, `<option value="" selected>— Diretta Bottega Esempio</option>`)
	r = b.vai("POST", "/nuova", con(con(bustaProva(b), "repair[oreficeria_id]", ""), "repair[description]", ""), nil)
	contiene(t, "vuota dopo l'errore", r.corpo, `<option value="">— Diretta Bottega Esempio</option>`)
	r = b.vai("POST", "/nuova", con(con(bustaProva(b), "repair[oreficeria_id]", " 2"), "repair[description]", ""), nil)
	contiene(t, "risolta dopo l'errore", r.corpo, `<option value="2" selected>Oreficeria Esempio</option>`)
	if r := b.vai("POST", "/nuova", con(bustaProva(b), "repair[oreficeria_id]", "2"), nil); !creata(r) {
		t.Fatalf("salvataggio: %d", r.codice)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM buste WHERE oreficeria_id = 2`); n != 1 {
		t.Errorf("busta dell'oreficeria: %d", n)
	}
}

// inventario: RIP-H-35
// «Artigiano»: «— Nessuno —» e gli artigiani per nome; la scelta va sulla busta.
func TestNuovaArtigiano(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Cesellatore'), (2, 'Argentiere')`)
	b := titolareDentro(t, h)
	r := b.vai("GET", "/nuova", nil, nil)
	contiene(t, "menu", r.corpo, `<select id="repair_artisan_id" name="repair[artisan_id]">`+"\n"+`<option value="">— Nessuno —</option>`+"\n"+
		`<option value="2">Argentiere</option>`+"\n"+`<option value="1">Cesellatore</option>`+"\n"+`</select>`)
	if r := b.vai("POST", "/nuova", con(bustaProva(b), "repair[artisan_id]", "1"), nil); !creata(r) {
		t.Fatalf("salvataggio: %d", r.codice)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM buste WHERE artigiano_id = 1`); n != 1 {
		t.Errorf("busta dell'artigiano: %d", n)
	}
}

// inventario: RIP-H-36, RIP-H-37, RIP-H-38
// Il preventivo a testo libero si salva com'è; la consegna prevista è una data; la spunta compro
// oro (campo nascosto «false» e casella «true», vale l'ultimo) apre una scheda in valutazione e
// resta spuntata dopo un errore. «Togli data» svuota il campo con app.js, come lo standalone (che
// lo fa con JS.dispatch): senza JavaScript non fa niente.
func TestNuovaPreventivoConsegnaCompraOro(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	b.vai("GET", "/nuova", nil, nil)
	v := bustaProva(b)
	v.Set("repair[estimate_euros]", "400/500")
	v.Set("repair[expected_on]", "2026-11-20")
	v["repair[compro_oro]"] = []string{"false", "true"}
	v.Set("repair[description]", "")
	r := b.vai("POST", "/nuova", v, nil)
	contiene(t, "dopo l'errore", r.corpo, `value="400/500"`, `id="repair_expected_on" value="2026-11-20"`,
		`<input type="checkbox" id="repair_compro_oro" name="repair[compro_oro]" value="true" checked>`)
	v.Set("repair[description]", "Valutare un bracciale")
	if r := b.vai("POST", "/nuova", v, nil); !creata(r) {
		t.Fatalf("salvataggio: %d", r.codice)
	}
	var riga string
	if err := d.Lettura.QueryRow(`SELECT preventivo_testo || '|' || coalesce(preventivo_centesimi, '-') || '|' || data_prevista || '|' ||
		compro_oro || '|' || stato FROM buste`).Scan(&riga); err != nil {
		t.Fatal(err)
	}
	if riga != "400/500|-|2026-11-20|1|in_valutazione" {
		t.Errorf("busta %q", riga)
	}
	js := string(leggiStatico("app.js"))
	contiene(t, "app.js", js, `"[data-clear-field]"`, `input.value = ""`, `new Event("input", {bubbles: true})`)
}

// inventario: RIP-H-39
// Senza QR il primo «Genera busta» non salva: il campo è evidenziato, l'avviso dice il testo dello
// standalone, il pulsante diventa «Genera busta senza QR» e i campi restano; il secondo salva
// senza QR. Gli spazi non sono un QR; un QR scritto dopo l'avviso si salva; con il QR si salva
// subito; l'avviso vale per una busta, la pagina dopo chiede di nuovo.
func TestPaginaAvvisoQR(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	b.vai("GET", "/nuova", nil, nil)
	senza := con(bustaProva(b), "repair[qr_code]", "")
	r := b.vai("POST", "/nuova", senza, nil)
	if r.codice != 200 || contaBuste(t, d) != 0 {
		t.Fatalf("primo invio senza QR: %d, buste %d", r.codice, contaBuste(t, d))
	}
	contiene(t, "avviso", r.corpo, `<div id="intake-qr-code-row" class="intake-qr-row intake-qr-missing">`,
		`<p id="intake-qr-warning" role="alert">`+"\n"+`⚠ Manca il codice QR della busta: scansionalo, oppure premi di nuovo`+"\n"+
			`«Genera busta» per salvarla senza.`+"\n"+`</p>`,
		`<button type="submit">Genera busta senza QR</button>`, `<input name="qr_avvisato" type="hidden" value="1">`,
		">\nRidurre anello</textarea>", `value="Ada Esempio"`)
	nonContiene(t, "avviso", r.corpo, "field-error", "flash-errore")
	secondo := con(senza, "_csrf_token", b.token)
	secondo.Set("qr_avvisato", "1")
	if r := b.vai("POST", "/nuova", secondo, nil); !creata(r) {
		t.Fatalf("secondo invio: %d", r.codice)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM buste WHERE codice_qr IS NULL`); n != 1 {
		t.Errorf("busta senza QR: %d", n)
	}
	// La pagina dopo chiede di nuovo; gli spazi non sono un QR.
	r = b.vai("GET", "/nuova", nil, nil)
	nonContiene(t, "pagina dopo", r.corpo, `id="intake-qr-warning"`, "senza QR", "qr_avvisato")
	r = b.vai("POST", "/nuova", con(con(bustaProva(b), "repair[qr_code]", "   "), "repair[bag_number]", "2/26"), nil)
	contiene(t, "spazi", r.corpo, `id="intake-qr-warning"`)
	// Un QR scritto dopo l'avviso si salva.
	dopo := con(con(bustaProva(b), "repair[qr_code]", "QR-0042"), "repair[bag_number]", "2/26")
	dopo.Set("qr_avvisato", "1")
	if r := b.vai("POST", "/nuova", dopo, nil); !creata(r) {
		t.Fatalf("QR dopo l'avviso: %d", r.codice)
	}
	// Con il QR si salva al primo invio.
	b.vai("GET", "/nuova", nil, nil)
	if r := b.vai("POST", "/nuova", con(con(bustaProva(b), "repair[qr_code]", "QR-0007"), "repair[bag_number]", "3/26"), nil); !creata(r) {
		t.Fatalf("QR al primo invio: %d", r.codice)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM buste WHERE codice_qr IN ('QR-0042', 'QR-0007')`); n != 2 {
		t.Errorf("buste con il QR: %d", n)
	}
	// Dopo l'avviso un errore tiene l'avviso: il terzo invio non chiede più.
	b.vai("GET", "/nuova", nil, nil)
	r = b.vai("POST", "/nuova", con(con(senza, "_csrf_token", b.token), "repair[bag_number]", "4/26"), nil)
	contiene(t, "avviso", r.corpo, `id="intake-qr-warning"`)
	errato := con(con(con(senza, "_csrf_token", b.token), "repair[bag_number]", "4/26"), "repair[description]", "")
	errato.Set("qr_avvisato", "1")
	r = b.vai("POST", "/nuova", errato, nil)
	contiene(t, "errore dopo l'avviso", r.corpo, `id="intake-qr-warning"`, "Genera busta senza QR", `name="qr_avvisato"`, "can&#39;t be blank")
	rifiutato := con(con(con(senza, "_csrf_token", b.token), "repair[bag_number]", "4/26"), "repair[customer_name]", "")
	rifiutato.Set("qr_avvisato", "1")
	r = b.vai("POST", "/nuova", rifiutato, nil)
	contiene(t, "rifiuto dopo l'avviso", r.corpo, `id="intake-qr-warning"`, "Genera busta senza QR", `name="qr_avvisato"`,
		"Inserisci il nome del cliente.")
}

// reEsito è l'indirizzo dell'esito di una busta appena creata: un identificativo casuale opaco.
var reEsito = regexp.MustCompile(`^/\?esito=([A-Za-z0-9_-]{22})$`)

// creata dice se la risposta è il redirect all'esito di una busta salvata.
func creata(r risposta) bool {
	return r.codice == http.StatusFound && reEsito.MatchString(r.location) &&
		r.corpo == strings.Replace(corpoRedirect, "%s", strings.ReplaceAll(r.location, "&", "&amp;"), 1)
}

// reInvioNelModulo trova l'identificativo dell'invio nel modulo.
var reInvioNelModulo = regexp.MustCompile(`<input name="invio" type="hidden" value="([A-Za-z0-9_-]{22})">`)

func invioDi(t *testing.T, corpo string) string {
	t.Helper()
	m := reInvioNelModulo.FindStringSubmatch(corpo)
	if m == nil {
		t.Fatal("il modulo non ha l'identificativo dell'invio")
	}
	return m[1]
}

// inventario: RIP-H-41
// Il token esce solo nella pagina dell'esito, una volta: né nella risposta al salvataggio, né
// nell'indirizzo del redirect (che porta un identificativo opaco), né nei cookie (la sessione non
// porta niente dell'esito), né nel registro, né nell'audit.
func TestNuovaTokenMaiFuori(t *testing.T) {
	var reg bytes.Buffer
	h, d := facciaConBuste(t, &reg)
	b := titolareDentro(t, h)
	b.vai("GET", "/nuova", nil, nil)
	r := b.vai("POST", "/nuova", bustaProva(b), nil)
	if !creata(r) {
		t.Fatalf("salvataggio: %d %q", r.codice, r.location)
	}
	token := tokenDellaBusta(t, d, "1/26")
	var sessione string
	if k := setCookie(r.h, "__Host-gestrip_sessione_interna"); k != nil {
		dati, _, _ := strings.Cut(k.Value, ".")
		j, _ := base64.RawURLEncoding.DecodeString(dati)
		sessione = string(j)
	}
	if strings.Contains(sessione, "creata") || strings.Contains(sessione, reEsito.FindStringSubmatch(r.location)[1]) {
		t.Errorf("l'esito è nella sessione: %s", sessione)
	}
	casa := b.vai("GET", r.location, nil, nil)
	if strings.Count(casa.corpo, token) != 1 {
		t.Errorf("il token compare %d volte nella pagina del link", strings.Count(casa.corpo, token))
	}
	var righeAudit string
	_ = d.Lettura.QueryRow(`SELECT group_concat(coalesce(dati, '')) FROM eventi_audit`).Scan(&righeAudit)
	for cosa, testo := range map[string]string{"corpo": r.corpo, "location": r.location, "intestazioni": headerTesto(r.h),
		"sessione": sessione, "intestazioni della home": headerTesto(casa.h), "registro": reg.String(), "audit": righeAudit} {
		if strings.Contains(testo, token) {
			t.Errorf("il token è finito in %s", cosa)
		}
	}
}

// inventario: RIP-H-41, RIP-H-34
// Il redirect porta a /?esito=<id>: la prima GET di quell'indirizzo mostra «Riparazione N creata.» e
// «Link cliente, mostrato una volta» con <indirizzo_pubblico>/r/<token>, senza cache; per una busta
// di un'oreficeria esterna «Link dell'oreficeria». Una HEAD non lo consuma; una GET / normale non
// lo consuma; la seconda GET non lo mostra più. Un altro utente o un'altra sessione dello stesso
// utente con lo stesso indirizzo non vedono niente e non lo consumano.
func TestNuovaLinkMostratoUnaVolta(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (1, 'Il negozio di prova'), (2, 'Oreficeria Esempio')`)
	b := titolareDentro(t, h)
	altroUtente := dipendenteDentro(t, h)
	altraSessione := titolareDentro(t, h)
	b.vai("GET", "/nuova", nil, nil)
	r := b.vai("POST", "/nuova", bustaProva(b), nil)
	if !creata(r) {
		t.Fatalf("salvataggio: %d %q", r.codice, r.location)
	}
	esito := r.location
	indirizzo := indirizzoPubblicoProva + "/r/" + tokenDellaBusta(t, d, "1/26")
	for nome, altro := range map[string]*browser{"un altro utente": altroUtente, "un'altra sessione": altraSessione} {
		if r := altro.vai("GET", esito, nil, nil); r.codice != 200 || strings.Contains(r.corpo, "/r/") || strings.Contains(r.corpo, "creata.") {
			t.Errorf("%s vede l'esito: %d", nome, r.codice)
		}
	}
	if r := b.vai("HEAD", esito, nil, nil); r.codice != 200 {
		t.Errorf("HEAD: %d", r.codice)
	}
	if r := b.vai("GET", "/", nil, nil); strings.Contains(r.corpo, "creata.") {
		t.Error("GET / mostra l'esito")
	}
	r = b.vai("GET", esito, nil, nil)
	contiene(t, "link", r.corpo, `<div role="status" class="flash flash-info">Riparazione 1/26 creata.</div>`, `<div id="public-link-box">`,
		"<p>Link cliente, mostrato una volta</p>", `<p id="public-link">`+indirizzo+`</p>`)
	if r.h.Get("Cache-Control") != "no-store" {
		t.Errorf("la pagina del link si tiene in cache: %q", r.h.Get("Cache-Control"))
	}
	if r = b.vai("GET", esito, nil, nil); strings.Contains(r.corpo, "/r/") || strings.Contains(r.corpo, "creata.") {
		t.Error("l'esito si vede una seconda volta")
	}
	// Busta di un'oreficeria esterna.
	b.vai("GET", "/nuova", nil, nil)
	r = b.vai("POST", "/nuova", con(con(bustaProva(b), "repair[oreficeria_id]", "2"), "repair[bag_number]", "2/26"), nil)
	if !creata(r) {
		t.Fatalf("salvataggio dell'oreficeria: %d", r.codice)
	}
	r = b.vai("GET", r.location, nil, nil)
	contiene(t, "link dell'oreficeria", r.corpo, "<p>Link dell'oreficeria, mostrato una volta</p>",
		indirizzoPubblicoProva+"/r/"+tokenDellaBusta(t, d, "2/26"))
	// Dopo l'uscita e un nuovo accesso l'esito non vale più.
	b.vai("GET", "/nuova", nil, nil)
	r = b.vai("POST", "/nuova", con(bustaProva(b), "repair[bag_number]", "3/26"), nil)
	if !creata(r) {
		t.Fatalf("terzo salvataggio: %d", r.codice)
	}
	esito = r.location
	b.vai("POST", "/logout", url.Values{"_csrf_token": {b.token}}, nil)
	if r := b.prova("titolare@esempio.it", pwProva); !redirectA(r, "/") {
		t.Fatalf("nuovo accesso: %d", r.codice)
	}
	if r := b.vai("GET", esito, nil, nil); strings.Contains(r.corpo, tokenDellaBusta(t, d, "3/26")) || strings.Contains(r.corpo, "creata.") {
		t.Error("l'esito sopravvive a un nuovo accesso")
	}
}

// inventario: RIP-H-41
// Due schede: ognuna vede il suo esito, nell'ordine in cui le apre.
func TestNuovaDueSchede(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	b.vai("GET", "/nuova", nil, nil)
	primo := b.vai("POST", "/nuova", bustaProva(b), nil)
	secondo := b.vai("POST", "/nuova", con(bustaProva(b), "repair[bag_number]", "2/26"), nil)
	if !creata(primo) || !creata(secondo) || primo.location == secondo.location {
		t.Fatalf("salvataggi: %q %q", primo.location, secondo.location)
	}
	r := b.vai("GET", secondo.location, nil, nil)
	contiene(t, "seconda scheda", r.corpo, "Riparazione 2/26 creata.", tokenDellaBusta(t, d, "2/26"))
	r = b.vai("GET", primo.location, nil, nil)
	contiene(t, "prima scheda", r.corpo, "Riparazione 1/26 creata.", tokenDellaBusta(t, d, "1/26"))
}

// inventario: RIP-H-30, RIP-H-41
// Lo stesso modulo inviato due volte (doppio tocco, invio ripetuto dal browser) crea una busta sola,
// anche con il numero vuoto: il secondo invio porta allo stesso esito, senza errori. L'identificativo
// dell'invio resta nel modulo dopo un errore e dopo l'avviso del QR; un modulo nuovo ne ha un altro.
// Lo stesso identificativo da un'altra sessione o da un altro utente non porta all'esito di
// nessuno. app.js disabilita il pulsante al primo invio, come phx-submit di LiveView.
func TestNuovaDoppioInvio(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	pagina := b.vai("GET", "/nuova", nil, nil)
	invio := invioDi(t, pagina.corpo)
	contiene(t, "modulo", pagina.corpo, `<form id="new-repair-form" action="/nuova" method="post" data-invio-unico>`)
	if altro := invioDi(t, b.vai("GET", "/nuova", nil, nil).corpo); altro == invio {
		t.Error("due moduli con lo stesso identificativo")
	}
	v := con(bustaProva(b), "repair[bag_number]", "")
	v.Set("invio", invio)
	// Dopo l'avviso del QR e dopo un errore l'identificativo resta.
	r := b.vai("POST", "/nuova", con(v, "repair[qr_code]", ""), nil)
	if invioDi(t, r.corpo) != invio {
		t.Error("l'avviso del QR cambia l'identificativo")
	}
	r = b.vai("POST", "/nuova", con(v, "repair[description]", ""), nil)
	if invioDi(t, r.corpo) != invio {
		t.Error("l'errore cambia l'identificativo")
	}
	primo := b.vai("POST", "/nuova", v, nil)
	secondo := b.vai("POST", "/nuova", v, nil)
	if !creata(primo) || !creata(secondo) || primo.location != secondo.location {
		t.Fatalf("doppio invio: %d %q, %d %q %s", primo.codice, primo.location, secondo.codice, secondo.location, secondo.corpo)
	}
	if n := contaBuste(t, d); n != 1 {
		t.Errorf("buste create: %d", n)
	}
	r = b.vai("GET", primo.location, nil, nil)
	contiene(t, "esito", r.corpo, "Riparazione 1/26 creata.")
	nonContiene(t, "esito", r.corpo, "già esistente", "flash-errore")
	// Il terzo invio, a esito già visto, porta alla home senza link e non crea niente.
	terzo := b.vai("POST", "/nuova", v, nil)
	if !creata(terzo) || terzo.location != primo.location || contaBuste(t, d) != 1 {
		t.Errorf("terzo invio: %q, buste %d", terzo.location, contaBuste(t, d))
	}
	if r = b.vai("GET", terzo.location, nil, nil); strings.Contains(r.corpo, "/r/") || strings.Contains(r.corpo, "creata.") {
		t.Error("l'esito si vede di nuovo")
	}
	// Lo stesso identificativo da un'altra sessione dello stesso utente o da un altro utente non
	// porta all'esito: è un invio nuovo (qui con il numero vuoto, quindi una busta nuova).
	for i, altro := range []*browser{titolareDentro(t, h), dipendenteDentro(t, h)} {
		altro.vai("GET", "/nuova", nil, nil)
		w := con(v, "_csrf_token", altro.token)
		r := altro.vai("POST", "/nuova", w, nil)
		if !creata(r) || r.location == primo.location || contaBuste(t, d) != 2+i {
			t.Errorf("altro %d: %q, buste %d", i, r.location, contaBuste(t, d))
		}
	}
	js := string(leggiStatico("app.js"))
	contiene(t, "app.js", js, `"form[data-invio-unico]"`, "button.disabled = true", "pageshow")
}

// inventario: RIP-H-30
// Due invii dello stesso modulo che arrivano insieme creano una busta sola.
func TestNuovaDoppioInvioInsieme(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	invio := invioDi(t, b.vai("GET", "/nuova", nil, nil).corpo)
	v := con(bustaProva(b), "repair[bag_number]", "")
	v.Set("invio", invio)
	luoghi := make(chan string, 4)
	for range 4 {
		go func() {
			req, _ := http.NewRequest("POST", b.srv.URL+"/nuova", strings.NewReader(v.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			res, err := b.c.Do(req)
			if err != nil {
				luoghi <- "errore: " + err.Error()
				return
			}
			_ = res.Body.Close()
			luoghi <- res.Header.Get("Location")
		}()
	}
	primo := <-luoghi
	for range 3 {
		if l := <-luoghi; l != primo {
			t.Errorf("esiti diversi: %q %q", primo, l)
		}
	}
	if !reEsito.MatchString(primo) {
		t.Errorf("esito %q", primo)
	}
	if n := contaBuste(t, d); n != 1 {
		t.Errorf("buste create: %d", n)
	}
}

// inventario: RIP-H-41
// L'esito aspetta al massimo cinque minuti: dopo, la home non mostra niente.
func TestNuovaLinkScaduto(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := titolareDentro(t, h)
	b.vai("GET", "/nuova", nil, nil)
	r := b.vai("POST", "/nuova", bustaProva(b), nil)
	if !creata(r) {
		t.Fatalf("salvataggio: %d", r.codice)
	}
	o.Sposta(5*time.Minute + time.Second)
	r = b.vai("GET", r.location, nil, nil)
	nonContiene(t, "scaduto", r.corpo, tokenDellaBusta(t, d, "1/26"), "creata.", "mostrato una volta", "public-link")
}

// inventario: RIP-H-41
// Senza indirizzo_pubblico nel file della macchina la busta si salva lo stesso; l'esito dice
// «Riparazione N creata.» e una riga che spiega perché il link non si vede, senza il token.
func TestNuovaSenzaIndirizzoPubblico(t *testing.T) {
	h, d, _ := facciaConBusteE(t, nil, "")
	b := titolareDentro(t, h)
	b.vai("GET", "/nuova", nil, nil)
	r := b.vai("POST", "/nuova", bustaProva(b), nil)
	if !creata(r) {
		t.Fatalf("salvataggio: %d", r.codice)
	}
	esito := r.location
	r = b.vai("GET", esito, nil, nil)
	contiene(t, "senza indirizzo", r.corpo, "Riparazione 1/26 creata.",
		`<p id="public-link-missing">Link cliente non mostrato: manca «indirizzo_pubblico = https://host[:porta]» in `+configProva+
			`, poi riavvia gestrip serve con «sudo systemctl restart gestrip». Il link della busta resta valido: dalla scheda se ne potrà generare uno nuovo.</p>`)
	nonContiene(t, "senza indirizzo", r.corpo, tokenDellaBusta(t, d, "1/26"), `id="public-link"`)
	if r = b.vai("GET", esito, nil, nil); strings.Contains(r.corpo, "public-link-missing") {
		t.Error("la riga si vede una seconda volta")
	}
}

// Il deposito degli esiti: una sola lettura, solo per la stessa sessione (utente e nonce), scade;
// gli invii già andati a buon fine portano allo stesso esito, solo nella stessa sessione.
func TestDepositoEsiti(t *testing.T) {
	o := clock.NuovoFinto(time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
	dep := nuovoDepositoEsiti(o)
	id := dep.metti(7, "nonce-a", esitoBusta{Numero: "1/26", Link: linkMostrato{URL: "https://h/r/x"}})
	if len(id) != 22 || id == dep.metti(7, "nonce-a", esitoBusta{Numero: "2/26"}) {
		t.Errorf("identificativo %q", id)
	}
	if _, ok := dep.prendi(id, 8, "nonce-a"); ok {
		t.Error("un altro utente lo prende")
	}
	if _, ok := dep.prendi(id, 7, "nonce-b"); ok {
		t.Error("un'altra sessione lo prende")
	}
	if e, ok := dep.prendi(id, 7, "nonce-a"); !ok || e.Numero != "1/26" || e.Link.URL != "https://h/r/x" {
		t.Errorf("lettura: %+v %v", e, ok)
	}
	if _, ok := dep.prendi(id, 7, "nonce-a"); ok {
		t.Error("seconda lettura")
	}
	vecchio := dep.metti(7, "nonce-a", esitoBusta{Numero: "3/26"})
	o.Sposta(durataEsito)
	if _, ok := dep.prendi(vecchio, 7, "nonce-a"); ok {
		t.Error("scaduto ma letto")
	}
	dep.metti(7, "nonce-a", esitoBusta{})
	if n := dep.quantiEsiti(); n != 1 {
		t.Errorf("gli esiti scaduti restano: %d", n)
	}
	// Gli invii.
	a, primo := dep.prenota("inv", 7, "nonce-a")
	if !primo {
		t.Fatal("il primo invio non è il primo")
	}
	dep.chiudi(a, "esito-1")
	if a2, primo := dep.prenota("inv", 7, "nonce-a"); primo || a2.esito != "esito-1" {
		t.Errorf("secondo invio: %v %+v", primo, a2)
	}
	if _, primo := dep.prenota("inv", 7, "nonce-b"); !primo {
		t.Error("un'altra sessione trova l'invio")
	}
	if _, primo := dep.prenota("inv", 8, "nonce-a"); !primo {
		t.Error("un altro utente trova l'invio")
	}
	// Un invio non riuscito si può rifare.
	f, _ := dep.prenota("fallito", 7, "nonce-a")
	dep.chiudi(f, "")
	if _, primo := dep.prenota("fallito", 7, "nonce-a"); !primo {
		t.Error("un invio fallito blocca il successivo")
	}
	o.Sposta(durataInvio)
	if _, primo := dep.prenota("inv", 7, "nonce-a"); !primo {
		t.Error("un invio scaduto porta ancora all'esito")
	}
}

func tokenDellaBusta(t *testing.T, d *db.DB, numero string) string {
	t.Helper()
	var token string
	if err := d.Lettura.QueryRow(`SELECT token_pubblico FROM buste WHERE numero = ?`, numero).Scan(&token); err != nil || len(token) != 43 {
		t.Fatalf("token: %v %d", err, len(token))
	}
	return token
}

func headerTesto(h http.Header) string {
	var b strings.Builder
	_ = h.Write(&b) // in memoria: non fallisce
	return b.String()
}

// inventario: RIP-H-29
// Manca il ramo «AI configurata» (riquadro «1 · Incolla il testo dettato», modulo chiuso «Inserimento
// manuale senza AI» che si riapre a ogni estrazione): arriva con l'assistente (T4.10).
func TestNuovaConAIConfigurata(t *testing.T) {
	t.Skip("assistente AI: T4.10")
}

// inventario: RIP-H-30, RIP-H-39
// Mancano le foto scelte prima di generare la busta (T4.3) e il «📷 Scansiona» del QR, che
// richiede la fotocamera e JavaScript (con la prima funzione con JavaScript e il livello browser
// del banco).
func TestNuovaFotoEScansione(t *testing.T) {
	t.Skip("foto: T4.3; «📷 Scansiona»: JavaScript e livello browser del banco")
}

// inventario: RIP-H-31
// Manca il numero proposto con l'AI: quello calcolato al momento dell'estrazione, mai quello
// inventato dal modello (T4.10).
func TestNuovaNumeroConAI(t *testing.T) {
	t.Skip("assistente AI: T4.10")
}

// inventario: RIP-H-38
// Mancano i segni della scheda compro oro nella lista: etichetta 🪙, solo i suoi stati nel menu,
// niente pulsanti rapidi, niente «Preventivo» da QR (T4.2).
func TestNuovaCompraOroNellaLista(t *testing.T) {
	t.Skip("lista delle buste: T4.2")
}

// inventario: RIP-H-41
// Mancano le parti della lista e dell'AI dopo il salvataggio: l'esito che sparisce dopo 3 s, lo
// scorrimento alla prima busta, il link che sparisce cambiando filtri (T4.2) e lo svuotamento del
// testo dettato (T4.10).
func TestNuovaEsitoNellaLista(t *testing.T) {
	t.Skip("lista delle buste: T4.2; dettato: T4.10")
}

// busteLente: la prima creazione aspetta via e poi rifiuta; le altre contano e rifiutano.
type busteLente struct {
	busteFinte
	via    chan struct{}
	dentro chan struct{}
	mu     sync.Mutex
	crea   int
}

func (b *busteLente) Crea(context.Context, buste.Accettazione, audit.Contesto) (buste.Creata, error) {
	b.mu.Lock()
	b.crea++
	n := b.crea
	b.mu.Unlock()
	if n == 1 {
		close(b.dentro)
		<-b.via
	}
	return buste.Creata{}, buste.ErrNomeCliente
}

// inventario: RIP-H-30
// Un secondo invio che aspetta un primo andato male non porta a nessun esito: diventa lui il primo,
// prova a creare la sua busta e ridisegna il modulo con il suo rifiuto.
func TestNuovaDoppioInvioDopoUnRifiuto(t *testing.T) {
	lente := &busteLente{via: make(chan struct{}), dentro: make(chan struct{})}
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, &utentiFinti{attivo: true})
	dd.Buste = lente
	b := nuovoBrowser(t, mustInterna(t, dd))
	if r := b.entra(emailProva, passwordProva); !redirectA(r, "/") {
		t.Fatalf("accesso: %d", r.codice)
	}
	invio := invioDi(t, b.vai("GET", "/nuova", nil, nil).corpo)
	v := bustaProva(b)
	v.Set("invio", invio)
	risposte := make(chan string, 2)
	manda := func() {
		req, _ := http.NewRequest("POST", b.srv.URL+"/nuova", strings.NewReader(v.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := b.c.Do(req)
		if err != nil {
			risposte <- "errore: " + err.Error()
			return
		}
		defer func() { _ = res.Body.Close() }()
		corpo, _ := io.ReadAll(res.Body)
		risposte <- strconv.Itoa(res.StatusCode) + " " + string(corpo)
	}
	go manda()
	<-lente.dentro
	go manda()
	time.Sleep(50 * time.Millisecond) // il secondo invio arriva mentre il primo è dentro
	close(lente.via)
	for range 2 {
		if r := <-risposte; !strings.HasPrefix(r, "200 ") || !strings.Contains(r, "Inserisci il nome del cliente.") {
			t.Errorf("risposta %.80q", r)
		}
	}
	if lente.crea != 2 {
		t.Errorf("creazioni tentate: %d", lente.crea)
	}
}
