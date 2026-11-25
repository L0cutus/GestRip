package backup

// La chiave dei backup e la cifratura age (slice 120, D-113). Lo standalone (scripts/packaging.py a
// go-baseline: backup_quiesced, private_file) cifra il tar con il comando age verso la chiave pubblica
// in un file uuid.pending, fa fsync e lo rinomina in uuid.tar.age; l'identità (chiave privata) è un
// file privato obbligatorio. Il Go cifra con la libreria filippo.io/age (PIANO §2.5, stesso formato del
// comando age: l'archivio si apre anche a mano con «age -d -i chiave»), in flusso: il tar in chiaro
// passa solo in memoria, sul disco della destinazione arrivano solo byte cifrati.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
)

// EstensioneArchivio è la fine del nome di un archivio cifrato; SuffissoInCorso quella del file
// mentre si scrive (lo standalone usa gli stessi due).
const (
	EstensioneArchivio = ".tar.age"
	SuffissoInCorso    = ".pending"
)

// MargineDestinazione è lo spazio che deve restare libero nella destinazione oltre alla stima
// dell'archivio cifrato. La stima è già per eccesso (database con il WAL, ogni file con la sua
// intestazione); il margine copre un file salvato durante il backup e i metadati del file system.
const MargineDestinazione = 64 << 20

// maxChiave è la dimensione massima del file della chiave: age-keygen ne scrive circa 190 byte.
const maxChiave = 4 << 10

// Gli errori della chiave e della cifratura.
var (
	ErrChiaveEsistente    = errors.New("backup: la chiave dei backup esiste già, non la sovrascrivo")
	ErrChiaveAssente      = errors.New("backup: chiave dei backup assente (si crea con «gestrip chiave-backup»)")
	ErrChiavePermessi     = errors.New("backup: la chiave dei backup deve essere un file 0600 dell'utente del programma")
	ErrArchivioEsistente  = errors.New("backup: un archivio con lo stesso nome esiste già, non lo sovrascrivo")
	ErrSpazioDestinazione = errors.New("backup: spazio libero insufficiente nella destinazione")
	ErrDestinazione       = errors.New("backup: destinazione non valida")
)

// Solo nei test: primaDiPubblicare gira prima della rinomina che pubblica la chiave o l'archivio (il
// nome finale), dopoCifratura appena aperta la cifratura, primaDiTogliere prima di togliere il
// .pending dopo un errore; erroreScrittura fa fallire ogni scrittura nel .pending.
var (
	primaDiPubblicare func(finale string)
	dopoCifratura     func()
	primaDiTogliere   func(pending string)
	erroreScrittura   error
)

// scaricaCache è scaricaDallaCache (posix_fadvise DONTNEED dopo l'fsync, nota 7b); una variabile per i
// test.
var scaricaCache = scaricaDallaCache

// ChiaveNuova è la chiave appena creata: la privata (da scrivere sul foglio di carta, si mostra una
// volta sola), la pubblica e il file che la tiene.
type ChiaveNuova struct {
	Privata, Pubblica, File string
}

// FileChiave è il file della chiave dei backup nella cartella dei dati.
func FileChiave(dati string) string {
	return filepath.Join(dati, CartellaSegreti, NomeChiaveBackup)
}

