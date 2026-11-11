package buste

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
)

// Il popup «📲 Avvisa cliente» (CLI-29…36, RIP-H-17, RIP-S-20, RIP-S-23), come CustomerNotice,
// Outgoing (destination_for_repair/2, compose/3, compose_reply_notice/2, resolve_link/3,
// record_handoff/3), ReadyNotice (state/1, notify/4, reset/2) e Workshop.Recipient dello standalone
// (go-baseline):
//   - ApriAvviso compone la bozza e non scrive niente, nemmeno il link: {link} resta il segnaposto;
//   - InviaAvviso è l'«Invia» del popup e la sola scrittura: rilegge la busta e rifiuta se dal popup
//     sono cambiati controparte, numero, canale o tipo; poi il link (in una transazione sua), poi la
//     riga WhatsApp (con l'avviso di ritiro, se lo è, nella stessa transazione del blocco);
//   - AzzeraAvviso riapre il blocco dell'avviso di ritiro, con la riga che lo dice.
// Il canale è sempre WhatsApp: Telegram arriva con la T4.7, e allora sceglieTelegram decide (oggi
// nessuna chat: canale spento). Il numero nei testi è quello visibile senza «N.» (D-64, D-82).

// ScopoAvviso è perché si apre il popup (i purpose dello standalone; il sollecito dell'artigiano
// arriva con la T4.8).
type ScopoAvviso string

const (
	ScopoControparte ScopoAvviso = "controparte" // «📲 Avvisa cliente» della scheda e della lista
	ScopoRisposta    ScopoAvviso = "risposta"    // dopo «Rispondi al cliente»
)

// TipoAvviso è il kind della bozza.
type TipoAvviso string

const (
	TipoPronta        TipoAvviso = "ready"  // l'avviso di ritiro, una volta sola
	TipoAggiornamento TipoAvviso = "update" // ogni altro messaggio alla controparte
	TipoRisposta      TipoAvviso = "reply"  // dopo una risposta: sempre l'aggiornamento
)

// ProblemaAvviso dice perché la bozza non si può mandare.
type ProblemaAvviso string

// ProblemaNessunRecapito è :no_address: la controparte non ha un numero con delle cifre.
const ProblemaNessunRecapito ProblemaAvviso = "no_address"

// CanaleWhatsApp è l'unico canale del popup fino alla T4.7.
const CanaleWhatsApp = "whatsapp"

// BozzaAvviso è quello che il popup mostra e che il suo «Invia» ricontrolla (CustomerNotice.Draft).
type BozzaAvviso struct {
	BustaID        int64
	Numero         string // il numero visibile (D-64), per «Busta …»
	Scopo          ScopoAvviso
	Tipo           TipoAvviso
	Canale         string
	Parte          Parte
	Nome           string // il nome della controparte, senza spazi ai bordi ("" se manca)
	Indirizzo      string // il numero per wa.me; "" con un problema
	OreficeriaID   *int64
	Chiusa         bool // consegnata, annullata o chiusa (la lista non apre il popup)
	Problema       ProblemaAvviso
	Modello        *ModelloNonValido // il testo salvato che non si compone: nessuna proposta
	Testo          string
	TelegramSpento bool // il cliente diretto con le notifiche Telegram spente (:telegram_off)
}

// Disegnata è ciò per cui il popup era stato disegnato, rimandato dal suo modulo.
type Disegnata struct {
	Scopo        ScopoAvviso
	Tipo         TipoAvviso
	Canale       string
	Indirizzo    string
	OreficeriaID *int64
}

// AvvisoInviato è l'esito dell'«Invia»: l'indirizzo wa.me del testo registrato.
type AvvisoInviato struct {
	URL  string
	Tipo TipoAvviso
}

// I rifiuti dell'«Invia» (i motivi dello standalone).
var (
	ErrAvvisoVuoto         = errors.New("buste: messaggio vuoto")                  // :empty
	ErrControparteCambiata = errors.New("buste: controparte cambiata")             // :counterparty_changed
	ErrRecapitoCambiato    = errors.New("buste: recapito cambiato")                // :number_changed
	ErrCanaleCambiato      = errors.New("buste: canale cambiato")                  // :channel_changed
	ErrTipoCambiato        = errors.New("buste: diventata da avvisare")            // :kind_changed
	ErrGiaAvvisata         = errors.New("buste: già avvisata")                     // :already_notified
	ErrInLottoAperto       = errors.New("buste: in un lotto aperto")               // :in_open_lot
	ErrNonPronta           = errors.New("buste: non più pronta")                   // :not_ready
	ErrLinkNonSalvato      = errors.New("buste: link della busta non creato")      // :link_not_saved
	ErrTelegramSoloCliente = errors.New("buste: {telegram_optin} solo al cliente") // :telegram_customer_only
	ErrNonAvvisata         = errors.New("buste: busta non avvisata")               // :not_notified
)

// ErrSenzaIndirizzoPubblico è ErrLinkNonSalvato perché indirizzo_pubblico manca (D-46 estesa, slice
// 067): il testo con {link} o {telegram_optin} non parte e la pagina dice cosa configurare.
var ErrSenzaIndirizzoPubblico = fmt.Errorf("%w: indirizzo pubblico mancante", ErrLinkNonSalvato)

