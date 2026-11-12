package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clientip"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// L'avviso email della pagina del cliente sulla faccia pubblica (CLI-39, CLI-40, CLI-41, CLI-19 per il
// link storico), come email_optin/2, email_optout/2, legacy_email_optin/2, legacy_email_optout/2,
// email_confirm/2, email_confirm_post/2, email_stop/2 ed email_stop_post/2 di PublicRepairController
// dello standalone (go-baseline). Le regole stanno in internal/buste/email_cliente.go; qui gli
// indirizzi, le risposte e i testi:
//   - POST /r/{token}/email-optin e …/email-optout, POST /stato/{busta}/{anno}/email-optin e
//     …/email-optout (con la credenziale nei campi del modulo): un redirect al riquadro (#public-email)
//     con l'esito nella sua riga, così ricaricare non chiede due volte; link morto il 404 della pagina,
//     che il limite conta; busta chiusa la frase in cima alla pagina;
//   - D-16 (solo Go): il riquadro solo con l'email disponibile (uscita.EmailAttiva e l'indirizzo
//     pubblico); una richiesta da una pagina disegnata prima che l'email si spegnesse torna in cima alla
//     pagina con «non disponibile», senza scrivere niente;
//   - GET e POST /stato/email-conferma e /stato/email-disattiva: il link apre una pagina con un
//     pulsante, solo il pulsante scrive; i rifiuti sono i 410 dello standalone;
//   - D-02 (solo Go): un link di disattivazione non valido apre la pagina di ripiego, un modulo «scrivi
//     la tua email» (POST /stato/email-ripiego) che manda un link nuovo se l'indirizzo è iscritto: un
//     vecchio link dello standalone (Phoenix.Token) e, per la decisione di Stefano del 12/11/2026, ogni
//     altro link che non si verifica (della vecchia app Flask con ?e=, inventato, senza t, firmato con
//     una chiave di prima del rinnovo). Con l'email spenta le frasi del ripiego non promettono un'email
//     (testo neutro provvisorio, Stefano 12/11/2026). La risposta è sempre la stessa pagina, e il lavoro (cercare
//     l'indirizzo, accodare le email) parte dopo la risposta (Sottofondo), così nemmeno il tempo dice
//     se l'indirizzo c'è; al più 5 invii in 15 minuti per indirizzo IP (ratelimit.RipiegoEmail), oltre
//     la stessa risposta e niente lavoro. Un vecchio link di conferma (48 ore) è il 410 «Link non
//     valido o scaduto», che dice di chiedere un link nuovo dalla pagina.

// I testi dello standalone (emailed/3, revoked/3, email_refusal/1, confirmed/2, stop_refused/1).
const (
	msgEmailInInvio        = "Ti stiamo inviando l'email di conferma: apri il link che contiene per attivare l'avviso."
	msgEmailRimessa        = "✉️ Email di conferma rimessa in invio."
	msgEmailDisattivata    = "Avviso email disattivato."
	msgNonRiceveAvvisi     = "Questa riparazione non riceve più avvisi."
	msgEmailConsenso       = "⚠️ Serve il consenso per attivare l'avviso."
	msgEmailIndirizzo      = "⚠️ Controlla l'indirizzo email."
	msgEmailNonDisponibile = "⚠️ L'invio dell'email non è al momento disponibile. Riprova più tardi."
	msgEmailTroppe         = "⚠️ Troppe richieste nell'ultima ora: riprova più tardi o contatta il negozio."
	msgEmailNessunaAttesa  = "⚠️ Nessun indirizzo in attesa di conferma."
	msgEmailNonDisattivata = "⚠️ Non è stato possibile disattivare l'avviso. Riprova più tardi."
)

// datiAzioneEmail è quello che email_azione.html riceve: la pagina che un link dell'email apre (il
// pulsante che fa quello per cui il link c'è), o il modulo della pagina di ripiego (ConIndirizzo).
type datiAzioneEmail struct {
	Titolo, Messaggio, Azione, Token, Pulsante, IDPulsante string
	ConIndirizzo                                           bool
}

