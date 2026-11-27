// Package jobs è il pianificatore dei lavori automatici del negozio (Jobs e Jobs.Scheduler dello
// standalone; JOB-01…04, D-16, D-17): quali lavori esistono, quando vanno eseguiti, a chi scrivono e
// che riga lasciano nello storico.
//
//   - **Un giro al minuto** (OgniQuanto), dentro gestrip serve, senza timer di sistema: Lavoro aspetta,
//     fa un giro (Giro) e riaspetta, finché il contesto non finisce. Un giro che va in panico o non
//     riesce non ferma il lavoro.
//   - **Quando è dovuto** (Dovuto, due?/2): il lavoro c'è (registrato), oggi il negozio è aperto (D-17,
//     solo il giorno, non le ore), è attivo, l'ora del negozio ha raggiunto quella impostata, ha almeno
//     un canale (D-16), oggi non c'è già un'esecuzione automatica riuscita e quelle di oggi sono meno
//     di TentativiAlGiorno. Tutto sull'orologio del negozio (clock, Europe/Rome, JOB-01).
//   - **A chi scrive** (Destinazioni, destinations/1): il negozio stesso, su ogni canale attivo (email
//     al mittente dell'SMTP, Telegram alla chat del negozio); il briefing solo su Telegram.
//   - **Canali spenti (D-16, solo Go):** senza un canale il lavoro non è dovuto e non lascia righe; anche
//     l'esecuzione a mano dà ErrSenzaCanali senza scrivere niente (nello standalone «Saltato»).
//   - **Storico** (esecuzioni_lavori, JobRun): ogni esecuzione è una riga che non si modifica, con
//     jobs.ran nella stessa transazione. Un lavoro che va in panico o dà un errore è «failed» con
//     DettaglioInterrotto: il testo dell'errore va solo nel registro, e nemmeno lì (solo il tipo).
//   - **Mai due esecuzioni insieme:** il giro e l'esecuzione a mano passano dallo stesso lucchetto, così
//     nel processo un lavoro gira una volta per volta e il «già fatto oggi» si legge dopo l'ultima riga.
//
// I lavori veri (ritardi, solleciti, briefing) si registrano con Nuovo: un nome senza lavoro registrato
// non è mai dovuto e non si esegue.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// I nomi dei lavori, le parole della colonna lavoro (CHECK di esecuzioni_lavori).
const (
	Ritardi   = "overdue"
	Solleciti = "chases"
	Briefing  = "briefing"
)

// Nomi sono i lavori nell'ordine dello standalone (jobs/0): ritardi, solleciti, briefing.
var Nomi = []string{Ritardi, Solleciti, Briefing}

// Avvii ed esiti, le parole delle colonne avvio ed esito.
const (
	AvvioAutomatico = "scheduled"
	AvvioAMano      = "manual"
	EsitoOK         = "ok"
	EsitoSaltato    = "skipped"
	EsitoFallito    = "failed"
)

// TentativiAlGiorno sono le esecuzioni automatiche al più per lavoro e giorno del negozio
// (@scheduled_attempts).
const TentativiAlGiorno = 3

// OgniQuanto è l'intervallo fra due giri (tick_interval_ms: 60_000).
const OgniQuanto = time.Minute

// AttoreScheduler firma le scritture di un'esecuzione automatica (@scheduler_actor).
const AttoreScheduler = "scheduler"

// DettaglioInterrotto è la frase della riga di un lavoro che è andato in panico o ha dato un errore
// (@raised_detail): mai il testo dell'errore, che può avere dati personali.
const DettaglioInterrotto = "Il job si è interrotto. Il dettaglio è nel log dell'applicazione."

// DettaglioSpento è la frase di un lavoro spento dal titolare ed eseguito a mano (JOB-06).
const DettaglioSpento = "Job disattivato dal titolare."

// MassimoDettaglio è la lunghezza massima del dettaglio, in grafemi (JobRun.max_detail/0).
const MassimoDettaglio = 500

var (
	// ErrLavoroSconosciuto: il nome non è uno dei lavori registrati.
	ErrLavoroSconosciuto = errors.New("jobs: lavoro sconosciuto")
	// ErrSenzaCanali: il lavoro non ha nessun canale attivo (D-16); niente è stato scritto.
	ErrSenzaCanali = errors.New("jobs: nessun canale di invio attivo")
	// ErrCopiaIsolata: il serve gira su una copia ripristinata non ancora confermata (slice 131, D-118 (b)):
	// «Esegui adesso» non accoda niente, altrimenti le righe partirebbero dopo la conferma senza che il titolare le
	// abbia viste. Vale fino al riavvio del serve, anche dopo la conferma.
	ErrCopiaIsolata = errors.New("jobs: copia isolata, esecuzioni a mano spente")
)

