package firmeflask

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// I vettori sintetici dello standalone (LegacyPublicLinks.compatibility_evidence/0, go-baseline):
// segreto «synthetic-legacy-secret», scadenza 2000000000, adesso 2000000000. Inventati, non di una
// installazione vera.
const (
	segretoVettori = "synthetic-legacy-secret"
	scadVettori    = "2000000000"
	adessoVettori  = int64(2_000_000_000)
)

// inventario: CLI-16
// La firma dello stato del vettore dello standalone apre «42/26»; tutto il resto no: un altro
// numero, una firma toccata, maiuscola, corta o lunga, una scadenza passata o non di cifre, nessun
// segreto o un altro segreto.
func TestVerificaStatoVettori(t *testing.T) {
	s := Nuovo(segretoVettori)
	const buona = "dabbdeed7a356af74ce85ca0fd25b859"
	if !s.VerificaStato("42/26", buona, scadVettori, adessoVettori) {
		t.Fatal("il vettore dello standalone non passa")
	}
	// exp ≥ adesso: alla scadenza esatta vale ancora, un secondo dopo no.
	if !s.VerificaStato("42/26", buona, scadVettori, adessoVettori) || s.VerificaStato("42/26", buona, scadVettori, adessoVettori+1) {
		t.Error("scadenza: vale fino all'istante compreso")
	}
	// Gli zeri davanti alla scadenza non contano (Integer.to_string/1 nel messaggio firmato).
	if !s.VerificaStato("42/26", buona, "0002000000000", adessoVettori) {
		t.Error("scadenza con zeri davanti rifiutata")
	}
	for _, c := range []struct{ nome, numero, firma, scad string }{
		{"altro numero", "43/26", buona, scadVettori},
		{"numero con zeri", "042/26", buona, scadVettori},
		{"numero vuoto", "", buona, scadVettori},
		{"firma toccata", "42/26", "dabbdeed7a356af74ce85ca0fd25b858", scadVettori},
		{"firma maiuscola", "42/26", strings.ToUpper(buona), scadVettori},
		{"firma corta", "42/26", buona[:31], scadVettori},
		{"firma lunga", "42/26", buona + "0", scadVettori},
		{"firma intera (64)", "42/26", buona + buona, scadVettori},
		{"firma vuota", "42/26", "", scadVettori},
		{"scadenza vuota", "42/26", buona, ""},
		{"scadenza con segno", "42/26", buona, "+2000000000"},
		{"scadenza con spazio", "42/26", buona, " 2000000000"},
		{"scadenza non ASCII", "42/26", buona, "２000000000"},
		{"altra scadenza", "42/26", buona, "2000000001"},
	} {
		if s.VerificaStato(c.numero, c.firma, c.scad, adessoVettori) {
			t.Errorf("%s: passa", c.nome)
		}
	}
	if (Segreto{}).VerificaStato("42/26", buona, scadVettori, adessoVettori) {
		t.Error("senza segreto passa")
	}
	if Nuovo("").VerificaStato("42/26", buona, scadVettori, adessoVettori) {
		t.Error("con il segreto vuoto passa")
	}
	if Nuovo(segretoVettori+"x").VerificaStato("42/26", buona, scadVettori, adessoVettori) {
		t.Error("con un altro segreto passa")
	}
}

// inventario: CLI-16
// Una scadenza oltre i 64 bit è sempre dopo adesso (Elixir non ha limiti sugli interi): con la firma
// giusta per quel numero passa.
func TestVerificaStatoScadenzaEnorme(t *testing.T) {
	s := Nuovo(segretoVettori)
	scad := "99999999999999999999999"
	f := s.firma([]string{"stato", "42/26"}, scad, 32)
	if !s.VerificaStato("42/26", f, scad, adessoVettori) {
		t.Error("scadenza oltre int64 rifiutata")
	}
	if !s.VerificaStato("42/26", f, "000"+scad, adessoVettori) {
		t.Error("la stessa con zeri davanti rifiutata")
	}
}

