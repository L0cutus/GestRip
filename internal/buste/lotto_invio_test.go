package buste

import (
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
)

// «📤 Chiudi e invia» del lotto d'avviso (slice 091, ORE-10), come ReadyLots.send_lot/2 dello standalone: i casi di
// ready_lots_test.exs («sending a lot») e di ready_lot_live_test.exs («the batch send and the bag links (slice
// 104)»), più il guasto a metà e la gara. Le buste sono dell'oreficeria 2 di prova («Oreficeria Esempio», telefono
// 0490000000), accettate il 31/10/2026: 1/26, 2/26, … senza etichetta. Nomi e indirizzi inventati.

const basePubblicaProva = "https://negozio.esempio.ts.net"

// testoMembroProva è il testo «Avviso oreficeria» (wa_oreficeria) della busta n con il link del lotto.
func testoMembroProva(n, link string) string {
	return "Ciao Oreficeria Esempio, aggiornamento sulla lavorazione:\nCliente: Cliente Prova — busta n. " + n +
		"\nLavorazione: Anello da stringere\nStato: PRONTA\n\nDettagli e foto: " + link + "\nNegozio"
}

func gettoneDelLotto(t *testing.T, b *Buste, l int64) string {
	t.Helper()
	return valore(t, b.d, `SELECT token FROM lotti_avviso WHERE id = ?`, l).(string)
}

func nelLotto(t *testing.T, b *Buste, l int64, buste ...int64) {
	t.Helper()
	for _, id := range buste {
		esegui(t, b.d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, l, id)
	}
}

// fotoInvio è quello che un invio scrive: un rifiuto la lascia uguale.
type fotoInvio struct {
	Lotti, Buste, Messaggi, Eventi, Audit string
}

func scattaInvio(t *testing.T, b *Buste) fotoInvio {
	t.Helper()
	g := func(q string) string {
		var s sql.NullString
		if err := b.d.Lettura.QueryRow(q).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s.String
	}
	return fotoInvio{
		Lotti:    g(`SELECT group_concat(id || ':' || coalesce(inviato_il, '-') || ':' || coalesce(accesso_pubblico_spento_il, '-') || ':' || aggiornato_il, ',') FROM lotti_avviso`),
		Buste:    g(`SELECT group_concat(id || ':' || coalesce(avviso_pronta_il, '-') || ':' || coalesce(token_pubblico, '-') || ':' || aggiornato_il || ':' || coalesce(lotto_avviso_id, '-'), ',') FROM buste`),
		Messaggi: g(`SELECT count(*) FROM messaggi_in_uscita`),
		Eventi:   g(`SELECT count(*) FROM eventi_avviso_pronta`),
		Audit:    g(`SELECT count(*) FROM eventi_audit`),
	}
}

func nienteScritto(t *testing.T, cosa string, prima, dopo fotoInvio) {
	t.Helper()
	if prima != dopo {
		t.Errorf("%s: il database è cambiato\nprima %+v\ndopo  %+v", cosa, prima, dopo)
	}
}

