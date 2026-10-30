package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La conservazione dell'audit (ADM-17, D-14 di Stefano, 29/10/2026): gli eventi più vecchi di 5
// anni si cancellano con un lavoro mensile. Lo standalone li tiene per sempre. Il lavoro tocca solo
// eventi_audit e la sua riga di stato in impostazioni: il registro di emergenza (EME) resta fuori.
//
// Regole:
//   - «più vecchio di 5 anni»: creato_il prima dello stesso giorno e della stessa ora di 5 anni fa
//     sul calendario del negozio (LimiteConservazione); un evento di esattamente 5 anni resta;
//   - guardia contro l'orologio avanti: il limite non va oltre l'evento più recente meno 5 anni, e
//     con adesso oltre l'evento più recente di più di MargineOrologio il giro si rimanda
//     (limiteEffettivo);
//   - un giro per mese del calendario del negozio; serve controlla all'avvio (recupero dopo lo
//     spegnimento) e poi ogni OgniQuantoConservazione;
//   - si cancella a lotti di LottoConservazione righe, una transazione per lotto, così il
//     collegamento di scrittura non resta preso a lungo;
//   - lo stato del giro (mese, limite, totale, finito) sta in impostazioni e si legge e si scrive
//     in ogni transazione: due processi insieme fanno un solo giro, un giro interrotto riprende
//     con lo stesso limite e lo stesso totale, anche nel mese dopo (prima si chiude quello);
//   - l'ultimo lotto scrive, nella sua transazione, una riga audit.retention_applied con
//     {"deleted": n, "cutoff": limite}, anche con n = 0; la riga è di adesso, quindi il giro non
//     la cancella.

const (
	// AnniConservazione: gli eventi più vecchi si cancellano (D-14).
	AnniConservazione = 5
	// LottoConservazione è quante righe si cancellano per transazione.
	LottoConservazione = 1000
	// MargineOrologio: oltre questo distacco tra adesso e l'evento più recente, il giro si rimanda.
	MargineOrologio = 31 * 24 * time.Hour
	// OgniQuantoConservazione è ogni quanto il lavoro controlla se il mese è fatto.
	OgniQuantoConservazione = time.Hour
	// ChiaveConservazione è la riga di impostazioni con lo stato del giro del mese.
	ChiaveConservazione = "audit.conservazione"
	// AzioneConservazione è la riga d'audit che chiude il giro del mese.
	AzioneConservazione = "audit.retention_applied"
	// attoreConservazione è l'attore dei lavori di pulizia nello standalone (PhotoRetention,
	// Assist.Training.expire).
	attoreConservazione = "retention"
)

// Conservazione è il lavoro mensile che cancella gli eventi più vecchi di 5 anni.
type Conservazione struct {
	d     *db.DB
	o     clock.Orologio
	r     *Registro
	lotto int
	// traLotti, se c'è, si chiama dopo ogni lotto che non chiude il giro (per i test).
	traLotti func()
}

// EsitoConservazione dice com'è andato un controllo. Girata è vera solo per il controllo che ha
// chiuso il giro del mese; Cancellati è allora il totale del giro, anche dei lotti fatti prima di
// un'interruzione o da un altro processo.
type EsitoConservazione struct {
	Girata     bool
	Rimandata  bool
	PiuRecente time.Time // con Rimandata: l'evento più recente
	Cancellati int64
	Limite     time.Time
}

// NuovaConservazione dà il lavoro del database d con l'orologio o.
func NuovaConservazione(d *db.DB, o clock.Orologio) *Conservazione {
	return &Conservazione{d: d, o: o, r: Nuovo(d, o), lotto: LottoConservazione}
}

// LimiteConservazione è l'istante prima del quale un evento si cancella: adesso meno 5 anni del
// calendario del negozio (Europe/Rome), in UTC. Un 29 febbraio che 5 anni prima non c'è diventa
// il 1° marzo.
func LimiteConservazione(adesso time.Time) time.Time {
	return clock.Locale(adesso).AddDate(-AnniConservazione, 0, 0).UTC()
}

