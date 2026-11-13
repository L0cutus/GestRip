package uscita

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/db"
)

// Telegram è il provider Telegram della coda (slice 071, CLI-51 parte Telegram, ADM-10), come
// Outgoing.Providers.Telegram dello standalone a go-baseline: il bot del negozio, un sendMessage per riga
// della coda, e getMe per la prova della pagina. Solo la libreria standard (net/http, encoding/json).
//
// Come lo standalone:
//   - «ok: true» vuol dire che il bot ha preso il messaggio, e il message_id è il riferimento; mai
//     «consegnato»;
//   - il token viaggia nel percorso dell'indirizzo, l'unico posto dove il Bot API lo prende: non finisce
//     mai in un errore, nel registro o nella pagina. Un errore del trasporto di net/http porta
//     l'indirizzo, quindi non si usa mai: si salva solo un genere fisso («telegram:transport::timeout»,
//     come l'SMTP della 069), e ogni testo che torna passa dalla pulizia del token;
//   - un rifiuto è «telegram:http:<stato>:<descrizione>», con le parole di Telegram dopo lo stato (la coda
//     le legge: il 429 «retry after N», il 403 di un cliente che ha bloccato il bot, il 400 di un HTML che
//     Telegram non legge), o «telegram:http:<stato>» senza descrizione;
//   - parse_mode e reply_markup partono solo se la riga li porta: le righe di prima vanno come testo
//     semplice;
//   - l'indirizzo del Bot API è quello di Telegram, a meno che l'ambiente non lo sposti
//     (TELEGRAM_API_ENDPOINT, EndpointDaAmbiente): nessuna pagina e nessuna impostazione lo cambiano,
//     perché chi sposta il bot riceve il suo token;
//   - un redirect non si segue (hackney non li segue): è un rifiuto con il suo stato.
//
// Tempi: 15 secondi per una richiesta (@timeout), tutta compresa (collegamento, TLS, risposta); il
// contesto annullato (serve che si ferma) la chiude subito. Una risposta più grande di 1 MiB non si legge
// tutta (solo Go: il Bot API risponde poche centinaia di byte).
type Telegram struct {
	d    *db.DB
	base string
	// tempo è il tempo massimo di una richiesta (@timeout, 15 s); solo i test lo accorciano.
	tempo time.Duration
}

// EndpointTelegram è l'indirizzo del Bot API di Telegram (@endpoint).
const EndpointTelegram = "https://api.telegram.org"

// massimoRisposta è quanto si legge al più di una risposta del Bot API (solo Go).
const massimoRisposta = 1 << 20

// erroreClientiSpenti è @customer_channel_off: la riga di un cliente con «Notifiche Telegram ai clienti»
// spente (slice 137 dello standalone). La coda la fa fallire subito.
const erroreClientiSpenti = "telegram:notifiche ai clienti spente"

// EndpointDaAmbiente è il blocco di config/runtime.exs per TELEGRAM_API_ENDPOINT: c dice se la variabile
// c'è. Assente, il Bot API di Telegram; presente (anche vuota, come System.get_env), deve essere un
// indirizzo http o https con un host, e torna senza la «/» finale (endpoint/0). Solo Go: niente utente,
// query o frammento, che finirebbero in mezzo all'indirizzo con il token. L'errore nomina la variabile e
// mai il valore.
func EndpointDaAmbiente(valore string, c bool) (string, error) {
	if !c {
		return EndpointTelegram, nil
	}
	u, err := url.Parse(valore)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" ||
		u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("TELEGRAM_API_ENDPOINT deve essere un indirizzo http(s) con un host (senza utente, query né frammento)")
	}
	return strings.TrimRight(valore, "/"), nil
}

// NuovoTelegram è il provider sul database della configurazione (letta a ogni tentativo) e sull'indirizzo
// del Bot API (EndpointDaAmbiente).
func NuovoTelegram(d *db.DB, endpoint string) *Telegram {
	return &Telegram{d: d, base: strings.TrimRight(endpoint, "/"), tempo: 15 * time.Second}
}

// Attivo dice se l'invio Telegram è acceso e configurato (TelegramAttivo): la coda legge le righe
// Telegram solo allora, e Accoda le rifiuta altrimenti.
func (t *Telegram) Attivo(ctx context.Context) (bool, error) {
	return TelegramAttivo(ctx, t.d.Lettura)
}

// Consegna è attempt/1 e deliver/2 per Telegram: la configurazione di adesso; senza (o con l'invio
// spento) «not_configured» senza nessuna richiesta; la riga di un cliente con «Notifiche Telegram ai
// clienti» spente «telegram:notifiche ai clienti spente», senza richiesta; poi sendMessage.
func (t *Telegram) Consegna(ctx context.Context, m Messaggio) (string, error) {
	return t.consegna(ctx, m, true)
}

