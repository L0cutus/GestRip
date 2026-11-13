package uscita

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Il provider Telegram della coda (slice 071, CLI-51 parte Telegram, ADM-10), come
// Outgoing.Providers.Telegram dello standalone a go-baseline (telegram_test.exs): sendMessage e getMe sul
// Bot API finto, mai su Telegram. Dati inventati.

func messaggioTelegram(parte string) Messaggio {
	return Messaggio{ID: 1, Canale: CanaleTelegram, Destinatario: parte, Indirizzo: "4242", Testo: "Ciao <b>Ada</b>"}
}

// inventario: CLI-51, ADM-10
// deliver/2: un POST a <endpoint>/bot<token>/sendMessage con chat_id (l'indirizzo della riga, com'è) e
// text; parse_mode e reply_markup solo se la riga li porta (reply_markup è il JSON salvato, come oggetto).
// «ok: true» è accettato, e il riferimento è il message_id.
func TestTelegramConsegna(t *testing.T) {
	_, p, b := codaTelegram(t)
	rif, motivo := mandaA(t, p, messaggioTelegram("shop"))
	if rif != "77" || motivo != "" {
		t.Fatalf("rif %q motivo %q", rif, motivo)
	}
	r := b.tutte()[0]
	if r.Metodo != "POST" || r.Percorso != "/bot"+tokenFinto+"/sendMessage" {
		t.Errorf("richiesta %s %s", r.Metodo, r.Percorso)
	}
	if r.Grezzo != `{"chat_id":"4242","text":"Ciao <b>Ada</b>"}` {
		t.Errorf("corpo senza formato %s", r.Grezzo)
	}
	m := messaggioTelegram("shop")
	formato, tastiera := FormatoHTML, `{"inline_keyboard":[[{"text":"📂 Apri scheda riparazione","url":"https://negozio.example.invalid/r/TOKEN"}]]}`
	m.Formato, m.Tastiera = &formato, &tastiera
	if _, motivo := mandaA(t, p, m); motivo != "" {
		t.Fatal(motivo)
	}
	r = b.tutte()[1]
	if r.Corpo["parse_mode"] != "HTML" {
		t.Errorf("parse_mode %v", r.Corpo["parse_mode"])
	}
	j, _ := json.Marshal(r.Corpo["reply_markup"])
	if string(j) != tastiera {
		t.Errorf("reply_markup %s", j)
	}
	// Un message_id che non è un numero o un testo, o una risposta che non si capisce.
	for corpo, atteso := range map[string]string{
		`{"ok":true,"result":{"message_id":"abc"}}`: "",
		`{"ok":true,"result":{}}`:                   "telegram:risposta non riconosciuta",
		`non è JSON`:                                "telegram:risposta non riconosciuta",
		`{"ok":false,"description":"Bad Request: message is too long"}`: "telegram:Bad Request: message is too long",
	} {
		b.risposta(func(richiestaBot) (int, string) { return 200, corpo })
		if _, motivo := mandaA(t, p, messaggioTelegram("shop")); motivo != atteso {
			t.Errorf("%s: %q", corpo, motivo)
		}
	}
}

