package web

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// «📨 Invii registrati» della scheda e «Togli dalla lista» (CLI-37, RIP-S-21, slice 067), come
// #repair-send-history e l'evento discard-send di RepairLive dello standalone (go-baseline):
//   - la lista è un <details> chiuso sotto «Rispondi al cliente», solo se la busta ha degli invii (anche
//     se la controparte ha perso il numero): «📨 Invii registrati (N)», una didascalia per canale presente
//     (WhatsApp, email, Telegram, sempre in quest'ordine) che dice che «registrato» non è «recapitato»,
//     le righe dalla più recente con canale e parte, stato e ora, indirizzo e testo. Telegram cliente
//     senza la chat, l'email con i link di conferma e di disattivazione mascherati, Telegram senza HTML;
//   - «Togli dalla lista» c'è sulle righe WhatsApp senza avviso di ritiro: POST
//     /repairs/{id}/togli-invio con la riga nel campo invio, per titolare e dipendente; riuscito
//     «Messaggio tolto dalla lista.»; l'avviso di ritiro «Questo è l'avviso di pronta: si annulla con
//     «azzera avviso», non da qui.»; ogni altro rifiuto (già tolta, di un'altra busta, malformata)
//     «Questo messaggio non è più in lista.». Il testo del messaggio non va mai nel registro.

const (
	msgInvioTolto        = "Messaggio tolto dalla lista."
	msgInvioAvvisoPronta = "Questo è l'avviso di pronta: si annulla con «azzera avviso», non da qui."
	msgInvioNonInLista   = "Questo messaggio non è più in lista."
	confermaTogliInvio   = "Togliere questo messaggio dalla lista? Non cambia ciò che hai già inviato su WhatsApp."
)

// didascalieInvii sono history_caption/1, nell'ordine di history_channels/1.
var didascalieInvii = []struct{ Canale, Testo string }{
	{"whatsapp", "WhatsApp: registrano che hai aperto WhatsApp con questo messaggio, mai che sia stato recapitato o letto."},
	{"email", "Posta SMTP: registrano che l'email è in coda o che il server di posta l'ha accettata, mai che sia stata recapitata " +
		"o letta. I link di conferma e di disattivazione restano solo nell'email del destinatario."},
	{"telegram", "Telegram cliente: registrano che la notifica è in coda o che Telegram l'ha accettata, mai che sia stata " +
		"recapitata o letta. Il tasto della notifica apre la pagina della busta."},
}

// riquadroInvii è la lista #repair-send-history.
type riquadroInvii struct {
	Righe      []rigaInvio
	Didascalie []struct{ Canale, Testo string }
	Conferma   string
}

type rigaInvio struct {
	ID         int64
	Etichetta  string // history_label/1
	Artigiano  bool   // « · all'artigiano»
	Stato, Il  string // state_label/2 e Clock.stamp/1
	Indirizzo  string // address_label/1, "" per Telegram cliente
	Testo      string // history_body/1
	Togliibile bool
}

// nomiCanale sono channel_label/1.
var nomiCanale = map[string]string{"email": "Posta SMTP", "telegram": "Telegram", "whatsapp": "WhatsApp"}

// statoInvio è state_label/2.
func statoInvio(canale, stato string) string {
	switch {
	case canale == "whatsapp" && stato == "accepted":
		return "Consegnato a WhatsApp"
	case stato == "queued":
		return "In coda"
	case stato == "accepted":
		return "Accettato dal provider"
	case stato == "failed":
		return "Fallito"
	}
	return stato
}

var (
	// mask_links/1 di EmailNotice: tutta la parola (senza spazi ASCII, come \S di una regex senza u).
	reLinkConferma   = regexp.MustCompile(`[^\t\n\v\f\r ]*/stato/email-conferma\?[^\t\n\v\f\r ]*`)
	reLinkDisattiva  = regexp.MustCompile(`[^\t\n\v\f\r ]*/stato/email-disattiva\?[^\t\n\v\f\r ]*`)
	reTagTelegram    = regexp.MustCompile(`<[^>]*>`)
	entitaTelegramGo = strings.NewReplacer("&lt;", "<", "&gt;", ">")
)

// testoInvio è history_body/1: Telegram senza i tag e con le tre entità rilette (plain_text/1), ogni
// altro canale con i link firmati dell'email mascherati (mask_links/1).
func testoInvio(canale, testo string) string {
	if canale == "telegram" {
		return strings.ReplaceAll(entitaTelegramGo.Replace(reTagTelegram.ReplaceAllString(testo, "")), "&amp;", "&")
	}
	testo = reLinkConferma.ReplaceAllString(testo, "[link di conferma: solo nell'email]")
	return reLinkDisattiva.ReplaceAllString(testo, "[link per disattivare gli avvisi: solo nell'email]")
}

// inviiDellaScheda prepara la lista; nil senza invii (la lista non c'è).
func inviiDellaScheda(s buste.Scheda) *riquadroInvii {
	if len(s.Invii) == 0 {
		return nil
	}
	q := &riquadroInvii{Conferma: confermaTogliInvio}
	presenti := map[string]bool{}
	for _, i := range s.Invii {
		presenti[i.Canale] = true
		r := rigaInvio{ID: i.ID, Etichetta: nomiCanale[i.Canale], Artigiano: i.Parte == "artisan", Stato: statoInvio(i.Canale, i.Stato),
			Il: clock.Timbro(i.Il), Indirizzo: i.Indirizzo, Testo: testoInvio(i.Canale, i.Testo), Togliibile: i.Togliibile}
		if r.Etichetta == "" {
			r.Etichetta = i.Canale
		}
		if i.Canale == "telegram" && i.Parte == "customer" {
			r.Etichetta, r.Indirizzo = "Telegram cliente", "" // mai la chat
		} else if i.Indirizzo == "contatto-scelto" {
			r.Indirizzo = "contatto scelto sul telefono"
		}
		q.Righe = append(q.Righe, r)
	}
	for _, d := range didascalieInvii {
		if presenti[d.Canale] {
			q.Didascalie = append(q.Didascalie, d)
		}
	}
	return q
}

// togliInvio è POST /repairs/{id}/togli-invio (l'evento discard-send).
func (f *facciaWeb) togliInvio(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	}
	conSegno := f.segnoDelModulo(r, id)
	testo, _ := ultimo(r, "invio")
	invio, err := strconv.ParseInt(testo, 10, 64)
	if err != nil || invio <= 0 { // Id.positive/1: un id che non è un intero positivo non c'è
		err = buste.ErrInvioNonInLista
	} else {
		err = f.buste.TogliInvio(r.Context(), id, invio, contestoDi(r))
	}
	switch {
	case err == nil:
		st := statoDi(r)
		st.dati.Info, st.cambiata = msgInvioTolto, true
		f.vaiA(w, r, f.allaScheda(r, id, ""))
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
	case errors.Is(err, buste.ErrInvioAvvisoPronta):
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgInvioAvvisoPronta)
	case errors.Is(err, buste.ErrInvioNonInLista):
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgInvioNonInLista)
	default:
		f.registro.Error("messaggio non tolto dalla lista")
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgInvioNonInLista)
	}
}
