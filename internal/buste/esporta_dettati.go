package buste

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/assist"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
)

// L'esportazione dei dettati raccolti per l'addestramento (slice 117; RIP-H-52, OPS-09), come TrainingExport.run/2 dello
// standalone a go-baseline, nel suo ordine:
//   - prima si leggono i dettati (con la loro busta com'è adesso, anche nel cestino, e se il cliente si è opposto) e le
//     parole dei nomi degli altri (tutti i clienti e i nomi propri comuni, meno le parole delle oreficerie): una lettura
//     che fallisce non lascia un file vuoto che rifiuterebbe l'esportazione dopo;
//   - il file si crea qui e solo qui: mai sopra uno che c'è (O_EXCL, anche un collegamento simbolico), del solo
//     proprietario (0600) prima di scrivere una riga;
//   - ogni riga scritta o scartata con il motivo (assist.RigaPerAddestramento), con una chiave nuova
//     (assist.NuovaChiave) che non si salva mai; il numero di busta non si legge nemmeno;
//   - il file sincronizzato (fsync), chiuso, e la cartella sincronizzata (il Go in più dello standalone, chiesto dal
//     piano: il nome del file resta anche dopo un'interruzione di corrente);
//   - solo allora, in una transazione, si cancellano le righe lette (scritte e scartate, a blocchi di 500) e si scrive
//     l'audit assist.training_exported dell'attore «export» con i soli numeri. Una riga raccolta mentre l'esportazione
//     girava non è stata letta e resta.
//
// Un errore prima della transazione non cancella niente e toglie il file a metà. Un errore della transazione stessa (la
// cancellazione o l'audit) toglie anche il file finito, ma solo se una lettura nuova trova ancora tutte le righe lette
// (D-112, coordinatore 24/11/2026); se ne manca anche una, o la lettura non riesce, il file resta (ErrFileTenuto). Lo
// standalone in quel caso si ferma con un'eccezione e il file resta sempre.
//
// Gli errori dei file non portano il percorso (senzaPercorso): il nome del file potrebbe essere quello di un cliente.
//
// Riservatezza: dettati, nomi e telefoni non vanno mai in un errore restituito; il resoconto è fatto di soli numeri.

// ErrFileEsistente: il file dell'esportazione c'è già. Niente è stato scritto né cancellato.
var ErrFileEsistente = errors.New("il file esiste già: l'esportazione non sovrascrive")

// attoreEsportazione è l'attore dell'audit dell'esportazione (%{actor: "export"}).
const attoreEsportazione = "export"

// azioneEsportazione è la riga d'audit dell'esportazione (assist.training_exported dello standalone).
const azioneEsportazione = "assist.training_exported"

// bloccoCancellazione è Enum.chunk_every(500) della cancellazione.
const bloccoCancellazione = 500

// fileDettati è il file in cui si scrive: *os.File, o nei test un file con un guasto.
type fileDettati interface {
	io.Writer
	Sync() error
	Close() error
}

// ConteggiEsportazione sono i numeri dell'esportazione (counts dello standalone).
type ConteggiEsportazione struct {
	Totali, Esportati, Cancellati int64
	// Scartati sono le righe scartate per motivo (assist.MotivoScarto); vuota se nessuna.
	Scartati map[string]int64
}

// Resoconto è TrainingExport.report/1: le righe nome=numero che il comando stampa, e nient'altro. Gli scartati per motivo
// vengono dopo, in ordine di motivo.
func (c ConteggiEsportazione) Resoconto() []string {
	var scartati int64
	motivi := make([]string, 0, len(c.Scartati))
	for m, n := range c.Scartati {
		scartati += n
		motivi = append(motivi, m)
	}
	slices.Sort(motivi)
	out := []string{
		"totali=" + strconv.FormatInt(c.Totali, 10),
		"esportati=" + strconv.FormatInt(c.Esportati, 10),
		"scartati=" + strconv.FormatInt(scartati, 10),
		"cancellati=" + strconv.FormatInt(c.Cancellati, 10),
	}
	for _, m := range motivi {
		out = append(out, "scartati_"+m+"="+strconv.FormatInt(c.Scartati[m], 10))
	}
	return out
}