const (
	segnapostoLink     = "{link}"
	segnapostoTelegram = "{telegram_optin}"
)

// rigaAvviso è la busta come la legge l'avviso.
type rigaAvviso struct {
	id                                 int64
	numero, codiceQR, stato            string
	compraOro                          bool
	nomeCliente, telefonoCliente       string
	oreficeriaID                       *int64
	oreficeriaNome, oreficeriaTelefono *string
	descrizione                        string
	notePubbliche                      *string
	preventivoTesto, prezzoTesto       *string
	preventivoCent, prezzoCent         *int64
	consegnaTesto, consegnaData        *string
	linkInviato, avvisoPronta          *string
	lotto                              *int64
	lottoAperto, conArtigiano          bool
	giorniInPiu                        int64
}

func (r rigaAvviso) b2b() bool { return r.oreficeriaID != nil && *r.oreficeriaID > 1 }

func (r rigaAvviso) parte() Parte {
	if r.b2b() {
		return ParteOreficeria
	}
	return ParteCliente
}

// pronta è Repair.ready?/1: la riparazione pronta o la scheda compro oro valutata.
func (r rigaAvviso) pronta() bool {
	if r.compraOro {
		return r.stato == "valutata"
	}
	return r.stato == "pronta"
}

// statoAvviso è ReadyNotice.state/1.
func (r rigaAvviso) statoAvviso() StatoAvvisoPronta {
	switch {
	case !r.pronta():
		return AvvisoNonApplicabile
	case r.avvisoPronta != nil:
		return AvvisoFatto
	case r.lotto != nil && r.lottoAperto:
		return AvvisoInLottoAperto
	}
	return AvvisoDaFare
}

// destinatario è Recipient.resolve/1: l'oreficeria su una busta B2B (mai il cliente finale), il
// cliente altrimenti; false se quella parte non ha un telefono. Nome e telefono senza spazi ai bordi.
func (r rigaAvviso) destinatario() (Destinatario, bool) {
	d := Destinatario{Parte: r.parte(), Nome: r.nomeCliente, Telefono: r.telefonoCliente}
	if r.b2b() {
		d.Nome, d.Telefono = valoreTesto(r.oreficeriaNome), valoreTesto(r.oreficeriaTelefono)
	}
	d.Nome, d.Telefono = strings.TrimSpace(d.Nome), strings.TrimSpace(d.Telefono)
	return d, d.Telefono != ""
}

// indirizzoWhatsApp è destination_for_repair(repair, :whatsapp): il numero per wa.me della
// controparte, o false (nessun telefono, o nessuna cifra).
func (r rigaAvviso) indirizzoWhatsApp() (Destinatario, string, bool) {
	d, ok := r.destinatario()
	if !ok {
		return d, "", false
	}
	n := NumeroWhatsApp(d.Telefono)
	return d, n, n != ""
}

