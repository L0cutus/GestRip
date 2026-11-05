package web

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/L0cutus/gestrip/internal/buste"
)

// La home con la lista delle buste (slice 050: RIP-H-01 in parte, RIP-H-02, 08, 09, 10, 11, 12, 21,
// 56), come WorkshopLive (:index) dello standalone a go-baseline, senza LiveView: una GET con dei
// link, che funziona senza JavaScript.
//   - intestazione della bottega: «GestRip», nome del negozio, «Banco riparazioni» (shop_header/1);
//     il pulsante «📷 Scan», la campanella e il logo non ci sono ancora (slice 052-055, ADM-05);
//   - i riquadri dei contatori, ognuno un link al filtro che conta (dashboard_card/1);
//   - la lista di buste.Lista, 30 per pagina, con «← Precedente», «N / M», «Successiva →»;
//   - ogni riquadro porta alla scheda /repairs/{id}: un link senza il segno di D-44, quindi
//     un'apertura vera con la sua riga repair.viewed, come l'apertura della scheda da un indirizzo
//     scritto a mano;
//   - il numero mostrato è quello dell'etichetta QR (D-64, buste.NumeroVisibile);
//   - ogni visita scrive repair.index_viewed con i nomi dei filtri (RIP-H-21), una volta sola: lo
//     standalone la scrive al mount statico e di nuovo alla connessione di LiveView (D-68 proposta,
//     da approvare). Se la riga non si scrive, la pagina va in errore.
// Cassetto dei filtri, pillole, ricerca, pulsanti rapidi, 📌, pannello QR e galleria sono delle
// slice 051-057.

// riquadroHome è un riquadro dei contatori (dashboard_card/1).
type riquadroHome struct {
	ID, Emoji, Breve, Href, Etichetta, Tono string
	N                                       int64
}

// rigaHome è il riquadro di una busta della lista, già scritto come lo mostra lo standalone.
type rigaHome struct {
	ID            int64
	Href          string // /repairs/{id}
	Numero        string // il numero visibile (D-64): «#0045/26» o «N. 45/26»
	Apri          string // aria-label del link: «Apri busta 0045/26» (lo standalone interpola senza «#»)
	Cliente, Data string
	Descrizione   string
	Oreficeria    *string
	Stato         string // status_label/1
	ToniStato     string // status_classes/1
	CompraOro     bool
	Attesa        bool
	AttesaGiorni  string // waiting_age/1: «3gg», "" se la data non si legge
	AttesaTitolo  string
	SenzaQR       bool
	Consegna      string // delivery/1: "" se non c'è
	Urgente       bool
	Conversazione bool
	NonLetto      bool // messaggio non letto
	Accettato     bool // preventivo accettato, non letto
	Rifiutato     bool // preventivo rifiutato, non letto
	Fissata       bool
	Chiusa        bool
}

// datiLista è la lista della home.
type datiLista struct {
	Bottega    string // il nome del negozio, per l'intestazione
	Riquadri   []riquadroHome
	Righe      []rigaHome
	Totale     int64
	Pagina     int64
	Pagine     int64
	Precedente string // l'indirizzo della pagina prima, "" sulla prima
	Successiva string // l'indirizzo della pagina dopo, "" sull'ultima
}

// datiHome è quello che la pagina iniziale riceve: la lista e, una volta, il link di una busta
// appena creata.
type datiHome struct {
	Link *linkMostrato
	// ComeConfigurare è il testo per chi non ha indirizzo_pubblico (Link.URL vuoto).
	ComeConfigurare string
	// Foto è l'avviso alla coda delle foto scelte prima del salvataggio, con l'esito (RIP-H-40).
	Foto  *fotoDaMandare
	Lista datiLista
}

// bottega è il nome del negozio dell'intestazione della bottega (conBottega).
func (d datiHome) bottega() string { return d.Lista.Bottega }

