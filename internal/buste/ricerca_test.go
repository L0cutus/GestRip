package buste

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// La ricerca della home dopo D-63 e D-64 (ricerca.go): il numero d'etichetta scritto, i filtri che
// la ricerca per etichetta toglie, la scansione con la sua riga repair.qr_lookup e la busta che si
// apre senza scelta.

// inventario: RIP-H-03
// D-64, risposta 3 del 05/11/2026: da 1 a 4 cifre, con o senza «/AA», è un numero d'etichetta; ogni
// altra forma resta un «contiene».
func TestLeggiRicercaEtichetta(t *testing.T) {
	for cerca, voluto := range map[string]string{
		"0045/26": "45 26", "045/26": "45 26", "45/26": "45 26", "0045": "45 ", "45": "45 ", "0012": "12 ", "1": "1 ",
		"9999/25": "9999 25", "012/26": "12 26", "0001/00": "1 00",
		"0": "no", "0000": "no", "00/26": "no", "00045": "no", "12345": "no", "45/2026": "no", "45/6": "no", "#45": "no",
		"45 /26": "no", "4 5": "no", "": "no", "anello": "no", "RIP-2026-0045": "no", "45/26/1": "no", "٤٥": "no", "+45": "no",
		"-45": "no", "45/": "no", "/26": "no",
	} {
		e, ok := LeggiRicercaEtichetta(cerca)
		got := "no"
		if ok {
			got = fmt.Sprint(e.Numero, " ", e.Anno)
		}
		if got != voluto {
			t.Errorf("%q: %q, voluto %q", cerca, got, voluto)
		}
	}
}

// inventario: RIP-H-03, RIP-H-11
// Trova: l'etichetta col suo numero e il suo anno (come NumeroVisibile), il numero interno solo per
// le buste senza etichetta, senza gli zeri davanti.
func TestRicercaEtichettaTrova(t *testing.T) {
	s := func(v string) *string { return &v }
	tutti := RicercaEtichetta{Numero: 45}
	del26 := RicercaEtichetta{Numero: 45, Anno: "26"}
	del25 := RicercaEtichetta{Numero: 45, Anno: "25"}
	for _, c := range []struct {
		numero string
		qr     *string
		e      RicercaEtichetta
		voluto bool
	}{
		{"21/26", s("RIP-2026-0045"), tutti, true},
		{"21/26", s("RIP-2026-0045"), del26, true},
		{"21/26", s("RIP-2026-0045"), del25, false},
		{"25/26", s("RIP-2025-0045"), del25, true},
		{"25/26", s(" RIP-2025-0045 "), tutti, true},
		{"45/26", s("RIP-2026-0012"), tutti, false}, // il numero interno di una busta con l'etichetta non si cerca
		{"45/26", nil, tutti, true},
		{"45/26", s(""), del26, true},
		{"45/26", s("SINT-QR-0045"), del26, true}, // un codice che non è un'etichetta: il numero interno
		{"045/26", nil, del26, true},
		{"45/25", nil, del26, false},
		{"145/26", nil, tutti, false},
		{"4/26", nil, tutti, false},
		{"21/26", s("RIP-2026-00045"), tutti, false},
		{"21/26", s("RIP-2026-0145"), tutti, false},
		{"21/26", s("rip-2026-0045"), tutti, false},
		{"45/26", s("rip-2026-0045"), tutti, true},
		{"10000/25", s("RIP-2025-10000"), RicercaEtichetta{Numero: 1000}, false},
		{"9999/25", s("RIP-2025-0012"), RicercaEtichetta{Numero: 12}, true},
		{"x", nil, tutti, false},
	} {
		if got := c.e.Trova(c.numero, c.qr); got != c.voluto {
			q := "nil"
			if c.qr != nil {
				q = *c.qr
			}
			t.Errorf("%s %q %+v: %v", c.numero, q, c.e, got)
		}
	}
}

