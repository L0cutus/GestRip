package web

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La home con la lista (home.go, pagine/home.html), come WorkshopLive (:index) e i casi di
// workshop_live_test.exs, workshop_qr_nudge_live_test.exs e workshop_test.exs dello standalone, su
// account e buste veri, orologio fermo al 31/10/2026.

// bustaHome crea una busta col numero e il QR dati e scrive le colonne in più. Dà l'id.
func bustaHome(t *testing.T, d *db.DB, o clock.Orologio, numero, qr, colonne string, arg ...any) int64 {
	t.Helper()
	id := bustaScheda(t, d, o, func(a *buste.Accettazione) {
		a.Numero, a.CodiceQR, a.DataPrevista, a.NomeCliente = numero, qr, "", "Cliente "+numero
	})
	if colonne != "" {
		eseguiSQL(t, d, `UPDATE buste SET `+colonne+` WHERE id = ?`, append(arg, id)...)
	}
	return id
}

const istanteHome = "2026-10-31T09:00:00.000000Z"

// riquadroBusta è il riquadro della busta id nella pagina, dal suo inizio al prossimo riquadro.
func riquadroBusta(t *testing.T, corpo string, id int64) string {
	t.Helper()
	inizio := strings.Index(corpo, `<div id="repair-`+strconv.FormatInt(id, 10)+`" class="rep-card`)
	if inizio < 0 {
		t.Fatalf("manca il riquadro della busta %d", id)
	}
	resto := corpo[inizio+1:]
	if fine := strings.Index(resto, `<div id="repair-`); fine >= 0 {
		resto = resto[:fine]
	}
	return "<" + resto
}

var reRiquadri = regexp.MustCompile(`<div id="repair-([0-9]+)" class="rep-card`)

func ordineRiquadri(corpo string) []string {
	var out []string
	for _, m := range reRiquadri.FindAllStringSubmatch(corpo, -1) {
		out = append(out, m[1])
	}
	return out
}

// inventario: RIP-H-01, RIP-H-02, RIP-H-11
// La home del titolare e del dipendente: titolo «Riparazioni», intestazione della bottega col nome
// del negozio, i riquadri dei contatori (sempre i sette e 💬, gli altri tre solo se contano), il
// totale, «+ Nuova», e la lista vuota con «Nessuna riparazione trovata.».
func TestHomeVuota(t *testing.T) {
	h, d, _ := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	eseguiSQL(t, d, `INSERT INTO profilo_negozio (nome, condizioni_garanzia, creato_il, aggiornato_il) VALUES ('Bottega & Figli', 'x', ?, ?)`, istanteHome, istanteHome)
	for _, b := range []*browser{titolareDentro(t, h), dipendenteDentro(t, h)} {
		r := b.vai("GET", "/", nil, nil)
		if r.codice != 200 {
			t.Fatalf("%d", r.codice)
		}
		contiene(t, "home", r.corpo, "<title>Riparazioni · Laboratorio</title>", `<p class="bottega-marchio">GestRip</p>`,
			`<h1 class="bottega-nome">Bottega &amp; Figli</h1>`, `<p class="bottega-sotto">Banco riparazioni</p>`,
			`<a id="dashboard-pronta" href="/?stato=pronta" class="dash-card dash-green" title="Pronte" aria-label="Pronte: 0">`,
			`<a id="dashboard-lavorazione" href="/?stato=in_lavorazione"`, `<a id="dashboard-preventivo" href="/?stato=preventivo"`,
			`<a id="dashboard-attesa" href="/?stato=in_attesa_materiali"`, `<a id="dashboard-consegnate" href="/?stato=consegnata"`,
			`<a id="dashboard-scadenza" href="/?scadenza=1"`, `<a id="dashboard-compro-oro" href="/?compro_oro=1"`,
			`<a id="dashboard-messaggi" href="/?messaggi=1" class="dash-card dash-blue" title="💬 Messaggi" aria-label="💬 Messaggi: 0">`,
			`<p id="repairs-total" class="gs-lista-totale">0 riparazioni</p>`, `<a id="new-repair-link" href="/nuova"`,
			`<div id="repairs-empty" class="gs-lista-vuota">Nessuna riparazione trovata.</div>`)
		nonContiene(t, "home", r.corpo, "dashboard-attesa-cliente", "dashboard-fuori-bottega", "dashboard-senza-qr",
			"repair-pagination", "phx-")
	}
}

