package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/printing"
	"github.com/L0cutus/gestrip/internal/stampe"
)

// La stampa diretta (slice 046, D-08 e D-09), solo nel Go: lo standalone ha solo i PDF. Approvata da
// Stefano il 04/11/2026:
//   - GET e POST /settings/stampanti, solo il titolare: per le etichette QR e per l'etichetta busta
//     76×127 una coda dall'elenco di lpstat -e, o «Nessuna (solo PDF)» (il valore iniziale), e le
//     opzioni della stampante (chiave=valore separate da spazi, validate; PageSize mai). Se CUPS non
//     risponde la pagina lo dice e non cade;
//   - POST /labels/stampa (personale): dalla pagina intermedia. Le stesse regole del PDF (limite di
//     100 compreso) e in più l'anno 2024…2099; un solo lavoro lp per il foglio; solo se CUPS lo accetta, nella
//     stessa transazione, contatore max(ultimo, a), anno, formato e label_sheet.printed con coda e
//     lavoro; poi /labels con «Inviate N etichette a <coda> (lavoro <id>).». Se CUPS rifiuta o non
//     risponde niente si salva e il modulo torna con l'errore e i valori del foglio. Il pulsante sta
//     nel modulo GET del PDF (formaction="/labels/stampa"), senza token: porta alla pagina intermedia
//     GET /labels/stampa, che chiede «Stampa N etichette (…) su <coda>?» (con la proposta del D-66 se
//     serve) e ha il modulo POST con CSRF e invio;
//   - POST /repairs/{id}/stampa-etichetta (personale): «🖨 Stampa etichetta» nella scheda, con
//     repair.label_printed solo se CUPS accetta, e l'esito sulla scheda.
//
// Un doppio invio non manda due lavori: il modulo porta un identificativo monouso (invio, come la
// nuova busta: il secondo aspetta il primo e ne mostra l'esito) e, per ogni foglio e ogni busta, un
// blocco in memoria tiene fuori un secondo lavoro finché il primo non ha risposto (due schede aperte
// sulla stessa pagina). Il lavoro non dipende dalla connessione del browser: se il browser se ne va,
// lp e la transazione finiscono lo stesso (context.WithoutCancel).

const (
	msgStampantiSalvate   = "Stampanti salvate."
	msgStampantiNonSalvat = "Stampanti non salvate: riprova."
	// Nei messaggi per chi usa GestRip «stampante», mai «CUPS» (slice 055d, Stefano 06/11/2026, punto
	// 5 della 049): CUPS resta solo nel glossario del manuale e nei registri.
	msgStampaNonAttiva    = "Stampa diretta non disponibile: GestRip è partito senza i comandi delle stampanti."
	msgCodaNonTrovata     = "Questa stampante non c'è più nel computer: scegline una dall'elenco."
	msgCodaSenzaCUPS      = "Le stampanti non rispondono: puoi tenere la stampante salvata o scegliere «Nessuna (solo PDF)»."
	msgCodaNonValida      = "Nome della coda non valido."
	msgNessunaCodaQR      = "Nessuna stampante scelta per le etichette QR: il titolare la sceglie in Impostazioni → Stampanti."
	msgNessunaCodaBusta   = "Nessuna stampante scelta per l'etichetta busta: il titolare la sceglie in Impostazioni → Stampanti."
	msgStampaInCorso      = "Stampa già in corso: aspetta l'esito prima di riprovare."
	msgEtichetteInviate   = "Inviate %d etichette a %s (lavoro %s). Contatore aggiornato: ultimo numero stampato %d."
	msgInviateNonContate  = "Etichette inviate a %s (lavoro %s), ma il contatore non è stato aggiornato: controlla i numeri proposti prima del prossimo foglio."
	msgEtichettaInviata   = "Etichetta inviata a %s (lavoro %s)."
	msgEtichettaNonRegist = "Etichetta inviata a %s (lavoro %s), ma la stampa non è stata registrata."
	msgEtichettaPDF       = "Etichetta non stampata: PDF non disponibile."
)

// msgElencoNonLetto è l'errore della pagina quando l'elenco delle stampanti non si legge (prima la
// pagina mostrava l'errore di CUPS, «CUPS non risponde»).
const msgElencoNonLetto = "Le stampanti non rispondono"

