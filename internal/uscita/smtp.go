package uscita

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// SMTP è il provider email della coda (slice 069, CLI-43), come Outgoing.Providers.Smtp e
// Providers.Smtp.Tcp dello standalone a go-baseline: una conversazione SMTP per riga della coda col
// server del negozio. Solo la libreria standard (net, crypto/tls): net/smtp non serve, perché la
// conversazione è quella dello standalone, passo per passo.
//
// Quello che non fa, come lo standalone:
//   - non scende mai in chiaro: se STARTTLS è configurato e il server lo rifiuta, il tentativo finisce
//     lì; e non manda mai AUTH LOGIN su una sessione non cifrata, qualunque cosa dica la configurazione;
//   - non lascia scrivere un'intestazione ad altri: il nome del mittente e l'oggetto non semplici
//     viaggiano come parole codificate (RFC 2047), il testo in base64;
//   - non mette mai il testo del server nell'errore: un rifiuto è «smtp:<codice>» e basta, perché un
//     server può ripetere l'indirizzo o la credenziale appena rifiutata; un errore del trasporto è
//     «smtp:transport:<genere>»;
//   - non dice mai «consegnato»: il 250 dopo il corpo è il server che ha preso i byte, ed è il
//     riferimento.
//
// Lo standalone non separa i rifiuti permanenti (5xx) dai temporanei (4xx): ognuno è un tentativo della
// coda, fino al quinto. Il Go fa lo stesso.
type SMTP struct {
	d *db.DB
	o clock.Orologio
	// tempo è @timeout: 15 secondi per collegarsi, per il TLS e per ogni risposta.
	tempo time.Duration
	// radici sono i certificati di cui fidarsi; nil: quelli del sistema (cacerts_get/0). Solo i test le
	// cambiano, con il certificato di prova.
	radici *x509.CertPool
	// durata è la scadenza complessiva di una conversazione (2 minuti, review della 069): un server
	// che risponde un po' alla volta, ognuna entro tempo, non la tiene aperta per sempre.
	durata time.Duration
}

// NuovoSMTP è il provider sul database della configurazione (letta a ogni tentativo) e l'orologio
// dell'intestazione Date.
func NuovoSMTP(d *db.DB, o clock.Orologio) *SMTP {
	return &SMTP{d: d, o: o, tempo: 15 * time.Second, durata: 2 * time.Minute}
}

// Attivo dice se il canale email è acceso e configurato (EmailAttiva): la coda legge le righe email
// solo allora, e Accoda le rifiuta altrimenti.
func (s *SMTP) Attivo(ctx context.Context) (bool, error) {
	return EmailAttiva(ctx, s.d.Lettura)
}

// Consegna è attempt/1 per l'email: la configurazione di adesso; senza (o col canale spento)
// «not_configured» senza aprire niente; poi la conversazione. La password non esce mai: il riferimento e
// il motivo si puliscono anche qui (scrub/2), prima di tornare alla coda.
func (s *SMTP) Consegna(ctx context.Context, m Messaggio) (string, error) {
	c, err := LeggiSMTP(ctx, s.d.Lettura)
	if err != nil {
		return "", err
	}
	acceso, err := emailAccesa(ctx, s.d.Lettura)
	if err != nil {
		return "", err
	}
	if !c.Usabile() || !acceso {
		return "", Rifiuto(erroreNonConfigurato)
	}
	rif, err := s.consegnaCon(ctx, m, c)
	if p := c.Password.valore(); p != "" {
		rif = strings.ReplaceAll(rif, p, passwordNascosta)
		var r Rifiuto
		if errors.As(err, &r) {
			err = Rifiuto(strings.ReplaceAll(string(r), p, passwordNascosta))
		}
	}
	return rif, err
}

// sessione è la connessione di adesso: in chiaro o cifrata (il socket «{:tcp, _}» o «{:tls, _}»).
type sessione struct {
	conn    net.Conn
	r       *bufio.Reader
	cifrata bool
	tempo   time.Duration
	fine    time.Time // la scadenza complessiva della conversazione
}

// scadenza è il momento entro cui deve finire la prossima lettura o scrittura: adesso più il tempo
// massimo, ma mai dopo la scadenza complessiva.
func (ss *sessione) scadenza() time.Time {
	t := clock.Sistema{}.Adesso().Add(ss.tempo)
	if ss.fine.Before(t) {
		return ss.fine
	}
	return t
}

// lunghezzaRiga è il massimo di una riga di risposta: oltre è un errore di protocollo.
const lunghezzaRiga = 64 * 1024

