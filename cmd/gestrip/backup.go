package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/L0cutus/gestrip/internal/backup"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/media"
	"github.com/L0cutus/gestrip/internal/segreti"
)

// I comandi del backup a mano (slice 120, T5.2, D-113): chiave-backup crea la chiave dei backup e la
// mostra una volta sola; backup scrive un archivio cifrato in una cartella. Il backup gira anche con
// serve acceso, in un altro processo: la coda delle cancellazioni dei media di serve lo aspetta sul
// lucchetto su file delle cancellazioni (media.LucchettoCancellazioni), non solo sul RWMutex del
// processo.

// opzioniChiave è il database, accanto al quale sta secrets/.
type opzioniChiave struct{ DB string }

// opzioniBackup sono il database e la cartella dove scrivere l'archivio.
type opzioniBackup struct{ DB, Cartella string }

var (
	errUsoChiave = errors.New("uso: gestrip chiave-backup [-db FILE]")
	errUsoBackup = errors.New("uso: gestrip backup -cartella CARTELLA [-db FILE]")
)

func opzioniDiChiave(args []string) (opzioniChiave, error) {
	fs := flag.NewFlagSet("chiave-backup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := opzioniChiave{}
	fs.StringVar(&o.DB, "db", "/var/lib/gestrip/gestrip.db", "file del database")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return opzioniChiave{}, errUsoChiave
	}
	return o, nil
}

func opzioniDiBackup(args []string) (opzioniBackup, error) {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := opzioniBackup{}
	fs.StringVar(&o.DB, "db", "/var/lib/gestrip/gestrip.db", "file del database")
	fs.StringVar(&o.Cartella, "cartella", "", "cartella dove scrivere l'archivio cifrato")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || strings.TrimSpace(o.Cartella) == "" {
		return opzioniBackup{}, errUsoBackup
	}
	return o, nil
}

// databaseEsistente rifiuta un database che non c'è: db.Apri lo creerebbe vuoto, e secrets/ finirebbe
// accanto a un percorso sbagliato.
func databaseEsistente(p string) error {
	if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("database %s non trovato: lo crea «gestrip serve»", p)
	}
	return nil
}

// chiaveBackup crea la chiave dei backup e scrive su out, una volta sola, la chiave privata da copiare
// sul foglio di carta. Nessun altro comando la mostra più. Se la scrittura su out non riesce, la
// chiave appena creata si toglie: nessuno l'ha vista, nessun backup la usa ancora.
func chiaveBackup(o opzioniChiave, out io.Writer, orologio clock.Orologio) error {
	if err := segreti.ControllaAmbiente(os.LookupEnv); err != nil {
		return err
	}
	if err := databaseEsistente(o.DB); err != nil {
		return err
	}
	c, err := backup.CreaChiave(filepath.Dir(o.DB), orologio.Adesso())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Chiave dei backup creata in %s (permessi 0600).\n\n"+
		"Chiave pubblica (cifra i backup; non è segreta):\n  %s\n\n"+
		"CHIAVE PRIVATA, scrivila adesso sul foglio di carta: non verrà mostrata mai più.\n"+
		"Senza questa chiave i backup non si possono aprire.\n  %s\n\n"+
		"Controlla di averla copiata giusta, poi chiudi o pulisci il terminale.\n",
		c.File, c.Pubblica, c.Privata)
	if err != nil {
		// Tolta e con la cartella sincronizzata: dopo un blackout non deve ricomparire una chiave mai vista.
		if errRm := errors.Join(os.Remove(c.File), sincronizzaCartella(filepath.Dir(c.File))); errRm != nil {
			return fmt.Errorf("la chiave non si è potuta mostrare (%w) e nemmeno togliere: toglila a mano da %s e rifai il comando", err, c.File)
		}
		return fmt.Errorf("la chiave non si è potuta mostrare (%w): l'ho tolta, rifai il comando su un terminale", err)
	}
	return nil
}

// faiBackup scrive un archivio cifrato nella cartella (backup.Cifra) e dice su out il nome, i byte e lo
// SHA-256. Non cancella mai archivi (la rotazione è della 123) e non dice «riuscito»: la verifica
// arriva con la 121.
func faiBackup(ctx context.Context, o opzioniBackup, out io.Writer) (err error) {
	if err := segreti.ControllaAmbiente(os.LookupEnv); err != nil {
		return err
	}
	if err := databaseEsistente(o.DB); err != nil {
		return err
	}
	d, err := db.Apri(o.DB)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, d.Chiudi()) }()
	s := backup.Sorgente{DB: d, Archivio: media.Nuovo(media.CartellaDi(o.DB)), Orologio: clock.Sistema{}}
	r, err := backup.Cifra(ctx, s, o.Cartella)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "archivio scritto: %s\nbyte: %d\nsha256: %s\nmedia: %d\nnon ancora verificato\n",
		r.File, r.ByteCifrati, r.SHA256, contaMedia(r.Inventario))
	return err
}

func contaMedia(inv map[string]string) int {
	n := 0
	for k := range inv {
		if strings.HasPrefix(k, "media/") {
			n++
		}
	}
	return n
}

// sincronizzaCartella fa fsync della cartella dir.
func sincronizzaCartella(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}
