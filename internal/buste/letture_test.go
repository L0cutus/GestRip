package buste

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/imaging"
	"github.com/L0cutus/gestrip/internal/imaging/sintetiche"
	"github.com/L0cutus/gestrip/internal/media"
)

// read_staff_media/4 e read_staff_media_covered/4 di RepairPhotos (go-baseline): l'unica strada da
// una riga di foto ai byte per il personale, con l'audit di ogni lettura.

type miniaturaNelProcesso struct{}

func (miniaturaNelProcesso) Miniatura(_ context.Context, in []byte) ([]byte, error) {
	return imaging.Miniatura(in)
}

func conMiniature(t *testing.T) (*Buste, *provaFoto) {
	t.Helper()
	b, p := conFoto(t)
	b.ConMiniature(media.NuoveMiniature(media.Nuovo(p.mediaDi), filepath.Join(p.dir, media.CartellaMiniature), miniaturaNelProcesso{}))
	return b, p
}

// inventario: RIP-S-33
// La foto intera e la miniatura di una foto della busta, ognuna con la sua riga
// repair.staff_photo_viewed: la miniatura dice la variante che ha dato.
func TestLeggiFotoPersonale(t *testing.T) {
	b, p := conMiniature(t)
	in := scriviFile(t, p.dir, "grande.png", sintetiche.PNG(1000, 750, 1))
	f := allega(t, b, p.busta, []Caricamento{{in, "grande.png"}})[0]
	m, err := b.LeggiFotoPersonale(ctx, itoa(p.busta), itoa(f.ID), VarianteIntera, chi)
	if err != nil || m == nil || m.Tipo != media.TipoJPEG || m.Variante != VarianteIntera || !strings.HasPrefix(string(m.Byte), "\xff\xd8\xff") {
		t.Fatalf("intera: %+v %v", m, err)
	}
	if got := ultimoAudit(t, p.b.d, "repair.staff_photo_viewed"); got != "repair.staff_photo_viewed|repair|"+itoa(p.busta)+`|ok|{"photo_id":`+itoa(f.ID)+`}` {
		t.Errorf("audit intera: %s", got)
	}
	m, err = b.LeggiFotoPersonale(ctx, itoa(p.busta), itoa(f.ID), VarianteMiniatura, chi)
	if err != nil || m == nil || m.Tipo != media.TipoWebP || m.Variante != VarianteMiniatura {
		t.Fatalf("miniatura: %+v %v", m, err)
	}
	if got := ultimoAudit(t, p.b.d, "repair.staff_photo_viewed"); got != "repair.staff_photo_viewed|repair|"+itoa(p.busta)+`|ok|{"photo_id":`+itoa(f.ID)+`,"variant":"thumb"}` {
		t.Errorf("audit miniatura: %s", got)
	}
	var ip string
	if err := p.b.d.Lettura.QueryRow(`SELECT ip FROM eventi_audit WHERE azione = 'repair.staff_photo_viewed' ORDER BY id DESC LIMIT 1`).Scan(&ip); err != nil || ip != chi.IP {
		t.Errorf("ip %q %v", ip, err)
	}
}

// inventario: RIP-S-33
// Senza cache delle miniature, o con una foto che non si riduce, la miniatura ripiega sulla foto
// intera e lo dice.
func TestMiniaturaRipiegaSullaFoto(t *testing.T) {
	b, p := conFoto(t)
	f := allega(t, b, p.busta, []Caricamento{{p.input, "a.png"}})[0]
	m, err := b.LeggiFotoPersonale(ctx, itoa(p.busta), itoa(f.ID), VarianteMiniatura, chi)
	if err != nil || m == nil || m.Variante != VarianteIntera || m.Tipo != media.TipoJPEG {
		t.Fatalf("%+v %v", m, err)
	}
	if got := ultimoAudit(t, p.b.d, "repair.staff_photo_viewed"); !strings.HasSuffix(got, `|ok|{"photo_id":`+itoa(f.ID)+`,"variant":"full"}`) {
		t.Errorf("audit: %s", got)
	}
}

