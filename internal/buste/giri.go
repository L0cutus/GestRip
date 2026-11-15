package buste

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
)

// I giri dall'artigiano (slice 081, ART-19, tema H): la tabella giri_artigiani (migrazione 0019) al posto
// della ricostruzione dall'audit di HandoverHistory.cycles/1 dello standalone (302439d), che con la
// conservazione di 5 anni dell'audit (D-14) perderebbe i giri vecchi. Le regole sono quelle della
// ricostruzione e stanno tutte nelle istruzioni qui sotto, le stesse della migrazione
// (TestMigrazioneGiriComeIlCodice):
//   - un giro chiuso per ogni riga repair.returned_from_artisan o repair.returned_from_artisan_auto della
//     busta (id_risorsa = l'id scritto com'è), con la consegna di metadata.handover_id (solo un intero) e la
//     nota presente solo con artisan_notes = true;
//   - la sua uscita è l'ultima handover.confirmed della stessa consegna che nomina la busta in repair_ids e
//     non è dopo il rientro; nessuna: uscita sconosciuta. Dal vivo l'uscita è già scritta nel giro aperto
//     che la conferma ha lasciato (lo stesso istante della sua riga d'audit), e il rientro la copia: così
//     non serve più l'audit;
//   - sganciata: la busta tolta a mano da quella consegna chiusa (handover.bag_detached con repair_id), che
//     la 082 legge per le «riaperte».

// sqlRientriNeiGiri scrive i giri chiusi dalle righe di rientro; chi la usa chiude la sottointerrogazione r:
// la ricostruzione con l'ordine dell'audit, il rientro dal vivo con la sola riga appena scritta.
const sqlRientriNeiGiri = `INSERT INTO giri_artigiani (busta_id, consegna_artigiano_id, uscita_il, rientro_il, nota_presente, sganciata)
SELECT r.busta_id, r.consegna_id,
  coalesce(
    (SELECT g.uscita_il FROM giri_artigiani g
      WHERE g.busta_id = r.busta_id AND g.rientro_il IS NULL AND g.consegna_artigiano_id IS r.consegna_id
        AND g.uscita_il <= r.creato_il
      ORDER BY g.uscita_il DESC, g.id DESC LIMIT 1),
    (SELECT max(c.creato_il) FROM eventi_audit c, json_each(c.dati, '$.repair_ids') j
      WHERE c.azione = 'handover.confirmed' AND c.risorsa = 'handover' AND c.id_risorsa = CAST(r.consegna_id AS TEXT)
        AND json_type(c.dati, '$.repair_ids') = 'array' AND j.type = 'integer' AND j.value = r.busta_id
        AND c.creato_il <= r.creato_il)),
  r.creato_il, r.nota,
  EXISTS (SELECT 1 FROM eventi_audit s
    WHERE s.azione = 'handover.bag_detached' AND s.risorsa = 'handover' AND s.id_risorsa = CAST(r.consegna_id AS TEXT)
      AND json_type(s.dati, '$.repair_id') = 'integer' AND json_extract(s.dati, '$.repair_id') = r.busta_id)
FROM (SELECT e.id, e.creato_il, b.id AS busta_id,
    CASE WHEN json_type(e.dati, '$.handover_id') = 'integer' THEN json_extract(e.dati, '$.handover_id') END AS consegna_id,
    json_type(e.dati, '$.artisan_notes') IS 'true' AS nota
  FROM eventi_audit e JOIN buste b ON b.id = CAST(e.id_risorsa AS INTEGER) AND CAST(b.id AS TEXT) = e.id_risorsa
  WHERE e.azione IN ('repair.returned_from_artisan', 'repair.returned_from_artisan_auto') AND e.risorsa = 'repair'`

// sqlRicostruzioneRientri è la ricostruzione dei giri chiusi, nell'ordine dell'audit.
const sqlRicostruzioneRientri = sqlRientriNeiGiri + `
) r ORDER BY r.creato_il, r.id`

// sqlGiriAperti scrive un giro aperto per ogni busta fuori adesso, con l'uscita che il suo rientro copierà
// (la conferma della sua consegna che la nomina; nessuna: sconosciuta).
const sqlGiriAperti = `INSERT INTO giri_artigiani (busta_id, consegna_artigiano_id, uscita_il)
SELECT b.id, b.consegna_artigiano_id,
  (SELECT max(c.creato_il) FROM eventi_audit c, json_each(c.dati, '$.repair_ids') j
    WHERE c.azione = 'handover.confirmed' AND c.risorsa = 'handover' AND c.id_risorsa = CAST(b.consegna_artigiano_id AS TEXT)
      AND json_type(c.dati, '$.repair_ids') = 'array' AND j.type = 'integer' AND j.value = b.id)
FROM buste b WHERE b.consegnata_artigiano_il IS NOT NULL AND b.rientrata_artigiano_il IS NULL ORDER BY b.id`

