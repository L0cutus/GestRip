package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"strconv"
	"sync"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// L'esito di una busta appena creata e il suo link pubblico, mostrati una volta (RIP-H-41;
// decisioni della slice 031). Lo standalone li tiene nello stato del LiveView (flash e
// public_token) e li disegna sulla lista dopo il push_patch. Qui il salvataggio è un POST seguito
// da un redirect a /?esito=<id>: l'identificativo è casuale e opaco, e la voce sta solo in memoria,
// legata a utente e accesso (nonce della sessione). La prima GET di quell'indirizzo la toglie;
// dopo durataEsito non vale più; un riavvio la perde (la busta resta, e dalla scheda se ne potrà
// generare uno nuovo). Ogni scheda ha il suo esito, e il cookie di sessione, leggibile, non ne
// porta niente. Il link non va mai in un indirizzo, in un cookie, nel registro o nell'audit.
//
// Gli invii: il modulo porta un identificativo monouso (campo invio). Lo stesso modulo inviato due
// volte, anche insieme, crea una busta sola: il secondo invio aspetta il primo e porta al suo
// esito. Nello standalone ci pensa phx-submit, che blocca il modulo finché il server risponde.

const (
	// durataEsito è quanto aspetta un esito non ancora mostrato.
	durataEsito = 5 * time.Minute
	// durataInvio è quanto si ricorda un invio andato a buon fine.
	durataInvio = 10 * time.Minute
)

// linkMostrato è il link dell'esito. URL vuoto: il link non si può costruire (manca
// indirizzo_pubblico) e la home dice perché.
type linkMostrato struct {
	URL        string
	Oreficeria bool // «Link dell'oreficeria» invece di «Link cliente»
}

// esitoBusta è quello che la home disegna una volta.
type esitoBusta struct {
	Numero string
	Link   linkMostrato
}

type voceEsito struct {
	utente int64
	nonce  string
	esito  esitoBusta
	scade  time.Time
}

// invioInCorso è un invio del modulo: il primo lo prende, gli altri con lo stesso identificativo
// aspettano fatto e poi leggono esito ("" se il primo non ha creato niente).
type invioInCorso struct {
	fatto chan struct{}
	esito string
	scade time.Time // zero finché il primo non ha finito
}

// depositoEsiti tiene in memoria gli esiti da mostrare e gli invii già fatti.
type depositoEsiti struct {
	mu    sync.Mutex
	o     clock.Orologio
	esiti map[string]voceEsito
	invii map[string]*invioInCorso // chiave: utente, nonce e identificativo dell'invio
}

func nuovoDepositoEsiti(o clock.Orologio) *depositoEsiti {
	return &depositoEsiti{o: o, esiti: map[string]voceEsito{}, invii: map[string]*invioInCorso{}}
}

// casuale è un identificativo di 16 byte casuali in base64url (22 caratteri).
func casuale() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read non fallisce
	return base64.RawURLEncoding.EncodeToString(b)
}

// pulisci toglie le voci scadute; va chiamata con il lucchetto.
func (d *depositoEsiti) pulisci(adesso time.Time) {
	for k, v := range d.esiti {
		if !adesso.Before(v.scade) {
			delete(d.esiti, k)
		}
	}
	for k, v := range d.invii {
		if !v.scade.IsZero() && !adesso.Before(v.scade) {
			delete(d.invii, k)
		}
	}
}

// metti salva l'esito per la sessione (utente, nonce) e dà l'identificativo dell'indirizzo.
func (d *depositoEsiti) metti(utente int64, nonce string, e esitoBusta) string {
	id := casuale()
	d.mu.Lock()
	defer d.mu.Unlock()
	adesso := d.o.Adesso()
	d.pulisci(adesso)
	d.esiti[id] = voceEsito{utente: utente, nonce: nonce, esito: e, scade: adesso.Add(durataEsito)}
	return id
}

// prendi dà l'esito una volta sola, se è della stessa sessione e non è scaduto. Una richiesta di
// un'altra sessione non lo consuma; uno scaduto si toglie.
func (d *depositoEsiti) prendi(id string, utente int64, nonce string) (esitoBusta, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	v, ok := d.esiti[id]
	if !ok {
		return esitoBusta{}, false
	}
	if !d.o.Adesso().Before(v.scade) {
		delete(d.esiti, id)
		return esitoBusta{}, false
	}
	if v.utente != utente || v.nonce != nonce {
		return esitoBusta{}, false
	}
	delete(d.esiti, id)
	return v.esito, true
}

func chiaveInvio(invio string, utente int64, nonce string) string {
	return strconv.FormatInt(utente, 10) + "\x00" + nonce + "\x00" + invio
}

// prenota prende l'invio per questa sessione. primo: tocca a chi chiama creare la busta e poi
// chiudere; altrimenti l'invio c'è già (finito, o da aspettare con aspetta).
func (d *depositoEsiti) prenota(invio string, utente int64, nonce string) (*invioInCorso, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pulisci(d.o.Adesso())
	k := chiaveInvio(invio, utente, nonce)
	if a, ok := d.invii[k]; ok {
		return a, false
	}
	a := &invioInCorso{fatto: make(chan struct{})}
	d.invii[k] = a
	return a, true
}

// chiudi finisce l'invio preso con prenota: con un esito lo ricorda per durataInvio; senza
// (niente creato) lo dimentica, così il modulo corretto si può inviare di nuovo.
func (d *depositoEsiti) chiudi(a *invioInCorso, esito string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	a.esito = esito
	if esito == "" {
		for k, v := range d.invii {
			if v == a {
				delete(d.invii, k)
			}
		}
	} else {
		a.scade = d.o.Adesso().Add(durataInvio)
	}
	close(a.fatto)
}

// aspetta attende che il primo invio finisca e ne dà l'esito ("" se non ha creato niente).
func (d *depositoEsiti) aspetta(ctx context.Context, a *invioInCorso) (string, error) {
	select {
	case <-a.fatto:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return a.esito, nil
}

// quantiEsiti è il numero di esiti in memoria (per i test).
func (d *depositoEsiti) quantiEsiti() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.esiti)
}