// riquadroEmail è il riquadro della pagina della busta id (EmailNotice.box/2), solo con l'email
// disponibile (D-16). Un errore nel leggerlo non toglie la pagina: il riquadro manca e resta una riga
// nel registro.
func (f *facciaWeb) riquadroEmail(r *http.Request, id int64, storico bool) *buste.RiquadroEmail {
	ok, err := f.buste.EmailDisponibile(r.Context(), f.pubblico)
	if err == nil && ok {
		var q *buste.RiquadroEmail
		if q, err = f.buste.RiquadroEmail(r.Context(), id, storico); err == nil {
			return q
		}
	}
	if err != nil {
		f.registro.Error("riquadro email non letto")
	}
	return nil
}

// richiestaEmail legge il modulo del riquadro: reinvia=1 è «Reinvia conferma», che non legge altro.
func richiestaEmail(r *http.Request) buste.RichiestaEmail {
	var q buste.RichiestaEmail
	if v, ok := ultimo(r, "reinvia"); ok && v == "1" {
		q.Reinvia = true
		return q
	}
	q.Indirizzo, q.ConIndirizzo = ultimo(r, "email")
	q.Consenso, q.ConConsenso = ultimo(r, "consenso")
	return q
}

// emailOptin è POST /r/{token}/email-optin.
func (f *facciaWeb) emailOptin(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	tok := r.PathValue("token")
	q := richiestaEmail(r)
	err := f.buste.ChiediEmailCliente(r.Context(), tok, q, f.pubblico, contestoCliente(r))
	f.esitoRichiestaEmail(w, r, err, q.Reinvia, "/r/"+url.PathEscape(tok))
}

// emailOptinStorico è POST /stato/{busta}/{anno}/email-optin, con la credenziale nei campi del modulo.
func (f *facciaWeb) emailOptinStorico(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	cred, campi := credenzialeStorica(dalModulo(r))
	q := richiestaEmail(r)
	err := f.buste.ChiediEmailStorica(r.Context(), scrittoStorico(r), cred, q, f.pubblico, contestoCliente(r))
	f.esitoRichiestaEmail(w, r, err, q.Reinvia, paginaStoricaCon(r, campi))
}

// tornaAlRiquadro è put_flash(:email_info | :email_error) e il redirect al riquadro della pagina.
func (f *facciaWeb) tornaAlRiquadro(w http.ResponseWriter, pagina, info, errore string) {
	f.tornaAllaPaginaCon(w, pagina+"#public-email", datiSessione{InfoEmail: info, ErroreEmail: errore})
}

// esitoRichiestaEmail è emailed/3.
func (f *facciaWeb) esitoRichiestaEmail(w http.ResponseWriter, r *http.Request, err error, reinvia bool, pagina string) {
	switch {
	case err == nil && reinvia:
		f.tornaAlRiquadro(w, pagina, msgEmailRimessa, "")
	case err == nil:
		f.tornaAlRiquadro(w, pagina, msgEmailInInvio, "")
	case errors.Is(err, buste.ErrBustaNonTrovata):
		RifiutoPubblico(w, r, rifiutoRiparazione, false)
	case errors.Is(err, buste.ErrEmailChiusa):
		f.tornaAllaPaginaCon(w, pagina, datiSessione{Errore: msgNonRiceveAvvisi})
	case errors.Is(err, buste.ErrEmailNonDisponibile):
		// D-16: la pagina non ha il riquadro, quindi l'esito va in cima.
		f.tornaAllaPaginaCon(w, pagina, datiSessione{Errore: msgEmailNonDisponibile})
	case errors.Is(err, buste.ErrConsensoEmail):
		f.tornaAlRiquadro(w, pagina, "", msgEmailConsenso)
	case errors.Is(err, buste.ErrIndirizzoEmail):
		f.tornaAlRiquadro(w, pagina, "", msgEmailIndirizzo)
	case errors.Is(err, buste.ErrTroppeRichiesteMail):
		f.tornaAlRiquadro(w, pagina, "", msgEmailTroppe)
	case errors.Is(err, buste.ErrNessunaAttesa):
		f.tornaAlRiquadro(w, pagina, "", msgEmailNessunaAttesa)
	case errors.Is(err, buste.ErrEmailNonInviata):
		f.tornaAlRiquadro(w, pagina, "", msgEmailNonDisponibile)
	default:
		f.registro.Error("richiesta dell'avviso email non scritta")
		f.tornaAlRiquadro(w, pagina, "", msgEmailNonDisponibile)
	}
}

