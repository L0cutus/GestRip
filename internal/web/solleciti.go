package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// I solleciti all'artigiano (slice 083, ART-20, ART-21, ART-22, ART-07 contatore), come ArtisanRemindersLive
// (/handovers/reminders), il contatore di HandoverLive e #repair-artisan-reminders di RepairLive (302439d):
//   - la pagina (personale): la soglia del parametro giorni (o dell'impostazione, 14; buste.SogliaSolleciti), il
//     modulo GET della soglia (1…120, come la fonte), le buste in ritardo per artigiano della consegna con i giorni
//     fuori, il telefono, l'ultimo sollecito; ogni visita scrive handover.reminders_viewed con soglia e conteggio
//     (se non si scrive, niente pagina), tranne la pagina dopo un sollecito (sotto), che in LiveView non è una
//     visita;
//   - «⏰ Registra sollecito» (data-confirm, data-invio-unico) è POST /handovers/reminders con artigiano e il
//     parametro giorni della pagina: buste.RegistraSollecito rilegge le buste e scrive; poi la pagina con
//     «Sollecito registrato per N buste.» o «Nessuna busta da sollecitare per questo artigiano.»;
//   - «📱 WhatsApp sollecito» (solo con un numero componibile) e «👥 Scegli contatto» (sempre) sono lo stesso POST
//     con wa=numero o wa=scegli: registrato il sollecito, senza JavaScript il redirect a wa.me con il testo del
//     Flask; con JavaScript (data-avviso-invio, come «Invia» del popup della 066) il JSON {"url": …}, e app.js
//     manda la scheda aperta al tocco a wa.me e questa pagina alla lista (D-74). Il testo nomina le buste rilette
//     nella transazione. Nessuna riga nella coda in uscita: il messaggio lo manda l'operatore.
// Nessuna regola di ruolo (titolare e dipendente), come la fonte.

const (
	rottaSolleciti       = "/handovers/reminders"
	descrizioneSolleciti = 50 // truncate/1 della pagina: fino a 50 grafemi, altrimenti 44 e «…»
	troncaturaSolleciti  = 44
	domandaSollecito     = "Registrare il sollecito per queste buste?"
)

type bustaSollecitoVista struct {
	ID                               int64
	Numero, Descrizione, Sollecitata string
	Giorni                           int64
}

type gruppoSollecitoVista struct {
	ID                  int64
	Artigiano, Telefono string
	Quante, Ultimo      string
	ConNumero           bool // «📱 WhatsApp sollecito»; senza, l'avviso del numero da salvare
	Buste               []bustaSollecitoVista
}

type datiSolleciti struct {
	Giorni    int64
	Parametro string // il parametro giorni della pagina, rimandato dai moduli
	Gruppi    []gruppoSollecitoVista
}

// buste1 è bags/1 della pagina: «busta» o «buste».
func buste1(n int) string {
	if n == 1 {
		return "busta"
	}
	return "buste"
}

// troncaSolleciti è truncate/1 della pagina dei solleciti.
func troncaSolleciti(s string) string {
	if len(buste.Grafemi(s, descrizioneSolleciti+1)) <= descrizioneSolleciti {
		return s
	}
	return strings.Join(buste.Grafemi(s, troncaturaSolleciti), "") + "…"
}

// telefonoDi è il telefono dell'artigiano, "" se non c'è.
func telefonoDi(g buste.GruppoSolleciti) string {
	if g.Telefono == nil {
		return ""
	}
	return *g.Telefono
}

// testoSollecito è reminder_text/1 (consegna_artigiano_solleciti.html:41 del Flask), con il numero dei testi
// della D-64 (buste.NumeroNeiTesti).
func testoSollecito(g buste.GruppoSolleciti) string {
	nn := make([]string, 0, len(g.Buste))
	for _, x := range g.Buste {
		qr := ""
		if x.CodiceQR != nil {
			qr = *x.CodiceQR
		}
		nn = append(nn, buste.NumeroNeiTesti(qr, x.Numero))
	}
	return "Ciao " + g.Artigiano + ", ti ricordo che hai ancora " + strconv.Itoa(len(g.Buste)) + " buste fuori bottega. Buste: " + strings.Join(nn, ", ")
}

// indirizzoSolleciti è la pagina con il parametro giorni com'era ("" se non c'era o era vuoto).
func indirizzoSolleciti(giorni string) string {
	if giorni == "" {
		return rottaSolleciti
	}
	return rottaSolleciti + "?giorni=" + url.QueryEscape(giorni)
}

