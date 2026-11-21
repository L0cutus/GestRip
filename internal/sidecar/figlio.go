package sidecar

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

// figlio è un sidecar partito.
type figlio struct {
	pid    int
	proc   *os.Process
	uscito chan struct{} // chiuso quando Wait è tornata
	stato  *os.ProcessState
}

// avvia fa partire il programma di c con l'ambiente di c (solo quello), la cartella «/», niente
// standard input né output, un gruppo di processi suo e, su Linux, SIGKILL se muore il thread che
// lo ha avviato (Pdeathsig). Pdeathsig guarda il thread, non il processo: la goroutine che fa Start
// resta bloccata sul suo thread fino a Wait, così il runtime non chiude quel thread mentre il
// figlio vive.
func avvia(c Comando) (*figlio, error) {
	cmd := &exec.Cmd{
		Path:        c.Programma,
		Args:        c.argomenti(),
		Env:         append([]string{}, c.Ambiente...), // mai nil: nil vorrebbe dire l'ambiente di GestRip
		Dir:         "/",
		SysProcAttr: attributi(),
	}
	f := &figlio{uscito: make(chan struct{})}
	partito := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := cmd.Start(); err != nil {
			partito <- err
			return
		}
		f.pid, f.proc = cmd.Process.Pid, cmd.Process
		partito <- nil
		_ = cmd.Wait() // l'esito sta in ProcessState
		f.stato = cmd.ProcessState
		close(f.uscito)
	}()
	if err := <-partito; err != nil {
		return nil, err
	}
	return f, nil
}

// ferma chiude il figlio: SIGTERM al gruppo e al figlio, SIGKILL dopo arresto, e dopo che il figlio
// è uscito SIGKILL al gruppo per i nipoti rimasti. Torna a figlio raccolto (o dopo un secondo
// arresto, se nemmeno SIGKILL lo raccoglie: un processo bloccato nel kernel).
func (f *figlio) ferma(arresto time.Duration) {
	if !f.finito() {
		segnala(f.pid, syscall.SIGTERM)
		if !f.aspetta(arresto) {
			segnala(f.pid, syscall.SIGKILL)
			f.aspetta(arresto)
		}
	}
	segnalaGruppo(f.pid, syscall.SIGKILL)
}

func (f *figlio) finito() bool {
	select {
	case <-f.uscito:
		return true
	default:
		return false
	}
}

// aspetta aspetta l'uscita del figlio al più d.
func (f *figlio) aspetta(d time.Duration) bool {
	ctx, annulla := context.WithTimeout(context.Background(), d)
	defer annulla()
	select {
	case <-f.uscito:
		return true
	case <-ctx.Done():
		return false
	}
}

// esito è il codice d'uscita o il segnale del figlio raccolto, per il registro.
func (f *figlio) esito() string {
	if !f.finito() || f.stato == nil {
		return "non raccolto"
	}
	if ws, ok := f.stato.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return "segnale " + ws.Signal().String()
	}
	return "codice " + strconv.Itoa(f.stato.ExitCode())
}
