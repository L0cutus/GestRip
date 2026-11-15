package web

import (
	"context"
	"html/template"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
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

// Le stampe della consegna (consegne_stampe.go, slice 078, ART-12): GET /handovers/{id}/labels.pdf come
// HandoverLabelController dello standalone, i link «👁 Anteprima», «🖨 Ristampa», «📤 Condividi» e «🖨 Stampa
// e conferma» della pagina della sessione, e la stampa diretta con la conferma solo a lavoro accettato
// (D-09), sempre con la stampante finta di printing. Nomi e numeri inventati; l'orologio è fermo al
// 31/10/2026.

// inventario: ART-12
// Il PDF delle etichette, per tutto il personale: una pagina 76×127 per busta (cestino escluso) con «→» l'artigiano
// della consegna, in linea «etichette_consegna_<id>.pdf» (allegato con download non vuoto, anche 0), privato e
// senza cache, con handover.labels_generated {"bags": N}. Una consegna che non c'è, vuota, o un id non valido:
// 404 «Consegna vuota o non trovata» senza tipo e senza riga; una riga che non si scrive: 503 «PDF non
// disponibile». Il PDF non cambia niente.
func TestPDFEtichetteConsegna(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Alfa Orafi'), (2, 'Beta')`)
	c := consegnaProva(t, d, 1, "2026-10-31T07:05:00.000000Z", "")
	pdf := func(q string) risposta { return b.vai("GET", percorsoConsegna(c)+"/labels.pdf"+q, nil, nil) }
	for _, p := range []string{percorsoConsegna(c), "/handovers/999", "/handovers/abc", "/handovers/0", "/handovers/99999999999999999999"} {
		r := b.vai("GET", p+"/labels.pdf", nil, nil)
		// Le intestazioni partono prima del corpo (niente Content-Length), come send_resp dello standalone: un proxy
		// davanti che riceve tutto insieme sceglierebbe da sé text/plain (banco s078p, Caddy).
		if r.codice != 404 || r.corpo != "Consegna vuota o non trovata" || len(r.h.Values("Content-Type")) != 0 || r.h.Get("Content-Length") != "" ||
			r.h.Get("Content-Disposition") != "" || !unaVolta(r, "Cache-Control", "max-age=0, private, must-revalidate") {
			t.Errorf("%s: %d %q %v", p, r.codice, r.corpo, r.h)
		}
	}
	uno, _ := bustaConsegna(t, d, o, "RIP-2026-0045", "Anello")
	due, n2 := bustaConsegna(t, d, o, "", "Collana")
	tre, _ := bustaConsegna(t, d, o, "", "Spilla")
	eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ?, artigiano_id = 2 WHERE id IN (?, ?, ?)`, c, uno, due, tre)
	eseguiSQL(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, "2026-10-31T08:00:00.000000Z", tre)
	giri := giroBusta(t, d, uno) + giroBusta(t, d, due)
	r := pdf("")
	if r.codice != 200 || !unaVolta(r, "Content-Type", "application/pdf") || !unaVolta(r, "Cache-Control", "private, no-store") ||
		r.h.Get("Content-Disposition") != `inline; filename="etichette_consegna_`+itoa(c)+`.pdf"` {
		t.Fatalf("%d %v", r.codice, r.h)
	}
	l, err := lettura.Leggi([]byte(r.corpo))
	if err != nil || lettura.Formato(l) != "76x127,76x127" || len(l.Pagine) != 2 {
		t.Fatalf("PDF %v", err)
	}
	testo := lettura.TestoNormalizzato(l)
	if !strings.HasPrefix(testo, "→ ALFA ORAFI #0045/26 All'artigiano: 31/10/2026") || !strings.Contains(testo, "→ ALFA ORAFI N. "+n2+" All'artigiano: 31/10/2026") ||
		strings.Contains(testo, "BETA") || strings.Contains(testo, "Spilla") {
		t.Errorf("testo %q", testo)
	}
	for q, disp := range map[string]string{"?download=1": "attachment", "?download=0": "attachment", "?download=": "inline", "?download=1&download=": "inline"} {
		if r := pdf(q); r.codice != 200 || r.h.Get("Content-Disposition") != disp+`; filename="etichette_consegna_`+itoa(c)+`.pdf"` {
			t.Errorf("%s: %d %q", q, r.codice, r.h.Get("Content-Disposition"))
		}
	}
	if righeAudit(t, d, "handover.labels_generated") != 5 || ultimoAuditWeb(t, d, "handover.labels_generated") != itoa(c)+`|ok|{"bags":2}` {
		t.Fatalf("audit %d", righeAudit(t, d, "handover.labels_generated"))
	}
	if giroBusta(t, d, uno)+giroBusta(t, d, due) != giri || righeAudit(t, d, "handover.confirmed") != 0 {
		t.Fatal("il PDF ha cambiato la consegna")
	}
	// Anche il titolare; una consegna già confermata dà lo stesso PDF (Ristampa).
	eseguiSQL(t, d, `UPDATE buste SET consegnata_artigiano_il = ? WHERE id IN (?, ?)`, "2026-10-31T08:30:00.000000Z", uno, due)
	if r := titolareDentro(t, h).vai("GET", percorsoConsegna(c)+"/labels.pdf", nil, nil); r.codice != 200 {
		t.Fatalf("titolare %d", r.codice)
	}
	eseguiSQL(t, d, `CREATE TRIGGER no_pdf BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'handover.labels_generated' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if r := pdf(""); r.codice != 503 || r.corpo != "PDF non disponibile" || len(r.h.Values("Content-Type")) != 0 || r.h.Get("Content-Length") != "" ||
		r.h.Get("Content-Disposition") != "" || !unaVolta(r, "Cache-Control", "max-age=0, private, must-revalidate") {
		t.Errorf("503: %d %q %v", r.codice, r.corpo, r.h)
	}
}

// facciaConStampanteE è facciaConStampante con le buste passate da avvolgi (per i guasti del dominio).
func facciaConStampanteE(t *testing.T, p printing.Printer, avvolgi func(Buste) Buste) (http.Handler, *db.DB, *clock.Finto) {
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
	dd.Buste = avvolgi(buste.Nuove(d, o))
	dd.IndirizzoPubblico, dd.Configurazione = indirizzoPubblicoProva, configProva
	dd.Stampanti = p
	return mustInterna(t, dd), d, o
}

// pdfRotto sono le buste con il PDF da stampare che non si fa.
type pdfRotto struct{ Buste }

func (pdfRotto) EtichetteDaStampare(context.Context, int64, func([]buste.DatiEtichetta) ([]byte, error)) ([]byte, []int64, error) {
	return nil, nil, buste.ErrPDFNonFatto
}

// moduloStampa sono i valori del modulo di «🖨 Stampa e conferma» della pagina della sessione: token, modo e invio.
func moduloStampa(t *testing.T, b *browser, p string) url.Values {
	t.Helper()
	r := b.vai("GET", p, nil, nil)
	m := reModoStampa.FindStringSubmatch(r.corpo)
	if m == nil {
		t.Fatal("la pagina non ha il modulo di «🖨 Stampa e conferma» con il modo")
	}
	v := url.Values{"_csrf_token": {b.token}, "modo": {m[1]}}
	if i := reInvioStampa.FindStringSubmatch(r.corpo); i != nil {
		v.Set("invio", i[1])
	}
	return v
}

var reModoStampa = regexp.MustCompile(`<input name="modo" type="hidden" value="(stampante|pdf)">`)

// inventario: ART-12
// I pulsanti della sessione (consegna_artigiano.html:57-84): prima della conferma «👁 Anteprima» (scheda nuova)
// e «📤 Condividi» (data-share-pdf, il download senza JavaScript), poi, nello stesso gruppo d'invio
// (data-invio-gruppo: il primo invio spegne tutti e due i moduli), «✓ Conferma senza stampare» e «🖨 Stampa e
// conferma» con la domanda e la frase sotto; il modulo porta il modo con cui la pagina è stata disegnata. Senza
// buste «🖨 Stampa e conferma» è un pulsante semplice (né domanda né PDF: il rifiuto lo dà il server). Con le buste e
// senza stampante è il link al PDF in una scheda nuova, come lo standalone, che invia anche il modulo
// (data-invia-modulo); con la stampante dell'etichetta busta è un pulsante con l'invio monouso. Dopo la conferma
// «🖨 Ristampa» e «📤 Condividi», nient'altro.
func TestPulsantiStampeConsegna(t *testing.T) {
	f := fintaProva()
	h, d, o := facciaConStampante(t, f)
	b := dipendenteDentro(t, h)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Alfa Orafi')`)
	c := consegnaProva(t, d, 1, "2026-10-31T07:05:00.000000Z", "")
	p := percorsoConsegna(c)
	r := b.vai("GET", p, nil, nil)
	contiene(t, "vuota", r.corpo,
		`<div class="consegna-stampe">`+"\n"+`<a id="handover-labels-preview" href="`+p+`/labels.pdf" target="_blank" class="pulsante-contorno">👁 Anteprima</a>`+"\n"+
			`<a id="handover-labels-share" href="`+p+`/labels.pdf?download=1" data-share-pdf="`+p+`/labels.pdf" class="pulsante-contorno">📤 Condividi</a>`+"\n"+`</div>`,
		`<div class="consegna-conferme" data-invio-gruppo>`+"\n"+`<form id="handover-confirm-form" action="`+p+`/conferma" method="post" class="consegna-azioni" data-invio-unico>`,
		`<form id="handover-print-confirm-form" action="`+p+`/stampa-conferma" method="post" class="consegna-azioni" data-invio-unico>`+"\n"+
			`<input name="_csrf_token" type="hidden" value="`+b.token+`">`+"\n"+`<input name="modo" type="hidden" value="pdf">`+"\n"+
			`<button id="handover-print-confirm-empty" type="submit" class="pulsante">🖨 Stampa e conferma</button>`+"\n"+`</form>`+"\n"+`</div>`,
		`<p class="sotto">"Conferma senza stampare" se dai le buste originali col tuo logo. "Stampa e conferma" se usi buste anonime e attacchi le etichette QR.</p>`)
	nonContiene(t, "vuota", r.corpo, "handover-labels-reprint", `id="handover-print-confirm"`, `name="invio"`, "data-invia-modulo")
	if strings.Index(r.corpo, "handover-labels-preview") > strings.Index(r.corpo, "handover-confirm-form") ||
		strings.Index(r.corpo, "handover-confirm-form") > strings.Index(r.corpo, "handover-print-confirm-form") {
		t.Error("ordine dei pulsanti")
	}
	busta, _ := bustaConsegna(t, d, o, "", "Anello")
	eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, c, busta)
	r = b.vai("GET", p, nil, nil)
	contiene(t, "piena", r.corpo, `<input name="modo" type="hidden" value="pdf">`+"\n"+
		`<a id="handover-print-confirm" href="`+p+`/labels.pdf" target="_blank" class="pulsante" data-confirm="Stampare 1 etichetta/e e confermare la consegna?" data-invia-modulo>🖨 Stampa e conferma</a>`+"\n"+`</form>`)
	nonContiene(t, "piena", r.corpo, "handover-print-confirm-empty", `name="invio"`)
	// Con la stampante dell'etichetta busta: il lavoro va alla stampante, niente PDF nel browser.
	scegliStampanti(t, d, "", "", "Buste-76x127", "")
	r = b.vai("GET", p, nil, nil)
	contiene(t, "stampante", r.corpo, `<form id="handover-print-confirm-form" action="`+p+`/stampa-conferma" method="post" class="consegna-azioni" data-invio-unico>`+"\n"+
		`<input name="_csrf_token" type="hidden" value="`+b.token+`">`+"\n"+`<input name="modo" type="hidden" value="stampante">`+"\n"+
		`<input name="invio" type="hidden" value="`+invioStampaDi(t, r.corpo)+`">`+"\n"+
		`<button id="handover-print-confirm" type="submit" class="pulsante" data-confirm="Stampare 1 etichetta/e e confermare la consegna?">🖨 Stampa e conferma</button>`)
	nonContiene(t, "stampante", r.corpo, "data-invia-modulo")
	eseguiSQL(t, d, `UPDATE buste SET consegnata_artigiano_il = ? WHERE id = ?`, "2026-10-31T08:30:00.000000Z", busta)
	r = b.vai("GET", p, nil, nil)
	contiene(t, "confermata", r.corpo, `<div class="consegna-stampe">`+"\n"+`<a id="handover-labels-reprint" href="`+p+`/labels.pdf" target="_blank" class="pulsante-contorno">🖨 Ristampa</a>`+"\n"+
		`<a id="handover-labels-share" href="`+p+`/labels.pdf?download=1" data-share-pdf="`+p+`/labels.pdf" class="pulsante-contorno">📤 Condividi</a>`)
	nonContiene(t, "confermata", r.corpo, "handover-labels-preview", "handover-print-confirm", "data-invio-gruppo", `"Conferma senza stampare" se`)
}

