package uscita

import (
	"context"
	"database/sql"
	"errors"
	"regexp"

	"github.com/L0cutus/gestrip/internal/db"
)

// La configurazione del canale Telegram (slice 071, ADM-10, CLI-52 parte Telegram), come
// Outgoing.Config.get(:telegram) e usable?/1 dello standalone a go-baseline: la chat del negozio in
// impostazioni (outgoing.telegram.chat_id, come la porta l'import) e il token del bot in segreti_provider
// (D-03, migrazione 0006), mai tra le impostazioni. Si legge a ogni tentativo, come l'SMTP.
//
// Solo del Go (D-03): l'invio Telegram è spento finché il titolare non lo accende (ChiaveTelegramAcceso),
// come l'email della 069; la configurazione importata resta ferma, e con lei le righe Telegram importate
// (D-27). Lo standalone ha solo «Notifiche Telegram ai clienti» (ChiaveNotificheClienti), che ferma le righe
// dei clienti e il lettore; quelle del negozio partono con il bot configurato.

// ChiaveTelegramAcceso è la chiave di impostazioni, solo del Go, dell'interruttore dell'invio Telegram:
// «1» acceso, ogni altro valore o nessuna riga spento (D-03).
const ChiaveTelegramAcceso = "canale_telegram_acceso"

// Le chiavi dello standalone (Outgoing.Config.keys(:telegram) con outgoing.telegram., e quelle di
// TelegramInbound): l'import le porta come sono.
const (
	// ChiaveNotificheClienti è «Notifiche Telegram ai clienti» (@switch_key): «1» accese, altro spente.
	ChiaveNotificheClienti = "outgoing.telegram.customer_notifications"
	// ChiaveNomeBot è il nome che getMe ha dato per il token salvato (@username_key): non è un segreto.
	ChiaveNomeBot = "outgoing.telegram.bot_username"
	// ChiaveErroreLettore e ChiaveErroreLettoreIl sono l'ultimo errore del lettore getUpdates (un 409
	// soprattutto) e la sua ora ISO 8601: li scrive il lettore (slice 073), la pagina li mostra.
	ChiaveErroreLettore   = "telegram_inbound.last_error"
	ChiaveErroreLettoreIl = "telegram_inbound.last_error_at"
	// ChiaveChatSconosciute conta le chat a cui il lettore ha dato la risposta fissa (@unknown_key).
	ChiaveChatSconosciute = "telegram_inbound.unknown_chats"
	chiaveOffset          = "telegram_inbound.offset"
	chiaveAggInErrore     = "telegram_inbound.failing_update"
	// chiaveTokenBot sta in segreti_provider (importer.ChiaviSegreteProvider).
	chiaveTokenBot = "outgoing.telegram.bot_token"
	chiaveChat     = "outgoing.telegram.chat_id"
)

// chiaviDelToken sono le impostazioni che appartengono a un token (TelegramInbound.token_keys/0): un token
// nuovo le dimentica (forget_bot_state/2).
var chiaviDelToken = []string{ChiaveNomeBot, chiaveOffset, ChiaveErroreLettore, ChiaveErroreLettoreIl, chiaveAggInErrore}

// ConfigTelegram è Outgoing.Config.Telegram: un valore assente è "" (setting/1). Il token è una Password:
// non si stampa mai (come @derive {Inspect, except: [:bot_token]}).
type ConfigTelegram struct {
	Token Password // bot_token
	Chat  string   // chat_id
}

// La forma esatta del token e della chat, per la seconda serratura (Usabile): le regex del changeset,
// senza l'a capo finale che «$» lascia passare.
var (
	reTokenEsatto = regexp.MustCompile(`\A[0-9]+:[A-Za-z0-9_-]+\z`)
	reChatEsatta  = regexp.MustCompile(`\A(?:-?[0-9]+|@[A-Za-z0-9_]+)\z`)
)

// Usabile è usable?/1: token e chat presenti.
//
// In più, solo nel Go (come la seconda serratura dell'SMTP, review della 069), fallisce chiusa: un token
// che non è esattamente «numero:testo» (finisce nel percorso dell'indirizzo del Bot API: una «/», un «?»
// o un a capo lo cambierebbero) e una chat che non è esattamente un numero o «@nome» danno un canale non
// configurato. Lo standalone non li può salvare dalla pagina; l'import sì.
func (c ConfigTelegram) Usabile() bool {
	return presente(c.Token.valore()) && presente(c.Chat) && reTokenEsatto.MatchString(c.Token.valore()) && reChatEsatta.MatchString(c.Chat)
}

// leggiValore è setting/1 su una tabella: una chiave che manca o di soli spazi è "".
func leggiValore(ctx context.Context, q db.Interrogatore, dove, chiave string) (string, error) {
	var v string
	err := q.QueryRowContext(ctx, `SELECT valore FROM `+dove, chiave).Scan(&v)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", nil
	case err != nil:
		return "", err
	case !presente(v):
		return "", nil
	}
	return v, nil
}

const (
	inImpostazioni = `impostazioni WHERE chiave = ?`
	neiSegreti     = `segreti_provider WHERE nome = ?`
)

// LeggiTelegram è Config.get(:telegram).
func LeggiTelegram(ctx context.Context, q db.Interrogatore) (ConfigTelegram, error) {
	token, err := leggiValore(ctx, q, neiSegreti, chiaveTokenBot)
	if err != nil {
		return ConfigTelegram{}, err
	}
	chat, err := leggiValore(ctx, q, inImpostazioni, chiaveChat)
	if err != nil {
		return ConfigTelegram{}, err
	}
	return ConfigTelegram{Token: NuovaPassword(token), Chat: chat}, nil
}

// telegramAcceso legge l'interruttore della D-03 per Telegram.
func telegramAcceso(ctx context.Context, q db.Interrogatore) (bool, error) {
	v, err := leggiValore(ctx, q, inImpostazioni, ChiaveTelegramAcceso)
	return v == "1", err
}

// TelegramAttivo dice se il canale Telegram c'è: configurazione usabile e invio acceso (D-03). È la domanda
// di Accoda e del giro per le righe Telegram.
func TelegramAttivo(ctx context.Context, q db.Interrogatore) (bool, error) {
	c, err := LeggiTelegram(ctx, q)
	if err != nil || !c.Usabile() {
		return false, err
	}
	return telegramAcceso(ctx, q)
}

// NotificheClientiAccese è TelegramInbound.enabled?/0: «Notifiche Telegram ai clienti» accese solo con «1».
func NotificheClientiAccese(ctx context.Context, q db.Interrogatore) (bool, error) {
	var v string
	err := q.QueryRowContext(ctx, `SELECT valore FROM impostazioni WHERE chiave = ?`, ChiaveNotificheClienti).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return v == "1", err
}

// NomeBot è TelegramInbound.bot_username/0: il nome che getMe ha dato per il token salvato, "" se non c'è
// (una riga che manca o di soli spazi).
func NomeBot(ctx context.Context, q db.Interrogatore) (string, error) {
	return leggiValore(ctx, q, inImpostazioni, ChiaveNomeBot)
}
