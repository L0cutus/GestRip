package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// Lo storico delle consegne all'artigiano (slice 082, ART-15…18), come HandoverHistoryLive (/handovers/history,
// /handovers/history/:id) e HandoverHistoryController dello standalone (302439d):
//   - l'elenco (personale): un modulo GET con i filtri (artigiano, stato, date d'apertura), le consegne dalla
//     più recente, 30 per pagina con «← Precedenti» e «Successive →» che portano i filtri; ogni visita scrive
//     handover.history_viewed con i soli nomi dei filtri usati (se non si scrive, niente pagina);
//   - il dettaglio (personale): la riga handover.history_detail_viewed prima della lettura, per ogni id
//     leggibile (anche uno che non c'è); illeggibile o che non c'è: all'elenco con «Consegna non trovata.»;
//   - «✕ Elimina dallo storico» e «✕ Sgancia» del titolare, moduli POST con la domanda dello standalone
//     (data-confirm) e data-invio-unico; le rotte sono soloTitolare come :require_owner_user (il dipendente va
//     a / con «Operazione riservata al titolare.»), i pulsanti si disegnano solo al titolare e solo su una
//     consegna chiusa. Dopo l'eliminazione all'elenco, dopo lo sgancio al dettaglio, con i messaggi dello
//     standalone.
// Il dettaglio si registra come GET /handovers/{storico}/{id} (rottaDettaglioStorico): il mux di Go rifiuta
// insieme /handovers/history/{id} e /handovers/{id}/labels.pdf (tutti e due prendono
// /handovers/history/labels.pdf e nessuno è più preciso). Così /handovers/history/labels.pdf resta il PDF della
// consegna «history» (il 404 di una consegna vuota), come nello standalone dove HandoverLabelController viene
// prima nel router, e un primo segmento che non è «history» è il 404 di una rotta che non c'è, prima della catena
// della rotta (rottaConGuscioSe: anche chi non è entrato ha il 404, non /login).

const (
	rottaStorico            = "/handovers/history"
	rottaDettaglioStorico   = "GET /handovers/{storico}/{id}"
	msgBustaSganciata       = "Busta sganciata dalla consegna."
	msgIDBustaNonValido     = "ID busta non valido."
	lunghezzaDescrStorico   = 55
	troncaturaDescrStorico  = 49
	domandaEliminaStorico   = "Eliminare questa consegna dallo storico? Le %d buste verranno sganciate ma restano in archivio."
	rottaPaginaSolleciti    = "GET /handovers/reminders"
	segnoSconosciutoStorico = "—"
)

// rigaStoricoVista è una consegna dell'elenco come la disegna la pagina.
type rigaStoricoVista struct {
	ID                      int64
	Giorno, Artigiano       string
	Stato, Classe           string // «chiusa», «in corso», «in raccolta» e il segno per il colore
	Buste, Chiusa           string // «N buste»; il giorno della chiusura, "" se aperta
	Fuori                   int
	Elimina, DomandaElimina string // il modulo «✕ Elimina dallo storico», "" se non c'è
}

type datiStorico struct {
	Artigiani              []voceArtigiano
	Stato, DataDa, DataA   string
	Righe                  []rigaStoricoVista
	Pagina, Pagine         int64
	Precedenti, Successive string
	Solleciti              bool
}

// bustaStoricoVista è una busta del dettaglio (o una riaperta).
type bustaStoricoVista struct {
	ID                         int64
	Numero, Stato, Descrizione string
	Uscita, Rientro            string
	Note                       *string
	Nota                       bool // riaperta: «📝 nota registrata»
}

type datiDettaglioStorico struct {
	ID                int64
	Artigiano, Aperta string
	Chiusa            string // "" se aperta
	Correzioni        bool   // il titolare su una consegna chiusa: «✕ Elimina dallo storico» e «✕ Sgancia»
	DomandaElimina    string
	Buste, Riaperte   []bustaStoricoVista
}