// inventario: ART-12
// «🖨 Stampa e conferma» senza stampante, come lo standalone: la conferma di «✓ Conferma senza stampare» (il PDF lo
// apre il link), con lo stesso rifiuto senza buste; nessun lavoro.
func TestStampaEConfermaSenzaStampante(t *testing.T) {
	f := fintaProva()
	h, d, o := facciaConStampante(t, f)
	b := dipendenteDentro(t, h)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Alfa Orafi')`)
	c := consegnaProva(t, d, 1, "2026-10-31T07:05:00.000000Z", "")
	stampa := func() risposta {
		return b.vai("POST", percorsoConsegna(c)+"/stampa-conferma", moduloStampa(t, b, percorsoConsegna(c)), nil)
	}
	if r := stampa(); !redirectA(r, percorsoConsegna(c)) {
		t.Fatalf("vuota: %d %q", r.codice, r.location)
	}
	contiene(t, "vuota", b.vai("GET", percorsoConsegna(c), nil, nil).corpo, `<div role="alert" class="flash flash-errore">Aggiungi almeno una busta.</div>`)
	busta, _ := bustaConsegna(t, d, o, "", "Anello")
	eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, c, busta)
	if r := stampa(); !redirectA(r, "/?fuori_bottega=1") {
		t.Fatalf("conferma: %d %q", r.codice, r.location)
	}
	contiene(t, "home", b.vai("GET", "/?fuori_bottega=1", nil, nil).corpo, `<div role="status" class="flash flash-info">Consegna confermata: 1 busta fuori bottega.</div>`)
	if giroBusta(t, d, busta) != itoa(c)+"|si|-" || righeAudit(t, d, "handover.confirmed") != 1 || righeAudit(t, d, "handover.labels_printed") != 0 || len(f.Lavori()) != 0 {
		t.Fatal("conferma")
	}
	if r := b.vai("POST", "/handovers/999/stampa-conferma", url.Values{"_csrf_token": {b.token}, "modo": {"pdf"}}, nil); !redirectA(r, rottaConsegne) {
		t.Fatalf("sessione che non c'è: %d %q", r.codice, r.location)
	}
	if r := b.vai("POST", percorsoConsegna(c)+"/stampa-conferma", nil, nil); r.codice != 403 {
		t.Fatalf("senza token: %d", r.codice)
	}
}

