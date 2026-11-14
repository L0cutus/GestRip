package buste

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// Gli avvisi Telegram al cliente e al negozio (slice 074: CLI-47, CLI-48, RIP-H-18, CLI-08, CLI-13), come
// TelegramNotices, TelegramNotices.Pending, QuoteDecision.notify_shop/3 e PublicMessage.notify_shop/3 dello
// standalone (go-baseline):
//   - il cambio di stato del personale dalla scheda e dalla lista (CambiaStato, CambiaStatoDallaLista,
//     PassoRapido) «congela» nella sua transazione, **prima** del cambio, le chat iscritte alla busta con il
//     consenso (il trigger della 0014 spegne le iscrizioni nella stessa scrittura che consegna la busta), e
//     dopo il commit prepara l'avviso (NotificaTelegram) che il popup mostra: niente si accoda. I cambi senza
//     popup (pannello del QR, lotti, import, lavori, riapertura) non congelano niente;
//   - solo «Invia» del popup accoda (AutorizzaNotificaTelegram): il testo che il popup ha mostrato, parola
//     per parola, alle chat congelate (anche a quella che la consegna ha spento), se nessun cambio di stato
//     successivo della busta l'ha superato (versioni_stato_buste), il canale è ancora acceso e la busta è
//     ancora del cliente diretto; le chat tagliate dalla revoca del link restano fuori. Il controllo della
//     versione e le righe in una transazione;
//   - il nuovo messaggio del negozio (NotificaNuovoMessaggio) va subito, senza popup, alle chat iscritte con
//     il consenso adesso, con il testo tagliato a 400 caratteri;
//   - ogni riga: il testo HTML di Telegram con i valori protetti (&, <, >), il pulsante «📂 Apri scheda
//     riparazione» sul link stabile /r/ della busta (garantisciLinkIn), una riga per chat, tutte o nessuna;
//   - un testo salvato che non si compone non accoda niente e lo dice (*ModelloNonValido), mai il predefinito;
//   - il canale (telegramClienti, solo Go): la coda con Telegram acceso (D-03) e il bot configurato, «Notifiche
//     Telegram ai clienti» accese e l'indirizzo pubblico, senza il quale il pulsante non avrebbe un link. Lo
//     standalone chiede solo le notifiche e il bot (TelegramInbound.active?/0);
//   - gli avvisi alla chat del negozio (CLI-08 dopo la risposta al preventivo, CLI-13 dopo il messaggio del
//     cliente) partono in una transazione loro, dopo il commit: senza Telegram o con la coda che rifiuta la
//     risposta e il messaggio restano. Il mancato avviso del messaggio va in repair.message_notice_not_queued
//     (not_configured o error); quello del preventivo in repair.quote_notice_not_queued con gli stessi
//     motivi (D-86, solo Go, slice 075: lo standalone non lascia traccia). Il nome del cliente non viaggia
//     mai.

// PulsanteTelegram è @button: il pulsante di ogni avviso al cliente.
const PulsanteTelegram = "📂 Apri scheda riparazione"

const (
	// lunghezzaDescrizioneTelegram e lunghezzaTestoTelegram sono @description_length e @text_length, in code
	// point (come len e le slice di Python della sorgente).
	lunghezzaDescrizioneTelegram = 100
	lunghezzaTestoTelegram       = 400
	// lunghezzaAvvisoNegozio è @notice_chars di PublicMessage, in grafemi (String.length/1).
	lunghezzaAvvisoNegozio  = 400
	azioneAvvisoNonAccodato = "repair.message_notice_not_queued"
	// azionePreventivoNonAccodato è la riga del mancato avviso della risposta al preventivo (D-86, solo Go:
	// lo standalone non lascia traccia).
	azionePreventivoNonAccodato = "repair.quote_notice_not_queued"
)

// visivoStati è @visual: l'emoji e il titolo di ogni stato (_STATO_VISUAL della sorgente).
var visivoStati = map[string][2]string{
	"in_lavorazione":      {"🔧", "In lavorazione"},
	"preventivo":          {"📋", "Preventivo proposto"},
	"pronta":              {"✨", "Pronta per il ritiro"},
	"consegnata":          {"🤝", "Consegnata"},
	"in_attesa_materiali": {"⏳", "In attesa materiali"},
	"annullata":           {"❌", "Annullata"},
	"valutata":            {"⚖️", "Valutazione completata"},
	"in_valutazione":      {"🔎", "In valutazione"},
	"perfezionata":        {"💰", "Pagata"},
	"chiusa":              {"🔒", "Chiusa"},
}

