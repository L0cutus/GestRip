package buste

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// I comandi del bot sulle iscrizioni vere (slice 073, CLI-46), come TelegramSubscriptions.start/2,
// consent/3 e stop_chat/1 dello standalone a go-baseline (telegram_inbound_customer_test.exs): /start
// <token> consuma il token della 072, il pulsante scrive il consenso, /stop spegne le iscrizioni della
// chat. Chat inventate sopra 2^52.

const (
	chatBot      int64 = 4_503_599_627_370_497
	altraChatBot int64 = 4_503_599_627_370_499
)

// tokenDiPagina è il token del pulsante della pagina della busta (la 072), da dare a /start.
func tokenDiPagina(t *testing.T, b *Buste, c Creata) string {
	t.Helper()
	link, err := b.IscriviTelegramCliente(ctx, c.Token, baseEmail, pubblico)
	if err != nil {
		t.Fatal(err)
	}
	return startDi(t, link)
}

// tokenScritto mette un token della busta direttamente nel database (come lo lascia l'import), con la
// scadenza data.
func tokenScritto(t *testing.T, d *db.DB, busta int64, token, scade string) {
	t.Helper()
	h := sha256.Sum256([]byte(token))
	esegui(t, d, `INSERT INTO token_telegram (busta_id, hash_token, scade_il, creato_il) VALUES (?, ?, ?, ?)`, busta, h[:], scade, istanteProva)
}