// Risultato è quello che un lavoro dice di sé (Job.result): Fallito è {:error, …} (un guasto che il
// lavoro ha visto e sa descrivere), Avvisi le buste nominate, Messaggi le righe accodate, Dettaglio la
// frase della pagina.
type Risultato struct {
	Fallito          bool
	Avvisi, Messaggi int
	Dettaglio        string
}

// Lavoro è un lavoro automatico (il behaviour Job): riceve le impostazioni, l'istante su cui leggere il
// calendario, i destinatari già scelti e il contesto d'audit con cui firmare ogni sua scrittura. Un
// errore (o un panico) è un guasto che il lavoro non ha visto: la riga dirà DettaglioInterrotto.
type Lavoro interface {
	Esegui(ctx context.Context, conf Config, adesso time.Time, dest []uscita.Destinazione, a audit.Contesto) (Risultato, error)
}

// Canali dice se un canale della coda in uscita è attivo adesso (uscita.Coda.CanaleAttivo).
type Canali interface {
	CanaleAttivo(ctx context.Context, canale string) (bool, error)
}

// Esecuzione è una riga di esecuzioni_lavori.
type Esecuzione struct {
	ID               int64
	Lavoro           string
	Avvio            string
	Esito            string
	EseguitoIl       time.Time
	Giorno           clock.Data
	Avvisi, Messaggi int
	Dettaglio        string // "" se la colonna è NULL
}

// Pianificatore è il pianificatore di un database: uno solo per processo (gestrip serve).
type Pianificatore struct {
	d      *db.DB
	o      clock.Orologio
	canali Canali
	lavori map[string]Lavoro
	reg    *audit.Registro
	log    *slog.Logger
	// mu tiene in fila giri ed esecuzioni a mano: mai due esecuzioni insieme nel processo, e il «già
	// fatto oggi» di un giro si legge dopo la riga dell'esecuzione precedente.
	mu sync.Mutex
	// isolata: il serve di una copia ripristinata non confermata (ConCopiaIsolata).
	isolata bool
}

// Nuovo è il pianificatore con i lavori registrati per nome (fra Nomi; gli altri si ignorano).
func Nuovo(d *db.DB, o clock.Orologio, canali Canali, lavori map[string]Lavoro) *Pianificatore {
	l := map[string]Lavoro{}
	for _, n := range Nomi {
		if x, ok := lavori[n]; ok && x != nil {
			l[n] = x
		}
	}
	return &Pianificatore{d: d, o: o, canali: canali, lavori: l, reg: audit.Nuovo(d, o), log: slog.New(slog.DiscardHandler)}
}

// ConRegistro dà al pianificatore le righe del server (mai testi d'errore, indirizzi o dati).
func (p *Pianificatore) ConRegistro(l *slog.Logger) *Pianificatore {
	if l != nil {
		p.log = l
	}
	return p
}

// ConCopiaIsolata spegne le esecuzioni a mano (EseguiAMano dà ErrCopiaIsolata): il serve di una copia ripristinata
// ancora isolata (slice 131), dove il giro al minuto non parte.
func (p *Pianificatore) ConCopiaIsolata() *Pianificatore {
	p.isolata = true
	return p
}

// Dovuto è due?/2 adesso.
func (p *Pianificatore) Dovuto(ctx context.Context, nome string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	conf, err := LeggiConfig(ctx, p.d.Lettura)
	if err != nil {
		return false, err
	}
	return p.dovuto(ctx, conf, nome, p.o.Adesso())
}

// dovuto è due?/2 con le impostazioni già lette: prima le domande che non leggono il database.
func (p *Pianificatore) dovuto(ctx context.Context, conf Config, nome string, adesso time.Time) (bool, error) {
	if _, ok := p.lavori[nome]; !ok {
		return false, nil
	}
	if !nellOra(conf, nome, adesso) {
		return false, nil
	}
	dest, err := p.Destinazioni(ctx, nome)
	if err != nil || len(dest) == 0 {
		return false, err
	}
	return tentativiRimasti(ctx, p.d.Lettura, nome, clock.DataDi(adesso))
}

