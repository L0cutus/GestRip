package apparecchio

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func scrivi(t *testing.T, testo string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "apparecchio.conf")
	if err := os.WriteFile(p, []byte(testo), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// L'indirizzo della faccia interna sta nel file dell'apparecchio, come PHX_HOST e PHX_URL_PORT
// dello standalone stanno nell'ambiente della macchina: i link si costruiscono lì, mai sull'Host
// della richiesta, e non viaggia con il backup.
func TestIndirizzoInterno(t *testing.T) {
	if _, err := IndirizzoInterno(filepath.Join(t.TempDir(), "manca.conf")); !errors.Is(err, ErrNonConfigurato) {
		t.Errorf("file mancante: %v", err)
	}
	if _, err := IndirizzoInterno(scrivi(t, "# solo un commento\n\n")); !errors.Is(err, ErrNonConfigurato) {
		t.Errorf("senza la chiave: %v", err)
	}
	p := scrivi(t, "# configurazione scritta dall'installer\n\n  indirizzo_interno =  https://Negozio.esempio.ts.net:8443/  \n")
	if s, err := IndirizzoInterno(p); err != nil || s != "https://Negozio.esempio.ts.net:8443" {
		t.Errorf("lettura: %q %v", s, err)
	}
	if s, err := IndirizzoInterno(scrivi(t, "indirizzo_interno=https://negozio.esempio.ts.net\r\n")); err != nil || s != "https://negozio.esempio.ts.net" {
		t.Errorf("senza spazi, con CRLF: %q %v", s, err)
	}
	for _, testo := range []string{
		"indirizzo_interno = https://h/percorso\n",
		"indirizzo_interno = http://negozio.esempio.ts.net\n",
		"indirizzo_interno = \n",
		"indirizzo_interno https://negozio.esempio.ts.net\n",
		"indirizzo_interno = https://a.esempio.ts.net\nindirizzo_interno = https://b.esempio.ts.net\n",
		"indirizzo_pubblico = https://a.esempio.ts.net\nindirizzo_interno = https://b.esempio.ts.net\n",
		"indirizzo_interno = https://a.esempio.ts.net\n" + strings.Repeat("#\n", massimoFile/2),
	} {
		if s, err := IndirizzoInterno(scrivi(t, testo)); err == nil || errors.Is(err, ErrNonConfigurato) {
			t.Errorf("%q accettato: %q %v", testo[:min(len(testo), 70)], s, err)
		}
	}
	// Fino al limite si legge tutto.
	al := "indirizzo_interno = https://a.esempio.ts.net\n"
	al += strings.Repeat("#", massimoFile-len(al))
	if s, err := IndirizzoInterno(scrivi(t, al)); err != nil || s != "https://a.esempio.ts.net" {
		t.Errorf("file al limite: %q %v", s, err)
	}
	// Un file che non si legge (qui una cartella) è un errore, non «non configurato».
	if _, err := IndirizzoInterno(t.TempDir()); err == nil || errors.Is(err, ErrNonConfigurato) {
		t.Errorf("cartella al posto del file: %v", err)
	}
}

// Le regole di forma della 011: https, nome di host valido o indirizzo IP, al più una porta da 1 a
// 65535, niente altro. Una barra finale si toglie, la porta 443 anche (lo standalone la omette).
func TestControllaIndirizzo(t *testing.T) {
	for _, u := range []string{"", "http://negozio.esempio.ts.net:8443", "https://", "https://h/percorso", "https://h?x=1", "https://u@h", "negozio:8443", "https://h#x",
		"https://h:99999", "https://h:65536", "https://h:", "https://h:0", "https://h;x", "https://-h", "https://h_x.it", "https://h..it", "https://h:8443:1", "https://h?", "https://h#"} {
		if s, err := ControllaIndirizzo(u); err == nil {
			t.Errorf("indirizzo %q accettato (%q)", u, s)
		}
	}
	for u, atteso := range map[string]string{
		"https://negozio.esempio.ts.net:8443/": "https://negozio.esempio.ts.net:8443",
		"https://negozio.esempio.ts.net":       "https://negozio.esempio.ts.net",
		"https://negozio.esempio.ts.net:443":   "https://negozio.esempio.ts.net",
		"https://h:0443":                       "https://h",
		"https://h:08443":                      "https://h:8443",
		"https://100.64.0.1:8443":              "https://100.64.0.1:8443",
		"https://[fd7a:115c:a1e0::1]:8443":     "https://[fd7a:115c:a1e0::1]:8443",
		"https://[fd7a:115c:a1e0::1]:443":      "https://[fd7a:115c:a1e0::1]",
		"https://h:1":                          "https://h:1",
		"https://h:65535":                      "https://h:65535",
	} {
		if s, err := ControllaIndirizzo(u); err != nil || s != atteso {
			t.Errorf("%q: %q %v", u, s, err)
		}
	}
}

// L'istruzione dice quale riga e in quale file; senza file non ne inventa uno.
func TestComeConfigurare(t *testing.T) {
	if s := ComeConfigurare("/etc/x.conf"); s != "scrivi «indirizzo_interno = https://host[:porta]» in /etc/x.conf" {
		t.Errorf("con file: %q", s)
	}
	if s := ComeConfigurare(""); strings.Contains(s, " in .") || !strings.Contains(s, "-config") {
		t.Errorf("senza file: %q", s)
	}
}
