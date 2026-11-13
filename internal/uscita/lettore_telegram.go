package uscita

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Il lettore del bot Telegram (slice 073, CLI-46), come TelegramInbound.poll_once/0, TelegramPoller e
// TelegramInbound.ReplyLimit dello standalone a go-baseline (slice 134 e 135 di lì): quello che i clienti
// scrivono al bot del negozio, letto con getUpdates in lettura lunga, perché un'installazione non deve
// aprire una porta pubblica per un webhook.
//
//   - **Quando legge (vincolo di Stefano, Dubbio 1 della 071):** solo con «Invio Telegram» (D-03, solo Go)
//     e «Notifiche Telegram ai clienti» accese tutte e due e il bot configurato (Usabile). Altrimenti una
//     lettura è «inattiva» e non chiama la rete; lo standalone chiede solo le notifiche e il bot.
//   - **L'offset** (telegram_inbound.offset) si salva dopo **ogni** aggiornamento: uno dietro l'offset non
//     si gestisce due volte (Telegram consegna almeno una volta). Un aggiornamento che fallisce (un errore
//     o un panico) ferma la lettura con l'offset su di lui; alla terza volta si salta
//     (telegram_inbound.failing_update «id:volte»).
//   - **L'ultimo errore** (telegram_inbound.last_error e last_error_at), un 409 soprattutto (un webhook
//     attivo su questo token, o un secondo lettore): lo mostra la pagina dei provider della 071. Il bot non
//     chiama mai deleteWebhook: decide il titolare, e intanto il lavoro aspetta sempre di più.
//   - **I comandi** (slice 135 dello standalone): /start <token> iscrive la chat alla busta (le iscrizioni,
//     IscrizioniTelegram: internal/buste) e chiede il consenso con due pulsanti; il pulsante lo scrive;
//     /stop e /disattiva spengono le iscrizioni della chat; /privacy manda l'indirizzo dell'informativa;
//     tutto il resto di una chat privata riceve la risposta fissa. Fuori dal flusso, senza risposta: la
//     chat del negozio, gruppi e canali, gli altri bot.
//   - **Le risposte partono subito** (sendMessage diretto, come send_text/4 dello standalone), non dalla
//     coda in uscita: nessuna riga in messaggi_in_uscita, niente di salvato. Quante, lo dicono i limiti in
//     memoria (limiteRisposte, come ReplyLimit).
//   - **Mai** la chat, il token di /start o quello che la persona ha scritto: né nel registro, né nelle
//     impostazioni, né nell'audit. Il registro dice il numero dell'aggiornamento e il genere dell'errore.
//
// Fuori dalla 073: le risposte del cliente a una notifica (CLI-49) e la pulizia oraria delle iscrizioni
// (CLI-50), entrambe della 075.

// I rifiuti delle iscrizioni che il bot dice al cliente.
var (
	// ErrLinkTelegramNonValido è {:error, :invalid} di start/2: token vuoto, ignoto, già usato, scaduto, o
	// di una busta che non è più aperta e del cliente diretto.
	ErrLinkTelegramNonValido = errors.New("uscita: link Telegram non valido o scaduto")
	// ErrIscrizioneTelegramNonTrovata è {:error, :not_found} di consent/3: l'iscrizione non è di quella chat
	// o è già fermata.
	ErrIscrizioneTelegramNonTrovata = errors.New("uscita: iscrizione Telegram non trovata")
)

// IscrizioniTelegram sono le iscrizioni dei clienti che il bot tocca (TelegramSubscriptions start/2,
// consent/3, stop_chat/1 e OnlineSales.shop_name/0): le dà internal/buste. Ogni errore che non è uno dei
// due rifiuti sopra fa fallire l'aggiornamento, che si riprova.
type IscrizioniTelegram interface {
	// AvviaTelegram è /start <token>: l'id dell'iscrizione, o ErrLinkTelegramNonValido.
	AvviaTelegram(ctx context.Context, chat int64, token string) (int64, error)
	// ConsensoTelegram è il pulsante: nil o ErrIscrizioneTelegramNonTrovata.
	ConsensoTelegram(ctx context.Context, chat, id int64, valore bool) error
	// FermaChatTelegram è /stop: quante iscrizioni ha spento, e se la chat ne aveva almeno una.
	FermaChatTelegram(ctx context.Context, chat int64) (int, bool, error)
	// NomeNegozio è il nome del negozio nei messaggi del bot («Negozio» senza profilo).
	NomeNegozio(ctx context.Context) (string, error)
}

// Le attese del lavoro (TelegramPoller): inattivo si guarda di nuovo dopo AttesaInattivo (@idle 30 s), così
// accendere il canale non vuole un riavvio; una lettura vuota aspetta un secondo (un Bot API che non tiene
// la lettura lunga non fa girare a vuoto); un errore da 5 secondi a 5 minuti.
const (
	AttesaInattivo = 30 * time.Second
	attesaVuota    = time.Second
)

