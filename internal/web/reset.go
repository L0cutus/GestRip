package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/clientip"
)

// La password dimenticata del titolare (ACC-13, ACC-14): /reset-request e /reset/{token}, come
// PasswordResetController dello standalone, sulla faccia interna (D-28). Il token sta nel percorso:
// il registro del processo scrive solo il pattern della rotta, mai il percorso.

// RecuperoPassword è quello che le pagine chiedono al recupero (internal/accounts).
type RecuperoPassword interface {
	RichiestaDallaPagina(ctx context.Context, ip string) (string, error)
	AccountDel(ctx context.Context, token string) (string, bool, error)
	Reimposta(ctx context.Context, token, password, conferma, ip string) error
}

// inDisparte fa girare il lavoro della richiesta dopo la risposta (Task.Supervisor nello
// standalone): né le parole né il tempo della risposta dicono che cosa è successo. I test lo
// fanno girare subito.
var inDisparte = func(f func()) { go f() }

const (
	msgPasswordAggiornata = "Password aggiornata. Accedi con la nuova password."
	msgPasswordDiverse    = "Le due password non coincidono."
)

type datiReset struct {
	Inviata bool
	Minuti  int
	Token   string
	Email   string
	Errore  string
	Minimo  int
}

func minutiReset() int { return int(accounts.DurataReset.Minutes()) }

func (f *facciaWeb) paginaRichiestaReset(w http.ResponseWriter, r *http.Request) {
	f.mostraCon(w, r, "reset_richiesta", http.StatusOK, "", datiReset{Minuti: minutiReset()})
}

// richiediReset risponde subito «Richiesta inviata», qualunque cosa succeda dopo.
func (f *facciaWeb) richiediReset(w http.ResponseWriter, r *http.Request) {
	ip := clientip.Di(r)
	ctx := context.WithoutCancel(r.Context())
	inDisparte(func() {
		if _, err := f.recupero.RichiestaDallaPagina(ctx, ip); err != nil {
			f.registro.Error("recupero della password non riuscito") // mai il link né l'errore
		}
	})
	f.mostraCon(w, r, "reset_richiesta", http.StatusOK, "", datiReset{Inviata: true, Minuti: minutiReset()})
}

func (f *facciaWeb) rifiutaReset(w http.ResponseWriter, r *http.Request) {
	f.mostraCon(w, r, "reset_non_valido", http.StatusBadRequest, "", nil)
}

func (f *facciaWeb) moduloReset(w http.ResponseWriter, r *http.Request, codice int, errore string) {
	token := r.PathValue("token")
	email, ok, err := f.recupero.AccountDel(r.Context(), token)
	switch {
	case err != nil:
		Errore(w, r)
	case !ok:
		f.rifiutaReset(w, r)
	default:
		f.mostraCon(w, r, "reset_nuova", codice, "", datiReset{Token: token, Email: email, Errore: errore, Minimo: accounts.LunghezzaMinima})
	}
}

func (f *facciaWeb) paginaNuovaPassword(w http.ResponseWriter, r *http.Request) {
	f.moduloReset(w, r, http.StatusOK, "")
}

// reimposta è update/2: le due password diverse o corte tornano al modulo (422) finché il link
// vale; riuscito, la sessione di questo browser riparte da zero e si va al login.
func (f *facciaWeb) reimposta(w http.ResponseWriter, r *http.Request) {
	pw, conf := r.PostForm["password[password]"], r.PostForm["password[password_confirmation]"]
	if len(pw) == 0 || len(conf) == 0 {
		f.rifiutaReset(w, r)
		return
	}
	err := f.recupero.Reimposta(r.Context(), r.PathValue("token"), pw[len(pw)-1], conf[len(conf)-1], clientip.Di(r))
	switch {
	case err == nil:
		st := statoDi(r)
		st.dati, st.cambiata = datiSessione{Info: msgPasswordAggiornata}, true
		f.csrf.Rinnova(w)
		f.vaiA(w, r, "/login")
	case errors.Is(err, accounts.ErrPasswordDiverse):
		f.moduloReset(w, r, http.StatusUnprocessableEntity, msgPasswordDiverse)
	case errors.Is(err, accounts.ErrPasswordCorta):
		f.moduloReset(w, r, http.StatusUnprocessableEntity, fmt.Sprintf("La password deve avere almeno %d caratteri.", accounts.LunghezzaMinima))
	case errors.Is(err, accounts.ErrLinkNonValido):
		f.rifiutaReset(w, r)
	default:
		Errore(w, r)
	}
}
