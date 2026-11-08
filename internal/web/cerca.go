package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
)

// Il campo unico della ricerca della home (D-63, D-64; slice 052, RIP-H-03 e RIP-H-22), al posto
// della ricerca testuale e di «📷 Cerca per QR busta» dello standalone (WorkshopLive: il modulo
// #repair-search-form e gli eventi qr_lookup e qr_select), senza LiveView e senza JavaScript:
//   - il modulo #repair-search-form è un POST a /cerca-qr (il codice letto non va mai
//     nell'indirizzo), con il CSRF, il campo «cerca», gli altri filtri come campi nascosti, «🔍»
//     (il pulsante del modulo, quello dell'Invio e del lettore a pistola) e «📷» con via=qr;
//   - POST /cerca-qr: un valore uguale esatto al codice QR di una busta è una scansione (risposta 1):
//     l'unica busta, o l'unica aperta fra le gemelle (D-64), apre il pannello #qr-action con le
//     azioni (slice 054, pannello_qr.go), altrimenti la scelta fra le gemelle; la riga
//     repair.qr_lookup dice quante e quali, mai il codice. Con via=qr anche un codice sconosciuto è
//     una scansione: «Nessuna riparazione trovata per questo QR». Ogni altro testo è una ricerca
//     scritta e va a smista;
//   - GET /cerca è la ricerca scritta da un indirizzo (smista), senza scansione;
//   - smista: una sola busta trovata apre la scheda (302 come ogni redirect del Go, vaiA; la scheda
//     scrive la sua repair.viewed e nessuna riga della lista: D-68), più buste o nessuna la lista con
//     la ricerca (dal POST disegnata nella risposta, da GET /cerca un 302 a /?cerca=…:
//     la lista scrive la sua repair.index_viewed). La ricerca per numero d'etichetta toglie tutti gli
//     altri filtri (Filtri.Effettivi, D-64); le altre li tengono.
// D-70: con JavaScript la conferma del pannello si apre senza passare dal server (slice 054, app.js) e
// sulla lista la scansione (📷 con il lettore della fotocamera, o il lettore a pistola) manda il modulo
// con fetch e mette il riquadro #qr-lookup di questa pagina sopra la lista, che resta con i filtri
// (slice 055); le rotte restano la riserva senza JavaScript e la fonte dei dati.

// datiCercaQR è la pagina della scansione: il pannello, la scelta fra le gemelle o il messaggio.
type datiCercaQR struct {
	Bottega  string
	Nascosti []coppiaQuery // i filtri del modulo, che restano per la ricerca dopo
	Codice   string        // il codice letto, per le scelte fra le gemelle (solo nel corpo dei moduli)
	Chiudi   string        // la lista con i filtri del modulo, senza la ricerca
	Errore   string        // «QR non fornito», «Nessuna riparazione trovata per questo QR», …
	Azione   *pannelloQR   // il pannello della busta
	Gemelle  []rigaQR      // la scelta, con più buste
	// Esito è l'esito della scansione o dell'azione per il suono del telefono (RIP-H-27, slice 055),
	// deciso qui come qr_outcome/2 dello standalone e mai dalla sola lettura della fotocamera:
	// esitoQROK (busta trovata, una o più), esitoQRKO (codice sconosciuto o vuoto, ricerca non
	// riuscita, scelta non valida, azione rifiutata o busta cambiata), vuoto quando lo standalone non
	// suona (la scelta di una gemella, la conferma disegnata dal server). Va nell'attributo
	// data-qr-esito di #qr-lookup; un'azione riuscita è un redirect, e il suono lo dà app.js.
	Esito string
}

const (
	esitoQROK = "ok"
	esitoQRKO = "ko"
)

// rigaQR è una busta della scansione come la mostrano la scelta e il pannello.
type rigaQR struct {
	ID          int64
	Href        string
	Titolo      string // «#0045/26 — Cliente» (D-64)
	Data        string
	Stato       string
	CompraOro   bool
	Descrizione string // i primi 80 caratteri (String.slice/3)
	Attenuata   bool   // consegnata o annullata
}

// bottega è il nome del negozio dell'intestazione (conBottega).
func (d datiCercaQR) bottega() string { return d.Bottega }

// Ricerca è il modulo del campo unico in questa pagina: vuoto, con i filtri di prima.
func (d datiCercaQR) Ricerca() campoRicerca { return campoRicerca{Nascosti: d.Nascosti} }

// campoRicerca è il modulo #repair-search-form: i filtri da tenere e il testo cercato.
type campoRicerca struct {
	Nascosti []coppiaQuery
	Cerca    string
}

// I due suggerimenti sotto il campo (slice 055d, D-63): quello dello standalone, e quello di un
// numero d'etichetta (da 1 a 4 cifre), che non cerca nel telefono.
const (
	suggerimentoTesto = "Cerca in: cliente, n° busta, descrizione, note, note interne, telefono, QR"
	suggerimentoCifre = "Cerca in: cliente, n° busta, descrizione, note, note interne, QR"
)

