package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// La lista dei lotti d'avviso alle oreficerie (slice 088, ORE-04 e ORE-05), come ReadyLotsLive dello standalone
// (/ready-lots), per tutto il personale. Senza LiveView l'evento open è un modulo POST:
//   - riuscita → la pagina del lotto (push_navigate a /ready-lots/{id}, lotto.go dalla 089);
//   - rifiuto → la lista ridisegnata (200) con il messaggio e il menu su «Scegli un'oreficeria»: ogni rifiuto viene da un
//     valore che il menu non offre (vuoto, forgiato, la riga 1, un id che non c'è), quindi non c'è una scelta da tenere;
//   - guasto del database → la pagina d'errore con una riga nel registro (nello standalone la LiveView cade).
// Solo la GET della lista pulisce i lotti vuoti dei giorni prima (mount, una volta per caricamento), non il
// ridisegno dopo un rifiuto. Lo standalone la fa solo a LiveView collegata; nel Go non c'è che la GET, quindi
// la fa anche una richiesta senza JavaScript: nella lista non cambia niente (mostra solo i lotti di oggi).
// Il link «Storico dei lotti inviati» compare con la sua pagina (093).

const (
	rottaPaginaLotti       = "/ready-lots"
	rottaStoricoLotti      = "GET /ready-lots/history"
	msgSelezionaOreficeria = "Seleziona un'oreficeria."
	msgOreficeriaNonValida = "Oreficeria non valida."
)

// voceOreficeria è un'opzione del menu «Oreficeria».
type voceOreficeria struct {
	ID   int64
	Nome string
}

// rigaLotto è una riga di «Lotti aperti oggi».
type rigaLotto struct {
	ID                        int64
	Oreficeria, Buste, Aperto string
}

type datiLotti struct {
	Oreficerie []voceOreficeria
	Lotti      []rigaLotto
	Storico    bool
}

// mostraLotti disegna la lista con il menu e i lotti di oggi.
func (f *facciaWeb) mostraLotti(w http.ResponseWriter, r *http.Request, errore string) {
	oo, err := f.buste.Oreficerie(r.Context())
	if err != nil {
		f.registro.Error("oreficerie dei lotti non lette", "errore", err.Error())
		Errore(w, r)
		return
	}
	ll, err := f.buste.LottiApertiOggi(r.Context())
	if err != nil {
		f.registro.Error("lotti aperti non letti", "errore", err.Error())
		Errore(w, r)
		return
	}
	d := datiLotti{Storico: f.haRotta(rottaStoricoLotti)}
	for _, o := range oo {
		d.Oreficerie = append(d.Oreficerie, voceOreficeria{ID: o.ID, Nome: o.Nome})
	}
	for _, l := range ll {
		d.Lotti = append(d.Lotti, rigaLotto{ID: l.ID, Oreficeria: l.Oreficeria, Buste: busteFrase(l.Buste), Aperto: clock.Timbro(l.Aperto)})
	}
	f.mostraCon(w, r, "lotti", http.StatusOK, errore, d)
}

// paginaLotti è GET /ready-lots (mount di ReadyLotsLive): prima la pulizia, poi la pagina.
func (f *facciaWeb) paginaLotti(w http.ResponseWriter, r *http.Request) {
	if _, err := f.buste.PulisciLottiVuoti(r.Context()); err != nil {
		f.registro.Error("lotti vuoti non puliti", "errore", err.Error())
		Errore(w, r)
		return
	}
	f.mostraLotti(w, r, "")
}

// oreficeriaScelta è goldsmith_param/1: lot[oreficeria_id] senza spazi ai bordi (l'ultimo valore, come Plug);
// "" se manca, se lot non è la mappa («lot» o «lot[]», che Plug farebbe testo o lista) o se il valore non è un
// testo (lot[oreficeria_id][…], che Plug farebbe mappa o lista).
func oreficeriaScelta(r *http.Request) string {
	for k := range r.PostForm {
		if k == "lot" || k == "lot[]" || strings.HasPrefix(k, "lot[oreficeria_id][") {
			return ""
		}
	}
	v, _ := ultimo(r, "lot[oreficeria_id]")
	return strings.TrimSpace(v)
}

// apriLotto è l'evento open: i due rifiuti della pagina, poi quelli del dominio.
func (f *facciaWeb) apriLotto(w http.ResponseWriter, r *http.Request) {
	scelta := oreficeriaScelta(r)
	if scelta == "" {
		f.mostraLotti(w, r, msgSelezionaOreficeria)
		return
	}
	oreficeria, ok := idPositivo(scelta)
	if !ok {
		f.mostraLotti(w, r, msgOreficeriaNonValida)
		return
	}
	id, err := f.buste.ApriLotto(r.Context(), oreficeria, contestoDi(r))
	var rifiuto buste.Rifiuto
	switch {
	case err == nil:
		f.vaiA(w, r, rottaPaginaLotti+"/"+strconv.FormatInt(id, 10))
	case errors.As(err, &rifiuto):
		f.mostraLotti(w, r, rifiuto.Error())
	default:
		f.registro.Error("lotto non aperto", "errore", err.Error())
		Errore(w, r)
	}
}

// registraLotti registra le rotte della lista dei lotti (del personale, come la live dello standalone).
func (f *facciaWeb) registraLotti(mux *http.ServeMux) {
	f.rottaConGuscio(mux, "GET /ready-lots", personale, f.paginaLotti)
	f.rottaConGuscio(mux, "POST /ready-lots", personale, f.apriLotto) // ridisegna dopo un rifiuto
	f.registraLotto(mux)                                              // la pagina del lotto (089)
}