// CreaChiave crea la chiave dei backup (un'identità age X25519) in secrets/chiave_backup della
// cartella dei dati, 0600, nel formato di age-keygen (due righe di commento con l'ora e la chiave
// pubblica, poi la privata). Una chiave che c'è già, anche rovinata, non si tocca mai
// (ErrChiaveEsistente): i backup fatti con lei diventerebbero illeggibili. Il file si scrive in un
// temporaneo 0600 della stessa cartella, con fsync, e si pubblica con una rinomina che non
// sovrascrive; il temporaneo si toglie sempre.
func CreaChiave(dati string, adesso time.Time) (ChiaveNuova, error) {
	dir := filepath.Join(dati, CartellaSegreti)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ChiaveNuova{}, fmt.Errorf("backup: cartella dei segreti: %w", err)
	}
	finale := FileChiave(dati)
	if _, err := os.Lstat(finale); err == nil {
		return ChiaveNuova{}, ErrChiaveEsistente
	} else if !errors.Is(err, fs.ErrNotExist) {
		return ChiaveNuova{}, fmt.Errorf("backup: chiave dei backup: %w", err)
	}
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return ChiaveNuova{}, err
	}
	c := ChiaveNuova{Privata: id.String(), Pubblica: id.Recipient().String(), File: finale}
	contenuto := fmt.Sprintf("# created: %s\n# public key: %s\n%s\n", adesso.UTC().Format(time.RFC3339), c.Pubblica, c.Privata)
	tmp, err := os.CreateTemp(dir, "."+NomeChiaveBackup+"-") // 0600
	if err != nil {
		return ChiaveNuova{}, fmt.Errorf("backup: chiave dei backup: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // dopo la rinomina non c'è più: niente da togliere
	_, err = tmp.WriteString(contenuto)
	if err == nil {
		err = tmp.Sync()
	}
	if errClose := tmp.Close(); err == nil {
		err = errClose
	}
	if err != nil {
		return ChiaveNuova{}, fmt.Errorf("backup: chiave dei backup: %w", err)
	}
	if primaDiPubblicare != nil {
		primaDiPubblicare(finale)
	}
	if err := rinominaSenzaSovrascrivere(tmp.Name(), finale); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return ChiaveNuova{}, ErrChiaveEsistente
		}
		return ChiaveNuova{}, fmt.Errorf("backup: chiave dei backup: %w", err)
	}
	if err := sincronizzaCartella(dir); err != nil {
		return ChiaveNuova{}, fmt.Errorf("backup: chiave dei backup scritta, ma la cartella non si sincronizza: %w", err)
	}
	return c, nil
}

// LeggiChiave legge la chiave dei backup della cartella dei dati: un file normale (non un
// collegamento) dell'utente del programma, senza permessi per gli altri (private_file dello
// standalone), con una sola identità X25519. Senza file: ErrChiaveAssente.
func LeggiChiave(dati string) (*age.X25519Identity, error) {
	f, err := apriSenzaCollegamenti(FileChiave(dati))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrChiaveAssente
	}
	if err != nil {
		return nil, fmt.Errorf("backup: chiave dei backup: %w", err)
	}
	defer func() { _ = f.Close() }() // sola lettura
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0 || !dellUtente(fi) {
		return nil, ErrChiavePermessi
	}
	b, err := io.ReadAll(io.LimitReader(f, maxChiave+1))
	if err != nil {
		return nil, fmt.Errorf("backup: chiave dei backup: %w", err)
	}
	if len(b) > maxChiave {
		return nil, errors.New("backup: il file della chiave dei backup è troppo grande")
	}
	ids, err := age.ParseIdentities(bytes.NewReader(b))
	if err != nil {
		// Il messaggio di age può citare la riga: non si riporta, è la chiave.
		return nil, errors.New("backup: il file della chiave dei backup non contiene una chiave age valida")
	}
	if len(ids) != 1 {
		return nil, fmt.Errorf("backup: il file della chiave dei backup ha %d chiavi, ne serve una sola", len(ids))
	}
	id, ok := ids[0].(*age.X25519Identity)
	if !ok {
		return nil, errors.New("backup: la chiave dei backup non è una chiave age X25519")
	}
	return id, nil
}

// NomeArchivio è il nome dell'archivio catturato all'istante t: data e ora UTC, al secondo
// (gestrip-backup-20261125T023000Z.tar.age), così l'ordine dei nomi è quello del tempo anche al
// cambio dell'ora legale.
func NomeArchivio(t time.Time) string {
	return "gestrip-backup-" + t.UTC().Format("20060102T150405Z") + EstensioneArchivio
}

// Risultato è l'archivio cifrato scritto: quello che c'è dentro (Esito di Scrivi), il file, i suoi
// byte e il suo SHA-256 (la 122 li registra).
type Risultato struct {
	Esito
	File        string
	ByteCifrati int64
	SHA256      string
}

