package buste

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/db"
)

// I giri dall'artigiano (slice 081, ART-19): la tabella giri_artigiani contro la ricostruzione dall'audit
// dello standalone. cicliDallAudit è HandoverHistory.cycles/1 (handover_history.ex, 302439d) riscritta in
// Go, riga per riga e senza SQL in comune con giri.go: è il riferimento della prova di equivalenza.

// cicliDallAudit legge eventi_audit, buste, consegne e artigiani come cycles/1.
func cicliDallAudit(t *testing.T, d *db.DB, busta int64) []Giro {
	t.Helper()
	var consegna *int64
	var uscita, rientro, note *string
	if err := d.Lettura.QueryRow(`SELECT consegna_artigiano_id, consegnata_artigiano_il, rientrata_artigiano_il, note_artigiano
		FROM buste WHERE id = ?`, busta).Scan(&consegna, &uscita, &rientro, &note); err != nil {
		t.Fatal(err)
	}
	type evento struct {
		creato string
		dati   map[string]any
	}
	leggi := func(q string, args ...any) []evento {
		rr, err := d.Lettura.Query(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rr.Close()
		var out []evento
		for rr.Next() {
			var e evento
			var dati string
			if err := rr.Scan(&e.creato, &dati); err != nil {
				t.Fatal(err)
			}
			dec := json.NewDecoder(strings.NewReader(dati))
			dec.UseNumber()
			if err := dec.Decode(&e.dati); err != nil {
				t.Fatal(err)
			}
			out = append(out, e)
		}
		return out
	}
	// past_events/1: i rientri della busta (resource_id è l'id scritto com'è) prima dell'uscita delle colonne.
	var passati []evento
	for _, e := range leggi(`SELECT creato_il, dati FROM eventi_audit WHERE azione IN ('repair.returned_from_artisan',
		'repair.returned_from_artisan_auto') AND risorsa = 'repair' AND id_risorsa = ? ORDER BY creato_il, id`, strconv.FormatInt(busta, 10)) {
		if uscita == nil || e.creato < *uscita {
			passati = append(passati, e)
		}
	}
	nomeDi := func(c *int64) *string {
		if c == nil {
			return nil
		}
		var n *string
		_ = d.Lettura.QueryRow(`SELECT a.nome FROM consegne_artigiani c JOIN artigiani a ON a.id = c.artigiano_id WHERE c.id = ?`, *c).Scan(&n)
		return n
	}
	var giri []Giro
	if uscita != nil {
		giri = append(giri, Giro{Consegna: consegna, Artigiano: nomeDi(consegna), Uscita: tempo(t, uscita), Rientro: tempo(t, rientro),
			Note: note, NotaPresente: note != nil, Attuale: true})
	}
	var vecchi []Giro
	for _, e := range passati {
		c := interoJSON(e.dati["handover_id"])
		g := Giro{Consegna: c, Artigiano: nomeDi(c), Rientro: tempo(t, &e.creato), NotaPresente: e.dati["artisan_notes"] == true}
		if c != nil {
			// outbound/3: l'ultima conferma della stessa consegna che nomina la busta e non è dopo il rientro.
			for _, conf := range leggi(`SELECT creato_il, dati FROM eventi_audit WHERE azione = 'handover.confirmed' AND risorsa = 'handover'
				AND id_risorsa = ? ORDER BY creato_il, id`, strconv.FormatInt(*c, 10)) {
				ids, _ := conf.dati["repair_ids"].([]any)
				nomina := slices.ContainsFunc(ids, func(v any) bool { n := interoJSON(v); return n != nil && *n == busta })
				if nomina && conf.creato <= e.creato {
					g.Uscita = tempo(t, &conf.creato)
				}
			}
		}
		vecchi = append(vecchi, g)
	}
	// newest_first/1: Enum.sort_by è stabile, a parità di rientro resta l'ordine dell'audit.
	slices.SortStableFunc(vecchi, func(a, b Giro) int { return b.Rientro.Compare(*a.Rientro) })
	return append(giri, vecchi...)
}

// interoJSON è is_integer/1 su un valore di Jason: un numero senza parte decimale né esponente.
func interoJSON(v any) *int64 {
	n, ok := v.(json.Number)
	if !ok || strings.ContainsAny(string(n), ".eE") {
		return nil
	}
	i, err := n.Int64()
	if err != nil {
		return nil
	}
	return &i
}

func tempo(t *testing.T, s *string) *time.Time {
	t.Helper()
	v, err := istante(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func scriviGiri(gg []Giro) string {
	var b strings.Builder
	p := func(v any) string {
		switch x := v.(type) {
		case *int64:
			if x != nil {
				return itoa(*x)
			}
		case *string:
			if x != nil {
				return *x
			}
		case *time.Time:
			if x != nil {
				return x.UTC().Format(db.FormatoIstante)
			}
		}
		return "-"
	}
	for _, g := range gg {
		fmt.Fprintf(&b, "[%s %s %s→%s nota=%s/%v attuale=%v]", p(g.Consegna), p(g.Artigiano), p(g.Uscita), p(g.Rientro), p(g.Note),
			g.NotaPresente, g.Attuale)
	}
	return b.String()
}

// tabellaGiri è giri_artigiani letta da q (un collegamento o una transazione), senza gli id, in righe ordinate:
// dal vivo e nella ricostruzione le righe sono le stesse, non nello stesso ordine di scrittura.
func tabellaGiri(t *testing.T, q interrogatore) string {
	t.Helper()
	rr, err := q.QueryContext(ctx, `SELECT busta_id || '|' || coalesce(consegna_artigiano_id, '-') || '|' || coalesce(uscita_il, '-') || '|' ||
		coalesce(rientro_il, '-') || '|' || nota_presente || '|' || sganciata FROM giri_artigiani`)
	if err != nil {
		t.Fatal(err)
	}
	defer rr.Close()
	var out []string
	for rr.Next() {
		var s string
		if err := rr.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	slices.Sort(out)
	return strings.Join(out, "\n")
}

const fileMigrazioneGiri = "../db/migrazioni/0019_giri_artigiani.sql"

// ricostruite sono la tabella rifatta da RicostruisciGiri e quella rifatta dal file della migrazione 0019
// eseguito di nuovo, ognuna in una transazione che si annulla.
func ricostruite(t *testing.T, d *db.DB) (daRicostruisci, daMigrazione string) {
	t.Helper()
	migrazione, err := os.ReadFile(fileMigrazioneGiri)
	if err != nil {
		t.Fatal(err)
	}
	for i, rifai := range []func(tx *sql.Tx) error{
		func(tx *sql.Tx) error { return RicostruisciGiri(ctx, tx) },
		func(tx *sql.Tx) error { _, err := tx.ExecContext(ctx, string(migrazione)); return err },
	} {
		tx, err := d.Scrittura.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if err := rifai(tx); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		s := tabellaGiri(t, tx)
		_ = tx.Rollback()
		if i == 0 {
			daRicostruisci = s
		} else {
			daMigrazione = s
		}
	}
	return daRicostruisci, daMigrazione
}

// stessiGiri è la prova di equivalenza a un punto della storia: per ogni busta i giri letti dalla tabella sono
// quelli di cycles/1 dall'audit, e la tabella scritta dal vivo è quella che la ricostruzione e la migrazione
// rifarebbero adesso dall'audit.
func stessiGiri(t *testing.T, b *Buste, d *db.DB, cosa string, buste ...int64) {
	t.Helper()
	for _, id := range buste {
		gg, err := b.GiriDellArtigiano(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := scriviGiri(gg), scriviGiri(cicliDallAudit(t, d, id)); got != want {
			t.Errorf("%s, busta %d:\ntabella %s\naudit   %s", cosa, id, got, want)
		}
	}
	vivo := tabellaGiri(t, d.Lettura)
	r, m := ricostruite(t, d)
	if vivo != r || vivo != m {
		t.Errorf("%s: tabella dal vivo\n%s\nricostruita\n%s\nmigrazione\n%s", cosa, vivo, r, m)
	}
}

func conferma(t *testing.T, b *Buste, consegna int64) {
	t.Helper()
	if _, err := b.ConfermaConsegna(ctx, consegna, chi); err != nil {
		t.Fatal(err)
	}
}

func dentro(t *testing.T, b *Buste, consegna int64, codice string) {
	t.Helper()
	if _, err := aggiungi(b, consegna, codice); err != nil {
		t.Fatal(err)
	}
}

func rientra(t *testing.T, b *Buste, id int64, nota string) {
	t.Helper()
	if _, err := b.RientraDallArtigiano(ctx, id, "", nota, chi); err != nil {
		t.Fatal(err)
	}
}

// inventario: ART-19
// Dal vivo: conferma (anche quella della stampa, 078), rientro con e senza nota (080), rientro automatico dal QR
// (054) e la riapertura di una busta rientrata in un'altra consegna o nella stessa (077). A ogni passo i giri
// della tabella sono quelli che cycles/1 ricostruisce dall'audit, e la tabella è quella della ricostruzione.
func TestGiriDalVivo(t *testing.T) {
	b, d, o := prova(t)
	esegui(t, d, `INSERT INTO artigiani (id, nome) VALUES (2, 'Bottega Zeta')`)
	x, _ := bustaCon(t, b, d, "RIP-2026-0001")
	y, _ := bustaCon(t, b, d, "RIP-2026-0002")
	z, _ := bustaCon(t, b, d, "RIP-2026-0003")
	tutte := []int64{x, y, z}
	stessiGiri(t, b, d, "mai uscite", tutte...)
	if gg, err := b.GiriDellArtigiano(ctx, x); err != nil || len(gg) != 0 {
		t.Fatalf("una busta mai uscita non ha giri: %+v %v", gg, err)
	}
	if _, err := b.GiriDellArtigiano(ctx, 999); err != ErrBustaNonTrovata {
		t.Fatalf("busta che non c'è: %v", err)
	}

	prima := consegnaIl(t, d, 1, istanteProva, "")
	dentro(t, b, prima, "RIP-2026-0001")
	dentro(t, b, prima, "RIP-2026-0002")
	o.Sposta(time.Minute)
	conferma(t, b, prima)
	stessiGiri(t, b, d, "uscite", tutte...)
	if got := tabellaGiri(t, d.Lettura); got != itoa(x)+"|"+itoa(prima)+"|2026-10-31T09:01:00.000000Z|-|0|0\n"+
		itoa(y)+"|"+itoa(prima)+"|2026-10-31T09:01:00.000000Z|-|0|0" {
		t.Fatalf("giri aperti della conferma:\n%s", got)
	}
	o.Sposta(time.Hour)
	rientra(t, b, x, "Pietra da cambiare")
	stessiGiri(t, b, d, "rientro con nota", tutte...)
	if got := tabellaGiri(t, d.Lettura); !strings.HasPrefix(got, itoa(x)+"|"+itoa(prima)+"|2026-10-31T09:01:00.000000Z|2026-10-31T10:01:00.000000Z|1|0\n") ||
		strings.Count(got, "\n") != 1 {
		t.Fatalf("il rientro chiude il giro aperto:\n%s", got)
	}
	gg, _ := b.GiriDellArtigiano(ctx, x)
	if len(gg) != 1 || !gg[0].Attuale || gg[0].Note == nil || *gg[0].Note != "Pietra da cambiare" || *gg[0].Artigiano != "Artigiano Esempio" {
		t.Fatalf("il giro attuale resta quello delle colonne: %s", scriviGiri(gg))
	}

	// La X riparte con un altro artigiano: il giro di prima diventa passato, con la nota presente e senza testo.
	seconda := consegnaIl(t, d, 2, istanteProva, "")
	o.Sposta(time.Hour)
	dentro(t, b, seconda, "RIP-2026-0001")
	stessiGiri(t, b, d, "riaperta, non ancora uscita", tutte...)
	dentro(t, b, seconda, "RIP-2026-0003")
	o.Sposta(time.Minute)
	if _, err := b.StampaConsegnaAccettata(ctx, seconda, []int64{x, z}, "coda", "lavoro-1", chi); err != nil {
		t.Fatal(err)
	}
	stessiGiri(t, b, d, "uscita con la stampa", tutte...)
	gg, _ = b.GiriDellArtigiano(ctx, x)
	if len(gg) != 2 || !gg[0].Attuale || gg[1].Attuale || gg[1].Note != nil || !gg[1].NotaPresente || *gg[1].Consegna != prima {
		t.Fatalf("giro passato: %s", scriviGiri(gg))
	}

	// Rientro automatico dal QR (la X diventa pronta), senza nota nella riga d'audit.
	o.Sposta(time.Hour)
	if c, err := b.AvanzaDaQR(ctx, x, "in_lavorazione", chi); err != nil || !c.Rientrata {
		t.Fatalf("rientro automatico: %+v %v", c, err)
	}
	stessiGiri(t, b, d, "rientro automatico", tutte...)

	// La Y torna, la prima consegna si chiude; la Z torna senza nota, la seconda si chiude.
	o.Sposta(time.Minute)
	rientra(t, b, y, "")
	rientra(t, b, z, "  ")
	stessiGiri(t, b, d, "consegne chiuse", tutte...)

	// La Y riparte con una terza consegna del primo artigiano e ci torna due volte: rientrata, si riaggiunge alla
	// stessa consegna, ancora aperta per la Z (riaperta pulita, «già in lista»), e si conferma di nuovo.
	terza := consegnaIl(t, d, 1, istanteProva, "")
	o.Sposta(time.Minute)
	dentro(t, b, terza, "RIP-2026-0002")
	dentro(t, b, terza, "RIP-2026-0003")
	conferma(t, b, terza)
	o.Sposta(time.Minute)
	rientra(t, b, y, "Da lucidare")
	stessiGiri(t, b, d, "prima volta nella terza", tutte...)
	o.Sposta(time.Minute)
	if a, err := aggiungi(b, terza, "RIP-2026-0002"); err != nil || !a.GiaInLista {
		t.Fatalf("stessa consegna: %+v %v", a, err)
	}
	stessiGiri(t, b, d, "riaperta nella stessa consegna", tutte...)
	o.Sposta(time.Minute)
	conferma(t, b, terza)
	o.Sposta(time.Minute)
	rientra(t, b, y, "")
	stessiGiri(t, b, d, "seconda volta nella terza", tutte...)
	gg, _ = b.GiriDellArtigiano(ctx, y)
	if got := scriviGiri(gg); got != "["+itoa(terza)+" Artigiano Esempio 2026-10-31T12:07:00.000000Z→2026-10-31T12:08:00.000000Z nota=-/false attuale=true]"+
		"["+itoa(terza)+" Artigiano Esempio 2026-10-31T12:04:00.000000Z→2026-10-31T12:05:00.000000Z nota=-/true attuale=false]"+
		"["+itoa(prima)+" Artigiano Esempio 2026-10-31T09:01:00.000000Z→2026-10-31T12:03:00.000000Z nota=-/false attuale=false]" {
		t.Fatalf("giri della Y:\n%s", got)
	}
}

// evento scrive una riga d'audit inventata (come quelle importate dallo standalone).
func evento(t *testing.T, d *db.DB, azione, risorsa string, id any, dati, creato string) {
	t.Helper()
	esegui(t, d, `INSERT INTO eventi_audit (attore, azione, risorsa, id_risorsa, esito, dati, creato_il) VALUES ('titolare@esempio.test', ?, ?, ?, 'ok', ?, ?)`,
		azione, risorsa, fmt.Sprint(id), dati, creato)
}

func ricostruisci(t *testing.T, d *db.DB) {
	t.Helper()
	if err := RicostruisciGiri(ctx, d.Scrittura); err != nil {
		t.Fatal(err)
	}
}

// storiaInventata scrive buste, consegne e audit con i casi limite di cycles/1 che dal vivo non capitano (righe
// importate dallo standalone o da prima che repair_ids esistesse). Dà le buste.
func storiaInventata(t *testing.T, b *Buste, d *db.DB) (b1, b2, b3, b4, b5, b6 int64) {
	t.Helper()
	esegui(t, d, `INSERT INTO artigiani (id, nome) VALUES (2, 'Bottega Zeta')`)
	k := map[int]int64{}
	for i := 1; i <= 10; i++ {
		art := int64(1 + i%2)
		chiusa := "2026-10-01T08:00:00.000000Z"
		if i == 1 || i == 10 {
			chiusa = ""
		}
		k[i] = consegnaIl(t, d, art, "2026-08-01T08:00:00.000000Z", chiusa)
	}
	esegui(t, d, `DELETE FROM consegne_artigiani WHERE id = ?`, k[8]) // uscita dallo storico: l'id resta nell'audit
	ids := make([]int64, 6)
	for i := range ids {
		ids[i], _ = bustaCon(t, b, d, "")
	}
	b1, b2, b3, b4, b5, b6 = ids[0], ids[1], ids[2], ids[3], ids[4], ids[5]
	conf := func(consegna int64, dati, creato string) {
		evento(t, d, "handover.confirmed", "handover", consegna, dati, creato)
	}
	rit := func(busta int64, dati, creato string) {
		evento(t, d, "repair.returned_from_artisan", "repair", busta, dati, creato)
	}
	nomi := func(id int64) string { return `{"handed_over":1,"repair_ids":[` + itoa(id) + `]}` }
	cons := func(c int64) string { return `"handover_id":` + itoa(c) }

	// b1: fuori adesso nella consegna 1 (giro attuale), con una storia lunga.
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ?, artigiano_id = 1, consegnata_artigiano_il = '2026-10-20T08:00:00.000000Z',
		note_artigiano = 'Nota del giro attuale' WHERE id = ?`, k[1], b1)
	conf(k[1], nomi(b1), "2026-09-01T08:00:00.000000Z")
	rit(b1, `{`+cons(k[1])+`,"artisan_notes":true}`, "2026-09-05T08:00:00.000000Z") // uscita nota
	conf(k[2], `{"handed_over":1}`, "2026-09-10T08:00:00.000000Z")                  // conferma senza repair_ids
	rit(b1, `{`+cons(k[2])+`}`, "2026-09-12T08:00:00.000000Z")                      // uscita sconosciuta
	conf(k[3], nomi(b1), "2026-09-20T08:00:00.000000Z")
	conf(k[3], nomi(b1), "2026-09-22T08:00:00.000000Z") // dopo il rientro: non conta
	rit(b1, `{`+cons(k[3])+`}`, "2026-09-21T08:00:00.000000Z")
	rit(b1, `{"handover_id":"`+itoa(k[3])+`","artisan_notes":true}`, "2026-09-25T08:00:00.000000Z") // consegna scritta come testo: nessuna
	evento(t, d, "repair.returned_from_artisan_auto", "repair", b1, `{"bag_number":"1/26"}`, "2026-09-25T08:00:00.000000Z")
	conf(k[4], nomi(b1), "2026-09-26T08:00:00.000000Z")
	conf(k[5], nomi(b1), "2026-09-26T12:00:00.000000Z") // un'altra consegna che la nomina: non conta
	rit(b1, `{`+cons(k[4])+`,"artisan_notes":"true"}`, "2026-09-27T08:00:00.000000Z")
	conf(k[6], `{"repair_ids":`+itoa(b1)+`}`, "2026-09-28T08:00:00.000000Z") // repair_ids non è un elenco
	rit(b1, `{`+cons(k[6])+`}`, "2026-09-29T08:00:00.000000Z")
	conf(k[7], `{"repair_ids":["`+itoa(b1)+`"]}`, "2026-09-30T08:00:00.000000Z") // l'id come testo
	rit(b1, `{`+cons(k[7])+`}`, "2026-10-01T08:00:00.000000Z")
	rit(b1, `{"handover_id":`+itoa(k[7])+`.0}`, "2026-10-02T08:00:00.000000Z") // un numero con la virgola
	rit(b1, `{`+cons(k[1])+`}`, "2026-10-25T08:00:00.000000Z")                 // dopo l'uscita attuale: non è un giro passato

	// b2: mai fuori adesso; un rientro da una consegna uscita dallo storico, e righe che non sono suoi rientri.
	conf(k[8], nomi(b2), "2026-09-02T08:00:00.000000Z")
	rit(b2, `{`+cons(k[8])+`}`, "2026-09-03T08:00:00.000000Z")
	evento(t, d, "repair.returned_from_artisan", "repair", "0"+itoa(b2), `{`+cons(k[8])+`}`, "2026-09-04T08:00:00.000000Z")
	evento(t, d, "repair.returned_from_artisan", "handover", b2, `{`+cons(k[8])+`}`, "2026-09-04T08:00:00.000000Z")
	evento(t, d, "repair.returned_from_artisan", "repair", 999, `{`+cons(k[8])+`}`, "2026-09-04T08:00:00.000000Z") // busta che non c'è

	// b3: sganciata a mano dalla consegna 9 chiusa (e una riga di sgancio con l'id come testo, che non conta).
	conf(k[9], nomi(b3), "2026-09-02T08:00:00.000000Z")
	rit(b3, `{`+cons(k[9])+`,"artisan_notes":false}`, "2026-09-03T08:00:00.000000Z")
	evento(t, d, "handover.bag_detached", "handover", k[9], `{"repair_id":`+itoa(b3)+`}`, "2026-10-02T08:00:00.000000Z")
	conf(k[9], nomi(b4), "2026-09-02T08:00:00.000000Z")
	rit(b4, `{`+cons(k[9])+`}`, "2026-09-03T08:00:00.000000Z")
	evento(t, d, "handover.bag_detached", "handover", k[9], `{"repair_id":"`+itoa(b4)+`"}`, "2026-10-02T08:00:00.000000Z")

	// b4, b5, b6 fuori adesso: con la conferma che le nomina, senza, e senza consegna.
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ?, artigiano_id = 2, consegnata_artigiano_il = '2026-10-19T08:00:00.000000Z' WHERE id = ?`, k[10], b4)
	conf(k[10], nomi(b4), "2026-10-19T08:00:00.000001Z") // come nello standalone, un poco dopo la colonna
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ?, artigiano_id = 2, consegnata_artigiano_il = '2026-10-19T08:00:00.000000Z' WHERE id = ?`, k[10], b5)
	esegui(t, d, `UPDATE buste SET artigiano_id = 2, consegnata_artigiano_il = '2026-10-19T08:00:00.000000Z' WHERE id = ?`, b6)
	return
}

// inventario: ART-19
// Righe d'audit inventate con i casi limite di cycles/1: la tabella ricostruita (import e migrazione) dà gli
// stessi giri della ricostruzione dall'audit. Uscita sconosciuta (conferma senza repair_ids, repair_ids non
// elenco o con testi, consegna non intera), conferme dopo il rientro o di un'altra consegna, nota solo con
// true, rientri a pari istante nell'ordine dell'audit, rientri dopo l'uscita attuale, id scritto con uno zero
// davanti, consegna uscita dallo storico (resta il numero, senza artigiano), sgancio a mano.
func TestGiriInventati(t *testing.T) {
	b, d, _ := prova(t)
	b1, b2, b3, b4, b5, b6 := storiaInventata(t, b, d)
	ricostruisci(t, d)
	stessiGiri(t, b, d, "storia inventata", b1, b2, b3, b4, b5, b6)
	gg, _ := b.GiriDellArtigiano(ctx, b1)
	k := func(i int) string { return itoa(int64(i)) }
	atteso := "[1 Bottega Zeta 2026-10-20T08:00:00.000000Z→- nota=Nota del giro attuale/true attuale=true]" +
		"[- - -→2026-10-02T08:00:00.000000Z nota=-/false attuale=false]" +
		"[" + k(7) + " Bottega Zeta -→2026-10-01T08:00:00.000000Z nota=-/false attuale=false]" +
		"[" + k(6) + " Artigiano Esempio -→2026-09-29T08:00:00.000000Z nota=-/false attuale=false]" +
		"[" + k(4) + " Artigiano Esempio 2026-09-26T08:00:00.000000Z→2026-09-27T08:00:00.000000Z nota=-/false attuale=false]" +
		"[- - -→2026-09-25T08:00:00.000000Z nota=-/true attuale=false]" +
		"[- - -→2026-09-25T08:00:00.000000Z nota=-/false attuale=false]" +
		"[" + k(3) + " Bottega Zeta 2026-09-20T08:00:00.000000Z→2026-09-21T08:00:00.000000Z nota=-/false attuale=false]" +
		"[" + k(2) + " Artigiano Esempio -→2026-09-12T08:00:00.000000Z nota=-/false attuale=false]" +
		"[" + k(1) + " Bottega Zeta 2026-09-01T08:00:00.000000Z→2026-09-05T08:00:00.000000Z nota=-/true attuale=false]"
	if got := scriviGiri(gg); got != atteso {
		t.Errorf("giri di b1:\n%s\natteso\n%s", got, atteso)
	}
	gg, _ = b.GiriDellArtigiano(ctx, b2)
	if got := scriviGiri(gg); got != "["+k(8)+" - 2026-09-02T08:00:00.000000Z→2026-09-03T08:00:00.000000Z nota=-/false attuale=false]" {
		t.Errorf("giri di b2: %s", got)
	}
	if got := valore(t, d, `SELECT group_concat(busta_id || ':' || sganciata, ',') FROM giri_artigiani WHERE consegna_artigiano_id = ?`, 9); got != itoa(b3)+":1,"+itoa(b4)+":0" {
		t.Errorf("sganciate: %v", got)
	}
	if got := valore(t, d, `SELECT group_concat(busta_id || ':' || coalesce(consegna_artigiano_id, '-') || ':' || coalesce(uscita_il, '-'), ',')
		FROM giri_artigiani WHERE rientro_il IS NULL`); got != itoa(b1)+":1:2026-09-01T08:00:00.000000Z,"+itoa(b4)+":10:2026-10-19T08:00:00.000001Z,"+
		itoa(b5)+":10:-,"+itoa(b6)+":-:-" {
		t.Errorf("giri aperti: %v", got)
	}
	// La consegna tolta alla b4 mentre è fuori (un dato strano): il rientro non ha consegna, e l'uscita del giro
	// aperto della consegna 10 non è la sua (cycles/1: nessuna consegna, nessuna conferma).
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = NULL WHERE id = ?`, b4)
	rientra(t, b, b4, "")
	stessiGiri(t, b, d, "rientro senza consegna", b1, b2, b3, b4, b5, b6)
	if got := valore(t, d, `SELECT coalesce(consegna_artigiano_id, '-') || ':' || coalesce(uscita_il, '-') FROM giri_artigiani
		WHERE busta_id = ? ORDER BY id DESC LIMIT 1`, b4); got != "-:-" {
		t.Errorf("rientro senza consegna: %v", got)
	}
}

// inventario: ART-19
// Il motivo della tabella (D-14): quando la conservazione toglie l'audit, i giri restano quelli di prima, e un
// rientro dopo la pulizia prende l'uscita dal giro aperto, non dall'audit che non c'è più.
func TestGiriSopravvivonoAllAudit(t *testing.T) {
	b, d, o := prova(t)
	b1, b2, b3, b4, b5, b6 := storiaInventata(t, b, d)
	ricostruisci(t, d)
	prima := map[int64]string{}
	for _, id := range []int64{b1, b2, b3, b4, b5, b6} {
		prima[id] = scriviGiri(cicliDallAudit(t, d, id))
	}
	esegui(t, d, `DELETE FROM eventi_audit`)
	for _, id := range []int64{b1, b2, b3, b4, b5, b6} {
		if gg, err := b.GiriDellArtigiano(ctx, id); err != nil || scriviGiri(gg) != prima[id] {
			t.Errorf("busta %d dopo la pulizia: %s %v\nprima %s", id, scriviGiri(gg), err, prima[id])
		}
	}
	o.Sposta(time.Hour)
	for _, id := range []int64{b4, b5, b6} {
		rientra(t, b, id, "")
	}
	if got := valore(t, d, `SELECT group_concat(busta_id || ':' || coalesce(consegna_artigiano_id, '-') || ':' || coalesce(uscita_il, '-') || ':' || rientro_il, ',')
		FROM (SELECT * FROM giri_artigiani WHERE busta_id IN (?, ?, ?) AND rientro_il > '2026-10-30' ORDER BY id)`, b4, b5, b6); got != itoa(b4)+":10:2026-10-19T08:00:00.000001Z:2026-10-31T10:00:00.000000Z,"+
		itoa(b5)+":10:-:2026-10-31T10:00:00.000000Z,"+itoa(b6)+":-:-:2026-10-31T10:00:00.000000Z" {
		t.Errorf("rientri dopo la pulizia: %v", got)
	}
	if n := conta(t, d, `SELECT count(*) FROM giri_artigiani WHERE rientro_il IS NULL AND busta_id IN (?, ?, ?)`, b4, b5, b6); n != 0 {
		t.Errorf("giri aperti rimasti: %d", n)
	}
}

// La migrazione 0019 ha le stesse istruzioni di giri.go (la ricostruzione dell'import).
func TestMigrazioneGiriComeIlCodice(t *testing.T) {
	m, err := os.ReadFile(fileMigrazioneGiri)
	if err != nil {
		t.Fatal(err)
	}
	piatto := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	for _, q := range []string{`DELETE FROM giri_artigiani`, sqlRicostruzioneRientri, sqlGiriAperti} {
		if !strings.Contains(piatto(string(m)), piatto(q)+";") {
			t.Errorf("la migrazione non ha:\n%s", q)
		}
	}
}

// inventario: ART-19
// La migrazione su un database già in uso (o già importato) alla 0018: costruisce la tabella dall'audit
// come RicostruisciGiri; rieseguita, dà la stessa tabella.
func TestMigrazioneGiriSuUnDatabaseInUso(t *testing.T) {
	b, d, o := prova(t)
	b1, b2, b3, b4, b5, b6 := storiaInventata(t, b, d)
	// La tabella dall'import, un giro anche dal vivo, poi il database torna alla 0018.
	ricostruisci(t, d)
	o.Sposta(time.Hour)
	rientra(t, b, b4, "Fatto")
	vivo := tabellaGiri(t, d.Lettura)
	esegui(t, d, `DROP TABLE giri_artigiani`)
	// Le migrazioni dopo la 0019 si tolgono con le loro tabelle: la 0021 (slice 122) crea verifiche_backup, la
	// 0022 (slice 124) esecuzioni_backup, la 0023 (slice 125) prove_ripristino (e due colonne di verifiche_backup), la
	// 0024 (slice 128) copie_remote.
	esegui(t, d, `DROP TABLE verifiche_backup`)
	esegui(t, d, `DROP TABLE esecuzioni_backup`)
	esegui(t, d, `DROP TABLE prove_ripristino`)
	esegui(t, d, `DROP TABLE copie_remote`)
	esegui(t, d, `DELETE FROM schema_migrations WHERE versione >= 19`)
	fatte, err := d.Migra(ctx, o)
	// La 0020 (slice 086, la riga 1 di oreficerie) segue la 0019 e su un database in uso non cambia niente.
	if err != nil || !slices.Equal(fatte, []int{19, 20, 21, 22, 23, 24}) {
		t.Fatalf("migra: %v %v", fatte, err)
	}
	r, _ := ricostruite(t, d)
	if got := tabellaGiri(t, d.Lettura); got != r || got != vivo {
		t.Fatalf("migrazione\n%s\nricostruzione\n%s\ndal vivo\n%s", got, r, vivo)
	}
	stessiGiri(t, b, d, "dopo la migrazione", b1, b2, b3, b4, b5, b6)
	m, err := os.ReadFile(fileMigrazioneGiri)
	if err != nil {
		t.Fatal(err)
	}
	prima := tabellaGiri(t, d.Lettura)
	esegui(t, d, string(m))
	if got := tabellaGiri(t, d.Lettura); got != prima {
		t.Fatalf("rieseguita:\n%s\nprima\n%s", got, prima)
	}
}