// inventario: RIP-H-02, RIP-H-06
// Un riquadro nascosto a zero ricompare quando il suo filtro è attivo; ogni riquadro porta al filtro
// che conta e la lista mostra quello che contava.
func TestHomeRiquadriPortanoAlFiltro(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := titolareDentro(t, h)
	for _, q := range []string{"attesa=1", "fuori_bottega=1", "senza_qr=1"} {
		corpo := b.vai("GET", "/?"+q, nil, nil).corpo
		id := map[string]string{"attesa=1": "dashboard-attesa-cliente", "fuori_bottega=1": "dashboard-fuori-bottega", "senza_qr=1": "dashboard-senza-qr"}[q]
		contiene(t, q, corpo, `<a id="`+id+`" href="/?`+q+`"`)
	}
	pronta := bustaHome(t, d, o, "1/26", "Q1", `stato = 'pronta'`)
	attesa := bustaHome(t, d, o, "2/26", "Q2", `attesa_cliente = 1`)
	fuori := bustaHome(t, d, o, "3/26", "Q3", `consegnata_artigiano_il = ?`, istanteHome)
	senza := bustaHome(t, d, o, "4/26", "", ``)
	corpo := b.vai("GET", "/", nil, nil).corpo
	contiene(t, "home", corpo, `aria-label="Pronte: 1"`, `aria-label="In lavorazione: 3"`, `aria-label="⏳ In attesa: 1"`,
		`aria-label="📤 Fuori bottega: 1"`, `aria-label="⚠ Senza QR: 1"`, `<p id="repairs-total" class="gs-lista-totale">4 riparazioni</p>`)
	for q, voluti := range map[string][]int64{
		"stato=pronta": {pronta}, "attesa=1": {attesa}, "fuori_bottega=1": {fuori}, "senza_qr=1": {senza},
		"stato=in_lavorazione": {senza, fuori, attesa}, "messaggi=1": nil, "compro_oro=1": nil, "scadenza=1": nil,
	} {
		var v []string
		for _, id := range voluti {
			v = append(v, strconv.FormatInt(id, 10))
		}
		if got := ordineRiquadri(b.vai("GET", "/?"+q, nil, nil).corpo); strings.Join(got, ",") != strings.Join(v, ",") {
			t.Errorf("%s: %v, voluti %v", q, got, v)
		}
	}
	if c := b.vai("GET", "/?stato=pronta", nil, nil).corpo; !strings.Contains(c, `<p id="repairs-total" class="gs-lista-totale">1 riparazione</p>`) {
		t.Error("«1 riparazione» al singolare")
	}
}

// inventario: RIP-H-11, RIP-H-12, RIP-H-56
// Il riquadro di una busta: tutto il riquadro e il numero aprono /repairs/{id} con un link semplice
// (senza il segno di D-44: un'apertura vera); numero dell'etichetta (D-64); cliente, data,
// descrizione; oreficeria, stato, compro oro, attesa con i giorni e il motivo nel titolo, ⚠ senza QR
// solo sulle aperte, consegna a testo o a data, 💬/🗨, le due 🤝; bordi e attenuazione.
func TestHomeRiquadroBusta(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('scadenza_giorni', '7', ?)`, istanteHome)
	eseguiSQL(t, d, `INSERT OR IGNORE INTO oreficerie (id, nome) VALUES (2, 'Oreficeria Sintetica')`)
	piena := bustaHome(t, d, o, "21/26", "RIP-2026-0045", `oreficeria_id = 2, compro_oro = 0, attesa_cliente = 1,
		attesa_dal = '2026-10-28 09:00:00', attesa_motivo = 'Quale <pietra>?', data_prevista = '2026-11-02', ultimo_messaggio_il = ?,
		preventivo_accettato_il = ?, preventivo_rifiutato_il = ?, fissata_il = ?`, istanteHome, istanteHome, istanteHome, istanteHome)
	vecchia := bustaHome(t, d, o, "22/26", "RIP-2025-0045", `data_prevista_testo = 'fine mese', data_prevista = '2026-11-01',
		ultimo_messaggio_il = ?, messaggi_letti_il = ?`, istanteHome, istanteHome)
	senza := bustaHome(t, d, o, "23/26", "  ", `data_prevista = '2026-11-20'`)
	chiusa := bustaHome(t, d, o, "24/26", "", `stato = 'consegnata', data_prevista = '2026-10-01'`)
	altro := bustaHome(t, d, o, "25/26", "SINT-QR-25", `stato = 'pronta', data_prevista = '2026-10-01'`)
	b := titolareDentro(t, h)
	corpo := b.vai("GET", "/", nil, nil).corpo
	if got := strings.Join(ordineRiquadri(corpo), ","); got != fmt.Sprintf("%d,%d,%d,%d,%d", piena, altro, chiusa, senza, vecchia) {
		t.Errorf("ordine %s", got)
	}
	p := strconv.FormatInt(piena, 10)
	contiene(t, "piena", riquadroBusta(t, corpo, piena),
		`<div id="repair-`+p+`" class="rep-card rep-card-urgente rep-card-pinned rep-card-messaggio">`,
		`<a id="repair-open-`+p+`" href="/repairs/`+p+`" class="rep-card-apri" aria-label="Apri busta 0045/26"></a>`,
		`<a href="/repairs/`+p+`" class="rep-card-numero" aria-label="Apri busta 0045/26">#0045/26</a>`,
		`<p class="rep-card-cliente">Cliente 21/26</p>`, `<p class="rep-card-data">05/09/2026</p>`,
		`<p class="rep-card-descrizione">Anello
Saldatura</p>`, `<span class="segno segno-oreficeria">Oreficeria Sintetica</span>`,
		`<span id="repair-status-`+p+`" class="stato stato-orange">In lavorazione</span>`,
		`<span id="waiting-repair-`+p+`" class="segno segno-attesa" title="In attesa risposta cliente — Quale &lt;pietra&gt;?">⏳ 3gg</span>`,
		`<span id="repair-delivery-`+p+`" class="segno consegna-tag-urgente">⏰ 02/11/2026</span>`,
		`<span id="repair-message-`+p+`" class="segno segno-messaggio" title="Messaggio non letto">💬</span>`,
		`<span id="repair-quote-approved-`+p+`" class="segno quote-badge-approved" title="Preventivo confermato dal cliente — non letto">🤝</span>`,
		`<span id="repair-quote-rejected-`+p+`" class="segno quote-badge-rejected" title="Cliente ha rifiutato il preventivo — non letto">🤝</span>`)
	nonContiene(t, "piena", riquadroBusta(t, corpo, piena), "mostra=", "repair-noqr-", "🪙")
	v := strconv.FormatInt(vecchia, 10)
	contiene(t, "vecchia", riquadroBusta(t, corpo, vecchia), `<div id="repair-`+v+`" class="rep-card rep-card-urgente">`, `>#0045/25</a>`,
		`<span id="repair-delivery-`+v+`" class="segno consegna-tag-urgente">⏰ fine mese</span>`,
		`<span id="repair-message-`+v+`" class="segno segno-letto" title="Conversazione (già letta)">🗨</span>`)
	s := strconv.FormatInt(senza, 10)
	contiene(t, "senza", riquadroBusta(t, corpo, senza), `aria-label="Apri busta N. 23/26">N. 23/26</a>`,
		`<span id="repair-noqr-`+s+`" class="segno segno-noqr" title="Busta senza codice QR: aggiungilo dalla scheda">⚠ senza QR</span>`,
		`<span id="repair-delivery-`+s+`" class="segno segno-consegna">⏰ 20/11/2026</span>`)
	c := strconv.FormatInt(chiusa, 10)
	contiene(t, "chiusa", riquadroBusta(t, corpo, chiusa), `<div id="repair-`+c+`" class="rep-card rep-card-chiusa">`, `>N. 24/26</a>`,
		`<span id="repair-status-`+c+`" class="stato stato-stone">Consegnata</span>`, `class="segno segno-consegna">⏰ 01/10/2026</span>`)
	nonContiene(t, "chiusa", riquadroBusta(t, corpo, chiusa), "repair-noqr-")
	a := strconv.FormatInt(altro, 10)
	contiene(t, "altro", riquadroBusta(t, corpo, altro), `>N. 25/26</a>`, `<span id="repair-status-`+a+`" class="stato stato-emerald">Pronta</span>`,
		`class="segno segno-consegna">⏰ 01/10/2026</span>`)
	nonContiene(t, "altro", riquadroBusta(t, corpo, altro), "repair-noqr-", "repair-message-")
	// Il link del riquadro è un'apertura vera: la scheda scrive repair.viewed.
	prima := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`)
	if r := b.vai("GET", "/repairs/"+p, nil, nil); r.codice != 200 {
		t.Fatalf("scheda: %d", r.codice)
	}
	if dopo := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`); dopo != prima+1 {
		t.Errorf("repair.viewed: %d", dopo-prima)
	}
}

