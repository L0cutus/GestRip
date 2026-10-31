package buste

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

// L'apertura della scheda (RIP-S-01, RIP-S-02), come RepairLive.mount/3, Workshop.get_repair/1,
// Workshop.record_repair_event/3, RepairMessages.mark_read/2, Notifications.mark_quote_read/2 e
// Workshop.Recipient.resolve/1 dello standalone (go-baseline); i casi da repair_live_test.exs e
// repair_quote_live_test.exs.

func apri(t *testing.T, b *Buste, id int64) Scheda {
	t.Helper()
	s, err := b.ApriScheda(ctx, itoa(id), chi)
	if err != nil {
		t.Fatalf("apertura di %d: %v", id, err)
	}
	return s
}

// inventario: RIP-S-01
// Aprire una busta la dà con tutti i campi della scheda e scrive repair.viewed (con il numero) a
// ogni apertura, con l'attore di chi apre.
func TestApriScheda(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, Accettazione{
		NomeCliente: "Ada Esempio", TelefonoCliente: "333 111", ArtigianoID: "1", DataAccettazione: "2026-10-30",
		DataPrevista: "2026-11-20", Descrizione: "Anello\nSaldatura", NotePubbliche: "Nota pubblica",
		NoteInterne: "Nota riservata", Preventivo: "123,45", CompraOro: "false", CodiceQR: "QR-1",
	})
	s := apri(t, b, c.ID)
	if s.ID != c.ID || s.Numero != "1/26" || s.CompraOro || s.DataAccettazione != "2026-10-30" || s.NomeCliente != "Ada Esempio" ||
		s.TelefonoCliente != "333 111" || s.Stato != "in_lavorazione" || s.Descrizione != "Anello\nSaldatura" {
		t.Errorf("scheda %+v", s)
	}
	if s.NotePubbliche == nil || *s.NotePubbliche != "Nota pubblica" || s.NoteInterne == nil || *s.NoteInterne != "Nota riservata" {
		t.Errorf("note %v %v", s.NotePubbliche, s.NoteInterne)
	}
	if s.Artigiano == nil || *s.Artigiano != "Artigiano Esempio" {
		t.Errorf("artigiano %v", s.Artigiano)
	}
	if s.Preventivo.Testo == nil || *s.Preventivo.Testo != "123,45" || s.Preventivo.Centesimi == nil || *s.Preventivo.Centesimi != 12345 {
		t.Errorf("preventivo %+v", s.Preventivo)
	}
	if s.PrezzoFinale.Testo != nil || s.PrezzoFinale.Centesimi != nil {
		t.Errorf("prezzo finale %+v", s.PrezzoFinale)
	}
	if s.Consegna.Testo != nil || s.Consegna.Data == nil || *s.Consegna.Data != "2026-11-20" {
		t.Errorf("consegna %+v", s.Consegna)
	}
	if s.OreficeriaID != nil || s.Oreficeria != nil || s.NomeNegozio != "Negozio" {
		t.Errorf("oreficeria %v %v %q", s.OreficeriaID, s.Oreficeria, s.NomeNegozio)
	}
	if s.DataPronta != nil || s.DataConsegna != nil || s.CodiceQR == nil || *s.CodiceQR != "QR-1" {
		t.Errorf("date e QR %v %v %v", s.DataPronta, s.DataConsegna, s.CodiceQR)
	}
	if !s.CreataIl.Equal(time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("creata il %v", s.CreataIl)
	}
	if s.PreventivoAccettatoIl != nil || s.PreventivoRifiutatoIl != nil {
		t.Errorf("risposte %v %v", s.PreventivoAccettatoIl, s.PreventivoRifiutatoIl)
	}
	if a := ultimoAudit(t, d, "repair.viewed"); a != "repair.viewed|repair|"+itoa(c.ID)+`|ok|{"bag_number":"1/26"}` {
		t.Errorf("audit %q", a)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed' AND attore = ?`, chi.Attore); n != 1 {
		t.Errorf("repair.viewed %d", n)
	}
	apri(t, b, c.ID)
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`); n != 2 {
		t.Errorf("repair.viewed dopo due aperture: %d", n)
	}
	// Nessun segno «letto» da scrivere: nessun'altra riga.
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione IN ('repair.messages_read', 'repair.quote_read')`); n != 0 {
		t.Errorf("segni letti senza niente da leggere: %d", n)
	}
	// Il nome del negozio della voce «Diretta …» viene dal profilo, se c'è.
	esegui(t, d, `INSERT INTO profilo_negozio (nome, condizioni_garanzia, creato_il, aggiornato_il) VALUES ('Oreficeria del Corso', '', ?, ?)`, istanteProva, istanteProva)
	if s := apri(t, b, c.ID); s.NomeNegozio != "Oreficeria del Corso" {
		t.Errorf("nome del negozio %q", s.NomeNegozio)
	}
}

