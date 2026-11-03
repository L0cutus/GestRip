package main

// Il livello browser del banco (slice 040a, PIANO §4.3): Chromium con Playwright 1.58.2, la stessa
// versione dello standalone, nel container grgo-<nome>-browser sulla rete interna dell'istanza.
// Lo script (tools/banco/browser/banco.mjs) fa le stesse azioni sulle due app, nelle due viste
// (desktop 1440×1000, telefono 390×844), e scrive le osservazioni: campo → valore per app, scenario
// e vista. Qui si confrontano come al livello HTTP: ogni differenza è inattesa (banco rosso) se non
// è una D-xx approvata in bench/deviazioni.json con "livello": "browser", dove la vista è il «passo»
// (0 desktop, 1 telefono).
//
// Gli scenari stanno in bench/browser/<nome>.json: le voci dell'inventario che coprono, i dati con
// cui le provano davvero (prova_con), le attese per vista (e per i dati, come atteso_dati degli
// scenari HTTP) e le differenze «da decidere»: quelle che aspettano una decisione, con la coppia di
// valori fissata (standalone, Go). Una differenza da decidere con quella coppia si stampa e lo
// scenario non è verde per la copertura, ma il banco non diventa rosso; con un'altra coppia è
// inattesa; se le due app diventano uguali è un errore, e la voce va tolta.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// immagineBrowser si costruisce con `banco immagine-browser` da tools/banco/browser
// (docs/SVILUPPO.md): base Playwright fissata per digest, dipendenze npm dal lock, e l'etichetta
// etichettaImpronta con l'impronta dei suoi file (fileImmagineBrowser).
const (
	immagineBrowser   = "grgo-banco-browser:1.58.2"
	etichettaImpronta = "grgo.banco.impronta"
	livelloBrowser    = "browser"
)

// fileImmagineBrowser sono i file da cui dipende l'immagine; lo script degli scenari no, perché si
// monta a ogni giro.
var fileImmagineBrowser = []string{"Dockerfile", "package.json", "package-lock.json", "entrypoint.sh"}

// visteBrowser nell'ordine dei «passi» delle deviazioni.
var visteBrowser = []string{"desktop", "telefono"}

var (
	reCampoBrowser = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)
	reNomeBrowser  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

type daDecidere struct {
	Campo      string `json:"campo"`
	Standalone string `json:"standalone"`
	Go         string `json:"go"`
	Motivo     string `json:"motivo"`
}

type scenarioBrowser struct {
	Nome       string   `json:"nome"`
	Inventario []string `json:"inventario"`
	// ProvaCon come per gli scenari HTTP: con altri dati lo scenario esce «non provato».
	ProvaCon []string `json:"prova_con,omitempty"`
	// Atteso: vista → campo → valore che **tutte e due** le app devono dare, sul database con i soli
	// account seminati. AttesoDati: per modalità dei dati, le attese che lì valgono al posto di
	// quelle di Atteso o in più (come gli scenari HTTP, slice 029).
	Atteso     map[string]map[string]string            `json:"atteso,omitempty"`
	AttesoDati map[string]map[string]map[string]string `json:"atteso_dati,omitempty"`
	DaDecidere []daDecidere                            `json:"da_decidere,omitempty"`
	// SoloGo sono campi la cui attesa vale solo per il Go: il valore dello standalone si stampa
	// come NOTA, senza confronto né rosso (un difetto noto dello standalone, scritto nel motivo).
	SoloGo []campoSoloGo `json:"solo_go,omitempty"`
}

type campoSoloGo struct {
	Campo  string `json:"campo"`
	Motivo string `json:"motivo"`
}

func controllaAttese(file string, a map[string]map[string]string) error {
	for v, campi := range a {
		if !slices.Contains(visteBrowser, v) {
			return fmt.Errorf("%s: vista %q sconosciuta (desktop o telefono)", file, v)
		}
		for c := range campi {
			if !reCampoBrowser.MatchString(c) {
				return fmt.Errorf("%s: campo %q non valido", file, c)
			}
		}
	}
	return nil
}

