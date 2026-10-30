package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// «gestrip primo-titolare» (ACC-16, «sostituita»): il link monouso /configura/<codice> per creare
// il primo titolare dal browser, al posto dei due file privati dell'installer dello standalone.

var reConfiguraStampato = regexp.MustCompile(`(?m)^https://negozio\.esempio\.ts\.net:8443/configura/([A-Za-z0-9_-]{43})$`)

// databaseVuoto: un database migrato senza nessun account.
func databaseVuoto(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "gestrip.db")
	d, err := db.Apri(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Chiudi() }()
	if _, err := d.Migra(context.Background(), clock.Sistema{}); err != nil {
		t.Fatal(err)
	}
	return p
}

// inventario: ACC-16
// Su un database vuoto il comando stampa sul terminale il link /configura/<codice> sull'indirizzo
// interno dell'apparecchio (o su -url); vale 60 minuti, una volta; riga
// administrator.bootstrap_requested con attore «console», senza il codice. Un secondo comando dà
// un codice nuovo e il primo non vale più.
func TestPrimoTitolareDallaConsole(t *testing.T) {
	p := databaseVuoto(t)
	conf := configurazione(t, "indirizzo_interno = https://negozio.esempio.ts.net:8443\n")
	var out bytes.Buffer
	if err := primoTitolare(context.Background(), opzioniPrimo{DB: p, Config: conf}, &out); err != nil {
		t.Fatal(err)
	}
	m := reConfiguraStampato.FindStringSubmatch(out.String())
	if m == nil {
		t.Fatalf("uscita: %q", out.String())
	}
	for _, s := range []string{"Link per creare il primo titolare", "(60 minuti) e una sola volta:",
		"Attenzione: un nuovo «gestrip primo-titolare» annulla questo link."} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("l'uscita non contiene %q: %q", s, out.String())
		}
	}
	valido := func(c string) bool {
		d, _ := db.Apri(p)
		defer func() { _ = d.Chiudi() }()
		ok, err := accounts.NuovoPrimoTitolare(d, clock.Sistema{}).CodiceValido(context.Background(), c)
		return ok && err == nil
	}
	if !valido(m[1]) {
		t.Error("il codice stampato non vale")
	}
	if got := righeDi(t, p, `SELECT group_concat(attore || '|' || azione || '|' || risorsa || '|' || id_risorsa || '|' || dati) FROM eventi_audit`); got !=
		`console|administrator.bootstrap_requested|installation|administrator|{"channel":"console"}` || strings.Contains(got, m[1]) {
		t.Errorf("registro: %q", got)
	}
	out.Reset()
	if err := primoTitolare(context.Background(), opzioniPrimo{DB: p, Config: conf, URL: "https://altro.esempio.ts.net:9443"}, &out); err != nil {
		t.Fatal(err)
	}
	m2 := regexp.MustCompile(`(?m)^https://altro\.esempio\.ts\.net:9443/configura/([A-Za-z0-9_-]{43})$`).FindStringSubmatch(out.String())
	if m2 == nil || m2[1] == m[1] || strings.Contains(out.String(), "negozio.esempio") {
		t.Fatalf("-url: %q", out.String())
	}
	if valido(m[1]) || !valido(m2[1]) {
		t.Error("il codice nuovo non annulla il vecchio")
	}
}

// inventario: ACC-16
// I rifiuti del comando non stampano nessun link e non scrivono niente: un account nel database
// (il primo titolare si crea solo su un database vuoto: il messaggio dice quali comandi servono
// dopo), nessun indirizzo, -url sbagliato, database che manca (che non si crea).
func TestPrimoTitolareDallaConsoleRifiuti(t *testing.T) {
	conf := configurazione(t, "indirizzo_interno = https://negozio.esempio.ts.net:8443\n")
	conteggi := `SELECT (SELECT count(*) FROM utenti) || '|' || (SELECT count(*) FROM codici_primo_titolare) || '|' || (SELECT count(*) FROM eventi_audit)`
	var out bytes.Buffer
	p := databaseConTitolare(t)
	err := primoTitolare(context.Background(), opzioniPrimo{DB: p, Config: conf}, &out)
	if !errors.Is(err, accounts.ErrGiaConfigurato) || out.Len() > 0 {
		t.Errorf("con un account: %v %q", err, out.String())
	}
	if err == nil || !strings.Contains(err.Error(), "solo su un database senza account") || !strings.Contains(err.Error(), "gestrip invito") ||
		!strings.Contains(err.Error(), "gestrip reset-link") {
		t.Errorf("messaggio: %v", err)
	}
	if got := righeDi(t, p, conteggi); got != "1|0|0" {
		t.Errorf("il rifiuto ha scritto: %s", got)
	}
	v := databaseVuoto(t)
	for _, o := range []opzioniPrimo{
		{Config: filepath.Join(t.TempDir(), "manca.conf")},
		{Config: conf, URL: "http://negozio.esempio.ts.net"},
		{Config: conf, URL: "https://h/percorso"},
	} {
		o.DB = v
		out.Reset()
		if err := primoTitolare(context.Background(), o, &out); err == nil || out.Len() > 0 {
			t.Errorf("%+v: %v %q", o, err, out.String())
		}
	}
	if got := righeDi(t, v, conteggi); got != "0|0|0" {
		t.Errorf("i rifiuti hanno scritto: %s", got)
	}
	out.Reset()
	if err := primoTitolare(context.Background(), opzioniPrimo{DB: v, Config: filepath.Join(t.TempDir(), "manca.conf")}, &out); err == nil ||
		!strings.Contains(err.Error(), "indirizzo_interno = https://") || !strings.Contains(err.Error(), "-url") {
		t.Errorf("senza indirizzo: %v", err)
	}
	q := filepath.Join(t.TempDir(), "manca.db")
	if err := primoTitolare(context.Background(), opzioniPrimo{DB: q, Config: conf}, &out); err == nil || !strings.Contains(err.Error(), "non trovato") || out.Len() > 0 {
		t.Errorf("database mancante: %v", err)
	}
	if _, err := os.Stat(q); err == nil {
		t.Error("creato il database")
	}
}

// inventario: ACC-16
// Il link stampato crea davvero il titolare (dal dominio, come farebbe la pagina): dopo, il comando
// rifiuta.
func TestPrimoTitolareDallaConsoleECreazione(t *testing.T) {
	p := databaseVuoto(t)
	conf := configurazione(t, "indirizzo_interno = https://negozio.esempio.ts.net:8443\n")
	var out bytes.Buffer
	if err := primoTitolare(context.Background(), opzioniPrimo{DB: p, Config: conf}, &out); err != nil {
		t.Fatal(err)
	}
	m := reConfiguraStampato.FindStringSubmatch(out.String())
	if m == nil {
		t.Fatalf("uscita: %q", out.String())
	}
	d, err := db.Apri(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := accounts.NuovoPrimoTitolare(d, clock.Sistema{}).Crea(context.Background(), m[1], "titolare@esempio.it", "una password lunga", "una password lunga", ""); err != nil {
		t.Fatal(err)
	}
	_ = d.Chiudi()
	out.Reset()
	if err := primoTitolare(context.Background(), opzioniPrimo{DB: p, Config: conf}, &out); !errors.Is(err, accounts.ErrGiaConfigurato) || out.Len() > 0 {
		t.Errorf("dopo il titolare: %v %q", err, out.String())
	}
}
