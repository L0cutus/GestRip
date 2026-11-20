package main

// Il modello finto del banco (slice 106): `banco modello-finto -ascolta :8082` serve il pacchetto
// modellofinto (risposte programmabili su /v1/chat/completions, rotte /_prova/…) in un container della
// rete interna dell'istanza. Dalla slice 107, con -dati sintetici-import, banco su lo avvia accanto al bot
// finto (grgo-<nome>-modello, alias modello.invalid, dal binario del banco stesso, nessuna porta sull'host):
// le due app lo raggiungono quando la loro pagina dell'assistente punta lì, il browser lo programma
// (MODELLO_URL, scenario assistente-salva).

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

// portaModelloFinto è la porta predefinita del modello finto; aliasModelloFinto il suo nome sulla rete interna
// dell'istanza (.invalid: nessun nome vero), indirizzoModelloFinto quello che le pagine e il browser usano.
const (
	portaModelloFinto     = "8082"
	aliasModelloFinto     = "modello.invalid"
	indirizzoModelloFinto = "http://" + aliasModelloFinto + ":" + portaModelloFinto
)

// comandoModelloFinto avvia il modello finto nella rete interna, con l'alias modello.invalid, dal binario del
// banco in sola lettura (come il bot finto); nessuna porta sull'host.
func comandoModelloFinto(n nomi, banco string) []string {
	return []string{"run", "-d", "--name", n.base + "-modello", "--network", n.rete, "--network-alias", aliasModelloFinto, "--label", "grgo=1",
		"--init", "--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--read-only",
		"--cpus", "1", "--memory", "256m", "--pids-limit", "64",
		"--log-opt", "max-size=10m", "--log-opt", "max-file=2",
		"--mount", "type=bind,src=" + banco + ",dst=/banco/banco,readonly",
		immagineBase, "/banco/banco", "modello-finto", "-ascolta", ":" + portaModelloFinto}
}

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
