package backup

import (
	"os"

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