// inventario: RIP-H-03, RIP-H-04, RIP-H-06, RIP-H-11
// D-64 nella lista: «0045/26», «045/26», «0045» e «45» danno tutte le buste che mostrano quel numero,
// di tutti gli anni senza «/AA» (le più recenti prima, cioè l'ordine della lista), chiuse e
// consegnate comprese e senza nessun altro filtro né il nascondimento; il numero interno solo per le
// buste senza etichetta; mai un «contiene» (telefoni, descrizioni, 145/26). Il cestino resta fuori.
func TestListaRicercaEtichetta(t *testing.T) {
	b, d, _ := prova(t)
	consegnata := bustaLista(t, b, "21/26", "RIP-2026-0045", `stato = 'consegnata'`)
	aperta := bustaLista(t, b, "22/26", "RIP-2026-0045", "")
	vecchia := bustaLista(t, b, "25/26", "RIP-2025-0045", `stato = 'pronta'`)
	interna := bustaLista(t, b, "45/26", "", "")
	altroCodice := bustaLista(t, b, "45/25", "SINT-QR-1", `stato = 'annullata'`)
	conEtichetta := bustaLista(t, b, "45/24", "RIP-2024-0099", "")
	bustaLista(t, b, "145/26", "", `descrizione = 'anello 0045', telefono_cliente = '+390045'`)
	bustaLista(t, b, "3/26", "RIP-2026-0145", `note_interne = '45'`)
	bustaLista(t, b, "4/26", "RIP-2026-0045", `cancellata_il = ?`, istanteProva)
	dodici := bustaLista(t, b, "12/26", "", "")
	dodiciEtichetta := bustaLista(t, b, "9999/25", "RIP-2025-0012", "")
	// Un numero interno con lo zero davanti (import) e un'etichetta salvata con lo spazio in fondo: le
	// candidate di busteDellEtichetta li prendono tutti e due (review della 052, punto 7).
	zeri := bustaLista(t, b, "98/23", "", `numero = '045/23'`)
	spazio := bustaLista(t, b, "50/26", "Q", `codice_qr = 'RIP-2026-0045 '`)
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('home_mostra_pronte', '0', ?), ('home_mostra_consegnate', '0', ?)`, istanteProva, istanteProva)
	tutte45 := []int64{spazio, interna, vecchia, aperta, consegnata, altroCodice, zeri}
	for _, c := range []struct {
		f      Filtri
		voluti []int64
	}{
		{Filtri{Cerca: "45"}, tutte45},
		{Filtri{Cerca: "0045"}, tutte45},
		{Filtri{Cerca: "0045/26"}, []int64{spazio, interna, aperta, consegnata}},
		{Filtri{Cerca: "045/26"}, []int64{spazio, interna, aperta, consegnata}},
		{Filtri{Cerca: "45/23"}, []int64{zeri}},
		{Filtri{Cerca: "45/25"}, []int64{vecchia, altroCodice}},
		{Filtri{Cerca: "0045/24"}, nil},
		{Filtri{Cerca: "0099"}, []int64{conEtichetta}},
		{Filtri{Cerca: "0012"}, []int64{dodici, dodiciEtichetta}},
		{Filtri{Cerca: "12/25"}, []int64{dodiciEtichetta}},
		{Filtri{Cerca: "12/26"}, []int64{dodici}},
		{Filtri{Cerca: "7"}, nil},
		// Tutti i filtri ignorati (risposta 4), anche quelli che non c'entrano niente.
		{Filtri{Cerca: "45", Stato: "pronta", CompraOro: true, Scadenza: true, Messaggi: true, Oreficeria: 9, Artigiano: 9,
			DataDa: "2030-01-01", DataCampo: "consegnata"}, tutte45},
	} {
		l := lista(t, b, c.f)
		if got := idDi(l); !slices.Equal(got, c.voluti) {
			t.Errorf("%+v: %v, voluti %v", c.f, got, c.voluti)
		}
		if l.Totale != int64(len(c.voluti)) {
			t.Errorf("%+v: totale %d", c.f, l.Totale)
		}
		if want := (Filtri{Cerca: c.f.Cerca, DataCampo: CampoDataPredefinito, Pagina: 1}); l.Filtri != want {
			t.Errorf("%+v: filtri della lista %+v", c.f, l.Filtri)
		}
		if n, ids, err := b.Selezione(ctx, c.f, 50); err != nil || n != int64(len(c.voluti)) || !slices.Equal(ids, c.voluti) {
			t.Errorf("%+v: selezione %d %v %v", c.f, n, ids, err)
		}
	}
	// Un testo che non è un numero d'etichetta resta un «contiene» con i filtri.
	if got := idDi(lista(t, b, Filtri{Cerca: "0045/26 "})); len(got) != 0 {
		t.Errorf("«0045/26 » con lo spazio dentro (il modello non toglie gli spazi): %v", got)
	}
	if got := idDi(lista(t, b, Filtri{Cerca: "anello 0045", Stato: "in_lavorazione"})); len(got) != 1 {
		t.Errorf("contiene: %v", got)
	}
	if got := idDi(lista(t, b, Filtri{Cerca: "RIP-2026-004", Stato: "consegnata"})); !slices.Equal(got, []int64{consegnata}) {
		t.Errorf("contiene con lo stato: %v", got)
	}
}

// inventario: RIP-H-06
// Effettivi: con un numero d'etichetta restano la ricerca e la pagina; altrimenti tutto com'è.
func TestFiltriEffettivi(t *testing.T) {
	tutti := Filtri{Cerca: "x", Stato: "pronta", Oreficeria: 2, Artigiano: 1, Scadenza: true, CompraOro: true, Attesa: true,
		FuoriBottega: true, SenzaQR: true, Messaggi: true, Notifiche: true, DataDa: "2026-01-01", DataA: "2026-02-01",
		DataCampo: "pronta", Pagina: 3}
	if got := tutti.Effettivi(); got != tutti {
		t.Errorf("testo: %+v", got)
	}
	e := tutti
	e.Cerca = "0045/26"
	if got := e.Effettivi(); got != (Filtri{Cerca: "0045/26", DataCampo: CampoDataPredefinito, Pagina: 3}) {
		t.Errorf("etichetta: %+v", got)
	}
}

// righeQRLookup sono le righe repair.qr_lookup: risorsa, id e i dati.
func righeQRLookup(t *testing.T, b *Buste) []string {
	t.Helper()
	r, err := b.d.Lettura.Query(`SELECT risorsa, id_risorsa, esito, dati FROM eventi_audit WHERE azione = 'repair.qr_lookup' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	var out []string
	for r.Next() {
		var ris, id, e, dati string
		if err := r.Scan(&ris, &id, &e, &dati); err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(dati), &m); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprint(ris, " ", id, " ", e, " ", m["matches"], " ", m["repair_ids"], " ", len(m)))
	}
	return out
}

