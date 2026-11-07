package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/L0cutus/gestrip/internal/buste"
)

// Il pannello delle azioni dopo una scansione (slice 054; RIP-H-23, RIP-H-24, RIP-H-25, RIP-H-26),
// come il riquadro #qr-action di WorkshopLive a go-baseline (qr_select/2 e gli eventi qr_advance,
// qr_confirm_yes, qr_confirm_no e qr_estimate), senza LiveView:
//   - la pagina della scansione (POST /cerca-qr) ha già il pannello: «#qr-btn-avanza» con il testo di
//     @qr_actions, «💰 Preventivo» (#qr-btn-prev) per le riparazioni che lo hanno, «👁 Consultazione»;
//     una busta chiusa ha solo il riquadro «ℹ Stato X — non avanzabile.» e «👁 Consultazione»;
//   - i due pulsanti sono un modulo POST con il CSRF, lo stato mostrato (stato_visto, perché «stato» è
//     il filtro della lista) e i filtri della
//     lista; mai il codice letto (D-63);
//   - un secondo invio dello stesso modulo (doppio tocco): app.js lo ferma; se arriva lo stesso, la
//     busta già nello stato che l'azione avrebbe prodotto vale come riuscita (buste.AvanzaDaQR);
//   - la conferma (D-70): con JavaScript app.js la mostra al posto dei pulsanti, senza passare dal
//     server («Sì» manda il modulo con conferma=1, «Annulla» torna ai pulsanti), come la conferma in
//     posto dello standalone (#qr-action-confirm, #qr-btn-si, #qr-btn-no). Senza JavaScript lo stesso
//     pulsante manda il modulo senza conferma e il server risponde con il pannello in conferma; il
//     suo «Annulla» (annulla=1) ridisegna il pannello;
//   - POST /repairs/{id}/qr-avanza è qr_execute/2: «✅ Pronta» porta alla scheda con il modulo sul
//     prezzo (?edit=prezzo); gli altri «✓ <nuovo stato>» e la lista con i filtri di prima (qr_reset e
//     refresh: la lista resta quella filtrata). «✅ Valutata» del compro oro fa lo stesso: l'avviso
//     WhatsApp di ritiro dello standalone (qr_announce/2) arriva con la T4.6 (risposta 10). Una busta
//     cambiata nel frattempo: «Stato cambiato nel frattempo: ricontrolla.» e il pannello della busta
//     com'è adesso; un rifiuto: il pannello con il riquadro del rifiuto e solo «👁 Consultazione»;
//   - POST /repairs/{id}/qr-preventivo è qr_estimate: la scheda con il modulo sul preventivo
//     (?edit=preventivo), o il pannello con il rifiuto.
// Le righe d'audit sono quelle dei pulsanti della scheda (repair.status_changed) con «via»: «qr», e
// repair.returned_from_artisan_auto per il rientro (buste.AvanzaDaQR).
// Slice 055: con JavaScript, sulla lista, questi moduli partono con fetch e la risposta resta sopra la
// lista (app.js, D-70); la pagina di un rifiuto, di una busta cambiata o assente porta l'esito «ko»
// per il suono (mostraQRKO, RIP-H-27), un redirect vuol dire azione riuscita.

const (
	msgQRNonTrovata    = "Nessuna riparazione trovata"
	msgQRNonConsentita = "Transizione non consentita."
)

// pannelloQR è il riquadro #qr-action della busta.
type pannelloQR struct {
	rigaQR
	StatoGrezzo string          // lo stato mostrato, rimandato dal modulo (:expected_status)
	Avviso      string          // il riquadro #qr-action-notice; con l'avviso solo «👁 Consultazione»
	Avanza      *buste.AzioneQR // #qr-btn-avanza, se c'è
	Preventivo  bool            // #qr-btn-prev
	Chiede      bool            // la conferma dal server (senza JavaScript): solo «Sì» e «Annulla»
}

// pannelloDi è qr_select/2 per una busta: l'avviso della busta, o avviso se non è vuoto (il rifiuto di
// un'azione), oppure i pulsanti.
func pannelloDi(x buste.RigaQR, avviso string) *pannelloQR {
	p := &pannelloQR{rigaQR: rigaDellaScansione(x), StatoGrezzo: x.Stato, Avviso: avviso}
	if p.Avviso == "" {
		p.Avviso = x.AvvisoDelPannello()
	}
	if p.Avviso == "" {
		if a, ok := x.Azione(); ok {
			p.Avanza, p.Preventivo = &a, x.ConPreventivo()
		}
	}
	return p
}

// rifiutoQR è qr_error/1: le parole del rifiuto, «Transizione non consentita.» per tutto il resto
// (anche un guasto, che va nelle righe del server).
func (f *facciaWeb) rifiutoQR(err error) string {
	var (
		chiusa buste.ErrNonAvanzabile
		oro    buste.ErrOroNonGestito
	)
	switch {
	case errors.As(err, &chiusa), errors.As(err, &oro), errors.Is(err, buste.ErrPreventivoOro), errors.Is(err, buste.ErrTransizione):
		return err.Error()
	case errors.Is(err, buste.ErrBustaNonTrovata):
		return msgQRNonTrovata
	}
	f.registro.Error("azione del pannello QR non riuscita")
	return msgQRNonConsentita
}

