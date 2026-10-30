//go:build unix

package segreti

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// inventario: OPS-13
// Il percorso deve essere il file stesso: un link a un file privato è rifiutato (lstat dello
// standalone), come una cartella, una FIFO (senza restare appesi) o un file che manca.
func TestLeggiFileSoloFileRegolari(t *testing.T) {
	vero := file(t, "segreto-sintetico-di-prova", 0o600)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(vero, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LeggiFile(link, 1, MassimoFile); err == nil || !strings.Contains(err.Error(), "link") {
		t.Errorf("link: %v", err)
	}
	cartella := filepath.Join(t.TempDir(), "cartella")
	if err := os.Mkdir(cartella, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := LeggiFile(cartella, 1, MassimoFile); err == nil || !strings.Contains(err.Error(), "file regolare") {
		t.Errorf("cartella: %v", err)
	}
	fifo := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	fatto := make(chan error, 1)
	go func() { _, err := LeggiFile(fifo, 1, MassimoFile); fatto <- err }()
	select {
	case err := <-fatto:
		if err == nil || !strings.Contains(err.Error(), "file regolare") {
			t.Errorf("fifo: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("la lettura di una FIFO resta appesa")
	}
	if _, err := LeggiFile(filepath.Join(t.TempDir(), "manca"), 1, MassimoFile); err == nil {
		t.Error("file mancante accettato")
	}
}

// inventario: OPS-13
// Un link si riconosce da lstat, non dal nome: «alias» che punta a un file privato è rifiutato
// con «è un link».
func TestLeggiFileLinkConUnNomeQualunque(t *testing.T) {
	vero := file(t, "segreto-sintetico-di-prova", 0o600)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(vero, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := LeggiFile(alias, 1, MassimoFile); err == nil || !strings.Contains(err.Error(), "è un link") {
		t.Errorf("alias: %v", err)
	}
}