// limiteEffettivo è il limite di un giro nuovo, letto nella sua transazione: il minimo tra adesso e
// l'evento più recente, meno 5 anni, così non si cancella niente che i dati stessi dicono più
// giovane di 5 anni. ok è false (giro rimandato) se adesso è oltre l'evento più recente di più
// di MargineOrologio: l'orologio del sistema è probabilmente avanti (apparecchio senza RTC, NTP
// non ancora sincronizzato all'avvio). Le righe del giro non contano: le scrive il lavoro stesso,
// anche con l'orologio sbagliato. Senza eventi non c'è niente da cancellare né da confrontare.
func limiteEffettivo(ctx context.Context, tx *sql.Tx, adesso time.Time) (limite, recente time.Time, ok bool, err error) {
	limite = LimiteConservazione(adesso)
	var v sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT max(creato_il) FROM eventi_audit WHERE azione <> ?`, AzioneConservazione).Scan(&v); err != nil {
		return
	}
	if !v.Valid {
		return limite, recente, true, nil
	}
	if recente, err = time.Parse(db.FormatoIstante, v.String); err != nil {
		err = fmt.Errorf("conservazione dell'audit: evento più recente: %w", err)
		return
	}
	if adesso.Sub(recente) > MargineOrologio {
		return limite, recente, false, nil
	}
	if l := LimiteConservazione(recente); l.Before(limite) {
		limite = l
	}
	return limite, recente, true, nil
}

// statoConservazione è il valore della riga ChiaveConservazione.
type statoConservazione struct {
	Mese       string `json:"mese"`
	Limite     string `json:"limite"`
	Cancellati int64  `json:"cancellati"`
	Finito     bool   `json:"finito"`
}

type lettore interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

func leggiStato(ctx context.Context, l lettore) (statoConservazione, error) {
	var v string
	err := l.QueryRowContext(ctx, `SELECT valore FROM impostazioni WHERE chiave = ?`, ChiaveConservazione).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return statoConservazione{}, nil
	}
	if err != nil {
		return statoConservazione{}, fmt.Errorf("conservazione dell'audit: stato: %w", err)
	}
	var s statoConservazione
	if err := json.Unmarshal([]byte(v), &s); err != nil {
		return statoConservazione{}, fmt.Errorf("conservazione dell'audit: stato illeggibile in impostazioni %s: %w", ChiaveConservazione, err)
	}
	return s, nil
}

// meseDi è il mese del calendario del negozio, aaaa-mm.
func meseDi(t time.Time) string {
	d := clock.DataDi(t)
	return fmt.Sprintf("%04d-%02d", d.Anno, d.Mese)
}

// Applica fa il giro del mese, se non è già fatto, o riprende quello interrotto.
func (c *Conservazione) Applica(ctx context.Context) (EsitoConservazione, error) {
	adesso := c.o.Adesso()
	mese := meseDi(adesso)
	// Il caso di tutte le ore (mese già fatto) non prende il collegamento di scrittura.
	s, err := leggiStato(ctx, c.d.Lettura)
	if err != nil {
		return EsitoConservazione{}, err
	}
	if s.Mese == mese && s.Finito {
		return EsitoConservazione{}, nil
	}
	for {
		e, fatto, err := c.unLotto(ctx, adesso, mese)
		if err != nil || fatto {
			return e, err
		}
		if c.traLotti != nil {
			c.traLotti()
		}
	}
}

// unLotto cancella un lotto in una transazione, con il controllo del mese dentro la transazione;
// fatto dice che il giro del mese è chiuso, da questa transazione (Girata) o da un altro processo.
func (c *Conservazione) unLotto(ctx context.Context, adesso time.Time, mese string) (e EsitoConservazione, fatto bool, err error) {
	tx, err := c.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return EsitoConservazione{}, false, err
	}
	// Il rollback anche dopo un panico: una transazione rimasta aperta terrebbe per sempre
	// l'unico collegamento di scrittura. Dopo il commit non fa niente.
	defer func() {
		_ = tx.Rollback()
		if err != nil {
			e, fatto = EsitoConservazione{}, false
		}
	}()
	s, err := leggiStato(ctx, tx)
	if err != nil {
		return
	}
	if s.Mese == mese && s.Finito {
		return EsitoConservazione{}, true, tx.Commit()
	}
	// Un giro non finito, di questo mese o di uno prima, continua con il suo limite e il suo
	// totale; altrimenti parte quello di questo mese.
	if s.Mese == "" || s.Finito {
		var lim, recente time.Time
		var ok bool
		if lim, recente, ok, err = limiteEffettivo(ctx, tx, adesso); err != nil {
			return
		}
		if !ok {
			// Niente cancellato, niente scritto: il rollback del defer chiude la transazione.
			return EsitoConservazione{Rimandata: true, PiuRecente: recente}, true, nil
		}
		s = statoConservazione{Mese: mese, Limite: lim.Format(db.FormatoIstante)}
	}
	limite, err := time.Parse(db.FormatoIstante, s.Limite)
	if err != nil {
		err = fmt.Errorf("conservazione dell'audit: limite illeggibile nello stato: %w", err)
		return
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM eventi_audit WHERE id IN (SELECT id FROM eventi_audit WHERE creato_il < ? ORDER BY creato_il LIMIT ?)`,
		s.Limite, c.lotto)
	if err != nil {
		err = fmt.Errorf("conservazione dell'audit: cancellazione: %w", err)
		return
	}
	n, err := res.RowsAffected()
	if err != nil {
		return
	}
	s.Cancellati += n
	if n < int64(c.lotto) {
		// Chiuso il giro di un mese prima, il lotto dopo comincia quello di questo mese.
		s.Finito, fatto = true, s.Mese == mese
		if err = c.r.Scrivi(ctx, tx, Contesto{Attore: attoreConservazione}, AzioneConservazione, "audit", "events", "ok",
			map[string]any{"deleted": s.Cancellati, "cutoff": s.Limite}); err != nil {
			return
		}
	}
	v, err := json.Marshal(s)
	if err != nil {
		return
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, ?)
		ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore, aggiornata_il = excluded.aggiornata_il`,
		ChiaveConservazione, string(v), c.o.Adesso().UTC().Format(db.FormatoIstante)); err != nil {
		err = fmt.Errorf("conservazione dell'audit: stato: %w", err)
		return
	}
	if err = tx.Commit(); err != nil {
		return
	}
	if fatto {
		e = EsitoConservazione{Girata: true, Cancellati: s.Cancellati, Limite: limite}
	}
	return e, fatto, nil
}

// Lavoro è il lavoro di serve: controlla subito (il recupero dopo uno spegnimento) e poi dopo
// ogni attesa, finché ctx non finisce o aspetta dice false. Un errore o un panico vanno in log e
// il controllo dopo riprova, come MediaCleanup dello standalone: il lavoro non ferma serve.
func (c *Conservazione) Lavoro(ctx context.Context, aspetta func(context.Context) bool, log *slog.Logger) {
	for {
		c.giro(ctx, log)
		if !aspetta(ctx) {
			return
		}
	}
}

func (c *Conservazione) giro(ctx context.Context, log *slog.Logger) {
	defer func() {
		if p := recover(); p != nil {
			log.Error("conservazione dell'audit: giro interrotto da un panico, si riprova al prossimo controllo", "panico", fmt.Sprint(p))
		}
	}()
	e, err := c.Applica(ctx)
	switch {
	case err != nil && ctx.Err() != nil:
		// Lo spegnimento: i lotti fatti restano, il giro riprende al prossimo avvio.
	case err != nil:
		log.Error("conservazione dell'audit: giro non riuscito, si riprova al prossimo controllo", "errore", err)
	case e.Rimandata:
		log.Warn("conservazione dell'audit: giro rimandato, l'orologio del sistema è oltre l'evento più recente di più di 31 giorni: niente cancellato",
			"adesso", c.o.Adesso().UTC().Format(db.FormatoIstante), "piu_recente", e.PiuRecente.UTC().Format(db.FormatoIstante))
	case e.Girata:
		log.Info("conservazione dell'audit: giro del mese fatto", "cancellati", e.Cancellati, "limite", e.Limite.Format(db.FormatoIstante))
	}
}
