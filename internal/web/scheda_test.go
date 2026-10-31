package web

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La scheda della busta in sola lettura (/repairs/{id}, RIP-S-01, RIP-S-02, RIP-S-18), come
// RepairLive.mount/3 e render/1 dello standalone (go-baseline), su account e buste veri. I casi da
// repair_live_test.exs e repair_quote_live_test.exs.

// bustaScheda crea una busta come il setup di repair_live_test.exs: cliente «Cliente storico»
// con il telefono «333 111», due righe di lavorazione, le due note, preventivo di 12345 centesimi
// senza testo (estimate_cents del setup; dal modulo il testo resterebbe e vincerebbe), consegna il
// 20/09/2026. Dà l'id.
func bustaScheda(t *testing.T, d *db.DB, o clock.Orologio, f func(*buste.Accettazione)) int64 {
	t.Helper()
	a := buste.Accettazione{
		NomeCliente: "Cliente storico", TelefonoCliente: "333 111", DataAccettazione: "2026-09-05",
		DataPrevista: "2026-09-20", Descrizione: "Anello\nSaldatura", NotePubbliche: "Nota pubblica",
		NoteInterne: "Nota riservata", Preventivo: "123,45", CodiceQR: "QR-STORICO",
	}
	if f != nil {
		f(&a)
	}
	c, err := buste.Nuove(d, o).Crea(context.Background(), a, audit.Contesto{Attore: "titolare@esempio.it"})
	if err != nil {
		t.Fatal(err)
	}
	eseguiSQL(t, d, `UPDATE buste SET preventivo_testo = NULL WHERE id = ?`, c.ID)
	return c.ID
}

func percorsoScheda(id int64) string { return "/repairs/" + strconv.FormatInt(id, 10) }

// inventario: RIP-S-01
// La scheda si apre per titolare e dipendente con il titolo «Busta #<numero>» (anche quello della
// finestra, con « · Laboratorio»), «← Indietro» verso / (la lista con i filtri usati è T4.2) e il
// guscio interno; ogni apertura scrive repair.viewed con l'attore di chi apre.
func TestSchedaApertura(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	for i, c := range []struct {
		b     *browser
		email string
	}{{titolareDentro(t, h), "titolare@esempio.it"}, {dipendenteDentro(t, h), "banco@esempio.it"}} {
		r := c.b.vai("GET", percorsoScheda(id), nil, nil)
		if r.codice != 200 {
			t.Fatalf("%s: %d %q", c.email, r.codice, r.location)
		}
		contiene(t, c.email, r.corpo,
			"<title>Busta #1/26 · Laboratorio</title>",
			`<nav class="gs-back-bar" aria-label="Ritorno"><a id="repair-back-list" href="/" class="gs-back-btn">← Indietro</a></nav>`,
			`<h1 class="titolo-busta">Busta #1/26</h1>`,
			`<section id="repair-detail" class="riquadro">`, "<h2>Dati riparazione</h2>",
			`id="app-mobile-navigation"`, `id="fab-home"`)
		nonContiene(t, c.email, r.corpo, `id="repair-compro-oro"`, "🪙 Compro Oro", `id="repair-form"`, `id="edit-repair"`)
		if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed' AND attore = '`+c.email+`' AND id_risorsa = '`+strconv.FormatInt(id, 10)+`'`); n != 1 {
			t.Errorf("%s: repair.viewed %d", c.email, n)
		}
		if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`); n != i+1 {
			t.Errorf("repair.viewed in tutto %d", n)
		}
	}
}

// inventario: RIP-S-01
// Una busta che non c'è, un id malformato (come Workshop.get_repair/1: nil, quindi «non
// trovata», anche se non è un numero) e una busta nel cestino tornano alla home con «Riparazione
// non trovata.», senza dire niente della busta e senza audit.
func TestSchedaNonTrovata(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	cestinata := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `UPDATE buste SET cancellata_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, cestinata)
	b := titolareDentro(t, h)
	prima := contaRighe(t, d, `SELECT count(*) FROM eventi_audit`)
	for _, p := range []string{"/repairs/999999", "/repairs/invalid", "/repairs/0", "/repairs/-1",
		"/repairs/99999999999999999999999999999", percorsoScheda(cestinata)} {
		r := b.vai("GET", p, nil, nil)
		if !redirectA(r, "/") {
			t.Errorf("%s: %d %q", p, r.codice, r.location)
			continue
		}
		r = b.vai("GET", "/", nil, nil)
		contiene(t, p, r.corpo, `<div role="alert" class="flash flash-errore">Riparazione non trovata.</div>`)
		nonContiene(t, p, r.corpo, "Cliente storico", "Busta #")
	}
	if dopo := contaRighe(t, d, `SELECT count(*) FROM eventi_audit`); dopo != prima {
		t.Errorf("righe d'audit: %d", dopo-prima)
	}
}

