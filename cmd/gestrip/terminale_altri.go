//go:build !linux

package main

import "os"

// terminale fuori da Linux rifiuta sempre: l'apparecchio è Linux, e un controllo che qui non si
// prova non decide dove si stampa un link segreto (D-04). Il codice compila (go vet su Windows).
func terminale(*os.File) bool { return false }
