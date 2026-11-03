package media

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/db"
)

// La pulizia dei media (RIP-S-57), come MediaCleanup e MediaStore.delete_pending/0,
// delete_pending_while_locked/0, delete_one_pending/1, cleanup_temporary_uploads_while_locked/0,
// queue_deletions/1 e referenced?/1 dello standalone a go-baseline
// (lib/gestrip_standalone/media_cleanup.ex, media_store.ex).
//
// La coda (cancellazioni_media) tiene le chiavi degli oggetti che un'operazione confermata ha
// lasciato senza righe: la mettono lì, nella propria transazione, il cestino definitivo, la
// sostituzione del logo e la conservazione delle foto (queue_deletions/1), che nel Go non ci sono
// ancora; oggi la riempie solo l'import (media_deletion_jobs dello standalone). Togliere una foto
// da una busta non la mette in coda (RepairPhotos.remove/3): quel file lo raccoglie la pulizia degli
// orfani dopo un backup verificato (RIP-S-55, T4.11).

// OgniQuantoPulizia è l'attesa fra due giri della pulizia (tick_interval_ms di MediaCleanup in
// config/config.exs).
const OgniQuantoPulizia = 60 * time.Second

// prefissoTemporaneo è il nome dei temporanei di SalvaConLucchetto.
const prefissoTemporaneo = ".upload-"

// Riferimento è una colonna del database che nomina un oggetto dell'archivio con la sua chiave.
type Riferimento struct{ Tabella, Colonna string }

// Riferimenti sono **tutte** le colonne che nominano un oggetto dell'archivio (referenced?/1): la
// pulizia non cancella un oggetto in coda che una di queste righe usa ancora. Ci sono tutte da
// oggi, anche quelle delle funzioni che il Go non ha ancora, perché l'import le riempie:
//   - foto_buste: le foto delle buste (T4.3);
//   - buste.firma_chiave ed eventi_firma_consegna: la firma della consegna; una firma azzerata
//     (firma_chiave NULL) resta protetta dal suo evento;
//   - foto_vendite e foto_revisioni_vendite: le foto delle vendite e delle loro revisioni;
//   - documenti_storici: i PDF del Flask;
//   - immagini_negozio: logo e icone.
//
// Una tabella nuova con una colonna che nomina un oggetto va aggiunta qui:
// TestRiferimentiCopronoLoSchema fallisce per ogni colonna dello schema con «chiave» nel nome che
// non è qui né tra le escluse con il perché.
var Riferimenti = []Riferimento{
	{"foto_buste", "chiave"},
	{"buste", "firma_chiave"},
	{"eventi_firma_consegna", "chiave"},
	{"foto_vendite", "chiave"},
	{"foto_revisioni_vendite", "chiave"},
	{"documenti_storici", "chiave"},
	{"immagini_negozio", "chiave"},
}

// interrogazioneUsata dice se una riga di Riferimenti nomina la chiave ?1.
var interrogazioneUsata = func() string {
	v := make([]string, len(Riferimenti))
	for i, r := range Riferimenti {
		v[i] = fmt.Sprintf("EXISTS (SELECT 1 FROM %s WHERE %s = ?1)", r.Tabella, r.Colonna)
	}
	return "SELECT " + strings.Join(v, " OR ")
}()

// Esecutore è la transazione di chi mette in coda.
type Esecutore interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

