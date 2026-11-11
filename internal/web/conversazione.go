package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// La conversazione della pagina del cliente e della scheda (CLI-10, CLI-11, CLI-12, CLI-14,
// RIP-S-19, RIP-S-20), come poll/2, message/2 e written/3 di PublicRepairController e gli eventi
// reply di RepairLive dello standalone (go-baseline):
//   - GET /r/{token}/poll è il JSON che lo script della pagina chiede ogni 30 secondi: per riga id,
//     lato, ora e testo, e l'istante dell'ultima; mai un nome, un numero o la busta. Link morto e
//     conversazione nascosta sono lo stesso 404 della pagina (contato dal limite, come lì); riuscito
//     non scrive niente;
//   - POST /r/{token}/messaggio (CSRF della faccia pubblica) torna alla pagina: riuscito, «Messaggio
//     inviato.» nel riquadro e l'ancora #public-thread; vuoto, troppo lungo o oltre il limite, la
//     frase nel riquadro; busta chiusa nel frattempo, la frase in cima alla pagina (che non ha più il
//     riquadro); link morto, il 404;
//   - POST /repairs/{id}/risposta (titolare e dipendente) è «Invia 🏪» della scheda: la risposta
//     nella conversazione, già letta, poi «Risposta inviata.» e il popup dell'avviso su WhatsApp
//     (slice 066, avvisi.go; la notifica Telegram al posto del popup arriva con la T4.7).

const (
	msgMessaggioInviato    = "Messaggio inviato."
	msgMessaggioVuoto      = "Scrivi un messaggio prima di inviare."
	msgMessaggioLungo      = "Messaggio troppo lungo (max 1000 caratteri)"
	msgTroppiMessaggi      = "Ha già inviato molti messaggi nell'ultima ora: riprovi più tardi o contatti il negozio."
	msgMessaggioNonInviato = "Non è stato possibile inviare il messaggio."
	msgNonRiceveMessaggi   = "Questa riparazione non riceve più messaggi."

	msgRispostaInviata  = "Risposta inviata."
	msgRispostaVuota    = "La risposta è vuota: niente è stato inviato."
	msgRispostaLunga    = "La risposta supera i 1000 caratteri: niente è stato inviato."
	msgRispostaNonFatta = "La risposta non si è potuta registrare: niente è stato inviato."
)

// rigaPoll è una riga del JSON del poll, con le chiavi nell'ordine in cui le manda lo standalone
// (misurato sul banco: id, at, body, party, side).
type rigaPoll struct {
	ID    int64  `json:"id"`
	At    string `json:"at"`
	Body  string `json:"body"`
	Party string `json:"party"`
	Side  string `json:"side"`
}

type corpoPoll struct {
	Messages []rigaPoll `json:"messages"`
	TS       *string    `json:"ts"`
}

// pollCliente è GET /r/{token}/poll.
func (f *facciaWeb) pollCliente(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	ms, err := f.buste.ConversazioneCliente(r.Context(), r.PathValue("token"))
	f.rispostaPoll(w, r, ms, err)
}

