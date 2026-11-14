package uscita

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Il lettore getUpdates e i comandi del bot (slice 073, CLI-46), come telegram_inbound_test.exs,
// telegram_inbound_customer_test.exs e telegram_poller_test.exs dello standalone a go-baseline: ogni
// richiesta va al Bot API finto in processo (telegram_finto_test.go), mai a Telegram; le iscrizioni sono
// finte (iscrizioniFinte), quelle vere le prova internal/buste (telegram_bot_test.go). Chat inventate
// sopra 2^52: Telegram non le usa per le persone.

const (
	chatA int64 = 4_503_599_627_370_497
	chatB int64 = 4_503_599_627_370_499
	// pubblicoProva è l'indirizzo pubblico del negozio nei test (dominio .invalid).
	pubblicoProva = "https://negozio.esempio.invalid"
	// Le risposte dello standalone.
	testoFisso            = "Questo è il bot di Negozio. Per le notifiche apri il link della tua riparazione."
	testoLinkMorto        = "❌ Link di collegamento non valido o scaduto. Rigenera un nuovo link."
	testoConsensoSi       = "✅ Notifiche attive. Ti avviserò quando ci sono aggiornamenti."
	testoConsensoNo       = "🔕 Notifiche disattivate per questa riparazione."
	testoNonTrovataAtteso = "❌ Iscrizione non trovata. Rigenera il link dalla pagina della riparazione."
	testoBenvenutoN       = "👋 Sei stato collegato alla tua riparazione presso <b>Negozio</b>.\n\nVuoi ricevere notifiche quando il negozio aggiorna lo stato o ti scrive un messaggio? Scegli le tue preferenze qui sotto:\n\n<i>Puoi sempre digitare /stop per disattivare tutto, /privacy per leggere come trattiamo i tuoi dati.</i>"
	descrizioneConfl      = "Conflict: terminated by other getUpdates request; make sure that only one bot instance is running"
)

// iscrizioniFinte sono le iscrizioni del lettore nei test: annotano le chiamate e rispondono quello che
// il test dice.
type iscrizioniFinte struct {
	mu          sync.Mutex
	negozio     string
	avvii       []string
	idAvvio     int64
	errAvvio    error
	consensi    []string
	errConsenso error
	// fermate: chat → iscrizioni spente; una chat che manca non ha iscrizioni.
	fermate  map[int64]int
	fermati  []int64
	errFerma error
	panico   bool
	// errNegozio fa fallire NomeNegozio (review della 073).
	errNegozio error
	// Le risposte a una notifica (CLI-49, slice 075): notifiche è (chat, messaggio) → busta; cercate le
	// ricerche «chat messaggio»; scritte i testi passati a RispostaTelegram «busta|testo»; errRisposta il
	// rifiuto da dare (senza chiamare dopo); d il database su cui dopo scrive nella sua transazione.
	notifiche   map[[2]int64]int64
	cercate     []string
	scritte     []string
	errRisposta error
	errCerca    error
	d           *db.DB
}

func (f *iscrizioniFinte) BustaDellaRisposta(_ context.Context, chat, messaggio int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cercate = append(f.cercate, fmt.Sprintf("%d %d", chat, messaggio))
	return f.notifiche[[2]int64{chat, messaggio}], f.errCerca
}

