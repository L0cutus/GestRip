package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// L'ora del backup notturno scelta dal titolare nella pagina Backup (slice 133b, D-115: «alle 02:30 di default, con
// l'ora modificabile dal titolare»). L'ora è ChiaveOra («HH:MM»); il cambio lascia anche ChiaveCambioOra, che dice al
// calendario come passare dall'ora vecchia alla nuova senza un backup subito e senza buchi:
//
//   - **Coperto:** la prima ora nuova dopo il cambio (Previsto) cade al più LimiteDopoCambio (25 ore) dopo la cattura
//     dell'ultimo backup verificato sulla chiavetta. Allora i turni prima di quell'ora valgono fatti e il primo backup è
//     Previsto: l'allarme delle 26 ore non suona, perché il backup arriva (e si verifica) prima.
//   - **Non coperto** (Previsto più lontano, o nessun backup verificato): il turno dell'ora nuova di adesso è da fare e il
//     backup parte al prossimo giro, come un recupero. Così fra due backup non passano mai più di 26 ore per un cambio.
//   - In tutti e due i casi, per i turni da Dal in poi contano solo le esecuzioni iniziate dal cambio in poi (Il): un
//     backup fatto stanotte all'ora vecchia non chiude il turno dell'ora nuova di stasera.
//
// La regola tocca solo il passaggio: senza cambi, o per i turni prima di Dal, il calendario è quello della 124.
// L'allarme non cambia: suona sempre oltre le 26 ore dall'ultimo backup verificato (revisione della 133b).

// ChiaveCambioOra è la chiave di impostazioni, solo del Go, dell'ultimo cambio dell'ora del backup (CambioOra in JSON).
const ChiaveCambioOra = "backup.ora_cambiata"

// TempoPerIlBackup è il tempo che si lascia al primo backup all'ora nuova per partire (un giro al minuto), cifrare e
// verificarsi prima delle 26 ore dell'allarme.
const TempoPerIlBackup = time.Hour

// LimiteDopoCambio è la distanza massima fra la cattura dell'ultimo backup verificato e la prima ora nuova perché il
// cambio sia coperto: le 26 ore dell'allarme meno TempoPerIlBackup.
const LimiteDopoCambio = EtaMassimaBackup - TempoPerIlBackup

// ErrOraNonValida: l'ora mandata dalla pagina non è «HH:MM» fra 00:00 e 23:59.
var ErrOraNonValida = errors.New("backup: ora del backup non valida")

// CambioOra è l'ultimo cambio dell'ora del backup.
type CambioOra struct {
	Il       time.Time  // quando è stata salvata l'ora nuova
	Dal      clock.Data // il primo turno dell'ora nuova; i turni prima valgono fatti
	Previsto time.Time  // il primo backup all'ora nuova se il cambio era coperto; zero se non coperto
}

// cambioJSON è CambioOra come sta nella riga.
type cambioJSON struct {
	Il       string `json:"il"`
	Dal      string `json:"dal"`
	Previsto string `json:"previsto,omitempty"`
}

// EsitoOra è l'esito di SalvaOra.
type EsitoOra struct {
	Ora      time.Duration // l'ora salvata (o quella che c'era già)
	Cambiata bool          // false: era già quella, niente scritto
	Subito   bool          // non coperto: il backup è da fare al prossimo giro
	Prossimo time.Time     // il primo backup previsto all'ora nuova (zero se Subito)
	// Acceso: il lavoro notturno gira (chiavetta configurata, lavori automatici accesi, non una copia isolata); se no
	// l'ora vale, ma nessun backup automatico parte.
	Acceso bool
}

// TestoOra è l'ora «HH:MM» (i secondi, che la pagina non scrive, non si mostrano).
func TestoOra(o time.Duration) string {
	return fmt.Sprintf("%02d:%02d", int(o/time.Hour), int(o%time.Hour/time.Minute))
}

// leggiOraModulo legge l'ora della pagina: esattamente «HH:MM» con quattro cifre, 00:00…23:59 (il campo type="time"
// la manda così; leggiOraBackup da solo accetterebbe anche «+2:30»).
func leggiOraModulo(v string) (time.Duration, bool) {
	if len(v) != 5 || v[2] != ':' {
		return 0, false
	}
	for i := range len(v) {
		if i != 2 && (v[i] < '0' || v[i] > '9') {
			return 0, false
		}
	}
	return leggiOraBackup(v)
}