func valoreTesto(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// presente è present/1: il testo senza spazi ai bordi, o "" (nil nello standalone).
func presente(p *string) string { return strings.TrimSpace(valoreTesto(p)) }

const colonneAvviso = `b.id, b.numero, coalesce(b.codice_qr, ''), b.stato, b.compro_oro, b.nome_cliente, b.telefono_cliente,
	b.oreficeria_id, o.nome, o.telefono, b.descrizione, b.note_pubbliche, b.preventivo_testo, b.prezzo_testo,
	b.preventivo_centesimi, b.prezzo_centesimi, b.data_prevista_testo, b.data_prevista, b.link_inviato_il, b.avviso_pronta_il,
	b.lotto_avviso_id, EXISTS (SELECT 1 FROM lotti_avviso l WHERE l.id = b.lotto_avviso_id AND l.inviato_il IS NULL AND l.chiuso_il IS NULL),
	b.consegnata_artigiano_il IS NOT NULL, b.giorni_in_piu`

// leggiAvviso legge la busta (fuori dal cestino) per l'avviso.
func leggiAvviso(ctx context.Context, q lettore, id int64) (rigaAvviso, error) {
	var r rigaAvviso
	var oro int
	err := q.QueryRowContext(ctx, `SELECT `+colonneAvviso+` FROM buste b LEFT JOIN oreficerie o ON o.id = b.oreficeria_id
		WHERE b.id = ? AND b.cancellata_il IS NULL`, id).Scan(&r.id, &r.numero, &r.codiceQR, &r.stato, &oro, &r.nomeCliente,
		&r.telefonoCliente, &r.oreficeriaID, &r.oreficeriaNome, &r.oreficeriaTelefono, &r.descrizione, &r.notePubbliche,
		&r.preventivoTesto, &r.prezzoTesto, &r.preventivoCent, &r.prezzoCent, &r.consegnaTesto, &r.consegnaData,
		&r.linkInviato, &r.avvisoPronta, &r.lotto, &r.lottoAperto, &r.conArtigiano, &r.giorniInPiu)
	if errors.Is(err, sql.ErrNoRows) {
		return rigaAvviso{}, ErrBustaNonTrovata
	}
	if err != nil {
		return rigaAvviso{}, err
	}
	r.compraOro = oro == 1
	if r.b2b() && r.oreficeriaNome == nil {
		// Recipient.orefice/1 solleva: la chiave esterna lo impedisce, quindi è un dato rotto.
		return rigaAvviso{}, fmt.Errorf("buste: la busta %d punta a un'oreficeria che non c'è", r.id)
	}
	return r, nil
}

// sceglieTelegram è telegram_chats/2: le chat Telegram iscritte del cliente diretto, con il canale
// acceso; mai dopo una risposta (lì la notifica Telegram parte con la risposta stessa). Fino alla T4.7
// il canale non c'è (TelegramInbound.active?/0 falso): nessuna chat, quindi WhatsApp. La T4.7 scrive
// qui la regola e il ramo Telegram dell'invio.
func sceglieTelegram(_ rigaAvviso, _ ScopoAvviso) []int64 { return nil }

// telegramDisponibile è TelegramSubscriptions.available?/0: spento fino alla T4.7.
const telegramDisponibile = false

// ApriAvviso è CustomerNotice.open/2 per la busta id: compone la bozza e non scrive niente.
func (b *Buste) ApriAvviso(ctx context.Context, id int64, scopo ScopoAvviso) (BozzaAvviso, error) {
	r, err := leggiAvviso(ctx, b.d.Lettura, id)
	if err != nil {
		return BozzaAvviso{}, err
	}
	z := BozzaAvviso{BustaID: r.id, Scopo: scopo, Tipo: TipoAggiornamento, Canale: CanaleWhatsApp, Parte: r.parte(),
		OreficeriaID: r.oreficeriaID, Chiusa: slices.Contains(statiChiusi, r.stato)}
	z.Numero, _ = NumeroVisibile(r.codiceQR, r.numero)
	switch {
	case scopo == ScopoRisposta:
		z.Tipo = TipoRisposta
	case r.statoAvviso() == AvvisoDaFare:
		z.Tipo = TipoPronta
	}
	_ = sceglieTelegram(r, scopo) // T4.7: con delle chat il canale è Telegram e il numero non serve
	d, indirizzo, ok := r.indirizzoWhatsApp()
	if !ok {
		z.Problema = ProblemaNessunRecapito
		return z, nil
	}
	z.Indirizzo, z.Nome = indirizzo, d.Nome
	z.TelegramSpento = d.Parte == ParteCliente && !telegramDisponibile
	famiglia := famigliaPer(r)
	if scopo == ScopoRisposta {
		famiglia = FamClienteAggiornamento
		if r.b2b() {
			famiglia = FamOreficeria
		}
	}
	// D-82: nei testi il numero visibile senza «N.»; il popup («Busta …») resta quello della D-64.
	valori, err := b.valoriSegnaposto(ctx, famiglia, r, d, NumeroNeiTesti(r.codiceQR, r.numero))
	if err != nil {
		return BozzaAvviso{}, err
	}
	if z.Testo, z.Modello, err = b.componiFamiglia(ctx, famiglia, valori); err != nil {
		return BozzaAvviso{}, err
	}
	return z, nil
}

// famigliaPer è family_for/3: l'oreficeria ha sempre il suo testo; poi il preventivo (con importo
// se il preventivo, non il prezzo finale, ha un valore), la compro oro valutata, la pronta, e il
// primo invio o l'aggiornamento secondo link_inviato_il.
func famigliaPer(r rigaAvviso) Famiglia {
	switch {
	case r.b2b():
		return FamOreficeria
	case r.stato == "preventivo":
		if _, ok := importo(r.preventivoTesto, r.preventivoCent); ok {
			return FamPreventivoConImporto
		}
		return FamPreventivoSenzaImporto
	case r.stato == "valutata":
		return FamProntaCompraOro
	case r.stato == "pronta":
		return FamPronta
	case r.linkInviato == nil:
		return FamLinkStato
	}
	return FamClienteAggiornamento
}

// importo è money/2: il testo scritto senza spazi ai bordi, o i centesimi «70,50»; false se niente.
func importo(testo *string, cent *int64) (string, bool) {
	if t := presente(testo); t != "" {
		return t, true
	}
	if cent != nil {
		return fmt.Sprintf("%d,%02d", *cent/100, *cent%100), true
	}
	return "", false
}

// riga è row/2: «Etichetta: valore\n», o niente senza valore.
func riga(etichetta, valore string) string {
	if valore == "" {
		return ""
	}
	return etichetta + ": " + valore + "\n"
}

// primaRiga è first_line/1: la prima riga, senza spazi ai bordi.
func primaRiga(s string) string {
	prima, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(prima)
}

// consegnaAvviso è delivery/1: il testo scritto (senza spazi ai bordi), o la data gg/mm/aaaa.
func consegnaAvviso(r rigaAvviso) string {
	if t := presente(r.consegnaTesto); t != "" {
		return t
	}
	if r.consegnaData != nil {
		if g, err := time.Parse(time.DateOnly, *r.consegnaData); err == nil {
			return g.Format("02/01/2006")
		}
		return *r.consegnaData
	}
	return ""
}

// profiloAvviso è la riga del profilo che i testi usano (OnlineSales.get_shop_profile/0).
type profiloAvviso struct {
	nome, citta, orari, instagram, facebook, sito *string
}

func (b *Buste) leggiProfiloAvviso(ctx context.Context) (profiloAvviso, error) {
	var p profiloAvviso
	var nome string
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT nome, citta, orari, instagram, facebook, sito FROM profilo_negozio WHERE ambito = 'default'`).
		Scan(&nome, &p.citta, &p.orari, &p.instagram, &p.facebook, &p.sito)
	if errors.Is(err, sql.ErrNoRows) {
		return profiloAvviso{}, nil
	}
	p.nome = &nome
	return p, err
}

// negozio è shop_name/1: il nome del profilo senza spazi ai bordi, o «Negozio».
func (p profiloAvviso) negozio() string {
	if n := presente(p.nome); n != "" {
		return n
	}
	return "Negozio"
}

// valoriSegnaposto sono customer_context/2 e template_context/3 per la famiglia: il numero è quello
// dei testi (NumeroNeiTesti, D-64 e D-82), {link} e {telegram_optin} restano i segnaposto fino all'invio.
func (b *Buste) valoriSegnaposto(ctx context.Context, f Famiglia, r rigaAvviso, d Destinatario, numero string) (map[string]string, error) {
	p, err := b.leggiProfiloAvviso(ctx)
	if err != nil {
		return nil, err
	}
	if f == FamOreficeria {
		v := map[string]string{"oref_nome": d.Nome, "nome_cliente": strings.TrimSpace(r.nomeCliente), "numero": numero,
			"lavorazione": riga("Lavorazione", primaRiga(r.descrizione)),
			"stato":       strings.ToUpper(strings.ReplaceAll(r.stato, "_", " ")),
			"consegna":    riga("Consegna prevista", consegnaAvviso(r)), "note_riga": riga("Note", presente(r.notePubbliche)),
			"link": segnapostoLink, "negozio": p.negozio(), "importo": ""}
		if v["oref_nome"] == "" {
			v["oref_nome"] = "Collega"
		}
		if v["nome_cliente"] == "" {
			v["nome_cliente"] = "(cliente)"
		}
		if t, ok := importo(r.prezzoTesto, r.prezzoCent); ok {
			v["importo"] = "Totale: € " + t + "\n"
		} else if t, ok := importo(r.preventivoTesto, r.preventivoCent); ok {
			v["importo"] = "Preventivo: € " + t + "\n"
		}
		return v, nil
	}
	v := map[string]string{"nome": d.Nome, "numero": numero, "link": segnapostoLink, "telegram_optin": segnapostoTelegram,
		"negozio": p.negozio(), "orari": presente(p.orari), "citta": ""}
	if v["nome"] == "" {
		v["nome"] = "Cliente"
	}
	if c := presente(p.citta); c != "" {
		v["citta"] = " 💍 " + c
	}
	var social strings.Builder
	for _, s := range []struct {
		etichetta string
		valore    *string
	}{{"Instagram", p.instagram}, {"Facebook", p.facebook}, {"Blog", p.sito}} {
		if t := presente(s.valore); t != "" {
			social.WriteString(s.etichetta + ": " + t + "\n")
		}
	}
	v["social"] = social.String()
	switch f {
	case FamLinkStato, FamClienteAggiornamento:
		// {stima}: la frase con la riga vuota dopo, o niente (RepairEstimate.message_line/1).
		conConsegna := presente(r.consegnaTesto) != "" || r.consegnaData != nil
		s, err := b.stimaPerIlCliente(ctx, r.compraOro, r.stato, conConsegna, r.conArtigiano, r.giorniInPiu)
		if err != nil {
			return nil, err
		}
		v["stima"] = ""
		if s != nil {
			v["stima"] = s.Frase() + ".\n\n"
		}
	case FamPreventivoConImporto:
		v["preventivo"], _ = importo(r.preventivoTesto, r.preventivoCent)
		v["descrizione"] = primaRiga(r.descrizione)
	}
	return v, nil
}

// stessaControparte è same_counterparty?/2: nessuna oreficeria e la riga 1 sono lo stesso cliente.
func stessaControparte(a, c *int64) bool { return controparteDi(a) == controparteDi(c) }

func controparteDi(p *int64) int64 {
	if p != nil && *p > 1 {
		return *p
	}
	return 0
}

// InviaAvviso è CustomerNotice.deliver/3 con il testo lasciato nel popup: rilegge la busta, rifiuta
// senza scrivere se dal popup è cambiato qualcosa, poi il link (se il testo lo chiede, in una
// transazione sua: una gara persa dopo può lasciare alla busta un link nuovo senza messaggio, come
// nello standalone), poi la riga WhatsApp. base è indirizzo_pubblico.
func (b *Buste) InviaAvviso(ctx context.Context, id int64, dis Disegnata, testo, base string, c audit.Contesto) (AvvisoInviato, error) {
	if strings.TrimSpace(testo) == "" {
		return AvvisoInviato{}, ErrAvvisoVuoto
	}
	// Il tipo viene dal modulo (nello standalone sta nella bozza del processo LiveView): uno dei tre, e
	// «reply» solo per il popup aperto dopo una risposta.
	switch {
	case dis.Tipo == TipoRisposta && dis.Scopo != ScopoRisposta,
		dis.Tipo != TipoRisposta && dis.Tipo != TipoPronta && dis.Tipo != TipoAggiornamento:
		return AvvisoInviato{}, ErrTipoCambiato
	}
	r, err := leggiAvviso(ctx, b.d.Lettura, id)
	if err != nil {
		return AvvisoInviato{}, err
	}
	if !stessaControparte(dis.OreficeriaID, r.oreficeriaID) {
		return AvvisoInviato{}, ErrControparteCambiata
	}
	_, indirizzo, ok := r.indirizzoWhatsApp()
	if !ok {
		return AvvisoInviato{}, ErrRecapitoCambiato
	}
	if dis.Canale != CanaleWhatsApp {
		return AvvisoInviato{}, ErrCanaleCambiato
	}
	if indirizzo != dis.Indirizzo {
		return AvvisoInviato{}, ErrRecapitoCambiato
	}
	stato := r.statoAvviso()
	if dis.Tipo == TipoAggiornamento && stato == AvvisoDaFare {
		return AvvisoInviato{}, ErrTipoCambiato
	}
	if dis.Tipo == TipoPronta {
		if err := rifiutoAvviso(stato); err != nil {
			return AvvisoInviato{}, err
		}
	}
	if testo, err = b.mettiLink(ctx, r, testo, base, c); err != nil {
		return AvvisoInviato{}, err
	}
	// Message.base_changeset/2 (update_change(:body, &trim/1)): la riga e quindi wa.me hanno il testo
	// senza spazi né a capo ai bordi (misurato sul banco, slice 066).
	testo = strings.TrimFunc(testo, unicode.IsSpace)
	m := consegnaAMano{bustaID: r.id, parte: r.parte(), indirizzo: indirizzo, testo: testo}
	if dis.Tipo == TipoPronta {
		err = b.avvisaRitiro(ctx, m, c)
	} else {
		err = b.inTx(ctx, func(tx *sql.Tx, ora string) error {
			_, err := b.registraConsegna(ctx, tx, m, ora, c)
			return err
		})
	}
	if err != nil {
		return AvvisoInviato{}, err
	}
	return AvvisoInviato{URL: IndirizzoWaMe(indirizzo, testo), Tipo: dis.Tipo}, nil
}

// ErrNessunRecapito è {:missing_contact, party} e {:no_channel_address, :whatsapp, party} di
// ReadyNotice.announce/3: la controparte non ha un numero da comporre.
var ErrNessunRecapito = errors.New("buste: nessun numero per WhatsApp")

// ErrModelloRitiro è {:invalid_template, family, warning} di ReadyNotice.announce/3: il testo salvato
// dell'avviso di ritiro non si compone; niente è stato scritto.
type ErrModelloRitiro struct{ ModelloNonValido }

func (e ErrModelloRitiro) Error() string { return "buste: testo dell'avviso di ritiro non valido" }

// AnnunciaRitiro è ReadyNotice.announce(repair, :whatsapp, context) (RIP-H-26, slice 068): l'avviso di
// ritiro di un tocco che non offre un riquadro da correggere, cioè «✅ Valutata» dal pannello del QR. Nello
// stesso ordine dello standalone: la controparte con un numero (ErrNessunRecapito), il testo della sua
// famiglia d'avviso (famigliaPer: quello per le oreficerie su una busta conto terzi; un testo salvato che
// non si compone è ErrModelloRitiro, mai il predefinito), il link della busta al posto di {link} e
// {telegram_optin} (mettiLink: in una transazione sua, e senza indirizzo pubblico
// ErrSenzaIndirizzoPubblico prima di emetterlo), poi lo stesso blocco di «Invia» (avvisaRitiro: già
// avvisata, in un lotto aperto o non più pronta e niente scritto). Il testo si registra senza spazi ai
// bordi; l'esito è l'indirizzo wa.me da offrire, che non si apre da solo.
func (b *Buste) AnnunciaRitiro(ctx context.Context, id int64, base string, c audit.Contesto) (AvvisoInviato, error) {
	r, err := leggiAvviso(ctx, b.d.Lettura, id)
	if err != nil {
		return AvvisoInviato{}, err
	}
	d, indirizzo, ok := r.indirizzoWhatsApp()
	if !ok {
		return AvvisoInviato{}, ErrNessunRecapito
	}
	famiglia := famigliaPer(r)
	valori, err := b.valoriSegnaposto(ctx, famiglia, r, d, NumeroNeiTesti(r.codiceQR, r.numero))
	if err != nil {
		return AvvisoInviato{}, err
	}
	testo, modello, err := b.componiFamiglia(ctx, famiglia, valori)
	switch {
	case err != nil:
		return AvvisoInviato{}, err
	case modello != nil:
		return AvvisoInviato{}, ErrModelloRitiro{*modello}
	}
	if testo, err = b.mettiLink(ctx, r, testo, base, c); err != nil {
		return AvvisoInviato{}, err
	}
	testo = strings.TrimFunc(testo, unicode.IsSpace)
	if err := b.avvisaRitiro(ctx, consegnaAMano{bustaID: r.id, parte: r.parte(), indirizzo: indirizzo, testo: testo}, c); err != nil {
		return AvvisoInviato{}, err
	}
	return AvvisoInviato{URL: IndirizzoWaMe(indirizzo, testo), Tipo: TipoPronta}, nil
}

// rifiutoAvviso è may_tell(:ready, …): da avvisare va; avvisata, in un lotto aperto o non pronta no.
func rifiutoAvviso(s StatoAvvisoPronta) error {
	switch s {
	case AvvisoDaFare:
		return nil
	case AvvisoFatto:
		return ErrGiaAvvisata
	case AvvisoInLottoAperto:
		return ErrInLottoAperto
	}
	return ErrNonPronta
}

// mettiLink è resolve_link/3: {telegram_optin} su una busta dell'oreficeria è rifiutato; con uno dei
// due segnaposto il link della busta (garantisciLink) prende il loro posto; senza, il testo resta
// com'è e niente si scrive. Senza indirizzo pubblico il link non si può scrivere: ErrLinkNonSalvato,
// prima di emetterlo.
func (b *Buste) mettiLink(ctx context.Context, r rigaAvviso, testo, base string, c audit.Contesto) (string, error) {
	switch {
	case strings.Contains(testo, segnapostoTelegram) && r.b2b():
		return "", ErrTelegramSoloCliente
	case !strings.Contains(testo, segnapostoLink) && !strings.Contains(testo, segnapostoTelegram):
		return testo, nil
	case base == "":
		return "", ErrSenzaIndirizzoPubblico
	}
	token, err := b.garantisciLink(ctx, r.id, r.oreficeriaID, c)
	if err != nil {
		return "", err
	}
	link := base + "/r/" + token
	return strings.ReplaceAll(strings.ReplaceAll(testo, segnapostoTelegram, link+"/telegram"), segnapostoLink, link), nil
}

// garantisciLink è Workshop.ensure_public_link/2: in una transazione, se la busta ha cambiato
// controparte rispetto a chi il testo era stato composto ErrControparteCambiata; un link con il
// testo del token, né scaduto né revocato, si riusa senza scrivere; altrimenti un token nuovo con
// l'impronta, la scadenza a 90 giorni e la revoca tolta, e repair.public_link_issued con il motivo
// (legacy_hash_only, expired, revoked, missing). Il vecchio link con codice non si tocca.
func (b *Buste) garantisciLink(ctx context.Context, id int64, oreficeria *int64, c audit.Contesto) (string, error) {
	t, ora := b.adesso()
	var token string
	err := b.inTx(ctx, func(tx *sql.Tx, _ string) error {
		var rl rigaLink
		var oref *int64
		var testo *string
		err := tx.QueryRowContext(ctx, `SELECT b.oreficeria_id, b.token_pubblico, `+colonneLink+` FROM buste b WHERE b.id = ? AND b.cancellata_il IS NULL`, id).
			Scan(&oref, &testo, &rl.testo, &rl.impronta, &rl.scade, &rl.revocato, &rl.codiceRevocato, &rl.codice, &rl.inviato)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrBustaNonTrovata
		}
		if err != nil {
			return err
		}
		if !stessaControparte(oreficeria, oref) {
			return ErrControparteCambiata
		}
		scade, err := time.Parse(db.FormatoIstante, rl.scade)
		if err != nil {
			return err
		}
		motivo := map[StatoLink]string{LinkRevocato: "revoked", LinkScaduto: "expired", LinkSoloImpronta: "legacy_hash_only",
			LinkMancante: "missing"}
		s := rl.stato(scade, t)
		if s == LinkAttivo {
			token = *testo
			return nil
		}
		genera := gettone
		if b.nuovoGettone != nil {
			genera = b.nuovoGettone
		}
		nuovo, impronta := genera()
		if _, err := tx.ExecContext(ctx, `UPDATE buste SET token_pubblico = ?1, hash_token_pubblico = ?2, token_pubblico_scade_il = ?3,
			token_pubblico_revocato_il = NULL, aggiornato_il = ?4 WHERE id = ?5`, nuovo, impronta, t.Add(DurataGettone).Format(db.FormatoIstante), ora, id); err != nil {
			return ErrLinkNonSalvato // l'errore di SQLite non porta il token, ma qui non serve altro
		}
		if err := b.reg.Scrivi(ctx, tx, c, "repair.public_link_issued", "repair", id, "ok", map[string]any{"reason": motivo[s]}); err != nil {
			return err
		}
		token = nuovo
		return nil
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

// inTx fa fai in una transazione di scrittura, con l'istante di adesso.
func (b *Buste) inTx(ctx context.Context, fai func(tx *sql.Tx, ora string) error) error {
	_, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	if err := fai(tx, ora); err != nil {
		return err
	}
	return tx.Commit()
}

// consegnaAMano è un messaggio dato all'operatore da mandare con WhatsApp.
type consegnaAMano struct {
	bustaID   int64
	parte     Parte
	indirizzo string
	testo     string
}

func (m consegnaAMano) destinatario() string {
	if m.parte == ParteOreficeria {
		return "oreficeria"
	}
	return "customer"
}

// registraConsegna è handoff_multi/3: la riga «accepted» (l'operatore ha già il testo: non è un
// recapito né una lettura), il timbro link_inviato_il del primo messaggio al cliente diretto (mai
// all'oreficeria, mai su una busta pronta, consegnata o chiusa, una volta sola, senza toccare
// aggiornato_il come update_all) e outgoing.handed_off con canale, parte e busta, mai il testo.
func (b *Buste) registraConsegna(ctx context.Context, tx *sql.Tx, m consegnaAMano, ora string, c audit.Contesto) (int64, error) {
	res, err := tx.ExecContext(ctx, `INSERT INTO messaggi_in_uscita (busta_id, canale, destinatario, indirizzo, testo, stato, tentativi,
		prossimo_tentativo_il, accodato_il, accettato_il) VALUES (?1, 'whatsapp', ?2, ?3, ?4, 'accepted', 1, ?5, ?5, ?5)`,
		m.bustaID, m.destinatario(), m.indirizzo, m.testo, ora)
	if err != nil {
		return 0, err
	}
	mid, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if m.parte == ParteCliente {
		if _, err := tx.ExecContext(ctx, `UPDATE buste SET link_inviato_il = ?1 WHERE id = ?2 AND link_inviato_il IS NULL
			AND stato NOT IN ('pronta', 'consegnata', 'chiusa') AND cancellata_il IS NULL`, ora, m.bustaID); err != nil {
			return 0, err
		}
	}
	if err := b.reg.Scrivi(ctx, tx, c, "outgoing.handed_off", "outgoing_message", mid, "ok",
		map[string]any{"channel": "whatsapp", "party": m.destinatario(), "repair_id": m.bustaID}); err != nil {
		return 0, err
	}
	return mid, nil
}

// condizioneDaAvvisare è pending_query/1: pronta (o valutata), non avvisata, in nessun lotto o in un
// lotto già inviato. Una sola scrittura decide (claim/3).
const condizioneDaAvvisare = `((compro_oro = 1 AND stato = 'valutata') OR (compro_oro = 0 AND stato = 'pronta'))
	AND avviso_pronta_il IS NULL AND (lotto_avviso_id IS NULL OR lotto_avviso_id IN (SELECT id FROM lotti_avviso WHERE inviato_il IS NOT NULL))`

// avvisaRitiro è ReadyNotice.notify/4 su WhatsApp: prima il blocco (chi perde questa scrittura non
// scrive niente), poi la riga WhatsApp, l'evento «notified» e ready_notice.sent, tutto in una
// transazione.
func (b *Buste) avvisaRitiro(ctx context.Context, m consegnaAMano, c audit.Contesto) error {
	return b.inTx(ctx, func(tx *sql.Tx, ora string) error {
		res, err := tx.ExecContext(ctx, `UPDATE buste SET avviso_pronta_il = ?1, aggiornato_il = ?1 WHERE id = ?2 AND cancellata_il IS NULL AND `+
			condizioneDaAvvisare, ora, m.bustaID)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return rifiutoDelBlocco(ctx, tx, m.bustaID)
		}
		mid, err := b.registraConsegna(ctx, tx, m, ora, c)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO eventi_avviso_pronta (busta_id, evento, canale, messaggio_in_uscita_id, attore,
			avvenuto_il, creato_il, aggiornato_il) VALUES (?1, 'notified', 'whatsapp', ?2, ?3, ?4, ?4, ?4)`, m.bustaID, mid, attoreDi(c), ora); err != nil {
			return err
		}
		return b.reg.Scrivi(ctx, tx, c, "ready_notice.sent", "repair", m.bustaID, "ok",
			map[string]any{"channel": "whatsapp", "party": m.destinatario(), "outgoing_message_id": mid})
	})
}

