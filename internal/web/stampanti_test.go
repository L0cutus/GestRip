package web

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/pdf/lettura"
	"github.com/L0cutus/gestrip/internal/printing"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// La stampa diretta (slice 046, D-08 e D-09, Stefano 04/11/2026): sempre con la stampante
// finta di printing (nessun comando lanciato, nessuna coda CUPS vera).

func facciaConStampante(t *testing.T, p printing.Printer) (http.Handler, *db.DB, *clock.Finto) {
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
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti = o, ratelimit.Nuovo(o)
	dd.Buste = buste.Nuove(d, o)
	dd.IndirizzoPubblico, dd.Configurazione = indirizzoPubblicoProva, configProva
	dd.Stampanti = p
	return mustInterna(t, dd), d, o
}

func fintaProva() *printing.Finta {
	return &printing.Finta{Code: []string{"Etichette_QR", "Buste-76x127"}, Dichiarate: map[string][]printing.OpzioneCoda{
		"Etichette_QR": {{Chiave: "MediaTracking", Scelte: []string{"Continuous", "Gap", "Mark"}, Predefinita: "Gap"},
			{Chiave: "Darkness", Scelte: []string{"1", "8", "15"}, Predefinita: "8"}}}}
}

func scegliStampanti(t *testing.T, d *db.DB, qr, opzQR, busta, opzBusta string) {
	t.Helper()
	eseguiSQL(t, d, `INSERT OR REPLACE INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('stampa_coda_etichette_qr', ?, 'x'),
		('stampa_opzioni_etichette_qr', ?, 'x'), ('stampa_coda_etichetta_busta', ?, 'x'), ('stampa_opzioni_etichetta_busta', ?, 'x')`, qr, opzQR, busta, opzBusta)
}

var reInvioStampa = regexp.MustCompile(`<input name="invio" type="hidden" value="([A-Za-z0-9_-]{22})">`)

func invioStampaDi(t *testing.T, corpo string) string {
	t.Helper()
	m := reInvioStampa.FindStringSubmatch(corpo)
	if m == nil {
		t.Fatal("la pagina non ha il modulo della stampa con l'invio")
	}
	return m[1]
}

// inventario: RIP-S-48, RIP-S-43
// Impostazioni → Stampanti, solo del titolare: le due voci con «Nessuna (solo PDF)» scelta all'inizio,
// le code sceglibili di lpstat e quelle con un nome che il Go non usa; il dipendente non entra.
func TestPaginaStampanti(t *testing.T) {
	f := fintaProva()
	f.Code = append(f.Code, "Coda strana")
	h, d, _ := facciaConStampante(t, f)
	b := titolareDentro(t, h)
	r := b.vai("GET", "/settings/stampanti", nil, nil)
	if r.codice != 200 {
		t.Fatalf("%d", r.codice)
	}
	contiene(t, "pagina", r.corpo, "<h1>Stampanti</h1>", `id="settings-tab-stampanti" href="/settings/stampanti" aria-current="page"`,
		`<form id="printers-form" action="/settings/stampanti" method="post">`, "Etichette QR", "Etichetta busta", "76×127 mm",
		`<select id="printers-qr-queue" name="coda_qr">`, `<option value="" selected>Nessuna (solo PDF)</option>`,
		`<option value="Etichette_QR">Etichette_QR</option>`, `<option value="Buste-76x127">Buste-76x127</option>`,
		`name="opzioni_qr" value=""`, "«Coda strana»", "PageSize")
	nonContiene(t, "pagina", r.corpo, `<option value="Coda strana"`, "CUPS non risponde", "Opzioni di questa stampante")
	// Con una coda scelta: le opzioni dichiarate dalla stampante, come aiuto.
	scegliStampanti(t, d, "Etichette_QR", "Darkness=8", "", "")
	r = b.vai("GET", "/settings/stampanti", nil, nil)
	contiene(t, "scelta", r.corpo, `<option value="Etichette_QR" selected>Etichette_QR</option>`, `name="opzioni_qr" value="Darkness=8"`,
		"Opzioni di questa stampante", "<code>MediaTracking</code>", "<code>Gap</code>", "— ora Gap")
	dip := dipendenteDentro(t, h)
	if r := dip.vai("GET", "/settings/stampanti", nil, nil); r.codice == 200 || strings.Contains(r.corpo, "printers-form") {
		t.Errorf("dipendente: %d", r.codice)
	}
	dip.vai("GET", "/labels", nil, nil)
	if r := dip.vai("POST", "/settings/stampanti", url.Values{"_csrf_token": {dip.token}, "coda_qr": {"Etichette_QR"}}, nil); r.codice == 302 && r.location == "/settings/stampanti" {
		t.Error("il dipendente ha salvato")
	}
	if impostazioneWeb(t, d, "stampa_coda_etichetta_busta") != "" || righeAudit(t, d, "printer_settings.updated") != 0 {
		t.Error("salvato dal dipendente")
	}
}

