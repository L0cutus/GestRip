package sidecar

import "syscall"

// attributi: gruppo di processi suo (il suo pid) e SIGKILL se muore il thread che lo avvia.
func attributi() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}

// segnala manda sig al gruppo pid e al processo pid. Un gruppo o un processo che non c'è più
// (ESRCH) va bene.
func segnala(pid int, sig syscall.Signal) {
	segnalaGruppo(pid, sig)
	_ = syscall.Kill(pid, sig)
}

// segnalaGruppo manda sig al gruppo pid. Finché nel gruppo resta un processo il kernel non
// riusa quel numero come gruppo, quindi dopo la raccolta del figlio colpisce solo i suoi nipoti.
func segnalaGruppo(pid int, sig syscall.Signal) {
	if pid > 0 {
		_ = syscall.Kill(-pid, sig)
	}
}
