package buste

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/db"
)

// Lo storico delle consegne (storico.go, slice 082): HandoverHistory.build/1 e session/1,
// Workshop.delete_handover_from_history/2 e detach_bag_from_history/3 dello standalone (302439d), con le
// «riaperte» lette da giri_artigiani. riaperteDallAudit è reopened/1 riscritta in Go senza SQL in comune con
// storico.go: è il riferimento della prova di equivalenza. L'orologio è fermo al 31/10/2026, le 10:00 in
// negozio (CET, UTC+1).

// riaperteDallAudit è reopened/1: i rientri della consegna (metadata.handover_id uguale all'id per SQLite,
// quindi anche 7.0), delle buste che si leggono come intero (Integer.parse/1: anche «05»), fuori dal cestino
// e ora in un'altra consegna o in nessuna, tranne quelle sganciate a mano; dal rientro più recente.
func riaperteDallAudit(t *testing.T, d *db.DB, consegna int64) []RiapertaDelloStorico {
	t.Helper()
	rr, err := d.Lettura.Query(`SELECT id_risorsa, creato_il, dati FROM eventi_audit
		WHERE azione IN ('repair.returned_from_artisan', 'repair.returned_from_artisan_auto') AND risorsa = 'repair'
		  AND json_extract(dati, '$.handover_id') = ? ORDER BY creato_il, id`, consegna)
	if err != nil {
		t.Fatal(err)
	}
	type rientro struct {
		risorsa, creato string
		dati            map[string]any
	}
	var ee []rientro
	for rr.Next() {
		var e rientro
		var dati string
		if err := rr.Scan(&e.risorsa, &e.creato, &dati); err != nil {
			t.Fatal(err)
		}
		dec := json.NewDecoder(strings.NewReader(dati))
		dec.UseNumber()
		if err := dec.Decode(&e.dati); err != nil {
			t.Fatal(err)
		}
		ee = append(ee, e)
	}
	_ = rr.Close()
	// detached_by_hand/1: i repair_id interi delle righe di sgancio della consegna.
	sganciate := map[int64]bool{}
	rs, err := d.Lettura.Query(`SELECT dati FROM eventi_audit WHERE azione = 'handover.bag_detached' AND risorsa = 'handover' AND id_risorsa = ?`,
		strconv.FormatInt(consegna, 10))
	if err != nil {
		t.Fatal(err)
	}
	for rs.Next() {
		var dati string
		var m map[string]any
		_ = rs.Scan(&dati)
		dec := json.NewDecoder(strings.NewReader(dati))
		dec.UseNumber()
		_ = dec.Decode(&m)
		if n := interoJSON(m["repair_id"]); n != nil {
			sganciate[*n] = true
		}
	}
	_ = rs.Close()
	var out []RiapertaDelloStorico
	for _, e := range ee {
		id, err := strconv.ParseInt(e.risorsa, 10, 64) // Integer.parse/1 con il resto vuoto
		if err != nil {
			continue
		}
		var r RiapertaDelloStorico
		var adesso *int64
		err = d.Lettura.QueryRow(`SELECT id, numero, codice_qr, descrizione, consegna_artigiano_id FROM buste WHERE id = ? AND cancellata_il IS NULL`, id).
			Scan(&r.ID, &r.Numero, &r.CodiceQR, &r.Descrizione, &adesso)
		if err != nil || (adesso != nil && *adesso == consegna) || sganciate[id] {
			continue
		}
		r.Rientro, r.NotaPresente = tempo(t, &e.creato), e.dati["artisan_notes"] == true
		// outbound/3 con le conferme della consegna.
		cc, err := d.Lettura.Query(`SELECT creato_il, dati FROM eventi_audit WHERE azione = 'handover.confirmed' AND risorsa = 'handover'
			AND id_risorsa = ? ORDER BY creato_il, id`, strconv.FormatInt(consegna, 10))
		if err != nil {
			t.Fatal(err)
		}
		for cc.Next() {
			var creato, dati string
			var m map[string]any
			_ = cc.Scan(&creato, &dati)
			dec := json.NewDecoder(strings.NewReader(dati))
			dec.UseNumber()
			_ = dec.Decode(&m)
			ids, _ := m["repair_ids"].([]any)
			if slices.ContainsFunc(ids, func(v any) bool { n := interoJSON(v); return n != nil && *n == id }) && creato <= e.creato {
				r.Uscita = tempo(t, &creato)
			}
		}
		_ = cc.Close()
		out = append(out, r)
	}
	slices.SortStableFunc(out, func(a, b RiapertaDelloStorico) int { return b.Rientro.Compare(*a.Rientro) })
	return out
}

