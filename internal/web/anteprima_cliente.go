package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/L0cutus/gestrip/internal/buste"
)

// L'anteprima del cliente dalla scheda (CLI-27, RIP-S-27), come preview/2 di PublicRepairController
// dello standalone (go-baseline, slice 143):
//   - GET /repairs/{id}/anteprima-cliente sulla faccia interna, per titolare e dipendente, con la
//     catena delle pagine ma senza guscio (rotta, non rottaConGuscio: la pagina è del cliente, niente
//     manifest, menu né tavolozze, come la rotta fuori da :staff_shell dello standalone);
//   - private, no-store e no-referrer; il messaggio del personale lasciato nella sessione non si
//     mostra e se ne va (clear_flash/1);
//   - la pagina è pagina_cliente.html con i dati del link (buste.AnteprimaCliente: le foto, il
//     preventivo e la conversazione di chi ha il link, l'oreficeria su una busta B2B), senza token:
//     nessun modulo ha un indirizzo, il poll non c'è, ogni controllo è nel fieldset spento; le foto
//     passano dalla rotta del personale, che scrive la sua riga per ogni foto servita;
//   - una sola riga, repair.customer_preview_viewed; busta che non c'è, malformata o nel cestino: il
//     404 «Riparazione non disponibile» senza tipo e senza righe; riga che non si scrive: 503
//     «Anteprima non disponibile».
// Nessuna POST: la rotta è solo GET (una POST è il 404 della faccia).

// rottaAnteprima è il pattern della rotta dell'anteprima.
const rottaAnteprima = "GET /repairs/{id}/anteprima-cliente"

// msgAnteprimaNonDisponibile è il corpo del 503 di preview/2.
const msgAnteprimaNonDisponibile = "Anteprima non disponibile"

// anteprimaCliente è GET /repairs/{id}/anteprima-cliente.
func (f *facciaWeb) anteprimaCliente(w http.ResponseWriter, r *http.Request) {
	st := statoDi(r)
	if st.dati.Errore != "" || st.dati.Info != "" { // clear_flash/1
		st.dati.Errore, st.dati.Info, st.cambiata = "", "", true
	}
	h := w.Header()
	h.Set("Cache-Control", "private, no-store")
	h.Set("Referrer-Policy", "no-referrer")
	p, err := f.buste.AnteprimaCliente(r.Context(), r.PathValue("id"), contestoFoto(r))
	switch {
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.salva(w, st)
		testoSenzaTipo(w, http.StatusNotFound, rifiutoRiparazione)
		return
	case errors.Is(err, buste.ErrAnteprimaNonRegistrata):
		f.registro.Error("anteprima del cliente non registrata")
		f.salva(w, st)
		testoSenzaTipo(w, http.StatusServiceUnavailable, msgAnteprimaNonDisponibile)
		return
	case err != nil:
		f.registro.Error("anteprima del cliente non letta")
		Errore(w, r)
		return
	}
	scheda := "/repairs/" + strconv.FormatInt(p.ID, 10)
	d := datiPaginaCliente{PaginaCliente: p, FotoPagina: fotoPerLaScheda(p.Foto), Anteprima: true, Scheda: scheda}
	for i := range d.FotoPagina {
		d.FotoPagina[i].Src = scheda + "/photos/" + strconv.FormatInt(d.FotoPagina[i].ID, 10)
		d.FotoPagina[i].SrcMiniatura = indirizzoMiniatura(d.FotoPagina[i].Src)
	}
	// L'informativa sta sulla faccia pubblica (PIANO §1.5): l'anteprima la apre là, come la apre il
	// cliente; senza indirizzo pubblico configurato resta /privacy, come lo standalone.
	if f.pubblico != "" {
		d.Privacy = f.pubblico + "/privacy"
	}
	// Il riquadro email come lo vede chi ha il link, spento come ogni controllo dell'anteprima: nessun
	// indirizzo dove mandare i suoi moduli (D-16: solo con l'email disponibile).
	d.Email = f.riquadroEmail(r, p.ID, false)
	if d.ConConversazione {
		d.Righe = righeConversazione(d.Conversazione)
		if n := len(d.Conversazione); n > 0 {
			d.UltimoTS = d.Conversazione[n-1].Inviato
		}
	}
	f.salva(w, st)
	f.mostraPubblica(w, r, "pagina_cliente", http.StatusOK, d)
}

// testoSenzaTipo è send_resp/3 senza tipo: il testo da solo, senza Content-Type (net/http non ne
// indovina uno) e senza lunghezza, come paginaFoto, così nemmeno il proxy davanti ne indovina uno.
func testoSenzaTipo(w http.ResponseWriter, codice int, testo string) {
	w.Header()["Content-Type"] = nil
	w.WriteHeader(codice)
	_ = http.NewResponseController(w).Flush()
	_, _ = w.Write([]byte(testo)) // se il client se n'è andato non c'è niente da fare
}
