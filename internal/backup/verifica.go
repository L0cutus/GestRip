package backup

// La verifica completa di un archivio del backup (slice 121, D-113, OPS-16 «riuscito solo dopo la
// verifica», OPS-18 controlli dell'archivio). Lo standalone (scripts/packaging.py a go-baseline:
// decrypted, validate_archive) decifra con il comando age in un file temporaneo, rifiuta percorsi che
// escono, link, file speciali, duplicati e membri inattesi, e confronta ogni SHA-256 con
// inventory.json; integrità e conteggi li fa solo al ripristino e nella prova settimanale. Il Go fa
// tutto a ogni backup (D-113), in flusso: il tar decifrato passa solo in memoria, tranne il database,
// che si estrae in una cartella temporanea del volume dei dati (mai /tmp) per PRAGMA integrity_check e
// per i conteggi. Ogni dubbio è un rifiuto: una verifica troppo morbida sbloccherà le cancellazioni
// irreversibili (122, D-11).

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
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"filippo.io/age"

	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/media"
)

// Gli errori della verifica. Un errore qualsiasi (anche di lettura, di age o del contesto) vuol dire
// «non verificato»: questi servono a dire perché.
var (
	ErrVoce         = errors.New("backup non verificato: voce dell'archivio non ammessa")
	ErrInventario   = errors.New("backup non verificato: inventario")
	ErrFotografia   = errors.New("backup non verificato: fotografia (snapshot.json)")
	ErrIntegrita    = errors.New("backup non verificato: integrità del database")
	ErrFineArchivio = errors.New("backup non verificato: fine dell'archivio")
	ErrImpronta     = errors.New("backup non verificato: l'archivio verificato non è quello scritto")
)

// Limiti contro un archivio rovinato o costruito apposta. Sono variabili solo per i test. Il tar
// decifrato non è mai più grande del file cifrato, quindi il disco lo limita già; questi limitano la
// memoria e il tempo prima di arrivare a un rifiuto.
var (
	// maxVoci è il numero massimo di voci del tar: 200 000 foto sono più di 100 GB, molto oltre un
	// apparecchio del negozio.
	maxVoci = 200_000
	// maxInventario è la dimensione massima di inventory.json: 140 byte per voce per maxVoci, con
	// margine.
	maxInventario int64 = 32 << 20
	// maxFotografia è la dimensione massima di snapshot.json: un conteggio per tabella.
	maxFotografia int64 = 1 << 20
)

// prefissoVerifica è il nome della cartella temporanea della verifica, nel volume dei dati.
const prefissoVerifica = ".verifica-backup-"

// Solo nei test: dopoEstrazione gira con il database appena estratto, prima di aprirlo.
var dopoEstrazione func(file string)

// Verificato è un archivio verificato: il file, lo SHA-256 e i byte del file cifrato (quelli letti
// davvero durante la verifica, per la 122), la fotografia e l'inventario.
type Verificato struct {
	File        string
	SHA256      string
	ByteCifrati int64
	Fotografia
	Inventario map[string]string
}

