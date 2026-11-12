// Package audit scrive il registro delle azioni (ADM-17), come Audit.insert_with_outcome/7 dello
// standalone: una riga per azione importante, quasi sempre nella stessa transazione della
// modifica, così se la riga non si scrive la modifica si annulla. Mai segreti o token nei dati.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Contesto è chi fa l'azione: l'email dell'utente, «public», «installer»…; ip e user agent quando
// servono.
type Contesto struct {
	Attore, IP, UserAgent string
}

// Esecutore è dove si scrive: la transazione della modifica o il collegamento di scrittura.
type Esecutore interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

// Registro scrive le righe con l'orologio del negozio.
type Registro struct {
	d *db.DB
	o clock.Orologio
}

// Nuovo dà il registro del database d.
func Nuovo(d *db.DB, o clock.Orologio) *Registro { return &Registro{d: d, o: o} }

// Scrivi aggiunge una riga con ex, di solito la transazione della modifica. id è l'id della
// risorsa, scritto come testo; dati nil vuol dire {}.
func (r *Registro) Scrivi(ctx context.Context, ex Esecutore, c Contesto, azione, risorsa string, id any, esito string, dati map[string]any) error {
	_, err := r.ScriviConID(ctx, ex, c, azione, risorsa, id, esito, dati)
	return err
}

// ScriviConID è Scrivi che dà anche l'id della riga scritta: lo vuole chi lega qualcosa a quella riga
// (il link di conferma dell'email porta l'id della richiesta, slice 070).
func (r *Registro) ScriviConID(ctx context.Context, ex Esecutore, c Contesto, azione, risorsa string, id any, esito string, dati map[string]any) (int64, error) {
	j := []byte("{}")
	if len(dati) > 0 {
		var err error
		if j, err = json.Marshal(dati); err != nil {
			return 0, fmt.Errorf("audit: dati di %s: %w", azione, err)
		}
	}
	res, err := ex.ExecContext(ctx, `INSERT INTO eventi_audit (attore, azione, risorsa, id_risorsa, esito, dati, ip, user_agent, creato_il) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Attore, azione, risorsa, fmt.Sprint(id), esito, string(j), nulla(c.IP), nulla(c.UserAgent),
		r.o.Adesso().UTC().Format(db.FormatoIstante))
	if err != nil {
		return 0, fmt.Errorf("audit: %s: %w", azione, err)
	}
	n, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("audit: %s: %w", azione, err)
	}
	return n, nil
}

// Registra aggiunge una riga da sola, fuori da una transazione (l'inizio di un blocco).
func (r *Registro) Registra(ctx context.Context, c Contesto, azione, risorsa string, id any, esito string, dati map[string]any) error {
	return r.Scrivi(ctx, r.d.Scrittura, c, azione, risorsa, id, esito, dati)
}

func nulla(s string) any {
	if s == "" {
		return nil
	}
	return s
}