// motivoStampa è la frase che dice perché la stampa non è riuscita.
func motivoStampa(err error) string {
	var r printing.Rifiuto
	switch {
	case errors.Is(err, printing.ErrTempoScaduto):
		return "la stampante non ha risposto in tempo. Il lavoro potrebbe essere partito lo stesso: controlla la stampante prima di riprovare."
	case errors.As(err, &r):
		if r.Motivo != "" {
			return "la stampante ha rifiutato il lavoro (" + r.Motivo + ")."
		}
		return "la stampante ha rifiutato il lavoro."
	case errors.Is(err, printing.ErrNonDisponibile):
		return "la stampante non risponde."
	}
	return "errore della stampa."
}

// stampeInCorso sono i blocchi in memoria: una chiave (un foglio, una busta) presa da un lavoro che
// non ha ancora risposto.
type stampeInCorso struct {
	mu sync.Mutex
	in map[string]bool
}

func nuoveStampeInCorso() *stampeInCorso { return &stampeInCorso{in: map[string]bool{}} }

// prendi prende la chiave; false se un altro lavoro la tiene.
func (s *stampeInCorso) prendi(k string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.in[k] {
		return false
	}
	s.in[k] = true
	return true
}

func (s *stampeInCorso) lascia(k string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.in, k)
}

// L'esito di un invio della stampa, ricordato per il secondo invio dello stesso modulo, porta il suo
// tipo: riuscito (un messaggio di esito), errore, o avviso (il lavoro è partito ma qualcosa non è
// andato: il contatore o la riga non salvati). Errore e avviso si mostrano come un errore. Forma:
// il tipo, un byte zero, il messaggio.
const (
	esitoRiuscito = "r"
	esitoErrore   = "e"
	esitoAvviso   = "a"
)

func esitoStampa(tipo, msg string) string { return tipo + "\x00" + msg }

// leggiEsitoStampa dà il messaggio dell'esito e se è riuscito.
func leggiEsitoStampa(esito string) (msg string, riuscito bool) {
	tipo, msg, ok := strings.Cut(esito, "\x00")
	if !ok {
		return esito, false
	}
	return msg, tipo == esitoRiuscito
}

// invioStampa prenota l'invio del modulo per la sessione. primo: tocca a chi chiama stampare e poi
// chiudere (fine); altrimenti esito è quello del primo invio (che ha già stampato), o "" se il primo
// non ha mandato niente e questa richiesta lo rifà. Senza un identificativo valido niente prenotazione
// (fine non fa niente): restano i blocchi per foglio e per busta.
func (f *facciaWeb) invioStampa(r *http.Request) (primo bool, esito string, fine func(string), err error) {
	invio, _ := ultimo(r, "invio")
	if !reInvio.MatchString(invio) {
		return true, "", func(string) {}, nil
	}
	st := statoDi(r)
	a, primo := f.esiti.prenota("stampa\x00"+invio, st.dati.Utente, st.dati.Nonce)
	if !primo {
		esito, err := f.esiti.aspetta(r.Context(), a)
		if err != nil || esito != "" {
			return false, esito, nil, err
		}
		a, primo = f.esiti.prenota("stampa\x00"+invio, st.dati.Utente, st.dati.Nonce)
		if !primo {
			return false, "", nil, errors.New("invio ancora in corso")
		}
	}
	chiuso := false
	return true, "", func(e string) {
		if !chiuso {
			chiuso = true
			f.esiti.chiudi(a, e)
		}
	}, nil
}

// ---- Impostazioni → Stampanti ----

// documentoStampa è un riquadro della pagina: la coda e le opzioni di un documento.
type documentoStampa struct {
	ID, Nome, Formato       string
	CampoCoda, CampoOpzioni string
	Scelte                  []opzione
	Opzioni                 string
	ErroreCoda, ErroreOpz   string
	Segnaposto              string                 // l'esempio nel campo delle opzioni (segnapostoOpzioni)
	Dichiarate              []printing.OpzioneCoda // le opzioni della coda scelta (lpoptions), per aiuto
}

type datiStampanti struct {
	Disponibile   bool   // la stampa diretta c'è in questo avvio
	ErroreCUPS    string // le stampanti non rispondono: il perché, per chi legge
	NonSceglibili []string
	Documenti     []documentoStampa
}

