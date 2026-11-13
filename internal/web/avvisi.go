package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// Il popup «📲 Avvisa cliente» (CLI-29…36, RIP-H-17, RIP-S-20, RIP-S-23), come NoticePopup e gli eventi
// notice-open, notice-cancel, notice-send e reset-notice di RepairLive e WorkshopLive dello standalone
// (go-baseline), senza LiveView:
//   - GET /repairs/{id}/avviso è il popup: dalla scheda (con il segno della scheda in mostra, che non si
//     consuma qui), dalla lista (da=lista con i filtri in lista) o dopo «Rispondi» (scopo=risposta). Non
//     scrive niente. Senza JavaScript è una pagina con il riquadro; con JavaScript app.js la chiede e
//     mette il riquadro sopra la pagina, e «Annulla», Esc e un tocco fuori lo chiudono senza richieste.
//     Una busta chiusa dalla lista, come notice-open di WorkshopLive: la lista con «La busta è chiusa:
//     niente è stato inviato.»;
//   - POST /repairs/{id}/avviso è «Invia»: il testo del riquadro e, nei campi nascosti, ciò per cui il
//     popup era disegnato (buste.Disegnata). Riuscito: senza JavaScript il redirect a wa.me (la pagina
//     stessa va a WhatsApp, come lo standalone quando il browser blocca la scheda nuova); con
//     JavaScript (Accept: application/json) il JSON con l'indirizzo, e app.js manda a wa.me la scheda
//     aperta al tocco e porta questa alla lista con i filtri di prima (D-74). Un testo vuoto ridisegna
//     il popup con il testo e «Il messaggio è vuoto: niente è stato inviato.», e così un testo con {link}
//     o {telegram_optin} senza indirizzo_pubblico, con «Messaggio non inviato: manca …» (D-46 estesa,
//     slice 067; il popup lo dice già prima, in #notice-link-missing); ogni altro rifiuto torna
//     da dove si è partiti (la scheda o la lista) con il messaggio dello standalone, niente scritto;
//   - POST /repairs/{id}/azzera-avviso è «Azzera l'avviso» del riquadro #repair-notice della scheda.
// Il canale è sempre WhatsApp: Telegram arriva con la T4.7.

const (
	msgAvvisoVuoto         = "Il messaggio è vuoto: niente è stato inviato."
	msgAvvisoAzzerato      = "Fatto: la busta può essere avvisata di nuovo."
	msgAvvisoNonAvvisata   = "Questa busta non risulta avvisata."
	msgAvvisoNonAzzerato   = "Non è stato possibile azzerare l'avviso."
	msgAvvisoChiusa        = "La busta è chiusa: niente è stato inviato."
	msgAvvisoNonInviato    = "Il messaggio non si è potuto inviare: niente è stato registrato."
	confermaAzzeraAvviso   = "Azzerare l'avviso? La busta potrà essere avvisata di nuovo e lo storico resta."
	avvisoTelegramSpentoJS = "⚠️ Notifiche Telegram ai clienti spente: il link Telegram porterà il cliente alla frase «Servizio notifiche temporaneamente non disponibile.» finché non le accendi in Impostazioni › Provider di invio."
)

// rifiutoAvviso è refusal/1 di NoticePopup: perché «Invia» non ha scritto niente.
func rifiutoAvviso(err error) string {
	switch {
	case errors.Is(err, buste.ErrControparteCambiata):
		return "La busta ha cambiato destinatario mentre il messaggio era aperto: niente è stato inviato. Riaprilo."
	case errors.Is(err, buste.ErrRecapitoCambiato):
		return "Il recapito è cambiato mentre il messaggio era aperto: niente è stato inviato. Riaprilo."
	case errors.Is(err, buste.ErrCanaleCambiato):
		return "La piattaforma d'invio è cambiata mentre il messaggio era aperto: niente è stato inviato. Riaprilo."
	case errors.Is(err, buste.ErrTipoCambiato):
		return "La busta è diventata pronta mentre il messaggio era aperto: niente è stato inviato. Riaprilo per l'avviso di ritiro."
	case errors.Is(err, buste.ErrGiaAvvisata):
		return "Questa busta è già stata avvisata: azzera l'avviso per rifarlo."
	case errors.Is(err, buste.ErrInLottoAperto):
		return "Questa busta è in un lotto ancora aperto: l'avviso parte con il lotto."
	case errors.Is(err, buste.ErrNonPronta):
		return "Questa busta non è più pronta: niente è stato inviato."
	case errors.Is(err, buste.ErrLinkNonSalvato):
		return "Il link della busta non si è potuto creare: niente è stato inviato."
	case errors.Is(err, buste.ErrTelegramSoloCliente):
		return "{telegram_optin} vale solo per il cliente diretto: niente è stato inviato."
	}
	return msgAvvisoNonInviato
}

