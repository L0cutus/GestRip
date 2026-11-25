package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/media"
)

// Il backup notturno dentro gestrip serve (slice 124, T5.2, risposta C del piano, D-115) e «Esegui adesso»
// del titolare. Lo standalone non ce l'ha: il backup lo fa un timer di otto-vps alle 02:30, fuori dal
// pacchetto (OPS-28).
//
//   - **Quando:** ogni giorno, anche nei giorni di chiusura (D-115: niente giorni di apertura, D-17, qui),
//     all'ora del negozio ChiaveOra (02:30 di default), confrontata sull'ora del muro di Roma come i lavori
//     della 094: il giorno in cui alle 02:00 si salta alle 03:00 il backup parte alle 03:00; il giorno in cui
//     le 02:30 vengono due volte parte una volta sola.
//   - **Turni e recupero:** ogni esecuzione appartiene a un turno, il giorno del negozio dell'ultima ora del
//     backup passata (alle 01:00 è ancora il turno di ieri). Un turno senza un'esecuzione automatica riuscita
//     è da fare: se l'apparecchio era spento all'ora giusta, il backup parte AttesaDopoAvvio (10 minuti, il
//     piano) dopo l'avvio di serve, anche se è il turno di ieri. Al più TentativiPerTurno tentativi per turno,
//     a distanza di almeno AttesaFraTentativi.
//   - **Mai due insieme:** ogni esecuzione tiene il lucchetto dei backup (PrendiLucchetto), lo stesso del
//     comando gestrip backup e di gestrip verifica-backup; se è occupato il giro non parte (riprova al minuto
//     dopo) e «Esegui adesso» rifiuta con ErrBackupInCorso.
//   - **Dove:** solo la chiavetta, con il punto di montaggio del file dell'apparecchio (backup_chiavetta):
//     senza, il backup non è configurato, il lavoro non parte e «Esegui adesso» dà ErrNonConfigurato.
//   - **Storico:** la tabella esecuzioni_backup (migrazione 0022), una riga per esecuzione; uno stop di serve
//     a metà non lascia righe e il turno resta da fare.

// ChiaveOra è la chiave di impostazioni, solo del Go, dell'ora del backup notturno: «HH:MM» o «HH:MM:SS»,
// ora del negozio. Senza la riga o con un valore illeggibile: OraPredefinita. La scriverà la pagina.
const ChiaveOra = "backup.ora"

// OraPredefinita sono le 02:30, l'ora del backup di otto-vps (risposta C).
const OraPredefinita = 2*time.Hour + 30*time.Minute

// AttesaDopoAvvio è quanto il lavoro aspetta dopo l'avvio di serve prima del primo giro (risposta C: «parte 10
// minuti dopo la riaccensione»).
const AttesaDopoAvvio = 10 * time.Minute

// OgniQuantoNotturno è l'intervallo fra due giri del lavoro.
const OgniQuantoNotturno = time.Minute

// TentativiPerTurno sono le esecuzioni automatiche al più per turno, come i lavori della 094.
const TentativiPerTurno = 3

// AttesaFraTentativi è il tempo minimo fra la fine di un tentativo automatico fallito e il successivo dello
// stesso turno: una chiavetta tolta non si ritenta ogni minuto.
const AttesaFraTentativi = 30 * time.Minute

// Avvii ed esiti, le parole delle colonne di esecuzioni_backup.
const (
	AvvioAutomatico = "automatico"
	AvvioAMano      = "a_mano"
	EsitoOK         = "ok"
	EsitoAvviso     = "avviso"
	EsitoFallito    = "fallito"
)

// maxErroreEsecuzione è quanti byte del motivo entrano nello storico.
const maxErroreEsecuzione = 1000

// DettaglioPanico è il motivo di un'esecuzione interrotta da un panico: il dettaglio va solo nel registro.
const DettaglioPanico = "il backup si è interrotto per un errore interno: il dettaglio è nel registro del programma"

