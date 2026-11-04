package web

import (
	"errors"
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
// (slice 210). Dalla slice 046 (Stefano, 04/11/2026) il PDF salva il contatore come lo standalone
// (max(ultimo, a), formato e label_sheet.generated): la conferma «Etichette stampate correttamente?»
// della 045 non c'è più. Restano le deviazioni:
//   - GET /labels: il modulo (new/2), con il guscio, verso GET /labels.pdf come lo standalone; con una
//     coda scelta (slice 046) anche «🖨 Stampa su <coda>» (POST /labels/stampa, stampanti.go);
//   - GET /labels.pdf: il PDF (show/2) come allegato «etichette-qr-<anno>-<da>-<a>.pdf», private,
//     no-store; 400 «Richiesta non valida» senza tipo (send_resp); oltre 100 etichette 422 con il
//     modulo, la richiesta com'era e «Massimo 100 etichette per volta: hai chiesto N.»; 503 «PDF non
//     disponibile». D-66: se l'anno del foglio è più nuovo dell'anno salvato e il contatore non è 0, al
//     posto del PDF la pagina «Nuovo anno: ripartire da 1?» (etichetteNuovoAnno), con l'azzeramento per
//     il titolare, l'avviso per il dipendente, e «Continua» (la stessa richiesta con continua=1);
//   - POST /labels/azzera (D-22 e D-66): solo il titolare; il dipendente torna a /labels con
//     «Solo il titolare può azzerare il contatore.» e niente cambia. Senza conferma=1 è la pagina
//     della domanda («Sì, azzera», «Annulla»): azzera solo il POST con conferma=1.

const (
	msgSoloTitolareAzzera = "Solo il titolare può azzerare il contatore."
	// confermaAzzeraEtichette è la domanda della pagina di conferma dell'azzeramento (D-22).
	confermaAzzeraEtichette = "Azzerare il contatore? Il prossimo foglio ripartirà da 1."
	msgEtichetteAzzerate    = "Contatore azzerato: il prossimo foglio parte da 1."
	msgEtichetteNonAzzerate = "Contatore non azzerato: riprova."
)

// datiEtichette è la pagina del modulo (etichette) e quella del nuovo anno (etichetteNuovoAnno).
type datiEtichette struct {
	Modulo     buste.ModuloEtichette
	L, H       string // FormatoG dei mm del modulo
	Troppe     string // il messaggio del 422, "" se non c'è
	Titolare   bool
	Conferma   string
	NuovoAnno  bool   // nella pagina della conferma dell'azzeramento: viene dalla proposta del D-66
	AnnoAzzera string // l'anno per cui si azzera, nei moduli di azzeramento ("" = l'anno di oggi)
	CodaQR     string // la coda della stampa diretta (slice 046), "" se non c'è
	Invio      string // l'identificativo monouso del modulo della stampa (pagina intermedia)
	// La pagina del nuovo anno (D-66): il foglio chiesto, l'ultimo numero e l'anno salvati, e come
	// continuare (il PDF con continua=1, o il modulo della stampa).
	Foglio        buste.FoglioEtichette
	LEsatto       string // i mm del foglio, esatti, per il modulo della stampa
	HEsatto       string
	Primo, Ultimo string // «0045/26»
	Quante        int64
	HrefContinua  template.URL
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
// Con ?anno=N (2024…2099, il ritorno dopo un azzeramento dalla pagina del nuovo anno) il modulo propone
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
	var foglio *buste.FoglioEtichette
	errore := ""
	if troppe != nil {
		foglio, errore = &troppe.Foglio, ""
	}
	f.mostraModuloCon(w, r, codice, foglio, troppe, errore)
}

// mostraModuloCon disegna il modulo con i valori del foglio (se c'è: un 422 o una stampa non
// riuscita) e l'errore della risposta.
func (f *facciaWeb) mostraModuloCon(w http.ResponseWriter, r *http.Request, codice int, foglio *buste.FoglioEtichette, troppe *buste.TroppeEtichette, errore string) {
	m, err := f.buste.ModuloEtichette(r.Context())
	if err != nil {
		f.registro.Error("modulo delle etichette non letto", "errore", err.Error())
		Errore(w, r)
		return
	}
	d := datiEtichette{Titolare: titolareDi(r), Conferma: confermaAzzeraEtichette}
	if a, ok := annoDelModulo(parametroQuery(r.URL.RawQuery, "anno")); ok && foglio == nil {
		m = m.ConAnno(a)
	}
	if foglio != nil {
		m = m.ConFoglio(*foglio)
	}
	if troppe != nil {
		d.Troppe = troppe.Error()
	}
	d.Modulo, d.L, d.H = m, buste.FormatoG(m.L), buste.FormatoG(m.H)
	d.AnnoAzzera = strconv.FormatInt(m.Anno, 10)
	// La stampa diretta (slice 046): con la coda scelta, «🖨 Stampa su <coda>», un secondo pulsante GET
	// verso la pagina intermedia (niente token nel modulo, che resta quello dello standalone).
	d.CodaQR = f.codaEtichetteQR(r)
	f.mostraCon(w, r, "etichette", codice, errore, d)
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
	if continua, _ := parametroQuery(r.URL.RawQuery, "continua"); continua != "1" && f.nuovoAnnoPrima(w, r, foglio) {
		return
	}
	pdf, err := f.buste.GeneraEtichette(r.Context(), foglio, contestoFoto(r), stampe.EtichetteQR)
	f.inviaPDF(w, r, pdf, err, "etichette QR", disposizioneAllegato(buste.NomeFileEtichetteQR(foglio)))
}

// queryFoglio è la query del foglio letto, per «Continua» della pagina del nuovo anno: i valori
// letti, i mm esatti.
func queryFoglio(fg buste.FoglioEtichette) string {
	v := url.Values{}
	v.Set("da", strconv.FormatInt(fg.Da, 10))
	v.Set("a", strconv.FormatInt(fg.A, 10))
	v.Set("anno", strconv.FormatInt(fg.Anno, 10))
	v.Set("w", strconv.FormatFloat(fg.L, 'f', -1, 64))
	v.Set("h", strconv.FormatFloat(fg.H, 'f', -1, 64))
	return v.Encode()
}

// datiFoglioChiesto sono i dati delle pagine che vengono prima di un foglio: la pagina del nuovo
// anno (D-66) del PDF e la pagina della stampa diretta.
func (f *facciaWeb) datiFoglioChiesto(r *http.Request, m buste.ModuloEtichette, foglio buste.FoglioEtichette) datiEtichette {
	return datiEtichette{Modulo: m, Foglio: foglio, Titolare: titolareDi(r), AnnoAzzera: strconv.FormatInt(foglio.Anno, 10),
		LEsatto: strconv.FormatFloat(foglio.L, 'f', -1, 64), HEsatto: strconv.FormatFloat(foglio.H, 'f', -1, 64),
		Primo: buste.NumeroEtichetta(foglio.Anno, foglio.Da), Ultimo: buste.NumeroEtichetta(foglio.Anno, foglio.A),
		Quante: foglio.A - foglio.Da + 1, NuovoAnno: buste.AnnoDelModulo(foglio.Anno) && m.NuovoAnnoPer(foglio.Anno)}
}

// nuovoAnnoPrima è la pagina del D-66 prima del PDF: se l'anno del foglio (2024…2099) è più nuovo
// dell'anno salvato e il contatore non è 0, la disegna e dice true. Il titolare ha l'azzeramento (la
// pagina della conferma, poi /labels?anno=N) e «Continua senza azzerare»; il dipendente l'avviso e
// «Continua». Continuare rifà la richiesta con continua=1. La stampa diretta ha la proposta nella sua
// pagina intermedia (paginaStampaEtichette).
func (f *facciaWeb) nuovoAnnoPrima(w http.ResponseWriter, r *http.Request, foglio buste.FoglioEtichette) bool {
	m, err := f.buste.ModuloEtichette(r.Context())
	if err != nil {
		f.registro.Error("modulo delle etichette non letto", "errore", err.Error())
		Errore(w, r)
		return true
	}
	d := f.datiFoglioChiesto(r, m, foglio)
	if !d.NuovoAnno {
		return false // un anno fuori dal modulo (20267) non propone niente e non si salva come anno
	}
	d.HrefContinua = template.URL("/labels.pdf?" + queryFoglio(foglio) + "&continua=1")
	f.mostraCon(w, r, "etichetteNuovoAnno", http.StatusOK, "", d)
	return true
}

// azzeraEtichette è POST /labels/azzera (D-22, D-66), solo per il titolare (azioneDelTitolare). Senza
// conferma=1 mostra la domanda, con «Sì, azzera» e «Annulla»: la conferma esplicita vale anche senza
// JavaScript. Con conferma=1 azzera per l'anno del modulo (2024…2099, o l'anno di oggi) e torna a
// /labels; con l'anno, a /labels?anno=N, così dalla pagina del nuovo anno il modulo propone da = 1
// per l'anno del foglio.
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
	f.azioneDelTitolareConGuscio(mux, "POST /labels/azzera", msgSoloTitolareAzzera, tornaAlleEtichette, f.azzeraEtichette)
}
