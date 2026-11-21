package assist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/audit"
)

// Il collegamento al modello (slice 106; RIP-H-46 parte estrazione, RIP-H-47, RIP-H-45 in parte; D-19,
// domanda F del piano T4.10). Come LocalChat e Assist.extract_with_model/3 dello standalone: una
// richiesta compatibile OpenAI a <indirizzo>/v1/chat/completions, temperature 0, la chiave come Bearer
// solo se c'è, il nome del modello che ha risposto, i rifiuti con i loro nomi e la riga di audit
// assist.extract con soli conteggi. Il testo del dettato, la risposta del modello e la chiave non
// finiscono mai in un log, in un audit, in un errore restituito o in un panic: gli errori portano solo
// un motivo fisso («ai:http:500», «ai:transport::timeout»…), mai il testo di un errore di rete (che
// può portare l'indirizzo) né il corpo di una risposta (che può ripetere quello che ha ricevuto).

// Limiti del dettato dello standalone (app_riparazioni_bp.py:396-400), in grafemi come String.length/1,
// contati sul dettato senza spazi ai bordi.
const (
	DettatoMinimo  = 10
	DettatoMassimo = 5000
)

// AttesaModello è l'attesa del modello: 90 s IN TUTTO, compreso il nuovo tentativo (domanda F). Lo
// standalone aspetta 90 s per richiesta (billing_client.py:184); qui la scadenza è una sola.
const AttesaModello = 90 * time.Second

// massimoRispostaModello è quanto si legge al più della risposta del server: una scheda sono poche
// centinaia di byte; oltre è «risposta non riconosciuta».
const massimoRispostaModello = 1 << 20

// Configurazione è la famiglia «ai» delle impostazioni dell'assistente (assist.ai.*, slice 107):
// indirizzo del server, nome del modello, chiave (anche vuota).
type Configurazione struct {
	Indirizzo, Modello, Chiave string
}

// Usabile è Config.usable?/1: indirizzo e modello non vuoti (a parte gli spazi).
func (c Configurazione) Usabile() bool {
	return trimElixir(c.Indirizzo) != "" && trimElixir(c.Modello) != ""
}

// Rifiuti dell'estrazione, uno per nome come in Assist.extract/3. ErrRispostaIllegibile è in scheda.go.
var (
	ErrNonConfigurato = errors.New("assist: nessun modello configurato")
	ErrTestoCorto     = errors.New("assist: testo troppo corto")
	ErrTestoLungo     = errors.New("assist: testo troppo lungo")
)

// ErrModello è {:provider, reason}: il server del modello non ha dato una risposta. Motivo è un codice
// fisso, senza testo del dettato, della risposta o della chiave: «ai:http:<stato>»,
// «ai:transport:<genere>» o «ai:risposta non riconosciuta».
type ErrModello struct{ Motivo string }

func (e ErrModello) Error() string { return "assist: modello non disponibile (" + e.Motivo + ")" }

// MessaggioDiRifiuto è assist_refusal/2 di WorkshopLive per la famiglia «ai»: la frase che vede il
// negozio per un rifiuto dell'estrazione; vuota per un errore che non è un rifiuto.
func MessaggioDiRifiuto(err error) string {
	var em ErrModello
	switch {
	case errors.Is(err, ErrNonConfigurato):
		return "Nessun modello è configurato: non è stato chiesto niente a nessuno."
	case errors.Is(err, ErrTestoCorto):
		return "⚠ Testo troppo corto: servono almeno " + strconv.Itoa(DettatoMinimo) + " caratteri."
	case errors.Is(err, ErrTestoLungo):
		return "⚠ Testo troppo lungo: al massimo " + strconv.Itoa(DettatoMassimo) + " caratteri."
	case errors.Is(err, ErrRispostaIllegibile):
		return "⚠ Il modello non ha risposto con una scheda leggibile, riprova."
	case errors.As(err, &em):
		return "⚠ AI temporaneamente non disponibile, riprova fra qualche minuto."
	}
	return ""
}

// Registratore scrive la riga di audit: *audit.Registro.
type Registratore interface {
	Registra(ctx context.Context, c audit.Contesto, azione, risorsa string, id any, esito string, dati map[string]any) error
}

// Collegamento parla con il server del modello. Attesa la accorciano i test.
type Collegamento struct {
	Config Configurazione
	Attesa time.Duration // in tutto, nuovo tentativo compreso; 0 = AttesaModello
}

// trasportoModello è la rete vera: senza proxy dall'ambiente, come hackney dello standalone, e senza
// keep-alive. Con una connessione tenuta aperta, i byte che un server manda oltre il Content-Length
// net/http li scrive nel log standard («Unsolicited response received on idle HTTP channel…»), e
// possono ripetere il dettato (revisione della 106); una connessione per richiesta non lo fa.
var trasportoModello = func() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	t.DisableKeepAlives = true
	return t
}()

