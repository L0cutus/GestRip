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

// I comandi del backup a mano (slice 120-123, T5.2, D-113, D-114): chiave-backup crea la chiave dei backup
// e la mostra una volta sola; backup scrive un archivio cifrato in una cartella o sulla chiavetta (123: con
// controlli del montaggio, pulizia dei .pending e rotazione 7 + 4) e lo verifica per
// intero prima di dirlo riuscito e lo scrive nel registro dei backup verificati (slice 122, D-11);
// verifica-backup verifica un archivio già scritto e non scrive nel registro (un archivio qualsiasi, magari
// di un altro database, non deve sbloccare le cancellazioni di questo). Il backup gira anche con
// serve acceso, in un altro processo: la coda delle cancellazioni dei media di serve lo aspetta sul
// lucchetto su file delle cancellazioni (media.LucchettoCancellazioni), non solo sul RWMutex del
// processo. Dalla slice 124 backup e verifica-backup prendono il lucchetto dei backup
// (backup.PrendiLucchetto), lo stesso del backup notturno di serve: mai due insieme, nemmeno fra processi.

// opzioniChiave è il database, accanto al quale sta secrets/.
type opzioniChiave struct{ DB string }

// opzioniBackup sono il database e dove scrivere l'archivio: una cartella qualsiasi (-cartella) o la
// chiavetta (-chiavetta, slice 123), una sola delle due.
type opzioniBackup struct{ DB, Cartella, Chiavetta string }

// opzioniVerificaBackup sono il database (accanto stanno la chiave dei backup e il volume dove estrarre) e
// l'archivio da verificare.
type opzioniVerificaBackup struct{ DB, Archivio string }

var (
	errUsoChiave         = errors.New("uso: gestrip chiave-backup [-db FILE]")
	errUsoBackup         = errors.New("uso: gestrip backup -cartella CARTELLA|-chiavetta CARTELLA [-db FILE]")
	errUsoVerificaBackup = errors.New("uso: gestrip verifica-backup [-db FILE] ARCHIVIO")
)

// verificaArchivio è backup.VerificaArchivio; una variabile per i test del comando.
var verificaArchivio = backup.VerificaArchivio

// orologioRegistro dà l'ora della verifica scritta nel registro dei backup; una variabile per i test.
var orologioRegistro clock.Orologio = clock.Sistema{}

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
	fs.StringVar(&o.Chiavetta, "chiavetta", "", "punto di montaggio della chiavetta: archivio, verifica, registro e rotazione 7 + 4")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 ||
		(strings.TrimSpace(o.Cartella) == "") == (strings.TrimSpace(o.Chiavetta) == "") {
		return opzioniBackup{}, errUsoBackup
	}
	return o, nil
}

func opzioniDiVerificaBackup(args []string) (opzioniVerificaBackup, error) {
	fs := flag.NewFlagSet("verifica-backup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := opzioniVerificaBackup{}
	fs.StringVar(&o.DB, "db", "/var/lib/gestrip/gestrip.db", "file del database")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		return opzioniVerificaBackup{}, errUsoVerificaBackup
	}
	o.Archivio = fs.Arg(0)
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

// faiBackup scrive un archivio cifrato nella destinazione (backup.Cifra), lo verifica per intero
// (backup.VerificaArchivio, D-113), lo scrive nel registro dei backup verificati (slice 122) e solo allora
// dice «backup riuscito». Se la verifica non passa lo dice chiaro, registra la verifica fallita, lascia
// l'archivio dov'è e dà un errore: il comando esce con 1. Anche un registro che non si scrive è «non
// riuscito»: l'archivio non conterebbe.
//
// Con -chiavetta (slice 123) prima controlla la chiavetta (backup.ApriChiavetta: punto di montaggio, un
// dispositivo diverso dai dati, scrivibile) e toglie i .pending rimasti; registra «chiavetta» solo se alla
// fine la chiavetta è ancora quella (ConfermaChiavetta), altrimenti «cartella», che non sblocca niente; dopo
// la riga 'ok' ruota gli archivi (7 + 4, D-114). Con -cartella non pulisce e non ruota niente. In tutti e
// due i casi tiene il lucchetto della cartella per tutto il giro: un secondo backup nella stessa cartella
// rifiuta.
//
// Dalla slice 124 la sequenza è backup.Giro, la stessa del backup notturno di serve, e prima di tutto il
// comando prende il lucchetto dei backup della cartella dei dati (backup.PrendiLucchetto): con un altro
// backup o una verifica in corso, anche dentro serve, rifiuta subito (backup.ErrBackupInCorso, nota 6b). Con
// il lucchetto preso toglie le cartelle temporanee rimaste da un backup interrotto (nota 7(e)).
func faiBackup(ctx context.Context, o opzioniBackup, out io.Writer) (err error) {
	if err := segreti.ControllaAmbiente(os.LookupEnv); err != nil {
		return err
	}
	if err := databaseEsistente(o.DB); err != nil {
		return err
	}
	l, err := backup.PrendiLucchetto(filepath.Dir(o.DB))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, l.Rilascia()) }()
	d, err := db.Apri(o.DB)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, d.Chiudi()) }()
	g := backup.Giro{Lucchetto: l, DB: d, Archivio: media.Nuovo(media.CartellaDi(o.DB)), Chiavetta: o.Chiavetta,
		Cartella: o.Cartella, Registro: orologioRegistro, Verifica: verificaArchivio,
		Dire: func(riga string) error { _, err := io.WriteString(out, riga); return err }}
	_, err = g.Esegui(ctx)
	return err
}

// verificaBackup verifica per intero un archivio già scritto (backup.VerificaArchivio) con la chiave
// dei backup accanto al database, e dice su out cosa contiene. Non apre il database dei dati: estrae
// quello dell'archivio in una cartella temporanea del volume dei dati, tolta alla fine.
func verificaBackup(ctx context.Context, o opzioniVerificaBackup, out io.Writer) (err error) {
	if err := segreti.ControllaAmbiente(os.LookupEnv); err != nil {
		return err
	}
	if err := databaseEsistente(o.DB); err != nil {
		return err
	}
	// Mai insieme a un backup (slice 124): la verifica estrae il database nel volume dei dati e ne conta lo
	// spazio, come il backup; e la pulizia delle cartelle temporanee non deve togliere la sua.
	l, err := backup.PrendiLucchetto(filepath.Dir(o.DB))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, l.Rilascia()) }()
	v, err := verificaArchivio(ctx, filepath.Dir(o.DB), o.Archivio)
	if err != nil {
		return fmt.Errorf("%s NON verificato: %w", o.Archivio, err)
	}
	_, err = fmt.Fprintf(out, "archivio: %s\nbyte: %d\nsha256: %s\ncatturato il: %s\nversione: %s\nmedia: %d\n"+
		"verificato: decifrato, %d file con l'impronta giusta, database integro, %d tabelle con i conteggi giusti\n",
		v.File, v.ByteCifrati, v.SHA256, v.CatturatoIl, v.Versione, contaMedia(v.Inventario), len(v.Inventario), len(v.Conteggi))
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
