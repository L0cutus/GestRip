package web

import (
	"errors"
	"fmt"
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
//     refresh: la lista resta quella filtrata). «✅ Valutata» del compro oro registra anche l'avviso di
//     ritiro (qr_announce/2, slice 068, RIP-H-26; vedi avvisaDopoValutazione). Una busta
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
	esito := "✓ " + buste.ParolaDelloStato(cambio.Dopo)
	// Il secondo invio di un doppio tocco (la busta era già valutata: niente cambiato) non avvisa di nuovo.
	if azione.Dopo == buste.DopoAvviso && cambio.Prima != cambio.Dopo {
		f.avvisaDopoValutazione(w, r, id, esito, d)
		return
	}
	st := statoDi(r)
	st.dati.Info, st.cambiata = esito, true
	f.vaiA(w, r, d.Chiudi)
}

// aperturaQR è il riquadro #qr-opening: l'avviso di ritiro registrato e il link «↗ Apri WhatsApp», che
// non si apre da solo (un window.open bloccato lascerebbe l'avviso bruciato senza niente da toccare).
type aperturaQR struct {
	rigaQR
	URL string // wa.me, con il numero e il testo registrato
}

// avvisaDopoValutazione è qr_announce/2: lo stato è già avanzato (un avviso che non parte non lo riporta
// indietro). Registrato l'avviso, la pagina della scansione con «✓ valutata», il riquadro «↗ Apri
// WhatsApp» con «👁 Consultazione» e il bip di successo, senza il pannello delle azioni; altrimenti il
// pannello si chiude e la lista dice perché (info o errore come lo standalone), con lo stesso bip: il
// tocco ha fatto avanzare la busta.
func (f *facciaWeb) avvisaDopoValutazione(w http.ResponseWriter, r *http.Request, id int64, esito string, d datiCercaQR) {
	inv, err := f.buste.AnnunciaRitiro(r.Context(), id, f.pubblico, contestoDi(r))
	if err == nil {
		x, err := f.buste.RigaPerQR(r.Context(), id)
		if err == nil {
			d.Apertura = &aperturaQR{rigaQR: rigaDellaScansione(x), URL: inv.URL}
			d.Esito = esitoQROK
			w.Header().Set("Cache-Control", "no-store")
			f.mostraConInfo(w, r, "cercaqr", http.StatusOK, "", esito, d)
			return
		}
		f.registro.Error("busta del pannello QR non letta dopo l'avviso")
	}
	st := statoDi(r)
	var modello buste.ErrModelloRitiro
	switch {
	case err == nil:
		// L'avviso è registrato ma la busta non si rilegge: la lista, senza il link (lo dà la scheda).
		st.dati.Info = esito
	case errors.Is(err, buste.ErrNessunRecapito):
		st.dati.Info = esito + " (no WA, telefono mancante)"
	case errors.Is(err, buste.ErrGiaAvvisata):
		st.dati.Errore = esito + " — già stata avvisata: azzera l'avviso dal dettaglio per rifarlo."
	case errors.Is(err, buste.ErrInLottoAperto):
		st.dati.Errore = esito + " — è in un lotto ancora aperto: l'avviso parte col lotto."
	case errors.As(err, &modello):
		st.dati.Errore = esito + " — testo «" + modello.Famiglia.Etichetta() + "» non valido: " + modello.Avviso + ". Avviso non registrato."
	case errors.Is(err, buste.ErrSenzaIndirizzoPubblico):
		// D-46 estesa (slice 067): cosa configurare, con la frase della D-46.
		st.dati.Errore = esito + " — avviso non registrato: " + f.comeConfigurare() + "."
	default:
		// I rifiuti attesi (una busta cambiata nel frattempo, {telegram_optin} su una busta conto terzi) sono
		// avvisi; il resto è un errore. Solo il tipo, mai il testo dell'errore.
		if errors.Is(err, buste.ErrTelegramSoloCliente) || errors.Is(err, buste.ErrControparteCambiata) {
			f.registro.Warn("avviso di ritiro dal pannello QR non registrato", "tipo", fmt.Sprintf("%T", err))
		} else {
			f.registro.Error("avviso di ritiro dal pannello QR non registrato", "tipo", fmt.Sprintf("%T", err))
		}
		st.dati.Errore = esito + " — avviso non registrato."
	}
	st.cambiata = true
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
