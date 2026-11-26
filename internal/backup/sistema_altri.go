//go:build !linux

package backup

import (
	"errors"
	"fmt"
	"io/fs"
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

// rinominaSenzaSovrascrivere fuori da Linux (solo sviluppo): Lstat e poi rinomina, con una finestra
// fra i due.
func rinominaSenzaSovrascrivere(da, a string) error {
	if _, err := os.Lstat(a); err == nil {
		return &os.LinkError{Op: "rename", Old: da, New: a, Err: fs.ErrExist}
	}
	return os.Rename(da, a)
}

// dellUtente fuori da Linux non si controlla (solo sviluppo).
func dellUtente(os.FileInfo) bool { return true }

// Fuori da Linux la chiavetta non si usa (l'apparecchio è Linux): ogni controllo rifiuta.
var errSoloLinux = errors.New("backup: chiavetta e rotazione solo su Linux")

func scaricaDallaCache(*os.File) error { return errSoloLinux }

type sistemaVero struct{}

func (sistemaVero) identita(string) (identitaFile, error) { return identitaFile{}, errSoloLinux }
func (sistemaVero) montaggi() ([]byte, error)             { return nil, errSoloLinux }
func (sistemaVero) identitaAperta(*os.File) (identitaFile, error) {
	return identitaFile{}, errSoloLinux
}
func (sistemaVero) scrivibile(string) error { return errSoloLinux }

func bloccaCartella(string) (*os.File, error)           { return nil, errSoloLinux }
func bloccaFile(string) (*os.File, error)               { return nil, errSoloLinux }
func identitaDi(*os.File) (identitaFile, error)         { return identitaFile{}, errSoloLinux }
func guardaNome(*os.File, string) (voceCartella, error) { return voceCartella{}, errSoloLinux }
func togliNome(*os.File, string) error                  { return errSoloLinux }
func nomiDi(*os.File) ([]string, error)                 { return nil, errSoloLinux }
func apriNome(*os.File, string) (*os.File, error)       { return nil, errSoloLinux }
