package backup

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La prova di ripristino settimanale (slice 125, T5.2, D-15, OPS-28 «prova», risposta G del piano): la
// domenica notte, dopo il backup notturno, sull'ultimo archivio della chiavetta. Lo standalone non ce l'ha
// nel pacchetto: la fa otto-vps, e fallisce se l'archivio ha più di 8 giorni.
//
//   - **Quale archivio:** l'ultimo verificato sulla chiavetta del registro dei backup
//     (db.UltimoVerificatoSullaChiavetta, quello che sblocca le cancellazioni e che la rotazione non toglie
//     mai), cioè quello da cui si ripartirebbe. Deve stare nella chiavetta configurata, montata e controllata
//     come per il backup (ApriChiavetta, ConfermaChiavetta), con gli stessi byte, lo stesso SHA-256 e la
//     stessa ora di cattura del registro.
//   - **Cosa fa:** la verifica completa della 121 (VerificaArchivio: decifratura con la chiave dei backup di
//     secrets/, tar, impronte, PRAGMA integrity_check e conteggi sul database estratto in una cartella
//     temporanea del volume dei dati, poi tolta). Annota i media rovinati (D-116), che non la fanno fallire.
//   - **Quando fallisce:** chiavetta non configurata, assente o non controllabile; nessun backup verificato nel
//     registro; archivio che manca o è cambiato; verifica non passata; archivio catturato più di EtaMassimaProva
//     fa (o nel futuro).
//   - **Quando gira:** nel lavoro notturno (Notturno.Lavoro), dopo il backup dello stesso giro. La settimana di
//     una prova è la domenica del turno del backup (SettimanaDi): la prova della settimana è dovuta appena il
//     turno del backup di adesso è chiuso (riuscito, o con i tentativi finiti). Così la domenica alle 02:30
//     parte dopo il backup della domenica; con l'apparecchio spento la domenica notte, parte al primo giro utile
//     della settimana (dopo il backup di recupero). Tentativi come il backup: al più TentativiPerTurno per
//     settimana, a distanza di AttesaFraTentativi, fino alla prima riuscita.
//   - **Lucchetti:** il lucchetto dei backup (mai insieme a un backup o a una verifica), poi quello della
//     chiavetta (ApriChiavetta), come il giro della 124.
//   - **Esito:** una riga di prove_ripristino (migrazione 0023, db.RegistraProvaRipristino). L'allarme della 126
//     legge db.ProvaSettimanaleFallita. Uno stop di serve a metà non lascia righe.

// EtaMassimaProva è l'età massima dell'archivio provato (risposta G: «fallisce anche se l'archivio ha più di 8
// giorni», come la prova di otto-vps): l'ora di cattura dell'archivio contro l'inizio della prova.
const EtaMassimaProva = 8 * 24 * time.Hour

// DettaglioPanicoProva è il motivo di una prova interrotta da un panico: il dettaglio va solo nel registro del
// programma (revisione della 125: DettaglioPanico parla del backup).
const DettaglioPanicoProva = "la prova di ripristino si è interrotta per un errore interno: il dettaglio è nel registro del programma"

var (
	// ErrProvaSenzaBackup: il registro non ha un backup verificato sulla chiavetta.
	ErrProvaSenzaBackup = errors.New("prova di ripristino: nel registro non c'è un backup verificato sulla chiavetta")
	// ErrProvaVecchio: l'ultimo backup verificato sulla chiavetta è più vecchio di EtaMassimaProva.
	ErrProvaVecchio = errors.New("prova di ripristino: l'ultimo backup verificato sulla chiavetta ha più di 8 giorni")
	// ErrProvaFuoriChiavetta: l'ultimo backup verificato non sta nella chiavetta configurata.
	ErrProvaFuoriChiavetta = errors.New("prova di ripristino: l'ultimo backup verificato non sta nella chiavetta configurata")
)

// SettimanaDi è la domenica della settimana del turno: il turno stesso se è domenica, altrimenti la domenica
// prima.
func SettimanaDi(turno clock.Data) clock.Data {
	g := time.Date(turno.Anno, turno.Mese, turno.Giorno, 12, 0, 0, 0, time.UTC)
	d := g.AddDate(0, 0, -int(g.Weekday()-time.Sunday))
	return clock.Data{Anno: d.Year(), Mese: d.Month(), Giorno: d.Day()}
}

