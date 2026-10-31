package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Tutto ciò che il banco crea porta il prefisso grgo- (non gbg-, che è della skill
// gestrip-browser-gate): container, rete e cartella. giu toglie solo quello.
const prefisso = "grgo-"

const (
	immagineCaddy  = "gestrip-caddy:2.11.7-unprivileged"
	immagineBase   = "debian:bookworm-slim@sha256:a4672c0cb26fbdde88e38fa2dfb6c681942306680e41e4378b28770b6e79ee91" // binari Go statici
	repoStandalone = "~/sviluppo/gestrip-standalone"
	tagStandalone  = "go-baseline"
)

var reNome = regexp.MustCompile(`^[a-z0-9]{1,16}$`)

// Percorsi di produzione: mai toccarli (stessa regola della skill). Oltre a /opt/gestrip, i
// percorsi veri dell'apparecchio di produzione stanno fuori dal repo, perché contengono il nome
// del negozio: ${GESTRIP_PERCORSI_PRODUZIONE:-~/.config/gestrip-go/percorsi-produzione.txt},
// uno per riga.
var vietati = percorsiProduzione()

func percorsiProduzione() []string {
	v := []string{"/opt/gestrip"}
	f := os.Getenv("GESTRIP_PERCORSI_PRODUZIONE")
	if f == "" {
		h, _ := os.UserHomeDir()
		f = filepath.Join(h, ".config", "gestrip-go", "percorsi-produzione.txt")
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return v
	}
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			v = append(v, l)
		}
	}
	return v
}

type nomi struct{ base, rete, app, caddy, gox, host, dir string }

func nomiPer(nome string) (nomi, error) {
	if !reNome.MatchString(nome) {
		return nomi{}, fmt.Errorf("nome %q non valido: usa [a-z0-9]{1,16}", nome)
	}
	b := prefisso + nome
	return nomi{
		base: b, rete: b + "-net", app: b + "-app", caddy: b + "-caddy", gox: b + "-go",
		host: "synthetic-" + nome,
		dir:  filepath.Join(cartellaRun(), b),
	}, nil
}

// cartellaRun sta negli artefatti temporanei del progetto (CLAUDE.md), esclusi da git.
func cartellaRun() string {
	if d := os.Getenv("GESTRIP_BANCO_RUN"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "sviluppo", "gestrip-go", "to-be-deleted-after-use", "banco")
}

func docker(args ...string) error {
	c := exec.Command("docker", args...)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
}

func dockerOut(args ...string) (string, error) {
	b, err := exec.Command("docker", args...).Output()
	return strings.TrimSpace(string(b)), err
}

func dockerMuto(args ...string) error {
	return exec.Command("docker", args...).Run()
}

func segreto(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func scriviPrivato(p, s string) error {
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(s); err != nil {
		return errors.Join(err, f.Close())
	}
	return f.Close()
}

// Agganci per i test: ciò che cmdSu e la catena chiedono a docker, a git e al disco passa da
// qui, così i test provano i rami di errore senza docker.
var (
	dockerFa    = docker
	dockerLegge = dockerOut
	dockerProva = dockerMuto
	dockerTiene = dockerTee
	daTag       = leggiDaTag
	spazioDisco = controllaSpazio
	// dockerCombina è docker con uscita ed errori insieme, per giu.
	dockerCombina = func(args ...string) ([]byte, error) { return exec.Command("docker", args...).CombinedOutput() }
)

// leggiDaTag legge un file dello standalone al tag go-baseline, mai dal worktree.
func leggiDaTag(percorso string) ([]byte, error) {
	repo := strings.Replace(repoStandalone, "~", os.Getenv("HOME"), 1)
	return exec.Command("git", "-C", repo, "show", tagStandalone+":"+percorso).Output()
}

// controllaSpazio vuole almeno 10 GiB liberi e il disco pieno al massimo al 90%.
func controllaSpazio(dir string) error {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return err
	}
	libero, totale := st.Bavail*uint64(st.Bsize), st.Blocks*uint64(st.Bsize)
	if libero < 10<<30 || float64(totale-libero)/float64(totale) > 0.9 {
		return fmt.Errorf("spazio su disco insufficiente: %d GiB liberi", libero>>30)
	}
	return nil
}

