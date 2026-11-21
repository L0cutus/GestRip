package buste

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/L0cutus/gestrip/internal/audit"
)

// La raccolta dei dettati per l'addestramento al salvataggio della busta (slice 113; RIP-H-48), come
// Training.record/3 chiamato da Workshop.create_repair/4 dello standalone a go-baseline:
//   - la riga nasce solo dentro Crea, nella transazione della busta: una busta rifiutata (o un avviso che non la salva
//     ancora) non lascia niente;
//   - solo con una cattura (Accettazione.Dettato): la pagina la passa quando la busta è stata compilata da
//     un'estrazione «Dettato AI» della stessa pagina; una busta scritta a mano non ne ha;
//   - solo con l'interruttore acceso (assist.training.collect = «1»; spenta di default), con il cliente che non si è
//     opposto (dettati_esclusioni: niente riga, lotto non consumato) e con il lotto (assist.training.remaining) non
//     finito: ogni riga consuma un'unità con un UPDATE condizionato (WHERE rimaste > 0), così due salvataggi
//     sull'ultima unità non scrivono tutti e due; l'ultima unità spegne la raccolta subito, e un interruttore rimasto
//     acceso su un lotto finito (o senza lotto) si spegne al salvataggio dopo, tutte e due le volte con la riga d'audit
//     assist.training_collect dell'attore «system» e soli numeri;
//   - un errore nella raccolta non blocca mai la busta (D-108): tutto quello che la raccolta legge e scrive sta in un
//     SAVEPOINT dentro la transazione della busta, e un errore lo annulla per intero (riga, unità consumata, spegnimento)
//     lasciando la busta com'è (vedi registraDettato).
//
// Riservatezza: il testo, la proposta e il modello non vanno mai in un registro, in un audit o in un errore restituito.

// Le chiavi delle impostazioni della raccolta, quelle dello standalone (l'import le porta come sono).
const (
	ChiaveRaccoltaAccesa  = "assist.training.collect"
	ChiaveRaccoltaRimaste = "assist.training.remaining"
)

// AttoreSistema è l'attore delle righe d'audit scritte dal programma da solo (%{actor: "system"}).
const AttoreSistema = "system"

// Dettato è la cattura di un'estrazione (Training.capture/3) che la pagina passa al salvataggio: il dettato mandato al
// modello senza spazi ai bordi, la proposta del modello già in JSON e il nome del modello che ha risposto (vuoto: NULL).
// Dati del cliente: String, GoString e LogValue li nascondono.
type Dettato struct {
	Testo    string
	Proposta []byte
	Modello  string
}

// String nasconde la cattura (%v e %+v), come redact: true dello schema Dettato dello standalone.
func (Dettato) String() string { return "{Dettato:**redacted**}" }

// GoString la nasconde anche con %#v.
func (Dettato) GoString() string { return "buste.Dettato{**redacted**}" }

// LogValue la nasconde nel registro slog.
func (Dettato) LogValue() slog.Value { return slog.StringValue("**redacted**") }

// EsitoRaccolta dice che cosa ha fatto la raccolta per una busta salvata. Lo standalone risponde {:ok, :recorded |
// :excluded | :off} e la pagina non lo guarda; qui serve ai test e al registro (una frase fissa per RaccoltaNonScritta).
type EsitoRaccolta int

const (
	RaccoltaNessuna    EsitoRaccolta = iota // nessuna cattura: busta scritta a mano
	RaccoltaSpenta                          // interruttore spento, o lotto finito (e ora spento)
	RaccoltaEsclusa                         // il cliente si è opposto: niente riga, lotto intero
	RaccoltaRegistrata                      // la riga è scritta e il lotto è sceso di uno
	RaccoltaNonScritta                      // un errore: niente della raccolta è rimasto, la busta sì
)

func (e EsitoRaccolta) String() string {
	switch e {
	case RaccoltaNessuna:
		return "nessuna"
	case RaccoltaSpenta:
		return "spenta"
	case RaccoltaEsclusa:
		return "esclusa"
	case RaccoltaRegistrata:
		return "registrata"
	case RaccoltaNonScritta:
		return "non scritta"
	}
	return fmt.Sprintf("EsitoRaccolta(%d)", int(e))
}