// inventario: RIP-S-01
// Un id che non è un intero positivo (Id.positive/1: Integer.parse senza resto, da 1 al massimo
// di un intero a 64 bit), una busta che non c'è e una nel cestino sono la stessa «non trovata»,
// senza audit. «+1» e «01» sono l'1, come Integer.parse/1.
func TestApriSchedaNonTrovata(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, modulo())
	cestinata := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-2" }))
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestinata.ID)
	prima := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	for _, id := range []string{"999999", "invalid", "0", "-1", "99999999999999999999999999999", "9223372036854775808", "",
		" 1", "1 ", "1.0", "1_0", "0x1", "١", itoa(cestinata.ID)} {
		if _, err := b.ApriScheda(ctx, id, chi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("id %q: %v", id, err)
		}
	}
	if dopo := conta(t, d, `SELECT count(*) FROM eventi_audit`); dopo != prima {
		t.Errorf("righe d'audit per buste non trovate: %d", dopo-prima)
	}
	for _, id := range []string{"+" + itoa(c.ID), "0" + itoa(c.ID)} {
		if s, err := b.ApriScheda(ctx, id, chi); err != nil || s.ID != c.ID {
			t.Errorf("id %q: %v %d", id, err, s.ID)
		}
	}
}

// inventario: RIP-S-01
// Se l'audit dell'apertura non si scrive, la busta non si apre (il ramo {:error, _} di mount:
// «Impossibile aprire la riparazione.»).
func TestApriSchedaAuditNonScritto(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, modulo())
	esegui(t, d, `CREATE TEMP TRIGGER no_visto BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.viewed' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	_, err := b.ApriScheda(ctx, itoa(c.ID), chi)
	if !errors.Is(err, ErrAperturaNonRiuscita) || errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("apertura con l'audit rotto: %v", err)
	}
}

// inventario: RIP-S-01
// Aprire la scheda legge la conversazione (slice 118: messaggi_letti_il = ultimo_messaggio_il
// della busta letta) con l'audit repair.messages_read, e le risposte al preventivo (slice 144:
// il segno «letto» uguale all'istante della risposta) con repair.quote_read; ma solo se
// cambiano: la seconda apertura non scrive niente. L'ultima modifica della busta non cambia
// (update_all dello standalone).
func TestApriSchedaSegnaLettoUnaVolta(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, modulo())
	const msg, risposta = "2026-10-30T08:00:00.000000Z", "2026-10-30T09:30:00.000000Z"
	esegui(t, d, `UPDATE buste SET ultimo_messaggio_il = ?, preventivo_accettato_il = ?, aggiornato_il = '2026-10-01T00:00:00.000000Z' WHERE id = ?`, msg, risposta, c.ID)
	s := apri(t, b, c.ID)
	if valore(t, d, `SELECT messaggi_letti_il FROM buste WHERE id = ?`, c.ID) != msg {
		t.Error("conversazione non letta")
	}
	if valore(t, d, `SELECT preventivo_accettato_letto_il FROM buste WHERE id = ?`, c.ID) != risposta {
		t.Error("accettazione non letta")
	}
	if valore(t, d, `SELECT preventivo_rifiutato_letto_il FROM buste WHERE id = ?`, c.ID) != nil {
		t.Error("rifiuto letto senza rifiuto")
	}
	if valore(t, d, `SELECT aggiornato_il FROM buste WHERE id = ?`, c.ID) != "2026-10-01T00:00:00.000000Z" {
		t.Error("il segno letto ha cambiato l'ultima modifica")
	}
	if s.PreventivoAccettatoIl == nil || !s.PreventivoAccettatoIl.Equal(time.Date(2026, 10, 30, 9, 30, 0, 0, time.UTC)) {
		t.Errorf("accettato il %v", s.PreventivoAccettatoIl)
	}
	if a := ultimoAudit(t, d, "repair.messages_read"); a != "repair.messages_read|repair|"+itoa(c.ID)+`|ok|{"bag_number":"1/26"}` {
		t.Errorf("audit conversazione %q", a)
	}
	if a := ultimoAudit(t, d, "repair.quote_read"); a != "repair.quote_read|repair|"+itoa(c.ID)+`|ok|{"bag_number":"1/26"}` {
		t.Errorf("audit preventivo %q", a)
	}
	// L'ordine dello standalone: vista, conversazione, preventivo.
	if v := valore(t, d, `SELECT group_concat(azione, ',') FROM (SELECT azione FROM eventi_audit WHERE azione LIKE 'repair.%' AND azione != 'repair.created' ORDER BY id)`); v != "repair.viewed,repair.messages_read,repair.quote_read" {
		t.Errorf("ordine %v", v)
	}
	apri(t, b, c.ID)
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione IN ('repair.messages_read', 'repair.quote_read')`); n != 2 {
		t.Errorf("seconda apertura: %d righe di lettura", n)
	}
	// Un messaggio nuovo e un rifiuto (dopo una revoca dell'accettazione) si leggono di nuovo.
	const msg2, rifiuto = "2026-10-31T07:00:00.000000Z", "2026-10-31T08:00:00.000000Z"
	esegui(t, d, `UPDATE buste SET ultimo_messaggio_il = ?, preventivo_accettato_il = NULL, preventivo_rifiutato_il = ? WHERE id = ?`, msg2, rifiuto, c.ID)
	s = apri(t, b, c.ID)
	if valore(t, d, `SELECT messaggi_letti_il FROM buste WHERE id = ?`, c.ID) != msg2 ||
		valore(t, d, `SELECT preventivo_rifiutato_letto_il FROM buste WHERE id = ?`, c.ID) != rifiuto {
		t.Error("messaggio nuovo o rifiuto non letti")
	}
	// Il segno dell'accettazione revocata resta com'era: si scrive solo dove c'è una risposta.
	if valore(t, d, `SELECT preventivo_accettato_letto_il FROM buste WHERE id = ?`, c.ID) != risposta {
		t.Error("il segno dell'accettazione revocata è cambiato")
	}
	if s.PreventivoRifiutatoIl == nil || s.PreventivoAccettatoIl != nil {
		t.Errorf("risposte %v %v", s.PreventivoAccettatoIl, s.PreventivoRifiutatoIl)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione IN ('repair.messages_read', 'repair.quote_read')`); n != 4 {
		t.Errorf("dopo il messaggio nuovo: %d righe di lettura", n)
	}
	// Già letti (segno uguale): niente.
	apri(t, b, c.ID)
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione IN ('repair.messages_read', 'repair.quote_read')`); n != 4 {
		t.Errorf("già letti: %d righe di lettura", n)
	}
}