// iscrizione è la riga dell'iscrizione come testo: busta|chat|consenso|collegata|consenso_il|fermata|motivo.
func rigaIscrizione(t *testing.T, d *db.DB, id int64) string {
	t.Helper()
	var s string
	if err := d.Lettura.QueryRow(`SELECT busta_id || '|' || chat_id || '|' || consenso || '|' || collegata_il || '|' || coalesce(consenso_il, '-')
		|| '|' || coalesce(fermata_il, '-') || '|' || coalesce(motivo_fermata, '-') FROM iscrizioni_telegram WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// audit è l'ultima riga d'audit dell'azione: attore|risorsa|id|dati.
func auditDelBot(t *testing.T, d *db.DB, azione string) string {
	t.Helper()
	var s string
	if err := d.Lettura.QueryRow(`SELECT attore || '|' || risorsa || '|' || id_risorsa || '|' || coalesce(dati, '') FROM eventi_audit
		WHERE azione = ? ORDER BY id DESC LIMIT 1`, azione).Scan(&s); err != nil {
		t.Fatalf("%s: %v", azione, err)
	}
	return s
}

func numeroDi(t *testing.T, d *db.DB, id int64) string {
	t.Helper()
	var n string
	if err := d.Lettura.QueryRow(`SELECT numero FROM buste WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// inventario: CLI-46
// /start <token>: il token si usa una volta (la riga se ne va), la chat si iscrive alla busta con il
// consenso spento, repair.telegram_linked con l'attore «telegram», la busta e l'iscrizione, mai la chat né
// il token. Lo stesso token una seconda volta (anche da un'altra chat) è un link non valido.
func TestAvviaTelegram(t *testing.T) {
	b, d, o, _ := provaTelegram(t)
	c := crea(t, b, modulo())
	tok := tokenDiPagina(t, b, c)
	id, err := b.AvviaTelegram(ctx, chatBot, tok)
	if err != nil {
		t.Fatal(err)
	}
	ora := o.Adesso().UTC().Format(db.FormatoIstante)
	if got := rigaIscrizione(t, d, id); got != strconv.FormatInt(c.ID, 10)+"|"+strconv.FormatInt(chatBot, 10)+"|0|"+ora+"|-|-|-" {
		t.Errorf("iscrizione %s", got)
	}
	if n := conta(t, d, `SELECT count(*) FROM token_telegram`); n != 0 {
		t.Errorf("token rimasti: %d", n)
	}
	atteso := `telegram|repair|` + strconv.FormatInt(c.ID, 10) + `|{"bag_number":"` + numeroDi(t, d, c.ID) + `","subscription_id":` + strconv.FormatInt(id, 10) + `}`
	if got := auditDelBot(t, d, "repair.telegram_linked"); got != atteso {
		t.Errorf("audit %s, atteso %s", got, atteso)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE instr(coalesce(dati, ''), ?) > 0 OR instr(coalesce(dati, ''), ?) > 0`,
		strconv.FormatInt(chatBot, 10), tok); n != 0 {
		t.Errorf("chat o token nell'audit: %d", n)
	}
	for _, chat := range []int64{chatBot, altraChatBot} {
		if _, err := b.AvviaTelegram(ctx, chat, tok); !errors.Is(err, uscita.ErrLinkTelegramNonValido) {
			t.Errorf("token già usato (%d): %v", chat, err)
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM iscrizioni_telegram`); n != 1 {
		t.Errorf("iscrizioni %d", n)
	}
}

// inventario: CLI-46
// I rifiuti di /start: token vuoto, più lungo di 64, ignoto, scaduto (alla scadenza esatta), di una busta
// chiusa, conto terzi o nel cestino. Nessuna iscrizione e nessun audit; il token di una busta che non va
// più bene si consuma lo stesso, come lo standalone.
func TestAvviaTelegramRifiuti(t *testing.T) {
	b, d, o, _ := provaTelegram(t)
	c := crea(t, b, modulo())
	oref := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = "2"; a.CodiceQR = "QR-OREF" }))
	chiusa := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-CHIUSA" }))
	cestino := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-CESTINO" }))
	esegui(t, d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, chiusa.ID)
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestino.ID)
	scade := o.Adesso().UTC().Add(time.Hour).Format(db.FormatoIstante)
	tokenScritto(t, d, oref.ID, "TOKEN-OREFICERIA", scade)
	tokenScritto(t, d, chiusa.ID, "TOKEN-CHIUSA", scade)
	tokenScritto(t, d, cestino.ID, "TOKEN-CESTINO", scade)
	// Il limite di 64 byte del parametro start vale anche per un token che il database ha (importato).
	tokenScritto(t, d, c.ID, strings.Repeat("a", 65), scade)
	tokenScritto(t, d, c.ID, strings.Repeat("b", 64), scade)
	scaduto := tokenDiPagina(t, b, c)
	quasi := tokenDiPagina(t, b, c)
	esegui(t, d, `UPDATE token_telegram SET scade_il = ? WHERE hash_token = ?`, o.Adesso().UTC().Format(db.FormatoIstante), hashDi(scaduto))
	esegui(t, d, `UPDATE token_telegram SET scade_il = ? WHERE hash_token = ?`, o.Adesso().UTC().Add(time.Microsecond).Format(db.FormatoIstante), hashDi(quasi))
	prima := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	for _, caso := range []struct{ nome, token string }{
		{"vuoto", ""}, {"65 caratteri", strings.Repeat("a", 65)}, {"ignoto", "NON-ESISTE"}, {"scaduto", scaduto},
		{"oreficeria", "TOKEN-OREFICERIA"}, {"chiusa", "TOKEN-CHIUSA"}, {"cestino", "TOKEN-CESTINO"},
	} {
		if _, err := b.AvviaTelegram(ctx, chatBot, caso.token); !errors.Is(err, uscita.ErrLinkTelegramNonValido) {
			t.Errorf("%s: %v", caso.nome, err)
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM iscrizioni_telegram`); n != 0 {
		t.Errorf("iscrizioni %d", n)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit`) != prima {
		t.Error("un rifiuto ha scritto l'audit")
	}
	// Restano il token scaduto (lo toglierà la pulizia della 075), quello ancora valido e i due lunghi.
	if n := conta(t, d, `SELECT count(*) FROM token_telegram WHERE busta_id <> ?`, c.ID); n != 0 {
		t.Errorf("token delle buste che non vanno più bene: %d", n)
	}
	if n := conta(t, d, `SELECT count(*) FROM token_telegram WHERE busta_id = ?`, c.ID); n != 4 {
		t.Errorf("token della busta: %d", n)
	}
	if _, err := b.AvviaTelegram(ctx, chatBot, quasi); err != nil {
		t.Errorf("un microsecondo prima della scadenza: %v", err)
	}
	if _, err := b.AvviaTelegram(ctx, altraChatBot, strings.Repeat("b", 64)); err != nil {
		t.Errorf("token di 64 caratteri: %v", err)
	}
}

