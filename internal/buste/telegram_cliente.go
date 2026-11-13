package buste

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// L'iscrizione Telegram del cliente dalla sua pagina (CLI-44) e dal link dei messaggi WhatsApp (CLI-45),
// anche dalla pagina storica (CLI-19), come TelegramSubscriptions.box/2, issue_link/4 e revoke/3 dello
// standalone (go-baseline, slice 135 e 136 di lì):
//   - il pulsante della pagina («Attiva su Telegram», e «Modifica preferenze» nello stato B) crea un token
//     di 24 byte casuali in base64url (32 caratteri) e risponde con https://t.me/<bot>?start=<token>; nel
//     database (token_telegram) solo il suo SHA-256, la busta e la scadenza: 24 ore dal pulsante, 60 minuti
//     dal link dei messaggi WhatsApp ({telegram_optin} = GET /r/<token>/telegram). Aprire il link non iscrive
//     nessuno: lo fa /start <token> nel bot (slice 073, che consuma il token);
//   - sei all'ora per busta, contati sulle righe repair.telegram_optin a parte per le due vie (via «page» o
//     «whatsapp»; una riga senza via è della pagina): un'anteprima di WhatsApp che apre il link non consuma
//     il pulsante;
//   - solo le buste del cliente diretto, non chiuse (quella conto terzi è «nascosta», anche chiusa);
//   - il canale disponibile (TelegramDisponibile, regola D-16 del Go): l'invio Telegram acceso con il bot
//     configurato (la coda, uscita.TelegramAttivo), «Notifiche Telegram ai clienti» accese, il nome del bot
//     noto (lo salva getMe, slice 071) e l'indirizzo pubblico, senza il quale gli avvisi Telegram (slice
//     074) non avrebbero il pulsante della pagina. Senza, niente riquadro e nessun token;
//   - «Disattiva» spegne ogni iscrizione ancora attiva della busta (motivo e via «page»);
//   - l'audit (repair.telegram_optin, repair.telegram_stop) porta la busta e l'iscrizione, mai la chat, il
//     token o il nome di chi ha scritto.

// Le durate dei token (ttl_min=60*24 della pagina e ttl_min=60 del link WhatsApp dell'app Flask), il
// limite (6 per hour) e le azioni d'audit dello standalone.
const (
	durataTokenPagina   = 24 * time.Hour
	durataTokenWhatsApp = time.Hour
	limiteTelegram      = 6
	finestraTelegram    = time.Hour
	byteTokenTelegram   = 24
	azioneOptinTelegram = "repair.telegram_optin"
	azioneStopTelegram  = "repair.telegram_stop"
	viaPagina           = "page"
	viaWhatsApp         = "whatsapp"
)

// ErrTroppeRichiesteTelegram è {:error, :rate_limited}: sei token nell'ultima ora per quella via.
var ErrTroppeRichiesteTelegram = errors.New("buste: troppe richieste Telegram nell'ultima ora")

// RiquadroTelegram è il riquadro della pagina (box/2): Attive con un'iscrizione con il consenso e non
// fermata (stato B), altrimenti la proposta (stato A).
type RiquadroTelegram struct{ Attive bool }

// reNomeBot è la forma di un nome di bot di Telegram. Solo del Go (seconda serratura, come quella del token
// della 071): il nome finisce nell'indirizzo t.me, e uno importato con «/», «?» o uno spazio lo cambierebbe;
// un nome così è un nome ignoto.
var reNomeBot = regexp.MustCompile(`\A[A-Za-z0-9_]+\z`)

// nomeBot è TelegramInbound.bot_username/0 con la seconda serratura: "" se non è noto o non ha la forma.
func (b *Buste) nomeBot(ctx context.Context) (string, error) {
	v, err := uscita.NomeBot(ctx, b.d.Lettura)
	if err != nil || !reNomeBot.MatchString(v) {
		return "", err
	}
	return v, nil
}

