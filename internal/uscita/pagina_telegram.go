package uscita

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
)

// La pagina «Provider di invio», parte Telegram (slice 071, ADM-10, CLI-52 parte Telegram), come
// ProvidersLive, Outgoing.Config.put(:telegram, …), Telegram.changeset/2, TelegramInbound.set_enabled/2,
// status/0 e refresh_bot_username/0 dello standalone a go-baseline (la prova però parte subito, senza
// coda, come la posta della 069c), più due cose solo del Go, come per l'email:
//   - l'interruttore dell'invio Telegram (D-03): dopo l'import il bot è fermo finché il titolare non lo
//     accende (ChiaveTelegramAcceso);
//   - la revisione dei messaggi Telegram rimasti in coda (D-27): l'invio non si accende finché ce n'è
//     uno; il titolare li scarta o li manda uno per uno (ScartaInSospeso, MandaInSospeso).
// Il token non esce mai: né nella pagina (la casella è sempre vuota), né nell'audit, né nel registro.

// CampiTelegram sono i campi del modulo (telegram[…]) nell'ordine di Config.keys(:telegram).
var CampiTelegram = []string{campoToken, campoChat}

const (
	campoToken = "bot_token"
	campoChat  = "chat_id"
	// I testi della prova, test_content(:telegram) dello standalone.
	testoProvaTelegram = "GestRip — prova del provider Telegram.\nSe stai leggendo questo messaggio, il bot configurato funziona."
	// erroreNomeNonSalvato è il motivo di refresh_bot_username/0 quando il nome non si salva.
	erroreNomeNonSalvato = "telegram:username non salvato"
)

// I rifiuti delle azioni Telegram della pagina: niente è scritto.
var (
	// ErrTelegramInSospeso: ci sono messaggi Telegram in coda da guardare (D-27): l'invio non si accende.
	ErrTelegramInSospeso = errors.New("uscita: messaggi Telegram in coda da guardare prima di accendere")
	// ErrTelegramAcceso: con l'invio Telegram acceso i messaggi in coda li manda la coda, non il titolare.
	ErrTelegramAcceso = errors.New("uscita: invio Telegram acceso")
)

// ModuloTelegram sono i campi del modulo come li manda il browser (bot_token, chat_id, clear_secret): un
// campo che manca non c'era e resta quello salvato (cast/3).
type ModuloTelegram map[string]string

// RifiutoTelegram è un modulo rifiutato: gli errori per campo (il più recente prima) e i valori da
// rimettere nel modulo; il token è sempre vuoto.
type RifiutoTelegram struct {
	Errori map[string][]string
	Valori map[string]string
}

// PaginaTelegram è quello che il riquadro Telegram mostra: la chat salvata (mai il token, solo se c'è),
// configurato (Usabile), l'invio acceso (D-03), le notifiche ai clienti e lo stato del lettore
// (TelegramInbound.status/0: nome del bot, chat sconosciute, ultimo errore con l'ora, conflitto per un
// 409) e, con l'invio spento, i messaggi Telegram rimasti in coda (D-27), dal più vecchio.
type PaginaTelegram struct {
	Chat                                                string
	TokenSalvato, Configurata, Acceso, NotificheClienti bool
	NomeBot, UltimoErrore                               string
	UltimoErroreIl                                      *time.Time
	Conflitto                                           bool
	ChatSconosciute                                     int
	InSospeso                                           []RigaCoda
}

// EsitoProvaTelegram è com'è andata la prova: il nome del bot che getMe ha dato, o il motivo per cui
// getMe non ha risposto (GetMe, e allora niente è partito), e l'errore del messaggio di prova ("" se il
// bot l'ha accettato). Mai il token.
type EsitoProvaTelegram struct {
	Nome, GetMe, Errore string
}

// Le regex di Telegram.changeset/2 lette come PCRE senza «u», «$» compreso (combacia anche prima di un
// solo a capo finale): si tolgono l'a capo finale e si confronta con la forma esatta.
var (
	reTokenModulo = regexp.MustCompile(`\A[0-9]+:[A-Za-z0-9_\-]+\z`)
	reChatModulo  = regexp.MustCompile(`\A-?[0-9]+\z|\A@[A-Za-z0-9_]+\z`)
)

