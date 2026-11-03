package buste

import (
	"errors"
	"strconv"
	"testing"
)

// La busta per la ricevuta (RIP-S-42), come Receipt.render_html/1 e ReceiptController.show/2 dello
// standalone a go-baseline: il numero dell'indirizzo letto come Ecto (cast di :id), il negozio letto
// insieme alla busta, la riga d'audit solo dopo un PDF fatto.

func faiFinto(b []byte, err error) func(DatiRicevuta) ([]byte, error) {
	return func(DatiRicevuta) ([]byte, error) { return b, err }
}

// inventario: RIP-S-42
// La ricevuta riuscita scrive repair.receipt_generated con il numero della busta, dopo il PDF; un PDF
// non fatto non scrive niente.
func TestRicevutaScriveLaSuaRiga(t *testing.T) {
	b, d, _ := prova(t)
	c, err := b.Crea(ctx, modulo(), chi)
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(c.ID, 10)
	var visto DatiRicevuta
	pdf, dati, err := b.Ricevuta(ctx, id, chi, func(r DatiRicevuta) ([]byte, error) {
		visto = r
		if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.receipt_generated'`); n != 0 {
			t.Error("riga d'audit prima del PDF")
		}
		return []byte("%PDF-finto"), nil
	})
	if err != nil || string(pdf) != "%PDF-finto" || dati.Numero != c.Numero || visto.Numero != c.Numero {
		t.Fatalf("%v %q %+v", err, pdf, dati)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.receipt_generated' AND esito = 'ok'
		AND risorsa = 'repair' AND id_risorsa = ? AND attore = ? AND dati = ?`, id, chi.Attore, `{"bag_number":"`+c.Numero+`"}`); n != 1 {
		t.Errorf("righe d'audit: %d", n)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`); n != 0 {
		t.Error("la ricevuta ha scritto repair.viewed")
	}
	guasto := errors.New("motore guasto")
	if _, _, err := b.Ricevuta(ctx, id, chi, faiFinto(nil, guasto)); !errors.Is(err, ErrRicevutaNonFatta) || !errors.Is(err, guasto) {
		t.Errorf("errore del motore: %v", err)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.receipt_generated'`); n != 1 {
		t.Errorf("riga scritta per un PDF non fatto: %d", n)
	}
}

// inventario: RIP-S-42
// Se la riga d'audit non si scrive, la ricevuta non si consegna (il with del controller: 503).
func TestRicevutaSenzaAuditNonEsce(t *testing.T) {
	b, d, _ := prova(t)
	c, err := b.Crea(ctx, modulo(), chi)
	if err != nil {
		t.Fatal(err)
	}
	esegui(t, d, `CREATE TRIGGER no_ricevuta BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.receipt_generated'
		BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if pdf, _, err := b.Ricevuta(ctx, strconv.FormatInt(c.ID, 10), chi, faiFinto([]byte("%PDF-x"), nil)); !errors.Is(err, ErrRicevutaNonFatta) || pdf != nil {
		t.Errorf("%v %q", err, pdf)
	}
}

// inventario: RIP-S-42
// L'id dell'indirizzo come lo legge Ecto (Workshop.get_repair!/1, cast di :id, misurato sul banco il
// 03/11/2026): segno e zeri davanti vanno bene («+1», «01»); spazi, decimali o altro sono
// ErrIdNonValido (400); zero, negativi, busta che non c'è o nel cestino sono ErrBustaNonTrovata (404);
// oltre il massimo di un intero a 64 bit ErrIdFuoriScala (lo standalone risponde 500). In nessuno di
// questi casi si fa il PDF o si scrive l'audit.
func TestRicevutaIdComeEcto(t *testing.T) {
	b, d, _ := prova(t)
	c, err := b.Crea(ctx, modulo(), chi)
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(c.ID, 10)
	for _, buono := range []string{id, "+" + id, "0" + id, "00" + id} {
		if _, dati, err := b.Ricevuta(ctx, buono, chi, faiFinto([]byte("%PDF-x"), nil)); err != nil || dati.ID != c.ID {
			t.Errorf("%q: %v", buono, err)
		}
	}
	mai := func(DatiRicevuta) ([]byte, error) { t.Error("PDF fatto"); return nil, nil }
	casi := map[string]error{
		"": ErrIdNonValido, " 1": ErrIdNonValido, "1 ": ErrIdNonValido, "1.0": ErrIdNonValido, "abc": ErrIdNonValido,
		"1/2": ErrIdNonValido, "1_0": ErrIdNonValido, "٣": ErrIdNonValido, "+": ErrIdNonValido, "--1": ErrIdNonValido,
		"0": ErrBustaNonTrovata, "-1": ErrBustaNonTrovata, "-0": ErrBustaNonTrovata, "999": ErrBustaNonTrovata,
		"9223372036854775807": ErrBustaNonTrovata, "-9223372036854775808": ErrBustaNonTrovata,
		"9223372036854775808": ErrIdFuoriScala, "99999999999999999999": ErrIdFuoriScala, "-9223372036854775809": ErrIdFuoriScala,
	}
	for s, atteso := range casi {
		if _, _, err := b.Ricevuta(ctx, s, chi, mai); !errors.Is(err, atteso) {
			t.Errorf("%q: %v, atteso %v", s, err, atteso)
		}
	}
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, c.ID)
	if _, _, err := b.Ricevuta(ctx, id, chi, mai); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("nel cestino: %v", err)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.receipt_generated'`); n != 4 {
		t.Errorf("righe d'audit: %d", n)
	}
}