// Accoda è queue_deletions/1: mette in coda le chiavi, nella transazione tx di chi ha tolto le
// righe, con l'istante adesso; una chiave ripetuta conta una volta e una già in coda tiene il suo
// istante (INSERT OR IGNORE). A differenza dello standalone una chiave malformata è ErrChiave e
// non accoda niente: in coda bloccherebbe il giro per sempre.
func Accoda(ctx context.Context, tx Esecutore, chiavi []string, adesso time.Time) error {
	for _, k := range chiavi {
		if !ChiaveValida(k) {
			return ErrChiave
		}
	}
	quando := adesso.UTC().Format(db.FormatoIstante)
	for _, k := range chiavi {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO cancellazioni_media (chiave, creato_il) VALUES (?, ?)`, k, quando); err != nil {
			return err
		}
	}
	return nil
}

// Pulizia è il giro di MediaCleanup sull'archivio a, con la coda del database d e la cache delle
// miniature m (può essere nil).
type Pulizia struct {
	d *db.DB
	a *Archivio
	m *Miniature
	// traFileERiga, solo nei test, gira dopo la rimozione del file e prima di quella della riga.
	traFileERiga func() error
	// attesaDetta: la riga «la coda aspetta il backup verificato» è già nel registro di questo avvio.
	attesaDetta bool
}

// NuovaPulizia dà la pulizia dell'archivio a. L'archivio deve essere lo stesso dei caricamenti:
// il lucchetto è il suo.
func NuovaPulizia(d *db.DB, a *Archivio, m *Miniature) *Pulizia {
	return &Pulizia{d: d, a: a, m: m}
}

// CancellaInCoda è delete_pending/0: CancellaInCodaConLucchetto con il lucchetto dell'archivio,
// che la separa da caricamenti, cancellazioni ed esportazioni.
func (p *Pulizia) CancellaInCoda(ctx context.Context) error {
	_, err := p.cancellaInCoda(ctx)
	return err
}

func (p *Pulizia) cancellaInCoda(ctx context.Context) (inAttesa bool, err error) {
	err = p.a.ConLucchetto(func() error {
		inAttesa, err = p.cancellaInCodaConLucchetto(ctx)
		return err
	})
	return inAttesa, err
}

// CancellaInCodaConLucchetto è delete_pending_while_locked/0, per chi tiene già il lucchetto: prima
// i temporanei abbandonati, poi le chiavi in coda in ordine, una transazione per chiave; si ferma al
// primo errore e la coda resta da riprovare.
//
// Differenza dallo standalone, in attesa della domanda aperta 20: dopo un import dallo standalone,
// finché non c'è un backup verificato (db.BloccatoDopoImport, lo stesso controllo della
// conservazione dell'audit, ADM-17), la coda non si tocca: niente file cancellati, niente righe
// tolte, nessun errore. I temporanei si tolgono comunque: non sono dati.
func (p *Pulizia) CancellaInCodaConLucchetto(ctx context.Context) error {
	_, err := p.cancellaInCodaConLucchetto(ctx)
	return err
}

func (p *Pulizia) cancellaInCodaConLucchetto(ctx context.Context) (inAttesa bool, err error) {
	if err := p.togliTemporanei(); err != nil {
		return false, err
	}
	bloccata, err := db.BloccatoDopoImport(ctx, p.d.Scrittura)
	if err != nil || bloccata {
		return bloccata, err
	}
	chiavi, err := p.inCoda(ctx)
	if err != nil {
		return false, err
	}
	for _, k := range chiavi {
		if err := p.cancellaUna(ctx, k); err != nil {
			return false, err
		}
	}
	return false, nil
}

func (p *Pulizia) inCoda(ctx context.Context) ([]string, error) {
	r, err := p.d.Scrittura.QueryContext(ctx, `SELECT chiave FROM cancellazioni_media ORDER BY chiave`)
	if err != nil {
		return nil, err
	}
	var v []string
	for r.Next() {
		var k string
		if err := r.Scan(&k); err != nil {
			_ = r.Close()
			return nil, err
		}
		v = append(v, k)
	}
	return v, errors.Join(r.Err(), r.Close())
}

// cancellaUna è delete_one_pending/1, in una transazione (immediate: nessuno scrive una riga che
// usa la chiave fra il controllo e la fine). Se nessuna riga usa la chiave: via il file (se manca
// va bene), fsync della cartella, via la miniatura. Poi via la riga della coda. Un errore del file
// o della cartella annulla la transazione e la riga resta: un file già tolto non torna, e al giro
// dopo «manca» va bene.
func (p *Pulizia) cancellaUna(ctx context.Context, k string) error {
	if !ChiaveValida(k) {
		return ErrChiave
	}
	tx, err := p.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var usata bool
	if err := tx.QueryRowContext(ctx, interrogazioneUsata, k).Scan(&usata); err != nil {
		return err
	}
	if !usata {
		if err := os.Remove(filepath.Join(p.a.Cartella(), k)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return ErrSalvataggio
		}
		if err := p.a.sincronizzaDir(); err != nil {
			return ErrSalvataggio
		}
		if p.m != nil {
			p.m.Togli(k)
		}
	}
	if p.traFileERiga != nil {
		if err := p.traFileERiga(); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM cancellazioni_media WHERE chiave = ?`, k); err != nil {
		return err
	}
	return tx.Commit()
}

