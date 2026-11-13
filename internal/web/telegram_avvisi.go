package web

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// Il popup «📲 Notifica Telegram al cliente» dopo un cambio di stato (CLI-47, RIP-H-18, slice 074), come
// TelegramStatusPopup e gli eventi telegram-status-send e telegram-status-cancel di RepairLive e
// WorkshopLive dello standalone (go-baseline), senza LiveView:
//   - un cambio di stato dalla scheda (POST /repairs/{id}/stato) o dalla lista (stato-lista, rapido) che
//     prepara un avviso (buste.Cambio.Telegram) lo mette nel deposito del server (depositoNotifiche) con un
//     gettone casuale, e la sessione tiene solo il gettone: le chat congelate non vanno mai nel browser né
//     nel cookie. La pagina dopo il redirect (la scheda o la lista) disegna il popup una volta e toglie il
//     gettone dalla sessione: ricaricarla è «Annulla». Un cambio dopo, anche senza avviso, toglie il popup
//     di prima (un popup vale per un cambio solo);
//   - il popup ha l'anteprima in sola lettura (il testo come lo legge il cliente, senza i tag), il pulsante
//     e un modulo POST /repairs/{id}/notifica-telegram con «Invia» e «Annulla» (azione=invia|annulla);
//     Esc e un tocco fuori sono «Annulla» (app.js). Il modulo è data-invio-unico, e l'avviso si risponde
//     una volta sola: un secondo «Invia» dello stesso popup (entro 5 minuti) torna dove torna il primo,
//     senza accodare di nuovo e senza errore; un gettone che non c'è (scaduto, di un'altra sessione) va
//     alla lista con «…il popup non è più valido.». «Invia» riuscito «📲 Notifica Telegram accodata per il cliente»,
//     rifiutato la frase dello standalone; «Annulla» non dice niente;
//   - dopo la risposta si torna dove il cambio era partito: la scheda (con un segno nuovo: l'avviso è il
//     seguito di un'azione della scheda appena fatta), la lista, o il prezzo finale dopo «✓ Pronta» (la
//     navigazione al prezzo aspetta la risposta del popup, qualunque sia);
//   - un testo che non si compone non apre il popup e lo dice con la frase dello standalone.

const (
	msgTelegramAccodata  = "📲 Notifica Telegram accodata per il cliente"
	msgTelegramNonValida = "Notifica Telegram non inviata: il popup non è più valido."
	titoloPopupTelegram  = "📲 Notifica Telegram al cliente"
	durataNotifica       = time.Hour       // quanto vale un popup lasciato aperto (solo Go: lo standalone lo tiene finché la pagina vive)
	durataRisposto       = 5 * time.Minute // quanto si ricorda un popup già risposto (il doppio tocco su «Invia»)
	maxNotificheInAttesa = 256             // oltre, si perde la più vecchia
	byteGettoneNotifica  = 16
	azioneNotificaInvia  = "invia" // l'altro valore del modulo è «annulla»: tutto quello che non è «invia» non manda
)

// rifiutoNotificaTelegram è refusal/1 di TelegramStatusPopup.
func rifiutoNotificaTelegram(err error) string {
	switch {
	case errors.Is(err, buste.ErrTelegramSuperata):
		return "Notifica Telegram non inviata: superata da un cambio di stato successivo della busta."
	case errors.Is(err, buste.ErrTelegramSpento):
		return "Notifica Telegram non inviata: le notifiche Telegram ai clienti sono spente."
	case errors.Is(err, buste.ErrTelegramNonCliente):
		return "Notifica Telegram non inviata: la busta non è più del cliente diretto."
	case errors.Is(err, buste.ErrTelegramLinkRevocato):
		return "Notifica Telegram non inviata: il link della busta è stato revocato o rigenerato."
	}
	return "Notifica Telegram non inviata: non è stato possibile accodarla."
}

