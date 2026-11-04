package web

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/pdf/lettura"
)

// Le etichette QR prenumerate (RIP-S-47, RIP-S-48), come LabelSheetController e i suoi test dello
// standalone (label_sheet_controller_test.exs, slice 210), con D-08, D-21/D-64, D-22 e D-66
// (Stefano, 04/11/2026). L'orologio dei test è al 31/10/2026.

func impostazioneWeb(t *testing.T, d *db.DB, chiave string) string {
	t.Helper()
	var v string
	_ = d.Lettura.QueryRow(`SELECT valore FROM impostazioni WHERE chiave = ?`, chiave).Scan(&v)
	return v
}

func righeAudit(t *testing.T, d *db.DB, azione string) int {
	t.Helper()
	return contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = '`+azione+`'`)
}

// inventario: RIP-S-47
// Il modulo dello standalone con i valori iniziali, per titolare e dipendente; la voce di menu.
func TestModuloEtichetteQR(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	r := b.vai("GET", "/labels", nil, nil)
	if r.codice != 200 {
		t.Fatalf("%d", r.codice)
	}
	contiene(t, "modulo", r.corpo, "🏷️ Etichette QR", "Genera etichette pre-numerate", "RIP-2026-NNNN",
		"Massimo 100 etichette per volta.", `id="label-sheet-form" method="get" action="/labels/genera"`,
		`name="da" value="1" min="1" max="9999" required`, `name="a" value="50" min="1" max="9999" required`,
		`name="anno" value="2026" min="2024" max="2099" required`, `name="w" step="0.5" value="57" min="16" max="200" required`,
		`name="h" step="0.5" value="32" min="16" max="200" required`, "Genera PDF", "overlapping", `href="/labels"`)
	nonContiene(t, "modulo", r.corpo, "Ultimo numero stampato", "Formato piccolo", "200 etichette", "Azzera contatore", "Nuovo anno")
	eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('etichette_qr_ultimo', '120', 'x'),
		('etichette_qr_w', '18', 'x'), ('etichette_qr_h', '32.5', 'x')`)
	r = b.vai("GET", "/labels", nil, nil)
	contiene(t, "dopo 120", r.corpo, "📌 Ultimo numero stampato: <strong>120</strong>", `name="da" value="121"`, `name="a" value="170"`,
		`name="w" step="0.5" value="18"`, `name="h" step="0.5" value="32.5"`, "⚠ Formato piccolo")
	nonContiene(t, "dipendente", r.corpo, "Azzera contatore", `action="/labels/azzera"`)
	tit := titolareDentro(t, h)
	r = tit.vai("GET", "/labels", nil, nil)
	contiene(t, "titolare", r.corpo, `<form id="label-sheet-reset" action="/labels/azzera" method="post">`,
		`<button id="label-sheet-reset-button" type="submit">Azzera contatore</button>`)
}

