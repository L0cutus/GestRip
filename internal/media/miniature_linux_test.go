//go:build linux

package media

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
)

// inventario: RIP-S-33
// La miniatura non riscrive mai la foto: stessi byte, stessa impronta, stesso file, un solo oggetto
// nell'archivio (la cache sta fuori).
func TestMiniaturaNonToccaLaFoto(t *testing.T) {
	p := nuoveProva(t)
	k, orig := p.salvata(t, 1, 1000, 750)
	percorso := filepath.Join(p.media, k)
	prima, err := os.Lstat(percorso)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.m.Miniatura(t.Context(), k); err != nil {
		t.Fatal(err)
	}
	dopo, err := os.Lstat(percorso)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(percorso)
	if err != nil {
		t.Fatal(err)
	}
	sp, sd := prima.Sys().(*syscall.Stat_t), dopo.Sys().(*syscall.Stat_t)
	if !bytes.Equal(b, orig) || impronta(b) != k || !os.SameFile(prima, dopo) || prima.Mode() != dopo.Mode() ||
		prima.ModTime() != dopo.ModTime() || sp.Ctim != sd.Ctim || sp.Nlink != sd.Nlink || prima.Size() != dopo.Size() {
		t.Errorf("la foto è cambiata")
	}
	if n := nomiIn(t, p.media); !slices.Equal(n, []string{k}) {
		t.Errorf("archivio: %v", n)
	}
}
