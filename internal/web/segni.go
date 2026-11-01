package web

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// La scheda mostrata senza una riga repair.viewed (D-44 estesa, RIP-S-01; Stefano 01/11/2026).
// Nello standalone le azioni della scheda, «Modifica» e «Annulla» sono eventi della stessa pagina
// LiveView e non scrivono righe; qui sono un POST con un redirect alla scheda, o un link. Una riga
// repair.viewed la scrive solo un'apertura vera: la scheda mostrata dopo il redirect di un'azione
// (rifiuti compresi: l'azione ha già la sua riga, o la pagina era già aperta) e la scheda aperta da
// «Modifica» o «Annulla» non la scrivono, e non segnano letto niente (non sono aperture).
//
// Il segno che dice «non è un'apertura» è un identificativo casuale e monouso, nel parametro mostra
// dell'indirizzo, che vale solo con una voce in memoria del server legata a utente, accesso (nonce
// della sessione) e busta, con una scadenza:
//   - lo mette solo il server: un POST di un'azione della scheda, dopo il CSRF, l'accesso e il ruolo
//     (o il rifiuto del ruolo di azioneDelTitolare), nell'indirizzo del suo redirect
//     (durataSegnoAzione); e la pagina della scheda nei link «Modifica» e «Annulla» che disegna
//     (durataSegnoLink);
//   - la GET di quella busta lo consuma: la stessa richiesta lo trova una volta sola. Riusarlo,
//     ricaricare la pagina, tornarci con «indietro», un segno inventato o vuoto, di un'altra
//     sessione (anche dello stesso account), di un altro utente, di un'altra busta, scaduto, o una
//     HEAD sono aperture vere, con la riga. Un segno chiesto da un'altra sessione o per un'altra
//     busta non si consuma;
//   - non è nel cookie: un cookie firmato si può rimandare uguale, e il segno varrebbe di nuovo; e
//     legato all'indirizzo vale solo per quella navigazione (un redirect non seguito non lascia un
//     segno che tolga la riga all'apertura dopo).
//
// Il segno non dà niente che la sessione non abbia già: è solo per chi ha fatto l'accesso, e chi lo
// usa vede la busta che potrebbe aprire comunque. Toglie solo la riga di quella mostra. Un riavvio
// perde i segni: la mostra dopo diventa un'apertura, con la riga. Pieno il deposito (maxSegni), il
// link resta senza segno e aprirlo scrive la riga.

const (
	// durataSegnoAzione è quanto vale il segno del redirect di un'azione: il browser lo segue subito.
	durataSegnoAzione = 2 * time.Minute
	// durataSegnoLink è quanto vale il segno di «Modifica» e «Annulla»: il tempo di una modifica.
	durataSegnoLink = time.Hour
	// maxSegni è il massimo di segni in memoria per tutto il processo.
	maxSegni = 10000
	// maxSegniSessione è il massimo di segni di una sessione (utente e nonce): oltre, si toglie il
	// più vecchio di quella sessione, così una sessione sola non riempie il deposito di tutti.
	maxSegniSessione = 64
	// durataCatena è quanto si può mostrare la scheda senza righe dall'ultima riga scritta da quella
	// sessione per quella busta (apertura vera o azione riuscita, review della 037b): oltre, un link o
	// un rifiuto della pagina è un'apertura vera e fa ripartire la catena.
	durataCatena = time.Hour
)

type voceSegno struct {
	utente int64
	nonce  string
	busta  int64
	inizio time.Time // l'inizio della catena: l'ultima riga scritta
	creato time.Time
	scade  time.Time
}

// depositoSegni tiene in memoria i segni non ancora usati.
type depositoSegni struct {
	mu    sync.Mutex
	o     clock.Orologio
	segni map[string]voceSegno
}

func nuovoDepositoSegni(o clock.Orologio) *depositoSegni {
	return &depositoSegni{o: o, segni: map[string]voceSegno{}}
}

