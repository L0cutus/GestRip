package media

import (
	"os"

	"golang.org/x/sys/unix"
)

// ApriInLettura apre un file media in sola lettura senza seguire un link (O_NOFOLLOW) e senza
// restare appesa a una FIFO (O_NONBLOCK).
func ApriInLettura(p string) (*os.File, error) {
	return os.OpenFile(p, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
}
