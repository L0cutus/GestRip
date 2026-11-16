package buste

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
)

// Le buste di un lotto d'avviso (slice 089, ORE-06, ORE-08, ORE-09), come ReadyLots.add_bag/3, remove_bag/3,
// candidates/1, members/1 e fetch_lot/1 dello standalone: i casi di ready_lots_test.exs («adding a bag»,
// «removing a bag», «the candidate list»), più le gare fra aggiunte e con l'avviso singolo. Le buste sono
// dell'oreficeria 2 di prova (con un telefono, così l'avviso singolo parte), accettate il 31/10/2026: i numeri
// automatici sono 1/26, 2/26, …, senza etichetta.

// prontaPerLotto è una busta pronta dell'oreficeria 2 con il codice dato ("" per nessuno).
func prontaPerLotto(t *testing.T, b *Buste, codice string) int64 {
	t.Helper()
	c := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID, a.CodiceQR = "2", codice }))
	if _, err := b.CambiaStato(ctx, c.ID, "pronta", chi); err != nil {
		t.Fatal(err)
	}
	return c.ID
}

// lottoDi è il lotto della busta (0: nessuno).
func lottoDi(t *testing.T, b *Buste, busta int64) int64 {
	t.Helper()
	if v := valore(t, b.d, `SELECT lotto_avviso_id FROM buste WHERE id = ?`, busta); v != nil {
		return v.(int64)
	}
	return 0
}

// righeAudit sono le righe di quell'azione: «id_risorsa|dati».
func righeAudit(t *testing.T, b *Buste, azione string) []string {
	t.Helper()
	rr, err := b.d.Lettura.Query(`SELECT id_risorsa || '|' || dati FROM eventi_audit WHERE azione = ? ORDER BY id`, azione)
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

func idDelLotto(x []BustaDelLotto) []int64 {
	var out []int64
	for _, y := range x {
		out = append(out, y.ID)
	}
	return out
}

func aggiungiPerID(b *Buste, lotto, busta int64) (BustaDelLotto, error) {
	return b.AggiungiAlLotto(ctx, lotto, RichiestaLotto{ID: itoa(busta)}, chi)
}

// inventario: ORE-06
// Per id, per numero scritto (anche con gli zeri davanti, «02/26» è «2/26») e per QR: la busta entra nel lotto,
// con l'istante di adesso, e ogni aggiunta scrive ready_lot.bag_added con il lotto e la busta. Spazi ai bordi
// tolti. Il QR vince sul numero e il numero sull'id.
func TestAggiungiAlLottoPerIDNumeroQR(t *testing.T) {
	b, d, _ := prova(t)
	l := lottoDel(t, b, 2, istanteProva)
	uno, due, tre := prontaPerLotto(t, b, ""), prontaPerLotto(t, b, ""), prontaPerLotto(t, b, "QR-TRE")
	esegui(t, d, `UPDATE buste SET aggiornato_il = '2026-01-01T00:00:00.000000Z'`)
	x, err := b.AggiungiAlLotto(ctx, l, RichiestaLotto{ID: " " + itoa(uno) + " "}, chi)
	if err != nil || x.ID != uno || x.Numero != "1/26" || x.Descrizione != "Anello da stringere" || x.GiaAvvisata {
		t.Fatalf("per id: %+v %v", x, err)
	}
	if x, err := b.AggiungiAlLotto(ctx, l, RichiestaLotto{Numero: " 02/26 "}, chi); err != nil || x.ID != due {
		t.Fatalf("per numero: %+v %v", x, err)
	}
	if x, err := b.AggiungiAlLotto(ctx, l, RichiestaLotto{QR: " QR-TRE ", Numero: "99/26", ID: "abc"}, chi); err != nil || x.ID != tre || x.Visibile().Titolo() != "N. 3/26" {
		t.Fatalf("per QR: %+v %v", x, err)
	}
	for _, id := range []int64{uno, due, tre} {
		if lottoDi(t, b, id) != l || valore(t, d, `SELECT aggiornato_il FROM buste WHERE id = ?`, id) != istanteProva {
			t.Fatalf("busta %d non nel lotto", id)
		}
	}
	atteso := []string{itoa(l) + `|{"repair_id":` + itoa(uno) + `}`, itoa(l) + `|{"repair_id":` + itoa(due) + `}`, itoa(l) + `|{"repair_id":` + itoa(tre) + `}`}
	if a := righeAudit(t, b, "ready_lot.bag_added"); strings.Join(a, " ") != strings.Join(atteso, " ") {
		t.Fatalf("audit: %v", a)
	}
	if v := valore(t, d, `SELECT attore FROM eventi_audit WHERE azione = 'ready_lot.bag_added' LIMIT 1`); v != chi.Attore {
		t.Fatalf("attore: %v", v)
	}
	// Il numero vince sull'id.
	quattro, cinque := prontaPerLotto(t, b, ""), prontaPerLotto(t, b, "")
	if x, err := b.AggiungiAlLotto(ctx, l, RichiestaLotto{Numero: "4/26", ID: itoa(cinque)}, chi); err != nil || x.ID != quattro || lottoDi(t, b, cinque) != 0 {
		t.Fatalf("numero e id: %+v %v", x, err)
	}
}

// inventario: ORE-06
// I rifiuti senza busta: tre campi vuoti (o di soli spazi), un numero, un id o un QR che non trovano niente, un
// id che non è un numero, una busta nel cestino; un QR scritto che non trova niente non passa al numero; il lotto
// che non c'è viene prima di tutto. Niente si scrive.
func TestAggiungiAlLottoRifiutiSenzaBusta(t *testing.T) {
	b, d, _ := prova(t)
	l := lottoDel(t, b, 2, istanteProva)
	uno := prontaPerLotto(t, b, "QR-UNO")
	cestino := prontaPerLotto(t, b, "QR-CESTINO")
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestino)
	prima := scatta(t, d)
	for _, c := range []struct {
		q     RichiestaLotto
		lotto int64
		err   error
	}{
		{RichiestaLotto{}, l, ErrLottoRichiestaVuota},
		{RichiestaLotto{QR: "  ", Numero: " ", ID: "\t"}, l, ErrLottoRichiestaVuota},
		{RichiestaLotto{Numero: "99/26"}, l, ErrLottoBustaNonTrovata},
		{RichiestaLotto{ID: "x"}, l, ErrLottoBustaNonTrovata},
		{RichiestaLotto{ID: "99999"}, l, ErrLottoBustaNonTrovata},
		{RichiestaLotto{ID: "-1"}, l, ErrLottoBustaNonTrovata},
		{RichiestaLotto{QR: "QR-NESSUNO"}, l, ErrLottoBustaNonTrovata},
		{RichiestaLotto{QR: "QR-NESSUNO", Numero: "1/26", ID: itoa(uno)}, l, ErrLottoBustaNonTrovata},
		{RichiestaLotto{ID: itoa(cestino)}, l, ErrLottoBustaNonTrovata},
		{RichiestaLotto{QR: "QR-CESTINO"}, l, ErrLottoBustaNonTrovata},
		{RichiestaLotto{Numero: "2/26"}, l, ErrLottoBustaNonTrovata},
		{RichiestaLotto{ID: itoa(uno)}, 9999, ErrLottoNonDisponibile},
		{RichiestaLotto{}, 9999, ErrLottoNonDisponibile},
	} {
		if _, err := b.AggiungiAlLotto(ctx, c.lotto, c.q, chi); !errors.Is(err, c.err) {
			t.Errorf("%+v nel lotto %d: %v, atteso %v", c.q, c.lotto, err, c.err)
		}
	}
	uguali(t, "rifiuti", prima, scatta(t, d))
	if lottoDi(t, b, uno) != 0 {
		t.Fatal("busta nel lotto dopo un rifiuto")
	}
}

