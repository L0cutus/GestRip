package db

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// Il registro dei backup verificati (slice 122, T5.2, D-11, OPS-17): la tabella verifiche_backup
// (migrazione 0021) e la domanda «c'è un backup verificato catturato dopo l'istante T?», che sblocca
// le cancellazioni irreversibili. È qui e non in internal/backup perché la usa BloccatoDopoImport, e
// i pacchetti che lo chiamano (audit, media, buste) non possono dipendere da internal/backup (che
// dipende da media).

// Destinazioni di un archivio registrato.
const (
	// DestinazioneCartella: gestrip backup -cartella DIR.
	DestinazioneCartella = "cartella"
	// DestinazioneChiavetta: la chiavetta USB (slice 123).
	DestinazioneChiavetta = "chiavetta"
	// DestinazioneSFTP: la copia remota (slice 128). Da sola non sblocca niente (risposta F).
	DestinazioneSFTP = "sftp"
)

// ValiditaRicontrollo è per quanto tempo BloccatoDopoImport si fida di uno SHA-256 ricalcolato da
// PreparaBloccoImport, se riga del registro e file sono ancora gli stessi (stesso inode e dispositivo, byte,
// ora di modifica): dopo dice bloccato finché PreparaBloccoImport non lo ricalcola. BackupVerificatoDopo lo
// ricalcola sempre.
const ValiditaRicontrollo = 24 * time.Hour

// AnticipoRicontrollo: PreparaBloccoImport ricalcola quando alla memoria manca meno di questo alla
// scadenza, così BloccatoDopoImport non la trova scaduta fra un giro e l'altro.
const AnticipoRicontrollo = time.Hour

// maxErroreRegistro è quanti byte del motivo di una verifica fallita entrano nel registro.
const maxErroreRegistro = 1000

var (
	// ErrRegistroBackup è una riga che non si può scrivere nel registro.
	ErrRegistroBackup = errors.New("registro dei backup: riga non valida")
	// ErrOrologioBackup è un istante del registro che non torna con l'orologio: una cattura o una verifica
	// nel futuro, una verifica prima della cattura, un istante T nel futuro. È un «no».
	ErrOrologioBackup = errors.New("registro dei backup: istanti che non tornano con l'orologio")
	// ErrArchivioCambiato è l'ultimo archivio verificato che non è più quello registrato: un altro tipo di
	// file, altri byte o un altro SHA-256. È un «no».
	ErrArchivioCambiato = errors.New("registro dei backup: l'ultimo archivio verificato non è più quello registrato")
)

// adessoRegistro è l'orologio del registro, quello del sistema; una variabile per i test.
var adessoRegistro = clock.Sistema{}.Adesso

// VerificaBackup è una riga del registro: la verifica di un archivio, riuscita o no.
type VerificaBackup struct {
	Riuscita     bool
	Destinazione string
	Archivio     string // percorso assoluto del file cifrato
	SHA256       string // 64 cifre esadecimali minuscole
	Byte         int64
	CatturatoIl  time.Time
	VerificatoIl time.Time
	Conteggi     map[string]int64
	Errore       string // il motivo, solo se non riuscita
	// MediaRovinati sono le chiavi dei media con un contenuto che non è il loro nome (slice 125, D-116): non
	// cambiano l'esito. Nel registro vanno il numero e i primi MaxNomiRovinati nomi, in ordine.
	MediaRovinati []string
}

// MaxNomiRovinati è quanti nomi di media rovinati entrano in una riga del registro (il numero c'è sempre
// intero): con un disco guasto potrebbero essere migliaia, alla 126 ne bastano alcuni per dire quali.
const MaxNomiRovinati = 100

