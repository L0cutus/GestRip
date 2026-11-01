package media

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/L0cutus/gestrip/internal/imaging"
)

// La cache delle miniature (RIP-S-33, RIP-S-34), come MediaStore.thumbnail/1, warm_thumbnail/1,
// sweep_thumbnails/0, keep_thumbnail/2 e make_room/1 dello standalone a go-baseline, e il
// ThumbnailWarmer.
//
// La miniatura è una risposta **derivata**, mai una seconda copia della foto: la foto si legge e non
// si riscrive, la voce ha per nome l'impronta della foto (<chiave>.webp) e qui niente sa chi può
// vedere cosa (lo ha già chiesto buste.Visibile). La cache sta accanto al database, fuori
// dall'archivio media (quindi fuori dalle esportazioni e dalla pulizia degli orfani), e niente ne
// dipende: una voce va con la sua foto (Togli), Pulisci toglie quello che un arresto o un ripristino
// hanno lasciato, una voce che non è un WebP intero si rifà e non si serve mai, piena si svuota
// tutta prima della voce dopo, e se non si può scrivere la miniatura si dà lo stesso, senza tenerla.

// CartellaMiniature è il nome della cartella delle miniature accanto al database (nello
// standalone «thumbnails», config/runtime.exs).
const CartellaMiniature = "miniature"

// MaxMiniature è quante voci la cache tiene prima di svuotarsi: thumbnail_cache_max_entries di
// produzione nello standalone (config/runtime.exs; 2000 è solo il valore di sviluppo e test).
const MaxMiniature = 20_000

// maxByteVoce è la voce più grande che si legge: una miniatura pesa al più 4 MiB
// (thumbnail_image.py); una voce più grande non è una miniatura e si rifà.
const maxByteVoce = 4 * 1024 * 1024

// ErrMiniatura: nessuna miniatura per questa chiave (:thumbnail_failed): foto che manca, guasta o
// che non si decodifica. Chi chiama decide cosa servire al suo posto. Una miniatura che il figlio non
// fa adesso per le risorse non è ErrMiniatura ma imaging.ErrRisorse (slice 037c): chi chiama ripiega
// allo stesso modo, ma la foto non è guasta.
var ErrMiniatura = errors.New("media: miniatura non disponibile")

// MiniatureDi è la cartella delle miniature che va con il database db.
func MiniatureDi(db string) string { return filepath.Join(filepath.Dir(db), CartellaMiniature) }

// Miniaturista fa la miniatura dai byte di una foto salvata: imaging.Lavoratore in produzione
// (processo figlio, mai nel processo che serve le pagine).
type Miniaturista interface {
	Miniatura(ctx context.Context, in []byte) ([]byte, error)
}

// Miniature è la cache delle miniature delle foto dell'archivio.
type Miniature struct {
	archivio *Archivio
	dir      string
	fai      Miniaturista
	// Max è quante voci si tengono; 0 vuol dire MaxMiniature.
	Max int
	// Registro riceve una riga per ogni miniatura non fatta: un avviso con la causa per le risorse
	// (imaging.ErrRisorse), una riga di debug per una foto rifiutata; mai la chiave né un percorso.
	// nil: niente righe.
	Registro *slog.Logger
}

// NuoveMiniature dà la cache nella cartella dir per le foto di a.
func NuoveMiniature(a *Archivio, dir string, f Miniaturista) *Miniature {
	return &Miniature{archivio: a, dir: dir, fai: f}
}

// Cartella è la cartella della cache.
func (m *Miniature) Cartella() string { return m.dir }

// Miniatura dà la miniatura della foto k (thumbnail/1): dalla cache se la voce c'è, è un WebP
// intero e la foto c'è ancora; altrimenti la fa dalla foto verificata (Leggi) e prova a tenerla.
// ErrMiniatura per una chiave malformata o una foto senza miniatura; imaging.ErrRisorse se il figlio
// non l'ha fatta adesso; l'errore del contesto se la richiesta se n'è andata.
func (m *Miniature) Miniatura(ctx context.Context, k string) ([]byte, error) {
	if !ChiaveValida(k) {
		return nil, ErrMiniatura
	}
	if b, ok := m.inCache(k); ok {
		return b, nil
	}
	return m.fa(ctx, k)
}

// fotoPresente dice se la foto k è ancora nell'archivio come file regolare: una miniatura non
// sopravvive alla sua foto.
func (m *Miniature) fotoPresente(k string) bool {
	fi, err := os.Lstat(filepath.Join(m.archivio.Cartella(), k))
	return err == nil && fi.Mode().IsRegular()
}

