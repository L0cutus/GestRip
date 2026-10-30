package audit

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/db"
)

// La pagina del registro (ADM-16), come AuditLog.load/3 dello standalone: 100 eventi per pagina
// dal più recente, filtri esatti su attore e azione, periodo in giorni (default 30, massimo 365),
// pagine fotografate su un id massimo (fino_a). L'apertura si scrive come audit.viewed nella
// stessa transazione della lettura, dopo aver letto le righe: la pagina non mostra sé stessa.

const (
	perPagina         = 100
	giorniPredefiniti = 30
	giorniMassimi     = 365
)

// Filtri sono i filtri della pagina del registro, già ripuliti.
type Filtri struct {
	Attore, Azione string
	Giorni         int
}

// Richiesta sono i valori grezzi dell'indirizzo: actor, action, giorni, pagina, fino_a.
type Richiesta struct {
	Attore, Azione, Giorni, Pagina, FinoA string
}

// Evento è una riga del registro come la mostra la pagina.
type Evento struct {
	ID                                        int64
	Quando                                    time.Time
	Attore, Azione, Risorsa, IDRisorsa, Esito string
}

// Pagina è una pagina del registro, come la mappa di AuditLog.load/3.
type Pagina struct {
	Eventi                 []Evento
	Totale, Pagina, Pagine int
	FinoA                  int64
	Attori, Azioni         []string
	Filtri                 Filtri
}

// Consulta legge una pagina del registro e scrive la riga dell'apertura con l'attore di c; se la
// riga non si scrive (senza attore) la lettura non dà niente.
func (r *Registro) Consulta(ctx context.Context, q Richiesta, c Contesto) (p Pagina, err error) {
	p.Filtri = Filtri{Attore: strings.TrimSpace(q.Attore), Azione: strings.TrimSpace(q.Azione),
		Giorni: int(min(interoPositivo(q.Giorni, giorniPredefiniti), giorniMassimi))}
	adesso := r.o.Adesso()
	tx, err := r.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return Pagina{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
			p = Pagina{}
		}
	}()
	var ultimo int64
	if err = tx.QueryRowContext(ctx, `SELECT coalesce(max(id), 0) FROM eventi_audit`).Scan(&ultimo); err != nil {
		return
	}
	p.FinoA = min(interoPositivo(q.FinoA, ultimo), ultimo)

	dove := ` FROM eventi_audit WHERE creato_il >= ? AND id <= ?`
	arg := []any{adesso.Add(-time.Duration(p.Filtri.Giorni) * 24 * time.Hour).UTC().Format(db.FormatoIstante), p.FinoA}
	if p.Filtri.Attore != "" {
		dove += ` AND attore = ?`
		arg = append(arg, p.Filtri.Attore)
	}
	if p.Filtri.Azione != "" {
		dove += ` AND azione = ?`
		arg = append(arg, p.Filtri.Azione)
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*)`+dove, arg...).Scan(&p.Totale); err != nil {
		return
	}
	p.Pagine = max((p.Totale+perPagina-1)/perPagina, 1)
	p.Pagina = int(min(interoPositivo(q.Pagina, 1), int64(p.Pagine)))
	if p.Eventi, err = eventi(ctx, tx, `SELECT id, creato_il, attore, azione, risorsa, id_risorsa, esito`+dove+
		` ORDER BY creato_il DESC, id DESC LIMIT ? OFFSET ?`, append(arg, perPagina, (p.Pagina-1)*perPagina)...); err != nil {
		return
	}
	if p.Attori, err = distinti(ctx, tx, "attore"); err != nil {
		return
	}
	if p.Azioni, err = distinti(ctx, tx, "azione"); err != nil {
		return
	}
	if err = r.Scrivi(ctx, tx, Contesto{Attore: c.Attore}, "audit.viewed", "audit", "global", "ok",
		map[string]any{"filters": chiaviAttive(p.Filtri)}); err != nil {
		return
	}
	err = tx.Commit()
	return
}

func eventi(ctx context.Context, tx *sql.Tx, q string, arg ...any) ([]Evento, error) {
	righe, err := tx.QueryContext(ctx, q, arg...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = righe.Close() }()
	var ee []Evento
	for righe.Next() {
		var e Evento
		var quando string
		if err := righe.Scan(&e.ID, &quando, &e.Attore, &e.Azione, &e.Risorsa, &e.IDRisorsa, &e.Esito); err != nil {
			return nil, err
		}
		if e.Quando, err = time.Parse(db.FormatoIstante, quando); err != nil {
			return nil, fmt.Errorf("audit: istante %d: %w", e.ID, err)
		}
		ee = append(ee, e)
	}
	return ee, righe.Err()
}

// distinti sono i valori presenti di una colonna, in ordine: le scelte dei filtri.
func distinti(ctx context.Context, tx *sql.Tx, colonna string) ([]string, error) {
	righe, err := tx.QueryContext(ctx, `SELECT DISTINCT `+colonna+` FROM eventi_audit ORDER BY `+colonna)
	if err != nil {
		return nil, err
	}
	defer func() { _ = righe.Close() }()
	var vv []string
	for righe.Next() {
		var v string
		if err := righe.Scan(&v); err != nil {
			return nil, err
		}
		vv = append(vv, v)
	}
	return vv, righe.Err()
}

// chiaviAttive sono i nomi dei filtri usati, come active_filter_keys/1: actor e action se ci sono,
// days sempre.
func chiaviAttive(f Filtri) []string {
	var kk []string
	if f.Attore != "" {
		kk = append(kk, "actor")
	}
	if f.Azione != "" {
		kk = append(kk, "action")
	}
	return append(kk, "days")
}

// interoPositivo è positive_integer/2 con Integer.parse: un segno facoltativo e solo cifre; se il
// valore non è maggiore di zero vale predefinito. Un numero oltre int64 resta il più grande (in
// Elixir è un intero grande, poi limitato dal chiamante).
func interoPositivo(s string, predefinito int64) int64 {
	cifre := strings.TrimPrefix(s, "+") // un «-» non è una cifra: negativo vuol dire predefinito
	if cifre == "" {
		return predefinito
	}
	var n int64
	for _, c := range cifre {
		if c < '0' || c > '9' {
			return predefinito
		}
		if n > (math.MaxInt64-int64(c-'0'))/10 {
			n = math.MaxInt64
			continue
		}
		n = n*10 + int64(c-'0')
	}
	if n == 0 {
		return predefinito
	}
	return n
}