// VerificaArchivio verifica per intero l'archivio file con la chiave dei backup della cartella dei
// dati. Solo un errore nil vuol dire verificato. Nell'ordine:
//  1. legge la chiave (LeggiChiave) e apre il file senza seguire collegamenti, solo un file normale;
//  2. decifra in flusso con age (che rifiuta chiave sbagliata, testo cifrato toccato o troncato e byte
//     in più dopo la fine) calcolando lo SHA-256 del file cifrato;
//  3. legge il tar voce per voce: solo file normali con nomi sicuri, nell'ordine in cui li scrive
//     Scrivi (data/gestrip.db, media/ in ordine, secrets/ in ordine, inventory.json, snapshot.json),
//     quindi niente duplicati né voci dopo snapshot.json; calcola lo SHA-256 di ogni file ed estrae
//     il database in una cartella temporanea del volume dei dati;
//  4. vuole la fine del tar esatta (due blocchi vuoti) e niente dopo, né nel tar né nel file;
//  5. inventory.json uguale alle impronte calcolate (stesse voci, nessuna in più né in meno);
//     inventory.json e snapshot.json scritti esattamente come li scrive Scrivi (niente campi
//     sconosciuti, chiavi doppie, spazi o byte in più);
//  6. PRAGMA integrity_check completo sul database estratto, poi migrazioni e conteggi uguali a
//     snapshot.json.
//
// La cartella temporanea si toglie sempre; ogni lettura guarda ctx.
func VerificaArchivio(ctx context.Context, dati, file string) (v Verificato, err error) {
	id, err := LeggiChiave(dati)
	if err != nil {
		return Verificato{}, err
	}
	f, err := apriSenzaCollegamenti(file)
	if err != nil {
		return Verificato{}, fmt.Errorf("backup non verificato: archivio %s: %w", file, err)
	}
	defer func() { _ = f.Close() }() // sola lettura
	fi, err := f.Stat()
	if err != nil {
		return Verificato{}, err
	}
	if !fi.Mode().IsRegular() {
		return Verificato{}, fmt.Errorf("backup non verificato: %s non è un file normale", file)
	}
	tmp, err := os.MkdirTemp(dati, prefissoVerifica) // 0700
	if err != nil {
		return Verificato{}, fmt.Errorf("backup non verificato: cartella temporanea: %w", err)
	}
	defer func() {
		if errRm := os.RemoveAll(tmp); errRm != nil {
			err = errors.Join(err, fmt.Errorf("backup: cartella temporanea %s da togliere a mano: %w", tmp, errRm))
		}
	}()
	h := sha256.New()
	c := &lettoreContato{r: io.TeeReader(lettoreConContesto{ctx: ctx, r: f}, h)}
	foto, inv, err := verifica(ctx, id, c, dati, filepath.Join(tmp, "gestrip.db"))
	if err != nil {
		// age e SQLite non sempre avvolgono l'errore del contesto: uno stop resta riconoscibile.
		if errC := ctx.Err(); errC != nil && !errors.Is(err, errC) {
			err = fmt.Errorf("%w (%w)", errC, err)
		}
		return Verificato{}, err
	}
	// age ha già rifiutato byte dopo la fine; qui si legge comunque il file fino in fondo, così lo
	// SHA-256 è quello di tutto il file.
	if n, err := io.Copy(io.Discard, c); err != nil {
		return Verificato{}, fmt.Errorf("backup non verificato: lettura dell'archivio: %w", err)
	} else if n != 0 {
		return Verificato{}, fmt.Errorf("%w: %d byte dopo la fine dei dati cifrati", ErrFineArchivio, n)
	}
	return Verificato{File: file, SHA256: hex.EncodeToString(h.Sum(nil)), ByteCifrati: c.n,
		Fotografia: foto, Inventario: inv}, nil
}

// verifica decifra r con id e controlla il tar (passi 2-6 di VerificaArchivio); estrae il database in
// copia, un file che non c'è ancora nella cartella temporanea.
func verifica(ctx context.Context, id *age.X25519Identity, r io.Reader, dati, copia string) (Fotografia, map[string]string, error) {
	in, err := age.Decrypt(r, identitaSola{id})
	if err != nil {
		return Fotografia{}, nil, fmt.Errorf("backup non verificato: decifratura: %w", err)
	}
	z := &lettoreCoda{r: in}
	l := lettoreTar{tr: tar.NewReader(z), z: z, dati: dati, copia: copia, impronte: map[string]string{}}
	if err := l.leggi(ctx); err != nil {
		return Fotografia{}, nil, err
	}
	// Dopo i due blocchi vuoti della fine del tar non ci deve essere niente; leggere fino all'EOF fa
	// anche controllare ad age l'ultimo pezzo (troncato o con byte in più è un errore).
	if n, err := io.Copy(io.Discard, z); err != nil {
		return Fotografia{}, nil, fmt.Errorf("backup non verificato: decifratura: %w", err)
	} else if n != 0 {
		return Fotografia{}, nil, fmt.Errorf("%w: %d byte dopo la fine del tar", ErrFineArchivio, n)
	}
	// Scrivi chiude il tar con esattamente due blocchi vuoti: con uno solo il lettore di Go si ferma
	// lo stesso, quindi si controlla qui.
	if z.n%512 != 0 || z.zeri < 1024 {
		return Fotografia{}, nil, fmt.Errorf("%w: il tar non finisce con due blocchi vuoti", ErrFineArchivio)
	}
	inv, err := l.inventario()
	if err != nil {
		return Fotografia{}, nil, err
	}
	foto, err := l.fotografia()
	if err != nil {
		return Fotografia{}, nil, err
	}
	// Scrivi dà a ogni voce l'ora della copia, al secondo: la stessa di snapshot.json.
	if t, _ := time.Parse(db.FormatoIstante, foto.CatturatoIl); !l.quando.Equal(t.Truncate(time.Second)) {
		return Fotografia{}, nil, fmt.Errorf("%w: ora delle voci %v diversa dall'ora di cattura %s", ErrVoce, l.quando, foto.CatturatoIl)
	}
	if err := controllaDatabase(ctx, copia, foto); err != nil {
		return Fotografia{}, nil, err
	}
	return foto, inv, nil
}

