// Comando gestrip: il binario unico dell'apparecchio.
//
//	gestrip serve [-db FILE] [-config FILE] [-interna IND] [-pubblica IND]
//	gestrip reset-link [-url https://HOST[:PORTA]] [-db FILE] [-config FILE]
//	gestrip invito -email EMAIL [-ruolo titolare|dipendente] [-nome NOME] [-url https://HOST[:PORTA]] [-db FILE] [-config FILE]
//
// Il file -config (di default /etc/gestrip/apparecchio.conf, lo scrive l'installer) dà
// l'indirizzo della faccia interna su cui si costruiscono i link:
//
//	indirizzo_interno = https://negozio.tailnet.ts.net:8443
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/apparecchio"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/ratelimit"
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
		fs.StringVar(&o.Config, "config", apparecchio.PercorsoConfigurazione, "file di configurazione dell'apparecchio")
		_ = fs.Parse(os.Args[2:]) // ExitOnError: con un errore esce da sola
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := serve(ctx, o, nil); err != nil {
			fmt.Fprintln(os.Stderr, "gestrip:", err)
			os.Exit(1)
		}
	case "reset-link":
		fs := flag.NewFlagSet("reset-link", flag.ExitOnError)
		o := opzioniReset{}
		fs.StringVar(&o.DB, "db", "/var/lib/gestrip/gestrip.db", "file del database")
		fs.StringVar(&o.Config, "config", apparecchio.PercorsoConfigurazione, "file di configurazione dell'apparecchio")
		fs.StringVar(&o.URL, "url", "", "solo per questo link, al posto di indirizzo_interno del file di configurazione")
		_ = fs.Parse(os.Args[2:]) // ExitOnError: con un errore esce da sola
		if err := argomentiFiniti(fs.Args()); err != nil {
			fmt.Fprintln(os.Stderr, "gestrip:", err)
			os.Exit(2)
		}
		if !terminale(os.Stdout) {
			fmt.Fprintln(os.Stderr, "gestrip: il link si stampa solo su un terminale (non su un file, una pipe o il journal)")
			os.Exit(1)
		}
		if err := resetLink(context.Background(), o, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "gestrip:", err)
			os.Exit(1)
		}
	case "invito":
		fs := flag.NewFlagSet("invito", flag.ExitOnError)
		o := opzioniInvito{}
		fs.StringVar(&o.DB, "db", "/var/lib/gestrip/gestrip.db", "file del database")
		fs.StringVar(&o.Config, "config", apparecchio.PercorsoConfigurazione, "file di configurazione dell'apparecchio")
		fs.StringVar(&o.URL, "url", "", "solo per questo link, al posto di indirizzo_interno del file di configurazione")
		fs.StringVar(&o.Email, "email", "", "email dell'account da invitare (o in attesa della password)")
		fs.StringVar(&o.Nome, "nome", "", "nome, solo per un account nuovo (facoltativo)")
		fs.StringVar(&o.Ruolo, "ruolo", "", "titolare o dipendente, solo per un account nuovo (di default dipendente)")
		_ = fs.Parse(os.Args[2:]) // ExitOnError: con un errore esce da sola
		if err := argomentiFiniti(fs.Args()); err != nil {
			fmt.Fprintln(os.Stderr, "gestrip:", err)
			os.Exit(2)
		}
		if !terminale(os.Stdout) {
			fmt.Fprintln(os.Stderr, "gestrip: il link si stampa solo su un terminale (non su un file, una pipe o il journal)")
			os.Exit(1)
		}
		if err := invito(context.Background(), o, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "gestrip:", err)
			os.Exit(1)
		}
	default:
		uso()
	}
}

func uso() {
	fmt.Fprintln(os.Stderr, "uso: gestrip serve [-db FILE] [-config FILE] [-interna IND] [-pubblica IND]")
	fmt.Fprintln(os.Stderr, "     gestrip reset-link [-url https://HOST[:PORTA]] [-db FILE] [-config FILE]")
	fmt.Fprintln(os.Stderr, "     gestrip invito -email EMAIL [-ruolo titolare|dipendente] [-nome NOME] [-url https://HOST[:PORTA]] [-db FILE] [-config FILE]")
	os.Exit(2)
}

type opzioniServe struct {
	DB, Interna, Pubblica string
	// Config è il file di configurazione dell'apparecchio (011b); vuoto: nessun file.
	Config string
}