// codeDiCUPS è l'elenco di lpstat: le code sceglibili, quelle con un nome che il Go non accetta, e
// l'errore (CUPS non risponde) da mostrare.
func (f *facciaWeb) codeDiCUPS(ctx context.Context) (valide, altre []string, errore string) {
	qq, err := f.stampanti.List(ctx)
	if err != nil {
		f.registro.Warn("elenco delle code di CUPS non letto", "errore", err.Error())
		return nil, nil, msgElencoNonLetto
	}
	for _, q := range qq {
		if printing.CodaValida(q.Nome) {
			valide = append(valide, q.Nome)
		} else {
			altre = append(altre, q.Nome)
		}
	}
	return valide, altre, ""
}

// documentiStampa sono i due riquadri con i valori dati e l'elenco delle code; la coda salvata che
// CUPS non elenca più resta sceglibile, segnata.
func (f *facciaWeb) documentiStampa(ctx context.Context, s buste.ScelteStampa, valide []string, cupsOk bool) []documentoStampa {
	dd := []documentoStampa{
		{ID: "qr", Nome: "Etichette QR", Formato: "57×32 mm, o il formato scelto nel foglio", CampoCoda: "coda_qr", CampoOpzioni: "opzioni_qr", Opzioni: s.QR.Opzioni},
		{ID: "busta", Nome: "Etichetta busta", Formato: "76×127 mm", CampoCoda: "coda_busta", CampoOpzioni: "opzioni_busta", Opzioni: s.Busta.Opzioni},
	}
	for i, scelta := range []string{s.QR.Coda, s.Busta.Coda} {
		d := &dd[i]
		d.Scelte = []opzione{{Valore: "", Etichetta: "Nessuna (solo PDF)", Scelta: scelta == ""}}
		for _, q := range valide {
			d.Scelte = append(d.Scelte, opzione{Valore: q, Etichetta: q, Scelta: q == scelta})
		}
		if scelta != "" && !slices.Contains(valide, scelta) {
			nota := " (non trovata nel computer)"
			if !cupsOk {
				nota = " (salvata)"
			}
			d.Scelte = append(d.Scelte, opzione{Valore: scelta, Etichetta: scelta + nota, Scelta: true})
		}
		if scelta != "" && cupsOk {
			if co, ok := f.stampanti.(printing.ConOpzioni); ok {
				if oo, err := co.Opzioni(ctx, scelta); err == nil {
					d.Dichiarate = oo
				}
			}
		}
	}
	for i := range dd {
		dd[i].Segnaposto = segnapostoOpzioni(dd[i].ID, dd[i].Dichiarate)
	}
	return dd
}

// segnapostoPredefinito è l'esempio del campo delle opzioni per tipo di stampante, quando la
// stampante scelta non dice le sue opzioni (slice 055d, punto 5 della 049): per le etichette QR
// 57×32 una stampante termica a rotolo con le etichette staccate (MediaTracking=Gap) e la
// scurezza; per l'etichetta busta 76×127 una termica per etichette grandi, con scurezza e velocità.
var segnapostoPredefinito = map[string]string{
	"qr":    "MediaTracking=Gap Darkness=8",
	"busta": "Darkness=12 PrintSpeed=3",
}

// segnapostoOpzioni è l'esempio del campo delle opzioni del documento id: con le opzioni che la
// stampante scelta dichiara, le prime due che si possono scrivere (LeggiOpzioni le accetta, quindi
// mai PageSize), ognuna con il valore di adesso della stampante (o la prima scelta), così l'esempio
// copiato com'è non cambia niente; altrimenti l'esempio del tipo di stampante (segnapostoPredefinito).
func segnapostoOpzioni(id string, dichiarate []printing.OpzioneCoda) string {
	var coppie []string
	for _, o := range dichiarate {
		valore := o.Predefinita
		if valore == "" && len(o.Scelte) > 0 {
			valore = o.Scelte[0]
		}
		if valore == "" {
			continue
		}
		c := o.Chiave + "=" + valore
		if _, err := printing.LeggiOpzioni(c); err != nil {
			continue
		}
		if coppie = append(coppie, c); len(coppie) == 2 {
			break
		}
	}
	if len(coppie) > 0 {
		return strings.Join(coppie, " ")
	}
	return segnapostoPredefinito[id]
}