// home è la pagina iniziale. Con ?esito=<id> (il redirect dopo una busta nuova) la prima GET mostra
// anche «Riparazione N creata.» e il link della busta (esiti.go), come il flash e public_link/1 sulla
// lista dello standalone, e quella risposta non resta nella cache del browser. Una HEAD non consuma
// l'esito.
func (f *facciaWeb) home(w http.ResponseWriter, r *http.Request) {
	q := r.URL.RawQuery
	valore := func(nome string) (string, bool) { return testoPlug(q, nome) }
	if err := f.buste.RegistraLista(r.Context(), chiaviRegistroLista(q), contestoDi(r)); err != nil {
		f.registro.Error("lettura della lista non registrata")
		Errore(w, r)
		return
	}
	l, err := f.buste.Lista(r.Context(), buste.LeggiFiltri(valore))
	if err != nil {
		f.registro.Error("lista delle buste non letta")
		Errore(w, r)
		return
	}
	d := datiHome{Lista: datiDellaLista(l, f.orologio.Adesso())}
	info := ""
	if id := r.URL.Query().Get("esito"); id != "" && r.Method == http.MethodGet {
		st := statoDi(r)
		if e, ok := f.esiti.prendi(id, st.dati.Utente, st.dati.Nonce); ok {
			info = fmt.Sprintf("Riparazione %s creata.", e.Numero)
			d.Link, d.Foto = &e.Link, e.Foto
			if e.Link.URL == "" {
				d.ComeConfigurare = f.comeConfigurare()
			}
			w.Header().Set("Cache-Control", "no-store")
		}
	}
	f.mostraConInfo(w, r, "home", http.StatusOK, "", info, d)
}

// testoPlug è il parametro nome della query come testo di Plug.Conn.Query.decode/1 (parametroPlug):
// false se manca o se l'ultima assegnazione è una lista o una mappa. «stato[=x» è un'altra chiave e
// non tocca stato.
func testoPlug(q, nome string) (string, bool) {
	v, ok := parametroPlug(q, nome)
	if !ok {
		return "", false
	}
	return v.testo()
}

