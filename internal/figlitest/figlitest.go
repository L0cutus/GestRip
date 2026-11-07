// Package figlitest avvia i processi figli dei test (il binario dei test che fa da gestrip serve,
// import, image-worker, …) in modo che non restino mai orfani, e serve solo ai test: il binario di
// gestrip non lo importa (slice 051b).
//
// Il 30/10/2026 due figli «gestrip.test serve» sono rimasti vivi sei giorni al 110% di CPU: un
// mutante della conservazione dell'audit girava a vuoto dopo SIGTERM, il test aspettava il figlio
// senza limite, il timeout di go test ha fatto uscire il processo dei test con un panic, senza i
// Cleanup, e il figlio è passato a init. Qui:
//
//   - su Linux il figlio ha un gruppo di processi suo (Setpgid) e riceve SIGKILL quando muore il
//     thread che lo ha avviato (Pdeathsig): quando il processo dei test muore per qualunque motivo
//     (anche il panic del timeout, che non fa girare i Cleanup) il kernel uccide il figlio.
//     Pdeathsig guarda il thread, non il processo: il figlio parte da una goroutine bloccata sul
//     suo thread (runtime.LockOSThread) fino al Cleanup, così il runtime non chiude quel thread
//     mentre il figlio serve ancora;
//   - il figlio ha un limite di vita proprio, ben sotto il timeout di go test: passato il limite il
//     gruppo riceve SIGKILL, chi legge stderr o aspetta Wait si sblocca e il test fallisce con un
//     messaggio, con i Cleanup che girano;
//   - nel Cleanup, sempre (test passato o fallito), SIGKILL a tutto il gruppo, Wait con un limite
//     e il controllo che nel gruppo non resti nessun processo.
//
// Il gruppo copre i nipoti che restano nel gruppo del figlio: uno che se ne fa uno suo (Setpgid,
// Setsid, come i comandi di stampa di internal/printing) sfugge sia al SIGKILL del gruppo sia al
// controllo, e Pdeathsig vale per il solo figlio, non per i nipoti.
package figlitest

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// LimiteMassimo è la vita più lunga di un figlio dei test. Il limite vero è il più piccolo fra
// questo e il tempo che resta prima del timeout di go test, meno un margine (limiteDi).
const LimiteMassimo = 60 * time.Second

// margineTimeout è quanto prima del timeout di go test il figlio riceve SIGKILL: abbastanza per far
// fallire il test con un messaggio e far girare i Cleanup.
const margineTimeout = 10 * time.Second

// TempoRaccolta è quanto il Cleanup aspetta Wait dopo SIGKILL, e poi che il gruppo sia vuoto.
const TempoRaccolta = 10 * time.Second

// Figlio è un processo figlio di un test avviato da Avvia. Ha i campi e i metodi di exec.Cmd
// (Process, ProcessState, …), ma Wait è la sua: non si chiamano Start, Run o Output.
type Figlio struct {
	*exec.Cmd
	nome   string
	limite time.Duration

	fine     chan struct{} // chiuso dal Cleanup, a figlio raccolto: la goroutine lascia il thread
	raccolto chan struct{} // chiuso quando Wait è tornata
	attesa   sync.Once
	errWait  error

	mu      sync.Mutex
	chiuso  bool // il Cleanup è cominciato: il limite non uccide più
	finito  bool // Wait è tornata: il figlio è raccolto e il limite non uccide più
	scaduto bool // il limite di vita ha ucciso il gruppo
	timer   *time.Timer
}

// Avvia avvia cmd come figlio del test con il limite di vita di limiteDi(t). Stdin, Stdout,
// Stderr e le pipe (StderrPipe, …) si danno prima. Il test aspetta il figlio con f.Wait, mai con
// cmd.Wait: il Cleanup lo aspetta anche lui.
func Avvia(t testing.TB, cmd *exec.Cmd) *Figlio {
	t.Helper()
	return AvviaEntro(t, cmd, limiteDi(t))
}

// AvviaEntro è Avvia con un limite di vita dato.
func AvviaEntro(t testing.TB, cmd *exec.Cmd, limite time.Duration) *Figlio {
	t.Helper()
	f := &Figlio{Cmd: cmd, nome: nomeDi(cmd), limite: limite, fine: make(chan struct{}), raccolto: make(chan struct{})}
	prepara(cmd)
	avviato := make(chan error, 1)
	go func() {
		// Pdeathsig è legato al thread che fa fork, non al processo. Senza il blocco il fork
		// verrebbe da un thread qualunque, che il runtime chiude se ci finisce sopra un'altra
		// goroutine bloccata con LockOSThread: raro, ma il figlio morirebbe a metà test. Bloccato,
		// il thread resta finché il Cleanup ha ucciso e raccolto il figlio; poi torna al runtime.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		avviato <- cmd.Start()
		<-f.fine
	}()
	if err := <-avviato; err != nil {
		close(f.fine)
		t.Fatalf("figlio %s: avvio: %v", f.nome, err)
	}
	f.timer = time.AfterFunc(limite, func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if !f.chiuso && !f.finito {
			f.scaduto = true
			uccidi(cmd.Process)
		}
	})
	t.Cleanup(func() { f.chiudi(t) })
	return f
}

