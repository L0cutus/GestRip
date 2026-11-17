package buste

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
)

// «📤 Chiudi e invia» del lotto d'avviso (slice 091, ORE-10), come ReadyLots.send_lot/2 dello standalone
// (ready_lots.ex, go-baseline: mark_sent/2, send_refusal/2, open_lot_refusal/2, members/2, announce_members/3,
// batch_message/2, sent_batch/1) e ReadyNotice.announce/3 con link: e chosen_contact: true. Nessun controllo di
// ruolo (titolare e dipendente).
//
// Tutto in **una** transazione di scrittura (BEGIN IMMEDIATE, come la D-94): prima la scrittura condizionata che
// segna il lotto inviato (ancora aperto, con almeno una busta fuori dal cestino, e nessuna busta, nemmeno nel
// cestino, di un'altra oreficeria o di nessuna); poi il testo del lotto (wa_oreficeria_lotto); poi ogni busta fuori
// dal cestino, dalla più recente, con la stessa porta dell'avviso singolo (avvisaRitiroIn: il blocco
// condizionaDaAvvisare, la riga WhatsApp all'oreficeria, l'evento «notified», ready_notice.sent); infine
// ready_lot.sent con il numero di buste. Il lotto si segna **prima** delle buste perché l'avviso singolo rifiuta una
// busta di un lotto aperto. Un rifiuto o un errore qualunque, anche a metà, annulla tutto: niente è partito.
//
// Il testo di ogni busta è quello per le oreficerie («Avviso oreficeria», la famiglia d'avviso di una busta conto
// terzi) con il link del lotto al posto di {link}: nessun link della busta si emette (slice 104 dello standalone).
// Un'oreficeria senza un numero da comporre non è un rifiuto (slice 196): le righe delle buste dicono
// «contatto-scelto» e il numero dell'esito è vuoto. Il link è indirizzo_pubblico + /lotto/<gettone>, mai quello della
// richiesta; senza indirizzo_pubblico un testo con {link} non parte (D-46 estesa, ErrSenzaIndirizzoPubblico), e senza
// {link} il link dell'esito è il solo percorso. Il server non manda niente: il testo lo manda l'operatore. Niente
// Telegram al bot centrale né link al portale /m/ (esclusi nello standalone).

// I rifiuti dell'invio, con i testi di ReadyLots.refusal_message/1 (:empty_lot, :already_sent).
const (
	ErrLottoVuoto      Rifiuto = "Lotto vuoto: aggiungi almeno una busta."
	ErrLottoGiaInviato Rifiuto = "Lotto già inviato."
)

// I motivi di RifiutoMembro, con i testi di ReadyLots.member_refusal_message/1.
const (
	motivoAltraOreficeria = "non è più di questa oreficeria"
	motivoNonPronta       = "non è più pronta"
	motivoGiaAvvisata     = "è già stata avvisata"
	motivoInLottoAperto   = "è in un altro lotto ancora aperto"
	motivoNonEsiste       = "non esiste più"
)

// indirizzoContattoScelto è Outgoing.chosen_contact/0: l'indirizzo salvato di una riga WhatsApp il cui contatto
// l'operatore sceglie sul telefono (wa.me/?text=).
const indirizzoContattoScelto = "contatto-scelto"

// RifiutoMembro è {:member_refused, repair_id, reason}: la busta Busta non si può avvisare, e niente è partito.
// D-99 (Stefano, 17/11/2026): il testo nomina il numero visibile della busta (D-64, «Busta #0045/26: …», Numero), non
// l'id interno che scrive lo standalone («Busta ##{repair_id}: …»).
type RifiutoMembro struct {
	Busta  int64
	Numero string // il numero visibile (Mostrato.Titolo: «#0045/26», «N. 45/26»)
	Motivo string // member_refusal_message/1
}

func (e *RifiutoMembro) Error() string {
	return "Busta " + e.Numero + ": " + e.Motivo + ". Niente è partito."
}

// rifiutoMembro è il *RifiutoMembro della busta, con il suo numero visibile letto anche dal cestino (D-99).
func rifiutoMembro(ctx context.Context, tx *sql.Tx, busta int64, motivo string) error {
	var numero, qr string
	err := tx.QueryRowContext(ctx, `SELECT numero, coalesce(codice_qr, '') FROM buste WHERE id = ?`, busta).Scan(&numero, &qr)
	if err != nil {
		return err
	}
	return &RifiutoMembro{Busta: busta, Numero: NumeroMostrato(qr, numero).Titolo(), Motivo: motivo}
}

// ErrModelloLotto è {:invalid_template, family, warning} dell'invio: il testo salvato del lotto o quello per le
// oreficerie delle buste non si compone (nessun ripiego sul predefinito); niente è partito.
type ErrModelloLotto struct{ ModelloNonValido }