var (
	// ErrNonConfigurato: il file dell'apparecchio non dice dov'è la chiavetta dei backup.
	ErrNonConfigurato = errors.New("backup: la chiavetta dei backup non è configurata (backup_chiavetta nel file dell'apparecchio)")
	// ErrFermato: serve si sta fermando, il backup a mano non parte.
	ErrFermato = errors.New("backup: il programma si sta fermando, il backup non parte")
)

// Esecuzione è una riga di esecuzioni_backup.
type Esecuzione struct {
	ID                   int64
	Avvio                string
	Turno                clock.Data
	IniziatoIl, FinitoIl time.Time
	Esito                string
	Errore               string // "" se la colonna è NULL
	Archivio             string // "" se la colonna è NULL
}

// Notturno è il backup notturno e a mano di un database: uno per processo (gestrip serve).
type Notturno struct {
	d         *db.DB
	archivio  *media.Archivio
	dati      string
	chiavetta string
	o         clock.Orologio
	log       *slog.Logger
	// ctx e lancia servono a «Esegui adesso»: il backup gira con il contesto dei lavori di serve (lo stop lo
	// ferma) e serve lo aspetta prima di chiudere il database.
	ctx    context.Context
	lancia func(func()) bool
	// verifica è VerificaArchivio; una variabile per i test.
	verifica func(ctx context.Context, dati, file string) (Verificato, error)
	// nonScritte sono le esecuzioni automatiche la cui riga dello storico non si è potuta scrivere (CHECK,
	// database occupato, I/O), per turno: dovuto le conta come quelle scritte, così una riga mancante non fa
	// ripartire un backup intero a ogni giro (revisione della 124). Solo del processo: dopo un riavvio il turno
	// si rifà, al più TentativiPerTurno volte.
	muNonScritte sync.Mutex
	nonScritte   map[string]tentativiNonScritti
}

// tentativiNonScritti sono le esecuzioni automatiche di un turno senza riga: quante, se una era riuscita (ok o
// avviso) e quando è finita l'ultima.
type tentativiNonScritti struct {
	tutte    int
	riuscita bool
	ultima   time.Time
}

// TempoScritturaStorico è il tempo massimo di una scrittura fatta dopo lo stop (riga dello storico, riga della
// verifica fallita): con il contesto staccato dallo stop, un database bloccato non tiene serve fermo per sempre.
const TempoScritturaStorico = 30 * time.Second

// staccato è il contesto delle scritture che devono arrivare anche dopo lo stop: non finisce con ctx, ma dura al
// più TempoScritturaStorico.
func staccato(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), TempoScritturaStorico)
}

// NuovoNotturno è il backup notturno del database d, con i media di archivio, sulla chiavetta montata in
// chiavetta ("" non configurato), sull'orologio o.
func NuovoNotturno(d *db.DB, archivio *media.Archivio, chiavetta string, o clock.Orologio) *Notturno {
	return &Notturno{d: d, archivio: archivio, dati: filepath.Dir(d.Percorso()), chiavetta: chiavetta, o: o,
		log: slog.New(slog.DiscardHandler)}
}

// ConRegistro dà al backup le righe del server (mai testi d'errore: quelli vanno nello storico).
func (n *Notturno) ConRegistro(l *slog.Logger) *Notturno {
	if l != nil {
		n.log = l
	}
	return n
}

// ConSottofondo dà a «Esegui adesso» il contesto dei lavori di serve e il modo di farli partire: lancia dice
// false se serve si sta già fermando (il lavoro non parte).
func (n *Notturno) ConSottofondo(ctx context.Context, lancia func(func()) bool) *Notturno {
	n.ctx, n.lancia = ctx, lancia
	return n
}

// Configurato dice se c'è una chiavetta dove scrivere.
func (n *Notturno) Configurato() bool { return n.chiavetta != "" }

