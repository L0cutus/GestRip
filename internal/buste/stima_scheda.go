package buste

import "context"

// La stima dei tempi sulla scheda della busta (RIP-S-03, RIP-H-55 parte negozio): RepairEstimate.for_shop/2
// dello standalone (go-baseline). Stesso modello della pagina del cliente (pagina_cliente.go): la scheda lo
// chiede direttamente, perché il negozio vede la stima anche con l'interruttore spento o con una consegna
// scritta, e allora la scheda dice perché il cliente non la vede. Leggerla non scrive niente.

// Perché il cliente non vede la stima (hidden_reason/2).
const (
	NascostaNessuna  = ""         // il cliente la vede
	NascostaSpenta   = "spenta"   // l'interruttore del titolare è spento (:switch_off)
	NascostaConsegna = "consegna" // la busta ha una data di consegna scritta (:deadline)
)

// StimaScheda è quello che la scheda mostra della stima.
type StimaScheda struct {
	// Intervallo è la stima di questa busta; nil senza abbastanza buste pronte («Dati insufficienti»).
	Intervallo *Stima
	// Buste è «su X buste» (count: quelle di bottega e quelle passate dall'artigiano); Bottega è «ora N»
	// (shop_count: solo quelle rimaste in bottega, le sole che bastano a dare una base).
	Buste, Bottega int64
	// Minimo è RepairEstimate.minimum_shop/0: le buste di bottega che servono.
	Minimo int64
	// Nascosta è il motivo per cui il cliente non la vede: NascostaSpenta, NascostaConsegna o
	// NascostaNessuna. La scheda lo dice solo con un intervallo.
	Nascosta string
}

// stimaDellaScheda è RepairEstimate.for_shop/2: nil per una busta che non è una riparazione aperta.
// conConsegna è date_source/2 diverso da :none (un testo non vuoto, o una data); giorni sono i «Giorni in
// più» della busta e artigiano dice se è passata dall'artigiano (handed_over_at).
func (b *Buste) stimaDellaScheda(ctx context.Context, compraOro bool, stato string, conConsegna, artigiano bool, giorni int64) (*StimaScheda, error) {
	if !riparazioneAperta(compraOro, stato) {
		return nil, nil
	}
	imp, err := b.leggiImpostazioniStima(ctx)
	if err != nil {
		return nil, err
	}
	m, err := b.modelloStima(ctx, imp)
	if err != nil {
		return nil, err
	}
	s := &StimaScheda{
		Intervallo: m.perLaBusta(artigiano, giorni),
		Buste:      m.bottega + m.artigiano,
		Bottega:    m.bottega,
		Minimo:     stimaMinimoBottega,
	}
	switch {
	case !imp.attiva:
		s.Nascosta = NascostaSpenta
	case conConsegna:
		s.Nascosta = NascostaConsegna
	}
	return s, nil
}
