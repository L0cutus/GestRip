package web

import (
	"net/http"
	"strconv"

	"github.com/L0cutus/gestrip/internal/buste"
)

// La vista «Galleria» della home (RIP-H-13, slice 057), come assign_gallery/3, sign_gallery/2,
// gallery_src/3 e il blocco #repair-gallery di WorkshopLive dello standalone a go-baseline (slice
// 202 e 205):
//   - con vista=galleria la pagina mostra, al posto della lista, le buste della pagina che hanno
//     almeno una foto: la prima (buste.Galleria), «+N» per le altre, e sotto i dati essenziali
//     (numero visibile D-64, data, cliente, oreficeria, stato, 🪙, 🔧, ⏰ consegna, 💬). La foto apre
//     il visualizzatore della scheda (/repairs/<id>#repair-photo-viewer-<foto>, una pagina intera,
//     così :target apre il riquadro), i dati la scheda;
//   - sopra, «N schede con foto · M senza foto nascoste»; senza nessuna foto nella pagina, «Nessuna
//     scheda con foto trovata.» con «M senza foto escluse dalla galleria.»; la paginazione resta
//     quella della lista (30 buste, con o senza foto);
//   - la dimensione delle foto (S, M, L) è l'impostazione galleria_dim (M senza);
//   - ogni visita scrive una riga repair.staff_gallery_viewed con gli id delle foto mostrate
//     (buste.RegistraGalleria) e solo allora ogni miniatura riceve il gettone della galleria (g,
//     foto.go), firmato all'inizio del giorno UTC: la rotta delle foto la serve senza una riga sua. Se
//     la riga non si scrive la pagina si mostra lo stesso, senza gettoni: ogni miniatura scrive la sua
//     riga. Nessuna foto, nessuna riga. Lo standalone, LiveView, scrive la riga al primo disegno e di
//     nuovo alla connessione (e la riscrive solo se cambiano le foto o il giorno, durante la vita del
//     socket); il Go non ha un socket e scrive una riga per visita, come repair.index_viewed (D-68).
// Con JavaScript una miniatura che non arriva (un momento in cui il server è occupato) si chiede
// un'altra volta dopo un attimo (data-retry, img_retry.js dello standalone, in app.js).

// datiGalleria è la galleria della pagina.
type datiGalleria struct {
	Classe   string // gallery-S, gallery-M o gallery-L (gallery_grid/1)
	Voci     []voceGalleria
	Nascoste int // le buste della pagina senza foto
}

// Schede è «1 scheda» o «N schede», per «N schede con foto».
func (g datiGalleria) Schede() string {
	if len(g.Voci) == 1 {
		return "1 scheda"
	}
	return strconv.Itoa(len(g.Voci)) + " schede"
}

// voceGalleria è una busta della galleria, già scritta come la mostra lo standalone.
type voceGalleria struct {
	ID         int64
	Href       string // la scheda
	HrefFoto   string // la scheda con il visualizzatore aperto sulla foto
	Src        string // la miniatura, con il gettone se la riga della galleria c'è
	Alt        string // «Foto 0045/26» (D-64: il numero visibile, senza «#»)
	Altre      int64  // le altre foto della busta: «+N» se più di zero
	Numero     string // «#0045/26» o «N. 45/26»
	Data       string
	Cliente    string
	Oreficeria *string
	Stato      string
	ToniStato  string
	CompraOro  bool
	Fuori      bool // fuori bottega presso l'artigiano (Repair.out_of_shop?/1)
	Consegna   string
	Urgente    bool
	NonLetto   bool
}

// classeGalleria è gallery_grid/1: la classe della dimensione, M per ogni valore che non è S o L.
func classeGalleria(dim string) string {
	switch dim {
	case "S", "L":
		return "gallery-" + dim
	}
	return "gallery-M"
}

// galleriaDi scrive la galleria della lista l: le prime foto delle buste della pagina, la riga
// repair.staff_gallery_viewed e i gettoni. Un errore è della lettura delle foto; la riga che non si
// scrive non lo è (sign_gallery/2: la pagina resta, senza gettoni).
func (f *facciaWeb) galleriaDi(r *http.Request, l buste.Lista) (*datiGalleria, error) {
	ids := make([]int64, len(l.Righe))
	for i, riga := range l.Righe {
		ids[i] = riga.ID
	}
	prime, err := f.buste.Galleria(r.Context(), ids)
	if err != nil {
		return nil, err
	}
	adesso := f.orologio.Adesso()
	_, perEtichetta := l.Filtri.Etichetta()
	g := &datiGalleria{Classe: classeGalleria(l.GalleriaDim)}
	var foto []int64
	for _, riga := range l.Righe {
		p, ok := prime[riga.ID]
		if !ok {
			g.Nascoste++
			continue
		}
		h := rigaDellaLista(riga, adesso, perEtichetta)
		busta := strconv.FormatInt(riga.ID, 10)
		numero, _ := riga.NumeroVisibile()
		g.Voci = append(g.Voci, voceGalleria{
			ID: riga.ID, Href: h.Href, HrefFoto: h.Href + "#repair-photo-viewer-" + strconv.FormatInt(p.ID, 10),
			Src: "/repairs/" + busta + "/photos/" + strconv.FormatInt(p.ID, 10) + "?size=thumb", Alt: "Foto " + numero,
			Altre: p.Quante - 1, Numero: h.Numero, Data: h.Data, Cliente: h.Cliente, Oreficeria: h.Oreficeria,
			Stato: h.Stato, ToniStato: h.ToniStato, CompraOro: h.CompraOro, Fuori: riga.FuoriBottega, Consegna: h.Consegna,
			Urgente: h.Urgente, NonLetto: h.NonLetto,
		})
		foto = append(foto, p.ID)
	}
	if len(foto) == 0 {
		return g, nil
	}
	if err := f.buste.RegistraGalleria(r.Context(), foto, contestoDi(r)); err != nil {
		f.registro.Warn("riga della galleria non scritta: miniature senza gettone")
		return g, nil
	}
	utente, giorno := statoDi(r).utente.ID, inizioGiorno(adesso)
	for i := range g.Voci {
		g.Voci[i].Src += "&g=" + f.galleria.firma(utente, g.Voci[i].ID, foto[i], giorno)
	}
	return g, nil
}
