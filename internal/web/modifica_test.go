package web

import (
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/db"
)

// La modifica della busta dalla scheda (RIP-S-04…12), come gli eventi edit, save, cancel e
// clear-expected-on di RepairLive e open_edit_from_query/3 dello standalone (go-baseline), su
// account e buste veri. I casi da repair_live_test.exs, repair_sheet_ux_live_test.exs e
// repair_kind_live_test.exs.

func percorsoModifica(id int64) string { return percorsoScheda(id) + "?modifica=1" }
func azioneModifica(id int64) string   { return percorsoScheda(id) + "/modifica" }

var (
	reModulo    = regexp.MustCompile(`(?s)<form id="repair-form"[^>]*>.*?</form>`)
	reInput     = regexp.MustCompile(`<input [^>]*>`)
	reTextarea  = regexp.MustCompile(`(?s)<textarea [^>]*name="([^"]+)"[^>]*>\n?(.*?)</textarea>`)
	reSelect    = regexp.MustCompile(`(?s)<select [^>]*name="([^"]+)"[^>]*>(.*?)</select>`)
	reOpzione   = regexp.MustCompile(`<option value="([^"]*)"( selected)?>`)
	reAttributo = regexp.MustCompile(`\b(name|value|type)="([^"]*)"`)
)

// moduloDi è il modulo di modifica della pagina come lo manda il browser: i campi non disabilitati,
// la casella solo se segnata, il primo «— …» di un menu senza voce scelta.
func moduloDi(t *testing.T, corpo string) url.Values {
	t.Helper()
	f := reModulo.FindString(corpo)
	if f == "" {
		t.Fatal("manca il modulo di modifica")
	}
	v := url.Values{}
	for _, in := range reInput.FindAllString(f, -1) {
		if strings.Contains(in, " disabled") {
			continue
		}
		a := map[string]string{}
		for _, m := range reAttributo.FindAllStringSubmatch(in, -1) {
			a[m[1]] = unescapeHTML(m[2])
		}
		if a["type"] == "checkbox" && !strings.Contains(in, " checked") {
			continue
		}
		v.Add(a["name"], a["value"])
	}
	for _, m := range reTextarea.FindAllStringSubmatch(f, -1) {
		v.Add(m[1], unescapeHTML(m[2]))
	}
	for _, m := range reSelect.FindAllStringSubmatch(f, -1) {
		oo := reOpzione.FindAllStringSubmatch(m[2], -1)
		scelta := oo[0][1]
		for _, o := range oo {
			if o[2] != "" {
				scelta = o[1]
			}
		}
		v.Add(m[1], scelta)
	}
	return v
}

func unescapeHTML(s string) string {
	return strings.NewReplacer("&#34;", `"`, "&quot;", `"`, "&#39;", "'", "&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(s)
}

// salva apre il modulo, cambia i campi dati e lo manda.
func salva(t *testing.T, b *browser, id int64, campi map[string]string) risposta {
	t.Helper()
	r := b.vai("GET", percorsoModifica(id), nil, nil)
	v := moduloDi(t, r.corpo)
	for k, val := range campi {
		v.Set(k, val)
	}
	return b.vai("POST", azioneModifica(id), v, nil)
}

func colonna(t *testing.T, d *db.DB, id int64, c string) string {
	t.Helper()
	var s string
	if err := d.Lettura.QueryRow(`SELECT coalesce(CAST(`+c+` AS TEXT), '∅') FROM buste WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// campiModuloInOrdine sono le etichette del modulo, nell'ordine dello standalone.
var campiModuloInOrdine = []string{"Nome cliente", "Telefono", ">Data<", "Oggetti e lavorazioni", "Note (visibili al cliente)",
	"Note interne (non visibili)", "Preventivo", "Prezzo finale", "Consegna entro", "Giorni in più (stima tempi)", "Codice QR busta",
	"Artigiano", "🪙 Scheda Compro Oro", "Salva modifiche", "Annulla"}

// inventario: RIP-S-04
// «Modifica» nella scheda apre il modulo «Modifica riparazione» al posto del riquadro dei dati, per
// titolare e dipendente: i campi dello standalone nel suo ordine, i valori della busta (i
// centesimi senza testo come «123.45»), nessun campo per stato, numero, date di pronta e
// consegna o centesimi; solo nomi dell'elenco fisso (@edit_fields); nessun cursore spostato.
func TestModificaApreIlModulo(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `UPDATE buste SET prezzo_centesimi = 0, giorni_in_piu = 4 WHERE id = ?`, id)
	for _, b := range []*browser{titolareDentro(t, h), dipendenteDentro(t, h)} {
		r := b.vai("GET", percorsoScheda(id), nil, nil)
		contiene(t, "scheda", r.corpo, `<a id="edit-repair" href="/repairs/`+strconv.FormatInt(id, 10)+`?modifica=1" class="pulsante">Modifica</a>`)
		r = b.vai("GET", percorsoModifica(id), nil, nil)
		if r.codice != 200 {
			t.Fatalf("%d %q", r.codice, r.location)
		}
		contiene(t, "modulo", r.corpo, `<form id="repair-form" action="/repairs/`+strconv.FormatInt(id, 10)+`/modifica" method="post"`,
			"<h2>Modifica riparazione</h2>", `<title>Busta #1/26 · Laboratorio</title>`,
			`name="repair[customer_name_snapshot]" id="repair_customer_name_snapshot" value="Cliente storico" maxlength="160" autocomplete="off" required>`,
			`name="repair[customer_phone_snapshot]" id="repair_customer_phone_snapshot" value="333 111" maxlength="40" autocomplete="off">`,
			`name="repair[accepted_on]" id="repair_accepted_on" value="2026-09-05" required>`,
			"name=\"repair[description]\" rows=\"4\" required>\nAnello\nSaldatura</textarea>",
			"name=\"repair[public_notes]\" rows=\"2\">\nNota pubblica</textarea>",
			"name=\"repair[internal_notes]\" rows=\"2\">\nNota riservata</textarea>",
			`name="repair[estimate_euros]" id="repair_estimate_euros" value="123.45" placeholder="es. 120 oppure 400/500">`,
			`name="repair[final_price_euros]" id="repair_final_price_euros" value="0.00" placeholder="es. 120 oppure da definire">`,
			`name="repair[expected_on]" id="repair_expected_on" value="2026-09-20">`,
			`<button id="repair-expected-clear" type="button" data-clear-field="repair_expected_on repair_expected_on_text">Togli data</button>`,
			`name="repair[extra_days]" id="repair_extra_days" value="4" min="0" max="90">`,
			`name="repair[qr_code]" id="repair_qr_code" value="QR-STORICO" placeholder="Scansiona o inserisci manualmente" autocomplete="off">`,
			`<input type="hidden" name="repair[compro_oro]" value="false">`,
			`<input id="repair-compro-oro-box" type="checkbox" name="repair[compro_oro]" value="true">`,
			`<input name="tipo_mostrato" type="hidden" value="false">`,
			`<button id="save-repair" type="submit">Salva modifiche</button>`,
			`<a id="cancel-repair" href="/repairs/`+strconv.FormatInt(id, 10)+`" class="pulsante-secondario">Annulla</a>`)
		nonContiene(t, "modulo", r.corpo, `id="repair-detail"`, `id="edit-repair"`, "autofocus", `id="repair-expected-text"`,
			`name="repair[expected_on_text]"`, `id="repair-phone-locked"`, `id="repair-kind-hint"`, "repair[status]",
			"repair[bag_number]", "_cents", "repair[ready_on]", `id="repair_oreficeria_id"`)
		ultimo := -1
		for _, e := range campiModuloInOrdine {
			i := strings.Index(r.corpo, e)
			if i < ultimo || i < 0 {
				t.Errorf("%q fuori posto (%d)", e, i)
			}
			ultimo = i
		}
		modulo := reModulo.FindString(r.corpo)
		for _, m := range regexp.MustCompile(`name="repair\[([a-z_]+)\]"`).FindAllStringSubmatch(modulo, -1) {
			if !slices.Contains(campiModifica, m[1]) {
				t.Errorf("campo %q fuori dall'elenco", m[1])
			}
		}
	}
	// Aprire il modulo è aprire la scheda: una riga repair.viewed per GET (D-44).
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`); n != 4 {
		t.Errorf("repair.viewed %d", n)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.updated'`); n != 0 {
		t.Errorf("repair.updated %d", n)
	}
}

