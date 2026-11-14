package buste

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// Le risposte del cliente a una notifica Telegram (CLI-49, slice 075), come TelegramReplies.bag/2 e post/3
// e telegram_inbound_reply_test.exs dello standalone a go-baseline; l'audit del mancato avviso del
// preventivo (D-86); lo spegnimento che non si riaccende (CLI-50). Chat inventate sopra 2^52.

// notificaTG scrive una riga Telegram della coda: la notifica alla chat con il riferimento (message_id) dato,
// accodata in quel momento.
func notificaTG(t *testing.T, d *db.DB, busta any, parte, chat, stato, rif, accodata string) int64 {
	t.Helper()
	r, err := d.Scrittura.Exec(`INSERT INTO messaggi_in_uscita (busta_id, canale, destinatario, indirizzo, testo, stato, tentativi,
		prossimo_tentativo_il, accodato_il, riferimento_provider) VALUES (?, 'telegram', ?, ?, 'Ciao', ?, 1, ?5, ?5, ?6)`,
		busta, parte, chat, stato, accodata, rif)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := r.LastInsertId()
	return id
}

// fraGiorni è istanteProva spostato di d.
func fraGiorni(d time.Duration) string {
	return orologio().Adesso().UTC().Add(d).Format(db.FormatoIstante)
}