// leggiScenarioBrowser legge uno scenario di bench/browser, in modo stretto: il nome è quello del
// file, perché la copertura lega le voci al nome del file.
func leggiScenarioBrowser(file string, r io.Reader) (scenarioBrowser, error) {
	var s scenarioBrowser
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return s, fmt.Errorf("%s: %w", file, err)
	}
	if s.Nome == "" || s.Nome+".json" != file || !reNomeBrowser.MatchString(s.Nome) {
		return s, fmt.Errorf("%s: il nome deve essere quello del file", file)
	}
	for _, id := range s.Inventario {
		if !reVoce.MatchString(id) {
			return s, fmt.Errorf("%s: voce d'inventario malformata %q", file, id)
		}
	}
	if err := controllaProvaCon(file, s.ProvaCon); err != nil {
		return s, err
	}
	if err := controllaAttese(file, s.Atteso); err != nil {
		return s, err
	}
	for d, a := range s.AttesoDati {
		if d != datiSinteticiImport {
			return s, fmt.Errorf("%s: dati %q sconosciuti in atteso_dati", file, d)
		}
		if err := controllaAttese(file, a); err != nil {
			return s, err
		}
	}
	visti := map[string]bool{}
	for _, d := range s.DaDecidere {
		if !reCampoBrowser.MatchString(d.Campo) || strings.TrimSpace(d.Motivo) == "" || visti[d.Campo] ||
			d.Standalone == "" || d.Go == "" || d.Standalone == d.Go {
			return s, fmt.Errorf("%s: da_decidere %+v: campo valido, una volta sola, i due valori diversi e il motivo", file, d)
		}
		visti[d.Campo] = true
		// Un campo da decidere non ha un valore atteso: sarebbe già deciso.
		for _, v := range visteBrowser {
			for _, dati := range []string{"", datiSinteticiImport} {
				if _, ok := attesePerBrowser(s, v, dati)[d.Campo]; ok {
					return s, fmt.Errorf("%s: %s è da decidere e ha un'attesa", file, d.Campo)
				}
			}
		}
	}
	for i, g := range s.SoloGo {
		conAttesa := false
		for _, v := range visteBrowser {
			for _, dati := range []string{"", datiSinteticiImport} {
				_, ok := attesePerBrowser(s, v, dati)[g.Campo]
				conAttesa = conAttesa || ok
			}
		}
		if !conAttesa || strings.TrimSpace(g.Motivo) == "" || visti[g.Campo] ||
			slices.ContainsFunc(s.SoloGo[:i], func(x campoSoloGo) bool { return x.Campo == g.Campo }) {
			return s, fmt.Errorf("%s: solo_go %+v: un campo con un'attesa, non da decidere, una volta sola, con il motivo", file, g)
		}
	}
	return s, nil
}

func leggiScenariBrowser(dir string) ([]scenarioBrowser, error) {
	nomi, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(nomi)
	if len(nomi) == 0 {
		return nil, fmt.Errorf("nessuno scenario browser in %s", dir)
	}
	var ss []scenarioBrowser
	for _, n := range nomi {
		f, err := leggiFile(n)
		if err != nil {
			return nil, err
		}
		s, err := leggiScenarioBrowser(filepath.Base(n), f)
		if err != nil {
			return nil, err
		}
		ss = append(ss, s)
	}
	return ss, nil
}

// attesePerBrowser dà le attese di una vista con i dati della modalità dati.
func attesePerBrowser(s scenarioBrowser, vista, dati string) map[string]string {
	out := map[string]string{}
	for c, v := range s.Atteso[vista] {
		out[c] = v
	}
	if dati != "" {
		for c, v := range s.AttesoDati[dati][vista] {
			out[c] = v
		}
	}
	return out
}

