// Package backup è il backup a caldo dell'apparecchio (PIANO §2.5, tema E, T5.2). Questa parte
// (slice 119) scrive il contenuto dell'archivio, senza cifratura: la cifratura age arriva con la
// 120, la verifica con la 121, il registro dei backup verificati con la 122.
//
// Lo standalone (scripts/packaging.py a go-baseline: backup_quiesced, files, validate_archive;
// maintenance.ex: snapshot_repo!) ferma l'app e mette in un tar data/gestrip.db, media/,
// inventory.json (SHA-256 di ogni file) e snapshot.json (formato, immagine, conteggi delle righe di
// ogni tabella tranne audit_events). Il Go fa lo stesso con l'app accesa (D-10): copia il database
// con VACUUM INTO in una cartella temporanea del volume dei dati, conta le righe sulla copia e
// aggiunge secrets/ (segreto Flask e segreto dello standalone, D-10, OPS-27). La chiave dei backup,
// che sta anche lei in secrets/ (D-113), non entra mai nell'archivio.
package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/media"
)

// Formato è il formato dell'archivio scritto in snapshot.json.
const Formato = 1

// CartellaSegreti è la cartella dei segreti accanto al database (PIANO §1.9).
const CartellaSegreti = "secrets"

// NomeChiaveBackup è il nome del file della chiave privata dei backup (identità age) in secrets/
// (D-113). Lo crea la 120, con questo nome: è l'unico file di secrets/ che resta fuori
// dall'archivio di proposito.
const NomeChiaveBackup = "chiave_backup"

// segretiNellArchivio sono i file di secrets/ che entrano nell'archivio (risposta B della T5.2):
// solo questi. Un file nuovo in secrets/ va messo qui o in segretiFuori, altrimenti il backup si
// ferma con ErrFileInatteso: né un segreto dimenticato né una chiave entrata per sbaglio. La
// chiave SFTP dell'apparecchio (128) va aggiunta qui.
var segretiNellArchivio = []string{"segreto_flask", "segreto_standalone"}

// segretiFuori sono i file di secrets/ che restano fuori, con il perché.
var segretiFuori = []string{
	NomeChiaveBackup, // apre tutti i backup: un archivio che la contiene si apre da solo (D-113)
}

// maxSegreto è la dimensione massima di un file di secrets/: sono chiavi e segreti di poche
// decine di byte; uno più grande non è un segreto nostro.
const maxSegreto = 64 << 10

// segnoChiaveAge è l'inizio di una chiave privata age (formato Bech32 di age): un file che la
// contiene non entra nell'archivio, con qualunque nome.
const segnoChiaveAge = "AGE-SECRET-KEY-"

// MargineSpazio è lo spazio libero che deve restare nel volume dei dati oltre alla copia del
// database: 10 GiB, come headroom/budget dello standalone (MIN_FREE).
const MargineSpazio = 10 << 30

// prefissoTemporanea è il nome della cartella temporanea della copia, nel volume dei dati.
const prefissoTemporanea = ".backup-"

// tabellaAudit resta fuori dai conteggi: ogni verifica ne aggiunge una riga (snapshot_repo!).
const tabellaAudit = "eventi_audit"

// Gli errori del backup.
var (
	ErrSpazio                   = errors.New("backup: spazio libero insufficiente nel volume dei dati")
	ErrFileInatteso             = errors.New("backup: file inatteso")
	ErrChiaveBackupNellArchivio = errors.New("backup: una chiave privata age stava per entrare nell'archivio")
)

// Sorgente è quello che si mette nell'archivio: il database acceso, l'archivio media dello stesso
// processo (il suo, perché la pulizia aspetti) e l'orologio per l'ora di cattura. La cartella dei
// dati è quella del database: lì stanno media/, secrets/ e la cartella temporanea della copia.
type Sorgente struct {
	DB       *db.DB
	Archivio *media.Archivio
	Orologio clock.Orologio
}

