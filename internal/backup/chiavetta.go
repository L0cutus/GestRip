package backup

// La destinazione dei backup e la chiavetta USB (slice 123, T5.2, D-11, D-114, risposta F del piano).
// Lo standalone scrive l'archivio in una cartella qualsiasi e non controlla dove sta (la copia fuori sede
// la fa restic su otto-vps). Il Go scrive sulla chiavetta e da qui, nel registro, la destinazione
// «chiavetta» sblocca le cancellazioni irreversibili dopo un import (122): per questo la chiavetta si
// riconosce con controlli stretti, e ogni dubbio la tratta come una cartella qualsiasi o rifiuta.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/db"
)

// Gli errori della destinazione e della chiavetta.
var (
	// ErrDestinazioneOccupata: un altro backup scrive o ruota nella stessa cartella (flock della cartella).
	ErrDestinazioneOccupata = errors.New("backup: un altro backup sta scrivendo nella stessa cartella, riprova quando ha finito")
	// ErrNonMontata: la cartella non è il punto di montaggio di un file system suo.
	ErrNonMontata = errors.New("backup: la cartella non è il punto di montaggio di una chiavetta (è montata?)")
	// ErrStessoDisco: la cartella sta sullo stesso dispositivo dei dati: non salva niente se il disco si rompe.
	ErrStessoDisco = errors.New("backup: la cartella sta sullo stesso disco dei dati, non è una chiavetta")
	// ErrNonChiavetta: il file system montato non viene da un dispositivo a blocchi (/dev/…): memoria
	// (tmpfs), rete, overlay.
	ErrNonChiavetta = errors.New("backup: la cartella non è montata da un dispositivo (/dev/…): non è una chiavetta")
	// ErrChiavettaSolaLettura: montata in sola lettura o non scrivibile dall'utente del programma.
	ErrChiavettaSolaLettura = errors.New("backup: la chiavetta non è scrivibile")
	// ErrChiavettaCambiata: fra l'inizio e la fine del backup la chiavetta non è più la stessa (tolta,
	// rimontata, un'altra al suo posto).
	ErrChiavettaCambiata = errors.New("backup: la chiavetta è cambiata durante il backup")
	// ErrRinominaNonSupportata (nota 6c): il file system non sa rinominare senza sovrascrivere né fare
	// collegamenti; il backup fallisce invece di rischiare.
	ErrRinominaNonSupportata = errors.New("backup: il file system della destinazione non sa rinominare un file senza " +
		"rischiare di sovrascriverne un altro (renameat2 RENAME_NOREPLACE e link non supportati, per esempio una " +
		"chiavetta NTFS o FAT montata con FUSE, o una cartella di rete): formatta la chiavetta in exFAT o ext4 " +
		"e montala con il driver del kernel")
)

// identitaFile è dispositivo e inode di un file o di una cartella.
type identitaFile struct{ dev, ino uint64 }

// voceCartella è un nome dentro la cartella della destinazione, visto senza seguire i collegamenti.
type voceCartella struct {
	nome    string
	normale bool
	byte    int64
	id      identitaFile
}

// sistema è quello che il controllo della chiavetta chiede al kernel: identità (stat, seguendo i
// collegamenti, e fstat di una cartella aperta), la tabella dei montaggi (/proc/self/mountinfo) e se si può
// scrivere. Una variabile per i
// test, che simulano «stesso dispositivo», «non montata», «tmpfs» senza montare niente.
type sistema interface {
	identita(p string) (identitaFile, error)
	identitaAperta(f *os.File) (identitaFile, error)
	montaggi() ([]byte, error)
	scrivibile(p string) error
}

var sistemaMontaggi sistema = sistemaVero{}

// montaggio è una riga di /proc/self/mountinfo (proc(5)): numero maggiore:minore, punto di montaggio,
// opzioni del montaggio, tipo e sorgente del file system.
type montaggio struct {
	majmin, punto, opzioni, tipo, sorgente string
}

// leggiMontaggi legge /proc/self/mountinfo: «id genitore maj:min radice punto opzioni [campi…] - tipo
// sorgente opzioni». Una riga che non torna è un errore: senza la tabella intera non si decide niente.
func leggiMontaggi(b []byte) ([]montaggio, error) {
	var mm []montaggio
	for i, riga := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if riga == "" {
			continue
		}
		c := strings.Split(riga, " ")
		sep := -1
		for j := 6; j < len(c); j++ {
			if c[j] == "-" {
				sep = j
				break
			}
		}
		if len(c) < 10 || sep < 0 || len(c) < sep+3 || !strings.Contains(c[2], ":") {
			return nil, fmt.Errorf("backup: riga %d della tabella dei montaggi illeggibile", i+1)
		}
		punto, err := togliEscape(c[4])
		if err != nil {
			return nil, err
		}
		sorgente, err := togliEscape(c[sep+2])
		if err != nil {
			return nil, err
		}
		mm = append(mm, montaggio{majmin: c[2], punto: punto, opzioni: c[5], tipo: c[sep+1], sorgente: sorgente})
	}
	if len(mm) == 0 {
		return nil, errors.New("backup: tabella dei montaggi vuota")
	}
	return mm, nil
}

