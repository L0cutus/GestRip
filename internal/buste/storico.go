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
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Lo storico delle consegne all'artigiano (slice 082, ART-15…18), come HandoverHistory.build/1 e session/1 e
// Workshop.delete_handover_from_history/2 e detach_bag_from_history/3 dello standalone (302439d):
//   - l'elenco di tutte le consegne (aperte e chiuse) dalla più recente, con i filtri artigiano, stato e data
//     d'apertura (giorni del negozio, estremi compresi), 30 per pagina; buste e «ancora fuori» senza il cestino;
//   - il dettaglio: le buste della consegna fuori dal cestino per numero come testo, e le «riaperte», lette
//     dalla tabella dei giri (081) e non dall'audit: i giri chiusi della consegna, non sganciati, di buste fuori
//     dal cestino che ora sono in un'altra consegna o in nessuna, dal rientro più recente;
//   - «Elimina dallo storico» (solo una consegna chiusa): sgancia le buste, anche nel cestino, e i solleciti
//     (solleciti_artigiani.consegna_artigiano_id è ON DELETE RESTRICT), poi cancella la consegna e scrive
//     handover.deleted. I giri non cambiano: la consegna vi resta come numero, come nell'audit;
//   - «Sgancia» (solo una consegna chiusa): la busta fuori dal cestino perde la consegna, handover.bag_detached
//     e i giri chiusi della coppia diventano sganciati (la ricostruzione della 081 legge la stessa riga).
// Il ruolo (solo titolare) è della rotta, come :require_owner_user. Nessuna delle due scritture tocca
// aggiornato_il, come update_all/3.

// RighePerPaginaStorico è @page_size di HandoverHistory.
const RighePerPaginaStorico = 30

// I rifiuti dello storico, con i messaggi di HandoverHistoryController.
const (
	ErrSoloChiuseElimina  Rifiuto = "Solo le consegne chiuse possono essere eliminate dallo storico."
	ErrSoloChiuseSgancia  Rifiuto = "Solo le consegne chiuse permettono questa rimozione."
	ErrBustaNonInConsegna Rifiuto = "Busta non trovata in questa consegna."
)

// ChiaviRegistroStorico sono le chiavi che la riga handover.history_viewed può nominare
// (record_handover_history_view/2): mai i valori.
var ChiaviRegistroStorico = []string{"artigiano", "stato", "data_da", "data_a", "p"}

// FiltriStorico sono i filtri dell'elenco come normalize/1: Artigiano 0 nessuno, Stato "" tutti, le date
// come Date.to_iso8601/1 ("" nessuna), Pagina almeno 1.
type FiltriStorico struct {
	Artigiano     int64
	Stato         string
	DataDa, DataA string
	Pagina        int64
}

// LeggiFiltriStorico legge i filtri da valore(nome) (il parametro come testo di Plug, false se manca o non è
// un testo): lo stato solo «aperte» o «chiuse» com'è scritto; l'artigiano e la pagina con Id.positive/1; le
// date con Date.from_iso8601/1 dopo String.trim/1.
func LeggiFiltriStorico(valore func(nome string) (string, bool)) FiltriStorico {
	f := FiltriStorico{Pagina: 1}
	if v, ok := valore("stato"); ok && (v == "aperte" || v == "chiuse") {
		f.Stato = v
	}
	if v, ok := valore("artigiano"); ok {
		if n, ok := idPositivo(v); ok {
			f.Artigiano = n
		}
	}
	data := func(nome string) string {
		v, _ := valore(nome)
		d, _ := DataISO(strings.TrimFunc(v, unicode.IsSpace))
		return d
	}
	f.DataDa, f.DataA = data("data_da"), data("data_a")
	if v, ok := valore("p"); ok {
		if n, ok := idPositivo(v); ok {
			f.Pagina = n
		}
	}
	return f
}

// RigaStorico è una consegna dell'elenco: Buste quelle fuori dal cestino, Fuori quelle ancora dall'artigiano.
type RigaStorico struct {
	ID, ArtigianoID int64
	Artigiano       string
	Aperta          time.Time
	Chiusa          *time.Time
	Buste, Fuori    int
}

// Storico è una pagina dell'elenco: i filtri con la pagina riportata nei limiti.
type Storico struct {
	Righe          []RigaStorico
	Filtri         FiltriStorico
	Pagine, Totale int64
}

