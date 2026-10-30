package apparecchio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// inventario: OPS-12
// Il montaggio di un file aperto si legge da /proc/self/fdinfo (mnt_id) ed è la riga con quel
// numero in /proc/self/mountinfo, il cui punto contiene il file: sul sistema vero, non sintetico.
func TestIdMontaggioDaFdinfo(t *testing.T) {
	d := dati(t)
	p := filepath.Join(d, "gestrip.db")
	tocca(t, p)
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		t.Skip("niente /proc/self/mountinfo")
	}
	for _, percorso := range []string{d, p} {
		f, err := os.Open(percorso)
		if err != nil {
			t.Fatal(err)
		}
		id, err := IdMontaggioDaFdinfo(f)
		_ = f.Close() // in sola lettura
		if err != nil || id == "" {
			t.Fatalf("%s: %q %v", percorso, id, err)
		}
		trovata := false
		for _, r := range strings.Split(string(b), "\n") {
			c := strings.Split(r, " ")
			if len(c) > 4 && c[0] == id {
				trovata = true
				if !dentro(percorso, senzaEscape(c[4])) {
					t.Errorf("%s: montaggio %s in %s, che non lo contiene", percorso, id, c[4])
				}
			}
		}
		if !trovata {
			t.Errorf("%s: montaggio %s assente in mountinfo", percorso, id)
		}
	}
	chiuso, err := os.Open(d)
	if err != nil {
		t.Fatal(err)
	}
	_ = chiuso.Close()
	if id, err := IdMontaggioDaFdinfo(chiuso); err == nil {
		t.Errorf("file chiuso: %q", id)
	}
}

// dentro dice se il percorso p sta sotto il punto di montaggio (cartella per cartella).
func dentro(p, punto string) bool {
	return punto == "/" || p == punto || strings.HasPrefix(p, punto+"/")
}
