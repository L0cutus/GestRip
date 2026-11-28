package buste

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// La raccolta dei dettati dalla scheda e la scadenza a 12 mesi (slice 115; RIP-S-26, RIP-H-51 parte durata), come
// l'evento training-exclude-customer di RepairLive (repair_sheet_ux_live_test.exs, «Escludi dalla raccolta dettati» on the
// sheet) e Training.expire/1 (training_test.exs, «how long a dictation lives») dello standalone a go-baseline. Dettati,
// nomi e telefoni inventati.

// inventario: RIP-S-26
// Dalla scheda la busta è già nota: il cliente della busta perde i dettati di tutte le sue buste (anche nel cestino) ed
// entra in dettati_esclusioni, con l'audit della 114 sul cliente; le buste di un altro cliente non si toccano.
func TestRaccoltaEscludiClienteDallaScheda(t *testing.T) {
	b, d, _ := prova(t)
	accendi(t, b, "500")
	prima := crea(t, b, conDettato("QR-S1"))
	seconda := crea(t, b, conDettato("QR-S2"))
	nelCestino := crea(t, b, conDettato("QR-S3"))
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, nelCestino.ID)
	altro := crea(t, b, con(conDettato("QR-S4"), func(a *Accettazione) { a.NomeCliente, a.TelefonoCliente = "Altro Inventato", "333 000 0998" }))
	if prima.ClienteID != seconda.ClienteID || prima.ClienteID != nelCestino.ClienteID || altro.ClienteID == prima.ClienteID {
		t.Fatal("clienti delle buste di prova")
	}
	n, fatto, err := b.EscludiClienteDellaBusta(ctx, seconda.ID, chi)
	if err != nil || !fatto || n != 3 {
		t.Fatalf("esclusione: %d %v %v", n, fatto, err)
	}
	if righeRaccolta(t, b) != 1 || righeDi(t, b, altro.ID) != 1 {
		t.Errorf("righe rimaste %d", righeRaccolta(t, b))
	}
	if conta(t, d, `SELECT count(*) FROM dettati_esclusioni WHERE cliente_id = ?`, prima.ClienteID) != 1 ||
		conta(t, d, `SELECT count(*) FROM dettati_esclusioni`) != 1 {
		t.Error("esclusione non scritta")
	}
	if conta(t, d, `SELECT count(*) FROM buste WHERE id IN (?, ?, ?)`, prima.ID, seconda.ID, nelCestino.ID) != 3 {
		t.Error("buste sparite")
	}
	atteso := "assist.training_customer_excluded|customer|" + itoa(prima.ClienteID) + `|ok|{"cancellati":3}|titolare@esempio.test`
	if got := azioniRaccolta(t, b); strings.Join(got, "\n") != atteso {
		t.Errorf("audit %v", got)
	}
}

// inventario: RIP-S-26
// Un secondo tocco (cliente già escluso, anche da un'altra sua busta) non scrive niente: né riga, né audit; lo dice
// fatto = false.
func TestRaccoltaEscludiClienteDallaSchedaSecondoTocco(t *testing.T) {
	b, d, _ := prova(t)
	accendi(t, b, "500")
	prima := crea(t, b, conDettato("QR-T1"))
	seconda := crea(t, b, conDettato("QR-T2"))
	if _, fatto, err := b.EscludiClienteDellaBusta(ctx, prima.ID, chi); err != nil || !fatto {
		t.Fatalf("prima volta: %v %v", fatto, err)
	}
	// Una riga nata dopo (dato importato, o scritta a mano) resta: il secondo tocco non cancella.
	esegui(t, d, `INSERT INTO dettati_addestramento (busta_id, testo, proposta, modello, creato_il) VALUES (?, 'inventato', '{}', NULL, ?)`,
		seconda.ID, istanteProva)
	audit := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	for _, id := range []int64{prima.ID, seconda.ID} {
		if n, fatto, err := b.EscludiClienteDellaBusta(ctx, id, chi); err != nil || fatto || n != 0 {
			t.Errorf("secondo tocco dalla busta %d: %d %v %v", id, n, fatto, err)
		}
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit`) != audit {
		t.Error("il secondo tocco ha scritto l'audit")
	}
	if righeDi(t, b, seconda.ID) != 1 || conta(t, d, `SELECT count(*) FROM dettati_esclusioni`) != 1 {
		t.Error("il secondo tocco ha scritto o cancellato")
	}
}

// inventario: RIP-S-26
// Con la raccolta mai accesa (la chiave del lotto non c'è) l'esclusione dalla scheda non c'è: niente si scrive. Una
// raccolta accesa e poi spenta (o a lotto finito) la permette ancora.
func TestRaccoltaEscludiClienteDallaSchedaMaiAccesa(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, modulo())
	if n, fatto, err := b.EscludiClienteDellaBusta(ctx, c.ID, chi); err != nil || fatto || n != 0 {
		t.Errorf("mai accesa: %d %v %v", n, fatto, err)
	}
	if conta(t, d, `SELECT count(*) FROM dettati_esclusioni`) != 0 || conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'assist.%'`) != 0 {
		t.Error("scritto con la raccolta mai accesa")
	}
	// Conta solo la chiave del lotto: l'interruttore da solo (scritto a mano o importato) non vuol dire «accesa una volta».
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.training.collect', '0', ?)`, istanteProva)
	if n, fatto, err := b.EscludiClienteDellaBusta(ctx, c.ID, chi); err != nil || fatto || n != 0 {
		t.Errorf("solo l'interruttore: %d %v %v", n, fatto, err)
	}
	if conta(t, d, `SELECT count(*) FROM dettati_esclusioni`) != 0 || conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'assist.%'`) != 0 {
		t.Error("scritto con il solo interruttore")
	}
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.training.remaining', '0', ?)`, istanteProva)
	if n, fatto, err := b.EscludiClienteDellaBusta(ctx, c.ID, chi); err != nil || !fatto || n != 0 {
		t.Errorf("spenta a lotto finito: %d %v %v", n, fatto, err)
	}
}