// inventario: ORE-10
// L'invio segna il lotto inviato (adesso, link storico riacceso), avvisa ogni busta visibile con la stessa porta
// dell'avviso singolo (blocco, riga WhatsApp all'oreficeria con il suo numero, evento «notified», ready_notice.sent
// e outgoing.handed_off), dalla busta più recente; il testo di ogni riga è «Avviso oreficeria» con il link del lotto
// (nessun link della busta: niente token nuovo né repair.public_link_issued); poi ready_lot.sent con il numero di
// buste, tutto con l'utente. Dà il testo del lotto (wa_oreficeria_lotto), il numero WhatsApp dell'oreficeria e il
// link pubblico. Una busta nel cestino resta nel lotto ma non si avvisa né si conta.
func TestInviaLotto(t *testing.T) {
	b, d, _ := prova(t)
	l := lottoDel(t, b, 2, istanteProva)
	uno, due, cestino := prontaPerLotto(t, b, ""), prontaPerLotto(t, b, ""), prontaPerLotto(t, b, "")
	nelLotto(t, b, l, uno, due, cestino)
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestino)
	esegui(t, d, `UPDATE lotti_avviso SET accesso_pubblico_spento_il = '2026-01-01T00:00:00.000000Z', aggiornato_il = '2026-01-01T00:00:00.000000Z' WHERE id = ?`, l)
	gettoni := valore(t, d, `SELECT group_concat(coalesce(token_pubblico, '-')) FROM buste`)
	audPrima := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_link_issued'`)
	g := gettoneDelLotto(t, b, l)
	link := basePubblicaProva + "/lotto/" + g
	x, err := b.InviaLotto(ctx, l, basePubblicaProva, chi)
	if err != nil {
		t.Fatal(err)
	}
	if x.Buste != 2 || x.Numero != "+390490000000" || x.Link != link || x.Lotto.ID != l || x.Lotto.Inviato == nil || x.Lotto.Modificabile() ||
		x.Lotto.Oreficeria != "Oreficeria Esempio" || x.Lotto.AccessoSpento {
		t.Fatalf("esito: %+v", x)
	}
	if want := "Buongiorno Oreficeria Esempio,\nho 2 riparazioni pronte da farvi ritirare.\nDettagli e foto: " + link + "\n\nNegozio"; x.Messaggio != want {
		t.Fatalf("testo del lotto:\n%q\n%q", x.Messaggio, want)
	}
	if v := valore(t, d, `SELECT inviato_il || '|' || coalesce(accesso_pubblico_spento_il, '-') || '|' || aggiornato_il || '|' || coalesce(chiuso_il, '-') FROM lotti_avviso WHERE id = ?`, l); v != istanteProva+"|-|"+istanteProva+"|-" {
		t.Fatalf("lotto: %v", v)
	}
	// Dalla più recente: la 2/26 ha la prima riga.
	rr, err := d.Lettura.Query(`SELECT m.busta_id, m.destinatario, m.indirizzo, m.testo, m.stato, e.evento, e.attore, b.avviso_pronta_il
		FROM messaggi_in_uscita m JOIN eventi_avviso_pronta e ON e.messaggio_in_uscita_id = m.id JOIN buste b ON b.id = m.busta_id ORDER BY m.id`)
	if err != nil {
		t.Fatal(err)
	}
	var righe []string
	for rr.Next() {
		var busta int64
		var dest, ind, testo, stato, ev, attore, il string
		if err := rr.Scan(&busta, &dest, &ind, &testo, &stato, &ev, &attore, &il); err != nil {
			t.Fatal(err)
		}
		righe = append(righe, strings.Join([]string{itoa(busta), dest, ind, testo, stato, ev, attore, il}, "|"))
	}
	_ = rr.Close()
	atteso := []string{
		strings.Join([]string{itoa(due), "oreficeria", "+390490000000", testoMembroProva("2/26", link), "accepted", "notified", chi.Attore, istanteProva}, "|"),
		strings.Join([]string{itoa(uno), "oreficeria", "+390490000000", testoMembroProva("1/26", link), "accepted", "notified", chi.Attore, istanteProva}, "|"),
	}
	if strings.Join(righe, "\n") != strings.Join(atteso, "\n") {
		t.Fatalf("righe:\n%s\natteso:\n%s", strings.Join(righe, "\n"), strings.Join(atteso, "\n"))
	}
	if v := valore(t, d, `SELECT coalesce(avviso_pronta_il, '-') FROM buste WHERE id = ?`, cestino); v != "-" {
		t.Fatalf("busta nel cestino avvisata: %v", v)
	}
	if v := valore(t, d, `SELECT group_concat(coalesce(token_pubblico, '-')) FROM buste`); v != gettoni ||
		conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_link_issued'`) != audPrima {
		t.Fatal("link della busta emesso")
	}
	if a := righeAudit(t, b, "ready_lot.sent"); len(a) != 1 || a[0] != itoa(l)+`|{"bags":2}` {
		t.Fatalf("ready_lot.sent: %v", a)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'ready_notice.sent' AND attore = ? AND dati LIKE '%"party":"oreficeria"%'`, chi.Attore); n != 2 {
		t.Fatalf("ready_notice.sent: %d", n)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.handed_off'`); n != 2 {
		t.Fatalf("outgoing.handed_off: %d", n)
	}
	if v := valore(t, d, `SELECT attore || '|' || risorsa || '|' || esito FROM eventi_audit WHERE azione = 'ready_lot.sent'`); v != chi.Attore+"|ready_lot|ok" {
		t.Fatalf("attore: %v", v)
	}
	// L'ordine nel registro: le buste, poi il lotto.
	if v := valore(t, d, `SELECT azione FROM eventi_audit ORDER BY id DESC LIMIT 1`); v != "ready_lot.sent" {
		t.Fatalf("ultima riga: %v", v)
	}
	// Il membro resta del lotto.
	if lottoDi(t, b, uno) != l || lottoDi(t, b, due) != l {
		t.Fatal("membri staccati")
	}
}

