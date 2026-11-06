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
// dalla 053; PDF, etichette e testi per il cliente la useranno nelle loro slice (058, T4.5–T4.8).

// reEtichetta è la forma di CodiceEtichetta: «RIP-», l'anno a quattro cifre, «-», il numero con
// almeno quattro cifre (Quattro). Solo cifre ASCII.
var reEtichetta = regexp.MustCompile(`^RIP-([0-9]{4})-([0-9]{4,18})$`)

// EtichettaDi legge il codice QR come un'etichetta prenumerata: anno e numero, se il codice (senza
// spazi ai bordi, come la ricerca del QR) è esattamente quello che CodiceEtichetta scriverebbe,
// con il numero da 1 in su. «RIP-2026-45», «RIP-2026-00045», «rip-2026-0045» e «RIP-2026-0000»
// non sono etichette.
func EtichettaDi(codiceQR string) (anno, n int64, ok bool) {
	c := strings.TrimFunc(codiceQR, unicode.IsSpace)
	m := reEtichetta.FindStringSubmatch(c)
	if m == nil {
		return 0, 0, false
	}
	anno, _ = strconv.ParseInt(m[1], 10, 64) // quattro cifre: non fallisce
	n, err := strconv.ParseInt(m[2], 10, 64)
	if err != nil || n < 1 || CodiceEtichetta(anno, n) != c {
		return 0, 0, false
	}
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

// QRMancante è Repair.blank_qr?/1: nessun codice, vuoto o di soli spazi (String.trim/1).
func QRMancante(codiceQR *string) bool {
	return codiceQR == nil || strings.TrimFunc(*codiceQR, unicode.IsSpace) == ""
}

// SenzaQR è Repair.missing_qr?/1 (RIP-H-56): una busta aperta senza codice QR, quella che porta il
// segno «⚠ senza QR». Una busta chiusa non lo porta.
func SenzaQR(stato string, codiceQR *string) bool {
	return !Chiuso(stato) && QRMancante(codiceQR)
}