// Cifra scrive nella cartella un archivio nuovo del backup, cifrato verso la chiave pubblica della
// chiave dei backup. Nell'ordine:
//  1. legge la chiave (LeggiChiave) e controlla la destinazione: una cartella che c'è, fuori dalla
//     cartella dei dati;
//  2. controlla lo spazio libero nella destinazione (spazioDestinazione);
//  3. apre <nome>.pending (0600, mai sopra un file che c'è) e ci scrive il tar di Scrivi passato
//     da age, senza copie in chiaro: il tar passa solo in memoria;
//  4. fsync del file e della cartella, rinomina in <nome> senza sovrascrivere, fsync della cartella.
//
// Con un errore il .pending si toglie; un archivio che c'è già non si tocca mai, e nessun archivio
// vecchio si cancella qui (la rotazione è Destinazione.Ruota, 123). L'archivio non è ancora verificato
// (121). Dopo l'fsync le sue pagine escono dalla cache (scaricaCache, nota 7b).
func Cifra(ctx context.Context, s Sorgente, cartella string) (r Risultato, err error) {
	dati := filepath.Dir(s.DB.Percorso())
	id, err := LeggiChiave(dati)
	if err != nil {
		return Risultato{}, err
	}
	if err := controllaDestinazione(cartella, dati); err != nil {
		return Risultato{}, err
	}
	serve, err := spazioDestinazione(s)
	if err != nil {
		return Risultato{}, err
	}
	libero, err := spazioLibero(cartella)
	if err != nil {
		return Risultato{}, fmt.Errorf("backup: spazio libero della destinazione non leggibile, non parto: %w", err)
	}
	if libero < serve {
		return Risultato{}, fmt.Errorf("%w: servono %d byte (archivio stimato e margine), ci sono %d byte", ErrSpazioDestinazione, serve, libero)
	}
	finale := filepath.Join(cartella, NomeArchivio(s.Orologio.Adesso()))
	if _, err := os.Lstat(finale); err == nil {
		return Risultato{}, ErrArchivioEsistente
	}
	pending := finale + SuffissoInCorso
	f, err := os.OpenFile(pending, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Risultato{}, fmt.Errorf("backup: file %s: %w", filepath.Base(pending), err)
	}
	aperto, pubblicato := true, false
	defer func() {
		if pubblicato {
			return
		}
		if aperto {
			_ = f.Close() // il file si toglie qui sotto: niente da perdere
		}
		if primaDiTogliere != nil {
			primaDiTogliere(pending)
		}
		if errRm := os.Remove(pending); errRm != nil && !errors.Is(errRm, fs.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("backup: %s da togliere a mano: %w", pending, errRm))
		}
	}()
	h := sha256.New()
	c := &contatore{w: io.MultiWriter(scrittoreProva{f}, h)}
	aw, err := age.Encrypt(c, id.Recipient())
	if err != nil {
		return Risultato{}, fmt.Errorf("backup: cifratura: %w", err)
	}
	if dopoCifratura != nil {
		dopoCifratura()
	}
	e, err := Scrivi(ctx, s, aw)
	if err != nil {
		return Risultato{}, err
	}
	if err := aw.Close(); err != nil {
		return Risultato{}, fmt.Errorf("backup: chiusura della cifratura: %w", err)
	}
	if err := f.Sync(); err != nil {
		return Risultato{}, fmt.Errorf("backup: fsync dell'archivio: %w", err)
	}
	// Nota 7b (slice 123): le pagine appena scritte, ora pulite, escono dalla cache, così la verifica che
	// segue rilegge dal supporto. Solo un aiuto (il kernel può ignorarlo): un errore non ferma niente.
	_ = scaricaCache(f)
	aperto = false
	if err := f.Close(); err != nil {
		return Risultato{}, fmt.Errorf("backup: chiusura dell'archivio: %w", err)
	}
	if err := sincronizzaCartella(cartella); err != nil {
		return Risultato{}, fmt.Errorf("backup: fsync della destinazione: %w", err)
	}
	if primaDiPubblicare != nil {
		primaDiPubblicare(finale)
	}
	if err := rinominaSenzaSovrascrivere(pending, finale); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return Risultato{}, ErrArchivioEsistente
		}
		return Risultato{}, fmt.Errorf("backup: rinomina dell'archivio: %w", err)
	}
	pubblicato = true
	r = Risultato{Esito: e, File: finale, ByteCifrati: c.n, SHA256: hex.EncodeToString(h.Sum(nil))}
	if err := sincronizzaCartella(cartella); err != nil {
		// L'archivio c'è ed è completo: non si toglie (nessun archivio si cancella), ma la rinomina
		// potrebbe non sopravvivere a un blackout.
		return r, fmt.Errorf("backup: archivio scritto, ma fsync della destinazione non riuscito: %w", err)
	}
	return r, nil
}