// ConsegnaAMano è Consegna senza l'interruttore della D-03: la usano solo «Manda adesso» di un messaggio
// rimasto in coda (D-27, Coda.MandaInSospeso) e la prova della pagina. Senza configurazione usabile resta
// «not_configured»; le notifiche ai clienti spente valgono anche qui.
func (t *Telegram) ConsegnaAMano(ctx context.Context, m Messaggio) (string, error) {
	return t.consegna(ctx, m, false)
}

func (t *Telegram) consegna(ctx context.Context, m Messaggio, conInterruttore bool) (string, error) {
	c, err := LeggiTelegram(ctx, t.d.Lettura)
	if err != nil {
		return "", err
	}
	acceso := true
	if conInterruttore {
		if acceso, err = telegramAcceso(ctx, t.d.Lettura); err != nil {
			return "", err
		}
	}
	if !c.Usabile() || !acceso {
		return "", Rifiuto(erroreNonConfigurato)
	}
	if m.Destinatario == "customer" {
		accese, err := NotificheClientiAccese(ctx, t.d.Lettura)
		if err != nil {
			return "", err
		}
		if !accese {
			return "", Rifiuto(erroreClientiSpenti)
		}
	}
	return t.mandaMessaggio(ctx, c, m)
}

// corpoMessaggio è il corpo di sendMessage nell'ordine dello standalone: chat_id e text, poi parse_mode e
// reply_markup solo se ci sono (put_present/3). La tastiera è il JSON salvato sulla riga, com'è.
type corpoMessaggio struct {
	ChatID      string          `json:"chat_id"`
	Text        string          `json:"text"`
	ParseMode   string          `json:"parse_mode,omitempty"`
	ReplyMarkup json.RawMessage `json:"reply_markup,omitempty"`
}

func (t *Telegram) mandaMessaggio(ctx context.Context, c ConfigTelegram, m Messaggio) (string, error) {
	corpo := corpoMessaggio{ChatID: m.Indirizzo, Text: m.Testo}
	if m.Formato != nil {
		corpo.ParseMode = *m.Formato
	}
	if m.Tastiera != nil {
		corpo.ReplyMarkup = json.RawMessage(*m.Tastiera)
	}
	j, err := jsonSenzaEscape(corpo)
	if err != nil {
		return "", err
	}
	stato, risposta, err := t.richiesta(ctx, c, http.MethodPost, "sendMessage", j)
	switch {
	case err != nil:
		return "", err
	case stato == http.StatusOK:
		rif, err := interpreta(risposta)
		return pulisciToken(rif, c), pulisciRifiuto(err, c)
	default:
		return "", Rifiuto(pulisciToken(rifiutoHTTP(stato, risposta), c))
	}
}

// ChiediNome è get_me/1: il nome del bot del token salvato, se getMe risponde «ok» con un username non
// vuoto; altrimenti un Rifiuto senza token («telegram:http:<stato>», «telegram:getMe senza username», o
// del trasporto). Senza configurazione usabile «not_configured», senza richiesta.
func (t *Telegram) ChiediNome(ctx context.Context) (string, error) {
	c, err := LeggiTelegram(ctx, t.d.Lettura)
	if err != nil {
		return "", err
	}
	if !c.Usabile() {
		return "", Rifiuto(erroreNonConfigurato)
	}
	stato, risposta, err := t.richiesta(ctx, c, http.MethodGet, "getMe", nil)
	switch {
	case err != nil:
		return "", err
	case stato != http.StatusOK:
		return "", Rifiuto("telegram:http:" + strconv.Itoa(stato))
	}
	var r struct {
		OK     bool `json:"ok"`
		Result struct {
			Username any `json:"username"`
		} `json:"result"`
	}
	if json.Unmarshal(risposta, &r) == nil && r.OK {
		if nome, ok := r.Result.Username.(string); ok && nome != "" {
			return nome, nil
		}
	}
	return "", Rifiuto("telegram:getMe senza username")
}