// inventario: RIP-S-04
// «Salva modifiche» salva i campi del modulo e torna alla scheda con «Riparazione aggiornata.»;
// «Annulla» torna alla scheda senza salvare. Come «edit save persists all mutable fields and
// survives reload; cancel does not save».
func TestModificaSalva(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := dipendenteDentro(t, h)
	r := salva(t, b, id, map[string]string{"repair[description]": "Collana\nLucidatura", "repair[public_notes]": "Pubblica aggiornata",
		"repair[internal_notes]": "Interna aggiornata", "repair[accepted_on]": "2026-09-04", "repair[expected_on]": "2026-09-30",
		"repair[estimate_euros]": "124,56", "repair[final_price_euros]": "0.01"})
	if !redirectA(r, percorsoScheda(id)) {
		t.Fatalf("%d %q", r.codice, r.location)
	}
	for c, v := range map[string]string{"descrizione": "Collana\nLucidatura", "preventivo_centesimi": "12456", "prezzo_centesimi": "1",
		"data_accettazione": "2026-09-04", "data_prevista": "2026-09-30", "note_pubbliche": "Pubblica aggiornata",
		"note_interne": "Interna aggiornata", "nome_cliente": "Cliente storico", "preventivo_testo": "124,56"} {
		if got := colonna(t, d, id, c); got != v {
			t.Errorf("%s = %q, atteso %q", c, got, v)
		}
	}
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "dopo", r.corpo, `<div role="status" class="flash flash-info">Riparazione aggiornata.</div>`,
		`<dd id="repair-estimate">124,56</dd>`, "Interna aggiornata", `id="repair-detail"`)
	nonContiene(t, "dopo", r.corpo, `id="repair-form"`)
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.updated' AND attore = 'banco@esempio.it'`); n != 1 {
		t.Errorf("repair.updated %d", n)
	}
	// «Annulla» è la scheda.
	r = b.vai("GET", percorsoModifica(id), nil, nil)
	contiene(t, "annulla", r.corpo, `<a id="cancel-repair" href="`+percorsoScheda(id)+`"`)
}

