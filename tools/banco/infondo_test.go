package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// in_fondo (slice 047): uno scenario che cambia dati letti da altri gira dopo tutti gli altri;
// l'ordine dei nomi resta dentro i due gruppi.
func TestScenariInFondo(t *testing.T) {
	dir := t.TempDir()
	for nome, in := range map[string]bool{"a": false, "b": true, "c": false, "d": true, "e": false} {
		j := `{"nome":"` + nome + `","passi":[{"metodo":"GET","percorso":"/healthz","confronta":["codice"]}]`
		if in {
			j += `,"in_fondo":true`
		}
		if err := os.WriteFile(filepath.Join(dir, nome+".json"), []byte(j+"}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ss, err := leggiScenari(dir)
	if err != nil {
		t.Fatal(err)
	}
	var nomi []string
	for _, s := range ss {
		nomi = append(nomi, s.Nome)
	}
	if !slices.Equal(nomi, []string{"a", "c", "e", "b", "d"}) {
		t.Errorf("ordine %v", nomi)
	}
	// Gli scenari veri: la firma e poi il pannello del QR (slice 054, cambia le buste 29/26 e 33/26
	// solo nel Go) sono in fondo.
	veri, err := leggiScenari(filepath.Join("..", "..", "bench", "scenari"))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(veri); veri[n-1].Nome != "qr-pannello" || !veri[n-1].InFondo || veri[n-2].Nome != "firma" || !veri[n-2].InFondo {
		t.Errorf("ultimi scenari: %s, %s", veri[n-2].Nome, veri[n-1].Nome)
	}
}
