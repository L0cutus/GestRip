//go:build unix

package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// permessi dà i permessi di un file, o os.ModeIrregular se non c'è.
func permessi(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Lstat(p)
	if os.IsNotExist(err) {
		return os.ModeIrregular
	}
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

// conUmask fa girare f con la umask data (quella di un servizio qualunque, 022) e poi la rimette.
func conUmask(t *testing.T, m int, f func()) {
	t.Helper()
	vecchia := syscall.Umask(m)
	defer syscall.Umask(vecchia)
	f()
}

// inventario: OPS-12
// Il database nuovo nasce 0600, e così il -wal e lo -shm che SQLite crea accanto, anche con la
// umask 022 (lo standalone metteva il file a 0600 con chmod a ogni avvio).
func TestApriDatabaseNuovoPrivato(t *testing.T) {
	p := filepath.Join(t.TempDir(), "gestrip.db")
	conUmask(t, 0o022, func() {
		d, err := Apri(p)
		if err != nil {
			t.Fatal(err)
		}
		defer chiudi(t, d)
		if _, err := d.Scrittura.Exec(`CREATE TABLE prova (x)`); err != nil {
			t.Fatal(err)
		}
		var uno int
		if err := d.Lettura.QueryRow(`SELECT count(*) FROM prova`).Scan(&uno); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{p, p + "-wal", p + "-shm"} {
			if got := permessi(t, f); got != 0o600 {
				t.Errorf("%s: %v, atteso 0600", filepath.Base(f), got)
			}
		}
	})
}

// inventario: OPS-12
// Un database che c'è già con permessi larghi (anche il -wal e lo -shm rimasti) torna a 0600
// all'apertura, prima che SQLite lo legga.
func TestApriRiportaIlDatabaseA0600(t *testing.T) {
	p := filepath.Join(t.TempDir(), "gestrip.db")
	vecchio, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vecchio.Exec(`CREATE TABLE prova (x)`); err != nil {
		t.Fatal(err)
	}
	if err := vecchio.Close(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{p, p + "-wal", p + "-shm"} {
		if _, err := os.Stat(f); os.IsNotExist(err) {
			if err := os.WriteFile(f, nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Chmod(f, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	d, err := Apri(p)
	if err != nil {
		t.Fatal(err)
	}
	defer chiudi(t, d)
	for _, f := range []string{p, p + "-wal", p + "-shm"} {
		if got := permessi(t, f); got != 0o600 {
			t.Errorf("%s: %v, atteso 0600", filepath.Base(f), got)
		}
	}
}

// inventario: OPS-12
// Il percorso del database deve essere un file regolare (empty_or_missing? dello standalone).
func TestApriRifiutaUnPercorsoNonRegolare(t *testing.T) {
	cartella := filepath.Join(t.TempDir(), "gestrip.db")
	if err := os.Mkdir(cartella, 0o700); err != nil {
		t.Fatal(err)
	}
	if d, err := Apri(cartella); err == nil {
		chiudi(t, d)
		t.Error("una cartella aperta come database")
	}
	fifo := filepath.Join(t.TempDir(), "gestrip.db")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if d, err := Apri(fifo); err == nil {
		chiudi(t, d)
		t.Error("una FIFO aperta come database")
	}
}

// inventario: OPS-12
// Il database non è un link, nemmeno pendente (che con O_CREATE farebbe nascere il bersaglio
// altrove), e il -wal e lo -shm che ci sono non sono link: errore, non saltati. Nello standalone un
// database link era accettato.
func TestApriRifiutaILink(t *testing.T) {
	altrove := filepath.Join(t.TempDir(), "vero.db")
	if err := os.WriteFile(altrove, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "gestrip.db")
	if err := os.Symlink(altrove, link); err != nil {
		t.Fatal(err)
	}
	if d, err := Apri(link); err == nil || !strings.Contains(err.Error(), "è un link") {
		if d != nil {
			chiudi(t, d)
		}
		t.Errorf("database link: %v", err)
	}
	bersaglio := filepath.Join(t.TempDir(), "non-ancora.db")
	pendente := filepath.Join(t.TempDir(), "gestrip.db")
	if err := os.Symlink(bersaglio, pendente); err != nil {
		t.Fatal(err)
	}
	if d, err := Apri(pendente); err == nil {
		chiudi(t, d)
		t.Error("link pendente accettato")
	}
	if _, err := os.Lstat(bersaglio); err == nil {
		t.Error("il link pendente ha creato il bersaglio")
	}
	for _, accanto := range []string{"-wal", "-shm"} {
		p := filepath.Join(t.TempDir(), "gestrip.db")
		if err := os.Symlink(altrove, p+accanto); err != nil {
			t.Fatal(err)
		}
		if d, err := Apri(p); err == nil || !strings.Contains(err.Error(), "è un link") {
			if d != nil {
				chiudi(t, d)
			}
			t.Errorf("%s link: %v", accanto, err)
		}
	}
}

// inventario: OPS-12
// Un -wal o uno -shm che non è un file regolare (FIFO, cartella) ferma l'apertura.
func TestApriRifiutaAccantoNonRegolari(t *testing.T) {
	for _, accanto := range []string{"-wal", "-shm"} {
		for nome, crea := range map[string]func(string) error{
			"fifo":     func(p string) error { return syscall.Mkfifo(p, 0o600) },
			"cartella": func(p string) error { return os.Mkdir(p, 0o700) },
		} {
			p := filepath.Join(t.TempDir(), "gestrip.db")
			if err := crea(p + accanto); err != nil {
				t.Fatal(err)
			}
			if d, err := Apri(p); err == nil || !strings.Contains(err.Error(), "file regolare") {
				if d != nil {
					chiudi(t, d)
				}
				t.Errorf("%s %s: %v", accanto, nome, err)
			}
		}
	}
}

// inventario: OPS-12
// privato rimette a 0600 il database e il -wal e lo -shm rimasti prima che SQLite li apra, e crea
// il database 0600 se manca.
func TestPrivato(t *testing.T) {
	p := filepath.Join(t.TempDir(), "gestrip.db")
	conUmask(t, 0o022, func() {
		if err := privato(p); err != nil {
			t.Fatal(err)
		}
	})
	if got := permessi(t, p); got != 0o600 {
		t.Errorf("database nuovo: %v", got)
	}
	for _, f := range []string{p, p + "-wal", p + "-shm"} {
		if err := os.WriteFile(f, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(f, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := privato(p); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{p, p + "-wal", p + "-shm"} {
		if got := permessi(t, f); got != 0o600 {
			t.Errorf("%s: %v, atteso 0600", filepath.Base(f), got)
		}
	}
	// Anche solo il gruppo (0640) torna a 0600.
	for _, f := range []string{p, p + "-wal", p + "-shm"} {
		if err := os.Chmod(f, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	if err := privato(p); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{p, p + "-wal", p + "-shm"} {
		if got := permessi(t, f); got != 0o600 {
			t.Errorf("%s da 0640: %v, atteso 0600", filepath.Base(f), got)
		}
	}
	// Uno solo dei due accanto.
	q := filepath.Join(t.TempDir(), "gestrip.db")
	if err := os.WriteFile(q+"-shm", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(q+"-shm", 0o644); err != nil {
		t.Fatal(err)
	}
	if err := privato(q); err != nil {
		t.Fatal(err)
	}
	if got := permessi(t, q+"-shm"); got != 0o600 {
		t.Errorf("-shm da solo: %v", got)
	}
	if got := permessi(t, q+"-wal"); got != os.ModeIrregular {
		t.Errorf("-wal creato da privato: %v", got)
	}
}
