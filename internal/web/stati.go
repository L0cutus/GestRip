package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/L0cutus/gestrip/internal/buste"
)

// Stati, riapertura e attesa dalla scheda (RIP-S-13, RIP-S-14, RIP-S-15), come gli eventi
// transition, reopen e waiting di RepairLive dello standalone (go-baseline), senza LiveView:
//   - POST /repairs/{id}/stato è «Aggiorna stato» (transition[status]): riuscito torna alla scheda
//     con «Stato aggiornato.», rifiutato con «Transizione non consentita.». Anche lo stato che la
//     busta ha già è un rifiuto, come sulla scheda dello standalone (repair_live.ex:212-223 non
//     confronta); il silenzio è solo della lista (workshop_live.ex:222-229, T4.2), che confronterà
//     da sé prima di chiamare buste.CambiaStato;
//   - POST /repairs/{id}/riapri è «Riapri busta» (repair[status], repair[reopen_reason]): riuscita
//     torna alla scheda con «Busta riaperta.»; gli errori dei campi ridisegnano la scheda con il
//     modulo, i valori scritti e gli errori sotto i campi; ogni altro errore torna alla scheda con
//     «Riapertura non riuscita.». La conferma la chiede app.js (data-confirm);
//   - POST /repairs/{id}/attesa è «⏳ Metti in attesa» e «✓ Risposta arrivata» (waiting[azione],
//     waiting[motivo]): torna alla scheda con «Busta messa in attesa risposta cliente.» o «Attesa
//     rimossa.», o con «Azione non valida.».
//
// Come la modifica (slice 033), un id malformato o una busta che non c'è o sta nel cestino portano
// alla home con «Riparazione non trovata.». Dopo il cambio di stato lo standalone apre il popup
// della notifica Telegram (area Telegram) e accoda l'email di pronta (area email): arrivano con le
// loro aree.

const (
	msgStatoAggiornato     = "Stato aggiornato."
	msgTransizione         = "Transizione non consentita."
	msgBustaRiaperta       = "Busta riaperta."
	msgRiaperturaNonRiusci = "Riapertura non riuscita."
	msgInAttesa            = "Busta messa in attesa risposta cliente."
	msgAttesaRimossa       = "Attesa rimossa."
)

// datiRiapertura è il modulo «Riapri la busta» come lo disegna la scheda.
type datiRiapertura struct {
	Stati                     []opzione
	Motivo                    string
	ConMotivo                 bool // il campo ha un valore: Phoenix non scrive value per nil
	ErroriStato, ErroriMotivo []string
}

// conStati aggiunge alla scheda quello che dipende dallo stato: «Cambia stato» con gli stati
// raggiungibili (allowed_transitions/1) e, su una busta chiusa, «Riapri la busta»
// (reopen_statuses/1), con nessuno stato scelto.
func conStati(d *datiScheda, s buste.Scheda) {
	for _, st := range buste.Consentiti(s.Stato, s.CompraOro) {
		d.Transizioni = append(d.Transizioni, opzione{Valore: st, Etichetta: etichettaStato(st)})
	}
	if rr := buste.StatiDiRiapertura(s.Stato, s.CompraOro); len(rr) > 0 {
		r := &datiRiapertura{}
		for _, st := range rr {
			r.Stati = append(r.Stati, opzione{Valore: st, Etichetta: etichettaStato(st)})
		}
		d.Riapertura = r
	}
}

// cambiaStato è POST /repairs/{id}/stato (l'evento transition).
func (f *facciaWeb) cambiaStato(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	}
	stato, _ := ultimo(r, "transition[status]")
	// La busta si legge solo per dire «non trovata» come le altre azioni della scheda; lo stato
	// uguale non si confronta (lo standalone sulla scheda non lo fa): il dominio lo rifiuta.
	switch _, err := f.buste.Scheda(r.Context(), id); {
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	case err != nil:
		f.registro.Error("busta non letta")
		Errore(w, r)
		return
	}
	dove := "/repairs/" + strconv.FormatInt(id, 10)
	if _, err := f.buste.CambiaStato(r.Context(), id, stato, contestoDi(r)); err != nil {
		if !errors.Is(err, buste.ErrTransizione) {
			f.registro.Error("stato della busta non cambiato")
		}
		f.vaiAConErrore(w, r, dove, msgTransizione)
		return
	}
	st := statoDi(r)
	st.dati.Info, st.cambiata = msgStatoAggiornato, true
	f.vaiA(w, r, dove)
}

// riapriBusta è POST /repairs/{id}/riapri (l'evento reopen).
func (f *facciaWeb) riapriBusta(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	}
	stato, conStato := ultimo(r, "repair[status]")
	motivo, conMotivo := ultimo(r, "repair[reopen_reason]")
	dove := "/repairs/" + strconv.FormatInt(id, 10)
	_, err := f.buste.Riapri(r.Context(), id, stato, motivo, contestoDi(r))
	var campi buste.Campi
	switch {
	case err == nil:
		st := statoDi(r)
		st.dati.Info, st.cambiata = msgBustaRiaperta, true
		f.vaiA(w, r, dove)
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
	case errors.As(err, &campi):
		// Come used_input?/1: l'errore di un campo si vede solo se il campo è arrivato.
		s, err := f.buste.Scheda(r.Context(), id)
		if errors.Is(err, buste.ErrBustaNonTrovata) {
			f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
			return
		}
		if err != nil {
			f.registro.Error("busta non letta")
			Errore(w, r)
			return
		}
		d, err := datiDellaScheda(s, titolareDi(r))
		if err != nil {
			f.registro.Error("busta con una data illeggibile")
			Errore(w, r)
			return
		}
		if m := d.Riapertura; m != nil {
			for i := range m.Stati {
				m.Stati[i].Scelta = m.Stati[i].Valore == stato
			}
			m.Motivo, m.ConMotivo = motivo, conMotivo
			if conStato {
				m.ErroriStato = campi["stato"]
			}
			if conMotivo {
				m.ErroriMotivo = campi["motivo"]
			}
		}
		f.mostraCon(w, r, "scheda", http.StatusOK, "", d)
	default:
		f.registro.Error("busta non riaperta")
		f.vaiAConErrore(w, r, dove, msgRiaperturaNonRiusci)
	}
}

// attesaBusta è POST /repairs/{id}/attesa (l'evento waiting).
func (f *facciaWeb) attesaBusta(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	}
	azione, _ := ultimo(r, "waiting[azione]")
	motivo, _ := ultimo(r, "waiting[motivo]")
	dove := "/repairs/" + strconv.FormatInt(id, 10)
	in, err := f.buste.Attesa(r.Context(), id, azione, motivo, contestoDi(r))
	switch {
	case err == nil:
		st := statoDi(r)
		st.dati.Info, st.cambiata = msgAttesaRimossa, true
		if in {
			st.dati.Info = msgInAttesa
		}
		f.vaiA(w, r, dove)
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
	default:
		if !errors.Is(err, buste.ErrAzioneNonValida) {
			f.registro.Error("attesa della busta non cambiata")
		}
		f.vaiAConErrore(w, r, dove, string(buste.ErrAzioneNonValida))
	}
}
