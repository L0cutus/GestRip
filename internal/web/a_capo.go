package web

import "strings"

// campiConRighe sono le caselle di più righe dei moduli della modifica e di «Nuova riparazione»: nello
// standalone sono di LiveView (slice 068).
var campiConRighe = map[string]bool{"description": true, "public_notes": true, "internal_notes": true}

// aCapoUnix scrive le righe di una casella come «\n» (slice 068, Dubbio 3 della 067). Senza JavaScript
// il browser manda il testo di una <textarea> con «\r\n»; LiveView manda allo standalone il valore della
// casella, che ha «\n». Si usa per le caselle che nello standalone sono di LiveView (la risposta della
// scheda, descrizione e note della modifica e di «Nuova riparazione», il popup dell'avviso, i testi dei
// messaggi), mai per quelle della pagina pubblica: lì lo standalone riceve un modulo vero, con «\r\n».
// Un «\r» da solo non viene da una casella e resta com'è.
func aCapoUnix(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }
