// Package sftpfinto è il server SFTP finto dei test della copia remota dei backup (slice 128): SSH e SFTP veri
// (golang.org/x/crypto/ssh e il server a richieste di github.com/pkg/sftp) nello stesso processo, su un socket Unix
// in una cartella temporanea: niente porte, niente rete, niente server veri. (Non net.Pipe: senza buffer, client e
// server SSH scrivono insieme la riga della versione e restano fermi tutti e due.) I file del server stanno in una cartella temporanea del test
// (Radice). I Guasti simulano spazio pieno, connessione che cade a metà, server che non risponde più, rinomina che
// sovrascrive (un server che non fa come OpenSSH) e lo spazio libero dichiarato. Solo per i test: nessun pacchetto
// del programma lo importa.
package sftpfinto

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"sync"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Guasti sono i comportamenti del server; zero: un server che funziona.
type Guasti struct {
	// SpazioFinito: dopo questi byte scritti in un file le scritture danno errore (0: mai).
	SpazioFinito int64
	// CadeDopo: dopo questi byte scritti in un file la connessione si chiude dal lato del server (0: mai).
	CadeDopo int64
	// FermoAllaScrittura: la prima scrittura non risponde finché la connessione non si chiude.
	FermoAllaScrittura bool
	// RinominaSovrascrive: la rinomina schiaccia un file che c'è (come posix-rename).
	RinominaSovrascrive bool
	// Liberi: lo spazio libero dichiarato con statvfs@openssh.com (0: estensione non supportata).
	Liberi uint64
	// PerdeDopo: i byte oltre questo punto di un file si dicono scritti ma non si scrivono (un server che perde
	// dati senza dirlo; 0: mai).
	PerdeDopo int64
	// TroncaDopoRinomina: dopo una rinomina il file di arrivo si accorcia a metà (un server che perde dati).
	TroncaDopoRinomina bool
	// RifiutaRemove: ogni Remove dà errore.
	RifiutaRemove bool
	// AlLstat gira a ogni Lstat, con il percorso del server, dopo aver guardato il file (la risposta è quella di prima).
	AlLstat func(p string)
	// DopoLista gira dopo ogni elenco di una cartella; AllaPrimaScrittura alla prima scrittura di ogni file.
	DopoLista, AllaPrimaScrittura func()
}

// Server è il server finto.
type Server struct {
	// Radice è la cartella del test con i file del server.
	Radice string
	// Chiave è la chiave del server (ed25519).
	Chiave ssh.Signer

	mu          sync.Mutex
	g           Guasti
	autorizzata ssh.PublicKey
	arrivata    chan struct{}
	unaVolta    sync.Once
	tolti       []string
	conns       []net.Conn
	ln          net.Listener
}

// Nuovo è un server finto con una chiave nuova, nessuna chiave autorizzata e una radice vuota; le connessioni si
// chiudono alla fine del test.
func Nuovo(t testing.TB) *Server {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	// Un percorso corto: un socket Unix sta in 108 byte.
	dir, err := os.MkdirTemp("", "sftpfinto-")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", filepath.Join(dir, "s"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Radice: t.TempDir(), Chiave: k, arrivata: make(chan struct{}), ln: ln}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.conns = append(s.conns, c)
			s.mu.Unlock()
			go s.servi(c)
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		s.mu.Lock()
		for _, c := range s.conns {
			_ = c.Close()
		}
		s.mu.Unlock()
		_ = os.RemoveAll(dir)
	})
	return s
}

// Autorizza fa entrare la chiave pubblica k (una sola).
func (s *Server) Autorizza(k ssh.PublicKey) {
	s.mu.Lock()
	s.autorizzata = k
	s.mu.Unlock()
}

// Imposta cambia i guasti.
func (s *Server) Imposta(g Guasti) {
	s.mu.Lock()
	s.g = g
	s.mu.Unlock()
}

