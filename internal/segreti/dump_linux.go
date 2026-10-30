package segreti

import (
	"fmt"
	"syscall"
)

// NonScaricabile toglie al processo la possibilità di lasciare un core dump: è il posto di
// ERL_CRASH_DUMP=/dev/null dello standalone. Con PR_SET_DUMPABLE a 0 il kernel non scrive il core
// (con suid_dumpable a 0, il default), lo stesso utente non legge la memoria con ptrace né
// /proc/<pid>/mem e environ. Un panic del Go stampa solo le pile su stderr, senza la memoria.
func NonScaricabile() error {
	if _, _, e := syscall.RawSyscall(syscall.SYS_PRCTL, syscall.PR_SET_DUMPABLE, 0, 0); e != 0 {
		return fmt.Errorf("prctl(PR_SET_DUMPABLE, 0): %w", e)
	}
	return nil
}
