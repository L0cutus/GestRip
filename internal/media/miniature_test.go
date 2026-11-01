package media

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/imaging"
	"github.com/L0cutus/gestrip/internal/imaging/sintetiche"
)

// I casi di media_store_thumbnail_test.exs e thumbnail_cache_test.exs dello standalone a
// go-baseline (la parte della cache; la cancellazione con la sua foto è della pulizia, slice 041).

// inProcesso fa la miniatura nel processo del test (in produzione la fa il figlio,
// imaging.Lavoratore) e conta le volte.
type inProcesso struct{ volte atomic.Int32 }

func (p *inProcesso) Miniatura(_ context.Context, in []byte) ([]byte, error) {
	p.volte.Add(1)
	return imaging.Miniatura(in)
}

type provaMiniature struct {
	base, media, cache string
	a                  *Archivio
	m                  *Miniature
	fai                *inProcesso
}

func nuoveProva(t *testing.T) *provaMiniature {
	t.Helper()
	base := t.TempDir()
	p := &provaMiniature{base: base, media: filepath.Join(base, Cartella), cache: filepath.Join(base, CartellaMiniature), fai: &inProcesso{}}
	p.a = Nuovo(p.media)
	p.m = NuoveMiniature(p.a, p.cache, p.fai)
	return p
}

// salvata è quello che il caricamento salva: il JPEG normalizzato, con la sua impronta per nome.
func (p *provaMiniature) salvata(t *testing.T, seme uint32, l, a int) (string, []byte) {
	t.Helper()
	b, err := imaging.Normalizza(sintetiche.PNG(l, a, seme))
	if err != nil {
		t.Fatal(err)
	}
	k, err := p.a.Salva(b)
	if err != nil {
		t.Fatal(err)
	}
	return k, b
}

func (p *provaMiniature) voce(k string) string { return filepath.Join(p.cache, k+".webp") }

func nomiIn(t *testing.T, dir string) []string {
	t.Helper()
	v, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var n []string
	for _, e := range v {
		n = append(n, e.Name())
	}
	slices.Sort(n)
	return n
}

// inventario: RIP-S-33
// Dalla foto salvata si fa un piccolo WebP che sta nel riquadro di 400 px, molto più leggero della
// foto; una foto piccola non si ingrandisce.
func TestMiniaturaDallaFotoSalvata(t *testing.T) {
	p := nuoveProva(t)
	k, orig := p.salvata(t, 1, 1000, 750)
	m, err := p.m.Miniatura(t.Context(), k)
	if err != nil {
		t.Fatal(err)
	}
	if l, a, ok := sintetiche.DimensioniWebP(m); !ok || l != 400 || a != 300 {
		t.Errorf("miniatura %d×%d %v", l, a, ok)
	}
	if len(m)*10 >= len(orig) {
		t.Errorf("miniatura di %d byte, foto di %d", len(m), len(orig))
	}
	piccola, _ := p.salvata(t, 2, 120, 80)
	m, err = p.m.Miniatura(t.Context(), piccola)
	if err != nil {
		t.Fatal(err)
	}
	if l, a, ok := sintetiche.DimensioniWebP(m); !ok || l != 120 || a != 80 {
		t.Errorf("foto piccola: %d×%d %v", l, a, ok)
	}
}

// inventario: RIP-S-34
// La prima richiesta scrive una voce nella cache e la seguente la legge: una miniatura valida
// piantata al suo posto è quella che si serve, senza rifarla.
func TestMiniaturaPrimaVoltaInCache(t *testing.T) {
	p := nuoveProva(t)
	k, _ := p.salvata(t, 1, 1000, 750)
	altra, _ := p.salvata(t, 3, 800, 800)
	if _, err := os.Lstat(p.voce(k)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("voce prima della richiesta: %v", err)
	}
	m, err := p.m.Miniatura(t.Context(), k)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(p.voce(k)); err != nil || !bytes.Equal(b, m) {
		t.Errorf("voce: %v", err)
	}
	if n := nomiIn(t, p.cache); !slices.Equal(n, []string{k + ".webp"}) {
		t.Errorf("cache: %v", n)
	}
	piantata, err := p.m.Miniatura(t.Context(), altra)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.voce(k), piantata, 0o600); err != nil {
		t.Fatal(err)
	}
	volte := p.fai.volte.Load()
	if m, err := p.m.Miniatura(t.Context(), k); err != nil || !bytes.Equal(m, piantata) {
		t.Errorf("seconda richiesta: %v, piantata %v", err, bytes.Equal(m, piantata))
	}
	if p.fai.volte.Load() != volte {
		t.Error("la seconda richiesta ha rifatto la miniatura")
	}
}

