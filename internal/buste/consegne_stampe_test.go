package buste

import (
	"errors"
	"strings"
	"testing"
)

// Le etichette della consegna (slice 078, ART-12) e la stampa diretta con la conferma (D-09): come
// Workshop.list_handover_label_bags/1, HandoverLabels e record_handover_labels/3 dello standalone per il
// PDF; solo Go la conferma dopo il lavoro accettato.

// pdfFinto è un fai che ricorda le etichette ricevute e dà un PDF finto.
func pdfFinto(viste *[]DatiEtichetta) func([]DatiEtichetta) ([]byte, error) {
	return func(ee []DatiEtichetta) ([]byte, error) {
		*viste = append((*viste)[:0], ee...)
		return []byte("%PDF-finto"), nil
	}
}

// inventario: ART-12
// Il PDF della consegna: le buste della consegna lette adesso, per numero (il testo, come bag_number
// dello standalone), il cestino escluso, una busta rientrata compresa; ogni etichetta con «→» l'artigiano
// della consegna (non quello della busta) e i dati dell'etichetta della busta. Solo a PDF fatto
// handover.labels_generated con il numero delle buste; nessuna modifica a consegna e buste. Una consegna che
// non c'è o senza buste è ErrConsegnaVuota, senza riga; un PDF non fatto è ErrPDFNonFatto, senza riga.
func TestEtichetteDellaConsegna(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO artigiani (id, nome) VALUES (2, 'Beta Incastonatore')`)
	consegna := consegnaIl(t, d, 1, istanteProva, "")
	var viste []DatiEtichetta
	for _, id := range []int64{consegna, 999} {
		if _, err := b.EtichetteDellaConsegna(ctx, id, chi, pdfFinto(&viste)); !errors.Is(err, ErrConsegnaVuota) || err.Error() != "Consegna vuota o non trovata" {
			t.Fatalf("consegna %d: %v", id, err)
		}
	}
	// Create in un ordine diverso da quello dei numeri: l'ordine è del numero, non dell'id.
	rientrata, _ := bustaCon(t, b, d, "")
	due, _ := bustaCon(t, b, d, "")
	cestino, _ := bustaCon(t, b, d, "")
	dieci, _ := bustaCon(t, b, d, "RIP-2026-0045")
	altra, _ := bustaCon(t, b, d, "")
	esegui(t, d, `UPDATE buste SET numero = '10/26', descrizione = 'Collana', data_prevista_testo = 'fine mese', stato = 'in_lavorazione', artigiano_id = 2 WHERE id = ?`, dieci)
	esegui(t, d, `UPDATE buste SET numero = '2/26' WHERE id = ?`, due)
	esegui(t, d, `UPDATE buste SET numero = '30/26', rientrata_artigiano_il = ?, consegnata_artigiano_il = ? WHERE id = ?`, istanteProva, istanteProva, rientrata)
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id IN (?, ?, ?, ?)`, consegna, dieci, due, cestino, rientrata)
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestino)
	prima := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'handover.%'`)
	giri := giroDi(t, d, dieci) + giroDi(t, d, due) + giroDi(t, d, rientrata) + giroDi(t, d, altra)
	pdf, err := b.EtichetteDellaConsegna(ctx, consegna, chi, pdfFinto(&viste))
	if err != nil || string(pdf) != "%PDF-finto" {
		t.Fatalf("%q, %v", pdf, err)
	}
	var numeri []string
	for _, e := range viste {
		numeri = append(numeri, e.Numero)
		if e.Artigiano != "Artigiano Esempio" || e.Oggi != "31/10/2026" {
			t.Errorf("%s: artigiano %q, oggi %q", e.Numero, e.Artigiano, e.Oggi)
		}
	}
	if strings.Join(numeri, " ") != "10/26 2/26 30/26" {
		t.Fatalf("ordine %v", numeri)
	}
	if e := viste[0]; e.ID != dieci || e.QR != "RIP-2026-0045" || e.Visibile.Titolo() != "#0045/26" || e.Scadenza != "fine mese" ||
		e.Stato != "in lavorazione" || e.Descrizione != "Collana" {
		t.Errorf("etichetta %+v", e)
	}
	if e := viste[1]; e.QR != "2/26" || e.Visibile.Titolo() != "N. 2/26" {
		t.Errorf("senza codice %+v", e)
	}
	if got := ultimoAudit(t, d, "handover.labels_generated"); got != "handover.labels_generated|handover|"+itoa(consegna)+`|ok|{"bags":3}` {
		t.Fatalf("audit %s", got)
	}
	if got := valore(t, d, `SELECT attore FROM eventi_audit WHERE azione = 'handover.labels_generated'`); got != chi.Attore {
		t.Fatalf("attore %v", got)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'handover.%'`) != prima+1 || giroDi(t, d, dieci)+giroDi(t, d, due)+giroDi(t, d, rientrata)+giroDi(t, d, altra) != giri ||
		consegneScritte(t, d) != itoa(consegna)+":1:aperta" {
		t.Fatal("il PDF ha cambiato qualcosa")
	}
	// Un PDF non fatto: niente riga.
	_, err = b.EtichetteDellaConsegna(ctx, consegna, chi, func([]DatiEtichetta) ([]byte, error) { return nil, errors.New("rotto") })
	if !errors.Is(err, ErrPDFNonFatto) || conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'handover.%'`) != prima+1 {
		t.Fatalf("PDF non fatto: %v", err)
	}
	// La riga che non si scrive: niente PDF.
	esegui(t, d, `CREATE TRIGGER niente_audit BEFORE INSERT ON eventi_audit BEGIN SELECT RAISE(ABORT, 'audit rotto'); END`)
	if pdf, err := b.EtichetteDellaConsegna(ctx, consegna, chi, pdfFinto(&viste)); !errors.Is(err, ErrPDFNonFatto) || pdf != nil {
		t.Fatalf("senza riga: %q, %v", pdf, err)
	}
	esegui(t, d, `DROP TRIGGER niente_audit`)
	// Una consegna chiusa dà lo stesso il PDF; le sue buste tutte nel cestino sono una consegna vuota.
	esegui(t, d, `UPDATE consegne_artigiani SET chiusa_il = ? WHERE id = ?`, istanteProva, consegna)
	if _, err := b.EtichetteDellaConsegna(ctx, consegna, chi, pdfFinto(&viste)); err != nil || len(viste) != 3 {
		t.Fatalf("chiusa: %v", err)
	}
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE consegna_artigiano_id = ?`, istanteProva, consegna)
	if _, err := b.EtichetteDellaConsegna(ctx, consegna, chi, pdfFinto(&viste)); !errors.Is(err, ErrConsegnaVuota) {
		t.Fatalf("tutte nel cestino: %v", err)
	}
}