// inventario: RIP-S-01
// Se l'apertura non si può registrare, la busta non si apre: home con «Impossibile aprire la
// riparazione.».
func TestSchedaNonAperta(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `CREATE TEMP TRIGGER no_visto BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.viewed' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	b := titolareDentro(t, h)
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	if !redirectA(r, "/") {
		t.Fatalf("%d %q", r.codice, r.location)
	}
	r = b.vai("GET", "/", nil, nil)
	contiene(t, "home", r.corpo, `<div role="alert" class="flash flash-errore">Impossibile aprire la riparazione.</div>`)
	nonContiene(t, "home", r.corpo, "Cliente storico")
}

// inventario: RIP-S-01
// Senza accesso, con l'account disattivato o con la sessione revocata («chiudi tutte») si va al
// login: niente lettura, niente audit.
func TestSchedaSenzaAccesso(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	p := percorsoScheda(id)
	if r := nuovoBrowser(t, h).vai("GET", p, nil, nil); !redirectA(r, "/login") {
		t.Errorf("anonimo: %d %q", r.codice, r.location)
	}
	disattivato := dipendenteDentro(t, h)
	revocato := titolareDentro(t, h)
	eseguiSQL(t, d, `UPDATE utenti SET attivo = 0 WHERE email = 'banco@esempio.it'`)
	eseguiSQL(t, d, `UPDATE utenti SET versione_sessione = versione_sessione + 1 WHERE email = 'titolare@esempio.it'`)
	for nome, b := range map[string]*browser{"disattivato": disattivato, "revocato": revocato} {
		if r := b.vai("GET", p, nil, nil); !redirectA(r, "/login") {
			t.Errorf("%s: %d %q", nome, r.codice, r.location)
		}
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`); n != 0 {
		t.Errorf("repair.viewed senza accesso: %d", n)
	}
}

