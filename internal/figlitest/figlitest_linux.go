package figlitest

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// prepara dà al figlio un gruppo di processi suo (il suo pid) e SIGKILL alla morte del thread che
// lo avvia. Gli altri attributi già dati restano.
func prepara(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pgid = 0
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}

// uccidi manda SIGKILL al gruppo del figlio (il figlio e i nipoti che non se ne sono fatti uno
// loro) e al figlio stesso. Un gruppo già vuoto (ESRCH) va bene.
func uccidi(p *os.Process) {
	_ = syscall.Kill(-p.Pid, syscall.SIGKILL)
	_ = p.Kill()
}

// vivo dice se c'è ancora il processo pid (anche zombie): kill(pid, 0) senza ESRCH.
func vivo(pid int) bool {
	return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

// gruppoVivo dice se nel gruppo pid c'è ancora un processo (anche zombie).
func gruppoVivo(pid int) bool {
	return !errors.Is(syscall.Kill(-pid, 0), syscall.ESRCH)
}
