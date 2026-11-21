// Package sidecar avvia e sorveglia i programmi separati dell'IA (PIANO §1.1 e §1.8): il server
// dell'estrazione dei campi e, se configurata, la trascrizione. Ogni sidecar è un processo figlio
// che ascolta su 127.0.0.1 a una porta alta; il supervisore ne controlla la salute con una GET su
// un percorso (di solito /health), lo riavvia con attese crescenti se cade o smette di rispondere
// e lo ferma, con tutto il suo gruppo di processi, quando GestRip si chiude.
//
// Il supervisore è generico (comando, argomenti, porta, percorso di salute): il sidecar
// dell'estrazione potrebbe non essere llama-server (risposta A del piano della T4.10). Un sidecar
// che non parte non ferma GestRip: il supervisore lo scrive nel registro e riprova.
//
// Riservatezza (RIP-H-47): l'uscita standard e d'errore del figlio vanno perse (/dev/null), perché
// un server del modello può scriverci le richieste, cioè i dettati. Nel registro vanno solo il nome
// del sidecar, il pid, l'esito (codice d'uscita o segnale, porta occupata, salute mancata) e le
// attese: mai gli argomenti, l'ambiente o il corpo di una risposta.
package sidecar

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/L0cutus/gestrip/internal/clock"
)

// SegnapostoPorta negli argomenti diventa la porta del sidecar.
const SegnapostoPorta = "{porta}"

// SaluteDiDefault è il percorso di salute di llama-server (e di quasi tutti i server del genere).
const SaluteDiDefault = "/health"

// PortaMinima: il sidecar ascolta su una porta alta, mai su una che vuole i privilegi di root.
const PortaMinima = 1024

// Comando descrive un sidecar.
type Comando struct {
	// Nome va solo nel registro: «estrazione», «trascrizione».
	Nome string
	// Programma è il percorso assoluto dell'eseguibile (niente ricerca nel PATH).
	Programma string
	// Argomenti, senza il programma; SegnapostoPorta diventa la porta.
	Argomenti []string
	// Porta su 127.0.0.1, da PortaMinima a 65535.
	Porta int
	// Salute è il percorso della GET di salute; vuoto vale SaluteDiDefault.
	Salute string
	// Ambiente del figlio, intero: nil o vuoto vuol dire nessuna variabile (come il figlio delle
	// foto). L'ambiente di GestRip non passa mai al sidecar.
	Ambiente []string
}

// Tempi del supervisore. Un campo a zero prende il valore di TempiDiDefault.
type Tempi struct {
	// Avvio è il tempo massimo fra la partenza del figlio e la prima risposta 200 di salute
	// (llama-server risponde 503 mentre carica il modello).
	Avvio time.Duration
	// PassoAvvio è l'intervallo fra due controlli di salute durante l'avvio.
	PassoAvvio time.Duration
	// Controllo è l'intervallo fra due controlli di salute a sidecar pronto.
	Controllo time.Duration
	// Risposta è il tempo massimo di una GET di salute.
	Risposta time.Duration
	// Malato è quanti controlli falliti di fila (a sidecar pronto) fanno ripartire il figlio.
	Malato int
	// AttesaMinima e AttesaMassima: la prima attesa prima di un riavvio e il tetto delle attese,
	// che raddoppiano a ogni caduta.
	AttesaMinima, AttesaMassima time.Duration
	// Stabile: un figlio rimasto pronto almeno così a lungo riporta l'attesa al minimo.
	Stabile time.Duration
	// Arresto è quanto si aspetta dopo SIGTERM prima di SIGKILL.
	Arresto time.Duration
}

// TempiDiDefault sono i tempi dell'apparecchio.
var TempiDiDefault = Tempi{
	Avvio:         3 * time.Minute,
	PassoAvvio:    500 * time.Millisecond,
	Controllo:     10 * time.Second,
	Risposta:      5 * time.Second,
	Malato:        3,
	AttesaMinima:  time.Second,
	AttesaMassima: 5 * time.Minute,
	Stabile:       10 * time.Minute,
	Arresto:       10 * time.Second,
}

