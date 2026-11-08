package importer

import (
	"context"
	"errors"

	"github.com/L0cutus/gestrip/internal/buste"
)

// Le due riscritture una tantum delle buste importate (slice 057), dopo la copia di tutte le tabelle e
// nella stessa transazione, decise da Stefano il 07/11/2026 (docs/SVILUPPO.md):
//   - D-77: un numero interno con zeri davanti («0046/26») si riscrive senza (buste.NormalizzaNumero,
//     «46/26»), con l'avviso buste/numero_senza_zeri. Se il numero senza zeri è già di un'altra busta
//     (cestino compreso: l'indice unico lo copre), o due buste importate lo darebbero tutte e due
//     («0046/26» e «046/26»), nessuna di quelle si riscrive e l'avviso a parte
//     buste/numero_senza_zeri_occupato le nomina. È la scelta provvisoria della slice 057 (la strada
//     che non cambia dati), da confermare con Stefano. Il valore del numero non cambia, quindi il
//     contatore dell'anno (sequenze_buste, contatore.go) resta giusto: legge già «0046/26» come 46;
//   - D-76: un codice QR salvato con spazi ai bordi o con «rip-» minuscolo si riscrive com'è normalizzato
//     (buste.NormalizzaCodice: anche gli spazi accanto ai trattini, quando il risultato è
//     un'etichetta), con l'avviso buste/codice_qr_normalizzato. Un codice di soli spazi resta com'è:
//     vale già «senza QR» (Repair.blank_qr?/1) e non si cambia in un altro vuoto.
// Lo standalone importa e tiene numeri e codici come sono. Gli avvisi dicono quante buste e quali,
// con l'id e il numero interno (mai il codice né un dato del cliente); aggiornato_il non si tocca.

// riscriviBuste fa le due riscritture, prima i numeri: così l'avviso dei codici ha il numero finale.
func (c *copiatore) riscriviBuste(ctx context.Context) error {
	if err := c.numeriSenzaZeri(ctx); err != nil {
		return err
	}
	return c.codiciNormalizzati(ctx)
}

// numeriSenzaZeri è la D-77.
func (c *copiatore) numeriSenzaZeri(ctx context.Context) error {
	rr, err := c.tx.QueryContext(ctx, `SELECT id, numero FROM buste ORDER BY id`)
	if err != nil {
		return err
	}
	usati := map[string]bool{}
	type candidata struct {
		id          int64
		prima, dopo string
	}
	var cc []candidata
	for rr.Next() {
		var id int64
		var n string
		if err := rr.Scan(&id, &n); err != nil {
			_ = rr.Close()
			return err
		}
		usati[n] = true
		if d := buste.NormalizzaNumero(n); d != n {
			cc = append(cc, candidata{id, n, d})
		}
	}
	if err := errors.Join(rr.Err(), rr.Close()); err != nil {
		return err
	}
	quante := map[string]int{}
	for _, x := range cc {
		quante[x.dopo]++
	}
	var fatte, occupate []BustaReport
	for _, x := range cc {
		if usati[x.dopo] || quante[x.dopo] > 1 {
			occupate = append(occupate, BustaReport{Id: x.id, Numero: x.prima, Occupato: x.dopo})
			continue
		}
		if _, err := c.tx.ExecContext(ctx, `UPDATE buste SET numero = ? WHERE id = ?`, x.dopo, x.id); err != nil {
			return err
		}
		fatte = append(fatte, BustaReport{Id: x.id, Numero: x.dopo, Prima: x.prima})
	}
	if len(fatte) > 0 {
		c.rep.avvisa(Voce{Codice: "buste/numero_senza_zeri", Tabella: "repairs", Colonna: "bag_number", Righe: ptr(int64(len(fatte))), Buste: fatte})
	}
	if len(occupate) > 0 {
		c.rep.avvisa(Voce{Codice: "buste/numero_senza_zeri_occupato", Tabella: "repairs", Colonna: "bag_number",
			Righe: ptr(int64(len(occupate))), Buste: occupate})
	}
	return nil
}

// codiciNormalizzati è la D-76.
func (c *copiatore) codiciNormalizzati(ctx context.Context) error {
	rr, err := c.tx.QueryContext(ctx, `SELECT id, numero, codice_qr FROM buste WHERE codice_qr IS NOT NULL ORDER BY id`)
	if err != nil {
		return err
	}
	type riscrittura struct {
		id             int64
		numero, codice string
	}
	var rs []riscrittura
	for rr.Next() {
		var id int64
		var n, qr string
		if err := rr.Scan(&id, &n, &qr); err != nil {
			_ = rr.Close()
			return err
		}
		if nuovo := buste.NormalizzaCodice(qr); nuovo != qr && nuovo != "" {
			rs = append(rs, riscrittura{id, n, nuovo})
		}
	}
	if err := errors.Join(rr.Err(), rr.Close()); err != nil {
		return err
	}
	var fatte []BustaReport
	for _, x := range rs {
		if _, err := c.tx.ExecContext(ctx, `UPDATE buste SET codice_qr = ? WHERE id = ?`, x.codice, x.id); err != nil {
			return err
		}
		fatte = append(fatte, BustaReport{Id: x.id, Numero: x.numero})
	}
	if len(fatte) > 0 {
		c.rep.avvisa(Voce{Codice: "buste/codice_qr_normalizzato", Tabella: "repairs", Colonna: "qr_code", Righe: ptr(int64(len(fatte))), Buste: fatte})
	}
	return nil
}
