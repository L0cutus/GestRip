package main

// Le schermate del manuale (slice 049, docs/SVILUPPO.md «Schermate del manuale»): `banco manuale`
// fotografa il solo Go di un'istanza nuova del banco, con i dati sintetici, nelle due viste del
// livello browser (desktop 1440×1000, telefono 390×844), e copia i PNG in docs/manuale/img con i nomi
// dell'elenco dichiarativo bench/manuale/schermate.json. Lo script è tools/banco/browser/manuale.mjs,
// montato al posto di banco.mjs nella stessa immagine grgo-banco-browser (entrypoint.sh lancia
// /gate/banco.mjs): niente immagine nuova. Le schermate cambiano i dati (una busta nuova con le foto,
// le stampanti scelte, un foglio di etichette stampato sulla coda finta, una firma): per questo,
// come il livello browser, gira una volta sola su un'istanza nuova, e mai dopo `banco esegui`.

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Le soglie delle schermate: oltre si stampa un avviso, il comando non fallisce (un'interfaccia che
// cambia può far crescere un PNG di poco; lo si vede e si decide).
const (
	maxBytePNGManuale    = 250 << 10
	maxByteTotaleManuale = 3 << 20
	// maxLetturaPNGManuale è il PNG più grande che si accetta dal container.
	maxLetturaPNGManuale = 8 << 20
)

var (
	reFileSchermata    = regexp.MustCompile(`^[0-9]{2}-[a-z0-9]+(-[a-z0-9]+)*-(desktop|telefono)\.png$`)
	reRicettaSchermata = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	magicoPNGManuale   = []byte("\x89PNG\r\n\x1a\n")
)

// schermata è una voce dell'elenco: il file (con la vista in fondo al nome), la vista, la ricetta di
// manuale.mjs che prepara la pagina, la busta su cui la ricetta lavora (0 se non serve), l'elemento da
// ritagliare (selettore CSS; vuoto = la finestra intera) e la descrizione, per chi legge l'elenco.
type schermata struct {
	File        string `json:"file"`
	Vista       string `json:"vista"`
	Ricetta     string `json:"ricetta"`
	Busta       int    `json:"busta,omitempty"`
	Elemento    string `json:"elemento,omitempty"`
	Descrizione string `json:"descrizione"`
}

type elencoSchermate struct {
	Schermate []schermata `json:"schermate"`
}

// leggiSchermate legge l'elenco in modo stretto: campi noti, nomi stabili e unici, la vista del nome
// uguale a quella dichiarata.
func leggiSchermate(r io.Reader) (elencoSchermate, error) {
	var e elencoSchermate
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return e, fmt.Errorf("elenco delle schermate: %w", err)
	}
	if dec.More() {
		return e, errors.New("elenco delle schermate: dati dopo l'oggetto")
	}
	if len(e.Schermate) == 0 {
		return e, errors.New("elenco delle schermate vuoto")
	}
	visti := map[string]bool{}
	for i, s := range e.Schermate {
		switch {
		case !reFileSchermata.MatchString(s.File):
			return e, fmt.Errorf("schermata %d: nome %q non valido (NN-nome-desktop.png o NN-nome-telefono.png)", i, s.File)
		case !slices.Contains(visteBrowser, s.Vista):
			return e, fmt.Errorf("schermata %s: vista %q sconosciuta (desktop o telefono)", s.File, s.Vista)
		case !strings.HasSuffix(s.File, "-"+s.Vista+".png"):
			return e, fmt.Errorf("schermata %s: il nome non finisce con la vista %s", s.File, s.Vista)
		case visti[s.File]:
			return e, fmt.Errorf("schermata %s ripetuta", s.File)
		case !reRicettaSchermata.MatchString(s.Ricetta):
			return e, fmt.Errorf("schermata %s: ricetta %q non valida", s.File, s.Ricetta)
		case s.Busta < 0:
			return e, fmt.Errorf("schermata %s: busta negativa", s.File)
		case len(s.Elemento) > 200 || strings.ContainsAny(s.Elemento, "\r\n\x00"):
			return e, fmt.Errorf("schermata %s: elemento non valido", s.File)
		case strings.TrimSpace(s.Descrizione) == "":
			return e, fmt.Errorf("schermata %s: manca la descrizione", s.File)
		}
		visti[s.File] = true
	}
	return e, nil
}

// cartellaManuale è la sola cartella che il container delle schermate può scrivere.
func cartellaManuale(n nomi) string { return filepath.Join(n.dir, "esito", "manuale") }

