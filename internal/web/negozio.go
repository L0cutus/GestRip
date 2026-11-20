package web

import (
	"errors"
	"net/http"
	"regexp"

	"github.com/L0cutus/gestrip/internal/buste"
)

// La pagina «Impostazioni del negozio», sezione del profilo (slice 087, ADM-02), come ShopLive dello standalone
// (go-baseline) per gli eventi save e save_estimate, solo per il titolare (live_session :owner). Le altre sezioni
// della pagina dello standalone (operatività, logo e icone, colori della barra, accessi) sono di altre voci
// (ADM-03, 05…07 e gli accessi): qui non ci sono ancora. Senza LiveView:
//   - GET /settings/shop disegna il modulo #shop-form con il profilo salvato (vuoto senza profilo);
//   - POST /settings/shop è l'evento save (shop_profile[<campo>], gli stessi nomi dello standalone):
//     riuscito, ritorno alla pagina (302) con «Impostazioni salvate. I documenti emessi restano invariati.»
//     (come put_flash e il modulo ricostruito dal profilo salvato); rifiutato, la pagina ridisegnata (200)
//     con l'errore sotto ogni campo e i valori del changeset, senza messaggio in alto;
//   - un modulo senza la mappa shop_profile è il catch-all di handle_event: «Richiesta non riconosciuta:
//     nessuna impostazione salvata.»
//   - la sezione «Stima tempi» (slice 102, ADM-04, RIP-H-55) ha il suo modulo #estimate-form e la sua rotta, POST
//     /settings/shop/stima (l'evento save_estimate: estimate[enabled], estimate[artisan_min], estimate[artisan_max]):
//     riuscito, ritorno alla pagina (302, come push_navigate) con «Stima tempi salvata.»; un modulo senza la mappa
//     estimate è lo stesso catch-all; un guasto del database «Stima tempi non salvata: riprova.» con le impostazioni
//     lette di nuovo (non quelle scritte). I giorni non si rifiutano mai: si riportano (buste.SalvaStima).
//
// Le regole del changeset e la scrittura sono in buste.SalvaProfilo e buste.SalvaStima. Il profilo non va mai nel
// registro, né i valori della stima.

const (
	rottaPaginaNegozio     = "/settings/shop"
	rottaStimaNegozio      = "/settings/shop/stima"
	msgStimaSalvata        = "Stima tempi salvata."
	msgStimaNonSalvata     = "Stima tempi non salvata: riprova."
	msgNegozioSalvato      = "Impostazioni salvate. I documenti emessi restano invariati."
	msgNegozioNonRiconosc  = "Richiesta non riconosciuta: nessuna impostazione salvata."
	msgNegozioNonSalvato   = "Impostazioni non salvate: riprova."
	introNegozio           = "I dati sono congelati in ogni documento al momento dell'emissione. Nome e sottotitolo sono la testata della ricevuta che il cliente porta a casa."
	campoNegozioTesto      = "text"
	campoNegozioEmail      = "email"
	campoNegozioAreaDiTest = "textarea"
)

// campoNegozio è un campo del modulo, con l'etichetta e gli attributi di ShopLive.render/1.
type campoNegozio struct {
	Chiave, Etichetta, Tipo string
	Obbligatorio            bool
	SenzaCompletamento      bool // autocomplete="off"
	Valore                  string
	Errori                  []string
}

// chiaveGaranzia è il campo di più righe: ha il suo blocco nel modello (la casella col nome scritto per esteso).
const chiaveGaranzia = "warranty_terms"

// campiNegozio sono i campi nell'ordine e con le etichette del modulo dello standalone (le etichette sono
// quelle, salvo «Città» con l'accento, che nello standalone è «Citta»: D-97).
var campiNegozio = []campoNegozio{
	{Chiave: "shop_name", Etichetta: "Nome negozio", Tipo: campoNegozioTesto, Obbligatorio: true},
	{Chiave: "subtitle", Etichetta: "Sottotitolo", Tipo: campoNegozioTesto},
	{Chiave: "legal_name", Etichetta: "Ragione sociale", Tipo: campoNegozioTesto},
	{Chiave: "fiscal_id", Etichetta: "Identificativo fiscale", Tipo: campoNegozioTesto},
	{Chiave: "tax_code", Etichetta: "Codice fiscale (vuoto = uguale alla P.IVA)", Tipo: campoNegozioTesto, SenzaCompletamento: true},
	{Chiave: "address", Etichetta: "Indirizzo", Tipo: campoNegozioTesto},
	{Chiave: "postal_code", Etichetta: "CAP", Tipo: campoNegozioTesto},
	{Chiave: "city", Etichetta: "Città", Tipo: campoNegozioTesto},
	{Chiave: "province", Etichetta: "Provincia", Tipo: campoNegozioTesto},
	{Chiave: "phone", Etichetta: "Telefono", Tipo: campoNegozioTesto},
	{Chiave: "email", Etichetta: "Email", Tipo: campoNegozioEmail},
	{Chiave: "sales_email", Etichetta: "Email vendite", Tipo: campoNegozioEmail},
	{Chiave: "opening_hours", Etichetta: "Orari (nei messaggi al cliente)", Tipo: campoNegozioTesto},
	{Chiave: "instagram", Etichetta: "Instagram", Tipo: campoNegozioTesto},
	{Chiave: "facebook", Etichetta: "Facebook", Tipo: campoNegozioTesto},
	{Chiave: "website", Etichetta: "Blog o sito", Tipo: campoNegozioTesto},
	{Chiave: "warranty_terms", Etichetta: "Condizioni di garanzia", Tipo: campoNegozioAreaDiTest, Obbligatorio: true},
}

