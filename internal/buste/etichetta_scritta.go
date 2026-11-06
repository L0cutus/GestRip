package buste

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
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
//     per il numero visibile conta come «senza etichetta».
//
// Un'etichetta già usata da una busta ancora aperta (una busta di carta riusata) non blocca: la
// creazione lo dice una volta (GemelleAperte) e alla seconda conferma salva lo stesso. Le gemelle
// chiuse (consegnate, annullate, chiuse) non contano: è il caso normale della busta riusata.

// EtichettaScritta è il codice da salvare per il testo del campo «Etichetta», con anno l'anno della
// data di accettazione (AnnoDiAccettazione).
func EtichettaScritta(scritto string, anno int) string {
	c := CodiceLetto(scritto)
	if _, _, ok := EtichettaDi(c); ok {
		return c
	}
	e, ok := LeggiRicercaEtichetta(c)
	if !ok {
		return scritto
	}
	a := int64(anno)
	if e.Anno != "" {
		aa, _ := strconv.ParseInt(e.Anno, 10, 64) // due cifre: non fallisce
		a = a - a%100 + aa                        // lo stesso secolo della data di accettazione
	}
	return CodiceEtichetta(a, e.Numero)
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

// righeConCodice sono le buste fuori dal cestino con codice_qr uguale esatto al codice (già senza
// spazi ai bordi), tolta esclusa (0: nessuna), dalla più recente: le buste della scansione
// (lookup_repairs_by_qr/2 confronta il salvato com'è). Con ripulito il salvato si confronta senza
// spazi ai bordi (CodiceLetto, anche Unicode): le gemelle della creazione e della scheda (review della
// 053), così «ABC » salvato è gemello di un altro «ABC ». Le candidate le dà SQL (il codice dentro il
// salvato), la regola esatta è in Go.
func righeConCodice(ctx context.Context, q interrogatore, codice string, esclusa int64, ripulito bool) ([]RigaQR, error) {
	confronto := `b.codice_qr = ?`
	if ripulito {
		confronto = `instr(b.codice_qr, ?) > 0`
	}
	rr, err := q.QueryContext(ctx, `SELECT b.id, b.numero, b.codice_qr, b.nome_cliente, b.data_accettazione,
		b.descrizione, b.stato, b.compro_oro FROM buste b WHERE b.cancellata_il IS NULL AND `+confronto+` AND b.id <> ?
		ORDER BY b.id DESC`, codice, esclusa)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rr.Close() }()
	var out []RigaQR
	for rr.Next() {
		var r RigaQR
		if err := rr.Scan(&r.ID, &r.Numero, &r.CodiceQR, &r.NomeCliente, &r.DataAccettazione, &r.Descrizione, &r.Stato, &r.CompraOro); err != nil {
			return nil, err
		}
		if ripulito && (r.CodiceQR == nil || CodiceLetto(*r.CodiceQR) != codice) {
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

// codiceDellaScheda è il codice della busta come lo cerca la scansione: senza spazi ai bordi, "" se
// non c'è.
func codiceDellaScheda(qr *string) string {
	if qr == nil {
		return ""
	}
	return CodiceLetto(*qr)
}
