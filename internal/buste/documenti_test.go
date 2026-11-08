package buste

import (
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/media"
)

// I dati dell'etichetta busta (RIP-S-43) e della scheda PDF (RIP-S-45), come BagLabel, RepairSheet e
// i loro controller dello standalone a go-baseline: la busta letta come get_repair!/1 con artigiano e
// oreficeria (preload_repair/1), la riga d'audit solo dopo il PDF fatto.

func etichettaFinta(b []byte, err error) func(DatiEtichetta) ([]byte, error) {
	return func(DatiEtichetta) ([]byte, error) { return b, err }
}

func schedaFinta(b []byte, err error) func(DatiSchedaPDF) ([]byte, error) {
	return func(DatiSchedaPDF) ([]byte, error) { return b, err }
}

// inventario: RIP-S-43
// L'etichetta di una busta: «→ artigiano» se la busta ne ha uno con il nome, il numero, il QR (il
// codice salvato così com'è, spazi compresi, o il numero busta se manca o è vuoto), la data di oggi
// nel fuso del negozio, «Scad.:» con il testo scritto o la data AAAA-MM-GG (to_string/1 di Date) o
// niente, lo stato con gli spazi al posto di «_», la descrizione. La riga repair.label_generated con
// il numero si scrive dopo il PDF; un PDF non fatto non scrive niente.
func TestDatiDellEtichetta(t *testing.T) {
	b, d, o := prova(t)
	c := crea(t, b, modulo())
	id := itoa(c.ID)
	// Un codice salvato con gli spazi (importato: la creazione dalla slice 053 toglie gli spazi a
	// un'etichetta, D-64).
	esegui(t, d, `UPDATE buste SET codice_qr = ' RIP-2026-0042 ', artigiano_id = 1, data_prevista = '2026-11-20', stato = 'in_attesa_materiali' WHERE id = ?`, c.ID)
	// 23:30 UTC del 31/12 è già l'1/1 in negozio.
	o.Imposta(time.Date(2026, 12, 31, 23, 30, 0, 0, time.UTC))
	pdf, e, err := b.Etichetta(ctx, id, chi, func(e DatiEtichetta) ([]byte, error) {
		if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.label_generated'`); n != 0 {
			t.Error("riga d'audit prima del PDF")
		}
		return []byte("%PDF-finto"), nil
	})
	if err != nil || string(pdf) != "%PDF-finto" {
		t.Fatal(err, pdf)
	}
	atteso := DatiEtichetta{ID: c.ID, Numero: c.Numero, Visibile: Mostrato{Testo: "0042/26", Etichetta: true}, Artigiano: "Artigiano Esempio", QR: " RIP-2026-0042 ", Oggi: "01/01/2027",
		Scadenza: "2026-11-20", Stato: "in attesa materiali", Descrizione: "Anello da stringere"}
	if e != atteso {
		t.Errorf("%+v\natteso\n%+v", e, atteso)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.label_generated' AND esito = 'ok'
		AND risorsa = 'repair' AND id_risorsa = ? AND attore = ? AND dati = ?`, id, chi.Attore, `{"bag_number":"`+c.Numero+`"}`); n != 1 {
		t.Errorf("righe d'audit: %d", n)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`); n != 0 {
		t.Error("l'etichetta ha scritto repair.viewed")
	}

	// Il testo della consegna vince sulla data; senza codice, il QR è il numero; un artigiano senza
	// nome non si stampa.
	esegui(t, d, `UPDATE artigiani SET nome = '' WHERE id = 1`)
	esegui(t, d, `UPDATE buste SET data_prevista_testo = 'fine mese', codice_qr = NULL WHERE id = ?`, c.ID)
	_, e, _ = b.Etichetta(ctx, id, chi, etichettaFinta([]byte("%PDF-x"), nil))
	if e.Scadenza != "fine mese" || e.QR != c.Numero || e.Artigiano != "" {
		t.Errorf("%+v", e)
	}
	esegui(t, d, `UPDATE buste SET data_prevista_testo = NULL, data_prevista = NULL, codice_qr = '', artigiano_id = NULL WHERE id = ?`, c.ID)
	_, e, _ = b.Etichetta(ctx, id, chi, etichettaFinta([]byte("%PDF-x"), nil))
	if e.Scadenza != "" || e.QR != c.Numero || e.Artigiano != "" {
		t.Errorf("%+v", e)
	}
	esegui(t, d, `UPDATE buste SET codice_qr = ' ' WHERE id = ?`, c.ID)
	if _, e, _ = b.Etichetta(ctx, id, chi, etichettaFinta([]byte("%PDF-x"), nil)); e.QR != " " {
		t.Errorf("QR di uno spazio: %q", e.QR)
	}

	guasto := errors.New("motore guasto")
	if _, _, err := b.Etichetta(ctx, id, chi, etichettaFinta(nil, guasto)); !errors.Is(err, ErrPDFNonFatto) || !errors.Is(err, guasto) {
		t.Errorf("errore del motore: %v", err)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.label_generated'`); n != 4 {
		t.Errorf("righe: %d", n)
	}
}