type datiNegozio struct {
	Intro    string
	Campi    []campoNegozio // tutti tranne la garanzia
	Garanzia campoNegozio
	Stima    stimaNegozio
}

// stimaNegozio è la sezione «Stima tempi»: le impostazioni salvate (quelle riparate che si leggono, non quelle scritte) e la
// riga «Oggi».
type stimaNegozio struct {
	Attiva   bool
	Min, Max int64
	// Oggi è «Pronta di solito in N-M giorni dal ritiro», o vuoto senza abbastanza buste pronte.
	Oggi                       string
	Minimo, Bottega, Artigiano int64
	Supplemento                buste.Stima
	Fonte                      string // «misurato sui dati» o «dalle impostazioni» (supplement_source/1)
}

// mostraNegozio disegna la pagina con i valori dati (nil: quelli salvati) e gli errori per campo.
func (f *facciaWeb) mostraNegozio(w http.ResponseWriter, r *http.Request, errore string, valori map[string]string, errori map[string][]string) {
	if valori == nil {
		var err error
		if valori, err = f.buste.ProfiloPerModulo(r.Context()); err != nil {
			f.registro.Error("profilo del negozio non letto", "errore", err.Error())
			Errore(w, r)
			return
		}
	}
	p, err := f.buste.PaginaStima(r.Context())
	if err != nil {
		f.registro.Error("stima dei tempi non letta", "errore", err.Error())
		Errore(w, r)
		return
	}
	d := datiNegozio{Intro: introNegozio, Stima: stimaNegozio{Attiva: p.Attiva, Min: p.Min, Max: p.Max, Minimo: p.Minimo,
		Bottega: p.Bottega, Artigiano: p.Artigiano, Supplemento: p.Supplemento, Fonte: "dalle impostazioni"}}
	if p.Oggi != nil {
		d.Stima.Oggi = p.Oggi.Frase()
	}
	if p.Misurato {
		d.Stima.Fonte = "misurato sui dati"
	}
	for _, c := range campiNegozio {
		c.Valore, c.Errori = valori[c.Chiave], errori[c.Chiave]
		if c.Chiave == chiaveGaranzia {
			d.Garanzia = c
		} else {
			d.Campi = append(d.Campi, c)
		}
	}
	f.mostraCon(w, r, "negozio", http.StatusOK, errore, d)
}

// paginaNegozio è GET /settings/shop (mount di ShopLive).
func (f *facciaWeb) paginaNegozio(w http.ResponseWriter, r *http.Request) {
	f.mostraNegozio(w, r, "", nil, nil)
}

// reCampoNegozio e reCampoStima sono un nome del modulo: shop_profile[<campo>] o estimate[<campo>] con, dopo, niente
// (un testo) o altre parentesi (una lista o una mappa).
var (
	reCampoNegozio = regexp.MustCompile(`^shop_profile\[([^\[\]]+)\](.*)$`)
	reCampoStima   = regexp.MustCompile(`^estimate\[([^\[\]]+)\](.*)$`)
)

// moduloDelNegozio legge shop_profile[<campo>] (l'ultimo valore, come Plug). Il secondo valore dice se il modulo è
// la mappa shop_profile (%{"shop_profile" => attrs}): almeno un campo shop_profile[…], e nessun «shop_profile» o
// «shop_profile[]», che Plug farebbe testo o lista. Un campo del profilo scritto come lista o mappa arriva nil (non
// è un testo).
func moduloDelNegozio(r *http.Request) (buste.ModuloProfilo, bool) {
	campi, mappa := moduloAMappa(r, "shop_profile", reCampoNegozio)
	m := buste.ModuloProfilo(campi)
	if v := m[chiaveGaranzia]; v != nil {
		a := aCapoUnix(*v) // la casella è di LiveView nello standalone: «\n» (decisione 069c)
		m[chiaveGaranzia] = &a
	}
	return m, mappa
}

