package web

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
)

// La pagina «Testi dei messaggi» (CLI-38, ADM-13, slice 067), come TemplatesLive dello standalone
// (go-baseline), senza LiveView, solo per il titolare:
//   - GET /settings/templates: le tre sezioni della sorgente (WhatsApp con il riquadro di
//     {telegram_optin}, Telegram cliente, email cliente) e per ogni famiglia il titolo, l'uso, i
//     segnaposto con le loro note, la casella con il testo in vigore, «Salva il testo», «Ripristina
//     predefinito» (con la conferma) e l'anteprima sull'esempio; un testo in vigore che non vale più ha
//     il riquadro dell'errore;
//   - POST /settings/templates è «Salva il testo» (save), POST /settings/templates/ripristina è
//     «Ripristina predefinito» (restore: una casella vuota); i campi sono quelli del modulo della
//     sorgente, template[family] e template[text]. Riuscito: la pagina (all'altezza della famiglia) con
//     «Testo salvato: <etichetta>.» o «Testo predefinito ripristinato: <etichetta>.»; rifiutato: la
//     pagina con il testo mandato nella sua casella e «Testo non salvato: <avviso>», «Testo troppo lungo:
//     al massimo 4000 caratteri.» o «Non è stato possibile salvare il testo.». Una famiglia che non c'è
//     non fa niente (draft/1 risponde :error): si torna alla pagina.
// Le righe del browser (\r\n, come le manda un modulo) diventano \n prima di tutto: è il testo che
// LiveView manda allo standalone (FormData della casella). L'anteprima è quella del testo disegnato,
// non mentre si scrive (lo standalone la rifà a ogni tasto: phx-change). Il testo non va mai nel
// registro né nell'audit.

const (
	introTesti     = "Ogni testo vale per una famiglia di messaggi. Lasciando la casella vuota, o con «Ripristina predefinito», torna il testo predefinito. Un testo con un segnaposto non supportato non si salva."
	confermaTesti  = "Rimettere il testo predefinito al posto di quello salvato?"
	senzaAnteprima = "Nessuna anteprima: il testo non è valido."
)

// titoliTesti è @headings: il riquadro il cui titolo dice più del nome nell'avviso.
var titoliTesti = map[buste.Famiglia]string{buste.FamClienteAggiornamento: "Aggiornamento link al cliente (re-invio)"}

// usiTesti è @usages (impostazioni.html:366 e :383).
var usiTesti = map[buste.Famiglia]string{
	buste.FamClienteAggiornamento: `Usato quando si preme "Aggiorna cliente" su una scheda a cui il link è già stato inviato in passato. ` +
		"Per default non include i link social (re-invio più sobrio). Qui lo usa anche l'avviso al cliente di una risposta nella " +
		"conversazione, in qualunque stato.",
	buste.FamOreficeriaLotto: "Inviato come messaggio unico quando si chiude un lotto di pronte da ritirare per un'oreficeria B2B.",
}

// noteCampiTesti è @field_hints.
var noteCampiTesti = map[buste.Famiglia]map[string]string{
	buste.FamLinkStato:            {"stima": "(tempo indicativo; vuoto se la stima è spenta o manca)"},
	buste.FamClienteAggiornamento: {"social": "(ammesso ma non usato di default)", "stima": "(ammesso ma non usato di default)"},
}

// avanzatiTesti è @advanced; noteTesti è @notes.
var (
	avanzatiTesti = map[buste.Famiglia][]string{buste.FamPreventivoConImporto: {"preventivo", "descrizione"}}
	noteTesti     = map[buste.Famiglia][]string{buste.FamOreficeria: {"lavorazione", "importo", "consegna", "note_riga"}}
)

// righeTesti è @rows; le altre famiglie hanno 8 righe.
var righeTesti = map[buste.Famiglia]int{buste.FamPreventivoConImporto: 5, buste.FamPreventivoSenzaImporto: 5, buste.FamProntaCompraOro: 7,
	buste.FamPronta: 7, buste.FamLinkStato: 7, buste.FamClienteAggiornamento: 7, buste.FamOreficeria: 9, buste.FamOreficeriaLotto: 6,
	buste.FamArtigiano: 8}

type campoTesto struct{ Nome, Nota string }

// famigliaTesto è il riquadro family/1 di una famiglia.
type famigliaTesto struct {
	Nome, Titolo, Uso string
	Campi             []campoTesto
	Avanzati, Note    []string
	Errore            string // format_error/2 del testo disegnato
	Testo, Anteprima  string
	Righe             int
	Token, Conferma   string
}

// datiTesti è la pagina.
type datiTesti struct {
	Intro                     string
	WhatsApp, Telegram, Email []famigliaTesto
}

func graffe(nomi []string) []string {
	g := make([]string, len(nomi))
	for i, n := range nomi {
		g[i] = "{" + n + "}"
	}
	return g
}

