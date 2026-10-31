package importer

import (
	"database/sql"
	"fmt"
	"reflect"
	"testing"
)

// Le regole sui dati dell'import verso Go (OPS-03, decise da Stefano il 31/10/2026): le righe nel
// cestino si importano; una riga che non si traduce o che il Go rifiuta blocca, con tabella, id,
// colonna e motivo; nessuna riga sparisce in silenzio; i testi liberi passano così come sono
// accanto ai valori tipizzati; il consenso email non si attribuisce mai.

// colonnaGoDi dà il nome Go di una colonna dello standalone nella tabella t ("" se non c'è).
func colonnaGoDi(t Tabella, col string) string {
	for _, c := range t.Colonne {
		if c.Standalone == col {
			return c.Go
		}
	}
	return ""
}

// idDi legge gli id (in ordine) di una query.
func idDi(t *testing.T, c *sql.DB, q string) []int64 {
	t.Helper()
	rows, err := c.Query(q)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	chiudiRighe(t, rows)
	return ids
}

// inventario: OPS-03
// Le righe nel cestino (deleted_at) si importano come le altre: in ogni tabella che ha il cestino
// le righe cancellate della sorgente sono nel Go, con lo stesso id e lo stesso istante tradotto
// (TestImportCompletoRiconciliato confronta ogni colonna); una busta nel cestino e una no.
func TestRigheNelCestinoImportate(t *testing.T) {
	_, o := preparaImport(t, func(c *sql.DB) {
		esegui(c, `UPDATE repairs SET deleted_at = NULL WHERE id = 2`)
	})
	if es := importa(t, o); es.Verdetto != Riconciliato {
		r, _ := leggiReport(t, o.Report)
		t.Fatalf("verdetto %s, differenze %s", es.Verdetto, differenzeDi(r))
	}
	src, dst := apriLettura(t, o.Sorgente), apriLettura(t, o.Destinazione)
	tabelle := 0
	for _, tab := range Mappa {
		colGo := colonnaGoDi(tab, "deleted_at")
		if tab.Go == "" || colGo == "" {
			continue
		}
		tabelle++
		a := idDi(t, src, `SELECT id FROM `+tab.Standalone+` WHERE deleted_at IS NOT NULL ORDER BY id`)
		b := idDi(t, dst, `SELECT id FROM `+tab.Go+` WHERE `+colGo+` IS NOT NULL ORDER BY id`)
		if len(a) == 0 || !reflect.DeepEqual(a, b) {
			t.Errorf("%s: nel cestino nella sorgente %v, nel Go %v", tab.Standalone, a, b)
		}
	}
	if tabelle < 2 {
		t.Errorf("tabelle con il cestino: %d", tabelle)
	}
	if ids := idDi(t, dst, `SELECT id FROM buste WHERE cancellata_il IS NULL`); !reflect.DeepEqual(ids, []int64{2}) {
		t.Errorf("buste fuori dal cestino %v", ids)
	}
}

// inventario: OPS-03
// Nessuna riga sparisce in silenzio: con righe che non si traducono o che il Go rifiuta, per ogni
// tabella del report le righe scritte sono quelle lette, oppure c'è la differenza del conteggio con
// le righe che mancano; e la riga che manca ha la sua differenza con tabella, id, colonna e motivo.
func TestNessunaRigaSparisceInSilenzio(t *testing.T) {
	casi := []struct {
		nome    string
		ritocca func(*sql.DB)
		voce    Voce
	}{
		{"istante illeggibile", func(c *sql.DB) { esegui(c, `UPDATE customers SET updated_at = 'ieri' WHERE id = 2`) },
			Voce{Codice: "traduzione/istante", Tabella: "customers", Id: ptr(2), Colonna: "updated_at"}},
		{"busta nel cestino che non si traduce", func(c *sql.DB) { esegui(c, `UPDATE repairs SET pinned_at = 'ieri' WHERE id = 1`) },
			Voce{Codice: "traduzione/istante", Tabella: "repairs", Id: ptr(1), Colonna: "pinned_at"}},
		{"canale che il Go rifiuta", func(c *sql.DB) { esegui(c, `UPDATE password_resets SET channel = 'sms'`) },
			Voce{Codice: "vincolo/check", Tabella: "password_resets", Id: ptr(1), Colonna: "channel"}},
		{"data illeggibile in una vendita", func(c *sql.DB) { esegui(c, `UPDATE sales SET sale_on = '2026-13-01' WHERE id = 1`) },
			Voce{Codice: "traduzione/data", Tabella: "sales", Id: ptr(1), Colonna: "sale_on"}},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			dir, o := preparaImport(t, c.ritocca)
			r := controllaBloccato(t, dir, o, importa(t, o))
			if !ha(r.Differenze, c.voce.Codice, c.voce.Tabella, *c.voce.Id, c.voce.Colonna) {
				t.Errorf("differenze %s, manca %+v", differenzeDi(r), c.voce)
			}
			mancanti := 0
			for _, tr := range r.Tabelle {
				var scritte int64
				for _, n := range tr.Scritte {
					scritte += n
				}
				if scritte == tr.Lette {
					continue
				}
				mancanti++
				want := Voce{Codice: "riconciliazione/conteggio", Tabella: tr.Standalone, Righe: ptr(tr.Lette - scritte)}
				trovata := false
				for _, d := range r.Differenze {
					trovata = trovata || reflect.DeepEqual(d, want)
				}
				if !trovata {
					t.Errorf("%s: lette %d, scritte %d, senza la differenza del conteggio", tr.Standalone, tr.Lette, scritte)
				}
			}
			if mancanti == 0 {
				t.Errorf("nessuna tabella con righe mancanti: %s", differenzeDi(r))
			}
		})
	}
}