// emailOptout è POST /r/{token}/email-optout.
func (f *facciaWeb) emailOptout(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	tok := r.PathValue("token")
	err := f.buste.DisattivaEmailCliente(r.Context(), tok, contestoCliente(r))
	f.esitoDisattivazione(w, r, err, "/r/"+url.PathEscape(tok))
}

// emailOptoutStorico è POST /stato/{busta}/{anno}/email-optout, con la credenziale nei campi del modulo.
func (f *facciaWeb) emailOptoutStorico(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	cred, campi := credenzialeStorica(dalModulo(r))
	err := f.buste.DisattivaEmailStorica(r.Context(), scrittoStorico(r), cred, contestoCliente(r))
	f.esitoDisattivazione(w, r, err, paginaStoricaCon(r, campi))
}

// esitoDisattivazione è revoked/3: la stessa frase anche quando non c'era niente da togliere.
func (f *facciaWeb) esitoDisattivazione(w http.ResponseWriter, r *http.Request, err error, pagina string) {
	switch {
	case err == nil:
		f.tornaAlRiquadro(w, pagina, msgEmailDisattivata, "")
	case errors.Is(err, buste.ErrBustaNonTrovata):
		RifiutoPubblico(w, r, rifiutoRiparazione, false)
	case errors.Is(err, buste.ErrTroppeRichiesteMail):
		f.tornaAlRiquadro(w, pagina, "", msgEmailTroppe)
	default:
		f.registro.Error("avviso email non disattivato")
		f.tornaAlRiquadro(w, pagina, "", msgEmailNonDisattivata)
	}
}

// tokenEmail è il t di un link dell'email: dalla query nella GET, dal modulo nel POST (param/2: solo un
// testo; altrimenti vuoto, un link che non c'è).
func tokenEmail(r *http.Request) string {
	if r.Method == http.MethodPost {
		t, _ := ultimo(r, "t")
		return t
	}
	t, _ := parametroQuery(r.URL.RawQuery, "t")
	return t
}

// mostraEsitoEmail è la pagina email_outcome con il suo codice.
func (f *facciaWeb) mostraEsitoEmail(w http.ResponseWriter, r *http.Request, codice int, ok bool, titolo, messaggio, link string) {
	f.mostraPubblica(w, r, "esito_pubblico", codice, datiEsitoPubblico{OK: ok, Titolo: titolo, Messaggio: messaggio, Link: link})
}

// confermaEmail è GET /stato/email-conferma (email_confirm/2): la pagina con «Conferma», che non
// scrive niente; un link già usato risponde subito la pagina della conferma.
func (f *facciaWeb) confermaEmail(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	tok := tokenEmail(r)
	e, err := f.buste.ControllaConferma(r.Context(), tok)
	if err == nil && !e.Gia {
		f.mostraPubblica(w, r, "email_azione", http.StatusOK, datiAzioneEmail{Titolo: "Conferma la tua email",
			Messaggio: "Premi «Conferma» per ricevere l'avviso a questo indirizzo quando la riparazione sarà pronta per il ritiro.",
			Azione:    "/stato/email-conferma", Token: tok, Pulsante: "Conferma", IDPulsante: "email-confirm-button"})
		return
	}
	f.esitoConferma(w, r, e, err)
}

// confermaEmailPost è POST /stato/email-conferma (email_confirm_post/2): la conferma.
func (f *facciaWeb) confermaEmailPost(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	e, err := f.buste.ConfermaEmail(r.Context(), tokenEmail(r), contestoCliente(r))
	f.esitoConferma(w, r, e, err)
}

// esitoConferma è confirmed/2. Un vecchio link dello standalone (D-02) è un link non valido: dura 48
// ore e la frase dice già come averne uno nuovo.
func (f *facciaWeb) esitoConferma(w http.ResponseWriter, r *http.Request, e buste.EsitoConferma, err error) {
	switch {
	case err == nil:
		link := ""
		if e.Link != "" {
			link = f.pubblico + "/r/" + url.PathEscape(e.Link)
		}
		f.mostraEsitoEmail(w, r, http.StatusOK, true, "Email confermata ✅",
			"Ti avviseremo a questo indirizzo quando la riparazione sarà pronta per il ritiro.", link)
	case errors.Is(err, buste.ErrIndirizzoCambiato):
		f.mostraEsitoEmail(w, r, http.StatusGone, false, "Link non più valido", "L'indirizzo email di questa riparazione è cambiato.", "")
	case errors.Is(err, buste.ErrRichiestaSostituta):
		f.mostraEsitoEmail(w, r, http.StatusGone, false, "Link non più valido",
			"Per questa riparazione è stata fatta una richiesta più recente: usa il link dell'ultima email ricevuta.", "")
	case errors.Is(err, buste.ErrLinkEmailNonValido), errors.Is(err, buste.ErrLinkEmailVecchio):
		f.mostraEsitoEmail(w, r, http.StatusGone, false, "Link non valido o scaduto",
			"Richiedi un nuovo link dalla pagina della tua riparazione (bottone «Reinvia conferma»).", "")
	default:
		f.registro.Error("conferma dell'email non letta")
		Errore(w, r)
	}
}

