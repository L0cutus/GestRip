package buste

import (
	"context"
	"database/sql"
	"strings"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/audit"
)

// Il cliente della busta (RIP-H-33), come resolve_intake_customer/2 di WorkshopLive con
// Workshop.create_customer/2, update_customer/3 e Customer.changeset/2. Gira dentro la
// transazione della creazione: una busta rifiutata dopo si porta via il cliente creato, il
// telefono cambiato e le loro righe d'audit.

const (
	nomeMassimo     = 160 // grafemi, come validate_length di Ecto
	telefonoMassimo = 40
)

type cliente struct {
	id             int64
	nome, telefono string
}

// stessoNome è same_customer_name?/2: senza maiuscole e senza spazi ai bordi, mai un nome vuoto.
func stessoNome(salvato, scritto string) bool {
	q := minuscole(strings.TrimSpace(scritto))
	return q != "" && minuscole(strings.TrimSpace(salvato)) == q
}

// minuscole è String.downcase/1 di Elixir nella modalità predefinita (String.Unicode, 1.18):
// la tabella semplice di UnicodeData più le mappature senza condizioni di SpecialCasing, che per
// le minuscole differiscono dalla semplice solo per «İ» (U+0130), che diventa «i» + punto
// combinante (U+0307). Le mappature con condizione (la sigma finale, il turco, il lituano) non si
// applicano: la sigma finale solo con :greek. strings.ToLower darebbe «i» per «İ».
func minuscole(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "\u0130", "i\u0307"))
}

// risolviCliente sceglie o crea il cliente della coppia nome + telefono scritta:
//   - nome vuoto: ErrNomeCliente, prima di tutto;
//   - nessun cliente con quel nome: uno nuovo;
//   - uno solo: si riusa, e un telefono scritto diverso da quello salvato lo sostituisce (un
//     telefono vuoto non cancella mai quello salvato);
//   - più omonimi: quello con lo stesso telefono (il primo per nome e id), altrimenti uno nuovo.
//
// Il telefono non si normalizza: solo gli spazi ai bordi. Nome o telefono oltre i limiti:
// ErrCliente.
func (b *Buste) risolviCliente(ctx context.Context, tx *sql.Tx, nomeScritto, telScritto string, c audit.Contesto, ora string) (cliente, error) {
	nome := strings.TrimSpace(nomeScritto)
	if nome == "" {
		return cliente{}, ErrNomeCliente
	}
	tel := strings.TrimSpace(telScritto)
	// list_customers/0: tutti, per nome e id; il confronto senza maiuscole si fa qui.
	rows, err := tx.QueryContext(ctx, `SELECT id, nome, telefono FROM clienti ORDER BY nome, id`)
	if err != nil {
		return cliente{}, err
	}
	defer rows.Close()
	var omonimi []cliente
	for rows.Next() {
		var cl cliente
		if err := rows.Scan(&cl.id, &cl.nome, &cl.telefono); err != nil {
			return cliente{}, err
		}
		if stessoNome(cl.nome, nome) {
			omonimi = append(omonimi, cl)
		}
	}
	if err := rows.Err(); err != nil {
		return cliente{}, err
	}
	switch len(omonimi) {
	case 0:
		return b.creaCliente(ctx, tx, nome, tel, c, ora)
	case 1:
		return b.allineaTelefono(ctx, tx, omonimi[0], tel, c, ora)
	}
	if tel != "" {
		for _, cl := range omonimi {
			if strings.TrimSpace(cl.telefono) == tel {
				return cl, nil
			}
		}
	}
	return b.creaCliente(ctx, tx, nome, tel, c, ora)
}

func (b *Buste) creaCliente(ctx context.Context, tx *sql.Tx, nome, tel string, c audit.Contesto, ora string) (cliente, error) {
	if uniseg.GraphemeClusterCount(nome) > nomeMassimo || uniseg.GraphemeClusterCount(tel) > telefonoMassimo {
		return cliente{}, ErrCliente
	}
	r, err := tx.ExecContext(ctx, `INSERT INTO clienti (nome, telefono, creato_il, aggiornato_il) VALUES (?, ?, ?, ?)`, nome, tel, ora, ora)
	if err != nil {
		return cliente{}, err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return cliente{}, err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "customer.created", "customer", id, "ok", nil); err != nil {
		return cliente{}, err
	}
	return cliente{id: id, nome: nome, telefono: tel}, nil
}

// allineaTelefono è sync_customer_phone/3: il confronto è con il telefono salvato com'è; il nome
// salvato non si tocca.
func (b *Buste) allineaTelefono(ctx context.Context, tx *sql.Tx, cl cliente, tel string, c audit.Contesto, ora string) (cliente, error) {
	if tel == "" || tel == cl.telefono {
		return cl, nil
	}
	if uniseg.GraphemeClusterCount(tel) > telefonoMassimo {
		return cliente{}, ErrCliente
	}
	if _, err := tx.ExecContext(ctx, `UPDATE clienti SET telefono = ?, aggiornato_il = ? WHERE id = ?`, tel, ora, cl.id); err != nil {
		return cliente{}, err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "customer.updated", "customer", cl.id, "ok", nil); err != nil {
		return cliente{}, err
	}
	cl.telefono = tel
	return cl, nil
}
