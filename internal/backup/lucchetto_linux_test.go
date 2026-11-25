package backup

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Il lucchetto dei backup e la pulizia delle cartelle temporanee rimaste (slice 124, note 6b e 7(e)). Cartelle
// temporanee del test; il flock è quello vero, anche fra due processi (il binario dei test rilanciato).

// Voce OPS-28, in parte: niente segno «inventario:» (la voce chiude con 125-127).
// Mai due backup insieme nello stesso processo: la seconda presa rifiuta subito con ErrBackupInCorso, dopo il
// rilascio si riprende; il file è 0600 e non si apre attraverso un collegamento.
func TestLucchettoMaiDueInsieme(t *testing.T) {
	dati := t.TempDir()
	l, err := PrendiLucchetto(dati)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrendiLucchetto(dati); !errors.Is(err, ErrBackupInCorso) {
		t.Fatalf("seconda presa: %v", err)
	}
	if _, err := PulisciAllAvvio(dati); !errors.Is(err, ErrBackupInCorso) {
		t.Errorf("pulizia all'avvio con il lucchetto preso: %v", err)
	}
	fi, err := os.Lstat(filepath.Join(dati, LucchettoBackup))
	if err != nil || fi.Mode().Perm() != 0o600 || !fi.Mode().IsRegular() {
		t.Errorf("file del lucchetto: %v %v", fi, err)
	}
	if err := l.Rilascia(); err != nil || l.Preso() {
		t.Fatalf("rilascio: %v", err)
	}
	if err := l.Rilascia(); err != nil {
		t.Errorf("secondo rilascio: %v", err)
	}
	l2, err := PrendiLucchetto(dati)
	if err != nil {
		t.Fatalf("dopo il rilascio: %v", err)
	}
	_ = l2.Rilascia()
	// Un collegamento al posto del file: rifiuto, il file di destinazione non nasce.
	altra := t.TempDir()
	if err := os.Symlink(filepath.Join(altra, "bersaglio"), filepath.Join(altra, LucchettoBackup)); err != nil {
		t.Fatal(err)
	}
	if _, err := PrendiLucchetto(altra); err == nil || errors.Is(err, ErrBackupInCorso) {
		t.Errorf("collegamento: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(altra, "bersaglio")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("il collegamento è stato seguito: %v", err)
	}
}

// figlioLucchetto rilancia il binario dei test in TestAiutoLucchettoFiglio: il figlio prende il lucchetto di
// dati, scrive «preso» (o l'errore) e lo tiene finché lo standard input non si chiude.
func figlioLucchetto(t *testing.T, dati string) (rilascia func(), riga string) {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^TestAiutoLucchettoFiglio$")
	c.Env = append(os.Environ(), "GESTRIP_TEST_LUCCHETTO_DATI="+dati)
	in, err := c.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(out)
	riga, err = r.ReadString('\n')
	if err != nil {
		_ = c.Process.Kill()
		t.Fatalf("figlio: %v", err)
	}
	finito := false
	rilascia = func() {
		if finito {
			return
		}
		finito = true
		_ = in.Close()
		_, _ = io.Copy(io.Discard, r)
		_ = c.Wait()
	}
	t.Cleanup(rilascia)
	return rilascia, strings.TrimSpace(riga)
}

// TestAiutoLucchettoFiglio è il figlio di figlioLucchetto; senza la variabile non fa niente.
func TestAiutoLucchettoFiglio(t *testing.T) {
	dati := os.Getenv("GESTRIP_TEST_LUCCHETTO_DATI")
	if dati == "" {
		return
	}
	l, err := PrendiLucchetto(dati)
	if err != nil {
		_, _ = os.Stdout.WriteString("errore: " + err.Error() + "\n")
		return
	}
	_, _ = os.Stdout.WriteString("preso\n")
	_, _ = io.Copy(io.Discard, os.Stdin)
	_ = l.Rilascia()
}

// Voce OPS-28, in parte (vedi TestLucchettoMaiDueInsieme).
// Mai due backup insieme fra due processi (il comando gestrip backup e serve): con il lucchetto preso da un
// altro processo la presa rifiuta subito; quando l'altro finisce (anche morendo: il kernel toglie il flock) si
// riprende. E al contrario: con il lucchetto preso qui il figlio rifiuta.
func TestLucchettoFraProcessi(t *testing.T) {
	dati := t.TempDir()
	rilascia, riga := figlioLucchetto(t, dati)
	if riga != "preso" {
		t.Fatalf("figlio: %q", riga)
	}
	if _, err := PrendiLucchetto(dati); !errors.Is(err, ErrBackupInCorso) {
		t.Fatalf("con il lucchetto del figlio: %v", err)
	}
	rilascia()
	l, err := PrendiLucchetto(dati)
	if err != nil {
		t.Fatalf("dopo il figlio: %v", err)
	}
	_, riga = figlioLucchetto(t, dati)
	if riga != "errore: "+ErrBackupInCorso.Error() {
		t.Errorf("figlio con il lucchetto preso qui: %q", riga)
	}
	_ = l.Rilascia()
}

// restiDiProva prepara nella cartella dei dati due cartelle temporanee rimaste (una con dentro un collegamento
// a un file fuori) e intorno nomi che la pulizia non deve toccare. Dà il file fuori.
func restiDiProva(t *testing.T, dati string) string {
	t.Helper()
	fuori := t.TempDir()
	bersaglio := filepath.Join(fuori, "da-non-toccare")
	if err := os.WriteFile(bersaglio, []byte("dato inventato fuori"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{".backup-123", ".verifica-backup-4294967295", ".backup-123/sotto",
		".backup-abc", ".backup-", ".backup-12345678901", ".verifica-backup-1x", "x.backup-1", "media", ".gestrip-import-5"} {
		if err := os.MkdirAll(filepath.Join(dati, c), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{".backup-123/gestrip.db", ".backup-123/sotto/gestrip.db-wal", ".verifica-backup-4294967295/gestrip.db", "gestrip.db"} {
		if err := os.WriteFile(filepath.Join(dati, f), []byte("copia inventata"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Un file normale con il nostro nome e un collegamento con il nostro nome a una cartella fuori: restano.
	if err := os.WriteFile(filepath.Join(dati, ".backup-7"), []byte("file, non cartella"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fuori, filepath.Join(dati, ".verifica-backup-8")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(bersaglio, filepath.Join(dati, ".backup-123", "collegamento")); err != nil {
		t.Fatal(err)
	}
	return bersaglio
}

func nomiIn(t *testing.T, dir string) []string {
	t.Helper()
	v, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var n []string
	for _, e := range v {
		n = append(n, e.Name())
	}
	return n
}

// Voce OPS-28, in parte (vedi TestLucchettoMaiDueInsieme).
// La pulizia (nota 7(e)) toglie solo le cartelle con il nome esatto delle temporanee di Scrivi e della verifica,
// solo cartelle vere, senza seguire i collegamenti (né quello con il nostro nome né quelli dentro), e solo con il
// lucchetto preso.
func TestPulisciResti(t *testing.T) {
	dati := t.TempDir()
	bersaglio := restiDiProva(t, dati)
	// Senza il lucchetto: niente.
	l, err := PrendiLucchetto(dati)
	if err != nil {
		t.Fatal(err)
	}
	prima := nomiIn(t, dati)
	if err := l.Rilascia(); err != nil {
		t.Fatal(err)
	}
	if _, err := l.PulisciResti(); err == nil {
		t.Error("pulizia senza il lucchetto")
	}
	if v := nomiIn(t, dati); !slices.Equal(v, prima) {
		t.Errorf("toccato senza il lucchetto: %v", v)
	}
	tolti, err := PulisciAllAvvio(dati)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tolti, []string{".backup-123", ".verifica-backup-4294967295"}) {
		t.Errorf("tolti %v", tolti)
	}
	resto := slices.DeleteFunc(slices.Clone(prima), func(n string) bool { return n == ".backup-123" || n == ".verifica-backup-4294967295" })
	if v := nomiIn(t, dati); !slices.Equal(v, resto) {
		t.Errorf("rimasti %v, attesi %v", v, resto)
	}
	if b, err := os.ReadFile(bersaglio); err != nil || string(b) != "dato inventato fuori" {
		t.Errorf("file fuori: %q %v", b, err)
	}
	if fi, err := os.Lstat(filepath.Join(dati, ".verifica-backup-8")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("collegamento col nostro nome: %v %v", fi, err)
	}
	if v := nomiIn(t, filepath.Dir(bersaglio)); len(v) != 1 {
		t.Errorf("cartella fuori: %v", v)
	}
	// Un secondo giro non trova niente.
	if tolti, err := PulisciAllAvvio(dati); err != nil || len(tolti) != 0 {
		t.Errorf("secondo giro: %v %v", tolti, err)
	}
}

// Voce OPS-28, in parte (vedi TestLucchettoMaiDueInsieme).
// Un backup in corso in un altro processo tiene il lucchetto: la pulizia all'avvio di serve rifiuta e la sua
// cartella temporanea resta. Una cartella che non si toglie ferma la pulizia con l'errore.
func TestPulisciRestiConUnBackupInCorso(t *testing.T) {
	dati := t.TempDir()
	if err := os.Mkdir(filepath.Join(dati, ".backup-42"), 0o700); err != nil {
		t.Fatal(err)
	}
	rilascia, riga := figlioLucchetto(t, dati)
	if riga != "preso" {
		t.Fatalf("figlio: %q", riga)
	}
	if tolti, err := PulisciAllAvvio(dati); !errors.Is(err, ErrBackupInCorso) || len(tolti) != 0 {
		t.Errorf("con il backup in corso: %v %v", tolti, err)
	}
	if _, err := os.Lstat(filepath.Join(dati, ".backup-42")); err != nil {
		t.Errorf("cartella in uso tolta: %v", err)
	}
	rilascia()
	vecchio := togliResto
	togliResto = func(*os.Root, string) error { return errors.New("cancellazione rifiutata inventata") }
	t.Cleanup(func() { togliResto = vecchio })
	if _, err := PulisciAllAvvio(dati); err == nil || !strings.Contains(err.Error(), ".backup-42") {
		t.Errorf("errore di cancellazione: %v", err)
	}
}
