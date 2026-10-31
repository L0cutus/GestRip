package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// I file media dell'import (slice 023, OPS-02, OPS-05). Lo standalone tiene i file in una cartella
// piatta (/data/media, MediaStore): il nome di ogni file è lo SHA-256 in esadecimale minuscolo dei
// suoi byte, permessi 0400, cartella 0700; le righe del database nominano i file con quel nome
// (ColonneMedia). Le miniature stanno in un'altra cartella (thumbnails accanto al database), sono
// una cache che si rifà e non si importano: si contano soltanto.
//
// L'import legge la cartella della sorgente senza scriverci, copia ogni file nominato nella
// cartella di lavoro privata controllandone l'indirizzo, e la cartella prende il suo nome
// (CartellaMediaDi, accanto alla destinazione come in PIANO §1.9) solo con il verdetto
// riconciliato, prima del database (Importa).

// CartellaMedia è il nome della cartella media accanto al database (PIANO §1.9).
const CartellaMedia = "media"

// CartellaMediaDi è la cartella media che va con il database destinazione.
func CartellaMediaDi(destinazione string) string {
	return filepath.Join(filepath.Dir(destinazione), CartellaMedia)
}

// MediaReport è la parte dei media del report: per famiglia (una colonna di ColonneMedia) le
// righe che nominano un file e com'è andata; i file e i byte copiati contano ogni file una volta
// sola (più righe possono nominare lo stesso file); InPiu sono i file della cartella che nessuna
// riga nomina, non copiati. Mai nomi di file, indirizzi o contenuti.
type MediaReport struct {
	// Importati dice se la cartella media della sorgente è stata data (-media): solo allora i file
	// si copiano e media/ si attiva. È false senza -media, anche con SenzaMedia.
	Importati   bool            `json:"importati"`
	Famiglie    []FamigliaMedia `json:"famiglie"`
	FileCopiati int64           `json:"file_copiati"`
	ByteCopiati int64           `json:"byte_copiati"`
	InPiu       int64           `json:"in_piu"`
	// Miniature sono contate, mai copiate; nil se la cartella non è stata data.
	Miniature *ConteggioFile `json:"miniature"`
}

// FamigliaMedia sono i conteggi di una colonna che nomina file. Byte è la somma dei file delle
// righe copiate (un file nominato da due righe conta due volte).
type FamigliaMedia struct {
	Tabella         string `json:"tabella"`
	Colonna         string `json:"colonna"`
	Attesi          int64  `json:"attesi"`
	Copiati         int64  `json:"copiati"`
	Mancanti        int64  `json:"mancanti"`
	HashDiversi     int64  `json:"hash_diversi"`
	AltreDifferenze int64  `json:"altre_differenze"`
	// GiaCancellati sono le righe facoltative (la coda delle cancellazioni) il cui file non c'è più.
	GiaCancellati int64 `json:"gia_cancellati"`
	Byte          int64 `json:"byte"`
}

// ConteggioFile è un numero di file e la loro dimensione.
type ConteggioFile struct {
	File int64 `json:"file"`
	Byte int64 `json:"byte"`
}

// Agganci dei test: dopoUnFileMedia gira dopo ogni file media guardato, dopoLaCartellaMedia
// subito dopo che la cartella media ha preso il suo nome, traLstatEOpen fra Lstat e l'apertura di
// un file della sorgente. nil fuori dai test.
var (
	dopoUnFileMedia     func()
	dopoLaCartellaMedia func()
	traLstatEOpen       func(p string)
)

// maxByteMedia è la dimensione massima di un file media: quella che lo standalone accetta in
// lettura (lib/gestrip_standalone/media_store.ex a go-baseline: @max_output_bytes 16_777_216
// alla riga 2, read_object vuole size in 1..@max_output_bytes alla riga 130).
const maxByteMedia = 16_777_216