// mezzanotteDi è start_of_local_day/1 della data AAAA-MM-GG (anche con il segno) più giorni, come istante
// salvato; tutte = il confine sta prima (o dopo) di ogni istante a quattro cifre: anno sotto lo zero o oltre
// il 9999, dove il confronto dei testi non vale.
func mezzanotteDi(data string, giorni int) (confine string, sotto, sopra bool) {
	segno := 1
	if strings.HasPrefix(data, "-") {
		segno, data = -1, data[1:]
	}
	a, _ := strconv.Atoi(data[0:4])
	m, _ := strconv.Atoi(data[5:7])
	g, _ := strconv.Atoi(data[8:10])
	t := clock.Data{Anno: segno * a, Mese: time.Month(m), Giorno: g + giorni}.Inizio()
	switch {
	case t.Year() < 0:
		return "", true, false
	case t.Year() > 9999:
		return "", false, true
	}
	return t.Format(db.FormatoIstante), false, false
}

// condizioniStorico è apply_filters/2 come WHERE sulla consegna c.
func condizioniStorico(f FiltriStorico) (string, []any) {
	w, a := []string{"1"}, []any{}
	switch f.Stato {
	case "aperte":
		w = append(w, "c.chiusa_il IS NULL")
	case "chiuse":
		w = append(w, "c.chiusa_il IS NOT NULL")
	}
	if f.Artigiano != 0 {
		w, a = append(w, "c.artigiano_id = ?"), append(a, f.Artigiano)
	}
	if f.DataDa != "" {
		switch confine, sotto, sopra := mezzanotteDi(f.DataDa, 0); {
		case sopra:
			w = append(w, "0")
		case !sotto:
			w, a = append(w, "c.creato_il >= ?"), append(a, confine)
		}
	}
	if f.DataA != "" {
		switch confine, sotto, sopra := mezzanotteDi(f.DataA, 1); {
		case sotto:
			w = append(w, "0")
		case !sopra:
			w, a = append(w, "c.creato_il < ?"), append(a, confine)
		}
	}
	return strings.Join(w, " AND "), a
}

// StoricoConsegne è HandoverHistory.build/1: una pagina delle consegne filtrate, dalla più recente (a pari
// apertura l'id più alto); una pagina oltre la fine è l'ultima.
func (b *Buste) StoricoConsegne(ctx context.Context, f FiltriStorico) (Storico, error) {
	where, args := condizioniStorico(f)
	s := Storico{Filtri: f}
	if err := b.d.Lettura.QueryRowContext(ctx, `SELECT count(*) FROM consegne_artigiani c WHERE `+where, args...).Scan(&s.Totale); err != nil {
		return Storico{}, err
	}
	s.Pagine = max(1, (s.Totale+RighePerPaginaStorico-1)/RighePerPaginaStorico)
	s.Filtri.Pagina = max(1, min(f.Pagina, s.Pagine))
	rr, err := b.d.Lettura.QueryContext(ctx, `SELECT c.id, c.artigiano_id, a.nome, c.creato_il, c.chiusa_il,
		(SELECT count(*) FROM buste x WHERE x.consegna_artigiano_id = c.id AND x.cancellata_il IS NULL),
		(SELECT count(*) FROM buste x WHERE x.consegna_artigiano_id = c.id AND x.cancellata_il IS NULL AND `+strings.ReplaceAll(fuoriBottegaSQL, "b.", "x.")+`)
		FROM consegne_artigiani c JOIN artigiani a ON a.id = c.artigiano_id WHERE `+where+`
		ORDER BY c.creato_il DESC, c.id DESC LIMIT ? OFFSET ?`, append(args, RighePerPaginaStorico, (s.Filtri.Pagina-1)*RighePerPaginaStorico)...)
	if err != nil {
		return Storico{}, err
	}
	defer func() { _ = rr.Close() }() // la lettura è finita o l'errore è già quello che conta
	for rr.Next() {
		var r RigaStorico
		var aperta string
		var chiusa *string
		if err := rr.Scan(&r.ID, &r.ArtigianoID, &r.Artigiano, &aperta, &chiusa, &r.Buste, &r.Fuori); err != nil {
			return Storico{}, err
		}
		if r.Aperta, err = time.Parse(db.FormatoIstante, aperta); err != nil {
			return Storico{}, err
		}
		if r.Chiusa, err = istante(chiusa); err != nil {
			return Storico{}, err
		}
		s.Righe = append(s.Righe, r)
	}
	return s, rr.Err()
}

