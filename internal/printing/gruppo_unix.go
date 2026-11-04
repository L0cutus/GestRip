//go:build unix

package printing

import (
	"os/exec"
	"syscall"
)

// inGruppo mette il comando in un gruppo di processi suo e fa sì che il tempo scaduto (o il
// contesto cancellato) uccida tutto il gruppo, figli e nipoti compresi (un filtro lanciato da lp,
// un sleep di uno script), non solo il primo processo.
func inGruppo(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
}
