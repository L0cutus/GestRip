package importer

import (
	"database/sql"
	"testing"
)

// La stima dei tempi dopo un import (ADM-04, RIP-H-55; D9 dello standalone: «spenta finché il titolare non la accende,
// anche dopo un import»). L'import copia le impostazioni dello standalone come sono (le chiavi stima_tempi_* sono dello
// standalone, nessuna è solo del Go): non ne scrive di sue, quindi con la sorgente che non le ha la stima resta spenta,
// e con la sorgente dove il titolare l'ha accesa o spenta le porta com'erano.

func chiaviStimaImportate(t *testing.T, o Opzioni) map[string]string {
	t.Helper()
	righe, err := apriLettura(t, o.Destinazione).Query(`SELECT chiave, valore FROM impostazioni WHERE chiave LIKE 'stima\_tempi\_%' ESCAPE '\'`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = righe.Close() }()
	got := map[string]string{}
	for righe.Next() {
		var k, v string
		if err := righe.Scan(&k, &v); err != nil {
			t.Fatal(err)
		}
		got[k] = v
	}
	if err := righe.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

// inventario: ADM-04
// Una sorgente senza le chiavi della stima (il caso di un'installazione mai passata dalla pagina del negozio, e dei dati
// che vengono dal Flask): dopo l'import non c'è nessuna riga stima_tempi_*, quindi l'interruttore è spento (qualunque
// valore diverso da «1», o nessuno, è spento) e il supplemento ha i valori di partenza.
func TestImportNonAccendeLaStima(t *testing.T) {
	_, o := preparaImport(t, nil)
	importa(t, o)
	if got := chiaviStimaImportate(t, o); len(got) != 0 {
		t.Fatalf("l'import ha scritto la stima: %v", got)
	}
}

// inventario: ADM-04
// Una sorgente dove il titolare ha scelto (acceso con un supplemento suo, o spento): l'import porta quelle righe com'erano,
// senza accenderle né cambiarle.
func TestImportPortaLaStimaComeEra(t *testing.T) {
	for nome, attesa := range map[string]map[string]string{
		"acceso": {"stima_tempi_attiva": "1", "stima_tempi_artigiano_min": "10", "stima_tempi_artigiano_max": "12"},
		"spento": {"stima_tempi_attiva": "0", "stima_tempi_artigiano_min": "7", "stima_tempi_artigiano_max": "8"},
		"strano": {"stima_tempi_attiva": "true"},
	} {
		t.Run(nome, func(t *testing.T) {
			_, o := preparaImport(t, func(c *sql.DB) {
				for k, v := range attesa {
					esegui(c, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, '2026-01-01T00:00:00.000000Z')`, k, v)
				}
			})
			importa(t, o)
			got := chiaviStimaImportate(t, o)
			if len(got) != len(attesa) {
				t.Fatalf("righe %v, attese %v", got, attesa)
			}
			for k, v := range attesa {
				if got[k] != v {
					t.Errorf("%s = %q, atteso %q", k, got[k], v)
				}
			}
		})
	}
}