// Wait aspetta la fine del figlio, una volta sola: le chiamate dopo danno lo stesso esito. Con
// StderrPipe o StdoutPipe si chiama dopo aver letto tutto (come per exec.Cmd.Wait).
func (f *Figlio) Wait() error {
	f.attesa.Do(func() {
		f.errWait = f.Cmd.Wait()
		// Raccolto: il limite non deve più scattare, né su questo pid né sul suo gruppo (il
		// pid può tornare a un altro processo). I nipoti rimasti li uccide il Cleanup.
		f.mu.Lock()
		f.finito = true
		if f.timer != nil {
			f.timer.Stop()
		}
		f.mu.Unlock()
		close(f.raccolto)
	})
	return f.errWait
}

// Pid è il pid del figlio, che è anche il suo gruppo di processi su Linux.
func (f *Figlio) Pid() int { return f.Cmd.Process.Pid }

// Scaduto dice se il limite di vita ha ucciso il figlio.
func (f *Figlio) Scaduto() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.scaduto
}

func (f *Figlio) chiudi(t testing.TB) {
	t.Helper()
	f.mu.Lock()
	f.chiuso = true
	scaduto := f.scaduto
	f.mu.Unlock()
	f.timer.Stop()
	pid := f.Cmd.Process.Pid
	uccidi(f.Cmd.Process)        // sempre: il figlio e i nipoti del suo gruppo, anche a test passato
	go func() { _ = f.Wait() }() // l'esito, se serviva, lo ha già letto il test
	// clock.Aspetta: il pacchetto non legge l'orologio da sé (la guardia di internal/clock).
	if clock.Aspetta(contestoFinoA(f.raccolto), TempoRaccolta) { // tempo passato, Wait non tornata
		t.Errorf("figlio %s (pid %d): Wait non torna %v dopo SIGKILL", f.nome, pid, TempoRaccolta)
	}
	close(f.fine)
	if scaduto {
		t.Errorf("figlio %s (pid %d): non è uscito entro il suo limite di %v ed è stato ucciso con SIGKILL", f.nome, pid, f.limite)
	}
	if err := aspettaMorti(pid, TempoRaccolta); err != nil {
		t.Errorf("figlio %s: %v", f.nome, err)
	}
}

// aspettaMorti aspetta che il processo pid e ogni processo del suo gruppo non ci siano più
// (kill(…, 0) dà ESRCH); un nipote già ucciso resta zombie finché init non lo raccoglie.
func aspettaMorti(pid int, entro time.Duration) error {
	inizio := clock.Parti() // lettura monotona: un salto dell'ora non accorcia l'attesa
	for {
		p, g := vivo(pid), gruppoVivo(pid)
		if !p && !g {
			return nil
		}
		if inizio.Trascorso() > entro {
			return fmt.Errorf("pid %d ancora vivo %v (processo %v, gruppo %v) dopo SIGKILL", pid, entro, p, g)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// limiteDi è LimiteMassimo, o meno se il timeout di go test arriva prima: il tempo che resta meno
// margineTimeout, e almeno metà del tempo che resta.
func limiteDi(t testing.TB) time.Duration {
	l := LimiteMassimo
	if d, ok := t.(interface{ Deadline() (time.Time, bool) }); ok {
		if s, ok := d.Deadline(); ok {
			resta := clock.Resta(s) // la scadenza di go test ha la lettura monotona
			l = min(l, max(resta-margineTimeout, resta/2))
		}
	}
	return max(l, time.Millisecond)
}

// nomeDi è il comando con il primo argomento, per i messaggi.
func nomeDi(cmd *exec.Cmd) string {
	n := filepath.Base(cmd.Path)
	if len(cmd.Args) > 1 {
		n += " " + cmd.Args[1]
	}
	return n
}

// contestoFinoA è un contesto che finisce quando si chiude c (raccolto, che il Wait in corso
// chiude sempre quando torna).
func contestoFinoA(c <-chan struct{}) context.Context {
	ctx, annulla := context.WithCancel(context.Background())
	go func() { <-c; annulla() }()
	return ctx
}
