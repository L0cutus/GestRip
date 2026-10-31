//go:build !linux

package importer

import (
	"errors"
	"io/fs"
	"os"
)

// rinominaSenzaSovrascrivere fuori da Linux (solo sviluppo: l'apparecchio è Linux): os.Rename
// dopo un controllo, con una finestra tra i due.
func rinominaSenzaSovrascrivere(da, a string) error {
	if _, err := os.Lstat(a); !errors.Is(err, fs.ErrNotExist) {
		return &os.LinkError{Op: "rename", Old: da, New: a, Err: fs.ErrExist}
	}
	return os.Rename(da, a)
}

// apriFileMedia fuori da Linux: un'apertura normale (Lstat e SameFile restano).
func apriFileMedia(p string) (*os.File, error) { return os.Open(p) }

// spazioLiberoDi fuori da Linux non si legge: l'import rifiuta invece di partire senza saperlo.
func spazioLiberoDi(string) (uint64, error) {
	return 0, errors.New("controllo dello spazio libero solo su Linux")
}