// maxContinuazioni è il massimo delle righe «NNN-» di una risposta: oltre è un errore di protocollo.
const maxContinuazioni = 512

// consegnaCon è deliver/2: collegarsi, la conversazione, chiudere la connessione su cui è finita (quella
// cifrata dopo STARTTLS: il TLS manda il suo close_notify).
func (s *SMTP) consegnaCon(ctx context.Context, m Messaggio, c ConfigSMTP) (string, error) {
	ss, err := s.collega(ctx, c)
	if err != nil {
		return "", rifiutoTrasporto(err)
	}
	defer func() { _ = ss.conn.Close() }()
	// Il contesto annullato (serve che si ferma) chiude la connessione: la lettura in corso finisce
	// subito. Basta chiudere quella di partenza: il TLS di STARTTLS ci sta sopra.
	grezza := ss.conn
	stop := context.AfterFunc(ctx, func() { _ = grezza.Close() })
	defer stop()
	if err := s.apriSessione(ctx, ss, c); err != nil {
		return "", err
	}
	return s.manda(ss, m, c)
}

func host(c ConfigSMTP) string {
	if c.Server == "" {
		return "localhost"
	}
	return c.Server
}

func (s *SMTP) configTLS(c ConfigSMTP) *tls.Config {
	// Il nome scritto dal titolare è quello dell'SNI e quello che il certificato deve avere; le radici
	// del sistema (o quelle dei test).
	return &tls.Config{ServerName: host(c), RootCAs: s.radici, MinVersion: tls.VersionTLS12}
}

func (s *SMTP) collega(ctx context.Context, c ConfigSMTP) (*sessione, error) {
	fine := clock.Sistema{}.Adesso().Add(s.durata)
	dialer := &net.Dialer{Timeout: s.tempo}
	indirizzo := net.JoinHostPort(host(c), strconv.Itoa(c.Porta))
	if c.Sicurezza == SicurezzaTLS {
		td := &tls.Dialer{NetDialer: dialer, Config: s.configTLS(c)}
		conn, err := td.DialContext(ctx, "tcp", indirizzo)
		if err != nil {
			return nil, err
		}
		return &sessione{conn: conn, r: bufio.NewReaderSize(conn, 4096), cifrata: true, tempo: s.tempo, fine: fine}, nil
	}
	conn, err := dialer.DialContext(ctx, "tcp", indirizzo)
	if err != nil {
		return nil, err
	}
	return &sessione{conn: conn, r: bufio.NewReaderSize(conn, 4096), tempo: s.tempo, fine: fine}, nil
}

// apriSessione è open_session/3: il saluto 220, l'EHLO (con STARTTLS se configurato) e il login.
func (s *SMTP) apriSessione(ctx context.Context, ss *sessione, c ConfigSMTP) error {
	if _, err := ss.aspetta(220); err != nil {
		return err
	}
	if err := ss.comando("EHLO " + nomeEhlo(c)); err != nil {
		return err
	}
	if _, err := ss.aspetta(250); err != nil {
		return err
	}
	if c.Sicurezza == SicurezzaStartTLS {
		if err := ss.comando("STARTTLS"); err != nil {
			return err
		}
		if _, err := ss.aspetta(220); err != nil {
			return err
		}
		if err := s.cifra(ctx, ss, c); err != nil {
			return err
		}
		if err := ss.comando("EHLO " + nomeEhlo(c)); err != nil {
			return err
		}
		if _, err := ss.aspetta(250); err != nil {
			return err
		}
	}
	return ss.autentica(c)
}

// cifra è upgrade/3: il TLS sulla stessa connessione; quello che il server aveva mandato prima in
// chiaro si butta (il lettore riparte da zero sulla connessione cifrata).
func (s *SMTP) cifra(ctx context.Context, ss *sessione, c ConfigSMTP) error {
	tc := tls.Client(ss.conn, s.configTLS(c))
	ctxTLS, ferma := context.WithDeadline(ctx, ss.scadenza())
	defer ferma()
	if err := tc.HandshakeContext(ctxTLS); err != nil {
		return rifiutoTrasporto(err)
	}
	ss.conn, ss.r, ss.cifrata = tc, bufio.NewReaderSize(tc, 4096), true
	return nil
}