// inventario: ORE-06
// Una busta già avvisata singolarmente è rifiutata da ogni porta dell'aggiunta (id, numero, QR) e il lotto resta
// vuoto; una di un'altra oreficeria, una diretta del negozio, una non pronta e una scheda compro oro valutata hanno
// ognuna il suo rifiuto. Niente si scrive.
func TestAggiungiAlLottoRifiutiDellaBusta(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Oreficeria Altra')`)
	l := lottoDel(t, b, 2, istanteProva)
	avvisata := prontaPerLotto(t, b, "QR-AVVISATA")
	if _, err := b.AnnunciaRitiro(ctx, avvisata, baseProva, chi); err != nil {
		t.Fatal(err)
	}
	altra := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = "3" })).ID
	if _, err := b.CambiaStato(ctx, altra, "pronta", chi); err != nil {
		t.Fatal(err)
	}
	diretta := crea(t, b, modulo()).ID
	if _, err := b.CambiaStato(ctx, diretta, "pronta", chi); err != nil {
		t.Fatal(err)
	}
	inLavorazione := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = "2" })).ID
	scheda := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID, a.CompraOro = "2", "1" })).ID
	if _, err := b.CambiaStato(ctx, scheda, "valutata", chi); err != nil {
		t.Fatal(err)
	}
	prima := scatta(t, d)
	for _, c := range []struct {
		q   RichiestaLotto
		err error
	}{
		{RichiestaLotto{ID: itoa(avvisata)}, ErrLottoGiaAvvisata},
		{RichiestaLotto{Numero: "1/26"}, ErrLottoGiaAvvisata},
		{RichiestaLotto{QR: "QR-AVVISATA"}, ErrLottoGiaAvvisata},
		{RichiestaLotto{ID: itoa(altra)}, ErrLottoAltraOreficeria},
		{RichiestaLotto{ID: itoa(diretta)}, ErrLottoAltraOreficeria},
		{RichiestaLotto{ID: itoa(inLavorazione)}, ErrLottoNonPronta},
		{RichiestaLotto{ID: itoa(scheda)}, ErrLottoCompraOro},
	} {
		if _, err := b.AggiungiAlLotto(ctx, l, c.q, chi); !errors.Is(err, c.err) {
			t.Errorf("%+v: %v, atteso %v", c.q, err, c.err)
		}
	}
	uguali(t, "rifiuti", prima, scatta(t, d))
	if conta(t, d, `SELECT count(*) FROM buste WHERE lotto_avviso_id IS NOT NULL`) != 0 {
		t.Fatal("una busta è entrata nel lotto")
	}
	// I testi della pagina.
	for err, testo := range map[error]string{
		ErrLottoGiaAvvisata: "Busta già avvisata singolarmente: azzera l'avviso dal dettaglio, poi riprova.", ErrLottoAltraOreficeria: "Busta di altra oreficeria.",
		ErrLottoNonPronta: "Busta non pronta.", ErrLottoCompraOro: "Le schede compro oro non entrano in un lotto.", ErrLottoNonDisponibile: "Lotto non disponibile.",
		ErrLottoRichiestaVuota: "Numero busta, QR o id richiesto.", ErrLottoBustaNonTrovata: "Riparazione non trovata.", ErrGiaInQuestoLotto: "Busta già in questo lotto.",
		ErrNonInQuestoLotto: "Busta non in questo lotto.", ErrLottoNonTrovato: "Lotto non trovato.",
	} {
		if err.Error() != testo {
			t.Errorf("testo %q, atteso %q", err.Error(), testo)
		}
	}
}

