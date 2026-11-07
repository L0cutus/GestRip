package buste

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/audit"
)

// Il contatore rimasto indietro (slice 055a, decisione di Stefano del 06/11/2026). Dalla 053 il
// numero interno di una busta nuova lo dà solo sequenze_buste; se il contatore di un anno è dietro a
// un numero già usato (un dato vecchio, o scritto a mano nel database) la creazione trova il numero
// occupato. Allora il contatore va al massimo dei numeri già usati di quell'anno e la creazione
// riprova una volta. Solo del Go: lo standalone non ce l'ha.

// numeroLetto è la lettura larga di un numero salvato per il massimo dell'anno: spazi ai bordi
// tolti, zeri davanti ignorati, al più cinque cifre che contano (il formato del contatore), la
// barra e due cifre dell'anno. «0045/26», « 45/26 » e «45/26» valgono tutti 45; «45/2026»,
// «45-26», «A45/26» e «123456/26» non sono numeri dell'anno e non contano.
var numeroLetto = regexp.MustCompile(`^0*(\d{1,5})/(\d{2})$`)

// numeroDellAnno legge un numero salvato: il suo valore, se è un numero dell'anno a due cifre aa.
func numeroDellAnno(salvato string, aa int) (int64, bool) {
	m := numeroLetto.FindStringSubmatch(strings.TrimSpace(salvato))
	if m == nil {
		return 0, false
	}
	if a, _ := strconv.Atoi(m[2]); a != aa {
		return 0, false
	}
	n, _ := strconv.ParseInt(m[1], 10, 64) // al più cinque cifre: non fallisce
	return n, true
}

// massimoDellAnno è il numero più alto già usato dalle buste dell'anno, cestino compreso (l'indice
// unico di buste.numero copre anche il cestino); 0 senza nessun numero leggibile. I numeri fuori
// formato non contano (numeroDellAnno).
func massimoDellAnno(ctx context.Context, tx *sql.Tx, anno int) (int64, error) {
	aa := anno % 100
	righe, err := tx.QueryContext(ctx, `SELECT numero FROM buste WHERE numero LIKE ?`, fmt.Sprintf("%%/%02d%%", aa))
	if err != nil {
		return 0, err
	}
	defer func() { _ = righe.Close() }()
	var massimo int64
	for righe.Next() {
		var s string
		if err := righe.Scan(&s); err != nil {
			return 0, err
		}
		if n, ok := numeroDellAnno(s, aa); ok && n > massimo {
			massimo = n
		}
	}
	return massimo, righe.Err()
}

// riallineaContatore porta il contatore dell'anno al massimo dei numeri già usati, dopo che il
// numero automatico tentato (il prossimo del contatore) è risultato già usato, e dà il prossimo
// numero, da riprovare una volta. Nella transazione della creazione, con la riga d'audit
// bag_sequence.realigned (anno, valore di prima e valore nuovo); se la creazione poi si rifiuta,
// la transazione annulla anche questo. Il contatore non scende mai. ok è false quando non c'è
// niente da riallineare (il massimo non arriva al numero tentato: il conflitto non viene dal
// contatore) o il numero dopo il massimo non sta nelle cinque cifre: la creazione allora dice
// «Numero busta già esistente.» come prima.
func (b *Buste) riallineaContatore(ctx context.Context, tx *sql.Tx, anno int, tentato string, c audit.Contesto) (string, bool, error) {
	m := formatoNumero.FindStringSubmatch(tentato)
	if m == nil {
		return "", false, nil
	}
	n, _ := strconv.ParseInt(m[1], 10, 64)
	massimo, err := massimoDellAnno(ctx, tx, anno)
	if err != nil {
		return "", false, err
	}
	if massimo < n {
		return "", false, nil
	}
	// Il tentativo ha già fatto avanzare il contatore di uno (o creato la riga a 1): il valore di
	// prima è n-1, 0 per un anno senza riga, come l'anteprima che dà «1».
	if _, err := tx.ExecContext(ctx, `UPDATE sequenze_buste SET ultimo_numero = MAX(ultimo_numero, ?) WHERE anno = ?`, massimo, anno); err != nil {
		return "", false, err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "bag_sequence.realigned", "bag_sequence", anno, "ok", map[string]any{
		"year": anno, "previous": n - 1, "new": massimo, "bag_number_taken": tentato,
	}); err != nil {
		return "", false, err
	}
	nuovo, err := risolviNumero(ctx, tx, "", anno)
	if err != nil {
		return "", false, err
	}
	if !formatoNumeroScritto.MatchString(nuovo) {
		return "", false, nil
	}
	return nuovo, true, nil
}

// automatico dice se il numero del modulo è vuoto, cioè lo assegna il contatore (risolviNumero).
func automatico(scritto string) bool { return strings.TrimSpace(scritto) == "" }
