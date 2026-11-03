package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// dockerEsegui simula docker per esegui: attendi risponde, confronta scrive l'esito HTTP (verde o
// rosso), il container del browser scrive le osservazioni (uguali o diverse) o fallisce.
type dockerEsegui struct {
	n                         nomi
	httpVerde, browserVerde   bool
	browserFallisce           bool
	immagine                  bool
	impronta                  string
	comandi                   []string
	vecchioEsito, vecchioJSON bool // al momento di attendi / del browser c'era ancora il file vecchio
}

func (d *dockerEsegui) fa(args ...string) error {
	s := strings.Join(args, " ")
	d.comandi = append(d.comandi, s)
	esito := filepath.Join(d.n.dir, "esito", "esito.json")
	switch {
	case strings.Contains(s, " attendi "):
		_, err := os.Stat(esito)
		d.vecchioEsito = err == nil
		return nil
	case strings.Contains(s, " confronta "):
		b, _ := json.Marshal(map[string]any{"verde": d.httpVerde, "dati": datiSinteticiImport, "scenari": map[string]bool{"healthz": d.httpVerde}})
		if err := os.WriteFile(esito, b, 0o600); err != nil {
			return err
		}
		if !d.httpVerde {
			return errors.New("exit status 1")
		}
		return nil
	case strings.Contains(s, "--name "+d.n.base+"-browser"):
		uscita := filepath.Join(d.n.dir, "esito", "browser", "browser.json")
		_, err := os.Stat(uscita)
		d.vecchioJSON = err == nil
		if d.browserFallisce {
			return errors.New("exit status 125")
		}
		g := "1"
		if !d.browserVerde {
			g = "2"
		}
		b, _ := json.Marshal(map[string]any{"osservazioni": map[string]any{
			"standalone": map[string]any{"prova": map[string]any{"desktop": map[string]string{"a": "1"}, "telefono": map[string]string{"a": "1"}}},
			"go":         map[string]any{"prova": map[string]any{"desktop": map[string]string{"a": g}, "telefono": map[string]string{"a": "1"}}},
		}, "errori": []string{}})
		return os.WriteFile(uscita, b, 0o600)
	}
	return errors.New("comando docker inatteso: " + s)
}

func (d *dockerEsegui) prova(args ...string) error {
	if strings.Join(args, " ") == "image inspect "+immagineBrowser && !d.immagine {
		return errors.New("no such image")
	}
	return nil
}

func (d *dockerEsegui) legge(args ...string) (string, error) {
	if len(args) > 2 && args[0] == "image" && args[2] == immagineBrowser {
		return d.impronta, nil
	}
	return "", nil
}

