package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// pseudoTerminale apre un pseudo-terminale vero (/dev/ptmx, poi /dev/pts/N) e ne dà i due lati:
// master, quello del terminale che legge, e slave, quello che un comando lanciato in una shell ha
// come stdout. Senza /dev/ptmx o senza devpts completo il test si salta.
func pseudoTerminale(t *testing.T) (master, slave *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("/dev/ptmx non disponibile: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	fd := int(m.Fd())
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Skipf("sblocco del pseudo-terminale: %v", err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Skipf("numero del pseudo-terminale: %v", err)
	}
	s, err := os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("lato slave del pseudo-terminale: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() }) // chiuso anche prima: il secondo errore non conta
	return m, s
}

// D-04: un pseudo-terminale vero (una shell, ssh) vale come terminale.
func TestTerminaleSuUnPseudoTerminale(t *testing.T) {
	_, slave := pseudoTerminale(t)
	if !terminale(slave) {
		t.Error("un pseudo-terminale non vale come terminale")
	}
}

// Un fd che non si può usare non è un terminale: un file nil (SyscallConn fallisce) e un
// pseudo-terminale già chiuso (Control fallisce).
func TestTerminaleFdNonUsabile(t *testing.T) {
	if terminale(nil) {
		t.Error("un file nil vale come terminale")
	}
	_, slave := pseudoTerminale(t)
	_ = slave.Close() // chiuso apposta: l'errore non conta
	if terminale(slave) {
		t.Error("un terminale chiuso vale come terminale")
	}
}

// inventario: ACC-13, ACC-07, ACC-16
// D-04, processo vero: con stdout su /dev/null (un device a caratteri, non un terminale)
// reset-link, invito e primo-titolare escono con 1 e il messaggio di rifiuto, senza un link su
// stdout e senza toccare il database. Ogni database è pronto perché il comando riesca: il solo
// motivo del rifiuto è lo stdout.
func TestComandiDelLinkConStdoutSuDevNull(t *testing.T) {
	const rifiuto = "gestrip: il link si stampa solo su un terminale (non su un file, una pipe o il journal)"
	for _, c := range comandiDelLink() {
		t.Run(c.argomenti[0], func(t *testing.T) {
			p := c.db(t)
			if got := righeDi(t, p, contaDelLink); got != c.prima {
				t.Fatalf("database di partenza: %s", got)
			}
			// Con Stdout nil exec collega lo stdout del figlio a /dev/null (os.DevNull).
			cmd, righe := figlio(t, nil, append(c.argomenti, "-db", p, "-url", "https://negozio.esempio.ts.net:8443")...)
			if cmd.Stdout != nil {
				t.Fatal("lo stdout del figlio non è /dev/null")
			}
			var stderr strings.Builder
			for r := range righe {
				stderr.WriteString(r + "\n")
			}
			err := cmd.Wait()
			var uscita *exec.ExitError
			if !errors.As(err, &uscita) || uscita.ExitCode() != 1 {
				t.Errorf("stdout su /dev/null: %v, attesa l'uscita 1\nstderr:\n%s", err, stderr.String())
			}
			if stderr.String() != rifiuto+"\n" {
				t.Errorf("stderr:\n%s", stderr.String())
			}
			if got := righeDi(t, p, contaDelLink); got != c.prima {
				t.Errorf("il rifiuto ha scritto nel database: %s, prima %s", got, c.prima)
			}
		})
	}
}

// comandoDelLink è uno dei tre comandi che stampano un link (D-04), con il database su cui riesce.
type comandoDelLink struct {
	db        func(*testing.T) string
	argomenti []string
	prima     string
	// link è il link stampato; dopo, i conteggi del database dopo un comando riuscito.
	link *regexp.Regexp
	dopo string
}

// contaDelLink conta le righe di utenti, reset_password, inviti_utenti, codici_primo_titolare,
// eventi_audit.
const contaDelLink = `SELECT (SELECT count(*) FROM utenti) || '|' || (SELECT count(*) FROM reset_password) || '|' ||
	(SELECT count(*) FROM inviti_utenti) || '|' || (SELECT count(*) FROM codici_primo_titolare) || '|' ||
	(SELECT count(*) FROM eventi_audit)`

func comandiDelLink() []comandoDelLink {
	return []comandoDelLink{
		{databaseConTitolare, []string{"reset-link"}, "1|0|0|0|0",
			regexp.MustCompile(`(?m)^https://negozio\.esempio\.ts\.net:8443/reset/[A-Za-z0-9_-]{43}$`), "1|1|0|0|1"},
		{databaseConTitolare, []string{"invito", "-email", "nuovo@esempio.it"}, "1|0|0|0|0", reInvitoStampato, "2|0|1|0|1"},
		{databaseVuoto, []string{"primo-titolare"}, "0|0|0|0|0", reConfiguraStampato, "0|0|0|1|1"},
	}
}

// D-04, il controllo è su stdout: con stdin e stderr su un pseudo-terminale vero e stdout su
// /dev/null i tre comandi rifiutano (uscita 1) e non toccano il database.
func TestComandiDelLinkControllanoStdout(t *testing.T) {
	for _, c := range comandiDelLink() {
		t.Run(c.argomenti[0], func(t *testing.T) {
			p := c.db(t)
			master, slave := pseudoTerminale(t)
			testo := leggiTutto(master)
			cmd, _ := figlioCon(t, func(cmd *exec.Cmd) { cmd.Stdin, cmd.Stderr = slave, slave },
				nil, append(c.argomenti, "-db", p, "-url", "https://negozio.esempio.ts.net:8443")...)
			_ = slave.Close() // ora è solo del figlio: alla sua uscita il master legge EIO
			err := cmd.Wait()
			var uscita *exec.ExitError
			if !errors.As(err, &uscita) || uscita.ExitCode() != 1 {
				t.Errorf("stdout su /dev/null, stdin e stderr sul terminale: %v, attesa l'uscita 1", err)
			}
			if e := attendi(t, testo); !strings.Contains(e, "il link si stampa solo su un terminale") {
				t.Errorf("stderr sul terminale: %q", e)
			}
			if got := righeDi(t, p, contaDelLink); got != c.prima {
				t.Errorf("il rifiuto ha scritto nel database: %s, prima %s", got, c.prima)
			}
		})
	}
}

// D-04, processo vero: con stdout su un pseudo-terminale vero (stdin /dev/null, stderr una pipe)
// i tre comandi stampano il link sul terminale, escono con 0 e scrivono la riga nel database.
func TestComandiDelLinkSulTerminale(t *testing.T) {
	for _, c := range comandiDelLink() {
		t.Run(c.argomenti[0], func(t *testing.T) {
			p := c.db(t)
			master, slave := pseudoTerminale(t)
			testo := leggiTutto(master)
			cmd, righe := figlioCon(t, func(cmd *exec.Cmd) { cmd.Stdout = slave },
				nil, append(c.argomenti, "-db", p, "-url", "https://negozio.esempio.ts.net:8443")...)
			_ = slave.Close() // ora è solo del figlio: alla sua uscita il master legge EIO
			var stderr strings.Builder
			for r := range righe {
				stderr.WriteString(r + "\n")
			}
			if err := cmd.Wait(); err != nil {
				t.Errorf("stdout sul terminale: %v, attesa l'uscita 0\nstderr:\n%s", err, stderr.String())
			}
			// Il terminale scrive «\r\n» al posto di «\n» (ONLCR).
			if out := strings.ReplaceAll(attendi(t, testo), "\r\n", "\n"); !c.link.MatchString(out) {
				t.Errorf("nessun link sul terminale: %q", out)
			}
			if got := righeDi(t, p, contaDelLink); got != c.dopo {
				t.Errorf("database dopo il comando: %s, atteso %s", got, c.dopo)
			}
		})
	}
}

// leggiTutto legge il master del pseudo-terminale fino a quando nessuno ha più lo slave aperto
// (EIO, la fine normale) e dà il testo sul canale.
func leggiTutto(master *os.File) <-chan string {
	testo := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(master) // finisce con EIO, quando lo slave si chiude: è la fine normale
		testo <- string(b)
	}()
	return testo
}

// attendi aspetta il testo del terminale, senza lasciare il test appeso.
func attendi(t *testing.T, testo <-chan string) string {
	t.Helper()
	select {
	case s := <-testo:
		return s
	case <-time.After(20 * time.Second):
		t.Fatal("il terminale non si chiude")
		return ""
	}
}
