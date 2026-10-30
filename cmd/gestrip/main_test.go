package main

import (
	"context"
	"database/sql"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
