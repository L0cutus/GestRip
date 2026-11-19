package buste

import (
	"strings"
	"testing"
)

// La stima dei tempi sulla scheda della busta (RIP-S-03, RIP-H-55 parte negozio), come
// RepairEstimate.for_shop/2 dello standalone (go-baseline): il negozio la vede anche con
// l'interruttore spento o con una consegna scritta, e allora c'è scritto perché il cliente no.
// Il modello è quello della pagina del cliente (pagina_cliente_test.go).

func stimaScheda(t *testing.T, b *Buste, id int64) *StimaScheda {
	t.Helper()
	s, err := b.Scheda(ctx, id)
	if err != nil {
		t.Fatalf("scheda %d: %v", id, err)
	}
	return s.Stima
}

func accendiStima(t *testing.T, b *Buste) {
	t.Helper()
	esegui(t, b.d, `INSERT OR REPLACE INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('stima_tempi_attiva', '1', ?)`, istanteProva)
}

// venti buste di bottega da 5 a 24 giorni: percentili 9,75 e 19,25, quindi 9-20.
func ventiPronte(t *testing.T, b *Buste) {
	t.Helper()
	for g := 5; g < 25; g++ {
		pronteDa(t, b, 1, g, false)
	}
}

func TestStimaSchedaPiena(t *testing.T) {
	b, _, _ := prova(t)
	busta := crea(t, b, modulo())
	ventiPronte(t, b)
	// Interruttore spento (partenza): il negozio vede la stima, e perché il cliente no.
	s := stimaScheda(t, b, busta.ID)
	if s == nil || s.Intervallo == nil || *s.Intervallo != (Stima{9, 20}) || s.Buste != 20 || s.Bottega != 20 || s.Minimo != 20 || s.Nascosta != NascostaSpenta {
		t.Fatalf("spenta: %+v", s)
	}
	if s.Intervallo.Frase() != "Pronta di solito in 9-20 giorni dal ritiro" {
		t.Errorf("frase %q", s.Intervallo.Frase())
	}
	// Accesa e senza consegna: niente da spiegare.
	accendiStima(t, b)
	if s := stimaScheda(t, b, busta.ID); s == nil || s.Intervallo == nil || *s.Intervallo != (Stima{9, 20}) || s.Nascosta != NascostaNessuna {
		t.Fatalf("accesa: %+v", s)
	}
	// Una consegna scritta (data) la nasconde al cliente, non al negozio; il testo scritto uguale.
	esegui(t, b.d, `UPDATE buste SET data_prevista = '2026-11-20' WHERE id = ?`, busta.ID)
	if s := stimaScheda(t, b, busta.ID); s == nil || s.Intervallo == nil || s.Nascosta != NascostaConsegna {
		t.Fatalf("con la data: %+v", s)
	}
	esegui(t, b.d, `UPDATE buste SET data_prevista = NULL, data_prevista_testo = 'fine mese' WHERE id = ?`, busta.ID)
	if s := stimaScheda(t, b, busta.ID); s == nil || s.Intervallo == nil || s.Nascosta != NascostaConsegna {
		t.Fatalf("con il testo: %+v", s)
	}
	// Un testo vuoto non è una consegna (date_source/2: text != "").
	esegui(t, b.d, `UPDATE buste SET data_prevista_testo = '' WHERE id = ?`, busta.ID)
	if s := stimaScheda(t, b, busta.ID); s == nil || s.Nascosta != NascostaNessuna {
		t.Fatalf("con il testo vuoto: %+v", s)
	}
	// Spenta e con la consegna: vince «spenta» (hidden_reason/2 guarda l'interruttore per primo).
	esegui(t, b.d, `UPDATE impostazioni SET valore = '0' WHERE chiave = 'stima_tempi_attiva'`)
	esegui(t, b.d, `UPDATE buste SET data_prevista = '2026-11-20' WHERE id = ?`, busta.ID)
	if s := stimaScheda(t, b, busta.ID); s == nil || s.Nascosta != NascostaSpenta {
		t.Fatalf("spenta e consegna: %+v", s)
	}
}

