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
		"indirizzo_altro = https://a.esempio.ts.net\nindirizzo_interno = https://b.esempio.ts.net\n",
		"indirizzo_pubblico = http://a.esempio.ts.net\nindirizzo_interno = https://b.esempio.ts.net\n",
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

// L'indirizzo della faccia pubblica (decisione del 31/10/2026, slice 031) sta nello stesso file,
// con le stesse regole: la riga indirizzo_pubblico, letta insieme all'interno. Manca: vuoto, non è
// un errore (serve parte lo stesso). Ripetuta o sbagliata: errore che nomina la faccia pubblica.
func TestIndirizzoPubblico(t *testing.T) {
	c, err := Leggi(scrivi(t, "indirizzo_interno = https://negozio.esempio.ts.net:8443\nindirizzo_pubblico = https://Negozio.esempio.ts.net:443/\n"))
	if err != nil || c.IndirizzoPubblico != "https://Negozio.esempio.ts.net" || c.IndirizzoInterno != "https://negozio.esempio.ts.net:8443" {
		t.Errorf("lettura: %+v %v", c, err)
	}
	if c, err := Leggi(scrivi(t, "indirizzo_interno = https://negozio.esempio.ts.net:8443\n")); err != nil || c.IndirizzoPubblico != "" {
		t.Errorf("senza la riga: %+v %v", c, err)
	}
	for _, testo := range []string{
		"indirizzo_pubblico = https://h/r\n",
		"indirizzo_pubblico = https://a.esempio.ts.net\nindirizzo_pubblico = https://b.esempio.ts.net\n",
		"indirizzo_pubblico = \n",
	} {
		c, err := Leggi(scrivi(t, testo))
		if err == nil {
			t.Errorf("%q accettato: %+v", testo, c)
		}
	}
	_, err = Leggi(scrivi(t, "indirizzo_pubblico = http://h\n"))
	if err == nil || !strings.Contains(err.Error(), "faccia pubblica") {
		t.Errorf("errore che non nomina la faccia pubblica: %v", err)
	}
}

// La chiavetta dei backup notturni (slice 124): un percorso assoluto e pulito, anche con spazi; senza la riga
// il backup notturno non è configurato. Relativo, con la barra finale o «..», la radice, vuoto o ripetuto
// fermano la lettura.
func TestChiavettaBackup(t *testing.T) {
	c, err := Leggi(scrivi(t, "backup_chiavetta =  /media/gestrip backup  \n"))
	if err != nil || c.ChiavettaBackup != "/media/gestrip backup" {
		t.Errorf("lettura: %+v %v", c, err)
	}
	if c, err := Leggi(scrivi(t, "indirizzo_interno = https://negozio.esempio.ts.net\n")); err != nil || c.ChiavettaBackup != "" {
		t.Errorf("senza la riga: %+v %v", c, err)
	}
	for _, v := range []string{"media/chiavetta", "/media/chiavetta/", "/media/../chiavetta", "/", "", "/media//chiavetta"} {
		if c, err := Leggi(scrivi(t, "backup_chiavetta = "+v+"\n")); err == nil || !strings.Contains(err.Error(), "backup_chiavetta") {
			t.Errorf("%q: %+v %v", v, c, err)
		}
	}
	if _, err := Leggi(scrivi(t, "backup_chiavetta = /a\nbackup_chiavetta = /b\n")); err == nil {
		t.Error("riga ripetuta accettata")
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

// I sidecar dell'IA (slice 111): di default nessuno; con le righe, programma, argomenti, porta e salute.
func TestSidecar(t *testing.T) {
	c, err := Leggi(scrivi(t, "indirizzo_interno = https://negozio.esempio.ts.net\n"))
	if err != nil || len(c.Sidecar) != 0 {
		t.Fatalf("senza righe: %+v %v", c.Sidecar, err)
	}
	c, err = Leggi(scrivi(t, "sidecar_trascrizione = /opt/stt/srv  --porta {porta}\n"+
		"sidecar_estrazione_salute = /v1/models\n"+
		"sidecar_trascrizione_porta = 47302\n"+
		"sidecar_estrazione = /opt/gestrip/llama-server -m /opt/gestrip/m.gguf --port {porta} -ngl 0\n"+
		"sidecar_estrazione_porta = 47301\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Sidecar) != 2 {
		t.Fatalf("sidecar: %+v", c.Sidecar)
	}
	e, s := c.Sidecar[0], c.Sidecar[1]
	if e.Nome != "estrazione" || e.Programma != "/opt/gestrip/llama-server" || e.Porta != 47301 || e.Salute != "/v1/models" ||
		strings.Join(e.Argomenti, "|") != "-m|/opt/gestrip/m.gguf|--port|{porta}|-ngl|0" || len(e.Ambiente) != 0 {
		t.Errorf("estrazione: %+v", e)
	}
	if s.Nome != "trascrizione" || s.Programma != "/opt/stt/srv" || s.Porta != 47302 || s.Salute != "" ||
		strings.Join(s.Argomenti, "|") != "--porta|{porta}" {
		t.Errorf("trascrizione: %+v", s)
	}
	for _, testo := range []string{
		"sidecar_estrazione = /opt/srv\n",                                   // manca la porta
		"sidecar_estrazione_porta = 47301\n",                                // manca il programma
		"sidecar_estrazione_salute = /health\n",                             // solo la salute
		"sidecar_estrazione = srv\nsidecar_estrazione_porta = 47301\n",      // programma relativo
		"sidecar_estrazione = \nsidecar_estrazione_porta = 47301\n",         // programma vuoto
		"sidecar_estrazione = /opt/srv\nsidecar_estrazione_porta = 1023\n",  // porta bassa
		"sidecar_estrazione = /opt/srv\nsidecar_estrazione_porta = 65536\n", // porta alta
		"sidecar_estrazione = /opt/srv\nsidecar_estrazione_porta = 4730x\n", // porta non numero
		"sidecar_estrazione = /opt/srv\nsidecar_estrazione_porta = 47301\nsidecar_estrazione_salute = health\n",
		"sidecar_estrazione = /opt/srv\nsidecar_estrazione_porta = 47301\nsidecar_estrazione_salute =\n",
		"sidecar_estrazione = /opt/srv\nsidecar_estrazione_porta = 47301\nsidecar_estrazione_porta = 47302\n", // ripetuta
		"sidecar_altro = /opt/srv\nsidecar_altro_porta = 47301\n",                                             // famiglia sconosciuta
		"sidecar_estrazione_argomenti = -m x\n",                                                               // chiave sconosciuta
		"sidecar_estrazione = /opt/a {porta}\nsidecar_estrazione_porta = 47301\n" +
			"sidecar_trascrizione = /opt/b {porta}\nsidecar_trascrizione_porta = 47301\n", // stessa porta
		"sidecar_estrazione = /opt/srv --port 47301\nsidecar_estrazione_porta = 47301\n", // senza {porta}
	} {
		if c, err := Leggi(scrivi(t, testo)); err == nil {
			t.Errorf("%q accettato: %+v", testo, c.Sidecar)
		}
	}
	// Una riga sola dice quale manca.
	for _, testo := range []string{"sidecar_estrazione = /opt/srv\n", "sidecar_estrazione_porta = 47301\n"} {
		if _, err := Leggi(scrivi(t, testo)); err == nil || !strings.Contains(err.Error(), "vuole sia «sidecar_estrazione» sia «sidecar_estrazione_porta»") {
			t.Errorf("%q: %v", testo, err)
		}
	}
}