// inventario: RIP-S-48, RIP-S-43
// Il salvataggio: coda dall'elenco o nessuna, opzioni chiave=valore controllate (forma, PageSize mai,
// scelte dichiarate dalla stampante); un errore ridisegna con i valori scritti e non salva niente;
// riuscito torna alla pagina con «Stampanti salvate.» e la riga printer_settings.updated.
func TestSalvaStampanti(t *testing.T) {
	h, d, _ := facciaConStampante(t, fintaProva())
	b := titolareDentro(t, h)
	b.vai("GET", "/settings/stampanti", nil, nil)
	invia := func(qr, oqr, busta, obusta string) risposta {
		return b.vai("POST", "/settings/stampanti", url.Values{"_csrf_token": {b.token}, "coda_qr": {qr}, "opzioni_qr": {oqr},
			"coda_busta": {busta}, "opzioni_busta": {obusta}}, nil)
	}
	for _, k := range []struct{ qr, oqr, busta, obusta, msg string }{
		{"Nessuna", "", "", "", msgCodaNonTrovata},
		{"-dAltra", "", "", "", msgCodaNonValida},
		{"Etichette_QR", "PageSize=w288h432", "", "", "la sceglie GestRip dal formato del documento"},
		{"Etichette_QR", "Darkness=8 -o", "", "", "non è nella forma chiave=valore"},
		{"Etichette_QR", "MediaTracking=Nastro", "", "", "non è una scelta di «MediaTracking»"},
		{"Etichette_QR", "Velocita=3", "", "", "la stampante non ha l'opzione «Velocita»"},
		{"", "", "Buste-76x127", "Darkness=8;id", "il valore di «Darkness» non è valido"},
	} {
		r := invia(k.qr, k.oqr, k.busta, k.obusta)
		if r.codice != http.StatusUnprocessableEntity || !strings.Contains(r.corpo, template.HTMLEscapeString(k.msg)) {
			t.Errorf("%+v: %d", k, r.codice)
		}
	}
	if impostazioneWeb(t, d, "stampa_coda_etichette_qr") != "" || righeAudit(t, d, "printer_settings.updated") != 0 {
		t.Fatal("un rifiuto ha salvato")
	}
	r := invia("Etichette_QR", "  MediaTracking=Gap   Darkness=8 ", "Buste-76x127", "")
	if !redirectA(r, "/settings/stampanti") {
		t.Fatalf("%d %q", r.codice, r.location)
	}
	if impostazioneWeb(t, d, "stampa_coda_etichette_qr") != "Etichette_QR" || impostazioneWeb(t, d, "stampa_opzioni_etichette_qr") != "MediaTracking=Gap Darkness=8" ||
		impostazioneWeb(t, d, "stampa_coda_etichetta_busta") != "Buste-76x127" || righeAudit(t, d, "printer_settings.updated") != 1 {
		t.Error("non salvate")
	}
	if a := ultimoAuditWeb(t, d, "printer_settings.updated"); !strings.Contains(a, `"queue":"Etichette_QR"`) || !strings.Contains(a, `"options":"MediaTracking=Gap Darkness=8"`) {
		t.Errorf("audit %s", a)
	}
	if r := b.vai("GET", "/settings/stampanti", nil, nil); !strings.Contains(r.corpo, msgStampantiSalvate) {
		t.Error("niente esito")
	}
	// Di nuovo «Nessuna (solo PDF)».
	if r := invia("", "", "", ""); !redirectA(r, "/settings/stampanti") || impostazioneWeb(t, d, "stampa_coda_etichette_qr") != "" {
		t.Errorf("nessuna: %d", r.codice)
	}
	// Senza CSRF niente.
	if r := b.vai("POST", "/settings/stampanti", url.Values{"coda_qr": {"Etichette_QR"}}, nil); r.codice != 403 {
		t.Errorf("senza CSRF: %d", r.codice)
	}
}

// inventario: RIP-S-48
// CUPS che non risponde: la pagina lo dice e resta usabile; si può tenere la coda salvata o scegliere
// nessuna, non una coda nuova. Senza stampa diretta (nil) la pagina lo dice e nessun pulsante compare.
func TestStampantiSenzaCUPS(t *testing.T) {
	f := fintaProva()
	h, d, _ := facciaConStampante(t, f)
	scegliStampanti(t, d, "Etichette_QR", "", "", "")
	f.ErrElenco = printing.ErrNonDisponibile
	b := titolareDentro(t, h)
	r := b.vai("GET", "/settings/stampanti", nil, nil)
	if r.codice != 200 {
		t.Fatalf("%d", r.codice)
	}
	contiene(t, "senza CUPS", r.corpo, `id="printers-cups-error"`, `Le stampanti non rispondono. L'elenco delle stampanti non c'è.`, `<option value="Etichette_QR" selected>Etichette_QR (salvata)</option>`)
	invia := func(qr, busta string) risposta {
		return b.vai("POST", "/settings/stampanti", url.Values{"_csrf_token": {b.token}, "coda_qr": {qr}, "coda_busta": {busta}, "opzioni_qr": {"Darkness=8"}}, nil)
	}
	if r := invia("Etichette_QR", "Buste-76x127"); r.codice != 422 || !strings.Contains(r.corpo, msgCodaSenzaCUPS) {
		t.Errorf("coda nuova senza CUPS: %d", r.codice)
	}
	if r := invia("Etichette_QR", ""); !redirectA(r, "/settings/stampanti") || impostazioneWeb(t, d, "stampa_opzioni_etichette_qr") != "Darkness=8" {
		t.Errorf("tenere la salvata: %d", r.codice)
	}
	// Senza la stampa diretta.
	h2, d2, _ := facciaConStampante(t, nil)
	scegliStampanti(t, d2, "Etichette_QR", "", "Buste-76x127", "")
	b2 := titolareDentro(t, h2)
	contiene(t, "senza stampa", b2.vai("GET", "/settings/stampanti", nil, nil).corpo, `id="printers-unavailable"`)
	nonContiene(t, "modulo senza stampa", b2.vai("GET", "/labels", nil, nil).corpo, "label-sheet-print")
}

