package main

import (
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

// gestrip import, processo vero: un argomento in più esce con 2 senza toccare niente; un rifiuto
// prima del report (qui la dimensione dichiarata sbagliata) esce con 1, dice il motivo e non
// stampa la riga del verdetto.
func TestImportProcessoRifiuti(t *testing.T) {
	dir := t.TempDir()
	src, sha := sorgenteVuota(t, dir)
	base := []string{"import", "-senza-volume-cifrato", "-sintetico", "-sorgente", src, "-sha256", sha,
		"-destinazione", filepath.Join(dir, "gestrip.db"), "-report", filepath.Join(dir, "report.json")}
	for _, c := range []struct {
		nome   string
		arg    []string
		uscita int
		errore string
	}{
		{"argomento in più", append(append([]string{}, base...), "avanzo"), 2, "argomenti in più: avanzo"},
		{"dimensione sbagliata", append(append([]string{}, base...), "-dimensione", "12345"), 1, "non 12345 come dichiarato"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			cmd, righe := figlio(t, nil, c.arg...)
			var e strings.Builder
			for r := range righe {
				e.WriteString(r + "\n")
			}
			var uscita *exec.ExitError
			if err := cmd.Wait(); !errors.As(err, &uscita) || uscita.ExitCode() != c.uscita {
				t.Errorf("uscita %v, attesa %d\n%s", err, c.uscita, e.String())
			}
			if !strings.Contains(e.String(), c.errore) || strings.Contains(e.String(), "verdetto") {
				t.Errorf("stderr:\n%s", e.String())
			}
			if got := voci(t, dir); !reflect.DeepEqual(got, []string{filepath.Base(src)}) {
				t.Errorf("nella cartella: %v", got)
			}
		})
	}
}

// gestrip import, processo vero, fermato con SIGINT (Ctrl-C) a metà: come con SIGTERM esce con 1,
// niente attivato, nessun report e nessun temporaneo.
func TestImportProcessoInterrottoDaSIGINT(t *testing.T) {
	dir := t.TempDir()
	src, sha := sorgenteVuota(t, dir)
	cmd, righe := figlio(t, []string{"GESTRIP_TEST_IMPORT_FERMO=1"}, "import", "-senza-volume-cifrato", "-sintetico", "-sorgente", src,
		"-sha256", sha, "-destinazione", filepath.Join(dir, "gestrip.db"), "-report", filepath.Join(dir, "report.json"))
	var e strings.Builder
	scadenza := time.After(20 * time.Second)
	for fermo := false; !fermo; {
		select {
		case r, ok := <-righe:
			if !ok {
				t.Fatalf("import è uscito prima di arrivare a metà:\n%s", e.String())
			}
			e.WriteString(r + "\n")
			fermo = r == rigaImportFermo
		case <-scadenza:
			t.Fatal("import non arriva a metà")
		}
	}
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	for r := range righe {
		e.WriteString(r + "\n")
	}
	var uscita *exec.ExitError
	if err := cmd.Wait(); !errors.As(err, &uscita) || uscita.ExitCode() != 1 {
		t.Errorf("stop: %v, attesa l'uscita 1\n%s", err, e.String())
	}
	if !strings.Contains(e.String(), "import interrotto: niente attivato") {
		t.Errorf("stderr:\n%s", e.String())
	}
	if got := voci(t, dir); !reflect.DeepEqual(got, []string{filepath.Base(src)}) {
		t.Errorf("nella cartella: %v", got)
	}
}
