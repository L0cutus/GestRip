package buste

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Le consegne all'artigiano (slice 077, ART-05…08, 10, 11), come la parte «Handover» di Workshop dello
// standalone (workshop.ex:204-420, 621-680, 920-940, 1085-1270): pulizia delle consegne vuote dei giorni
// prima, apertura o ripresa della consegna di oggi, menu degli artigiani e consegne in raccolta, aggiunta di
// una busta per numero o QR, «togli» e conferma. Nessun controllo di ruolo (titolare e dipendente).
//
// «Oggi» è il giorno del negozio (clock.Oggi, Europe/Rome): dalla sua mezzanotte (Data.Inizio), come
// start_of_local_day/1. Come Repo.prepare_query/3 dello standalone, ogni lettura o scrittura delle buste
// esclude il cestino; tranne le due domande «la consegna ha delle buste» (la pulizia) e «la consegna ha
// buste uscite» (in raccolta), che contano anche quelle nel cestino, come linked_handover_ids/0 e
// confirmed_handover_ids/0: una busta nel cestino si può ripristinare e deve ritrovare la sua consegna.
//
// Il numero scritto (D-64, Stefano 03/11/2026) è quello dell'etichetta, come nella ricerca della home:
// «0045/26», «045/26», «0045», «45» trovano le buste che mostrano quel numero (RicercaEtichetta.Trova: le
// etichette RIP-AAAA-NNNN, e il numero interno solo per le buste senza etichetta), un altro testo il numero
// interno senza zeri davanti (NormalizzaNumero) delle sole buste senza etichetta. Il QR è il codice
// normalizzato (righeConCodice, D-64 della 055d): fra più buste vince la più recente, come per il QR non unico
// (deciso da Stefano il 29/10/2026). Fra più buste con il numero scritto, la regola di bustaPerNumero.

// ArtigianoDelMenu è una voce del menu della nuova consegna: Aperte sono le sue consegne non chiuse, di
// qualunque giorno («(sessione aperta)»).
type ArtigianoDelMenu struct {
	ID     int64
	Nome   string
	Aperte int
}

// ConsegnaInRaccolta è una riga di «Sessioni aperte oggi»: Buste sono quelle fuori dal cestino.
type ConsegnaInRaccolta struct {
	ID        int64
	Artigiano string
	Buste     int
	Aperta    time.Time
}

// ConsegnaArtigiano è una consegna con il nome del suo artigiano (get_handover/1).
type ConsegnaArtigiano struct {
	ID, ArtigianoID int64
	Artigiano       string
	Aperta          time.Time
	Chiusa          bool
}

// BustaInConsegna è una busta della pagina della consegna. Uscita: già confermata (handed_over_at).
type BustaInConsegna struct {
	ID          int64
	Numero      string
	CodiceQR    *string
	Descrizione string
	Uscita      bool
}

// Visibile è il numero da mostrare (D-64): «#0045/26» con Titolo, «N. 45/26» senza etichetta.
func (c BustaInConsegna) Visibile() Mostrato {
	qr := ""
	if c.CodiceQR != nil {
		qr = *c.CodiceQR
	}
	return NumeroMostrato(qr, c.Numero)
}

// Aggiunta è l'esito di un'aggiunta riuscita: GiaInLista se la busta era già della consegna (la pagina dice
// «↻ … già in lista»; niente cambia, salvo la riapertura di una busta rientrata).
type Aggiunta struct {
	Busta      BustaInConsegna
	GiaInLista bool
}

// Lettura è l'ordine delle due letture del codice: il campo scritto prova prima il numero, la scansione con
// la fotocamera (079) prima il QR. Si passa alla seconda solo se la prima non trova niente.
type Lettura int

const (
	LetturaScritta Lettura = iota
	LetturaScansione
)