// inventario: RIP-S-48
// «🖨 Stampa su <coda>» nel modulo /labels: un solo lavoro per il foglio, PageSize dai mm e le
// opzioni della coda; poi, nella stessa transazione, contatore max(ultimo, a), anno, formato e
// label_sheet.printed con coda e lavoro; /labels con «Inviate N etichette a <coda> (lavoro <id>).».
// Anche il dipendente stampa.
func TestStampaEtichetteQR(t *testing.T) {
	f := fintaProva()
	h, d, _ := facciaConStampante(t, f)
	b := dipendenteDentro(t, h)
	nonContiene(t, "senza coda", b.vai("GET", "/labels", nil, nil).corpo, "label-sheet-print", `<input name="invio"`)
	scegliStampanti(t, d, "Etichette_QR", "MediaTracking=Gap Darkness=8", "", "")
	eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('etichette_qr_ultimo', '300', 'x')`)
	r := b.vai("GET", "/labels", nil, nil)
	contiene(t, "modulo", r.corpo, `<form id="label-sheet-form" method="get" action="/labels.pdf">`+"\n"+`<label>Da`,
		`<button id="label-sheet-print-button" type="submit" formaction="/labels/stampa">🖨 Stampa su Etichette_QR</button>`,
		`<button id="label-sheet-submit">⬇ Scarica PDF</button>`)
	nonContiene(t, "modulo", r.corpo, `<input name="invio"`, "formmethod")
	// Il secondo pulsante porta, in GET e senza token, alla pagina intermedia con la domanda e il modulo POST.
	r = b.vai("GET", "/labels/stampa?da=44&a=46&anno=2026&w=40&h=20.5", nil, nil)
	contiene(t, "pagina intermedia", r.corpo, "<strong>Stampa 3 etichette (0044/26–0046/26) su Etichette_QR?</strong>",
		`<form id="label-sheet-print-form" action="/labels/stampa" method="post" data-invio-unico>`, `name="w" type="hidden" value="40"`,
		`<input name="continua" type="hidden" value="1">`, `<button id="label-sheet-print-yes" type="submit">🖨 Stampa</button>`,
		`<a id="label-sheet-print-cancel" href="/labels">Annulla</a>`)
	nonContiene(t, "pagina intermedia", r.corpo, "Nuovo anno")
	if len(f.Lavori()) != 0 || righeAudit(t, d, "label_sheet.printed") != 0 {
		t.Fatal("la pagina intermedia ha stampato")
	}
	invio := invioStampaDi(t, r.corpo)
	modulo := url.Values{"_csrf_token": {b.token}, "invio": {invio}, "da": {"44"}, "a": {"46"}, "anno": {"2026"}, "w": {"40"}, "h": {"20.5"}}
	r = b.vai("POST", "/labels/stampa", modulo, nil)
	if !redirectA(r, "/labels") {
		t.Fatalf("%d %q %s", r.codice, r.location, r.corpo)
	}
	ll := f.Lavori()
	if len(ll) != 1 || ll[0].Coda != "Etichette_QR" || ll[0].Opzioni.LarghezzaMM != 40 || ll[0].Opzioni.AltezzaMM != 20.5 ||
		printing.ScriviOpzioni(ll[0].Opzioni.Extra) != "MediaTracking=Gap Darkness=8" || ll[0].Opzioni.Titolo != "etichette-qr-2026-0044-0046.pdf" {
		t.Fatalf("%+v", ll)
	}
	l, err := lettura.Leggi(ll[0].PDF)
	if err != nil || lettura.Formato(l) != "40x20,40x20,40x20" || lettura.TestoNormalizzato(l) != "0044/26 0045/26 0046/26" {
		t.Errorf("PDF: %v %q %q", err, lettura.Formato(l), lettura.TestoNormalizzato(l))
	}
	if impostazioneWeb(t, d, "etichette_qr_ultimo") != "300" || impostazioneWeb(t, d, "etichette_qr_anno") != "2026" ||
		impostazioneWeb(t, d, "etichette_qr_w") != "40" || impostazioneWeb(t, d, "etichette_qr_h") != "20.5" {
		t.Error("contatore, anno o formato")
	}
	if a := ultimoAuditWeb(t, d, "label_sheet.printed"); !strings.Contains(a, `"queue":"Etichette_QR"`) || !strings.Contains(a, `"job":"Etichette_QR-1"`) ||
		!strings.Contains(a, `"previous":300`) || !strings.Contains(a, `"last":300`) || !strings.HasPrefix(a, "2026-0044-0046|ok|") {
		t.Errorf("audit %s", a)
	}
	if righeAudit(t, d, "label_sheet.generated") != 0 {
		t.Error("righe in più")
	}
	contiene(t, "esito", b.vai("GET", "/labels", nil, nil).corpo, "Inviate 3 etichette a Etichette_QR (lavoro Etichette_QR-1). Contatore aggiornato: ultimo numero stampato 300.")
	// Lo stesso modulo rimandato (doppio clic arrivato dopo): nessun secondo lavoro, lo stesso esito.
	r = b.vai("POST", "/labels/stampa", modulo, nil)
	if !redirectA(r, "/labels") || len(f.Lavori()) != 1 {
		t.Errorf("doppio invio: %d, %d lavori", r.codice, len(f.Lavori()))
	}
	contiene(t, "esito del doppio", b.vai("GET", "/labels", nil, nil).corpo, "(lavoro Etichette_QR-1)")
	// Un modulo nuovo dello stesso foglio è una ristampa voluta: un altro lavoro, il contatore con max.
	r = b.vai("GET", "/labels/stampa?da=301&a=302&anno=2026&w=40&h=20.5", nil, nil)
	m2 := url.Values{"_csrf_token": {b.token}, "invio": {invioStampaDi(t, r.corpo)}, "da": {"301"}, "a": {"302"}, "anno": {"2026"}, "w": {"40"}, "h": {"20.5"}}
	if r := b.vai("POST", "/labels/stampa", m2, nil); !redirectA(r, "/labels") || impostazioneWeb(t, d, "etichette_qr_ultimo") != "302" || len(f.Lavori()) != 2 {
		t.Errorf("secondo foglio: %d %d", r.codice, len(f.Lavori()))
	}
}

// inventario: RIP-S-48
// CUPS che rifiuta, che non risponde in tempo, o la coda non scelta: niente salvato, nessuna riga, e
// il modulo torna con l'errore e i valori del foglio. Le regole del PDF valgono anche qui (limite di
// 100, valori non validi, CSRF) e in più l'anno del modulo.
func TestStampaEtichetteQRRifiutata(t *testing.T) {
	f := fintaProva()
	h, d, _ := facciaConStampante(t, f)
	b := titolareDentro(t, h)
	b.vai("GET", "/labels", nil, nil)
	modulo := func(q string) url.Values {
		v, _ := url.ParseQuery(q)
		v.Set("_csrf_token", b.token)
		return v
	}
	if r := b.vai("POST", "/labels/stampa", modulo("da=1&a=2&anno=2026"), nil); r.codice != 200 || !strings.Contains(r.corpo, msgNessunaCodaQR) ||
		!strings.Contains(r.corpo, `name="da" value="1"`) || len(f.Lavori()) != 0 {
		t.Errorf("senza coda: %d", r.codice)
	}
	scegliStampanti(t, d, "Etichette_QR", "", "", "")
	for _, k := range []struct {
		err error
		msg string
	}{{printing.Rifiuto{Motivo: "lp: The printer or class does not exist."}, "la stampante ha rifiutato il lavoro (lp: The printer or class does not exist.)."},
		{printing.ErrTempoScaduto, "Il lavoro potrebbe essere partito lo stesso"},
		{printing.ErrNonDisponibile, "la stampante non risponde."}} {
		f.ErrStampa = k.err
		r := b.vai("POST", "/labels/stampa", modulo("da=5&a=7&anno=2026"), nil)
		if r.codice != 200 {
			t.Fatalf("%v: %d", k.err, r.codice)
		}
		contiene(t, k.msg, r.corpo, "Stampa non riuscita: ", k.msg, "Il contatore non è cambiato", `id="label-sheet-submit"`,
			`name="da" value="5"`, `name="a" value="7"`, `id="label-sheet-print-button"`)
	}
	f.ErrStampa = nil
	if impostazioneWeb(t, d, "etichette_qr_ultimo") != "" || righeAudit(t, d, "label_sheet.printed") != 0 || len(f.Lavori()) != 0 {
		t.Error("un rifiuto ha salvato")
	}
	for q, codice := range map[string]int{"da=1&a=101": 422, "da=5&a=2": 400, "da=1&a=2&anno=20267": 400, "w=15": 400, "da[]=1": 400} {
		if r := b.vai("POST", "/labels/stampa", modulo(q), nil); r.codice != codice {
			t.Errorf("%s: %d", q, r.codice)
		}
	}
	v := modulo("da=1&a=2")
	v.Del("_csrf_token")
	if r := b.vai("POST", "/labels/stampa", v, nil); r.codice != 403 {
		t.Errorf("senza CSRF: %d", r.codice)
	}
	if len(f.Lavori()) != 0 {
		t.Errorf("%d lavori", len(f.Lavori()))
	}
}

// inventario: RIP-S-48
// CUPS ha accettato ma la transazione non si chiude: il lavoro c'è, il contatore no, e il modulo lo
// dice; un secondo invio dello stesso modulo non rimanda il lavoro.
func TestStampaAccettataNonSalvata(t *testing.T) {
	f := fintaProva()
	h, d, _ := facciaConStampante(t, f)
	scegliStampanti(t, d, "Etichette_QR", "", "", "")
	// Solo la riga della transazione (esito ok) non si scrive: la traccia «best effort» sì.
	eseguiSQL(t, d, `CREATE TRIGGER no_stampa BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'label_sheet.printed' AND NEW.esito = 'ok' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	b := titolareDentro(t, h)
	r := b.vai("GET", "/labels/stampa?da=1&a=3&anno=2026", nil, nil)
	m := url.Values{"_csrf_token": {b.token}, "invio": {invioStampaDi(t, r.corpo)}, "da": {"1"}, "a": {"3"}, "anno": {"2026"}, "w": {"57"}, "h": {"32"}}
	r = b.vai("POST", "/labels/stampa", m, nil)
	contiene(t, "non salvata", r.corpo, "Etichette inviate a Etichette_QR (lavoro Etichette_QR-1), ma il contatore non è stato aggiornato")
	if impostazioneWeb(t, d, "etichette_qr_ultimo") != "" || impostazioneWeb(t, d, "etichette_qr_w") != "" || len(f.Lavori()) != 1 {
		t.Error("salvato a metà")
	}
	if a := ultimoAuditWeb(t, d, "label_sheet.printed"); !strings.HasPrefix(a, "2026-0001-0003|counter_not_saved|") ||
		!strings.Contains(a, `"queue":"Etichette_QR"`) || !strings.Contains(a, `"job":"Etichette_QR-1"`) {
		t.Errorf("traccia: %s", a)
	}
	// Lo stesso modulo rimandato: nessun secondo lavoro, e l'esito è un errore (non un esito verde).
	r = b.vai("POST", "/labels/stampa", m, nil)
	if r.codice != 200 || len(f.Lavori()) != 1 || !strings.Contains(r.corpo, `class="flash flash-errore">Etichette inviate a Etichette_QR (lavoro Etichette_QR-1), ma`) {
		t.Errorf("rimandato: %d, %d lavori", r.codice, len(f.Lavori()))
	}
	// Se non si scrive nemmeno la traccia resta la riga del registro, e l'esito non cambia.
	eseguiSQL(t, d, `CREATE TRIGGER no_traccia BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'label_sheet.printed' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	r = b.vai("GET", "/labels/stampa?da=4&a=5&anno=2026", nil, nil)
	m2 := url.Values{"_csrf_token": {b.token}, "invio": {invioStampaDi(t, r.corpo)}, "da": {"4"}, "a": {"5"}, "anno": {"2026"}, "w": {"57"}, "h": {"32"}}
	if r := b.vai("POST", "/labels/stampa", m2, nil); r.codice != 200 || !strings.Contains(r.corpo, "(lavoro Etichette_QR-2), ma il contatore") {
		t.Errorf("senza traccia: %d", r.codice)
	}
	if righeAudit(t, d, "label_sheet.printed") != 1 {
		t.Errorf("righe: %d", righeAudit(t, d, "label_sheet.printed"))
	}
}

// inventario: RIP-S-48, RIP-S-43
// Il tempo scaduto di lp: il lavoro potrebbe essere già nella coda, quindi lo stesso modulo rimandato
// non lo rimanda e mostra l'esito del primo come errore; niente contatore né riga. Per il foglio e per
// la busta.
func TestStampaTempoScadutoNonRimanda(t *testing.T) {
	f := fintaProva()
	h, d, o := facciaConStampante(t, f)
	id := bustaScheda(t, d, o, nil)
	scegliStampanti(t, d, "Etichette_QR", "", "Buste-76x127", "")
	b := titolareDentro(t, h)
	chiamate := 0
	var mu sync.Mutex
	f.Prima = func() { mu.Lock(); chiamate++; mu.Unlock() }
	f.ErrStampa = printing.ErrTempoScaduto
	r := b.vai("GET", "/labels/stampa?da=1&a=3&anno=2026", nil, nil)
	m := url.Values{"_csrf_token": {b.token}, "invio": {invioStampaDi(t, r.corpo)}, "da": {"1"}, "a": {"3"}, "anno": {"2026"}, "w": {"57"}, "h": {"32"}}
	for i := range 2 {
		r = b.vai("POST", "/labels/stampa", m, nil)
		if r.codice != 200 || !strings.Contains(r.corpo, `class="flash flash-errore">Stampa non riuscita: la stampante non ha risposto in tempo.`) {
			t.Errorf("foglio, invio %d: %d", i+1, r.codice)
		}
	}
	if chiamate != 1 || impostazioneWeb(t, d, "etichette_qr_ultimo") != "" || righeAudit(t, d, "label_sheet.printed") != 0 {
		t.Errorf("foglio: %d chiamate a Print", chiamate)
	}
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	mb := url.Values{"_csrf_token": {b.token}, "invio": {invioStampaDi(t, r.corpo)}}
	for i := range 2 {
		r = b.vai("POST", percorsoScheda(id)+"/stampa-etichetta", mb, nil)
		if r.codice != 302 {
			t.Fatalf("busta, invio %d: %d", i+1, r.codice)
		}
		if p := b.vai("GET", r.location, nil, nil); !strings.Contains(p.corpo, `class="flash flash-errore">Etichetta non stampata: la stampante non ha risposto in tempo.`) {
			t.Errorf("busta, invio %d: niente errore", i+1)
		}
	}
	if chiamate != 2 || righeAudit(t, d, "repair.label_printed") != 0 {
		t.Errorf("busta: %d chiamate a Print in tutto", chiamate)
	}
}

// inventario: RIP-S-43
// La busta: CUPS accetta ma la riga non si scrive; resta la traccia counter_not_saved, il modulo
// rimandato non rimanda il lavoro e l'esito è un errore.
func TestStampaEtichettaAccettataNonRegistrata(t *testing.T) {
	f := fintaProva()
	h, d, o := facciaConStampante(t, f)
	id := bustaScheda(t, d, o, nil)
	scegliStampanti(t, d, "", "", "Buste-76x127", "")
	eseguiSQL(t, d, `CREATE TRIGGER no_riga BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.label_printed' AND NEW.esito = 'ok' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	b := titolareDentro(t, h)
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	m := url.Values{"_csrf_token": {b.token}, "invio": {invioStampaDi(t, r.corpo)}}
	for i := range 2 {
		r = b.vai("POST", percorsoScheda(id)+"/stampa-etichetta", m, nil)
		if p := b.vai("GET", r.location, nil, nil); !strings.Contains(p.corpo, `class="flash flash-errore">Etichetta inviata a Buste-76x127 (lavoro Buste-76x127-1), ma la stampa non è stata registrata.`) {
			t.Errorf("invio %d: niente errore", i+1)
		}
	}
	if len(f.Lavori()) != 1 {
		t.Errorf("%d lavori", len(f.Lavori()))
	}
	if a := ultimoAuditWeb(t, d, "repair.label_printed"); !strings.Contains(a, "|counter_not_saved|") || !strings.Contains(a, `"job":"Buste-76x127-1"`) {
		t.Errorf("traccia: %s", a)
	}
}

// ricevi aspetta un valore dal canale al più 10 s: un blocco che manca fa fallire il test invece di
// lasciarlo appeso.
func ricevi[T any](t *testing.T, c <-chan T, cosa string) T {
	t.Helper()
	select {
	case v := <-c:
		return v
	case <-time.After(10 * time.Second):
		t.Fatalf("%s: niente dopo 10 s", cosa)
	}
	var zero T
	return zero
}

// inventario: RIP-S-48
// Doppio invio mentre il primo lavoro è in corso: con lo stesso modulo il secondo aspetta e mostra
// l'esito del primo; da un'altra scheda (un altro invio) dello stesso foglio il blocco lo tiene fuori.
// Un solo lavoro.
func TestStampaDoppioInvio(t *testing.T) {
	f := fintaProva()
	h, d, _ := facciaConStampante(t, f)
	scegliStampanti(t, d, "Etichette_QR", "", "", "")
	b := titolareDentro(t, h)
	r := b.vai("GET", "/labels/stampa?da=1&a=3&anno=2026", nil, nil)
	invio := invioStampaDi(t, r.corpo)
	r = b.vai("GET", "/labels/stampa?da=1&a=3&anno=2026", nil, nil)
	altro := invioStampaDi(t, r.corpo)
	modulo := func(i string) url.Values {
		return url.Values{"_csrf_token": {b.token}, "invio": {i}, "da": {"1"}, "a": {"3"}, "anno": {"2026"}, "w": {"57"}, "h": {"32"}}
	}
	entrato, via := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-via:
		default:
			close(via) // un test fallito non lascia appeso il lavoro
		}
	}()
	var una sync.Once
	f.Prima = func() { una.Do(func() { close(entrato); <-via }) }
	primo := make(chan risposta, 1)
	go func() { primo <- b.vai("POST", "/labels/stampa", modulo(invio), nil) }()
	ricevi(t, entrato, "il primo lavoro")
	// Un'altra scheda, stesso foglio: tenuta fuori.
	if r := b.vai("POST", "/labels/stampa", modulo(altro), nil); r.codice != 200 || !strings.Contains(r.corpo, template.HTMLEscapeString(msgStampaInCorso)) {
		t.Errorf("altra scheda: %d", r.codice)
	}
	// Lo stesso modulo: aspetta il primo.
	secondo := make(chan risposta, 1)
	go func() { secondo <- b.vai("POST", "/labels/stampa", modulo(invio), nil) }()
	time.Sleep(100 * time.Millisecond)
	close(via)
	r1, r2 := ricevi(t, primo, "il primo invio"), ricevi(t, secondo, "il secondo invio")
	if !redirectA(r1, "/labels") || !redirectA(r2, "/labels") || len(f.Lavori()) != 1 || righeAudit(t, d, "label_sheet.printed") != 1 {
		t.Errorf("%d %d, %d lavori", r1.codice, r2.codice, len(f.Lavori()))
	}
}