// SalvaOra scrive l'ora del backup notturno scelta dal titolare («HH:MM») con il cambio (CambioOra) e la riga
// d'audit backup.schedule_updated, tutto in una transazione. La stessa ora di prima non scrive niente. Non fa
// partire niente: il backup lo fa il giro del lavoro notturno, se è acceso.
func (n *Notturno) SalvaOra(ctx context.Context, testo string, a audit.Contesto) (EsitoOra, error) {
	nuova, ok := leggiOraModulo(testo)
	if !ok {
		return EsitoOra{}, ErrOraNonValida
	}
	tx, err := n.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return EsitoOra{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	vecchia := OraPredefinita
	var v string
	switch err := tx.QueryRowContext(ctx, `SELECT valore FROM impostazioni WHERE chiave = ?`, ChiaveOra).Scan(&v); {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return EsitoOra{}, err
	default:
		if o, ok := leggiOraBackup(v); ok {
			vecchia = o
		}
	}
	adesso := n.o.Adesso()
	if nuova == vecchia {
		return EsitoOra{Ora: nuova, Prossimo: ProssimoGiro(adesso, nuova), Acceso: n.Configurato() && !n.lavoroSpento}, nil
	}
	ultimo, ce, err := db.UltimoVerificatoSullaChiavetta(ctx, tx)
	if err != nil {
		return EsitoOra{}, err
	}
	c := CambioOra{Il: adesso}
	if previsto := ProssimoGiro(adesso, nuova); ce && previsto.Sub(ultimo.CatturatoIl) <= LimiteDopoCambio {
		c.Previsto = previsto
		c.Dal = Turno(c.Previsto, nuova)
	} else {
		c.Dal = Turno(adesso, nuova)
	}
	j := cambioJSON{Il: c.Il.UTC().Format(db.FormatoIstante), Dal: c.Dal.String()}
	if !c.Previsto.IsZero() {
		j.Previsto = c.Previsto.UTC().Format(db.FormatoIstante)
	}
	jb, err := json.Marshal(j)
	if err != nil {
		return EsitoOra{}, err
	}
	quando := adesso.UTC().Format(db.FormatoIstante)
	for _, kv := range [][2]string{{ChiaveOra, TestoOra(nuova)}, {ChiaveCambioOra, string(jb)}} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, ?)
			ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore, aggiornata_il = excluded.aggiornata_il`, kv[0], kv[1], quando); err != nil {
			return EsitoOra{}, fmt.Errorf("backup: impostazione %s: %w", kv[0], err)
		}
	}
	dati := map[string]any{"from": TestoOra(vecchia), "to": TestoOra(nuova), "first_turn": c.Dal.String(), "due_now": c.Previsto.IsZero()}
	if err := audit.Nuovo(n.d, n.o).Scrivi(ctx, tx, a, "backup.schedule_updated", "backup", "ora", "ok", dati); err != nil {
		return EsitoOra{}, err
	}
	if err := tx.Commit(); err != nil {
		return EsitoOra{}, err
	}
	n.dimenticaTentativiBackup()
	return EsitoOra{Ora: nuova, Cambiata: true, Subito: c.Previsto.IsZero(), Prossimo: c.Previsto,
		Acceso: n.Configurato() && !n.lavoroSpento}, nil
}

// cambioOra legge l'ultimo cambio dell'ora. Senza la riga, o con una riga che non si legge, false: il calendario è
// quello della 124 (una riga rovinata non deve fermare i backup).
func cambioOra(ctx context.Context, q db.Interrogatore) (CambioOra, bool, error) {
	var v string
	err := q.QueryRowContext(ctx, `SELECT valore FROM impostazioni WHERE chiave = ?`, ChiaveCambioOra).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return CambioOra{}, false, nil
	}
	if err != nil {
		return CambioOra{}, false, err
	}
	var j cambioJSON
	if json.Unmarshal([]byte(v), &j) != nil {
		return CambioOra{}, false, nil
	}
	var c CambioOra
	il, err1 := time.Parse(db.FormatoIstante, j.Il)
	dal, err2 := time.Parse(time.DateOnly, j.Dal)
	if err1 != nil || err2 != nil {
		return CambioOra{}, false, nil
	}
	c.Il, c.Dal = il, clock.Data{Anno: dal.Year(), Mese: dal.Month(), Giorno: dal.Day()}
	if j.Previsto != "" {
		if c.Previsto, err = time.Parse(db.FormatoIstante, j.Previsto); err != nil {
			return CambioOra{}, false, nil
		}
	}
	return c, true, nil
}

// prima dice se il turno a viene prima del turno b.
func prima(a, b clock.Data) bool { return a.String() < b.String() }

// dimenticaTentativiBackup toglie i tentativi di backup contati solo in memoria (righe dello storico non scritte): sono
// di prima del cambio dell'ora e non contano per l'ora nuova. Quelli delle prove restano.
func (n *Notturno) dimenticaTentativiBackup() {
	n.muNonScritte.Lock()
	defer n.muNonScritte.Unlock()
	for k := range n.nonScritte {
		if !strings.HasPrefix(k, "prova ") {
			delete(n.nonScritte, k)
		}
	}
}
