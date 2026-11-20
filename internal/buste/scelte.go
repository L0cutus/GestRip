package buste

import (
	"context"
	"database/sql"
	"errors"
)

// Le scelte del modulo della nuova busta (RIP-H-29, 34, 35): quello che refresh/1 di WorkshopLive
// carica per :new, cioè le oreficerie esterne e gli artigiani (list_external_oreficerie/0,
// list_artisans/0), più il nome del negozio della voce «— Diretta <negozio>»
// (OnlineSales.shop_name/0). Nessuna lettura della lista e nessuna riga d'audit. I clienti, che lo
// standalone carica per i suggerimenti mentre si scrive (RIP-H-32), non servono finché quei
// suggerimenti, che vogliono JavaScript, non ci sono; il «Dettato AI» (slice 108) li legge a parte, solo
// quando si estrae (ClientiNoti).

// nomeNegozioDiRipiego è il nome di shop_name/1 senza profilo o con il nome vuoto.
const nomeNegozioDiRipiego = "Negozio"

// Voce è un'oreficeria o un artigiano di un menu del modulo.
type Voce struct {
	ID   int64
	Nome string
}

// Scelte sono i menu del modulo della nuova busta.
type Scelte struct {
	NomeNegozio string
	Oreficerie  []Voce // solo le esterne (id > 1), per nome e id
	Artigiani   []Voce // per nome e id
}

// Scelte legge le scelte del modulo.
func (b *Buste) Scelte(ctx context.Context) (Scelte, error) {
	var s Scelte
	var err error
	if s.NomeNegozio, err = b.nomeNegozio(ctx); err != nil {
		return Scelte{}, err
	}
	if s.Oreficerie, err = b.voci(ctx, `SELECT id, nome FROM oreficerie WHERE id > 1 ORDER BY nome, id`); err != nil {
		return Scelte{}, err
	}
	if s.Artigiani, err = b.voci(ctx, `SELECT id, nome FROM artigiani ORDER BY nome, id`); err != nil {
		return Scelte{}, err
	}
	return s, nil
}

// nomeNegozio è OnlineSales.shop_name/0: il nome del profilo del negozio, o «Negozio» senza
// profilo o con il nome vuoto.
func (b *Buste) nomeNegozio(ctx context.Context) (string, error) {
	var nome string
	switch err := b.d.Lettura.QueryRowContext(ctx, `SELECT nome FROM profilo_negozio WHERE ambito = 'default'`).Scan(&nome); {
	case errors.Is(err, sql.ErrNoRows):
		return nomeNegozioDiRipiego, nil
	case err != nil:
		return "", err
	case nome == "":
		return nomeNegozioDiRipiego, nil
	}
	return nome, nil
}

func (b *Buste) voci(ctx context.Context, q string) ([]Voce, error) {
	rows, err := b.d.Lettura.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var vv []Voce
	for rows.Next() {
		var v Voce
		if err := rows.Scan(&v.ID, &v.Nome); err != nil {
			return nil, err
		}
		vv = append(vv, v)
	}
	return vv, rows.Err()
}

// ClienteNoto è un cliente dell'anagrafica come lo usa il «Dettato AI» della busta nuova (slice 108): id,
// nome e telefono come sono salvati.
type ClienteNoto struct {
	ID             int64
	Nome, Telefono string
}

// ClientiNoti è list_customers/0 per l'estrazione dal dettato (shape/3 e assign_customer_pair/3 dello
// standalone, slice 108): tutti i clienti, per nome e id. Le oreficerie esterne sono quelle di Scelte. Nessuna
// riga d'audit.
func (b *Buste) ClientiNoti(ctx context.Context) ([]ClienteNoto, error) {
	rows, err := b.d.Lettura.QueryContext(ctx, `SELECT id, nome, telefono FROM clienti ORDER BY nome, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cc []ClienteNoto
	for rows.Next() {
		var c ClienteNoto
		if err := rows.Scan(&c.ID, &c.Nome, &c.Telefono); err != nil {
			return nil, err
		}
		cc = append(cc, c)
	}
	return cc, rows.Err()
}