// inventario: RIP-S-48
// Il PDF: allegato con il nome dello standalone, una pagina per etichetta, il QR RIP-AAAA-NNNN e sotto
// «0045/26» (D-21, D-64); salva formato e label_sheet.generated ma non il contatore (D-08).
func TestPDFEtichetteQR(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	r := b.vai("GET", "/labels.pdf?da=44&a=46&w=40&h=20", nil, nil)
	if r.codice != 200 || r.h.Get("Content-Type") != "application/pdf" || r.h.Get("Cache-Control") != "private, no-store" ||
		r.h.Get("Content-Disposition") != `attachment; filename="etichette-qr-2026-0044-0046.pdf"` {
		t.Fatalf("%d %v", r.codice, r.h)
	}
	l, err := lettura.Leggi([]byte(r.corpo))
	if err != nil || lettura.Formato(l) != "40x20,40x20,40x20" || lettura.TestoNormalizzato(l) != "0044/26 0045/26 0046/26" {
		t.Fatalf("%v %q %q", err, lettura.Formato(l), lettura.TestoNormalizzato(l))
	}
	qq, err := lettura.CodiciQR(l.Pagine[1])
	if err != nil || len(qq) != 1 || qq[0].Testo != "RIP-2026-0045" || l.Pagine[1].Righe[0].Y < qq[0].Y+qq[0].A {
		t.Errorf("QR %v %+v, numero %+v", err, qq, l.Pagine[1].Righe)
	}
	if v := impostazioneWeb(t, d, "etichette_qr_ultimo"); v != "" {
		t.Errorf("il PDF ha salvato il contatore: %q (D-08)", v)
	}
	if impostazioneWeb(t, d, "etichette_qr_w") != "40" || impostazioneWeb(t, d, "etichette_qr_h") != "20" || righeAudit(t, d, "label_sheet.generated") != 1 {
		t.Error("formato o riga d'audit non salvati")
	}
	// Senza parametri: 1-50 dell'anno di oggi, 57×32 anche con un altro formato salvato.
	r = b.vai("GET", "/labels.pdf", nil, nil)
	if r.h.Get("Content-Disposition") != `attachment; filename="etichette-qr-2026-0001-0050.pdf"` || impostazioneWeb(t, d, "etichette_qr_w") != "57" {
		t.Errorf("default: %v", r.h)
	}
	// Numeri oltre 9999: quattro cifre almeno, mai tagliati.
	r = b.vai("GET", "/labels.pdf?da=9999&a=10000&anno=2027", nil, nil)
	l, _ = lettura.Leggi([]byte(r.corpo))
	if r.h.Get("Content-Disposition") != `attachment; filename="etichette-qr-2027-9999-10000.pdf"` || lettura.TestoNormalizzato(l) != "9999/27 10000/27" {
		t.Errorf("oltre 9999: %v %q", r.h, lettura.TestoNormalizzato(l))
	}
	if qq, _ := lettura.CodiciQR(l.Pagine[1]); len(qq) != 1 || qq[0].Testo != "RIP-2027-10000" {
		t.Errorf("QR oltre 9999: %+v", qq)
	}
}