// Prova è una prova di ripristino da fare.
type Prova struct {
	// Lucchetto è il lucchetto dei backup, già preso da chi chiama e rilasciato da lui.
	Lucchetto *Lucchetto
	DB        *db.DB
	// Chiavetta è il punto di montaggio della chiavetta (backup_chiavetta, o -chiavetta del comando).
	Chiavetta string
	// Adesso è l'inizio della prova: l'età dell'archivio si misura da qui.
	Adesso time.Time
	// Verifica è la verifica dell'archivio; nil: VerificaArchivio.
	Verifica func(ctx context.Context, dati, file string) (Verificato, error)
	// Dire riceve le righe per chi guarda (il comando le stampa). Un suo errore NON ferma la prova (revisione
	// della 125): un'uscita chiusa (`| head`) non deve registrare una prova fallita e accendere l'allarme; chi
	// chiama, se gli serve, ricorda l'errore da sé. nil: nessuna.
	Dire func(riga string) error
}

// EsitoProva è quello che la prova ha trovato: l'archivio del registro (vuoto se il registro non ne aveva uno)
// e, se la verifica è arrivata in fondo, quello che ha letto.
type EsitoProva struct {
	Archivio   db.ArchivioRegistrato
	Verificato Verificato
}

func (p Prova) dire(formato string, arg ...any) {
	if p.Dire != nil {
		_ = p.Dire(fmt.Sprintf(formato, arg...)) // vedi Dire: l'uscita non decide l'esito della prova
	}
}

// Esegui fa la prova, nell'ordine: le cartelle temporanee rimaste (PulisciResti) → la chiavetta con il suo
// lucchetto (ApriChiavetta) → l'ultimo backup verificato sulla chiavetta del registro → l'età → l'archivio è
// ancora un file della chiavetta (ConfermaChiavetta) → la verifica completa → stessi byte, SHA-256 e ora di
// cattura del registro. Solo un errore nil vuol dire riuscita. Non scrive niente: la riga la scrive chi chiama.
func (p Prova) Esegui(ctx context.Context) (e EsitoProva, err error) {
	if !p.Lucchetto.Preso() {
		return EsitoProva{}, ErrSenzaLucchetto
	}
	if p.Chiavetta == "" {
		return EsitoProva{}, ErrNonConfigurato
	}
	dati := filepath.Dir(p.DB.Percorso())
	if !p.Lucchetto.diDati(dati) {
		return EsitoProva{}, fmt.Errorf("%w: il lucchetto è di un'altra cartella dei dati", ErrSenzaLucchetto)
	}
	resti, err := p.Lucchetto.PulisciResti()
	for _, n := range resti {
		p.dire("tolta %s, cartella temporanea rimasta da un backup interrotto\n", filepath.Join(dati, n))
	}
	if err != nil {
		return EsitoProva{}, err
	}
	dest, err := ApriChiavetta(p.Chiavetta, dati)
	if err != nil {
		return EsitoProva{}, err
	}
	defer func() { err = errors.Join(err, dest.Chiudi()) }()
	u, ok, err := db.UltimoVerificatoSullaChiavetta(ctx, p.DB.Lettura)
	if err != nil {
		return EsitoProva{}, err
	}
	if !ok {
		return EsitoProva{}, ErrProvaSenzaBackup
	}
	e.Archivio = u
	p.dire("prova di ripristino di %s\ncatturato il: %s\n", u.Archivio, u.CatturatoIl.Format(db.FormatoIstante))
	switch eta := p.Adesso.Sub(u.CatturatoIl); {
	case eta > EtaMassimaProva:
		return e, fmt.Errorf("%w: catturato il %s, %d giorni fa", ErrProvaVecchio, clock.Timbro(u.CatturatoIl), int(eta/(24*time.Hour)))
	case eta < 0:
		return e, fmt.Errorf("%w: l'ultimo backup è catturato nel futuro (%s)", db.ErrOrologioBackup, clock.Timbro(u.CatturatoIl))
	}
	if filepath.Dir(u.Archivio) != dest.Cartella {
		return e, fmt.Errorf("%w: %s non è in %s", ErrProvaFuoriChiavetta, u.Archivio, dest.Cartella)
	}
	if err := dest.ConfermaChiavetta(u.Archivio); err != nil {
		return e, err
	}
	verifica := p.Verifica
	if verifica == nil {
		verifica = VerificaArchivio
	}
	v, err := verifica(ctx, dati, u.Archivio)
	if err != nil {
		return e, err
	}
	e.Verificato = v
	if v.SHA256 != u.SHA256 || v.ByteCifrati != u.Byte {
		return e, fmt.Errorf("%w: %s", db.ErrArchivioCambiato, u.Archivio)
	}
	if t, errT := time.Parse(db.FormatoIstante, v.CatturatoIl); errT != nil || !t.Equal(u.CatturatoIl) {
		return e, fmt.Errorf("%w: ora di cattura dell'archivio diversa dal registro", db.ErrArchivioCambiato)
	}
	p.dire("verificato: decifrato, %d file con l'impronta giusta, database integro, %d tabelle con i conteggi giusti\n",
		len(v.Inventario), len(v.Conteggi))
	if len(v.MediaRovinati) > 0 {
		p.dire("ATTENZIONE: %d media con il contenuto diverso dal nome (rovinati sul disco prima del backup): "+
			"la prova vale lo stesso\n", len(v.MediaRovinati))
	}
	return e, nil
}