// inventario: ART-12
// Il modo della pagina e quello del POST non coincidono (la stampante scelta o tolta dopo che la pagina è stata
// disegnata, o un modulo senza modo): né lavoro né conferma, la sessione con «La stampante delle etichette è
// cambiata: controlla e riprova.». Nei due versi.
func TestStampaEConfermaModoCambiato(t *testing.T) {
	f := fintaProva()
	h, d, o := facciaConStampante(t, f)
	b := dipendenteDentro(t, h)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Alfa Orafi')`)
	c := consegnaProva(t, d, 1, "2026-10-31T07:05:00.000000Z", "")
	p := percorsoConsegna(c)
	busta, _ := bustaConsegna(t, d, o, "", "Anello")
	eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, c, busta)
	cambiata := `<div role="alert" class="flash flash-errore">` + template.HTMLEscapeString("La stampante delle etichette è cambiata: controlla e riprova.") + `</div>`
	senza := moduloStampa(t, b, p) // disegnata senza stampante
	scegliStampanti(t, d, "", "", "Buste-76x127", "")
	if r := b.vai("POST", p+"/stampa-conferma", senza, nil); !redirectA(r, p) {
		t.Fatalf("pdf → stampante: %d %q", r.codice, r.location)
	}
	contiene(t, "pdf → stampante", b.vai("GET", p, nil, nil).corpo, cambiata)
	con := moduloStampa(t, b, p) // disegnata con la stampante
	scegliStampanti(t, d, "", "", "", "")
	if r := b.vai("POST", p+"/stampa-conferma", con, nil); !redirectA(r, p) {
		t.Fatalf("stampante → pdf: %d %q", r.codice, r.location)
	}
	contiene(t, "stampante → pdf", b.vai("GET", p, nil, nil).corpo, cambiata)
	if r := b.vai("POST", p+"/stampa-conferma", url.Values{"_csrf_token": {b.token}}, nil); !redirectA(r, p) {
		t.Fatalf("senza modo: %d %q", r.codice, r.location)
	}
	contiene(t, "senza modo", b.vai("GET", p, nil, nil).corpo, cambiata)
	if len(f.Lavori()) != 0 || giroBusta(t, d, busta) != itoa(c)+"|-|-" || righeAudit(t, d, "handover.confirmed") != 0 {
		t.Fatal("stampato o confermato con il modo cambiato")
	}
}