// dettatoConBusta è una riga letta: il dettato e la sua busta com'è adesso; illeggibile se l'istante non si legge.
type dettatoConBusta struct {
	d           assist.DettatoRaccolto
	b           assist.BustaAttuale
	illeggibile bool
}

// EsportaDettati è TrainingExport.run/2: scrive in un file nuovo percorso ogni dettato raccolto con i pseudonimi, poi
// cancella le righe lette. Dà i conteggi; ErrFileEsistente se il file c'è già.
func (b *Buste) EsportaDettati(ctx context.Context, percorso string) (ConteggiEsportazione, error) {
	chiave, err := assist.NuovaChiave()
	if err != nil {
		return ConteggiEsportazione{}, err
	}
	righe, err := b.dettatiDaEsportare(ctx)
	if err != nil {
		return ConteggiEsportazione{}, err
	}
	altri, err := b.paroleDiAltriNomi(ctx)
	if err != nil {
		return ConteggiEsportazione{}, err
	}
	f, err := os.OpenFile(percorso, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return ConteggiEsportazione{}, ErrFileEsistente
	}
	if err != nil {
		return ConteggiEsportazione{}, fmt.Errorf("apertura del file: %w", senzaPercorso(err))
	}
	finito := false
	defer func() {
		if !finito {
			_ = os.Remove(percorso) // il file a metà (o finito, se la cancellazione non è riuscita) non resta
		}
	}()
	esiti, err := b.scriviDettati(ctx, f, righe, altri, chiave)
	if err != nil {
		return ConteggiEsportazione{}, err
	}
	sync := syncCartella
	if b.syncCartellaEsportazione != nil {
		sync = b.syncCartellaEsportazione
	}
	if err := sync(filepath.Dir(percorso)); err != nil {
		return ConteggiEsportazione{}, fmt.Errorf("sync della cartella: %w", senzaPercorso(err))
	}
	c, err := b.chiudiEsportazione(ctx, righe, esiti)
	if err != nil {
		// D-112: il file si toglie solo se le righe lette sono ancora tutte nel database; altrimenti (commit riuscito
		// nonostante l'errore, o verifica non riuscita) il file è forse l'unica copia di quei dettati e resta.
		if tenuto := b.verificaDopoErrore(ctx, righe); tenuto != nil {
			finito = true
			return ConteggiEsportazione{}, tenuto
		}
		return ConteggiEsportazione{}, err
	}
	finito = true
	return c, nil
}

// ErrFileTenuto (D-112): la transazione finale ha dato errore, ma le righe lette non sono più tutte nel database, o la
// verifica non è riuscita: il file resta. Il messaggio è fatto di soli numeri, senza percorso né dati.
type ErrFileTenuto struct {
	// Mancanti sono le righe lette che non ci sono più, Lette quelle lette.
	Mancanti, Lette int
	// VerificaFallita: la lettura di verifica non è riuscita (Mancanti non è noto).
	VerificaFallita bool
}

func (e *ErrFileTenuto) Error() string {
	if e.VerificaFallita {
		return "la cancellazione dei dettati ha dato errore e la verifica non è riuscita: il file resta"
	}
	return fmt.Sprintf("la cancellazione dei dettati ha dato errore, ma %d righe su %d non ci sono più: il file resta", e.Mancanti, e.Lette)
}

// verificaDopoErrore conta, con una lettura nuova (anche con il contesto annullato), le righe lette ancora nel database:
// nil se ci sono tutte (il file si può togliere), altrimenti l'ErrFileTenuto.
func (b *Buste) verificaDopoErrore(ctx context.Context, righe []dettatoConBusta) *ErrFileTenuto {
	ctx = context.WithoutCancel(ctx)
	var presenti int
	for da := 0; da < len(righe); da += bloccoCancellazione {
		blocco := righe[da:min(da+bloccoCancellazione, len(righe))]
		args := make([]any, len(blocco))
		for i, r := range blocco {
			args[i] = r.d.ID
		}
		var n int
		q := `SELECT count(*) FROM dettati_addestramento WHERE id IN (?` + strings.Repeat(`, ?`, len(blocco)-1) + `)`
		if err := b.d.Lettura.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			return &ErrFileTenuto{Lette: len(righe), VerificaFallita: true}
		}
		presenti += n
	}
	if presenti == len(righe) {
		return nil
	}
	return &ErrFileTenuto{Mancanti: len(righe) - presenti, Lette: len(righe)}
}