var atteseLettore = []time.Duration{5 * time.Second, 15 * time.Second, time.Minute, 5 * time.Minute}

// Le costanti della lettura e delle risposte, quelle dello standalone.
const (
	// secondiLetturaLunga è il timeout di getUpdates (@poll_seconds): Telegram tiene la richiesta fino a 25
	// secondi; il trasporto ne aspetta 35 (@poll_timeout).
	secondiLetturaLunga = 25
	tempoLetturaLunga   = 35 * time.Second
	// massimoAggiornamenti è quanto si legge al più della risposta di getUpdates (solo Go): fino a 100
	// aggiornamenti, ognuno con un testo fino a 4096 caratteri.
	massimoAggiornamenti   = 8 << 20
	tentativiAggiornamento = 3
	lunghezzaUltimoErrore  = 500
)

// Le risposte del bot, con le parole dello standalone (handshake.py della fonte).
const (
	testoLinkNonValido  = "❌ Link di collegamento non valido o scaduto. Rigenera un nuovo link."
	testoConsensoAcceso = "✅ Notifiche attive. Ti avviserò quando ci sono aggiornamenti."
	testoConsensoSpento = "🔕 Notifiche disattivate per questa riparazione."
	testoNonTrovata     = "❌ Iscrizione non trovata. Rigenera il link dalla pagina della riparazione."
	pulsanteSi          = "✅ Sì, notifiche sulla riparazione"
	pulsanteNo          = "🔕 No grazie"
	formatoHTML         = "HTML"
	prefissoConflitto   = "409"
)

// LettoreTelegram è il lettore del bot. Uno solo per processo: i limiti delle risposte stanno nella sua
// memoria.
type LettoreTelegram struct {
	d        *db.DB
	o        clock.Orologio
	t        *Telegram
	isc      IscrizioniTelegram
	pubblico string
	log      *slog.Logger
	limiti   *limiteRisposte
}

// NuovoLettoreTelegram è il lettore sul bot t (lo stesso provider della coda: endpoint e trasporto della
// 071), con le iscrizioni isc e l'indirizzo pubblico del negozio (per /privacy).
func NuovoLettoreTelegram(d *db.DB, o clock.Orologio, t *Telegram, isc IscrizioniTelegram, pubblico string) *LettoreTelegram {
	return &LettoreTelegram{d: d, o: o, t: t, isc: isc, pubblico: pubblico, log: slog.New(slog.DiscardHandler), limiti: &limiteRisposte{}}
}

// ConRegistro dà al lettore il registro del server.
func (l *LettoreTelegram) ConRegistro(log *slog.Logger) *LettoreTelegram {
	l.log = log
	return l
}

// EsitoLettura è com'è andata una lettura (poll_once/0): inattiva (senza richieste), riuscita con gli
// aggiornamenti gestiti, o non riuscita con il motivo salvato come ultimo errore (Conflitto per un 409).
type EsitoLettura struct {
	Inattivo  bool
	Gestiti   int
	Conflitto bool
	Errore    string
}

// ProssimaAttesa è next_delay/3: quanto aspettare prima della prossima lettura dopo e (o dopo un errore
// interno err), e i fallimenti di fila da portare avanti.
func ProssimaAttesa(e EsitoLettura, err error, fallimenti int) (time.Duration, int) {
	switch {
	case err != nil || e.Errore != "":
		return atteseLettore[min(fallimenti, len(atteseLettore)-1)], fallimenti + 1
	case e.Inattivo:
		return AttesaInattivo, 0
	case e.Gestiti == 0:
		return attesaVuota, 0
	}
	return 0, 0
}

// Lavoro è TelegramPoller: una lettura subito (lo standalone aspetta prima l'intervallo da inattivo), poi
// una dopo ogni aspetta con l'attesa di ProssimaAttesa, finché aspetta dice di smettere. Una lettura che va
// in panico non ferma il lavoro: nel registro solo il genere, e si aspetta come dopo un errore.
func (l *LettoreTelegram) Lavoro(ctx context.Context, aspetta func(context.Context, time.Duration) bool) {
	fallimenti := 0
	for {
		e, err := l.letturaProtetta(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			l.log.Error("lettore Telegram: lettura non finita", "tipo", fmt.Sprintf("%T", err))
		}
		var d time.Duration
		d, fallimenti = ProssimaAttesa(e, err, fallimenti)
		if !aspetta(ctx, d) {
			return
		}
	}
}

// errPanico è una lettura finita in panico.
var errPanico = errors.New("uscita: lettura Telegram finita in panico")

