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

type chiave struct {
	s Secchio
	k string
}

type voce struct {
	falliti []time.Time // dentro la finestra, il più recente per ultimo
	fino    time.Time   // fine del blocco; zero se non bloccata
}

// Limitatore tiene i conteggi; si usa da più goroutine.
type Limitatore struct {
	o      clock.Orologio
	mu     sync.Mutex
	voci   map[chiave]*voce
	pulito time.Time
}

// Nuovo dà un limitatore vuoto.
func Nuovo(o clock.Orologio) *Limitatore {
	return &Limitatore{o: o, voci: map[chiave]*voce{}, pulito: o.Adesso()}
}

// Bloccato dice quanto manca alla fine del blocco di k nel secchio s; 0 se non è bloccata.
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

// Fallito conta un fallito di k nel secchio s e dice se è quello che fa partire il blocco (allora
// i falliti ripartono da zero). Durante un blocco non conta niente: le rotte rifiutano prima, e
// un fallito arrivato insieme non allunga il blocco né ne fa partire un altro.
func (l *Limitatore) Fallito(s Secchio, k string) bool {
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
	recenti := v.falliti[:0]
	for _, t := range v.falliti {
		if t.After(adesso.Add(-r.finestra)) {
			recenti = append(recenti, t)
		}
	}
	v.falliti = append(recenti, adesso)
	if len(v.falliti) >= r.falliti {
		v.falliti, v.fino = nil, adesso.Add(r.blocco)
		return true
	}
	return false
}

// Azzera dimentica i falliti di k nel secchio s (un login riuscito).
func (l *Limitatore) Azzera(s Secchio, k string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.voci, chiave{s, k})
}

// pulisci toglie, al massimo una volta al minuto, le chiavi senza blocco in corso e senza falliti
// recenti (la pulizia ogni minuto dello standalone, fatta qui alla prima richiesta utile).
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
		for _, t := range v.falliti {
			if t.After(limite) {
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
