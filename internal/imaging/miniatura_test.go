package imaging

import (
	"bytes"
	"errors"
	"image"
	"testing"

	"github.com/L0cutus/gestrip/internal/imaging/sintetiche"
	"github.com/gen2brain/webp"
	xwebp "golang.org/x/image/webp"
)

// I casi di media_store_thumbnail_test.exs dello standalone che riguardano la conversione (la cache
// è della slice della rotta delle foto).

// salvata è quello che il caricamento salva: la foto normalizzata.
func salvata(t *testing.T, l, a int, seme uint32) []byte {
	t.Helper()
	out, err := Normalizza(sintetiche.PNG(l, a, seme))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func miniatura(t *testing.T, in []byte) (int, int, []byte) {
	t.Helper()
	m, err := Miniatura(in)
	if err != nil {
		t.Fatalf("Miniatura: %v", err)
	}
	l, a, ok := sintetiche.DimensioniWebP(m)
	if !ok {
		t.Fatalf("non è un WebP con perdita semplice: % x", m[:min(len(m), 16)])
	}
	return l, a, m
}

// inventario: RIP-S-33
// Dalla foto salvata un WebP piccolo che sta nel riquadro di 400 px, con perdita: pesa meno di un
// decimo della foto.
func TestMiniaturaWebPNelRiquadro(t *testing.T) {
	orig := salvata(t, 1000, 750, 1)
	l, a, m := miniatura(t, orig)
	if l != 400 || a != 300 {
		t.Errorf("%d×%d, attese 400×300", l, a)
	}
	if len(m)*10 >= len(orig) {
		t.Errorf("miniatura di %d byte per una foto di %d", len(m), len(orig))
	}
	if _, err := xwebp.Decode(bytes.NewReader(m)); err != nil {
		t.Errorf("la miniatura non si decodifica: %v", err)
	}
}

// inventario: RIP-S-33
// La foto più grande che si salva (4096 × 4096) e un PNG vecchio hanno la miniatura; una foto
// piccola non si ingrandisce.
func TestMiniaturaGrandeVecchiaEPiccola(t *testing.T) {
	grande, err := Normalizza(sintetiche.JPEG(sintetiche.Liscio(4096, 4096), sintetiche.OpzioniJPEG{}))
	if err != nil {
		t.Fatal(err)
	}
	if i, _ := sintetiche.LeggiJPEG(grande); i.Larghezza != 4096 || i.Altezza != 4096 {
		t.Fatalf("salvata %d×%d", i.Larghezza, i.Altezza)
	}
	if l, a, _ := miniatura(t, grande); l != 400 || a != 400 {
		t.Errorf("4096²: %d×%d", l, a)
	}
	if l, a, _ := miniatura(t, sintetiche.PNG(1000, 750, 7)); l != 400 || a != 300 {
		t.Errorf("PNG vecchio: %d×%d", l, a)
	}
	if l, a, _ := miniatura(t, salvata(t, 120, 80, 3)); l != 120 || a != 80 {
		t.Errorf("piccola: %d×%d", l, a)
	}
}

// inventario: RIP-S-33
// Una foto vecchia con l'orientamento EXIF (importata) esce raddrizzata, come exif_transpose nello
// script della miniatura.
func TestMiniaturaRaddrizza(t *testing.T) {
	j := sintetiche.JPEG(sintetiche.Gradiente(800, 400, 1), sintetiche.OpzioniJPEG{Orientamento: 8})
	if l, a, _ := miniatura(t, j); l != 200 || a != 400 {
		t.Errorf("%d×%d, attese 200×400", l, a)
	}
}

// inventario: RIP-S-33
// Niente miniatura per ciò che non si decodifica, per una foto oltre 16 MiB e oltre 4096 × 4096
// pixel (MAX_IMAGE_PIXELS dello script).
func TestMiniaturaRifiuti(t *testing.T) {
	for _, c := range []struct {
		cosa string
		in   []byte
	}{
		{"non un'immagine", []byte("not an image at all")},
		{"vuota", nil},
		{"oltre 16 MiB", sintetiche.JPEG(sintetiche.Gradiente(64, 64, 1), sintetiche.OpzioniJPEG{Pad: MaxByteUscita + 1})},
		{"oltre 4096² pixel", sintetiche.CodificaPNG(image.NewGray(image.Rect(0, 0, 4097, 4096)))},
	} {
		if m, err := Miniatura(c.in); !errors.Is(err, ErrImmagine) || m != nil {
			t.Errorf("%s: %d byte, %v", c.cosa, len(m), err)
		}
	}
	if _, _, m := miniatura(t, sintetiche.JPEG(sintetiche.Gradiente(64, 64, 1), sintetiche.OpzioniJPEG{Pad: MaxByteUscita})); len(m) == 0 {
		t.Error("16 MiB esatti: niente miniatura")
	}
	if _, _, m := miniatura(t, sintetiche.CodificaPNG(image.NewGray(image.Rect(0, 0, 4096, 4096)))); len(m) == 0 {
		t.Error("4096² esatti: niente miniatura")
	}
}

// inventario: RIP-S-33
// Il WebP si scrive con il libwebp tradotto in Go, compilato senza il caricamento dinamico: con
// -tags nodynamic gen2brain/webp dice proprio «dynamic disabled» (senza il tag direbbe perché la
// libreria di sistema non si è caricata, o niente se l'ha caricata). La guardia sul binario intero
// è TestBinarioStatico in cmd/gestrip.
func TestWebPSenzaLibreriaDiSistema(t *testing.T) {
	if err := webp.Dynamic(); err == nil || err.Error() != "webp: dynamic disabled" {
		t.Fatalf("webp.Dynamic() = %v, atteso «webp: dynamic disabled» (-tags nodynamic)", err)
	}
}
