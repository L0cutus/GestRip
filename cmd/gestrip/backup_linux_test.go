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

// databaseConChiave è databaseVuoto con la chiave dei backup; salta il test se la cartella temporanea
// non ha il margine di 10 GiB (backup e verifica usano lo statfs vero).
func databaseConChiave(t *testing.T) string {
	t.Helper()
	p := databaseVuoto(t)
	var s unix.Statfs_t
	if err := unix.Statfs(filepath.Dir(p), &s); err != nil || s.Bavail*uint64(s.Bsize) < backup.MargineSpazio+1<<30 {
		t.Skip("meno di 11 GiB liberi nella cartella temporanea: il backup rifiuterebbe")
	}
	if err := chiaveBackup(opzioniChiave{DB: p}, &bytes.Buffer{}, oraChiave); err != nil {
		t.Fatal(err)
	}
	return p
}

// Voce OPS-16, in parte: niente segno «inventario:» (la voce chiude con chiavetta, lavoro notturno e
// SFTP, 123-128).
// Il comando backup scrive un archivio che si apre con la chiave dei backup, lo verifica e solo dopo
// dice «backup riuscito»; dice nome, byte e SHA-256, mai la chiave. verifica-backup sullo stesso
// archivio dice lo stesso SHA-256.
func TestBackupComando(t *testing.T) {
	p := databaseConChiave(t)
	dest := t.TempDir()
	var out bytes.Buffer
	if err := faiBackup(context.Background(), opzioniBackup{DB: p, Cartella: dest}, &out); err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`\Aarchivio scritto: (` + regexp.QuoteMeta(dest) + `/gestrip-backup-\d{8}T\d{6}Z\.tar\.age)\nbyte: (\d+)\nsha256: ([0-9a-f]{64})\nmedia: 0\n` +
		`verificato: decifrato, 1 file con l'impronta giusta, database integro, \d+ tabelle con i conteggi giusti\nbackup riuscito\n\z`)
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
	var v bytes.Buffer
	if err := verificaBackup(context.Background(), opzioniVerificaBackup{DB: p, Archivio: m[1]}, &v); err != nil {
		t.Fatal(err)
	}
	re = regexp.MustCompile(`\Aarchivio: ` + regexp.QuoteMeta(m[1]) + `\nbyte: ` + m[2] + `\nsha256: ` + m[3] +
		`\ncatturato il: \d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{6}Z\nversione: .+\nmedia: 0\n` +
		`verificato: decifrato, 1 file con l'impronta giusta, database integro, \d+ tabelle con i conteggi giusti\n\z`)
	if !re.MatchString(v.String()) {
		t.Errorf("verifica-backup:\n%s", v.String())
	}
	if strings.Contains(out.String()+v.String(), "AGE-SECRET-KEY") {
		t.Error("la chiave privata nell'uscita")
	}
	if m, _ := filepath.Glob(filepath.Join(filepath.Dir(p), ".verifica-backup-*")); len(m) != 0 {
		t.Errorf("cartelle temporanee rimaste: %v", m)
	}
}

// Voce OPS-16, in parte (senza segno «inventario:», vedi TestBackupComando).
// Se la verifica non passa (o verifica un file diverso da quello scritto) il comando lo dice chiaro,
// non dice «riuscito», lascia l'archivio dov'è e dà un errore (uscita 1).
func TestBackupComandoVerificaFallita(t *testing.T) {
	p := databaseConChiave(t)
	vecchia := verificaArchivio
	t.Cleanup(func() { verificaArchivio = vecchia })
	for nome, f := range map[string]func(context.Context, string, string) (backup.Verificato, error){
		"verifica fallita": func(context.Context, string, string) (backup.Verificato, error) {
			return backup.Verificato{}, backup.ErrIntegrita
		},
		"altro file": func(ctx context.Context, dati, file string) (backup.Verificato, error) {
			v, err := vecchia(ctx, dati, file)
			v.SHA256 = strings.Repeat("0", 64)
			return v, err
		},
	} {
		t.Run(nome, func(t *testing.T) {
			verificaArchivio = f
			dest := t.TempDir()
			var out bytes.Buffer
			err := faiBackup(context.Background(), opzioniBackup{DB: p, Cartella: dest}, &out)
			if err == nil || !strings.Contains(err.Error(), "backup non riuscito") {
				t.Fatalf("errore %v", err)
			}
			if strings.Contains(out.String(), "backup riuscito") || !strings.Contains(out.String(), "BACKUP NON RIUSCITO") {
				t.Errorf("uscita:\n%s", out.String())
			}
			// L'archivio resta: nessun archivio si cancella qui.
			if v, _ := filepath.Glob(filepath.Join(dest, "gestrip-backup-*.tar.age")); len(v) != 1 || !strings.Contains(out.String(), v[0]) {
				t.Errorf("archivi nella destinazione %v", v)
			}
		})
	}
}

// verifica-backup su un file che non è un archivio, o senza database: errore, con «NON verificato».
func TestVerificaBackupComando(t *testing.T) {
	p := databaseConChiave(t)
	f := filepath.Join(t.TempDir(), "finto.tar.age")
	if err := os.WriteFile(f, []byte("non è un archivio age"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := verificaBackup(context.Background(), opzioniVerificaBackup{DB: p, Archivio: f}, &out)
	if err == nil || !strings.Contains(err.Error(), "NON verificato") || out.Len() != 0 {
		t.Errorf("errore %v, uscita %q", err, out.String())
	}
	if err := verificaBackup(context.Background(), opzioniVerificaBackup{DB: filepath.Join(t.TempDir(), "gestrip.db"), Archivio: f}, &out); err == nil {
		t.Error("verifica senza database")
	}
	for _, a := range [][]string{nil, {""}, {"a", "b"}, {"-sconosciuta", "a"}} {
		if _, err := opzioniDiVerificaBackup(a); !errors.Is(err, errUsoVerificaBackup) {
			t.Errorf("%q: %v", a, err)
		}
	}
	o, err := opzioniDiVerificaBackup([]string{"-db", "/d/g.db", "/mnt/a.tar.age"})
	if err != nil || o != (opzioniVerificaBackup{DB: "/d/g.db", Archivio: "/mnt/a.tar.age"}) {
		t.Errorf("%+v %v", o, err)
	}
}
