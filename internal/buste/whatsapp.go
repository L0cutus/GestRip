package buste

import (
	"strings"
	"unicode"
)

// Il numero WhatsApp e l'indirizzo wa.me (CLI-34), come Outgoing.WhatsApp dello standalone
// (go-baseline), copia di wa_telefono della sorgente: niente si apre da qui, si costruisce solo
// l'indirizzo che il browser dell'operatore apre.

// prefissiZeroDiLinea sono i prefissi esteri dopo i quali lo zero della linea nazionale si toglie,
// nell'ordine della sorgente (i più lunghi prima). L'Italia (39) non c'è apposta: lo zero di un
// fisso italiano fa parte del numero. Nessun prefisso è l'inizio di un altro.
var prefissiZeroDiLinea = strings.Fields(`
	351 352 353 354 355 356 357 358 359
	370 371 372 373 374 375 376 377 378 380
	381 382 383 385 386 387 389
	420 421 423
	30 31 32 33 34 36 40 41 43 44 45
	46 47 48 49 90`)

// NumeroWhatsApp è number/1: il telefono nella forma di un indirizzo wa.me («+39…»), o "" se non ha
// cifre (allora non è un recapito). Contano solo le cifre ASCII; il «+» davanti (dopo gli spazi)
// resta sempre, perché senza WhatsApp leggerebbe il numero come nazionale del paese di chi lo apre.
func NumeroWhatsApp(grezzo string) string {
	scritto := strings.TrimLeftFunc(grezzo, unicode.IsSpace)
	var c strings.Builder
	for i := 0; i < len(scritto); i++ {
		if scritto[i] >= '0' && scritto[i] <= '9' {
			c.WriteByte(scritto[i])
		}
	}
	cifre := c.String()
	switch {
	case cifre == "":
		return ""
	case strings.HasPrefix(scritto, "+"):
		if strings.HasPrefix(cifre, "39") {
			return "+" + cifre
		}
		return "+" + togliZeroDiLinea(cifre)
	case strings.HasPrefix(cifre, "0039"):
		return "+" + cifre[2:]
	case strings.HasPrefix(cifre, "00"):
		return "+" + togliZeroDiLinea(cifre[2:])
	case strings.HasPrefix(cifre, "39"):
		return "+" + cifre
	}
	return "+39" + cifre
}

// togliZeroDiLinea è _strip_zero_rete: il primo prefisso della tabella che combacia decide; se
// dopo c'è uno zero si toglie, altrimenti il numero resta com'è (il break della sorgente).
func togliZeroDiLinea(cifre string) string {
	for _, p := range prefissiZeroDiLinea {
		if strings.HasPrefix(cifre, p) {
			if resto := cifre[len(p):]; strings.HasPrefix(resto, "0") {
				return p + resto[1:]
			}
			return cifre
		}
	}
	return cifre
}

// IndirizzoWaMe è url/2: https://wa.me/<numero>?text=<testo>, tutti e due codificati come
// URI.encode(…, &URI.char_unreserved?/1). Un numero vuoto dà wa.me/?text=, la scelta del contatto.
func IndirizzoWaMe(numero, testo string) string {
	return "https://wa.me/" + codificaRiservati(numero) + "?text=" + codificaRiservati(testo)
}

// codificaRiservati scrive %XX (esadecimale maiuscolo) per ogni byte che non è una lettera o una
// cifra ASCII o uno di «-._~».
func codificaRiservati(s string) string {
	const esa = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(esa[c>>4])
		b.WriteByte(esa[c&15])
	}
	return b.String()
}