// inventario: RIP-S-26
// La busta della scheda: una che non c'è o nel cestino è ErrBustaNonTrovata e niente si scrive, come le altre azioni della
// scheda.
func TestRaccoltaEscludiClienteDallaSchedaBustaNonTrovata(t *testing.T) {
	b, d, _ := prova(t)
	accendi(t, b, "500")
	c := crea(t, b, conDettato("QR-U1"))
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, c.ID)
	for _, id := range []int64{c.ID, c.ID + 100, 0, -1} {
		if _, fatto, err := b.EscludiClienteDellaBusta(ctx, id, chi); !errors.Is(err, ErrBustaNonTrovata) || fatto {
			t.Errorf("busta %d: %v %v", id, fatto, err)
		}
	}
	if righeRaccolta(t, b) != 1 || conta(t, d, `SELECT count(*) FROM dettati_esclusioni`) != 0 {
		t.Error("scritto per una busta non trovata")
	}
}

// inventario: RIP-S-26
// Le gemelle (dubbio 1 della 114): due buste con la stessa etichetta e clienti diversi. Dalla pagina il numero è un rifiuto;
// dalla scheda la busta è quella aperta, e si esclude il suo cliente, non quello della gemella.
func TestRaccoltaEscludiClienteDallaSchedaGemelle(t *testing.T) {
	b, d, _ := prova(t)
	accendi(t, b, "500")
	una := bustaD109(t, b, "6/26", "RIP-2026-0070", "0041")
	gemella := bustaD109(t, b, "7/26", "RIP-2026-0070", "0042")
	if una.ClienteID == gemella.ClienteID {
		t.Fatal("clienti uguali")
	}
	if _, _, err := b.EscludiClienteDallaRaccolta(ctx, "0070/26", chi); !errors.Is(err, ErrPiuBuste) {
		t.Fatalf("pagina: %v", err)
	}
	if n, fatto, err := b.EscludiClienteDellaBusta(ctx, gemella.ID, chi); err != nil || !fatto || n != 1 {
		t.Fatalf("scheda: %d %v %v", n, fatto, err)
	}
	if righeDi(t, b, una.ID) != 1 || righeDi(t, b, gemella.ID) != 0 {
		t.Error("cancellato il dettato sbagliato")
	}
	if conta(t, d, `SELECT count(*) FROM dettati_esclusioni WHERE cliente_id = ?`, gemella.ClienteID) != 1 ||
		conta(t, d, `SELECT count(*) FROM dettati_esclusioni`) != 1 {
		t.Error("escluso il cliente sbagliato")
	}
}