// scrittoreProva scrive nel .pending; nei test erroreScrittura lo fa fallire.
type scrittoreProva struct{ w io.Writer }

func (s scrittoreProva) Write(b []byte) (int, error) {
	if erroreScrittura != nil {
		return 0, erroreScrittura
	}
	return s.w.Write(b)
}

// controllaDestinazione vuole una cartella che c'è, fuori dalla cartella dei dati (collegamenti
// risolti): un archivio in media/ o in secrets/ fermerebbe i backup seguenti, e uno nello stesso
// disco dei dati non salva niente.
func controllaDestinazione(cartella, dati string) error {
	if cartella == "" {
		return fmt.Errorf("%w: cartella vuota", ErrDestinazione)
	}
	fi, err := os.Stat(cartella)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDestinazione, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%w: %s non è una cartella", ErrDestinazione, cartella)
	}
	vera, err := filepath.EvalSymlinks(cartella)
	if err == nil {
		vera, err = filepath.Abs(vera)
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDestinazione, err)
	}
	datiVeri, err := filepath.EvalSymlinks(dati)
	if err == nil {
		datiVeri, err = filepath.Abs(datiVeri)
	}
	if err != nil {
		return fmt.Errorf("backup: cartella dei dati: %w", err)
	}
	rel, err := filepath.Rel(datiVeri, vera)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDestinazione, err)
	}
	if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: %s sta nella cartella dei dati", ErrDestinazione, cartella)
	}
	return nil
}

// Byte in più per ogni file del tar nella stima: intestazione con il record PAX (tre blocchi), il
// riempimento dell'ultimo blocco e la riga dell'inventario.
const perFileStima = 3*512 + 512 + 256

// Byte fissi della stima: snapshot.json, la fine del tar e l'intestazione di age.
const fissoStima = 256 << 10

// spazioDestinazione stima per eccesso lo spazio che l'archivio cifrato prende nella destinazione,
// più MargineDestinazione: database e WAL (la copia VACUUM INTO non è più grande), ogni media e
// ogni segreto che entra, perFileStima per file, fissoStima, e 16 byte di age ogni 64 KiB. Un
// file inatteso in media/ o in secrets/ è già qui un rifiuto, come in Scrivi.
func spazioDestinazione(s Sorgente) (uint64, error) {
	var tar uint64 = fissoStima
	for _, n := range []string{s.DB.Percorso(), s.DB.Percorso() + "-wal"} {
		fi, err := os.Stat(n)
		if errors.Is(err, fs.ErrNotExist) && n != s.DB.Percorso() {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("backup: dimensione del database: %w", err)
		}
		tar += uint64(fi.Size())
	}
	tar += perFileStima
	chiavi, err := elencaMedia(s.Archivio.Cartella())
	if err != nil {
		return 0, err
	}
	for _, k := range chiavi {
		fi, err := os.Lstat(filepath.Join(s.Archivio.Cartella(), k))
		if errors.Is(err, fs.ErrNotExist) {
			continue // tolto dalla coda nel frattempo: non entrerà
		}
		if err != nil {
			return 0, fmt.Errorf("backup: dimensione di un media: %w", err)
		}
		tar += uint64(fi.Size()) + perFileStima
	}
	segreti, err := leggiSegreti(filepath.Join(filepath.Dir(s.DB.Percorso()), CartellaSegreti))
	if err != nil {
		return 0, err
	}
	for _, b := range segreti {
		tar += uint64(len(b)) + perFileStima
	}
	return tar + (tar/(64<<10)+1)*16 + MargineDestinazione, nil
}

// sincronizzaCartella fa fsync della cartella: una rinomina o un file nuovo sopravvivono a un
// blackout.
func sincronizzaCartella(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	return errors.Join(err, d.Close())
}