// datiAvviso è il popup come lo disegna popup/1 di NoticePopup.
type datiAvviso struct {
	ID                   int64
	Titolo               string // title/1
	Numero               string // «Busta 0045/26» (D-64)
	Piattaforma          string // platform/1
	Dettaglio            string // platform_detail/1
	Problema             string // problem_text/1, "" senza problema
	ModelloEtichetta     string // il testo salvato che non si compone: la sua etichetta
	ModelloAvviso        string
	Testo                string
	Suggerimento         string // hint/1
	LinkMancante         string // senza indirizzo_pubblico: cosa configurare (D-46 estesa, slice 067)
	TelegramSpento       bool   // il cliente diretto con le notifiche Telegram spente
	MostraTelegramSpento bool   // e il testo ha il link Telegram
	AvvisoTelegram       string
	// I campi nascosti: ciò per cui il popup è disegnato e dove tornare.
	Scopo, Tipo, Canale, Indirizzo, Controparte string
	Mostra, Da, Lista                           string
	Annulla                                     string // «Annulla» e «Chiudi» senza JavaScript
}

// titoloPagina: il popup senza JavaScript è una pagina con il titolo del riquadro.
func (d datiAvviso) titoloPagina() string { return strings.TrimPrefix(d.Titolo, "📲 ") }

// reLinkTelegram è un /r/<token>/telegram scritto a mano (telegram_link?/1).
var reLinkTelegram = regexp.MustCompile(`/r/[A-Za-z0-9_-]+/telegram\b`)

func linkTelegram(testo string) bool {
	return strings.Contains(testo, "{telegram_optin}") || reLinkTelegram.MatchString(testo)
}

// origineAvviso è da dove si apre il popup: la scheda (con il suo segno) o la lista (con i filtri).
type origineAvviso struct {
	lista  bool
	mostra string // il segno della scheda, rimandato com'è
	query  string // i filtri della lista, già riscritti (ritornoLista)
}

// annulla è dove portano «Annulla» e «Chiudi» senza JavaScript, e dove torna un rifiuto.
func (o origineAvviso) annulla(id int64) string {
	if o.lista {
		if o.query == "" {
			return "/"
		}
		return "/?" + o.query
	}
	dove := "/repairs/" + strconv.FormatInt(id, 10)
	if o.mostra != "" {
		dove += "?mostra=" + url.QueryEscape(o.mostra)
	}
	return dove
}

// queryLista riscrive i filtri della lista come quelli della home (listaDelModulo).
func queryLista(lista string) string {
	_, indirizzo := ritornoLista(buste.LeggiFiltri(func(nome string) (string, bool) { return testoPlug(lista, nome) }))
	return strings.TrimPrefix(strings.TrimPrefix(indirizzo, "/"), "?")
}

// datiDelPopup scrive il popup della bozza.
func datiDelPopup(z buste.BozzaAvviso, o origineAvviso) datiAvviso {
	d := datiAvviso{ID: z.BustaID, Numero: z.Numero, Testo: z.Testo, Scopo: string(z.Scopo), Tipo: string(z.Tipo),
		Canale: z.Canale, Indirizzo: z.Indirizzo, Mostra: o.mostra, Lista: o.query, Annulla: o.annulla(z.BustaID),
		TelegramSpento: z.TelegramSpento, AvvisoTelegram: avvisoTelegramSpentoJS, Piattaforma: "WhatsApp"}
	if z.OreficeriaID != nil {
		d.Controparte = strconv.FormatInt(*z.OreficeriaID, 10)
	}
	if o.lista {
		d.Da = "lista"
	}
	switch {
	case z.Tipo == buste.TipoPronta:
		d.Titolo = "📲 Avviso di ritiro"
	case z.Parte == buste.ParteOreficeria:
		d.Titolo = "📲 Avvisa l'oreficeria"
	default:
		d.Titolo = "📲 Avvisa il cliente"
	}
	if z.Indirizzo != "" {
		// platform_detail/1: il nome fra parentesi anche vuoto (in Elixir "" è vero).
		d.Dettaglio = "a " + z.Indirizzo + " (" + z.Nome + ")"
	} else {
		d.Dettaglio = "nessun recapito"
	}
	if z.Problema != "" {
		d.Problema = "Nessun recapito per il cliente: avvisalo in un altro modo."
		if z.Parte == buste.ParteOreficeria {
			d.Problema = "Nessun recapito per l'oreficeria: avvisala in un altro modo."
		}
	}
	if z.Modello != nil {
		d.ModelloEtichetta, d.ModelloAvviso = z.Modello.Famiglia.Etichetta(), z.Modello.Avviso
	}
	d.Suggerimento = "{link} diventa il link della busta all'invio. Invia registra il messaggio in lista e apre WhatsApp."
	if z.Parte == buste.ParteCliente {
		d.Suggerimento = "{link} diventa il link della busta all'invio, e {telegram_optin} il suo link per le notifiche " +
			"Telegram. Invia registra il messaggio in lista e apre WhatsApp."
	}
	d.MostraTelegramSpento = d.TelegramSpento && linkTelegram(d.Testo)
	return d
}

