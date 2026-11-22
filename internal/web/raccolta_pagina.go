package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/L0cutus/gestrip/internal/buste"
)

// La raccolta dei dettati nella pagina «Assistente e dettatura» (slice 114; RIP-H-49, RIP-H-50, ADM-12), come la
// sezione #training di AssistLive dello standalone a go-baseline, senza LiveView, solo per il titolare:
//   - la sezione «Dettati per l'addestramento»: «Accesa»/«Spenta», il testo dello standalone e, in un paragrafo a parte
//     (#training-privacy), le tre frasi della D-07; il conteggio (#training-remaining: buste rimaste, dettati raccolti,
//     clienti esclusi, solo numeri); «Accendi la raccolta» (spenta, con buste nel lotto), «Spegni la raccolta» (accesa),
//     a lotto finito «Budget esaurito: la raccolta è finita.» e «Avvia un nuovo lotto di 500 buste»; i moduli «Escludi»
//     (una busta) ed «Escludi il cliente»;
//   - POST /settings/assistente/raccolta/accendi e …/spegni sono training_on e training_off; …/lotto è training_refill;
//     …/escludi è training_exclude (campo exclude[bag_number]); …/escludi-cliente è training_exclude_customer (campo
//     exclude_customer[bag_number]). Ognuno torna alla pagina (302) con il messaggio dello standalone; un modulo senza il
//     campo del numero non fa niente (l'evento dello standalone senza quei parametri risponde :noreply);
//   - nuovo lotto ed esclusione del cliente hanno la conferma dello standalone (data-confirm). Con JavaScript la chiede
//     app.js e mette conferma=1 nel modulo; senza, il server risponde con la pagina e la conferma nella sezione
//     (#training-confirm, «Sì» rimanda il modulo con conferma=1, «Annulla» torna alla pagina) e non scrive niente, come la
//     conferma di «✓ Consegnata» (D-70: il passaggio dal server solo come riserva senza JavaScript);
//   - il numero scritto è quello dell'etichetta (D-109, buste.EscludiBustaDallaRaccolta): più buste danno il rifiuto
//     della D-89 («Più buste corrispondono a «X»: …») e niente si cancella; gli esiti nominano il numero visibile della
//     busta trovata («Busta 0045/26 esclusa: …», «La busta 0045/26 non ha un dettato raccolto.», «Cliente della busta
//     0045/26 escluso: …»), dove lo standalone dice «Busta esclusa», «Questa busta» e «Cliente escluso»;
//   - mai un dettato: la pagina, i messaggi e l'audit (scritto dal dominio) hanno solo numeri; un guasto è la pagina
//     d'errore con una riga fissa nel registro, senza il numero scritto né il testo dell'errore.

// I testi della sezione #training di AssistLive (il 500 è @training.budget) e le tre frasi della D-07 (Stefano,
// 29/10/2026: dati solo sull'apparecchio ed esportazione solo a mano, raccolta spenta finché il titolare non la accende,
// modello addestrato su testi anonimi cedibile ad altri negozi).
const (
	testoRaccolta = "Con la raccolta accesa, ogni busta salvata dopo un «Dettato AI» conserva il testo trascritto e la proposta del modello, " +
		"legati alla busta. Il dettato sparisce quando la busta va nel cestino, quando l'esportazione lo ha usato e comunque dopo 12 mesi. " +
		"Prima di qualunque addestramento nome, cognome e telefono del cliente vengono sostituiti con altri inventati, qui in negozio; " +
		"un dettato con il nome di un altro cliente o un nome di persona comune viene scartato. La raccolta si spegne da sola dopo 500 buste."
	testoRaccoltaD07 = "I dettati raccolti restano solo su questo apparecchio: non vengono mandati a nessuno e si esportano solo a mano. " +
		"La raccolta resta spenta finché il titolare non la accende. " +
		"Il modello addestrato con questi testi, resi anonimi, potrà essere ceduto ad altri negozi."

	domandaNuovoLotto     = "Avviare un nuovo lotto di 500 buste e riaccendere la raccolta?"
	domandaEscludiCliente = "Cancellare i dettati di tutte le buste di questo cliente ed escludere le sue buste future?"

	msgLottoEsaurito      = "Budget esaurito: la raccolta non si riaccende."
	msgNessunaBustaNumero = "Nessuna busta con questo numero."
	msgNuovoLotto         = "Nuovo lotto avviato: la raccolta è accesa."
	msgLottoInCorso       = "Il lotto in corso ha ancora buste: niente da ricaricare."
)

const (
	percorsoRaccoltaLotto          = "/settings/assistente/raccolta/lotto"
	percorsoRaccoltaEscludiCliente = "/settings/assistente/raccolta/escludi-cliente"
)

// riquadroRaccolta è la sezione #training.
type riquadroRaccolta struct {
	buste.StatoRaccolta
	Testo, TestoD07, DomandaLotto, DomandaCliente string
	Conferma                                      *confermaRaccolta
}

// confermaRaccolta è la conferma senza JavaScript: la frase, dove rimandare il modulo e, per il cliente, il numero scritto.
type confermaRaccolta struct {
	Frase, Azione string
	Numero        *string
}

