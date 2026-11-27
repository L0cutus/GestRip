package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/jobs"
)

// La pagina «Promemoria e job» (slice 098, JOB-05, JOB-06), come JobsLive dello standalone (go-baseline), senza
// LiveView, solo per il titolare (live_session :owner):
//   - GET /settings/promemoria: i tre lavori (nome, «▶ Esegui adesso», la frase, «Ultima esecuzione», «Attivo»,
//     «Ora (Europe/Rome)», la soglia), «Salva cadenze e soglie» e lo storico delle ultime 20 esecuzioni;
//   - POST /settings/promemoria è l'evento save (jobs[<campo>], gli stessi nomi dello standalone): riuscito, ritorno
//     alla pagina (302) con «Impostazioni salvate.»; rifiutato, la pagina ridisegnata (200) con l'errore sotto ogni
//     campo e i valori mandati; un modulo senza la mappa jobs non salva niente (catch-all di handle_event);
//   - POST /settings/promemoria/esegui è l'evento run (campo job): solo i tre nomi dei lavori; eseguito, ritorno alla
//     pagina con «Job eseguito.» (l'esito si legge nello storico); un altro nome non fa niente.
// Solo del Go:
//   - D-16: senza un canale attivo la riga «partono solo con email o Telegram attivi» e nessun «▶ Esegui adesso»;
//     il briefing ha il pulsante solo con Telegram. Un «Esegui adesso» mandato lo stesso non scrive righe e lo dice
//     (jobs.ErrSenzaCanali; l'ordine dei controlli è quello di EseguiAMano: prima i canali, poi l'interruttore). Le
//     impostazioni si salvano anche senza canali; le righe importate restano nello storico;
//   - D-102: le sette caselle dei giorni di apertura (jobs[giorni_apertura][], con il nascosto vuoto: un modulo senza
//     nessun giorno spuntato si rifiuta).
// L'esecuzione a mano non si ferma se il browser chiude la richiesta (il processo di LiveView dello standalone va
// avanti): usa il contesto della richiesta senza la sua cancellazione.

// LavoriAutomatici è il pianificatore visto dalla pagina (jobs.Pianificatore).
type LavoriAutomatici interface {
	Pagina(ctx context.Context) (jobs.Pagina, error)
	SalvaConfig(ctx context.Context, m jobs.ModuloConfig, a audit.Contesto) (*jobs.RifiutoConfig, error)
	EseguiAMano(ctx context.Context, nome string, a audit.Contesto) (jobs.Esecuzione, error)
}

const (
	rottaPaginaPromemoria    = "/settings/promemoria"
	introPromemoria          = "I promemoria girano dentro questa installazione: nessun servizio esterno, nessun timer di sistema. Se non hai configurato nessun canale di invio, un job non manda niente e lo dice qui sotto invece di provarci."
	msgPromemoriaSalvate     = "Impostazioni salvate."
	msgJobEseguito           = "Job eseguito."
	msgPromemoriaSenzaCanali = "I promemoria partono solo con email o Telegram attivi: finché non ne accendi uno in Provider non partono e non lasciano righe. Le impostazioni si salvano lo stesso."
	msgBriefingSoloTelegram  = "Il briefing parte solo con Telegram attivo."
	msgEseguiSenzaCanali     = "Niente è stato eseguito: i promemoria partono solo con email o Telegram attivi."
	msgBriefingNonEseguito   = "Niente è stato eseguito: il briefing parte solo con Telegram attivo."
	msgEseguiNonRiuscito     = "Non è stato possibile avviare il promemoria: riprova."
	msgEseguiCopiaIsolata    = "Niente è stato eseguito: questa è una copia isolata. I promemoria ripartono dopo la conferma del ripristino e il riavvio di GestRip."
	msgPromemoriaNonRiconosc = "Richiesta non riconosciuta: nessuna impostazione salvata."
	msgPromemoriaNonSalvate  = "Impostazioni non salvate: riprova."
	introGiorniApertura      = "I promemoria automatici partono solo nei giorni spuntati; «Esegui adesso» funziona sempre."
)

// I testi per lavoro di JobsLive: la frase (blurb/1) e l'etichetta della soglia (threshold_label/1).
var (
	frasiLavori = map[string]string{
		jobs.Ritardi:   "Segnala al negozio le buste la cui data di consegna è passata. Ogni busta viene segnalata una volta sola.",
		jobs.Solleciti: "Segnala al negozio le buste ferme dall'artigiano oltre la soglia, raggruppate per artigiano. Il sollecito all'artigiano resta un gesto tuo, dalla pagina Solleciti.",
		jobs.Briefing:  "Manda ogni mattina al bot Telegram del negozio l'elenco delle buste da avvisare, in ritardo, in scadenza e ferme dall'artigiano. Va spento finché non lo attivi e serve un bot configurato in Provider. Si ripete ogni giorno finché le buste non cambiano stato.",
	}
	soglieLavori = map[string]struct {
		etichetta, minimo, massimo string
	}{
		jobs.Ritardi:   {"Giorni di ritardo", "1", "365"},
		jobs.Solleciti: {"Giorni fuori bottega", "1", ""},
		jobs.Briefing:  {"Fuori bottega (0 = escludi)", "0", "365"},
	}
	nomiGiorni  = [8]string{1: "Lunedì", 2: "Martedì", 3: "Mercoledì", 4: "Giovedì", 5: "Venerdì", 6: "Sabato", 7: "Domenica"}
	classiEsito = map[string]string{jobs.EsitoOK: "badge-ok", jobs.EsitoSaltato: "badge-attesa", jobs.EsitoFallito: "badge-errore"}
)