func (l *LettoreTelegram) letturaProtetta(ctx context.Context) (e EsitoLettura, err error) {
	defer func() {
		if v := recover(); v != nil {
			l.log.Error("lettore Telegram: lettura interrotta da un panico", "tipo", fmt.Sprintf("%T", v))
			e, err = EsitoLettura{}, errPanico
		}
	}()
	return l.Leggi(ctx)
}

// Leggi è poll_once/0: getUpdates dall'offset salvato, ogni aggiornamento gestito e l'offset salvato dopo
// di lui. Un errore interno (il database) torna come errore, senza ultimo errore salvato. All'arresto (ctx
// finito) la lettura lunga si chiude e niente si salva come errore.
func (l *LettoreTelegram) Leggi(ctx context.Context) (EsitoLettura, error) {
	cfg, attivo, err := l.attivo(ctx)
	if err != nil {
		return EsitoLettura{}, err
	}
	if !attivo {
		return EsitoLettura{Inattivo: true}, nil
	}
	offset, err := l.offset(ctx)
	if err != nil {
		return EsitoLettura{}, err
	}
	agg, conflitto, motivo := l.chiediAggiornamenti(ctx, cfg, offset)
	if ctx.Err() != nil {
		return EsitoLettura{}, ctx.Err()
	}
	// Da qui le scritture finiscono anche se serve si ferma: un aggiornamento gestito a metà non si
	// ripete (un /start rigestito risponderebbe «link non valido»). Fra un aggiornamento e l'altro si
	// guarda ctx.
	h := context.WithoutCancel(ctx)
	switch {
	case conflitto:
		l.log.Warn("lettore Telegram: 409 su getUpdates (un webhook o un altro lettore su questo token), si aspetta")
		return EsitoLettura{Conflitto: true, Errore: motivo}, l.salvaErrore(h, motivo)
	case motivo != "":
		l.log.Warn("lettore Telegram: getUpdates non riuscito", "motivo", motivo)
		return EsitoLettura{Errore: motivo}, l.salvaErrore(h, motivo)
	}
	if err := l.pulisciErrore(h); err != nil {
		return EsitoLettura{}, err
	}
	gestiti, motivo, err := l.gestisciTutti(ctx, h, agg, offset, cfg)
	switch {
	case err != nil:
		return EsitoLettura{}, err
	case motivo != "":
		return EsitoLettura{Errore: motivo}, l.salvaErrore(h, motivo)
	}
	return EsitoLettura{Gestiti: gestiti}, nil
}

// attivo è la domanda del vincolo: notifiche ai clienti accese, invio Telegram acceso (D-03) e bot usabile.
func (l *LettoreTelegram) attivo(ctx context.Context) (ConfigTelegram, bool, error) {
	if ok, err := NotificheClientiAccese(ctx, l.d.Lettura); err != nil || !ok {
		return ConfigTelegram{}, false, err
	}
	if ok, err := telegramAcceso(ctx, l.d.Lettura); err != nil || !ok {
		return ConfigTelegram{}, false, err
	}
	cfg, err := LeggiTelegram(ctx, l.d.Lettura)
	if err != nil || !cfg.Usabile() {
		return ConfigTelegram{}, false, err
	}
	return cfg, true, nil
}

// offset è integer/1 dell'offset salvato: nil se manca o non è un intero.
func (l *LettoreTelegram) offset(ctx context.Context) (*int64, error) {
	v, err := leggiValore(ctx, l.d.Lettura, inImpostazioni, chiaveOffset)
	if err != nil {
		return nil, err
	}
	if n, ok := interoElixir(v); ok {
		return &n, nil
	}
	return nil, nil
}

// corpoAggiornamenti è il corpo di getUpdates: solo messaggi e pulsanti, la lettura lunga, l'offset se c'è.
type corpoAggiornamenti struct {
	AllowedUpdates []string `json:"allowed_updates"`
	Offset         *int64   `json:"offset,omitempty"`
	Timeout        int      `json:"timeout"`
}

