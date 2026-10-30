package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
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

// limiti dei container: il dettato del negozio non deve rallentare.
var limitiContainer = []string{"--cpus", "4", "--memory", "4g", "--pids-limit", "512"}

func cmdSu(args []string) error {
	fs := flag.NewFlagSet("su", flag.ExitOnError)
	nome := fs.String("nome", "", "nome dell'istanza ([a-z0-9]{1,16})")
	img := fs.String("immagine", "", "immagine dello standalone al tag go-baseline, es. gestrip-standalone:fab-213")
	bin := fs.String("gestrip", "", "binario gestrip statico (CGO_ENABLED=0)")
	_ = fs.Parse(args) // ExitOnError: con un errore esce da sola
	n, err := nomiPer(*nome)
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
	var st syscall.Statfs_t
	if err := syscall.Statfs(cartellaRun(), &st); err != nil {
		return err
	}
	libero, totale := st.Bavail*uint64(st.Bsize), st.Blocks*uint64(st.Bsize)
	if libero < 10<<30 || float64(totale-libero)/float64(totale) > 0.9 {
		return fmt.Errorf("spazio su disco insufficiente: %d GiB liberi", libero>>30)
	}
	for _, im := range []string{*img, immagineCaddy, immagineBase} {
		if dockerMuto("image", "inspect", im) != nil {
			return fmt.Errorf("immagine %s assente (niente pull né build qui)", im)
		}
	}
	if dockerMuto("network", "inspect", n.rete) == nil {
		return fmt.Errorf("%s esiste già: prima banco giu -nome %s", n.rete, *nome)
	}
	if err := os.Mkdir(n.dir, 0o700); err != nil {
		return err
	}
	for _, d := range []string{"secrets", "data", "media", "caddy-data", "caddy-config", "go-data"} {
		if err := os.Mkdir(filepath.Join(n.dir, d), 0o700); err != nil {
			return err
		}
	}
	// Il profilo seccomp di Chromium si legge dal tag, in sola lettura (mai il worktree).
	repo := strings.Replace(repoStandalone, "~", os.Getenv("HOME"), 1)
	seccomp, err := exec.Command("git", "-C", repo, "show", tagStandalone+":deploy/chromium-seccomp.json").Output()
	if err != nil {
		return fmt.Errorf("lettura del profilo seccomp da %s: %w", tagStandalone, err)
	}
	if err := os.WriteFile(filepath.Join(n.dir, "chromium-seccomp.json"), seccomp, 0o600); err != nil {
		return err
	}
	caddyfile := "{\n    admin off\n    auto_https disable_redirects\n    skip_install_trust\n}\n\n" +
		fmt.Sprintf("https://%s:8443 {\n    tls internal\n    reverse_proxy %s:4000\n}\n", n.host, n.app)
	if err := os.WriteFile(filepath.Join(n.dir, "Caddyfile"), []byte(caddyfile), 0o600); err != nil {
		return err
	}
	pw := make([]byte, 27)
	rand.Read(pw)
	valori := map[string]string{
		"sqlcipher.key":   segreto(32),
		"secret_key_base": segreto(64),
		"admin_email":     "titolare" + *nome + "@example.invalid",
		"admin_password":  base64.RawURLEncoding.EncodeToString(pw),
	}
	for k, v := range valori {
		if err := scriviPrivato(filepath.Join(n.dir, "secrets", k), v+"\n"); err != nil {
			return err
		}
	}
	if err := docker("network", "create", "--internal", "--label", "grgo=1", n.rete); err != nil {
		return err
	}
	comuni := append([]string{
		"--init", "--user", "1000:1000", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges:true", "--read-only",
		"--security-opt", "seccomp=" + filepath.Join(n.dir, "chromium-seccomp.json"),
		"--shm-size", "256m",
		"--tmpfs", "/tmp:uid=1000,gid=1000,mode=1777,size=256m",
		"--tmpfs", "/home/app:uid=1000,gid=1000,mode=0700,size=32m",
		"--log-opt", "max-size=10m", "--log-opt", "max-file=2",
		"--mount", "type=bind,src=" + filepath.Join(n.dir, "data") + ",dst=/data",
		"--mount", "type=bind,src=" + filepath.Join(n.dir, "media") + ",dst=/data/media",
		"--mount", "type=bind,src=" + filepath.Join(n.dir, "secrets") + ",dst=/run/secrets,readonly",
	}, limitiContainer...)
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
	run := func(a ...string) error { return docker(append([]string{"run"}, a...)...) }

	fmt.Println("bootstrap dello standalone…")
	a := append([]string{"--rm", "--name", n.base + "-boot", "--network", "none"}, comuni...)
	a = append(append(a, flagEnv...), *img, "bin/gestrip_standalone", "eval", "GestripStandalone.Bootstrap.run()")
	if err := run(a...); err != nil {
		return fmt.Errorf("bootstrap: %w (poi banco giu -nome %s)", err, *nome)
	}
	a = append([]string{"-d", "--name", n.app, "--network", n.rete, "--label", "grgo=1"}, comuni...)
	a = append(append(a, flagEnv...), "--env", "PHX_SERVER=true", *img)
	if err := run(a...); err != nil {
		return err
	}
	a = append([]string{"-d", "--name", n.caddy, "--network", n.rete, "--label", "grgo=1",
		"--network-alias", n.host, "--user", "1000:1000", "--cap-drop", "ALL",
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
	a = append(a, immagineBase, "/gestrip", "serve", "-db", "/data/gestrip.db",
		"-interna", "0.0.0.0:47101", "-pubblica", "0.0.0.0:47102")
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
	idApp, _ := dockerOut("image", "inspect", *img, "--format", "{{.Id}}")
	idBase, _ := dockerOut("image", "inspect", immagineBase, "--format", "{{.Id}}")
	rt, _ := json.MarshalIndent(map[string]any{
		"sintetica": true, "nome": *nome, "rete": n.rete, "porte_sull_host": false,
		"standalone": "https://" + n.host + ":8443", "go": "http://" + n.gox + ":47102",
		"immagine_standalone": *img, "id_standalone": idApp, "id_base": idBase, "gestrip": gestrip,
		"creata": time.Now().Format(time.RFC3339),
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
	su, gx := "https://"+n.host+":8443", "http://"+n.gox+":47102"
	attendi := append(append([]string{}, base...), "--name", n.base+"-attendi", immagineBase,
		"/banco/banco", "attendi", "-ca", "/banco/ca.crt", su+"/healthz", gx+"/healthz")
	conf := append(append([]string{}, base...), "--name", n.base+"-banco",
		"--mount", "type=bind,src="+abs["scenari"]+",dst=/banco/scenari,readonly",
		"--mount", "type=bind,src="+abs["deviazioni"]+",dst=/banco/deviazioni.json,readonly",
		"--mount", "type=bind,src="+filepath.Join(n.dir, "esito")+",dst=/banco/esito")
	argConf := []string{"/banco/banco", "confronta", "-ca", "/banco/ca.crt", "-standalone", su, "-go", gx,
		"-scenari", "/banco/scenari", "-deviazioni", "/banco/deviazioni.json", "-esito", "/banco/esito/esito.json"}
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
	if out, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").CombinedOutput(); err != nil {
		return fmt.Errorf("docker non risponde, non tolgo niente: %s", strings.TrimSpace(string(out)))
	}
	for _, s := range []string{"-banco", "-attendi", "-go", "-caddy", "-app", "-boot"} {
		if out, err := exec.Command("docker", "rm", "-f", "-v", n.base+s).CombinedOutput(); err != nil &&
			!strings.Contains(string(out), "No such container") {
			return fmt.Errorf("docker rm %s: %s", n.base+s, strings.TrimSpace(string(out)))
		}
	}
	if out, err := exec.Command("docker", "network", "rm", n.rete).CombinedOutput(); err != nil &&
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
		out, err := dockerOut(cmd...)
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