// inventario: RIP-S-01
// Solo l'uno o solo l'altro: un messaggio senza risposte scrive solo la sua riga; un segno
// diverso dall'istante (non solo vuoto) vale non letto.
func TestApriSchedaSegniSeparati(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET ultimo_messaggio_il = '2026-10-30T08:00:00.000000Z', messaggi_letti_il = '2026-10-29T08:00:00.000000Z' WHERE id = ?`, c.ID)
	apri(t, b, c.ID)
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.messages_read'`); n != 1 {
		t.Errorf("messages_read %d", n)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.quote_read'`); n != 0 {
		t.Errorf("quote_read senza risposte: %d", n)
	}
	esegui(t, d, `UPDATE buste SET preventivo_rifiutato_il = '2026-10-30T08:00:00.000000Z', preventivo_rifiutato_letto_il = '2026-10-29T08:00:00.000000Z' WHERE id = ?`, c.ID)
	apri(t, b, c.ID)
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.messages_read'`); n != 1 {
		t.Errorf("messages_read già letta riscritta: %d", n)
	}
	if valore(t, d, `SELECT preventivo_rifiutato_letto_il FROM buste WHERE id = ?`, c.ID) != "2026-10-30T08:00:00.000000Z" {
		t.Error("rifiuto con un segno vecchio non letto")
	}
}

