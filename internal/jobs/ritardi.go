package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// Il lavoro «Buste in ritardo» (Jobs.OverdueRepairs dello standalone a go-baseline, JOB-07, JOB-08; slice 095):
//
//   - **In ritardo** (unnamed_candidates/1): data promessa presente e al più oggi meno la soglia
//     (Config.GiorniRitardi, overdue_days: 1 = la data è già passata), stato non chiuso (consegnata, annullata,
//     chiusa del compro oro), non nel cestino, mai segnalata (ritardo_avvisato_il vuoto). «Oggi» è quello del
//     negozio (clock, Europe/Rome). Una nuova data promessa azzera il timbro e riapre l'avviso (slice 211 dello
//     standalone, in buste.Modifica): il lavoro non lo sa e non gli serve.
//   - **Un avviso per busta, un messaggio per canale** al negozio (i destinatari del pianificatore), testo
//     semplice: oggetto «Busta in ritardo <numero>», righe «🕒 Busta in ritardo», «Busta <numero> — <cliente>»,
//     «Consegna prevista: gg/mm/aaaa (Ngg fa)», la descrizione tagliata a 200 grafemi con «…» (se c'è).
//   - **Il timbro è il lucchetto** (claim/6): ogni busta è una transazione sua con l'UPDATE condizionato da tutto
//     il predicato, non solo dal timbro (slice 213), e i messaggi scritti dalla riga timbrata (RETURNING), non
//     dall'elenco. Chi perde l'UPDATE non scrive niente; un rifiuto della coda annulla anche il timbro e i
//     messaggi già accodati della stessa busta.
//   - **Un rifiuto non ferma le altre:** la busta rifiutata resta da segnalare, le altre vanno avanti; l'esecuzione
//     è «Fallito» con «… N non riuscite: <primo motivo>». Un guasto che non è un rifiuto della coda (il database)
//     ferma il lavoro, come l'eccezione dello standalone: la riga dirà DettaglioInterrotto, le buste già timbrate
//     restano timbrate e le altre aspettano il giro dopo.
//   - **Silenziamento alla prima esecuzione vera** (silence_history/2, JOB-08): se manca la chiave
//     ChiaveStoricoSilenziato, le buste in ritardo da GiorniStorico giorni o più si timbrano senza messaggi e la
//     chiave si scrive, tutto in una transazione. «Vera» perché il pianificatore chiama il lavoro solo con un
//     canale e il lavoro acceso (anche a mano, come lo standalone); l'import porta la chiave.
//   - **Stop:** con il contesto finito il lavoro torna con l'errore del contesto e il pianificatore non scrive la
//     riga (il giorno resta da fare): le buste già timbrate non si ripetono.

// GiorniStorico è il ritardo, in giorni, dal quale una busta si silenzia alla prima esecuzione (@backfill_days):
// data promessa al più oggi meno 7.
const GiorniStorico = 7

// MassimoDescrizione è la lunghezza della descrizione nel messaggio, in grafemi (@max_description).
const MassimoDescrizione = 200

// Accodatore è la parte della coda in uscita che scrive nella transazione di chi chiama (uscita.Coda.AccodaIn).
type Accodatore interface {
	AccodaIn(ctx context.Context, tx *sql.Tx, dest uscita.Destinazione, cont uscita.Contenuto, a audit.Contesto) (int64, error)
}

// LavoroRitardi è il lavoro dei ritardi di un database, da registrare nel pianificatore come Ritardi.
type LavoroRitardi struct {
	d     *db.DB
	posta Accodatore
	log   *slog.Logger
	// primaDelTimbro, solo nei test, gira fra l'elenco e la transazione del timbro di ogni busta: quello che una
	// modifica può fare in quel momento. nil fuori dai test.
	primaDelTimbro func(busta int64)
}

// NuoviRitardi è il lavoro dei ritardi sul database d, che accoda con posta (la coda di serve).
func NuoviRitardi(d *db.DB, posta Accodatore) *LavoroRitardi {
	return &LavoroRitardi{d: d, posta: posta, log: slog.New(slog.DiscardHandler)}
}

// ConRegistro dà al lavoro le righe del server: per una busta non riuscita il suo id e il motivo (testi fissi,
// mai indirizzi o testi dei messaggi).
func (r *LavoroRitardi) ConRegistro(l *slog.Logger) *LavoroRitardi {
	if l != nil {
		r.log = l
	}
	return r
}