// inventario: RIP-S-42
// I dati della ricevuta: le copie di nome e telefono sulla busta, la data di accettazione, la
// lavorazione, le note visibili al cliente, i prezzi come stanno, il nome e il sottotitolo del
// negozio letti con la busta; senza profilo del negozio il nome di ripiego «Negozio» e nessun
// sottotitolo.
func TestRicevutaDati(t *testing.T) {
	b, d, _ := prova(t)
	c, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) {
		a.NotePubbliche, a.Preventivo, a.DataPrevista = "Pronta venerdì", "400/500", "2026-11-20"
	}), chi)
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(c.ID, 10)
	_, r, err := b.Ricevuta(ctx, id, chi, faiFinto([]byte("x"), nil))
	if err != nil {
		t.Fatal(err)
	}
	if r.NomeNegozio != "Negozio" || r.Sottotitolo != nil || r.NomeCliente != "Cliente Prova" || r.TelefonoCliente != "333 000 0001" ||
		r.DataAccettazione != "2026-10-31" || r.Descrizione != "Anello da stringere" || r.NotePubbliche == nil ||
		*r.NotePubbliche != "Pronta venerdì" || r.Preventivo.Testo == nil || *r.Preventivo.Testo != "400/500" ||
		r.Consegna.Data == nil || *r.Consegna.Data != "2026-11-20" {
		t.Errorf("%+v", r)
	}
	esegui(t, d, `INSERT INTO profilo_negozio (nome, condizioni_garanzia, sottotitolo, creato_il, aggiornato_il)
		VALUES ('Bottega Sintetica', 'Dodici mesi', 'Oreficeria dal 1952', ?, ?)`, istanteProva, istanteProva)
	if _, r, err = b.Ricevuta(ctx, id, chi, faiFinto([]byte("x"), nil)); err != nil || r.NomeNegozio != "Bottega Sintetica" ||
		r.Sottotitolo == nil || *r.Sottotitolo != "Oreficeria dal 1952" {
		t.Errorf("%v %+v", err, r)
	}
	esegui(t, d, `UPDATE profilo_negozio SET nome = '', sottotitolo = NULL`)
	if _, r, err = b.Ricevuta(ctx, id, chi, faiFinto([]byte("x"), nil)); err != nil || r.NomeNegozio != "Negozio" || r.Sottotitolo != nil {
		t.Errorf("nome vuoto: %v %+v", err, r)
	}
}

func cent(c int64) *int64 { return &c }

// inventario: RIP-S-42
// Prevista: il testo scritto, la data, o «Da concordare» (Repair.date_source/2).
func TestRicevutaPrevista(t *testing.T) {
	for _, c := range []struct {
		c      Consegna
		attesa string
	}{
		{Consegna{Testo: testo("fine mese")}, "fine mese"},
		{Consegna{Testo: testo("fine mese"), Data: testo("2026-09-20")}, "fine mese"},
		{Consegna{Data: testo("2026-09-20")}, "2026-09-20"},
		{Consegna{Testo: testo(""), Data: testo("2026-09-20")}, "2026-09-20"},
		{Consegna{}, "Da concordare"},
		{Consegna{Testo: testo("")}, "Da concordare"},
	} {
		if got := (DatiRicevuta{Consegna: c.c}).Prevista(); got != c.attesa {
			t.Errorf("%+v: %q", c.c, got)
		}
	}
}

