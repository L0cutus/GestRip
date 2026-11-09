package buste

import (
	"database/sql/driver"

	"modernc.org/sqlite"
)

// D-79 estesa (Stefano 09/11/2026, slice 060): il riquadro «⚠ Senza QR», il filtro senza_qr e la sua
// pillola contano, come il segno della lista, ogni busta aperta senza un'etichetta RIP-AAAA-NNNN:
// senza codice (Repair.missing_qr_query/1 dello standalone) o con un QR di altro formato
// (QRAltroFormato). La forma dell'etichetta è quella di EtichettaDi, con la normalizzazione di
// NormalizzaCodice: in SQL non si riscrive, la query chiama la funzione Go fnSenzaEtichetta,
// registrata nel driver per ogni connessione.

// fnSenzaEtichetta è il nome della funzione SQL: 1 se il codice (anche NULL, vuoto o di soli spazi)
// non è un'etichetta, 0 se lo è.
const fnSenzaEtichetta = "gestrip_senza_etichetta"

func init() {
	sqlite.MustRegisterDeterministicScalarFunction(fnSenzaEtichetta, 1, func(_ *sqlite.FunctionContext, a []driver.Value) (driver.Value, error) {
		var codice string
		switch v := a[0].(type) {
		case string:
			codice = v
		case []byte:
			codice = string(v)
		}
		if _, _, ok := EtichettaDi(codice); ok {
			return int64(0), nil
		}
		return int64(1), nil
	})
}
