package web

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La stima dei tempi sulla scheda (RIP-S-03, RIP-H-55 parte negozio), come il riquadro
// #repair-time-estimate di RepairLive (go-baseline): per le buste in lavorazione, preventivo e in
// attesa materiali, con i testi dello standalone. Il modello è quello della pagina del cliente.

const (
	testoStimaTitolo   = `<div id="repair-time-estimate"><dt>Stima tempi</dt><dd>`
	testoSpenta        = "non mostrata ai clienti: stima spenta nelle impostazioni"
	testoConsegna      = "non mostrata ai clienti: c&#39;è una data di consegna" // l'apostrofo scappato, come Phoenix
	testoInsufficienti = "Dati insufficienti: servono almeno 20 buste pronte (ora "
)

// pronteProva scrive n buste pronte (consegnate) con la durata data in giorni, dentro il campione di oggi;
// passate dall'artigiano se artigiano è vero.
func pronteProva(t *testing.T, d *db.DB, o clock.Orologio, n, giorni int, artigiano bool) {
	t.Helper()
	for range n {
		id := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.CodiceQR, a.DataPrevista = "", "" })
		var uscita any
		if artigiano {
			uscita = "2026-09-10T09:00:00.000000Z"
		}
		eseguiSQL(t, d, `UPDATE buste SET stato = 'consegnata', data_accettazione = '2026-09-01', data_pronta = ?, consegnata_artigiano_il = ? WHERE id = ?`,
			time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC).AddDate(0, 0, giorni).Format(time.DateOnly), uscita, id)
	}
}

func ventiProntePerLaScheda(t *testing.T, d *db.DB, o clock.Orologio) {
	t.Helper()
	for g := 5; g < 25; g++ {
		pronteProva(t, d, o, 1, g, false)
	}
}

func accendiStimaProva(t *testing.T, d *db.DB, valore string) {
	t.Helper()
	eseguiSQL(t, d, `INSERT OR REPLACE INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('stima_tempi_attiva', ?, '2026-10-31T09:00:00.000000Z')`, valore)
}

func schedaStima(t *testing.T, h *browser, id int64) string {
	t.Helper()
	r := h.vai("GET", percorsoScheda(id), nil, nil)
	if r.codice != 200 {
		t.Fatalf("scheda %d: %d", id, r.codice)
	}
	return r.corpo
}

// inventario: RIP-S-03
// Con meno di venti buste pronte di bottega il riquadro dice «Dati insufficienti» con il conto di
// quelle di bottega; mai il motivo per cui il cliente non la vede (vale solo con una stima), mai
// «su X buste». Il riquadro sta dopo «Consegna entro».
func TestSchedaStimaDatiInsufficienti(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil) // con la consegna del 20/09/2026 e l'interruttore spento
	pronteProva(t, d, o, 3, 6, false)
	pronteProva(t, d, o, 2, 9, true)
	b := titolareDentro(t, h)
	corpo := schedaStima(t, b, id)
	contiene(t, "insufficienti", corpo, testoStimaTitolo, "<span>"+testoInsufficienti+"3)</span>", "</dd></div>")
	nonContiene(t, "insufficienti", corpo, "non mostrata ai clienti", " buste</span>", "Giorni in più")
	if i, j := strings.Index(corpo, `id="repair-delivery"`), strings.Index(corpo, testoStimaTitolo); i < 0 || j < i {
		t.Errorf("ordine: consegna %d, stima %d", i, j)
	}
	// Il dipendente la vede uguale.
	contiene(t, "dipendente", schedaStima(t, dipendenteDentro(t, h), id), testoStimaTitolo, "ora 3)")
}

// inventario: RIP-S-03
// Con venti buste: «Pronta di solito in 9-20 giorni dal ritiro · su 20 buste», e perché il cliente
// non la vede: interruttore spento, oppure una consegna scritta; accesa e senza consegna, nessuna nota.
func TestSchedaStimaPiena(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	ventiProntePerLaScheda(t, d, o)
	b := titolareDentro(t, h)
	const frase = "Pronta di solito in 9-20 giorni dal ritiro · su 20 buste"
	// Spenta (e con la consegna): vince «spenta».
	corpo := schedaStima(t, b, id)
	contiene(t, "spenta", corpo, testoStimaTitolo, "<span>"+frase+"</span>", `<span class="sotto">`+testoSpenta+"</span>")
	nonContiene(t, "spenta", corpo, testoInsufficienti, testoConsegna)
	// Accesa, con la consegna: il perché è la consegna.
	accendiStimaProva(t, d, "1")
	corpo = schedaStima(t, b, id)
	contiene(t, "consegna", corpo, "<span>"+frase+"</span>", `<span class="sotto">`+testoConsegna+"</span>")
	nonContiene(t, "consegna", corpo, testoSpenta)
	// Con un testo di consegna importato, uguale.
	eseguiSQL(t, d, `UPDATE buste SET data_prevista = NULL, data_prevista_testo = 'fine mese' WHERE id = ?`, id)
	contiene(t, "testo", schedaStima(t, b, id), testoConsegna)
	// Accesa e senza consegna: nessuna nota.
	eseguiSQL(t, d, `UPDATE buste SET data_prevista_testo = NULL WHERE id = ?`, id)
	corpo = schedaStima(t, b, id)
	contiene(t, "libera", corpo, "<span>"+frase+"</span>")
	nonContiene(t, "libera", corpo, "non mostrata ai clienti")
	// Il negozio la vede anche con l'interruttore a un valore che non è «1».
	accendiStimaProva(t, d, "true")
	contiene(t, "non uno", schedaStima(t, b, id), testoSpenta)
}

