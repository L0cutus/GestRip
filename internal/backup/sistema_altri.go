//go:build !linux

package backup

import (
	"errors"
	"fmt"
	"os"
)

// spazioLiberoDi fuori da Linux non si legge: il backup rifiuta invece di partire senza saperlo
// (l'apparecchio è Linux).
func spazioLiberoDi(string) (uint64, error) {
	return 0, errors.New("controllo dello spazio libero solo su Linux")
}

// apriSenzaCollegamenti fuori da Linux (solo sviluppo): Lstat e poi apertura, con una finestra fra
// i due.
func apriSenzaCollegamenti(p string) (*os.File, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s è un collegamento", p)
	}
	return os.Open(p)
}