// TelegramDisponibile è available?/0 con la regola D-16 del Go: la coda ha il canale Telegram acceso e
// configurato (CanaleAttivo: l'invio Telegram della D-03 e il bot), le notifiche ai clienti sono accese,
// il nome del bot è noto e c'è l'indirizzo pubblico base.
func (b *Buste) TelegramDisponibile(ctx context.Context, base string) (bool, error) {
	_, ok, err := b.botDisponibile(ctx, base)
	return ok, err
}

// botDisponibile è TelegramDisponibile che dà anche il nome del bot.
func (b *Buste) botDisponibile(ctx context.Context, base string) (string, bool, error) {
	if base == "" || b.posta == nil {
		return "", false, nil
	}
	if ok, err := b.posta.CanaleAttivo(ctx, uscita.CanaleTelegram); err != nil || !ok {
		return "", false, err
	}
	if ok, err := uscita.NotificheClientiAccese(ctx, b.d.Lettura); err != nil || !ok {
		return "", false, err
	}
	nome, err := b.nomeBot(ctx)
	return nome, nome != "" && err == nil, err
}

// bustaTelegram è quello che l'iscrizione guarda della busta.
type bustaTelegram struct {
	id              int64
	numero, stato   string
	oreficeriaID    *int64
	storicoRevocato bool
}

func leggiBustaTelegram(ctx context.Context, q lettore, id int64) (bustaTelegram, error) {
	var bt bustaTelegram
	var oref sql.NullInt64
	var revocato int
	err := q.QueryRowContext(ctx, `SELECT id, numero, stato, oreficeria_id, codice_flask_revocato_il IS NOT NULL FROM buste
		WHERE id = ? AND cancellata_il IS NULL`, id).Scan(&bt.id, &bt.numero, &bt.stato, &oref, &revocato)
	if errors.Is(err, sql.ErrNoRows) {
		return bustaTelegram{}, ErrBustaNonTrovata
	}
	if err != nil {
		return bustaTelegram{}, err
	}
	bt.oreficeriaID, bt.storicoRevocato = nullaIntero(oref), revocato == 1
	return bt, nil
}

// visibile è visible/1 e link_alive/2: prima la controparte (una busta conto terzi è nascosta, chiusa o
// no), poi la busta chiusa; il link storico di una busta revocata è una busta che non c'è.
func (bt bustaTelegram) visibile(storico bool) error {
	switch {
	case Classificazione(bt.oreficeriaID) == ClasseEsterna:
		return ErrTelegramNascosto
	case slices.Contains(statiChiusi, bt.stato):
		return ErrTelegramChiusa
	case storico && bt.storicoRevocato:
		return ErrBustaNonTrovata
	}
	return nil
}

