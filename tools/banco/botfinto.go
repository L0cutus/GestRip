package main

// Il Bot API finto del banco con il canale Telegram acceso (slice 074b, piano della ricevuta 074).
// Gira come `banco bot-finto -ascolta :8081` in un container della rete interna dell'istanza, con
// l'alias bot.invalid: il Go con Telegram (gotelegram.go) ha TELEGRAM_API_ENDPOINT puntato qui. Nessuna
// rete esterna: la rete del banco è internal.
//
// Fa la parte di Telegram che il Go usa, con la forma del Bot API (POST JSON su /bot<token>/<metodo>,
// risposte {"ok": …}):
//   - getMe: il nome del bot sintetico;
//   - sendMessage e answerCallbackQuery: li registra («mandati»), solo il corpo, mai il token;
//   - getUpdates: gli aggiornamenti in coda, come Telegram: restano finché una richiesta non porta un
//     offset dopo di loro (conferma); nessuna lettura lunga, la lista vuota torna subito (il lettore
//     del Go aspetta 1 s e riprova).
//
// Il token del percorso deve essere quello che banco su semina (tokenBotBanco): un altro è 401, come
// Telegram, e nessuna risposta lo ripete.
//
// Le rotte di prova le usa il passo «bot» degli scenari (esegui.go):
//   - POST /_prova/aggiornamenti {"comando": "start", "token": …} | {"comando": "si"|"no"} |
//     {"comando": "stop"} | {"comando": "rispondi", "testo": …}: mette in coda il messaggio del cliente
//     sintetico (/start <token>, /stop), il pulsante del consenso dell'ultimo benvenuto mandato
//     (cli:tx:1:<id> o cli:tx:0:<id>) o (slice 075) il testo che risponde con «Rispondi» all'ultima
//     notifica mandata alla sua chat (reply_to_message con il suo message_id);
//   - GET /_prova/mandati[?metodo=…][&chat=…][&indice=N]: {"in_attesa": aggiornamenti non ancora
//     confermati, "totale": mandati (del metodo, alla chat: slice 075), "ultimo": il corpo dell'ultimo (o
//     dell'N-esimo da 1), o null};
//   - GET /_prova/salute: «ok», per banco attendi.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// botFinto tiene in memoria la coda degli aggiornamenti e i mandati.
type botFinto struct {
	mu        sync.Mutex
	prossimo  int64           // update_id del prossimo aggiornamento
	coda      []aggiornamento // non ancora confermati, in ordine
	mandati   []mandato
	messaggio int64 // ultimo message_id dato
}

type aggiornamento struct {
	ID    int64
	Campo string         // "message" o "callback_query"
	Dati  map[string]any // il messaggio o il pulsante
}

type mandato struct {
	Metodo string
	Corpo  map[string]any
	ID     int64 // il message_id che sendMessage ha dato (0 per gli altri metodi)
}

// primoAggiornamento è il primo update_id: lontano da 0, come quelli di Telegram.
const primoAggiornamento = 700000001

func nuovoBotFinto() *botFinto { return &botFinto{prossimo: primoAggiornamento} }

// reTokenStart è il token di /start che il bot accetta: quello di t.me (base64url) entro i 64 byte del
// Bot API; niente che rompa il testo del messaggio.
var reTokenStart = regexp.MustCompile(`\A[A-Za-z0-9_-]{1,64}\z`)

// massimoCorpo è quanto si legge di una richiesta (il Go manda pochi KiB).
const massimoCorpo = 1 << 20

func (b *botFinto) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/_prova/salute":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "ok") // la risposta della salute: un errore di scrittura non si può dire a nessuno
	case r.Method == http.MethodPost && r.URL.Path == "/_prova/aggiornamenti":
		b.aggiungi(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/_prova/mandati":
		b.leggiMandati(w, r)
	default:
		b.api(w, r)
	}
}