// moduloDellaStima legge estimate[<campo>] come moduloDelNegozio legge shop_profile[<campo>]: la mappa estimate di
// %{"estimate" => attrs} dell'evento save_estimate.
func moduloDellaStima(r *http.Request) (buste.ModuloStima, bool) {
	campi, mappa := moduloAMappa(r, "estimate", reCampoStima)
	return buste.ModuloStima(campi), mappa
}

// moduloAMappa legge i campi <mappa>[<campo>] del modulo (l'ultimo valore, come Plug) e dice se il modulo è la mappa
// <mappa>: almeno un campo <mappa>[…], e nessun «<mappa>» o «<mappa>[]», che Plug farebbe testo o lista. Un campo scritto
// come lista o mappa arriva nil (non è un testo), qualunque sia l'ordine delle chiavi.
func moduloAMappa(r *http.Request, mappa string, re *regexp.Regexp) (map[string]*string, bool) {
	m := map[string]*string{}
	trovata := false
	var testi []string // le chiavi con un testo, applicate dopo il giro: una forma lista o mappa dello stesso campo vince sempre
	for k := range r.PostForm {
		if k == mappa || k == mappa+"[]" {
			return nil, false
		}
		sotto := re.FindStringSubmatch(k)
		if sotto == nil {
			continue
		}
		trovata = true
		if sotto[2] != "" {
			m[sotto[1]] = nil
		} else {
			testi = append(testi, k)
		}
	}
	for _, k := range testi {
		campo := re.FindStringSubmatch(k)[1]
		if _, nonTesto := m[campo]; nonTesto {
			continue
		}
		v, _ := ultimo(r, k)
		m[campo] = &v
	}
	return m, trovata
}

// salvaNegozio è l'evento save.
func (f *facciaWeb) salvaNegozio(w http.ResponseWriter, r *http.Request) {
	m, mappa := moduloDelNegozio(r)
	if !mappa {
		f.mostraNegozio(w, r, msgNegozioNonRiconosc, nil, nil)
		return
	}
	err := f.buste.SalvaProfilo(r.Context(), m, contestoDi(r))
	var rifiuto *buste.ProfiloRifiutato
	switch {
	case err == nil:
		st := statoDi(r)
		st.dati.Info, st.cambiata = msgNegozioSalvato, true
		f.vaiA(w, r, rottaPaginaNegozio)
	case errors.As(err, &rifiuto):
		f.mostraNegozio(w, r, "", rifiuto.Valori, rifiuto.Errori)
	default:
		// Un guasto del database: i valori scritti nel modulo, mai il testo nel registro.
		f.registro.Error("profilo del negozio non salvato", "errore", err.Error())
		scritti := map[string]string{}
		for k, v := range m {
			if v != nil {
				scritti[k] = *v
			}
		}
		f.mostraNegozio(w, r, msgNegozioNonSalvato, scritti, nil)
	}
}

// salvaStima è l'evento save_estimate: i giorni non si rifiutano, si riportano; riuscito, ritorno alla pagina (push_navigate)
// con il messaggio. Un guasto del database ridisegna la pagina con le impostazioni lette di nuovo.
func (f *facciaWeb) salvaStima(w http.ResponseWriter, r *http.Request) {
	m, mappa := moduloDellaStima(r)
	if !mappa {
		f.mostraNegozio(w, r, msgNegozioNonRiconosc, nil, nil)
		return
	}
	if err := f.buste.SalvaStima(r.Context(), m, contestoDi(r)); err != nil {
		// Mai i valori scritti nel registro.
		f.registro.Error("stima dei tempi non salvata", "errore", err.Error())
		f.mostraNegozio(w, r, msgStimaNonSalvata, nil, nil)
		return
	}
	st := statoDi(r)
	st.dati.Info, st.cambiata = msgStimaSalvata, true
	f.vaiA(w, r, rottaPaginaNegozio)
}

// registraNegozio registra le rotte della pagina (solo il titolare).
func (f *facciaWeb) registraNegozio(mux *http.ServeMux) {
	f.rottaConGuscio(mux, "GET /settings/shop", soloTitolare, f.paginaNegozio)
	f.rottaConGuscio(mux, "POST /settings/shop", soloTitolare, f.salvaNegozio) // ridisegna dopo un rifiuto
	f.rottaConGuscio(mux, "POST "+rottaStimaNegozio, soloTitolare, f.salvaStima)
}