// inventario: RIP-S-34
// La cartella della cache è solo dell'app: 0700 anche se c'era già con altri permessi.
func TestCartellaMiniaturePrivata(t *testing.T) {
	p := nuoveProva(t)
	k, _ := p.salvata(t, 1, 1000, 750)
	if err := os.MkdirAll(p.cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p.cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := p.m.Miniatura(t.Context(), k); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p.cache)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("cartella %v %v", fi.Mode(), err)
	}
}

// inventario: RIP-S-34
// Una voce guasta (vuota, non WebP, tagliata, con byte in più) si rifà, mai servita.
func TestVoceGuastaRifatta(t *testing.T) {
	p := nuoveProva(t)
	k, _ := p.salvata(t, 1, 1000, 750)
	m, err := p.m.Miniatura(t.Context(), k)
	if err != nil {
		t.Fatal(err)
	}
	for _, guasta := range [][]byte{{}, []byte("not a webp"), m[:20], append(slices.Clone(m), []byte("trailing")...)} {
		if err := os.WriteFile(p.voce(k), guasta, 0o600); err != nil {
			t.Fatal(err)
		}
		if got, err := p.m.Miniatura(t.Context(), k); err != nil || !bytes.Equal(got, m) {
			t.Errorf("voce guasta di %d byte: %v", len(guasta), err)
		}
		if b, err := os.ReadFile(p.voce(k)); err != nil || !bytes.Equal(b, m) {
			t.Errorf("voce guasta di %d byte non rifatta", len(guasta))
		}
	}
}