// RestiDiImport sono i resti di un import interrotto accanto al database destinazione, in ordine:
// le cartelle di lavoro .gestrip-import-* e, se il database non c'è, la cartella media (dopo un
// arresto di colpo fra l'attivazione dei media e quella del database). serve e un import nuovo
// rifiutano finché ci sono: si tolgono a mano.
func RestiDiImport(destinazione string) ([]string, error) {
	resti, err := filepath.Glob(filepath.Join(filepath.Dir(destinazione), prefissoLavoro+"*"))
	if err != nil {
		return nil, err
	}
	_, err = os.Lstat(destinazione)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		m := CartellaMediaDi(destinazione)
		if _, err := os.Lstat(m); err == nil {
			resti = append(resti, m)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	case err != nil:
		return nil, err
	}
	return resti, nil
}

// senzaPercorsi è senzaPercorso per più errori insieme (i nil si tolgono): i messaggi dei media
// non nominano mai un file, che si chiama come il suo indirizzo.
func senzaPercorsi(errs ...error) error {
	var out []error
	for _, e := range errs {
		if e != nil {
			out = append(out, senzaPercorso(e))
		}
	}
	return errors.Join(out...)
}

// reChiaveMedia è la forma di un indirizzo di contenuto (MediaStore.read_object): così una chiave
// non può mai uscire dalla cartella («..», un percorso assoluto, una barra).
var reChiaveMedia = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Gli esiti di un file nominato che non torna: sono anche i codici del report.
const (
	mediaMancante    = "media/mancante"
	mediaNonRegolare = "media/non_regolare"
	mediaIlleggibile = "media/illeggibile"
	mediaHashDiverso = "media/hash_diverso"
	mediaVuoto       = "media/vuoto"
	mediaTroppo      = "media/troppo_grande"
	mediaByteDiversi = "media/byte_diversi"
)

// controllaCartelleMedia sono i rifiuti dei media prima di aprire la sorgente, senza file: una
// sola tra Media e SenzaMedia; la cartella media della destinazione non c'è ancora; le cartelle
// della sorgente (media e miniature) sono cartelle vere, non link, e né la destinazione né il
// report ci stanno dentro (l'import ci scriverebbe). I messaggi non nominano le cartelle della
// sorgente.
func controllaCartelleMedia(o Opzioni) error {
	if o.Media != "" && o.SenzaMedia {
		return errors.New("-media e -senza-media: una sola delle due")
	}
	// Scelta prudente in attesa di Stefano: senza media solo con dati inventati.
	if o.SenzaMedia && !o.Sintetico {
		return errors.New("-senza-media per ora vale solo con -sintetico")
	}
	if err := nonEsiste("la cartella media della destinazione", CartellaMediaDi(o.Destinazione)); err != nil {
		return fmt.Errorf("%w; se nessun database la usa è probabilmente il resto di un import interrotto: toglila a mano", err)
	}
	for _, c := range []struct{ cosa, p string }{{"media della sorgente", o.Media}, {"delle miniature", o.Miniature}} {
		if c.p == "" {
			continue
		}
		fi, err := os.Lstat(c.p)
		if err != nil {
			return fmt.Errorf("la cartella %s: %w", c.cosa, senzaPercorso(err))
		}
		if !fi.IsDir() {
			return fmt.Errorf("la cartella %s non è una cartella (un link non vale)", c.cosa)
		}
		for _, p := range []string{o.Destinazione, o.Report} {
			dentro, err := dentroDi(filepath.Dir(p), c.p)
			if err != nil {
				return fmt.Errorf("la cartella %s: %w", c.cosa, senzaPercorso(err))
			}
			if dentro {
				return fmt.Errorf("la destinazione e il report non possono stare dentro la cartella %s", c.cosa)
			}
		}
	}
	return nil
}