// disattivaEmail è GET /stato/email-disattiva (email_stop/2): la pagina con «Disattiva», che non
// scrive niente; un link non valido la pagina di ripiego (D-02), dove lo standalone dà il 410
// stop_refused/1.
func (f *facciaWeb) disattivaEmail(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	tok := tokenEmail(r)
	if f.buste.ControllaStop(tok) != nil {
		f.mostraRipiego(w, r)
		return
	}
	f.mostraPubblica(w, r, "email_azione", http.StatusOK, datiAzioneEmail{Titolo: "Disattivare gli avvisi email?",
		Messaggio: "Premi «Disattiva» per non ricevere più email per questa riparazione.", Azione: "/stato/email-disattiva",
		Token: tok, Pulsante: "Disattiva", IDPulsante: "email-stop-button"})
}

// disattivaEmailPost è POST /stato/email-disattiva (email_stop_post/2): lo stesso successo che la
// busta porti ancora l'indirizzo o no (il link non dice niente della busta a chi lo ha).
func (f *facciaWeb) disattivaEmailPost(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	switch err := f.buste.DisattivaConLink(r.Context(), tokenEmail(r), contestoCliente(r)); {
	case err == nil:
		f.mostraEsitoEmail(w, r, http.StatusOK, true, "Avvisi email disattivati", "Non riceverai più email per questa riparazione.", "")
	case errors.Is(err, buste.ErrDisattivazioneNonRiuscita):
		f.registro.Error("avvisi email non disattivati dal link")
		f.mostraEsitoEmail(w, r, http.StatusServiceUnavailable, false, "Disattivazione non riuscita",
			"Non è stato possibile disattivare gli avvisi. Riprova più tardi con lo stesso link.", "")
	default: // un link non valido: il ripiego (D-02), dove lo standalone dà il 410 stop_refused/1
		f.mostraRipiego(w, r)
	}
}

// I testi della pagina di ripiego (D-02, solo Go); quelli «Spenta» con l'email spenta, senza promettere
// un'email (testo neutro provvisorio, Stefano 12/11/2026: il definitivo lo prepara il coordinatore con il
// manuale).
const (
	titoloRipiego         = "Disattiva gli avvisi email"
	msgRipiego            = "Questo link non è valido o non vale più. Scrivi l'indirizzo a cui ricevi gli avvisi: se è iscritto, ti mandiamo un link nuovo per disattivarli."
	msgRipiegoSpenta      = "Questo link non è valido o non vale più. Scrivi l'indirizzo a cui ricevi gli avvisi per chiedere di disattivarli."
	titoloRipiegoOK       = "Controlla la tua email"
	msgRipiegoOK          = "Se l'indirizzo che hai scritto riceve gli avvisi di una riparazione, tra poco ti arriva un'email con il link per disattivarli. Se non arriva, controlla lo spam o chiedi al negozio."
	titoloRipiegoSpentaOK = "Avvisi email"
	msgRipiegoSpentaOK    = "Al momento il negozio non manda email. Per disattivare gli avvisi o per qualunque dubbio chiedi al negozio."
	tempoRipiego          = 2 * time.Minute // tempo massimo del lavoro in sottofondo
)

// emailAccesa dice se l'invio delle email è acceso, per le frasi del ripiego: è uno stato di tutto il
// negozio, non dell'indirizzo, quindi non dice niente di chi scrive. Un errore nel leggerlo vale come
// spenta (la frase che non promette).
func (f *facciaWeb) emailAccesa(r *http.Request) bool {
	ok, err := f.buste.EmailDisponibile(r.Context(), f.pubblico)
	if err != nil {
		f.registro.Error("stato dell'email non letto per il ripiego")
	}
	return err == nil && ok
}

