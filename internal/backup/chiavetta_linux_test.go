package backup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/L0cutus/gestrip/internal/db"
)

// La chiavetta (slice 123). Niente montaggi veri: il sistema (stat, /proc/self/mountinfo, permessi) è
// finto, le cartelle sono temporanee del test. Il lucchetto, gli elenchi e le cancellazioni sono veri.

// sistemaFinto dà a ogni percorso il dispositivo di dev (gli altri: errore), la tabella dei montaggi info e
// l'esito di scrivibile.
type sistemaFinto struct {
	dev           map[string]uint64
	devAperta     map[string]uint64 // fstat di una cartella aperta, se diverso dallo stat del percorso
	info          string
	errScrivibile error
}

func (s *sistemaFinto) identita(p string) (identitaFile, error) {
	d, ok := s.dev[p]
	if !ok {
		return identitaFile{}, &os.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
	}
	return identitaFile{dev: d, ino: 1}, nil
}
func (s *sistemaFinto) identitaAperta(f *os.File) (identitaFile, error) {
	if d, ok := s.devAperta[f.Name()]; ok {
		return identitaFile{dev: d, ino: 1}, nil
	}
	return s.identita(f.Name())
}
func (s *sistemaFinto) montaggi() ([]byte, error) { return []byte(s.info), nil }
func (s *sistemaFinto) scrivibile(string) error   { return s.errScrivibile }

// scena è una chiavetta finta: dati, la cartella sopra la chiavetta e la chiavetta, con un sistema che la
// dice montata da /dev/sdb1 (exfat) e i dati sul disco interno.
type scena struct {
	dati, sopra, chiavetta string
	sis                    *sistemaFinto
}

func rigaRadice() string {
	return "22 1 259:2 / / rw,relatime shared:1 - ext4 /dev/nvme0n1p2 rw\n"
}

func rigaChiavetta(punto string) string {
	return "40 22 8:17 / " + strings.ReplaceAll(punto, " ", `\040`) + " rw,nosuid,nodev,relatime shared:2 - exfat /dev/sdb1 rw,uid=1000\n"
}

func nuovaScena(t *testing.T, nome string) *scena {
	t.Helper()
	dati, err := percorsoVero(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sopra, err := percorsoVero(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ch := filepath.Join(sopra, nome)
	if err := os.Mkdir(ch, 0o700); err != nil {
		t.Fatal(err)
	}
	s := &scena{dati: dati, sopra: sopra, chiavetta: ch, sis: &sistemaFinto{
		dev:  map[string]uint64{dati: 1, sopra: 1, ch: 7},
		info: rigaRadice() + rigaChiavetta(ch),
	}}
	vecchio := sistemaMontaggi
	sistemaMontaggi = s.sis
	t.Cleanup(func() { sistemaMontaggi = vecchio })
	return s
}

// Voce OPS-16, in parte: niente segno «inventario:» (la voce chiude con lavoro notturno e SFTP, 124-128).
// La chiavetta passa solo se è un punto di montaggio, su un dispositivo a blocchi diverso da quello dei dati,
// scrivibile: ogni altro caso è un rifiuto con il suo errore.
func TestControllaChiavetta(t *testing.T) {
	casi := []struct {
		nome   string
		cambia func(s *scena)
		err    error
	}{
		{"buona", func(*scena) {}, nil},
		{"non un punto di montaggio (stesso dispositivo della cartella sopra)", func(s *scena) { s.sis.dev[s.chiavetta] = 1; s.sis.dev[s.dati] = 3 }, ErrNonMontata},
		{"dispositivo diverso ma non nella tabella", func(s *scena) { s.sis.info = rigaRadice() }, ErrNonMontata},
		{"tabella con un'altra cartella", func(s *scena) { s.sis.info = rigaRadice() + rigaChiavetta(s.chiavetta+"x") }, ErrNonMontata},
		{"stesso dispositivo dei dati", func(s *scena) { s.sis.dev[s.dati] = 7 }, ErrStessoDisco},
		{"bind mount del disco dei dati", func(s *scena) {
			s.sis.info = rigaRadice() + "40 22 259:2 /home/x " + s.chiavetta + " rw shared:2 - ext4 /dev/nvme0n1p2 rw\n"
		}, ErrStessoDisco},
		{"stessa sorgente dei dati", func(s *scena) {
			s.sis.info = rigaRadice() + "40 22 0:51 / " + s.chiavetta + " rw shared:2 - btrfs /dev/nvme0n1p2 rw\n"
		}, ErrStessoDisco},
		{"tmpfs", func(s *scena) { s.sis.info = rigaRadice() + "40 22 0:50 / " + s.chiavetta + " rw - tmpfs tmpfs rw\n" }, ErrNonChiavetta},
		{"cartella di rete", func(s *scena) {
			s.sis.info = rigaRadice() + "40 22 0:52 / " + s.chiavetta + " rw - cifs //server/backup rw\n"
		}, ErrNonChiavetta},
		{"montaggio sopra un altro: vince l'ultimo (tmpfs)", func(s *scena) {
			s.sis.info += "41 40 0:50 / " + s.chiavetta + " rw - tmpfs tmpfs rw\n"
		}, ErrNonChiavetta},
		{"sola lettura", func(s *scena) {
			s.sis.info = rigaRadice() + strings.Replace(rigaChiavetta(s.chiavetta), " rw,", " ro,", 1)
		}, ErrChiavettaSolaLettura},
		{"non scrivibile", func(s *scena) { s.sis.errScrivibile = errors.New("EACCES finto") }, ErrChiavettaSolaLettura},
		{"tabella illeggibile", func(s *scena) { s.sis.info = "riga sbagliata\n" }, nil},
		{"stat che non riesce", func(s *scena) { delete(s.sis.dev, s.sopra) }, ErrDestinazione},
		{"dentro la cartella dei dati", func(s *scena) {
			p := filepath.Join(s.dati, "chiavetta")
			if err := os.Mkdir(p, 0o700); err != nil {
				panic(err)
			}
			s.chiavetta = p
		}, ErrDestinazione},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			s := nuovaScena(t, "chiavetta")
			c.cambia(s)
			k, err := controllaChiavetta(s.sis, s.chiavetta, s.dati)
			switch {
			case c.nome == "buona":
				if err != nil || k.Cartella != s.chiavetta || k.TipoFS != "exfat" || k.Sorgente != "/dev/sdb1" || k.dev != 7 {
					t.Fatalf("%+v %v", k, err)
				}
			case c.err == nil:
				if err == nil {
					t.Fatal("passata")
				}
			case !errors.Is(err, c.err):
				t.Fatalf("errore %v, atteso %v", err, c.err)
			}
		})
	}
}