// inventario: RIP-S-26
// Una busta senza etichetta «N. 45/26» e una con l'etichetta «0045/26» (domanda aperta della 115): dalla pagina ogni modo
// di scrivere il numero («45/26», «0045/26», «45») trova tutte e due ed è un rifiuto, e «N. 45/26» non è un numero; dalla
// scheda il cliente di ognuna si esclude.
func TestRaccoltaEscludiClienteDallaSchedaSenzaEtichetta(t *testing.T) {
	b, d, _ := prova(t)
	accendi(t, b, "500")
	conEtichetta := bustaD109(t, b, "9/26", "RIP-2026-0045", "0051")
	senza := bustaD109(t, b, "45/26", "SINT-115-SENZA", "0052")
	for _, scritto := range []string{"45/26", "0045/26", "45"} {
		if _, err := b.EscludiBustaDallaRaccolta(ctx, scritto, chi); !errors.Is(err, ErrPiuBuste) {
			t.Errorf("busta %q: %v", scritto, err)
		}
		if _, _, err := b.EscludiClienteDallaRaccolta(ctx, scritto, chi); !errors.Is(err, ErrPiuBuste) {
			t.Errorf("cliente %q: %v", scritto, err)
		}
	}
	if _, err := b.EscludiBustaDallaRaccolta(ctx, "N. 45/26", chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("N. 45/26: %v", err)
	}
	if n, fatto, err := b.EscludiClienteDellaBusta(ctx, senza.ID, chi); err != nil || !fatto || n != 1 {
		t.Fatalf("scheda senza etichetta: %d %v %v", n, fatto, err)
	}
	if righeDi(t, b, senza.ID) != 0 || righeDi(t, b, conEtichetta.ID) != 1 {
		t.Error("cancellato il dettato sbagliato")
	}
	if n, fatto, err := b.EscludiClienteDellaBusta(ctx, conEtichetta.ID, chi); err != nil || !fatto || n != 1 {
		t.Fatalf("scheda con etichetta: %d %v %v", n, fatto, err)
	}
	if conta(t, d, `SELECT count(*) FROM dettati_esclusioni`) != 2 {
		t.Error("esclusioni")
	}
}

// inventario: RIP-S-26
// Tutto o niente: se l'audit non si scrive, niente è cancellato e nessun cliente è escluso.
func TestRaccoltaEscludiClienteDallaSchedaTuttoONiente(t *testing.T) {
	b, d, _ := prova(t)
	accendi(t, b, "500")
	c := crea(t, b, conDettato("QR-V1"))
	esegui(t, d, `CREATE TRIGGER niente_audit BEFORE INSERT ON eventi_audit WHEN NEW.azione LIKE 'assist.training%' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if _, fatto, err := b.EscludiClienteDellaBusta(ctx, c.ID, chi); err == nil || fatto {
		t.Errorf("senza errore: %v", fatto)
	}
	if righeRaccolta(t, b) != 1 || conta(t, d, `SELECT count(*) FROM dettati_esclusioni`) != 0 {
		t.Error("cancellazione a metà")
	}
}

// inventario: RIP-S-26
// La scheda sa se la raccolta è stata accesa almeno una volta (la chiave del lotto c'è) e se il cliente è escluso.
func TestSchedaRaccolta(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, modulo())
	leggi := func() Scheda {
		t.Helper()
		s, err := b.Scheda(ctx, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	if s := leggi(); s.RaccoltaAvviata || s.ClienteEscluso {
		t.Errorf("mai accesa: %v %v", s.RaccoltaAvviata, s.ClienteEscluso)
	}
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.training.remaining', '0', ?)`, istanteProva)
	if s := leggi(); !s.RaccoltaAvviata || s.ClienteEscluso {
		t.Errorf("avviata: %v %v", s.RaccoltaAvviata, s.ClienteEscluso)
	}
	esegui(t, d, `INSERT INTO dettati_esclusioni (cliente_id, creato_il) VALUES (?, ?)`, c.ClienteID, istanteProva)
	if s := leggi(); !s.RaccoltaAvviata || !s.ClienteEscluso {
		t.Errorf("escluso: %v %v", s.RaccoltaAvviata, s.ClienteEscluso)
	}
}

// dettatoDel è una riga raccolta per la busta id, con l'istante dato.
func dettatoDel(t *testing.T, b *Buste, id int64, quando string) {
	t.Helper()
	esegui(t, b.d, `INSERT INTO dettati_addestramento (busta_id, testo, proposta, modello, creato_il) VALUES (?, 'dettato inventato', '{}', NULL, ?)`,
		id, quando)
}

