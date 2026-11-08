package buste

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Il numero visibile della busta (D-64, Stefano 03/11/2026; studio in
// docs/studio-D64-numero-etichetta.md): quello dell'etichetta QR prenumerata, «0045/26», con l'anno
// dell'etichetta («RIP-2025-0045» su una busta del 2026 è «0045/25»). Una busta senza un'etichetta
// RIP-AAAA-NNNN (QR mancante, vecchio, del Flask o di un altro formato) mostra il numero interno
// come «N. 45/26», che non si confonde con un'etichetta. Il numero interno N/AA resta il dato
// tecnico (registro, link, ordine della lista).
//
// È la regola unica: la lista della home la usa dalla slice 050, la scheda e «Nuova riparazione»
// dalla 053, i PDF (ricevuta, etichetta 76×127, scheda PDF, report) e la pagina della firma dalla
// 058; i testi per il cliente la useranno nelle loro slice (T4.5–T4.8).

// reEtichetta è la forma di CodiceEtichetta: «RIP-», l'anno a quattro cifre, «-», il numero con
// almeno quattro cifre (Quattro). Solo cifre ASCII.
var reEtichetta = regexp.MustCompile(`^RIP-([0-9]{4})-([0-9]{4,18})$`)

// NormalizzaCodice è il codice QR come si salva e si confronta (slice 055d, decisione di Stefano del
// 06/11/2026: «gli spazi si tolgono da ogni codice», il prefisso «rip-» minuscolo si riconosce):
//   - gli spazi ai bordi si tolgono sempre (CodiceLetto, anche quelli Unicode);
//   - se, togliendo anche gli spazi accanto ai trattini e scrivendo «RIP-» in maiuscolo, il codice
//     diventa esattamente un'etichetta (la forma di CodiceEtichetta), vale quella: «rip-2026-0045»,
//     «RIP - 2026 - 0045», «rip-2026- 0045» sono «RIP-2026-0045»;
//   - altrimenti resta com'è, con le maiuscole e gli spazi dentro: «ABC 12» e «abc» di un collega non
//     si toccano, e nemmeno «RIP 2026 0045» (senza trattini), «RIP-2026-00 45» o «RIP-2026-45».
//
// È la regola prudente: dentro un codice si tolgono solo gli spazi accanto a un separatore («-», e «/»
// per i numeri scritti, EtichettaScritta), e solo quando il risultato è un'etichetta riconosciuta;
// uno spazio fra due cifre non si toglie mai, perché «4 5» non è per forza «45».
func NormalizzaCodice(codice string) string {
	c := CodiceLetto(codice)
	if e, ok := etichettaCompatta(c); ok {
		return e
	}
	return c
}

// reSpaziSeparatore sono gli spazi (anche Unicode) intorno a un trattino o a una barra.
var reSpaziSeparatore = regexp.MustCompile(`[\s\p{Z}]*([-/])[\s\p{Z}]*`)

// spaziAiSeparatori è s senza gli spazi accanto ai trattini e alle barre.
func spaziAiSeparatori(s string) string { return reSpaziSeparatore.ReplaceAllString(s, "$1") }

// etichettaCompatta è il codice senza gli spazi accanto ai trattini e con «RIP-» maiuscolo, se così è
// esattamente un'etichetta (numero da 1 in su).
func etichettaCompatta(c string) (string, bool) {
	k := spaziAiSeparatori(c)
	if len(k) < 4 || !strings.EqualFold(k[:4], "RIP-") {
		return "", false
	}
	k = "RIP-" + k[4:]
	m := reEtichetta.FindStringSubmatch(k)
	if m == nil {
		return "", false
	}
	anno, _ := strconv.ParseInt(m[1], 10, 64) // quattro cifre: non fallisce
	n, err := strconv.ParseInt(m[2], 10, 64)
	if err != nil || n < 1 || CodiceEtichetta(anno, n) != k {
		return "", false
	}
	return k, true
}

// senzaSpazi è s senza nessuno spazio (anche Unicode): solo per riconoscere le forme simili a
// un'etichetta (SembraEtichetta), mai per il codice da salvare.
func senzaSpazi(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.Is(unicode.Z, r) {
			return -1
		}
		return r
	}, s)
}

// EtichettaDi legge il codice QR come un'etichetta prenumerata: anno e numero, se il codice
// normalizzato (NormalizzaCodice: spazi ai bordi e accanto ai trattini tolti, «rip-» in maiuscolo) è
// esattamente quello che CodiceEtichetta scriverebbe, con il numero da 1 in su. «rip-2026-0045» e
// « RIP-2026- 0045 » sono etichette (slice 055d); «RIP-2026-45», «RIP-2026-00045», «RIP-2026-0000»
// e «RIP 2026 0045» no.
func EtichettaDi(codiceQR string) (anno, n int64, ok bool) {
	k, ok := etichettaCompatta(CodiceLetto(codiceQR))
	if !ok {
		return 0, 0, false
	}
	m := reEtichetta.FindStringSubmatch(k)
	anno, _ = strconv.ParseInt(m[1], 10, 64)
	n, _ = strconv.ParseInt(m[2], 10, 64)
	return anno, n, true
}

// NumeroVisibile è il numero da mostrare per una busta con quel codice QR e quel numero interno:
// «0045/26» con un'etichetta (NumeroEtichetta), altrimenti «N. 45/26». etichetta dice quale dei due.
func NumeroVisibile(codiceQR, numero string) (testo string, etichetta bool) {
	if anno, n, ok := EtichettaDi(codiceQR); ok {
		return NumeroEtichetta(anno, n), true
	}
	return "N. " + numero, false
}

// Mostrato è il numero visibile di una busta (D-64) già scelto per un documento o una pagina:
// Testo è «0045/26» o «N. 45/26», Etichetta dice se è quello di un'etichetta. Lo portano i dati dei
// PDF (ricevuta, etichetta 76×127, scheda PDF, report) e della pagina della firma (slice 058), così
// l'impaginazione non rifà la regola.
type Mostrato struct {
	Testo     string
	Etichetta bool
}

// NumeroMostrato è NumeroVisibile come Mostrato.
func NumeroMostrato(codiceQR, numero string) Mostrato {
	t, e := NumeroVisibile(codiceQR, numero)
	return Mostrato{Testo: t, Etichetta: e}
}

// Titolo è il numero dove lo standalone scrive «#N/AA» (titoli, intestazioni, il numero grande
// dell'etichetta e del report): «#0045/26» con un'etichetta, «N. 45/26» senza, mai «#N. 45/26».
func (m Mostrato) Titolo() string {
	if m.Etichetta {
		return "#" + m.Testo
	}
	return m.Testo
}

// QRMancante è Repair.blank_qr?/1: nessun codice, vuoto o di soli spazi (String.trim/1).
func QRMancante(codiceQR *string) bool {
	return codiceQR == nil || strings.TrimFunc(*codiceQR, unicode.IsSpace) == ""
}

// SenzaQR è Repair.missing_qr?/1 (RIP-H-56): una busta aperta senza codice QR, quella che porta il
// segno «⚠ senza QR». Una busta chiusa non lo porta.
func SenzaQR(stato string, codiceQR *string) bool {
	return !Chiuso(stato) && QRMancante(codiceQR)
}