// I rifiuti di AutorizzaNotificaTelegram, con i nomi dello standalone: niente è accodato. Il canale spento
// (:channel_off) è ErrTelegramSpento, lo stesso dell'iscrizione.
var (
	ErrTelegramSuperata     = errors.New("buste: notifica Telegram superata da un cambio di stato successivo") // :superseded
	ErrTelegramNonCliente   = errors.New("buste: la busta non è più del cliente diretto")                      // :not_customer
	ErrTelegramLinkRevocato = errors.New("buste: link della busta revocato o rigenerato")                      // :link_revoked
)

// Error fa di un testo che non si compone un errore: la famiglia e l'avviso, mai il testo.
func (m *ModelloNonValido) Error() string {
	return "buste: testo «" + string(m.Famiglia) + "» non valido: " + m.Avviso
}

// NotificaTelegram è TelegramNotices.Pending: l'avviso di un cambio di stato in attesa di «Invia» o
// «Annulla», con la busta, lo stato raggiunto, la versione che il cambio ha lasciato, il testo HTML che il
// popup mostra (e che «Invia» accoda così com'è) e il pulsante. Le chat congelate restano dentro il
// pacchetto: String e GoString non le scrivono, così un registro o un panico non le porta mai.
type NotificaTelegram struct {
	BustaID   int64
	Numero    string // il numero visibile della busta (D-64), per il popup
	Stato     string
	Versione  int
	Anteprima string // il testo HTML di tg_cliente_cambio_stato
	Pulsante  string
	chat      []int64
}

// String è la notifica senza le chat.
func (n *NotificaTelegram) String() string {
	return "NotificaTelegram{busta " + strconv.FormatInt(n.BustaID, 10) + ", " + n.Stato + ", versione " + strconv.Itoa(n.Versione) +
		", " + strconv.Itoa(len(n.chat)) + " chat}"
}

// GoString è String anche per %#v.
func (n *NotificaTelegram) GoString() string { return n.String() }

// telegramClienti è sendable?/1 senza la controparte: il canale degli avvisi al cliente (vedi sopra).
func (b *Buste) telegramClienti(ctx context.Context) (bool, error) {
	if b.posta == nil || b.pubblico == "" {
		return false, nil
	}
	if ok, err := b.posta.CanaleAttivo(ctx, uscita.CanaleTelegram); err != nil || !ok {
		return false, err
	}
	return uscita.NotificheClientiAccese(ctx, b.d.Lettura)
}

// chatIscritte è recipients/1: le chat iscritte alla busta con il consenso e non fermate, nell'ordine in
// cui si sono iscritte.
func chatIscritte(ctx context.Context, q interrogatore, id int64) ([]int64, error) {
	return chatDove(ctx, q, `busta_id = ? AND consenso = 1 AND fermata_il IS NULL`, id)
}

// chatTagliate è TelegramSubscriptions.cut_off_chats/1: le chat tagliate con il link della busta e non
// iscritte di nuovo (un /start nuovo toglie il motivo).
func chatTagliate(ctx context.Context, q interrogatore, id int64) ([]int64, error) {
	return chatDove(ctx, q, `busta_id = ? AND motivo_fermata = 'link'`, id)
}

func chatDove(ctx context.Context, q interrogatore, dove string, id int64) ([]int64, error) {
	righe, err := q.QueryContext(ctx, `SELECT chat_id FROM iscrizioni_telegram WHERE `+dove+` ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer righe.Close()
	var chat []int64
	for righe.Next() {
		var c int64
		if err := righe.Scan(&c); err != nil {
			return nil, err
		}
		chat = append(chat, c)
	}
	return chat, righe.Err()
}

// bustaNotifica è quello che i testi Telegram leggono della busta.
type bustaNotifica struct {
	id                       int64
	numero, codiceQR         string
	nome, descrizione, stato string
	oreficeriaID             *int64
}

func leggiBustaNotifica(ctx context.Context, q lettore, id int64) (bustaNotifica, error) {
	var x bustaNotifica
	var qr sql.NullString
	var oref sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT id, numero, codice_qr, nome_cliente, descrizione, stato, oreficeria_id FROM buste
		WHERE id = ? AND cancellata_il IS NULL`, id).Scan(&x.id, &x.numero, &qr, &x.nome, &x.descrizione, &x.stato, &oref)
	if errors.Is(err, sql.ErrNoRows) {
		return bustaNotifica{}, ErrBustaNonTrovata
	}
	if err != nil {
		return bustaNotifica{}, err
	}
	x.codiceQR, x.oreficeriaID = qr.String, nullaIntero(oref)
	return x, nil
}

