package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// I lavori del giorno alla conferma del ripristino (D-119, slice 133, solo Go). Dopo un ripristino il database ha le
// righe di esecuzioni_lavori del backup: i lavori che la macchina vecchia ha fatto dopo il backup risultano non fatti
// e il primo giro del serve riavviato li rifarebbe (avvisi doppi al negozio). Lo standalone, dopo un ripristino
// --live, li rifà. Il Go, alla conferma «questa è ora la macchina vera» (backup.ConfermaRipristino, nella stessa
// transazione), segna come già fatti oggi i lavori già dovuti a quell'ora; uno non ancora dovuto parte alla sua ora.

// DettaglioDopoRipristino è la frase della riga di un lavoro segnato come fatto alla conferma del ripristino.
const DettaglioDopoRipristino = "Segnato come già fatto alla conferma del ripristino: su questa macchina non è partito."

// Transazione è dove SegnaDovutiComeFatti legge e scrive: la transazione della conferma.
type Transazione interface {
	db.Interrogatore
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

// SegnaDovutiComeFatti scrive, nella transazione tx, una riga automatica «ok» (0 avvisi, 0 messaggi,
// DettaglioDopoRipristino) per ogni lavoro già dovuto adesso, nell'ordine di Nomi, e dà i loro nomi. «Dovuto» è
// quello del giro (due?/2) senza le domande sui canali: oggi il negozio è aperto (D-17), il lavoro è attivo, l'ora del
// negozio ha raggiunto la sua (nellOra), oggi non c'è già un'esecuzione automatica riuscita e quelle di oggi sono meno
// di TentativiAlGiorno. I canali non contano perché nella copia isolata sono spenti e tornano solo con la conferma.
// Niente jobs.ran: il lavoro non è partito; il conteggio va nell'audit della conferma.
func SegnaDovutiComeFatti(ctx context.Context, tx Transazione, adesso time.Time) ([]string, error) {
	conf, err := LeggiConfig(ctx, tx)
	if err != nil {
		return nil, err
	}
	giorno := clock.DataDi(adesso)
	il := adesso.UTC().Truncate(time.Microsecond).Format(db.FormatoIstante)
	var segnati []string
	for _, n := range Nomi {
		if !nellOra(conf, n, adesso) {
			continue
		}
		rimasti, err := tentativiRimasti(ctx, tx, n, giorno)
		if err != nil {
			return nil, err
		}
		if !rimasti {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO esecuzioni_lavori (lavoro, avvio, esito, eseguito_il, giorno, avvisi, messaggi, dettaglio)
			VALUES (?, ?, ?, ?, ?, 0, 0, ?)`, n, AvvioAutomatico, EsitoOK, il, giorno.String(), DettaglioDopoRipristino); err != nil {
			return nil, fmt.Errorf("jobs: lavoro %s segnato come fatto: %w", n, err)
		}
		segnati = append(segnati, n)
	}
	return segnati, nil
}

// FraseSegnati dice al titolare, dopo la conferma del ripristino, quali lavori di oggi sono stati segnati come già
// fatti (con i nomi della pagina «Promemoria e job») e che cosa fare se la macchina vecchia non li aveva fatti; ""
// se nessuno.
func FraseSegnati(nomi []string) string {
	if len(nomi) == 0 {
		return ""
	}
	parole := make([]string, 0, len(nomi))
	for _, n := range nomi {
		e := Etichetta(n)
		if e == "" {
			e = n
		}
		parole = append(parole, e)
	}
	return "Lavori di oggi segnati come già fatti: " + strings.Join(parole, ", ") +
		". Se la macchina vecchia non li aveva fatti, usa «Esegui adesso» in Promemoria e job dopo il riavvio."
}