// inventario: ORE-10
// I rifiuti del lotto, senza scrivere niente: un lotto che non c'è «Lotto non disponibile.», vuoto (o con le sole
// buste nel cestino) «Lotto vuoto: …», già inviato o chiuso «Lotto già inviato.»; una seconda volta dopo un invio
// riuscito è «già inviato» e non avvisa di nuovo.
func TestInviaLottoRifiutiDelLotto(t *testing.T) {
	b, d, _ := prova(t)
	vuoto := lottoDel(t, b, 2, istanteProva)
	soloCestino := lottoDel(t, b, 2, istanteProva)
	c := prontaPerLotto(t, b, "")
	nelLotto(t, b, soloCestino, c)
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, c)
	inviato := lottoDel(t, b, 2, istanteProva)
	chiuso := lottoDel(t, b, 2, istanteProva)
	x, y := prontaPerLotto(t, b, ""), prontaPerLotto(t, b, "")
	nelLotto(t, b, inviato, x)
	nelLotto(t, b, chiuso, y)
	esegui(t, d, `UPDATE lotti_avviso SET inviato_il = '2026-10-30T08:00:00.000000Z' WHERE id IN (?, ?)`, inviato, chiuso)
	esegui(t, d, `UPDATE lotti_avviso SET chiuso_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, chiuso)
	prima := scattaInvio(t, b)
	for _, k := range []struct {
		lotto int64
		err   error
	}{{9999, ErrLottoNonDisponibile}, {vuoto, ErrLottoVuoto}, {soloCestino, ErrLottoVuoto}, {inviato, ErrLottoGiaInviato}, {chiuso, ErrLottoGiaInviato}} {
		if _, err := b.InviaLotto(ctx, k.lotto, basePubblicaProva, chi); !errors.Is(err, k.err) {
			t.Fatalf("lotto %d: %v, atteso %v", k.lotto, err, k.err)
		}
	}
	nienteScritto(t, "rifiuti del lotto", prima, scattaInvio(t, b))
	if ErrLottoVuoto.Error() != "Lotto vuoto: aggiungi almeno una busta." || ErrLottoGiaInviato.Error() != "Lotto già inviato." {
		t.Fatal("testi")
	}
	// Due volte: la seconda è «già inviato».
	l := lottoDel(t, b, 2, istanteProva)
	z := prontaPerLotto(t, b, "")
	nelLotto(t, b, l, z)
	if _, err := b.InviaLotto(ctx, l, basePubblicaProva, chi); err != nil {
		t.Fatal(err)
	}
	prima = scattaInvio(t, b)
	if _, err := b.InviaLotto(ctx, l, basePubblicaProva, chi); !errors.Is(err, ErrLottoGiaInviato) {
		t.Fatalf("seconda volta: %v", err)
	}
	nienteScritto(t, "seconda volta", prima, scattaInvio(t, b))
}

// inventario: ORE-10
// Tutto o niente: una busta che non è più pronta (la più vecchia, avvisata per ultima, così la più recente è già
// passata dalla porta quando arriva il rifiuto), già avvisata, o spostata a un'altra oreficeria (anche a nessuna,
// anche dal cestino: la più recente fra quelle) ferma tutto l'invio con «Busta <numero visibile>: <motivo>. Niente è
// partito.» (D-99);
// niente si scrive, il lotto resta aperto.
func TestInviaLottoRifiutiDelleBuste(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO oreficerie (id, nome, telefono) VALUES (3, 'Oreficeria Altra', '0491111111')`)
	for _, k := range []struct {
		nome   string
		cambia func(vecchia, nuova int64)
		busta  func(vecchia, nuova int64) int64
		motivo string
	}{
		{"non pronta", func(v, _ int64) { esegui(t, d, `UPDATE buste SET stato = 'in_lavorazione' WHERE id = ?`, v) },
			func(v, _ int64) int64 { return v }, "non è più pronta"},
		{"consegnata", func(v, _ int64) { esegui(t, d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, v) },
			func(v, _ int64) int64 { return v }, "non è più pronta"},
		{"già avvisata", func(v, _ int64) {
			esegui(t, d, `UPDATE buste SET avviso_pronta_il = '2026-10-30T08:00:00.000000Z' WHERE id = ?`, v)
		}, func(v, _ int64) int64 { return v }, "è già stata avvisata"},
		{"già avvisata e non pronta", func(v, _ int64) {
			esegui(t, d, `UPDATE buste SET avviso_pronta_il = '2026-10-30T08:00:00.000000Z', stato = 'in_lavorazione' WHERE id = ?`, v)
		}, func(v, _ int64) int64 { return v }, "è già stata avvisata"},
		{"altra oreficeria", func(v, _ int64) { esegui(t, d, `UPDATE buste SET oreficeria_id = 3 WHERE id = ?`, v) },
			func(v, _ int64) int64 { return v }, "non è più di questa oreficeria"},
		{"nessuna oreficeria", func(v, _ int64) { esegui(t, d, `UPDATE buste SET oreficeria_id = NULL WHERE id = ?`, v) },
			func(v, _ int64) int64 { return v }, "non è più di questa oreficeria"},
		{"tutte e due altrove", func(v, n int64) { esegui(t, d, `UPDATE buste SET oreficeria_id = 3 WHERE id IN (?, ?)`, v, n) },
			func(_, n int64) int64 { return n }, "non è più di questa oreficeria"},
		{"altrove nel cestino", func(v, _ int64) {
			esegui(t, d, `UPDATE buste SET oreficeria_id = 3, cancellata_il = ? WHERE id = ?`, istanteProva, v)
		}, func(v, _ int64) int64 { return v }, "non è più di questa oreficeria"},
	} {
		l := lottoDel(t, b, 2, istanteProva)
		vecchia, nuova := prontaPerLotto(t, b, ""), prontaPerLotto(t, b, "")
		nelLotto(t, b, l, vecchia, nuova)
		k.cambia(vecchia, nuova)
		prima := scattaInvio(t, b)
		_, err := b.InviaLotto(ctx, l, basePubblicaProva, chi)
		var m *RifiutoMembro
		if !errors.As(err, &m) || m.Busta != k.busta(vecchia, nuova) || m.Motivo != k.motivo {
			t.Fatalf("%s: %v (%+v)", k.nome, err, m)
		}
		// D-99: il numero visibile della busta, non l'id.
		numero := valore(t, d, `SELECT numero FROM buste WHERE id = ?`, m.Busta).(string)
		if want := "Busta N. " + numero + ": " + k.motivo + ". Niente è partito."; err.Error() != want || m.Numero != "N. "+numero {
			t.Fatalf("%s: %q", k.nome, err.Error())
		}
		nienteScritto(t, k.nome, prima, scattaInvio(t, b))
		// Rimessa a posto, il lotto parte: nessun altro resto.
		esegui(t, d, `UPDATE buste SET stato = 'pronta', avviso_pronta_il = NULL, oreficeria_id = 2, cancellata_il = NULL WHERE id IN (?, ?)`, vecchia, nuova)
		if _, err := b.InviaLotto(ctx, l, basePubblicaProva, chi); err != nil {
			t.Fatalf("%s rimessa a posto: %v", k.nome, err)
		}
	}
}