// Il punto di montaggio con uno spazio (\040 nella tabella) e una chiavetta raggiunta con un collegamento:
// si confronta il percorso vero, e quello finisce nel registro.
func TestControllaChiavettaPercorsi(t *testing.T) {
	s := nuovaScena(t, "chiave tta")
	k, err := controllaChiavetta(s.sis, s.chiavetta, s.dati)
	if err != nil || k.Cartella != s.chiavetta {
		t.Fatalf("con lo spazio: %+v %v", k, err)
	}
	link := filepath.Join(t.TempDir(), "usb")
	if err := os.Symlink(s.chiavetta, link); err != nil {
		t.Fatal(err)
	}
	k, err = controllaChiavetta(s.sis, link, s.dati)
	if err != nil || k.Cartella != s.chiavetta {
		t.Fatalf("dietro un collegamento: %+v %v", k, err)
	}
	if _, err := controllaChiavetta(s.sis, "/", s.dati); err == nil {
		t.Error("la radice passa")
	}
	for _, info := range []string{"", "1 2 3\n", rigaRadice() + `40 22 8:17 / /a\04 rw - exfat /dev/sdb1 rw` + "\n",
		rigaRadice() + `40 22 8:17 / /a\999 rw - exfat /dev/sdb1 rw` + "\n", "40 22 8:17 / /a rw x y z w\n"} {
		if _, err := leggiMontaggi([]byte(info)); err == nil {
			t.Errorf("tabella %q letta", info)
		}
	}
}

// Il lucchetto della destinazione: un secondo backup nella stessa cartella (chiavetta o cartella) rifiuta
// finché il primo non la rilascia.
func TestDestinazioneOccupata(t *testing.T) {
	s := nuovaScena(t, "chiavetta")
	d, err := ApriChiavetta(s.chiavetta, s.dati)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Chiavetta() {
		t.Error("non è una chiavetta")
	}
	if _, err := ApriChiavetta(s.chiavetta, s.dati); !errors.Is(err, ErrDestinazioneOccupata) {
		t.Errorf("seconda chiavetta: %v", err)
	}
	if _, err := BloccaDestinazione(s.chiavetta, s.dati); !errors.Is(err, ErrDestinazioneOccupata) {
		t.Errorf("cartella sulla chiavetta: %v", err)
	}
	if err := d.Chiudi(); err != nil {
		t.Fatal(err)
	}
	c, err := BloccaDestinazione(s.chiavetta, s.dati)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Chiudi() }()
	if c.Chiavetta() {
		t.Error("-cartella detta chiavetta")
	}
	if _, err := BloccaDestinazione(s.dati, s.dati); !errors.Is(err, ErrDestinazione) {
		t.Errorf("la cartella dei dati: %v", err)
	}
}

