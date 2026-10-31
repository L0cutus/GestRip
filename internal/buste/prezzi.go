package buste

import (
	"math/big"
	"regexp"
	"strings"
)

// I prezzi a testo (RIP-H-36, RIP-S-05), come GestripStandalone.Money.cents/1 e
// Repair.cast_price/4: il testo scritto si conserva tale e quale e, solo se è un importo semplice,
// se ne ricavano anche i centesimi. Niente si indovina.

const (
	byteImportoMassimi = 20  // @max_bytes di Money
	byteTestoMassimi   = 255 // @max_price_text e @max_date_text di Repair: byte, non caratteri
)

// formatoImporto è @amount_format di Money: cifre ASCII, poi virgola o punto e al più due decimali.
var formatoImporto = regexp.MustCompile(`\A[0-9]+(?:[.,][0-9]{1,2})?\z`)

// Centesimi sono i centesimi di un importo semplice, con il conto decimale esatto («19,99» è
// 1999); false se il testo non è un importo semplice, è più lungo di 20 byte o non sta in un
// intero a 64 bit.
func Centesimi(testo string) (int64, bool) {
	if len(testo) > byteImportoMassimi || !formatoImporto.MatchString(testo) {
		return 0, false
	}
	intero, decimali, _ := strings.Cut(strings.ReplaceAll(testo, ",", "."), ".")
	n, ok := new(big.Int).SetString(intero+(decimali + "00")[:2], 10)
	if !ok || !n.IsInt64() {
		return 0, false
	}
	return n.Int64(), true
}

// Prezzo è un preventivo o un prezzo finale come si salva: testo e centesimi, nil per NULL.
type Prezzo struct {
	Testo     *string
	Centesimi *int64
}

// LeggiPrezzo è cast_price/4 per un testo scritto: vuoto o di soli spazi dà testo e centesimi
// NULL; oltre 255 byte ErrImportoLungo; altrimenti il testo com'è, con i centesimi se è un importo
// semplice.
func LeggiPrezzo(testo string) (Prezzo, error) {
	if vuoto(testo) {
		return Prezzo{}, nil
	}
	if len(testo) > byteTestoMassimi {
		return Prezzo{}, ErrImportoLungo
	}
	p := Prezzo{Testo: &testo}
	if c, ok := Centesimi(testo); ok {
		p.Centesimi = &c
	}
	return p, nil
}

func (p Prezzo) testo() any {
	if p.Testo == nil {
		return nil
	}
	return *p.Testo
}

func (p Prezzo) centesimi() any {
	if p.Centesimi == nil {
		return nil
	}
	return *p.Centesimi
}
