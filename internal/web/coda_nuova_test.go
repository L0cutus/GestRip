package web

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Le foto scelte prima di generare la busta (RIP-H-40, slice 040b), come la sezione #intake-photos
// di WorkshopLive, push_intake_photos/3 e photo_queue.js dello standalone (go-baseline), su account e
// buste veri. I casi vengono da workshop_photo_live_test.exs. Le foto non passano mai dal server di
// «Nuova riparazione»: restano nel browser e la coda (app.js) le manda una alla volta a
// POST /repairs/{id}/photos (slice 039) dopo il salvataggio. Qui si prova la parte del server: la
// sezione, la chiave del modulo e l'avviso alla coda nella pagina dell'esito. Il comportamento nel
// browser lo prova lo scenario coda del banco (bench/browser/coda.json).

// reChiaveSezione trova la chiave del modulo sulla sezione e nel suo campo nascosto.
var (
	reChiaveSezione = regexp.MustCompile(`<section id="intake-photos" data-intake-key="([A-Za-z0-9_-]{22})"`)
	reChiaveCampo   = regexp.MustCompile(`<input type="hidden" name="intake_key" value="([A-Za-z0-9_-]{22})">`)
)

// chiaveDi è la chiave del modulo della pagina: la stessa sulla sezione e nel campo.
func chiaveDi(t *testing.T, corpo string) string {
	t.Helper()
	s, c := reChiaveSezione.FindStringSubmatch(corpo), reChiaveCampo.FindStringSubmatch(corpo)
	if s == nil || c == nil {
		t.Fatalf("il modulo non ha la chiave delle foto (sezione %v, campo %v)", s != nil, c != nil)
	}
	if s[1] != c[1] {
		t.Fatalf("chiave della sezione %q diversa da quella del campo %q", s[1], c[1])
	}
	return s[1]
}

// avvisoFoto è l'avviso alla coda nella pagina dell'esito (intake-photos-saved dello standalone).
func avvisoFoto(id int64, numero, chiave string) string {
	return `<div id="intake-photos-saved" hidden data-repair-id="` + strconv.FormatInt(id, 10) +
		`" data-bag-number="` + numero + `" data-intake-key="` + chiave + `"></div>`
}

// inventario: RIP-H-40, RIP-H-30
// «Nuova riparazione» ha la sezione delle foto dello standalone, dentro il modulo prima di «Genera
// busta»: «Fotocamera» (capture="environment") e «Galleria», due campi file multipli senza nome (le
// foto restano nel browser, il modulo non le manda), gli stessi formati, il conteggio nascosto
// intake_photos a 0, lo stato (role=status) e gli errori (role=alert), i testi copiati. Senza
// JavaScript la sezione è nascosta: le foto le manda solo la coda. Ogni modulo ha la sua chiave
// casuale, sulla sezione (data-intake-key) e nel campo nascosto intake_key, diversa a ogni pagina e
// dall'identificativo dell'invio.
func TestNuovaSezioneFoto(t *testing.T) {
	h, _ := facciaConBuste(t, nil)
	for _, b := range []*browser{titolareDentro(t, h), dipendenteDentro(t, h)} {
		r := b.vai("GET", "/nuova", nil, nil)
		chiave := chiaveDi(t, r.corpo)
		contiene(t, "sezione delle foto", r.corpo,
			`<section id="intake-photos" data-intake-key="`+chiave+`" aria-label="Foto della nuova busta" class="foto-accettazione" hidden>`,
			"<h3>Foto della busta (facoltative)</h3>",
			"<p class=\"sotto\">Puoi scattare o scegliere le foto prima di generare la busta: si caricano\ndopo, mentre prepari la prossima. Massimo 2 per selezione, JPEG/PNG/WebP,\n25 MiB ciascuna.</p>",
			`<input type="hidden" name="intake_photos" value="0" data-intake-count>`,
			`<input type="hidden" name="intake_key" value="`+chiave+`">`,
			`<label id="intake-camera-control" class="scelta-foto">Fotocamera
<input id="intake-camera-input" type="file" accept=".jpg,.jpeg,.png,.webp,image/jpeg,image/png,image/webp" capture="environment" multiple>
</label>`,
			`<label id="intake-gallery-control" class="scelta-foto">Galleria
<input id="intake-gallery-input" type="file" accept=".jpg,.jpeg,.png,.webp,image/jpeg,image/png,image/webp" multiple>
</label>`,
			`<div id="intake-photo-status" role="status" aria-live="polite" class="stato-foto"></div>`,
			`<div id="intake-photo-errors" role="alert" class="errori-foto"></div>`)
		// Dentro il modulo, prima del pulsante.
		modulo := r.corpo[strings.Index(r.corpo, `<form id="new-repair-form"`):]
		modulo = modulo[:strings.Index(modulo, "</form>")]
		if i, j := strings.Index(modulo, `<section id="intake-photos"`), strings.Index(modulo, `<button type="submit">Genera busta`); i < 0 || j < i {
			t.Errorf("la sezione non sta nel modulo prima di «Genera busta» (%d, %d)", i, j)
		}
		// I campi file non hanno nome: il modulo non manda le foto.
		if regexp.MustCompile(`<input id="intake-(camera|gallery)-input"[^>]* name=`).MatchString(r.corpo) {
			t.Error("un campo file delle foto ha un nome")
		}
		if chiave == invioDi(t, r.corpo) {
			t.Error("la chiave delle foto è l'identificativo dell'invio")
		}
		if altra := chiaveDi(t, b.vai("GET", "/nuova", nil, nil).corpo); altra == chiave {
			t.Error("due moduli con la stessa chiave delle foto")
		}
	}
}