// inventario: CLI-20
// La firma delle foto: il vettore dello standalone su slug e nome del file, e la firma che il Go
// rifà per la pagina /stato è la stessa che verifica.
func TestFotoBustaVettori(t *testing.T) {
	s := Nuovo(segretoVettori)
	const buona = "4b47ae564a07d5a8053794293d71d08e"
	if !s.VerificaFotoBusta("42_26", "front.png", buona, scadVettori, adessoVettori) {
		t.Fatal("il vettore dello standalone non passa")
	}
	if g := s.FirmaFotoBusta("42_26", "front.png", adessoVettori); g != buona {
		t.Errorf("firma %q, attesa %q", g, buona)
	}
	for _, c := range []struct{ nome, slug, file, firma string }{
		{"altro file", "42_26", "back.png", buona},
		{"altro slug", "43_26", "front.png", buona},
		{"slug con zeri", "042_26", "front.png", buona},
		{"slug con la barra", "42/26", "front.png", buona},
		{"slug vuoto", "", "front.png", buona},
		{"file vuoto", "42_26", "", buona},
		{"firma di zeri", "42_26", "front.png", strings.Repeat("0", 32)},
	} {
		if s.VerificaFotoBusta(c.slug, c.file, c.firma, scadVettori, adessoVettori) {
			t.Errorf("%s: passa", c.nome)
		}
	}
	if s.VerificaFotoBusta("42_26", "front.png", buona, scadVettori, adessoVettori+1) {
		t.Error("scaduta passa")
	}
	if (Segreto{}).VerificaFotoBusta("42_26", "front.png", buona, scadVettori, adessoVettori) {
		t.Error("senza segreto passa")
	}
	if g := (Segreto{}).FirmaFotoBusta("42_26", "front.png", adessoVettori); g != "" {
		t.Errorf("senza segreto firma %q", g)
	}
	if g := s.FirmaFotoBusta("", "front.png", adessoVettori); g != "" {
		t.Errorf("slug vuoto: firma %q", g)
	}
}

// inventario: CLI-16
// Le altre famiglie del Flask usano la stessa firma con le loro parti: i vettori dello standalone di
// garanzia (32), foto delle vendite (32) e artigiano (16) escono da firma.
func TestFirmaAltreFamiglie(t *testing.T) {
	s := Nuovo(segretoVettori)
	for _, c := range []struct {
		parti  []string
		n      int
		attesa string
	}{
		{[]string{"garanzia", "4711"}, 32, "be1ef8346aa9e1af8b075773dfe98b1a"},
		{[]string{"vendite/4711", "ring.png"}, 32, "452b22abd305edfe3ef9b04db47862c4"},
		{[]string{"artigiano", "42/26"}, 16, "ea27d8f154a4c60f"},
	} {
		if g := s.firma(c.parti, scadVettori, c.n); g != c.attesa {
			t.Errorf("%v: %q, attesa %q", c.parti, g, c.attesa)
		}
		if !s.verifica(c.parti, c.attesa, scadVettori, adessoVettori, c.n) {
			t.Errorf("%v: la verifica non passa", c.parti)
		}
	}
}

// inventario: CLI-16
// Il codice breve: spazi ai bordi tolti e maiuscolo, impronta sul numero; un altro numero (anche con
// gli zeri), un altro codice o un'impronta che non è di 32 byte non passano.
func TestCodiceBreve(t *testing.T) {
	imp := ImprontaCodice("42/26", "AB-CD")
	// sha256sum di «42/26:AB-CD», calcolato fuori dal codice.
	if hex.EncodeToString(imp) != "ca3abe06ca2268a8ab1f58d824f376031b9600f9c6602cf6ab785eff0cd60ae6" {
		t.Fatalf("impronta %x", imp)
	}
	for _, c := range []string{"AB-CD", " ab-cd ", "\tAb-Cd\n", "ab-cd"} {
		if !VerificaCodice("42/26", c, imp) {
			t.Errorf("%q non passa", c)
		}
	}
	for _, c := range []struct{ numero, codice string }{
		{"042/26", "AB-CD"}, {"43/26", "AB-CD"}, {"42/26", "AB-CE"}, {"42/26", "AB CD"}, {"42/26", ""}, {"", "AB-CD"},
	} {
		if VerificaCodice(c.numero, c.codice, imp) {
			t.Errorf("%q %q passa", c.numero, c.codice)
		}
	}
	if VerificaCodice("42/26", "AB-CD", imp[:31]) || VerificaCodice("42/26", "AB-CD", nil) {
		t.Error("un'impronta che non è di 32 byte passa")
	}
}

