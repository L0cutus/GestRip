package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
// un colpo solo): un file che c'è già dà un errore con fs.ErrExist e resta com'è.
//
// Nota 6c (slice 123): FUSE senza rename2 (ntfs-3g), CIFS e 9p rispondono EINVAL (o ENOSYS) a
// RENAME_NOREPLACE. Lì si ripiega su link(2) e poi unlink del vecchio nome: link non sovrascrive mai
// (EEXIST se a c'è già), quindi nemmeno il ripiego può schiacciare un archivio. Se si ferma a metà
// (blackout fra link e unlink) restano due nomi dello stesso file: l'archivio è intero, il .pending in
// più lo toglie la pulizia della chiavetta. Un file system che non sa fare né l'una né l'altra cosa (FAT
// senza collegamenti, per esempio sotto FUSE) dà ErrRinominaNonSupportata con un messaggio che lo dice:
// il backup fallisce invece di rischiare una rinomina che sovrascrive.
func rinominaSenzaSovrascrivere(da, a string) error {
	err := renameat2(unix.AT_FDCWD, da, unix.AT_FDCWD, a, unix.RENAME_NOREPLACE)
	if err == nil {
		return nil
	}
	if !errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.ENOSYS) {
		return &os.LinkError{Op: "rename", Old: da, New: a, Err: err}
	}
	errLink := linkat(unix.AT_FDCWD, da, unix.AT_FDCWD, a, 0)
	switch {
	case errLink == nil:
	case errors.Is(errLink, unix.EEXIST):
		return &os.LinkError{Op: "link", Old: da, New: a, Err: errLink} // fs.ErrExist
	default:
		return fmt.Errorf("%w (renameat2: %w; link: %w)", ErrRinominaNonSupportata, err, errLink)
	}
	if err := os.Remove(da); err != nil {
		// L'archivio c'è con il nome giusto; il vecchio nome resta: lo toglie la pulizia dei .pending.
		return fmt.Errorf("backup: archivio pubblicato con un collegamento, ma %s non si toglie: %w", filepath.Base(da), err)
	}
	return nil
}

// renameat2 e linkat sono le chiamate di sistema; variabili per i test (nota 6c: EINVAL di FUSE e CIFS).
var (
	renameat2 = unix.Renameat2
	linkat    = unix.Linkat
)

// scaricaDallaCache chiede al kernel di dimenticare le pagine del file nella cache (posix_fadvise
// DONTNEED, nota 7b): dopo l'fsync sono pulite, quindi la verifica le rilegge dal supporto invece che
// dalla memoria. È solo un aiuto: il kernel può ignorarlo, la cache del dispositivo resta; un errore non
// ferma il backup.
func scaricaDallaCache(f *os.File) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var errF error
	if err := rc.Control(func(fd uintptr) { errF = unix.Fadvise(int(fd), 0, 0, unix.FADV_DONTNEED) }); err != nil {
		return err
	}
	return errF
}

// sistemaVero legge stat, /proc/self/mountinfo e i permessi dal kernel.
type sistemaVero struct{}

func (sistemaVero) identita(p string) (identitaFile, error) {
	var st unix.Stat_t
	if err := unix.Stat(p, &st); err != nil {
		return identitaFile{}, &os.PathError{Op: "stat", Path: p, Err: err}
	}
	return identitaFile{dev: st.Dev, ino: st.Ino}, nil
}

func (sistemaVero) identitaAperta(f *os.File) (identitaFile, error) { return identitaDi(f) }

func (sistemaVero) montaggi() ([]byte, error) {
	return os.ReadFile("/proc/self/mountinfo")
}

func (sistemaVero) scrivibile(p string) error {
	var s unix.Statfs_t
	if err := unix.Statfs(p, &s); err != nil {
		return err
	}
	if s.Flags&unix.ST_RDONLY != 0 {
		return errors.New("montata in sola lettura")
	}
	if err := unix.Access(p, unix.W_OK|unix.X_OK); err != nil {
		return fmt.Errorf("non scrivibile dall'utente del programma: %w", err)
	}
	return nil
}

