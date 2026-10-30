package main

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMain: con GESTRIP_TEST_FIGLIO=1 il binario dei test fa da gestrip (rientra in main con gli
// argomenti dati), così un test lo avvia come processo figlio.
func TestMain(m *testing.M) {
	if os.Getenv("GESTRIP_TEST_FIGLIO") == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// inventario: OPS-13
// gestrip serve, processo vero, non è «dumpable»: lo stesso utente non legge /proc/<pid>/environ
// (EACCES), e un core dump non si scrive. SIGTERM lo ferma pulito.
func TestServeProcessoNonScaricabile(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "serve", "-senza-volume-cifrato", "-db", filepath.Join(dir, "gestrip.db"),
		"-interna", "127.0.0.1:0", "-pubblica", "127.0.0.1:0", "-config", filepath.Join(dir, "manca.conf"))
	cmd.Env = append(os.Environ(), "GESTRIP_TEST_FIGLIO=1")
	errore, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	fermato := false
	t.Cleanup(func() {
		if !fermato {
			_ = cmd.Process.Kill() // solo se il test è uscito prima
			_ = cmd.Wait()
		}
	})
	// Si aspetta la riga «in ascolto», scritta quando le due facce servono e serve aspetta il
	// segnale. La riga delle migrazioni non bastava: un SIGTERM arrivato subito dopo, sotto carico,
	// trovava serve ancora a salvare la chiave del server, che falliva con «context canceled» e
	// uscita 1 (misurato il 30/10/2026 con go test ./internal/web/ in parallelo).
	righe := make(chan string)
	go func() {
		sc := bufio.NewScanner(errore)
		for sc.Scan() {
			righe <- sc.Text()
		}
		close(righe)
	}()
	scadenza := time.After(20 * time.Second)
	for partito := false; !partito; {
		select {
		case r, ok := <-righe:
			if !ok {
				t.Fatal("serve è uscito prima di partire")
			}
			partito = strings.HasPrefix(r, "gestrip: in ascolto: ")
		case <-scadenza:
			t.Fatal("serve non parte")
		}
	}
	resto := make(chan string, 1)
	go func() { // il resto di stderr serve solo a spiegare un errore, ma la pipe non si deve riempire
		var b strings.Builder
		for r := range righe {
			b.WriteString(r + "\n")
		}
		resto <- b.String()
	}()
	_, err = os.ReadFile("/proc/" + strconv.Itoa(cmd.Process.Pid) + "/environ")
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("/proc/<pid>/environ: %v, atteso EACCES", err)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	fermato = true
	altro := <-resto // arriva quando il processo ha chiuso stderr, prima di Wait che chiude la pipe
	if err := cmd.Wait(); err != nil {
		t.Errorf("chiusura con SIGTERM: %v\nstderr:\n%s", err, altro)
	}
}