func scriviRiaperte(rr []RiapertaDelloStorico) string {
	var s strings.Builder
	p := func(t *time.Time) string {
		if t == nil {
			return "-"
		}
		return t.UTC().Format(db.FormatoIstante)
	}
	for _, r := range rr {
		fmt.Fprintf(&s, "[%d %s %s→%s nota=%v]", r.ID, r.Visibile().Titolo(), p(r.Uscita), p(r.Rientro), r.NotaPresente)
	}
	return s.String()
}

// stesseRiaperte: per ogni consegna le riaperte del dettaglio sono quelle di reopened/1 dall'audit.
func stesseRiaperte(t *testing.T, b *Buste, d *db.DB, cosa string, consegne ...int64) {
	t.Helper()
	for _, c := range consegne {
		det, err := b.DettaglioStorico(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := scriviRiaperte(det.Riaperte), scriviRiaperte(riaperteDallAudit(t, d, c)); got != want {
			t.Errorf("%s, consegna %d:\ntabella %s\naudit   %s", cosa, c, got, want)
		}
	}
}

func busteDelDettaglio(det DettaglioStorico) string {
	var s []string
	for _, x := range det.Buste {
		s = append(s, itoa(x.ID))
	}
	return strings.Join(s, ",")
}

// inventario: ART-15
// L'elenco: tutte le consegne dalla più recente (a pari apertura l'id più alto), con artigiano, buste fuori dal
// cestino, quelle ancora fuori, apertura e chiusura; i filtri artigiano, stato (aperte, chiuse) e data
// d'apertura (estremi compresi, giorni del negozio); 30 per pagina, una pagina oltre la fine è l'ultima.
func TestStoricoConsegne(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO artigiani (id, nome) VALUES (2, 'Bottega Zeta')`)
	tardi := consegnaIl(t, d, 1, "2026-10-29T22:59:59.999999Z", "")                             // il 29/10 in negozio
	presto := consegnaIl(t, d, 2, "2026-10-29T23:00:00.000000Z", "2026-10-30T10:00:00.000000Z") // il 30/10
	pari := consegnaIl(t, d, 1, "2026-10-29T23:00:00.000000Z", "")
	x, _ := bustaCon(t, b, d, "")
	y, _ := bustaCon(t, b, d, "")
	z, _ := bustaCon(t, b, d, "")
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ?, consegnata_artigiano_il = '2026-10-30T08:00:00.000000Z' WHERE id IN (?, ?)`, tardi, x, y)
	esegui(t, d, `UPDATE buste SET rientrata_artigiano_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, y)
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ?, consegnata_artigiano_il = '2026-10-30T08:00:00.000000Z', cancellata_il = ? WHERE id = ?`, tardi, istanteProva, z)
	riga := func(r RigaStorico) string {
		ch := "-"
		if r.Chiusa != nil {
			ch = r.Chiusa.UTC().Format(db.FormatoIstante)
		}
		return fmt.Sprintf("%d|%d|%s|%s|%s|%d|%d", r.ID, r.ArtigianoID, r.Artigiano, r.Aperta.UTC().Format(db.FormatoIstante), ch, r.Buste, r.Fuori)
	}
	elenco := func(f FiltriStorico) (string, Storico) {
		t.Helper()
		s, err := b.StoricoConsegne(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range s.Righe {
			out = append(out, riga(r))
		}
		return strings.Join(out, "\n"), s
	}
	got, s := elenco(FiltriStorico{Pagina: 1})
	want := itoa(pari) + "|1|Artigiano Esempio|2026-10-29T23:00:00.000000Z|-|0|0\n" +
		itoa(presto) + "|2|Bottega Zeta|2026-10-29T23:00:00.000000Z|2026-10-30T10:00:00.000000Z|0|0\n" +
		itoa(tardi) + "|1|Artigiano Esempio|2026-10-29T22:59:59.999999Z|-|2|1"
	if got != want || s.Totale != 3 || s.Pagine != 1 || s.Filtri.Pagina != 1 {
		t.Fatalf("tutte:\n%s\n%+v", got, s)
	}
	for _, c := range []struct {
		f    FiltriStorico
		want []int64
	}{
		{FiltriStorico{Stato: "aperte"}, []int64{pari, tardi}},
		{FiltriStorico{Stato: "chiuse"}, []int64{presto}},
		{FiltriStorico{Artigiano: 2}, []int64{presto}},
		{FiltriStorico{Artigiano: 9}, nil},
		{FiltriStorico{DataDa: "2026-10-30"}, []int64{pari, presto}},
		{FiltriStorico{DataA: "2026-10-29"}, []int64{tardi}},
		{FiltriStorico{DataDa: "2026-10-29", DataA: "2026-10-29"}, []int64{tardi}},
		{FiltriStorico{DataDa: "2026-10-31"}, nil},
		{FiltriStorico{DataA: "-0001-01-01"}, nil},
		{FiltriStorico{DataDa: "-0001-01-01", DataA: "9999-12-31"}, []int64{pari, presto, tardi}},
		{FiltriStorico{Artigiano: 1, Stato: "aperte", DataDa: "2026-10-30"}, []int64{pari}},
	} {
		c.f.Pagina = 1
		s, err := b.StoricoConsegne(ctx, c.f)
		if err != nil {
			t.Fatal(err)
		}
		var ids []int64
		for _, r := range s.Righe {
			ids = append(ids, r.ID)
		}
		if !slices.Equal(ids, c.want) || s.Totale != int64(len(c.want)) || s.Pagine != 1 {
			t.Errorf("%+v: %v totale %d pagine %d, attese %v", c.f, ids, s.Totale, s.Pagine, c.want)
		}
	}
	// 30 per pagina: 33 consegne sono due pagine; la 5 è la 2; la prima ha le più recenti.
	for i := range 30 {
		consegnaIl(t, d, 2, fmt.Sprintf("2026-10-%02dT08:00:00.000000Z", 1+i%28), "")
	}
	_, s = elenco(FiltriStorico{Pagina: 1})
	if len(s.Righe) != 30 || s.Totale != 33 || s.Pagine != 2 || s.Righe[0].ID != pari {
		t.Fatalf("prima pagina: %d righe, totale %d, pagine %d", len(s.Righe), s.Totale, s.Pagine)
	}
	_, s = elenco(FiltriStorico{Pagina: 5})
	if len(s.Righe) != 3 || s.Filtri.Pagina != 2 || s.Righe[2].Aperta.Day() != 1 {
		t.Fatalf("oltre la fine: %d righe, pagina %d", len(s.Righe), s.Filtri.Pagina)
	}
}

// inventario: ART-15
// I filtri come normalize/1: lo stato solo «aperte» o «chiuse» com'è scritto, l'artigiano con Id.positive/1, le
// date con Date.from_iso8601/1 dopo String.trim/1, la pagina con Id.positive/1 (altrimenti 1).
func TestLeggiFiltriStorico(t *testing.T) {
	leggi := func(q map[string]string) FiltriStorico {
		return LeggiFiltriStorico(func(n string) (string, bool) { v, ok := q[n]; return v, ok })
	}
	if f := leggi(nil); f != (FiltriStorico{Pagina: 1}) {
		t.Fatalf("vuoti: %+v", f)
	}
	f := leggi(map[string]string{"stato": "chiuse", "artigiano": "7", "data_da": " 2026-10-01\n", "data_a": "+2026-10-31", "p": "3"})
	if f != (FiltriStorico{Stato: "chiuse", Artigiano: 7, DataDa: "2026-10-01", DataA: "2026-10-31", Pagina: 3}) {
		t.Fatalf("pieni: %+v", f)
	}
	f = leggi(map[string]string{"stato": " aperte", "artigiano": "0", "data_da": "2026-02-30", "data_a": "31/10/2026", "p": "0"})
	if f != (FiltriStorico{Pagina: 1}) {
		t.Fatalf("illeggibili: %+v", f)
	}
	f = leggi(map[string]string{"stato": "tutte", "artigiano": " 7", "p": "x"})
	if f != (FiltriStorico{Pagina: 1}) {
		t.Fatalf("illeggibili 2: %+v", f)
	}
	if !slices.Equal(ChiaviRegistroStorico, []string{"artigiano", "stato", "data_da", "data_a", "p"}) {
		t.Fatal(ChiaviRegistroStorico)
	}
}

// inventario: ART-15, ART-16
// Le due letture registrate: handover.history_viewed con i soli nomi dei filtri, handover.history_detail_viewed
// sulla consegna, senza dati.
func TestRegistriDelloStorico(t *testing.T) {
	b, d, _ := prova(t)
	if err := b.RegistraStorico(ctx, nil, chi); err != nil {
		t.Fatal(err)
	}
	if got := ultimoAudit(t, d, "handover.history_viewed"); got != `handover.history_viewed|handover_history|index|ok|{"filter_keys":[]}` {
		t.Fatal(got)
	}
	if err := b.RegistraStorico(ctx, []string{"data_a", "stato"}, chi); err != nil {
		t.Fatal(err)
	}
	if got := ultimoAudit(t, d, "handover.history_viewed"); got != `handover.history_viewed|handover_history|index|ok|{"filter_keys":["data_a","stato"]}` {
		t.Fatal(got)
	}
	if err := b.RegistraDettaglioStorico(ctx, 77, chi); err != nil {
		t.Fatal(err)
	}
	if got := ultimoAudit(t, d, "handover.history_detail_viewed"); got != `handover.history_detail_viewed|handover|77|ok|{}` {
		t.Fatal(got)
	}
}

// inventario: ART-16, ART-18, ART-19
// Il dettaglio e le riaperte dal vivo: le buste della consegna fuori dal cestino per numero come testo, e le
// buste che un giro dopo le ha portate via, dalla tabella dei giri, come reopened/1 dall'audit a ogni passo;
// «Sgancia» toglie la busta dalla consegna chiusa e dalle riaperte (giri sganciati, come la ricostruzione).
// Tolto l'audit, le riaperte restano.
func TestDettaglioERiaperteDalVivo(t *testing.T) {
	b, d, o := prova(t)
	esegui(t, d, `INSERT INTO artigiani (id, nome) VALUES (2, 'Bottega Zeta')`)
	x, _ := bustaCon(t, b, d, "RIP-2026-0011")
	y, _ := bustaCon(t, b, d, "RIP-2026-0010")
	w, _ := bustaCon(t, b, d, "RIP-2026-0012")
	if _, err := b.DettaglioStorico(ctx, 999); err != ErrConsegnaNonTrovata {
		t.Fatalf("consegna che non c'è: %v", err)
	}
	prima := consegnaIl(t, d, 1, istanteProva, "")
	dentro(t, b, prima, "RIP-2026-0011")
	dentro(t, b, prima, "RIP-2026-0010")
	dentro(t, b, prima, "RIP-2026-0012")
	o.Sposta(time.Minute)
	conferma(t, b, prima)
	det, err := b.DettaglioStorico(ctx, prima)
	if err != nil || det.Consegna.Artigiano != "Artigiano Esempio" || det.Chiusa != nil || busteDelDettaglio(det) != itoa(x)+","+itoa(y)+","+itoa(w) {
		t.Fatalf("aperta: %+v %v", det, err)
	}
	o.Sposta(time.Hour)
	rientra(t, b, x, "Pietra")
	rientra(t, b, y, "")
	o.Sposta(time.Minute)
	rientra(t, b, w, "")
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, w) // nel cestino: non si vede
	stesseRiaperte(t, b, d, "rientrate", prima)
	det, _ = b.DettaglioStorico(ctx, prima)
	if det.Chiusa == nil || busteDelDettaglio(det) != itoa(x)+","+itoa(y) || len(det.Riaperte) != 0 {
		t.Fatalf("chiusa: %+v", det)
	}
	if g := det.Buste[0]; g.Note == nil || *g.Note != "Pietra" || g.Uscita == nil || g.Rientro == nil || g.Visibile().Titolo() != "#0011/26" {
		t.Fatalf("busta: %+v", g)
	}
	// La X riparte con un'altra consegna: è una riaperta della prima, con l'uscita e la nota del suo giro.
	seconda := consegnaIl(t, d, 2, istanteProva, "")
	o.Sposta(time.Hour)
	dentro(t, b, seconda, "RIP-2026-0011")
	stesseRiaperte(t, b, d, "riaperta", prima, seconda)
	det, _ = b.DettaglioStorico(ctx, prima)
	if busteDelDettaglio(det) != itoa(y) || scriviRiaperte(det.Riaperte) != "["+itoa(x)+" #0011/26 2026-10-31T09:01:00.000000Z→2026-10-31T10:01:00.000000Z nota=true]" {
		t.Fatalf("riaperte: %s", scriviRiaperte(det.Riaperte))
	}
	// La W torna dal cestino in un'altra consegna: riaperta anche lei.
	esegui(t, d, `UPDATE buste SET cancellata_il = NULL WHERE id = ?`, w)
	dentro(t, b, seconda, "RIP-2026-0012")
	stesseRiaperte(t, b, d, "dal cestino", prima, seconda)

	// Sgancia: rifiuti senza riga d'audit, poi la Y fuori dalla prima; la X sganciata non è più riaperta.
	for _, c := range []struct {
		consegna, busta int64
		err             error
	}{
		{999, y, ErrConsegnaNonTrovata},
		{seconda, x, ErrSoloChiuseSgancia},
		{prima, x, ErrBustaNonInConsegna},
		{prima, 999, ErrBustaNonInConsegna},
	} {
		if err := b.SganciaDalloStorico(ctx, c.consegna, c.busta, chi); !errors.Is(err, c.err) {
			t.Errorf("sgancia %d/%d: %v", c.consegna, c.busta, err)
		}
	}
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, y)
	if err := b.SganciaDalloStorico(ctx, prima, y, chi); !errors.Is(err, ErrBustaNonInConsegna) {
		t.Fatalf("sgancia dal cestino: %v", err)
	}
	esegui(t, d, `UPDATE buste SET cancellata_il = NULL WHERE id = ?`, y)
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'handover.bag_detached'`); n != 0 {
		t.Fatalf("righe di sgancio dei rifiuti: %d", n)
	}
	prima1 := valore(t, d, `SELECT aggiornato_il FROM buste WHERE id = ?`, y)
	if err := b.SganciaDalloStorico(ctx, prima, y, chi); err != nil {
		t.Fatal(err)
	}
	if got := ultimoAudit(t, d, "handover.bag_detached"); got != "handover.bag_detached|handover|"+itoa(prima)+`|ok|{"repair_id":`+itoa(y)+`}` {
		t.Fatal(got)
	}
	if giroDi(t, d, y) != "-|si|si|-|1" || valore(t, d, `SELECT aggiornato_il FROM buste WHERE id = ?`, y) != prima1 {
		t.Fatalf("sganciata: %s", giroDi(t, d, y))
	}
	if n := conta(t, d, `SELECT count(*) FROM giri_artigiani WHERE busta_id = ? AND sganciata = 1`, y); n != 1 {
		t.Fatalf("giri sganciati: %d", n)
	}
	stessiGiri(t, b, d, "sganciata", x, y, w)
	stesseRiaperte(t, b, d, "sganciata", prima, seconda)
	det, _ = b.DettaglioStorico(ctx, prima)
	if len(det.Buste) != 0 || len(det.Riaperte) != 2 {
		t.Fatalf("dopo lo sgancio: %+v", det)
	}
	// La Y si riapre in un'altra consegna: resta fuori dalle riaperte della prima.
	dentro(t, b, seconda, "RIP-2026-0010")
	stesseRiaperte(t, b, d, "sganciata e riaperta", prima)

	det, _ = b.DettaglioStorico(ctx, prima)
	if got := scriviRiaperte(det.Riaperte); !strings.HasPrefix(got, "["+itoa(w)+" ") || !strings.Contains(got, "]["+itoa(x)+" ") {
		t.Fatalf("dal rientro più recente: %s", got)
	}
	// La W nel cestino esce dalle riaperte.
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, w)
	stesseRiaperte(t, b, d, "riaperta nel cestino", prima)
	det, _ = b.DettaglioStorico(ctx, prima)
	if len(det.Riaperte) != 1 || det.Riaperte[0].ID != x {
		t.Fatalf("nel cestino: %s", scriviRiaperte(det.Riaperte))
	}

	// Tolto l'audit, le riaperte sono le stesse (D-14).
	prima2 := scriviRiaperte(det.Riaperte)
	esegui(t, d, `DELETE FROM eventi_audit`)
	det, _ = b.DettaglioStorico(ctx, prima)
	if scriviRiaperte(det.Riaperte) != prima2 {
		t.Fatalf("senza audit: %s\nprima: %s", scriviRiaperte(det.Riaperte), prima2)
	}
}

// inventario: ART-16, ART-19
// Le riaperte della storia inventata della 081 (righe importate, casi limite): ricostruita la tabella, sono
// quelle di reopened/1 per ogni consegna, tranne la consegna del rientro con «handover_id» 7.0 (Dubbio 2
// della ricevuta: reopened/1 lo prende, cycles/1 e la tabella no).
func TestRiaperteInventate(t *testing.T) {
	b, d, _ := prova(t)
	b1, _, b3, b4, _, _ := storiaInventata(t, b, d)
	ricostruisci(t, d)
	var consegne []int64
	rr, err := d.Lettura.Query(`SELECT id FROM consegne_artigiani ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rr.Next() {
		var id int64
		_ = rr.Scan(&id)
		consegne = append(consegne, id)
	}
	_ = rr.Close()
	k7, k9 := consegne[6], consegne[7] // la 8 è uscita dallo storico
	for _, c := range consegne {
		if c != k7 {
			stesseRiaperte(t, b, d, "storia inventata", c)
		}
	}
	det, _ := b.DettaglioStorico(ctx, k7)
	if got := scriviRiaperte(det.Riaperte); got != "["+itoa(b1)+" N. 1/26 -→2026-10-01T08:00:00.000000Z nota=false]" {
		t.Fatalf("consegna 7: %s", got)
	}
	if got := scriviRiaperte(riaperteDallAudit(t, d, k7)); !strings.HasPrefix(got, "["+itoa(b1)+" N. 1/26 -→2026-10-02T08:00:00.000000Z nota=false]") {
		t.Fatalf("consegna 7 dall'audit: %s", got)
	}
	// La 9: la b3 sganciata non c'è, la b4 (sgancio con l'id come testo) sì.
	det, _ = b.DettaglioStorico(ctx, k9)
	if len(det.Riaperte) != 1 || det.Riaperte[0].ID != b4 || det.Riaperte[0].ID == b3 {
		t.Fatalf("consegna 9: %s", scriviRiaperte(det.Riaperte))
	}
}