// chiaviRegistroLista è audited_filter_keys/2 della lista: le chiavi di buste.ChiaviRegistroLista
// che nella query hanno un testo non vuoto senza spazi ai bordi, in ordine alfabetico.
func chiaviRegistroLista(q string) []string {
	var out []string
	for _, k := range buste.ChiaviRegistroLista {
		if v, testo := testoPlug(q, k); testo && strings.TrimFunc(v, unicode.IsSpace) != "" {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// datiDellaLista scrive la lista come la pagina dello standalone.
func datiDellaLista(l buste.Lista, adesso time.Time) datiLista {
	c, f := l.Contatori, l.Filtri
	d := datiLista{Bottega: l.NomeNegozio, Totale: l.Totale, Pagina: f.Pagina, Pagine: l.Pagine}
	d.Riquadri = []riquadroHome{
		{"dashboard-pronta", "✅", "Pronte", "/?stato=pronta", "Pronte", "green", c.Pronte},
		{"dashboard-lavorazione", "🔧", "Lavor.", "/?stato=in_lavorazione", "In lavorazione", "blue", c.Lavorazione},
		{"dashboard-preventivo", "💰", "Prev.", "/?stato=preventivo", "Preventivi", "orange", c.Preventivo},
		{"dashboard-attesa", "📦", "Mat.", "/?stato=in_attesa_materiali", "In attesa mat.", "blue", c.AttesaMateriali},
		{"dashboard-consegnate", "🛍️", "Oggi", "/?stato=consegnata", "Consegnate oggi", "grey", c.ConsegnateOggi},
		{"dashboard-scadenza", "⏰", "Scad.", "/?scadenza=1", "In scadenza", "red", c.Scadenza},
		{"dashboard-compro-oro", "🥇", "Oro", "/?compro_oro=1", "Compro oro", "gold", c.CompraOro},
	}
	// «In attesa», «Fuori bottega» e «Senza QR» solo se contano qualcosa o il loro filtro è attivo;
	// «Messaggi» sempre: è l'unica porta verso ?messaggi=1.
	if c.AttesaCliente > 0 || f.Attesa {
		d.Riquadri = append(d.Riquadri, riquadroHome{"dashboard-attesa-cliente", "⏳", "Attesa", "/?attesa=1", "⏳ In attesa", "gold", c.AttesaCliente})
	}
	if c.FuoriBottega > 0 || f.FuoriBottega {
		d.Riquadri = append(d.Riquadri, riquadroHome{"dashboard-fuori-bottega", "📤", "Fuori", "/?fuori_bottega=1", "📤 Fuori bottega", "blue", c.FuoriBottega})
	}
	if c.SenzaQR > 0 || f.SenzaQR {
		d.Riquadri = append(d.Riquadri, riquadroHome{"dashboard-senza-qr", "⚠", "No QR", "/?senza_qr=1", "⚠ Senza QR", "gold", c.SenzaQR})
	}
	d.Riquadri = append(d.Riquadri, riquadroHome{"dashboard-messaggi", "💬", "Msg", "/?messaggi=1", "💬 Messaggi", "blue", c.Messaggi})
	if f.Pagina > 1 {
		d.Precedente = indirizzoLista(f, f.Pagina-1)
	}
	if f.Pagina < l.Pagine {
		d.Successiva = indirizzoLista(f, f.Pagina+1)
	}
	for _, r := range l.Righe {
		d.Righe = append(d.Righe, rigaDellaLista(r, adesso))
	}
	return d
}

// indirizzoLista è ~p"/?#{filter_query(filters, page)}" con i filtri di questa slice, nell'ordine di
// filter_query/3 (i valori vuoti tolti).
func indirizzoLista(f buste.Filtri, pagina int64) string {
	var parti []string
	metti := func(k, v string) { parti = append(parti, k+"="+url.QueryEscape(v)) }
	if f.Stato != "" {
		metti("stato", f.Stato)
	}
	for _, x := range []struct {
		k  string
		ok bool
	}{{"scadenza", f.Scadenza}, {"compro_oro", f.CompraOro}, {"attesa", f.Attesa}, {"fuori_bottega", f.FuoriBottega},
		{"senza_qr", f.SenzaQR}, {"messaggi", f.Messaggi}} {
		if x.ok {
			metti(x.k, "1")
		}
	}
	metti("p", strconv.FormatInt(pagina, 10))
	return "/?" + strings.Join(parti, "&")
}

// rigaDellaLista scrive una busta come il suo riquadro nella lista.
func rigaDellaLista(r buste.RigaLista, adesso time.Time) rigaHome {
	numero, etichetta := r.NumeroVisibile()
	apri := "Apri busta " + numero
	if etichetta {
		numero = "#" + numero
	}
	h := rigaHome{
		ID: r.ID, Href: "/repairs/" + strconv.FormatInt(r.ID, 10), Numero: numero, Apri: apri, Cliente: r.NomeCliente,
		Descrizione: r.Descrizione, Oreficeria: r.Oreficeria, Stato: etichettaStato(r.Stato), ToniStato: toniStato(r.Stato),
		CompraOro: r.CompraOro, Attesa: r.AttesaCliente, SenzaQR: r.SenzaQR(), Urgente: r.Urgente,
		Conversazione: r.Conversazione(), NonLetto: r.MessaggioNonLetto(), Accettato: r.AccettazioneNonLetta(),
		Rifiutato: r.RifiutoNonLetto(), Fissata: r.Fissata, Chiusa: r.Chiusa(),
	}
	// date_label/1: la data d'accettazione c'è sempre (NOT NULL con il CHECK della forma).
	if g, err := giorno(r.DataAccettazione); err == nil {
		h.Data = g
	}
	// delivery/1 (Repair.date_source/2): il testo se non è vuoto, poi la data, poi niente.
	switch c := r.Consegna; {
	case c.Testo != nil && *c.Testo != "":
		h.Consegna = *c.Testo
	case c.Data != nil:
		if g, err := giorno(*c.Data); err == nil {
			h.Consegna = g
		}
	}
	if r.AttesaCliente {
		h.AttesaTitolo = "In attesa risposta cliente"
		if r.AttesaMotivo != nil && *r.AttesaMotivo != "" {
			h.AttesaTitolo += " — " + *r.AttesaMotivo
		}
		if r.AttesaDal != nil {
			if n, ok := buste.GiorniAttesa(*r.AttesaDal, adesso); ok {
				h.AttesaGiorni = strconv.FormatInt(n, 10) + "gg"
			}
		}
	}
	return h
}

// toniStato è status_classes/1: la base e il tono dello stato.
func toniStato(stato string) string {
	tono := "stato-orange"
	switch stato {
	case "pronta", "perfezionata":
		tono = "stato-emerald"
	case "preventivo", "valutata":
		tono = "stato-violet"
	case "consegnata", "chiusa":
		tono = "stato-stone"
	case "in_attesa_materiali":
		tono = "stato-blue"
	case "annullata":
		tono = "stato-red"
	}
	return "stato " + tono
}
