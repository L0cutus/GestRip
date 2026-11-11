package uscita

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// Un server SMTP finto, in processo, su 127.0.0.1 e una porta casuale: parla quanto basta della
// conversazione di Providers.Smtp (EHLO, STARTTLS, AUTH LOGIN, MAIL, RCPT, DATA, QUIT) e annota quello
// che riceve. Nessun server vero, nessuna rete fuori da loopback, dati inventati.

// certificatoDiProva è un certificato autofirmato per 127.0.0.1, generato nel test, e le radici che
// lo accettano.
func certificatoDiProva(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	chiave, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	modello := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "smtp di prova"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, modello, modello, &chiave.PublicKey, chiave)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	radici := x509.NewCertPool()
	radici.AddCert(c)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: chiave, Leaf: c}, radici
}

// serverFinto è il comportamento del server: quello che non è detto risponde come un server che
// accetta tutto.
type serverFinto struct {
	TLSImplicito  bool              // il TLS parte subito (porta 465)
	SenzaStartTLS bool              // STARTTLS risponde 454
	Muto          bool              // non saluta mai
	Utente        string            // le credenziali che accetta (altrimenti 535, con utente e password nel testo)
	Password      string            //
	Risposte      map[string]string // il comando (MAIL, RCPT, DATA, EHLO, saluto) → la risposta al posto di quella buona
	Accettato     string            // la riga dopo il corpo; vuota: «250 2.0.0 Ok: queued as ABC123»
	Spezzato      bool              // saluto ed EHLO su più righe, ognuna scritta da sola

	ln     net.Listener
	cert   tls.Certificate
	Radici *x509.CertPool

	mu        sync.Mutex
	comandi   []string // le righe ricevute, senza \r\n
	dati      string   // quello che è arrivato dopo DATA, terminatore compreso
	authInTLS []bool   // per ogni AUTH: la sessione era cifrata?
	cifrata   bool     // la sessione è passata al TLS (STARTTLS o implicito)
	chiusa    chan struct{}
}

func avviaServerFinto(t *testing.T, s *serverFinto) *serverFinto {
	t.Helper()
	s.cert, s.Radici = certificatoDiProva(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.ln = ln
	s.chiusa = make(chan struct{})
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		defer close(s.chiusa)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		_ = c.SetDeadline(time.Now().Add(20 * time.Second))
		s.conversa(c)
	}()
	return s
}

// porta è la porta su cui ascolta.
func (s *serverFinto) porta() int { return s.ln.Addr().(*net.TCPAddr).Port }

// fine aspetta che la conversazione sia finita (il client ha chiuso o il server ha smesso).
func (s *serverFinto) fine(t *testing.T) {
	t.Helper()
	select {
	case <-s.chiusa:
	case <-time.After(10 * time.Second):
		t.Fatal("la conversazione col server finto non è finita")
	}
}

func (s *serverFinto) ricevuti() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.comandi...)
}

func (s *serverFinto) corpo() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dati
}

func (s *serverFinto) haRicevuto(prefisso string) bool {
	for _, c := range s.ricevuti() {
		if strings.HasPrefix(c, prefisso) {
			return true
		}
	}
	return false
}

func (s *serverFinto) risposta(comando, buona string) string {
	if r, ok := s.Risposte[comando]; ok {
		return r
	}
	return buona
}

func (s *serverFinto) conversa(c net.Conn) {
	if s.TLSImplicito {
		tc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{s.cert}})
		if tc.Handshake() != nil {
			return
		}
		s.mu.Lock()
		s.cifrata = true
		s.mu.Unlock()
		c = tc
	}
	if s.Muto {
		// Tiene la connessione aperta senza dire niente, finché il client non chiude.
		_, _ = c.Read(make([]byte, 1))
		return
	}
	r := bufio.NewReader(c)
	scrivi := func(righe ...string) bool {
		for _, riga := range righe {
			if _, err := c.Write([]byte(riga + "\r\n")); err != nil {
				return false
			}
		}
		return true
	}
	if s.Spezzato {
		scrivi("220-smtp.example.invalid ESMTP", "220-ancora una riga", "220 pronto")
	} else {
		scrivi(s.risposta("saluto", "220 smtp.example.invalid ESMTP"))
	}
	leggi := func() (string, bool) {
		riga, err := r.ReadString('\n')
		if err != nil {
			return "", false
		}
		riga = strings.TrimSuffix(strings.TrimSuffix(riga, "\n"), "\r")
		s.mu.Lock()
		s.comandi = append(s.comandi, riga)
		s.mu.Unlock()
		return riga, true
	}
	for {
		riga, ok := leggi()
		if !ok {
			return
		}
		verbo := strings.ToUpper(strings.SplitN(riga, " ", 2)[0])
		switch {
		case verbo == "EHLO":
			if r, ok := s.Risposte["EHLO"]; ok {
				scrivi(r)
				continue
			}
			if s.Spezzato {
				scrivi("250-smtp.example.invalid", "250-PIPELINING", "250-SIZE 35882577", "250-STARTTLS", "250 AUTH LOGIN")
			} else {
				scrivi("250-smtp.example.invalid\r\n250-STARTTLS\r\n250 AUTH LOGIN PLAIN")
			}
		case verbo == "STARTTLS":
			if s.SenzaStartTLS {
				scrivi("454 4.7.0 TLS not available")
				continue
			}
			scrivi("220 2.0.0 Ready to start TLS")
			tc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{s.cert}})
			if tc.Handshake() != nil {
				return
			}
			c = tc
			r = bufio.NewReader(c)
			s.mu.Lock()
			s.cifrata = true
			s.mu.Unlock()
		case riga == "AUTH LOGIN":
			s.mu.Lock()
			s.authInTLS = append(s.authInTLS, s.cifrata)
			s.mu.Unlock()
			scrivi("334 VXNlcm5hbWU6")
			u, ok := leggi()
			if !ok {
				return
			}
			scrivi("334 UGFzc3dvcmQ6")
			p, ok := leggi()
			if !ok {
				return
			}
			uu, _ := base64.StdEncoding.DecodeString(u)
			pp, _ := base64.StdEncoding.DecodeString(p)
			if string(uu) != s.Utente || string(pp) != s.Password {
				// Un server vero può ripetere quello che ha appena rifiutato: il provider non deve mai riportarlo.
				scrivi("535 5.7.8 Username and Password not accepted for " + string(uu) + " / " + string(pp) + " " + p)
				continue
			}
			scrivi("235 2.7.0 Authentication successful")
		case verbo == "MAIL":
			scrivi(s.risposta("MAIL", "250 2.1.0 Ok"))
		case verbo == "RCPT":
			scrivi(s.risposta("RCPT", "250 2.1.5 Ok"))
		case verbo == "DATA":
			if r, ok := s.Risposte["DATA"]; ok {
				scrivi(r)
				continue
			}
			scrivi("354 End data with <CR><LF>.<CR><LF>")
			var b strings.Builder
			for !strings.HasSuffix(b.String(), "\r\n.\r\n") {
				pezzo, err := r.ReadString('\n')
				b.WriteString(pezzo)
				if err != nil {
					return
				}
			}
			s.mu.Lock()
			s.dati = b.String()
			s.mu.Unlock()
			acc := s.Accettato
			if acc == "" {
				acc = "250 2.0.0 Ok: queued as ABC123"
			}
			scrivi(acc)
		case verbo == "QUIT":
			scrivi("221 2.0.0 Bye")
			return
		default:
			scrivi("502 5.5.2 Error: command not recognized")
		}
	}
}
