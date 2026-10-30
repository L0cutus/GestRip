// Package importer porterà i dati dallo standalone nel Go (`gestrip import`, PIANO T2).
//
// Per ora c'è solo la mappa: ogni tabella e ogni colonna dello standalone a go-baseline, dove va
// nel Go e come si traduce, o perché si scarta. L'import la usa per rifiutare una tabella o una
// colonna che non conosce (OPS-01) e per tradurre i valori (OPS-03).
package importer

// Traduzione dice come passa un valore dallo standalone al Go.
type Traduzione string

const (
	// Uguale: lo stesso valore, lo stesso tipo.
	Uguale Traduzione = "uguale"
	// Istante: un :utc_datetime_usec (testo ISO 8601 in UTC) riscritto in db.FormatoIstante.
	Istante Traduzione = "istante"
	// Data: un :date, testo AAAA-MM-GG, uguale (si controlla la forma).
	Data Traduzione = "data"
	// Booleano: 0 o 1, uguale (ecto_sqlite3 li scrive già così).
	Booleano Traduzione = "booleano"
	// JSON: un :map (o un testo JSON), uguale; deve essere JSON valido.
	JSON Traduzione = "json"
	// Ruolo: owner e staff diventano titolare e dipendente.
	Ruolo Traduzione = "ruolo"
	// Email: minuscola e senza spazi ai lati; una differenza va nel report (PIANO T2).
	Email Traduzione = "email"
)

// Colonna è una colonna dello standalone e la sua destinazione. Go vuoto: scartata, e Nota
// dice perché.
type Colonna struct {
	Standalone string
	Go         string
	Traduzione Traduzione
	Nota       string
}

// Tabella è una tabella dello standalone e la sua destinazione nel Go. Go vuoto: scartata, e
// Nota dice perché, con il riferimento all'inventario o alla deviazione. Una tabella che va in
// più tabelle Go compare una volta per destinazione e Righe dice quali righe.
type Tabella struct {
	Standalone string
	Go         string
	Righe      string
	Nota       string
	Colonne    []Colonna
}

// DestinoTrigger dice dove va una regola che lo standalone tiene in un trigger: lo stesso
// trigger nel Go (Go è il suo nome), un CHECK, o la slice della sua area.
type DestinoTrigger struct {
	Go   string
	Nota string
}