// RegistraStorico scrive handover.history_viewed con i nomi dei filtri usati (già filtrati e in ordine).
// Se non si scrive, l'elenco non si mostra.
func (b *Buste) RegistraStorico(ctx context.Context, chiavi []string, c audit.Contesto) error {
	if chiavi == nil {
		chiavi = []string{}
	}
	return b.reg.Registra(ctx, c, "handover.history_viewed", "handover_history", "index", "ok", map[string]any{"filter_keys": chiavi})
}

// RegistraDettaglioStorico scrive handover.history_detail_viewed sulla consegna, anche una che non c'è.
func (b *Buste) RegistraDettaglioStorico(ctx context.Context, id int64, c audit.Contesto) error {
	return b.reg.Registra(ctx, c, "handover.history_detail_viewed", "handover", id, "ok", nil)
}

// BustaDelloStorico è una busta del dettaglio, con il suo giro attuale.
type BustaDelloStorico struct {
	ID              int64
	Numero          string
	CodiceQR        *string
	Descrizione     string
	Uscita, Rientro *time.Time
	Note            *string
}

// Visibile è il numero da mostrare (D-64).
func (x BustaDelloStorico) Visibile() Mostrato { return mostratoDi(x.CodiceQR, x.Numero) }

// RiapertaDelloStorico è una busta che la consegna ha avuto e che un giro dopo le ha portato via: il giro
// chiuso di quella consegna (uscita nil: sconosciuta).
type RiapertaDelloStorico struct {
	ID              int64
	Numero          string
	CodiceQR        *string
	Descrizione     string
	Uscita, Rientro *time.Time
	NotaPresente    bool
}

// Visibile è il numero da mostrare (D-64).
func (x RiapertaDelloStorico) Visibile() Mostrato { return mostratoDi(x.CodiceQR, x.Numero) }

func mostratoDi(qr *string, numero string) Mostrato {
	q := ""
	if qr != nil {
		q = *qr
	}
	return NumeroMostrato(q, numero)
}

// DettaglioStorico è session/1: la consegna con l'artigiano, le sue buste e le riaperte.
type DettaglioStorico struct {
	Consegna ConsegnaArtigiano
	Chiusa   *time.Time
	Buste    []BustaDelloStorico
	Riaperte []RiapertaDelloStorico
}

// DettaglioStorico legge una consegna dello storico; ErrConsegnaNonTrovata se non c'è.
func (b *Buste) DettaglioStorico(ctx context.Context, id int64) (DettaglioStorico, error) {
	c, err := b.ConsegnaArtigiano(ctx, id)
	if err != nil {
		return DettaglioStorico{}, err
	}
	det := DettaglioStorico{Consegna: c}
	var chiusa *string
	if err := b.d.Lettura.QueryRowContext(ctx, `SELECT chiusa_il FROM consegne_artigiani WHERE id = ?`, id).Scan(&chiusa); err != nil {
		return DettaglioStorico{}, err
	}
	if det.Chiusa, err = istante(chiusa); err != nil {
		return DettaglioStorico{}, err
	}
	// bags/1: ORDER BY numero_busta come testo, poi id.
	rr, err := b.d.Lettura.QueryContext(ctx, `SELECT b.id, b.numero, b.codice_qr, b.descrizione, b.consegnata_artigiano_il,
		b.rientrata_artigiano_il, b.note_artigiano FROM buste b WHERE b.consegna_artigiano_id = ? AND b.cancellata_il IS NULL
		ORDER BY b.numero, b.id`, id)
	if err != nil {
		return DettaglioStorico{}, err
	}
	defer func() { _ = rr.Close() }() // la lettura è finita o l'errore è già quello che conta
	for rr.Next() {
		var x BustaDelloStorico
		var u, r *string
		if err := rr.Scan(&x.ID, &x.Numero, &x.CodiceQR, &x.Descrizione, &u, &r, &x.Note); err != nil {
			return DettaglioStorico{}, err
		}
		if x.Uscita, err = istante(u); err != nil {
			return DettaglioStorico{}, err
		}
		if x.Rientro, err = istante(r); err != nil {
			return DettaglioStorico{}, err
		}
		det.Buste = append(det.Buste, x)
	}
	if err := rr.Err(); err != nil {
		return DettaglioStorico{}, err
	}
	// reopened/1 dalla tabella dei giri.
	rg, err := b.d.Lettura.QueryContext(ctx, `SELECT b.id, b.numero, b.codice_qr, b.descrizione, g.uscita_il, g.rientro_il, g.nota_presente
		FROM giri_artigiani g JOIN buste b ON b.id = g.busta_id
		WHERE g.consegna_artigiano_id = ?1 AND g.rientro_il IS NOT NULL AND g.sganciata = 0
		  AND b.cancellata_il IS NULL AND b.consegna_artigiano_id IS NOT ?1
		ORDER BY g.rientro_il DESC, g.id`, id)
	if err != nil {
		return DettaglioStorico{}, err
	}
	defer func() { _ = rg.Close() }() // la lettura è finita o l'errore è già quello che conta
	for rg.Next() {
		var x RiapertaDelloStorico
		var u, r *string
		if err := rg.Scan(&x.ID, &x.Numero, &x.CodiceQR, &x.Descrizione, &u, &r, &x.NotaPresente); err != nil {
			return DettaglioStorico{}, err
		}
		if x.Uscita, err = istante(u); err != nil {
			return DettaglioStorico{}, err
		}
		if x.Rientro, err = istante(r); err != nil {
			return DettaglioStorico{}, err
		}
		det.Riaperte = append(det.Riaperte, x)
	}
	return det, rg.Err()
}