// inventario: RIP-S-43
// «🖨 Stampa etichetta» nella scheda (D-09), per titolare e dipendente, solo con la coda scelta: un
// lavoro 76×127 con le opzioni della coda, repair.label_printed con numero, coda e lavoro (e nessuna
// repair.label_generated), poi la scheda con «Etichetta inviata a <coda> (lavoro <id>).». Un rifiuto
// torna alla scheda con l'errore e niente riga.
func TestStampaEtichettaBusta(t *testing.T) {
	f := fintaProva()
	h, d, o := facciaConStampante(t, f)
	id := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	nonContiene(t, "senza coda", b.vai("GET", percorsoScheda(id), nil, nil).corpo, "repair-label-print")
	scegliStampanti(t, d, "", "", "Buste-76x127", "Darkness=8")
	for i, b := range []*browser{titolareDentro(t, h), dipendenteDentro(t, h)} {
		r := b.vai("GET", percorsoScheda(id), nil, nil)
		contiene(t, "scheda", r.corpo, `📤 Scarica etichetta</a>`+"\n"+`<form id="repair-label-print-form" action="/repairs/`+strconv.FormatInt(id, 10)+`/stampa-etichetta" method="post" class="modulo-in-riga" data-invio-unico>`,
			`<button id="repair-label-print" type="submit" class="pulsante-contorno">🖨 Stampa etichetta</button>`)
		r = b.vai("POST", percorsoScheda(id)+"/stampa-etichetta", url.Values{"_csrf_token": {b.token}, "invio": {invioStampaDi(t, r.corpo)}}, nil)
		if !redirectAllaScheda(r, id) {
			t.Fatalf("%d %q", r.codice, r.location)
		}
		ll := f.Lavori()
		if len(ll) != i+1 || ll[i].Coda != "Buste-76x127" || ll[i].Opzioni.LarghezzaMM != 76 || ll[i].Opzioni.AltezzaMM != 127 ||
			printing.ScriviOpzioni(ll[i].Opzioni.Extra) != "Darkness=8" || ll[i].Opzioni.Titolo != "etichetta_busta_1_26.pdf" {
			t.Fatalf("%+v", ll)
		}
		if l, err := lettura.Leggi(ll[i].PDF); err != nil || lettura.Formato(l) != "76x127" {
			t.Errorf("PDF %v", err)
		}
		contiene(t, "esito", b.vai("GET", r.location, nil, nil).corpo, "Etichetta inviata a Buste-76x127 (lavoro Buste-76x127-"+strconv.Itoa(i+1)+").")
	}
	if a := ultimoAuditWeb(t, d, "repair.label_printed"); !strings.Contains(a, `"bag_number":"1/26"`) || !strings.Contains(a, `"queue":"Buste-76x127"`) || !strings.Contains(a, `"job":"Buste-76x127-2"`) {
		t.Errorf("audit %s", a)
	}
	if righeAudit(t, d, "repair.label_generated") != 0 || righeAudit(t, d, "repair.label_printed") != 2 {
		t.Error("righe")
	}
	f.ErrStampa = printing.Rifiuto{Motivo: "lp: Unable to print."}
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	r = b.vai("POST", percorsoScheda(id)+"/stampa-etichetta", url.Values{"_csrf_token": {b.token}, "invio": {invioStampaDi(t, r.corpo)}}, nil)
	if r.codice != 302 {
		t.Fatalf("%d", r.codice)
	}
	contiene(t, "rifiuto", b.vai("GET", r.location, nil, nil).corpo, "Etichetta non stampata: la stampante ha rifiutato il lavoro (lp: Unable to print.).")
	if righeAudit(t, d, "repair.label_printed") != 2 {
		t.Error("riga dopo un rifiuto")
	}
	// Busta che non c'è e coda tolta.
	f.ErrStampa = nil
	if r := b.vai("POST", "/repairs/999/stampa-etichetta", url.Values{"_csrf_token": {b.token}}, nil); !redirectA(r, "/") {
		t.Errorf("busta che non c'è: %d %q", r.codice, r.location)
	}
	scegliStampanti(t, d, "", "", "", "")
	r = b.vai("POST", percorsoScheda(id)+"/stampa-etichetta", url.Values{"_csrf_token": {b.token}}, nil)
	contiene(t, "senza coda", b.vai("GET", r.location, nil, nil).corpo, template.HTMLEscapeString(msgNessunaCodaBusta))
	if len(f.Lavori()) != 2 {
		t.Errorf("%d lavori", len(f.Lavori()))
	}
}