// togliTemporanei è cleanup_temporary_uploads_while_locked/0: con il lucchetto un temporaneo
// «.upload-…» è di un salvataggio interrotto, quindi si toglie; uno che manca va bene, uno che non
// si toglie è ErrSalvataggio. Se ne ha tolti, fsync della cartella. Una cartella che non c'è non ha
// temporanei.
func (p *Pulizia) togliTemporanei() error {
	v, err := os.ReadDir(p.a.Cartella())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return ErrSalvataggio
	}
	tolti := false
	for _, e := range v {
		if !strings.HasPrefix(e.Name(), prefissoTemporaneo) {
			continue
		}
		if err := os.Remove(filepath.Join(p.a.Cartella(), e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return ErrSalvataggio
		}
		tolti = true
	}
	if tolti {
		if err := p.a.sincronizzaDir(); err != nil {
			return ErrSalvataggio
		}
	}
	return nil
}

// Lavoro è il lavoro di serve (MediaCleanup): un giro subito (handle_continue :drain, quindi anche
// i temporanei rimasti da un arresto) e uno dopo ogni attesa, finché aspetta dice di smettere. Un
// errore o un panico vanno nel registro e il giro dopo riprova: la pulizia non ferma serve.
func (p *Pulizia) Lavoro(ctx context.Context, aspetta func(context.Context) bool, log *slog.Logger) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	for {
		p.giro(ctx, log)
		if !aspetta(ctx) {
			return
		}
	}
}

// giro è drain/1, nell'ordine dello standalone. Le parti che non ci sono ancora sono segnate al loro
// posto (test saltati in pulizia_resto_test.go).
func (p *Pulizia) giro(ctx context.Context, log *slog.Logger) {
	defer func() {
		if v := recover(); v != nil {
			// Il valore di un panico può avere una chiave o un percorso: solo il tipo.
			log.Error("pulizia dei media: giro interrotto da un panico, si riprova al prossimo giro", "tipo", fmt.Sprintf("%T", v))
		}
	}()
	// Da fare: le esportazioni vecchie (cleanup_stale_exports/0), con l'esportazione dell'istanza.
	// Da fare: una volta al giorno la conservazione delle foto (run_retention/3), T4.11.
	// Da fare: una volta al giorno la scadenza dei dettati (run_training_expiry/3), T4.10.
	inAttesa, err := p.cancellaInCoda(ctx)
	if inAttesa && !p.attesaDetta {
		p.attesaDetta = true
		log.Warn("pulizia dei media: il database viene da un import e manca un backup verificato, la coda delle cancellazioni aspetta (domanda aperta 20)")
	}
	if err == nil || ctx.Err() != nil {
		// Lo stop: quello che è fatto resta, la coda riprende al prossimo avvio.
		return
	}
	log.Warn("pulizia dei media rimandata: la coda resta da riprovare", "causa", causa(err))
}

// causa è il genere dell'errore per il registro, mai il testo: niente chiavi né percorsi.
func causa(err error) string {
	switch {
	case errors.Is(err, ErrChiave):
		return "chiave"
	case errors.Is(err, ErrSalvataggio):
		return "archivio"
	}
	return "database"
}