// RegistraVerificaBackup scrive una riga nel registro dei backup verificati. Una riga senza i campi
// giusti (percorso non assoluto o non pulito, impronta malformata, motivo mancante in una verifica
// fallita) è ErrRegistroBackup e non si scrive. Il motivo si accorcia a maxErroreRegistro byte.
func RegistraVerificaBackup(ctx context.Context, d *DB, v VerificaBackup) error {
	esito, errore := "ok", sql.NullString{}
	if !v.Riuscita {
		esito = "fallita"
		e := strings.TrimSpace(v.Errore)
		if e == "" {
			return fmt.Errorf("%w: verifica fallita senza motivo", ErrRegistroBackup)
		}
		if len(e) > maxErroreRegistro {
			e = strings.ToValidUTF8(e[:maxErroreRegistro], "")
		}
		errore = sql.NullString{String: e, Valid: true}
	}
	switch {
	case v.Destinazione != DestinazioneCartella && v.Destinazione != DestinazioneChiavetta && v.Destinazione != DestinazioneSFTP:
		return fmt.Errorf("%w: destinazione %q", ErrRegistroBackup, v.Destinazione)
	case !filepath.IsAbs(v.Archivio) || filepath.Clean(v.Archivio) != v.Archivio:
		return fmt.Errorf("%w: percorso dell'archivio non assoluto", ErrRegistroBackup)
	case !impronta(v.SHA256):
		return fmt.Errorf("%w: SHA-256 malformato", ErrRegistroBackup)
	case v.Byte <= 0:
		return fmt.Errorf("%w: byte %d", ErrRegistroBackup, v.Byte)
	case v.CatturatoIl.IsZero() || v.VerificatoIl.IsZero():
		return fmt.Errorf("%w: istanti mancanti", ErrRegistroBackup)
	case v.Riuscita && len(v.Conteggi) == 0:
		return fmt.Errorf("%w: verifica riuscita senza conteggi", ErrRegistroBackup)
	}
	rovinati := slices.Sorted(slices.Values(v.MediaRovinati))
	for _, k := range rovinati {
		if !impronta(k) {
			return fmt.Errorf("%w: nome di un media rovinato malformato", ErrRegistroBackup)
		}
	}
	nomi, err := json.Marshal(append([]string{}, rovinati[:min(len(rovinati), MaxNomiRovinati)]...))
	if err != nil {
		return err
	}
	conteggi := v.Conteggi
	if conteggi == nil {
		conteggi = map[string]int64{}
	}
	j, err := json.Marshal(conteggi)
	if err != nil {
		return err
	}
	_, err = d.Scrittura.ExecContext(ctx, `INSERT INTO verifiche_backup
		(esito, destinazione, archivio, sha256, byte, catturato_il, verificato_il, conteggi, errore,
		 media_rovinati, media_rovinati_nomi)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		esito, v.Destinazione, v.Archivio, v.SHA256, v.Byte,
		v.CatturatoIl.UTC().Format(FormatoIstante), v.VerificatoIl.UTC().Format(FormatoIstante), string(j), errore,
		len(rovinati), string(nomi))
	if err != nil {
		return fmt.Errorf("registro dei backup: %w", err)
	}
	return nil
}

func impronta(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range []byte(s) {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ArchivioRegistrato è una riga del registro come la vede la rotazione della chiavetta (slice 123).
type ArchivioRegistrato struct {
	ID           int64
	Riuscita     bool
	Destinazione string
	Archivio     string
	SHA256       string
	Byte         int64
	// CatturatoIl lo riempie solo UltimoVerificatoSullaChiavetta (la prova settimanale ne guarda l'età, 125).
	CatturatoIl time.Time
}

// Elencatore è quello che serve ad ArchiviNellaCartella: un *sql.DB o una transazione.
type Elencatore interface {
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
}

// ArchiviNellaCartella dà le righe del registro (di ogni esito, destinazione cartella o chiavetta) il cui
// archivio sta direttamente nella cartella (percorso assoluto e pulito): la rotazione (slice 123) cancella solo
// archivi che il registro conosce. Le righe «sftp» (128) restano fuori: sono percorsi del server.
func ArchiviNellaCartella(ctx context.Context, q Elencatore, cartella string) ([]ArchivioRegistrato, error) {
	if !filepath.IsAbs(cartella) || filepath.Clean(cartella) != cartella {
		return nil, fmt.Errorf("%w: cartella non assoluta", ErrRegistroBackup)
	}
	rr, err := q.QueryContext(ctx, `SELECT id, esito, destinazione, archivio, sha256, byte FROM verifiche_backup ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("registro dei backup: %w", err)
	}
	defer func() { _ = rr.Close() }()
	var v []ArchivioRegistrato
	for rr.Next() {
		var a ArchivioRegistrato
		var esito string
		if err := rr.Scan(&a.ID, &esito, &a.Destinazione, &a.Archivio, &a.SHA256, &a.Byte); err != nil {
			return nil, fmt.Errorf("registro dei backup: %w", err)
		}
		a.Riuscita = esito == "ok"
		// Le copie remote (128) hanno un percorso del server: anche se per caso è uguale a quello della chiavetta,
		// non dicono niente dei file della chiavetta (ArchiviRemotiNellaCartella è la loro).
		if a.Destinazione != DestinazioneSFTP && filepath.Dir(a.Archivio) == cartella {
			v = append(v, a)
		}
	}
	if err := rr.Err(); err != nil {
		return nil, fmt.Errorf("registro dei backup: %w", err)
	}
	return v, nil
}

