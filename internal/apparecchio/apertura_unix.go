//go:build unix

package apparecchio

import "syscall"

// aperturaCartella apre la cartella dei dati senza seguire un link al suo posto; aperturaFile
// apre un file accanto senza seguire un link e senza restare appesa su una FIFO.
const (
	aperturaCartella = syscall.O_DIRECTORY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	aperturaFile     = syscall.O_NOFOLLOW | syscall.O_NONBLOCK | syscall.O_CLOEXEC
)
