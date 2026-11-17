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

// Le buste di un lotto d'avviso (slice 089, ORE-06, ORE-08, ORE-09), come ReadyLots.fetch_lot/1, members/1,
// candidates/1, candidate_refusal/1, add_bag/3 e remove_bag/3 dello standalone (ready_lots.ex): la lettura del
// lotto con la sua oreficeria, le buste del lotto, le candidate («Pronte di <oreficeria> non ancora in lotto»),
// l'aggiunta per QR, numero o id e il «Rimuovi». Nessun controllo di ruolo (titolare e dipendente). Come
// Repo.prepare_query/3 dello standalone, ogni lettura e scrittura delle buste esclude il cestino.
//
// L'aggiunta ha una sola scrittura che decide (attach_bag/3): l'UPDATE condizionato chiede nello stesso istante
// che la busta sia libera da ogni lotto, ancora da avvisare (condizioneDaAvvisare, la stessa dell'avviso
// singolo: «si può mettere in lotto» e «si può avvisare» sono una domanda sola), di questa oreficeria, non compro
// oro, pronta, e che il lotto sia ancora aperto. Quello che si è letto prima serve solo a trovare la busta e, se
// la scrittura non prende niente, a dire perché (add_refusal/3, nell'ordine dello standalone: lotto non più
// aperto, già in questo lotto, già avvisata, in un altro lotto, altra oreficeria, compro oro, non pronta). Così
// due aggiunte insieme danno un membro solo e una riga d'audit, e una busta non entra in un lotto se l'avviso
// singolo l'ha presa un attimo prima (e viceversa: l'avviso singolo rifiuta una busta in un lotto aperto).
//
// Il numero scritto (D-64) è quello dell'etichetta, come nella consegna all'artigiano (bustaPerNumeroCon): fra
// più buste contano, per il loro stato, quelle che il lotto può prendere (lottoBuono: pronte, non compro oro,
// dell'oreficeria del lotto, fuori dal cestino; come aggiungibile della consegna, che guarda solo lo stato e non dove
// la busta è già), poi l'anno in corso; se ne resta più d'una il rifiuto solo del Go «Più buste corrispondono a
// «X»: …» (RifiutoPiuBuste, come la D-89). Avviso e lotto non filtrano: la busta scelta dice il suo rifiuto
// («già in questo lotto», «già avvisata», …), senza che un'altra con lo stesso numero entri al suo posto. Il QR è
// il codice normalizzato (D-64 della 055d), fra più buste la più recente. L'id si legge com'è (Integer.parse/1).

// LottoAvviso è un lotto con la sua oreficeria (fetch_lot/1). Inviato e Chiuso sono nil finché non lo sono;
// AccessoSpento: il link storico non riattivato all'importazione (public_access_disabled_at).
type LottoAvviso struct {
	ID            int64
	OreficeriaID  int64
	Oreficeria    string
	Telefono      string // il telefono dell'oreficeria com'è scritto ("" se manca)
	Gettone       string
	Aperto        time.Time
	Inviato       *time.Time
	Chiuso        *time.Time
	AccessoSpento bool
}

// Modificabile è ReadyLot.open?/1: né inviato né chiuso.
func (l LottoAvviso) Modificabile() bool { return l.Inviato == nil && l.Chiuso == nil }

// BustaDelLotto è una busta della pagina del lotto: un membro o una candidata. GiaAvvisata è
// candidate_refusal/1 (:already_notified): la candidata non ha «Aggiungi».
type BustaDelLotto struct {
	ID          int64
	Numero      string
	CodiceQR    *string
	Descrizione string
	GiaAvvisata bool
}

// Visibile è il numero da mostrare (D-64): «#0045/26» con Titolo, «N. 45/26» senza etichetta.
func (x BustaDelLotto) Visibile() Mostrato {
	qr := ""
	if x.CodiceQR != nil {
		qr = *x.CodiceQR
	}
	return NumeroMostrato(qr, x.Numero)
}

