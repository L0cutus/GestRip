package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La prova mensile della copia remota (slice 129c, T5.2, OPS-28, risposta E del piano: «una volta al mese un
// download completo di notte con la verifica intera»). La copia notturna (128) controlla sul server solo la
// dimensione (SFTP non dà lo SHA-256): questa prova scarica davvero l'ultima copia e la riapre come in un ripristino.
// Lo standalone non ce l'ha nel pacchetto: su otto-vps restic copia e la prova di ripristino controlla.
//
//   - **Quale copia:** l'ultima riga «sftp» 'ok' del registro dei backup (db.UltimoVerificatoRemoto), cioè l'ultima
//     copia remota registrata, che la rotazione remota non toglie mai. Deve stare nella cartella remota configurata.
//   - **Cosa fa:** controlla l'età della copia (EtaMassimaCopiaProvata), lo spazio locale (i byte della copia più
//     MargineSpazio nel volume dei dati), si collega come la copia notturna (impronta, chiave dell'apparecchio,
//     sorvegliante dei tempi), guarda sul server tipo e dimensione, scarica in una cartella temporanea del volume dei
//     dati (prefissoScaricamento) calcolando lo SHA-256, confronta byte e SHA-256 con il registro, poi fa la verifica
//     completa della 121 (VerificaArchivio: decifratura, integrity_check, conteggi, impronte dei media, D-116) e
//     confronta di nuovo SHA-256, byte e ora di cattura. La cartella temporanea si toglie sempre (anche con un
//     panico o lo stop); se un blackout la lascia, la toglie PulisciResti.
//   - **Quando gira:** nel lavoro notturno, dopo il backup e dopo la prova settimanale (125) dello stesso giro. Il
//     mese di una prova è il mese della domenica della settimana del turno (MeseDi): la prova di un mese è dovuta
//     dalla prima domenica del mese, appena il turno del backup e la prova settimanale di quella settimana sono
//     chiusi. Finché il mese non ha una prova riuscita (spento quella notte, o prove fallite) si riprova **ogni
//     notte**, dopo il backup, fino alla prima domenica del mese seguente; dentro ogni notte (turno) al più
//     TentativiPerTurno tentativi a AttesaFraTentativi (revisione della 129c: un mese fallito non resta acceso per
//     cinque settimane quando le copie tornano buone). Le prove a mano non contano per il calendario.
//   - **Solo con la copia remota configurata:** senza ChiaveRemoto la prova non è dovuta e lo stato non ne parla.
//     Con la configurazione appena scritta e nessuna copia ancora registrata la prova aspetta EtaMassimaBackup (la
//     prima copia notturna), poi fallisce con «nessuna».
//   - **Lucchetti:** solo il lucchetto dei backup (la chiavetta non serve): mai insieme a un backup, a una verifica o
//     alla prova settimanale. Il download non scrive mai nel registro dei backup (non sblocca D-11) e non tocca la
//     rotazione, né locale né remota.
//   - **Esito:** una riga di prove_remote (migrazione 0025) con un codice fisso; lo stato dei backup ne fa i problemi
//     ProblemaProvaRemotaFallita e ProblemaProvaRemotaMancante (banner e avvisi). Uno stop di serve a metà non lascia
//     righe.

// EtaMassimaCopiaProvata è l'età massima della copia remota scaricata: l'ora di cattura contro l'inizio della prova.
// Otto giorni come la prova settimanale (EtaMassimaProva): è la stessa domanda («da quanto vecchio ripartirei?»).
// Non 26 ore: quelle le guarda già ProblemaRemotoVecchio ogni giorno, e una sola notte senza rete non deve far
// fallire anche la prova del mese.
const EtaMassimaCopiaProvata = EtaMassimaProva

// EtaMassimaProvaRemota è l'età oltre la quale manca la prova mensile (ProblemaProvaRemotaMancante): 36 giorni,
// cioè le 5 settimane al più fra due prime domeniche del mese più un giorno, come gli 8 giorni (7 + 1) della prova
// settimanale e le 26 ore (24 + 2) del backup.
const EtaMassimaProvaRemota = 36 * 24 * time.Hour

// prefissoScaricamento è il nome della cartella temporanea del download, nel volume dei dati (PulisciResti la
// toglie se un blackout la lascia).
const prefissoScaricamento = ".scarica-backup-"

// I codici della prova remota che la copia notturna non ha (gli altri sono quelli della copia: configurazione,
// chiave, rete, impronta, accesso, cartella, spazio, controllo, altro). Lo stesso elenco del CHECK della 0025.
const (
	CodiceRemotoNessuna      = "nessuna"      // nel registro nessuna copia remota (nella cartella configurata)
	CodiceRemotoVecchia      = "vecchia"      // l'ultima copia ha più di EtaMassimaCopiaProvata
	CodiceRemotoOrologio     = "orologio"     // l'ultima copia è catturata nel futuro
	CodiceRemotoAssente      = "assente"      // la copia del registro non c'è sul server
	CodiceRemotoScaricamento = "scaricamento" // il download si è interrotto
	CodiceRemotoVerifica     = "verifica"     // la copia scaricata non passa la verifica completa
)

// DettaglioPanicoProvaRemota è il motivo di una prova remota interrotta da un panico: il dettaglio va solo nel
// registro del programma.
const DettaglioPanicoProvaRemota = "la prova della copia remota si è interrotta per un errore interno: il dettaglio è nel registro del programma"

var (
	// ErrProvaRemotaSenzaCopia: il registro non ha una copia remota riuscita nella cartella configurata.
	ErrProvaRemotaSenzaCopia = errors.New("prova della copia remota: nel registro non c'è una copia remota riuscita nella cartella configurata")
	// ErrProvaRemotaVecchia: l'ultima copia remota è più vecchia di EtaMassimaCopiaProvata.
	ErrProvaRemotaVecchia = errors.New("prova della copia remota: l'ultima copia remota ha più di 8 giorni")
	// ErrProvaRemotaAssente: la copia del registro non c'è sul server.
	ErrProvaRemotaAssente = errors.New("prova della copia remota: la copia del registro non c'è più sul server")
	// ErrProvaRemotaDiversa: la copia sul server o scaricata non ha i byte o lo SHA-256 del registro.
	ErrProvaRemotaDiversa = errors.New("prova della copia remota: la copia non ha i byte del registro")
	// ErrProvaRemotaTroppoGrande: il server manda più byte di quelli del registro.
	ErrProvaRemotaTroppoGrande = errors.New("il server manda più byte della copia registrata")
)

// MeseDi è il mese della prova remota del turno: il mese della sua settimana (SettimanaDi), «aaaa-mm». Così il mese
// comincia con la prima domenica: dal primo del mese al sabato prima, la settimana è ancora quella del mese prima.
func MeseDi(turno clock.Data) string {
	s := SettimanaDi(turno)
	return fmt.Sprintf("%04d-%02d", s.Anno, int(s.Mese))
}

// ProvaRemota è una prova della copia remota da fare.
type ProvaRemota struct {
	// Lucchetto è il lucchetto dei backup, già preso da chi chiama e rilasciato da lui.
	Lucchetto *Lucchetto
	DB        *db.DB
	// Remoto è la copia remota configurata (CaricaRemoto); nil: non configurata.
	Remoto *Remoto
	// Adesso è l'inizio della prova: l'età della copia si misura da qui.
	Adesso time.Time
	// Verifica è la verifica dell'archivio scaricato; nil: VerificaArchivio.
	Verifica func(ctx context.Context, dati, file string) (Verificato, error)
	// Dire riceve le righe per chi guarda (il comando le stampa); un suo errore non ferma la prova (come Prova).
	Dire func(riga string) error
}

// EsitoProvaRemota è quello che la prova ha trovato: la copia del registro (vuota se non ce n'era una) e, se la
// verifica è arrivata in fondo, quello che ha letto.
type EsitoProvaRemota struct {
	Copia      db.ArchivioRegistrato
	Verificato Verificato
}

func (p ProvaRemota) dire(formato string, arg ...any) {
	if p.Dire != nil {
		_ = p.Dire(fmt.Sprintf(formato, arg...)) // l'uscita non decide l'esito della prova
	}
}

// Esegui fa la prova, nell'ordine: le cartelle temporanee rimaste (PulisciResti) → la configurazione → l'ultima
// copia remota del registro nella cartella configurata → l'età → la chiave dell'apparecchio → lo spazio locale → il
// download con byte e SHA-256 → la verifica completa → stessi byte, SHA-256 e ora di cattura del registro. Solo un
// errore nil vuol dire riuscita; un errore porta il suo codice (CodiceRemoto). Non scrive niente: la riga la scrive
// chi chiama.
func (p ProvaRemota) Esegui(ctx context.Context) (e EsitoProvaRemota, err error) {
	if !p.Lucchetto.Preso() {
		return e, ErrSenzaLucchetto
	}
	dati := filepath.Dir(p.DB.Percorso())
	if !p.Lucchetto.diDati(dati) {
		return e, fmt.Errorf("%w: il lucchetto è di un'altra cartella dei dati", ErrSenzaLucchetto)
	}
	resti, err := p.Lucchetto.PulisciResti()
	for _, n := range resti {
		p.dire("tolta %s, cartella temporanea rimasta da un backup interrotto\n", filepath.Join(dati, n))
	}
	if err != nil {
		return e, err
	}
	if p.Remoto == nil {
		return e, errRemoto(CodiceRemotoConfigurazione, ErrRemotoNonConfigurato)
	}
	if p.Remoto.ErrConfig != nil {
		return e, errRemoto(CodiceRemotoConfigurazione, p.Remoto.ErrConfig)
	}
	if err := p.Remoto.Config.Controlla(); err != nil {
		return e, errRemoto(CodiceRemotoConfigurazione, err)
	}
	u, ok, err := db.UltimoVerificatoRemoto(ctx, p.DB.Lettura)
	if err != nil {
		return e, err
	}
	// La copia deve stare proprio nella cartella configurata (non in una che comincia allo stesso modo) e avere il
	// nome di un archivio del programma, come per la rotazione remota.
	if !ok || path.Dir(u.Archivio) != path.Clean(p.Remoto.Config.Cartella) || !reArchivio.MatchString(path.Base(u.Archivio)) {
		return e, errRemoto(CodiceRemotoNessuna, ErrProvaRemotaSenzaCopia)
	}
	e.Copia = u
	p.dire("prova della copia remota %s (%d byte)\ncatturata il: %s\n", path.Base(u.Archivio), u.Byte,
		u.CatturatoIl.Format(db.FormatoIstante))
	switch eta := p.Adesso.Sub(u.CatturatoIl); {
	case eta > EtaMassimaCopiaProvata:
		return e, errRemoto(CodiceRemotoVecchia, fmt.Errorf("%w: catturata il %s, %d giorni fa", ErrProvaRemotaVecchia,
			clock.Timbro(u.CatturatoIl), int(eta/(24*time.Hour))))
	case eta < 0:
		return e, errRemoto(CodiceRemotoOrologio, fmt.Errorf("%w: l'ultima copia remota è catturata nel futuro (%s)",
			db.ErrOrologioBackup, clock.Timbro(u.CatturatoIl)))
	}
	firma, err := LeggiChiaveSFTP(p.Remoto.Dati)
	if err != nil {
		return e, errRemoto(CodiceRemotoChiave, err)
	}
	libero, err := spazioLibero(dati)
	if err != nil {
		return e, errRemoto(CodiceRemotoSpazio, fmt.Errorf("%w: spazio libero del volume dei dati non leggibile: %w", ErrSpazio, err))
	}
	if serve := uint64(u.Byte) + MargineSpazio; libero < serve {
		return e, errRemoto(CodiceRemotoSpazio, fmt.Errorf("%w: per scaricare la copia servono %d byte (copia e 10 GiB di margine), ci sono %d byte",
			ErrSpazio, serve, libero))
	}
	tmp, err := os.MkdirTemp(dati, prefissoScaricamento) // 0700
	if err != nil {
		return e, errRemoto(CodiceRemotoAltro, fmt.Errorf("prova della copia remota: cartella temporanea: %w", err))
	}
	defer func() {
		if errRm := togliScaricamento(tmp); errRm != nil {
			err = errors.Join(err, fmt.Errorf("prova della copia remota: cartella temporanea %s da togliere a mano: %w", tmp, errRm))
		}
	}()
	file := filepath.Join(tmp, path.Base(u.Archivio))
	if err := p.Remoto.scarica(ctx, firma, u, file); err != nil {
		return e, err
	}
	p.dire("scaricata: %d byte, SHA-256 uguale al registro\n", u.Byte)
	verifica := p.Verifica
	if verifica == nil {
		verifica = VerificaArchivio
	}
	v, err := verifica(ctx, dati, file)
	if err != nil {
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return e, err // lo stop: senza codice, niente riga
		}
		if errors.Is(err, ErrSpazio) {
			return e, errRemoto(CodiceRemotoSpazio, err)
		}
		return e, errRemoto(CodiceRemotoVerifica, err)
	}
	e.Verificato = v
	if v.SHA256 != u.SHA256 || v.ByteCifrati != u.Byte {
		return e, errRemoto(CodiceRemotoControllo, fmt.Errorf("%w: la copia verificata è un'altra", ErrProvaRemotaDiversa))
	}
	if t, errT := time.Parse(db.FormatoIstante, v.CatturatoIl); errT != nil || !t.Equal(u.CatturatoIl) {
		return e, errRemoto(CodiceRemotoControllo, fmt.Errorf("%w: ora di cattura diversa dal registro", ErrProvaRemotaDiversa))
	}
	p.dire("verificata: decifrata, %d file con l'impronta giusta, database integro, %d tabelle con i conteggi giusti\n",
		len(v.Inventario), len(v.Conteggi))
	if len(v.MediaRovinati) > 0 {
		p.dire("ATTENZIONE: %d media con il contenuto diverso dal nome (rovinati sul disco prima del backup): "+
			"la prova vale lo stesso\n", len(v.MediaRovinati))
	}
	return e, nil
}