// I rifiuti delle consegne, con i testi di handover_error_message/1 e delle pagine.
const (
	ErrConsegnaNonDisponibile Rifiuto = "Consegna non disponibile"
	ErrCodiceVuoto            Rifiuto = "Numero busta o QR richiesto"
	ErrRiparazioneNonTrovata  Rifiuto = "Riparazione non trovata"
	ErrCompraOroAllArtigiano  Rifiuto = "Buste compro oro non possono essere date ad un artigiano"
	ErrGiaFuoriBottega        Rifiuto = "Busta già fuori bottega in altra consegna"
	ErrGiaInRaccolta          Rifiuto = "Busta già in raccolta in altra consegna"
	ErrNonAggiungibile        Rifiuto = "Impossibile aggiungere la busta a questa consegna"
	ErrConsegnaNonTrovata     Rifiuto = "Consegna non trovata."
	// ErrPiuBuste è solo del Go (review della 077): il numero scritto vale per più buste che si possono dare.
	ErrPiuBuste Rifiuto = "Più buste corrispondono"
)

// StatoNonAdatto è {:bad_status, status}: una busta consegnata o annullata.
type StatoNonAdatto struct{ Stato string }

func (e *StatoNonAdatto) Error() string { return "Stato '" + e.Stato + "' non adatto" }

// RifiutoAggiunta è un'aggiunta rifiutata con il codice scritto, come refusal/2 della pagina: «non
// trovata» dice «nessuna busta con numero o QR «X»», il campo vuoto solo il suo testo, gli altri il testo
// e «(«X»)». Motivo è il rifiuto del dominio (errors.Is ed errors.As lo trovano).
type RifiutoAggiunta struct {
	Motivo error
	Codice string
}

func (e *RifiutoAggiunta) Error() string {
	switch {
	case errors.Is(e.Motivo, ErrRiparazioneNonTrovata):
		return e.Motivo.Error() + ": nessuna busta con numero o QR «" + e.Codice + "»"
	case errors.Is(e.Motivo, ErrCodiceVuoto):
		return e.Motivo.Error()
	case errors.Is(e.Motivo, ErrPiuBuste):
		return e.Motivo.Error() + " a «" + e.Codice + "»: scrivi il numero con l'anno o usa il QR."
	}
	return e.Motivo.Error() + " («" + e.Codice + "»)"
}

func (e *RifiutoAggiunta) Unwrap() error { return e.Motivo }

// inizioDiOggi è la mezzanotte di oggi in negozio, nel formato degli istanti salvati.
func (b *Buste) inizioDiOggi() string {
	return clock.Oggi(b.o).Inizio().Format(db.FormatoIstante)
}