// predicatoRitardo è unnamed_candidates/1 con il cestino (prepare_query/3): il primo parametro è il confine
// (aaaa-mm-gg), poi gli stati chiusi.
var predicatoRitardo, statiChiusi = func() (string, []any) {
	chiusi := buste.StatiChiusi()
	segni := strings.TrimSuffix(strings.Repeat("?, ", len(chiusi)), ", ")
	args := make([]any, len(chiusi))
	for i, s := range chiusi {
		args[i] = s
	}
	return `data_prevista IS NOT NULL AND data_prevista <= ? AND stato NOT IN (` + segni + `)
		AND ritardo_avvisato_il IS NULL AND cancellata_il IS NULL`, args
}()

func argomenti(confine clock.Data, prima ...any) []any {
	return append(append(prima, confine.String()), statiChiusi...)
}

// meno è il giorno n giorni prima di g.
func meno(g clock.Data, n int) clock.Data {
	t := time.Date(g.Anno, g.Mese, g.Giorno, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -n)
	return clock.Data{Anno: t.Year(), Mese: t.Month(), Giorno: t.Day()}
}

// Esegui è run/4.
func (r *LavoroRitardi) Esegui(ctx context.Context, conf Config, adesso time.Time, dest []uscita.Destinazione, a audit.Contesto) (Risultato, error) {
	if len(dest) == 0 {
		return Risultato{}, ErrSenzaCanali // il pianificatore non lo fa mai (D-16): niente timbri senza messaggi
	}
	oggi := clock.DataDi(adesso)
	silenziate, err := r.silenzia(ctx, adesso, oggi)
	if err != nil {
		return Risultato{}, err
	}
	confine := meno(oggi, conf.GiorniRitardi())
	ids, err := r.candidate(ctx, confine)
	if err != nil {
		return Risultato{}, err
	}
	var avvisi, messaggi, falliti int
	motivo := ""
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return Risultato{}, err
		}
		if r.primaDelTimbro != nil {
			r.primaDelTimbro(id)
		}
		scritti, perche, err := r.timbra(ctx, id, adesso, oggi, confine, dest, a)
		switch {
		case err != nil && ctx.Err() != nil:
			return Risultato{}, ctx.Err()
		case err != nil:
			return Risultato{}, err
		case perche != "":
			r.log.Warn("ritardi: avviso di una busta non riuscito", "busta", id, "motivo", perche)
			falliti++
			if motivo == "" {
				motivo = perche
			}
		case scritti > 0:
			avvisi++
			messaggi += scritti
		}
	}
	frase := fraseRitardi(avvisi, silenziate)
	if falliti > 0 {
		return Risultato{Fallito: true, Avvisi: avvisi, Messaggi: messaggi, Dettaglio: fmt.Sprintf("%s %d non riuscite: %s", frase, falliti, motivo)}, nil
	}
	return Risultato{Avvisi: avvisi, Messaggi: messaggi, Dettaglio: frase}, nil
}

// fraseRitardi è sentence/2 con silenced_note/1, parole dello standalone («1 busta storiche» compreso).
func fraseRitardi(avvisi, silenziate int) string {
	f := "Nessuna busta in ritardo."
	if avvisi > 0 {
		f = quanteBuste(avvisi) + " in ritardo."
	}
	if silenziate > 0 {
		f += " " + quanteBuste(silenziate) + " storiche silenziate alla prima esecuzione."
	}
	return f
}

func quanteBuste(n int) string {
	if n == 1 {
		return "1 busta"
	}
	return fmt.Sprintf("%d buste", n)
}