func (s *Server) guasti() Guasti {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.g
}

// ScritturaArrivata si chiude alla prima scrittura di un file.
func (s *Server) ScritturaArrivata() <-chan struct{} { return s.arrivata }

// Impronta è l'impronta SHA-256 della chiave del server, come ssh-keygen -l.
func (s *Server) Impronta() string { return ssh.FingerprintSHA256(s.Chiave.PublicKey()) }

// Tolti sono i percorsi del server tolti con Remove, in ordine.
func (s *Server) Tolti() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.tolti...)
}

// Locale è il file della cartella del test che corrisponde al percorso p del server.
func (s *Server) Locale(p string) string {
	return filepath.Join(s.Radice, filepath.FromSlash(path.Clean("/"+p)))
}

// Componi è la Componi della copia remota: si collega al socket del server, qualunque sia l'indirizzo.
func (s *Server) Componi(ctx context.Context, _ string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", s.ln.Addr().String())
}

// connServer è il lato del server: chiusa si chiude quando la connessione cade (una lettura che fallisce).
type connServer struct {
	net.Conn
	chiusa chan struct{}
	una    sync.Once
}

func (c *connServer) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if err != nil {
		c.una.Do(func() { close(c.chiusa) })
	}
	return n, err
}

func (s *Server) servi(nc net.Conn) {
	conn := &connServer{Conn: nc, chiusa: make(chan struct{})}
	defer func() { _ = conn.Close() }()
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		s.mu.Lock()
		aut := s.autorizzata
		s.mu.Unlock()
		if aut != nil && bytes.Equal(k.Marshal(), aut.Marshal()) {
			return nil, nil
		}
		return nil, errors.New("chiave non autorizzata")
	}}
	cfg.AddHostKey(s.Chiave)
	_, canali, richieste, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(richieste)
	for nc := range canali {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "no")
			continue
		}
		ch, reqs, err := nc.Accept()
		if err != nil {
			return
		}
		go func() {
			for r := range reqs {
				ok := r.Type == "subsystem" && len(r.Payload) >= 4 && string(r.Payload[4:]) == "sftp"
				_ = r.Reply(ok, nil)
				if ok {
					h := gestore{s: s, conn: conn}
					rs := sftp.NewRequestServer(ch, sftp.Handlers{FileGet: h, FilePut: h, FileCmd: h, FileList: h})
					_ = rs.Serve()
					_ = rs.Close()
					return
				}
			}
		}()
	}
}

// gestore risponde alle richieste SFTP con i file della radice.
type gestore struct {
	s    *Server
	conn *connServer
}

func (h gestore) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	return os.Open(h.s.Locale(r.Filepath))
}

func (h gestore) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	pf := r.Pflags()
	fl := os.O_WRONLY
	if pf.Creat {
		fl |= os.O_CREATE
	}
	if pf.Excl {
		fl |= os.O_EXCL
	}
	if pf.Trunc {
		fl |= os.O_TRUNC
	}
	f, err := os.OpenFile(h.s.Locale(r.Filepath), fl, 0o600)
	if err != nil {
		return nil, err
	}
	return &scrittore{f: f, h: h}, nil
}

// scrittore è un file del server con i guasti.
type scrittore struct {
	f       *os.File
	h       gestore
	mu      sync.Mutex
	scritti int64
	prima   sync.Once
}

