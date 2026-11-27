package backup

// Il ripristino isolato (slice 130, OPS-18, OPS-19, D-12, risposta I del piano della T5.2). Lo standalone
// (restore.sh → packaging.py install «restored», extract_backup, validate_archive a go-baseline) decifra l'archivio,
// crea sempre un'installazione nuova, controlla conteggi e una riparazione campione prima di avviare HTTP, e senza
// --live la isola su una rete Docker interna. Il Go:
//   - scrive solo in una cartella dei dati nuova (che non c'è o è vuota), mai sopra dati esistenti;
//   - fa la verifica completa della 121 (la stessa funzione di ogni backup e della prova settimanale della 125,
//     verificaFile) estraendo però tutto: database, media e segreti, in una cartella temporanea accanto alla
//     destinazione;
//   - mette il database nello stato isolato (db.IsolaRipristino: canali e copia remota spenti e messi da parte,
//     registri dei backup svuotati, chiave del server rinnovata, riga ripristino.isolato che serve legge a ogni
//     avvio);
//   - solo alla fine rinomina la cartella temporanea nella destinazione: un errore o uno stop non lasciano mai una
//     cartella dei dati a metà.
//
// La conferma «questa è ora la macchina vera» (131) non è qui.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/media"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// prefissoRipristino è il nome della cartella temporanea del ripristino, accanto alla destinazione.
const prefissoRipristino = ".ripristino-"

// Gli errori della cartella dei dati nuova.
var (
	ErrCartellaNonVuota   = errors.New("ripristino: la cartella dei dati esiste e non è vuota; il ripristino non scrive mai sopra dati esistenti")
	ErrCartellaRipristino = errors.New("ripristino: cartella dei dati non valida")
)

// ImpostazioniSpenteDalRipristino sono le impostazioni che il ripristino toglie e mette da parte nello stato
// isolato (risposta I): gli interruttori di email e Telegram (D-03) e la configurazione della copia remota (nota
// 13(a): la copia di prova non deve mai caricare né ruotare archivi sulla Storage Box vera). La 131 le rimette.
var ImpostazioniSpenteDalRipristino = []string{uscita.ChiaveEmailAcceso, uscita.ChiaveTelegramAcceso, ChiaveRemoto}

// ImpostazioniTolteDalRipristino sono quelle che si tolgono e basta: lo stato degli avvisi dei backup (127) parla
// dei problemi della macchina vecchia; l'ultimo cambio dell'ora del backup (133b) parla dei suoi backup, che il
// ripristino toglie dal registro (con il suo Dal la copia resterebbe senza backup fino all'ora nuova). L'ora resta.
var ImpostazioniTolteDalRipristino = []string{ChiaveAvvisi, ChiaveCambioOra}

// Ripristino è quello che il ripristino chiede: l'archivio, il contenuto del file della chiave dei backup (o la riga
// AGE-SECRET-KEY-… ricopiata dal foglio) e la cartella dei dati nuova (assoluta).
type Ripristino struct {
	Archivio string
	Chiave   []byte
	Cartella string
	Orologio clock.Orologio
}

// Ripristinato è il ripristino riuscito: l'archivio verificato, la cartella e il database nuovi, le migrazioni
// applicate a un backup di un programma più vecchio e lo stato isolato scritto.
type Ripristinato struct {
	Verificato
	Cartella, DB string
	Migrazioni   []int
	Isolamento   db.Isolamento
}

