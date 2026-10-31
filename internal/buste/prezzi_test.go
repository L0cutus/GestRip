package buste

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// I prezzi a testo (RIP-H-36, RIP-S-05), come Money.cents/1 e Repair.cast_price/4; i casi da
// money_test.exs, workshop_test.exs e workshop_live_test.exs.

// inventario: RIP-H-36, RIP-S-05
// Centesimi solo per un importo semplice (cifre ASCII, virgola o punto, al più due decimali, al
// più 20 byte), con il conto decimale esatto; tutto il resto non ha centesimi.
func TestCentesimi(t *testing.T) {
	for _, c := range []struct {
		testo string
		cent  int64
		ok    bool
	}{
		{"120", 12000, true}, {"120,50", 12050, true}, {"120.5", 12050, true}, {"19,99", 1999, true},
		{"0", 0, true}, {"007", 700, true}, {"0,01", 1, true}, {"10,00", 1000, true},
		{"92233720368547758.07", 9223372036854775807, true},
		{"92233720368547758.08", 0, false}, {"99999999999999999999", 0, false},
		{"000000000000000000001", 0, false}, // 21 byte
		{"400/500", 0, false}, {"da definire", 0, false}, {"35 €", 0, false}, {"-1", 0, false},
		{"+1", 0, false}, {"1.001", 0, false}, {"1,2,3", 0, false}, {" 120", 0, false},
		{"120 ", 0, false}, {"12.", 0, false}, {".5", 0, false}, {"1.000,00", 0, false},
		{"١٢٣", 0, false}, {"", 0, false}, {"12\n", 0, false},
	} {
		got, ok := Centesimi(c.testo)
		if ok != c.ok || got != c.cent {
			t.Errorf("Centesimi(%q) = %d %v, atteso %d %v", c.testo, got, ok, c.cent, c.ok)
		}
	}
}

// inventario: RIP-H-36, RIP-S-05
// Il testo si conserva com'è, anche con gli spazi; vuoto o di soli spazi vuol dire testo e
// centesimi a NULL; oltre 255 byte «Importo troppo lungo: massimo 255 caratteri.».
func TestPrezzoATesto(t *testing.T) {
	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	cent := func(p *int64) string {
		if p == nil {
			return "<nil>"
		}
		return fmt.Sprint(*p)
	}
	for _, c := range []struct{ in, testo, cent string }{
		{"400/500", "400/500", "<nil>"},
		{"120,50", "120,50", "12050"},
		{" 120 ", " 120 ", "<nil>"},
		{"", "<nil>", "<nil>"},
		{"   ", "<nil>", "<nil>"},
		{"\t\n", "<nil>", "<nil>"},
		{strings.Repeat("9", 255), strings.Repeat("9", 255), "<nil>"},
	} {
		p, err := LeggiPrezzo(c.in)
		if err != nil || str(p.Testo) != c.testo || cent(p.Centesimi) != c.cent {
			t.Errorf("LeggiPrezzo(%q) = %q %s %v", c.in, str(p.Testo), cent(p.Centesimi), err)
		}
	}
	for _, lungo := range []string{strings.Repeat("9", 256), strings.Repeat("é", 128)} {
		if _, err := LeggiPrezzo(lungo); !errors.Is(err, ErrImportoLungo) || err.Error() != "Importo troppo lungo: massimo 255 caratteri." {
			t.Errorf("%d byte: %v", len(lungo), err)
		}
	}
}

// inventario: RIP-H-36, RIP-S-05
// All'accettazione il preventivo va nel testo e, se è un importo, nei centesimi; un testo troppo
// lungo resta un errore sul campo e non salva niente. Il prezzo finale non si imposta.
func TestPreventivoAllAccettazione(t *testing.T) {
	b, d, _ := prova(t)
	for _, c := range []struct {
		scritto string
		testo   any
		cent    any
	}{
		{"120,50", "120,50", int64(12050)}, {"400/500", "400/500", nil}, {"120.5", "120.5", int64(12050)},
		{"0", "0", int64(0)}, {"92233720368547758.08", "92233720368547758.08", nil}, {"", nil, nil},
	} {
		got := crea(t, b, con(modulo(), func(a *Accettazione) { a.Preventivo = c.scritto }))
		if v := valore(t, d, `SELECT preventivo_testo FROM buste WHERE id = ?`, got.ID); v != c.testo {
			t.Errorf("%q: testo %v", c.scritto, v)
		}
		if v := valore(t, d, `SELECT preventivo_centesimi FROM buste WHERE id = ?`, got.ID); v != c.cent {
			t.Errorf("%q: centesimi %v", c.scritto, v)
		}
		if v := valore(t, d, `SELECT coalesce(prezzo_testo, prezzo_centesimi, 'nessuno') FROM buste WHERE id = ?`, got.ID); v != "nessuno" {
			t.Errorf("%q: prezzo finale %v", c.scritto, v)
		}
	}
	prima := scatta(t, d)
	_, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.Preventivo = strings.Repeat("9", 256) }), chi)
	if c := erroriCampi(t, err); fmt.Sprint(c["preventivo"]) != "[Importo troppo lungo: massimo 255 caratteri.]" {
		t.Errorf("errori %v", c)
	}
	uguali(t, "preventivo troppo lungo", prima, scatta(t, d))
}