func (m *Miniature) voce(k string) string { return filepath.Join(m.dir, k+".webp") }

// inCache è cached_thumbnail/1.
func (m *Miniature) inCache(k string) ([]byte, bool) {
	if !m.fotoPresente(k) {
		return nil, false
	}
	f, err := ApriInLettura(m.voce(k))
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }() // in sola lettura: niente da perdere
	b, err := io.ReadAll(io.LimitReader(f, maxByteVoce+1))
	if err != nil || len(b) > maxByteVoce || !webpIntero(b) {
		return nil, false
	}
	return b, true
}

// fa è build_thumbnail/1.
func (m *Miniature) fa(ctx context.Context, k string) ([]byte, error) {
	orig, err := m.archivio.Leggi(k)
	if err != nil {
		return nil, ErrMiniatura
	}
	if m.fai == nil {
		return nil, ErrMiniatura
	}
	b, err := m.fai.Miniatura(ctx, orig)
	if err != nil {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		if errors.Is(err, imaging.ErrRisorse) {
			m.registro().Warn("miniatura non fatta adesso", "causa", string(imaging.CausaDi(err)))
			return nil, err
		}
		m.registro().Debug("miniatura non fatta: foto rifiutata")
		return nil, ErrMiniatura
	}
	if !webpIntero(b) {
		return nil, ErrMiniatura
	}
	m.tieni(k, b)
	return b, nil
}

func (m *Miniature) registro() *slog.Logger {
	if m.Registro == nil {
		return slog.New(slog.DiscardHandler)
	}
	return m.Registro
}

// webpIntero è webp?/1: «RIFF», la dimensione del RIFF uguale ai byte che seguono, «WEBP».
func webpIntero(b []byte) bool {
	return len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP" &&
		int64(binary.LittleEndian.Uint32(b[4:8])) == int64(len(b))-8
}

// tieni è keep_thumbnail/2: cartella 0700, posto fatto, temporaneo «.tmp-…» rinominato nella voce;
// se nel frattempo la foto è sparita, la voce si toglie. Ogni errore lascia la cache com'era (la
// miniatura si dà lo stesso).
func (m *Miniature) tieni(k string, b []byte) {
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return
	}
	if fi, err := os.Lstat(m.dir); err != nil || !fi.IsDir() {
		return
	}
	if err := os.Chmod(m.dir, 0o700); err != nil {
		return
	}
	if err := m.faiPosto(); err != nil {
		return
	}
	var caso [12]byte
	if _, err := rand.Read(caso[:]); err != nil {
		return
	}
	tmp := filepath.Join(m.dir, ".tmp-"+base64.RawURLEncoding.EncodeToString(caso[:]))
	if err := scriviNuovo(tmp, b); err != nil {
		_ = os.Remove(tmp) // può non esserci: niente da fare
		return
	}
	if err := os.Rename(tmp, m.voce(k)); err != nil {
		_ = os.Remove(tmp) // il temporaneo resta solo se anche questo fallisce: lo toglie Pulisci
		return
	}
	if !m.fotoPresente(k) {
		_ = os.Remove(m.voce(k)) // la foto è stata tolta mentre si faceva la miniatura
	}
}

func scriviNuovo(p string, b []byte) error {
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	return errors.Join(err, f.Close())
}

// faiPosto è make_room/1: con Max voci .webp o più, la cache si svuota tutta (anche i temporanei).
func (m *Miniature) faiPosto() error {
	limite := m.Max
	if limite == 0 {
		limite = MaxMiniature
	}
	v, err := os.ReadDir(m.dir)
	if err != nil {
		return err
	}
	n := 0
	for _, e := range v {
		if strings.HasSuffix(e.Name(), ".webp") {
			n++
		}
	}
	if n >= limite {
		for _, e := range v {
			_ = os.Remove(filepath.Join(m.dir, e.Name())) // come File.rm: una voce che non si toglie resta
		}
	}
	return nil
}

// Preparazione è quello che Prepara ha fatto.
type Preparazione int

// I tre esiti di warm_thumbnail/1.
const (
	PreparazioneErrore    Preparazione = iota // :error, nessuna miniatura possibile
	PreparazioneGiaPronta                     // :cached
	PreparazioneFatta                         // :built
	PreparazioneRimandata                     // non fatta adesso per le risorse (imaging.ErrRisorse): non è una foto guasta
)

