package buste

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
)

// Lo spostamento nel cestino (slice 136, Trash.discard/4 dello standalone): la busta e il suo dettato raccolto, l'audit
// repair.trashed, tutto o niente.

// inventario: RIP-S-49, RIP-H-51
// La busta va nel cestino adesso, il suo dettato raccolto se ne va nella stessa transazione (quello di un'altra busta
// resta), una riga repair.trashed dell'autore senza dati; la scheda non si apre più. Una seconda volta, un id che non
// c'è o non positivo: ErrBustaNonTrovata e niente si scrive.
func TestSpostaNelCestino(t *testing.T) {
	b, d, o := prova(t)
	adesso := time.Date(2026, 11, 28, 9, 30, 0, 123456000, time.UTC)
	o.Imposta(adesso)
	busta := crea(t, b, modulo())
	altra := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-ALTRA" }))
	dettatoDel(t, b, busta.ID, istanteProva)
	dettatoDel(t, b, altra.ID, istanteProva)
	adesso = adesso.Add(time.Minute) // aggiornato_il della creazione è un minuto prima: deve cambiare
	o.Imposta(adesso)
	n, err := b.SpostaNelCestino(ctx, busta.ID, chi)
	if err != nil || n != 1 {
		t.Fatalf("cestino: %d %v", n, err)
	}
	ora := adesso.Format(db.FormatoIstante)
	if got := valore(t, d, `SELECT cancellata_il FROM buste WHERE id = ?`, busta.ID); got != ora {
		t.Errorf("cancellata_il = %v, atteso %s", got, ora)
	}
	if got := valore(t, d, `SELECT aggiornato_il FROM buste WHERE id = ?`, busta.ID); got != ora {
		t.Errorf("aggiornato_il = %v, atteso %s", got, ora)
	}
	if righeDi(t, b, busta.ID) != 0 || righeDi(t, b, altra.ID) != 1 {
		t.Errorf("dettati: busta %d, altra %d", righeDi(t, b, busta.ID), righeDi(t, b, altra.ID))
	}
	if got := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.trashed' AND attore = ? AND risorsa = 'repair'
		AND id_risorsa = ? AND esito = 'ok' AND dati = '{}' AND ip = ?`, chi.Attore, itoa(busta.ID), chi.IP); got != 1 {
		t.Errorf("repair.trashed: %d righe", got)
	}
	if _, err := b.Scheda(ctx, busta.ID); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("scheda della busta nel cestino: %v", err)
	}
	if valore(t, d, `SELECT cancellata_il FROM buste WHERE id = ?`, altra.ID) != nil {
		t.Error("anche l'altra busta nel cestino")
	}
	prima := scatta(t, d)
	o.Imposta(adesso.Add(time.Hour))
	for _, id := range []int64{busta.ID, 0, -1, altra.ID + 100} {
		if n, err := b.SpostaNelCestino(ctx, id, chi); !errors.Is(err, ErrBustaNonTrovata) || n != 0 {
			t.Errorf("id %d: %d %v", id, n, err)
		}
	}
	uguali(t, "rifiuti", prima, scatta(t, d))
	if got := valore(t, d, `SELECT cancellata_il FROM buste WHERE id = ?`, busta.ID); got != ora {
		t.Errorf("la seconda volta ha cambiato la data del cestino: %v", got)
	}
}

// inventario: RIP-S-49
// Una busta senza dettato raccolto va nel cestino lo stesso (0 dettati).
func TestSpostaNelCestinoSenzaDettato(t *testing.T) {
	b, d, _ := prova(t)
	busta := crea(t, b, modulo())
	if n, err := b.SpostaNelCestino(ctx, busta.ID, chi); err != nil || n != 0 {
		t.Fatalf("cestino: %d %v", n, err)
	}
	if valore(t, d, `SELECT cancellata_il FROM buste WHERE id = ?`, busta.ID) == nil {
		t.Error("non nel cestino")
	}
}

// inventario: RIP-S-49
// Tutto o niente: senza autore dell'audit (lo schema rifiuta l'attore vuoto), con l'audit che non si scrive o un errore
// prima del commit, la busta resta fuori dal cestino e il dettato resta.
func TestSpostaNelCestinoTuttoONiente(t *testing.T) {
	b, d, _ := prova(t)
	busta := crea(t, b, modulo())
	dettatoDel(t, b, busta.ID, istanteProva)
	fuori := func(cosa string) {
		t.Helper()
		if valore(t, d, `SELECT cancellata_il FROM buste WHERE id = ?`, busta.ID) != nil || righeDi(t, b, busta.ID) != 1 ||
			conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.trashed'`) != 0 {
			t.Errorf("%s: scritto a metà", cosa)
		}
	}
	if _, err := b.SpostaNelCestino(ctx, busta.ID, audit.Contesto{}); err == nil || errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("senza autore: %v", err)
	}
	fuori("senza autore")
	esegui(t, d, `CREATE TRIGGER niente_audit BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.trashed' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if _, err := b.SpostaNelCestino(ctx, busta.ID, chi); err == nil {
		t.Error("audit guasto senza errore")
	}
	fuori("audit guasto")
	esegui(t, d, `DROP TRIGGER niente_audit`)
	guasto := errors.New("guasto prima del commit")
	b.primaDelCommit = func(*sql.Tx) error { return guasto }
	if _, err := b.SpostaNelCestino(ctx, busta.ID, chi); !errors.Is(err, guasto) {
		t.Errorf("prima del commit: %v", err)
	}
	fuori("prima del commit")
	b.primaDelCommit = nil
	if n, err := b.SpostaNelCestino(ctx, busta.ID, chi); err != nil || n != 1 {
		t.Errorf("dopo: %d %v", n, err)
	}
}

// inventario: RIP-S-50
// La busta spostata nel cestino sparisce da lista, conteggio, contatori, ricerca, campanella, briefing e link del
// cliente /r/; l'altra resta.
func TestSpostaNelCestinoSparisceDalleListe(t *testing.T) {
	b, d, o := prova(t)
	o.Imposta(time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC))
	busta := crea(t, b, con(modulo(), func(a *Accettazione) { a.NomeCliente, a.CodiceQR = "Cliente Cestino", "QR-CESTINO" }))
	altra := crea(t, b, con(modulo(), func(a *Accettazione) { a.NomeCliente, a.CodiceQR = "Cliente Resta", "QR-RESTA" }))
	for _, id := range []int64{busta.ID, altra.ID} {
		// pronta da avvisare (briefing, contatore «Pronte») e con un messaggio non letto (campanella, «Messaggi»)
		esegui(t, d, `UPDATE buste SET stato = 'pronta', data_pronta = '2026-11-27', ultimo_messaggio_il = ?, messaggi_letti_il = NULL
			WHERE id = ?`, istanteProva, id)
	}
	type visto struct {
		totale, pronte, messaggi, campanella, cercate, briefing int64
		pagina                                                  bool
	}
	guarda := func() visto {
		t.Helper()
		l, err := b.Lista(ctx, Filtri{Pagina: 1})
		if err != nil {
			t.Fatal(err)
		}
		c, err := b.LeggiCampanella(ctx)
		if err != nil {
			t.Fatal(err)
		}
		n, _, err := b.Selezione(ctx, Filtri{Cerca: "Cliente Cestino"}, 10)
		if err != nil {
			t.Fatal(err)
		}
		br, err := LeggiBriefing(ctx, d.Lettura, o.Adesso(), 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		_, err = b.PaginaCliente(ctx, busta.Token)
		if err != nil && !errors.Is(err, ErrBustaNonTrovata) {
			t.Fatal(err)
		}
		return visto{l.Totale, l.Contatori.Pronte, l.Contatori.Messaggi, c.Totale, n, int64(len(br.Pronte)), err == nil}
	}
	if v := guarda(); v != (visto{2, 2, 2, 2, 1, 2, true}) {
		t.Fatalf("prima: %+v", v)
	}
	if _, err := b.SpostaNelCestino(ctx, busta.ID, chi); err != nil {
		t.Fatal(err)
	}
	if v := guarda(); v != (visto{1, 1, 1, 1, 0, 1, false}) {
		t.Errorf("dopo: %+v", v)
	}
	if _, err := b.PaginaCliente(ctx, altra.Token); err != nil {
		t.Errorf("la pagina dell'altra busta: %v", err)
	}
}

// inventario: RIP-S-49
// La finestra del cestino della conferma: 30 senza riga o con un valore che Integer.parse/1 non prende per intero o
// fuori da 1–365 (Trash.Config.retention_days/0); la scheda la porta.
func TestGiorniCestino(t *testing.T) {
	b, d, _ := prova(t)
	busta := crea(t, b, modulo())
	if g, err := b.GiorniCestino(ctx); err != nil || g != 30 {
		t.Errorf("senza riga: %d %v", g, err)
	}
	for _, c := range []struct {
		v string
		g int64
	}{{"45", 45}, {"1", 1}, {"365", 365}, {"+7", 7}, {"0", 30}, {"366", 30}, {"-5", 30}, {" 45", 30}, {"45 ", 30},
		{"", 30}, {"4.5", 30}, {"abc", 30}, {"1_0", 30}, {"0x1f", 30}, {"99999999999999999999", 30}} {
		esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('trash.retention_days', ?, ?)
			ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore`, c.v, istanteProva)
		if g, err := b.GiorniCestino(ctx); err != nil || g != c.g {
			t.Errorf("%q: %d %v, attesi %d", c.v, g, err, c.g)
		}
	}
	esegui(t, d, `UPDATE impostazioni SET valore = '12' WHERE chiave = 'trash.retention_days'`)
	if s := apri(t, b, busta.ID); s.GiorniCestino != 12 {
		t.Errorf("scheda: %d", s.GiorniCestino)
	}
}