// SalvaTelegram è Config.put(:telegram, attrs, context): con la casella del token vuota tiene quello
// salvato (keep_secret/3), con «Cancella il token salvato» lo toglie (solo lui: Telegram non ha una
// coppia); poi Telegram.changeset/2 (le validazioni guardano solo i campi cambiati). Valido: in una
// transazione la chat (nil = ""), il token in segreti_provider (nil lo toglie), lo stato del bot del token
// di prima dimenticato se il token è cambiato (forget_bot_state/2) e outgoing.provider_configured con il
// canale, i nomi delle chiavi cambiate e se è configurato; mai un valore. Rifiutato: niente scritto.
func (c *Coda) SalvaTelegram(ctx context.Context, m ModuloTelegram, a audit.Contesto) (*RifiutoTelegram, error) {
	cfg, err := LeggiTelegram(ctx, c.d.Lettura)
	if err != nil {
		return nil, err
	}
	prima := map[string]*string{}
	if v := cfg.Token.valore(); v != "" {
		prima[campoToken] = ptr(v)
	}
	if cfg.Chat != "" {
		prima[campoChat] = ptr(cfg.Chat)
	}
	param := map[string]*string{}
	for k, v := range m {
		param[k] = ptr(v)
	}
	switch t, mandato := m[campoToken]; {
	case vero(m[campoCancella]):
		param[campoToken] = nil
	case mandato && !vuoto(t):
	default:
		param[campoToken] = prima[campoToken]
	}
	errori := map[string][]string{}
	sbaglia := func(campo, msg string) { errori[campo] = append(errori[campo], msg) }
	dopo := map[string]*string{campoToken: prima[campoToken], campoChat: prima[campoChat]}
	cambiati := map[string]bool{}
	for _, campo := range CampiTelegram {
		v, c := param[campo]
		if !c {
			continue
		}
		if v != nil && vuoto(*v) {
			v = nil
		}
		if !uguali(v, prima[campo]) {
			dopo[campo], cambiati[campo] = v, true
		}
	}
	if v, c := param[campoCancella]; c && !vuoto(*v) && *v != "true" && *v != "1" && *v != "false" && *v != "0" {
		sbaglia(campoCancella, msgNonValido) // il cast di :boolean
	}
	nuovo := func(campo string) (string, bool) {
		v := dopo[campo]
		if !cambiati[campo] || v == nil {
			return "", false
		}
		return *v, true
	}
	if s, ok := nuovo(campoToken); ok && !reTokenModulo.MatchString(senzaACapoFinale(s)) {
		sbaglia(campoToken, msgFormato)
	}
	if s, ok := nuovo(campoChat); ok && !reChatModulo.MatchString(senzaACapoFinale(s)) {
		sbaglia(campoChat, msgFormato)
	}
	for _, l := range []struct {
		campo string
		max   int
	}{{campoToken, 200}, {campoChat, 64}} {
		if s, ok := nuovo(l.campo); ok && uniseg.GraphemeClusterCount(s) > l.max {
			sbaglia(l.campo, msgTroppoLungo(l.max))
		}
	}
	if len(errori) > 0 {
		// Più errori di un campo dal più recente, come changeset.errors.
		for _, e := range errori {
			slices.Reverse(e)
		}
		rif := &RifiutoTelegram{Errori: errori, Valori: map[string]string{campoToken: "", campoChat: ""}}
		if prima[campoChat] != nil {
			rif.Valori[campoChat] = *prima[campoChat]
		}
		if v, c := param[campoChat]; c {
			rif.Valori[campoChat] = ""
			if v != nil {
				rif.Valori[campoChat] = *v
			}
		}
		return rif, nil
	}
	return nil, c.scriviTelegram(ctx, prima, dopo, a)
}

