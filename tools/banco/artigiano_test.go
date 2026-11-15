package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/firmeflask"
	"github.com/L0cutus/gestrip/internal/firmephoenix"
)

// Le pagine dell'artigiano nel banco (slice 084): i vecchi gettoni Flask scritti nello scenario e i
// Phoenix.Token firmati a ogni richiesta, tutti con i segreti sintetici del banco.

var reArtigianoFlask = regexp.MustCompile(`^/artigiano/([^/?]+)/([^/?]+)\?t=([0-9]+)\.([0-9A-Fa-f]{16})$`)

// inventario: ART-26
// I gettoni Flask di pagina-artigiano.json sono quelli del segreto del banco sul numero scritto; gli
// unici sbagliati sono quelli voluti (zeri, maiuscole, il gettone della 4/26 su 04/26 e 4/25).
func TestFirmeScenarioArtigiano(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "bench", "scenari", "pagina-artigiano.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sc scenario
	if err := json.Unmarshal(b, &sc); err != nil {
		t.Fatal(err)
	}
	s := firmeflask.Nuovo(segretoFlaskBanco)
	giuste, volute := 0, 0
	for i, p := range sc.Passi {
		m := reArtigianoFlask.FindStringSubmatch(p.Percorso)
		if m == nil {
			continue
		}
		atteso := s.FirmaArtigiano(m[1]+"/"+m[2], mustInt(t, m[3]))
		if m[3]+"."+m[4] == atteso {
			giuste++
			continue
		}
		volute++
		if m[4] != strings.Repeat("0", 16) && strings.ToLower(m[4]) == m[4] && m[1]+"/"+m[2] == "4/26" {
			t.Errorf("passo %d: gettone %s.%s, atteso %s", i, m[3], m[4], atteso)
		}
	}
	if giuste != 3 || volute != 4 {
		t.Errorf("giuste %d, volute %d", giuste, volute)
	}
}

func mustInt(t *testing.T, s string) int64 {
	t.Helper()
	var n int64
	for _, c := range s {
		n = n*10 + int64(c-'0')
	}
	return n
}

// inventario: ART-23
// {phoenix_artigiano:<id>:<secondi fa>} diventa un Phoenix.Token del secret_key_base del banco per
// quella busta, firmato quei secondi prima: valido entro i 90 giorni, scaduto oltre; il resto del
// percorso resta com'è.
func TestGettoniPhoenix(t *testing.T) {
	adesso := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	s := firmephoenix.Nuovo(segretoStandaloneBanco)
	p := gettoniPhoenix("/artigiano/4?t={phoenix_artigiano:4:60}&x={phoenix_artigiano:12:7776060}", adesso)
	m := regexp.MustCompile(`^/artigiano/4\?t=(SFMyNTY\.[^&]+)&x=(SFMyNTY\.[^&]+)$`).FindStringSubmatch(p)
	if m == nil {
		t.Fatalf("percorso: %s", p)
	}
	if d, ok := s.VerificaIntero(buste.SaleArtigiano, m[1], buste.DurataLinkArtigiano, adesso); !ok || d != 4 {
		t.Errorf("valido: %d %v", d, ok)
	}
	if _, ok := s.VerificaIntero(buste.SaleArtigiano, m[2], buste.DurataLinkArtigiano, adesso); ok {
		t.Error("oltre i 90 giorni accettato")
	}
	if g := gettoniPhoenix("/artigiano/4?t={phoenix_artigiano:x:1}", adesso); g != "/artigiano/4?t={phoenix_artigiano:x:1}" {
		t.Errorf("segnaposto sbagliato toccato: %s", g)
	}
	if len(segretoStandaloneBanco) < firmephoenix.MinimoSegreto || !strings.Contains(segretoStandaloneBanco, "SINTETICO") {
		t.Error("il secret_key_base del banco deve essere sintetico e lungo almeno 64 byte")
	}
}

// inventario: RIP-S-25
// {href:…} di un link assoluto (il link dell'artigiano della scheda) dà il percorso con la query.
func TestPercorsoDopoAssoluto(t *testing.T) {
	prima := risposta{Corpo: `<a id="repair-artisan-link-url" href="https://negozio.example/artigiano/4?t=g1.1.AbC_-">x</a>`}
	p, err := percorsoDopo("{href:repair-artisan-link-url}", prima)
	if err != nil || p != "/artigiano/4?t=g1.1.AbC_-" {
		t.Errorf("%q %v", p, err)
	}
	if _, err := percorsoDopo("{href:repair-artisan-link-url}", risposta{Corpo: `<a id="repair-artisan-link-url" href="http://altro/x">`}); err == nil {
		t.Error("http assoluto accettato")
	}
}
