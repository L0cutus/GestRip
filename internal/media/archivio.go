// Package media è l'archivio dei file a indirizzo di contenuto (PIANO §1.9, RIP-S-30): ogni file
// sta nella cartella media accanto al database con il nome dello SHA-256 in esadecimale minuscolo
// dei suoi byte, permessi 0400, cartella 0700. Copia MediaStore dello standalone a go-baseline
// (lib/gestrip_standalone/media_store.ex: store/1, read_object/1, image_type/1,2,
// image_extension/1, with_lock/1); l'import (internal/importer, slice 023) scrive la stessa forma.
package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// Cartella è il nome della cartella media accanto al database.
const Cartella = "media"

// MaxByte è la dimensione massima di un file media (read_object/1).
const MaxByte = 16_777_216

// Gli errori di MediaStore.
var (
	ErrSalvataggio   = errors.New("media: salvataggio non riuscito") // :storage_failed
	ErrChiave        = errors.New("media: chiave non valida")        // :invalid_storage_key
	ErrOggettoGuasto = errors.New("media: oggetto guasto")           // :corrupt_object
	// ErrLucchetto: il file del lucchetto delle cancellazioni non si apre o non si blocca (slice 120;
	// per esempio creato da root da un «gestrip backup» lanciato con l'utente sbagliato).
	ErrLucchetto = errors.New("media: lucchetto delle cancellazioni")
)

// CartellaDi è la cartella media che va con il database db.
func CartellaDi(db string) string { return filepath.Join(filepath.Dir(db), Cartella) }

var reChiave = regexp.MustCompile(`\A[0-9a-f]{64}\z`)

// ChiaveValida dice se k ha la forma di un indirizzo di contenuto: 64 cifre esadecimali minuscole
// (niente percorsi).
func ChiaveValida(k string) bool { return reChiave.MatchString(k) }

// Archivio è la cartella media. Uno solo per processo: il lucchetto serializza salvataggi,
// cancellazioni ed esportazioni come with_lock/1 (:global.trans) dello standalone.
type Archivio struct {
	dir string
	mu  sync.Mutex
	// cancellazioni separa le cancellazioni della coda (Pulizia) da chi legge tutto l'archivio
	// (SenzaCancellazioni, il backup): le prime lo prendono in scrittura, il backup in lettura.
	// Non sta in mu, così caricamenti e salvataggi vanno avanti durante il backup. Vale solo nel
	// processo: fra processi (gestrip backup accanto a serve, slice 120) c'è anche il lucchetto su
	// file LucchettoCancellazioni, preso dopo questo.
	cancellazioni sync.RWMutex
	// sincronizza, solo nei test, sta al posto di sincronizzaCartella.
	sincronizza func(string) error
}

// Nuovo dà l'archivio della cartella dir.
func Nuovo(dir string) *Archivio { return &Archivio{dir: dir} }

// Cartella è la cartella dell'archivio.
func (a *Archivio) Cartella() string { return a.dir }

// Salva salva b (non vuoto) con il lucchetto e ne dà la chiave (store/1).
func (a *Archivio) Salva(b []byte) (string, error) {
	var k string
	err := a.ConLucchetto(func() error {
		var err error
		k, err = a.SalvaConLucchetto(b)
		return err
	})
	return k, err
}