// inventario: RIP-S-43
// La descrizione dell'etichetta come truncate(180, True, '…') di Jinja con la tolleranza di 5: fino a
// 185 caratteri resta intera, oltre 179 caratteri e «…»; i caratteri sono grafemi (String.length/1 di
// Elixir), quindi un accento combinato non si separa. Vuota: «—».
func TestDescrizioneDellEtichetta(t *testing.T) {
	for _, c := range []struct{ in, out string }{
		{strings.Repeat("a", 185), strings.Repeat("a", 185)},
		{strings.Repeat("b", 186), strings.Repeat("b", 179) + "…"},
		{strings.Repeat("é", 185), strings.Repeat("é", 185)},
		{strings.Repeat("é", 186), strings.Repeat("é", 179) + "…"},
		{"", "—"},
		{"Anello", "Anello"},
	} {
		if got := DescrizioneEtichetta(c.in); got != c.out {
			t.Errorf("%q: %q", c.in, got)
		}
	}
	if NomeFileEtichetta("12/26") != "etichetta_busta_12_26.pdf" || NomeFileSchedaPDF("12/26") != "busta_12_26.pdf" {
		t.Error("nomi dei file")
	}
}

// oltre64 è 2^64 + id: un intero che, troncato a 64 bit, sarebbe id.
func oltre64(id int64) string {
	return new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(id)).String()
}

