package buste

import (
	"context"
	"strings"

	"github.com/L0cutus/gestrip/internal/audit"
)

// La galleria della home (RIP-H-13, slice 057), come RepairPhotos.gallery/1 e audit_gallery/2 dello
// standalone a go-baseline (slice 202 e 205):
//   - per le buste della pagina, la prima foto (posizione, poi id) e quante sono in tutto; le buste
//     senza foto non ci sono. Il personale vede tutte le foto di una busta (Visibile con
//     PubblicoPersonale): la galleria non guarda i canali;
//   - una riga repair.staff_gallery_viewed per le foto mostrate, risorsa repair, id «gallery», dati
//     photo_ids nell'ordine della pagina; è la riga che copre le miniature firmate (gettone della
//     galleria, internal/web/foto.go), così le miniature non scrivono una riga ciascuna.

// PrimaFoto è la voce di una busta nella galleria: la prima foto e quante foto ha la busta.
type PrimaFoto struct {
	Foto
	Quante int64
}

// Galleria è gallery/1: per ogni busta di ids che ha almeno una foto, la prima e il numero delle
// foto. Nessun id, nessuna query.
func (b *Buste) Galleria(ctx context.Context, ids []int64) (map[int64]PrimaFoto, error) {
	out := map[int64]PrimaFoto{}
	if len(ids) == 0 {
		return out, nil
	}
	arg := make([]any, len(ids))
	for i, id := range ids {
		arg[i] = id
	}
	righe, err := b.d.Lettura.QueryContext(ctx, `SELECT `+colonneFoto+` FROM foto_buste WHERE busta_id IN (?`+
		strings.Repeat(", ?", len(ids)-1)+`) ORDER BY posizione, id`, arg...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = righe.Close() }()
	for righe.Next() {
		f, err := leggiFoto(righe)
		if err != nil {
			return nil, err
		}
		p, c := out[f.BustaID]
		if !c {
			p.Foto = f
		}
		p.Quante++
		out[f.BustaID] = p
	}
	return out, righe.Err()
}

// RegistraGalleria è audit_gallery/2: la riga repair.staff_gallery_viewed con gli id delle foto
// mostrate, nell'ordine dato. Un errore vuol dire che la riga non c'è: allora le miniature non
// ricevono il gettone e ognuna scrive la sua riga (sign_gallery/2).
func (b *Buste) RegistraGalleria(ctx context.Context, fotoIDs []int64, c audit.Contesto) error {
	if fotoIDs == nil {
		fotoIDs = []int64{}
	}
	return b.reg.Registra(ctx, c, "repair.staff_gallery_viewed", "repair", "gallery", "ok", map[string]any{"photo_ids": fotoIDs})
}