// comandoManuale è il `docker run` delle schermate: come comandoBrowser, ma solo il Go (nessun
// indirizzo dello standalone), le sole credenziali sintetiche del titolare, l'elenco in sola lettura
// e la cartella esito/manuale per scrivere.
func comandoManuale(n nomi, script, elenco string) ([]string, error) {
	for _, p := range []string{script, elenco} {
		if !filepath.IsAbs(p) {
			return nil, fmt.Errorf("percorso non assoluto: %s", p)
		}
	}
	ca := filepath.Join(n.dir, "caddy-data/caddy/pki/authorities/local/root.crt")
	c := []string{"run", "--rm", "--pull", "never", "--name", n.base + "-manuale", "--network", n.rete, "--label", "grgo=1",
		"--init", "--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--read-only",
		"--security-opt", "seccomp=" + filepath.Join(n.dir, "chromium-seccomp.json"),
		"--shm-size", "256m", "--tmpfs", "/tmp:uid=1000,gid=1000,mode=1777,size=512m",
		"--cpus", "2", "--memory", "2g", "--pids-limit", "512",
		"--log-opt", "max-size=10m", "--log-opt", "max-file=2"}
	c = append(c, monta(script, "/gate/banco.mjs", true)...)
	c = append(c, monta(elenco, "/banco/schermate.json", true)...)
	c = append(c, monta(ca, "/banco/ca.crt", true)...)
	c = append(c, monta(filepath.Join(n.dir, "secrets", "admin_email"), "/banco/cred/email", true)...)
	c = append(c, monta(filepath.Join(n.dir, "secrets", "admin_password"), "/banco/cred/password", true)...)
	c = append(c, monta(cartellaManuale(n), "/banco/esito", false)...)
	c = append(c, "--env", "TEST_CA_FILE=/banco/ca.crt", "--env", "GO_URL=https://"+n.host+"-go:8443",
		"--env", "SCHERMATE=/banco/schermate.json", "--env", "USCITA=/banco/esito", immagineBrowser)
	return c, nil
}

// segnoManuale dice se su questa istanza è già girato qualcosa che cambia i dati nel browser: il
// livello browser di esegui (browser_girato) o un giro delle schermate (manuale_girato).
func segnoManuale(n nomi) (string, error) {
	b, err := leggiPiccolo(filepath.Join(n.dir, "runtime.json"), 1<<20)
	if err != nil {
		return "", fmt.Errorf("runtime.json dell'istanza (prima banco su -nome …): %w", err)
	}
	var rt map[string]any
	if err := json.Unmarshal(b, &rt); err != nil {
		return "", fmt.Errorf("runtime.json dell'istanza: %w", err)
	}
	for _, k := range []string{"browser_girato", "manuale_girato"} {
		if _, c := rt[k]; c {
			return k, nil
		}
	}
	return "", nil
}

func segnaManualeGirato(n nomi) error {
	p := filepath.Join(n.dir, "runtime.json")
	b, err := leggiPiccolo(p, 1<<20)
	if err != nil {
		return err
	}
	var rt map[string]any
	if err := json.Unmarshal(b, &rt); err != nil {
		return err
	}
	rt["manuale_girato"] = time.Now().Format(time.RFC3339)
	out, err := json.MarshalIndent(rt, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(out, '\n'), 0o600)
}

// misuraSchermata è una schermata copiata: nome, pixel e byte.
type misuraSchermata struct {
	File      string
	Larghezza int
	Altezza   int
	Byte      int64
	Oltre     bool
}

// raccogliSchermate copia in uscita i PNG dell'elenco scritti dal container in da: solo quelli
// dell'elenco, ognuno un PNG vero. Prima controlla tutti, poi scrive: un giro guasto non lascia metà
// delle immagini nuove e metà vecchie.
func raccogliSchermate(e elencoSchermate, da, uscita string) ([]misuraSchermata, error) {
	letti := map[string][]byte{}
	var mm []misuraSchermata
	for _, s := range e.Schermate {
		b, err := leggiPiccolo(filepath.Join(da, s.File), maxLetturaPNGManuale)
		if err != nil {
			return nil, fmt.Errorf("schermata %s: %w", s.File, err)
		}
		if !bytes.HasPrefix(b, magicoPNGManuale) {
			return nil, fmt.Errorf("schermata %s: non è un PNG", s.File)
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(b))
		if err != nil {
			return nil, fmt.Errorf("schermata %s: %w", s.File, err)
		}
		letti[s.File] = b
		mm = append(mm, misuraSchermata{File: s.File, Larghezza: cfg.Width, Altezza: cfg.Height, Byte: int64(len(b)),
			Oltre: len(b) > maxBytePNGManuale})
	}
	if err := os.MkdirAll(uscita, 0o755); err != nil {
		return nil, err
	}
	for _, s := range e.Schermate {
		if err := os.WriteFile(filepath.Join(uscita, s.File), letti[s.File], 0o644); err != nil {
			return nil, err
		}
	}
	return mm, nil
}

