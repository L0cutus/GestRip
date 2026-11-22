package buste

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/L0cutus/gestrip/internal/audit"
)

// La raccolta dei dettati dalla pagina «Assistente e dettatura» (slice 114; RIP-H-49, RIP-H-50, ADM-12), come
// Training.status/0, set_collect/2, refill/1, exclude/2 ed exclude_customer/2 dello standalone a go-baseline:
//   - lo stato è fatto di soli numeri: interruttore, buste rimaste nel lotto, righe raccolte, clienti esclusi;
//   - l'interruttore: la prima accensione scrive il lotto di 500 (la chiave assist.training.remaining non c'era),
//     spegnere e riaccendere non lo ricarica, l'accensione a lotto esaurito è rifiutata (ErrLottoEsaurito); spegnere si
//     può sempre. Ogni scatto scrive assist.training_collect con {"on", "remaining"};
//   - il nuovo lotto: solo se il lotto salvato è esattamente «0» (ErrLottoInCorso altrimenti, anche prima del primo
//     lotto), rimette 500 e accende, con assist.training_refill {"on":true,"remaining":500};
//   - l'esclusione di una busta (art. 21): il numero scritto senza spazi ai bordi è quello dell'etichetta (D-109, come la
//     D-89 e la D-95: bustaPerNumeroScelta), il numero interno solo per le buste senza etichetta; cestino compreso; più
//     buste sono sempre il rifiuto RifiutoPiuBuste (senza la scelta dell'anno in corso); cancella il suo dettato e la busta
//     resta.
//     ErrBustaNonTrovata o ErrBustaNonRaccolta senza scrivere niente; audit assist.training_excluded sulla busta, {};
//   - l'esclusione di un cliente: trovato dal numero di una sua busta (anche nel cestino), entra in dettati_esclusioni
//     (una volta sola: la seconda non aggiunge niente) e perde i dettati di tutte le sue buste, cestino compreso; le sue
//     buste future non si raccolgono più (registraNelSavepoint). Audit assist.training_customer_excluded sul cliente con
//     {"cancellati": n}.
//
// Tutto in una transazione: un errore (anche dell'audit) non lascia una cancellazione a metà. Lo standalone cerca la
// busta prima della transazione; qui la ricerca sta dentro, così la busta trovata è quella su cui si cancella.
// Riservatezza: niente di quello che sta in dettati_addestramento passa di qui; gli errori non portano valori.

// LottoRaccolta è @budget di Training: le buste di un lotto della raccolta dei dettati (D6 dello standalone).
const LottoRaccolta = 500

// StatoRaccolta è Training.status/0: solo numeri, mai un dettato.
type StatoRaccolta struct {
	Accesa   bool
	Rimaste  int64 // buste rimaste nel lotto (remaining/0)
	Lotto    int64 // LottoRaccolta
	Raccolti int64 // righe in dettati_addestramento
	Esclusi  int64 // clienti in dettati_esclusioni
}

var (
	// ErrLottoEsaurito è {:error, :budget_exhausted}: l'accensione con il lotto finito.
	ErrLottoEsaurito = errors.New("buste: lotto della raccolta esaurito")
	// ErrLottoInCorso è {:error, :budget_left}: il nuovo lotto mentre quello in corso ha ancora buste.
	ErrLottoInCorso = errors.New("buste: il lotto della raccolta ha ancora buste")
	// ErrBustaNonRaccolta è {:error, :not_collected}: la busta c'è ma non ha un dettato raccolto.
	ErrBustaNonRaccolta = errors.New("buste: la busta non ha un dettato raccolto")
)

// reInteroRaccolta è quello che Integer.parse/1 legge per intero: un segno facoltativo e solo cifre.
var reInteroRaccolta = regexp.MustCompile(`^[+-]?[0-9]+$`)

