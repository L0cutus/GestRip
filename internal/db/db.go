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
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // driver "sqlite"
)

// TempoMassimoSonda è il tempo massimo della sonda di salute (ADM-20: 2 s).
const TempoMassimoSonda = 2 * time.Second

// TempoMassimoCheckpoint è quanto Chiudi aspetta la connessione dello scrittore per il checkpoint.
const TempoMassimoCheckpoint = 2 * time.Second

// istruzioneCheckpoint è una variabile solo perché i test possano provocare un errore vero.
var istruzioneCheckpoint = `PRAGMA wal_checkpoint(PASSIVE)`

// LettoriMassimi è il numero di connessioni di sola lettura.
const LettoriMassimi = 4

// DB tiene le due vie verso lo stesso file.
type DB struct {
	Scrittura *sql.DB
	Lettura   *sql.DB
	percorso  string

	chiusura    sync.Once
	errChiusura error
}

// URIDi è il percorso come URI di SQLite ("file:…", senza parametri), perché il nome arrivi intatto:
// %, ? e # si codificano (SQLite decodifica i %XX e finisce il nome al primo ? o #) e un percorso
// assoluto prende l'autorità vuota, così uno che comincia con due barre non si legge come
// un'autorità. Spazi e lettere non ASCII SQLite li prende come sono. Tutti i collegamenti con un
// percorso passano di qui: database, backup, sorgente e database nuovo dell'import.
func URIDi(percorso string) string {
	p := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(percorso)
	if strings.HasPrefix(p, "/") {
		return "file://" + p
	}
	return "file:" + p
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
	return URIDi(percorso) + "?" + q.Encode()
}

// Apri apre (e se serve crea) il database in percorso, privato (0600, OPS-12).
func Apri(percorso string) (*DB, error) {
	// Assoluto prima di tutto: un relativo che comincia con «file:» o è «:memory:» SQLite lo
	// leggerebbe come URI o come database in memoria, anche nel VACUUM INTO del backup.
	percorso, err := filepath.Abs(percorso)
	if err != nil {
		return nil, err
	}
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

// Chiudi chiude prima i lettori e poi lo scrittore, che fa il checkpoint del WAL; quando lo
// scrittore è l'ultima connessione sul file, SQLite alla chiusura porta il resto del WAL nel file
// principale e toglie -wal e -shm. L'ordine conta: i lettori sono in sola lettura e, se l'ultima
// connessione a chiudersi è una di loro, il checkpoint non si fa e -wal e -shm restano pieni
// accanto (misurato il 31/10/2026). Va chiamata senza query in corso: sql.DB.Close chiude solo le
// connessioni libere, e una in uso si chiude quando torna libera.
//
// Il checkpoint esplicito serve a riportare un suo errore vero (I/O, disco pieno), che la sola
// chiusura di SQLite tacerebbe. È PASSIVE: non chiama il busy handler, quindi non aspetta i
// busy_timeout quando il file è occupato. Non sono errori, e i dati restano al sicuro nel WAL (si
// rileggono a ogni apertura, li porta nel file chi chiude per ultimo in scrittura):
//   - un checkpoint occupato o parziale: un'altra connessione sul file (un altro comando gestrip,
//     un altro processo) o un nostro lettore ancora in uso; riportarlo farebbe fallire un comando
//     che ha fatto il suo lavoro;
//   - la connessione dello scrittore che non si libera entro TempoMassimoCheckpoint (una
//     transazione ancora aperta, in serve dopo uno Shutdown scaduto): senza scadenza lo
//     spegnimento resterebbe fermo fino al kill.
//
// Chi deve avere il file principale completo e da solo (import, backup) lo controlla per conto
// suo. I lettori e lo scrittore si chiudono comunque. Chiudi è idempotente: le chiamate dopo la
// prima restituiscono lo stesso esito, così un errore della chiusura non sparisce a un secondo
// controllo.
func (d *DB) Chiudi() error {
	d.chiusura.Do(func() {
		errR := d.Lettura.Close()
		errC := checkpoint(d.Scrittura)
		errW := d.Scrittura.Close()
		d.errChiusura = errors.Join(errR, errC, errW)
	})
	return d.errChiusura
}

func checkpoint(w *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), TempoMassimoCheckpoint)
	defer cancel()
	var occupato, pagine, copiate int
	err := w.QueryRowContext(ctx, istruzioneCheckpoint).Scan(&occupato, &pagine, &copiate)
	if errors.Is(err, context.DeadlineExceeded) {
		return nil // vedi Chiudi: lo scrittore è occupato, i dati sono nel WAL
	}
	if err != nil {
		return fmt.Errorf("checkpoint del WAL: %w", err)
	}
	return nil // occupato != 0 o pagine non copiate: vedi Chiudi
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