// inventario: RIP-S-03
// «Giorni in più» c'è solo se maggiore di zero, con o senza stima; la stima sale dei giorni in più
// e, dopo l'uscita dall'artigiano, del supplemento (7-8 di partenza).
func TestSchedaStimaGiorniInPiu(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	corpo := schedaStima(t, b, id)
	nonContiene(t, "zero", corpo, "Giorni in più")
	eseguiSQL(t, d, `UPDATE buste SET giorni_in_piu = 4 WHERE id = ?`, id)
	corpo = schedaStima(t, b, id)
	contiene(t, "senza stima", corpo, testoInsufficienti, `<span class="blocco-piccolo">Giorni in più: 4</span>`)
	ventiProntePerLaScheda(t, d, o)
	contiene(t, "con stima", schedaStima(t, b, id), "Pronta di solito in 13-24 giorni dal ritiro · su 20 buste", `<span class="blocco-piccolo">Giorni in più: 4</span>`)
	eseguiSQL(t, d, `UPDATE buste SET consegnata_artigiano_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, id)
	contiene(t, "artigiano", schedaStima(t, b, id), "Pronta di solito in 20-32 giorni dal ritiro")
	// Dieci buste passate dall'artigiano: supplemento misurato (30-31 contro 9-20 → 21-11), e «su X buste»
	// le conta (20 + 10) mentre «ora N» sarebbe solo quelle di bottega.
	pronteProva(t, d, o, 10, 30, true)
	contiene(t, "misurato", schedaStima(t, b, id), "<span>Pronta di solito in 34-35 giorni dal ritiro · su 30 buste</span>")
}

// inventario: RIP-S-03
// Solo in lavorazione, preventivo e in attesa materiali: pronta, consegnata, annullata e compro oro
// non hanno il riquadro (open_repair?/1). Una busta nel cestino non si apre.
func TestSchedaStimaSoloBusteAperte(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	ventiProntePerLaScheda(t, d, o)
	b := titolareDentro(t, h)
	for _, c := range []struct {
		stato     string
		compraOro int
		ce        bool
	}{
		{"in_lavorazione", 0, true}, {"preventivo", 0, true}, {"in_attesa_materiali", 0, true},
		{"pronta", 0, false}, {"consegnata", 0, false}, {"annullata", 0, false},
		{"in_valutazione", 1, false}, {"chiusa", 1, false},
	} {
		eseguiSQL(t, d, `UPDATE buste SET stato = ?, compro_oro = ? WHERE id = ?`, c.stato, c.compraOro, id)
		corpo := schedaStima(t, b, id)
		if ha := strings.Contains(corpo, `id="repair-time-estimate"`); ha != c.ce {
			t.Errorf("%s (compro oro %d): riquadro %v", c.stato, c.compraOro, ha)
		}
		if !c.ce {
			nonContiene(t, c.stato, corpo, "Stima tempi", "Giorni in più")
		}
	}
}

// inventario: RIP-S-03
// Leggere la scheda non scrive niente a parte la riga repair.viewed: la stima non tocca le
// impostazioni, le buste, il registro.
func TestSchedaStimaLeggereNonScrive(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	ventiProntePerLaScheda(t, d, o)
	b := titolareDentro(t, h)
	schedaStima(t, b, id) // la prima apertura scrive il suo repair.viewed
	conti := func() []int {
		var n []int
		for _, q := range []string{`SELECT count(*) FROM impostazioni`, `SELECT count(*) FROM buste`,
			`SELECT count(*) FROM eventi_audit WHERE azione <> 'repair.viewed'`, `SELECT coalesce(sum(giorni_in_piu), 0) FROM buste`,
			`SELECT count(*) FROM buste WHERE aggiornato_il <> creato_il`} {
			n = append(n, contaRighe(t, d, q))
		}
		return n
	}
	prima := conti()
	for range 3 {
		schedaStima(t, b, id)
	}
	dopo := conti()
	for i := range prima {
		if prima[i] != dopo[i] {
			t.Errorf("scritture: prima %v, dopo %v", prima, dopo)
			break
		}
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed' AND id_risorsa = '`+strconv.FormatInt(id, 10)+`'`); n != 4 {
		t.Errorf("repair.viewed %d, attese 4 aperture", n)
	}
}