// inventario: RIP-S-04
// Date, prezzi e descrizione sbagliati restano sul modulo con i valori scritti e gli errori sotto i
// campi, senza modifiche né audit; il modulo salva lo stesso anche se il testo arriva da Invio
// (non c'è un pulsante spento davvero). Un nome troppo lungo, giorni fuori da 0–90.
func TestModificaErroriSulModulo(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	for _, c := range []struct {
		campo, valore, errore, mostrato string
	}{
		{"repair[description]", "", "can't be blank", "name=\"repair[description]\" rows=\"4\" required>\n</textarea>"},
		{"repair[accepted_on]", "bad", "is invalid", `id="repair_accepted_on" value="bad" required>`},
		{"repair[expected_on]", "2026-02-31", "is invalid", `id="repair_expected_on" value="2026-02-31">`},
		{"repair[estimate_euros]", strings.Repeat("9", 256), "Importo troppo lungo: massimo 255 caratteri.", `value="` + strings.Repeat("9", 256) + `"`},
		{"repair[final_price_euros]", strings.Repeat("9", 256), "Importo troppo lungo: massimo 255 caratteri.", `value="` + strings.Repeat("9", 256) + `"`},
		{"repair[customer_name_snapshot]", strings.Repeat("a", 161), "should be at most 160 character(s)", `value="` + strings.Repeat("a", 161) + `"`},
		{"repair[customer_phone_snapshot]", strings.Repeat("3", 41), "should be at most 40 character(s)", `value="` + strings.Repeat("3", 41) + `"`},
		{"repair[extra_days]", "91", "Giorni in più: da 0 a 90.", `id="repair_extra_days" value="91"`},
		{"repair[extra_days]", "", "can't be blank", `id="repair_extra_days" value=""`},
	} {
		r := salva(t, b, id, map[string]string{c.campo: c.valore, "repair[qr_code]": "QR-SCRITTO"})
		if r.codice != 200 {
			t.Fatalf("%s: %d %q", c.campo, r.codice, r.location)
		}
		contiene(t, c.campo, r.corpo, `<p class="field-error">`+escapeHTMLTest(c.errore)+`</p>`, c.mostrato,
			`id="repair_qr_code" value="QR-SCRITTO"`, "<h2>Modifica riparazione</h2>")
		nonContiene(t, c.campo, r.corpo, `id="repair-detail"`, "Riparazione aggiornata.")
		if got := colonna(t, d, id, "codice_qr"); got != "QR-STORICO" {
			t.Errorf("%s: QR salvato %q", c.campo, got)
		}
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.updated'`); n != 0 {
		t.Errorf("repair.updated %d", n)
	}
}

func escapeHTMLTest(s string) string { return strings.ReplaceAll(s, "'", "&#39;") }

// inventario: RIP-S-04, RIP-S-05
// Un modulo forgiato non cambia stato, numero, date di pronta e consegna, cliente, gettone,
// centesimi né tipo: si leggono solo i campi dell'elenco fisso. Come «status uses validated
// transition and general save cannot override it» e «a forged edit cannot mass assign cents and
// wipe the typed price text».
func TestModificaCampiForzati(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `UPDATE buste SET preventivo_testo = '400/500', preventivo_centesimi = NULL, prezzo_centesimi = 0 WHERE id = ?`, id)
	fisse := []string{"stato", "numero", "data_pronta", "data_consegna", "cliente_id", "hex(hash_token_pubblico)", "preventivo_testo",
		"preventivo_centesimi", "prezzo_centesimi", "compro_oro", "conservazione_iniziata_il"}
	prima := map[string]string{}
	for _, c := range fisse {
		prima[c] = colonna(t, d, id, c)
	}
	b := titolareDentro(t, h)
	b.vai("GET", percorsoModifica(id), nil, nil)
	r := b.vai("POST", azioneModifica(id), url.Values{"_csrf_token": {b.token}, "repair[description]": {"Descrizione valida"},
		"repair[accepted_on]": {"2026-09-05"}, "repair[estimate_cents]": {"5000"}, "repair[final_price_cents]": {"99900"},
		"repair[status]": {"consegnata"}, "repair[ready_on]": {"2000-01-01"}, "repair[delivered_on]": {"2000-01-01"},
		"repair[bag_number]": {"999/99"}, "repair[customer_id]": {"999"}, "repair[public_token_hash]": {"x"},
		"status": {"consegnata"}, "estimate_cents": {"1"}}, nil)
	if !redirectA(r, percorsoScheda(id)) {
		t.Fatalf("%d %q", r.codice, r.location)
	}
	for _, c := range fisse {
		if got := colonna(t, d, id, c); got != prima[c] {
			t.Errorf("%s: %q, era %q", c, got, prima[c])
		}
	}
	if got := colonna(t, d, id, "descrizione"); got != "Descrizione valida" {
		t.Errorf("descrizione %q", got)
	}
	// La query non conta: solo il corpo del modulo.
	r = b.vai("POST", azioneModifica(id)+"?repair%5Bdescription%5D=dalla+query", url.Values{"_csrf_token": {b.token}}, nil)
	if !redirectA(r, percorsoScheda(id)) || colonna(t, d, id, "descrizione") != "Descrizione valida" {
		t.Errorf("dalla query: %d %q", r.codice, colonna(t, d, id, "descrizione"))
	}
}

// inventario: RIP-S-04
// Senza accesso, con l'account disattivato o con la sessione revocata la modifica non passa (al
// login, niente scritto); senza token CSRF è un 403. La busta che non c'è (o nel cestino) torna
// alla home con «Riparazione non trovata.».
func TestModificaSenzaAccesso(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := dipendenteDentro(t, h)
	v := moduloDi(t, b.vai("GET", percorsoModifica(id), nil, nil).corpo)
	v.Set("repair[description]", "Forbidden")
	senza := url.Values{}
	for k, vv := range v {
		if k != "_csrf_token" {
			senza[k] = vv
		}
	}
	if r := b.vai("POST", azioneModifica(id), senza, nil); r.codice != 403 {
		t.Errorf("senza CSRF: %d", r.codice)
	}
	eseguiSQL(t, d, `UPDATE utenti SET attivo = 0 WHERE email = 'banco@esempio.it'`)
	if r := b.vai("POST", azioneModifica(id), v, nil); !redirectA(r, "/login") {
		t.Errorf("disattivato: %d %q", r.codice, r.location)
	}
	if r := nuovoBrowser(t, h).vai("POST", azioneModifica(id), url.Values{"repair[description]": {"x"}}, nil); r.codice != 403 {
		t.Errorf("anonimo: %d", r.codice)
	}
	if got := colonna(t, d, id, "descrizione"); got != "Anello\nSaldatura" {
		t.Errorf("descrizione %q", got)
	}
	tit := titolareDentro(t, h)
	tit.vai("GET", "/", nil, nil)
	eseguiSQL(t, d, `UPDATE buste SET cancellata_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, id)
	for _, p := range []string{azioneModifica(id), "/repairs/999/modifica", "/repairs/abc/modifica", "/repairs/0/togli-data", "/repairs/x/togli-data"} {
		r := tit.vai("POST", p, url.Values{"_csrf_token": {tit.token}, "repair[description]": {"x"}}, nil)
		if !redirectA(r, "/") {
			t.Errorf("%s: %d %q", p, r.codice, r.location)
			continue
		}
		contiene(t, p, tit.vai("GET", "/", nil, nil).corpo, "Riparazione non trovata.")
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.updated'`); n != 0 {
		t.Errorf("repair.updated %d", n)
	}
}

