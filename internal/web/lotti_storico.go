package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// Lo storico dei lotti inviati, il dettaglio e il reinvio (slice 093, ORE-11, ORE-12, ORE-13), come
// ReadyLotHistoryLive dello standalone (/ready-lots/history e /ready-lots/history/:id, go-baseline), per tutto il
// personale:
//   - la lista: gli ultimi 100 lotti inviati dal più recente (buste.StoricoLotti), «Completato il …» o «N da
//     ritirare», «Apri»; nessuna eliminazione (come lo standalone);
//   - il dettaglio: inviato e completato sull'orologio del negozio, il link pubblico (lo stesso dell'invio e della
//     pagina del lotto: indirizzo_pubblico + /lotto/<gettone>) o «Link storico non riattivato durante
//     l'importazione.», le buste con «Ritirata» e «Apri busta». Un lotto aperto, che non c'è o un id non valido torna
//     alla lista con «Lotto non trovato nello storico.» (mount);
//   - il reinvio (solo a lotto inviato, non chiuso e con il link): «Prepara di nuovo il messaggio» è POST
//     /ready-lots/history/{id}/prepara, che non scrive (buste.PreparaReinvio) e ridisegna il dettaglio (200) con il
//     testo, «↗ Apri WhatsApp» (solo con un numero) e «👥 Scegli contatto». Senza LiveView il testo preparato (il
//     resend_pending del socket) sta nel deposito in memoria, legato a utente, accesso e lotto, con una chiave casuale
//     che i due moduli rimandano. Il tocco è POST /ready-lots/history/{id}/reinvia (data-avviso-invio, D-74 come i
//     solleciti della 083): la prima volta registra la riga e ready_lot.resent con il testo preparato
//     (buste.RegistraReinvio), poi wa.me (JSON con JavaScript, redirect senza); un secondo tocco sulla stessa
//     chiave riapre WhatsApp e non registra (anche due tocchi insieme: decide il deposito). Un rifiuto al tocco
//     consuma la chiave e ridisegna il dettaglio con il messaggio. Il messaggio dopo la registrazione («Reinvio
//     registrato.») non si mostra: con JavaScript la pagina va alla lista (come lo standalone dalla slice 225), senza
//     va a wa.me;
//   - D-101: una chiave che il deposito non ha (inventata, di un'altra sessione, persa con un
//     riavvio, scaduta dopo durataReinvio) ridisegna il dettaglio con «Prepara di nuovo il messaggio: niente è stato
//     registrato.» e non scrive: nello standalone il socket ha il testo finché la pagina è aperta;
//   - senza indirizzo_pubblico un testo del lotto con {link} non si prepara (D-46 estesa, la frase del popup).
// Guasto del database → la pagina d'errore con una riga nel registro.

const (
	rottaPaginaStoricoLotti    = "/ready-lots/history"
	msgReinvioPronto           = "Messaggio pronto: apri WhatsApp per reinviarlo e registrarlo."
	msgReinvioDaPreparare      = "Prepara di nuovo il messaggio: niente è stato registrato." // D-101
	lunghezzaDescrStoricoLotti = 60                                                          // truncate/1 dello storico
	// durataReinvio è quanto resta valido un messaggio preparato (e quanto si ricorda che è stato registrato).
	durataReinvio = 30 * time.Minute
	// maxReinvii è il massimo dei messaggi preparati in memoria; oltre, si toglie quello che scade prima.
	maxReinvii = 500
)

// rigaStoricoLotti è una riga della lista dello storico.
type rigaStoricoLotti struct {
	ID                              int64
	Oreficeria, Inviato, Completato string
	Buste, DaRitirare               int
}

type datiStoricoLotti struct{ Lotti []rigaStoricoLotti }

// rigaBustaStorico è una busta del dettaglio.
type rigaBustaStorico struct {
	ID                  int64
	Numero, Descrizione string
	Ritirata            bool
}

// riquadroReinvio è il messaggio preparato del dettaglio.
type riquadroReinvio struct {
	Messaggio, Chiave string
	ConNumero         bool
}

