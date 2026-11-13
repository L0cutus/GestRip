package uscita

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/L0cutus/gestrip/internal/db"
)

// Il Bot API finto dei test della 071: un server HTTP in processo (httptest, 127.0.0.1, porta casuale) che
// annota ogni richiesta e risponde quello che il test dice. Come il «bot_api» di
// telegram_queue_limits_test.exs dello standalone: nessuna richiesta esce mai verso Telegram. Token e chat
// sono inventati.

const (
	tokenFinto = "123456:AAH-token-finto-di-prova"
	chatFinta  = "-1009999999999"
)

// richiestaBot è una richiesta arrivata al Bot API finto: metodo HTTP, percorso e corpo JSON.
type richiestaBot struct {
	Metodo, Percorso string
	Corpo            map[string]any
	Grezzo           string
}

type botFinto struct {
	srv       *httptest.Server
	mu        sync.Mutex
	richieste []richiestaBot
	// rispondi dà stato e corpo della risposta; nil: sendMessage accettato (message_id 77) e getMe con
	// il nome «negozio_prova_bot».
	rispondi func(r richiestaBot) (int, string)
}

func nuovoBotFinto(t *testing.T) *botFinto {
	t.Helper()
	b := &botFinto{}
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corpo, _ := io.ReadAll(r.Body)
		rq := richiestaBot{Metodo: r.Method, Percorso: r.URL.Path, Grezzo: string(corpo)}
		if len(corpo) > 0 {
			_ = json.Unmarshal(corpo, &rq.Corpo)
		}
		b.mu.Lock()
		b.richieste = append(b.richieste, rq)
		f := b.rispondi
		b.mu.Unlock()
		stato, risposta := 200, `{"ok":true,"result":{"message_id":77}}`
		if strings.HasSuffix(rq.Percorso, "/getMe") {
			risposta = `{"ok":true,"result":{"id":123456,"is_bot":true,"username":"negozio_prova_bot"}}`
		}
		if f != nil {
			stato, risposta = f(rq)
		}
		w.Header().Set("Content-Type", "application/json")
		if stato >= 300 && stato < 400 {
			// Un redirect vero: chi lo seguisse ripeterebbe la richiesta altrove.
			w.Header().Set("Location", "/altrove")
		}
		w.WriteHeader(stato)
		_, _ = io.WriteString(w, risposta)
	}))
	t.Cleanup(b.srv.Close)
	return b
}

func (b *botFinto) tutte() []richiestaBot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]richiestaBot(nil), b.richieste...)
}

func (b *botFinto) quante() int { return len(b.tutte()) }

// risposta imposta la risposta di ogni richiesta da qui in poi.
func (b *botFinto) risposta(f func(r richiestaBot) (int, string)) {
	b.mu.Lock()
	b.rispondi = f
	b.mu.Unlock()
}

// rifiutoBot è la risposta di un rifiuto del Bot API, come refusal/2 del test dello standalone.
func rifiutoBot(stato int, descrizione string) func(richiestaBot) (int, string) {
	return func(richiestaBot) (int, string) {
		j, _ := json.Marshal(map[string]any{"ok": false, "error_code": stato, "description": descrizione})
		return stato, string(j)
	}
}

// impostaTelegram scrive la configurazione del bot come la lascia l'import o la pagina: il token in
// segreti_provider, la chat in impostazioni; acceso è l'interruttore della D-03.
func impostaTelegram(t *testing.T, d *db.DB, token, chat string, acceso bool) {
	t.Helper()
	ora := "2026-01-15T08:05:00.000000Z"
	if token != "" {
		if _, err := d.Scrittura.Exec(`INSERT INTO segreti_provider (nome, valore, aggiornato_il) VALUES ('outgoing.telegram.bot_token', ?, ?)
			ON CONFLICT (nome) DO UPDATE SET valore = excluded.valore`, token, ora); err != nil {
			t.Fatal(err)
		}
	}
	valori := map[string]string{"outgoing.telegram.chat_id": chat, ChiaveTelegramAcceso: "0"}
	if acceso {
		valori[ChiaveTelegramAcceso] = "1"
	}
	for k, v := range valori {
		if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, ?)
			ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore`, k, v, ora); err != nil {
			t.Fatal(err)
		}
	}
}

// impostazione scrive una riga di impostazioni.
func impostazione(t *testing.T, d *db.DB, chiave, valore string) {
	t.Helper()
	if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, '2026-01-15T08:05:00.000000Z')
		ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore`, chiave, valore); err != nil {
		t.Fatal(err)
	}
}

// codaTelegram è la coda con il provider Telegram vero puntato al Bot API finto, e la configurazione
// accesa (token e chat finti).
func codaTelegram(t *testing.T) (*Coda, *Telegram, *botFinto) {
	t.Helper()
	d, o := apriDB(t)
	b := nuovoBotFinto(t)
	p := NuovoTelegram(d, b.srv.URL)
	impostaTelegram(t, d, tokenFinto, chatFinta, true)
	return Nuova(d, o, map[string]Provider{CanaleTelegram: p}), p, b
}

// mandaA consegna m con p e dà riferimento e motivo del rifiuto ("" se accettato).
func mandaA(t *testing.T, p *Telegram, m Messaggio) (string, string) {
	t.Helper()
	rif, err := p.Consegna(context.Background(), m)
	if err == nil {
		return rif, ""
	}
	r, ok := err.(Rifiuto)
	if !ok {
		t.Fatalf("errore che non è un Rifiuto: %T", err)
	}
	return rif, string(r)
}
