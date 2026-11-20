package main

// Il modello finto del banco (slice 106): `banco modello-finto -ascolta :8082` serve il pacchetto
// modellofinto (risposte programmabili su /v1/chat/completions, rotte /_prova/…) in un container della
// rete interna dell'istanza. Nessuno scenario lo usa ancora: lo collegano le slice 107-108.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/L0cutus/gestrip/tools/banco/modellofinto"
)

// portaModelloFinto è la porta predefinita del modello finto.
const portaModelloFinto = "8082"

// cmdModelloFinto è `banco modello-finto -ascolta IND`: resta in piedi fino a SIGTERM o SIGINT.
func cmdModelloFinto(args []string) error {
	fs := flag.NewFlagSet("modello-finto", flag.ContinueOnError)
	ascolta := fs.String("ascolta", ":"+portaModelloFinto, "indirizzo su cui ascoltare")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("argomenti in più non riconosciuti: %v", fs.Args())
	}
	ctx, ferma := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer ferma()
	err := modellofinto.Servi(ctx, modellofinto.Nuovo(), *ascolta, func(a string) { fmt.Println("modello finto in ascolto su", a) })
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
