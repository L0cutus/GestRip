package backup

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La copia remota dei backup su SFTP (slice 128, T5.2, OPS-16 destinazione remota, §8.2 n. 15, risposte D, E, F e I
// del piano): dopo il backup riuscito sulla chiavetta, lo stesso archivio (già cifrato e verificato) va sul server
// remoto (la Storage Box, un sottoaccount per apparecchio con la sua cartella). Lo standalone la fa con restic da
// otto-vps, fuori dal pacchetto.
//
//   - **Configurazione:** l'impostazione solo Go ChiaveRemoto (host, porta, utente, cartella, tipo e impronta della
//     chiave del server), scritta dal comando gestrip backup-remoto o dalla pagina Impostazioni → Backup (129),
//     tutti e due con FissaRemoto (configura_remoto.go). Sta nel database e
//     viaggia con il backup: il ripristino (130) la spegne (risposta I), altrimenti una copia di prova ruoterebbe i
//     backup veri sul server.
//   - **Chiave dell'apparecchio:** ed25519 in secrets/chiave_sftp (0600, formato OpenSSH), creata da gestrip
//     chiave-sftp, che stampa la parte pubblica da incollare nel pannello della Storage Box. Entra nell'archivio
//     (risposta B): una macchina ripristinata riparte con la stessa chiave.
//   - **Impronta del server:** fissata nella configurazione, mai accettata in silenzio (niente «prima volta
//     buona»): il comando la mostra e la scrive solo se chi configura la riscrive uguale (-impronta) dopo averla
//     confrontata con quella pubblicata da Hetzner. A ogni collegamento una chiave diversa, o di un altro tipo, è
//     ErrImprontaServer prima dell'autenticazione: niente viene mandato.
//   - **Caricamento:** in <nome>.pending (O_EXCL), lo SHA-256 dei byte mandati ricalcolato mentre vanno (deve
//     essere quello della verifica), la dimensione del .pending sul server, la rinomina (SSH_FXP_RENAME, che come
//     OpenSSH non sovrascrive; prima un Lstat del nome finale) e di nuovo la dimensione del nome finale. Lo
//     SHA-256 dal server non si chiede: SFTP non ha un comando per farlo (l'estensione check-file non c'è in
//     OpenSSH né nella Storage Box); il controllo intero è il download mensile della risposta E (non qui).
//   - **Registro:** una riga «sftp» del registro dei backup verificati per la copia, che non sblocca niente (D-11,
//     risposta F: solo la chiavetta); una riga di copie_remote per ogni copia arrivata in fondo, per lo stato e gli
//     avvisi.
//   - **Rotazione 7 + 4** (D-114) solo nella cartella remota configurata, con daTenere della chiavetta: solo i nomi
//     esatti, solo file normali, solo archivi con una riga «sftp» del registro con gli stessi byte, mai la copia
//     appena fatta (che dev'essere l'ultima «sftp» 'ok' del registro), un nome registrato nel futuro ferma tutto;
//     prima di ogni Remove si riguarda il nome (tipo, byte, ora di modifica). I .pending rimasti si tolgono prima
//     del caricamento, solo con i nomi esatti.
//   - **Tempi:** connessione, handshake (con autenticazione e avvio di SFTP), assenza di risposte dal server e
//     durata intera hanno un tetto; allo scadere, o allo stop di serve, il sorvegliante chiude la connessione e la
//     lettura dell'archivio: nessuna operazione resta appesa alla rete. La lettura dell'archivio dalla chiavetta
//     gira in una goroutine a parte, così anche una chiavetta appesa non tiene ferma la copia (la goroutine resta
//     appesa con lei; è il rischio in lista della 124, che qui non peggiora).

// ChiaveRemoto è la chiave di impostazioni, solo del Go, della copia remota (JSON di ConfigRemota). Senza la riga la
// copia remota non è configurata.
const ChiaveRemoto = "backup.remoto"

// NomeChiaveSFTP è il file della chiave dell'apparecchio per la copia remota in secrets/ (entra nell'archivio).
const NomeChiaveSFTP = "chiave_sftp"

// commentoChiaveSFTP è il commento della chiave pubblica, quello che si vede nel pannello della Storage Box.
const commentoChiaveSFTP = "gestrip-apparecchio"

// PortaRemotaPredefinita è la porta proposta dal comando e dalla pagina: la 23, dove la Storage Box risponde con
// OpenSSH e la chiave ED25519 pubblicata da Hetzner (sulla 22 c'è ProFTPD con sola RSA; prova vera della 129b,
// voce 19 decisa da Stefano il 27/11/2026).
const PortaRemotaPredefinita = 23

// I tempi massimi della copia remota.
const (
	// TempoConnessioneRemota è il tempo massimo per aprire la connessione TCP.
	TempoConnessioneRemota = 30 * time.Second
	// TempoHandshakeRemoto è il tempo massimo per handshake SSH, autenticazione e avvio di SFTP.
	TempoHandshakeRemoto = 30 * time.Second
	// TempoSenzaRisposteRemoto è quanto il server può restare zitto mentre aspettiamo (un caricamento fermo, una
	// cartella che non si elenca, un server lento): poi la connessione si chiude.
	TempoSenzaRisposteRemoto = 2 * time.Minute
	// TempoMassimoCopiaRemota è la durata massima di una copia: con ~3,6 GB e una banda in uscita di 2 Mbit/s
	// servono circa 4 ore (la misura vera è della 129b, la prova vera). Oltre, la copia fallisce e il backup notturno finisce.
	TempoMassimoCopiaRemota = 6 * time.Hour
	// TempoMassimoProvaRemota è la durata massima di Prova e di ChiaveDelServer (comando e pagina, slice 129):
	// connessione, handshake, avvio di SFTP e controllo della cartella insieme.
	TempoMassimoProvaRemota = 60 * time.Second
)

// MargineSpazioRemoto è lo spazio che deve restare libero sul server oltre all'archivio, se il server sa dire lo
// spazio libero (statvfs@openssh.com).
const MargineSpazioRemoto = 64 << 20

// I codici di una copia remota non riuscita (colonna codice di copie_remote, db.CodiciCopiaRemota): parole fisse,
// da cui lo stato fa il testo per il titolare e per gli avvisi, mai dal motivo grezzo.
const (
	CodiceRemotoConfigurazione = "configurazione"
	CodiceRemotoChiave         = "chiave"
	CodiceRemotoRete           = "rete"
	CodiceRemotoImpronta       = "impronta"
	CodiceRemotoAccesso        = "accesso"
	CodiceRemotoCartella       = "cartella"
	CodiceRemotoSpazio         = "spazio"
	CodiceRemotoEsistente      = "esistente"
	CodiceRemotoCaricamento    = "caricamento"
	CodiceRemotoControllo      = "controllo"
	CodiceRemotoRegistro       = "registro"
	CodiceRemotoRotazione      = "rotazione"
	CodiceRemotoAltro          = "altro"
)

var (
	// ErrConfigRemota: la configurazione della copia remota non è valida.
	ErrConfigRemota = errors.New("backup: configurazione della copia remota non valida")
	// ErrChiaveSFTPAssente: manca la chiave dell'apparecchio per la copia remota.
	ErrChiaveSFTPAssente = errors.New("backup: chiave dell'apparecchio per la copia remota assente (si crea con «gestrip chiave-sftp»)")
	// ErrImprontaServer: il server si è presentato con una chiave diversa da quella configurata.
	ErrImprontaServer = errors.New("backup: il server remoto ha un'impronta diversa da quella configurata, non mi collego")
	// ErrTempoRemoto: il server non risponde entro i tempi (connessione, handshake, risposte, durata).
	ErrTempoRemoto = errors.New("backup: il server remoto non risponde entro il tempo massimo")
	// ErrCartellaRemota: la cartella configurata non c'è sul server o non è una cartella.
	ErrCartellaRemota = errors.New("backup: la cartella della copia remota non c'è sul server o non è una cartella")
	// ErrSpazioRemoto: sul server non c'è spazio per l'archivio.
	ErrSpazioRemoto = errors.New("backup: sul server remoto non c'è spazio per l'archivio")
	// ErrRemotoEsistente: sul server c'è già un file con il nome dell'archivio (o del suo .pending): non si
	// sovrascrive mai.
	ErrRemotoEsistente = errors.New("backup: sul server remoto c'è già un file con quel nome, non lo sovrascrivo")
	// ErrControlloRemoto: quello che è arrivato sul server non è l'archivio verificato (byte o SHA-256 dei byte
	// mandati diversi).
	ErrControlloRemoto = errors.New("backup: l'archivio sul server remoto non ha i byte dell'archivio verificato")
	// ErrNomeServer: il nome del server non si trova (DNS). Il messaggio non ripete il nome.
	ErrNomeServer = errors.New("nome del server non trovato")
)

// ErrRemoto è una copia remota non riuscita con il suo codice fisso.
type ErrRemoto struct {
	Codice string
	Err    error
}

func (e *ErrRemoto) Error() string { return "copia remota: " + e.Err.Error() }
func (e *ErrRemoto) Unwrap() error { return e.Err }

func errRemoto(codice string, err error) error { return &ErrRemoto{Codice: codice, Err: err} }

// CodiceRemoto è il codice di un errore della copia remota (CodiceRemotoAltro se non ne ha uno).
func CodiceRemoto(err error) string {
	var e *ErrRemoto
	if errors.As(err, &e) {
		return e.Codice
	}
	return CodiceRemotoAltro
}

// ConfigRemota è la configurazione della copia remota (JSON in ChiaveRemoto).
type ConfigRemota struct {
	Host   string `json:"host"`
	Porta  int    `json:"porta"`
	Utente string `json:"utente"`
	// Cartella è la cartella sul server, assoluta (come la dà il server, RealPath) e non la radice.
	Cartella string `json:"cartella"`
	// TipoChiaveServer (per esempio ssh-ed25519) e Impronta («SHA256:…», come ssh-keygen -l) sono la chiave del
	// server fissata alla configurazione.
	TipoChiaveServer string `json:"tipo_chiave_server"`
	Impronta         string `json:"impronta"`
}

// tipiChiaveServer sono i tipi di chiave del server accettati, con gli algoritmi da chiedere nell'handshake.
var tipiChiaveServer = map[string][]string{
	ssh.KeyAlgoED25519:  {ssh.KeyAlgoED25519},
	ssh.KeyAlgoECDSA256: {ssh.KeyAlgoECDSA256},
	ssh.KeyAlgoECDSA384: {ssh.KeyAlgoECDSA384},
	ssh.KeyAlgoECDSA521: {ssh.KeyAlgoECDSA521},
	ssh.KeyAlgoRSA:      {ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256},
}

// ControllaImpronta dice se s è un'impronta SHA-256 come la scrive ssh-keygen -l: «SHA256:» e 43 caratteri base64
// senza «=».
func ControllaImpronta(s string) bool {
	b, ok := strings.CutPrefix(s, "SHA256:")
	if !ok || len(b) != 43 {
		return false
	}
	d, err := base64.RawStdEncoding.DecodeString(b)
	return err == nil && len(d) == sha256.Size
}

// Controlla dice se la configurazione si può usare: host senza spazi, porta 1-65535, utente, cartella assoluta e
// pulita che non è la radice, tipo di chiave conosciuto e impronta ben scritta.
func (c ConfigRemota) Controlla() error {
	switch c.campoSbagliato() {
	case "":
		return nil
	case CampoHost:
		return fmt.Errorf("%w: host", ErrConfigRemota)
	case CampoPorta:
		return fmt.Errorf("%w: porta", ErrConfigRemota)
	case CampoUtente:
		return fmt.Errorf("%w: utente", ErrConfigRemota)
	case CampoCartella:
		return fmt.Errorf("%w: la cartella dev'essere assoluta, pulita, non la radice e non «home» (né dentro «home/home»)", ErrConfigRemota)
	case CampoTipo:
		return fmt.Errorf("%w: tipo della chiave del server", ErrConfigRemota)
	}
	return fmt.Errorf("%w: impronta del server", ErrConfigRemota)
}

// campoSbagliato è il primo campo che non va di c ("" se vanno tutti), con le regole di Controlla.
func (c ConfigRemota) campoSbagliato() string {
	switch {
	case c.Host == "" || strings.ContainsAny(c.Host, " \t\r\n/@") || len(c.Host) > 253:
		return CampoHost
	case c.Porta < 1 || c.Porta > 65535:
		return CampoPorta
	case c.Utente == "" || strings.ContainsAny(c.Utente, " \t\r\n@:/") || len(c.Utente) > 64:
		return CampoUtente
	case !path.IsAbs(c.Cartella) || path.Clean(c.Cartella) != c.Cartella || c.Cartella == "/" ||
		strings.ContainsAny(c.Cartella, "\r\n\x00") || cartellaAmbigua(c.Cartella):
		return CampoCartella
	case tipiChiaveServer[c.TipoChiaveServer] == nil:
		return CampoTipo
	case !ControllaImpronta(c.Impronta):
		return CampoImpronta
	}
	return ""
}

// cartellaAmbigua dice se la cartella vera del server è una di quelle che, cambiando porta, si confonderebbero con
// un'altra per db.StessaCartellaRemota (revisione della 133c): «/home» (la radice della Storage Box dalla 23, o una
// cartella «home» dalla 22) e «/home/home» con quello che c'è dentro (una cartella «home» dalla 23). Si rifiutano:
// la cartella dei backup ha un altro nome. Resta accettata, ambigua, una cartella «home/…» creata a mano dalla 22
// («/home/backup» dalla 22 è «/home/home/backup» dalla 23, che si rifiuta: rifacendo dalla 23 si vede subito).
func cartellaAmbigua(c string) bool {
	return c == "/home" || c == "/home/home" || strings.HasPrefix(c, "/home/home/")
}

// LeggiConfigRemota legge la configurazione della copia remota: configurata dice se la riga c'è; aggiornata è quando
// è stata scritta. Una riga illeggibile o non valida è configurata con un errore ErrConfigRemota (lo stato lo dice e
// la copia non parte).
func LeggiConfigRemota(ctx context.Context, q db.Interrogatore) (c ConfigRemota, aggiornata time.Time, configurata bool, err error) {
	var v, a string
	err = q.QueryRowContext(ctx, `SELECT valore, aggiornata_il FROM impostazioni WHERE chiave = ?`, ChiaveRemoto).Scan(&v, &a)
	if errors.Is(err, sql.ErrNoRows) {
		return ConfigRemota{}, time.Time{}, false, nil
	}
	if err != nil {
		return ConfigRemota{}, time.Time{}, false, fmt.Errorf("backup: configurazione della copia remota: %w", err)
	}
	aggiornata, _ = time.Parse(db.FormatoIstante, a) // illeggibile: zero (lo stato usa l'ora per «mai riuscita»)
	dec := json.NewDecoder(strings.NewReader(v))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return ConfigRemota{}, aggiornata, true, fmt.Errorf("%w: illeggibile", ErrConfigRemota)
	}
	if err := c.Controlla(); err != nil {
		return ConfigRemota{}, aggiornata, true, err
	}
	return c, aggiornata, true, nil
}

