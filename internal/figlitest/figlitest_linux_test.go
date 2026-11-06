package figlitest

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestMain: con FIGLITEST_MODO il binario dei test fa da figlio.
//   - «sordo»: ignora SIGTERM, scrive «pronto» e dorme (come serve fermo nello spegnimento);
//   - «nonno»: avvia un figlio «sordo» senza figlitest (resta nel suo gruppo), ne scrive il pid e
//     dorme anche lui.
func TestMain(m *testing.M) {
	switch os.Getenv("FIGLITEST_MODO") {
	case "sordo":
		signal.Ignore(syscall.SIGTERM)
		fmt.Println("pronto")
		time.Sleep(time.Hour)
		os.Exit(0)
	case "nonno":
		signal.Ignore(syscall.SIGTERM)
		c := exec.Command(os.Args[0])
		c.Env = append(os.Environ(), "FIGLITEST_MODO=sordo")
		if err := c.Start(); err != nil {
			os.Exit(3)
		}
		fmt.Println(c.Process.Pid)
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// comando è il binario dei test nel modo dato.
func comando(modo string, args ...string) *exec.Cmd {
	c := exec.Command(os.Args[0], args...)
	c.Env = append(os.Environ(), "FIGLITEST_MODO="+modo)
	return c
}

// primaRiga legge la prima riga dello standard output del figlio, entro 20 s.
func primaRiga(t *testing.T, sc *bufio.Scanner) string {
	t.Helper()
	riga := make(chan string, 1)
	go func() {
		if sc.Scan() {
			riga <- sc.Text()
		}
		close(riga)
	}()
	select {
	case r, ok := <-riga:
		if !ok {
			t.Fatal("il figlio non ha scritto niente")
		}
		return r
	case <-time.After(20 * time.Second):
		t.Fatal("il figlio non scrive")
	}
	return ""
}

// uccidiAMano toglie a fine test un processo che figlitest avrebbe dovuto togliere: un errore del
// test (o un mutante) non lascia orfani. Va registrato in modo da girare dopo i controlli.
func uccidiAMano(t *testing.T, pid int) {
	t.Cleanup(func() {
		if vivo(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
}

// Il Cleanup toglie il figlio che ignora SIGTERM e il nipote nel suo gruppo, anche a test passato e
// senza che il test chiami Wait; poi kill(pid, 0) dà ESRCH per tutti e due.
func TestCleanupTogliFiglioENipote(t *testing.T) {
	var figlio, nipote int
	esterno := t
	t.Run("dentro", func(t *testing.T) {
		c := comando("nonno")
		out, err := c.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		f := Avvia(t, c)
		figlio = f.Pid()
		uccidiAMano(esterno, figlio) // sul test di fuori: dopo il controllo, non prima del Cleanup di Avvia
		nipote, err = strconv.Atoi(primaRiga(t, bufio.NewScanner(out)))
		if err != nil {
			t.Fatal(err)
		}
		uccidiAMano(esterno, nipote)
		if !vivo(figlio) || !vivo(nipote) {
			t.Fatal("figlio o nipote non partiti")
		}
	})
	if figlio == 0 || nipote == 0 {
		t.Fatal("niente da controllare")
	}
	if vivo(figlio) || vivo(nipote) || gruppoVivo(figlio) {
		t.Errorf("dopo il Cleanup: figlio vivo %v, nipote vivo %v, gruppo vivo %v", vivo(figlio), vivo(nipote), gruppoVivo(figlio))
	}
}

// tFinto raccoglie gli errori e i Cleanup al posto di testing.T, per provare un test che fallisce.
type tFinto struct {
	testing.TB
	mu       sync.Mutex
	errori   []string
	cleanups []func()
}

func (f *tFinto) Helper()                   {}
func (f *tFinto) Cleanup(fn func())         { f.cleanups = append(f.cleanups, fn) }
func (f *tFinto) Fatalf(s string, a ...any) { f.Errorf(s, a...); panic("Fatalf") }
func (f *tFinto) Errorf(s string, a ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errori = append(f.errori, fmt.Sprintf(s, a...))
}

func (f *tFinto) chiudi() {
	for i := len(f.cleanups) - 1; i >= 0; i-- {
		f.cleanups[i]()
	}
}

// Un figlio che non esce (ignora SIGTERM) riceve SIGKILL al suo limite di vita: chi lo aspetta si
// sblocca, e il Cleanup fa fallire il test dicendo che il figlio è stato ucciso al limite.
func TestLimiteDiVita(t *testing.T) {
	ft := &tFinto{TB: t}
	defer ft.chiudi() // anche se il test cade prima: niente figli vivi
	c := comando("sordo")
	out, err := c.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	f := AvviaEntro(ft, c, 2*time.Second) // largo: il figlio deve scrivere «pronto» prima
	uccidiAMano(t, f.Pid())
	if r := primaRiga(t, bufio.NewScanner(out)); r != "pronto" {
		t.Fatalf("prima riga %q", r)
	}
	if err := f.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	inizio := time.Now()
	err = f.Wait()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL || !f.Scaduto() {
		t.Errorf("Wait: %v, scaduto %v; atteso SIGKILL dal limite", err, f.Scaduto())
	}
	if d := time.Since(inizio); d > 10*time.Second {
		t.Errorf("Wait tornata dopo %v", d)
	}
	ft.chiudi()
	ft.cleanups = nil
	if len(ft.errori) != 1 || !strings.Contains(ft.errori[0], "non è uscito entro il suo limite di 2s") {
		t.Errorf("errori del test: %q", ft.errori)
	}
}

// Un figlio che esce da sé prima del limite: nessun errore e il limite non scatta.
func TestFiglioCheEsce(t *testing.T) {
	ft := &tFinto{TB: t}
	defer ft.chiudi()
	f := AvviaEntro(ft, exec.Command("/bin/true"), time.Minute)
	if err := f.Wait(); err != nil || f.Scaduto() {
		t.Errorf("Wait: %v, scaduto %v", err, f.Scaduto())
	}
	ft.chiudi()
	ft.cleanups = nil
	if len(ft.errori) != 0 {
		t.Errorf("errori del test: %q", ft.errori)
	}
}

// TestAiutoProcessoCheMuore non è una prova da sola: con FIGLITEST_GENITORE=1 avvia con Avvia un
// figlio che ignora SIGTERM, ne scrive il pid e si blocca, finché il timeout di go test (-test.timeout
// corto) fa uscire il processo con un panic, senza Cleanup. È il caso del 30/10/2026.
func TestAiutoProcessoCheMuore(t *testing.T) {
	if os.Getenv("FIGLITEST_GENITORE") != "1" {
		t.Skip("solo come processo figlio di TestPdeathsigSenzaCleanup")
	}
	c := comando("sordo")
	c.Env = append(c.Env, "FIGLITEST_GENITORE=")
	out, err := c.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	// Un limite di vita più lungo del timeout: qui solo Pdeathsig può fermare il figlio.
	f := AvviaEntro(t, c, time.Hour)
	if r := primaRiga(t, bufio.NewScanner(out)); r != "pronto" {
		t.Fatalf("prima riga %q", r)
	}
	fmt.Printf("figlio %d\n", f.Pid())
	select {} // il timeout di go test lo ferma
}

// Il processo dei test che muore senza Cleanup (il panic del timeout di go test) si porta via il
// figlio: Pdeathsig. Senza, il figlio, che ignora SIGTERM, passerebbe a init e resterebbe vivo.
func TestPdeathsigSenzaCleanup(t *testing.T) {
	c := exec.Command(os.Args[0], "-test.run=^TestAiutoProcessoCheMuore$", "-test.timeout=10s", "-test.v")
	c.Env = append(os.Environ(), "FIGLITEST_GENITORE=1")
	out, err := c.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	g := Avvia(t, c)
	sc := bufio.NewScanner(out)
	var nipote int
	for sc.Scan() {
		if n, ok := strings.CutPrefix(sc.Text(), "figlio "); ok {
			if nipote, err = strconv.Atoi(n); err != nil {
				t.Fatal(err)
			}
			uccidiAMano(t, nipote)
			break
		}
	}
	if nipote == 0 {
		t.Fatal("il processo intermedio non ha avviato il figlio")
	}
	for sc.Scan() { // il resto, fino all'uscita del processo intermedio
	}
	if err := g.Wait(); err == nil || g.Scaduto() {
		t.Fatalf("processo intermedio: %v, scaduto %v; atteso il panic del timeout", err, g.Scaduto())
	}
	if err := aspettaMorti(nipote, 5*time.Second); err != nil {
		t.Errorf("figlio del processo morto senza Cleanup: %v", err)
	}
}

// tScadenza è un test con il timeout di go test fra resta.
type tScadenza struct {
	testing.TB
	resta time.Duration
}

func (s tScadenza) Deadline() (time.Time, bool) { return time.Now().Add(s.resta), s.resta != 0 }

// Il limite di vita: LimiteMassimo, o il tempo che resta prima del timeout di go test meno 10 s, ma
// almeno metà di quello che resta.
func TestLimiteDi(t *testing.T) {
	for _, c := range []struct{ resta, min, max time.Duration }{
		{0, LimiteMassimo, LimiteMassimo}, // -timeout=0: nessun timeout
		{10 * time.Minute, LimiteMassimo, LimiteMassimo},
		{60 * time.Second, 49 * time.Second, 50 * time.Second},
		{12 * time.Second, 5900 * time.Millisecond, 6 * time.Second},
	} {
		if l := limiteDi(tScadenza{TB: t, resta: c.resta}); l < c.min || l > c.max {
			t.Errorf("resta %v: limite %v, atteso fra %v e %v", c.resta, l, c.min, c.max)
		}
	}
	if l := limiteDi(&tFinto{TB: t}); l != LimiteMassimo {
		t.Errorf("senza Deadline: %v", l)
	}
}

// Un figlio già uscito e raccolto non fa scattare il limite di vita (review della 051b): niente
// falso errore e niente kill(-pid) su un pid che può essere già di un altro processo.
func TestLimiteDopoWait(t *testing.T) {
	ft := &tFinto{TB: t}
	defer ft.chiudi()
	f := AvviaEntro(ft, exec.Command("/bin/true"), 300*time.Millisecond)
	if err := f.Wait(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond) // oltre il limite
	if f.Scaduto() {
		t.Error("il limite è scattato su un figlio già raccolto")
	}
	ft.chiudi()
	ft.cleanups = nil
	if len(ft.errori) != 0 {
		t.Errorf("errori del test: %q", ft.errori)
	}
}

// aspettaMorti dà errore entro il tempo dato su un processo vivo (qui fermato con SIGSTOP, così
// non può nemmeno uscire da sé) e nil appena il processo è morto e raccolto.
func TestAspettaMorti(t *testing.T) {
	c := comando("sordo")
	out, err := c.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	f := Avvia(t, c)
	uccidiAMano(t, f.Pid())
	if r := primaRiga(t, bufio.NewScanner(out)); r != "pronto" {
		t.Fatalf("prima riga %q", r)
	}
	if err := syscall.Kill(f.Pid(), syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	inizio := time.Now()
	err = aspettaMorti(f.Pid(), 300*time.Millisecond)
	if d := time.Since(inizio); err == nil || d < 300*time.Millisecond || d > 5*time.Second {
		t.Errorf("processo vivo: %v dopo %v; atteso un errore dopo 300ms", err, d)
	}
	if err := syscall.Kill(-f.Pid(), syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = f.Wait() // ucciso: l'errore è atteso
	if err := aspettaMorti(f.Pid(), 5*time.Second); err != nil {
		t.Errorf("processo morto e raccolto: %v", err)
	}
}
