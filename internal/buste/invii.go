package buste

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
)

// «📨 Invii registrati» della scheda e «Togli dalla lista» (CLI-37, RIP-S-21), come
// Outgoing.history_for_repair/1, discardable_ids/1 e discard_handoff/3 dello standalone (go-baseline):
//   - la lista è tutto ciò che è stato registrato per la busta, di ogni canale (WhatsApp, e le righe
//     email e Telegram che arrivano dall'import), dalla più recente per id; resta leggibile anche se la
//     controparte ha perso il numero;
//   - si toglie solo una riga WhatsApp della busta (l'operatore l'ha preparata, ha aperto WhatsApp e
//     poi non ha mandato niente), mai quella su cui poggia un avviso di ritiro: quella ha la sua storia
//     in eventi_avviso_pronta e il suo azzeramento. Il timbro «link inviato» che la riga ha messo resta;
//   - togliere scrive outgoing.handoff_discarded con canale, parte e busta, mai il testo.

// Invio è una riga della lista «📨 Invii registrati».
type Invio struct {
	ID         int64
	Canale     string    // whatsapp, email, telegram
	Parte      string    // customer, oreficeria, artisan, shop
	Stato      string    // queued, accepted, failed
	Indirizzo  string    // come è salvato (il numero, l'email, la chat, o «contatto-scelto»)
	Testo      string    // com'è partito: la pagina maschera i link dell'email e toglie l'HTML di Telegram
	Il         time.Time // accettato_il, o accodato_il se non ancora accettato
	Togliibile bool      // una riga WhatsApp senza avviso di ritiro (discardable_ids/1)
}

// I rifiuti di «Togli dalla lista».
var (
	ErrInvioNonInLista   = errors.New("buste: messaggio non più in lista")       // :not_found
	ErrInvioAvvisoPronta = errors.New("buste: è l'avviso di ritiro della busta") // :notice
)

// inviiDella legge la lista della busta id, dalla più recente.
func inviiDella(ctx context.Context, q *sql.DB, id int64) ([]Invio, error) {
	righe, err := q.QueryContext(ctx, `SELECT m.id, m.canale, m.destinatario, m.stato, m.indirizzo, m.testo, coalesce(m.accettato_il, m.accodato_il),
		m.canale = 'whatsapp' AND NOT EXISTS (SELECT 1 FROM eventi_avviso_pronta e WHERE e.messaggio_in_uscita_id = m.id)
		FROM messaggi_in_uscita m WHERE m.busta_id = ? ORDER BY m.id DESC`, id)
	if err != nil {
		return nil, err
	}
	defer righe.Close()
	var invii []Invio
	for righe.Next() {
		var i Invio
		var il string
		if err := righe.Scan(&i.ID, &i.Canale, &i.Parte, &i.Stato, &i.Indirizzo, &i.Testo, &il, &i.Togliibile); err != nil {
			return nil, err
		}
		if i.Il, err = time.Parse(db.FormatoIstante, il); err != nil {
			return nil, err
		}
		invii = append(invii, i)
	}
	return invii, righe.Err()
}

// TogliInvio è discard_handoff/3 per la riga invio della busta id: in una transazione la cerca fra le
// righe WhatsApp della busta (ErrInvioNonInLista se non c'è), rifiuta quella di un avviso di ritiro
// (ErrInvioAvvisoPronta), la toglie e scrive outgoing.handoff_discarded. Una busta che non c'è o è nel
// cestino è ErrBustaNonTrovata (get_repair!/1 della scheda). Niente si scrive se rifiuta.
func (b *Buste) TogliInvio(ctx context.Context, id, invio int64, c audit.Contesto) error {
	return b.inTx(ctx, func(tx *sql.Tx, _ string) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM buste WHERE id = ? AND cancellata_il IS NULL`, id).Scan(&n); err != nil {
			return err
		} else if n == 0 {
			return ErrBustaNonTrovata
		}
		var parte string
		var avviso bool
		err := tx.QueryRowContext(ctx, `SELECT m.destinatario, EXISTS (SELECT 1 FROM eventi_avviso_pronta e WHERE e.messaggio_in_uscita_id = m.id)
			FROM messaggi_in_uscita m WHERE m.id = ? AND m.busta_id = ? AND m.canale = 'whatsapp'`, invio, id).Scan(&parte, &avviso)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return ErrInvioNonInLista
		case err != nil:
			return err
		case avviso:
			return ErrInvioAvvisoPronta
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM messaggi_in_uscita WHERE id = ?`, invio); err != nil {
			return err
		}
		return b.reg.Scrivi(ctx, tx, c, "outgoing.handoff_discarded", "outgoing_message", invio, "ok",
			map[string]any{"channel": "whatsapp", "party": parte, "repair_id": id})
	})
}