// togliEscape toglie le sequenze ottali del kernel (\040 lo spazio, \011, \012, \134).
func togliEscape(s string) (string, error) {
	if !strings.Contains(s, `\`) {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		if i+4 > len(s) {
			return "", errors.New("backup: sequenza ottale troncata nella tabella dei montaggi")
		}
		n, err := strconv.ParseUint(s[i+1:i+4], 8, 8)
		if err != nil {
			return "", errors.New("backup: sequenza ottale sbagliata nella tabella dei montaggi")
		}
		b.WriteByte(byte(n))
		i += 3
	}
	return b.String(), nil
}

// montaggioDi è il montaggio che contiene p (percorso vero): il punto più lungo che è p o un suo
// antenato; a parità vince l'ultima riga (il montaggio sopra gli altri).
func montaggioDi(mm []montaggio, p string) (montaggio, bool) {
	var m montaggio
	trovato := false
	for _, x := range mm {
		if x.punto == p || x.punto == "/" || strings.HasPrefix(p, x.punto+"/") {
			if !trovato || len(x.punto) >= len(m.punto) {
				m, trovato = x, true
			}
		}
	}
	return m, trovato
}

// Chiavetta è il risultato dei controlli della chiavetta: la cartella vera (collegamenti risolti), il
// suo dispositivo, e come è montata.
type Chiavetta struct {
	Cartella, Sorgente, TipoFS string
	dev                        uint64
}

// percorsoVero è p con i collegamenti risolti e assoluto.
func percorsoVero(p string) (string, error) {
	v, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	return filepath.Abs(v)
}

// controllaChiavetta dice se cartella è una chiavetta per il backup. Tutti insieme, ognuno un rifiuto:
//   - una cartella che c'è, fuori dalla cartella dei dati (controllaDestinazione), non la radice;
//   - punto di montaggio: dispositivo diverso da quello della cartella sopra (stat) **e** una riga di
//     /proc/self/mountinfo con quel punto di montaggio esatto;
//   - dispositivo diverso da quello della cartella dei dati: diverso st_dev, diverso maggiore:minore e
//     diversa sorgente nella tabella dei montaggi (un bind mount di una cartella dei dati non passa);
//   - sorgente che è un dispositivo a blocchi (/dev/…): niente tmpfs, rete, overlay;
//   - non montata in sola lettura e scrivibile dall'utente del programma.
//
// Lo spazio lo controlla Cifra con la stima dell'archivio (la rotazione viene dopo: se manca spazio il
// backup fallisce e non si cancella niente per farne, risposta D).
func controllaChiavetta(sis sistema, cartella, dati string) (Chiavetta, error) {
	if err := controllaDestinazione(cartella, dati); err != nil {
		return Chiavetta{}, err
	}
	vera, err := percorsoVero(cartella)
	if err != nil {
		return Chiavetta{}, fmt.Errorf("%w: %w", ErrDestinazione, err)
	}
	datiVeri, err := percorsoVero(dati)
	if err != nil {
		return Chiavetta{}, fmt.Errorf("backup: cartella dei dati: %w", err)
	}
	if vera == "/" {
		return Chiavetta{}, fmt.Errorf("%w: la radice", ErrNonMontata)
	}
	idC, err := sis.identita(vera)
	if err != nil {
		return Chiavetta{}, fmt.Errorf("%w: %w", ErrDestinazione, err)
	}
	idS, err := sis.identita(filepath.Dir(vera))
	if err != nil {
		return Chiavetta{}, fmt.Errorf("%w: %w", ErrDestinazione, err)
	}
	idD, err := sis.identita(datiVeri)
	if err != nil {
		return Chiavetta{}, fmt.Errorf("backup: cartella dei dati: %w", err)
	}
	if idC.dev == idS.dev {
		return Chiavetta{}, fmt.Errorf("%w: %s ha lo stesso dispositivo della cartella sopra", ErrNonMontata, vera)
	}
	if idC.dev == idD.dev {
		return Chiavetta{}, fmt.Errorf("%w: %s", ErrStessoDisco, vera)
	}
	b, err := sis.montaggi()
	if err != nil {
		return Chiavetta{}, fmt.Errorf("backup: tabella dei montaggi: %w", err)
	}
	mm, err := leggiMontaggi(b)
	if err != nil {
		return Chiavetta{}, err
	}
	var mc montaggio
	trovato := false
	for _, m := range mm {
		if m.punto == vera {
			mc, trovato = m, true // l'ultima riga è il montaggio sopra gli altri
		}
	}
	if !trovato {
		return Chiavetta{}, fmt.Errorf("%w: %s non è nella tabella dei montaggi", ErrNonMontata, vera)
	}
	md, ok := montaggioDi(mm, datiVeri)
	if !ok {
		return Chiavetta{}, errors.New("backup: il montaggio della cartella dei dati non è nella tabella dei montaggi")
	}
	if mc.majmin == md.majmin || mc.sorgente == md.sorgente {
		return Chiavetta{}, fmt.Errorf("%w: %s (%s) e i dati (%s)", ErrStessoDisco, vera, mc.sorgente, md.sorgente)
	}
	if !strings.HasPrefix(mc.sorgente, "/dev/") {
		return Chiavetta{}, fmt.Errorf("%w: %s è montata da %q (%s)", ErrNonChiavetta, vera, mc.sorgente, mc.tipo)
	}
	for _, o := range strings.Split(mc.opzioni, ",") {
		if o == "ro" {
			return Chiavetta{}, fmt.Errorf("%w: montata in sola lettura", ErrChiavettaSolaLettura)
		}
	}
	if err := sis.scrivibile(vera); err != nil {
		return Chiavetta{}, fmt.Errorf("%w: %w", ErrChiavettaSolaLettura, err)
	}
	return Chiavetta{Cartella: vera, Sorgente: mc.sorgente, TipoFS: mc.tipo, dev: idC.dev}, nil
}

// Destinazione è la cartella dove si scrive un backup, con il suo lucchetto (flock esclusivo della
// cartella, preso senza aspettare): finché è aperta nessun altro backup del Go scrive, pulisce o ruota
// lì dentro. Per la chiavetta porta anche i controlli (Chiavetta).
type Destinazione struct {
	// Cartella è il percorso vero (collegamenti risolti): lì si scrive e quello va nel registro.
	Cartella  string
	chiavetta *Chiavetta
	dati      string
	d         *os.File
	id        identitaFile
}

// BloccaDestinazione apre la cartella di gestrip backup -cartella e ne prende il lucchetto. Non è una
// chiavetta: nel registro va «cartella».
func BloccaDestinazione(cartella, dati string) (*Destinazione, error) {
	if err := controllaDestinazione(cartella, dati); err != nil {
		return nil, err
	}
	vera, err := percorsoVero(cartella)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDestinazione, err)
	}
	return blocca(vera, dati, nil)
}

// ApriChiavetta controlla la chiavetta (controllaChiavetta) e ne prende il lucchetto; poi controlla che la
// cartella bloccata sia quella controllata.
func ApriChiavetta(cartella, dati string) (*Destinazione, error) {
	c, err := controllaChiavetta(sistemaMontaggi, cartella, dati)
	if err != nil {
		return nil, err
	}
	return blocca(c.Cartella, dati, &c)
}

func blocca(vera, dati string, c *Chiavetta) (*Destinazione, error) {
	d, err := bloccaCartella(vera)
	if err != nil {
		return nil, err
	}
	id, err := identitaDi(d)
	if err != nil {
		_ = d.Close() // solo lettura
		return nil, fmt.Errorf("backup: destinazione: %w", err)
	}
	// La cartella aperta e bloccata dev'essere sul dispositivo controllato: una chiavetta smontata fra il
	// controllo e l'apertura lascerebbe aprire la cartella vuota del punto di montaggio, sul disco dei dati
	// (revisione della 123).
	if c != nil {
		if err := suDispositivo(d, c.dev); err != nil {
			_ = d.Close() // solo lettura
			return nil, err
		}
	}
	return &Destinazione{Cartella: vera, chiavetta: c, dati: dati, d: d, id: id}, nil
}

// suDispositivo vuole la cartella aperta d sul dispositivo dev (fstat, dallo strato sistemaMontaggi).
func suDispositivo(d *os.File, dev uint64) error {
	id, err := sistemaMontaggi.identitaAperta(d)
	if err != nil {
		return errors.Join(ErrChiavettaCambiata, err)
	}
	if id.dev != dev {
		return fmt.Errorf("%w: la cartella aperta non è sul dispositivo controllato", ErrChiavettaCambiata)
	}
	return nil
}

// Chiavetta dice se la destinazione è una chiavetta che ha passato i controlli.
func (d *Destinazione) Chiavetta() bool { return d.chiavetta != nil }

// Chiudi rilascia il lucchetto.
func (d *Destinazione) Chiudi() error { return d.d.Close() }

// ConfermaChiavetta, prima di scrivere «chiavetta» nel registro, rifà i controlli: la chiavetta è ancora
// montata allo stesso posto con lo stesso dispositivo, la cartella bloccata è ancora quella, e il file sta
// lì dentro, file normale, sullo stesso dispositivo. Se no ErrChiavettaCambiata (o l'errore del
// controllo): l'archivio non conta come backup sulla chiavetta.
func (d *Destinazione) ConfermaChiavetta(file string) error {
	if d.chiavetta == nil {
		return fmt.Errorf("%w: non è una chiavetta", ErrChiavettaCambiata)
	}
	c, err := controllaChiavetta(sistemaMontaggi, d.chiavetta.Cartella, d.dati)
	if err != nil {
		return errors.Join(ErrChiavettaCambiata, err)
	}
	if c.Cartella != d.chiavetta.Cartella || c.dev != d.chiavetta.dev || c.Sorgente != d.chiavetta.Sorgente {
		return fmt.Errorf("%w: un altro dispositivo montato al suo posto", ErrChiavettaCambiata)
	}
	if err := suDispositivo(d.d, d.chiavetta.dev); err != nil {
		return err
	}
	if filepath.Dir(file) != d.Cartella {
		return fmt.Errorf("%w: l'archivio non sta nella chiavetta", ErrChiavettaCambiata)
	}
	v, err := guardaVoce(d.d, filepath.Base(file))
	if err != nil {
		return errors.Join(ErrChiavettaCambiata, err)
	}
	if !v.normale || v.id.dev != d.id.dev {
		return fmt.Errorf("%w: l'archivio non è un file della chiavetta", ErrChiavettaCambiata)
	}
	return nil
}

// DestinazioneDaRegistrare è la destinazione da scrivere nel registro per l'archivio file:
// db.DestinazioneChiavetta solo se la destinazione è una chiavetta che ha passato i controlli all'inizio e
// li ripassa adesso con l'archivio dentro (ConfermaChiavetta); altrimenti db.DestinazioneCartella, che non
// sblocca niente, con l'errore della conferma per una chiavetta cambiata.
func (d *Destinazione) DestinazioneDaRegistrare(file string) (string, error) {
	if d.chiavetta == nil {
		return db.DestinazioneCartella, nil
	}
	if err := d.ConfermaChiavetta(file); err != nil {
		return db.DestinazioneCartella, err
	}
	return db.DestinazioneChiavetta, nil
}

// reArchivio e rePending sono i soli nomi che la pulizia e la rotazione toccano: quelli di NomeArchivio,
// esatti.
var (
	reArchivio = regexp.MustCompile(`\Agestrip-backup-(\d{8}T\d{6}Z)\.tar\.age\z`)
	rePending  = regexp.MustCompile(`\Agestrip-backup-(\d{8}T\d{6}Z)\.tar\.age\.pending\z`)
)

// istanteDelNome legge l'istante di un nome di archivio (o di un .pending): solo se è esattamente quello
// che NomeArchivio scriverebbe (data vera, niente 25:61, niente 29 febbraio inventato).
func istanteDelNome(re *regexp.Regexp, nome string) (time.Time, bool) {
	m := re.FindStringSubmatch(nome)
	if m == nil {
		return time.Time{}, false
	}
	t, err := time.Parse("20060102T150405Z", m[1])
	if err != nil || t.UTC().Format("20060102T150405Z") != m[1] {
		return time.Time{}, false
	}
	return t, true
}

// PulisciPending toglie dalla destinazione i .pending rimasti da un backup interrotto (blackout, chiavetta
// tolta, processo ucciso): solo i nomi esatti di Cifra (rePending), solo file normali (un collegamento o
// una cartella con quel nome restano), solo in questa cartella e solo con il lucchetto preso (nessun
// backup del Go sta scrivendo qui: chi scrive tiene il lucchetto). Dà i nomi tolti; al primo errore si
// ferma.
func (d *Destinazione) PulisciPending() ([]string, error) {
	nomi, err := nomiDi(d.d)
	if err != nil {
		return nil, err
	}
	var tolti []string
	for _, n := range nomi {
		if _, ok := istanteDelNome(rePending, n); !ok {
			continue
		}
		v, err := guardaVoce(d.d, n)
		if err != nil {
			return tolti, fmt.Errorf("backup: pulizia dei .pending: %w", err)
		}
		if !v.normale || v.id.dev != d.id.dev {
			continue
		}
		if err := togliVoce(d.d, n); err != nil {
			return tolti, fmt.Errorf("backup: pulizia dei .pending: %w", err)
		}
		tolti = append(tolti, n)
	}
	if len(tolti) > 0 {
		if err := d.d.Sync(); err != nil {
			return tolti, fmt.Errorf("backup: pulizia dei .pending, fsync della destinazione: %w", err)
		}
	}
	return tolti, nil
}

// togliVoce è togliNome e guardaVoce guardaNome; variabili per i test (errore di cancellazione, un file
// su un altro dispositivo).
var (
	togliVoce  = togliNome
	guardaVoce = guardaNome
)