// inventario: RIP-H-11
// La compro oro ha il segno 🪙 Compro Oro; una busta senza attesa_dal leggibile ha «⏳ » senza giorni.
func TestHomeCompraOroEAttesaSenzaData(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	oro := bustaHome(t, d, o, "1/26", "Q", `compro_oro = 1, stato = 'in_valutazione', attesa_cliente = 1, attesa_dal = 'ieri', attesa_motivo = ''`)
	corpo := titolareDentro(t, h).vai("GET", "/", nil, nil).corpo
	x := strconv.FormatInt(oro, 10)
	contiene(t, "oro", riquadroBusta(t, corpo, oro), `<span class="segno segno-oro">🪙 Compro Oro</span>`,
		`<span id="waiting-repair-`+x+`" class="segno segno-attesa" title="In attesa risposta cliente">⏳ </span>`,
		`class="stato stato-orange">In valutazione</span>`)
}

// inventario: RIP-H-10, RIP-H-06
// 30 per pagina, «← Precedente», «N / M», «Successiva →»; la navigazione tiene i filtri dei
// riquadri; una pagina oltre l'ultima è l'ultima, una non valida la prima.
func TestHomePaginazione(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	for i := 1; i <= 32; i++ {
		bustaHome(t, d, o, fmt.Sprintf("%d/26", i), "Q", `attesa_cliente = 1`)
	}
	b := titolareDentro(t, h)
	r := b.vai("GET", "/", nil, nil).corpo
	contiene(t, "prima", r, `<p id="repairs-total" class="gs-lista-totale">32 riparazioni</p>`,
		`<nav id="repair-pagination" class="gs-paginazione" aria-label="Paginazione riparazioni">`,
		`<span class="pulsante-contorno gs-spento" aria-disabled="true">← Precedente</span>`, `<span class="gs-pagina">1 / 2</span>`,
		`<a href="/?p=2" class="pulsante-contorno">Successiva →</a>`)
	if n := len(ordineRiquadri(r)); n != 30 {
		t.Errorf("prima pagina: %d", n)
	}
	r = b.vai("GET", "/?attesa=1&p=2", nil, nil).corpo
	contiene(t, "seconda", r, `<a href="/?attesa=1&amp;p=1" class="pulsante-contorno">← Precedente</a>`, `<span class="gs-pagina">2 / 2</span>`,
		`<span class="pulsante-contorno gs-spento" aria-disabled="true">Successiva →</span>`)
	if n := len(ordineRiquadri(r)); n != 2 {
		t.Errorf("seconda pagina: %d", n)
	}
	for q, pagina := range map[string]string{"p=99": "2 / 2", "p=abc": "1 / 2", "p=0": "1 / 2", "p=-1": "1 / 2", "p[]=2": "1 / 2"} {
		contiene(t, q, b.vai("GET", "/?"+q, nil, nil).corpo, `<span class="gs-pagina">`+pagina+`</span>`)
	}
	// Tutti i filtri dei riquadri passano, nell'ordine di filter_query/3.
	contiene(t, "tutti", b.vai("GET", "/?messaggi=1&senza_qr=1&fuori_bottega=1&attesa=1&compro_oro=1&scadenza=1&stato=a+b", nil, nil).corpo,
		`<p id="repairs-total" class="gs-lista-totale">0 riparazioni</p>`)
	eseguiSQL(t, d, `UPDATE buste SET stato = 'preventivo', compro_oro = 0`)
	contiene(t, "stato", b.vai("GET", "/?stato=preventivo&attesa=yes", nil, nil).corpo, `<a href="/?stato=preventivo&amp;attesa=1&amp;p=2" class="pulsante-contorno">Successiva →</a>`)
}