// paginaSolleciti è GET /handovers/reminders (handle_params/3).
func (f *facciaWeb) paginaSolleciti(w http.ResponseWriter, r *http.Request) {
	giorni, _ := testoPlug(r.URL.RawQuery, "giorni")
	// Dopo un sollecito la pagina si ridisegna senza una visita nuova (in LiveView l'evento non rifà
	// handle_params): il segno della sessione, una volta, tolto subito, così una lettura non riuscita non lo
	// lascia alla visita dopo.
	st := statoDi(r)
	dopoSollecito := st.dati.DopoSollecito
	if dopoSollecito {
		st.dati.DopoSollecito, st.cambiata = false, true
	}
	s, err := f.buste.SollecitiPendenti(r.Context(), giorni)
	if err != nil {
		f.registro.Error("solleciti pendenti non letti", "errore", err.Error())
		f.salva(w, st) // il segno tolto resta tolto anche con la pagina d'errore
		Errore(w, r)
		return
	}
	if !dopoSollecito {
		if err := f.buste.RegistraVistaSolleciti(r.Context(), s.Giorni, s.Totale, contestoDi(r)); err != nil {
			f.registro.Error("lettura dei solleciti non registrata")
			Errore(w, r)
			return
		}
	}
	d := datiSolleciti{Giorni: s.Giorni, Parametro: giorni}
	for _, g := range s.Gruppi {
		v := gruppoSollecitoVista{ID: g.ArtigianoID, Artigiano: g.Artigiano, Telefono: telefonoDi(g),
			Quante: strconv.Itoa(len(g.Buste)) + " " + buste1(len(g.Buste)), ConNumero: buste.NumeroWhatsApp(telefonoDi(g)) != ""}
		if g.UltimoSollecito != nil {
			v.Ultimo = clock.Timbro(*g.UltimoSollecito)
		}
		for _, x := range g.Buste {
			b := bustaSollecitoVista{ID: x.ID, Numero: x.Visibile().Titolo(), Descrizione: troncaSolleciti(x.Descrizione), Giorni: x.GiorniFuori}
			if x.UltimoSollecito != nil {
				b.Sollecitata = clock.Timbro(*x.UltimoSollecito)
			}
			v.Buste = append(v.Buste, b)
		}
		d.Gruppi = append(d.Gruppi, v)
	}
	f.mostraCon(w, r, "solleciti", http.StatusOK, "", d)
}

// registraSollecito è POST /handovers/reminders (l'evento record-reminder).
func (f *facciaWeb) registraSollecito(w http.ResponseWriter, r *http.Request) {
	giorni, _ := ultimo(r, "giorni")
	wa, _ := ultimo(r, "wa")
	v, _ := ultimo(r, "artigiano")
	artigiano, _ := idPositivo(v)
	torna := indirizzoSolleciti(giorni)
	g, err := f.buste.RegistraSollecito(r.Context(), artigiano, giorni, contestoDi(r))
	st := statoDi(r)
	switch {
	case errors.Is(err, buste.ErrNessunaBustaDaSollecitare):
		st.dati.DopoSollecito = true
		f.vaiAConErrore(w, r, torna, err.Error())
		return
	case err != nil:
		f.registro.Error("sollecito non registrato", "errore", err.Error())
		Errore(w, r)
		return
	}
	numero := ""
	switch wa {
	case "numero":
		numero = buste.NumeroWhatsApp(telefonoDi(g))
		fallthrough
	case "scegli":
		indirizzo := buste.IndirizzoWaMe(numero, testoSollecito(g))
		w.Header().Set("Cache-Control", "no-store")
		if strings.Contains(r.Header.Get("Accept"), "application/json") {
			corpo, _ := json.Marshal(map[string]string{"url": indirizzo}) // una mappa di testi: non fallisce
			f.salva(w, st)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(corpo)
			return
		}
		f.vaiA(w, r, indirizzo)
		return
	}
	st.dati.Info, st.dati.DopoSollecito, st.cambiata = "Sollecito registrato per "+strconv.Itoa(len(g.Buste))+" "+buste1(len(g.Buste))+".", true, true
	f.vaiA(w, r, torna)
}

// sollecitoVisto è una riga del riquadro «⏰ Solleciti all'artigiano» della scheda (#repair-artisan-reminders).
type sollecitoVisto struct {
	Il, Attore string
	Giorni     int64
	ConAttore  bool
}

func sollecitiVisti(ss []buste.SollecitoDellaBusta) []sollecitoVisto {
	var out []sollecitoVisto
	for _, s := range ss {
		v := sollecitoVisto{Il: clock.Timbro(s.Il), Giorni: s.GiorniFuori}
		if s.Attore != nil {
			v.Attore, v.ConAttore = *s.Attore, true
		}
		out = append(out, v)
	}
	return out
}

// registraSolleciti registra le rotte dei solleciti (personale, come la fonte).
func (f *facciaWeb) registraSolleciti(mux *http.ServeMux) {
	f.rottaConGuscio(mux, "GET "+rottaSolleciti, personale, f.paginaSolleciti)
	f.rotta(mux, "POST "+rottaSolleciti, personale, f.registraSollecito)
}