// silenzia è silence_history/2: senza la chiave, le buste in ritardo da GiorniStorico giorni o più si timbrano
// adesso, senza messaggi, e la chiave si scrive, in una transazione (un guasto a metà rifà tutto la volta dopo).
// La chiave si guarda nella transazione di scrittura, così due esecuzioni (anche di due processi) non silenziano
// due volte.
func (r *LavoroRitardi) silenzia(ctx context.Context, adesso time.Time, oggi clock.Data) (int, error) {
	tx, err := r.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	if fatto, err := storicoSilenziato(ctx, tx); err != nil || fatto {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE buste SET ritardo_avvisato_il = ? WHERE `+predicatoRitardo,
		argomenti(meno(oggi, GiorniStorico), adesso.UTC().Format(db.FormatoIstante))...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := segnaStoricoSilenziato(ctx, tx, adesso); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	// Il numero resta nel registro anche se l'esecuzione poi si interrompe e la riga non lo dice.
	r.log.Info("ritardi: storico silenziato alla prima esecuzione", "buste", n)
	return int(n), nil
}

// candidate è candidates/1: gli id delle buste in ritardo, per id. Il resto si rilegge nel timbro.
func (r *LavoroRitardi) candidate(ctx context.Context, confine clock.Data) ([]int64, error) {
	rs, err := r.d.Lettura.QueryContext(ctx, `SELECT id FROM buste WHERE `+predicatoRitardo+` ORDER BY id`, argomenti(confine)...)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var ids []int64
	for rs.Next() {
		var id int64
		if err := rs.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rs.Err()
}

// timbra è claim/6: l'UPDATE condizionato da tutto il predicato e, se la busta è ancora in ritardo e non
// segnalata, un messaggio per destinatario dalla riga timbrata, tutto in una transazione. scritti è quanti
// messaggi (0: la busta non si è timbrata); perche non vuoto è il rifiuto della coda (niente scritto).
func (r *LavoroRitardi) timbra(ctx context.Context, id int64, adesso time.Time, oggi, confine clock.Data, dest []uscita.Destinazione, a audit.Contesto) (scritti int, perche string, err error) {
	tx, err := r.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var numero, nome, descrizione, prevista string
	err = tx.QueryRowContext(ctx, `UPDATE buste SET ritardo_avvisato_il = ? WHERE id = ? AND `+predicatoRitardo+`
		RETURNING numero, nome_cliente, descrizione, data_prevista`,
		argomenti(confine, adesso.UTC().Format(db.FormatoIstante), id)...).Scan(&numero, &nome, &descrizione, &prevista)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", nil // timbrata da un altro, chiusa, spostata, tolta: niente
	}
	if err != nil {
		return 0, "", err
	}
	g, err := time.Parse(time.DateOnly, prevista)
	if err != nil {
		// Solo Go: la colonna accetta 2026-02-31 (GLOB), il tipo date di Ecto no. È un rifiuto della busta, non un
		// guasto: si annulla il timbro e si passa alla prossima.
		return 0, "data promessa non valida della busta " + numero, nil
	}
	cont := testoRitardo(numero, nome, descrizione, clock.Data{Anno: g.Year(), Mese: g.Month(), Giorno: g.Day()}, oggi)
	for _, d := range dest {
		if _, err := r.posta.AccodaIn(ctx, tx, d, cont, a); err != nil {
			switch {
			case errors.Is(err, uscita.ErrNonConfigurato):
				return 0, "il canale " + d.Canale + " non è più configurato", nil
			case errors.Is(err, uscita.ErrMessaggioNonValido), errors.Is(err, uscita.ErrSoloAMano):
				return 0, "la coda ha rifiutato il messaggio della busta " + numero, nil
			}
			return 0, "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, "", err
	}
	return len(dest), "", nil
}

// testoRitardo è content/2: format_ritardo dello standalone senza i tag (nessun parse_mode).
func testoRitardo(numero, nome, descrizione string, prevista, oggi clock.Data) uscita.Contenuto {
	righe := []string{
		"🕒 Busta in ritardo",
		"Busta " + numero + " — " + nome,
		fmt.Sprintf("Consegna prevista: %s (%dgg fa)", prevista.Formatta("02/01/2006"), oggi.Sub(prevista)),
	}
	if d := strings.TrimFunc(descrizione, unicode.IsSpace); d != "" {
		righe = append(righe, tagliaDescrizione(d))
	}
	oggetto := "Busta in ritardo " + numero
	return uscita.Contenuto{Oggetto: &oggetto, Testo: strings.Join(righe, "\n")}
}

// tagliaDescrizione è cut/1: oltre MassimoDescrizione grafemi, i primi MassimoDescrizione e «…».
func tagliaDescrizione(s string) string {
	if uniseg.GraphemeClusterCount(s) <= MassimoDescrizione {
		return s
	}
	g := uniseg.NewGraphemes(s)
	fine := 0
	for i := 0; i < MassimoDescrizione && g.Next(); i++ {
		_, fine = g.Positions()
	}
	return s[:fine] + "…"
}
