package buste

import (
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
)

// La scadenza dei dettati più prudente dello standalone (D-110, coordinatore 24/11/2026): con l'orologio andato avanti
// rispetto all'evento d'audit più recente (la stessa guardia della conservazione dell'audit, audit.MargineOrologio) o
// dopo un import senza un backup verificato (db.BloccatoDopoImport, come la coda delle cancellazioni dei media) il giro
// non cancella niente, non scrive audit e non dà errore: la pulizia lo segna fatto per oggi e riprova il giorno dopo.

// inventario: RIP-H-51
// Orologio avanti: oltre MargineOrologio dall'evento più recente niente si cancella e niente si scrive; esattamente al
// margine si cancella (come la conservazione dell'audit, che rimanda solo oltre il margine). Il giorno dopo, con un evento
// recente (l'orologio è tornato credibile), il giro cancella.
func TestRaccoltaScadenzaOrologioAvanti(t *testing.T) {
	b, d, o := prova(t)
	inizio := time.Date(2027, 11, 15, 10, 0, 0, 0, time.UTC)
	o.Imposta(inizio)
	vecchia := crea(t, b, modulo()) // l'audit repair.created di adesso è l'evento più recente
	dettatoDel(t, b, vecchia.ID, "2025-01-01T00:00:00.000000Z")

	o.Imposta(inizio.Add(audit.MargineOrologio + time.Microsecond))
	if n, err := b.ScadenzaDettati(ctx); err != nil || n != 0 {
		t.Fatalf("orologio avanti: %d %v", n, err)
	}
	if righeDi(t, b, vecchia.ID) != 1 {
		t.Error("cancellato con l'orologio avanti")
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.training_expired'`); n != 0 {
		t.Errorf("audit con l'orologio avanti: %d", n)
	}

	// Al margine esatto la guardia non scatta.
	o.Imposta(inizio.Add(audit.MargineOrologio))
	if n, err := b.ScadenzaDettati(ctx); err != nil || n != 1 || righeDi(t, b, vecchia.ID) != 0 {
		t.Errorf("al margine: %d %v", n, err)
	}
}

// inventario: RIP-H-51
// Riprova il giorno dopo: rimandata oggi, cancella il giorno dopo quando c'è un evento recente (l'apparecchio è stato
// usato con l'ora giusta).
func TestRaccoltaScadenzaOrologioAvantiRiprova(t *testing.T) {
	b, _, o := prova(t)
	inizio := time.Date(2027, 11, 15, 10, 0, 0, 0, time.UTC)
	o.Imposta(inizio)
	vecchia := crea(t, b, modulo())
	dettatoDel(t, b, vecchia.ID, "2025-01-01T00:00:00.000000Z")
	avanti := inizio.AddDate(0, 3, 0)
	o.Imposta(avanti)
	if n, err := b.ScadenzaDettati(ctx); err != nil || n != 0 || righeDi(t, b, vecchia.ID) != 1 {
		t.Fatalf("oggi: %d %v", n, err)
	}
	o.Imposta(avanti.Add(-2 * time.Hour))
	crea(t, b, modulo()) // un evento d'audit recente
	o.Imposta(avanti.AddDate(0, 0, 1))
	if n, err := b.ScadenzaDettati(ctx); err != nil || n != 1 || righeDi(t, b, vecchia.ID) != 0 {
		t.Errorf("il giorno dopo: %d %v", n, err)
	}
}

// inventario: RIP-H-51
// Dopo un import (riga di import_standalone) senza backup verificato niente si cancella e niente si scrive, senza errore;
// tolto il segno (oggi: un backup verificato dopo l'import lo toglierà, D-11) il giro cancella.
func TestRaccoltaScadenzaDopoImport(t *testing.T) {
	b, d, o := prova(t)
	o.Imposta(time.Date(2027, 11, 15, 10, 0, 0, 0, time.UTC))
	vecchia := crea(t, b, modulo())
	dettatoDel(t, b, vecchia.ID, "2025-01-01T00:00:00.000000Z")
	esegui(t, d, `INSERT INTO import_standalone (id, modalita, sha256_sorgente, byte_sorgente, importato_il)
		VALUES (1, 'sintetico', zeroblob(32), 4096, '2027-11-01T10:00:00.000000Z')`)
	if n, err := b.ScadenzaDettati(ctx); err != nil || n != 0 {
		t.Fatalf("dopo l'import: %d %v", n, err)
	}
	if righeDi(t, b, vecchia.ID) != 1 {
		t.Error("cancellato dopo l'import")
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.training_expired'`); n != 0 {
		t.Errorf("audit dopo l'import: %d", n)
	}
	esegui(t, d, `DELETE FROM import_standalone`)
	if n, err := b.ScadenzaDettati(ctx); err != nil || n != 1 || righeDi(t, b, vecchia.ID) != 0 {
		t.Errorf("senza il segno: %d %v", n, err)
	}
}

