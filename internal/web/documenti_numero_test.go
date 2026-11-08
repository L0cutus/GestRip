package web

import (
	"strconv"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/pdf/lettura"
)

// inventario: RIP-S-42, RIP-S-43, RIP-S-45, RIP-S-46
// D-64 nei PDF (slice 058): la ricevuta, l'etichetta 76×127, la scheda PDF e il report scrivono il
// numero visibile della busta, quello dell'etichetta («0045/26», «0045/25») o, senza etichetta, il
// numero interno «N. 1/26»; il «#» solo dove lo standalone lo scrive (etichetta e report) e solo con
// un'etichetta. Il QR dell'etichetta resta il codice salvato, i nomi dei file e le righe del registro
// il numero interno. Sull'etichetta la data di stampa è «All'artigiano:» (D-73).
func TestNumeroVisibileNeiPDF(t *testing.T) {
	for _, c := range []struct {
		qr                                  string
		ricevuta, etichetta, scheda, report string
	}{
		{"RIP-2026-0045", "Busta 0045/26 CLIENTE", "#0045/26 All'artigiano: 31/10/2026", "N° BUSTA 0045/26 DATA RICEZIONE",
			"1 scheda #0045/26 Cliente storico"},
		{"RIP-2025-0045", "Busta 0045/25 CLIENTE", "#0045/25 All'artigiano: 31/10/2026", "N° BUSTA 0045/25 DATA RICEZIONE",
			"1 scheda #0045/25 Cliente storico"},
		{"SINT-ALTRO-36", "Busta N. 1/26 CLIENTE", "N. 1/26 All'artigiano: 31/10/2026", "N° BUSTA N. 1/26 DATA RICEZIONE",
			"1 scheda N. 1/26 Cliente storico"},
	} {
		h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
		id := strconv.FormatInt(bustaScheda(t, d, o, nil), 10)
		eseguiSQL(t, d, `UPDATE buste SET codice_qr = ? WHERE id = ?`, c.qr, id)
		b := titolareDentro(t, h)
		for _, x := range []struct{ percorso, file, atteso string }{
			{percorsoRicevuta(id), `attachment; filename="ricevuta-1-26.pdf"`, c.ricevuta},
			{percorsoEtichetta(id), `inline; filename="etichetta_busta_1_26.pdf"`, c.etichetta},
			{percorsoSchedaPDF(id), `inline; filename="busta_1_26.pdf"`, c.scheda},
			{percorsoReport + "?ids=" + id, `inline; filename="schede_1.pdf"`, c.report},
		} {
			r := b.vai("GET", x.percorso, nil, nil)
			if r.codice != 200 || !unaVolta(r, "Content-Disposition", x.file) {
				t.Fatalf("%s %s: %d %v", c.qr, x.percorso, r.codice, r.h)
			}
			l, err := lettura.Leggi([]byte(r.corpo))
			if err != nil {
				t.Fatal(err)
			}
			testo := lettura.TestoNormalizzato(l)
			if !strings.Contains(testo, x.atteso) || strings.Contains(testo, "#N.") || strings.Contains(testo, "Consegna:") {
				t.Errorf("%s %s: %q", c.qr, x.percorso, testo)
			}
			if x.percorso == percorsoEtichetta(id) {
				if qq, err := lettura.CodiciQR(l.Pagine[0]); err != nil || len(qq) != 1 || qq[0].Testo != c.qr {
					t.Errorf("%s: QR %v %+v", c.qr, err, qq)
				}
			}
		}
		if n := contaCon(t, d, `SELECT count(*) FROM eventi_audit WHERE azione IN ('repair.receipt_generated', 'repair.label_generated',
			'repair.sheet_generated', 'repair.report_generated') AND dati = '{"bag_number":"1/26"}'`); n != 4 {
			t.Errorf("%s: righe con il numero interno %d", c.qr, n)
		}
	}
}

// inventario: RIP-S-37
// La pagina della firma (D-64, slice 058; Stefano 06/11/2026, risposta 3 alla 053): titolo e
// intestazione con il numero visibile, come la scheda: «Firma busta #0045/26» e «✍️ Consegna busta
// #0045/26» con un'etichetta, «N. 1/26» senza «#» per una busta senza etichetta. Anche dopo «Firma
// mancante o non valida.», che torna alla stessa pagina.
func TestPaginaFirmaNumeroVisibile(t *testing.T) {
	p := facciaConFirma(t)
	for _, c := range []struct{ qr, numero string }{
		{"RIP-2026-0045", "#0045/26"}, {"rip-2025- 0012", "#0012/25"}, {"", "N. 1/26"}, {"SINT-QR-0001", "N. 1/26"},
	} {
		eseguiSQL(t, p.d, `UPDATE buste SET codice_qr = ? WHERE id = ?`, c.qr, p.busta)
		b := p.titolare(t)
		for _, r := range []risposta{b.vai("GET", percorsoFirma(p.busta), nil, nil), postFirma(b, p.busta, "-", "")} {
			if r.codice == 302 {
				r = b.vai("GET", r.location, nil, nil)
			}
			contiene(t, c.qr, r.corpo, "<title>Firma busta "+c.numero+" · Laboratorio</title>",
				`<h1 class="titolo-firma">✍️ Consegna busta `+c.numero+`</h1>`)
			if strings.Contains(r.corpo, "busta #1/26") || strings.Contains(r.corpo, "#N.") {
				t.Errorf("%s: numero interno col «#»", c.qr)
			}
			// Review 058: la pagina la vede il cliente; con un'etichetta il numero interno non c'è
			// da nessuna parte (né in piccolo come nella scheda, né nei link).
			if strings.HasPrefix(c.numero, "#") && strings.Contains(r.corpo, "1/26") {
				t.Errorf("%s: il numero interno è nella pagina", c.qr)
			}
		}
	}
}
