package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// goTelegramFinto fa il Go con Telegram dello scenario: una pagina con il campo nascosto del popup e il
// redirect a t.me del pulsante. Registra i corpi che riceve.
type goTelegramFinto struct {
	mu     sync.Mutex
	corpi  []string
	numero int
}

func (g *goTelegramFinto) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm() // un modulo sbagliato resta vuoto
	g.mu.Lock()
	g.corpi = append(g.corpi, r.URL.Path+"?"+r.PostForm.Encode())
	g.numero++
	g.mu.Unlock()
	switch r.URL.Path {
	case "/pagina":
		_, _ = fmt.Fprint(w, `<meta name="csrf-token" content="tok-1"><form><input name="notifica" type="hidden" value="gettone&amp;1+2"></form>`)
	case "/attiva":
		http.Redirect(w, r, "https://t.me/"+nomeBotBanco+"?start=TOKEN_sintetico-9", http.StatusFound)
	case "/attiva-http":
		http.Redirect(w, r, "http://t.me/"+nomeBotBanco+"?start=TOKEN", http.StatusFound)
	case "/attiva-altrove":
		http.Redirect(w, r, "https://altro.invalid/"+nomeBotBanco+"?start=TOKEN", http.StatusFound)
	case "/attiva-sbagliato":
		http.Redirect(w, r, "https://t.me/"+nomeBotBanco+"?start=a%22b", http.StatusFound)
	default:
		_, _ = fmt.Fprint(w, "ok")
	}
}

// contaRichieste conta le richieste di un server che non deve riceverne.
func contaRichieste(n *atomic.Int64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { n.Add(1) }))
}

var scenarioTelegram = scenario{Nome: "tg", Istanza: istanzaGoTelegram, ProvaCon: []string{datiSinteticiImport}, Passi: []passo{
	{Metodo: "GET", Percorso: "/pagina", SoloGo: "D-03", Atteso: map[string]string{"codice": "200"}},
	{Metodo: "POST", Percorso: "/invia", Corpo: "_csrf_token={csrf}&notifica={campo:notifica}&azione=invia", SoloGo: "D-03", Atteso: map[string]string{"codice": "200"}},
	{Metodo: "POST", Percorso: "/attiva", SoloGo: "D-03", Atteso: map[string]string{"codice": "302"}},
	{Metodo: "POST", Percorso: "/_prova/aggiornamenti", Corpo: `{"comando":"start","token":"{start}"}`, Bot: true, SoloGo: "D-03",
		Atteso: map[string]string{"codice": "200"}},
	{Metodo: "GET", Percorso: "/_prova/mandati", Bot: true, SoloGo: "D-03", Atteso: map[string]string{"json:in_attesa": "1", "json:totale": "0"}},
	// Il passo del bot non cambia la pagina di prima del Go: {campo:…} viene ancora dalla pagina.
	{Metodo: "GET", Percorso: "/pagina", SoloGo: "D-03", Atteso: map[string]string{"codice": "200"}},
	{Metodo: "GET", Percorso: "/_prova/mandati", Bot: true, SoloGo: "D-03", Atteso: map[string]string{"codice": "200"}},
	{Metodo: "POST", Percorso: "/invia", Corpo: "notifica={campo:notifica}", SoloGo: "D-03", Atteso: map[string]string{"codice": "200"}},
}}

// Lo scenario di un'istanza go-telegram gira tutto sul Go con Telegram e sul bot finto, mai sullo
// standalone né sul Go del confronto; {campo:…} e {start} vengono dalla risposta di prima del Go.
func TestScenarioGoTelegram(t *testing.T) {
	var nSA, nGo atomic.Int64
	sa, gx := contaRichieste(&nSA), contaRichieste(&nGo)
	defer sa.Close()
	defer gx.Close()
	g := &goTelegramFinto{}
	gtg := httptest.NewServer(g)
	defer gtg.Close()
	bot := botDiProva(t)
	tg := istanzeTelegram{Go: istanza{"go-telegram", gtg.URL, gtg.Client()}, Bot: istanza{"bot", bot.URL, bot.Client()}}
	rap := eseguiScenariTutti([]scenario{scenarioTelegram}, nil, istanza{"standalone", sa.URL, sa.Client()}, istanza{"go", gx.URL, gx.Client()},
		tg, credenziali{}, datiSinteticiImport)
	if rap.Inattese != 0 || len(rap.Errori) != 0 || rap.Passi != len(scenarioTelegram.Passi) || !rap.Verdi["tg"] {
		t.Errorf("rapporto = %+v", rap)
	}
	if nSA.Load() != 0 || nGo.Load() != 0 {
		t.Errorf("richieste allo standalone %d, al Go del confronto %d", nSA.Load(), nGo.Load())
	}
	if len(g.corpi) != 5 || g.corpi[1] != "/invia?_csrf_token=tok-1&azione=invia&notifica=gettone%261%2B2" || g.corpi[4] != "/invia?notifica=gettone%261%2B2" {
		t.Errorf("corpi del Go = %q", g.corpi)
	}
	_, b := chiamaBot(t, bot, "POST", apiBanco+"/getUpdates", "{}")
	if campo(t, b, "result.0.message.text") != "/start TOKEN_sintetico-9" {
		t.Errorf("start nel bot = %s", b)
	}
}