// cliente dice se la busta è del cliente diretto (Recipient.party/1 = :customer).
func (x bustaNotifica) cliente() bool { return Classificazione(x.oreficeriaID) != ClasseEsterna }

// protetto è escape/1: &, < e > come entità, perché un valore non diventi mai un tag di Telegram.
var protetto = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// primoNome è first_name/1: la prima parola del nome (String.split/1, spazi Unicode), "" senza.
func primoNome(nome string) string {
	if f := strings.Fields(nome); len(f) > 0 {
		return f[0]
	}
	return ""
}

// tagliaPunti è cut/2: oltre max code point, i primi max e «…».
func tagliaPunti(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}

// slugStato è slug/1: lo stato con gli spazi della sorgente al posto di «_».
func slugStato(s string) string { return strings.ReplaceAll(s, "_", " ") }

// maiuscola è String.capitalize/1: la prima lettera maiuscola, le altre minuscole.
func maiuscola(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + strings.ToLower(s[n:])
}

// valoriTelegram sono i valori comuni dei due testi (nome, numero come la D-82, negozio).
func (b *Buste) valoriTelegram(ctx context.Context, x bustaNotifica) (map[string]string, error) {
	negozio, err := b.NomeNegozio(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]string{"nome": primoNome(x.nome), "numero": NumeroNeiTesti(x.codiceQR, x.numero), "negozio": negozio}, nil
}

// componiTelegram è render/2 con i valori protetti.
func (b *Buste) componiTelegram(ctx context.Context, f Famiglia, valori map[string]string) (string, error) {
	for k, v := range valori {
		valori[k] = protetto.Replace(v)
	}
	s, m, err := b.componiFamiglia(ctx, f, valori)
	switch {
	case err != nil:
		return "", err
	case m != nil:
		return "", m
	}
	return s, nil
}

// componiCambioStato è compose_status/2: tg_cliente_cambio_stato della busta nello stato dato.
func (b *Buste) componiCambioStato(ctx context.Context, x bustaNotifica, stato string) (string, error) {
	valori, err := b.valoriTelegram(ctx, x)
	if err != nil {
		return "", err
	}
	v, ok := visivoStati[stato]
	if !ok {
		v = [2]string{"🔔", maiuscola(slugStato(stato))}
	}
	riga, _, _ := strings.Cut(strings.TrimFunc(x.descrizione, unicode.IsSpace), "\n")
	valori["emoji"], valori["titolo_stato"], valori["stato"] = v[0], v[1], slugStato(stato)
	valori["descrizione"] = tagliaPunti(riga, lunghezzaDescrizioneTelegram)
	return b.componiTelegram(ctx, FamTGCambioStato, valori)
}

// componiNuovoMessaggio è compose_message/2: tg_cliente_nuovo_messaggio con il testo del negozio.
func (b *Buste) componiNuovoMessaggio(ctx context.Context, x bustaNotifica, testo string) (string, error) {
	valori, err := b.valoriTelegram(ctx, x)
	if err != nil {
		return "", err
	}
	valori["testo"] = tagliaPunti(strings.TrimFunc(testo, unicode.IsSpace), lunghezzaTestoTelegram)
	return b.componiTelegram(ctx, FamTGNuovoMessaggio, valori)
}

// preparaNotifica è prepare/3, dopo il commit del cambio: nil senza chat, su una busta che non è del
// cliente diretto o con il canale spento; un testo che non si compone è il suo *ModelloNonValido.
func (b *Buste) preparaNotifica(ctx context.Context, id int64, chat []int64, versione int) (*NotificaTelegram, error) {
	if len(chat) == 0 {
		return nil, nil
	}
	x, err := leggiBustaNotifica(ctx, b.d.Lettura, id)
	if errors.Is(err, ErrBustaNonTrovata) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !x.cliente() {
		return nil, nil
	}
	if ok, err := b.telegramClienti(ctx); err != nil || !ok {
		return nil, err
	}
	testo, err := b.componiCambioStato(ctx, x, x.stato)
	if err != nil {
		return nil, err
	}
	numero, _ := NumeroVisibile(x.codiceQR, x.numero)
	return &NotificaTelegram{BustaID: id, Numero: numero, Stato: x.stato, Versione: versione, Anteprima: testo,
		Pulsante: PulsanteTelegram, chat: slices.Clone(chat)}, nil
}

