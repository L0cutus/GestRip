package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// chiamaBot fa una richiesta al bot finto e ne legge codice e corpo.
func chiamaBot(t *testing.T, s *httptest.Server, metodo, percorso, corpo string) (int, string) {
	t.Helper()
	var r io.Reader
	if corpo != "" {
		r = strings.NewReader(corpo)
	}
	req, err := http.NewRequest(metodo, s.URL+percorso, r)
	if err != nil {
		t.Fatal(err)
	}
	if corpo != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }() // letto qui sotto
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(b)
}

// campo legge un campo del corpo JSON con il percorso degli scenari (json:…).
func campo(t *testing.T, corpo, percorso string) string {
	t.Helper()
	return risposta{Corpo: corpo}.valore("json:" + percorso)
}

func botDiProva(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(nuovoBotFinto())
	t.Cleanup(s.Close)
	return s
}

const apiBanco = "/bot" + tokenBotBanco

func TestBotFintoGetMeETokenSbagliato(t *testing.T) {
	s := botDiProva(t)
	c, b := chiamaBot(t, s, "GET", apiBanco+"/getMe", "")
	if c != 200 || campo(t, b, "ok") != "true" || campo(t, b, "result.username") != nomeBotBanco || campo(t, b, "result.is_bot") != "true" {
		t.Errorf("getMe = %d %s", c, b)
	}
	for _, p := range []string{"/bot4242:altro/getMe", "/bot/getMe", "/x" + tokenBotBanco + "/getMe", "/getMe"} {
		c, b := chiamaBot(t, s, "GET", p, "")
		if c == 200 || strings.Contains(b, "4242:") || strings.Contains(b, `"ok":true`) {
			t.Errorf("%s = %d %s", p, c, b)
		}
	}
	if c, b := chiamaBot(t, s, "POST", apiBanco+"/deleteWebhook", "{}"); c != 404 || campo(t, b, "ok") != "false" {
		t.Errorf("metodo sconosciuto = %d %s", c, b)
	}
	if c, b := chiamaBot(t, s, "GET", "/_prova/salute", ""); c != 200 || b != "ok\n" {
		t.Errorf("salute = %d %q", c, b)
	}
}

// Un /start messo in coda arriva a getUpdates con la forma di Telegram (chat privata del cliente) e
// resta in attesa finché getUpdates non chiede l'offset dopo di lui, come Telegram.
func TestBotFintoStartEConferma(t *testing.T) {
	s := botDiProva(t)
	c, b := chiamaBot(t, s, "POST", "/_prova/aggiornamenti", `{"comando":"start","token":"TOKEN_di-prova_1"}`)
	if c != 200 {
		t.Fatalf("aggiornamenti = %d %s", c, b)
	}
	id := campo(t, b, "update_id")
	if _, b := chiamaBot(t, s, "GET", "/_prova/mandati", ""); campo(t, b, "in_attesa") != "1" || campo(t, b, "totale") != "0" || campo(t, b, "ultimo") != "null" {
		t.Errorf("mandati = %s", b)
	}
	c, b = chiamaBot(t, s, "POST", apiBanco+"/getUpdates", `{"allowed_updates":["message","callback_query"],"timeout":25}`)
	if c != 200 || campo(t, b, "ok") != "true" || campo(t, b, "result.0.update_id") != id ||
		campo(t, b, "result.0.message.text") != "/start TOKEN_di-prova_1" ||
		campo(t, b, "result.0.message.chat.type") != "private" || campo(t, b, "result.0.message.chat.id") != "4242001" ||
		campo(t, b, "result.0.message.from.is_bot") != "false" || campo(t, b, "result.1") != assenteVisibile {
		t.Errorf("getUpdates = %d %s", c, b)
	}
	// Lo stesso offset (nessuno, o l'aggiornamento stesso) non conferma.
	n := mustAtoi(t, id)
	if _, b := chiamaBot(t, s, "POST", apiBanco+"/getUpdates", `{"offset":`+id+`,"timeout":25}`); campo(t, b, "result.0.update_id") != id {
		t.Errorf("offset uguale: %s", b)
	}
	if _, b := chiamaBot(t, s, "GET", "/_prova/mandati", ""); campo(t, b, "in_attesa") != "1" {
		t.Errorf("confermato troppo presto: %s", b)
	}
	if _, b := chiamaBot(t, s, "POST", apiBanco+"/getUpdates", `{"offset":`+itoa(n+1)+`,"timeout":25}`); campo(t, b, "result") != "[]" {
		t.Errorf("dopo la conferma: %s", b)
	}
	if _, b := chiamaBot(t, s, "GET", "/_prova/mandati", ""); campo(t, b, "in_attesa") != "0" {
		t.Errorf("non confermato: %s", b)
	}
	// /stop è il secondo aggiornamento, con un id dopo.
	_, b = chiamaBot(t, s, "POST", "/_prova/aggiornamenti", `{"comando":"stop"}`)
	if mustAtoi(t, campo(t, b, "update_id")) != n+1 {
		t.Errorf("secondo id = %s", b)
	}
	if _, b := chiamaBot(t, s, "POST", apiBanco+"/getUpdates", `{"offset":`+itoa(n+1)+`}`); campo(t, b, "result.0.message.text") != "/stop" {
		t.Errorf("stop = %s", b)
	}
}

