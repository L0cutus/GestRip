package web

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/L0cutus/gestrip/internal/buste"
)

// Le pagine pubbliche dell'artigiano (ART-24…26, slice 084), come PublicArtisanController dello
// standalone (go-baseline). Le regole (gettoni, proiezione, audit, foto) stanno in
// internal/buste/artigiano_link.go; qui gli indirizzi, le risposte e i testi:
//   - GET /artigiano/{id}?t= (il gettone del Go o, D-87, il Phoenix.Token dello standalone) e GET
//     /artigiano/{busta}/{anno}?t= (il vecchio link Flask): la pagina di sola consultazione, no-store,
//     no-referrer e X-Robots-Tag «noindex, nofollow»; ogni rifiuto il 404 «Riparazione non disponibile»
//     (RifiutoPubblico), che il limite conta nel secchio dei token (famiglia artigiano, limiti.go);
//   - GET /artigiano/{id}/foto/{foto}?t= e GET /artigiano/{busta}/{anno}/foto/{file}?t=: la foto o, con
//     ?size=thumb, la miniatura, o il 404 «Foto non disponibile»; il limite non le conta e non hanno
//     X-Robots-Tag (come lo standalone).

// datiPaginaArtigiano è quello che artigiano.html riceve.
type datiPaginaArtigiano struct {
	buste.PaginaArtigiano
	FotoPagina []fotoDellaScheda
}

// gettoneArtigiano è params["t"] solo se è un testo (un t[a]=… non conta: lo standalone lo rifiuta).
func gettoneArtigiano(r *http.Request) string {
	v, testo := parametroQuery(r.URL.RawQuery, "t")
	if !testo {
		return ""
	}
	return v
}

// mostraPaginaArtigiano disegna la pagina con le foto: src dà l'indirizzo della foto intera, la
// miniatura è lo stesso con size=thumb (thumbnail_src/1).
func (f *facciaWeb) mostraPaginaArtigiano(w http.ResponseWriter, r *http.Request, p buste.PaginaArtigiano, src func(buste.Foto) string) {
	d := datiPaginaArtigiano{PaginaArtigiano: p, FotoPagina: fotoPerLaScheda(p.Foto)}
	for i := range d.FotoPagina {
		d.FotoPagina[i].Src = src(p.Foto[i])
		d.FotoPagina[i].SrcMiniatura = indirizzoMiniatura(d.FotoPagina[i].Src)
	}
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	f.mostraPubblica(w, r, "artigiano", http.StatusOK, d)
}

// rispostaPaginaArtigiano è il ramo comune di show/2 e legacy/2.
func (f *facciaWeb) rispostaPaginaArtigiano(w http.ResponseWriter, r *http.Request, p buste.PaginaArtigiano, err error, src func(buste.Foto) string) {
	switch {
	case errors.Is(err, buste.ErrBustaNonTrovata):
		RifiutoPubblico(w, r, rifiutoRiparazione, false)
	case err != nil:
		f.registro.Error("pagina dell'artigiano non letta")
		Errore(w, r)
	default:
		f.mostraPaginaArtigiano(w, r, p, src)
	}
}

// queryGettone è ?t=<gettone> come lo scrive Plug ([t: token]).
func queryGettone(g string) string { return "?t=" + url.QueryEscape(g) }

// paginaArtigiano è GET /artigiano/{id}.
func (f *facciaWeb) paginaArtigiano(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	g := gettoneArtigiano(r)
	p, err := f.buste.PaginaArtigiano(r.Context(), r.PathValue("id"), g, contestoCliente(r))
	f.rispostaPaginaArtigiano(w, r, p, err, func(x buste.Foto) string {
		return "/artigiano/" + strconv.FormatInt(p.ID, 10) + "/foto/" + strconv.FormatInt(x.ID, 10) + queryGettone(g)
	})
}

// paginaArtigianoStorica è GET /artigiano/{busta}/{anno}: le foto per nome file sul numero com'è
// scritto nell'indirizzo (lo standalone le scrive sul numero salvato, che lì è lo stesso).
func (f *facciaWeb) paginaArtigianoStorica(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	g := gettoneArtigiano(r)
	p, err := f.buste.PaginaArtigianoStorica(r.Context(), r.PathValue("busta")+"/"+r.PathValue("anno"), g, contestoCliente(r))
	base := "/artigiano/" + segmentoPhoenix(r.PathValue("busta")) + "/" + segmentoPhoenix(r.PathValue("anno")) + "/foto/"
	f.rispostaPaginaArtigiano(w, r, p, err, func(x buste.Foto) string {
		return base + segmentoPhoenix(x.NomeFile) + queryGettone(g)
	})
}

// fotoArtigiano è GET /artigiano/{id}/foto/{foto}.
func (f *facciaWeb) fotoArtigiano(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	m, err := f.buste.LeggiFotoArtigiano(r.Context(), r.PathValue("id"), gettoneArtigiano(r), r.PathValue("foto"), variante(r), contestoCliente(r))
	f.rispostaFotoArtigiano(w, r, m, err)
}

// fotoArtigianoStorica è GET /artigiano/{busta}/{anno}/foto/{file}.
func (f *facciaWeb) fotoArtigianoStorica(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	m, err := f.buste.LeggiFotoArtigianoStorica(r.Context(), r.PathValue("busta")+"/"+r.PathValue("anno"), gettoneArtigiano(r),
		r.PathValue("file"), variante(r), contestoCliente(r))
	f.rispostaFotoArtigiano(w, r, m, err)
}

func (f *facciaWeb) rispostaFotoArtigiano(w http.ResponseWriter, r *http.Request, m *buste.MediaFoto, err error) {
	if err != nil {
		f.registro.Error("foto dell'artigiano non letta")
		Errore(w, r)
		return
	}
	rispostaFotoPubblica(w, m)
}

// registraArtigiano registra le rotte dell'artigiano sulla faccia pubblica.
func (f *facciaWeb) registraArtigiano(mux *http.ServeMux) {
	f.rottaPubblica(mux, "GET /artigiano/{id}", f.paginaArtigiano)
	f.rottaPubblica(mux, "GET /artigiano/{busta}/{anno}", f.paginaArtigianoStorica)
	f.rottaPubblica(mux, "GET /artigiano/{id}/foto/{foto}", f.fotoArtigiano)
	f.rottaPubblica(mux, "GET /artigiano/{busta}/{anno}/foto/{file}", f.fotoArtigianoStorica)
}