// inventario: RIP-H-21
// Ogni visita della lista scrive una riga repair.index_viewed con i soli nomi dei filtri (mai i
// valori), in ordine, solo quelli della regola e solo se hanno un testo; una sola riga per visita
// (D-68). Se la riga non si scrive, la pagina va in errore.
func TestHomeRegistro(t *testing.T) {
	h, d, _ := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := titolareDentro(t, h)
	const righe = `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.index_viewed'`
	prima := contaRighe(t, d, righe) // l'accesso porta già alla home
	for q, voluti := range map[string]string{
		"":                 `[]`,
		"stato=pronta&p=2": `["p","stato"]`,
		"cerca=Mario+Rossi&artigiano=3&oreficeria=2&data_campo=pronta&data_da=2026-01-01&data_a=x&scadenza=1": `["artigiano","cerca","data_a","data_campo","data_da","oreficeria","scadenza"]`,
		"stato=+&p=&scadenza[]=1&compro_oro=1&attesa=1&vista=galleria&messaggi=1":                             `[]`,
		"esito=abc&senza_qr=1&fuori_bottega=1":                                                                `[]`,
	} {
		if r := b.vai("GET", "/?"+q, nil, nil); r.codice != 200 {
			t.Fatalf("%s: %d", q, r.codice)
		}
		var dati, attore string
		if err := d.Lettura.QueryRow(`SELECT dati, attore FROM eventi_audit WHERE azione = 'repair.index_viewed' ORDER BY id DESC LIMIT 1`).Scan(&dati, &attore); err != nil {
			t.Fatal(err)
		}
		if dati != `{"filter_keys":`+voluti+`}` || attore != "titolare@esempio.it" || strings.Contains(dati, "Mario") {
			t.Errorf("%q: %s %s", q, dati, attore)
		}
	}
	if n := contaRighe(t, d, righe); n != prima+5 {
		t.Errorf("una riga per visita: %d", n-prima)
	}
	// «Nuova riparazione» non scrive la riga.
	b.vai("GET", "/nuova?stato=pronta", nil, nil)
	if n := contaRighe(t, d, righe); n != prima+5 {
		t.Errorf("/nuova: %d", n-prima)
	}
	eseguiSQL(t, d, `CREATE TEMP TRIGGER no_lista BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.index_viewed' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if r := b.vai("GET", "/", nil, nil); r.codice != 500 || strings.Contains(r.corpo, "repair-dashboard") {
		t.Errorf("senza riga: %d", r.codice)
	}
}

// inventario: RIP-H-08
// Con gli interruttori spenti la home senza filtri non mostra pronte e consegnate; il riquadro le
// rimostra; i contatori non cambiano.
func TestHomeNascondePronteEConsegnate(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	p := bustaHome(t, d, o, "1/26", "Q", `stato = 'pronta'`)
	c := bustaHome(t, d, o, "2/26", "Q", `stato = 'consegnata', data_consegna = '2026-10-31'`)
	l := bustaHome(t, d, o, "3/26", "Q", ``)
	eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('home_mostra_pronte', '0', ?), ('home_mostra_consegnate', '0', ?)`, istanteHome, istanteHome)
	b := titolareDentro(t, h)
	corpo := b.vai("GET", "/", nil, nil).corpo
	if got := strings.Join(ordineRiquadri(corpo), ","); got != strconv.FormatInt(l, 10) {
		t.Errorf("senza filtri %s", got)
	}
	contiene(t, "contatori", corpo, `aria-label="Pronte: 1"`, `aria-label="Consegnate oggi: 1"`, `>1 riparazione</p>`)
	if got := strings.Join(ordineRiquadri(b.vai("GET", "/?stato=pronta", nil, nil).corpo), ","); got != strconv.FormatInt(p, 10) {
		t.Errorf("pronte %s", got)
	}
	if got := strings.Join(ordineRiquadri(b.vai("GET", "/?stato=consegnata", nil, nil).corpo), ","); got != strconv.FormatInt(c, 10) {
		t.Errorf("consegnate %s", got)
	}
	// La pagina da sola non è un filtro.
	if got := strings.Join(ordineRiquadri(b.vai("GET", "/?p=1", nil, nil).corpo), ","); got != strconv.FormatInt(l, 10) {
		t.Errorf("p=1 %s", got)
	}
}

// inventario: RIP-H-01
// Senza accesso la home rimanda al login e non scrive niente.
func TestHomeSenzaAccesso(t *testing.T) {
	h, d, _ := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	if r := nuovoBrowser(t, h).vai("GET", "/?stato=pronta", nil, nil); !redirectA(r, "/login") {
		t.Fatalf("%d %q", r.codice, r.location)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.index_viewed'`); n != 0 {
		t.Errorf("righe: %d", n)
	}
}

