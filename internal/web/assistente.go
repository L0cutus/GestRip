package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/L0cutus/gestrip/internal/assist"
	"github.com/L0cutus/gestrip/internal/audit"
)

// La pagina «Assistente e dettatura» (slice 107; ADM-11, RIP-H-46 parte impostazioni), come AssistLive dello
// standalone a go-baseline senza LiveView, solo per il titolare, parte configurazione (la raccolta dei dettati
// arriva con la 114):
//   - GET /settings/assistente: i riquadri «Estrazione dal dettato» (indirizzo, modello, chiave mai rimostrata,
//     «✦ Prova il modello») e «Trascrizione del parlato» (indirizzo, chiave, glossario), ognuno con
//     «Configurato» o «Non configurato»;
//   - POST /settings/assistente/estrazione è save_ai, POST /settings/assistente/trascrizione è save_stt:
//     salvato, ritorno alla pagina con «Impostazione salvata.»; rifiutato, la pagina con gli errori sotto i
//     campi e i valori mandati (mai la chiave), l'altro riquadro com'è salvato;
//   - POST /settings/assistente/prova è test: la frase fissa senza clienti al modello configurato (il client
//     della 106) e la pagina con l'esito sotto il pulsante (#ai-probe), come lo standalone lo mostra sul posto.
// Né le chiavi né l'indirizzo vanno nel registro del server; l'audit lo scrive il dominio, con i soli nomi.

// Assistente è la pagina dell'assistente vista dal web (assist.Impostazioni).
type Assistente interface {
	Pagina(ctx context.Context) (assist.PaginaAssistente, error)
	Salva(ctx context.Context, famiglia string, m assist.Modulo, a audit.Contesto) (*assist.Rifiuto, error)
	Prova(ctx context.Context, a audit.Contesto) (assist.Estrazione, error)
	// Il «Dettato AI» della busta nuova (slice 108, dettato.go).
	EstrazioneConfigurata(ctx context.Context) (bool, error)
	EstraiDettato(ctx context.Context, dettato string, negozio func(context.Context) (assist.Negozio, error), a audit.Contesto) (assist.Estrazione, error)
}

const paginaAssistentePercorso = "/settings/assistente"

// I testi di AssistLive.
const (
	introAssistente        = "Questa installazione parla soltanto con i modelli che indichi qui: nessun servizio centrale, nessuna quota, nessun invio a fornitori esterni se non ne scrivi uno. Finché non c'è niente, la pagina di accettazione non mostra né il riquadro del dettato né il microfono."
	testoEstrazione        = "Un server compatibile con le API di chat: Ollama, llama.cpp, vLLM. L'indirizzo è quello della base, senza il percorso."
	testoTrascrizione      = "Il servizio di trascrizione della bottega, quello che espone `POST /stt`: il clip esce da qui e non passa da nessun altro, il glossario viaggia con la richiesta e il modello lo sceglie il server, non questa pagina."
	segnapostoChiave       = "salvata — lascia vuoto per non cambiarla"
	msgImpostazioneSalvata = "Impostazione salvata."
	msgProvaNonConfig      = "Il modello non è configurato: non è stato chiesto niente."
	msgProvaIllegibile     = "Il modello ha risposto qualcosa che non è un oggetto JSON."
	// La prova non arriva mai ai limiti del testo (la frase è fissa): sono «Prova non riuscita: #{inspect(reason)}.».
	msgProvaCorta = "Prova non riuscita: :text_too_short."
	msgProvaLunga = "Prova non riuscita: :text_too_long."
)

// aiutoGlossario è la riga sotto il glossario, con Glossary.cap/0.
var aiutoGlossario = "Una voce per riga, fino a " + strconv.Itoa(assist.MassimoGlossario) +
	" caratteri una volta unite. Più voci non vuol dire meglio: ogni voce attira a sé anche parole che non sono lei."