// Voce OPS-16, in parte (senza segno «inventario:», vedi TestControllaChiavetta).
// Nel registro va «chiavetta» solo se alla fine la chiavetta è ancora quella con l'archivio dentro; una
// cartella qualsiasi e una chiavetta cambiata danno «cartella».
func TestDestinazioneDaRegistrare(t *testing.T) {
	s := nuovaScena(t, "chiavetta")
	file := filepath.Join(s.chiavetta, NomeArchivio(istanteChiave))
	if err := os.WriteFile(file, []byte("archivio finto"), 0o600); err != nil {
		t.Fatal(err)
	}
	casi := []struct {
		nome   string
		cambia func() string // dà il file da confermare
		ok     bool
	}{
		{"la stessa chiavetta", func() string { return file }, true},
		{"un'altra chiavetta al suo posto", func() string { s.sis.dev[s.chiavetta] = 8; return file }, false},
		{"un'altra sorgente al suo posto", func() string {
			s.sis.info = rigaRadice() + strings.Replace(rigaChiavetta(s.chiavetta), "/dev/sdb1", "/dev/sdc1", 1)
			return file
		}, false},
		{"tolta (non più montata)", func() string { s.sis.info = rigaRadice(); return file }, false},
		{"archivio fuori dalla chiavetta", func() string { return filepath.Join(s.sopra, filepath.Base(file)) }, false},
		{"archivio che non c'è", func() string { return file + "x" }, false},
		{"archivio scambiato con un collegamento", func() string {
			l := filepath.Join(s.chiavetta, "collegamento.tar.age")
			if err := os.Symlink(file, l); err != nil {
				t.Fatal(err)
			}
			return l
		}, false},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			s.sis.dev[s.chiavetta] = 7
			s.sis.info = rigaRadice() + rigaChiavetta(s.chiavetta)
			d, err := ApriChiavetta(s.chiavetta, s.dati)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = d.Chiudi() }()
			f := c.cambia()
			dest, err := d.DestinazioneDaRegistrare(f)
			if c.ok && (dest != db.DestinazioneChiavetta || err != nil) {
				t.Fatalf("%s %v", dest, err)
			}
			if !c.ok && (dest != db.DestinazioneCartella || !errors.Is(err, ErrChiavettaCambiata)) {
				t.Fatalf("%s %v", dest, err)
			}
		})
	}
	s.sis.dev[s.chiavetta] = 7
	s.sis.info = rigaRadice() + rigaChiavetta(s.chiavetta)
	c, err := BloccaDestinazione(s.chiavetta, s.dati)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Chiudi() }()
	if dest, err := c.DestinazioneDaRegistrare(file); dest != db.DestinazioneCartella || err != nil {
		t.Errorf("-cartella: %s %v", dest, err)
	}
	if err := c.ConfermaChiavetta(file); !errors.Is(err, ErrChiavettaCambiata) {
		t.Errorf("-cartella confermata come chiavetta: %v", err)
	}
}