// Senza le istanze go-telegram lo scenario è un errore, senza richieste; con dati che non lo provano
// non gira affatto (non provato).
func TestScenarioGoTelegramSenzaIstanze(t *testing.T) {
	var n atomic.Int64
	s := contaRichieste(&n)
	defer s.Close()
	i := istanza{"x", s.URL, s.Client()}
	rap := eseguiScenariTutti([]scenario{scenarioTelegram}, nil, i, i, istanzeTelegram{}, credenziali{}, datiSinteticiImport)
	if len(rap.Errori) != 1 || !strings.Contains(rap.Errori[0], "go-telegram") || rap.Verdi["tg"] || rap.ok() {
		t.Errorf("rapporto = %+v", rap)
	}
	rap = eseguiScenariTutti([]scenario{scenarioTelegram}, nil, i, i, istanzeTelegram{Go: i, Bot: i}, credenziali{}, "")
	if len(rap.Errori) != 0 || rap.Passi != 0 || len(rap.NonProvati) != 1 || rap.NonProvati[0] != "tg" {
		t.Errorf("rapporto non provato = %+v", rap)
	}
	if n.Load() != 0 {
		t.Errorf("%d richieste", n.Load())
	}
	// Uno scenario normale con le istanze go-telegram non le usa.
	var nTg atomic.Int64
	tgs := contaRichieste(&nTg)
	defer tgs.Close()
	j := istanza{"tg", tgs.URL, tgs.Client()}
	normale := scenario{Nome: "n", Passi: []passo{{Metodo: "GET", Percorso: "/", Confronta: []string{"codice"}},
		{Metodo: "GET", Percorso: "/", SoloGo: "D-03", Atteso: map[string]string{"codice": "200"}}}}
	if rap := eseguiScenariTutti([]scenario{normale}, nil, i, i, istanzeTelegram{Go: j, Bot: j}, credenziali{}, ""); !rap.ok() || nTg.Load() != 0 || n.Load() != 3 {
		t.Errorf("scenario normale: %+v, %d richieste alle istanze go-telegram", rap, nTg.Load())
	}
}

// {start} viene solo da un redirect a t.me con un token pulito; {campo:…} da un campo che c'è.
func TestSegnapostoTelegramRifiutati(t *testing.T) {
	g := &goTelegramFinto{}
	gtg := httptest.NewServer(g)
	defer gtg.Close()
	bot := botDiProva(t)
	tg := istanzeTelegram{Go: istanza{"go-telegram", gtg.URL, gtg.Client()}, Bot: istanza{"bot", bot.URL, bot.Client()}}
	start := passo{Metodo: "POST", Percorso: "/_prova/aggiornamenti", Corpo: `{"comando":"start","token":"{start}"}`, Bot: true, SoloGo: "D-03",
		Atteso: map[string]string{"codice": "200"}}
	for _, prima := range []string{"/attiva-altrove", "/attiva-http", "/attiva-sbagliato", "/pagina"} {
		sc := scenario{Nome: "s", Istanza: istanzaGoTelegram, ProvaCon: []string{datiSinteticiImport}, Passi: []passo{
			{Metodo: "POST", Percorso: prima, SoloGo: "D-03", Atteso: map[string]string{"codice": "302"}}, start}}
		rap := eseguiScenariTutti([]scenario{sc}, nil, tg.Go, tg.Go, tg, credenziali{}, datiSinteticiImport)
		if len(rap.Errori) != 1 || !strings.Contains(rap.Errori[0], "{start}") {
			t.Errorf("%s: %+v", prima, rap)
		}
	}
	if _, b := chiamaBot(t, bot, "GET", "/_prova/mandati", ""); campo(t, b, "in_attesa") != "0" {
		t.Errorf("un {start} rifiutato è arrivato al bot: %s", b)
	}
	sc := scenario{Nome: "c", Istanza: istanzaGoTelegram, ProvaCon: []string{datiSinteticiImport}, Passi: []passo{
		{Metodo: "GET", Percorso: "/pagina", SoloGo: "D-03", Atteso: map[string]string{"codice": "200"}},
		{Metodo: "POST", Percorso: "/invia", Corpo: "x={campo:altro}", SoloGo: "D-03", Atteso: map[string]string{"codice": "200"}}}}
	if rap := eseguiScenariTutti([]scenario{sc}, nil, tg.Go, tg.Go, tg, credenziali{}, datiSinteticiImport); len(rap.Errori) != 1 || !strings.Contains(rap.Errori[0], "{campo:altro}") {
		t.Errorf("campo che manca: %+v", rap)
	}
}

