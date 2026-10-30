// Comando gestrip: il binario unico dell'apparecchio.
//
//	gestrip serve [-db FILE] [-interna IND] [-pubblica IND]
//	gestrip reset-link -url https://HOST[:PORTA] [-db FILE]
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
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
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
		fs.StringVar(&o.URL, "url", "", "indirizzo della faccia interna, es. https://negozio.tailnet.ts.net:8443")
		_ = fs.Parse(os.Args[2:]) // ExitOnError: con un errore esce da sola
		if !terminale(os.Stdout) {
			fmt.Fprintln(os.Stderr, "gestrip: il link si stampa solo su un terminale (non su un file, una pipe o il journal)")
			os.Exit(1)
		}
		if err := resetLink(context.Background(), o, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "gestrip:", err)
			os.Exit(1)
		}
	default:
		uso()
	}
}

func uso() {
	fmt.Fprintln(os.Stderr, "uso: gestrip serve [-db FILE] [-interna IND] [-pubblica IND]")
	fmt.Fprintln(os.Stderr, "     gestrip reset-link -url https://HOST[:PORTA] [-db FILE]")
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
		Recupero: accounts.NuovoRecupero(d, clock.Sistema{})}
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
	DB, URL string
}

// resetLink è il comando locale della password dimenticata del titolare (D-04): chi ha in mano
// l'apparecchio è il titolare. Il link si scrive solo su out, il terminale: mai nel registro del
// processo né nel database (lì c'è solo il suo SHA-256). Vale 30 minuti, una volta; il registro
// delle azioni ha la riga password_reset.requested senza il token. Il database non si migra: lo
// fa «gestrip serve».
func resetLink(ctx context.Context, o opzioniReset, out io.Writer) (err error) {
	base, err := baseInterna(o.URL)
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
	_, err = fmt.Fprintf(out, "Link per reimpostare la password di %s,\nvalido fino alle %s (%d minuti) e una sola volta:\n%s/reset/%s\n",
		email, clock.Timbro(scade), int(accounts.DurataReset.Minutes()), base, token)
	return err
}

var reNomeHost = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

// baseInterna controlla l'indirizzo della faccia interna: https, un nome di host valido (o un
// indirizzo IP), al più una porta da 1 a 65535, niente altro. Dove punti lo decide chi è al
// terminale, cioè il titolare.
func baseInterna(s string) (string, error) {
	sbagliato := errors.New("-url deve essere https://host[:porta] della faccia interna")
	u, err := url.Parse(strings.TrimSuffix(s, "/"))
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.User != nil || u.Path != "" || u.RawQuery != "" ||
		u.Fragment != "" || strings.ContainsAny(s, "?#") {
		return "", sbagliato
	}
	host, porta := u.Hostname(), u.Port()
	if _, e := netip.ParseAddr(host); e != nil && !reNomeHost.MatchString(strings.ToLower(host)) {
		return "", sbagliato
	}
	if strings.HasSuffix(u.Host, ":") {
		return "", sbagliato
	}
	if porta != "" {
		if n, err := strconv.Atoi(porta); err != nil || n < 1 || n > 65535 {
			return "", sbagliato
		}
	}
	return "https://" + u.Host, nil
}

// terminale dice se f è un terminale: il link di D-04 si stampa solo lì.
func terminale(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0 && fi.Mode()&os.ModeDevice != 0
}
