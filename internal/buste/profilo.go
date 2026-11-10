package buste

import (
	"context"
	"database/sql"
	"errors"
)

// ProfiloNegozio è la riga del profilo del negozio come la leggono le pagine pubbliche senza busta:
// il portale ritirato /m/ (CLI-53) e l'informativa /privacy (CLI-54), come
// OnlineSales.get_shop_profile/0 dello standalone. Presente è falso se il negozio non ha ancora un
// profilo; i campi sono nil quando la colonna è NULL e restano com'è scritti (anche vuoti): chi li
// mostra decide, come lo standalone, cosa vale come assente.
type ProfiloNegozio struct {
	Presente                                                      bool
	Nome                                                          string
	RagioneSociale, PartitaIVA, Indirizzo, Citta, Telefono, Email *string
}

// LeggiProfiloNegozio legge il profilo del negozio (ambito «default»).
func (b *Buste) LeggiProfiloNegozio(ctx context.Context) (ProfiloNegozio, error) {
	p := ProfiloNegozio{Presente: true}
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT nome, ragione_sociale, partita_iva, indirizzo, citta, telefono, email
		FROM profilo_negozio WHERE ambito = 'default'`).Scan(&p.Nome, &p.RagioneSociale, &p.PartitaIVA, &p.Indirizzo, &p.Citta, &p.Telefono, &p.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return ProfiloNegozio{}, nil
	}
	if err != nil {
		return ProfiloNegozio{}, err
	}
	return p, nil
}
