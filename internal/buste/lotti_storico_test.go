package buste

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// Lo storico dei lotti inviati, il dettaglio e il reinvio (slice 093, ORE-11, ORE-12, ORE-13), come
// ReadyLots.list_sent_lots/0, fetch_sent_lot/1, members/1, resend_message/1 e record_resend/4 dello standalone: i casi
// di ready_lots_test.exs («the history of sent lots») e di ready_lot_live_test.exs («resending a sent lot», «the batch
// send and the bag links (slice 104)»). Le buste sono dell'oreficeria 2 di prova («Oreficeria Esempio», telefono
// 0490000000). Nomi inventati.

// inviaPerProva segna il lotto inviato all'istante dato, senza passare dall'invio (nessuna riga, nessun avviso).
func inviaPerProva(t *testing.T, b *Buste, l int64, il string) {
	t.Helper()
	esegui(t, b.d, `UPDATE lotti_avviso SET inviato_il = ? WHERE id = ?`, il, l)
}

func idStorico(x []LottoStorico) []int64 {
	var out []int64
	for _, y := range x {
		out = append(out, y.ID)
	}
	return out
}

// inventario: ORE-11
// Lo storico: solo i lotti inviati (mai un aperto), chiusi compresi, dal più recente per id (non per data di invio),
// con l'oreficeria, le buste fuori dal cestino e quelle non ancora consegnate («tre e una», mai «due e due»: un conto
// delle ritirate si leggerebbe come quello delle rimaste). Nessun lotto: vuoto.
func TestStoricoLotti(t *testing.T) {
	b, d, _ := prova(t)
	if x, err := b.StoricoLotti(ctx); err != nil || len(x) != 0 {
		t.Fatalf("vuoto: %v %v", x, err)
	}
	esegui(t, d, `INSERT INTO oreficerie (id, nome, telefono) VALUES (3, 'Beta Gioielli', NULL)`)
	aperto := lottoDel(t, b, 2, istanteProva)
	nelLotto(t, b, aperto, prontaPerLotto(t, b, ""))
	inviato := lottoDel(t, b, 2, istanteProva)
	uno, due, tre, cestino := prontaPerLotto(t, b, ""), prontaPerLotto(t, b, ""), prontaPerLotto(t, b, ""), prontaPerLotto(t, b, "")
	nelLotto(t, b, inviato, uno, due, tre, cestino)
	esegui(t, d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, uno)
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestino)
	inviaPerProva(t, b, inviato, "2026-10-31T08:30:00.000000Z")
	// Più recente per id ma inviato prima: viene prima lo stesso.
	chiuso := lottoDel(t, b, 3, istanteProva)
	inviaPerProva(t, b, chiuso, "2026-10-20T08:00:00.000000Z")
	esegui(t, d, `UPDATE lotti_avviso SET chiuso_il = '2026-10-21T07:15:00.000000Z' WHERE id = ?`, chiuso)
	vuoto := lottoDel(t, b, 2, istanteProva)
	inviaPerProva(t, b, vuoto, "2026-10-31T08:45:00.000000Z")
	x, err := b.StoricoLotti(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := idStorico(x); len(got) != 3 || got[0] != vuoto || got[1] != chiuso || got[2] != inviato {
		t.Fatalf("ordine: %v (aperto %d)", got, aperto)
	}
	i := x[2]
	if i.Oreficeria != "Oreficeria Esempio" || i.Buste != 3 || i.DaRitirare != 2 || i.Inviato == nil || i.Chiuso != nil ||
		!i.Inviato.Equal(time.Date(2026, 10, 31, 8, 30, 0, 0, time.UTC)) || i.OreficeriaID != 2 {
		t.Fatalf("inviato: %+v", i)
	}
	c := x[1]
	if c.Oreficeria != "Beta Gioielli" || c.Buste != 0 || c.DaRitirare != 0 || c.Chiuso == nil || !c.Chiuso.Equal(time.Date(2026, 10, 21, 7, 15, 0, 0, time.UTC)) {
		t.Fatalf("chiuso: %+v", c)
	}
	if v := x[0]; v.Buste != 0 || v.DaRitirare != 0 || v.Chiuso != nil {
		t.Fatalf("vuoto: %+v", v)
	}
}