// inventario: ORE-10
// Un testo salvato che non si compone non ricade sul predefinito: quello del lotto («Avviso lotto pronte
// oreficeria») e quello delle buste («Avviso oreficeria») fermano l'invio prima di ogni scrittura, con la famiglia
// e l'avviso; il testo del lotto si guarda per primo.
func TestInviaLottoModelloNonValido(t *testing.T) {
	b, d, _ := prova(t)
	l := lottoDel(t, b, 2, istanteProva)
	nelLotto(t, b, l, prontaPerLotto(t, b, ""))
	salva := func(f Famiglia, testo string) {
		esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?1, ?2, ?3)
			ON CONFLICT (chiave) DO UPDATE SET valore = ?2`, f.Chiave(), testo, istanteProva)
	}
	togli := func(f Famiglia) { esegui(t, d, `DELETE FROM impostazioni WHERE chiave = ?`, f.Chiave()) }
	controlla := func(cosa string, f Famiglia, avviso string) {
		t.Helper()
		prima := scattaInvio(t, b)
		_, err := b.InviaLotto(ctx, l, basePubblicaProva, chi)
		var m ErrModelloLotto
		if !errors.As(err, &m) || m.Famiglia != f || m.Avviso != avviso {
			t.Fatalf("%s: %v %+v", cosa, err, m)
		}
		nienteScritto(t, cosa, prima, scattaInvio(t, b))
	}
	salva(FamOreficeriaLotto, "Lotto {password}")
	controlla("testo del lotto", FamOreficeriaLotto, "Segnaposto non supportato: {password}")
	salva(FamOreficeria, "Ciao {telegram_optin}")
	controlla("tutti e due", FamOreficeriaLotto, "Segnaposto non supportato: {password}")
	togli(FamOreficeriaLotto)
	controlla("testo delle buste", FamOreficeria, "Segnaposto non supportato: {telegram_optin}")
	togli(FamOreficeria)
	if _, err := b.InviaLotto(ctx, l, basePubblicaProva, chi); err != nil {
		t.Fatal(err)
	}
}

// inventario: ORE-10
// Un'oreficeria senza numero (nessun telefono, un telefono senza cifre, cifre che non sono ASCII): il lotto parte lo
// stesso, il numero dell'esito è vuoto e ogni riga delle buste dice «contatto-scelto» (contatto scelto sul telefono).
func TestInviaLottoSenzaNumero(t *testing.T) {
	for _, tel := range []any{nil, "", "da chiedere", "³⁴⁷"} {
		b, d, _ := prova(t)
		esegui(t, d, `UPDATE oreficerie SET telefono = ? WHERE id = 2`, tel)
		l := lottoDel(t, b, 2, istanteProva)
		uno := prontaPerLotto(t, b, "")
		nelLotto(t, b, l, uno)
		x, err := b.InviaLotto(ctx, l, basePubblicaProva, chi)
		if err != nil || x.Numero != "" || !strings.Contains(x.Messaggio, "/lotto/") {
			t.Fatalf("%v: %+v %v", tel, x, err)
		}
		if v := valore(t, d, `SELECT destinatario || '|' || indirizzo FROM messaggi_in_uscita WHERE busta_id = ?`, uno); v != "oreficeria|contatto-scelto" {
			t.Fatalf("%v: riga %v", tel, v)
		}
		if valore(t, d, `SELECT avviso_pronta_il FROM buste WHERE id = ?`, uno) == nil {
			t.Fatalf("%v: non avvisata", tel)
		}
	}
}

// inventario: ORE-10
// Senza indirizzo pubblico (D-46 estesa ai testi con {link}, slice 067): un testo del lotto o delle buste con {link}
// non parte e niente si scrive (ErrSenzaIndirizzoPubblico); con i due testi senza {link} il lotto parte e il link
// dell'esito è il solo percorso /lotto/<gettone>, come la pagina del lotto uscito (089).
func TestInviaLottoSenzaIndirizzoPubblico(t *testing.T) {
	b, d, _ := prova(t)
	l := lottoDel(t, b, 2, istanteProva)
	uno := prontaPerLotto(t, b, "")
	nelLotto(t, b, l, uno)
	salva := func(f Famiglia, testo string) {
		esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?1, ?2, ?3)
			ON CONFLICT (chiave) DO UPDATE SET valore = ?2`, f.Chiave(), testo, istanteProva)
	}
	for _, k := range []struct {
		lotto, busta string
	}{{"", ""}, {"Lotto di {oref_nome}: {n_buste} buste. {negozio}", ""}, {"", "Busta {numero} pronta. {negozio}"}} {
		esegui(t, d, `DELETE FROM impostazioni WHERE chiave LIKE 'outgoing.template.%'`)
		if k.lotto != "" {
			salva(FamOreficeriaLotto, k.lotto)
		}
		if k.busta != "" {
			salva(FamOreficeria, k.busta)
		}
		prima := scattaInvio(t, b)
		if _, err := b.InviaLotto(ctx, l, "", chi); !errors.Is(err, ErrSenzaIndirizzoPubblico) {
			t.Fatalf("%+v: %v", k, err)
		}
		nienteScritto(t, "senza indirizzo", prima, scattaInvio(t, b))
	}
	salva(FamOreficeriaLotto, "Lotto di {oref_nome}: {n_buste} buste. {negozio}")
	salva(FamOreficeria, "Busta {numero} pronta. {negozio}")
	x, err := b.InviaLotto(ctx, l, "", chi)
	if err != nil || x.Link != "/lotto/"+gettoneDelLotto(t, b, l) || x.Messaggio != "Lotto di Oreficeria Esempio: 1 buste. Negozio" {
		t.Fatalf("%+v %v", x, err)
	}
	if v := valore(t, d, `SELECT testo FROM messaggi_in_uscita WHERE busta_id = ?`, uno); v != "Busta 1/26 pronta. Negozio" {
		t.Fatalf("riga: %v", v)
	}
}

