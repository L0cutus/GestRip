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
// 56; slice 051: RIP-H-03, 04, 05, 06, 07, 19, 56 pillola e casella; slice 052: il campo unico),
// come WorkshopLive (:index) dello standalone a go-baseline, senza LiveView: una GET con dei link, il
// modulo GET del cassetto e il POST del campo unico (cerca.go), che funziona senza JavaScript.
//   - intestazione della bottega: «GestRip», nome del negozio, «Banco riparazioni» (shop_header/1);
//     la campanella e il logo non ci sono ancora (ADM-05); il pulsante «📷 Scan» non c'è (D-63:
//     il 📷 è dentro il campo unico, risposta 6 del 05/11/2026);
//   - i riquadri dei contatori, ognuno un link al filtro che conta (dashboard_card/1);
//   - le pillole dei filtri attivi con «× Azzera filtro» (solo con un filtro esplicito), il link
//     «⧉ Schede PDF» al report della selezione (i filtri senza la pagina), il campo unico
//     (#repair-search-form, D-63, con gli altri filtri come campi nascosti) e il cassetto dei filtri
//     (#repair-filter-form, con i campi nascosti della slice 214 dello standalone: attesa, scadenza,
//     notifiche);
//   - la lista di buste.Lista, 30 per pagina, con «← Precedente», «N / M», «Successiva →», che
//     conservano tutti i filtri (filter_query/3);
//   - ogni riquadro porta alla scheda /repairs/{id}: un link senza il segno di D-44, quindi
//     un'apertura vera con la sua riga repair.viewed, come l'apertura della scheda da un indirizzo
//     scritto a mano;
//   - il numero mostrato è quello dell'etichetta QR (D-64, buste.NumeroVisibile); una ricerca per
//     numero d'etichetta mostra solo la ricerca, senza gli altri filtri (Filtri.Effettivi);
//   - la pillola «⏰ In scadenza (N giorni)» dice la soglia vera (D-69);
//   - ogni visita scrive repair.index_viewed con i nomi dei filtri (RIP-H-21), una volta sola: lo
//     standalone la scrive al mount statico e di nuovo alla connessione di LiveView (D-68, Stefano
//     05/11/2026). Se la riga non si scrive, la pagina va in errore.
// Slice 056: ogni riquadro ha i pulsanti rapidi, il 📌 e il menu dello stato (lista_azioni.go), e la
// lista scrive il suo indirizzo in #list-return, che app.js tiene per «← Indietro» della scheda e di
// «Nuova riparazione» (RIP-H-20).
// Slice 057: le linguette «☰ Lista / 🖼 Galleria» e la galleria (RIP-H-13, galleria.go). vista=galleria
// è la vista e non un filtro (buste.Filtri.Galleria): non cambia la selezione né la riga del registro,
// ma la portano tutti i link che portano i filtri (paginazione, «⧉ Schede PDF», i campi nascosti del
// campo unico e del cassetto, #list-return e il campo «lista» delle azioni), come filter_query/3 dello
// standalone; i riquadri dei contatori e «× Azzera filtro» no, come lì.

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
	// NumeroInterno: la busta è nella ricerca per numero d'etichetta solo per il suo numero interno
	// (slice 055d, D-64 punto a): il segno «⚠ senza QR» c'è anche se è chiusa o ha un codice di altro
	// formato, con il suo titolo.
	NumeroInterno bool
	// AltroFormato: la busta aperta ha un codice QR che non è un'etichetta RIP-AAAA-NNNN (D-79,
	// slice 059): porta «⚠ senza QR» come quella senza codice, con un titolo suo.
	AltroFormato  bool
	Consegna      string // delivery/1: "" se non c'è
	Urgente       bool
	Conversazione bool
	NonLetto      bool // messaggio non letto
	Accettato     bool // preventivo accettato, non letto
	Rifiutato     bool // preventivo rifiutato, non letto
	Fissata       bool
	Chiusa        bool
	// Le azioni del riquadro (slice 056, lista_azioni.go): il pulsante rapido ("pronta" per «✓ Pronta»,
	// "consegnata" per «✓ Consegnata», "" per nessuno) con la frase della conferma, e il menu dello stato
	// con lo stato di adesso e i soli passi del grafo (status_choices/1), spento se non ce n'è.
	Rapido           string
	ConfermaConsegna string // «Segnare la busta 0045/26 come consegnata?» (D-64: il numero visibile)
	NumeroStato      string // l'aria-label del menu: «Stato 0045/26»
	Stati            []opzione
	StatoFermo       bool
}

