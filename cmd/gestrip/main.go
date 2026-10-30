// Comando gestrip: il binario unico dell'apparecchio.
//
//	gestrip serve [-db FILE] [-interna IND] [-pubblica IND]
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/web"
)

func main() {
	if len(os.Args) < 2 {
		uso()
	}
	switch os.Args[1] {
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		o := opzioniServe{}
		fs.StringVar(&o.DB, "db", "/var/lib/gestrip/gestrip.db", "file del database")
		fs.StringVar(&o.Interna, "interna", "127.0.0.1:47101", "indirizzo della faccia interna (dietro tailscale serve)")
		fs.StringVar(&o.Pubblica, "pubblica", "127.0.0.1:47102", "indirizzo della faccia pubblica (dietro tailscale funnel)")
		_ = fs.Parse(os.Args[2:]) // ExitOnError: con un errore esce da sola
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := serve(ctx, o, nil); err != nil {
			fmt.Fprintln(os.Stderr, "gestrip:", err)
			os.Exit(1)
		}
	default:
		uso()
	}
}

func uso() {
	fmt.Fprintln(os.Stderr, "uso: gestrip serve [-db FILE] [-interna IND] [-pubblica IND]")
	os.Exit(2)
}

type opzioniServe struct {
	DB, Interna, Pubblica string
}

// serve apre il database, lo migra e apre le due facce, e resta in piedi fino alla fine di ctx.
// pronto, se c'è, riceve gli indirizzi veri (utile con la porta 0 nei test).
func serve(ctx context.Context, o opzioniServe, pronto func(interna, pubblica net.Addr)) (err error) {
	d, err := db.Apri(o.DB)
	if err != nil {
		return err
	}
	// La chiusura dello scrittore fa il checkpoint del WAL: un suo errore va riportato.
	defer func() { err = errors.Join(err, d.Chiudi()) }()
	applicate, err := d.Migra(ctx, clock.Sistema{})
	if err != nil {
		return err
	}
	if len(applicate) > 0 {
		fmt.Fprintf(os.Stderr, "gestrip: migrazioni applicate: %v\n", applicate)
	}
	li, err := net.Listen("tcp", o.Interna)
	if err != nil {
		return fmt.Errorf("faccia interna: %w", err)
	}
	lp, err := net.Listen("tcp", o.Pubblica)
	if err != nil {
		return errors.Join(fmt.Errorf("faccia pubblica: %w", err), li.Close())
	}
	server := func(h http.Handler) *http.Server {
		// DisableGeneralOptionsHandler: «OPTIONS *» arriva alla faccia (404 come lo standalone).
		return &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second, DisableGeneralOptionsHandler: true}
	}
	// Registro su stderr (journald): una riga per richiesta, senza percorsi né query.
	registro := slog.New(slog.NewTextHandler(os.Stderr, nil))
	si, sp := server(web.Interna(d, registro.With("faccia", "interna"))), server(web.Pubblica(d, registro.With("faccia", "pubblica")))
	errs := make(chan error, 2)
	go func() { errs <- si.Serve(li) }()
	go func() { errs <- sp.Serve(lp) }()
	if pronto != nil {
		pronto(li.Addr(), lp.Addr())
	}
	select {
	case <-ctx.Done():
	case err := <-errs:
		// Conta l'errore del server caduto; la chiusura dell'altro è solo pulizia.
		_ = si.Close()
		_ = sp.Close()
		return err
	}
	fine, annulla := context.WithTimeout(context.Background(), 10*time.Second)
	defer annulla()
	return errors.Join(si.Shutdown(fine), sp.Shutdown(fine))
}