type lavoroPromemoria struct {
	Nome, Etichetta, Frase, Ultima        string
	Esegui                                bool
	Avviso                                string // la riga del briefing senza Telegram (D-16), "" se non serve
	Attivo                                bool
	Ora, Soglia                           string
	EtichettaSoglia, Minimo, Massimo      string
	ErroriAttivo, ErroriOra, ErroriSoglia []string
}

type giornoPromemoria struct {
	Numero   int
	Nome     string
	Spuntato bool
}

type rigaPromemoria struct {
	ID                                              int64
	Lavoro, Esito, Classe, Quando, Avvio, Dettaglio string
	Avvisi, Messaggi                                int
}

type datiPromemoria struct {
	Intro, SenzaCanali, IntroGiorni string
	Lavori                          []lavoroPromemoria
	Giorni                          []giornoPromemoria
	ErroriGiorni                    []string
	Storico                         []rigaPromemoria
}

// mostraPromemoria disegna la pagina; rif, se c'è, dà i valori e gli errori del modulo rifiutato.
func (f *facciaWeb) mostraPromemoria(w http.ResponseWriter, r *http.Request, errore string, rif *jobs.RifiutoConfig) {
	if f.lavori == nil {
		Errore(w, r)
		return
	}
	pg, err := f.lavori.Pagina(r.Context())
	if err != nil {
		f.registro.Error("pagina dei promemoria non letta", "errore", err.Error())
		Errore(w, r)
		return
	}
	valori, errori := pg.ValoriConfig, map[string][]string{}
	if rif != nil {
		valori, errori = rif.ValoriConfig, rif.Errori
	}
	d := datiPromemoria{Intro: introPromemoria, IntroGiorni: introGiorniApertura, ErroriGiorni: errori[jobs.CampoGiorniApertura]}
	if !pg.ConCanali {
		d.SenzaCanali = msgPromemoriaSenzaCanali
	}
	for _, l := range pg.Lavori {
		s := soglieLavori[l.Nome]
		lp := lavoroPromemoria{Nome: l.Nome, Etichetta: l.Etichetta, Frase: frasiLavori[l.Nome], Ultima: "mai", Esegui: l.Raggiungibile,
			Attivo: valori.Campi[l.Nome+"_enabled"] == "true", Ora: valori.Campi[l.Nome+"_hour"], Soglia: valori.Campi[l.Nome+"_days"],
			EtichettaSoglia: s.etichetta, Minimo: s.minimo, Massimo: s.massimo, ErroriAttivo: errori[l.Nome+"_enabled"],
			ErroriOra: errori[l.Nome+"_hour"], ErroriSoglia: errori[l.Nome+"_days"]}
		if l.Ultima != nil {
			lp.Ultima = clock.Timbro(l.Ultima.EseguitoIl) + " · " + jobs.EtichettaEsito(l.Ultima.Esito)
		}
		if l.Nome == jobs.Briefing && pg.ConCanali && !l.Raggiungibile {
			lp.Avviso = msgBriefingSoloTelegram
		}
		d.Lavori = append(d.Lavori, lp)
	}
	for n := 1; n <= 7; n++ {
		d.Giorni = append(d.Giorni, giornoPromemoria{Numero: n, Nome: nomiGiorni[n], Spuntato: valori.Giorni[n]})
	}
	for _, e := range pg.Storico {
		d.Storico = append(d.Storico, rigaPromemoria{ID: e.ID, Lavoro: jobs.Etichetta(e.Lavoro), Esito: jobs.EtichettaEsito(e.Esito),
			Classe: classiEsito[e.Esito], Quando: clock.Timbro(e.EseguitoIl), Avvio: jobs.EtichettaAvvio(e.Avvio), Dettaglio: e.Dettaglio,
			Avvisi: e.Avvisi, Messaggi: e.Messaggi})
	}
	f.mostraCon(w, r, "promemoria", http.StatusOK, errore, d)
}

func (f *facciaWeb) paginaPromemoria(w http.ResponseWriter, r *http.Request) {
	f.mostraPromemoria(w, r, "", nil)
}

// reCampoPromemoria è un nome del modulo: jobs[<campo>] con, dopo, niente (un testo) o altre parentesi.
var reCampoPromemoria = regexp.MustCompile(`^jobs\[([^\[\]]+)\](.*)$`)