// uscitaBrowser è il file che scrive banco.mjs: app → scenario → vista → campo → valore, e gli
// errori del giro (scenario, vista, app e fase, mai indirizzi completi o valori dei moduli; un
// errore che non comincia con il nome di uno scenario è di tutto il giro).
type uscitaBrowser struct {
	Osservazioni map[string]map[string]map[string]map[string]string `json:"osservazioni"`
	Errori       []string                                           `json:"errori"`
}

func leggiUscitaBrowser(r io.Reader) (uscitaBrowser, error) {
	var u uscitaBrowser
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&u); err != nil {
		return u, fmt.Errorf("osservazioni del browser: %w", err)
	}
	for app := range u.Osservazioni {
		if app != "standalone" && app != "go" {
			return u, fmt.Errorf("osservazioni del browser: app %q sconosciuta", app)
		}
	}
	return u, nil
}

const assente = "(assente)"

func valoreOsservato(m map[string]string, c string) string {
	if v, ok := m[c]; ok {
		return v
	}
	return assente
}

// confrontaBrowser confronta le osservazioni delle due app, scenario per scenario e vista per vista.
func confrontaBrowser(ss []scenarioBrowser, u uscitaBrowser, devs deviazioni, dati string) rapporto {
	rap := rapporto{Verdi: map[string]bool{}}
	if _, err := modalitaDati(dati); err != nil {
		rap.Errori = append(rap.Errori, err.Error())
		return rap
	}
	devs = devs.delLivello(livelloBrowser)
	noti := map[string]bool{}
	for _, s := range ss {
		noti[s.Nome] = true
	}
	for _, app := range []string{"standalone", "go"} {
		for nome := range u.Osservazioni[app] {
			if !noti[nome] {
				rap.Errori = append(rap.Errori, fmt.Sprintf("osservazioni di %s per lo scenario browser sconosciuto %q", app, nome))
			}
		}
	}
	generali := false
	for _, e := range u.Errori {
		if !slices.ContainsFunc(ss, func(s scenarioBrowser) bool { return strings.HasPrefix(e, s.Nome+" ") }) {
			rap.Errori = append(rap.Errori, "browser (giro): "+e)
			generali = true
		}
	}
	for _, s := range ss {
		rap.Scenari++
		prima := rap.Inattese + len(rap.Errori) + rap.DaDecidere
		for _, e := range u.Errori {
			if strings.HasPrefix(e, s.Nome+" ") {
				rap.Errori = append(rap.Errori, "browser: "+e)
			}
		}
		soloGo := map[string]string{}
		for _, g := range s.SoloGo {
			soloGo[g.Campo] = g.Motivo
		}
		decidere := map[string]daDecidere{}
		for _, d := range s.DaDecidere {
			decidere[d.Campo] = d
		}
		// diverso: il campo differisce fra le app in almeno una vista (con la coppia fissata o no);
		// osservato: c'è dalle due parti in almeno una vista.
		diverso, osservato := map[string]bool{}, map[string]bool{}
		for i, v := range visteBrowser {
			a, okA := u.Osservazioni["standalone"][s.Nome][v]
			b, okB := u.Osservazioni["go"][s.Nome][v]
			if !okA || !okB || len(a) == 0 || len(b) == 0 {
				rap.Errori = append(rap.Errori, fmt.Sprintf("%s[%d] %s: osservazioni mancanti (standalone %d, go %d campi)", s.Nome, i, v, len(a), len(b)))
				continue
			}
			rap.Passi++
			var diff []differenza
			for c, atteso := range attesePerBrowser(s, v, dati) {
				if m, ok := soloGo[c]; ok {
					if va := valoreOsservato(a, c); va != atteso {
						rap.Righe = append(rap.Righe, fmt.Sprintf("NOTA standalone %s[%d] %s: %q (%s)", s.Nome, i, c, va, m))
					}
					if vb := valoreOsservato(b, c); vb != atteso {
						diff = append(diff, differenza{Campo: "atteso " + c + "=" + atteso + " (solo Go)", Standalone: "(non confrontato)", Go: vb})
					}
					continue
				}
				if va, vb := valoreOsservato(a, c), valoreOsservato(b, c); va != atteso || vb != atteso {
					diff = append(diff, differenza{Campo: "atteso " + c + "=" + atteso, Standalone: va, Go: vb})
				}
			}
			var campi []string
			for c := range a {
				campi = append(campi, c)
			}
			for c := range b {
				if _, ok := a[c]; !ok {
					campi = append(campi, c)
				}
			}
			sort.Strings(campi)
			for _, c := range campi {
				if _, ok := soloGo[c]; ok {
					continue
				}
				_, inA := a[c]
				_, inB := b[c]
				if inA && inB {
					osservato[c] = true
				}
				va, vb := valoreOsservato(a, c), valoreOsservato(b, c)
				if va == vb {
					continue
				}
				diverso[c] = true
				if d, ok := decidere[c]; ok && va == d.Standalone && vb == d.Go {
					rap.DaDecidere++
					rap.Righe = append(rap.Righe, "DA DECIDERE ("+d.Motivo+") "+differenza{Scenario: s.Nome, Passo: i, Campo: c, Standalone: va, Go: vb}.String())
					continue
				}
				diff = append(diff, differenza{Campo: c, Standalone: va, Go: vb})
			}
			sort.Slice(diff, func(x, y int) bool { return diff[x].Campo < diff[y].Campo })
			for _, d := range diff {
				d.Scenario, d.Passo = s.Nome, i
				if id, ok := devs.copre(d); ok {
					rap.Attese++
					rap.Righe = append(rap.Righe, "ATTESA ("+id+") "+d.String())
				} else {
					rap.Inattese++
					rap.Righe = append(rap.Righe, "INATTESA "+d.String())
				}
			}
		}
		// Una differenza da decidere osservata dalle due parti e ormai uguale va tolta dallo scenario
		// (se nessuna vista l'ha osservata dalle due parti non si sa).
		for _, d := range s.DaDecidere {
			if osservato[d.Campo] && !diverso[d.Campo] {
				rap.Errori = append(rap.Errori, fmt.Sprintf("%s: %s è da decidere ma le due app ora sono uguali: togliere la voce", s.Nome, d.Campo))
			}
		}
		switch {
		case !provato(s.ProvaCon, dati):
			rap.NonProvati = append(rap.NonProvati, s.Nome)
		default:
			// Con un errore di tutto il giro nessuno scenario è verde.
			rap.Verdi[s.Nome] = !generali && rap.Inattese+len(rap.Errori)+rap.DaDecidere == prima
		}
	}
	return rap
}

