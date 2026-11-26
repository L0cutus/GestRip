package backup

// Il lucchetto dei backup e la pulizia delle cartelle temporanee rimaste (slice 124, T5.2, note 6b e 7(e) di
// docs/domande-aperte.md). Mai due backup insieme, nemmeno fra processi diversi: il lavoro notturno e
// «Esegui adesso» dentro gestrip serve, il comando gestrip backup e gestrip verifica-backup prendono tutti lo
// stesso flock esclusivo su un file della cartella dei dati, senza aspettare. Così non si sovrappongono
// nemmeno i controlli dello spazio (10 GiB nel volume dei dati per la copia del database e per l'estrazione
// della verifica): ognuno conta lo spazio da solo.
//
// Ordine dei lucchetti, dall'esterno all'interno (ognuno si prende solo dopo quelli prima, mai al
// contrario): 1. questo (LucchettoBackup, senza aspettare); 2. il lucchetto della destinazione (flock della
// cartella, slice 123, senza aspettare); 3. dentro Scrivi il lucchetto delle cancellazioni dei media
// (media.LucchettoCancellazioni, condiviso, con l'attesa interrompibile). La pulizia dei media prende solo il
// 3 e non prende mai gli altri due, quindi nessun giro di attese.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// LucchettoBackup è il file, nella cartella dei dati, del flock dei backup. Il file resta: conta solo il
// flock, che il kernel toglie da solo se il processo muore. Non è un dato: non entra nell'archivio.
const LucchettoBackup = ".lucchetto-backup"

// ErrBackupInCorso: un altro backup (o una verifica) tiene il lucchetto, in questo processo o in un altro.
var ErrBackupInCorso = errors.New("backup: un altro backup o una verifica è già in corso, riprova quando ha finito")

// reResto sono i soli nomi che la pulizia toglie: le cartelle temporanee di Scrivi (.backup-), di
// VerificaArchivio (.verifica-backup-) e del download della prova remota (.scarica-backup-, 129c), con il suffisso
// numerico di os.MkdirTemp (un uint32 in decimale).
var reResto = regexp.MustCompile(`\A(?:` + regexp.QuoteMeta(prefissoTemporanea) + `|` + regexp.QuoteMeta(prefissoVerifica) + `|` +
	regexp.QuoteMeta(prefissoScaricamento) + `)[0-9]{1,10}\z`)

// Lucchetto è il lucchetto dei backup preso: finché non si rilascia nessun altro backup o verifica parte,
// e solo chi lo tiene può togliere le cartelle temporanee rimaste.
type Lucchetto struct {
	dati string
	f    *os.File
}

// PrendiLucchetto prende il lucchetto dei backup della cartella dei dati dati, senza aspettare: se un altro
// backup lo tiene, ErrBackupInCorso.
func PrendiLucchetto(dati string) (*Lucchetto, error) {
	assoluta, err := filepath.Abs(dati)
	if err != nil {
		return nil, fmt.Errorf("backup: lucchetto dei backup: %w", err)
	}
	f, err := bloccaFile(filepath.Join(assoluta, LucchettoBackup))
	if err != nil {
		return nil, err
	}
	return &Lucchetto{dati: assoluta, f: f}, nil
}

// diDati dice se il lucchetto è quello della cartella dei dati dati.
func (l *Lucchetto) diDati(dati string) bool {
	assoluta, err := filepath.Abs(dati)
	return err == nil && assoluta == l.dati
}

// Preso dice se il lucchetto è ancora tenuto (preso e non rilasciato).
func (l *Lucchetto) Preso() bool { return l != nil && l.f != nil }

// Rilascia lascia il lucchetto (chiudere il file toglie il flock). Una seconda volta non fa niente.
func (l *Lucchetto) Rilascia() error {
	if !l.Preso() {
		return nil
	}
	f := l.f
	l.f = nil
	return f.Close()
}

// PulisciResti toglie dalla cartella dei dati le cartelle temporanee rimaste da un backup o da una verifica
// interrotti (blackout, processo ucciso: il defer di Scrivi e di VerificaArchivio non è arrivato). Solo con
// il lucchetto preso (nessun backup e nessuna verifica del Go stanno lavorando: chi lavora lo tiene), solo
// i nomi esatti di reResto, solo cartelle vere: un collegamento con quel nome resta, e dentro le cartelle i
// collegamenti si tolgono senza seguirli (os.Root). Dà i nomi tolti, in ordine; al primo errore si ferma.
func (l *Lucchetto) PulisciResti() ([]string, error) {
	if !l.Preso() {
		return nil, errors.New("backup: pulizia delle cartelle temporanee senza il lucchetto dei backup")
	}
	r, err := os.OpenRoot(l.dati)
	if err != nil {
		return nil, fmt.Errorf("backup: pulizia delle cartelle temporanee: %w", err)
	}
	defer func() { _ = r.Close() }() // solo lettura della cartella
	cartella, err := r.Open(".")
	if err != nil {
		return nil, fmt.Errorf("backup: pulizia delle cartelle temporanee: %w", err)
	}
	nomi, err := cartella.Readdirnames(-1)
	_ = cartella.Close() // solo lettura
	if err != nil {
		return nil, fmt.Errorf("backup: pulizia delle cartelle temporanee: %w", err)
	}
	sort.Strings(nomi)
	var tolti []string
	for _, n := range nomi {
		if !reResto.MatchString(n) {
			continue
		}
		fi, err := r.Lstat(n)
		if err != nil {
			return tolti, fmt.Errorf("backup: pulizia delle cartelle temporanee: %w", err)
		}
		if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if err := togliResto(r, n); err != nil {
			return tolti, fmt.Errorf("backup: pulizia delle cartelle temporanee, %s: %w", n, err)
		}
		tolti = append(tolti, n)
	}
	return tolti, nil
}

// togliResto toglie la cartella n dentro r senza seguire collegamenti; una variabile per i test.
var togliResto = func(r *os.Root, n string) error { return r.RemoveAll(n) }
