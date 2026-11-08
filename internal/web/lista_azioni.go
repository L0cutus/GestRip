package web

import (
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/L0cutus/gestrip/internal/buste"
)

// Le azioni del riquadro di una busta nella lista (slice 056; RIP-H-14, RIP-H-15, RIP-H-16), come gli
// eventi quick-status, transition e toggle_pin di WorkshopLive a go-baseline, senza LiveView: un
// modulo POST con il CSRF per azione, e il campo «lista» con i filtri e la pagina della lista da cui
// si parte (ritornoLista). Il server non segue mai «lista» com'è: la rilegge come i filtri della home
// e ne riscrive l'indirizzo, quindi un valore falsificato porta al più a un'altra lista.
//   - POST /repairs/{id}/rapido è «✓ Pronta» (da in lavorazione) e «✓ Consegnata» (da pronta, con la
//     conferma data-confirm): buste.PassoRapido. «✓ Pronta» porta alla scheda con il modulo sul prezzo
//     (?edit=prezzo, come segnaPronte); «✓ Consegnata» torna alla lista con «Stato aggiornato.». Un
//     passo che non è rapido, una busta cambiata nel frattempo, che non c'è o un id malformato: la
//     lista con «Transizione non consentita.» e niente scritto;
//   - POST /repairs/{id}/stato-lista è il menu dello stato del riquadro: il cambio della scheda
//     (buste.CambiaStatoDallaLista: lo stesso grafo, le stesse date, la stessa riga d'audit). Lo stato
//     che la busta ha già (riletto in transazione) torna alla lista in silenzio, senza audit (la pagina rimandata da un telefono che
//     si riconnette, slice 185 dello standalone); riuscito «Stato aggiornato.», rifiutato (anche un id
//     malformato o una busta che non c'è) «Transizione non consentita.»;
//   - POST /repairs/{id}/pin è il 📌: buste.CambiaFissata, poi la lista in silenzio; una busta che non
//     c'è o un id malformato: «Riparazione non trovata.» e niente scritto.
// Dopo un cambio di stato lo standalone apre il popup della notifica Telegram (RIP-H-18, T4.6, e
// «✓ Pronta» aspetta la risposta prima di andare al prezzo) e accoda l'email di pronta (T4.7): come
// nella scheda (stati.go), arrivano con le loro aree.
// Con JavaScript (app.js) il menu parte appena si sceglie, senza il pulsante «Cambia», e i moduli
// partono con fetch: la lista di ritorno prende il posto di questa allo stesso punto della pagina, con
// il messaggio sospeso in cima allo schermo, come la lista di LiveView che si aggiorna sul posto.

// conAzioni scrive nel riquadro il pulsante rapido e il menu dello stato di una busta in stato, con il
// suo numero visibile (D-64) nella conferma e nell'aria-label del menu.
func conAzioni(h *rigaHome, stato string, compraOro bool, numero string) {
	for arrivo, partenza := range buste.PassiRapidi {
		if stato == partenza && !compraOro {
			h.Rapido = arrivo
		}
	}
	h.ConfermaConsegna = "Segnare la busta " + numero + " come consegnata?"
	h.NumeroStato = "Stato " + numero
	consentiti := buste.Consentiti(stato, compraOro)
	h.StatoFermo = len(consentiti) == 0
	for _, s := range buste.Stati(compraOro) {
		if s == stato || slices.Contains(consentiti, s) {
			h.Stati = append(h.Stati, opzione{Valore: s, Etichetta: etichettaStatoLista(s), Scelta: s == stato})
		}
	}
}

// listaDelModulo è l'indirizzo della lista del campo «lista» del modulo, riscritto da filtri letti
// come quelli della home (ritornoLista); «/» se manca.
func listaDelModulo(r *http.Request) string {
	q, _ := ultimo(r, "lista")
	_, indirizzo := ritornoLista(buste.LeggiFiltri(func(nome string) (string, bool) { return testoPlug(q, nome) }))
	return indirizzo
}

// statoRapido è POST /repairs/{id}/rapido (quick-status).
func (f *facciaWeb) statoRapido(w http.ResponseWriter, r *http.Request) {
	lista := listaDelModulo(r)
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, lista, msgTransizione)
		return
	}
	stato, _ := ultimo(r, "status")
	if _, err := f.buste.PassoRapido(r.Context(), id, stato, contestoDi(r)); err != nil {
		if !errors.Is(err, buste.ErrTransizione) {
			f.registro.Error("stato della busta non cambiato dalla lista")
		}
		f.vaiAConErrore(w, r, lista, msgTransizione)
		return
	}
	if stato == "pronta" {
		f.vaiA(w, r, "/repairs/"+strconv.FormatInt(id, 10)+"?edit=prezzo")
		return
	}
	st := statoDi(r)
	st.dati.Info, st.cambiata = msgStatoAggiornato, true
	f.vaiA(w, r, lista)
}

// statoDallaLista è POST /repairs/{id}/stato-lista (l'evento transition del riquadro).
func (f *facciaWeb) statoDallaLista(w http.ResponseWriter, r *http.Request) {
	lista := listaDelModulo(r)
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, lista, msgTransizione)
		return
	}
	stato, _ := ultimo(r, "status")
	cambio, err := f.buste.CambiaStatoDallaLista(r.Context(), id, stato, contestoDi(r))
	switch {
	case err != nil:
		if !errors.Is(err, buste.ErrTransizione) {
			f.registro.Error("stato della busta non cambiato dalla lista")
		}
		f.vaiAConErrore(w, r, lista, msgTransizione)
		return
	case cambio.Prima == cambio.Dopo: // lo stato che la busta ha già: in silenzio
		f.vaiA(w, r, lista)
		return
	}
	st := statoDi(r)
	st.dati.Info, st.cambiata = msgStatoAggiornato, true
	f.vaiA(w, r, lista)
}

// cambiaPin è POST /repairs/{id}/pin (toggle_pin).
func (f *facciaWeb) cambiaPin(w http.ResponseWriter, r *http.Request) {
	lista := listaDelModulo(r)
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, lista, msgBustaNonTrovata)
		return
	}
	if _, err := f.buste.CambiaFissata(r.Context(), id, contestoDi(r)); err != nil {
		if !errors.Is(err, buste.ErrBustaNonTrovata) {
			f.registro.Error("pin della busta non cambiato")
		}
		f.vaiAConErrore(w, r, lista, msgBustaNonTrovata)
		return
	}
	f.vaiA(w, r, lista)
}
