package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

// inventario: OPS-10
// gestrip import, processo vero: con il verdetto riconciliato esce con 0 e l'ultima riga dice
// modalità, verdetto, report e sha256 della sorgente; il registro non nomina mai la sorgente. Con una
// sorgente da rifiutare (sha256 sbagliato) esce con 1 e non crea niente.
func TestImportProcesso(t *testing.T) {
	dir := t.TempDir()
	src, sha := sorgenteVuota(t, dir)
	dest, rep := filepath.Join(dir, "gestrip.db"), filepath.Join(dir, "report.json")
	cmd, righe := figlio(t, nil, "import", "-senza-volume-cifrato", "-sintetico", "-sorgente", src, "-sha256", strings.Repeat("ab", 32),
		"-destinazione", dest, "-report", rep)
	var e strings.Builder
	for r := range righe {
		e.WriteString(r + "\n")
	}
	var uscita *exec.ExitError
	if err := cmd.Wait(); !errors.As(err, &uscita) || uscita.ExitCode() != 1 {
		t.Errorf("sha256 sbagliato: %v, attesa l'uscita 1\n%s", err, e.String())
	}
	if got := voci(t, dir); !reflect.DeepEqual(got, []string{filepath.Base(src)}) || strings.Contains(e.String(), "nome-riservato") {
		t.Errorf("nella cartella %v; stderr:\n%s", got, e.String())
	}

	cmd, righe = figlio(t, nil, "import", "-senza-volume-cifrato", "-sintetico", "-sorgente", src, "-sha256", sha,
		"-destinazione", dest, "-report", rep)
	e.Reset()
	for r := range righe {
		e.WriteString(r + "\n")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("import: %v\n%s", err, e.String())
	}
	s := e.String()
	if !strings.Contains(s, "gestrip: import sintetico: verdetto riconciliato; report "+rep+"; sha256 della sorgente "+sha+"; righe lette 0, scritte 0\n") ||
		strings.Contains(s, "nome-riservato") || strings.Contains(s, src) {
		t.Errorf("stderr:\n%s", s)
	}
	if got := voci(t, dir); !reflect.DeepEqual(got, []string{filepath.Base(src), "gestrip.db", "report.json"}) {
		t.Errorf("nella cartella: %v", got)
	}
}

// inventario: OPS-07
// gestrip import, processo vero, fermato con SIGTERM a metà (dopo la copia delle buste): esce con
// 1, niente attivato, nessun report e nessun temporaneo. Il figlio si ferma da sé a metà e lo dice.
func TestImportProcessoInterrotto(t *testing.T) {
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
	// A metà la cartella di lavoro privata c'è, accanto alla destinazione, ed è 0700.
	var lavoro string
	for _, v := range voci(t, dir) {
		if strings.HasPrefix(v, ".gestrip-import-") {
			lavoro = v
		}
	}
	if fi, err := os.Stat(filepath.Join(dir, lavoro)); lavoro == "" || err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("cartella di lavoro %q: %v %v", lavoro, fi, err)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
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

// inventario: OPS-10
// gestrip import, processo vero, con una sorgente da bloccare (una tabella sconosciuta): esce con
// 1, scrive il report con il verdetto bloccato e la riga finale lo dice; nessuna destinazione.
func TestImportProcessoBloccato(t *testing.T) {
	dir := t.TempDir()
	src, _ := sorgenteVuota(t, dir)
	c, err := sql.Open("sqlite", "file:"+src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Exec(`CREATE TABLE intrusa (x TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(b)
	sha := hex.EncodeToString(h[:])
	rep := filepath.Join(dir, "report.json")
	cmd, righe := figlio(t, nil, "import", "-senza-volume-cifrato", "-autorizzato", "-riferimento", "prova del comando", "-sorgente", src,
		"-sha256", sha, "-destinazione", filepath.Join(dir, "gestrip.db"), "-report", rep)
	var e strings.Builder
	for r := range righe {
		e.WriteString(r + "\n")
	}
	var uscita *exec.ExitError
	if err := cmd.Wait(); !errors.As(err, &uscita) || uscita.ExitCode() != 1 {
		t.Errorf("bloccato: %v, attesa l'uscita 1\n%s", err, e.String())
	}
	if !strings.Contains(e.String(), "gestrip: import autorizzato: verdetto bloccato; report "+rep+"; sha256 della sorgente "+sha) {
		t.Errorf("stderr:\n%s", e.String())
	}
	if got := voci(t, dir); !reflect.DeepEqual(got, []string{filepath.Base(src), "report.json"}) {
		t.Errorf("nella cartella: %v", got)
	}
}

// inventario: OPS-07
// gestrip import ucciso con SIGKILL a metà lascia la sua cartella di lavoro: il secondo import la
// trova, rifiuta prima di qualsiasi lavoro e non crea niente; i resti non li toglie da solo.
func TestImportDopoSIGKILL(t *testing.T) {
	dir := t.TempDir()
	src, sha := sorgenteVuota(t, dir)
	arg := []string{"import", "-senza-volume-cifrato", "-sintetico", "-sorgente", src, "-sha256", sha,
		"-destinazione", filepath.Join(dir, "gestrip.db"), "-report", filepath.Join(dir, "report.json")}
	cmd, righe := figlio(t, []string{"GESTRIP_TEST_IMPORT_FERMO=1"}, arg...)
	scadenza := time.After(20 * time.Second)
	for fermo := false; !fermo; {
		select {
		case r, ok := <-righe:
			if !ok {
				t.Fatal("import è uscito prima di arrivare a metà")
			}
			fermo = r == rigaImportFermo
		case <-scadenza:
			t.Fatal("import non arriva a metà")
		}
	}
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	for range righe {
	}
	_ = cmd.Wait() // ucciso: l'errore è atteso
	prima := voci(t, dir)
	if len(prima) != 2 || !strings.HasPrefix(prima[0], ".gestrip-import-") {
		t.Fatalf("dopo SIGKILL: %v", prima)
	}
	cmd, righe = figlio(t, nil, arg...)
	var e strings.Builder
	for r := range righe {
		e.WriteString(r + "\n")
	}
	var uscita *exec.ExitError
	if err := cmd.Wait(); !errors.As(err, &uscita) || uscita.ExitCode() != 1 {
		t.Errorf("secondo import: %v\n%s", err, e.String())
	}
	if !strings.Contains(e.String(), "resti di un import interrotto in "+filepath.Join(dir, prima[0])) {
		t.Errorf("stderr:\n%s", e.String())
	}
	if dopo := voci(t, dir); !reflect.DeepEqual(prima, dopo) {
		t.Errorf("prima %v, dopo %v", prima, dopo)
	}
}