// inventario: ORE-11
// Gli ultimi 100: con 101 lotti inviati il più vecchio (per id) non c'è.
func TestStoricoLottiCento(t *testing.T) {
	b, d, _ := prova(t)
	var primo, ultimo int64
	for i := 0; i < 101; i++ {
		l := lottoDel(t, b, 2, istanteProva)
		esegui(t, d, `UPDATE lotti_avviso SET token = ?, inviato_il = ? WHERE id = ?`, "gettone-cento-"+itoa(l), istanteProva, l)
		if i == 0 {
			primo = l
		}
		ultimo = l
	}
	x, err := b.StoricoLotti(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(x) != 100 || x[0].ID != ultimo || x[99].ID != primo+1 {
		t.Fatalf("cento: %d, primo %d, ultimo %d", len(x), x[0].ID, x[len(x)-1].ID)
	}
}

// inventario: ORE-12
// Il dettaglio: solo un lotto inviato (anche chiuso, anche con il link spento all'importazione); un lotto aperto o
// che non c'è è «Lotto non trovato nello storico.». Le buste del lotto (fuori dal cestino, anche di un'altra
// oreficeria) dicono se sono ritirate.
func TestLottoDelloStorico(t *testing.T) {
	b, d, _ := prova(t)
	aperto := lottoDel(t, b, 2, istanteProva)
	for _, id := range []int64{aperto, 9999, 0, -1} {
		if _, err := b.LottoDelloStorico(ctx, id); !errors.Is(err, ErrLottoNonNelloStorico) {
			t.Fatalf("lotto %d: %v", id, err)
		}
	}
	if ErrLottoNonNelloStorico.Error() != "Lotto non trovato nello storico." {
		t.Fatal("testo")
	}
	l := lottoDel(t, b, 2, istanteProva)
	uno, due, cestino := prontaPerLotto(t, b, ""), prontaPerLotto(t, b, ""), prontaPerLotto(t, b, "")
	nelLotto(t, b, l, uno, due, cestino)
	esegui(t, d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Beta Gioielli')`)
	esegui(t, d, `UPDATE buste SET stato = 'consegnata', oreficeria_id = 3 WHERE id = ?`, uno)
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestino)
	inviaPerProva(t, b, l, "2026-10-31T08:30:00.000000Z")
	x, err := b.LottoDelloStorico(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	if x.ID != l || x.Oreficeria != "Oreficeria Esempio" || x.Inviato == nil || x.Chiuso != nil || x.AccessoSpento || !x.Reinviabile() {
		t.Fatalf("lotto: %+v", x)
	}
	bb, err := b.BusteDelLotto(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	if len(bb) != 2 || bb[0].ID != due || bb[0].Ritirata || bb[1].ID != uno || !bb[1].Ritirata {
		t.Fatalf("buste: %+v", bb)
	}
	esegui(t, d, `UPDATE lotti_avviso SET accesso_pubblico_spento_il = ? WHERE id = ?`, istanteProva, l)
	if x, err = b.LottoDelloStorico(ctx, l); err != nil || !x.AccessoSpento || x.Reinviabile() {
		t.Fatalf("spento: %+v %v", x, err)
	}
	esegui(t, d, `UPDATE lotti_avviso SET accesso_pubblico_spento_il = NULL, chiuso_il = ? WHERE id = ?`, istanteProva, l)
	if x, err = b.LottoDelloStorico(ctx, l); err != nil || x.Chiuso == nil || x.Reinviabile() {
		t.Fatalf("chiuso: %+v %v", x, err)
	}
}

// lottoInviatoProva è un lotto dell'oreficeria 2 inviato con l'invio vero (le buste avvisate, le righe scritte), con
// le buste date.
func lottoInviatoProva(t *testing.T, b *Buste, buste ...int64) int64 {
	t.Helper()
	l := lottoDel(t, b, 2, istanteProva)
	nelLotto(t, b, l, buste...)
	if _, err := b.InviaLotto(ctx, l, basePubblicaProva, chi); err != nil {
		t.Fatal(err)
	}
	return l
}

// inventario: ORE-13
// «Prepara di nuovo il messaggio»: lo stesso testo del lotto con lo **stesso link**, per le buste che la pagina
// pubblica mostra ancora (dell'oreficeria del lotto, fuori dal cestino), il numero WhatsApp dell'oreficeria ("" senza:
// contatto scelto). Non scrive niente.
func TestPreparaReinvio(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Beta Gioielli')`)
	uno, due, cestino := prontaPerLotto(t, b, ""), prontaPerLotto(t, b, ""), prontaPerLotto(t, b, "")
	l := lottoInviatoProva(t, b, uno, due, cestino)
	esegui(t, d, `UPDATE buste SET oreficeria_id = 3 WHERE id = ?`, uno)
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestino)
	link := basePubblicaProva + "/lotto/" + gettoneDelLotto(t, b, l)
	prima := scattaInvio(t, b)
	x, err := b.PreparaReinvio(ctx, l, basePubblicaProva)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Buongiorno Oreficeria Esempio,\nho 1 riparazioni pronte da farvi ritirare.\nDettagli e foto: " + link + "\n\nNegozio"; x.Messaggio != want {
		t.Fatalf("testo:\n%q\n%q", x.Messaggio, want)
	}
	if x.Numero != "+390490000000" || x.Buste != 1 || x.Lotto.ID != l || x.Lotto.Oreficeria != "Oreficeria Esempio" {
		t.Fatalf("reinvio: %+v", x)
	}
	nienteScritto(t, "prepara", prima, scattaInvio(t, b))
	for _, tel := range []any{nil, "", "da chiedere"} {
		esegui(t, d, `UPDATE oreficerie SET telefono = ? WHERE id = 2`, tel)
		if x, err := b.PreparaReinvio(ctx, l, basePubblicaProva); err != nil || x.Numero != "" {
			t.Fatalf("telefono %v: %+v %v", tel, x, err)
		}
	}
	nienteScritto(t, "senza numero", prima, scattaInvio(t, b))
}

// inventario: ORE-13
// I rifiuti del reinvio, senza scrivere: un lotto che non c'è, aperto, chiuso o con il link spento all'importazione è
// «Lotto non disponibile.»; tutte le buste ritirate (anche se il lotto non è stato chiuso: nessuno ha aperto la pagina
// dopo; contano anche le buste passate a un'altra oreficeria) o nessuna busta fuori dal cestino «Tutte le buste del
// lotto sono state ritirate.»; nessuna busta ancora dell'oreficeria «Nessuna busta del lotto è ancora di questa
// oreficeria: niente da reinviare.»; un testo del lotto non valido è ErrModelloLotto; senza indirizzo pubblico un testo
// con {link} non si prepara (D-46 estesa), senza {link} sì.
func TestPreparaReinvioRifiuti(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Beta Gioielli')`)
	aperto := lottoDel(t, b, 2, istanteProva)
	nelLotto(t, b, aperto, prontaPerLotto(t, b, ""))
	chiuso := lottoInviatoProva(t, b, prontaPerLotto(t, b, ""))
	esegui(t, d, `UPDATE lotti_avviso SET chiuso_il = ? WHERE id = ?`, istanteProva, chiuso)
	spento := lottoInviatoProva(t, b, prontaPerLotto(t, b, ""))
	esegui(t, d, `UPDATE lotti_avviso SET accesso_pubblico_spento_il = ? WHERE id = ?`, istanteProva, spento)
	r1, r2 := prontaPerLotto(t, b, ""), prontaPerLotto(t, b, "")
	ritirato := lottoInviatoProva(t, b, r1, r2)
	esegui(t, d, `UPDATE buste SET stato = 'consegnata' WHERE id IN (?, ?)`, r1, r2)
	esegui(t, d, `UPDATE buste SET oreficeria_id = 3 WHERE id = ?`, r2)
	c := prontaPerLotto(t, b, "")
	soloCestino := lottoInviatoProva(t, b, c)
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, c)
	s := prontaPerLotto(t, b, "")
	spostato := lottoInviatoProva(t, b, s)
	esegui(t, d, `UPDATE buste SET oreficeria_id = 3 WHERE id = ?`, s)
	prima := scattaInvio(t, b)
	for _, k := range []struct {
		lotto int64
		err   error
	}{{9999, ErrLottoNonDisponibile}, {0, ErrLottoNonDisponibile}, {aperto, ErrLottoNonDisponibile}, {chiuso, ErrLottoNonDisponibile},
		{spento, ErrLottoNonDisponibile}, {ritirato, ErrLottoCompletato}, {soloCestino, ErrLottoCompletato}, {spostato, ErrNienteDaReinviare}} {
		if _, err := b.PreparaReinvio(ctx, k.lotto, basePubblicaProva); !errors.Is(err, k.err) {
			t.Fatalf("prepara %d: %v, atteso %v", k.lotto, err, k.err)
		}
		if _, err := b.RegistraReinvio(ctx, k.lotto, "Testo", false, chi); !errors.Is(err, k.err) {
			t.Fatalf("registra %d: %v, atteso %v", k.lotto, err, k.err)
		}
	}
	nienteScritto(t, "rifiuti", prima, scattaInvio(t, b))
	if ErrLottoCompletato.Error() != "Tutte le buste del lotto sono state ritirate." ||
		ErrNienteDaReinviare.Error() != "Nessuna busta del lotto è ancora di questa oreficeria: niente da reinviare." {
		t.Fatal("testi")
	}
	buono := lottoInviatoProva(t, b, prontaPerLotto(t, b, ""))
	prima = scattaInvio(t, b)
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.template.wa_oreficeria_lotto', 'Lotto {password}', ?)`, istanteProva)
	var m ErrModelloLotto
	if _, err := b.PreparaReinvio(ctx, buono, basePubblicaProva); !errors.As(err, &m) || m.Famiglia != FamOreficeriaLotto ||
		m.Avviso != "Segnaposto non supportato: {password}" {
		t.Fatalf("testo non valido: %v", err)
	}
	esegui(t, d, `DELETE FROM impostazioni WHERE chiave = 'outgoing.template.wa_oreficeria_lotto'`)
	if _, err := b.PreparaReinvio(ctx, buono, ""); !errors.Is(err, ErrSenzaIndirizzoPubblico) {
		t.Fatalf("senza indirizzo pubblico: %v", err)
	}
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.template.wa_oreficeria_lotto', 'Ciao {oref_nome}, {n_buste} pronte.', ?)`, istanteProva)
	if x, err := b.PreparaReinvio(ctx, buono, ""); err != nil || x.Messaggio != "Ciao Oreficeria Esempio, 1 pronte." {
		t.Fatalf("senza {link}: %+v %v", x, err)
	}
	esegui(t, d, `DELETE FROM impostazioni WHERE chiave = 'outgoing.template.wa_oreficeria_lotto'`)
	nienteScritto(t, "testi", prima, scattaInvio(t, b))
}