// RichiestaLotto sono i tre campi dell'aggiunta (lot_param/2: senza spazi ai bordi), letti in quest'ordine: il
// QR, poi il numero, poi l'id. Il primo non vuoto decide la lettura; se non trova niente non si prova il dopo.
type RichiestaLotto struct {
	QR, Numero, ID string
}

// I rifiuti dei lotti, con i testi di ReadyLots.refusal_message/1 e della pagina.
const (
	ErrLottoNonDisponibile  Rifiuto = "Lotto non disponibile."
	ErrLottoRichiestaVuota  Rifiuto = "Numero busta, QR o id richiesto."
	ErrLottoBustaNonTrovata Rifiuto = "Riparazione non trovata."
	ErrGiaInQuestoLotto     Rifiuto = "Busta già in questo lotto."
	ErrLottoGiaAvvisata     Rifiuto = "Busta già avvisata singolarmente: azzera l'avviso dal dettaglio, poi riprova."
	ErrLottoAltraOreficeria Rifiuto = "Busta di altra oreficeria."
	ErrLottoCompraOro       Rifiuto = "Le schede compro oro non entrano in un lotto."
	ErrLottoNonPronta       Rifiuto = "Busta non pronta."
	ErrNonInQuestoLotto     Rifiuto = "Busta non in questo lotto."
	ErrLottoNonTrovato      Rifiuto = "Lotto non trovato."
)

// InAltroLotto è {:in_other_lot, id} (Inviato falso: il lotto è ancora aperto, la busta si può togliere da lì) o
// {:in_sent_lot, id} (il lotto è uscito: la busta resta sua).
type InAltroLotto struct {
	Lotto   int64
	Inviato bool
}

func (e *InAltroLotto) Error() string {
	n := strconv.FormatInt(e.Lotto, 10)
	if e.Inviato {
		return "Busta già uscita col lotto #" + n + ": resta sua. Per riavvisare il cliente usa l'avviso singolo dal dettaglio."
	}
	return "Busta già nel lotto #" + n + ": toglila da lì, poi riprova."
}

// RifiutoPiuBuste è il numero scritto che vale per più buste che possono entrare nel lotto (solo del Go, come
// ErrPiuBuste della consegna, D-89). errors.Is lo riconosce come ErrPiuBuste.
type RifiutoPiuBuste struct{ Codice string }

func (e *RifiutoPiuBuste) Error() string {
	return ErrPiuBuste.Error() + " a «" + e.Codice + "»: scrivi il numero con l'anno o usa il QR."
}

func (e *RifiutoPiuBuste) Unwrap() error { return ErrPiuBuste }

const colonneBustaDelLotto = `b.id, b.numero, b.codice_qr, b.descrizione, b.avviso_pronta_il IS NOT NULL`

func scandisciBustaDelLotto(s interface{ Scan(...any) error }) (BustaDelLotto, error) {
	var x BustaDelLotto
	err := s.Scan(&x.ID, &x.Numero, &x.CodiceQR, &x.Descrizione, &x.GiaAvvisata)
	return x, err
}

