package media

import (
	"context"
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// lucchettoFile prende il flock del file p (condiviso, o esclusivo con esclusivo), creandolo 0600 se
// manca, e dà la funzione che lo rilascia. Il flock vale per descrizione di file aperto: due
// aperture, anche nello stesso processo, si escludono come due processi. Si riprova senza bloccare
// (LOCK_NB) finché ctx non finisce.
func lucchettoFile(ctx context.Context, p string, esclusivo bool) (func(), error) {
	f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrLucchetto, err)
	}
	rc, err := f.SyscallConn()
	if err != nil {
		_ = f.Close() // mai usato: niente da perdere
		return nil, err
	}
	modo := unix.LOCK_SH
	if esclusivo {
		modo = unix.LOCK_EX
	}
	err = aspettaFinche(ctx, func() (bool, error) {
		var errFlock error
		if err := rc.Control(func(fd uintptr) { errFlock = unix.Flock(int(fd), modo|unix.LOCK_NB) }); err != nil {
			return false, err
		}
		switch {
		case errFlock == nil:
			return true, nil
		case errors.Is(errFlock, unix.EWOULDBLOCK), errors.Is(errFlock, unix.EINTR):
			return false, nil
		}
		return false, fmt.Errorf("%w: %w", ErrLucchetto, errFlock)
	})
	if err != nil {
		_ = f.Close() // il lucchetto non è preso: niente da perdere
		return nil, err
	}
	// Chiudere il file rilascia il flock; un errore di chiusura non lascia il lucchetto preso.
	return func() { _ = f.Close() }, nil
}
