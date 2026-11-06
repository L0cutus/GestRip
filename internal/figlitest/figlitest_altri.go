//go:build !linux

package figlitest

import (
	"os"
	"os/exec"
)

// Fuori da Linux (solo sviluppo: l'apparecchio è Linux) non c'è Pdeathsig e il gruppo non si
// prepara: resta il limite di vita, che uccide il solo figlio, e il Cleanup.
func prepara(*exec.Cmd) {}

func uccidi(p *os.Process) { _ = p.Kill() }

func vivo(int) bool { return false }

func gruppoVivo(int) bool { return false }