// Suggerimento è il testo sotto il campo per il testo cercato adesso.
func (c campoRicerca) Suggerimento() string {
	if _, ok := buste.LeggiRicercaEtichetta(c.Cerca); ok {
		return suggerimentoCifre
	}
	return suggerimentoTesto
}

// SuggerimentoCifre e SuggerimentoTesto sono i due testi, per app.js.
func (campoRicerca) SuggerimentoCifre() string { return suggerimentoCifre }
func (campoRicerca) SuggerimentoTesto() string { return suggerimentoTesto }

// Ricerca è il modulo del campo unico della home.
func (d datiHome) Ricerca() campoRicerca {
	return campoRicerca{Nascosti: d.Lista.Nascosti, Cerca: d.Lista.Cerca}
}

// valoreDelModulo legge un campo del modulo come testo: l'ultimo valore, false se manca (un campo
// «cerca[]» è un'altra chiave, come in Plug).
func valoreDelModulo(r *http.Request) func(string) (string, bool) {
	return func(nome string) (string, bool) {
		v := r.PostForm[nome]
		if len(v) == 0 {
			return "", false
		}
		return v[len(v)-1], true
	}
}

// cerca è GET /cerca: la ricerca scritta con i filtri della query, letti come la home.
func (f *facciaWeb) cerca(w http.ResponseWriter, r *http.Request) {
	q := r.URL.RawQuery
	f.smista(w, r, buste.LeggiFiltri(func(nome string) (string, bool) { return testoPlug(q, nome) }), nil)
}

// smista porta una ricerca scritta dove deve: la lista senza ricerca, la scheda dell'unica busta
// trovata o la lista con la ricerca. La pagina non conta: una ricerca nuova parte dalla prima. Da GET
// /cerca (chiavi nil) la lista è un redirect a /?cerca=…; dal POST del campo unico (chiavi: quelle
// della riga repair.index_viewed) la lista si disegna nella risposta, no-store e senza redirect, così
// il testo (anche un codice letto dalla pistola che non è di nessuna busta) non va nell'indirizzo
// (D-63, review della 052).
func (f *facciaWeb) smista(w http.ResponseWriter, r *http.Request, filtri buste.Filtri, chiavi []string) {
	filtri = filtri.Effettivi()
	filtri.Pagina = 1
	if filtri.Cerca == "" {
		f.vaiA(w, r, indirizzoListaOHome(filtri))
		return
	}
	n, ids, err := f.buste.Selezione(r.Context(), filtri, 1)
	if err != nil {
		f.registro.Error("ricerca della lista non letta")
		Errore(w, r)
		return
	}
	if n == 1 && len(ids) == 1 {
		f.vaiA(w, r, "/repairs/"+strconv.FormatInt(ids[0], 10))
		return
	}
	if chiavi != nil {
		w.Header().Set("Cache-Control", "no-store")
		f.disegnaLista(w, r, filtri, chiavi, false, esitoRicercaScritta(filtri.Cerca, n))
		return
	}
	f.vaiA(w, r, indirizzoListaOHome(filtri))
}

// esitoRicercaScritta è l'esito per il suono di una ricerca scritta dal campo unico che disegna la
// lista (RIP-H-27, slice 059, come lo standalone senza D): un testo con la forma esatta di
// un'etichetta (buste.EtichettaDi: «RIP-2026-0045», anche «rip-2026-0045» o con gli spazi che
// NormalizzaCodice toglie) che non trova nessuna busta è il codice letto dal lettore a pistola di
// un'etichetta che non è di nessuna busta: esitoQRKO, i due bip gravi, come lo scanner dello standalone
// nel campo «Cerca per QR busta». Ogni altra ricerca (un nome, un numero, un'etichetta che trova
// qualcosa con la ricerca testuale) non suona: "".
func esitoRicercaScritta(cerca string, trovate int64) string {
	if _, _, ok := buste.EtichettaDi(cerca); ok && trovate == 0 {
		return esitoQRKO
	}
	return ""
}

// indirizzoListaOHome è la lista con i filtri, senza pagina; «/» senza filtri.
func indirizzoListaOHome(f buste.Filtri) string {
	if q := parametriLista(f, 0); len(q) > 0 {
		return "/?" + codificaQuery(q)
	}
	return "/"
}

