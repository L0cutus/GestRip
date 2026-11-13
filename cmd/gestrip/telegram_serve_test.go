package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
