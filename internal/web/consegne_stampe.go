package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/printing"
	"github.com/L0cutus/gestrip/internal/stampe"
)

// Le stampe della consegna all'artigiano (slice 078, ART-12):
//   - GET /handovers/{id}/labels.pdf, come HandoverLabelController.show/2 dello standalone, per tutto il
//     personale (nessun controllo di ruolo): una pagina 76×127 per busta (buste.EtichetteDellaConsegna,
//     stampe.EtichetteConsegna), in linea «etichette_consegna_<id>.pdf» e allegato con download non vuoto
//     (comeAllegato, come l'etichetta della busta), «private, no-store»; un id non valido, una consegna che
//     non c'è o vuota: 404 «Consegna vuota o non trovata» (send_resp, senza tipo e con le intestazioni prima del
//     corpo: rispostaSenzaTipo); un PDF non fatto o la
//     riga handover.labels_generated non scritta: 503 «PDF non disponibile». La riga si scrive prima dei byte;
//   - «👁 Anteprima» e, dopo la conferma, «🖨 Ristampa» sono link al PDF in una scheda nuova; «📤 Condividi» è
//     il link al download con data-share-pdf (app.js, slice 046);
//   - POST /handovers/{id}/stampa-conferma, «🖨 Stampa e conferma». Il modulo porta il modo con cui la pagina è
//     stata disegnata (stampante o PDF): se non è più quello di adesso né stampa né conferma. Senza la stampante
//     dell'etichetta busta (Impostazioni → Stampanti, slice 046) è come lo standalone: il pulsante è il link al
//     PDF in una scheda nuova che invia anche il modulo (data-invia-modulo, app.js) e il server conferma come
//     «✓ Conferma senza stampare». Con la stampante (D-09,
//     Stefano 29/10/2026) le etichette vanno a lp con le opzioni dell'etichetta busta, e solo se il lavoro è
//     accettato la consegna si conferma, con handover.labels_printed e handover.confirmed in una
//     transazione; un rifiuto torna alla sessione con il motivo e non conferma. Il lavoro sta fuori dalla
//     transazione e non dipende dalla connessione del browser (context.WithoutCancel); l'invio monouso e il
//     blocco per consegna tengono fuori un secondo lavoro, come «🖨 Stampa etichetta» della scheda, e anche
//     «✓ Conferma senza stampare» finché il lavoro non ha risposto. Una consegna senza buste ancora da timbrare
//     non stampa (review della 078); se un altro conferma mentre lp lavora, il dominio non scrive una seconda
//     conferma da 0 e resta la traccia not_confirmed.

const (
	msgConsegnaVuota          = "Consegna vuota o non trovata"
	msgConsegnaStampata       = "Consegna confermata: %s fuori bottega. Etichette inviate a %s (lavoro %s)."
	msgEtichetteNonStampate   = "Etichette non stampate: %s Consegna non confermata."
	msgEtichetteConsegnaPDF   = "Etichette non stampate: PDF non disponibile. Consegna non confermata."
	msgConsegnaNonConfermata  = "Etichette inviate a %s (lavoro %s), ma la consegna non è stata confermata: %s"
	motivoConfermaSenzaStampa = "usa «✓ Conferma senza stampare»."
	msgConsegnaGiaConfermata  = "Etichette inviate a %s (lavoro %s), ma la consegna era già confermata: nessuna busta da timbrare."
	// Solo Go (review della 078): con la stampante, una consegna senza buste ancora da timbrare non stampa.
	msgNienteDaConfermare = "Nessuna busta da confermare: le buste di questa consegna sono già fuori bottega. Per rifare le etichette usa «🖨 Ristampa»."
	// Solo Go (review della 078): il modo con cui la pagina è stata disegnata non è più quello di adesso.
	msgModoStampaCambiato = "La stampante delle etichette è cambiata: controlla e riprova."
)

// I modi di «🖨 Stampa e conferma», scritti nel modulo con cui la pagina è stata disegnata: con la stampante
// dell'etichetta busta, o il PDF nel browser.
const (
	modoStampante = "stampante"
	modoPDF       = "pdf"
)

// modoStampaConsegna è il modo di adesso: la stampante dell'etichetta busta scelta (e GestRip partito con i
// comandi delle stampanti) o il PDF.
func (f *facciaWeb) modoStampaConsegna(s buste.ScelteStampa) string {
	if f.stampanti != nil && s.Busta.Attiva() {
		return modoStampante
	}
	return modoPDF
}

