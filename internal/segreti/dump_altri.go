//go:build !linux

package segreti

// NonScaricabile fuori da Linux (solo sviluppo) non fa niente: l'apparecchio è Linux.
func NonScaricabile() error { return nil }
