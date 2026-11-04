package web

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/stampe"
)

// Le etichette QR prenumerate (RIP-S-47, RIP-S-48), come LabelSheetController dello standalone
// (slice 210), con le deviazioni approvate da Stefano il 04/11/2026:
//   - GET /labels: il modulo (new/2), con il guscio; il pulsante è «Genera PDF» e porta a
//     GET /labels/genera (D-08), non direttamente al PDF;
//   - GET /labels.pdf: il PDF (show/2) come allegato «etichette-qr-<anno>-<da>-<a>.pdf», private,
//     no-store; 400 «Richiesta non valida» senza tipo (send_resp); oltre 100 etichette 422 con il
//     modulo, la richiesta com'era e «Massimo 100 etichette per volta: hai chiesto N.»; 503 «PDF non
//     disponibile». Salva formato e label_sheet.generated, **non** il contatore (D-08);
//   - GET /labels/genera (solo Go, D-08): le stesse regole del PDF; se il foglio è valido, la pagina con
//     «⬇ Scarica PDF» e la domanda «Etichette stampate correttamente?», con «Sì, stampate» (POST
//     /labels/conferma) e «No» (la stessa pagina con stampate=no: niente salvato, si può riscaricare);
//   - POST /labels/conferma (solo Go, D-08): CSRF, le stesse regole (limite compreso: una richiesta
//     forgiata non passa), poi buste.ConfermaEtichette e ritorno a /labels con l'esito;
//   - POST /labels/azzera (solo Go, D-22 e D-66): solo il titolare; il dipendente torna a /labels con
//     «Solo il titolare può azzerare il contatore.» e niente cambia. Senza conferma=1 è la pagina
//     della domanda («Sì, azzera», «Annulla»): azzera solo il POST con conferma=1.

const (
	msgSoloTitolareAzzera = "Solo il titolare può azzerare il contatore."
	// confermaAzzeraEtichette è la domanda della pagina di conferma dell'azzeramento (D-22).
	confermaAzzeraEtichette = "Azzerare il contatore? Il prossimo foglio ripartirà da 1."
	msgEtichetteConfermate  = "Contatore aggiornato: ultimo numero stampato %d."
	msgEtichetteNonSalvate  = "Contatore non aggiornato: riprova."
	msgEtichetteAzzerate    = "Contatore azzerato: il prossimo foglio parte da 1."
	msgEtichetteNonAzzerate = "Contatore non azzerato: riprova."
)

// datiEtichette è la pagina del modulo (etichette) e quella del PDF pronto (etichetteGenerate).
type datiEtichette struct {
	Modulo     buste.ModuloEtichette
	L, H       string // FormatoG dei mm del modulo
	Troppe     string // il messaggio del 422, "" se non c'è
	Titolare   bool
	Foglio     buste.FoglioEtichette
	HrefPDF    template.URL // il PDF del foglio
	HrefNo     template.URL // «No»: la stessa pagina con stampate=no
	LEsatto    string       // i mm del foglio, esatti, per la conferma
	HEsatto    string
	Primo      string // «0045/26»
	Ultimo     string
	Quante     int64
	NonSalvato bool // dopo «No»
	Conferma   string
	NuovoAnno  bool   // nella pagina del PDF pronto e in quella della conferma: la proposta del D-66
	AnnoAzzera string // l'anno per cui si azzera, nei moduli di azzeramento ("" = l'anno di oggi)
}

// parametroDellaQuery legge un parametro della query come Plug (parametroPlug).
func parametroDellaQuery(grezza string) func(string) buste.Parametro {
	return func(nome string) buste.Parametro {
		v, ok := parametroPlug(grezza, nome)
		if !ok {
			return buste.Parametro{}
		}
		s, testo := v.testo()
		return buste.Parametro{Valore: s, Presente: true, Testo: testo}
	}
}

// parametroDelModulo legge un campo del modulo POST: l'ultimo valore; con una chiave «nome[…]» il
// valore non è un testo (Plug ne farebbe una lista o una mappa).
func parametroDelModulo(v url.Values) func(string) buste.Parametro {
	return func(nome string) buste.Parametro {
		for k := range v {
			if strings.HasPrefix(k, nome+"[") {
				return buste.Parametro{Presente: true}
			}
		}
		vv, ok := v[nome]
		if !ok || len(vv) == 0 {
			return buste.Parametro{}
		}
		return buste.Parametro{Valore: vv[len(vv)-1], Presente: true, Testo: true}
	}
}

func (f *facciaWeb) annoLocale() int { return clock.Oggi(f.orologio).Anno }