// nellOra è la parte di due?/2 che legge solo le impostazioni e l'orologio del negozio: oggi il negozio è aperto
// (D-17), il lavoro è attivo e l'ora del negozio (con l'ora legale) ha raggiunto quella impostata. La usa anche la
// conferma del ripristino (SegnaDovutiComeFatti, D-119).
func nellOra(conf Config, nome string, adesso time.Time) bool {
	loc := clock.Locale(adesso)
	if !conf.Aperto(loc.Weekday()) || !conf.Attivo(nome) {
		return false
	}
	mezzanotte := time.Date(loc.Year(), loc.Month(), loc.Day(), 0, 0, 0, 0, time.UTC)
	ora := time.Date(loc.Year(), loc.Month(), loc.Day(), loc.Hour(), loc.Minute(), loc.Second(), loc.Nanosecond(), time.UTC)
	return ora.Sub(mezzanotte) >= conf.Ora(nome)
}

// tentativiRimasti è attempts_left?/2: nessuna esecuzione automatica riuscita nel giorno e meno di
// TentativiAlGiorno in tutto. q è il collegamento di lettura o una transazione.
func tentativiRimasti(ctx context.Context, q db.Interrogatore, nome string, giorno clock.Data) (bool, error) {
	var tutte, riuscite int
	err := q.QueryRowContext(ctx, `SELECT count(*), count(*) FILTER (WHERE esito = ?) FROM esecuzioni_lavori
		WHERE lavoro = ? AND avvio = ? AND giorno = ?`, EsitoOK, nome, AvvioAutomatico, giorno.String()).Scan(&tutte, &riuscite)
	if err != nil {
		return false, err
	}
	return riuscite == 0 && tutte < TentativiAlGiorno, nil
}

// pubblico è il destinatario dei lavori: chi li riceve è sempre il negozio stesso.
var pubblico = map[string][]string{Briefing: {uscita.CanaleTelegram}}

// Destinazioni sono i destinatari del lavoro adesso (destinations/1): il negozio su ogni canale attivo
// del suo pubblico (email, poi Telegram; il briefing solo Telegram). Un canale attivo senza indirizzo
// (non succede: attivo vuol dire configurato) non è un destinatario.
func (p *Pianificatore) Destinazioni(ctx context.Context, nome string) ([]uscita.Destinazione, error) {
	canali, ok := pubblico[nome]
	if !ok {
		canali = []string{uscita.CanaleEmail, uscita.CanaleTelegram}
	}
	var dest []uscita.Destinazione
	for _, c := range canali {
		attivo, err := p.canali.CanaleAttivo(ctx, c)
		if err != nil {
			return nil, err
		}
		if !attivo {
			continue
		}
		indirizzo, err := p.indirizzoDelNegozio(ctx, c)
		if err != nil {
			return nil, err
		}
		if indirizzo != "" {
			dest = append(dest, uscita.Destinazione{Canale: c, Destinatario: "shop", Indirizzo: indirizzo})
		}
	}
	return dest, nil
}

// indirizzoDelNegozio è destination_for_shop/1: il mittente dell'SMTP per l'email, la chat del negozio
// per Telegram.
func (p *Pianificatore) indirizzoDelNegozio(ctx context.Context, canale string) (string, error) {
	switch canale {
	case uscita.CanaleEmail:
		c, err := uscita.LeggiSMTP(ctx, p.d.Lettura)
		if err != nil || !c.Usabile() {
			return "", err
		}
		return c.Mittente, nil
	case uscita.CanaleTelegram:
		c, err := uscita.LeggiTelegram(ctx, p.d.Lettura)
		if err != nil || !c.Usabile() {
			return "", err
		}
		return c.Chat, nil
	}
	return "", nil
}

// Giro è run_due/1: ogni lavoro dovuto adesso, nell'ordine di Nomi, con lo stesso istante. Un errore di
// lettura o di scrittura della riga ferma il giro (il lavoro resta dovuto al giro dopo).
func (p *Pianificatore) Giro(ctx context.Context) ([]Esecuzione, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	adesso := p.o.Adesso()
	conf, err := LeggiConfig(ctx, p.d.Lettura)
	if err != nil {
		return nil, err
	}
	var fatte []Esecuzione
	for _, n := range Nomi {
		if ctx.Err() != nil {
			return fatte, ctx.Err()
		}
		dovuto, err := p.dovuto(ctx, conf, n, adesso)
		if err != nil {
			return fatte, err
		}
		if !dovuto {
			continue
		}
		e, scritta, err := p.esegui(ctx, conf, n, AvvioAutomatico, adesso, audit.Contesto{})
		if errors.Is(err, ErrSenzaCanali) {
			continue // i canali spenti fra la domanda e l'esecuzione: niente riga (D-16)
		}
		if err != nil {
			return fatte, err
		}
		if scritta {
			fatte = append(fatte, e)
		}
	}
	return fatte, nil
}