func (d datiDettaglioStorico) titoloPagina() string {
	return "Consegna #" + strconv.FormatInt(d.ID, 10)
}

// troncaStorico è truncate/1 del dettaglio (truncate(50, True, '…') della fonte): fino a 55 grafemi com'è,
// altrimenti i primi 49 e «…».
func troncaStorico(s string) string {
	if len(buste.Grafemi(s, lunghezzaDescrStorico+1)) <= lunghezzaDescrStorico {
		return s
	}
	return strings.Join(buste.Grafemi(s, troncaturaDescrStorico), "") + "…"
}

func timbroODash(t *time.Time) string {
	if t == nil {
		return segnoSconosciutoStorico
	}
	return clock.Timbro(*t)
}

// statoDellaRiga è state_word/1 e state_class/1.
func statoDellaRiga(r buste.RigaStorico) (string, string) {
	switch {
	case r.Chiusa != nil:
		return "chiusa", "chiusa"
	case r.Fuori > 0:
		return "in corso", "corso"
	}
	return "in raccolta", "raccolta"
}

// indirizzoPaginaStorico è page_query/2: i filtri non vuoti nell'ordine dello standalone, poi la pagina.
func indirizzoPaginaStorico(f buste.FiltriStorico, pagina int64) string {
	var q []coppiaQuery
	if f.Artigiano != 0 {
		q = append(q, coppiaQuery{"artigiano", strconv.FormatInt(f.Artigiano, 10)})
	}
	for _, x := range []coppiaQuery{{"stato", f.Stato}, {"data_da", f.DataDa}, {"data_a", f.DataA}} {
		if x.Valore != "" {
			q = append(q, x)
		}
	}
	q = append(q, coppiaQuery{"p", strconv.FormatInt(pagina, 10)})
	return rottaStorico + "?" + codificaQuery(q)
}

// paginaStorico è GET /handovers/history (handle_params :index).
func (f *facciaWeb) paginaStorico(w http.ResponseWriter, r *http.Request) {
	q := r.URL.RawQuery
	if err := f.buste.RegistraStorico(r.Context(), chiaviRegistroDi(q, buste.ChiaviRegistroStorico), contestoDi(r)); err != nil {
		f.registro.Error("lettura dello storico delle consegne non registrata")
		Errore(w, r)
		return
	}
	filtri := buste.LeggiFiltriStorico(func(nome string) (string, bool) { return testoPlug(q, nome) })
	aa, err := f.buste.Artigiani(r.Context())
	if err != nil {
		f.registro.Error("artigiani dello storico non letti", "errore", err.Error())
		Errore(w, r)
		return
	}
	s, err := f.buste.StoricoConsegne(r.Context(), filtri)
	if err != nil {
		f.registro.Error("storico delle consegne non letto", "errore", err.Error())
		Errore(w, r)
		return
	}
	titolare := titolareDi(r)
	d := datiStorico{Stato: s.Filtri.Stato, DataDa: s.Filtri.DataDa, DataA: s.Filtri.DataA, Pagina: s.Filtri.Pagina, Pagine: s.Pagine,
		Solleciti: f.haRotta(rottaPaginaSolleciti)}
	for _, a := range aa {
		d.Artigiani = append(d.Artigiani, voceArtigiano{ID: a.ID, Etichetta: a.Nome, Scelto: a.ID == s.Filtri.Artigiano})
	}
	for _, x := range s.Righe {
		v := rigaStoricoVista{ID: x.ID, Giorno: clock.Formatta(x.Aperta, "02/01/2006"), Artigiano: x.Artigiano, Buste: busteFrase(x.Buste), Fuori: x.Fuori}
		v.Stato, v.Classe = statoDellaRiga(x)
		if x.Chiusa != nil {
			v.Chiusa = clock.Formatta(*x.Chiusa, "02/01/2006")
			if titolare {
				v.Elimina = rottaStorico + "/" + strconv.FormatInt(x.ID, 10) + "/delete"
				v.DomandaElimina = domandaElimina(x.Buste)
			}
		}
		d.Righe = append(d.Righe, v)
	}
	if s.Filtri.Pagina > 1 {
		d.Precedenti = indirizzoPaginaStorico(s.Filtri, s.Filtri.Pagina-1)
	}
	if s.Filtri.Pagina < s.Pagine {
		d.Successive = indirizzoPaginaStorico(s.Filtri, s.Filtri.Pagina+1)
	}
	f.mostraCon(w, r, "storico", http.StatusOK, "", d)
}

