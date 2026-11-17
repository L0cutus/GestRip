package buste

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
)

// Lo storico dei lotti inviati, il dettaglio e il reinvio (slice 093, ORE-11, ORE-12, ORE-13), come
// ReadyLots.list_sent_lots/0, fetch_sent_lot/1, resend_message/1 e record_resend/4 dello standalone (ready_lots.ex,
// go-baseline). Nessun controllo di ruolo (titolare e dipendente).
//
// Un lotto entra nello storico quando parte e non ne esce: nessuna eliminazione (lo standalone non ha la
// /storico/<id>/elimina del Flask, scelta voluta). Il reinvio è dello standalone (slice 104, il Flask non lo aveva):
// stesso link pubblico, testo nuovo. Prepararlo non scrive niente; la registrazione (il tocco su «Apri WhatsApp» o
// «👥 Scegli contatto») è una riga WhatsApp «accepted» all'oreficeria, senza busta, e ready_lot.resent, in una
// transazione che rilegge il lotto; non tocca gli avvisi delle buste, il gettone né la data di invio. Il «una volta per
// messaggio preparato» è della pagina (web/lotti_storico.go), come nello standalone è del socket.

// I rifiuti dello storico e del reinvio, con i testi di ReadyHistoryLive e ReadyLots.refusal_message/1
// (:lot_completed, :no_members_left). Il lotto non reinviabile è ErrLottoNonDisponibile (:lot_unavailable).
const (
	ErrLottoNonNelloStorico Rifiuto = "Lotto non trovato nello storico."
	ErrLottoCompletato      Rifiuto = "Tutte le buste del lotto sono state ritirate."
	ErrNienteDaReinviare    Rifiuto = "Nessuna busta del lotto è ancora di questa oreficeria: niente da reinviare."
)

// maxStoricoLotti è il limit: 100 di list_sent_lots/0.
const maxStoricoLotti = 100

// LottoStorico è una riga dello storico: Buste sono le buste fuori dal cestino, DaRitirare quelle non consegnate.
type LottoStorico struct {
	LottoAvviso
	Buste      int
	DaRitirare int
}

// ReinvioLotto è resend_message/1: il testo del lotto composto di nuovo con lo stesso link, il numero WhatsApp
// dell'oreficeria ("": il contatto si sceglie sul telefono) e le buste che il link mostra.
type ReinvioLotto struct {
	Lotto     LottoAvviso
	Messaggio string
	Numero    string
	Buste     int
}

// Reinviabile è resendable?/1 della pagina: inviato, non completato e con il link acceso.
func (l LottoAvviso) Reinviabile() bool {
	return l.Inviato != nil && l.Chiuso == nil && !l.AccessoSpento
}

// colonneLotto sono le colonne di LottoAvviso, sul lotto l con la sua oreficeria o.
const colonneLotto = `l.id, l.oreficeria_id, o.nome, coalesce(o.telefono, ''), l.token, l.creato_il, l.inviato_il, l.chiuso_il,
	l.accesso_pubblico_spento_il IS NOT NULL`

// scandisciLotto legge le colonneLotto (più le altre in dopo).
func scandisciLotto(s interface{ Scan(...any) error }, dopo ...any) (LottoAvviso, error) {
	var l LottoAvviso
	var aperto string
	var inviato, chiuso *string
	err := s.Scan(append([]any{&l.ID, &l.OreficeriaID, &l.Oreficeria, &l.Telefono, &l.Gettone, &aperto, &inviato, &chiuso, &l.AccessoSpento}, dopo...)...)
	if err != nil {
		return l, err
	}
	if l.Aperto, err = time.Parse(db.FormatoIstante, aperto); err != nil {
		return l, err
	}
	for _, x := range []struct {
		da *string
		a  **time.Time
	}{{inviato, &l.Inviato}, {chiuso, &l.Chiuso}} {
		if x.da == nil {
			continue
		}
		t, err := time.Parse(db.FormatoIstante, *x.da)
		if err != nil {
			return l, err
		}
		*x.a = &t
	}
	return l, nil
}

