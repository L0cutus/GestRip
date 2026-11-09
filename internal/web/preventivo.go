package web

import (
	"errors"
	"net/http"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// La risposta del cliente al preventivo nella scheda e la sua revoca (RIP-S-22, CLI-09), come il
// riquadro #repair-quote e l'evento revoke-quote di RepairLive dello standalone (go-baseline):
//   - il riquadro c'è quando la busta ha una storia del preventivo (non sui segni): la risposta
//     data con l'istante, il motivo del rifiuto, «Revoca» con la sua conferma (data-confirm) e la
//     storia dalla riga più vecchia;
//   - POST /repairs/{id}/preventivo/revoca, per titolare e dipendente, revoca la risposta che la
//     pagina mostrava (campo answer, approved o rejected), mai quella riletta adesso: se nel
//     frattempo è cambiata, «Questa risposta non risulta più registrata.» e niente scritto;
//     riuscita, «Fatto: la risposta al preventivo è stata revocata.»; un answer diverso, «Richiesta
//     non valida.». Come le altre azioni della scheda, un id malformato o una busta che non c'è
//     portano alla home con «Riparazione non trovata.».

const (
	msgRispostaRevocata   = "Fatto: la risposta al preventivo è stata revocata."
	msgRispostaNonPiu     = "Questa risposta non risulta più registrata."
	msgRevocaNonRiuscita  = "Non è stato possibile revocare la risposta."
	msgRichiestaNonValida = "Richiesta non valida."
)

// rispostaScheda è il riquadro #repair-quote.
type rispostaScheda struct {
	Approvata, Rifiutata bool
	Il                   string // Clock.stamp/1
	Motivo               *string
	Storia               []rigaStoriaPreventivo
}

type rigaStoriaPreventivo struct {
	ID                   int64
	Etichetta, Il, Parte string
}

// rispostaDellaScheda prepara il riquadro: nil senza storia (il riquadro non c'è).
func rispostaDellaScheda(s buste.Scheda) *rispostaScheda {
	if len(s.StoriaPreventivo) == 0 {
		return nil
	}
	r := &rispostaScheda{}
	switch {
	case s.PreventivoAccettatoIl != nil:
		r.Approvata, r.Il = true, clock.Timbro(*s.PreventivoAccettatoIl)
	case s.PreventivoRifiutatoIl != nil:
		r.Rifiutata, r.Il, r.Motivo = true, clock.Timbro(*s.PreventivoRifiutatoIl), s.MotivoRifiuto
	}
	for _, e := range s.StoriaPreventivo {
		r.Storia = append(r.Storia, rigaStoriaPreventivo{ID: e.ID, Etichetta: e.Etichetta(), Il: clock.Timbro(e.Il), Parte: e.NomeParte()})
	}
	return r
}

// revocaRisposta è POST /repairs/{id}/preventivo/revoca (l'evento revoke-quote).
func (f *facciaWeb) revocaRisposta(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	}
	conSegno := f.segnoDelModulo(r, id)
	risposta, _ := ultimo(r, "answer")
	err := f.buste.RevocaRisposta(r.Context(), id, risposta, contestoDi(r))
	switch {
	case err == nil:
		st := statoDi(r)
		st.dati.Info, st.cambiata = msgRispostaRevocata, true
		f.vaiA(w, r, f.allaScheda(r, id, ""))
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
	case errors.Is(err, buste.ErrRispostaNonValid):
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgRichiestaNonValida)
	case errors.Is(err, buste.ErrNonApprovato), errors.Is(err, buste.ErrNonRifiutato):
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgRispostaNonPiu)
	default:
		f.registro.Error("risposta al preventivo non revocata")
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgRevocaNonRiuscita)
	}
}