// Fotografia è snapshot.json: il formato, la versione del programma, l'ora della copia, le
// migrazioni applicate e le righe di ogni tabella della copia (tranne il registro). La 121 la
// confronta con il database estratto.
type Fotografia struct {
	Formato     int              `json:"formato"`
	Versione    string           `json:"versione"`
	CatturatoIl string           `json:"catturato_il"`
	Migrazioni  []int            `json:"migrazioni"`
	Conteggi    map[string]int64 `json:"conteggi"`
}

// Esito è quello che Scrivi ha messo nell'archivio: la fotografia, l'inventario (nome → SHA-256,
// come inventory.json) e i byte scritti.
type Esito struct {
	Fotografia
	Inventario map[string]string
	Byte       int64
}

// spazioLibero dà i byte liberi della cartella; una variabile per i test.
var spazioLibero = spazioLiberoDi

// versioneProgramma è la versione del binario (modulo e commit); una variabile per i test.
var versioneProgramma = func() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "sconosciuta"
	}
	v := bi.Main.Version
	for _, s := range bi.Settings {
		switch {
		case s.Key == "vcs.revision":
			v += " " + s.Value
		case s.Key == "vcs.modified" && s.Value == "true":
			v += " (modificato)"
		}
	}
	return v
}

// Scrivi scrive in w il tar del backup: data/gestrip.db, media/<chiave>, secrets/<nome>, poi
// inventory.json e snapshot.json. Nell'ordine:
//  1. controlla lo spazio libero nel volume dei dati (la copia del database più MargineSpazio);
//  2. ferma le cancellazioni della pulizia dei media (SenzaCancellazioni) fino alla fine;
//  3. copia il database con VACUUM INTO in una cartella temporanea del volume dei dati (0700,
//     mai /tmp: la copia è in chiaro, deve restare nel volume cifrato) e ne legge migrazioni e
//     conteggi;
//  4. elenca i media e legge i segreti, rifiutando ogni file inatteso, prima di scrivere un byte;
//  5. scrive il tar calcolando lo SHA-256 di ogni file mentre lo copia.
//
// I media si elencano dopo la copia: ogni oggetto che la copia nomina c'è già, e nessuno lo
// cancella fino alla fine. Un file salvato dopo la copia può entrare senza righe che lo usano: è
// un orfano, innocuo. La cartella temporanea si toglie sempre; w non si chiude.
//
// Lo spazio della destinazione non si controlla qui: lo fa chi scrive l'archivio da qualche parte
// (Cifra, 120; la chiavetta, 123). Ogni scrittura in w guarda ctx.
func Scrivi(ctx context.Context, s Sorgente, w io.Writer) (e Esito, err error) {
	// Ogni scrittura guarda il contesto, anche dentro la copia di un file grande: finito il contesto
	// (lo stop di serve, un Ctrl-C) il backup si ferma alla scrittura dopo (review della 119, 5a).
	w = scrittoreConContesto{ctx: ctx, w: w}
	dati := filepath.Dir(s.DB.Percorso())
	if err := controllaSpazio(s.DB.Percorso()); err != nil {
		return Esito{}, err
	}
	tmp, err := os.MkdirTemp(dati, prefissoTemporanea)
	if err != nil {
		return Esito{}, fmt.Errorf("backup: cartella temporanea: %w", err)
	}
	defer func() {
		if errRm := os.RemoveAll(tmp); errRm != nil {
			err = errors.Join(err, fmt.Errorf("backup: cartella temporanea %s da togliere a mano: %w", tmp, errRm))
		}
	}()
	err = s.Archivio.SenzaCancellazioni(ctx, func() error {
		var err error
		e, err = scrivi(ctx, s, filepath.Join(tmp, "gestrip.db"), w)
		return err
	})
	if err != nil {
		return Esito{}, err
	}
	return e, nil
}