// Ora è l'ora del backup notturno (ChiaveOra, altrimenti OraPredefinita).
func (n *Notturno) Ora(ctx context.Context) (time.Duration, error) {
	var v string
	err := n.d.Lettura.QueryRowContext(ctx, `SELECT valore FROM impostazioni WHERE chiave = ?`, ChiaveOra).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return OraPredefinita, nil
	}
	if err != nil {
		return 0, err
	}
	if o, ok := leggiOraBackup(v); ok {
		return o, nil
	}
	return OraPredefinita, nil
}

// leggiOraBackup legge «HH:MM» o «HH:MM:SS» (00:00…23:59:59).
func leggiOraBackup(v string) (time.Duration, bool) {
	parti := strings.Split(strings.TrimSpace(v), ":")
	if len(parti) < 2 || len(parti) > 3 {
		return 0, false
	}
	massimi := []int{23, 59, 59}
	var n [3]int
	for i, p := range parti {
		if len(p) != 2 {
			return 0, false
		}
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 || x > massimi[i] {
			return 0, false
		}
		n[i] = x
	}
	return time.Duration(n[0])*time.Hour + time.Duration(n[1])*time.Minute + time.Duration(n[2])*time.Second, true
}

// Turno è il giorno del negozio dell'ultima ora del backup passata ad adesso: oggi se l'ora del muro di Roma
// ha raggiunto ora, altrimenti ieri. Con il salto delle 02:00 un'ora delle 02:30 si raggiunge alle 03:00; con
// le 02:30 che vengono due volte il turno è lo stesso.
func Turno(adesso time.Time, ora time.Duration) clock.Data {
	loc := clock.Locale(adesso)
	mezzanotte := time.Date(loc.Year(), loc.Month(), loc.Day(), 0, 0, 0, 0, time.UTC)
	muro := time.Date(loc.Year(), loc.Month(), loc.Day(), loc.Hour(), loc.Minute(), loc.Second(), loc.Nanosecond(), time.UTC)
	if muro.Sub(mezzanotte) >= ora {
		return clock.DataDi(adesso)
	}
	ieri := time.Date(loc.Year(), loc.Month(), loc.Day()-1, 12, 0, 0, 0, time.UTC)
	return clock.Data{Anno: ieri.Year(), Mese: ieri.Month(), Giorno: ieri.Day()}
}

// Dovuto dice se adesso il backup automatico va fatto.
func (n *Notturno) Dovuto(ctx context.Context) (bool, error) {
	ora, err := n.Ora(ctx)
	if err != nil {
		return false, err
	}
	return n.dovuto(ctx, n.o.Adesso(), ora)
}

// dovuto: configurato, e nel turno di adesso nessuna esecuzione automatica riuscita (ok o avviso), meno di
// TentativiPerTurno in tutto e l'ultima finita da almeno AttesaFraTentativi (o, con l'orologio tornato
// indietro, «nel futuro»: si riprova).
func (n *Notturno) dovuto(ctx context.Context, adesso time.Time, ora time.Duration) (bool, error) {
	if !n.Configurato() {
		return false, nil
	}
	turno := Turno(adesso, ora)
	var tutte, riuscite int
	var ultima sql.NullString
	err := n.d.Lettura.QueryRowContext(ctx, `SELECT count(*), count(*) FILTER (WHERE esito IN (?, ?)), max(finito_il)
		FROM esecuzioni_backup WHERE avvio = ? AND turno = ?`, EsitoOK, EsitoAvviso, AvvioAutomatico, turno.String()).
		Scan(&tutte, &riuscite, &ultima)
	if err != nil {
		return false, err
	}
	var fine time.Time
	if ultima.Valid {
		if fine, err = time.Parse(db.FormatoIstante, ultima.String); err != nil {
			return false, fmt.Errorf("backup: fine dell'ultima esecuzione: %w", err)
		}
	}
	n.muNonScritte.Lock()
	m := n.nonScritte[turno.String()]
	n.muNonScritte.Unlock()
	tutte += m.tutte
	if m.riuscita {
		riuscite++
	}
	if m.ultima.After(fine) {
		fine = m.ultima
	}
	if riuscite > 0 || tutte >= TentativiPerTurno {
		return false, nil
	}
	if !fine.IsZero() {
		if trascorso := adesso.Sub(fine); trascorso >= 0 && trascorso < AttesaFraTentativi {
			return false, nil
		}
	}
	return true, nil
}