func (f *facciaWeb) registraRaccolta(mux *http.ServeMux) {
	f.rotta(mux, "POST /settings/assistente/raccolta/accendi", soloTitolare, func(w http.ResponseWriter, r *http.Request) { f.interruttoreRaccolta(w, r, true) })
	f.rotta(mux, "POST /settings/assistente/raccolta/spegni", soloTitolare, func(w http.ResponseWriter, r *http.Request) { f.interruttoreRaccolta(w, r, false) })
	f.rotta(mux, "POST /settings/assistente/raccolta/escludi", soloTitolare, f.escludiBustaRaccolta)
	f.rottaConGuscio(mux, "POST "+percorsoRaccoltaLotto, soloTitolare, f.nuovoLottoRaccolta)              // ridisegna per la conferma
	f.rottaConGuscio(mux, "POST "+percorsoRaccoltaEscludiCliente, soloTitolare, f.escludiClienteRaccolta) // ridisegna per la conferma
}

// riquadroRaccoltaDi legge lo stato per la pagina.
func (f *facciaWeb) riquadroRaccoltaDi(r *http.Request, conferma *confermaRaccolta) (*riquadroRaccolta, error) {
	s, err := f.buste.StatoRaccolta(r.Context())
	if err != nil {
		return nil, err
	}
	return &riquadroRaccolta{StatoRaccolta: s, Testo: testoRaccolta, TestoD07: testoRaccoltaD07, DomandaLotto: domandaNuovoLotto,
		DomandaCliente: domandaEscludiCliente, Conferma: conferma}, nil
}

// esitoRaccolta torna alla pagina con il messaggio (info o errore).
func (f *facciaWeb) esitoRaccolta(w http.ResponseWriter, r *http.Request, info, errore string) {
	st := statoDi(r)
	if info != "" || errore != "" {
		st.dati.Info, st.dati.Errore, st.cambiata = info, errore, true
	}
	f.vaiA(w, r, paginaAssistentePercorso)
}

// guastoRaccolta è la pagina d'errore con la riga fissa nel registro.
func (f *facciaWeb) guastoRaccolta(w http.ResponseWriter, r *http.Request) {
	f.registro.Error("raccolta dei dettati non cambiata")
	Errore(w, r)
}

// interruttoreRaccolta è training_on e training_off.
func (f *facciaWeb) interruttoreRaccolta(w http.ResponseWriter, r *http.Request, accesa bool) {
	switch err := f.buste.ImpostaRaccolta(r.Context(), accesa, contestoDi(r)); {
	case err == nil:
		f.esitoRaccolta(w, r, "", "")
	case errors.Is(err, buste.ErrLottoEsaurito):
		f.esitoRaccolta(w, r, "", msgLottoEsaurito)
	default:
		f.guastoRaccolta(w, r)
	}
}

// confermata dice se il modulo porta conferma=1 (data-confirm con JavaScript, o «Sì» della conferma del server).
func confermata(r *http.Request) bool {
	v, _ := ultimo(r, "conferma")
	return v == "1"
}

// nuovoLottoRaccolta è training_refill.
func (f *facciaWeb) nuovoLottoRaccolta(w http.ResponseWriter, r *http.Request) {
	if !confermata(r) {
		f.mostraAssistenteConRaccolta(w, r, &confermaRaccolta{Frase: domandaNuovoLotto, Azione: percorsoRaccoltaLotto})
		return
	}
	switch err := f.buste.NuovoLottoRaccolta(r.Context(), contestoDi(r)); {
	case err == nil:
		f.esitoRaccolta(w, r, msgNuovoLotto, "")
	case errors.Is(err, buste.ErrLottoInCorso):
		f.esitoRaccolta(w, r, "", msgLottoInCorso)
	default:
		f.guastoRaccolta(w, r)
	}
}

// escludiBustaRaccolta è training_exclude.
func (f *facciaWeb) escludiBustaRaccolta(w http.ResponseWriter, r *http.Request) {
	numero, ok := ultimo(r, "exclude[bag_number]")
	if !ok {
		f.esitoRaccolta(w, r, "", "")
		return
	}
	switch visibile, err := f.buste.EscludiBustaDallaRaccolta(r.Context(), numero, contestoDi(r)); {
	case err == nil:
		f.esitoRaccolta(w, r, "Busta "+visibile+" esclusa: il suo dettato è stato cancellato.", "")
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.esitoRaccolta(w, r, "", msgNessunaBustaNumero)
	case errors.Is(err, buste.ErrBustaNonRaccolta):
		f.esitoRaccolta(w, r, "", "La busta "+visibile+" non ha un dettato raccolto.")
	case errors.Is(err, buste.ErrPiuBuste):
		f.esitoRaccolta(w, r, "", err.Error())
	default:
		f.guastoRaccolta(w, r)
	}
}

// escludiClienteRaccolta è training_exclude_customer.
func (f *facciaWeb) escludiClienteRaccolta(w http.ResponseWriter, r *http.Request) {
	numero, ok := ultimo(r, "exclude_customer[bag_number]")
	if !ok {
		f.esitoRaccolta(w, r, "", "")
		return
	}
	if !confermata(r) {
		f.mostraAssistenteConRaccolta(w, r, &confermaRaccolta{Frase: domandaEscludiCliente, Azione: percorsoRaccoltaEscludiCliente, Numero: &numero})
		return
	}
	switch visibile, n, err := f.buste.EscludiClienteDallaRaccolta(r.Context(), numero, contestoDi(r)); {
	case err == nil:
		f.esitoRaccolta(w, r, "Cliente della busta "+visibile+" escluso: cancellati "+strconv.FormatInt(n, 10)+
			" dettati; le sue buste future non saranno raccolte.", "")
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.esitoRaccolta(w, r, "", msgNessunaBustaNumero)
	case errors.Is(err, buste.ErrPiuBuste):
		f.esitoRaccolta(w, r, "", err.Error())
	default:
		f.guastoRaccolta(w, r)
	}
}