// rispostaPoll scrive il JSON del poll (poll_body/1), o il 404 della pagina.
func (f *facciaWeb) rispostaPoll(w http.ResponseWriter, r *http.Request, ms []buste.Messaggio, err error) {
	switch {
	case errors.Is(err, buste.ErrBustaNonTrovata):
		RifiutoPubblico(w, r, rifiutoRiparazione, false)
		return
	case err != nil:
		f.registro.Error("conversazione del cliente non letta")
		Errore(w, r)
		return
	}
	c := corpoPoll{Messages: []rigaPoll{}}
	for _, m := range ms {
		c.Messages = append(c.Messages, rigaPoll{ID: m.ID, At: clock.Timbro(m.InviatoIl), Body: m.Testo, Party: m.Parte, Side: m.Lato()})
	}
	if n := len(ms); n > 0 {
		c.TS = &ms[n-1].Inviato
	}
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false) // Jason non scappa <, > e &
	if err := e.Encode(c); err != nil {
		Errore(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(bytes.TrimSuffix(b.Bytes(), []byte("\n")))
}

// messaggioCliente è POST /r/{token}/messaggio, con il testo nel campo testo.
func (f *facciaWeb) messaggioCliente(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	token := r.PathValue("token")
	testo, _ := ultimo(r, "testo")
	err := f.buste.ScriviMessaggioCliente(r.Context(), token, testo, contestoCliente(r))
	f.esitoMessaggio(w, r, err, "/r/"+url.PathEscape(token))
}

// conversazioneScheda è il riquadro #repair-thread e il modulo #repair-reply della scheda.
type conversazioneScheda struct {
	Con    string // «il cliente» o «l'oreficeria»
	Righe  []rigaScheda
	Titolo string // «Rispondi al cliente» o «Rispondi all'oreficeria»
	Chiusa bool   // busta chiusa: la nota sotto la risposta
}

type rigaScheda struct {
	ID                      int64
	Parte, Segno, Il, Testo string
	Negozio                 bool
}

// conversazioneDellaScheda prepara il riquadro: tutta la conversazione, dal più vecchio.
func conversazioneDellaScheda(s buste.Scheda) conversazioneScheda {
	c := conversazioneScheda{Con: "il cliente", Titolo: "Rispondi al cliente", Chiusa: buste.ConversazioneChiusa(s.Stato)}
	if s.B2B() {
		c.Con, c.Titolo = "l'oreficeria", "Rispondi all'oreficeria"
	}
	for _, m := range s.Messaggi {
		c.Righe = append(c.Righe, rigaScheda{ID: m.ID, Parte: m.Parte, Segno: m.Segno(), Il: clock.Timbro(m.InviatoIl), Testo: m.Testo, Negozio: m.Parte == "shop"})
	}
	return c
}

// rispondiAlCliente è POST /repairs/{id}/risposta (l'evento reply), con il testo in reply[body].
func (f *facciaWeb) rispondiAlCliente(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	}
	conSegno := f.segnoDelModulo(r, id)
	testo, _ := ultimo(r, "reply[body]")
	testo = aCapoUnix(testo) // a_capo.go: la casella di LiveView nello standalone
	err := f.buste.RispondiAlCliente(r.Context(), id, testo, contestoDi(r))
	switch {
	case err == nil:
		// reply_notice/2 (slice 066): «Risposta inviata.» e il popup dell'avviso su WhatsApp per la
		// controparte, con il segno della scheda (la risposta ha scritto la sua riga); su una busta chiusa
		// il popup rimanda subito alla scheda (avvisi.go).
		st := statoDi(r)
		st.dati.Info, st.cambiata = msgRispostaInviata, true
		dove := "/repairs/" + strconv.FormatInt(id, 10) + "/avviso?scopo=risposta"
		if s := f.segnoPer(r, id, durataSegnoLink, f.orologio.Adesso()); s != "" {
			dove += "&mostra=" + s
		}
		f.vaiA(w, r, dove)
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
	case errors.Is(err, buste.ErrMessaggioVuoto):
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgRispostaVuota)
	case errors.Is(err, buste.ErrMessaggioTroppoLungo):
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgRispostaLunga)
	default:
		f.registro.Error("risposta al cliente non scritta")
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgRispostaNonFatta)
	}
}

// latiConversazione sono i tre lati che hanno un'icona, per i <template> dello script.
var latiConversazione = []string{"negozio", "wa", "cliente"}

type datiRigaConv struct {
	Riga    *rigaConversazione // nil: la riga vuota del <template>
	Negozio string
}

// rigaConv dà al modello «rigaConversazione» la riga (o nil) e il nome del negozio.
func rigaConv(riga any, negozio string) datiRigaConv {
	d := datiRigaConv{Negozio: negozio}
	if r, ok := riga.(rigaConversazione); ok {
		d.Riga = &r
	}
	return d
}

type datiIconaConv struct{ Lato, Negozio string }

// iconaConv dà al modello «iconaConversazione» il lato e il nome del negozio (l'alt del marchio).
func iconaConv(lato, negozio string) datiIconaConv { return datiIconaConv{lato, negozio} }
