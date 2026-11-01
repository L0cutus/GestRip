package media

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// I casi di repair_photos_test.exs e media_store_*_test.exs dello standalone che riguardano
// l'archivio.

func archivio(t *testing.T) (*Archivio, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), Cartella)
	return Nuovo(dir), dir
}

func impronta(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func elenco(t *testing.T, dir string) []string {
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

// inventario: RIP-S-30
// Il file si chiama con lo SHA-256 dei suoi byte, è 0400 in una cartella 0700 (creata se manca,
// riportata a 0700 se c'era più aperta), e lo stesso contenuto è lo stesso oggetto. Nessun
// temporaneo resta.
func TestSalvaPerImpronta(t *testing.T) {
	a, dir := archivio(t)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b := []byte("new durable object")
	k, err := a.Salva(b)
	if err != nil {
		t.Fatal(err)
	}
	if k != impronta(b) || !regexp.MustCompile(`\A[0-9a-f]{64}\z`).MatchString(k) {
		t.Errorf("chiave %q", k)
	}
	fi, err := os.Lstat(filepath.Join(dir, k))
	if err != nil || fi.Mode().Perm() != 0o400 || !fi.Mode().IsRegular() {
		t.Errorf("file: %v %v", fi.Mode(), err)
	}
	if di, _ := os.Stat(dir); di.Mode().Perm() != 0o700 {
		t.Errorf("cartella %v", di.Mode().Perm())
	}
	if k2, err := a.Salva(b); err != nil || k2 != k {
		t.Errorf("di nuovo: %q %v", k2, err)
	}
	if n := elenco(t, dir); len(n) != 1 || n[0] != k {
		t.Errorf("cartella: %v", n)
	}
	if got, err := a.Leggi(k); err != nil || string(got) != string(b) {
		t.Errorf("Leggi: %q %v", got, err)
	}
}

// inventario: RIP-S-30
// La pubblicazione sincronizza la cartella due volte (dopo il link e dopo aver tolto il
// temporaneo), per un oggetto nuovo e per uno che c'era; se la sincronizzazione fallisce il
// salvataggio fallisce.
func TestSalvaSincronizzaLaCartella(t *testing.T) {
	a, dir := archivio(t)
	var viste []string
	a.sincronizza = func(p string) error { viste = append(viste, p); return nil }
	b := []byte("new durable object")
	if _, err := a.Salva(b); err != nil {
		t.Fatal(err)
	}
	if len(viste) != 2 || viste[0] != dir || viste[1] != dir {
		t.Errorf("nuovo: %v", viste)
	}
	viste = nil
	if _, err := a.Salva(b); err != nil {
		t.Fatal(err)
	}
	if len(viste) != 2 || viste[0] != dir || viste[1] != dir {
		t.Errorf("esistente: %v", viste)
	}
	a.sincronizza = func(string) error { return errors.New("io") }
	if _, err := a.Salva([]byte("sync failure orphan")); !errors.Is(err, ErrSalvataggio) {
		t.Errorf("sincronizzazione fallita: %v", err)
	}
	for _, n := range elenco(t, dir) {
		if strings.HasPrefix(n, ".upload-") {
			t.Errorf("temporaneo rimasto: %s", n)
		}
	}
}

// inventario: RIP-S-30
// Niente da salvare, una cartella che è un file o un link: ErrSalvataggio, e niente si scrive.
func TestSalvaRifiuti(t *testing.T) {
	a, _ := archivio(t)
	if _, err := a.Salva(nil); !errors.Is(err, ErrSalvataggio) {
		t.Errorf("vuoto: %v", err)
	}
	base := t.TempDir()
	file := filepath.Join(base, "not-a-directory")
	if err := os.WriteFile(file, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Nuovo(file).Salva([]byte("x")); !errors.Is(err, ErrSalvataggio) {
		t.Errorf("file al posto della cartella: %v", err)
	}
	vera := filepath.Join(base, "vera")
	if err := os.Mkdir(vera, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(vera, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Nuovo(link).Salva([]byte("x")); !errors.Is(err, ErrSalvataggio) {
		t.Errorf("link al posto della cartella: %v", err)
	}
	if n := elenco(t, vera); len(n) != 0 {
		t.Errorf("scritto attraverso il link: %v", n)
	}
}

// inventario: RIP-S-30
// Un oggetto con il nome giusto ma byte diversi non si «ripara»: il salvataggio fallisce.
func TestSalvaSuUnOggettoGuasto(t *testing.T) {
	a, dir := archivio(t)
	b := []byte("contenuto")
	k := impronta(b)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, k), []byte("altro"), 0o400); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Salva(b); !errors.Is(err, ErrSalvataggio) {
		t.Errorf("%v", err)
	}
}

// inventario: RIP-S-30, RIP-S-33
// read_object: chiave senza la forma giusta → ErrChiave; file mancante, vuoto, oltre 16 MiB,
// non regolare (link, cartella) o con un'altra impronta → ErrOggettoGuasto.
func TestLeggi(t *testing.T) {
	a, dir := archivio(t)
	k, err := a.Salva([]byte("ok"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"", "../" + k, strings.ToUpper(k), k[:63], k + "0", " " + k[1:]} {
		if _, err := a.Leggi(c); !errors.Is(err, ErrChiave) {
			t.Errorf("chiave %q: %v", c, err)
		}
	}
	scrivi := func(b []byte) string {
		k := impronta(b)
		if err := os.WriteFile(filepath.Join(dir, k), b, 0o400); err != nil {
			t.Fatal(err)
		}
		return k
	}
	grande := scrivi(make([]byte, MaxByte+1))
	limite := scrivi(make([]byte, MaxByte))
	guasto := impronta([]byte("originale"))
	if err := os.WriteFile(filepath.Join(dir, guasto), []byte("corrupt"), 0o400); err != nil {
		t.Fatal(err)
	}
	vuoto := impronta(nil)
	if err := os.WriteFile(filepath.Join(dir, vuoto), nil, 0o400); err != nil {
		t.Fatal(err)
	}
	altrove := filepath.Join(t.TempDir(), "fuori")
	if err := os.WriteFile(altrove, []byte("fuori"), 0o400); err != nil {
		t.Fatal(err)
	}
	link := impronta([]byte("fuori"))
	if err := os.Symlink(altrove, filepath.Join(dir, link)); err != nil {
		t.Fatal(err)
	}
	cartella := impronta([]byte("cartella"))
	if err := os.Mkdir(filepath.Join(dir, cartella), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{strings.Repeat("0", 64), grande, guasto, vuoto, link, cartella} {
		if _, err := a.Leggi(c); !errors.Is(err, ErrOggettoGuasto) {
			t.Errorf("%s: %v", c[:8], err)
		}
	}
	if b, err := a.Leggi(limite); err != nil || len(b) != MaxByte {
		t.Errorf("16 MiB esatti: %d %v", len(b), err)
	}
}

// inventario: RIP-S-30
// Il lucchetto esclude una seconda operazione finché la prima non finisce.
func TestLucchetto(t *testing.T) {
	a, _ := archivio(t)
	tenuto, libera := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = a.ConLucchetto(func() error { close(tenuto); <-libera; return nil }) // la prova guarda i canali
	}()
	<-tenuto
	entrato := make(chan struct{})
	go func() {
		_, _ = a.Salva([]byte("secondo")) // la prova guarda solo quando entra
		close(entrato)
	}()
	select {
	case <-entrato:
		t.Fatal("la seconda operazione è entrata con il lucchetto preso")
	case <-time.After(100 * time.Millisecond):
	}
	close(libera)
	wg.Wait()
	select {
	case <-entrato:
	case <-time.After(5 * time.Second):
		t.Fatal("la seconda operazione non è mai entrata")
	}
	if err := a.ConLucchetto(func() error { return errors.New("dentro") }); err == nil || err.Error() != "dentro" {
		t.Errorf("ConLucchetto: %v", err)
	}
}

// inventario: RIP-S-30, RIP-S-33
// Il tipo si legge dalla firma dei byte, mai indovinato; senza firma nota vale quello registrato
// nella riga; l'estensione va con il tipo.
func TestTipoImmagine(t *testing.T) {
	for _, c := range []struct {
		b         string
		tipo, est string
	}{
		{"\xFF\xD8\xFF\xE0rest", "image/jpeg", ".jpg"},
		{"\x89PNG\r\n\x1a\nrest", "image/png", ".png"},
		{"RIFF\x04\x00\x00\x00WEBP", "image/webp", ".webp"},
		{"GIF89a", "application/octet-stream", ""},
		{"", "application/octet-stream", ""},
		{"\xFF\xD8", "application/octet-stream", ""},
		{"RIFF\x04\x00\x00\x00WEB", "application/octet-stream", ""},
		{"RIFF\x04\x00\x00\x00WAVEfmt ", "application/octet-stream", ""},
	} {
		if g := TipoImmagine([]byte(c.b)); g != c.tipo {
			t.Errorf("%q: %q, atteso %q", c.b, g, c.tipo)
		}
		if g := Estensione([]byte(c.b)); g != c.est {
			t.Errorf("%q: estensione %q, attesa %q", c.b, g, c.est)
		}
	}
	if g := TipoImmagineORegistrato([]byte("GIF89a"), "image/png"); g != "image/png" {
		t.Errorf("registrato: %q", g)
	}
	if g := TipoImmagineORegistrato([]byte("\xFF\xD8\xFFx"), "image/png"); g != "image/jpeg" {
		t.Errorf("firma: %q", g)
	}
}

// inventario: RIP-S-30
// La cartella media sta accanto al database; una chiave valida è di 64 cifre esadecimali minuscole.
func TestCartellaEChiave(t *testing.T) {
	if g := CartellaDi("/var/lib/gestrip/gestrip.db"); g != "/var/lib/gestrip/media" {
		t.Errorf("%q", g)
	}
	if !ChiaveValida(strings.Repeat("a", 64)) || ChiaveValida(strings.Repeat("A", 64)) || ChiaveValida(strings.Repeat("a", 65)) || ChiaveValida("") {
		t.Error("ChiaveValida")
	}
}
