package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/audit"
)

// Gli inviti (ACC-07, ACC-08, ACC-09): «Aggiungi un account» e «Nuovo link» sulla pagina utenti,
// come gli eventi invite e reissue di UsersLive, e /invito/{token} come InvitationController, sulla
// faccia interna (D-28). Il link appena creato si mostra una volta, nella risposta al modulo: non
// passa dal cookie di sessione e non si salva da nessuna parte. Si costruisce sull'indirizzo
// interno dell'apparecchio (011b), mai sull'Host della richiesta.

// Inviti è quello che le pagine chiedono agli inviti (internal/accounts).
type Inviti interface {
	Invita(ctx context.Context, email, nome, ruolo string, c audit.Contesto) (accounts.Invito, error)
	NuovoLink(ctx context.Context, id int64, c audit.Contesto) (accounts.Invito, error)
	Destinatario(ctx context.Context, token string) (string, bool, error)
	Accetta(ctx context.Context, token, password string) error
}

const (
	msgNonAggiunto        = "Account non aggiunto."
	msgRuoloInvito        = "Ruolo non valido."
	msgEmailPresa         = "Questa email è già registrata su un account."
	msgEmailNonValida     = "Email non valida."
	msgNuovoLinkPronto    = "Nuovo link pronto. Il precedente non vale più."
	msgNessunNuovoLink    = "Nessun nuovo link: questo account ha già una password."
	msgPasswordImpostata  = "Password impostata. Ora puoi accedere."
	msgSenzaIndirizzoAdd  = "Account non aggiunto: manca l'indirizzo della faccia interna (indirizzo_interno nel file di configurazione dell'apparecchio)."
	msgSenzaIndirizzoLink = "Nessun nuovo link: manca l'indirizzo della faccia interna (indirizzo_interno nel file di configurazione dell'apparecchio)."
)

type invitoMostrato struct {
	Email, URL string
}

type moduloInvito struct {
	Email, Nome, Ruolo string
}

// ultimo è il valore di un campo ripetuto come lo legge Plug: l'ultimo; "" se manca.
func ultimo(r *http.Request, campo string) (string, bool) {
	v := r.PostForm[campo]
	if len(v) == 0 {
		return "", false
	}
	return v[len(v)-1], true
}

// mostraUtenti risponde con la pagina utenti, il messaggio e, se c'è, il link appena creato.
func (f *facciaWeb) mostraUtenti(w http.ResponseWriter, r *http.Request, info, errore string, inv *invitoMostrato, m *moduloInvito) {
	d, err := f.datiUtenti(w, r, r.PostForm.Get("disattivati") == "1")
	if err != nil {
		Errore(w, r)
		return
	}
	d.Invito = inv
	if inv != nil {
		// Il link si mostra una volta: la risposta non resta nella cache del browser.
		w.Header().Set("Cache-Control", "no-store")
	}
	if m != nil {
		d.Modulo = *m
	}
	if info != "" {
		st := statoDi(r)
		st.dati.Info, st.cambiata = info, true
	}
	f.mostraCon(w, r, "utenti", http.StatusOK, errore, d)
}

func (f *facciaWeb) linkInvito(token string) string { return f.interno + "/invito/" + token }

// invitaUtente è l'evento invite. I campi sono quelli del modulo dello standalone (user[…]); un
// modulo senza nessuno di quei campi è «un'altra forma» (Account non aggiunto.). Il ruolo che
// manca è dipendente, il default dello schema; i valori scritti restano nel modulo dopo un rifiuto.
func (f *facciaWeb) invitaUtente(w http.ResponseWriter, r *http.Request) {
	// %{"user" => attrs} when is_map: «user» o «user[]» ne fanno una stringa o una lista.
	forma := false
	for k := range r.PostForm {
		if k == "user" || k == "user[]" {
			forma = false
			break
		}
		if strings.HasPrefix(k, "user[") {
			forma = true
		}
	}
	if !forma {
		f.mostraUtenti(w, r, "", msgNonAggiunto, nil, nil)
		return
	}
	m := moduloInvito{Ruolo: accounts.Dipendente}
	m.Email, _ = ultimo(r, "user[email]")
	m.Nome, _ = ultimo(r, "user[display_name]")
	if v, ok := ultimo(r, "user[role]"); ok {
		m.Ruolo = v
	}
	// Dopo un rifiuto il modulo ripropone i valori del changeset: email normalizzata, nome senza
	// spazi ai lati.
	rifiutato := moduloInvito{Email: accounts.NormalizzaEmail(m.Email), Nome: strings.TrimSpace(m.Nome), Ruolo: m.Ruolo}
	if f.interno == "" {
		f.mostraUtenti(w, r, "", msgSenzaIndirizzoAdd, nil, &rifiutato)
		return
	}
	inv, err := f.inviti.Invita(r.Context(), m.Email, m.Nome, m.Ruolo, contestoDi(r))
	switch {
	case err == nil:
		f.mostraUtenti(w, r, fmt.Sprintf("Account %s aggiunto. Consegna il link una volta sola.", inv.Email), "",
			&invitoMostrato{Email: inv.Email, URL: f.linkInvito(inv.Token)}, nil)
	case errors.Is(err, accounts.ErrRuoloNonValido):
		f.mostraUtenti(w, r, "", msgRuoloInvito, nil, &rifiutato)
	case errors.Is(err, accounts.ErrEmailOccupata):
		f.mostraUtenti(w, r, "", msgEmailPresa, nil, &rifiutato)
	case errors.Is(err, accounts.ErrEmailNonValida), errors.Is(err, accounts.ErrNomeNonValido):
		f.mostraUtenti(w, r, "", msgEmailNonValida, nil, &rifiutato)
	default:
		f.registro.Error("invito non riuscito")
		f.mostraUtenti(w, r, "", msgNonAggiunto, nil, &rifiutato)
	}
}