// inventario: ORE-06
// La stessa busta due volte: la seconda è «già in questo lotto», senza un'altra riga d'audit.
func TestAggiungiAlLottoDueVolte(t *testing.T) {
	b, _, _ := prova(t)
	l := lottoDel(t, b, 2, istanteProva)
	uno := prontaPerLotto(t, b, "")
	if _, err := aggiungiPerID(b, l, uno); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AggiungiAlLotto(ctx, l, RichiestaLotto{Numero: "1/26"}, chi); !errors.Is(err, ErrGiaInQuestoLotto) {
		t.Fatalf("seconda volta: %v", err)
	}
	if a := righeAudit(t, b, "ready_lot.bag_added"); len(a) != 1 {
		t.Fatalf("audit: %v", a)
	}
}

// inventario: ORE-06
// Una busta in un altro lotto: ancora aperto, «Busta già nel lotto #N: toglila da lì, poi riprova.» (prima di
// «altra oreficeria»); uscito, «Busta già uscita col lotto #N: resta sua. …», e quel lotto non la lascia togliere.
func TestAggiungiAlLottoInAltroLotto(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Oreficeria Altra')`)
	primo := lottoDel(t, b, 2, istanteProva)
	uno := prontaPerLotto(t, b, "")
	if _, err := aggiungiPerID(b, primo, uno); err != nil {
		t.Fatal(err)
	}
	altrove := lottoDel(t, b, 3, istanteProva)
	_, err := aggiungiPerID(b, altrove, uno)
	var a *InAltroLotto
	if !errors.As(err, &a) || a.Lotto != primo || a.Inviato || err.Error() != "Busta già nel lotto #"+itoa(primo)+": toglila da lì, poi riprova." {
		t.Fatalf("lotto aperto: %v", err)
	}
	esegui(t, d, `UPDATE lotti_avviso SET inviato_il = ? WHERE id = ?`, istanteProva, primo)
	_, err = aggiungiPerID(b, altrove, uno)
	if !errors.As(err, &a) || a.Lotto != primo || !a.Inviato ||
		err.Error() != "Busta già uscita col lotto #"+itoa(primo)+": resta sua. Per riavvisare il cliente usa l'avviso singolo dal dettaglio." {
		t.Fatalf("lotto uscito: %v", err)
	}
	if _, err := b.TogliDalLotto(ctx, primo, uno, chi); !errors.Is(err, ErrLottoNonDisponibile) {
		t.Fatalf("togli dal lotto uscito: %v", err)
	}
	if lottoDi(t, b, uno) != primo {
		t.Fatal("la busta ha lasciato il lotto uscito")
	}
}

// inventario: ORE-06
// Avvisata e in un lotto uscito: vince «già avvisata» (di quelle due cose solo l'avviso si può disfare); azzerato
// l'avviso, il rifiuto è quello del lotto uscito.
func TestAggiungiAlLottoAvvisataVinceSulLottoUscito(t *testing.T) {
	b, d, _ := prova(t)
	primo := lottoDel(t, b, 2, istanteProva)
	uno := prontaPerLotto(t, b, "")
	if _, err := aggiungiPerID(b, primo, uno); err != nil {
		t.Fatal(err)
	}
	// Come l'invio del lotto (091): il lotto esce e la busta risulta avvisata.
	esegui(t, d, `UPDATE lotti_avviso SET inviato_il = ? WHERE id = ?`, istanteProva, primo)
	esegui(t, d, `UPDATE buste SET avviso_pronta_il = ? WHERE id = ?`, istanteProva, uno)
	secondo := lottoDel(t, b, 2, istanteProva)
	if _, err := aggiungiPerID(b, secondo, uno); !errors.Is(err, ErrLottoGiaAvvisata) {
		t.Fatalf("avvisata e uscita: %v", err)
	}
	if err := b.AzzeraAvviso(ctx, uno, chi); err != nil {
		t.Fatal(err)
	}
	var a *InAltroLotto
	if _, err := aggiungiPerID(b, secondo, uno); !errors.As(err, &a) || a.Lotto != primo || !a.Inviato {
		t.Fatalf("dopo l'azzeramento: %v", err)
	}
}

// inventario: ORE-06
// Un lotto inviato o chiuso (e uno che non c'è) non prende niente.
func TestAggiungiAlLottoUscitoOChiuso(t *testing.T) {
	b, d, _ := prova(t)
	l := lottoDel(t, b, 2, istanteProva)
	uno := prontaPerLotto(t, b, "")
	esegui(t, d, `UPDATE lotti_avviso SET inviato_il = ? WHERE id = ?`, istanteProva, l)
	if _, err := aggiungiPerID(b, l, uno); !errors.Is(err, ErrLottoNonDisponibile) {
		t.Fatalf("inviato: %v", err)
	}
	esegui(t, d, `UPDATE lotti_avviso SET chiuso_il = ? WHERE id = ?`, istanteProva, l)
	if _, err := aggiungiPerID(b, l, uno); !errors.Is(err, ErrLottoNonDisponibile) {
		t.Fatalf("chiuso: %v", err)
	}
	if lottoDi(t, b, uno) != 0 || len(righeAudit(t, b, "ready_lot.bag_added")) != 0 {
		t.Fatal("aggiunta a un lotto uscito")
	}
}