// campoAssistente è un campo dei moduli (l'input di core_components).
type campoAssistente struct {
	ID, Nome, Etichetta, Valore, Segnaposto string
	Chiave                                  bool // type="password", sempre value=""
	Errori                                  []string
}

// riquadroAssistente è un riquadro della pagina.
type riquadroAssistente struct {
	Configurata bool
	Campi       []campoAssistente // indirizzo, modello o chiave (il glossario è a parte)
	Glossario   campoAssistente
}

// esitoProva è il paragrafo #ai-probe: il testo e se è un errore.
type esitoProva struct {
	Testo  string
	Errore bool
}

// datiAssistente è la pagina.
type datiAssistente struct {
	Intro, TestoEstrazione, TestoTrascrizione, AiutoGlossario string
	Estrazione, Trascrizione                                  riquadroAssistente
	Prova                                                     *esitoProva
}

// titoloPagina è il page_title di AssistLive.
func (datiAssistente) titoloPagina() string { return "Assistente e dettatura" }

func (f *facciaWeb) registraAssistente(mux *http.ServeMux) {
	f.rottaConGuscio(mux, "GET /settings/assistente", soloTitolare, f.paginaAssistente)
	f.rottaConGuscio(mux, "POST /settings/assistente/estrazione", soloTitolare, f.salvaEstrazione)     // ridisegna dopo un rifiuto
	f.rottaConGuscio(mux, "POST /settings/assistente/trascrizione", soloTitolare, f.salvaTrascrizione) // ridisegna dopo un rifiuto
	f.rottaConGuscio(mux, "POST /settings/assistente/prova", soloTitolare, f.provaAssistente)          // l'esito sta nella pagina
}

// riquadroDi prepara un riquadro: i valori salvati, o quelli di un salvataggio rifiutato con i suoi errori. La
// chiave non torna mai: la casella è vuota, il segnaposto dice se ce n'è una salvata.
func riquadroDi(famiglia string, salvata assist.Famiglia, rif *assist.Rifiuto) riquadroAssistente {
	valori := map[string]string{"endpoint": salvata.Indirizzo, "model": salvata.Modello, "glossary": salvata.Glossario}
	errori := map[string][]string{}
	if rif != nil {
		valori, errori = rif.Valori, rif.Errori
	}
	campo := func(nome, etichetta, segnaposto string) campoAssistente {
		return campoAssistente{ID: famiglia + "_" + nome, Nome: famiglia + "[" + nome + "]", Etichetta: etichetta, Valore: valori[nome],
			Segnaposto: segnaposto, Errori: errori[nome]}
	}
	chiave := campo("api_key", "Chiave (se il server la chiede)", "")
	chiave.Valore, chiave.Chiave = "", true
	if salvata.ChiaveSalvata {
		chiave.Segnaposto = segnapostoChiave
	}
	r := riquadroAssistente{Configurata: salvata.Configurata}
	if famiglia == assist.FamigliaEstrazione {
		r.Campi = []campoAssistente{campo("endpoint", "Indirizzo del server", "http://127.0.0.1:11434"), campo("model", "Modello", "qwen2.5:7b"), chiave}
	} else {
		r.Campi = []campoAssistente{campo("endpoint", "Indirizzo del server", "http://127.0.0.1:8900"), chiave}
		r.Glossario = campo("glossary", "Parole di questo negozio", "Una per riga: marche, cognomi, fornitori.")
	}
	return r
}

