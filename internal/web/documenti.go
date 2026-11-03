package web

import (
	"net/http"
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/stampe"
)

// L'etichetta busta (RIP-S-43) e la scheda PDF (RIP-S-45), come BagLabelController.show/2 e
// RepairSheetController.show/2 dello standalone a go-baseline. La catena, la busta (get_repair!/1),
// gli errori, la riga d'audit dopo il PDF e la risposta sono quelli della ricevuta (ricevuta.go);
// cambiano solo:
//   - l'etichetta è in linea («inline; filename="etichetta_busta_<numero>.pdf"») e diventa un allegato
//     se la query ha download con un valore (params["download"] not in [nil, ""]: anche
//     «download=0», come la verità di Flask, e «download[]=…», che per Plug non è un testo);
//   - la scheda è sempre in linea («inline; filename="busta_<numero>.pdf"»), download o no.

// etichetta è GET /repairs/{id}/label.pdf.
func (f *facciaWeb) etichetta(w http.ResponseWriter, r *http.Request) {
	pdf, e, err := f.buste.Etichetta(r.Context(), r.PathValue("id"), contestoFoto(r), stampe.Etichetta)
	tipo := "inline"
	if comeAllegato(r.URL.RawQuery) {
		tipo = "attachment"
	}
	f.inviaPDF(w, r, pdf, err, "etichetta", disposizione(tipo, buste.NomeFileEtichetta(e.Numero)))
}

// schedaPDF è GET /repairs/{id}/scheda.pdf.
func (f *facciaWeb) schedaPDF(w http.ResponseWriter, r *http.Request) {
	pdf, s, err := f.buste.SchedaPDF(r.Context(), r.PathValue("id"), contestoFoto(r), stampe.SchedaPDF)
	f.inviaPDF(w, r, pdf, err, "scheda", disposizione("inline", buste.NomeFileSchedaPDF(s.Numero)))
}

// comeAllegato dice se la query chiede l'allegato: l'ultima assegnazione di download (come Plug) ha
// un valore, o ha le parentesi (allora per Plug è una lista o una mappa, non nil né "").
func comeAllegato(grezza string) bool {
	allegato := false
	for _, coppia := range strings.Split(grezza, "&") {
		if coppia == "" {
			continue
		}
		k, v, _ := strings.Cut(coppia, "=")
		k = decodificaForm(strings.ReplaceAll(k, "+", " "))
		radice, _, conParentesi := strings.Cut(k, "[")
		if radice != "download" {
			continue
		}
		allegato = conParentesi || decodificaForm(strings.ReplaceAll(v, "+", " ")) != ""
	}
	return allegato
}