// inventario: RIP-H-51
// Il limite è adesso meno 12 mesi del calendario, in UTC, come DateTime.shift(now, month: -12): il giorno che manca nel
// mese di un anno prima diventa l'ultimo del mese (29 febbraio → 28 febbraio), non il primo del mese dopo.
func TestLimiteDettati(t *testing.T) {
	for _, c := range []struct{ adesso, atteso string }{
		{"2027-11-15T10:00:00.000000Z", "2026-11-15T10:00:00.000000Z"},
		{"2028-02-29T10:00:00.000000Z", "2027-02-28T10:00:00.000000Z"},
		{"2027-03-31T23:30:00.123456Z", "2026-03-31T23:30:00.123456Z"},
		{"2027-01-01T00:00:00.000000Z", "2026-01-01T00:00:00.000000Z"},
	} {
		a, err := time.Parse(time.RFC3339Nano, c.adesso)
		if err != nil {
			t.Fatal(err)
		}
		if got := LimiteDettati(a).Format("2006-01-02T15:04:05.000000Z"); got != c.atteso {
			t.Errorf("%s: %s, atteso %s", c.adesso, got, c.atteso)
		}
	}
	// Un istante con i nanosecondi vale al microsecondo, come gli istanti salvati.
	a := time.Date(2027, 11, 15, 10, 0, 0, 999, time.UTC)
	if got := LimiteDettati(a); !got.Equal(time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("nanosecondi: %v", got)
	}
	// Un istante dato con l'ora del negozio vale come lo stesso istante in UTC, e il risultato è in UTC: il 1° marzo alle
	// 00:30 CET è il 28 febbraio alle 23:30 UTC; il 28 marzo 2027 alle 03:30 (ora legale, che nel 2026 inizia il 29) è
	// all'01:30 UTC, e un anno prima in negozio sarebbe un'ora dopo.
	for _, c := range []struct {
		adesso time.Time
		atteso string
	}{
		{time.Date(2027, 3, 1, 0, 30, 0, 0, clock.Roma), "2026-02-28T23:30:00.000000Z"},
		{time.Date(2027, 3, 28, 3, 30, 0, 0, clock.Roma), "2026-03-28T01:30:00.000000Z"},
	} {
		got := LimiteDettati(c.adesso)
		if got.Location() != time.UTC || got.Format("2006-01-02T15:04:05.000000Z") != c.atteso || !got.Equal(LimiteDettati(c.adesso.UTC())) {
			t.Errorf("%v: %v, atteso %s", c.adesso, got, c.atteso)
		}
	}
}

// inventario: RIP-H-51
// La scadenza: cancella solo i dettati raccolti prima del limite (appena sotto sì; al limite e appena sopra no), le buste
// restano; un audit assist.training_expired dell'attore «retention» con il numero, solo se ha cancellato qualcosa. Il
// secondo giro non trova niente e non scrive. Le esclusioni non si toccano.
func TestRaccoltaScadenzaDodiciMesi(t *testing.T) {
	b, d, o := prova(t)
	o.Imposta(time.Date(2027, 11, 15, 10, 0, 0, 0, time.UTC))
	vecchia := crea(t, b, modulo())
	limite := crea(t, b, modulo())
	giovane := crea(t, b, modulo())
	molto := crea(t, b, modulo())
	dettatoDel(t, b, vecchia.ID, "2026-11-15T09:59:59.999999Z")
	dettatoDel(t, b, limite.ID, "2026-11-15T10:00:00.000000Z")
	dettatoDel(t, b, giovane.ID, "2026-11-15T10:00:00.000001Z")
	dettatoDel(t, b, molto.ID, "2025-01-01T00:00:00.000000Z")
	esegui(t, d, `INSERT INTO dettati_esclusioni (cliente_id, creato_il) VALUES (?, '2020-01-01T00:00:00.000000Z')`, vecchia.ClienteID)

	n, err := b.ScadenzaDettati(ctx)
	if err != nil || n != 2 {
		t.Fatalf("scadenza: %d %v", n, err)
	}
	if righeDi(t, b, vecchia.ID) != 0 || righeDi(t, b, molto.ID) != 0 || righeDi(t, b, limite.ID) != 1 || righeDi(t, b, giovane.ID) != 1 {
		t.Error("cancellato il dettato sbagliato")
	}
	if conta(t, d, `SELECT count(*) FROM buste WHERE id IN (?, ?)`, vecchia.ID, molto.ID) != 2 {
		t.Error("buste sparite")
	}
	if conta(t, d, `SELECT count(*) FROM dettati_esclusioni`) != 1 {
		t.Error("esclusione toccata")
	}
	atteso := `assist.training_expired|assist|training|ok|{"cancellati":2}|retention`
	if got := azioniRaccolta(t, b); strings.Join(got, "\n") != atteso {
		t.Errorf("audit %v", got)
	}
	if n, err := b.ScadenzaDettati(ctx); err != nil || n != 0 {
		t.Errorf("secondo giro: %d %v", n, err)
	}
	if got := azioniRaccolta(t, b); len(got) != 1 {
		t.Errorf("audit dopo il secondo giro %v", got)
	}
	// Un microsecondo dopo, la riga al limite è oltre i 12 mesi. Serve un evento d'audit di quell'istante: il limite non va
	// oltre l'evento più recente meno 12 mesi (D-110 punto 3).
	o.Imposta(time.Date(2027, 11, 15, 10, 0, 0, 1000, time.UTC))
	crea(t, b, modulo())
	if n, err := b.ScadenzaDettati(ctx); err != nil || n != 1 || righeDi(t, b, limite.ID) != 0 || righeDi(t, b, giovane.ID) != 1 {
		t.Errorf("un microsecondo dopo: %d %v", n, err)
	}
}