// ProvaDovuta dice se adesso la prova settimanale automatica va fatta.
func (n *Notturno) ProvaDovuta(ctx context.Context) (bool, error) {
	ora, err := n.Ora(ctx)
	if err != nil {
		return false, err
	}
	return n.provaDovuta(ctx, n.o.Adesso(), ora)
}

// provaDovuta: configurato; il turno del backup di adesso chiuso (un'esecuzione automatica riuscita, o i
// tentativi finiti: «dopo il backup»); per la settimana di quel turno la regola dei tentativi del backup (daRifare)
// sulle prove automatiche.
func (n *Notturno) provaDovuta(ctx context.Context, adesso time.Time, ora time.Duration) (bool, error) {
	if !n.Configurato() {
		return false, nil
	}
	turno := Turno(adesso, ora)
	tutte, riuscite, _, err := n.statoTurno(ctx, turno)
	if err != nil {
		return false, err
	}
	if riuscite == 0 && tutte < TentativiPerTurno {
		return false, nil // il backup del turno è ancora da fare (o da ritentare): la prova aspetta
	}
	settimana := SettimanaDi(turno)
	tutte, riuscite, fine, err := db.TentativiProva(ctx, n.d.Lettura, db.AvvioProvaAutomatico, settimana)
	if err != nil {
		return false, err
	}
	tutte, riuscite, fine = n.conNonScritte(chiaveProva(settimana), tutte, riuscite, fine)
	return daRifare(adesso, tutte, riuscite, fine), nil
}

// GiroProva fa la prova automatica se è dovuta adesso. eseguita dice se c'è una riga nuova nel registro delle
// prove. Con il lucchetto occupato non fa niente: la settimana resta da fare.
func (n *Notturno) GiroProva(ctx context.Context) (p db.ProvaRipristino, eseguita bool, err error) {
	ora, err := n.Ora(ctx)
	if err != nil {
		return db.ProvaRipristino{}, false, err
	}
	adesso := n.o.Adesso()
	dovuta, err := n.provaDovuta(ctx, adesso, ora)
	if err != nil || !dovuta {
		return db.ProvaRipristino{}, false, err
	}
	l, err := PrendiLucchetto(n.dati)
	if errors.Is(err, ErrBackupInCorso) {
		n.log.Info("prova di ripristino: un backup è in corso, riprovo al prossimo giro")
		return db.ProvaRipristino{}, false, nil
	}
	riga := db.ProvaRipristino{Avvio: db.AvvioProvaAutomatico, Settimana: SettimanaDi(Turno(adesso, ora)), IniziatoIl: adesso}
	if err != nil {
		return n.registraProva(ctx, riga, EsitoProva{}, err)
	}
	defer func() { err = errors.Join(err, l.Rilascia()) }()
	return n.eseguiProva(ctx, l, riga, nil)
}

// ProvaAMano è la prova del comando gestrip prova-backup: con il lucchetto l già preso, scrive la riga «a mano»
// (conta per l'allarme, non per il calendario) e dice su dire cosa succede.
func (n *Notturno) ProvaAMano(ctx context.Context, l *Lucchetto, dire func(string) error) (db.ProvaRipristino, bool, error) {
	ora, err := n.Ora(ctx)
	if err != nil {
		return db.ProvaRipristino{}, false, err
	}
	adesso := n.o.Adesso()
	riga := db.ProvaRipristino{Avvio: db.AvvioProvaAMano, Settimana: SettimanaDi(Turno(adesso, ora)), IniziatoIl: adesso}
	return n.eseguiProva(ctx, l, riga, dire)
}