// togliScaricamento toglie la cartella temporanea del download; una variabile per i test.
var togliScaricamento = os.RemoveAll

// scarica scarica dal server la copia u nel file locale file (nuovo, 0600): sessione come la copia notturna (con la
// durata massima TempoMassimo e il sorvegliante), cartella, tipo e dimensione sul server, download con lo SHA-256,
// confronto con il registro. Lo stop è l'errore del contesto, senza codice.
func (r *Remoto) scarica(ctx context.Context, firma ssh.Signer, u db.ArchivioRegistrato, file string) error {
	ctx, annulla := context.WithTimeoutCause(ctx, durataODi(r.TempoMassimo, TempoMassimoCopiaRemota), errTempoMassimo)
	defer annulla()
	s, err := r.collega(ctx, firma)
	if err != nil {
		return err
	}
	defer s.chiudi()
	if err := s.cartella(r.Config.Cartella); err != nil {
		return err
	}
	fi, err := s.sftp.Lstat(u.Archivio)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errRemoto(CodiceRemotoAssente, ErrProvaRemotaAssente)
	case err != nil:
		return s.causa(errRemoto(CodiceRemotoRete, fmt.Errorf("server remoto: %w", err)))
	case !fi.Mode().IsRegular() || fi.Size() != u.Byte:
		return errRemoto(CodiceRemotoControllo, fmt.Errorf("%w: sul server %d byte invece di %d (o non è un file)",
			ErrProvaRemotaDiversa, fi.Size(), u.Byte))
	}
	rf, err := s.sftp.Open(u.Archivio)
	if err != nil {
		return s.causa(errRemoto(CodiceRemotoScaricamento, fmt.Errorf("apertura sul server remoto: %w", err)))
	}
	defer func() { _ = rf.Close() }() // sola lettura
	lf, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errRemoto(CodiceRemotoAltro, fmt.Errorf("prova della copia remota: file locale: %w", err))
	}
	h := sha256.New()
	var dest io.Writer = lf
	if scrittoreScaricamentoPerTest != nil {
		dest = scrittoreScaricamentoPerTest(dest)
	}
	// WriteTo legge con più richieste insieme (la banda del negozio, non la latenza); il tetto ferma un server che
	// manda più byte di quelli visti con Lstat.
	n, errCopia := rf.WriteTo(&scrittoreConTetto{w: io.MultiWriter(dest, h), resta: u.Byte})
	errChiudi := lf.Close()
	if err := errors.Join(errCopia, errChiudi); err != nil {
		switch {
		case ctx.Err() != nil && s.sorv.scattato() == nil:
			return ctx.Err()
		case errors.Is(err, ErrProvaRemotaTroppoGrande):
			return errRemoto(CodiceRemotoControllo, fmt.Errorf("%w: %w", ErrProvaRemotaDiversa, err))
		case errors.Is(err, syscall.ENOSPC):
			return errRemoto(CodiceRemotoSpazio, fmt.Errorf("%w: disco pieno durante il download", ErrSpazio))
		}
		return s.causa(errRemoto(CodiceRemotoScaricamento, fmt.Errorf("download dal server remoto: %w", senzaIndirizzo(err))))
	}
	if n != u.Byte || hex.EncodeToString(h.Sum(nil)) != u.SHA256 {
		return errRemoto(CodiceRemotoControllo, fmt.Errorf("%w: scaricati %d byte con un altro SHA-256", ErrProvaRemotaDiversa, n))
	}
	return nil
}

