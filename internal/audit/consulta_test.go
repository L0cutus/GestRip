package audit

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// adessoProva è l'ora dell'orologio di registroProva.
var adessoProva = time.Date(2026, 10, 30, 9, 15, 7, 123456789, time.UTC)

var titolareProva = Contesto{Attore: "titolare@esempio.it"}

// inserisci scrive le righe come Repo.insert_all dei test dello standalone, con l'ora data.
func inserisci(t *testing.T, d *db.DB, attore, azione string, quando time.Time) int64 {
	t.Helper()
	r, err := d.Scrittura.Exec(`INSERT INTO eventi_audit (attore, azione, risorsa, id_risorsa, esito, dati, creato_il) VALUES (?, ?, 'repair', '1', 'ok', '{}', ?)`,
		attore, azione, quando.UTC().Format(db.FormatoIstante))
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func conta(t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	if err := d.Lettura.QueryRow(`SELECT count(*) FROM eventi_audit`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// inventario: ADM-16
// Come «filters exact actors and actions inside the requested period and pages by one hundred»:
// filtri esatti su attore e azione, 100 per pagina, la pagina oltre l'ultima torna all'ultima, i
// valori dei filtri tra quelli presenti, e l'apertura scritta come audit.viewed con i filtri
// usati, nella stessa transazione.
func TestConsultaFiltriEPagine(t *testing.T) {
	r, d := registroProva(t)
	for i := 1; i <= 101; i++ {
		attore, azione := "titolare@esempio.it", "repair.updated"
		if i == 101 {
			attore, azione = "staff@esempio.it", "sale.issued"
		}
		inserisci(t, d, attore, azione, adessoProva.Add(-time.Duration(i)*time.Second))
	}
	p, err := r.Consulta(ctx, Richiesta{Attore: "titolare@esempio.it", Azione: "repair.updated", Giorni: "30", Pagina: "2"}, titolareProva)
	if err != nil {
		t.Fatal(err)
	}
	if p.Totale != 100 || p.Pagina != 1 || p.Pagine != 1 || len(p.Eventi) != 100 {
		t.Fatalf("totale %d pagina %d di %d, %d eventi", p.Totale, p.Pagina, p.Pagine, len(p.Eventi))
	}
	for _, e := range p.Eventi {
		if e.Attore != "titolare@esempio.it" || e.Azione != "repair.updated" {
			t.Fatalf("evento fuori dal filtro: %+v", e)
		}
	}
	if !reflect.DeepEqual(p.Azioni, []string{"repair.updated", "sale.issued"}) ||
		!reflect.DeepEqual(p.Attori, []string{"staff@esempio.it", "titolare@esempio.it"}) {
		t.Errorf("valori dei filtri: %v %v", p.Azioni, p.Attori)
	}
	if p.Filtri != (Filtri{Attore: "titolare@esempio.it", Azione: "repair.updated", Giorni: 30}) {
		t.Errorf("filtri %+v", p.Filtri)
	}
	got := ultima(t, d)
	want := riga{"titolare@esempio.it", "audit.viewed", "audit", "global", "ok", `{"filters":["actor","action","days"]}`, nil, nil, "2026-10-30T09:15:07.123456Z"}
	if got != want {
		t.Errorf("riga %+v\natteso %+v", got, want)
	}
	// La sola azione filtra da sola.
	if p, err = r.Consulta(ctx, Richiesta{Azione: "sale.issued"}, titolareProva); err != nil || p.Totale != 1 || p.Eventi[0].Attore != "staff@esempio.it" {
		t.Errorf("solo azione: %d %v", p.Totale, err)
	}
	if ultima(t, d).dati != `{"filters":["action","days"]}` {
		t.Errorf("dati %s", ultima(t, d).dati)
	}
}

// inventario: ADM-16
// Senza filtri: dal più recente (a parità d'ora, l'id più alto prima), 100 per pagina, l'ultima
// pagina con il resto; nei dati di audit.viewed solo «days».
func TestConsultaOrdineEPaginaDue(t *testing.T) {
	r, d := registroProva(t)
	stessa := adessoProva.Add(-time.Hour)
	a := inserisci(t, d, "uno@esempio.it", "x.uno", stessa)
	b := inserisci(t, d, "due@esempio.it", "x.due", stessa)
	for i := 0; i < 100; i++ {
		inserisci(t, d, "tre@esempio.it", "x.tre", adessoProva.Add(-time.Duration(i+1)*time.Second))
	}
	p, err := r.Consulta(ctx, Richiesta{}, titolareProva)
	if err != nil {
		t.Fatal(err)
	}
	if p.Totale != 102 || p.Pagine != 2 || p.Pagina != 1 || len(p.Eventi) != 100 || p.Eventi[0].Azione != "x.tre" {
		t.Fatalf("prima pagina: totale %d, %d di %d, %d eventi", p.Totale, p.Pagina, p.Pagine, len(p.Eventi))
	}
	if !p.Eventi[0].Quando.After(p.Eventi[1].Quando) || p.Eventi[0].Quando.Location() != time.UTC {
		t.Errorf("ordine o fuso: %v %v", p.Eventi[0].Quando, p.Eventi[1].Quando)
	}
	if ultima(t, d).dati != `{"filters":["days"]}` {
		t.Errorf("dati %s", ultima(t, d).dati)
	}
	p, err = r.Consulta(ctx, Richiesta{Pagina: "2", FinoA: fmt.Sprint(p.FinoA)}, titolareProva)
	if err != nil {
		t.Fatal(err)
	}
	if p.Pagina != 2 || len(p.Eventi) != 2 || p.Eventi[0].ID != b || p.Eventi[1].ID != a {
		t.Fatalf("seconda pagina: %+v", p.Eventi)
	}
	e := p.Eventi[0]
	if e.Attore != "due@esempio.it" || e.Risorsa != "repair" || e.IDRisorsa != "1" || e.Esito != "ok" || !e.Quando.Equal(stessa.Truncate(time.Microsecond)) {
		t.Errorf("evento %+v", e)
	}
}

// inventario: ADM-16
// Come «pagination stays on one snapshot while newer audit rows arrive»: con fino_a le righe nuove
// (comprese quelle delle aperture) non spostano le pagine; le due pagine insieme sono tutte le
// righe di prima.
func TestConsultaFotografia(t *testing.T) {
	r, d := registroProva(t)
	attese := map[int64]bool{}
	for i := 1; i <= 150; i++ {
		attese[inserisci(t, d, "titolare@esempio.it", "repair.updated", adessoProva.Add(-time.Duration(i)*time.Second))] = true
	}
	prima, err := r.Consulta(ctx, Richiesta{Pagina: "1"}, titolareProva)
	if err != nil {
		t.Fatal(err)
	}
	inserisci(t, d, "nuovo@esempio.it", "new.event", adessoProva)
	seconda, err := r.Consulta(ctx, Richiesta{Pagina: "2", FinoA: fmt.Sprint(prima.FinoA)}, titolareProva)
	if err != nil {
		t.Fatal(err)
	}
	if seconda.FinoA != prima.FinoA || prima.FinoA != 150 {
		t.Errorf("fotografia %d poi %d", prima.FinoA, seconda.FinoA)
	}
	viste := map[int64]bool{}
	for _, e := range append(prima.Eventi, seconda.Eventi...) {
		viste[e.ID] = true
	}
	if !reflect.DeepEqual(viste, attese) {
		t.Errorf("righe viste %d, attese %d", len(viste), len(attese))
	}
	// Senza fino_a la fotografia è l'ultima riga: l'apertura di prima ora si vede, quella di
	// adesso no.
	p, err := r.Consulta(ctx, Richiesta{}, titolareProva)
	if err != nil {
		t.Fatal(err)
	}
	if p.FinoA != 153 || p.Totale != 153 || p.Eventi[0].ID == 154 || conta(t, d) != 154 {
		t.Errorf("senza fino_a: fino a %d, totale %d, righe %d", p.FinoA, p.Totale, conta(t, d))
	}
	// fino_a oltre l'ultima riga vale l'ultima; più basso limita le righe.
	if p, err = r.Consulta(ctx, Richiesta{FinoA: "99999"}, titolareProva); err != nil || p.FinoA != 154 {
		t.Errorf("fino_a alto: %d %v", p.FinoA, err)
	}
	if p, err = r.Consulta(ctx, Richiesta{FinoA: "10"}, titolareProva); err != nil || p.FinoA != 10 || p.Totale != 10 {
		t.Errorf("fino_a basso: %d %d %v", p.FinoA, p.Totale, err)
	}
	// Un fino_a che non è un intero positivo vale l'ultima riga.
	for _, f := range []string{"abc", "0", "-1", "10.5", " 10"} {
		ultimo := int64(conta(t, d))
		if p, err = r.Consulta(ctx, Richiesta{FinoA: f}, titolareProva); err != nil || p.FinoA != ultimo {
			t.Errorf("fino_a %q: %d, atteso %d (%v)", f, p.FinoA, ultimo, err)
		}
	}
}

// inventario: ADM-16
// Su un registro vuoto la fotografia è 0, una pagina, nessun evento; l'apertura si scrive lo stesso.
func TestConsultaRegistroVuoto(t *testing.T) {
	r, d := registroProva(t)
	p, err := r.Consulta(ctx, Richiesta{FinoA: "5", Pagina: "3"}, titolareProva)
	if err != nil {
		t.Fatal(err)
	}
	if p.FinoA != 0 || p.Totale != 0 || p.Pagina != 1 || p.Pagine != 1 || len(p.Eventi) != 0 || len(p.Attori) != 0 {
		t.Errorf("vuoto: %+v", p)
	}
	if conta(t, d) != 1 {
		t.Errorf("righe %d", conta(t, d))
	}
}

// inventario: ADM-16
// Il periodo: giorni da adesso, il confine compreso; default 30, massimo 365; un valore che non è
// un intero positivo intero (Integer.parse con resto vuoto) vale 30, uno enorme vale 365.
func TestConsultaGiorni(t *testing.T) {
	r, d := registroProva(t)
	inserisci(t, d, "a@esempio.it", "x.sette", adessoProva.Add(-7*24*time.Hour))
	inserisci(t, d, "a@esempio.it", "x.otto", adessoProva.Add(-8*24*time.Hour))
	inserisci(t, d, "a@esempio.it", "x.trentuno", adessoProva.Add(-31*24*time.Hour))
	inserisci(t, d, "a@esempio.it", "x.anno", adessoProva.Add(-365*24*time.Hour))
	inserisci(t, d, "a@esempio.it", "x.oltre", adessoProva.Add(-365*24*time.Hour-time.Microsecond))
	casi := []struct {
		giorni string
		valore int
		righe  int
	}{
		{"", 30, 2}, {"7", 7, 1}, {"+7", 7, 1}, {"007", 7, 1}, {"8", 8, 2}, {"31", 31, 3}, {"365", 365, 4},
		{"366", 365, 4}, {"1000", 365, 4}, {"99999999999999999999999", 365, 4},
		{"0", 30, 2}, {"-7", 30, 2}, {"7.5", 30, 2}, {" 7", 30, 2}, {"7 ", 30, 2}, {"sette", 30, 2}, {"+", 30, 2}, {"--7", 30, 2},
	}
	for _, c := range casi {
		p, err := r.Consulta(ctx, Richiesta{Giorni: c.giorni, Azione: ""}, titolareProva)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, e := range p.Eventi {
			if strings.HasPrefix(e.Azione, "x.") {
				n++
			}
		}
		if p.Filtri.Giorni != c.valore || n != c.righe {
			t.Errorf("giorni %q: valore %d, %d righe; attesi %d e %d", c.giorni, p.Filtri.Giorni, n, c.valore, c.righe)
		}
	}
}

// inventario: ADM-16
// Attore e azione senza spazi ai lati, confronto esatto; la pagina come intero positivo, oltre
// l'ultima vale l'ultima (anche enorme).
func TestConsultaValoriDellIndirizzo(t *testing.T) {
	r, d := registroProva(t)
	for i := 0; i < 250; i++ {
		inserisci(t, d, "a@esempio.it", "x.uno", adessoProva.Add(-time.Duration(i+1)*time.Second))
	}
	inserisci(t, d, "A@esempio.it", "x.uno", adessoProva.Add(-time.Hour))
	p, err := r.Consulta(ctx, Richiesta{Attore: "  a@esempio.it\t", Azione: " x.uno "}, titolareProva)
	if err != nil {
		t.Fatal(err)
	}
	if p.Totale != 250 || p.Filtri.Attore != "a@esempio.it" || p.Filtri.Azione != "x.uno" {
		t.Errorf("filtri senza spazi: %d %+v", p.Totale, p.Filtri)
	}
	for _, c := range []struct {
		pagina string
		vale   int
	}{{"2", 2}, {"3", 3}, {"4", 3}, {"99999999999999999999999", 3}, {"0", 1}, {"-1", 1}, {"x", 1}, {"+2", 2}} {
		p, err := r.Consulta(ctx, Richiesta{Pagina: c.pagina}, titolareProva)
		if err != nil {
			t.Fatal(err)
		}
		if p.Pagina != c.vale {
			t.Errorf("pagina %q: %d, attesa %d", c.pagina, p.Pagina, c.vale)
		}
	}
	p, err = r.Consulta(ctx, Richiesta{Pagina: "3", FinoA: "251"}, titolareProva)
	if err != nil || len(p.Eventi) != 51 {
		t.Errorf("ultima pagina: %d eventi, %v", len(p.Eventi), err)
	}
}

// inventario: ADM-16
// Come «an anonymous read fails closed»: senza attore la riga dell'apertura non si scrive e la
// lettura non dà niente.
func TestConsultaSenzaAttore(t *testing.T) {
	r, d := registroProva(t)
	inserisci(t, d, "titolare@esempio.it", "repair.updated", adessoProva)
	if p, err := r.Consulta(ctx, Richiesta{}, Contesto{}); err == nil || len(p.Eventi) != 0 {
		t.Errorf("senza attore: %v, %d eventi", err, len(p.Eventi))
	}
	// Un attore che il registro non accetta (oltre 255 caratteri): stessa chiusura.
	if p, err := r.Consulta(ctx, Richiesta{}, Contesto{Attore: strings.Repeat("a", 256)}); err == nil || len(p.Eventi) != 0 || p.Totale != 0 {
		t.Errorf("attore troppo lungo: %v, %d eventi", err, len(p.Eventi))
	}
	if conta(t, d) != 1 {
		t.Errorf("righe %d", conta(t, d))
	}
}

// inventario: ADM-16
// L'orologio del negozio decide il periodo: un orologio fermo diverso cambia le righe dentro.
func TestConsultaOrologio(t *testing.T) {
	r, d := registroProva(t)
	inserisci(t, d, "a@esempio.it", "x.uno", adessoProva.Add(-10*24*time.Hour))
	r.o = clock.NuovoFinto(adessoProva.Add(25 * 24 * time.Hour))
	p, err := r.Consulta(ctx, Richiesta{Giorni: "30"}, titolareProva)
	if err != nil || p.Totale != 0 {
		t.Errorf("con l'orologio avanti: %d %v", p.Totale, err)
	}
}