// Il motivo mostrato per ogni errore della stampa.
func TestMotivoStampa(t *testing.T) {
	for err, atteso := range map[error]string{
		printing.Rifiuto{}: "la stampante ha rifiutato il lavoro.", printing.ErrNonDisponibile: "la stampante non risponde.",
		errors.New("altro"): "errore della stampa.",
	} {
		if s := motivoStampa(err); s != atteso {
			t.Errorf("%v: %q", err, s)
		}
	}
}

// inventario: RIP-S-44
// «📲 Condividi / Stampa» (pdf_share.js dello standalone, slice 199) in app.js: il controllo segnato
// data-share-pdf scarica il PDF e lo passa come file al menu di condivisione, con il nome del server;
// dove non si può si scarica (download=1); una condivisione annullata non fa niente. Come i test dello
// standalone (pdf_share_asset_test.exs), il contratto si fissa sul sorgente. Nessuna pagina portata lo
// usa ancora: lo standalone lo mette nel riquadro «fuori bottega» della scheda, nello storico e nella
// sessione delle consegne e nel registro d'emergenza, che arrivano con le loro slice.
func TestCondividiPDFNelloScript(t *testing.T) {
	js := string(leggiStatico("app.js"))
	contiene(t, "app.js", js, `closest("[data-share-pdf]")`, "event.preventDefault()",
		`new File([await risposta.blob()], nomePDF(risposta), {type: "application/pdf"})`,
		"navigator.canShare({files: [file]})", "await navigator.share({files: [file], title: file.name})",
		`risposta.headers.get("content-disposition")`, `fetch(indirizzo, {credentials: "same-origin"})`,
		"if (!navigator.share || !navigator.canShare) return scaricaPDF(indirizzo)",
		"if (!navigator.canShare({files: [file]})) return scaricaPDF(indirizzo)",
		`if (errore && errore.name === "AbortError") return`, `"download=1"`, `"documento.pdf"`)
}