// inventario: RIP-H-40
// La chiave è del modulo, come intake_key nel socket dello standalone: resta dopo l'avviso del QR e
// dopo un salvataggio rifiutato (le foto scelte aspettano la stessa busta), il modulo dopo un
// salvataggio ne ha una nuova. Una chiave mandata male o mancante non si ripete: il server ne dà una
// nuova, e nel browser le foto scelte sotto quella vecchia si tolgono.
func TestNuovaChiaveDelModulo(t *testing.T) {
	h, _ := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	chiave := chiaveDi(t, b.vai("GET", "/nuova", nil, nil).corpo)
	v := con(bustaProva(b), "intake_key", chiave)
	r := b.vai("POST", "/nuova", con(v, "repair[qr_code]", ""), nil)
	if !strings.Contains(r.corpo, `id="intake-qr-warning"`) || chiaveDi(t, r.corpo) != chiave {
		t.Errorf("dopo l'avviso del QR la chiave cambia")
	}
	r = b.vai("POST", "/nuova", con(v, "repair[customer_name]", " "), nil)
	if r.codice != 200 || chiaveDi(t, r.corpo) != chiave {
		t.Errorf("dopo un rifiuto la chiave cambia: %d", r.codice)
	}
	r = b.vai("POST", "/nuova", con(con(v, "repair[description]", ""), "repair[qr_code]", "Q2"), nil)
	if r.codice != 200 || chiaveDi(t, r.corpo) != chiave {
		t.Errorf("dopo un errore di un campo la chiave cambia: %d", r.codice)
	}
	for _, male := range []string{"", "corta", chiave + "x", strings.Repeat("!", 22)} {
		r = b.vai("POST", "/nuova", con(con(v, "intake_key", male), "repair[customer_name]", " "), nil)
		if got := chiaveDi(t, r.corpo); got == male || got == chiave {
			t.Errorf("chiave %q: il modulo ripete %q", male, got)
		}
	}
	senza := con(bustaProva(b), "repair[customer_name]", " ")
	if r = b.vai("POST", "/nuova", senza, nil); chiaveDi(t, r.corpo) == "" {
		t.Error("senza chiave il modulo non ne ha una")
	}
	if r = b.vai("POST", "/nuova", v, nil); !creata(r) {
		t.Fatalf("salvataggio: %d", r.codice)
	}
	if dopo := chiaveDi(t, b.vai("GET", "/nuova", nil, nil).corpo); dopo == chiave {
		t.Error("il modulo dopo il salvataggio ha la stessa chiave")
	}
}