// Prepara è warm_thumbnail/1: la miniatura di k si fa se non è già in cache. Se il figlio non la fa
// adesso per le risorse, PreparazioneRimandata (nello standalone :error).
func (m *Miniature) Prepara(ctx context.Context, k string) Preparazione {
	if !ChiaveValida(k) {
		return PreparazioneErrore
	}
	if _, ok := m.inCache(k); ok {
		return PreparazioneGiaPronta
	}
	if _, err := m.fa(ctx, k); err != nil {
		if errors.Is(err, imaging.ErrRisorse) {
			return PreparazioneRimandata
		}
		return PreparazioneErrore
	}
	return PreparazioneFatta
}

// PreparaTutte è warm_all/0 sulle chiavi date (ripetute contano una volta): quante miniature ha
// fatto adesso.
func (m *Miniature) PreparaTutte(ctx context.Context, chiavi []string) int {
	n := 0
	visti := map[string]bool{}
	for _, k := range chiavi {
		if visti[k] || ctx.Err() != nil {
			continue
		}
		visti[k] = true
		if m.Prepara(ctx, k) == PreparazioneFatta {
			n++
		}
	}
	return n
}

var reVoce = regexp.MustCompile(`\A([0-9a-f]{64})\.webp\z`)

// Pulisci è sweep_thumbnails/0: toglie la miniatura di una foto che non c'è più e tutto il resto
// (un temporaneo lasciato a metà da un arresto). Una cache che non c'è non è un errore.
func (m *Miniature) Pulisci() error {
	v, err := os.ReadDir(m.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return ErrMiniatura
	}
	for _, e := range v {
		if s := reVoce.FindStringSubmatch(e.Name()); s != nil && m.fotoPresente(s[1]) {
			continue
		}
		_ = os.Remove(filepath.Join(m.dir, e.Name())) // come File.rm: una voce che non si toglie resta
	}
	return nil
}

// Togli toglie la miniatura di k, quando si cancella la sua foto (delete_one_pending/1, slice
// 041). Una chiave malformata non tocca niente.
func (m *Miniature) Togli(k string) {
	if ChiaveValida(k) {
		_ = os.Remove(m.voce(k)) // può non esserci
	}
}

// Riscaldatore prepara le miniature prima che una pagina le chieda, una alla volta
// (ThumbnailWarmer): all'avvio pulisce la cache e prepara le miniature che mancano, dopo un
// caricamento quelle delle foto nuove (Scalda). Una richiesta che arriva prima fa la stessa
// miniatura da sé; la seconda scrittura della stessa voce è un rename.
type Riscaldatore struct {
	m   *Miniature
	mu  sync.Mutex
	c   []string
	sve chan struct{}
}

// NuovoRiscaldatore dà il riscaldatore della cache m.
func NuovoRiscaldatore(m *Miniature) *Riscaldatore {
	return &Riscaldatore{m: m, sve: make(chan struct{}, 1)}
}

// Scalda mette in coda chiavi e torna subito; senza riscaldatore (nil) non fa niente.
func (r *Riscaldatore) Scalda(chiavi []string) {
	if r == nil || len(chiavi) == 0 {
		return
	}
	r.mu.Lock()
	r.c = append(r.c, chiavi...)
	r.mu.Unlock()
	select {
	case r.sve <- struct{}{}:
	default:
	}
}

// Lavoro gira fino alla fine di ctx: pulisce la cache, mette in coda le chiavi di chiavi (le foto
// delle buste, le più recenti prima) e poi prepara la coda una chiave alla volta. Un errore
// all'avvio non ferma niente: si registra e si prepara solo quello che arriva dopo.
func (r *Riscaldatore) Lavoro(ctx context.Context, chiavi func(context.Context) ([]string, error), registro *slog.Logger) {
	if registro == nil {
		registro = slog.New(slog.DiscardHandler)
	}
	if err := r.m.Pulisci(); err != nil {
		registro.Warn("pulizia delle miniature non riuscita")
	}
	if k, err := chiavi(ctx); err != nil {
		registro.Warn("foto da preparare non lette")
	} else {
		r.mu.Lock()
		r.c = append(k, r.c...)
		r.mu.Unlock()
	}
	for {
		r.mu.Lock()
		var k string
		ok := len(r.c) > 0
		if ok {
			k, r.c = r.c[0], r.c[1:]
		}
		r.mu.Unlock()
		if ok {
			if r.m.Prepara(ctx, k) == PreparazioneErrore && ctx.Err() == nil {
				registro.Debug("miniatura non preparata")
			}
			if ctx.Err() != nil {
				return
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-r.sve:
		}
	}
}