// inventario: RIP-S-44
// «Condividi / Stampa» è davvero sulle pagine solo quando una pagina portata ha un controllo
// data-share-pdf: fino ad allora questo test si salta e la voce resta parziale (oggi lo standalone lo
// usa nel riquadro «fuori bottega», nelle consegne e nell'emergenza, non ancora portati). Quando una
// pagina lo usa, il controllo deve essere un link al download (download=1) con l'indirizzo del PDF.
func TestCondividiPDFSullePagine(t *testing.T) {
	voci, err := filePagine.ReadDir("pagine")
	if err != nil {
		t.Fatal(err)
	}
	reControllo := regexp.MustCompile(`<a [^>]*data-share-pdf="([^"]+)"[^>]*>`)
	trovati := 0
	for _, v := range voci {
		b, err := filePagine.ReadFile("pagine/" + v.Name())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "data-share-pdf") {
			continue
		}
		trovati++
		for _, m := range reControllo.FindAllStringSubmatch(string(b), -1) {
			if !strings.Contains(m[0], "download=1") {
				t.Errorf("%s: controllo senza il download: %s", v.Name(), m[0])
			}
		}
	}
	if trovati == 0 {
		t.Skip("nessuna pagina portata usa ancora data-share-pdf (RIP-S-44 parziale)")
	}
}

