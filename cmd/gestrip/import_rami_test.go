package main

import (
	"bytes"
	"context"
	"flag"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/importer"
)

// Ogni opzione di gestrip import arriva al suo campo, e i default sono quelli scritti nell'uso:
// nessuna modalità, dimensione non dichiarata, nessun report, media non scelti, controllo del
// volume cifrato acceso.
func TestImportOpzioniEDefault(t *testing.T) {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	o := opzioniDiImport(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*o, opzioniImport{}) {
		t.Errorf("default %+v", *o)
	}
	fs = flag.NewFlagSet("import", flag.ContinueOnError)
	o = opzioniDiImport(fs)
	if err := fs.Parse([]string{"-sintetico", "-autorizzato", "-riferimento", "chi e quando", "-sorgente", "s.db", "-sha256", "ab",
		"-dimensione", "77", "-destinazione", "d.db", "-report", "r.json", "-media", "m", "-senza-media", "-miniature", "n",
		"-senza-volume-cifrato"}); err != nil {
		t.Fatal(err)
	}
	atteso := opzioniImport{SenzaVolumeCifrato: true, Opzioni: importer.Opzioni{Sintetico: true, Autorizzato: true, Riferimento: "chi e quando",
		Sorgente: "s.db", Sha256: "ab", Dimensione: 77, Destinazione: "d.db", Report: "r.json", Media: "m", SenzaMedia: true, Miniature: "n"}}
	if !reflect.DeepEqual(*o, atteso) {
		t.Errorf("opzioni %+v", *o)
	}
}

// Con il controllo del volume acceso e senza -destinazione il rifiuto è quello delle opzioni,
// non uno del volume.
func TestImportSenzaDestinazione(t *testing.T) {
	var avvisi bytes.Buffer
	_, err := importa(context.Background(), opzioniImport{radiceSistema: t.TempDir()}, &avvisi)
	if err == nil || !strings.Contains(err.Error(), "servono -sorgente, -destinazione e -report") {
		t.Errorf("errore %v", err)
	}
}

// Come gli altri comandi, import si ferma con un segreto come valore nell'ambiente, prima di
// toccare la sorgente e senza creare niente.
func TestImportRifiutaISegretiNellAmbiente(t *testing.T) {
	t.Setenv("SECRET_KEY_BASE", strings.Repeat("x", 64))
	dir := t.TempDir()
	src, sha := sorgenteVuota(t, dir)
	o := opzioniImport{SenzaVolumeCifrato: true}
	o.Sorgente, o.Sha256, o.Sintetico = src, sha, true
	o.Destinazione, o.Report = filepath.Join(dir, "gestrip.db"), filepath.Join(dir, "report.json")
	var avvisi bytes.Buffer
	if _, err := importa(context.Background(), o, &avvisi); err == nil || !strings.Contains(err.Error(), "SECRET_KEY_BASE non è accettata") {
		t.Errorf("errore %v", err)
	}
	if got := voci(t, dir); !reflect.DeepEqual(got, []string{filepath.Base(src)}) {
		t.Errorf("nella cartella: %v", got)
	}
}