// inventario: RIP-H-40
// Una busta salvata con delle foto scelte (intake_photos > 0) dice alla coda dove mandarle: la
// pagina dell'esito, una volta, ha l'avviso con l'id della busta (non quello del cliente), il
// numero e la chiave del modulo salvato, e nient'altro. Il salvataggio non allega niente. La chiave
// non va nell'indirizzo del redirect, nel registro o nell'audit; un'altra sessione non vede
// l'avviso. Lo stesso modulo mandato due volte porta allo stesso avviso, con la sua chiave.
func TestNuovaAvvisoAllaCoda(t *testing.T) {
	var reg bytes.Buffer
	h, d := facciaConBuste(t, &reg)
	eseguiSQL(t, d, `INSERT INTO clienti (nome, telefono, creato_il, aggiornato_il) VALUES
		('Primo', '', '2026-10-31T09:00:00.000000Z', '2026-10-31T09:00:00.000000Z'),
		('Secondo', '', '2026-10-31T09:00:00.000000Z', '2026-10-31T09:00:00.000000Z')`)
	b := dipendenteDentro(t, h)
	altra := dipendenteDentro(t, h)
	modulo := b.vai("GET", "/nuova", nil, nil).corpo
	chiave := chiaveDi(t, modulo)
	v := con(con(con(bustaProva(b), "intake_key", chiave), "intake_photos", "2"), "invio", invioDi(t, modulo))
	r := b.vai("POST", "/nuova", v, nil)
	if !creata(r) {
		t.Fatalf("salvataggio: %d %q", r.codice, r.location)
	}
	if strings.Contains(r.location, chiave) || strings.Contains(r.corpo, chiave) {
		t.Error("la chiave è nel redirect")
	}
	var id, cliente int64
	if err := d.Lettura.QueryRow(`SELECT id, cliente_id FROM buste`).Scan(&id, &cliente); err != nil {
		t.Fatal(err)
	}
	if id == cliente {
		t.Fatalf("busta e cliente con lo stesso id %d: il test non distingue", id)
	}
	// Il doppio invio dello stesso modulo porta allo stesso esito.
	if r2 := b.vai("POST", "/nuova", v, nil); r2.location != r.location {
		t.Errorf("doppio invio: %q, atteso %q", r2.location, r.location)
	}
	if o := altra.vai("GET", r.location, nil, nil); strings.Contains(o.corpo, "intake-photos-saved") {
		t.Error("un'altra sessione vede l'avviso")
	}
	pagina := b.vai("GET", r.location, nil, nil)
	contiene(t, "esito", pagina.corpo, "Riparazione 1/26 creata.", avvisoFoto(id, "1/26", chiave))
	if strings.Count(pagina.corpo, chiave) != 1 {
		t.Errorf("la chiave compare %d volte nella pagina dell'esito", strings.Count(pagina.corpo, chiave))
	}
	if again := b.vai("GET", r.location, nil, nil); strings.Contains(again.corpo, "intake-photos-saved") {
		t.Error("l'avviso si vede due volte")
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM foto_buste`); n != 0 {
		t.Errorf("il salvataggio ha allegato %d foto", n)
	}
	var n int
	if err := d.Lettura.QueryRow(`SELECT count(*) FROM eventi_audit WHERE instr(coalesce(dati, ''), ?) > 0 OR instr(coalesce(id_risorsa, ''), ?) > 0`,
		chiave, chiave).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("la chiave è in %d righe d'audit", n)
	}
	if strings.Contains(reg.String(), chiave) {
		t.Error("la chiave è nel registro")
	}
}

// inventario: RIP-H-40
// Nessuna foto, nessun avviso: intake_photos assente, vuoto, zero, negativo o non un intero
// (Integer.parse/1 con il resto vuoto e un valore positivo). Con un intero positivo, anche con il
// segno o molto grande, l'avviso c'è. Le buste si salvano tutte.
func TestNuovaNessunaFotoNessunAvviso(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	casi := []struct {
		valore string
		manda  bool
		avviso bool
	}{
		{"", false, false}, {"0", true, false}, {"", true, false}, {"due", true, false}, {"-1", true, false},
		{" 2", true, false}, {"2 ", true, false}, {"1.0", true, false}, {"0x1", true, false}, {"-0", true, false}, {"00", true, false}, {"+0", true, false}, {"+", true, false},
		{"1", true, true}, {"2", true, true}, {"+2", true, true}, {"007", true, true}, {"99999999999999999999999", true, true},
	}
	for i, c := range casi {
		chiave := chiaveDi(t, b.vai("GET", "/nuova", nil, nil).corpo)
		v := con(con(bustaProva(b), "intake_key", chiave), "repair[bag_number]", strconv.Itoa(i+1)+"/26")
		if c.manda {
			v.Set("intake_photos", c.valore)
		}
		r := b.vai("POST", "/nuova", v, nil)
		if !creata(r) {
			t.Fatalf("%q: salvataggio %d", c.valore, r.codice)
		}
		if got := strings.Contains(b.vai("GET", r.location, nil, nil).corpo, `id="intake-photos-saved"`); got != c.avviso {
			t.Errorf("intake_photos %q (mandato %v): avviso %v, atteso %v", c.valore, c.manda, got, c.avviso)
		}
	}
	if n := contaBuste(t, d); n != len(casi) {
		t.Errorf("buste %d, attese %d", n, len(casi))
	}
	// Più valori: vale l'ultimo, come i parametri di Phoenix.
	chiave := chiaveDi(t, b.vai("GET", "/nuova", nil, nil).corpo)
	v := con(con(bustaProva(b), "intake_key", chiave), "repair[bag_number]", "50/26")
	v["intake_photos"] = []string{"2", "0"}
	r := b.vai("POST", "/nuova", v, nil)
	if strings.Contains(b.vai("GET", r.location, nil, nil).corpo, `id="intake-photos-saved"`) {
		t.Error("con «2» e poi «0» l'avviso c'è")
	}
}

// inventario: RIP-H-40
// Un salvataggio rifiutato non manda niente e tiene il modulo con la stessa chiave: le foto
// aspettano la busta. Il salvataggio dopo dà l'avviso con quella chiave. Un utente disattivato prima
// del salvataggio va al login: nessuna busta, nessun avviso.
func TestNuovaRifiutoNienteFoto(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	chiave := chiaveDi(t, b.vai("GET", "/nuova", nil, nil).corpo)
	v := con(con(bustaProva(b), "intake_key", chiave), "intake_photos", "1")
	r := b.vai("POST", "/nuova", con(v, "repair[customer_name]", " "), nil)
	if r.codice != 200 || strings.Contains(r.corpo, "intake-photos-saved") || contaBuste(t, d) != 0 {
		t.Fatalf("rifiuto: %d, buste %d", r.codice, contaBuste(t, d))
	}
	contiene(t, "modulo dopo il rifiuto", r.corpo, `<section id="intake-photos" data-intake-key="`+chiave+`"`)
	if r = b.vai("POST", "/nuova", v, nil); !creata(r) {
		t.Fatalf("salvataggio: %d", r.codice)
	}
	var id int64
	if err := d.Lettura.QueryRow(`SELECT id FROM buste`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	contiene(t, "esito", b.vai("GET", r.location, nil, nil).corpo, avvisoFoto(id, "1/26", chiave))

	dip := dipendenteDentro(t, h)
	chiave = chiaveDi(t, dip.vai("GET", "/nuova", nil, nil).corpo)
	eseguiSQL(t, d, `UPDATE utenti SET attivo = 0 WHERE email = 'banco@esempio.it'`)
	v = con(con(con(bustaProva(dip), "intake_key", chiave), "intake_photos", "1"), "repair[bag_number]", "2/26")
	if r = dip.vai("POST", "/nuova", v, nil); r.codice != 302 || r.location != "/login" || contaBuste(t, d) != 1 {
		t.Errorf("disattivato: %d %q, buste %d", r.codice, r.location, contaBuste(t, d))
	}
}

// inventario: RIP-H-40
// La striscia della coda (#photo-queue) è figlia del body sulle pagine del personale, nascosta finché
// la coda è vuota, e non c'è sul login né sulle pagine pubbliche.
func TestStrisciaDellaCoda(t *testing.T) {
	h, _ := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	striscia := `<div id="photo-queue" role="status" aria-live="polite" hidden class="coda-foto"></div>`
	for _, p := range []string{"/", "/nuova", "/settings/utenti", "/audit"} {
		r := b.vai("GET", p, nil, nil)
		if r.codice != 200 {
			t.Fatalf("%s: %d", p, r.codice)
		}
		if strings.Count(r.corpo, `id="photo-queue"`) != 1 {
			t.Errorf("%s: la striscia compare %d volte", p, strings.Count(r.corpo, `id="photo-queue"`))
		}
		contiene(t, p, r.corpo, striscia)
		// Fuori da <main>: la pagina che cambia non la ridisegna.
		if i, j := strings.Index(r.corpo, "</main>"), strings.Index(r.corpo, `id="photo-queue"`); j < i {
			t.Errorf("%s: la striscia sta dentro <main>", p)
		}
	}
	fuori := nuovoBrowser(t, h)
	if r := fuori.vai("GET", "/login", nil, nil); strings.Contains(r.corpo, "photo-queue") {
		t.Error("il login ha la striscia della coda")
	}
}

// inventario: RIP-H-40
// La coda è in app.js (photo_queue.js dello standalone): i testi copiati, i limiti, l'invio a
// /repairs/{id}/photos con x-csrf-token e x-file-name, gli errori definitivi (413, 415, 422) e la
// conferma prima di uscire con foto in coda. Il comportamento lo prova lo scenario coda del banco.
func TestCodaNelloScript(t *testing.T) {
	js := string(leggiStatico("app.js"))
	contiene(t, "app.js", js,
		"const MAX_BYTES = 25 * 1024 * 1024", "const PER_SOURCE = 2",
		`".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".webp": "image/webp"`,
		`"Formato non supportato."`, `"Foto oltre il limite di 25 MiB."`, `"Foto vuota."`,
		`"Le foto scelte prima sono state tolte: scegli di nuovo quelle di questa busta."`,
		`"Massimo 2 foto per volta."`, "`${ready} per la busta.`", `"1 foto pronta"`, "`${staged.length} foto pronte`",
		"`Anteprima di ${photo.file.name}`", "`Rimuovi ${photo.file.name}`", `"Rimuovi"`,
		`"accesso scaduto, rientra e riprova"`, `"foto oltre il limite di 25 MiB"`, `"formato non supportato"`,
		`"foto rifiutata"`, `"busta non trovata"`, "`errore ${xhr.status}`",
		"new Set([413, 415, 422])", `"connessione assente"`, `"invio interrotto"`, `"tempo scaduto"`,
		"xhr.timeout = 300000", "`/repairs/${encodeURIComponent(job.repairId)}/photos`",
		`xhr.setRequestHeader("x-csrf-token", csrfToken())`,
		`xhr.setRequestHeader("x-file-name", encodeURIComponent((file.name || "").slice(-200)))`,
		"`❌ Busta ${job.bag}: ${sent}/${total} foto caricate, ${job.error}.`", `"Riprova"`,
		"`Lascia perdere le foto della busta ${job.bag}`",
		"`❌ Busta ${job.bag}: ${sent}/${total} foto caricate, ${refused} (${[...new Set(job.rejected)].join(\", \")}).`",
		"`📷 Busta ${job.bag}: ${sent}/${total} foto, ${percent}%`", "`Foto della busta ${job.bag}`",
		`"beforeunload"`, `event.returnValue = ""`)
}