// paginaEtichette è GET /labels.
// Con ?anno=N (2024…2099, il ritorno dopo un azzeramento dalla pagina del PDF pronto) il modulo propone
// quell'anno; altrimenti l'anno di oggi, come lo standalone, che non legge la query.
func (f *facciaWeb) paginaEtichette(w http.ResponseWriter, r *http.Request) {
	f.mostraModuloEtichette(w, r, http.StatusOK, nil)
}

// annoDelModulo legge un anno scritto come intero nell'intervallo del modulo (2024…2099).
func annoDelModulo(s string, presente bool) (int64, bool) {
	if !presente {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n, err == nil && buste.AnnoDelModulo(n)
}

// mostraModuloEtichette disegna il modulo; con troppe (il 422) i valori sono quelli della richiesta.
func (f *facciaWeb) mostraModuloEtichette(w http.ResponseWriter, r *http.Request, codice int, troppe *buste.TroppeEtichette) {
	m, err := f.buste.ModuloEtichette(r.Context())
	if err != nil {
		f.registro.Error("modulo delle etichette non letto", "errore", err.Error())
		Errore(w, r)
		return
	}
	d := datiEtichette{Titolare: titolareDi(r), Conferma: confermaAzzeraEtichette}
	if a, ok := annoDelModulo(parametroQuery(r.URL.RawQuery, "anno")); ok && troppe == nil {
		m = m.ConAnno(a)
	}
	if troppe != nil {
		m = m.ConFoglio(troppe.Foglio)
		d.Troppe = troppe.Error()
	}
	d.Modulo, d.L, d.H = m, buste.FormatoG(m.L), buste.FormatoG(m.H)
	d.AnnoAzzera = strconv.FormatInt(m.Anno, 10)
	f.mostraCon(w, r, "etichette", codice, "", d)
}

// rifiutoEtichette risponde ai rifiuti della lettura; true se ha risposto.
func (f *facciaWeb) rifiutoEtichette(w http.ResponseWriter, r *http.Request, err error) bool {
	var troppe buste.TroppeEtichette
	switch {
	case err == nil:
		return false
	case errors.As(err, &troppe):
		f.mostraModuloEtichette(w, r, http.StatusUnprocessableEntity, &troppe)
	default:
		h := w.Header()
		h.Set("Cache-Control", cacheBase)
		h["Content-Type"] = nil // send_resp senza tipo, e niente sniffing di net/http
		w.WriteHeader(http.StatusBadRequest)
		// Le intestazioni partono subito, senza Content-Length, come la risposta dello standalone
		// (misurato sul banco): un proxy davanti (il Caddy del banco) che riceve tutto insieme
		// sceglierebbe da sé un text/plain.
		_ = http.NewResponseController(w).Flush()
		_, _ = w.Write([]byte(buste.MsgRichiestaEtichette)) // se il client se n'è andato non c'è niente da fare
	}
	return true
}

// etichetteQR è GET /labels.pdf.
func (f *facciaWeb) etichetteQR(w http.ResponseWriter, r *http.Request) {
	foglio, err := buste.LeggiFoglio(parametroDellaQuery(r.URL.RawQuery), f.annoLocale())
	if f.rifiutoEtichette(w, r, err) {
		return
	}
	pdf, err := f.buste.GeneraEtichette(r.Context(), foglio, contestoFoto(r), stampe.EtichetteQR)
	f.inviaPDF(w, r, pdf, err, "etichette QR", disposizioneAllegato(buste.NomeFileEtichetteQR(foglio)))
}

// queryFoglio è la query del foglio letto, per il PDF e per «No»: i valori letti, i mm esatti.
func queryFoglio(fg buste.FoglioEtichette) string {
	v := url.Values{}
	v.Set("da", strconv.FormatInt(fg.Da, 10))
	v.Set("a", strconv.FormatInt(fg.A, 10))
	v.Set("anno", strconv.FormatInt(fg.Anno, 10))
	v.Set("w", strconv.FormatFloat(fg.L, 'f', -1, 64))
	v.Set("h", strconv.FormatFloat(fg.H, 'f', -1, 64))
	return v.Encode()
}

// etichetteGenerate è GET /labels/genera (D-08).
func (f *facciaWeb) etichetteGenerate(w http.ResponseWriter, r *http.Request) {
	foglio, err := buste.LeggiFoglio(parametroDellaQuery(r.URL.RawQuery), f.annoLocale())
	if f.rifiutoEtichette(w, r, err) {
		return
	}
	no, _ := parametroQuery(r.URL.RawQuery, "stampate")
	m, err := f.buste.ModuloEtichette(r.Context())
	if err != nil {
		f.registro.Error("modulo delle etichette non letto", "errore", err.Error())
		Errore(w, r)
		return
	}
	q := queryFoglio(foglio)
	d := datiEtichette{Foglio: foglio, HrefPDF: template.URL("/labels.pdf?" + q), HrefNo: template.URL("/labels/genera?" + q + "&stampate=no"),
		LEsatto: strconv.FormatFloat(foglio.L, 'f', -1, 64), HEsatto: strconv.FormatFloat(foglio.H, 'f', -1, 64), L: buste.FormatoG(foglio.L), H: buste.FormatoG(foglio.H),
		Primo: buste.NumeroEtichetta(foglio.Anno, foglio.Da), Ultimo: buste.NumeroEtichetta(foglio.Anno, foglio.A),
		Quante: foglio.A - foglio.Da + 1, NonSalvato: no == "no",
		// D-66 sull'anno scelto: il foglio di un anno più nuovo dell'ultimo confermato, con il
		// contatore non a zero, propone l'azzeramento prima della domanda.
		NuovoAnno: m.NuovoAnnoPer(foglio.Anno), Titolare: titolareDi(r), AnnoAzzera: strconv.FormatInt(foglio.Anno, 10)}
	f.mostraCon(w, r, "etichetteGenerate", http.StatusOK, "", d)
}

// confermaEtichette è POST /labels/conferma (D-08): «Sì, stampate».
func (f *facciaWeb) confermaEtichette(w http.ResponseWriter, r *http.Request) {
	foglio, err := buste.LeggiFoglio(parametroDelModulo(r.PostForm), f.annoLocale())
	// La conferma c'è solo nel Go: oltre alle regole del PDF vuole un anno del modulo (2024…2099),
	// così un anno forgiato non ferma la D-66 per sempre.
	if err == nil && !buste.AnnoDelModulo(foglio.Anno) {
		err = buste.ErrRichiestaEtichette
	}
	if f.rifiutoEtichette(w, r, err) {
		return
	}
	st := statoDi(r)
	ultimo, err := f.buste.ConfermaEtichette(r.Context(), foglio, contestoFoto(r))
	if err != nil {
		f.registro.Error("conferma delle etichette non salvata", "errore", err.Error())
		st.dati.Errore = msgEtichetteNonSalvate
	} else {
		st.dati.Info = fmt.Sprintf(msgEtichetteConfermate, ultimo)
	}
	st.cambiata = true
	f.vaiA(w, r, "/labels")
}

// azzeraEtichette è POST /labels/azzera (D-22, D-66), solo per il titolare (azioneDelTitolare). Senza
// conferma=1 mostra la domanda, con «Sì, azzera» e «Annulla»: la conferma esplicita vale anche senza
// JavaScript. Con conferma=1 azzera per l'anno del modulo (2024…2099, o l'anno di oggi) e torna a
// /labels; con l'anno, a /labels?anno=N, così dalla pagina del PDF pronto il modulo propone da = 1
// per l'anno del foglio (il PDF già fatto ha numeri vecchi e va rifatto).
func (f *facciaWeb) azzeraEtichette(w http.ResponseWriter, r *http.Request) {
	nuovo := r.PostForm.Get("nuovo_anno") == "1"
	anno, conAnno := annoDelModulo(r.PostForm.Get("anno"), r.PostForm.Has("anno"))
	if r.PostForm.Get("conferma") != "1" {
		d := datiEtichette{Conferma: confermaAzzeraEtichette, NuovoAnno: nuovo}
		if conAnno {
			d.AnnoAzzera = strconv.FormatInt(anno, 10)
		}
		f.mostraCon(w, r, "etichetteAzzera", http.StatusOK, "", d)
		return
	}
	dove := "/labels"
	if conAnno {
		dove += "?anno=" + strconv.FormatInt(anno, 10)
	} else {
		anno = int64(f.annoLocale())
	}
	st := statoDi(r)
	if err := f.buste.AzzeraEtichette(r.Context(), nuovo, anno, contestoFoto(r)); err != nil {
		f.registro.Error("contatore delle etichette non azzerato", "errore", err.Error())
		st.dati.Errore = msgEtichetteNonAzzerate
	} else {
		st.dati.Info = msgEtichetteAzzerate
	}
	st.cambiata = true
	f.vaiA(w, r, dove)
}

func tornaAlleEtichette(*http.Request) string { return "/labels" }

// registraEtichette registra le rotte delle etichette QR.
func (f *facciaWeb) registraEtichette(mux *http.ServeMux) {
	f.rottaConGuscio(mux, "GET /labels", personale, f.paginaEtichette)
	f.rottaConGuscio(mux, "GET /labels.pdf", personale, f.etichetteQR)
	f.rottaConGuscio(mux, "GET /labels/genera", personale, f.etichetteGenerate)
	f.rottaConGuscio(mux, "POST /labels/conferma", personale, f.confermaEtichette)
	f.azioneDelTitolareConGuscio(mux, "POST /labels/azzera", msgSoloTitolareAzzera, tornaAlleEtichette, f.azzeraEtichette)
}