// mostraAssistente disegna la pagina; rif è un salvataggio rifiutato della famiglia famiglia, prova l'esito
// della prova.
func (f *facciaWeb) mostraAssistente(w http.ResponseWriter, r *http.Request, famiglia string, rif *assist.Rifiuto, prova *esitoProva) {
	if f.assistente == nil {
		Errore(w, r)
		return
	}
	p, err := f.assistente.Pagina(r.Context())
	if err != nil {
		f.registro.Error("pagina dell'assistente non letta")
		Errore(w, r)
		return
	}
	d := datiAssistente{Intro: introAssistente, TestoEstrazione: testoEstrazione, TestoTrascrizione: testoTrascrizione, AiutoGlossario: aiutoGlossario,
		Prova: prova}
	var rifAI, rifSTT *assist.Rifiuto
	switch famiglia {
	case assist.FamigliaEstrazione:
		rifAI = rif
	case assist.FamigliaTrascrizione:
		rifSTT = rif
	}
	d.Estrazione = riquadroDi(assist.FamigliaEstrazione, p.Estrazione, rifAI)
	d.Trascrizione = riquadroDi(assist.FamigliaTrascrizione, p.Trascrizione, rifSTT)
	f.mostraCon(w, r, "assistente", http.StatusOK, "", d)
}

// paginaAssistente è GET /settings/assistente.
func (f *facciaWeb) paginaAssistente(w http.ResponseWriter, r *http.Request) {
	f.mostraAssistente(w, r, "", nil, nil)
}

func (f *facciaWeb) salvaEstrazione(w http.ResponseWriter, r *http.Request) {
	f.salvaAssistente(w, r, assist.FamigliaEstrazione)
}

func (f *facciaWeb) salvaTrascrizione(w http.ResponseWriter, r *http.Request) {
	f.salvaAssistente(w, r, assist.FamigliaTrascrizione)
}

// salvaAssistente è save_ai e save_stt: i campi <famiglia>[…] che il modulo manda (uno che manca resta il
// salvato), più la casella «Cancella la chiave salvata».
func (f *facciaWeb) salvaAssistente(w http.ResponseWriter, r *http.Request, famiglia string) {
	if f.assistente == nil {
		Errore(w, r)
		return
	}
	m := assist.Modulo{}
	for _, c := range append(assist.CampiDi(famiglia), "clear_secret") {
		if v, ok := ultimo(r, famiglia+"["+c+"]"); ok {
			if c == "glossary" {
				v = aCapoUnix(v) // le caselle di più righe interne vanno a «\n» (069c)
			}
			m[c] = v
		}
	}
	rif, err := f.assistente.Salva(r.Context(), famiglia, m, contestoDi(r))
	switch {
	case err != nil:
		f.registro.Error("impostazione dell'assistente non salvata", "famiglia", famiglia)
		Errore(w, r)
	case rif != nil:
		f.mostraAssistente(w, r, famiglia, rif, nil)
	default:
		st := statoDi(r)
		st.dati.Info, st.cambiata = msgImpostazioneSalvata, true
		f.vaiA(w, r, paginaAssistentePercorso)
	}
}

// provaAssistente è test: l'esito come lo scrive AssistLive. Il motivo di un rifiuto del modello è il codice fisso
// della 106 («ai:http:500»), mai testo del server, indirizzo o chiave.
func (f *facciaWeb) provaAssistente(w http.ResponseWriter, r *http.Request) {
	if f.assistente == nil {
		Errore(w, r)
		return
	}
	e, err := f.assistente.Prova(r.Context(), contestoDi(r))
	var em assist.ErrModello
	var esito esitoProva
	switch {
	case err == nil:
		esito.Testo = "Il modello ha risposto: «" + primiGrafemi(e.Proposta.Descrizione, 80) + "»."
	case errors.Is(err, assist.ErrNonConfigurato):
		esito = esitoProva{msgProvaNonConfig, true}
	case errors.As(err, &em):
		esito = esitoProva{"Il modello ha rifiutato: " + em.Motivo + ".", true}
	case errors.Is(err, assist.ErrRispostaIllegibile):
		esito = esitoProva{msgProvaIllegibile, true}
	case errors.Is(err, assist.ErrTestoCorto):
		esito = esitoProva{msgProvaCorta, true}
	case errors.Is(err, assist.ErrTestoLungo):
		esito = esitoProva{msgProvaLunga, true}
	default:
		f.registro.Error("prova del modello non fatta")
		Errore(w, r)
		return
	}
	f.mostraAssistente(w, r, "", nil, &esito)
}