// inventario: RIP-S-05
// Il prezzo a testo resta com'è nella scheda e nel modulo; svuotare preventivo, prezzo, note e
// consegna li toglie; il testo è scappato. Come «a text price survives edit, detail, form…» e
// «clears optional values and escapes content…».
func TestModificaPrezziATesto(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	salva(t, b, id, map[string]string{"repair[estimate_euros]": "400/500", "repair[final_price_euros]": "da definire"})
	if got := colonna(t, d, id, "preventivo_testo") + "|" + colonna(t, d, id, "preventivo_centesimi") + "|" +
		colonna(t, d, id, "prezzo_testo") + "|" + colonna(t, d, id, "prezzo_centesimi"); got != "400/500|∅|da definire|∅" {
		t.Errorf("prezzi %q", got)
	}
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "scheda", r.corpo, `<dd id="repair-estimate">400/500</dd>`, `<dd id="repair-final-price" class="grassetto">da definire</dd>`)
	nonContiene(t, "scheda", r.corpo, "123,45")
	r = b.vai("GET", percorsoModifica(id), nil, nil)
	contiene(t, "modulo", r.corpo, `id="repair_estimate_euros" value="400/500"`, `id="repair_final_price_euros" value="da definire"`)

	salva(t, b, id, map[string]string{"repair[description]": "<script>alert(1)</script>", "repair[expected_on]": "",
		"repair[public_notes]": "", "repair[estimate_euros]": "", "repair[final_price_euros]": ""})
	for _, c := range []string{"data_prevista", "data_prevista_testo", "note_pubbliche", "preventivo_centesimi", "prezzo_centesimi",
		"preventivo_testo", "prezzo_testo"} {
		if got := colonna(t, d, id, c); got != "∅" {
			t.Errorf("%s = %q", c, got)
		}
	}
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "scappato", r.corpo, "&lt;script&gt;alert(1)&lt;/script&gt;")
	nonContiene(t, "scappato", r.corpo, "<script>alert")
	r = b.vai("GET", percorsoModifica(id), nil, nil)
	contiene(t, "scappato nel modulo", r.corpo, "\n&lt;script&gt;alert(1)&lt;/script&gt;</textarea>")
}

// inventario: RIP-S-06
// Una consegna a testo (importata) si vede com'è; nel modulo viaggia nascosta con «Scritta a mano:
// …» e una modifica ordinaria non la tocca; «Togli data» del modulo svuota anche quella (app.js);
// una data scelta la sostituisce.
func TestModificaConsegnaATesto(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `UPDATE buste SET data_prevista_testo = 'fine mese', data_prevista = NULL WHERE id = ?`, id)
	b := titolareDentro(t, h)
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "scheda", r.corpo, `<dd id="repair-delivery">fine mese</dd>`)
	r = b.vai("GET", percorsoModifica(id), nil, nil)
	// Un campo senza valore non ha l'attributo value, come Phoenix con nil.
	contiene(t, "modulo", r.corpo, `id="repair_expected_on">`,
		`<input type="hidden" id="repair_expected_on_text" name="repair[expected_on_text]" value="fine mese">`,
		`<p id="repair-expected-text" class="sotto">Scritta a mano: fine mese</p>`)
	salva(t, b, id, map[string]string{"repair[description]": "Collana\nLucidatura", "repair[expected_on]": ""})
	if got := colonna(t, d, id, "data_prevista_testo") + "|" + colonna(t, d, id, "data_prevista") + "|" + colonna(t, d, id, "descrizione"); got != "fine mese|∅|Collana\nLucidatura" {
		t.Errorf("modifica ordinaria %q", got)
	}
	// «Togli data» del modulo: app.js svuota i due campi.
	salva(t, b, id, map[string]string{"repair[expected_on]": "", "repair[expected_on_text]": ""})
	if got := colonna(t, d, id, "data_prevista_testo") + "|" + colonna(t, d, id, "data_prevista"); got != "∅|∅" {
		t.Errorf("tolta dal modulo %q", got)
	}
	eseguiSQL(t, d, `UPDATE buste SET data_prevista_testo = 'fine mese' WHERE id = ?`, id)
	salva(t, b, id, map[string]string{"repair[expected_on]": "2026-10-01"})
	if got := colonna(t, d, id, "data_prevista_testo") + "|" + colonna(t, d, id, "data_prevista"); got != "∅|2026-10-01" {
		t.Errorf("data scelta %q", got)
	}
	r = b.vai("GET", percorsoModifica(id), nil, nil)
	nonContiene(t, "senza testo", r.corpo, `name="repair[expected_on_text]"`, `id="repair-expected-text"`)
}