// inventario: RIP-S-48
// Rifiuti: 400 «Richiesta non valida» senza tipo; oltre 100 il modulo con la richiesta e il messaggio
// (422); niente PDF né salvataggi, sul PDF, sulla pagina del PDF pronto e sulla conferma.
func TestEtichetteQRRifiuti(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('etichette_qr_ultimo', '300', 'x')`)
	for _, q := range []string{"da=5&a=2", "w=15", "h=201", "da=abc", "anno=", "w=nan", "da[]=1", "da[a]=1", "w=1e400"} {
		for _, p := range []string{"/labels.pdf?", "/labels/genera?"} {
			r := b.vai("GET", p+q, nil, nil)
			if r.codice != 400 || r.corpo != "Richiesta non valida" || r.h.Get("Content-Type") != "" || r.h.Get("Cache-Control") != cacheBase {
				t.Errorf("%s%s: %d %q %v", p, q, r.codice, r.corpo, r.h)
			}
		}
		b.vai("GET", "/labels", nil, nil)
		v, _ := url.ParseQuery(q)
		v.Set("_csrf_token", b.token)
		if r := b.vai("POST", "/labels/conferma", v, nil); r.codice != 400 || r.corpo != "Richiesta non valida" {
			t.Errorf("conferma %s: %d %q", q, r.codice, r.corpo)
		}
	}
	for _, p := range []string{"/labels.pdf?", "/labels/genera?"} {
		r := b.vai("GET", p+"da=301&a=401&anno=2027&w=40&h=20", nil, nil)
		if r.codice != 422 || r.h.Get("Content-Disposition") != "" {
			t.Errorf("%s 101: %d", p, r.codice)
		}
		contiene(t, p+" 101", r.corpo, `id="label-sheet-too-many"`, "Massimo 100 etichette per volta: hai chiesto 101.",
			`name="da" value="301"`, `name="a" value="401"`, `name="anno" value="2027"`, `name="w" step="0.5" value="40"`,
			"📌 Ultimo numero stampato: <strong>300</strong>")
	}
	b.vai("GET", "/labels", nil, nil)
	r := b.vai("POST", "/labels/conferma", url.Values{"_csrf_token": {b.token}, "da": {"1"}, "a": {"101"}}, nil)
	if r.codice != 422 || !strings.Contains(r.corpo, "hai chiesto 101.") {
		t.Errorf("conferma forgiata oltre 100: %d", r.codice)
	}
	if impostazioneWeb(t, d, "etichette_qr_ultimo") != "300" || impostazioneWeb(t, d, "etichette_qr_w") != "" ||
		righeAudit(t, d, "label_sheet.generated")+righeAudit(t, d, "label_sheet.confirmed") != 0 {
		t.Error("un rifiuto ha salvato qualcosa")
	}
	// Una conferma senza token: 403, niente salvato.
	if r := b.vai("POST", "/labels/conferma", url.Values{"da": {"1"}, "a": {"2"}}, nil); r.codice != 403 {
		t.Errorf("senza CSRF: %d", r.codice)
	}
}

// inventario: RIP-S-48
// D-08: «Genera PDF» porta alla pagina con il PDF da scaricare e la domanda; «No» non salva; «Sì,
// stampate» salva max(ultimo, a) e torna al modulo con i numeri dopo. Senza JavaScript: link e moduli.
func TestConfermaEtichetteQR(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	r := b.vai("GET", "/labels/genera?da=1&a=50&anno=2026&w=57&h=32", nil, nil)
	if r.codice != 200 {
		t.Fatalf("%d", r.codice)
	}
	contiene(t, "pronto", r.corpo, "Etichette da <strong>0001/26</strong> a <strong>0050/26</strong> (50), formato 57×32",
		`href="/labels.pdf?a=50&amp;anno=2026&amp;da=1&amp;h=32&amp;w=57"`, "Etichette stampate correttamente?",
		`<form id="label-sheet-confirm" action="/labels/conferma" method="post">`, `<input name="a" type="hidden" value="50">`,
		">Sì, stampate</button>", `href="/labels/genera?a=50&amp;anno=2026&amp;da=1&amp;h=32&amp;w=57&amp;stampate=no"`)
	nonContiene(t, "pronto", r.corpo, "Contatore non aggiornato")
	if impostazioneWeb(t, d, "etichette_qr_ultimo") != "" || righeAudit(t, d, "label_sheet.generated") != 0 {
		t.Error("la pagina del PDF pronto ha salvato qualcosa")
	}
	if r := b.vai("GET", "/labels.pdf?a=50&anno=2026&da=1&h=32&w=57", nil, nil); r.codice != 200 {
		t.Fatalf("PDF %d", r.codice)
	}
	r = b.vai("GET", "/labels/genera?a=50&anno=2026&da=1&h=32&w=57&stampate=no", nil, nil)
	contiene(t, "no", r.corpo, "Contatore non aggiornato: puoi scaricare di nuovo il PDF e ristampare.", "Sì, stampate")
	if impostazioneWeb(t, d, "etichette_qr_ultimo") != "" {
		t.Error("«No» ha salvato il contatore")
	}
	r = b.vai("POST", "/labels/conferma", url.Values{"_csrf_token": {b.token}, "da": {"1"}, "a": {"50"}, "anno": {"2026"}, "w": {"57"}, "h": {"32"}}, nil)
	if !redirectA(r, "/labels") {
		t.Fatalf("conferma: %d %q", r.codice, r.location)
	}
	if impostazioneWeb(t, d, "etichette_qr_ultimo") != "50" || impostazioneWeb(t, d, "etichette_qr_anno") != "2026" || righeAudit(t, d, "label_sheet.confirmed") != 1 {
		t.Error("conferma non salvata")
	}
	r = b.vai("GET", "/labels", nil, nil)
	contiene(t, "dopo la conferma", r.corpo, "Contatore aggiornato: ultimo numero stampato 50.", `name="da" value="51"`, `name="a" value="100"`)
	// Una ristampa confermata non riporta indietro il contatore.
	b.vai("POST", "/labels/conferma", url.Values{"_csrf_token": {b.token}, "da": {"12"}, "a": {"12"}}, nil)
	if v := impostazioneWeb(t, d, "etichette_qr_ultimo"); v != "50" {
		t.Errorf("ristampa: %q", v)
	}
	// La riga che non si scrive: niente salvato e il messaggio.
	eseguiSQL(t, d, `CREATE TRIGGER no_conferma BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'label_sheet.confirmed' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	b.vai("POST", "/labels/conferma", url.Values{"_csrf_token": {b.token}, "da": {"51"}, "a": {"60"}}, nil)
	if v := impostazioneWeb(t, d, "etichette_qr_ultimo"); v != "50" {
		t.Errorf("senza riga: %q", v)
	}
	contiene(t, "senza riga", b.vai("GET", "/labels", nil, nil).corpo, "Contatore non aggiornato: riprova.")
}