// scrittoreScaricamentoPerTest, solo nei test, cambia il file locale del download (disco pieno).
var scrittoreScaricamentoPerTest func(io.Writer) io.Writer

// scrittoreConTetto scrive al più resta byte; oltre dà ErrProvaRemotaTroppoGrande.
type scrittoreConTetto struct {
	w     io.Writer
	resta int64
}

func (s *scrittoreConTetto) Write(p []byte) (int, error) {
	if int64(len(p)) > s.resta {
		return 0, ErrProvaRemotaTroppoGrande
	}
	n, err := s.w.Write(p)
	s.resta -= int64(n)
	return n, err
}

// ProvaRemotaDovuta dice se adesso la prova mensile automatica della copia remota va fatta.
func (n *Notturno) ProvaRemotaDovuta(ctx context.Context) (bool, error) {
	ora, err := n.Ora(ctx)
	if err != nil {
		return false, err
	}
	return n.provaRemotaDovuta(ctx, n.o.Adesso(), ora)
}

// provaRemotaDovuta: chiavetta e copia remota configurate; il turno del backup di adesso chiuso e la prova
// settimanale della sua settimana chiusa (riuscita, o con i tentativi finiti: «dopo la prova settimanale»); una copia
// remota nel registro, o la configurazione più vecchia di EtaMassimaBackup (la prima copia notturna ha avuto il suo
// tempo); il mese del turno senza una prova automatica riuscita; per il turno la regola dei tentativi (daRifare) sulle
// prove remote automatiche di quella notte: un mese fallito si riprova ogni notte, al più tre volte per notte.
func (n *Notturno) provaRemotaDovuta(ctx context.Context, adesso time.Time, ora time.Duration) (bool, error) {
	if !n.Configurato() {
		return false, nil
	}
	_, dal, configurata, err := LeggiConfigRemota(ctx, n.d.Lettura)
	if !configurata {
		return false, err
	}
	turno := Turno(adesso, ora)
	tutte, riuscite, _, err := n.statoTurno(ctx, turno)
	if err != nil {
		return false, err
	}
	if riuscite == 0 && tutte < TentativiPerTurno {
		return false, nil // il backup del turno è ancora da fare
	}
	settimana := SettimanaDi(turno)
	tutte, riuscite, fine, err := db.TentativiProva(ctx, n.d.Lettura, db.AvvioProvaAutomatico, settimana)
	if err != nil {
		return false, err
	}
	tutte, riuscite, _ = n.conNonScritte(chiaveProva(settimana), tutte, riuscite, fine)
	if riuscite == 0 && tutte < TentativiPerTurno {
		return false, nil // la prova settimanale è ancora da fare
	}
	_, ce, err := db.UltimoVerificatoRemoto(ctx, n.d.Lettura)
	if err != nil {
		return false, err
	}
	if da := adesso.Sub(dal); !ce && da >= 0 && da <= EtaMassimaBackup {
		return false, nil // configurata da poco: si aspetta la prima copia notturna
	}
	mese := MeseDi(turno)
	tutte, riuscite, fine, err = db.TentativiProvaRemota(ctx, n.d.Lettura, db.AvvioProvaAutomatico, mese, turno)
	if err != nil {
		return false, err
	}
	_, riuscite, _ = n.conNonScritte(chiaveMeseRemoto(mese), 0, riuscite, time.Time{})
	tutte, _, fine = n.conNonScritte(chiaveProvaRemota(mese, turno), tutte, 0, fine)
	return daRifare(adesso, tutte, riuscite, fine), nil
}