// nuovoLinkUtente è l'evento reissue: ogni rifiuto ha lo stesso messaggio, come lo standalone.
func (f *facciaWeb) nuovoLinkUtente(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.mostraUtenti(w, r, "", msgNessunNuovoLink, nil, nil)
		return
	}
	if f.interno == "" {
		f.mostraUtenti(w, r, "", msgSenzaIndirizzoLink, nil, nil)
		return
	}
	inv, err := f.inviti.NuovoLink(r.Context(), id, contestoDi(r))
	if err != nil {
		f.mostraUtenti(w, r, "", msgNessunNuovoLink, nil, nil)
		return
	}
	f.mostraUtenti(w, r, msgNuovoLinkPronto, "", &invitoMostrato{Email: inv.Email, URL: f.linkInvito(inv.Token)}, nil)
}

type datiInvito struct {
	Link   string // il token del percorso, per l'action del modulo
	Email  string
	Errore string
	Minimo int
}

// rifiutoInvito è la stessa pagina per ogni rifiuto, 404: niente dice quali inviti esistono.
func (f *facciaWeb) rifiutoInvito(w http.ResponseWriter, r *http.Request) {
	f.mostraCon(w, r, "invito_non_valido", http.StatusNotFound, "", nil)
}

func (f *facciaWeb) moduloInvito(w http.ResponseWriter, r *http.Request, codice int, errore string) {
	token := r.PathValue("token")
	email, ok, err := f.inviti.Destinatario(r.Context(), token)
	switch {
	case err != nil:
		Errore(w, r)
	case !ok:
		f.rifiutoInvito(w, r)
	default:
		f.mostraCon(w, r, "invito", codice, "", datiInvito{Link: token, Email: email, Errore: errore, Minimo: accounts.LunghezzaMinima})
	}
}

// paginaInvito è new/2: aprire il link non lo spende (anteprime e scanner non lo bruciano).
func (f *facciaWeb) paginaInvito(w http.ResponseWriter, r *http.Request) {
	f.moduloInvito(w, r, http.StatusOK, "")
}

// accettaInvito è create/2: password diverse o corte tornano al modulo (422) finché il link vale;
// riuscito, si va al login con «Password impostata. Ora puoi accedere.».
func (f *facciaWeb) accettaInvito(w http.ResponseWriter, r *http.Request) {
	pw, ok1 := ultimo(r, "password[password]")
	conf, ok2 := ultimo(r, "password[password_confirmation]")
	if !ok1 || !ok2 {
		f.rifiutoInvito(w, r)
		return
	}
	if pw != conf {
		f.moduloInvito(w, r, http.StatusUnprocessableEntity, msgPasswordDiverse)
		return
	}
	switch err := f.inviti.Accetta(r.Context(), r.PathValue("token"), pw); {
	case err == nil:
		st := statoDi(r)
		st.dati.Info, st.cambiata = msgPasswordImpostata, true
		f.vaiA(w, r, "/login")
	case errors.Is(err, accounts.ErrPasswordCorta):
		f.moduloInvito(w, r, http.StatusUnprocessableEntity, fmt.Sprintf("La password deve avere almeno %d caratteri.", accounts.LunghezzaMinima))
	case errors.Is(err, accounts.ErrInvitoNonValido):
		f.rifiutoInvito(w, r)
	default:
		Errore(w, r)
	}
}
