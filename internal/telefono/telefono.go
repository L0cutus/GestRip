// Package telefono è la normalizzazione del telefono dello standalone (Workshop.Phone), senza
// dipendenze interne: la usano le anagrafiche (buste, web) e l'assistente della busta nuova (assist).
package telefono

import (
	"strings"
	"unicode"
)

// cifrePython sono i caratteri che str.isdigit di Python accetta oltre a \p{Nd}
// (@python_digit_ranges di Workshop.Phone): apici, pedici, cifre cerchiate e affini.
var cifrePython = &unicode.RangeTable{
	R16: []unicode.Range16{
		{Lo: 0x00B2, Hi: 0x00B3, Stride: 1}, {Lo: 0x00B9, Hi: 0x00B9, Stride: 1},
		{Lo: 0x1369, Hi: 0x1371, Stride: 1}, {Lo: 0x19DA, Hi: 0x19DA, Stride: 1},
		{Lo: 0x2070, Hi: 0x2070, Stride: 1}, {Lo: 0x2074, Hi: 0x2079, Stride: 1},
		{Lo: 0x2080, Hi: 0x2089, Stride: 1}, {Lo: 0x2460, Hi: 0x2468, Stride: 1},
		{Lo: 0x2474, Hi: 0x247C, Stride: 1}, {Lo: 0x2488, Hi: 0x2490, Stride: 1},
		{Lo: 0x24EA, Hi: 0x24EA, Stride: 1}, {Lo: 0x24F5, Hi: 0x24FD, Stride: 1},
		{Lo: 0x24FF, Hi: 0x24FF, Stride: 1}, {Lo: 0x2776, Hi: 0x277E, Stride: 1},
		{Lo: 0x2780, Hi: 0x2788, Stride: 1}, {Lo: 0x278A, Hi: 0x2792, Stride: 1},
	},
	R32: []unicode.Range32{
		{Lo: 0x10A40, Hi: 0x10A43, Stride: 1}, {Lo: 0x10E60, Hi: 0x10E68, Stride: 1},
		{Lo: 0x11052, Hi: 0x1105A, Stride: 1}, {Lo: 0x1F100, Hi: 0x1F10A, Stride: 1},
	},
}

// Normalizza è Workshop.Phone.normalize/1 (normalizza_telefono del Flask): tolti gli spazi
// ai bordi, un «+» in testa resta e poi solo le cifre di Python; "" se non resta nessuna cifra.
func Normalizza(s string) string {
	s = strings.TrimSpace(s)
	var cifre strings.Builder
	for _, r := range s {
		if unicode.Is(unicode.Nd, r) || unicode.Is(cifrePython, r) {
			cifre.WriteRune(r)
		}
	}
	if cifre.Len() == 0 {
		return ""
	}
	if strings.HasPrefix(s, "+") {
		return "+" + cifre.String()
	}
	return cifre.String()
}
