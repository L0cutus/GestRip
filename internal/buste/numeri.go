package buste

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/clock"
)

// La numerazione N/AA (RIP-H-31), come peek_next_bag_number/1, resolve_bag_number/3,
// advance_bag_sequence/2 e next_bag_number/2 di workshop.ex. Un contatore per anno in
// sequenze_buste; l'unicità del numero la tiene l'indice unico di buste.numero, su tutte le buste
// (cestino compreso), e un numero già usato non si rinumera mai.

// formatoNumero è @bag_number_format di repair.ex: fino a cinque cifre (divergenza voluta dal
// Flask, che ne accettava quattro), la barra, due cifre dell'anno. \d è solo ASCII, come nella
// regex dello standalone senza «u».
var formatoNumero = regexp.MustCompile(`^(\d{1,5})/(\d{2})$`)

func numero(n int64, anno int) string { return fmt.Sprintf("%d/%02d", n, anno%100) }

// ProssimoNumero è l'anteprima del prossimo numero automatico dell'anno di oggi in negozio, per
// riempire il modulo. Non scrive niente: il numero vero si assegna alla creazione, quindi
// l'anteprima può invecchiare (e allora la creazione dice «Numero busta già esistente.»).
func (b *Buste) ProssimoNumero(ctx context.Context) (string, error) {
	return b.ProssimoNumeroDellAnno(ctx, clock.Oggi(b.o).Anno)
}

// ProssimoNumeroDellAnno è l'anteprima per un anno. Come lo standalone, qualunque risposta che non
// sia una riga del contatore (nessuna riga, o un errore della lettura) vale «1».
func (b *Buste) ProssimoNumeroDellAnno(ctx context.Context, anno int) (string, error) {
	var ultimo int64
	if err := b.d.Lettura.QueryRowContext(ctx, `SELECT ultimo_numero FROM sequenze_buste WHERE anno = ?`, anno).Scan(&ultimo); err != nil {
		return numero(1, anno), nil
	}
	return numero(ultimo+1, anno), nil
}

// esecutore: la transazione della creazione.
type esecutore interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

// risolviNumero è resolve_bag_number/3: vuoto (o di soli spazi) assegna il prossimo dell'anno
// della data di accettazione; uno scritto si usa senza gli spazi ai bordi e alza il contatore del
// suo anno. Il formato lo controlla la creazione con gli altri campi.
func risolviNumero(ctx context.Context, tx esecutore, scritto string, anno int) (string, error) {
	manuale := strings.TrimSpace(scritto)
	if manuale == "" {
		var n int64
		err := tx.QueryRowContext(ctx, `INSERT INTO sequenze_buste (anno, ultimo_numero) VALUES (?, 1)
			ON CONFLICT (anno) DO UPDATE SET ultimo_numero = ultimo_numero + 1 RETURNING ultimo_numero`, anno).Scan(&n)
		if err != nil {
			return "", err
		}
		return numero(n, anno), nil
	}
	return manuale, alzaContatore(ctx, tx, manuale)
}

// alzaContatore è advance_bag_sequence/2: porta il contatore dell'anno del numero (2000 + le due
// cifre) almeno al numero, così il prossimo automatico non lo ripete. Un numero fuori formato non
// tocca niente.
func alzaContatore(ctx context.Context, tx esecutore, manuale string) error {
	m := formatoNumero.FindStringSubmatch(manuale)
	if m == nil {
		return nil
	}
	n, _ := strconv.ParseInt(m[1], 10, 64) // al più cinque cifre: non fallisce
	aa, _ := strconv.Atoi(m[2])
	_, err := tx.ExecContext(ctx, `INSERT INTO sequenze_buste (anno, ultimo_numero) VALUES (?, ?)
		ON CONFLICT (anno) DO UPDATE SET ultimo_numero = MAX(ultimo_numero, excluded.ultimo_numero)`, 2000+aa, n)
	return err
}
