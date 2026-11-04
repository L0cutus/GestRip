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
// standalone (label_sheet_controller_test.exs, slice 210), con D-21/D-64, D-22 e D-66 (Stefano,
// 04/11/2026). Dalla 046 il PDF salva il contatore come lo standalone (D-08 cambiata, Stefano
// 04/11/2026). L'orologio dei test è al 31/10/2026.

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
		"Massimo 100 etichette per volta.", `id="label-sheet-form" method="get" action="/labels.pdf"`,
		`name="da" value="1" min="1" max="9999" required`, `name="a" value="50" min="1" max="9999" required`,
		`name="anno" value="2026" min="2024" max="2099" required`, `name="w" step="0.5" value="57" min="16" max="200" required`,
		`name="h" step="0.5" value="32" min="16" max="200" required`, `<button id="label-sheet-submit">⬇ Scarica PDF</button></div>`, "overlapping", `href="/labels"`)
	nonContiene(t, "modulo", r.corpo, "Ultimo numero stampato", "Formato piccolo", "200 etichette", "Azzera contatore", "Nuovo anno",
		"label-sheet-print-button", `<input name="invio"`)
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
// «0045/26» (D-21, D-64); salva max(ultimo, a), formato, anno e label_sheet.generated, come lo
// standalone dopo la 210.
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
	if v := impostazioneWeb(t, d, "etichette_qr_ultimo"); v != "46" || impostazioneWeb(t, d, "etichette_qr_anno") != "2026" {
		t.Errorf("contatore %q", v)
	}
	if impostazioneWeb(t, d, "etichette_qr_w") != "40" || impostazioneWeb(t, d, "etichette_qr_h") != "20" || righeAudit(t, d, "label_sheet.generated") != 1 {
		t.Error("formato o riga d'audit non salvati")
	}
	// Senza parametri: 1-50 dell'anno di oggi, 57×32 anche con un altro formato salvato.
	r = b.vai("GET", "/labels.pdf", nil, nil)
	if r.h.Get("Content-Disposition") != `attachment; filename="etichette-qr-2026-0001-0050.pdf"` || impostazioneWeb(t, d, "etichette_qr_w") != "57" {
		t.Errorf("default: %v", r.h)
	}
	contiene(t, "modulo dopo il PDF", b.vai("GET", "/labels", nil, nil).corpo, "📌 Ultimo numero stampato: <strong>50</strong>", `name="da" value="51"`)
	// Un anno fuori dal modulo si accetta come lo standalone.
	if r := b.vai("GET", "/labels.pdf?da=1&a=1&anno=20267", nil, nil); r.codice != 200 || r.h.Get("Content-Type") != "application/pdf" {
		t.Errorf("PDF con l'anno 20267: %d", r.codice)
	}
	if v := impostazioneWeb(t, d, "etichette_qr_anno"); v != "2026" {
		t.Errorf("anno salvato dopo il 20267: %q", v)
	}
	// Numeri oltre 9999: quattro cifre almeno, mai tagliati.
	r = b.vai("GET", "/labels.pdf?da=9999&a=10000&anno=2026", nil, nil)
	l, _ = lettura.Leggi([]byte(r.corpo))
	if r.h.Get("Content-Disposition") != `attachment; filename="etichette-qr-2026-9999-10000.pdf"` || lettura.TestoNormalizzato(l) != "9999/26 10000/26" {
		t.Errorf("oltre 9999: %v %q", r.h, lettura.TestoNormalizzato(l))
	}
	if qq, _ := lettura.CodiciQR(l.Pagine[1]); len(qq) != 1 || qq[0].Testo != "RIP-2026-10000" {
		t.Errorf("QR oltre 9999: %+v", qq)
	}
}