// inventario: RIP-S-33
// Busta nel cestino, che non c'è, foto di un'altra busta, id non positivi: nessun byte e una riga
// «denied» con l'id ripulito («invalid» per un id che non è un intero positivo).
func TestLeggiFotoRifiutata(t *testing.T) {
	b, p := conMiniature(t)
	f := allega(t, b, p.busta, []Caricamento{{p.input, "a.png"}})[0]
	altra := crea(t, b, modulo()).ID
	for _, c := range []struct{ busta, foto, id, dati string }{
		{itoa(altra), itoa(f.ID), itoa(altra), `{"photo_id":` + itoa(f.ID) + `}`},
		{"999", itoa(f.ID), "999", `{"photo_id":` + itoa(f.ID) + `}`},
		{itoa(p.busta), "0", itoa(p.busta), `{"photo_id":"invalid"}`},
		{itoa(p.busta), "+x", itoa(p.busta), `{"photo_id":"invalid"}`},
		{"<script>", itoa(f.ID), "invalid", `{"photo_id":` + itoa(f.ID) + `}`},
		{"99999999999999999999", "-1", "invalid", `{"photo_id":"invalid"}`},
	} {
		m, err := b.LeggiFotoPersonale(ctx, c.busta, c.foto, VarianteIntera, chi)
		if err != nil || m != nil {
			t.Errorf("%+v: %+v %v", c, m, err)
		}
		if got := ultimoAudit(t, p.b.d, "repair.staff_photo_viewed"); got != "repair.staff_photo_viewed|repair|"+c.id+"|denied|"+c.dati {
			t.Errorf("%+v: %s", c, got)
		}
	}
	esegui(t, p.b.d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, p.busta)
	if m, err := b.LeggiFotoPersonale(ctx, itoa(p.busta), itoa(f.ID), VarianteMiniatura, chi); err != nil || m != nil {
		t.Errorf("cestino: %+v %v", m, err)
	}
	if got := ultimoAudit(t, p.b.d, "repair.staff_photo_viewed"); got != "repair.staff_photo_viewed|repair|"+itoa(p.busta)+`|denied|{"photo_id":`+itoa(f.ID)+`,"variant":"thumb"}` {
		t.Errorf("cestino: %s", got)
	}
}