// ErrDestinatari: l'archivio è cifrato anche per altri destinatari, o non per una chiave X25519.
var ErrDestinatari = errors.New("backup non verificato: l'archivio deve essere cifrato per la sola chiave dei backup")

// identitaSola è la chiave dei backup che apre solo un archivio con un destinatario solo, X25519, come
// lo scrive Cifra: un archivio che apre anche qualcun altro non è nostro, anche se la chiave lo apre.
type identitaSola struct{ id *age.X25519Identity }

func (i identitaSola) Unwrap(s []*age.Stanza) ([]byte, error) {
	if len(s) != 1 || s[0].Type != "X25519" {
		return nil, fmt.Errorf("%w: %d destinatari", ErrDestinatari, len(s))
	}
	return i.id.Unwrap(s)
}

// lettoreTar legge le voci del tar e tiene quello che serve ai controlli finali.
type lettoreTar struct {
	tr          *tar.Reader
	z           *lettoreCoda // per il blocco grezzo dell'ultima intestazione
	quando      time.Time    // l'ora di tutte le voci
	dati, copia string
	impronte    map[string]string // nome → SHA-256 calcolato, tranne i due json
	jInv, jFoto []byte
	voci        int
	ultima      string // il nome dell'ultima voce, per l'ordine
	rangoUltima int
	visto       [rangoFotografia + 1]bool // i ranghi visti: database e i due json sono obbligatori
}

// Ranghi delle voci nell'ordine di Scrivi.
const (
	rangoDatabase = iota + 1
	rangoMedia
	rangoSegreto
	rangoInventario
	rangoFotografia
)

// rango dice il posto di un nome nell'ordine di Scrivi e la dimensione massima della sua voce (-1:
// solo lo spazio del volume dei dati, per il database); un nome che Scrivi non scrive mai è ErrVoce.
func rango(n string) (int, int64, error) {
	if nomeSicuro(n) != nil {
		return 0, 0, fmt.Errorf("%w: nome %q", ErrVoce, n)
	}
	dir, base, _ := strings.Cut(n, "/")
	switch {
	case n == "data/gestrip.db":
		return rangoDatabase, -1, nil
	case dir == "media" && media.ChiaveValida(base):
		return rangoMedia, media.MaxByte, nil
	case dir == "secrets" && slices.Contains(segretiNellArchivio, base):
		return rangoSegreto, maxSegreto, nil
	case n == "inventory.json":
		return rangoInventario, maxInventario, nil
	case n == "snapshot.json":
		return rangoFotografia, maxFotografia, nil
	}
	return 0, 0, fmt.Errorf("%w: %q non è un file del backup", ErrVoce, n)
}

func (l *lettoreTar) leggi(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		h, err := l.tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: intestazione non leggibile: %w", ErrVoce, err)
		}
		if l.voci++; l.voci > maxVoci {
			return fmt.Errorf("%w: più di %d voci", ErrVoce, maxVoci)
		}
		if err := l.controllaQuando(h); err != nil {
			return err
		}
		if err := controllaIntestazione(h, l.z.blocco[:]); err != nil {
			return err
		}
		if err := l.voce(h); err != nil {
			return err
		}
	}
	if !l.visto[rangoDatabase] || !l.visto[rangoInventario] || !l.visto[rangoFotografia] {
		return fmt.Errorf("%w: mancano il database, inventory.json o snapshot.json", ErrVoce)
	}
	return nil
}

// controllaQuando vuole la stessa ora in tutte le voci (Scrivi le scrive tutte con l'ora della copia).
func (l *lettoreTar) controllaQuando(h *tar.Header) error {
	if l.voci == 1 {
		l.quando = h.ModTime
	}
	if !h.ModTime.Equal(l.quando) {
		return fmt.Errorf("%w: %q con un'ora diversa dalle altre voci", ErrVoce, h.Name)
	}
	return nil
}

