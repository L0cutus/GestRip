package buste

import (
	"errors"
	"regexp"
	"sync"
	"testing"
	"time"
)

// I lotti d'avviso alle oreficerie (slice 088, ORE-04 e ORE-05), come ReadyLots.open_lot/2,
// list_open_lots/0 e purge_stale_empty_lots/0 dello standalone: i casi di ready_lots_test.exs («opening a
// lot», «the stale purge») e di ready_lot_live_test.exs («the list page»). L'orologio è fermo al
// 31/10/2026 alle 9:00 UTC, le 10:00 in negozio (dal 25/10 l'ora è quella solare, UTC+1): la mezzanotte
// del negozio è il 30/10 alle 23:00 UTC (mezzanotteProva, consegne_test.go).

// lottoDel mette un lotto aperto dell'oreficeria con l'istante di creazione dato.
func lottoDel(t *testing.T, b *Buste, oreficeria int64, creato string) int64 {
	t.Helper()
	r, err := b.d.Scrittura.Exec(`INSERT INTO lotti_avviso (oreficeria_id, token, creato_il, aggiornato_il) VALUES (?, ?, ?, ?)`,
		oreficeria, "gettone-"+time.Now().Format("150405.000000000"), creato, creato)
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func esisteLotto(t *testing.T, b *Buste, id int64) bool {
	t.Helper()
	return conta(t, b.d, `SELECT count(*) FROM lotti_avviso WHERE id = ?`, id) == 1
}

// auditLotti sono le righe d'audit dei lotti: «attore|azione|id|dati».
func auditLotti(t *testing.T, b *Buste) []string {
	t.Helper()
	rr, err := b.d.Lettura.Query(`SELECT attore || '|' || azione || '|' || id_risorsa || '|' || dati FROM eventi_audit WHERE risorsa = 'ready_lot' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rr.Close()
	var out []string
	for rr.Next() {
		var s string
		if err := rr.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

var formaGettoneLotto = regexp.MustCompile(`\A[A-Za-z0-9_-]{32}\z`)

// inventario: ORE-05
// «Apri lotto» crea un lotto aperto dell'oreficeria con il gettone pubblico di 24 byte casuali (32 caratteri
// url-safe), l'istante di adesso e la riga ready_lot.opened con l'oreficeria; la seconda volta, lo stesso
// giorno, riprende lo stesso lotto senza un'altra riga. Un'altra oreficeria ha il suo.
func TestApriLottoNuovoERipreso(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Oreficeria Altra')`)
	id, err := b.ApriLotto(ctx, 2, chi)
	if err != nil {
		t.Fatal(err)
	}
	var oreficeria int64
	var gettone, creato, aggiornato string
	var inviato, chiuso, spento any
	if err := d.Lettura.QueryRow(`SELECT oreficeria_id, token, inviato_il, chiuso_il, accesso_pubblico_spento_il, creato_il, aggiornato_il FROM lotti_avviso WHERE id = ?`, id).
		Scan(&oreficeria, &gettone, &inviato, &chiuso, &spento, &creato, &aggiornato); err != nil {
		t.Fatal(err)
	}
	if oreficeria != 2 || !formaGettoneLotto.MatchString(gettone) || inviato != nil || chiuso != nil || spento != nil || creato != istanteProva || aggiornato != istanteProva {
		t.Fatalf("lotto: %d %q %v %v %v %s %s", oreficeria, gettone, inviato, chiuso, spento, creato, aggiornato)
	}
	if a := auditLotti(t, b); len(a) != 1 || a[0] != "titolare@esempio.test|ready_lot.opened|"+itoa(id)+`|{"oreficeria_id":2}` {
		t.Fatalf("audit: %v", a)
	}
	ancora, err := b.ApriLotto(ctx, 2, chi)
	if err != nil || ancora != id {
		t.Fatalf("ripresa: %d %v, atteso %d", ancora, err, id)
	}
	altra, err := b.ApriLotto(ctx, 3, chi)
	if err != nil || altra == id {
		t.Fatalf("altra oreficeria: %d %v", altra, err)
	}
	var altroGettone string
	if err := d.Lettura.QueryRow(`SELECT token FROM lotti_avviso WHERE id = ?`, altra).Scan(&altroGettone); err != nil {
		t.Fatal(err)
	}
	if altroGettone == gettone || !formaGettoneLotto.MatchString(altroGettone) {
		t.Fatalf("gettoni: %q %q", gettone, altroGettone)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'ready_lot.opened'`) != 2 {
		t.Fatal("una ripresa ha scritto una riga d'audit")
	}
}

// inventario: ORE-05
// La riga 1 (il negozio stesso) è «Non si può creare un avviso per l'oreficeria principale.», come ogni id
// che non è esterno (Oreficeria.is_b2b/1); un'oreficeria che non c'è «Oreficeria non trovata.». Niente lotti
// e niente audit.
func TestApriLottoRifiuti(t *testing.T) {
	b, d, _ := prova(t)
	for _, c := range []struct {
		id  int64
		err error
	}{
		{1, ErrLottoOreficeriaPrincipale},
		{0, ErrLottoOreficeriaPrincipale},
		{-4, ErrLottoOreficeriaPrincipale},
		{9999, ErrLottoOreficeriaNonTrovata},
	} {
		if _, err := b.ApriLotto(ctx, c.id, chi); !errors.Is(err, c.err) {
			t.Fatalf("%d: %v, atteso %v", c.id, err, c.err)
		}
	}
	if ErrLottoOreficeriaPrincipale.Error() != "Non si può creare un avviso per l'oreficeria principale." || ErrLottoOreficeriaNonTrovata.Error() != "Oreficeria non trovata." {
		t.Fatal("testi dei rifiuti")
	}
	if conta(t, d, `SELECT count(*) FROM lotti_avviso`) != 0 || len(auditLotti(t, b)) != 0 {
		t.Fatal("un rifiuto ha scritto")
	}
}

// inventario: ORE-05
// Un lotto inviato (o chiuso) non si riusa: se ne apre uno nuovo. Nemmeno un lotto aperto dei giorni prima,
// anche con delle buste: «oggi» è dalla mezzanotte del negozio. Fra due lotti aperti di oggi si riprende il
// più recente.
func TestApriLottoNonRiusaInviatiNeVecchi(t *testing.T) {
	b, d, _ := prova(t)
	inviato := lottoDel(t, b, 2, "2026-10-31T08:00:00.000000Z")
	esegui(t, d, `UPDATE lotti_avviso SET inviato_il = ? WHERE id = ?`, istanteProva, inviato)
	nuovo, err := b.ApriLotto(ctx, 2, chi)
	if err != nil || nuovo == inviato {
		t.Fatalf("dopo l'inviato: %d %v", nuovo, err)
	}
	esegui(t, d, `UPDATE lotti_avviso SET inviato_il = ?1, chiuso_il = ?1 WHERE id = ?2`, istanteProva, nuovo)
	terzo, err := b.ApriLotto(ctx, 2, chi)
	if err != nil || terzo == nuovo || terzo == inviato {
		t.Fatalf("dopo il chiuso: %d %v", terzo, err)
	}

	b, d, _ = prova(t)
	ieri := lottoDel(t, b, 2, "2026-10-30T22:59:59.999999Z") // le 23:59:59 del 30/10 in negozio
	esegui(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, ieri, prontaDiOreficeria(t, b))
	oggi, err := b.ApriLotto(ctx, 2, chi)
	if err != nil || oggi == ieri {
		t.Fatalf("lotto di ieri ripreso: %d %v", oggi, err)
	}

	b, _, _ = prova(t)
	lottoDel(t, b, 2, mezzanotteProva)
	recente := lottoDel(t, b, 2, "2026-10-31T07:00:00.000000Z")
	if id, err := b.ApriLotto(ctx, 2, chi); err != nil || id != recente {
		t.Fatalf("fra due di oggi: %d %v, atteso %d", id, err, recente)
	}
}

// inventario: ORE-05
// La mezzanotte è quella del negozio, non quella UTC: alle 00:30 di Roma (le 23:30 UTC del giorno prima) un
// lotto aperto alle 23:59 di Roma è di ieri, uno aperto alle 00:00 esatte è di oggi e si riprende.
func TestApriLottoMezzanotteDelNegozio(t *testing.T) {
	b, _, o := prova(t)
	o.Imposta(time.Date(2026, 10, 31, 23, 30, 0, 0, time.UTC)) // 01/11 00:30 in negozio
	ieri := lottoDel(t, b, 2, "2026-10-31T22:59:00.000000Z")
	id, err := b.ApriLotto(ctx, 2, chi)
	if err != nil || id == ieri {
		t.Fatalf("lotto delle 23:59: %d %v", id, err)
	}

	b, _, o = prova(t)
	o.Imposta(time.Date(2026, 10, 31, 23, 30, 0, 0, time.UTC))
	mezzanotte := lottoDel(t, b, 2, "2026-10-31T23:00:00.000000Z")
	if id, err := b.ApriLotto(ctx, 2, chi); err != nil || id != mezzanotte {
		t.Fatalf("lotto di mezzanotte: %d %v, atteso %d", id, err, mezzanotte)
	}
}

// inventario: ORE-05
// Il lotto e la sua riga d'audit stanno insieme: se l'audit non si scrive, il lotto non c'è.
func TestApriLottoAuditNellaTransazione(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `CREATE TRIGGER audit_rotto BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'ready_lot.opened' BEGIN SELECT RAISE(ABORT, 'rotto'); END`)
	if _, err := b.ApriLotto(ctx, 2, chi); err == nil {
		t.Fatal("aperto senza audit")
	}
	if conta(t, d, `SELECT count(*) FROM lotti_avviso`) != 0 {
		t.Fatal("lotto rimasto senza la sua riga d'audit")
	}
}

// D-94 (Stefano, 15/11/2026): due aperture nello stesso istante per la stessa oreficeria danno un lotto solo
// e una riga d'audit, perché la ricerca del lotto di oggi e la scrittura stanno nella stessa transazione di
// scrittura. Lo standalone (come il Flask) legge e poi scrive, e può crearne due (ORE-05, «come nel Flask»).
func TestApriLottoInsiemeUnoSolo(t *testing.T) {
	b, d, _ := prova(t)
	const quanti = 12
	ids := make([]int64, quanti)
	errs := make([]error, quanti)
	var via sync.WaitGroup
	partenza := make(chan struct{})
	for i := range quanti {
		via.Add(1)
		go func() {
			defer via.Done()
			<-partenza
			ids[i], errs[i] = b.ApriLotto(ctx, 2, chi)
		}()
	}
	close(partenza)
	via.Wait()
	for i := range quanti {
		if errs[i] != nil || ids[i] != ids[0] {
			t.Fatalf("apertura %d: %d %v, attesa %d", i, ids[i], errs[i], ids[0])
		}
	}
	if conta(t, d, `SELECT count(*) FROM lotti_avviso`) != 1 || conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'ready_lot.opened'`) != 1 {
		t.Fatal("due lotti aperti insieme")
	}
}

// inventario: ORE-04
// La pulizia cancella i lotti aperti dei giorni prima senza buste e dice quanti. Restano: quelli con una
// busta, anche nel cestino; quelli di oggi, anche aperti esattamente alla mezzanotte del negozio; quelli
// inviati o chiusi, anche vuoti. Una busta in nessun lotto non ferma la pulizia. Nessuna riga d'audit.
func TestPulisciLottiVuoti(t *testing.T) {
	b, d, _ := prova(t)
	prontaDiOreficeria(t, b) // una busta in nessun lotto
	vuotoIeri := lottoDel(t, b, 2, "2026-10-29T08:00:00.000000Z")
	vuotoPrimaDiMezzanotte := lottoDel(t, b, 2, "2026-10-30T22:59:59.999999Z")
	conBusta := lottoDel(t, b, 2, "2026-10-29T08:00:00.000000Z")
	esegui(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, conBusta, prontaDiOreficeria(t, b))
	nelCestino := lottoDel(t, b, 2, "2026-10-29T08:00:00.000000Z")
	cestinata := prontaDiOreficeria(t, b)
	esegui(t, d, `UPDATE buste SET lotto_avviso_id = ?, cancellata_il = ? WHERE id = ?`, nelCestino, istanteProva, cestinata)
	inviatoVuoto := lottoDel(t, b, 2, "2026-10-29T08:00:00.000000Z")
	esegui(t, d, `UPDATE lotti_avviso SET inviato_il = ? WHERE id = ?`, istanteProva, inviatoVuoto)
	mezzanotte := lottoDel(t, b, 2, mezzanotteProva)
	oggi := lottoDel(t, b, 2, istanteProva)
	n, err := b.PulisciLottiVuoti(ctx)
	if err != nil || n != 2 {
		t.Fatalf("pulizia: %d %v", n, err)
	}
	if esisteLotto(t, b, vuotoIeri) || esisteLotto(t, b, vuotoPrimaDiMezzanotte) {
		t.Fatal("restano i lotti vuoti dei giorni prima")
	}
	for _, id := range []int64{conBusta, nelCestino, inviatoVuoto, mezzanotte, oggi} {
		if !esisteLotto(t, b, id) {
			t.Fatalf("cancellato il lotto %d", id)
		}
	}
	if len(auditLotti(t, b)) != 0 {
		t.Fatal("la pulizia ha scritto l'audit")
	}
	if n, err := b.PulisciLottiVuoti(ctx); err != nil || n != 0 {
		t.Fatalf("seconda pulizia: %d %v", n, err)
	}
}

// inventario: ORE-04
// I lotti aperti di oggi, dal più recente, con l'oreficeria, le buste fuori dal cestino e l'istante
// d'apertura. Non ci sono quelli inviati o chiusi né quelli dei giorni prima, anche con delle buste (il lotto
// aperto ieri e non vuoto sparisce dalla lista, come nello standalone: Stefano, 15/11/2026). La mezzanotte
// del negozio è di oggi.
func TestLottiApertiOggi(t *testing.T) {
	b, d, _ := prova(t)
	if ll, err := b.LottiApertiOggi(ctx); err != nil || len(ll) != 0 {
		t.Fatalf("vuoto: %v %v", ll, err)
	}
	esegui(t, d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Alfa <Oro>')`)
	ieri := lottoDel(t, b, 2, "2026-10-30T22:59:59.999999Z")
	esegui(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, ieri, prontaDiOreficeria(t, b))
	mezzanotte := lottoDel(t, b, 3, mezzanotteProva)
	inviato := lottoDel(t, b, 2, "2026-10-31T08:00:00.000000Z")
	esegui(t, d, `UPDATE lotti_avviso SET inviato_il = ? WHERE id = ?`, istanteProva, inviato)
	recente := lottoDel(t, b, 2, "2026-10-31T08:05:00.000000Z")
	for range 2 {
		esegui(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, recente, prontaDiOreficeria(t, b))
	}
	cestinata := prontaDiOreficeria(t, b)
	esegui(t, d, `UPDATE buste SET lotto_avviso_id = ?, cancellata_il = ? WHERE id = ?`, recente, istanteProva, cestinata)
	ll, err := b.LottiApertiOggi(ctx)
	if err != nil {
		t.Fatal(err)
	}
	atteso := []LottoAperto{
		{ID: recente, Oreficeria: "Oreficeria Esempio", Buste: 2, Aperto: time.Date(2026, 10, 31, 8, 5, 0, 0, time.UTC)},
		{ID: mezzanotte, Oreficeria: "Alfa <Oro>", Buste: 0, Aperto: time.Date(2026, 10, 30, 23, 0, 0, 0, time.UTC)},
	}
	if len(ll) != len(atteso) {
		t.Fatalf("lotti: %+v", ll)
	}
	for i := range atteso {
		if ll[i].ID != atteso[i].ID || ll[i].Oreficeria != atteso[i].Oreficeria || ll[i].Buste != atteso[i].Buste || !ll[i].Aperto.Equal(atteso[i].Aperto) {
			t.Fatalf("lotto %d: %+v, atteso %+v", i, ll[i], atteso[i])
		}
	}
}