// chiediAggiornamenti è get_updates/2: gli aggiornamenti, o il conflitto (409) con le parole di Telegram
// («409: <descrizione>», «Conflict» senza), o il motivo di un altro errore. Mai il token.
func (l *LettoreTelegram) chiediAggiornamenti(ctx context.Context, cfg ConfigTelegram, offset *int64) ([]json.RawMessage, bool, string) {
	j, err := json.Marshal(corpoAggiornamenti{AllowedUpdates: []string{"message", "callback_query"}, Offset: offset, Timeout: secondiLetturaLunga})
	if err != nil {
		return nil, false, "telegram:risposta non riconosciuta"
	}
	stato, risposta, err := l.t.richiestaPer(ctx, cfg, http.MethodPost, "getUpdates", j, tempoLetturaLunga, massimoAggiornamenti)
	var r Rifiuto
	switch {
	case errors.As(err, &r):
		return nil, false, pulisciToken(string(r), cfg)
	case err != nil:
		return nil, false, "telegram:transport::error"
	case stato == http.StatusConflict:
		return nil, true, prefissoConflitto + ": " + pulisciToken(descrizione(risposta, "Conflict"), cfg)
	case stato != http.StatusOK:
		return nil, false, "telegram:http:" + strconv.Itoa(stato)
	}
	var corpo struct {
		OK     json.RawMessage `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	var agg []json.RawMessage
	if json.Unmarshal(risposta, &corpo) != nil || string(corpo.OK) != "true" || json.Unmarshal(corpo.Result, &agg) != nil || agg == nil {
		return nil, false, "telegram:risposta non riconosciuta"
	}
	return agg, false, ""
}

// descrizione è description/2: la descrizione del corpo JSON, o ripiego.
func descrizione(risposta []byte, ripiego string) string {
	var r struct {
		Description any `json:"description"`
	}
	if json.Unmarshal(risposta, &r) == nil {
		if d, ok := r.Description.(string); ok {
			return d
		}
	}
	return ripiego
}

// gestisciTutti è handle_all/3: ogni aggiornamento con un update_id intero e non dietro l'offset si
// gestisce e l'offset va dopo di lui; gli altri si saltano senza contarli. Un aggiornamento che fallisce
// ferma la lettura (motivo «telegram:update <id> non gestito»), al terzo fallimento si salta.
func (l *LettoreTelegram) gestisciTutti(ctx, h context.Context, grezzi []json.RawMessage, offset *int64, cfg ConfigTelegram) (int, string, error) {
	gestiti := 0
	for _, g := range grezzi {
		if ctx.Err() != nil {
			break
		}
		agg, ok := decodifica(g).(map[string]any)
		if !ok {
			continue
		}
		id, ok := intero(agg["update_id"])
		if !ok || offset != nil && id < *offset {
			continue
		}
		fatto, err := l.tenta(h, agg, id, cfg)
		if err != nil {
			return 0, "", err
		}
		if !fatto {
			return 0, "telegram:update " + strconv.FormatInt(id, 10) + " non gestito", nil
		}
		dopo := id + 1
		if err := l.salva(h, chiaveOffset, strconv.FormatInt(dopo, 10)); err != nil {
			return 0, "", err
		}
		offset, gestiti = &dopo, gestiti+1
	}
	return gestiti, "", nil
}

// tenta è attempt/4 e failed/2: l'aggiornamento gestito (fatto), oppure il fallimento contato; al terzo è
// fatto lo stesso (saltato). Nel registro il numero dell'aggiornamento e il genere dell'errore: il suo
// testo e un panico possono portare la chat, il token di un /start o quello che la persona ha scritto.
func (l *LettoreTelegram) tenta(ctx context.Context, agg map[string]any, id int64, cfg ConfigTelegram) (bool, error) {
	genere := l.gestisciProtetto(ctx, agg, cfg)
	if genere == "" {
		return true, l.pulisciFallimento(ctx)
	}
	volte, err := l.fallimenti(ctx, id)
	if err != nil {
		return false, err
	}
	volte++
	if volte >= tentativiAggiornamento {
		l.log.Error(fmt.Sprintf("lettore Telegram: aggiornamento %d saltato dopo %d tentativi", id, volte), "tipo", genere)
		return true, l.pulisciFallimento(ctx)
	}
	l.log.Warn(fmt.Sprintf("lettore Telegram: aggiornamento %d non gestito, tentativo %d di %d", id, volte, tentativiAggiornamento), "tipo", genere)
	return false, l.salva(ctx, chiaveAggInErrore, fmt.Sprintf("%d:%d", id, volte))
}

// gestisciProtetto gestisce l'aggiornamento e dà "" se è andato, altrimenti il genere dell'errore o del
// panico.
func (l *LettoreTelegram) gestisciProtetto(ctx context.Context, agg map[string]any, cfg ConfigTelegram) (genere string) {
	defer func() {
		if v := recover(); v != nil {
			genere = fmt.Sprintf("panico %T", v)
		}
	}()
	if err := l.gestisci(ctx, agg, cfg); err != nil {
		return fmt.Sprintf("%T", err)
	}
	return ""
}

// fallimenti è failures/1: quante volte l'aggiornamento id è già fallito («id:volte»), 0 se è un altro.
func (l *LettoreTelegram) fallimenti(ctx context.Context, id int64) (int64, error) {
	v, err := leggiValore(ctx, l.d.Lettura, inImpostazioni, chiaveAggInErrore)
	if err != nil {
		return 0, err
	}
	pezzi := strings.Split(v, ":")
	if len(pezzi) != 2 {
		return 0, nil
	}
	quale, ok := interoElixir(pezzi[0])
	volte, ok2 := interoElixir(pezzi[1])
	if !ok || !ok2 || quale != id {
		return 0, nil
	}
	return volte, nil
}

// pulisciFallimento è clear_failure/0: scrive solo se c'è qualcosa da pulire.
func (l *LettoreTelegram) pulisciFallimento(ctx context.Context) error {
	v, err := leggiValore(ctx, l.d.Lettura, inImpostazioni, chiaveAggInErrore)
	if err != nil || v == "" {
		return err
	}
	return l.salva(ctx, chiaveAggInErrore, "")
}

// salvaErrore è record_error/1: il motivo (al più 500 caratteri) e l'ora.
func (l *LettoreTelegram) salvaErrore(ctx context.Context, motivo string) error {
	if r := []rune(motivo); len(r) > lunghezzaUltimoErrore {
		motivo = string(r[:lunghezzaUltimoErrore])
	}
	if err := l.salva(ctx, ChiaveErroreLettore, motivo); err != nil {
		return err
	}
	return l.salva(ctx, ChiaveErroreLettoreIl, l.o.Adesso().UTC().Format(time.RFC3339Nano))
}

// pulisciErrore è clear_error/0: scrive solo se c'è un errore da pulire (un canale sano legge ogni 25
// secondi).
func (l *LettoreTelegram) pulisciErrore(ctx context.Context) error {
	v, err := leggiValore(ctx, l.d.Lettura, inImpostazioni, ChiaveErroreLettore)
	if err != nil || v == "" {
		return err
	}
	if err := l.salva(ctx, ChiaveErroreLettore, ""); err != nil {
		return err
	}
	return l.salva(ctx, ChiaveErroreLettoreIl, "")
}

func (l *LettoreTelegram) salva(ctx context.Context, chiave, valore string) error {
	return scriviImpostazione(ctx, l.d.Scrittura, chiave, valore, l.o.Adesso().UTC().Format(db.FormatoIstante))
}

// gestisci è handle/3: un messaggio di una chat privata, o un pulsante premuto in una chat privata; tutto
// il resto (gruppi, canali, altre forme) è fuori dal flusso.
func (l *LettoreTelegram) gestisci(ctx context.Context, agg map[string]any, cfg ConfigTelegram) error {
	if m, ok := agg["message"].(map[string]any); ok {
		if chat, ok := m["chat"].(map[string]any); ok && chat["type"] == "private" {
			if id, ok := intero(chat["id"]); ok {
				return l.messaggio(ctx, id, chat, m, cfg)
			}
		}
	}
	if q, ok := agg["callback_query"].(map[string]any); ok {
		cb, ok := q["id"].(string)
		m, ok2 := q["message"].(map[string]any)
		if ok && ok2 {
			if chat, ok := m["chat"].(map[string]any); ok && chat["type"] == "private" {
				if id, ok := intero(chat["id"]); ok {
					return l.pulsante(ctx, id, chat, cb, q, cfg)
				}
			}
		}
	}
	return nil
}

// daUnBot dice se il mittente è un bot (from.is_bot vero).
func daUnBot(v map[string]any) bool {
	from, ok := v["from"].(map[string]any)
	return ok && from["is_bot"] == true
}

// chatDelNegozio è shop_chat?/2: la chat del negozio, per numero o per @nome.
func chatDelNegozio(chat map[string]any, id int64, cfg ConfigTelegram) bool {
	if strconv.FormatInt(id, 10) == cfg.Chat {
		return true
	}
	nome, ok := chat["username"].(string)
	return ok && "@"+nome == cfg.Chat
}

func (l *LettoreTelegram) messaggio(ctx context.Context, chat int64, c, m map[string]any, cfg ConfigTelegram) error {
	if daUnBot(m) || chatDelNegozio(c, chat, cfg) {
		return nil
	}
	// Qui si aggancerà la 075 (CLI-49, TelegramReplies.bag/2): un messaggio che risponde
	// (reply_to_message) a una notifica di una busta, e che non è un comando del bot, va nella
	// conversazione della busta. Finché non c'è, il lettore fa come lo standalone quando la notifica non è
	// di nessuna busta: il messaggio è un comando o testo libero.
	return l.comando(ctx, chat, m["text"], cfg)
}

// reSpazi è \s+ di String.split/3 senza «u»: gli spazi ASCII.
var reSpazi = regexp.MustCompile("[\t\n\v\f\r ]+")

// comando è command/4: /start <token>, /stop e /disattiva, /privacy; il resto (anche /start senza token,
// /stop d'una chat senza iscrizioni, un messaggio senza testo) riceve la risposta fissa.
func (l *LettoreTelegram) comando(ctx context.Context, chat int64, v any, cfg ConfigTelegram) error {
	testo, ok := v.(string)
	if !ok {
		return l.rispostaFissa(ctx, chat, cfg)
	}
	pezzi := reSpazi.Split(strings.TrimSpace(testo), 2)
	nome, _, _ := strings.Cut(pezzi[0], "@")
	switch strings.ToLower(nome) {
	case "/start":
		if len(pezzi) == 2 {
			return l.avvia(ctx, chat, pezzi[1], cfg)
		}
	case "/stop", "/disattiva":
		return l.ferma(ctx, chat, cfg)
	case "/privacy":
		return l.privacy(ctx, chat, cfg)
	}
	return l.rispostaFissa(ctx, chat, cfg)
}

// tastieraConsenso è la tastiera del benvenuto: «Sì» e «No grazie», ognuno su una riga, con l'iscrizione.
type pulsanteConsenso struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

type tastieraConsenso struct {
	InlineKeyboard [][]pulsanteConsenso `json:"inline_keyboard"`
}

// avvia è start/4. Il nome del negozio si legge prima di toccare le iscrizioni (lo standalone legge
// shop_name/0 una volta, prima di gestire): se non si legge l'aggiornamento fallisce senza aver consumato
// il token, e il tentativo dopo dà il benvenuto, non «link non valido» (review della 073).
func (l *LettoreTelegram) avvia(ctx context.Context, chat int64, token string, cfg ConfigTelegram) error {
	negozio, err := l.isc.NomeNegozio(ctx)
	if err != nil {
		return err
	}
	id, err := l.isc.AvviaTelegram(ctx, chat, token)
	switch {
	case errors.Is(err, ErrLinkTelegramNonValido):
		return l.rispondi(ctx, cfg, chat, testoLinkNonValido, "", nil)
	case err != nil:
		return err
	}
	n := strconv.FormatInt(id, 10)
	return l.rispondi(ctx, cfg, chat, benvenuto(negozio), formatoHTML, tastieraConsenso{InlineKeyboard: [][]pulsanteConsenso{
		{{Text: pulsanteSi, CallbackData: "cli:tx:1:" + n}},
		{{Text: pulsanteNo, CallbackData: "cli:tx:0:" + n}},
	}})
}

// benvenuto è welcome/1 (handshake.py della fonte), con il nome del negozio protetto per l'HTML.
func benvenuto(negozio string) string {
	return "👋 Sei stato collegato alla tua riparazione presso <b>" + protettoHTML(negozio) + "</b>.\n\n" +
		"Vuoi ricevere notifiche quando il negozio aggiorna lo stato o " +
		"ti scrive un messaggio? Scegli le tue preferenze qui sotto:\n\n" +
		"<i>Puoi sempre digitare /stop per disattivare tutto, " +
		"/privacy per leggere come trattiamo i tuoi dati.</i>"
}

// protettoHTML è Plug.HTML.html_escape/1.
var protettoHTML = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;").Replace

func (l *LettoreTelegram) ferma(ctx context.Context, chat int64, cfg ConfigTelegram) error {
	n, conosciuta, err := l.isc.FermaChatTelegram(ctx, chat)
	switch {
	case err != nil:
		return err
	case !conosciuta:
		return l.rispostaFissa(ctx, chat, cfg)
	}
	return l.rispondi(ctx, cfg, chat, "🔕 Notifiche disattivate per "+strconv.Itoa(n)+" iscrizione/i. Grazie!", "", nil)
}

// privacy è privacy/3: l'indirizzo dell'informativa sulla faccia pubblica. Solo Go: senza indirizzo
// pubblico non c'è un indirizzo da dare, e la chat riceve la risposta fissa.
func (l *LettoreTelegram) privacy(ctx context.Context, chat int64, cfg ConfigTelegram) error {
	if l.pubblico == "" {
		return l.rispostaFissa(ctx, chat, cfg)
	}
	negozio, err := l.isc.NomeNegozio(ctx)
	if err != nil {
		return err
	}
	return l.rispondi(ctx, cfg, chat, "<b>Privacy policy</b>\n• <b>"+protettoHTML(negozio)+"</b>: "+l.pubblico+"/privacy", formatoHTML, nil)
}

// pulsante è la seconda clausola di handle/3: un pulsante oltre il limite si lascia cadere tutto (né
// risposta né scrittura); altrimenti si risponde al pulsante (answerCallbackQuery) e poi press/4.
func (l *LettoreTelegram) pulsante(ctx context.Context, chat int64, c map[string]any, cb string, q map[string]any, cfg ConfigTelegram) error {
	if daUnBot(q) || chatDelNegozio(c, chat, cfg) || !l.limiti.permesso(chat, rispostaComando, l.o.Adesso().Unix()) {
		return nil
	}
	if err := l.rispondiAlPulsante(ctx, cfg, cb); err != "" {
		l.log.Warn("lettore Telegram: pulsante senza risposta", "motivo", err)
	}
	dati, ok := q["data"].(string)
	if !ok {
		return nil
	}
	pezzi := strings.Split(dati, ":")
	if len(pezzi) != 4 || pezzi[0] != "cli" || pezzi[1] != "tx" || pezzi[2] != "0" && pezzi[2] != "1" {
		return nil
	}
	id, ok := interoElixir(pezzi[3])
	if !ok {
		return nil
	}
	err := l.isc.ConsensoTelegram(ctx, chat, id, pezzi[2] == "1")
	switch {
	case errors.Is(err, ErrIscrizioneTelegramNonTrovata):
		return l.rispondi(ctx, cfg, chat, testoNonTrovata, "", nil)
	case err != nil:
		return err
	case pezzi[2] == "1":
		return l.rispondi(ctx, cfg, chat, testoConsensoAcceso, "", nil)
	}
	return l.rispondi(ctx, cfg, chat, testoConsensoSpento, "", nil)
}

// rispondi è reply/4: la risposta parte se il limite delle risposte lo permette, e si conta.
func (l *LettoreTelegram) rispondi(ctx context.Context, cfg ConfigTelegram, chat int64, testo, formato string, tastiera any) error {
	adesso := l.o.Adesso().Unix()
	if l.limiti.permesso(chat, rispostaComando, adesso) {
		l.manda(ctx, cfg, chat, testo, formato, tastiera)
		l.limiti.conta(chat, rispostaComando, adesso)
	}
	return nil
}

// rispostaFissa è answer_unknown/3: una volta ogni 10 minuti per chat (e 20 al minuto in tutto), contata
// nel contatore delle chat sconosciute; il resto si legge e resta senza risposta.
func (l *LettoreTelegram) rispostaFissa(ctx context.Context, chat int64, cfg ConfigTelegram) error {
	adesso := l.o.Adesso().Unix()
	if !l.limiti.permesso(chat, rispostaFissa, adesso) {
		return nil
	}
	negozio, err := l.isc.NomeNegozio(ctx)
	if err != nil {
		return err
	}
	l.manda(ctx, cfg, chat, "Questo è il bot di "+negozio+". Per le notifiche apri il link della tua riparazione.", "", nil)
	l.limiti.conta(chat, rispostaFissa, adesso)
	v, err := leggiValore(ctx, l.d.Lettura, inImpostazioni, ChiaveChatSconosciute)
	if err != nil {
		return err
	}
	n, ok := interoElixir(v)
	if !ok {
		n = 0
	}
	return l.salva(ctx, ChiaveChatSconosciute, strconv.FormatInt(n+1, 10))
}

// corpoRisposta è il corpo di send_text/4: chat (un numero) e testo, parse_mode e reply_markup solo se ci
// sono.
type corpoRisposta struct {
	ChatID      int64  `json:"chat_id"`
	Text        string `json:"text"`
	ParseMode   string `json:"parse_mode,omitempty"`
	ReplyMarkup any    `json:"reply_markup,omitempty"`
}

// manda è send_reply/4: sendMessage subito, senza coda. Una risposta rifiutata si scrive nel registro (il
// motivo, senza chat né testo) e si lascia: quello che l'aggiornamento ha scritto resta, e gestirlo di
// nuovo risponderebbe peggio. Anche un panico nell'invio si lascia cadere così (send_reply/4 con rescue,
// review della 073): nel registro solo il genere.
func (l *LettoreTelegram) manda(ctx context.Context, cfg ConfigTelegram, chat int64, testo, formato string, tastiera any) {
	defer func() {
		if v := recover(); v != nil {
			l.log.Warn("lettore Telegram: risposta interrotta da un panico", "tipo", fmt.Sprintf("%T", v))
		}
	}()
	j, err := jsonSenzaEscape(corpoRisposta{ChatID: chat, Text: testo, ParseMode: formato, ReplyMarkup: tastiera})
	if err != nil {
		l.log.Warn("lettore Telegram: risposta non scritta", "tipo", fmt.Sprintf("%T", err))
		return
	}
	stato, risposta, err := l.t.richiesta(ctx, cfg, http.MethodPost, "sendMessage", j)
	motivo := ""
	switch {
	case err != nil:
		motivo = errore(err, cfg)
	case stato != http.StatusOK:
		motivo = "telegram:http:" + strconv.Itoa(stato)
	default:
		if _, err := interpreta(risposta); err != nil {
			motivo = errore(err, cfg)
		}
	}
	if motivo != "" {
		l.log.Warn("lettore Telegram: risposta non accettata", "motivo", motivo)
	}
}

// rispondiAlPulsante è answer_callback/2: "" se Telegram l'ha presa, altrimenti il motivo.
func (l *LettoreTelegram) rispondiAlPulsante(ctx context.Context, cfg ConfigTelegram, cb string) string {
	j, err := jsonSenzaEscape(map[string]string{"callback_query_id": cb})
	if err != nil {
		return fmt.Sprintf("%T", err)
	}
	stato, _, err := l.t.richiesta(ctx, cfg, http.MethodPost, "answerCallbackQuery", j)
	switch {
	case err != nil:
		return errore(err, cfg)
	case stato != http.StatusOK:
		return "telegram:http:" + strconv.Itoa(stato)
	}
	return ""
}

// errore è il motivo di un Rifiuto senza token, o il genere di un altro errore.
func errore(err error, cfg ConfigTelegram) string {
	var r Rifiuto
	if errors.As(err, &r) {
		return pulisciToken(string(r), cfg)
	}
	return fmt.Sprintf("%T", err)
}

// decodifica legge un aggiornamento con i numeri esatti (json.Number); nil se non è JSON.
func decodifica(g json.RawMessage) any {
	d := json.NewDecoder(bytes.NewReader(g))
	d.UseNumber()
	var v any
	if d.Decode(&v) != nil {
		return nil
	}
	return v
}

// intero è is_integer/1 di un numero JSON: scritto senza parte decimale né esponente.
func intero(v any) (int64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	i, err := n.Int64()
	return i, err == nil
}

// Il genere di una risposta per i limiti.
const (
	rispostaFissa   = iota // la risposta fissa a una chat sconosciuta
	rispostaComando        // ogni altra risposta (/start, pulsanti, /stop, /privacy)
)

// I limiti di ReplyLimit, in secondi: la risposta fissa una volta ogni 10 minuti per chat e 20 al minuto
// in tutto; le altre 8 al minuto per chat e 30 al minuto in tutto. Niente più vecchio di 10 minuti resta.
const (
	fissaPerChat      = 600
	fisseInTutto      = 20
	altrePerChat      = 8
	altreInTutto      = 30
	finestraRisposte  = 60
	orizzonteRisposte = 600
)

// limiteRisposte è ReplyLimit: i momenti delle risposte (in secondi), per chat e in tutto, solo nella
// memoria del lettore: una chat non finisce mai nel database o nel registro per questo.
type limiteRisposte struct {
	mu         sync.Mutex
	fisse      map[int64][]int64
	fisseTutte []int64
	altre      map[int64][]int64
	altreTutte []int64
}

// pota dimentica quello che è oltre l'orizzonte (state/1).
func (r *limiteRisposte) pota(adesso int64) {
	dopo := adesso - orizzonteRisposte
	filtra := func(s []int64) []int64 {
		var out []int64
		for _, t := range s {
			if t > dopo {
				out = append(out, t)
			}
		}
		return out
	}
	r.fisseTutte, r.altreTutte = filtra(r.fisseTutte), filtra(r.altreTutte)
	for _, m := range []map[int64][]int64{r.fisse, r.altre} {
		for chat, s := range m {
			if s = filtra(s); len(s) == 0 {
				delete(m, chat)
			} else {
				m[chat] = s
			}
		}
	}
}

// dal conta i momenti dopo da (count_since/2).
func dal(s []int64, da int64) int {
	n := 0
	for _, t := range s {
		if t > da {
			n++
		}
	}
	return n
}

// permesso è allow?/2.
func (r *limiteRisposte) permesso(chat int64, genere int, adesso int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pota(adesso)
	if genere == rispostaFissa {
		for _, t := range r.fisse[chat] {
			if adesso-t < fissaPerChat {
				return false
			}
		}
		return dal(r.fisseTutte, adesso-finestraRisposte) < fisseInTutto
	}
	return dal(r.altre[chat], adesso-finestraRisposte) < altrePerChat && dal(r.altreTutte, adesso-finestraRisposte) < altreInTutto
}

// conta è record/2.
func (r *limiteRisposte) conta(chat int64, genere int, adesso int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pota(adesso)
	if r.fisse == nil {
		r.fisse, r.altre = map[int64][]int64{}, map[int64][]int64{}
	}
	if genere == rispostaFissa {
		r.fisse[chat] = append(r.fisse[chat], adesso)
		r.fisseTutte = append(r.fisseTutte, adesso)
		return
	}
	r.altre[chat] = append(r.altre[chat], adesso)
	r.altreTutte = append(r.altreTutte, adesso)
}
