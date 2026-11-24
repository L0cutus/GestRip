package audit

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// inventario: ADM-17
// L'errore di OrologioAvanti è neutro: la usa anche la scadenza dei dettati (D-110), quindi non parla della
// conservazione dell'audit. L'evento illeggibile si ottiene con una tabella senza il CHECK dello schema.
func TestOrologioAvantiErroreNeutro(t *testing.T) {
	d, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if _, err := d.Exec(`CREATE TABLE eventi_audit (azione TEXT, creato_il TEXT); INSERT INTO eventi_audit VALUES ('x', 'illeggibile')`); err != nil {
		t.Fatal(err)
	}
	_, _, err = OrologioAvanti(ctx, d, time.Now())
	if err == nil || strings.Contains(err.Error(), "conservazione") || !strings.Contains(err.Error(), "evento più recente") {
		t.Errorf("errore %v", err)
	}
}