// inventario: RIP-H-11
// status_classes/1 su tutti gli stati delle due famiglie, e uno sconosciuto (il tono di ripiego).
func TestToniStato(t *testing.T) {
	for stato, tono := range map[string]string{
		"in_lavorazione": "stato-orange", "preventivo": "stato-violet", "pronta": "stato-emerald", "consegnata": "stato-stone",
		"in_attesa_materiali": "stato-blue", "annullata": "stato-red", "in_valutazione": "stato-orange", "valutata": "stato-violet",
		"perfezionata": "stato-emerald", "chiusa": "stato-stone", "boh": "stato-orange",
	} {
		if g := toniStato(stato); g != "stato "+tono {
			t.Errorf("%s: %q", stato, g)
		}
	}
}

// inventario: RIP-H-10, RIP-H-06, RIP-H-19
// filter_query/3: nell'ordine cerca, stato, oreficeria, artigiano, scadenza, compro_oro, attesa,
// fuori_bottega, senza_qr, messaggi, notifiche, data_da, data_a, data_campo (solo con una data), poi
// p; i valori codificati come un modulo (URI.encode_www_form/1); i filtri spenti tolti.
func TestIndirizzoLista(t *testing.T) {
	tutti := buste.Filtri{Cerca: "a b&c/é+%~*", Stato: "a b&c", Oreficeria: 2, Artigiano: 7, Scadenza: true, CompraOro: true, Attesa: true,
		FuoriBottega: true, SenzaQR: true, Messaggi: true, Notifiche: true, DataDa: "2026-01-01", DataA: "-0001-12-31", DataCampo: "consegnata"}
	for _, c := range []struct {
		f      buste.Filtri
		p      int64
		voluto string
	}{
		{tutti, 3, "/?cerca=a+b%26c%2F%C3%A9%2B%25~%2A&stato=a+b%26c&oreficeria=2&artigiano=7&scadenza=1&compro_oro=1&attesa=1&fuori_bottega=1" +
			"&senza_qr=1&messaggi=1&notifiche=1&data_da=2026-01-01&data_a=-0001-12-31&data_campo=consegnata&p=3"},
		{buste.Filtri{}, 2, "/?p=2"},
		{buste.Filtri{SenzaQR: true, Scadenza: true}, 1, "/?scadenza=1&senza_qr=1&p=1"},
		{buste.Filtri{DataCampo: "pronta"}, 1, "/?p=1"},
		{buste.Filtri{DataCampo: "boh", DataA: "2026-01-01"}, 1, "/?data_a=2026-01-01&data_campo=accettazione&p=1"},
		{buste.Filtri{DataCampo: "pronta", DataDa: "2026-01-01"}, 1, "/?data_da=2026-01-01&data_campo=pronta&p=1"},
	} {
		if g := indirizzoLista(c.f, c.p); g != c.voluto {
			t.Errorf("%+v: %q", c.f, g)
		}
	}
	if g := codificaQuery(parametriLista(tutti, 0)); strings.Contains(g, "p=") {
		t.Errorf("senza pagina: %q", g)
	}
}