// EseguiAMano è run/3 con l'origine «A mano» (JOB-06, la pagina della 098), adesso e in ogni giorno
// della settimana. Senza canali ErrSenzaCanali (D-16); un nome non registrato ErrLavoroSconosciuto. Aspetta
// il turno se un giro è in corso.
func (p *Pianificatore) EseguiAMano(ctx context.Context, nome string, a audit.Contesto) (Esecuzione, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.isolata {
		return Esecuzione{}, ErrCopiaIsolata
	}
	if _, ok := p.lavori[nome]; !ok {
		return Esecuzione{}, ErrLavoroSconosciuto
	}
	conf, err := LeggiConfig(ctx, p.d.Lettura)
	if err != nil {
		return Esecuzione{}, err
	}
	e, scritta, err := p.esegui(ctx, conf, nome, AvvioAMano, p.o.Adesso(), a)
	if err == nil && !scritta {
		err = ctx.Err()
	}
	return e, err
}

// esegui è run/3: i destinatari (nessuno: ErrSenzaCanali, nessuna riga), l'interruttore del titolare
// (spento: «Saltato»), il lavoro protetto e la riga con jobs.ran. scritta dice se la riga c'è: non c'è
// solo se il lavoro è tornato con l'errore dello stop (il giorno resta da fare); un Risultato, anche
// fallito, si scrive sempre con i suoi conteggi, perché il lavoro ha già fatto qualcosa.
func (p *Pianificatore) esegui(ctx context.Context, conf Config, nome, avvio string, adesso time.Time, a audit.Contesto) (Esecuzione, bool, error) {
	if a.Attore == "" {
		a.Attore = AttoreScheduler
	}
	dest, err := p.Destinazioni(ctx, nome)
	if err != nil {
		return Esecuzione{}, false, err
	}
	if len(dest) == 0 {
		return Esecuzione{}, false, ErrSenzaCanali
	}
	var r Risultato
	esito := EsitoSaltato
	if !conf.Attivo(nome) {
		r = Risultato{Dettaglio: DettaglioSpento}
	} else {
		var interrotto, fermato bool
		r, interrotto, fermato = p.protetto(ctx, nome, conf, adesso, dest, a)
		if fermato {
			return Esecuzione{}, false, nil
		}
		esito = EsitoOK
		if interrotto || r.Fallito {
			esito = EsitoFallito
		}
		if interrotto {
			r = Risultato{Dettaglio: DettaglioInterrotto}
		}
	}
	e := Esecuzione{Lavoro: nome, Avvio: avvio, Esito: esito, EseguitoIl: adesso.UTC().Truncate(time.Microsecond),
		Giorno: clock.DataDi(adesso), Avvisi: max(r.Avvisi, 0), Messaggi: max(r.Messaggi, 0), Dettaglio: taglia(r.Dettaglio)}
	// La riga si scrive anche se lo stop è arrivato intanto: quello che il lavoro ha fatto resta scritto.
	id, err := p.registra(context.WithoutCancel(ctx), e, a)
	if err != nil {
		return Esecuzione{}, false, err
	}
	e.ID = id
	return e, true, nil
}

// protetto è perform/5: il corpo del lavoro e niente altro. Un panico o un errore danno interrotto. Nel
// registro, per un panico il tipo e lo stack (ACC-19: mai il valore), per un errore solo il tipo (il testo
// può avere indirizzi o nomi). fermato: il lavoro è tornato con l'errore dello stop (il contesto finito),
// e allora non si scrive la riga (il giorno resta da fare, come lo standalone fermato a metà).
func (p *Pianificatore) protetto(ctx context.Context, nome string, conf Config, adesso time.Time, dest []uscita.Destinazione, a audit.Contesto) (r Risultato, interrotto, fermato bool) {
	defer func() {
		if v := recover(); v != nil {
			p.log.Error("lavoro automatico interrotto da un panico", "nome", nome, "tipo", fmt.Sprintf("%T", v), "stack", string(debug.Stack()))
			r, interrotto, fermato = Risultato{}, true, false
		}
	}()
	r, err := p.lavori[nome].Esegui(ctx, conf, adesso, dest, a)
	if err != nil {
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return Risultato{}, true, true
		}
		p.log.Error("lavoro automatico interrotto da un errore", "nome", nome, "tipo", fmt.Sprintf("%T", err))
		return Risultato{}, true, false
	}
	return r, false, false
}