// bloccaCartella apre la cartella e ne prende il flock esclusivo senza aspettare: un altro backup che
// scrive nella stessa cartella (o la ruota) lo tiene. Il kernel lo toglie se il processo muore.
func bloccaCartella(p string) (*os.File, error) {
	f, err := os.OpenFile(p, os.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	rc, err := f.SyscallConn()
	if err == nil {
		var errF error
		if err = rc.Control(func(fd uintptr) { errF = unix.Flock(int(fd), unix.LOCK_EX|unix.LOCK_NB) }); err == nil {
			err = errF
		}
	}
	if err != nil {
		_ = f.Close() // mai bloccata: niente da perdere
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrDestinazioneOccupata
		}
		return nil, fmt.Errorf("backup: lucchetto della destinazione: %w", err)
	}
	return f, nil
}

// identitaDi è l'identità della cartella aperta (fstat).
func identitaDi(d *os.File) (identitaFile, error) {
	fi, err := d.Stat()
	if err != nil {
		return identitaFile{}, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return identitaFile{}, errors.New("stat senza dispositivo")
	}
	return identitaFile{dev: st.Dev, ino: st.Ino}, nil
}

// guardaNome guarda il nome dentro la cartella aperta senza seguire i collegamenti (fstatat
// AT_SYMLINK_NOFOLLOW): un nome scambiato con un collegamento non porta fuori.
func guardaNome(d *os.File, nome string) (voceCartella, error) {
	var st unix.Stat_t
	var err error
	if errC := conFd(d, func(fd int) { err = unix.Fstatat(fd, nome, &st, unix.AT_SYMLINK_NOFOLLOW) }); errC != nil {
		return voceCartella{}, errC
	}
	if err != nil {
		return voceCartella{}, &os.PathError{Op: "fstatat", Path: nome, Err: err}
	}
	return voceCartella{nome: nome, normale: st.Mode&unix.S_IFMT == unix.S_IFREG, byte: st.Size,
		id: identitaFile{dev: st.Dev, ino: st.Ino}, modif: st.Mtim.Nano(), cambio: st.Ctim.Nano()}, nil
}

// togliNome toglie il nome dalla cartella aperta (unlinkat, mai una cartella): la cartella è quella del
// lucchetto, anche se nel frattempo qualcuno ha cambiato il percorso.
func togliNome(d *os.File, nome string) error {
	var err error
	if errC := conFd(d, func(fd int) { err = unix.Unlinkat(fd, nome, 0) }); errC != nil {
		return errC
	}
	if err != nil {
		return &os.PathError{Op: "unlinkat", Path: nome, Err: err}
	}
	return nil
}

func conFd(d *os.File, f func(fd int)) error {
	rc, err := d.SyscallConn()
	if err != nil {
		return err
	}
	return rc.Control(func(fd uintptr) { f(int(fd)) })
}

// dellUtente dice se il file è dell'utente del programma (private_file dello standalone).
func dellUtente(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}

// nomiDi elenca i nomi della cartella aperta con un descrittore nuovo della stessa cartella (openat
// «.»): la lettura non dipende dalla posizione del descrittore del lucchetto e non segue il percorso. I nomi
// tornano in ordine alfabetico.
func nomiDi(d *os.File) ([]string, error) {
	var nfd int
	var err error
	if errC := conFd(d, func(fd int) {
		nfd, err = unix.Openat(fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	}); errC != nil {
		return nil, errC
	}
	if err != nil {
		return nil, fmt.Errorf("backup: elenco della destinazione: %w", err)
	}
	f := os.NewFile(uintptr(nfd), d.Name())
	defer func() { _ = f.Close() }() // sola lettura
	nomi, err := f.Readdirnames(-1)
	// In ordine: l'ordine di readdir dipende dal file system (tmpfs al contrario, ext4 per hash), e l'uscita del
	// comando deve essere la stessa ovunque.
	sort.Strings(nomi)
	return nomi, err
}
