package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	_ "modernc.org/sqlite"
)

// inventario: ADM-20
// Il binario serve /healthz su tutte e due le facce, solo su loopback di default.
func TestServeRispondeSuDueFacce(t *testing.T) {
	ctx, ferma := context.WithCancel(context.Background())
	pronto := make(chan [2]string, 1)
	finito := make(chan error, 1)
	go func() {
		finito <- serve(ctx, opzioniServe{
			DB:       filepath.Join(t.TempDir(), "gestrip.db"),
			Interna:  "127.0.0.1:0",
			Pubblica: "127.0.0.1:0",
		}, func(interna, pubblica net.Addr) { pronto <- [2]string{interna.String(), pubblica.String()} })
	}()
	var indirizzi [2]string
	select {
	case indirizzi = <-pronto:
	case err := <-finito:
		t.Fatal(err)
	case <-time.After(10 * time.Second):
		t.Fatal("serve non parte")
	}
	for _, a := range indirizzi {
		r, err := http.Get("http://" + a + "/healthz")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if r.StatusCode != 200 || string(b) != "ok" {
			t.Errorf("%s: %d %q", a, r.StatusCode, b)
		}
	}
	ferma()
	if err := <-finito; err != nil {
		t.Errorf("chiusura: %v", err)
	}
}

func TestServeRifiutaLeFacceSullaStessaPorta(t *testing.T) {
	err := serve(context.Background(), opzioniServe{
		DB: filepath.Join(t.TempDir(), "g.db"), Interna: "127.0.0.1:47999", Pubblica: "127.0.0.1:47999",
	}, nil)
	if err == nil {
		t.Error("interna e pubblica sullo stesso indirizzo accettate")
	}
}