// SalvaConLucchetto è Salva per chi tiene già il lucchetto (store_while_locked/2), nell'ordine di
// do_store/2: cartella creata se manca e riportata a 0700 (deve essere una cartella, non un link);
// un temporaneo «.upload-…» nuovo (O_EXCL) 0600, i byte, fsync, 0400; link al nome definitivo (se
// c'è già va bene: stesso contenuto, stesso oggetto); fsync della cartella; rilettura con Leggi,
// che deve dare gli stessi byte; poi via il temporaneo e di nuovo fsync della cartella. Ogni
// passo che fallisce è ErrSalvataggio.
func (a *Archivio) SalvaConLucchetto(b []byte) (string, error) {
	if len(b) == 0 {
		return "", ErrSalvataggio
	}
	h := sha256.Sum256(b)
	k := hex.EncodeToString(h[:])
	if err := os.MkdirAll(a.dir, 0o700); err != nil {
		return "", ErrSalvataggio
	}
	if fi, err := os.Lstat(a.dir); err != nil || !fi.IsDir() {
		return "", ErrSalvataggio
	}
	if err := os.Chmod(a.dir, 0o700); err != nil {
		return "", ErrSalvataggio
	}
	var caso [16]byte
	if _, err := rand.Read(caso[:]); err != nil {
		return "", ErrSalvataggio
	}
	tmp := filepath.Join(a.dir, ".upload-"+base64.RawURLEncoding.EncodeToString(caso[:]))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", ErrSalvataggio
	}
	esito := a.pubblica(f, tmp, k, b)
	if err := f.Close(); err != nil {
		esito = ErrSalvataggio
	}
	if err := os.Remove(tmp); err != nil {
		return "", ErrSalvataggio
	}
	if err := a.sincronizzaDir(); err != nil {
		return "", ErrSalvataggio
	}
	if esito != nil {
		return "", ErrSalvataggio
	}
	return k, nil
}

// pubblica scrive b nel temporaneo f e gli dà il nome k.
func (a *Archivio) pubblica(f *os.File, tmp, k string, b []byte) error {
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Chmod(0o400); err != nil {
		return err
	}
	if err := os.Link(tmp, filepath.Join(a.dir, k)); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	if err := a.sincronizzaDir(); err != nil {
		return err
	}
	letti, err := a.Leggi(k)
	if err != nil {
		return err
	}
	if !bytes.Equal(letti, b) {
		return ErrOggettoGuasto
	}
	return nil
}

func (a *Archivio) sincronizzaDir() error {
	if a.sincronizza != nil {
		return a.sincronizza(a.dir)
	}
	return sincronizzaCartella(a.dir)
}

// sincronizzaCartella fa fsync della cartella dir.
func sincronizzaCartella(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}

// Leggi dà i byte dell'oggetto k (read_object/1): ErrChiave se k non ha la forma di una chiave;
// ErrOggettoGuasto se il file manca, non è un file regolare (un link non si segue), è vuoto, supera
// MaxByte o i suoi byte hanno un'altra impronta.
func (a *Archivio) Leggi(k string) ([]byte, error) {
	if !ChiaveValida(k) {
		return nil, ErrChiave
	}
	p := filepath.Join(a.dir, k)
	prima, err := os.Lstat(p)
	if err != nil || !prima.Mode().IsRegular() || prima.Size() < 1 || prima.Size() > MaxByte {
		return nil, ErrOggettoGuasto
	}
	f, err := ApriInLettura(p)
	if err != nil {
		return nil, ErrOggettoGuasto
	}
	defer func() { _ = f.Close() }() // in sola lettura: niente da perdere
	if aperto, err := f.Stat(); err != nil || !os.SameFile(prima, aperto) {
		return nil, ErrOggettoGuasto
	}
	b, err := io.ReadAll(io.LimitReader(f, MaxByte+1))
	if err != nil || len(b) < 1 || len(b) > MaxByte {
		return nil, ErrOggettoGuasto
	}
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != k {
		return nil, ErrOggettoGuasto
	}
	return b, nil
}

// ConLucchetto esegue f con il lucchetto dell'archivio e ne dà l'errore.
func (a *Archivio) ConLucchetto(f func() error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return f()
}

// LucchettoCancellazioni è il file, nella cartella dei dati accanto a media/, su cui le
// cancellazioni della coda e i backup si escludono anche fra processi diversi (slice 120): gestrip
// backup è un processo a sé, e il RWMutex di serve non lo vede. Sta fuori da media/, dove il backup
// accetta solo oggetti a indirizzo di contenuto. Il file resta: conta solo il flock, che il kernel
// toglie da solo quando il processo muore.
const LucchettoCancellazioni = ".lucchetto-cancellazioni-media"