// inventario: RIP-S-43, RIP-S-45
// Etichetta e scheda leggono l'id come get_repair!/1 (gli stessi casi della ricevuta) e non fanno il
// PDF né scrivono la riga per una busta che non c'è, nel cestino o con l'id sbagliato.
func TestDocumentiIdComeEcto(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, modulo())
	viva := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, c.ID)
	mai := func() { t.Error("PDF fatto") }
	for id, atteso := range map[string]error{
		// 2^64 + l'id di una busta che c'è non è quella busta (D-56: oltre i 64 bit non si tronca).
		oltre64(viva.ID): ErrBustaNonTrovata, "+0" + oltre64(viva.ID): ErrBustaNonTrovata,
		itoa(c.ID): ErrBustaNonTrovata, "999": ErrBustaNonTrovata, "0": ErrBustaNonTrovata, "abc": ErrIdNonValido,
		" 1": ErrIdNonValido, "99999999999999999999": ErrBustaNonTrovata, "+099999999999999999999": ErrBustaNonTrovata,
	} {
		if _, _, err := b.Etichetta(ctx, id, chi, func(DatiEtichetta) ([]byte, error) { mai(); return nil, nil }); !errors.Is(err, atteso) {
			t.Errorf("etichetta %q: %v", id, err)
		}
		if _, _, err := b.SchedaPDF(ctx, id, chi, func(DatiSchedaPDF) ([]byte, error) { mai(); return nil, nil }); !errors.Is(err, atteso) {
			t.Errorf("scheda %q: %v", id, err)
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione IN ('repair.label_generated', 'repair.sheet_generated')`); n != 0 {
		t.Errorf("righe: %d", n)
	}
}

// inventario: RIP-S-45
// La scheda PDF: nome del negozio (o «Negozio»), l'oreficeria della busta se ha un nome (il badge),
// le date gg/mm/aaaa, la consegna come testo scritto o data, cliente e telefono salvati sulla busta,
// l'artigiano con il nome, descrizione, note e note interne, il preventivo «testo €» o «€ 123,45» dai
// soli centesimi o niente, la compro oro. Riga repair.sheet_generated dopo il PDF, nessuna
// repair.viewed; un PDF non fatto non scrive niente.
func TestDatiDellaSchedaPDF(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, modulo())
	id := itoa(c.ID)
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2, artigiano_id = 1, data_prevista = '2026-11-20', note_pubbliche = 'Pronta venerdì',
		note_interne = 'Cliente <difficile>', preventivo_testo = '400/500', preventivo_centesimi = 1 WHERE id = ?`, c.ID)
	esegui(t, d, `INSERT INTO profilo_negozio (ambito, nome, condizioni_garanzia, creato_il, aggiornato_il) VALUES ('default', 'Bottega Sintetica', '', ?, ?)`, istanteProva, istanteProva)
	pdf, s, err := b.SchedaPDF(ctx, id, chi, func(DatiSchedaPDF) ([]byte, error) {
		if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.sheet_generated'`); n != 0 {
			t.Error("riga d'audit prima del PDF")
		}
		return []byte("%PDF-finto"), nil
	})
	if err != nil || string(pdf) != "%PDF-finto" {
		t.Fatal(err)
	}
	atteso := DatiSchedaPDF{ID: c.ID, Numero: c.Numero, Visibile: Mostrato{Testo: "N. " + c.Numero}, NomeNegozio: "Bottega Sintetica", Oreficeria: "Oreficeria Esempio",
		DataAccettazione: "31/10/2026", Consegna: "20/11/2026", NomeCliente: "Cliente Prova", TelefonoCliente: "333 000 0001",
		Artigiano: "Artigiano Esempio", Descrizione: "Anello da stringere", NotePubbliche: "Pronta venerdì",
		NoteInterne: "Cliente <difficile>", Preventivo: "400/500 €"}
	if !ugualeScheda(s, atteso) {
		t.Errorf("%+v\natteso\n%+v", s, atteso)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.sheet_generated' AND esito = 'ok'
		AND risorsa = 'repair' AND id_risorsa = ? AND attore = ? AND dati = ?`, id, chi.Attore, `{"bag_number":"`+c.Numero+`"}`); n != 1 {
		t.Errorf("righe d'audit: %d", n)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`); n != 0 {
		t.Error("la scheda ha scritto repair.viewed")
	}

	esegui(t, d, `UPDATE buste SET oreficeria_id = NULL, artigiano_id = NULL, data_prevista = NULL, data_prevista_testo = 'fine mese',
		note_pubbliche = NULL, note_interne = NULL, preventivo_testo = NULL, preventivo_centesimi = 12345, compro_oro = 1, stato = 'in_valutazione' WHERE id = ?`, c.ID)
	esegui(t, d, `UPDATE profilo_negozio SET nome = ''`)
	_, s, _ = b.SchedaPDF(ctx, id, chi, schedaFinta([]byte("%PDF-x"), nil))
	if s.NomeNegozio != "Negozio" || s.Oreficeria != "" || s.Artigiano != "" || s.Consegna != "fine mese" || s.NotePubbliche != "" ||
		s.NoteInterne != "" || s.Preventivo != "€ 123,45" || !s.CompraOro || s.Firma != nil {
		t.Errorf("%+v", s)
	}
	esegui(t, d, `UPDATE buste SET preventivo_centesimi = 0 WHERE id = ?`, c.ID)
	if _, s, _ = b.SchedaPDF(ctx, id, chi, schedaFinta([]byte("%PDF-x"), nil)); s.Preventivo != "€ 0,00" {
		t.Errorf("zero centesimi: %q", s.Preventivo)
	}
	esegui(t, d, `UPDATE buste SET data_prevista_testo = NULL, preventivo_centesimi = NULL WHERE id = ?`, c.ID)
	esegui(t, d, `UPDATE oreficerie SET nome = '' WHERE id = 2`)
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, c.ID)
	_, s, _ = b.SchedaPDF(ctx, id, chi, schedaFinta([]byte("%PDF-x"), nil))
	if s.Consegna != "" || s.Preventivo != "" || s.Oreficeria != "" {
		t.Errorf("%+v", s)
	}

	guasto := errors.New("motore guasto")
	if _, _, err := b.SchedaPDF(ctx, id, chi, schedaFinta(nil, guasto)); !errors.Is(err, ErrPDFNonFatto) || !errors.Is(err, guasto) {
		t.Errorf("errore del motore: %v", err)
	}
	esegui(t, d, `CREATE TRIGGER no_scheda BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.sheet_generated' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if pdf, _, err := b.SchedaPDF(ctx, id, chi, schedaFinta([]byte("%PDF-x"), nil)); !errors.Is(err, ErrPDFNonFatto) || pdf != nil {
		t.Errorf("senza riga: %v %q", err, pdf)
	}
}

func ugualeScheda(a, b DatiSchedaPDF) bool {
	fa, fb := a.Firma, b.Firma
	a.Firma, b.Firma = nil, nil
	if a != b || (fa == nil) != (fb == nil) {
		return false
	}
	return fa == nil || (fa.Riga == fb.Riga && string(fa.PNG) == string(fb.PNG))
}

// inventario: RIP-S-45
// Una busta firmata ha il blocco della firma: la dichiarazione della pagina di firma, «Firmata il
// gg/mm/aaaa hh:mm» nel fuso del negozio (23:04 UTC del 2 gennaio sono le 00:04 del 3), « — ritirata
// da …» se c'è chi ha ritirato, e i byte della firma letti dall'archivio con la verifica
// dell'impronta. Senza istante: «Firmata (data non registrata)». Una firma il cui file manca, con
// un'altra impronta o una chiave non valida, o senza archivio, non dà nessun blocco. Vale anche per
// le firme importate dallo standalone: le colonne sono le stesse.
func TestFirmaDellaSchedaPDF(t *testing.T) {
	b, d, _ := prova(t)
	dir := filepath.Join(t.TempDir(), "media")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	a := media.Nuovo(dir)
	firma := []byte("\x89PNG\r\n\x1a\nfirma-di-prova")
	k, err := a.Salva(firma)
	if err != nil {
		t.Fatal(err)
	}
	c := crea(t, b, modulo())
	id := itoa(c.ID)
	esegui(t, d, `UPDATE buste SET firma_chiave = ?, firma_il = '2026-01-02T23:04:05.000000Z', firma_ritirata_da = 'Mario <Rossi>',
		firma_byte = ? WHERE id = ?`, k, len(firma), c.ID)
	leggi := func() *FirmaScheda {
		t.Helper()
		_, s, err := b.SchedaPDF(ctx, id, chi, schedaFinta([]byte("%PDF-x"), nil))
		if err != nil {
			t.Fatal(err)
		}
		return s.Firma
	}
	if f := leggi(); f != nil {
		t.Errorf("firma senza archivio: %+v", f)
	}
	b.ConFoto(a, nil)
	f := leggi()
	if f == nil || f.Riga != "Firmata il 03/01/2026 00:04 — ritirata da Mario <Rossi>" || string(f.PNG) != string(firma) ||
		DichiarazioneFirma != "Dichiaro di aver ritirato l'oggetto sopra indicato e di accettarne lo stato." {
		t.Fatalf("%+v", f)
	}
	esegui(t, d, `UPDATE buste SET firma_il = NULL, firma_ritirata_da = NULL WHERE id = ?`, c.ID)
	if f := leggi(); f == nil || f.Riga != "Firmata (data non registrata)" {
		t.Errorf("%+v", f)
	}
	// Un nome vuoto (possibile solo da un import: la firma lo normalizza a NULL) si stampa com'è.
	esegui(t, d, `UPDATE buste SET firma_ritirata_da = '' WHERE id = ?`, c.ID)
	if f := leggi(); f == nil || f.Riga != "Firmata (data non registrata) — ritirata da " {
		t.Errorf("ritirata da vuoto: %+v", f)
	}
	esegui(t, d, `UPDATE buste SET firma_il = '2026-07-02T15:04:05.000000Z', firma_ritirata_da = NULL WHERE id = ?`, c.ID)
	if f := leggi(); f == nil || f.Riga != "Firmata il 02/07/2026 17:04" {
		t.Errorf("estate: %+v", f)
	}
	for _, chiave := range []string{strings.Repeat("a", 64), "../fuori", strings.ToUpper(k)} {
		esegui(t, d, `UPDATE buste SET firma_chiave = ? WHERE id = ?`, chiave, c.ID)
		if f := leggi(); f != nil {
			t.Errorf("%q: %+v", chiave, f)
		}
	}
	// Un file con un'altra impronta (guasto o cambiato) non si stampa.
	esegui(t, d, `UPDATE buste SET firma_chiave = ? WHERE id = ?`, k, c.ID)
	p := filepath.Join(dir, k)
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("\x89PNG\r\n\x1a\naltra"), 0o600); err != nil {
		t.Fatal(err)
	}
	if f := leggi(); f != nil {
		t.Errorf("impronta diversa: %+v", f)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.signature_viewed'`); n != 0 {
		t.Error("la scheda ha scritto repair.signature_viewed")
	}
}