// inventario: RIP-S-48
// Rifiuti: 400 «Richiesta non valida» senza tipo; oltre 100 il modulo con la richiesta e il messaggio
// (422); niente PDF né salvataggi, sul PDF e sulla stampa diretta (anche forgiata).
func TestEtichetteQRRifiuti(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('etichette_qr_ultimo', '300', 'x')`)
	for _, q := range []string{"da=5&a=2", "w=15", "h=201", "da=abc", "anno=", "w=nan", "da[]=1", "da[a]=1", "w=1e400"} {
		r := b.vai("GET", "/labels.pdf?"+q, nil, nil)
		if r.codice != 400 || r.corpo != "Richiesta non valida" || r.h.Get("Content-Type") != "" || r.h.Get("Cache-Control") != cacheBase {
			t.Errorf("%s: %d %q %v", q, r.codice, r.corpo, r.h)
		}
		b.vai("GET", "/labels", nil, nil)
		v, _ := url.ParseQuery(q)
		v.Set("_csrf_token", b.token)
		if r := b.vai("POST", "/labels/stampa", v, nil); r.codice != 400 || r.corpo != "Richiesta non valida" {
			t.Errorf("stampa %s: %d %q", q, r.codice, r.corpo)
		}
	}
	r := b.vai("GET", "/labels.pdf?da=301&a=401&anno=2027&w=40&h=20", nil, nil)
	if r.codice != 422 || r.h.Get("Content-Disposition") != "" {
		t.Errorf("101: %d", r.codice)
	}
	contiene(t, "101", r.corpo, `id="label-sheet-too-many"`, "Massimo 100 etichette per volta: hai chiesto 101.",
		`name="da" value="301"`, `name="a" value="401"`, `name="anno" value="2027"`, `name="w" step="0.5" value="40"`,
		"📌 Ultimo numero stampato: <strong>300</strong>")
	b.vai("GET", "/labels", nil, nil)
	r = b.vai("POST", "/labels/stampa", url.Values{"_csrf_token": {b.token}, "da": {"1"}, "a": {"101"}}, nil)
	if r.codice != 422 || !strings.Contains(r.corpo, "hai chiesto 101.") {
		t.Errorf("stampa forgiata oltre 100: %d", r.codice)
	}
	if impostazioneWeb(t, d, "etichette_qr_ultimo") != "300" || impostazioneWeb(t, d, "etichette_qr_w") != "" ||
		righeAudit(t, d, "label_sheet.generated")+righeAudit(t, d, "label_sheet.printed") != 0 {
		t.Error("un rifiuto ha salvato qualcosa")
	}
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
	dip.vai("GET", "/labels.pdf?da=1&a=50&anno=2025", nil, nil)
	dip.vai("GET", "/labels", nil, nil)
	for _, conferma := range []string{"", "1"} {
		r := dip.vai("POST", "/labels/azzera", url.Values{"_csrf_token": {dip.token}, "conferma": {conferma}}, nil)
		if !redirectA(r, "/labels") || impostazioneWeb(t, d, "etichette_qr_ultimo") != "50" || righeAudit(t, d, "label_sheet.counter_reset") != 0 {
			t.Fatalf("dipendente %q: %d %q", conferma, r.codice, r.location)
		}
		contiene(t, "rifiuto", dip.vai("GET", "/labels", nil, nil).corpo, msgSoloTitolareAzzera)
	}
	// L'ultimo foglio generato è del 2025, oggi è il 2026.
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
	tit.vai("GET", "/labels.pdf?da=1&a=7", nil, nil)
	r = tit.vai("GET", "/labels", nil, nil)
	contiene(t, "pulsante generale", r.corpo, `<form id="label-sheet-reset" action="/labels/azzera" method="post">`, `<input name="anno" type="hidden" value="2026">`,
		`<button id="label-sheet-reset-button" type="submit">Azzera contatore</button>`)
	r = tit.vai("POST", "/labels/azzera", url.Values{"_csrf_token": {tit.token}, "conferma": {"1"}, "anno": {"20267"}}, nil)
	if !redirectA(r, "/labels") || impostazioneWeb(t, d, "etichette_qr_anno") != "2026" || impostazioneWeb(t, d, "etichette_qr_ultimo") != "0" {
		t.Errorf("anno fuori dal modulo: %d %q %q", r.codice, r.location, impostazioneWeb(t, d, "etichette_qr_anno"))
	}
}

// inventario: RIP-S-47, RIP-S-48
// D-66 sull'anno scelto: il foglio di un anno più nuovo dell'anno salvato, con il contatore non a zero,
// passa prima dalla pagina «Nuovo anno: ripartire da 1?» (niente PDF né salvataggi). Il titolare ha
// l'azzeramento e «Continua senza azzerare»; il dipendente l'avviso e «Continua». Continuare scarica
// il PDF e salva; dopo l'azzeramento si torna al modulo con quell'anno e da = 1. Un ?anno= fuori dal
// modulo si ignora.
func TestNuovoAnnoPrimaDelPDF(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	tit := titolareDentro(t, h)
	if r := tit.vai("GET", "/labels.pdf?da=301&a=350&anno=2026", nil, nil); r.h.Get("Content-Type") != "application/pdf" {
		t.Fatalf("primo foglio: %d", r.codice)
	}
	if r := tit.vai("GET", "/labels.pdf?da=351&a=360&anno=2026", nil, nil); r.h.Get("Content-Type") != "application/pdf" {
		t.Errorf("stesso anno: %d %q", r.codice, r.h.Get("Content-Type"))
	}
	r := tit.vai("GET", "/labels.pdf?da=361&a=400&anno=2027", nil, nil)
	if r.codice != 200 || !strings.HasPrefix(r.h.Get("Content-Type"), "text/html") || r.h.Get("Content-Disposition") != "" {
		t.Fatalf("anno dopo: %d %v", r.codice, r.h)
	}
	contiene(t, "anno dopo", r.corpo, "Nuovo anno: ripartire da 1?", `<input name="anno" type="hidden" value="2027">`,
		`id="label-sheet-new-year-reset" type="submit">Azzera contatore</button>`,
		`<a id="label-sheet-continue" href="/labels.pdf?a=400&amp;anno=2027&amp;da=361&amp;h=32&amp;w=57&amp;continua=1">Continua senza azzerare: scarica il PDF</a>`,
		"Etichette da <strong>0361/27</strong> a <strong>0400/27</strong>; ultimo numero stampato: <strong>360</strong> del 2026.")
	if impostazioneWeb(t, d, "etichette_qr_ultimo") != "360" || impostazioneWeb(t, d, "etichette_qr_anno") != "2026" || righeAudit(t, d, "label_sheet.generated") != 2 {
		t.Error("la pagina del nuovo anno ha salvato")
	}
	dip := dipendenteDentro(t, h)
	r = dip.vai("GET", "/labels.pdf?da=361&a=400&anno=2027", nil, nil)
	contiene(t, "dipendente", r.corpo, "Nuovo anno: chiedi al titolare di azzerare il contatore", ">Continua: scarica il PDF</a>")
	nonContiene(t, "dipendente", r.corpo, "Azzera contatore")
	// Continuare: il PDF del foglio chiesto, con il contatore e l'anno salvati.
	r = dip.vai("GET", "/labels.pdf?a=400&anno=2027&da=361&h=32&w=57&continua=1", nil, nil)
	if r.h.Get("Content-Disposition") != `attachment; filename="etichette-qr-2027-0361-0400.pdf"` ||
		impostazioneWeb(t, d, "etichette_qr_ultimo") != "400" || impostazioneWeb(t, d, "etichette_qr_anno") != "2027" {
		t.Errorf("continua: %d %v", r.codice, r.h)
	}
	if r := dip.vai("GET", "/labels.pdf?da=401&a=402&anno=2027", nil, nil); r.h.Get("Content-Type") != "application/pdf" {
		t.Errorf("dopo aver continuato: %d", r.codice)
	}
	// Azzerare: il titolare dalla pagina del 2028 torna al modulo del 2028 con da = 1.
	tit.vai("GET", "/labels.pdf?da=1&a=2&anno=2028", nil, nil)
	r = tit.vai("POST", "/labels/azzera", url.Values{"_csrf_token": {tit.token}, "nuovo_anno": {"1"}, "anno": {"2028"}, "conferma": {"1"}}, nil)
	if !redirectA(r, "/labels?anno=2028") || impostazioneWeb(t, d, "etichette_qr_anno") != "2028" || impostazioneWeb(t, d, "etichette_qr_ultimo") != "0" {
		t.Fatalf("azzeramento: %d %q", r.codice, r.location)
	}
	r = tit.vai("GET", r.location, nil, nil)
	contiene(t, "ritorno", r.corpo, `name="da" value="1"`, `name="a" value="50"`, `name="anno" value="2028"`, "RIP-2028-NNNN")
	for _, q := range []string{"anno=20267", "anno=abc", "anno=2023"} {
		contiene(t, q, tit.vai("GET", "/labels?"+q, nil, nil).corpo, `name="anno" value="2026"`)
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
