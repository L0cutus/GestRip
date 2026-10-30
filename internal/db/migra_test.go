package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

var ctx = context.Background()

func orologio() *clock.Finto {
	return clock.NuovoFinto(time.Date(2026, 10, 30, 9, 15, 7, 0, time.UTC))
}

func apriProva(t *testing.T) (*DB, string) {
	t.Helper()
	dir := t.TempDir()
	d, err := Apri(filepath.Join(dir, "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { chiudi(t, d) })
	return d, dir
}

func file(m map[string]string) fstest.MapFS {
	f := fstest.MapFS{}
	for k, v := range m {
		f[k] = &fstest.MapFile{Data: []byte(v)}
	}
	return f
}

var dueMigrazioni = map[string]string{
	"0001_prima.sql":   "CREATE TABLE a (id INTEGER PRIMARY KEY) STRICT;\nINSERT INTO a VALUES (1);",
	"0002_seconda.sql": "CREATE TABLE b (id INTEGER PRIMARY KEY, a_id INTEGER NOT NULL REFERENCES a(id)) STRICT;",
}

func versioni(t *testing.T, d *DB) []string {
	t.Helper()
	if !tabella(t, d, "schema_migrations") {
		return nil
	}
	rows, err := d.Lettura.Query(`SELECT versione || ':' || nome || ':' || applicata_il FROM schema_migrations ORDER BY versione`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var v []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		v = append(v, s)
	}
	return v
}

func tabella(t *testing.T, d *DB, nome string) bool {
	t.Helper()
	var n int
	if err := d.Lettura.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = ?`, nome).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func backups(t *testing.T, dir string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(dir, "*.prima-di-*"))
	return m
}

func TestMigraDatabaseNuovo(t *testing.T) {
	d, dir := apriProva(t)
	app, err := d.migraDa(ctx, file(dueMigrazioni), orologio())
	if err != nil {
		t.Fatal(err)
	}
	if len(app) != 2 {
		t.Errorf("applicate %v, attese 1 e 2", app)
	}
	got := strings.Join(versioni(t, d), " ")
	if got != "1:prima:2026-10-30T09:15:07.000000Z 2:seconda:2026-10-30T09:15:07.000000Z" {
		t.Errorf("schema_migrations = %q", got)
	}
	if b := backups(t, dir); len(b) != 0 {
		t.Errorf("su un database nuovo nessun backup, trovati %v", b)
	}
	app, err = d.migraDa(ctx, file(dueMigrazioni), orologio())
	if err != nil || len(app) != 0 {
		t.Errorf("seconda volta: applicate %v, err %v; attese nessuna", app, err)
	}
}

func TestMigraFaIlBackupPrima(t *testing.T) {
	d, dir := apriProva(t)
	solo1 := file(map[string]string{"0001_prima.sql": dueMigrazioni["0001_prima.sql"]})
	if _, err := d.migraDa(ctx, solo1, orologio()); err != nil {
		t.Fatal(err)
	}
	if _, err := d.migraDa(ctx, file(dueMigrazioni), orologio()); err != nil {
		t.Fatal(err)
	}
	b := backups(t, dir)
	atteso := filepath.Join(dir, "gestrip.db.prima-di-0002-20261030T091507.000Z")
	if len(b) != 1 || b[0] != atteso {
		t.Fatalf("backup = %v, atteso %s", b, atteso)
	}
	// Il backup è il database com'era prima: versione 1, senza la tabella b.
	c, err := Apri(b[0])
	if err != nil {
		t.Fatal(err)
	}
	defer chiudi(t, c)
	if v := versioni(t, c); len(v) != 1 || tabella(t, c, "b") || !tabella(t, c, "a") {
		t.Errorf("il backup non è lo stato di prima: versioni %v", v)
	}
}

func TestMigraSbagliataNonLasciaNiente(t *testing.T) {
	casi := map[string]string{
		"errore SQL a metà": "CREATE TABLE c (id INTEGER PRIMARY KEY) STRICT;\nINSERT INTO non_esiste VALUES (1);",
		"chiave esterna rotta": "CREATE TABLE c (id INTEGER PRIMARY KEY, a_id INTEGER REFERENCES a(id)) STRICT;\n" +
			"INSERT INTO c VALUES (1, 99);",
	}
	for nome, sql := range casi {
		t.Run(nome, func(t *testing.T) {
			d, _ := apriProva(t)
			m := file(map[string]string{"0001_prima.sql": dueMigrazioni["0001_prima.sql"], "0002_rotta.sql": sql})
			app, err := d.migraDa(ctx, m, orologio())
			if err == nil || !strings.Contains(err.Error(), "0002") {
				t.Fatalf("err = %v, atteso errore sulla 0002", err)
			}
			if len(app) != 1 || app[0] != 1 {
				t.Errorf("applicate %v, attesa solo la 1", app)
			}
			if v := versioni(t, d); len(v) != 1 {
				t.Errorf("schema_migrations = %v, attesa solo la 1", v)
			}
			if tabella(t, d, "c") {
				t.Error("la tabella c della migrazione fallita è rimasta")
			}
		})
	}
}

func TestMigraRifiutaDatabaseIncoerenti(t *testing.T) {
	t.Run("database più nuovo del programma", func(t *testing.T) {
		d, _ := apriProva(t)
		if _, err := d.migraDa(ctx, file(dueMigrazioni), orologio()); err != nil {
			t.Fatal(err)
		}
		solo1 := file(map[string]string{"0001_prima.sql": dueMigrazioni["0001_prima.sql"]})
		if _, err := d.migraDa(ctx, solo1, orologio()); err == nil || !strings.Contains(err.Error(), "più nuovo") {
			t.Errorf("err = %v, atteso «più nuovo»", err)
		}
	})
	t.Run("nome diverso per la stessa versione", func(t *testing.T) {
		d, _ := apriProva(t)
		if _, err := d.migraDa(ctx, file(dueMigrazioni), orologio()); err != nil {
			t.Fatal(err)
		}
		// Stesso contenuto, nome diverso: l'impronta coincide, conta il nome.
		altro := file(map[string]string{"0001_prima.sql": dueMigrazioni["0001_prima.sql"], "0002_altra.sql": dueMigrazioni["0002_seconda.sql"]})
		if _, err := d.migraDa(ctx, altro, orologio()); err == nil {
			t.Error("atteso errore: la 0002 nel database ha un altro nome")
		}
	})
	t.Run("schema_migrations con un buco", func(t *testing.T) {
		d, _ := apriProva(t)
		if _, err := d.migraDa(ctx, file(dueMigrazioni), orologio()); err != nil {
			t.Fatal(err)
		}
		if _, err := d.Scrittura.Exec(`UPDATE schema_migrations SET versione = 3 WHERE versione = 2`); err != nil {
			t.Fatal(err)
		}
		if _, err := d.migraDa(ctx, file(dueMigrazioni), orologio()); err == nil {
			t.Error("atteso errore: in schema_migrations manca la 2")
		}
	})
	t.Run("solo tabelle interne di SQLite: è ancora nuovo", func(t *testing.T) {
		d, _ := apriProva(t)
		if _, err := d.Scrittura.Exec(`ANALYZE`); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := d.Scrittura.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name LIKE 'sqlite_%'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Skip("ANALYZE non ha creato tabelle interne")
		}
		if _, err := d.migraDa(ctx, file(dueMigrazioni), orologio()); err != nil {
			t.Errorf("database con sole tabelle interne rifiutato: %v", err)
		}
	})
	t.Run("database estraneo senza schema_migrations", func(t *testing.T) {
		d, _ := apriProva(t)
		if _, err := d.Scrittura.Exec(`CREATE TABLE altro (x)`); err != nil {
			t.Fatal(err)
		}
		if _, err := d.migraDa(ctx, file(dueMigrazioni), orologio()); err == nil {
			t.Error("atteso rifiuto di un database con tabelle e senza schema_migrations")
		}
		if tabella(t, d, "a") {
			t.Error("ha migrato un database estraneo")
		}
	})
}

func TestMigraRifiutaFileSbagliati(t *testing.T) {
	casi := map[string]map[string]string{
		"buco":            {"0001_a.sql": "SELECT 1;", "0003_c.sql": "SELECT 1;"},
		"doppia":          {"0001_a.sql": "SELECT 1;", "0001_b.sql": "SELECT 1;"},
		"non parte da 1":  {"0002_a.sql": "SELECT 1;"},
		"nome sbagliato":  {"0001_A b.sql": "SELECT 1;"},
		"file estraneo":   {"0001_a.sql": "SELECT 1;", "LEGGIMI.md": "x"},
		"vuota":           {"0001_a.sql": "  \n"},
		"nessuna":         {},
		"versione a zero": {"0000_a.sql": "SELECT 1;"},
	}
	for nome, m := range casi {
		t.Run(nome, func(t *testing.T) {
			d, _ := apriProva(t)
			if _, err := d.migraDa(ctx, file(m), orologio()); err == nil {
				t.Error("atteso errore")
			}
			if v := versioni(t, d); len(v) != 0 {
				t.Errorf("applicate %v con file sbagliati", v)
			}
		})
	}
}

func TestMigraSenzaBackupNonMigra(t *testing.T) {
	d, dir := apriProva(t)
	solo1 := file(map[string]string{"0001_prima.sql": dueMigrazioni["0001_prima.sql"]})
	if _, err := d.migraDa(ctx, solo1, orologio()); err != nil {
		t.Fatal(err)
	}
	// Il file del backup esiste già (VACUUM INTO lo sovrascriverebbe in silenzio): resta com'è e
	// la migrazione non parte.
	if err := os.WriteFile(filepath.Join(dir, "gestrip.db.prima-di-0002-20261030T091507.000Z"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.migraDa(ctx, file(dueMigrazioni), orologio()); err == nil || !strings.Contains(err.Error(), "backup") {
		t.Fatalf("err = %v, atteso errore di backup", err)
	}
	if v := versioni(t, d); len(v) != 1 || tabella(t, d, "b") {
		t.Errorf("ha migrato senza backup: %v", v)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "gestrip.db.prima-di-0002-20261030T091507.000Z")); string(b) != "x" {
		t.Error("il file che esisteva già è stato sovrascritto")
	}
}

func TestMigrazioniDelProdotto(t *testing.T) {
	d, _ := apriProva(t)
	app, err := d.Migra(ctx, orologio())
	if err != nil {
		t.Fatal(err)
	}
	if len(app) == 0 || !tabella(t, d, "impostazioni") {
		t.Fatalf("applicate %v, tabella impostazioni assente", app)
	}
	if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni VALUES ('k', 'v', ?)`, FormatoIstante); err != nil {
		t.Fatal(err)
	}
	// STRICT: un valore del tipo sbagliato viene rifiutato.
	if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni VALUES ('k2', x'00', 'x')`); err == nil {
		t.Error("impostazioni accetta un blob come valore: manca STRICT")
	}
}

// Le istruzioni che chiudono o aprono transazioni, cambiano pragma o toccano altri file
// romperebbero l'atomicità (una migrazione a metà resta applicata): i file che le contengono
// sono rifiutati prima di toccare il database. I trigger restano permessi.
func TestMigraRifiutaIstruzioniFuoriTransazione(t *testing.T) {
	vietate := []string{
		"CREATE TABLE c(x);\nCOMMIT;\nINSERT INTO c VALUES (1);",
		"BEGIN;\nCREATE TABLE c(x);",
		"CREATE TABLE c(x); rollback;",
		"PRAGMA foreign_keys = OFF;\nCREATE TABLE c(x);",
		"  pragma defer_foreign_keys=1; CREATE TABLE c(x);",
		"CREATE TABLE c(x);\nEND TRANSACTION;",
		"CREATE TABLE c(x); END;",
		"SAVEPOINT s; CREATE TABLE c(x); RELEASE s;",
		"VACUUM;",
		"ATTACH 'altro.db' AS a;",
		"/* commento */ DETACH a;",
		"CREATE TABLE c(x); CREATE TRIGGER t AFTER INSERT ON c BEGIN SELECT 1; END; END;",
		"CREATE TABLE c(x); /* aperto",
		"CREATE TABLE c(x DEFAULT 'aperta);",
	}
	for _, sql := range vietate {
		d, _ := apriProva(t)
		_, err := d.migraDa(ctx, file(map[string]string{"0001_a.sql": sql}), orologio())
		if err == nil || !strings.Contains(err.Error(), "0001_a.sql") {
			t.Errorf("%q: err = %v, atteso rifiuto del file", sql, err)
		}
		if tabella(t, d, "c") || tabella(t, d, "schema_migrations") {
			t.Errorf("%q: il database è stato toccato", sql)
		}
	}
	permesse := []string{
		"CREATE TABLE c(x, y);\nCREATE TRIGGER t AFTER INSERT ON c FOR EACH ROW WHEN NEW.x > 0\nBEGIN\n  UPDATE c SET y = 'commit;' WHERE x = NEW.x;\nEND;\nINSERT INTO c VALUES (1, 'BEGIN');",
		"-- PRAGMA in un commento non conta\nCREATE TABLE c(\"commit\" TEXT);",
		"CREATE TABLE c(x TEXT DEFAULT 'x; pragma y');",
		"CREATE TABLE c([a; commit] TEXT);",
		"CREATE TABLE c(x TEXT DEFAULT 'a''; commit; ''b');",
		"-- nota; PRAGMA foreign_keys = OFF\nCREATE TABLE c(x);",
		"/* nota; PRAGMA */ CREATE TABLE c(x);",
	}
	for _, sql := range permesse {
		d, _ := apriProva(t)
		if _, err := d.migraDa(ctx, file(map[string]string{"0001_a.sql": sql}), orologio()); err != nil {
			t.Errorf("%q: rifiutata: %v", sql, err)
		}
	}
}

// Un database estraneo non viene modificato: nessuna schema_migrations creata.
func TestMigraNonToccaUnDatabaseEstraneo(t *testing.T) {
	d, _ := apriProva(t)
	if _, err := d.Scrittura.Exec(`CREATE TABLE altro (x)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.migraDa(ctx, file(dueMigrazioni), orologio()); err == nil {
		t.Fatal("database estraneo accettato")
	}
	if tabella(t, d, "schema_migrations") {
		t.Error("schema_migrations creata in un database estraneo")
	}
}

// Un database dello standalone (Ecto) si importa, non si migra: messaggio chiaro, nessuna modifica.
func TestMigraRiconosceUnDatabaseEcto(t *testing.T) {
	d, _ := apriProva(t)
	if _, err := d.Scrittura.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, inserted_at TEXT); INSERT INTO schema_migrations VALUES (20260909120000, 'x'); CREATE TABLE repairs (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	_, err := d.migraDa(ctx, file(dueMigrazioni), orologio())
	if err == nil || !strings.Contains(err.Error(), "standalone") {
		t.Errorf("err = %v, atteso messaggio sul database dello standalone", err)
	}
	if tabella(t, d, "a") {
		t.Error("ha migrato un database dello standalone")
	}
}

// Una migrazione già applicata e poi modificata nel programma ferma l'avvio.
func TestMigraRifiutaUnaMigrazioneCambiata(t *testing.T) {
	d, _ := apriProva(t)
	if _, err := d.migraDa(ctx, file(dueMigrazioni), orologio()); err != nil {
		t.Fatal(err)
	}
	cambiata := file(map[string]string{
		"0001_prima.sql":   dueMigrazioni["0001_prima.sql"] + "\nINSERT INTO a VALUES (2);",
		"0002_seconda.sql": dueMigrazioni["0002_seconda.sql"],
	})
	if _, err := d.migraDa(ctx, cambiata, orologio()); err == nil || !strings.Contains(err.Error(), "0001") {
		t.Errorf("err = %v, atteso rifiuto della 0001 cambiata", err)
	}
}

// Un backup non riuscito non lascia un file vuoto o rotto con il nome di un backup.
func TestMigraBackupFallitoNonLasciaAvanzi(t *testing.T) {
	d, dir := apriProva(t)
	solo1 := file(map[string]string{"0001_prima.sql": dueMigrazioni["0001_prima.sql"]})
	if _, err := d.migraDa(ctx, solo1, orologio()); err != nil {
		t.Fatal(err)
	}
	// Il backup deve contenere le migrazioni registrate: con il conteggio falsato la verifica
	// fallisce, come per una copia incompleta.
	vecchio := verificaBackup
	defer func() { verificaBackup = vecchio }()
	verificaBackup = func(ctx context.Context, dest string, attese int) error { return vecchio(ctx, dest, attese+1) }
	if _, err := d.migraDa(ctx, file(dueMigrazioni), orologio()); err == nil || !strings.Contains(err.Error(), "backup") {
		t.Fatalf("err = %v, atteso errore di backup", err)
	}
	if b := backups(t, dir); len(b) != 0 {
		t.Errorf("avanzi del backup fallito: %v", b)
	}
	if tabella(t, d, "b") {
		t.Error("ha migrato con un backup non verificato")
	}
}

// Uno stop che arriva mentre si verifica la copia (quick_check) resta l'errore del contesto, non
// un'integrità rotta: serve lo riconosce come uno stop (ADM-20). La copia non resta e non si migra.
func TestMigraStopDuranteLaVerificaDelBackup(t *testing.T) {
	d, dir := apriProva(t)
	solo1 := file(map[string]string{"0001_prima.sql": dueMigrazioni["0001_prima.sql"]})
	if _, err := d.migraDa(ctx, solo1, orologio()); err != nil {
		t.Fatal(err)
	}
	vecchio := verificaBackup
	defer func() { verificaBackup = vecchio }()
	verificaBackup = func(ctx context.Context, dest string, attese int) error {
		fermato, ferma := context.WithCancel(ctx)
		ferma() // lo stop arriva qui, prima del quick_check
		return vecchio(fermato, dest, attese)
	}
	_, err := d.migraDa(ctx, file(dueMigrazioni), orologio())
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "backup") {
		t.Errorf("err = %v, atteso lo stop (context.Canceled) dalla verifica del backup", err)
	}
	if b := backups(t, dir); len(b) != 0 {
		t.Errorf("avanzi del backup fermato: %v", b)
	}
	if tabella(t, d, "b") {
		t.Error("ha migrato dopo lo stop")
	}
}

func chiudi(t *testing.T, d *DB) {
	t.Helper()
	if err := d.Chiudi(); err != nil {
		t.Errorf("chiusura: %v", err)
	}
}