// attesaLucchetto è ogni quanto si riprova un lucchetto occupato; una variabile per i test.
var attesaLucchetto = 50 * time.Millisecond

// SenzaCancellazioni esegue f mentre nessuno cancella oggetti dall'archivio (slice 119): la coda
// delle cancellazioni della pulizia (D-55) aspetta che f finisca, così un file non sparisce a metà
// della lettura del backup. I salvataggi dei file non aspettano (le scritture nel database aspettano
// la copia VACUUM INTO, che usa la connessione di scrittura). Lo standalone non ne ha bisogno perché fa il
// backup con l'app ferma.
//
// Dalla slice 120 vale anche fra processi (LucchettoCancellazioni, flock condiviso) e l'attesa
// guarda ctx: con ctx finito torna ctx.Err() senza eseguire f.
func (a *Archivio) SenzaCancellazioni(ctx context.Context, f func() error) error {
	return a.conCancellazioni(ctx, false, f)
}

// conCancellazioni esegue f con il lucchetto delle cancellazioni: in lettura (il backup) o in
// scrittura (la coda della pulizia). Prima il RWMutex del processo, poi il flock del file, tutti e
// due riprovati ogni attesaLucchetto finché ctx non finisce: né lo stop di serve né un backup
// interrotto restano appesi a un lucchetto (review della 119, difetto in lista 5a).
func (a *Archivio) conCancellazioni(ctx context.Context, scrittura bool, f func() error) error {
	prova, sblocca := a.cancellazioni.TryRLock, a.cancellazioni.RUnlock
	if scrittura {
		prova, sblocca = a.cancellazioni.TryLock, a.cancellazioni.Unlock
	}
	if err := aspettaFinche(ctx, func() (bool, error) { return prova(), nil }); err != nil {
		return err
	}
	defer sblocca()
	rilascia, err := lucchettoFile(ctx, filepath.Join(filepath.Dir(a.dir), LucchettoCancellazioni), scrittura)
	if err != nil {
		return err
	}
	defer rilascia()
	return f()
}

// aspettaFinche riprova prova ogni attesaLucchetto finché riesce, sbaglia o ctx finisce. Con ctx
// già finito non prova nemmeno.
func aspettaFinche(ctx context.Context, prova func() (bool, error)) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ok, err := prova()
		if err != nil || ok {
			return err
		}
		if !clock.Aspetta(ctx, attesaLucchetto) {
			return ctx.Err()
		}
	}
}

// I tipi che image_type/1 riconosce.
const (
	TipoJPEG   = "image/jpeg"
	TipoPNG    = "image/png"
	TipoWebP   = "image/webp"
	TipoIgnoto = "application/octet-stream"
)

// TipoImmagine è il tipo dei byte salvati letto dalla loro firma (image_type/1): le foto nuove
// sono JPEG, le vecchie PNG, le importate quello che la sorgente teneva. Altrimenti
// application/octet-stream, mai un'ipotesi.
func TipoImmagine(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return TipoJPEG
	case len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return TipoPNG
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return TipoWebP
	}
	return TipoIgnoto
}

// TipoImmagineORegistrato è TipoImmagine, o registrato (il tipo della riga) se i byte non hanno
// una firma nota (image_type/2): quello che si serve non cambia.
func TipoImmagineORegistrato(b []byte, registrato string) string {
	if t := TipoImmagine(b); t != TipoIgnoto {
		return t
	}
	return registrato
}

// Estensione è l'estensione che va con TipoImmagine (image_extension/1), vuota per un tipo ignoto.
func Estensione(b []byte) string {
	switch TipoImmagine(b) {
	case TipoJPEG:
		return ".jpg"
	case TipoPNG:
		return ".png"
	case TipoWebP:
		return ".webp"
	}
	return ""
}