// Giro fa il backup automatico se è dovuto adesso. eseguito dice se c'è una riga nuova nello storico. Con il
// lucchetto occupato (un backup a mano, il comando, una verifica) non fa niente: il turno resta da fare.
func (n *Notturno) Giro(ctx context.Context) (e Esecuzione, eseguito bool, err error) {
	ora, err := n.Ora(ctx)
	if err != nil {
		return Esecuzione{}, false, err
	}
	adesso := n.o.Adesso()
	dovuto, err := n.dovuto(ctx, adesso, ora)
	if err != nil || !dovuto {
		return Esecuzione{}, false, err
	}
	l, err := PrendiLucchetto(n.dati)
	if errors.Is(err, ErrBackupInCorso) {
		n.log.Info("backup notturno: un altro backup è in corso, riprovo al prossimo giro")
		return Esecuzione{}, false, nil
	}
	if err != nil {
		// Un lucchetto che non si apre (permessi): un tentativo fallito, così non si riprova ogni minuto.
		return n.registra(ctx, Esecuzione{Avvio: AvvioAutomatico, Turno: Turno(adesso, ora), IniziatoIl: adesso}, EsitoGiro{}, err)
	}
	defer func() { err = errors.Join(err, l.Rilascia()) }()
	return n.esegui(ctx, l, AvvioAutomatico, adesso, ora)
}

// EseguiAdesso è «Esegui adesso» del titolare: prende subito il lucchetto (occupato: ErrBackupInCorso, niente
// parte) e fa il backup in sottofondo, con il contesto dei lavori di serve; l'esito va nello storico.
func (n *Notturno) EseguiAdesso() error {
	if !n.Configurato() {
		return ErrNonConfigurato
	}
	if n.ctx == nil || n.lancia == nil {
		return ErrFermato
	}
	if err := n.ctx.Err(); err != nil {
		return ErrFermato
	}
	ora, err := n.Ora(n.ctx)
	if err != nil {
		return err
	}
	l, err := PrendiLucchetto(n.dati)
	if err != nil {
		return err
	}
	adesso := n.o.Adesso()
	partito := n.lancia(func() {
		defer func() {
			if err := l.Rilascia(); err != nil {
				n.log.Warn("backup a mano: lucchetto non rilasciato bene", "tipo", fmt.Sprintf("%T", err))
			}
		}()
		_, _, _ = n.esegui(n.ctx, l, AvvioAMano, adesso, ora)
	})
	if !partito {
		return errors.Join(ErrFermato, l.Rilascia())
	}
	return nil
}

// esegui fa il giro (Giro.Esegui) con il lucchetto l già preso e scrive la riga dello storico. Uno stop che
// ferma un backup non riuscito non lascia righe (il turno resta da fare). Un panico è un'esecuzione fallita con
// DettaglioPanico (nel registro tipo e stack).
func (n *Notturno) esegui(ctx context.Context, l *Lucchetto, avvio string, iniziato time.Time, ora time.Duration) (e Esecuzione, eseguito bool, err error) {
	riga := Esecuzione{Avvio: avvio, Turno: Turno(iniziato, ora), IniziatoIl: iniziato}
	var eg EsitoGiro
	func() {
		defer func() {
			if v := recover(); v != nil {
				n.log.Error("backup interrotto da un panico", "avvio", avvio, "tipo", fmt.Sprintf("%T", v), "stack", string(debug.Stack()))
				eg, err = EsitoGiro{}, errors.New(DettaglioPanico)
			}
		}()
		g := Giro{Lucchetto: l, DB: n.d, Archivio: n.archivio, Chiavetta: n.chiavetta, Cattura: n.o, Registro: n.o, Verifica: n.verifica}
		eg, err = g.Esegui(ctx)
	}()
	if !eg.Riuscito && err != nil && ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		n.log.Info("backup fermato dallo stop del programma: il turno resta da fare", "avvio", avvio)
		return Esecuzione{}, false, err
	}
	return n.registra(ctx, riga, eg, err)
}