// inventario: ART-17, ART-22
// «Elimina dallo storico»: solo una consegna chiusa; prima sgancia le buste (anche nel cestino) e i solleciti,
// che restano con la loro busta e l'artigiano; la riga handover.deleted conta le buste vive, quelle del cestino
// e i solleciti. I giri non cambiano (la consegna resta un numero).
func TestEliminaDalloStorico(t *testing.T) {
	b, d, _ := prova(t)
	aperta := consegnaIl(t, d, 1, istanteProva, "")
	chiusa := consegnaIl(t, d, 1, istanteProva, istanteProva)
	if _, err := b.EliminaDalloStorico(ctx, 999, chi); !errors.Is(err, ErrConsegnaNonTrovata) {
		t.Fatalf("che non c'è: %v", err)
	}
	if _, err := b.EliminaDalloStorico(ctx, aperta, chi); !errors.Is(err, ErrSoloChiuseElimina) {
		t.Fatalf("aperta: %v", err)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'handover.deleted'`); n != 0 {
		t.Fatalf("righe dei rifiuti: %d", n)
	}
	x, _ := bustaCon(t, b, d, "")
	y, _ := bustaCon(t, b, d, "")
	z, _ := bustaCon(t, b, d, "")
	altra, _ := bustaCon(t, b, d, "")
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ?, artigiano_id = 1, consegnata_artigiano_il = '2026-10-20T08:00:00.000000Z',
		rientrata_artigiano_il = '2026-10-21T08:00:00.000000Z', note_artigiano = 'Nota' WHERE id IN (?, ?, ?)`, chiusa, x, y, z)
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, z)
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id = ?`, aperta, altra)
	esegui(t, d, `INSERT INTO solleciti_artigiani (busta_id, artigiano_id, consegna_artigiano_id, attore, giorni_fuori, creato_il)
		VALUES (?, 1, ?, 'banco@esempio.test', 15, ?), (?, 1, ?, 'banco@esempio.test', 3, ?)`, x, chiusa, istanteProva, altra, aperta, istanteProva)
	esegui(t, d, `INSERT INTO giri_artigiani (busta_id, consegna_artigiano_id, uscita_il, rientro_il) VALUES (?, ?, '2026-10-20T08:00:00.000000Z', '2026-10-21T08:00:00.000000Z')`, x, chiusa)
	giri := tabellaGiri(t, d.Lettura)
	aggiornata := valore(t, d, `SELECT aggiornato_il FROM buste WHERE id = ?`, x)
	n, err := b.EliminaDalloStorico(ctx, chiusa, chi)
	if err != nil || n != 2 {
		t.Fatalf("eliminata: %d %v", n, err)
	}
	if got := ultimoAudit(t, d, "handover.deleted"); got != "handover.deleted|handover|"+itoa(chiusa)+`|ok|{"bags_detached":2,"discarded_bags_detached":1,"reminders_unlinked":1}` {
		t.Fatal(got)
	}
	if conta(t, d, `SELECT count(*) FROM consegne_artigiani WHERE id = ?`, chiusa) != 0 || conta(t, d, `SELECT count(*) FROM consegne_artigiani WHERE id = ?`, aperta) != 1 {
		t.Fatal("consegne")
	}
	for _, id := range []int64{x, y, z} {
		if g := giroDi(t, d, id); g != "-|si|si|Nota|1" {
			t.Fatalf("busta %d: %s", id, g)
		}
	}
	if g := giroDi(t, d, altra); g != itoa(aperta)+"|-|-|-|-" || valore(t, d, `SELECT aggiornato_il FROM buste WHERE id = ?`, x) != aggiornata {
		t.Fatal("le altre buste e aggiornato_il non cambiano")
	}
	if got := valore(t, d, `SELECT group_concat(busta_id || ':' || coalesce(consegna_artigiano_id, '-') || ':' || artigiano_id || ':' || giorni_fuori, ' ') FROM solleciti_artigiani`); got != itoa(x)+":-:1:15 "+itoa(altra)+":"+itoa(aperta)+":1:3" {
		t.Fatalf("solleciti: %v", got)
	}
	if tabellaGiri(t, d.Lettura) != giri {
		t.Fatal("i giri sono cambiati")
	}
	if _, err := b.EliminaDalloStorico(ctx, chiusa, chi); !errors.Is(err, ErrConsegnaNonTrovata) {
		t.Fatalf("due volte: %v", err)
	}
}
