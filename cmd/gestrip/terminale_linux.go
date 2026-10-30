package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// terminale dice se f è un terminale: il link di D-04 si stampa solo lì. Vale solo un fd che
// risponde a TCGETS (isatty), non un device a caratteri qualsiasi: /dev/null lo è ma non è un
// terminale. Il fd si usa con SyscallConn, che non lo rimette in modalità bloccante come Fd; se
// non si può usare, non è un terminale.
func terminale(f *os.File) bool {
	rc, err := f.SyscallConn()
	if err != nil {
		return false
	}
	var errTermios error = unix.ENOTTY
	if err := rc.Control(func(fd uintptr) { _, errTermios = unix.IoctlGetTermios(int(fd), unix.TCGETS) }); err != nil {
		return false
	}
	return errTermios == nil
}