func TestStimaSchedaArtigianoEGiorniInPiu(t *testing.T) {
	b, _, _ := prova(t)
	busta := crea(t, b, modulo())
	ventiPronte(t, b)
	// Giorni in più su tutti e due i capi; il supplemento (7-8) solo dopo l'uscita dall'artigiano,
	// anche con l'interruttore spento.
	esegui(t, b.d, `UPDATE buste SET giorni_in_piu = 2 WHERE id = ?`, busta.ID)
	if s := stimaScheda(t, b, busta.ID); s == nil || *s.Intervallo != (Stima{11, 22}) {
		t.Fatalf("giorni in più: %+v", s)
	}
	esegui(t, b.d, `UPDATE buste SET consegnata_artigiano_il = ? WHERE id = ?`, istanteProva, busta.ID)
	if s := stimaScheda(t, b, busta.ID); s == nil || *s.Intervallo != (Stima{18, 30}) {
		t.Fatalf("con l'artigiano: %+v", s)
	}
	// Già rientrata dall'artigiano (uscita e rientro scritti): il supplemento resta, come handed_over_at
	// nello standalone, che il rientro non toglie. Non conta «fuori bottega adesso».
	esegui(t, b.d, `UPDATE buste SET rientrata_artigiano_il = ? WHERE id = ?`, istanteProva, busta.ID)
	if s := stimaScheda(t, b, busta.ID); s == nil || *s.Intervallo != (Stima{18, 30}) {
		t.Fatalf("rientrata dall'artigiano: %+v", s)
	}
	// Il supplemento dell'impostazione del titolare.
	esegui(t, b.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('stima_tempi_artigiano_min', '3', ?), ('stima_tempi_artigiano_max', '4', ?)`, istanteProva, istanteProva)
	if s := stimaScheda(t, b, busta.ID); s == nil || *s.Intervallo != (Stima{14, 26}) {
		t.Fatalf("supplemento dell'impostazione: %+v", s)
	}
	// Dieci buste passate dall'artigiano: supplemento misurato (30-31 contro 9-20 → 21-11), e «su X
	// buste» conta anche quelle (20 + 10), mentre «ora N» conta solo quelle di bottega.
	pronteDa(t, b, 10, 30, true)
	s := stimaScheda(t, b, busta.ID)
	if s == nil || *s.Intervallo != (Stima{9 + 21 + 2, 20 + 11 + 2}) || s.Buste != 30 || s.Bottega != 20 {
		t.Fatalf("misurato: %+v", s)
	}
}

func TestStimaSchedaDatiInsufficienti(t *testing.T) {
	b, _, _ := prova(t)
	busta := crea(t, b, modulo())
	if s := stimaScheda(t, b, busta.ID); s == nil || s.Intervallo != nil || s.Bottega != 0 || s.Buste != 0 || s.Minimo != 20 {
		t.Fatalf("senza buste pronte: %+v", s)
	}
	pronteDa(t, b, 19, 4, false)
	// Anche con dieci buste dall'artigiano la base manca: l'intervallo no, il conto di bottega è 19.
	pronteDa(t, b, 10, 12, true)
	accendiStima(t, b)
	s := stimaScheda(t, b, busta.ID)
	if s == nil || s.Intervallo != nil || s.Bottega != 19 || s.Buste != 29 {
		t.Fatalf("19 di bottega: %+v", s)
	}
	// Senza intervallo non c'è nulla da spiegare, nemmeno con una consegna: il motivo lo dice
	// comunque la stima, ma la pagina lo mostra solo con l'intervallo (TestSchedaStima*).
	pronteDa(t, b, 1, 4, false)
	if s := stimaScheda(t, b, busta.ID); s == nil || s.Intervallo == nil || *s.Intervallo != (Stima{4, 5}) || s.Bottega != 20 {
		t.Fatalf("la ventesima: %+v", s)
	}
}

func TestStimaSchedaCampione(t *testing.T) {
	b, _, _ := prova(t)
	busta := crea(t, b, modulo())
	pronteDa(t, b, 19, 4, false)
	// Fuori dal campione: oltre 90 giorni, durata negativa, nel cestino, compro oro.
	pronteDa(t, b, 1, 91, false)
	pronteDa(t, b, 1, -1, false)
	pronteDa(t, b, 1, 4, false)
	esegui(t, b.d, `UPDATE buste SET cancellata_il = ? WHERE id = (SELECT max(id) FROM buste)`, istanteProva)
	pronteDa(t, b, 1, 4, false)
	esegui(t, b.d, `UPDATE buste SET compro_oro = 1, stato = 'chiusa' WHERE id = (SELECT max(id) FROM buste)`)
	if s := stimaScheda(t, b, busta.ID); s == nil || s.Intervallo != nil || s.Bottega != 19 {
		t.Fatalf("19 valide: %+v", s)
	}
}

// Una stima c'è solo per una riparazione aperta (open_repair?/1): in lavorazione, preventivo, in
// attesa materiali; mai per un compro oro, né per gli altri stati.
func TestStimaSchedaSoloBusteAperte(t *testing.T) {
	b, _, _ := prova(t)
	busta := crea(t, b, modulo())
	ventiPronte(t, b)
	for _, c := range []struct {
		stato     string
		compraOro bool
		attesa    bool
	}{
		{"in_lavorazione", false, true}, {"preventivo", false, true}, {"in_attesa_materiali", false, true},
		{"pronta", false, false}, {"consegnata", false, false}, {"annullata", false, false},
		{"in_valutazione", true, false}, {"valutata", true, false}, {"perfezionata", true, false},
		{"chiusa", true, false}, {"annullata", true, false},
	} {
		esegui(t, b.d, `UPDATE buste SET stato = ?, compro_oro = ? WHERE id = ?`, c.stato, c.compraOro, busta.ID)
		if s := stimaScheda(t, b, busta.ID); (s != nil) != c.attesa {
			t.Errorf("%s (compro oro %v): stima %+v", c.stato, c.compraOro, s)
		}
	}
}

// La scheda e la pagina del cliente rispondono con lo stesso intervallo: un solo modello.
func TestStimaSchedaUgualeAllaPaginaDelCliente(t *testing.T) {
	b, _, _ := prova(t)
	busta := crea(t, b, modulo())
	ventiPronte(t, b)
	pronteDa(t, b, 10, 30, true)
	accendiStima(t, b)
	esegui(t, b.d, `UPDATE buste SET giorni_in_piu = 3, consegnata_artigiano_il = ? WHERE id = ?`, istanteProva, busta.ID)
	s, p := stimaScheda(t, b, busta.ID), pagina(t, b, busta.Token).Stima
	if s == nil || p == nil || *s.Intervallo != *p {
		t.Fatalf("scheda %+v, pagina %+v", s, p)
	}
}

// Leggere la scheda non scrive niente: né impostazioni, né buste, né registro.
func TestStimaSchedaLeggereNonScrive(t *testing.T) {
	b, d, _ := prova(t)
	busta := crea(t, b, modulo())
	ventiPronte(t, b)
	accendiStima(t, b)
	foto := func() string {
		var sb strings.Builder
		for _, q := range []string{
			`SELECT chiave || '=' || valore || '@' || aggiornata_il FROM impostazioni ORDER BY chiave`,
			`SELECT id || stato || ifnull(data_pronta,'') || giorni_in_piu || ifnull(consegnata_artigiano_il,'') FROM buste ORDER BY id`,
			`SELECT id || azione FROM eventi_audit ORDER BY id`,
		} {
			righe, err := d.Lettura.Query(q)
			if err != nil {
				t.Fatal(err)
			}
			for righe.Next() {
				var r string
				if err := righe.Scan(&r); err != nil {
					t.Fatal(err)
				}
				sb.WriteString(r + "\n")
			}
			_ = righe.Close()
		}
		return sb.String()
	}
	prima := foto()
	for range 3 {
		if s := stimaScheda(t, b, busta.ID); s == nil || s.Intervallo == nil {
			t.Fatal("niente stima")
		}
	}
	if dopo := foto(); dopo != prima {
		t.Errorf("la lettura ha scritto:\n%s\n---\n%s", prima, dopo)
	}
}