// registra scrive la riga dello storico con l'esito del giro: ok, avviso (riuscito con un errore dopo) o
// fallito. La riga si scrive anche se lo stop è arrivato intanto: quello che il backup ha fatto resta scritto.
func (n *Notturno) registra(ctx context.Context, riga Esecuzione, eg EsitoGiro, errGiro error) (Esecuzione, bool, error) {
	riga.FinitoIl = n.o.Adesso()
	riga.Archivio = eg.Risultato.File
	switch {
	case eg.Riuscito && errGiro == nil:
		riga.Esito = EsitoOK
	case eg.Riuscito:
		riga.Esito = EsitoAvviso
	default:
		riga.Esito = EsitoFallito
	}
	if riga.Esito != EsitoOK {
		riga.Errore = "errore senza testo"
		if errGiro != nil && strings.TrimSpace(errGiro.Error()) != "" {
			riga.Errore = strings.TrimSpace(errGiro.Error())
		}
		if len(riga.Errore) > maxErroreEsecuzione {
			riga.Errore = strings.ToValidUTF8(riga.Errore[:maxErroreEsecuzione], "")
		}
	}
	// Nel registro del server esito e tipo; il motivo intero è nello storico (percorsi della chiavetta, mai dati
	// dei clienti, ma più lungo di una riga).
	if riga.Esito == EsitoOK {
		n.log.Info("backup riuscito", "avvio", riga.Avvio)
	} else {
		n.log.Warn("backup non riuscito o con un avviso: il motivo è nello storico dei backup", "avvio", riga.Avvio,
			"esito", riga.Esito, "tipo", fmt.Sprintf("%T", errGiro))
	}
	var archivio any
	if riga.Archivio != "" {
		if a, err := filepath.Abs(riga.Archivio); err == nil {
			archivio = a
		}
	}
	var errore any
	if riga.Errore != "" {
		errore = riga.Errore
	}
	ctxScrivi, annulla := staccato(ctx)
	defer annulla()
	res, err := inserisciEsecuzione(ctxScrivi, n.d, `INSERT INTO esecuzioni_backup
		(avvio, turno, iniziato_il, finito_il, esito, errore, archivio) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		riga.Avvio, riga.Turno.String(), riga.IniziatoIl.UTC().Format(db.FormatoIstante), riga.FinitoIl.UTC().Format(db.FormatoIstante),
		riga.Esito, errore, archivio)
	if err != nil {
		if riga.Avvio == AvvioAutomatico {
			n.ricordaNonScritta(riga)
		}
		n.log.Error("backup: la riga dello storico non si è scritta, il tentativo è contato solo in memoria",
			"avvio", riga.Avvio, "esito", riga.Esito, "tipo", fmt.Sprintf("%T", err))
		return Esecuzione{}, false, errors.Join(errGiro, fmt.Errorf("backup: storico delle esecuzioni: %w", err))
	}
	if riga.ID, err = res.LastInsertId(); err != nil {
		return riga, true, errors.Join(errGiro, err)
	}
	return riga, true, errGiro
}

// ricordaNonScritta conta in memoria un'esecuzione automatica senza riga dello storico.
func (n *Notturno) ricordaNonScritta(riga Esecuzione) {
	n.muNonScritte.Lock()
	defer n.muNonScritte.Unlock()
	if n.nonScritte == nil {
		n.nonScritte = map[string]tentativiNonScritti{}
	}
	m := n.nonScritte[riga.Turno.String()]
	m.tutte++
	m.riuscita = m.riuscita || riga.Esito != EsitoFallito
	if riga.FinitoIl.After(m.ultima) {
		m.ultima = riga.FinitoIl
	}
	n.nonScritte[riga.Turno.String()] = m
}

// inserisciEsecuzione scrive la riga dello storico; una variabile per i test (una scrittura che non riesce).
var inserisciEsecuzione = func(ctx context.Context, d *db.DB, q string, arg ...any) (sql.Result, error) {
	return d.Scrittura.ExecContext(ctx, q, arg...)
}

// Lavoro è il backup notturno in serve: senza chiavetta configurata lo dice una volta ed esce; altrimenti
// aspetta AttesaDopoAvvio, poi fa un giro ogni OgniQuantoNotturno finché aspetta dice di smettere (il contesto
// finito). Un giro che va in panico o non riesce non ferma il lavoro.
func (n *Notturno) Lavoro(ctx context.Context, aspetta func(context.Context, time.Duration) bool) {
	if !n.Configurato() {
		n.log.Info("backup notturno non configurato: manca backup_chiavetta nel file dell'apparecchio")
		return
	}
	if !aspetta(ctx, AttesaDopoAvvio) {
		return
	}
	for ctx.Err() == nil {
		n.giroProtetto(ctx)
		if !aspetta(ctx, OgniQuantoNotturno) {
			return
		}
	}
}

func (n *Notturno) giroProtetto(ctx context.Context) {
	defer func() {
		if v := recover(); v != nil {
			n.log.Error("backup notturno: giro interrotto da un panico, si riprova al prossimo giro", "tipo", fmt.Sprintf("%T", v),
				"stack", string(debug.Stack()))
		}
	}()
	if _, _, err := n.Giro(ctx); err != nil && ctx.Err() == nil {
		n.log.Warn("backup notturno: giro non finito, si riprova al prossimo giro", "tipo", fmt.Sprintf("%T", err))
	}
}

// Storico sono le ultime esecuzioni, la più recente prima (per id: le righe si aggiungono soltanto).
func (n *Notturno) Storico(ctx context.Context, limite int) ([]Esecuzione, error) {
	rs, err := n.d.Lettura.QueryContext(ctx, `SELECT id, avvio, turno, iniziato_il, finito_il, esito, coalesce(errore, ''),
		coalesce(archivio, '') FROM esecuzioni_backup ORDER BY id DESC LIMIT ?`, limite)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var es []Esecuzione
	for rs.Next() {
		var e Esecuzione
		var turno, inizio, fine string
		if err := rs.Scan(&e.ID, &e.Avvio, &turno, &inizio, &fine, &e.Esito, &e.Errore, &e.Archivio); err != nil {
			return nil, err
		}
		g, err := time.Parse(time.DateOnly, turno)
		if err != nil {
			return nil, fmt.Errorf("backup: turno dell'esecuzione %d: %w", e.ID, err)
		}
		e.Turno = clock.Data{Anno: g.Year(), Mese: g.Month(), Giorno: g.Day()}
		if e.IniziatoIl, err = time.Parse(db.FormatoIstante, inizio); err != nil {
			return nil, fmt.Errorf("backup: inizio dell'esecuzione %d: %w", e.ID, err)
		}
		if e.FinitoIl, err = time.Parse(db.FormatoIstante, fine); err != nil {
			return nil, fmt.Errorf("backup: fine dell'esecuzione %d: %w", e.ID, err)
		}
		es = append(es, e)
	}
	return es, rs.Err()
}

// PulisciAllAvvio toglie le cartelle temporanee rimaste nella cartella dei dati all'avvio di serve, solo se il
// lucchetto dei backup è libero (con un backup o una verifica in corso in un altro processo dà
// ErrBackupInCorso e non tocca niente). Dà i nomi tolti.
func PulisciAllAvvio(dati string) (tolti []string, err error) {
	l, err := PrendiLucchetto(dati)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, l.Rilascia()) }()
	return l.PulisciResti()
}