// I .pending rimasti si tolgono solo con il nome esatto di Cifra e solo se sono file normali: archivi,
// altri file, nomi quasi uguali, date impossibili, collegamenti (e il file a cui puntano) e cartelle
// restano.
func TestPulisciPending(t *testing.T) {
	s := nuovaScena(t, "chiavetta")
	fuori := filepath.Join(t.TempDir(), "file di un altro")
	scrivi := func(n string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(s.chiavetta, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(fuori, []byte("da non toccare"), 0o600); err != nil {
		t.Fatal(err)
	}
	nostri := []string{"gestrip-backup-20261124T013000Z.tar.age.pending", "gestrip-backup-20261125T013000Z.tar.age.pending"}
	for _, n := range nostri {
		scrivi(n)
	}
	restano := []string{"gestrip-backup-20261125T013000Z.tar.age", "foto.pending", "gestrip-backup-20261125T013000Z.tar.age.pending.x",
		"xgestrip-backup-20261125T013000Z.tar.age.pending", "gestrip-backup-20261340T013000Z.tar.age.pending",
		"gestrip-backup-20260229T013000Z.tar.age.pending", "gestrip-backup-20261125T013000.tar.age.pending"}
	for _, n := range restano {
		scrivi(n)
	}
	link := "gestrip-backup-20261123T013000Z.tar.age.pending"
	if err := os.Symlink(fuori, filepath.Join(s.chiavetta, link)); err != nil {
		t.Fatal(err)
	}
	cart := "gestrip-backup-20261122T013000Z.tar.age.pending"
	if err := os.Mkdir(filepath.Join(s.chiavetta, cart), 0o700); err != nil {
		t.Fatal(err)
	}
	d, err := ApriChiavetta(s.chiavetta, s.dati)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Chiudi() }()
	tolti, err := d.PulisciPending()
	if err != nil || fmt.Sprint(tolti) != fmt.Sprint(nostri) {
		t.Fatalf("tolti %v, %v", tolti, err)
	}
	for _, n := range append(restano, link, cart) {
		if _, err := os.Lstat(filepath.Join(s.chiavetta, n)); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
	if b, err := os.ReadFile(fuori); err != nil || string(b) != "da non toccare" {
		t.Errorf("file fuori: %q %v", b, err)
	}
	// Un errore di cancellazione ferma la pulizia e si dice.
	scrivi(nostri[0])
	scrivi(nostri[1])
	vecchio := togliVoce
	t.Cleanup(func() { togliVoce = vecchio })
	togliVoce = func(*os.File, string) error { return errors.New("EIO finto") }
	if tolti, err := d.PulisciPending(); err == nil || len(tolti) != 0 {
		t.Errorf("con un errore: %v %v", tolti, err)
	}
}

// Nota 6c: dove renameat2 con RENAME_NOREPLACE dà EINVAL o ENOSYS (FUSE senza rename2, CIFS) si ripiega su
// link + unlink, che non sovrascrive mai; se nemmeno link si può, un errore chiaro. Un altro errore di
// renameat2 non ripiega.
func TestRinominaSenzaRename2(t *testing.T) {
	vr, vl := renameat2, linkat
	t.Cleanup(func() { renameat2, linkat = vr, vl })
	dir := t.TempDir()
	prepara := func(t *testing.T) (string, string) {
		t.Helper()
		da, a := filepath.Join(dir, "x.pending"), filepath.Join(dir, "x")
		_ = os.Remove(a)
		if err := os.WriteFile(da, []byte("archivio nuovo"), 0o600); err != nil {
			t.Fatal(err)
		}
		return da, a
	}
	for _, e := range []error{unix.EINVAL, unix.ENOSYS} {
		renameat2 = func(int, string, int, string, uint) error { return e }
		linkat = vl
		da, a := prepara(t)
		if err := rinominaSenzaSovrascrivere(da, a); err != nil {
			t.Fatalf("%v: %v", e, err)
		}
		if b, err := os.ReadFile(a); err != nil || string(b) != "archivio nuovo" {
			t.Errorf("%v: %q %v", e, b, err)
		}
		if _, err := os.Lstat(da); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%v: il vecchio nome resta: %v", e, err)
		}
		// Il nome finale c'è già: link non sovrascrive.
		da, a = prepara(t)
		if err := os.WriteFile(a, []byte("archivio vecchio"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := rinominaSenzaSovrascrivere(da, a); !errors.Is(err, fs.ErrExist) {
			t.Errorf("%v sopra un file: %v", e, err)
		}
		if b, _ := os.ReadFile(a); string(b) != "archivio vecchio" {
			t.Errorf("%v: sovrascritto: %q", e, b)
		}
	}
	renameat2 = func(int, string, int, string, uint) error { return unix.EINVAL }
	linkat = func(int, string, int, string, int) error { return unix.EPERM }
	da, a := prepara(t)
	err := rinominaSenzaSovrascrivere(da, a)
	if !errors.Is(err, ErrRinominaNonSupportata) || !strings.Contains(err.Error(), "exFAT o ext4") {
		t.Errorf("senza link: %v", err)
	}
	if _, err := os.Lstat(a); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("senza link il nome finale c'è: %v", err)
	}
	chiamato := false
	renameat2 = func(int, string, int, string, uint) error { return unix.EACCES }
	linkat = func(int, string, int, string, int) error { chiamato = true; return nil }
	if err := rinominaSenzaSovrascrivere(da, a); !errors.Is(err, unix.EACCES) || chiamato {
		t.Errorf("EACCES: %v, link chiamato %v", err, chiamato)
	}
}

// Nota 6c dentro Cifra: con un file system che non sa rinominare senza sovrascrivere il backup fallisce con
// il messaggio chiaro e il .pending si toglie.
func TestCifraSenzaRename2(t *testing.T) {
	p := nuovaProva(t)
	conChiave(t, p)
	vr, vl := renameat2, linkat
	t.Cleanup(func() { renameat2, linkat = vr, vl })
	renameat2 = func(int, string, int, string, uint) error { return unix.EINVAL }
	linkat = func(int, string, int, string, int) error { return unix.EOPNOTSUPP }
	dest := t.TempDir()
	if _, err := Cifra(context.Background(), p.s, dest); !errors.Is(err, ErrRinominaNonSupportata) {
		t.Fatalf("%v", err)
	}
	if v := voceDi(t, dest); len(v) != 0 {
		t.Errorf("restano %v", v)
	}
}

// Nota 7b: dopo l'fsync Cifra chiede al kernel di dimenticare le pagine dell'archivio (fadvise DONTNEED),
// sul file completo, prima della rinomina; se il kernel rifiuta, il backup va avanti lo stesso.
func TestCifraScaricaLaCache(t *testing.T) {
	p := nuovaProva(t)
	conChiave(t, p)
	vecchio := scaricaCache
	t.Cleanup(func() { scaricaCache = vecchio })
	var visti []string
	var byte int64
	scaricaCache = func(f *os.File) error {
		fi, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		visti, byte = append(visti, filepath.Base(f.Name())), fi.Size()
		return vecchio(f) // quella vera, sul file vero
	}
	r, err := Cifra(context.Background(), p.s, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(visti) != 1 || visti[0] != filepath.Base(r.File)+SuffissoInCorso || byte != r.ByteCifrati {
		t.Errorf("fadvise su %v con %d byte, archivio di %d", visti, byte, r.ByteCifrati)
	}
	scaricaCache = func(*os.File) error { return errors.New("fadvise rifiutato finto") }
	if _, err := Cifra(context.Background(), p.s, t.TempDir()); err != nil {
		t.Errorf("un fadvise rifiutato ferma il backup: %v", err)
	}
}

// Revisione della 123 (R11): la chiavetta smontata fra il controllo e l'apertura lascia aprire la cartella
// vuota del punto di montaggio sul disco dei dati. ApriChiavetta lo vede dal dispositivo della cartella
// aperta; ConfermaChiavetta lo rivede alla fine, anche se la tabella dei montaggi e lo stat del percorso dicono
// di nuovo «chiavetta» (rimontata con lo stesso maggiore:minore).
func TestChiavettaSmontataPrimaDelLucchetto(t *testing.T) {
	s := nuovaScena(t, "chiavetta")
	s.sis.devAperta = map[string]uint64{s.chiavetta: 1}
	if d, err := ApriChiavetta(s.chiavetta, s.dati); !errors.Is(err, ErrChiavettaCambiata) {
		if d != nil {
			_ = d.Chiudi()
		}
		t.Fatalf("aperta la cartella del disco dei dati: %v", err)
	}
	// Il lucchetto non resta preso.
	s.sis.devAperta = nil
	d, err := ApriChiavetta(s.chiavetta, s.dati)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Chiudi() }()
	file := filepath.Join(s.chiavetta, NomeArchivio(istanteChiave))
	if err := os.WriteFile(file, []byte("archivio finto"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.sis.devAperta = map[string]uint64{s.chiavetta: 1}
	if dest, err := d.DestinazioneDaRegistrare(file); dest != db.DestinazioneCartella || !errors.Is(err, ErrChiavettaCambiata) {
		t.Errorf("registrata come %s: %v", dest, err)
	}
}

// Revisione della 123 (R14): un .pending con il nostro nome su un altro dispositivo (per esempio un
// montaggio dentro la chiavetta) non si tocca.
func TestPulisciPendingAltroDispositivo(t *testing.T) {
	s := nuovaScena(t, "chiavetta")
	altro := "gestrip-backup-20261124T013000Z.tar.age.pending"
	nostro := "gestrip-backup-20261125T013000Z.tar.age.pending"
	for _, n := range []string{altro, nostro} {
		if err := os.WriteFile(filepath.Join(s.chiavetta, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	vecchio := guardaVoce
	t.Cleanup(func() { guardaVoce = vecchio })
	guardaVoce = func(d *os.File, n string) (voceCartella, error) {
		v, err := vecchio(d, n)
		if n == altro {
			v.id.dev++
		}
		return v, err
	}
	d, err := ApriChiavetta(s.chiavetta, s.dati)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Chiudi() }()
	if tolti, err := d.PulisciPending(); err != nil || len(tolti) != 1 || tolti[0] != nostro {
		t.Errorf("tolti %v %v", tolti, err)
	}
	if _, err := os.Lstat(filepath.Join(s.chiavetta, altro)); err != nil {
		t.Errorf("tolto quello dell'altro dispositivo: %v", err)
	}
}
