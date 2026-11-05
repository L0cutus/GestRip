package main

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const elencoProva = `{"schermate": [
 {"file": "03-foto-scheda-desktop.png", "vista": "desktop", "ricetta": "scheda", "busta": 5, "elemento": "#repair-photos", "descrizione": "foto"},
 {"file": "05-firma-pagina-telefono.png", "vista": "telefono", "ricetta": "firma-pagina", "busta": 6, "descrizione": "firma"}
]}`

func TestLeggiSchermate(t *testing.T) {
	e, err := leggiSchermate(strings.NewReader(elencoProva))
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Schermate) != 2 || e.Schermate[0].Busta != 5 || e.Schermate[1].Elemento != "" {
		t.Errorf("elenco = %+v", e)
	}
}

func TestLeggiSchermateRifiuta(t *testing.T) {
	voce := func(campi string) string { return `{"schermate": [{` + campi + `}]}` }
	ok := `"file": "03-a-desktop.png", "vista": "desktop", "ricetta": "scheda", "descrizione": "x"`
	if _, err := leggiSchermate(strings.NewReader(voce(ok))); err != nil {
		t.Fatalf("voce buona rifiutata: %v", err)
	}
	for nome, testo := range map[string]string{
		"vuoto":             `{"schermate": []}`,
		"campo sconosciuto": voce(ok + `, "altro": 1`),
		"dopo l'oggetto":    voce(ok) + ` {}`,
		"nome senza vista":  voce(`"file": "03-a.png", "vista": "desktop", "ricetta": "scheda", "descrizione": "x"`),
		"nome senza numero": voce(`"file": "a-desktop.png", "vista": "desktop", "ricetta": "scheda", "descrizione": "x"`),
		"nome con percorso": voce(`"file": "../03-a-desktop.png", "vista": "desktop", "ricetta": "scheda", "descrizione": "x"`),
		"vista diversa":     voce(`"file": "03-a-telefono.png", "vista": "desktop", "ricetta": "scheda", "descrizione": "x"`),
		"vista sconosciuta": voce(`"file": "03-a-tablet.png", "vista": "tablet", "ricetta": "scheda", "descrizione": "x"`),
		"ricetta":           voce(`"file": "03-a-desktop.png", "vista": "desktop", "ricetta": "Scheda X", "descrizione": "x"`),
		"busta negativa":    voce(ok + `, "busta": -1`),
		"elemento a capo":   voce(ok + `, "elemento": "main\nx"`),
		"senza descrizione": voce(`"file": "03-a-desktop.png", "vista": "desktop", "ricetta": "scheda", "descrizione": " "`),
		"ripetuta":          `{"schermate": [{` + ok + `}, {` + ok + `}]}`,
	} {
		if _, err := leggiSchermate(strings.NewReader(testo)); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
}

// L'elenco del repo si legge e ogni ricetta che nomina c'è in manuale.mjs.
func TestSchermateDelRepo(t *testing.T) {
	f, err := leggiFile(filepath.Join("..", "..", "bench", "manuale", "schermate.json"))
	if err != nil {
		t.Fatal(err)
	}
	e, err := leggiSchermate(f)
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join("browser", "manuale.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range e.Schermate {
		if !bytes.Contains(script, []byte("'"+s.Ricetta+"': async")) && !bytes.Contains(script, []byte("  "+s.Ricetta+": async")) {
			t.Errorf("%s: ricetta %q assente da manuale.mjs", s.File, s.Ricetta)
		}
	}
}

func TestComandoManuale(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GESTRIP_BANCO_RUN", dir)
	n, _ := nomiPer("t0")
	script, elenco := filepath.Join(dir, "manuale.mjs"), filepath.Join(dir, "schermate.json")
	c, err := comandoManuale(n, script, elenco)
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Join(c, " ")
	for _, v := range []string{"run --rm", "--pull never", "--name grgo-t0-manuale", "--network grgo-t0-net", "--label grgo=1",
		"--user 1000:1000", "--cap-drop ALL", "--security-opt no-new-privileges:true", "--read-only",
		"--security-opt seccomp=" + filepath.Join(n.dir, "chromium-seccomp.json"),
		"src=" + script + ",dst=/gate/banco.mjs,readonly", "src=" + elenco + ",dst=/banco/schermate.json,readonly",
		",dst=/banco/ca.crt,readonly", ",dst=/banco/cred/email,readonly", ",dst=/banco/cred/password,readonly",
		"src=" + filepath.Join(n.dir, "esito", "manuale") + ",dst=/banco/esito",
		"--env GO_URL=https://synthetic-t0-go:8443", "--env SCHERMATE=/banco/schermate.json", "--env USCITA=/banco/esito"} {
		if !strings.Contains(s, v) {
			t.Errorf("manca %q in %s", v, s)
		}
	}
	// Solo il Go, e dei segreti solo l'accesso del titolare.
	for _, v := range []string{"STANDALONE_URL", "dipendente", " -p ", "--publish", "--privileged", "DEBUG"} {
		if strings.Contains(s, v) {
			t.Errorf("non ammesso (%s): %s", v, s)
		}
	}
	if k := strings.Count(s, "/secrets/"); k != 2 {
		t.Errorf("segreti montati %d, attesi 2", k)
	}
	if c[len(c)-1] != immagineBrowser {
		t.Errorf("l'immagine non è l'ultimo argomento: %v", c)
	}
	if !slices.Contains(suffissiContainer(), "-manuale") {
		t.Error("banco giu non toglie il container delle schermate")
	}
	if _, err := comandoManuale(n, "relativo.mjs", elenco); err == nil {
		t.Error("script relativo accettato")
	}
}

func pngProva(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// dockerManuale simula il container delle schermate: scrive i PNG (o un file guasto) e conta i giri.
type dockerManuale struct {
	n        nomi
	guasto   bool
	fallisce bool
	giri     int
	impronta string
	t        *testing.T
}

func (d *dockerManuale) fa(args ...string) error {
	s := strings.Join(args, " ")
	if !strings.Contains(s, "--name "+d.n.base+"-manuale") {
		return errors.New("comando docker inatteso: " + s)
	}
	d.giri++
	if d.fallisce {
		return errors.New("exit status 1")
	}
	for i, f := range []string{"03-foto-scheda-desktop.png", "05-firma-pagina-telefono.png"} {
		b := pngProva(d.t, 10+i, 20)
		if d.guasto && i == 1 {
			b = []byte("non un png")
		}
		if err := os.WriteFile(filepath.Join(cartellaManuale(d.n), f), b, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func istanzaManuale(t *testing.T, dati string) (*dockerManuale, opzioniManuale) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GESTRIP_BANCO_RUN", filepath.Join(dir, "run"))
	n, _ := nomiPer("t0")
	for _, d := range []string{n.dir, filepath.Join(dir, "img")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	file := map[string]string{
		filepath.Join(n.dir, "runtime.json"):          `{"sintetica":true,"dati":"` + dati + `"}`,
		filepath.Join(dir, "schermate.json"):          elencoProva,
		filepath.Join(dir, "img", "manuale.mjs"):      "//",
		filepath.Join(dir, "uscita", "vecchia.png"):   "x",
		filepath.Join(dir, "uscita", "nota.md"):       "x",
		filepath.Join(n.dir, "esito", "manuale", "x"): "vecchio",
	}
	for _, f := range fileImmagineBrowser {
		file[filepath.Join(dir, "img", f)] = f
	}
	for p, c := range file {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	imp, err := improntaImmagineBrowser(filepath.Join(dir, "img"))
	if err != nil {
		t.Fatal(err)
	}
	d := &dockerManuale{n: n, impronta: imp, t: t}
	fa, prova, legge := dockerFa, dockerProva, dockerLegge
	t.Cleanup(func() { dockerFa, dockerProva, dockerLegge = fa, prova, legge })
	dockerFa = d.fa
	dockerProva = func(...string) error { return nil }
	dockerLegge = func(args ...string) (string, error) { return d.impronta, nil }
	return d, opzioniManuale{nome: "t0", elenco: filepath.Join(dir, "schermate.json"),
		script: filepath.Join(dir, "img", "manuale.mjs"), uscita: filepath.Join(dir, "uscita")}
}

func TestManualeOrchestrazione(t *testing.T) {
	t.Run("giro buono poi rifiutato", func(t *testing.T) {
		d, o := istanzaManuale(t, datiSinteticiImport)
		if err := manuale(o); err != nil {
			t.Fatal(err)
		}
		for i, f := range []string{"03-foto-scheda-desktop.png", "05-firma-pagina-telefono.png"} {
			b, err := os.ReadFile(filepath.Join(o.uscita, f))
			if err != nil || !bytes.Equal(b, pngProva(t, 10+i, 20)) {
				t.Errorf("%s non copiata: %v", f, err)
			}
		}
		if _, err := os.Stat(filepath.Join(o.uscita, "vecchia.png")); err != nil {
			t.Error("un PNG fuori dall'elenco è stato tolto")
		}
		if _, err := os.Stat(filepath.Join(cartellaManuale(d.n), "x")); err == nil {
			t.Error("l'uscita vecchia del container è rimasta")
		}
		if err := manuale(o); err == nil || !strings.Contains(err.Error(), "manuale_girato") || d.giri != 1 {
			t.Errorf("secondo giro: %v, giri %d", err, d.giri)
		}
	})
	t.Run("dopo il livello browser", func(t *testing.T) {
		d, o := istanzaManuale(t, datiSinteticiImport)
		if err := segnaBrowserGirato(d.n); err != nil {
			t.Fatal(err)
		}
		if err := manuale(o); err == nil || !strings.Contains(err.Error(), "browser_girato") || d.giri != 0 {
			t.Errorf("errore %v, giri %d", err, d.giri)
		}
	})
	t.Run("senza dati sintetici", func(t *testing.T) {
		d, o := istanzaManuale(t, "account-seminati")
		if err := manuale(o); err == nil || !strings.Contains(err.Error(), "dati sintetici") || d.giri != 0 {
			t.Errorf("errore %v, giri %d", err, d.giri)
		}
	})
	t.Run("PNG guasto niente copiato", func(t *testing.T) {
		d, o := istanzaManuale(t, datiSinteticiImport)
		d.guasto = true
		if err := manuale(o); err == nil || !strings.Contains(err.Error(), "non è un PNG") {
			t.Errorf("errore %v", err)
		}
		if _, err := os.Stat(filepath.Join(o.uscita, "03-foto-scheda-desktop.png")); err == nil {
			t.Error("copiata una schermata di un giro guasto")
		}
	})
	t.Run("container fallito", func(t *testing.T) {
		d, o := istanzaManuale(t, datiSinteticiImport)
		d.fallisce = true
		if err := manuale(o); err == nil || !strings.Contains(err.Error(), "container delle schermate") {
			t.Errorf("errore %v", err)
		}
	})
	t.Run("immagine vecchia", func(t *testing.T) {
		d, o := istanzaManuale(t, datiSinteticiImport)
		d.impronta = "vecchia"
		if err := manuale(o); err == nil || !strings.Contains(err.Error(), "ricostruirla") || d.giri != 0 {
			t.Errorf("errore %v, giri %d", err, d.giri)
		}
	})
}

func TestRaccogliSchermateMancante(t *testing.T) {
	e, _ := leggiSchermate(strings.NewReader(elencoProva))
	if _, err := raccogliSchermate(e, t.TempDir(), t.TempDir()); err == nil {
		t.Error("schermata mancante accettata")
	}
}
