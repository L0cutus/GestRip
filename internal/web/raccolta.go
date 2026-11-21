package web

import (
	"sync"
	"time"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// La cattura dell'estrazione per la raccolta dei dettati (slice 113; RIP-H-48). Lo standalone la tiene nello stato del
// LiveView (assist_capture): la mette un'estrazione riuscita, la sostituisce la prossima, la passa e la toglie il
// salvataggio riuscito della busta; una pagina chiusa la perde. Qui la pagina non ha uno stato fra una richiesta e
// l'altra, quindi la cattura sta in memoria sul server, legata a:
//   - la sessione (utente e nonce dell'accesso), come gli esiti di esiti.go: un'altra sessione, anche dello stesso
//     utente, non la vede;
//   - la chiave del modulo (intake_key, nuova.go), che resta la stessa finché il modulo non è salvato e cambia con la
//     busta dopo: è «questa pagina», anche senza JavaScript (l'estrazione senza i campi del modulo dà una chiave nuova,
//     e il modulo che torna la porta).
//
// Il client manda solo la chiave: testo, proposta e modello non passano mai dal browser, quindi non si possono
// inventare né cambiare; una chiave sbagliata non trova niente. Una cattura vale durataCattura (una pagina lasciata
// aperta più a lungo salva la busta senza riga); un riavvio la perde (la busta si salva lo stesso, senza riga).
// «Genera busta» la prende (la toglie) prima del salvataggio, così due invii della stessa pagina non la usano tutti e due;
// se la busta non si salva la rimette, se nel frattempo non ne è arrivata una nuova.
//
// Riservatezza: la cattura sta solo qui e nella riga; mai in un registro, in un audit, in un cookie o nella pagina.

const (
	// durataCattura è quanto vale una cattura non ancora salvata.
	durataCattura = 2 * time.Hour
	// maxCatture è il massimo di catture in memoria per tutto il processo: a deposito pieno (dopo aver tolto le scadute)
	// una cattura nuova non si tiene, e quella busta si salverà senza riga.
	maxCatture = 1000
	// maxCattureSessione è il massimo di catture di una sessione (utente e nonce): oltre, si toglie la più vecchia di
	// quella sessione, così una sessione sola non riempie il deposito di tutti (come maxSegniSessione di segni.go).
	maxCattureSessione = 16
)

type voceCattura struct {
	utente int64
	nonce  string
	d      buste.Dettato
	scade  time.Time
}

// depositoCatture tiene le catture per sessione e chiave del modulo.
type depositoCatture struct {
	mu   sync.Mutex
	o    clock.Orologio
	voci map[string]voceCattura // chiave: chiaveInvio(chiave del modulo, utente, nonce)
}

func nuovoDepositoCatture(o clock.Orologio) *depositoCatture {
	return &depositoCatture{o: o, voci: map[string]voceCattura{}}
}

// pulisci toglie le catture scadute; va chiamata con il lucchetto.
func (c *depositoCatture) pulisci(adesso time.Time) {
	for k, v := range c.voci {
		if !adesso.Before(v.scade) {
			delete(c.voci, k)
		}
	}
}

// metti tiene la cattura d dell'estrazione riuscita della pagina (chiave del modulo) per la sessione, al posto di
// quella di prima.
func (c *depositoCatture) metti(utente int64, nonce, chiave string, d buste.Dettato) {
	c.mu.Lock()
	defer c.mu.Unlock()
	adesso := c.o.Adesso()
	c.pulisci(adesso)
	c.tieni(chiaveInvio(chiave, utente, nonce), voceCattura{utente: utente, nonce: nonce, d: d, scade: adesso.Add(durataCattura)})
}

// tieni mette v alla chiave k nei tetti del deposito: al posto di quella che c'è già, altrimenti dopo aver tolto la più
// vecchia della sessione se ne ha maxCattureSessione, e solo se il deposito ha posto. Va chiamata con il lucchetto.
func (c *depositoCatture) tieni(k string, v voceCattura) {
	if _, ok := c.voci[k]; !ok {
		suoi, vecchia := 0, ""
		for kk, x := range c.voci {
			if x.utente == v.utente && x.nonce == v.nonce {
				suoi++
				if vecchia == "" || x.scade.Before(c.voci[vecchia].scade) {
					vecchia = kk
				}
			}
		}
		if suoi >= maxCattureSessione {
			delete(c.voci, vecchia)
		}
		if len(c.voci) >= maxCatture {
			return
		}
	}
	c.voci[k] = v
}

// prendi toglie e dà la cattura della pagina per la sessione, con la sua scadenza, se c'è e non è scaduta; nil
// altrimenti.
func (c *depositoCatture) prendi(utente int64, nonce, chiave string) (*buste.Dettato, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	adesso := c.o.Adesso()
	c.pulisci(adesso)
	k := chiaveInvio(chiave, utente, nonce)
	v, ok := c.voci[k]
	if !ok {
		return nil, time.Time{}
	}
	delete(c.voci, k)
	return &v.d, v.scade
}

// rimetti ridà alla pagina la cattura presa per un salvataggio non riuscito, con la sua scadenza di prima, se nel
// frattempo un'estrazione non ne ha messa una nuova. Una già scaduta la toglie il prossimo pulisci.
func (c *depositoCatture) rimetti(utente int64, nonce, chiave string, d *buste.Dettato, scade time.Time) {
	if d == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	k := chiaveInvio(chiave, utente, nonce)
	if _, ok := c.voci[k]; !ok {
		c.pulisci(c.o.Adesso())
		c.tieni(k, voceCattura{utente: utente, nonce: nonce, d: *d, scade: scade})
	}
}