func domandaElimina(n int) string {
	return strings.Replace(domandaEliminaStorico, "%d", strconv.Itoa(n), 1)
}

// statoBustaStorico è bag_state/1.
func statoBustaStorico(x buste.BustaDelloStorico) string {
	switch {
	case x.Uscita == nil:
		return "in raccolta"
	case x.Rientro == nil:
		return "📤 fuori bottega"
	}
	return "✓ rientrata"
}

// dalloStorico dice se la richiesta del dettaglio è davvero sotto /handovers/history (rottaDettaglioStorico).
func dalloStorico(r *http.Request) bool { return r.PathValue("storico") == "history" }

// dettaglioStorico è GET /handovers/history/{id} (handle_params :show); il controllo di dalloStorico resta anche
// qui, oltre che davanti alla catena.
func (f *facciaWeb) dettaglioStorico(w http.ResponseWriter, r *http.Request) {
	if !dalloStorico(r) {
		NonTrovata(w, r)
		return
	}
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, rottaStorico, buste.ErrConsegnaNonTrovata.Error())
		return
	}
	if err := f.buste.RegistraDettaglioStorico(r.Context(), id, contestoDi(r)); err != nil {
		f.registro.Error("lettura del dettaglio dello storico non registrata")
		Errore(w, r)
		return
	}
	det, err := f.buste.DettaglioStorico(r.Context(), id)
	switch {
	case errors.Is(err, buste.ErrConsegnaNonTrovata):
		f.vaiAConErrore(w, r, rottaStorico, err.Error())
		return
	case err != nil:
		f.registro.Error("dettaglio dello storico non letto", "errore", err.Error())
		Errore(w, r)
		return
	}
	d := datiDettaglioStorico{ID: id, Artigiano: det.Consegna.Artigiano, Aperta: clock.Timbro(det.Consegna.Aperta), DomandaElimina: domandaElimina(len(det.Buste))}
	if det.Chiusa != nil {
		d.Chiusa, d.Correzioni = clock.Timbro(*det.Chiusa), titolareDi(r)
	}
	for _, x := range det.Buste {
		d.Buste = append(d.Buste, bustaStoricoVista{ID: x.ID, Numero: x.Visibile().Titolo(), Stato: statoBustaStorico(x), Descrizione: troncaStorico(x.Descrizione),
			Uscita: timbroODash(x.Uscita), Rientro: timbroODash(x.Rientro), Note: x.Note})
	}
	for _, x := range det.Riaperte {
		d.Riaperte = append(d.Riaperte, bustaStoricoVista{ID: x.ID, Numero: x.Visibile().Titolo(), Descrizione: troncaStorico(x.Descrizione),
			Uscita: timbroODash(x.Uscita), Rientro: timbroODash(x.Rientro), Nota: x.NotaPresente})
	}
	f.mostraCon(w, r, "storico_consegna", http.StatusOK, "", d)
}