// inventario: CLI-51
// refusal/3: un rifiuto del Bot API è «telegram:http:<stato>:<descrizione>» con le parole di Telegram, o
// «telegram:http:<stato>» senza descrizione; il token non c'è mai, nemmeno se Telegram lo ripete. Un
// redirect non si segue (è un rifiuto con il suo stato, come hackney).
func TestTelegramRifiuti(t *testing.T) {
	_, p, b := codaTelegram(t)
	casi := []struct {
		stato  int
		corpo  string
		atteso string
	}{
		{400, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`, "telegram:http:400:Bad Request: chat not found"},
		{400, ``, "telegram:http:400"},
		{403, `{"ok":false,"description":"Forbidden: bot was blocked by the user"}`, "telegram:http:403:Forbidden: bot was blocked by the user"},
		{429, `{"ok":false,"description":"Too Many Requests: retry after 37","parameters":{"retry_after":37}}`, "telegram:http:429:Too Many Requests: retry after 37"},
		{401, `{"ok":false,"description":"Unauthorized: token ` + tokenFinto + ` non valido"}`, "telegram:http:401:Unauthorized: token *** non valido"},
		{502, `<html>Bad Gateway</html>`, "telegram:http:502"},
		{302, ``, "telegram:http:302"},
	}
	for _, c := range casi {
		b.risposta(func(richiestaBot) (int, string) { return c.stato, c.corpo })
		if _, motivo := mandaA(t, p, messaggioTelegram("shop")); motivo != c.atteso {
			t.Errorf("%d %s: %q", c.stato, c.corpo, motivo)
		}
	}
	b.risposta(func(richiestaBot) (int, string) { return 302, `` })
	n := b.quante()
	if _, motivo := mandaA(t, p, messaggioTelegram("shop")); motivo != "telegram:http:302" || b.quante() != n+1 {
		t.Errorf("redirect seguito: %q, %d richieste", motivo, b.quante()-n)
	}
}

// inventario: CLI-51
// Errori del trasporto: «telegram:transport:<genere>» con un genere fisso (come la 069 per l'SMTP), mai
// l'errore di net/http, che porta l'indirizzo e quindi il token. Il tempo massimo di una richiesta (15 s
// in serve, qui accorciato) chiude anche un server che non risponde.
func TestTelegramErroriDiTrasporto(t *testing.T) {
	d, _ := apriDB(t)
	impostaTelegram(t, d, tokenFinto, chatFinta, true)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	chiuso := "http://" + l.Addr().String()
	_ = l.Close()
	p := NuovoTelegram(d, chiuso)
	if _, motivo := mandaA(t, p, messaggioTelegram("shop")); motivo != "telegram:transport::econnrefused" {
		t.Errorf("rifiutato %q", motivo)
	}
	muto := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(muto.Close)
	p = NuovoTelegram(d, muto.URL)
	p.tempo = 200 * time.Millisecond
	inizio := time.Now()
	_, motivo := mandaA(t, p, messaggioTelegram("shop"))
	if motivo != "telegram:transport::timeout" || time.Since(inizio) > 3*time.Second {
		t.Errorf("muto %q dopo %s", motivo, time.Since(inizio))
	}
	if strings.Contains(motivo, tokenFinto) {
		t.Error("token nel motivo")
	}
	// Una risposta enorme non si legge tutta: oltre 1 MiB non si capisce.
	grande := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":true,"result":{"message_id":1},"x":"`+strings.Repeat("a", 2<<20)+`"}`)
	}))
	t.Cleanup(grande.Close)
	if _, motivo := mandaA(t, NuovoTelegram(d, grande.URL), messaggioTelegram("shop")); motivo != "telegram:risposta non riconosciuta" {
		t.Errorf("risposta enorme %q", motivo)
	}
}

// inventario: CLI-51, ADM-10
// attempt/1: senza configurazione usabile, o con l'invio Telegram spento (D-03, solo Go), «not_configured»
// senza nessuna richiesta; ConsegnaAMano (la revisione della D-27 e la prova) salta solo l'interruttore.
// Una riga di un cliente con «Notifiche Telegram ai clienti» spente è «telegram:notifiche ai clienti
// spente», senza richiesta (slice 137 dello standalone); la riga del negozio parte lo stesso.
func TestTelegramSpentoNonApre(t *testing.T) {
	c, p, b := codaTelegram(t)
	ctx := context.Background()
	impostaTelegram(t, c.d, tokenFinto, chatFinta, false)
	if _, err := p.Consegna(ctx, messaggioTelegram("shop")); err != Rifiuto("not_configured") {
		t.Errorf("spento: %v", err)
	}
	if acceso, _ := p.Attivo(ctx); acceso {
		t.Error("attivo da spento")
	}
	if rif, err := p.ConsegnaAMano(ctx, messaggioTelegram("shop")); err != nil || rif != "77" {
		t.Errorf("a mano da spento: %q %v", rif, err)
	}
	if _, err := p.ConsegnaAMano(ctx, messaggioTelegram("customer")); err != Rifiuto("telegram:notifiche ai clienti spente") {
		t.Errorf("cliente con le notifiche spente: %v", err)
	}
	impostazione(t, c.d, ChiaveNotificheClienti, "1")
	if _, err := p.ConsegnaAMano(ctx, messaggioTelegram("customer")); err != nil {
		t.Errorf("cliente con le notifiche accese: %v", err)
	}
	if n := b.quante(); n != 2 {
		t.Errorf("richieste %d", n)
	}
	// Un token importato che non ha la forma «numero:testo» (finirebbe nel percorso dell'indirizzo):
	// non configurato, anche a mano.
	for _, token := range []string{"123456:AAH/../../x", "123456:AAH?x=1", "123456:AAH\n", "abc:def", " 123456:AAH"} {
		impostaTelegram(t, c.d, token, chatFinta, true)
		if _, err := p.ConsegnaAMano(ctx, messaggioTelegram("shop")); err != Rifiuto("not_configured") {
			t.Errorf("token %q: %v", token, err)
		}
	}
	if n := b.quante(); n != 2 {
		t.Errorf("richieste con token sbagliati: %d", n)
	}
}