// Un passo del bot con attendi rifà la GET finché le attese tornano, entro il limite; scaduto il limite
// la differenza resta.
func TestPassoBotAttende(t *testing.T) {
	vecchio := intervalloAttendi
	intervalloAttendi = 20 * time.Millisecond
	defer func() { intervalloAttendi = vecchio }()
	bot := botDiProva(t)
	tg := istanzeTelegram{Go: istanza{"go-telegram", bot.URL, bot.Client()}, Bot: istanza{"bot", bot.URL, bot.Client()}}
	aspetta := func(totale string, secondi int) rapporto {
		sc := scenario{Nome: "a", Istanza: istanzaGoTelegram, ProvaCon: []string{datiSinteticiImport}, Passi: []passo{
			{Metodo: "GET", Percorso: "/_prova/mandati?metodo=sendMessage", Bot: true, SoloGo: "D-03", Attendi: secondi,
				Atteso: map[string]string{"json:totale": totale}}}}
		return eseguiScenariTutti([]scenario{sc}, nil, tg.Go, tg.Go, tg, credenziali{}, datiSinteticiImport)
	}
	// Il messaggio arriva da un'altra goroutine: niente t.Fatal lì, l'esito torna su un canale.
	mandato := make(chan error, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		res, err := bot.Client().Post(bot.URL+apiBanco+"/sendMessage", "application/json", strings.NewReader(`{"chat_id":1,"text":"x"}`))
		if err == nil {
			_ = res.Body.Close() // basta il codice
			if res.StatusCode != 200 {
				err = fmt.Errorf("sendMessage = %d", res.StatusCode)
			}
		}
		mandato <- err
	}()
	// I limiti alti sono larghi (macchina carica); quelli bassi dicono che l'attesa c'è stata davvero.
	inizio := time.Now()
	if rap := aspetta("1", 20); !rap.ok() || time.Since(inizio) < 250*time.Millisecond || time.Since(inizio) > 15*time.Second {
		t.Errorf("attesa riuscita: %+v in %v", rap, time.Since(inizio))
	}
	select {
	case err := <-mandato:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("sendMessage di prova non tornato")
	}
	inizio = time.Now()
	if rap := aspetta("2", 1); rap.Inattese != 1 || time.Since(inizio) < time.Second || time.Since(inizio) > 6*time.Second {
		t.Errorf("attesa scaduta: %+v in %v", rap, time.Since(inizio))
	}
	// Senza attendi una sola richiesta.
	inizio = time.Now()
	if rap := aspetta("2", 0); rap.Inattese != 1 || time.Since(inizio) > 3*time.Second {
		t.Errorf("senza attesa: %+v in %v", rap, time.Since(inizio))
	}
}