// leggiEsito legge l'esito del livello HTTP (esito.json di confronta) come mappa, per cambiarlo
// senza perdere campi.
func leggiEsito(p string) (map[string]any, error) {
	b, err := leggiPiccolo(p, 4<<20)
	if err != nil {
		return nil, fmt.Errorf("esito del livello HTTP: %w", err)
	}
	var e map[string]any
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("esito del livello HTTP: %w", err)
	}
	if _, ok := e["scenari"].(map[string]any); !ok {
		return nil, errors.New("esito del livello HTTP senza scenari")
	}
	if _, ok := e["verde"].(bool); !ok {
		return nil, errors.New("esito del livello HTTP senza verde")
	}
	return e, nil
}

func scriviEsito(p string, e map[string]any) error {
	out, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(out, '\n'), 0o644)
}

// verdeHTTP è il verdetto del solo livello HTTP: quello scritto da un giro browser di prima, se
// c'è, altrimenti quello di confronta.
func verdeHTTP(e map[string]any) bool {
	if br, ok := e["browser"].(map[string]any); ok {
		if v, ok := br["verde_http"].(bool); ok {
			return v
		}
	}
	v, _ := e["verde"].(bool)
	return v
}

// segnaBrowserNonEseguito scrive nell'esito che il browser non ha (ancora) un risultato: il banco
// non è verde finché unisciEsito non scrive il risultato vero.
func segnaBrowserNonEseguito(p string) error {
	e, err := leggiEsito(p)
	if err != nil {
		return err
	}
	vh := verdeHTTP(e)
	e["browser"] = map[string]any{"verde": false, "verde_http": vh, "errore": "non eseguito",
		"quando": time.Now().UTC().Format(time.RFC3339)}
	e["verde"] = false
	return scriviEsito(p, e)
}

