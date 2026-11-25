package buste

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
)

// La raccolta dei dettati dalla scheda della busta e la loro durata (slice 115; RIP-S-26, RIP-H-51 parte durata), come
// l'evento training-exclude-customer di RepairLive e Training.started?/0, excluded_customer?/2 ed expire/1 dello
// standalone a go-baseline:
//   - «🎙️ Escludi dalla raccolta dettati» della scheda: il cliente della busta si oppone (art. 21). Dalla scheda la busta
//     è già nota, quindi niente ricerca per numero: anche una busta gemella (stessa etichetta, D-109) esclude il suo
//     cliente, non quello della gemella. Lo standalone passa il numero della busta a exclude_customer/2, che con le
//     gemelle non ne sceglie una (Repo.one solleva). Il cuore è quello della pagina (escludiCliente, slice 114);
//   - l'esclusione dalla scheda c'è solo se la raccolta è stata accesa almeno una volta (la chiave del lotto
//     assist.training.remaining esiste: la scrive la prima accensione e niente la toglie) e solo per un cliente non
//     ancora escluso; altrimenti non scrive niente, né riga né audit (lo standalone ignora l'evento quando il pulsante
//     non c'è: training_offer != :offer). Il ruolo lo controlla la rotta;
//   - la scadenza: i dettati raccolti prima di adesso meno 12 mesi del calendario (UTC, come DateTime.shift(now, month:
//     -12): il 29 febbraio diventa il 28) si cancellano in una transazione, con l'audit assist.training_expired
//     dell'attore «retention» e il numero, solo se ne ha cancellato almeno uno. La gira la pulizia giornaliera
//     (media.Pulizia, run_training_expiry/3). Lo standalone cancella nello stesso giro anche i dettati delle buste nel
//     cestino: quella parte è della T4.11, con il cestino.
//
// Riservatezza: niente di quello che sta in dettati_addestramento passa di qui; gli errori non portano valori.

// MesiDettati è @max_age_months di Training: quanto vive un dettato raccolto.
const MesiDettati = 12

// attoreScadenza è l'attore dell'audit della scadenza (%{actor: "retention"}).
const attoreScadenza = "retention"

// azioneScadenza è la riga d'audit della scadenza dei dettati (assist.training_expired dello standalone).
const azioneScadenza = "assist.training_expired"

// EscludiClienteDellaBusta è training-exclude-customer della scheda: il cliente della busta (fuori dal cestino) si
// oppone alla raccolta. Dà quanti dettati sono stati cancellati e fatto = true; fatto = false se non ha scritto niente
// (raccolta mai accesa, o cliente già escluso: il secondo tocco). ErrBustaNonTrovata per una busta che non c'è o nel
// cestino.
func (b *Buste) EscludiClienteDellaBusta(ctx context.Context, busta int64, c audit.Contesto) (cancellati int64, fatto bool, err error) {
	err = b.inTransazioneTx(ctx, func(tx *sql.Tx, ora string) error {
		var cliente int64
		var avviata, escluso bool
		err := tx.QueryRowContext(ctx, `SELECT b.cliente_id,
			EXISTS (SELECT 1 FROM impostazioni WHERE chiave = ?),
			EXISTS (SELECT 1 FROM dettati_esclusioni e WHERE e.cliente_id = b.cliente_id)
			FROM buste b WHERE b.id = ? AND b.cancellata_il IS NULL`, ChiaveRaccoltaRimaste, busta).Scan(&cliente, &avviata, &escluso)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return ErrBustaNonTrovata
		case err != nil:
			return err
		case !avviata || escluso:
			return nil
		}
		fatto = true
		cancellati, err = b.escludiCliente(ctx, tx, cliente, ora, c)
		return err
	})
	if err != nil {
		return 0, false, err
	}
	return cancellati, fatto, nil
}

// LimiteDettati è DateTime.shift(adesso, month: -MesiDettati) al microsecondo, in UTC: un dettato raccolto prima di
// questo istante è scaduto. Il giorno che non c'è nel mese di arrivo diventa l'ultimo del mese (29 febbraio → 28).
func LimiteDettati(adesso time.Time) time.Time {
	t := adesso.UTC().Truncate(time.Microsecond)
	a, m, g := t.Date()
	mesi := int(m) - 1 - MesiDettati
	a += mesi / 12
	if mesi %= 12; mesi < 0 {
		mesi += 12
		a--
	}
	m = time.Month(mesi + 1)
	if ultimo := time.Date(a, m+1, 0, 0, 0, 0, 0, time.UTC).Day(); g > ultimo {
		g = ultimo
	}
	return time.Date(a, m, g, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
}

// ScadenzaDettati è Training.expire/1 con l'orologio delle buste: cancella i dettati raccolti prima di LimiteDettati e dice
// quanti; l'audit solo se sono più di zero. Tutto in una transazione: un errore (anche dell'audit) non cancella niente.
//
// Più prudente dello standalone (D-110): con l'orologio andato avanti rispetto all'evento d'audit più recente
// (audit.OrologioAvanti, la guardia della conservazione dell'audit) o dopo un import senza backup verificato
// (db.BloccatoDopoImport, come la coda delle cancellazioni dei media) non cancella niente, non scrive niente e non dà
// errore: la pulizia segna il giorno fatto e riprova il giorno dopo. Il limite è il minimo fra adesso e l'evento d'audit più
// recente, meno 12 mesi (D-110 punto 3).
func (b *Buste) ScadenzaDettati(ctx context.Context) (int64, error) {
	adesso, _ := b.adesso()
	// Il ricontrollo dell'archivio del backup dopo un import (slice 122) può leggere qualche GB: fuori dalla
	// transazione. Dentro, BloccatoDopoImport usa solo la memoria.
	if err := db.PreparaBloccoImport(ctx, b.d.Lettura); err != nil {
		return 0, err
	}
	var cancellati int64
	err := b.inTransazioneTx(ctx, func(tx *sql.Tx, _ string) error {
		if bloccata, err := db.BloccatoDopoImport(ctx, tx); err != nil || bloccata {
			return err
		}
		// Le righe assist.training_expired le scrive questo lavoro: non contano come evento più recente (D-110 punto 3).
		recente, avanti, err := audit.OrologioAvanti(ctx, tx, adesso, azioneScadenza)
		if err != nil || avanti {
			return err
		}
		// D-110 punto 3: il limite non va oltre l'evento d'audit più recente meno 12 mesi, come la conservazione
		// dell'audit con i suoi 5 anni (l'orologio avanti di meno di MargineOrologio non anticipa la scadenza).
		lim := LimiteDettati(adesso)
		if !recente.IsZero() {
			if l := LimiteDettati(recente); l.Before(lim) {
				lim = l
			}
		}
		limite := lim.Format(db.FormatoIstante)
		// creato_il ha sempre la forma di db.FormatoIstante (CHECK dello schema): il confronto fra testi è quello fra istanti.
		r, err := tx.ExecContext(ctx, `DELETE FROM dettati_addestramento WHERE creato_il < ?`, limite)
		if err != nil {
			return err
		}
		if cancellati, err = r.RowsAffected(); err != nil || cancellati == 0 {
			return err
		}
		return b.reg.Scrivi(ctx, tx, audit.Contesto{Attore: attoreScadenza}, azioneScadenza, "assist", "training", "ok",
			map[string]any{"cancellati": cancellati})
	})
	if err != nil {
		return 0, err
	}
	return cancellati, nil
}