type datiLottoStorico struct {
	ID                              int64
	Oreficeria, Inviato, Completato string
	Link                            string
	AccessoSpento, Reinviabile      bool
	Buste                           []rigaBustaStorico
	Reinvio                         *riquadroReinvio
}

// troncaStoricoLotti è truncate/1 dello storico dei lotti: oltre 60 grafemi i primi 60 e «…».
func troncaStoricoLotti(s string) string {
	if len(buste.Grafemi(s, lunghezzaDescrStoricoLotti+1)) <= lunghezzaDescrStoricoLotti {
		return s
	}
	return strings.Join(buste.Grafemi(s, lunghezzaDescrStoricoLotti), "") + "…"
}

func timbroDi(t *time.Time) string {
	if t == nil {
		return ""
	}
	return clock.Timbro(*t)
}

// paginaStoricoLotti è GET /ready-lots/history.
func (f *facciaWeb) paginaStoricoLotti(w http.ResponseWriter, r *http.Request) {
	ll, err := f.buste.StoricoLotti(r.Context())
	if err != nil {
		f.registro.Error("storico dei lotti non letto", "errore", err.Error())
		Errore(w, r)
		return
	}
	var d datiStoricoLotti
	for _, l := range ll {
		d.Lotti = append(d.Lotti, rigaStoricoLotti{ID: l.ID, Oreficeria: l.Oreficeria, Inviato: timbroDi(l.Inviato), Completato: timbroDi(l.Chiuso),
			Buste: l.Buste, DaRitirare: l.DaRitirare})
	}
	f.mostraCon(w, r, "lotti_storico", http.StatusOK, "", d)
}

// lottoDelloStoricoDellaRotta legge il lotto inviato di {id}; altrimenti torna alla lista con «Lotto non trovato nello
// storico.» e dà false.
func (f *facciaWeb) lottoDelloStoricoDellaRotta(w http.ResponseWriter, r *http.Request) (buste.LottoAvviso, bool) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, rottaPaginaStoricoLotti, buste.ErrLottoNonNelloStorico.Error())
		return buste.LottoAvviso{}, false
	}
	l, err := f.buste.LottoDelloStorico(r.Context(), id)
	switch {
	case errors.Is(err, buste.ErrLottoNonNelloStorico):
		f.vaiAConErrore(w, r, rottaPaginaStoricoLotti, err.Error())
		return l, false
	case err != nil:
		f.registro.Error("lotto dello storico non letto", "errore", err.Error())
		Errore(w, r)
		return l, false
	}
	return l, true
}

// mostraLottoStorico disegna il dettaglio con l'errore o l'esito e, dopo «Prepara», il messaggio q.
func (f *facciaWeb) mostraLottoStorico(w http.ResponseWriter, r *http.Request, l buste.LottoAvviso, errore, esito string, q *riquadroReinvio) {
	bb, err := f.buste.BusteDelLotto(r.Context(), l.ID)
	if err != nil {
		f.registro.Error("buste del lotto non lette", "errore", err.Error())
		Errore(w, r)
		return
	}
	d := datiLottoStorico{ID: l.ID, Oreficeria: l.Oreficeria, Inviato: timbroDi(l.Inviato), Completato: timbroDi(l.Chiuso),
		AccessoSpento: l.AccessoSpento, Reinviabile: l.Reinviabile(), Reinvio: q}
	if !l.AccessoSpento {
		d.Link = f.pubblico + "/lotto/" + l.Gettone
	}
	for _, x := range bb {
		d.Buste = append(d.Buste, rigaBustaStorico{ID: x.ID, Numero: x.Visibile().Titolo(), Descrizione: troncaStoricoLotti(x.Descrizione), Ritirata: x.Ritirata})
	}
	f.mostraConInfo(w, r, "lotto_storico", http.StatusOK, errore, esito, d)
}

// ridisegnaLottoStorico ridisegna il dettaglio dopo un rifiuto, con il lotto riletto.
func (f *facciaWeb) ridisegnaLottoStorico(w http.ResponseWriter, r *http.Request, id int64, errore string) {
	l, err := f.buste.LottoDelloStorico(r.Context(), id)
	if err != nil {
		if errors.Is(err, buste.ErrLottoNonNelloStorico) {
			f.vaiAConErrore(w, r, rottaPaginaStoricoLotti, err.Error())
			return
		}
		f.registro.Error("lotto dello storico non letto", "errore", err.Error())
		Errore(w, r)
		return
	}
	f.mostraLottoStorico(w, r, l, errore, "", nil)
}