// senzaPercorso toglie il percorso da un errore di file (*fs.PathError, *os.LinkError): il nome del file potrebbe essere
// quello di un cliente, e non va su stderr.
func senzaPercorso(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Err
	}
	return err
}

// scriviDettati è write/5: i permessi prima di ogni riga, le righe, la sync e la chiusura; dà il motivo dello scarto di
// ogni riga ("" se scritta). Con un errore il file è chiuso lo stesso.
func (b *Buste) scriviDettati(ctx context.Context, f *os.File, righe []dettatoConBusta, altri map[string]bool, k assist.Chiave) (esiti []assist.MotivoScarto, err error) {
	chiuso := false
	defer func() {
		if !chiuso {
			_ = f.Close()
		}
	}()
	// La umask può solo togliere permessi a 0600; il chmod li rimette uguali comunque, come File.chmod dello standalone.
	if err := f.Chmod(0o600); err != nil {
		return nil, fmt.Errorf("permessi del file: %w", senzaPercorso(err))
	}
	var w fileDettati = f
	if b.fileEsportazione != nil {
		w = b.fileEsportazione(f)
	}
	buf := bufio.NewWriterSize(w, 64<<10)
	esiti = make([]assist.MotivoScarto, len(righe))
	for i, r := range righe {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if r.illeggibile {
			esiti[i] = assist.ScartoErrore
			continue
		}
		riga, m := assist.RigaPerAddestramento(r.d, r.b, altri, k)
		if m != "" {
			esiti[i] = m
			continue
		}
		if _, err := buf.Write(riga); err != nil {
			return nil, fmt.Errorf("scrittura del file: %w", senzaPercorso(err))
		}
	}
	if err := buf.Flush(); err != nil {
		return nil, fmt.Errorf("scrittura del file: %w", senzaPercorso(err))
	}
	if err := w.Sync(); err != nil {
		return nil, fmt.Errorf("sync del file: %w", senzaPercorso(err))
	}
	chiuso = true
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("chiusura del file: %w", senzaPercorso(err))
	}
	return esiti, nil
}

// syncCartella rende durevole il nome del file nella cartella.
func syncCartella(cartella string) error {
	c, err := os.Open(cartella)
	if err != nil {
		return err
	}
	if err := c.Sync(); err != nil {
		_ = c.Close()
		return err
	}
	return c.Close()
}

