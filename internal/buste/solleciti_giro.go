package buste

import (
	"context"
	"database/sql"
	"time"

	"github.com/L0cutus/gestrip/internal/db"
)

// Il lato del dominio del lavoro «Solleciti artigiano» (Jobs.ArtisanChases dello standalone, JOB-09; slice 096):
// le stesse buste e la stessa soglia della pagina dei solleciti (late_query/3 e threshold_days/1 qui sopra, non
// una copia), ristrette a quelle non ancora nominate in questo giro dall'artigiano, e il loro timbro.
//
// **Per giro:** sollecito_avvisato_il si confronta con consegnata_artigiano_il, l'uscita del giro in corso: un
// timbro vuoto o di prima dell'uscita è di un giro passato (o di nessuno) e la busta si può nominare; un timbro
// uguale o dopo l'uscita vuol dire «già nominata in questo giro». Nessun altro percorso deve azzerare il timbro:
// la nuova uscita lo rende vecchio da sola.

// condNonNominataNelGiro è il where che il lavoro aggiunge a late_query/3 (candidates/2) ed è anche la guardia del
// timbro (claim/2).
const condNonNominataNelGiro = ` AND (b.sollecito_avvisato_il IS NULL OR b.sollecito_avvisato_il < b.consegnata_artigiano_il)`

// TimbraSollecitiDelGiro è candidates/2 più claim/2 del lavoro dei solleciti, nella transazione tx del lavoro
// (che accoda il riepilogo e fa il commit, o annulla tutto): la soglia dell'impostazione (threshold_days(%{})),
// le buste in ritardo della pagina non ancora nominate in questo giro, lette dentro tx, e il timbro adesso su
// ognuna con la guardia del giro. Ritorna la soglia, i gruppi delle buste timbrate (per artigiano della consegna,
// per nome e poi per uscita, come la pagina) e quante. Non scrive un sollecito (solleciti_artigiani) né audit.
func TimbraSollecitiDelGiro(ctx context.Context, tx *sql.Tx, adesso time.Time) (int64, []GruppoSolleciti, int, error) {
	giorni, err := sogliaSolleciti(ctx, tx, "")
	if err != nil {
		return 0, nil, 0, err
	}
	gg, _, err := busteInRitardoDove(ctx, tx, adesso, giorni, 0, condNonNominataNelGiro)
	if err != nil {
		return 0, nil, 0, err
	}
	ora := adesso.UTC().Format(db.FormatoIstante)
	var timbrati []GruppoSolleciti
	n := 0
	for _, g := range gg {
		presi := g
		presi.Buste = nil
		for _, x := range g.Buste {
			res, err := tx.ExecContext(ctx, `UPDATE buste AS b SET sollecito_avvisato_il = ? WHERE b.id = ?`+condNonNominataNelGiro, ora, x.ID)
			if err != nil {
				return 0, nil, 0, err
			}
			k, err := res.RowsAffected()
			if err != nil {
				return 0, nil, 0, err
			}
			if k == 1 {
				presi.Buste = append(presi.Buste, x)
				n++
			}
		}
		if len(presi.Buste) > 0 {
			timbrati = append(timbrati, presi)
		}
	}
	return giorni, timbrati, n, nil
}