func hashDi(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// inventario: CLI-46
// Un nuovo /start sulla stessa busta rimette il consenso a spento e riapre l'iscrizione fermata (la stessa
// riga: una chat ha una sola iscrizione per busta).
func TestAvviaTelegramDiNuovo(t *testing.T) {
	b, d, o, _ := provaTelegram(t)
	c := crea(t, b, modulo())
	vecchia := iscrivi(t, d, c.ID, chatBot, 1, istanteProva)
	esegui(t, d, `UPDATE iscrizioni_telegram SET consenso_il = ?, motivo_fermata = 'stop' WHERE id = ?`, istanteProva, vecchia)
	o.Sposta(time.Hour)
	id, err := b.AvviaTelegram(ctx, chatBot, tokenDiPagina(t, b, c))
	if err != nil || id != vecchia {
		t.Fatalf("iscrizione %d (vecchia %d): %v", id, vecchia, err)
	}
	ora := o.Adesso().UTC().Format(db.FormatoIstante)
	if got := rigaIscrizione(t, d, id); got != strconv.FormatInt(c.ID, 10)+"|"+strconv.FormatInt(chatBot, 10)+"|0|"+ora+"|-|-|-" {
		t.Errorf("iscrizione %s", got)
	}
	attiva := iscrivi(t, d, c.ID, altraChatBot, 1, nil)
	if _, err := b.AvviaTelegram(ctx, altraChatBot, tokenDiPagina(t, b, c)); err != nil {
		t.Fatal(err)
	}
	if got := rigaIscrizione(t, d, attiva); !strings.Contains(got, "|0|"+ora+"|-|-|-") {
		t.Errorf("iscrizione attiva dopo un nuovo /start: %s", got)
	}
}

// inventario: CLI-46
// Il pulsante del consenso vale solo per la chat dell'iscrizione e solo se non è fermata (un pulsante
// vecchio non riaccende); la stessa risposta due volte non scrive di nuovo; repair.telegram_consent con
// busta, iscrizione e consenso, mai la chat.
func TestConsensoTelegram(t *testing.T) {
	b, d, o, _ := provaTelegram(t)
	c := crea(t, b, modulo())
	id, err := b.AvviaTelegram(ctx, chatBot, tokenDiPagina(t, b, c))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.ConsensoTelegram(ctx, altraChatBot, id, true); !errors.Is(err, uscita.ErrIscrizioneTelegramNonTrovata) {
		t.Errorf("altra chat: %v", err)
	}
	if err := b.ConsensoTelegram(ctx, chatBot, id+100, true); !errors.Is(err, uscita.ErrIscrizioneTelegramNonTrovata) {
		t.Errorf("iscrizione che non c'è: %v", err)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.telegram_consent'`); n != 0 {
		t.Errorf("audit dei rifiuti: %d", n)
	}
	o.Sposta(time.Minute)
	if err := b.ConsensoTelegram(ctx, chatBot, id, true); err != nil {
		t.Fatal(err)
	}
	ora := o.Adesso().UTC().Format(db.FormatoIstante)
	if got := rigaIscrizione(t, d, id); !strings.HasSuffix(got, "|1|"+o.Adesso().UTC().Add(-time.Minute).Format(db.FormatoIstante)+"|"+ora+"|-|-") {
		t.Errorf("iscrizione %s", got)
	}
	atteso := `telegram|repair|` + strconv.FormatInt(c.ID, 10) + `|{"bag_number":"` + numeroDi(t, d, c.ID) + `","consent":true,"subscription_id":` + strconv.FormatInt(id, 10) + `}`
	if got := auditDelBot(t, d, "repair.telegram_consent"); got != atteso {
		t.Errorf("audit %s, atteso %s", got, atteso)
	}
	o.Sposta(time.Minute)
	if err := b.ConsensoTelegram(ctx, chatBot, id, true); err != nil {
		t.Fatal(err)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.telegram_consent'`); n != 1 {
		t.Errorf("la stessa risposta ha scritto: %d righe", n)
	}
	if err := b.ConsensoTelegram(ctx, chatBot, id, false); err != nil {
		t.Fatal(err)
	}
	if got := rigaIscrizione(t, d, id); !strings.Contains(got, "|0|") || !strings.Contains(got, "|"+o.Adesso().UTC().Format(db.FormatoIstante)+"|-|-") {
		t.Errorf("dopo «No grazie»: %s", got)
	}
	if got := auditDelBot(t, d, "repair.telegram_consent"); !strings.Contains(got, `"consent":false`) {
		t.Errorf("audit del no: %s", got)
	}
	esegui(t, d, `UPDATE iscrizioni_telegram SET fermata_il = ?, motivo_fermata = 'stop' WHERE id = ?`, istanteProva, id)
	if err := b.ConsensoTelegram(ctx, chatBot, id, true); !errors.Is(err, uscita.ErrIscrizioneTelegramNonTrovata) {
		t.Errorf("pulsante vecchio: %v", err)
	}
	if got := rigaIscrizione(t, d, id); !strings.Contains(got, "|0|") {
		t.Errorf("un pulsante vecchio ha riacceso: %s", got)
	}
	// Una iscrizione nuova, senza risposta: «No grazie» scrive (consenso_il), poi «No» di nuovo no.
	altra := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-ALTRA" }))
	nuova, err := b.AvviaTelegram(ctx, chatBot, tokenDiPagina(t, b, altra))
	if err != nil {
		t.Fatal(err)
	}
	prima := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.telegram_consent'`)
	for range 2 {
		if err := b.ConsensoTelegram(ctx, chatBot, nuova, false); err != nil {
			t.Fatal(err)
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.telegram_consent'`); n != prima+1 {
		t.Errorf("«No grazie» sul nuovo: %d righe nuove", n-prima)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE instr(coalesce(dati, ''), ?) > 0`, strconv.FormatInt(chatBot, 10)); n != 0 {
		t.Errorf("chat nell'audit: %d", n)
	}
}

// inventario: CLI-46
// /stop spegne ogni iscrizione ancora attiva della chat (consenso 0, fermata, motivo «stop»,
// repair.telegram_stop con via «bot» e attore «telegram»); quelle già fermate e quelle d'altre chat no.
// Una chat senza iscrizioni è sconosciuta; una con le sole fermate è conosciuta e spegne 0.
func TestFermaChatTelegram(t *testing.T) {
	b, d, o, _ := provaTelegram(t)
	uno := crea(t, b, modulo())
	due := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-DUE" }))
	tre := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-TRE" }))
	a1 := iscrivi(t, d, uno.ID, chatBot, 1, nil)
	a2 := iscrivi(t, d, due.ID, chatBot, 0, nil)
	f3 := iscrivi(t, d, tre.ID, chatBot, 0, istanteProva)
	esegui(t, d, `UPDATE iscrizioni_telegram SET motivo_fermata = 'page' WHERE id = ?`, f3)
	altra := iscrivi(t, d, uno.ID, altraChatBot, 1, nil)
	o.Sposta(time.Hour)
	n, conosciuta, err := b.FermaChatTelegram(ctx, chatBot)
	if err != nil || n != 2 || !conosciuta {
		t.Fatalf("ferma: %d %v %v", n, conosciuta, err)
	}
	ora := o.Adesso().UTC().Format(db.FormatoIstante)
	for _, id := range []int64{a1, a2} {
		if got := rigaIscrizione(t, d, id); !strings.HasSuffix(got, "|"+ora+"|stop") || !strings.Contains(got, "|0|") {
			t.Errorf("iscrizione %d: %s", id, got)
		}
	}
	if got := rigaIscrizione(t, d, f3); !strings.HasSuffix(got, "|"+istanteProva+"|page") {
		t.Errorf("già fermata: %s", got)
	}
	if got := rigaIscrizione(t, d, altra); !strings.HasSuffix(got, "|-|-") || !strings.Contains(got, "|1|") {
		t.Errorf("altra chat: %s", got)
	}
	righe := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.telegram_stop' AND attore = 'telegram'
		AND json_extract(dati, '$.via') = 'bot'`)
	if righe != 2 {
		t.Errorf("righe d'audit %d", righe)
	}
	atteso := `telegram|repair|` + strconv.FormatInt(due.ID, 10) + `|{"bag_number":"` + numeroDi(t, d, due.ID) + `","subscription_id":` + strconv.FormatInt(a2, 10) + `,"via":"bot"}`
	if got := auditDelBot(t, d, "repair.telegram_stop"); got != atteso {
		t.Errorf("audit %s, atteso %s", got, atteso)
	}
	if n, conosciuta, err := b.FermaChatTelegram(ctx, chatBot); err != nil || n != 0 || !conosciuta {
		t.Errorf("di nuovo: %d %v %v", n, conosciuta, err)
	}
	if n, conosciuta, err := b.FermaChatTelegram(ctx, chatBot+4); err != nil || n != 0 || conosciuta {
		t.Errorf("chat sconosciuta: %d %v %v", n, conosciuta, err)
	}
	if c := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.telegram_stop'`); c != 2 {
		t.Errorf("righe d'audit dopo: %d", c)
	}
}

// inventario: CLI-46
// Il nome del negozio del bot (OnlineSales.shop_name/0): quello del profilo, «Negozio» senza profilo o
// con il nome vuoto.
func TestNomeNegozioDelBot(t *testing.T) {
	b, d, _ := prova(t)
	if n, err := b.NomeNegozio(ctx); err != nil || n != "Negozio" {
		t.Errorf("senza profilo: %q %v", n, err)
	}
	esegui(t, d, `INSERT INTO profilo_negozio (nome, condizioni_garanzia, creato_il, aggiornato_il) VALUES ('', 'garanzia', ?, ?)`, istanteProva, istanteProva)
	if n, err := b.NomeNegozio(ctx); err != nil || n != "Negozio" {
		t.Errorf("nome vuoto: %q %v", n, err)
	}
	esegui(t, d, `UPDATE profilo_negozio SET nome = 'Bottega di Prova'`)
	if n, err := b.NomeNegozio(ctx); err != nil || n != "Bottega di Prova" {
		t.Errorf("con il nome: %q %v", n, err)
	}
}

// botDiProva è un Bot API finto in processo (httptest) per il giro completo: getUpdates dà gli
// aggiornamenti messi dal test, ogni altra richiesta è accettata e annotata.
type botDiProva struct {
	srv  *httptest.Server
	mu   sync.Mutex
	agg  []any
	sent []map[string]any
}

func nuovoBotDiProva(t *testing.T) *botDiProva {
	t.Helper()
	b := &botDiProva{}
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corpo, _ := io.ReadAll(r.Body)
		b.mu.Lock()
		defer b.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			j, _ := json.Marshal(map[string]any{"ok": true, "result": b.agg})
			_, _ = w.Write(j)
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			var m map[string]any
			_ = json.Unmarshal(corpo, &m)
			b.sent = append(b.sent, m)
			_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":9}}`)
		default:
			_, _ = io.WriteString(w, `{"ok":true,"result":true}`)
		}
	}))
	t.Cleanup(b.srv.Close)
	return b
}