// istanzaFinta prepara la cartella di un'istanza, i file che esegui controlla e i ganci di docker.
func istanzaFinta(t *testing.T) (*dockerEsegui, opzioniEsegui) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GESTRIP_BANCO_RUN", filepath.Join(dir, "run"))
	n, _ := nomiPer("t0")
	for _, d := range []string{n.dir, filepath.Join(dir, "scen"), filepath.Join(dir, "browser"), filepath.Join(dir, "img")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	file := map[string]string{
		filepath.Join(n.dir, "runtime.json"):        `{"sintetica":true,"dati":"sintetici-import"}`,
		filepath.Join(dir, "banco"):                 "x",
		filepath.Join(dir, "dev.json"):              "[]",
		filepath.Join(dir, "browser", "prova.json"): `{"nome":"prova","inventario":[]}`,
		filepath.Join(dir, "img", "banco.mjs"):      "//",
	}
	for _, f := range fileImmagineBrowser {
		file[filepath.Join(dir, "img", f)] = f
	}
	for p, c := range file {
		if err := os.WriteFile(p, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	impronta, err := improntaImmagineBrowser(filepath.Join(dir, "img"))
	if err != nil {
		t.Fatal(err)
	}
	d := &dockerEsegui{n: n, httpVerde: true, browserVerde: true, immagine: true, impronta: impronta}
	fa, prova, legge := dockerFa, dockerProva, dockerLegge
	t.Cleanup(func() { dockerFa, dockerProva, dockerLegge = fa, prova, legge })
	dockerFa, dockerProva, dockerLegge = d.fa, d.prova, d.legge
	return d, opzioniEsegui{nome: "t0", bin: filepath.Join(dir, "banco"), scen: filepath.Join(dir, "scen"),
		dev: filepath.Join(dir, "dev.json"), inv: filepath.Join(dir, "manca.md"), scenBrowser: filepath.Join(dir, "browser"),
		script: filepath.Join(dir, "img", "banco.mjs")}
}

func esitoFinto(t *testing.T, d *dockerEsegui) esitoProva {
	t.Helper()
	e, _ := leggiEsitoProva(t, filepath.Join(d.n.dir, "esito", "esito.json"))
	return e
}

func TestEseguiOrchestrazione(t *testing.T) {
	t.Run("tutto verde", func(t *testing.T) {
		d, o := istanzaFinta(t)
		if err := esegui(o); err != nil {
			t.Fatal(err)
		}
		if e := esitoFinto(t, d); !e.Verde || !e.Browser.Verde || !e.Scenari["prova"] {
			t.Errorf("esito = %+v", e)
		}
	})
	t.Run("HTTP rosso e browser verde", func(t *testing.T) {
		d, o := istanzaFinta(t)
		d.httpVerde = false
		if err := esegui(o); err == nil {
			t.Error("uscita verde con il livello HTTP rosso")
		}
		if e := esitoFinto(t, d); e.Verde || !e.Browser.Verde {
			t.Errorf("esito = %+v", e)
		}
	})
	t.Run("HTTP verde e browser rosso", func(t *testing.T) {
		d, o := istanzaFinta(t)
		d.browserVerde = false
		if err := esegui(o); err == nil {
			t.Error("uscita verde con il browser rosso")
		}
		if e := esitoFinto(t, d); e.Verde || e.Browser.Verde || e.Scenari["prova"] {
			t.Errorf("esito = %+v", e)
		}
	})
	t.Run("container del browser fallito", func(t *testing.T) {
		d, o := istanzaFinta(t)
		d.browserFallisce = true
		if err := esegui(o); err == nil || !strings.Contains(err.Error(), "container del browser") {
			t.Errorf("errore = %v", err)
		}
		if e := esitoFinto(t, d); e.Verde || e.Browser.Verde || e.Browser.Errore != "non eseguito" {
			t.Errorf("esito = %+v", e)
		}
	})
	t.Run("immagine assente", func(t *testing.T) {
		d, o := istanzaFinta(t)
		d.immagine = false
		err := esegui(o)
		if err == nil || !strings.Contains(err.Error(), "immagine-browser") {
			t.Errorf("errore = %v", err)
		}
		if e := esitoFinto(t, d); e.Verde {
			t.Errorf("esito = %+v", e)
		}
	})
	t.Run("immagine vecchia", func(t *testing.T) {
		d, o := istanzaFinta(t)
		d.impronta = "vecchia"
		err := esegui(o)
		if err == nil || !strings.Contains(err.Error(), "ricostruirla") {
			t.Errorf("errore = %v", err)
		}
		if slices.ContainsFunc(d.comandi, func(c string) bool { return strings.Contains(c, "-browser") }) {
			t.Error("browser lanciato con un'immagine vecchia")
		}
	})
	t.Run("esito e osservazioni vecchi tolti", func(t *testing.T) {
		d, o := istanzaFinta(t)
		for _, p := range []string{filepath.Join(d.n.dir, "esito", "browser", "browser.json"), filepath.Join(d.n.dir, "esito", "esito.json")} {
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(`{"verde":true}`), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := esegui(o); err != nil {
			t.Fatal(err)
		}
		if d.vecchioEsito || d.vecchioJSON {
			t.Errorf("file vecchi ancora presenti: esito %v, osservazioni %v", d.vecchioEsito, d.vecchioJSON)
		}
	})
	t.Run("secondo giro rifiutato", func(t *testing.T) {
		d, o := istanzaFinta(t)
		if err := esegui(o); err != nil {
			t.Fatal(err)
		}
		d.comandi = nil
		err := esegui(o)
		if err == nil || !strings.Contains(err.Error(), "già girato") {
			t.Errorf("errore = %v", err)
		}
		if len(d.comandi) != 0 {
			t.Errorf("comandi lanciati nel secondo giro: %v", d.comandi)
		}
		// Il solo livello HTTP si può ancora rifare.
		o.senzaBrowser = true
		if err := esegui(o); err != nil {
			t.Errorf("solo HTTP: %v", err)
		}
	})
	t.Run("senza browser", func(t *testing.T) {
		d, o := istanzaFinta(t)
		o.senzaBrowser = true
		if err := esegui(o); err != nil {
			t.Fatal(err)
		}
		if slices.ContainsFunc(d.comandi, func(c string) bool { return strings.Contains(c, "-browser") }) {
			t.Error("browser lanciato con -senza-browser")
		}
	})
}