// eliminaDalloStorico è POST /handovers/history/{id}/delete (HandoverHistoryController.delete/2).
func (f *facciaWeb) eliminaDalloStorico(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, rottaStorico, buste.ErrConsegnaNonTrovata.Error())
		return
	}
	n, err := f.buste.EliminaDalloStorico(r.Context(), id, contestoDi(r))
	var rifiuto buste.Rifiuto
	switch {
	case err == nil:
		st := statoDi(r)
		st.dati.Info, st.cambiata = "Consegna #"+strconv.FormatInt(id, 10)+" eliminata dallo storico ("+strconv.FormatInt(n, 10)+" buste sganciate).", true
		f.vaiA(w, r, rottaStorico)
	case errors.As(err, &rifiuto):
		f.vaiAConErrore(w, r, rottaStorico, rifiuto.Error())
	default:
		f.registro.Error("consegna non eliminata dallo storico", "errore", err.Error())
		Errore(w, r)
	}
}

// sganciaDalloStorico è POST /handovers/history/{id}/detach (HandoverHistoryController.detach/2): l'id della
// busta si legge prima della consegna, come la fonte.
func (f *facciaWeb) sganciaDalloStorico(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, rottaStorico, buste.ErrConsegnaNonTrovata.Error())
		return
	}
	dettaglio := rottaStorico + "/" + strconv.FormatInt(id, 10)
	v, _ := ultimo(r, "riparazione_id")
	busta, ok := idPositivo(v)
	if !ok {
		f.vaiAConErrore(w, r, dettaglio, msgIDBustaNonValido)
		return
	}
	err := f.buste.SganciaDalloStorico(r.Context(), id, busta, contestoDi(r))
	switch {
	case err == nil:
		st := statoDi(r)
		st.dati.Info, st.cambiata = msgBustaSganciata, true
		f.vaiA(w, r, dettaglio)
	case errors.Is(err, buste.ErrConsegnaNonTrovata):
		f.vaiAConErrore(w, r, rottaStorico, err.Error())
	case errors.Is(err, buste.ErrSoloChiuseSgancia), errors.Is(err, buste.ErrBustaNonInConsegna):
		f.vaiAConErrore(w, r, dettaglio, err.Error())
	default:
		f.registro.Error("busta non sganciata dallo storico", "errore", err.Error())
		Errore(w, r)
	}
}

// registraStorico registra le rotte dello storico.
func (f *facciaWeb) registraStorico(mux *http.ServeMux) {
	f.rottaConGuscio(mux, rottaStoricoConsegne, personale, f.paginaStorico)
	f.rottaConGuscioSe(mux, rottaDettaglioStorico, personale, dalloStorico, f.dettaglioStorico)
	f.rotta(mux, "POST /handovers/history/{id}/delete", soloTitolare, f.eliminaDalloStorico)
	f.rotta(mux, "POST /handovers/history/{id}/detach", soloTitolare, f.sganciaDalloStorico)
}

// giroVisto è una riga del riquadro «🔁 Cicli dall'artigiano» della scheda (#repair-artisan-cycles).
type giroVisto struct {
	Consegna, Artigiano string // «Consegna #N» o «Consegna senza sessione»; «artigiano» senza
	Uscita, Rientro     string
	Attuale, Nota       bool // «ciclo corrente»; «📝 nota registrata» (solo un giro passato con la nota)
}

// giriVisti è il riquadro dei giri: nil se nessuno è passato (past_cycle?/1), perché il giro attuale è già nei
// riquadri della busta fuori e della nota del rientro.
func giriVisti(gg []buste.Giro) []giroVisto {
	passato := false
	for _, g := range gg {
		passato = passato || !g.Attuale
	}
	if !passato {
		return nil
	}
	out := make([]giroVisto, 0, len(gg))
	for _, g := range gg {
		v := giroVisto{Consegna: "Consegna senza sessione", Artigiano: "artigiano", Uscita: timbroODash(g.Uscita), Rientro: timbroODash(g.Rientro),
			Attuale: g.Attuale, Nota: g.NotaPresente && g.Note == nil}
		if g.Consegna != nil {
			v.Consegna = "Consegna #" + strconv.FormatInt(*g.Consegna, 10)
		}
		if g.Artigiano != nil {
			v.Artigiano = *g.Artigiano
		}
		out = append(out, v)
	}
	return out
}