// ScriviConfigRemota scrive la configurazione (già controllata con il server da chi chiama).
func ScriviConfigRemota(ctx context.Context, d *db.DB, c ConfigRemota, adesso time.Time) error {
	if err := c.Controlla(); err != nil {
		return err
	}
	v, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = d.Scrittura.ExecContext(ctx, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, ?)
		ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore, aggiornata_il = excluded.aggiornata_il`,
		ChiaveRemoto, string(v), adesso.UTC().Format(db.FormatoIstante))
	if err != nil {
		return fmt.Errorf("backup: configurazione della copia remota: %w", err)
	}
	return nil
}

// TogliConfigRemota spegne la copia remota (toglie la riga). Gli archivi sul server restano.
func TogliConfigRemota(ctx context.Context, d *db.DB) error {
	if _, err := d.Scrittura.ExecContext(ctx, `DELETE FROM impostazioni WHERE chiave = ?`, ChiaveRemoto); err != nil {
		return fmt.Errorf("backup: configurazione della copia remota: %w", err)
	}
	return nil
}

// FileChiaveSFTP è il file della chiave dell'apparecchio per la copia remota.
func FileChiaveSFTP(dati string) string {
	return filepath.Join(dati, CartellaSegreti, NomeChiaveSFTP)
}

// maxChiaveSFTP è la dimensione massima del file della chiave (una chiave ed25519 OpenSSH è ~400 byte).
const maxChiaveSFTP = 16 << 10

// CreaChiaveSFTP crea la chiave ed25519 dell'apparecchio in secrets/chiave_sftp (0600, formato OpenSSH) e ne dà la
// parte pubblica (una riga di authorized_keys). Se la chiave c'è già non la tocca: la rilegge e ne dà la pubblica,
// con creata false (il server la conosce già: cambiarla fermerebbe la copia remota). Come CreaChiave: temporaneo
// 0600 nella stessa cartella, fsync, rinomina che non sovrascrive.
func CreaChiaveSFTP(dati string) (pubblica string, creata bool, err error) {
	if s, err := LeggiChiaveSFTP(dati); err == nil {
		return PubblicaSFTP(s), false, nil
	} else if !errors.Is(err, ErrChiaveSFTPAssente) {
		return "", false, err
	}
	dir := filepath.Join(dati, CartellaSegreti)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", false, fmt.Errorf("backup: cartella dei segreti: %w", err)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", false, err
	}
	blocco, err := ssh.MarshalPrivateKey(priv, commentoChiaveSFTP)
	if err != nil {
		return "", false, err
	}
	tmp, err := os.CreateTemp(dir, "."+NomeChiaveSFTP+"-") // 0600
	if err != nil {
		return "", false, fmt.Errorf("backup: chiave della copia remota: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // dopo la rinomina non c'è più
	_, err = tmp.Write(pem.EncodeToMemory(blocco))
	if err == nil {
		err = tmp.Sync()
	}
	if errClose := tmp.Close(); err == nil {
		err = errClose
	}
	if err != nil {
		return "", false, fmt.Errorf("backup: chiave della copia remota: %w", err)
	}
	if err := rinominaSenzaSovrascrivere(tmp.Name(), FileChiaveSFTP(dati)); err != nil {
		return "", false, fmt.Errorf("backup: chiave della copia remota: %w", err)
	}
	if err := sincronizzaCartella(dir); err != nil {
		return "", false, fmt.Errorf("backup: chiave della copia remota scritta, ma la cartella non si sincronizza: %w", err)
	}
	s, err := LeggiChiaveSFTP(dati)
	if err != nil {
		return "", false, err
	}
	return PubblicaSFTP(s), true, nil
}

// LeggiChiaveSFTP legge la chiave dell'apparecchio: un file normale (non un collegamento) dell'utente del programma,
// senza permessi per gli altri, con una chiave ed25519 OpenSSH senza passphrase.
func LeggiChiaveSFTP(dati string) (ssh.Signer, error) {
	f, err := apriSenzaCollegamenti(FileChiaveSFTP(dati))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrChiaveSFTPAssente
	}
	if err != nil {
		return nil, fmt.Errorf("backup: chiave della copia remota: %w", err)
	}
	defer func() { _ = f.Close() }() // sola lettura
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0 || !dellUtente(fi) {
		return nil, fmt.Errorf("backup: la chiave della copia remota dev'essere un file normale del programma, 0600")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxChiaveSFTP+1))
	if err != nil {
		return nil, fmt.Errorf("backup: chiave della copia remota: %w", err)
	}
	if len(b) > maxChiaveSFTP {
		return nil, errors.New("backup: il file della chiave della copia remota è troppo grande")
	}
	s, err := ssh.ParsePrivateKey(b)
	if err != nil {
		return nil, errors.New("backup: il file della chiave della copia remota non contiene una chiave OpenSSH valida")
	}
	if s.PublicKey().Type() != ssh.KeyAlgoED25519 {
		return nil, errors.New("backup: la chiave della copia remota non è ed25519")
	}
	return s, nil
}

// PubblicaSFTP è la riga authorized_keys della chiave pubblica dell'apparecchio.
func PubblicaSFTP(s ssh.Signer) string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(s.PublicKey()))) + " " + commentoChiaveSFTP
}

// Remoto è una copia remota da fare: la configurazione, la cartella dei dati (dove sta la chiave), i tempi (zero:
// quelli predefiniti) e come aprire la connessione (nil: TCP; i test la sostituiscono).
type Remoto struct {
	Config ConfigRemota
	Dati   string
	// ErrConfig è l'errore della configurazione letta (CaricaRemoto): la copia non parte e lo registra.
	ErrConfig error
	// I tempi; zero: TempoConnessioneRemota, TempoHandshakeRemoto, TempoSenzaRisposteRemoto, TempoMassimoCopiaRemota,
	// TempoMassimoProvaRemota (Prova e ChiaveDelServer, slice 129).
	TempoConnessione, TempoHandshake, TempoSenzaRisposte, TempoMassimo, TempoProva time.Duration
	// Componi apre la connessione; nil: TCP con net.Dialer.
	Componi func(ctx context.Context, indirizzo string) (net.Conn, error)
}

// CaricaRemoto legge la configurazione della copia remota del database d: nil senza configurazione; con una
// configurazione non valida un Remoto con ErrConfig (la copia fallisce con il codice «configurazione»).
func CaricaRemoto(ctx context.Context, d *db.DB) (*Remoto, error) {
	c, _, ok, err := LeggiConfigRemota(ctx, d.Lettura)
	if !ok && err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return &Remoto{Config: c, Dati: filepath.Dir(d.Percorso()), ErrConfig: err}, nil
}

func durataODi(d, predefinita time.Duration) time.Duration {
	if d <= 0 {
		return predefinita
	}
	return d
}

// EsitoCopiaRemota è quello che la copia remota ha fatto: i .pending tolti, l'archivio sul server (se la rinomina è
// arrivata), se è registrato (riga «sftp» 'ok') e la rotazione remota.
type EsitoCopiaRemota struct {
	PendingTolti []string
	Archivio     string
	Registrato   bool
	Rotazione    EsitoRotazione
}

// sorvegliante chiude la connessione (e la lettura dell'archivio) quando il server resta zitto troppo, quando la
// copia dura troppo o allo stop: dopo, ogni operazione in corso sulla connessione finisce con un errore.
type sorvegliante struct {
	conn    net.Conn
	mu      sync.Mutex
	ultimo  clock.Partenza // l'ultimo byte arrivato dal server (lettura monotona)
	causa   error
	lettura *io.PipeReader
	annulla context.CancelFunc
	fermato atomic.Bool
	fatto   chan struct{}
}

func avviaSorvegliante(ctx context.Context, conn net.Conn, senzaRisposte time.Duration) *sorvegliante {
	interno, annulla := context.WithCancel(ctx)
	s := &sorvegliante{conn: conn, ultimo: clock.Parti(), annulla: annulla, fatto: make(chan struct{})}
	passo := min(senzaRisposte/4, time.Second)
	if passo <= 0 {
		passo = time.Millisecond
	}
	go func() {
		defer close(s.fatto)
		for clock.Aspetta(interno, passo) {
			s.mu.Lock()
			zitto := s.ultimo.Trascorso() > senzaRisposte
			s.mu.Unlock()
			if zitto {
				s.scatta(fmt.Errorf("%w (nessuna risposta dal server)", ErrTempoRemoto))
				return
			}
		}
		if s.fermato.Load() {
			return // la sessione è chiusa: niente da sorvegliare
		}
		err := ctx.Err()
		if errors.Is(context.Cause(ctx), errTempoMassimo) {
			err = fmt.Errorf("%w (durata massima della copia)", ErrTempoRemoto)
		}
		s.scatta(err)
	}()
	return s
}

// errTempoMassimo è la causa del contesto della copia scaduto per la durata massima.
var errTempoMassimo = errors.New("durata massima della copia remota")

func (s *sorvegliante) vivo() {
	s.mu.Lock()
	s.ultimo = clock.Parti()
	s.mu.Unlock()
}

func (s *sorvegliante) scatta(err error) {
	s.mu.Lock()
	if s.causa == nil {
		s.causa = err
	}
	l := s.lettura
	s.mu.Unlock()
	_ = s.conn.Close() // la connessione si butta: le operazioni in corso finiscono con un errore
	if l != nil {
		l.CloseWithError(err)
	}
}

func (s *sorvegliante) conLettura(l *io.PipeReader) {
	s.mu.Lock()
	s.lettura = l
	causa := s.causa
	s.mu.Unlock()
	if causa != nil {
		l.CloseWithError(causa)
	}
}

func (s *sorvegliante) scattato() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.causa
}

func (s *sorvegliante) ferma() {
	s.fermato.Store(true)
	s.annulla()
	<-s.fatto
}

// connSorvegliata segna «vivo» a ogni byte che arriva dal server.
type connSorvegliata struct {
	net.Conn
	s *sorvegliante
}

func (c connSorvegliata) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.s.vivo()
	}
	return n, err
}

// sessione è una connessione SFTP aperta con il suo sorvegliante.
type sessione struct {
	conn  net.Conn
	ssh   *ssh.Client
	sftp  *sftp.Client
	sorv  *sorvegliante
	causa func(error) error
}

func (s *sessione) chiudi() {
	_ = s.sftp.Close() // sola chiusura: la copia è già decisa
	_ = s.ssh.Close()
	_ = s.conn.Close()
	s.sorv.ferma()
}

// controllaChiaveServer è la verifica della chiave del server: tipo e impronta della configurazione, nient'altro
// (niente known_hosts, niente «prima volta buona»).
func controllaChiaveServer(c ConfigRemota) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, k ssh.PublicKey) error {
		if k.Type() != c.TipoChiaveServer ||
			subtle.ConstantTimeCompare([]byte(ssh.FingerprintSHA256(k)), []byte(c.Impronta)) != 1 {
			return ErrImprontaServer
		}
		return nil
	}
}

// senzaIndirizzo toglie host e indirizzi da un errore di rete (il motivo va nella pagina e nel registro, mai l'host
// intero): resta la causa del sistema.
func senzaIndirizzo(err error) error {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return ErrNomeServer
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Err != nil {
		return op.Err
	}
	return err
}

// componi apre la connessione TCP (o quella dei test) con il tempo massimo di connessione.
func (r *Remoto) componi(ctx context.Context) (net.Conn, error) {
	ind := net.JoinHostPort(r.Config.Host, strconv.Itoa(r.Config.Porta))
	cctx, annulla := context.WithTimeout(ctx, durataODi(r.TempoConnessione, TempoConnessioneRemota))
	defer annulla()
	var conn net.Conn
	var err error
	if r.Componi != nil {
		conn, err = r.Componi(cctx, ind)
	} else {
		conn, err = (&net.Dialer{}).DialContext(cctx, "tcp", ind)
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if cctx.Err() != nil {
			return nil, errRemoto(CodiceRemotoRete, fmt.Errorf("%w (connessione)", ErrTempoRemoto))
		}
		return nil, errRemoto(CodiceRemotoRete, fmt.Errorf("connessione al server remoto non riuscita: %w", senzaIndirizzo(err)))
	}
	return conn, nil
}

// collega apre la sessione SFTP: connessione, handshake con la verifica dell'impronta, autenticazione con la chiave
// dell'apparecchio e avvio di SFTP, tutto entro TempoHandshake. Senza la chiave del server giusta non si manda
// niente (la verifica è prima dell'autenticazione).
func (r *Remoto) collega(ctx context.Context, firma ssh.Signer) (*sessione, error) {
	conn, err := r.componi(ctx)
	if err != nil {
		return nil, err
	}
	sorv := avviaSorvegliante(ctx, conn, durataODi(r.TempoSenzaRisposte, TempoSenzaRisposteRemoto))
	causa := func(err error) error {
		if c := sorv.scattato(); c != nil {
			if errors.Is(c, ErrTempoRemoto) {
				return errRemoto(CodiceRemotoRete, c)
			}
			return c // lo stop: il contesto, senza codice
		}
		return err
	}
	fallito := func(err error) (*sessione, error) {
		_ = conn.Close()
		sorv.ferma()
		return nil, err
	}
	_ = conn.SetDeadline(clock.Sistema{}.Adesso().Add(durataODi(r.TempoHandshake, TempoHandshakeRemoto)))
	cfg := &ssh.ClientConfig{
		User:              r.Config.Utente,
		Auth:              []ssh.AuthMethod{ssh.PublicKeys(firma)},
		HostKeyCallback:   controllaChiaveServer(r.Config),
		HostKeyAlgorithms: tipiChiaveServer[r.Config.TipoChiaveServer],
	}
	cc, canali, richieste, err := ssh.NewClientConn(connSorvegliata{Conn: conn, s: sorv}, net.JoinHostPort(r.Config.Host, strconv.Itoa(r.Config.Porta)), cfg)
	if err != nil {
		switch {
		case errors.Is(err, ErrImprontaServer):
			return fallito(errRemoto(CodiceRemotoImpronta, ErrImprontaServer))
		case strings.Contains(err.Error(), "no common algorithm for host key"):
			// Il server non ha più una chiave del tipo configurato: è una chiave diversa, come un'impronta diversa.
			return fallito(errRemoto(CodiceRemotoImpronta, fmt.Errorf("%w (nessuna chiave del tipo configurato)", ErrImprontaServer)))
		case strings.Contains(err.Error(), "unable to authenticate"):
			return fallito(errRemoto(CodiceRemotoAccesso, errors.New("backup: il server remoto ha rifiutato la chiave dell'apparecchio")))
		case errors.Is(err, os.ErrDeadlineExceeded):
			return fallito(causa(errRemoto(CodiceRemotoRete, fmt.Errorf("%w (handshake)", ErrTempoRemoto))))
		}
		return fallito(causa(errRemoto(CodiceRemotoRete, fmt.Errorf("handshake con il server remoto non riuscito: %w", senzaIndirizzo(err)))))
	}
	client := ssh.NewClient(cc, canali, richieste)
	sc, err := sftp.NewClient(client, sftp.UseConcurrentWrites(true))
	if err != nil {
		_ = client.Close()
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return fallito(causa(errRemoto(CodiceRemotoRete, fmt.Errorf("%w (avvio di SFTP)", ErrTempoRemoto))))
		}
		return fallito(causa(errRemoto(CodiceRemotoRete, fmt.Errorf("SFTP non parte sul server remoto: %w", senzaIndirizzo(err)))))
	}
	_ = conn.SetDeadline(time.Time{})
	return &sessione{conn: conn, ssh: client, sftp: sc, sorv: sorv, causa: causa}, nil
}

// Prova apre la sessione con la configurazione e la chiave dell'apparecchio e controlla la cartella: è quello che il
// comando gestrip backup-remoto fa prima di scrivere la configurazione. Dà la cartella vera (RealPath).
//
// Dura al più TempoProva (TempoMassimoProvaRemota, slice 129): allo scadere il sorvegliante chiude la connessione e
// l'errore è ErrTempoRemoto, codice «rete» (nota 13: prima solo connessione, handshake e assenza di risposte).
func (r *Remoto) Prova(ctx context.Context) (string, error) {
	firma, err := LeggiChiaveSFTP(r.Dati)
	if err != nil {
		return "", errRemoto(CodiceRemotoChiave, err)
	}
	pctx, annulla := context.WithTimeoutCause(ctx, durataODi(r.TempoProva, TempoMassimoProvaRemota), errTempoProva)
	defer annulla()
	vera, err := r.prova(pctx, firma)
	return vera, tempoDellaProva(pctx, err)
}

func (r *Remoto) prova(ctx context.Context, firma ssh.Signer) (string, error) {
	s, err := r.collega(ctx, firma)
	if err != nil {
		return "", err
	}
	defer s.chiudi()
	vera, err := s.sftp.RealPath(r.Config.Cartella)
	if err != nil {
		return "", s.causa(errRemoto(CodiceRemotoCartella, errors.Join(ErrCartellaRemota, err)))
	}
	if err := s.cartella(vera); err != nil {
		return "", err
	}
	return vera, nil
}

// errTempoProva è la causa del contesto di Prova e ChiaveDelServer scaduto per la durata massima.
var errTempoProva = errors.New("durata massima della prova del collegamento")

// tempoDellaProva: se la prova (contesto pctx) è finita perché è scaduto il suo tempo massimo, e non per lo stop di
// chi l'ha chiesta (allora la causa è quella dello stop), l'errore è ErrTempoRemoto con il codice «rete», qualunque
// operazione si sia interrotta (connessione, handshake, cartella: il sorvegliante chiude la connessione).
func tempoDellaProva(pctx context.Context, err error) error {
	if err == nil || !errors.Is(context.Cause(pctx), errTempoProva) {
		return err
	}
	return errRemoto(CodiceRemotoRete, fmt.Errorf("%w (durata massima della prova)", ErrTempoRemoto))
}

// ChiaveDelServer apre la connessione solo fino a vedere la chiave del server (si ferma prima dell'autenticazione)
// e ne dà tipo e impronta: per mostrarla a chi configura, che la confronta con quella pubblicata dal fornitore. Non
// la accetta e non scrive niente. Dura al più TempoProva, come Prova.
func (r *Remoto) ChiaveDelServer(ctx context.Context) (tipo, impronta string, err error) {
	pctx, annulla := context.WithTimeoutCause(ctx, durataODi(r.TempoProva, TempoMassimoProvaRemota), errTempoProva)
	defer annulla()
	tipo, impronta, err = r.chiaveDelServer(pctx)
	return tipo, impronta, tempoDellaProva(pctx, err)
}

func (r *Remoto) chiaveDelServer(ctx context.Context) (tipo, impronta string, err error) {
	conn, err := r.componi(ctx)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(clock.Sistema{}.Adesso().Add(durataODi(r.TempoHandshake, TempoHandshakeRemoto)))
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	errVisto := errors.New("chiave vista")
	cfg := &ssh.ClientConfig{
		User: r.Config.Utente,
		HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
			tipo, impronta = k.Type(), ssh.FingerprintSHA256(k)
			return errVisto
		},
		HostKeyAlgorithms: []string{ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521,
			ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256},
	}
	_, _, _, err = ssh.NewClientConn(conn, net.JoinHostPort(r.Config.Host, strconv.Itoa(r.Config.Porta)), cfg)
	if errors.Is(err, errVisto) {
		return tipo, impronta, nil
	}
	if ctx.Err() != nil {
		return "", "", ctx.Err()
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return "", "", errRemoto(CodiceRemotoRete, fmt.Errorf("%w (handshake)", ErrTempoRemoto))
	}
	return "", "", errRemoto(CodiceRemotoRete, fmt.Errorf("handshake con il server remoto non riuscito: %w", senzaIndirizzo(err)))
}

// cartella vuole la cartella remota: c'è ed è una cartella vera (Lstat: un collegamento non vale).
func (s *sessione) cartella(c string) error {
	fi, err := s.sftp.Lstat(c)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return errRemoto(CodiceRemotoCartella, ErrCartellaRemota)
		}
		return s.causa(errRemoto(CodiceRemotoRete, fmt.Errorf("cartella remota: %w", err)))
	}
	if !fi.IsDir() {
		return errRemoto(CodiceRemotoCartella, ErrCartellaRemota)
	}
	return nil
}

// Copia carica sul server l'archivio r della chiavetta dest, verificato (v) e registrato come «chiavetta», lo
// registra come «sftp» e ruota la cartella remota. La chiama Giro.Esegui solo dopo un backup riuscito sulla
// chiavetta, con il lucchetto dei backup e quello della chiavetta presi. adesso è l'orologio del registro.
func (r *Remoto) Copia(ctx context.Context, d *db.DB, dest *Destinazione, ris Risultato, v Verificato, adesso clock.Orologio) (e EsitoCopiaRemota, err error) {
	if r.ErrConfig != nil {
		return e, errRemoto(CodiceRemotoConfigurazione, r.ErrConfig)
	}
	if err := r.Config.Controlla(); err != nil {
		return e, errRemoto(CodiceRemotoConfigurazione, err)
	}
	if dest == nil || !dest.Chiavetta() || filepath.Dir(ris.File) != dest.Cartella || ris.ByteCifrati <= 0 ||
		ris.SHA256 != v.SHA256 || ris.ByteCifrati != v.ByteCifrati {
		return e, errRemoto(CodiceRemotoAltro, errors.New("backup: la copia remota vuole l'archivio verificato della chiavetta"))
	}
	firma, err := LeggiChiaveSFTP(r.Dati)
	if err != nil {
		return e, errRemoto(CodiceRemotoChiave, err)
	}
	ctx, annulla := context.WithTimeoutCause(ctx, durataODi(r.TempoMassimo, TempoMassimoCopiaRemota), errTempoMassimo)
	defer annulla()
	s, err := r.collega(ctx, firma)
	if err != nil {
		return e, err
	}
	defer s.chiudi()
	cartella := r.Config.Cartella
	if err := s.cartella(cartella); err != nil {
		return e, err
	}
	e.PendingTolti, err = s.pulisciPending(cartella)
	if err != nil {
		return e, s.causa(errRemoto(CodiceRemotoAltro, err))
	}
	nome := filepath.Base(ris.File)
	finale := path.Join(cartella, nome)
	if err := s.nonCe(finale); err != nil {
		return e, err
	}
	if err := s.nonCe(finale + ".pending"); err != nil {
		return e, err
	}
	if _, ok := s.sftp.HasExtension("statvfs@openssh.com"); ok {
		if st, err := s.sftp.StatVFS(cartella); err == nil && st.Bavail*st.Frsize < uint64(ris.ByteCifrati)+MargineSpazioRemoto {
			return e, errRemoto(CodiceRemotoSpazio, ErrSpazioRemoto)
		}
	}
	if err := s.carica(ctx, dest, nome, finale+".pending", ris); err != nil {
		return e, err
	}
	if err := s.nonCe(finale); err != nil {
		s.togliPending(finale + ".pending")
		return e, err
	}
	if err := s.sftp.Rename(finale+".pending", finale); err != nil {
		s.togliPending(finale + ".pending")
		if _, errL := s.sftp.Lstat(finale); errL == nil {
			return e, errRemoto(CodiceRemotoEsistente, ErrRemotoEsistente)
		}
		return e, s.causa(errRemoto(CodiceRemotoCaricamento, fmt.Errorf("rinomina sul server remoto: %w", err)))
	}
	e.Archivio = finale
	catturato, err := time.Parse(db.FormatoIstante, ris.CatturatoIl)
	if err != nil {
		return e, errRemoto(CodiceRemotoAltro, fmt.Errorf("ora di cattura: %w", err))
	}
	riga := db.VerificaBackup{Riuscita: true, Destinazione: db.DestinazioneSFTP, Archivio: finale, SHA256: ris.SHA256,
		Byte: ris.ByteCifrati, CatturatoIl: catturato, Conteggi: v.Conteggi, MediaRovinati: v.MediaRovinati}
	fi, err := s.sftp.Lstat(finale)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != ris.ByteCifrati {
		// Il nome finale c'è ma non è l'archivio (server che tronca o perde il file dopo la rinomina): l'abbiamo appena
		// creato noi, con il lucchetto dei backup, quindi si toglie subito, riguardandolo prima (file normale con la
		// dimensione appena vista). Se non si toglie, una riga 'fallita' con i byte VISTI sul server (non quelli
		// dell'archivio) lo fa riconoscere dalla rotazione remota, che lo toglie quando esce dalla finestra dei
		// giornalieri; senza riga resterebbe per sempre.
		errCtl := errRemoto(CodiceRemotoControllo, fmt.Errorf("%w: il file sul server dopo la rinomina non è l'archivio", ErrControlloRemoto))
		if err != nil || !fi.Mode().IsRegular() {
			return e, errors.Join(s.causa(errCtl), err)
		}
		if s.togliTroncato(finale, fi) {
			e.Archivio = ""
			return e, errCtl
		}
		if fi.Size() <= 0 {
			return e, errCtl // il registro non accetta 0 byte: resta da togliere a mano (lo dice il motivo)
		}
		riga.Riuscita, riga.Byte = false, fi.Size()
		riga.Errore = fmt.Sprintf("dopo la rinomina sul server %d byte invece di %d: file da togliere", fi.Size(), ris.ByteCifrati)
		riga.VerificatoIl = adesso.Adesso()
		ctxScrivi, annullaScrivi := staccato(ctx)
		errReg := db.RegistraVerificaBackup(ctxScrivi, d, riga)
		annullaScrivi()
		return e, errors.Join(s.causa(errCtl), errReg)
	}
	riga.VerificatoIl = adesso.Adesso()
	ctxScrivi, annullaScrivi := staccato(ctx)
	err = db.RegistraVerificaBackup(ctxScrivi, d, riga)
	annullaScrivi()
	if err != nil {
		return e, errRemoto(CodiceRemotoRegistro, err)
	}
	e.Registrato = true
	e.Rotazione, err = s.ruota(ctx, d, cartella, finale, ris, adesso.Adesso())
	if err != nil {
		return e, s.causa(errRemoto(CodiceRemotoRotazione, err))
	}
	return e, nil
}

// nonCe vuole che sul server non ci sia niente con il nome p: mai sovrascrivere.
func (s *sessione) nonCe(p string) error {
	_, err := s.sftp.Lstat(p)
	switch {
	case err == nil:
		return errRemoto(CodiceRemotoEsistente, ErrRemotoEsistente)
	case errors.Is(err, fs.ErrNotExist):
		return nil
	}
	return s.causa(errRemoto(CodiceRemotoRete, fmt.Errorf("server remoto: %w", err)))
}

// togliTroncato toglie il nome finale appena rinominato che sul server non è l'archivio, se è ancora il file visto
// (file normale, stessa dimensione e ora di modifica). Dice se l'ha tolto.
func (s *sessione) togliTroncato(p string, visto os.FileInfo) bool {
	if s.sorv.scattato() != nil {
		return false
	}
	fi, err := s.sftp.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != visto.Size() || !fi.ModTime().Equal(visto.ModTime()) {
		return false
	}
	return s.sftp.Remove(p) == nil
}

// togliPending toglie il .pending di un caricamento non riuscito, se la connessione c'è ancora; altrimenti lo toglie
// la pulizia della copia seguente.
func (s *sessione) togliPending(p string) {
	if s.sorv.scattato() == nil {
		_ = s.sftp.Remove(p) // se non va, lo toglie pulisciPending la notte dopo
	}
}

// carica manda l'archivio nome della chiavetta dest in pending (creato con O_EXCL) e controlla byte e SHA-256 dei
// byte mandati, e la dimensione del .pending sul server. La lettura dalla chiavetta gira in una goroutine che scrive
// in una pipe: se la chiavetta si blocca, il sorvegliante chiude la pipe e la copia finisce lo stesso.
func (s *sessione) carica(ctx context.Context, dest *Destinazione, nome, pending string, ris Risultato) error {
	f, err := s.sftp.OpenFile(pending, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		if _, errL := s.sftp.Lstat(pending); errL == nil {
			return errRemoto(CodiceRemotoEsistente, ErrRemotoEsistente)
		}
		return s.causa(errRemoto(CodiceRemotoCaricamento, fmt.Errorf("creazione sul server remoto: %w", err)))
	}
	pr, pw := io.Pipe()
	s.sorv.conLettura(pr)
	go leggiArchivio(dest, nome, ris.ByteCifrati, pw)
	h := sha256.New()
	n, errCopia := f.ReadFrom(&io.LimitedReader{R: io.TeeReader(pr, h), N: ris.ByteCifrati})
	// La goroutine della lettura non resta ferma su una pipe che nessuno legge più.
	pr.CloseWithError(errors.New("caricamento finito"))
	errChiudi := f.Close()
	if err := errors.Join(errCopia, errChiudi); err != nil {
		s.togliPending(pending)
		if ctx.Err() != nil && s.sorv.scattato() == nil {
			return ctx.Err()
		}
		return s.causa(errRemoto(CodiceRemotoCaricamento, fmt.Errorf("caricamento sul server remoto: %w", err)))
	}
	if n != ris.ByteCifrati || hex.EncodeToString(h.Sum(nil)) != ris.SHA256 {
		s.togliPending(pending)
		return errRemoto(CodiceRemotoControllo, fmt.Errorf("%w: mandati %d byte con un altro SHA-256", ErrControlloRemoto, n))
	}
	fi, err := s.sftp.Lstat(pending)
	if err != nil {
		s.togliPending(pending)
		return s.causa(errRemoto(CodiceRemotoControllo, fmt.Errorf("%w: %w", ErrControlloRemoto, err)))
	}
	if !fi.Mode().IsRegular() || fi.Size() != ris.ByteCifrati {
		s.togliPending(pending)
		return errRemoto(CodiceRemotoControllo, fmt.Errorf("%w: %d byte sul server invece di %d", ErrControlloRemoto, fi.Size(), ris.ByteCifrati))
	}
	return nil
}

// leggiArchivio apre l'archivio nella cartella bloccata della chiavetta (openat, senza seguire collegamenti), vuole un
// file normale di quei byte e li scrive tutti in w; l'esito chiude w.
func leggiArchivio(dest *Destinazione, nome string, byte int64, w *io.PipeWriter) {
	f, err := apriNome(dest.d, nome)
	if err != nil {
		w.CloseWithError(fmt.Errorf("archivio sulla chiavetta: %w", err))
		return
	}
	defer func() { _ = f.Close() }() // sola lettura
	fi, err := f.Stat()
	if err == nil && (!fi.Mode().IsRegular() || fi.Size() != byte) {
		err = fmt.Errorf("l'archivio sulla chiavetta non è più quello verificato (%d byte)", fi.Size())
	}
	if err == nil {
		var r io.Reader = f
		if lettoreArchivioPerTest != nil {
			r = lettoreArchivioPerTest(r)
		}
		_, err = io.CopyN(w, r, byte)
	}
	w.CloseWithError(err)
}

// pulisciPending toglie dalla cartella remota i .pending rimasti da un caricamento interrotto: solo i nomi esatti,
// solo file normali. Con il lucchetto dei backup preso nessun altro backup di questo apparecchio carica qui.
func (s *sessione) pulisciPending(cartella string) ([]string, error) {
	voci, err := s.sftp.ReadDir(cartella)
	if err != nil {
		return nil, fmt.Errorf("elenco della cartella remota: %w", err)
	}
	var tolti []string
	for _, fi := range ordinate(voci) {
		if _, ok := istanteDelNome(rePending, fi.Name()); !ok || !fi.Mode().IsRegular() {
			continue
		}
		if err := s.sftp.Remove(path.Join(cartella, fi.Name())); err != nil {
			return tolti, fmt.Errorf("pulizia dei .pending remoti: %w", err)
		}
		tolti = append(tolti, fi.Name())
	}
	return tolti, nil
}

func ordinate(voci []os.FileInfo) []os.FileInfo {
	sort.Slice(voci, func(i, j int) bool { return voci[i].Name() < voci[j].Name() })
	return voci
}

// ruota toglie dalla cartella remota gli archivi che D-114 non tiene, con le regole di Destinazione.Ruota: parte
// solo se l'ultima riga «sftp» 'ok' del registro è la copia appena fatta (percorso, SHA-256, byte); guarda solo i
// nomi esatti e i file normali; cancella solo archivi con una riga «sftp» del registro con gli stessi byte, nella
// cartella o nella stessa vista dall'altra porta della Storage Box (db.StessaCartellaRemota, slice 133c); un nome
// registrato nel futuro o più nuovo della copia ferma tutto; la copia appena fatta non si toglie mai; prima di ogni
// Remove riguarda il nome (tipo, byte, ora di modifica).
func (s *sessione) ruota(ctx context.Context, q *db.DB, cartella, nuovo string, ris Risultato, adesso time.Time) (EsitoRotazione, error) {
	if path.Dir(nuovo) != cartella {
		return EsitoRotazione{}, fmt.Errorf("%w: la copia non sta nella cartella remota", ErrRotazione)
	}
	ultimo, ok, err := db.UltimoVerificatoRemoto(ctx, q.Lettura)
	if err != nil {
		return EsitoRotazione{}, errors.Join(ErrRotazione, err)
	}
	if !ok || ultimo.Archivio != nuovo || ultimo.SHA256 != ris.SHA256 || ultimo.Byte != ris.ByteCifrati {
		return EsitoRotazione{}, fmt.Errorf("%w: il registro non dice che la copia appena fatta è l'ultima copia remota", ErrRotazione)
	}
	tNuovo, ok := istanteDelNome(reArchivio, path.Base(nuovo))
	if !ok {
		return EsitoRotazione{}, fmt.Errorf("%w: nome della copia", ErrRotazione)
	}
	righe, err := db.ArchiviRemotiNellaCartella(ctx, q.Lettura, cartella)
	if err != nil {
		return EsitoRotazione{}, errors.Join(ErrRotazione, err)
	}
	voci, err := s.sftp.ReadDir(cartella)
	if err != nil {
		return EsitoRotazione{}, errors.Join(ErrRotazione, err)
	}
	var e EsitoRotazione
	var aa []archivioInCartella
	visti := map[string]os.FileInfo{}
	trovato := false
	for _, fi := range ordinate(voci) {
		n := fi.Name()
		t, ok := istanteDelNome(reArchivio, n)
		if !ok {
			continue
		}
		if !fi.Mode().IsRegular() {
			e.Lasciati = append(e.Lasciati, n)
			continue
		}
		a := archivioInCartella{voce: voceCartella{nome: n, normale: true, byte: fi.Size()}, t: t}
		p := path.Join(cartella, n)
		// righe sono già della cartella (o della stessa vista dall'altra porta, db.StessaCartellaRemota): basta il
		// nome. Così un archivio caricato dalla porta 22 («/backup/…») si riconosce dalla 23 («/home/backup/…»).
		for _, r := range righe {
			if path.Base(r.Archivio) == n && r.Byte == fi.Size() {
				a.registrato = true
				a.verificato = a.verificato || r.Riuscita
			}
		}
		if t.After(adesso) || t.After(tNuovo) {
			if a.registrato {
				return EsitoRotazione{}, fmt.Errorf("%w: %s è più nuovo della copia appena fatta (orologio?)", ErrRotazione, n)
			}
			e.Lasciati = append(e.Lasciati, n)
			continue
		}
		if p == nuovo {
			if !a.verificato || fi.Size() != ris.ByteCifrati {
				return EsitoRotazione{}, fmt.Errorf("%w: la copia sul server non è quella registrata", ErrRotazione)
			}
			trovato = true
		}
		visti[n] = fi
		aa = append(aa, a)
	}
	if !trovato {
		return EsitoRotazione{}, fmt.Errorf("%w: la copia appena fatta non è nella cartella remota", ErrRotazione)
	}
	sort.Slice(aa, func(i, j int) bool { return aa[i].t.After(aa[j].t) })
	tieni := daTenere(aa, fusoRotazione)
	tieni[path.Base(nuovo)] = true
	var togliere []archivioInCartella
	for _, a := range aa {
		switch {
		case tieni[a.voce.nome]:
			e.Tenuti = append(e.Tenuti, a.voce.nome)
		case !a.registrato:
			e.Lasciati = append(e.Lasciati, a.voce.nome)
		default:
			togliere = append(togliere, a)
		}
	}
	for _, a := range togliere {
		if err := ctx.Err(); err != nil {
			return e, errors.Join(ErrRotazione, err)
		}
		if primaDiTogliereRemoto != nil {
			primaDiTogliereRemoto(a.voce.nome)
		}
		p := path.Join(cartella, a.voce.nome)
		prima := visti[a.voce.nome]
		fi, err := s.sftp.Lstat(p)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() != prima.Size() || !fi.ModTime().Equal(prima.ModTime()) {
			return e, fmt.Errorf("%w: %s è cambiato sul server mentre si ruotava", ErrRotazione, a.voce.nome)
		}
		if err := s.sftp.Remove(p); err != nil {
			return e, errors.Join(ErrRotazione, err)
		}
		e.Tolti = append(e.Tolti, a.voce.nome)
	}
	return e, nil
}

// lettoreArchivioPerTest, solo nei test, sostituisce la lettura dell'archivio dalla chiavetta (una chiavetta appesa).
var lettoreArchivioPerTest func(io.Reader) io.Reader

// primaDiTogliereRemoto, solo nei test, gira prima di riguardare un archivio remoto da togliere.
var primaDiTogliereRemoto func(nome string)