// unisciEsito aggiunge all'esito del livello HTTP gli scenari browser e il loro verdetto: il banco
// è verde solo se lo sono i due livelli, e i due livelli devono aver girato con gli stessi dati.
func unisciEsito(p string, rap rapporto, dati string) error {
	e, err := leggiEsito(p)
	if err != nil {
		return err
	}
	if d, _ := e["dati"].(string); d != datiDi(dati != "") {
		return fmt.Errorf("esito del livello HTTP con i dati %q, il browser con %q", d, datiDi(dati != ""))
	}
	scenari := e["scenari"].(map[string]any)
	vh := verdeHTTP(e)
	// Un giro browser ripetuto sostituisce i suoi scenari, mai quelli HTTP.
	precedenti := map[string]bool{}
	if br, ok := e["browser"].(map[string]any); ok {
		if nomi, ok := br["scenari"].([]any); ok {
			for _, n := range nomi {
				if s, ok := n.(string); ok {
					precedenti[s] = true
				}
			}
		}
	}
	var nonProvati []string
	if l, ok := e["non_provati"].([]any); ok {
		for _, n := range l {
			if s, ok := n.(string); ok && !precedenti[s] {
				nonProvati = append(nonProvati, s)
			}
		}
	}
	for nome := range precedenti {
		delete(scenari, nome)
	}
	var nomi []string
	for nome := range rap.Verdi {
		nomi = append(nomi, nome)
	}
	nomi = append(nomi, rap.NonProvati...)
	for _, nome := range nomi {
		if _, c := scenari[nome]; c || slices.Contains(nonProvati, nome) {
			return fmt.Errorf("lo scenario browser %q ha il nome di uno scenario HTTP", nome)
		}
	}
	for nome, v := range rap.Verdi {
		scenari[nome] = v
	}
	nonProvati = append(nonProvati, rap.NonProvati...)
	sort.Strings(nonProvati)
	sort.Strings(nomi)
	e["non_provati"] = nonNil(nonProvati)
	e["browser"] = map[string]any{"verde": rap.ok(), "verde_http": vh, "scenari": nomi, "passi": rap.Passi,
		"inattese": rap.Inattese, "attese": rap.Attese, "da_decidere": rap.DaDecidere, "errori": len(rap.Errori),
		"quando": time.Now().UTC().Format(time.RFC3339)}
	e["verde"] = vh && rap.ok()
	return scriviEsito(p, e)
}