// estranee sono i PNG di uscita che l'elenco non nomina (una schermata tolta dall'elenco): si
// stampano, non si cancellano.
func estranee(e elencoSchermate, uscita string) []string {
	nomi, _ := filepath.Glob(filepath.Join(uscita, "*.png")) // l'unico errore è un pattern guasto
	var out []string
	for _, n := range nomi {
		if !slices.ContainsFunc(e.Schermate, func(s schermata) bool { return s.File == filepath.Base(n) }) {
			out = append(out, filepath.Base(n))
		}
	}
	return out
}

// opzioniManuale sono i flag di manuale.
type opzioniManuale struct {
	nome, elenco, script, uscita string
}

func cmdManuale(args []string) error {
	fs := flag.NewFlagSet("manuale", flag.ExitOnError)
	var o opzioniManuale
	fs.StringVar(&o.nome, "nome", "", "nome dell'istanza (nuova, con -dati "+datiSinteticiImport+")")
	fs.StringVar(&o.elenco, "schermate", "bench/manuale/schermate.json", "elenco delle schermate")
	fs.StringVar(&o.script, "script", "tools/banco/browser/manuale.mjs", "script delle schermate (montato nel container)")
	fs.StringVar(&o.uscita, "uscita", "docs/manuale/img", "cartella dove copiare i PNG")
	_ = fs.Parse(args) // ExitOnError: con un errore esce da sola
	if fs.NArg() > 0 {
		return fmt.Errorf("argomenti in più non riconosciuti: %v", fs.Args())
	}
	return manuale(o)
}

// manuale fa le schermate sull'istanza o.nome. Docker passa dai ganci (dockerFa, dockerProva,
// dockerLegge), così i test provano tutto senza docker.
func manuale(o opzioniManuale) error {
	n, err := nomiPer(o.nome)
	if err != nil {
		return err
	}
	elenco, err := filepath.Abs(o.elenco)
	if err != nil {
		return err
	}
	f, err := leggiFile(elenco)
	if err != nil {
		return err
	}
	e, err := leggiSchermate(f)
	if err != nil {
		return err
	}
	dati, err := datiIstanza(n)
	if err != nil {
		return err
	}
	if dati != datiSinteticiImport {
		return fmt.Errorf("le schermate vogliono i dati sintetici: banco su -nome %s … -dati %s", o.nome, datiSinteticiImport)
	}
	segno, err := segnoManuale(n)
	if err != nil {
		return err
	}
	if segno != "" {
		return fmt.Errorf("sull'istanza %s i dati sono già cambiati (%s): serve un'istanza nuova (banco giu, banco su)", n.base, segno)
	}
	script, err := filepath.Abs(o.script)
	if err != nil {
		return err
	}
	uscita, err := filepath.Abs(o.uscita)
	if err != nil {
		return err
	}
	for _, p := range []string{elenco, script, uscita} {
		if err := percorsoAmmesso(p); err != nil {
			return err
		}
	}
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("-script: %w", err)
	}
	if err := controllaImmagineBrowser(filepath.Dir(script)); err != nil {
		return err
	}
	if err := os.RemoveAll(cartellaManuale(n)); err != nil {
		return err
	}
	if err := os.MkdirAll(cartellaManuale(n), 0o700); err != nil {
		return err
	}
	c, err := comandoManuale(n, script, elenco)
	if err != nil {
		return err
	}
	if err := segnaManualeGirato(n); err != nil {
		return err
	}
	if err := dockerFa(c...); err != nil {
		return fmt.Errorf("container delle schermate: %w", err)
	}
	mm, err := raccogliSchermate(e, cartellaManuale(n), uscita)
	if err != nil {
		return err
	}
	var totale int64
	for _, m := range mm {
		totale += m.Byte
		avviso := ""
		if m.Oltre {
			avviso = fmt.Sprintf("  ATTENZIONE: oltre %d KB", maxBytePNGManuale>>10)
		}
		fmt.Printf("%-45s %4d×%-4d %6.1f KB%s\n", m.File, m.Larghezza, m.Altezza, float64(m.Byte)/1024, avviso)
	}
	fmt.Printf("schermate %d, totale %.1f KB, in %s\n", len(mm), float64(totale)/1024, uscita)
	if totale > maxByteTotaleManuale {
		fmt.Printf("ATTENZIONE: il totale supera %d MB\n", maxByteTotaleManuale>>20)
	}
	for _, x := range estranee(e, uscita) {
		fmt.Printf("ATTENZIONE: %s non è nell'elenco (non la tolgo io)\n", x)
	}
	return nil
}