// inventario: RIP-S-06
// «Togli data» nella scheda toglie la scadenza (data o testo) con un tocco: «Data di consegna
// tolta.» e l'audit della scadenza tolta; se non riesce «Data non tolta. Nessuna modifica
// applicata.». Senza scadenza il pulsante non c'è.
func TestTogliDataDallaScheda(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := dipendenteDentro(t, h)
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "pulsante", r.corpo, `<dd id="repair-delivery">20/09/2026</dd>`+"\n"+
		`<form action="/repairs/`+strconv.FormatInt(id, 10)+`/togli-data" method="post" class="in-linea">`,
		`<button id="repair-delivery-clear" type="submit" class="pulsante-piccolo">Togli data</button>`)
	r = b.vai("POST", percorsoScheda(id)+"/togli-data", url.Values{"_csrf_token": {b.token}}, nil)
	if !redirectA(r, percorsoScheda(id)) {
		t.Fatalf("%d %q", r.codice, r.location)
	}
	if got := colonna(t, d, id, "data_prevista"); got != "∅" {
		t.Errorf("data %q", got)
	}
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "tolta", r.corpo, `<div role="status" class="flash flash-info">Data di consegna tolta.</div>`, `<dd id="repair-delivery">-</dd>`)
	nonContiene(t, "tolta", r.corpo, `id="repair-delivery-clear"`)
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.updated' AND attore = 'banco@esempio.it' AND dati = '{"expected_on":{"from":"2026-09-20","to":null}}'`); n != 1 {
		t.Errorf("audit %d", n)
	}
	// Il testo importato si toglie allo stesso modo; un audit che non si scrive lascia tutto.
	eseguiSQL(t, d, `UPDATE buste SET data_prevista_testo = 'fine mese' WHERE id = ?`, id)
	eseguiSQL(t, d, `CREATE TEMP TRIGGER no_modifica BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.updated' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	r = b.vai("POST", percorsoScheda(id)+"/togli-data", url.Values{"_csrf_token": {b.token}}, nil)
	if !redirectA(r, percorsoScheda(id)) {
		t.Fatalf("%d %q", r.codice, r.location)
	}
	contiene(t, "non tolta", b.vai("GET", percorsoScheda(id), nil, nil).corpo,
		`<div role="alert" class="flash flash-errore">Data non tolta. Nessuna modifica applicata.</div>`, `<dd id="repair-delivery">fine mese</dd>`)
}

// inventario: RIP-S-04
// Un salvataggio che il database rifiuta (qui la riga d'audit) resta sul modulo con «Salvataggio
// non riuscito. Nessuna modifica applicata.» e i valori scritti.
func TestModificaNonRiuscita(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `CREATE TEMP TRIGGER no_modifica BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.updated' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	r := salva(t, titolareDentro(t, h), id, map[string]string{"repair[description]": "Nuova"})
	if r.codice != 200 {
		t.Fatalf("%d %q", r.codice, r.location)
	}
	contiene(t, "rifiuto", r.corpo, `<div role="alert" class="flash flash-errore">Salvataggio non riuscito. Nessuna modifica applicata.</div>`,
		"name=\"repair[description]\" rows=\"4\" required>\nNuova</textarea>")
	if got := colonna(t, d, id, "descrizione"); got != "Anello\nSaldatura" {
		t.Errorf("descrizione %q", got)
	}
}

// inventario: RIP-S-07
// Nome e telefono si correggono sulla busta, non sul cliente. Su una busta conto terzi il
// telefono è spento con il suo avviso, e un telefono forgiato non passa.
func TestModificaClienteDallaScheda(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (1, 'Il negozio di prova'), (2, 'Gioielleria Bianchi')`)
	id := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	salva(t, b, id, map[string]string{"repair[customer_name_snapshot]": " Cliente corretto ", "repair[customer_phone_snapshot]": ""})
	if got := colonna(t, d, id, "nome_cliente") + "|" + colonna(t, d, id, "telefono_cliente"); got != "Cliente corretto|" {
		t.Errorf("copie %q", got)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM clienti WHERE nome = 'Cliente storico' AND telefono = '333 111'`); n != 1 {
		t.Error("il cliente è cambiato")
	}
	eseguiSQL(t, d, `UPDATE buste SET oreficeria_id = 2, telefono_cliente = '333 111' WHERE id = ?`, id)
	r := b.vai("GET", percorsoModifica(id), nil, nil)
	contiene(t, "bloccato", r.corpo, `id="repair_customer_phone_snapshot" value="333 111" maxlength="40" autocomplete="off" disabled>`,
		`<p id="repair-phone-locked" class="sotto">Busta conto terzi: si scrive solo all'oreficeria, il telefono del cliente finale non si modifica.</p>`)
	v := moduloDi(t, r.corpo)
	if v.Has("repair[customer_phone_snapshot]") {
		t.Error("un campo spento non parte")
	}
	v.Set("repair[customer_phone_snapshot]", "000")
	b.vai("POST", azioneModifica(id), v, nil)
	if got := colonna(t, d, id, "telefono_cliente"); got != "333 111" {
		t.Errorf("telefono forzato %q", got)
	}
	// Dopo un rifiuto il blocco segue l'oreficeria scelta nel modulo.
	r = salva(t, b, id, map[string]string{"repair[oreficeria_id]": "", "repair[description]": ""})
	nonContiene(t, "sbloccato", r.corpo, `id="repair-phone-locked"`)
}

