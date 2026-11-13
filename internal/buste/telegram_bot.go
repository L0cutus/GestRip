package buste

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// Le iscrizioni che il bot Telegram tocca (slice 073, CLI-46), come TelegramSubscriptions.start/2,
// consent/3 e stop_chat/1 dello standalone a go-baseline (slice 135 di lì): le Buste sono le
// uscita.IscrizioniTelegram del lettore getUpdates.
//   - /start <token> usa il token della 072 una volta sola (la riga se ne va nella stessa transazione) e
//     iscrive la chat alla busta con il consenso **spento**, anche se era già iscritta (lo standalone
//     riapre la riga e rimette il consenso a spento; la fonte teneva quello vecchio);
//   - il pulsante scrive il consenso solo per la chat dell'iscrizione e solo se l'iscrizione non è fermata:
//     un pulsante vecchio non riaccende; la stessa risposta due volte non scrive;
//   - /stop e /disattiva spengono ogni iscrizione ancora attiva della chat (motivo «stop», via «bot»);
//   - l'audit (repair.telegram_linked, repair.telegram_consent, repair.telegram_stop) con l'attore
//     «telegram», la busta e l'iscrizione: mai la chat, il token o quello che la persona ha scritto.

// attoreBot è @bot di TelegramSubscriptions: chi scrive le righe d'audit del bot.
var attoreBot = audit.Contesto{Attore: "telegram"}

// massimoTokenStart è il limite del parametro start di Telegram (byte_size(token) <= 64 di start/2).
const massimoTokenStart = 64