// inventario: ORE-06
// La scrittura decide: quello che un'altra richiesta scrive fra la lettura della busta e l'UPDATE (l'avviso
// singolo, un altro lotto, lo stato, l'oreficeria, il cestino, il lotto inviato) ferma l'aggiunta con il suo
// rifiuto, senza membro né audit. Con la stessa busta e senza niente in mezzo, entra.
func TestAggiungiAlLottoDecideLaScrittura(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Oreficeria Altra')`)
	for _, c := range []struct {
		nome string
		q    string
		err  error
	}{
		{"avviso singolo", `UPDATE buste SET avviso_pronta_il = '` + istanteProva + `' WHERE id = ?`, ErrLottoGiaAvvisata},
		{"stato", `UPDATE buste SET stato = 'in_lavorazione' WHERE id = ?`, ErrLottoNonPronta},
		{"oreficeria", `UPDATE buste SET oreficeria_id = 3 WHERE id = ?`, ErrLottoAltraOreficeria},
		{"cestino", `UPDATE buste SET cancellata_il = '` + istanteProva + `' WHERE id = ?`, ErrLottoBustaNonTrovata},
		{"lotto inviato", `UPDATE lotti_avviso SET inviato_il = '` + istanteProva + `' WHERE id = (SELECT max(id) FROM lotti_avviso) AND ? > 0`, ErrLottoNonDisponibile},
		{"lotto chiuso", `UPDATE lotti_avviso SET inviato_il = '` + istanteProva + `', chiuso_il = '` + istanteProva + `' WHERE id = (SELECT max(id) FROM lotti_avviso) AND ? > 0`, ErrLottoNonDisponibile},
		{"compro oro", `UPDATE buste SET compro_oro = 1, stato = 'valutata' WHERE id = ?`, ErrLottoCompraOro},
	} {
		l := lottoDel(t, b, 2, istanteProva)
		uno := prontaPerLotto(t, b, "")
		b.primaDelLotto = func(tx *sql.Tx) error {
			_, err := tx.Exec(c.q, uno)
			return err
		}
		_, err := aggiungiPerID(b, l, uno)
		b.primaDelLotto = nil
		if !errors.Is(err, c.err) {
			t.Errorf("%s: %v, atteso %v", c.nome, err, c.err)
		}
		if lottoDi(t, b, uno) != 0 {
			t.Errorf("%s: la busta è entrata", c.nome)
		}
	}
	// Una busta in un altro lotto un attimo prima.
	l := lottoDel(t, b, 2, istanteProva)
	altro := lottoDel(t, b, 2, istanteProva)
	uno := prontaPerLotto(t, b, "")
	b.primaDelLotto = func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, altro, uno)
		return err
	}
	_, err := aggiungiPerID(b, l, uno)
	b.primaDelLotto = nil
	var a *InAltroLotto
	if !errors.As(err, &a) || a.Lotto != altro || lottoDi(t, b, uno) != 0 { // il rifiuto annulla anche la scrittura del gancio
		t.Fatalf("altro lotto: %v", err)
	}
	if n := len(righeAudit(t, b, "ready_lot.bag_added")); n != 0 {
		t.Fatalf("audit di aggiunte rifiutate: %d", n)
	}
	due := prontaPerLotto(t, b, "")
	if _, err := aggiungiPerID(b, l, due); err != nil || lottoDi(t, b, due) != l {
		t.Fatalf("senza niente in mezzo: %v", err)
	}
}

// inventario: ORE-06
// Due (dodici) aggiunte della stessa busta insieme: un membro, una riga d'audit, le altre «già in questo lotto».
func TestAggiungiAlLottoInsiemeUnaSola(t *testing.T) {
	b, _, _ := prova(t)
	l := lottoDel(t, b, 2, istanteProva)
	uno := prontaPerLotto(t, b, "")
	const quanti = 12
	errs := make([]error, quanti)
	var via sync.WaitGroup
	partenza := make(chan struct{})
	for i := range quanti {
		via.Add(1)
		go func() {
			defer via.Done()
			<-partenza
			_, errs[i] = aggiungiPerID(b, l, uno)
		}()
	}
	close(partenza)
	via.Wait()
	ok, gia := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrGiaInQuestoLotto):
			gia++
		default:
			t.Errorf("aggiunta: %v", err)
		}
	}
	if ok != 1 || gia != quanti-1 || len(righeAudit(t, b, "ready_lot.bag_added")) != 1 || lottoDi(t, b, uno) != l {
		t.Fatalf("riuscite %d, già nel lotto %d", ok, gia)
	}
}

