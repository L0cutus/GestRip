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

// senzaEco toglie l'eco al terminale f (la chiave dei backup incollata non resta sullo schermo, slice 130) e dà la
// funzione che lo rimette com'era. Un f che non è un terminale non si tocca (rimetti non fa niente).
func senzaEco(f *os.File) (rimetti func(), err error) {
	if !terminale(f) {
		return func() {}, nil
	}
	rc, err := f.SyscallConn()
	if err != nil {
		return nil, err
	}
	var prima *unix.Termios
	var errT error
	if err := rc.Control(func(fd uintptr) {
		if prima, errT = unix.IoctlGetTermios(int(fd), unix.TCGETS); errT != nil {
			return
		}
		muto := *prima
		muto.Lflag &^= unix.ECHO
		errT = unix.IoctlSetTermios(int(fd), unix.TCSETS, &muto)
	}); err != nil {
		return nil, err
	}
	if errT != nil {
		return nil, errT
	}
	return func() {
		_ = rc.Control(func(fd uintptr) { _ = unix.IoctlSetTermios(int(fd), unix.TCSETS, prima) }) // al meglio
	}, nil
}