// inventario: RIP-H-40, RIP-S-29
// Le pagine cambiate senza ricaricare (giro della coda, caricamento dalla scheda): un salto d'ancora
// (il visualizzatore) non ricarica la pagina, che ha il segno monouso nell'indirizzo; il login o una
// pagina senza la striscia della coda si aprono con una navigazione normale; una risposta del
// caricamento che non è la scheda o la lista dopo il redirect apre la scheda; durante l'invio
// «Fotocamera» e «Galleria» sono spenti. Nel browser lo provano gli scenari coda e caricamento.
func TestCambioPaginaNelloScript(t *testing.T) {
	js := string(leggiStatico("app.js"))
	contiene(t, "app.js", js,
		"if (url.pathname + url.search === paginaMostrata) return",
		"paginaMostrata = location.pathname + location.search",
		`if (new URL(risposta.url).pathname === "/login" || !doc.getElementById("photo-queue")) {`,
		`xhr.status === 200 && tipo.startsWith("text/html") && (arrivo === scheda || arrivo === "/")`,
		"window.location.assign(scheda)", "comandi.forEach(c => { c.disabled = true })",
		"const corpoInvio = new FormData(form)", "el.focus({preventScroll: true})")
	nonContiene(t, "app.js", js, "window.location.assign(xhr.responseURL")
}

// inventario: RIP-H-40
// La chiave del modulo, l'id e il numero della busta finiscono nell'HTML escapati come ogni valore
// del template: un numero con caratteri speciali non rompe l'avviso.
func TestAvvisoFotoEscapato(t *testing.T) {
	var buf bytes.Buffer
	d := datiPagina{Dati: datiHome{Foto: &fotoDaMandare{ID: 7, Numero: `1/26"><x`, Chiave: "abc"}}}
	if err := pagine.ExecuteTemplate(&buf, "avvisoFoto", d.Dati); err != nil {
		t.Fatal(err)
	}
	want := `<div id="intake-photos-saved" hidden data-repair-id="7" data-bag-number="1/26&#34;&gt;&lt;x" data-intake-key="abc"></div>`
	if !strings.Contains(buf.String(), want) {
		t.Errorf("avviso = %q", buf.String())
	}
}
