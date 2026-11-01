package web

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/L0cutus/gestrip/internal/apparecchio"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// Il riquadro del link pubblico sulla scheda (RIP-S-24, CLI-23, CLI-24, CLI-25), come la sezione
// #repair-public-link e gli eventi revoke-link e regenerate-link di RepairLive dello standalone
// (go-baseline), senza LiveView:
//   - la riga «🔗 Link cliente|dell'oreficeria <stato>» con «· link storico revocato» e «· inviato
//     il <giorno>», per tutti; il token salvato non si stampa mai;
//   - il menu «⋯» solo al titolare: «Revoca link» (solo con un link vivo) e «Genera nuovo link»,
//     ognuno un modulo POST con la conferma di app.js (data-confirm) e il menu chiuso da app.js
//     (data-close-outside);
//   - POST /repairs/{id}/revoca-link e POST /repairs/{id}/nuovo-link sono del titolare dentro la
//     scheda (azioneDelTitolare): il dipendente torna alla scheda con «Solo il titolare può …»;
//   - riuscita, si torna alla scheda con il messaggio dello standalone; un guasto torna alla scheda
//     con «… non revocato.» o «… non generato.»; una busta che non c'è o nel cestino porta alla home
//     con «Riparazione non trovata.», come le altre azioni della scheda (033, 034).
//
// Il nuovo link si mostra una volta, come quello della busta nuova (slice 031, esiti.go): il redirect
// porta a /repairs/<id>?esito=<identificativo casuale, non il token>, la voce (link e messaggio
// «Nuovo link … generato») sta in memoria legata a utente, accesso e busta, e la prima GET la
// consuma, con Cache-Control: no-store. Nello standalone
// il link resta nello stato del LiveView finché un altro evento lo toglie (clear_one_time_link/3):
// qui ogni altra azione è un redirect senza esito, e la revoca e un altro nuovo link tolgono anche
// gli esiti non ancora aperti della busta per la stessa sessione. Il token non va mai in un
// indirizzo, in un cookie, in un'intestazione, nel registro o nell'audit. Ogni ritorno alla scheda
// porta anche il segno della scheda mostrata senza un'apertura (segni.go).
//
// Senza indirizzo_pubblico «Genera nuovo link» rifiuta prima di toccare qualunque cosa, con la riga
// da scrivere, il file e il riavvio (D-46, Stefano 01/11/2026, sul modello della D-32); «Revoca
// link» non ha bisogno dell'indirizzo e funziona. La busta nuova (031) si crea comunque e la home
// dice che cosa manca.

const (
	confermaNuovoLink   = "Generare un nuovo link? Il link attuale smette di funzionare."
	confermaRevocaLink  = "Revocare il link? Il cliente non potrà più aprire la pagina con questo link. La cronologia della busta resta."
	confermaRevocaVivo  = "Revocare il link? Il cliente non potrà più aprire la pagina, né con questo link né con il vecchio link con codice. La cronologia della busta resta."
	msgSoloTitolareRevo = "Solo il titolare può revocare il link."
	msgSoloTitolareNuov = "Solo il titolare può generare un nuovo link."
)

// datiLink è il riquadro come lo disegna la scheda.
type datiLink struct {
	Proprietario    string // «cliente» o «dell'oreficeria» (link_owner/1)
	Stato           string // link_state_text/1
	StoricoRevocato bool   // « · link storico revocato»
	InviatoIl       string // « · inviato il <giorno>», "" se mai
	Titolare        bool   // il menu «⋯»
	Vivo            bool   // «Revoca link» (link_alive?/1)
	ConfermaRevoca  string // revoke_confirmation/1
	Nuovo           *linkMostrato
}

// proprietarioLink è link_owner/1: su una busta di un'oreficeria esterna il link è dell'oreficeria,
// mai «cliente».
func proprietarioLink(oreficeria bool) string {
	if oreficeria {
		return "dell'oreficeria"
	}
	return "cliente"
}

// datiDelLink scrive il riquadro dallo stato del link della busta; i giorni sono date/1 per un
// istante, cioè il giorno in negozio.
func datiDelLink(s buste.Scheda, titolare bool) datiLink {
	l := s.Link
	d := datiLink{Proprietario: proprietarioLink(s.B2B()), Titolare: titolare, Vivo: l.Vivo(),
		StoricoRevocato: l.Storico == buste.StoricoRevocato, ConfermaRevoca: confermaRevocaLink}
	if l.Storico == buste.StoricoVivo {
		d.ConfermaRevoca = confermaRevocaVivo
	}
	switch l.Stato {
	case buste.LinkAttivo:
		d.Stato = "attivo fino al " + clock.Formatta(*l.Scade, "02/01/2006")
	case buste.LinkSoloImpronta:
		d.Stato = "attivo fino al " + clock.Formatta(*l.Scade, "02/01/2006") +
			", emesso prima dei link stabili: al prossimo invio ne nasce uno nuovo"
	case buste.LinkRevocato:
		d.Stato = "revocato: al prossimo invio ne nasce uno nuovo"
	case buste.LinkScaduto:
		d.Stato = "scaduto: al prossimo invio ne nasce uno nuovo"
	default:
		d.Stato = "non ancora emesso: nasce al primo invio"
	}
	if l.InviatoIl != nil {
		d.InviatoIl = clock.Formatta(*l.InviatoIl, "02/01/2006")
	}
	return d
}