func (f *iscrizioniFinte) RispostaTelegram(ctx context.Context, busta int64, testo string, dopo func(context.Context, *sql.Tx) error) error {
	f.mu.Lock()
	f.scritte = append(f.scritte, fmt.Sprintf("%d|%s", busta, testo))
	err := f.errRisposta
	f.mu.Unlock()
	if err != nil {
		return err
	}
	tx, err := f.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	if err := dopo(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (f *iscrizioniFinte) AvviaTelegram(_ context.Context, chat int64, token string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.panico {
		panic("iscrizioni finte: panico " + token)
	}
	f.avvii = append(f.avvii, fmt.Sprintf("%d %s", chat, token))
	return f.idAvvio, f.errAvvio
}

func (f *iscrizioniFinte) ConsensoTelegram(_ context.Context, chat, id int64, valore bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.consensi = append(f.consensi, fmt.Sprintf("%d %d %v", chat, id, valore))
	return f.errConsenso
}

func (f *iscrizioniFinte) FermaChatTelegram(_ context.Context, chat int64) (int, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fermati = append(f.fermati, chat)
	if f.errFerma != nil {
		return 0, false, f.errFerma
	}
	n, ok := f.fermate[chat]
	return n, ok, nil
}

func (f *iscrizioniFinte) NomeNegozio(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.negozio, f.errNegozio
}

// provaLettore è il lettore con il bot configurato, l'invio Telegram acceso (D-03) e le notifiche ai
// clienti accese, sul Bot API finto; il registro finisce in log.
type provaLettore struct {
	l   *LettoreTelegram
	d   *db.DB
	o   *clock.Finto
	b   *botFinto
	isc *iscrizioniFinte
	log *bytes.Buffer
	// aggiornamenti è il «result» di ogni getUpdates.
	mu            sync.Mutex
	aggiornamenti []any
}

func nuovaProvaLettore(t *testing.T) *provaLettore {
	t.Helper()
	d, o := apriDB(t)
	b := nuovoBotFinto(t)
	impostaTelegram(t, d, tokenFinto, chatFinta, true)
	impostazione(t, d, ChiaveNotificheClienti, "1")
	isc := &iscrizioniFinte{negozio: "Negozio", fermate: map[int64]int{}, idAvvio: 41, notifiche: map[[2]int64]int64{}, d: d}
	p := &provaLettore{d: d, o: o, b: b, isc: isc, log: &bytes.Buffer{}}
	p.l = NuovoLettoreTelegram(d, o, NuovoTelegram(d, b.srv.URL), isc, pubblicoProva).ConRegistro(slog.New(slog.NewTextHandler(p.log, nil)))
	b.risposta(func(r richiestaBot) (int, string) {
		switch {
		case strings.HasSuffix(r.Percorso, "/getUpdates"):
			p.mu.Lock()
			defer p.mu.Unlock()
			j, _ := json.Marshal(map[string]any{"ok": true, "result": p.aggiornamenti})
			return 200, string(j)
		case strings.HasSuffix(r.Percorso, "/answerCallbackQuery"):
			return 200, `{"ok":true,"result":true}`
		}
		return 200, `{"ok":true,"result":{"message_id":77}}`
	})
	return p
}

// arrivano imposta gli aggiornamenti dei prossimi getUpdates e dimentica le richieste di prima.
func (p *provaLettore) arrivano(agg ...any) {
	p.mu.Lock()
	p.aggiornamenti = append([]any{}, agg...)
	p.mu.Unlock()
	p.b.mu.Lock()
	p.b.richieste = nil
	p.b.mu.Unlock()
}

func (p *provaLettore) leggi(t *testing.T) EsitoLettura {
	t.Helper()
	e, err := p.l.Leggi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// chiamata è una richiesta al Bot API: il metodo (l'ultimo pezzo del percorso) e il corpo letto con i
// numeri esatti.
type chiamata struct {
	Metodo string
	Corpo  map[string]any
	Grezzo string
}

// chiamate sono le richieste al Bot API dopo l'ultimo arrivano, senza getUpdates.
func (p *provaLettore) chiamate(t *testing.T) []chiamata {
	t.Helper()
	var out []chiamata
	for _, r := range p.b.tutte() {
		m := r.Percorso[strings.LastIndex(r.Percorso, "/")+1:]
		if m == "getUpdates" {
			continue
		}
		c := chiamata{Metodo: m, Grezzo: r.Grezzo}
		dec := json.NewDecoder(strings.NewReader(r.Grezzo))
		dec.UseNumber()
		if err := dec.Decode(&c.Corpo); err != nil {
			t.Fatalf("corpo di %s non JSON: %q", m, r.Grezzo)
		}
		out = append(out, c)
	}
	return out
}

// risposte sono i corpi dei sendMessage.
func (p *provaLettore) risposte(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, c := range p.chiamate(t) {
		if c.Metodo == "sendMessage" {
			out = append(out, c.Corpo)
		}
	}
	return out
}

func messaggio(id, chat int64, testo string) map[string]any {
	return map[string]any{"update_id": id, "message": map[string]any{
		"message_id": id,
		"from":       map[string]any{"id": chat, "is_bot": false, "first_name": "Esempio"},
		"chat":       map[string]any{"id": chat, "type": "private", "first_name": "Esempio"},
		"text":       testo,
	}}
}

func pulsante(id, chat int64, dati string) map[string]any {
	return map[string]any{"update_id": id, "callback_query": map[string]any{
		"id":      "cb-" + strconv.FormatInt(id, 10),
		"from":    map[string]any{"id": chat, "is_bot": false},
		"message": map[string]any{"message_id": 1, "chat": map[string]any{"id": chat, "type": "private"}},
		"data":    dati,
	}}
}

// sotto è il valore annidato di m lungo le chiavi.
func sotto(m map[string]any, chiavi ...string) map[string]any {
	for _, k := range chiavi {
		m = m[k].(map[string]any)
	}
	return m
}

// imp è il valore di una chiave di impostazioni, «<assente>» se la riga non c'è.
func imp(t *testing.T, d *db.DB, chiave string) string {
	t.Helper()
	var v string
	err := d.Lettura.QueryRow(`SELECT valore FROM impostazioni WHERE chiave = ?`, chiave).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "<assente>"
	}
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func chatDi(t *testing.T, r map[string]any) int64 {
	t.Helper()
	n, ok := r["chat_id"].(json.Number)
	if !ok {
		t.Fatalf("chat_id non numerico: %#v", r["chat_id"])
	}
	v, err := n.Int64()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// inventario: CLI-46
// Vincolo di Stefano (Dubbio 1 della 071): il lettore legge solo con «Invio Telegram» (D-03) e «Notifiche
// Telegram ai clienti» accese tutte e due e il bot configurato; altrimenti è inattivo e non chiama la
// rete. Se uno dei due si spegne fra due letture, la lettura dopo è di nuovo inattiva.
func TestLettoreSoloConIDueInterruttori(t *testing.T) {
	p := nuovaProvaLettore(t)
	casi := []struct {
		nome    string
		prepara func()
	}{
		{"notifiche spente", func() { impostazione(t, p.d, ChiaveNotificheClienti, "") }},
		{"notifiche «true»", func() { impostazione(t, p.d, ChiaveNotificheClienti, "true") }},
		{"invio spento", func() {
			impostazione(t, p.d, ChiaveNotificheClienti, "1")
			impostazione(t, p.d, ChiaveTelegramAcceso, "0")
		}},
		{"invio senza riga", func() {
			if _, err := p.d.Scrittura.Exec(`DELETE FROM impostazioni WHERE chiave = ?`, ChiaveTelegramAcceso); err != nil {
				t.Fatal(err)
			}
		}},
		{"bot senza token", func() {
			impostazione(t, p.d, ChiaveTelegramAcceso, "1")
			if _, err := p.d.Scrittura.Exec(`DELETE FROM segreti_provider`); err != nil {
				t.Fatal(err)
			}
		}},
		{"token di forma sbagliata", func() { impostaTelegram(t, p.d, "123/getMe?x", chatFinta, true) }},
	}
	p.arrivano(messaggio(1, chatA, "ciao"))
	for _, c := range casi {
		c.prepara()
		if e := p.leggi(t); e != (EsitoLettura{Inattivo: true}) {
			t.Errorf("%s: esito %+v", c.nome, e)
		}
		if n := p.b.quante(); n != 0 {
			t.Errorf("%s: %d richieste al Bot API", c.nome, n)
		}
	}
	impostaTelegram(t, p.d, tokenFinto, chatFinta, true)
	if e := p.leggi(t); e != (EsitoLettura{Gestiti: 1}) {
		t.Errorf("acceso: esito %+v", e)
	}
	if n := p.b.quante(); n != 2 {
		t.Errorf("acceso: richieste %d, attese 2 (getUpdates e la risposta)", n)
	}
	impostazione(t, p.d, ChiaveNotificheClienti, "")
	p.arrivano(messaggio(2, chatA, "ciao"))
	if e := p.leggi(t); e != (EsitoLettura{Inattivo: true}) || p.b.quante() != 0 {
		t.Errorf("rispento: esito %+v, richieste %d", e, p.b.quante())
	}
}

// inventario: CLI-46
// Una chat privata sconosciuta riceve la risposta fissa (testo semplice, chat_id numerico), l'offset va
// oltre l'ultimo aggiornamento e il contatore delle chat sconosciute sale (lo mostra la pagina dei
// provider della 071).
func TestLettoreRispostaFissa(t *testing.T) {
	p := nuovaProvaLettore(t)
	p.arrivano(messaggio(10, chatA, "buongiorno"), messaggio(11, chatB, "ciao"))
	if e := p.leggi(t); e != (EsitoLettura{Gestiti: 2}) {
		t.Fatalf("esito %+v", e)
	}
	c := p.chiamate(t)
	if len(c) != 2 {
		t.Fatalf("chiamate %+v", c)
	}
	if c[0].Grezzo != `{"chat_id":4503599627370497,"text":"`+testoFisso+`"}` {
		t.Errorf("prima risposta %s", c[0].Grezzo)
	}
	if chatDi(t, c[1].Corpo) != chatB || c[1].Corpo["text"] != testoFisso {
		t.Errorf("seconda risposta %s", c[1].Grezzo)
	}
	for _, r := range p.b.tutte() {
		if r.Metodo != "POST" || !strings.HasPrefix(r.Percorso, "/bot"+tokenFinto+"/") {
			t.Errorf("richiesta %s %s", r.Metodo, r.Percorso)
		}
	}
	if o := imp(t, p.d, chiaveOffset); o != "12" {
		t.Errorf("offset %s", o)
	}
	if n := imp(t, p.d, ChiaveChatSconosciute); n != "2" {
		t.Errorf("chat sconosciute %s", n)
	}
	pg, err := Nuova(p.d, p.o, nil).PaginaTelegram(context.Background())
	if err != nil || pg.ChatSconosciute != 2 {
		t.Errorf("pagina: %d %v", pg.ChatSconosciute, err)
	}
}

// inventario: CLI-46
// getUpdates chiede dall'offset salvato (senza offset la prima volta), per 25 secondi, solo messaggi e
// pulsanti.
func TestLettoreChiedeDallOffset(t *testing.T) {
	p := nuovaProvaLettore(t)
	p.arrivano()
	if e := p.leggi(t); e != (EsitoLettura{}) {
		t.Errorf("esito %+v", e)
	}
	r := p.b.tutte()
	if len(r) != 1 || r[0].Metodo != "POST" || r[0].Percorso != "/bot"+tokenFinto+"/getUpdates" {
		t.Fatalf("richieste %+v", r)
	}
	if r[0].Grezzo != `{"allowed_updates":["message","callback_query"],"timeout":25}` {
		t.Errorf("senza offset: %s", r[0].Grezzo)
	}
	offsetSalvato(t, p.d, "40", fra(0))
	p.arrivano()
	p.leggi(t)
	if r := p.b.tutte(); len(r) != 1 || r[0].Grezzo != `{"allowed_updates":["message","callback_query"],"offset":40,"timeout":25}` {
		t.Errorf("con offset: %+v", r)
	}
}

// inventario: CLI-46
// L'offset si salva dopo ogni aggiornamento, prima di gestire il successivo.
func TestLettoreOffsetDopoOgniAggiornamento(t *testing.T) {
	p := nuovaProvaLettore(t)
	var mu sync.Mutex
	visti := map[int64]string{}
	p.b.risposta(func(r richiestaBot) (int, string) {
		if strings.HasSuffix(r.Percorso, "/getUpdates") {
			j, _ := json.Marshal(map[string]any{"ok": true, "result": []any{messaggio(20, chatA, "a"), messaggio(21, chatB, "b")}})
			return 200, string(j)
		}
		var c struct {
			Chat int64 `json:"chat_id"`
		}
		_ = json.Unmarshal([]byte(r.Grezzo), &c)
		mu.Lock()
		visti[c.Chat] = imp(t, p.d, chiaveOffset)
		mu.Unlock()
		return 200, `{"ok":true,"result":{"message_id":1}}`
	})
	if e := p.leggi(t); e.Gestiti != 2 {
		t.Fatalf("esito %+v", e)
	}
	if visti[chatA] != "<assente>" || visti[chatB] != "21" {
		t.Errorf("offset alle risposte: %v", visti)
	}
	if o := imp(t, p.d, chiaveOffset); o != "22" {
		t.Errorf("offset %s", o)
	}
}

// inventario: CLI-46
// Un aggiornamento già dietro l'offset non si gestisce due volte.
func TestLettoreAggiornamentoGiaLetto(t *testing.T) {
	p := nuovaProvaLettore(t)
	offsetSalvato(t, p.d, "50", fra(0))
	p.arrivano(messaggio(48, chatA, "vecchio"), messaggio(50, chatB, "nuovo"))
	if e := p.leggi(t); e != (EsitoLettura{Gestiti: 1}) {
		t.Errorf("esito %+v", e)
	}
	r := p.risposte(t)
	if len(r) != 1 || chatDi(t, r[0]) != chatB {
		t.Errorf("risposte %v", r)
	}
	if imp(t, p.d, chiaveOffset) != "51" || imp(t, p.d, ChiaveChatSconosciute) != "1" {
		t.Errorf("offset %s, sconosciute %s", imp(t, p.d, chiaveOffset), imp(t, p.d, ChiaveChatSconosciute))
	}
}

// inventario: CLI-46
// Fuori dal flusso del cliente, senza risposta e senza chiamare le iscrizioni: gruppi, supergruppi,
// canali, il pulsante premuto in un gruppo, un altro bot (messaggio o pulsante), la chat del negozio (per
// numero e per @nome). L'offset va avanti lo stesso.
func TestLettoreFuoriDalFlusso(t *testing.T) {
	p := nuovaProvaLettore(t)
	gruppo := messaggio(30, -4_503_599_627_370_497, "ciao a tutti")
	sotto(gruppo, "message", "chat")["type"] = "group"
	super := messaggio(31, -1_004_503_599_627, "/stop")
	sotto(super, "message", "chat")["type"] = "supergroup"
	canale := messaggio(32, -1_004_503_599_628, "/start x")
	sotto(canale, "message", "chat")["type"] = "channel"
	post := map[string]any{"update_id": 33, "channel_post": map[string]any{"chat": map[string]any{"id": chatA, "type": "channel"}, "text": "x"}}
	premutoInGruppo := pulsante(34, chatA, "cli:tx:1:41")
	sotto(premutoInGruppo, "callback_query", "message")["chat"] = map[string]any{"id": -4_503_599_627_370_497, "type": "group"}
	altroBot := messaggio(35, chatA, "/stop")
	sotto(altroBot, "message", "from")["is_bot"] = true
	pulsanteBot := pulsante(36, chatA, "cli:tx:1:41")
	sotto(pulsanteBot, "callback_query", "from")["is_bot"] = true
	negozio := messaggio(37, -1009999999999, "/stop")
	p.arrivano(gruppo, super, canale, post, premutoInGruppo, altroBot, pulsanteBot, negozio)
	if e := p.leggi(t); e != (EsitoLettura{Gestiti: 8}) {
		t.Errorf("esito %+v", e)
	}
	if c := p.chiamate(t); len(c) != 0 {
		t.Errorf("chiamate %+v", c)
	}
	impostazione(t, p.d, "outgoing.telegram.chat_id", "@negozio_prova")
	perNome := messaggio(38, chatB, "ciao")
	sotto(perNome, "message", "chat")["username"] = "negozio_prova"
	premutoPerNome := pulsante(39, chatB, "cli:tx:1:41")
	sotto(premutoPerNome, "callback_query", "message", "chat")["username"] = "negozio_prova"
	p.arrivano(perNome, premutoPerNome)
	if e := p.leggi(t); e != (EsitoLettura{Gestiti: 2}) {
		t.Errorf("esito per @nome %+v", e)
	}
	if c := p.chiamate(t); len(c) != 0 {
		t.Errorf("chiamate per @nome %+v", c)
	}
	if imp(t, p.d, chiaveOffset) != "40" || imp(t, p.d, ChiaveChatSconosciute) != "<assente>" {
		t.Errorf("offset %s, sconosciute %s", imp(t, p.d, chiaveOffset), imp(t, p.d, ChiaveChatSconosciute))
	}
	if len(p.isc.avvii)+len(p.isc.consensi)+len(p.isc.fermati) != 0 {
		t.Errorf("iscrizioni chiamate: %v %v %v", p.isc.avvii, p.isc.consensi, p.isc.fermati)
	}
}

// inventario: CLI-46
// Una risposta che Telegram rifiuta non ferma l'offset; aggiornamenti senza la forma attesa: con un
// update_id intero si saltano contandoli, senza (non è una mappa, update_id non intero) non si contano e
// l'offset non si muove.
func TestLettoreAggiornamentiStrani(t *testing.T) {
	p := nuovaProvaLettore(t)
	p.arrivano(messaggio(60, chatA, "x"))
	p.b.risposta(func(r richiestaBot) (int, string) {
		if strings.HasSuffix(r.Percorso, "/getUpdates") {
			j, _ := json.Marshal(map[string]any{"ok": true, "result": []any{messaggio(60, chatA, "x")}})
			return 200, string(j)
		}
		return 403, `{"ok":false,"description":"Forbidden: bot was blocked by the user"}`
	})
	if e := p.leggi(t); e != (EsitoLettura{Gestiti: 1}) || imp(t, p.d, chiaveOffset) != "61" {
		t.Errorf("risposta rifiutata: %+v, offset %s", e, imp(t, p.d, chiaveOffset))
	}
	p2 := nuovaProvaLettore(t)
	p2.arrivano(map[string]any{"update_id": 70, "edited_message": map[string]any{}}, map[string]any{"update_id": 71, "message": "x"},
		map[string]any{"update_id": 72, "message": map[string]any{"chat": map[string]any{"id": "4503599627370497", "type": "private"}, "text": "x"}},
		map[string]any{"update_id": 73, "message": map[string]any{"chat": map[string]any{"id": 4.5, "type": "private"}, "text": "x"}},
		map[string]any{"update_id": 74, "callback_query": map[string]any{"id": 5, "message": map[string]any{"chat": map[string]any{"id": chatA, "type": "private"}}, "data": "cli:tx:1:41"}})
	if e := p2.leggi(t); e != (EsitoLettura{Gestiti: 5}) || imp(t, p2.d, chiaveOffset) != "75" || len(p2.chiamate(t)) != 0 {
		t.Errorf("forme sbagliate: %+v, offset %s, chiamate %d", e, imp(t, p2.d, chiaveOffset), len(p2.chiamate(t)))
	}
	p3 := nuovaProvaLettore(t)
	p3.arrivano("x", map[string]any{"update_id": "80"}, map[string]any{"update_id": 81.5}, map[string]any{"messaggio": 1})
	if e := p3.leggi(t); e != (EsitoLettura{}) || imp(t, p3.d, chiaveOffset) != "<assente>" || len(p3.chiamate(t)) != 0 {
		t.Errorf("senza update_id intero: %+v, offset %s", e, imp(t, p3.d, chiaveOffset))
	}
}

// inventario: CLI-46
// Un 409 (webhook attivo o un secondo lettore) finisce nell'ultimo errore del lettore, che la pagina dei
// provider della 071 mostra come conflitto; l'offset resta, l'unica richiesta è getUpdates (il bot non
// cancella mai il webhook) e il token non compare.
func TestLettore409(t *testing.T) {
	p := nuovaProvaLettore(t)
	offsetSalvato(t, p.d, "80", fra(0))
	p.b.risposta(rifiutoBot(409, descrizioneConfl+" "+tokenFinto))
	e := p.leggi(t)
	if !e.Conflitto || e.Errore != "409: "+descrizioneConfl+" ***" || e.Gestiti != 0 || e.Inattivo {
		t.Errorf("esito %+v", e)
	}
	r := p.b.tutte()
	if len(r) != 1 || !strings.HasSuffix(r[0].Percorso, "/getUpdates") {
		t.Errorf("richieste %+v", r)
	}
	if imp(t, p.d, chiaveOffset) != "80" {
		t.Errorf("offset %s", imp(t, p.d, chiaveOffset))
	}
	ultimo := imp(t, p.d, ChiaveErroreLettore)
	if ultimo != "409: "+descrizioneConfl+" ***" || strings.Contains(ultimo, tokenFinto) {
		t.Errorf("ultimo errore %q", ultimo)
	}
	if il := imp(t, p.d, ChiaveErroreLettoreIl); il != p.o.Adesso().UTC().Format(time.RFC3339Nano) {
		t.Errorf("ora dell'errore %q", il)
	}
	pg, err := Nuova(p.d, p.o, nil).PaginaTelegram(context.Background())
	if err != nil || !pg.Conflitto || pg.UltimoErroreIl == nil {
		t.Errorf("pagina: %+v %v", pg, err)
	}
	if strings.Contains(p.log.String(), tokenFinto) {
		t.Errorf("token nel registro: %s", p.log.String())
	}
	p.b.risposta(func(richiestaBot) (int, string) { return 409, "non json" })
	if e := p.leggi(t); e.Errore != "409: Conflict" || !e.Conflitto {
		t.Errorf("409 senza descrizione: %+v", e)
	}
}

// inventario: CLI-46
// Un altro errore si salva ma non è un conflitto; una lettura riuscita lo pulisce (e senza errori non
// scrive niente).
func TestLettoreAltriErrori(t *testing.T) {
	p := nuovaProvaLettore(t)
	p.arrivano()
	p.leggi(t)
	if imp(t, p.d, ChiaveErroreLettore) != "<assente>" || imp(t, p.d, ChiaveErroreLettoreIl) != "<assente>" {
		t.Error("una lettura riuscita senza errori di prima ha scritto l'errore")
	}
	for _, c := range []struct {
		stato         int
		corpo, atteso string
	}{
		{502, "bad gateway", "telegram:http:502"},
		{200, `{"ok":false,"description":"x"}`, "telegram:risposta non riconosciuta"},
		{200, `{"ok":true,"result":{}}`, "telegram:risposta non riconosciuta"},
		{200, `{"ok":true,"result":null}`, "telegram:risposta non riconosciuta"},
		{302, ``, "telegram:http:302"},
	} {
		p.b.risposta(func(richiestaBot) (int, string) { return c.stato, c.corpo })
		e := p.leggi(t)
		if e.Errore != c.atteso || e.Conflitto {
			t.Errorf("%d %s: esito %+v", c.stato, c.corpo, e)
		}
		if imp(t, p.d, ChiaveErroreLettore) != c.atteso {
			t.Errorf("%d: ultimo errore %q", c.stato, imp(t, p.d, ChiaveErroreLettore))
		}
	}
	p.arrivano()
	nuovaRisposta(p)
	if e := p.leggi(t); e != (EsitoLettura{}) {
		t.Errorf("lettura riuscita: %+v", e)
	}
	if imp(t, p.d, ChiaveErroreLettore) != "" || imp(t, p.d, ChiaveErroreLettoreIl) != "" {
		t.Errorf("errore non pulito: %q %q", imp(t, p.d, ChiaveErroreLettore), imp(t, p.d, ChiaveErroreLettoreIl))
	}
}

// nuovaRisposta rimette le risposte normali del Bot API finto della prova.
func nuovaRisposta(p *provaLettore) {
	p.b.risposta(func(r richiestaBot) (int, string) {
		switch {
		case strings.HasSuffix(r.Percorso, "/getUpdates"):
			p.mu.Lock()
			defer p.mu.Unlock()
			j, _ := json.Marshal(map[string]any{"ok": true, "result": p.aggiornamenti})
			return 200, string(j)
		case strings.HasSuffix(r.Percorso, "/answerCallbackQuery"):
			return 200, `{"ok":true,"result":true}`
		}
		return 200, `{"ok":true,"result":{"message_id":77}}`
	})
}

// inventario: CLI-46
// Un aggiornamento che fallisce sempre si riprova alla lettura dopo (l'offset resta su di lui, il
// conteggio in telegram_inbound.failing_update) e alla terza si salta; il registro dice l'aggiornamento,
// mai la chat.
func TestLettoreAggiornamentoCheFallisce(t *testing.T) {
	p := nuovaProvaLettore(t)
	p.isc.errFerma = errors.New("iscrizioni: tabella che non si legge")
	p.isc.fermate[chatA] = 1
	p.arrivano(messaggio(2, chatA, "/stop"), messaggio(3, chatB, "ciao"))
	for i := 1; i <= 2; i++ {
		e := p.leggi(t)
		if e.Errore != "telegram:update 2 non gestito" || e.Gestiti != 0 {
			t.Errorf("lettura %d: %+v", i, e)
		}
		if imp(t, p.d, chiaveOffset) != "<assente>" || imp(t, p.d, chiaveAggInErrore) != fmt.Sprintf("2:%d", i) {
			t.Errorf("lettura %d: offset %s, in errore %s", i, imp(t, p.d, chiaveOffset), imp(t, p.d, chiaveAggInErrore))
		}
		if imp(t, p.d, ChiaveErroreLettore) != "telegram:update 2 non gestito" {
			t.Errorf("lettura %d: ultimo errore %q", i, imp(t, p.d, ChiaveErroreLettore))
		}
		if len(p.risposte(t)) != 0 {
			t.Errorf("lettura %d: risposte %v", i, p.risposte(t))
		}
	}
	if e := p.leggi(t); e != (EsitoLettura{Gestiti: 2}) {
		t.Errorf("terza lettura: %+v", e)
	}
	if imp(t, p.d, chiaveOffset) != "4" || imp(t, p.d, chiaveAggInErrore) != "" {
		t.Errorf("dopo: offset %s, in errore %q", imp(t, p.d, chiaveOffset), imp(t, p.d, chiaveAggInErrore))
	}
	r := p.risposte(t)
	if len(r) != 1 || chatDi(t, r[0]) != chatB {
		t.Errorf("risposte %v", r)
	}
	log := p.log.String()
	if !strings.Contains(log, "aggiornamento 2 saltato dopo 3 tentativi") {
		t.Errorf("registro senza il salto: %s", log)
	}
	for _, vietato := range []string{strconv.FormatInt(chatA, 10), strconv.FormatInt(chatB, 10), "tabella che non si legge"} {
		if strings.Contains(log, vietato) {
			t.Errorf("registro con %q: %s", vietato, log)
		}
	}
	// Un panico conta come un errore: alla terza volta si salta, e il token del /start non va nel registro.
	q := nuovaProvaLettore(t)
	q.isc.panico = true
	q.arrivano(messaggio(9, chatA, "/start TOKENSEGRETODIPROVA"))
	for i := 1; i <= 3; i++ {
		e := q.leggi(t)
		if (i < 3) != (e.Errore != "") {
			t.Errorf("panico, lettura %d: %+v", i, e)
		}
	}
	if imp(t, q.d, chiaveOffset) != "10" || strings.Contains(q.log.String(), "TOKENSEGRETODIPROVA") {
		t.Errorf("panico: offset %s, registro %s", imp(t, q.d, chiaveOffset), q.log.String())
	}
}

// inventario: CLI-46
// Il conteggio è dell'aggiornamento: uno riuscito lo pulisce, uno nuovo che fallisce riparte da 1.
func TestLettoreConteggioDegliErrori(t *testing.T) {
	p := nuovaProvaLettore(t)
	impostazione(t, p.d, chiaveAggInErrore, "7:2")
	p.arrivano(messaggio(8, chatA, "ciao"))
	if e := p.leggi(t); e.Gestiti != 1 || imp(t, p.d, chiaveAggInErrore) != "" {
		t.Errorf("riuscito: %+v, in errore %q", e, imp(t, p.d, chiaveAggInErrore))
	}
	impostazione(t, p.d, chiaveAggInErrore, "7:2")
	p.isc.errFerma = errors.New("guasto")
	p.arrivano(messaggio(9, chatA, "/stop"))
	if e := p.leggi(t); e.Errore == "" || imp(t, p.d, chiaveAggInErrore) != "9:1" {
		t.Errorf("altro aggiornamento: %+v, in errore %q", e, imp(t, p.d, chiaveAggInErrore))
	}
}

// inventario: CLI-46
// /start <token>: le iscrizioni ricevono chat e token (anche «/START@bot», spazi e tab); la risposta è il
// benvenuto HTML dello standalone con il nome del negozio protetto e la tastiera dei due pulsanti con
// l'iscrizione; un link non valido riceve il rifiuto; /start senza token, «/starttok» e un messaggio senza
// testo ricevono la risposta fissa.
func TestLettoreStart(t *testing.T) {
	p := nuovaProvaLettore(t)
	p.arrivano(messaggio(1, chatA, "/start TOKENDIPROVA"))
	if e := p.leggi(t); e != (EsitoLettura{Gestiti: 1}) {
		t.Fatalf("esito %+v", e)
	}
	if len(p.isc.avvii) != 1 || p.isc.avvii[0] != fmt.Sprintf("%d TOKENDIPROVA", chatA) {
		t.Errorf("avvii %v", p.isc.avvii)
	}
	c := p.chiamate(t)
	atteso := `{"chat_id":4503599627370497,"text":` + strconv.Quote(testoBenvenutoN) + `,"parse_mode":"HTML","reply_markup":{"inline_keyboard":[[{"text":"✅ Sì, notifiche sulla riparazione","callback_data":"cli:tx:1:41"}],[{"text":"🔕 No grazie","callback_data":"cli:tx:0:41"}]]}}`
	var a, g any
	_ = json.Unmarshal([]byte(atteso), &a)
	_ = json.Unmarshal([]byte(c[0].Grezzo), &g)
	if len(c) != 1 || fmt.Sprint(a) != fmt.Sprint(g) || strings.Contains(c[0].Grezzo, `\u00`) {
		t.Errorf("benvenuto %s", c[0].Grezzo)
	}
	if imp(t, p.d, ChiaveChatSconosciute) != "<assente>" {
		t.Error("il benvenuto ha contato una chat sconosciuta")
	}
	p.isc.negozio = `Oro & <Argento> "d'oro"`
	p.arrivano(messaggio(2, chatB, "  /START@Negozio_prova_bot \t TOK2  "), messaggio(3, chatA, "/start\tTOK3"), messaggio(4, chatA, "/start a b"))
	p.leggi(t)
	if len(p.isc.avvii) != 4 || p.isc.avvii[1] != fmt.Sprintf("%d TOK2", chatB) || p.isc.avvii[2] != fmt.Sprintf("%d TOK3", chatA) ||
		p.isc.avvii[3] != fmt.Sprintf("%d a b", chatA) {
		t.Errorf("avvii %q", p.isc.avvii)
	}
	if r := p.risposte(t); len(r) == 0 || !strings.Contains(r[0]["text"].(string), "presso <b>Oro &amp; &lt;Argento&gt; &quot;d&#39;oro&quot;</b>.") {
		t.Errorf("nome protetto: %v", r)
	}
	p.isc.negozio = "Negozio"
	p.isc.errAvvio = ErrLinkTelegramNonValido
	p.arrivano(messaggio(5, chatB, "/start SCADUTO"))
	p.leggi(t)
	if c := p.chiamate(t); len(c) != 1 || c[0].Grezzo != `{"chat_id":4503599627370499,"text":"`+testoLinkMorto+`"}` {
		t.Errorf("rifiuto %+v", c)
	}
	p.isc.avvii = nil
	senzaTesto := messaggio(8, chatB, "")
	delete(sotto(senzaTesto, "message"), "text")
	sotto(senzaTesto, "message")["photo"] = []any{map[string]any{"file_id": "x"}}
	p.arrivano(messaggio(6, chatA, "/start"), messaggio(7, chatB, "/starttok"))
	p.leggi(t)
	p.o.Sposta(10 * time.Minute)
	p.arrivano(senzaTesto)
	p.leggi(t)
	if len(p.isc.avvii) != 0 {
		t.Errorf("avvii senza token: %v", p.isc.avvii)
	}
	if n := imp(t, p.d, ChiaveChatSconosciute); n != "3" {
		t.Errorf("risposte fisse %s", n)
	}
}

// inventario: CLI-46
// /stop e /disattiva (anche con il nome del bot) spengono le iscrizioni della chat con il conteggio dello
// standalone; una chat senza iscrizioni riceve la risposta fissa; /privacy manda l'indirizzo
// dell'informativa, in HTML con il nome del negozio.
func TestLettoreStopEPrivacy(t *testing.T) {
	p := nuovaProvaLettore(t)
	p.isc.fermate[chatA] = 2
	p.arrivano(messaggio(1, chatA, "/stop"), messaggio(2, chatA, "/Disattiva@negozio_prova_bot"), messaggio(3, chatB, "/stop"))
	if e := p.leggi(t); e.Gestiti != 3 {
		t.Fatalf("esito %+v", e)
	}
	r := p.risposte(t)
	if len(r) != 3 || r[0]["text"] != "🔕 Notifiche disattivate per 2 iscrizione/i. Grazie!" || r[1]["text"] != r[0]["text"] ||
		chatDi(t, r[2]) != chatB || r[2]["text"] != testoFisso || r[0]["parse_mode"] != nil {
		t.Errorf("risposte %v", r)
	}
	if fmt.Sprint(p.isc.fermati) != fmt.Sprint([]int64{chatA, chatA, chatB}) {
		t.Errorf("fermate %v", p.isc.fermati)
	}
	p.isc.fermate[chatB] = 0
	p.arrivano(messaggio(4, chatB, "/disattiva"), messaggio(5, chatA, "/privacy"))
	p.leggi(t)
	r = p.risposte(t)
	if len(r) != 2 || r[0]["text"] != "🔕 Notifiche disattivate per 0 iscrizione/i. Grazie!" {
		t.Errorf("chat con iscrizioni già spente: %v", r)
	}
	if len(r) == 2 && (r[1]["parse_mode"] != "HTML" || r[1]["text"] != "<b>Privacy policy</b>\n• <b>Negozio</b>: "+pubblicoProva+"/privacy") {
		t.Errorf("privacy %v", r[1])
	}
}

// inventario: CLI-46
// Solo Go: senza indirizzo pubblico /privacy non ha un indirizzo da dare e riceve la risposta fissa.
func TestLettorePrivacySenzaIndirizzo(t *testing.T) {
	p := nuovaProvaLettore(t)
	p.l.pubblico = ""
	p.arrivano(messaggio(1, chatA, "/privacy"))
	p.leggi(t)
	if r := p.risposte(t); len(r) != 1 || r[0]["text"] != testoFisso {
		t.Errorf("risposte %v", r)
	}
}

// inventario: CLI-46
// I pulsanti del consenso: si risponde al pulsante (answerCallbackQuery) e poi si conferma; il consenso va
// alle iscrizioni con la chat di chi ha premuto, che rifiutano una iscrizione d'altri o spenta; un
// pulsante sconosciuto riceve solo la risposta al pulsante.
func TestLettorePulsanti(t *testing.T) {
	p := nuovaProvaLettore(t)
	p.arrivano(pulsante(2, chatA, "cli:tx:1:41"), pulsante(3, chatA, "cli:tx:0:+41"))
	if e := p.leggi(t); e.Gestiti != 2 {
		t.Fatalf("esito %+v", e)
	}
	c := p.chiamate(t)
	if len(c) != 4 || c[0].Metodo != "answerCallbackQuery" || c[0].Grezzo != `{"callback_query_id":"cb-2"}` ||
		c[1].Metodo != "sendMessage" || c[1].Corpo["text"] != testoConsensoSi || chatDi(t, c[1].Corpo) != chatA ||
		c[3].Corpo["text"] != testoConsensoNo {
		t.Errorf("chiamate %+v", c)
	}
	if fmt.Sprint(p.isc.consensi) != fmt.Sprintf("[%d 41 true %d 41 false]", chatA, chatA) {
		t.Errorf("consensi %v", p.isc.consensi)
	}
	p.isc.consensi = nil
	p.isc.errConsenso = ErrIscrizioneTelegramNonTrovata
	p.arrivano(pulsante(4, chatB, "cli:tx:1:41"))
	p.leggi(t)
	if r := p.risposte(t); len(r) != 1 || r[0]["text"] != testoNonTrovataAtteso || chatDi(t, r[0]) != chatB {
		t.Errorf("non trovata %v", r)
	}
	p.isc.consensi = nil
	senzaDati := pulsante(10, chatA, "")
	delete(sotto(senzaDati, "callback_query"), "data")
	p.arrivano(pulsante(5, chatA, "cli:promo:1:41"), pulsante(6, chatA, "qualcosa"), pulsante(7, chatA, "cli:tx:2:41"),
		pulsante(8, chatA, "cli:tx:1:x"), pulsante(9, chatA, "cli:tx:1:41:9"), senzaDati)
	p.leggi(t)
	c = p.chiamate(t)
	if len(c) != 6 || len(p.isc.consensi) != 0 {
		t.Errorf("pulsanti sconosciuti: %+v, consensi %v", c, p.isc.consensi)
	}
	for _, x := range c {
		if x.Metodo != "answerCallbackQuery" {
			t.Errorf("chiamata %+v", x)
		}
	}
}

// inventario: CLI-46
// I limiti in memoria di reply_limit.ex: la risposta fissa una volta ogni 10 minuti per chat (alla
// scadenza esatta di nuovo) e 20 al minuto in tutto; le altre 8 al minuto per chat e 30 al minuto in
// tutto. Un pulsante oltre il limite non riceve nemmeno la risposta al pulsante. Nessuna chat nel database.
func TestLettoreLimiti(t *testing.T) {
	p := nuovaProvaLettore(t)
	p.arrivano(messaggio(1, chatA, "ciao"), messaggio(2, chatA, "ci sei?"), messaggio(3, chatA, "ehi"), messaggio(4, chatB, "salve"))
	if e := p.leggi(t); e.Gestiti != 4 {
		t.Fatalf("esito %+v", e)
	}
	if r := p.risposte(t); len(r) != 2 || chatDi(t, r[0]) != chatA || chatDi(t, r[1]) != chatB {
		t.Errorf("fisse per chat %v", r)
	}
	if imp(t, p.d, ChiaveChatSconosciute) != "2" {
		t.Errorf("sconosciute %s", imp(t, p.d, ChiaveChatSconosciute))
	}
	p.o.Sposta(10*time.Minute - time.Second)
	p.arrivano(messaggio(5, chatA, "ancora"))
	p.leggi(t)
	if r := p.risposte(t); len(r) != 0 {
		t.Errorf("prima dei 10 minuti %v", r)
	}
	p.o.Sposta(time.Second)
	p.arrivano(messaggio(6, chatA, "ancora"))
	p.leggi(t)
	if r := p.risposte(t); len(r) != 1 {
		t.Errorf("dopo 10 minuti %v", r)
	}
	for _, x := range []struct {
		nome, testo    string
		quante, attese int
	}{{"fisse in tutto", "spam", 25, 20}, {"altre in tutto", "/privacy", 40, 30}} {
		q := nuovaProvaLettore(t)
		var agg []any
		for n := 1; n <= x.quante; n++ {
			agg = append(agg, messaggio(int64(n), chatA+2*int64(n), x.testo))
		}
		q.arrivano(agg...)
		q.leggi(t)
		if r := q.risposte(t); len(r) != x.attese {
			t.Errorf("%s: %d risposte", x.nome, len(r))
		}
		q.o.Sposta(59 * time.Second)
		q.arrivano(messaggio(100, chatB+1000, x.testo))
		q.leggi(t)
		if r := q.risposte(t); len(r) != 0 {
			t.Errorf("%s dopo 59 s: %d", x.nome, len(r))
		}
		q.o.Sposta(time.Second)
		q.arrivano(messaggio(101, chatB+1002, x.testo))
		q.leggi(t)
		if r := q.risposte(t); len(r) != 1 {
			t.Errorf("%s dopo un minuto: %d", x.nome, len(r))
		}
	}
	q := nuovaProvaLettore(t)
	var agg []any
	for n := 1; n <= 10; n++ {
		agg = append(agg, messaggio(int64(n), chatA, "/privacy"))
	}
	q.arrivano(agg...)
	q.leggi(t)
	if r := q.risposte(t); len(r) != 8 {
		t.Errorf("altre per chat: %d", len(r))
	}
	q.o.Sposta(time.Minute)
	agg = []any{messaggio(20, chatA, "/privacy")}
	for n := int64(21); n <= 30; n++ {
		agg = append(agg, pulsante(n, chatA, "cli:tx:1:41"))
	}
	q.arrivano(agg...)
	q.leggi(t)
	risposti := 0
	for _, c := range q.chiamate(t) {
		if c.Metodo == "answerCallbackQuery" {
			risposti++
		}
	}
	if risposti != 7 {
		t.Errorf("pulsanti risposti %d, attesi 7", risposti)
	}
	if n := contaCon(t, q.d, strconv.FormatInt(chatA, 10)); n != 0 {
		t.Errorf("chat nel database: %d righe", n)
	}
}

// contaCon conta le righe di impostazioni e dell'audit che contengono s.
func contaCon(t *testing.T, d *db.DB, s string) int {
	t.Helper()
	var n int
	if err := d.Lettura.QueryRow(`SELECT (SELECT count(*) FROM impostazioni WHERE instr(valore, ?1) > 0 OR instr(chiave, ?1) > 0)
		+ (SELECT count(*) FROM eventi_audit WHERE instr(coalesce(dati, ''), ?1) > 0)`, s).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// inventario: CLI-46
// Niente di quello che la chat scrive resta: né il testo né la chat né il token nel registro, nelle
// impostazioni o nell'audit, anche quando le iscrizioni non rispondono.
func TestLettoreNienteDatiNeiRegistri(t *testing.T) {
	p := nuovaProvaLettore(t)
	p.isc.errAvvio = errors.New("database chiuso")
	p.arrivano(messaggio(10, chatA, "il mio numero è 333 000 1111"), messaggio(11, chatB, "/start TOKENSEGRETO123"))
	for range 3 {
		if _, err := p.l.Leggi(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	log := p.log.String()
	for _, vietato := range []string{"333 000 1111", strconv.FormatInt(chatA, 10), strconv.FormatInt(chatB, 10), "TOKENSEGRETO123", tokenFinto, "database chiuso"} {
		if strings.Contains(log, vietato) {
			t.Errorf("registro con %q: %s", vietato, log)
		}
		if n := contaCon(t, p.d, vietato); n != 0 && vietato != tokenFinto {
			t.Errorf("%q nel database: %d righe", vietato, n)
		}
	}
	if !strings.Contains(log, "aggiornamento 11 saltato") {
		t.Errorf("registro senza il salto: %s", log)
	}
}

// inventario: CLI-46, CLI-49
// Una risposta (reply_to_message) a un messaggio che non è la notifica di nessuna busta è testo libero (la
// risposta fissa), come lo standalone; un comando che risponde a una notifica resta il comando e la busta
// non si cerca nemmeno.
func TestLettoreRispostaANotificaSenzaBusta(t *testing.T) {
	p := nuovaProvaLettore(t)
	p.isc.fermate[chatB] = 1
	risposta := messaggio(1, chatA, "va bene, grazie")
	sotto(risposta, "message")["reply_to_message"] = map[string]any{"message_id": 5}
	stop := messaggio(2, chatB, "/stop")
	sotto(stop, "message")["reply_to_message"] = map[string]any{"message_id": 6}
	p.arrivano(risposta, stop)
	p.leggi(t)
	r := p.risposte(t)
	if len(r) != 2 || r[0]["text"] != testoFisso || r[1]["text"] != "🔕 Notifiche disattivate per 1 iscrizione/i. Grazie!" {
		t.Errorf("risposte %v", r)
	}
	if fmt.Sprint(p.isc.cercate) != fmt.Sprintf("[%d 5]", chatA) || len(p.isc.scritte) != 0 {
		t.Errorf("cercate %v, scritte %v", p.isc.cercate, p.isc.scritte)
	}
}

// inventario: CLI-46
// L'attesa fra due letture (next_delay/3 di TelegramPoller): con aggiornamenti subito, una lettura vuota 1
// secondo, inattivo 30 secondi, un errore (409 o altro) da 5 secondi a 5 minuti crescendo.
func TestProssimaAttesa(t *testing.T) {
	casi := []struct {
		e              EsitoLettura
		err            error
		fallimenti     int
		attesa         time.Duration
		fallimentiDopo int
	}{
		{EsitoLettura{Gestiti: 1}, nil, 0, 0, 0},
		{EsitoLettura{Gestiti: 3}, nil, 4, 0, 0},
		{EsitoLettura{}, nil, 2, time.Second, 0},
		{EsitoLettura{Inattivo: true}, nil, 2, AttesaInattivo, 0},
		{EsitoLettura{Errore: "telegram:http:502"}, nil, 0, 5 * time.Second, 1},
		{EsitoLettura{Errore: "409: x", Conflitto: true}, nil, 0, 5 * time.Second, 1},
		{EsitoLettura{}, errors.New("database"), 0, 5 * time.Second, 1},
	}
	for _, c := range casi {
		a, f := ProssimaAttesa(c.e, c.err, c.fallimenti)
		if a != c.attesa || f != c.fallimentiDopo {
			t.Errorf("%+v %v %d: %v %d", c.e, c.err, c.fallimenti, a, f)
		}
	}
	var attese []time.Duration
	for f := range 8 {
		a, dopo := ProssimaAttesa(EsitoLettura{Errore: "409: x", Conflitto: true}, nil, f)
		if dopo != f+1 {
			t.Errorf("fallimenti %d → %d", f, dopo)
		}
		attese = append(attese, a)
	}
	if fmt.Sprint(attese) != "[5s 15s 1m0s 5m0s 5m0s 5m0s 5m0s 5m0s]" {
		t.Errorf("attese %v", attese)
	}
	if AttesaInattivo != 30*time.Second {
		t.Errorf("attesa da inattivo %v", AttesaInattivo)
	}
}

// inventario: CLI-46
// Il lavoro del lettore (TelegramPoller): la prima lettura subito, poi le attese di ProssimaAttesa; si
// ferma quando l'attesa dice di smettere. Un panico in una lettura non lo ferma: aspetta come dopo un
// errore e nel registro c'è solo il genere.
func TestLavoroDelLettore(t *testing.T) {
	giri := func(p *provaLettore, quanti int) []time.Duration {
		var attese []time.Duration
		p.l.Lavoro(context.Background(), func(_ context.Context, d time.Duration) bool {
			attese = append(attese, d)
			return len(attese) < quanti
		})
		return attese
	}
	p := nuovaProvaLettore(t)
	impostazione(t, p.d, ChiaveNotificheClienti, "")
	if a := giri(p, 2); fmt.Sprint(a) != "[30s 30s]" || p.b.quante() != 0 {
		t.Errorf("inattivo: %v, richieste %d", a, p.b.quante())
	}
	p = nuovaProvaLettore(t)
	p.b.risposta(rifiutoBot(409, descrizioneConfl))
	if a := giri(p, 5); fmt.Sprint(a) != "[5s 15s 1m0s 5m0s 5m0s]" || p.b.quante() != 5 {
		t.Errorf("409: %v, richieste %d", a, p.b.quante())
	}
	p = nuovaProvaLettore(t)
	p.arrivano(messaggio(1, chatA, "ciao"))
	// La seconda lettura riceve lo stesso aggiornamento, ormai dietro l'offset: è vuota.
	if a := giri(p, 2); fmt.Sprint(a) != "[0s 1s]" {
		t.Errorf("con aggiornamenti: %v", a)
	}
	p.arrivano()
	if a := giri(p, 1); fmt.Sprint(a) != "[1s]" {
		t.Errorf("vuota: %v", a)
	}
	p = nuovaProvaLettore(t)
	// Iscrizioni che mancano: ogni aggiornamento va in panico dentro gestisciProtetto, che lo conta come un
	// fallimento (la lettura non va in panico): le attese sono quelle dopo un errore. Il panico di una
	// lettura intera lo prova TestLavoroDopoUnPanicoDellaLettura.
	p.l.isc = nil
	p.arrivano(messaggio(1, chatA, "/stop"))
	if a := giri(p, 2); fmt.Sprint(a) != "[5s 15s]" {
		t.Errorf("panico: %v", a)
	}
}

// inventario: CLI-46
// Review della 073: il nome del negozio si legge prima di toccare le iscrizioni (lo standalone legge
// shop_name/0 una volta, prima). Se non si legge, /start fallisce senza iscrivere né consumare il token,
// e al tentativo dopo il cliente riceve il benvenuto con i pulsanti, non «link non valido».
func TestLettoreNomeNegozioPrimaDiAvviare(t *testing.T) {
	p := nuovaProvaLettore(t)
	p.isc.errNegozio = errors.New("profilo che non si legge")
	p.arrivano(messaggio(1, chatA, "/start TOKENDIPROVA"))
	if e := p.leggi(t); e.Errore == "" {
		t.Errorf("esito %+v", e)
	}
	if len(p.isc.avvii) != 0 || len(p.risposte(t)) != 0 {
		t.Fatalf("avvii %v, risposte %v", p.isc.avvii, p.risposte(t))
	}
	p.isc.errNegozio = nil
	p.arrivano(messaggio(1, chatA, "/start TOKENDIPROVA"))
	if e := p.leggi(t); e != (EsitoLettura{Gestiti: 1}) {
		t.Errorf("secondo tentativo: %+v", e)
	}
	r := p.risposte(t)
	if len(p.isc.avvii) != 1 || len(r) != 1 || r[0]["reply_markup"] == nil || !strings.Contains(r[0]["text"].(string), "presso <b>Negozio</b>") {
		t.Errorf("avvii %v, risposte %v", p.isc.avvii, r)
	}
}

// trasportoInPanico va in panico su sendMessage e passa il resto al trasporto vero.
type trasportoInPanico struct{}

func (trasportoInPanico) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.HasSuffix(r.URL.Path, "/sendMessage") {
		panic("trasporto finto: panico con " + r.URL.Path)
	}
	return trasportoTelegram.RoundTrip(r)
}

// inventario: CLI-46
// Review della 073: una risposta che va in panico si lascia cadere come una rifiutata (send_reply/4 dello
// standalone): l'aggiornamento è gestito, l'offset va avanti, /start non si ripete; nel registro solo il
// genere, mai il token.
func TestLettoreRispostaInPanico(t *testing.T) {
	p := nuovaProvaLettore(t)
	p.l.t.trasporto = trasportoInPanico{}
	p.arrivano(messaggio(1, chatA, "/start TOKENDIPROVA"))
	for range 3 {
		p.leggi(t)
	}
	if len(p.isc.avvii) != 1 || imp(t, p.d, chiaveOffset) != "2" {
		t.Errorf("avvii %v, offset %s", p.isc.avvii, imp(t, p.d, chiaveOffset))
	}
	if strings.Contains(p.log.String(), tokenFinto) || strings.Contains(p.log.String(), "TOKENDIPROVA") {
		t.Errorf("registro: %s", p.log.String())
	}
}

// orologioInPanico va in panico quando si chiede l'ora: un panico fuori da gestisciProtetto.
type orologioInPanico struct{}

func (orologioInPanico) Adesso() time.Time { panic("orologio finto") }

// inventario: CLI-46
// Review della 073 (R1): un panico di una lettura intera (qui nel salvare l'ultimo errore di un 502) non
// ferma il lavoro: aspetta come dopo un errore, e nel registro c'è il genere.
func TestLavoroDopoUnPanicoDellaLettura(t *testing.T) {
	p := nuovaProvaLettore(t)
	p.l.o = orologioInPanico{}
	p.b.risposta(func(richiestaBot) (int, string) { return 502, "bad gateway" })
	var attese []time.Duration
	p.l.Lavoro(context.Background(), func(_ context.Context, d time.Duration) bool {
		attese = append(attese, d)
		return len(attese) < 2
	})
	if fmt.Sprint(attese) != "[5s 15s]" || p.b.quante() != 2 || !strings.Contains(p.log.String(), "panico") {
		t.Errorf("attese %v, richieste %d, registro %s", attese, p.b.quante(), p.log.String())
	}
}

// inventario: CLI-46
// Review della 073 (R2): l'arresto fra due aggiornamenti. Il primo, già cominciato, si finisce (risposta e
// offset); il secondo non si tocca: nessuna richiesta per lui, l'offset resta sul secondo.
func TestLettoreArrestoFraDueAggiornamenti(t *testing.T) {
	p := nuovaProvaLettore(t)
	ctx, ferma := context.WithCancel(context.Background())
	defer ferma()
	agg := []any{messaggio(1, chatA, "ciao"), messaggio(2, chatB, "ciao")}
	p.b.risposta(func(r richiestaBot) (int, string) {
		if strings.HasSuffix(r.Percorso, "/getUpdates") {
			j, _ := json.Marshal(map[string]any{"ok": true, "result": agg})
			return 200, string(j)
		}
		ferma()
		return 200, `{"ok":true,"result":{"message_id":1}}`
	})
	if _, err := p.l.Leggi(ctx); err != nil {
		t.Fatal(err)
	}
	r := p.risposte(t)
	if len(r) != 1 || chatDi(t, r[0]) != chatA || imp(t, p.d, chiaveOffset) != "2" {
		t.Errorf("risposte %v, offset %s", r, imp(t, p.d, chiaveOffset))
	}
}

// inventario: CLI-46
// Review della 073 (R3): il trasporto della lettura lunga aspetta più dei 25 secondi che Telegram tiene la
// richiesta.
func TestTempoDellaLetturaLunga(t *testing.T) {
	if tempoLetturaLunga <= secondiLetturaLunga*time.Second {
		t.Errorf("tempo %v, lettura lunga %d s", tempoLetturaLunga, secondiLetturaLunga)
	}
}

// inventario: CLI-46
// Review della 073 (R4, R11): un 409 con una descrizione di 600 caratteri si salva tagliato a 500; una
// risposta di getUpdates di circa 2 MiB si legge e si capisce.
func TestLettoreLimitiDiLunghezza(t *testing.T) {
	p := nuovaProvaLettore(t)
	p.b.risposta(rifiutoBot(409, strings.Repeat("è", 600)))
	p.leggi(t)
	if e := imp(t, p.d, ChiaveErroreLettore); len([]rune(e)) != 500 || !strings.HasPrefix(e, "409: è") {
		t.Errorf("ultimo errore di %d caratteri", len([]rune(e)))
	}
	q := nuovaProvaLettore(t)
	q.arrivano(messaggio(1, chatA, strings.Repeat("x", 2<<20)))
	if e := q.leggi(t); e != (EsitoLettura{Gestiti: 1}) || len(q.risposte(t)) != 1 {
		t.Errorf("risposta di 2 MiB: %+v", e)
	}
}