// AutorizzaNotificaTelegram è authorize/2, l'«Invia» del popup: in una transazione la versione dello stato
// (ErrTelegramSuperata se un cambio successivo l'ha superata), la busta (ErrBustaNonTrovata se è sparita o
// nel cestino), il canale (ErrTelegramSpento), la controparte (ErrTelegramNonCliente) e le righe, una per
// chat congelata che il link non ha tagliato (ErrTelegramLinkRevocato senza nessuna). Dà quante righe ha
// accodato; con un errore niente è scritto.
func (b *Buste) AutorizzaNotificaTelegram(ctx context.Context, n *NotificaTelegram, c audit.Contesto) (int, error) {
	if n == nil || len(n.chat) == 0 {
		return 0, ErrTelegramSuperata
	}
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	v, err := versione(ctx, tx, n.BustaID)
	switch {
	case err != nil:
		return 0, err
	case v != n.Versione:
		return 0, ErrTelegramSuperata
	}
	x, err := leggiBustaNotifica(ctx, tx, n.BustaID)
	if err != nil {
		return 0, err
	}
	switch ok, err := b.telegramClienti(ctx); {
	case err != nil:
		return 0, err
	case !ok:
		return 0, ErrTelegramSpento
	}
	if !x.cliente() {
		return 0, ErrTelegramNonCliente
	}
	conte, err := b.accodaTelegram(ctx, tx, x, n.Anteprima, n.chat, c)
	if err != nil {
		return 0, err
	}
	return conte, tx.Commit()
}

// NotificaNuovoMessaggio è notify_new_message/3: il nuovo messaggio del negozio sulla busta id a ogni chat
// iscritta adesso con il consenso, senza popup. Dà quante righe ha accodato: 0 su una busta che non è del
// cliente diretto, con il canale spento o senza iscritti. Un testo che non si compone è il suo
// *ModelloNonValido; con un errore niente è scritto.
func (b *Buste) NotificaNuovoMessaggio(ctx context.Context, id int64, testo string, c audit.Contesto) (int, error) {
	x, err := leggiBustaNotifica(ctx, b.d.Lettura, id)
	if err != nil {
		return 0, err
	}
	if !x.cliente() {
		return 0, nil
	}
	switch ok, err := b.telegramClienti(ctx); {
	case err != nil:
		return 0, err
	case !ok:
		return 0, nil
	}
	chat, err := chatIscritte(ctx, b.d.Lettura, id)
	if err != nil || len(chat) == 0 {
		return 0, err
	}
	corpo, err := b.componiNuovoMessaggio(ctx, x, testo)
	if err != nil {
		return 0, err
	}
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	conte, err := b.accodaTelegram(ctx, tx, x, corpo, chat, c)
	if err != nil {
		return 0, err
	}
	return conte, tx.Commit()
}

// accodaTelegram è queue_messages/4 nella transazione tx: le chat tagliate con il link fuori
// (ErrTelegramLinkRevocato senza nessuna), il link stabile della busta, una riga per chat con il testo HTML e
// il pulsante. Tutte o nessuna: un errore lascia a chi chiama il ROLLBACK.
func (b *Buste) accodaTelegram(ctx context.Context, tx *sql.Tx, x bustaNotifica, corpo string, chat []int64, c audit.Contesto) (int, error) {
	tagliate, err := chatTagliate(ctx, tx, x.id)
	if err != nil {
		return 0, err
	}
	var restano []int64
	for _, ch := range chat {
		if !slices.Contains(tagliate, ch) {
			restano = append(restano, ch)
		}
	}
	if len(restano) == 0 {
		return 0, ErrTelegramLinkRevocato
	}
	token, err := b.garantisciLinkIn(ctx, tx, x.id, x.oreficeriaID, c)
	if err != nil {
		return 0, err
	}
	cont := uscita.Contenuto{Testo: corpo, Formato: uscita.FormatoHTML,
		Tastiera: &uscita.PulsanteURL{Testo: PulsanteTelegram, URL: b.pubblico + "/r/" + token}}
	for _, ch := range restano {
		if _, err := b.posta.AccodaIn(ctx, tx, uscita.Destinazione{BustaID: &x.id, Canale: uscita.CanaleTelegram,
			Destinatario: "customer", Indirizzo: strconv.FormatInt(ch, 10)}, cont, c); err != nil {
			return 0, err
		}
	}
	return len(restano), nil
}

