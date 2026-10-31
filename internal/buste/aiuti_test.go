package buste

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Aiuti dei test del pacchetto: un database migrato con un'oreficeria esterna e un artigiano
// inventati, un orologio fermo al 31/10/2026 (in negozio le 10:00) e un modulo di base.

var ctx = context.Background()

var chi = audit.Contesto{Attore: "titolare@esempio.test", IP: "203.0.113.7"}

const istanteProva = "2026-10-31T09:00:00.000000Z"

func orologio() *clock.Finto {
	return clock.NuovoFinto(time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
}

func prova(t *testing.T) (*Buste, *db.DB, *clock.Finto) {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := orologio()
	if _, err := d.Migra(ctx, o); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO oreficerie (id, nome) VALUES (1, 'Il negozio di prova')`,
		`INSERT INTO oreficerie (id, nome, telefono) VALUES (2, 'Oreficeria Esempio', '0490000000')`,
		`INSERT INTO artigiani (id, nome) VALUES (1, 'Artigiano Esempio')`,
	} {
		esegui(t, d, q)
	}
	return Nuove(d, o), d, o
}

// modulo è un'accettazione valida: cliente nuovo, numero automatico, QR presente.
func modulo() Accettazione {
	return Accettazione{
		NomeCliente:      "Cliente Prova",
		TelefonoCliente:  "333 000 0001",
		DataAccettazione: "2026-10-31",
		Descrizione:      "Anello da stringere",
		CodiceQR:         "QR-PROVA",
	}
}

func con(a Accettazione, f func(*Accettazione)) Accettazione {
	f(&a)
	return a
}

func esegui(t *testing.T, d *db.DB, q string, args ...any) {
	t.Helper()
	if _, err := d.Scrittura.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func conta(t *testing.T, d *db.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := d.Lettura.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// valore legge una sola colonna; NULL diventa nil, un BLOB []byte, il resto stringa o intero.
func valore(t *testing.T, d *db.DB, q string, args ...any) any {
	t.Helper()
	var v any
	if err := d.Lettura.QueryRow(q, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return v
}

func crea(t *testing.T, b *Buste, a Accettazione) Creata {
	t.Helper()
	c, err := b.Crea(ctx, a, chi)
	if err != nil {
		t.Fatalf("crea: %v", err)
	}
	return c
}

// fotografia è lo stato delle tabelle che una creazione può toccare: un rifiuto la lascia uguale.
type fotografia struct {
	Buste, Clienti, Audit, Sequenze, Versioni int
	Contatori, Telefoni                       string
}

func scatta(t *testing.T, d *db.DB) fotografia {
	t.Helper()
	var f fotografia
	f.Buste = conta(t, d, `SELECT count(*) FROM buste`)
	f.Clienti = conta(t, d, `SELECT count(*) FROM clienti`)
	f.Audit = conta(t, d, `SELECT count(*) FROM eventi_audit`)
	f.Sequenze = conta(t, d, `SELECT count(*) FROM sequenze_buste`)
	f.Versioni = conta(t, d, `SELECT count(*) FROM versioni_stato_buste`)
	var s sql.NullString
	_ = d.Lettura.QueryRow(`SELECT group_concat(anno || ':' || ultimo_numero, ',') FROM sequenze_buste`).Scan(&s)
	f.Contatori = s.String
	s = sql.NullString{}
	_ = d.Lettura.QueryRow(`SELECT group_concat(id || ':' || nome || ':' || telefono || ':' || aggiornato_il, ',') FROM clienti`).Scan(&s)
	f.Telefoni = s.String
	return f
}

func uguali(t *testing.T, cosa string, prima, dopo fotografia) {
	t.Helper()
	if !reflect.DeepEqual(prima, dopo) {
		t.Errorf("%s: il database è cambiato\nprima %+v\ndopo  %+v", cosa, prima, dopo)
	}
}

// erroriCampi estrae gli errori sui campi, o fallisce.
func erroriCampi(t *testing.T, err error) Campi {
	t.Helper()
	var c Campi
	if !errors.As(err, &c) {
		t.Fatalf("atteso un errore sui campi, avuto %v", err)
	}
	return c
}

// ultimoAudit è «azione|risorsa|id|esito|dati» dell'ultima riga con quell'azione.
func ultimoAudit(t *testing.T, d *db.DB, azione string) string {
	t.Helper()
	var a, r, i, e, dati string
	if err := d.Lettura.QueryRow(`SELECT azione, risorsa, id_risorsa, esito, dati FROM eventi_audit WHERE azione = ? ORDER BY id DESC LIMIT 1`, azione).
		Scan(&a, &r, &i, &e, &dati); err != nil {
		return ""
	}
	return a + "|" + r + "|" + i + "|" + e + "|" + dati
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