// chiaveStampaConsegna è il blocco in memoria dei lavori della consegna.
func chiaveStampaConsegna(id int64) string { return "consegna\x00" + strconv.FormatInt(id, 10) }

// etichetteConsegna è GET /handovers/{id}/labels.pdf.
func (f *facciaWeb) etichetteConsegna(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	var pdf []byte
	var err error = buste.ErrConsegnaVuota
	if ok {
		pdf, err = f.buste.EtichetteDellaConsegna(r.Context(), id, contestoFoto(r), stampe.EtichetteConsegna)
	}
	if errors.Is(err, buste.ErrConsegnaVuota) {
		rispostaSenzaTipo(w, http.StatusNotFound, msgConsegnaVuota)
		return
	}
	tipo := "inline"
	if comeAllegato(r.URL.RawQuery) {
		tipo = "attachment"
	}
	f.inviaPDF(w, r, pdf, err, "etichette della consegna", disposizione(tipo, buste.NomeFileEtichetteConsegna(id)))
}

// confermaLaConsegna è la conferma di «✓ Conferma senza stampare» per la consegna c: la pagina rifiuta una
// consegna senza buste, poi il dominio timbra e si va alla lista delle buste fuori bottega.
func (f *facciaWeb) confermaLaConsegna(w http.ResponseWriter, r *http.Request, c buste.ConsegnaArtigiano) {
	// Mentre un lavoro della consegna è in corso, la conferma aspetta il suo esito (review della 078).
	if f.inCorso.tenuta(chiaveStampaConsegna(c.ID)) {
		f.tornaAllaConsegna(w, r, c.ID, "", msgStampaInCorso)
		return
	}
	bb, err := f.buste.BusteDellaConsegna(r.Context(), c.ID)
	if err != nil {
		f.registro.Error("buste della consegna non lette", "errore", err.Error())
		Errore(w, r)
		return
	}
	if len(bb) == 0 {
		f.tornaAllaConsegna(w, r, c.ID, "", msgAggiungiAlmenoUna)
		return
	}
	n, err := f.buste.ConfermaConsegna(r.Context(), c.ID, contestoDi(r))
	var rifiuto buste.Rifiuto
	switch {
	case err == nil:
		st := statoDi(r)
		st.dati.Info, st.cambiata = "Consegna confermata: "+busteFrase(n)+" fuori bottega.", true
		f.vaiA(w, r, rottaFuoriBottega)
	case errors.As(err, &rifiuto):
		f.tornaAllaConsegna(w, r, c.ID, "", err.Error())
	default:
		f.registro.Error("consegna non confermata", "errore", err.Error())
		Errore(w, r)
	}
}