// inventario: RIP-H-07, RIP-H-56
// Le pillole dei filtri attivi, nell'ordine e con i testi dello standalone: «⏰ In scadenza (12
// giorni)» con la soglia vera (D-69: lo standalone scrive sempre 7), lo stato con status_label/1 (com'è se non si conosce),
// l'oreficeria e l'artigiano per nome («Oreficeria», «🔧 Artigiano» se l'id non c'è), «🔍 testo»,
// il periodo «Campo: da → a» con «…»; «× Azzera filtro» a /. Senza filtri (o con la sola pagina o
// il solo data_campo) niente pillole.
func TestHomePillole(t *testing.T) {
	h, d, _ := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (1, 'Il negozio di prova'), (2, 'Oreficeria <Esempio>')`)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Bulino')`)
	eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('scadenza_giorni', '12', ?)`, istanteHome)
	b := titolareDentro(t, h)
	pillole := func(q string) string {
		corpo := b.vai("GET", "/?"+q, nil, nil).corpo
		i := strings.Index(corpo, `<div id="active-repair-filters"`)
		if i < 0 {
			return ""
		}
		var out []string
		for _, m := range regexp.MustCompile(`<span class="filter-pill [a-z-]+">([^<]*)</span>`).FindAllStringSubmatch(corpo[i:strings.Index(corpo[i:], "</div>")+i], -1) {
			out = append(out, m[1])
		}
		if !strings.Contains(corpo[i:], `<a id="clear-repair-filters" href="/" class="filter-pill pill-grigia">× Azzera filtro</a>`) {
			t.Errorf("%s: manca «× Azzera filtro»", q)
		}
		return strings.Join(out, " | ")
	}
	for q, voluto := range map[string]string{
		"": "", "p=2": "", "data_campo=pronta": "", "cerca=+": "", "oreficeria=1": "", "artigiano=x": "", "data_da=2026-02-30": "",
		"messaggi=1&notifiche=1&senza_qr=1&fuori_bottega=1&compro_oro=1&attesa=1&scadenza=1&stato=pronta&oreficeria=2&artigiano=1&cerca=%3Cb%3E&data_da=2026-01-01&data_campo=consegna": "⏰ In scadenza (12 giorni) | ⏳ In attesa cliente | 🥇 Compro oro | 📤 Fuori bottega | ⚠ Senza QR | 💬 Con messaggi | " +
			"🔔 Da leggere | Pronta | Oreficeria &lt;Esempio&gt; | 🔧 Bulino | 🔍 &lt;b&gt; | 📅 Consegna: 2026-01-01 → …",
		"stato=boh_x":                                            "boh_x",
		"stato=in_attesa_materiali":                              "In attesa materiali",
		"oreficeria=99&artigiano=99":                             "Oreficeria | 🔧 Artigiano",
		"data_a=%202026-03-01&data_campo=boh":                    "📅 Accettazione: … → 2026-03-01",
		"data_da=2026-01-01&data_a=2026-01-31&data_campo=pronta": "📅 Pronta: 2026-01-01 → 2026-01-31",
		"data_da=%2B2026-01-01&data_campo=consegnata":            "📅 Consegnata: 2026-01-01 → …",
		"notifiche=1":                                            "🔔 Da leggere",
	} {
		if g := pillole(q); g != voluto {
			t.Errorf("%q: %q, voluto %q", q, g, voluto)
		}
	}
	// D-69: la soglia letta dalla lista, anche quella riparata (fuori dai limiti, non un intero).
	// Con un giorno il singolare.
	for v, voluto := range map[string]string{"3": "3 giorni", "0": "1 giorno", "1": "1 giorno", "2": "2 giorni", "400": "365 giorni", "x": "7 giorni"} {
		eseguiSQL(t, d, `UPDATE impostazioni SET valore = ? WHERE chiave = 'scadenza_giorni'`, v)
		if g := pillole("scadenza=1"); g != "⏰ In scadenza ("+voluto+")" {
			t.Errorf("soglia %q: %q", v, g)
		}
	}
}

// inventario: RIP-H-03, RIP-H-04, RIP-H-05, RIP-H-06, RIP-H-56
// La ricerca e il cassetto. La ricerca è il campo unico (D-63, slice 052): un POST a /cerca-qr con il
// CSRF, «🔍» prima (il pulsante dell'Invio), il campo cerca, «📷» con via=qr, e gli altri filtri come
// campi nascosti (filter_query/3 senza cerca, mai la pagina), prima di cerca come lo standalone; il
// token sta fuori dal modulo, legato con form=. Il cassetto è un GET a /cerca (lo smistamento) e porta attesa, scadenza e notifiche
// nascosti (slice 214) e ha il resto come campi: cerca, gli stati di tutte e due le famiglie, le
// oreficerie esterne e gli artigiani solo se ce ne sono, il campo del periodo (accettazione se non
// si sceglie), le due date, le quattro caselle; «Applica» e «Reset».
func TestHomeRicercaECassetto(t *testing.T) {
	h, d, _ := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := titolareDentro(t, h)
	corpo := b.vai("GET", "/", nil, nil).corpo
	contiene(t, "vuota", corpo, `<form id="repair-search-form" method="post" action="/cerca-qr" class="gs-ricerca">
<div class="gs-ricerca-riga">
<button id="repair-search-submit" type="submit" class="gs-ricerca-cerca" title="Cerca" aria-label="Cerca">🔍</button>
<input id="repair-search-code" class="gs-campo" type="search" name="cerca" value="" placeholder="Cerca nome, n° busta o QR…" aria-label="Cerca nome, n° busta o QR" autocomplete="off" enterkeyhint="search" data-seleziona-tutto>
<button id="qr-lookup-scan" type="submit" name="via" value="qr" data-target="repair-search-code" data-submit="repair-search-form" class="gs-ricerca-qr" title="Scansiona QR busta" aria-label="Scansiona QR busta">📷</button>
</div>
<small id="repair-search-hint" data-cifre="Cerca in: cliente, n° busta, descrizione, note, note interne, QR" data-testo="Cerca in: cliente, n° busta, descrizione, note, note interne, telefono, QR">Cerca in: cliente, n° busta, descrizione, note, note interne, telefono, QR</small>
</form>
<input id="repair-search-token" name="_csrf_token" type="hidden" value="`+b.token+`" form="repair-search-form">
`, `<details id="repair-filter-drawer" class="gs-cassetto">
<summary><span>🔍 Filtri e ricerca</span><span aria-hidden="true">⌄</span></summary>
<form id="repair-filter-form" method="get" action="/cerca" class="gs-cassetto-modulo">
<label class="gs-largo">`, `<option value="">Tutti</option>
<option value="in_lavorazione">In lavorazione</option>
<option value="preventivo">Preventivo</option>
<option value="pronta">Pronta</option>
<option value="consegnata">Consegnata</option>
<option value="in_attesa_materiali">In attesa materiali</option>
<option value="annullata">Annullata</option>
<option value="in_valutazione">In valutazione</option>
<option value="valutata">Valutata</option>
<option value="perfezionata">Perfezionata</option>
<option value="chiusa">Chiusa</option>
</select>`, `<select class="gs-campo" name="data_campo">
<option value="accettazione" selected>Accettazione</option>
<option value="pronta">Pronta</option>
<option value="consegna">Consegna prevista</option>
<option value="consegnata">Consegnata (effettiva)</option>
</select>`, `<input class="gs-campo" type="date" name="data_da" value="">`, `<input type="checkbox" name="compro_oro" value="1">`,
		`<button type="submit" class="gs-btn-applica">Applica</button>
