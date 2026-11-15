package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Il riquadro «🔧 Fuori bottega» della scheda e il rientro dall'artigiano (rientro.go, slice 080, RIP-S-16,
// ART-13), come #repair-handed-over, #repair-artisan-notes e l'evento return-from-artisan di RepairLive
// (302439d): i casi di repair_live_test.exs («the banner returns the bag in place…», «a forged return is
// refused…», «an outcome the transition graph forbids…»), senza LiveView. Nomi e numeri inventati;
// l'orologio è fermo al 31/10/2026, le 10:00 in negozio.

// provaRientro è la faccia con le buste, il database e l'orologio.
type provaRientro struct {
	h http.Handler
	d *db.DB
	o *clock.Finto
}

func nuovaProvaBuste(t *testing.T) *provaRientro {
	t.Helper()
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	return &provaRientro{h, d, o}
}

func azioneRientro(id int64) string { return percorsoScheda(id) + "/rientro" }

func formRientro(b *browser, esito, nota string) url.Values {
	return url.Values{"_csrf_token": {b.token}, "return[stato_lavoro]": {esito}, "return[note_artigiano]": {nota}}
}

// bustaFuori crea una busta uscita il 20/10/2026 alle 10:00 in negozio verso l'artigiano 1, nella consegna
// data (0: nessuna), con le colonne in più; dà l'id.
func bustaFuori(t *testing.T, h *provaRientro, consegna int64, colonne string) int64 {
	t.Helper()
	id := bustaScheda(t, h.d, h.o, nil)
	q := `consegnata_artigiano_il = '2026-10-20T08:00:00.000000Z', artigiano_id = 1`
	if consegna != 0 {
		q += `, consegna_artigiano_id = ` + itoa(consegna)
	}
	if colonne != "" {
		q += ", " + colonne
	}
	eseguiSQL(t, h.d, `UPDATE buste SET `+q+` WHERE id = ?`, id)
	return id
}

