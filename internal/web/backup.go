package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/L0cutus/gestrip/internal/backup"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Il backup nella faccia interna, solo del Go: lo standalone fa il backup fuori dal pacchetto (OPS-28).
//   - «Esegui adesso» (slice 124, D-115): POST /settings/backup/esegui, solo il titolare (CSRF e ruolo dalla catena
//     di f.rotta). Il backup dura minuti: l'azione prende subito il lucchetto dei backup (con un backup già in corso,
//     anche del comando gestrip backup in un altro processo, rifiuta senza far partire niente), poi il backup gira in
//     sottofondo con il contesto dei lavori di serve e l'esito va nello storico dei backup.
//   - La pagina Impostazioni → Backup (slice 126, tema E): GET /settings/backup, solo il titolare, sola lettura:
//     stato e problemi, ora del backup notturno e prossimo giro, «Esegui adesso», ultime esecuzioni, registro dei
//     backup verificati (destinazione ed esito), ultima prova di ripristino, media rovinati. Il ritorno di «Esegui
//     adesso» arriva qui e il messaggio si vede qui (nota 10(b)).
//   - Il banner rosso del titolare (slice 126, risposta H): su ogni pagina interna del titolare, mai per gli altri
//     ruoli, né sulle pagine pubbliche, sulla firma o sulla pagina Backup, finché un problema resta
//     (backup.Stato.Allarme). Lo stato si calcola al più una volta ogni durataAllarme per faccia (cache sotto): sono
//     poche letture piccole del database e i controlli della chiavetta senza aprirla (stat, statfs, /proc), che a
//     ogni pagina sarebbero lavoro inutile. Una lettura alla volta, in una goroutine, con un tempo massimo (una
//     chiavetta appesa non ferma le pagine: «non si legge»). La pagina Backup lo legge sempre fresco e rinnova la
//     cache: chi ha appena sistemato la chiavetta e apre la pagina vede subito il banner sparire.
//   - La sezione «Copia remota» della stessa pagina (slice 129): configurazione SFTP, chiave pubblica
//     dell'apparecchio, conferma dell'impronta del server, prova e spegnimento (backup_remoto.go).

// BackupDelNegozio è il backup di serve visto dalla faccia (backup.Notturno).
type BackupDelNegozio interface {
	EseguiAdesso() error
	Stato(ctx context.Context) (backup.Stato, error)
}

const (
	rottaPaginaBackup       = "/settings/backup"
	msgBackupAvviato        = "Backup avviato: ci vuole qualche minuto. L'esito compare nello storico dei backup."
	msgBackupInCorso        = "Un backup è già in corso: riprova quando ha finito."
	msgBackupNonConfigurato = "Il backup non è configurato: manca la chiavetta nel file dell'apparecchio."
	msgBackupFermato        = "Il programma si sta fermando: il backup non è partito."
	msgBackupNonAvviato     = "Non è stato possibile avviare il backup: riprova."
	// msgStatoBackupIlleggibile è il banner quando lo stato non si legge: l'allarme non tace per un guasto.
	msgStatoBackupIlleggibile = "Lo stato dei backup non si riesce a leggere. Apri la pagina Backup; se il problema resta, chiedi assistenza."
	msgLavoroSpento           = "In questo avvio i lavori automatici sono spenti (solo sviluppo e prove): il backup notturno e la prova settimanale non partono, e il banner rosso non segnala backup mancanti o vecchi né la configurazione (gli altri problemi sì). «Esegui adesso» funziona."
)

// durataAllarme è quanto vale lo stato del banner prima di rileggerlo.
const durataAllarme = time.Minute

// tempoStatoBackup è il tempo massimo per leggere lo stato (database occupato, chiavetta appesa); una variabile
// per i test.
var tempoStatoBackup = 5 * time.Second