// rigaDelPannello legge la busta del percorso per il pannello. false: la risposta è già scritta
// (una busta che non c'è dice «Nessuna riparazione trovata» nella pagina della scansione).
func (f *facciaWeb) rigaDelPannello(w http.ResponseWriter, r *http.Request, d datiCercaQR) (int64, buste.RigaQR, bool) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		d.Errore = msgQRNonTrovata
		f.mostraQRKO(w, r, "", d)
		return 0, buste.RigaQR{}, false
	}
	x, err := f.buste.RigaPerQR(r.Context(), id)
	switch {
	case errors.Is(err, buste.ErrBustaNonTrovata):
		d.Errore = msgQRNonTrovata
		f.mostraQRKO(w, r, "", d)
		return 0, buste.RigaQR{}, false
	case err != nil:
		f.registro.Error("busta del pannello QR non letta")
		Errore(w, r)
		return 0, buste.RigaQR{}, false
	}
	return id, x, true
}

// mostraQRKO scrive la pagina della scansione dopo un'azione che non è andata: busta assente, cambiata
// o rifiutata, con l'esito che suona il fallimento (qr_outcome(false) dello standalone, RIP-H-27).
func (f *facciaWeb) mostraQRKO(w http.ResponseWriter, r *http.Request, errore string, d datiCercaQR) {
	d.Esito = esitoQRKO
	f.mostraQR(w, r, http.StatusOK, errore, d)
}

// qrAvanza è POST /repairs/{id}/qr-avanza: qr_advance, qr_confirm_yes e qr_confirm_no.
func (f *facciaWeb) qrAvanza(w http.ResponseWriter, r *http.Request) {
	valore := valoreDelModulo(r)
	d, ok := f.paginaQR(w, r, buste.LeggiFiltri(valore))
	if !ok {
		return
	}
	id, x, ok := f.rigaDelPannello(w, r, d)
	if !ok {
		return
	}
	visto, _ := valore("stato_visto")
	conferma, _ := valore("conferma")
	annulla, _ := valore("annulla")
	// La conferma prima di tutto, se la busta è ancora come la mostrava il pannello: senza conferma=1
	// niente si scrive. Una busta cambiata o chiusa va al dominio, che la rifiuta senza scrivere.
	if a, ok := x.Azione(); ok && x.Stato == visto && a.Conferma != "" && conferma != "1" {
		d.Azione = pannelloDi(x, "")
		d.Azione.Chiede = annulla != "1"
		f.mostraQR(w, r, http.StatusOK, "", d)
		return
	}
	// Cosa segue lo dice l'azione dello stato mostrato: anche il secondo invio di un doppio tocco, che
	// trova la busta già avanzata, va dove sarebbe andato il primo (review della 054, punto M1).
	azione, _ := buste.AzioneDiStato(visto, x.CompraOro)
	cambio, err := f.buste.AvanzaDaQR(r.Context(), id, visto, contestoDi(r))
	var cambiata buste.ErrStatoCambiato
	switch {
	case errors.As(err, &cambiata):
		// qr_select con la busta riletta: i pulsanti veri del suo stato di adesso.
		ora, err := f.buste.RigaPerQR(r.Context(), id)
		if err != nil {
			d.Errore = f.rifiutoQR(err)
			f.mostraQRKO(w, r, "", d)
			return
		}
		d.Azione = pannelloDi(ora, "")
		f.mostraQRKO(w, r, cambiata.Error(), d)
		return
	case errors.Is(err, buste.ErrBustaNonTrovata):
		d.Errore = msgQRNonTrovata
		f.mostraQRKO(w, r, "", d)
		return
	case err != nil:
		d.Azione = pannelloDi(x, f.rifiutoQR(err))
		f.mostraQRKO(w, r, "", d)
		return
	}
	if azione.Dopo == buste.DopoPrezzo {
		f.vaiA(w, r, "/repairs/"+strconv.FormatInt(id, 10)+"?edit=prezzo")
		return
	}
	st := statoDi(r)
	st.dati.Info, st.cambiata = "✓ "+buste.ParolaDelloStato(cambio.Dopo), true
	f.vaiA(w, r, d.Chiudi)
}

// qrPreventivo è POST /repairs/{id}/qr-preventivo: qr_estimate.
func (f *facciaWeb) qrPreventivo(w http.ResponseWriter, r *http.Request) {
	d, ok := f.paginaQR(w, r, buste.LeggiFiltri(valoreDelModulo(r)))
	if !ok {
		return
	}
	id, x, ok := f.rigaDelPannello(w, r, d)
	if !ok {
		return
	}
	if _, err := f.buste.PreventivoDaQR(r.Context(), id, contestoDi(r)); err != nil {
		if errors.Is(err, buste.ErrBustaNonTrovata) {
			d.Errore = msgQRNonTrovata
		} else {
			d.Azione = pannelloDi(x, f.rifiutoQR(err))
		}
		f.mostraQRKO(w, r, "", d)
		return
	}
	f.vaiA(w, r, "/repairs/"+strconv.FormatInt(id, 10)+"?edit=preventivo")
}