// Il pulsante del consenso è quello dell'ultimo benvenuto mandato: «sì» e «no» danno il callback del
// Go (cli:tx:1:<id>, cli:tx:0:<id>) sulla chat del cliente. Senza un benvenuto il bot rifiuta.
func TestBotFintoPulsante(t *testing.T) {
	s := botDiProva(t)
	if c, b := chiamaBot(t, s, "POST", "/_prova/aggiornamenti", `{"comando":"si"}`); c != 409 {
		t.Errorf("senza benvenuto = %d %s", c, b)
	}
	tastiera := func(id string) string {
		return `{"chat_id":4242001,"text":"benvenuto","parse_mode":"HTML","reply_markup":{"inline_keyboard":[[{"text":"Sì","callback_data":"cli:tx:1:` +
			id + `"}],[{"text":"No","callback_data":"cli:tx:0:` + id + `"}]]}}`
	}
	// Un messaggio del cliente prima, così il message_id del benvenuto non coincide con la sua posizione fra i mandati.
	chiamaBot(t, s, "POST", "/_prova/aggiornamenti", `{"comando":"stop"}`)
	var midSette string
	for i, corpo := range []string{tastiera("6"), tastiera("7"), `{"chat_id":4242001,"text":"altro"}`} {
		c, b := chiamaBot(t, s, "POST", apiBanco+"/sendMessage", corpo)
		if c != 200 || campo(t, b, "ok") != "true" || campo(t, b, "result.message_id") == assenteVisibile {
			t.Fatalf("sendMessage = %d %s", c, b)
		}
		if i == 1 {
			midSette = campo(t, b, "result.message_id")
		}
	}
	for _, x := range []struct{ comando, dati string }{{"si", "cli:tx:1:7"}, {"no", "cli:tx:0:7"}} {
		c, b := chiamaBot(t, s, "POST", "/_prova/aggiornamenti", `{"comando":"`+x.comando+`"}`)
		if c != 200 {
			t.Fatalf("%s = %d %s", x.comando, c, b)
		}
		id := campo(t, b, "update_id")
		_, b = chiamaBot(t, s, "POST", apiBanco+"/getUpdates", `{}`)
		var trovato bool
		for i := 0; i < 3; i++ {
			p := "result." + itoa(int64(i))
			if campo(t, b, p+".update_id") == id {
				trovato = true
				if campo(t, b, p+".callback_query.data") != x.dati || campo(t, b, p+".callback_query.message.chat.id") != "4242001" ||
					campo(t, b, p+".callback_query.message.chat.type") != "private" || campo(t, b, p+".callback_query.id") == assenteVisibile ||
					campo(t, b, p+".callback_query.from.id") != "4242001" ||
					campo(t, b, p+".callback_query.message.message_id") != midSette {
					t.Errorf("%s: %s", x.comando, b)
				}
			}
		}
		if !trovato {
			t.Errorf("%s non in getUpdates: %s", x.comando, b)
		}
	}
}

