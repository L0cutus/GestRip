package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Il passo dei token gira come gli altri: immagine dello standalone, senza rete, con le protezioni e
// i dati dell'istanza, e nient'altro; banco giu lo toglie.
func TestComandoToken(t *testing.T) {
	n, _ := istanzaDiProva(t)
	c := comandoToken(n, "img:1", []string{"--env", "DATABASE_PATH=/data/gestrip.db"})
	s := strings.Join(c, " ")
	if valoreDopo(c, "--name") != n.base+"-"+passoToken || valoreDopo(c, "--network") != "none" || indice(c, "--rm") < 0 {
		t.Errorf("nome, rete o --rm: %s", s)
	}
	if !strings.Contains(s, strings.Join(protezioniStandalone(n), " ")) {
		t.Errorf("protezioni: %s", s)
	}
	i := indice(c, "img:1")
	if i < 0 || len(c) != i+4 || c[i+1] != "bin/gestrip_standalone" || c[i+2] != "eval" || c[i+3] != scriptToken() {
		t.Fatalf("comando = %v", c)
	}
	found := false
	for _, x := range suffissiContainer() {
		found = found || x == "-"+passoToken
	}
	if !found {
		t.Error("banco giu non toglie il container dei token")
	}
}

// I token sono inventati, nella forma dello standalone (43 caratteri base64url), tutti diversi, e
// ogni scenario che apre /r/ usa uno di questi; lo script salva l'impronta SHA-256 del testo
// (hash_token/1) e il testo solo per i link vivi.
func TestTokenBanco(t *testing.T) {
	forma := regexp.MustCompile(`^SINTETICO-banco-r-[a-z]+-[0-9]+-x+$`)
	visti := map[string]bool{}
	for _, x := range tokenBanco {
		if len(x.token) != 43 || !forma.MatchString(x.token) || visti[x.token] {
			t.Errorf("token %q", x.token)
		}
		visti[x.token] = true
		if !strings.Contains(scriptToken(), `imposta.("`+x.numero+`", "`+x.token+`", "`+x.stato+`")`) {
			t.Errorf("lo script non imposta %s", x.numero)
		}
	}
	// I codici storici della slice 064, sulle due buste della cattura, con la funzione del legacy import.
	for _, c := range codiciStoriciBanco {
		if !strings.Contains(scriptToken(), `codice.("`+c.numero+`", "`+c.codice+`")`) || !strings.HasPrefix(c.codice, "SINT-") {
			t.Errorf("lo script non dà il codice a %s", c.numero)
		}
	}
	for _, voluto := range []string{"LegacyPublicLinks.repair_code_digest(r.bag_number, testo)", "LegacyPublicLinks.verify_repair_code(",
		":crypto.hash(:sha256, token)", "Workshop.get_public_repair(token)", "Workshop.public_link_status()",
		`if(atteso == "usable", do: [public_token: token,`, "System.halt(1)"} {
		if !strings.Contains(scriptToken(), voluto) {
			t.Errorf("lo script non contiene %q", voluto)
		}
	}
	scenari, err := filepath.Glob(filepath.Join("..", "..", "bench", "scenari", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	reR := regexp.MustCompile(`"/r/([^"/?#]*)`)
	reSalvato := regexp.MustCompile(`^SINTETICO-banco-r-(diretta|oreficeria|revocata|scaduta|foto|approvata|rifiutata|messaggi|negozio)-`)
	for _, f := range scenari {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range reR.FindAllStringSubmatch(string(b), -1) {
			if reSalvato.MatchString(m[1]) && !visti[m[1]] {
				t.Errorf("%s: token %q che il passo non salva", filepath.Base(f), m[1])
			}
		}
	}
}

func TestLeggiEsitoToken(t *testing.T) {
	if err := leggiEsitoToken("altro\nGR_TOKEN:ok\n"); err != nil {
		t.Error(err)
	}
	for _, out := range []string{"", "GR_TOKEN:ok\nGR_TOKEN:ok\n", "GR_TOKEN:errore [{:errore, \"2/26\", \"nil\"}]\n"} {
		if leggiEsitoToken(out) == nil {
			t.Errorf("%q accettato", out)
		}
	}
}