func scrivi(ctx context.Context, s Sorgente, copia string, w io.Writer) (Esito, error) {
	adesso := s.Orologio.Adesso()
	if err := s.DB.CopiaACaldo(ctx, copia); err != nil {
		return Esito{}, fmt.Errorf("backup: copia del database: %w", err)
	}
	f, err := fotografa(ctx, copia)
	if err != nil {
		return Esito{}, err
	}
	f.Formato, f.Versione, f.CatturatoIl = Formato, versioneProgramma(), adesso.UTC().Format(db.FormatoIstante)
	chiavi, err := elencaMedia(s.Archivio.Cartella())
	if err != nil {
		return Esito{}, err
	}
	segreti, err := leggiSegreti(filepath.Join(filepath.Dir(s.DB.Percorso()), CartellaSegreti))
	if err != nil {
		return Esito{}, err
	}

	c := &contatore{w: w}
	t := &scrittoreTar{tw: tar.NewWriter(c), quando: adesso, inv: map[string]string{}}
	if err := t.file(ctx, "data/gestrip.db", copia); err != nil {
		return Esito{}, err
	}
	for _, k := range chiavi {
		if err := t.file(ctx, "media/"+k, filepath.Join(s.Archivio.Cartella(), k)); err != nil {
			return Esito{}, err
		}
	}
	for _, n := range slices.Sorted(maps.Keys(segreti)) {
		if err := t.byte("secrets/"+n, segreti[n], true); err != nil {
			return Esito{}, err
		}
	}
	inv := maps.Clone(t.inv)
	jInv, err := json.Marshal(inv)
	if err != nil {
		return Esito{}, err
	}
	jFoto, err := json.Marshal(f)
	if err != nil {
		return Esito{}, err
	}
	if err := t.byte("inventory.json", jInv, false); err != nil {
		return Esito{}, err
	}
	if err := t.byte("snapshot.json", jFoto, false); err != nil {
		return Esito{}, err
	}
	if err := t.tw.Close(); err != nil {
		return Esito{}, fmt.Errorf("backup: chiusura del tar: %w", err)
	}
	return Esito{Fotografia: f, Inventario: inv, Byte: c.n}, nil
}

// controllaSpazio vuole nel volume dei dati, libero, il database (con il suo WAL, che VACUUM INTO
// copia nelle pagine) più MargineSpazio. Uno spazio che non si legge è un rifiuto.
func controllaSpazio(dbp string) error {
	var serve uint64 = MargineSpazio
	for _, n := range []string{dbp, dbp + "-wal"} {
		fi, err := os.Stat(n)
		if errors.Is(err, fs.ErrNotExist) && n != dbp {
			continue
		}
		if err != nil {
			return fmt.Errorf("backup: dimensione del database: %w", err)
		}
		serve += uint64(fi.Size())
	}
	dir := filepath.Dir(dbp)
	libero, err := spazioLibero(dir)
	if err != nil {
		return fmt.Errorf("backup: spazio libero di %s non leggibile, non parto: %w", dir, err)
	}
	if libero < serve {
		return fmt.Errorf("%w: in %s servono %d byte (copia del database e 10 GiB di margine), ci sono %d byte", ErrSpazio, dir, serve, libero)
	}
	return nil
}

// fotografa legge sulla copia le migrazioni applicate e i conteggi (snapshot_repo!): ogni tabella
// tranne quelle interne di SQLite e il registro.
func fotografa(ctx context.Context, copia string) (Fotografia, error) {
	c, err := sql.Open("sqlite", db.URIDi(copia)+"?mode=ro")
	if err != nil {
		return Fotografia{}, err
	}
	defer func() { _ = c.Close() }() // sola lettura: niente da perdere
	return fotografaCon(ctx, c)
}