// autentica è authenticate/3: AUTH LOGIN solo su una sessione cifrata e solo con utente e password; una
// password su una sessione mai cifrata è «smtp:auth_requires_tls» senza mandare niente; senza le due
// credenziali, anonimo.
func (ss *sessione) autentica(c ConfigSMTP) error {
	password := c.Password.valore()
	switch {
	case ss.cifrata && c.Utente != "" && password != "":
		passi := []struct {
			riga   string
			codice int
		}{
			{"AUTH LOGIN", 334},
			{base64.StdEncoding.EncodeToString([]byte(c.Utente)), 334},
			{base64.StdEncoding.EncodeToString([]byte(password)), 235},
		}
		for _, p := range passi {
			if err := ss.comando(p.riga); err != nil {
				return err
			}
			if _, err := ss.aspetta(p.codice); err != nil {
				return err
			}
		}
		return nil
	case !ss.cifrata && password != "":
		return Rifiuto("smtp:auth_requires_tls")
	}
	return nil
}

// manda è send_message/4: MAIL, RCPT, DATA, il messaggio, il 250 che è il riferimento, poi QUIT (senza
// aspettare la risposta).
func (s *SMTP) manda(ss *sessione, m Messaggio, c ConfigSMTP) (string, error) {
	passi := []struct {
		riga   string
		codice int
	}{
		{"MAIL FROM:<" + c.Mittente + ">", 250},
		{"RCPT TO:<" + m.Indirizzo + ">", 250},
		{"DATA", 354},
	}
	for _, p := range passi {
		if err := ss.comando(p.riga); err != nil {
			return "", err
		}
		if _, err := ss.aspetta(p.codice); err != nil {
			return "", err
		}
	}
	if err := ss.scrivi(s.contenuto(m, c)); err != nil {
		return "", err
	}
	accettato, err := ss.aspetta(250)
	if err != nil {
		return "", err
	}
	_ = ss.comando("QUIT") // la risposta non serve: il messaggio è già stato preso
	return accettato, nil
}

func (ss *sessione) comando(riga string) error { return ss.scrivi(riga + "\r\n") }

func (ss *sessione) scrivi(dati string) error {
	_ = ss.conn.SetWriteDeadline(ss.scadenza())
	if _, err := io.WriteString(ss.conn, dati); err != nil {
		return rifiutoTrasporto(err)
	}
	return nil
}

// aspetta è expect/3: la risposta intera; il codice atteso dà la riga finale, un altro «smtp:<codice>».
// Più di maxContinuazioni righe «NNN-» sono un errore di protocollo.
func (ss *sessione) aspetta(atteso int) (string, error) {
	for continuazioni := 0; ; continuazioni++ {
		if continuazioni > maxContinuazioni {
			return "", rifiutoTrasporto(errProtocollo)
		}
		_ = ss.conn.SetReadDeadline(ss.scadenza())
		riga, err := ss.leggiRiga()
		if err != nil {
			return "", rifiutoTrasporto(err)
		}
		codice, finale, testo, ok := classifica(riga)
		if !ok {
			return "", rifiutoTrasporto(errProtocollo)
		}
		if !finale {
			continue
		}
		if codice != atteso {
			return "", Rifiuto("smtp:" + strconv.Itoa(codice))
		}
		return testo, nil
	}
}

var errProtocollo = errors.New("risposta SMTP non valida")

// leggiRiga è take_line/1: fino a «\n», senza il «\r» finale.
func (ss *sessione) leggiRiga() (string, error) {
	var b []byte
	for {
		pezzo, err := ss.r.ReadSlice('\n')
		b = append(b, pezzo...)
		if len(b) > lunghezzaRiga {
			return "", errProtocollo
		}
		switch {
		case err == nil:
			return strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r"), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		default:
			return "", err
		}
	}
}

// classifica è classify/1: tre cifre e «-» (continua) o « » o niente (finale); il testo della riga
// finale è codice, spazio e testo, senza spazi ai bordi.
func classifica(riga string) (codice int, finale bool, testo string, ok bool) {
	if len(riga) < 3 {
		return 0, false, "", false
	}
	n, err := strconv.Atoi(riga[:3])
	if err != nil {
		return 0, false, "", false
	}
	if len(riga) == 3 {
		return n, true, riga[:3], true
	}
	switch riga[3] {
	case '-':
		return n, false, "", true
	case ' ':
		return n, true, strings.TrimSpace(riga[:3] + " " + riga[4:]), true
	}
	return 0, false, "", false
}