// inventario: ART-12
// «🖨 Stampa e conferma» con la stampante dell'etichetta busta (D-09): un lavoro 76×127 con le opzioni della
// coda e il nome del file, una pagina per busta; solo se il lavoro è accettato la consegna si conferma, con
// handover.labels_printed (con gli id stampati) e handover.confirmed, e si va a /?fuori_bottega=1. Un rifiuto
// torna alla sessione con il motivo e non conferma niente; senza buste, con la consegna chiusa o senza buste ancora
// da timbrare (già confermata) nessun lavoro parte. Lo stesso invio rimandato non manda un secondo lavoro.
func TestStampaEConfermaConStampante(t *testing.T) {
	f := fintaProva()
	h, d, o := facciaConStampante(t, f)
	scegliStampanti(t, d, "", "", "Buste-76x127", "Darkness=8")
	b := dipendenteDentro(t, h)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Alfa Orafi')`)
	c := consegnaProva(t, d, 1, "2026-10-31T07:05:00.000000Z", "")
	p := percorsoConsegna(c)
	stampa := func() risposta { return b.vai("POST", p+"/stampa-conferma", moduloStampa(t, b, p), nil) }
	if r := stampa(); !redirectA(r, p) {
		t.Fatalf("vuota: %d %q", r.codice, r.location)
	}
	contiene(t, "vuota", b.vai("GET", p, nil, nil).corpo, `<div role="alert" class="flash flash-errore">Aggiungi almeno una busta.</div>`)
	uno, _ := bustaConsegna(t, d, o, "", "Anello")
	due, _ := bustaConsegna(t, d, o, "", "Collana")
	eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id IN (?, ?)`, c, uno, due)
	// Rifiutato: niente conferma, niente righe.
	f.ErrStampa = printing.Rifiuto{Motivo: "lp: Unable to print."}
	if r := stampa(); !redirectA(r, p) {
		t.Fatalf("rifiuto: %d %q", r.codice, r.location)
	}
	contiene(t, "rifiuto", b.vai("GET", p, nil, nil).corpo,
		`<div role="alert" class="flash flash-errore">`+template.HTMLEscapeString("Etichette non stampate: la stampante ha rifiutato il lavoro (lp: Unable to print.). Consegna non confermata.")+`</div>`)
	if giroBusta(t, d, uno) != itoa(c)+"|-|-" || righeAudit(t, d, "handover.confirmed") != 0 || righeAudit(t, d, "handover.labels_printed") != 0 ||
		righeAudit(t, d, "handover.labels_generated") != 0 {
		t.Fatal("conferma dopo un rifiuto")
	}
	// Accettato.
	f.ErrStampa = nil
	m := moduloStampa(t, b, p)
	r := b.vai("POST", p+"/stampa-conferma", m, nil)
	if !redirectA(r, "/?fuori_bottega=1") {
		t.Fatalf("accettato: %d %q", r.codice, r.location)
	}
	contiene(t, "accettato", b.vai("GET", "/?fuori_bottega=1", nil, nil).corpo,
		`<div role="status" class="flash flash-info">Consegna confermata: 2 buste fuori bottega. Etichette inviate a Buste-76x127 (lavoro Buste-76x127-1).</div>`)
	ll := f.Lavori()
	if len(ll) != 1 || ll[0].Coda != "Buste-76x127" || ll[0].Opzioni.LarghezzaMM != 76 || ll[0].Opzioni.AltezzaMM != 127 ||
		printing.ScriviOpzioni(ll[0].Opzioni.Extra) != "Darkness=8" || ll[0].Opzioni.Titolo != "etichette_consegna_"+itoa(c)+".pdf" {
		t.Fatalf("%+v", ll)
	}
	if l, err := lettura.Leggi(ll[0].PDF); err != nil || lettura.Formato(l) != "76x127,76x127" || len(l.Pagine) != 2 ||
		!strings.HasPrefix(lettura.TestoNormalizzato(l), "→ ALFA ORAFI") {
		t.Errorf("PDF %v", err)
	}
	if giroBusta(t, d, uno) != itoa(c)+"|si|-" || giroBusta(t, d, due) != itoa(c)+"|si|-" {
		t.Fatal("buste non uscite")
	}
	ids := `[` + itoa(uno) + "," + itoa(due) + "]"
	if ultimoAuditWeb(t, d, "handover.labels_printed") != itoa(c)+`|ok|{"bags":2,"job":"Buste-76x127-1","queue":"Buste-76x127","repair_ids":`+ids+`}` ||
		ultimoAuditWeb(t, d, "handover.confirmed") != itoa(c)+`|ok|{"handed_over":2,"repair_ids":`+ids+`}` ||
		righeAudit(t, d, "handover.labels_generated") != 0 {
		t.Fatal("righe")
	}
	// Lo stesso modulo rimandato: l'esito del primo, nessun secondo lavoro.
	if r := b.vai("POST", p+"/stampa-conferma", m, nil); !redirectA(r, "/?fuori_bottega=1") || len(f.Lavori()) != 1 || righeAudit(t, d, "handover.confirmed") != 1 {
		t.Fatalf("doppio invio: %d %q, %d lavori", r.codice, r.location, len(f.Lavori()))
	}
	contiene(t, "doppio", b.vai("GET", "/?fuori_bottega=1", nil, nil).corpo, "Etichette inviate a Buste-76x127 (lavoro Buste-76x127-1).")
	// Già confermata (POST forgiato, scheda vecchia): nessuna busta da timbrare, nessun lavoro, nessuna riga.
	if r := b.vai("POST", p+"/stampa-conferma", url.Values{"_csrf_token": {b.token}, "modo": {"stampante"}}, nil); !redirectA(r, p) {
		t.Fatalf("già confermata: %d %q", r.codice, r.location)
	}
	contiene(t, "già confermata", b.vai("GET", p, nil, nil).corpo, `<div role="alert" class="flash flash-errore">`+
		template.HTMLEscapeString("Nessuna busta da confermare: le buste di questa consegna sono già fuori bottega. Per rifare le etichette usa «🖨 Ristampa».")+`</div>`)
	if len(f.Lavori()) != 1 || righeAudit(t, d, "handover.confirmed") != 1 || righeAudit(t, d, "handover.labels_printed") != 1 {
		t.Fatal("lavoro o righe per una consegna già confermata")
	}
	// Consegna chiusa: nessun lavoro.
	chiusa := consegnaProva(t, d, 1, "2026-10-31T07:05:00.000000Z", "2026-10-31T08:00:00.000000Z")
	tre, _ := bustaConsegna(t, d, o, "", "Spilla")
	eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, chiusa, tre)
	r = b.vai("POST", percorsoConsegna(chiusa)+"/stampa-conferma", url.Values{"_csrf_token": {b.token}, "modo": {"stampante"}}, nil)
	if !redirectA(r, percorsoConsegna(chiusa)) {
		t.Fatalf("chiusa: %d %q", r.codice, r.location)
	}
	contiene(t, "chiusa", b.vai("GET", percorsoConsegna(chiusa), nil, nil).corpo, `<div role="alert" class="flash flash-errore">Consegna non disponibile</div>`)
	if len(f.Lavori()) != 1 {
		t.Fatal("lavoro per una consegna chiusa")
	}
	if r := b.vai("POST", "/handovers/999/stampa-conferma", url.Values{"_csrf_token": {b.token}, "modo": {"stampante"}}, nil); !redirectA(r, rottaConsegne) || len(f.Lavori()) != 1 {
		t.Fatalf("sessione che non c'è: %d %q", r.codice, r.location)
	}
}

// inventario: ART-12
// Il lavoro accettato ma la conferma non scritta (riga che non si scrive): il messaggio lo dice, resta la traccia
// handover.labels_printed «not_confirmed» con coda, lavoro e id, e lo stesso invio non ristampa.
func TestStampaEConfermaAccettataNonConfermata(t *testing.T) {
	f := fintaProva()
	h, d, o := facciaConStampante(t, f)
	scegliStampanti(t, d, "", "", "Buste-76x127", "")
	b := titolareDentro(t, h)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Alfa Orafi')`)
	c := consegnaProva(t, d, 1, "2026-10-31T07:05:00.000000Z", "")
	busta, _ := bustaConsegna(t, d, o, "", "Anello")
	eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, c, busta)
	eseguiSQL(t, d, `CREATE TRIGGER no_conferma BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'handover.confirmed' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	m := moduloStampa(t, b, percorsoConsegna(c))
	for i := range 2 {
		r := b.vai("POST", percorsoConsegna(c)+"/stampa-conferma", m, nil)
		if !redirectA(r, percorsoConsegna(c)) {
			t.Fatalf("invio %d: %d %q", i+1, r.codice, r.location)
		}
		contiene(t, "non confermata", b.vai("GET", percorsoConsegna(c), nil, nil).corpo,
			`<div role="alert" class="flash flash-errore">`+template.HTMLEscapeString("Etichette inviate a Buste-76x127 (lavoro Buste-76x127-1), ma la consegna non è stata confermata: usa «✓ Conferma senza stampare».")+`</div>`)
	}
	if len(f.Lavori()) != 1 || giroBusta(t, d, busta) != itoa(c)+"|-|-" {
		t.Fatalf("%d lavori, giro %s", len(f.Lavori()), giroBusta(t, d, busta))
	}
	if a := ultimoAuditWeb(t, d, "handover.labels_printed"); a != itoa(c)+`|not_confirmed|{"bags":1,"job":"Buste-76x127-1","queue":"Buste-76x127","repair_ids":[`+itoa(busta)+`]}` {
		t.Errorf("traccia %s", a)
	}
}

// inventario: ART-12
// Mentre lp lavora la consegna cambia: chiusa (il rifiuto del dominio dopo il lavoro: «… ma la consegna non è stata
// confermata: Consegna non disponibile.») o confermata da un'altra richiesta (nessuna seconda handover.confirmed:
// «… la consegna era già confermata.»); in tutti e due i casi la traccia not_confirmed.
func TestStampaEConfermaConsegnaCambiataDuranteIlLavoro(t *testing.T) {
	for _, x := range []struct {
		nome, sql, msg string
		confermate     int
	}{
		{"chiusa", `UPDATE consegne_artigiani SET chiusa_il = '2026-10-31T08:00:00.000000Z'`,
			"Etichette inviate a Buste-76x127 (lavoro Buste-76x127-1), ma la consegna non è stata confermata: Consegna non disponibile.", 0},
		{"confermata da un altro", `UPDATE buste SET consegnata_artigiano_il = '2026-10-31T08:00:00.000000Z' WHERE consegna_artigiano_id IS NOT NULL`,
			"Etichette inviate a Buste-76x127 (lavoro Buste-76x127-1), ma la consegna era già confermata: nessuna busta da timbrare.", 0},
	} {
		t.Run(x.nome, func(t *testing.T) {
			f := fintaProva()
			h, d, o := facciaConStampante(t, f)
			scegliStampanti(t, d, "", "", "Buste-76x127", "")
			b := dipendenteDentro(t, h)
			eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Alfa Orafi')`)
			c := consegnaProva(t, d, 1, "2026-10-31T07:05:00.000000Z", "")
			busta, _ := bustaConsegna(t, d, o, "", "Anello")
			eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, c, busta)
			m := moduloStampa(t, b, percorsoConsegna(c))
			f.Prima = func() { eseguiSQL(t, d, x.sql) }
			if r := b.vai("POST", percorsoConsegna(c)+"/stampa-conferma", m, nil); !redirectA(r, percorsoConsegna(c)) {
				t.Fatalf("%d %q", r.codice, r.location)
			}
			contiene(t, x.nome, b.vai("GET", percorsoConsegna(c), nil, nil).corpo, `<div role="alert" class="flash flash-errore">`+template.HTMLEscapeString(x.msg)+`</div>`)
			if righeAudit(t, d, "handover.confirmed") != x.confermate || len(f.Lavori()) != 1 ||
				ultimoAuditWeb(t, d, "handover.labels_printed") != itoa(c)+`|not_confirmed|{"bags":1,"job":"Buste-76x127-1","queue":"Buste-76x127","repair_ids":[`+itoa(busta)+`]}` {
				t.Errorf("righe %d, lavori %d", righeAudit(t, d, "handover.confirmed"), len(f.Lavori()))
			}
		})
	}
}