// inventario: OPS-03
// «Vince il testo»: preventivo, prezzo e data prevista scritti come testo libero passano identici,
// accanto ai valori tipizzati, anche quando i due non si somigliano (il Go non ricalcola niente
// nell'import); un testo con un valore tipizzato NULL e un valore tipizzato con il testo NULL
// passano come sono. Lo stesso per il prezzo delle vendite.
func TestTestiLiberiAccantoAiValori(t *testing.T) {
	_, o := preparaImport(t, func(c *sql.DB) {
		esegui(c, `UPDATE repairs SET estimate_text = 'circa 50 €, forse di più', estimate_cents = 9900,
			final_price_text = 'da definire con il cliente', final_price_cents = NULL,
			expected_on_text = 'fine mese, dopo le ferie', expected_on = '2026-01-01' WHERE id = 1`)
		esegui(c, `UPDATE repairs SET estimate_text = NULL, estimate_cents = 4500, final_price_text = '12,50', final_price_cents = 1250,
			expected_on_text = NULL, expected_on = NULL WHERE id = 2`)
		esegui(c, `UPDATE sales SET price_text = 'trattabile, 30 o 40', price_cents = 3500 WHERE id = 1`)
	})
	if es := importa(t, o); es.Verdetto != Riconciliato {
		r, _ := leggiReport(t, o.Report)
		t.Fatalf("verdetto %s, differenze %s", es.Verdetto, differenzeDi(r))
	}
	dst := apriLettura(t, o.Destinazione)
	got := righe(t, dst, `SELECT id, preventivo_testo, preventivo_centesimi, prezzo_testo, prezzo_centesimi, data_prevista_testo, data_prevista
		FROM buste ORDER BY id`, 7)
	want := map[string][]any{
		"1": {int64(1), "circa 50 €, forse di più", int64(9900), "da definire con il cliente", nil, "fine mese, dopo le ferie", "2026-01-01"},
		"2": {int64(2), nil, int64(4500), "12,50", int64(1250), nil, nil},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buste %v, attese %v", got, want)
	}
	var testo string
	var cent int64
	if err := dst.QueryRow(`SELECT prezzo_testo, prezzo_centesimi FROM vendite WHERE id = 1`).Scan(&testo, &cent); err != nil ||
		testo != "trattabile, 30 o 40" || cent != 3500 {
		t.Errorf("vendita: %q %d %v", testo, cent, err)
	}
}

// inventario: OPS-03
// Il consenso email passa così com'è, mai attribuito: l'email della busta resta il testo della
// sorgente (senza normalizzazione: il consenso è legato al testo, CLI-39) e senza avvisi; una busta
// senza conferma resta senza, una confermata tiene il suo istante. Lo stesso per il consenso di
// Telegram: 0 resta 0.
func TestConsensoEmailComeNellaSorgente(t *testing.T) {
	const emailBusta = " Cliente.Inventato@Esempio.TEST "
	_, o := preparaImport(t, func(c *sql.DB) {
		esegui(c, `UPDATE repairs SET customer_email = ?, email_confirmed_at = NULL, email_ready_sent_at = NULL WHERE id = 1`, emailBusta)
		esegui(c, `UPDATE repairs SET customer_email = 'altro.cliente@esempio.test', email_confirmed_at = '2025-06-07T08:09:10.000000Z' WHERE id = 2`)
		esegui(c, `UPDATE telegram_subscriptions SET consent = 0, consent_at = NULL`)
	})
	if es := importa(t, o); es.Verdetto != Riconciliato {
		r, _ := leggiReport(t, o.Report)
		t.Fatalf("verdetto %s, differenze %s", es.Verdetto, differenzeDi(r))
	}
	r, _ := leggiReport(t, o.Report)
	if len(r.Avvisi) != 0 {
		t.Errorf("avvisi %+v", r.Avvisi)
	}
	dst := apriLettura(t, o.Destinazione)
	got := righe(t, dst, `SELECT id, email_cliente, email_confermata_il, email_pronta_inviata_il FROM buste ORDER BY id`, 4)
	if g := got["1"]; g[1] != emailBusta || g[2] != nil || g[3] != nil {
		t.Errorf("busta 1: %#v", g)
	}
	if g := got["2"]; g[1] != "altro.cliente@esempio.test" || g[2] != "2025-06-07T08:09:10.000000Z" {
		t.Errorf("busta 2: %#v", g)
	}
	var consenso int64
	var il any
	if err := dst.QueryRow(`SELECT consenso, consenso_il FROM iscrizioni_telegram`).Scan(&consenso, &il); err != nil || consenso != 0 || il != nil {
		t.Errorf("consenso Telegram %d %v %v", consenso, il, fmt.Sprint(err))
	}
}
