package buste

import (
	"context"
	"errors"

	"github.com/L0cutus/gestrip/internal/audit"
)

// Le foto della pagina del cliente (CLI-04), come photo/2 di PublicRepairController e
// read_public_media/5 di RepairPhotos dello standalone (go-baseline): il link apre la busta come
// la pagina (bustaDelToken), il pubblico è chi ha il link («orefice» su una busta B2B, «customer»
// altrimenti), la visibilità si chiede una volta con Visibile prima di guardare la variante. Ogni
// lettura di una busta trovata scrive la riga repair.public_photo_viewed («ok» o «denied»); un link
// che non apre niente non scrive niente, come lo standalone (Workshop.get_public_repair/1 è nil e
// read_public_media non parte).

// pubblico è audience/1 del controller.
func (t bustaDelToken) pubblico() Pubblico {
	if Classificazione(t.oreficeriaID) == ClasseEsterna {
		return PubblicoOreficeria
	}
	return PubblicoCliente
}

// LeggiFotoCliente dà la foto foto (l'id come arriva dall'indirizzo) nella variante v per il link
// token; nil se il link non apre niente, la foto non c'è, è di un'altra busta, non è visibile a chi
// ha il link o ha il file guasto. Un errore vuol dire che la riga d'audit non si è scritta: niente
// byte.
func (b *Buste) LeggiFotoCliente(ctx context.Context, token, foto string, v Variante, c audit.Contesto) (*MediaFoto, error) {
	if b.archivio == nil {
		return nil, ErrSenzaArchivio
	}
	t, err := b.bustaDelToken(ctx, token)
	if errors.Is(err, ErrBustaNonTrovata) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p := t.pubblico()
	// I byte prima dell'audit, come lo standalone: la riga è una INSERT da sola, dopo.
	var m *MediaFoto
	if fid, ok := idPositivo(foto); ok {
		f, trovata, err := fotoDi(ctx, b.d.Lettura, t.id, fid)
		if err != nil {
			return nil, err
		}
		if trovata && Visibile(BustaDellaFoto{ID: t.id, OreficeriaID: t.oreficeriaID}, f, p) {
			m = b.mediaDi(ctx, f, v)
		}
	}
	esito := "denied"
	if m != nil {
		esito = "ok"
	}
	dati := map[string]any{"photo_id": idRipulito(foto), "audience": string(p)}
	if v == VarianteMiniatura {
		dati["variant"] = string(VarianteMiniatura)
		if m != nil {
			dati["variant"] = string(m.Variante)
		}
	}
	if err := b.reg.Registra(ctx, c, "repair.public_photo_viewed", "repair", t.id, esito, dati); err != nil {
		return nil, err
	}
	return m, nil
}