// fotografaCon è fotografa su un collegamento già aperto (la verifica della 121 apre il database
// estratto a modo suo).
func fotografaCon(ctx context.Context, c *sql.DB) (Fotografia, error) {
	f := Fotografia{Conteggi: map[string]int64{}}
	tabelle, err := colonna[string](ctx, c, `SELECT name FROM sqlite_schema WHERE type = 'table'
		AND name NOT LIKE 'sqlite_%' AND name <> ? ORDER BY name`, tabellaAudit)
	if err != nil {
		return Fotografia{}, fmt.Errorf("backup: tabelle della copia: %w", err)
	}
	for _, n := range tabelle {
		var k int64
		if err := c.QueryRowContext(ctx, `SELECT count(*) FROM "`+strings.ReplaceAll(n, `"`, `""`)+`"`).Scan(&k); err != nil {
			return Fotografia{}, fmt.Errorf("backup: conteggio di %s: %w", n, err)
		}
		f.Conteggi[n] = k
	}
	if f.Migrazioni, err = colonna[int](ctx, c, `SELECT versione FROM schema_migrations ORDER BY versione`); err != nil {
		return Fotografia{}, fmt.Errorf("backup: migrazioni della copia: %w", err)
	}
	if len(f.Migrazioni) == 0 {
		return Fotografia{}, errors.New("backup: la copia non ha migrazioni registrate")
	}
	return f, nil
}

func colonna[T any](ctx context.Context, c *sql.DB, q string, args ...any) ([]T, error) {
	r, err := c.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var v []T
	for r.Next() {
		var x T
		if err := r.Scan(&x); err != nil {
			_ = r.Close()
			return nil, err
		}
		v = append(v, x)
	}
	return v, errors.Join(r.Err(), r.Close())
}

// elencaMedia dà le chiavi della cartella media, in ordine. I temporanei «.upload-» di un
// salvataggio in corso non sono dati e restano fuori; un collegamento, una cartella, un file
// speciale o un nome che non è un indirizzo di contenuto sono ErrFileInatteso: il backup non
// indovina. Una cartella media che non c'è non ha file.
func elencaMedia(dir string) ([]string, error) {
	v, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("backup: cartella media: %w", err)
	}
	var chiavi []string
	for _, e := range v {
		n := e.Name()
		if strings.HasPrefix(n, ".upload-") {
			continue
		}
		if !e.Type().IsRegular() || !media.ChiaveValida(n) {
			return nil, fmt.Errorf("%w in media: %q (%v)", ErrFileInatteso, n, e.Type())
		}
		chiavi = append(chiavi, n)
	}
	return chiavi, nil
}

// leggiSegreti legge i file di secrets/ che entrano nell'archivio (segretiNellArchivio). Un file
// sconosciuto, una cartella, un collegamento, un file troppo grande sono ErrFileInatteso; un file
// che contiene una chiave privata age è ErrChiaveBackupNellArchivio. Tutto si controlla prima di
// scrivere il tar. Una cartella secrets/ che non c'è non ha segreti; uno che manca non c'è
// (il segreto Flask manca se il negozio non veniva dal Flask).
func leggiSegreti(dir string) (map[string][]byte, error) {
	v, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("backup: cartella dei segreti: %w", err)
	}
	m := map[string][]byte{}
	for _, e := range v {
		n := e.Name()
		if slices.Contains(segretiFuori, n) {
			continue
		}
		if strings.HasPrefix(n, "."+NomeChiaveBackup+"-") {
			return nil, fmt.Errorf("%w in %s: %q è il resto di un «gestrip chiave-backup» interrotto: toglilo a mano (contiene una chiave mai usata)", ErrFileInatteso, CartellaSegreti, n)
		}
		if !slices.Contains(segretiNellArchivio, n) || !e.Type().IsRegular() {
			return nil, fmt.Errorf("%w in %s: %q, da mettere fra i segreti dentro o fuori dall'archivio", ErrFileInatteso, CartellaSegreti, n)
		}
		b, err := leggiFile(filepath.Join(dir, n), maxSegreto)
		if err != nil {
			return nil, err
		}
		if bytes.Contains(bytes.ToUpper(b), []byte(segnoChiaveAge)) {
			return nil, fmt.Errorf("%w: %s/%s", ErrChiaveBackupNellArchivio, CartellaSegreti, n)
		}
		m[n] = b
	}
	return m, nil
}