// serve apre il database, lo migra e apre le due facce, e resta in piedi fino alla fine di ctx.
// pronto, se c'è, riceve gli indirizzi veri (utile con la porta 0 nei test).
func serve(ctx context.Context, o opzioniServe, pronto func(interna, pubblica net.Addr)) (err error) {
	// Un file di configurazione rovinato ferma l'avvio prima di toccare il database; uno che manca
	// lascia l'indirizzo interno non configurato (i link lo diranno).
	var conf apparecchio.Configurazione
	if o.Config != "" {
		if conf, err = apparecchio.Leggi(o.Config); err != nil {
			return fmt.Errorf("configurazione dell'apparecchio: %w", err)
		}
	}
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
	// La chiave del server si crea alla prima partenza e poi resta (sessioni e CSRF, 008b); una
	// chiave rovinata ferma l'avvio prima di aprire le facce.
	chiave, err := d.ChiaveServer(ctx, clock.Sistema{})
	if err != nil {
		return err
	}
	li, err := net.Listen("tcp", o.Interna)
	if err != nil {
		return fmt.Errorf("faccia interna: %w", err)
	}
	lp, err := net.Listen("tcp", o.Pubblica)
	if err != nil {
		return errors.Join(fmt.Errorf("faccia pubblica: %w", err), li.Close())
	}
	// Registro su stderr (journald): una riga per richiesta, senza percorsi né query.
	registro := slog.New(slog.NewTextHandler(os.Stderr, nil))
	// Alle facce solo sottochiavi, mai la chiave del server.
	dip := web.Dipendenze{Sonda: d, Utenti: accounts.NuoviUtenti(d, clock.Sistema{}),
		ChiaveSessione: db.SottoChiave(chiave, "sessione"), ChiaveCSRF: db.SottoChiave(chiave, "csrf"),
		Orologio: clock.Sistema{}, Limiti: ratelimit.Nuovo(clock.Sistema{}), Audit: audit.Nuovo(d, clock.Sistema{}),
		Recupero: accounts.NuovoRecupero(d, clock.Sistema{}), Inviti: accounts.NuoviInviti(d, clock.Sistema{}),
		IndirizzoInterno: conf.IndirizzoInterno}
	di, dp := dip, dip
	di.Registro, dp.Registro = registro.With("faccia", "interna"), registro.With("faccia", "pubblica")
	hi, err := web.Interna(di)
	if err != nil {
		return errors.Join(err, li.Close(), lp.Close())
	}
	hp, err := web.Pubblica(dp)
	if err != nil {
		return errors.Join(err, li.Close(), lp.Close())
	}
	si, sp := nuovoServer(hi), nuovoServer(hp)
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

// nuovoServer è un server HTTP di una faccia. DisableGeneralOptionsHandler: «OPTIONS *» arriva
// alla faccia (404 come lo standalone). I tempi massimi tengono a bada i client lenti: ora le
// facce leggono anche i corpi dei moduli (fino a 1 MB).
func nuovoServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:                      h,
		ReadHeaderTimeout:            10 * time.Second,
		ReadTimeout:                  60 * time.Second,
		IdleTimeout:                  120 * time.Second,
		DisableGeneralOptionsHandler: true,
	}
}

type opzioniReset struct {
	DB, Config string
	// URL, se c'è, sostituisce indirizzo_interno del file di configurazione, solo per questo link.
	URL string
}

// resetLink è il comando locale della password dimenticata del titolare (D-04): chi ha in mano
// l'apparecchio è il titolare. Il link si scrive solo su out, il terminale: mai nel registro del
// processo né nel database (lì c'è solo il suo SHA-256). Vale 30 minuti, una volta; il registro
// delle azioni ha la riga password_reset.requested senza il token. Si costruisce sull'indirizzo
// della faccia interna del file dell'apparecchio (011b), o su -url. Il database non si migra: lo
// fa «gestrip serve». Se la scrittura sul terminale fallisce dopo il commit, il link resta valido
// ma non stampato (come nella 011): scade da solo e il comando si rilancia.
func resetLink(ctx context.Context, o opzioniReset, out io.Writer) (err error) {
	base, err := indirizzoDelLink(o.URL, o.Config)
	if err != nil {
		return err
	}
	// db.Apri crea il file se manca: un percorso sbagliato lascerebbe un database vuoto.
	if fi, err := os.Stat(o.DB); err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("database %s non trovato: lo crea «gestrip serve»", o.DB)
	}
	d, err := db.Apri(o.DB)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, d.Chiudi()) }()
	token, email, scade, err := accounts.NuovoRecupero(d, clock.Sistema{}).LinkDallaConsole(ctx)
	if err != nil {
		return err
	}
	// Un link nuovo annulla quelli aperti, come nello standalone: anche quello chiesto dalla pagina,
	// che senza canali non arriva a nessuno.
	_, err = fmt.Fprintf(out, "Link per reimpostare la password di %s,\nvalido fino alle %s (%d minuti) e una sola volta:\n%s/reset/%s\n"+
		"Attenzione: una nuova richiesta dalla pagina «Password dimenticata?» annulla questo link.\n",
		email, clock.Timbro(scade), int(accounts.DurataReset.Minutes()), base, token)
	return err
}