// paginaStampanti è GET /settings/stampanti.
func (f *facciaWeb) paginaStampanti(w http.ResponseWriter, r *http.Request) {
	s, err := f.buste.ScelteStampa(r.Context())
	if err != nil {
		f.registro.Error("scelte delle stampanti non lette", "errore", err.Error())
		Errore(w, r)
		return
	}
	f.mostraStampanti(w, r, http.StatusOK, s, nil)
}

// mostraStampanti disegna la pagina con le scelte s; errori per campo (coda_qr, opzioni_qr…).
func (f *facciaWeb) mostraStampanti(w http.ResponseWriter, r *http.Request, codice int, s buste.ScelteStampa, errori map[string]string) {
	d := datiStampanti{Disponibile: f.stampanti != nil}
	var valide []string
	if d.Disponibile {
		valide, d.NonSceglibili, d.ErroreCUPS = f.codeDiCUPS(r.Context())
	}
	d.Documenti = f.documentiStampa(r.Context(), s, valide, d.Disponibile && d.ErroreCUPS == "")
	for i := range d.Documenti {
		d.Documenti[i].ErroreCoda = errori[d.Documenti[i].CampoCoda]
		d.Documenti[i].ErroreOpz = errori[d.Documenti[i].CampoOpzioni]
	}
	f.mostraCon(w, r, "stampanti", codice, "", d)
}

// salvaStampanti è POST /settings/stampanti.
func (f *facciaWeb) salvaStampanti(w http.ResponseWriter, r *http.Request) {
	salvate, err := f.buste.ScelteStampa(r.Context())
	if err != nil {
		f.registro.Error("scelte delle stampanti non lette", "errore", err.Error())
		Errore(w, r)
		return
	}
	campo := func(n string) string { v, _ := ultimo(r, n); return strings.TrimSpace(v) }
	nuove := buste.ScelteStampa{QR: buste.SceltaStampa{Coda: campo("coda_qr"), Opzioni: campo("opzioni_qr")},
		Busta: buste.SceltaStampa{Coda: campo("coda_busta"), Opzioni: campo("opzioni_busta")}}
	errori := map[string]string{}
	var valide []string
	cupsOk := false
	if f.stampanti != nil && (nuove.QR.Coda != "" || nuove.Busta.Coda != "") {
		var e string
		valide, _, e = f.codeDiCUPS(r.Context())
		cupsOk = e == ""
	}
	for _, x := range []struct {
		campo, opz string
		scelta     buste.SceltaStampa
		prima      string
	}{{"coda_qr", "opzioni_qr", nuove.QR, salvate.QR.Coda}, {"coda_busta", "opzioni_busta", nuove.Busta, salvate.Busta.Coda}} {
		coda := x.scelta.Coda
		switch {
		case coda == "":
		case f.stampanti == nil:
			errori[x.campo] = msgStampaNonAttiva
		case !printing.CodaValida(coda):
			errori[x.campo] = msgCodaNonValida
		case cupsOk && !slices.Contains(valide, coda):
			errori[x.campo] = msgCodaNonTrovata
		case !cupsOk && coda != x.prima:
			errori[x.campo] = msgCodaSenzaCUPS
		}
		oo, err := printing.LeggiOpzioni(x.scelta.Opzioni)
		if err != nil {
			errori[x.opz] = testoOpzione(err)
			continue
		}
		if coda != "" && cupsOk && errori[x.campo] == "" {
			if co, ok := f.stampanti.(printing.ConOpzioni); ok {
				// Le opzioni dichiarate dalla stampante, se lpoptions risponde: altrimenti bastano
				// le regole della forma.
				if dich, err := co.Opzioni(r.Context(), coda); err == nil && len(dich) > 0 {
					if err := printing.ControllaOpzioni(oo, dich); err != nil {
						errori[x.opz] = testoOpzione(err)
					}
				}
			}
		}
	}
	if len(errori) > 0 {
		f.mostraStampanti(w, r, http.StatusUnprocessableEntity, nuove, errori)
		return
	}
	st := statoDi(r)
	if err := f.buste.SalvaScelteStampa(r.Context(), nuove, contestoDi(r)); err != nil {
		f.registro.Error("scelte delle stampanti non salvate", "errore", err.Error())
		st.dati.Errore = msgStampantiNonSalvat
	} else {
		st.dati.Info = msgStampantiSalvate
	}
	st.cambiata = true
	f.vaiA(w, r, "/settings/stampanti")
}

