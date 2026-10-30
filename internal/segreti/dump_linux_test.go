package segreti

import (
	"syscall"
	"testing"
)

// inventario: OPS-13
// Al posto di ERL_CRASH_DUMP=/dev/null: il processo non è «dumpable», quindi il kernel non scrive
// un core dump con i segreti in memoria (e lo stesso utente non lo legge con ptrace).
func TestNonScaricabile(t *testing.T) {
	t.Cleanup(func() {
		// Il processo dei test torna com'era.
		_, _, _ = syscall.RawSyscall(syscall.SYS_PRCTL, syscall.PR_SET_DUMPABLE, 1, 0)
	})
	if err := NonScaricabile(); err != nil {
		t.Fatal(err)
	}
	v, _, e := syscall.RawSyscall(syscall.SYS_PRCTL, syscall.PR_GET_DUMPABLE, 0, 0)
	if e != 0 || v != 0 {
		t.Errorf("PR_GET_DUMPABLE = %d (%v), atteso 0", v, e)
	}
}