// avvisaNegozio è destination_for_shop(:telegram) ed enqueue/3 in una transazione sua: il testo semplice
// alla chat del negozio, senza busta (le righe del negozio non ne portano). Senza coda o senza la
// configurazione uscita.ErrNonConfigurato; un panico è un errore, mai un panico di chi chiama (rescue).
func (b *Buste) avvisaNegozio(ctx context.Context, testo string, c audit.Contesto) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = errors.New("buste: avviso al negozio andato in panico")
		}
	}()
	if b.posta == nil {
		return uscita.ErrNonConfigurato
	}
	conf, err := uscita.LeggiTelegram(ctx, b.d.Lettura)
	if err != nil {
		return err
	}
	if !conf.Usabile() {
		return uscita.ErrNonConfigurato
	}
	_, err = b.posta.Accoda(ctx, uscita.Destinazione{Canale: uscita.CanaleTelegram, Destinatario: "shop", Indirizzo: conf.Chat},
		uscita.Contenuto{Testo: testo}, c)
	return err
}

// testoAvvisoMessaggio è notice_content/2 di PublicMessage: chi ha scritto, la busta, i primi 400 caratteri
// (grafemi) e il link della scheda del Go (indirizzo_interno + /repairs/<id>). Mai il nome del cliente.
func (b *Buste) testoAvvisoMessaggio(id int64, numero string, oreficeria bool, testo string) string {
	chi := "dal cliente"
	if oreficeria {
		chi = "dall'orefice"
	}
	if uniseg.GraphemeClusterCount(testo) > lunghezzaAvvisoNegozio {
		g := uniseg.NewGraphemes(testo)
		fine := 0
		for i := 0; i < lunghezzaAvvisoNegozio && g.Next(); i++ {
			_, fine = g.Positions()
		}
		testo = testo[:fine] + "…"
	}
	return "💬 Messaggio " + chi + " — busta " + numero + "\n\n" + testo + "\n\nApri la scheda: " + b.interno + "/repairs/" +
		strconv.FormatInt(id, 10)
}

// avvisaMessaggio è notify_shop/3 e record_miss/3 di PublicMessage, dopo il commit del messaggio: l'avviso
// alla chat del negozio o la riga repair.message_notice_not_queued con il motivo (not_configured, error).
// Né l'avviso né la riga toccano il messaggio, già salvato.
func (b *Buste) avvisaMessaggio(ctx context.Context, id int64, numero string, oreficeria bool, testo string, c audit.Contesto) {
	// Il numero nel testo come la D-82 (quello dell'etichetta, senza «N.»); nella riga quello salvato.
	var qr sql.NullString
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT codice_qr FROM buste WHERE id = ?`, id).Scan(&qr)
	if err == nil {
		err = b.avvisaNegozio(ctx, b.testoAvvisoMessaggio(id, NumeroNeiTesti(qr.String, numero), oreficeria, testo), c)
	}
	if err == nil {
		return
	}
	b.registraAvvisoMancato(ctx, azioneAvvisoNonAccodato, id, numero, err, c)
}

// registraAvvisoMancato è record_miss/3 di PublicMessage: la riga d'audit del mancato avviso al negozio con
// il numero salvato della busta e il motivo (not_configured senza coda, configurazione o canale; error per
// tutto il resto). Una riga che non si scrive si lascia: l'avviso è già perso, la risposta resta.
func (b *Buste) registraAvvisoMancato(ctx context.Context, azione string, id int64, numero string, err error, c audit.Contesto) {
	motivo := "error"
	if errors.Is(err, uscita.ErrNonConfigurato) {
		motivo = "not_configured"
	}
	_ = b.reg.Scrivi(ctx, b.d.Scrittura, c, azione, "repair", id, "ok", map[string]any{"bag_number": numero, "reason": motivo})
}

// rispostaPreventivo è quello che l'avviso della risposta al preventivo legge della busta.
type rispostaPreventivo struct {
	numero        string
	testoImporto  *string
	centesimi     *int64
	motivoRifiuto *string
}

// testoAvvisoPreventivo è notice_content/2 di QuoteDecision: la risposta, la busta, l'importo (il testo
// scritto com'è, o i centesimi con Money.label/1) e, per l'approvazione, il passaggio in lavorazione; per il
// rifiuto il motivo o «Nessun motivo indicato.».
func testoAvvisoPreventivo(evento string, r rispostaPreventivo) string {
	importo := ""
	switch {
	case r.testoImporto != nil && *r.testoImporto != "":
		importo = "Importo: " + *r.testoImporto + "\n"
	case r.centesimi != nil:
		importo = "Importo: " + EtichettaCentesimi(*r.centesimi) + "\n"
	}
	if evento == "approved" {
		return "🤝 Preventivo APPROVATO — busta " + r.numero + "\n" + importo + "La busta è passata in lavorazione."
	}
	motivo := "Nessun motivo indicato."
	if r.motivoRifiuto != nil {
		motivo = "Motivo: " + *r.motivoRifiuto
	}
	return "🚫 Preventivo RIFIUTATO — busta " + r.numero + "\n" + importo + motivo
}