// inventario: ORE-06
// La stessa busta in due lotti insieme: entra in uno solo, gli altri dicono in quale; una riga d'audit.
func TestAggiungiAlLottoInsiemeDueLotti(t *testing.T) {
	b, _, _ := prova(t)
	lotti := []int64{lottoDel(t, b, 2, istanteProva), lottoDel(t, b, 2, istanteProva)}
	uno := prontaPerLotto(t, b, "")
	const quanti = 12
	errs := make([]error, quanti)
	var via sync.WaitGroup
	partenza := make(chan struct{})
	for i := range quanti {
		via.Add(1)
		go func() {
			defer via.Done()
			<-partenza
			_, errs[i] = aggiungiPerID(b, lotti[i%2], uno)
		}()
	}
	close(partenza)
	via.Wait()
	vinto := lottoDi(t, b, uno)
	ok := 0
	for i, err := range errs {
		var a *InAltroLotto
		switch {
		case err == nil:
			ok++
		case lotti[i%2] == vinto && errors.Is(err, ErrGiaInQuestoLotto):
		case lotti[i%2] != vinto && errors.As(err, &a) && a.Lotto == vinto && !a.Inviato:
		default:
			t.Errorf("aggiunta %d al lotto %d (vinto %d): %v", i, lotti[i%2], vinto, err)
		}
	}
	if ok != 1 || vinto == 0 || len(righeAudit(t, b, "ready_lot.bag_added")) != 1 {
		t.Fatalf("riuscite %d, lotto %d", ok, vinto)
	}
}

// inventario: ORE-06
// L'aggiunta e l'avviso singolo della stessa busta insieme, su più buste: per ognuna vince uno solo dei due. Mai una
// busta in un lotto aperto e avvisata, mai una busta senza né l'uno né l'altro; chi perde ha il suo rifiuto
// («già avvisata» per il lotto, «in un lotto aperto» per l'avviso).
func TestAggiungiAlLottoEAvvisoSingoloInsieme(t *testing.T) {
	b, d, _ := prova(t)
	l := lottoDel(t, b, 2, istanteProva)
	const quante = 8
	ids := make([]int64, quante)
	for i := range ids {
		ids[i] = prontaPerLotto(t, b, "")
	}
	errLotto := make([]error, quante)
	errAvviso := make([]error, quante)
	var via sync.WaitGroup
	partenza := make(chan struct{})
	for i := range quante {
		via.Add(2)
		go func() {
			defer via.Done()
			<-partenza
			_, errLotto[i] = aggiungiPerID(b, l, ids[i])
		}()
		go func() {
			defer via.Done()
			<-partenza
			_, errAvviso[i] = b.AnnunciaRitiro(ctx, ids[i], baseProva, chi)
		}()
	}
	close(partenza)
	via.Wait()
	for i, id := range ids {
		nelLotto := lottoDi(t, b, id) == l
		avvisata := valore(t, d, `SELECT avviso_pronta_il FROM buste WHERE id = ?`, id) != nil
		switch {
		case nelLotto == avvisata:
			t.Errorf("busta %d: nel lotto %v, avvisata %v", id, nelLotto, avvisata)
		case nelLotto && (errLotto[i] != nil || !errors.Is(errAvviso[i], ErrInLottoAperto)):
			t.Errorf("busta %d nel lotto: %v / %v", id, errLotto[i], errAvviso[i])
		case avvisata && (errAvviso[i] != nil || !errors.Is(errLotto[i], ErrLottoGiaAvvisata)):
			t.Errorf("busta %d avvisata: %v / %v", id, errLotto[i], errAvviso[i])
		}
	}
	if n, m := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'ready_lot.bag_added'`), conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'ready_notice.sent'`); n+m != quante {
		t.Fatalf("righe d'audit: %d aggiunte, %d avvisi", n, m)
	}
}

// inventario: ORE-06
// Una busta in un lotto aperto non riceve l'avviso singolo, e niente si scrive.
func TestAvvisoSingoloRifiutaLaBustaDelLotto(t *testing.T) {
	b, d, _ := prova(t)
	l := lottoDel(t, b, 2, istanteProva)
	uno := prontaPerLotto(t, b, "")
	if _, err := aggiungiPerID(b, l, uno); err != nil {
		t.Fatal(err)
	}
	prima := statoAvvisi(t, b)
	if _, err := b.AnnunciaRitiro(ctx, uno, baseProva, chi); !errors.Is(err, ErrInLottoAperto) {
		t.Fatalf("avviso: %v", err)
	}
	if valore(t, d, `SELECT avviso_pronta_il FROM buste WHERE id = ?`, uno) != nil || statoAvvisi(t, b) != prima {
		t.Fatal("avviso scritto")
	}
}

// inventario: ORE-06, ORE-09
// La riga d'audit è nella transazione: se non si scrive, la busta non entra (né esce).
func TestLottoAuditNellaTransazione(t *testing.T) {
	b, d, _ := prova(t)
	l := lottoDel(t, b, 2, istanteProva)
	uno, due := prontaPerLotto(t, b, ""), prontaPerLotto(t, b, "")
	if _, err := aggiungiPerID(b, l, due); err != nil {
		t.Fatal(err)
	}
	esegui(t, d, `CREATE TRIGGER audit_rotto BEFORE INSERT ON eventi_audit WHEN NEW.azione IN ('ready_lot.bag_added', 'ready_lot.bag_removed') BEGIN SELECT RAISE(ABORT, 'rotto'); END`)
	if _, err := aggiungiPerID(b, l, uno); err == nil || lottoDi(t, b, uno) != 0 {
		t.Fatalf("aggiunta senza audit: %v", err)
	}
	if _, err := b.TogliDalLotto(ctx, l, due, chi); err == nil || lottoDi(t, b, due) != l {
		t.Fatalf("tolta senza audit: %v", err)
	}
}

