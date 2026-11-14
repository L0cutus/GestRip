package web

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/buste"
)

// La pagina del cliente con le date gg/mm/aaaa (D-84, slice 074c) e il numero senza «N.» (D-82
// estesa, slice 074c): sul link /r/, nell'anteprima del negozio e sulla pagina storica /stato. Dati
// inventati.

// reDataISO è una data AAAA-MM-GG, quella che lo standalone scrive sulla pagina.
var reDataISO = regexp.MustCompile(`\b[0-9]{4}-[0-9]{2}-[0-9]{2}\b`)

// dateBusta mette alla busta le quattro date della pagina.
const dateBusta = `UPDATE buste SET stato = 'consegnata', data_accettazione = '2026-10-01', data_prevista_testo = NULL,
	data_prevista = '2026-11-05', data_pronta = '2026-11-03', data_consegna = '2026-11-04', codice_qr = NULL WHERE id = ?`

// controllaDateENumero vuole nel testo dell'<article> le quattro date gg/mm/aaaa, il numero senza «N.»
// e nessuna data AAAA-MM-GG.
func controllaDateENumero(t *testing.T, dove, corpo, numero string) {
	t.Helper()
	testo := testoArticolo(corpo)
	contiene(t, dove, testo, "Accettata 01/10/2026", "Consegna prevista 05/11/2026", "Pronta dal 03/11/2026",
		"Consegnata 04/11/2026", "La tua riparazione "+numero+" ")
	nonContiene(t, dove, testo, "N. "+numero)
	if d := reDataISO.FindString(testo); d != "" {
		t.Errorf("%s: data AAAA-MM-GG %q nella pagina\n%s", dove, d, testo)
	}
	contiene(t, dove, corpo, `<strong class="font-mono cliente-numero">`+numero+`</strong>`)
}

// inventario: CLI-01, CLI-27
func TestPaginaClienteDateENumero(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	c := p.busta(t, nil)
	eseguiSQL(t, p.d, dateBusta, c.ID)
	cliente := nuovoBrowser(t, p.pubblica)
	r := cliente.vai("GET", "/r/"+c.Token, nil, nil)
	if r.codice != http.StatusOK {
		t.Fatalf("link: %d", r.codice)
	}
	numero := c.Numero
	if strings.HasPrefix(numero, "N.") || numero == "" {
		t.Fatalf("numero interno %q", numero)
	}
	controllaDateENumero(t, "link", r.corpo, numero)
	staff := titolareDentro(t, p.interna)
	a := staff.vai("GET", percorsoAnteprima(c.ID), nil, nil)
	if a.codice != http.StatusOK {
		t.Fatalf("anteprima: %d", a.codice)
	}
	controllaDateENumero(t, "anteprima", a.corpo, numero)
	// Un testo scritto al posto della data di consegna resta com'è.
	eseguiSQL(t, p.d, `UPDATE buste SET data_prevista_testo = 'fine mese' WHERE id = ?`, c.ID)
	contiene(t, "consegna scritta", testoArticolo(cliente.vai("GET", "/r/"+c.Token, nil, nil).corpo), "Consegna prevista fine mese")
	// Con l'etichetta RIP il numero dell'etichetta, senza «N.» come prima.
	eseguiSQL(t, p.d, `UPDATE buste SET codice_qr = 'RIP-2026-0045' WHERE id = ?`, c.ID)
	contiene(t, "etichetta", cliente.vai("GET", "/r/"+c.Token, nil, nil).corpo, `<strong class="font-mono cliente-numero">0045/26</strong>`)
	// Le pagine interne del negozio restano con «N.» (D-64).
	eseguiSQL(t, p.d, `UPDATE buste SET codice_qr = NULL WHERE id = ?`, c.ID)
	contiene(t, "scheda", staff.vai("GET", "/repairs/"+strconv.FormatInt(c.ID, 10), nil, nil).corpo, "N. "+numero)
}

// inventario: CLI-16
func TestPaginaStoricaDateENumero(t *testing.T) {
	p := paginaClienteProva(t)
	c := storicaWeb(t, p, nil)
	eseguiSQL(t, p.d, dateBusta, c.ID)
	r := nuovoBrowser(t, p.h).vai("GET", "/stato/0046/26?pw=CODICE", nil, nil)
	if r.codice != http.StatusOK {
		t.Fatalf("codice %d: %s", r.codice, r.corpo)
	}
	controllaDateENumero(t, "storica", r.corpo, "46/26")
}

// reAttributoISO è un attributo che comincia con una data AAAA-MM-GG; data-last-ts (l'istante
// dell'ultimo messaggio, per lo script) non si vede e resta com'è.
var reAttributoISO = regexp.MustCompile(`([a-z-]+)="[0-9]{4}-[0-9]{2}-[0-9]{2}`)

// inventario: CLI-05, CLI-27
// La data della risposta al preventivo, gg/mm/aaaa hh:mm (Clock.stamp/1), sul link e nell'anteprima del
// negozio, per la conferma e per il rifiuto (review della 074c: l'anteprima la lasciava vuota). Nessun
// attributo dell'<article> con una data AAAA-MM-GG, tranne data-last-ts.
func TestPaginaClienteDataRisposta(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	staff := titolareDentro(t, p.interna)
	cliente := nuovoBrowser(t, p.pubblica)
	for nome, q := range map[string]string{
		"conferma": `UPDATE buste SET stato = 'preventivo', preventivo_accettato_il = '2026-11-05T09:00:00.000000Z' WHERE id = ?`,
		"rifiuto":  `UPDATE buste SET stato = 'preventivo', preventivo_rifiutato_il = '2026-11-05T09:00:00.000000Z' WHERE id = ?`,
	} {
		c := p.busta(t, func(a *buste.Accettazione) { a.CodiceQR = "" })
		p.dice(t, c, "Quando è pronto?")
		eseguiSQL(t, p.d, q, c.ID)
		for dove, r := range map[string]risposta{
			"link":      cliente.vai("GET", "/r/"+c.Token, nil, nil),
			"anteprima": staff.vai("GET", percorsoAnteprima(c.ID), nil, nil),
		} {
			if r.codice != http.StatusOK {
				t.Fatalf("%s %s: %d", nome, dove, r.codice)
			}
			contiene(t, nome+" "+dove, testoArticolo(r.corpo), "il 05/11/2026 10:00")
			for _, m := range reAttributoISO.FindAllStringSubmatch(articolo(r.corpo), -1) {
				if m[1] != "data-last-ts" {
					t.Errorf("%s %s: attributo con data AAAA-MM-GG %q", nome, dove, m[0])
				}
			}
		}
	}
}
