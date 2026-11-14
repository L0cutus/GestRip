package buste

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"

	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// Le risposte del cliente a una notifica Telegram (CLI-49, slice 075), come TelegramReplies dello standalone
// a go-baseline (slice 138 di lì): il cliente che risponde con «Rispondi» a una notifica della busta scrive
// nella conversazione della busta, come il messaggio della pagina.
//   - **La corrispondenza** messaggio → busta è la riga della notifica nella coda in uscita: la chat a cui è
//     andata (indirizzo), il message_id che Telegram ha dato (riferimento_provider) e la busta, su una riga
//     Telegram del cliente accettata. Niente tabella sua: sarebbe una seconda copia della chat. Dura quanto la
//     riga tiene la chat: 90 giorni, poi la pulizia la cancella (uscita.LettoreTelegram.Pulisci), e una riga
//     più vecchia si rifiuta anche prima. Valgono le notifiche del cambio di stato e del nuovo messaggio.
//   - **Il messaggio** è quello della pagina (scriviMessaggio dal lato Telegram): ripulito, al più 1000
//     caratteri, dieci all'ora per busta contati con quelli della pagina, una riga del cliente senza canale,
//     la busta non letta, l'avviso alla chat del negozio (CLI-13). Rifiutato su una busta consegnata,
//     annullata o chiusa, e su una busta che non è più del cliente diretto (di un'oreficeria o nel cestino):
//     il cliente non scrive nella conversazione di un altro. Una chat che ha fatto /stop risponde ancora
//     (scrivere è un atto suo, /stop riguarda quello che manda il negozio); una tagliata con il link della
//     busta no, finché non si iscrive di nuovo con il link nuovo.
//   - L'audit è quello della conversazione (repair.message_appended) con l'attore «telegram»: mai la chat.
// Il limite del bot (otto al minuto per chat) e il rifiuto di quello che non è testo sono del lettore.

// BustaDellaRisposta è TelegramReplies.bag/2: la busta della notifica messaggio nella chat, o 0: nessuna
// notifica così, una che Telegram non ha accettato, di un'altra chat, accodata più di 90 giorni fa, o di una
// busta che ha tagliato la chat con il link.
func (b *Buste) BustaDellaRisposta(ctx context.Context, chat, messaggio int64) (int64, error) {
	adesso, _ := b.adesso()
	limite := adesso.Add(-uscita.ConservazioneTelegram).Format(db.FormatoIstante)
	var busta int64
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT busta_id FROM messaggi_in_uscita WHERE canale = 'telegram' AND destinatario = 'customer'
		AND stato = 'accepted' AND riferimento_provider = ? AND indirizzo = ? AND accodato_il >= ? AND busta_id IS NOT NULL
		ORDER BY id DESC LIMIT 1`, strconv.FormatInt(messaggio, 10), strconv.FormatInt(chat, 10), limite).Scan(&busta)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, nil
	case err != nil:
		return 0, err
	}
	tagliate, err := chatTagliate(ctx, b.d.Lettura, busta)
	if err != nil || slices.Contains(tagliate, chat) {
		return 0, err
	}
	return busta, nil
}

// RispostaTelegram è TelegramReplies.post/3 e PublicMessage.post/5 dal lato :telegram: il testo nella
// conversazione della busta come messaggio del cliente, con dopo (l'offset del lettore) nella stessa
// transazione, poi l'avviso alla chat del negozio. Rifiuti, senza scrivere niente: la busta che non c'è o è
// nel cestino (prima di guardare il testo, come Repo.get/2), il testo vuoto o troppo lungo, la busta chiusa,
// di un'oreficeria, oltre i dieci messaggi all'ora; ogni altro errore torna com'è.
func (b *Buste) RispostaTelegram(ctx context.Context, busta int64, testo string, dopo func(context.Context, *sql.Tx) error) error {
	var c int
	if err := b.d.Lettura.QueryRowContext(ctx, `SELECT count(*) FROM buste WHERE id = ? AND cancellata_il IS NULL`, busta).Scan(&c); err != nil {
		return err
	}
	if c == 0 {
		return uscita.ErrRispostaNonTrovata
	}
	testo, err := testoMessaggio(testo)
	if err == nil {
		err = b.scriviMessaggio(ctx, busta, linkTelegram, testo, attoreBot, dopo)
	}
	switch {
	case errors.Is(err, ErrMessaggioVuoto):
		return uscita.ErrRispostaVuota
	case errors.Is(err, ErrMessaggioTroppoLungo):
		return uscita.ErrRispostaTroppoLunga
	case errors.Is(err, ErrBustaNonTrovata), errors.Is(err, errConversazioneNascosta):
		return uscita.ErrRispostaNonTrovata
	case errors.Is(err, ErrConversazioneChiusa):
		return uscita.ErrRispostaChiusa
	case errors.Is(err, ErrTroppiMessaggi):
		return uscita.ErrRispostaTroppe
	}
	return err
}