// paginaQR prepara la pagina della scansione dai campi del modulo: il nome del negozio, i filtri
// da tenere (senza la ricerca) e «Chiudi», la lista con quei filtri. false se le scelte non si
// leggono: la risposta è già scritta.
func (f *facciaWeb) paginaQR(w http.ResponseWriter, r *http.Request, filtri buste.Filtri) (datiCercaQR, bool) {
	sc, err := f.buste.Scelte(r.Context())
	if err != nil {
		f.registro.Error("scelte della ricerca non lette")
		Errore(w, r)
		return datiCercaQR{}, false
	}
	d := datiCercaQR{Bottega: sc.NomeNegozio}
	for _, x := range parametriLista(filtri, 0) {
		if x.Nome != "cerca" {
			d.Nascosti = append(d.Nascosti, x)
		}
	}
	d.Chiudi = "/"
	if len(d.Nascosti) > 0 {
		d.Chiudi = "/?" + codificaQuery(d.Nascosti)
	}
	return d, true
}

// mostraQR scrive la pagina della scansione, mai in cache, con il messaggio d'errore (flash) errore.
func (f *facciaWeb) mostraQR(w http.ResponseWriter, r *http.Request, codiceHTTP int, errore string, d datiCercaQR) {
	w.Header().Set("Cache-Control", "no-store")
	f.mostraCon(w, r, "cercaqr", codiceHTTP, errore, d)
}

// cercaQR è POST /cerca-qr, il modulo del campo unico: la scansione o la ricerca scritta.
func (f *facciaWeb) cercaQR(w http.ResponseWriter, r *http.Request) {
	valore := valoreDelModulo(r)
	filtri := buste.LeggiFiltri(valore)
	via, _ := valore("via")
	scansione := via == "qr"
	cerca, _ := valore("cerca")
	codice := buste.CodiceLetto(cerca)
	d, ok := f.paginaQR(w, r, filtri)
	if !ok {
		return
	}
	d.Codice = codice
	// Le chiavi della riga repair.index_viewed se la ricerca scritta disegna la lista: quelle del
	// modulo, come la home le legge dalla query (mai i valori).
	chiavi := chiaviRegistroLista(r.PostForm.Encode())
	if chiavi == nil {
		chiavi = []string{}
	}
	mostra := func(codiceHTTP int) {
		if d.Esito == "" && (d.Errore != "" || d.Azione != nil || d.Gemelle != nil) {
			d.Esito = esitoQRKO
			if d.Errore == "" {
				d.Esito = esitoQROK
			}
		}
		f.mostraQR(w, r, codiceHTTP, "", d)
	}
	if codice == "" {
		if scansione {
			d.Errore = "QR non fornito"
			mostra(http.StatusOK)
			return
		}
		f.smista(w, r, filtri, chiavi)
		return
	}
	righe, err := f.buste.CercaPerQR(r.Context(), codice, scansione, contestoDi(r))
	switch {
	case errors.Is(err, buste.ErrQRVuoto): // codice non vuoto: non succede
		d.Errore = "QR non fornito"
		mostra(http.StatusOK)
		return
	case err != nil:
		f.registro.Error("ricerca per QR non riuscita")
		d.Errore = "Ricerca non riuscita."
		mostra(http.StatusInternalServerError)
		return
	case len(righe) == 0 && !scansione:
		f.smista(w, r, filtri, chiavi)
		return
	case len(righe) == 0:
		d.Errore = "Nessuna riparazione trovata per questo QR"
		mostra(http.StatusOK)
		return
	}
	// La scelta di una gemella (qr_select): l'id deve essere fra quelle trovate adesso.
	if v, ok := valore("scelta"); ok {
		id, _ := idPositivo(v)
		for _, x := range righe {
			if x.ID == id {
				// qr_select con un id valido non suona nello standalone.
				d.Azione = pannelloDi(x, "")
				f.mostraQR(w, r, http.StatusOK, "", d)
				return
			}
		}
		d.Errore = "Riparazione non trovata."
		mostra(http.StatusOK)
		return
	}
	if x, ok := buste.UnicaDaAprire(righe); ok {
		d.Azione = pannelloDi(x, "")
		mostra(http.StatusOK)
		return
	}
	for _, x := range righe {
		d.Gemelle = append(d.Gemelle, rigaDellaScansione(x))
	}
	mostra(http.StatusOK)
}

// rigaDellaScansione scrive una busta trovata come la scelta e il pannello dello standalone.
func rigaDellaScansione(x buste.RigaQR) rigaQR {
	numero, etichetta := x.NumeroVisibile()
	if etichetta {
		numero = "#" + numero
	}
	r := rigaQR{
		ID: x.ID, Href: "/repairs/" + strconv.FormatInt(x.ID, 10), Titolo: numero + " — " + x.NomeCliente,
		Stato: etichettaStato(x.Stato), CompraOro: x.CompraOro, Attenuata: x.Stato == "consegnata" || x.Stato == "annullata",
		Descrizione: strings.Join(buste.Grafemi(x.Descrizione, 80), ""),
	}
	if g, err := giorno(x.DataAccettazione); err == nil {
		r.Data = g
	}
	return r
}