// UltimoVerificatoSullaChiavetta dà la riga che BackupVerificatoDopo e BloccatoDopoImport guardano:
// l'ultima verifica 'ok' sulla chiavetta per ora di cattura. Senza righe: false.
func UltimoVerificatoSullaChiavetta(ctx context.Context, q Interrogatore) (ArchivioRegistrato, bool, error) {
	a := ArchivioRegistrato{Riuscita: true, Destinazione: DestinazioneChiavetta}
	var catturato string
	err := q.QueryRowContext(ctx, `SELECT id, archivio, sha256, byte, catturato_il FROM verifiche_backup
		WHERE esito = 'ok' AND destinazione = ? ORDER BY catturato_il DESC, id DESC LIMIT 1`,
		DestinazioneChiavetta).Scan(&a.ID, &a.Archivio, &a.SHA256, &a.Byte, &catturato)
	if errors.Is(err, sql.ErrNoRows) {
		return ArchivioRegistrato{}, false, nil
	}
	if err != nil {
		return ArchivioRegistrato{}, false, fmt.Errorf("registro dei backup: %w", err)
	}
	if a.CatturatoIl, err = time.Parse(FormatoIstante, catturato); err != nil {
		return ArchivioRegistrato{}, false, fmt.Errorf("registro dei backup: ora di cattura: %w", err)
	}
	return a, true, nil
}

// MediaRovinati sono i media rovinati dell'ultimo backup verificato sulla chiavetta (la riga di
// UltimoVerificatoSullaChiavetta; slice 125, D-116): quanti e i primi MaxNomiRovinati nomi. È lo stato che la 126
// mostra nel banner e negli avvisi finché un backup nuovo non li trova più. Senza backup verificati: 0.
func MediaRovinati(ctx context.Context, q Interrogatore) (n int, nomi []string, err error) {
	var j string
	err = q.QueryRowContext(ctx, `SELECT media_rovinati, media_rovinati_nomi FROM verifiche_backup
		WHERE esito = 'ok' AND destinazione = ? ORDER BY catturato_il DESC, id DESC LIMIT 1`,
		DestinazioneChiavetta).Scan(&n, &j)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil, nil
	}
	if err != nil {
		return 0, nil, fmt.Errorf("registro dei backup: %w", err)
	}
	if err := json.Unmarshal([]byte(j), &nomi); err != nil {
		return 0, nil, fmt.Errorf("registro dei backup: media rovinati: %w", err)
	}
	return n, nomi, nil
}

// BackupVerificatoDopo dice se c'è un backup verificato catturato dopo l'istante t (D-11, risposta F del
// piano della T5.2): fallisce chiusa, quindi solo (true, nil) vuol dire sì. Ricalcola sempre lo SHA-256
// dell'archivio (come verify_after dello standalone), quindi può leggere qualche GB: va chiamata fuori da
// ogni transazione e da ogni lucchetto (il cestino definitivo, T4.11). Le regole sono quelle di
// backupVerificatoDopo.
func BackupVerificatoDopo(ctx context.Context, q Interrogatore, t time.Time) (bool, error) {
	return backupVerificatoDopo(ctx, q, t, nil, ricalcolaSempre)
}

// modoRicontrollo dice se e quando ricontrolla rilegge l'archivio.
type modoRicontrollo int

const (
	// ricalcolaSempre: rilegge sempre tutto l'archivio, senza memoria (BackupVerificatoDopo).
	ricalcolaSempre modoRicontrollo = iota
	// riempiMemoria: rilegge l'archivio solo se la memoria non è fresca per il ricalcolo anticipato, un
	// ricalcolo alla volta (PreparaBloccoImport, fuori da transazioni e lucchetti).
	riempiMemoria
	// soloMemoria: non rilegge mai; sì solo con la memoria fresca, altrimenti «no» (BloccatoDopoImport,
	// dentro la transazione o il lucchetto dei chiamanti).
	soloMemoria
)