// testoOpzione è il messaggio di un'opzione rifiutata, senza il prefisso dell'errore.
func testoOpzione(err error) string {
	s, _ := strings.CutPrefix(err.Error(), printing.ErrOpzioneVietata.Error()+": ")
	return "Opzione non valida: " + s + "."
}

// ---- Etichette QR: «🖨 Stampa su <coda>» ----

// opzioniDi sono le opzioni salvate per lp (già valide: ScelteStampa scarta quelle guaste).
func opzioniDi(s buste.SceltaStampa) []printing.Opzione {
	oo, _ := printing.LeggiOpzioni(s.Opzioni)
	return oo
}

// stampaEtichette è POST /labels/stampa.
func (f *facciaWeb) stampaEtichette(w http.ResponseWriter, r *http.Request) {
	foglio, err := buste.LeggiFoglio(parametroDelModulo(r.PostForm), f.annoLocale())
	if err == nil && !buste.AnnoDelModulo(foglio.Anno) {
		err = buste.ErrRichiestaEtichette
	}
	if f.rifiutoEtichette(w, r, err) {
		return
	}
	scelte, err := f.buste.ScelteStampa(r.Context())
	if err != nil {
		f.registro.Error("scelte delle stampanti non lette", "errore", err.Error())
		Errore(w, r)
		return
	}
	if f.stampanti == nil || !scelte.QR.Attiva() {
		f.mostraModuloCon(w, r, http.StatusOK, &foglio, nil, msgNessunaCodaQR)
		return
	}
	// D-66: il modulo della pagina intermedia porta continua=1 (la proposta l'ha già mostrata); un POST
	// senza, se vale la condizione, torna alla pagina intermedia.
	if continua, _ := ultimo(r, "continua"); continua != "1" {
		m, err := f.buste.ModuloEtichette(r.Context())
		if err != nil {
			f.registro.Error("modulo delle etichette non letto", "errore", err.Error())
			Errore(w, r)
			return
		}
		if d := f.datiFoglioChiesto(r, m, foglio); d.NuovoAnno {
			d.CodaQR, d.Invio = scelte.QR.Coda, casuale()
			f.mostraCon(w, r, "etichetteStampa", http.StatusOK, "", d)
			return
		}
	}
	primo, esito, fine, err := f.invioStampa(r)
	switch {
	case err != nil:
		f.mostraModuloCon(w, r, http.StatusOK, &foglio, nil, msgStampaInCorso)
		return
	case !primo:
		// L'esito del primo invio, con il suo tipo: riuscito torna a /labels, altrimenti la pagina del
		// modulo con l'errore e i valori del foglio, come al primo.
		msg, riuscito := leggiEsitoStampa(esito)
		if !riuscito {
			f.mostraModuloCon(w, r, http.StatusOK, &foglio, nil, msg)
			return
		}
		st := statoDi(r)
		st.dati.Info, st.cambiata = msg, true
		f.vaiA(w, r, "/labels")
		return
	}
	defer fine("") // se non è chiuso con un esito, il modulo si può rimandare
	chiave := "foglio\x00" + buste.IdFoglio(foglio) + "\x00" + buste.FormatoG(foglio.L) + "x" + buste.FormatoG(foglio.H)
	if !f.inCorso.prendi(chiave) {
		f.mostraModuloCon(w, r, http.StatusOK, &foglio, nil, msgStampaInCorso)
		return
	}
	defer f.inCorso.lascia(chiave)
	pdf, err := stampe.EtichetteQR(foglio)
	if err != nil {
		f.registro.Error("PDF delle etichette QR non fatto", "errore", err.Error())
		f.mostraModuloCon(w, r, http.StatusOK, &foglio, nil, "Stampa non riuscita: PDF non disponibile. Il contatore non è cambiato.")
		return
	}
	ctx := context.WithoutCancel(r.Context()) // il lavoro finisce anche se il browser se ne va
	coda := scelte.QR.Coda
	id, err := f.stampanti.Print(ctx, coda, pdf, printing.Options{LarghezzaMM: foglio.L, AltezzaMM: foglio.H,
		Extra: opzioniDi(scelte.QR), Titolo: buste.NomeFileEtichetteQR(foglio)})
	if err != nil {
		f.registro.Warn("etichette QR non stampate", "coda", coda, "errore", err.Error())
		msg := "Stampa non riuscita: " + motivoStampa(err) + " Il contatore non è cambiato: puoi riprovare, oppure scaricare il PDF."
		if errors.Is(err, printing.ErrTempoScaduto) {
			// Il lavoro potrebbe essere già nella coda: lo stesso modulo rimandato non lo rimanda.
			fine(esitoStampa(esitoErrore, msg))
		}
		f.mostraModuloCon(w, r, http.StatusOK, &foglio, nil, msg)
		return
	}
	ultimo, err := f.buste.StampaEtichetteAccettata(ctx, foglio, coda, string(id), contestoFoto(r))
	if err != nil {
		f.registro.Error("stampa delle etichette QR non salvata", "coda", coda, "lavoro", string(id), "errore", err.Error())
		// La traccia del lavoro partito, fuori dalla transazione e senza garanzia.
		if err := f.buste.StampaEtichetteNonSalvata(ctx, foglio, coda, string(id), contestoFoto(r)); err != nil {
			f.registro.Error("traccia della stampa delle etichette QR non scritta", "coda", coda, "lavoro", string(id), "errore", err.Error())
		}
		msg := fmt.Sprintf(msgInviateNonContate, coda, id)
		fine(esitoStampa(esitoAvviso, msg)) // un secondo invio dello stesso modulo non rimanda il lavoro
		f.mostraModuloCon(w, r, http.StatusOK, &foglio, nil, msg)
		return
	}
	msg := fmt.Sprintf(msgEtichetteInviate, foglio.A-foglio.Da+1, coda, id, ultimo)
	fine(esitoStampa(esitoRiuscito, msg))
	st := statoDi(r)
	st.dati.Info, st.cambiata = msg, true
	f.vaiA(w, r, "/labels")
}

