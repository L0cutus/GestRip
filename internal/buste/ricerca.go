package buste

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/L0cutus/gestrip/internal/audit"
)

// La ricerca della home dopo D-63 e D-64 (slice 052; Stefano 03/11/2026 e risposte del 05/11/2026
// alle domande della T4.2):
//   - un numero d'etichetta scritto a mano («0045/26», «045/26», «0045», «45»: da 1 a 4 cifre, con o
//     senza «/AA») non è un «contiene»: dà tutte le buste che mostrano quel numero (NumeroVisibile),
//     cioè le etichette RIP-AAAA-NNNN con quel numero (di quell'anno, o di tutti gli anni senza
//     «/AA») e, solo per le buste senza etichetta, il numero interno N/AA. Chiuse e consegnate
//     comprese, e senza nessun altro filtro: Filtri.Effettivi li toglie (risposta 4);
//   - ogni altro testo resta il «contiene» di search/2 (ModelloRicerca), con i filtri;
//   - la scansione (CercaPerQR) è Workshop.lookup_repairs_by_qr/2: lo stesso codice (dalla slice 055d
//     normalizzato da tutte e due le parti, NormalizzaCodice), dalla più recente, con la riga
//     repair.qr_lookup che dice quante e quali, mai il codice.

// reCercaEtichetta è la forma di un numero d'etichetta scritto (risposta 3): da 1 a 4 cifre ASCII, e
// facoltativi la barra e l'anno a 2 cifre.
var reCercaEtichetta = regexp.MustCompile(`^([0-9]{1,4})(?:/([0-9]{2}))?$`)

// RicercaEtichetta è un numero d'etichetta cercato: il numero (da 1) e, se scritto, l'anno a 2 cifre.
type RicercaEtichetta struct {
	Numero int64
	Anno   string // "" tutti gli anni, altrimenti le due cifre scritte («26»)
}

// LeggiRicercaEtichetta dice se il testo cercato (già senza spazi ai bordi) è un numero d'etichetta
// (D-64): «0045/26», «45/26», «0045», «45», «012/26». «0», «0000/26», «00045», «45/2026», «#45» e
// «12/26 » con lo spazio dentro non lo sono e restano un «contiene».
func LeggiRicercaEtichetta(cerca string) (RicercaEtichetta, bool) {
	m := reCercaEtichetta.FindStringSubmatch(cerca)
	if m == nil {
		return RicercaEtichetta{}, false
	}
	n, _ := strconv.ParseInt(m[1], 10, 64) // al più 4 cifre: non fallisce
	if n < 1 {
		return RicercaEtichetta{}, false
	}
	return RicercaEtichetta{Numero: n, Anno: m[2]}, true
}

// Trova dice se una busta con quel numero interno e quel codice QR mostra il numero cercato: il
// numero dell'etichetta («0045/26») se ne ha una, altrimenti il numero interno senza gli zeri davanti
// (NormalizzaNumero: «045/26» è «45/26»). Numero e anno si confrontano come li mostra NumeroVisibile.
func (e RicercaEtichetta) Trova(numero string, codiceQR *string) bool {
	qr := ""
	if codiceQR != nil {
		qr = *codiceQR
	}
	testo, etichetta := NumeroVisibile(qr, numero)
	if !etichetta {
		testo = NormalizzaNumero(numero)
	}
	n, anno, ok := strings.Cut(testo, "/")
	if !ok || strings.Trim(n, "0123456789") != "" || n == "" {
		return false
	}
	v, err := strconv.ParseInt(n, 10, 64)
	if err != nil || v != e.Numero {
		return false
	}
	return e.Anno == "" || anno == e.Anno
}

// Etichetta dice se la ricerca dei filtri è per numero d'etichetta.
func (f Filtri) Etichetta() (RicercaEtichetta, bool) { return LeggiRicercaEtichetta(f.Cerca) }

// Effettivi sono i filtri che la lista applica davvero: con una ricerca per numero d'etichetta
// solo la ricerca e la pagina (D-64, risposta 4: la ricerca per etichetta ignora tutti i filtri,
// anche il nascondimento di pronte e consegnate), e la vista, che non è un filtro (slice 057);
// altrimenti tutti.
func (f Filtri) Effettivi() Filtri {
	if _, ok := f.Etichetta(); !ok {
		return f
	}
	return Filtri{Cerca: f.Cerca, DataCampo: CampoDataPredefinito, Pagina: f.Pagina, Galleria: f.Galleria}
}

// busteDellEtichetta sono gli id delle buste fuori dal cestino che mostrano il numero cercato. Le
// candidate le sceglie SQL (un codice con le cifre «NNNN» dentro, che NormalizzaCodice non separa mai,
// o un numero interno con «N/» dentro); la regola esatta è Trova, la stessa di NumeroVisibile. Review
// della 055d: senza il trattino, perché fra trattino e cifre il salvato può avere uno spazio Unicode
// o un tab, che NormalizzaCodice toglie.
func (b *Buste) busteDellEtichetta(ctx context.Context, e RicercaEtichetta) ([]int64, error) {
	righe, err := b.d.Lettura.QueryContext(ctx, `SELECT b.id, b.numero, b.codice_qr FROM buste b
		WHERE b.cancellata_il IS NULL AND (b.codice_qr LIKE ? OR b.numero LIKE ?)`,
		"%"+Quattro(e.Numero)+"%", "%"+strconv.FormatInt(e.Numero, 10)+"/%")
	if err != nil {
		return nil, err
	}
	defer func() { _ = righe.Close() }()
	ids := []int64{}
	for righe.Next() {
		var id int64
		var numero string
		var qr *string
		if err := righe.Scan(&id, &numero, &qr); err != nil {
			return nil, err
		}
		if e.Trova(numero, qr) {
			ids = append(ids, id)
		}
	}
	return ids, righe.Err()
}