// backupVerificatoDopo è BackupVerificatoDopo con la memoria m e il modo del ricontrollo. Le regole:
//   - conta solo l'ultima verifica 'ok' (per ora di cattura) sulla chiavetta (risposta F: né la cartella
//     di gestrip backup -cartella né la copia SFTP sbloccano); se è catturata prima di t o nello stesso
//     istante, è «no» anche se ce ne fossero altre più vecchie;
//   - un istante del registro nel futuro (qualunque riga, anche fallita), una verifica prima della sua
//     cattura o un t zero sono ErrOrologioBackup: «no»; un t nel futuro è «no» senza errore (nessuna
//     cattura buona può essere dopo);
//   - l'archivio di quella riga deve esserci ancora, al suo percorso, file normale (non un collegamento),
//     con gli stessi byte e lo stesso SHA-256 (ricontrolla). Un archivio che manca è «no» senza errore (la
//     chiavetta tolta); un altro tipo di file, altri byte o un'altra impronta sono ErrArchivioCambiato;
//     ogni altro errore di lettura (anche ctx finito) è un errore: sempre «no».
func backupVerificatoDopo(ctx context.Context, q Interrogatore, t time.Time, m *memoriaRicontrollo, modo modoRicontrollo) (bool, error) {
	adesso := adessoRegistro()
	if t.IsZero() {
		return false, fmt.Errorf("%w: istante zero", ErrOrologioBackup)
	}
	if t.After(adesso) {
		// Nessun backup buono può essere catturato dopo un istante nel futuro (una cattura nel futuro è un
		// errore qui sotto): «no», senza errore, come prima dell'import con un orologio indietro.
		return false, nil
	}
	// Prima la guardia sull'orologio, su tutte le righe: con una cattura o una verifica nel futuro non ci si
	// fida di nessun confronto fra istanti.
	var maxCatturato, maxVerificato sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT max(catturato_il), max(verificato_il) FROM verifiche_backup`).
		Scan(&maxCatturato, &maxVerificato); err != nil {
		return false, fmt.Errorf("registro dei backup: %w", err)
	}
	if !maxCatturato.Valid {
		return false, nil
	}
	for _, s := range []string{maxCatturato.String, maxVerificato.String} {
		i, err := time.Parse(FormatoIstante, s)
		if err != nil {
			return false, fmt.Errorf("registro dei backup: istante illeggibile: %w", err)
		}
		if i.After(adesso) {
			return false, fmt.Errorf("%w: %s nel futuro", ErrOrologioBackup, s)
		}
	}
	var r rigaVerifica
	var catturato, verificato string
	err := q.QueryRowContext(ctx, `SELECT id, archivio, sha256, byte, catturato_il, verificato_il FROM verifiche_backup
		WHERE esito = 'ok' AND destinazione = ? ORDER BY catturato_il DESC, id DESC LIMIT 1`,
		DestinazioneChiavetta).Scan(&r.id, &r.archivio, &r.sha256, &r.byte, &catturato, &verificato)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("registro dei backup: %w", err)
	}
	c, errC := time.Parse(FormatoIstante, catturato)
	v, errV := time.Parse(FormatoIstante, verificato)
	if err := errors.Join(errC, errV); err != nil {
		return false, fmt.Errorf("registro dei backup: istante illeggibile: %w", err)
	}
	if v.Before(c) {
		return false, fmt.Errorf("%w: verifica %s prima della cattura %s", ErrOrologioBackup, verificato, catturato)
	}
	if !c.After(t) {
		return false, nil
	}
	return ricontrolla(ctx, r, adesso, m, modo)
}

// rigaVerifica è quello che serve del registro per ricontrollare un archivio.
type rigaVerifica struct {
	id       int64
	archivio string
	sha256   string
	byte     int64
}

// ricontrolla vuole l'archivio della riga r ancora al suo posto, file normale, con gli stessi byte e lo
// stesso SHA-256. I controlli leggeri (Lstat, tipo, byte) si fanno sempre; il ricalcolo dello SHA-256 dipende
// da modo (vedi modoRicontrollo). La memoria m vale per la stessa riga, sullo stesso file non cambiato
// (stesso inode e dispositivo, byte, ora di modifica) e per un tempo che dipende dal modo.
func ricontrolla(ctx context.Context, r rigaVerifica, adesso time.Time, m *memoriaRicontrollo, modo modoRicontrollo) (bool, error) {
	if !filepath.IsAbs(r.archivio) {
		return false, fmt.Errorf("%w: percorso registrato non assoluto", ErrArchivioCambiato)
	}
	fi, err := os.Lstat(r.archivio)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("registro dei backup: archivio: %w", err)
	}
	if !fi.Mode().IsRegular() || fi.Size() != r.byte {
		return false, fmt.Errorf("%w: tipo o byte diversi", ErrArchivioCambiato)
	}
	switch modo {
	case soloMemoria:
		// Dentro una transazione o un lucchetto: mai rileggere qualche GB. Memoria non fresca → «no».
		return m.vale(r, fi, adesso, ValiditaRicontrollo), nil
	case riempiMemoria:
		// Un ricalcolo alla volta: chi aspetta trova la memoria riempita da chi ha finito e non rilegge.
		m.calcolo.Lock()
		defer m.calcolo.Unlock()
		// L'ora si rilegge dopo l'attesa: quella letta prima è più vecchia della memoria che chi ha finito ha
		// appena riempito, e farebbe ricalcolare una seconda volta (seconda revisione della 122).
		adesso = adessoRegistro()
		if m.vale(r, fi, adesso, ValiditaRicontrollo-AnticipoRicontrollo) {
			return true, nil
		}
		// Da qui la memoria vale solo se il ricalcolo riesce di nuovo.
		m.dimentica()
		// Nota 8c: lo stesso archivio, sullo stesso file non cambiato, non è passato poco fa: non si
		// rilegge fino alla fine dell'attesa, e intanto è «no» con lo stesso errore.
		if err := m.rimandato(r, fi, adesso); err != nil {
			return false, err
		}
	}
	if err := ricalcola(ctx, r, fi); err != nil {
		if modo == riempiMemoria && ctx.Err() == nil {
			m.ricordaFallimento(r, fi, adesso, err)
		}
		return false, err
	}
	if modo == riempiMemoria {
		m.ricorda(r, fi, adesso)
	}
	return true, nil
}

// AttesaDopoFallimento è quanto PreparaBloccoImport aspetta prima di rileggere un archivio che non è passato
// (nota 8c della 122): raddoppia a ogni fallimento della stessa riga sullo stesso file, fino a
// MaxAttesaDopoFallimento. Un'altra riga del registro o un file cambiato (inode, byte, ora di modifica) si
// rileggono subito.
const (
	AttesaDopoFallimento    = 10 * time.Minute
	MaxAttesaDopoFallimento = 6 * time.Hour
)

// ricalcola rilegge tutto l'archivio di r e vuole gli stessi byte e lo stesso SHA-256; il file aperto deve
// essere quello visto con Lstat (fi).
func ricalcola(ctx context.Context, r rigaVerifica, fi os.FileInfo) error {
	ricalcoli.Add(1)
	if duranteRicalcolo != nil {
		duranteRicalcolo()
	}
	f, err := os.Open(r.archivio)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: archivio sparito mentre si apriva", ErrArchivioCambiato)
		}
		return fmt.Errorf("registro dei backup: archivio: %w", err)
	}
	defer func() { _ = f.Close() }() // solo lettura
	// Un collegamento o un altro file messo al suo posto nel mezzo no.
	aperto, err := f.Stat()
	if err != nil {
		return fmt.Errorf("registro dei backup: archivio: %w", err)
	}
	if !os.SameFile(fi, aperto) {
		return fmt.Errorf("%w: il file è cambiato mentre si apriva", ErrArchivioCambiato)
	}
	h := sha256.New()
	n, err := io.Copy(h, lettoreDelContesto{ctx: ctx, r: f})
	if err != nil {
		return fmt.Errorf("registro dei backup: lettura dell'archivio: %w", err)
	}
	if n != r.byte || subtle.ConstantTimeCompare([]byte(hex.EncodeToString(h.Sum(nil))), []byte(r.sha256)) != 1 {
		return fmt.Errorf("%w: SHA-256 o byte diversi", ErrArchivioCambiato)
	}
	return nil
}

// ricalcoli conta le letture complete di un archivio (per i test e la diagnosi).
var ricalcoli atomic.Int64

// duranteRicalcolo, solo nei test, gira all'inizio di ogni ricalcolo.
var duranteRicalcolo func()

// lettoreDelContesto smette di leggere quando ctx finisce: un archivio di qualche GB su una chiavetta
// lenta non deve tenere fermo lo stop di serve.
type lettoreDelContesto struct {
	ctx context.Context
	r   io.Reader
}

func (l lettoreDelContesto) Read(p []byte) (int, error) {
	if err := l.ctx.Err(); err != nil {
		return 0, err
	}
	return l.r.Read(p)
}

// memoriaRicontrollo ricorda l'ultimo ricalcolo riuscito dello SHA-256 di un archivio (uno per processo).
// La riempie PreparaBloccoImport, fuori da transazioni e lucchetti; la legge BloccatoDopoImport, che la
// pulizia dei media chiama ogni 60 secondi e la conservazione dell'audit a ogni lotto.
type memoriaRicontrollo struct {
	// calcolo tiene un solo ricalcolo alla volta (riempiMemoria).
	calcolo sync.Mutex
	mu      sync.Mutex
	riga    rigaVerifica
	fi      os.FileInfo
	il      time.Time
	// L'ultimo ricalcolo non riuscito (nota 8c): riga, file, quando, quanto aspettare, l'errore.
	fallito fallimento
}

// fallimento è un ricalcolo non riuscito di PreparaBloccoImport.
type fallimento struct {
	riga   rigaVerifica
	fi     os.FileInfo
	il     time.Time
	attesa time.Duration
	err    error
}

// stessoFile dice se a e b sono lo stesso file non cambiato: inode e dispositivo, byte, ora di modifica.
func stessoFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// rimandato dà l'errore dell'ultimo ricalcolo se è della stessa riga, sullo stesso file non cambiato, e
// l'attesa non è finita (un orologio tornato indietro la fa finire: si rilegge).
func (m *memoriaRicontrollo) rimandato(r rigaVerifica, fi os.FileInfo, adesso time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.fallito
	if f.err == nil || f.riga != r || !stessoFile(f.fi, fi) {
		return nil
	}
	if eta := adesso.Sub(f.il); eta < 0 || eta >= f.attesa {
		return nil
	}
	return fmt.Errorf("%w (non riletto fino alle %s)", f.err, f.il.Add(f.attesa).UTC().Format(time.RFC3339))
}

// ricordaFallimento ricorda un ricalcolo non riuscito; per la stessa riga e lo stesso file l'attesa
// raddoppia, fino a MaxAttesaDopoFallimento.
func (m *memoriaRicontrollo) ricordaFallimento(r rigaVerifica, fi os.FileInfo, adesso time.Time, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	attesa := AttesaDopoFallimento
	if p := m.fallito; p.err != nil && p.riga == r && stessoFile(p.fi, fi) {
		attesa = min(p.attesa*2, MaxAttesaDopoFallimento)
	}
	m.fallito = fallimento{riga: r, fi: fi, il: adesso, attesa: attesa, err: err}
}

// vale dice se la memoria è della riga r, sullo stesso file fi non cambiato, e ha meno di validita.
func (m *memoriaRicontrollo) vale(r rigaVerifica, fi os.FileInfo, adesso time.Time, validita time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fi == nil || m.riga != r {
		return false
	}
	eta := adesso.Sub(m.il)
	return eta >= 0 && eta < validita && stessoFile(m.fi, fi)
}

// ricorda riempie la memoria con un ricalcolo riuscito e dimentica il fallimento.
func (m *memoriaRicontrollo) ricorda(r rigaVerifica, fi os.FileInfo, adesso time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.riga, m.fi, m.il = r, fi, adesso
	m.fallito = fallimento{}
}

func (m *memoriaRicontrollo) dimentica() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.riga, m.fi, m.il = rigaVerifica{}, nil, time.Time{}
}

// azzera dimentica tutto, anche il fallimento (test).
func (m *memoriaRicontrollo) azzera() {
	m.dimentica()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fallito = fallimento{}
}

// memoriaImport è la memoria del ricontrollo di PreparaBloccoImport e BloccatoDopoImport.
var memoriaImport memoriaRicontrollo