// conDefault riempie i campi a zero.
func (t Tempi) conDefault() Tempi {
	d := TempiDiDefault
	if t.Avvio <= 0 {
		t.Avvio = d.Avvio
	}
	if t.PassoAvvio <= 0 {
		t.PassoAvvio = d.PassoAvvio
	}
	if t.Controllo <= 0 {
		t.Controllo = d.Controllo
	}
	if t.Risposta <= 0 {
		t.Risposta = d.Risposta
	}
	if t.Malato <= 0 {
		t.Malato = d.Malato
	}
	if t.AttesaMinima <= 0 {
		t.AttesaMinima = d.AttesaMinima
	}
	if t.AttesaMassima <= 0 {
		t.AttesaMassima = d.AttesaMassima
	}
	t.AttesaMassima = max(t.AttesaMassima, t.AttesaMinima)
	if t.Stabile <= 0 {
		t.Stabile = d.Stabile
	}
	if t.Arresto <= 0 {
		t.Arresto = d.Arresto
	}
	return t
}

// Controlla dice se il comando è usabile: nome, programma assoluto, SegnapostoPorta in almeno un
// argomento (un sidecar che non riceve la porta ascolterebbe altrove), porta alta, percorso di
// salute che comincia con «/», senza host, spazi, query o frammento.
func (c Comando) Controlla() error {
	if strings.TrimSpace(c.Nome) == "" {
		return errors.New("sidecar: manca il nome")
	}
	if !filepath.IsAbs(c.Programma) {
		return fmt.Errorf("sidecar %s: il programma deve essere un percorso assoluto", c.Nome)
	}
	if !slices.ContainsFunc(c.Argomenti, func(a string) bool { return strings.Contains(a, SegnapostoPorta) }) {
		return fmt.Errorf("sidecar %s: serve %s in almeno un argomento", c.Nome, SegnapostoPorta)
	}
	if c.Porta < PortaMinima || c.Porta > 65535 {
		return fmt.Errorf("sidecar %s: la porta deve essere fra %d e 65535", c.Nome, PortaMinima)
	}
	if s := c.Salute; s != "" && !percorsoDiSalute(s) {
		return fmt.Errorf("sidecar %s: il percorso di salute deve cominciare con «/», senza spazi, «?» o «#»", c.Nome)
	}
	return nil
}