// inventario: RIP-S-08
// L'oreficeria si cambia dal menu (la voce della busta scelta) e la busta torna diretta con «—
// Diretta <negozio>», senza toccare le copie del cliente; la riga 1 e un id che non c'è sono
// rifiutati con il messaggio generico, e il modulo resta.
func TestModificaOreficeriaDallaScheda(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (1, 'Il negozio di prova'), (2, 'Gioielleria Bianchi')`)
	id := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.OreficeriaID = "2" })
	b := titolareDentro(t, h)
	r := b.vai("GET", percorsoModifica(id), nil, nil)
	contiene(t, "menu", r.corpo, `<select id="repair_oreficeria_id" name="repair[oreficeria_id]">`+"\n"+
		`<option value="">— Diretta Negozio</option>`+"\n"+`<option value="2" selected>Gioielleria Bianchi</option>`)
	salva(t, b, id, map[string]string{"repair[description]": "Aggiornata", "repair[oreficeria_id]": ""})
	if got := colonna(t, d, id, "oreficeria_id") + "|" + colonna(t, d, id, "nome_cliente") + "|" + colonna(t, d, id, "telefono_cliente"); got != "∅|Cliente storico|333 111" {
		t.Errorf("tornata diretta %q", got)
	}
	contiene(t, "diretta", b.vai("GET", percorsoScheda(id), nil, nil).corpo, "<span>Diretta Negozio</span>")
	for _, v := range []string{"1", "999999"} {
		r := salva(t, b, id, map[string]string{"repair[oreficeria_id]": v, "repair[description]": "Mai"})
		if r.codice != 200 {
			t.Fatalf("%s: %d", v, r.codice)
		}
		contiene(t, v, r.corpo, `<div role="alert" class="flash flash-errore">Salvataggio non riuscito. Nessuna modifica applicata.</div>`,
			`id="repair-form"`, "\nMai</textarea>")
		if got := colonna(t, d, id, "oreficeria_id") + "|" + colonna(t, d, id, "descrizione"); got != "∅|Aggiornata" {
			t.Errorf("%s: %q", v, got)
		}
	}
	// Spostata a un'oreficeria: il link si revoca nella stessa modifica.
	salva(t, b, id, map[string]string{"repair[oreficeria_id]": "2"})
	if got := colonna(t, d, id, "token_pubblico"); got != "∅" {
		t.Errorf("token %q", got)
	}
}

// inventario: RIP-S-09
// L'artigiano si assegna, si cambia e si toglie dal modulo; un id falso è «Seleziona un artigiano
// valido.» senza toccare la busta.
func TestModificaArtigianoDallaScheda(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Cesellatore'), (2, 'Incastonatore')`)
	id := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	contiene(t, "scheda", b.vai("GET", percorsoScheda(id), nil, nil).corpo, `<dd id="repair-artisan">—</dd>`)
	contiene(t, "menu", b.vai("GET", percorsoModifica(id), nil, nil).corpo, `<select id="repair_artisan_id" name="repair[artisan_id]">`+"\n"+
		`<option value="">— Nessuno —</option>`+"\n"+`<option value="1">Cesellatore</option>`+"\n"+`<option value="2">Incastonatore</option>`)
	for _, c := range []struct{ id, nome string }{{"1", "Cesellatore"}, {"2", "Incastonatore"}, {"", "—"}} {
		salva(t, b, id, map[string]string{"repair[artisan_id]": c.id})
		contiene(t, c.nome, b.vai("GET", percorsoScheda(id), nil, nil).corpo, `<dd id="repair-artisan">`+c.nome+`</dd>`)
	}
	r := salva(t, b, id, map[string]string{"repair[artisan_id]": "999999"})
	contiene(t, "falso", r.corpo, `<div role="alert" class="flash flash-errore">Seleziona un artigiano valido.</div>`, `id="repair-form"`)
	if got := colonna(t, d, id, "artigiano_id"); got != "∅" {
		t.Errorf("artigiano %q", got)
	}
}

// inventario: RIP-S-10
// Il QR si scrive dal modulo e si vede nella scheda; vuoto lo toglie («—»).
func TestModificaQRDallaScheda(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.CodiceQR = "" })
	b := titolareDentro(t, h)
	contiene(t, "senza", b.vai("GET", percorsoScheda(id), nil, nil).corpo, `<dd id="repair-qr-code" class="mono">—</dd>`)
	// Senza QR e senza prezzo finale: niente attributo value (Phoenix con nil); dopo un rifiuto il
	// valore scritto, anche vuoto, c'è.
	contiene(t, "senza valore", b.vai("GET", percorsoModifica(id), nil, nil).corpo,
		`id="repair_qr_code" placeholder="Scansiona o inserisci manualmente"`, `id="repair_final_price_euros" placeholder=`)
	contiene(t, "vuoto scritto", salva(t, b, id, map[string]string{"repair[description]": ""}).corpo,
		`id="repair_qr_code" value="" placeholder=`, `id="repair_final_price_euros" value="" placeholder=`)
	salva(t, b, id, map[string]string{"repair[qr_code]": "QR-EDIT-7"})
	contiene(t, "scritto", b.vai("GET", percorsoScheda(id), nil, nil).corpo, `<dd id="repair-qr-code" class="mono">QR-EDIT-7</dd>`)
	contiene(t, "modulo", b.vai("GET", percorsoModifica(id), nil, nil).corpo, `id="repair_qr_code" value="QR-EDIT-7"`)
	salva(t, b, id, map[string]string{"repair[qr_code]": ""})
	if got := colonna(t, d, id, "codice_qr"); got != "∅" {
		t.Errorf("QR %q", got)
	}
}

