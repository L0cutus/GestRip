//go:build unix

package db

import "syscall"

// aperturaPrivata: aprire il database non segue un link al suo posto e non resta appeso su una
// FIFO.
const aperturaPrivata = syscall.O_NOFOLLOW | syscall.O_NONBLOCK | syscall.O_CLOEXEC