// rigaConModuli è il riquadro con quello che servono ai suoi moduli: il token CSRF della pagina e la
// lista a cui tornare (Lista, la query senza «?»).
type rigaConModuli struct {
	rigaHome
	Token, Lista string
}

// conModuli è la funzione dei modelli che dà a rigaBusta il token e la lista (accesso.go).
func conModuli(r rigaHome, token, lista string) rigaConModuli {
	return rigaConModuli{rigaHome: r, Token: token, Lista: lista}
}

// pillola è una pillola di un filtro attivo.
type pillola struct{ Testo, Classe string }

// coppiaQuery è un parametro di filter_query/3: nome e valore, già testo.
type coppiaQuery struct{ Nome, Valore string }

// datiCassetto è il modulo del cassetto dei filtri, con i valori dei filtri di adesso.
type datiCassetto struct {
	// I filtri senza un campo nel cassetto, che lo attraversano come campi nascosti (slice 214), e la
	// vista (slice 057).
	Attesa, Scadenza, Notifiche, Galleria bool
	Cerca                                 string
	Stati                                 []opzione
	Oreficerie, Artigiani                 []opzione // nil: nessun menu
	Campi                                 []opzione // data_campo
	DataDa, DataA                         string
	CompraOro, FuoriBottega               bool
	SenzaQR, Messaggi                     bool
}

// datiLista è la lista della home.
type datiLista struct {
	Bottega    string // il nome del negozio, per l'intestazione
	Attivo     bool   // un filtro esplicito: le pillole e «× Azzera filtro»
	Pillole    []pillola
	Report     string        // l'indirizzo di «⧉ Schede PDF»
	Nascosti   []coppiaQuery // i campi nascosti della ricerca: i filtri tranne cerca
	Cerca      string
	Cassetto   datiCassetto
	Riquadri   []riquadroHome
	Righe      []rigaHome
	Totale     int64
	Pagina     int64
	Pagine     int64
	Precedente string // l'indirizzo della pagina prima, "" sulla prima
	Successiva string // l'indirizzo della pagina dopo, "" sull'ultima
	// Ritorno è la lista di adesso (filtri e pagina) come query senza «?», per il campo «lista» dei
	// moduli dei riquadri (lista_azioni.go); Indirizzo la stessa lista come la tiene app.js per
	// «← Indietro» (#list-return, RIP-H-20).
	Ritorno   string
	Indirizzo string
	// Le linguette della vista (view_path/2: gli stessi filtri senza la pagina) e la galleria, che c'è
	// solo con vista=galleria (galleria.go).
	VistaLista, VistaGalleria string
	Galleria                  *datiGalleria
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
	// EsitoQR è l'esito per il suono della ricerca scritta disegnata dal POST del campo unico
	// (esitoRicercaScritta, slice 059): "ko" per un'etichetta che non è di nessuna busta, "" altrimenti.
	// Va nell'attributo data-qr-esito di #repair-search-outcome; con JavaScript app.js lo suona.
	EsitoQR string
}

// bottega è il nome del negozio dell'intestazione della bottega (conBottega).
func (d datiHome) bottega() string { return d.Lista.Bottega }

// home è la pagina iniziale. Con ?esito=<id> (il redirect dopo una busta nuova) la prima GET mostra
// anche «Busta 0045/26 salvata» (D-64) e il link della busta (esiti.go), come il flash e public_link/1 sulla
// lista dello standalone, e quella risposta non resta nella cache del browser. Una HEAD non consuma
// l'esito.
func (f *facciaWeb) home(w http.ResponseWriter, r *http.Request) {
	q := r.URL.RawQuery
	f.disegnaLista(w, r, buste.LeggiFiltri(func(nome string) (string, bool) { return testoPlug(q, nome) }), chiaviRegistroLista(q), true, "")
}

