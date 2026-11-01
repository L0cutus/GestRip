package importer

import (
	"os"

	"golang.org/x/sys/unix"
)

// rinominaSenzaSovrascrivere dà a da il nome a solo se a non c'è (renameat2 con
// RENAME_NOREPLACE): qualunque nome che c'è già dà EEXIST. rename(2) da solo sostituirebbe una
// cartella vuota comparsa nel frattempo (e un file, se da fosse un file).
func rinominaSenzaSovrascrivere(da, a string) error {
	if err := unix.Renameat2(unix.AT_FDCWD, da, unix.AT_FDCWD, a, unix.RENAME_NOREPLACE); err != nil {
		return &os.LinkError{Op: "rename", Old: da, New: a, Err: err}
	}
	return nil
}

// spazioLiberoDi dà i byte liberi per l'utente nella cartella: Bavail × Frsize di statfs (i blocchi
// si contano in Frsize, come statvfs; Bsize è solo la dimensione consigliata per scrivere e su
// alcuni file system di rete è più grande). Bsize resta il ripiego se Frsize è 0.
func spazioLiberoDi(dir string) (uint64, error) {
	var s unix.Statfs_t
	if err := unix.Statfs(dir, &s); err != nil {
		return 0, err
	}
	blocco := uint64(s.Frsize)
	if blocco == 0 {
		blocco = uint64(s.Bsize)
	}
	return s.Bavail * blocco, nil
}