// inventario: ART-12
// La stampante non risponde in tempo: niente conferma, il motivo (il lavoro potrebbe essere partito), e l'esito
// resta per l'invio: lo stesso modulo rimandato non chiede un secondo lavoro. Un PDF che non si fa: «Etichette non
// stampate: PDF non disponibile. Consegna non confermata.», nessun lavoro.
func TestStampaEConfermaTempoScadutoEPDF(t *testing.T) {
	f := fintaProva()
	h, d, o := facciaConStampante(t, f)
	scegliStampanti(t, d, "", "", "Buste-76x127", "")
	b := dipendenteDentro(t, h)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Alfa Orafi')`)
	c := consegnaProva(t, d, 1, "2026-10-31T07:05:00.000000Z", "")
	p := percorsoConsegna(c)
	busta, _ := bustaConsegna(t, d, o, "", "Anello")
	eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, c, busta)
	chiamate := 0
	f.Prima = func() { chiamate++ }
	f.ErrStampa = printing.ErrTempoScaduto
	m := moduloStampa(t, b, p)
	for i := range 2 {
		if r := b.vai("POST", p+"/stampa-conferma", m, nil); !redirectA(r, p) {
			t.Fatalf("invio %d: %d %q", i+1, r.codice, r.location)
		}
		contiene(t, "tempo scaduto", b.vai("GET", p, nil, nil).corpo, template.HTMLEscapeString("Etichette non stampate: la stampante non ha risposto in tempo."), "Consegna non confermata.")
	}
	if chiamate != 1 || giroBusta(t, d, busta) != itoa(c)+"|-|-" || righeAudit(t, d, "handover.confirmed") != 0 {
		t.Fatalf("%d lavori chiesti, giro %s", chiamate, giroBusta(t, d, busta))
	}
	// Il PDF che non si fa.
	h2, d2, o2 := facciaConStampanteE(t, f, func(b Buste) Buste { return pdfRotto{b} })
	scegliStampanti(t, d2, "", "", "Buste-76x127", "")
	b2 := dipendenteDentro(t, h2)
	eseguiSQL(t, d2, `INSERT INTO artigiani (id, nome) VALUES (1, 'Alfa Orafi')`)
	c2 := consegnaProva(t, d2, 1, "2026-10-31T07:05:00.000000Z", "")
	altra, _ := bustaConsegna(t, d2, o2, "", "Anello")
	eseguiSQL(t, d2, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, c2, altra)
	if r := b2.vai("POST", percorsoConsegna(c2)+"/stampa-conferma", moduloStampa(t, b2, percorsoConsegna(c2)), nil); !redirectA(r, percorsoConsegna(c2)) {
		t.Fatalf("PDF: %d %q", r.codice, r.location)
	}
	contiene(t, "PDF", b2.vai("GET", percorsoConsegna(c2), nil, nil).corpo, `<div role="alert" class="flash flash-errore">Etichette non stampate: PDF non disponibile. Consegna non confermata.</div>`)
	if chiamate != 1 || righeAudit(t, d2, "handover.confirmed") != 0 {
		t.Fatalf("lavoro senza PDF: %d", chiamate)
	}
}

