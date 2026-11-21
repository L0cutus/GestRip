package importer

import (
	"database/sql"
	"testing"
)

// inventario: RIP-H-48
// D-105 (slice 113): dopo l'import la raccolta dei dettati è spenta, qualunque cosa dicesse lo standalone; le buste
// rimaste del lotto si conservano, e l'istante della riga resta quello della sorgente. Senza la chiave nella sorgente
// l'import non ne scrive una.
func TestImportSpegneLaRaccolta(t *testing.T) {
	for nome, c := range map[string]struct {
		sorgente map[string]string
		attesa   map[string]string
	}{
		"accesa":    {map[string]string{"assist.training.collect": "1", "assist.training.remaining": "300"}, map[string]string{"assist.training.collect": "0", "assist.training.remaining": "300"}},
		"spenta":    {map[string]string{"assist.training.collect": "0", "assist.training.remaining": "0"}, map[string]string{"assist.training.collect": "0", "assist.training.remaining": "0"}},
		"mai usata": {map[string]string{}, map[string]string{}},
	} {
		t.Run(nome, func(t *testing.T) {
			_, o := preparaImport(t, func(db *sql.DB) {
				for k, v := range c.sorgente {
					esegui(db, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, '2026-01-01T00:00:00.000000Z')`, k, v)
				}
			})
			importa(t, o)
			righe, err := apriLettura(t, o.Destinazione).Query(`SELECT chiave, valore, aggiornata_il FROM impostazioni WHERE chiave LIKE 'assist.training.%'`)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = righe.Close() }()
			got := map[string]string{}
			for righe.Next() {
				var k, v, quando string
				if err := righe.Scan(&k, &v, &quando); err != nil {
					t.Fatal(err)
				}
				got[k] = v
				if quando != "2026-01-01T00:00:00.000000Z" {
					t.Errorf("%s aggiornata_il %s", k, quando)
				}
			}
			if len(got) != len(c.attesa) {
				t.Fatalf("righe %v, attese %v", got, c.attesa)
			}
			for k, v := range c.attesa {
				if got[k] != v {
					t.Errorf("%s = %q, atteso %q", k, got[k], v)
				}
			}
		})
	}
}