// RicostruisciGiri rifà giri_artigiani dall'audit e dalle buste, come la migrazione 0019: la usa l'import
// dopo aver copiato audit e buste, nella sua transazione. Solo per l'import, su un database appena creato:
// svuota la tabella, e dopo una pulizia dell'audit (D-14) cancellerebbe per sempre i giri vecchi.
func RicostruisciGiri(ctx context.Context, ex audit.Esecutore) error {
	for _, q := range []string{`DELETE FROM giri_artigiani`, sqlRicostruzioneRientri, sqlGiriAperti} {
		if _, err := ex.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

// usciteNeiGiri è la parte della conferma (077, 078): per ogni busta timbrata dalla riga handover.confirmed
// evento, un giro aperto con la consegna e l'istante della riga. Una busta timbrata non ha giri aperti: si
// timbra solo una busta in bottega, e il rientro toglie i giri aperti della busta.
func usciteNeiGiri(ctx context.Context, tx *sql.Tx, evento int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO giri_artigiani (busta_id, consegna_artigiano_id, uscita_il)
		SELECT j.value, CAST(e.id_risorsa AS INTEGER), e.creato_il FROM eventi_audit e, json_each(e.dati, '$.repair_ids') j
		WHERE e.id = ? ORDER BY j.key`, evento)
	return err
}

// rientroNeiGiri è la parte del rientro (080) e del rientro automatico (054): il giro chiuso della riga di
// rientro evento, con l'uscita del giro aperto della stessa consegna; poi la busta non ha più giri aperti.
func rientroNeiGiri(ctx context.Context, tx *sql.Tx, evento, busta int64) error {
	if _, err := tx.ExecContext(ctx, sqlRientriNeiGiri+` AND e.id = ?) r`, evento); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM giri_artigiani WHERE rientro_il IS NULL AND busta_id = ?`, busta)
	return err
}

// Giro è un giro della busta dall'artigiano, come le mappe di cycles/1.
type Giro struct {
	Consegna     *int64     // handover_id: nil = «Consegna senza sessione»
	Artigiano    *string    // l'artigiano della consegna, se la consegna c'è ancora
	Uscita       *time.Time // nil = sconosciuta
	Rientro      *time.Time // nil = ancora fuori (solo il giro attuale)
	Note         *string    // il testo, solo nel giro attuale
	NotaPresente bool
	Attuale      bool // il giro delle colonne della busta
}

// GiriDellArtigiano è Workshop.artisan_cycles/1 (HandoverHistory.cycles/1): tutti i giri della busta dal più
// recente, quello attuale dalle colonne quando la busta è uscita, poi i giri chiusi della tabella con il
// rientro prima della sua uscita (tutti, se non è uscita). Una busta che non c'è: ErrBustaNonTrovata.
func (b *Buste) GiriDellArtigiano(ctx context.Context, id int64) ([]Giro, error) {
	var (
		consegna       sql.NullInt64
		artigiano      sql.NullString
		uscita, rientr sql.NullString
		note           sql.NullString
	)
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT b.consegna_artigiano_id, a.nome, b.consegnata_artigiano_il, b.rientrata_artigiano_il,
		b.note_artigiano FROM buste b LEFT JOIN consegne_artigiani c ON c.id = b.consegna_artigiano_id
		LEFT JOIN artigiani a ON a.id = c.artigiano_id WHERE b.id = ?`, id).Scan(&consegna, &artigiano, &uscita, &rientr, &note)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBustaNonTrovata
	}
	if err != nil {
		return nil, err
	}
	var giri []Giro
	if uscita.Valid {
		g := Giro{Consegna: nullaOIntero(consegna), Artigiano: nullaOTesto(artigiano), Note: nullaOTesto(note), NotaPresente: note.Valid, Attuale: true}
		if g.Uscita, err = istante(nullaOTesto(uscita)); err != nil {
			return nil, err
		}
		if g.Rientro, err = istante(nullaOTesto(rientr)); err != nil {
			return nil, err
		}
		giri = append(giri, g)
	}
	rr, err := b.d.Lettura.QueryContext(ctx, `SELECT g.consegna_artigiano_id, a.nome, g.uscita_il, g.rientro_il, g.nota_presente
		FROM giri_artigiani g LEFT JOIN consegne_artigiani c ON c.id = g.consegna_artigiano_id LEFT JOIN artigiani a ON a.id = c.artigiano_id
		WHERE g.busta_id = ?1 AND g.rientro_il IS NOT NULL
		  AND (?2 IS NULL OR g.rientro_il < ?2)
		ORDER BY g.rientro_il DESC, g.id`, id, uscita)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rr.Close() }() // la lettura è finita o l'errore è già quello che conta
	for rr.Next() {
		var g Giro
		var u, r sql.NullString
		if err := rr.Scan(&consegna, &artigiano, &u, &r, &g.NotaPresente); err != nil {
			return nil, err
		}
		g.Consegna, g.Artigiano = nullaOIntero(consegna), nullaOTesto(artigiano)
		if g.Uscita, err = istante(nullaOTesto(u)); err != nil {
			return nil, err
		}
		if g.Rientro, err = istante(nullaOTesto(r)); err != nil {
			return nil, err
		}
		giri = append(giri, g)
	}
	return giri, rr.Err()
}

func nullaOIntero(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

func nullaOTesto(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	v := s.String
	return &v
}