// limiti dei container: il dettato del negozio non deve rallentare.
var limitiContainer = []string{"--cpus", "4", "--memory", "4g", "--pids-limit", "512"}

func cmdSu(args []string) error {
	fs := flag.NewFlagSet("su", flag.ExitOnError)
	nome := fs.String("nome", "", "nome dell'istanza ([a-z0-9]{1,16})")
	img := fs.String("immagine", "", "immagine dello standalone al tag go-baseline, es. gestrip-standalone:fab-219")
	bin := fs.String("gestrip", "", "binario gestrip statico (CGO_ENABLED=0)")
	dati := fs.String("dati", "", "vuoto: solo i due account sintetici; "+datiSinteticiImport+
		": catena cattura Flask sintetica → legacy import dello standalone → gestrip import (PIANO §4.2)")
	_ = fs.Parse(args) // ExitOnError: con un errore esce da sola
	n, err := nomiPer(*nome)
	if err != nil {
		return err
	}
	conCatena, err := modalitaDati(*dati)
	if err != nil {
		return err
	}
	if *img == "" || *bin == "" {
		return errors.New("servono -immagine e -gestrip")
	}
	gestrip, err := filepath.Abs(*bin)
	if err != nil {
		return err
	}
	for _, p := range []string{gestrip, n.dir} {
		if err := percorsoAmmesso(p); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(cartellaRun(), 0o700); err != nil {
		return err
	}
	// Spazio su disco prima di partire (regola di Stefano, 30/10/2026; soglie della skill).
	if err := spazioDisco(cartellaRun()); err != nil {
		return err
	}
	for _, im := range []string{*img, immagineCaddy, immagineBase} {
		if dockerProva("image", "inspect", im) != nil {
			return fmt.Errorf("immagine %s assente (niente pull né build qui)", im)
		}
	}
	if dockerProva("network", "inspect", n.rete) == nil {
		return fmt.Errorf("%s esiste già: prima banco giu -nome %s", n.rete, *nome)
	}
	if err := os.Mkdir(n.dir, 0o700); err != nil {
		return err
	}
	for _, d := range []string{"secrets", "data", "media", "caddy-data", "caddy-config", "go-data", "esito"} {
		if err := os.Mkdir(filepath.Join(n.dir, d), 0o700); err != nil {
			return err
		}
	}
	// Il profilo seccomp di Chromium si legge dal tag, in sola lettura (mai il worktree).
	seccomp, err := daTag("deploy/chromium-seccomp.json")
	if err != nil {
		return fmt.Errorf("lettura del profilo seccomp da %s: %w", tagStandalone, err)
	}
	if err := os.WriteFile(filepath.Join(n.dir, "chromium-seccomp.json"), seccomp, 0o600); err != nil {
		return err
	}
	// Il Go sta dietro lo stesso Caddy, in TLS come lo standalone: i cookie sono Secure e il
	// client del banco non li rimanderebbe in http. Davanti c'è la faccia interna (login, D-28).
	if err := os.WriteFile(filepath.Join(n.dir, "Caddyfile"), []byte(caddyfile(n)), 0o600); err != nil {
		return err
	}
	pw, pwd := make([]byte, 27), make([]byte, 27)
	rand.Read(pw)
	rand.Read(pwd)
	valori := map[string]string{
		"sqlcipher.key":   segreto(32),
		"secret_key_base": segreto(64),
		"admin_email":     "titolare" + *nome + "@example.invalid",
		"admin_password":  base64.RawURLEncoding.EncodeToString(pw),
		// Il dipendente sintetico (slice 008c): invitato e con la password scelta, sulle due istanze.
		"dipendente_email":    "dipendente" + *nome + "@example.invalid",
		"dipendente_password": base64.RawURLEncoding.EncodeToString(pwd),
	}
	for k, v := range valori {
		if err := scriviPrivato(filepath.Join(n.dir, "secrets", k), v+"\n"); err != nil {
			return err
		}
	}
	if err := dockerFa("network", "create", "--internal", "--label", "grgo=1", n.rete); err != nil {
		return err
	}
	comuni := append(protezioniStandalone(n), montaggiStandalone(n)...)
	env := map[string]string{
		"PHX_HOST": n.host, "PHX_URL_PORT": "8443", "PORT": "4000",
		"DATABASE_PATH": "/data/gestrip.db", "MEDIA_ROOT": "/data/media",
		"SQLCIPHER_KEY_FILE": "/run/secrets/sqlcipher.key", "SECRET_KEY_BASE_FILE": "/run/secrets/secret_key_base",
		"ADMIN_EMAIL_FILE": "/run/secrets/admin_email", "ADMIN_PASSWORD_FILE": "/run/secrets/admin_password",
		"RELEASE_TMP": "/tmp", "RELEASE_DISTRIBUTION": "none", "ERL_CRASH_DUMP": "/dev/null",
	}
	var flagEnv []string
	for k, v := range env {
		flagEnv = append(flagEnv, "--env", k+"="+v)
	}
	run := func(a ...string) error { return dockerFa(append([]string{"run"}, a...)...) }

	fmt.Println("bootstrap dello standalone…")
	a := append([]string{"--rm", "--name", n.base + "-boot", "--network", "none"}, comuni...)
	a = append(append(a, flagEnv...), *img, "bin/gestrip_standalone", "eval", "GestripStandalone.Bootstrap.run()")
	if err := run(a...); err != nil {
		return fmt.Errorf("bootstrap: %w (poi banco giu -nome %s)", err, *nome)
	}
	fmt.Println("dipendente sintetico nello standalone…")
	a = append([]string{"--rm", "--name", n.base + "-dipendente", "--network", "none"}, comuni...)
	a = append(append(a, flagEnv...), *img, "bin/gestrip_standalone", "eval", seminaDipendenteStandalone)
	if err := run(a...); err != nil {
		return fmt.Errorf("dipendente nello standalone: %w (poi banco giu -nome %s)", err, *nome)
	}
	var riassunto *riassuntoImport
	if conCatena {
		// Tutta la catena prima di avviare un container che resta acceso: il database dello
		// standalone si copia solo quando nessuno lo usa, e se la catena si ferma non resta acceso
		// niente. Il database del Go viene da quello dello standalone, con i suoi account (titolare
		// e dipendente sintetici compresi): niente semina.
		sorgente, err := catenaStandalone(n, *img, flagEnv)
		if err != nil {
			return fmt.Errorf("%w (poi banco giu -nome %s)", err, *nome)
		}
		r, err := catenaGo(n, gestrip, sorgente)
		if err != nil {
			return fmt.Errorf("%w (poi banco giu -nome %s)", err, *nome)
		}
		riassunto = &r
		stampaRiassunto(r)
	} else if err := seminaGo(filepath.Join(n.dir, "go-data", "gestrip.db"), valori["admin_email"], valori["admin_password"],
		valori["dipendente_email"], valori["dipendente_password"]); err != nil {
		// Lo stesso titolare sintetico dello standalone, seminato prima di avviare il Go.
		return fmt.Errorf("semina del Go: %w (poi banco giu -nome %s)", err, *nome)
	}
	a = append([]string{"-d", "--name", n.app, "--network", n.rete, "--label", "grgo=1"}, comuni...)
	a = append(append(a, flagEnv...), "--env", "PHX_SERVER=true", *img)
	if err := run(a...); err != nil {
		return err
	}
	a = append([]string{"-d", "--name", n.caddy, "--network", n.rete, "--label", "grgo=1",
		"--network-alias", n.host, "--network-alias", n.host + "-go", "--user", "1000:1000", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges:true", "--read-only", "--pids-limit", "128",
		"--cpus", "1", "--memory", "512m",
		"--tmpfs", "/tmp:uid=1000,gid=1000,mode=1777,size=32m",
		"--log-opt", "max-size=10m", "--log-opt", "max-file=2",
		"--mount", "type=bind,src=" + filepath.Join(n.dir, "Caddyfile") + ",dst=/etc/caddy/Caddyfile,readonly",
		"--mount", "type=bind,src=" + filepath.Join(n.dir, "caddy-data") + ",dst=/data",
		"--mount", "type=bind,src=" + filepath.Join(n.dir, "caddy-config") + ",dst=/config"}, immagineCaddy)
	if err := run(a...); err != nil {
		return err
	}
	a = append([]string{"-d", "--name", n.gox, "--network", n.rete, "--label", "grgo=1",
		"--init", "--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
		"--read-only", "--tmpfs", "/tmp:uid=1000,gid=1000,mode=1777,size=64m",
		"--log-opt", "max-size=10m", "--log-opt", "max-file=2",
		"--mount", "type=bind,src=" + gestrip + ",dst=/gestrip,readonly",
		"--mount", "type=bind,src=" + filepath.Join(n.dir, "go-data") + ",dst=/data"}, limitiContainer...)
	// Il banco gira su pc-fisso, il cui disco non è cifrato: il controllo del volume (OPS-12) si
	// spegne qui, con l'avviso nel registro del container. Caddy raggiunge il Go dall'altro
	// container della rete interna: le facce ascoltano su 0.0.0.0, permesso solo con
	// -ascolto-non-locale (anche questo con l'avviso).
	a = append(a, immagineBase, "/gestrip", "serve", "-db", "/data/gestrip.db",
		"-interna", "0.0.0.0:47101", "-pubblica", "0.0.0.0:47102", "-senza-volume-cifrato", "-ascolto-non-locale")
	if err := run(a...); err != nil {
		return err
	}
	ca := filepath.Join(n.dir, "caddy-data/caddy/pki/authorities/local/root.crt")
	for i := 0; ; i++ {
		if _, err := os.Stat(ca); err == nil {
			break
		}
		if i > 60 {
			return errors.New("CA sintetica di Caddy assente dopo 60 s")
		}
		time.Sleep(time.Second)
	}
	idApp, _ := dockerLegge("image", "inspect", *img, "--format", "{{.Id}}")
	idBase, _ := dockerLegge("image", "inspect", immagineBase, "--format", "{{.Id}}")
	rt, _ := json.MarshalIndent(map[string]any{
		"sintetica": true, "nome": *nome, "rete": n.rete, "porte_sull_host": false,
		"standalone": "https://" + n.host + ":8443", "go": "https://" + n.host + "-go:8443",
		"immagine_standalone": *img, "id_standalone": idApp, "id_base": idBase, "gestrip": gestrip,
		"creata": time.Now().Format(time.RFC3339), "dati": datiDi(conCatena), "import_go": riassunto,
	}, "", "  ")
	if err := os.WriteFile(filepath.Join(n.dir, "runtime.json"), append(rt, '\n'), 0o600); err != nil {
		return err
	}
	fmt.Printf("PRONTA %s: rete interna, CA propria, nessuna porta sull'host, dati inventati\n", n.base)
	return nil
}

func cmdEsegui(args []string) error {
	fs := flag.NewFlagSet("esegui", flag.ExitOnError)
	nome := fs.String("nome", "", "nome dell'istanza")
	bin := fs.String("banco", "", "binario banco statico (CGO_ENABLED=0)")
	scen := fs.String("scenari", "bench/scenari", "cartella degli scenari")
	dev := fs.String("deviazioni", "bench/deviazioni.json", "deviazioni attese")
	inv := fs.String("inventario", os.Getenv("HOME")+"/Condivisi/GestRip-Go/inventario-funzioni.md", "inventario, per le D-xx approvate")
	_ = fs.Parse(args) // ExitOnError: con un errore esce da sola
	n, err := nomiPer(*nome)
	if err != nil {
		return err
	}
	cmds, err := comandiEsegui(n, *bin, *scen, *dev, *inv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(n.dir, "esito"), 0o700); err != nil {
		return err
	}
	for _, c := range cmds {
		if err := docker(c...); err != nil {
			return err
		}
	}
	fmt.Println("esito:", filepath.Join(n.dir, "esito", "esito.json"))
	return nil
}

// comandiEsegui prepara i due `docker run` del banco: attendi (le due istanze rispondono) e
// confronta. Niente shell in mezzo: gli argomenti arrivano a banco così come sono.
func comandiEsegui(n nomi, bin, scen, dev, inv string) ([][]string, error) {
	if bin == "" || scen == "" || dev == "" {
		return nil, errors.New("servono -banco, -scenari e -deviazioni")
	}
	abs := map[string]string{}
	for k, p := range map[string]string{"banco": bin, "scenari": scen, "deviazioni": dev} {
		a, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(a); err != nil {
			return nil, fmt.Errorf("-%s: %w", k, err)
		}
		if err := percorsoAmmesso(a); err != nil {
			return nil, err
		}
		abs[k] = a
	}
	devs, err := os.ReadFile(abs["deviazioni"])
	if err != nil {
		return nil, err
	}
	conInventario := false
	if a, err := filepath.Abs(inv); err == nil && inv != "" {
		if _, err := os.Stat(a); err == nil {
			abs["inventario"], conInventario = a, true
		}
	}
	if !conInventario && strings.TrimSpace(string(devs)) != "[]" {
		return nil, errors.New("ci sono deviazioni ma l'inventario non è leggibile: non si può controllare che siano approvate")
	}
	ca := filepath.Join(n.dir, "caddy-data/caddy/pki/authorities/local/root.crt")
	base := []string{"run", "--rm", "--network", n.rete, "--label", "grgo=1",
		"--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--read-only",
		"--cpus", "2", "--memory", "1g",
		"--mount", "type=bind,src=" + abs["banco"] + ",dst=/banco/banco,readonly",
		"--mount", "type=bind,src=" + ca + ",dst=/banco/ca.crt,readonly"}
	su, gx := "https://"+n.host+":8443", "https://"+n.host+"-go:8443"
	attendi := append(append([]string{}, base...), "--name", n.base+"-attendi", immagineBase,
		"/banco/banco", "attendi", "-ca", "/banco/ca.crt", su+"/healthz", gx+"/healthz")
	conf := append(append([]string{}, base...), "--name", n.base+"-banco",
		"--mount", "type=bind,src="+abs["scenari"]+",dst=/banco/scenari,readonly",
		"--mount", "type=bind,src="+abs["deviazioni"]+",dst=/banco/deviazioni.json,readonly",
		"--mount", "type=bind,src="+filepath.Join(n.dir, "esito")+",dst=/banco/esito",
		"--mount", "type=bind,src="+filepath.Join(n.dir, "secrets", "admin_email")+",dst=/banco/cred/email,readonly",
		"--mount", "type=bind,src="+filepath.Join(n.dir, "secrets", "admin_password")+",dst=/banco/cred/password,readonly",
		"--mount", "type=bind,src="+filepath.Join(n.dir, "secrets", "dipendente_email")+",dst=/banco/cred/dipendente_email,readonly",
		"--mount", "type=bind,src="+filepath.Join(n.dir, "secrets", "dipendente_password")+",dst=/banco/cred/dipendente_password,readonly")
	argConf := []string{"/banco/banco", "confronta", "-ca", "/banco/ca.crt", "-standalone", su, "-go", gx,
		"-scenari", "/banco/scenari", "-deviazioni", "/banco/deviazioni.json", "-esito", "/banco/esito/esito.json",
		"-credenziali", "/banco/cred"}
	if conInventario {
		conf = append(conf, "--mount", "type=bind,src="+abs["inventario"]+",dst=/banco/inventario.md,readonly")
		argConf = append(argConf, "-inventario", "/banco/inventario.md")
	}
	conf = append(append(conf, immagineBase), argConf...)
	return [][]string{attendi, conf}, nil
}

// percorsoAmmesso rifiuta i percorsi relativi, con virgole (romperebbero --mount) o che,
// anche attraverso collegamenti, finiscono in un percorso di produzione.
func percorsoAmmesso(p string) error {
	if !filepath.IsAbs(p) {
		return fmt.Errorf("percorso non assoluto: %s", p)
	}
	if strings.Contains(p, ",") {
		return fmt.Errorf("percorso con una virgola: %s", p)
	}
	controlla := func(x string) error {
		for _, v := range vietati {
			if strings.Contains(x, v) {
				return fmt.Errorf("percorso di produzione rifiutato: %s", p)
			}
		}
		return nil
	}
	if err := controlla(p); err != nil {
		return err
	}
	// Risolve i collegamenti sul più lungo antenato esistente.
	for x := p; ; x = filepath.Dir(x) {
		if _, err := os.Lstat(x); err == nil {
			// Un collegamento che non si risolve è rifiutato anche lui.
			r, err := filepath.EvalSymlinks(x)
			if err != nil {
				return fmt.Errorf("percorso non risolvibile %s: %w", x, err)
			}
			return controlla(r)
		}
		if x == filepath.Dir(x) {
			return nil
		}
	}
}

func cmdGiu(args []string) error {
	fs := flag.NewFlagSet("giu", flag.ExitOnError)
	nome := fs.String("nome", "", "nome dell'istanza")
	_ = fs.Parse(args) // ExitOnError: con un errore esce da sola
	n, err := nomiPer(*nome)
	if err != nil {
		return err
	}
	if err := percorsoAmmesso(n.dir); err != nil {
		return err
	}
	// Se docker non risponde (per esempio senza sg docker) non si cancella niente: i
	// container resterebbero accesi sopra cartelle sparite.
	if out, err := dockerCombina("version", "--format", "{{.Server.Version}}"); err != nil {
		return fmt.Errorf("docker non risponde, non tolgo niente: %s", strings.TrimSpace(string(out)))
	}
	for _, s := range suffissiContainer() {
		if out, err := dockerCombina("rm", "-f", "-v", n.base+s); err != nil &&
			!strings.Contains(string(out), "No such container") {
			return fmt.Errorf("docker rm %s: %s", n.base+s, strings.TrimSpace(string(out)))
		}
	}
	if out, err := dockerCombina("network", "rm", n.rete); err != nil &&
		!strings.Contains(string(out), "not found") {
		return fmt.Errorf("docker network rm %s: %s", n.rete, strings.TrimSpace(string(out)))
	}
	if !strings.HasPrefix(filepath.Base(n.dir), prefisso) {
		return fmt.Errorf("cartella inattesa %s", n.dir)
	}
	if err := os.RemoveAll(n.dir); err != nil {
		return err
	}
	fmt.Println("TOLTA", n.base)
	return cmdControlla()
}

func cmdControlla() error {
	var avanzi []string
	for tipo, cmd := range map[string][]string{
		"container": {"ps", "-a", "--format", "{{.Names}}"},
		"rete":      {"network", "ls", "--format", "{{.Name}}"},
		"volume":    {"volume", "ls", "--format", "{{.Name}}"},
	} {
		out, err := dockerLegge(cmd...)
		if err != nil {
			return err
		}
		for _, x := range strings.Fields(out) {
			if strings.HasPrefix(x, prefisso) {
				avanzi = append(avanzi, tipo+" "+x)
			}
		}
	}
	if ds, err := os.ReadDir(cartellaRun()); err == nil {
		for _, d := range ds {
			if strings.HasPrefix(d.Name(), prefisso) {
				avanzi = append(avanzi, "cartella "+filepath.Join(cartellaRun(), d.Name()))
			}
		}
	}
	if len(avanzi) > 0 {
		fmt.Println("AVANZI:", strings.Join(avanzi, ", "))
		return errors.New("ci sono avanzi grgo-")
	}
	fmt.Println("AVANZI: nessuno")
	return nil
}

// caddyfile è il Caddy davanti alle due istanze, in TLS con la CA propria. Si fida dei vicini
// privati come deploy/Caddyfile.* dello standalone (slice 208): l'X-Forwarded-For che scrive il
// banco arriva alle app con il vicino accodato, e ognuna legge la voce più a destra pubblica.
func caddyfile(n nomi) string {
	return "{\n    admin off\n    auto_https disable_redirects\n    skip_install_trust\n" +
		"    servers {\n        trusted_proxies static private_ranges\n    }\n}\n\n" +
		fmt.Sprintf("https://%s:8443 {\n    tls internal\n    reverse_proxy %s:4000\n}\n\n", n.host, n.app) +
		fmt.Sprintf("https://%s:8443 {\n    tls internal\n    reverse_proxy %s:47101\n}\n", n.host+"-go", n.gox)
}

// seminaDipendenteStandalone invita il dipendente sintetico nello standalone e ne accetta l'invito
// con la sua password, con le funzioni dello standalone (Accounts.invite_user/2 e
// accept_invitation/2), avviato come fa Bootstrap.run/0: senza HTTP e senza Chromium.
const seminaDipendenteStandalone = `Application.load(:gestrip_standalone)
endpoint = Application.fetch_env!(:gestrip_standalone, GestripStandaloneWeb.Endpoint)
Application.put_env(:gestrip_standalone, GestripStandaloneWeb.Endpoint, Keyword.put(endpoint, :server, false))
Application.put_env(:gestrip_standalone, :start_chromic_pdf, false)
{:ok, _} = Application.ensure_all_started(:gestrip_standalone)
leggi = fn f -> "/run/secrets/" |> Kernel.<>(f) |> File.read!() |> String.trim_trailing() end
{:ok, %{token: token}} = GestripStandalone.Accounts.invite_user(%{"email" => leggi.("dipendente_email"), "role" => "staff"}, %{actor: "banco"})
:ok = GestripStandalone.Accounts.accept_invitation(token, leggi.("dipendente_password"))
IO.puts("Dipendente sintetico pronto")`

// seminaGo crea il database del Go con il titolare e il dipendente sintetici (le stesse
// credenziali inventate dello standalone del banco). Rifiuta un database che ha già utenti.
func seminaGo(percorso, email, password, emailDipendente, passwordDipendente string) error {
	d, err := db.Apri(percorso)
	if err != nil {
		return err
	}
	defer func() { _ = d.Chiudi() }()
	ctx := context.Background()
	if _, err := d.Migra(ctx, clock.Sistema{}); err != nil {
		return err
	}
	var n int
	if err := d.Lettura.QueryRowContext(ctx, `SELECT count(*) FROM utenti`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return errors.New("il database del Go ha già utenti")
	}
	adesso := clock.Sistema{}.Adesso().Format(db.FormatoIstante)
	_, err = d.Scrittura.ExecContext(ctx, `INSERT INTO utenti (email, hash_password, ruolo, creato_il, aggiornato_il) VALUES (?, ?, ?, ?, ?), (?, ?, ?, ?, ?)`,
		accounts.NormalizzaEmail(email), accounts.Hash(strings.TrimSpace(password)), accounts.Titolare, adesso, adesso,
		accounts.NormalizzaEmail(emailDipendente), accounts.Hash(strings.TrimSpace(passwordDipendente)), accounts.Dipendente, adesso, adesso)
	return err
}