// paginaLottoStorico è GET /ready-lots/history/{id}.
func (f *facciaWeb) paginaLottoStorico(w http.ResponseWriter, r *http.Request) {
	if l, ok := f.lottoDelloStoricoDellaRotta(w, r); ok {
		f.mostraLottoStorico(w, r, l, "", "", nil)
	}
}

// rifiutoDelReinvio è il messaggio di un rifiuto del reinvio (refusal/1), "" se err non è un rifiuto.
func (f *facciaWeb) rifiutoDelReinvio(err error) string {
	var rifiuto buste.Rifiuto
	var modello buste.ErrModelloLotto
	switch {
	case errors.As(err, &rifiuto):
		return err.Error()
	case errors.As(err, &modello):
		return "Il testo «" + modello.Famiglia.Etichetta() + "» non è valido: " + modello.Avviso + "." + msgNientePartito
	case errors.Is(err, buste.ErrSenzaIndirizzoPubblico):
		return "Messaggio non inviato: " + f.comeConfigurare() + "."
	}
	return ""
}

// preparaReinvio è l'evento prepare-resend: «Prepara di nuovo il messaggio».
func (f *facciaWeb) preparaReinvio(w http.ResponseWriter, r *http.Request) {
	l, ok := f.lottoDelloStoricoDellaRotta(w, r)
	if !ok {
		return
	}
	x, err := f.buste.PreparaReinvio(r.Context(), l.ID, f.pubblico)
	if err != nil {
		if m := f.rifiutoDelReinvio(err); m != "" {
			f.mostraLottoStorico(w, r, l, m, "", nil)
			return
		}
		f.registro.Error("reinvio non preparato", "errore", err.Error())
		Errore(w, r)
		return
	}
	st := statoDi(r)
	k := f.reinvii.metti(st.dati.Utente, st.dati.Nonce, l.ID, x)
	f.mostraLottoStorico(w, r, x.Lotto, "", msgReinvioPronto, &riquadroReinvio{Messaggio: x.Messaggio, Chiave: k, ConNumero: x.Numero != ""})
}

// reinviaLotto è l'evento open-resend: il tocco su «↗ Apri WhatsApp» (via=numero) o «👥 Scegli contatto» (via=scegli).
func (f *facciaWeb) reinviaLotto(w http.ResponseWriter, r *http.Request) {
	l, ok := f.lottoDelloStoricoDellaRotta(w, r)
	if !ok {
		return
	}
	chiave, _ := ultimo(r, "reinvio")
	via, _ := ultimo(r, "via")
	scelto := via == "scegli" // come phx-value-via: tutto ciò che non è «scegli» è il numero
	st := statoDi(r)
	x, stato := f.reinvii.prendi(chiave, st.dati.Utente, st.dati.Nonce, l.ID)
	switch stato {
	case reinvioAssente:
		f.mostraLottoStorico(w, r, l, msgReinvioDaPreparare, "", nil)
		return
	case reinvioDaRegistrare:
		if _, err := f.buste.RegistraReinvio(r.Context(), l.ID, x.Messaggio, scelto, contestoDi(r)); err != nil {
			f.reinvii.togli(chiave)
			if m := f.rifiutoDelReinvio(err); m != "" {
				f.ridisegnaLottoStorico(w, r, l.ID, m)
				return
			}
			f.registro.Error("reinvio non registrato", "errore", err.Error())
			Errore(w, r)
			return
		}
	}
	numero := ""
	if !scelto {
		numero = x.Numero
	}
	f.rispondiWaMe(w, r, buste.IndirizzoWaMe(numero, x.Messaggio))
}

// rispondiWaMe porta a wa.me dopo una scrittura: con Accept: application/json il JSON {"url": …} (app.js manda la
// scheda aperta al tocco a WhatsApp e questa pagina alla lista, D-74), altrimenti il redirect; mai in cache.
func (f *facciaWeb) rispondiWaMe(w http.ResponseWriter, r *http.Request, indirizzo string) {
	w.Header().Set("Cache-Control", "no-store")
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		corpo, _ := json.Marshal(map[string]string{"url": indirizzo}) // una mappa di testi: non fallisce
		f.salva(w, statoDi(r))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(corpo)
		return
	}
	f.vaiA(w, r, indirizzo)
}