// I mandati si leggono per metodo, l'ultimo con il suo corpo, mai con il token del bot.
func TestBotFintoMandati(t *testing.T) {
	s := botDiProva(t)
	chiamaBot(t, s, "POST", apiBanco+"/sendMessage", `{"chat_id":"4242001","text":"<b>uno</b> & due","parse_mode":"HTML","reply_markup":{"inline_keyboard":[[{"text":"📂 Apri","url":"https://go-telegram.invalid:8443/r/x"}]]}}`)
	if c, b := chiamaBot(t, s, "POST", apiBanco+"/answerCallbackQuery", `{"callback_query_id":"cb-1"}`); c != 200 || campo(t, b, "result") != "true" {
		t.Errorf("answerCallbackQuery = %d %s", c, b)
	}
	chiamaBot(t, s, "POST", apiBanco+"/sendMessage", `{"chat_id":4242001,"text":"due"}`)
	_, b := chiamaBot(t, s, "GET", "/_prova/mandati?metodo=sendMessage", "")
	if campo(t, b, "totale") != "2" || campo(t, b, "ultimo.text") != "due" || campo(t, b, "ultimo.chat_id") != "4242001" {
		t.Errorf("mandati sendMessage = %s", b)
	}
	_, b = chiamaBot(t, s, "GET", "/_prova/mandati?metodo=answerCallbackQuery", "")
	if campo(t, b, "totale") != "1" || campo(t, b, "ultimo.callback_query_id") != "cb-1" {
		t.Errorf("mandati answerCallbackQuery = %s", b)
	}
	_, b = chiamaBot(t, s, "GET", "/_prova/mandati", "")
	if campo(t, b, "totale") != "3" || campo(t, b, "ultimo.text") != "due" {
		t.Errorf("tutti i mandati = %s", b)
	}
	if strings.Contains(b, tokenBotBanco) || strings.Contains(b, "4242:") {
		t.Errorf("token nei mandati: %s", b)
	}
	// Il primo, letto dal metodo con il suo indice: testo, HTML e pulsante come li ha mandati il Go.
	_, b = chiamaBot(t, s, "GET", "/_prova/mandati?metodo=sendMessage&indice=1", "")
	if campo(t, b, "ultimo.text") != "<b>uno</b> & due" || campo(t, b, "ultimo.parse_mode") != "HTML" ||
		campo(t, b, "ultimo.reply_markup.inline_keyboard.0.0.url") != "https://go-telegram.invalid:8443/r/x" {
		t.Errorf("primo mandato = %s", b)
	}
	if c, _ := chiamaBot(t, s, "GET", "/_prova/mandati?indice=9", ""); c != 400 {
		t.Errorf("indice fuori = %d", c)
	}
}