// inventario: ORE-10
// Un guasto a metà (dopo l'avviso della prima busta) annulla tutto: il lotto resta aperto, nessuna busta avvisata,
// nessuna riga, nessun audit.
func TestInviaLottoGuastoAMeta(t *testing.T) {
	b, _, _ := prova(t)
	l := lottoDel(t, b, 2, istanteProva)
	uno, due, tre := prontaPerLotto(t, b, ""), prontaPerLotto(t, b, ""), prontaPerLotto(t, b, "")
	nelLotto(t, b, l, uno, due, tre)
	guasto := errors.New("guasto a metà")
	var viste []int64
	b.dopoAvvisoNelLotto = func(tx *sql.Tx, busta int64) error {
		viste = append(viste, busta)
		var n int
		if err := tx.QueryRow(`SELECT count(*) FROM messaggi_in_uscita`).Scan(&n); err != nil || n != len(viste) {
			t.Errorf("nella transazione %d righe (%v)", n, err)
		}
		if len(viste) == 2 {
			return guasto
		}
		return nil
	}
	prima := scattaInvio(t, b)
	if _, err := b.InviaLotto(ctx, l, basePubblicaProva, chi); !errors.Is(err, guasto) {
		t.Fatalf("guasto: %v", err)
	}
	if len(viste) != 2 || viste[0] != tre || viste[1] != due {
		t.Fatalf("ordine: %v", viste)
	}
	nienteScritto(t, "guasto a metà", prima, scattaInvio(t, b))
	b.dopoAvvisoNelLotto = nil
	if _, err := b.InviaLotto(ctx, l, basePubblicaProva, chi); err != nil {
		t.Fatal(err)
	}
}