func (w *scrittore) WriteAt(p []byte, off int64) (int, error) {
	s := w.h.s
	g := s.guasti()
	w.prima.Do(func() {
		s.unaVolta.Do(func() { close(s.arrivata) })
		if g.AllaPrimaScrittura != nil {
			g.AllaPrimaScrittura()
		}
	})
	if g.FermoAllaScrittura {
		<-w.h.conn.chiusa // il server non risponde più: aspetta che il client chiuda
		return 0, io.ErrUnexpectedEOF
	}
	w.mu.Lock()
	w.scritti += int64(len(p))
	n := w.scritti
	w.mu.Unlock()
	if g.SpazioFinito > 0 && n > g.SpazioFinito {
		return 0, errors.New("no space left on device")
	}
	if g.CadeDopo > 0 && n > g.CadeDopo {
		_ = w.h.conn.Close()
		return 0, io.ErrUnexpectedEOF
	}
	if g.PerdeDopo > 0 && off+int64(len(p)) > g.PerdeDopo {
		if off >= g.PerdeDopo {
			return len(p), nil
		}
		if _, err := w.f.WriteAt(p[:g.PerdeDopo-off], off); err != nil {
			return 0, err
		}
		return len(p), nil
	}
	return w.f.WriteAt(p, off)
}

func (w *scrittore) Close() error { return w.f.Close() }

func (h gestore) Filecmd(r *sftp.Request) error {
	p := h.s.Locale(r.Filepath)
	switch r.Method {
	case "Rename":
		t := h.s.Locale(r.Target)
		g := h.s.guasti()
		if _, err := os.Lstat(t); err == nil && !g.RinominaSovrascrive {
			return os.ErrExist
		}
		if err := os.Rename(p, t); err != nil {
			return err
		}
		if g.TroncaDopoRinomina {
			if fi, err := os.Stat(t); err == nil {
				return os.Truncate(t, fi.Size()/2)
			}
		}
		return nil
	case "Remove":
		if h.s.guasti().RifiutaRemove {
			return errors.New("permesso negato")
		}
		h.s.mu.Lock()
		h.s.tolti = append(h.s.tolti, r.Filepath)
		h.s.mu.Unlock()
		return os.Remove(p)
	case "Setstat":
		return nil
	case "Mkdir":
		return os.Mkdir(p, 0o700)
	}
	return errors.New("non supportato")
}

// StatVFS dà lo spazio libero dei Guasti (senza: non supportato).
func (h gestore) StatVFS(*sftp.Request) (*sftp.StatVFS, error) {
	liberi := h.s.guasti().Liberi
	if liberi == 0 {
		return nil, errors.New("non supportato")
	}
	return &sftp.StatVFS{Bsize: 4096, Frsize: 4096, Bavail: liberi / 4096, Bfree: liberi / 4096, Blocks: 1 << 30}, nil
}

func (h gestore) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	p := h.s.Locale(r.Filepath)
	switch r.Method {
	case "List":
		voci, err := os.ReadDir(p)
		if err != nil {
			return nil, err
		}
		var v []os.FileInfo
		for _, e := range voci {
			fi, err := e.Info()
			if err != nil {
				return nil, err
			}
			v = append(v, fi)
		}
		if dopo := h.s.guasti().DopoLista; dopo != nil {
			dopo()
		}
		return elenco(v), nil
	case "Stat":
		fi, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		return elenco{fi}, nil
	}
	return nil, errors.New("non supportato")
}

// Lstat non segue i collegamenti, come OpenSSH.
func (h gestore) Lstat(r *sftp.Request) (sftp.ListerAt, error) {
	fi, err := os.Lstat(h.s.Locale(r.Filepath))
	if f := h.s.guasti().AlLstat; f != nil {
		f(r.Filepath) // dopo aver guardato: quello che fa si vede solo dalla richiesta dopo
	}
	if err != nil {
		return nil, err
	}
	return elenco{fi}, nil
}

// RealPath dà il percorso pulito e assoluto (la radice del server è la radice del sottoaccount).
func (h gestore) RealPath(p string) (string, error) { return path.Clean("/" + p), nil }

type elenco []os.FileInfo

func (e elenco) ListAt(v []os.FileInfo, off int64) (int, error) {
	if off >= int64(len(e)) {
		return 0, io.EOF
	}
	n := copy(v, e[off:])
	if n < len(v) {
		return n, io.EOF
	}
	return n, nil
}
