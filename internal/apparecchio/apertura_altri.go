//go:build !unix

package apparecchio

// Fuori da unix (solo sviluppo) il controllo comunque rifiuta: non c'è /proc.
const (
	aperturaCartella = 0
	aperturaFile     = 0
)