// inventario: RIP-S-01
// Aprire la scheda legge la conversazione una volta (il segno e l'audit solo se cambia), come
// «opening the detail marks the thread read once and audits it»; e le risposte al preventivo.
func TestSchedaSegnaLetto(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `UPDATE buste SET ultimo_messaggio_il = '2026-10-30T08:00:00.000000Z', preventivo_rifiutato_il = '2026-10-30T08:10:00.000000Z' WHERE id = ?`, id)
	b := dipendenteDentro(t, h)
	for range 2 {
		if r := b.vai("GET", percorsoScheda(id), nil, nil); r.codice != 200 {
			t.Fatalf("%d", r.codice)
		}
		if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.messages_read' AND attore = 'banco@esempio.it'`); n != 1 {
			t.Errorf("repair.messages_read %d", n)
		}
		if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.quote_read'`); n != 1 {
			t.Errorf("repair.quote_read %d", n)
		}
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM buste WHERE messaggi_letti_il = ultimo_messaggio_il AND preventivo_rifiutato_letto_il = preventivo_rifiutato_il`); n != 1 {
		t.Error("segni letti non scritti")
	}
}

// righeDati sono le etichette del riquadro, nell'ordine dello standalone.
var righeDati = []string{"<dt>Data</dt>", "<dt>Cliente</dt>", "<dt>Telefono</dt>", "<dt>Stato</dt>", "<dt>Oggetti e lavorazioni</dt>",
	"<dt>Note (visibili al cliente)</dt>", "<dt>Note interne</dt>", "<dt>Artigiano</dt>", "<dt>Preventivo</dt>", "<dt>Prezzo finale</dt>",
	"<dt>Oreficeria cliente</dt>", "<dt>Consegna entro</dt>", "<dt>Pronta dal</dt>", "<dt>Consegnata il</dt>", "<dt>Registrata il</dt>",
	"<dt>Codice QR busta</dt>"}

// inventario: RIP-S-02
// Il riquadro «Dati riparazione» con tutte le righe, nell'ordine e con i testi dello standalone:
// date gg/mm/aaaa, il telefono del cliente da chiamare su una busta diretta (nessuna riga «cliente
// finale»), lo stato in parole, i centesimi «€ 123,45», «-» senza prezzo, «Diretta <negozio>»,
// «Registrata il» sull'ora del negozio, il QR; il segno «🪙 Compro Oro» nel titolo.
func TestSchedaDatiRiparazione(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Bulino')`)
	id := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.ArtigianoID = "1" })
	eseguiSQL(t, d, `UPDATE buste SET stato = 'consegnata', data_pronta = '2026-09-18', data_consegna = '2026-09-21',
		creato_il = '2026-01-15T08:05:00.000000Z' WHERE id = ?`, id)
	b := titolareDentro(t, h)
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	ultimo := -1
	for _, e := range righeDati {
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
	contiene(t, "dati", r.corpo,
		"<dt>Data</dt><dd>05/09/2026</dd>",
		"<dt>Cliente</dt><dd>Cliente storico</dd>",
		`<dt>Telefono</dt><dd id="repair-recipient"><a href="tel:333 111">333 111</a></dd>`,
		`<dt>Stato</dt><dd id="repair-status" class="forte">Consegnata</dd>`,
		`<dt>Artigiano</dt><dd id="repair-artisan">Bulino</dd>`,
		`<dt>Preventivo</dt><dd id="repair-estimate">€ 123,45</dd>`,
		`<dt>Prezzo finale</dt><dd id="repair-final-price" class="grassetto">-</dd>`,
		`<div id="repair-oreficeria"><dt>Oreficeria cliente</dt><dd><span>Diretta Negozio</span></dd></div>`,
		`<dt>Consegna entro</dt><dd id="repair-delivery">20/09/2026</dd>`,
		`<dt>Pronta dal</dt><dd id="repair-ready-on">18/09/2026</dd>`,
		`<dt>Consegnata il</dt><dd id="repair-delivered-on">21/09/2026</dd>`,
		// gennaio: Roma è UTC+1, le 08:05 UTC sono le 09:05 in negozio
		"<dt>Registrata il</dt><dd>15/01/2026 09:05</dd>",
		`<dt>Codice QR busta</dt><dd id="repair-qr-code" class="mono">QR-STORICO</dd>`)
	// Il prezzo finale in grassetto (font-bold dello standalone), lo stato in semigrassetto.
	css := string(leggiStatico("app.css"))
	contiene(t, "app.css", css, ".grassetto { font-weight: 700; }", ".forte { font-weight: 600; }")
	nonContiene(t, "dati", r.corpo, "15/01/2026 08:05", `id="repair-customer-phone"`, "Solo anagrafica", `id="repair-estimate-answer"`,
		"Togli data", "Stima tempi", "Fuori bottega")

	// Senza artigiano, senza QR, senza note, aperta, compro oro: le righe facoltative spariscono,
	// i vuoti sono «—» o «-».
	eseguiSQL(t, d, `UPDATE buste SET stato = 'in_valutazione', compro_oro = 1, artigiano_id = NULL, codice_qr = NULL, note_pubbliche = NULL,
		note_interne = NULL, data_pronta = NULL, data_consegna = NULL, data_prevista = NULL, preventivo_centesimi = NULL, preventivo_testo = NULL WHERE id = ?`, id)
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "vuoti", r.corpo,
		`<h1 class="titolo-busta">Busta #1/26`+"\n"+`<span id="repair-compro-oro" class="badge-compro-oro">🪙 Compro Oro</span></h1>`,
		`<dd id="repair-status" class="forte">In valutazione</dd>`,
		`<dd id="repair-artisan">—</dd>`, `<dd id="repair-estimate">-</dd>`, `<dd id="repair-delivery">-</dd>`,
		`<dd id="repair-qr-code" class="mono">—</dd>`)
	nonContiene(t, "vuoti", r.corpo, "<dt>Note (visibili al cliente)</dt>", "<dt>Note interne</dt>", "<dt>Pronta dal</dt>", "<dt>Consegnata il</dt>")
	// Gli stati con più parole e il QR scritto vuoto (dall'import): resta vuoto, non «—».
	eseguiSQL(t, d, `UPDATE buste SET stato = 'in_attesa_materiali', compro_oro = 0, codice_qr = '' WHERE id = ?`, id)
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "stato", r.corpo, `<dd id="repair-status" class="forte">In attesa materiali</dd>`, `<dd id="repair-qr-code" class="mono"></dd>`)
	// Il nome del negozio dal profilo.
	eseguiSQL(t, d, `INSERT INTO profilo_negozio (nome, condizioni_garanzia, creato_il, aggiornato_il) VALUES ('Oreficeria del Corso', '', '2026-01-01T00:00:00.000000Z', '2026-01-01T00:00:00.000000Z')`)
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "negozio", r.corpo, "<span>Diretta Oreficeria del Corso</span>")
}