// D-64 (come la D-89 della consegna, proposta D-95): il numero scritto è quello dell'etichetta. Fra più buste che
// lo mostrano contano, per lo stato, quelle che il lotto prende (pronte dell'oreficeria, non compro oro); con più e
// senza anno, quella dell'anno in corso; se ne restano più d'una, «Più buste corrispondono a «X»: …» senza scrivere
// niente; se nessuna è pronta, la più recente. La busta scelta dice il suo rifiuto (già nel lotto, già avvisata).
func TestAggiungiAlLottoNumeroDiPiuBuste(t *testing.T) {
	b, d, _ := prova(t)
	l := lottoDel(t, b, 2, istanteProva)
	vecchia := crea(t, b, con(modulo(), func(a *Accettazione) {
		a.OreficeriaID, a.CodiceQR, a.DataAccettazione = "2", "RIP-2025-0045", "2025-06-02"
	})).ID
	nuova := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID, a.CodiceQR = "2", "RIP-2026-0045" })).ID
	for _, id := range []int64{vecchia, nuova} {
		if _, err := b.CambiaStato(ctx, id, "pronta", chi); err != nil {
			t.Fatal(err)
		}
	}
	// Tutte e due pronte: senza anno quella dell'anno in corso; con l'anno quella.
	if x, err := b.AggiungiAlLotto(ctx, l, RichiestaLotto{Numero: "45"}, chi); err != nil || x.ID != nuova || x.Visibile().Titolo() != "#0045/26" {
		t.Fatalf("45: %+v %v", x, err)
	}
	// Review della 089: «45» di nuovo, con la 45/25 ancora pronta e libera, è la 45/26 già nel lotto, non la 45/25.
	prima := scatta(t, d)
	if _, err := b.AggiungiAlLotto(ctx, l, RichiestaLotto{Numero: "45"}, chi); !errors.Is(err, ErrGiaInQuestoLotto) {
		t.Fatalf("45 due volte: %v", err)
	}
	uguali(t, "45 due volte", prima, scatta(t, d))
	if x, err := b.AggiungiAlLotto(ctx, l, RichiestaLotto{Numero: "0045/25"}, chi); err != nil || x.ID != vecchia {
		t.Fatalf("0045/25: %+v %v", x, err)
	}
	// Una gemella dell'anno in corso: due buone, il rifiuto. Anche se una delle due è avvisata: l'avviso non filtra.
	esegui(t, d, `UPDATE buste SET lotto_avviso_id = NULL`)
	gemella := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID, a.CodiceQR = "2", "RIP-2026-0045" })).ID
	if _, err := b.CambiaStato(ctx, gemella, "pronta", chi); err != nil {
		t.Fatal(err)
	}
	for _, avvisata := range []bool{false, true} {
		if avvisata {
			esegui(t, d, `UPDATE buste SET avviso_pronta_il = ? WHERE id = ?`, istanteProva, nuova)
		}
		prima := scatta(t, d)
		_, err := b.AggiungiAlLotto(ctx, l, RichiestaLotto{Numero: "45"}, chi)
		if !errors.Is(err, ErrPiuBuste) || err.Error() != "Più buste corrispondono a «45»: scrivi il numero con l'anno o usa il QR." {
			t.Fatalf("gemelle (avvisata %v): %v", avvisata, err)
		}
		uguali(t, "più buste", prima, scatta(t, d))
	}
	// La gemella non più pronta: resta l'avvisata, con il suo rifiuto (non la 45/25 dell'anno prima).
	esegui(t, d, `UPDATE buste SET stato = 'in_lavorazione' WHERE id = ?`, gemella)
	if _, err := b.AggiungiAlLotto(ctx, l, RichiestaLotto{Numero: "45"}, chi); !errors.Is(err, ErrLottoGiaAvvisata) {
		t.Fatalf("una buona avvisata: %v", err)
	}
	// Nessuna pronta con quel numero e anno: la più recente, con il suo rifiuto.
	esegui(t, d, `UPDATE buste SET stato = 'in_lavorazione' WHERE id = ?`, nuova)
	if _, err := b.AggiungiAlLotto(ctx, l, RichiestaLotto{Numero: "0045/26"}, chi); !errors.Is(err, ErrLottoNonPronta) {
		t.Fatalf("nessuna buona: %v", err)
	}
}