// origineDellaGet legge l'origine dalla query della GET del popup.
func origineDellaGet(r *http.Request) origineAvviso {
	q := r.URL.RawQuery
	da, _ := parametroQuery(q, "da")
	if da == "lista" {
		lista, _ := parametroQuery(q, "lista")
		return origineAvviso{lista: true, query: queryLista(lista)}
	}
	mostra, _ := parametroQuery(q, "mostra")
	return origineAvviso{mostra: mostra}
}

// scopoDi legge lo scopo: «risposta» dopo una risposta, altrimenti la controparte.
func scopoDi(s string) buste.ScopoAvviso {
	if s == string(buste.ScopoRisposta) {
		return buste.ScopoRisposta
	}
	return buste.ScopoControparte
}

// paginaAvviso è GET /repairs/{id}/avviso (notice-open): niente si scrive.
func (f *facciaWeb) paginaAvviso(w http.ResponseWriter, r *http.Request) {
	o := origineDellaGet(r)
	torna := "/"
	if o.lista {
		torna = o.annulla(0)
	}
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, torna, msgBustaNonTrovata)
		return
	}
	scopo, _ := parametroQuery(r.URL.RawQuery, "scopo")
	z, err := f.buste.ApriAvviso(r.Context(), id, scopoDi(scopo))
	switch {
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, torna, msgBustaNonTrovata)
		return
	case err != nil:
		f.registro.Error("busta del popup dell'avviso non letta")
		Errore(w, r)
		return
	case o.lista && z.Chiusa:
		// notice-open di WorkshopLive: una busta chiusa dalla lista non apre niente.
		f.vaiAConErrore(w, r, torna, msgAvvisoChiusa)
		return
	case z.Scopo == buste.ScopoRisposta && z.Chiusa:
		// reply_notice/2: su una busta chiusa la pagina pubblica non mostra la conversazione e non si
		// propone niente; si torna alla scheda con il messaggio della risposta.
		f.vaiA(w, r, o.annulla(id))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	d := datiDelPopup(z, o)
	d.LinkMancante = f.linkMancante(d.Testo)
	f.mostraCon(w, r, "avviso", http.StatusOK, "", d)
}

// linkMancante è la riga del popup senza indirizzo_pubblico (D-46 estesa, slice 067): {link} e
// {telegram_optin} non partono, e cosa configurare con la frase della D-46, più come mandarlo lo stesso
// (069c, decisione dell'11/11): «Togli …» nomina quello che il testo contiene (slice 071, Dubbio 7 della
// 069c): {telegram_optin} da solo, tutti e due, o {link} (anche per un testo che non ne ha nessuno); ""
// con l'indirizzo.
func (f *facciaWeb) linkMancante(testo string) string {
	if f.pubblico != "" {
		return ""
	}
	togli := "{link}"
	switch conLink, conTelegram := strings.Contains(testo, "{link}"), strings.Contains(testo, "{telegram_optin}"); {
	case conLink && conTelegram:
		togli = "{link} e {telegram_optin}"
	case conTelegram:
		togli = "{telegram_optin}"
	}
	return "{link} e {telegram_optin} non si possono usare: " + f.comeConfigurare() + ". Togli " + togli + " dal testo per mandarlo senza link."
}