// Ripristina ripristina l'archivio nella cartella dei dati nuova, isolato. Nell'ordine:
//  1. la chiave (ChiaveDaTesto) e la cartella: assoluta, pulita, che non c'è (con la cartella sopra che c'è) o
//     una cartella vera (non un collegamento) vuota; altrimenti ErrCartellaRipristino o ErrCartellaNonVuota;
//  2. l'archivio, aperto senza seguire collegamenti, solo un file normale; nel volume della cartella sopra i byte
//     dell'archivio più MargineSpazio liberi (il tar decifrato non è mai più grande), altrimenti ErrSpazio;
//  3. la cartella temporanea .ripristino-XXXX accanto alla destinazione (0700, stesso volume), con media/ e
//     secrets/;
//  4. la verifica completa della 121 (verificaFile con tutto): ogni file si estrae lì, con fsync;
//  5. la chiave dei backup in secrets/chiave_backup (0600): la copia potrà fare i suoi backup;
//  6. il database: migrazioni del programma (un backup più nuovo del programma è un rifiuto), poi
//     db.IsolaRipristino; chiusura con il checkpoint del WAL;
//  7. fsync delle cartelle, poi la rinomina nella destinazione: rename(2) mette una cartella al posto di una
//     cartella solo se questa è vuota, quindi anche un file arrivato intanto ferma tutto (mai sopra dati); poi
//     fsync della cartella sopra.
//
// Con un errore o lo stop la cartella temporanea si toglie e la destinazione resta com'era (se il ripristino
// l'aveva creata vuota per la rinomina, la toglie).
func Ripristina(ctx context.Context, r Ripristino) (out Ripristinato, err error) {
	id, err := ChiaveDaTesto(r.Chiave)
	if err != nil {
		return Ripristinato{}, err
	}
	dest, err := controllaCartellaNuova(r.Cartella)
	if err != nil {
		return Ripristinato{}, err
	}
	padre := filepath.Dir(dest)
	f, err := apriSenzaCollegamenti(r.Archivio)
	if err != nil {
		return Ripristinato{}, fmt.Errorf("ripristino: archivio %s: %w", r.Archivio, err)
	}
	defer func() { _ = f.Close() }() // sola lettura
	fi, err := f.Stat()
	if err != nil {
		return Ripristinato{}, err
	}
	if !fi.Mode().IsRegular() {
		return Ripristinato{}, fmt.Errorf("ripristino: %s non è un file normale", r.Archivio)
	}
	libero, err := spazioLibero(padre)
	if err != nil {
		return Ripristinato{}, fmt.Errorf("ripristino: spazio libero di %s non leggibile: %w", padre, err)
	}
	if serve := uint64(fi.Size()) + MargineSpazio; libero < serve {
		return Ripristinato{}, fmt.Errorf("%w: per il ripristino servono %d byte (l'archivio e 10 GiB di margine), ci sono %d byte",
			ErrSpazio, serve, libero)
	}
	tmp, err := os.MkdirTemp(padre, prefissoRipristino) // 0700
	if err != nil {
		return Ripristinato{}, fmt.Errorf("ripristino: cartella temporanea: %w", err)
	}
	pubblicata := false
	defer func() {
		if pubblicata {
			return
		}
		if errRm := os.RemoveAll(tmp); errRm != nil {
			err = errors.Join(err, fmt.Errorf("ripristino: cartella temporanea %s da togliere a mano: %w", tmp, errRm))
		}
	}()
	for _, c := range []string{media.Cartella, CartellaSegreti} {
		if err := os.Mkdir(filepath.Join(tmp, c), 0o700); err != nil {
			return Ripristinato{}, fmt.Errorf("ripristino: %w", err)
		}
	}
	v, err := verificaFile(ctx, id, f, r.Archivio, padre, tmp, true)
	if err != nil {
		return Ripristinato{}, err
	}
	if err := scriviNuovo(filepath.Join(tmp, CartellaSegreti, NomeChiaveBackup), bytes.NewReader(r.Chiave), io.Discard, true); err != nil {
		return Ripristinato{}, fmt.Errorf("ripristino: chiave dei backup: %w", err)
	}
	migrazioni, iso, err := isola(ctx, filepath.Join(tmp, "gestrip.db"), r.Orologio, db.Isolamento{
		Archivio: filepath.Base(r.Archivio), SHA256: v.SHA256, CatturatoIl: v.CatturatoIl})
	if err != nil {
		return Ripristinato{}, err
	}
	for _, c := range []string{filepath.Join(tmp, media.Cartella), filepath.Join(tmp, CartellaSegreti), tmp} {
		if err := sincronizzaCartella(c); err != nil {
			return Ripristinato{}, fmt.Errorf("ripristino: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return Ripristinato{}, err
	}
	if err := pubblicaCartella(tmp, dest); err != nil {
		return Ripristinato{}, err
	}
	pubblicata = true
	if err := sincronizzaCartella(padre); err != nil {
		return Ripristinato{}, fmt.Errorf("ripristino: riuscito in %s, ma la cartella sopra non si sincronizza: %w", dest, err)
	}
	v.File = r.Archivio
	return Ripristinato{Verificato: v, Cartella: dest, DB: filepath.Join(dest, "gestrip.db"), Migrazioni: migrazioni, Isolamento: iso}, nil
}

// controllaCartellaNuova vuole un percorso assoluto e pulito che non c'è (con la cartella sopra che c'è) o una
// cartella vera vuota. Dà il percorso.
func controllaCartellaNuova(c string) (string, error) {
	if c == "" || !filepath.IsAbs(c) || filepath.Clean(c) != c {
		return "", fmt.Errorf("%w: serve un percorso assoluto e pulito, non %q", ErrCartellaRipristino, c)
	}
	fi, err := os.Lstat(c)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		su, err := os.Stat(filepath.Dir(c))
		if err != nil || !su.IsDir() {
			return "", fmt.Errorf("%w: la cartella %s che deve contenerla non c'è", ErrCartellaRipristino, filepath.Dir(c))
		}
		return c, nil
	case err != nil:
		return "", fmt.Errorf("%w: %w", ErrCartellaRipristino, err)
	case !fi.IsDir():
		return "", fmt.Errorf("%w: %s c'è e non è una cartella (anche un collegamento è rifiutato)", ErrCartellaRipristino, c)
	}
	voci, err := os.ReadDir(c)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrCartellaRipristino, err)
	}
	if len(voci) > 0 {
		return "", fmt.Errorf("%w (%s)", ErrCartellaNonVuota, c)
	}
	return c, nil
}

