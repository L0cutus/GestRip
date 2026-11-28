package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/L0cutus/gestrip/internal/buste"
)

// «🗑 Sposta nel cestino» sulla scheda (slice 136, RIP-S-49), come il pulsante #trash-repair e l'evento trash di
// RepairLive dello standalone a go-baseline, senza LiveView:
//   - il pulsante c'è per titolare e dipendente (Trash.owner_only?(:repair) è false), in fondo alla scheda, con la
//     conferma dello standalone «Spostare la busta N nel cestino? Resta recuperabile per X giorni.»: il numero è quello
//     visibile del titolo (D-64: «#0045/26» o «N. 45/26»), i giorni la finestra del cestino (buste.GiorniCestino);
//   - POST /repairs/{id}/cestino, con il CSRF della faccia interna come ogni modulo; riuscita: la home con «Busta
//     spostata nel cestino.» (push_navigate a ~p"/");
//   - una busta che non c'è, già nel cestino o un id malformato: la home con «Busta non spostata nel cestino.», lo
//     stesso rifiuto per tutti (lo standalone lo dà sulla scheda, che nel Go non si riapre per una busta nel cestino);
//   - un guasto (audit compreso): di nuovo la scheda con «Busta non spostata nel cestino.» e una riga fissa nel
//     registro, senza numeri né testo dell'errore; la busta resta dov'era.

const (
	msgSpostataNelCestino    = "Busta spostata nel cestino."
	msgNonSpostataNelCestino = "Busta non spostata nel cestino."
)

// confermaCestino è il data-confirm del pulsante, con il numero visibile e la finestra.
func confermaCestino(numero string, giorni int64) string {
	return "Spostare la busta " + numero + " nel cestino? Resta recuperabile per " + strconv.FormatInt(giorni, 10) + " giorni."
}

// spostaNelCestino è POST /repairs/{id}/cestino (l'evento trash).
func (f *facciaWeb) spostaNelCestino(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, "/", msgNonSpostataNelCestino)
		return
	}
	conSegno := f.segnoDelModulo(r, id)
	_, err := f.buste.SpostaNelCestino(r.Context(), id, contestoDi(r))
	switch {
	case err == nil:
		st := statoDi(r)
		st.dati.Info, st.cambiata = msgSpostataNelCestino, true
		f.vaiA(w, r, "/")
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgNonSpostataNelCestino)
	default:
		f.registro.Error("busta non spostata nel cestino")
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgNonSpostataNelCestino)
	}
}