// inventario: ART-12
// Un lavoro della consegna in corso tiene fuori un secondo, anche da un altro modulo (un altro invio: due schede)
// e da «✓ Conferma senza stampare»: «Stampa già in corso: aspetta l'esito prima di riprovare.». Un lavoro solo.
func TestStampaEConfermaBloccoPerConsegna(t *testing.T) {
	f := fintaProva()
	h, d, o := facciaConStampante(t, f)
	scegliStampanti(t, d, "", "", "Buste-76x127", "")
	b := dipendenteDentro(t, h)
	eseguiSQL(t, d, `INSERT INTO artigiani (id, nome) VALUES (1, 'Alfa Orafi')`)
	c := consegnaProva(t, d, 1, "2026-10-31T07:05:00.000000Z", "")
	p := percorsoConsegna(c)
	busta, _ := bustaConsegna(t, d, o, "", "Anello")
	eseguiSQL(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, c, busta)
	primoModulo, altroModulo := moduloStampa(t, b, p), moduloStampa(t, b, p)
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
	go func() { primo <- b.vai("POST", p+"/stampa-conferma", primoModulo, nil) }()
	ricevi(t, entrato, "il primo lavoro")
	inCorso := `<div role="alert" class="flash flash-errore">` + template.HTMLEscapeString(msgStampaInCorso) + `</div>`
	if r := b.vai("POST", p+"/stampa-conferma", altroModulo, nil); !redirectA(r, p) {
		t.Fatalf("altro modulo: %d %q", r.codice, r.location)
	}
	contiene(t, "altro modulo", b.vai("GET", p, nil, nil).corpo, inCorso)
	if r := b.vai("POST", p+"/conferma", url.Values{"_csrf_token": {b.token}}, nil); !redirectA(r, p) {
		t.Fatalf("conferma senza stampare: %d %q", r.codice, r.location)
	}
	contiene(t, "conferma senza stampare", b.vai("GET", p, nil, nil).corpo, inCorso)
	close(via)
	if r := ricevi(t, primo, "il primo invio"); !redirectA(r, "/?fuori_bottega=1") || len(f.Lavori()) != 1 || righeAudit(t, d, "handover.confirmed") != 1 {
		t.Fatalf("primo: %d %q, %d lavori", r.codice, r.location, len(f.Lavori()))
	}
}