<a href="/" class="pulsante-contorno">Reset</a>`, `<a id="repair-report-pdf" href="/repairs/report.pdf" target="_blank" class="filter-pill pill-bianca">⧉ Schede PDF</a>`)
	nonContiene(t, "vuota", corpo, `name="oreficeria"`, `name="artigiano"`, `active-repair-filters`)
	if modulo := corpo[strings.Index(corpo, `<form id="repair-search-form"`):strings.Index(corpo, `<details id="repair-filter-drawer"`)]; strings.Count(modulo, `<input type="hidden"`) != 0 {
		t.Errorf("campi nascosti della ricerca senza filtri\n%s", modulo)
	}
	// Il campo unico non c'è su /nuova (D-63).
	nonContiene(t, "nuova", b.vai("GET", "/nuova", nil, nil).corpo, "repair-search-form", "qr-lookup-scan", "/cerca-qr")
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (1, 'Il negozio di prova'), (3, 'Zeta'), (2, 'Alfa')`)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Bulino'), (2, 'Bulino B')`)
	q := "p=2&cerca=a+%26b&stato=pronta&oreficeria=3&artigiano=%2B2&scadenza=1&compro_oro=1&attesa=1&fuori_bottega=1&senza_qr=1&messaggi=1&notifiche=1" +
		"&data_da=2026-01-01&data_a=2026-02-01&data_campo=consegnata&vista=galleria"
	corpo = b.vai("GET", "/?"+q, nil, nil).corpo
	tuttiTranneCerca := "stato=pronta&oreficeria=3&artigiano=2&scadenza=1&compro_oro=1&attesa=1&fuori_bottega=1&senza_qr=1&messaggi=1&notifiche=1" +
		"&data_da=2026-01-01&data_a=2026-02-01&data_campo=consegnata"
	var nascosti []string
	for _, x := range strings.Split(tuttiTranneCerca, "&") {
		k, v, _ := strings.Cut(x, "=")
		nascosti = append(nascosti, `<input type="hidden" name="`+k+`" value="`+v+`">`)
	}
	contiene(t, "ricerca", corpo, `<form id="repair-search-form" method="post" action="/cerca-qr" class="gs-ricerca">
`+strings.Join(nascosti, "\n")+`
<div class="gs-ricerca-riga">`, `<input id="repair-search-code" class="gs-campo" type="search" name="cerca" value="a &amp;b"`)
	contiene(t, "cassetto", corpo, `<form id="repair-filter-form" method="get" action="/cerca" class="gs-cassetto-modulo">
<input type="hidden" name="attesa" value="1">
<input type="hidden" name="scadenza" value="1">
<input type="hidden" name="notifiche" value="1">
<label class="gs-largo">
<span class="gs-etichetta">Cerca</span>
<input class="gs-campo" type="search" name="cerca" value="a &amp;b" placeholder="Nome cliente o n° busta…">`,
		`<option value="pronta" selected>Pronta</option>`, `<select class="gs-campo" name="oreficeria">
<option value="">Tutte</option>
<option value="2">Alfa</option>
<option value="3" selected>Zeta</option>
</select>`, `<select class="gs-campo" name="artigiano">
<option value="">Tutti</option>
<option value="1">Bulino</option>
<option value="2" selected>Bulino B</option>
</select>`, `<option value="consegnata" selected>Consegnata (effettiva)</option>`, `<option value="accettazione">Accettazione</option>`,
		`<input class="gs-campo" type="date" name="data_da" value="2026-01-01">`, `<input class="gs-campo" type="date" name="data_a" value="2026-02-01">`,
		`<input type="checkbox" name="compro_oro" value="1" checked>`, `<input type="checkbox" name="fuori_bottega" value="1" checked>`,
		`<input type="checkbox" name="senza_qr" value="1" checked>`, `<input type="checkbox" name="messaggi" value="1" checked>`,
		`<a id="repair-report-pdf" href="/repairs/report.pdf?cerca=a&#43;%26b&amp;`+strings.ReplaceAll(tuttiTranneCerca, "&", "&amp;")+`" target="_blank"`)
	nonContiene(t, "cassetto", corpo, `name="vista"`, `name="p"`)
	// Il cassetto di una lista con il solo stato non ha campi nascosti.
	corpo = b.vai("GET", "/?stato=pronta&compro_oro=1", nil, nil).corpo
	contiene(t, "stato", corpo, `<form id="repair-filter-form" method="get" action="/cerca" class="gs-cassetto-modulo">
<label class="gs-largo">`, `<input type="hidden" name="stato" value="pronta">
<input type="hidden" name="compro_oro" value="1">
<div class="gs-ricerca-riga">`)
}

