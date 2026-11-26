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
	"github.com/L0cutus/gestrip/internal/segreti"
)

// I comandi della copia remota dei backup (slice 128, T5.2, OPS-16): chiave-sftp crea la chiave ed25519
// dell'apparecchio (o rilegge quella che c'è) e stampa la parte pubblica da incollare nel pannello del server;
// backup-remoto fissa la configurazione (host, porta, utente, cartella e impronta del server) o la toglie. La pagina
// con gli stessi campi è della 129.
//
// L'impronta del server non si accetta mai in silenzio: senza -impronta il comando si collega solo fino a vedere la
// chiave del server, la mostra e non scrive niente; con -impronta scrive la configurazione solo se il server si
// presenta proprio con quella (chi configura la confronta con quella pubblicata dal fornitore, per la Storage Box
// nella documentazione di Hetzner), se la chiave dell'apparecchio entra e se la cartella c'è.

type opzioniChiaveSFTP struct{ DB string }

type opzioniBackupRemoto struct {
	DB, Host, Utente, Cartella, Impronta string
	Porta                                int
	Togli                                bool
}

var (
	errUsoChiaveSFTP    = errors.New("uso: gestrip chiave-sftp [-db FILE]")
	errUsoBackupRemoto  = errors.New("uso: gestrip backup-remoto -host HOST [-porta N] -utente UTENTE -cartella CARTELLA [-impronta SHA256:…] [-db FILE] | gestrip backup-remoto -togli [-db FILE]")
	errImprontaDaVedere = errors.New("impronta del server non confermata: niente è stato scritto")
)

// componiRemoto è la connessione dei comandi; nil: TCP. Una variabile per i test (server SFTP finto).
var componiRemoto func(*backup.Remoto)

func opzioniDiChiaveSFTP(args []string) (opzioniChiaveSFTP, error) {
	fs := flag.NewFlagSet("chiave-sftp", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := opzioniChiaveSFTP{}
	fs.StringVar(&o.DB, "db", "/var/lib/gestrip/gestrip.db", "file del database")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return opzioniChiaveSFTP{}, errUsoChiaveSFTP
	}
	return o, nil
}

func opzioniDiBackupRemoto(args []string) (opzioniBackupRemoto, error) {
	fs := flag.NewFlagSet("backup-remoto", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := opzioniBackupRemoto{}
	fs.StringVar(&o.DB, "db", "/var/lib/gestrip/gestrip.db", "file del database")
	fs.StringVar(&o.Host, "host", "", "server SFTP")
	fs.IntVar(&o.Porta, "porta", backup.PortaRemotaPredefinita, "porta SSH del server")
	fs.StringVar(&o.Utente, "utente", "", "utente (il sottoaccount della Storage Box)")
	fs.StringVar(&o.Cartella, "cartella", "", "cartella dei backup sul server")
	fs.StringVar(&o.Impronta, "impronta", "", "impronta della chiave del server (SHA256:…), confrontata con quella del fornitore")
	fs.BoolVar(&o.Togli, "togli", false, "spegne la copia remota (gli archivi sul server restano)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return opzioniBackupRemoto{}, errUsoBackupRemoto
	}
	if o.Togli {
		if o.Host != "" || o.Utente != "" || o.Cartella != "" || o.Impronta != "" {
			return opzioniBackupRemoto{}, errUsoBackupRemoto
		}
		return o, nil
	}
	if strings.TrimSpace(o.Host) == "" || strings.TrimSpace(o.Utente) == "" || strings.TrimSpace(o.Cartella) == "" {
		return opzioniBackupRemoto{}, errUsoBackupRemoto
	}
	return o, nil
}

// chiaveSFTP crea la chiave dell'apparecchio per la copia remota (o rilegge quella che c'è) e ne stampa la parte
// pubblica: non è segreta, si può mostrare ovunque. Prende il lucchetto dei backup: un backup in corso non deve
// trovare il file temporaneo della chiave in secrets/.
func chiaveSFTP(o opzioniChiaveSFTP, out io.Writer) (err error) {
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
	pub, creata, err := backup.CreaChiaveSFTP(filepath.Dir(o.DB))
	if err != nil {
		return err
	}
	cosa := "Chiave dell'apparecchio per la copia remota già presente (non l'ho cambiata)."
	if creata {
		cosa = "Chiave dell'apparecchio per la copia remota creata in " + backup.FileChiaveSFTP(filepath.Dir(o.DB)) + " (permessi 0600)."
	}
	_, err = fmt.Fprintf(out, "%s\n\nChiave pubblica (non è segreta): incollala nel pannello della Storage Box, nelle chiavi SSH del sottoaccount dei backup:\n  %s\n",
		cosa, pub)
	return err
}

// backupRemoto fissa o toglie la configurazione della copia remota.
func backupRemoto(ctx context.Context, o opzioniBackupRemoto, out io.Writer, orologio clock.Orologio) (err error) {
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
	if o.Togli {
		if err := backup.TogliConfigRemota(ctx, d); err != nil {
			return err
		}
		_, err = io.WriteString(out, "Copia remota spenta. Gli archivi già sul server restano: toglili dal pannello se non servono più.\n")
		return err
	}
	c := backup.ConfigRemota{Host: strings.TrimSpace(o.Host), Porta: o.Porta, Utente: strings.TrimSpace(o.Utente),
		Cartella: strings.TrimSpace(o.Cartella)}
	r := &backup.Remoto{Config: c, Dati: filepath.Dir(o.DB)}
	if componiRemoto != nil {
		componiRemoto(r)
	}
	tipo, impronta, err := r.ChiaveDelServer(ctx)
	if err != nil {
		return err
	}
	voluta := strings.TrimSpace(o.Impronta)
	if voluta == "" {
		_, err = fmt.Fprintf(out, "Il server si presenta con questa chiave:\n  tipo: %s\n  impronta: %s\n\n"+
			"Confrontala con l'impronta pubblicata dal fornitore (per la Storage Box: la documentazione di Hetzner). Se è "+
			"uguale, rilancia lo stesso comando con -impronta %s\n", tipo, impronta, impronta)
		return errors.Join(errImprontaDaVedere, err)
	}
	if !backup.ControllaImpronta(voluta) {
		return errors.New("l'impronta va scritta come la dà ssh-keygen -l: SHA256: e 43 caratteri")
	}
	if voluta != impronta {
		_, err = fmt.Fprintf(out, "IMPRONTA DIVERSA: il server si presenta con %s (%s), non con quella scritta. Niente è stato scritto.\n",
			impronta, tipo)
		return errors.Join(backup.ErrImprontaServer, err)
	}
	r.Config.TipoChiaveServer, r.Config.Impronta = tipo, voluta
	// La cartella si può scrivere relativa (alla cartella del sottoaccount): la prova la risolve sul server
	// (RealPath) e ScriviConfigRemota controlla quella vera (assoluta, non la radice). Il resto si controlla prima.
	prima := r.Config
	prima.Cartella = "/cartella-da-risolvere"
	if err := prima.Controlla(); err != nil {
		return err
	}
	vera, err := r.Prova(ctx)
	if err != nil {
		return err
	}
	r.Config.Cartella = vera
	if err := backup.ScriviConfigRemota(ctx, d, r.Config, orologio.Adesso()); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Copia remota configurata: impronta del server %s fissata, chiave dell'apparecchio accettata, "+
		"cartella %s trovata.\nDa stanotte, dopo il backup sulla chiavetta, l'archivio va anche sul server.\n", voluta, vera)
	return err
}
