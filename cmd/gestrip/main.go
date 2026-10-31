// Comando gestrip: il binario unico dell'apparecchio.
//
//	gestrip serve [-db FILE] [-config FILE] [-interna IND] [-pubblica IND]
//	gestrip reset-link [-url https://HOST[:PORTA]] [-db FILE] [-config FILE]
//	gestrip invito -email EMAIL [-ruolo titolare|dipendente] [-nome NOME] [-url https://HOST[:PORTA]] [-db FILE] [-config FILE]
//	gestrip primo-titolare [-url https://HOST[:PORTA]] [-db FILE] [-config FILE]
//	gestrip import -sintetico|-autorizzato -riferimento "CHI E QUANDO" -sorgente FILE -sha256 HEX [-dimensione BYTE] -destinazione FILE -report FILE
//
// Il file -config (di default /etc/gestrip/apparecchio.conf, lo scrive l'installer) dà
// l'indirizzo della faccia interna su cui si costruiscono i link:
//
//	indirizzo_interno = https://negozio.tailnet.ts.net:8443
//
// serve parte solo se la cartella del database sta su un volume cifrato (LUKS, OPS-12), se le due
// facce ascoltano su loopback e se nell'ambiente non c'è un segreto come valore (SECRET_KEY_BASE,
// LEGACY_WARRANTY_SECRET: OPS-13). Le opzioni che spengono i primi due controlli, solo per lo
// sviluppo e il banco, sono in docs/SVILUPPO.md: i rifiuti non le nominano.
//
// Uno stop (SIGTERM, SIGINT) che arriva mentre serve si avvia non è un errore: serve esce con 0.
//
// import porta il SQLite in chiaro dello standalone in un database Go nuovo (PIANO T2) e scrive il
// report; esce con 0 solo con il verdetto «riconciliato». Come serve, vuole la cartella della
// destinazione su un volume cifrato.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/apparecchio"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/importer"
	"github.com/L0cutus/gestrip/internal/ratelimit"
	"github.com/L0cutus/gestrip/internal/segreti"
	"github.com/L0cutus/gestrip/internal/web"
)

