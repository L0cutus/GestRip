// Package db apre il database SQLite (modernc.org/sqlite, Go puro, niente CGO).
//
// Due insiemi di connessioni: uno solo per scrivere (le scritture non si pestano i piedi e
// SQLite ha comunque un solo scrittore) e alcuni per leggere, in WAL.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
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
	percorso  string
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

// Apri apre (e se serve crea) il database in percorso, privato (0600, OPS-12).
func Apri(percorso string) (*DB, error) {
	if err := privato(percorso); err != nil {
		return nil, err
	}
	w, err := sql.Open("sqlite", dsn(percorso, false))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	w.SetConnMaxIdleTime(0)
	if err := w.Ping(); err != nil {
		return nil, errors.Join(fmt.Errorf("apertura di %s: %w", percorso, err), w.Close())
	}
	r, err := sql.Open("sqlite", dsn(percorso, true))
	if err != nil {
		return nil, errors.Join(err, w.Close())
	}
	r.SetMaxOpenConns(LettoriMassimi)
	if err := r.Ping(); err != nil {
		return nil, errors.Join(fmt.Errorf("apertura in lettura di %s: %w", percorso, err), w.Close(), r.Close())
	}
	return &DB{Scrittura: w, Lettura: r, percorso: percorso}, nil
}

// privato crea il file del database se manca e lo mette a 0600 prima che SQLite lo apra, come
// prepare! dello standalone a ogni avvio (lì con chmod dopo l'apertura). SQLite crea il -wal e lo
// -shm con i permessi del database, quindi nascono 0600; quelli rimasti da prima si rimettono a
// 0600 qui. Il database, il -wal e lo -shm devono essere file regolari e non link: con un link
// (anche pendente, che O_CREATE farebbe nascere altrove) i dati finirebbero fuori dalla cartella
// controllata sul volume cifrato. Nello standalone un database link era accettato. L'apertura non
// resta appesa su una FIFO.
func privato(percorso string) error {
	for _, accanto := range []string{percorso, percorso + "-wal", percorso + "-shm"} {
		if fi, err := os.Lstat(accanto); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("database %s: è un link, deve essere il file stesso", accanto)
		}
	}
	f, err := os.OpenFile(percorso, os.O_RDONLY|os.O_CREATE|aperturaPrivata, 0o600)
	if err != nil {
		return fmt.Errorf("apertura di %s: %w", percorso, err)
	}
	fi, err := f.Stat()
	if err == nil && !fi.Mode().IsRegular() {
		err = errors.New("non è un file regolare")
	}
	if err == nil {
		err = f.Chmod(0o600)
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return fmt.Errorf("database %s: %w", percorso, err)
	}
	for _, accanto := range []string{percorso + "-wal", percorso + "-shm"} {
		fi, err := os.Lstat(accanto)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err == nil && !fi.Mode().IsRegular():
			err = errors.New("non è un file regolare (un link non vale)")
		case err == nil && fi.Mode().Perm() != 0o600:
			err = os.Chmod(accanto, 0o600)
		}
		if err != nil {
			return fmt.Errorf("database %s: %w", accanto, err)
		}
	}
	return nil
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