// inventario: RIP-S-33
// La miniatura coperta dal gettone della galleria non scrive righe; quella che ripiega sulla foto
// intera, o che è rifiutata, si legge come read_staff_media/4 con la sua riga.
func TestMiniaturaCoperta(t *testing.T) {
	b, p := conMiniature(t)
	in := scriviFile(t, p.dir, "grande.png", sintetiche.PNG(800, 600, 2))
	f := allega(t, b, p.busta, []Caricamento{{in, "grande.png"}})[0]
	prima := conta(t, p.b.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.staff_photo_viewed'`)
	m, err := b.LeggiMiniaturaCoperta(ctx, itoa(p.busta), itoa(f.ID), chi)
	if err != nil || m == nil || m.Variante != VarianteMiniatura {
		t.Fatalf("%+v %v", m, err)
	}
	if n := conta(t, p.b.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.staff_photo_viewed'`); n != prima {
		t.Errorf("righe %d", n-prima)
	}
	k, err := media.Nuovo(p.mediaDi).Salva([]byte("non un'immagine"))
	if err != nil {
		t.Fatal(err)
	}
	esegui(t, p.b.d, `UPDATE foto_buste SET chiave = ?, sha256 = ? WHERE id = ?`, k, mustHex(t, k), f.ID)
	m, err = b.LeggiMiniaturaCoperta(ctx, itoa(p.busta), itoa(f.ID), chi)
	if err != nil || m == nil || m.Variante != VarianteIntera {
		t.Fatalf("ripiego: %+v %v", m, err)
	}
	if got := ultimoAudit(t, p.b.d, "repair.staff_photo_viewed"); got != "repair.staff_photo_viewed|repair|"+itoa(p.busta)+`|ok|{"photo_id":`+itoa(f.ID)+`,"variant":"full"}` {
		t.Errorf("ripiego: %s", got)
	}
	if m, err := b.LeggiMiniaturaCoperta(ctx, itoa(p.busta), "999", chi); err != nil || m != nil {
		t.Errorf("rifiuto: %+v %v", m, err)
	}
	if got := ultimoAudit(t, p.b.d, "repair.staff_photo_viewed"); got != "repair.staff_photo_viewed|repair|"+itoa(p.busta)+`|denied|{"photo_id":999,"variant":"thumb"}` {
		t.Errorf("rifiuto: %s", got)
	}
}

// inventario: RIP-S-33
// Una riga d'audit che non si scrive è un errore e non dà byte; senza archivio, ErrSenzaArchivio.
func TestLetturaSenzaAudit(t *testing.T) {
	b, p := conMiniature(t)
	f := allega(t, b, p.busta, []Caricamento{{p.input, "a.png"}})[0]
	esegui(t, p.b.d, `CREATE TRIGGER ferma BEFORE INSERT ON eventi_audit BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if m, err := b.LeggiFotoPersonale(ctx, itoa(p.busta), itoa(f.ID), VarianteIntera, chi); err == nil || m != nil {
		t.Errorf("%+v %v", m, err)
	}
	senza, _, _ := prova(t)
	if _, err := senza.LeggiFotoPersonale(ctx, "1", "1", VarianteIntera, audit.Contesto{}); !errors.Is(err, ErrSenzaArchivio) {
		t.Errorf("senza archivio: %v", err)
	}
}

func mustHex(t *testing.T, k string) []byte {
	t.Helper()
	b := make([]byte, 0, 32)
	for i := 0; i < len(k); i += 2 {
		var v byte
		for _, c := range k[i : i+2] {
			v <<= 4
			switch {
			case c >= '0' && c <= '9':
				v |= byte(c - '0')
			case c >= 'a' && c <= 'f':
				v |= byte(c-'a') + 10
			default:
				t.Fatalf("chiave %q", k)
			}
		}
		b = append(b, v)
	}
	return b
}

type riscaldatoreFinto struct{ chiavi [][]string }

func (r *riscaldatoreFinto) Scalda(k []string) { r.chiavi = append(r.chiavi, k) }

// inventario: RIP-S-34
// Dopo un caricamento le miniature delle foto nuove si preparano subito (warm_previews/1 di
// attach_many/3); un caricamento rifiutato non prepara niente. ChiaviFoto dà le chiavi di tutte
// le foto delle buste, le più recenti prima, senza ripetizioni (ThumbnailWarmer.keys/0).
func TestCaricamentoPreparaLeMiniature(t *testing.T) {
	b, p := conFoto(t)
	r := &riscaldatoreFinto{}
	b.ConRiscaldatore(r)
	f := allega(t, b, p.busta, p.due())
	if len(r.chiavi) != 1 || len(r.chiavi[0]) != 2 || r.chiavi[0][0] != f[0].Chiave || r.chiavi[0][1] != f[1].Chiave {
		t.Errorf("preparate %v", r.chiavi)
	}
	if _, err := b.AllegaFoto(ctx, p.busta, []Caricamento{{p.input, "a.gif"}}, nil, chi); err == nil || len(r.chiavi) != 1 {
		t.Errorf("rifiuto: %v %v", err, r.chiavi)
	}
	in := scriviFile(t, p.dir, "altra.png", sintetiche.PNG(30, 20, 9))
	g := allega(t, b, p.busta, []Caricamento{{in, "altra.png"}})
	k, err := b.ChiaviFoto(ctx)
	if err != nil || len(k) != 2 || k[0] != g[0].Chiave || k[1] != f[0].Chiave {
		t.Errorf("chiavi %v %v", k, err)
	}
}
