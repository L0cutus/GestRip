// Package ratelimit conta i tentativi falliti per indirizzo (CLI-21, ACC-20), come AttemptLimiter
// dello standalone (slice 208).
//
// Si contano **solo i falliti** (un codice /stato sbagliato, un token che non c'è, un login
// rifiutato): molti clienti condividono l'indirizzo dell'operatore mobile, e i loro link buoni non
// devono mai fermarsi a vicenda (scelta di Stefano del 29/10/2026).
//
//	secchio          chiave               falliti  finestra  blocco
//	Corto            indirizzo            10       15 min    15 min   il codice di /stato
//	Token            indirizzo            30       1 min     15 min   le rotte a token lungo
//	LoginIndirizzo   indirizzo            20       15 min    15 min   qualunque account
//	LoginAccount     indirizzo + email     5       15 min    15 min   un account
//
// Slice 219: un posto si prenota prima della verifica (Prenota) e l'esito lo sistema: Fallito lo
// rende un fallito, Rilascia lo restituisce, così contano solo i falliti come prima. Compromesso
// dichiarato dello standalone: più richieste del limite nello stesso istante dallo stesso
// indirizzo ricevono il rifiuto per quell'istante anche se funzionerebbero.
//
// In memoria: un riavvio azzera tutto, come nello standalone (una installazione, un processo). Il
// tempo viene dall'orologio del negozio, non da un orologio monotono come nello standalone: un
// salto dell'ora di sistema allunga o accorcia un blocco in corso, mai oltre il salto.
package ratelimit

import (
	"sync"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// Secchio è una famiglia di tentativi con le sue regole.
type Secchio int

const (
	Corto          Secchio = iota // /stato/<busta>/<anno> e tutto sotto
	Token                         // /r, /lotto, /artigiano, /w, /g (non le foto)
	LoginIndirizzo                // POST /login, per indirizzo
	LoginAccount                  // POST /login, per indirizzo e account
)

func (s Secchio) String() string {
	return [...]string{"short", "token", "login_ip", "login_user"}[s]
}

type regola struct {
	falliti          int
	finestra, blocco time.Duration
}

var regole = [...]regola{
	Corto:          {10, 15 * time.Minute, 15 * time.Minute},
	Token:          {30, time.Minute, 15 * time.Minute},
	LoginIndirizzo: {20, 15 * time.Minute, 15 * time.Minute},
	LoginAccount:   {5, 15 * time.Minute, 15 * time.Minute},
}

// finestraPiuLunga: oltre questa un fallito non conta in nessun secchio (la pulizia).
const finestraPiuLunga = 15 * time.Minute

// attesaInCorso è l'attesa detta a chi trova i posti pieni di verifiche ancora in corso: rispondono
// in un attimo (@running_wait dello standalone).
const attesaInCorso = time.Second

type chiave struct {
	s Secchio
	k string
}

// Biglietto è un posto prenotato prima di una verifica (slice 219); 0 vuol dire nessun posto.
type Biglietto uint64

// posto è un fallito (biglietto 0) o una verifica ancora in corso.
type posto struct {
	t time.Time
	b Biglietto
}

type voce struct {
	posti []posto   // falliti e verifiche in corso
	fino  time.Time // fine del blocco; zero se non bloccata
}

// Limitatore tiene i conteggi; si usa da più goroutine. Un solo lucchetto: prenotare, contare e
// restituire sono atomici, come le chiamate al processo dei contatori dello standalone.
type Limitatore struct {
	o      clock.Orologio
	mu     sync.Mutex
	voci   map[chiave]*voce
	pulito time.Time
	ultimo Biglietto
}

// Nuovo dà un limitatore vuoto.
func Nuovo(o clock.Orologio) *Limitatore {
	return &Limitatore{o: o, voci: map[chiave]*voce{}, pulito: o.Adesso()}
}

// Bloccato dice quanto manca alla fine del blocco di k nel secchio s; 0 se non è bloccata. I posti
// pieni di verifiche in corso non sono un blocco.
func (l *Limitatore) Bloccato(s Secchio, k string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	adesso := l.o.Adesso()
	l.pulisci(adesso)
	if v := l.voci[chiave{s, k}]; v != nil && v.fino.After(adesso) {
		return v.fino.Sub(adesso)
	}
	return 0
}

// Prenota prende un posto di k **prima** della verifica (AttemptLimiter.reserve/3): così le
// richieste mandate tutte insieme non passano tutte il controllo prima che il primo fallito
// conti. Ogni chiave ha tanti posti quanto il limite, presi dai falliti nella finestra e dalle
// verifiche in corso. Dà il biglietto, oppure 0 e l'attesa: quella del blocco, o un attimo se i
// posti sono pieni di verifiche in corso. Chi prenota deve poi chiamare Fallito o Rilascia.
func (l *Limitatore) Prenota(s Secchio, k string) (Biglietto, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	adesso := l.o.Adesso()
	l.pulisci(adesso)
	r := regole[s]
	v := l.voci[chiave{s, k}]
	if v == nil {
		v = &voce{}
		l.voci[chiave{s, k}] = v
	}
	if v.fino.After(adesso) {
		return 0, v.fino.Sub(adesso)
	}
	v.posti = nellaFinestra(v.posti, adesso, r.finestra)
	if len(v.posti) >= r.falliti {
		// I soli falliti al limite avrebbero già bloccato: c'è una verifica in corso.
		return 0, attesaInCorso
	}
	l.ultimo++
	v.posti = append(v.posti, posto{adesso, l.ultimo})
	return l.ultimo, 0
}

// Fallito: la verifica ha rifiutato, il posto di b diventa un fallito (b = 0: un fallito senza
// prenotazione). Dice se è quello che fa partire il blocco (allora i posti ripartono da zero).
// Durante un blocco non conta niente: un fallito arrivato insieme non allunga il blocco né ne fa
// partire un altro. Contano per il blocco solo i falliti, mai le verifiche in corso.
func (l *Limitatore) Fallito(s Secchio, k string, b Biglietto) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	adesso := l.o.Adesso()
	l.pulisci(adesso)
	r := regole[s]
	v := l.voci[chiave{s, k}]
	if v == nil {
		v = &voce{}
		l.voci[chiave{s, k}] = v
	}
	if v.fino.After(adesso) {
		return false
	}
	v.posti = append(senza(nellaFinestra(v.posti, adesso, r.finestra), b), posto{t: adesso})
	falliti := 0
	for _, p := range v.posti {
		if p.b == 0 {
			falliti++
		}
	}
	if falliti >= r.falliti {
		v.posti, v.fino = nil, adesso.Add(r.blocco)
		return true
	}
	return false
}