// inventario: RIP-S-01
// Un segno «letto» che non si scrive non ferma l'apertura (mark_thread_read/2 e
// mark_quote_read/2 tengono la busta com'era): la scheda si apre con la busta letta prima.
func TestApriSchedaSegnoNonScritto(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET ultimo_messaggio_il = '2026-10-30T08:00:00.000000Z', preventivo_accettato_il = '2026-10-30T08:00:00.000000Z' WHERE id = ?`, c.ID)
	esegui(t, d, `CREATE TEMP TRIGGER no_letto BEFORE INSERT ON eventi_audit WHEN NEW.azione IN ('repair.messages_read', 'repair.quote_read') BEGIN SELECT RAISE(ABORT, 'no'); END`)
	s, err := b.ApriScheda(ctx, itoa(c.ID), chi)
	if err != nil || s.ID != c.ID {
		t.Fatalf("apertura: %v", err)
	}
	if valore(t, d, `SELECT messaggi_letti_il FROM buste WHERE id = ?`, c.ID) != nil ||
		valore(t, d, `SELECT preventivo_accettato_letto_il FROM buste WHERE id = ?`, c.ID) != nil {
		t.Error("segno scritto senza il suo audit")
	}
}

// inventario: RIP-S-02
// La busta B2B porta l'oreficeria con nome e telefono; la riga 1 («il negozio») è caricata anche
// lei, ma non fa la busta B2B (Oreficeria.is_b2b/1).
func TestApriSchedaOreficeria(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = "2" }))
	s := apri(t, b, c.ID)
	if s.OreficeriaID == nil || *s.OreficeriaID != 2 || s.Oreficeria == nil || s.Oreficeria.Nome != "Oreficeria Esempio" ||
		s.Oreficeria.Telefono == nil || *s.Oreficeria.Telefono != "0490000000" || !s.B2B() {
		t.Errorf("oreficeria %+v %+v", s.OreficeriaID, s.Oreficeria)
	}
	esegui(t, d, `UPDATE buste SET oreficeria_id = 1 WHERE id = ?`, c.ID)
	s = apri(t, b, c.ID)
	if s.Oreficeria == nil || s.Oreficeria.Nome != "Il negozio di prova" || s.Oreficeria.Telefono != nil || s.B2B() {
		t.Errorf("riga 1 %+v %v", s.Oreficeria, s.B2B())
	}
}

func testo(s string) *string { return &s }

// inventario: RIP-S-02
// Il destinatario della busta (Workshop.Recipient.resolve/1): diretta (anche la riga 1) il
// cliente, B2B l'oreficeria, mai l'altro; un telefono vuoto o di soli spazi è nessun recapito;
// telefono e nome si danno senza spazi ai bordi.
func TestDestinatario(t *testing.T) {
	uno, due := int64(1), int64(2)
	casi := []struct {
		nome   string
		s      Scheda
		atteso Destinatario
		ok     bool
	}{
		{"diretta", Scheda{NomeCliente: " Ada ", TelefonoCliente: " 333 111 "}, Destinatario{ParteCliente, "Ada", "333 111"}, true},
		{"diretta senza telefono", Scheda{NomeCliente: "Ada", TelefonoCliente: "  "}, Destinatario{Parte: ParteCliente}, false},
		{"riga 1", Scheda{NomeCliente: "Ada", TelefonoCliente: "333", OreficeriaID: &uno, Oreficeria: &Oreficeria{Nome: "Negozio", Telefono: testo("049")}},
			Destinatario{ParteCliente, "Ada", "333"}, true},
		{"B2B", Scheda{NomeCliente: "Ada", TelefonoCliente: "333", OreficeriaID: &due, Oreficeria: &Oreficeria{Nome: " Oro ", Telefono: testo(" 049 ")}},
			Destinatario{ParteOreficeria, "Oro", "049"}, true},
		{"B2B senza telefono", Scheda{NomeCliente: "Ada", TelefonoCliente: "333", OreficeriaID: &due, Oreficeria: &Oreficeria{Nome: "Oro"}},
			Destinatario{Parte: ParteOreficeria}, false},
		{"B2B telefono vuoto", Scheda{NomeCliente: "Ada", TelefonoCliente: "333", OreficeriaID: &due, Oreficeria: &Oreficeria{Nome: "Oro", Telefono: testo(" ")}},
			Destinatario{Parte: ParteOreficeria}, false},
		{"senza nome", Scheda{NomeCliente: "  ", TelefonoCliente: "333"}, Destinatario{ParteCliente, "", "333"}, true},
	}
	for _, c := range casi {
		got, ok := c.s.Destinatario()
		if got != c.atteso || ok != c.ok {
			t.Errorf("%s: %+v %v, atteso %+v %v", c.nome, got, ok, c.atteso, c.ok)
		}
	}
}

// inventario: RIP-S-02
// Una busta che punta a un'oreficeria che non c'è è un dato rotto, non una busta diretta: lo
// standalone solleva un errore invece di rispondere al cliente finale, qui l'apertura fallisce.
func TestApriSchedaOreficeriaMancante(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = "2" }))
	esegui(t, d, `PRAGMA foreign_keys = OFF`)
	esegui(t, d, `DELETE FROM oreficerie WHERE id = 2`)
	esegui(t, d, `PRAGMA foreign_keys = ON`)
	if _, err := b.ApriScheda(ctx, itoa(c.ID), chi); err == nil || errors.Is(err, ErrBustaNonTrovata) || errors.Is(err, ErrAperturaNonRiuscita) {
		t.Errorf("oreficeria mancante: %v", err)
	}
}

// inventario: RIP-S-02
// I centesimi come li legge il cliente (Money.label/1 con {:cents, _}): «€ » e due decimali con
// la virgola.
func TestEtichettaCentesimi(t *testing.T) {
	for c, atteso := range map[int64]string{12345: "€ 123,45", 0: "€ 0,00", 5: "€ 0,05", 100: "€ 1,00", 1999: "€ 19,99", 9223372036854775807: "€ 92233720368547758,07"} {
		if got := EtichettaCentesimi(c); got != atteso {
			t.Errorf("%d: %q, atteso %q", c, got, atteso)
		}
	}
}

// conAggancio fa girare dopo la lettura della busta (e l'audit dell'apertura), prima dei segni
// «letto»: quello che un'altra richiesta può fare in quel momento.
func conAggancio(b *Buste, f func()) { b.dopoLettura = f }

// inventario: RIP-S-01
// Una risposta al preventivo arrivata fra la lettura e il segno (answered_since?/1) si segna letta
// lo stesso, e la scheda la mostra (la busta si rilegge dopo il segno).
func TestApriSchedaRispostaArrivataDurante(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, modulo())
	const risposta = "2026-10-31T08:30:00.000000Z"
	conAggancio(b, func() { esegui(t, d, `UPDATE buste SET preventivo_accettato_il = ? WHERE id = ?`, risposta, c.ID) })
	s := apri(t, b, c.ID)
	if valore(t, d, `SELECT preventivo_accettato_letto_il FROM buste WHERE id = ?`, c.ID) != risposta {
		t.Error("risposta arrivata durante l'apertura non segnata letta")
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.quote_read'`); n != 1 {
		t.Errorf("repair.quote_read %d", n)
	}
	if s.PreventivoAccettatoIl == nil || !s.PreventivoAccettatoIl.Equal(time.Date(2026, 10, 31, 8, 30, 0, 0, time.UTC)) {
		t.Errorf("la scheda non mostra la risposta arrivata: %v", s.PreventivoAccettatoIl)
	}
}

