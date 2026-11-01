package buste

import (
	"context"
	"database/sql"
	"errors"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/media"
)

// Le letture delle foto per il personale (RIP-S-33), come read_staff_media/4,
// read_staff_media_covered/4, authorized_media/4 e media_for/2 di RepairPhotos dello standalone a
// go-baseline: l'unica strada da una riga di foto ai byte. Chi può vedere la foto si chiede una
// volta, con Visibile, prima di guardare la variante: una miniatura non risponde mai dove la foto
// non risponderebbe.

// Variante è quale dei due file di una foto si chiede (PhotoMedia.variant/1).
type Variante string

// Le due varianti, con i nomi dell'audit e di x-photo-variant.
const (
	VarianteIntera    Variante = "full"
	VarianteMiniatura Variante = "thumb"
)

// MediaFoto è la risposta autorizzata: i byte, il loro tipo e la variante che portano davvero (una
// miniatura che non si fa ripiega sulla foto intera, e lo dice).
type MediaFoto struct {
	Byte     []byte
	Tipo     string
	Variante Variante
}

// MiniatureFoto fa la miniatura di una foto dell'archivio (media.Miniature).
type MiniatureFoto interface {
	Miniatura(ctx context.Context, chiave string) ([]byte, error)
}

// ConMiniature dà alle buste la cache delle miniature.
func (b *Buste) ConMiniature(m MiniatureFoto) *Buste {
	b.miniature = m
	return b
}

// LeggiFotoPersonale è read_staff_media/4: la foto foto della busta busta (gli id come arrivano
// dall'indirizzo) nella variante v, per il personale, con una riga repair.staff_photo_viewed in
// ogni caso: «ok» con i byte, «denied» con nil (busta che non c'è, malformata o nel cestino; foto
// che non c'è, non è sua o con il file guasto). Gli id si scrivono ripuliti: l'intero, o «invalid»
// (sanitized_id/1); i dati dicono la variante solo per una miniatura chiesta: quella data, o
// «thumb» sul rifiuto. Un errore vuol dire che la riga non si è scritta: niente byte.
func (b *Buste) LeggiFotoPersonale(ctx context.Context, busta, foto string, v Variante, c audit.Contesto) (*MediaFoto, error) {
	if b.archivio == nil {
		return nil, ErrSenzaArchivio
	}
	m, err := b.mediaAutorizzato(ctx, busta, foto, v)
	if err != nil {
		return nil, err
	}
	if err := b.registraLettura(ctx, busta, foto, m, v, c); err != nil {
		return nil, err
	}
	return m, nil
}

// LeggiMiniaturaCoperta è read_staff_media_covered/4: la miniatura che il gettone della galleria
// copre (chi chiama lo ha verificato per questo utente, busta e foto) si dà senza una riga sua; se
// ripiega sulla foto intera, o è rifiutata, è LeggiFotoPersonale con la sua riga.
func (b *Buste) LeggiMiniaturaCoperta(ctx context.Context, busta, foto string, c audit.Contesto) (*MediaFoto, error) {
	if b.archivio == nil {
		return nil, ErrSenzaArchivio
	}
	m, err := b.mediaAutorizzato(ctx, busta, foto, VarianteMiniatura)
	if err != nil {
		return nil, err
	}
	if m != nil && m.Variante == VarianteMiniatura {
		return m, nil
	}
	return b.LeggiFotoPersonale(ctx, busta, foto, VarianteMiniatura, c)
}

// mediaAutorizzato è authorized_media/4 per il personale: nil se la busta (fuori dal cestino, come
// Repo.get con il filtro di prepare_query/3) o la foto mancano, o se la foto non ha byte buoni.
func (b *Buste) mediaAutorizzato(ctx context.Context, busta, foto string, v Variante) (*MediaFoto, error) {
	bid, ok := idPositivo(busta)
	if !ok {
		return nil, nil
	}
	var oreficeria sql.NullInt64
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT oreficeria_id FROM buste WHERE id = ? AND cancellata_il IS NULL`, bid).Scan(&oreficeria)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	fid, ok := idPositivo(foto)
	if !ok {
		return nil, nil
	}
	f, trovata, err := fotoDi(ctx, b.d.Lettura, bid, fid)
	if err != nil || !trovata {
		return nil, err
	}
	if !Visibile(BustaDellaFoto{ID: bid, OreficeriaID: nullaIntero(oreficeria)}, f, PubblicoPersonale) {
		return nil, nil
	}
	return b.mediaDi(ctx, f, v), nil
}

// mediaDi è media_for/2: per una miniatura la cache, e se non si fa la foto intera (variant full);
// la foto intera con il tipo dei suoi byte, o quello della riga se non hanno una firma nota.
func (b *Buste) mediaDi(ctx context.Context, f Foto, v Variante) *MediaFoto {
	if v == VarianteMiniatura && b.miniature != nil {
		if m, err := b.miniature.Miniatura(ctx, f.Chiave); err == nil {
			return &MediaFoto{Byte: m, Tipo: media.TipoWebP, Variante: VarianteMiniatura}
		}
	}
	byt, err := b.archivio.Leggi(f.Chiave)
	if err != nil {
		return nil
	}
	return &MediaFoto{Byte: byt, Tipo: media.TipoImmagineORegistrato(byt, f.TipoMime), Variante: VarianteIntera}
}

// registraLettura è audit_read/5.
func (b *Buste) registraLettura(ctx context.Context, busta, foto string, m *MediaFoto, v Variante, c audit.Contesto) error {
	esito := "denied"
	if m != nil {
		esito = "ok"
	}
	dati := map[string]any{"photo_id": idRipulito(foto)}
	if v == VarianteMiniatura {
		dati["variant"] = string(VarianteMiniatura)
		if m != nil {
			dati["variant"] = string(m.Variante)
		}
	}
	return b.reg.Registra(ctx, c, "repair.staff_photo_viewed", "repair", idRipulito(busta), esito, dati)
}

// idRipulito è sanitized_id/1: l'id come intero, o «invalid».
func idRipulito(s string) any {
	if n, ok := idPositivo(s); ok {
		return n
	}
	return "invalid"
}

// Riscaldatore prepara le miniature in sottofondo (media.Riscaldatore).
type Riscaldatore interface{ Scalda(chiavi []string) }

// ConRiscaldatore dà alle buste chi prepara le miniature dopo un caricamento (warm_previews/1).
func (b *Buste) ConRiscaldatore(r Riscaldatore) *Buste {
	b.riscaldatore = r
	return b
}

// ChiaviFoto dà le chiavi delle foto di tutte le buste, le più recenti prima e senza ripetizioni
// (ThumbnailWarmer.keys/0: sono le buste della prima pagina). Come lo standalone, anche quelle
// delle buste nel cestino: le foto non hanno il filtro del cestino.
func (b *Buste) ChiaviFoto(ctx context.Context) ([]string, error) {
	righe, err := b.d.Lettura.QueryContext(ctx, `SELECT chiave FROM foto_buste ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer righe.Close()
	var v []string
	visti := map[string]bool{}
	for righe.Next() {
		var k string
		if err := righe.Scan(&k); err != nil {
			return nil, err
		}
		if !visti[k] {
			visti[k] = true
			v = append(v, k)
		}
	}
	return v, righe.Err()
}
