//go:build !linux

package imaging

import (
	"errors"
	"os"
	"syscall"
)

// impostaLimiti fuori da Linux (solo sviluppo: l'apparecchio è Linux) non sa dare i limiti, e il
// figlio non converte nessuna foto (UscitaSenzaLimiti) invece di girare senza.
func impostaLimiti(Limiti) error { return errors.New("imaging: limiti del figlio solo su Linux") }

func attributiFiglio() *syscall.SysProcAttr { return nil }

// causaDelSegnale fuori da Linux non legge i segnali: il figlio ucciso esce come CausaUscita.
func causaDelSegnale(*os.ProcessState, uint64) (Causa, string, bool) { return "", "", false }