// metti dà un segno per la scheda della busta, per la sessione (utente, nonce), della catena che
// comincia a inizio: vale durata, ma mai oltre inizio più durataCatena. "" se manca la sessione, la
// busta o l'inizio, se la catena è già finita, o se il deposito è pieno anche dopo aver tolto gli
// scaduti. Una sessione con maxSegniSessione segni perde il più vecchio.
func (d *depositoSegni) metti(utente int64, nonce string, busta int64, durata time.Duration, inizio time.Time) string {
	if utente <= 0 || nonce == "" || busta <= 0 {
		return ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	adesso := d.o.Adesso()
	scade := adesso.Add(durata)
	if fine := inizio.Add(durataCatena); fine.Before(scade) {
		scade = fine
	}
	if !adesso.Before(scade) {
		return ""
	}
	var suoi int
	var vecchio string
	for k, v := range d.segni {
		if v.utente == utente && v.nonce == nonce {
			suoi++
			if vecchio == "" || v.creato.Before(d.segni[vecchio].creato) {
				vecchio = k
			}
		}
	}
	if suoi >= maxSegniSessione {
		delete(d.segni, vecchio)
	}
	if len(d.segni) >= maxSegni {
		for k, v := range d.segni {
			if !adesso.Before(v.scade) {
				delete(d.segni, k)
			}
		}
		if len(d.segni) >= maxSegni {
			return ""
		}
	}
	id := casuale()
	d.segni[id] = voceSegno{utente: utente, nonce: nonce, busta: busta, inizio: inizio, creato: adesso, scade: scade}
	return id
}

// prendi consuma il segno id se è della sessione (utente, nonce) e della busta e non è scaduto, e
// dà l'inizio della sua catena. Uno scaduto si toglie; uno di un'altra sessione o di un'altra busta
// resta.
func (d *depositoSegni) prendi(id string, utente int64, nonce string, busta int64) (time.Time, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	v, ok := d.segni[id]
	if !ok {
		return time.Time{}, false
	}
	if !d.o.Adesso().Before(v.scade) {
		delete(d.segni, id)
		return time.Time{}, false
	}
	if v.utente != utente || v.nonce != nonce || v.busta != busta {
		return time.Time{}, false
	}
	delete(d.segni, id)
	return v.inizio, true
}

// quanti è il numero di segni in memoria (per i test).
func (d *depositoSegni) quanti() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.segni)
}

// segnoPer dà un segno della sessione della richiesta per la scheda della busta, nella catena che
// comincia a inizio; nessuno per una HEAD, che non mostra la pagina.
func (f *facciaWeb) segnoPer(r *http.Request, busta int64, durata time.Duration, inizio time.Time) string {
	if r.Method == http.MethodHead {
		return ""
	}
	// Senza accesso Utente è 0 e metti non dà segni.
	st := statoDi(r)
	return f.segni.metti(st.dati.Utente, st.dati.Nonce, busta, durata, inizio)
}

// allaScheda è l'indirizzo del redirect di un'azione riuscita alla scheda della busta, con il
// segno di una catena nuova (l'azione ha appena scritto la sua riga); dopo esito, se c'è (il nuovo
// link mostrato una volta). Va chiamato solo da un'azione della scheda riuscita, quando il redirect
// va davvero alla scheda.
func (f *facciaWeb) allaScheda(r *http.Request, busta int64, esito string) string {
	return f.schedaConSegno(r, busta, esito, f.orologio.Adesso())
}

// schedaConSegno è l'indirizzo della scheda con un segno della catena che comincia a inizio.
func (f *facciaWeb) schedaConSegno(r *http.Request, busta int64, esito string, inizio time.Time) string {
	dove := "/repairs/" + strconv.FormatInt(busta, 10)
	sep := "?"
	if esito != "" {
		dove += "?esito=" + esito
		sep = "&"
	}
	if s := f.segnoPer(r, busta, durataSegnoAzione, inizio); s != "" {
		dove += sep + "mostra=" + s
	}
	return dove
}

// tornaAllaScheda è dove torna il dipendente rifiutato da un'azione del titolare della scheda: la
// scheda (con il segno solo se il modulo portava quello della pagina, dopoRifiuto), o la home per un
// id malformato.
func (f *facciaWeb) tornaAllaScheda(r *http.Request) string {
	if id, ok := idPositivo(r.PathValue("id")); ok {
		return f.dopoRifiuto(r, id, f.segnoDelModulo(r, id))
	}
	return "/"
}