// inventario: RIP-S-16
// Una busta fuori bottega ha, dopo i dati, il riquadro dello standalone: «🔧 Fuori bottega presso» con il nome
// dell'artigiano assegnato, «Consegnata il» nell'ora del negozio, «✓ Segna rientrata» che apre il modulo
// (Stato lavoro con «fatto» scelto e per primo, la nota, «Conferma rientro» con la conferma), «🖨 Ristampa»
// dell'etichetta della busta in una nuova scheda e «📤 Condividi» (data-share-pdf con il download). Una busta
// in bottega, rientrata o mai uscita non ce l'ha. Lo vedono titolare e dipendente.
func TestRiquadroFuoriBottega(t *testing.T) {
	h := nuovaProvaBuste(t)
	eseguiSQL(t, h.d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Orafo <Prova>')`)
	id := bustaFuori(t, h, 0, "")
	sid := itoa(id)
	for _, b := range []*browser{titolareDentro(t, h.h), dipendenteDentro(t, h.h)} {
		r := b.vai("GET", percorsoScheda(id), nil, nil)
		contiene(t, "fuori", r.corpo, `<div id="repair-handed-over" class="riquadro-fuori">`+"\n"+`<div>`+"\n"+
			`<strong>🔧 Fuori bottega presso Orafo &lt;Prova&gt;</strong>`+"\n"+
			`<div id="repair-handed-over-since" class="sotto">Consegnata il 20/10/2026 10:00</div>`+"\n"+`</div>`+"\n"+
			`<details id="repair-return-control" class="rientro">`+"\n"+
			`<summary id="return-from-artisan" class="pulsante-fuori">✓ Segna rientrata</summary>`+"\n"+
			`<form id="repair-return-form" action="/repairs/`+sid+`/rientro" method="post" class="modulo-rientro" data-invio-unico>`+"\n"+
			`<input name="_csrf_token" type="hidden" value="`+b.token+`">`,
			`<h3>Rientro da Orafo &lt;Prova&gt;</h3>`+"\n"+
				`<div class="fieldset">`+"\n"+`<label for="return_stato_lavoro">`+"\n"+`<span class="label">Stato lavoro</span>`+"\n"+
				`<select id="return_stato_lavoro" name="return[stato_lavoro]">`+"\n"+
				`<option value="fatto" selected>Fatto → passa a "pronta"</option>`+"\n"+
				`<option value="da_rifare">Da rifare → torna "in lavorazione"</option>`+"\n"+
				`<option value="">Nessun cambio di stato</option>`+"\n"+`</select>`+"\n"+`</label>`+"\n"+`</div>`+"\n"+
				`<div class="fieldset">`+"\n"+`<label for="return_note_artigiano">`+"\n"+`<span class="label">Note artigiano (trascritte dall'etichetta)</span>`+"\n"+
				`<textarea id="return_note_artigiano" name="return[note_artigiano]" rows="3" placeholder="Annotazioni manoscritte dall'artigiano…"></textarea>`+"\n"+
				`</label>`+"\n"+`</div>`+"\n"+
				`<button type="submit" data-confirm="Confermare il rientro della busta dall'artigiano?">Conferma rientro</button>`+"\n"+`</form>`+"\n"+`</details>`+"\n"+
				`<a id="repair-handed-over-reprint" href="/repairs/`+sid+`/label.pdf" target="_blank" class="pulsante-contorno">🖨 Ristampa</a>`+"\n"+
				`<a id="repair-handed-over-share" href="/repairs/`+sid+`/label.pdf?download=1" data-share-pdf="/repairs/`+sid+`/label.pdf" class="pulsante-contorno">📤 Condividi</a>`+"\n"+`</div>`)
		nonContiene(t, "fuori", r.corpo, `id="repair-return-outcome-warning"`, `id="repair-artisan-notes"`)
		// Il riquadro sta dopo i dati e prima della conversazione, come nello standalone.
		if i := strings.Index(r.corpo, `id="repair-handed-over"`); i < strings.Index(r.corpo, `id="repair-qr-code"`) || i > strings.Index(r.corpo, `id="repair-reply"`) {
			t.Error("posto del riquadro")
		}
	}
	b := titolareDentro(t, h.h)
	// La nota del giro in corso (non ancora rientrata) non si mostra come nota dell'ultimo rientro.
	eseguiSQL(t, h.d, `UPDATE buste SET note_artigiano = 'del giro' WHERE id = ?`, id)
	nonContiene(t, "nota del giro", b.vai("GET", percorsoScheda(id), nil, nil).corpo, `id="repair-artisan-notes"`)
	// Senza l'artigiano (anagrafica tolta): «artigiano».
	eseguiSQL(t, h.d, `UPDATE buste SET artigiano_id = NULL WHERE id = ?`, id)
	contiene(t, "senza artigiano", b.vai("GET", percorsoScheda(id), nil, nil).corpo, `<strong>🔧 Fuori bottega presso artigiano</strong>`, `<h3>Rientro da artigiano</h3>`)
	for _, colonne := range []string{`rientrata_artigiano_il = '2026-10-25T08:00:00.000000Z'`, `consegnata_artigiano_il = NULL`} {
		eseguiSQL(t, h.d, `UPDATE buste SET consegnata_artigiano_il = '2026-10-20T08:00:00.000000Z', rientrata_artigiano_il = NULL WHERE id = ?`, id)
		eseguiSQL(t, h.d, `UPDATE buste SET `+colonne+` WHERE id = ?`, id)
		nonContiene(t, colonne, b.vai("GET", percorsoScheda(id), nil, nil).corpo, `id="repair-handed-over"`, "data-share-pdf", `id="repair-return-form"`)
	}
}

// inventario: RIP-S-16, ART-13
// L'avviso prima della conferma (return_warning/1): da preventivo «Fatto» non è applicabile, da consegnata
// nessun esito lo è; il menu offre sempre i tre esiti, con «fatto» scelto.
func TestRiquadroFuoriBottegaAvviso(t *testing.T) {
	h := nuovaProvaBuste(t)
	eseguiSQL(t, h.d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Orafo Prova')`)
	b := titolareDentro(t, h.h)
	casi := map[string]string{
		"preventivo":          `<p id="repair-return-outcome-warning" class="grassetto">Da «Preventivo» l&#39;esito «Fatto» non è applicabile: il rientro con quell&#39;esito viene rifiutato per intero.</p>`,
		"consegnata":          `<p id="repair-return-outcome-warning" class="grassetto">Da «Consegnata» nessun esito è applicabile: il rientro può solo lasciare lo stato invariato.</p>`,
		"in_attesa_materiali": "",
		"pronta":              "",
	}
	for stato, avviso := range casi {
		id := bustaFuori(t, h, 0, `stato = '`+stato+`'`)
		r := b.vai("GET", percorsoScheda(id), nil, nil)
		contiene(t, stato, r.corpo, `<option value="fatto" selected>`, `<option value="da_rifare">`, `<option value="">Nessun cambio di stato</option>`)
		if avviso == "" {
			nonContiene(t, stato, r.corpo, `id="repair-return-outcome-warning"`)
			continue
		}
		contiene(t, stato, r.corpo, `<h3>Rientro da Orafo Prova</h3>`+"\n"+avviso+"\n"+`<div class="fieldset">`)
	}
}

// inventario: RIP-S-16, ART-13
// «Conferma rientro» con «fatto»: la scheda con «Rientro registrato.», la busta pronta (con la data), il
// riquadro sparito e, con la nota, «📝 Note ultimo rientro da artigiano:» con il testo com'è scritto; la
// consegna chiusa con l'ultima busta. Il dipendente può. Un secondo invio dello stesso modulo dice «La
// riparazione non risulta fuori bottega.» e non scrive niente.
func TestRientroDallaScheda(t *testing.T) {
	h := nuovaProvaBuste(t)
	eseguiSQL(t, h.d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Orafo Prova')`)
	consegna := consegnaProva(t, h.d, 1, "2026-10-20T07:00:00.000000Z", "")
	id := bustaFuori(t, h, consegna, "")
	b := dipendenteDentro(t, h.h)
	b.vai("GET", percorsoScheda(id), nil, nil)
	if r := b.vai("POST", azioneRientro(id), formRientro(b, "fatto", "  Riga 1\n<b>Riga 2</b>  "), nil); !redirectAllaScheda(r, id) {
		t.Fatalf("rientro: %d %q", r.codice, r.location)
	}
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "dopo", r.corpo, `<div role="status" class="flash flash-info">Rientro registrato.</div>`,
		`<dd id="repair-status" class="forte">Pronta</dd>`, `<dd id="repair-ready-on">31/10/2026</dd>`,
		`<div id="repair-artisan-notes" class="riquadro-note-artigiano">`+"\n"+`<strong>📝 Note ultimo rientro da artigiano:</strong>`+"\n"+
			`<div class="whitespace-pre-wrap">Riga 1`+"\n"+`&lt;b&gt;Riga 2&lt;/b&gt;</div>`+"\n"+`</div>`)
	nonContiene(t, "dopo", r.corpo, `id="repair-handed-over"`)
	if got := colonna(t, h.d, id, "note_artigiano"); got != "Riga 1\n<b>Riga 2</b>" {
		t.Errorf("nota %q", got)
	}
	if contaRighe(t, h.d, `SELECT count(*) FROM consegne_artigiani WHERE id = `+itoa(consegna)+` AND chiusa_il IS NOT NULL`) != 1 {
		t.Error("consegna non chiusa")
	}
	if n := contaRighe(t, h.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.returned_from_artisan' AND attore = 'banco@esempio.it'`); n != 1 {
		t.Errorf("audit %d", n)
	}
	r = b.vai("POST", azioneRientro(id), formRientro(b, "fatto", "altra"), nil)
	if r.codice != 200 {
		t.Fatalf("di nuovo: %d %q", r.codice, r.location)
	}
	contiene(t, "di nuovo", r.corpo, `<div role="alert" class="flash flash-errore">La riparazione non risulta fuori bottega.</div>`)
	nonContiene(t, "di nuovo", r.corpo, `id="repair-handed-over"`)
	if got := colonna(t, h.d, id, "note_artigiano"); got != "Riga 1\n<b>Riga 2</b>" {
		t.Errorf("nota cambiata %q", got)
	}
	// Senza nota il riquadro delle note non c'è; senza esito (il campo assente) lo stato resta.
	altra := bustaFuori(t, h, 0, `note_artigiano = 'vecchia'`)
	b.vai("GET", percorsoScheda(altra), nil, nil)
	if r := b.vai("POST", azioneRientro(altra), url.Values{"_csrf_token": {b.token}}, nil); !redirectAllaScheda(r, altra) {
		t.Fatalf("senza campi: %d %q", r.codice, r.location)
	}
	r = b.vai("GET", percorsoScheda(altra), nil, nil)
	contiene(t, "senza campi", r.corpo, "Rientro registrato.", `<dd id="repair-status" class="forte">In lavorazione</dd>`)
	nonContiene(t, "senza campi", r.corpo, `id="repair-artisan-notes"`, `id="repair-handed-over"`)
}

// inventario: RIP-S-16, ART-13
// I rifiuti tornano alla scheda con il testo di return_error_message/1 e non cambiano niente: l'esito che il
// grafo non permette, una parola che il campo non può dare. Un id malformato, una busta che non c'è o nel
// cestino portano alla home con «Riparazione non trovata.»; senza token 403, senza accesso il login; un
// errore del database «Rientro non riuscito. Nessuna modifica applicata.».
func TestRientroRifiutatoDallaScheda(t *testing.T) {
	h := nuovaProvaBuste(t)
	eseguiSQL(t, h.d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Orafo Prova')`)
	id := bustaFuori(t, h, 0, `stato = 'preventivo'`)
	b := titolareDentro(t, h.h)
	b.vai("GET", percorsoScheda(id), nil, nil)
	// Un rifiuto ridisegna la scheda con il modulo aperto, l'esito scelto e la nota scritta (nello standalone il
	// modulo resta com'era); un esito che il campo non può dare lascia scelto «fatto».
	for _, c := range []struct{ esito, messaggio, scelto string }{
		{"fatto", "Da «preventivo» non si passa a «pronta»: scegli un altro esito lavoro.", "fatto"},
		{"consegnata", "Esito lavoro non valido.", "fatto"},
	} {
		r := b.vai("POST", azioneRientro(id), formRientro(b, c.esito, "Nota <scritta>\r\nsu due righe"), nil)
		if r.codice != 200 {
			t.Fatalf("%s: %d %q", c.esito, r.codice, r.location)
		}
		contiene(t, c.esito, r.corpo, `<div role="alert" class="flash flash-errore">`+c.messaggio+`</div>`,
			`<details id="repair-return-control" class="rientro" open>`, `<option value="`+c.scelto+`" selected>`,
			`name="return[note_artigiano]" rows="3" placeholder="Annotazioni manoscritte dall'artigiano…">Nota &lt;scritta&gt;`+"\n"+`su due righe</textarea>`)
		if strings.Count(r.corpo, " selected>") != 1 {
			t.Errorf("%s: più di un esito scelto", c.esito)
		}
	}
	if got := colonna(t, h.d, id, "stato") + "|" + colonna(t, h.d, id, "rientrata_artigiano_il") + "|" + colonna(t, h.d, id, "note_artigiano"); got != "preventivo|∅|∅" {
		t.Errorf("busta cambiata: %s", got)
	}
	r := b.vai("POST", azioneRientro(id), formRientro(b, "da_rifare", ""), nil)
	if !redirectAllaScheda(r, id) {
		t.Fatalf("da rifare: %d %q", r.codice, r.location)
	}
	eseguiSQL(t, h.d, `UPDATE buste SET stato = 'preventivo', rientrata_artigiano_il = NULL WHERE id = ?`, id)
	// Il GET della scheda dopo un rifiuto torna con il modulo chiuso e «fatto» scelto.
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "chiuso", r.corpo, `<details id="repair-return-control" class="rientro">`, `<option value="fatto" selected>`,
		`placeholder="Annotazioni manoscritte dall'artigiano…"></textarea>`)
	senza := formRientro(b, "", "")
	senza.Del("_csrf_token")
	if r := b.vai("POST", azioneRientro(id), senza, nil); r.codice != 403 {
		t.Errorf("senza CSRF: %d", r.codice)
	}
	if r := nuovoBrowser(t, h.h).vai("POST", azioneRientro(id), senza, nil); r.codice != 403 {
		t.Errorf("anonimo: %d", r.codice)
	}
	cestino := bustaFuori(t, h, 0, `cancellata_il = '2026-10-30T09:00:00.000000Z'`)
	for _, p := range []string{"/repairs/999/rientro", "/repairs/abc/rientro", "/repairs/0/rientro", azioneRientro(cestino)} {
		r := b.vai("POST", p, formRientro(b, "", ""), nil)
		if !redirectA(r, "/") {
			t.Errorf("%s: %d %q", p, r.codice, r.location)
			continue
		}
		contiene(t, p, b.vai("GET", "/", nil, nil).corpo, "Riparazione non trovata.")
	}
	eseguiSQL(t, h.d, `CREATE TEMP TRIGGER no_rientro BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.returned_from_artisan' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	b.vai("GET", percorsoScheda(id), nil, nil)
	if r := b.vai("POST", azioneRientro(id), formRientro(b, "", "nota"), nil); r.codice != 200 {
		t.Fatalf("guasto: %d %q", r.codice, r.location)
	} else {
		contiene(t, "guasto", r.corpo, `<div role="alert" class="flash flash-errore">Rientro non riuscito. Nessuna modifica applicata.</div>`,
			`<details id="repair-return-control" class="rientro" open>`, `<option value="" selected>`, `>nota</textarea>`)
	}
	if got := colonna(t, h.d, id, "rientrata_artigiano_il"); got != "∅" {
		t.Errorf("rientro scritto: %s", got)
	}
	eseguiSQL(t, h.d, `UPDATE utenti SET attivo = 0 WHERE email = 'titolare@esempio.it'`)
	if r := b.vai("POST", azioneRientro(id), formRientro(b, "", ""), nil); !redirectA(r, "/login") {
		t.Errorf("disattivato: %d %q", r.codice, r.location)
	}
}

// inventario: RIP-S-16
// L'avviso prima della conferma per ogni stato delle due famiglie, deciso dagli esiti applicabili (non dal loro
// numero): niente dove si possono dare tutti, «Fatto» non applicabile da preventivo, nessun esito altrove.
func TestAvvisoDelRientro(t *testing.T) {
	fatto := func(s string) string {
		return "Da «" + s + "» l'esito «Fatto» non è applicabile: il rientro con quell'esito viene rifiutato per intero."
	}
	nessuno := func(s string) string {
		return "Da «" + s + "» nessun esito è applicabile: il rientro può solo lasciare lo stato invariato."
	}
	casi := []struct {
		stato  string
		oro    bool
		avviso string
	}{
		{"in_lavorazione", false, ""}, {"in_attesa_materiali", false, ""}, {"pronta", false, ""},
		{"preventivo", false, fatto("Preventivo")}, {"consegnata", false, nessuno("Consegnata")},
		{"annullata", false, nessuno("Annullata")}, {"in_valutazione", true, nessuno("In valutazione")},
		{"valutata", true, nessuno("Valutata")}, {"perfezionata", true, nessuno("Perfezionata")},
		{"chiusa", true, nessuno("Chiusa")}, {"annullata", true, nessuno("Annullata")},
	}
	for _, c := range casi {
		if got := avvisoDelRientro(c.stato, c.oro); got != c.avviso {
			t.Errorf("%s %v: %q", c.stato, c.oro, got)
		}
	}
}
