// Package db apre il database SQLite (modernc.org/sqlite, Go puro, niente CGO).
//
// Due insiemi di connessioni: uno solo per scrivere (le scritture non si pestano i piedi e
// SQLite ha comunque un solo scrittore) e alcuni per leggere, in WAL.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"time"

	_ "modernc.org/sqlite" // driver "sqlite"
)

// TempoMassimoSonda è il tempo massimo della sonda di salute (ADM-20: 2 s).
const TempoMassimoSonda = 2 * time.Second

// LettoriMassimi è il numero di connessioni di sola lettura.
const LettoriMassimi = 4

// DB tiene le due vie verso lo stesso file.
type DB struct {
	Scrittura *sql.DB
	Lettura   *sql.DB
}

func dsn(percorso string, sola bool) string {
	q := url.Values{}
	for _, p := range []string{"journal_mode(WAL)", "foreign_keys(1)", "busy_timeout(5000)", "synchronous(NORMAL)"} {
		q.Add("_pragma", p)
	}
	if sola {
		q.Set("mode", "ro")
	} else {
		q.Set("_txlock", "immediate")
	}
	return "file:" + percorso + "?" + q.Encode()
}

// Apri apre (e se serve crea) il database in percorso.
func Apri(percorso string) (*DB, error) {
	w, err := sql.Open("sqlite", dsn(percorso, false))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	w.SetConnMaxIdleTime(0)
	if err := w.Ping(); err != nil {
		w.Close()
		return nil, fmt.Errorf("apertura di %s: %w", percorso, err)
	}
	r, err := sql.Open("sqlite", dsn(percorso, true))
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(LettoriMassimi)
	if err := r.Ping(); err != nil {
		w.Close()
		r.Close()
		return nil, fmt.Errorf("apertura in lettura di %s: %w", percorso, err)
	}
	return &DB{Scrittura: w, Lettura: r}, nil
}

// Chiudi chiude le due vie.
func (d *DB) Chiudi() error {
	errW := d.Scrittura.Close()
	errR := d.Lettura.Close()
	if errW != nil {
		return errW
	}
	return errR
}

// Pronto è la sonda di salute (ADM-20): solo `SELECT 1` su una connessione di lettura, nessun
// dato del negozio, nessuna scrittura. Con le connessioni tutte occupate o il database
// chiuso risponde no entro TempoMassimoSonda invece di bloccarsi.
func (d *DB) Pronto(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, TempoMassimoSonda)
	defer cancel()
	var uno int
	if err := d.Lettura.QueryRowContext(ctx, "SELECT 1").Scan(&uno); err != nil {
		return false
	}
	return uno == 1
}
