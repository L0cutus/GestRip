package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// inventario: CLI-51, ADM-10
// gestrip serve registra il provider Telegram (slice 071) sull'indirizzo del Bot API di
// TELEGRAM_API_ENDPOINT (qui un Bot API finto in processo, mai Telegram): con il bot configurato e
// l'invio Telegram acceso (D-03) la riga rimasta in coda parte al primo giro ed è accettata.
func TestServeTelegram(t *testing.T) {
	vera := aspettaPer
	t.Cleanup(func() { aspettaPer = vera })
	aspettaPer = func(ctx context.Context, _ time.Duration) bool {
		<-ctx.Done()
		return false
	}
	arrivati := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corpo, _ := io.ReadAll(r.Body)
		arrivati <- r.URL.Path + " " + string(corpo)
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":5}}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("TELEGRAM_API_ENDPOINT", srv.URL+"/")
	percorso := filepath.Join(t.TempDir(), "g.db")
	d, err := db.Apri(percorso)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Migra(context.Background(), clock.Sistema{}); err != nil {
		t.Fatal(err)
	}
	ora := "2026-01-01T09:00:00.000000Z"
	for _, q := range []string{
		`INSERT INTO segreti_provider (nome, valore, aggiornato_il) VALUES ('outgoing.telegram.bot_token', '42:AAH-finto', '` + ora + `')`,
		`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.telegram.chat_id', '-1001', '` + ora + `'), ('` +
			uscita.ChiaveTelegramAcceso + `', '1', '` + ora + `')`,
		`INSERT INTO messaggi_in_uscita (canale, destinatario, indirizzo, testo, stato, tentativi, prossimo_tentativo_il, accodato_il)
			VALUES ('telegram', 'shop', '-1001', 'testo di prova', 'queued', 0, '` + ora + `', '` + ora + `')`,
	} {
		if _, err := d.Scrittura.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Chiudi(); err != nil {
		t.Fatal(err)
	}
	o := opzioniServe{SenzaVolumeCifrato: true, DB: percorso, Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0", avvisi: io.Discard}
	ctx, ferma := context.WithCancel(context.Background())
	finito := make(chan error, 1)
	go func() { finito <- serve(ctx, o, nil) }()
	select {
	case got := <-arrivati:
		if got != `/bot42:AAH-finto/sendMessage {"chat_id":"-1001","text":"testo di prova"}` {
			t.Errorf("richiesta %q", got)
		}
	case err := <-finito:
		t.Fatal(err)
	case <-time.After(15 * time.Second):
		t.Fatal("il messaggio rimasto in coda non è arrivato al Bot API")
	}
	var stati map[string]string
	for range 100 {
		stati = statiRighe(t, percorso, map[string]int64{"telegram": 1})
		if stati["telegram"] != "queued|0" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	ferma()
	if err := <-finito; err != nil {
		t.Fatal(err)
	}
	if stati["telegram"] != "accepted|1" {
		t.Errorf("stato %v", stati)
	}
}

// inventario: ADM-10
// Un TELEGRAM_API_ENDPOINT che non è un indirizzo http(s) con un host ferma l'avvio prima del database,
// come config/runtime.exs dello standalone; il messaggio nomina la variabile, mai il valore.
func TestServeTelegramEndpointSbagliato(t *testing.T) {
	t.Setenv("TELEGRAM_API_ENDPOINT", "ftp://bot.example.invalid/segreto")
	percorso := filepath.Join(t.TempDir(), "g.db")
	o := opzioniServe{SenzaVolumeCifrato: true, DB: percorso, Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0", avvisi: io.Discard}
	err := serve(context.Background(), o, nil)
	if err == nil || !strings.Contains(err.Error(), "TELEGRAM_API_ENDPOINT") || strings.Contains(err.Error(), "segreto") {
		t.Fatalf("errore %v", err)
	}
	if _, err := os.Stat(percorso); !os.IsNotExist(err) {
		t.Errorf("database creato: %v", err)
	}
}

// serveConBot avvia serve su un database con il bot configurato (token e chat finti), l'invio Telegram
// acceso o no e le notifiche ai clienti accese o no, con TELEGRAM_API_ENDPOINT puntato a gestore (un Bot
// API finto in processo). Dà la funzione che ferma serve e aspetta che finisca, e il database.
func serveConBot(t *testing.T, acceso, notifiche string, gestore http.HandlerFunc) (func() (time.Duration, error), string) {
	t.Helper()
	srv := httptest.NewServer(gestore)
	t.Cleanup(srv.Close)
	t.Setenv("TELEGRAM_API_ENDPOINT", srv.URL)
	percorso := filepath.Join(t.TempDir(), "g.db")
	d, err := db.Apri(percorso)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Migra(context.Background(), clock.Sistema{}); err != nil {
		t.Fatal(err)
	}
	ora := "2026-01-01T09:00:00.000000Z"
	for _, q := range []string{
		`INSERT INTO segreti_provider (nome, valore, aggiornato_il) VALUES ('outgoing.telegram.bot_token', '42:AAH-finto', '` + ora + `')`,
		`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.telegram.chat_id', '-1001', '` + ora + `'), ('` +
			uscita.ChiaveTelegramAcceso + `', '` + acceso + `', '` + ora + `'), ('` + uscita.ChiaveNotificheClienti + `', '` + notifiche + `', '` + ora + `')`,
		`INSERT INTO messaggi_in_uscita (canale, destinatario, indirizzo, testo, stato, tentativi, prossimo_tentativo_il, accodato_il)
			VALUES ('telegram', 'shop', '-1001', 'testo di prova', 'queued', 0, '` + ora + `', '` + ora + `')`,
	} {
		if _, err := d.Scrittura.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Chiudi(); err != nil {
		t.Fatal(err)
	}
	o := opzioniServe{SenzaVolumeCifrato: true, DB: percorso, Interna: "127.0.0.1:0", Pubblica: "127.0.0.1:0", avvisi: io.Discard}
	ctx, ferma := context.WithCancel(context.Background())
	finito := make(chan error, 1)
	go func() { finito <- serve(ctx, o, nil) }()
	return func() (time.Duration, error) {
		inizio := time.Now()
		ferma()
		select {
		case err := <-finito:
			return time.Since(inizio), err
		case <-time.After(30 * time.Second):
			t.Fatal("serve non si è fermato")
			return 0, nil
		}
	}, percorso
}

// inventario: CLI-46
// gestrip serve avvia il lettore getUpdates (slice 073) con i lavori: con «Invio Telegram» e «Notifiche
// Telegram ai clienti» accesi chiede gli aggiornamenti al Bot API di TELEGRAM_API_ENDPOINT (qui finto, mai
// Telegram); all'arresto la lettura lunga si chiude subito e serve aspetta il lettore, senza scrivere un
// errore del lettore per la chiusura.
func TestServeLettoreTelegram(t *testing.T) {
	letture := make(chan struct{}, 8)
	ferma, percorso := serveConBot(t, "1", "1", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getUpdates") {
			// Il corpo letto tutto: solo allora il server si accorge che il lettore chiude.
			_, _ = io.ReadAll(r.Body)
			letture <- struct{}{}
			// La lettura lunga: risponde solo dopo 25 secondi, o mai se il lettore la chiude.
			select {
			case <-r.Context().Done():
				return
			case <-time.After(25 * time.Second):
			}
			_, _ = io.WriteString(w, `{"ok":true,"result":[]}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":5}}`)
	})
	select {
	case <-letture:
	case <-time.After(15 * time.Second):
		t.Fatal("il lettore non ha chiesto getUpdates")
	}
	quanto, err := ferma()
	if err != nil {
		t.Fatal(err)
	}
	if quanto > 5*time.Second {
		t.Errorf("serve si è fermato dopo %v", quanto)
	}
	d, err := db.Apri(percorso)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Chiudi() }()
	var errore string
	_ = d.Lettura.QueryRow(`SELECT valore FROM impostazioni WHERE chiave = ?`, uscita.ChiaveErroreLettore).Scan(&errore)
	if errore != "" {
		t.Errorf("errore del lettore scritto all'arresto: %q", errore)
	}
}

// inventario: CLI-46
// Con «Notifiche Telegram ai clienti» spente (o «Invio Telegram» spento) il lettore resta fermo: nessuna
// getUpdates, anche se la coda manda la riga del negozio.
func TestServeLettoreTelegramSpento(t *testing.T) {
	for _, c := range []struct{ acceso, notifiche string }{{"1", ""}, {"0", "1"}} {
		var mu sync.Mutex
		var percorsi []string
		ferma, _ := serveConBot(t, c.acceso, c.notifiche, func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			percorsi = append(percorsi, r.URL.Path)
			mu.Unlock()
			_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":5}}`)
		})
		time.Sleep(time.Second)
		if _, err := ferma(); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		for _, p := range percorsi {
			if strings.HasSuffix(p, "/getUpdates") {
				t.Errorf("invio %q, notifiche %q: getUpdates chiesto", c.acceso, c.notifiche)
			}
		}
		if c.acceso == "1" && len(percorsi) != 1 {
			t.Errorf("invio acceso: richieste %v", percorsi)
		}
		mu.Unlock()
	}
}

// inventario: CLI-46
// serve aspetta il lettore: un aggiornamento cominciato quando arriva l'arresto si finisce (la risposta
// parte, l'offset si salva) prima che serve chiuda il database.
func TestServeAspettaIlLettore(t *testing.T) {
	arrivata, finita := make(chan struct{}), make(chan struct{})
	var una sync.Once
	var mu sync.Mutex
	letto := false
	ferma, percorso := serveConBot(t, "1", "1", func(w http.ResponseWriter, r *http.Request) {
		corpo, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			mu.Lock()
			primo := !letto
			letto = true
			mu.Unlock()
			if primo {
				_, _ = io.WriteString(w, `{"ok":true,"result":[{"update_id":1,"message":{"message_id":1,"from":{"id":4503599627370497,"is_bot":false},"chat":{"id":4503599627370497,"type":"private"},"text":"ciao"}}]}`)
				return
			}
			<-r.Context().Done()
		case strings.Contains(string(corpo), `"chat_id":4503599627370497`):
			una.Do(func() { close(arrivata) })
			time.Sleep(1500 * time.Millisecond)
			_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":5}}`)
			close(finita)
		default:
			_, _ = io.WriteString(w, `{"ok":true,"result":{"message_id":6}}`)
		}
	})
	select {
	case <-arrivata:
	case <-time.After(15 * time.Second):
		t.Fatal("la risposta del bot non è arrivata")
	}
	if _, err := ferma(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finita:
	default:
		t.Fatal("serve è finito prima della risposta del lettore")
	}
	d, err := db.Apri(percorso)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Chiudi() }()
	var offset string
	_ = d.Lettura.QueryRow(`SELECT valore FROM impostazioni WHERE chiave = 'telegram_inbound.offset'`).Scan(&offset)
	if offset != "2" {
		t.Errorf("offset %q dopo l'arresto", offset)
	}
}