// inventario: RIP-H-22
// lookup_repairs_by_qr/2: il codice senza spazi ai bordi, uguale esatto col salvato (che non si
// ripulisce), dalla più recente, fuori dal cestino; repair.qr_lookup con numero e id, mai il codice,
// anche con nessuna busta se è una scansione (ancheNessuna); un testo che non trova niente senza
// scansione non scrive; il codice vuoto non legge né scrive.
func TestCercaPerQR(t *testing.T) {
	b, d, _ := prova(t)
	vecchia := bustaLista(t, b, "1/26", "RIP-2026-0045", `stato = 'consegnata'`)
	nuova := bustaLista(t, b, "2/26", "RIP-2026-0045", "")
	bustaLista(t, b, "3/26", "RIP-2026-0045", `cancellata_il = ?`, istanteProva)
	bustaLista(t, b, "4/26", "Q", `codice_qr = ' RIP-2026-0046'`)
	bustaLista(t, b, "5/26", "rip-2026-0045", "")
	ids := func(r []RigaQR) []int64 {
		var out []int64
		for _, x := range r {
			out = append(out, x.ID)
		}
		return out
	}
	r, err := b.CercaPerQR(ctx, " \tRIP-2026-0045 ", false, chi)
	if err != nil || !slices.Equal(ids(r), []int64{nuova, vecchia}) {
		t.Fatalf("%v %v", ids(r), err)
	}
	if r[0].Numero != "2/26" || r[0].Stato != "in_lavorazione" || !r[0].Aperta() || r[1].Aperta() || r[0].NomeCliente == "" {
		t.Errorf("%+v", r)
	}
	if v, ok := r[0].NumeroVisibile(); v != "0045/26" || !ok {
		t.Errorf("numero visibile %q %v", v, ok)
	}
	for _, c := range []struct {
		codice string
		anche  bool
	}{{"RIP-2026-0046", false}, {"RIP-2026-0046", true}, {"RIP-2026-004", false}, {"RIP-2026-0099", true}} {
		if r, err := b.CercaPerQR(ctx, c.codice, c.anche, chi); err != nil || len(r) != 0 {
			t.Errorf("%q: %v %v", c.codice, ids(r), err)
		}
	}
	for _, vuoto := range []string{"", "  ", "　"} {
		if _, err := b.CercaPerQR(ctx, vuoto, true, chi); !errors.Is(err, ErrQRVuoto) {
			t.Errorf("%q: %v", vuoto, err)
		}
	}
	voluti := []string{
		fmt.Sprintf("repair_index qr ok 2 [%d %d] 2", nuova, vecchia),
		"repair_index qr ok 0 [] 2",
		"repair_index qr ok 0 [] 2",
	}
	if got := righeQRLookup(t, b); !slices.Equal(got, voluti) {
		t.Errorf("righe %q, volute %q", got, voluti)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE dati LIKE '%RIP-%' OR dati LIKE '%0045%' OR dati LIKE '%0099%'`); n != 0 {
		t.Errorf("il codice nel registro: %d righe", n)
	}
	// Se la riga non si scrive, niente buste.
	esegui(t, d, `CREATE TRIGGER no_qr BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.qr_lookup' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if r, err := b.CercaPerQR(ctx, "RIP-2026-0045", false, chi); err == nil || r != nil {
		t.Errorf("senza riga: %v %v", ids(r), err)
	}
}