// registraDettato è Training.record/3 per la busta appena inserita (busta, del cliente cliente), dentro la sua
// transazione tx. Senza cattura non fa niente. Con la cattura tutto va in un SAVEPOINT: un errore (lettura, lotto,
// riga, spegnimento, audit) torna al SAVEPOINT e dà RaccoltaNonScritta, e la busta si salva lo stesso (D-108). Lo
// standalone, se l'INSERT della riga fallisce, tiene l'unità consumata e risponde :off, e con un guasto nelle letture o
// nel lotto fa fallire la busta; qui niente della raccolta resta e l'unità non si consuma. Solo se il ritorno al SAVEPOINT fallisce (il collegamento è rotto: il commit fallirebbe
// comunque) l'errore torna a Crea.
func (b *Buste) registraDettato(ctx context.Context, tx esecutoreLettore, busta, cliente int64, d *Dettato, ora string) (EsitoRaccolta, error) {
	if d == nil {
		return RaccoltaNessuna, nil
	}
	if _, err := tx.ExecContext(ctx, `SAVEPOINT raccolta_dettato`); err != nil {
		return RaccoltaNonScritta, nil // niente è stato scritto: la busta va avanti
	}
	esito, err := b.registraNelSavepoint(ctx, tx, busta, cliente, d, ora)
	if err != nil {
		// L'errore non si guarda né si registra: potrebbe nominare il contenuto della riga.
		if _, errR := tx.ExecContext(ctx, `ROLLBACK TO raccolta_dettato`); errR != nil {
			return RaccoltaNonScritta, errors.New("buste: raccolta dei dettati non annullata")
		}
		esito = RaccoltaNonScritta
	}
	if _, err := tx.ExecContext(ctx, `RELEASE raccolta_dettato`); err != nil {
		return RaccoltaNonScritta, errors.New("buste: raccolta dei dettati non chiusa")
	}
	return esito, nil
}

// registraNelSavepoint è il corpo di record/3: interruttore, esclusione, unità del lotto, riga, spegnimento all'ultima.
func (b *Buste) registraNelSavepoint(ctx context.Context, tx esecutoreLettore, busta, cliente int64, d *Dettato, ora string) (EsitoRaccolta, error) {
	accesa, _, err := leggiImpostazione(ctx, tx, ChiaveRaccoltaAccesa)
	if err != nil {
		return 0, err
	}
	if accesa != "1" {
		return RaccoltaSpenta, nil
	}
	var escluso bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM dettati_esclusioni WHERE cliente_id = ?)`, cliente).Scan(&escluso); err != nil {
		return 0, err
	}
	if escluso {
		return RaccoltaEsclusa, nil
	}
	// Un'unità del lotto, o nessuna: la guardia è nel WHERE (spend/1). CAST come lo standalone: un valore che non è un
	// numero vale 0, cioè lotto finito.
	r, err := tx.ExecContext(ctx, `UPDATE impostazioni SET valore = CAST(CAST(valore AS INTEGER) - 1 AS TEXT), aggiornata_il = ?
		WHERE chiave = ? AND CAST(valore AS INTEGER) > 0`, ora, ChiaveRaccoltaRimaste)
	if err != nil {
		return 0, err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n != 1 {
		// exhausted/1: l'interruttore acceso su un lotto finito si spegne adesso.
		return RaccoltaSpenta, b.spegniRaccolta(ctx, tx, ora)
	}
	var modello any
	if d.Modello != "" {
		modello = d.Modello
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO dettati_addestramento (busta_id, testo, proposta, modello, creato_il) VALUES (?, ?, ?, ?, ?)`,
		busta, d.Testo, string(d.Proposta), modello, ora); err != nil {
		return 0, err
	}
	rimaste, _, err := leggiImpostazione(ctx, tx, ChiaveRaccoltaRimaste)
	if err != nil {
		return 0, err
	}
	if rimaste == "0" {
		// L'ultima unità spegne subito, non al salvataggio dopo.
		if err := b.spegniRaccolta(ctx, tx, ora); err != nil {
			return 0, err
		}
	}
	return RaccoltaRegistrata, nil
}

// spegniRaccolta è switch_off/1: l'interruttore a «0» e la riga d'audit dell'attore «system», con soli numeri.
func (b *Buste) spegniRaccolta(ctx context.Context, tx esecutoreLettore, ora string) error {
	if err := b.salvaImpostazione(ctx, tx, ChiaveRaccoltaAccesa, "0", ora); err != nil {
		return err
	}
	return b.reg.Scrivi(ctx, tx, audit.Contesto{Attore: AttoreSistema}, "assist.training_collect", "assist", "training", "ok",
		map[string]any{"on": false, "remaining": 0})
}