// comandoBrowser è il `docker run` del container del browser: rete interna dell'istanza, nessuna
// porta, utente 1000, nessuna capacità, sola lettura, il profilo seccomp di Chromium dello
// standalone (come il container dell'app), le sole credenziali sintetiche del titolare e del
// dipendente (lo scenario sfondo, slice 044b: le foto di un utente non le manda un altro) e la CA, e
// per scrivere la sola cartella esito/browser. Mai un pull: l'immagine è quella costruita qui.
func comandoBrowser(n nomi, script string, scenari []string, dati string) ([]string, error) {
	if _, err := modalitaDati(dati); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(script) {
		return nil, fmt.Errorf("script del browser non assoluto: %s", script)
	}
	for _, s := range scenari {
		if !reNomeBrowser.MatchString(s) {
			return nil, fmt.Errorf("nome di scenario browser non valido: %q", s)
		}
	}
	ca := filepath.Join(n.dir, "caddy-data/caddy/pki/authorities/local/root.crt")
	c := []string{"run", "--rm", "--pull", "never", "--name", n.base + "-browser", "--network", n.rete, "--label", "grgo=1",
		"--init", "--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--read-only",
		"--security-opt", "seccomp=" + filepath.Join(n.dir, "chromium-seccomp.json"),
		"--shm-size", "256m", "--tmpfs", "/tmp:uid=1000,gid=1000,mode=1777,size=512m",
		"--cpus", "2", "--memory", "2g", "--pids-limit", "512",
		"--log-opt", "max-size=10m", "--log-opt", "max-file=2"}
	c = append(c, monta(script, "/gate/banco.mjs", true)...)
	c = append(c, monta(ca, "/banco/ca.crt", true)...)
	c = append(c, monta(filepath.Join(n.dir, "secrets", "admin_email"), "/banco/cred/email", true)...)
	c = append(c, monta(filepath.Join(n.dir, "secrets", "admin_password"), "/banco/cred/password", true)...)
	c = append(c, monta(filepath.Join(n.dir, "secrets", "dipendente_email"), "/banco/cred/dipendente_email", true)...)
	c = append(c, monta(filepath.Join(n.dir, "secrets", "dipendente_password"), "/banco/cred/dipendente_password", true)...)
	c = append(c, monta(cartellaBrowser(n), "/banco/esito", false)...)
	c = append(c, "--env", "TEST_CA_FILE=/banco/ca.crt",
		"--env", "STANDALONE_URL=https://"+n.host+":8443", "--env", "GO_URL=https://"+n.host+"-go:8443",
		"--env", "DATI="+dati, "--env", "SCENARI="+strings.Join(scenari, ","),
		"--env", "USCITA=/banco/esito/browser.json", immagineBrowser)
	return c, nil
}

// cartellaBrowser è la sola cartella che il container del browser può scrivere.
func cartellaBrowser(n nomi) string { return filepath.Join(n.dir, "esito", "browser") }