// inventario: RIP-S-02
// Prezzi e consegna (Repair.price_source/2, date_source/2): il testo scritto a mano vince, così
// com'è (anche sul prezzo finale, senza «€»), sui centesimi e sulla data; poi i centesimi «€ 0,00»
// e la data; poi «-».
func TestSchedaTestoVince(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `UPDATE buste SET preventivo_testo = '400/500', preventivo_centesimi = 40000, prezzo_testo = 'da definire <b>',
		prezzo_centesimi = 1, data_prevista_testo = 'fine mese', data_prevista = '2026-09-20' WHERE id = ?`, id)
	b := titolareDentro(t, h)
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "testi", r.corpo, `<dd id="repair-estimate">400/500</dd>`,
		`<dd id="repair-final-price" class="grassetto">da definire &lt;b&gt;</dd>`, `<dd id="repair-delivery">fine mese</dd>`)
	eseguiSQL(t, d, `UPDATE buste SET preventivo_testo = NULL, prezzo_testo = NULL, prezzo_centesimi = 0, data_prevista_testo = NULL WHERE id = ?`, id)
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "centesimi", r.corpo, `<dd id="repair-estimate">€ 400,00</dd>`,
		`<dd id="repair-final-price" class="grassetto">€ 0,00</dd>`, `<dd id="repair-delivery">20/09/2026</dd>`)
	// Un testo vuoto non vince (price_source/2 vuole un testo non vuoto).
	eseguiSQL(t, d, `UPDATE buste SET preventivo_testo = '', data_prevista_testo = '' WHERE id = ?`, id)
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "testo vuoto", r.corpo, `<dd id="repair-estimate">€ 400,00</dd>`, `<dd id="repair-delivery">20/09/2026</dd>`)
}

// inventario: RIP-S-02
// Il telefono è l'unico destinatario della busta (Workshop.Recipient): su una busta B2B quello
// dell'oreficeria con il suo nome, mai quello del cliente finale, che resta come «Solo
// anagrafica» senza link; senza recapito lo si dice, senza ripiegare sull'altro.
func TestSchedaDestinatario(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (1, 'Il negozio di prova')`)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome, telefono) VALUES (2, 'Gioielleria Rossi', '0423555000')`)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome, telefono) VALUES (3, 'Senza recapito', '  ')`)
	id := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.OreficeriaID = "2" })
	b := titolareDentro(t, h)
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "B2B", r.corpo,
		`<dd id="repair-recipient"><a href="tel:0423555000">0423555000</a>`+"\n"+`<span class="sotto">Oreficeria Gioielleria Rossi</span></dd>`,
		`<dt>Telefono cliente finale</dt><dd id="repair-customer-phone"><span>333 111</span>`+"\n"+`<span class="sotto">Solo anagrafica: su busta B2B si scrive all'oreficeria.</span></dd>`,
		"<span>Gioielleria Rossi</span>")
	nonContiene(t, "B2B", r.corpo, "tel:333")
	eseguiSQL(t, d, `UPDATE buste SET oreficeria_id = 3, telefono_cliente = '' WHERE id = ?`, id)
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "B2B senza recapito", r.corpo,
		`<dd id="repair-recipient"><span class="sbiadito">— Nessun recapito per l&#39;oreficeria: nessun canale può partire.</span></dd>`,
		`<dd id="repair-customer-phone"><span>—</span>`)
	nonContiene(t, "B2B senza recapito", r.corpo, `href="tel:`)
	// La riga 1 non è B2B: il cliente, e il nome dell'oreficeria come oreficeria cliente.
	eseguiSQL(t, d, `UPDATE buste SET oreficeria_id = 1, telefono_cliente = ' 333 222 ' WHERE id = ?`, id)
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "riga 1", r.corpo, `<dd id="repair-recipient"><a href="tel:333 222">333 222</a></dd>`, "<span>Il negozio di prova</span>")
	nonContiene(t, "riga 1", r.corpo, `id="repair-customer-phone"`, "Oreficeria Il negozio")
	// Il telefono nell'href com'è (Phoenix: «tel:333 111», non «%20»), scappato come attributo: è un
	// dato scritto da chi lavora.
	eseguiSQL(t, d, `UPDATE buste SET oreficeria_id = NULL, telefono_cliente = ? WHERE id = ?`, `33"3<x>&'1`, id)
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "telefono da scappare", r.corpo, `<dd id="repair-recipient"><a href="tel:33&#34;3&lt;x&gt;&amp;&#39;1">33&#34;3&lt;x&gt;&amp;&#39;1</a></dd>`)
	nonContiene(t, "telefono da scappare", r.corpo, `33"3<x>`)
	eseguiSQL(t, d, `UPDATE buste SET oreficeria_id = NULL, telefono_cliente = '' WHERE id = ?`, id)
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "diretta senza recapito", r.corpo,
		`<dd id="repair-recipient"><span class="sbiadito">— Nessun recapito per il cliente: nessun canale può partire.</span></dd>`)
}