// disegnaLista scrive repair.index_viewed con le chiavi e mostra la lista con i filtri: per la GET
// della home (conEsito: anche l'avviso di una busta appena creata) e per la risposta al POST del
// campo unico (cerca.go, senza redirect: il testo cercato non va nell'indirizzo). esitoQR è l'esito del
// suono di quella ricerca (esitoRicercaScritta), "" per la home.
func (f *facciaWeb) disegnaLista(w http.ResponseWriter, r *http.Request, filtri buste.Filtri, chiavi []string, conEsito bool, esitoQR string) {
	if err := f.buste.RegistraLista(r.Context(), chiavi, contestoDi(r)); err != nil {
		f.registro.Error("lettura della lista non registrata")
		Errore(w, r)
		return
	}
	l, err := f.buste.Lista(r.Context(), filtri)
	if err != nil {
		f.registro.Error("lista delle buste non letta")
		Errore(w, r)
		return
	}
	// Le oreficerie esterne e gli artigiani dei menu del cassetto e dei nomi delle pillole
	// (list_external_oreficerie/0, list_artisans/0 in refresh/1).
	sc, err := f.buste.Scelte(r.Context())
	if err != nil {
		f.registro.Error("scelte della lista non lette")
		Errore(w, r)
		return
	}
	d := datiHome{Lista: datiDellaLista(l, sc, f.orologio.Adesso()), EsitoQR: esitoQR}
	if l.Filtri.Galleria {
		g, err := f.galleriaDi(r, l)
		if err != nil {
			f.registro.Error("galleria della lista non letta")
			Errore(w, r)
			return
		}
		d.Lista.Galleria = g
	}
	info := ""
	if id := r.URL.Query().Get("esito"); conEsito && id != "" && r.Method == http.MethodGet {
		st := statoDi(r)
		if e, ok := f.esiti.prendi(id, st.dati.Utente, st.dati.Nonce); ok {
			info = fmt.Sprintf("Busta %s salvata", e.Numero) // D-64, studio punto (d): lo standalone dice «Riparazione N creata.»
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
func datiDellaLista(l buste.Lista, sc buste.Scelte, adesso time.Time) datiLista {
	c, f := l.Contatori, l.Filtri
	d := datiLista{Bottega: l.NomeNegozio, Totale: l.Totale, Pagina: f.Pagina, Pagine: l.Pagine, Attivo: f.Attivo(),
		Cerca: f.Cerca, Report: "/repairs/report.pdf"}
	if f.Attivo() {
		d.Pillole = pilloleDi(f, sc, l.GiorniScadenza)
	}
	if q := parametriLink(f, 0); len(q) > 0 {
		d.Report += "?" + codificaQuery(q)
	}
	for _, x := range parametriLista(f, 0) {
		if x.Nome != "cerca" {
			d.Nascosti = append(d.Nascosti, x)
		}
	}
	d.Cassetto = cassettoDi(f, sc)
	d.VistaLista, d.VistaGalleria = indirizzoVista(f, false), indirizzoVista(f, true)
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
	d.Ritorno, d.Indirizzo = ritornoLista(f)
	_, perEtichetta := f.Etichetta()
	for _, r := range l.Righe {
		d.Righe = append(d.Righe, rigaDellaLista(r, adesso, perEtichetta))
	}
	return d
}

// parametriLista è filter_query/3: i filtri nel loro ordine, i valori vuoti tolti, data_campo solo con
// una data, poi la pagina se pagina > 0.
func parametriLista(f buste.Filtri, pagina int64) []coppiaQuery {
	var q []coppiaQuery
	metti := func(k, v string) {
		if v != "" {
			q = append(q, coppiaQuery{k, v})
		}
	}
	id := func(n int64) string {
		if n == 0 {
			return ""
		}
		return strconv.FormatInt(n, 10)
	}
	uno := func(b bool) string {
		if b {
			return "1"
		}
		return ""
	}
	metti("cerca", f.Cerca)
	metti("stato", f.Stato)
	metti("oreficeria", id(f.Oreficeria))
	metti("artigiano", id(f.Artigiano))
	metti("scadenza", uno(f.Scadenza))
	metti("compro_oro", uno(f.CompraOro))
	metti("attesa", uno(f.Attesa))
	metti("fuori_bottega", uno(f.FuoriBottega))
	metti("senza_qr", uno(f.SenzaQR))
	metti("messaggi", uno(f.Messaggi))
	metti("notifiche", uno(f.Notifiche))
	metti("data_da", f.DataDa)
	metti("data_a", f.DataA)
	if f.DataDa != "" || f.DataA != "" {
		metti("data_campo", campoData(f.DataCampo))
	}
	if f.Galleria {
		metti("vista", "galleria")
	}
	if pagina > 0 {
		metti("p", strconv.FormatInt(pagina, 10))
	}
	return q
}

// parametriLink sono i parametri dei link della lista (paginazione, «⧉ Schede PDF»): quelli di
// filter_query/3, ma senza cerca quando il testo ha la forma esatta di un codice d'etichetta
// RIP-AAAA-NNNN (buste.EtichettaDi): un codice letto dalla pistola non va mai in un indirizzo (D-63,
// review della 052). Per ogni altro testo i link portano la ricerca come nella 051.
func parametriLink(f buste.Filtri, pagina int64) []coppiaQuery {
	if _, _, ok := buste.EtichettaDi(f.Cerca); ok {
		f.Cerca = ""
	}
	return parametriLista(f, pagina)
}

// ritornoLista è la lista dei filtri f, alla sua pagina, come query (senza «?») e come indirizzo:
// list_path/1 dello standalone (slice 187), «/» senza niente. I parametri sono quelli dei link
// (parametriLink: mai un codice d'etichetta, D-63), la pagina solo dalla 2, in ordine di nome come
// Plug.Conn.Query.encode/1 su una mappa, così una lista senza parametri sbagliati ha lo stesso
// indirizzo nelle due app.
func ritornoLista(f buste.Filtri) (string, string) {
	pagina := int64(0)
	if f.Pagina > 1 {
		pagina = f.Pagina
	}
	q := parametriLink(f, pagina)
	if len(q) == 0 {
		return "", "/"
	}
	slices.SortStableFunc(q, func(a, b coppiaQuery) int { return strings.Compare(a.Nome, b.Nome) })
	c := codificaQuery(q)
	return c, "/?" + c
}

// campoData è date_field_value/1: la chiave del campo, l'accettazione se non si conosce.
func campoData(k string) string {
	switch k {
	case "pronta", "consegna", "consegnata":
		return k
	}
	return buste.CampoDataPredefinito
}

// codificaQuery è Plug.Conn.Query.encode/1 su una lista di coppie di testi (URI.encode_www_form/1:
// tutto tranne lettere, cifre e «-._~» in %XX, lo spazio come «+»).
func codificaQuery(q []coppiaQuery) string {
	parti := make([]string, len(q))
	for i, x := range q {
		parti[i] = url.QueryEscape(x.Nome) + "=" + url.QueryEscape(x.Valore)
	}
	return strings.Join(parti, "&")
}

// indirizzoVista è view_path/2: gli stessi filtri (quelli dei link, parametriLink) senza la pagina,
// nella lista o nella galleria; «/» senza niente.
func indirizzoVista(f buste.Filtri, galleria bool) string {
	f.Galleria = galleria
	if q := parametriLink(f, 0); len(q) > 0 {
		return "/?" + codificaQuery(q)
	}
	return "/"
}

// indirizzoLista è ~p"/?#{filter_query(filters, page)}": la lista con tutti i filtri e la pagina.
func indirizzoLista(f buste.Filtri, pagina int64) string {
	return "/?" + codificaQuery(parametriLink(f, pagina))
}

// etichettaStatoLista è status_label/1 della lista: la parola italiana dei dieci stati, lo stato com'è
// per tutto il resto.
func etichettaStatoLista(stato string) string {
	switch stato {
	case "in_lavorazione":
		return "In lavorazione"
	case "in_attesa_materiali":
		return "In attesa materiali"
	case "preventivo":
		return "Preventivo"
	case "pronta":
		return "Pronta"
	case "consegnata":
		return "Consegnata"
	case "annullata":
		return "Annullata"
	case "in_valutazione":
		return "In valutazione"
	case "valutata":
		return "Valutata"
	case "perfezionata":
		return "Perfezionata"
	case "chiusa":
		return "Chiusa"
	}
	return stato
}

// pilloleDi sono le pillole di #active-repair-filters, nell'ordine dello standalone. «In scadenza»
// dice la soglia vera della lista, giorni (D-69, Stefano 05/11/2026): lo standalone scrive sempre «7
// giorni».
func pilloleDi(f buste.Filtri, sc buste.Scelte, giorni int) []pillola {
	var p []pillola
	metti := func(si bool, testo, classe string) {
		if si {
			p = append(p, pillola{testo, classe})
		}
	}
	nome := func(id int64, voci []buste.Voce, ripiego string) string {
		for _, v := range voci {
			if v.ID == id {
				return v.Nome
			}
		}
		return ripiego
	}
	giorniTesto := strconv.Itoa(giorni) + " giorni"
	if giorni == 1 {
		giorniTesto = "1 giorno"
	}
	metti(f.Scadenza, "⏰ In scadenza ("+giorniTesto+")", "pill-rossa")
	metti(f.Attesa, "⏳ In attesa cliente", "pill-ambra")
	metti(f.CompraOro, "🥇 Compro oro", "pill-ambra")
	metti(f.FuoriBottega, "📤 Fuori bottega", "pill-blu")
	metti(f.SenzaQR, "⚠ Senza QR", "pill-ambra")
	metti(f.Messaggi, "💬 Con messaggi", "pill-blu")
	metti(f.Notifiche, "🔔 Da leggere", "pill-rossa")
	metti(f.Stato != "", etichettaStatoLista(f.Stato), "pill-ambra")
	metti(f.Oreficeria != 0, nome(f.Oreficeria, sc.Oreficerie, "Oreficeria"), "pill-azzurra")
	metti(f.Artigiano != 0, "🔧 "+nome(f.Artigiano, sc.Artigiani, "Artigiano"), "pill-viola")
	metti(f.Cerca != "", "🔍 "+f.Cerca, "pill-indaco")
	metti(f.DataDa != "" || f.DataA != "", "📅 "+etichettaPeriodo(f), "pill-verde")
	return p
}

// etichettaPeriodo è date_filter_label/1: «Ricezione: 2026-01-01 → …». La data di accettazione si
// chiama «Ricezione» (D-71 estesa al filtro e alla pillola, Stefano 07/11/2026); lo standalone dice
// «Accettazione».
func etichettaPeriodo(f buste.Filtri) string {
	campo := map[string]string{"pronta": "Pronta", "consegna": "Consegna", "consegnata": "Consegnata"}[f.DataCampo]
	if campo == "" {
		campo = "Ricezione"
	}
	estremo := func(s string) string {
		if s == "" {
			return "…"
		}
		return s
	}
	return campo + ": " + estremo(f.DataDa) + " → " + estremo(f.DataA)
}

// cassettoDi scrive il modulo del cassetto con i filtri di adesso.
func cassettoDi(f buste.Filtri, sc buste.Scelte) datiCassetto {
	cerca := f.Cerca
	if _, _, ok := buste.EtichettaDi(cerca); ok {
		cerca = "" // il cassetto è un GET: un codice letto non ci va (parametriLink)
	}
	c := datiCassetto{
		Attesa: f.Attesa, Scadenza: f.Scadenza, Notifiche: f.Notifiche, Galleria: f.Galleria, Cerca: cerca, DataDa: f.DataDa, DataA: f.DataA,
		CompraOro: f.CompraOro, FuoriBottega: f.FuoriBottega, SenzaQR: f.SenzaQR, Messaggi: f.Messaggi,
	}
	for _, s := range buste.TuttiGliStati() {
		c.Stati = append(c.Stati, opzione{s, etichettaStatoLista(s), f.Stato == s})
	}
	for _, v := range sc.Oreficerie {
		c.Oreficerie = append(c.Oreficerie, opzione{strconv.FormatInt(v.ID, 10), v.Nome, f.Oreficeria == v.ID})
	}
	for _, v := range sc.Artigiani {
		c.Artigiani = append(c.Artigiani, opzione{strconv.FormatInt(v.ID, 10), v.Nome, f.Artigiano == v.ID})
	}
	for _, x := range []struct{ k, testo string }{
		{"accettazione", "Ricezione"}, {"pronta", "Pronta"}, {"consegna", "Consegna prevista"}, // D-71
		{"consegnata", "Consegnata (effettiva)"},
	} {
		c.Campi = append(c.Campi, opzione{x.k, x.testo, campoData(f.DataCampo) == x.k})
	}
	return c
}

// rigaDellaLista scrive una busta come il suo riquadro nella lista. perEtichetta: la lista è una
// ricerca per numero d'etichetta, dove una busta senza etichetta c'è solo per il numero interno e
// porta sempre «⚠ senza QR» (slice 055d, Stefano 06/11/2026). D-79 (Stefano 07/11/2026, slice 059):
// fuori da quella ricerca il segno c'è sulla busta aperta senza codice (Repair.missing_qr?/1, come lo
// standalone) e anche su quella aperta con un codice di altro formato (buste.QRAltroFormato).
func rigaDellaLista(r buste.RigaLista, adesso time.Time, perEtichetta bool) rigaHome {
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
	conAzioni(&h, r.Stato, r.CompraOro, strings.TrimPrefix(apri, "Apri busta "))
	if !h.SenzaQR && buste.QRAltroFormato(r.Stato, r.CodiceQR) {
		h.SenzaQR, h.AltroFormato = true, true
	}
	if perEtichetta && !etichetta {
		h.SenzaQR, h.NumeroInterno, h.AltroFormato = true, true, false
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
