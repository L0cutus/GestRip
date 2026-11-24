package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/L0cutus/gestrip/internal/buste"
)

// «🎙️ Escludi dalla raccolta dettati» sulla scheda (slice 115, RIP-S-26), come la sezione #repair-training e l'evento
// training-exclude-customer di RepairLive dello standalone a go-baseline, senza LiveView:
//   - la sezione c'è solo per il titolare e solo se la raccolta è stata accesa almeno una volta (assign_training_offer/1):
//     il pulsante con la conferma dello standalone (data-confirm, chiesta da app.js come gli altri pulsanti della
//     scheda), o per un cliente già escluso la riga «🎙️ Cliente escluso dalla raccolta dettati (opposizione).»;
//   - POST /repairs/{id}/escludi-raccolta è del titolare dentro la scheda (azioneDelTitolare): il dipendente torna alla
//     scheda con «Solo il titolare può escludere un cliente.»; il ruolo è riletto a ogni richiesta, quindi anche un
//     titolare declassato a pagina aperta è rifiutato;
//   - la busta è quella della scheda (buste.EscludiClienteDellaBusta): niente numero da scrivere, niente gemelle;
//   - riuscita, torna alla scheda con il messaggio dello standalone e il numero dei dettati cancellati; con la raccolta
//     mai accesa o il cliente già escluso (il secondo tocco) torna alla scheda senza messaggio e niente si scrive, come
//     l'evento ignorato dello standalone; una busta che non c'è o nel cestino porta alla home con «Riparazione non
//     trovata.», come le altre azioni della scheda; un guasto torna alla scheda con «Esclusione non riuscita.» e una riga
//     fissa nel registro, senza numeri né testo dell'errore.

const (
	confermaEscludiRaccolta  = "Escludere questo cliente dalla raccolta dei dettati? I dettati di tutte le sue buste vengono cancellati e le sue buste future non saranno raccolte."
	msgSoloTitolareRaccolta  = "Solo il titolare può escludere un cliente."
	msgEsclusioneNonRiuscita = "Esclusione non riuscita."
)

// offertaRaccolta è training_offer: Mostra false è :none, Escluso è :excluded, altrimenti :offer.
type offertaRaccolta struct {
	Mostra, Escluso bool
	Conferma        string
}

// offertaDellaRaccolta è assign_training_offer/1: niente al dipendente; il cliente escluso prima della raccolta avviata,
// come lo standalone.
func offertaDellaRaccolta(s buste.Scheda, titolare bool) offertaRaccolta {
	switch {
	case !titolare:
		return offertaRaccolta{}
	case s.ClienteEscluso:
		return offertaRaccolta{Mostra: true, Escluso: true}
	case s.RaccoltaAvviata:
		return offertaRaccolta{Mostra: true, Conferma: confermaEscludiRaccolta}
	}
	return offertaRaccolta{}
}

// escludiClienteDallaScheda è POST /repairs/{id}/escludi-raccolta (l'evento training-exclude-customer).
func (f *facciaWeb) escludiClienteDallaScheda(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	}
	conSegno := f.segnoDelModulo(r, id)
	n, fatto, err := f.buste.EscludiClienteDellaBusta(r.Context(), id, contestoDi(r))
	switch {
	case err == nil && fatto:
		st := statoDi(r)
		st.dati.Info, st.cambiata = "Cliente escluso dalla raccolta dettati: cancellati "+strconv.FormatInt(n, 10)+
			" dettati; le sue buste future non saranno raccolte.", true
		f.vaiA(w, r, f.allaScheda(r, id, ""))
	case err == nil:
		f.vaiA(w, r, f.dopoRifiuto(r, id, conSegno))
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
	default:
		f.registro.Error("esclusione dalla raccolta dei dettati non riuscita")
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgEsclusioneNonRiuscita)
	}
}