// inventario: RIP-S-02
// Sotto il preventivo la risposta del cliente, sull'ora del negozio: «✅ ACCETTATO dal cliente il
// …» o «❌ RIFIUTATO dal cliente il …» (QuoteDecision.state/1: l'accettazione vince).
func TestSchedaRispostaPreventivo(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	eseguiSQL(t, d, `UPDATE buste SET preventivo_accettato_il = '2026-07-15T08:05:00.000000Z' WHERE id = ?`, id)
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "accettato", r.corpo, `<dd id="repair-estimate">€ 123,45`+"\n"+
		`<span id="repair-estimate-answer" class="estimate-answer-approved">✅ ACCETTATO dal cliente il 15/07/2026 10:05</span></dd>`)
	eseguiSQL(t, d, `UPDATE buste SET preventivo_rifiutato_il = '2026-01-15T08:05:00.000000Z' WHERE id = ?`, id)
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	nonContiene(t, "tutte e due", r.corpo, "RIFIUTATO")
	eseguiSQL(t, d, `UPDATE buste SET preventivo_accettato_il = NULL WHERE id = ?`, id)
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "rifiutato", r.corpo, `<dd id="repair-estimate">€ 123,45`+"\n"+
		`<span id="repair-estimate-answer" class="estimate-answer-rejected">❌ RIFIUTATO dal cliente il 15/01/2026 09:05</span></dd>`)
	nonContiene(t, "rifiutato", r.corpo, "ACCETTATO")
}

// inventario: RIP-S-02, RIP-S-18
// I tre riquadri di testo tengono il testo contro i tag: nessuna riga vuota in testa o in coda
// (slice 159), come «the sheet's three text boxes carry no blank line around their text».
func TestSchedaRiquadriSenzaRigheVuote(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	r := titolareDentro(t, h).vai("GET", percorsoScheda(id), nil, nil)
	for _, testo := range []string{"Anello\nSaldatura", "Nota pubblica", "Nota riservata"} {
		if !regexp.MustCompile(`<dd class="whitespace-pre-wrap"[^>]*>` + testo + `</dd>`).MatchString(r.corpo) {
			t.Errorf("riquadro di %q con righe vuote o mancante", testo)
		}
	}
}

// inventario: RIP-S-18
// Le note visibili al cliente e quelle interne sono due righe distinte (le interne nel riquadro
// grigio), ognuna solo se c'è (una nota scritta vuota dall'import c'è); i testi si escapano
// sempre, nessun HTML eseguito.
func TestSchedaNote(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, func(a *buste.Accettazione) {
		a.Descrizione, a.NotePubbliche, a.NoteInterne = "<script>alert(1)</script>", "Ritiro <sabato> & \"presto\"", "<img src=x onerror=alert(2)>"
		a.NomeCliente = "Ada <i>Esempio</i>"
	})
	b := titolareDentro(t, h)
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "note", r.corpo,
		`<div class="largo"><dt>Oggetti e lavorazioni</dt><dd class="whitespace-pre-wrap">&lt;script&gt;alert(1)&lt;/script&gt;</dd></div>`,
		`<div class="largo"><dt>Note (visibili al cliente)</dt><dd class="whitespace-pre-wrap">Ritiro &lt;sabato&gt; &amp; &#34;presto&#34;</dd></div>`,
		`<div class="largo note-interne"><dt>Note interne</dt><dd class="whitespace-pre-wrap">&lt;img src=x onerror=alert(2)&gt;</dd></div>`,
		"<dd>Ada &lt;i&gt;Esempio&lt;/i&gt;</dd>")
	nonContiene(t, "note", r.corpo, "<script>alert", "<img src=x", "<i>Esempio")
	eseguiSQL(t, d, `UPDATE buste SET note_pubbliche = '', note_interne = NULL WHERE id = ?`, id)
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "nota vuota", r.corpo, `<dt>Note (visibili al cliente)</dt><dd class="whitespace-pre-wrap"></dd>`)
	nonContiene(t, "nota vuota", r.corpo, "<dt>Note interne</dt>")
	eseguiSQL(t, d, `UPDATE buste SET note_pubbliche = NULL, note_interne = '' WHERE id = ?`, id)
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "interna vuota", r.corpo, `<dt>Note interne</dt><dd class="whitespace-pre-wrap"></dd>`)
	nonContiene(t, "interna vuota", r.corpo, "<dt>Note (visibili al cliente)</dt>")
}
