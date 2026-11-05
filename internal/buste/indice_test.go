package buste

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// La lista della home (indice.go, visibile.go), come RepairIndex dello standalone e i casi di
// repair_index_test.exs, workshop_test.exs e workshop_qr_nudge_live_test.exs, su un database vero e
// con l'orologio fermo al 31/10/2026 (aiuti_test.go).

// bustaLista crea una busta con il numero e il codice QR dati e poi scrive le colonne in più (stato,
// date, segni) direttamente: la lista legge le righe come sono, da qualunque strada arrivino.
func bustaLista(t *testing.T, b *Buste, numero, qr string, colonne string, arg ...any) int64 {
	t.Helper()
	c := crea(t, b, con(modulo(), func(a *Accettazione) { a.Numero, a.CodiceQR = numero, qr }))
	if colonne != "" {
		esegui(t, b.d, `UPDATE buste SET `+colonne+` WHERE id = ?`, append(arg, c.ID)...)
	}
	return c.ID
}

func idDi(l Lista) []int64 {
	var out []int64
	for _, r := range l.Righe {
		out = append(out, r.ID)
	}
	return out
}

func lista(t *testing.T, b *Buste, f Filtri) Lista {
	t.Helper()
	if f.Pagina == 0 {
		f.Pagina = 1
	}
	l, err := b.Lista(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// inventario: RIP-H-11, RIP-H-56
// D-64: il numero visibile è quello dell'etichetta «0045/26», con l'anno dell'etichetta; ogni altro
// codice (mancante, di un altro formato, scritto in un'altra forma) mostra «N. <numero interno>».
func TestNumeroVisibile(t *testing.T) {
	for _, c := range []struct {
		qr, numero, testo string
		etichetta         bool
	}{
		{"RIP-2026-0045", "21/26", "0045/26", true},
		{"RIP-2025-0045", "25/26", "0045/25", true},
		{"  RIP-2026-0046\t", "3/26", "0046/26", true},
		{"RIP-2026-10000", "4/26", "10000/26", true},
		{"RIP-2026-0001", "1/26", "0001/26", true},
		{"", "45/26", "N. 45/26", false},
		{"   ", "45/26", "N. 45/26", false},
		{"SINT-QR-0045", "45/26", "N. 45/26", false},
		{"RIP-2026-45", "45/26", "N. 45/26", false},
		{"RIP-2026-00045", "45/26", "N. 45/26", false},
		{"rip-2026-0045", "45/26", "N. 45/26", false},
		{"RIP-2026-0000", "45/26", "N. 45/26", false},
		{"RIP-26-0045", "45/26", "N. 45/26", false},
		{"RIP-2026-0045x", "45/26", "N. 45/26", false},
		{"RIP-2026-٠٠٤٥", "45/26", "N. 45/26", false},
	} {
		testo, etichetta := NumeroVisibile(c.qr, c.numero)
		if testo != c.testo || etichetta != c.etichetta {
			t.Errorf("%q %q: %q %v, voluto %q %v", c.qr, c.numero, testo, etichetta, c.testo, c.etichetta)
		}
	}
	s := func(v string) *string { return &v }
	for _, c := range []struct {
		stato string
		qr    *string
		segno bool
	}{
		{"in_lavorazione", nil, true}, {"pronta", s("  "), true}, {"preventivo", s(" "), true},
		{"in_valutazione", s(""), true}, {"in_lavorazione", s("X"), false},
		{"consegnata", nil, false}, {"annullata", nil, false}, {"chiusa", s(""), false},
	} {
		if g := SenzaQR(c.stato, c.qr); g != c.segno {
			t.Errorf("SenzaQR(%s, %v) = %v", c.stato, c.qr, g)
		}
	}
}

// inventario: RIP-H-12
// urgent?/3: né pronta né chiusa, con la data entro oggi più la soglia, scadute comprese; mai
// senza data (il testo libero non conta).
func TestUrgente(t *testing.T) {
	oggi := clock.Data{Anno: 2026, Mese: time.October, Giorno: 31}
	s := func(v string) *string { return &v }
	for _, c := range []struct {
		stato   string
		data    *string
		giorni  int
		urgente bool
	}{
		{"in_lavorazione", s("2026-11-07"), 7, true},
		{"in_lavorazione", s("2026-11-08"), 7, false},
		{"in_lavorazione", s("2026-11-08"), 8, true},
		{"in_lavorazione", s("2026-10-31"), 1, true},
		{"in_lavorazione", s("2020-01-01"), 1, true},
		{"preventivo", s("2026-11-01"), 7, true},
		{"in_attesa_materiali", s("2026-11-01"), 7, true},
		{"in_valutazione", s("2026-11-01"), 7, true},
		{"valutata", s("2026-11-01"), 7, true},
		{"perfezionata", s("2026-11-01"), 7, true},
		{"pronta", s("2026-10-01"), 7, false},
		{"consegnata", s("2026-10-01"), 7, false},
		{"annullata", s("2026-10-01"), 7, false},
		{"chiusa", s("2026-10-01"), 7, false},
		{"in_lavorazione", nil, 7, false},
		{"in_lavorazione", s("2027-10-31"), 365, true},
		{"in_lavorazione", s("2027-11-01"), 365, false},
	} {
		if g := Urgente(c.stato, c.data, oggi, c.giorni); g != c.urgente {
			t.Errorf("%s %v %d: %v", c.stato, c.data, c.giorni, g)
		}
	}
}

// inventario: RIP-H-08, RIP-H-02
// Operator.read/0: la soglia è Integer.parse/1 per intero, portata fra 1 e 365, altrimenti 7; un
// interruttore è spento solo per «0»; senza righe tutto acceso e 7 giorni.
func TestImpostazioniHome(t *testing.T) {
	b, d, _ := prova(t)
	i, err := b.LeggiImpostazioniHome(ctx)
	if err != nil || i != (ImpostazioniHome{7, true, true}) {
		t.Fatalf("predefinite: %+v %v", i, err)
	}
	for v, g := range map[string]int{
		"12": 12, "1": 1, "365": 365, "0": 1, "-3": 1, "400": 365, "+7": 7, "007": 7, "abc": 7, " 7": 7, "7 ": 7,
		"7abc": 7, "": 7, "--7": 7, "+-7": 7, "7.5": 7, "99999999999999999999": 365, "-99999999999999999999": 1, "٧": 7,
	} {
		esegui(t, d, `INSERT OR REPLACE INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('scadenza_giorni', ?, ?)`, v, istanteProva)
		if i, err := b.LeggiImpostazioniHome(ctx); err != nil || i.GiorniScadenza != g {
			t.Errorf("scadenza_giorni %q: %d %v, voluto %d", v, i.GiorniScadenza, err, g)
		}
	}
	for v, acceso := range map[string]bool{"0": false, "1": true, "": true, "false": true, " 0": true, "no": true} {
		esegui(t, d, `INSERT OR REPLACE INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('home_mostra_pronte', ?, ?), ('home_mostra_consegnate', ?, ?)`, v, istanteProva, v, istanteProva)
		if i, err := b.LeggiImpostazioniHome(ctx); err != nil || i.MostraPronte != acceso || i.MostraConsegnate != acceso {
			t.Errorf("interruttori %q: %+v %v", v, i, err)
		}
	}
}

// inventario: RIP-H-06, RIP-H-10
// normalize/1 per i filtri dei riquadri e la pagina: lo stato senza spazi ai bordi, una casella
// c'è se non è vuota senza spazi, una lista (testo falso) non conta; la pagina con Id.positive/1.
func TestLeggiFiltri(t *testing.T) {
	q := func(m map[string]string) func(string) (string, bool) {
		return func(n string) (string, bool) { v, ok := m[n]; return v, ok }
	}
	f := LeggiFiltri(q(map[string]string{"stato": "  pronta ", "scadenza": "1", "compro_oro": " ", "attesa": "x",
		"fuori_bottega": "0", "senza_qr": " ", "messaggi": "si", "p": "3"}))
	if f != (Filtri{Stato: "pronta", Scadenza: true, Attesa: true, FuoriBottega: true, Messaggi: true, Pagina: 3}) {
		t.Errorf("%+v", f)
	}
	if !f.Attivo() || (Filtri{Pagina: 4}).Attivo() {
		t.Error("Attivo")
	}
	for _, c := range []struct {
		da     Filtri
		attivo bool
	}{{Filtri{Stato: "x"}, true}, {Filtri{Scadenza: true}, true}, {Filtri{CompraOro: true}, true}, {Filtri{Attesa: true}, true},
		{Filtri{FuoriBottega: true}, true}, {Filtri{SenzaQR: true}, true}, {Filtri{Messaggi: true}, true}, {Filtri{}, false}} {
		if c.da.Attivo() != c.attivo {
			t.Errorf("%+v: Attivo %v", c.da, !c.attivo)
		}
	}
	for v, p := range map[string]int64{"2": 2, "+2": 2, "0": 1, "-1": 1, "x": 1, "": 1, " 2": 1, "2 ": 1, "9223372036854775807": 9223372036854775807, "9223372036854775808": 1} {
		if g := LeggiFiltri(q(map[string]string{"p": v})).Pagina; g != p {
			t.Errorf("p=%q: %d", v, g)
		}
	}
	if f := LeggiFiltri(q(nil)); f != (Filtri{Pagina: 1}) {
		t.Errorf("senza parametri: %+v", f)
	}
}

// inventario: RIP-H-09
// order_rows/1: fissate, poi con un messaggio non letto, poi anno e progressivo letti come numeri
// (10000/25 prima di 9999/25), poi l'id; letto il messaggio la busta torna al suo posto.
func TestListaOrdine(t *testing.T) {
	b, d, _ := prova(t)
	a := bustaLista(t, b, "9999/25", "Q1", "")
	bb := bustaLista(t, b, "10000/25", "Q2", "")
	c := bustaLista(t, b, "2/26", "Q3", "")
	e := bustaLista(t, b, "10/26", "Q4", "")
	f := bustaLista(t, b, "3/26", "Q5", `fissata_il = ?`, istanteProva)
	g := bustaLista(t, b, "1/26", "Q6", `ultimo_messaggio_il = ?`, istanteProva)
	h := bustaLista(t, b, "5/24", "Q7", `ultimo_messaggio_il = ?, messaggi_letti_il = ?`, istanteProva, istanteProva)
	voluti := []int64{f, g, e, c, bb, a, h}
	if got := idDi(lista(t, b, Filtri{})); !slices.Equal(got, voluti) {
		t.Fatalf("ordine %v, voluto %v", got, voluti)
	}
	// Una fissata con un messaggio non letto resta fra le fissate; due fissate vanno per numero.
	esegui(t, d, `UPDATE buste SET fissata_il = ? WHERE id = ?`, istanteProva, h)
	if got := idDi(lista(t, b, Filtri{})); !slices.Equal(got, []int64{f, h, g, e, c, bb, a}) {
		t.Fatalf("con due fissate %v", got)
	}
	// Letto il messaggio (il segno uguale all'ultimo), la 1/26 torna in fondo alle 2026.
	esegui(t, d, `UPDATE buste SET messaggi_letti_il = ultimo_messaggio_il WHERE id = ?`, g)
	if got := idDi(lista(t, b, Filtri{})); !slices.Equal(got, []int64{f, h, e, c, g, bb, a}) {
		t.Fatalf("dopo la lettura %v", got)
	}
	// Un segno di lettura diverso (anche più nuovo) è ancora «non letto», come DateTime.compare/2 != :eq.
	esegui(t, d, `UPDATE buste SET messaggi_letti_il = '2026-10-31T10:00:00.000000Z' WHERE id = ?`, g)
	if got := idDi(lista(t, b, Filtri{})); !slices.Equal(got, []int64{f, h, g, e, c, bb, a}) {
		t.Fatalf("segno diverso %v", got)
	}
}

// inventario: RIP-H-09
// A pari numero (non succede per l'indice unico, ma l'ordine lo prevede) decide l'id: qui due anni
// diversi con lo stesso progressivo vanno per anno, e lo stesso anno per progressivo.
func TestListaOrdinePerAnno(t *testing.T) {
	b, _, _ := prova(t)
	x := bustaLista(t, b, "7/25", "Q1", "")
	y := bustaLista(t, b, "7/27", "Q2", "")
	z := bustaLista(t, b, "8/26", "Q3", "")
	if got := idDi(lista(t, b, Filtri{})); !slices.Equal(got, []int64{y, z, x}) {
		t.Fatalf("%v", got)
	}
}

// inventario: RIP-H-02, RIP-H-56
// counters/2: ognuno con la sua regola, senza guardare filtri e impostazioni; il cestino non conta.
func TestListaContatori(t *testing.T) {
	b, d, _ := prova(t)
	bustaLista(t, b, "1/26", "Q", `stato = 'pronta', data_prevista = '2026-11-01'`)
	bustaLista(t, b, "2/26", "", `stato = 'pronta'`)
	bustaLista(t, b, "3/26", "Q", `data_prevista = '2026-10-31'`)
	bustaLista(t, b, "4/26", "Q", `data_prevista = '2026-11-07'`)
	bustaLista(t, b, "5/26", "Q", `data_prevista = '2026-11-08'`)
	bustaLista(t, b, "6/26", "Q", `data_prevista = '2026-10-30'`)
	bustaLista(t, b, "7/26", "Q", `stato = 'preventivo'`)
	bustaLista(t, b, "8/26", "  ", `stato = 'in_attesa_materiali', attesa_cliente = 1`)
	bustaLista(t, b, "9/26", "", `stato = 'consegnata', data_consegna = '2026-10-31', attesa_cliente = 1, data_prevista = '2026-11-01'`)
	bustaLista(t, b, "10/26", "Q", `stato = 'consegnata', data_consegna = '2026-10-30'`)
	bustaLista(t, b, "11/26", "Q", `stato = 'annullata', ultimo_messaggio_il = ?`, istanteProva)
	bustaLista(t, b, "12/26", "Q", `compro_oro = 1, stato = 'chiusa', ultimo_messaggio_il = ?`, istanteProva)
	bustaLista(t, b, "13/26", "Q", `compro_oro = 1, stato = 'in_valutazione', consegnata_artigiano_il = ?`, istanteProva)
	bustaLista(t, b, "14/26", "Q", `consegnata_artigiano_il = ?, rientrata_artigiano_il = ?`, istanteProva, istanteProva)
	bustaLista(t, b, "15/26", "Q", `stato = 'consegnata', consegnata_artigiano_il = ?, ultimo_messaggio_il = ?`, istanteProva, istanteProva)
	bustaLista(t, b, "16/26", "Q", `ultimo_messaggio_il = ?, messaggi_letti_il = ?`, istanteProva, istanteProva)
	bustaLista(t, b, "17/26", "Q", `ultimo_messaggio_il = ?`, istanteProva)
	// Un QR di soli spazi scritto direttamente (non dal modulo) è un QR mancante; una consegnata
	// con la data di consegna nel futuro non è «consegnata oggi».
	bustaLista(t, b, "19/26", "Q", `codice_qr = '   '`)
	bustaLista(t, b, "20/26", "Q", `stato = 'consegnata', data_consegna = '2026-11-01'`)
	// Nel cestino: non conta per niente.
	bustaLista(t, b, "18/26", "", `stato = 'pronta', attesa_cliente = 1, ultimo_messaggio_il = ?, cancellata_il = ?`, istanteProva, istanteProva)
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('scadenza_giorni', '7', ?), ('home_mostra_pronte', '0', ?), ('home_mostra_consegnate', '0', ?)`, istanteProva, istanteProva, istanteProva)
	voluti := Contatori{Pronte: 2, Lavorazione: 8, Preventivo: 1, AttesaMateriali: 1, ConsegnateOggi: 1, Scadenza: 3,
		CompraOro: 2, AttesaCliente: 1, FuoriBottega: 2, SenzaQR: 3, Messaggi: 2}
	for _, f := range []Filtri{{}, {Stato: "pronta"}, {Scadenza: true}, {Messaggi: true, Pagina: 9}} {
		if l := lista(t, b, f); l.Contatori != voluti {
			t.Errorf("%+v: %+v, voluti %+v", f, l.Contatori, voluti)
		}
	}
	// La soglia cambia il contatore della scadenza: 1 giorno prende solo oggi e domani.
	esegui(t, d, `UPDATE impostazioni SET valore = '1' WHERE chiave = 'scadenza_giorni'`)
	if l := lista(t, b, Filtri{}); l.Contatori.Scadenza != 2 || l.GiorniScadenza != 1 {
		t.Errorf("soglia 1: %d %d", l.Contatori.Scadenza, l.GiorniScadenza)
	}
}

// inventario: RIP-H-06, RIP-H-12
// I filtri dei riquadri: compro oro, attesa, fuori bottega e messaggi non escludono le chiuse;
// senza_qr e scadenza sì; i filtri si combinano; il cestino non c'è mai. Le urgenti sono segnate
// con la soglia letta.
func TestListaFiltri(t *testing.T) {
	b, d, _ := prova(t)
	pronta := bustaLista(t, b, "1/26", "Q", `stato = 'pronta', data_prevista = '2026-11-01'`)
	senza := bustaLista(t, b, "2/26", "", ``)
	oggi := bustaLista(t, b, "3/26", "Q", `data_prevista = '2026-10-31'`)
	oltre := bustaLista(t, b, "4/26", "Q", `data_prevista = '2026-11-08'`)
	scaduta := bustaLista(t, b, "5/26", "Q", `data_prevista = '2026-10-30'`)
	attesaChiusa := bustaLista(t, b, "6/26", "", `stato = 'consegnata', attesa_cliente = 1, data_prevista = '2026-11-01'`)
	oroChiusa := bustaLista(t, b, "7/26", "Q", `compro_oro = 1, stato = 'chiusa', ultimo_messaggio_il = ?, messaggi_letti_il = ?`, istanteProva, istanteProva)
	fuoriChiusa := bustaLista(t, b, "8/26", "Q", `stato = 'annullata', consegnata_artigiano_il = ?`, istanteProva)
	rientrata := bustaLista(t, b, "9/26", "Q", `consegnata_artigiano_il = ?, rientrata_artigiano_il = ?`, istanteProva, istanteProva)
	bustaLista(t, b, "10/26", "", `stato = 'pronta', attesa_cliente = 1, compro_oro = 0, ultimo_messaggio_il = ?, cancellata_il = ?`, istanteProva, istanteProva)
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('scadenza_giorni', '7', ?)`, istanteProva)
	for _, c := range []struct {
		f      Filtri
		voluti []int64
	}{
		{Filtri{}, []int64{rientrata, fuoriChiusa, oroChiusa, attesaChiusa, scaduta, oltre, oggi, senza, pronta}},
		{Filtri{Stato: "pronta"}, []int64{pronta}},
		{Filtri{Stato: "boh"}, nil},
		{Filtri{Scadenza: true}, []int64{oggi, pronta}},
		{Filtri{CompraOro: true}, []int64{oroChiusa}},
		{Filtri{Attesa: true}, []int64{attesaChiusa}},
		{Filtri{FuoriBottega: true}, []int64{fuoriChiusa}},
		{Filtri{SenzaQR: true}, []int64{senza}},
		{Filtri{Messaggi: true}, []int64{oroChiusa}},
		{Filtri{Scadenza: true, Stato: "pronta"}, []int64{pronta}},
		{Filtri{Scadenza: true, Stato: "in_lavorazione"}, []int64{oggi}},
		{Filtri{Attesa: true, SenzaQR: true}, nil},
	} {
		l := lista(t, b, c.f)
		if got := idDi(l); !slices.Equal(got, c.voluti) || l.Totale != int64(len(c.voluti)) {
			t.Errorf("%+v: %v (%d), voluti %v", c.f, got, l.Totale, c.voluti)
		}
	}
	urgenti := map[int64]bool{}
	for _, r := range lista(t, b, Filtri{}).Righe {
		if r.Urgente {
			urgenti[r.ID] = true
		}
	}
	if len(urgenti) != 2 || !urgenti[oggi] || !urgenti[scaduta] {
		t.Errorf("urgenti %v", urgenti)
	}
}

// inventario: RIP-H-08
// hide_defaults/2: senza filtri le pronte e/o le consegnate spariscono se l'interruttore è spento;
// qualsiasi filtro (anche quello dello stato nascosto, o un altro riquadro) le rimostra; la pagina
// da sola no.
func TestListaNascondePronteEConsegnate(t *testing.T) {
	b, d, _ := prova(t)
	p := bustaLista(t, b, "1/26", "Q", `stato = 'pronta'`)
	c := bustaLista(t, b, "2/26", "Q", `stato = 'consegnata', compro_oro = 0`)
	l := bustaLista(t, b, "3/26", "Q", ``)
	o := bustaLista(t, b, "4/26", "Q", `compro_oro = 1, stato = 'valutata'`)
	tutte := []int64{o, l, c, p}
	if got := idDi(lista(t, b, Filtri{})); !slices.Equal(got, tutte) {
		t.Fatalf("predefinito %v", got)
	}
	metti := func(pronte, consegnate string) {
		esegui(t, d, `INSERT OR REPLACE INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('home_mostra_pronte', ?, ?), ('home_mostra_consegnate', ?, ?)`,
			pronte, istanteProva, consegnate, istanteProva)
	}
	metti("0", "1")
	if got := idDi(lista(t, b, Filtri{Pagina: 1})); !slices.Equal(got, []int64{o, l, c}) {
		t.Errorf("senza pronte %v", got)
	}
	metti("1", "0")
	if got := idDi(lista(t, b, Filtri{})); !slices.Equal(got, []int64{o, l, p}) {
		t.Errorf("senza consegnate %v", got)
	}
	metti("0", "0")
	if got := idDi(lista(t, b, Filtri{})); !slices.Equal(got, []int64{o, l}) {
		t.Errorf("senza tutte e due %v", got)
	}
	// La pagina da sola non è un filtro: con p=2 (portata all'ultima, la 1) restano nascoste.
	if l2 := lista(t, b, Filtri{Pagina: 2}); !slices.Equal(idDi(l2), []int64{o, l}) || l2.Totale != 2 || l2.Filtri.Pagina != 1 {
		t.Errorf("p=2: %v totale %d pagina %d", idDi(l2), l2.Totale, l2.Filtri.Pagina)
	}
	if got := idDi(lista(t, b, Filtri{Stato: "pronta"})); !slices.Equal(got, []int64{p}) {
		t.Errorf("riquadro delle pronte %v", got)
	}
	if got := idDi(lista(t, b, Filtri{Stato: "consegnata"})); !slices.Equal(got, []int64{c}) {
		t.Errorf("riquadro delle consegnate %v", got)
	}
	esegui(t, d, `UPDATE buste SET ultimo_messaggio_il = ? WHERE id IN (?, ?)`, istanteProva, p, c)
	if got := idDi(lista(t, b, Filtri{Messaggi: true})); !slices.Equal(got, []int64{c, p}) {
		t.Errorf("un altro filtro %v", got)
	}
	if l := lista(t, b, Filtri{}); l.Contatori.Pronte != 1 || l.Totale != 2 {
		t.Errorf("i contatori non nascondono: %+v", l.Contatori)
	}
}

// inventario: RIP-H-10
// 30 per pagina; una pagina oltre l'ultima è l'ultima; il totale conta tutte le pagine; senza
// buste una pagina.
func TestListaPaginazione(t *testing.T) {
	b, _, _ := prova(t)
	if l := lista(t, b, Filtri{Pagina: 5}); l.Pagine != 1 || l.Filtri.Pagina != 1 || l.Totale != 0 || len(l.Righe) != 0 {
		t.Fatalf("vuota: %+v", l)
	}
	var ids []int64
	for i := 1; i <= 61; i++ {
		ids = append(ids, bustaLista(t, b, fmt.Sprintf("%d/26", i), "Q", ""))
	}
	slices.Reverse(ids)
	for _, c := range []struct {
		p, pagina int64
		righe     []int64
	}{{1, 1, ids[:30]}, {2, 2, ids[30:60]}, {3, 3, ids[60:]}, {4, 3, ids[60:]}, {9223372036854775807, 3, ids[60:]}} {
		l := lista(t, b, Filtri{Pagina: c.p})
		if l.Pagine != 3 || l.Totale != 61 || l.Filtri.Pagina != c.pagina || !slices.Equal(idDi(l), c.righe) {
			t.Errorf("p=%d: pagina %d di %d, %d buste, righe %v", c.p, l.Filtri.Pagina, l.Pagine, l.Totale, idDi(l))
		}
	}
	// Esattamente 30: una pagina sola.
	b2, _, _ := prova(t)
	for i := 1; i <= 30; i++ {
		bustaLista(t, b2, fmt.Sprintf("%d/26", i), "Q", "")
	}
	if l := lista(t, b2, Filtri{Pagina: 2}); l.Pagine != 1 || l.Filtri.Pagina != 1 || len(l.Righe) != 30 {
		t.Errorf("30 buste: %d pagine, pagina %d, %d righe", l.Pagine, l.Filtri.Pagina, len(l.Righe))
	}
}

// inventario: RIP-H-11
// La riga ha quello che il riquadro mostra: oreficeria (anche il negozio), consegna a testo o a
// data, attesa, segni dei messaggi e delle risposte al preventivo; il nome del negozio.
func TestListaRiga(t *testing.T) {
	b, d, _ := prova(t)
	id := bustaLista(t, b, "1/26", "RIP-2026-0045", `oreficeria_id = 1, data_prevista_testo = 'fine mese', data_prevista = '2026-11-01',
		attesa_cliente = 1, attesa_dal = '2026-10-29 08:00:00', attesa_motivo = 'Quale pietra?', ultimo_messaggio_il = ?,
		preventivo_accettato_il = ?, preventivo_rifiutato_il = ?, preventivo_rifiutato_letto_il = ?`, istanteProva, istanteProva, istanteProva, istanteProva)
	esegui(t, d, `INSERT INTO profilo_negozio (nome, condizioni_garanzia, creato_il, aggiornato_il) VALUES ('Bottega Prova', 'x', ?, ?)`, istanteProva, istanteProva)
	l := lista(t, b, Filtri{})
	if len(l.Righe) != 1 || l.NomeNegozio != "Bottega Prova" {
		t.Fatalf("%+v", l)
	}
	r := l.Righe[0]
	if r.ID != id || r.Oreficeria == nil || *r.Oreficeria != "Il negozio di prova" || *r.Consegna.Testo != "fine mese" ||
		!r.AttesaCliente || *r.AttesaMotivo != "Quale pietra?" || !r.Conversazione() || !r.MessaggioNonLetto() ||
		!r.AccettazioneNonLetta() || r.RifiutoNonLetto() || r.SenzaQR() || r.Chiusa() || !r.Urgente {
		t.Errorf("%+v", r)
	}
	if n, e := r.NumeroVisibile(); n != "0045/26" || !e {
		t.Errorf("numero %q %v", n, e)
	}
}

// inventario: RIP-H-11
// waiting_days/2: giorni interi dall'istante locale (o dalla mezzanotte del giorno), almeno 0;
// un testo che non si legge non dà niente.
func TestGiorniAttesa(t *testing.T) {
	adesso := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC) // le 10:00 in negozio
	for _, c := range []struct {
		dal string
		n   int64
		ok  bool
	}{
		{"2026-10-29 10:00:00", 2, true}, {"2026-10-29 10:00:01", 1, true}, {"2026-10-31 10:00:00", 0, true},
		{"2026-11-02 10:00:00", 0, true}, {"2026-10-29", 2, true}, {"  2026-10-29  ", 2, true},
		{"2026-10-29T08:00:00", 2, true}, {"2026-10-29 08:00", 2, true}, {"2026-10-29 8:00:00", 2, true},
		{"2026-10-29 10:00:00.123456", 2, true}, {"2026-10-29 10:00:01.5", 1, true}, {"2026-02-30", 0, false}, {"ieri", 0, false}, {"", 0, false},
		{"2026-10-29 25:00:00", 2, true},
	} {
		n, ok := GiorniAttesa(c.dal, adesso)
		if n != c.n || ok != c.ok {
			t.Errorf("%q: %d %v, voluto %d %v", c.dal, n, ok, c.n, c.ok)
		}
	}
}