// motivoTelegram è telegram_refusal/1 della scheda: il testo che non si compone con la sua etichetta e
// l'avviso, o la frase generica.
func motivoTelegram(err error) string {
	var m *buste.ModelloNonValido
	if errors.As(err, &m) {
		return "il testo «" + m.Famiglia.Etichetta() + "» non è valido (" + m.Avviso + ")."
	}
	return "non è stato possibile accodarla."
}

// attesaTelegram è un avviso in attesa della risposta del popup: di chi (utente e nonce della sessione),
// della busta, e dove tornare dopo.
type attesaTelegram struct {
	utente   int64
	nonce    string
	n        *buste.NotificaTelegram
	scheda   bool   // il cambio è partito dalla scheda: si torna lì con un segno nuovo
	dopo     string // altrimenti qui: la lista o il prezzo finale
	scade    time.Time
	inserto  uint64
	risposto bool // la risposta c'è già stata: un secondo invio torna a dopo senza fare niente
}

// depositoNotifiche sono gli avvisi in attesa, in memoria (come l'assign del LiveView dello standalone):
// un riavvio li perde, come una pagina chiusa.
type depositoNotifiche struct {
	mu      sync.Mutex
	o       clock.Orologio
	voci    map[string]*attesaTelegram
	contate uint64
}

func nuovoDepositoNotifiche(o clock.Orologio) *depositoNotifiche {
	return &depositoNotifiche{o: o, voci: map[string]*attesaTelegram{}}
}

// metti tiene l'avviso e dà il suo gettone: 16 byte casuali in base64url.
func (d *depositoNotifiche) metti(a *attesaTelegram) string {
	b := make([]byte, byteGettoneNotifica)
	_, _ = rand.Read(b) // crypto/rand.Read non fallisce (in caso estremo il processo si ferma)
	g := base64.RawURLEncoding.EncodeToString(b)
	d.mu.Lock()
	defer d.mu.Unlock()
	adesso := d.o.Adesso()
	for k, v := range d.voci {
		if !adesso.Before(v.scade) {
			delete(d.voci, k)
		}
	}
	if len(d.voci) >= maxNotificheInAttesa {
		var vecchia string
		for k, v := range d.voci {
			if vecchia == "" || v.inserto < d.voci[vecchia].inserto {
				vecchia = k
			}
		}
		delete(d.voci, vecchia)
	}
	d.contate++
	a.scade, a.inserto = adesso.Add(durataNotifica), d.contate
	d.voci[g] = a
	return g
}

// guarda dà l'avviso del gettone per quella sessione, se c'è e vale ancora; non lo toglie.
func (d *depositoNotifiche) guarda(g string, utente int64, nonce string) *attesaTelegram {
	d.mu.Lock()
	defer d.mu.Unlock()
	a := d.voci[g]
	if a == nil || a.risposto || a.utente != utente || a.nonce != nonce || !d.o.Adesso().Before(a.scade) {
		return nil
	}
	return a
}

// prendi è guarda per la risposta del popup: un avviso si risponde una volta sola. La prima volta lo segna
// risposto (per durataRisposto) e dà gia falso; un secondo invio dello stesso popup (il doppio tocco su
// «Invia», che il browser mostra al posto della prima risposta) ha l'avviso con gia vero, e chi chiama
// torna a dopo senza accodare di nuovo, come il secondo invio dello standalone che non fa niente.
func (d *depositoNotifiche) prendi(g string, utente int64, nonce string, busta int64) (a *attesaTelegram, gia bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	a = d.voci[g]
	adesso := d.o.Adesso()
	if a == nil || a.utente != utente || a.nonce != nonce || a.n.BustaID != busta || !adesso.Before(a.scade) {
		return nil, false
	}
	if a.risposto {
		return a, true
	}
	a.risposto = true
	if r := adesso.Add(durataRisposto); r.Before(a.scade) {
		a.scade = r
	}
	return a, false
}