// linkPubblico è <indirizzo_pubblico>/r/<token>, o un URL vuoto se indirizzo_pubblico manca.
func (f *facciaWeb) linkPubblico(token string, oreficeria bool) linkMostrato {
	l := linkMostrato{Oreficeria: oreficeria}
	if f.pubblico != "" {
		l.URL = f.pubblico + "/r/" + token
	}
	return l
}

// comeConfigurare dice che cosa manca per mostrare un link: la riga del file e il riavvio.
func (f *facciaWeb) comeConfigurare() string {
	return fmt.Sprintf("manca «indirizzo_pubblico = https://host[:porta]» in %s, poi riavvia gestrip serve con «%s»",
		f.config, apparecchio.ComandoRiavvio)
}

// proprietarioDellaBusta è link_owner/1 della busta com'è adesso, per il messaggio di un guasto
// (lo standalone usa la busta della pagina). ok false: la busta non c'è più e la risposta è data.
func (f *facciaWeb) proprietarioDellaBusta(w http.ResponseWriter, r *http.Request, id int64) (string, bool) {
	s, err := f.buste.Scheda(r.Context(), id)
	switch {
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return "", false
	case err != nil:
		f.registro.Error("busta non letta")
		Errore(w, r)
		return "", false
	}
	return proprietarioLink(s.B2B()), true
}

// revocaLink è POST /repairs/{id}/revoca-link (l'evento revoke-link).
func (f *facciaWeb) revocaLink(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	}
	conSegno := f.segnoDelModulo(r, id)
	// Come clear_one_time_link/3: il comando toglie il link mostrato una volta, anche non ancora visto.
	f.esiti.togliDellaBusta(statoDi(r).dati.Utente, statoDi(r).dati.Nonce, id)
	b2b, err := f.buste.RevocaLink(r.Context(), id, contestoDi(r))
	switch {
	case err == nil:
		st := statoDi(r)
		st.dati.Info, st.cambiata = "Link "+proprietarioLink(b2b)+" revocato.", true
		f.vaiA(w, r, f.allaScheda(r, id, ""))
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
	default:
		f.registro.Error("link della busta non revocato")
		if p, ok := f.proprietarioDellaBusta(w, r, id); ok {
			f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), "Link "+p+" non revocato.")
		}
	}
}

// nuovoLink è POST /repairs/{id}/nuovo-link (l'evento regenerate-link).
func (f *facciaWeb) nuovoLink(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	}
	conSegno := f.segnoDelModulo(r, id)
	// D-46: senza indirizzo_pubblico il rifiuto viene prima di toccare qualunque cosa (vecchio link,
	// Telegram, email, l'esito non ancora visto), dopo il ruolo (la catena di azioneDelTitolare) e
	// dopo l'id, come «Nuovo link» degli inviti senza indirizzo_interno (D-32).
	if f.pubblico == "" {
		if p, ok := f.proprietarioDellaBusta(w, r, id); ok {
			f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), "Nuovo link "+p+" non generato: "+f.comeConfigurare()+".")
		}
		return
	}
	// Il nuovo link prende il posto di quello di prima (assign(:new_public_link, …)).
	f.esiti.togliDellaBusta(statoDi(r).dati.Utente, statoDi(r).dati.Nonce, id)
	n, err := f.buste.NuovoLink(r.Context(), id, contestoDi(r))
	switch {
	case err == nil:
		// Il messaggio viaggia con il link, come il flash e il link nello stesso render dello
		// standalone: un'altra pagina aperta prima non lo consuma.
		st := statoDi(r)
		esito := f.esiti.metti(st.dati.Utente, st.dati.Nonce, esitoBusta{Busta: id, Link: f.linkPubblico(n.Token, n.Oreficeria),
			Messaggio: "Nuovo link " + proprietarioLink(n.Oreficeria) + " generato: il precedente non funziona più."})
		f.vaiA(w, r, f.allaScheda(r, id, esito))
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
	default:
		f.registro.Error("nuovo link della busta non generato")
		if p, ok := f.proprietarioDellaBusta(w, r, id); ok {
			f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), "Nuovo link "+p+" non generato.")
		}
	}
}

// nuovoLinkDaMostrare è il link dell'esito ?esito=<id> della scheda della busta, una volta sola, con
// la risposta che non resta nella cache del browser; dà il messaggio da mostrare con il link ("" se
// non c'è niente). Una HEAD non lo consuma.
func (f *facciaWeb) nuovoLinkDaMostrare(w http.ResponseWriter, r *http.Request, busta int64, d *datiLink) string {
	id := r.URL.Query().Get("esito")
	if id == "" || r.Method != http.MethodGet {
		return ""
	}
	st := statoDi(r)
	e, ok := f.esiti.prendiPer(id, st.dati.Utente, st.dati.Nonce, busta)
	if !ok {
		return ""
	}
	d.Nuovo = &e.Link
	w.Header().Set("Cache-Control", "no-store")
	return e.Messaggio
}