// registra è record/5: la riga e jobs.ran nella stessa transazione.
func (p *Pianificatore) registra(ctx context.Context, e Esecuzione, a audit.Contesto) (int64, error) {
	tx, err := p.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var dettaglio any
	if e.Dettaglio != "" {
		dettaglio = e.Dettaglio
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO esecuzioni_lavori (lavoro, avvio, esito, eseguito_il, giorno, avvisi, messaggi, dettaglio)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, e.Lavoro, e.Avvio, e.Esito, e.EseguitoIl.Format(db.FormatoIstante), e.Giorno.String(),
		e.Avvisi, e.Messaggi, dettaglio)
	if err != nil {
		return 0, fmt.Errorf("jobs: riga dell'esecuzione: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := p.reg.Scrivi(ctx, tx, a, "jobs.ran", "job", e.Lavoro, e.Esito, map[string]any{
		"job": e.Lavoro, "trigger": e.Avvio, "notices": e.Avvisi, "messages": e.Messaggi}); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// taglia tiene i primi MassimoDettaglio grafemi (String.slice/3).
func taglia(s string) string {
	if uniseg.GraphemeClusterCount(s) <= MassimoDettaglio {
		return s
	}
	g := uniseg.NewGraphemes(s)
	fine := 0
	for i := 0; i < MassimoDettaglio && g.Next(); i++ {
		_, fine = g.Positions()
	}
	return s[:fine]
}

// Lavoro è Jobs.Scheduler: aspetta (il primo giro dopo la prima attesa, come lo standalone), fa un giro e
// riaspetta, finché aspetta dice di smettere (il contesto finito). Un giro che va in panico o non riesce
// non ferma il lavoro: nel registro solo il genere.
func (p *Pianificatore) Lavoro(ctx context.Context, aspetta func(context.Context) bool) {
	for aspetta(ctx) {
		if ctx.Err() != nil {
			return
		}
		p.giroProtetto(ctx)
	}
}

func (p *Pianificatore) giroProtetto(ctx context.Context) {
	defer func() {
		if v := recover(); v != nil {
			p.log.Error("lavori automatici: giro interrotto da un panico, si riprova al prossimo giro", "tipo", fmt.Sprintf("%T", v),
				"stack", string(debug.Stack()))
		}
	}()
	if _, err := p.Giro(ctx); err != nil && ctx.Err() == nil {
		p.log.Warn("lavori automatici: giro non finito, si riprova al prossimo giro", "tipo", fmt.Sprintf("%T", err))
	}
}

const colonneEsecuzione = `id, lavoro, avvio, esito, eseguito_il, giorno, avvisi, messaggi, coalesce(dettaglio, '')`

// Storico sono le ultime esecuzioni, la più recente prima (history/1): per id, perché le righe si
// aggiungono soltanto e due esecuzioni possono avere lo stesso istante.
func (p *Pianificatore) Storico(ctx context.Context, limite int) ([]Esecuzione, error) {
	return p.esecuzioni(ctx, `SELECT `+colonneEsecuzione+` FROM esecuzioni_lavori ORDER BY id DESC LIMIT ?`, limite)
}

// Ultime sono l'ultima esecuzione di ogni lavoro, per nome (last_runs/0); un lavoro mai eseguito non c'è.
func (p *Pianificatore) Ultime(ctx context.Context) (map[string]Esecuzione, error) {
	es, err := p.esecuzioni(ctx, `SELECT `+colonneEsecuzione+` FROM esecuzioni_lavori
		WHERE id IN (SELECT max(id) FROM esecuzioni_lavori GROUP BY lavoro)`)
	if err != nil {
		return nil, err
	}
	u := map[string]Esecuzione{}
	for _, e := range es {
		u[e.Lavoro] = e
	}
	return u, nil
}

func (p *Pianificatore) esecuzioni(ctx context.Context, q string, arg ...any) ([]Esecuzione, error) {
	rs, err := p.d.Lettura.QueryContext(ctx, q, arg...)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var es []Esecuzione
	for rs.Next() {
		var e Esecuzione
		var il, giorno string
		if err := rs.Scan(&e.ID, &e.Lavoro, &e.Avvio, &e.Esito, &il, &giorno, &e.Avvisi, &e.Messaggi, &e.Dettaglio); err != nil {
			return nil, err
		}
		if e.EseguitoIl, err = time.Parse(db.FormatoIstante, il); err != nil {
			return nil, fmt.Errorf("jobs: istante dell'esecuzione %d: %w", e.ID, err)
		}
		g, err := time.Parse(time.DateOnly, giorno)
		if err != nil {
			return nil, fmt.Errorf("jobs: giorno dell'esecuzione %d: %w", e.ID, err)
		}
		e.Giorno = clock.Data{Anno: g.Year(), Mese: g.Month(), Giorno: g.Day()}
		es = append(es, e)
	}
	return es, rs.Err()
}
