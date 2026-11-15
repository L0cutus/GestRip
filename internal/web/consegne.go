package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// Le pagine delle consegne all'artigiano (slice 077, ART-05…08, 10, 11), come HandoverLive (/handovers) e
// HandoverSessionLive (/handovers/:id) dello standalone, per tutto il personale. Senza LiveView gli eventi
// open, add, remove e confirm sono moduli POST:
//   - «Apri sessione»: riuscita → la pagina della consegna (push_navigate); rifiuto → la lista ridisegnata
//     con il messaggio e la scelta tenuta (LiveView non tocca il menu);
//   - «Aggiungi»: riuscita (anche «già in lista») → ritorno alla pagina con il messaggio e il campo vuoto;
//     rifiuto → la pagina ridisegnata con il messaggio e il codice scritto nel campo (assign_form(code));
//   - ✕ e «✓ Conferma senza stampare»: sempre un redirect, con il messaggio.
// Solo la GET della lista pulisce le consegne vuote dei giorni prima (mount di HandoverLive, una volta per
// caricamento), non il ridisegno dopo un rifiuto. Dopo «Aggiungi» app.js rimette il cursore nel campo del
// numero, come LiveView che non ricarica la pagina (lettore a pistola).
// Le stampe della consegna (👁 Anteprima, 🖨 Stampa e conferma, Ristampa, Condividi) sono della 078
// (consegne_stampe.go), lo scanner continuo con la fotocamera della 079 (consegne_scansione.go); i link a storico
// e solleciti arrivano quando le loro pagine ci sono (082, 083).

const (
	msgSelezionaArtigiano  = "Seleziona un artigiano."
	msgArtigianoNonValido  = "Artigiano non valido."
	msgSessioneNonAperta   = "Impossibile aprire la sessione."
	msgBustaRimossa        = "Busta rimossa dalla consegna."
	msgAggiungiAlmenoUna   = "Aggiungi almeno una busta."
	rottaFuoriBottega      = "/?fuori_bottega=1"
	rottaPaginaConsegne    = "/handovers"
	lunghezzaDescrizione   = 65
	troncaturaDescrizione  = 59
	rottaStoricoConsegne   = "GET /handovers/history"
	rottaSollecitiConsegne = "GET /handovers/reminders"
)

// voceArtigiano è un'opzione del menu della nuova consegna.
type voceArtigiano struct {
	ID        int64
	Etichetta string
	Scelto    bool
}

// rigaInRaccolta è una riga di «Sessioni aperte oggi».
type rigaInRaccolta struct {
	ID             int64
	Artigiano, Ora string
	Buste          string
}

type datiConsegne struct {
	InRaccolta         []rigaInRaccolta
	Artigiani          []voceArtigiano
	Storico, Solleciti bool
}

// rigaBusta è una busta della pagina della consegna.
type rigaBusta struct {
	ID                  int64
	Numero, Descrizione string
}

type datiConsegna struct {
	ID         int64
	Artigiano  string
	Aperta     string
	Confermata bool
	Scritto    string // il valore del campo «Numero busta»
	Buste      []rigaBusta
	// Modo è il modo di «🖨 Stampa e conferma» con cui la pagina è disegnata (078): modoStampante o modoPDF;
	// InvioStampa è il suo invio monouso, solo con la stampante.
	Modo, InvioStampa string
}

func (d datiConsegna) titoloPagina() string { return "Consegna a " + d.Artigiano }

// busteFrase è bags_sentence/1: «1 busta», «N buste».
func busteFrase(n int) string {
	if n == 1 {
		return "1 busta"
	}
	return strconv.Itoa(n) + " buste"
}

// troncaDescrizione è truncate/1 della pagina (truncate(60, True, '…') della fonte): fino a 65 grafemi
// com'è, altrimenti i primi 59 e «…».
func troncaDescrizione(s string) string {
	if len(buste.Grafemi(s, lunghezzaDescrizione+1)) <= lunghezzaDescrizione {
		return s
	}
	return strings.Join(buste.Grafemi(s, troncaturaDescrizione), "") + "…"
}

