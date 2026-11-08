package buste

import "testing"

// inventario: RIP-H-12
// D-79 (slice 059): una busta aperta con un codice che non è un'etichetta RIP-AAAA-NNNN ha un QR di
// altro formato; senza codice, con un'etichetta (anche minuscola o con spazi) o chiusa no.
func TestQRAltroFormato(t *testing.T) {
	testo := func(s string) *string { return &s }
	for _, c := range []struct {
		stato  string
		codice *string
		altro  bool
	}{
		{"in_lavorazione", testo("SINT-QR-0045"), true}, {"pronta", testo("RIP-2026-45"), true},
		{"in_valutazione", testo("RIP 2026 0045"), true}, {"in_lavorazione", testo("RIP-2026-0000"), true},
		{"in_lavorazione", testo("RIP-2026-0045"), false}, {"in_lavorazione", testo(" rip-2026- 0045 "), false},
		{"in_lavorazione", nil, false}, {"in_lavorazione", testo("  "), false}, {"in_lavorazione", testo(""), false},
		{"consegnata", testo("SINT-QR-0045"), false}, {"annullata", testo("SINT-QR-0045"), false},
		{"chiusa", testo("SINT-QR-0045"), false},
	} {
		if got := QRAltroFormato(c.stato, c.codice); got != c.altro {
			t.Errorf("%s %v: %v", c.stato, c.codice, got)
		}
		if QRAltroFormato(c.stato, c.codice) && SenzaQR(c.stato, c.codice) {
			t.Errorf("%s %v: altro formato e senza QR insieme", c.stato, c.codice)
		}
	}
}