// inventario: RIP-S-47
// D-22: solo il titolare azzera, e solo con la conferma esplicita (anche senza JavaScript): il POST
// senza conferma=1 mostra la domanda con «Sì, azzera» e «Annulla». Il dipendente non vede il pulsante e
// il suo POST torna a /labels con il messaggio, senza toccare niente. D-66: la proposta del nuovo anno
// (titolare: azzeramento; dipendente: avviso), che sparisce dopo l'azzeramento; mentre c'è, il
// pulsante generale non c'è.
func TestAzzeraEtichetteQR(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	dip := dipendenteDentro(t, h)
	dip.vai("GET", "/labels", nil, nil)
	dip.vai("POST", "/labels/conferma", url.Values{"_csrf_token": {dip.token}, "da": {"1"}, "a": {"50"}, "anno": {"2025"}}, nil)
	dip.vai("GET", "/labels", nil, nil)
	for _, conferma := range []string{"", "1"} {
		r := dip.vai("POST", "/labels/azzera", url.Values{"_csrf_token": {dip.token}, "conferma": {conferma}}, nil)
		if !redirectA(r, "/labels") || impostazioneWeb(t, d, "etichette_qr_ultimo") != "50" || righeAudit(t, d, "label_sheet.counter_reset") != 0 {
			t.Fatalf("dipendente %q: %d %q", conferma, r.codice, r.location)
		}
		contiene(t, "rifiuto", dip.vai("GET", "/labels", nil, nil).corpo, msgSoloTitolareAzzera)
	}
	// L'ultimo foglio confermato è del 2025, oggi è il 2026.
	r := dip.vai("GET", "/labels", nil, nil)
	contiene(t, "dipendente nel nuovo anno", r.corpo, `id="label-sheet-new-year">Nuovo anno: chiedi al titolare di azzerare il contatore`, `name="anno" value="2026"`)
	nonContiene(t, "dipendente nel nuovo anno", r.corpo, "Azzera contatore")
	tit := titolareDentro(t, h)
	r = tit.vai("GET", "/labels", nil, nil)
	contiene(t, "titolare nel nuovo anno", r.corpo, "Nuovo anno: ripartire da 1?", `<input name="nuovo_anno" type="hidden" value="1">`,
		`<input name="anno" type="hidden" value="2026">`, `id="label-sheet-new-year-reset" type="submit">Azzera contatore</button>`)
	if strings.Count(r.corpo, "Azzera contatore") != 1 || strings.Contains(r.corpo, `id="label-sheet-reset"`) {
		t.Error("due pulsanti di azzeramento con la proposta del nuovo anno")
	}
	// Senza conferma=1: la domanda, niente azzerato.
	r = tit.vai("POST", "/labels/azzera", url.Values{"_csrf_token": {tit.token}, "nuovo_anno": {"1"}, "anno": {"2026"}}, nil)
	if r.codice != 200 || impostazioneWeb(t, d, "etichette_qr_ultimo") != "50" || righeAudit(t, d, "label_sheet.counter_reset") != 0 {
		t.Fatalf("domanda: %d", r.codice)
	}
	contiene(t, "domanda", r.corpo, `<p id="label-sheet-reset-question"><strong>Azzerare il contatore? Il prossimo foglio ripartirà da 1.</strong></p>`,
		`<input name="conferma" type="hidden" value="1">`, `<input name="nuovo_anno" type="hidden" value="1">`, `<input name="anno" type="hidden" value="2026">`,
		`>Sì, azzera</button>`, `<a id="label-sheet-reset-cancel" href="/labels">Annulla</a>`, `id="app-mobile-navigation"`)
	r = tit.vai("POST", "/labels/azzera", url.Values{"_csrf_token": {tit.token}, "nuovo_anno": {"1"}, "anno": {"2026"}, "conferma": {"1"}}, nil)
	if !redirectA(r, "/labels?anno=2026") || impostazioneWeb(t, d, "etichette_qr_ultimo") != "0" || impostazioneWeb(t, d, "etichette_qr_anno") != "2026" ||
		righeAudit(t, d, "label_sheet.counter_reset") != 1 {
		t.Fatalf("titolare: %d %q", r.codice, r.location)
	}
	r = tit.vai("GET", "/labels?anno=2026", nil, nil)
	contiene(t, "dopo l'azzeramento", r.corpo, "Contatore azzerato: il prossimo foglio parte da 1.", `name="da" value="1"`, `name="a" value="50"`)
	nonContiene(t, "dopo l'azzeramento", r.corpo, "Nuovo anno", "Ultimo numero stampato", "Azzera contatore")
	if r := tit.vai("POST", "/labels/azzera", url.Values{"conferma": {"1"}}, nil); r.codice != 403 {
		t.Errorf("senza CSRF: %d", r.codice)
	}
	// Il pulsante generale, senza la proposta: porta l'anno del modulo; senza anno vale l'anno di oggi.
	tit.vai("POST", "/labels/conferma", url.Values{"_csrf_token": {tit.token}, "da": {"1"}, "a": {"7"}}, nil)
	r = tit.vai("GET", "/labels", nil, nil)
	contiene(t, "pulsante generale", r.corpo, `<form id="label-sheet-reset" action="/labels/azzera" method="post">`, `<input name="anno" type="hidden" value="2026">`,
		`<button id="label-sheet-reset-button" type="submit">Azzera contatore</button>`)
	r = tit.vai("POST", "/labels/azzera", url.Values{"_csrf_token": {tit.token}, "conferma": {"1"}, "anno": {"20267"}}, nil)
	if !redirectA(r, "/labels") || impostazioneWeb(t, d, "etichette_qr_anno") != "2026" || impostazioneWeb(t, d, "etichette_qr_ultimo") != "0" {
		t.Errorf("anno fuori dal modulo: %d %q %q", r.codice, r.location, impostazioneWeb(t, d, "etichette_qr_anno"))
	}
}