// consegnaChiusa è closed_handover/2 nella transazione: ErrConsegnaNonTrovata, o aperta il rifiuto dato.
func consegnaChiusa(ctx context.Context, tx *sql.Tx, id int64, aperta error) error {
	var chiusa *string
	err := tx.QueryRowContext(ctx, `SELECT chiusa_il FROM consegne_artigiani WHERE id = ?`, id).Scan(&chiusa)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrConsegnaNonTrovata
	case err != nil:
		return err
	case chiusa == nil:
		return aperta
	}
	return nil
}

// EliminaDalloStorico è delete_handover_from_history/2: dà le buste sganciate fuori dal cestino.
func (b *Buste) EliminaDalloStorico(ctx context.Context, id int64, c audit.Contesto) (int64, error) {
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	if err := consegnaChiusa(ctx, tx, id, ErrSoloChiuseElimina); err != nil {
		return 0, err
	}
	var vive int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM buste WHERE consegna_artigiano_id = ? AND cancellata_il IS NULL`, id).Scan(&vive); err != nil {
		return 0, err
	}
	conta := func(q string) (int64, error) {
		res, err := tx.ExecContext(ctx, q, id)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}
	tutte, err := conta(`UPDATE buste SET consegna_artigiano_id = NULL WHERE consegna_artigiano_id = ?`)
	if err != nil {
		return 0, err
	}
	solleciti, err := conta(`UPDATE solleciti_artigiani SET consegna_artigiano_id = NULL WHERE consegna_artigiano_id = ?`)
	if err != nil {
		return 0, err
	}
	if _, err := conta(`DELETE FROM consegne_artigiani WHERE id = ?`); err != nil {
		return 0, err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "handover.deleted", "handover", id, "ok", map[string]any{
		"bags_detached": vive, "discarded_bags_detached": tutte - vive, "reminders_unlinked": solleciti}); err != nil {
		return 0, err
	}
	return vive, tx.Commit()
}

// SganciaDalloStorico è detach_bag_from_history/3: la busta (fuori dal cestino) lascia la consegna chiusa, e i
// giri chiusi della coppia diventano sganciati, nella transazione della riga handover.bag_detached.
func (b *Buste) SganciaDalloStorico(ctx context.Context, consegna, busta int64, c audit.Contesto) error {
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	if err := consegnaChiusa(ctx, tx, consegna, ErrSoloChiuseSgancia); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE buste SET consegna_artigiano_id = NULL WHERE id = ? AND consegna_artigiano_id = ? AND cancellata_il IS NULL`,
		busta, consegna)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrBustaNonInConsegna
	}
	if err := b.reg.Scrivi(ctx, tx, c, "handover.bag_detached", "handover", consegna, "ok", map[string]any{"repair_id": busta}); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE giri_artigiani SET sganciata = 1 WHERE busta_id = ? AND consegna_artigiano_id = ? AND rientro_il IS NOT NULL`,
		busta, consegna); err != nil {
		return err
	}
	return tx.Commit()
}