// inventario: RIP-H-21
// record_repair_index_view/2: una riga repair.index_viewed con i soli nomi dei filtri, anche
// nessuno.
func TestRegistraLista(t *testing.T) {
	b, d, _ := prova(t)
	if err := b.RegistraLista(ctx, nil, chi); err != nil {
		t.Fatal(err)
	}
	if err := b.RegistraLista(ctx, []string{"p", "stato"}, chi); err != nil {
		t.Fatal(err)
	}
	var righe []string
	r, err := d.Lettura.Query(`SELECT attore, risorsa, id_risorsa, esito, dati FROM eventi_audit WHERE azione = 'repair.index_viewed' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	for r.Next() {
		var a, ris, id, e, dati string
		if err := r.Scan(&a, &ris, &id, &e, &dati); err != nil {
			t.Fatal(err)
		}
		var m map[string][]string
		if err := json.Unmarshal([]byte(dati), &m); err != nil {
			t.Fatal(err)
		}
		righe = append(righe, fmt.Sprint(a, ris, id, e, m["filter_keys"], len(m)))
	}
	voluti := []string{"titolare@esempio.testrepair_indexindexok[] 1", "titolare@esempio.testrepair_indexindexok[p stato] 1"}
	if !slices.Equal(righe, voluti) {
		t.Errorf("%q", righe)
	}
}
