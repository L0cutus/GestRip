package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/segreti"
)

// esporta-dettati (slice 117; OPS-09) è mix gestrip.export_dettati dello standalone a go-baseline: i dettati raccolti,
// uniti alle buste come sono adesso e con i pseudonimi, in un file nuovo (buste.EsportaDettati); poi le righe lette si
// cancellano. Stampa solo numeri (TrainingExport.report/1), mai un dettato, un campo o un esempio, e nemmeno i suoi
// errori: guardare un esempio è compito del titolare, sulla macchina che tiene il file.

// opzioniEsporta sono il database e il file da scrivere.
type opzioniEsporta struct{ DB, File string }

// errUsoEsporta è un uso sbagliato del comando (uscita 2).
var errUsoEsporta = errors.New("uso: gestrip esporta-dettati [-db FILE] FILE.jsonl")

// opzioniDiEsporta legge gli argomenti: -db e un solo percorso, non vuoto.
func opzioniDiEsporta(args []string) (opzioniEsporta, error) {
	fs := flag.NewFlagSet("esporta-dettati", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := opzioniEsporta{}
	fs.StringVar(&o.DB, "db", "/var/lib/gestrip/gestrip.db", "file del database")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		return opzioniEsporta{}, errUsoEsporta
	}
	o.File = fs.Arg(0)
	return o, nil
}

// esportaDettati esegue l'esportazione e stampa su out il resoconto, una riga nome=numero per volta.
func esportaDettati(ctx context.Context, o opzioniEsporta, out io.Writer) (err error) {
	// Come serve: un segreto come valore nell'ambiente ferma ogni comando (OPS-13).
	if err := segreti.ControllaAmbiente(os.LookupEnv); err != nil {
		return err
	}
	// db.Apri crea il file se manca: un percorso sbagliato lascerebbe un database vuoto. Diversamente dagli altri comandi,
	// i messaggi non nominano il percorso (né del database né del file): su stderr non va un nome che potrebbe essere
	// quello di un cliente.
	if fi, err := os.Stat(o.DB); err != nil || !fi.Mode().IsRegular() {
		return errors.New("database non trovato: lo crea «gestrip serve»")
	}
	d, err := db.Apri(o.DB)
	if err != nil {
		var pe *fs.PathError
		if errors.As(err, &pe) {
			return fmt.Errorf("apertura del database: %w", pe.Err)
		}
		return err
	}
	defer func() { err = errors.Join(err, d.Chiudi()) }()
	c, err := buste.Nuove(d, clock.Sistema{}).EsportaDettati(ctx, o.File)
	if err != nil {
		return err
	}
	for _, riga := range c.Resoconto() {
		if _, err := fmt.Fprintln(out, riga); err != nil {
			return err
		}
	}
	return nil
}

// messaggioEsportazione è il testo di un errore dell'esportazione, come i Mix.raise dello standalone; per ErrFileTenuto
// (D-112) un testo suo, di soli numeri.
func messaggioEsportazione(err error) string {
	if errors.Is(err, buste.ErrFileEsistente) {
		return "Il file esiste già: l'esportazione non sovrascrive."
	}
	var tenuto *buste.ErrFileTenuto
	if errors.As(err, &tenuto) {
		return "Esportazione non completata: " + tenuto.Error() + "."
	}
	return "Esportazione non riuscita: " + err.Error() + "."
}
