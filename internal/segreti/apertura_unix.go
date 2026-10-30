//go:build unix

package segreti

import "syscall"

// aperturaSicura: l'apertura non segue un link al posto del file e non resta appesa su una FIFO.
const aperturaSicura = syscall.O_NOFOLLOW | syscall.O_NONBLOCK | syscall.O_CLOEXEC
