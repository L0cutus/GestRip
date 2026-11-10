package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// firmaFlask è l'HMAC dei link storici con il segreto sintetico del banco, troncato a 32.
func firmaFlask(parti ...string) string {
	m := hmac.New(sha256.New, []byte(segretoFlaskBanco))
	m.Write([]byte(strings.Join(parti, ":")))
	return hex.EncodeToString(m.Sum(nil))[:32]
}

var reStorico = regexp.MustCompile(`^/(stato)/([^/]+)/([^/?]+)|^/(foto|thumb)/([^/]+)/([^/?]+)`)

// Le firme scritte negli scenari dei link storici sono quelle del segreto sintetico del banco, sul
// numero com'è scritto nel percorso (stato) o su slug e file (foto); le uniche sbagliate sono quelle
// volute: la firma di zeri e la firma di a.jpg usata per b.jpg.
func TestFirmeScenariStorici(t *testing.T) {
	sbagliate := 0
	for _, nome := range []string{"pagina-storica.json", "pagina-storica-rifiuti.json"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "bench", "scenari", nome))
		if err != nil {
			t.Fatal(err)
		}
		var sc scenario
		if err := json.Unmarshal(b, &sc); err != nil {
			t.Fatal(err)
		}
		for i, p := range sc.Passi {
			m := reStorico.FindStringSubmatch(p.Percorso)
			if m == nil {
				continue
			}
			q, _ := url.ParseQuery(p.Percorso[strings.Index(p.Percorso+"?", "?")+min(1, len(p.Percorso)-strings.Index(p.Percorso+"?", "?")):])
			if p.Corpo != "" {
				if c, err := url.ParseQuery(p.Corpo); err == nil {
					for k, v := range c {
						q[k] = v
					}
				}
			}
			var firma, attesa string
			switch {
			case m[1] == "stato" && q.Get("t") != "":
				firma, attesa = q.Get("t"), firmaFlask("stato", m[2]+"/"+m[3], q.Get("exp"))
				// La firma sul numero salvato (senza zeri) di un indirizzo scritto con gli zeri: voluta,
				// lo standalone verifica sul numero salvato.
				if salvato := firmaFlask("stato", strings.TrimLeft(m[2], "0")+"/"+m[3], q.Get("exp")); firma == salvato {
					attesa = salvato
				}
			case m[4] != "" && q.Get("sig") != "":
				firma, attesa = q.Get("sig"), firmaFlask(m[5], m[6], q.Get("exp"))
			default:
				continue
			}
			if firma != attesa {
				if firma != strings.Repeat("0", 32) && !strings.Contains(p.Percorso, "/b.jpg") {
					t.Errorf("%s passo %d: firma %s, attesa %s", nome, i, firma, attesa)
				}
				sbagliate++
			}
		}
	}
	if sbagliate != 2 {
		t.Errorf("firme sbagliate apposta: %d, attese 2", sbagliate)
	}
}

// Il segreto sintetico va al Go in secrets/segreto_flask (0700, 0600) e allo standalone con
// LEGACY_WARRANTY_SECRET_FILE; è chiaramente finto.
func TestSegretoFlaskBanco(t *testing.T) {
	if !strings.Contains(segretoFlaskBanco, "SINTETICO") || len(segretoFlaskBanco) < 16 {
		t.Errorf("segreto del banco: %q", segretoFlaskBanco)
	}
	dir := t.TempDir()
	if err := scriviSegretoFlaskGo(dir); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, "secrets"))
	if err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("cartella: %v %v", st, err)
	}
	f := filepath.Join(dir, "secrets", "segreto_flask")
	if st, err := os.Stat(f); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("file: %v", err)
	}
	if b, _ := os.ReadFile(f); string(b) != segretoFlaskBanco+"\n" {
		t.Errorf("contenuto: %q", b)
	}
	if err := scriviSegretoFlaskGo(dir); err == nil {
		t.Error("riscrive un segreto che c'è già")
	}
}