// PulisciConsegneVuote è purge_stale_empty_handovers/1: cancella le consegne ancora aperte dei giorni prima
// senza nessuna busta (anche nel cestino), e dice quante. Nessuna riga d'audit, come lo standalone.
func (b *Buste) PulisciConsegneVuote(ctx context.Context) (int64, error) {
	res, err := b.d.Scrittura.ExecContext(ctx, `DELETE FROM consegne_artigiani WHERE chiusa_il IS NULL AND creato_il < ?
		AND NOT EXISTS (SELECT 1 FROM buste b WHERE b.consegna_artigiano_id = consegne_artigiani.id)`, b.inizioDiOggi())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// condInRaccolta è handover_in_collection/2 senza l'artigiano: di oggi, aperta, senza buste uscite (anche
// nel cestino: confirmed_handover_ids/0).
const condInRaccolta = `c.chiusa_il IS NULL AND c.creato_il >= ?
	AND NOT EXISTS (SELECT 1 FROM buste u WHERE u.consegna_artigiano_id = c.id AND u.consegnata_artigiano_il IS NOT NULL)`

// ApriConsegna è open_handover/2: riprende la consegna di oggi dell'artigiano ancora in raccolta (la più
// recente) o ne apre una nuova. L'artigiano che non c'è è ErrArtigianoNonTrovato. Nessuna riga d'audit.
func (b *Buste) ApriConsegna(ctx context.Context, artigiano int64) (int64, error) {
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM artigiani WHERE id = ?`, artigiano).Scan(&n); err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, ErrArtigianoNonTrovato
	}
	var id int64
	err = tx.QueryRowContext(ctx, `SELECT c.id FROM consegne_artigiani c WHERE c.artigiano_id = ? AND `+condInRaccolta+`
		ORDER BY c.id DESC LIMIT 1`, artigiano, b.inizioDiOggi()).Scan(&id)
	switch {
	case err == nil:
		return id, tx.Commit()
	case !errors.Is(err, sql.ErrNoRows):
		return 0, err
	}
	_, ora := b.adesso()
	res, err := tx.ExecContext(ctx, `INSERT INTO consegne_artigiani (artigiano_id, creato_il, aggiornato_il) VALUES (?, ?, ?)`, artigiano, ora, ora)
	if err != nil {
		return 0, err
	}
	if id, err = res.LastInsertId(); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// ArtigianiDelMenu è list_artisans_with_open_handovers/0: l'anagrafica per nome e id, con le consegne non
// chiuse di ognuno, di qualunque giorno.
func (b *Buste) ArtigianiDelMenu(ctx context.Context) ([]ArtigianoDelMenu, error) {
	rr, err := b.d.Lettura.QueryContext(ctx, `SELECT a.id, a.nome,
		(SELECT count(*) FROM consegne_artigiani c WHERE c.artigiano_id = a.id AND c.chiusa_il IS NULL)
		FROM artigiani a ORDER BY a.nome, a.id`)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	var out []ArtigianoDelMenu
	for rr.Next() {
		var a ArtigianoDelMenu
		if err := rr.Scan(&a.ID, &a.Nome, &a.Aperte); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rr.Err()
}

// ConsegneInRaccolta è list_handovers_in_collection/1: le consegne di oggi ancora in raccolta, dalla più
// recente, con il nome dell'artigiano e le buste fuori dal cestino.
func (b *Buste) ConsegneInRaccolta(ctx context.Context) ([]ConsegnaInRaccolta, error) {
	rr, err := b.d.Lettura.QueryContext(ctx, `SELECT c.id, a.nome,
		(SELECT count(*) FROM buste x WHERE x.consegna_artigiano_id = c.id AND x.cancellata_il IS NULL), c.creato_il
		FROM consegne_artigiani c JOIN artigiani a ON a.id = c.artigiano_id
		WHERE `+condInRaccolta+` ORDER BY c.id DESC`, b.inizioDiOggi())
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	var out []ConsegnaInRaccolta
	for rr.Next() {
		var c ConsegnaInRaccolta
		var il string
		if err := rr.Scan(&c.ID, &c.Artigiano, &c.Buste, &il); err != nil {
			return nil, err
		}
		if c.Aperta, err = time.Parse(db.FormatoIstante, il); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rr.Err()
}

// ConsegnaArtigiano è get_handover/1: la consegna con il suo artigiano, aperta o chiusa;
// ErrConsegnaNonTrovata se non c'è.
func (b *Buste) ConsegnaArtigiano(ctx context.Context, id int64) (ConsegnaArtigiano, error) {
	c := ConsegnaArtigiano{ID: id}
	var il string
	var chiusa sql.NullString
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT c.artigiano_id, a.nome, c.creato_il, c.chiusa_il
		FROM consegne_artigiani c JOIN artigiani a ON a.id = c.artigiano_id WHERE c.id = ?`, id).Scan(&c.ArtigianoID, &c.Artigiano, &il, &chiusa)
	if errors.Is(err, sql.ErrNoRows) {
		return ConsegnaArtigiano{}, ErrConsegnaNonTrovata
	}
	if err != nil {
		return ConsegnaArtigiano{}, err
	}
	c.Chiusa = chiusa.Valid
	if c.Aperta, err = time.Parse(db.FormatoIstante, il); err != nil {
		return ConsegnaArtigiano{}, err
	}
	return c, nil
}

// colonneBustaInConsegna sono le colonne di una BustaInConsegna, nell'ordine di scandisciBustaInConsegna.
const colonneBustaInConsegna = `b.id, b.numero, b.codice_qr, b.descrizione, b.consegnata_artigiano_il IS NOT NULL`

func scandisciBustaInConsegna(s interface{ Scan(...any) error }) (BustaInConsegna, error) {
	var x BustaInConsegna
	err := s.Scan(&x.ID, &x.Numero, &x.CodiceQR, &x.Descrizione, &x.Uscita)
	return x, err
}

