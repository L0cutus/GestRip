package web

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
)

// Le azioni del riquadro di una busta nella lista (slice 056; RIP-H-14, RIP-H-15, RIP-H-16), come gli
// eventi quick-status, transition e toggle_pin di WorkshopLive a go-baseline, senza LiveView: un
// modulo POST con il CSRF per azione, e il campo «lista» con i filtri e la pagina della lista da cui
// si parte (ritornoLista). Il server non segue mai «lista» com'è: la rilegge come i filtri della home
// e ne riscrive l'indirizzo, quindi un valore falsificato porta al più a un'altra lista.
//   - POST /repairs/{id}/rapido è «✓ Pronta» (da in lavorazione) e «✓ Consegnata» (da pronta, con la
//     conferma data-confirm; senza JavaScript, dalla slice 060, la conferma la disegna il server:
//     senza conferma=1 la pagina conferma_consegna, come il pannello del QR): buste.PassoRapido. «✓ Pronta» porta alla scheda con il modulo sul prezzo
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
// Dopo un cambio di stato si apre il popup della notifica Telegram al cliente (RIP-H-18, slice 074,
// telegram_avvisi.go: «✓ Pronta» aspetta la risposta prima di andare al prezzo) e l'email di pronta
// parte nella transazione del cambio (slice 070b), come nella scheda (stati.go).
// Con JavaScript (app.js) il menu parte appena si sceglie, senza il pulsante «Cambia stato», e i moduli
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
	h.AvvisaPer = "Avvisa per la busta " + numero
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
	if conferma, _ := ultimo(r, "conferma"); stato == "consegnata" && conferma != "1" {
		f.chiediConsegna(w, r, id, lista)
		return
	}
	cambio, err := f.buste.PassoRapido(r.Context(), id, stato, contestoDi(r))
	if err != nil {
		if !errors.Is(err, buste.ErrTransizione) {
			f.registro.Error("stato della busta non cambiato dalla lista")
		}
		f.vaiAConErrore(w, r, lista, msgTransizione)
		return
	}
	st := statoDi(r)
	if stato == "pronta" {
		// quick_status/3: con il popup Telegram si resta sulla lista con «Stato aggiornato.», e il prezzo
		// finale aspetta la risposta del popup; senza, subito al prezzo.
		prezzo := "/repairs/" + strconv.FormatInt(id, 10) + "?edit=prezzo"
		f.dopoCambio(r, cambio, false, prezzo)
		if cambio.Telegram == nil {
			f.vaiA(w, r, prezzo)
			return
		}
		st.dati.Info, st.cambiata = msgStatoAggiornato, true
		f.vaiA(w, r, lista)
		return
	}
	st.dati.Info, st.cambiata = msgStatoAggiornato, true
	f.dopoCambio(r, cambio, false, lista)
	f.vaiA(w, r, lista)
}

// datiConfermaConsegna è la pagina conferma_consegna.
type datiConfermaConsegna struct {
	Frase, Azione string
	Lista         string // il campo «lista» del modulo, già riscritto da listaDelModulo
	Ritorno       string // la lista, per «Annulla»
}

// chiediConsegna è la conferma di «✓ Consegnata» senza JavaScript (slice 060, Stefano 09/11/2026),
// come la conferma del pannello del QR: niente si scrive. Solo su una busta che il passo rapido
// accetterebbe adesso (pronta e non Compro Oro); altrimenti la lista con «Transizione non
// consentita.», come il passo rifiutato.
func (f *facciaWeb) chiediConsegna(w http.ResponseWriter, r *http.Request, id int64, lista string) {
	x, err := f.buste.RigaPerQR(r.Context(), id)
	switch {
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, lista, msgTransizione)
		return
	case err != nil:
		f.registro.Error("busta della conferma di consegna non letta")
		f.vaiAConErrore(w, r, lista, msgTransizione)
		return
	case x.Stato != buste.PassiRapidi["consegnata"] || x.CompraOro:
		f.vaiAConErrore(w, r, lista, msgTransizione)
		return
	}
	numero, _ := x.NumeroVisibile()
	f.mostraCon(w, r, "conferma_consegna", http.StatusOK, "", datiConfermaConsegna{
		Frase:  "Segnare la busta " + numero + " come consegnata?",
		Azione: "/repairs/" + strconv.FormatInt(id, 10) + "/rapido",
		Lista:  strings.TrimPrefix(strings.TrimPrefix(lista, "/"), "?"), Ritorno: lista,
	})
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
	f.dopoCambio(r, cambio, false, lista)
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
