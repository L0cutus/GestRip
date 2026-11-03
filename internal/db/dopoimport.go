package db

import (
	"context"
	"database/sql"
)

// Interrogatore è quello che serve a BloccatoDopoImport: una transazione o un *sql.DB.
type Interrogatore interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

// BloccatoDopoImport dice se il database viene da un import dallo standalone e non c'è ancora un
// backup verificato: allora non si cancella niente di quello che l'import ha portato (PIANO T2,
// ADM-17, decisione di Stefano del 30/10/2026). Lo usano la conservazione dell'audit e, in attesa
// della domanda aperta 20, la coda delle cancellazioni dei media (media.Pulizia). Il registro dei
// backup verificati non esiste ancora (D-11, T5): finché non c'è, il segno dell'import basta a
// bloccare. La slice dei backup aggiunge qui la condizione «e nessun backup verificato dopo
// importato_il», che toglie il blocco a tutti e due.
func BloccatoDopoImport(ctx context.Context, q Interrogatore) (bool, error) {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM import_standalone`).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}