// AvviaTelegram è start/2: l'id dell'iscrizione della chat alla busta del token, o
// uscita.ErrLinkTelegramNonValido per un token vuoto, troppo lungo, ignoto, già usato, scaduto, o di una
// busta che non è più del cliente diretto e aperta (o è nel cestino). In quest'ultimo caso il token si
// consuma lo stesso, come lo standalone.
func (b *Buste) AvviaTelegram(ctx context.Context, chat int64, token string) (int64, error) {
	if token == "" || len(token) > massimoTokenStart {
		return 0, uscita.ErrLinkTelegramNonValido
	}
	_, ora := b.adesso()
	h := sha256.Sum256([]byte(token))
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var riga, busta int64
	err = tx.QueryRowContext(ctx, `SELECT id, busta_id FROM token_telegram WHERE hash_token = ? AND scade_il > ?`, h[:], ora).Scan(&riga, &busta)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, uscita.ErrLinkTelegramNonValido
	}
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM token_telegram WHERE id = ?`, riga); err != nil {
		return 0, err
	}
	bt, err := leggiBustaTelegram(ctx, tx, busta)
	switch {
	case errors.Is(err, ErrBustaNonTrovata):
		return 0, errors.Join(uscita.ErrLinkTelegramNonValido, tx.Commit())
	case err != nil:
		return 0, err
	case bt.visibile(false) != nil:
		return 0, errors.Join(uscita.ErrLinkTelegramNonValido, tx.Commit())
	}
	id, err := iscriviChat(ctx, tx, chat, busta, ora)
	if err != nil {
		return 0, err
	}
	if err := b.reg.Scrivi(ctx, tx, attoreBot, "repair.telegram_linked", "repair", busta, "ok", map[string]any{
		"bag_number": bt.numero, "subscription_id": id,
	}); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// iscriviChat è subscribe/3: la riga della chat e della busta, nuova o riaperta, con il consenso spento e
// senza risposta, non fermata, collegata adesso.
func iscriviChat(ctx context.Context, tx *sql.Tx, chat, busta int64, ora string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM iscrizioni_telegram WHERE chat_id = ? AND busta_id = ?`, chat, busta).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		r, err := tx.ExecContext(ctx, `INSERT INTO iscrizioni_telegram (busta_id, chat_id, consenso, collegata_il, creato_il, aggiornato_il)
			VALUES (?1, ?2, 0, ?3, ?3, ?3)`, busta, chat, ora)
		if err != nil {
			return 0, err
		}
		return r.LastInsertId()
	case err != nil:
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE iscrizioni_telegram SET consenso = 0, consenso_il = NULL, fermata_il = NULL, motivo_fermata = NULL,
		collegata_il = ?1, aggiornato_il = ?1 WHERE id = ?2`, ora, id)
	return id, err
}

// ConsensoTelegram è consent/3: il consenso valore sull'iscrizione id, solo se è della chat e non è
// fermata (altrimenti uscita.ErrIscrizioneTelegramNonTrovata); la stessa risposta già data non scrive
// niente. repair.telegram_consent con la busta, l'iscrizione e il consenso.
func (b *Buste) ConsensoTelegram(ctx context.Context, chat, id int64, valore bool) error {
	_, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var busta int64
	var consenso int
	var risposto bool
	var numero sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT i.busta_id, i.consenso, i.consenso_il IS NOT NULL, b.numero FROM iscrizioni_telegram i
		JOIN buste b ON b.id = i.busta_id WHERE i.id = ? AND i.chat_id = ? AND i.fermata_il IS NULL`, id, chat).Scan(&busta, &consenso, &risposto, &numero)
	if errors.Is(err, sql.ErrNoRows) {
		return uscita.ErrIscrizioneTelegramNonTrovata
	}
	if err != nil {
		return err
	}
	nuovo := 0
	if valore {
		nuovo = 1
	}
	if consenso == nuovo && risposto {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE iscrizioni_telegram SET consenso = ?1, consenso_il = ?2, aggiornato_il = ?2 WHERE id = ?3`,
		nuovo, ora, id); err != nil {
		return err
	}
	if err := b.reg.Scrivi(ctx, tx, attoreBot, "repair.telegram_consent", "repair", busta, "ok", map[string]any{
		"bag_number": numeroONil(numero), "subscription_id": id, "consent": valore,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// FermaChatTelegram è stop_chat/1: ogni iscrizione ancora attiva della chat si spegne (consenso 0, fermata
// adesso, motivo «stop») con repair.telegram_stop (via «bot») per ognuna. Dà quante, e se la chat aveva
// almeno un'iscrizione (anche già fermata): una chat sconosciuta riceve la risposta fissa.
func (b *Buste) FermaChatTelegram(ctx context.Context, chat int64) (int, bool, error) {
	_, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var conosciuta bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM iscrizioni_telegram WHERE chat_id = ?)`, chat).Scan(&conosciuta); err != nil {
		return 0, false, err
	}
	if !conosciuta {
		return 0, false, nil
	}
	righe, err := tx.QueryContext(ctx, `SELECT i.id, i.busta_id, b.numero FROM iscrizioni_telegram i LEFT JOIN buste b ON b.id = i.busta_id
		WHERE i.chat_id = ? AND i.fermata_il IS NULL ORDER BY i.id`, chat)
	if err != nil {
		return 0, false, err
	}
	type attiva struct {
		id, busta int64
		numero    sql.NullString
	}
	var attive []attiva
	for righe.Next() {
		var a attiva
		if err := righe.Scan(&a.id, &a.busta, &a.numero); err != nil {
			_ = righe.Close() // l'errore che conta è quello della lettura
			return 0, false, err
		}
		attive = append(attive, a)
	}
	if err := righe.Err(); err != nil {
		return 0, false, err
	}
	for _, a := range attive {
		if _, err := tx.ExecContext(ctx, `UPDATE iscrizioni_telegram SET consenso = 0, fermata_il = ?1, motivo_fermata = 'stop',
			aggiornato_il = ?1 WHERE id = ?2`, ora, a.id); err != nil {
			return 0, false, err
		}
		if err := b.reg.Scrivi(ctx, tx, attoreBot, azioneStopTelegram, "repair", a.busta, "ok", map[string]any{
			"bag_number": numeroONil(a.numero), "subscription_id": a.id, "via": "bot",
		}); err != nil {
			return 0, false, err
		}
	}
	return len(attive), true, tx.Commit()
}

func numeroONil(n sql.NullString) any {
	if n.Valid {
		return n.String
	}
	return nil
}

// NomeNegozio è OnlineSales.shop_name/0: il nome del profilo del negozio, «Negozio» senza profilo o con il
// nome vuoto.
func (b *Buste) NomeNegozio(ctx context.Context) (string, error) {
	p, err := b.LeggiProfiloNegozio(ctx)
	if err != nil {
		return "", err
	}
	if p.Presente && p.Nome != "" {
		return p.Nome, nil
	}
	return "Negozio", nil
}