// dentroDi dice se la cartella dir è base o sta sotto base, con i link risolti.
func dentroDi(dir, base string) (bool, error) {
	d, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, err
	}
	b, err := filepath.EvalSymlinks(base)
	if err != nil {
		return false, err
	}
	if d, err = filepath.Abs(d); err != nil {
		return false, err
	}
	if b, err = filepath.Abs(b); err != nil {
		return false, err
	}
	rel, err := filepath.Rel(b, d)
	if err != nil {
		return false, nil // su volumi diversi (Windows): non dentro
	}
	// «.» (la stessa cartella) passa anche lei.
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}

// esitoFile è com'è andato un file nominato: stato vuoto se copiato con l'indirizzo giusto.
type esitoFile struct {
	stato    string
	byte     int64
	impronta [32]byte
}

// rigaMedia è una riga che nomina un file: rowid, chiave e, dove ci sono, sha256 e byte salvati.
type rigaMedia struct {
	id               int64
	chiave, sha, dim any
}

// importaMedia guarda ogni riga della sorgente che nomina un file (ColonneMedia) e, con la
// cartella media della sorgente, copia i file nella cartella dest (nuova, 0700) controllandoli:
// il file c'è ed è un file regolare (non un link, non una cartella), i suoi byte hanno lo SHA-256
// del nome, che è anche lo sha256 e la dimensione salvati nella riga dove ci sono. Ogni problema
// è una differenza bloccante con tabella, id e colonna. I file che nessuna riga nomina sono un
// avviso (lo standalone li tiene finché la pulizia degli orfani non li raccoglie, RIP-S-32) e non
// si copiano. Senza cartella: bloccante se una riga chiede un file, o un avviso con SenzaMedia.
// L'errore è per tutto quello che non è dei dati (scrittura, interruzione).
func importaMedia(ctx context.Context, src *sql.DB, o Opzioni, dest string, rep *Report) error {
	mr := &MediaReport{Importati: o.Media != "", Famiglie: []FamigliaMedia{}}
	rep.Media = mr
	if o.Media != "" {
		if err := os.Mkdir(dest, 0o700); err != nil {
			return fmt.Errorf("cartella media di lavoro: %w", senzaPercorso(err))
		}
		if err := os.Chmod(dest, 0o700); err != nil {
			return fmt.Errorf("cartella media di lavoro: %w", senzaPercorso(err))
		}
	}
	nominati := map[string]bool{}
	file := map[string]esitoFile{}
	var richiesti int64
	for _, cm := range ColonneMedia {
		f := FamigliaMedia{Tabella: cm.Tabella, Colonna: cm.Chiave}
		righe, err := leggiRigheMedia(ctx, src, cm)
		if err != nil {
			return err
		}
		for _, r := range righe {
			f.Attesi++
			if !cm.Facoltativo {
				richiesti++
			}
			if o.Media == "" {
				continue
			}
			voce := func(codice, col string) {
				rep.blocca(Voce{Codice: codice, Tabella: cm.Tabella, Id: ptr(r.id), Colonna: col})
			}
			k, ok := r.chiave.(string)
			if !ok || !reChiaveMedia.MatchString(k) {
				f.AltreDifferenze++
				voce("media/chiave_non_valida", cm.Chiave)
				continue
			}
			nominati[k] = true
			e, visto := file[k]
			if !visto {
				// La dimensione della riga, dove c'è, si guarda prima di copiare (-2: assente o non
				// intera, quindi diversa da qualunque file).
				atteso := int64(-1)
				if cm.Byte != "" {
					atteso = -2
					if n, ok := r.dim.(int64); ok {
						atteso = n
					}
				}
				if e, err = copiaFileMedia(o.Media, dest, k, atteso); err != nil {
					return err
				}
				// Un file che non va bene per questa riga può andare bene per un'altra: non si ricorda.
				if e.stato != mediaByteDiversi {
					file[k] = e
				}
				if dopoUnFileMedia != nil {
					dopoUnFileMedia()
				}
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			switch e.stato {
			case "":
			case mediaMancante:
				if cm.Facoltativo {
					f.GiaCancellati++ // in coda di cancellazione: il file può essere già andato
					continue
				}
				f.Mancanti++
				voce(e.stato, cm.Chiave)
				continue
			case mediaHashDiverso:
				f.HashDiversi++
				voce(e.stato, cm.Chiave)
				continue
			case mediaByteDiversi:
				f.AltreDifferenze++
				voce(e.stato, cm.Byte)
				continue
			default:
				f.AltreDifferenze++
				voce(e.stato, cm.Chiave)
				continue
			}
			if cm.Sha256 != "" {
				if b, _ := r.sha.([]byte); !bytes.Equal(b, e.impronta[:]) {
					f.HashDiversi++
					voce(mediaHashDiverso, cm.Sha256)
					continue
				}
			}
			if cm.Byte != "" {
				if n, _ := r.dim.(int64); n != e.byte {
					f.AltreDifferenze++
					voce(mediaByteDiversi, cm.Byte)
					continue
				}
			}
			f.Copiati++
			f.Byte += e.byte
		}
		mr.Famiglie = append(mr.Famiglie, f)
	}
	if err := contaMiniature(o.Miniature, mr); err != nil {
		return err
	}
	if o.Media == "" {
		if richiesti > 0 && o.SenzaMedia {
			rep.avvisa(Voce{Codice: "media/non_importati", Righe: ptr(richiesti)})
		} else if richiesti > 0 {
			rep.blocca(Voce{Codice: "media/cartella_non_data", Righe: ptr(richiesti)})
		}
		return nil
	}
	for _, e := range file {
		if e.stato == "" {
			mr.FileCopiati++
			mr.ByteCopiati += e.byte
		}
	}
	voci, err := os.ReadDir(o.Media)
	if err != nil {
		return fmt.Errorf("cartella media della sorgente: %w", senzaPercorso(err))
	}
	for _, v := range voci {
		if !nominati[v.Name()] {
			mr.InPiu++
		}
	}
	if mr.InPiu > 0 {
		rep.avvisa(Voce{Codice: "media/in_piu", Righe: ptr(mr.InPiu)})
	}
	if err := sincronizzaCartella(dest); err != nil {
		return fmt.Errorf("cartella media di lavoro: %w", senzaPercorso(err))
	}
	return nil
}

// leggiRigheMedia legge le righe di una colonna di ColonneMedia con la chiave non NULL, in ordine
// di rowid.
func leggiRigheMedia(ctx context.Context, src *sql.DB, cm ColonnaMedia) ([]rigaMedia, error) {
	col := func(c string) string {
		if c == "" {
			return "NULL"
		}
		return virgolette(c)
	}
	rows, err := src.QueryContext(ctx, fmt.Sprintf(`SELECT rowid, %s, %s, %s FROM %s WHERE %s IS NOT NULL ORDER BY rowid`,
		virgolette(cm.Chiave), col(cm.Sha256), col(cm.Byte), virgolette(cm.Tabella), virgolette(cm.Chiave)))
	if err != nil {
		return nil, err
	}
	var righe []rigaMedia
	for rows.Next() {
		var r rigaMedia
		if err := rows.Scan(&r.id, &r.chiave, &r.sha, &r.dim); err != nil {
			_ = rows.Close()
			return nil, err
		}
		righe = append(righe, r)
	}
	return righe, errors.Join(rows.Err(), rows.Close())
}

// copiaFileMedia copia il file chiave dalla cartella della sorgente (in sola lettura) in dest,
// un file nuovo che finisce 0400 e sincronizzato, e lo confronta con il suo nome: SHA-256 e
// dimensione sono quelli dei byte copiati. Prima di copiare guarda la dimensione: vuoto, oltre
// maxByteMedia o diversa da atteso (-1: la riga non ha una dimensione) è un esito, senza
// scrivere un byte.
// Un file che non c'è, non è regolare, cambia mentre si apre o si legge, non si legge o ha un
// altro SHA-256 è un esito (e la copia si toglie); l'errore è per la scrittura della copia, e non
// nomina il file.
func copiaFileMedia(sorgente, dest, chiave string, atteso int64) (e esitoFile, err error) {
	p := filepath.Join(sorgente, chiave)
	prima, err := os.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return esitoFile{stato: mediaMancante}, nil
	case err != nil:
		return esitoFile{stato: mediaIlleggibile}, nil
	case !prima.Mode().IsRegular():
		return esitoFile{stato: mediaNonRegolare}, nil
	case prima.Size() == 0:
		return esitoFile{stato: mediaVuoto}, nil
	case prima.Size() > maxByteMedia:
		return esitoFile{stato: mediaTroppo}, nil
	case atteso != -1 && prima.Size() != atteso:
		return esitoFile{stato: mediaByteDiversi}, nil
	}
	if traLstatEOpen != nil {
		traLstatEOpen(p)
	}
	in, err := apriFileMedia(p)
	if err != nil {
		return esitoFile{stato: mediaIlleggibile}, nil
	}
	defer func() { _ = in.Close() }() // in sola lettura: niente da perdere
	if aperto, err := in.Stat(); err != nil || !os.SameFile(prima, aperto) {
		return esitoFile{stato: mediaNonRegolare}, nil
	}
	errCopia := func(errs ...error) error {
		return fmt.Errorf("copia di un file media nella cartella di lavoro: %w", senzaPercorsi(errs...))
	}
	q := filepath.Join(dest, chiave)
	out, err := os.OpenFile(q, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return e, errCopia(err)
	}
	h := sha256.New()
	buf := make([]byte, 1<<20)
	// Al più un byte oltre la dimensione vista: un file che cresce non si copia tutto.
	r := io.LimitReader(in, prima.Size()+1)
	for {
		n, errL := r.Read(buf)
		if n > 0 {
			if _, err := out.Write(buf[:n]); err != nil {
				return e, errCopia(err, out.Close(), os.Remove(q))
			}
			h.Write(buf[:n])
			e.byte += int64(n)
		}
		if errors.Is(errL, io.EOF) {
			break
		}
		if errL != nil {
			if err := senzaPercorsi(out.Close(), os.Remove(q)); err != nil {
				return e, errCopia(err)
			}
			return esitoFile{stato: mediaIlleggibile}, nil
		}
	}
	err = out.Chmod(0o400)
	if err == nil {
		err = out.Sync()
	}
	if err = errors.Join(err, out.Close()); err != nil {
		return e, errCopia(err, os.Remove(q))
	}
	if e.byte != prima.Size() {
		if err := os.Remove(q); err != nil {
			return e, errCopia(err)
		}
		return esitoFile{stato: mediaNonRegolare}, nil // cambiato mentre si leggeva
	}
	copy(e.impronta[:], h.Sum(nil))
	if hex.EncodeToString(e.impronta[:]) != chiave {
		if err := os.Remove(q); err != nil {
			return e, errCopia(err)
		}
		return esitoFile{stato: mediaHashDiverso}, nil
	}
	return e, nil
}

// contaMiniature conta i file regolari della cartella delle miniature e i loro byte, senza
// copiarli (una cache che si rifà, OPS-02).
func contaMiniature(dir string, mr *MediaReport) error {
	if dir == "" {
		return nil
	}
	voci, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("cartella delle miniature: %w", senzaPercorso(err))
	}
	c := &ConteggioFile{}
	for _, v := range voci {
		fi, err := v.Info()
		if err != nil {
			return fmt.Errorf("cartella delle miniature: %w", senzaPercorso(err))
		}
		if fi.Mode().IsRegular() {
			c.File++
			c.Byte += fi.Size()
		}
	}
	mr.Miniature = c
	return nil
}