// inventario: RIP-S-47
// D-66 sull'anno scelto: il foglio dell'anno dopo (preparato a fine anno) propone l'azzeramento nella
// pagina del PDF pronto, prima della domanda; dopo l'azzeramento si torna al modulo con quell'anno e
// da = 1. Il dipendente vede l'avviso. Un ?anno= fuori dal modulo si ignora.
func TestNuovoAnnoNelPDFPronto(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	tit := titolareDentro(t, h)
	tit.vai("GET", "/labels", nil, nil)
	tit.vai("POST", "/labels/conferma", url.Values{"_csrf_token": {tit.token}, "da": {"301"}, "a": {"350"}, "anno": {"2026"}}, nil)
	r := tit.vai("GET", "/labels/genera?da=351&a=400&anno=2026", nil, nil)
	nonContiene(t, "stesso anno", r.corpo, "Nuovo anno")
	r = tit.vai("GET", "/labels/genera?da=351&a=400&anno=2027", nil, nil)
	contiene(t, "anno dopo", r.corpo, "Nuovo anno: ripartire da 1?", `<input name="anno" type="hidden" value="2027">`)
	if strings.Index(r.corpo, "Nuovo anno: ripartire da 1?") > strings.Index(r.corpo, "Etichette stampate correttamente?") {
		t.Error("la proposta non viene prima della domanda")
	}
	dip := dipendenteDentro(t, h)
	r = dip.vai("GET", "/labels/genera?da=351&a=400&anno=2027", nil, nil)
	contiene(t, "dipendente", r.corpo, "Nuovo anno: chiedi al titolare di azzerare il contatore")
	nonContiene(t, "dipendente", r.corpo, "Azzera contatore")
	r = tit.vai("POST", "/labels/azzera", url.Values{"_csrf_token": {tit.token}, "nuovo_anno": {"1"}, "anno": {"2027"}, "conferma": {"1"}}, nil)
	if !redirectA(r, "/labels?anno=2027") || impostazioneWeb(t, d, "etichette_qr_anno") != "2027" {
		t.Fatalf("azzeramento: %d %q", r.codice, r.location)
	}
	r = tit.vai("GET", r.location, nil, nil)
	contiene(t, "ritorno", r.corpo, `name="da" value="1"`, `name="a" value="50"`, `name="anno" value="2027"`, "RIP-2027-NNNN")
	for _, q := range []string{"anno=20267", "anno=abc", "anno=2023"} {
		contiene(t, q, tit.vai("GET", "/labels?"+q, nil, nil).corpo, `name="anno" value="2026"`)
	}
}