// Estrazione è l'esito di Estrai: la proposta e il modello che ha risposto.
type Estrazione struct {
	Proposta Proposta
	Modello  string
	// Tentativi sono le richieste mandate al server (0, 1 o 2, D-19), anche quando Estrai dà un errore:
	// li conta il banco del modello (slice 112).
	Tentativi int
}

// Estrai è Assist.extract_with_model/3: dal dettato la proposta di scheda, con il modello che ha
// risposto. Prima, senza chiedere niente a nessuno e senza audit, i rifiuti ErrNonConfigurato,
// ErrTestoCorto e ErrTestoLungo (sul dettato senza spazi ai bordi, come String.trim/1). Poi la
// domanda nel formato corto (D-103) con al più un nuovo tentativo, dopo una risposta illeggibile o
// dopo un errore (D-19, domanda F), sempre dentro l'attesa unica; infine la riga assist.extract con il
// numero dei caratteri e, se è andata male, il motivo (ErrRispostaIllegibile o ErrModello). Oggi è
// l'ora del negozio (clock.Roma), negozio quello che conosce (slice 105).
func (m *Collegamento) Estrai(ctx context.Context, dettato string, oggi time.Time, negozio Negozio, reg Registratore, chi audit.Contesto) (Estrazione, error) {
	testo, caratteri, err := m.controllaPrima(dettato)
	if err != nil {
		return Estrazione{}, err
	}
	ctx, ferma := context.WithTimeout(ctx, m.attesa())
	defer ferma()
	var modello string
	tentativi := 0
	chiedi := func() (string, error) {
		if err := ctx.Err(); err != nil {
			// Attesa finita (o chiamante andato via): niente nuovo tentativo.
			return "", ErrModello{"ai:transport:" + genereRete(err)}
		}
		tentativi++
		contenuto, nome, err := m.Chiedi(ctx, "", Domanda(testo, oggi))
		modello = nome
		return contenuto, err
	}
	p, err := Estrai(chiedi, testo, oggi, negozio)
	dati := map[string]any{"characters": caratteri}
	if err != nil {
		var em ErrModello
		switch {
		case errors.As(err, &em):
			dati["reason"] = em.Motivo
		default: // ErrRispostaIllegibile: Estrai non dà altro
			dati["reason"] = "unreadable_answer"
		}
		scriviAudit(ctx, reg, chi, "error", dati)
		return Estrazione{Tentativi: tentativi}, err
	}
	scriviAudit(ctx, reg, chi, "ok", dati)
	return Estrazione{Proposta: p, Modello: modello, Tentativi: tentativi}, nil
}

// controllaPrima sono i rifiuti che non chiedono niente a nessuno e non scrivono audit (require_configured/2 e
// check_bounds/1): ErrNonConfigurato, poi ErrTestoCorto o ErrTestoLungo sul dettato senza spazi ai bordi, contato in
// grafemi. Dà il dettato pulito e i suoi caratteri.
func (m *Collegamento) controllaPrima(dettato string) (string, int, error) {
	if !m.Config.Usabile() {
		return "", 0, ErrNonConfigurato
	}
	testo := trimElixir(dettato)
	caratteri := uniseg.GraphemeClusterCount(testo)
	switch {
	case caratteri < DettatoMinimo:
		return "", 0, ErrTestoCorto
	case caratteri > DettatoMassimo:
		return "", 0, ErrTestoLungo
	}
	return testo, caratteri, nil
}

// scriviAudit è audit/5 dello standalone, che non guarda l'esito della scrittura: la proposta vale
// anche se la riga non si scrive. La scrittura non usa la scadenza del modello, che può essere passata.
func scriviAudit(ctx context.Context, reg Registratore, chi audit.Contesto, esito string, dati map[string]any) {
	registraAudit(ctx, reg, chi, "assist.extract", FamigliaEstrazione, esito, dati)
}

func (m *Collegamento) attesa() time.Duration {
	if m.Attesa > 0 {
		return m.Attesa
	}
	return AttesaModello
}