// inventario: RIP-H-03, RIP-H-06, RIP-H-10
// I filtri del cassetto filtrano la home e passano nella paginazione: «10%» e «a_b» come scritti,
// «012/26» trova la 12/26; con un filtro qualsiasi le pronte nascoste tornano.
func TestHomeFiltriDelCassetto(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	sconto := bustaHome(t, d, o, "1/26", "Q", `descrizione = 'Sconto 10% sul lavoro'`)
	bustaHome(t, d, o, "2/26", "Q", `descrizione = 'Sconto 100 euro'`)
	ab := bustaHome(t, d, o, "3/26", "Q", `descrizione = 'Anello a_b'`)
	bustaHome(t, d, o, "4/26", "Q", `descrizione = 'Anello axb'`)
	dodici := bustaHome(t, d, o, "12/26", "Q", `stato = 'pronta'`)
	eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('home_mostra_pronte', '0', ?)`, istanteHome)
	b := titolareDentro(t, h)
	for q, voluti := range map[string][]int64{
		"cerca=10%25": {sconto}, "cerca=a_b": {ab}, "cerca=012/26": {dodici}, "cerca=%20012%2F26%20": {dodici}, "cerca=12/26": {dodici},
		"cerca=Cliente+12": {dodici}, "data_campo=accettazione": nil,
	} {
		var v []string
		for _, id := range voluti {
			v = append(v, strconv.FormatInt(id, 10))
		}
		got := ordineRiquadri(b.vai("GET", "/?"+q, nil, nil).corpo)
		if voluti == nil {
			if slices.Contains(got, strconv.FormatInt(dodici, 10)) || len(got) != 4 {
				t.Errorf("%s: %v", q, got)
			}
			continue
		}
		if strings.Join(got, ",") != strings.Join(v, ",") {
			t.Errorf("%s: %v, voluti %v", q, got, v)
		}
	}
	for i := 5; i <= 35; i++ {
		bustaHome(t, d, o, fmt.Sprintf("%d/26", i+100), "Q", `descrizione = 'Lavoro'`)
	}
	contiene(t, "pagine", b.vai("GET", "/?cerca=Lavoro&data_a=2026-12-31&notifiche=&oreficeria=1", nil, nil).corpo,
		`<a href="/?cerca=Lavoro&amp;data_a=2026-12-31&amp;data_campo=accettazione&amp;p=2" class="pulsante-contorno">Successiva →</a>`)
}

// inventario: RIP-H-11
// Le due 🤝 sono indipendenti: una busta con la sola accettazione non letta ha solo la verde, una
// con il solo rifiuto non letto solo la rossa; lette, nessuna.
func TestHomeRisposteAlPreventivo(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	acc := bustaHome(t, d, o, "1/26", "Q", `preventivo_accettato_il = ?`, istanteHome)
	rif := bustaHome(t, d, o, "2/26", "Q", `preventivo_rifiutato_il = ?`, istanteHome)
	lette := bustaHome(t, d, o, "3/26", "Q", `preventivo_accettato_il = ?, preventivo_accettato_letto_il = ?, preventivo_rifiutato_il = ?, preventivo_rifiutato_letto_il = ?`,
		istanteHome, istanteHome, istanteHome, istanteHome)
	corpo := titolareDentro(t, h).vai("GET", "/", nil, nil).corpo
	contiene(t, "accettato", riquadroBusta(t, corpo, acc), "repair-quote-approved-")
	nonContiene(t, "accettato", riquadroBusta(t, corpo, acc), "repair-quote-rejected-")
	contiene(t, "rifiutato", riquadroBusta(t, corpo, rif), "repair-quote-rejected-")
	nonContiene(t, "rifiutato", riquadroBusta(t, corpo, rif), "repair-quote-approved-")
	nonContiene(t, "lette", riquadroBusta(t, corpo, lette), "repair-quote-approved-", "repair-quote-rejected-")
}

// inventario: RIP-H-01
// «Nuova riparazione» ha la stessa intestazione della bottega della home (shop_header/1).
func TestNuovaConLaBottega(t *testing.T) {
	h, _, _ := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	contiene(t, "nuova", titolareDentro(t, h).vai("GET", "/nuova", nil, nil).corpo, `<p class="bottega-marchio">GestRip</p>`,
		`<h1 class="bottega-nome">`, `<p class="bottega-sotto">Banco riparazioni</p>`)
}

// inventario: RIP-H-06, RIP-H-21
// I parametri si leggono come Plug.Conn.Query.decode/1: «stato[=x» è un'altra chiave e non tocca lo
// stato; «stato[]=x» dopo lo stato lo rende una lista (niente filtro, niente chiave nel registro).
func TestHomeParametriComePlug(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	p := bustaHome(t, d, o, "1/26", "Q", `stato = 'pronta'`)
	l := bustaHome(t, d, o, "2/26", "Q", ``)
	b := titolareDentro(t, h)
	ultima := func() string {
		var dati string
		if err := d.Lettura.QueryRow(`SELECT dati FROM eventi_audit WHERE azione = 'repair.index_viewed' ORDER BY id DESC LIMIT 1`).Scan(&dati); err != nil {
			t.Fatal(err)
		}
		return dati
	}
	for q, c := range map[string]struct{ righe, chiavi string }{
		"stato=pronta&stato[=x":   {strconv.FormatInt(p, 10), `["stato"]`},
		"stato=pronta&stato[]=x":  {fmt.Sprintf("%d,%d", l, p), `[]`},
		"stato[]=x&stato=pronta":  {strconv.FormatInt(p, 10), `["stato"]`},
		"stato=pronta&stato[a]=x": {fmt.Sprintf("%d,%d", l, p), `[]`},
	} {
		if g := strings.Join(ordineRiquadri(b.vai("GET", "/?"+q, nil, nil).corpo), ","); g != c.righe {
			t.Errorf("%s: righe %s, volute %s", q, g, c.righe)
		}
		if g := ultima(); g != `{"filter_keys":`+c.chiavi+`}` {
			t.Errorf("%s: registro %s", q, g)
		}
	}
}