// inventario: RIP-S-48
// D-66 anche per la stampa diretta, nella stessa pagina intermedia: con il foglio di un anno più nuovo,
// prima della domanda la proposta (azzeramento per il titolare, avviso per il dipendente) e il pulsante
// «Continua senza azzerare» (o «Continua»); la pagina non stampa e non salva. Il POST della pagina
// porta continua=1 e stampa una volta sola; un POST forgiato senza continua torna alla pagina.
func TestStampaNuovoAnno(t *testing.T) {
	f := fintaProva()
	h, d, _ := facciaConStampante(t, f)
	scegliStampanti(t, d, "Etichette_QR", "", "", "")
	b := titolareDentro(t, h)
	b.vai("GET", "/labels.pdf?da=1&a=10&anno=2026", nil, nil)
	r := b.vai("GET", "/labels/stampa?da=1&a=5&anno=2027", nil, nil)
	contiene(t, "titolare", r.corpo, "Nuovo anno: ripartire da 1?", `id="label-sheet-new-year-reset" type="submit">Azzera contatore</button>`,
		"<strong>Stampa 5 etichette (0001/27–0005/27) su Etichette_QR?</strong>", `<input name="continua" type="hidden" value="1">`,
		">Continua senza azzerare: 🖨 Stampa</button>", `<a id="label-sheet-print-cancel" href="/labels">Annulla</a>`)
	if strings.Index(r.corpo, "Nuovo anno: ripartire da 1?") > strings.Index(r.corpo, "Stampa 5 etichette") {
		t.Error("la proposta non viene prima della domanda")
	}
	dip := dipendenteDentro(t, h)
	rd := dip.vai("GET", "/labels/stampa?da=1&a=5&anno=2027", nil, nil)
	contiene(t, "dipendente", rd.corpo, "Nuovo anno: chiedi al titolare di azzerare il contatore", ">Continua: 🖨 Stampa</button>")
	nonContiene(t, "dipendente", rd.corpo, "Azzera contatore")
	if len(f.Lavori()) != 0 || impostazioneWeb(t, d, "etichette_qr_anno") != "2026" {
		t.Fatalf("lavoro o anno dalla pagina: %d", len(f.Lavori()))
	}
	// Un POST senza continua (forgiato) torna alla pagina intermedia, senza stampare.
	m := url.Values{"_csrf_token": {b.token}, "invio": {invioStampaDi(t, r.corpo)}, "da": {"1"}, "a": {"5"}, "anno": {"2027"}, "w": {"57"}, "h": {"32"}}
	if r := b.vai("POST", "/labels/stampa", m, nil); r.codice != 200 || !strings.Contains(r.corpo, "Continua senza azzerare: 🖨 Stampa") || len(f.Lavori()) != 0 {
		t.Errorf("senza continua: %d, %d lavori", r.codice, len(f.Lavori()))
	}
	m.Set("continua", "1")
	m.Set("_csrf_token", b.token)
	for range 2 {
		if r := b.vai("POST", "/labels/stampa", m, nil); !redirectA(r, "/labels") {
			t.Errorf("continua: %d", r.codice)
		}
	}
	if len(f.Lavori()) != 1 || impostazioneWeb(t, d, "etichette_qr_anno") != "2027" || impostazioneWeb(t, d, "etichette_qr_ultimo") != "10" {
		t.Errorf("dopo: %d lavori, anno %q", len(f.Lavori()), impostazioneWeb(t, d, "etichette_qr_anno"))
	}
}

// inventario: RIP-S-48
// La pagina intermedia rifà le regole del PDF (400, 422) e vuole l'anno del modulo; senza coda torna al
// modulo con il messaggio; non stampa e non salva niente.
func TestPaginaStampaRifiuti(t *testing.T) {
	f := fintaProva()
	h, d, _ := facciaConStampante(t, f)
	b := dipendenteDentro(t, h)
	if r := b.vai("GET", "/labels/stampa?da=1&a=2", nil, nil); r.codice != 200 || !strings.Contains(r.corpo, template.HTMLEscapeString(msgNessunaCodaQR)) {
		t.Errorf("senza coda: %d", r.codice)
	}
	scegliStampanti(t, d, "Etichette_QR", "", "", "")
	for q, codice := range map[string]int{"da=1&a=101": 422, "da=5&a=2": 400, "da=1&a=2&anno=20267": 400, "w=15": 400, "da[]=1": 400} {
		if r := b.vai("GET", "/labels/stampa?"+q, nil, nil); r.codice != codice {
			t.Errorf("%s: %d", q, r.codice)
		}
	}
	if len(f.Lavori()) != 0 || impostazioneWeb(t, d, "etichette_qr_ultimo") != "" {
		t.Error("stampato o salvato")
	}
}