// inviaAvviso è POST /repairs/{id}/avviso (notice-send).
func (f *facciaWeb) inviaAvviso(w http.ResponseWriter, r *http.Request) {
	da, _ := ultimo(r, "da")
	o := origineAvviso{lista: da == "lista"}
	if o.lista {
		lista, _ := ultimo(r, "lista")
		o.query = queryLista(lista)
	}
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, o.annulla(0), msgBustaNonTrovata)
		return
	}
	conSegno := false
	if !o.lista {
		conSegno = f.segnoDelModulo(r, id)
	}
	torna := func() string {
		if o.lista {
			return o.annulla(id)
		}
		return f.dopoRifiuto(r, id, conSegno)
	}
	dis := disegnataDelModulo(r)
	testo, _ := ultimo(r, "notice[body]")
	// Il modulo senza JavaScript manda le righe come «\r\n»: si registra con «\n», come il valore della
	// casella che LiveView manda allo standalone (review della 067; a_capo.go).
	testo = aCapoUnix(testo)
	inv, err := f.buste.InviaAvviso(r.Context(), id, dis, testo, f.pubblico, contestoDi(r))
	switch {
	case err == nil:
		w.Header().Set("Cache-Control", "no-store")
		if strings.Contains(r.Header.Get("Accept"), "application/json") {
			corpo, _ := json.Marshal(map[string]string{"url": inv.URL}) // una mappa di testi: non fallisce
			f.salva(w, statoDi(r))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(corpo)
			return
		}
		f.vaiA(w, r, inv.URL)
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
	case errors.Is(err, buste.ErrAvvisoVuoto):
		// Il testo resta nel riquadro: basta scrivere qualcosa. Il popup si ridisegna con la busta di
		// adesso, ma i campi nascosti restano quelli del popup di prima.
		f.ridisegnaAvviso(w, r, id, dis, testo, o, msgAvvisoVuoto)
	case errors.Is(err, buste.ErrSenzaIndirizzoPubblico):
		// D-46 estesa (slice 067): il testo resta nel riquadro e il popup dice cosa configurare; niente
		// è stato scritto (né il link né la riga).
		f.ridisegnaAvviso(w, r, id, dis, testo, o, "Messaggio non inviato: "+f.comeConfigurare()+".")
	default:
		if errors.Is(err, buste.ErrLinkNonSalvato) {
			f.registro.Error("link della busta non creato per l'avviso", "indirizzo_pubblico", f.pubblico != "")
		} else if rifiutoAvviso(err) == msgAvvisoNonInviato {
			f.registro.Error("avviso non registrato")
		}
		f.vaiAConErrore(w, r, torna(), rifiutoAvviso(err))
	}
}

// disegnataDelModulo legge dai campi nascosti ciò per cui il popup era disegnato.
func disegnataDelModulo(r *http.Request) buste.Disegnata {
	scopo, _ := ultimo(r, "scopo")
	tipo, _ := ultimo(r, "tipo")
	canale, _ := ultimo(r, "canale")
	indirizzo, _ := ultimo(r, "indirizzo")
	d := buste.Disegnata{Scopo: scopoDi(scopo), Tipo: buste.TipoAvviso(tipo), Canale: canale, Indirizzo: indirizzo}
	if c, _ := ultimo(r, "controparte"); c != "" {
		if n, err := strconv.ParseInt(c, 10, 64); err == nil {
			d.OreficeriaID = &n
		}
	}
	return d
}

// ridisegnaAvviso è il popup dopo un testo vuoto o senza indirizzo_pubblico (D-46 estesa): lo stesso
// riquadro con il testo mandato e l'errore.
func (f *facciaWeb) ridisegnaAvviso(w http.ResponseWriter, r *http.Request, id int64, dis buste.Disegnata, testo string, o origineAvviso, errore string) {
	z, err := f.buste.ApriAvviso(r.Context(), id, dis.Scopo)
	switch {
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	case err != nil:
		f.registro.Error("busta del popup dell'avviso non letta")
		Errore(w, r)
		return
	}
	if !o.lista {
		// La scheda di ritorno vuole un segno nuovo: quello del modulo è stato consumato.
		o.mostra = f.segnoPer(r, id, durataSegnoLink, statoDi(r).inizioCatena)
	}
	d := datiDelPopup(z, o)
	d.Testo, d.Tipo, d.Canale, d.Indirizzo = testo, string(dis.Tipo), dis.Canale, dis.Indirizzo
	d.Controparte = ""
	if dis.OreficeriaID != nil {
		d.Controparte = strconv.FormatInt(*dis.OreficeriaID, 10)
	}
	d.MostraTelegramSpento = d.TelegramSpento && linkTelegram(testo)
	d.LinkMancante = f.linkMancante(testo)
	w.Header().Set("Cache-Control", "no-store")
	f.mostraCon(w, r, "avviso", http.StatusOK, errore, d)
}

