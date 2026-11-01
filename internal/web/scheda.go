package web

import (
	"errors"
	"html"
	"html/template"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// La scheda della busta in sola lettura (RIP-S-01, RIP-S-02, RIP-S-18), come RepairLive.mount/3 e
// il riquadro «Dati riparazione» di render/1 dello standalone (go-baseline), senza LiveView e
// senza JavaScript:
//   - GET /repairs/{id}, lo stesso percorso dello standalone, per titolare e dipendente;
//   - busta che non c'è, id malformato o busta nel cestino: home con «Riparazione non trovata.»
//     (Workshop.get_repair/1 dà nil anche per un id che non è un numero); apertura che non si
//     registra: home con «Impossibile aprire la riparazione.»;
//   - «← Indietro» porta a /: la lista con i filtri usati arriva con la T4.2.
//
// La modifica (slice 033) sta in modifica.go; stati, riapertura e attesa (slice 034) in stati.go; il
// riquadro del link pubblico (slice 035) in link.go. Il resto della pagina dello standalone (foto,
// messaggi, stampa, stima dei tempi, rientro dall'artigiano, Telegram, i pulsanti
// fluttuanti ➕ e ✏️) appartiene ad altre slice e tappe.

const (
	msgBustaNonTrovata = "Riparazione non trovata."
	msgBustaNonAperta  = "Impossibile aprire la riparazione."
)

// datiScheda è quello che la pagina riceve, già scritto come lo mostra lo standalone.
type datiScheda struct {
	ID             int64
	Numero         string
	CompraOro      bool
	Data, Cliente  string
	Recapito       string            // il telefono del destinatario, "" se non c'è
	HrefTel        template.HTMLAttr // href="tel:<telefono>" (attributoTel)
	PerOreficeria  bool              // il destinatario è l'oreficeria: sotto il telefono il suo nome
	NomeOreficeria string
	ParolaParte    string // «il cliente» o «l'oreficeria», per «Nessun recapito per …»
	B2B            bool
	TelefonoFinale string // il telefono del cliente finale su una busta B2B, «—» se vuoto
	Stato          string
	Descrizione    string
	NotePubbliche  *string
	NoteInterne    *string
	Artigiano      string
	Preventivo     string
	Risposta       string // "accettato", "rifiutato" o ""
	RispostaIl     string
	Prezzo         string
	HaOreficeria   bool
	Oreficeria     string
	NomeNegozio    string
	Consegna       string
	Pronta         string // "" se non c'è
	Consegnata     string // "" se non c'è
	Registrata     string
	QR             string
	TogliData      bool        // la consegna c'è: «Togli data» (RIP-S-06)
	Modulo         *datiModulo // il modulo «Modifica riparazione» al posto del riquadro (modifica.go)
	// Stati, riapertura e attesa (stati.go).
	Transizioni  []opzione       // «Cambia stato»: gli stati raggiungibili; vuoto, niente modulo
	Riapertura   *datiRiapertura // «Riapri la busta», solo su una busta chiusa
	Attesa       bool            // in attesa della risposta del cliente
	AttesaDal    string          // i primi dieci caratteri di attesa_dal, "" se non c'è
	AttesaMotivo string          // "" se non c'è
	Link         datiLink        // il riquadro del link pubblico (link.go)
}

// titoloPagina è il page_title: «Busta #<numero>».
func (d datiScheda) titoloPagina() string { return "Busta #" + d.Numero }

// paginaScheda è GET /repairs/{id}.
func (f *facciaWeb) paginaScheda(w http.ResponseWriter, r *http.Request) {
	s, err := f.buste.ApriScheda(r.Context(), r.PathValue("id"), contestoDi(r))
	switch {
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	case errors.Is(err, buste.ErrAperturaNonRiuscita):
		f.registro.Error("apertura della busta non registrata")
		f.vaiAConErrore(w, r, "/", msgBustaNonAperta)
		return
	case err != nil:
		f.registro.Error("busta non letta")
		Errore(w, r)
		return
	}
	d, err := datiDellaScheda(s, titolareDi(r))
	if err != nil {
		f.registro.Error("busta con una data illeggibile")
		Errore(w, r)
		return
	}
	info := f.nuovoLinkDaMostrare(w, r, s.ID, &d.Link)
	if apri, fuoco := apriModulo(r); apri {
		valori, nulli := valoriDellaBusta(s)
		m, err := f.moduloDellaBusta(r.Context(), s, valori, nulli)
		if err != nil {
			Errore(w, r)
			return
		}
		m.Fuoco = fuoco
		d.Modulo = &m
	}
	f.mostraConInfo(w, r, "scheda", http.StatusOK, "", info, d)
}

// vaiAConErrore è put_flash(:error, …) e redirect.
func (f *facciaWeb) vaiAConErrore(w http.ResponseWriter, r *http.Request, dove, errore string) {
	st := statoDi(r)
	st.dati.Errore, st.cambiata = errore, true
	f.vaiA(w, r, dove)
}

// titolareDi dice se chi chiede è il titolare (Accounts.owner?/1), con il ruolo riletto adesso.
func titolareDi(r *http.Request) bool {
	u := statoDi(r).utente
	return u != nil && u.Ruolo == accounts.Titolare
}

// datiDellaScheda scrive la busta come il riquadro dello standalone; titolare decide il menu del
// link.
func datiDellaScheda(s buste.Scheda, titolare bool) (datiScheda, error) {
	d := datiScheda{
		ID: s.ID, Numero: s.Numero, CompraOro: s.CompraOro, Cliente: s.NomeCliente, B2B: s.B2B(), Stato: etichettaStato(s.Stato),
		Descrizione: s.Descrizione, NotePubbliche: s.NotePubbliche, NoteInterne: s.NoteInterne, Artigiano: "—",
		Preventivo: prezzoVisto(s.Preventivo), Prezzo: prezzoVisto(s.PrezzoFinale), NomeNegozio: s.NomeNegozio,
		Registrata: clock.Timbro(s.CreataIl), QR: "—",
	}
	var err error
	if d.Data, err = giorno(s.DataAccettazione); err != nil {
		return datiScheda{}, err
	}
	if dest, ok := s.Destinatario(); ok {
		d.Recapito, d.HrefTel = dest.Telefono, attributoTel(dest.Telefono)
		d.PerOreficeria, d.NomeOreficeria = dest.Parte == buste.ParteOreficeria, dest.Nome
	} else if dest.Parte == buste.ParteOreficeria {
		d.ParolaParte = "l'oreficeria"
	} else {
		d.ParolaParte = "il cliente"
	}
	d.TelefonoFinale = s.TelefonoCliente
	if d.TelefonoFinale == "" {
		d.TelefonoFinale = "—"
	}
	if s.Artigiano != nil {
		d.Artigiano = *s.Artigiano
	}
	// QuoteDecision.state/1: l'accettazione vince sul rifiuto.
	switch {
	case s.PreventivoAccettatoIl != nil:
		d.Risposta, d.RispostaIl = "accettato", clock.Timbro(*s.PreventivoAccettatoIl)
	case s.PreventivoRifiutatoIl != nil:
		d.Risposta, d.RispostaIl = "rifiutato", clock.Timbro(*s.PreventivoRifiutatoIl)
	}
	if s.Oreficeria != nil {
		d.HaOreficeria, d.Oreficeria = true, s.Oreficeria.Nome
	}
	if d.Consegna, err = consegnaVista(s.Consegna); err != nil {
		return datiScheda{}, err
	}
	d.TogliData = d.Consegna != "-" // il pulsante dello standalone: :if={delivery(@repair) != "-"}
	for _, c := range []struct {
		da *string
		a  *string
	}{{s.DataPronta, &d.Pronta}, {s.DataConsegna, &d.Consegnata}} {
		if c.da != nil {
			if *c.a, err = giorno(*c.da); err != nil {
				return datiScheda{}, err
			}
		}
	}
	if s.CodiceQR != nil {
		d.QR = *s.CodiceQR
	}
	d.Attesa = s.AttesaCliente
	if s.AttesaDal != nil {
		// String.slice(attesa_dal, 0, 10): dieci grafemi.
		d.AttesaDal = primiGrafemi(*s.AttesaDal, 10)
	}
	if s.AttesaMotivo != nil {
		d.AttesaMotivo = *s.AttesaMotivo
	}
	conStati(&d, s)
	d.Link = datiDelLink(s, titolare)
	return d, nil
}

// primiGrafemi sono i primi n grafemi di s, come String.slice/3 da 0.
func primiGrafemi(s string, n int) string {
	g := uniseg.NewGraphemes(s)
	fine := 0
	for i := 0; i < n && g.Next(); i++ {
		_, fine = g.Positions()
	}
	return s[:fine]
}

// attributoTel è href="tel:<telefono>" come lo scrive Phoenix: il telefono com'è («tel:333 111»),
// scappato come valore di un attributo fra virgolette doppie (& < > " '). In un href html/template
// lo normalizzerebbe come indirizzo («%20»); il prefisso «tel:» fisso esclude ogni altro schema.
func attributoTel(telefono string) template.HTMLAttr {
	return template.HTMLAttr(`href="tel:` + html.EscapeString(telefono) + `"`)
}

// giorno è date/1 per un giorno: gg/mm/aaaa.
func giorno(s string) (string, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return "", err
	}
	return t.Format("02/01/2006"), nil
}

// prezzoVisto è price/2 (Repair.price_source/2): il testo scritto, se non è vuoto, com'è; poi i
// centesimi «€ 12,50»; poi «-».
func prezzoVisto(p buste.Prezzo) string {
	switch {
	case p.Testo != nil && *p.Testo != "":
		return *p.Testo
	case p.Centesimi != nil:
		return buste.EtichettaCentesimi(*p.Centesimi)
	}
	return "-"
}

// consegnaVista è delivery/1 (Repair.date_source/2): il testo importato, se non è vuoto, com'è;
// poi la data; poi «-».
func consegnaVista(c buste.Consegna) (string, error) {
	switch {
	case c.Testo != nil && *c.Testo != "":
		return *c.Testo, nil
	case c.Data != nil:
		return giorno(*c.Data)
	}
	return "-", nil
}

// etichettaStato è status_label/1: i trattini bassi diventano spazi, poi String.capitalize/1 (la
// prima lettera maiuscola, il resto minuscolo).
func etichettaStato(stato string) string {
	s := strings.ReplaceAll(stato, "_", " ")
	p, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return ""
	}
	return string(unicode.ToTitle(p)) + strings.ToLower(s[n:])
}