// pubblicaCartella mette tmp al posto di dest: se dest non c'è la crea vuota (Mkdir non sovrascrive mai), poi
// rename(2), che sostituisce una cartella solo se è vuota (ENOTEMPTY o EEXIST altrimenti). Se la rinomina
// fallisce, una dest creata qui si toglie (Remove toglie solo una cartella vuota).
func pubblicaCartella(tmp, dest string) error {
	creata := false
	if err := os.Mkdir(dest, 0o700); err == nil {
		creata = true
	} else if !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%w: %w", ErrCartellaRipristino, err)
	}
	if fi, err := os.Lstat(dest); err != nil || !fi.IsDir() {
		return fmt.Errorf("%w: %s non è più una cartella", ErrCartellaRipristino, dest)
	}
	// syscall.Rename e non os.Rename: os.Rename rifiuta sempre una destinazione che è una cartella (EEXIST),
	// anche vuota; rename(2) la sostituisce solo se è vuota.
	if err := syscall.Rename(tmp, dest); err != nil {
		if creata {
			_ = os.Remove(dest) // vuota, creata qui; un errore lascia una cartella vuota, innocua
		}
		if errors.Is(err, syscall.EEXIST) || errors.Is(err, syscall.ENOTEMPTY) {
			return fmt.Errorf("%w (%s): qualcosa ci è stato scritto durante il ripristino", ErrCartellaNonVuota, dest)
		}
		return fmt.Errorf("%w: la rinomina in %s non riesce (se è il punto di montaggio di un disco, scegli una cartella "+
			"nuova al suo interno): %w", ErrCartellaRipristino, dest, err)
	}
	return nil
}

// isola applica le migrazioni del programma al database estratto e lo mette nello stato isolato, poi lo chiude.
func isola(ctx context.Context, file string, o clock.Orologio, iso db.Isolamento) (_ []int, _ db.Isolamento, err error) {
	d, err := db.Apri(file)
	if err != nil {
		return nil, db.Isolamento{}, fmt.Errorf("ripristino: database: %w", err)
	}
	defer func() {
		if errC := d.Chiudi(); errC != nil {
			err = errors.Join(err, fmt.Errorf("ripristino: chiusura del database: %w", errC))
		}
	}()
	applicate, err := d.Migra(ctx, o)
	if err != nil {
		return nil, db.Isolamento{}, fmt.Errorf("ripristino: migrazioni del database: %w", err)
	}
	iso, err = db.IsolaRipristino(ctx, d, o, iso, ImpostazioniSpenteDalRipristino, ImpostazioniTolteDalRipristino)
	if err != nil {
		return nil, db.Isolamento{}, fmt.Errorf("ripristino: stato isolato: %w", err)
	}
	return applicate, iso, nil
}
