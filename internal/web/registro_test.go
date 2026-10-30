package web

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// consultazioneFinta è il registro delle pagine che non lo guardano.
type consultazioneFinta struct{ err error }

func (c consultazioneFinta) Consulta(context.Context, audit.Richiesta, audit.Contesto) (audit.Pagina, error) {
	return audit.Pagina{}, c.err
}

// adessoRegistro è l'ora dell'orologio fermo di facciaConRegistro.
var adessoRegistro = time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)

func facciaConRegistro(t *testing.T, conti []contoProva) (http.Handler, *db.DB) {
	t.Helper()
	var d *db.DB
	h := conConsultazione(t, conti, func(dd *db.DB, o clock.Orologio) Consultazione { d = dd; return audit.Nuovo(dd, o) })
	return h, d
}

func evento(t *testing.T, d *db.DB, attore, azione, idRisorsa string, quando time.Time) int64 {
	t.Helper()
	r, err := d.Scrittura.Exec(`INSERT INTO eventi_audit (attore, azione, risorsa, id_risorsa, esito, dati, creato_il) VALUES (?, ?, 'repair', ?, 'ok', '{}', ?)`,
		attore, azione, idRisorsa, quando.UTC().Format(db.FormatoIstante))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := r.LastInsertId()
	return id
}

// rigaAudit è la riga id della tabella del registro.
func rigaAudit(corpo string, id int64) string {
	m := regexp.MustCompile(`(?s)<tr id="audit-row-` + fmt.Sprint(id) + `">.*?</tr>`).FindString(corpo)
	return m
}