// scriviTelegram è persist/4 per Telegram.
func (c *Coda) scriviTelegram(ctx context.Context, prima, dopo map[string]*string, a audit.Contesto) error {
	cambiate := []string{}
	for _, campo := range CampiTelegram {
		if !uguali(prima[campo], dopo[campo]) {
			cambiate = append(cambiate, campo)
		}
	}
	ora := c.o.Adesso().UTC().Format(db.FormatoIstante)
	tx, err := c.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	chat := ""
	if dopo[campoChat] != nil {
		chat = *dopo[campoChat]
	}
	if err := scriviImpostazione(ctx, tx, chiaveChat, chat, ora); err != nil {
		return err
	}
	if t := dopo[campoToken]; t != nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO segreti_provider (nome, valore, aggiornato_il) VALUES (?, ?, ?)
			ON CONFLICT (nome) DO UPDATE SET valore = excluded.valore, aggiornato_il = excluded.aggiornato_il`, chiaveTokenBot, *t, ora)
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM segreti_provider WHERE nome = ?`, chiaveTokenBot)
	}
	if err != nil {
		return err
	}
	// forget_bot_state/2: nome, offset, ultimo errore e aggiornamento in errore sono del token di prima.
	if !uguali(prima[campoToken], dopo[campoToken]) {
		for _, k := range chiaviDelToken {
			if err := scriviImpostazione(ctx, tx, k, "", ora); err != nil {
				return err
			}
		}
	}
	token := ""
	if dopo[campoToken] != nil {
		token = *dopo[campoToken]
	}
	configurata := ConfigTelegram{Token: NuovaPassword(token), Chat: chat}.Usabile()
	if err := c.reg.Scrivi(ctx, tx, a, "outgoing.provider_configured", "outgoing_provider", CanaleTelegram, "ok",
		map[string]any{"channel": CanaleTelegram, "changed": cambiate, "configured": configurata}); err != nil {
		return err
	}
	return tx.Commit()
}

// PaginaTelegram legge quello che il riquadro Telegram mostra (load/1 e TelegramInbound.status/0, più
// l'interruttore e la revisione).
func (c *Coda) PaginaTelegram(ctx context.Context) (PaginaTelegram, error) {
	cfg, err := LeggiTelegram(ctx, c.d.Lettura)
	if err != nil {
		return PaginaTelegram{}, err
	}
	p := PaginaTelegram{Chat: cfg.Chat, TokenSalvato: cfg.Token.Presente(), Configurata: cfg.Usabile()}
	if p.Acceso, err = telegramAcceso(ctx, c.d.Lettura); err != nil {
		return PaginaTelegram{}, err
	}
	if p.NotificheClienti, err = NotificheClientiAccese(ctx, c.d.Lettura); err != nil {
		return PaginaTelegram{}, err
	}
	leggi := func(chiave string) string {
		if err != nil {
			return ""
		}
		var v string
		v, err = leggiValore(ctx, c.d.Lettura, inImpostazioni, chiave)
		return v
	}
	p.NomeBot = leggi(ChiaveNomeBot)
	sconosciute := leggi(ChiaveChatSconosciute)
	p.UltimoErrore = leggi(ChiaveErroreLettore)
	quando := leggi(ChiaveErroreLettoreIl)
	if err != nil {
		return PaginaTelegram{}, err
	}
	if n, ok := interoElixir(sconosciute); ok {
		p.ChatSconosciute = int(min(max(n, 0), 1<<31-1))
	}
	if t, e := time.Parse(time.RFC3339Nano, quando); e == nil {
		p.UltimoErroreIl = &t
	}
	p.Conflitto = len(p.UltimoErrore) >= 3 && p.UltimoErrore[:3] == "409"
	if !p.Acceso {
		if p.InSospeso, err = c.righe(ctx, `canale = 'telegram' AND stato = 'queued' ORDER BY id`); err != nil {
			return PaginaTelegram{}, err
		}
	}
	return p, nil
}

// conProvaTelegram è un provider che sa chiedere il nome del bot e mandare senza l'interruttore: il bot
// vero (Telegram).
type conProvaTelegram interface {
	conInvioAMano
	ChiediNome(ctx context.Context) (string, error)
}

