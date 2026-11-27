package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/backup"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
	"github.com/L0cutus/gestrip/internal/web"

	"golang.org/x/sys/unix"
)

// Il ripristino isolato (slice 130): gestrip restore su un archivio inventato, poi un serve di prova sulla copia,
// avviato due volte. Tutto inventato: bot, chat, server SFTP e chiavetta non esistono; il Bot API è un httptest
// che conta ogni richiesta.

// archivioConCanaliAccesi fa una cartella dei dati inventata con email e Telegram configurati e accesi, un messaggio
// Telegram in coda, la copia remota configurata e una riga nel registro dei backup; ne fa un backup con gestrip
// backup -cartella e dà il database, l'archivio e il file della chiave.
func archivioConCanaliAccesi(t *testing.T) (string, string, string) {
	t.Helper()
	p := databaseConChiave(t)
	ora := "2026-01-01T09:00:00.000000Z"
	conDatabase(t, p, func(d *db.DB) {
		for _, q := range []string{
			`INSERT INTO segreti_provider (nome, valore, aggiornato_il) VALUES ('outgoing.telegram.bot_token', '42:AAH-finto', '` + ora + `')`,
			`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.telegram.chat_id', '-1001', '` + ora + `'), ('` +
				uscita.ChiaveTelegramAcceso + `', '1', '` + ora + `'), ('` + uscita.ChiaveNotificheClienti + `', '1', '` + ora + `'), ('` +
				uscita.ChiaveEmailAcceso + `', '1', '` + ora + `'), ('` + backup.ChiaveRemoto + `', '{"host":"sftp.example.invalid"}', '` + ora + `')`,
			`INSERT INTO messaggi_in_uscita (canale, destinatario, indirizzo, testo, stato, tentativi, prossimo_tentativo_il, accodato_il)
				VALUES ('telegram', 'shop', '-1001', 'testo di prova', 'queued', 0, '` + ora + `', '` + ora + `')`,
		} {
			if _, err := d.Scrittura.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	})
	dest := t.TempDir()
	var out bytes.Buffer
	if err := faiBackup(context.Background(), opzioniBackup{DB: p, Cartella: dest}, &out); err != nil {
		t.Fatal(err)
	}
	archivi, _ := filepath.Glob(filepath.Join(dest, "gestrip-backup-*.tar.age"))
	if len(archivi) != 1 {
		t.Fatalf("archivi: %v", archivi)
	}
	return p, archivi[0], backup.FileChiave(filepath.Dir(p))
}

// Voce OPS-18 (il segno sta su TestRipristinoInCartellaNuova, internal/backup).
// gestrip restore: con la chiave da file o incollata sullo standard input ripristina in una cartella nuova e dice,
// in parole semplici, che la copia è isolata e cosa resta spento; senza archivio o cartella è un errore d'uso; con
// la cartella piena rifiuta e non tocca niente; mai la chiave nell'uscita.
func TestRestoreComando(t *testing.T) {
	_, archivio, chiave := archivioConCanaliAccesi(t)
	padre := t.TempDir()
	dest := filepath.Join(padre, "dati")
	var out bytes.Buffer
	if err := ripristina(context.Background(), opzioniRipristino{Archivio: archivio, Chiave: chiave, Cartella: dest}, &out, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`\Aarchivio: ` + regexp.QuoteMeta(archivio) + `\nbyte: \d+\nsha256: [0-9a-f]{64}\ncatturato il: .+\nversione: .+\nmedia: 0\n` +
		`verificato: decifrato, \d+ file con l'impronta giusta, database integro, \d+ tabelle con i conteggi giusti\n` +
		`ripristinato in ` + regexp.QuoteMeta(dest) + ` \(database ` + regexp.QuoteMeta(filepath.Join(dest, "gestrip.db")) + `\)\n` +
		`COPIA ISOLATA: .*in manutenzione.*\n` +
		`spenti dal ripristino, da riaccendere alla conferma: email, Telegram, copia remota\n` +
		`registro dei backup svuotato: .*\n` +
		`avvio: gestrip serve -db ` + regexp.QuoteMeta(filepath.Join(dest, "gestrip.db")) + `\n\z`)
	if !re.MatchString(out.String()) {
		t.Errorf("uscita:\n%s", out.String())
	}
	// La chiave incollata (la riga del foglio) sullo standard input, con la richiesta sul terminale.
	b, err := os.ReadFile(chiave)
	if err != nil {
		t.Fatal(err)
	}
	riga := regexp.MustCompile(`AGE-SECRET-KEY-[0-9A-Z]+`).Find(b)
	var richiesta bytes.Buffer
	seconda := filepath.Join(padre, "seconda")
	out.Reset()
	if err := ripristina(context.Background(), opzioniRipristino{Archivio: archivio, Cartella: seconda},
		&out, bytes.NewReader(append(riga, '\n')), &richiesta); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(richiesta.String(), "chiave dei backup") || len(riga) == 0 || bytes.Contains(out.Bytes(), riga) ||
		strings.Contains(richiesta.String(), string(riga)) {
		t.Errorf("richiesta %q, uscita %q", richiesta.String(), out.String())
	}
	// Sopra la cartella appena ripristinata: rifiutato, la cartella resta com'è.
	err = ripristina(context.Background(), opzioniRipristino{Archivio: archivio, Chiave: chiave, Cartella: dest}, &out, nil, io.Discard)
	if !errors.Is(err, backup.ErrCartellaNonVuota) || !strings.Contains(err.Error(), "RIPRISTINO NON RIUSCITO") {
		t.Errorf("sopra i dati: %v", err)
	}
	for _, args := range [][]string{{"-archivio", archivio}, {"-cartella", dest}, {"-archivio", archivio, "-cartella", dest, "altro"}} {
		if _, err := opzioniDiRipristino(args); err == nil {
			t.Errorf("opzioni %q accettate", args)
		}
	}
}

// serveIsolato avvia serve sul database dato con la chiavetta del file dell'apparecchio configurata, ogni lavoro
// iniettabile che segna se parte e TELEGRAM_API_ENDPOINT su bot; dà gli indirizzi, il backup notturno, le
// dipendenze della faccia interna, gli avvisi e la funzione che ferma serve.
type avvioIsolato struct {
	interna, pubblica string
	notturno          *backup.Notturno
	dip               web.Dipendenze
	avvisi            *bytes.Buffer
	ferma             func() error
}

func serveIsolato(t *testing.T, percorso string, partiti *sync.Map) avvioIsolato {
	t.Helper()
	return serveIsolatoCon(t, percorso, partiti, nil)
}

// serveIsolatoCon è serveIsolato con le opzioni cambiate da cambia, se c'è (slice 133: l'orologio e l'attesa dei
// lavori automatici).
func serveIsolatoCon(t *testing.T, percorso string, partiti *sync.Map, cambia func(*opzioniServe)) avvioIsolato {
	t.Helper()
	config := filepath.Join(t.TempDir(), "apparecchio.conf")
	if err := os.WriteFile(config, []byte("backup_chiavetta = /mnt/chiavetta-inventata\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	blocca := func(nome string) func(context.Context) bool {
		return func(ctx context.Context) bool { partiti.Store(nome, true); <-ctx.Done(); return false }
	}
	bloccaD := func(nome string) func(context.Context, time.Duration) bool {
		return func(ctx context.Context, _ time.Duration) bool { partiti.Store(nome, true); <-ctx.Done(); return false }
	}
	a := avvioIsolato{avvisi: &bytes.Buffer{}}
	o := opzioniServe{SenzaVolumeCifrato: true, DB: percorso, Config: config, Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0", avvisi: a.avvisi,
		aspettaConservazione: func(ctx context.Context) bool { <-ctx.Done(); return false },
		aspettaPulizia:       func(ctx context.Context) bool { <-ctx.Done(); return false },
		aspettaLavori:        blocca("lavori automatici"), aspettaBackup: bloccaD("backup notturno"), aspettaAvvisi: bloccaD("avvisi dei backup"),
		conBackup: func(n *backup.Notturno) { a.notturno = n }, conDipendenze: func(d web.Dipendenze) { a.dip = d }}
	if cambia != nil {
		cambia(&o)
	}
	ctx, ferma := context.WithCancel(context.Background())
	pronto := make(chan [2]net.Addr, 1)
	finito := make(chan error, 1)
	go func() { finito <- serve(ctx, o, func(i, p net.Addr) { pronto <- [2]net.Addr{i, p} }) }()
	select {
	case ind := <-pronto:
		a.interna, a.pubblica = "http://"+ind[0].String(), "http://"+ind[1].String()
	case err := <-finito:
		ferma()
		t.Fatal(err)
	case <-time.After(30 * time.Second):
		ferma()
		t.Fatal("serve non parte")
	}
	a.ferma = func() error { ferma(); return <-finito }
	return a
}

func richiesta(t *testing.T, metodo, url string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(metodo, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	b, _ := io.ReadAll(r.Body)
	return r.StatusCode, string(b)
}

// Voce OPS-19 (il segno sta su TestConfermaRipristinoFinoAlServeNormale, con la conferma della 131).
// La copia ripristinata parte isolata e resta isolata dopo un riavvio: con il bot configurato e acceso nel backup e
// un messaggio in coda, il Bot API finto non riceve niente (né invii né getUpdates: incidente del 27/10); né la coda
// in uscita né il lettore Telegram aspettano un giro; pianificatore, backup notturno e avvisi dei backup non partono;
// «Esegui adesso» del backup dice non configurato anche con la chiavetta nel file dell'apparecchio; la copia remota
// non c'è nella pagina; la faccia pubblica risponde 503 «in manutenzione» su /r/…, /healthz resta; la faccia
// interna funziona. L'ora del backup (133b) si salva e non fa partire niente. Lo stato isolato resta nel database.
func TestServeCopiaIsolata(t *testing.T) {
	_, archivio, chiave := archivioConCanaliAccesi(t)
	var bot sync.Map
	contaBot := 0
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		contaBot++
		mu.Unlock()
		bot.Store(r.URL.Path, true)
		_, _ = io.WriteString(w, `{"ok":true,"result":[]}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("TELEGRAM_API_ENDPOINT", srv.URL+"/")
	vera := aspettaPer
	t.Cleanup(func() { aspettaPer = vera })
	var partiti sync.Map
	aspettaPer = func(ctx context.Context, d time.Duration) bool {
		partiti.Store("attesa della coda o del lettore Telegram: "+d.String(), true)
		<-ctx.Done()
		return false
	}
	dest := filepath.Join(t.TempDir(), "dati")
	if err := ripristina(context.Background(), opzioniRipristino{Archivio: archivio, Chiave: chiave, Cartella: dest}, io.Discard, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	percorso := filepath.Join(dest, "gestrip.db")
	// Gli interruttori riaccesi a mano nella copia (dalla pagina, o da un programma vecchio): l'isolamento di serve
	// deve tenerli spenti lo stesso, non solo il ripristino che li ha tolti.
	conDatabase(t, percorso, func(d *db.DB) {
		if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni VALUES (?, '1', '2026-11-26T10:00:00.000000Z'), (?, '1', '2026-11-26T10:00:00.000000Z')`,
			uscita.ChiaveTelegramAcceso, uscita.ChiaveEmailAcceso); err != nil {
			t.Fatal(err)
		}
	})
	for giro := 1; giro <= 2; giro++ {
		a := serveIsolato(t, percorso, &partiti)
		if !strings.Contains(a.avvisi.String(), "COPIA ISOLATA") {
			t.Errorf("giro %d: avvisi %q", giro, a.avvisi.String())
		}
		for _, p := range []string{"/r/TOKENINVENTATO", "/r/TOKENINVENTATO/telegram", "/"} {
			if c, corpo := richiesta(t, "GET", a.pubblica+p); c != http.StatusServiceUnavailable || !strings.Contains(corpo, "manutenzione") {
				t.Errorf("giro %d: pubblica %s: %d %q", giro, p, c, corpo)
			}
		}
		if c, _ := richiesta(t, "GET", a.pubblica+"/healthz"); c != http.StatusOK {
			t.Errorf("giro %d: /healthz %d", giro, c)
		}
		if c, corpo := richiesta(t, "GET", a.interna+"/login"); c != http.StatusOK || strings.Contains(corpo, "manutenzione") {
			t.Errorf("giro %d: interna /login %d", giro, c)
		}
		if a.notturno == nil || a.notturno.Configurato() || !errors.Is(a.notturno.EseguiAdesso(), backup.ErrNonConfigurato) {
			t.Errorf("giro %d: backup sulla chiavetta configurato nella copia isolata", giro)
		}
		// Il banner dei backup del titolare (Stato.Allarme, quello che disegna allarmeBackup): nella copia isolata
		// nessun backup automatico è atteso, quindi né «non configurato» né «backup vecchio» né «prova mancante».
		if st, err := a.notturno.Stato(context.Background()); err != nil {
			t.Errorf("giro %d: stato dei backup: %v", giro, err)
		} else if al := st.Allarme(); len(al) != 0 {
			t.Errorf("giro %d: banner dei backup nella copia isolata: %+v", giro, al)
		}
		if a.dip.CopiaRemota != nil {
			t.Errorf("giro %d: copia remota nella pagina della copia isolata", giro)
		}
		// L'ora del backup dalla pagina (slice 133b): si salva, resta dopo il riavvio e non fa partire niente.
		if giro == 1 {
			if es, err := a.notturno.SalvaOra(context.Background(), "23:00", audit.Contesto{Attore: "titolare@esempio.it"}); err != nil || !es.Cambiata || es.Acceso {
				t.Errorf("ora del backup nella copia isolata: %+v %v", es, err)
			}
		} else if o, err := a.notturno.Ora(context.Background()); err != nil || backup.TestoOra(o) != "23:00" {
			t.Errorf("giro %d: ora del backup dopo il riavvio: %v %v", giro, o, err)
		}
		if dovuto, err := a.notturno.Dovuto(context.Background()); dovuto || err != nil {
			t.Errorf("giro %d: backup dovuto nella copia isolata: %v %v", giro, dovuto, err)
		}
		// Un po' di tempo per i lavori che partirebbero subito (la coda fa il primo giro all'avvio, il lettore la
		// prima getUpdates): senza isolamento qui il Bot API riceverebbe il messaggio in coda (TestServeTelegram).
		time.Sleep(500 * time.Millisecond)
		if err := a.ferma(); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	n := contaBot
	mu.Unlock()
	if n != 0 {
		var percorsi []string
		bot.Range(func(k, _ any) bool { percorsi = append(percorsi, k.(string)); return true })
		t.Errorf("il Bot API ha ricevuto %d richieste: %v", n, percorsi)
	}
	partiti.Range(func(k, _ any) bool { t.Errorf("partito nella copia isolata: %v", k); return true })
	conDatabase(t, percorso, func(d *db.DB) {
		if _, ok, err := db.LeggiIsolamento(context.Background(), d.Lettura); !ok || err != nil {
			t.Errorf("stato isolato perso: %v %v", ok, err)
		}
		var esecuzioni int
		if err := d.Lettura.QueryRow(`SELECT count(*) FROM esecuzioni_backup`).Scan(&esecuzioni); err != nil || esecuzioni != 0 {
			t.Errorf("backup partiti nella copia isolata: %d %v", esecuzioni, err)
		}
		var stato string
		if err := d.Lettura.QueryRow(`SELECT stato FROM messaggi_in_uscita`).Scan(&stato); err != nil || stato != "queued" {
			t.Errorf("messaggio in coda: %q %v", stato, err)
		}
	})
}

// Uno stato isolato rovinato ferma serve prima di aprire le facce (fallisce chiuso).
func TestServeIsolamentoRovinato(t *testing.T) {
	percorso := databaseVuoto(t)
	conDatabase(t, percorso, func(d *db.DB) {
		if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni VALUES (?, 'rovinato', '2026-11-26T10:00:00.000000Z')`, db.ChiaveIsolamento); err != nil {
			t.Fatal(err)
		}
	})
	o := opzioniServe{SenzaVolumeCifrato: true, DB: percorso, Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0", avvisi: io.Discard}
	aperte := false
	ctx, ferma := context.WithTimeout(context.Background(), 20*time.Second) // senza il controllo serve resterebbe su
	defer ferma()
	err := serve(ctx, o, func(net.Addr, net.Addr) { aperte = true })
	if !errors.Is(err, db.ErrIsolamento) || aperte {
		t.Errorf("errore %v, facce aperte %v", err, aperte)
	}
}

// I comandi da console che scrivono archivi, toccano la copia remota o scrivono prove rifiutano la copia isolata
// (revisione della 130): backup -cartella e -chiavetta (ruoterebbe gli archivi della chiavetta del posto dove gira la
// copia), backup-remoto (anche -togli), prova-backup -chiavetta e -remota. Niente archivi, niente righe.
// verifica-backup resta: non apre il database e non scrive nel registro.
func TestComandiSullaCopiaIsolata(t *testing.T) {
	_, archivio, chiave := archivioConCanaliAccesi(t)
	dest := filepath.Join(t.TempDir(), "dati")
	if err := ripristina(context.Background(), opzioniRipristino{Archivio: archivio, Chiave: chiave, Cartella: dest}, io.Discard, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dest, "gestrip.db")
	cartella, chiavetta := t.TempDir(), t.TempDir()
	ctx := context.Background()
	var out bytes.Buffer
	for nome, f := range map[string]func() error{
		"backup -cartella":  func() error { return faiBackup(ctx, opzioniBackup{DB: p, Cartella: cartella}, &out) },
		"backup -chiavetta": func() error { return faiBackup(ctx, opzioniBackup{DB: p, Chiavetta: chiavetta}, &out) },
		"backup-remoto": func() error {
			return backupRemoto(ctx, opzioniBackupRemoto{DB: p, Host: "sftp.example.invalid", Porta: 22, Utente: "u", Cartella: "/c"}, &out, orologioRipristino)
		},
		"backup-remoto -togli": func() error {
			return backupRemoto(ctx, opzioniBackupRemoto{DB: p, Togli: true}, &out, orologioRipristino)
		},
		"prova-backup -chiavetta": func() error { return provaBackup(ctx, opzioniProvaBackup{DB: p, Chiavetta: chiavetta}, &out) },
		"prova-backup -remota":    func() error { return provaBackup(ctx, opzioniProvaBackup{DB: p, Remota: true}, &out) },
	} {
		if err := f(); !errors.Is(err, errCopiaIsolata) || !strings.Contains(err.Error(), "copia isolata") {
			t.Errorf("%s: %v", nome, err)
		}
	}
	for _, c := range []string{cartella, chiavetta} {
		if v, _ := os.ReadDir(c); len(v) != 0 {
			t.Errorf("scritto in %s: %v", c, v)
		}
	}
	conDatabase(t, p, func(d *db.DB) {
		for _, tab := range []string{"verifiche_backup", "esecuzioni_backup", "prove_ripristino", "copie_remote", "prove_remote"} {
			var n int
			if err := d.Lettura.QueryRow(`SELECT count(*) FROM ` + tab).Scan(&n); err != nil || n != 0 {
				t.Errorf("%s: %d righe %v", tab, n, err)
			}
		}
	})
	// verifica-backup sulla copia: sola lettura, passa.
	if err := verificaBackup(ctx, opzioniVerificaBackup{DB: p, Archivio: archivio}, &out); err != nil {
		t.Errorf("verifica-backup: %v", err)
	}
}

// ecoAcceso legge dal terminale f se l'eco è acceso.
func ecoAcceso(t *testing.T, f *os.File) bool {
	t.Helper()
	tio, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	return tio.Lflag&unix.ECHO != 0
}

// aspettaEco aspetta (al più 10 s) che l'eco del terminale sia quello voluto.
func aspettaEco(t *testing.T, f *os.File, acceso bool) {
	t.Helper()
	for range 1000 {
		if ecoAcceso(t, f) == acceso {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("eco acceso %v atteso", !acceso)
}

// La chiave incollata su un terminale vero (pseudo-terminale): mentre il comando aspetta la riga l'eco è spento,
// dopo è di nuovo acceso; anche quando il comando si ferma (Ctrl-C) mentre aspetta.
func TestRestoreChiaveSenzaEco(t *testing.T) {
	master, slave := pseudoTerminale(t)
	go func() { _, _ = io.Copy(io.Discard, master) }() // quello che il terminale mostrerebbe
	dir := t.TempDir()
	c, err := backup.CreaChiave(dir, time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !ecoAcceso(t, slave) {
		t.Fatal("eco spento già prima")
	}
	o := opzioniRipristino{Archivio: filepath.Join(dir, "manca.tar.age"), Cartella: filepath.Join(dir, "dati")}
	finito := make(chan error, 1)
	go func() { finito <- ripristina(context.Background(), o, io.Discard, slave, io.Discard) }()
	aspettaEco(t, slave, false)
	if _, err := master.WriteString(c.Privata + "\n"); err != nil {
		t.Fatal(err)
	}
	// La chiave è stata letta e accettata: l'errore è dell'archivio che non c'è, non della chiave.
	if err := <-finito; err == nil || !strings.Contains(err.Error(), "manca.tar.age") {
		t.Errorf("errore %v", err)
	}
	if !ecoAcceso(t, slave) {
		t.Error("eco non rimesso dopo la lettura")
	}
	ctx, ferma := context.WithCancel(context.Background())
	go func() { finito <- ripristina(ctx, o, io.Discard, slave, io.Discard) }()
	aspettaEco(t, slave, false)
	ferma()
	if err := <-finito; !errors.Is(err, context.Canceled) {
		t.Errorf("errore dello stop %v", err)
	}
	if !ecoAcceso(t, slave) {
		t.Error("eco non rimesso dopo lo stop")
	}
}

// Un ripristino interrotto lascia .ripristino-* accanto alla destinazione: il comando lo dice con il percorso e non
// lo tocca. Una chiave su un file con permessi larghi (come su una chiavetta FAT) ha un messaggio che dice cosa fare.
func TestRestoreRestiEChiaveConPermessiLarghi(t *testing.T) {
	dir := t.TempDir()
	if _, err := backup.CreaChiave(dir, time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	resto := filepath.Join(dir, ".ripristino-123456")
	if err := os.Mkdir(resto, 0o700); err != nil {
		t.Fatal(err)
	}
	var richiesta bytes.Buffer
	o := opzioniRipristino{Archivio: filepath.Join(dir, "manca.tar.age"), Chiave: backup.FileChiave(dir), Cartella: filepath.Join(dir, "dati")}
	if err := ripristina(context.Background(), o, io.Discard, nil, &richiesta); err == nil {
		t.Fatal("riuscito senza archivio")
	}
	if !strings.Contains(richiesta.String(), resto) || !strings.Contains(richiesta.String(), "a mano") {
		t.Errorf("resto non segnalato: %q", richiesta.String())
	}
	if _, err := os.Stat(resto); err != nil {
		t.Errorf("resto toccato: %v", err)
	}
	if err := os.Chmod(backup.FileChiave(dir), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ripristina(context.Background(), o, io.Discard, nil, io.Discard)
	if !errors.Is(err, backup.ErrChiavePermessi) || !strings.Contains(err.Error(), "chmod 600") || !strings.Contains(err.Error(), "cartella tua") {
		t.Errorf("errore %v", err)
	}
}