// mostraRipiego è la pagina di ripiego della D-02: il modulo «scrivi la tua email».
func (f *facciaWeb) mostraRipiego(w http.ResponseWriter, r *http.Request) {
	msg, pulsante := msgRipiego, "Mandami il link"
	if !f.emailAccesa(r) {
		msg, pulsante = msgRipiegoSpenta, "Invia richiesta"
	}
	f.mostraPubblica(w, r, "email_azione", http.StatusOK, datiAzioneEmail{Titolo: titoloRipiego, Messaggio: msg,
		Azione: "/stato/email-ripiego", Pulsante: pulsante, IDPulsante: "email-fallback-button", ConIndirizzo: true})
}

// postiLavoriRipiego è quanti lavori del ripiego possono essere in corso o in attesa insieme: oltre,
// l'invio si scarta (la risposta è la stessa), così una raffica da molti indirizzi IP non accumula
// lavori.
const postiLavoriRipiego = 4

// ripiegoEmail è POST /stato/email-ripiego (D-02, solo Go): sempre la stessa pagina; il lavoro parte
// dopo, se l'indirizzo IP non ha già mandato troppi moduli e c'è un posto libero.
func (f *facciaWeb) ripiegoEmail(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	ip := clientip.Di(r)
	if b, attesa := f.limiti.Prenota(ratelimit.RipiegoEmail, ip); attesa == 0 {
		// Ogni invio conta, riuscito o no: è il limite agli invii ripetuti, non ai tentativi sbagliati.
		f.limiti.Fallito(ratelimit.RipiegoEmail, ip, b)
		select {
		case f.postiRipiego <- struct{}{}:
			f.lavoroRipiego(r)
		default: // posti pieni: il lavoro si scarta, la risposta no
		}
	}
	if f.emailAccesa(r) {
		f.mostraEsitoEmail(w, r, http.StatusOK, true, titoloRipiegoOK, msgRipiegoOK, "")
		return
	}
	f.mostraEsitoEmail(w, r, http.StatusOK, true, titoloRipiegoSpentaOK, msgRipiegoSpentaOK, "")
}

// lavoroRipiego fa partire il lavoro del ripiego (con un posto già preso): al più tempoRipiego, un
// panico resta nel lavoro e nel registro va solo una frase fissa con il tipo del valore.
func (f *facciaWeb) lavoroRipiego(r *http.Request) {
	indirizzo, _ := ultimo(r, "email")
	c := contestoCliente(r)
	ctx := context.WithoutCancel(r.Context())
	f.sottofondo(func() {
		defer func() { <-f.postiRipiego }()
		defer func() {
			if v := recover(); v != nil {
				f.registro.Error("lavoro del ripiego andato in panico", "tipo", fmt.Sprintf("%T", v))
			}
		}()
		ctx, fine := context.WithTimeout(ctx, tempoRipiego)
		defer fine()
		if _, err := f.buste.MandaLinkDisattivazione(ctx, indirizzo, f.pubblico, c); err != nil {
			f.registro.Warn("link di disattivazione della pagina di ripiego non mandato")
		}
	})
}

// registraEmail registra le rotte dell'avviso email sulla faccia pubblica.
func (f *facciaWeb) registraEmail(mux *http.ServeMux) {
	f.rottaPubblica(mux, "POST /r/{token}/email-optin", f.emailOptin)
	f.rottaPubblica(mux, "POST /r/{token}/email-optout", f.emailOptout)
	f.rottaPubblica(mux, "POST /stato/{busta}/{anno}/email-optin", f.emailOptinStorico)
	f.rottaPubblica(mux, "POST /stato/{busta}/{anno}/email-optout", f.emailOptoutStorico)
	f.rottaPubblica(mux, "GET /stato/email-conferma", f.confermaEmail)
	f.rottaPubblica(mux, "POST /stato/email-conferma", f.confermaEmailPost)
	f.rottaPubblica(mux, "GET /stato/email-disattiva", f.disattivaEmail)
	f.rottaPubblica(mux, "POST /stato/email-disattiva", f.disattivaEmailPost)
	f.rottaPubblica(mux, "POST /stato/email-ripiego", f.ripiegoEmail)
}