// rimasteRaccolta è remaining/0: il lotto salvato, 500 se la chiave non c'è; un valore che Integer.parse/1 non legge per
// intero, o negativo, vale 0 (lotto finito). Oltre i 64 bit (lo standalone ha gli interi senza limite) vale il massimo.
func rimasteRaccolta(valore string, presente bool) int64 {
	if !presente {
		return LottoRaccolta
	}
	if !reInteroRaccolta.MatchString(valore) {
		return 0
	}
	n, err := strconv.ParseInt(valore, 10, 64)
	switch {
	case err != nil && valore[0] != '-':
		return math.MaxInt64
	case err != nil, n < 0:
		return 0
	}
	return n
}

func leggiRimaste(ctx context.Context, q lettore) (int64, error) {
	v, ok, err := leggiImpostazione(ctx, q, ChiaveRaccoltaRimaste)
	if err != nil {
		return 0, err
	}
	return rimasteRaccolta(v, ok), nil
}

// StatoRaccolta è Training.status/0.
func (b *Buste) StatoRaccolta(ctx context.Context) (StatoRaccolta, error) {
	s := StatoRaccolta{Lotto: LottoRaccolta}
	accesa, _, err := leggiImpostazione(ctx, b.d.Lettura, ChiaveRaccoltaAccesa)
	if err != nil {
		return StatoRaccolta{}, err
	}
	s.Accesa = accesa == "1"
	if s.Rimaste, err = leggiRimaste(ctx, b.d.Lettura); err != nil {
		return StatoRaccolta{}, err
	}
	if err := b.d.Lettura.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM dettati_addestramento), (SELECT count(*) FROM dettati_esclusioni)`).
		Scan(&s.Raccolti, &s.Esclusi); err != nil {
		return StatoRaccolta{}, err
	}
	return s, nil
}

// ImpostaRaccolta è set_collect/2: accende o spegne la raccolta; ErrLottoEsaurito se si accende a lotto finito.
func (b *Buste) ImpostaRaccolta(ctx context.Context, accesa bool, c audit.Contesto) error {
	return b.inTransazione(ctx, func(tx esecutoreLettore, ora string) error {
		v, presente, err := leggiImpostazione(ctx, tx, ChiaveRaccoltaRimaste)
		if err != nil {
			return err
		}
		rimaste := rimasteRaccolta(v, presente)
		if accesa && rimaste == 0 {
			return ErrLottoEsaurito
		}
		valore := "0"
		if accesa {
			valore = "1"
			if !presente { // la prima accensione scrive il lotto
				if err := b.salvaImpostazione(ctx, tx, ChiaveRaccoltaRimaste, strconv.Itoa(LottoRaccolta), ora); err != nil {
					return err
				}
			}
		}
		if err := b.salvaImpostazione(ctx, tx, ChiaveRaccoltaAccesa, valore, ora); err != nil {
			return err
		}
		return b.reg.Scrivi(ctx, tx, c, "assist.training_collect", "assist", "training", "ok",
			map[string]any{"on": accesa, "remaining": rimaste})
	})
}

// NuovoLottoRaccolta è refill/1: un lotto nuovo di 500 e la raccolta accesa, solo se il lotto salvato è «0».
func (b *Buste) NuovoLottoRaccolta(ctx context.Context, c audit.Contesto) error {
	return b.inTransazione(ctx, func(tx esecutoreLettore, ora string) error {
		v, _, err := leggiImpostazione(ctx, tx, ChiaveRaccoltaRimaste)
		if err != nil {
			return err
		}
		if v != "0" { // Settings.get(@remaining) != "0": anche senza la chiave, prima del primo lotto
			return ErrLottoInCorso
		}
		if err := b.salvaImpostazione(ctx, tx, ChiaveRaccoltaRimaste, strconv.Itoa(LottoRaccolta), ora); err != nil {
			return err
		}
		if err := b.salvaImpostazione(ctx, tx, ChiaveRaccoltaAccesa, "1", ora); err != nil {
			return err
		}
		return b.reg.Scrivi(ctx, tx, c, "assist.training_refill", "assist", "training", "ok",
			map[string]any{"on": true, "remaining": LottoRaccolta})
	})
}

// sempreBuona: per l'esclusione dalla raccolta ogni busta trovata conta, qualunque sia il suo stato.
func sempreBuona(context.Context, interrogatore, int64) (bool, error) { return true, nil }

// bustaDellaRaccolta è la busta del numero scritto (D-109, gemella della D-89 e della D-95): senza spazi ai bordi, il numero
// dell'etichetta («0045/26», «0045», «45»), il numero interno solo per le buste senza etichetta RIP-…, cestino compreso,
// qualunque sia lo stato; più buste sono sempre il rifiuto RifiutoPiuBuste, anche senza l'anno scritto e con una sola
// dell'anno in corso (D-109 aggiornata il 22/11/2026: niente scelta dell'anno, che la consegna e il lotto invece fanno). Dà id, cliente e numero
// visibile (NumeroVisibile: «0045/26» o «N. 45/26»), o ErrBustaNonTrovata.
func (b *Buste) bustaDellaRaccolta(ctx context.Context, tx *sql.Tx, numero string) (id, cliente int64, visibile string, err error) {
	scritto := strings.TrimFunc(numero, unicode.IsSpace)
	id, err = b.bustaPerNumeroScelta(ctx, tx, scritto, sempreBuona, true, false)
	switch {
	case errors.Is(err, ErrPiuBuste):
		return 0, 0, "", &RifiutoPiuBuste{Codice: scritto}
	case err != nil:
		return 0, 0, "", err
	case id == 0:
		return 0, 0, "", ErrBustaNonTrovata
	}
	var n string
	var qr sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT cliente_id, numero, codice_qr FROM buste WHERE id = ?`, id).Scan(&cliente, &n, &qr); err != nil {
		return 0, 0, "", err
	}
	visibile, _ = NumeroVisibile(qr.String, n)
	return id, cliente, visibile, nil
}