func TestBotFintoRifiuti(t *testing.T) {
	s := botDiProva(t)
	for _, corpo := range []string{`{"comando":"start","token":""}`, `{"comando":"start","token":"a b"}`,
		`{"comando":"start","token":"` + strings.Repeat("a", 65) + `"}`, `{"comando":"start","token":"x\""}`,
		`{"comando":"boh"}`, `non json`, `{"comando":"stop","altro":1}`} {
		if c, b := chiamaBot(t, s, "POST", "/_prova/aggiornamenti", corpo); c != 400 {
			t.Errorf("%s = %d %s", corpo, c, b)
		}
	}
	if _, b := chiamaBot(t, s, "GET", "/_prova/mandati", ""); campo(t, b, "in_attesa") != "0" {
		t.Errorf("un rifiuto è finito in coda: %s", b)
	}
	// Un metodo con «/» non è un metodo del Bot API: 401 come un percorso sbagliato, niente registrato.
	for _, p := range []string{apiBanco + "/sendMessage/x", apiBanco + "/getMe/"} {
		if c, b := chiamaBot(t, s, "POST", p, `{"chat_id":4242001,"text":"x"}`); c != 401 || campo(t, b, "ok") != "false" {
			t.Errorf("%s = %d %s", p, c, b)
		}
	}
	if c, b := chiamaBot(t, s, "POST", "/_prova/aggiornamenti", `{"comando":"start","token":"`+strings.Repeat("a", 64)+`"}`); c != 200 {
		t.Errorf("token di 64 = %d %s", c, b)
	}
	for _, corpo := range []string{`{"chat_id":4242001,"text":""}`, `{"text":"x"}`, `non json`} {
		if c, b := chiamaBot(t, s, "POST", apiBanco+"/sendMessage", corpo); c != 400 || campo(t, b, "ok") != "false" {
			t.Errorf("sendMessage %s = %d %s", corpo, c, b)
		}
	}
	if _, b := chiamaBot(t, s, "GET", "/_prova/mandati", ""); campo(t, b, "totale") != "0" {
		t.Errorf("un sendMessage rifiutato è nei mandati: %s", b)
	}
}

// iscrizioniFinte stanno al posto delle buste nel lettore vero: registrano le chiamate.
type iscrizioniFinte struct {
	mu        sync.Mutex
	avviate   []string
	consensi  []string
	fermate   int
	idAvviato int64
}

func (f *iscrizioniFinte) AvviaTelegram(_ context.Context, chat int64, token string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.avviate = append(f.avviate, itoa(chat)+" "+token)
	return f.idAvviato, nil
}

func (f *iscrizioniFinte) ConsensoTelegram(_ context.Context, chat, id int64, v bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id != f.idAvviato {
		return uscita.ErrIscrizioneTelegramNonTrovata
	}
	s := "0"
	if v {
		s = "1"
	}
	f.consensi = append(f.consensi, itoa(chat)+" "+itoa(id)+" "+s)
	return nil
}

func (f *iscrizioniFinte) FermaChatTelegram(_ context.Context, chat int64) (int, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fermate++
	return 1, true, nil
}

func (f *iscrizioniFinte) NomeNegozio(context.Context) (string, error) { return "Negozio", nil }