// chiaveProvaRemota è la chiave di nonScritte per i tentativi di una notte (turno) di una prova remota; chiaveMeseRemoto
// quella per la riuscita del mese.
func chiaveProvaRemota(mese string, turno clock.Data) string {
	return "prova remota " + mese + " " + turno.String()
}
func chiaveMeseRemoto(mese string) string { return "prova remota riuscita " + mese }

// caricaRemotoPerProva legge la configurazione della copia remota per una prova: nil se non è configurata; con una
// configurazione illeggibile un Remoto con ErrConfig (la prova fallisce con «configurazione»).
func (n *Notturno) caricaRemotoPerProva(ctx context.Context, prepara func(*Remoto)) *Remoto {
	rm, err := CaricaRemoto(ctx, n.d)
	if err != nil {
		rm = &Remoto{Dati: n.dati, ErrConfig: err}
	}
	if rm != nil && prepara != nil {
		prepara(rm)
	}
	return rm
}

// GiroProvaRemota fa la prova mensile automatica se è dovuta adesso. eseguita dice se c'è una riga nuova nel
// registro delle prove remote. Con il lucchetto occupato non fa niente: il mese resta da fare.
func (n *Notturno) GiroProvaRemota(ctx context.Context) (p db.ProvaRemota, eseguita bool, err error) {
	ora, err := n.Ora(ctx)
	if err != nil {
		return db.ProvaRemota{}, false, err
	}
	adesso := n.o.Adesso()
	dovuta, err := n.provaRemotaDovuta(ctx, adesso, ora)
	if err != nil || !dovuta {
		return db.ProvaRemota{}, false, err
	}
	rm := n.caricaRemotoPerProva(ctx, n.preparaRemoto)
	if rm == nil {
		return db.ProvaRemota{}, false, nil // tolta fra il controllo e adesso
	}
	l, err := PrendiLucchetto(n.dati)
	if errors.Is(err, ErrBackupInCorso) {
		n.log.Info("prova della copia remota: un backup è in corso, riprovo al prossimo giro")
		return db.ProvaRemota{}, false, nil
	}
	turno := Turno(adesso, ora)
	riga := db.ProvaRemota{Avvio: db.AvvioProvaAutomatico, Mese: MeseDi(turno), Turno: turno, IniziatoIl: adesso}
	if err != nil {
		return n.registraProvaRemota(ctx, riga, EsitoProvaRemota{}, errRemoto(CodiceRemotoAltro, err))
	}
	defer func() { err = errors.Join(err, l.Rilascia()) }()
	return n.eseguiProvaRemota(ctx, l, rm, riga, nil)
}