// rifiutoTrasporto è transport_error/1: un Rifiuto già fatto resta com'è; ogni altro errore diventa
// «smtp:transport:<genere>» con un genere fisso, mai il testo dell'errore (che può avere l'indirizzo del
// server). Lo standalone scrive inspect/1 del motivo di :inet o :ssl (per esempio «:timeout»); qui i
// generi sono gli stessi dove ci sono, e «:tls» per ogni errore del TLS.
func rifiutoTrasporto(err error) error {
	var r Rifiuto
	if errors.As(err, &r) {
		return r
	}
	return Rifiuto("smtp:transport:" + genereTrasporto(err))
}

func genereTrasporto(err error) string {
	var (
		ne   net.Error
		dns  *net.DNSError
		cert *tls.CertificateVerificationError
		ua   x509.UnknownAuthorityError
		hn   x509.HostnameError
		ci   x509.CertificateInvalidError
		al   tls.AlertError
		rh   tls.RecordHeaderError
		errn syscall.Errno
	)
	switch {
	case errors.Is(err, errProtocollo):
		return ":protocol"
	case errors.Is(err, os.ErrDeadlineExceeded), errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return ":timeout"
	case errors.As(err, &cert), errors.As(err, &ua), errors.As(err, &hn), errors.As(err, &ci), errors.As(err, &al), errors.As(err, &rh):
		return ":tls"
	case errors.As(err, &dns):
		return ":nxdomain"
	case errors.As(err, &errn):
		switch errn {
		case syscall.ECONNREFUSED:
			return ":econnrefused"
		case syscall.ECONNRESET:
			return ":econnreset"
		case syscall.EHOSTUNREACH:
			return ":ehostunreach"
		case syscall.ENETUNREACH:
			return ":enetunreach"
		case syscall.EPIPE:
			return ":epipe"
		}
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, net.ErrClosed):
		return ":closed"
	case errors.Is(err, context.Canceled):
		return ":closed"
	}
	return ":error"
}

// I nomi dei giorni e dei mesi di rfc2822/1 (in inglese, come vuole l'RFC).
const formatoData = "Mon, 02 Jan 2006 15:04:05 +0000"

// contenuto è payload/2: le intestazioni dello standalone (nessun Message-ID: lo mette il server), una
// riga vuota, il testo in base64 a righe di 76 (senza punti: il terminatore non è mai ambiguo e
// nessuna riga supera i 998 byte) e il terminatore.
func (s *SMTP) contenuto(m Messaggio, c ConfigSMTP) string {
	oggetto := ""
	if m.Oggetto != nil {
		oggetto = parolaCodificata(*m.Oggetto)
	}
	intestazioni := strings.Join([]string{
		"From: " + intestazioneMittente(c),
		"To: " + m.Indirizzo,
		"Subject: " + oggetto,
		"Date: " + s.o.Adesso().UTC().Format(formatoData),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: base64",
	}, "\r\n")
	return intestazioni + "\r\n\r\n" + aRighe(base64.StdEncoding.EncodeToString([]byte(m.Testo)), 76) + "\r\n.\r\n"
}

// nomeSemplice è @plain_name.
var nomeSemplice = regexp.MustCompile(`^[A-Za-z0-9 .'\-]+$`)

// intestazioneMittente è from_header/1: «nome <indirizzo>» o il solo indirizzo senza nome; il nome che
// non è semplice passa da parolaCodificata.
func intestazioneMittente(c ConfigSMTP) string {
	nome := strings.TrimSpace(c.NomeMittente)
	switch {
	case nome == "":
		return c.Mittente
	case nomeSemplice.MatchString(nome):
		return nome + " <" + c.Mittente + ">"
	}
	return parolaCodificata(nome) + " <" + c.Mittente + ">"
}

// parolaCodificata è encode_word/1: un testo di soli caratteri ASCII stampabili (32-126) resta com'è,
// ogni altro diventa «=?utf-8?B?…?=» (un a capo non può quindi aprire un'intestazione).
func parolaCodificata(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] < 32 || s[i] > 126 {
			return "=?utf-8?B?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
		}
	}
	return s
}

// aRighe è wrap/2: pezzi di n caratteri uniti da «\r\n».
func aRighe(s string, n int) string {
	var b strings.Builder
	for len(s) > n {
		b.WriteString(s[:n])
		b.WriteString("\r\n")
		s = s[n:]
	}
	b.WriteString(s)
	return b.String()
}

// nomeEhlo è ehlo_name/1: il dominio del mittente se l'indirizzo ha una sola «@» con qualcosa dopo,
// altrimenti «localhost».
func nomeEhlo(c ConfigSMTP) string {
	parti := strings.Split(c.Mittente, "@")
	if len(parti) == 2 && parti[1] != "" {
		return parti[1]
	}
	return "localhost"
}