// codaEtichetteQR è la coda della stampa diretta delle etichette QR, "" se non c'è.
func (f *facciaWeb) codaEtichetteQR(r *http.Request) string {
	if f.stampanti == nil {
		return ""
	}
	s, err := f.buste.ScelteStampa(r.Context())
	if err != nil {
		f.registro.Error("scelte delle stampanti non lette", "errore", err.Error())
		return ""
	}
	return s.QR.Coda
}

// paginaStampaEtichette è GET /labels/stampa (solo Go): la pagina intermedia della stampa diretta. Le
// regole del PDF (400, 422) e l'anno del modulo (400), poi «Stampa N etichette (…) su <coda>?» con
// il modulo POST (CSRF, invio monouso, continua=1) e «Annulla»; se vale la D-66 la proposta viene
// prima della domanda. Il token CSRF non va mai in un indirizzo GET (cronologia, log dei proxy,
// Referer): sta solo nel modulo POST di questa pagina.
func (f *facciaWeb) paginaStampaEtichette(w http.ResponseWriter, r *http.Request) {
	foglio, err := buste.LeggiFoglio(parametroDellaQuery(r.URL.RawQuery), f.annoLocale())
	if err == nil && !buste.AnnoDelModulo(foglio.Anno) {
		err = buste.ErrRichiestaEtichette
	}
	if f.rifiutoEtichette(w, r, err) {
		return
	}
	coda := f.codaEtichetteQR(r)
	if coda == "" {
		f.mostraModuloCon(w, r, http.StatusOK, &foglio, nil, msgNessunaCodaQR)
		return
	}
	m, err := f.buste.ModuloEtichette(r.Context())
	if err != nil {
		f.registro.Error("modulo delle etichette non letto", "errore", err.Error())
		Errore(w, r)
		return
	}
	d := f.datiFoglioChiesto(r, m, foglio)
	d.CodaQR, d.Invio = coda, casuale()
	f.mostraCon(w, r, "etichetteStampa", http.StatusOK, "", d)
}

// ---- Etichetta busta: «🖨 Stampa etichetta» ----