// ProvaRemotaAMano è la prova del comando gestrip prova-backup -remota: con il lucchetto l già preso, scrive la riga
// «a mano» (conta per l'allarme, non per il calendario) e dice su dire cosa succede. prepara cambia la copia remota
// prima di collegarsi (solo i test: il server finto); nil: niente. Senza copia remota configurata
// ErrRemotoNonConfigurato e nessuna riga.
func (n *Notturno) ProvaRemotaAMano(ctx context.Context, l *Lucchetto, prepara func(*Remoto), dire func(string) error) (db.ProvaRemota, bool, error) {
	ora, err := n.Ora(ctx)
	if err != nil {
		return db.ProvaRemota{}, false, err
	}
	rm := n.caricaRemotoPerProva(ctx, prepara)
	if rm == nil {
		return db.ProvaRemota{}, false, ErrRemotoNonConfigurato
	}
	adesso := n.o.Adesso()
	turno := Turno(adesso, ora)
	riga := db.ProvaRemota{Avvio: db.AvvioProvaAMano, Mese: MeseDi(turno), Turno: turno, IniziatoIl: adesso}
	return n.eseguiProvaRemota(ctx, l, rm, riga, dire)
}

// eseguiProvaRemota fa la prova con il lucchetto l già preso e scrive la riga. Uno stop che la ferma non lascia
// righe; un panico è una prova fallita con DettaglioPanicoProvaRemota (la cartella temporanea si toglie lo stesso:
// il defer di Esegui gira prima del recover).
func (n *Notturno) eseguiProvaRemota(ctx context.Context, l *Lucchetto, rm *Remoto, riga db.ProvaRemota, dire func(string) error) (db.ProvaRemota, bool, error) {
	var ep EsitoProvaRemota
	var err error
	func() {
		defer func() {
			if v := recover(); v != nil {
				n.log.Error("prova della copia remota interrotta da un panico", "avvio", riga.Avvio, "tipo", fmt.Sprintf("%T", v),
					"stack", string(debug.Stack()))
				err = errors.New(DettaglioPanicoProvaRemota) // senza codice: CodiceRemoto dà «altro»
			}
		}()
		pr := ProvaRemota{Lucchetto: l, DB: n.d, Remoto: rm, Adesso: riga.IniziatoIl, Verifica: n.verifica, Dire: dire}
		ep, err = pr.Esegui(ctx)
	}()
	if err != nil && ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		n.log.Info("prova della copia remota fermata dallo stop del programma: il mese resta da fare", "avvio", riga.Avvio)
		return db.ProvaRemota{}, false, err
	}
	return n.registraProvaRemota(ctx, riga, ep, err)
}