// chiudiEsportazione è finish/1: in una transazione cancella le righe lette e scrive l'audit con i numeri; o tutte e due
// le cose o nessuna.
func (b *Buste) chiudiEsportazione(ctx context.Context, righe []dettatoConBusta, esiti []assist.MotivoScarto) (ConteggiEsportazione, error) {
	c := ConteggiEsportazione{Totali: int64(len(righe)), Scartati: map[string]int64{}}
	for _, m := range esiti {
		if m == "" {
			c.Esportati++
		} else {
			c.Scartati[string(m)]++
		}
	}
	err := b.inTransazioneTx(ctx, func(tx *sql.Tx, _ string) error {
		c.Cancellati = 0
		for da := 0; da < len(righe); da += bloccoCancellazione {
			blocco := righe[da:min(da+bloccoCancellazione, len(righe))]
			args := make([]any, len(blocco))
			for i, r := range blocco {
				args[i] = r.d.ID
			}
			q := `DELETE FROM dettati_addestramento WHERE id IN (?` + strings.Repeat(`, ?`, len(blocco)-1) + `)`
			res, err := tx.ExecContext(ctx, q, args...)
			if err != nil {
				return fmt.Errorf("cancellazione dei dettati: %w", err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			c.Cancellati += n
		}
		return b.reg.Scrivi(ctx, tx, audit.Contesto{Attore: attoreEsportazione}, azioneEsportazione, "assist", "training", "ok",
			map[string]any{"totali": c.Totali, "esportati": c.Esportati, "scartati": c.Scartati, "cancellati": c.Cancellati})
	})
	if err == nil && b.dopoCommitEsportazione != nil {
		err = b.dopoCommitEsportazione()
	}
	if err != nil {
		return ConteggiEsportazione{}, err
	}
	return c, nil
}

// errLettura è una riga che non si legge: l'errore di Scan può portare un valore, quindi non si passa.
var errLettura = errors.New("lettura dei dettati: una riga non si legge")

// dettatiDaEsportare è rows/0: ogni dettato con la sua busta (anche nel cestino, che si conta e si scarta), l'oreficeria
// e se il cliente si è opposto, per id.
func (b *Buste) dettatiDaEsportare(ctx context.Context) ([]dettatoConBusta, error) {
	rows, err := b.d.Lettura.QueryContext(ctx, `SELECT d.id, d.testo, d.proposta, d.modello, d.creato_il,
		b.data_accettazione, b.nome_cliente, b.telefono_cliente, b.descrizione, b.note_pubbliche,
		b.preventivo_testo, b.preventivo_centesimi, b.data_prevista_testo, b.data_prevista,
		o.nome, b.oreficeria_id, b.compro_oro, b.cancellata_il IS NOT NULL,
		EXISTS (SELECT 1 FROM dettati_esclusioni e WHERE e.cliente_id = b.cliente_id)
		FROM dettati_addestramento d
		JOIN buste b ON b.id = d.busta_id
		LEFT JOIN oreficerie o ON o.id = b.oreficeria_id
		ORDER BY d.id`)
	if err != nil {
		return nil, fmt.Errorf("lettura dei dettati: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []dettatoConBusta
	for rows.Next() {
		var r dettatoConBusta
		var modello, note, prevTesto, consTesto, consData, oreficeria sql.NullString
		var prevCent, oreID sql.NullInt64
		var creato, accettazione string
		if err := rows.Scan(&r.d.ID, &r.d.Testo, &r.d.Proposta, &modello, &creato,
			&accettazione, &r.b.Nome, &r.b.Telefono, &r.b.Descrizione, &note,
			&prevTesto, &prevCent, &consTesto, &consData,
			&oreficeria, &oreID, &r.b.CompraOro, &r.b.NelCestino, &r.b.ClienteEscluso); err != nil {
			return nil, errLettura
		}
		if modello.Valid {
			r.d.Modello = &modello.String
		}
		t, err := time.Parse(db.FormatoIstante, creato)
		r.d.CreatoIl, r.illeggibile = t, err != nil
		r.b.Data = dataGiornoMeseAnno(accettazione)
		r.b.Note = note.String
		var testo *string
		if prevTesto.Valid {
			testo = &prevTesto.String
		}
		var cent *int64
		if prevCent.Valid {
			cent = &prevCent.Int64
		}
		if p := testoMostrato(testo, cent); p != nil {
			r.b.Preventivo = *p
		}
		r.b.Consegna = prevista(consTesto, consData, dataGiornoMeseAnno)
		r.b.Oreficeria = oreficeria.String
		if oreID.Valid {
			r.b.OreficeriaID = &oreID.Int64
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("lettura dei dettati: %w", err)
	}
	return out, nil
}

// paroleDiAltriNomi è other_names/0: le parole dei nomi di tutti i clienti e dei nomi propri comuni, meno quelle delle
// oreficerie.
func (b *Buste) paroleDiAltriNomi(ctx context.Context) (map[string]bool, error) {
	clienti, err := b.nomi(ctx, `SELECT nome FROM clienti`)
	if err != nil {
		return nil, err
	}
	oreficerie, err := b.nomi(ctx, `SELECT nome FROM oreficerie`)
	if err != nil {
		return nil, err
	}
	return assist.ParoleDiAltriNomi(clienti, oreficerie), nil
}

func (b *Buste) nomi(ctx context.Context, q string) ([]string, error) {
	rows, err := b.d.Lettura.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("lettura dei nomi: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, errLettura
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("lettura dei nomi: %w", err)
	}
	return out, nil
}
