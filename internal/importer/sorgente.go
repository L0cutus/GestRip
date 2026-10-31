package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// I file che SQLite tiene accanto a un database aperto o chiuso male: con uno di questi la
// sorgente non è una copia a riposo, e una parte dei dati potrebbe stare lì (OPS-01).
var fileAccanto = []string{"-journal", "-wal", "-shm"}

// senzaPercorso toglie il percorso da un errore del file system: i messaggi del comando e il
// registro non nominano mai la sorgente, che sull'apparecchio può avere un nome vero.
func senzaPercorso(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Err
	}
	return err
}

// controllaAccanto rifiuta la sorgente se accanto c'è un -journal, un -wal o uno -shm, anche solo
// illeggibile (come reject_database_sidecars dello standalone).
func controllaAccanto(sorgente string) error {
	for _, s := range fileAccanto {
		_, err := os.Lstat(sorgente + s)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err == nil:
			return fmt.Errorf("accanto alla sorgente c'è un file %s: il database non è a riposo (chiudi lo standalone o fai una copia pulita)", s)
		default:
			return fmt.Errorf("il file %s accanto alla sorgente non si legge (%v): rifiuto", s, senzaPercorso(err))
		}
	}
	return nil
}

// copiaSorgente copia la sorgente (un file regolare, non un link) in dest, un file nuovo 0600, e
// ne dà dimensione e SHA-256 calcolati sui byte copiati. La sorgente si apre solo in lettura.
func copiaSorgente(sorgente, dest string) (n int64, impronta [32]byte, err error) {
	prima, err := os.Lstat(sorgente)
	if err != nil {
		return 0, impronta, fmt.Errorf("sorgente: %w", senzaPercorso(err))
	}
	if !prima.Mode().IsRegular() {
		return 0, impronta, errors.New("la sorgente non è un file regolare (un link o una cartella non vale)")
	}
	in, err := os.Open(sorgente)
	if err != nil {
		return 0, impronta, fmt.Errorf("sorgente: %w", senzaPercorso(err))
	}
	defer func() { _ = in.Close() }() // in sola lettura: niente da perdere
	aperto, err := in.Stat()
	if err != nil {
		return 0, impronta, fmt.Errorf("sorgente: %w", senzaPercorso(err))
	}
	if !os.SameFile(prima, aperto) {
		return 0, impronta, errors.New("la sorgente è cambiata mentre la aprivo: rifiuto")
	}
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, impronta, fmt.Errorf("copia della sorgente: %w", err)
	}
	h := sha256.New()
	n, err = io.Copy(io.MultiWriter(out, h), in)
	if err == nil {
		err = out.Sync()
	}
	if err = errors.Join(err, out.Close()); err != nil {
		return 0, impronta, fmt.Errorf("copia della sorgente: %w", senzaPercorso(err))
	}
	copy(impronta[:], h.Sum(nil))
	return n, impronta, nil
}

// impronta dichiarata: 64 cifre esadecimali.
func leggiImpronta(s string) ([32]byte, error) {
	var x [32]byte
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 32 {
		return x, errors.New("-sha256 deve essere lo SHA-256 della sorgente, 64 cifre esadecimali")
	}
	copy(x[:], b)
	return x, nil
}

// nonEsiste rifiuta un percorso che c'è già, anche come link pendente.
func nonEsiste(cosa, p string) error {
	_, err := os.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err == nil:
		return fmt.Errorf("%s %s esiste già: l'import non sovrascrive mai", cosa, p)
	default:
		return fmt.Errorf("%s %s: %w", cosa, p, err)
	}
}

// cartellaDi dà la cartella di p, che deve esistere ed essere una cartella.
func cartellaDi(cosa, p string) (string, error) {
	dir := filepath.Dir(p)
	fi, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("la cartella del %s %s non c'è: %w", cosa, dir, senzaPercorso(err))
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("%s non è una cartella", dir)
	}
	return dir, nil
}

// margineSpazio è il margine di spazio libero oltre alle copie (OPS-07, Stefano 31/10/2026).
const margineSpazio = 1 << 30

// spazioLibero dà i byte liberi per l'utente nella cartella (statfs: Bavail × Bsize). Una
// variabile per i test.
var spazioLibero = spazioLiberoDi

// controllaSpazio rifiuta l'import prima di copiare se nella cartella della destinazione non ci
// sono almeno due volte la sorgente (la copia di lavoro e il database nuovo), più i file della
// cartella media (con -media), più 1 GiB di margine (OPS-07).
func controllaSpazio(o Opzioni, dir string) error {
	fi, err := os.Lstat(o.Sorgente)
	if err != nil {
		return fmt.Errorf("sorgente: %w", senzaPercorso(err))
	}
	serve := 2*fi.Size() + margineSpazio
	if o.Media != "" {
		n, err := byteNellaCartella(o.Media)
		if err != nil {
			return fmt.Errorf("cartella media della sorgente: %w", senzaPercorso(err))
		}
		serve += n
	}
	libero, err := spazioLibero(dir)
	if err != nil {
		return fmt.Errorf("lo spazio libero in %s non si legge: %w", dir, err)
	}
	if libero < uint64(serve) {
		return fmt.Errorf("spazio libero insufficiente in %s: servono almeno %d byte (2 × la sorgente, la cartella media e 1 GiB di margine), ci sono %d byte", dir, serve, libero)
	}
	return nil
}

// byteNellaCartella somma la dimensione dei file regolari nella cartella (non nelle sottocartelle,
// senza seguire i link): i file media dello standalone stanno lì, uno per indirizzo.
func byteNellaCartella(dir string) (int64, error) {
	voci, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	var n int64
	for _, v := range voci {
		fi, err := v.Info()
		if err != nil {
			return 0, err
		}
		if fi.Mode().IsRegular() {
			n += fi.Size()
		}
	}
	return n, nil
}
