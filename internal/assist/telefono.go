package assist

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Il telefono letto dal dettato (D-104, Stefano 19/11/2026, domanda E del piano T4.10): porting di
// trova_telefono di traduttore.py, il traduttore di produzione di pc-fisso (28/10/2026). Whisper
// scrive i numeri dettati a pezzi in tanti modi («347-123-4567», «347, 123, 45, 67», «3 4 7 1 2…»,
// «347/1234567»): si uniscono i gruppi di cifre separati solo da spazi, punti, virgole, trattini o
// barre, e il primo gruppo di 7-13 cifre è il telefono. Prima si tolgono le date con l'anno e i
// numeri di busta «N/AA», che uniti farebbero 7 o più cifre; il numero subito dopo
// busta/acconto/preventivo/numero, o subito prima di euro/€, non si unisce agli altri.
//
// Le espressioni di Python sono Unicode: \d è una cifra decimale di qualsiasi scrittura (\p{Nd}),
// \s lo spazio di str.isspace(), \b il confine fra una «parola» (lettera, numero o «_») e il
// resto, re.I piega anche «İ» e «ı» su «i». In Go \d, \s e \b sono solo ASCII: qui le classi sono
// scritte per esteso e i \b si controllano a mano (confinePython), così il risultato è lo stesso
// anche con lettere accentate attaccate alle cifre («è12/12/2026» per Python non è una data).

// spazioPython sono i caratteri di str.isspace() di Python 3.12, per una classe di regexp.
const spazioPython = `\t\n\v\f\r\x{1c}-\x{20}\x{85}\x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}`

const sp = `[` + spazioPython + `]*`

var (
	// DATA_ANNO_RE senza i \b: data con l'anno di quattro cifre, e (se quella non chiude una
	// parola) di due, come il ritorno indietro di Python su (?:\d{4}|\d{2}).
	reDataAnno4 = regexp.MustCompile(`\A(?:0?[1-9]|[12]\p{Nd}|3[01])` + sp + `[/.-]` + sp + `(?:0?[1-9]|1[0-2])` + sp + `[/.-]` + sp + `\p{Nd}{4}`)
	reDataAnno2 = regexp.MustCompile(`\A(?:0?[1-9]|[12]\p{Nd}|3[01])` + sp + `[/.-]` + sp + `(?:0?[1-9]|1[0-2])` + sp + `[/.-]` + sp + `\p{Nd}{2}`)
	// BUSTA_ANNO_RE senza i \b: «45/26».
	reBustaAnno = regexp.MustCompile(`\A\p{Nd}{1,5}` + sp + `/` + sp + `\p{Nd}{2}`)
	// CIFRE_RE
	reCifre = regexp.MustCompile(`\+?\p{Nd}+`)
	// PRIMA_RE, sulla coda del testo prima del numero già senza gli spazi finali.
	rePrima = regexp.MustCompile(`(?i)(?:busta|acconto|prevent[iİı]vo|numero|n\.|n°)\z`)
	// DOPO_RE senza il \b dopo «eur».
	reDopo = regexp.MustCompile(`(?i)\A` + sp + `(?:euro|€|eur)`)
)

// parolaPython dice se r è un carattere di \w per Python: str.isalnum() o «_».
func parolaPython(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

// spazioPy dice se r è uno spazio per str.isspace().
func spazioPy(r rune) bool {
	switch {
	case r >= '\t' && r <= '\r', r >= 0x1c && r <= ' ', r == 0x85, r == 0xa0, r == 0x1680,
		r >= 0x2000 && r <= 0x200a, r == 0x2028, r == 0x2029, r == 0x202f, r == 0x205f, r == 0x3000:
		return true
	}
	return false
}

// confinePython è \b di Python alla posizione i (in byte) di s.
func confinePython(s string, i int) bool {
	prima, dopo := false, false
	if i > 0 {
		r, _ := utf8.DecodeLastRuneInString(s[:i])
		prima = parolaPython(r)
	}
	if i < len(s) {
		r, _ := utf8.DecodeRuneInString(s[i:])
		dopo = parolaPython(r)
	}
	return prima != dopo
}

// sostituisci è re.sub(\b<forme>\b, " § ", s): da sinistra, alla prima posizione con un confine
// prima e una delle forme (nell'ordine) che finisce su un confine; poi si riparte dopo.
func sostituisci(s string, forme ...*regexp.Regexp) string {
	var b strings.Builder
	ultimo := 0
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		// Tutte le forme cominciano con una cifra: altrove non c'è niente da provare.
		if unicode.Is(unicode.Nd, r) && confinePython(s, i) {
			fine := -1
			for _, re := range forme {
				if m := re.FindStringIndex(s[i:]); m != nil && confinePython(s, i+m[1]) {
					fine = i + m[1]
					break
				}
			}
			if fine >= 0 {
				b.WriteString(s[ultimo:i])
				b.WriteString(" § ")
				i, ultimo = fine, fine
				continue
			}
		}
		i += n
	}
	b.WriteString(s[ultimo:])
	return b.String()
}