// riquadroTesto prepara il riquadro della famiglia f con il testo disegnato.
func riquadroTesto(f buste.Famiglia, testo, token string) famigliaTesto {
	q := famigliaTesto{Nome: string(f), Titolo: f.Etichetta(), Uso: usiTesti[f], Avanzati: graffe(avanzatiTesti[f]),
		Note: graffe(noteTesti[f]), Errore: buste.ErroreModello(f, testo), Testo: testo, Righe: 8, Token: token, Conferma: confermaTesti}
	if t, ok := titoliTesti[f]; ok {
		q.Titolo = t
	}
	if n, ok := righeTesti[f]; ok {
		q.Righe = n
	}
	// base_fields/1: {telegram_optin} lo nomina una volta il riquadro della sezione, gli avanzati vanno a parte.
	for _, c := range f.Campi() {
		if c == "telegram_optin" || slices.Contains(avanzatiTesti[f], c) {
			continue
		}
		q.Campi = append(q.Campi, campoTesto{Nome: "{" + c + "}", Nota: noteCampiTesti[f][c]})
	}
	var ok bool
	if q.Anteprima, ok = buste.AnteprimaModello(f, testo); !ok {
		q.Anteprima = senzaAnteprima
	}
	return q
}

// mostraTesti disegna la pagina: ogni casella con il testo in vigore, tranne bozze (il testo mandato
// di un salvataggio rifiutato).
func (f *facciaWeb) mostraTesti(w http.ResponseWriter, r *http.Request, codice int, errore string, bozze map[buste.Famiglia]string) {
	d := datiTesti{Intro: introTesti}
	token := f.csrf.Token(w, r)
	for _, fam := range buste.Famiglie {
		testo, ok := bozze[fam]
		if !ok {
			var err error
			if testo, err = f.buste.TestoModello(r.Context(), fam); err != nil {
				f.registro.Error("testi dei messaggi non letti")
				Errore(w, r)
				return
			}
		}
		q := riquadroTesto(fam, testo, token)
		switch {
		case slices.Contains(buste.FamiglieWhatsApp, fam):
			d.WhatsApp = append(d.WhatsApp, q)
		case slices.Contains(buste.FamiglieTelegram, fam):
			d.Telegram = append(d.Telegram, q)
		default:
			d.Email = append(d.Email, q)
		}
	}
	f.mostraCon(w, r, "testi", codice, errore, d)
}

// paginaTesti è GET /settings/templates.
func (f *facciaWeb) paginaTesti(w http.ResponseWriter, r *http.Request) {
	f.mostraTesti(w, r, http.StatusOK, "", nil)
}

// salvaTesti è POST /settings/templates (save).
func (f *facciaWeb) salvaTesti(w http.ResponseWriter, r *http.Request) {
	testo, _ := ultimo(r, "template[text]")
	f.scriviTesto(w, r, strings.ReplaceAll(testo, "\r\n", "\n"))
}

// ripristinaTesti è POST /settings/templates/ripristina (restore): una casella vuota.
func (f *facciaWeb) ripristinaTesti(w http.ResponseWriter, r *http.Request) {
	f.scriviTesto(w, r, "")
}

// scriviTesto è save/3 di TemplatesLive per la famiglia del modulo.
func (f *facciaWeb) scriviTesto(w http.ResponseWriter, r *http.Request, testo string) {
	nome, _ := ultimo(r, "template[family]")
	fam, ok := buste.FamigliaDi(nome)
	if !ok {
		f.vaiA(w, r, "/settings/templates")
		return
	}
	_, err := f.buste.SalvaModello(r.Context(), fam, testo, contestoDi(r))
	var rifiuto *buste.ModelloRifiutato
	switch {
	case err == nil:
		st := statoDi(r)
		st.dati.Info, st.cambiata = "Testo salvato: "+fam.Etichetta()+".", true
		if strings.TrimSpace(testo) == "" {
			st.dati.Info = "Testo predefinito ripristinato: " + fam.Etichetta() + "."
		}
		f.vaiA(w, r, "/settings/templates#template-section-"+string(fam))
	case errors.As(err, &rifiuto):
		f.mostraTesti(w, r, http.StatusOK, "Testo non salvato: "+rifiuto.Avviso, map[buste.Famiglia]string{fam: testo})
	case errors.Is(err, buste.ErrModelloTroppoLungo):
		f.mostraTesti(w, r, http.StatusOK, "Testo troppo lungo: al massimo "+strconv.Itoa(buste.ModelloLunghezzaMassima)+" caratteri.", map[buste.Famiglia]string{fam: testo})
	default:
		f.registro.Error("testo del messaggio non salvato", "famiglia", string(fam))
		f.mostraTesti(w, r, http.StatusOK, "Non è stato possibile salvare il testo.", map[buste.Famiglia]string{fam: testo})
	}
}
