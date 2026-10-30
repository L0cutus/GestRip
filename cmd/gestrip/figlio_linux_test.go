package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
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

// rigaAvvioFermo è la riga che il figlio scrive quando l'avvio è fermo a metà.
const rigaAvvioFermo = "gestrip-test: avvio fermo a metà"

// TestMain: con GESTRIP_TEST_FIGLIO=1 il binario dei test fa da gestrip (rientra in main con gli
// argomenti dati), così un test lo avvia come processo figlio. Con GESTRIP_TEST_AVVIO_FERMO=1 in
// più, serve si ferma a metà dell'avvio (dopo le migrazioni, prima della chiave del server), lo
// scrive su stderr e aspetta lo stop.
func TestMain(m *testing.M) {
	if os.Getenv("GESTRIP_TEST_FIGLIO") == "1" {
		if os.Getenv("GESTRIP_TEST_AVVIO_FERMO") == "1" {
			avvioDeiTest = func(ctx context.Context) {
				fmt.Fprintln(os.Stderr, rigaAvvioFermo)
				<-ctx.Done()
			}
		}
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

// figlio avvia gestrip come processo figlio con gli argomenti dati e le variabili in più.
// Le righe di stderr arrivano sul canale, che si chiude quando il figlio chiude stderr.
func figlio(t *testing.T, ambiente []string, argomenti ...string) (*exec.Cmd, <-chan string) {
	t.Helper()
	return figlioCon(t, nil, ambiente, argomenti...)
}

// figlioCon è figlio con prepara, che prima dell'avvio può dare al comando Stdin, Stdout o
// Stderr (un pseudo-terminale). Con Stderr già dato il canale non riceve niente ed è chiuso.
func figlioCon(t *testing.T, prepara func(*exec.Cmd), ambiente []string, argomenti ...string) (*exec.Cmd, <-chan string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], argomenti...)
	cmd.Env = append(append(os.Environ(), "GESTRIP_TEST_FIGLIO=1"), ambiente...)
	if prepara != nil {
		prepara(cmd)
	}
	if cmd.Stderr != nil {
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if cmd.ProcessState == nil { // solo se il test è uscito prima di Wait
				_ = cmd.Process.Kill()
				_ = cmd.Wait() // ucciso: l'errore è atteso
			}
		})
		righe := make(chan string)
		close(righe)
		return cmd, righe
	}
	errore, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	finito := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-finito:
		default:
			_ = cmd.Process.Kill() // solo se il test è uscito prima di Wait
		}
	})
	righe := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(errore)
		for sc.Scan() {
			righe <- sc.Text()
		}
		close(righe)
		close(finito)
	}()
	return cmd, righe
}

// inventario: ADM-20
// gestrip serve, processo vero, fermato con SIGTERM a metà dell'avvio (le migrazioni fatte, la
// chiave del server non ancora salvata): esce con 0 e una riga sola, senza «context canceled» e
// senza aprire le facce. Il figlio si ferma da sé a metà e lo dice: nessuna attesa a tempo.
func TestServeProcessoFermatoDuranteLAvvio(t *testing.T) {
	dir := t.TempDir()
	cmd, righe := figlio(t, []string{"GESTRIP_TEST_AVVIO_FERMO=1"}, "serve", "-senza-volume-cifrato",
		"-db", filepath.Join(dir, "gestrip.db"), "-interna", "127.0.0.1:0", "-pubblica", "127.0.0.1:0",
		"-config", filepath.Join(dir, "manca.conf"))
	var stderr strings.Builder
	scadenza := time.After(20 * time.Second)
	for fermo := false; !fermo; {
		select {
		case r, ok := <-righe:
			if !ok {
				t.Fatalf("serve è uscito prima di arrivare a metà:\n%s", stderr.String())
			}
			stderr.WriteString(r + "\n")
			fermo = r == rigaAvvioFermo
		case <-scadenza:
			t.Fatal("serve non arriva a metà dell'avvio")
		}
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	for r := range righe {
		stderr.WriteString(r + "\n")
	}
	err := cmd.Wait()
	e := stderr.String()
	if err != nil {
		t.Errorf("stop durante l'avvio: %v, attesa l'uscita 0\nstderr:\n%s", err, e)
	}
	if !strings.Contains(e, "gestrip: fermato durante l'avvio\n") || strings.Contains(e, "context canceled") ||
		strings.Contains(e, "in ascolto") {
		t.Errorf("stderr:\n%s", e)
	}
}

// inventario: ADM-20
// Un errore vero all'avvio (qui il file di configurazione rovinato) resta un'uscita 1 con il suo
// messaggio.
func TestServeProcessoErroreDAvvio(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "apparecchio.conf")
	if err := os.WriteFile(conf, []byte("riga senza uguale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd, righe := figlio(t, nil, "serve", "-senza-volume-cifrato", "-db", filepath.Join(dir, "gestrip.db"),
		"-interna", "127.0.0.1:0", "-pubblica", "127.0.0.1:0", "-config", conf)
	var stderr strings.Builder
	for r := range righe {
		stderr.WriteString(r + "\n")
	}
	err := cmd.Wait()
	var uscita *exec.ExitError
	if !errors.As(err, &uscita) || uscita.ExitCode() != 1 {
		t.Errorf("configurazione rovinata: %v, attesa l'uscita 1", err)
	}
	if e := stderr.String(); !strings.Contains(e, "configurazione dell'apparecchio") || strings.Contains(e, "fermato durante l'avvio") {
		t.Errorf("stderr:\n%s", e)
	}
}