// primaIsolato è PRIMA_RE.search(testo[:inizio]): il testo prima del numero finisce con
// busta/acconto/preventivo/numero/«n.»/«n°» e degli spazi. Si guarda solo la coda: la parola
// più lunga ha dieci caratteri, al più 3 byte l'uno.
func primaIsolato(prima string) bool {
	prima = strings.TrimRightFunc(prima, spazioPy)
	if len(prima) > 40 {
		k := len(prima) - 40
		for k < len(prima) && !utf8.RuneStart(prima[k]) {
			k++
		}
		prima = prima[k:]
	}
	return rePrima.MatchString(prima)
}

// dopoIsolato è DOPO_RE.match(testo[fine:]): dopo il numero vengono (spazi e) euro, € o «eur»
// che chiude una parola.
func dopoIsolato(dopo string) bool {
	m := reDopo.FindStringIndex(dopo)
	if m == nil {
		return false
	}
	parola := strings.TrimLeftFunc(dopo[:m[1]], spazioPy)
	if utf8.RuneCountInString(parola) == 3 && parola != "€" {
		// «eur»: vale solo se chiude una parola (eur\b); «euro» è la prima scelta e vale sempre.
		return confinePython(dopo, m[1])
	}
	return true
}

// separatore è SEPARATORE_RE.fullmatch: da 1 a 3 fra spazi, punti, virgole, barre e trattini.
func separatore(s string) bool {
	n := 0
	for _, r := range s {
		if !spazioPy(r) && !strings.ContainsRune(".,/-", r) {
			return false
		}
		n++
	}
	return n >= 1 && n <= 3
}

// TelefonoDalDettato è trova_telefono(testo) di traduttore.py: le cifre del telefono detto nel
// dettato, "" se non ce n'è uno. Le cifre sono quelle di qualsiasi scrittura (\d di Python), senza
// il «+»; «0039» in testa si toglie quando restano più di sei cifre.
func TelefonoDalDettato(testo string) string {
	testo = sostituisci(sostituisci(testo, reDataAnno4, reDataAnno2), reBustaAnno)
	var gruppi [][]string
	var catena []string
	fine := 0
	for _, m := range reCifre.FindAllStringIndex(testo, -1) {
		isolato := primaIsolato(testo[:m[0]]) || dopoIsolato(testo[m[1]:])
		unito := len(catena) > 0 && !isolato && separatore(testo[fine:m[0]])
		if !unito && len(catena) > 0 {
			gruppi = append(gruppi, catena)
			catena = nil
		}
		if isolato {
			gruppi = append(gruppi, []string{testo[m[0]:m[1]]})
		} else {
			catena = append(catena, testo[m[0]:m[1]])
		}
		fine = m[1]
	}
	if len(catena) > 0 {
		gruppi = append(gruppi, catena)
	}
	for _, g := range gruppi {
		cifre := strings.ReplaceAll(strings.Join(g, ""), "+", "") // re.sub(r"\D", "", …): restano le cifre
		n := utf8.RuneCountInString(cifre)
		if strings.HasPrefix(cifre, "0039") && n > 10 {
			cifre, n = cifre[4:], n-4
		}
		if n >= 7 && n <= 13 {
			return cifre
		}
	}
	return ""
}