// inventario: ORE-10
// Dodici invii insieme dello stesso lotto: uno parte, gli altri «Lotto già inviato.»; un solo ready_lot.sent e un
// avviso per busta.
func TestInviaLottoGara(t *testing.T) {
	b, d, _ := prova(t)
	l := lottoDel(t, b, 2, istanteProva)
	nelLotto(t, b, l, prontaPerLotto(t, b, ""), prontaPerLotto(t, b, ""))
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, gia := 0, 0
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := b.InviaLotto(ctx, l, basePubblicaProva, chi)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrLottoGiaInviato):
				gia++
			default:
				t.Errorf("gara: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok != 1 || gia != 11 || conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'ready_lot.sent'`) != 1 ||
		conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'ready_notice.sent'`) != 2 ||
		conta(t, d, `SELECT count(*) FROM messaggi_in_uscita`) != 2 {
		t.Fatalf("gara: %d ok, %d già", ok, gia)
	}
}

// inventario: ORE-10
// Dopo l'invio una busta del lotto non si riavvisa da sola («già avvisata»); azzerato l'avviso, l'avviso singolo
// riparte e la busta resta del lotto.
func TestInviaLottoPoiAvvisoSingolo(t *testing.T) {
	b, _, _ := prova(t)
	l := lottoDel(t, b, 2, istanteProva)
	uno := prontaPerLotto(t, b, "")
	nelLotto(t, b, l, uno)
	// Prima dell'invio l'avviso singolo rifiuta la busta del lotto aperto.
	if _, err := b.AnnunciaRitiro(ctx, uno, basePubblicaProva, chi); !errors.Is(err, ErrInLottoAperto) {
		t.Fatalf("lotto aperto: %v", err)
	}
	if _, err := b.InviaLotto(ctx, l, basePubblicaProva, chi); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AnnunciaRitiro(ctx, uno, basePubblicaProva, chi); !errors.Is(err, ErrGiaAvvisata) {
		t.Fatalf("dopo l'invio: %v", err)
	}
	if err := b.AzzeraAvviso(ctx, uno, chi); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AnnunciaRitiro(ctx, uno, basePubblicaProva, chi); err != nil {
		t.Fatalf("dopo l'azzeramento: %v", err)
	}
	if lottoDi(t, b, uno) != l {
		t.Fatal("staccata dal lotto")
	}
}

// inventario: ORE-10
// Il blocco decide, non la lettura: una busta cestinata fra la lettura e la scrittura del suo avviso ferma l'invio con
// «Busta <numero>: non esiste più. Niente è partito.» (claim/3 non vede il cestino) e niente si scrive; lo stesso per una
// busta che nel frattempo smette di essere pronta o viene avvisata.
func TestInviaLottoBloccoDecide(t *testing.T) {
	for _, k := range []struct {
		nome, sql, motivo string
	}{
		{"cestinata", `UPDATE buste SET cancellata_il = '2026-10-31T09:00:00.000000Z' WHERE id = ?`, "non esiste più"},
		{"non pronta", `UPDATE buste SET stato = 'in_lavorazione' WHERE id = ?`, "non è più pronta"},
		{"avvisata", `UPDATE buste SET avviso_pronta_il = '2026-10-31T09:00:00.000000Z' WHERE id = ?`, "è già stata avvisata"},
	} {
		b, _, _ := prova(t)
		l := lottoDel(t, b, 2, istanteProva)
		uno := prontaPerLotto(t, b, "")
		nelLotto(t, b, l, uno)
		b.primaDelBloccoNelLotto = func(tx *sql.Tx, busta int64) error {
			_, err := tx.Exec(k.sql, busta)
			return err
		}
		prima := scattaInvio(t, b)
		_, err := b.InviaLotto(ctx, l, basePubblicaProva, chi)
		var m *RifiutoMembro
		if !errors.As(err, &m) || m.Busta != uno || m.Motivo != k.motivo {
			t.Fatalf("%s: %v", k.nome, err)
		}
		nienteScritto(t, k.nome, prima, scattaInvio(t, b))
	}
}

// inventario: ORE-10
// D-99 (Stefano, 17/11/2026): il rifiuto di una busta nomina il numero visibile (D-64), «#0045/26» per una busta con
// l'etichetta, mai l'id; anche per una busta cestinata (letta dal cestino) e per quella di un'altra oreficeria.
func TestInviaLottoRifiutoConNumeroVisibile(t *testing.T) {
	b, d, _ := prova(t)
	l := lottoDel(t, b, 2, istanteProva)
	uno := prontaPerLotto(t, b, "RIP-2026-0045")
	nelLotto(t, b, l, uno)
	esegui(t, d, `UPDATE buste SET stato = 'in_lavorazione' WHERE id = ?`, uno)
	if _, err := b.InviaLotto(ctx, l, basePubblicaProva, chi); err == nil || err.Error() != "Busta #0045/26: non è più pronta. Niente è partito." {
		t.Fatalf("non pronta: %v", err)
	}
	esegui(t, d, `UPDATE buste SET stato = 'pronta', oreficeria_id = NULL, cancellata_il = ? WHERE id = ?`, istanteProva, uno)
	if _, err := b.InviaLotto(ctx, l, basePubblicaProva, chi); err == nil || err.Error() != "Busta #0045/26: non è più di questa oreficeria. Niente è partito." {
		t.Fatalf("altrove nel cestino: %v", err)
	}
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2, cancellata_il = NULL WHERE id = ?`, uno)
	b.primaDelBloccoNelLotto = func(tx *sql.Tx, busta int64) error {
		_, err := tx.Exec(`UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, busta)
		return err
	}
	if _, err := b.InviaLotto(ctx, l, basePubblicaProva, chi); err == nil || err.Error() != "Busta #0045/26: non esiste più. Niente è partito." {
		t.Fatalf("cestinata: %v", err)
	}
}