// inventario: ART-12
// La stampa diretta (D-09): il PDF da stampare senza riga d'audit (EtichetteDaStampare, con gli id delle buste
// stampate, per numero); dopo il lavoro accettato, StampaConsegnaAccettata conferma come ConfermaConsegna e scrive
// handover.labels_printed (buste, coda, lavoro) e handover.confirmed nella stessa transazione. Una consegna
// chiusa o che non c'è è ErrConsegnaNonDisponibile, una senza buste da timbrare ErrNienteDaConfermare, senza scrivere niente; se una riga non si scrive nessuna
// busta esce. StampaConsegnaNonConfermata lascia la traccia del lavoro accettato e non confermato.
func TestStampaDellaConsegna(t *testing.T) {
	b, d, o := prova(t)
	consegna := consegnaIl(t, d, 1, istanteProva, "")
	var viste []DatiEtichetta
	if _, _, err := b.EtichetteDaStampare(ctx, consegna, pdfFinto(&viste)); !errors.Is(err, ErrConsegnaVuota) {
		t.Fatalf("vuota: %v", err)
	}
	uno, _ := bustaCon(t, b, d, "")
	due, _ := bustaCon(t, b, d, "")
	esegui(t, d, `UPDATE buste SET consegna_artigiano_id = ? WHERE id IN (?, ?)`, consegna, uno, due)
	pdf, ids, err := b.EtichetteDaStampare(ctx, consegna, pdfFinto(&viste))
	if err != nil || len(ids) != 2 || ids[0] != uno || ids[1] != due || string(pdf) != "%PDF-finto" || len(viste) != 2 || viste[0].Artigiano != "Artigiano Esempio" {
		t.Fatalf("%q %v %v", pdf, ids, err)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'handover.%'`) != 0 {
		t.Fatal("il PDF da stampare ha scritto nel registro")
	}
	if _, _, err := b.EtichetteDaStampare(ctx, consegna, func([]DatiEtichetta) ([]byte, error) { return nil, errors.New("rotto") }); !errors.Is(err, ErrPDFNonFatto) {
		t.Fatalf("PDF non fatto: %v", err)
	}
	// Una riga che non si scrive: niente esce.
	esegui(t, d, `CREATE TRIGGER niente_conferma BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'handover.confirmed' BEGIN SELECT RAISE(ABORT, 'audit rotto'); END`)
	if _, err := b.StampaConsegnaAccettata(ctx, consegna, ids, "Buste-76x127", "Buste-76x127-1", chi); err == nil {
		t.Fatal("conferma senza audit riuscita")
	}
	if giroDi(t, d, uno) != itoa(consegna)+"|-|-|-|-" || conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'handover.%'`) != 0 {
		t.Fatal("scritto qualcosa senza la riga della conferma")
	}
	esegui(t, d, `DROP TRIGGER niente_conferma`)
	o.Sposta(60e9)
	confermate, err := b.StampaConsegnaAccettata(ctx, consegna, ids, "Buste-76x127", "Buste-76x127-1", chi)
	if err != nil || confermate != 2 {
		t.Fatalf("%d, %v", confermate, err)
	}
	if giroDi(t, d, uno) != itoa(consegna)+"|si|-|-|-" || giroDi(t, d, due) != itoa(consegna)+"|si|-|-|-" {
		t.Fatal("buste non uscite")
	}
	if got := ultimoAudit(t, d, "handover.labels_printed"); got != "handover.labels_printed|handover|"+itoa(consegna)+`|ok|{"bags":2,"job":"Buste-76x127-1","queue":"Buste-76x127","repair_ids":[`+itoa(uno)+","+itoa(due)+"]}" {
		t.Fatalf("stampa %s", got)
	}
	if got := ultimoAudit(t, d, "handover.confirmed"); got != "handover.confirmed|handover|"+itoa(consegna)+`|ok|{"handed_over":2,"repair_ids":[`+itoa(uno)+","+itoa(due)+"]}" {
		t.Fatalf("conferma %s", got)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'handover.%'`) != 2 || consegneScritte(t, d) != itoa(consegna)+":1:aperta" {
		t.Fatal("righe o consegna")
	}
	// Già confermata (un altro l'ha fatto durante il lavoro): nessuna busta da timbrare, niente righe.
	if _, err := b.StampaConsegnaAccettata(ctx, consegna, ids, "Buste-76x127", "Buste-76x127-3", chi); !errors.Is(err, ErrNienteDaConfermare) {
		t.Fatalf("già confermata: %v", err)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'handover.%'`) != 2 {
		t.Fatal("righe per una consegna già confermata")
	}
	chiusa := consegnaIl(t, d, 1, istanteProva, istanteProva)
	for _, id := range []int64{chiusa, 999} {
		if _, err := b.StampaConsegnaAccettata(ctx, id, []int64{uno}, "Buste-76x127", "Buste-76x127-2", chi); !errors.Is(err, ErrConsegnaNonDisponibile) {
			t.Fatalf("consegna %d: %v", id, err)
		}
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'handover.%'`) != 2 {
		t.Fatal("un rifiuto ha scritto nel registro")
	}
	if err := b.StampaConsegnaNonConfermata(ctx, chiusa, []int64{uno}, "Buste-76x127", "Buste-76x127-2", chi); err != nil {
		t.Fatal(err)
	}
	if got := ultimoAudit(t, d, "handover.labels_printed"); got != "handover.labels_printed|handover|"+itoa(chiusa)+`|not_confirmed|{"bags":1,"job":"Buste-76x127-2","queue":"Buste-76x127","repair_ids":[`+itoa(uno)+"]}" {
		t.Fatalf("traccia %s", got)
	}
	if NomeFileEtichetteConsegna(12) != "etichette_consegna_12.pdf" {
		t.Error("nome del file")
	}
}
