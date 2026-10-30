package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/clientip"
)

// Il primo titolare (ACC-16, «sostituita»): /configura/{codice} sulla faccia interna (D-28), con il
// codice che «gestrip primo-titolare» stampa sul terminale. Lo standalone non ha questa pagina:
// crea il primo titolare dall'installer. Come /invito: il codice nel percorso è tutta
// l'autorizzazione, aprire non lo spende, ogni rifiuto è la stessa pagina 404 (non dice se il
// codice esiste, se è scaduto o se il titolare c'è già). La superficie è quella dell'invito (né
// manifest né worker): prima di ogni account non c'è niente da installare.

// PrimoTitolare è quello che la pagina chiede al primo titolare (internal/accounts).
type PrimoTitolare interface {
	CodiceValido(ctx context.Context, codice string) (bool, error)
	Crea(ctx context.Context, codice, email, password, conferma, ip string) (int64, string, error)
}

const msgTitolareCreato = "Titolare creato. Ora puoi accedere."

type datiConfigura struct {
	Codice string // il codice del percorso, per l'action del modulo
	Email  string // dopo un rifiuto, l'email normalizzata
	Errore string
	Minimo int
}

func (f *facciaWeb) rifiutoConfigura(w http.ResponseWriter, r *http.Request) {
	f.mostraCon(w, r, "configura_non_valido", http.StatusNotFound, "", nil)
}

// moduloConfigura disegna il modulo se il codice vale, altrimenti il rifiuto: un modulo sbagliato
// su un codice morto è lo stesso rifiuto. La risposta non resta nella cache del browser.
func (f *facciaWeb) moduloConfigura(w http.ResponseWriter, r *http.Request, codice int, email, errore string) {
	c := r.PathValue("codice")
	ok, err := f.primo.CodiceValido(r.Context(), c)
	switch {
	case err != nil:
		Errore(w, r)
	case !ok:
		f.rifiutoConfigura(w, r)
	default:
		w.Header().Set("Cache-Control", "no-store")
		f.mostraCon(w, r, "configura", codice, "", datiConfigura{Codice: c, Email: email, Errore: errore, Minimo: accounts.LunghezzaMinima})
	}
}

func (f *facciaWeb) paginaConfigura(w http.ResponseWriter, r *http.Request) {
	f.moduloConfigura(w, r, http.StatusOK, "", "")
}

// configura crea il titolare: email sbagliata, password diverse o corte tornano al modulo (422)
// finché il codice vale; riuscito, si va al login con «Titolare creato. Ora puoi accedere.».
func (f *facciaWeb) configura(w http.ResponseWriter, r *http.Request) {
	email, ok1 := ultimo(r, "user[email]")
	pw, ok2 := ultimo(r, "user[password]")
	conf, ok3 := ultimo(r, "user[password_confirmation]")
	if !ok1 || !ok2 || !ok3 {
		f.rifiutoConfigura(w, r)
		return
	}
	normalizzata := accounts.NormalizzaEmail(email)
	_, _, err := f.primo.Crea(r.Context(), r.PathValue("codice"), email, pw, conf, clientip.Di(r))
	switch {
	case err == nil:
		st := statoDi(r)
		st.dati.Info, st.cambiata = msgTitolareCreato, true
		f.vaiA(w, r, "/login")
	case errors.Is(err, accounts.ErrEmailNonValida):
		f.moduloConfigura(w, r, http.StatusUnprocessableEntity, normalizzata, msgEmailNonValida)
	case errors.Is(err, accounts.ErrPasswordDiverse):
		f.moduloConfigura(w, r, http.StatusUnprocessableEntity, normalizzata, msgPasswordDiverse)
	case errors.Is(err, accounts.ErrPasswordCorta):
		f.moduloConfigura(w, r, http.StatusUnprocessableEntity, normalizzata,
			fmt.Sprintf("La password deve avere almeno %d caratteri.", accounts.LunghezzaMinima))
	case errors.Is(err, accounts.ErrCodiceNonValido):
		f.rifiutoConfigura(w, r)
	default:
		f.registro.Error("creazione del primo titolare non riuscita") // mai il codice né l'errore
		Errore(w, r)
	}
}
