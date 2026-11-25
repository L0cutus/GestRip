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
	"github.com/L0cutus/gestrip/internal/db"
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
	for _, a := range [][]string{nil, {"-cartella", ""}, {"-cartella", " "}, {"-cartella", "/x", "altro"}, {"-sconosciuta"},
		{"-chiavetta", " "}, {"-cartella", "/x", "-chiavetta", "/y"}} {
		if _, err := opzioniDiBackup(a); !errors.Is(err, errUsoBackup) {
			t.Errorf("%q: %v", a, err)
		}
	}
	o, err := opzioniDiBackup([]string{"-cartella", "/mnt/chiavetta", "-db", "/d/g.db"})
	if err != nil || o != (opzioniBackup{DB: "/d/g.db", Cartella: "/mnt/chiavetta"}) {
		t.Errorf("%+v %v", o, err)
	}
	o, err = opzioniDiBackup([]string{"-chiavetta", "/media/chiavetta"})
	if err != nil || o != (opzioniBackup{DB: "/var/lib/gestrip/gestrip.db", Chiavetta: "/media/chiavetta"}) {
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
		`verificato: decifrato, 1 file con l'impronta giusta, database integro, \d+ tabelle con i conteggi giusti\n` +
		`registrato nel registro dei backup verificati\nbackup riuscito\n\z`)
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
	// Una riga 'ok' nel registro (slice 122), con l'archivio, lo SHA-256 e i byte detti; verifica-backup non
	// ne aggiunge.
	if r := righeRegistro(t, p); len(r) != 1 || r[0] != "ok cartella "+m[1]+" "+m[3]+" "+m[2]+" " {
		t.Errorf("registro: %q", r)
	}
}

