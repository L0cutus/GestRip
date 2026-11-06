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

	"github.com/L0cutus/gestrip/internal/figlitest"
	"github.com/L0cutus/gestrip/internal/imaging"
)

// rigaAvvioFermo è la riga che il figlio scrive quando l'avvio è fermo a metà.
const rigaAvvioFermo = "gestrip-test: avvio fermo a metà"

// rigaImportFermo è la riga che il figlio scrive quando l'import è fermo a metà.
const rigaImportFermo = "gestrip-test: import fermo a metà"

// TestMain: con GESTRIP_TEST_FIGLIO=1 il binario dei test fa da gestrip (rientra in main con gli
// argomenti dati), così un test lo avvia come processo figlio. Con GESTRIP_TEST_AVVIO_FERMO=1 in
// più, serve si ferma a metà dell'avvio (dopo le migrazioni, prima della chiave del server), lo
// scrive su stderr e aspetta lo stop. Con GESTRIP_TEST_IMPORT_FERMO=1, import si ferma dopo la copia
// delle buste, lo scrive e aspetta lo stop.
func TestMain(m *testing.M) {
	// Il lavoratore delle foto avvia il binario con l'ambiente vuoto: «image-worker» basta.
	if len(os.Args) >= 2 && os.Args[1] == imaging.ComandoFiglio {
		main()
	}
	if os.Getenv("GESTRIP_TEST_FIGLIO") == "1" {
		if os.Getenv("GESTRIP_TEST_IMPORT_FERMO") == "1" {
			importDeiTest = func(ctx context.Context, tab string) {
				if tab == "buste" {
					fmt.Fprintln(os.Stderr, rigaImportFermo)
					<-ctx.Done()
				}
			}
		}
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
	cmd, righe := figlio(t, nil, "serve", "-senza-volume-cifrato", "-db", filepath.Join(dir, "gestrip.db"),
		"-interna", "127.0.0.1:0", "-pubblica", "127.0.0.1:0", "-config", filepath.Join(dir, "manca.conf"))
	// Si aspetta la riga «in ascolto», scritta quando le due facce servono e serve aspetta il
	// segnale. La riga delle migrazioni non bastava: un SIGTERM arrivato subito dopo, sotto carico,
	// trovava serve ancora a salvare la chiave del server, che falliva con «context canceled» e
	// uscita 1 (misurato il 30/10/2026 con go test ./internal/web/ in parallelo).
	var stderr strings.Builder
	scadenza := time.After(20 * time.Second)
	for partito := false; !partito; {
		select {
		case r, ok := <-righe:
			if !ok {
				t.Fatalf("serve è uscito prima di partire:\n%s", stderr.String())
			}
			stderr.WriteString(r + "\n")
			partito = strings.HasPrefix(r, "gestrip: in ascolto: ")
		case <-scadenza:
			t.Fatal("serve non parte")
		}
	}
	_, err := os.ReadFile("/proc/" + strconv.Itoa(cmd.Process.Pid) + "/environ")
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("/proc/<pid>/environ: %v, atteso EACCES", err)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	// Il resto di stderr spiega un errore. Un serve che non si ferma (un lavoro che gira a vuoto
	// dopo lo stop, slice 051b) lo ferma aspettaFine dopo tempoDelloStop, con SIGKILL al gruppo e
	// l'errore; il limite di vita del figlio resta dietro, se il timeout di go test è più corto.
	if err := aspettaFine(cmd, righe, &stderr, tempoDelloStop); err != nil {
		t.Errorf("chiusura con SIGTERM: %v\nstderr:\n%s", err, stderr.String())
	}
}

// tempoDelloStop è quanto un figlio fermato con un segnale ha per uscire: serve dà 10 s allo
// Shutdown delle facce e 2 s al checkpoint del database.
const tempoDelloStop = 30 * time.Second

// aspettaFine legge il resto di stderr del figlio in b e ne aspetta l'uscita, entro tempo: se il
// figlio non esce, SIGKILL al suo gruppo e un errore che lo dice (con l'esito di Wait).
func aspettaFine(cmd *figlitest.Figlio, righe <-chan string, b *strings.Builder, tempo time.Duration) error {
	scadenza := time.After(tempo)
	for {
		select {
		case r, ok := <-righe:
			if ok {
				b.WriteString(r + "\n")
				continue
			}
			return cmd.Wait()
		case <-scadenza:
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			return fmt.Errorf("il figlio non è uscito entro %v (ucciso con SIGKILL: %v)", tempo, cmd.Wait())
		}
	}
}

// figlio avvia gestrip come processo figlio con gli argomenti dati e le variabili in più, con
// figlitest: il figlio non resta mai orfano, nemmeno se il test cade o va oltre il timeout.
// Le righe di stderr arrivano sul canale, che si chiude quando il figlio chiude stderr.
func figlio(t *testing.T, ambiente []string, argomenti ...string) (*figlitest.Figlio, <-chan string) {
	t.Helper()
	return figlioCon(t, nil, ambiente, argomenti...)
}

// figlioCon è figlio con prepara, che prima dell'avvio può dare al comando Stdin, Stdout o
// Stderr (un pseudo-terminale). Con Stderr già dato il canale non riceve niente ed è chiuso.
func figlioCon(t *testing.T, prepara func(*exec.Cmd), ambiente []string, argomenti ...string) (*figlitest.Figlio, <-chan string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], argomenti...)
	cmd.Env = append(append(os.Environ(), "GESTRIP_TEST_FIGLIO=1"), ambiente...)
	if prepara != nil {
		prepara(cmd)
	}
	if cmd.Stderr != nil {
		righe := make(chan string)
		close(righe)
		return figlitest.Avvia(t, cmd), righe
	}
	errore, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	f := figlitest.Avvia(t, cmd)
	righe := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(errore)
		for sc.Scan() {
			righe <- sc.Text()
		}
		close(righe)
	}()
	return f, righe
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
	err := aspettaFine(cmd, righe, &stderr, tempoDelloStop)
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