// registraStoricoLotti registra le rotte dello storico (del personale, come la live dello standalone).
func (f *facciaWeb) registraStoricoLotti(mux *http.ServeMux) {
	f.rottaConGuscio(mux, rottaStoricoLotti, personale, f.paginaStoricoLotti)
	f.rottaConGuscio(mux, "GET /ready-lots/history/{id}", personale, f.paginaLottoStorico)
	f.rottaConGuscio(mux, "POST /ready-lots/history/{id}/prepara", personale, f.preparaReinvio) // ridisegna il dettaglio
	f.rottaConGuscio(mux, "POST /ready-lots/history/{id}/reinvia", personale, f.reinviaLotto)   // ridisegna dopo un rifiuto
}

// --- il deposito dei messaggi preparati -------------------------------------------------------

// statoReinvio è quello che il deposito dice di una chiave al tocco.
type statoReinvio int

const (
	reinvioAssente       statoReinvio = iota // nessun messaggio preparato con questa chiave, per questa sessione e questo lotto
	reinvioDaRegistrare                      // il primo tocco: tocca a chi chiama registrare
	reinvioGiaRegistrato                     // un tocco dopo il primo (anche insieme): solo WhatsApp
)

type voceReinvio struct {
	utente int64
	nonce  string
	lotto  int64
	x      buste.ReinvioLotto
	preso  bool
	scade  time.Time
}

// depositoReinvii tiene in memoria i messaggi preparati (il resend_pending del socket dello standalone).
type depositoReinvii struct {
	mu   sync.Mutex
	o    clock.Orologio
	voci map[string]*voceReinvio
}

func nuovoDepositoReinvii(o clock.Orologio) *depositoReinvii {
	return &depositoReinvii{o: o, voci: map[string]*voceReinvio{}}
}

// metti salva il messaggio preparato per la sessione (utente, nonce) e il lotto, e dà la chiave dei moduli.
func (d *depositoReinvii) metti(utente int64, nonce string, lotto int64, x buste.ReinvioLotto) string {
	k := casuale()
	d.mu.Lock()
	defer d.mu.Unlock()
	adesso := d.o.Adesso()
	for c, v := range d.voci {
		if !adesso.Before(v.scade) {
			delete(d.voci, c)
		}
	}
	for len(d.voci) >= maxReinvii {
		prima := ""
		for c, v := range d.voci {
			if prima == "" || v.scade.Before(d.voci[prima].scade) {
				prima = c
			}
		}
		delete(d.voci, prima)
	}
	d.voci[k] = &voceReinvio{utente: utente, nonce: nonce, lotto: lotto, x: x, scade: adesso.Add(durataReinvio)}
	return k
}

// prendi dice cosa fare al tocco con la chiave: una chiave scaduta si toglie, una di un'altra sessione o di un altro
// lotto non si consuma; la prima volta dà reinvioDaRegistrare, poi sempre reinvioGiaRegistrato.
func (d *depositoReinvii) prendi(chiave string, utente int64, nonce string, lotto int64) (buste.ReinvioLotto, statoReinvio) {
	d.mu.Lock()
	defer d.mu.Unlock()
	v, ok := d.voci[chiave]
	switch {
	case !ok:
		return buste.ReinvioLotto{}, reinvioAssente
	case !d.o.Adesso().Before(v.scade):
		delete(d.voci, chiave)
		return buste.ReinvioLotto{}, reinvioAssente
	case v.utente != utente || v.nonce != nonce || v.lotto != lotto:
		return buste.ReinvioLotto{}, reinvioAssente
	case v.preso:
		return v.x, reinvioGiaRegistrato
	}
	v.preso = true
	return v.x, reinvioDaRegistrare
}

// togli dimentica la chiave (un rifiuto o un guasto al tocco).
func (d *depositoReinvii) togli(chiave string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.voci, chiave)
}