// controllaIntestazione vuole l'intestazione come la scrive Scrivi; blocco è il blocco grezzo di 512
// byte da cui il lettore tar l'ha letta (il lettore trasforma il vecchio tipo '\x00' in TypeReg, solo
// il blocco lo dice). Ammessi: tipo '0' (file normale), formato USTAR, nessun collegamento, permessi
// 0600, utente e gruppo 0 senza nome, nessun dispositivo, nessun campo PAX. Unica eccezione: il
// database oltre 8 GiB, per cui tar.Writer scrive la dimensione in un campo PAX «size» (il campo USTAR
// non ci sta): solo quel campo, uguale alla dimensione, solo per data/gestrip.db.
func controllaIntestazione(h *tar.Header, blocco []byte) error {
	n := h.Name
	if len(blocco) != 512 || blocco[156] != tar.TypeReg || h.Typeflag != tar.TypeReg || h.Linkname != "" {
		return fmt.Errorf("%w: %q di tipo %q, solo file normali", ErrVoce, n, h.Typeflag)
	}
	formato := tar.FormatUSTAR
	if len(h.PAXRecords) != 0 {
		// Gli attributi estesi e i file sparsi PAX passano da qui: fuori.
		if n != "data/gestrip.db" || len(h.PAXRecords) != 1 || h.PAXRecords["size"] != strconv.FormatInt(h.Size, 10) {
			return fmt.Errorf("%w: %q con campi PAX %q", ErrVoce, n, slices.Sorted(maps.Keys(h.PAXRecords)))
		}
		formato = tar.FormatPAX
	}
	if h.Format != formato {
		return fmt.Errorf("%w: %q in formato %v, atteso %v", ErrVoce, n, h.Format, formato)
	}
	if h.Mode != 0o600 || h.Uid != 0 || h.Gid != 0 || h.Uname != "" || h.Gname != "" || h.Devmajor != 0 || h.Devminor != 0 {
		return fmt.Errorf("%w: %q con permessi %o, utente %d/%q, gruppo %d/%q, attesi 0600 e 0 senza nome",
			ErrVoce, n, h.Mode, h.Uid, h.Uname, h.Gid, h.Gname)
	}
	return nil
}

// voce legge il contenuto della voce h, già controllata da controllaIntestazione, nel suo posto.
func (l *lettoreTar) voce(h *tar.Header) error {
	n := h.Name
	r, max, err := rango(n)
	if err != nil {
		return err
	}
	// L'ordine di Scrivi, con i nomi crescenti dentro ogni gruppo: esclude i duplicati e ogni voce
	// dopo snapshot.json; l'inventario arriva dopo tutti i file che descrive.
	if r < l.rangoUltima || (r == l.rangoUltima && n <= l.ultima) {
		return fmt.Errorf("%w: %q ripetuto o fuori posto (dopo %q)", ErrVoce, n, l.ultima)
	}
	l.rangoUltima, l.ultima, l.visto[r] = r, n, true
	if h.Size < 0 || (max >= 0 && h.Size > max) {
		return fmt.Errorf("%w: %q di %d byte, troppo grande", ErrVoce, n, h.Size)
	}
	switch r {
	case rangoDatabase:
		return l.estrai(h.Size)
	case rangoInventario:
		l.jInv, err = io.ReadAll(l.tr)
		return err
	case rangoFotografia:
		l.jFoto, err = io.ReadAll(l.tr)
		return err
	case rangoSegreto:
		b, err := io.ReadAll(l.tr)
		if err != nil {
			return err
		}
		// La chiave dei backup non sta mai nell'archivio (D-113): un archivio che la contiene non
		// conta come backup.
		if bytes.Contains(bytes.ToUpper(b), []byte(segnoChiaveAge)) {
			return fmt.Errorf("%w: %q contiene una chiave privata age", ErrVoce, n)
		}
		l.impronte[n] = impronta(b)
		return nil
	}
	hs := sha256.New()
	if _, err := io.Copy(hs, l.tr); err != nil {
		return fmt.Errorf("backup non verificato: %s: %w", n, err)
	}
	l.impronte[n] = hex.EncodeToString(hs.Sum(nil))
	return nil
}