// leggiFile legge un file normale senza seguire collegamenti, al massimo max byte.
func leggiFile(p string, max int64) ([]byte, error) {
	f, err := apriSenzaCollegamenti(p)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrFileInatteso, filepath.Base(p), err)
	}
	defer func() { _ = f.Close() }() // sola lettura
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || fi.Size() > max {
		return nil, fmt.Errorf("%w: %s", ErrFileInatteso, filepath.Base(p))
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%w: %s", ErrFileInatteso, filepath.Base(p))
	}
	return b, nil
}

// reNome sono i caratteri ammessi in un nome del tar (files/1 dello standalone).
var reNome = regexp.MustCompile(`\A[A-Za-z0-9_./-]+\z`)

// nomeSicuro dice se n può stare nel tar: relativo, già pulito, con i caratteri di reNome, e uno
// dei due json o un file diretto di data/, media/ o secrets/ (niente sottocartelle). Così nessun
// nome esce dalla cartella in cui il ripristino lo estrae (validate_archive dello standalone).
func nomeSicuro(n string) error {
	if !reNome.MatchString(n) || path.Clean(n) != n || path.IsAbs(n) || slices.Contains(strings.Split(n, "/"), "..") {
		return fmt.Errorf("%w: nome %q", ErrFileInatteso, n)
	}
	if n == "inventory.json" || n == "snapshot.json" {
		return nil
	}
	dir, base := path.Split(n)
	if (dir == "data/" || dir == "media/" || dir == "secrets/") && base != "" && base != "." {
		return nil
	}
	return fmt.Errorf("%w: nome %q", ErrFileInatteso, n)
}

// scrittoreTar scrive le voci del tar, tutte file normali 0600 con l'ora della copia, e tiene
// l'inventario.
type scrittoreTar struct {
	tw     *tar.Writer
	quando time.Time
	inv    map[string]string
}

func (t *scrittoreTar) intestazione(n string, size int64) error {
	if err := nomeSicuro(n); err != nil {
		return err
	}
	return t.tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg, Name: n, Size: size, Mode: 0o600,
		ModTime: t.quando.UTC().Truncate(time.Second), Format: tar.FormatPAX,
	})
}

// file copia nel tar il file p (senza seguire collegamenti) con il nome n, calcolando lo SHA-256
// mentre lo copia. Un file che cambia dimensione durante la copia è un errore del tar.
func (t *scrittoreTar) file(ctx context.Context, n, p string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := apriSenzaCollegamenti(p)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrFileInatteso, n, err)
	}
	defer func() { _ = f.Close() }() // sola lettura
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%w: %s non è un file normale", ErrFileInatteso, n)
	}
	if err := t.intestazione(n, fi.Size()); err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(t.tw, h), f); err != nil {
		return fmt.Errorf("backup: %s: %w", n, err)
	}
	t.inv[n] = hex.EncodeToString(h.Sum(nil))
	return nil
}

// byte scrive nel tar b con il nome n; conta dice se va nell'inventario (non i due json).
func (t *scrittoreTar) byte(n string, b []byte, conta bool) error {
	if err := t.intestazione(n, int64(len(b))); err != nil {
		return err
	}
	if _, err := t.tw.Write(b); err != nil {
		return fmt.Errorf("backup: %s: %w", n, err)
	}
	if conta {
		h := sha256.Sum256(b)
		t.inv[n] = hex.EncodeToString(h[:])
	}
	return nil
}

// contatore conta i byte scritti in w.
type contatore struct {
	w io.Writer
	n int64
}

func (c *contatore) Write(b []byte) (int, error) {
	n, err := c.w.Write(b)
	c.n += int64(n)
	return n, err
}

// scrittoreConContesto scrive in w finché ctx non è finito; dopo, ogni scrittura dà ctx.Err().
type scrittoreConContesto struct {
	ctx context.Context
	w   io.Writer
}

func (s scrittoreConContesto) Write(b []byte) (int, error) {
	if err := s.ctx.Err(); err != nil {
		return 0, err
	}
	return s.w.Write(b)
}