// mostraConsegne disegna la lista con le sessioni di oggi e il menu; scelto è la scelta da tenere.
func (f *facciaWeb) mostraConsegne(w http.ResponseWriter, r *http.Request, errore, scelto string) {
	aa, err := f.buste.ArtigianiDelMenu(r.Context())
	if err != nil {
		f.registro.Error("artigiani delle consegne non letti", "errore", err.Error())
		Errore(w, r)
		return
	}
	cc, err := f.buste.ConsegneInRaccolta(r.Context())
	if err != nil {
		f.registro.Error("consegne in raccolta non lette", "errore", err.Error())
		Errore(w, r)
		return
	}
	d := datiConsegne{Storico: f.haRotta(rottaStoricoConsegne), Solleciti: f.haRotta(rottaSollecitiConsegne)}
	for _, c := range cc {
		d.InRaccolta = append(d.InRaccolta, rigaInRaccolta{ID: c.ID, Artigiano: c.Artigiano, Buste: busteFrase(c.Buste), Ora: clock.Formatta(c.Aperta, "15:04")})
	}
	for _, a := range aa {
		v := voceArtigiano{ID: a.ID, Etichetta: a.Nome, Scelto: scelto == strconv.FormatInt(a.ID, 10)}
		if a.Aperte > 0 {
			v.Etichetta += " (sessione aperta)"
		}
		d.Artigiani = append(d.Artigiani, v)
	}
	f.mostraCon(w, r, "consegne", http.StatusOK, errore, d)
}

// paginaConsegne è GET /handovers (mount di HandoverLive): prima la pulizia delle consegne vuote dei giorni
// prima, poi la pagina.
func (f *facciaWeb) paginaConsegne(w http.ResponseWriter, r *http.Request) {
	if _, err := f.buste.PulisciConsegneVuote(r.Context()); err != nil {
		f.registro.Error("consegne vuote non pulite", "errore", err.Error())
		Errore(w, r)
		return
	}
	f.mostraConsegne(w, r, "", "")
}

// artigianoScelto è artisan_param/1: handover[artisan_id] senza spazi ai bordi (l'ultimo valore, come Plug);
// "" se manca o se handover non è la mappa (un «handover» o «handover[]» che Plug farebbe testo o lista).
func artigianoScelto(r *http.Request) string {
	for k := range r.PostForm {
		if k == "handover" || k == "handover[]" {
			return ""
		}
	}
	v, _ := ultimo(r, "handover[artisan_id]")
	return strings.TrimSpace(v)
}

// apriConsegna è l'evento open: i tre rifiuti della pagina, nell'ordine, e quelli del dominio.
func (f *facciaWeb) apriConsegna(w http.ResponseWriter, r *http.Request) {
	scelto := artigianoScelto(r)
	if scelto == "" {
		f.mostraConsegne(w, r, msgSelezionaArtigiano, scelto)
		return
	}
	artigiano, ok := idPositivo(scelto)
	if !ok {
		f.mostraConsegne(w, r, msgArtigianoNonValido, scelto)
		return
	}
	id, err := f.buste.ApriConsegna(r.Context(), artigiano)
	switch {
	case err == nil:
		f.vaiA(w, r, percorsoDellaConsegna(id))
	case errors.Is(err, buste.ErrArtigianoNonTrovato):
		f.mostraConsegne(w, r, err.Error(), scelto)
	default:
		f.registro.Error("consegna non aperta", "errore", err.Error())
		f.mostraConsegne(w, r, msgSessioneNonAperta, scelto)
	}
}

func percorsoDellaConsegna(id int64) string {
	return rottaPaginaConsegne + "/" + strconv.FormatInt(id, 10)
}

// consegnaDellaRotta legge la consegna di {id}; se l'id non è valido o la consegna non c'è torna alla lista
// con «Consegna non trovata.» (mount di HandoverSessionLive) e dà false.
func (f *facciaWeb) consegnaDellaRotta(w http.ResponseWriter, r *http.Request) (buste.ConsegnaArtigiano, bool) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, rottaPaginaConsegne, buste.ErrConsegnaNonTrovata.Error())
		return buste.ConsegnaArtigiano{}, false
	}
	c, err := f.buste.ConsegnaArtigiano(r.Context(), id)
	switch {
	case errors.Is(err, buste.ErrConsegnaNonTrovata):
		f.vaiAConErrore(w, r, rottaPaginaConsegne, err.Error())
		return c, false
	case err != nil:
		f.registro.Error("consegna non letta", "errore", err.Error())
		Errore(w, r)
		return c, false
	}
	return c, true
}

// mostraConsegna disegna la pagina della consegna con le sue buste, l'errore e il valore del campo.
func (f *facciaWeb) mostraConsegna(w http.ResponseWriter, r *http.Request, c buste.ConsegnaArtigiano, errore, scritto string) {
	bb, err := f.buste.BusteDellaConsegna(r.Context(), c.ID)
	if err != nil {
		f.registro.Error("buste della consegna non lette", "errore", err.Error())
		Errore(w, r)
		return
	}
	d := datiConsegna{ID: c.ID, Artigiano: c.Artigiano, Aperta: clock.Timbro(c.Aperta), Scritto: scritto}
	for _, x := range bb {
		d.Confermata = d.Confermata || x.Uscita
		d.Buste = append(d.Buste, rigaBusta{ID: x.ID, Numero: x.Visibile().Titolo(), Descrizione: troncaDescrizione(x.Descrizione)})
	}
	if !d.Confermata {
		scelte, err := f.buste.ScelteStampa(r.Context())
		if err != nil {
			f.registro.Error("scelte delle stampanti non lette", "errore", err.Error())
			Errore(w, r)
			return
		}
		if d.Modo = f.modoStampaConsegna(scelte); d.Modo == modoStampante {
			d.InvioStampa = casuale()
		}
	}
	f.mostraCon(w, r, "consegna", http.StatusOK, errore, d)
}