// estrai copia il database nel file l.copia (0600, nuovo) calcolando lo SHA-256, dopo aver
// controllato che nel volume dei dati ci siano la sua dimensione e MargineSpazio liberi.
func (l *lettoreTar) estrai(size int64) error {
	libero, err := spazioLibero(l.dati)
	if err != nil {
		return fmt.Errorf("backup non verificato: spazio libero del volume dei dati non leggibile: %w", err)
	}
	if libero < uint64(size)+MargineSpazio {
		return fmt.Errorf("%w: per estrarre il database servono %d byte (database e 10 GiB di margine), ci sono %d byte",
			ErrSpazio, uint64(size)+MargineSpazio, libero)
	}
	f, err := os.OpenFile(l.copia, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("backup non verificato: database estratto: %w", err)
	}
	hs := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, hs), l.tr) // ogni lettura del file guarda già ctx
	if errC := f.Close(); err == nil {
		err = errC
	}
	if err != nil {
		return fmt.Errorf("backup non verificato: estrazione del database: %w", err)
	}
	l.impronte["data/gestrip.db"] = hex.EncodeToString(hs.Sum(nil))
	return nil
}

// reImpronta è uno SHA-256 come lo scrive Scrivi.
var reImpronta = regexp.MustCompile(`\A[0-9a-f]{64}\z`)

// inventario controlla inventory.json: scritto come lo scrive Scrivi e uguale, voce per voce, alle
// impronte calcolate.
func (l *lettoreTar) inventario() (map[string]string, error) {
	var inv map[string]string
	if err := json.Unmarshal(l.jInv, &inv); err != nil || inv == nil {
		return nil, fmt.Errorf("%w: inventory.json non è un elenco di impronte", ErrInventario)
	}
	if err := canonico(l.jInv, inv); err != nil {
		return nil, fmt.Errorf("%w: inventory.json %w", ErrInventario, err)
	}
	for n, i := range inv {
		if !reImpronta.MatchString(i) {
			return nil, fmt.Errorf("%w: impronta di %q non valida", ErrInventario, n)
		}
	}
	for _, n := range slices.Sorted(maps.Keys(l.impronte)) {
		i, ok := inv[n]
		if !ok {
			return nil, fmt.Errorf("%w: %q è nel tar ma non nell'inventario", ErrInventario, n)
		}
		if i != l.impronte[n] {
			return nil, fmt.Errorf("%w: SHA-256 di %q diverso dall'inventario", ErrInventario, n)
		}
	}
	for _, n := range slices.Sorted(maps.Keys(inv)) {
		if _, ok := l.impronte[n]; !ok {
			return nil, fmt.Errorf("%w: %q è nell'inventario ma manca nel tar", ErrInventario, n)
		}
	}
	return inv, nil
}

// fotografia controlla snapshot.json: scritto come lo scrive Scrivi, del Formato che conosciamo, con
// la versione, un'ora di cattura valida, le migrazioni 1…n e i conteggi.
func (l *lettoreTar) fotografia() (Fotografia, error) {
	var f Fotografia
	d := json.NewDecoder(bytes.NewReader(l.jFoto))
	d.DisallowUnknownFields()
	if err := d.Decode(&f); err != nil {
		return Fotografia{}, fmt.Errorf("%w: non si legge: %w", ErrFotografia, err)
	}
	if err := canonico(l.jFoto, f); err != nil {
		return Fotografia{}, fmt.Errorf("%w: %w", ErrFotografia, err)
	}
	if f.Formato != Formato {
		return Fotografia{}, fmt.Errorf("%w: formato %d, conosco solo il %d", ErrFotografia, f.Formato, Formato)
	}
	if strings.TrimSpace(f.Versione) == "" {
		return Fotografia{}, fmt.Errorf("%w: versione del programma vuota", ErrFotografia)
	}
	if t, err := time.Parse(db.FormatoIstante, f.CatturatoIl); err != nil || t.UTC().Format(db.FormatoIstante) != f.CatturatoIl {
		return Fotografia{}, fmt.Errorf("%w: ora di cattura %q non valida", ErrFotografia, f.CatturatoIl)
	}
	if len(f.Migrazioni) == 0 {
		return Fotografia{}, fmt.Errorf("%w: nessuna migrazione", ErrFotografia)
	}
	for i, m := range f.Migrazioni {
		if m != i+1 {
			return Fotografia{}, fmt.Errorf("%w: migrazioni fuori sequenza", ErrFotografia)
		}
	}
	if len(f.Conteggi) == 0 {
		return Fotografia{}, fmt.Errorf("%w: nessun conteggio", ErrFotografia)
	}
	for n, k := range f.Conteggi {
		if k < 0 {
			return Fotografia{}, fmt.Errorf("%w: conteggio negativo per %q", ErrFotografia, n)
		}
	}
	return f, nil
}