// StoricoLotti è list_sent_lots/0: gli ultimi 100 lotti inviati (anche chiusi; mai un aperto), dal più recente per
// id, con l'oreficeria, le buste fuori dal cestino e quelle non consegnate.
func (b *Buste) StoricoLotti(ctx context.Context) ([]LottoStorico, error) {
	rr, err := b.d.Lettura.QueryContext(ctx, `SELECT `+colonneLotto+`,
		(SELECT count(*) FROM buste x WHERE x.lotto_avviso_id = l.id AND x.cancellata_il IS NULL),
		(SELECT count(*) FROM buste x WHERE x.lotto_avviso_id = l.id AND x.cancellata_il IS NULL AND x.stato != 'consegnata')
		FROM lotti_avviso l JOIN oreficerie o ON o.id = l.oreficeria_id
		WHERE l.inviato_il IS NOT NULL ORDER BY l.id DESC LIMIT `+strconv.Itoa(maxStoricoLotti))
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	var out []LottoStorico
	for rr.Next() {
		var x LottoStorico
		if x.LottoAvviso, err = scandisciLotto(rr, &x.Buste, &x.DaRitirare); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rr.Err()
}

// LottoDelloStorico è fetch_sent_lot/1: il lotto inviato (anche chiuso o con il link spento all'importazione);
// ErrLottoNonNelloStorico per un lotto aperto o che non c'è.
func (b *Buste) LottoDelloStorico(ctx context.Context, id int64) (LottoAvviso, error) {
	l, err := scandisciLotto(b.d.Lettura.QueryRowContext(ctx, `SELECT `+colonneLotto+`
		FROM lotti_avviso l JOIN oreficerie o ON o.id = l.oreficeria_id WHERE l.id = ? AND l.inviato_il IS NOT NULL`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return LottoAvviso{}, ErrLottoNonNelloStorico
	}
	return l, err
}

// lottoReinviabile è resendable_lot/2: il lotto inviato, non chiuso e con il link acceso; ErrLottoNonDisponibile
// altrimenti (anche per un id non positivo, Id.positive/1).
func lottoReinviabile(ctx context.Context, q interroga, id int64) (LottoAvviso, error) {
	if id <= 0 {
		return LottoAvviso{}, ErrLottoNonDisponibile
	}
	l, err := scandisciLotto(q.QueryRowContext(ctx, `SELECT `+colonneLotto+`
		FROM lotti_avviso l JOIN oreficerie o ON o.id = l.oreficeria_id
		WHERE l.id = ? AND l.inviato_il IS NOT NULL AND l.chiuso_il IS NULL AND l.accesso_pubblico_spento_il IS NULL`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return LottoAvviso{}, ErrLottoNonDisponibile
	}
	return l, err
}

// busteDaReinviare è resend_members/1: se nessuna busta del lotto fuori dal cestino è ancora da consegnare (comprese
// quelle passate a un'altra oreficeria, la prova di close_if_complete/1; anche nessuna busta) ErrLottoCompletato; poi
// le buste che il link mostra (public_members/1: dell'oreficeria del lotto, fuori dal cestino), nessuna
// ErrNienteDaReinviare. Dà quante.
func busteDaReinviare(ctx context.Context, q interroga, l LottoAvviso) (int, error) {
	var daRitirare, pubbliche int
	err := q.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM buste WHERE lotto_avviso_id = ?1 AND cancellata_il IS NULL AND stato != 'consegnata'),
		(SELECT count(*) FROM buste WHERE lotto_avviso_id = ?1 AND oreficeria_id = ?2 AND cancellata_il IS NULL)`, l.ID, l.OreficeriaID).
		Scan(&daRitirare, &pubbliche)
	switch {
	case err != nil:
		return 0, err
	case daRitirare == 0:
		return 0, ErrLottoCompletato
	case pubbliche == 0:
		return 0, ErrNienteDaReinviare
	}
	return pubbliche, nil
}

// testoDelLotto è batch_message/2: il testo wa_oreficeria_lotto del lotto con n buste e il link indirizzo pubblico +
// /lotto/<gettone>. Senza indirizzo pubblico ({link} come segnaposto) un testo con il link è ErrSenzaIndirizzoPubblico
// (D-46 estesa), un testo non valido ErrModelloLotto (nessun ripiego sul predefinito). Dà anche il link e se è il
// solo segnaposto.
func (b *Buste) testoDelLotto(ctx context.Context, l LottoAvviso, n int, base string) (testo, link string, segnaposto bool, err error) {
	link, segnaposto = base+"/lotto/"+l.Gettone, base == ""
	valoreLink := link
	if segnaposto {
		valoreLink = segnapostoLink
	}
	p, err := b.leggiProfiloAvviso(ctx)
	if err != nil {
		return "", "", false, err
	}
	testo, modello, err := b.componiFamiglia(ctx, FamOreficeriaLotto, map[string]string{"oref_nome": l.Oreficeria,
		"n_buste": strconv.Itoa(n), "link": valoreLink, "negozio": p.negozio()})
	switch {
	case err != nil:
		return "", "", false, err
	case modello != nil:
		return "", "", false, ErrModelloLotto{*modello}
	case segnaposto && strings.Contains(testo, segnapostoLink):
		return "", "", false, ErrSenzaIndirizzoPubblico
	}
	return testo, link, segnaposto, nil
}

// PreparaReinvio è resend_message/1 («Prepara di nuovo il messaggio»): base è indirizzo_pubblico. Non scrive niente.
// Rifiuti, in quest'ordine: ErrLottoNonDisponibile, ErrLottoCompletato, ErrNienteDaReinviare, ErrModelloLotto,
// ErrSenzaIndirizzoPubblico.
func (b *Buste) PreparaReinvio(ctx context.Context, id int64, base string) (ReinvioLotto, error) {
	l, err := lottoReinviabile(ctx, b.d.Lettura, id)
	if err != nil {
		return ReinvioLotto{}, err
	}
	n, err := busteDaReinviare(ctx, b.d.Lettura, l)
	if err != nil {
		return ReinvioLotto{}, err
	}
	testo, _, _, err := b.testoDelLotto(ctx, l, n, base)
	if err != nil {
		return ReinvioLotto{}, err
	}
	return ReinvioLotto{Lotto: l, Messaggio: testo, Numero: NumeroWhatsApp(l.Telefono), Buste: n}, nil
}

// RegistraReinvio è record_resend/4 (il tocco): in una transazione di scrittura rilegge il lotto (gli stessi rifiuti
// di PreparaReinvio prima del testo), scrive la riga WhatsApp «accepted» all'oreficeria senza busta con il testo dato
// (quello preparato) e il suo numero, o «contatto scelto» con scelto (il tocco su «👥 Scegli contatto») o senza un
// numero, outgoing.handed_off e ready_lot.resent con le buste e la riga. Dà l'id della riga.
func (b *Buste) RegistraReinvio(ctx context.Context, id int64, testo string, scelto bool, c audit.Contesto) (int64, error) {
	var mid int64
	err := b.inTx(ctx, func(tx *sql.Tx, ora string) error {
		l, err := lottoReinviabile(ctx, tx, id)
		if err != nil {
			return err
		}
		n, err := busteDaReinviare(ctx, tx, l)
		if err != nil {
			return err
		}
		indirizzo := NumeroWhatsApp(l.Telefono)
		if scelto || indirizzo == "" {
			indirizzo = indirizzoContattoScelto
		}
		if mid, err = b.registraConsegna(ctx, tx, consegnaAMano{parte: ParteOreficeria, indirizzo: indirizzo, testo: testo}, ora, c); err != nil {
			return err
		}
		return b.reg.Scrivi(ctx, tx, c, "ready_lot.resent", "ready_lot", l.ID, "ok", map[string]any{"bags": n, "message_id": mid})
	})
	if err != nil {
		return 0, err
	}
	return mid, nil
}