// inventario: ADM-16
// Come «the global audit is owner-only and carries its filters, page and exports» e «an event is
// stamped on the shop's clock with its seconds, not UTC»: titolo, filtri con i valori presenti,
// la riga con quando (ora del negozio, ai secondi), attore, azione, «risorsa #id» ed esito; il
// numero degli eventi; l'apertura scritta come audit.viewed e non mostrata.
func TestPaginaRegistro(t *testing.T) {
	h, d := facciaConRegistro(t, contiBase)
	id := evento(t, d, "titolare@esempio.it", "repair.updated", "17", time.Date(2026, 10, 29, 8, 5, 9, 0, time.UTC))
	b := titolareDentro(t, h)
	r := b.vai("GET", "/audit", nil, nil)
	if r.codice != http.StatusOK {
		t.Fatalf("codice %d", r.codice)
	}
	for _, s := range []string{
		"Registro attività", "Registro delle operazioni e delle letture sensibili dell'istanza.",
		`<form id="audit-filter" action="/audit" method="get">`,
		`<select name="actor">`, `<option value="">Tutti</option>`, `<option value="titolare@esempio.it">titolare@esempio.it</option>`,
		`<select name="action">`, `<option value="">Tutte</option>`, `<option value="repair.updated">repair.updated</option>`,
		`<select name="giorni">`, `<option value="7">Ultimi 7 giorni</option>`, `<option value="30" selected>Ultimi 30 giorni</option>`,
		`<option value="90">Ultimi 90 giorni</option>`, `<option value="365">Ultimi 365 giorni</option>`,
		"<button>Filtra</button>", `<p id="audit-total">`,
		"<th>Quando</th><th>Attore</th><th>Azione</th><th>Risorsa</th><th>Esito</th>",
	} {
		if !strings.Contains(r.corpo, s) {
			t.Errorf("manca %q", s)
		}
	}
	riga := rigaAudit(r.corpo, id)
	for _, s := range []string{"<td>29/10/2026 09:05:09</td>", "<td>titolare@esempio.it</td>", "<td>repair.updated</td>", "<td>repair #17</td>", "<td>ok</td>"} {
		if !strings.Contains(riga, s) {
			t.Errorf("riga senza %q: %s", s, riga)
		}
	}
	if strings.Contains(r.corpo, "audit-pagination") || strings.Contains(r.corpo, "audit.viewed") {
		t.Error("paginazione o riga dell'apertura sulla pagina")
	}
	var n int
	if err := d.Lettura.QueryRow(`SELECT count(*) FROM eventi_audit WHERE azione = 'audit.viewed' AND attore = 'titolare@esempio.it'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("aperture registrate: %d %v", n, err)
	}
	if !strings.Contains(r.corpo, fmt.Sprintf(`<p id="audit-total">%d eventi</p>`, contaRighe(t, d, `SELECT count(*) FROM eventi_audit`)-1)) {
		t.Error("numero degli eventi")
	}
}

// inventario: ADM-16
// I filtri sono un modulo GET su /audit con actor, action e giorni: la pagina mostra solo le righe
// che li rispettano e li ripropone scelti.
func TestRegistroFiltri(t *testing.T) {
	h, d := facciaConRegistro(t, contiBase)
	dentro := evento(t, d, "titolare@esempio.it", "repair.updated", "17", adessoRegistro.Add(-2*24*time.Hour))
	vecchio := evento(t, d, "titolare@esempio.it", "repair.updated", "18", adessoRegistro.Add(-8*24*time.Hour))
	altro := evento(t, d, "banco@esempio.it", "sale.issued", "19", adessoRegistro.Add(-time.Hour))
	b := titolareDentro(t, h)
	r := b.vai("GET", "/audit?action=repair.updated&actor=titolare%40esempio.it&giorni=7", nil, nil)
	if rigaAudit(r.corpo, dentro) == "" || rigaAudit(r.corpo, vecchio) != "" || rigaAudit(r.corpo, altro) != "" {
		t.Errorf("righe filtrate sbagliate")
	}
	for _, s := range []string{`<option value="titolare@esempio.it" selected>`, `<option value="repair.updated" selected>`,
		`<option value="7" selected>`, `<option value="banco@esempio.it">`, `<p id="audit-total">1 eventi</p>`} {
		if !strings.Contains(r.corpo, s) {
			t.Errorf("manca %q", s)
		}
	}
	if strings.Contains(r.corpo, `<option value="30" selected>`) {
		t.Error("30 giorni ancora scelto")
	}
	// Un campo ripetuto vale per l'ultimo, come Plug.
	r = b.vai("GET", "/audit?actor=banco%40esempio.it&actor=titolare%40esempio.it&giorni=90&giorni=7&action=x&action=repair.updated", nil, nil)
	if rigaAudit(r.corpo, dentro) == "" || rigaAudit(r.corpo, altro) != "" || !strings.Contains(r.corpo, `<option value="7" selected>`) {
		t.Error("campi ripetuti: non vale l'ultimo")
	}
	// La query si legge come Plug.Conn.Query (misurato sul banco): un «%» illeggibile resta
	// com'è e filtra per quel testo, il «+» è uno spazio, il «;» fa parte del valore.
	for q, vede := range map[string]bool{
		"/audit?action=%zz":                    false,
		"/audit?action=+repair.updated+":       true,
		"/audit?action=repair.updated;x":       false,
		"/audit?action=repair%2Eupdated":       true,
		"/audit?giorni=%zz&action=sale.issued": true,
	} {
		r = b.vai("GET", q, nil, nil)
		if r.codice != http.StatusOK || (rigaAudit(r.corpo, dentro) != "" || rigaAudit(r.corpo, altro) != "") != vede {
			t.Errorf("%s: righe %v, attese %v", q, !vede, vede)
		}
	}
}

// inventario: ADM-16
// Oltre 100 eventi: «Pagina 1 di 2» e «Successiva», con i filtri e la fotografia (fino_a)
// nell'indirizzo; la seconda pagina ha «Precedente» e non «Successiva», e le righe arrivate nel
// frattempo non la spostano.
func TestRegistroPagine(t *testing.T) {
	h, d := facciaConRegistro(t, contiBase)
	var ultimo int64
	for i := 0; i < 150; i++ {
		ultimo = evento(t, d, "titolare@esempio.it", "repair.updated", fmt.Sprint(i), adessoRegistro.Add(-time.Duration(i+1)*time.Minute))
	}
	b := titolareDentro(t, h)
	r := b.vai("GET", "/audit?actor=titolare%40esempio.it", nil, nil)
	if !strings.Contains(r.corpo, `<div id="audit-pagination">`) || !strings.Contains(r.corpo, "<span>Pagina 1 di 2</span>") ||
		strings.Contains(r.corpo, "Precedente") {
		t.Fatalf("prima pagina: %s", r.corpo)
	}
	succ := regexp.MustCompile(`<a href="([^"]*)">Successiva</a>`).FindStringSubmatch(r.corpo)
	fino := fmt.Sprint(contaRighe(t, d, `SELECT max(id) FROM eventi_audit`) - 1)
	if succ == nil || succ[1] != "/audit?actor=titolare%40esempio.it&amp;fino_a="+fino+"&amp;giorni=30&amp;pagina=2" {
		t.Fatalf("Successiva: %v", succ)
	}
	evento(t, d, "titolare@esempio.it", "repair.updated", "nuovo", adessoRegistro)
	r = b.vai("GET", strings.ReplaceAll(succ[1], "&amp;", "&"), nil, nil)
	if !strings.Contains(r.corpo, "<span>Pagina 2 di 2</span>") || strings.Contains(r.corpo, "Successiva") ||
		!strings.Contains(r.corpo, `<a href="/audit?actor=titolare%40esempio.it&amp;fino_a=`+fino+`&amp;giorni=30&amp;pagina=1">Precedente</a>`) {
		t.Fatalf("seconda pagina: %s", r.corpo)
	}
	if rigaAudit(r.corpo, ultimo) == "" || strings.Count(r.corpo, `<tr id="audit-row-`) != 50 {
		t.Errorf("seconda pagina: %d righe", strings.Count(r.corpo, `<tr id="audit-row-`))
	}
}