func scriviSegreto(t *testing.T, contenuto string, permessi os.FileMode) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "secrets")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "segreto_flask")
	if err := os.WriteFile(p, []byte(contenuto), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, permessi); err != nil {
		t.Fatal(err)
	}
	return p
}

// inventario: CLI-16
// Il file del segreto: assente vuol dire nessun segreto (nessun errore); presente e giusto, il valore
// senza gli spazi ai bordi; ogni file sbagliato è un errore che non riporta il contenuto.
func TestLeggi(t *testing.T) {
	assente := filepath.Join(t.TempDir(), "secrets", "segreto_flask")
	s, err := Leggi(assente)
	if err != nil || s.Presente() {
		t.Fatalf("file assente: %v, presente %v", err, s.Presente())
	}
	p := scriviSegreto(t, "  "+segretoVettori+" \n", 0o600)
	s, err = Leggi(p)
	if err != nil || !s.VerificaStato("42/26", "dabbdeed7a356af74ce85ca0fd25b859", scadVettori, adessoVettori) {
		t.Fatalf("file giusto: %v", err)
	}
	const contenutoSbagliato = "segreto-SINTETICO-che-non-deve-uscire"
	for _, c := range []struct {
		nome      string
		contenuto string
		permessi  os.FileMode
	}{
		{"permessi al gruppo", contenutoSbagliato, 0o640},
		{"permessi a tutti", contenutoSbagliato, 0o644},
		{"troppo corto", "corto-15-byte!!", 0o600},
		{"corto dopo gli spazi", "   corto-15-byte!!   ", 0o600},
		{"troppo lungo", strings.Repeat("x", MassimoSegreto+1), 0o600},
		{"non UTF-8", contenutoSbagliato + "\xff", 0o600},
	} {
		_, err := Leggi(scriviSegreto(t, c.contenuto, c.permessi))
		if err == nil {
			t.Errorf("%s: nessun errore", c.nome)
			continue
		}
		if strings.Contains(err.Error(), "SINTETICO") || strings.Contains(err.Error(), "corto-15") {
			t.Errorf("%s: l'errore riporta il contenuto: %v", c.nome, err)
		}
	}
	// Un link al file giusto non vale, una cartella nemmeno.
	link := filepath.Join(t.TempDir(), "segreto_flask")
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Leggi(link); err == nil {
		t.Error("un link passa")
	}
	if _, err := Leggi(filepath.Dir(p)); err == nil {
		t.Error("una cartella passa")
	}
}

// inventario: CLI-16
// Il segreto non si stampa: fmt con ogni verbo (anche dentro una struttura), slog e JSON danno il
// segnaposto o un indirizzo, mai i byte.
func TestSegretoNonSiStampa(t *testing.T) {
	s := Nuovo(segretoVettori)
	esadecimale := hex.EncodeToString([]byte(segretoVettori))
	controlla := func(nome, v string) {
		t.Helper()
		if strings.Contains(v, "synthetic") || strings.Contains(v, esadecimale) || strings.Contains(v, "115 121 110") {
			t.Errorf("%s mostra il segreto: %s", nome, v)
		}
	}
	for _, verbo := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d"} {
		controlla(verbo, fmt.Sprintf(verbo, s))
		controlla(verbo+" in una struttura", fmt.Sprintf(verbo, struct {
			nascosto Segreto
			Pubblico Segreto
		}{s, s}))
		controlla(verbo+" puntatore", fmt.Sprintf(verbo, &s))
	}
	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("prova", "segreto", s)
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("prova", "segreto", s)
	controlla("slog", buf.String())
	j, err := json.Marshal(map[string]any{"s": s})
	if err != nil {
		t.Fatal(err)
	}
	controlla("json", string(j))
	if s.String() != segnaposto || !strings.Contains(fmt.Sprint(s), "segreto Flask") {
		t.Errorf("segnaposto: %q", fmt.Sprint(s))
	}
}