// dopoCambio è TelegramStatusPopup.open/2 dopo un cambio di stato riuscito: il popup di prima se ne va; un
// avviso nuovo va nel deposito e il suo gettone nella sessione; un testo che non si compone lo dice. scheda
// e dopo sono dove tornare dopo la risposta del popup.
func (f *facciaWeb) dopoCambio(r *http.Request, c buste.Cambio, scheda bool, dopo string) {
	st := statoDi(r)
	if st.dati.Telegram != "" {
		st.dati.Telegram, st.cambiata = "", true
	}
	switch {
	case c.Telegram != nil:
		st.dati.Telegram = f.notifiche.metti(&attesaTelegram{utente: st.dati.Utente, nonce: st.dati.Nonce, n: c.Telegram, scheda: scheda, dopo: dopo})
		st.cambiata = true
	case c.TelegramNonPreparata != nil:
		var m *buste.ModelloNonValido
		if !errors.As(c.TelegramNonPreparata, &m) {
			f.registro.Error("notifica Telegram non preparata")
		}
		st.dati.Errore, st.cambiata = "Notifica Telegram non preparata: "+motivoTelegram(c.TelegramNonPreparata), true
	}
}

// datiPopupTelegram è il popup come lo disegna popup/1 di TelegramStatusPopup.
type datiPopupTelegram struct {
	Token, Gettone, Azione       string
	Titolo, Numero, Testo, Tasto string
}

// popupTelegram è il popup della sessione, se c'è, per la pagina che si sta disegnando: il gettone esce
// dalla sessione (una volta sola).
func (f *facciaWeb) popupTelegram(st *stato, token string) *datiPopupTelegram {
	g := st.dati.Telegram
	if g == "" {
		return nil
	}
	st.dati.Telegram, st.cambiata = "", true
	if st.utente == nil {
		return nil
	}
	a := f.notifiche.guarda(g, st.dati.Utente, st.dati.Nonce)
	if a == nil {
		return nil
	}
	return &datiPopupTelegram{Token: token, Gettone: g, Azione: "/repairs/" + strconv.FormatInt(a.n.BustaID, 10) + "/notifica-telegram",
		Titolo: titoloPopupTelegram, Numero: a.n.Numero, Testo: testoInvio("telegram", a.n.Anteprima), Tasto: a.n.Pulsante}
}

// notificaTelegram è POST /repairs/{id}/notifica-telegram: «Invia» (azione=invia) o «Annulla» del popup.
func (f *facciaWeb) notificaTelegram(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	g, _ := ultimo(r, "notifica")
	azione, _ := ultimo(r, "azione")
	st := statoDi(r)
	var a *attesaTelegram
	gia := false
	if ok {
		a, gia = f.notifiche.prendi(g, st.dati.Utente, st.dati.Nonce, id)
	}
	if a == nil {
		if azione == azioneNotificaInvia {
			f.vaiAConErrore(w, r, "/", msgTelegramNonValida)
			return
		}
		f.vaiA(w, r, "/")
		return
	}
	if azione == azioneNotificaInvia && !gia {
		if _, err := f.buste.AutorizzaNotificaTelegram(r.Context(), a.n, contestoDi(r)); err != nil {
			if !erroreNotoTelegram(err) {
				f.registro.Error("notifica Telegram non accodata")
			}
			st.dati.Errore, st.cambiata = rifiutoNotificaTelegram(err), true
		} else {
			st.dati.Info, st.cambiata = msgTelegramAccodata, true
		}
	}
	dove := a.dopo
	if a.scheda {
		dove = f.allaScheda(r, a.n.BustaID, "")
	}
	f.vaiA(w, r, dove)
}

// erroreNotoTelegram dice se il rifiuto è uno di quelli che il popup spiega (niente da scrivere nel
// registro).
func erroreNotoTelegram(err error) bool {
	for _, e := range []error{buste.ErrTelegramSuperata, buste.ErrTelegramSpento, buste.ErrTelegramNonCliente,
		buste.ErrTelegramLinkRevocato, buste.ErrBustaNonTrovata} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}
