package buste

import (
	"context"
	"errors"
	"fmt"

	"github.com/L0cutus/gestrip/internal/audit"
)

// L'anteprima del cliente dalla scheda (CLI-27, RIP-S-27), come preview/2 di PublicRepairController
// dello standalone (go-baseline, slice 143): la pagina che il link della busta disegna, per il
// personale, senza nessun token. La busta si cerca per id (Workshop.get_repair/1: Id.positive/1,
// fuori dal cestino); i dati sono quelli di token_page/1 (paginaDove, come /r/), quindi le foto, il
// preventivo e la conversazione di chi ha il link: l'oreficeria su una busta B2B, il cliente
// altrimenti. Un link revocato, scaduto o mai emesso non cambia niente: è l'anteprima della pagina,
// non del link. Non scrive niente tranne la riga repair.customer_preview_viewed
// (record_repair_event/3, con bag_number il numero interno): nessun segno «letto», nessun token.

// ErrAnteprimaNonRegistrata è la riga d'audit dell'anteprima che non si è scritta: niente pagina
// (preview/2 risponde 503 «Anteprima non disponibile»).
var ErrAnteprimaNonRegistrata = errors.New("buste: anteprima del cliente non registrata")

// AnteprimaCliente legge la pagina del cliente della busta id per chi la guarda (c) e scrive la
// riga repair.customer_preview_viewed. Busta che non c'è, id malformato o busta nel cestino:
// ErrBustaNonTrovata, senza righe.
func (b *Buste) AnteprimaCliente(ctx context.Context, id string, c audit.Contesto) (PaginaCliente, error) {
	n, ok := idPositivo(id)
	if !ok {
		return PaginaCliente{}, ErrBustaNonTrovata
	}
	p, err := b.paginaDove(ctx, `id = ? AND cancellata_il IS NULL`, []any{n}, false)
	if err != nil {
		return PaginaCliente{}, err
	}
	if err := b.reg.Registra(ctx, c, "repair.customer_preview_viewed", "repair", p.ID, "ok", map[string]any{"bag_number": p.numeroInterno}); err != nil {
		return PaginaCliente{}, fmt.Errorf("%w: %w", ErrAnteprimaNonRegistrata, err)
	}
	return p, nil
}