// inventario: RIP-H-51
// D-110 punto 3 (coordinatore, 24/11/2026): il limite è il minimo fra adesso meno 12 mesi e l'evento d'audit più recente
// meno 12 mesi, come la conservazione dell'audit con i suoi 5 anni. Con l'orologio avanti di 10 giorni (sotto il margine)
// un dettato che ha meno di 12 mesi rispetto all'ultimo evento resta; uno oltre i 12 mesi da quell'evento se ne va.
func TestRaccoltaScadenzaOrologioAvantiSottoIlMargine(t *testing.T) {
	b, _, o := prova(t)
	inizio := time.Date(2027, 11, 15, 10, 0, 0, 0, time.UTC)
	o.Imposta(inizio)
	giovane := crea(t, b, modulo()) // l'evento più recente è di adesso
	vecchia := crea(t, b, modulo())
	dettatoDel(t, b, giovane.ID, "2026-11-20T10:00:00.000000Z")
	dettatoDel(t, b, vecchia.ID, "2026-11-15T09:59:59.999999Z")
	o.Imposta(inizio.AddDate(0, 0, 10))
	if n, err := b.ScadenzaDettati(ctx); err != nil || n != 1 {
		t.Fatalf("scadenza: %d %v", n, err)
	}
	if righeDi(t, b, giovane.ID) != 1 || righeDi(t, b, vecchia.ID) != 0 {
		t.Error("cancellato il dettato sbagliato")
	}
}

// inventario: RIP-H-51
// D-110 punto 3: il riferimento è l'audit, non l'ultimo dettato. Con la raccolta spenta (nessun dettato nuovo) e un
// evento d'audit recente, i dettati oltre i 12 mesi si cancellano comunque.
func TestRaccoltaScadenzaRaccoltaSpenta(t *testing.T) {
	b, d, o := prova(t)
	o.Imposta(time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC))
	vecchia := crea(t, b, modulo())
	dettatoDel(t, b, vecchia.ID, "2026-10-01T10:00:00.000000Z")
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.training.collect', '0', '2026-10-01T10:00:00.000000Z')
		ON CONFLICT (chiave) DO UPDATE SET valore = '0'`)
	adesso := time.Date(2027, 11, 15, 10, 0, 0, 0, time.UTC)
	o.Imposta(adesso.Add(-time.Hour))
	crea(t, b, modulo()) // un evento d'audit recente, senza dettato
	o.Imposta(adesso)
	if n, err := b.ScadenzaDettati(ctx); err != nil || n != 1 || righeDi(t, b, vecchia.ID) != 0 {
		t.Errorf("scadenza: %d %v", n, err)
	}
}

// inventario: RIP-H-51
// D-110 punto 3: le righe assist.training_expired, che scrive il lavoro stesso (anche con l'orologio sbagliato), non
// contano come evento più recente, come la conservazione dell'audit esclude le proprie. Una da sola, recente, non sposta
// il limite: il dettato con meno di 12 mesi rispetto all'ultimo altro evento resta.
func TestRaccoltaScadenzaIgnoraLeProprieRighe(t *testing.T) {
	b, d, o := prova(t)
	inizio := time.Date(2027, 11, 15, 10, 0, 0, 0, time.UTC)
	o.Imposta(inizio)
	busta := crea(t, b, modulo())
	dettatoDel(t, b, busta.ID, "2026-11-20T10:00:00.000000Z")
	esegui(t, d, `INSERT INTO eventi_audit (attore, azione, risorsa, id_risorsa, esito, dati, creato_il)
		VALUES ('retention', 'assist.training_expired', 'assist', 'training', 'ok', '{"cancellati":1}', '2027-11-25T09:00:00.000000Z')`)
	o.Imposta(inizio.AddDate(0, 0, 10))
	if n, err := b.ScadenzaDettati(ctx); err != nil || n != 0 || righeDi(t, b, busta.ID) != 1 {
		t.Errorf("scadenza: %d %v", n, err)
	}
}
