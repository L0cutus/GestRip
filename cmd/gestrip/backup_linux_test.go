package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"golang.org/x/sys/unix"

	"github.com/L0cutus/gestrip/internal/backup"
	"github.com/L0cutus/gestrip/internal/clock"
)

// I comandi chiave-backup e backup (slice 120). Database inventati e vuoti, chiavi age generate nel test.

var oraChiave = clock.NuovoFinto(time.Date(2026, 11, 25, 2, 0, 0, 0, time.UTC))

// Con stdout che non è un terminale chiave-backup rifiuta prima di creare la chiave: una chiave che
// nessuno ha visto renderebbe illeggibili i backup.
func TestChiaveBackupConStdoutSuDevNull(t *testing.T) {
	p := databaseVuoto(t)
	cmd, righe := figlio(t, nil, "chiave-backup", "-db", p)
	var stderr strings.Builder
	for r := range righe {
		stderr.WriteString(r + "\n")
	}
	err := cmd.Wait()
	var uscita *exec.ExitError
	if !errors.As(err, &uscita) || uscita.ExitCode() != 1 {
		t.Errorf("uscita %v, attesa 1\nstderr:\n%s", err, stderr.String())
	}
	if want := "gestrip: la chiave dei backup si mostra solo su un terminale (non su un file, una pipe o il journal)\n"; stderr.String() != want {
		t.Errorf("stderr:\n%s", stderr.String())
	}
	if _, err := os.Lstat(backup.FileChiave(filepath.Dir(p))); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("la chiave è stata creata lo stesso: %v", err)
	}
}

// La chiave privata si mostra una volta sola: il secondo comando rifiuta e non la stampa.
func TestChiaveBackupMostrataUnaVolta(t *testing.T) {
	p := databaseVuoto(t)
	var out bytes.Buffer
	if err := chiaveBackup(opzioniChiave{DB: p}, &out, oraChiave); err != nil {
		t.Fatal(err)
	}
	id, err := backup.LeggiChiave(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out.String(), id.String()); n != 1 {
		t.Errorf("chiave privata stampata %d volte, attesa una\n%s", n, out.String())
	}
	if !strings.Contains(out.String(), id.Recipient().String()) || !strings.Contains(out.String(), "non verrà mostrata mai più") {
		t.Errorf("uscita:\n%s", out.String())
	}
	var di bytes.Buffer
	err = chiaveBackup(opzioniChiave{DB: p}, &di, oraChiave)
	if !errors.Is(err, backup.ErrChiaveEsistente) || di.Len() != 0 {
		t.Fatalf("seconda volta: %v, uscita %q", err, di.String())
	}
	if strings.Contains(err.Error(), "AGE-SECRET-KEY") {
		t.Error("l'errore contiene la chiave")
	}
	// Senza database non nasce niente accanto a un percorso sbagliato.
	manca := filepath.Join(t.TempDir(), "gestrip.db")
	if err := chiaveBackup(opzioniChiave{DB: manca}, &di, oraChiave); err == nil {
		t.Error("chiave creata senza database")
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(manca), backup.CartellaSegreti)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("secrets/ creata senza database: %v", err)
	}
}

type scrittoreRotto struct{}

func (scrittoreRotto) Write([]byte) (int, error) { return 0, errors.New("terminale chiuso inventato") }

// Se la chiave non si riesce a mostrare, si toglie: nessuno l'ha copiata.
func TestChiaveBackupNonMostrataSiToglie(t *testing.T) {
	p := databaseVuoto(t)
	if err := chiaveBackup(opzioniChiave{DB: p}, scrittoreRotto{}, oraChiave); err == nil {
		t.Fatal("nessun errore")
	}
	if _, err := os.Lstat(backup.FileChiave(filepath.Dir(p))); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("la chiave non mostrata è rimasta: %v", err)
	}
}

func TestOpzioniDelBackup(t *testing.T) {
	for _, a := range [][]string{nil, {"-cartella", ""}, {"-cartella", " "}, {"-cartella", "/x", "altro"}, {"-sconosciuta"}} {
		if _, err := opzioniDiBackup(a); !errors.Is(err, errUsoBackup) {
			t.Errorf("%q: %v", a, err)
		}
	}
	o, err := opzioniDiBackup([]string{"-cartella", "/mnt/chiavetta", "-db", "/d/g.db"})
	if err != nil || o != (opzioniBackup{DB: "/d/g.db", Cartella: "/mnt/chiavetta"}) {
		t.Errorf("%+v %v", o, err)
	}
	if _, err := opzioniDiChiave([]string{"altro"}); !errors.Is(err, errUsoChiave) {
		t.Errorf("chiave-backup con un argomento: %v", err)
	}
}

// Il comando backup scrive un archivio che si apre con la chiave dei backup, e dice nome, byte e
// SHA-256, mai la chiave. Usa lo statfs vero: serve il margine di 10 GiB nella cartella temporanea.
func TestBackupComando(t *testing.T) {
	p := databaseVuoto(t)
	var s unix.Statfs_t
	if err := unix.Statfs(filepath.Dir(p), &s); err != nil || s.Bavail*uint64(s.Bsize) < backup.MargineSpazio+1<<30 {
		t.Skip("meno di 11 GiB liberi nella cartella temporanea: il backup rifiuterebbe")
	}
	if err := chiaveBackup(opzioniChiave{DB: p}, &bytes.Buffer{}, oraChiave); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	var out bytes.Buffer
	if err := faiBackup(context.Background(), opzioniBackup{DB: p, Cartella: dest}, &out); err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`\Aarchivio scritto: (` + regexp.QuoteMeta(dest) + `/gestrip-backup-\d{8}T\d{6}Z\.tar\.age)\nbyte: \d+\nsha256: [0-9a-f]{64}\nmedia: 0\nnon ancora verificato\n\z`)
	m := re.FindStringSubmatch(out.String())
	if m == nil {
		t.Fatalf("uscita:\n%s", out.String())
	}
	id, err := backup.LeggiChiave(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(m[1])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := age.Decrypt(f, id); err != nil {
		t.Fatalf("l'archivio non si apre con la chiave dei backup: %v", err)
	}
	// Senza database il comando non crea niente.
	if err := faiBackup(context.Background(), opzioniBackup{DB: filepath.Join(t.TempDir(), "gestrip.db"), Cartella: dest}, &out); err == nil {
		t.Error("backup senza database")
	}
}