// Chiedi è exchange/3 di LocalChat: una richiesta a <indirizzo>/v1/chat/completions con le istruzioni
// come messaggio di sistema (se non sono vuote) e testo come messaggio dell'utente, temperature 0,
// niente stream, la chiave come Bearer se c'è. Dà il contenuto della risposta, com'è, e il nome del
// modello che ha risposto (quello configurato se il server non ne dà uno adatto, with_model/2). Un
// errore è sempre ErrModello. Il tempo è quello di ctx: Estrai ci mette l'attesa unica.
func (m *Collegamento) Chiedi(ctx context.Context, istruzioni, testo string) (contenuto, modello string, err error) {
	messaggi := []map[string]string{}
	if istruzioni != "" {
		messaggi = append(messaggi, map[string]string{"role": "system", "content": istruzioni})
	}
	messaggi = append(messaggi, map[string]string{"role": "user", "content": testo})
	corpo, err := json.Marshal(map[string]any{
		"model": m.Config.Modello, "temperature": 0, "stream": false, "messages": messaggi,
	})
	if err != nil {
		return "", "", ErrModello{"ai:transport::error"} // non succede: solo testi
	}
	indirizzo := strings.TrimRight(trimElixir(m.Config.Indirizzo), "/") + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, indirizzo, bytes.NewReader(corpo))
	if err != nil {
		// L'indirizzo non si legge: il messaggio di url.Parse lo ripeterebbe.
		return "", "", ErrModello{"ai:transport::error"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if chiave := trimElixir(m.Config.Chiave); chiave != "" {
		req.Header.Set("Authorization", "Bearer "+chiave)
	}
	// Niente redirect: la chiave va solo all'indirizzo configurato.
	cliente := &http.Client{Transport: trasportoModello, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := cliente.Do(req)
	if err != nil {
		return "", "", ErrModello{"ai:transport:" + genereRete(err)}
	}
	defer func() { _ = resp.Body.Close() }() // letto qui sotto quanto serve
	letto, err := io.ReadAll(io.LimitReader(resp.Body, massimoRispostaModello+1))
	if err != nil {
		return "", "", ErrModello{"ai:transport:" + genereRete(err)}
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", ErrModello{"ai:http:" + strconv.Itoa(resp.StatusCode)}
	}
	if len(letto) > massimoRispostaModello {
		return "", "", ErrModello{"ai:risposta non riconosciuta"}
	}
	contenuto, nome, ok := interpretaModello(letto)
	if !ok {
		return "", "", ErrModello{"ai:risposta non riconosciuta"}
	}
	return contenuto, nomeDelModello(nome, m.Config.Modello), nil
}

// interpretaModello è interpret/1: il content (testo) del primo choice e il «model» della risposta.
// Come il pattern di Jason, le chiavi contano con le loro maiuscole («Choices» non è «choices»: la
// lettura di encoding/json in una struct le confonderebbe), per questo si legge per mappe.
func interpretaModello(corpo []byte) (contenuto, modello string, ok bool) {
	var radice map[string]json.RawMessage
	if json.Unmarshal(corpo, &radice) != nil {
		return "", "", false
	}
	var scelte []json.RawMessage
	if json.Unmarshal(radice["choices"], &scelte) != nil || len(scelte) == 0 {
		return "", "", false
	}
	var scelta, messaggio map[string]json.RawMessage
	if json.Unmarshal(scelte[0], &scelta) != nil || json.Unmarshal(scelta["message"], &messaggio) != nil {
		return "", "", false
	}
	if contenuto, ok = testoJSON(messaggio["content"]); !ok {
		return "", "", false
	}
	modello, _ = testoJSON(radice["model"])
	return contenuto, modello, true
}

// testoJSON è un valore JSON che è una stringa (is_binary/1): null, numeri e il resto no. Un valore
// assente (nil) o null in un punto più in alto finisce qui o in un Unmarshal che fallisce.
func testoJSON(grezzo json.RawMessage) (string, bool) {
	var s string
	if len(grezzo) == 0 || grezzo[0] != '"' || json.Unmarshal(grezzo, &s) != nil {
		return "", false
	}
	return s, true
}

// reControllo è \p{C} di PCRE: un nome con un carattere di controllo, di formato, privato o non
// assegnato non si scrive.
var reControllo = regexp.MustCompile(`\p{C}`)

// nomeDelModello è with_model/2: il nome dato dal server, senza spazi ai bordi, se non è vuoto, ha al
// più 120 caratteri e nessun \p{C}; altrimenti quello configurato.
func nomeDelModello(dato, configurato string) string {
	nome := trimElixir(dato)
	if nome != "" && uniseg.GraphemeClusterCount(nome) <= 120 && !reControllo.MatchString(nome) {
		return nome
	}
	return configurato
}

// genereRete è il genere di un errore della rete, come inspect(reason) di hackney («:timeout»,
// «:econnrefused»…): mai il testo dell'errore, che porta l'indirizzo.
func genereRete(err error) string {
	var (
		ne   net.Error
		dns  *net.DNSError
		errn syscall.Errno
	)
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return ":timeout"
	case errors.As(err, &dns):
		return ":nxdomain"
	case errors.As(err, &errn):
		switch errn {
		case syscall.ECONNREFUSED:
			return ":econnrefused"
		case syscall.ECONNRESET:
			return ":econnreset"
		case syscall.EHOSTUNREACH:
			return ":ehostunreach"
		case syscall.ENETUNREACH:
			return ":enetunreach"
		}
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, context.Canceled):
		return ":closed"
	}
	return ":error"
}

// stripPython è str.strip() di Python, quello del traduttore sul dettato: via ai bordi gli spazi di
// str.isspace(), cioè quelli Unicode (White_Space) più i separatori U+001C-U+001F.
func stripPython(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f) })
}