// segnoDelModulo consuma il segno della pagina mandato con il modulo della scheda (campo nascosto
// mostra, l'ultima assegnazione del corpo): true se era valido per la sessione e per questa busta, e
// allora la richiesta eredita l'inizio della sua catena. Ogni azione della scheda lo chiama una
// volta, prima di qualunque risposta.
func (f *facciaWeb) segnoDelModulo(r *http.Request, busta int64) bool {
	s, _ := ultimo(r, "mostra")
	st := statoDi(r)
	inizio, ok := f.segni.prendi(s, st.dati.Utente, st.dati.Nonce, busta)
	if ok {
		st.inizioCatena = inizio
	}
	return ok // un segno vuoto non c'è nel deposito
}

// dopoRifiuto è dove torna un rifiuto, o un'azione che non scrive una riga sua (D-44 come
// applicata, 01/11/2026): la scheda con un segno nuovo solo se il modulo portava il segno della
// pagina (conSegno); altrimenti la scheda senza segno, che si apre con la sua riga repair.viewed. Un
// POST forgiato con un token CSRF valido ma senza il segno della pagina non mostra la busta senza
// traccia.
func (f *facciaWeb) dopoRifiuto(r *http.Request, busta int64, conSegno bool) string {
	if conSegno {
		return f.schedaConSegno(r, busta, "", statoDi(r).inizioCatena)
	}
	return "/repairs/" + strconv.FormatInt(busta, 10)
}

// leggiDopoRifiuto legge la busta per ridisegnare la scheda dopo un rifiuto: senza audit solo con il
// segno della pagina (conSegno), altrimenti come un'apertura vera (repair.viewed e segni «letto»).
func (f *facciaWeb) leggiDopoRifiuto(r *http.Request, busta int64, conSegno bool) (buste.Scheda, error) {
	if conSegno {
		return f.buste.Scheda(r.Context(), busta)
	}
	return f.apri(r, strconv.FormatInt(busta, 10))
}

// apri è l'apertura vera della busta (repair.viewed e segni «letto»): se riesce fa partire una
// catena nuova di segni.
func (f *facciaWeb) apri(r *http.Request, id string) (buste.Scheda, error) {
	s, err := f.buste.ApriScheda(r.Context(), id, contestoDi(r))
	if err == nil {
		statoDi(r).inizioCatena = f.orologio.Adesso()
	}
	return s, err
}

// mostraOApri legge la busta della GET della scheda: con un segno valido della sessione per questa
// busta la mostra (buste.Scheda: niente repair.viewed, niente segni «letto»); altrimenti è
// un'apertura (buste.ApriScheda). Solo una GET consuma il segno, con l'ultima assegnazione di
// mostra come testo (come i parametri di Plug).
func (f *facciaWeb) mostraOApri(r *http.Request) (buste.Scheda, error) {
	if id, ok := idPositivo(r.PathValue("id")); ok && r.Method == http.MethodGet {
		if s, testo := parametroQuery(r.URL.RawQuery, "mostra"); testo && s != "" {
			st := statoDi(r)
			if inizio, ok := f.segni.prendi(s, st.dati.Utente, st.dati.Nonce, id); ok {
				st.inizioCatena = inizio
				return f.buste.Scheda(r.Context(), id)
			}
		}
	}
	return f.apri(r, r.PathValue("id"))
}

// disegnaScheda disegna la scheda (anche ridisegnata dopo un rifiuto) con il segno della pagina per
// i suoi link «Modifica» e «Annulla» e per i suoi moduli (campo nascosto mostra).
func (f *facciaWeb) disegnaScheda(w http.ResponseWriter, r *http.Request, errore, esito string, d datiScheda) {
	// La pagina continua la catena della richiesta (segno consumato o riga appena scritta); senza
	// catena, niente segno: i link e i moduli valgono come un'apertura vera.
	d.Segno = f.segnoPer(r, d.ID, durataSegnoLink, statoDi(r).inizioCatena)
	f.mostraConInfo(w, r, "scheda", http.StatusOK, errore, esito, d)
}