// RiquadroTelegram è box/2 della busta id: nil dove la pagina non ne mostra (busta chiusa, conto terzi,
// nel cestino, link storico revocato). Non scrive niente. Chi la chiama ha già deciso che il canale è
// disponibile.
func (b *Buste) RiquadroTelegram(ctx context.Context, id int64, storico bool) (*RiquadroTelegram, error) {
	bt, err := leggiBustaTelegram(ctx, b.d.Lettura, id)
	if errors.Is(err, ErrBustaNonTrovata) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if bt.visibile(storico) != nil {
		return nil, nil
	}
	var attive int
	if err := b.d.Lettura.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM iscrizioni_telegram WHERE busta_id = ? AND consenso = 1
		AND fermata_il IS NULL)`, id).Scan(&attive); err != nil {
		return nil, err
	}
	return &RiquadroTelegram{Attive: attive == 1}, nil
}

// IscriviTelegramCliente è telegram_optin/2: il pulsante della pagina del link token, con il link t.me.
// ErrBustaNonTrovata per un link morto.
func (b *Buste) IscriviTelegramCliente(ctx context.Context, token, base string, c audit.Contesto) (string, error) {
	t, err := b.bustaDelToken(ctx, token)
	if err != nil {
		return "", err
	}
	return b.emettiTokenTelegram(ctx, t.id, false, viaPagina, base, c)
}

// LinkTelegram è telegram_link/2: quello che {telegram_optin} apre (GET /r/<token>/telegram), un token
// di 60 minuti contato a parte.
func (b *Buste) LinkTelegram(ctx context.Context, token, base string, c audit.Contesto) (string, error) {
	t, err := b.bustaDelToken(ctx, token)
	if err != nil {
		return "", err
	}
	return b.emettiTokenTelegram(ctx, t.id, false, viaWhatsApp, base, c)
}

// IscriviTelegramStorica è legacy_telegram_optin/2: il pulsante dalla pagina storica, con la credenziale.
// Ogni rifiuto della credenziale e la busta conto terzi sono ErrBustaNonTrovata con la riga
// repair.legacy_public_denied.
func (b *Buste) IscriviTelegramStorica(ctx context.Context, scritto string, cred CredenzialeStorica, base string, c audit.Contesto) (string, error) {
	s, ok, err := b.cercaStorica(ctx, scritto)
	if err != nil {
		return "", err
	}
	if !ok || !b.autorizzata(s, scritto, cred) {
		b.rifiutoStorico(ctx, c)
		return "", ErrBustaNonTrovata
	}
	if b.primaDelToken != nil {
		b.primaDelToken()
	}
	link, err := b.emettiTokenTelegram(ctx, s.id, true, viaPagina, base, c)
	if errors.Is(err, ErrTelegramNascosto) {
		b.rifiutoStorico(ctx, c)
		return "", ErrBustaNonTrovata
	}
	return link, err
}

// emettiTokenTelegram è issue_link/4: nella transazione la busta riletta (chiusa, cestinata o passata a
// un'oreficeria dopo che la pagina è stata disegnata; lo storico revocato dopo la verifica della rotta),
// poi il canale, il limite della via, il token e la riga d'audit. Niente si scrive se qualcosa manca.
func (b *Buste) emettiTokenTelegram(ctx context.Context, id int64, storico bool, via, base string, c audit.Contesto) (string, error) {
	bot, disponibile, err := b.botDisponibile(ctx, base)
	if err != nil {
		return "", err
	}
	adesso, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	bt, err := leggiBustaTelegram(ctx, tx, id)
	if err != nil {
		return "", err
	}
	if err := bt.visibile(storico); err != nil {
		return "", err
	}
	if !disponibile {
		return "", ErrTelegramSpento
	}
	if err := b.sottoLimiteTelegram(ctx, tx, id, via, adesso); err != nil {
		return "", err
	}
	durata := durataTokenPagina
	if via == viaWhatsApp {
		durata = durataTokenWhatsApp
	}
	token := nuovoTokenTelegram()
	h := sha256.Sum256([]byte(token))
	if _, err := tx.ExecContext(ctx, `INSERT INTO token_telegram (busta_id, hash_token, scade_il, creato_il) VALUES (?, ?, ?, ?)`,
		id, h[:], adesso.Add(durata).Format(db.FormatoIstante), ora); err != nil {
		return "", err
	}
	link := "legacy"
	if !storico {
		link = "token"
	}
	if err := b.reg.Scrivi(ctx, tx, c, azioneOptinTelegram, "repair", id, "ok", map[string]any{
		"bag_number": bt.numero, "link": link, "via": via,
	}); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return "https://t.me/" + bot + "?start=" + token, nil
}

// nuovoTokenTelegram è new_token/0: 24 byte casuali in base64url senza «=», 32 caratteri.
func nuovoTokenTelegram() string {
	b := make([]byte, byteTokenTelegram)
	_, _ = rand.Read(b) // crypto/rand.Read non fallisce
	return base64.RawURLEncoding.EncodeToString(b)
}

// sottoLimiteTelegram è under_limit/2: le righe repair.telegram_optin della busta nell'ultima ora per la
// via (una riga senza via è della pagina) sono meno di limiteTelegram.
func (b *Buste) sottoLimiteTelegram(ctx context.Context, tx *sql.Tx, id int64, via string, adesso time.Time) error {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM eventi_audit WHERE azione = ? AND risorsa = 'repair' AND id_risorsa = ?
		AND creato_il >= ? AND coalesce(json_extract(dati, '$.via'), 'page') = ?`, azioneOptinTelegram, strconv.FormatInt(id, 10),
		adesso.Add(-finestraTelegram).Format(db.FormatoIstante), via).Scan(&n); err != nil {
		return err
	}
	if n >= limiteTelegram {
		return ErrTroppeRichiesteTelegram
	}
	return nil
}