// terminale dice se f è un terminale: il link di D-04 si stampa solo lì.
func terminale(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0 && fi.Mode()&os.ModeDevice != 0
}

type opzioniInvito struct {
	DB, Config, URL string
	Email           string
	// Nome e Ruolo valgono solo per un account nuovo; vuoti: nessun nome, dipendente.
	Nome, Ruolo string
}

// invito è il comando locale degli inviti (D-32), con le regole di reset-link: l'account nuovo
// riceve il suo link, quello ancora in attesa della password un link nuovo che annulla i vecchi.
// Il link si scrive solo su out, il terminale; il registro delle azioni ha la riga con attore
// «console», senza il token. Il database non si migra e non si crea.
func invito(ctx context.Context, o opzioniInvito, out io.Writer) (err error) {
	base, err := indirizzoDelLink(o.URL, o.Config)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(o.DB); err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("database %s non trovato: lo crea «gestrip serve»", o.DB)
	}
	d, err := db.Apri(o.DB)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, d.Chiudi()) }()
	inviti := accounts.NuoviInviti(d, clock.Sistema{})
	console := audit.Contesto{Attore: "console"}
	id, inAttesa, trovato, err := inviti.AccountAttivo(ctx, o.Email)
	if err != nil {
		return err
	}
	var inv accounts.Invito
	var testa string
	switch {
	case trovato && !inAttesa:
		return fmt.Errorf("l'account %s ha già una password: nessun nuovo link", accounts.NormalizzaEmail(o.Email))
	case trovato && (o.Ruolo != "" || o.Nome != ""):
		return fmt.Errorf("l'account %s esiste già: -ruolo e -nome valgono solo per un account nuovo", accounts.NormalizzaEmail(o.Email))
	case trovato:
		if inv, err = inviti.NuovoLink(ctx, id, console); err != nil {
			return err
		}
		testa = fmt.Sprintf("Nuovo link d'invito per %s: il precedente non vale più.\n", inv.Email)
	default:
		ruolo := o.Ruolo
		if ruolo == "" {
			ruolo = accounts.Dipendente
		}
		inv, err = inviti.Invita(ctx, o.Email, o.Nome, ruolo, console)
		switch {
		case errors.Is(err, accounts.ErrRuoloNonValido):
			return fmt.Errorf("-ruolo deve essere %s o %s", accounts.Titolare, accounts.Dipendente)
		case errors.Is(err, accounts.ErrEmailNonValida):
			return errors.New("-email non è un indirizzo email valido")
		case errors.Is(err, accounts.ErrNomeNonValido):
			return errors.New("-nome è più lungo di 120 caratteri")
		case err != nil:
			return err
		}
		testa = fmt.Sprintf("Account %s aggiunto come %s.\n", inv.Email, ruolo)
	}
	_, err = fmt.Fprintf(out, "%sLink d'invito, valido fino alle %s (%d minuti) e una sola volta:\n%s/invito/%s\n"+
		"Attenzione: un nuovo link dalla pagina utenti annulla questo link.\n",
		testa, clock.Timbro(inv.Scade), int(accounts.DurataInvito.Minutes()), base, inv.Token)
	return err
}

// indirizzoDelLink è l'indirizzo su cui si costruisce un link della console: -url se c'è,
// altrimenti indirizzo_interno del file dell'apparecchio (011b).
func indirizzoDelLink(u, config string) (string, error) {
	if u != "" {
		base, err := apparecchio.ControllaIndirizzo(u)
		if err != nil {
			return "", fmt.Errorf("-url: %w", err)
		}
		return base, nil
	}
	base, err := apparecchio.IndirizzoInterno(config)
	if errors.Is(err, apparecchio.ErrNonConfigurato) {
		return "", fmt.Errorf("%w: scrivi «indirizzo_interno = https://host[:porta]» in %s, oppure dai -url per questo link", err, config)
	}
	return base, err
}

// argomentiFiniti: dopo le opzioni non deve restare niente (un -ruolo scritto male finirebbe lì).
func argomentiFiniti(resto []string) error {
	if len(resto) > 0 {
		return fmt.Errorf("argomenti in più: %s", strings.Join(resto, " "))
	}
	return nil
}
