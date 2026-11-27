package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/assist"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/backup"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/firmephoenix"
	"github.com/L0cutus/gestrip/internal/jobs"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// La conferma del ripristino (slice 131, OPS-19, D-12, D-118) dalla console e fino al serve normale. Tutto
// inventato: bot, chat, cliente, indirizzi; il Bot API è un httptest che segna ogni richiesta.

// copiaIsolataDaConfermare fa il backup di archivioConCanaliAccesi (un messaggio Telegram in coda), lo ripristina e
// dà il database della copia.
func copiaIsolataDaConfermare(t *testing.T) string {
	t.Helper()
	_, archivio, chiave := archivioConCanaliAccesi(t)
	dest := filepath.Join(t.TempDir(), "dati")
	if err := ripristina(context.Background(), opzioniRipristino{Archivio: archivio, Chiave: chiave, Cartella: dest}, io.Discard, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dest, "gestrip.db")
}

// Voce OPS-19 (D-118 (b), (e)): gestrip conferma-ripristino chiede di spegnere la macchina vecchia, mostra il
// messaggio in coda e chiede «manda» o «scarta» (Invio = scarta; una risposta non capita si richiede), poi la parola.
// La parola sbagliata o l'input che finisce non cambiano niente; la conferma dice cosa ha riacceso e di riavviare.
// Dopo, backup, backup-remoto e prova-backup non rifiutano più la copia; una seconda conferma non ha niente da fare.
func TestConfermaRipristinoComando(t *testing.T) {
	p := copiaIsolataDaConfermare(t)
	ctx := context.Background()
	for nome, c := range map[string]struct {
		in  string
		err error
	}{
		"parola sbagliata": {"\nNO\n", backup.ErrParolaSbagliata},
		"input finito":     {"\n", errConfermaInterrotta},
		"niente input":     {"", errConfermaInterrotta},
	} {
		var out bytes.Buffer
		if err := confermaRipristino(ctx, opzioniConfermaRipristino{DB: p}, &out, strings.NewReader(c.in)); !errors.Is(err, c.err) {
			t.Errorf("%s: %v, atteso %v", nome, err, c.err)
		}
	}
	conDatabase(t, p, func(d *db.DB) {
		if _, ok, err := db.LeggiIsolamento(ctx, d.Lettura); !ok || err != nil {
			t.Fatalf("non più isolata dopo i rifiuti: %v %v", ok, err)
		}
	})
	vero := orologioConferma
	t.Cleanup(func() { orologioConferma = vero })
	orologioConferma = clock.NuovoFinto(time.Date(2026, 11, 23, 12, 0, 0, 0, clock.Roma)) // lunedì: chiuso, nessun lavoro (D-119)
	var out bytes.Buffer
	if err := confermaRipristino(ctx, opzioniConfermaRipristino{DB: p}, &out, strings.NewReader("forse\n\nconfermo\n")); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"spegni la macchina vecchia", "alla conferma si riaccende: email, Telegram, copia remota", "testo di prova",
		"risposta non capita", "RIPRISTINO CONFERMATO", "riacceso: email, Telegram, copia remota", "0 lasciati in coda", "1 scartati",
		"Lavori di oggi: nessuno segnato come già fatto.", "ora riavvia GestRip"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("manca %q in:\n%s", s, out.String())
		}
	}
	conDatabase(t, p, func(d *db.DB) {
		var n int
		if err := d.Lettura.QueryRow(`SELECT count(*) FROM messaggi_in_uscita WHERE stato = 'queued'`).Scan(&n); err != nil || n != 0 {
			t.Errorf("in coda: %d %v", n, err)
		}
		var attore string
		if err := d.Lettura.QueryRow(`SELECT attore FROM eventi_audit WHERE azione = 'backup.restore_confirmed'`).Scan(&attore); err != nil || attore != "console" {
			t.Errorf("audit: %q %v", attore, err)
		}
	})
	if err := confermaRipristino(ctx, opzioniConfermaRipristino{DB: p}, io.Discard, strings.NewReader("CONFERMO\n")); !errors.Is(err, backup.ErrNonIsolata) {
		t.Errorf("seconda conferma: %v", err)
	}
	// I comandi che la 130 rifiutava sulla copia isolata.
	cartella := t.TempDir()
	if err := faiBackup(ctx, opzioniBackup{DB: p, Cartella: cartella}, io.Discard); err != nil {
		t.Errorf("backup dopo la conferma: %v", err)
	}
	if err := backupRemoto(ctx, opzioniBackupRemoto{DB: p, Togli: true}, io.Discard, orologioRipristino); err != nil {
		t.Errorf("backup-remoto -togli dopo la conferma: %v", err)
	}
	if err := provaBackup(ctx, opzioniProvaBackup{DB: p, Chiavetta: t.TempDir()}, io.Discard); errors.Is(err, errCopiaIsolata) {
		t.Errorf("prova-backup dopo la conferma: %v", err)
	}
	for _, args := range [][]string{{"-db"}, {"-db", p, "altro"}, {"-sconosciuta"}} {
		if _, err := opzioniDiConfermaRipristino(args); err == nil {
			t.Errorf("opzioni %q accettate", args)
		}
	}
	if o, err := opzioniDiConfermaRipristino(nil); err != nil || o.DB != "/var/lib/gestrip/gestrip.db" {
		t.Errorf("predefinite %+v %v", o, err)
	}
}