// inventario: CLI-49
// La busta della notifica a cui il cliente risponde (bag/2): la riga Telegram del cliente accettata con quel
// message_id, a quella chat, accodata da non più di 90 giorni, con la busta; la più recente se sono più
// d'una. Niente per un'altra chat, una riga in coda o fallita, del negozio, più vecchia di 90 giorni, senza
// busta, con la chat già cancellata dalla pulizia, o di una busta che ha tagliato la chat con il link
// (finché non si iscrive di nuovo); un /stop non conta, né il taglio di un'altra busta.
func TestBustaDellaRisposta(t *testing.T) {
	b, d, _ := prova(t)
	uno, due := crea(t, b, modulo()), crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-DUE" }))
	chat := strconv.FormatInt(chatUno, 10)
	cerca := func(c, m int64) int64 {
		t.Helper()
		id, err := b.BustaDellaRisposta(ctx, c, m)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	novanta := 90 * 24 * time.Hour
	notificaTG(t, d, uno.ID, "customer", chat, "accepted", "501", fraGiorni(-novanta))
	notificaTG(t, d, uno.ID, "customer", chat, "accepted", "502", fraGiorni(-novanta-time.Microsecond))
	notificaTG(t, d, uno.ID, "customer", chat, "queued", "503", istanteProva)
	notificaTG(t, d, uno.ID, "customer", chat, "failed", "504", istanteProva)
	notificaTG(t, d, uno.ID, "shop", chat, "accepted", "505", istanteProva)
	notificaTG(t, d, nil, "customer", chat, "accepted", "506", istanteProva)
	notificaTG(t, d, uno.ID, "customer", "cancellato", "accepted", "507", istanteProva)
	notificaTG(t, d, uno.ID, "customer", chat, "accepted", "508", istanteProva)
	notificaTG(t, d, due.ID, "customer", chat, "accepted", "508", istanteProva)
	notificaTG(t, d, uno.ID, "customer", chat, "accepted", "509", istanteProva)
	esegui(t, d, `UPDATE messaggi_in_uscita SET canale = 'email' WHERE riferimento_provider = '509'`)
	casi := []struct {
		chat, messaggio, busta int64
	}{
		{chatUno, 501, uno.ID}, {chatDue, 501, 0}, {chatUno, 502, 0}, {chatUno, 503, 0}, {chatUno, 504, 0},
		{chatUno, 505, 0}, {chatUno, 506, 0}, {chatUno, 507, 0}, {chatUno, 508, due.ID}, {chatUno, 509, 0}, {chatUno, 77, 0},
	}
	for _, k := range casi {
		if got := cerca(k.chat, k.messaggio); got != k.busta {
			t.Errorf("chat %d messaggio %d: busta %d, attesa %d", k.chat, k.messaggio, got, k.busta)
		}
	}
	// Il link della busta tagliato: la chat non risponde più alle notifiche di quella busta, anche se aveva
	// fatto /stop prima; un'altra busta tagliata non conta; un /start nuovo (motivo tolto) la riapre.
	iscrivi(t, d, due.ID, chatUno, 0, istanteProva)
	esegui(t, d, `UPDATE iscrizioni_telegram SET motivo_fermata = 'stop'`)
	if cerca(chatUno, 508) != due.ID {
		t.Error("una chat fermata con /stop non risponde più")
	}
	esegui(t, d, `UPDATE iscrizioni_telegram SET motivo_fermata = 'link'`)
	if cerca(chatUno, 508) != 0 || cerca(chatUno, 501) != uno.ID {
		t.Error("il taglio del link")
	}
	iscrivi(t, d, due.ID, chatDue, 0, nil)
	esegui(t, d, `UPDATE iscrizioni_telegram SET motivo_fermata = 'link' WHERE chat_id = ?`, chatDue)
	esegui(t, d, `UPDATE iscrizioni_telegram SET motivo_fermata = NULL WHERE chat_id = ?`, chatUno)
	if cerca(chatUno, 508) != due.ID {
		t.Error("dopo un /start nuovo")
	}
	// Review della 075 (R13): se le chat tagliate non si leggono, niente busta e l'errore torna.
	esegui(t, d, `ALTER TABLE iscrizioni_telegram RENAME TO iscrizioni_altrove`)
	if id, err := b.BustaDellaRisposta(ctx, chatUno, 508); err == nil || id != 0 {
		t.Errorf("chat tagliate illeggibili: %d %v", id, err)
	}
}

// scrittoDopo è il dopo dei test: una riga di impostazioni nella transazione della risposta.
func scrittoDopo(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('prova.dopo', '1', ?)`, istanteProva)
	return err
}

// inventario: CLI-49, CLI-13
// La risposta entra nella conversazione come il messaggio della pagina (PublicMessage.post/5 dal lato
// :telegram): ripulita, del cliente, senza canale, la busta non letta, repair.message_appended con
// l'attore «telegram» e senza il testo; dopo nella stessa transazione; poi l'avviso alla chat del negozio
// «dal cliente» (CLI-13) con l'attore «telegram». Mai la chat nell'audit.
func TestRispostaTelegram(t *testing.T) {
	b, d, _ := provaAvvisi(t)
	c := bustaTG(t, b)
	esegui(t, d, `UPDATE buste SET messaggi_letti_il = ? WHERE id = ?`, istanteProva, c.ID)
	if err := b.RispostaTelegram(ctx, c.ID, "   Va bene, passo sabato\n ", scrittoDopo); err != nil {
		t.Fatal(err)
	}
	if v := valore(t, d, `SELECT autore || '|' || coalesce(canale, '-') || '|' || testo || '|' || inviato_il FROM messaggi_buste WHERE busta_id = ?`, c.ID); v !=
		"customer|-|Va bene, passo sabato|"+istanteProva {
		t.Errorf("messaggio %v", v)
	}
	if v := valore(t, d, `SELECT coalesce(messaggi_letti_il, '-') || '|' || ultimo_messaggio_il FROM buste WHERE id = ?`, c.ID); v != "-|"+istanteProva {
		t.Errorf("busta %v", v)
	}
	if conta(t, d, `SELECT count(*) FROM impostazioni WHERE chiave = 'prova.dopo'`) != 1 {
		t.Error("dopo non scritto")
	}
	if v := valore(t, d, `SELECT attore || '|' || dati FROM eventi_audit WHERE azione = 'repair.message_appended'`); v !=
		`telegram|{"author":"customer","bag_number":"1/26","channel":null,"message_id":1}` {
		t.Errorf("audit %v", v)
	}
	if got := righeNegozio(t, d); fmt.Sprint(got) != fmt.Sprint([]string{"💬 Messaggio dal cliente — busta 1/26\n\nVa bene, passo sabato\n\nApri la scheda: https://interno.example.invalid:8443/repairs/" + itoa(c.ID)}) {
		t.Errorf("avviso %q", got)
	}
	if v := valore(t, d, `SELECT attore FROM eventi_audit WHERE azione = 'outgoing.queued'`); v != "telegram" {
		t.Errorf("attore dell'avviso %v", v)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE instr(coalesce(dati, ''), ?) > 0`, strconv.FormatInt(chatUno, 10)) != 0 {
		t.Error("chat nell'audit")
	}
	// Mille caratteri passano; dopo che fallisce annulla tutto.
	if err := b.RispostaTelegram(ctx, c.ID, strings.Repeat("a", MessaggioMassimo), func(context.Context, *sql.Tx) error { return nil }); err != nil {
		t.Errorf("1000 caratteri: %v", err)
	}
	errDopo := errors.New("dopo che fallisce")
	if err := b.RispostaTelegram(ctx, c.ID, "Annullato", func(context.Context, *sql.Tx) error { return errDopo }); !errors.Is(err, errDopo) {
		t.Errorf("dopo che fallisce: %v", err)
	}
	if conta(t, d, `SELECT count(*) FROM messaggi_buste WHERE busta_id = ?`, c.ID) != 2 || len(righeNegozio(t, d)) != 2 {
		t.Error("scritto con il dopo fallito")
	}
}