func (b *botDiProva) arrivano(agg ...any) []map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.agg, b.sent = agg, nil
	return nil
}

func (b *botDiProva) mandati() []map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]map[string]any(nil), b.sent...)
}

func messaggioBot(id, chat int64, testo string) map[string]any {
	return map[string]any{"update_id": id, "message": map[string]any{"message_id": id,
		"from": map[string]any{"id": chat, "is_bot": false}, "chat": map[string]any{"id": chat, "type": "private"}, "text": testo}}
}

// inventario: CLI-46, CLI-44
// Il giro completo con il lettore vero e un Bot API finto: il pulsante della pagina dà il link t.me, /start
// con quel token iscrive la chat e il benvenuto porta i pulsanti con l'iscrizione; «Sì» accende (il
// riquadro della pagina passa a «attive»), /stop spegne (il riquadro torna alla proposta).
func TestBotTelegramDallaPagina(t *testing.T) {
	b, d, o, _ := provaTelegram(t)
	bot := nuovoBotDiProva(t)
	esegui(t, d, `INSERT INTO segreti_provider (nome, valore, aggiornato_il) VALUES ('outgoing.telegram.bot_token', '42:AAH-finto', ?)`, istanteProva)
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.telegram.chat_id', '-1001', ?), (?, '1', ?)`,
		istanteProva, uscita.ChiaveTelegramAcceso, istanteProva)
	l := uscita.NuovoLettoreTelegram(d, o, uscita.NuovoTelegram(d, bot.srv.URL), b, baseEmail)
	c := crea(t, b, modulo())
	tok := tokenDiPagina(t, b, c)
	leggi := func(agg ...any) []map[string]any {
		t.Helper()
		bot.arrivano(agg...)
		if _, err := l.Leggi(context.Background()); err != nil {
			t.Fatal(err)
		}
		return bot.mandati()
	}
	m := leggi(messaggioBot(1, chatBot, "/start "+tok))
	var id int64
	if err := d.Lettura.QueryRow(`SELECT id FROM iscrizioni_telegram WHERE busta_id = ? AND chat_id = ?`, c.ID, chatBot).Scan(&id); err != nil {
		t.Fatal(err)
	}
	tastiera, _ := json.Marshal(m[0]["reply_markup"])
	if len(m) != 1 || !strings.Contains(m[0]["text"].(string), "presso <b>Negozio</b>") ||
		string(tastiera) != `{"inline_keyboard":[[{"callback_data":"cli:tx:1:`+strconv.FormatInt(id, 10)+`","text":"✅ Sì, notifiche sulla riparazione"}],[{"callback_data":"cli:tx:0:`+strconv.FormatInt(id, 10)+`","text":"🔕 No grazie"}]]}` {
		t.Fatalf("benvenuto %v", m)
	}
	premuto := map[string]any{"update_id": 2, "callback_query": map[string]any{"id": "cb-2", "from": map[string]any{"id": chatBot, "is_bot": false},
		"message": map[string]any{"message_id": 9, "chat": map[string]any{"id": chatBot, "type": "private"}}, "data": "cli:tx:1:" + strconv.FormatInt(id, 10)}}
	if m := leggi(premuto); len(m) != 1 || m[0]["text"] != "✅ Notifiche attive. Ti avviserò quando ci sono aggiornamenti." {
		t.Errorf("consenso %v", m)
	}
	if r, err := b.RiquadroTelegram(ctx, c.ID, false); err != nil || r == nil || !r.Attive {
		t.Errorf("riquadro dopo il sì: %+v %v", r, err)
	}
	if m := leggi(messaggioBot(3, chatBot, "/stop")); len(m) != 1 || m[0]["text"] != "🔕 Notifiche disattivate per 1 iscrizione/i. Grazie!" {
		t.Errorf("stop %v", m)
	}
	if r, err := b.RiquadroTelegram(ctx, c.ID, false); err != nil || r == nil || r.Attive {
		t.Errorf("riquadro dopo lo stop: %+v %v", r, err)
	}
	if m := leggi(messaggioBot(4, altraChatBot, "/start "+tok)); len(m) != 1 || m[0]["text"] != "❌ Link di collegamento non valido o scaduto. Rigenera un nuovo link." {
		t.Errorf("token già usato %v", m)
	}
}