// righeRegistro dà le righe del registro dei backup verificati: esito, destinazione, archivio, SHA-256,
// byte, errore.
func righeRegistro(t *testing.T, p string) []string {
	t.Helper()
	d, err := db.Apri(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Chiudi() }()
	rr, err := d.Lettura.Query(`SELECT esito || ' ' || destinazione || ' ' || archivio || ' ' || sha256 || ' ' || byte || ' ' ||
		coalesce(errore, '') FROM verifiche_backup ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rr.Close() }()
	var v []string
	for rr.Next() {
		var s string
		if err := rr.Scan(&s); err != nil {
			t.Fatal(err)
		}
		v = append(v, s)
	}
	if err := rr.Err(); err != nil {
		t.Fatal(err)
	}
	return v
}

// conDatabase apre il database p e chiama f.
func conDatabase(t *testing.T, p string, f func(d *db.DB)) {
	t.Helper()
	d, err := db.Apri(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Chiudi() }()
	f(d)
}

// Voce OPS-17, in parte (senza segno «inventario:»: vedi la ricevuta della 122); ADM-17.
// Il giro vero (slice 122, D-11, risposta F): dopo un import BloccatoDopoImport blocca; gestrip backup
// -cartella scrive, verifica e registra un archivio catturato dopo l'import, ma la cartella **non** sblocca.
// Con la stessa riga segnata come chiavetta (quello che farà la 123 dopo i suoi controlli) si sblocca; con
// l'archivio toccato (stessa lunghezza) torna il blocco con un errore; con l'archivio tolto anche.
func TestBackupComandoSbloccaDopoImport(t *testing.T) {
	p := databaseConChiave(t)
	importato := time.Now().Add(-time.Hour).UTC().Format(db.FormatoIstante)
	bloccato := func() (b bool, err error) {
		conDatabase(t, p, func(d *db.DB) {
			errP := db.PreparaBloccoImport(context.Background(), d.Lettura)
			b, err = db.BloccatoDopoImport(context.Background(), d.Lettura)
			err = errors.Join(errP, err)
		})
		return
	}
	conDatabase(t, p, func(d *db.DB) {
		if _, err := d.Scrittura.Exec(`INSERT INTO import_standalone (id, modalita, sha256_sorgente, byte_sorgente, importato_il)
			VALUES (1, 'sintetico', zeroblob(32), 4096, ?)`, importato); err != nil {
			t.Fatal(err)
		}
	})
	if b, err := bloccato(); !b || err != nil {
		t.Fatalf("dopo l'import senza backup: %v %v", b, err)
	}
	dest := t.TempDir()
	if err := faiBackup(context.Background(), opzioniBackup{DB: p, Cartella: dest}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if b, err := bloccato(); !b || err != nil {
		t.Fatalf("un backup nella cartella ha sbloccato: %v %v", b, err)
	}
	conDatabase(t, p, func(d *db.DB) {
		if _, err := d.Scrittura.Exec(`UPDATE verifiche_backup SET destinazione = 'chiavetta'`); err != nil {
			t.Fatal(err)
		}
	})
	if b, err := bloccato(); b || err != nil {
		t.Fatalf("dopo il backup verificato sulla chiavetta: %v %v", b, err)
	}
	a, _ := filepath.Glob(filepath.Join(dest, "gestrip-backup-*.tar.age"))
	if len(a) != 1 {
		t.Fatalf("archivi %v", a)
	}
	contenuto, err := os.ReadFile(a[0])
	if err != nil {
		t.Fatal(err)
	}
	contenuto[len(contenuto)/2] ^= 1
	if err := os.WriteFile(a[0], contenuto, 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := bloccato(); !b || !errors.Is(err, db.ErrArchivioCambiato) {
		t.Errorf("archivio toccato: %v %v", b, err)
	}
	if err := os.Remove(a[0]); err != nil {
		t.Fatal(err)
	}
	if b, err := bloccato(); !b || err != nil {
		t.Errorf("archivio tolto: %v %v", b, err)
	}
}

// Voce OPS-16, in parte (senza segno «inventario:», vedi TestBackupComando).
// Se il registro dei backup non si scrive (qui la tabella tolta) l'archivio verificato non conta: il comando
// dice «BACKUP NON RIUSCITO», lascia l'archivio e dà un errore.
func TestBackupComandoRegistroNonScritto(t *testing.T) {
	p := databaseConChiave(t)
	conDatabase(t, p, func(d *db.DB) {
		if _, err := d.Scrittura.Exec(`DROP TABLE verifiche_backup`); err != nil {
			t.Fatal(err)
		}
	})
	dest := t.TempDir()
	var out bytes.Buffer
	err := faiBackup(context.Background(), opzioniBackup{DB: p, Cartella: dest}, &out)
	if err == nil || !strings.Contains(err.Error(), "backup non registrato") {
		t.Fatalf("errore %v", err)
	}
	if strings.Contains(out.String(), "backup riuscito") || !strings.Contains(out.String(), "BACKUP NON RIUSCITO") {
		t.Errorf("uscita:\n%s", out.String())
	}
	if v, _ := filepath.Glob(filepath.Join(dest, "gestrip-backup-*.tar.age")); len(v) != 1 {
		t.Errorf("archivi nella destinazione %v", v)
	}
}

// Voce OPS-16, in parte (senza segno «inventario:», vedi TestBackupComando).
// Se la verifica non passa (o verifica un file diverso da quello scritto) il comando lo dice chiaro,
// non dice «riuscito», lascia l'archivio dov'è e dà un errore (uscita 1).
func TestBackupComandoVerificaFallita(t *testing.T) {
	p := databaseConChiave(t)
	vecchia := verificaArchivio
	t.Cleanup(func() { verificaArchivio = vecchia })
	motivi := map[string]string{"verifica fallita": backup.ErrIntegrita.Error(), "altro file": backup.ErrImpronta.Error()}
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
			v, _ := filepath.Glob(filepath.Join(dest, "gestrip-backup-*.tar.age"))
			if len(v) != 1 || !strings.Contains(out.String(), v[0]) {
				t.Fatalf("archivi nella destinazione %v", v)
			}
			// La verifica fallita è nel registro (slice 122), con il motivo, e non sblocca niente.
			var trovata bool
			for _, r := range righeRegistro(t, p) {
				trovata = trovata || (strings.HasPrefix(r, "fallita cartella "+v[0]+" ") && strings.HasSuffix(r, " "+motivi[nome]))
			}
			if !trovata {
				t.Errorf("verifica fallita non registrata: %q", righeRegistro(t, p))
			}
			conDatabase(t, p, func(d *db.DB) {
				if ok, err := db.BackupVerificatoDopo(context.Background(), d.Lettura, time.Now().Add(-time.Hour)); ok || err != nil {
					t.Errorf("verifica fallita contata: %v %v", ok, err)
				}
			})
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

// Voce OPS-16, in parte (senza segno «inventario:», vedi TestBackupComando).
// -chiavetta su una cartella che non è un punto di montaggio (qui una cartella temporanea, con il sistema
// vero) rifiuta prima di scrivere: niente archivio, niente .pending tolti, niente righe nel registro (né
// «chiavetta» né altro).
func TestBackupChiavettaNonMontata(t *testing.T) {
	p := databaseConChiave(t)
	dest := t.TempDir()
	pending := filepath.Join(dest, "gestrip-backup-20261124T013000Z.tar.age.pending")
	if err := os.WriteFile(pending, []byte("rimasto"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := faiBackup(context.Background(), opzioniBackup{DB: p, Chiavetta: dest}, &out)
	if !errors.Is(err, backup.ErrNonMontata) || out.Len() != 0 {
		t.Fatalf("errore %v, uscita %q", err, out.String())
	}
	if v, _ := os.ReadDir(dest); len(v) != 1 {
		t.Errorf("nella cartella: %v", v)
	}
	if r := righeRegistro(t, p); len(r) != 0 {
		t.Errorf("registro: %q", r)
	}
}

// Voce OPS-16, in parte (senza segno «inventario:», vedi TestBackupComando).
// -cartella tiene il lucchetto della cartella (un altro backup lì dentro rifiuta senza scrivere) e non pulisce
// né ruota niente: un .pending con il nostro nome e un archivio vecchio restano.
func TestBackupCartellaLucchettoSenzaPulizia(t *testing.T) {
	p := databaseConChiave(t)
	dest := t.TempDir()
	vecchi := []string{"gestrip-backup-20261124T013000Z.tar.age.pending", "gestrip-backup-20200101T013000Z.tar.age"}
	for _, n := range vecchi {
		if err := os.WriteFile(filepath.Join(dest, n), []byte("vecchio"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	l, err := backup.BloccaDestinazione(dest, filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	if err := faiBackup(context.Background(), opzioniBackup{DB: p, Cartella: dest}, &bytes.Buffer{}); !errors.Is(err, backup.ErrDestinazioneOccupata) {
		t.Errorf("con la cartella occupata: %v", err)
	}
	if v, _ := os.ReadDir(dest); len(v) != 2 {
		t.Errorf("scritto con la cartella occupata: %v", v)
	}
	if err := l.Chiudi(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := faiBackup(context.Background(), opzioniBackup{DB: p, Cartella: dest}, &out); err != nil {
		t.Fatal(err)
	}
	for _, n := range vecchi {
		if _, err := os.Lstat(filepath.Join(dest, n)); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
	if strings.Contains(out.String(), "tolto") || !strings.HasSuffix(out.String(), "backup riuscito\n") {
		t.Errorf("uscita:\n%s", out.String())
	}
}
