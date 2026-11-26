package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/L0cutus/gestrip/internal/backup"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/segreti"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// gestrip restore (slice 130, OPS-18, OPS-19, D-12, risposta I): ripristina un archivio del backup in una cartella
// dei dati nuova, con la verifica completa della 121, e la lascia isolata (backup.Ripristina). Si lancia come
// l'utente di serve, come gli altri comandi del backup: i file nascono suoi, 0600.

// opzioniRipristino sono l'archivio, il file della chiave dei backup (vuoto: la chiave si incolla sullo standard
// input) e la cartella dei dati nuova.
type opzioniRipristino struct{ Archivio, Chiave, Cartella string }

var errUsoRipristino = errors.New("uso: gestrip restore -archivio FILE -cartella CARTELLA-NUOVA [-chiave FILE]")

// leggiChiaveIncollata chiede su richiesta di incollare la chiave dei backup e ne legge una riga da in (al più 4 KiB).
// Se in è un terminale l'eco si toglie mentre si legge e si rimette sempre, anche con un errore o lo stop (Ctrl-C:
// il contesto finisce e la lettura, in una goroutine, si abbandona).
func leggiChiaveIncollata(ctx context.Context, in io.Reader, richiesta io.Writer) ([]byte, error) {
	if in == nil {
		in = strings.NewReader("")
	}
	if f, ok := in.(*os.File); ok {
		rimetti, err := senzaEco(f)
		if err != nil {
			return nil, fmt.Errorf("chiave dei backup: terminale: %w", err)
		}
		defer rimetti()
	}
	_, _ = fmt.Fprint(richiesta, "Incolla la chiave dei backup (la riga AGE-SECRET-KEY-… del foglio) e premi Invio: ")
	type letta struct {
		riga string
		err  error
	}
	fatto := make(chan letta, 1)
	go func() {
		riga, err := bufio.NewReaderSize(io.LimitReader(in, 4096), 4096).ReadString('\n')
		fatto <- letta{riga, err}
	}()
	select {
	case <-ctx.Done():
		_, _ = fmt.Fprintln(richiesta)
		return nil, ctx.Err()
	case l := <-fatto:
		_, _ = fmt.Fprintln(richiesta) // a capo dopo la riga incollata
		if l.err != nil && !errors.Is(l.err, io.EOF) {
			return nil, fmt.Errorf("chiave dei backup: %w", l.err)
		}
		return []byte(strings.TrimSpace(l.riga) + "\n"), nil
	}
}

// orologioRipristino è l'orologio del ripristino (ora dello stato isolato e della chiave del server); una
// variabile per i test.
var orologioRipristino clock.Orologio = clock.Sistema{}

func opzioniDiRipristino(args []string) (opzioniRipristino, error) {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := opzioniRipristino{}
	fs.StringVar(&o.Archivio, "archivio", "", "l'archivio del backup (gestrip-backup-….tar.age)")
	fs.StringVar(&o.Chiave, "chiave", "", "il file della chiave dei backup (vuoto: la si incolla quando il comando la chiede)")
	fs.StringVar(&o.Cartella, "cartella", "", "la cartella dei dati nuova: non deve esserci, o deve essere vuota")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 || o.Archivio == "" || o.Cartella == "" {
		return opzioniRipristino{}, errUsoRipristino
	}
	return o, nil
}

