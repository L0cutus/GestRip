package web

import (
	"strconv"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/pdf/lettura"
)

// L'etichetta busta (RIP-S-43, GET /repairs/{id}/label.pdf) e la scheda PDF (RIP-S-45,
// GET /repairs/{id}/scheda.pdf), come BagLabelController, RepairSheetController e i loro test dello
// standalone a go-baseline.

func percorsoEtichetta(id string) string { return "/repairs/" + id + "/label.pdf" }
func percorsoSchedaPDF(id string) string { return "/repairs/" + id + "/scheda.pdf" }

// inventario: RIP-S-43
// Titolare e dipendente aprono l'etichetta: un PDF valido di 76×127 mm con il QR della busta, in
// linea («inline; filename="etichetta_busta_1_26.pdf"»), application/pdf e private, no-store; con
// ?download=1 (e, come la verità di Flask, anche ?download=0) è un allegato, con ?download= resta in
// linea. Ogni risposta scrive repair.label_generated e nessuna repair.viewed.
func TestEtichettaScaricata(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := strconv.FormatInt(bustaScheda(t, d, o, nil), 10)
	qr := "QR-STORICO"
	for _, b := range []*browser{titolareDentro(t, h), dipendenteDentro(t, h)} {
		for q, disp := range map[string]string{"": "inline", "?download=1": "attachment", "?download=0": "attachment",
			"?download=": "inline", "?download%5B%5D=1": "attachment", "?download=1&download=": "inline"} {
			r := b.vai("GET", percorsoEtichetta(id)+q, nil, nil)
			if r.codice != 200 || !unaVolta(r, "Content-Type", "application/pdf") || !unaVolta(r, "Cache-Control", "private, no-store") ||
				!unaVolta(r, "Content-Disposition", disp+`; filename="etichetta_busta_1_26.pdf"`) ||
				r.h.Get("Content-Length") != strconv.Itoa(len(r.corpo)) {
				t.Fatalf("%q: %d %v", q, r.codice, r.h)
			}
			l, err := lettura.Leggi([]byte(r.corpo))
			if err != nil {
				t.Fatal(err)
			}
			qq, err := lettura.CodiciQR(l.Pagine[0])
			if lettura.Formato(l) != "76x127" || err != nil || len(qq) != 1 || qq[0].Testo != qr ||
				!strings.Contains(lettura.TestoNormalizzato(l), "N. 1/26 All'artigiano: 31/10/2026") {
				t.Errorf("%s %v %+v %q", lettura.Formato(l), err, qq, lettura.TestoNormalizzato(l))
			}
		}
	}
	if n := contaCon(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.label_generated' AND esito = 'ok'
		AND id_risorsa = ? AND dati = '{"bag_number":"1/26"}'`, id); n != 12 {
		t.Errorf("righe: %d", n)
	}
	if n := contaCon(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`); n != 0 {
		t.Errorf("repair.viewed: %d", n)
	}
}

