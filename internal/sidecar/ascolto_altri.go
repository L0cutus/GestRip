//go:build !linux

package sidecar

// Fuori da Linux (solo sviluppo) /proc non c'è: il controllo del proprietario della porta non si fa.
func ascoltoDelGruppo(int, int) (bool, string) { return true, "" }
