package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
)

// La pagina utenti del titolare (ACC-06, ACC-10, ACC-11), come UsersLive dello standalone. Senza
// LiveView le azioni sono moduli POST che tornano alla pagina con il messaggio dello standalone;
// «Mostra disattivati» è un indirizzo (?disattivati=1) invece di uno stato della pagina. Il
// permesso lo controlla f.rotta a ogni richiesta (live_session :owner).

const (
	msgDisattivato      = "Account disattivato. Le sessioni aperte cadono al primo gesto."
	msgNonDisattivato   = "Account non disattivato."
	msgRuoloAggiornato  = "Ruolo aggiornato."
	msgRuoloNonValido   = "Ruolo non valido."
	msgRuoloNonCambiato = "Ruolo non aggiornato."
	msgUltimoTitolare   = "È l'ultimo titolare attivo: nomina prima un altro titolare."
)

type rigaUtente struct {
	ID                        int64
	Email, Nome, RuoloScritto string
	Ruolo                     string
	Attivo                    bool
	InAttesa                  bool
	UltimoAccesso, Stato      string
}

type tabellaUtenti struct {
	ID     string
	Righe  []rigaUtente
	Token  string
	Aperti bool // i disattivati sono aperti: i moduli lo portano con sé
}

type datiUtenti struct {
	Attivi, Disattivati tabellaUtenti
	NumDisattivati      int
	Mostra              bool
	Invito              *invitoMostrato // il link appena creato, mostrato una volta (inviti.go)
	Modulo              moduloInvito    // i valori del modulo «Aggiungi un account»
}

// etichettaRuolo è role_label/1: il dipendente si chiama «Staff» sulla pagina, come nello
// standalone.
func etichettaRuolo(r string) string {
	if r == accounts.Titolare {
		return "Titolare"
	}
	return "Staff"
}

func statoUtente(v accounts.VoceUtente) string {
	switch {
	case !v.Attivo:
		return "Disattivato"
	case v.InAttesa:
		return "Invito da consegnare"
	}
	return "Attivo"
}

func (f *facciaWeb) paginaUtenti(w http.ResponseWriter, r *http.Request) {
	d, err := f.datiUtenti(w, r, r.URL.Query().Get("disattivati") == "1")
	if err != nil {
		Errore(w, r)
		return
	}
	f.mostraCon(w, r, "utenti", http.StatusOK, "", d)
}

// datiUtenti è l'elenco della pagina utenti, con i disattivati aperti se mostra.
func (f *facciaWeb) datiUtenti(w http.ResponseWriter, r *http.Request, mostra bool) (datiUtenti, error) {
	ee, err := f.utenti.ElencoUtenti(r.Context())
	if err != nil {
		return datiUtenti{}, err
	}
	token := f.csrf.Token(w, r)
	d := datiUtenti{Attivi: tabellaUtenti{ID: "users-active", Token: token},
		Disattivati: tabellaUtenti{ID: "users-deactivated-table", Token: token},
		Mostra:      mostra, Modulo: moduloInvito{Ruolo: accounts.Dipendente}}
	for _, e := range ee {
		riga := rigaUtente{ID: e.ID, Email: e.Email, Nome: "—", Ruolo: e.Ruolo, RuoloScritto: etichettaRuolo(e.Ruolo),
			Attivo: e.Attivo, InAttesa: e.InAttesa, UltimoAccesso: "—", Stato: statoUtente(e)}
		if e.Nome != "" {
			riga.Nome = e.Nome
		}
		if e.UltimoAccesso != nil {
			riga.UltimoAccesso = clock.Timbro(*e.UltimoAccesso)
		}
		if e.Attivo {
			d.Attivi.Righe = append(d.Attivi.Righe, riga)
		} else {
			d.Disattivati.Righe = append(d.Disattivati.Righe, riga)
		}
	}
	d.NumDisattivati = len(d.Disattivati.Righe)
	d.Attivi.Aperti = d.Mostra
	return d, nil
}

// idPositivo è Id.positive/1: solo un intero maggiore di zero.
func idPositivo(s string) (int64, bool) {
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil && id > 0
}

// esitoUtenti torna alla pagina utenti con un messaggio di esito o di errore, con i disattivati
// aperti se lo erano: nello standalone la pagina non si ricarica (show_deactivated resta).
func (f *facciaWeb) esitoUtenti(w http.ResponseWriter, r *http.Request, info, errore string) {
	st := statoDi(r)
	st.dati.Info, st.dati.Errore, st.cambiata = info, errore, true
	dove := "/settings/utenti"
	if r.PostForm.Get("disattivati") == "1" {
		dove += "?disattivati=1"
	}
	f.vaiA(w, r, dove)
}

// rifiutoRuolo: i rifiuti del ruolo ricaricano la pagina (redraw/2, push_navigate), che
// richiude i disattivati, perché il menu mostra già il ruolo che il server non ha accettato.
func (f *facciaWeb) rifiutoRuolo(w http.ResponseWriter, r *http.Request, errore string) {
	st := statoDi(r)
	st.dati.Errore, st.cambiata = errore, true
	f.vaiA(w, r, "/settings/utenti")
}

func contestoDi(r *http.Request) audit.Contesto {
	return audit.Contesto{Attore: statoDi(r).utente.Email}
}

// disattivaUtente è l'evento deactivate.
func (f *facciaWeb) disattivaUtente(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.esitoUtenti(w, r, "", msgNonDisattivato)
		return
	}
	switch err := f.utenti.Disattiva(r.Context(), id, contestoDi(r)); {
	case err == nil:
		f.esitoUtenti(w, r, msgDisattivato, "")
	case errors.Is(err, accounts.ErrUltimoTitolare):
		f.esitoUtenti(w, r, "", msgUltimoTitolare)
	default:
		f.esitoUtenti(w, r, "", msgNonDisattivato)
	}
}

// ruoloUtente è l'evento set_role: parte alla scelta nel menu (phx-change).
func (f *facciaWeb) ruoloUtente(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	ruolo := r.PostForm["ruolo"] // ripetuto: vale l'ultimo, come Plug
	if !ok || len(ruolo) == 0 {
		f.rifiutoRuolo(w, r, msgRuoloNonCambiato)
		return
	}
	switch err := f.utenti.CambiaRuolo(r.Context(), id, ruolo[len(ruolo)-1], contestoDi(r)); {
	case err == nil:
		f.esitoUtenti(w, r, msgRuoloAggiornato, "")
	case errors.Is(err, accounts.ErrUltimoTitolare):
		f.rifiutoRuolo(w, r, msgUltimoTitolare)
	case errors.Is(err, accounts.ErrRuoloNonValido):
		f.rifiutoRuolo(w, r, msgRuoloNonValido)
	default:
		f.rifiutoRuolo(w, r, msgRuoloNonCambiato)
	}
}
