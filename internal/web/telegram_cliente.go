package web

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/L0cutus/gestrip/internal/buste"
)

// L'iscrizione Telegram della pagina del cliente sulla faccia pubblica (CLI-44, CLI-45, CLI-19 per il link
// storico), come telegram_optin/2, telegram_link/2, legacy_telegram_optin/2, telegram_optout/2 e
// legacy_telegram_optout/2 di PublicRepairController dello standalone (go-baseline). Le regole stanno in
// internal/buste/telegram_cliente.go; qui gli indirizzi, le risposte e i testi:
//   - POST /r/{token}/telegram-optin e /stato/{busta}/{anno}/telegram-optin (la credenziale nei campi del
//     modulo): «Attiva su Telegram» e «Modifica preferenze», un 302 verso t.me con il token nuovo; il
//     limite torna al riquadro (#public-telegram) con la frase nella sua riga; la busta chiusa e il canale
//     spento (una pagina disegnata prima) tornano alla pagina con la frase in cima; link morto e busta
//     conto terzi il 404 della pagina, che il limite ai tentativi conta;
//   - POST …/telegram-optout: «Disattiva», di nuovo al riquadro con «Notifiche Telegram disattivate.»;
//   - GET /r/{token}/telegram: il link che {telegram_optin} diventa nei messaggi WhatsApp (slice 066): un
//     302 verso t.me a ogni apertura; senza riquadro dove tornare, il canale spento è il 503 e il limite il
//     429 in testo semplice dello standalone;
//   - il riquadro solo con il canale disponibile (TelegramDisponibile: l'invio Telegram acceso, il bot
//     configurato, le notifiche ai clienti accese, il nome del bot noto e l'indirizzo pubblico, D-16 come
//     per l'email). Nell'anteprima del negozio c'è, spento.

// I testi dello standalone (telegram_linked/3, telegram_followed/3, telegram_revoked/3).
const (
	msgTelegramNonDisponibile = "⚠️ Servizio non disponibile. Riprova più tardi."
	msgTelegramTroppe         = "⚠️ Troppe richieste nell'ultima ora: riprova più tardi o contatta il negozio."
	msgTelegramDisattivate    = "Notifiche Telegram disattivate."
	msgLinkTelegramSpento     = "Servizio notifiche temporaneamente non disponibile."
	msgLinkTelegramTroppe     = "Troppe richieste nell'ultima ora: riprova più tardi o contatta il negozio."
)

// riquadroTelegramCliente è il riquadro della pagina della busta id (TelegramSubscriptions.box/2), solo con il
// canale disponibile. Un errore nel leggerlo non toglie la pagina: il riquadro manca e resta una riga nel
// registro.
func (f *facciaWeb) riquadroTelegramCliente(r *http.Request, id int64, storico bool) *buste.RiquadroTelegram {
	ok, err := f.buste.TelegramDisponibile(r.Context(), f.pubblico)
	if err == nil && ok {
		var q *buste.RiquadroTelegram
		if q, err = f.buste.RiquadroTelegram(r.Context(), id, storico); err == nil {
			return q
		}
	}
	if err != nil {
		f.registro.Error("riquadro Telegram non letto")
	}
	return nil
}

// telegramOptin è POST /r/{token}/telegram-optin.
func (f *facciaWeb) telegramOptin(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	tok := r.PathValue("token")
	link, err := f.buste.IscriviTelegramCliente(r.Context(), tok, f.pubblico, contestoCliente(r))
	f.esitoIscrizioneTelegram(w, r, link, err, "/r/"+url.PathEscape(tok))
}

// telegramOptinStorico è POST /stato/{busta}/{anno}/telegram-optin, con la credenziale nei campi del modulo.
func (f *facciaWeb) telegramOptinStorico(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	cred, campi := credenzialeStorica(dalModulo(r))
	link, err := f.buste.IscriviTelegramStorica(r.Context(), scrittoStorico(r), cred, f.pubblico, contestoCliente(r))
	f.esitoIscrizioneTelegram(w, r, link, err, paginaStoricaCon(r, campi))
}