// eseguiBackup è «Esegui adesso»: torna sempre alla pagina del backup con l'esito della partenza.
func (f *facciaWeb) eseguiBackup(w http.ResponseWriter, r *http.Request) {
	if f.backup == nil {
		Errore(w, r)
		return
	}
	st := statoDi(r)
	switch err := f.backup.EseguiAdesso(); {
	case err == nil:
		st.dati.Info = msgBackupAvviato
	case errors.Is(err, backup.ErrBackupInCorso):
		st.dati.Errore = msgBackupInCorso
	case errors.Is(err, backup.ErrNonConfigurato):
		st.dati.Errore = msgBackupNonConfigurato
	case errors.Is(err, backup.ErrFermato):
		st.dati.Errore = msgBackupFermato
	default:
		f.registro.Error("backup a mano non partito", "tipo", fmt.Sprintf("%T", err))
		st.dati.Errore = msgBackupNonAvviato
	}
	st.cambiata = true
	f.vaiA(w, r, rottaPaginaBackup)
}

// errStatoScaduto: la lettura dello stato non è finita entro tempoStatoBackup (una chiavetta appesa, il database
// occupato).
var errStatoScaduto = errors.New("web: lo stato dei backup non è arrivato in tempo")

// letturaStato è la lettura dello stato in corso (una sola alla volta per faccia): fatto si chiude quando s ed err sono
// scritti.
type letturaStato struct {
	fatto chan struct{}
	s     backup.Stato
	err   error
}

// avviaLettura fa partire la lettura dello stato, se non ce n'è già una in corso (single-flight): i controlli della
// chiavetta non hanno un contesto, quindi una chiavetta appesa tiene ferma la goroutine; con una sola lettura alla
// volta non se ne accumulano. Il contesto è base senza la sua cancellazione: un browser che chiude non deve lasciare
// in cache un «non si legge». A lettura finita il risultato va in cache, prima di chiudere fatto.
func (f *facciaWeb) avviaLettura(base context.Context) *letturaStato {
	c := f.allarme
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inCorso != nil {
		return c.inCorso
	}
	l := &letturaStato{fatto: make(chan struct{})}
	c.inCorso = l
	go func() {
		ctx, annulla := context.WithTimeout(context.WithoutCancel(base), tempoStatoBackup)
		s, err := f.backup.Stato(ctx)
		annulla()
		if err != nil {
			f.registro.Error("stato dei backup non letto", "tipo", fmt.Sprintf("%T", err))
		}
		l.s, l.err = s, err
		c.mu.Lock()
		c.inCorso = nil
		c.letto, c.vista, c.pronto = f.orologio.Adesso(), vistaAllarme(s, err), true
		c.mu.Unlock()
		close(l.fatto)
	}()
	return l
}

// aspetta dà lo stato della lettura l se arriva entro tempoStatoBackup, altrimenti errStatoScaduto (fail-closed:
// chi lo mostra dice «non si legge»).
func aspetta(l *letturaStato) (backup.Stato, error) {
	ctx, annulla := context.WithTimeout(context.Background(), tempoStatoBackup)
	defer annulla()
	select {
	case <-l.fatto:
		return l.s, l.err
	case <-ctx.Done():
		return backup.Stato{}, errStatoScaduto
	}
}

// allarmeVista è il banner come lo disegna base.html.
type allarmeVista struct {
	Voci []string
}

// vistaIlleggibile è il banner quando lo stato non si legge: suona anche con i lavori spenti.
var vistaIlleggibile = &allarmeVista{Voci: []string{msgStatoBackupIlleggibile}}

// vistaAllarme è il banner dello stato s (err: lo stato non si legge); nil: niente banner.
func vistaAllarme(s backup.Stato, err error) *allarmeVista {
	if err != nil {
		return vistaIlleggibile
	}
	pp := s.Allarme()
	if len(pp) == 0 {
		return nil
	}
	v := &allarmeVista{}
	for _, p := range pp {
		v.Voci = append(v.Voci, p.Testo)
	}
	return v
}

// cacheAllarme è il banner dell'ultima lettura dello stato, per faccia, e la lettura in corso.
type cacheAllarme struct {
	mu      sync.Mutex
	letto   time.Time
	pronto  bool
	vista   *allarmeVista
	inCorso *letturaStato
}

// valida dà il banner in cache se è stato letto meno di durataAllarme fa (un orologio tornato indietro lo rilegge).
func (c *cacheAllarme) valida(adesso time.Time) (*allarmeVista, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.pronto || adesso.Before(c.letto) || adesso.Sub(c.letto) >= durataAllarme {
		return nil, false
	}
	return c.vista, true
}

