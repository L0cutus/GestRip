package buste

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// La funzione SQL gestrip_senza_etichetta c'è su ogni connessione nuova del driver (anche una fuori da
// db.Apri) e, su una busta aperta, dice la stessa cosa del segno della lista: SenzaQR o QRAltroFormato.
// Review della slice 060.
func TestSenzaEtichettaSQLComeIlSegno(t *testing.T) {
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	codici := []any{nil, "", "   ", " \t", "RIP-2026-0045", " rip-2026- 0046 ", "RIP-2026-45",
		"RIP-2026-0000", "SINT-QR-0045", "RIP-26-0045", []byte("RIP-2026-0047"), []byte("SINT"), int64(45)}
	for _, c := range codici {
		var n int64
		if err := d.QueryRow(`SELECT `+fnSenzaEtichetta+`(?)`, c).Scan(&n); err != nil {
			t.Fatalf("%#v: %v", c, err)
		}
		var p *string
		switch v := c.(type) {
		case string:
			p = &v
		case []byte:
			s := string(v)
			p = &s
		case int64:
			s := "45"
			p = &s
		}
		segno := SenzaQR("in_lavorazione", p) || QRAltroFormato("in_lavorazione", p)
		if (n == 1) != segno {
			t.Errorf("%#v: SQL %d, segno della lista %v", c, n, segno)
		}
	}
}
