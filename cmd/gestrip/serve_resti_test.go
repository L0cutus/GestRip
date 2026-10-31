package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// M1b: serve non crea un database vuoto accanto ai resti di un import (la cartella media o la
// cartella di lavoro .gestrip-import-*): se il database non c'è e i resti sì, rifiuta di partire e
// lo dice. Con il database presente, una cartella media accanto è quella normale e serve parte.
func TestServeNonCreaIlDatabaseAccantoAiResti(t *testing.T) {
	for _, resto := range []string{"media", ".gestrip-import-123"} {
		t.Run(resto, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, resto), 0o700); err != nil {
				t.Fatal(err)
			}
			db := filepath.Join(dir, "gestrip.db")
			var avvisi bytes.Buffer
			err := avviaServe(t, opzioniServe{DB: db, SenzaVolumeCifrato: true, avvisi: &avvisi})
			if err == nil || !strings.Contains(err.Error(), "import interrotto") || !strings.Contains(err.Error(), resto) {
				t.Errorf("errore %v", err)
			}
			if _, e := os.Lstat(db); !errors.Is(e, fs.ErrNotExist) {
				t.Errorf("database creato accanto ai resti: %v", e)
			}
		})
	}
	t.Run("con il database", func(t *testing.T) {
		dir := t.TempDir()
		db := filepath.Join(dir, "gestrip.db")
		var avvisi bytes.Buffer
		if err := avviaServe(t, opzioniServe{DB: db, SenzaVolumeCifrato: true, avvisi: &avvisi}); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(dir, "media"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := avviaServe(t, opzioniServe{DB: db, SenzaVolumeCifrato: true, avvisi: &avvisi}); err != nil {
			t.Errorf("con il database e la cartella media: %v", err)
		}
		// Quando il database c'è, una cartella di lavoro rimasta accanto non ferma serve (la
		// rifiuta il prossimo import).
		if err := os.Mkdir(filepath.Join(dir, ".gestrip-import-456"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := avviaServe(t, opzioniServe{DB: db, SenzaVolumeCifrato: true, avvisi: &avvisi}); err != nil {
			t.Errorf("con il database e una cartella di lavoro: %v", err)
		}
	})
}

// Con i caratteri speciali di uno schema nel nome della cartella dei dati, serve vede i resti
// che ci sono dentro (e rifiuta) e non quelli di una cartella vicina che lo schema prenderebbe
// (e parte).
func TestServeRestiNellaCartellaConMetacaratteri(t *testing.T) {
	for _, c := range []struct{ nome, vicina string }{
		{"negozio[", "negozio1"},
		{"negozio[1]", "negozio1"},
		{"negozio*", "negozio-vicino"},
		{"negozio?", "negozioX"},
		{`nego\zio`, "negozio"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			t.Run("resti dentro", func(t *testing.T) {
				d := filepath.Join(t.TempDir(), c.nome)
				resti := filepath.Join(d, ".gestrip-import-123")
				if err := os.MkdirAll(resti, 0o700); err != nil {
					t.Fatal(err)
				}
				db := filepath.Join(d, "gestrip.db")
				var avvisi bytes.Buffer
				err := avviaServe(t, opzioniServe{DB: db, SenzaVolumeCifrato: true, avvisi: &avvisi})
				if err == nil || !strings.Contains(err.Error(), "accanto c'è "+resti) {
					t.Errorf("errore %v", err)
				}
				if _, e := os.Lstat(db); !errors.Is(e, fs.ErrNotExist) {
					t.Errorf("database creato accanto ai resti: %v", e)
				}
			})
			t.Run("resti nella cartella vicina", func(t *testing.T) {
				dir := t.TempDir()
				d := filepath.Join(dir, c.nome)
				if err := os.Mkdir(d, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(dir, c.vicina, ".gestrip-import-123"), 0o700); err != nil {
					t.Fatal(err)
				}
				var avvisi bytes.Buffer
				if err := avviaServe(t, opzioniServe{DB: filepath.Join(d, "gestrip.db"), SenzaVolumeCifrato: true, avvisi: &avvisi}); err != nil {
					t.Errorf("errore %v", err)
				}
			})
		})
	}
}