// percorsoDiSalute: un percorso assoluto e basta (url.Parse senza schema, host, utente, query,
// frammento), senza spazi né caratteri di controllo.
func percorsoDiSalute(s string) bool {
	if !strings.HasPrefix(s, "/") || strings.ContainsAny(s, " \t?#") || strings.ContainsFunc(s, unicode.IsControl) {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "" && u.Host == "" && u.User == nil && u.Opaque == "" &&
		u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && strings.HasPrefix(u.Path, "/") && !strings.HasPrefix(u.Path, "//")
}

// Indirizzo è la radice HTTP del sidecar, per chi ci parla.
func (c Comando) Indirizzo() string {
	return "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(c.Porta))
}

// argomenti sono gli argomenti con la porta al posto del segnaposto.
func (c Comando) argomenti() []string {
	a := make([]string, 0, len(c.Argomenti)+1)
	a = append(a, c.Programma)
	for _, s := range c.Argomenti {
		a = append(a, strings.ReplaceAll(s, SegnapostoPorta, strconv.Itoa(c.Porta)))
	}
	return a
}

// Fase del sidecar, per Stato.
type Fase string

const (
	FaseSpento  Fase = "spento"  // il supervisore non gira (prima di Lavoro o dopo)
	FaseAvvio   Fase = "avvio"   // figlio partito, salute non ancora 200
	FasePronto  Fase = "pronto"  // salute 200
	FaseAttesa  Fase = "attesa"  // figlio caduto o mai partito: si aspetta prima di riprovare
	FaseArresto Fase = "arresto" // GestRip si chiude: si ferma il figlio
)

// Stato è una fotografia del supervisore.
type Stato struct {
	Fase Fase
	// Pid del figlio vivo (0 se non c'è).
	Pid int
	// Avvii: quante volte il figlio è partito (Start riuscito).
	Avvii int
	// Attese: le attese prima dei riavvii, nell'ordine (al più le ultime 64).
	Attese []time.Duration
}

// Supervisore tiene in vita un sidecar. Si crea con Nuovo e gira con Lavoro.
type Supervisore struct {
	c        Comando
	t        Tempi
	registro *slog.Logger
	aspetta  func(context.Context, time.Duration) bool
	salute   *http.Client
	// ascolto controlla che sulla porta ascolti solo il gruppo del figlio, su 127.0.0.1.
	ascolto func(porta, gruppo int) (bool, string)

	mu    sync.Mutex
	stato Stato
}

// Nuovo prepara il supervisore di c; tempi a zero prendono i default, registro nil scarta.
func Nuovo(c Comando, t Tempi, registro *slog.Logger) (*Supervisore, error) {
	if err := c.Controlla(); err != nil {
		return nil, err
	}
	if c.Salute == "" {
		c.Salute = SaluteDiDefault
	}
	c.Argomenti = append([]string(nil), c.Argomenti...)
	c.Ambiente = append([]string{}, c.Ambiente...)
	if registro == nil {
		registro = slog.New(slog.DiscardHandler)
	}
	t = t.conDefault()
	return &Supervisore{
		c:        c,
		t:        t,
		registro: registro.With("sidecar", c.Nome),
		aspetta:  clock.Aspetta,
		ascolto:  ascoltoDelGruppo,
		salute: &http.Client{
			// Niente proxy dall'ambiente, niente redirect, niente keep-alive: solo 127.0.0.1.
			Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			Timeout:       t.Risposta,
		},
		stato: Stato{Fase: FaseSpento},
	}, nil
}

// ConAttesa cambia l'attesa fra un riavvio e l'altro (i test registrano le durate).
func (s *Supervisore) ConAttesa(f func(context.Context, time.Duration) bool) *Supervisore {
	s.aspetta = f
	return s
}

// Indirizzo è la radice HTTP del sidecar.
func (s *Supervisore) Indirizzo() string { return s.c.Indirizzo() }

// Stato dà la fotografia attuale.
func (s *Supervisore) Stato() Stato {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.stato
	st.Attese = append([]time.Duration(nil), st.Attese...)
	return st
}

func (s *Supervisore) imposta(f func(*Stato)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(&s.stato)
}

// Lavoro tiene in vita il sidecar fino alla fine di ctx, poi ferma il figlio (SIGTERM al gruppo,
// SIGKILL dopo Tempi.Arresto) e torna solo a figlio raccolto. Non torna mai un errore: un sidecar
// che non parte si riprova con attese crescenti, e GestRip resta su.
func (s *Supervisore) Lavoro(ctx context.Context) {
	defer s.imposta(func(st *Stato) { st.Fase, st.Pid = FaseSpento, 0 })
	attesa := s.t.AttesaMinima
	for ctx.Err() == nil {
		stabile := s.giro(ctx)
		if ctx.Err() != nil {
			return
		}
		if stabile {
			attesa = s.t.AttesaMinima
		}
		s.imposta(func(st *Stato) {
			st.Fase, st.Pid = FaseAttesa, 0
			st.Attese = append(st.Attese, attesa)
			if len(st.Attese) > 64 {
				st.Attese = st.Attese[len(st.Attese)-64:]
			}
		})
		s.registro.Warn("sidecar: nuovo avvio fra poco", "attesa", attesa.String())
		if !s.aspetta(ctx, attesa) {
			return
		}
		attesa = min(2*attesa, s.t.AttesaMassima)
	}
}

// giro fa partire il figlio una volta e lo segue finché cade, smette di rispondere o ctx finisce;
// torna sempre a figlio raccolto e gruppo ucciso. Dice se il figlio è stato pronto almeno
// Tempi.Stabile.
func (s *Supervisore) giro(ctx context.Context) (stabile bool) {
	if ctx.Err() != nil {
		return false
	}
	// Una porta già occupata vuol dire un altro server lì (un sidecar rimasto, un altro servizio):
	// la salute risponderebbe per lui, quindi il figlio non parte.
	if err := portaLibera(s.c.Porta); err != nil {
		s.registro.Warn("sidecar: porta occupata, non parto", "porta", s.c.Porta)
		return false
	}
	f, err := avvia(s.c)
	if err != nil {
		// Solo il genere: il testo dell'errore di exec contiene il percorso.
		s.registro.Warn("sidecar: il programma non parte", "errore", genereAvvio(err))
		return false
	}
	s.imposta(func(st *Stato) { st.Fase, st.Pid = FaseAvvio, f.pid; st.Avvii++ })
	s.registro.Info("sidecar: avviato", "pid", f.pid)
	defer func() {
		s.imposta(func(st *Stato) { st.Fase = FaseArresto })
		f.ferma(s.t.Arresto)
		s.registro.Info("sidecar: fermato", "pid", f.pid, "uscita", f.esito())
	}()
	// ctxF finisce quando finisce ctx o quando il figlio esce da solo.
	ctxF, annulla := context.WithCancel(ctx)
	defer annulla()
	go func() {
		select {
		case <-f.uscito:
			annulla()
		case <-ctxF.Done():
		}
	}()

	inizio := clock.Parti()
	avvisato := false
	for !s.sanoESuo(ctxF, f, &avvisato) {
		if ctxF.Err() != nil {
			s.uscitaDa(f, "durante l'avvio")
			return false
		}
		if inizio.Trascorso() >= s.t.Avvio {
			s.registro.Warn("sidecar: nessuna risposta di salute entro il tempo d'avvio", "pid", f.pid, "tempo", s.t.Avvio.String())
			return false
		}
		if !clock.Aspetta(ctxF, s.t.PassoAvvio) {
			s.uscitaDa(f, "durante l'avvio")
			return false
		}
	}
	pronto := clock.Parti()
	s.imposta(func(st *Stato) { st.Fase = FasePronto })
	s.registro.Info("sidecar: pronto", "pid", f.pid, "avvio", inizio.Trascorso().Round(time.Millisecond).String())
	falliti := 0
	for {
		if !clock.Aspetta(ctxF, s.t.Controllo) {
			s.uscitaDa(f, "")
			return pronto.Trascorso() >= s.t.Stabile
		}
		if s.sanoESuo(ctxF, f, &avvisato) {
			falliti = 0
			continue
		}
		if ctxF.Err() != nil {
			s.uscitaDa(f, "")
			return pronto.Trascorso() >= s.t.Stabile
		}
		falliti++
		if falliti >= s.t.Malato {
			s.registro.Warn("sidecar: non risponde alla salute, lo riavvio", "pid", f.pid, "controlli_falliti", falliti)
			return pronto.Trascorso() >= s.t.Stabile
		}
	}
}

// uscitaDa scrive nel registro il figlio uscito da solo (non quando si chiude GestRip).
func (s *Supervisore) uscitaDa(f *figlio, quando string) {
	select {
	case <-f.uscito:
		s.registro.Warn("sidecar: il figlio è uscito"+suffisso(quando), "pid", f.pid, "uscita", f.esito())
	default:
	}
}

func suffisso(q string) string {
	if q == "" {
		return ""
	}
	return " " + q
}

// sanoESuo: la salute dà 200 e sulla porta ascolta solo il gruppo del figlio, su 127.0.0.1. Un 200
// che viene da un altro processo (o da un figlio che ascolta anche fuori da 127.0.0.1) non vale; lo
// dice il registro una volta per giro, con il solo motivo.
func (s *Supervisore) sanoESuo(ctx context.Context, f *figlio, avvisato *bool) bool {
	if !s.sano(ctx) {
		return false
	}
	ok, motivo := s.ascolto(s.c.Porta, f.pid)
	if !ok && !*avvisato {
		*avvisato = true
		s.registro.Warn("sidecar: la salute non viene dal figlio", "pid", f.pid, "motivo", motivo)
	}
	return ok
}

// sano fa una GET di salute: solo il 200 vale. Il corpo si legge (poco) e si butta.
func (s *Supervisore) sano(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.c.Indirizzo()+s.c.Salute, nil)
	if err != nil {
		return false
	}
	r, err := s.salute.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 64<<10))
	_ = r.Body.Close()
	return r.StatusCode == http.StatusOK
}

// portaLibera prova ad aprire la porta su 127.0.0.1 e la richiude.
func portaLibera(porta int) error {
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(porta)))
	if err != nil {
		return err
	}
	return l.Close()
}

// genereAvvio riduce l'errore di Start a un genere senza percorsi.
func genereAvvio(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "non trovato"
	case errors.Is(err, fs.ErrPermission):
		return "permesso negato"
	default:
		return "errore"
	}
}