func TestLeggiScenarioGoTelegram(t *testing.T) {
	ok := `{"nome":"t","istanza":"go-telegram","prova_con":["sintetici-import"],"passi":[
		{"metodo":"POST","percorso":"/r/x","corpo":"a={campo:notifica}","solo_go":"D-03","atteso":{"codice":"302"}},
		{"metodo":"POST","percorso":"/_prova/aggiornamenti","corpo":"{\"comando\":\"start\",\"token\":\"{start}\"}","bot":true,"solo_go":"D-03","atteso":{"codice":"200"}},
		{"metodo":"GET","percorso":"/_prova/mandati","bot":true,"attendi":60,"solo_go":"D-03","atteso":{"json:ultimo.reply_markup.inline_keyboard.0.0.url":"x"}}]}`
	if s, err := leggiScenario("ok.json", strings.NewReader(ok)); err != nil || s.Istanza != istanzaGoTelegram || !s.Passi[1].Bot || s.Passi[2].Attendi != 60 {
		t.Fatalf("scenario valido: %+v %v", s, err)
	}
	sbagliati := map[string]string{
		"istanza sconosciuta":            strings.Replace(ok, `"istanza":"go-telegram"`, `"istanza":"altro"`, 1),
		"go-telegram senza prova_con":    strings.Replace(ok, `"prova_con":["sintetici-import"],`, ``, 1),
		"go-telegram con passo comune":   strings.Replace(ok, `"corpo":"a={campo:notifica}","solo_go":"D-03"`, `"corpo":"a=1","confronta":["codice"]`, 1),
		"bot senza go-telegram":          strings.Replace(ok, `"istanza":"go-telegram",`, ``, 1),
		"bot senza solo_go":              strings.Replace(ok, `"bot":true,"solo_go":"D-03"`, `"bot":true`, 1),
		"bot fuori da /_prova/":          strings.Replace(ok, `"percorso":"/_prova/aggiornamenti"`, `"percorso":"/bot1:x/getUpdates"`, 1),
		"attendi su un POST":             strings.Replace(ok, `"bot":true,"solo_go":"D-03","atteso":{"codice":"200"}`, `"bot":true,"attendi":5,"solo_go":"D-03","atteso":{"codice":"200"}`, 1),
		"attendi troppo lungo":           strings.Replace(ok, `"attendi":60`, `"attendi":121`, 1),
		"attendi negativo":               strings.Replace(ok, `"attendi":60`, `"attendi":-1`, 1),
		"attendi senza bot":              strings.Replace(ok, `"corpo":"a={campo:notifica}","solo_go":"D-03"`, `"corpo":"a=1","attendi":3,"solo_go":"D-03"`, 1),
		"{start} fuori dal bot":          strings.Replace(ok, `a={campo:notifica}`, `a={start}`, 1),
		"{campo:…} nel bot":              strings.Replace(ok, `\"token\":\"{start}\"`, `\"token\":\"{campo:x}\"`, 1),
		"{campo:…} nel percorso del bot": strings.Replace(ok, `"percorso":"/_prova/mandati"`, `"percorso":"/_prova/mandati?{location}"`, 1),
	}
	for nome, j := range sbagliati {
		if _, err := leggiScenario(nome+".json", strings.NewReader(j)); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
	comune := `{"nome":"c","passi":[{"metodo":"POST","percorso":"/x","corpo":"a={campo:notifica}","confronta":["codice"]}]}`
	if _, err := leggiScenario("c.json", strings.NewReader(comune)); err == nil {
		t.Error("{campo:…} in un passo comune accettato")
	}
}

func TestValoreJSON(t *testing.T) {
	corpo := `{"a":{"b":[{"c":"x<y"},{"c":12.50}],"t":true,"n":null,"o":{"z":1,"y":"<"}},"s":""}`
	for p, v := range map[string]string{
		"a.b.0.c": "x<y", "a.b.1.c": "12.50", "a.t": "true", "a.n": "null", "a.o": `{"y":"<","z":1}`, "s": "",
		"a.b.2.c": assenteVisibile, "a.x": assenteVisibile, "a.b.c": assenteVisibile, "s.x": assenteVisibile, "a.b": `[{"c":"x<y"},{"c":12.50}]`,
	} {
		if g := (risposta{Corpo: corpo}).valore("json:" + p); g != v {
			t.Errorf("%s = %q, voluto %q", p, g, v)
		}
	}
	for _, c := range []string{"non json", `{"a":1} {"b":2}`, ""} {
		if g := (risposta{Corpo: c}).valore("json:a"); g != nonJSON {
			t.Errorf("%q = %q", c, g)
		}
	}
	for c, v := range map[string]bool{"json:a": true, "json:a.b.0.c": true, "json:in_attesa": true, "json:": false, "json:a..b": false,
		"json:.a": false, "json:a.": false, "json:a b": false, "json:0": false} {
		if campoValido(c) != v {
			t.Errorf("campoValido(%q) = %v", c, !v)
		}
	}
}

// Lo scenario del repo gira sul Go con Telegram, solo con i dati importati, con la D-03, segue il flusso
// intero (pulsante, /start, «Sì», cambio di stato, «Invia», coda, risposta del negozio con l'avviso, risposta
// del cliente con «Rispondi» e l'avviso al negozio della slice 075, /stop) e aspetta il bot con un limite;
// il link del pulsante del messaggio è quello della busta con il token del banco sull'indirizzo pubblico
// del Go con Telegram.
func TestScenarioTelegramAccesoNelRepo(t *testing.T) {
	ss, err := leggiScenari("../../bench/scenari")
	if err != nil {
		t.Fatal(err)
	}
	var s *scenario
	for i := range ss {
		if ss[i].Nome == "telegram-acceso" {
			s = &ss[i]
		}
	}
	if s == nil {
		t.Fatal("manca bench/scenari/telegram-acceso.json")
	}
	if s.Istanza != istanzaGoTelegram || len(s.ProvaCon) != 1 || s.ProvaCon[0] != datiSinteticiImport {
		t.Errorf("istanza %q, prova_con %v", s.Istanza, s.ProvaCon)
	}
	token := ""
	for _, x := range tokenBanco {
		if x.numero == "23/26" {
			token = x.token
		}
	}
	var comandi, totali []string
	var attese, urlPulsante, riquadroB, invia int
	dopoSi := false
	for i, p := range s.Passi {
		if p.Bot && p.Metodo == "GET" && strings.Contains(p.Percorso, "metodo=sendMessage") {
			if p.Atteso["json:in_attesa"] != "0" {
				t.Errorf("passo %d: senza in_attesa 0 (il Go non ha finito l'aggiornamento)", i)
			}
			totali = append(totali, p.Atteso["json:totale"])
		}
		if p.Bot && p.Corpo == `{"comando":"si"}` {
			dopoSi = true
		}
		// Dopo «Sì», prima del login: il riquadro B sulla pagina del cliente.
		if dopoSi && riquadroB == 0 && p.Metodo == "GET" && strings.HasPrefix(p.Percorso, "/r/") {
			if p.Atteso["testo:Notifiche Telegram attive"] != "presente" || p.Atteso["testo:Vuoi essere avvisato in tempo reale?"] != "assente" {
				t.Errorf("passo %d: la pagina dopo «Sì» non vuole il riquadro B: %v", i, p.Atteso)
			}
			riquadroB++
		}
		if p.Metodo == "POST" && strings.HasSuffix(p.Percorso, "/notifica-telegram") {
			if !strings.Contains(p.Corpo, "notifica={campo:notifica}") || !strings.Contains(p.Corpo, "azione=invia") || p.Atteso["codice"] != "302" {
				t.Errorf("passo %d: «Invia» del popup senza il gettone: %q", i, p.Corpo)
			}
			invia++
		}
		if p.SoloGo != "D-03" {
			t.Errorf("passo %d: solo_go %q", i, p.SoloGo)
		}
		if p.Bot && p.Metodo == "POST" {
			comandi = append(comandi, p.Corpo)
		}
		if p.Attendi > 0 {
			attese++
		}
		if v, ok := p.Atteso["json:ultimo.reply_markup.inline_keyboard.0.0.url"]; ok {
			urlPulsante++
			if v != indirizzoGoTelegram+"/r/"+token {
				t.Errorf("passo %d: pulsante %q", i, v)
			}
		}
		if strings.HasPrefix(p.Percorso, "/r/") && !strings.HasPrefix(p.Percorso, "/r/"+token) {
			t.Errorf("passo %d: un'altra busta %s", i, p.Percorso)
		}
	}
	if strings.Join(comandi, " ") != `{"comando":"start","token":"{start}"} {"comando":"si"} {"comando":"rispondi","testo":"Risposta sintetica del cliente"} {"comando":"stop"}` {
		t.Errorf("comandi del bot = %v", comandi)
	}
	if attese < 3 || urlPulsante != 2 || riquadroB != 1 || invia != 1 {
		t.Errorf("attese %d, pulsanti %d, riquadri B %d, «Invia» %d", attese, urlPulsante, riquadroB, invia)
	}
	// Un messaggio in più a ogni passo del bot: alla chat del cliente benvenuto, consenso, avviso della coda,
	// avviso del nuovo messaggio, conferma della risposta; alla chat del negozio l'avviso del messaggio del
	// cliente; di nuovo al cliente /stop. Niente di più.
	if strings.Join(totali, " ") != "1 2 3 4 5 1 6" {
		t.Errorf("totali dei sendMessage = %v", totali)
	}
}