// inventario: ADM-10
// get_me/1: GET <endpoint>/bot<token>/getMe; il nome del bot se «ok» e «username» non vuoto, altrimenti
// «telegram:getMe senza username»; un rifiuto «telegram:http:<stato>» (senza descrizione).
func TestTelegramChiediNome(t *testing.T) {
	_, p, b := codaTelegram(t)
	ctx := context.Background()
	nome, err := p.ChiediNome(ctx)
	if err != nil || nome != "negozio_prova_bot" {
		t.Fatalf("%q %v", nome, err)
	}
	if r := b.tutte()[0]; r.Metodo != "GET" || r.Percorso != "/bot"+tokenFinto+"/getMe" {
		t.Errorf("richiesta %s %s", r.Metodo, r.Percorso)
	}
	for corpo, stato := range map[string]int{`{"ok":true,"result":{"username":""}}`: 200, `{"ok":true}`: 200} {
		b.risposta(func(richiestaBot) (int, string) { return stato, corpo })
		if _, err := p.ChiediNome(ctx); err != Rifiuto("telegram:getMe senza username") {
			t.Errorf("%s: %v", corpo, err)
		}
	}
	b.risposta(rifiutoBot(401, "Unauthorized"))
	if _, err := p.ChiediNome(ctx); err != Rifiuto("telegram:http:401") {
		t.Errorf("401: %v", err)
	}
}

// inventario: ADM-10
// L'indirizzo del Bot API si cambia solo dall'ambiente (TELEGRAM_API_ENDPOINT, config/runtime.exs):
// assente è Telegram; presente deve essere un indirizzo http(s) con un host, anche vuoto è un errore
// (System.get_env dà ""), senza la «/» finale. Solo Go: niente query, frammento o utente (finirebbero in
// mezzo all'indirizzo con il token).
func TestEndpointDaAmbiente(t *testing.T) {
	if e, err := EndpointDaAmbiente("", false); err != nil || e != "https://api.telegram.org" {
		t.Errorf("assente: %q %v", e, err)
	}
	for v, atteso := range map[string]string{
		"https://bot.example.invalid/":          "https://bot.example.invalid",
		"http://127.0.0.1:8081":                 "http://127.0.0.1:8081",
		"https://bot.example.invalid/prefisso/": "https://bot.example.invalid/prefisso",
	} {
		if e, err := EndpointDaAmbiente(v, true); err != nil || e != atteso {
			t.Errorf("%q: %q %v", v, e, err)
		}
	}
	for _, v := range []string{"", "ftp://bot.example.invalid", "https://", "bot.example.invalid", "https:///x",
		"https://bot.example.invalid?x=1", "https://bot.example.invalid#x", "https://utente@bot.example.invalid"} {
		if e, err := EndpointDaAmbiente(v, true); err == nil {
			t.Errorf("%q accettato: %q", v, e)
		} else if !strings.Contains(err.Error(), "TELEGRAM_API_ENDPOINT") {
			t.Errorf("%q: %v", v, err)
		}
	}
}