// improntaImmagineBrowser è lo SHA-256 dei file dell'immagine (nome e contenuto, in ordine).
func improntaImmagineBrowser(dir string) (string, error) {
	h := sha256.New()
	for _, f := range fileImmagineBrowser {
		b, err := leggiPiccolo(filepath.Join(dir, f), 1<<20)
		if err != nil {
			return "", err
		}
		// hash.Hash.Write non fallisce mai.
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00", f, len(b))
		_, _ = h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// controllaImmagineBrowser vuole l'immagine presente e costruita dai file di adesso.
func controllaImmagineBrowser(dir string) error {
	if dockerProva("image", "inspect", immagineBrowser) != nil {
		return fmt.Errorf("immagine %s assente: banco immagine-browser (docs/SVILUPPO.md)", immagineBrowser)
	}
	voluta, err := improntaImmagineBrowser(dir)
	if err != nil {
		return err
	}
	c, err := dockerLegge("image", "inspect", immagineBrowser, "--format", `{{index .Config.Labels "`+etichettaImpronta+`"}}`)
	if err != nil {
		return err
	}
	if c != voluta {
		return fmt.Errorf("immagine %s costruita da altri file di %s (impronta %q, voluta %q): ricostruirla con banco immagine-browser",
			immagineBrowser, dir, c, voluta)
	}
	return nil
}

// cmdImmagineBrowser costruisce l'immagine del browser con l'etichetta dell'impronta; non scarica
// la base (--pull=false): va scaricata prima, per digest (docs/SVILUPPO.md).
func cmdImmagineBrowser(args []string) error {
	fs := flag.NewFlagSet("immagine-browser", flag.ExitOnError)
	dir := fs.String("dir", "tools/banco/browser", "cartella del Dockerfile")
	_ = fs.Parse(args) // ExitOnError: con un errore esce da sola
	if fs.NArg() > 0 {
		return fmt.Errorf("argomenti in più non riconosciuti: %v", fs.Args())
	}
	imp, err := improntaImmagineBrowser(*dir)
	if err != nil {
		return err
	}
	return dockerFa("build", "--pull=false", "--label", etichettaImpronta+"="+imp, "-t", immagineBrowser, *dir)
}

// controllaApprovate vuole ogni D-xx usata (deviazioni e passi solo_go o solo_standalone) approvata
// nell'inventario inv; senza inventario rifiuta se ce n'è qualcuna.
func controllaApprovate(devs deviazioni, altre []string, inv string) error {
	if inv == "" {
		if len(devs) > 0 || len(altre) > 0 {
			return errors.New("ci sono deviazioni: serve -inventario per controllare che siano approvate")
		}
		return nil
	}
	g, err := leggiFile(inv)
	if err != nil {
		return err
	}
	ok, err := deviazioniApprovate(g)
	if err != nil {
		return err
	}
	for _, d := range devs {
		if !ok[d.ID] {
			return fmt.Errorf("deviazione %s usata dal banco ma non approvata nell'inventario", d.ID)
		}
	}
	for _, id := range altre {
		if !ok[id] {
			return fmt.Errorf("deviazione %s di un passo solo_go ma non approvata nell'inventario", id)
		}
	}
	return nil
}

// confrontaBrowserDaFile legge osservazioni, scenari e deviazioni, confronta, stampa e aggiorna
// l'esito. Gira sull'host, dopo il container del browser.
func confrontaBrowserDaFile(osservazioni, dir, devf, inv, dati, esito string) error {
	ss, err := leggiScenariBrowser(dir)
	if err != nil {
		return err
	}
	f, err := leggiFile(devf)
	if err != nil {
		return err
	}
	devs, err := leggiDeviazioni(f)
	if err != nil {
		return err
	}
	if err := controllaApprovate(devs, nil, inv); err != nil {
		return err
	}
	o, err := leggiFile(osservazioni)
	if err != nil {
		return err
	}
	u, err := leggiUscitaBrowser(o)
	if err != nil {
		return err
	}
	rap := confrontaBrowser(ss, u, devs, dati)
	for _, r := range rap.Righe {
		fmt.Println(r)
	}
	for _, e := range rap.Errori {
		fmt.Println("ERRORE", e)
	}
	fmt.Printf("browser: scenari %d, viste %d, differenze attese %d, inattese %d, da decidere %d, errori %d, non provati con questi dati %d\n",
		rap.Scenari, rap.Passi, rap.Attese, rap.Inattese, rap.DaDecidere, len(rap.Errori), len(rap.NonProvati))
	if esito != "" {
		if err := unisciEsito(esito, rap, dati); err != nil {
			return err
		}
	}
	if !rap.ok() {
		return errors.New("banco browser rosso")
	}
	fmt.Println("BANCO BROWSER VERDE")
	return nil
}

// cmdConfrontaBrowser rifà il confronto da un file di osservazioni già scritto (per esempio dopo
// aver approvato una deviazione), senza rilanciare il browser.
func cmdConfrontaBrowser(args []string) error {
	fs := flag.NewFlagSet("confronta-browser", flag.ExitOnError)
	oss := fs.String("osservazioni", "", "browser.json scritto dal container del browser")
	dir := fs.String("scenari", "bench/browser", "cartella degli scenari browser")
	devf := fs.String("deviazioni", "bench/deviazioni.json", "deviazioni attese")
	inv := fs.String("inventario", "", "inventario (per controllare che ogni D-xx sia approvata)")
	dati := fs.String("dati", "", "i dati da cui sono partite le istanze: vuoto o "+datiSinteticiImport)
	esito := fs.String("esito", "", "esito.json del livello HTTP da completare")
	_ = fs.Parse(args) // ExitOnError: con un errore esce da sola
	if fs.NArg() > 0 {
		return fmt.Errorf("argomenti in più non riconosciuti: %v", fs.Args())
	}
	if *oss == "" {
		return errors.New("serve -osservazioni")
	}
	return confrontaBrowserDaFile(*oss, *dir, *devf, *inv, *dati, *esito)
}