// EscludiBustaDallaRaccolta è exclude/2: cancella il dettato della busta con quel numero (D-109); la busta resta. Dà il
// numero visibile della busta trovata, anche con ErrBustaNonRaccolta.
func (b *Buste) EscludiBustaDallaRaccolta(ctx context.Context, numero string, c audit.Contesto) (string, error) {
	var visibile string
	err := b.inTransazioneTx(ctx, func(tx *sql.Tx, _ string) error {
		id, _, v, err := b.bustaDellaRaccolta(ctx, tx, numero)
		if err != nil {
			return err
		}
		visibile = v
		r, err := tx.ExecContext(ctx, `DELETE FROM dettati_addestramento WHERE busta_id = ?`, id)
		if err != nil {
			return err
		}
		if n, err := r.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return ErrBustaNonRaccolta
		}
		return b.reg.Scrivi(ctx, tx, c, "assist.training_excluded", "repair", id, "ok", nil)
	})
	if err != nil && !errors.Is(err, ErrBustaNonRaccolta) {
		visibile = ""
	}
	return visibile, err
}

// EscludiClienteDallaRaccolta è exclude_customer/2: il cliente della busta con quel numero (D-109) si oppone; dà il numero
// visibile della busta trovata e quanti dettati sono stati cancellati.
func (b *Buste) EscludiClienteDallaRaccolta(ctx context.Context, numero string, c audit.Contesto) (string, int64, error) {
	var visibile string
	var cancellati int64
	err := b.inTransazioneTx(ctx, func(tx *sql.Tx, ora string) error {
		_, cliente, v, err := b.bustaDellaRaccolta(ctx, tx, numero)
		if err != nil {
			return err
		}
		visibile = v
		if _, err := tx.ExecContext(ctx, `INSERT INTO dettati_esclusioni (cliente_id, creato_il) VALUES (?, ?) ON CONFLICT (cliente_id) DO NOTHING`,
			cliente, ora); err != nil {
			return err
		}
		r, err := tx.ExecContext(ctx, `DELETE FROM dettati_addestramento WHERE busta_id IN (SELECT id FROM buste WHERE cliente_id = ?)`, cliente)
		if err != nil {
			return err
		}
		if cancellati, err = r.RowsAffected(); err != nil {
			return err
		}
		return b.reg.Scrivi(ctx, tx, c, "assist.training_customer_excluded", "customer", cliente, "ok",
			map[string]any{"cancellati": cancellati})
	})
	if err != nil {
		return "", 0, err
	}
	return visibile, cancellati, nil
}
