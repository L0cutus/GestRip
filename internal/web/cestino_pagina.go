package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// La pagina «🗑 Cestino» e il ripristino (slice 137, RIP-S-51, RIP-S-52), come TrashLive dello standalone a go-baseline,
// senza LiveView:
//   - GET /cestino (titolare e dipendente): il titolo, la frase con la finestra («… resta recuperabile per X giorni.
//     Passata la finestra non sparisce da solo: …»), «Il cestino è vuoto.» senza voci, altrimenti una riga per voce dal
//     più recente: il tipo («Busta»), il numero visibile (D-64), «descrizione · stato · cestinato il gg/mm/aaaa hh:mm»
//     (ora del negozio), i giorni rimasti con «giorni», «Finestra scaduta» a zero giorni e «↩ Ripristina» con la
//     conferma «Riportare questo elemento fra quelli attivi?». Nessun link alla scheda (la busta nel cestino non si
//     apre), mai il token del link del cliente. La voce del menu ☰ compare da sola con la rotta (vociMenu);
//   - lo stato con l'etichetta delle altre pagine («In lavorazione», etichettaStato; D-125, Stefano 28/11/2026: lo
//     standalone stampa lo stato com'è salvato);
//   - POST /cestino/<famiglia>/{id}/ripristina, con il CSRF di ogni modulo: riuscita «Ripristinato.» sulla pagina;
//     niente da ripristinare (id malformato, che non c'è, fuori dal cestino) o un guasto: la pagina con «Busta non
//     ripristinata.» nello stesso flash (D-126; lo standalone ignora l'evento); il guasto con una riga fissa nel
//     registro, senza numeri.
//
// Pronto per le vendite (domanda K): una rotta di ripristino per famiglia (buste.FamiglieCestino), con il permesso di
// buste.FamigliaCestino.SoloTitolare, che decide anche se il pulsante si disegna; il tipo della voce viene da
// tipiCestino. La T4.12 aggiunge «Vendita» qui e la famiglia nel dominio. L'eliminazione definitiva è della 140.

const (
	rottaCestino    = "/cestino"
	msgRipristinato = "Ripristinato."
	// D-126 (Stefano, 28/11/2026): il ripristino non riuscito lo dice, nello stesso flash.
	msgNonRipristinata = "Busta non ripristinata."
	domandaRipristino  = "Riportare questo elemento fra quelli attivi?"
)

// tipiCestino è family_label/1: il cartellino della voce.
var tipiCestino = map[buste.FamigliaCestino]string{buste.FamigliaBuste: "Busta"}

// azioneRipristino è l'indirizzo del ripristino di una voce.
func azioneRipristino(f buste.FamigliaCestino, id string) string {
	return rottaCestino + "/" + string(f) + "/" + id + "/ripristina"
}

type voceCestinoVista struct {
	Famiglia, ID, Tipo, Etichetta, Dettaglio, Stato, Il string
	Giorni                                              int64
	Scaduta                                             bool
	Ripristina                                          string // l'azione del modulo; "" senza pulsante
}

type datiCestino struct {
	Giorni int64
	Voci   []voceCestinoVista
}

// registraCestino mette la pagina e una rotta di ripristino per famiglia.
func (f *facciaWeb) registraCestino(mux *http.ServeMux) {
	f.rottaConGuscio(mux, "GET "+rottaCestino, personale, f.paginaCestino)
	for _, fam := range buste.FamiglieCestino() {
		chi := personale
		if fam.SoloTitolare() {
			chi = soloTitolare
		}
		f.rotta(mux, "POST "+azioneRipristino(fam, "{id}"), chi, f.ripristinaDalCestino(fam))
	}
}

// paginaCestino è GET /cestino (mount/3 e render/1 di TrashLive).
func (f *facciaWeb) paginaCestino(w http.ResponseWriter, r *http.Request) {
	c, err := f.buste.Cestino(r.Context())
	if err != nil {
		f.registro.Error("cestino non letto")
		Errore(w, r)
		return
	}
	titolare := titolareDi(r)
	d := datiCestino{Giorni: c.Giorni}
	for _, v := range c.Voci {
		id := strconv.FormatInt(v.ID, 10)
		x := voceCestinoVista{Famiglia: string(v.Famiglia), ID: id, Tipo: tipiCestino[v.Famiglia], Etichetta: v.Etichetta,
			Dettaglio: v.Dettaglio, Stato: etichettaStato(v.Stato), Il: clock.Timbro(v.CancellataIl), Giorni: v.GiorniRimasti,
			Scaduta: v.GiorniRimasti == 0}
		if !v.Famiglia.SoloTitolare() || titolare {
			x.Ripristina = azioneRipristino(v.Famiglia, id)
		}
		d.Voci = append(d.Voci, x)
	}
	f.mostraCon(w, r, "cestino", http.StatusOK, "", d)
}

// ripristinaDalCestino è POST /cestino/<famiglia>/{id}/ripristina (l'evento restore). Ogni rifiuto (id malformato,
// riga che non c'è o non più nel cestino, guasto) torna alla pagina con «Busta non ripristinata.» nello stesso flash
// di «Ripristinato.» (D-126: lo standalone lo ignora in silenzio).
func (f *facciaWeb) ripristinaDalCestino(fam buste.FamigliaCestino) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st := statoDi(r)
		id, ok := idPositivo(r.PathValue("id"))
		err := buste.ErrNonNelCestino
		if ok {
			err = f.buste.RipristinaDalCestino(r.Context(), fam, id, contestoDi(r))
		}
		switch {
		case err == nil:
			st.dati.Info = msgRipristinato
		case errors.Is(err, buste.ErrNonNelCestino):
			st.dati.Info = msgNonRipristinata
		default:
			f.registro.Error("elemento non ripristinato dal cestino")
			st.dati.Info = msgNonRipristinata
		}
		st.cambiata = true
		f.vaiA(w, r, rottaCestino)
	}
}