// inventario: RIP-S-42
// Gli importi (price_section/1): il preventivo come scritto o, solo in centesimi, «€ 12,50»; il
// prezzo finale con Money.label/1: «€ » davanti a un importo o a un intervallo, il resto com'è; una
// riga per prezzo, nessuna senza prezzi. I casi di receipt_test.exs e cents_and_seller_email_test.exs.
func TestRicevutaImporti(t *testing.T) {
	for _, c := range []struct {
		prev, fin Prezzo
		righe     []string
	}{
		{Prezzo{Testo: testo("400/500")}, Prezzo{Testo: testo("da definire")}, []string{"Preventivo: 400/500", "Prezzo finale: da definire"}},
		{Prezzo{Centesimi: cent(12345)}, Prezzo{}, []string{"Preventivo: € 123,45"}},
		{Prezzo{Centesimi: cent(1250)}, Prezzo{}, []string{"Preventivo: € 12,50"}},
		{Prezzo{Testo: testo("400/500"), Centesimi: cent(12345)}, Prezzo{}, []string{"Preventivo: 400/500"}},
		{Prezzo{Testo: testo("77")}, Prezzo{Testo: testo("35")}, []string{"Preventivo: 77", "Prezzo finale: € 35"}},
		{Prezzo{}, Prezzo{Centesimi: cent(4000)}, []string{"Prezzo finale: € 40,00"}},
		{Prezzo{}, Prezzo{Testo: testo(" 400 - 500,50\n")}, []string{"Prezzo finale: € 400 - 500,50"}},
		{Prezzo{Testo: testo(" 12 ")}, Prezzo{Testo: testo("35€")}, []string{"Preventivo:  12 ", "Prezzo finale: 35€"}},
		{Prezzo{}, Prezzo{}, nil},
		{Prezzo{}, Prezzo{Testo: testo(" da definire ")}, []string{"Prezzo finale:  da definire "}},
		{Prezzo{Testo: testo("")}, Prezzo{Testo: testo(""), Centesimi: cent(5)}, []string{"Prezzo finale: € 0,05"}},
	} {
		got := (DatiRicevuta{Preventivo: c.prev, PrezzoFinale: c.fin}).Importi()
		if len(got) != len(c.righe) {
			t.Errorf("%v: %q", c.righe, got)
			continue
		}
		for i := range got {
			if got[i] != c.righe[i] {
				t.Errorf("%q, attese %q", got, c.righe)
			}
		}
	}
}

// inventario: RIP-S-42
// Money.label/1 su un testo, misurato con lo standalone (GestripStandalone.Money nell'immagine
// fab-220, 03/11/2026): String.trim/1 toglie anche gli spazi Unicode (non separabili compresi).
func TestEtichettaPrezzoComeMoneyLabel(t *testing.T) {
	for in, atteso := range map[string]string{
		" 35 ": "€ 35", " 35 ": "€ 35", " 35 ": "€ 35", " 35": "€ 35", "35,5": "€ 35,5",
		"400/500": "€ 400/500", "400 - 500,50": "€ 400 - 500,50", "35€": "35€", "1.000": "1.000", "35\n": "€ 35",
		"\t35": "€ 35", "35,505": "35,505", "+35": "+35", "٣٥": "٣٥", "35\v-\v40": "€ 35\v-\v40",
	} {
		if got := EtichettaPrezzo(Prezzo{Testo: &in}); got != atteso {
			t.Errorf("%q: %q, atteso %q", in, got, atteso)
		}
	}
}

// inventario: RIP-S-42
// Il nome del file allegato: «ricevuta-<numero con / → ->.pdf».
func TestNomeFileRicevuta(t *testing.T) {
	for n, atteso := range map[string]string{"1/26": "ricevuta-1-26.pdf", "12/26/b": "ricevuta-12-26-b.pdf", "A 1": "ricevuta-A 1.pdf"} {
		if got := NomeFileRicevuta(n); got != atteso {
			t.Errorf("%q: %q", n, got)
		}
	}
}