// inAttesa dice se una lettura è già in corso e, se sì, il banner da mostrare intanto: l'ultimo letto, o «non si
// legge» se non ce n'è uno.
func (c *cacheAllarme) inAttesa() (*allarmeVista, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inCorso == nil {
		return nil, false
	}
	if c.pronto {
		return c.vista, true
	}
	return vistaIlleggibile, true
}

// allarmeBackup è il banner per questa richiesta del titolare (il chiamante guarda il ruolo). Con una lettura già in
// corso non aspetta: l'ultimo banner o «non si legge». Altrimenti ne fa partire una e la aspetta al più
// tempoStatoBackup; scaduto il tempo, «non si legge».
func (f *facciaWeb) allarmeBackup(r *http.Request) *allarmeVista {
	if f.backup == nil {
		return nil
	}
	if v, ok := f.allarme.valida(f.orologio.Adesso()); ok {
		return v
	}
	if v, ok := f.allarme.inAttesa(); ok {
		return v
	}
	s, err := aspetta(f.avviaLettura(r.Context()))
	return vistaAllarme(s, err)
}

// ---- la pagina ----

// datiBackup è la pagina Impostazioni → Backup.
type datiBackup struct {
	Illeggibile   bool   // lo stato non si legge: la pagina lo dice e mostra solo il pulsante
	NotaLavoro    string // msgLavoroSpento con i lavori automatici spenti
	Configurato   bool
	Problemi      []string
	Chiavetta     string // «inserita» o il problema, solo se configurato
	Ora           string // «02:30»
	Prossimo      string
	Esecuzioni    []rigaEsecuzioneBackup
	Verifiche     []rigaVerificaBackup
	Prova         *rigaProvaBackup
	Rovinati      int
	NomiRovinati  []string
	AltriRovinati int // quanti rovinati non hanno il nome nella pagina (oltre db.MaxNomiRovinati)
	// Remoto è la sezione della copia remota (slice 129, backup_remoto.go); nil se la faccia non la ha.
	Remoto *datiRemoto
}

type rigaEsecuzioneBackup struct {
	Quando, Avvio, Esito, Classe, Durata, Motivo string
}

type rigaVerificaBackup struct {
	Catturato, Destinazione, Esito, Classe, Archivio, Dimensione, Motivo string
	Rovinati                                                             int
}

type rigaProvaBackup struct {
	Quando, Avvio, Esito, Classe, Backup, Motivo string
	Rovinati                                     int
}

var (
	paroleAvvio = map[string]string{backup.AvvioAutomatico: "automatico", backup.AvvioAMano: "a mano"}
	paroleEsito = map[string]string{backup.EsitoOK: "riuscito", backup.EsitoAvviso: "riuscito, con un avviso",
		backup.EsitoFallito: "non riuscito"}
	classeEsito = map[string]string{backup.EsitoOK: "badge-ok", backup.EsitoAvviso: "badge-attesa", backup.EsitoFallito: "badge-errore"}
	paroleDest  = map[string]string{db.DestinazioneChiavetta: "chiavetta", db.DestinazioneCartella: "cartella (a mano)",
		db.DestinazioneSFTP: "copia remota"}
	giorniSettimana = [...]string{"domenica", "lunedì", "martedì", "mercoledì", "giovedì", "venerdì", "sabato"}
)

// dimensione è b in MB con una cifra decimale e la virgola («3612,4 MB»).
func dimensione(b int64) string {
	return strings.Replace(strconv.FormatFloat(float64(b)/(1024*1024), 'f', 1, 64), ".", ",", 1) + " MB"
}

// durata è il tempo di un'esecuzione in parole brevi.
func durata(d time.Duration) string {
	switch {
	case d < 0:
		return "—"
	case d < time.Minute:
		return strconv.Itoa(int(d/time.Second)) + " s"
	}
	return strconv.Itoa(int(d/time.Minute)) + " min"
}