// All'avvio il database è migrato prima di aprire le facce; un database estraneo ferma l'avvio.
func TestServeMigraPrimaDiAscoltare(t *testing.T) {
	percorso := filepath.Join(t.TempDir(), "gestrip.db")
	ctx, ferma := context.WithCancel(context.Background())
	finito := make(chan error, 1)
	pronto := make(chan struct{})
	go func() {
		finito <- serve(ctx, opzioniServe{DB: percorso, Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0"},
			func(net.Addr, net.Addr) { close(pronto) })
	}()
	select {
	case <-pronto:
	case err := <-finito:
		t.Fatal(err)
	}
	d, err := sql.Open("sqlite", "file:"+percorso+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil || n == 0 {
		t.Errorf("schema_migrations: %d righe, %v", n, err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	ferma()
	if err := <-finito; err != nil {
		t.Errorf("chiusura: %v", err)
	}

	estraneo := filepath.Join(t.TempDir(), "altro.db")
	e, _ := sql.Open("sqlite", estraneo)
	if _, err := e.Exec(`CREATE TABLE altro (x)`); err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	breve, annulla := context.WithTimeout(context.Background(), 2*time.Second)
	defer annulla()
	err = serve(breve, opzioniServe{DB: estraneo, Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0"},
		func(net.Addr, net.Addr) { t.Error("ha aperto le facce su un database estraneo") })
	if err == nil {
		t.Error("avvio su un database estraneo accettato")
	}
}

// «OPTIONS *» arriva alle facce, che rispondono il 404 dello standalone: senza l'opzione del
// server, net/http risponderebbe da solo 200 vuoto.
func TestServeOptionsAsterisco(t *testing.T) {
	ctx, ferma := context.WithCancel(context.Background())
	defer ferma()
	pronto := make(chan string, 1)
	finito := make(chan error, 1)
	go func() {
		finito <- serve(ctx, opzioniServe{DB: filepath.Join(t.TempDir(), "g.db"), Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0"},
			func(i, _ net.Addr) { pronto <- i.String() })
	}()
	var a string
	select {
	case a = <-pronto:
	case err := <-finito:
		t.Fatal(err)
	}
	c, err := net.Dial("tcp", a)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if _, err := c.Write([]byte("OPTIONS * HTTP/1.1\r\nHost: negozio.esempio\r\nConnection: close\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(c)
	if !strings.HasPrefix(string(b), "HTTP/1.1 404 ") || !strings.HasSuffix(string(b), "Not Found") {
		t.Errorf("OPTIONS * = %q", b)
	}
}

// I server delle facce hanno i tempi massimi di lettura e di attesa (review della 006b).
func TestNuovoServer(t *testing.T) {
	s := nuovoServer(http.NotFoundHandler())
	if s.ReadHeaderTimeout == 0 || s.ReadTimeout == 0 || s.IdleTimeout == 0 || !s.DisableGeneralOptionsHandler {
		t.Errorf("server = %+v", s)
	}
}

// All'avvio la chiave del server esiste (creata alla prima partenza) e resta la stessa al
// riavvio; una chiave rovinata ferma l'avvio prima di aprire le facce.
func TestServeChiaveServer(t *testing.T) {
	percorso := filepath.Join(t.TempDir(), "gestrip.db")
	avvia := func() error {
		ctx, ferma := context.WithCancel(context.Background())
		defer ferma()
		finito := make(chan error, 1)
		pronto := make(chan struct{})
		go func() {
			finito <- serve(ctx, opzioniServe{DB: percorso, Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0"},
				func(net.Addr, net.Addr) { close(pronto) })
		}()
		select {
		case <-pronto:
			ferma()
			return <-finito
		case err := <-finito:
			return err
		}
	}
	chiave := func() string {
		d, err := sql.Open("sqlite", "file:"+percorso+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = d.Close() }()
		var v string
		if err := d.QueryRow(`SELECT hex(valore) FROM segreti WHERE nome = 'chiave_server'`).Scan(&v); err != nil {
			t.Fatalf("chiave del server assente: %v", err)
		}
		return v
	}
	if err := avvia(); err != nil {
		t.Fatal(err)
	}
	k1 := chiave()
	if err := avvia(); err != nil {
		t.Fatal(err)
	}
	if chiave() != k1 {
		t.Error("la chiave del server è cambiata al riavvio")
	}
	d, err := sql.Open("sqlite", percorso)
	if err != nil {
		t.Fatal(err)
	}
	// Una riga rovinata arriva solo con i controlli spenti (copia manomessa).
	if _, err := d.Exec(`PRAGMA ignore_check_constraints = ON; UPDATE segreti SET valore = x'01' WHERE nome = 'chiave_server'`); err != nil {
		t.Fatal(err)
	}
	_ = d.Close()
	breve, annulla := context.WithTimeout(context.Background(), 2*time.Second)
	defer annulla()
	if err := serve(breve, opzioniServe{DB: percorso, Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0"},
		func(net.Addr, net.Addr) { t.Error("facce aperte con una chiave rovinata") }); err == nil {
		t.Error("avvio con una chiave rovinata accettato")
	}
}

// inventario: ACC-13
// D-04: «gestrip reset-link» stampa sul terminale (e solo lì) il link per reimpostare la password
// del titolare, sull'indirizzo della faccia interna dato a mano; vale 30 minuti, una volta, e lascia
// una riga nel registro delle azioni senza il token. Un indirizzo che non è https://host[:porta], o
// un database senza titolare, è un errore senza link.
func TestResetLink(t *testing.T) {
	p := filepath.Join(t.TempDir(), "gestrip.db")
	d, err := db.Apri(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Migra(context.Background(), clock.Sistema{}); err != nil {
		t.Fatal(err)
	}
	if err := d.Chiudi(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := resetLink(context.Background(), opzioniReset{DB: p, URL: "https://negozio.esempio.ts.net:8443"}, &out); !errors.Is(err, accounts.ErrNessunTitolare) {
		t.Fatalf("senza titolare: %v", err)
	}
	d, _ = db.Apri(p)
	if _, err := d.Scrittura.Exec(`INSERT INTO utenti (email, ruolo, hash_password, creato_il, aggiornato_il) VALUES ('titolare@esempio.it', 'titolare', ?, '2026-10-30T09:00:00.000000Z', '2026-10-30T09:00:00.000000Z')`, accounts.Hash("password-sintetica-di-prova")); err != nil {
		t.Fatal(err)
	}
	_ = d.Chiudi()
	for _, u := range []string{"", "http://negozio.esempio.ts.net:8443", "https://", "https://h/percorso", "https://h?x=1", "https://u@h", "negozio:8443", "https://h#x",
		"https://h:99999", "https://h:", "https://h:0", "https://h;x", "https://-h", "https://h_x.it", "https://h..it", "https://h:8443:1"} {
		out.Reset()
		if err := resetLink(context.Background(), opzioniReset{DB: p, URL: u}, &out); err == nil || out.Len() > 0 {
			t.Errorf("indirizzo %q accettato (%q)", u, out.String())
		}
	}
	out.Reset()
	if err := resetLink(context.Background(), opzioniReset{DB: p, URL: "https://negozio.esempio.ts.net:8443/"}, &out); err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`https://negozio\.esempio\.ts\.net:8443/reset/([A-Za-z0-9_-]{43})\n`).FindStringSubmatch(out.String())
	if m == nil || !strings.Contains(out.String(), "titolare@esempio.it") || !strings.Contains(out.String(), "30 minuti") || !strings.Contains(out.String(), "una sola volta") {
		t.Fatalf("uscita: %q", out.String())
	}
	d, _ = db.Apri(p)
	defer func() { _ = d.Chiudi() }()
	if email, ok, err := accounts.NuovoRecupero(d, clock.Sistema{}).AccountDel(context.Background(), m[1]); !ok || err != nil || email != "titolare@esempio.it" {
		t.Errorf("il link stampato non vale: %v %v", ok, err)
	}
	var righe string
	if err := d.Lettura.QueryRow(`SELECT group_concat(attore || '|' || azione || '|' || dati) FROM eventi_audit`).Scan(&righe); err != nil {
		t.Fatal(err)
	}
	if righe != `console|password_reset.requested|{"channel":"console"}` || strings.Contains(righe, m[1]) {
		t.Errorf("registro: %q", righe)
	}
}

// Il comando non crea un database dove non c'è: un percorso sbagliato è un errore, senza file.
func TestResetLinkSenzaDatabase(t *testing.T) {
	p := filepath.Join(t.TempDir(), "manca.db")
	var out bytes.Buffer
	if err := resetLink(context.Background(), opzioniReset{DB: p, URL: "https://negozio.esempio.ts.net:8443"}, &out); err == nil || out.Len() > 0 {
		t.Fatalf("database mancante: %v %q", err, out.String())
	}
	for _, x := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(p + x); err == nil {
			t.Errorf("creato %s", p+x)
		}
	}
}

// D-04, «solo sul terminale»: il link non si stampa su un file o una pipe (un journal, un tee).
func TestResetLinkSoloSulTerminale(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close(); _ = w.Close() }()
	if terminale(w) {
		t.Error("una pipe vale come terminale")
	}
	f, err := os.CreateTemp(t.TempDir(), "uscita")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if terminale(f) {
		t.Error("un file vale come terminale")
	}
}
