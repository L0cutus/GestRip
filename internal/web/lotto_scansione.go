package web

import (
	"errors"
	"net/http"

	"github.com/L0cutus/gestrip/internal/buste"
)

// Lo scanner continuo del lotto d'avviso (slice 090, ORE-07): «📷 Scansiona» della pagina del lotto apre il lettore QR
// di app.js nel modo continuo (lo stesso della consegna, 079: nessun'altra finestra, nessun secondo caricamento di
// jsQR) e ogni codice letto arriva qui, come l'evento scan_continuous di ReadyLotLive (slice 200 dello standalone).
// Si prova prima come QR e poi come numero (scan_add): solo «Riparazione non trovata.» passa alla seconda lettura, ogni
// altro rifiuto è la risposta («già avvisata» vince, come nel campo; D-95 per il numero che vale per più buste). La
// risposta è la reply dello standalone, {ok, message, count}, con info in più (esitoDellaScansione, come la consegna):
//   - ok solo per una busta appena aggiunta (il suono di successo): «✓ Aggiunta busta <numero visibile>» (D-64), info vero;
//   - «Busta già in questo lotto.» è «↻ Busta già in questo lotto» con ok falso, e come lo standalone un errore: nello
//     scan_continuous il flash è sempre rosso (nel campo è un esito), quindi info falso e alla chiusura della finestra
//     la pagina lo mostra come ultimo errore;
//   - un codice di nessuna busta «Riparazione non trovata. («X»)» con il codice; ogni altro rifiuto il testo del campo
//     senza il codice;
//   - count: le buste del lotto rilette adesso (anche dopo un rifiuto, come load_bags).
// Il lotto che non c'è o già uscito è il rifiuto del dominio «Lotto non disponibile.», con il conto delle sue buste. Nessun
// messaggio nella sessione (la finestra copre la pagina: lo mette app.js quando la rilegge, alla chiusura: la lista e il conto
// sotto la finestra si aggiornano solo allora, a differenza della LiveView che li ridisegna a ogni scansione, D-98). L'aggiunta è
// quella del campo (AggiungiAlLotto), con la sua riga ready_lot.bag_added e l'utente. I campi «Numero busta» e del QR
// non si toccano: la pagina li tiene.

// scansionaNelLotto è l'evento scan_continuous. Un id che non è un numero positivo è 404 (la pagina non lo manda mai).
func (f *facciaWeb) scansionaNelLotto(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	codice := codiceLetto(r)
	x, err := f.buste.AggiungiAlLotto(r.Context(), id, buste.RichiestaLotto{QR: codice}, contestoDi(r))
	if errors.Is(err, buste.ErrLottoBustaNonTrovata) {
		x, err = f.buste.AggiungiAlLotto(r.Context(), id, buste.RichiestaLotto{Numero: codice}, contestoDi(r))
	}
	var e esitoDellaScansione
	var rifiuto buste.Rifiuto
	var altro *buste.InAltroLotto // non è un Rifiuto; *RifiutoPiuBuste sì (Unwrap: ErrPiuBuste)
	switch {
	case err == nil:
		e = esitoDellaScansione{Ok: true, Message: "✓ Aggiunta busta " + x.Visibile().Titolo(), Info: true}
	case errors.Is(err, buste.ErrGiaInQuestoLotto):
		e = esitoDellaScansione{Message: "↻ Busta già in questo lotto"}
	case errors.Is(err, buste.ErrLottoBustaNonTrovata):
		e = esitoDellaScansione{Message: err.Error() + " («" + codice + "»)"}
	case errors.As(err, &rifiuto), errors.As(err, &altro):
		e = esitoDellaScansione{Message: err.Error()}
	default:
		f.registro.Error("busta scansionata non aggiunta al lotto", "errore", err.Error())
		Errore(w, r)
		return
	}
	bb, err := f.buste.BusteDelLotto(r.Context(), id)
	if err != nil {
		f.registro.Error("buste del lotto non lette", "errore", err.Error())
		Errore(w, r)
		return
	}
	e.Count = len(bb)
	f.rispondiEsito(w, r, e)
}