// Ctrl-C mentre il comando aspetta una risposta: si ferma e niente cambia.
func TestConfermaRipristinoStop(t *testing.T) {
	p := copiaIsolataDaConfermare(t)
	lettura, scrittura := io.Pipe()
	t.Cleanup(func() { _ = scrittura.Close() })
	ctx, ferma := context.WithCancel(context.Background())
	finito := make(chan error, 1)
	go func() { finito <- confermaRipristino(ctx, opzioniConfermaRipristino{DB: p}, io.Discard, lettura) }()
	time.Sleep(200 * time.Millisecond)
	ferma()
	if err := <-finito; !errors.Is(err, context.Canceled) {
		t.Errorf("stop: %v", err)
	}
	conDatabase(t, p, func(d *db.DB) {
		if _, ok, err := db.LeggiIsolamento(context.Background(), d.Lettura); !ok || err != nil {
			t.Errorf("non più isolata: %v %v", ok, err)
		}
	})
}

// firmaStopEmail è il link di disattivazione dell'email (scopo «stop») come lo firma internal/buste con la
// sottochiave email: lo rifà qui perché la firma non è esportata.
func firmaStopEmail(chiaveEmail []byte, busta int64, indirizzo string, adesso time.Time) string {
	b64 := base64.RawURLEncoding
	m := hmac.New(sha256.New, chiaveEmail)
	m.Write([]byte("repair email address:" + indirizzo))
	dati := "stop." + strconv.FormatInt(busta, 10) + "." + b64.EncodeToString(m.Sum(nil)) + "." + strconv.FormatInt(adesso.UnixMilli(), 10)
	m = hmac.New(sha256.New, chiaveEmail)
	m.Write([]byte("gestrip email link\x00" + dati))
	return b64.EncodeToString([]byte(dati)) + "." + b64.EncodeToString(m.Sum(nil))
}

