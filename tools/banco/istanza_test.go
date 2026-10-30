package main

import (
	"context"
	"flag"
	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// I due comandi di esegui devono portare a confronta proprio i flag voluti: la review ha
// trovato che con `sh -c … "$@"` -inventario si perdeva e le D-xx non venivano controllate.
func TestComandiEseguiPassanoInventarioEdEsito(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GESTRIP_BANCO_RUN", dir)
	n, _ := nomiPer("t0")
	inv := filepath.Join(dir, "inv.md")
	if err := os.WriteFile(inv, []byte("| D-01 | x |\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"banco", "dev.json"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("[]"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "scen"), 0o700); err != nil {
		t.Fatal(err)
	}
	cmds, err := comandiEsegui(n, filepath.Join(dir, "banco"), filepath.Join(dir, "scen"), filepath.Join(dir, "dev.json"), inv)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 2 {
		t.Fatalf("comandi = %d", len(cmds))
	}
	att, conf := cmds[0], cmds[1]
	if i := indice(att, immagineBase); i < 0 || att[i+1] != "/banco/banco" || att[i+2] != "attendi" {
		t.Errorf("attendi = %v", att)
	}
	i := indice(conf, immagineBase)
	if i < 0 || conf[i+1] != "/banco/banco" || conf[i+2] != "confronta" {
		t.Fatalf("confronta = %v", conf)
	}
	// Gli argomenti dopo «confronta» devono essere tutti flag che confronta riconosce.
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	valori := map[string]*string{}
	for _, k := range []string{"standalone", "go", "ca", "scenari", "deviazioni", "inventario", "esito", "credenziali"} {
		valori[k] = fs.String(k, "", "")
	}
	if err := fs.Parse(conf[i+3:]); err != nil || fs.NArg() != 0 {
		t.Fatalf("argomenti di confronta non validi: %v (avanzano %v)", err, fs.Args())
	}
	if *valori["inventario"] != "/banco/inventario.md" || *valori["esito"] == "" || *valori["go"] == "" || *valori["standalone"] == "" ||
		*valori["credenziali"] != "/banco/cred" || !strings.HasPrefix(*valori["go"], "https://") {
		t.Errorf("flag di confronta = %v", conf[i+3:])
	}
	for _, c := range cmds {
		if s := strings.Join(c, " "); strings.Contains(s, " -p ") || strings.Contains(s, "--publish") || strings.Contains(s, "sh -c") {
			t.Errorf("comando non ammesso: %s", s)
		}
	}
}

func TestComandiEseguiRifiutanoIngressiMancanti(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GESTRIP_BANCO_RUN", dir)
	n, _ := nomiPer("t0")
	if err := os.WriteFile(filepath.Join(dir, "dev.json"), []byte(`[{"id":"D-01"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := comandiEsegui(n, "", dir, filepath.Join(dir, "dev.json"), ""); err == nil {
		t.Error("-banco vuoto accettato")
	}
	if err := os.WriteFile(filepath.Join(dir, "banco"), nil, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := comandiEsegui(n, filepath.Join(dir, "banco"), dir, filepath.Join(dir, "dev.json"), filepath.Join(dir, "manca.md")); err == nil {
		t.Error("deviazioni presenti ma inventario mancante: accettato")
	}
}

func TestPercorsiControllati(t *testing.T) {
	elenco := filepath.Join(t.TempDir(), "percorsi.txt")
	if err := os.WriteFile(elenco, []byte("# percorsi inventati\ngestrip-negozio-esempio\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GESTRIP_PERCORSI_PRODUZIONE", elenco)
	vecchi := vietati
	vietati = percorsiProduzione()
	defer func() { vietati = vecchi }()
	for _, p := range []string{"/opt/gestrip/x", "/home/u/gestrip-negozio-esempio/data", "/tmp/a,b", "relativo/x"} {
		if err := percorsoAmmesso(p); err == nil {
			t.Errorf("%s accettato", p)
		}
	}
	if err := percorsoAmmesso(t.TempDir()); err != nil {
		t.Errorf("cartella temporanea rifiutata: %v", err)
	}
	// Un collegamento verso un percorso vietato è vietato.
	d := t.TempDir()
	if err := os.Symlink("/opt/gestrip", filepath.Join(d, "link")); err != nil {
		t.Fatal(err)
	}
	if err := percorsoAmmesso(filepath.Join(d, "link")); err == nil {
		t.Error("collegamento verso /opt/gestrip accettato")
	}
	if err := os.Symlink("/percorso/che/non/esiste", filepath.Join(d, "rotto")); err != nil {
		t.Fatal(err)
	}
	if err := percorsoAmmesso(filepath.Join(d, "rotto", "x")); err == nil {
		t.Error("collegamento rotto accettato")
	}
}

func indice(xs []string, s string) int {
	for i, x := range xs {
		if x == s {
			return i
		}
	}
	return -1
}

// Il titolare sintetico si semina nel database del Go con le stesse credenziali dello
// standalone; un database con già utenti si rifiuta.
func TestSeminaGo(t *testing.T) {
	p := filepath.Join(t.TempDir(), "gestrip.db")
	if err := seminaGo(p, "titolare-prova@example.invalid", "password-sintetica-di-prova"); err != nil {
		t.Fatal(err)
	}
	d, err := db.Apri(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Chiudi() }()
	u := accounts.NuoviUtenti(d, clock.Sistema{})
	if _, ok, err := u.Autentica(context.Background(), "titolare-prova@example.invalid", "password-sintetica-di-prova"); !ok || err != nil {
		t.Errorf("il titolare seminato non entra: %v", err)
	}
	if err := seminaGo(p, "altro@example.invalid", "x-sintetica-prova"); err == nil {
		t.Error("seconda semina accettata")
	}
}
