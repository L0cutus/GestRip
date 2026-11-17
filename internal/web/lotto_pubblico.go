package web

import (
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// La pagina pubblica del lotto (ORE-14, ORE-15, ORE-16, slice 092), come PublicLotController e PublicLotHTML dello
// standalone (go-baseline), sulla faccia pubblica dietro Funnel. Le regole (gettone, buste, chiusura alla lettura,
// foto) stanno in internal/buste/lotto_pubblico.go; qui gli indirizzi, le risposte e i testi:
//   - GET /lotto/{token}: no-store e no-referrer (public_headers/1), la pagina con «Buongiorno <oreficeria>», il
//     conto, il giorno del lotto nel calendario del negozio, il totale dei soli importi esatti con «(N senza importo
//     esatto)», lo stato del lotto, le buste e il piè con il profilo del negozio (slice 087). Ogni rifiuto è il 404
//     «Lotto non disponibile» di RifiutoPubblico, che il limite conta nel secchio dei token (famiglia lotto,
//     limiti.go: trenta falliti in un minuto da un indirizzo bloccano per quindici). Leggere la pagina chiude il lotto
//     completo (scrittura su GET, come lo standalone);
//   - GET /lotto/{token}/buste/{busta}/foto/{foto}: la foto della busta del lotto per l'oreficeria o, con
//     ?size=thumb esatto, la miniatura; ogni rifiuto il 404 «Foto non disponibile», che il limite non conta (come lo
//     standalone). Niente X-Robots-Tag, come lo standalone.

// lunghezzaDescrLottoPubblico è description/1: oltre 100 grafemi i primi 100 e «…».
const lunghezzaDescrLottoPubblico = 100

// datiLottoPubblico è quello che lotto_pubblico.html riceve.
type datiLottoPubblico struct {
	NomeNegozio  string
	Oreficeria   string
	Conteggio    string // «3 riparazioni pronte»
	Giorno       string // il giorno del lotto (inserted_at) nel calendario del negozio
	ConTotale    bool
	Totale       string
	SenzaImporto string // «(1 senza importo)», «(2 senza importo esatto)»; "" se nessuna
	Completato   bool
	Parziale     string // «N ancora da ritirare su M»
	InAttesa     bool   // tutte le visibili ritirate ma il lotto non è chiuso
	Vuoto        bool
	Buste        []rigaLottoPubblico
	Indirizzo    string // shop_line/1: indirizzo, CAP e città; "" se nessuno
	Telefono     string
	HrefTel      template.HTMLAttr
}

// rigaLottoPubblico è una busta della pagina.
type rigaLottoPubblico struct {
	ID          int64
	Numero      string
	Pronta      string // «dd/mm/aaaa», "" senza data di pronta
	Foto        string // il src della miniatura, "" senza foto
	AltFoto     string
	Descrizione string
	Ritirata    bool
	RitirataIl  string // « dd/mm/aaaa», "" senza data
	Prezzo      string
}

// descrizioneLotto è description/1: oltre 100 grafemi i primi 100 e «…».
func descrizioneLotto(s string) string {
	if len(buste.Grafemi(s, lunghezzaDescrLottoPubblico+1)) <= lunghezzaDescrLottoPubblico {
		return s
	}
	return strings.Join(buste.Grafemi(s, lunghezzaDescrLottoPubblico), "") + "…"
}

// rigaIndirizzo è shop_line/1: indirizzo, CAP e città non vuoti, separati da uno spazio; "" se nessuno.
func rigaIndirizzo(p buste.ProfiloNegozio) string {
	if !p.Presente {
		return ""
	}
	var parti []string
	for _, x := range []*string{p.Indirizzo, p.Cap, p.Citta} {
		if x != nil && *x != "" {
			parti = append(parti, *x)
		}
	}
	return strings.Join(parti, " ")
}

// giornoOVuoto è day/1 per una data senza ora: «dd/mm/aaaa», "" senza data o con una data che non si legge.
func giornoOVuoto(s *string) string {
	if s == nil {
		return ""
	}
	g, err := giorno(*s)
	if err != nil {
		return ""
	}
	return g
}

// datiDelLottoPubblico scrive la pagina dal lotto letto; gettone è quello della richiesta, per gli indirizzi delle
// foto (~p"/lotto/#{@token}/buste/…").
func datiDelLottoPubblico(p buste.PaginaLottoPubblico, gettone string) datiLottoPubblico {
	n := len(p.Buste)
	d := datiLottoPubblico{NomeNegozio: nomeDelNegozio(p.Profilo), Oreficeria: p.Oreficeria,
		Giorno: clock.Formatta(p.Aperto, "02/01/2006"), Completato: p.Completato, ConTotale: p.ConTotale(),
		Indirizzo: rigaIndirizzo(p.Profilo)}
	d.Conteggio = strconv.Itoa(n) + " riparazioni pronte"
	if n == 1 {
		d.Conteggio = "1 riparazione pronta"
	}
	cent, senza := p.Totale()
	d.Totale = buste.EtichettaCentesimi(cent)
	switch {
	case senza == 1:
		d.SenzaImporto = "(1 senza importo)"
	case senza > 1:
		d.SenzaImporto = "(" + strconv.Itoa(senza) + " senza importo esatto)"
	}
	if !p.Completato {
		switch da := p.DaRitirare(); {
		case n == 0:
			d.Vuoto = true
		case da > 0:
			d.Parziale = strconv.Itoa(da) + " ancora da ritirare su " + strconv.Itoa(n)
		default:
			d.InAttesa = true
		}
	}
	if p.Profilo.Presente && p.Profilo.Telefono != nil && *p.Profilo.Telefono != "" {
		d.Telefono, d.HrefTel = *p.Profilo.Telefono, attributoTel(*p.Profilo.Telefono)
	}
	base := "/lotto/" + segmentoPhoenix(gettone) + "/buste/"
	for _, x := range p.Buste {
		r := rigaLottoPubblico{ID: x.ID, Numero: x.Numero, Pronta: giornoOVuoto(x.DataPronta),
			Descrizione: descrizioneLotto(x.Descrizione), Ritirata: x.Ritirata(), Prezzo: x.EtichettaPrezzo()}
		if g := giornoOVuoto(x.DataRitiro); g != "" {
			r.RitirataIl = " " + g
		}
		if x.FotoID != nil {
			r.Foto = indirizzoMiniatura(base + strconv.FormatInt(x.ID, 10) + "/foto/" + strconv.FormatInt(*x.FotoID, 10))
			r.AltFoto = "Foto busta " + strings.TrimPrefix(x.Numero, "N. ")
		}
		d.Buste = append(d.Buste, r)
	}
	return d
}

// paginaLottoPubblica è GET /lotto/{token} (show/2).
func (f *facciaWeb) paginaLottoPubblica(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	gettone := r.PathValue("token")
	p, err := f.buste.PaginaLottoPubblica(r.Context(), gettone)
	switch {
	case errors.Is(err, buste.ErrLottoNonDisponibile):
		RifiutoPubblico(w, r, rifiutoLotto, false)
	case err != nil:
		f.registro.Error("pagina pubblica del lotto non letta")
		Errore(w, r)
	default:
		f.mostraPubblica(w, r, "lotto_pubblico", http.StatusOK, datiDelLottoPubblico(p, gettone))
	}
}

// fotoLottoPubblica è GET /lotto/{token}/buste/{busta}/foto/{foto} (photo/2).
func (f *facciaWeb) fotoLottoPubblica(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	m, err := f.buste.LeggiFotoLotto(r.Context(), r.PathValue("token"), r.PathValue("busta"), r.PathValue("foto"), variante(r), contestoCliente(r))
	if err != nil {
		// D-nuova (da approvare): una riga d'audit che non si scrive è la pagina d'errore 500, come le foto del cliente
		// (062) e dell'artigiano (084); lo standalone, che riceve {:error, changeset} da read_public_media/5, risponde
		// il 404 «Foto non disponibile». Un errore SQL vero è un 500 in tutte e due.
		f.registro.Error("foto del lotto non letta")
		Errore(w, r)
		return
	}
	rispostaFotoPubblica(w, m)
}

// registraLottoPubblico registra le rotte del lotto sulla faccia pubblica.
func (f *facciaWeb) registraLottoPubblico(mux *http.ServeMux) {
	f.rottaPubblica(mux, "GET /lotto/{token}", f.paginaLottoPubblica)
	f.rottaPubblica(mux, "GET /lotto/{token}/buste/{busta}/foto/{foto}", f.fotoLottoPubblica)
}
