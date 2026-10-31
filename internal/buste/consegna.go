package buste

import (
	"regexp"
	"time"
)

// La consegna prevista (RIP-H-37, RIP-S-06), come Repair.cast_expected_on/1. Dal modulo arriva
// solo una data vera (il selettore); il testo libero esiste solo da import e si mostra com'è:
// la data se ne ricava solo se il testo comincia con AAAA-MM-GG o GG/MM/AAAA (il LIKE a prefisso
// del Flask), altrimenti non entra in filtri, contatori e urgenze.

var (
	dataISO      = regexp.MustCompile(`\A(\d{4}-\d{2}-\d{2})`)
	dataItaliana = regexp.MustCompile(`\A(\d{2})/(\d{2})/(\d{4})`)
	dataModulo   = regexp.MustCompile(`\A\d{4}-\d{2}-\d{2}\z`)
)

// dataVera dice se AAAA-MM-GG è un giorno che esiste (Date.from_iso8601/1).
func dataVera(s string) bool {
	if !dataModulo.MatchString(s) {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// DataDalTesto è la data (AAAA-MM-GG) che un testo di consegna porta all'inizio, se esiste:
// «12/05/2026 pomeriggio» è il 2026-05-12, «31/02/2026» e «fine mese» nessuna.
func DataDalTesto(testo string) (string, bool) {
	var d string
	if m := dataISO.FindStringSubmatch(testo); m != nil {
		d = m[1]
	} else if m := dataItaliana.FindStringSubmatch(testo); m != nil {
		d = m[3] + "-" + m[2] + "-" + m[1]
	} else {
		return "", false
	}
	if !dataVera(d) {
		return "", false
	}
	return d, true
}

// Consegna è la consegna prevista come si salva: il testo importato e la data, nil per NULL.
type Consegna struct {
	Testo, Data *string
}

// LeggiConsegna è cast_expected_on/1 per un testo scritto (import, o il testo importato che il
// modulo di modifica rimanda): vuoto o di soli spazi toglie testo e data; oltre 255 byte
// ErrConsegnaLunga; altrimenti il testo com'è, con la data se il testo ne comincia con una.
func LeggiConsegna(testo string) (Consegna, error) {
	if vuoto(testo) {
		return Consegna{}, nil
	}
	if len(testo) > byteTestoMassimi {
		return Consegna{}, ErrConsegnaLunga
	}
	c := Consegna{Testo: &testo}
	if d, ok := DataDalTesto(testo); ok {
		c.Data = &d
	}
	return c, nil
}

// dataDalModulo legge una data del modulo (il cast :date di Ecto): vuota è nil; una che non è
// AAAA-MM-GG valida è MsgNonValido. Il cast di Ecto accetterebbe anche un istante senza zona
// («2026-10-31T10:00:00») tagliandolo al giorno: il selettore non lo manda mai, qui no.
func dataDalModulo(s string) (any, string) {
	if vuoto(s) {
		return nil, ""
	}
	if !dataVera(s) {
		return nil, MsgNonValido
	}
	return s, ""
}