// Il bot finto parla con il lettore vero del Go (uscita.LettoreTelegram) e con il provider vero, su un
// database con le impostazioni seminate da banco su: /start, «Sì», /stop e un messaggio della coda
// fanno quello che il banco si aspetta.
func TestBotFintoConIlLettoreVero(t *testing.T) {
	s := botDiProva(t)
	p := dbGoDiProva(t)
	if err := seminaTelegramGo(p); err != nil {
		t.Fatal(err)
	}
	d, err := db.Apri(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Chiudi() }()
	ctx := context.Background()
	tg := uscita.NuovoTelegram(d, s.URL)
	if nome, err := tg.ChiediNome(ctx); err != nil || nome != nomeBotBanco {
		t.Errorf("ChiediNome = %q %v", nome, err)
	}
	isc := &iscrizioniFinte{idAvviato: 31}
	l := uscita.NuovoLettoreTelegram(d, clock.Sistema{}, tg, isc, indirizzoGoTelegram)
	leggi := func(gestiti int) {
		t.Helper()
		e, err := l.Leggi(ctx)
		if err != nil || e.Inattivo || e.Errore != "" || e.Gestiti != gestiti {
			t.Fatalf("Leggi = %+v %v", e, err)
		}
	}
	leggi(0) // il lettore è attivo con le impostazioni del banco e la coda vuota
	chiamaBot(t, s, "POST", "/_prova/aggiornamenti", `{"comando":"start","token":"TOKENsintetico"}`)
	leggi(1)
	_, b := chiamaBot(t, s, "GET", "/_prova/mandati?metodo=sendMessage", "")
	if len(isc.avviate) != 1 || isc.avviate[0] != "4242001 TOKENsintetico" || campo(t, b, "totale") != "1" ||
		campo(t, b, "ultimo.reply_markup.inline_keyboard.0.0.callback_data") != "cli:tx:1:31" || campo(t, b, "in_attesa") != "1" {
		t.Fatalf("dopo /start: %v %s", isc.avviate, b)
	}
	chiamaBot(t, s, "POST", "/_prova/aggiornamenti", `{"comando":"si"}`)
	leggi(1)
	_, b = chiamaBot(t, s, "GET", "/_prova/mandati?metodo=sendMessage", "")
	if len(isc.consensi) != 1 || isc.consensi[0] != "4242001 31 1" || campo(t, b, "ultimo.text") != "✅ Notifiche attive. Ti avviserò quando ci sono aggiornamenti." {
		t.Errorf("dopo «Sì»: %v %s", isc.consensi, b)
	}
	if _, b := chiamaBot(t, s, "GET", "/_prova/mandati?metodo=answerCallbackQuery", ""); campo(t, b, "totale") != "1" {
		t.Errorf("answerCallbackQuery: %s", b)
	}
	chiamaBot(t, s, "POST", "/_prova/aggiornamenti", `{"comando":"stop"}`)
	leggi(1)
	leggi(0) // la lettura dopo conferma l'ultimo aggiornamento
	if _, b := chiamaBot(t, s, "GET", "/_prova/mandati", ""); isc.fermate != 1 || campo(t, b, "in_attesa") != "0" {
		t.Errorf("dopo /stop: %d %s", isc.fermate, b)
	}
	// Un messaggio della coda (sendMessage del provider) arriva com'è.
	rif, err := tg.Consegna(ctx, uscita.Messaggio{Canale: uscita.CanaleTelegram, Destinatario: "customer", Indirizzo: "4242001", Testo: "<b>ciao</b>",
		Formato: ptrStringa(uscita.FormatoHTML), Tastiera: ptrStringa(`{"inline_keyboard":[[{"text":"📂","url":"https://go-telegram.invalid:8443/r/t"}]]}`)})
	if err != nil || rif == "" {
		t.Fatalf("Consegna = %q %v", rif, err)
	}
	_, b = chiamaBot(t, s, "GET", "/_prova/mandati?metodo=sendMessage", "")
	if campo(t, b, "ultimo.text") != "<b>ciao</b>" || campo(t, b, "ultimo.parse_mode") != "HTML" || campo(t, b, "ultimo.chat_id") != "4242001" ||
		campo(t, b, "ultimo.reply_markup.inline_keyboard.0.0.url") != "https://go-telegram.invalid:8443/r/t" {
		t.Errorf("messaggio della coda: %s", b)
	}
}

func ptrStringa(s string) *string { return &s }

// Il comando del bot finto si ferma con il contesto (lo stop del container) e rifiuta argomenti in più.
func TestCmdBotFinto(t *testing.T) {
	if err := cmdBotFinto([]string{"-ascolta", "127.0.0.1:0", "avanzo"}); err == nil {
		t.Error("argomento in più accettato")
	}
	ctx, ferma := context.WithCancel(context.Background())
	pronto := make(chan string, 1)
	fatto := make(chan error, 1)
	go func() { fatto <- serviBotFinto(ctx, "127.0.0.1:0", func(a string) { pronto <- a }) }()
	var a string
	select {
	case a = <-pronto:
	case err := <-fatto:
		t.Fatalf("il bot finto non è partito: %v", err)
	case <-time.After(10 * time.Second):
		ferma()
		t.Fatal("il bot finto non è pronto dopo 10 s")
	}
	res, err := http.Get("http://" + a + "/_prova/salute")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close() // solo il codice
	if res.StatusCode != 200 {
		t.Errorf("salute = %d", res.StatusCode)
	}
	ferma()
	if err := <-fatto; err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Errorf("fermato con %v", err)
	}
}

func mustAtoi(t *testing.T, s string) int64 {
	t.Helper()
	var n int64
	if err := json.Unmarshal([]byte(s), &n); err != nil {
		t.Fatalf("%q non è un numero", s)
	}
	return n
}
