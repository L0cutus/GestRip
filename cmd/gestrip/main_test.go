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
	"github.com/L0cutus/gestrip/internal/apparecchio"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/web"
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
// del titolare, sull'indirizzo della faccia interna dell'apparecchio (011b); vale 30 minuti, una
// volta, e lascia una riga nel registro delle azioni senza il token. Il comando avvisa che una
// nuova richiesta dalla pagina annulla il link (come lo standalone). Un database senza titolare è
// un errore senza link.
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
	conf := configurazione(t, "indirizzo_interno = https://negozio.esempio.ts.net:8443\n")
	var out bytes.Buffer
	if err := resetLink(context.Background(), opzioniReset{DB: p, Config: conf}, &out); !errors.Is(err, accounts.ErrNessunTitolare) || out.Len() > 0 {
		t.Fatalf("senza titolare: %v %q", err, out.String())
	}
	d, _ = db.Apri(p)
	if _, err := d.Scrittura.Exec(`INSERT INTO utenti (email, ruolo, hash_password, creato_il, aggiornato_il) VALUES ('titolare@esempio.it', 'titolare', ?, '2026-10-30T09:00:00.000000Z', '2026-10-30T09:00:00.000000Z')`, accounts.Hash("password-sintetica-di-prova")); err != nil {
		t.Fatal(err)
	}
	_ = d.Chiudi()
	out.Reset()
	if err := resetLink(context.Background(), opzioniReset{DB: p, Config: conf}, &out); err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^https://negozio\.esempio\.ts\.net:8443/reset/([A-Za-z0-9_-]{43})$`).FindStringSubmatch(out.String())
	if m == nil || !strings.Contains(out.String(), "titolare@esempio.it") || !strings.Contains(out.String(), "30 minuti") || !strings.Contains(out.String(), "una sola volta") {
		t.Fatalf("uscita: %q", out.String())
	}
	if !strings.Contains(out.String(), "Attenzione: una nuova richiesta dalla pagina «Password dimenticata?» annulla questo link.") {
		t.Errorf("manca l'avviso: %q", out.String())
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

// configurazione scrive un file dell'apparecchio sintetico e ne dà il percorso.
func configurazione(t *testing.T, testo string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "apparecchio.conf")
	if err := os.WriteFile(p, []byte(testo), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// inventario: ACC-13
// -url sostituisce l'indirizzo del file dell'apparecchio solo per quel link, con le stesse regole
// di forma; senza file (o senza la chiave) e senza -url il comando dice come configurarlo e non
// crea nessun link; un file rovinato non diventa mai un link.
func TestResetLinkIndirizzo(t *testing.T) {
	p := filepath.Join(t.TempDir(), "gestrip.db")
	d, err := db.Apri(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Migra(context.Background(), clock.Sistema{}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Scrittura.Exec(`INSERT INTO utenti (email, ruolo, hash_password, creato_il, aggiornato_il) VALUES ('titolare@esempio.it', 'titolare', ?, '2026-10-30T09:00:00.000000Z', '2026-10-30T09:00:00.000000Z')`, accounts.Hash("password-sintetica-di-prova")); err != nil {
		t.Fatal(err)
	}
	_ = d.Chiudi()
	link := func() int {
		d, _ := db.Apri(p)
		defer func() { _ = d.Chiudi() }()
		var n int
		if err := d.Lettura.QueryRow(`SELECT count(*) FROM reset_password`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	manca := filepath.Join(t.TempDir(), "apparecchio.conf")
	var out bytes.Buffer
	err = resetLink(context.Background(), opzioniReset{DB: p, Config: manca}, &out)
	if err == nil || !strings.Contains(err.Error(), "indirizzo_interno = https://") || !strings.Contains(err.Error(), manca) ||
		!strings.Contains(err.Error(), "-url") || out.Len() > 0 || link() != 0 {
		t.Fatalf("senza indirizzo: %v %q", err, out.String())
	}
	for _, testo := range []string{"indirizzo_interno = https://h/percorso\n", "indirizzo_interno = https://h\naltro = 1\n", "indirizzo_interno https://h\n"} {
		out.Reset()
		if err := resetLink(context.Background(), opzioniReset{DB: p, Config: configurazione(t, testo)}, &out); err == nil || out.Len() > 0 {
			t.Errorf("file %q accettato (%q)", testo, out.String())
		}
	}
	conf := configurazione(t, "indirizzo_interno = https://negozio.esempio.ts.net:8443\n")
	for _, u := range []string{"http://negozio.esempio.ts.net:8443", "https://h/percorso", "https://h:0", "https://h?x=1"} {
		out.Reset()
		if err := resetLink(context.Background(), opzioniReset{DB: p, Config: conf, URL: u}, &out); err == nil || out.Len() > 0 {
			t.Errorf("indirizzo %q accettato (%q)", u, out.String())
		}
	}
	if link() != 0 {
		t.Error("un indirizzo rifiutato ha creato un link")
	}
	out.Reset()
	if err := resetLink(context.Background(), opzioniReset{DB: p, Config: conf, URL: "https://altro.esempio.ts.net:9443/"}, &out); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^https://altro\.esempio\.ts\.net:9443/reset/[A-Za-z0-9_-]{43}$`).MatchString(out.String()) || strings.Contains(out.String(), "negozio.esempio") {
		t.Errorf("-url non sostituisce: %q", out.String())
	}
	// -url scavalca anche un file rovinato o mancante.
	out.Reset()
	if err := resetLink(context.Background(), opzioniReset{DB: p, Config: configurazione(t, "rovinato\n"), URL: "https://altro.esempio.ts.net"}, &out); err != nil ||
		!strings.Contains(out.String(), "https://altro.esempio.ts.net/reset/") {
		t.Errorf("-url con file rovinato: %v %q", err, out.String())
	}
}

// «gestrip serve» legge il file dell'apparecchio prima di toccare il database: uno rovinato ferma
// l'avvio, uno che manca no (l'indirizzo resta non configurato).
func TestServeConfigurazione(t *testing.T) {
	avvia := func(percorso, conf string) error {
		ctx, ferma := context.WithCancel(context.Background())
		defer ferma()
		finito := make(chan error, 1)
		pronto := make(chan struct{})
		go func() {
			finito <- serve(ctx, opzioniServe{DB: percorso, Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0", Config: conf},
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
	if err := avvia(filepath.Join(t.TempDir(), "a.db"), filepath.Join(t.TempDir(), "manca.conf")); err != nil {
		t.Errorf("senza file: %v", err)
	}
	if err := avvia(filepath.Join(t.TempDir(), "b.db"), configurazione(t, "indirizzo_interno = https://negozio.esempio.ts.net:8443\n")); err != nil {
		t.Errorf("con il file: %v", err)
	}
	nuovo := filepath.Join(t.TempDir(), "c.db")
	err := avvia(nuovo, configurazione(t, "indirizzo_interno = http://negozio.esempio.ts.net\n"))
	if err == nil || !strings.Contains(err.Error(), "configurazione dell'apparecchio") {
		t.Errorf("file rovinato: %v", err)
	}
	if _, e := os.Stat(nuovo); e == nil {
		t.Error("con un file rovinato ha toccato il database")
	}
}

// Le pagine ricevono l'indirizzo interno e il percorso del file che citano quando manca (D-32), e
// il registro da consultare (ADM-16).
func TestDipendenzeWeb(t *testing.T) {
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	dip := dipendenzeWeb(d, make([]byte, 32), apparecchio.Configurazione{IndirizzoInterno: "https://negozio.esempio.ts.net"},
		opzioniServe{Config: "/prova/apparecchio.conf"})
	if dip.IndirizzoInterno != "https://negozio.esempio.ts.net" || dip.Configurazione != "/prova/apparecchio.conf" || dip.Consultazione == nil ||
		dip.Consultazione != dip.Audit.(web.Consultazione) {
		t.Errorf("dipendenze: %q %q", dip.IndirizzoInterno, dip.Configurazione)
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

// databaseConTitolare: un database migrato con il titolare inventato.
func databaseConTitolare(t *testing.T) string {
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
	if _, err := d.Scrittura.Exec(`INSERT INTO utenti (email, ruolo, hash_password, creato_il, aggiornato_il) VALUES ('titolare@esempio.it', 'titolare', ?, '2026-10-30T09:00:00.000000Z', '2026-10-30T09:00:00.000000Z')`, accounts.Hash("password-sintetica-di-prova")); err != nil {
		t.Fatal(err)
	}
	return p
}

func righeDi(t *testing.T, p, q string) string {
	t.Helper()
	d, err := db.Apri(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Chiudi() }()
	var s sql.NullString
	if err := d.Lettura.QueryRow(q).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s.String
}

var reInvitoStampato = regexp.MustCompile(`(?m)^https://negozio\.esempio\.ts\.net:8443/invito/([A-Za-z0-9_-]{43})$`)

// inventario: ACC-07, ACC-08
// D-32: «gestrip invito» invita un account nuovo, o dà un nuovo link a uno in attesa, e stampa il
// link sul terminale sull'indirizzo interno dell'apparecchio; vale 15 minuti, una volta; riga nel
// registro con attore «console», senza il token. Avvisa che un nuovo link dalla pagina utenti lo
// annulla.
func TestInvitoDallaConsole(t *testing.T) {
	p := databaseConTitolare(t)
	conf := configurazione(t, "indirizzo_interno = https://negozio.esempio.ts.net:8443\n")
	var out bytes.Buffer
	if err := invito(context.Background(), opzioniInvito{DB: p, Config: conf, Email: " Nuovo@Esempio.it", Nome: "Nuovo"}, &out); err != nil {
		t.Fatal(err)
	}
	m := reInvitoStampato.FindStringSubmatch(out.String())
	for _, s := range []string{"Account nuovo@esempio.it aggiunto come dipendente.", "15 minuti", "una sola volta",
		"Attenzione: un nuovo link dalla pagina utenti annulla questo link."} {
		if m == nil || !strings.Contains(out.String(), s) {
			t.Fatalf("uscita senza %q: %q", s, out.String())
		}
	}
	if got := righeDi(t, p, `SELECT group_concat(attore || '|' || azione || '|' || dati, ';') FROM eventi_audit`); got != `console|user.invited|{"role":"dipendente"}` || strings.Contains(got, m[1]) {
		t.Errorf("registro: %q", got)
	}
	if got := righeDi(t, p, `SELECT nome_visibile || '|' || ruolo FROM utenti WHERE email = 'nuovo@esempio.it'`); got != "Nuovo|dipendente" {
		t.Errorf("account: %q", got)
	}
	// Di nuovo, per lo stesso account in attesa: nuovo link, il vecchio non vale più.
	out.Reset()
	if err := invito(context.Background(), opzioniInvito{DB: p, Config: conf, Email: "nuovo@esempio.it"}, &out); err != nil {
		t.Fatal(err)
	}
	m2 := reInvitoStampato.FindStringSubmatch(out.String())
	if m2 == nil || m2[1] == m[1] || !strings.Contains(out.String(), "Nuovo link d'invito per nuovo@esempio.it: il precedente non vale più.") {
		t.Fatalf("nuovo link: %q", out.String())
	}
	if got := righeDi(t, p, `SELECT attore || '|' || azione FROM eventi_audit ORDER BY id DESC LIMIT 1`); got != "console|user.invitation_reissued" {
		t.Errorf("registro: %q", got)
	}
	d, _ := db.Apri(p)
	inv := accounts.NuoviInviti(d, clock.Sistema{})
	if err := inv.Accetta(context.Background(), m[1], "una password lunga"); err == nil {
		t.Error("il primo link vale ancora")
	}
	if err := inv.Accetta(context.Background(), m2[1], "una password lunga"); err != nil {
		t.Errorf("il link stampato non vale: %v", err)
	}
	_ = d.Chiudi()
	// Un titolare invitato dalla console.
	out.Reset()
	if err := invito(context.Background(), opzioniInvito{DB: p, Config: conf, Email: "socio@esempio.it", Ruolo: "titolare"}, &out); err != nil ||
		!strings.Contains(out.String(), "aggiunto come titolare.") {
		t.Errorf("titolare: %v %q", err, out.String())
	}
	// -url sostituisce l'indirizzo solo per quel link.
	out.Reset()
	if err := invito(context.Background(), opzioniInvito{DB: p, Config: conf, Email: "terzo@esempio.it", URL: "https://altro.esempio.ts.net"}, &out); err != nil ||
		!strings.Contains(out.String(), "\nhttps://altro.esempio.ts.net/invito/") {
		t.Errorf("-url: %v %q", err, out.String())
	}
}

// inventario: ACC-07, ACC-08
// I rifiuti del comando non stampano nessun link e non scrivono niente: account con la password
// (anche dopo un invito accettato), email o ruolo sbagliati, ruolo o nome per un account che
// esiste già, nessun indirizzo, -url sbagliato, database che manca (che non si crea).
func TestInvitoDallaConsoleRifiuti(t *testing.T) {
	p := databaseConTitolare(t)
	conf := configurazione(t, "indirizzo_interno = https://negozio.esempio.ts.net:8443\n")
	var out bytes.Buffer
	if err := invito(context.Background(), opzioniInvito{DB: p, Config: conf, Email: "atteso@esempio.it"}, &out); err != nil {
		t.Fatal(err)
	}
	prima := righeDi(t, p, `SELECT (SELECT count(*) FROM utenti) || '|' || (SELECT count(*) FROM inviti_utenti) || '|' || (SELECT count(*) FROM eventi_audit) || '|' || (SELECT count(*) FROM inviti_utenti WHERE revocato_il IS NOT NULL)`)
	for _, c := range []struct {
		o   opzioniInvito
		msg string
	}{
		{opzioniInvito{Email: "titolare@esempio.it"}, "ha già una password"},
		{opzioniInvito{Email: "non-un-indirizzo"}, "email"},
		{opzioniInvito{Email: ""}, "email"},
		{opzioniInvito{Email: "ok@esempio.it", Ruolo: "owner"}, "ruolo"},
		{opzioniInvito{Email: "ok@esempio.it", Nome: strings.Repeat("n", 121)}, "nome"},
		{opzioniInvito{Email: "atteso@esempio.it", Ruolo: "titolare"}, "esiste già"},
		{opzioniInvito{Email: "atteso@esempio.it", Nome: "Gino"}, "esiste già"},
		{opzioniInvito{Email: "ok@esempio.it", URL: "http://negozio.esempio.ts.net"}, "-url"},
	} {
		c.o.DB, c.o.Config = p, conf
		out.Reset()
		err := invito(context.Background(), c.o, &out)
		if err == nil || !strings.Contains(err.Error(), c.msg) || out.Len() > 0 {
			t.Errorf("%+v: %v %q", c.o, err, out.String())
		}
	}
	out.Reset()
	manca := filepath.Join(t.TempDir(), "manca.conf")
	if err := invito(context.Background(), opzioniInvito{DB: p, Config: manca, Email: "ok@esempio.it"}, &out); err == nil ||
		!strings.Contains(err.Error(), "indirizzo_interno = https://") || out.Len() > 0 {
		t.Errorf("senza indirizzo: %v %q", err, out.String())
	}
	if got := righeDi(t, p, `SELECT (SELECT count(*) FROM utenti) || '|' || (SELECT count(*) FROM inviti_utenti) || '|' || (SELECT count(*) FROM eventi_audit) || '|' || (SELECT count(*) FROM inviti_utenti WHERE revocato_il IS NOT NULL)`); got != prima {
		t.Errorf("i rifiuti hanno scritto: %s → %s", prima, got)
	}
	q := filepath.Join(t.TempDir(), "manca.db")
	if err := invito(context.Background(), opzioniInvito{DB: q, Config: conf, Email: "ok@esempio.it"}, &out); err == nil || out.Len() > 0 {
		t.Errorf("database mancante: %v", err)
	}
	if _, err := os.Stat(q); err == nil {
		t.Error("creato il database")
	}
}

// Un argomento in più dopo le opzioni è un errore, non si ignora (un -ruolo scritto male finirebbe
// lì senza che nessuno lo dica).
func TestArgomentiInPiu(t *testing.T) {
	if err := argomentiFiniti([]string{}); err != nil {
		t.Error(err)
	}
	if err := argomentiFiniti([]string{"extra"}); err == nil || !strings.Contains(err.Error(), "extra") {
		t.Errorf("argomento in più: %v", err)
	}
}