func main() {
	if len(os.Args) < 2 {
		uso()
	}
	// Il processo tiene in memoria la chiave del server: niente core dump (OPS-13).
	if err := segreti.NonScaricabile(); err != nil {
		fmt.Fprintln(os.Stderr, "gestrip:", err)
		os.Exit(1)
	}
	switch os.Args[1] {
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		o := opzioniDiServe(fs)
		_ = fs.Parse(os.Args[2:]) // ExitOnError: con un errore esce da sola
		o.inAvvio = avvioDeiTest
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		// «in ascolto» dice che le facce servono e serve aspetta il segnale. Un SIGTERM che arriva
		// prima interrompe l'avvio a metà: è uno stop, non un errore (uscita 0, ADM-20).
		inAscolto := func(interna, pubblica net.Addr) {
			fmt.Fprintf(os.Stderr, "gestrip: in ascolto: interna %s, pubblica %s\n", interna, pubblica)
		}
		err := serve(ctx, *o, inAscolto)
		switch {
		case err == nil:
		case err == errFermatoInAvvio: // proprio lui: insieme a un altro errore (la chiusura del database) è un'uscita 1
			fmt.Fprintln(os.Stderr, "gestrip:", err)
		default:
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
	case "primo-titolare":
		fs := flag.NewFlagSet("primo-titolare", flag.ExitOnError)
		o := opzioniPrimo{}
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
		if err := primoTitolare(context.Background(), o, os.Stdout); err != nil {
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
	case "import":
		fs := flag.NewFlagSet("import", flag.ExitOnError)
		o := opzioniDiImport(fs)
		_ = fs.Parse(os.Args[2:]) // ExitOnError: con un errore esce da sola
		if err := argomentiFiniti(fs.Args()); err != nil {
			fmt.Fprintln(os.Stderr, "gestrip:", err)
			os.Exit(2)
		}
		o.dopoTabella = importDeiTest
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		es, err := importa(ctx, *o, os.Stderr)
		if es.Report != "" {
			fmt.Fprintf(os.Stderr, "gestrip: import %s: verdetto %s; report %s; sha256 della sorgente %s; righe lette %d, scritte %d\n",
				es.Modalita, es.Verdetto, es.Report, es.Sha256, es.Lette, es.Scritte)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "gestrip:", err)
		}
		if err != nil || es.Verdetto != importer.Riconciliato {
			stop()
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
	fmt.Fprintln(os.Stderr, "     gestrip primo-titolare [-url https://HOST[:PORTA]] [-db FILE] [-config FILE]")
	fmt.Fprintln(os.Stderr, "     gestrip import -sintetico|-autorizzato -riferimento \"CHI E QUANDO\" -sorgente FILE -sha256 HEX [-dimensione BYTE] -destinazione FILE -report FILE -media CARTELLA|-senza-media [-miniature CARTELLA]")
	os.Exit(2)
}

type opzioniServe struct {
	DB, Interna, Pubblica string
	// Config è il file di configurazione dell'apparecchio (011b); vuoto: nessun file.
	Config string
	// SenzaVolumeCifrato salta il controllo del volume cifrato.
	SenzaVolumeCifrato bool
	// AscoltoNonLocale permette alle facce un indirizzo fuori da loopback (sviluppo e banco).
	AscoltoNonLocale bool
	// radiceSistema è dove si leggono /proc e /sys; vuota: «/». avvisi riceve gli avvisi
	// dell'avvio; nil: stderr. Servono ai test.
	radiceSistema string
	avvisi        io.Writer
	// aspettaConservazione è l'attesa tra due controlli della conservazione dell'audit; nil:
	// audit.OgniQuantoConservazione sull'orologio vero. Serve ai test.
	aspettaConservazione func(context.Context) bool
	// ascolta apre le facce; nil: net.Listen. Serve ai test (una faccia che cade).
	ascolta func(rete, indirizzo string) (net.Listener, error)
	// inAvvio, se c'è, gira a metà dell'avvio (dopo le migrazioni, prima della chiave del server)
	// con il contesto di serve. Serve ai test: blocca l'avvio finché arriva lo stop.
	inAvvio func(context.Context)
}

// avvioDeiTest è inAvvio per il processo figlio dei test (figlio_linux_test.go); nil fuori dai test.
var avvioDeiTest func(context.Context)

// errFermatoInAvvio: lo stop (SIGTERM, SIGINT: il contesto di serve finito) è arrivato mentre
// serve si avviava, prima che le facce fossero su. Non è un errore: main esce con 0.
var errFermatoInAvvio = errors.New("fermato durante l'avvio")

// fermatoOErrore dà errFermatoInAvvio se err viene solo dal contesto di serve, che è finito; ogni
// altro errore resta com'è, anche con il contesto finito: un errore vero non si nasconde dietro lo
// stop, nemmeno quando gli sta accanto (errors.Join dello stop con «copia … da togliere a mano»,
// in backupPrima), che errors.Is darebbe per uno stop.
func fermatoOErrore(ctx context.Context, err error) error {
	if causa := ctx.Err(); causa != nil && soloDa(err, causa) {
		return errFermatoInAvvio
	}
	return err
}

// soloDa dice se ogni ramo degli errori avvolti in err (anche più d'uno, come errors.Join o due
// %w) finisce in causa.
func soloDa(err, causa error) bool {
	if err == causa {
		return true
	}
	switch e := err.(type) {
	case interface{ Unwrap() []error }:
		figli := e.Unwrap()
		for _, f := range figli {
			if !soloDa(f, causa) {
				return false
			}
		}
		return len(figli) > 0
	case interface{ Unwrap() error }:
		f := e.Unwrap()
		return f != nil && soloDa(f, causa)
	}
	return false
}

// opzioneAscoltoNonLocale è l'opzione di serve che lascia le facce fuori da loopback, solo per lo
// sviluppo e il banco. Come per il volume cifrato, il rifiuto non la nomina.
const opzioneAscoltoNonLocale = "-ascolto-non-locale"

// controllaAscoltoLocale accetta solo un indirizzo d'ascolto su loopback: un IP scritto in
// 127.0.0.0/8 o ::1, oppure il nome localhost, sempre con la porta. Nessun nome si risolve; dopo
// l'apertura serve guarda anche dove ascolta davvero la faccia (ascoltoLocale). La sola porta
// (":47101") vuol dire tutte le interfacce: rifiutata.
func controllaAscoltoLocale(indirizzo string) error {
	host, _, err := net.SplitHostPort(indirizzo)
	if err != nil {
		return fmt.Errorf("l'indirizzo d'ascolto %q non è valido (serve HOST:PORTA)", indirizzo)
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("l'indirizzo d'ascolto %q non è locale: le facce ascoltano solo su loopback (127.0.0.1 o ::1), "+
		"dietro tailscale serve e funnel", indirizzo)
}

// ascoltoLocale controlla dove ascolta davvero una faccia aperta (localhost si risolve fuori da
// gestrip, in /etc/hosts). Arriva dopo le migrazioni: se localhost finisse fuori da loopback, il
// database sarebbe già creato e migrato prima del rifiuto. È un caso remoto (un /etc/hosts
// sbagliato) e il database resta sul volume cifrato, quindi si lascia così.
func ascoltoLocale(a net.Addr) error {
	if t, ok := a.(*net.TCPAddr); ok && t.IP.IsLoopback() {
		return nil
	}
	return fmt.Errorf("ascolta su %s, che non è locale: le facce ascoltano solo su loopback (127.0.0.1 o ::1)", a)
}

// aspettaPer è l'attesa vera dei lavori; un test la sostituisce per leggere l'intervallo.
var aspettaPer = clock.Aspetta

// opzioniDiServe registra le opzioni di serve in fs.
func opzioniDiServe(fs *flag.FlagSet) *opzioniServe {
	o := &opzioniServe{}
	fs.StringVar(&o.DB, "db", "/var/lib/gestrip/gestrip.db", "file del database")
	fs.StringVar(&o.Interna, "interna", "127.0.0.1:47101", "indirizzo della faccia interna (dietro tailscale serve)")
	fs.StringVar(&o.Pubblica, "pubblica", "127.0.0.1:47102", "indirizzo della faccia pubblica (dietro tailscale funnel)")
	fs.StringVar(&o.Config, "config", apparecchio.PercorsoConfigurazione, "file di configurazione dell'apparecchio")
	fs.BoolVar(&o.SenzaVolumeCifrato, strings.TrimPrefix(apparecchio.OpzioneSenzaVolumeCifrato, "-"), false,
		"salta il controllo che i dati stiano su un volume cifrato (LUKS): solo per lo sviluppo e il banco, mai sull'apparecchio")
	fs.BoolVar(&o.AscoltoNonLocale, strings.TrimPrefix(opzioneAscoltoNonLocale, "-"), false,
		"lascia le facce ascoltare fuori da loopback: solo per lo sviluppo e il banco, mai sull'apparecchio")
	return o
}

// serve apre il database, lo migra e apre le due facce, e resta in piedi fino alla fine di ctx.
// In sottofondo gira la conservazione dell'audit (D-14), che serve ferma e aspetta all'uscita.
// pronto, se c'è, riceve gli indirizzi veri (utile con la porta 0 nei test).
func serve(ctx context.Context, o opzioniServe, pronto func(interna, pubblica net.Addr)) (err error) {
	// Un segreto passato come valore nell'ambiente ferma l'avvio prima di tutto (OPS-13).
	if err := segreti.ControllaAmbiente(os.LookupEnv); err != nil {
		return err
	}
	// Un file di configurazione rovinato ferma l'avvio prima di toccare il database; uno che manca
	// lascia l'indirizzo interno non configurato (i link lo diranno).
	var conf apparecchio.Configurazione
	if o.Config != "" {
		if conf, err = apparecchio.Leggi(o.Config); err != nil {
			return fmt.Errorf("configurazione dell'apparecchio: %w", err)
		}
	}
	// Senza il volume cifrato sotto i dati non si parte, e il database in chiaro non si crea
	// (OPS-12); -senza-volume-cifrato, solo per lo sviluppo e il banco, lo dice forte.
	avvisi := o.avvisi
	if avvisi == nil {
		avvisi = os.Stderr
	}
	// Le facce sono in chiaro: ascoltano solo su loopback, dove le raggiunge solo tailscaled che fa
	// il TLS (OPS-12, decisione del 30/10/2026). -ascolto-non-locale, solo per lo sviluppo e il
	// banco (il container), lo dice forte. Il controllo viene prima del database.
	if o.AscoltoNonLocale {
		_, _ = fmt.Fprintf(avvisi, "gestrip: ATTENZIONE: ascolto fuori da loopback permesso (%s): le facce in chiaro "+
			"si raggiungono dalla rete; solo per lo sviluppo e il banco, mai sull'apparecchio\n", opzioneAscoltoNonLocale)
	} else {
		for _, f := range []struct{ nome, indirizzo string }{{"faccia interna", o.Interna}, {"faccia pubblica", o.Pubblica}} {
			if err := controllaAscoltoLocale(f.indirizzo); err != nil {
				return fmt.Errorf("%s: %w", f.nome, err)
			}
		}
	}
	if o.SenzaVolumeCifrato {
		// Un avviso che non si scrive non ferma l'avvio (come le migrazioni qui sotto).
		_, _ = fmt.Fprintf(avvisi, "gestrip: ATTENZIONE: controllo del volume cifrato spento (%s): i dati stanno in chiaro; "+
			"solo per lo sviluppo e il banco, mai sull'apparecchio\n", apparecchio.OpzioneSenzaVolumeCifrato)
	} else {
		radice := o.radiceSistema
		if radice == "" {
			radice = "/"
		}
		if err := apparecchio.ControllaVolumeCifrato(apparecchio.Sistema{Radice: radice}, o.DB); err != nil {
			return err
		}
	}
	// db.Apri crea il database se manca: accanto ai resti di un import interrotto (la cartella
	// media già attivata o la cartella di lavoro) ne nascerebbe uno vuoto vicino alle foto dei
	// clienti. Prima si tolgono i resti a mano o si rifà l'import.
	if _, err := os.Lstat(o.DB); errors.Is(err, fs.ErrNotExist) {
		resti, err := importer.RestiDiImport(o.DB)
		if err != nil {
			return err
		}
		if len(resti) > 0 {
			return fmt.Errorf("il database %s non c'è e accanto c'è %s: probabilmente il resto di un import interrotto; "+
				"non creo un database vuoto (togli a mano i resti e rifai l'import, o indica il database giusto)", o.DB, resti[0])
		}
	}
	d, err := db.Apri(o.DB)
	if err != nil {
		return err
	}
	// Chiudi porta il WAL nel file principale (checkpoint dello scrittore, chiuso per ultimo): un suo
	// errore va riportato. Senza errore di chiusura err resta com'è (errFermatoInAvvio si confronta
	// così com'è, in main).
	defer func() {
		if e := d.Chiudi(); e != nil {
			err = errors.Join(err, e)
		}
	}()
	// Da qui serve usa il contesto: lo stop durante le migrazioni o il salvataggio della chiave
	// non è un errore (fermatoOErrore).
	applicate, err := d.Migra(ctx, clock.Sistema{})
	if err != nil {
		return fermatoOErrore(ctx, err)
	}
	if len(applicate) > 0 {
		fmt.Fprintf(os.Stderr, "gestrip: migrazioni applicate: %v\n", applicate)
	}
	// La chiave del server si crea alla prima partenza e poi resta (sessioni e CSRF, 008b); una
	// chiave rovinata ferma l'avvio prima di aprire le facce.
	if o.inAvvio != nil {
		o.inAvvio(ctx)
	}
	chiave, err := d.ChiaveServer(ctx, clock.Sistema{})
	if err != nil {
		return fermatoOErrore(ctx, err)
	}
	ascolta := o.ascolta
	if ascolta == nil {
		ascolta = net.Listen
	}
	li, err := ascolta("tcp", o.Interna)
	if err != nil {
		return fmt.Errorf("faccia interna: %w", err)
	}
	if !o.AscoltoNonLocale {
		if err := ascoltoLocale(li.Addr()); err != nil {
			return errors.Join(fmt.Errorf("faccia interna: %w", err), li.Close())
		}
	}
	lp, err := ascolta("tcp", o.Pubblica)
	if err != nil {
		return errors.Join(fmt.Errorf("faccia pubblica: %w", err), li.Close())
	}
	if !o.AscoltoNonLocale {
		if err := ascoltoLocale(lp.Addr()); err != nil {
			return errors.Join(fmt.Errorf("faccia pubblica: %w", err), li.Close(), lp.Close())
		}
	}
	// Registro su stderr (journald): una riga per richiesta, senza percorsi né query.
	registro := slog.New(slog.NewTextHandler(os.Stderr, nil))
	dip := dipendenzeWeb(d, chiave, conf, o)
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
	// La conservazione dell'audit (D-14) gira in sottofondo; serve la ferma e la aspetta prima di
	// chiudere il database, in ogni uscita da qui in poi.
	var lavori sync.WaitGroup
	ctxLavori, fermaLavori := context.WithCancel(ctx)
	defer func() {
		fermaLavori()
		lavori.Wait()
	}()
	aspetta := o.aspettaConservazione
	if aspetta == nil {
		aspetta = func(ctx context.Context) bool { return aspettaPer(ctx, audit.OgniQuantoConservazione) }
	}
	lavori.Go(func() {
		audit.NuovaConservazione(d, clock.Sistema{}).Lavoro(ctxLavori, aspetta, registro.With("lavoro", "conservazione_audit"))
	})
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
	// Come serve: un segreto come valore nell'ambiente ferma ogni comando (OPS-13, runtime.exs).
	if err := segreti.ControllaAmbiente(os.LookupEnv); err != nil {
		return err
	}
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
	// Come serve: un segreto come valore nell'ambiente ferma ogni comando (OPS-13, runtime.exs).
	if err := segreti.ControllaAmbiente(os.LookupEnv); err != nil {
		return err
	}
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

type opzioniPrimo struct {
	DB, Config, URL string
}

// primoTitolare è il comando del primo titolare (ACC-16, «sostituita»: al posto dei due file privati
// dell'installer dello standalone), con le regole di reset-link: solo su un database senza nessun
// account, stampa sul terminale il link monouso /configura/<codice>, valido 60 minuti; un codice
// nuovo annulla quelli aperti; il registro delle azioni ha la riga con attore «console», senza il
// codice. Il database non si migra e non si crea: lo fa «gestrip serve», che serve anche la pagina.
func primoTitolare(ctx context.Context, o opzioniPrimo, out io.Writer) (err error) {
	// Come serve: un segreto come valore nell'ambiente ferma ogni comando (OPS-13, runtime.exs).
	if err := segreti.ControllaAmbiente(os.LookupEnv); err != nil {
		return err
	}
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
	codice, scade, err := accounts.NuovoPrimoTitolare(d, clock.Sistema{}).LinkDallaConsole(ctx)
	if errors.Is(err, accounts.ErrGiaConfigurato) {
		return fmt.Errorf("%w: il primo titolare si crea solo su un database senza account; per un altro account usa "+
			"«gestrip invito», per la password del titolare «gestrip reset-link»", err)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Link per creare il primo titolare, valido fino alle %s (%d minuti) e una sola volta:\n%s/configura/%s\n"+
		"Attenzione: un nuovo «gestrip primo-titolare» annulla questo link.\n",
		clock.Timbro(scade), int(accounts.DurataPrimoTitolare.Minutes()), base, codice)
	return err
}

type opzioniImport struct {
	importer.Opzioni
	// SenzaVolumeCifrato salta il controllo del volume cifrato sulla cartella della destinazione,
	// come in serve (sviluppo, banco, prove in tmpfs).
	SenzaVolumeCifrato bool
	// radiceSistema è dove si leggono /proc e /sys; vuota: «/». Serve ai test.
	radiceSistema string
	// dopoTabella, se c'è, si chiama dopo ogni tabella copiata. Serve ai test.
	dopoTabella func(context.Context, string)
}

// importDeiTest è dopoTabella per il processo figlio dei test (figlio_linux_test.go); nil fuori
// dai test.
var importDeiTest func(context.Context, string)

func opzioniDiImport(fs *flag.FlagSet) *opzioniImport {
	o := &opzioniImport{}
	fs.BoolVar(&o.Sintetico, "sintetico", false, "la sorgente ha solo dati inventati")
	fs.BoolVar(&o.Autorizzato, "autorizzato", false, "la sorgente è una copia di dati veri, con -riferimento")
	fs.StringVar(&o.Riferimento, "riferimento", "", "con -autorizzato: chi ha autorizzato l'import e quando (va nel report)")
	fs.StringVar(&o.Sorgente, "sorgente", "", "il database dello standalone, già decifrato (SQLite in chiaro)")
	fs.StringVar(&o.Sha256, "sha256", "", "lo SHA-256 atteso della sorgente (sha256sum)")
	fs.Int64Var(&o.Dimensione, "dimensione", 0, "la dimensione attesa della sorgente in byte (facoltativa)")
	fs.StringVar(&o.Destinazione, "destinazione", "", "il database Go nuovo: non deve esistere")
	fs.StringVar(&o.Report, "report", "", "il report.json: non deve esistere")
	fs.StringVar(&o.Media, "media", "", "la cartella media dello standalone (si legge soltanto); i file vanno in media/ accanto alla destinazione, che non deve esistere")
	fs.BoolVar(&o.SenzaMedia, "senza-media", false, "importa il database senza i file media (scelta esplicita, segnalata nel report)")
	fs.StringVar(&o.Miniature, "miniature", "", "la cartella delle miniature dello standalone (facoltativa): si contano, non si importano")
	fs.BoolVar(&o.SenzaVolumeCifrato, strings.TrimPrefix(apparecchio.OpzioneSenzaVolumeCifrato, "-"), false,
		"salta il controllo che la destinazione stia su un volume cifrato (LUKS): solo per lo sviluppo e il banco, mai sull'apparecchio")
	return o
}

// importa è il comando import: i controlli di ogni comando (OPS-13) e del volume cifrato (OPS-12,
// come serve), poi importer.Importa. Il registro (stderr) non ha mai valori dei dati né il
// percorso della sorgente: solo il verdetto, i conteggi, l'impronta e il percorso del report.
func importa(ctx context.Context, o opzioniImport, avvisi io.Writer) (importer.Esito, error) {
	if err := segreti.ControllaAmbiente(os.LookupEnv); err != nil {
		return importer.Esito{}, err
	}
	if o.SenzaVolumeCifrato {
		_, _ = fmt.Fprintf(avvisi, "gestrip: ATTENZIONE: controllo del volume cifrato spento (%s): i dati stanno in chiaro; "+
			"solo per lo sviluppo e il banco, mai sull'apparecchio\n", apparecchio.OpzioneSenzaVolumeCifrato)
	} else {
		radice := o.radiceSistema
		if radice == "" {
			radice = "/"
		}
		if o.Destinazione == "" {
			return importer.Esito{}, errors.New("servono -sorgente, -destinazione e -report")
		}
		if err := apparecchio.ControllaVolumeCifrato(apparecchio.Sistema{Radice: radice}, o.Destinazione); err != nil {
			return importer.Esito{}, err
		}
	}
	oi := o.Opzioni
	oi.Orologio = clock.Sistema{}
	oi.DopoTabella = o.dopoTabella
	return importer.Importa(ctx, oi)
}

// dipendenzeWeb è quello che serve alle due facce. Alle facce solo sottochiavi, mai la chiave del
// server; l'indirizzo interno e il percorso del file vengono dalla configurazione dell'apparecchio.
func dipendenzeWeb(d *db.DB, chiave []byte, conf apparecchio.Configurazione, o opzioniServe) web.Dipendenze {
	registro := audit.Nuovo(d, clock.Sistema{})
	return web.Dipendenze{Sonda: d, Utenti: accounts.NuoviUtenti(d, clock.Sistema{}),
		ChiaveSessione: db.SottoChiave(chiave, "sessione"), ChiaveCSRF: db.SottoChiave(chiave, "csrf"),
		Orologio: clock.Sistema{}, Limiti: ratelimit.Nuovo(clock.Sistema{}), Audit: registro, Consultazione: registro,
		Recupero: accounts.NuovoRecupero(d, clock.Sistema{}), Inviti: accounts.NuoviInviti(d, clock.Sistema{}),
		PrimoTitolare:    accounts.NuovoPrimoTitolare(d, clock.Sistema{}),
		IndirizzoInterno: conf.IndirizzoInterno, Configurazione: o.Config}
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
		return "", fmt.Errorf("%w: %s, oppure dai -url per questo link", err, apparecchio.ComeConfigurare(config))
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