// canonico vuole che b sia esattamente json.Marshal(v), come lo scrive Scrivi: così chiavi doppie,
// campi sconosciuti, spazi, byte dopo il JSON o numeri scritti in un altro modo sono un rifiuto.
func canonico(b []byte, v any) error {
	atteso, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if !bytes.Equal(b, atteso) {
		return errors.New("non è scritto come lo scrive il backup (campi doppi, sconosciuti o byte in più)")
	}
	return nil
}

// dsnVerifica apre il database estratto in sola lettura, con trusted_schema spento: lo schema di un
// archivio non fidato non fa girare funzioni che non siano innocue (oggi il driver non ne ha, è una
// difesa in più).
func dsnVerifica(copia string) string {
	return db.URIDi(copia) + "?mode=ro&_pragma=trusted_schema(0)"
}

// controllaDatabase apre in sola lettura il database estratto: PRAGMA integrity_check completo deve dare
// solo «ok», poi migrazioni e conteggi (fotografaCon, la stessa del backup) devono essere uguali alla
// fotografia, tabella per tabella, senza tabelle in più o in meno.
func controllaDatabase(ctx context.Context, copia string, foto Fotografia) error {
	if dopoEstrazione != nil {
		dopoEstrazione(copia)
	}
	c, err := sql.Open("sqlite", dsnVerifica(copia))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrIntegrita, err)
	}
	defer func() { _ = c.Close() }() // sola lettura: niente da perdere
	esiti, err := colonna[string](ctx, c, `PRAGMA integrity_check`)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrIntegrita, err)
	}
	if len(esiti) != 1 || esiti[0] != "ok" {
		if len(esiti) > 3 {
			esiti = append(esiti[:3], "…")
		}
		return fmt.Errorf("%w: %q", ErrIntegrita, esiti)
	}
	d, err := fotografaCon(ctx, c)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFotografia, err)
	}
	if !slices.Equal(d.Migrazioni, foto.Migrazioni) {
		return fmt.Errorf("%w: migrazioni del database %v, nella fotografia %v", ErrFotografia, d.Migrazioni, foto.Migrazioni)
	}
	for _, n := range slices.Sorted(maps.Keys(foto.Conteggi)) {
		k, ok := d.Conteggi[n]
		if !ok {
			return fmt.Errorf("%w: la tabella %q della fotografia manca nel database", ErrFotografia, n)
		}
		if k != foto.Conteggi[n] {
			return fmt.Errorf("%w: %q ha %d righe, la fotografia ne dice %d", ErrFotografia, n, k, foto.Conteggi[n])
		}
	}
	for _, n := range slices.Sorted(maps.Keys(d.Conteggi)) {
		if _, ok := foto.Conteggi[n]; !ok {
			return fmt.Errorf("%w: la tabella %q del database manca nella fotografia", ErrFotografia, n)
		}
	}
	return nil
}

// impronta è lo SHA-256 di b in esadecimale.
func impronta(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// lettoreConContesto legge da r finché ctx non è finito; dopo, ogni lettura dà ctx.Err().
type lettoreConContesto struct {
	ctx context.Context
	r   io.Reader
}

func (l lettoreConContesto) Read(b []byte) (int, error) {
	if err := l.ctx.Err(); err != nil {
		return 0, err
	}
	return l.r.Read(b)
}

// lettoreContato conta i byte letti.
type lettoreContato struct {
	r io.Reader
	n int64
}

func (l *lettoreContato) Read(b []byte) (int, error) {
	n, err := l.r.Read(b)
	l.n += int64(n)
	return n, err
}

// lettoreCoda conta i byte letti, quanti byte zero ci sono alla fine di quello letto finora e tiene
// gli ultimi 512 byte letti: dopo tar.Reader.Next sono il blocco grezzo dell'intestazione (il lettore
// legge l'intestazione per ultima, a blocchi interi).
type lettoreCoda struct {
	r      io.Reader
	n      int64
	zeri   int64
	blocco [512]byte
}

func (l *lettoreCoda) Read(b []byte) (int, error) {
	n, err := l.r.Read(b)
	l.n += int64(n)
	if n >= len(l.blocco) {
		copy(l.blocco[:], b[n-len(l.blocco):n])
	} else {
		copy(l.blocco[:], l.blocco[n:])
		copy(l.blocco[len(l.blocco)-n:], b[:n])
	}
	if k := len(b[:n]) - len(bytes.TrimRight(b[:n], "\x00")); k == n {
		l.zeri += int64(n)
	} else {
		l.zeri = int64(k)
	}
	return n, err
}
