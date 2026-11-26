package backup

import (
	"context"
	"crypto/subtle"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La configurazione della copia remota, comune al comando gestrip backup-remoto (slice 128) e alla pagina
// Impostazioni → Backup del titolare (slice 129, T5.2, OPS-16 destinazione remota). Le regole sono una sola:
//
//   - i campi scritti da chi configura si controllano prima di collegarsi (NuovaConfigRemota): host, porta, utente
//     e cartella (relativa o assoluta: la risolve il server);
//   - l'impronta del server non si accetta mai in silenzio: si mostra (Remoto.ChiaveDelServer) e si fissa solo se
//     chi configura la riscrive uguale (FissaRemoto), dopo averla confrontata con quella del fornitore;
//   - la configurazione si scrive solo se la prova riesce: la chiave del server è quella, la chiave
//     dell'apparecchio entra e la cartella c'è (Remoto.Prova);
//   - ogni operazione di rete ha un tempo massimo (TempoMassimoProvaRemota, uno solo per tutta FissaRemoto) e,
//     dalla pagina, ce n'è una alla volta (ConfigurazioneRemota, ErrProvaInCorso): una seconda richiesta non aspetta
//     la prima, risponde subito; anche lo spegnimento passa dallo stesso turno (revisione 129, B4); lo stop di serve
//     interrompe la prova in corso (ConStop, revisione 129, M1).

// I campi della configurazione che chi configura scrive (ErrCampoRemoto.Campo).
const (
	CampoHost     = "host"
	CampoPorta    = "porta"
	CampoUtente   = "utente"
	CampoCartella = "cartella"
	CampoTipo     = "tipo"     // il tipo della chiave del server (lo dà il server)
	CampoImpronta = "impronta" // l'impronta scritta da chi configura
)

// maxCartellaRemota è la lunghezza massima della cartella scritta da chi configura.
const maxCartellaRemota = 1024

var (
	// ErrImprontaMalScritta: l'impronta scritta da chi configura non è come la dà ssh-keygen -l.
	ErrImprontaMalScritta = errors.New("backup: l'impronta va scritta come la dà ssh-keygen -l: SHA256: e 43 caratteri")
	// ErrProvaInCorso: dalla pagina c'è già un'operazione con il server remoto in corso (una alla volta).
	ErrProvaInCorso = errors.New("backup: c'è già una prova del collegamento con il server remoto in corso")
	// ErrRemotoNonConfigurato: la copia remota non è configurata (niente da provare).
	ErrRemotoNonConfigurato = errors.New("backup: la copia remota non è configurata")
)

// ErrCampoRemoto è un campo scritto da chi configura che non va (si vede prima di collegarsi).
type ErrCampoRemoto struct{ Campo string }

func (e *ErrCampoRemoto) Error() string {
	return "backup: configurazione della copia remota: campo " + e.Campo + " non valido"
}
func (e *ErrCampoRemoto) Unwrap() error { return ErrConfigRemota }

// ErrImprontaDiversa è il server che si presenta con un'impronta diversa da quella scritta da chi configura: Tipo e
// Vista sono quelle del server (non sono segrete). Niente è stato scritto.
type ErrImprontaDiversa struct{ Tipo, Vista string }

func (e *ErrImprontaDiversa) Error() string {
	return "backup: il server remoto si presenta con l'impronta " + e.Vista + " (" + e.Tipo + "), non con quella scritta"
}
func (e *ErrImprontaDiversa) Unwrap() error { return ErrImprontaServer }

// NuovaConfigRemota è la configurazione scritta da chi configura, con gli spazi ai lati tolti e i campi controllati
// prima di collegarsi (*ErrCampoRemoto). La cartella può essere relativa (alla cartella del sottoaccount): la
// risolve Remoto.Prova sul server. Tipo e impronta del server restano vuoti: li fissa FissaRemoto.
func NuovaConfigRemota(host string, porta int, utente, cartella string) (ConfigRemota, error) {
	c := ConfigRemota{Host: strings.TrimSpace(host), Porta: porta, Utente: strings.TrimSpace(utente),
		Cartella: strings.TrimSpace(cartella)}
	prima := c
	prima.Cartella, prima.TipoChiaveServer, prima.Impronta = "/cartella-da-risolvere", ssh.KeyAlgoED25519,
		"SHA256:"+strings.Repeat("A", 43)
	if campo := prima.campoSbagliato(); campo != "" {
		return c, &ErrCampoRemoto{Campo: campo}
	}
	if c.Cartella == "" || len(c.Cartella) > maxCartellaRemota || strings.ContainsAny(c.Cartella, "\r\n\x00") {
		return c, &ErrCampoRemoto{Campo: CampoCartella}
	}
	return c, nil
}

// FissaRemoto fissa la configurazione di r (fatta con NuovaConfigRemota) con l'impronta voluta, scritta da chi
// configura dopo averla confrontata con quella pubblicata dal fornitore: rilegge la chiave del server e la vuole
// uguale (*ErrImprontaDiversa, codice «impronta»), poi prova il collegamento con quella chiave fissata (chiave
// dell'apparecchio accettata, cartella che c'è) e solo se riesce scrive la configurazione, con la cartella vera del
// server. Dà la configurazione scritta. Su ogni errore non scrive niente. Tutto (lettura della chiave del server e
// prova) dura al più r.TempoProva (TempoMassimoProvaRemota): un solo tempo massimo, non uno per passo.
func FissaRemoto(ctx context.Context, d *db.DB, r *Remoto, voluta string, adesso time.Time) (ConfigRemota, error) {
	pctx, annulla := context.WithTimeoutCause(ctx, durataODi(r.TempoProva, TempoMassimoProvaRemota), errTempoProva)
	defer annulla()
	c, err := fissaRemoto(pctx, d, r, voluta, adesso)
	return c, tempoDellaProva(pctx, err)
}

func fissaRemoto(ctx context.Context, d *db.DB, r *Remoto, voluta string, adesso time.Time) (ConfigRemota, error) {
	voluta = strings.TrimSpace(voluta)
	if !ControllaImpronta(voluta) {
		return ConfigRemota{}, ErrImprontaMalScritta
	}
	if _, err := NuovaConfigRemota(r.Config.Host, r.Config.Porta, r.Config.Utente, r.Config.Cartella); err != nil {
		return ConfigRemota{}, err
	}
	tipo, vista, err := r.ChiaveDelServer(ctx)
	if err != nil {
		return ConfigRemota{}, err
	}
	if subtle.ConstantTimeCompare([]byte(vista), []byte(voluta)) != 1 {
		return ConfigRemota{}, errRemoto(CodiceRemotoImpronta, &ErrImprontaDiversa{Tipo: tipo, Vista: vista})
	}
	// Da qui la chiave del server è fissata: la prova si collega solo a un server con quella (controllaChiaveServer).
	r.Config.TipoChiaveServer, r.Config.Impronta = tipo, voluta
	vera, err := r.Prova(ctx)
	if err != nil {
		return ConfigRemota{}, err
	}
	r.Config.Cartella = vera
	if campo := r.Config.campoSbagliato(); campo != "" {
		return ConfigRemota{}, &ErrCampoRemoto{Campo: campo} // per esempio la radice del server
	}
	if err := ScriviConfigRemota(ctx, d, r.Config, adesso); err != nil {
		return ConfigRemota{}, err
	}
	return r.Config, nil
}

// ConfigurazioneRemota è la copia remota vista dalla pagina del titolare (slice 129): legge la configurazione, crea
// e mostra la chiave pubblica dell'apparecchio, mostra l'impronta del server, fissa, prova e spegne. Le operazioni
// con il server sono una alla volta: una seconda, mentre la prima è in corso, è subito ErrProvaInCorso (non
// aspetta, non accoda).
type ConfigurazioneRemota struct {
	stop    context.Context // lo stop di serve: interrompe la prova in corso (nil: nessuno)
	d       *db.DB
	dati    string
	o       clock.Orologio
	rete    sync.Mutex
	prepara func(*Remoto)
}

// NuovaConfigurazioneRemota è la configurazione della copia remota del database d (la chiave sta in secrets/ della
// sua cartella).
func NuovaConfigurazioneRemota(d *db.DB, o clock.Orologio) *ConfigurazioneRemota {
	return &ConfigurazioneRemota{d: d, dati: filepath.Dir(d.Percorso()), o: o}
}

// ConPreparazione cambia ogni Remoto prima di collegarsi: solo per i test (server SFTP finto, tempi corti).
func (c *ConfigurazioneRemota) ConPreparazione(f func(*Remoto)) *ConfigurazioneRemota {
	c.prepara = f
	return c
}

// ConStop lega le operazioni con il server allo stop di serve (il contesto dei segnali): allo stop la prova in corso
// finisce subito, così lo spegnimento del server HTTP (che aspetta le richieste al più 10 s) non scade. Perché qui e
// non nel BaseContext del server HTTP: tocca solo le richieste che parlano con la rete per decine di secondi, non
// cambia come finiscono tutte le altre pagine (revisione 129, M1).
func (c *ConfigurazioneRemota) ConStop(stop context.Context) *ConfigurazioneRemota {
	c.stop = stop
	return c
}

// conStop è ctx che finisce anche allo stop di serve.
func (c *ConfigurazioneRemota) conStop(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, annulla := context.WithCancelCause(ctx)
	if c.stop == nil {
		return ctx, func() { annulla(nil) }
	}
	ferma := context.AfterFunc(c.stop, func() { annulla(context.Cause(c.stop)) })
	return ctx, func() { ferma(); annulla(nil) }
}

func (c *ConfigurazioneRemota) remoto(cfg ConfigRemota) *Remoto {
	r := &Remoto{Config: cfg, Dati: c.dati}
	if c.prepara != nil {
		c.prepara(r)
	}
	return r
}

// conRete fa f solo se nessun'altra operazione con il server è in corso.
func (c *ConfigurazioneRemota) conRete(f func() error) error {
	if !c.rete.TryLock() {
		return ErrProvaInCorso
	}
	defer c.rete.Unlock()
	return f()
}

// Leggi è la configurazione salvata (LeggiConfigRemota).
func (c *ConfigurazioneRemota) Leggi(ctx context.Context) (cfg ConfigRemota, configurata bool, err error) {
	cfg, _, configurata, err = LeggiConfigRemota(ctx, c.d.Lettura)
	return cfg, configurata, err
}

// ChiavePubblica è la parte pubblica della chiave dell'apparecchio (ErrChiaveSFTPAssente se non c'è ancora).
func (c *ConfigurazioneRemota) ChiavePubblica() (string, error) {
	s, err := LeggiChiaveSFTP(c.dati)
	if err != nil {
		return "", err
	}
	return PubblicaSFTP(s), nil
}

// CreaChiave crea la chiave dell'apparecchio (o rilegge quella che c'è, creata false), con il lucchetto dei backup
// come gestrip chiave-sftp: un backup in corso è ErrBackupInCorso.
func (c *ConfigurazioneRemota) CreaChiave() (pubblica string, creata bool, err error) {
	l, err := PrendiLucchetto(c.dati)
	if err != nil {
		return "", false, err
	}
	defer func() { err = errors.Join(err, l.Rilascia()) }()
	return CreaChiaveSFTP(c.dati)
}

// ChiaveDelServer mostra tipo e impronta della chiave del server della configurazione nuova (non la accetta, non
// scrive niente).
func (c *ConfigurazioneRemota) ChiaveDelServer(ctx context.Context, nuova ConfigRemota) (tipo, impronta string, err error) {
	ctx, annulla := c.conStop(ctx)
	defer annulla()
	err = c.conRete(func() error {
		var e error
		tipo, impronta, e = c.remoto(nuova).ChiaveDelServer(ctx)
		return e
	})
	return tipo, impronta, err
}

// Fissa è FissaRemoto per la configurazione nuova con l'impronta voluta.
func (c *ConfigurazioneRemota) Fissa(ctx context.Context, nuova ConfigRemota, voluta string) (cfg ConfigRemota, err error) {
	ctx, annulla := c.conStop(ctx)
	defer annulla()
	err = c.conRete(func() error {
		var e error
		cfg, e = FissaRemoto(ctx, c.d, c.remoto(nuova), voluta, c.o.Adesso())
		return e
	})
	return cfg, err
}

// Prova prova il collegamento con la configurazione salvata, senza scrivere niente: dà la cartella vera.
func (c *ConfigurazioneRemota) Prova(ctx context.Context) (cartella string, err error) {
	ctx, annulla := c.conStop(ctx)
	defer annulla()
	err = c.conRete(func() error {
		cfg, ok, e := c.Leggi(ctx)
		switch {
		case e != nil:
			return errRemoto(CodiceRemotoConfigurazione, e)
		case !ok:
			return ErrRemotoNonConfigurato
		}
		cartella, e = c.remoto(cfg).Prova(ctx)
		return e
	})
	return cartella, err
}

// Togli spegne la copia remota (TogliConfigRemota): gli archivi sul server restano. Passa dallo stesso turno delle
// operazioni con il server: durante una conferma in corso è ErrProvaInCorso, altrimenti la conferma che finisce dopo
// riscriverebbe la configurazione appena spenta (revisione 129, B4).
func (c *ConfigurazioneRemota) Togli(ctx context.Context) error {
	return c.conRete(func() error { return TogliConfigRemota(ctx, c.d) })
}
