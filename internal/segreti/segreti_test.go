package segreti

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// file scrive un file di segreti sintetico con i permessi dati.
func file(t *testing.T, contenuto string, permessi os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "segreto")
	if err := os.WriteFile(p, []byte(contenuto), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, permessi); err != nil {
		t.Fatal(err)
	}
	return p
}

// inventario: OPS-13
// Un segreto si legge da un file regolare e privato (0600 o 0400), senza gli a capo finali come
// read_secret dello standalone.
func TestLeggiFile(t *testing.T) {
	for _, c := range []struct {
		contenuto string
		permessi  os.FileMode
		atteso    string
	}{
		{"segreto-sintetico-di-prova\n", 0o600, "segreto-sintetico-di-prova"},
		{"segreto-sintetico-di-prova\r\n", 0o600, "segreto-sintetico-di-prova"},
		{"segreto-sintetico-di-prova\n\n", 0o400, "segreto-sintetico-di-prova"},
		{"  con spazi  ", 0o600, "  con spazi  "},
	} {
		v, err := LeggiFile(file(t, c.contenuto, c.permessi), 1, MassimoFile)
		if err != nil || v != c.atteso {
			t.Errorf("%q %o: %q, %v; atteso %q", c.contenuto, c.permessi, v, err, c.atteso)
		}
	}
}

// inventario: OPS-13
// Un file che dà permessi al gruppo o agli altri è rifiutato, anche con un solo bit.
func TestLeggiFileRifiutaIPermessiLarghi(t *testing.T) {
	for _, permessi := range []os.FileMode{0o640, 0o604, 0o601, 0o610, 0o644, 0o666} {
		_, err := LeggiFile(file(t, "segreto-sintetico-di-prova", permessi), 1, MassimoFile)
		if err == nil || !strings.Contains(err.Error(), "gruppo o altri") {
			t.Errorf("%o: %v", permessi, err)
		}
	}
}

// inventario: OPS-13
// Dimensioni: al più massimo byte nel file, almeno minimo nel valore (la chiave di sessione dello
// standalone: almeno 64). Un file di soli a capo è vuoto. Il messaggio non riporta il contenuto.
func TestLeggiFileDimensioni(t *testing.T) {
	sessantaquattro := strings.Repeat("a", 64)
	if v, err := LeggiFile(file(t, sessantaquattro+"\n", 0o600), 64, MassimoFile); err != nil || v != sessantaquattro {
		t.Errorf("64 byte: %q, %v", v, err)
	}
	corto := strings.Repeat("b", 63)
	_, err := LeggiFile(file(t, corto+"\n", 0o600), 64, MassimoFile)
	if err == nil || !strings.Contains(err.Error(), "almeno 64") || strings.Contains(err.Error(), corto) {
		t.Errorf("63 byte più a capo: %v", err)
	}
	if _, err := LeggiFile(file(t, strings.Repeat("c", MassimoFile), 0o600), 1, MassimoFile); err != nil {
		t.Errorf("4096 byte: %v", err)
	}
	if _, err := LeggiFile(file(t, strings.Repeat("c", MassimoFile+1), 0o600), 1, MassimoFile); err == nil || !strings.Contains(err.Error(), "al più 4096") {
		t.Errorf("4097 byte: %v", err)
	}
	for _, vuoto := range []string{"", "\n", "\r\n"} {
		if _, err := LeggiFile(file(t, vuoto, 0o600), 1, MassimoFile); err == nil {
			t.Errorf("%q accettato", vuoto)
		}
	}
	if _, err := LeggiFile(file(t, "x", 0o600), 0, MassimoFile); err == nil {
		t.Error("minimo 0 accettato: un segreto vuoto non è un segreto")
	}
	if _, err := LeggiFile(file(t, "x", 0o600), 1, MassimoFile+1); err == nil {
		t.Error("massimo oltre MassimoFile accettato")
	}
}

// inventario: OPS-13
// Il valore deve essere UTF-8 valido (String.valid? dello standalone).
func TestLeggiFileSoloUTF8(t *testing.T) {
	_, err := LeggiFile(file(t, "segreto\xff\xfe", 0o600), 1, MassimoFile)
	if err == nil || !strings.Contains(err.Error(), "UTF-8") || strings.Contains(err.Error(), "segreto\xff") {
		t.Errorf("UTF-8 non valido: %v", err)
	}
}

// inventario: OPS-13
// SECRET_KEY_BASE e LEGACY_WARRANTY_SECRET come valore fermano l'avvio, anche vuote (in Elixir
// System.get_env dà "" e "" è vero): non si ignorano. Le altre variabili non contano.
func TestControllaAmbiente(t *testing.T) {
	ambiente := func(m map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
	}
	if err := ControllaAmbiente(ambiente(map[string]string{
		"SECRET_KEY_BASE_FILE": "/run/secrets/x", "LEGACY_WARRANTY_SECRET_FILE": "/run/secrets/y", "PATH": "/usr/bin",
	})); err != nil {
		t.Errorf("senza le variabili rifiutate: %v", err)
	}
	for _, nome := range VariabiliRifiutate {
		for _, valore := range []string{strings.Repeat("x", 64), ""} {
			err := ControllaAmbiente(ambiente(map[string]string{nome: valore}))
			if err == nil || !strings.Contains(err.Error(), nome+" non è accettata") {
				t.Errorf("%s=%q: %v", nome, valore, err)
			}
			if err != nil && valore != "" && strings.Contains(err.Error(), valore) {
				t.Errorf("%s: il messaggio riporta il valore", nome)
			}
		}
	}
	err := ControllaAmbiente(ambiente(map[string]string{"SECRET_KEY_BASE": "a", "LEGACY_WARRANTY_SECRET": "b"}))
	if err == nil || !strings.Contains(err.Error(), "SECRET_KEY_BASE") || !strings.Contains(err.Error(), "LEGACY_WARRANTY_SECRET") {
		t.Errorf("tutte e due: %v", err)
	}
}

// inventario: OPS-13
// Gli a capo finali si tolgono come read_secret: prima tutti i «\n», poi tutti i «\r»; quello che
// resta prima non si tocca («a\n\r\n» dà «a\n»).
func TestLeggiFileOrdineDegliACapo(t *testing.T) {
	for contenuto, atteso := range map[string]string{"a\n\r\n": "a\n", "a\r\r\n\n": "a", "a\r\n\r": "a\r\n"} {
		if v, err := LeggiFile(file(t, contenuto, 0o600), 1, MassimoFile); err != nil || v != atteso {
			t.Errorf("%q: %q, %v; atteso %q", contenuto, v, err, atteso)
		}
	}
}
