package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/firmephoenix"
)

// Il segreto Flask di serve (slice 064, PIANO §1.9 e §2.4): secrets/segreto_flask accanto al
// database. Segreto inventato: il vettore sintetico dello standalone.

const segretoFlaskProva = "synthetic-legacy-secret"

// bustaStorica prepara un database con la busta «42/26», quella del vettore dello standalone.
func bustaStorica(t *testing.T, dbp string) {
	t.Helper()
	d, err := db.Apri(dbp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Chiudi() }()
	ctx := context.Background()
	if _, err := d.Migra(ctx, clock.Sistema{}); err != nil {
		t.Fatal(err)
	}
	c, err := buste.Nuove(d, clock.Sistema{}).Crea(ctx, buste.Accettazione{NomeCliente: "Cliente Prova", TelefonoCliente: "333 000 0001",
		DataAccettazione: "2026-10-31", Descrizione: "Anello sintetico"}, audit.Contesto{Attore: "titolare@esempio.it"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Scrittura.Exec(`UPDATE buste SET numero = '42/26' WHERE id = ?`, c.ID); err != nil {
		t.Fatal(err)
	}
}

// avviaConSegreto avvia serve sul database dbp e, se parte, chiede alla faccia pubblica percorso; dà
// l'errore di serve o il codice della risposta.
func avviaConSegreto(t *testing.T, dbp, percorso string) (int, error) {
	t.Helper()
	return avviaConAvvisi(t, dbp, percorso, nil)
}

// avviaConAvvisi è avviaConSegreto con gli avvisi dell'avvio in avvisi (nil: lo standard error).
func avviaConAvvisi(t *testing.T, dbp, percorso string, avvisi io.Writer) (int, error) {
	t.Helper()
	ctx, ferma := context.WithCancel(context.Background())
	defer ferma()
	finito := make(chan error, 1)
	pronto := make(chan string, 1)
	go func() {
		finito <- serve(ctx, opzioniServe{SenzaVolumeCifrato: true, DB: dbp, Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0", avvisi: avvisi},
			func(_, pubblica net.Addr) { pronto <- pubblica.String() })
	}()
	select {
	case a := <-pronto:
		r, err := http.Get("http://" + a + percorso)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		ferma()
		if err := <-finito; err != nil {
			t.Errorf("chiusura: %v", err)
		}
		return r.StatusCode, nil
	case err := <-finito:
		return 0, err
	}
}

func scriviSegretoFlask(t *testing.T, dbp, contenuto string, permessi os.FileMode) {
	t.Helper()
	p := percorsoSegretoFlask(dbp)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(contenuto), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, permessi); err != nil {
		t.Fatal(err)
	}
}

// inventario: CLI-16
// Senza il file la firma del vettore è rifiutata (404) e serve parte; con il file giusto la stessa
// firma apre la pagina; un file sbagliato ferma l'avvio senza dire il contenuto e senza toccare il
// database.
func TestServeSegretoFlask(t *testing.T) {
	const firmata = "/stato/42/26?t=dabbdeed7a356af74ce85ca0fd25b859&exp=2000000000"
	if percorsoSegretoFlask("/var/lib/gestrip/gestrip.db") != "/var/lib/gestrip/secrets/segreto_flask" {
		t.Errorf("percorso: %s", percorsoSegretoFlask("/var/lib/gestrip/gestrip.db"))
	}
	dbp := filepath.Join(t.TempDir(), "gestrip.db")
	bustaStorica(t, dbp)
	if c, err := avviaConSegreto(t, dbp, firmata); err != nil || c != http.StatusNotFound {
		t.Errorf("senza segreto: %d %v", c, err)
	}
	scriviSegretoFlask(t, dbp, segretoFlaskProva+"\n", 0o600)
	if c, err := avviaConSegreto(t, dbp, firmata); err != nil || c != http.StatusOK {
		t.Errorf("con il segreto: %d %v", c, err)
	}
	nuovo := filepath.Join(t.TempDir(), "nuovo.db")
	for nome, x := range map[string]struct {
		contenuto string
		permessi  os.FileMode
	}{
		"permessi larghi": {"SINTETICO-segreto-che-non-esce", 0o644},
		"troppo corto":    {"SINTETICO", 0o600},
	} {
		scriviSegretoFlask(t, nuovo, x.contenuto, x.permessi)
		_, err := avviaConSegreto(t, nuovo, "/healthz")
		if err == nil || !strings.Contains(err.Error(), "segreto Flask") || strings.Contains(err.Error(), "SINTETICO") {
			t.Errorf("%s: %v", nome, err)
		}
		if _, e := os.Stat(nuovo); e == nil {
			t.Errorf("%s: ha creato il database", nome)
		}
	}
}

// inventario: ART-23
// D-87: il secret_key_base dello standalone sta in secrets/segreto_standalone. Senza il file il
// Phoenix.Token della busta «42/26» è rifiutato (404) e serve parte; con il file giusto lo stesso
// gettone apre la pagina dell'artigiano; un file sbagliato ferma l'avvio senza dire il contenuto. Il file
// assente lo dice un avviso all'avvio (revisione della 084), senza il contenuto.
func TestServeSegretoStandalone(t *testing.T) {
	if percorsoSegretoStandalone("/var/lib/gestrip/gestrip.db") != "/var/lib/gestrip/secrets/segreto_standalone" {
		t.Errorf("percorso: %s", percorsoSegretoStandalone("/var/lib/gestrip/gestrip.db"))
	}
	base := strings.Repeat("SINTETICO-secret-key-base-", 3)
	dbp := filepath.Join(t.TempDir(), "gestrip.db")
	bustaStorica(t, dbp)
	g := firmephoenix.Nuovo(base).FirmaIntero(buste.SaleArtigiano, 1, time.Now().Add(-time.Minute))
	var avvisi strings.Builder
	if c, err := avviaConAvvisi(t, dbp, "/artigiano/1?t="+g, &avvisi); err != nil || c != http.StatusNotFound {
		t.Errorf("senza segreto: %d %v", c, err)
	}
	if !strings.Contains(avvisi.String(), "segreto dello standalone assente ("+percorsoSegretoStandalone(dbp)+
		"): i link Phoenix degli artigiani saranno rifiutati") {
		t.Errorf("avviso dell'assenza: %q", avvisi.String())
	}
	scriviSegreto(t, percorsoSegretoStandalone(dbp), base+"\n", 0o600)
	avvisi.Reset()
	if c, err := avviaConAvvisi(t, dbp, "/artigiano/1?t="+g, &avvisi); err != nil || c != http.StatusOK {
		t.Errorf("con il segreto: %d %v", c, err)
	}
	if strings.Contains(avvisi.String(), "standalone") || strings.Contains(avvisi.String(), "SINTETICO") {
		t.Errorf("avviso con il segreto presente: %q", avvisi.String())
	}
	nuovo := filepath.Join(t.TempDir(), "nuovo.db")
	for nome, x := range map[string]struct {
		contenuto string
		permessi  os.FileMode
	}{
		"permessi larghi": {base, 0o644},
		"troppo corto":    {"SINTETICO-corto", 0o600},
	} {
		scriviSegreto(t, percorsoSegretoStandalone(nuovo), x.contenuto, x.permessi)
		_, err := avviaConSegreto(t, nuovo, "/healthz")
		if err == nil || !strings.Contains(err.Error(), "segreto dello standalone") || strings.Contains(err.Error(), "SINTETICO") {
			t.Errorf("%s: %v", nome, err)
		}
		if _, e := os.Stat(nuovo); e == nil {
			t.Errorf("%s: ha creato il database", nome)
		}
	}
}

func scriviSegreto(t *testing.T, p, contenuto string, permessi os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(contenuto), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, permessi); err != nil {
		t.Fatal(err)
	}
}