// inventario: ART-12
// Lo script: il primo invio di un modulo dentro data-invio-gruppo spegne tutti i moduli del gruppo (le due
// conferme della consegna), e la pagina tenuta dal browser li riaccende; senza stampante il link di «🖨 Stampa e
// conferma» apre il PDF (target, come lo standalone: niente window.open che il blocco dei popup fermerebbe) e invia
// il suo modulo, non dopo una domanda annullata o con il gruppo già inviato.
func TestScriptStampaEConfermaDellaConsegna(t *testing.T) {
	b, _ := dentroCome(t, accounts.Dipendente, nil)
	js := assetDellaPagina(t, b, b.vai("GET", "/", nil, nil).corpo, "js", "text/javascript; charset=utf-8")
	for _, s := range []string{
		`const gruppo = form.closest("[data-invio-gruppo]")`,
		`if (form.dataset.inviato || (gruppo && gruppo.dataset.inviato)) { event.preventDefault(); return }`,
		`(gruppo || form).querySelectorAll("button[type=submit]").forEach(button => { button.disabled = true })`,
		`document.querySelectorAll("[data-invio-gruppo]").forEach(gruppo => { delete gruppo.dataset.inviato })`,
		`const link = event.target.closest && event.target.closest("a[data-invia-modulo]")` + "\n" + `  if (!link || event.defaultPrevented) return`,
		`if (gruppo && gruppo.dataset.inviato) { event.preventDefault(); return }` + "\n" + `  if (form) form.requestSubmit()`,
	} {
		if !strings.Contains(js, s) {
			t.Errorf("app.js non contiene %q", s)
		}
	}
	if strings.Contains(js, "data-apri-pdf") {
		t.Error("app.js apre ancora il PDF con window.open")
	}
}