// inventario: CLI-49, CLI-12
// I rifiuti, senza scrivere niente (né il messaggio, né dopo, né l'avviso): busta che non c'è o nel
// cestino, anche con un testo vuoto o troppo lungo (la busta prima, come Repo.get/2 di post/3); testo vuoto
// o di 1001 code point; busta chiusa, anche se è di un'oreficeria; busta di un'oreficeria; dieci messaggi
// web della controparte nell'ultima ora, contati con quelli della pagina (e la risposta conta per la
// pagina).
func TestRispostaTelegramRifiuti(t *testing.T) {
	b, d, _ := provaAvvisi(t)
	cestino, chiusa, oref, chiusaOref := bustaTG(t, b), bustaTG(t, b), bustaTG(t, b), bustaTG(t, b)
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestino.ID)
	esegui(t, d, `UPDATE buste SET stato = 'consegnata' WHERE id IN (?, ?)`, chiusa.ID, chiusaOref.ID)
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2 WHERE id IN (?, ?)`, oref.ID, chiusaOref.ID)
	aperta := bustaTG(t, b)
	lungo := "e" + strings.Repeat("́", MessaggioMassimo)
	casi := []struct {
		nome  string
		busta int64
		testo string
		err   error
	}{
		{"inesistente", 9999, "Ciao", uscita.ErrRispostaNonTrovata},
		{"inesistente, vuoto", 9999, "  ", uscita.ErrRispostaNonTrovata},
		{"cestino, troppo lungo", cestino.ID, lungo, uscita.ErrRispostaNonTrovata},
		{"vuoto", aperta.ID, " \n ", uscita.ErrRispostaVuota},
		{"troppo lungo", aperta.ID, lungo, uscita.ErrRispostaTroppoLunga},
		{"chiusa, troppo lungo", chiusa.ID, lungo, uscita.ErrRispostaTroppoLunga},
		{"chiusa", chiusa.ID, "Ciao", uscita.ErrRispostaChiusa},
		{"chiusa e dell'oreficeria", chiusaOref.ID, "Ciao", uscita.ErrRispostaChiusa},
		{"oreficeria", oref.ID, "Ciao", uscita.ErrRispostaNonTrovata},
		{"cestino", cestino.ID, "Ciao", uscita.ErrRispostaNonTrovata},
	}
	chiamato := false
	for _, k := range casi {
		err := b.RispostaTelegram(ctx, k.busta, k.testo, func(context.Context, *sql.Tx) error { chiamato = true; return nil })
		if !errors.Is(err, k.err) {
			t.Errorf("%s: %v, atteso %v", k.nome, err, k.err)
		}
	}
	if chiamato || conta(t, d, `SELECT count(*) FROM messaggi_buste`) != 0 || len(righeTG(t, d)) != 0 ||
		conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'repair.message%'`) != 0 {
		t.Error("un rifiuto ha scritto qualcosa")
	}
	// Review della 075 (R22): il link storico revocato non conta per Telegram (open/2 con :telegram guarda
	// solo chiusura e controparte), come lo standalone: la risposta passa.
	revocata := bustaTG(t, b)
	esegui(t, d, `UPDATE buste SET codice_flask_revocato_il = ? WHERE id = ?`, istanteProva, revocata.ID)
	if err := b.RispostaTelegram(ctx, revocata.ID, "Con il link storico revocato", func(context.Context, *sql.Tx) error { return nil }); err != nil {
		t.Errorf("link storico revocato: %v", err)
	}
	for n := range 9 {
		if err := b.ScriviMessaggioCliente(ctx, aperta.Token, "Dalla pagina "+strconv.Itoa(n), pubblico); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.RispostaTelegram(ctx, aperta.ID, "Il decimo", scrittoDopo); err != nil {
		t.Fatalf("il decimo: %v", err)
	}
	if err := b.ScriviMessaggioCliente(ctx, aperta.Token, "L'undicesimo", pubblico); !errors.Is(err, ErrTroppiMessaggi) {
		t.Errorf("la pagina dopo la risposta: %v", err)
	}
	if err := b.RispostaTelegram(ctx, aperta.ID, "L'undicesimo", func(context.Context, *sql.Tx) error { chiamato = true; return nil }); !errors.Is(err, uscita.ErrRispostaTroppe) || chiamato {
		t.Errorf("l'undicesimo: %v", err)
	}
}

