package buste

import (
	"encoding/hex"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// La pagina del cliente /r/<token> (CLI-01, CLI-02, CLI-03, CLI-26), come lo standalone
// (go-baseline): public_repair_controller_test.exs («the three token negatives answer the same
// way», «a link minted for another bag», «a bag moved to an orefice no longer answers its old
// link»), public_repair_estimate_test.exs e repair_estimate_test.exs.

// tokenStandalone è un token della forma dello standalone (43 caratteri base64url, senza «=»),
// inventato: l'import porta la sua impronta SHA-256 (e il testo, se c'è).
const tokenStandalone = "SINTETICO-token-dello-standalone-xxxxxxxxxx"

// conTokenImportato dà alla busta il link come lo porta l'import dallo standalone: solo
// l'impronta (o anche il testo), con la scadenza data.
func conTokenImportato(t *testing.T, b *Buste, id int64, testo bool, scade string) {
	t.Helper()
	var tok any
	if testo {
		tok = tokenStandalone
	}
	esegui(t, b.d, `UPDATE buste SET token_pubblico = ?, hash_token_pubblico = ?, token_pubblico_scade_il = ?,
		token_pubblico_revocato_il = NULL WHERE id = ?`, tok, impronta(tokenStandalone), scade, id)
}

func pagina(t *testing.T, b *Buste, token string) PaginaCliente {
	t.Helper()
	p, err := b.PaginaCliente(ctx, token)
	if err != nil {
		t.Fatalf("pagina di %q: %v", token, err)
	}
	return p
}

func TestPaginaClienteTokenImportato(t *testing.T) {
	for _, testo := range []bool{false, true} {
		t.Run(strconv.FormatBool(testo), func(t *testing.T) {
			b, _, _ := prova(t)
			busta := crea(t, b, modulo())
			conTokenImportato(t, b, busta.ID, testo, "2026-12-31T23:00:00.000000Z")
			if p := pagina(t, b, tokenStandalone); p.ID != busta.ID {
				t.Errorf("busta %d, attesa %d", p.ID, busta.ID)
			}
			// Il token che la busta aveva alla nascita non vale più: l'impronta è un'altra.
			if _, err := b.PaginaCliente(ctx, busta.Token); !errors.Is(err, ErrBustaNonTrovata) {
				t.Errorf("token di prima: %v", err)
			}
		})
	}
}

// TestPaginaClienteImprontaFissa non calcola l'impronta con lo stesso SHA-256 del codice: la
// scrive com'è (sha256sum del token, review della 061), così il test resta indipendente.
func TestPaginaClienteImprontaFissa(t *testing.T) {
	fissa, err := hex.DecodeString("172696fb636dcda3d8e08321c07894ba45466e21b23923c3b8a8dfa2e06deb28")
	if err != nil {
		t.Fatal(err)
	}
	b, _, _ := prova(t)
	busta := crea(t, b, modulo())
	esegui(t, b.d, `UPDATE buste SET token_pubblico = NULL, hash_token_pubblico = ?, token_pubblico_scade_il = ?,
		token_pubblico_revocato_il = NULL WHERE id = ?`, fissa, "2026-12-31T23:00:00.000000Z", busta.ID)
	if p := pagina(t, b, tokenStandalone); p.ID != busta.ID {
		t.Errorf("busta %d, attesa %d", p.ID, busta.ID)
	}
}

func TestPaginaClienteRifiutoUniforme(t *testing.T) {
	b, d, o := prova(t)
	viva := crea(t, b, modulo())
	if p := pagina(t, b, viva.Token); p.ID != viva.ID {
		t.Fatalf("token appena emesso: busta %d", p.ID)
	}
	revocata := crea(t, b, modulo())
	if _, err := b.RevocaLink(ctx, revocata.ID, chi); err != nil {
		t.Fatal(err)
	}
	scaduta := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET token_pubblico_scade_il = ? WHERE id = ?`, istanteProva, scaduta.ID) // alla scadenza esatta
	cestino := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestino.ID)
	rinnovata := crea(t, b, modulo())
	if _, err := b.NuovoLink(ctx, rinnovata.ID, chi); err != nil {
		t.Fatal(err)
	}
	prima := scatta(t, d)
	for nome, token := range map[string]string{
		"inesistente": "QUESTO-TOKEN-NON-ESISTE-0000000000000000000", "revocato": revocata.Token,
		"scaduto": scaduta.Token, "cestino": cestino.Token, "sostituito": rinnovata.Token, "vuoto": "",
		"maiuscolo": strings.ToUpper(viva.Token), "con spazio": viva.Token + " ",
	} {
		p, err := b.PaginaCliente(ctx, token)
		if !errors.Is(err, ErrBustaNonTrovata) || !reflect.DeepEqual(p, PaginaCliente{}) {
			t.Errorf("%s: %+v %v", nome, p, err)
		}
	}
	// Un secondo prima della scadenza vale ancora.
	esegui(t, d, `UPDATE buste SET token_pubblico_scade_il = ? WHERE id = ?`, o.Adesso().Add(time.Second).Format("2006-01-02T15:04:05.000000Z"), scaduta.ID)
	pagina(t, b, scaduta.Token)
	// Leggere non scrive niente: né audit né altro.
	pagina(t, b, viva.Token)
	uguali(t, "lettura della pagina", prima, scatta(t, d))
}

func TestPaginaClienteCambioDiControparte(t *testing.T) {
	b, d, _ := prova(t)
	busta := crea(t, b, modulo())
	// «Il negozio» (id 1) è il cliente diretto, come nessuna oreficeria (Oreficeria.is_b2b/1).
	esegui(t, d, `UPDATE buste SET oreficeria_id = 1 WHERE id = ?`, busta.ID)
	if p := pagina(t, b, busta.Token); p.Oreficeria {
		t.Errorf("oreficeria 1: %+v", p)
	}
	esegui(t, d, `UPDATE buste SET oreficeria_id = NULL WHERE id = ?`, busta.ID)
	if _, err := b.Modifica(ctx, busta.ID, Modifica{OreficeriaID: p("2")}, nil, chi); err != nil {
		t.Fatal(err)
	}
	if _, err := b.PaginaCliente(ctx, busta.Token); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("link di prima del cambio: %v", err)
	}
	// Il nuovo link è dell'oreficeria.
	nuovo, err := b.NuovoLink(ctx, busta.ID, chi)
	if err != nil {
		t.Fatal(err)
	}
	if p := pagina(t, b, nuovo.Token); !p.Oreficeria {
		t.Errorf("pagina dell'oreficeria: %+v", p)
	}
}

func TestPaginaClienteCosaMostra(t *testing.T) {
	b, d, _ := prova(t)
	busta := crea(t, b, con(modulo(), func(a *Accettazione) {
		a.CodiceQR = "RIP-2026-0045"
		a.NotePubbliche = "Pietra da ordinare"
		a.Preventivo = "da 30 a 40"
	}))
	esegui(t, d, `UPDATE buste SET stato = 'pronta', data_pronta = '2026-11-02', prezzo_testo = '35', prezzo_centesimi = 3500,
		data_prevista_testo = NULL, data_prevista = '2026-11-05' WHERE id = ?`, busta.ID)
	esegui(t, d, `INSERT INTO profilo_negozio (nome, condizioni_garanzia, creato_il, aggiornato_il) VALUES ('Oreficeria di Prova', '', ?, ?)`, istanteProva, istanteProva)
	p := pagina(t, b, busta.Token)
	note, pronta := "Pietra da ordinare", "2026-11-02"
	atteso := PaginaCliente{ID: busta.ID, NomeNegozio: "Oreficeria di Prova", Numero: "0045/26", Stato: "Pronta per il ritiro",
		DataAccettazione: "2026-10-31", Consegna: "2026-11-05", DataPronta: &pronta, Descrizione: "Anello da stringere",
		NotePubbliche: &note, Preventivo: "da 30 a 40", PrezzoFinale: "€ 35", ConConversazione: true, numeroInterno: "1/26"}
	if !reflect.DeepEqual(p, atteso) {
		t.Errorf("pagina\n%+v\nattesa\n%+v", p, atteso)
	}
	// Senza etichetta RIP il numero interno con «N.»; i centesimi del preventivo con «€»; il testo
	// della consegna com'è; nessun profilo: «Negozio».
	esegui(t, d, `DELETE FROM profilo_negozio`)
	esegui(t, d, `UPDATE buste SET codice_qr = 'QR-PROVA', preventivo_testo = NULL, preventivo_centesimi = 1250, prezzo_testo = NULL,
		prezzo_centesimi = NULL, data_prevista_testo = 'fine mese', stato = 'preventivo' WHERE id = ?`, busta.ID)
	p = pagina(t, b, busta.Token)
	if p.Numero != "N. "+busta.Numero || p.Preventivo != "€ 12,50" || p.PrezzoFinale != "" || p.Consegna != "fine mese" ||
		p.Stato != "Preventivo in attesa" || p.NomeNegozio != "Negozio" {
		t.Errorf("seconda pagina: %+v", p)
	}
}

func TestEtichetteStatoCliente(t *testing.T) {
	for stato, testo := range map[string]string{"in_lavorazione": "In lavorazione", "in_attesa_materiali": "In attesa materiali",
		"preventivo": "Preventivo in attesa", "pronta": "Pronta per il ritiro", "consegnata": "Consegnata", "annullata": "Annullata",
		"in_valutazione": "In valutazione", "valutata": "Valutata", "perfezionata": "Perfezionata", "chiusa": "Chiusa"} {
		if etichetteStatoCliente[stato] != testo {
			t.Errorf("%s: %q", stato, etichetteStatoCliente[stato])
		}
	}
}

// pronteDa scrive n buste pronte con la durata data (giorni dall'accettazione), fuori o no
// dall'artigiano.
func pronteDa(t *testing.T, b *Buste, n int, giorni int, artigiano bool) {
	t.Helper()
	for range n {
		c := crea(t, b, modulo())
		acc := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
		var consegnata any
		if artigiano {
			consegnata = istanteProva
		}
		esegui(t, b.d, `UPDATE buste SET stato = 'consegnata', data_accettazione = ?, data_pronta = ?, consegnata_artigiano_il = ? WHERE id = ?`,
			acc.Format(time.DateOnly), acc.AddDate(0, 0, giorni).Format(time.DateOnly), consegnata, c.ID)
	}
}

func TestPaginaClienteStima(t *testing.T) {
	b, d, _ := prova(t)
	busta := crea(t, b, modulo())
	stima := func() *Stima { return pagina(t, b, busta.Token).Stima }
	// 20 buste di bottega: 5, 6, …, 24 giorni (percentili 9,75 e 19,25 → 9-20).
	for g := 5; g < 25; g++ {
		pronteDa(t, b, 1, g, false)
	}
	if s := stima(); s != nil {
		t.Fatalf("con l'interruttore spento: %+v", s)
	}
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('stima_tempi_attiva', '1', ?)`, istanteProva)
	if s := stima(); s == nil || *s != (Stima{9, 20}) || s.Frase() != "Pronta di solito in 9-20 giorni dal ritiro" {
		t.Fatalf("stima: %+v", s)
	}
	// Giorni in più e il supplemento dell'impostazione (7-8) per una busta data all'artigiano.
	esegui(t, d, `UPDATE buste SET giorni_in_piu = 2, consegnata_artigiano_il = ? WHERE id = ?`, istanteProva, busta.ID)
	if s := stima(); s == nil || *s != (Stima{18, 30}) {
		t.Fatalf("con l'artigiano: %+v", s)
	}
	// Dieci buste passate dall'artigiano: il supplemento misurato (30-31 contro 9-20 → 21-11).
	pronteDa(t, b, 10, 30, true)
	if s := stima(); s == nil || *s != (Stima{9 + 21 + 2, 20 + 11 + 2}) {
		t.Fatalf("supplemento misurato: %+v", s)
	}
	// Una data di consegna scritta dal negozio la nasconde; togliendola torna.
	esegui(t, d, `UPDATE buste SET data_prevista = '2026-11-20' WHERE id = ?`, busta.ID)
	if s := stima(); s != nil {
		t.Errorf("con la consegna: %+v", s)
	}
	esegui(t, d, `UPDATE buste SET data_prevista = NULL WHERE id = ?`, busta.ID)
	for _, stato := range []string{"pronta", "consegnata", "annullata"} {
		esegui(t, d, `UPDATE buste SET stato = ? WHERE id = ?`, stato, busta.ID)
		if s := stima(); s != nil {
			t.Errorf("%s: %+v", stato, s)
		}
	}
	esegui(t, d, `UPDATE buste SET stato = 'in_attesa_materiali' WHERE id = ?`, busta.ID)
	if stima() == nil {
		t.Error("in attesa materiali: nessuna stima")
	}
}

