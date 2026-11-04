package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/printing"
)

// I comandi finti del banco rispondono come CUPS al codice vero di printing (nessuna coda vera: i
// comandi sono quelli scritti dal banco, e il contatore di lp va nella cartella del test).
func TestStampaFintaDelBanco(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "stampa-finta")
	if err := scriviStampaFinta(dir); err != nil {
		t.Fatal(err)
	}
	// Il contatore dei lavori: nel container è /tmp (tmpfs), qui la cartella del test.
	conta := t.TempDir()
	lp := strings.ReplaceAll(comandiStampaFinta["lp"], "/tmp/", conta+"/")
	if err := os.Chmod(filepath.Join(dir, "lp"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lp"), []byte(lp), 0o500); err != nil {
		t.Fatal(err)
	}
	c := printing.NuovoCUPS(dir)
	if !strings.HasPrefix(c.Lp, dir) {
		t.Fatal(c.Lp)
	}
	ctx := context.Background()
	qq, err := c.List(ctx)
	if err != nil || len(qq) != 3 || qq[0].Nome != "Etichette_QR" || qq[2].Nome != "Rifiutata" {
		t.Fatalf("%+v %v", qq, err)
	}
	oo, err := c.Opzioni(ctx, "Etichette_QR")
	if err != nil || len(oo) != 2 || oo[0].Chiave != "MediaTracking" || oo[1].Predefinita != "8" {
		t.Errorf("%+v %v", oo, err)
	}
	opt := printing.Options{LarghezzaMM: 57, AltezzaMM: 32, Extra: []printing.Opzione{{Chiave: "Darkness", Valore: "8"}}, Titolo: "etichette.pdf"}
	for i, atteso := range []string{"Etichette_QR-1", "Etichette_QR-2"} {
		if id, err := c.Print(ctx, "Etichette_QR", []byte("%PDF finto"), opt); err != nil || string(id) != atteso {
			t.Errorf("%d: %q %v", i, id, err)
		}
	}
	if id, err := c.Print(ctx, "Buste-76x127", []byte("%PDF"), printing.Options{LarghezzaMM: 76, AltezzaMM: 127}); err != nil || id != "Buste-76x127-3" {
		t.Errorf("busta: %q %v", id, err)
	}
	var r printing.Rifiuto
	if _, err := c.Print(ctx, "Rifiutata", []byte("%PDF"), opt); !errors.As(err, &r) || r.Motivo != "lp: The printer or class does not exist." {
		t.Errorf("rifiutata: %v", err)
	}
}
