package backup

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Lo stato dei backup per la pagina Impostazioni → Backup e per il banner rosso del titolare (slice 126, T5.2,
// tema E, risposta H del piano: «banner rosso solo per il titolare finché il problema resta»). Gli avvisi su
// Telegram ed email sono della 127: useranno gli stessi Problemi (il Codice dice quale problema nasce, resta o si
// risolve).
//
// Lo stato si legge con poche righe di database (storico delle esecuzioni, registro dei backup, registro delle
// prove) e i controlli della chiavetta della 123 (stat, statfs, access e la tabella dei montaggi di /proc): niente
// che legga o scriva file sulla chiavetta, niente ricalcolo degli archivi.

// EtaMassimaBackup è l'età oltre la quale l'ultimo backup buono accende l'allarme (risposta H, la stessa regola
// del controllo di otto-vps: «più di 26 ore»): un giorno e due ore di margine per il recupero e i tentativi.
const EtaMassimaBackup = 26 * time.Hour

// QuantiNelloStato sono quante esecuzioni e quante righe del registro la pagina mostra.
const QuantiNelloStato = 10

// I codici dei problemi (Problema.Codice): fissi, li useranno gli avvisi della 127.
const (
	ProblemaNonConfigurato = "non_configurato"
	ProblemaChiavetta      = "chiavetta"
	ProblemaBackupFallito  = "backup_fallito"
	ProblemaBackupVecchio  = "backup_vecchio"
	ProblemaOrologio       = "orologio"
	ProblemaProvaFallita   = "prova_fallita"
	ProblemaProvaMancante  = "prova_mancante"
	ProblemaMediaRovinati  = "media_rovinati"
)

// Problema è una condizione dell'allarme: Testo dice al titolare cosa succede e cosa fare, senza percorsi né chiavi.
type Problema struct {
	Codice, Testo string
}

// Stato è quello che serve alla pagina e al banner. Gli istanti sono UTC.
type Stato struct {
	Adesso       time.Time
	Configurato  bool  // il file dell'apparecchio dice dov'è la chiavetta
	LavoroSpento bool  // serve è partito con -senza-lavori-automatici: niente backup notturno né prova (Allarme)
	Chiavetta    error // l'esito dei controlli della chiavetta (nil: risponde); solo se Configurato
	Ora          time.Duration
	DaFare       bool      // il backup del turno di adesso è da fare (o da ritentare): parte al prossimo giro
	Prossimo     time.Time // la prossima ora del backup notturno dopo Adesso
	Esecuzioni   []Esecuzione
	Verifiche    []db.RigaRegistroBackup
	// UltimoBackup è l'ultimo backup verificato sulla chiavetta (quello di D-11 e della prova), se CeBackup.
	UltimoBackup db.ArchivioRegistrato
	CeBackup     bool
	PrimoBackup  time.Time // l'ora di cattura del primo backup verificato sulla chiavetta, zero senza
	PiuAvanti    time.Time // l'istante più avanti del registro dei backup (nota 8(a)), zero senza righe
	Prova        db.ProvaRipristino
	CeProva      bool
	Rovinati     int      // i media rovinati dell'ultimo backup verificato sulla chiavetta (D-116)
	NomiRovinati []string // i primi db.MaxNomiRovinati
}

// ConLavoroSpento dice al backup che serve non fa girare il lavoro notturno (-senza-lavori-automatici: sviluppo e
// banco, mai sull'apparecchio). Va chiamata prima che le facce partano.
func (n *Notturno) ConLavoroSpento() *Notturno {
	n.lavoroSpento = true
	return n
}

// ControllaChiavetta fa i controlli della chiavetta della 123 (punto di montaggio, dispositivo diverso dai dati,
// /dev/…, scrivibile) senza aprirla né prenderne il lucchetto: solo stat, statfs, access e /proc/self/mountinfo.
func ControllaChiavetta(cartella, dati string) error {
	_, err := controllaChiavetta(sistemaMontaggi, cartella, dati)
	return err
}

// Stato legge lo stato dei backup adesso. Con un errore di lettura Stato ha almeno Adesso, Configurato e
// LavoroSpento.
func (n *Notturno) Stato(ctx context.Context) (Stato, error) {
	s := Stato{Adesso: n.o.Adesso(), Configurato: n.Configurato(), LavoroSpento: n.lavoroSpento}
	ora, err := n.Ora(ctx)
	if err != nil {
		return s, fmt.Errorf("backup: ora del backup: %w", err)
	}
	s.Ora, s.Prossimo = ora, ProssimoGiro(s.Adesso, ora)
	if s.Configurato {
		s.Chiavetta = ControllaChiavetta(n.chiavetta, n.dati)
		if s.DaFare, err = n.dovuto(ctx, s.Adesso, ora); err != nil {
			return s, fmt.Errorf("backup: turno di adesso: %w", err)
		}
	}
	if s.Esecuzioni, err = n.Storico(ctx, QuantiNelloStato); err != nil {
		return s, fmt.Errorf("backup: storico delle esecuzioni: %w", err)
	}
	if s.Verifiche, err = db.UltimeVerificheBackup(ctx, n.d.Lettura, QuantiNelloStato); err != nil {
		return s, err
	}
	if s.UltimoBackup, s.CeBackup, err = db.UltimoVerificatoSullaChiavetta(ctx, n.d.Lettura); err != nil {
		return s, err
	}
	if s.PrimoBackup, s.PiuAvanti, err = db.IstantiRegistroBackup(ctx, n.d.Lettura); err != nil {
		return s, err
	}
	if s.Prova, s.CeProva, err = db.UltimaProvaRipristino(ctx, n.d.Lettura); err != nil {
		return s, err
	}
	if s.Rovinati, s.NomiRovinati, err = db.MediaRovinati(ctx, n.d.Lettura); err != nil {
		return s, err
	}
	return s, nil
}

// ProssimoGiro è la prima ora del backup (ora del muro di Roma) dopo adesso: oggi se non è ancora passata,
// altrimenti domani; con l'ora che viene due volte, la prima. Il giorno in cui alle 02:00 si salta alle 03:00,
// un'ora fra le 02:00 e le 03:00 la dice spostata in avanti di un'ora (time.Date), mentre Turno la raggiunge alle
// 03:00: è solo l'indicazione della pagina, il calendario vero è Turno.
func ProssimoGiro(adesso time.Time, ora time.Duration) time.Time {
	loc := clock.Locale(adesso)
	h, m, sec := int(ora/time.Hour), int(ora%time.Hour/time.Minute), int(ora%time.Minute/time.Second)
	for i := range 3 {
		t := time.Date(loc.Year(), loc.Month(), loc.Day()+i, h, m, sec, 0, clock.Roma)
		// Con le 02:30 che vengono due volte (ritorno all'ora solare) il backup parte alla prima.
		if prima := t.Add(-time.Hour); clock.Locale(prima).Format(time.TimeOnly) == clock.Locale(t).Format(time.TimeOnly) && prima.After(adesso) {
			t = prima
		}
		if t.After(adesso) {
			return t.UTC()
		}
	}
	return time.Time{}
}

// Problemi sono le condizioni dell'allarme di adesso, nell'ordine in cui la pagina e il banner le mostrano. Senza
// configurazione c'è solo quella: le altre (nessun backup, nessuna prova) ne sarebbero solo la conseguenza.
func (s Stato) Problemi() []Problema {
	if !s.Configurato {
		return []Problema{{ProblemaNonConfigurato, "Il backup automatico non è configurato: i dati del negozio non " +
			"vengono copiati da nessuna parte. Chiedi a chi ha installato l'apparecchio di configurare la chiavetta dei backup."}}
	}
	var v []Problema
	if s.Chiavetta != nil {
		v = append(v, Problema{ProblemaChiavetta, testoChiavetta(s.Chiavetta)})
	}
	if len(s.Esecuzioni) > 0 && s.Esecuzioni[0].Esito == EsitoFallito {
		v = append(v, Problema{ProblemaBackupFallito, "L'ultimo backup (" + clock.Timbro(s.Esecuzioni[0].IniziatoIl) +
			") non è riuscito. Il motivo è nella pagina Backup: sistemato il problema, premi «Esegui adesso»."})
	}
	switch {
	case !s.CeBackup:
		v = append(v, Problema{ProblemaBackupVecchio, "Sulla chiavetta non c'è ancora nessun backup buono. " +
			"Controlla la chiavetta e premi «Esegui adesso» nella pagina Backup."})
	case s.Adesso.Sub(s.UltimoBackup.CatturatoIl) > EtaMassimaBackup:
		v = append(v, Problema{ProblemaBackupVecchio, "L'ultimo backup buono è del " + clock.Timbro(s.UltimoBackup.CatturatoIl) +
			": più di 26 ore fa. Controlla che l'apparecchio resti acceso la notte e che la chiavetta sia inserita, " +
			"poi premi «Esegui adesso» nella pagina Backup."})
	}
	if s.PiuAvanti.After(s.Adesso) {
		v = append(v, Problema{ProblemaOrologio, "Nel registro dei backup c'è un'ora nel futuro (" + clock.Timbro(s.PiuAvanti) +
			"): l'orologio dell'apparecchio è andato avanti e poi è stato corretto, o è sbagliato adesso. Controlla data e " +
			"ora dell'apparecchio. Finché l'orologio non arriva a quell'ora, le cancellazioni che aspettano un backup " +
			"verificato restano ferme: se manca molto, chiedi assistenza."})
	}
	switch {
	case s.CeProva && !s.Prova.Riuscita:
		v = append(v, Problema{ProblemaProvaFallita, "L'ultima prova di ripristino (" + clock.Timbro(s.Prova.FinitoIl) +
			") non è riuscita: in caso di guasto il backup potrebbe non bastare. Il motivo è nella pagina Backup; " +
			"fai controllare la chiavetta. La prova si ripete da sola la domenica notte."})
	case s.CeProva && s.Adesso.Sub(s.Prova.FinitoIl) > EtaMassimaProva,
		!s.CeProva && s.CeBackup && !s.PrimoBackup.IsZero() && s.Adesso.Sub(s.PrimoBackup) > EtaMassimaProva:
		v = append(v, Problema{ProblemaProvaMancante, "La prova di ripristino settimanale non si fa da più di 8 giorni. " +
			"Controlla che l'apparecchio resti acceso la notte fra sabato e domenica."})
	}
	if s.Rovinati > 0 {
		cosa := strconv.Itoa(s.Rovinati) + " foto o file rovinati"
		if s.Rovinati == 1 {
			cosa = "1 foto o file rovinato"
		}
		v = append(v, Problema{ProblemaMediaRovinati, "L'ultimo backup ha trovato " + cosa + " sul disco dell'apparecchio: " +
			"il backup vale lo stesso, ma quei file non sono più quelli salvati. Fai controllare il disco dall'assistenza."})
	}
	return v
}

// testoChiavetta è il problema della chiavetta in parole del titolare: assente (o non montata), presente ma non
// usabile, o un controllo che non riesce per un motivo dell'apparecchio (cartella dei dati, tabella dei montaggi):
// allora non dice che la chiavetta manca. Mai il percorso.
func testoChiavetta(err error) string {
	switch {
	case errors.Is(err, ErrNonMontata), errors.Is(err, ErrDestinazione):
		return "La chiavetta dei backup non c'è o non risponde: controlla che sia inserita bene nell'apparecchio. " +
			"Finché non c'è, il backup non si fa."
	case errors.Is(err, ErrChiavettaSolaLettura):
		return "La chiavetta dei backup è inserita ma non si può scrivere (protetta o montata in sola lettura): " +
			"controllala e reinseriscila. Finché non si sistema, il backup non si fa."
	case errors.Is(err, ErrStessoDisco), errors.Is(err, ErrNonChiavetta):
		return "Al posto della chiavetta dei backup c'è qualcos'altro: il backup non si fa. Chiedi assistenza."
	}
	return "Il controllo della chiavetta dei backup non riesce per un problema dell'apparecchio, non della chiavetta: " +
		"il backup non si fa. Chiedi assistenza."
}

// taccionoConLavoroSpento sono i problemi che con il lavoro notturno spento (-senza-lavori-automatici: sviluppo e
// banco) non sono problemi: nessun backup automatico né prova sono attesi, e senza configurazione non c'è niente da
// fare. Gli altri (chiavetta, un'esecuzione fallita anche da «Esegui adesso», prova fallita, media rovinati,
// orologio) dicono un guasto vero e suonano comunque (scelta del coordinatore, revisione della 126).
var taccionoConLavoroSpento = map[string]bool{ProblemaNonConfigurato: true, ProblemaBackupVecchio: true, ProblemaProvaMancante: true}

// Allarme sono i problemi da mostrare nel banner: tutti, tranne taccionoConLavoroSpento con il lavoro spento (la
// pagina Backup li mostra comunque).
func (s Stato) Allarme() []Problema {
	var v []Problema
	for _, p := range s.Problemi() {
		if !s.LavoroSpento || !taccionoConLavoroSpento[p.Codice] {
			v = append(v, p)
		}
	}
	return v
}
