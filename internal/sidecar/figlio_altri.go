//go:build !linux

package sidecar

import (
	"os"
	"syscall"
)

// Fuori da Linux (solo sviluppo: l'apparecchio è Linux) niente gruppo né Pdeathsig: si ferma il
// solo figlio.
func attributi() *syscall.SysProcAttr { return nil }

func segnala(pid int, sig syscall.Signal) {
	if p, err := os.FindProcess(pid); err == nil {
		if sig == syscall.SIGKILL {
			_ = p.Kill()
		} else {
			_ = p.Signal(sig)
		}
	}
}

func segnalaGruppo(int, syscall.Signal) {}