func TestPaginaClienteStimaCampione(t *testing.T) {
	b, d, _ := prova(t)
	busta := crea(t, b, modulo())
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('stima_tempi_attiva', '1', ?)`, istanteProva)
	pronteDa(t, b, 19, 4, false)
	if s := pagina(t, b, busta.Token).Stima; s != nil {
		t.Fatalf("con 19 buste: %+v", s)
	}
	// Fuori dal campione: oltre 90 giorni, durata negativa, nel cestino, compro oro, pronta da più
	// di un anno.
	pronteDa(t, b, 1, 91, false)
	pronteDa(t, b, 1, -1, false)
	pronteDa(t, b, 1, 4, false)
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = (SELECT max(id) FROM buste)`, istanteProva)
	pronteDa(t, b, 1, 4, false)
	esegui(t, d, `UPDATE buste SET compro_oro = 1, stato = 'chiusa' WHERE id = (SELECT max(id) FROM buste)`)
	pronteDa(t, b, 1, 4, false)
	esegui(t, d, `UPDATE buste SET data_accettazione = '2025-09-01', data_pronta = '2025-09-05' WHERE id = (SELECT max(id) FROM buste)`)
	if s := pagina(t, b, busta.Token).Stima; s != nil {
		t.Fatalf("con 19 buste valide: %+v", s)
	}
	pronteDa(t, b, 1, 0, false) // la ventesima, di zero giorni: conta
	if s := pagina(t, b, busta.Token).Stima; s == nil || *s != (Stima{4, 5}) {
		t.Fatalf("largo almeno un giorno: %+v", s)
	}
}

func TestGiorniStima(t *testing.T) {
	s := func(v string) *string { return &v }
	for _, c := range []struct {
		v      *string
		atteso int64
	}{{nil, 7}, {s(""), 7}, {s("12"), 12}, {s("+3"), 3}, {s("-4"), 0}, {s("120"), 90}, {s(" 5"), 7}, {s("5x"), 7},
		{s("+"), 7}, {s("99999999999999999999"), 90}, {s("-99999999999999999999"), 0}, {s("007"), 7}} {
		if g := giorniStima(c.v, 7); g != c.atteso {
			t.Errorf("%v: %d, atteso %d", c.v, g, c.atteso)
		}
	}
}