// esitoIscrizioneTelegram è telegram_linked/3.
func (f *facciaWeb) esitoIscrizioneTelegram(w http.ResponseWriter, r *http.Request, link string, err error, pagina string) {
	switch {
	case err == nil:
		rimanda(w, link)
	case errors.Is(err, buste.ErrBustaNonTrovata), errors.Is(err, buste.ErrTelegramNascosto):
		RifiutoPubblico(w, r, rifiutoRiparazione, false)
	case errors.Is(err, buste.ErrTelegramChiusa):
		f.tornaAllaPaginaCon(w, pagina, datiSessione{Errore: msgNonRiceveAvvisi})
	case errors.Is(err, buste.ErrTroppeRichiesteTelegram):
		f.tornaAllaPaginaCon(w, pagina+"#public-telegram", datiSessione{ErroreTelegram: msgTelegramTroppe})
	case errors.Is(err, buste.ErrTelegramSpento):
		// Il riquadro non c'è con il canale spento: la pagina era stata disegnata prima.
		f.tornaAllaPaginaCon(w, pagina, datiSessione{Errore: msgTelegramNonDisponibile})
	default:
		f.registro.Error("link Telegram non emesso")
		f.tornaAllaPaginaCon(w, pagina, datiSessione{Errore: msgTelegramNonDisponibile})
	}
}

// linkTelegramWhatsApp è GET /r/{token}/telegram (telegram_link/2), il link che {telegram_optin} diventa:
// senza riquadro dove tornare, il canale spento e il limite sono le frasi dello standalone in testo
// semplice (telegram_followed/3); il resto come il pulsante.
func (f *facciaWeb) linkTelegramWhatsApp(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	tok := r.PathValue("token")
	link, err := f.buste.LinkTelegram(r.Context(), tok, f.pubblico, contestoCliente(r))
	switch {
	case errors.Is(err, buste.ErrTelegramSpento):
		testoSemplice(w, http.StatusServiceUnavailable, msgLinkTelegramSpento)
	case errors.Is(err, buste.ErrTroppeRichiesteTelegram):
		testoSemplice(w, http.StatusTooManyRequests, msgLinkTelegramTroppe)
	case err != nil && !errors.Is(err, buste.ErrBustaNonTrovata) && !errors.Is(err, buste.ErrTelegramNascosto) &&
		!errors.Is(err, buste.ErrTelegramChiusa):
		f.registro.Error("link Telegram non risolto")
		Errore(w, r)
	default:
		f.esitoIscrizioneTelegram(w, r, link, err, "/r/"+url.PathEscape(tok))
	}
}

// testoSemplice è plain/3 del controller: il codice e la frase in text/plain.
func testoSemplice(w http.ResponseWriter, codice int, testo string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(codice)
	_, _ = w.Write([]byte(testo))
}

// telegramOptout è POST /r/{token}/telegram-optout.
func (f *facciaWeb) telegramOptout(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	tok := r.PathValue("token")
	err := f.buste.DisattivaTelegramCliente(r.Context(), tok, contestoCliente(r))
	f.esitoDisattivazioneTelegram(w, r, err, "/r/"+url.PathEscape(tok))
}

// telegramOptoutStorico è POST /stato/{busta}/{anno}/telegram-optout, con la credenziale nei campi del modulo.
func (f *facciaWeb) telegramOptoutStorico(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	cred, campi := credenzialeStorica(dalModulo(r))
	err := f.buste.DisattivaTelegramStorica(r.Context(), scrittoStorico(r), cred, contestoCliente(r))
	f.esitoDisattivazioneTelegram(w, r, err, paginaStoricaCon(r, campi))
}

// esitoDisattivazioneTelegram è telegram_revoked/3: la stessa frase anche quando non c'era niente da
// spegnere; ogni rifiuto il 404.
func (f *facciaWeb) esitoDisattivazioneTelegram(w http.ResponseWriter, r *http.Request, err error, pagina string) {
	switch {
	case err == nil:
		f.tornaAllaPaginaCon(w, pagina+"#public-telegram", datiSessione{InfoTelegram: msgTelegramDisattivate})
	case errors.Is(err, buste.ErrBustaNonTrovata), errors.Is(err, buste.ErrTelegramNascosto):
		RifiutoPubblico(w, r, rifiutoRiparazione, false)
	default:
		f.registro.Error("notifiche Telegram non disattivate")
		Errore(w, r)
	}
}

// registraTelegram registra le rotte dell'iscrizione Telegram sulla faccia pubblica.
func (f *facciaWeb) registraTelegram(mux *http.ServeMux) {
	f.rottaPubblica(mux, "GET /r/{token}/telegram", f.linkTelegramWhatsApp) // {telegram_optin}, slice 066
	f.rottaPubblica(mux, "POST /r/{token}/telegram-optin", f.telegramOptin)
	f.rottaPubblica(mux, "POST /r/{token}/telegram-optout", f.telegramOptout)
	f.rottaPubblica(mux, "POST /stato/{busta}/{anno}/telegram-optin", f.telegramOptinStorico)
	f.rottaPubblica(mux, "POST /stato/{busta}/{anno}/telegram-optout", f.telegramOptoutStorico)
}