// paginaConsegna è GET /handovers/{id}.
func (f *facciaWeb) paginaConsegna(w http.ResponseWriter, r *http.Request) {
	if c, ok := f.consegnaDellaRotta(w, r); ok {
		f.mostraConsegna(w, r, c, "", "")
	}
}

// tornaAllaConsegna è il ritorno alla pagina della consegna con un messaggio di esito o di errore.
func (f *facciaWeb) tornaAllaConsegna(w http.ResponseWriter, r *http.Request, id int64, info, errore string) {
	st := statoDi(r)
	st.dati.Info, st.dati.Errore, st.cambiata = info, errore, true
	f.vaiA(w, r, percorsoDellaConsegna(id))
}

// codiceScritto è bag_param/1: bag[numero_busta] (l'ultimo valore), "" se manca o se bag non è la mappa.
func codiceScritto(r *http.Request) string {
	for k := range r.PostForm {
		if k == "bag" || k == "bag[]" {
			return ""
		}
	}
	v, _ := ultimo(r, "bag[numero_busta]")
	return v
}

// aggiungiBusta è l'evento add: il campo scritto, prima come numero dell'etichetta e poi come QR.
func (f *facciaWeb) aggiungiBusta(w http.ResponseWriter, r *http.Request) {
	c, ok := f.consegnaDellaRotta(w, r)
	if !ok {
		return
	}
	codice := strings.TrimSpace(codiceScritto(r))
	a, err := f.buste.AggiungiAllaConsegna(r.Context(), c.ID, codice, buste.LetturaScritta)
	var rifiuto *buste.RifiutoAggiunta
	switch {
	case err == nil && a.GiaInLista:
		f.tornaAllaConsegna(w, r, c.ID, "↻ Busta "+a.Busta.Visibile().Titolo()+" già in lista", "")
	case err == nil:
		f.tornaAllaConsegna(w, r, c.ID, "✓ Aggiunta busta "+a.Busta.Visibile().Titolo(), "")
	case errors.As(err, &rifiuto):
		f.mostraConsegna(w, r, c, err.Error(), codice)
	default:
		f.registro.Error("busta non aggiunta alla consegna", "errore", err.Error())
		Errore(w, r)
	}
}

// togliBusta è l'evento remove: sempre «Busta rimossa dalla consegna.», anche se niente cambia; un id di
// busta non valido torna senza messaggio.
func (f *facciaWeb) togliBusta(w http.ResponseWriter, r *http.Request) {
	c, ok := f.consegnaDellaRotta(w, r)
	if !ok {
		return
	}
	busta, ok := idPositivo(r.PathValue("busta"))
	if !ok {
		f.vaiA(w, r, percorsoDellaConsegna(c.ID))
		return
	}
	if err := f.buste.TogliDallaConsegna(r.Context(), c.ID, busta); err != nil {
		f.registro.Error("busta non tolta dalla consegna", "errore", err.Error())
		Errore(w, r)
		return
	}
	f.tornaAllaConsegna(w, r, c.ID, msgBustaRimossa, "")
}

// confermaConsegna è l'evento confirm (confermaLaConsegna, consegne_stampe.go).
func (f *facciaWeb) confermaConsegna(w http.ResponseWriter, r *http.Request) {
	if c, ok := f.consegnaDellaRotta(w, r); ok {
		f.confermaLaConsegna(w, r, c)
	}
}

// registraConsegne registra le rotte delle consegne (tutte del personale, come le due live dello
// standalone).
func (f *facciaWeb) registraConsegne(mux *http.ServeMux) {
	f.rottaConGuscio(mux, "GET /handovers", personale, f.paginaConsegne)
	f.rottaConGuscio(mux, "POST /handovers", personale, f.apriConsegna) // ridisegna dopo un rifiuto
	f.rottaConGuscio(mux, "GET /handovers/{id}", personale, f.paginaConsegna)
	f.rottaConGuscio(mux, "POST /handovers/{id}/aggiungi", personale, f.aggiungiBusta) // ridisegna dopo un rifiuto
	f.rotta(mux, "POST /handovers/{id}/buste/{busta}/togli", personale, f.togliBusta)
	f.rotta(mux, "POST /handovers/{id}/conferma", personale, f.confermaConsegna)
	f.rotta(mux, "POST /handovers/{id}/scansione", personale, f.scansionaBusta) // lo scanner continuo (079)
	f.registraStampeConsegne(mux)
}