// inventario: ADM-10
// Config.get(:telegram) e usable?/1: token (segreti_provider) e chat (impostazioni) presenti; un valore di
// soli spazi è assente. Seconda serratura solo Go (come la 069 per l'SMTP): token «numero:testo» e chat
// numerica o «@nome» esatti, senza a capo finale. Il token non si stampa mai.
func TestConfigTelegram(t *testing.T) {
	d, _ := apriDB(t)
	ctx := context.Background()
	c, err := LeggiTelegram(ctx, d.Lettura)
	if err != nil || c.Usabile() || c.Token.Presente() || c.Chat != "" {
		t.Fatalf("vuota: %+v %v", c, err)
	}
	impostaTelegram(t, d, tokenFinto, "  ", true)
	if c, _ = LeggiTelegram(ctx, d.Lettura); c.Usabile() || c.Chat != "" {
		t.Errorf("chat di spazi: %+v", c)
	}
	for chat, usabile := range map[string]bool{chatFinta: true, "4242": true, "@negozio_prova": true, "@negozio-prova": false,
		"4242\n": false, "42 42": false, "@": false, "-": false} {
		impostaTelegram(t, d, tokenFinto, chat, true)
		c, _ = LeggiTelegram(ctx, d.Lettura)
		if c.Usabile() != usabile {
			t.Errorf("chat %q: usabile %v", chat, c.Usabile())
		}
		if attivo, _ := TelegramAttivo(ctx, d.Lettura); attivo != usabile {
			t.Errorf("chat %q: attivo %v", chat, attivo)
		}
	}
	impostaTelegram(t, d, tokenFinto, chatFinta, false)
	if attivo, _ := TelegramAttivo(ctx, d.Lettura); attivo {
		t.Error("attivo con l'interruttore spento")
	}
	c, _ = LeggiTelegram(ctx, d.Lettura)
	for _, s := range []string{fmt.Sprint(c), fmt.Sprintf("%+v", c), fmt.Sprintf("%#v", c)} {
		if strings.Contains(s, tokenFinto) || strings.Contains(s, "AAH") {
			t.Errorf("token stampato: %s", s)
		}
	}
	if accese, _ := NotificheClientiAccese(ctx, d.Lettura); accese {
		t.Error("notifiche accese senza riga")
	}
	impostazione(t, d, ChiaveNotificheClienti, "1")
	if accese, _ := NotificheClientiAccese(ctx, d.Lettura); !accese {
		t.Error("notifiche spente con 1")
	}
	impostazione(t, d, ChiaveNotificheClienti, "true")
	if accese, _ := NotificheClientiAccese(ctx, d.Lettura); accese {
		t.Error("notifiche accese con «true»: vale solo «1»")
	}
}

// inventario: CLI-51
// Review della 071: il client smette di leggere una risposta senza fine dopo circa 1 MiB (solo Go): la
// richiesta torna subito, ben prima del tempo massimo, come una risposta che non si capisce, e il server
// che scrive senza sosta si ferma con un errore di scrittura dopo pochi MiB (quelli che stanno nei buffer
// del sistema), non dopo il tempo massimo.
func TestTelegramRispostaSenzaFine(t *testing.T) {
	d, _ := apriDB(t)
	impostaTelegram(t, d, tokenFinto, chatFinta, true)
	var scritti atomic.Int64
	fermato := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(fermato)
		pezzo := []byte(strings.Repeat("a", 64<<10))
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1},"x":"`))
		for {
			n, err := w.Write(pezzo)
			scritti.Add(int64(n))
			if err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			if r.Context().Err() != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	p := NuovoTelegram(d, srv.URL)
	p.tempo = 8 * time.Second
	inizio := time.Now()
	_, motivo := mandaA(t, p, messaggioTelegram("shop"))
	if motivo != "telegram:risposta non riconosciuta" || time.Since(inizio) > 4*time.Second {
		t.Errorf("flusso senza fine: %q dopo %s", motivo, time.Since(inizio))
	}
	select {
	case <-fermato:
	case <-time.After(4 * time.Second):
		t.Fatal("il server scrive ancora: il client non ha smesso di leggere")
	}
	if n := scritti.Load(); n > 64<<20 {
		t.Errorf("il server ha scritto %d byte", n)
	}
}

// inventario: CLI-51
// Review della 071: il provider non passa da un proxy dell'ambiente (HTTP(S)_PROXY): il token viaggia
// nel percorso, e un proxy lo vedrebbe. Il trasporto è suo, con Proxy nil. Segreti() dà il token
// salvato, così la pulizia della coda (scrub/2) lo toglie anche lei.
func TestTelegramSenzaProxyESegreti(t *testing.T) {
	if trasportoTelegram.Proxy != nil {
		t.Error("il trasporto del provider Telegram usa un proxy")
	}
	d, _ := apriDB(t)
	p := NuovoTelegram(d, EndpointTelegram)
	if s := p.Segreti(); len(s) != 0 {
		t.Errorf("segreti senza token: %v", s)
	}
	impostaTelegram(t, d, tokenFinto, chatFinta, false)
	if s := p.Segreti(); len(s) != 1 || s[0] != tokenFinto {
		t.Errorf("segreti %d", len(s))
	}
	if got := pulisci("motivo con "+tokenFinto, p); got != "motivo con ***" {
		t.Errorf("pulizia della coda %q", got)
	}
}
