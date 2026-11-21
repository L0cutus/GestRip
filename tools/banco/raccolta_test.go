package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La raccolta accesa dal banco (slice 113): le due chiavi dello standalone, con una busta rimasta, anche sopra valori già
// importati; un database che non c'è è un errore (niente database nuovo vuoto).
func TestSeminaRaccolta(t *testing.T) {
	p := filepath.Join(t.TempDir(), "gestrip.db")
	if err := seminaRaccoltaGo(p); err == nil {
		t.Fatal("database assente accettato")
	}
	d, err := db.Apri(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Migra(context.Background(), clock.Sistema{}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.training.collect', '0', '2026-01-01T00:00:00.000000Z')`); err != nil {
		t.Fatal(err)
	}
	_ = d.Chiudi()
	if err := seminaRaccoltaGo(p); err != nil {
		t.Fatal(err)
	}
	d, err = db.Apri(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Chiudi() }()
	var v string
	if err := d.Lettura.QueryRow(`SELECT group_concat(chiave || '=' || valore, ',') FROM (SELECT * FROM impostazioni WHERE chiave LIKE 'assist.training.%' ORDER BY chiave)`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != "assist.training.collect=1,assist.training.remaining=1" {
		t.Errorf("impostazioni %q", v)
	}
	for _, s := range []string{`Settings.put("assist.training.collect", "1")`, `Settings.put("assist.training.remaining", "1")`} {
		if !strings.Contains(seminaRaccoltaStandalone, s) {
			t.Errorf("manca %s", s)
		}
	}
}
