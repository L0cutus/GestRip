package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestApriImpostaWALEChiaviEsterne(t *testing.T) {
	d, err := Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Chiudi()
	var mode string
	if err := d.Lettura.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Errorf("journal_mode = %q, %v; atteso wal", mode, err)
	}
	var fk int
	if err := d.Scrittura.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Errorf("foreign_keys = %d, %v; atteso 1", fk, err)
	}
	if got := d.Scrittura.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("connessioni di scrittura = %d, attesa 1", got)
	}
}

// inventario: ADM-20
// La sonda esegue solo SELECT 1, entro 2 s, anche con il database occupato.
func TestPronto(t *testing.T) {
	d, err := Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	if !d.Pronto(context.Background()) {
		t.Fatal("database nuovo non pronto")
	}

	t.Run("lettori esauriti: no entro il tempo massimo", func(t *testing.T) {
		var conns []any
		for i := 0; i < d.Lettura.Stats().MaxOpenConnections; i++ {
			c, err := d.Lettura.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			conns = append(conns, c)
		}
		inizio := time.Now()
		if d.Pronto(context.Background()) {
			t.Error("pronto con tutte le connessioni occupate")
		}
		if dur := time.Since(inizio); dur > 3*time.Second || dur < TempoMassimoSonda-100*time.Millisecond {
			t.Errorf("risposta in %v, attesa circa %v e mai oltre 3 s", dur, TempoMassimoSonda)
		}
		for _, c := range conns {
			c.(interface{ Close() error }).Close()
		}
		if !d.Pronto(context.Background()) {
			t.Error("non torna pronto dopo aver liberato le connessioni")
		}
	})

	t.Run("database chiuso: no", func(t *testing.T) {
		d.Chiudi()
		if d.Pronto(context.Background()) {
			t.Error("pronto con il database chiuso")
		}
	})
}