// inventario: ADM-16, ACC-05
// Come «an owner demoted with the audit open is refused on the next filter / next page»: ogni
// richiesta ricontrolla il ruolo; il dipendente va a / con «Operazione riservata al titolare.»
// senza leggere il registro.
func TestRegistroTitolareRetrocesso(t *testing.T) {
	h, d := facciaConRegistro(t, contiBase)
	b := titolareDentro(t, h)
	if r := b.vai("GET", "/audit", nil, nil); r.codice != http.StatusOK {
		t.Fatalf("aperto: %d", r.codice)
	}
	if _, err := d.Scrittura.Exec(`UPDATE utenti SET ruolo = 'dipendente' WHERE email = 'titolare@esempio.it'`); err != nil {
		t.Fatal(err)
	}
	prima := contaRighe(t, d, `SELECT count(*) FROM eventi_audit`)
	for _, p := range []string{"/audit?giorni=30", "/audit?pagina=2&fino_a=1"} {
		if r := b.vai("GET", p, nil, nil); !redirectA(r, "/") {
			t.Errorf("%s: %d %q", p, r.codice, r.location)
		}
	}
	if r := b.vai("GET", "/", nil, nil); !strings.Contains(r.corpo, "Operazione riservata al titolare.") {
		t.Error("messaggio del rifiuto")
	}
	if contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'audit.viewed'`) != 1 || contaRighe(t, d, `SELECT count(*) FROM eventi_audit`) != prima {
		t.Error("il dipendente ha letto il registro")
	}
}

// inventario: ADM-16
// Se la lettura non riesce: «Audit non disponibile.», la pagina vuota con i filtri di partenza.
func TestRegistroNonDisponibile(t *testing.T) {
	b := titolareDentro(t, conConsultazione(t, contiBase, func(*db.DB, clock.Orologio) Consultazione {
		return consultazioneFinta{err: errors.New("disco pieno")}
	}))
	r := b.vai("GET", "/audit", nil, nil)
	if r.codice != http.StatusOK || !strings.Contains(r.corpo, "Audit non disponibile.") ||
		!strings.Contains(r.corpo, `<p id="audit-total">0 eventi</p>`) || !strings.Contains(r.corpo, `<option value="30" selected>`) ||
		strings.Contains(r.corpo, "disco pieno") {
		t.Errorf("rifiuto: %d %s", r.codice, r.corpo)
	}
}

// conConsultazione è la faccia interna con account veri e il registro che dà c.
func conConsultazione(t *testing.T, conti []contoProva, c func(*db.DB, clock.Orologio) Consultazione) http.Handler {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(adessoRegistro)
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	inserisciConti(t, d, conti)
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti, dd.Consultazione = o, ratelimit.Nuovo(o), c(d, o)
	return mustInterna(t, dd)
}

// inventario: ADM-16
// I link di pagina codificano i filtri: un attore con «&», «+» e spazi torna uguale.
func TestRegistroLinkCodificati(t *testing.T) {
	h, d := facciaConRegistro(t, contiBase)
	strano := "a&b+c d@esempio.it"
	for i := 0; i < 101; i++ {
		evento(t, d, strano, "x.y", fmt.Sprint(i), adessoRegistro.Add(-time.Duration(i+1)*time.Second))
	}
	b := titolareDentro(t, h)
	r := b.vai("GET", "/audit?actor=a%26b%2Bc+d%40esempio.it", nil, nil)
	succ := regexp.MustCompile(`<a href="([^"]*)">Successiva</a>`).FindStringSubmatch(r.corpo)
	// html/template scrive «+» come &#43;: il browser lo legge come lo stesso indirizzo.
	if succ == nil || !strings.HasPrefix(html.UnescapeString(succ[1]), "/audit?actor=a%26b%2Bc+d%40esempio.it&") {
		t.Fatalf("Successiva: %v", succ)
	}
	r = b.vai("GET", html.UnescapeString(succ[1]), nil, nil)
	if !strings.Contains(r.corpo, "<span>Pagina 2 di 2</span>") || strings.Count(r.corpo, `<tr id="audit-row-`) != 1 {
		t.Errorf("seconda pagina con l'attore strano")
	}
}

// inventario: ADM-16
// I valori del registro sono testo: un attore o un'azione con HTML escono escapati, nelle righe e
// nei filtri.
func TestRegistroEscape(t *testing.T) {
	h, d := facciaConRegistro(t, contiBase)
	evento(t, d, `<b>x</b>`, `"><script>`, "1", adessoRegistro.Add(-time.Hour))
	r := titolareDentro(t, h).vai("GET", "/audit", nil, nil)
	if strings.Contains(r.corpo, "<b>x</b>") || strings.Contains(r.corpo, `"><script>`) || !strings.Contains(r.corpo, "&lt;b&gt;x&lt;/b&gt;") {
		t.Error("valori non escapati")
	}
}
