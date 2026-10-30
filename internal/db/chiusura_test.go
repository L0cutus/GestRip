package db

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Dopo Chiudi i dati stanno nel file principale e accanto non resta il WAL: lo dicono serve
// (spegnimento), i backup e l'import.
func TestChiudiPortaIlWALNelFile(t *testing.T) {
	percorso := filepath.Join(t.TempDir(), "gestrip.db")
	d, err := Apri(percorso)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Scrittura.Exec(`CREATE TABLE prova (v TEXT); INSERT INTO prova VALUES ('sintetico')`); err != nil {
		t.Fatal(err)
	}
	// I lettori aperti e usati, come in serve.
	var n int
	if err := d.Lettura.QueryRow(`SELECT count(*) FROM prova`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("lettura = %d, %v", n, err)
	}
	if err := d.Chiudi(); err != nil {
		t.Fatalf("Chiudi: %v", err)
	}
	for _, s := range []string{"-wal", "-shm"} {
		fi, err := os.Stat(percorso + s)
		if err == nil && fi.Size() > 0 {
			t.Errorf("dopo Chiudi resta %s di %d byte", s, fi.Size())
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Error(err)
		}
	}
	// immutable=1: si legge il solo file principale, senza guardare WAL e shm.
	c, err := sql.Open("sqlite", "file:"+percorso+"?mode=ro&immutable=1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }() // sola lettura
	var v string
	if err := c.QueryRow(`SELECT v FROM prova`).Scan(&v); err != nil || v != "sintetico" {
		t.Errorf("file principale da solo: %q, %v; atteso il dato scritto", v, err)
	}
}

// Un'altra connessione che tiene aperta una lettura impedisce il checkpoint completo: Chiudi non
// lo tratta come un errore, non aspetta, chiude lo scrittore e i dati restano nel WAL, non persi; la chiusura
// successiva, da sola sul file, li porta nel file principale.
func TestChiudiConUnAltroLettoreAperto(t *testing.T) {
	percorso := filepath.Join(t.TempDir(), "gestrip.db")
	d, err := Apri(percorso)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Scrittura.Exec(`CREATE TABLE prova (v TEXT); INSERT INTO prova VALUES ('sintetico')`); err != nil {
		t.Fatal(err)
	}
	altro, err := sql.Open("sqlite", "file:"+percorso+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = altro.Close() }() // sola lettura
	ctx := context.Background()
	tx, err := altro.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }() // sola lettura
	var n int
	if err := tx.QueryRow(`SELECT count(*) FROM prova`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	// Una scrittura dopo l'istantanea dell'altro lettore: il WAL non si può azzerare.
	if _, err := d.Scrittura.Exec(`INSERT INTO prova VALUES ('dopo')`); err != nil {
		t.Fatal(err)
	}
	inizio := time.Now()
	if err := d.Chiudi(); err != nil {
		t.Errorf("Chiudi con un altro lettore aperto: %v", err)
	}
	if dur := time.Since(inizio); dur > time.Second {
		t.Errorf("Chiudi in %v con un altro lettore aperto; attesa sotto 1 s", dur)
	}
	if d.Scrittura.Ping() == nil {
		t.Error("lo scrittore resta aperto dopo un checkpoint occupato")
	}
	if fi, err := os.Stat(percorso + "-wal"); err != nil || fi.Size() == 0 {
		t.Fatalf("il WAL doveva restare con i dati: %v, %v", fi, err)
	}
	if err := errors.Join(tx.Rollback(), altro.Close()); err != nil {
		t.Fatal(err)
	}
	d, err = Apri(percorso)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Lettura.QueryRow(`SELECT count(*) FROM prova`).Scan(&n); err != nil || n != 2 {
		t.Errorf("dopo la riapertura %d righe, %v; attese 2", n, err)
	}
	if err := d.Chiudi(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(percorso + "-wal"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("dopo la chiusura da sola il WAL resta: %v", err)
	}
}

// Una transazione aperta su Scrittura in un'altra parte del programma (in serve: dopo uno
// Shutdown scaduto) non ferma Chiudi senza limite: il checkpoint rinuncia dopo la sua scadenza,
// lo scrittore si chiude lo stesso e i dati della transazione, confermata dopo, non si perdono.
func TestChiudiConUnaScritturaInCorso(t *testing.T) {
	percorso := filepath.Join(t.TempDir(), "gestrip.db")
	d, err := Apri(percorso)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Scrittura.Exec(`CREATE TABLE prova (v TEXT)`); err != nil {
		t.Fatal(err)
	}
	tx, err := d.Scrittura.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO prova VALUES ('in corso')`); err != nil {
		t.Fatal(err)
	}
	esito := make(chan error, 1)
	inizio := time.Now()
	go func() { esito <- d.Chiudi() }()
	bloccata := false
	select {
	case err := <-esito:
		if err != nil {
			t.Errorf("Chiudi con una scrittura in corso: %v", err)
		}
		t.Logf("Chiudi in %v", time.Since(inizio))
	case <-time.After(4 * time.Second):
		t.Error("Chiudi bloccata oltre 4 s da una transazione aperta su Scrittura")
		bloccata = true
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit dopo Chiudi: %v", err)
	}
	if bloccata {
		t.Logf("Chiudi dopo il commit: %v", <-esito) // si sblocca con il commit
	}
	d, err = Apri(percorso)
	if err != nil {
		t.Fatal(err)
	}
	defer chiudi(t, d)
	var n int
	if err := d.Lettura.QueryRow(`SELECT count(*) FROM prova`).Scan(&n); err != nil || n != 1 {
		t.Errorf("dopo la riapertura %d righe, %v; attesa 1", n, err)
	}
}

// Un rows di Lettura ancora aperto (un nostro lettore in uso: sql.DB.Close chiude solo le
// connessioni libere) non fa aspettare Chiudi: il checkpoint PASSIVE non chiama il busy handler.
// Nessun errore, e i dati si rileggono riaprendo.
func TestChiudiConUnaLetturaAperta(t *testing.T) {
	percorso := filepath.Join(t.TempDir(), "gestrip.db")
	d, err := Apri(percorso)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Scrittura.Exec(`CREATE TABLE prova (v TEXT); INSERT INTO prova VALUES ('a'), ('b'), ('c')`); err != nil {
		t.Fatal(err)
	}
	rows, err := d.Lettura.Query(`SELECT v FROM prova`)
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatal(rows.Err())
	}
	if _, err := d.Scrittura.Exec(`INSERT INTO prova VALUES ('dopo')`); err != nil {
		t.Fatal(err)
	}
	inizio := time.Now()
	err = d.Chiudi()
	dur := time.Since(inizio)
	t.Logf("Chiudi in %v: %v", dur, err)
	if err != nil {
		t.Errorf("Chiudi con una lettura aperta: %v", err)
	}
	if dur > time.Second {
		t.Errorf("Chiudi in %v con una lettura aperta; attesa sotto 1 s", dur)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	d, err = Apri(percorso)
	if err != nil {
		t.Fatal(err)
	}
	defer chiudi(t, d)
	var n int
	if err := d.Lettura.QueryRow(`SELECT count(*) FROM prova`).Scan(&n); err != nil || n != 4 {
		t.Errorf("dopo la riapertura %d righe, %v; attese 4", n, err)
	}
}

// Il checkpoint gira e un suo errore vero risale, con lo scrittore chiuso lo stesso. L'errore si
// provoca con l'aggancio istruzioneCheckpoint (un'istruzione che SQLite rifiuta). Chiudi è
// idempotente: la seconda chiamata dà lo stesso esito della prima.
func TestChiudiRiportaErroreDelCheckpoint(t *testing.T) {
	d, err := Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	prima := istruzioneCheckpoint
	istruzioneCheckpoint = `PRAGMA wal_checkpoint(PASSIVE`
	t.Cleanup(func() { istruzioneCheckpoint = prima })
	err = d.Chiudi()
	if err == nil || !strings.Contains(err.Error(), "checkpoint del WAL") {
		t.Errorf("Chiudi = %v; atteso l'errore del checkpoint", err)
	}
	if d.Scrittura.Ping() == nil {
		t.Error("lo scrittore resta aperto dopo un errore del checkpoint")
	}
	if d.Lettura.Ping() == nil {
		t.Error("i lettori restano aperti dopo un errore del checkpoint")
	}
	if err2 := d.Chiudi(); err2 != err {
		t.Errorf("seconda Chiudi = %v; atteso lo stesso esito della prima (%v)", err2, err)
	}
}

// Chiudere due volte un database chiuso bene non dà errore.
func TestChiudiDueVolte(t *testing.T) {
	d, err := Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	chiudi(t, d)
	if err := d.Chiudi(); err != nil {
		t.Errorf("seconda Chiudi = %v; atteso nil", err)
	}
}