// inventario: RIP-H-51
// Il giro giornaliero cancella anche i dettati delle buste già nel cestino (le importate così: Training.expire/1), anche
// giovani; quelli delle buste fuori dal cestino con meno di 12 mesi restano. Un'unica riga assist.training_expired con
// il numero di tutti.
func TestRaccoltaScadenzaBusteNelCestino(t *testing.T) {
	b, d, o := prova(t)
	o.Imposta(time.Date(2027, 11, 15, 10, 0, 0, 0, time.UTC))
	vecchia := crea(t, b, modulo())
	nelCestino := crea(t, b, modulo())
	viva := crea(t, b, modulo())
	dettatoDel(t, b, vecchia.ID, "2025-11-15T10:00:00.000000Z")
	dettatoDel(t, b, nelCestino.ID, "2027-11-01T10:00:00.000000Z")
	dettatoDel(t, b, viva.ID, "2027-11-01T10:00:00.000000Z")
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, nelCestino.ID) // come l'import
	if n, err := b.ScadenzaDettati(ctx); err != nil || n != 2 {
		t.Fatalf("scadenza: %d %v", n, err)
	}
	if righeDi(t, b, vecchia.ID) != 0 || righeDi(t, b, nelCestino.ID) != 0 || righeDi(t, b, viva.ID) != 1 {
		t.Error("cancellati i dettati sbagliati")
	}
	if got := ultimoAudit(t, d, "assist.training_expired"); got != `assist.training_expired|assist|training|ok|{"cancellati":2}` {
		t.Errorf("audit: %s", got)
	}
}