// inventario: OPS-19
// D-12, D-118 (a)-(e). Da capo a fondo: macchina vera con il bot acceso e tre messaggi in coda; link artigiano e di disattivazione
// dell'email firmati prima del backup; ripristino; serve sulla copia isolata (Bot API finto: zero richieste,
// «Esegui adesso» rifiuta, modello remoto spento); conferma dalla console mentre serve gira (ancora zero richieste:
// il serve che gira resta isolato); serve riavviato normale: al Bot API arriva solo il messaggio scelto «manda»,
// i lavori automatici ripartono, le pagine pubbliche rispondono, i link firmati dalla macchina vecchia valgono,
// la chiave della sessione no. D-119 (slice 133): la conferma, venerdì alle 09:30 in negozio, segna come già fatti
// il briefing (08:00, acceso) e i ritardi (09:00); il pianificatore del serve riavviato, alle 09:45, non li rifà (nessuna
// riga, niente in coda per il negozio) e alle 11:00 fa i solleciti (10:00, non ancora dovuti alla conferma), una volta.
func TestConfermaRipristinoFinoAlServeNormale(t *testing.T) {
	ctx := context.Background()
	p := databaseConChiave(t)
	ora := "2026-01-01T09:00:00.000000Z"
	var vecchia []byte
	conDatabase(t, p, func(d *db.DB) {
		for _, q := range []string{
			`INSERT INTO segreti_provider (nome, valore, aggiornato_il) VALUES ('outgoing.telegram.bot_token', '42:AAH-finto', ?1)`,
			`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.telegram.chat_id', '-1001', ?1), ('` +
				uscita.ChiaveTelegramAcceso + `', '1', ?1), ('` + uscita.ChiaveNotificheClienti + `', '1', ?1), ('jobs.briefing_enabled', '1', ?1)`,
			`INSERT INTO clienti (id, nome, telefono, creato_il, aggiornato_il) VALUES (1, 'Cliente Inventato', '3330000000', ?1, ?1)`,
			`INSERT INTO buste (id, cliente_id, nome_cliente, telefono_cliente, numero, data_accettazione, descrizione, hash_token_pubblico,
				token_pubblico_scade_il, creato_il, aggiornato_il) VALUES (1, 1, 'Cliente Inventato', '3330000000', '12/26', '2026-01-01',
				'Anello inventato', randomblob(32), ?1, ?1, ?1)`,
			`INSERT INTO messaggi_in_uscita (id, canale, destinatario, indirizzo, testo, stato, tentativi, prossimo_tentativo_il, accodato_il)
				VALUES (1, 'telegram', 'shop', '-1001', 'primo-da-mandare', 'queued', 0, ?1, ?1),
				(2, 'telegram', 'shop', '-1001', 'secondo-da-scartare', 'queued', 0, ?1, ?1),
				(3, 'telegram', 'shop', '-1001', 'terzo-da-scartare', 'queued', 0, ?1, ?1)`,
		} {
			if _, err := d.Scrittura.Exec(q, ora); err != nil {
				t.Fatal(err)
			}
		}
		var err error
		if vecchia, err = d.ChiaveServer(ctx, clock.Sistema{}); err != nil {
			t.Fatal(err)
		}
	})
	gettone := buste.Nuove(nil, clock.Sistema{}).ConLinkArtigiano(db.SottoChiave(vecchia, "artigiano"), firmephoenix.Segreto{}).GettoneArtigiano(1)
	altroGettone := buste.Nuove(nil, clock.Sistema{}).ConLinkArtigiano(db.SottoChiave(make([]byte, 32), "artigiano"), firmephoenix.Segreto{}).GettoneArtigiano(1)
	stop := firmaStopEmail(db.SottoChiave(vecchia, "email"), 1, "cliente@example.invalid", time.Now())
	if gettone == "" || gettone == altroGettone {
		t.Fatal("gettoni di prova")
	}
	backupDir := t.TempDir()
	if err := faiBackup(ctx, opzioniBackup{DB: p, Cartella: backupDir}, io.Discard); err != nil {
		t.Fatal(err)
	}
	archivi, _ := filepath.Glob(filepath.Join(backupDir, "gestrip-backup-*.tar.age"))
	if len(archivi) != 1 {
		t.Fatalf("archivi %v", archivi)
	}
	dest := filepath.Join(t.TempDir(), "dati")
	if err := ripristina(ctx, opzioniRipristino{Archivio: archivi[0], Chiave: backup.FileChiave(filepath.Dir(p)), Cartella: dest}, io.Discard, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	percorso := filepath.Join(dest, "gestrip.db")

	var mu sync.Mutex
	var mandati []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corpo, _ := io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/getUpdates") {
			time.Sleep(100 * time.Millisecond)
			_, _ = io.WriteString(w, `{"ok":true,"result":[]}`)
			return
		}
		mu.Lock()
		mandati = append(mandati, r.URL.Path+" "+string(corpo))
		mu.Unlock()
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":5}}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("TELEGRAM_API_ENDPOINT", srv.URL+"/")
	letti := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), mandati...)
	}
	vera := aspettaPer
	t.Cleanup(func() { aspettaPer = vera })
	aspettaPer = func(ctx context.Context, _ time.Duration) bool { <-ctx.Done(); return false }

	// Il serve sulla copia isolata.
	var partiti sync.Map
	a := serveIsolato(t, percorso, &partiti)
	if _, err := a.dip.Lavori.EseguiAMano(ctx, jobs.Ritardi, audit.Contesto{Attore: "titolare@example.invalid"}); !errors.Is(err, jobs.ErrCopiaIsolata) {
		t.Errorf("Esegui adesso nella copia isolata: %v", err)
	}
	if ai, ok := a.dip.Assistente.(*assist.Impostazioni); !ok || !ai.SoloLocale {
		t.Error("modello remoto acceso nella copia isolata")
	}
	if !a.dip.CopiaIsolata {
		t.Error("banner della copia isolata spento")
	}
	// La conferma dalla console mentre serve gira: 1 manda, 2 e 3 scarta. Venerdì 27/11/2026 alle 09:30 in negozio.
	veraConferma := orologioConferma
	t.Cleanup(func() { orologioConferma = veraConferma })
	orologioConferma = clock.NuovoFinto(time.Date(2026, 11, 27, 9, 30, 0, 0, clock.Roma))
	var out bytes.Buffer
	if err := confermaRipristino(ctx, opzioniConfermaRipristino{DB: percorso}, &out, strings.NewReader("m\ns\n\nCONFERMO\n")); err != nil {
		t.Fatal(err)
	}
	if f := "Lavori di oggi segnati come già fatti: Buste in ritardo, Briefing del mattino. Se la macchina vecchia non li aveva fatti, " +
		"usa «Esegui adesso» in Promemoria e job dopo il riavvio.\n"; !strings.Contains(out.String(), f) {
		t.Errorf("la console non dice i lavori segnati:\n%s", out.String())
	}
	time.Sleep(500 * time.Millisecond)
	if c, corpo := richiesta(t, "GET", a.pubblica+"/artigiano/1?t="+gettone); c != http.StatusServiceUnavailable || !strings.Contains(corpo, "manutenzione") {
		t.Errorf("pubblica prima del riavvio: %d", c)
	}
	if err := a.ferma(); err != nil {
		t.Fatal(err)
	}
	if m := letti(); len(m) != 0 {
		t.Fatalf("Bot API prima del riavvio: %v", m)
	}
	partiti.Range(func(k, _ any) bool { t.Errorf("partito nella copia isolata: %v", k); return true })

	// Il serve riavviato: normale. I giri del pianificatore li dà il test (giro), sul suo orologio (oraLavori); finito
	// indica che il giro di prima è finito.
	var partitiDopo sync.Map
	oraLavori := clock.NuovoFinto(time.Date(2026, 11, 27, 9, 45, 0, 0, clock.Roma))
	giro, finito := make(chan struct{}), make(chan struct{}, 1)
	b := serveIsolatoCon(t, percorso, &partitiDopo, func(o *opzioniServe) {
		o.orologioLavori = oraLavori
		o.aspettaLavori = func(ctx context.Context) bool {
			partitiDopo.Store("lavori automatici", true)
			select {
			case finito <- struct{}{}:
			case <-ctx.Done():
				return false
			}
			select {
			case <-giro:
				return true
			case <-ctx.Done():
				return false
			}
		}
	})
	defer func() {
		if err := b.ferma(); err != nil {
			t.Error(err)
		}
	}()
	if strings.Contains(b.avvisi.String(), "COPIA ISOLATA") || b.dip.CopiaIsolata {
		t.Errorf("serve ancora isolato: %q", b.avvisi.String())
	}
	if ai, ok := b.dip.Assistente.(*assist.Impostazioni); !ok || ai.SoloLocale {
		t.Error("modello remoto ancora spento")
	}
	scadenza := time.Now().Add(15 * time.Second)
	for len(letti()) == 0 && time.Now().Before(scadenza) {
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond) // il tempo di mandare anche gli altri, se ci fossero
	if m := letti(); len(m) != 1 || m[0] != `/bot42:AAH-finto/sendMessage {"chat_id":"-1001","text":"primo-da-mandare"}` {
		t.Errorf("Bot API dopo il riavvio: %v", m)
	}
	for _, n := range []string{"lavori automatici", "backup notturno", "avvisi dei backup"} {
		if _, ok := partitiDopo.Load(n); !ok {
			t.Errorf("%s non partiti dopo la conferma", n)
		}
	}
	if c, corpo := richiesta(t, "GET", b.pubblica+"/artigiano/1?t="+gettone); c != http.StatusOK || !strings.Contains(corpo, "Anello inventato") {
		t.Errorf("link artigiano della macchina vecchia: %d", c)
	}
	if c, _ := richiesta(t, "GET", b.pubblica+"/artigiano/1?t="+altroGettone); c == http.StatusOK {
		t.Error("gettone di un'altra chiave accettato")
	}
	if c, corpo := richiesta(t, "GET", b.pubblica+"/stato/email-disattiva?t="+stop); c != http.StatusOK || !strings.Contains(corpo, "email-stop-button") {
		t.Errorf("link di disattivazione della macchina vecchia: %d", c)
	}
	if bytes.Equal(b.dip.ChiaveSessione, db.SottoChiave(vecchia, "sessione")) || bytes.Equal(b.dip.ChiaveCSRF, db.SottoChiave(vecchia, "csrf")) ||
		bytes.Equal(b.dip.ChiaveGalleria, db.SottoChiave(vecchia, "galleria")) {
		t.Error("sessione, CSRF o galleria ancora dalla chiave della macchina vecchia")
	}

	// D-119: i lavori del giorno dopo il riavvio.
	unGiro := func(alle time.Time) {
		t.Helper()
		<-finito
		oraLavori.Imposta(alle)
		giro <- struct{}{}
		select {
		case <-finito: // il giro è finito: il pianificatore aspetta il prossimo
			finito <- struct{}{}
		case <-time.After(15 * time.Second):
			t.Fatal("giro del pianificatore non finito")
		}
	}
	lavori := func() (righe map[string][]string, perNegozio int) {
		t.Helper()
		righe = map[string][]string{}
		conDatabase(t, percorso, func(d *db.DB) {
			rs, err := d.Lettura.Query(`SELECT lavoro, coalesce(dettaglio, '') FROM esecuzioni_lavori WHERE giorno = '2026-11-27' ORDER BY id`)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = rs.Close() }()
			for rs.Next() {
				var l, dettaglio string
				if err := rs.Scan(&l, &dettaglio); err != nil {
					t.Fatal(err)
				}
				righe[l] = append(righe[l], dettaglio)
			}
			if err := rs.Err(); err != nil {
				t.Fatal(err)
			}
			if err := d.Lettura.QueryRow(`SELECT count(*) FROM messaggi_in_uscita WHERE destinatario = 'shop' AND id > 3`).Scan(&perNegozio); err != nil {
				t.Fatal(err)
			}
		})
		return righe, perNegozio
	}
	segnati := map[string][]string{jobs.Ritardi: {jobs.DettaglioDopoRipristino}, jobs.Briefing: {jobs.DettaglioDopoRipristino}}
	if righe, n := lavori(); !reflect.DeepEqual(righe, segnati) || n != 0 {
		t.Fatalf("lavori segnati dalla conferma: %v, %d messaggi al negozio", righe, n)
	}
	unGiro(time.Date(2026, 11, 27, 9, 45, 0, 0, clock.Roma))
	if righe, n := lavori(); !reflect.DeepEqual(righe, segnati) || n != 0 {
		t.Fatalf("giro delle 09:45 dopo il riavvio: %v, %d messaggi al negozio", righe, n)
	}
	unGiro(time.Date(2026, 11, 27, 11, 0, 0, 0, clock.Roma))
	segnati[jobs.Solleciti] = []string{"Nessuna busta oltre soglia."}
	if righe, n := lavori(); !reflect.DeepEqual(righe, segnati) || n != 0 {
		t.Fatalf("giro delle 11:00: %v, %d messaggi al negozio", righe, n)
	}
	// «Esegui adesso» dopo il riavvio fa ripartire un lavoro segnato (se la macchina vecchia non l'aveva fatto): il
	// briefing gira e accoda il suo messaggio al negozio.
	e, err := b.dip.Lavori.EseguiAMano(ctx, jobs.Briefing, audit.Contesto{Attore: "titolare@example.invalid"})
	if err != nil || e.Esito != jobs.EsitoOK || e.Avvio != jobs.AvvioAMano || e.Messaggi != 1 {
		t.Fatalf("«Esegui adesso» del briefing dopo il riavvio: %+v %v", e, err)
	}
	segnati[jobs.Briefing] = append(segnati[jobs.Briefing], e.Dettaglio)
	if righe, n := lavori(); !reflect.DeepEqual(righe, segnati) || n != 1 {
		t.Fatalf("dopo «Esegui adesso»: %v, %d messaggi al negozio", righe, n)
	}
	conDatabase(t, percorso, func(d *db.DB) {
		var dati string
		if err := d.Lettura.QueryRow(`SELECT dati FROM eventi_audit WHERE azione = 'backup.restore_confirmed'`).Scan(&dati); err != nil ||
			!strings.Contains(dati, `"jobs_marked_done":2`) {
			t.Errorf("audit della conferma: %s %v", dati, err)
		}
	})
}