// inventario: CLI-49, CLI-46
// Il giro con il lettore vero e un Bot API finto: il negozio scrive, la notifica parte (accettata con il
// message_id di Telegram), il cliente risponde con «Rispondi»: la risposta entra nella conversazione, il
// negozio è avvisato, il bot conferma citando il messaggio e l'offset va avanti. Una chat che ha fatto
// /stop risponde ancora; una tagliata con il link no (risposta fissa).
func TestRispostaTelegramDalBot(t *testing.T) {
	b, d, o := provaAvvisiConLettore(t)
	bot := nuovoBotDiProva(t)
	l := uscita.NuovoLettoreTelegram(d, o, uscita.NuovoTelegram(d, bot.srv.URL), b, baseEmail)
	c := bustaTG(t, b)
	iscrivi(t, d, c.ID, chatUno, 1, nil)
	if n, err := b.NotificaNuovoMessaggio(ctx, c.ID, "Il preventivo è pronto", chi); err != nil || n != 1 {
		t.Fatalf("notifica %d %v", n, err)
	}
	esegui(t, d, `UPDATE messaggi_in_uscita SET stato = 'accepted', riferimento_provider = '501' WHERE destinatario = 'customer'`)
	leggi := func(agg ...any) []map[string]any {
		t.Helper()
		bot.arrivano(agg...)
		if _, err := l.Leggi(context.Background()); err != nil {
			t.Fatal(err)
		}
		return bot.mandati()
	}
	risposta := func(id int64, testo string) map[string]any {
		m := messaggioBot(id, chatUno, testo)
		m["message"].(map[string]any)["reply_to_message"] = map[string]any{"message_id": 501}
		return m
	}
	m := leggi(risposta(40, "Va bene, grazie"))
	if len(m) != 1 || m[0]["text"] != "✅ Messaggio inviato al negozio." || fmt.Sprint(m[0]["reply_parameters"]) != "map[allow_sending_without_reply:true message_id:40]" {
		t.Errorf("conferma %v", m)
	}
	if v := valore(t, d, `SELECT group_concat(testo, '|') FROM messaggi_buste WHERE busta_id = ? AND autore = 'customer'`, c.ID); v != "Va bene, grazie" {
		t.Errorf("conversazione %v", v)
	}
	if v := valore(t, d, `SELECT group_concat(indirizzo || '|' || testo, ',') FROM messaggi_in_uscita WHERE destinatario = 'shop'`); v !=
		chatShop+"|💬 Messaggio dal cliente — busta 1/26\n\nVa bene, grazie\n\nApri la scheda: https://interno.example.invalid:8443/repairs/"+itoa(c.ID) {
		t.Errorf("avviso al negozio %v", v)
	}
	if v := valore(t, d, `SELECT valore FROM impostazioni WHERE chiave = 'telegram_inbound.offset'`); v != "41" {
		t.Errorf("offset %v", v)
	}
	if _, _, err := b.FermaChatTelegram(ctx, chatUno); err != nil {
		t.Fatal(err)
	}
	if m := leggi(risposta(41, "Ancora io")); len(m) != 1 || m[0]["text"] != "✅ Messaggio inviato al negozio." {
		t.Errorf("dopo /stop %v", m)
	}
	esegui(t, d, `UPDATE iscrizioni_telegram SET motivo_fermata = 'link'`)
	if m := leggi(risposta(42, "Sono ancora qui")); len(m) != 1 || !strings.HasPrefix(m[0]["text"].(string), "Questo è il bot di ") {
		t.Errorf("dopo il taglio %v", m)
	}
	if n := conta(t, d, `SELECT count(*) FROM messaggi_buste WHERE busta_id = ? AND autore = 'customer'`, c.ID); n != 2 {
		t.Errorf("messaggi del cliente %d", n)
	}
}