// inventario: ORE-09
// «Rimuovi» scollega la busta dal lotto aperto (con l'istante di adesso) e scrive ready_lot.bag_removed; una busta
// non nel lotto (anche una che non c'è, o nel cestino) è «Busta non in questo lotto.»; il lotto che non c'è è
// «Lotto non disponibile.»; la busta tolta si può aggiungere di nuovo.
func TestTogliDalLotto(t *testing.T) {
	b, d, _ := prova(t)
	l := lottoDel(t, b, 2, istanteProva)
	uno, due := prontaPerLotto(t, b, ""), prontaPerLotto(t, b, "")
	for _, id := range []int64{uno, due} {
		if _, err := aggiungiPerID(b, l, id); err != nil {
			t.Fatal(err)
		}
	}
	esegui(t, d, `UPDATE buste SET aggiornato_il = '2026-01-01T00:00:00.000000Z'`)
	x, err := b.TogliDalLotto(ctx, l, uno, chi)
	if err != nil || x.ID != uno || x.Visibile().Titolo() != "N. 1/26" || lottoDi(t, b, uno) != 0 || valore(t, d, `SELECT aggiornato_il FROM buste WHERE id = ?`, uno) != istanteProva {
		t.Fatalf("togli: %+v %v", x, err)
	}
	if a := righeAudit(t, b, "ready_lot.bag_removed"); len(a) != 1 || a[0] != itoa(l)+`|{"repair_id":`+itoa(uno)+`}` {
		t.Fatalf("audit: %v", a)
	}
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, due)
	prima := scatta(t, d)
	for _, c := range []struct {
		lotto, busta int64
		err          error
	}{{l, uno, ErrNonInQuestoLotto}, {l, 9999, ErrNonInQuestoLotto}, {l, due, ErrNonInQuestoLotto}, {9999, uno, ErrLottoNonDisponibile}} {
		if _, err := b.TogliDalLotto(ctx, c.lotto, c.busta, chi); !errors.Is(err, c.err) {
			t.Errorf("togli %d dal lotto %d: %v, atteso %v", c.busta, c.lotto, err, c.err)
		}
	}
	uguali(t, "rifiuti", prima, scatta(t, d))
	if lottoDi(t, b, due) != l {
		t.Fatal("la busta nel cestino ha lasciato il lotto")
	}
	if _, err := aggiungiPerID(b, l, uno); err != nil || lottoDi(t, b, uno) != l {
		t.Fatalf("di nuovo: %v", err)
	}
	// Un lotto inviato o chiuso tiene i suoi membri.
	esegui(t, d, `UPDATE lotti_avviso SET inviato_il = ? WHERE id = ?`, istanteProva, l)
	if _, err := b.TogliDalLotto(ctx, l, uno, chi); !errors.Is(err, ErrLottoNonDisponibile) || lottoDi(t, b, uno) != l {
		t.Fatalf("lotto inviato: %v", err)
	}
	esegui(t, d, `UPDATE lotti_avviso SET chiuso_il = ? WHERE id = ?`, istanteProva, l)
	if _, err := b.TogliDalLotto(ctx, l, uno, chi); !errors.Is(err, ErrLottoNonDisponibile) || lottoDi(t, b, uno) != l {
		t.Fatalf("lotto chiuso: %v", err)
	}
}

// inventario: ORE-08
// Le candidate: le buste pronte dell'oreficeria del lotto in nessun lotto, comprese quelle già avvisate (segnate),
// per data di pronta e id decrescenti. Fuori: un'altra oreficeria, in lavorazione, in un lotto (anche questo), compro
// oro, cestino.
func TestCandidateDelLotto(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Oreficeria Altra')`)
	l := lottoDel(t, b, 2, istanteProva)
	vecchia, libera, avvisata := prontaPerLotto(t, b, ""), prontaPerLotto(t, b, ""), prontaPerLotto(t, b, "")
	esegui(t, d, `UPDATE buste SET data_pronta = '2026-10-01' WHERE id = ?`, vecchia)
	if _, err := b.AnnunciaRitiro(ctx, avvisata, baseProva, chi); err != nil {
		t.Fatal(err)
	}
	nelLotto := prontaPerLotto(t, b, "")
	if _, err := aggiungiPerID(b, l, nelLotto); err != nil {
		t.Fatal(err)
	}
	altra := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = "3" })).ID
	if _, err := b.CambiaStato(ctx, altra, "pronta", chi); err != nil {
		t.Fatal(err)
	}
	crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = "2" })) // in lavorazione
	scheda := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID, a.CompraOro = "2", "1" })).ID
	if _, err := b.CambiaStato(ctx, scheda, "valutata", chi); err != nil {
		t.Fatal(err)
	}
	cestino := prontaPerLotto(t, b, "")
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestino)
	lo, err := b.LottoAvviso(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	cc, err := b.CandidateDelLotto(ctx, lo)
	if err != nil {
		t.Fatal(err)
	}
	if got := idDelLotto(cc); len(got) != 3 || got[0] != avvisata || got[1] != libera || got[2] != vecchia {
		t.Fatalf("candidate: %v (avvisata %d, libera %d, vecchia %d)", got, avvisata, libera, vecchia)
	}
	if !cc[0].GiaAvvisata || cc[1].GiaAvvisata || cc[2].GiaAvvisata || cc[1].Numero != "2/26" || cc[1].Descrizione != "Anello da stringere" {
		t.Fatalf("segni: %+v", cc)
	}
	// Una busta aggiunta lascia le candidate.
	if _, err := aggiungiPerID(b, l, libera); err != nil {
		t.Fatal(err)
	}
	if cc, _ := b.CandidateDelLotto(ctx, lo); len(cc) != 2 {
		t.Fatalf("dopo l'aggiunta: %v", idDelLotto(cc))
	}
}

