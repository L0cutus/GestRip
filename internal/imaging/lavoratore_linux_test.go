package imaging

import (
	"syscall"
	"testing"
)

// inventario: RIP-S-30
// Su Linux il figlio muore con il padre (Pdeathsig SIGKILL). Che il segnale arrivi davvero alla
// morte del padre non si prova qui senza fragilità (servirebbe un processo nonno): lo dichiara la
// ricevuta.
func TestFiglioMuoreConIlPadre(t *testing.T) {
	soloLinux(t)
	if a := attributiFiglio(); a == nil || a.Pdeathsig != syscall.SIGKILL {
		t.Errorf("attributi del figlio: %+v", a)
	}
}