// inventario: RIP-H-22
// D-64 (cambia RIP-H-22): l'unica busta, o l'unica aperta fra le gemelle, si apre senza scelta; con
// più gemelle aperte o nessuna aperta si sceglie.
func TestUnicaDaAprire(t *testing.T) {
	r := func(id int64, stato string) RigaQR { return RigaQR{ID: id, Stato: stato} }
	for _, c := range []struct {
		righe  []RigaQR
		voluta int64 // 0: si sceglie
	}{
		{[]RigaQR{r(1, "consegnata")}, 1},
		{[]RigaQR{r(1, "in_lavorazione")}, 1},
		{[]RigaQR{r(2, "in_lavorazione"), r(1, "consegnata")}, 2},
		{[]RigaQR{r(3, "chiusa"), r(2, "annullata"), r(1, "pronta")}, 1},
		{[]RigaQR{r(2, "in_lavorazione"), r(1, "in_attesa_materiali")}, 0},
		{[]RigaQR{r(2, "consegnata"), r(1, "annullata")}, 0},
		{[]RigaQR{r(3, "pronta"), r(2, "consegnata"), r(1, "preventivo")}, 0},
		{nil, 0},
	} {
		x, ok := UnicaDaAprire(c.righe)
		got := int64(0)
		if ok {
			got = x.ID
		}
		if got != c.voluta {
			var s []string
			for _, y := range c.righe {
				s = append(s, y.Stato)
			}
			t.Errorf("%s: %d, voluta %d", strings.Join(s, ","), got, c.voluta)
		}
	}
}