// inventario: RIP-H-51
// Il 29 febbraio: il limite è il 28 febbraio dell'anno prima alla stessa ora; una riga del 1° marzo resta.
func TestRaccoltaScadenzaVentinoveFebbraio(t *testing.T) {
	b, _, o := prova(t)
	o.Imposta(time.Date(2028, 2, 29, 10, 0, 0, 0, time.UTC))
	prima := crea(t, b, modulo())
	limite := crea(t, b, modulo())
	marzo := crea(t, b, modulo())
	dettatoDel(t, b, prima.ID, "2027-02-28T09:59:59.999999Z")
	dettatoDel(t, b, limite.ID, "2027-02-28T10:00:00.000000Z")
	dettatoDel(t, b, marzo.ID, "2027-03-01T00:00:00.000000Z")
	if n, err := b.ScadenzaDettati(ctx); err != nil || n != 1 {
		t.Fatalf("scadenza: %d %v", n, err)
	}
	if righeDi(t, b, prima.ID) != 0 || righeDi(t, b, limite.ID) != 1 || righeDi(t, b, marzo.ID) != 1 {
		t.Error("cancellato il dettato sbagliato")
	}
}

// inventario: RIP-H-51
// Niente da cancellare (nessun dettato, o solo giovani): nessun audit.
func TestRaccoltaScadenzaNienteDaCancellare(t *testing.T) {
	b, d, o := prova(t)
	o.Imposta(time.Date(2027, 11, 15, 10, 0, 0, 0, time.UTC))
	if n, err := b.ScadenzaDettati(ctx); err != nil || n != 0 {
		t.Errorf("vuota: %d %v", n, err)
	}
	c := crea(t, b, modulo())
	dettatoDel(t, b, c.ID, "2027-11-15T09:00:00.000000Z")
	if n, err := b.ScadenzaDettati(ctx); err != nil || n != 0 {
		t.Errorf("giovane: %d %v", n, err)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.training_expired'`); n != 0 {
		t.Errorf("audit %d", n)
	}
	if righeDi(t, b, c.ID) != 1 {
		t.Error("cancellato un dettato giovane")
	}
}

// inventario: RIP-H-51
// Tutto o niente: se l'audit non si scrive, niente è cancellato e l'errore torna a chi chiama (la pulizia riprova): né il
// dettato vecchio né quello giovane della busta nel cestino (dalla slice 136 il giro cancella anche quello).
func TestRaccoltaScadenzaTuttoONiente(t *testing.T) {
	b, d, o := prova(t)
	o.Imposta(time.Date(2027, 11, 15, 10, 0, 0, 0, time.UTC))
	vecchia := crea(t, b, modulo())
	nelCestino := crea(t, b, modulo())
	dettatoDel(t, b, vecchia.ID, "2025-11-15T10:00:00.000000Z")
	dettatoDel(t, b, nelCestino.ID, "2027-11-01T10:00:00.000000Z")
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, nelCestino.ID)
	esegui(t, d, `CREATE TRIGGER niente_audit BEFORE INSERT ON eventi_audit WHEN NEW.azione LIKE 'assist.training%' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if _, err := b.ScadenzaDettati(ctx); err == nil {
		t.Error("senza errore")
	}
	if righeDi(t, b, vecchia.ID) != 1 || righeDi(t, b, nelCestino.ID) != 1 {
		t.Error("cancellazione senza audit")
	}
	esegui(t, d, `DROP TRIGGER niente_audit`)
	if n, err := b.ScadenzaDettati(ctx); err != nil || n != 2 || righeDi(t, b, nelCestino.ID) != 0 || righeDi(t, b, vecchia.ID) != 0 {
		t.Errorf("dopo: %d %v", n, err)
	}
}