// stampaEConferma è POST /handovers/{id}/stampa-conferma.
func (f *facciaWeb) stampaEConferma(w http.ResponseWriter, r *http.Request) {
	c, ok := f.consegnaDellaRotta(w, r)
	if !ok {
		return
	}
	scelte, err := f.buste.ScelteStampa(r.Context())
	if err != nil {
		f.registro.Error("scelte delle stampanti non lette", "errore", err.Error())
		Errore(w, r)
		return
	}
	modo := f.modoStampaConsegna(scelte)
	if scritto, _ := ultimo(r, "modo"); scritto != modo {
		f.tornaAllaConsegna(w, r, c.ID, "", msgModoStampaCambiato)
		return
	}
	if modo == modoPDF {
		f.confermaLaConsegna(w, r, c)
		return
	}
	primo, esito, fine, err := f.invioStampa(r)
	switch {
	case err != nil:
		f.tornaAllaConsegna(w, r, c.ID, "", msgStampaInCorso)
		return
	case !primo:
		msg, riuscito := leggiEsitoStampa(esito)
		if riuscito {
			st := statoDi(r)
			st.dati.Info, st.cambiata = msg, true
			f.vaiA(w, r, rottaFuoriBottega)
			return
		}
		f.tornaAllaConsegna(w, r, c.ID, "", msg)
		return
	}
	defer fine("")
	bb, err := f.buste.BusteDellaConsegna(r.Context(), c.ID)
	if err != nil {
		f.registro.Error("buste della consegna non lette", "errore", err.Error())
		Errore(w, r)
		return
	}
	daTimbrare := 0
	for _, x := range bb {
		if !x.Uscita {
			daTimbrare++
		}
	}
	// I rifiuti che il dominio darebbe dopo la stampa vengono prima: nessun lavoro per una consegna vuota,
	// chiusa o già confermata (la ristampa è «🖨 Ristampa»).
	switch {
	case len(bb) == 0:
		f.tornaAllaConsegna(w, r, c.ID, "", msgAggiungiAlmenoUna)
		return
	case c.Chiusa:
		f.tornaAllaConsegna(w, r, c.ID, "", buste.ErrConsegnaNonDisponibile.Error())
		return
	case daTimbrare == 0:
		f.tornaAllaConsegna(w, r, c.ID, "", msgNienteDaConfermare)
		return
	}
	chiave := chiaveStampaConsegna(c.ID)
	if !f.inCorso.prendi(chiave) {
		f.tornaAllaConsegna(w, r, c.ID, "", msgStampaInCorso)
		return
	}
	defer f.inCorso.lascia(chiave)
	pdf, stampate, err := f.buste.EtichetteDaStampare(r.Context(), c.ID, stampe.EtichetteConsegna)
	switch {
	case errors.Is(err, buste.ErrConsegnaVuota):
		f.tornaAllaConsegna(w, r, c.ID, "", msgAggiungiAlmenoUna)
		return
	case err != nil:
		f.registro.Error("PDF delle etichette della consegna da stampare non fatto", "errore", err.Error())
		f.tornaAllaConsegna(w, r, c.ID, "", msgEtichetteConsegnaPDF)
		return
	}
	ctx := context.WithoutCancel(r.Context())
	coda := scelte.Busta.Coda
	lavoro, err := f.stampanti.Print(ctx, coda, pdf, printing.Options{LarghezzaMM: stampe.LarghezzaEtichetta,
		AltezzaMM: stampe.AltezzaEtichetta, Extra: opzioniDi(scelte.Busta), Titolo: buste.NomeFileEtichetteConsegna(c.ID)})
	if err != nil {
		f.registro.Warn("etichette della consegna non stampate", "coda", coda, "errore", err.Error())
		msg := fmt.Sprintf(msgEtichetteNonStampate, motivoStampa(err))
		if errors.Is(err, printing.ErrTempoScaduto) {
			fine(esitoStampa(esitoErrore, msg)) // il lavoro potrebbe essere già nella coda
		}
		f.tornaAllaConsegna(w, r, c.ID, "", msg)
		return
	}
	n, err := f.buste.StampaConsegnaAccettata(ctx, c.ID, stampate, coda, string(lavoro), contestoDi(r))
	if err != nil {
		var msg string
		var rifiuto buste.Rifiuto
		switch {
		case errors.Is(err, buste.ErrNienteDaConfermare):
			msg = fmt.Sprintf(msgConsegnaGiaConfermata, coda, lavoro)
		case errors.As(err, &rifiuto):
			msg = fmt.Sprintf(msgConsegnaNonConfermata, coda, lavoro, rifiuto.Error()+".")
		default:
			f.registro.Error("consegna stampata non confermata", "coda", coda, "lavoro", string(lavoro), "errore", err.Error())
			msg = fmt.Sprintf(msgConsegnaNonConfermata, coda, lavoro, motivoConfermaSenzaStampa)
		}
		if err := f.buste.StampaConsegnaNonConfermata(ctx, c.ID, stampate, coda, string(lavoro), contestoDi(r)); err != nil {
			f.registro.Error("traccia della stampa della consegna non scritta", "coda", coda, "lavoro", string(lavoro), "errore", err.Error())
		}
		fine(esitoStampa(esitoAvviso, msg))
		f.tornaAllaConsegna(w, r, c.ID, "", msg)
		return
	}
	msg := fmt.Sprintf(msgConsegnaStampata, busteFrase(n), coda, lavoro)
	fine(esitoStampa(esitoRiuscito, msg))
	st := statoDi(r)
	st.dati.Info, st.cambiata = msg, true
	f.vaiA(w, r, rottaFuoriBottega)
}

// registraStampeConsegne registra le rotte delle stampe della consegna (del personale).
func (f *facciaWeb) registraStampeConsegne(mux *http.ServeMux) {
	f.rotta(mux, "GET /handovers/{id}/labels.pdf", personale, f.etichetteConsegna)
	f.rotta(mux, "POST /handovers/{id}/stampa-conferma", personale, f.stampaEConferma)
}