// ripristina è gestrip restore: legge la chiave (dal file di -chiave, con i controlli di LeggiChiave, o una riga
// dallo standard input in, dopo la richiesta su richiesta), ripristina e dice su out cosa ha verificato, dove sono i
// dati e che la copia è isolata. Un errore comincia con «RIPRISTINO NON RIUSCITO» e dice se la cartella dei dati
// non è stata creata. La chiave non si stampa mai.
func ripristina(ctx context.Context, o opzioniRipristino, out io.Writer, in io.Reader, richiesta io.Writer) (err error) {
	if err := segreti.ControllaAmbiente(os.LookupEnv); err != nil {
		return err
	}
	cartella, err := filepath.Abs(o.Cartella)
	if err != nil {
		return err
	}
	defer func() {
		if err == nil {
			return
		}
		err = fmt.Errorf("RIPRISTINO NON RIUSCITO: %w", err)
		if _, errL := os.Lstat(cartella); errors.Is(errL, fs.ErrNotExist) {
			err = fmt.Errorf("%w (la cartella dei dati %s non è stata creata)", err, cartella)
		}
	}()
	var chiave []byte
	if o.Chiave != "" {
		if chiave, err = backup.LeggiFileChiave(o.Chiave); err != nil {
			if errors.Is(err, backup.ErrChiavePermessi) {
				// Una chiavetta FAT o exFAT dà a tutti i file permessi larghi e un altro utente.
				err = fmt.Errorf("%w: copia il file in una cartella tua (non sulla chiavetta) e dagli i permessi 0600 "+
					"(chmod 600 FILE), poi rifai il comando con -chiave su quella copia", err)
			}
			return err
		}
	} else if chiave, err = leggiChiaveIncollata(ctx, in, richiesta); err != nil {
		return err
	}
	// Cartelle .ripristino-* rimaste da un ripristino interrotto (blackout, kill): si dicono, non si toccano.
	if resti, _ := filepath.Glob(filepath.Join(filepath.Dir(cartella), ".ripristino-*")); len(resti) > 0 {
		for _, resto := range resti {
			_, _ = fmt.Fprintf(richiesta, "ATTENZIONE: %s è il resto di un ripristino interrotto: se nessun ripristino è in corso, "+
				"toglilo a mano (rm -r)\n", resto)
		}
	}
	r, err := backup.Ripristina(ctx, backup.Ripristino{Archivio: o.Archivio, Chiave: chiave, Cartella: cartella, Orologio: orologioRipristino})
	if err != nil {
		return err
	}
	var spenti []string
	if r.Isolamento.Spento[uscita.ChiaveEmailAcceso] == "1" {
		spenti = append(spenti, "email")
	}
	if r.Isolamento.Spento[uscita.ChiaveTelegramAcceso] == "1" {
		spenti = append(spenti, "Telegram")
	}
	if _, ok := r.Isolamento.Spento[backup.ChiaveRemoto]; ok {
		spenti = append(spenti, "copia remota")
	}
	elenco := strings.Join(spenti, ", ")
	if elenco == "" {
		elenco = "niente (nel backup email, Telegram e copia remota erano già spenti)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "archivio: %s\nbyte: %d\nsha256: %s\ncatturato il: %s\nversione: %s\nmedia: %d\n"+
		"verificato: decifrato, %d file con l'impronta giusta, database integro, %d tabelle con i conteggi giusti\n",
		r.File, r.ByteCifrati, r.SHA256, r.CatturatoIl, r.Versione, contaMedia(r.Inventario), len(r.Inventario), len(r.Conteggi))
	if len(r.MediaRovinati) > 0 {
		fmt.Fprintf(&b, "ATTENZIONE: %d media con il contenuto diverso dal nome (erano già rovinati quando il backup è stato fatto)\n",
			len(r.MediaRovinati))
	}
	if len(r.Migrazioni) > 0 {
		fmt.Fprintf(&b, "migrazioni applicate (il backup era di un programma più vecchio): %v\n", r.Migrazioni)
	}
	fmt.Fprintf(&b, "ripristinato in %s (database %s)\n", r.Cartella, r.DB)
	b.WriteString("COPIA ISOLATA: email, Telegram, lavori automatici, backup automatici e copia remota restano spenti e le pagine " +
		"dei clienti rispondono «in manutenzione», anche dopo un riavvio, finché non confermi che questa è ora la macchina vera\n")
	fmt.Fprintf(&b, "spenti dal ripristino, da riaccendere alla conferma: %s\n", elenco)
	b.WriteString("registro dei backup svuotato: dopo la conferma serve un backup nuovo su questa macchina\n")
	fmt.Fprintf(&b, "avvio: gestrip serve -db %s\n", r.DB)
	_, err = io.WriteString(out, b.String())
	return err
}
