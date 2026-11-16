package buste

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// I lotti d'avviso alle oreficerie (slice 088, ORE-04 e ORE-05), come ReadyLots.open_lot/2, list_open_lots/0 e
// purge_stale_empty_lots/0 dello standalone (ready_lots.ex): la pulizia dei lotti vuoti dei giorni prima, i
// lotti aperti di oggi e l'apertura (o la ripresa) del lotto di oggi di un'oreficeria. Nessun controllo di
// ruolo (titolare e dipendente).
//
// Un lotto è «aperto» se non è né inviato né chiuso (ReadyLot.open_query/1). «Oggi» è il giorno del negozio
// (clock.Oggi, Europe/Rome) dalla sua mezzanotte (inizioDiOggi, come start_of_local_day/1): un lotto aperto
// esattamente a mezzanotte è di oggi. Un lotto aperto ieri e non vuoto non è più nella lista né si riprende,
// come nello standalone (Stefano, 15/11/2026: tenuto fra le idee).
//
// D-94 (Stefano, 15/11/2026): la ricerca del lotto di oggi e la scrittura del nuovo stanno nella stessa
// transazione di scrittura (BEGIN IMMEDIATE, una sola connessione di scrittura), quindi due aperture nello
// stesso istante per la stessa oreficeria danno un lotto solo; lo standalone legge e poi scrive e può
// crearne due.

// LottoAperto è una riga di «Lotti aperti oggi»: Buste sono quelle fuori dal cestino.
type LottoAperto struct {
	ID         int64
	Oreficeria string
	Buste      int
	Aperto     time.Time
}

// I rifiuti dell'apertura, con i testi di ReadyLots.refusal_message/1 (:in_house, :unknown_goldsmith).
const (
	ErrLottoOreficeriaPrincipale Rifiuto = "Non si può creare un avviso per l'oreficeria principale."
	ErrLottoOreficeriaNonTrovata Rifiuto = "Oreficeria non trovata."
)

// byteGettoneLotto: secrets.token_urlsafe(24) del Flask, 24 byte e 32 caratteri url-safe.
const byteGettoneLotto = 24

// condLottoAperto è ReadyLot.open_query/1.
const condLottoAperto = `l.inviato_il IS NULL AND l.chiuso_il IS NULL`

// PulisciLottiVuoti è purge_stale_empty_lots/0: cancella i lotti aperti dei giorni prima senza nessuna busta
// (anche nel cestino, che si può ripristinare) e dice quanti. Nessuna riga d'audit, come lo standalone.
func (b *Buste) PulisciLottiVuoti(ctx context.Context) (int64, error) {
	res, err := b.d.Scrittura.ExecContext(ctx, `DELETE FROM lotti_avviso AS l WHERE `+condLottoAperto+` AND l.creato_il < ?
		AND NOT EXISTS (SELECT 1 FROM buste x WHERE x.lotto_avviso_id = l.id)`, b.inizioDiOggi())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// LottiApertiOggi è list_open_lots/0: i lotti aperti di oggi, dal più recente, con il nome dell'oreficeria e le
// buste fuori dal cestino.
func (b *Buste) LottiApertiOggi(ctx context.Context) ([]LottoAperto, error) {
	rr, err := b.d.Lettura.QueryContext(ctx, `SELECT l.id, o.nome,
		(SELECT count(*) FROM buste x WHERE x.lotto_avviso_id = l.id AND x.cancellata_il IS NULL), l.creato_il
		FROM lotti_avviso l JOIN oreficerie o ON o.id = l.oreficeria_id
		WHERE `+condLottoAperto+` AND l.creato_il >= ? ORDER BY l.id DESC`, b.inizioDiOggi())
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	var out []LottoAperto
	for rr.Next() {
		var l LottoAperto
		var il string
		if err := rr.Scan(&l.ID, &l.Oreficeria, &l.Buste, &il); err != nil {
			return nil, err
		}
		if l.Aperto, err = time.Parse(db.FormatoIstante, il); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rr.Err()
}

// ApriLotto è open_lot/2: la riga 1 e ogni id non esterno sono ErrLottoOreficeriaPrincipale (Oreficeria.is_b2b/1),
// un'oreficeria che non c'è ErrLottoOreficeriaNonTrovata; poi riprende il lotto aperto di oggi dell'oreficeria (il
// più recente) o ne crea uno nuovo con il gettone pubblico e la riga ready_lot.opened, nella stessa transazione
// (D-94). Dà l'id del lotto.
func (b *Buste) ApriLotto(ctx context.Context, oreficeria int64, c audit.Contesto) (int64, error) {
	if oreficeria <= 1 {
		return 0, ErrLottoOreficeriaPrincipale
	}
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM oreficerie WHERE id = ?`, oreficeria).Scan(&n); err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, ErrLottoOreficeriaNonTrovata
	}
	// Un'occhiata sola all'orologio: «oggi» e l'istante del lotto nuovo sono dello stesso momento, anche a cavallo
	// della mezzanotte.
	adesso, ora := b.adesso()
	var id int64
	err = tx.QueryRowContext(ctx, `SELECT l.id FROM lotti_avviso l WHERE l.oreficeria_id = ? AND `+condLottoAperto+`
		AND l.creato_il >= ? ORDER BY l.id DESC LIMIT 1`, oreficeria, clock.DataDi(adesso).Inizio().Format(db.FormatoIstante)).Scan(&id)
	switch {
	case err == nil:
		return id, tx.Commit()
	case !errors.Is(err, sql.ErrNoRows):
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO lotti_avviso (oreficeria_id, token, creato_il, aggiornato_il) VALUES (?, ?, ?, ?)`,
		oreficeria, gettoneLotto(), ora, ora)
	if err != nil {
		return 0, err
	}
	if id, err = res.LastInsertId(); err != nil {
		return 0, err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "ready_lot.opened", "ready_lot", id, "ok", map[string]any{"oreficeria_id": oreficeria}); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// gettoneLotto è mint_token/0: 24 byte casuali in base64 url-safe senza «=».
func gettoneLotto() string {
	g := make([]byte, byteGettoneLotto)
	_, _ = rand.Read(g) // crypto/rand.Read non fallisce
	return base64.RawURLEncoding.EncodeToString(g)
}