// Rilascia: la verifica è passata, il posto di b torna libero e non conta niente.
func (l *Limitatore) Rilascia(s Secchio, k string, b Biglietto) {
	if b == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if v := l.voci[chiave{s, k}]; v != nil {
		v.posti = senza(v.posti, b)
	}
}

// Azzera dimentica i falliti e i posti di k nel secchio s (un login riuscito).
func (l *Limitatore) Azzera(s Secchio, k string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.voci, chiave{s, k})
}

func nellaFinestra(posti []posto, adesso time.Time, finestra time.Duration) []posto {
	out := posti[:0]
	for _, p := range posti {
		if p.t.After(adesso.Add(-finestra)) {
			out = append(out, p)
		}
	}
	return out
}

// senza toglie il posto del biglietto b (nessuno se b è 0).
func senza(posti []posto, b Biglietto) []posto {
	if b == 0 {
		return posti
	}
	for i, p := range posti {
		if p.b == b {
			return append(posti[:i], posti[i+1:]...)
		}
	}
	return posti
}

// pulisci toglie, al massimo una volta al minuto, le chiavi senza blocco in corso e senza posti
// recenti (la pulizia ogni minuto dello standalone, fatta qui alla prima richiesta utile). Un
// posto mai sistemato esce con la finestra, senza aver mai contato per un blocco.
func (l *Limitatore) pulisci(adesso time.Time) {
	// Con l'orologio tornato indietro si pulisce subito e si riparte dall'ora nuova.
	if d := adesso.Sub(l.pulito); d >= 0 && d < time.Minute {
		return
	}
	l.pulito = adesso
	limite := adesso.Add(-finestraPiuLunga)
	for k, v := range l.voci {
		if v.fino.After(adesso) {
			continue
		}
		vecchia := true
		for _, p := range v.posti {
			if p.t.After(limite) {
				vecchia = false
				break
			}
		}
		if vecchia {
			delete(l.voci, k)
		}
	}
}

// quante conta le chiavi tenute (per i test della pulizia).
func (l *Limitatore) quante() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.voci)
}
