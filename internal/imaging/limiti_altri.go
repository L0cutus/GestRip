//go:build !linux

package imaging

import (
	"errors"
	"syscall"
)

// impostaLimiti fuori da Linux (solo sviluppo: l'apparecchio è Linux) non sa dare i limiti, e il
// figlio rifiuta ogni foto invece di girare senza.
func impostaLimiti(Limiti) error { return errors.New("imaging: limiti del figlio solo su Linux") }

func attributiFiglio() *syscall.SysProcAttr { return nil }
