package db

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestApriImpostaWALEChiaviEsterne(t *testing.T) {
	d, err := Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { chiudi(t, d) })
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
			if err := c.(interface{ Close() error }).Close(); err != nil {
				t.Fatal(err)
			}
		}
		if !d.Pronto(context.Background()) {
			t.Error("non torna pronto dopo aver liberato le connessioni")
		}
	})

	t.Run("database chiuso: no", func(t *testing.T) {
		chiudi(t, d)
		if d.Pronto(context.Background()) {
			t.Error("pronto con il database chiuso")
		}
	})
}

// nomiURI sono nomi di cartella con i caratteri che in un URI di SQLite hanno un senso (%, ?, #),
// uno spazio e una lettera non ASCII: il percorso arriva a SQLite intatto.
var nomiURI = []struct{ caso, nome string }{
	{"percento e due cifre", "con%41percento"},
	{"percento solo", "con%percento"},
	{"punto di domanda", "con?domanda"},
	{"cancelletto", "con#cancelletto"},
	{"spazio", "con spazio"},
	{"non ASCII", "perché è"},
}

// Apri apre proprio il file del percorso, qualunque carattere abbia il nome (della cartella e del
// file): nasce solo quel file, e riaprendolo si legge lo stesso database.
func TestApriPercorsiConCaratteriDellURI(t *testing.T) {
	for _, c := range nomiURI {
		t.Run(c.caso, func(t *testing.T) {
			dir := t.TempDir()
			sotto := filepath.Join(dir, c.nome)
			if err := os.Mkdir(sotto, 0o700); err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(sotto, c.nome+".db")
			d, err := Apri(p)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.Scrittura.Exec(`CREATE TABLE t (x INTEGER); INSERT INTO t VALUES (7)`); err != nil {
				t.Fatal(err)
			}
			chiudi(t, d)
			if got := nomiIn(t, dir); !reflect.DeepEqual(got, []string{c.nome}) {
				t.Errorf("accanto alla cartella: %v", got)
			}
			if got := nomiIn(t, sotto); !reflect.DeepEqual(got, []string{c.nome + ".db"}) {
				t.Errorf("nella cartella: %v", got)
			}
			d, err = Apri(p)
			if err != nil {
				t.Fatal(err)
			}
			defer chiudi(t, d)
			var x int
			if err := d.Lettura.QueryRow(`SELECT x FROM t`).Scan(&x); err != nil || x != 7 {
				t.Errorf("riaperto: %d %v", x, err)
			}
		})
	}
	// Un percorso assoluto che comincia con due barre: in un URI sarebbe l'inizio di un'autorità.
	t.Run("due barre in testa", func(t *testing.T) {
		dir := t.TempDir()
		d, err := Apri("/" + filepath.Join(dir, "gestrip.db"))
		if err != nil {
			t.Fatal(err)
		}
		chiudi(t, d)
		if got := nomiIn(t, dir); !reflect.DeepEqual(got, []string{"gestrip.db"}) {
			t.Errorf("nella cartella: %v", got)
		}
	})
}

// nomiIn elenca i nomi nella cartella.
func nomiIn(t *testing.T, dir string) []string {
	t.Helper()
	voci, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var nomi []string
	for _, v := range voci {
		nomi = append(nomi, v.Name())
	}
	return nomi
}

// Il backup prima di una migrazione si verifica anche quando il nome del database ha caratteri
// dell'URI: la copia letta è proprio quella scritta accanto.
func TestMigraBackupConCaratteriDellURI(t *testing.T) {
	for _, c := range nomiURI {
		t.Run(c.caso, func(t *testing.T) {
			sotto := filepath.Join(t.TempDir(), c.nome)
			if err := os.Mkdir(sotto, 0o700); err != nil {
				t.Fatal(err)
			}
			d, err := Apri(filepath.Join(sotto, "gestrip.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer chiudi(t, d)
			solo1 := file(map[string]string{"0001_prima.sql": dueMigrazioni["0001_prima.sql"]})
			if _, err := d.migraDa(ctx, solo1, orologio()); err != nil {
				t.Fatal(err)
			}
			if _, err := d.migraDa(ctx, file(dueMigrazioni), orologio()); err != nil {
				t.Fatal(err)
			}
			var copie []string
			for _, n := range nomiIn(t, sotto) {
				if strings.Contains(n, ".prima-di-") {
					copie = append(copie, n)
				}
			}
			if !reflect.DeepEqual(copie, []string{"gestrip.db.prima-di-0002-20261030T091507.000Z"}) {
				t.Errorf("backup: %v", copie)
			}
		})
	}
}

// Un percorso relativo apre il file con quel nome nella cartella corrente, anche se comincia con
// «file:» o è «:memory:» (che per SQLite sarebbe un database in memoria), e il backup prima di
// una migrazione nasce accanto con il nome giusto e si verifica.
func TestApriPercorsiRelativi(t *testing.T) {
	for _, nome := range []string{"rel.db", "file:rel.db", ":memory:", "file:con?domanda.db"} {
		t.Run(nome, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			d, err := Apri(nome)
			if err != nil {
				t.Fatal(err)
			}
			solo1 := file(map[string]string{"0001_prima.sql": dueMigrazioni["0001_prima.sql"]})
			if _, err := d.migraDa(ctx, solo1, orologio()); err != nil {
				t.Fatal(err)
			}
			if _, err := d.migraDa(ctx, file(dueMigrazioni), orologio()); err != nil {
				t.Fatal(err)
			}
			chiudi(t, d)
			att := []string{nome, nome + ".prima-di-0002-20261030T091507.000Z"}
			if n := nomiIn(t, dir); !reflect.DeepEqual(n, att) {
				t.Errorf("nella cartella %v, attesi %v", n, att)
			}
			d, err = Apri(nome)
			if err != nil {
				t.Fatal(err)
			}
			defer chiudi(t, d)
			var n int
			if err := d.Lettura.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil || n != 2 {
				t.Errorf("migrazioni rilette %d, %v", n, err)
			}
		})
	}
}