// inventario: ORE-06, ORE-09
// Il lotto con la sua oreficeria (aperto, inviato, chiuso, link storico spento) e le sue buste fuori dal cestino
// dalla più recente; un lotto che non c'è è «Lotto non trovato.».
func TestLottoAvvisoEBuste(t *testing.T) {
	b, d, _ := prova(t)
	l := lottoDel(t, b, 2, "2026-01-15T08:05:00.000000Z")
	lo, err := b.LottoAvviso(ctx, l)
	if err != nil || lo.ID != l || lo.OreficeriaID != 2 || lo.Oreficeria != "Oreficeria Esempio" || lo.Gettone == "" ||
		lo.Aperto.Format("2006-01-02 15:04") != "2026-01-15 08:05" || lo.Inviato != nil || lo.Chiuso != nil || lo.AccessoSpento || !lo.Modificabile() {
		t.Fatalf("lotto: %+v %v", lo, err)
	}
	esegui(t, d, `UPDATE lotti_avviso SET inviato_il = '2026-07-15T08:05:00.000000Z' WHERE id = ?`, l)
	if lo, err = b.LottoAvviso(ctx, l); err != nil || lo.Inviato == nil || lo.Inviato.Format("2006-01-02 15:04") != "2026-07-15 08:05" || lo.Chiuso != nil || lo.Modificabile() {
		t.Fatalf("inviato: %+v %v", lo, err)
	}
	esegui(t, d, `UPDATE lotti_avviso SET chiuso_il = ?, accesso_pubblico_spento_il = ? WHERE id = ?`, istanteProva, istanteProva, l)
	if lo, err = b.LottoAvviso(ctx, l); err != nil || lo.Chiuso == nil || !lo.AccessoSpento || lo.Modificabile() {
		t.Fatalf("chiuso: %+v %v", lo, err)
	}
	esegui(t, d, `UPDATE lotti_avviso SET inviato_il = NULL, chiuso_il = NULL WHERE id = ?`, l)
	if lo, _ = b.LottoAvviso(ctx, l); !lo.Modificabile() {
		t.Fatal("aperto non modificabile")
	}
	if _, err := b.LottoAvviso(ctx, 9999); !errors.Is(err, ErrLottoNonTrovato) {
		t.Fatalf("che non c'è: %v", err)
	}
	uno, due, tre := prontaPerLotto(t, b, ""), prontaPerLotto(t, b, ""), prontaPerLotto(t, b, "")
	for _, id := range []int64{uno, due, tre} {
		if _, err := aggiungiPerID(b, l, id); err != nil {
			t.Fatal(err)
		}
	}
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, due)
	esegui(t, d, `UPDATE buste SET avviso_pronta_il = ? WHERE id = ?`, istanteProva, tre)
	bb, err := b.BusteDelLotto(ctx, l)
	if got := idDelLotto(bb); err != nil || len(got) != 2 || got[0] != tre || got[1] != uno || !bb[0].GiaAvvisata {
		t.Fatalf("buste: %v %v", got, err)
	}
}

// inventario: ORE-06
// Mutazioni della review (09, 11, 17): una busta già in un lotto inviato, riaggiunta a quel lotto da una pagina vecchia,
// è «Lotto non disponibile.» (prima di «già in questo lotto»); una scheda compro oro valutata di un'altra oreficeria è
// «altra oreficeria» (prima di «compro oro»); un numero che mostra anche una busta pronta di un'altra oreficeria
// aggiunge quella di questa, senza «più buste».
func TestAggiungiAlLottoCasiDellaReview(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Oreficeria Altra')`)
	l := lottoDel(t, b, 2, istanteProva)
	uno := prontaPerLotto(t, b, "")
	if _, err := aggiungiPerID(b, l, uno); err != nil {
		t.Fatal(err)
	}
	esegui(t, d, `UPDATE lotti_avviso SET inviato_il = ? WHERE id = ?`, istanteProva, l)
	if _, err := aggiungiPerID(b, l, uno); !errors.Is(err, ErrLottoNonDisponibile) {
		t.Fatalf("pagina vecchia di un lotto inviato: %v", err)
	}
	aperto := lottoDel(t, b, 2, istanteProva)
	scheda := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID, a.CompraOro = "3", "1" })).ID
	if _, err := b.CambiaStato(ctx, scheda, "valutata", chi); err != nil {
		t.Fatal(err)
	}
	if _, err := aggiungiPerID(b, aperto, scheda); !errors.Is(err, ErrLottoAltraOreficeria) {
		t.Fatalf("compro oro di un'altra oreficeria: %v", err)
	}
	altrui := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID, a.CodiceQR = "3", "RIP-2026-0047" })).ID
	nostra := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID, a.CodiceQR = "2", "RIP-2026-0047" })).ID
	for _, id := range []int64{altrui, nostra} {
		if _, err := b.CambiaStato(ctx, id, "pronta", chi); err != nil {
			t.Fatal(err)
		}
	}
	if x, err := b.AggiungiAlLotto(ctx, aperto, RichiestaLotto{Numero: "47"}, chi); err != nil || x.ID != nostra || lottoDi(t, b, altrui) != 0 {
		t.Fatalf("numero condiviso con un'altra oreficeria: %+v %v", x, err)
	}
}

// inventario: ORE-08
// Mutazione della review (23): l'ordine delle candidate è per data di pronta prima che per id: una busta con l'id più
// alto e una data di pronta vecchia va in fondo.
func TestCandidatePerDataDiPronta(t *testing.T) {
	b, d, _ := prova(t)
	l := lottoDel(t, b, 2, istanteProva)
	prima, seconda, vecchia := prontaPerLotto(t, b, ""), prontaPerLotto(t, b, ""), prontaPerLotto(t, b, "")
	esegui(t, d, `UPDATE buste SET data_pronta = '2026-09-01' WHERE id = ?`, vecchia)
	lo, err := b.LottoAvviso(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	cc, err := b.CandidateDelLotto(ctx, lo)
	if got := idDelLotto(cc); err != nil || len(got) != 3 || got[0] != seconda || got[1] != prima || got[2] != vecchia {
		t.Fatalf("ordine: %v %v (prima %d, seconda %d, vecchia %d)", got, err, prima, seconda, vecchia)
	}
}