// provaAvvisiConLettore è provaAvvisi con l'invio Telegram acceso (D-03), perché il lettore legga.
func provaAvvisiConLettore(t *testing.T) (*Buste, *db.DB, clock.Orologio) {
	t.Helper()
	b, d, _ := provaAvvisi(t)
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, '1', ?)`, uscita.ChiaveTelegramAcceso, istanteProva)
	return b, d, b.o
}

// inventario: CLI-08
// D-86 (solo Go): quando l'avviso della risposta al preventivo alla chat del negozio non parte, una riga
// repair.quote_notice_not_queued con il numero della busta e il motivo, come repair.message_notice_not_queued
// (not_configured senza coda, configurazione o canale; error con la coda che rifiuta o va in panico). La
// risposta resta; con l'avviso partito nessuna riga.
func TestAvvisoPreventivoNonAccodato(t *testing.T) {
	b, d, p := provaAvvisi(t)
	posta := b.posta
	casi := []struct {
		nome, motivo string
		prima, dopo  func()
	}{
		{"senza coda", "not_configured", func() { b.posta = nil }, func() { b.posta = posta }},
		{"senza chat", "not_configured", func() { esegui(t, d, `UPDATE impostazioni SET valore = '' WHERE chiave = 'outgoing.telegram.chat_id'`) },
			func() {
				esegui(t, d, `UPDATE impostazioni SET valore = ? WHERE chiave = 'outgoing.telegram.chat_id'`, chatShop)
			}},
		{"canale spento", "not_configured", func() { p.spento = true }, func() { p.spento = false }},
		{"coda che rifiuta", "error", func() { b.posta = postaRifiutaTutto{posta} }, func() { b.posta = posta }},
		{"coda in panico", "error", func() { b.posta = postaInPanico{posta} }, func() { b.posta = posta }},
	}
	for i, k := range casi {
		c := bustaTG(t, b)
		// Review della 075 (R15): un'etichetta, così il numero dei testi («0045/26») non è quello salvato.
		esegui(t, d, `UPDATE buste SET stato = 'preventivo', codice_qr = 'RIP-2026-0045' WHERE id = ?`, c.ID)
		k.prima()
		var err error
		if i%2 == 0 {
			err = b.ApprovaPreventivo(ctx, c.Token, pubblico)
		} else {
			err = b.RifiutaPreventivo(ctx, c.Token, "caro", pubblico)
		}
		k.dopo()
		if err != nil {
			t.Fatalf("%s: %v", k.nome, err)
		}
		if NumeroNeiTesti("RIP-2026-0045", numeroDi(t, d, c.ID)) == numeroDi(t, d, c.ID) {
			t.Fatal("numero dei testi uguale a quello salvato")
		}
		atteso := `public|repair|` + itoa(c.ID) + `|ok|{"bag_number":"` + numeroDi(t, d, c.ID) + `","reason":"` + k.motivo + `"}`
		if v := valore(t, d, `SELECT attore || '|' || risorsa || '|' || id_risorsa || '|' || esito || '|' || dati FROM eventi_audit
			WHERE azione = 'repair.quote_notice_not_queued' ORDER BY id DESC LIMIT 1`); v != atteso {
			t.Errorf("%s: %v, atteso %s", k.nome, v, atteso)
		}
	}
	c := bustaTG(t, b)
	esegui(t, d, `UPDATE buste SET stato = 'preventivo' WHERE id = ?`, c.ID)
	if err := b.ApprovaPreventivo(ctx, c.Token, pubblico); err != nil {
		t.Fatal(err)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.quote_notice_not_queued'`); n != len(casi) || len(righeNegozio(t, d)) != 1 {
		t.Errorf("righe del mancato avviso %d", n)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = ?`, azioneAvvisoNonAccodato) != 0 {
		t.Error("riga del messaggio per il preventivo")
	}
}

// inventario: CLI-50
// Una busta riaperta, tolta dal cestino o tornata al cliente diretto non riaccende niente: le iscrizioni
// spente restano spente (con il loro motivo e la loro ora) e nessun token torna; il cliente si iscrive di
// nuovo dalla pagina.
func TestTelegramNonSiRiaccende(t *testing.T) {
	b, d, o := prova(t)
	chiusa, cestino, oref := crea(t, b, modulo()), crea(t, b, modulo()), crea(t, b, modulo())
	for i, id := range []int64{chiusa.ID, cestino.ID, oref.ID} {
		iscrivi(t, d, id, int64(700+i), 1, nil)
	}
	for _, s := range []string{"pronta", "consegnata"} {
		if _, err := b.CambiaStato(ctx, chiusa.ID, s, chi); err != nil {
			t.Fatal(err)
		}
	}
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestino.ID)
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, oref.ID)
	prima := valore(t, d, `SELECT group_concat(consenso || coalesce(fermata_il, '-') || coalesce(motivo_fermata, '-'), ',') FROM iscrizioni_telegram`).(string)
	o.Sposta(time.Hour)
	if _, err := b.Riapri(ctx, chiusa.ID, "pronta", "Riaperta per prova", chi); err != nil {
		t.Fatal(err)
	}
	esegui(t, d, `UPDATE buste SET cancellata_il = NULL WHERE id = ?`, cestino.ID)
	esegui(t, d, `UPDATE buste SET oreficeria_id = NULL WHERE id = ?`, oref.ID)
	if dopo := valore(t, d, `SELECT group_concat(consenso || coalesce(fermata_il, '-') || coalesce(motivo_fermata, '-'), ',') FROM iscrizioni_telegram`); dopo != prima ||
		!strings.Contains(prima, "consegnata") || !strings.Contains(prima, "deleted") || !strings.Contains(prima, "oreficeria") {
		t.Errorf("prima %v, dopo %v", prima, dopo)
	}
	if conta(t, d, `SELECT count(*) FROM token_telegram`) != 0 || conta(t, d, `SELECT count(*) FROM iscrizioni_telegram WHERE consenso = 1 OR fermata_il IS NULL`) != 0 {
		t.Error("qualcosa si è riacceso")
	}
}