// inventario: RIP-S-33
// Una foto che non si decodifica non ha miniatura e non lascia voci; una chiave sconosciuta,
// malformata o di un file manomesso nemmeno.
func TestNessunaMiniaturaSenzaFotoBuona(t *testing.T) {
	p := nuoveProva(t)
	k, err := p.a.Salva([]byte("not an image at all"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.m.Miniatura(t.Context(), k); !errors.Is(err, ErrMiniatura) {
		t.Errorf("non un'immagine: %v", err)
	}
	if v, err := os.ReadDir(p.cache); err == nil && len(v) > 0 {
		t.Errorf("voci dopo un rifiuto: %v", v)
	}
	buona, _ := p.salvata(t, 1, 1000, 750)
	for _, c := range []string{strings.Repeat("0", 64), "../" + buona, "", strings.ToUpper(buona)} {
		if _, err := p.m.Miniatura(t.Context(), c); !errors.Is(err, ErrMiniatura) {
			t.Errorf("%q: %v", c, err)
		}
	}
	percorso := filepath.Join(p.media, buona)
	if err := os.Chmod(percorso, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(percorso, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.m.Miniatura(t.Context(), buona); !errors.Is(err, ErrMiniatura) {
		t.Errorf("manomessa: %v", err)
	}
}

// inventario: RIP-S-34
// Una miniatura in cache non sopravvive alla sua foto: tolta la foto, niente miniatura.
func TestMiniaturaNonSopravviveAllaFoto(t *testing.T) {
	p := nuoveProva(t)
	k, _ := p.salvata(t, 1, 1000, 750)
	if _, err := p.m.Miniatura(t.Context(), k); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(p.voce(k)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(p.media, k)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.m.Miniatura(t.Context(), k); !errors.Is(err, ErrMiniatura) {
		t.Errorf("foto tolta: %v", err)
	}
}

// inventario: RIP-S-34
// Una cache che non si può scrivere (un file al posto della cartella) risponde lo stesso, senza
// tenere niente.
func TestCacheNonScrivibileRispondeLoStesso(t *testing.T) {
	p := nuoveProva(t)
	k, _ := p.salvata(t, 1, 1000, 750)
	if err := os.WriteFile(p.cache, []byte("a file where the directory should be"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := p.m.Miniatura(t.Context(), k)
	if err != nil {
		t.Fatal(err)
	}
	if l, a, ok := sintetiche.DimensioniWebP(m); !ok || l != 400 || a != 300 {
		t.Errorf("miniatura %d×%d", l, a)
	}
	if b, err := os.ReadFile(p.cache); err != nil || string(b) != "a file where the directory should be" {
		t.Errorf("il file al posto della cartella è cambiato: %v", err)
	}
}

// inventario: RIP-S-34
// La cache ha un limite: piena, si svuota tutta prima della voce dopo.
func TestCachePienaSiSvuota(t *testing.T) {
	p := nuoveProva(t)
	p.m.Max = 2
	var k []string
	for seme := uint32(1); seme <= 3; seme++ {
		c, _ := p.salvata(t, seme, 500, 500)
		k = append(k, c)
	}
	for _, c := range k[:2] {
		if _, err := p.m.Miniatura(t.Context(), c); err != nil {
			t.Fatal(err)
		}
	}
	attese := []string{k[0] + ".webp", k[1] + ".webp"}
	slices.Sort(attese)
	if n := nomiIn(t, p.cache); !slices.Equal(n, attese) {
		t.Errorf("cache: %v", n)
	}
	if _, err := p.m.Miniatura(t.Context(), k[2]); err != nil {
		t.Fatal(err)
	}
	if n := nomiIn(t, p.cache); !slices.Equal(n, []string{k[2] + ".webp"}) {
		t.Errorf("cache dopo il limite: %v", n)
	}
}

// inventario: RIP-S-34
// Il limite di produzione è quello di config/runtime.exs dello standalone (20 000 voci).
func TestLimiteDellaCache(t *testing.T) {
	if MaxMiniature != 20_000 {
		t.Errorf("MaxMiniature %d", MaxMiniature)
	}
	if got := MiniatureDi("/dati/gestrip.db"); got != "/dati/miniature" {
		t.Errorf("cartella: %q", got)
	}
}

// inventario: RIP-S-34
// La pulizia toglie le miniature delle foto che non ci sono più e i temporanei lasciati da un
// arresto, e tiene il resto; una cache che non c'è ancora non è un errore.
func TestPuliziaDelleMiniature(t *testing.T) {
	p := nuoveProva(t)
	if err := p.m.Pulisci(); err != nil {
		t.Errorf("cache che non c'è: %v", err)
	}
	tenuta, _ := p.salvata(t, 3, 800, 600)
	if _, err := p.m.Miniatura(t.Context(), tenuta); err != nil {
		t.Fatal(err)
	}
	orfana := strings.Repeat("a", 64)
	for nome, b := range map[string]string{orfana + ".webp": "RIFF", ".tmp-leftover": "half", "altro.txt": "x", strings.ToUpper(tenuta) + ".webp": "y"} {
		if err := os.WriteFile(filepath.Join(p.cache, nome), []byte(b), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.m.Pulisci(); err != nil {
		t.Fatal(err)
	}
	if n := nomiIn(t, p.cache); !slices.Equal(n, []string{tenuta + ".webp"}) {
		t.Errorf("cache dopo la pulizia: %v", n)
	}
}

// inventario: RIP-S-34
// Prepara (warm_thumbnail/1): la miniatura che manca si fa, quella che c'è resta, una foto senza
// miniatura possibile è un errore; Togli toglie la voce di una foto.
func TestPreparaETogli(t *testing.T) {
	p := nuoveProva(t)
	k, _ := p.salvata(t, 4, 800, 600)
	if got := p.m.Prepara(t.Context(), k); got != PreparazioneFatta {
		t.Errorf("prima: %v", got)
	}
	if got := p.m.Prepara(t.Context(), k); got != PreparazioneGiaPronta {
		t.Errorf("seconda: %v", got)
	}
	rotta, err := p.a.Salva([]byte("not an image"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{rotta, "../x", strings.Repeat("b", 64)} {
		if got := p.m.Prepara(t.Context(), c); got != PreparazioneErrore {
			t.Errorf("%q: %v", c, got)
		}
	}
	p.m.Togli(k)
	if _, err := os.Lstat(p.voce(k)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("voce dopo Togli: %v", err)
	}
	p.m.Togli("../../fuori") // una chiave che non è una chiave non tocca niente
}

// inventario: RIP-S-34
// warm_all: si preparano tutte le foto che non hanno la miniatura, una volta sola.
func TestPreparaTutte(t *testing.T) {
	p := nuoveProva(t)
	a, _ := p.salvata(t, 4, 800, 600)
	b, _ := p.salvata(t, 5, 800, 600)
	if n := p.m.PreparaTutte(t.Context(), []string{a, b, a}); n != 2 {
		t.Errorf("prima passata: %d", n)
	}
	if n := p.m.PreparaTutte(t.Context(), []string{a, b}); n != 0 {
		t.Errorf("seconda passata: %d", n)
	}
}

// inventario: RIP-S-34
// Il riscaldatore (ThumbnailWarmer): all'avvio pulisce la cache e prepara le miniature che
// mancano, le più recenti prima; dopo un caricamento prepara le foto nuove senza una richiesta.
// Senza riscaldatore Scalda non fa niente.
func TestRiscaldatore(t *testing.T) {
	p := nuoveProva(t)
	vecchia, _ := p.salvata(t, 6, 800, 600)
	orfana := filepath.Join(p.cache, strings.Repeat("c", 64)+".webp")
	if err := os.MkdirAll(p.cache, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orfana, []byte("RIFF"), 0o600); err != nil {
		t.Fatal(err)
	}
	var nessuno *Riscaldatore
	nessuno.Scalda([]string{vecchia})
	r := NuovoRiscaldatore(p.m)
	ctx, ferma := context.WithCancel(t.Context())
	fatto := make(chan struct{})
	go func() {
		defer close(fatto)
		r.Lavoro(ctx, func(context.Context) ([]string, error) { return []string{vecchia}, nil }, nil)
	}()
	nuova, _ := p.salvata(t, 7, 800, 600)
	r.Scalda([]string{nuova})
	if !presto(func() bool { return esiste(p.voce(vecchia)) && esiste(p.voce(nuova)) }) {
		t.Error("le miniature non sono state preparate")
	}
	if esiste(orfana) {
		t.Error("la pulizia dell'avvio non ha tolto la voce orfana")
	}
	ferma()
	<-fatto
}

func esiste(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func presto(f func() bool) bool {
	for range 200 {
		if f() {
			return true
		}
		attendi()
	}
	return false
}

// miniaturistaFinto fa la miniatura con f.
type miniaturistaFinto func(ctx context.Context, in []byte) ([]byte, error)

func (f miniaturistaFinto) Miniatura(ctx context.Context, in []byte) ([]byte, error) {
	return f(ctx, in)
}

// inventario: RIP-S-33
// La miniatura si fa solo dalla foto verificata (read_object/1): una foto sostituita con un'altra
// immagine valida (impronta diversa dal nome) non ha miniatura e non lascia voci.
func TestMiniaturaSoloDallaFotoVerificata(t *testing.T) {
	p := nuoveProva(t)
	k, _ := p.salvata(t, 1, 1000, 750)
	percorso := filepath.Join(p.media, k)
	if err := os.Chmod(percorso, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(percorso, sintetiche.PNG(300, 200, 9), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.m.Miniatura(t.Context(), k); !errors.Is(err, ErrMiniatura) {
		t.Errorf("foto sostituita: %v", err)
	}
	if esiste(p.voce(k)) {
		t.Error("voce di una foto non verificata")
	}
}

// inventario: RIP-S-34
// Se la foto sparisce mentre si fa la miniatura, la voce non resta (keep_thumbnail/2).
func TestFotoSparitaDuranteLaMiniatura(t *testing.T) {
	p := nuoveProva(t)
	k, _ := p.salvata(t, 1, 800, 600)
	m := NuoveMiniature(p.a, p.cache, miniaturistaFinto(func(_ context.Context, in []byte) ([]byte, error) {
		if err := os.Remove(filepath.Join(p.media, k)); err != nil {
			return nil, err
		}
		return imaging.Miniatura(in)
	}))
	if _, err := m.Miniatura(t.Context(), k); err != nil {
		t.Fatal(err)
	}
	if esiste(p.voce(k)) {
		t.Error("la voce sopravvive alla foto")
	}
}

// inventario: RIP-S-34
// Le difese della cache: voce 0600; cartella della cache che è un link (non si scrive dove punta);
// voce che è un link a un WebP valido (non si serve, si rifà); una chiave che non è una chiave in
// Miniatura e in Togli (niente fuori dalla cartella); un risultato del figlio che non è un WebP
// intero (non si serve né si tiene); una richiesta annullata ferma il figlio.
func TestDifeseDellaCache(t *testing.T) {
	t.Run("voce 0600", func(t *testing.T) {
		p := nuoveProva(t)
		k, _ := p.salvata(t, 1, 800, 600)
		if _, err := p.m.Miniatura(t.Context(), k); err != nil {
			t.Fatal(err)
		}
		if fi, err := os.Lstat(p.voce(k)); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("voce %v %v", fi.Mode(), err)
		}
	})
	t.Run("cartella link", func(t *testing.T) {
		p := nuoveProva(t)
		k, _ := p.salvata(t, 1, 800, 600)
		altrove := filepath.Join(p.base, "altrove")
		if err := os.Mkdir(altrove, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(altrove, p.cache); err != nil {
			t.Fatal(err)
		}
		if _, err := p.m.Miniatura(t.Context(), k); err != nil {
			t.Fatal(err)
		}
		if n := nomiIn(t, altrove); len(n) != 0 {
			t.Errorf("scritto dove punta il link: %v", n)
		}
		if fi, err := os.Stat(altrove); err != nil || fi.Mode().Perm() != 0o755 {
			t.Errorf("permessi cambiati dove punta il link: %v %v", fi.Mode(), err)
		}
	})
	t.Run("voce link", func(t *testing.T) {
		p := nuoveProva(t)
		k, _ := p.salvata(t, 1, 800, 600)
		vera, err := p.m.Miniatura(t.Context(), k)
		if err != nil {
			t.Fatal(err)
		}
		altra := filepath.Join(p.base, "altra.webp")
		if err := os.WriteFile(altra, sintetiche.WebP(30, 20, 1), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(p.voce(k)); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(altra, p.voce(k)); err != nil {
			t.Fatal(err)
		}
		if got, err := p.m.Miniatura(t.Context(), k); err != nil || !bytes.Equal(got, vera) {
			t.Errorf("voce link: servito il bersaglio (%v)", err)
		}
	})
	t.Run("chiave fuori dalla cartella", func(t *testing.T) {
		p := nuoveProva(t)
		k, _ := p.salvata(t, 1, 800, 600)
		// Una «chiave» che risale nell'archivio: la sua foto c'è e la «voce» sarebbe media/<k>.webp.
		if err := os.WriteFile(filepath.Join(p.media, k+".webp"), sintetiche.WebP(30, 20, 1), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := p.m.Miniatura(t.Context(), "../"+Cartella+"/"+k); !errors.Is(err, ErrMiniatura) {
			t.Errorf("chiave che risale: %v", err)
		}
		fuori := filepath.Join(p.base, "fuori.webp")
		if err := os.WriteFile(fuori, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(p.cache, 0o700); err != nil {
			t.Fatal(err)
		}
		p.m.Togli("../fuori")
		if !esiste(fuori) {
			t.Error("Togli ha tolto un file fuori dalla cache")
		}
	})
	t.Run("risultato non WebP", func(t *testing.T) {
		p := nuoveProva(t)
		k, _ := p.salvata(t, 1, 800, 600)
		m := NuoveMiniature(p.a, p.cache, miniaturistaFinto(func(context.Context, []byte) ([]byte, error) {
			return []byte("RIFF\x04\x00\x00\x00WEBPnon"), nil
		}))
		if _, err := m.Miniatura(t.Context(), k); !errors.Is(err, ErrMiniatura) {
			t.Errorf("non WebP: %v", err)
		}
		if esiste(p.voce(k)) {
			t.Error("voce di un risultato non WebP")
		}
	})
	t.Run("richiesta annullata", func(t *testing.T) {
		p := nuoveProva(t)
		k, _ := p.salvata(t, 1, 800, 600)
		fermato := false
		m := NuoveMiniature(p.a, p.cache, miniaturistaFinto(func(ctx context.Context, _ []byte) ([]byte, error) {
			select {
			case <-ctx.Done():
				fermato = true
				return nil, ctx.Err()
			case <-time.After(2 * time.Second):
				return nil, errors.New("il figlio non è stato fermato")
			}
		}))
		ctx, annulla := context.WithCancel(t.Context())
		annulla()
		if _, err := m.Miniatura(ctx, k); !errors.Is(err, context.Canceled) || !fermato {
			t.Errorf("annullata: %v, figlio fermato %v", err, fermato)
		}
	})
}