// stampaEtichettaBusta è POST /repairs/{id}/stampa-etichetta.
func (f *facciaWeb) stampaEtichettaBusta(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	}
	conSegno := f.segnoDelModulo(r, id)
	scelte, err := f.buste.ScelteStampa(r.Context())
	if err != nil {
		f.registro.Error("scelte delle stampanti non lette", "errore", err.Error())
		Errore(w, r)
		return
	}
	if f.stampanti == nil || !scelte.Busta.Attiva() {
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgNessunaCodaBusta)
		return
	}
	primo, esito, fine, err := f.invioStampa(r)
	switch {
	case err != nil:
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgStampaInCorso)
		return
	case !primo:
		msg, riuscito := leggiEsitoStampa(esito)
		st := statoDi(r)
		if riuscito {
			st.dati.Info = msg
		} else {
			st.dati.Errore = msg
		}
		st.cambiata = true
		f.vaiA(w, r, f.dopoRifiuto(r, id, conSegno))
		return
	}
	defer fine("")
	chiave := "busta\x00" + strconv.FormatInt(id, 10)
	if !f.inCorso.prendi(chiave) {
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgStampaInCorso)
		return
	}
	defer f.inCorso.lascia(chiave)
	pdf, e, err := f.buste.EtichettaDaStampare(r.Context(), strconv.FormatInt(id, 10), stampe.Etichetta)
	switch {
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	case err != nil:
		f.registro.Error("PDF dell'etichetta da stampare non fatto", "errore", err.Error())
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msgEtichettaPDF)
		return
	}
	ctx := context.WithoutCancel(r.Context())
	coda := scelte.Busta.Coda
	lavoro, err := f.stampanti.Print(ctx, coda, pdf, printing.Options{LarghezzaMM: stampe.LarghezzaEtichetta,
		AltezzaMM: stampe.AltezzaEtichetta, Extra: opzioniDi(scelte.Busta), Titolo: buste.NomeFileEtichetta(e.Numero)})
	if err != nil {
		f.registro.Warn("etichetta busta non stampata", "coda", coda, "errore", err.Error())
		msg := "Etichetta non stampata: " + motivoStampa(err) + " Puoi aprire il PDF con «🖨 Etichetta busta»."
		if errors.Is(err, printing.ErrTempoScaduto) {
			fine(esitoStampa(esitoErrore, msg)) // il lavoro potrebbe essere già nella coda
		}
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msg)
		return
	}
	if err := f.buste.StampaEtichettaAccettata(ctx, e, coda, string(lavoro), contestoFoto(r)); err != nil {
		f.registro.Error("stampa dell'etichetta busta non registrata", "coda", coda, "lavoro", string(lavoro), "errore", err.Error())
		if err := f.buste.StampaEtichettaNonSalvata(ctx, e, coda, string(lavoro), contestoFoto(r)); err != nil {
			f.registro.Error("traccia della stampa dell'etichetta busta non scritta", "coda", coda, "lavoro", string(lavoro), "errore", err.Error())
		}
		msg := fmt.Sprintf(msgEtichettaNonRegist, coda, lavoro)
		fine(esitoStampa(esitoAvviso, msg))
		f.vaiAConErrore(w, r, f.dopoRifiuto(r, id, conSegno), msg)
		return
	}
	msg := fmt.Sprintf(msgEtichettaInviata, coda, lavoro)
	fine(esitoStampa(esitoRiuscito, msg))
	st := statoDi(r)
	st.dati.Info, st.cambiata = msg, true
	f.vaiA(w, r, f.allaScheda(r, id, ""))
}

// codaEtichettaBusta è la coda della stampa dell'etichetta busta per la scheda, "" se non c'è.
func (f *facciaWeb) codaEtichettaBusta(ctx context.Context) string {
	if f.stampanti == nil {
		return ""
	}
	s, err := f.buste.ScelteStampa(ctx)
	if err != nil {
		f.registro.Error("scelte delle stampanti non lette", "errore", err.Error())
		return ""
	}
	return s.Busta.Coda
}

// registraStampanti registra le rotte della stampa diretta.
func (f *facciaWeb) registraStampanti(mux *http.ServeMux) {
	f.rottaConGuscio(mux, "GET /settings/stampanti", soloTitolare, f.paginaStampanti)
	f.rottaConGuscio(mux, "POST /settings/stampanti", soloTitolare, f.salvaStampanti)
	f.rottaConGuscio(mux, "GET /labels/stampa", personale, f.paginaStampaEtichette)
	f.rottaConGuscio(mux, "POST /labels/stampa", personale, f.stampaEtichette)
	f.rotta(mux, "POST /repairs/{id}/stampa-etichetta", personale, f.stampaEtichettaBusta)
}