// moduloPromemoria legge jobs[<campo>] (l'ultimo valore, come Plug: il nascosto «false» e poi la casella) e
// jobs[giorni_apertura][]. Il secondo valore dice se il modulo è la mappa jobs (almeno un campo jobs[…], e nessun
// «jobs» o «jobs[]», che Plug farebbe testo o lista). Un campo scritto come lista o mappa arriva nil (non è un testo).
func moduloPromemoria(r *http.Request) (jobs.ModuloConfig, bool) {
	m := jobs.ModuloConfig{Campi: map[string]*string{}}
	mappa := false
	for k, vs := range r.PostForm {
		if k == "jobs" || k == "jobs[]" {
			return m, false
		}
		sotto := reCampoPromemoria.FindStringSubmatch(k)
		if sotto == nil {
			continue
		}
		mappa = true
		if sotto[1] == jobs.CampoGiorniApertura {
			m.ConGiorni = true
			if sotto[2] == "[]" {
				m.Giorni = append(m.Giorni, vs...)
			} else {
				m.Giorni = append(m.Giorni, "non un giorno") // un testo o una mappa: non è la lista delle caselle
			}
			continue
		}
		if sotto[2] != "" {
			m.Campi[sotto[1]] = nil
			continue
		}
		if _, nonTesto := m.Campi[sotto[1]]; nonTesto {
			continue // la forma lista o mappa dello stesso campo vince sempre
		}
		v := vs[len(vs)-1]
		m.Campi[sotto[1]] = &v
	}
	return m, mappa
}

// salvaPromemoria è l'evento save.
func (f *facciaWeb) salvaPromemoria(w http.ResponseWriter, r *http.Request) {
	if f.lavori == nil {
		Errore(w, r)
		return
	}
	m, mappa := moduloPromemoria(r)
	if !mappa {
		f.mostraPromemoria(w, r, msgPromemoriaNonRiconosc, nil)
		return
	}
	rif, err := f.lavori.SalvaConfig(r.Context(), m, contestoDi(r))
	switch {
	case err != nil:
		// Un guasto del database: nel registro solo il genere.
		f.registro.Error("impostazioni dei promemoria non salvate", "tipo", fmt.Sprintf("%T", err))
		f.mostraPromemoria(w, r, msgPromemoriaNonSalvate, nil)
	case rif != nil:
		f.mostraPromemoria(w, r, "", rif)
	default:
		st := statoDi(r)
		st.dati.Info, st.cambiata = msgPromemoriaSalvate, true
		f.vaiA(w, r, rottaPaginaPromemoria)
	}
}

// eseguiPromemoria è l'evento run: solo un nome di lavoro che la pagina disegna; un altro non fa niente.
func (f *facciaWeb) eseguiPromemoria(w http.ResponseWriter, r *http.Request) {
	if f.lavori == nil {
		Errore(w, r)
		return
	}
	nome, _ := ultimo(r, "job")
	st := statoDi(r)
	conosciuto := false
	for _, n := range jobs.Nomi {
		conosciuto = conosciuto || n == nome
	}
	if !conosciuto {
		f.vaiA(w, r, rottaPaginaPromemoria)
		return
	}
	_, err := f.lavori.EseguiAMano(context.WithoutCancel(r.Context()), nome, contestoDi(r))
	switch {
	case err == nil:
		st.dati.Info = msgJobEseguito
	case errors.Is(err, jobs.ErrCopiaIsolata):
		st.dati.Errore = msgEseguiCopiaIsolata
	case errors.Is(err, jobs.ErrLavoroSconosciuto):
		// Un nome senza lavoro registrato (non succede in serve): come un nome inventato.
	case errors.Is(err, jobs.ErrSenzaCanali) && nome == jobs.Briefing:
		st.dati.Errore = msgBriefingNonEseguito
	case errors.Is(err, jobs.ErrSenzaCanali):
		st.dati.Errore = msgEseguiSenzaCanali
	default:
		f.registro.Error("esecuzione a mano non riuscita", "nome", nome, "tipo", fmt.Sprintf("%T", err))
		st.dati.Errore = msgEseguiNonRiuscito
	}
	st.cambiata = true
	f.vaiA(w, r, rottaPaginaPromemoria)
}

// registraPromemoria registra le rotte della pagina (solo il titolare).
func (f *facciaWeb) registraPromemoria(mux *http.ServeMux) {
	f.rottaConGuscio(mux, "GET "+rottaPaginaPromemoria, soloTitolare, f.paginaPromemoria)
	f.rottaConGuscio(mux, "POST "+rottaPaginaPromemoria, soloTitolare, f.salvaPromemoria) // ridisegna dopo un rifiuto
	f.rotta(mux, "POST "+rottaPaginaPromemoria+"/esegui", soloTitolare, f.eseguiPromemoria)
}