// rifiutoDelBlocco è claim_refusal/2: perché il blocco non ha preso la busta.
func rifiutoDelBlocco(ctx context.Context, tx *sql.Tx, id int64) error {
	r, err := leggiAvviso(ctx, tx, id)
	switch {
	case err != nil:
		return err
	case r.avvisoPronta != nil:
		return ErrGiaAvvisata
	case r.lotto != nil && r.lottoAperto:
		return ErrInLottoAperto
	}
	return ErrNonPronta
}

// attoreDi è actor/1: chi agisce, o NULL.
func attoreDi(c audit.Contesto) any {
	if c.Attore == "" {
		return nil
	}
	return c.Attore
}

// AzzeraAvviso è ReadyNotice.reset/2 («Azzera l'avviso»): il blocco torna vuoto (niente si cancella),
// l'evento «reset» e ready_notice.reset con lo stato e l'istante annullato, in una transazione. Una
// busta non avvisata è ErrNonAvvisata e niente si scrive.
func (b *Buste) AzzeraAvviso(ctx context.Context, id int64, c audit.Contesto) error {
	return b.inTx(ctx, func(tx *sql.Tx, ora string) error {
		var stato string
		var il *string
		err := tx.QueryRowContext(ctx, `SELECT stato, avviso_pronta_il FROM buste WHERE id = ? AND cancellata_il IS NULL`, id).Scan(&stato, &il)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrBustaNonTrovata
		}
		if err != nil {
			return err
		}
		if il == nil {
			return ErrNonAvvisata
		}
		if _, err := tx.ExecContext(ctx, `UPDATE buste SET avviso_pronta_il = NULL, aggiornato_il = ?1 WHERE id = ?2`, ora, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO eventi_avviso_pronta (busta_id, evento, attore, avvenuto_il, creato_il, aggiornato_il)
			VALUES (?1, 'reset', ?2, ?3, ?3, ?3)`, id, attoreDi(c), ora); err != nil {
			return err
		}
		return b.reg.Scrivi(ctx, tx, c, "ready_notice.reset", "repair", id, "ok", map[string]any{"status": stato, "notified_at": *il})
	})
}

// StatoAvvisoPronta è ReadyNotice.state/1.
type StatoAvvisoPronta int

const (
	AvvisoNonApplicabile StatoAvvisoPronta = iota // non è pronta: niente da avvisare
	AvvisoDaFare                                  // :pending
	AvvisoInLottoAperto                           // {:in_open_lot, lot}
	AvvisoFatto                                   // {:notified, at}
)

// EventoAvviso è una riga della storia dell'avviso di ritiro.
type EventoAvviso struct {
	ID     int64
	Evento string // notified o reset
	Attore string // "" se nessuno
	Il     time.Time
}

// AvvisoDellaScheda è il riquadro #repair-notice della scheda.
type AvvisoDellaScheda struct {
	Stato  StatoAvvisoPronta
	Il     *time.Time // l'istante dell'avviso (AvvisoFatto)
	Lotto  int64      // il lotto aperto (AvvisoInLottoAperto)
	Storia []EventoAvviso
}

// avvisoDellaScheda legge stato e storia dell'avviso di ritiro (ReadyNotice.state/1 e history/1,
// dalla più vecchia).
func (b *Buste) avvisoDellaScheda(ctx context.Context, id int64) (AvvisoDellaScheda, error) {
	r, err := leggiAvviso(ctx, b.d.Lettura, id)
	if err != nil {
		return AvvisoDellaScheda{}, err
	}
	a := AvvisoDellaScheda{Stato: r.statoAvviso()}
	switch a.Stato {
	case AvvisoFatto:
		if a.Il, err = istante(r.avvisoPronta); err != nil {
			return AvvisoDellaScheda{}, err
		}
	case AvvisoInLottoAperto:
		a.Lotto = *r.lotto
	}
	righe, err := b.d.Lettura.QueryContext(ctx, `SELECT id, evento, coalesce(attore, ''), avvenuto_il FROM eventi_avviso_pronta WHERE busta_id = ? ORDER BY id`, id)
	if err != nil {
		return AvvisoDellaScheda{}, err
	}
	defer righe.Close()
	for righe.Next() {
		var e EventoAvviso
		var il string
		if err := righe.Scan(&e.ID, &e.Evento, &e.Attore, &il); err != nil {
			return AvvisoDellaScheda{}, err
		}
		if e.Il, err = time.Parse(db.FormatoIstante, il); err != nil {
			return AvvisoDellaScheda{}, err
		}
		a.Storia = append(a.Storia, e)
	}
	return a, righe.Err()
}

// I rifiuti del link Telegram di un messaggio (TelegramSubscriptions.issue_link/4 con via: :whatsapp).
var (
	ErrTelegramNascosto = errors.New("buste: link Telegram di una busta non del cliente diretto") // :hidden
	ErrTelegramChiusa   = errors.New("buste: busta chiusa, niente avvisi")                        // :closed
	ErrTelegramSpento   = errors.New("buste: notifiche Telegram spente")                          // :unavailable
)

// LinkTelegram è ciò che {telegram_optin} apre (GET /r/<token>/telegram, telegram_link/2), con i
// controlli di issue_link/4 nel suo ordine: il link che non apre niente (ErrBustaNonTrovata), la
// busta di un'oreficeria (ErrTelegramNascosto), la busta chiusa (ErrTelegramChiusa), poi il canale.
// Fino alla T4.7 il canale è spento: ErrTelegramSpento, e niente si scrive. La T4.7 qui emette il
// token del bot e risponde con l'indirizzo t.me.
func (b *Buste) LinkTelegram(ctx context.Context, token string) error {
	t, err := b.bustaDelToken(ctx, token)
	if err != nil {
		return err
	}
	if t.parte() != "customer" {
		return ErrTelegramNascosto
	}
	var stato string
	if err := b.d.Lettura.QueryRowContext(ctx, `SELECT stato FROM buste WHERE id = ?`, t.id).Scan(&stato); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrBustaNonTrovata
		}
		return err
	}
	if slices.Contains(statiChiusi, stato) {
		return ErrTelegramChiusa
	}
	if !telegramDisponibile {
		return ErrTelegramSpento
	}
	return nil
}
