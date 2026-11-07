package buste

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Il campo «Etichetta (0045/26)» di «Nuova riparazione» e della modifica (D-64, slice 053; studio in
// docs/studio-D64-numero-etichetta.md, punto (d), e risposte di Stefano del 03/11/2026):
//   - il codice intero letto dal 📷 o dalla pistola («RIP-2026-0045», esattamente la forma di
//     CodiceEtichetta, spazi ai bordi tolti) resta quello;
//   - un numero d'etichetta scritto a mano, nelle forme della ricerca (LeggiRicercaEtichetta: da 1 a
//     4 cifre, con o senza «/AA»: «0045/26», «45/26», «0045», «45»), diventa il codice dell'etichetta:
//     l'anno è quello scritto, se c'è, altrimenti quello della data di accettazione;
//   - ogni altro testo (un codice di altro formato, «RIP-2026-45», «00045») resta com'è, come oggi:
//     per il numero visibile conta come «senza etichetta»;
//   - dalla slice 055d (Stefano 06/11/2026): «rip-» minuscolo e gli spazi accanto ai separatori si
//     tolgono quando il risultato è un'etichetta (NormalizzaCodice), gli spazi ai bordi da ogni codice;
//     un testo che somiglia a un'etichetta senza esserlo («45/2026») si avvisa una volta
//     (FormaNonRiconosciuta) e poi si salva com'è.
//
// Un'etichetta già usata da una busta ancora aperta (una busta di carta riusata) non blocca: la
// creazione lo dice una volta (GemelleAperte) e alla seconda conferma salva lo stesso. Le gemelle
// chiuse (consegnate, annullate, chiuse) non contano: è il caso normale della busta riusata.

// EtichettaScritta è il codice da salvare per il testo del campo «Etichetta», con anno l'anno della
// data di accettazione (AnnoDiAccettazione). Dalla slice 055d gli spazi si tolgono da ogni codice:
// ai bordi sempre, accanto ai trattini e alla barra solo se così il testo diventa un'etichetta
// («rip-2026- 0045» → «RIP-2026-0045», NormalizzaCodice) o un numero d'etichetta («0045 / 26» →
// «0045/26»); «4 5» resta «4 5» (e avvisa, SembraEtichetta).
func EtichettaScritta(scritto string, anno int) string {
	c := NormalizzaCodice(scritto)
	if _, _, ok := EtichettaDi(c); ok {
		return c
	}
	e, ok := LeggiRicercaEtichetta(spaziAiSeparatori(c))
	if !ok {
		return c
	}
	a := int64(anno)
	if e.Anno != "" {
		aa, _ := strconv.ParseInt(e.Anno, 10, 64) // due cifre: non fallisce
		a = a - a%100 + aa                        // lo stesso secolo della data di accettazione
	}
	return CodiceEtichetta(a, e.Numero)
}

// reQuasiNumero sono i numeri scritti che sembrano un'etichetta ma non hanno una forma riconosciuta
// (slice 055d): «45/2026», «0045-26», «45.26», «#45», «00045», «0», «0000/26».
var reQuasiNumero = regexp.MustCompile(`^#?[0-9]{1,6}(?:[-/._][0-9]{2}|[-/._][0-9]{4})?$`)

// SembraEtichetta dice se il codice da salvare (già passato da EtichettaScritta) somiglia a
// un'etichetta senza esserlo: comincia con «RIP» («RIP-2026-45», «RIP-26-0045», «RIP 2026 0045»),
// o è un numero di al più 6 cifre con o senza anno in un'altra forma (reQuasiNumero). Sono gli errori
// di battitura per cui il modulo avvisa una volta (FormaNonRiconosciuta). Un'etichetta vera, un
// codice vuoto o un codice di altro formato («ABC-12», 13 cifre di un codice a barre) no.
func SembraEtichetta(codice string) bool {
	if _, _, ok := EtichettaDi(codice); ok {
		return false
	}
	if _, ok := LeggiRicercaEtichetta(CodiceLetto(codice)); ok {
		return false // un numero scritto come la ricerca: EtichettaScritta lo risolve, qui non arriva
	}
	c := senzaSpazi(codice) // per riconoscere la somiglianza gli spazi non contano: «4 5», «RIP 2026 0045»
	if c == "" {
		return false
	}
	return (len(c) >= 3 && strings.EqualFold(c[:3], "RIP")) || reQuasiNumero.MatchString(c)
}

// FormaNonRiconosciuta è l'avviso della creazione e della modifica (slice 055d, Stefano 06/11/2026):
// il codice da salvare somiglia a un'etichetta ma non ha una forma riconosciuta (SembraEtichetta). Il
// modulo lo mostra e, rimandato con lo stesso codice (FormaConfermata), salva com'è.
type FormaNonRiconosciuta struct {
	Codice string // il codice com'è da salvare
}

func (f *FormaNonRiconosciuta) Error() string {
	return "buste: codice simile a un'etichetta, non riconosciuto"
}

// formaDaAvvisare dice se per il codice da salvare va dato l'avviso della forma: somiglia a
// un'etichetta e non è quello già confermato dal modulo.
func formaDaAvvisare(codice, confermato string) bool {
	return SembraEtichetta(codice) && NormalizzaCodice(confermato) != NormalizzaCodice(codice)
}

// EtichettaNelModulo è il valore del campo «Etichetta» nella modifica (D-64, studio punto (d): «si
// cambia come oggi il codice QR, mostrata come 0045/26»): il numero dell'etichetta «0045/26» solo se,
// rimandato così, EtichettaScritta ridà esattamente il codice salvato (con l'anno della data di
// accettazione della busta); altrimenti il codice com'è («RIP-2026-10000», un codice con gli spazi,
// uno di altro formato).
func EtichettaNelModulo(codice, dataAccettazione string) string {
	anno, n, ok := EtichettaDi(codice)
	if !ok || !dataVera(dataAccettazione) {
		return codice
	}
	v := NumeroEtichetta(anno, n)
	if EtichettaScritta(v, annoDiAccettazione(dataAccettazione, time.Time{})) != codice {
		return codice
	}
	return v
}

// AnnoDiAccettazione è l'anno della data di accettazione scritta (AAAA-MM-GG), se è una data;
// altrimenti quello di oggi in negozio (t): è l'anno che prende anche il numero interno.
func AnnoDiAccettazione(data string, t time.Time) int { return annoDiAccettazione(data, t) }

// GemelleAperte è l'avviso della creazione: il codice (già risolto, quello da salvare) è già sulle
// buste aperte Righe, dalla più recente. Il modulo lo mostra e, rimandato con lo stesso codice in
// Accettazione.GemelleConfermate, salva.
type GemelleAperte struct {
	Codice string
	Righe  []RigaQR
}

func (g *GemelleAperte) Error() string {
	return fmt.Sprintf("buste: etichetta già usata da %d buste aperte", len(g.Righe))
}

// interrogatore è chi legge le righe: il database o la transazione della creazione.
type interrogatore interface {
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
}

// righeConCodice sono le buste fuori dal cestino con lo stesso codice QR, tolta esclusa (0: nessuna),
// dalla più recente: le buste della scansione (lookup_repairs_by_qr/2) e le gemelle della creazione e
// della scheda. Dalla slice 055d (Stefano 06/11/2026: spazi tolti da ogni codice, «rip-» minuscolo
// riconosciuto) si confrontano i codici normalizzati da tutte e due le parti (NormalizzaCodice): un
// « RIP-2026-0045 » o un «rip-2026-0045» importati sono la stessa etichetta; lo standalone confronta
// il salvato com'è (D-64). Le candidate le dà SQL, la regola esatta è in Go: per un'etichetta i salvati
// con dentro l'anno e il numero (NormalizzaCodice toglie solo spazi accanto ai trattini, anche Unicode,
// e cambia solo le maiuscole di «rip», quindi anno e numero restano interi nel salvato); per ogni altro
// codice i salvati con dentro il codice (tolti solo gli spazi ai bordi). Review della 055d: la
// candidata di prima (il salvato senza spazi ASCII, in maiuscolo) perdeva un salvato con uno spazio
// Unicode o un tab accanto a un trattino, che NormalizzaCodice riconosce.
func righeConCodice(ctx context.Context, q interrogatore, codice string, esclusa int64) ([]RigaQR, error) {
	codice = NormalizzaCodice(codice)
	confronto, argomenti := `instr(b.codice_qr, ?) > 0`, []any{codice}
	if _, _, ok := EtichettaDi(codice); ok {
		parti := strings.Split(codice, "-") // «RIP», anno, numero: la forma di CodiceEtichetta
		confronto, argomenti = `instr(b.codice_qr, ?) > 0 AND instr(b.codice_qr, ?) > 0`, []any{parti[1], parti[2]}
	}
	rr, err := q.QueryContext(ctx, `SELECT `+colonneRigaQR+` FROM buste b WHERE b.cancellata_il IS NULL
		AND `+confronto+` AND b.id <> ? ORDER BY b.id DESC`, append(argomenti, esclusa)...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rr.Close() }()
	var out []RigaQR
	for rr.Next() {
		r, err := scandisciRigaQR(rr)
		if err != nil {
			return nil, err
		}
		if r.CodiceQR == nil || NormalizzaCodice(*r.CodiceQR) != codice {
			continue
		}
		out = append(out, r)
	}
	return out, rr.Err()
}

// aperte sono le righe delle buste non chiuse.
func aperte(rr []RigaQR) []RigaQR {
	var out []RigaQR
	for _, r := range rr {
		if r.Aperta() {
			out = append(out, r)
		}
	}
	return out
}

// GemelleAperte sono le altre buste aperte con la stessa etichetta (la riga «Un'altra busta ha la
// stessa etichetta» della scheda).
func (s Scheda) GemelleAperte() []RigaQR { return aperte(s.Gemelle) }

// GemelleChiuse dice se un'altra busta chiusa ha la stessa etichetta (il link «storico di questa
// etichetta» della scheda).
func (s Scheda) GemelleChiuse() bool { return len(s.Gemelle) > len(s.GemelleAperte()) }

// NumeroVisibile è il numero da mostrare (D-64) e se è quello di un'etichetta.
func (s Scheda) NumeroVisibile() (string, bool) {
	qr := ""
	if s.CodiceQR != nil {
		qr = *s.CodiceQR
	}
	return NumeroVisibile(qr, s.Numero)
}

// codiceDellaScheda è il codice della busta come lo cerca la scansione (NormalizzaCodice), "" se non
// c'è.
func codiceDellaScheda(qr *string) string {
	if qr == nil {
		return ""
	}
	return NormalizzaCodice(*qr)
}
