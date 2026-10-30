package audit

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

var ctx = context.Background()

func registroProva(t *testing.T) (*Registro, *db.DB) {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(time.Date(2026, 10, 30, 9, 15, 7, 123456789, time.UTC))
	if _, err := d.Migra(ctx, o); err != nil {
		t.Fatal(err)
	}
	return Nuovo(d, o), d
}

type riga struct {
	attore, azione, risorsa, id, esito, dati string
	ip, ua                                   any
	creato                                   string
}

func ultima(t *testing.T, d *db.DB) riga {
	t.Helper()
	var r riga
	if err := d.Lettura.QueryRow(`SELECT attore, azione, risorsa, id_risorsa, esito, dati, ip, user_agent, creato_il FROM eventi_audit ORDER BY id DESC LIMIT 1`).
		Scan(&r.attore, &r.azione, &r.risorsa, &r.id, &r.esito, &r.dati, &r.ip, &r.ua, &r.creato); err != nil {
		t.Fatal(err)
	}
	return r
}

// inventario: ADM-17
// Una riga come Audit.insert_with_outcome/7: attore, ip e user agent dal contesto (vuoti →
// nulli), l'id della risorsa come testo, i dati in JSON (nessun dato → {}), l'ora dell'orologio
// del negozio in UTC.
func TestRegistra(t *testing.T) {
	r, d := registroProva(t)
	if err := r.Registra(ctx, Contesto{Attore: "public", IP: "203.0.113.5"}, "security.attempts_blocked", "attempt_limit", "short", "denied",
		map[string]any{"failures": 10, "window_minutes": 15, "block_minutes": 15}); err != nil {
		t.Fatal(err)
	}
	got := ultima(t, d)
	want := riga{"public", "security.attempts_blocked", "attempt_limit", "short", "denied",
		`{"block_minutes":15,"failures":10,"window_minutes":15}`, "203.0.113.5", nil, "2026-10-30T09:15:07.123456Z"}
	if got != want {
		t.Errorf("riga %+v\natteso %+v", got, want)
	}
	if err := r.Registra(ctx, Contesto{Attore: "titolare@esempio.it", UserAgent: "Prova/1"}, "user.deactivated", "user", 7, "ok", nil); err != nil {
		t.Fatal(err)
	}
	if got := ultima(t, d); got.id != "7" || got.dati != "{}" || got.ip != nil || got.ua != "Prova/1" {
		t.Errorf("riga %+v", got)
	}
}

// inventario: ADM-17
// Dentro una transazione la riga c'è solo se la transazione va a buon fine: se la modifica si
// annulla, si annulla anche l'audit. Una riga che il database rifiuta è un errore, che annulla la
// modifica (validate_required dello standalone: attore vuoto).
func TestScriviNellaTransazione(t *testing.T) {
	r, d := registroProva(t)
	tx, err := d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Scrivi(ctx, tx, Contesto{Attore: "x"}, "a.b", "r", "1", "ok", nil); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = d.Lettura.QueryRow(`SELECT count(*) FROM eventi_audit`).Scan(&n)
	if n != 0 {
		t.Errorf("righe dopo il rollback: %d", n)
	}
	if err := r.Registra(ctx, Contesto{}, "a.b", "r", "1", "ok", nil); err == nil {
		t.Error("attore vuoto accettato")
	}
	if err := r.Registra(ctx, Contesto{Attore: "x"}, "a.b", "r", "1", "ok", map[string]any{"x": make(chan int)}); err == nil {
		t.Error("dati che non diventano JSON accettati")
	}
}