// inventario: RIP-S-11
// La casella «🪙 Scheda Compro Oro» mostra il tipo della busta; salvarla segnata fa della busta una
// scheda compro oro con lo stato riallineato e lo dice; e all'indietro. Come
// repair_kind_live_test.exs.
func TestModificaTipoDallaScheda(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	r := salva(t, b, id, map[string]string{"repair[compro_oro]": "true"})
	if !redirectA(r, percorsoScheda(id)) {
		t.Fatalf("%d %q", r.codice, r.location)
	}
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "compro oro", r.corpo, "Riparazione aggiornata. Tipo busta cambiato: stato riallineato a «In valutazione».",
		`<span id="repair-compro-oro" class="badge-compro-oro">🪙 Compro Oro</span>`, `<dd id="repair-status" class="forte">In valutazione</dd>`)
	if got := colonna(t, d, id, "compro_oro") + "|" + colonna(t, d, id, "stato"); got != "1|in_valutazione" {
		t.Errorf("tipo %q", got)
	}
	contiene(t, "segnata", b.vai("GET", percorsoModifica(id), nil, nil).corpo,
		`<input id="repair-compro-oro-box" type="checkbox" name="repair[compro_oro]" value="true" checked>`,
		`<input name="tipo_mostrato" type="hidden" value="true">`)
	salva(t, b, id, map[string]string{"repair[compro_oro]": "false"})
	if got := colonna(t, d, id, "compro_oro") + "|" + colonna(t, d, id, "stato"); got != "0|in_lavorazione" {
		t.Errorf("all'indietro %q", got)
	}
	// «annullata» resta: niente riallineamento da annunciare.
	eseguiSQL(t, d, `UPDATE buste SET stato = 'annullata' WHERE id = ?`, id)
	salva(t, b, id, map[string]string{"repair[compro_oro]": "true"})
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "annullata", r.corpo, `<div role="status" class="flash flash-info">Riparazione aggiornata.</div>`)
}

// inventario: RIP-S-11
// Cambia solo con un gesto: una pagina rimasta aperta (il tipo che mostrava) non annulla un cambio
// fatto altrove; un modulo senza la casella lascia il tipo com'è.
func TestModificaTipoSoloConUnGesto(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	v := moduloDi(t, b.vai("GET", percorsoModifica(id), nil, nil).corpo)
	if _, err := buste.Nuove(d, o).Modifica(t.Context(), id, buste.Modifica{}, ptrBool(true), contestoProva); err != nil {
		t.Fatal(err)
	}
	v.Set("repair[description]", "Solo la descrizione")
	b.vai("POST", azioneModifica(id), v, nil)
	if got := colonna(t, d, id, "compro_oro") + "|" + colonna(t, d, id, "stato") + "|" + colonna(t, d, id, "descrizione"); got != "1|in_valutazione|Solo la descrizione" {
		t.Errorf("pagina vecchia %q", got)
	}
	eseguiSQL(t, d, `UPDATE buste SET stato = 'valutata' WHERE id = ?`, id)
	b.vai("POST", azioneModifica(id), url.Values{"_csrf_token": {b.token}, "repair[description]": {"Senza casella"}}, nil)
	if got := colonna(t, d, id, "compro_oro") + "|" + colonna(t, d, id, "stato"); got != "1|valutata" {
		t.Errorf("senza casella %q", got)
	}
}