// LottoAvviso è fetch_lot/1: il lotto con il nome della sua oreficeria, in qualunque stato; ErrLottoNonTrovato se
// non c'è.
func (b *Buste) LottoAvviso(ctx context.Context, id int64) (LottoAvviso, error) {
	var l LottoAvviso
	var aperto string
	var inviato, chiuso *string
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT l.id, l.oreficeria_id, o.nome, coalesce(o.telefono, ''), l.token, l.creato_il, l.inviato_il, l.chiuso_il,
		l.accesso_pubblico_spento_il IS NOT NULL FROM lotti_avviso l JOIN oreficerie o ON o.id = l.oreficeria_id WHERE l.id = ?`, id).
		Scan(&l.ID, &l.OreficeriaID, &l.Oreficeria, &l.Telefono, &l.Gettone, &aperto, &inviato, &chiuso, &l.AccessoSpento)
	if errors.Is(err, sql.ErrNoRows) {
		return l, ErrLottoNonTrovato
	}
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

// BusteDelLotto è members/1: le buste del lotto fuori dal cestino, dalla più recente.
func (b *Buste) BusteDelLotto(ctx context.Context, lotto int64) ([]BustaDelLotto, error) {
	return b.righeDelLotto(ctx, `SELECT `+colonneBustaDelLotto+` FROM buste b WHERE b.lotto_avviso_id = ? AND b.cancellata_il IS NULL
		ORDER BY b.id DESC`, lotto)
}

// CandidateDelLotto è candidates/1: le buste pronte dell'oreficeria del lotto in nessun lotto, non compro oro, fuori
// dal cestino, comprese quelle già avvisate (segnate con GiaAvvisata), per data di pronta e id decrescenti (una
// data di pronta vuota va in fondo, come NULL in SQLite).
func (b *Buste) CandidateDelLotto(ctx context.Context, l LottoAvviso) ([]BustaDelLotto, error) {
	return b.righeDelLotto(ctx, `SELECT `+colonneBustaDelLotto+` FROM buste b WHERE b.oreficeria_id = ? AND b.lotto_avviso_id IS NULL
		AND b.compro_oro = 0 AND b.stato = 'pronta' AND b.cancellata_il IS NULL ORDER BY b.data_pronta DESC, b.id DESC`, l.OreficeriaID)
}

func (b *Buste) righeDelLotto(ctx context.Context, q string, arg any) ([]BustaDelLotto, error) {
	rr, err := b.d.Lettura.QueryContext(ctx, q, arg)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	var out []BustaDelLotto
	for rr.Next() {
		x, err := scandisciBustaDelLotto(rr)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rr.Err()
}

// condLottoApertoID è l'EXISTS del lotto ancora aperto (ReadyLot.open_query/1), sul segnaposto ?1.
const condLottoApertoID = `EXISTS (SELECT 1 FROM lotti_avviso l WHERE l.id = ?1 AND ` + condLottoAperto + `)`

// condEntraNelLotto è la condizione di attach_bag/3 sulla busta, senza il lotto aperto: ancora da avvisare
// (pending_query/0), in nessun lotto, dell'oreficeria ?2, non compro oro, fuori dal cestino.
const condEntraNelLotto = `cancellata_il IS NULL AND ` + condizioneDaAvvisare + ` AND lotto_avviso_id IS NULL AND oreficeria_id = ?2 AND compro_oro = 0`

// AggiungiAlLotto è add_bag/3: trova la busta (QR, poi numero, poi id) e la collega al lotto con una sola
// scrittura condizionata, con la riga ready_lot.bag_added (repair_id) nella stessa transazione. Rifiuti: il lotto
// che non c'è ErrLottoNonDisponibile; i tre campi vuoti ErrLottoRichiestaVuota; nessuna busta
// ErrLottoBustaNonTrovata; più buste col numero scritto *RifiutoPiuBuste; poi quelli di lottoRifiuto. Dà la busta
// aggiunta.
func (b *Buste) AggiungiAlLotto(ctx context.Context, lotto int64, q RichiestaLotto, c audit.Contesto) (BustaDelLotto, error) {
	q = RichiestaLotto{QR: strings.TrimSpace(q.QR), Numero: strings.TrimSpace(q.Numero), ID: strings.TrimSpace(q.ID)}
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return BustaDelLotto{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var oreficeria int64
	err = tx.QueryRowContext(ctx, `SELECT oreficeria_id FROM lotti_avviso WHERE id = ?`, lotto).Scan(&oreficeria)
	if errors.Is(err, sql.ErrNoRows) {
		return BustaDelLotto{}, ErrLottoNonDisponibile
	}
	if err != nil {
		return BustaDelLotto{}, err
	}
	id, err := b.bustaPerLotto(ctx, tx, oreficeria, q)
	if err != nil {
		return BustaDelLotto{}, err
	}
	if b.primaDelLotto != nil {
		if err := b.primaDelLotto(tx); err != nil {
			return BustaDelLotto{}, err
		}
	}
	_, ora := b.adesso()
	res, err := tx.ExecContext(ctx, `UPDATE buste SET lotto_avviso_id = ?1, aggiornato_il = ?3 WHERE id = ?4 AND `+condEntraNelLotto+
		` AND `+condLottoApertoID, lotto, oreficeria, ora, id)
	if err != nil {
		return BustaDelLotto{}, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return BustaDelLotto{}, err
	} else if n == 0 {
		return BustaDelLotto{}, lottoRifiuto(ctx, tx, lotto, oreficeria, id)
	}
	x, err := scandisciBustaDelLotto(tx.QueryRowContext(ctx, `SELECT `+colonneBustaDelLotto+` FROM buste b WHERE b.id = ?`, id))
	if err != nil {
		return BustaDelLotto{}, err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "ready_lot.bag_added", "ready_lot", lotto, "ok", map[string]any{"repair_id": id}); err != nil {
		return BustaDelLotto{}, err
	}
	return x, tx.Commit()
}

// bustaPerLotto è find_bag/4: l'id della busta fuori dal cestino che la richiesta indica.
func (b *Buste) bustaPerLotto(ctx context.Context, tx *sql.Tx, oreficeria int64, q RichiestaLotto) (int64, error) {
	var id int64
	var err error
	switch {
	case q.QR != "":
		id, err = bustaPerQR(ctx, tx, q.QR)
	case q.Numero != "":
		buona := func(ctx context.Context, x interrogatore, id int64) (bool, error) {
			return lottoBuono(ctx, x, oreficeria, id)
		}
		id, err = b.bustaPerNumeroCon(ctx, tx, q.Numero, buona)
		if errors.Is(err, ErrPiuBuste) {
			return 0, &RifiutoPiuBuste{Codice: q.Numero}
		}
	case q.ID != "":
		n, e := strconv.ParseInt(q.ID, 10, 64)
		if e != nil {
			return 0, ErrLottoBustaNonTrovata
		}
		err = tx.QueryRowContext(ctx, `SELECT id FROM buste WHERE id = ? AND cancellata_il IS NULL`, n).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			id, err = 0, nil
		}
	default:
		return 0, ErrLottoRichiestaVuota
	}
	if err != nil {
		return 0, err
	}
	if id == 0 {
		return 0, ErrLottoBustaNonTrovata
	}
	return id, nil
}

// lottoBuono dice se la busta, per il suo stato, è una che un lotto dell'oreficeria prende: pronta, non compro oro,
// dell'oreficeria, fuori dal cestino. Non guarda l'avviso né il lotto (review della 089): fra la 45/26 già nel lotto e
// una 45/25 pronta, «45» resta la 45/26 («Busta già in questo lotto.»), non aggiunge la 45/25 in silenzio.
func lottoBuono(ctx context.Context, q interrogatore, oreficeria, id int64) (bool, error) {
	rr, err := q.QueryContext(ctx, `SELECT 1 FROM buste WHERE id = ?1 AND cancellata_il IS NULL AND compro_oro = 0 AND stato = 'pronta'
		AND oreficeria_id = ?2`, id, oreficeria)
	if err != nil {
		return false, err
	}
	si := rr.Next()
	if err := rr.Close(); err != nil {
		return false, err
	}
	return si, rr.Err()
}

// lottoRifiuto è add_refusal/3 e bag_refusal/3: perché la scrittura non ha preso la busta, chiesto solo dopo e
// nello stesso ordine dello standalone. «Già avvisata» viene prima dei due rifiuti del lotto: di una busta avvisata
// e in un lotto uscito si può disfare solo l'avviso.
func lottoRifiuto(ctx context.Context, tx *sql.Tx, lotto, oreficeria, id int64) error {
	var avvisata, compraOro bool
	var suo, sua *int64
	err := tx.QueryRowContext(ctx, `SELECT avviso_pronta_il IS NOT NULL, lotto_avviso_id, oreficeria_id, compro_oro
		FROM buste WHERE id = ? AND cancellata_il IS NULL`, id).Scan(&avvisata, &suo, &sua, &compraOro)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLottoBustaNonTrovata // repo.get/2 non vede il cestino: una busta cestinata nel frattempo
	}
	if err != nil {
		return err
	}
	aperto, err := lottoAperto(ctx, tx, lotto)
	switch {
	case err != nil:
		return err
	case !aperto:
		return ErrLottoNonDisponibile
	case suo != nil && *suo == lotto:
		return ErrGiaInQuestoLotto
	case avvisata:
		return ErrLottoGiaAvvisata
	case suo != nil:
		altro, err := lottoAperto(ctx, tx, *suo)
		if err != nil {
			return err
		}
		return &InAltroLotto{Lotto: *suo, Inviato: !altro}
	case sua == nil || *sua != oreficeria:
		return ErrLottoAltraOreficeria
	case compraOro:
		return ErrLottoCompraOro
	}
	return ErrLottoNonPronta
}

// lottoAperto è still_open?/2.
func lottoAperto(ctx context.Context, q interrogatore, lotto int64) (bool, error) {
	rr, err := q.QueryContext(ctx, `SELECT 1 FROM lotti_avviso l WHERE l.id = ? AND `+condLottoAperto, lotto)
	if err != nil {
		return false, err
	}
	si := rr.Next()
	if err := rr.Close(); err != nil {
		return false, err
	}
	return si, rr.Err()
}

// TogliDalLotto è remove_bag/3: scollega la busta (fuori dal cestino) dal lotto ancora aperto con una sola
// scrittura condizionata e la riga ready_lot.bag_removed (repair_id) nella stessa transazione. Rifiuti: il lotto
// che non c'è, inviato o chiuso ErrLottoNonDisponibile (i membri restano); la busta non nel lotto
// ErrNonInQuestoLotto. Dà la busta tolta.
func (b *Buste) TogliDalLotto(ctx context.Context, lotto, busta int64, c audit.Contesto) (BustaDelLotto, error) {
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return BustaDelLotto{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM lotti_avviso WHERE id = ?`, lotto).Scan(&n); err != nil {
		return BustaDelLotto{}, err
	}
	if n == 0 {
		return BustaDelLotto{}, ErrLottoNonDisponibile
	}
	_, ora := b.adesso()
	res, err := tx.ExecContext(ctx, `UPDATE buste SET lotto_avviso_id = NULL, aggiornato_il = ?2 WHERE id = ?3 AND lotto_avviso_id = ?1
		AND cancellata_il IS NULL AND `+condLottoApertoID, lotto, ora, busta)
	if err != nil {
		return BustaDelLotto{}, err
	}
	if k, err := res.RowsAffected(); err != nil {
		return BustaDelLotto{}, err
	} else if k == 0 {
		aperto, err := lottoAperto(ctx, tx, lotto)
		switch {
		case err != nil:
			return BustaDelLotto{}, err
		case aperto:
			return BustaDelLotto{}, ErrNonInQuestoLotto
		}
		return BustaDelLotto{}, ErrLottoNonDisponibile
	}
	x, err := scandisciBustaDelLotto(tx.QueryRowContext(ctx, `SELECT `+colonneBustaDelLotto+` FROM buste b WHERE b.id = ?`, busta))
	if err != nil {
		return BustaDelLotto{}, err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "ready_lot.bag_removed", "ready_lot", lotto, "ok", map[string]any{"repair_id": busta}); err != nil {
		return BustaDelLotto{}, err
	}
	return x, tx.Commit()
}
