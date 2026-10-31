package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/apparecchio"
	"github.com/L0cutus/gestrip/internal/importer"
)

// sorgenteVuota scrive in dir un database dello standalone con lo schema della fixture e nessuna
// riga, con un nome che non deve mai comparire nei messaggi, e ne dà percorso e SHA-256. I trigger
// hanno il testo vero dello standalone (standalone-go-baseline-oggetti.json), che l'import pretende.
func sorgenteVuota(t *testing.T, dir string) (string, string) {
	t.Helper()
	ddl, err := os.ReadFile(filepath.Join("..", "..", "internal", "importer", "testdata", "standalone-go-baseline.sql"))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "copia-negozio-nome-riservato.db")
	c, err := sql.Open("sqlite", "file:"+p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Exec(string(ddl)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join("..", "..", "internal", "importer", "testdata", "standalone-go-baseline-oggetti.json"))
	if err != nil {
		t.Fatal(err)
	}
	var oggetti []struct{ Tipo, Nome, SQL string }
	if err := json.Unmarshal(b, &oggetti); err != nil {
		t.Fatal(err)
	}
	for _, o := range oggetti {
		if _, err := c.Exec(`DROP TRIGGER "` + o.Nome + `"`); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Exec(o.SQL); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(b)
	return p, hex.EncodeToString(h[:])
}

func voci(t *testing.T, dir string) []string {
	t.Helper()
	vv, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var n []string
	for _, v := range vv {
		n = append(n, v.Name())
	}
	return n
}

// inventario: OPS-10
// gestrip import, come serve, vuole la cartella della destinazione su un volume cifrato: senza,
// rifiuta prima di toccare la sorgente e non crea niente, e il rifiuto non nomina l'opzione che
// spegne il controllo. Sul volume cifrato importa; con -senza-volume-cifrato importa e avvisa.
func TestImportSoloSulVolumeCifrato(t *testing.T) {
	for _, c := range []struct {
		nome         string
		cifrato      bool
		senzaVolume  bool
		errore       string
		avviso       bool
		riconciliato bool
	}{
		{"in chiaro", false, false, "non sta su un volume cifrato", false, false},
		{"cifrato", true, false, "", false, true},
		{"senza controllo", false, true, "", true, true},
	} {
		t.Run(c.nome, func(t *testing.T) {
			dir := t.TempDir()
			src, sha := sorgenteVuota(t, dir)
			o := opzioniImport{radiceSistema: sistemaSintetico(t, dir, c.cifrato), SenzaVolumeCifrato: c.senzaVolume}
			o.Sorgente, o.Sha256, o.Sintetico = src, sha, true
			o.Destinazione, o.Report = filepath.Join(dir, "gestrip.db"), filepath.Join(dir, "report.json")
			var avvisi bytes.Buffer
			es, err := importa(context.Background(), o, &avvisi)
			if c.errore != "" {
				if err == nil || !strings.Contains(err.Error(), c.errore) || strings.Contains(err.Error(), apparecchio.OpzioneSenzaVolumeCifrato) {
					t.Errorf("errore %v", err)
				}
				if got := voci(t, dir); !reflect.DeepEqual(got, []string{filepath.Base(src)}) {
					t.Errorf("nella cartella: %v", got)
				}
				return
			}
			if err != nil || es.Verdetto != importer.Riconciliato {
				t.Fatalf("esito %+v, %v", es, err)
			}
			if got := strings.Contains(avvisi.String(), "ATTENZIONE: controllo del volume cifrato spento"); got != c.avviso {
				t.Errorf("avvisi: %q", avvisi.String())
			}
		})
	}
}

// inventario: OPS-10
// I rifiuti delle opzioni arrivano prima di aprire la sorgente e non creano niente: nessuna
// modalità, due modalità, -riferimento vuoto.
func TestImportModalitaObbligatoria(t *testing.T) {
	for _, c := range []struct {
		nome   string
		cambia func(o *opzioniImport)
		errore string
	}{
		{"nessuna", func(o *opzioniImport) { o.Sintetico = false }, "serve la modalità"},
		{"tutte e due", func(o *opzioniImport) { o.Autorizzato, o.Riferimento = true, "prova" }, "una sola modalità"},
		{"riferimento vuoto", func(o *opzioniImport) { o.Sintetico, o.Autorizzato = false, true }, "chiede -riferimento"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			dir := t.TempDir()
			src, sha := sorgenteVuota(t, dir)
			o := opzioniImport{SenzaVolumeCifrato: true}
			o.Sorgente, o.Sha256, o.Sintetico = src, sha, true
			o.Destinazione, o.Report = filepath.Join(dir, "gestrip.db"), filepath.Join(dir, "report.json")
			c.cambia(&o)
			var avvisi bytes.Buffer
			if _, err := importa(context.Background(), o, &avvisi); err == nil || !strings.Contains(err.Error(), c.errore) {
				t.Errorf("errore %v, atteso %q", err, c.errore)
			}
			if got := voci(t, dir); !reflect.DeepEqual(got, []string{filepath.Base(src)}) {
				t.Errorf("nella cartella: %v", got)
			}
		})
	}
}

// Le opzioni dei media di gestrip import arrivano a importer.Opzioni: -media (la cartella media
// dello standalone), -senza-media (scelta esplicita di non importarli), -miniature (solo contate).
func TestImportOpzioniMedia(t *testing.T) {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	o := opzioniDiImport(fs)
	if err := fs.Parse([]string{"-media", "/dati/media", "-senza-media", "-miniature", "/dati/thumbnails"}); err != nil {
		t.Fatal(err)
	}
	if o.Media != "/dati/media" || !o.SenzaMedia || o.Miniature != "/dati/thumbnails" {
		t.Errorf("opzioni %+v", o.Opzioni)
	}
}