// inventario: RIP-S-01
// Dopo il segno della conversazione la busta si rilegge (Workshop.preload_repair/1 su quella
// letta): la scheda mostra quello che c'è adesso, non la copia di prima.
func TestApriSchedaRiletturaDopoIMessaggi(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET ultimo_messaggio_il = '2026-10-30T08:00:00.000000Z' WHERE id = ?`, c.ID)
	conAggancio(b, func() { esegui(t, d, `UPDATE buste SET descrizione = 'Cambiata intanto' WHERE id = ?`, c.ID) })
	if s := apri(t, b, c.ID); s.Descrizione != "Cambiata intanto" {
		t.Errorf("descrizione %q: la busta non è stata riletta", s.Descrizione)
	}
	// Senza niente da segnare non si rilegge: resta la busta letta all'apertura.
	b2, d2, _ := prova(t)
	c2 := crea(t, b2, modulo())
	conAggancio(b2, func() { esegui(t, d2, `UPDATE buste SET descrizione = 'Cambiata intanto' WHERE id = ?`, c2.ID) })
	if s := apri(t, b2, c2.ID); s.Descrizione != "Anello da stringere" {
		t.Errorf("descrizione %q senza segni", s.Descrizione)
	}
}

// inventario: RIP-S-01
// Una busta finita nel cestino fra la lettura e i segni: i segni non si scrivono (gli UPDATE hanno
// il filtro del cestino, come Repo.prepare_query/3 su update_all). Come lo standalone, la riga
// repair.messages_read si scrive lo stesso (mark_read/2 scrive l'audit dopo update_all senza
// guardare quante righe ha cambiato), repair.quote_read no (read_quote/2 si ferma a zero righe); la
// scheda si apre con la busta letta prima.
func TestApriSchedaCestinoDurante(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET ultimo_messaggio_il = '2026-10-30T08:00:00.000000Z', preventivo_accettato_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, c.ID)
	conAggancio(b, func() { esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, c.ID) })
	s, err := b.ApriScheda(ctx, itoa(c.ID), chi)
	if err != nil || s.ID != c.ID {
		t.Fatalf("apertura: %v", err)
	}
	if valore(t, d, `SELECT messaggi_letti_il FROM buste WHERE id = ?`, c.ID) != nil {
		t.Error("segno della conversazione scritto su una busta nel cestino")
	}
	if valore(t, d, `SELECT preventivo_accettato_letto_il FROM buste WHERE id = ?`, c.ID) != nil {
		t.Error("segno del preventivo scritto su una busta nel cestino")
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.messages_read'`); n != 1 {
		t.Errorf("repair.messages_read %d", n)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.quote_read'`); n != 0 {
		t.Errorf("repair.quote_read %d", n)
	}
}