// MandaProvaTelegram è test di «telegram» (handle_event("test", %{"channel" => "telegram"})): con il bot
// configurato chiede getMe (refresh_bot_username/0) e, se il bot non risponde, niente parte (GetMe è il
// motivo, senza token); se risponde salva il nome (outgoing.telegram.bot_username) e manda il messaggio
// di prova dello standalone alla chat del negozio (parte «shop», nessuna busta). Come la posta della 069c
// (decisione di Stefano dell'11/11 per la prova «subito»): dentro la richiesta, senza riga nella coda, anche
// con l'invio Telegram spento; lo standalone lo accoda. Audit outgoing.test_sent (outgoing_provider,
// telegram) con canale, parte e stato, esito ok o error: mai chat, testo o errore. Senza configurazione
// ErrNonConfigurato, senza richieste.
func (c *Coda) MandaProvaTelegram(ctx context.Context, a audit.Contesto) (EsitoProvaTelegram, error) {
	cfg, err := LeggiTelegram(ctx, c.d.Lettura)
	if err != nil {
		return EsitoProvaTelegram{}, err
	}
	p, ok := c.provider[CanaleTelegram].(conProvaTelegram)
	if !cfg.Usabile() || !ok {
		return EsitoProvaTelegram{}, ErrNonConfigurato
	}
	nome, err := p.ChiediNome(ctx)
	var r Rifiuto
	switch {
	case errors.As(err, &r):
		return EsitoProvaTelegram{GetMe: pulisciToken(string(r), cfg)}, nil
	case err != nil:
		return EsitoProvaTelegram{}, err
	}
	if err := c.salvaNomeBot(ctx, nome); err != nil {
		c.log.Error("coda in uscita: nome del bot non salvato", "tipo", fmt.Sprintf("%T", err))
		return EsitoProvaTelegram{GetMe: erroreNomeNonSalvato}, nil
	}
	m := Messaggio{Canale: CanaleTelegram, Destinatario: "shop", Indirizzo: cfg.Chat, Testo: testoProvaTelegram}
	_, errore := c.provaCon(ctx, m, c.provider[CanaleTelegram], p.ConsegnaAMano)
	risultato, stato := "ok", "accepted"
	if errore != "" {
		risultato, stato = "error", "failed"
	}
	if err := c.reg.Registra(context.WithoutCancel(ctx), a, "outgoing.test_sent", "outgoing_provider", CanaleTelegram, risultato,
		map[string]any{"channel": CanaleTelegram, "party": "shop", "status": stato}); err != nil {
		c.log.Error("coda in uscita: audit della prova non scritto", "tipo", fmt.Sprintf("%T", err))
	}
	return EsitoProvaTelegram{Nome: nome, Errore: errore}, nil
}

func (c *Coda) salvaNomeBot(ctx context.Context, nome string) error {
	_, err := c.d.Scrittura.ExecContext(ctx, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, ?)
		ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore, aggiornata_il = excluded.aggiornata_il`,
		ChiaveNomeBot, nome, c.o.Adesso().UTC().Format(db.FormatoIstante))
	return err
}

// ImpostaTelegram accende o spegne l'invio Telegram (D-03, solo Go), come ImpostaEmail: lo stesso stato non
// scrive niente; per accendere servono il bot configurato (ErrNonConfigurato) e nessun messaggio Telegram
// in coda (ErrTelegramInSospeso, D-27). Scrive canale_telegram_acceso e outgoing.channel_enabled o
// outgoing.channel_disabled (outgoing_provider, telegram).
func (c *Coda) ImpostaTelegram(ctx context.Context, acceso bool, a audit.Contesto) error {
	return c.impostaCanale(ctx, CanaleTelegram, acceso, a)
}

// ImpostaNotificheClienti è TelegramInbound.set_enabled/2: «Notifiche Telegram ai clienti» accese («1») o
// spente (""), e outgoing.telegram_customer_notifications con «enabled», in una transazione. Come lo
// standalone scrive anche se lo stato è già quello (il modulo porta lo stato che chiede, così un doppio
// clic non torna indietro).
func (c *Coda) ImpostaNotificheClienti(ctx context.Context, accese bool, a audit.Contesto) error {
	tx, err := c.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	valore := ""
	if accese {
		valore = "1"
	}
	if err := scriviImpostazione(ctx, tx, ChiaveNotificheClienti, valore, c.o.Adesso().UTC().Format(db.FormatoIstante)); err != nil {
		return err
	}
	if err := c.reg.Scrivi(ctx, tx, a, "outgoing.telegram_customer_notifications", "outgoing_provider", CanaleTelegram, "ok",
		map[string]any{"enabled": accese}); err != nil {
		return err
	}
	return tx.Commit()
}