// rispondiJSON scrive v come JSON con il codice dato.
func rispondiJSON(w http.ResponseWriter, codice int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(codice)
	_ = json.NewEncoder(w).Encode(v) // la risposta è già partita: un errore qui non si può dire
}

// rifiutoAPI è un errore del Bot API: {"ok": false, "error_code": …, "description": …}.
func rifiutoAPI(w http.ResponseWriter, codice int, descrizione string) {
	rispondiJSON(w, codice, map[string]any{"ok": false, "error_code": codice, "description": descrizione})
}

// api è la parte del Bot API: /bot<token>/<metodo>.
func (b *botFinto) api(w http.ResponseWriter, r *http.Request) {
	token, metodo, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if !ok || token != "bot"+tokenBotBanco || strings.Contains(metodo, "/") {
		rifiutoAPI(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	corpo := map[string]any{}
	if r.Body != nil {
		letto, err := io.ReadAll(io.LimitReader(r.Body, massimoCorpo+1))
		if err != nil || len(letto) > massimoCorpo {
			rifiutoAPI(w, http.StatusBadRequest, "Bad Request: corpo non letto")
			return
		}
		if len(letto) > 0 {
			dec := json.NewDecoder(strings.NewReader(string(letto)))
			dec.UseNumber()
			if err := dec.Decode(&corpo); err != nil {
				rifiutoAPI(w, http.StatusBadRequest, "Bad Request: corpo non JSON")
				return
			}
		}
	}
	switch metodo {
	case "getMe":
		rispondiJSON(w, http.StatusOK, map[string]any{"ok": true, "result": map[string]any{
			"id": 4242, "is_bot": true, "first_name": "Banco", "username": nomeBotBanco}})
	case "sendMessage":
		testo, _ := corpo["text"].(string)
		if corpo["chat_id"] == nil || testo == "" {
			rifiutoAPI(w, http.StatusBadRequest, "Bad Request: message text is empty")
			return
		}
		b.mu.Lock()
		b.messaggio++
		id := b.messaggio
		b.mandati = append(b.mandati, mandato{Metodo: metodo, Corpo: corpo, ID: id})
		b.mu.Unlock()
		rispondiJSON(w, http.StatusOK, map[string]any{"ok": true, "result": map[string]any{
			"message_id": id, "date": time.Now().Unix(), "chat": map[string]any{"id": corpo["chat_id"]}, "text": testo}})
	case "answerCallbackQuery":
		b.mu.Lock()
		b.mandati = append(b.mandati, mandato{Metodo: metodo, Corpo: corpo})
		b.mu.Unlock()
		rispondiJSON(w, http.StatusOK, map[string]any{"ok": true, "result": true})
	case "getUpdates":
		b.aggiornamenti(w, corpo)
	default:
		rifiutoAPI(w, http.StatusNotFound, "Not Found")
	}
}

// aggiornamenti è getUpdates: un offset conferma (e toglie) gli aggiornamenti prima di lui; restano
// tutti gli altri.
func (b *botFinto) aggiornamenti(w http.ResponseWriter, corpo map[string]any) {
	b.mu.Lock()
	if n, ok := corpo["offset"].(json.Number); ok {
		if offset, err := n.Int64(); err == nil {
			resto := b.coda[:0]
			for _, a := range b.coda {
				if a.ID >= offset {
					resto = append(resto, a)
				}
			}
			b.coda = resto
		}
	}
	risultato := make([]map[string]any, 0, len(b.coda))
	for _, a := range b.coda {
		risultato = append(risultato, map[string]any{"update_id": a.ID, a.Campo: a.Dati})
	}
	b.mu.Unlock()
	rispondiJSON(w, http.StatusOK, map[string]any{"ok": true, "result": risultato})
}

// Il cliente sintetico: una chat privata con lo stesso id dell'utente, come in Telegram.
func utenteCliente() map[string]any {
	return map[string]any{"id": chatClienteBanco, "is_bot": false, "first_name": "Cliente"}
}

func chatCliente() map[string]any {
	return map[string]any{"id": chatClienteBanco, "type": "private", "first_name": "Cliente"}
}

// aggiungi è POST /_prova/aggiornamenti.
func (b *botFinto) aggiungi(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Comando string  `json:"comando"`
		Token   string  `json:"token"`
		Testo   *string `json:"testo"`
	}
	// Il corpo al più 64 KiB: c'entra il testo più lungo di «rispondi» (4096 caratteri, anche di 4 byte).
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		http.Error(w, "corpo non valido", http.StatusBadRequest)
		return
	}
	if (c.Testo != nil) != (c.Comando == "rispondi") || c.Testo != nil && (*c.Testo == "" || utf8.RuneCountInString(*c.Testo) > massimoTestoCliente) {
		http.Error(w, "testo solo con rispondi, da 1 a 4096 caratteri", http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var a aggiornamento
	switch c.Comando {
	case "rispondi":
		notifica, ok := b.ultimaNotifica()
		if !ok {
			http.Error(w, "nessuna notifica mandata alla chat del cliente", http.StatusConflict)
			return
		}
		a = b.messaggioDelCliente(*c.Testo)
		a.Dati["reply_to_message"] = map[string]any{"message_id": notifica, "date": time.Now().Unix(), "chat": chatCliente(),
			"from": map[string]any{"id": 4242, "is_bot": true, "first_name": "Banco", "username": nomeBotBanco}, "text": "Notifica"}
	case "start":
		if !reTokenStart.MatchString(c.Token) {
			http.Error(w, "token di /start non valido", http.StatusBadRequest)
			return
		}
		a = b.messaggioDelCliente("/start " + c.Token)
	case "stop":
		a = b.messaggioDelCliente("/stop")
	case "si", "no":
		valore := "0"
		if c.Comando == "si" {
			valore = "1"
		}
		dati, mid, ok := b.pulsanteDelConsenso(valore)
		if !ok {
			http.Error(w, "nessun pulsante del consenso fra i messaggi mandati", http.StatusConflict)
			return
		}
		a = aggiornamento{Campo: "callback_query", Dati: map[string]any{
			"id": "banco-cb-" + strconv.FormatInt(b.prossimo, 10), "from": utenteCliente(), "chat_instance": "banco",
			"message": map[string]any{"message_id": mid, "date": time.Now().Unix(), "chat": chatCliente()}, "data": dati}}
	default:
		http.Error(w, "comando sconosciuto", http.StatusBadRequest)
		return
	}
	a.ID = b.prossimo
	b.prossimo++
	b.coda = append(b.coda, a)
	rispondiJSON(w, http.StatusOK, map[string]any{"update_id": a.ID, "in_attesa": len(b.coda)})
}

// messaggioDelCliente è un messaggio di testo del cliente sintetico; da chiamare con il lucchetto.
func (b *botFinto) messaggioDelCliente(testo string) aggiornamento {
	b.messaggio++
	return aggiornamento{Campo: "message", Dati: map[string]any{"message_id": b.messaggio, "date": time.Now().Unix(),
		"from": utenteCliente(), "chat": chatCliente(), "text": testo}}
}

// massimoTestoCliente è quanto può essere lungo il testo di «rispondi»: il massimo di un messaggio di
// Telegram, 4096 caratteri.
const massimoTestoCliente = 4096

// ultimaNotifica è il message_id dell'ultimo sendMessage alla chat del cliente sintetico con un pulsante
// con un link (le notifiche della coda: «📂 Apri scheda riparazione»); da chiamare con il lucchetto.
func (b *botFinto) ultimaNotifica() (int64, bool) {
	for i := len(b.mandati) - 1; i >= 0; i-- {
		m := b.mandati[i]
		if m.Metodo != "sendMessage" || fmt.Sprint(m.Corpo["chat_id"]) != strconv.FormatInt(chatClienteBanco, 10) {
			continue
		}
		tastiera, _ := m.Corpo["reply_markup"].(map[string]any)
		righe, _ := tastiera["inline_keyboard"].([]any)
		for _, riga := range righe {
			pulsanti, _ := riga.([]any)
			for _, p := range pulsanti {
				if pp, _ := p.(map[string]any); pp["url"] != nil {
					return m.ID, true
				}
			}
		}
	}
	return 0, false
}

// pulsanteDelConsenso cerca, dall'ultimo mandato indietro, un sendMessage con un pulsante
// cli:tx:<valore>:<id> e ne dà il callback_data e il message_id del messaggio con il pulsante, come
// Telegram nel callback_query; da chiamare con il lucchetto.
func (b *botFinto) pulsanteDelConsenso(valore string) (string, int64, bool) {
	for i := len(b.mandati) - 1; i >= 0; i-- {
		m := b.mandati[i]
		if m.Metodo != "sendMessage" {
			continue
		}
		tastiera, _ := m.Corpo["reply_markup"].(map[string]any)
		righe, _ := tastiera["inline_keyboard"].([]any)
		for _, riga := range righe {
			pulsanti, _ := riga.([]any)
			for _, p := range pulsanti {
				pp, _ := p.(map[string]any)
				if dati, _ := pp["callback_data"].(string); strings.HasPrefix(dati, "cli:tx:"+valore+":") {
					return dati, m.ID, true
				}
			}
		}
	}
	return "", 0, false
}

// leggiMandati è GET /_prova/mandati.
func (b *botFinto) leggiMandati(w http.ResponseWriter, r *http.Request) {
	metodo, chat := r.URL.Query().Get("metodo"), r.URL.Query().Get("chat")
	b.mu.Lock()
	defer b.mu.Unlock()
	var scelti []mandato
	for _, m := range b.mandati {
		if (metodo == "" || m.Metodo == metodo) && (chat == "" || fmt.Sprint(m.Corpo["chat_id"]) == chat) {
			scelti = append(scelti, m)
		}
	}
	var ultimo any
	if len(scelti) > 0 {
		ultimo = scelti[len(scelti)-1].Corpo
	}
	if s := r.URL.Query().Get("indice"); s != "" {
		i, err := strconv.Atoi(s)
		if err != nil || i < 1 || i > len(scelti) {
			http.Error(w, "indice fuori dai mandati", http.StatusBadRequest)
			return
		}
		ultimo = scelti[i-1].Corpo
	}
	rispondiJSON(w, http.StatusOK, map[string]any{"in_attesa": len(b.coda), "totale": len(scelti), "ultimo": ultimo})
}

// cmdBotFinto è `banco bot-finto -ascolta IND`: resta in piedi fino a SIGTERM o SIGINT (lo stop del
// container).
func cmdBotFinto(args []string) error {
	fs := flag.NewFlagSet("bot-finto", flag.ContinueOnError)
	ascolta := fs.String("ascolta", ":"+portaBotFinto, "indirizzo su cui ascoltare")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("argomenti in più non riconosciuti: %v", fs.Args())
	}
	ctx, ferma := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer ferma()
	err := serviBotFinto(ctx, *ascolta, func(a string) { fmt.Println("bot finto in ascolto su", a) })
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// serviBotFinto ascolta su ascolta finché ctx non finisce; pronto riceve l'indirizzo vero.
func serviBotFinto(ctx context.Context, ascolta string, pronto func(string)) error {
	l, err := net.Listen("tcp", ascolta)
	if err != nil {
		return err
	}
	s := &http.Server{Handler: nuovoBotFinto(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute}
	go func() {
		<-ctx.Done()
		fine, annulla := context.WithTimeout(context.Background(), 5*time.Second)
		defer annulla()
		_ = s.Shutdown(fine) // all'arresto conta solo che il server si fermi
	}()
	pronto(l.Addr().String())
	return s.Serve(l)
}