// inventario: RIP-S-45
// La scheda PDF è sempre in linea («inline; filename="busta_1_26.pdf"», anche con ?download=1), un PDF
// di 100×230 mm con la busta, per titolare e dipendente; ognuno scrive repair.sheet_generated.
func TestSchedaPDFScaricata(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := strconv.FormatInt(bustaScheda(t, d, o, nil), 10)
	for _, b := range []*browser{titolareDentro(t, h), dipendenteDentro(t, h)} {
		for _, q := range []string{"", "?download=1"} {
			r := b.vai("GET", percorsoSchedaPDF(id)+q, nil, nil)
			if r.codice != 200 || !unaVolta(r, "Content-Type", "application/pdf") || !unaVolta(r, "Cache-Control", "private, no-store") ||
				!unaVolta(r, "Content-Disposition", `inline; filename="busta_1_26.pdf"`) || r.h.Get("Content-Length") != strconv.Itoa(len(r.corpo)) {
				t.Fatalf("%q: %d %v", q, r.codice, r.h)
			}
			l, err := lettura.Leggi([]byte(r.corpo))
			if err != nil {
				t.Fatal(err)
			}
			if lettura.Formato(l) != "100x230" || !strings.Contains(lettura.TestoNormalizzato(l), "N° BUSTA N. 1/26 DATA RICEZIONE") {
				t.Errorf("%s %q", lettura.Formato(l), lettura.TestoNormalizzato(l))
			}
		}
	}
	for _, email := range []string{"titolare@esempio.it", "banco@esempio.it"} {
		if n := contaCon(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.sheet_generated' AND attore = ?
			AND dati = '{"bag_number":"1/26"}'`, email); n != 2 {
			t.Errorf("%s: %d righe", email, n)
		}
	}
	if n := contaCon(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`); n != 0 {
		t.Errorf("repair.viewed: %d", n)
	}
}

// inventario: RIP-S-43, RIP-S-45
// Senza accesso si va a /login; le risposte di get_repair!/1 come la ricevuta (404, 400; oltre i 64 bit
// 404, D-56); se la
// riga d'audit non si scrive, 503 «PDF non disponibile» senza tipo né allegato.
func TestDocumentiPDFRifiuti(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := strconv.FormatInt(bustaScheda(t, d, o, nil), 10)
	b := titolareDentro(t, h)
	for _, percorso := range []func(string) string{percorsoEtichetta, percorsoSchedaPDF} {
		if r := nuovoBrowser(t, h).vai("GET", percorso(id), nil, nil); !redirectA(r, "/login") {
			t.Errorf("%s: %d %q", percorso(id), r.codice, r.location)
		}
		for _, c := range []struct {
			id     string
			codice int
			corpo  string
		}{{"999", 404, "Not Found"}, {"0", 404, "Not Found"}, {"abc", 400, "Bad Request"}, {"1.0", 400, "Bad Request"},
			{"99999999999999999999", 404, "Not Found"}} {
			r := b.vai("GET", percorso(c.id), nil, nil)
			if r.codice != c.codice || r.corpo != c.corpo || r.h.Get("Content-Disposition") != "" {
				t.Errorf("%s: %d %q", percorso(c.id), r.codice, r.corpo)
			}
		}
	}
	if n := contaCon(t, d, `SELECT count(*) FROM eventi_audit WHERE azione IN ('repair.label_generated', 'repair.sheet_generated')`); n != 0 {
		t.Errorf("righe: %d", n)
	}
	eseguiSQL(t, d, `CREATE TRIGGER no_pdf BEFORE INSERT ON eventi_audit WHEN NEW.azione IN ('repair.label_generated', 'repair.sheet_generated')
		BEGIN SELECT RAISE(ABORT, 'no'); END`)
	for _, percorso := range []string{percorsoEtichetta(id), percorsoSchedaPDF(id)} {
		r := b.vai("GET", percorso, nil, nil)
		if r.codice != 503 || r.corpo != "PDF non disponibile" || len(r.h.Values("Content-Type")) != 0 ||
			r.h.Get("Content-Disposition") != "" || !unaVolta(r, "Cache-Control", "max-age=0, private, must-revalidate") {
			t.Errorf("%s: %d %q %v", percorso, r.codice, r.corpo, r.h)
		}
	}
}

// inventario: RIP-S-43, RIP-S-45
// Sulla scheda, nella barra dopo «Modifica», i link dello standalone nel suo ordine: «⧉ Anteprima PDF»
// (scheda PDF in una nuova scheda), «Ricevuta PDF», «🖨 Etichetta busta» (in una nuova scheda) e
// «📤 Scarica etichetta» (?download=1), link semplici con il contorno, senza data-share-pdf.
func TestPulsantiDeiPDFSullaScheda(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := strconv.FormatInt(bustaScheda(t, d, o, nil), 10)
	r := titolareDentro(t, h).vai("GET", percorsoScheda(mustInt(t, id)), nil, nil)
	contiene(t, "scheda", r.corpo, `class="pulsante">Modifica</a>`+"\n"+
		`<a id="repair-sheet-pdf" href="/repairs/`+id+`/scheda.pdf" target="_blank" class="pulsante-contorno">⧉ Anteprima PDF</a>`+"\n"+
		`<a href="/repairs/`+id+`/receipt.pdf" class="pulsante-contorno">Ricevuta PDF</a>`+"\n"+
		`<a id="repair-label-pdf" href="/repairs/`+id+`/label.pdf" target="_blank" class="pulsante-contorno">🖨 Etichetta busta</a>`+"\n"+
		`<a id="repair-label-download" href="/repairs/`+id+`/label.pdf?download=1" class="pulsante-contorno">📤 Scarica etichetta</a>`+"\n"+
		`<a id="repair-notify" href="/repairs/`+id+`/avviso?mostra=`,
		`" class="pulsante-avvisa" data-avviso>📲 Avvisa cliente</a>`+"\n"+
			`<a id="repair-customer-preview" href="/repairs/`+id+`/anteprima-cliente" class="pulsante-contorno">👁 Anteprima cliente</a></div>`)
	nonContiene(t, "scheda", r.corpo, "data-share-pdf")
}