// inventario: RIP-S-11
// Una busta fuori bottega o chiusa è rifiutata con il testo dello standalone e niente si salva; un
// campo sbagliato nella stessa modifica si porta via il cambio di tipo, e il modulo ridisegnato
// dice che cosa farebbe la casella segnata.
func TestModificaTipoRifiutato(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := titolareDentro(t, h)
	fuori := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `UPDATE buste SET consegnata_artigiano_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, fuori)
	r := salva(t, b, fuori, map[string]string{"repair[compro_oro]": "true"})
	contiene(t, "fuori bottega", r.corpo, "Impossibile marcare come compro oro: la busta è fuori bottega presso un artigiano. Registrare il rientro prima.",
		`<p id="repair-kind-hint" class="sotto">Impossibile marcare come compro oro`)
	nonContiene(t, "fuori bottega", r.corpo, "Riparazione aggiornata.")
	consegnata := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.Numero = "2/26" })
	eseguiSQL(t, d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, consegnata)
	r = salva(t, b, consegnata, map[string]string{"repair[compro_oro]": "true"})
	contiene(t, "consegnata", r.corpo, "Per cambiare il tipo di una busta chiusa riaprila prima: riportarla al lavoro richiede un motivo.")
	aperta := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.Numero = "3/26" })
	r = salva(t, b, aperta, map[string]string{"repair[compro_oro]": "true", "repair[customer_name_snapshot]": ""})
	contiene(t, "campo sbagliato", r.corpo, `<p id="repair-kind-hint" class="sotto">Lo stato «In lavorazione» passerà a «In valutazione».</p>`,
		`<input id="repair-compro-oro-box" type="checkbox" name="repair[compro_oro]" value="true" checked>`, "can&#39;t be blank")
	for _, id := range []int64{fuori, consegnata, aperta} {
		if got := colonna(t, d, id, "compro_oro"); got != "0" {
			t.Errorf("%d: compro oro %q", id, got)
		}
	}
}

// inventario: RIP-S-12
// Dal popup del QR la scheda si apre già in modifica con il cursore nel prezzo finale (?edit=prezzo)
// o nel preventivo (?edit=preventivo); ogni altro valore apre la scheda; «Modifica» a mano non
// sposta il cursore, nemmeno dopo un salvataggio, e un modulo ridisegnato dopo un rifiuto nemmeno.
func TestModificaDalPopupQR(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := dipendenteDentro(t, h)
	fuoco := regexp.MustCompile(`<input [^>]*id="(repair_[a-z_]+)"[^>]* autofocus>`)
	for q, atteso := range map[string]string{"?edit=prezzo": "repair_final_price_euros", "?edit=preventivo": "repair_estimate_euros",
		"?edit=altro": "", "?edit=PREZZO": "", "?modifica=1": "", "?edit=prezzo&edit=preventivo": "repair_estimate_euros"} {
		r := b.vai("GET", percorsoScheda(id)+q, nil, nil)
		if r.codice != 200 {
			t.Fatalf("%s: %d", q, r.codice)
		}
		got := ""
		if m := fuoco.FindAllStringSubmatch(r.corpo, -1); len(m) == 1 {
			got = m[0][1]
		} else if len(m) > 1 {
			t.Errorf("%s: %d campi con il fuoco", q, len(m))
		}
		if got != atteso {
			t.Errorf("%s: fuoco su %q, atteso %q", q, got, atteso)
		}
		if strings.HasPrefix(q, "?edit=") && atteso == "" {
			contiene(t, q, r.corpo, `id="repair-detail"`, `id="edit-repair"`)
			nonContiene(t, q, r.corpo, `id="repair-form"`)
		} else {
			contiene(t, q, r.corpo, `id="repair-form"`)
		}
	}
	r := salva(t, b, id, map[string]string{"repair[description]": ""})
	nonContiene(t, "dopo un rifiuto", r.corpo, "autofocus")
}

// inventario: RIP-S-04, RIP-S-07, RIP-S-10, RIP-S-11
// Mancano le parti che nello standalone sono LiveView o JavaScript e arrivano con la prima funzione
// con JavaScript e il livello browser del banco: «Salva modifiche» spento nell'aspetto finché non
// cambia niente; gli errori dei campi mentre si scrive; il ✏️ flottante; il telefono che si
// spegne appena si sceglie un'oreficeria; «📷 Scansiona»; l'annuncio «Lo stato «…» passerà a «…»»
// prima di salvare.
func TestModificaConJavaScript(t *testing.T) {
	t.Skip("parti con JavaScript: con la prima funzione con JavaScript e il livello browser del banco")
}

func ptrBool(v bool) *bool { return &v }

var contestoProva = audit.Contesto{Attore: "titolare@esempio.it"}

// inventario: RIP-S-07
// Dopo un rifiuto, con il telefono bloccato, il modulo mostra il telefono salvato e non quello
// forzato; l'oreficeria 1 forzata non blocca il telefono (Oreficeria.is_b2b(1) è falso).
func TestModificaTelefonoBloccatoRidisegnato(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (1, 'Il negozio di prova'), (2, 'Gioielleria Bianchi')`)
	id := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.OreficeriaID = "2" })
	b := titolareDentro(t, h)
	b.vai("GET", percorsoModifica(id), nil, nil)
	r := b.vai("POST", azioneModifica(id), url.Values{"_csrf_token": {b.token}, "repair[customer_phone_snapshot]": {"000 forzato"},
		"repair[description]": {""}}, nil)
	contiene(t, "bloccato", r.corpo, `id="repair_customer_phone_snapshot" value="333 111" maxlength="40" autocomplete="off" disabled>`)
	nonContiene(t, "bloccato", r.corpo, "000 forzato")
	r = b.vai("POST", azioneModifica(id), url.Values{"_csrf_token": {b.token}, "repair[oreficeria_id]": {"1"},
		"repair[customer_phone_snapshot]": {"444"}}, nil)
	contiene(t, "oreficeria 1", r.corpo, "Salvataggio non riuscito. Nessuna modifica applicata.",
		`id="repair_customer_phone_snapshot" value="444" maxlength="40" autocomplete="off">`)
	nonContiene(t, "oreficeria 1", r.corpo, `id="repair-phone-locked"`)
}

// inventario: RIP-S-05
// Un prezzo a testo vuoto senza centesimi non ha valore nel modulo (price_input/2 dà nil).
func TestModificaPrezzoVuotoSenzaValore(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `UPDATE buste SET preventivo_testo = '', preventivo_centesimi = NULL WHERE id = ?`, id)
	r := titolareDentro(t, h).vai("GET", percorsoModifica(id), nil, nil)
	contiene(t, "vuoto", r.corpo, `id="repair_estimate_euros" placeholder=`)
}

// inventario: RIP-S-06
// «Togli data» su una busta nel cestino o che non c'è torna alla home con «Riparazione non
// trovata.».
func TestTogliDataBustaNonTrovata(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `UPDATE buste SET cancellata_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, id)
	b := titolareDentro(t, h)
	b.vai("GET", "/", nil, nil)
	for _, p := range []string{percorsoScheda(id) + "/togli-data", "/repairs/999/togli-data"} {
		r := b.vai("POST", p, url.Values{"_csrf_token": {b.token}}, nil)
		if !redirectA(r, "/") {
			t.Errorf("%s: %d %q", p, r.codice, r.location)
			continue
		}
		contiene(t, p, b.vai("GET", "/", nil, nil).corpo, "Riparazione non trovata.")
	}
	if got := colonna(t, d, id, "data_prevista"); got != "2026-09-20" {
		t.Errorf("data %q", got)
	}
}

// inventario: RIP-S-11
// Una casella con un valore diverso da "true" e "false" (per esempio «on») lascia il tipo com'è,
// come kind_param/2, anche nel modulo ridisegnato.
func TestModificaCompraOroValoreStrano(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	r := salva(t, b, id, map[string]string{"repair[compro_oro]": "on", "repair[description]": ""})
	contiene(t, "on", r.corpo, `<input id="repair-compro-oro-box" type="checkbox" name="repair[compro_oro]" value="true">`)
	nonContiene(t, "on", r.corpo, `id="repair-kind-hint"`)
	salva(t, b, id, map[string]string{"repair[compro_oro]": "on"})
	if got := colonna(t, d, id, "compro_oro"); got != "0" {
		t.Errorf("compro oro %q", got)
	}
}