// eseguiProva fa la prova con il lucchetto l già preso e scrive la riga. Uno stop che la ferma non lascia righe;
// un panico è una prova fallita con DettaglioPanicoProva.
func (n *Notturno) eseguiProva(ctx context.Context, l *Lucchetto, riga db.ProvaRipristino, dire func(string) error) (db.ProvaRipristino, bool, error) {
	var ep EsitoProva
	var err error
	func() {
		defer func() {
			if v := recover(); v != nil {
				n.log.Error("prova di ripristino interrotta da un panico", "avvio", riga.Avvio, "tipo", fmt.Sprintf("%T", v),
					"stack", string(debug.Stack()))
				err = errors.New(DettaglioPanicoProva)
			}
		}()
		pr := Prova{Lucchetto: l, DB: n.d, Chiavetta: n.chiavetta, Adesso: riga.IniziatoIl, Verifica: n.verifica, Dire: dire}
		ep, err = pr.Esegui(ctx)
	}()
	if err != nil && ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		n.log.Info("prova di ripristino fermata dallo stop del programma: la settimana resta da fare", "avvio", riga.Avvio)
		return db.ProvaRipristino{}, false, err
	}
	return n.registraProva(ctx, riga, ep, err)
}

// registraProva scrive la riga della prova: riuscita se errProva è nil, altrimenti fallita con il motivo. Anche
// dopo lo stop (contesto staccato, al più TempoScritturaStorico). Se non si scrive, una prova automatica si conta
// in memoria (come il backup, revisione della 124): una riga mancante non fa rifare la verifica a ogni minuto.
func (n *Notturno) registraProva(ctx context.Context, riga db.ProvaRipristino, ep EsitoProva, errProva error) (db.ProvaRipristino, bool, error) {
	riga.FinitoIl = n.o.Adesso()
	riga.Riuscita = errProva == nil
	if ep.Archivio.Archivio != "" {
		riga.Archivio, riga.SHA256, riga.CatturatoIl = ep.Archivio.Archivio, ep.Archivio.SHA256, ep.Archivio.CatturatoIl
	}
	riga.MediaRovinati = len(ep.Verificato.MediaRovinati)
	if !riga.Riuscita {
		riga.Errore = "errore senza testo"
		if t := strings.TrimSpace(errProva.Error()); t != "" {
			riga.Errore = t
		}
		if len(riga.Errore) > maxErroreEsecuzione {
			riga.Errore = strings.ToValidUTF8(riga.Errore[:maxErroreEsecuzione], "")
		}
		n.log.Warn("prova di ripristino non riuscita: il motivo è nel registro delle prove", "avvio", riga.Avvio,
			"tipo", fmt.Sprintf("%T", errProva))
	} else {
		n.log.Info("prova di ripristino riuscita", "avvio", riga.Avvio, "media_rovinati", riga.MediaRovinati)
	}
	ctxScrivi, annulla := staccato(ctx)
	defer annulla()
	id, err := n.inserisciProva(ctxScrivi, n.d, riga)
	if err != nil {
		if riga.Avvio == db.AvvioProvaAutomatico {
			n.ricordaNonScritta(chiaveProva(riga.Settimana), riga.Riuscita, riga.FinitoIl)
		}
		n.log.Error("prova di ripristino: la riga non si è scritta, il tentativo è contato solo in memoria",
			"avvio", riga.Avvio, "tipo", fmt.Sprintf("%T", err))
		return db.ProvaRipristino{}, false, errors.Join(errProva, fmt.Errorf("prova di ripristino: registro delle prove: %w", err))
	}
	riga.ID = id
	return riga, true, errProva
}

func (n *Notturno) provaProtetta(ctx context.Context) {
	defer func() {
		if v := recover(); v != nil {
			n.log.Error("prova di ripristino: giro interrotto da un panico, si riprova al prossimo giro", "tipo", fmt.Sprintf("%T", v),
				"stack", string(debug.Stack()))
		}
	}()
	if _, _, err := n.GiroProva(ctx); err != nil && ctx.Err() == nil {
		n.log.Warn("prova di ripristino: giro non finito o prova non riuscita", "tipo", fmt.Sprintf("%T", err))
	}
}