// datiPaginaBackup è la pagina dello stato s.
func datiPaginaBackup(s backup.Stato) datiBackup {
	d := datiBackup{Configurato: s.Configurato, Rovinati: s.Rovinati, NomiRovinati: s.NomiRovinati}
	d.AltriRovinati = max(0, s.Rovinati-len(s.NomiRovinati))
	if s.LavoroSpento {
		d.NotaLavoro = msgLavoroSpento
	}
	for _, p := range s.Problemi() {
		d.Problemi = append(d.Problemi, p.Testo)
	}
	if s.Configurato {
		d.Chiavetta = "inserita e pronta"
		if s.Chiavetta != nil {
			d.Chiavetta = "non risponde (vedi sopra)"
		}
	}
	d.Ora = fmt.Sprintf("%02d:%02d", int(s.Ora/time.Hour), int(s.Ora%time.Hour/time.Minute))
	switch {
	case !s.Configurato:
		d.Prossimo = "nessuno: il backup non è configurato"
	case s.LavoroSpento:
		d.Prossimo = "nessuno: i lavori automatici sono spenti"
	case s.DaFare:
		d.Prossimo = "entro pochi minuti (il backup di questo turno è ancora da fare)"
	case !s.Prossimo.IsZero():
		l := clock.Locale(s.Prossimo)
		d.Prossimo = giorniSettimana[l.Weekday()] + " " + clock.Timbro(s.Prossimo)
	}
	for _, e := range s.Esecuzioni {
		d.Esecuzioni = append(d.Esecuzioni, rigaEsecuzioneBackup{Quando: clock.Timbro(e.IniziatoIl), Avvio: paroleAvvio[e.Avvio],
			Esito: paroleEsito[e.Esito], Classe: classeEsito[e.Esito], Durata: durata(e.FinitoIl.Sub(e.IniziatoIl)), Motivo: e.Errore})
	}
	for _, v := range s.Verifiche {
		r := rigaVerificaBackup{Catturato: clock.Timbro(v.CatturatoIl), Destinazione: paroleDest[v.Destinazione],
			Archivio: filepath.Base(v.Archivio), Dimensione: dimensione(v.Byte), Motivo: v.Errore, Rovinati: v.MediaRovinati,
			Esito: "verificato", Classe: "badge-ok"}
		if !v.Riuscita {
			r.Esito, r.Classe = "verifica non riuscita", "badge-errore"
		}
		d.Verifiche = append(d.Verifiche, r)
	}
	if s.CeProva {
		p := &rigaProvaBackup{Quando: clock.Timbro(s.Prova.FinitoIl), Avvio: paroleAvvio[s.Prova.Avvio], Motivo: s.Prova.Errore,
			Rovinati: s.Prova.MediaRovinati, Esito: "riuscita", Classe: "badge-ok"}
		if !s.Prova.Riuscita {
			p.Esito, p.Classe = "non riuscita", "badge-errore"
		}
		if !s.Prova.CatturatoIl.IsZero() {
			p.Backup = clock.Timbro(s.Prova.CatturatoIl)
		}
		d.Prova = p
	}
	return d
}

// paginaBackup è GET /settings/backup: lo stato sempre fresco, che rinnova anche il banner.
func (f *facciaWeb) paginaBackup(w http.ResponseWriter, r *http.Request) {
	f.mostraBackup(w, r, "", "", f.datiCopiaRemota(r.Context(), nil, false, "", ""))
}

// mostraBackup disegna la pagina Backup con l'errore o il messaggio di questa risposta e la sezione della copia
// remota rem (slice 129).
func (f *facciaWeb) mostraBackup(w http.ResponseWriter, r *http.Request, errore, info string, rem *datiRemoto) {
	if f.backup == nil {
		Errore(w, r)
		return
	}
	// Sempre una lettura nuova (o quella già in corso), che rinnova anche il banner.
	s, err := aspetta(f.avviaLettura(r.Context()))
	d := datiBackup{Illeggibile: true}
	if s.LavoroSpento {
		d.NotaLavoro = msgLavoroSpento
	}
	if err == nil {
		d = datiPaginaBackup(s)
	}
	d.Remoto = rem
	f.mostraConInfo(w, r, "backup", http.StatusOK, errore, info, d)
}

// registraBackup registra la pagina e l'azione del backup (solo il titolare).
func (f *facciaWeb) registraBackup(mux *http.ServeMux) {
	f.rottaConGuscio(mux, "GET "+rottaPaginaBackup, soloTitolare, f.paginaBackup)
	f.rotta(mux, "POST "+rottaPaginaBackup+"/esegui", soloTitolare, f.eseguiBackup)
	f.registraCopiaRemota(mux)
}