// inventario: RIP-H-51
// D-110 e domanda L del piano T4.11: dopo un import senza backup verificato il giro non cancella nemmeno i dettati delle
// buste nel cestino (le importate), e non scrive niente; con il backup verificato dopo l'import sì. Con l'orologio
// andato avanti, lo stesso: il giro non cancella niente.
func TestRaccoltaScadenzaBusteNelCestinoBloccate(t *testing.T) {
	b, d, o := prova(t)
	inizio := time.Date(2027, 11, 15, 10, 0, 0, 0, time.UTC)
	o.Imposta(inizio)
	nelCestino := crea(t, b, modulo())
	dettatoDel(t, b, nelCestino.ID, "2027-11-01T10:00:00.000000Z")
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, nelCestino.ID)
	importato := time.Date(2026, 1, 10, 10, 0, 0, 0, time.UTC)
	esegui(t, d, `INSERT INTO import_standalone (id, modalita, sha256_sorgente, byte_sorgente, importato_il)
		VALUES (1, 'sintetico', zeroblob(32), 4096, ?)`, importato.Format(db.FormatoIstante))
	if n, err := b.ScadenzaDettati(ctx); err != nil || n != 0 || righeDi(t, b, nelCestino.ID) != 1 {
		t.Fatalf("dopo l'import: %d %v", n, err)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.training_expired'`) != 0 {
		t.Error("audit dopo l'import")
	}
	backupRegistrato(t, d, importato.Add(time.Hour))
	o.Imposta(inizio.Add(audit.MargineOrologio + time.Microsecond))
	if n, err := b.ScadenzaDettati(ctx); err != nil || n != 0 || righeDi(t, b, nelCestino.ID) != 1 {
		t.Fatalf("orologio avanti: %d %v", n, err)
	}
	o.Imposta(inizio)
	if n, err := b.ScadenzaDettati(ctx); err != nil || n != 1 || righeDi(t, b, nelCestino.ID) != 0 {
		t.Errorf("con il backup verificato: %d %v", n, err)
	}
}

// inventario: RIP-S-49, RIP-H-51
// Lo spostamento a mano cancella il dettato anche dopo un import senza backup: il blocco della D-110 vale per i lavori
// automatici, non per le azioni delle persone (come l'esclusione di un cliente, slice 114).
func TestSpostaNelCestinoDopoImport(t *testing.T) {
	b, d, _ := prova(t)
	busta := crea(t, b, modulo())
	dettatoDel(t, b, busta.ID, istanteProva)
	esegui(t, d, `INSERT INTO import_standalone (id, modalita, sha256_sorgente, byte_sorgente, importato_il)
		VALUES (1, 'sintetico', zeroblob(32), 4096, '2026-10-01T10:00:00.000000Z')`)
	if n, err := b.SpostaNelCestino(ctx, busta.ID, chi); err != nil || n != 1 || righeDi(t, b, busta.ID) != 0 {
		t.Errorf("dopo l'import: %d %v", n, err)
	}
}