// DisattivaTelegramCliente è telegram_optout/2: «Disattiva» dalla pagina del link token. ErrBustaNonTrovata
// per un link morto, ErrTelegramNascosto per la busta conto terzi.
func (b *Buste) DisattivaTelegramCliente(ctx context.Context, token string, c audit.Contesto) error {
	t, err := b.bustaDelToken(ctx, token)
	if err != nil {
		return err
	}
	return b.disattivaTelegram(ctx, t.id, c)
}

// DisattivaTelegramStorica è legacy_telegram_optout/2: lo stesso dalla pagina storica, con la credenziale;
// ogni rifiuto (anche la busta conto terzi) è ErrBustaNonTrovata con la riga repair.legacy_public_denied.
func (b *Buste) DisattivaTelegramStorica(ctx context.Context, scritto string, cred CredenzialeStorica, c audit.Contesto) error {
	s, ok, err := b.cercaStorica(ctx, scritto)
	if err != nil {
		return err
	}
	if !ok || !b.autorizzata(s, scritto, cred) {
		b.rifiutoStorico(ctx, c)
		return ErrBustaNonTrovata
	}
	err = b.disattivaTelegram(ctx, s.id, c)
	if errors.Is(err, ErrTelegramNascosto) {
		b.rifiutoStorico(ctx, c)
		return ErrBustaNonTrovata
	}
	return err
}

// disattivaTelegram è revoke/3: la busta riletta (solo del cliente diretto, chiusa o no: revocable/1), poi
// ogni iscrizione ancora attiva si spegne con motivo e via «page».
func (b *Buste) disattivaTelegram(ctx context.Context, id int64, c audit.Contesto) error {
	_, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	bt, err := leggiBustaTelegram(ctx, tx, id)
	if err != nil {
		return err
	}
	if Classificazione(bt.oreficeriaID) == ClasseEsterna {
		return ErrTelegramNascosto
	}
	if err := b.spegniIscrizioni(ctx, tx, id, bt.numero, viaPagina, viaPagina, c, ora); err != nil {
		return err
	}
	return tx.Commit()
}

// spegniIscrizioni è switch_off/4 sulle iscrizioni della busta id ancora attive: ognuna con consenso 0,
// fermata adesso e il motivo, e una riga repair.telegram_stop (busta, iscrizione, via; mai la chat), nella
// transazione di chi chiama.
func (b *Buste) spegniIscrizioni(ctx context.Context, tx *sql.Tx, id int64, numero, motivo, via string, c audit.Contesto, ora string) error {
	righe, err := tx.QueryContext(ctx, `SELECT id FROM iscrizioni_telegram WHERE busta_id = ? AND fermata_il IS NULL ORDER BY id`, id)
	if err != nil {
		return err
	}
	var attive []int64
	for righe.Next() {
		var n int64
		if err := righe.Scan(&n); err != nil {
			_ = righe.Close() // l'errore che conta è quello della lettura
			return err
		}
		attive = append(attive, n)
	}
	if err := righe.Err(); err != nil {
		return err
	}
	for _, n := range attive {
		if _, err := tx.ExecContext(ctx, `UPDATE iscrizioni_telegram SET consenso = 0, fermata_il = ?1, motivo_fermata = ?2,
			aggiornato_il = ?1 WHERE id = ?3`, ora, motivo, n); err != nil {
			return err
		}
		if err := b.reg.Scrivi(ctx, tx, c, azioneStopTelegram, "repair", id, "ok", map[string]any{
			"bag_number": numero, "subscription_id": n, "via": via,
		}); err != nil {
			return err
		}
	}
	return nil
}