// inventario: RIP-S-01
// Senza niente da segnare l'apertura non apre una transazione di scrittura (mark_quote_read/2:
// «no write transaction on every page view»), nemmeno per una risposta arrivata su una busta
// finita intanto nel cestino. Lo si vede tenendo occupata la scrittura dopo la lettura: se
// l'apertura la chiedesse, aspetterebbe fino alla scadenza del contesto.
func TestApriSchedaNienteScritturaSenzaSegni(t *testing.T) {
	for nome, prima := range map[string]string{
		"niente da leggere":           "",
		"risposta su busta cestinata": `UPDATE buste SET preventivo_accettato_il = '2026-10-31T08:30:00.000000Z', cancellata_il = '2026-10-31T08:31:00.000000Z' WHERE id = ?`,
	} {
		t.Run(nome, func(t *testing.T) {
			b, d, _ := prova(t)
			c := crea(t, b, modulo())
			var occupata *sql.Tx
			conAggancio(b, func() {
				if prima != "" {
					esegui(t, d, prima, c.ID)
				}
				var err error
				if occupata, err = d.Scrittura.BeginTx(ctx, nil); err != nil {
					t.Fatal(err)
				}
			})
			c2, annulla := context.WithTimeout(ctx, 2*time.Second)
			defer annulla()
			inizio := time.Now()
			_, err := b.ApriScheda(c2, itoa(c.ID), chi)
			durata := time.Since(inizio)
			_ = occupata.Rollback()
			if err != nil {
				t.Fatal(err)
			}
			if durata > time.Second {
				t.Errorf("l'apertura ha aspettato la scrittura per %v", durata)
			}
		})
	}
}
