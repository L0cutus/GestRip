package backup

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// spazioLiberoDi dà i byte liberi per l'utente nella cartella: Bavail × Frsize di statfs, come
// spazioLiberoDi dell'import (Bsize se Frsize è 0).
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

// apriSenzaCollegamenti apre p in lettura; un collegamento simbolico è un errore (O_NOFOLLOW): il
// tar non copia mai un file fuori dalle cartelle dei dati.
func apriSenzaCollegamenti(p string) (*os.File, error) {
	return os.OpenFile(p, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
}

// rinominaSenzaSovrascrivere rinomina da in a solo se a non c'è (renameat2 con RENAME_NOREPLACE, in
// un colpo solo): un file che c'è già dà un errore con fs.ErrExist e resta com'è. Un file system che
// non lo sa fare (EINVAL) dà un errore: il backup fallisce invece di rischiare.
func rinominaSenzaSovrascrivere(da, a string) error {
	err := unix.Renameat2(unix.AT_FDCWD, da, unix.AT_FDCWD, a, unix.RENAME_NOREPLACE)
	if err != nil {
		return &os.LinkError{Op: "rename", Old: da, New: a, Err: err}
	}
	return nil
}

// dellUtente dice se il file è dell'utente del programma (private_file dello standalone).
func dellUtente(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
