package web

import (
	"errors"
	"net/http"

	"github.com/L0cutus/gestrip/internal/buste"
)

// «📤 Chiudi e invia» nella pagina del lotto (slice 091, ORE-10), l'evento send di ReadyLotLive dello standalone, per
// tutto il personale. Senza LiveView è un modulo POST /ready-lots/{id}/invia (con data-confirm, la domanda dello
// standalone, e data-invio-unico):
//   - riuscito: la pagina del lotto (200, non un redirect) con «Lotto inviato: apri WhatsApp per mandarlo
//     all'oreficeria.» e il riquadro «Lotto inviato» (il testo, «Apri WhatsApp» se l'oreficeria ha un numero, «👥
//     Scegli contatto» sempre, il link pubblico): come nella LiveView il testo pronto si vede una volta, e riaperta la
//     pagina dice «Questo lotto è partito il …». I due link vanno a wa.me in una scheda nuova e, con JavaScript,
//     portano questa pagina alla lista ricordata (D-74, come wa_home.js dello standalone: le scritture sono già
//     fatte quando il link si vede);
//   - rifiutato: la pagina ridisegnata con il lotto riletto e il messaggio (refusal/1 dell'invio): «Lotto vuoto: …»,
//     «Lotto già inviato.», «Busta #0045/26: <motivo>. Niente è partito.» (D-99), «Il testo «…» non è valido: …. Niente è
//     partito.», e senza indirizzo_pubblico «Lotto non inviato: manca … Niente è partito.» (D-46 estesa);
//   - un lotto che non c'è (o un id non valido) torna alla lista con «Lotto non trovato.» (mount);
//   - guasto del database → la pagina d'errore con «lotto non inviato» nel registro (lo standalone mostra «Invio non
//     riuscito: niente è partito.», 089 scelta 7).

const (
	msgLottoInviato   = "Lotto inviato: apri WhatsApp per mandarlo all'oreficeria."
	msgNientePartito  = " Niente è partito."
	prefissoNonInvito = "Lotto non inviato: "
)

// riquadroInviato è il riquadro #ready-lot-sent dopo l'invio.
type riquadroInviato struct {
	Messaggio, WhatsApp, Scegli, Link string
}

// inviaLotto è l'evento send: «📤 Chiudi e invia».
func (f *facciaWeb) inviaLotto(w http.ResponseWriter, r *http.Request) {
	l, ok := f.lottoDellaRotta(w, r)
	if !ok {
		return
	}
	x, err := f.buste.InviaLotto(r.Context(), l.ID, f.pubblico, contestoDi(r))
	var rifiuto buste.Rifiuto
	var membro *buste.RifiutoMembro
	var modello buste.ErrModelloLotto
	switch {
	case err == nil:
		q := &riquadroInviato{Messaggio: x.Messaggio, Scegli: buste.IndirizzoWaMe("", x.Messaggio), Link: x.Link}
		if x.Numero != "" {
			q.WhatsApp = buste.IndirizzoWaMe(x.Numero, x.Messaggio)
		}
		f.mostraLottoCon(w, r, x.Lotto, "", msgLottoInviato, "", "", q)
	case errors.As(err, &rifiuto), errors.As(err, &membro):
		f.ridisegnaLotto(w, r, l.ID, err.Error(), "", "", "")
	case errors.As(err, &modello):
		f.ridisegnaLotto(w, r, l.ID, "Il testo «"+modello.Famiglia.Etichetta()+"» non è valido: "+modello.Avviso+"."+msgNientePartito, "", "", "")
	case errors.Is(err, buste.ErrSenzaIndirizzoPubblico):
		f.ridisegnaLotto(w, r, l.ID, prefissoNonInvito+f.comeConfigurare()+"."+msgNientePartito, "", "", "")
	default:
		f.registro.Error("lotto non inviato", "errore", err.Error())
		Errore(w, r)
	}
}