func (e ErrModelloLotto) Error() string { return "buste: testo dell'invio del lotto non valido" }

// LottoInviato è l'esito dell'invio (sent_batch/1).
type LottoInviato struct {
	Lotto     LottoAvviso // il lotto com'è dopo l'invio
	Buste     int         // le buste avvisate
	Messaggio string      // il testo del lotto (wa_oreficeria_lotto)
	Numero    string      // il numero WhatsApp dell'oreficeria (WhatsApp.number/1); "": il contatto si sceglie sul telefono
	Link      string      // il link pubblico del lotto
}

// InviaLotto è send_lot/2: base è indirizzo_pubblico. Rifiuti: ErrLottoNonDisponibile (il lotto non c'è),
// ErrLottoGiaInviato (inviato o chiuso), ErrLottoVuoto, *RifiutoMembro (una busta di un'altra oreficeria, non più
// pronta, già avvisata), ErrModelloLotto, ErrSenzaIndirizzoPubblico. Niente si scrive se non riesce tutto.
func (b *Buste) InviaLotto(ctx context.Context, id int64, base string, c audit.Contesto) (LottoInviato, error) {
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return LottoInviato{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var l LottoAvviso
	var aperto string
	err = tx.QueryRowContext(ctx, `SELECT l.id, l.oreficeria_id, o.nome, coalesce(o.telefono, ''), l.token, l.creato_il
		FROM lotti_avviso l JOIN oreficerie o ON o.id = l.oreficeria_id WHERE l.id = ?`, id).
		Scan(&l.ID, &l.OreficeriaID, &l.Oreficeria, &l.Telefono, &l.Gettone, &aperto)
	if errors.Is(err, sql.ErrNoRows) {
		return LottoInviato{}, ErrLottoNonDisponibile
	}
	if err != nil {
		return LottoInviato{}, err
	}
	if l.Aperto, err = time.Parse(db.FormatoIstante, aperto); err != nil {
		return LottoInviato{}, err
	}
	adesso, ora := b.adesso()
	// mark_sent/2: aperto, con una busta visibile, e solo buste di questa oreficeria (anche nel cestino: una busta
	// ripristinata non deve ricomparire nel lotto già uscito di un'altra; IS NULL perché un != con NULL non è vero).
	res, err := tx.ExecContext(ctx, `UPDATE lotti_avviso SET inviato_il = ?1, accesso_pubblico_spento_il = NULL, aggiornato_il = ?1
		WHERE id = ?2 AND inviato_il IS NULL AND chiuso_il IS NULL
		AND EXISTS (SELECT 1 FROM buste x WHERE x.lotto_avviso_id = ?2 AND x.cancellata_il IS NULL)
		AND NOT EXISTS (SELECT 1 FROM buste x WHERE x.lotto_avviso_id = ?2 AND (x.oreficeria_id IS NULL OR x.oreficeria_id != ?3))`,
		ora, l.ID, l.OreficeriaID)
	if err != nil {
		return LottoInviato{}, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return LottoInviato{}, err
	} else if n == 0 {
		return LottoInviato{}, rifiutoInvio(ctx, tx, l.ID, l.OreficeriaID)
	}
	l.Inviato = &adesso
	membri, err := idMembri(ctx, tx, l.ID)
	if err != nil {
		return LottoInviato{}, err
	}
	// batch_message/2: il testo del lotto prima delle buste, così un testo del lotto non valido vince.
	link, segnaposto := base+"/lotto/"+l.Gettone, base == ""
	valoreLink := link
	if segnaposto {
		valoreLink = segnapostoLink
	}
	p, err := b.leggiProfiloAvviso(ctx)
	if err != nil {
		return LottoInviato{}, err
	}
	testo, modello, err := b.componiFamiglia(ctx, FamOreficeriaLotto, map[string]string{"oref_nome": l.Oreficeria,
		"n_buste": strconv.Itoa(len(membri)), "link": valoreLink, "negozio": p.negozio()})
	switch {
	case err != nil:
		return LottoInviato{}, err
	case modello != nil:
		return LottoInviato{}, ErrModelloLotto{*modello}
	case segnaposto && strings.Contains(testo, segnapostoLink):
		return LottoInviato{}, ErrSenzaIndirizzoPubblico
	}
	// announce_members/3: una porta, una volta per busta.
	for _, busta := range membri {
		if err := b.avvisaNelLotto(ctx, tx, busta, link, segnaposto, ora, c); err != nil {
			return LottoInviato{}, err
		}
		if b.dopoAvvisoNelLotto != nil {
			if err := b.dopoAvvisoNelLotto(tx, busta); err != nil {
				return LottoInviato{}, err
			}
		}
	}
	if err := b.reg.Scrivi(ctx, tx, c, "ready_lot.sent", "ready_lot", l.ID, "ok", map[string]any{"bags": len(membri)}); err != nil {
		return LottoInviato{}, err
	}
	if err := tx.Commit(); err != nil {
		return LottoInviato{}, err
	}
	return LottoInviato{Lotto: l, Buste: len(membri), Messaggio: testo, Numero: NumeroWhatsApp(l.Telefono), Link: link}, nil
}

// idMembri è members/2: le buste del lotto fuori dal cestino, dalla più recente.
func idMembri(ctx context.Context, tx *sql.Tx, lotto int64) ([]int64, error) {
	rr, err := tx.QueryContext(ctx, `SELECT id FROM buste WHERE lotto_avviso_id = ? AND cancellata_il IS NULL ORDER BY id DESC`, lotto)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	var out []int64
	for rr.Next() {
		var id int64
		if err := rr.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rr.Err()
}

// rifiutoInvio è send_refusal/2 e open_lot_refusal/2: perché la scrittura non ha preso il lotto, chiesto solo dopo.
// Un lotto chiuso è per forza inviato: tutti e due «già inviato». Un lotto aperto ha una busta di un'altra oreficeria
// (la più recente, cestino compreso) o nessuna busta visibile.
func rifiutoInvio(ctx context.Context, tx *sql.Tx, lotto, oreficeria int64) error {
	aperto, err := lottoAperto(ctx, tx, lotto)
	switch {
	case err != nil:
		return err
	case !aperto:
		return ErrLottoGiaInviato
	}
	var altra int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM buste WHERE lotto_avviso_id = ? AND (oreficeria_id IS NULL OR oreficeria_id != ?)
		ORDER BY id DESC LIMIT 1`, lotto, oreficeria).Scan(&altra)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrLottoVuoto
	case err != nil:
		return err
	}
	return rifiutoMembro(ctx, tx, altra, motivoAltraOreficeria)
}

// avvisaNelLotto è ReadyNotice.announce(member, :whatsapp, context, link: link, chosen_contact: true) nella
// transazione dell'invio: la busta letta nella transazione, la sua famiglia d'avviso (quella per le oreficerie), il
// link del lotto al posto di {link} (nessun link della busta), il numero dell'oreficeria o il contatto scelto, poi il
// blocco dell'avviso singolo. Un rifiuto del blocco è un *RifiutoMembro.
func (b *Buste) avvisaNelLotto(ctx context.Context, tx *sql.Tx, busta int64, link string, segnaposto bool, ora string, c audit.Contesto) error {
	r, err := leggiAvviso(ctx, tx, busta)
	if errors.Is(err, ErrBustaNonTrovata) {
		return rifiutoMembro(ctx, tx, busta, motivoNonEsiste)
	}
	if err != nil {
		return err
	}
	d, indirizzo, ok := r.indirizzoWhatsApp()
	if !ok {
		indirizzo = indirizzoContattoScelto
	}
	famiglia := famigliaPer(r)
	valori, err := b.valoriSegnaposto(ctx, famiglia, r, d, NumeroNeiTesti(r.codiceQR, r.numero))
	if err != nil {
		return err
	}
	testo, modello, err := b.componiFamiglia(ctx, famiglia, valori)
	switch {
	case err != nil:
		return err
	case modello != nil:
		return ErrModelloLotto{*modello}
	case segnaposto && strings.Contains(testo, segnapostoLink):
		return ErrSenzaIndirizzoPubblico
	}
	testo = strings.TrimFunc(strings.ReplaceAll(testo, segnapostoLink, link), unicode.IsSpace)
	if b.primaDelBloccoNelLotto != nil {
		if err := b.primaDelBloccoNelLotto(tx, busta); err != nil {
			return err
		}
	}
	err = b.avvisaRitiroIn(ctx, tx, consegnaAMano{bustaID: r.id, parte: r.parte(), indirizzo: indirizzo, testo: testo}, ora, c)
	switch {
	case errors.Is(err, ErrGiaAvvisata):
		return rifiutoMembro(ctx, tx, busta, motivoGiaAvvisata)
	case errors.Is(err, ErrInLottoAperto):
		return rifiutoMembro(ctx, tx, busta, motivoInLottoAperto)
	case errors.Is(err, ErrNonPronta):
		return rifiutoMembro(ctx, tx, busta, motivoNonPronta)
	case errors.Is(err, ErrBustaNonTrovata):
		return rifiutoMembro(ctx, tx, busta, motivoNonEsiste)
	}
	return err
}