// registraProvaRemota scrive la riga della prova: riuscita se errProva è nil, altrimenti fallita con il codice e il
// motivo. Anche dopo lo stop (contesto staccato). Se non si scrive, una prova automatica si conta in memoria.
func (n *Notturno) registraProvaRemota(ctx context.Context, riga db.ProvaRemota, ep EsitoProvaRemota, errProva error) (db.ProvaRemota, bool, error) {
	riga.FinitoIl = n.o.Adesso()
	riga.Riuscita = errProva == nil
	if c := ep.Copia; c.Archivio != "" {
		riga.Archivio, riga.SHA256, riga.Byte, riga.CatturatoIl = c.Archivio, c.SHA256, c.Byte, c.CatturatoIl
	}
	riga.MediaRovinati = len(ep.Verificato.MediaRovinati)
	if !riga.Riuscita {
		riga.Codice = CodiceRemoto(errProva)
		if !slices.Contains(db.CodiciProvaRemota, riga.Codice) {
			riga.Codice = CodiceRemotoAltro
		}
		riga.Errore = "errore senza testo"
		if t := strings.TrimSpace(errProva.Error()); t != "" {
			riga.Errore = t
		}
		if len(riga.Errore) > maxErroreEsecuzione {
			riga.Errore = strings.ToValidUTF8(riga.Errore[:maxErroreEsecuzione], "")
		}
		n.log.Warn("prova della copia remota non riuscita: il motivo è nel registro delle prove remote", "avvio", riga.Avvio,
			"codice", riga.Codice)
	} else {
		n.log.Info("prova della copia remota riuscita", "avvio", riga.Avvio, "media_rovinati", riga.MediaRovinati)
	}
	ctxScrivi, annulla := staccato(ctx)
	defer annulla()
	id, err := n.inserisciProvaRemota(ctxScrivi, n.d, riga)
	if err != nil {
		if riga.Avvio == db.AvvioProvaAutomatico {
			n.ricordaNonScritta(chiaveProvaRemota(riga.Mese, riga.Turno), riga.Riuscita, riga.FinitoIl)
			if riga.Riuscita {
				n.ricordaNonScritta(chiaveMeseRemoto(riga.Mese), true, riga.FinitoIl)
			}
		}
		n.log.Error("prova della copia remota: la riga non si è scritta, il tentativo è contato solo in memoria",
			"avvio", riga.Avvio, "tipo", fmt.Sprintf("%T", err))
		return db.ProvaRemota{}, false, errors.Join(errProva, fmt.Errorf("prova della copia remota: registro delle prove remote: %w", err))
	}
	riga.ID = id
	return riga, true, errProva
}

func (n *Notturno) provaRemotaProtetta(ctx context.Context) {
	defer func() {
		if v := recover(); v != nil {
			n.log.Error("prova della copia remota: giro interrotto da un panico, si riprova al prossimo giro", "tipo", fmt.Sprintf("%T", v),
				"stack", string(debug.Stack()))
		}
	}()
	if _, _, err := n.GiroProvaRemota(ctx); err != nil && ctx.Err() == nil {
		n.log.Warn("prova della copia remota: giro non finito o prova non riuscita", "codice", CodiceRemoto(err))
	}
}