// inventario: RIP-S-48
// La conferma accetta solo gli anni del modulo (2024…2099): un anno forgiato è un 400 e non tocca
// niente; /labels.pdf lo accetta come lo standalone.
func TestConfermaAnnoForgiato(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	b.vai("GET", "/labels", nil, nil)
	for _, a := range []string{"20267", "2023", "2100", "-1"} {
		r := b.vai("POST", "/labels/conferma", url.Values{"_csrf_token": {b.token}, "da": {"1"}, "a": {"2"}, "anno": {a}}, nil)
		if r.codice != 400 || r.corpo != "Richiesta non valida" {
			t.Errorf("anno %s: %d %q", a, r.codice, r.corpo)
		}
	}
	if impostazioneWeb(t, d, "etichette_qr_ultimo") != "" || impostazioneWeb(t, d, "etichette_qr_anno") != "" || righeAudit(t, d, "label_sheet.confirmed") != 0 {
		t.Error("un anno forgiato ha salvato qualcosa")
	}
	for _, a := range []string{"2024", "2099"} {
		if r := b.vai("POST", "/labels/conferma", url.Values{"_csrf_token": {b.token}, "da": {"1"}, "a": {"2"}, "anno": {a}}, nil); !redirectA(r, "/labels") {
			t.Errorf("anno %s: %d", a, r.codice)
		}
	}
	if r := b.vai("GET", "/labels.pdf?da=1&a=1&anno=20267", nil, nil); r.codice != 200 {
		t.Errorf("PDF con l'anno 20267: %d", r.codice)
	}
}

// scriviDopoFlush registra se le intestazioni erano già partite (Flush) quando arriva il corpo.
type scriviDopoFlush struct {
	*httptest.ResponseRecorder
	flushPrimaDelCorpo bool
}

func (s *scriviDopoFlush) Write(b []byte) (int, error) {
	s.flushPrimaDelCorpo = s.Flushed
	return s.ResponseRecorder.Write(b)
}

// inventario: RIP-S-48
// Il 400 «Richiesta non valida» manda le intestazioni prima del corpo (Flush), senza tipo: come lo
// standalone, che risponde in streaming; un proxy che riceve tutto insieme sceglierebbe da sé
// text/plain (misurato sul Caddy del banco).
func TestRifiutoEtichetteFlush(t *testing.T) {
	w := &scriviDopoFlush{ResponseRecorder: httptest.NewRecorder()}
	f := &facciaWeb{}
	if !f.rifiutoEtichette(w, httptest.NewRequest("GET", "/labels.pdf?w=15", nil), buste.ErrRichiestaEtichette) {
		t.Fatal("nessuna risposta")
	}
	if w.Code != 400 || w.Body.String() != "Richiesta non valida" || !w.flushPrimaDelCorpo {
		t.Errorf("%d %q, flush prima del corpo %v", w.Code, w.Body.String(), w.flushPrimaDelCorpo)
	}
	if ct, c := w.Header()["Content-Type"]; !c || len(ct) != 0 {
		t.Errorf("Content-Type %v %v", ct, c)
	}
}
