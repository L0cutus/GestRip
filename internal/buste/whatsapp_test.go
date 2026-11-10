package buste

import (
	"strings"
	"testing"
)

// I casi di whatsapp_test.exs dello standalone (go-baseline): number/1 e url/2 di
// Outgoing.WhatsApp, copia di wa_telefono della sorgente. Numeri inventati.

// inventario: CLI-34
// Senza prefisso +39; +39 e 39 restano; 00 e 0039 diventano +; lo zero dopo un prefisso estero della
// tabella si toglie (anche dopo un + scritto), mai per l'Italia; il prefisso più lungo vince; un
// prefisso fuori tabella tiene tutte le cifre; senza cifre il numero non c'è; il + resta sempre.
func TestNumeroWhatsApp(t *testing.T) {
	casi := map[string]string{
		"333 1234567":          "+393331234567",
		"+39 333 1234567":      "+393331234567",
		"39 333 1234567":       "+393331234567",
		"00 39 333 1234567":    "+393331234567",
		"+32 82 33 53 58":      "+3282335358",
		"00 32 82 33 53 58":    "+3282335358",
		"0033 0626123456":      "+33626123456",
		"+39 0423 601234":      "+390423601234",
		"0039 0423 601234":     "+390423601234",
		"+33 0626123456":       "+33626123456",
		"00352 0621123456":     "+352621123456",
		"001 0555123456":       "+10555123456",
		"":                     "",
		"   ":                  "",
		"non un numero":        "",
		"  +33 0626 12 34 56 ": "+33626123456",
		"(0423) 60-12-34":      "+390423601234",
		"+":                    "",
		"٣٣٣ 1234":             "+391234", // cifre non ASCII: non contano, come il filtro sui byte 0-9
	}
	for dentro, atteso := range casi {
		if got := NumeroWhatsApp(dentro); got != atteso {
			t.Errorf("NumeroWhatsApp(%q) = %q, atteso %q", dentro, got, atteso)
		}
	}
	if NumeroWhatsApp("0039 0423 601234") != NumeroWhatsApp("00 39 0423 601234") {
		t.Error("0039 e 00 39 devono dare lo stesso numero")
	}
}

// inventario: CLI-34
// La tabella dei prefissi è quella della sorgente: l'Italia (39) non c'è, e nessun prefisso è
// l'inizio di un altro (così il primo che combacia è quello giusto).
func TestPrefissiZeroDiLinea(t *testing.T) {
	for _, p := range prefissiZeroDiLinea {
		if p == "39" {
			t.Error("39 nella tabella")
		}
		for _, q := range prefissiZeroDiLinea {
			if p != q && strings.HasPrefix(q, p) {
				t.Errorf("%s è l'inizio di %s", p, q)
			}
		}
	}
	if len(prefissiZeroDiLinea) != 45 {
		t.Errorf("prefissi %d, attesi 45", len(prefissiZeroDiLinea))
	}
}

// inventario: CLI-34
// url/2: il numero e il testo codificati come URI.encode(…, &URI.char_unreserved?/1): tutto tranne
// lettere e cifre ASCII e «-._~» diventa %XX maiuscolo (anche il «+»); senza numero wa.me/?text=.
func TestIndirizzoWaMe(t *testing.T) {
	casi := []struct{ numero, testo, atteso string }{
		{"+393331234567", "Ciao Ada & C.", "https://wa.me/%2B393331234567?text=Ciao%20Ada%20%26%20C."},
		{"", "Ciao", "https://wa.me/?text=Ciao"},
		{"+39333", "à\n👋 ~_-.", "https://wa.me/%2B39333?text=%C3%A0%0A%F0%9F%91%8B%20~_-."},
	}
	for _, c := range casi {
		if got := IndirizzoWaMe(c.numero, c.testo); got != c.atteso {
			t.Errorf("IndirizzoWaMe(%q, %q) = %q, atteso %q", c.numero, c.testo, got, c.atteso)
		}
	}
}