// richiesta è la porta di ogni chiamata al Bot API (request/4): <base>/bot<token>/<metodo>, JSON, il tempo
// massimo e niente redirect. Lo stato e al più massimoRisposta byte della risposta; un errore del
// trasporto è già un Rifiuto con il solo genere.
func (t *Telegram) richiesta(ctx context.Context, c ConfigTelegram, metodo, comando string, corpo []byte) (int, []byte, error) {
	ctx, ferma := context.WithTimeout(ctx, t.tempo)
	defer ferma()
	var dentro io.Reader
	if corpo != nil {
		dentro = bytes.NewReader(corpo)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, t.base+"/bot"+c.Token.valore()+"/"+comando, dentro)
	if err != nil {
		// Non succede con un token di forma esatta (Usabile): il messaggio di url.Parse porterebbe il token.
		return 0, nil, Rifiuto("telegram:transport::error")
	}
	if corpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	cliente := &http.Client{Transport: trasportoTelegram, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := cliente.Do(req)
	if err != nil {
		return 0, nil, Rifiuto("telegram:transport:" + genereTrasporto(err))
	}
	defer func() { _ = resp.Body.Close() }() // letto quanto serve
	letto, err := io.ReadAll(io.LimitReader(resp.Body, massimoRisposta+1))
	if err != nil {
		return 0, nil, Rifiuto("telegram:transport:" + genereTrasporto(err))
	}
	if len(letto) > massimoRisposta {
		// Troppo grande: non si capisce, come una risposta che non è JSON.
		letto = nil
	}
	return resp.StatusCode, letto, nil
}

// interpreta è interpret/1: «ok: true» con il message_id (il riferimento, come testo), «ok: false» con la
// descrizione («telegram:<descrizione>»), altrimenti «telegram:risposta non riconosciuta».
func interpreta(risposta []byte) (string, error) {
	var r struct {
		OK          bool `json:"ok"`
		Description any  `json:"description"`
		Result      *struct {
			MessageID json.RawMessage `json:"message_id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(risposta, &r); err == nil {
		switch {
		case r.OK && r.Result != nil && len(r.Result.MessageID) > 0:
			if rif, ok := testoDelRiferimento(r.Result.MessageID); ok {
				return rif, nil
			}
		case !r.OK:
			if d, ok := r.Description.(string); ok {
				return "", Rifiuto("telegram:" + d)
			}
		}
	}
	return "", Rifiuto("telegram:risposta non riconosciuta")
}

// testoDelRiferimento è to_string/1 del message_id: un numero intero com'è scritto, o un testo.
func testoDelRiferimento(j json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(j, &s) == nil {
		return s, true
	}
	var n json.Number
	d := json.NewDecoder(bytes.NewReader(j))
	d.UseNumber()
	if d.Decode(&n) == nil {
		if _, err := n.Int64(); err == nil {
			return n.String(), true
		}
	}
	return "", false
}

// rifiutoHTTP è refusal/3: «telegram:http:<stato>:<descrizione>» con la descrizione del corpo JSON, o
// «telegram:http:<stato>» senza.
func rifiutoHTTP(stato int, risposta []byte) string {
	var r struct {
		Description any `json:"description"`
	}
	if json.Unmarshal(risposta, &r) == nil {
		if d, ok := r.Description.(string); ok {
			return fmt.Sprintf("telegram:http:%d:%s", stato, d)
		}
	}
	return "telegram:http:" + strconv.Itoa(stato)
}

// pulisciToken è scrub/2: il token diventa «***».
func pulisciToken(s string, c ConfigTelegram) string {
	if tok := c.Token.valore(); tok != "" {
		return strings.ReplaceAll(s, tok, passwordNascosta)
	}
	return s
}

func pulisciRifiuto(err error, c ConfigTelegram) error {
	var r Rifiuto
	if errors.As(err, &r) {
		return Rifiuto(pulisciToken(string(r), c))
	}
	return err
}

// jsonSenzaEscape è Jason.encode!/1: come encoding/json, ma senza cambiare «<», «>» e «&» in <…
// (il testo HTML dei messaggi viaggia com'è), e senza l'a capo finale dell'Encoder.
func jsonSenzaEscape(v any) ([]byte, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

// trasportoTelegram è il trasporto del provider (review della 071): quello predefinito di net/http, ma
// senza proxy dell'ambiente (HTTP_PROXY, HTTPS_PROXY), perché il token viaggia nel percorso e un proxy lo
// vedrebbe. hackney dello standalone non legge quelle variabili.
var trasportoTelegram = func() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	return t
}()

// Segreti è Config.secrets(:telegram): il token salvato, così anche la pulizia della coda (pulisci, scrub/2)
// lo toglie da un riferimento o da un motivo, come la password per l'SMTP. Il provider li pulisce già da sé;
// questa è la seconda mano. Un database che non risponde dà nessun segreto (e il provider resta la prima).
func (t *Telegram) Segreti() []string {
	c, err := LeggiTelegram(context.Background(), t.d.Lettura)
	if err != nil || !c.Token.Presente() {
		return nil
	}
	return []string{c.Token.valore()}
}
