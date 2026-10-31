package importer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Verdetti del report (OPS-05): il database nuovo si attiva solo con Riconciliato.
const (
	Riconciliato = "riconciliato"
	Bloccato     = "bloccato"
)

// FormatoReport è la versione della forma del report.
const FormatoReport = 1

// Report è il report.json dell'import (OPS-05). Non contiene mai un valore dei dati (email,
// token, hash, nomi, testi): solo nomi di tabelle e colonne, id, conteggi, totali in centesimi,
// codici, l'impronta e la dimensione della sorgente, la modalità e il riferimento dato
// dall'operatore. Istante è l'unico campo che cambia tra due import della stessa sorgente.
type Report struct {
	Formato     int             `json:"formato"`
	Modalita    string          `json:"modalita"`
	Riferimento string          `json:"riferimento,omitempty"`
	Sorgente    SorgenteReport  `json:"sorgente"`
	Tabelle     []TabellaReport `json:"tabelle"`
	Scartate    []TabellaReport `json:"scartate"`
	Totali      []TotaleReport  `json:"totali_centesimi"`
	Avvisi      []Voce          `json:"avvisi"`
	Differenze  []Voce          `json:"differenze"`
	// Media sono i file media (slice 023): nil se l'import si è fermato prima di guardarli.
	Media    *MediaReport `json:"media"`
	Verdetto string       `json:"verdetto"`
	Istante  string       `json:"istante"`
}

// SorgenteReport dice quale file è stato importato, senza il percorso.
type SorgenteReport struct {
	Sha256 string `json:"sha256"`
	Byte   int64  `json:"byte"`
}

// TabellaReport sono i conteggi di una tabella dello standalone: righe lette nella sorgente e
// righe scritte in ogni tabella Go di destinazione.
type TabellaReport struct {
	Standalone string           `json:"standalone"`
	Lette      int64            `json:"lette"`
	Scritte    map[string]int64 `json:"scritte,omitempty"`
}

// TotaleReport è la somma di una colonna di soldi (centesimi) nella sorgente e nella destinazione.
type TotaleReport struct {
	Tabella      string `json:"tabella"`
	Colonna      string `json:"colonna"`
	Sorgente     int64  `json:"sorgente"`
	Destinazione int64  `json:"destinazione"`
}

// Voce è un avviso o una differenza bloccante: un codice e dove, mai il valore. Tabella e Colonna
// sono i nomi dello standalone; Id è il rowid della riga nella sorgente (l'id, dove la tabella ce
// l'ha); Righe è un numero di righe dove serve (tabella sconosciuta, conteggi).
type Voce struct {
	Codice  string `json:"codice"`
	Tabella string `json:"tabella,omitempty"`
	Id      *int64 `json:"id,omitempty"`
	Colonna string `json:"colonna,omitempty"`
	// Chiave è il nome di una chiave di settings, solo per quelle che la mappa conosce.
	Chiave string `json:"chiave,omitempty"`
	Righe  *int64 `json:"righe,omitempty"`
}

func (r *Report) blocca(v Voce)  { r.Differenze = append(r.Differenze, v) }
func (r *Report) avvisa(v Voce)  { r.Avvisi = append(r.Avvisi, v) }
func (r *Report) bloccato() bool { return len(r.Differenze) > 0 }

func ptr(n int64) *int64 { return &n }

// preparaReport scrive il report in un file temporaneo 0600 nella cartella del report, scritto e
// sincronizzato, e ne dà il nome. Il nome finale lo dà Importa con un link, che non sovrascrive
// mai un file che c'è già (come ReportWriter dello standalone).
func preparaReport(percorso string, r Report) (tmp string, err error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	b = append(b, '\n')
	f, err := os.CreateTemp(filepath.Dir(percorso), "."+filepath.Base(percorso)+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("report: %w", err)
	}
	tmp = f.Name()
	err = f.Chmod(0o600)
	if err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return "", errors.Join(fmt.Errorf("report: %w", err), os.Remove(tmp))
	}
	return tmp, nil
}

// sincronizzaCartella rende durevole un nome nuovo nella cartella (link, rinomina).
func sincronizzaCartella(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	return errors.Join(err, d.Close())
}