// BusteDellaConsegna è list_handover_bags/1: le buste della consegna fuori dal cestino, dalla più recente.
func (b *Buste) BusteDellaConsegna(ctx context.Context, id int64) ([]BustaInConsegna, error) {
	rr, err := b.d.Lettura.QueryContext(ctx, `SELECT `+colonneBustaInConsegna+` FROM buste b
		WHERE b.consegna_artigiano_id = ? AND b.cancellata_il IS NULL ORDER BY b.id DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	var out []BustaInConsegna
	for rr.Next() {
		x, err := scandisciBustaInConsegna(rr)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rr.Err()
}

// consegnaAperta è open_handover_row/2: l'artigiano della consegna non chiusa, o ErrConsegnaNonDisponibile.
func consegnaAperta(ctx context.Context, tx *sql.Tx, id int64) (int64, error) {
	var artigiano int64
	err := tx.QueryRowContext(ctx, `SELECT artigiano_id FROM consegne_artigiani WHERE id = ? AND chiusa_il IS NULL`, id).Scan(&artigiano)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrConsegnaNonDisponibile
	}
	return artigiano, err
}

// bustaPerNumero è la lettura del numero scritto (D-64, sopra), 0 se nessuna busta fuori dal cestino lo mostra. Fra
// più candidate (review della 077, Dubbio 1 della ricevuta): contano solo quelle che si possono dare all'artigiano
// (aggiungibile: non compro oro, né consegnate né annullate); con una sola, quella; con più e l'anno non scritto,
// quella dell'anno in corso in negozio (come «45» in «Nuova riparazione», D-64); se ne restano più d'una,
// ErrPiuBuste; se nessuna si può dare, la più recente, che i controlli dopo rifiutano con il suo motivo.
func (b *Buste) bustaPerNumero(ctx context.Context, q interrogatore, scritto string) (int64, error) {
	e, etichetta := LeggiRicercaEtichetta(scritto)
	var rr *sql.Rows
	var err error
	if etichetta {
		rr, err = righeDellEtichetta(ctx, q, e)
	} else {
		rr, err = q.QueryContext(ctx, `SELECT b.id, b.numero, b.codice_qr FROM buste b
			WHERE b.cancellata_il IS NULL AND instr(b.numero, ?) > 0`, NormalizzaNumero(scritto))
	}
	if err != nil {
		return 0, err
	}
	numero := NormalizzaNumero(scritto)
	type candidata struct {
		id   int64
		anno string // l'anno mostrato (D-64): dell'etichetta, o del numero interno
	}
	var cc []candidata
	for rr.Next() {
		var id int64
		var n string
		var qr *string
		if err := rr.Scan(&id, &n, &qr); err != nil {
			_ = rr.Close() // l'errore della lettura è quello che conta
			return 0, err
		}
		if (etichetta && e.Trova(n, qr)) || (!etichetta && !conEtichetta(qr) && NormalizzaNumero(n) == numero) {
			cc = append(cc, candidata{id, annoMostrato(n, qr)})
		}
	}
	if err := rr.Close(); err != nil {
		return 0, err
	}
	if err := rr.Err(); err != nil {
		return 0, err
	}
	var recente int64
	var buone []candidata
	for _, c := range cc {
		recente = max(recente, c.id)
		buona, err := aggiungibile(ctx, q, c.id)
		if err != nil {
			return 0, err
		}
		if buona {
			buone = append(buone, c)
		}
	}
	if len(buone) > 1 && etichetta && e.Anno == "" {
		oggi := fmt.Sprintf("%02d", clock.Oggi(b.o).Anno%100)
		var diQuestAnno []candidata
		for _, c := range buone {
			if c.anno == oggi {
				diQuestAnno = append(diQuestAnno, c)
			}
		}
		if len(diQuestAnno) > 0 {
			buone = diQuestAnno
		}
	}
	switch len(buone) {
	case 0:
		return recente, nil
	case 1:
		return buone[0].id, nil
	}
	return 0, ErrPiuBuste
}

// aggiungibile dice se la busta si può dare all'artigiano per il suo stato: non compro oro, né consegnata né
// annullata (i primi controlli di addable/2).
func aggiungibile(ctx context.Context, q interrogatore, id int64) (bool, error) {
	rr, err := q.QueryContext(ctx, `SELECT 1 FROM buste WHERE id = ? AND compro_oro = 0 AND stato NOT IN ('consegnata', 'annullata')`, id)
	if err != nil {
		return false, err
	}
	si := rr.Next()
	if err := rr.Close(); err != nil {
		return false, err
	}
	return si, rr.Err()
}

// annoMostrato sono le due cifre dell'anno del numero visibile (D-64): dell'etichetta, o del numero interno.
func annoMostrato(numero string, qr *string) string {
	codice := ""
	if qr != nil {
		codice = *qr
	}
	testo, _ := NumeroVisibile(codice, numero)
	_, anno, _ := strings.Cut(testo, "/")
	return anno
}

// conEtichetta dice se il codice è un'etichetta RIP-AAAA-NNNN (EtichettaDi, dopo NormalizzaCodice).
func conEtichetta(qr *string) bool {
	if qr == nil {
		return false
	}
	_, _, ok := EtichettaDi(*qr)
	return ok
}

// bustaPerQR è la lettura del QR: la busta più recente fuori dal cestino con lo stesso codice normalizzato,
// 0 se nessuna.
func bustaPerQR(ctx context.Context, q interrogatore, codice string) (int64, error) {
	rr, err := righeConCodice(ctx, q, codice, 0)
	if err != nil || len(rr) == 0 {
		return 0, err
	}
	return rr[0].ID, nil
}

// statoDellAggiunta è quello che addable/2 e attach_bag/3 leggono della busta.
type statoDellAggiunta struct {
	BustaInConsegna
	compraOro         bool
	stato             string
	consegna          *int64
	uscita, rientrata bool
}

// AggiungiAllaConsegna è add_bag_to_handover/2 con le due letture della pagina (add_code/3): il codice
// (senza spazi ai bordi) si legge nell'ordine dato, e si passa alla seconda lettura solo se la prima non
// trova la busta. Rifiuti in quest'ordine, come *RifiutoAggiunta: consegna chiusa o che non c'è, codice
// vuoto, busta non trovata, compro oro, consegnata o annullata, già fuori bottega in un'altra consegna, in
// raccolta in un'altra consegna, corsa persa. Una busta rientrata da un giro di prima riparte pulita
// (reopen_cycle/3); poi prende la consegna e il suo artigiano. Nessuna riga d'audit, come lo standalone.
func (b *Buste) AggiungiAllaConsegna(ctx context.Context, consegna int64, codice string, l Lettura) (Aggiunta, error) {
	codice = strings.TrimSpace(codice)
	rifiuto := func(err error) (Aggiunta, error) { return Aggiunta{}, &RifiutoAggiunta{Motivo: err, Codice: codice} }
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return Aggiunta{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	artigiano, err := consegnaAperta(ctx, tx, consegna)
	if errors.Is(err, ErrConsegnaNonDisponibile) {
		return rifiuto(err)
	}
	if err != nil {
		return Aggiunta{}, err
	}
	if codice == "" {
		return rifiuto(ErrCodiceVuoto)
	}
	letture := []func(context.Context, interrogatore, string) (int64, error){b.bustaPerNumero, bustaPerQR}
	if l == LetturaScansione {
		letture[0], letture[1] = letture[1], letture[0]
	}
	var id int64
	for _, leggi := range letture {
		if id, err = leggi(ctx, tx, codice); err != nil || id != 0 {
			break
		}
	}
	if errors.Is(err, ErrPiuBuste) {
		return rifiuto(err)
	}
	if err != nil {
		return Aggiunta{}, err
	}
	if id == 0 {
		return rifiuto(ErrRiparazioneNonTrovata)
	}
	var s statoDellAggiunta
	if err := tx.QueryRowContext(ctx, `SELECT `+colonneBustaInConsegna+`, b.compro_oro, b.stato, b.consegna_artigiano_id,
		b.rientrata_artigiano_il IS NOT NULL FROM buste b WHERE b.id = ?`, id).
		Scan(&s.ID, &s.Numero, &s.CodiceQR, &s.Descrizione, &s.uscita, &s.compraOro, &s.stato, &s.consegna, &s.rientrata); err != nil {
		return Aggiunta{}, err
	}
	diQuesta := s.consegna != nil && *s.consegna == consegna
	switch {
	case s.compraOro:
		return rifiuto(ErrCompraOroAllArtigiano)
	case s.stato == "consegnata" || s.stato == "annullata":
		return rifiuto(&StatoNonAdatto{Stato: s.stato})
	case s.uscita && !s.rientrata && !diQuesta:
		return rifiuto(ErrGiaFuoriBottega)
	case s.consegna != nil && !diQuesta && !s.uscita && !s.rientrata:
		return rifiuto(ErrGiaInRaccolta)
	}
	if b.primaDiCollegare != nil {
		if err := b.primaDiCollegare(tx); err != nil {
			return Aggiunta{}, err
		}
	}
	_, ora := b.adesso()
	if s.rientrata {
		// reopen_cycle/3: il giro finito si azzera, la consegna vecchia resta com'è (chiusa e ora vuota).
		if _, err := tx.ExecContext(ctx, `UPDATE buste SET consegnata_artigiano_il = NULL, rientrata_artigiano_il = NULL,
			note_artigiano = NULL, consegna_artigiano_id = NULL, aggiornato_il = ? WHERE id = ? AND cancellata_il IS NULL`, ora, id); err != nil {
			return Aggiunta{}, err
		}
		s.uscita = false
	}
	res, err := tx.ExecContext(ctx, `UPDATE buste SET consegna_artigiano_id = ?1, artigiano_id = ?2, aggiornato_il = ?3
		WHERE id = ?4 AND cancellata_il IS NULL AND (consegna_artigiano_id IS NULL OR consegna_artigiano_id = ?1)`, consegna, artigiano, ora, id)
	if err != nil {
		return Aggiunta{}, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return Aggiunta{}, err
	} else if n == 0 {
		return rifiuto(ErrNonAggiungibile)
	}
	if err := tx.Commit(); err != nil {
		return Aggiunta{}, err
	}
	return Aggiunta{Busta: s.BustaInConsegna, GiaInLista: diQuesta}, nil
}

// TogliDallaConsegna è remove_bag_from_handover/2: scollega la busta della consegna non ancora uscita (fuori
// dal cestino), senza togliere l'artigiano. Ogni altro caso non cambia niente e non è un errore, come lo
// standalone. Nessuna riga d'audit.
func (b *Buste) TogliDallaConsegna(ctx context.Context, consegna, busta int64) error {
	_, ora := b.adesso()
	_, err := b.d.Scrittura.ExecContext(ctx, `UPDATE buste SET consegna_artigiano_id = NULL, aggiornato_il = ?
		WHERE id = ? AND consegna_artigiano_id = ? AND consegnata_artigiano_il IS NULL AND cancellata_il IS NULL`, ora, busta, consegna)
	return err
}

// ConfermaConsegna è confirm_handover/2: timbra l'uscita di ogni busta della consegna non ancora uscita
// (fuori dal cestino) e dice quante; non chiude la consegna e non vuole buste (la pagina rifiuta prima una
// consegna vuota). Nella stessa transazione handover.confirmed con handed_over e repair_ids, gli id timbrati
// in ordine: lo storico dei giri li rilegge. Una consegna chiusa o che non c'è è ErrConsegnaNonDisponibile.
func (b *Buste) ConfermaConsegna(ctx context.Context, consegna int64, c audit.Contesto) (int, error) {
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	if _, err := consegnaAperta(ctx, tx, consegna); err != nil {
		return 0, err
	}
	rr, err := tx.QueryContext(ctx, `SELECT id FROM buste WHERE consegna_artigiano_id = ? AND consegnata_artigiano_il IS NULL
		AND cancellata_il IS NULL ORDER BY id`, consegna)
	if err != nil {
		return 0, err
	}
	ids := []int64{}
	for rr.Next() {
		var id int64
		if err := rr.Scan(&id); err != nil {
			_ = rr.Close() // l'errore della lettura è quello che conta
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rr.Close(); err != nil {
		return 0, err
	}
	if err := rr.Err(); err != nil {
		return 0, err
	}
	_, ora := b.adesso()
	res, err := tx.ExecContext(ctx, `UPDATE buste SET consegnata_artigiano_il = ?1, aggiornato_il = ?1
		WHERE consegna_artigiano_id = ?2 AND consegnata_artigiano_il IS NULL AND cancellata_il IS NULL`, ora, consegna)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "handover.confirmed", "handover", strconv.FormatInt(consegna, 10), "ok",
		map[string]any{"handed_over": n, "repair_ids": ids}); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(n), nil
}