// Grafemi sono i primi n grafemi di s (String.slice/3 da 0).
func Grafemi(s string, n int) []string { return grafemi(s, n) }

// RigaQR è una busta trovata dalla scansione, con quello che la scelta fra le gemelle e il pannello
// mostrano (qr-match-<id>, qr-action).
type RigaQR struct {
	ID               int64
	Numero           string
	CodiceQR         *string
	NomeCliente      string
	DataAccettazione string
	Descrizione      string
	Stato            string
	CompraOro        bool
	FuoriBottega     bool // consegnata all'artigiano e non ancora rientrata (Repair.out_of_shop?/1)
}

// colonneRigaQR sono le colonne di una RigaQR, nell'ordine di scandisciRigaQR.
const colonneRigaQR = `b.id, b.numero, b.codice_qr, b.nome_cliente, b.data_accettazione, b.descrizione, b.stato, b.compro_oro,
	` + fuoriBottegaSQL

// scandisciRigaQR legge una riga di colonneRigaQR.
func scandisciRigaQR(s interface{ Scan(...any) error }) (RigaQR, error) {
	var r RigaQR
	err := s.Scan(&r.ID, &r.Numero, &r.CodiceQR, &r.NomeCliente, &r.DataAccettazione, &r.Descrizione, &r.Stato, &r.CompraOro, &r.FuoriBottega)
	return r, err
}

// Aperta dice se la busta non è chiusa (Repair.closed_statuses/0).
func (r RigaQR) Aperta() bool { return !Chiuso(r.Stato) }

// NumeroVisibile è il numero da mostrare (D-64) e se è quello di un'etichetta.
func (r RigaQR) NumeroVisibile() (string, bool) {
	qr := ""
	if r.CodiceQR != nil {
		qr = *r.CodiceQR
	}
	return NumeroVisibile(qr, r.Numero)
}

// ErrQRVuoto è il codice vuoto o di soli spazi: «QR non fornito», senza lettura né riga.
var ErrQRVuoto = errors.New("buste: QR non fornito")

// CodiceLetto è qr_query/1: il codice senza gli spazi ai bordi (String.trim/1); "" se non c'è.
func CodiceLetto(codice string) string { return strings.TrimFunc(codice, unicode.IsSpace) }

// CercaPerQR è Workshop.lookup_repairs_by_qr/2: le buste fuori dal cestino con lo stesso codice
// del codice letto, dalla più recente (id a scendere). Dalla slice 055d i due codici si confrontano
// normalizzati (NormalizzaCodice: spazi tolti, «rip-» maiuscolo), anche il salvato; lo standalone
// toglie gli spazi ai bordi solo dal codice letto (D-64). Scrive repair.qr_lookup con il numero e gli id trovati, mai il codice, se ha trovato
// qualcosa o se ancheNessuna (la scansione col 📷, via=qr); un testo scritto che non è il codice di
// nessuna busta è una ricerca testuale e non scrive niente. Se la riga non si scrive, l'errore e
// nessuna busta («Ricerca non riuscita.»).
func (b *Buste) CercaPerQR(ctx context.Context, codice string, ancheNessuna bool, c audit.Contesto) ([]RigaQR, error) {
	codice = NormalizzaCodice(codice)
	if codice == "" {
		return nil, ErrQRVuoto
	}
	out, err := righeConCodice(ctx, b.d.Lettura, codice, 0)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for _, r := range out {
		ids = append(ids, r.ID)
	}
	if len(out) == 0 && !ancheNessuna {
		return nil, nil
	}
	if err := b.reg.Registra(ctx, c, "repair.qr_lookup", "repair_index", "qr", "ok", map[string]any{
		"matches": len(ids), "repair_ids": ids,
	}); err != nil {
		return nil, fmt.Errorf("buste: repair.qr_lookup non scritta: %w", err)
	}
	return out, nil
}

// UnicaDaAprire è la busta della scansione che si apre senza scelta: l'unica trovata, o l'unica
// aperta fra le gemelle (D-64, cambia RIP-H-22: una busta di carta riusata dopo il ritiro ha la
// gemella consegnata). Con più gemelle aperte, o nessuna aperta, si sceglie: false.
func UnicaDaAprire(righe []RigaQR) (RigaQR, bool) {
	if len(righe) == 1 {
		return righe[0], true
	}
	var aperta RigaQR
	n := 0
	for _, r := range righe {
		if r.Aperta() {
			aperta, n = r, n+1
		}
	}
	return aperta, n == 1
}