// azzeraAvviso è POST /repairs/{id}/azzera-avviso (reset-notice).
func (f *facciaWeb) azzeraAvviso(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	}
	conSegno := f.segnoDelModulo(r, id)
	err := f.buste.AzzeraAvviso(r.Context(), id, contestoDi(r))
	switch {
	case err == nil:
		st := statoDi(r)
		st.dati.Info, st.cambiata = msgAvvisoAzzerato, true
		f.vaiA(w, r, f.allaScheda(r, id, ""))
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
	case errors.Is(err, buste.ErrNonAvvisata):
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgAvvisoNonAvvisata)
	default:
		f.registro.Error("avviso non azzerato")
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgAvvisoNonAzzerato)
	}
}

// riquadroAvviso è il riquadro #repair-notice della scheda (RIP-S-23): lo stato dell'avviso di ritiro
// per una busta pronta, il segno «📲 Avvisato il …» con «Azzera l'avviso», la storia.
type riquadroAvviso struct {
	DaFare, InLotto bool
	Lotto           int64
	AvvisatoIl      string // «gg/mm/aaaa hh:mm», "" se non avvisata
	Conferma        string
	Storia          []rigaStoriaAvviso
}

type rigaStoriaAvviso struct {
	ID            int64
	Etichetta, Il string
	Attore        string
}

// avvisoDellaScheda prepara il riquadro; nil se la busta non è pronta (:not_applicable).
func avvisoDellaScheda(s buste.Scheda) *riquadroAvviso {
	a := s.Avviso
	if a.Stato == buste.AvvisoNonApplicabile {
		return nil
	}
	q := &riquadroAvviso{DaFare: a.Stato == buste.AvvisoDaFare, InLotto: a.Stato == buste.AvvisoInLottoAperto, Lotto: a.Lotto,
		Conferma: confermaAzzeraAvviso}
	if a.Stato == buste.AvvisoFatto && a.Il != nil {
		q.AvvisatoIl = clock.Timbro(*a.Il)
	}
	for _, e := range a.Storia {
		etichetta := "Avviso inviato"
		if e.Evento == "reset" {
			etichetta = "Avviso azzerato"
		}
		q.Storia = append(q.Storia, rigaStoriaAvviso{ID: e.ID, Etichetta: etichetta, Il: clock.Timbro(e.Il), Attore: e.Attore})
	}
	return q
}

// pulsanteAvvisa è «📲 Avvisa cliente» della scheda (notify_label/1): c'è solo se la controparte ha un
// numero per WhatsApp (send_address).
func pulsanteAvvisa(s buste.Scheda) string {
	d, ok := s.Destinatario()
	if !ok || buste.NumeroWhatsApp(d.Telefono) == "" {
		return ""
	}
	if d.Parte == buste.ParteOreficeria {
		return "📲 Avvisa oreficeria"
	}
	return "📲 Avvisa cliente"
}

// linkTelegram è GET /r/{token}/telegram (telegram_link/2), il link che {telegram_optin} diventa, con il
// canale Telegram spento (fino alla T4.7): il 503 in testo semplice di telegram_followed/3; una busta
// chiusa torna alla sua pagina con il messaggio; un link che non apre niente o la busta di
// un'oreficeria il 404 di sempre, contato dal limite come le altre rotte /r/.
func (f *facciaWeb) linkTelegram(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	token := r.PathValue("token")
	err := f.buste.LinkTelegram(r.Context(), token)
	switch {
	case errors.Is(err, buste.ErrBustaNonTrovata), errors.Is(err, buste.ErrTelegramNascosto):
		RifiutoPubblico(w, r, rifiutoRiparazione, false)
	case errors.Is(err, buste.ErrTelegramChiusa):
		f.tornaAlCliente(w, token, "", "Questa riparazione non riceve più avvisi.")
	case errors.Is(err, buste.ErrTelegramSpento):
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("Servizio notifiche temporaneamente non disponibile."))
	default:
		f.registro.Error("link Telegram non risolto")
		Errore(w, r)
	}
}