// inventario: ORE-13
// La registrazione del reinvio (il tocco su «Apri WhatsApp» o «👥 Scegli contatto»): una riga WhatsApp «accepted»
// all'oreficeria senza busta, con il testo dato e il numero dell'oreficeria (o «contatto scelto» col tocco su «Scegli
// contatto» o senza un numero), outgoing.handed_off e ready_lot.resent con le buste e la riga, nella stessa
// transazione e con l'utente. Il lotto (gettone, invio, aggiornamento) e gli avvisi delle buste non cambiano. Il
// dominio registra ogni volta che lo si chiama: il «una volta per preparazione» è della pagina.
func TestRegistraReinvio(t *testing.T) {
	b, d, _ := prova(t)
	uno, due := prontaPerLotto(t, b, ""), prontaPerLotto(t, b, "")
	l := lottoInviatoProva(t, b, uno, due)
	lotto := valore(t, d, `SELECT token || '|' || inviato_il || '|' || aggiornato_il || '|' || coalesce(chiuso_il, '-') FROM lotti_avviso WHERE id = ?`, l)
	avvisi := valore(t, d, `SELECT group_concat(id || ':' || avviso_pronta_il || ':' || aggiornato_il) FROM buste`)
	righe := conta(t, d, `SELECT count(*) FROM messaggi_in_uscita`)
	mid, err := b.RegistraReinvio(ctx, l, "Testo del reinvio", false, chi)
	if err != nil {
		t.Fatal(err)
	}
	if v := valore(t, d, `SELECT coalesce(busta_id, 0) || '|' || canale || '|' || destinatario || '|' || indirizzo || '|' || testo || '|' || stato || '|' ||
		tentativi || '|' || accettato_il FROM messaggi_in_uscita WHERE id = ?`, mid); v != "0|whatsapp|oreficeria|+390490000000|Testo del reinvio|accepted|1|"+istanteProva {
		t.Fatalf("riga: %v", v)
	}
	if conta(t, d, `SELECT count(*) FROM messaggi_in_uscita`) != righe+1 {
		t.Fatal("righe")
	}
	if a := righeAudit(t, b, "ready_lot.resent"); len(a) != 1 || a[0] != itoa(l)+`|{"bags":2,"message_id":`+itoa(mid)+`}` {
		t.Fatalf("ready_lot.resent: %v", a)
	}
	if v := valore(t, d, `SELECT attore || '|' || risorsa || '|' || esito FROM eventi_audit WHERE azione = 'ready_lot.resent'`); v != chi.Attore+"|ready_lot|ok" {
		t.Fatalf("attore: %v", v)
	}
	if v := valore(t, d, `SELECT dati FROM eventi_audit WHERE azione = 'outgoing.handed_off' AND id_risorsa = ?`, itoa(mid)); v != `{"channel":"whatsapp","party":"oreficeria","repair_id":null}` {
		t.Fatalf("outgoing.handed_off: %v", v)
	}
	if v := valore(t, d, `SELECT azione FROM eventi_audit ORDER BY id DESC LIMIT 1`); v != "ready_lot.resent" {
		t.Fatalf("ultima riga: %v", v)
	}
	if v := valore(t, d, `SELECT token || '|' || inviato_il || '|' || aggiornato_il || '|' || coalesce(chiuso_il, '-') FROM lotti_avviso WHERE id = ?`, l); v != lotto {
		t.Fatalf("lotto cambiato: %v", v)
	}
	if v := valore(t, d, `SELECT group_concat(id || ':' || avviso_pronta_il || ':' || aggiornato_il) FROM buste`); v != avvisi {
		t.Fatalf("avvisi cambiati: %v", v)
	}
	// «👥 Scegli contatto»: il contatto scelto sul telefono, anche con un numero.
	mid2, err := b.RegistraReinvio(ctx, l, "Altro testo", true, chi)
	if err != nil || mid2 == mid {
		t.Fatal(err)
	}
	if v := valore(t, d, `SELECT indirizzo FROM messaggi_in_uscita WHERE id = ?`, mid2); v != "contatto-scelto" {
		t.Fatalf("scelto: %v", v)
	}
	// Senza numero: contatto scelto anche da «Apri WhatsApp».
	esegui(t, d, `UPDATE oreficerie SET telefono = NULL WHERE id = 2`)
	mid3, err := b.RegistraReinvio(ctx, l, "Terzo", false, chi)
	if err != nil {
		t.Fatal(err)
	}
	if v := valore(t, d, `SELECT indirizzo FROM messaggi_in_uscita WHERE id = ?`, mid3); v != "contatto-scelto" {
		t.Fatalf("senza numero: %v", v)
	}
	if n := len(righeAudit(t, b, "ready_lot.resent")); n != 3 {
		t.Fatalf("righe ready_lot.resent: %d", n)
	}
	if strings.Contains(valore(t, d, `SELECT group_concat(testo, '|') FROM messaggi_in_uscita WHERE busta_id IS NULL`).(string), "Dettagli") {
		t.Fatal("il testo non è quello dato")
	}
}
