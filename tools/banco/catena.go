package main

// La catena dei dati sintetici (PIANO §4.2, T3): una cattura Flask sintetica, il legacy import
// dello standalone isolato, il suo database copiato e decifrato con il SQLCipher della sua stessa
// immagine, e `gestrip import -sintetico` nel Go. Così le due istanze del banco partono dagli
// stessi dati, e il banco prova l'import a ogni giro. Si sceglie con `banco su -dati
// sintetici-import`; senza, il banco semina solo i due account come prima.
//
// Ogni passo gira in un container `--rm` senza rete (`--network none`, più stretto della rete
// interna: nessuno di questi passi parla con un altro container), con utente 1000, sola lettura e
// le protezioni degli altri container del banco, e monta solo cartelle dell'istanza.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/L0cutus/gestrip/internal/importer"
)

// datiSinteticiImport è il valore di `banco su -dati` che accende la catena.
const datiSinteticiImport = "sintetici-import"

const (
	// scriptCattura è il generatore della cattura sintetica dello standalone, letto dal tag a ogni
	// giro (mai dal worktree) e scritto nella cartella dell'istanza.
	scriptCattura = "test/fixtures/legacy_import/synthetic_capture.py"
	nomeChiaro    = "standalone.db"
	nomeReportGo  = "import-go.json"
)

// modalitaDati legge `-dati`: vuoto è il banco di prima (solo i due account seminati), l'unico
// altro valore ammesso è la catena.
func modalitaDati(dati string) (bool, error) {
	switch dati {
	case "":
		return false, nil
	case datiSinteticiImport:
		return true, nil
	}
	return false, fmt.Errorf("-dati %q non valido: vuoto oppure %s", dati, datiSinteticiImport)
}

// percorsiCatena sono i file e le cartelle della catena, tutti dentro la cartella dell'istanza.
type percorsiCatena struct {
	dir              string // catena/
	script           string // il synthetic_capture.py del tag
	flask            string // la cattura sintetica (riparazioni.db, foto, firme, pdf, logs…)
	chiaveArchivio   string // la chiave di prova dell'archivio del legacy import
	foto             string // la foto sintetica della vendita
	uscitaStandalone string // report, archivio e copia della sorgente del legacy import
	cifrato          string // la copia del database dello standalone, ancora cifrato
	chiaro           string // la cartella del database decifrato (nomeChiaro)
	media            string // la copia della cartella media dello standalone
	miniature        string // la copia delle miniature, se lo standalone ne ha
}

func catenaPer(n nomi) percorsiCatena {
	d := filepath.Join(n.dir, "catena")
	return percorsiCatena{
		dir:              d,
		script:           filepath.Join(d, "script", "synthetic_capture.py"),
		flask:            filepath.Join(d, "cattura", "flask"),
		chiaveArchivio:   filepath.Join(d, "chiave-archivio"),
		foto:             filepath.Join(d, "vendita", "foto.jpg"),
		uscitaStandalone: filepath.Join(d, "import-standalone"),
		cifrato:          filepath.Join(d, "cifrato"),
		chiaro:           filepath.Join(d, "chiaro"),
		media:            filepath.Join(d, "media"),
		miniature:        filepath.Join(d, "miniature"),
	}
}

// dentroIstanza rifiuta un percorso che non sta sotto la cartella dell'istanza (la cartella
// stessa compresa) o che finisce in un percorso di produzione.
func dentroIstanza(n nomi, p string) error {
	if !filepath.IsAbs(p) || filepath.Clean(p) != p || !strings.HasPrefix(p, n.dir+string(filepath.Separator)) {
		return fmt.Errorf("percorso fuori dall'istanza: %s", p)
	}
	return percorsoAmmesso(p)
}

func monta(src, dst string, solaLettura bool) []string {
	m := "type=bind,src=" + src + ",dst=" + dst
	if solaLettura {
		m += ",readonly"
	}
	return []string{"--mount", m}
}

// protezioniStandalone sono le opzioni di ogni container dell'immagine dello standalone, senza
// rete né montaggi.
func protezioniStandalone(n nomi) []string {
	return append([]string{
		"--init", "--user", "1000:1000", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges:true", "--read-only",
		"--security-opt", "seccomp=" + filepath.Join(n.dir, "chromium-seccomp.json"),
		"--shm-size", "256m",
		"--tmpfs", "/tmp:uid=1000,gid=1000,mode=1777,size=256m",
		"--tmpfs", "/home/app:uid=1000,gid=1000,mode=0700,size=32m",
		"--log-opt", "max-size=10m", "--log-opt", "max-file=2",
	}, limitiContainer...)
}

// montaggiStandalone sono i dati e i segreti dell'istanza dello standalone.
func montaggiStandalone(n nomi) []string {
	m := monta(filepath.Join(n.dir, "data"), "/data", false)
	m = append(m, monta(filepath.Join(n.dir, "media"), "/data/media", false)...)
	return append(m, monta(filepath.Join(n.dir, "secrets"), "/run/secrets", true)...)
}

// I passi della catena: ognuno è un container grgo-<nome>-<passo>, e giu li toglie tutti.
const (
	passoCattura   = "cattura"
	passoImportSA  = "importsa"
	passoVendita   = "vendita"
	passoEmergenza = "emergenza"
	passoDecifra   = "decifra"
	passoImportGo  = "importgo"
)

// suffissiContainer sono tutti i container che un'istanza può avere, nell'ordine in cui giu li
// toglie: prima quelli che usano gli altri.
func suffissiContainer() []string {
	return []string{"-banco", "-attendi", "-go", "-caddy", "-app",
		"-" + passoImportGo, "-" + passoDecifra, "-" + passoEmergenza, "-" + passoVendita, "-" + passoImportSA, "-" + passoCattura, "-dipendente", "-boot"}
}

func passoSenzaRete(n nomi, nome string) []string {
	return []string{"run", "--rm", "--name", n.base + "-" + nome, "--network", "none", "--label", "grgo=1"}
}

// comandoCattura genera la cattura sintetica con il Python dell'immagine dello standalone: niente
// Python dell'host (versione e moduli che cambiano da una macchina all'altra), nessuna rete.
func comandoCattura(n nomi, img string, p percorsiCatena) []string {
	a := append(passoSenzaRete(n, passoCattura), protezioniStandalone(n)...)
	a = append(a, monta(p.script, "/cattura/synthetic_capture.py", true)...)
	a = append(a, monta(filepath.Dir(p.flask), "/out", false)...)
	return append(a, img, "python3", "-I", "/cattura/synthetic_capture.py", "/out/"+filepath.Base(p.flask))
}

// comandoImportStandalone è il legacy import come lo lancia `packaging.py import` (overlay
// deploy/compose.import.yaml a go-baseline): Release.main/0 con la cattura in sola lettura, una
// uscita privata e vuota e la chiave dell'archivio, in modalità sintetica, sui dati dell'istanza.
func comandoImportStandalone(n nomi, img string, env []string, p percorsiCatena) []string {
	a := append(passoSenzaRete(n, passoImportSA), protezioniStandalone(n)...)
	a = append(a, montaggiStandalone(n)...)
	a = append(a, monta(p.flask, "/import/capture", true)...)
	a = append(a, monta(p.uscitaStandalone, "/import/output", false)...)
	a = append(a, monta(p.chiaveArchivio, "/run/import/archive_key", true)...)
	a = append(a, env...)
	a = append(a, "--env", "GESTRIP_IMPORT_MODE=synthetic", "--env", "GESTRIP_IMPORT_AUTHORIZATION_REF=")
	return append(a, img, "bin/gestrip_standalone", "eval", "GestripStandalone.LegacyImport.Release.main()")
}

// decifraStandalone apre la copia cifrata con la chiave di prova dell'istanza, come fa
// GestripStandalone.Maintenance (Database.load_key!/1, verify_library!/1, Exqlite.Connection con
// :key), e la esporta in chiaro con sqlcipher_export in un database allegato senza chiave. Non
// avvia l'applicazione: niente migrazioni, niente audit, niente Repo.
const decifraStandalone = `Logger.configure(level: :error)
Application.load(:gestrip_standalone)
{:ok, _} = Application.ensure_all_started(:ecto_sqlite3)
alias GestripStandalone.Database
alias Exqlite.Basic
key = Database.load_key!("/run/secrets/sqlcipher.key")
{:ok, _} = Database.verify_library!(key)
{:ok, conn} = Exqlite.Connection.connect(database: "/catena/cifrato/gestrip.db", key: key)
esegui = fn sql -> {:ok, _rows, _columns} = conn |> Basic.exec(sql) |> Basic.rows() end
esegui.("ATTACH DATABASE '/catena/chiaro/` + nomeChiaro + `' AS chiaro KEY ''")
esegui.("SELECT sqlcipher_export('chiaro')")
{:ok, [[versione]], _columns} = conn |> Basic.exec("PRAGMA main.user_version") |> Basic.rows()
esegui.("PRAGMA chiaro.user_version = #{versione}")
esegui.("DETACH DATABASE chiaro")
_ = Basic.close(conn)
IO.puts("GR_DECIFRATO")`

// comandoDecifra gira nella stessa immagine dello standalone, che ha SQLCipher (l'host no), senza
// rete e senza i dati dell'istanza: solo la copia cifrata, la cartella del chiaro e due segreti.
// L'ambiente è solo quello che config/runtime.exs pretende in prod a go-baseline: DATABASE_PATH
// (qui la copia) e SECRET_KEY_BASE_FILE, più SQLCIPHER_KEY_FILE, che il Dockerfile fissa a
// /run/secrets/sqlcipher_key e runtime.exs controlla se c'è (va quindi spostato sul file
// dell'istanza), e l'ambiente degli script della release. Niente credenziali degli account,
// niente MEDIA_ROOT; il server non parte perché l'applicazione non si avvia.
func comandoDecifra(n nomi, img string, p percorsiCatena) []string {
	a := append(passoSenzaRete(n, passoDecifra), protezioniStandalone(n)...)
	a = append(a, monta(p.cifrato, "/catena/cifrato", false)...)
	a = append(a, monta(p.chiaro, "/catena/chiaro", false)...)
	for _, k := range []string{"sqlcipher.key", "secret_key_base"} {
		a = append(a, monta(filepath.Join(n.dir, "secrets", k), "/run/secrets/"+k, true)...)
	}
	a = append(a, "--env", "DATABASE_PATH=/catena/cifrato/gestrip.db",
		"--env", "SECRET_KEY_BASE_FILE=/run/secrets/secret_key_base", "--env", "SQLCIPHER_KEY_FILE=/run/secrets/sqlcipher.key",
		"--env", "RELEASE_TMP=/tmp", "--env", "RELEASE_DISTRIBUTION=none", "--env", "ERL_CRASH_DUMP=/dev/null")
	return append(a, img, "bin/gestrip_standalone", "eval", decifraStandalone)
}

// comandoImportGo è `gestrip import -sintetico` sul database decifrato e sulla copia dei media,
// verso il database del Go dell'istanza. Il volume del banco non è cifrato (pc-fisso), come per
// serve: -senza-volume-cifrato.
func comandoImportGo(n nomi, gestrip string, p percorsiCatena, sha string, dimensione int64, conMiniature bool) []string {
	a := append(passoSenzaRete(n, passoImportGo),
		"--init", "--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
		"--read-only", "--tmpfs", "/tmp:uid=1000,gid=1000,mode=1777,size=64m",
		"--log-opt", "max-size=10m", "--log-opt", "max-file=2")
	a = append(a, limitiContainer...)
	a = append(a, monta(gestrip, "/gestrip", true)...)
	a = append(a, monta(p.chiaro, "/sorgente", true)...)
	a = append(a, monta(p.media, "/media", true)...)
	if conMiniature {
		a = append(a, monta(p.miniature, "/miniature", true)...)
	}
	a = append(a, monta(filepath.Join(n.dir, "go-data"), "/data", false)...)
	a = append(a, monta(filepath.Join(n.dir, "esito"), "/esito", false)...)
	a = append(a, immagineBase, "/gestrip", "import", "-sintetico",
		"-sorgente", "/sorgente/"+nomeChiaro, "-sha256", sha, "-dimensione", strconv.FormatInt(dimensione, 10),
		"-destinazione", "/data/gestrip.db", "-report", "/esito/"+nomeReportGo, "-media", "/media")
	if conMiniature {
		a = append(a, "-miniature", "/miniature")
	}
	return append(a, "-senza-volume-cifrato")
}

// esitoImportStandalone legge l'unica riga GR_IMPORT: di Release.main/0: verdetto reconciled in
// modalità synthetic, altrimenti la catena si ferma.
func esitoImportStandalone(out string) error {
	righe := righeMarcatore(out, "GR_IMPORT:")
	if len(righe) != 1 {
		return fmt.Errorf("legacy import dello standalone: %d righe GR_IMPORT, ne serve una", len(righe))
	}
	var r struct{ Verdict, Mode string }
	if err := json.Unmarshal([]byte(righe[0]), &r); err != nil {
		return fmt.Errorf("legacy import dello standalone: riga GR_IMPORT non valida: %w", err)
	}
	if r.Verdict != "reconciled" || r.Mode != "synthetic" {
		return fmt.Errorf("legacy import dello standalone: verdetto %q, modalità %q", r.Verdict, r.Mode)
	}
	return nil
}

// riassuntoImport sono i numeri del report dell'import Go, per il registro e runtime.json.
// ImpronteTesto sono le righe degli avvisi tipo/impronta_testo per tabella; IstantiSenzaZona le
// righe degli avvisi traduzione/istante_senza_zona per tabella.colonna; MediaVendita i file che la vendita ha aggiunto ai
// media dello standalone; Emergenza ciò che ha scritto il passo del registro d'emergenza.
type riassuntoImport struct {
	Verdetto         string             `json:"verdetto"`
	Lette            map[string]int64   `json:"lette"`
	Scritte          map[string]int64   `json:"scritte"`
	Avvisi           int                `json:"avvisi"`
	ImpronteTesto    map[string]int64   `json:"impronte_testo"`
	IstantiSenzaZona map[string]int64   `json:"istanti_senza_zona"`
	FileMedia        int64              `json:"file_media"`
	ByteMedia        int64              `json:"byte_media"`
	MediaVendita     int64              `json:"media_vendita"`
	Emergenza        riassuntoEmergenza `json:"registro_emergenza"`
}

// leggiReportImport accetta solo un report riconciliato, sintetico, della sorgente data (impronta
// e dimensione), senza differenze e con i media importati: tutti e soli i file e i byte che la
// catena ha copiato dallo standalone, e la vendita del passo della vendita e il registro del passo
// del registro d'emergenza con i loro avvisi (controllaVendita). Un report bloccato dà i codici delle differenze.
func leggiReportImport(b []byte, att sorgenteGo) (riassuntoImport, error) {
	var r importer.Report
	dec := json.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&r); err != nil {
		return riassuntoImport{}, fmt.Errorf("report dell'import Go non valido: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return riassuntoImport{}, errors.New("report dell'import Go: dati dopo il JSON")
	}
	if len(r.Differenze) > 0 || r.Verdetto != importer.Riconciliato {
		var codici []string
		for _, d := range r.Differenze {
			c := d.Codice
			if d.Tabella != "" {
				c += " " + d.Tabella
			}
			if d.Colonna != "" {
				c += "." + d.Colonna
			}
			if d.Id != nil {
				c += " id " + strconv.FormatInt(*d.Id, 10)
			}
			codici = append(codici, c)
		}
		return riassuntoImport{}, fmt.Errorf("import Go %s, %d differenze: %s", r.Verdetto, len(r.Differenze), strings.Join(codici, "; "))
	}
	switch {
	case r.Formato != importer.FormatoReport:
		return riassuntoImport{}, fmt.Errorf("report dell'import Go: formato %d", r.Formato)
	case r.Modalita != importer.Sintetico:
		return riassuntoImport{}, fmt.Errorf("report dell'import Go: modalità %q", r.Modalita)
	case r.Sorgente.Sha256 != att.sha:
		return riassuntoImport{}, errors.New("report dell'import Go: impronta della sorgente diversa")
	case r.Sorgente.Byte != att.dimensione:
		return riassuntoImport{}, fmt.Errorf("report dell'import Go: sorgente di %d byte, attesi %d", r.Sorgente.Byte, att.dimensione)
	case r.Media == nil || !r.Media.Importati:
		return riassuntoImport{}, errors.New("report dell'import Go: media non importati")
	case r.Media.FileCopiati != att.fileMedia || r.Media.ByteCopiati != att.byteMedia || r.Media.InPiu != 0:
		return riassuntoImport{}, fmt.Errorf("report dell'import Go: media %d file %d byte (%d in più), copiati dallo standalone %d file %d byte",
			r.Media.FileCopiati, r.Media.ByteCopiati, r.Media.InPiu, att.fileMedia, att.byteMedia)
	case len(r.Tabelle) == 0:
		return riassuntoImport{}, errors.New("report dell'import Go: nessuna tabella")
	}
	impronte, senzaZona, err := controllaVendita(r, att)
	if err != nil {
		return riassuntoImport{}, err
	}
	s := riassuntoImport{Verdetto: r.Verdetto, Lette: map[string]int64{}, Scritte: map[string]int64{},
		Avvisi: len(r.Avvisi), ImpronteTesto: impronte, IstantiSenzaZona: senzaZona, FileMedia: r.Media.FileCopiati, ByteMedia: r.Media.ByteCopiati,
		MediaVendita: att.mediaVendita, Emergenza: att.emergenza.scritto()}
	for _, t := range r.Tabelle {
		s.Lette[t.Standalone] = t.Lette
		for k, v := range t.Scritte {
			s.Scritte[k] += v
		}
	}
	return s, nil
}

// apriRegolare apre in lettura un file che ha scritto un container: dev'essere un file regolare,
// senza collegamenti (Lstat e O_NOFOLLOW), e lo stesso file fra il controllo e l'apertura.
func apriRegolare(p string) (*os.File, error) {
	prima, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !prima.Mode().IsRegular() {
		return nil, fmt.Errorf("%s non è un file regolare", p)
	}
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	dopo, err := f.Stat()
	if err == nil && (!dopo.Mode().IsRegular() || !os.SameFile(prima, dopo)) {
		err = fmt.Errorf("%s è cambiato durante l'apertura", p)
	}
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return f, nil
}

// rendiPrivato mette 0600 sul file aperto, mai sul bersaglio di un collegamento.
func rendiPrivato(p string) error {
	f, err := apriRegolare(p)
	if err != nil {
		return err
	}
	return errors.Join(f.Chmod(0o600), f.Close())
}

// leggiPiccolo legge un file regolare scritto da un container, al massimo max byte.
func leggiPiccolo(p string, max int64) ([]byte, error) {
	f, err := apriRegolare(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // solo lettura
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err == nil && int64(len(b)) > max {
		err = fmt.Errorf("%s supera %d byte", p, max)
	}
	return b, err
}

// inChiaro dice se il file comincia con l'intestazione di un SQLite in chiaro: la copia dello
// standalone non deve averla (è cifrata), il risultato della decifratura sì.
func inChiaro(p string) (bool, error) {
	f, err := apriRegolare(p)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }() // solo lettura
	b := make([]byte, 16)
	n, err := io.ReadFull(f, b)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return false, err
	}
	return n == 16 && string(b) == "SQLite format 3\x00", nil
}

// copiaFile copia un file regolare (senza seguire collegamenti) in uno nuovo 0600.
func copiaFile(src, dst string) (int64, error) {
	in, err := os.OpenFile(src, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return 0, err
	}
	defer func() { _ = in.Close() }() // solo lettura
	fi, err := in.Stat()
	if err != nil {
		return 0, err
	}
	if !fi.Mode().IsRegular() {
		return 0, fmt.Errorf("%s non è un file regolare", src)
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	return n, errors.Join(err, out.Close())
}

// copiaAlbero copia una cartella con sole sottocartelle e file regolari in una cartella nuova
// (0700, file 0600). Un collegamento o un file speciale fermano la copia.
func copiaAlbero(src, dst string) (file, byte_ int64, err error) {
	fi, err := os.Lstat(src)
	if err != nil {
		return 0, 0, err
	}
	if !fi.IsDir() {
		return 0, 0, fmt.Errorf("%s non è una cartella", src)
	}
	if err := os.Mkdir(dst, 0o700); err != nil {
		return 0, 0, err
	}
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil || rel == "." {
			return err
		}
		q := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.Mkdir(q, 0o700)
		case d.Type().IsRegular():
			n, err := copiaFile(p, q)
			file, byte_ = file+1, byte_+n
			return err
		}
		return fmt.Errorf("%s non è né un file regolare né una cartella", p)
	})
	return file, byte_, err
}

// copiaDatabaseCifrato copia gestrip.db dello standalone fermo, con il suo -wal se c'è, in una
// cartella nuova. Un -journal vuol dire una transazione a metà: si rifiuta. Lo -shm non serve.
func copiaDatabaseCifrato(data, dst string) error {
	if _, err := os.Lstat(filepath.Join(data, "gestrip.db-journal")); err == nil {
		return errors.New("accanto al database dello standalone c'è un -journal: transazione a metà")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	fi, err := os.Lstat(filepath.Join(data, "gestrip.db"))
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return errors.New("il database dello standalone non è un file regolare")
	}
	if err := os.Mkdir(dst, 0o700); err != nil {
		return err
	}
	if _, err := copiaFile(filepath.Join(data, "gestrip.db"), filepath.Join(dst, "gestrip.db")); err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(data, "gestrip.db-wal")); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	_, err = copiaFile(filepath.Join(data, "gestrip.db-wal"), filepath.Join(dst, "gestrip.db-wal"))
	return err
}

func impronta(p string) (string, int64, error) {
	f, err := apriRegolare(p)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }() // solo lettura
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

// dockerTee lancia docker con l'uscita sul terminale e la tiene anche per leggerla.
func dockerTee(args ...string) (string, error) {
	var b bytes.Buffer
	c := exec.Command("docker", args...)
	c.Stdout, c.Stderr = io.MultiWriter(os.Stdout, &b), os.Stderr
	err := c.Run()
	return b.String(), err
}

// sorgenteGo è ciò che la parte standalone della catena consegna all'import Go.
type sorgenteGo struct {
	sha                  string
	dimensione           int64
	conMiniature         bool
	fileMedia, byteMedia int64
	mediaVendita         int64            // i file che la vendita ha aggiunto ai media
	conteggiVendita      map[string]int64 // righe delle tabelle delle vendite nello standalone
	emergenza            esitoEmergenza   // ciò che ha scritto il passo del registro d'emergenza
}

// catenaStandalone fa i passi della catena dalla parte dello standalone, con l'app ferma: cattura
// sintetica, legacy import, vendita con foto, registro d'emergenza, copia del database e dei media,
// decifratura. Va
// chiamata dopo il bootstrap e prima di avviare il container dell'app.
func catenaStandalone(n nomi, img string, env []string) (sorgenteGo, error) {
	p := catenaPer(n)
	for _, d := range []string{p.dir, filepath.Dir(p.script), filepath.Dir(p.flask), p.uscitaStandalone, filepath.Dir(p.foto), p.chiaro} {
		if err := dentroIstanza(n, d); err != nil {
			return sorgenteGo{}, err
		}
		if err := os.Mkdir(d, 0o700); err != nil {
			return sorgenteGo{}, err
		}
	}
	script, err := daTag(scriptCattura)
	if err != nil {
		return sorgenteGo{}, fmt.Errorf("lettura di %s da %s: %w", scriptCattura, tagStandalone, err)
	}
	if err := scriviPrivato(p.script, string(script)); err != nil {
		return sorgenteGo{}, err
	}
	fmt.Println("catena: cattura Flask sintetica…")
	if err := dockerFa(comandoCattura(n, img, p)...); err != nil {
		return sorgenteGo{}, fmt.Errorf("cattura sintetica: %w", err)
	}
	if ok, err := inChiaro(filepath.Join(p.flask, "riparazioni.db")); err != nil || !ok {
		return sorgenteGo{}, fmt.Errorf("la cattura sintetica non ha un riparazioni.db SQLite (%v)", err)
	}
	if err := scriviPrivato(p.chiaveArchivio, segreto(32)+"\n"); err != nil {
		return sorgenteGo{}, err
	}
	fmt.Println("catena: legacy import sintetico nello standalone…")
	out, err := dockerTiene(comandoImportStandalone(n, img, env, p)...)
	if err != nil {
		return sorgenteGo{}, fmt.Errorf("legacy import dello standalone: %w (report in %s)", err, p.uscitaStandalone)
	}
	if err := esitoImportStandalone(out); err != nil {
		return sorgenteGo{}, err
	}
	vendita, mediaVendita, err := venditaConFoto(n, img, env, p)
	if err != nil {
		return sorgenteGo{}, err
	}
	emergenza, err := registroEmergenza(n, img, env)
	if err != nil {
		return sorgenteGo{}, err
	}
	fmt.Println("catena: copia del database cifrato e dei media…")
	if err := copiaDatabaseCifrato(filepath.Join(n.dir, "data"), p.cifrato); err != nil {
		return sorgenteGo{}, err
	}
	if ok, err := inChiaro(filepath.Join(p.cifrato, "gestrip.db")); err != nil || ok {
		return sorgenteGo{}, fmt.Errorf("la copia del database dello standalone non è cifrata (%v)", err)
	}
	fileMedia, byteMedia, err := copiaAlbero(filepath.Join(n.dir, "media"), p.media)
	if err != nil {
		return sorgenteGo{}, fmt.Errorf("copia dei media: %w", err)
	}
	fmt.Printf("catena: media dello standalone: %d file, %d byte\n", fileMedia, byteMedia)
	conMiniature := false
	if _, err := os.Lstat(filepath.Join(n.dir, "data", "thumbnails")); err == nil {
		file, byte_, err := copiaAlbero(filepath.Join(n.dir, "data", "thumbnails"), p.miniature)
		if err != nil {
			return sorgenteGo{}, fmt.Errorf("copia delle miniature: %w", err)
		}
		conMiniature = true
		fmt.Printf("catena: miniature dello standalone: %d file, %d byte\n", file, byte_)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return sorgenteGo{}, err
	}
	fmt.Println("catena: decifratura con il SQLCipher dell'immagine dello standalone…")
	out, err = dockerTiene(comandoDecifra(n, img, p)...)
	if err != nil {
		return sorgenteGo{}, fmt.Errorf("decifratura: %w", err)
	}
	if !strings.Contains(out, "GR_DECIFRATO") {
		return sorgenteGo{}, errors.New("decifratura: manca GR_DECIFRATO")
	}
	chiaro := filepath.Join(p.chiaro, nomeChiaro)
	if ok, err := inChiaro(chiaro); err != nil || !ok {
		return sorgenteGo{}, fmt.Errorf("il database decifrato non è un SQLite in chiaro (%v)", err)
	}
	if err := rendiPrivato(chiaro); err != nil {
		return sorgenteGo{}, err
	}
	sha, dim, err := impronta(chiaro)
	if err != nil {
		return sorgenteGo{}, err
	}
	return sorgenteGo{sha: sha, dimensione: dim, conMiniature: conMiniature, fileMedia: fileMedia, byteMedia: byteMedia,
		mediaVendita: mediaVendita, conteggiVendita: vendita.Conteggi, emergenza: emergenza}, nil
}

// catenaGo importa nel Go il database decifrato e i media, e accetta solo un report riconciliato.
func catenaGo(n nomi, gestrip string, s sorgenteGo) (riassuntoImport, error) {
	p := catenaPer(n)
	report := filepath.Join(n.dir, "esito", nomeReportGo)
	fmt.Println("catena: gestrip import -sintetico…")
	errImport := dockerFa(comandoImportGo(n, gestrip, p, s.sha, s.dimensione, s.conMiniature)...)
	b, err := leggiPiccolo(report, 16<<20)
	if err != nil {
		return riassuntoImport{}, errors.Join(errImport, fmt.Errorf("report dell'import Go: %w", err))
	}
	r, err := leggiReportImport(b, s)
	if err != nil {
		return riassuntoImport{}, fmt.Errorf("%w (report in %s)", err, report)
	}
	if errImport != nil {
		return riassuntoImport{}, fmt.Errorf("gestrip import: %w (report in %s)", errImport, report)
	}
	// Riconciliato: le copie in chiaro e cifrate della catena non servono più (restano solo dopo
	// un errore, per la diagnosi). Restano la cattura sintetica e l'uscita del legacy import.
	for _, d := range []string{p.cifrato, p.chiaro, p.media, p.miniature} {
		if err := dentroIstanza(n, d); err != nil {
			return riassuntoImport{}, err
		}
		if err := os.RemoveAll(d); err != nil {
			return riassuntoImport{}, err
		}
	}
	return r, nil
}

func datiDi(conCatena bool) string {
	if conCatena {
		return datiSinteticiImport
	}
	return "account-seminati"
}

// stampaRiassunto scrive solo numeri: verdetto, avvisi delle impronte salvate come testo, righe
// delle vendite lette e scritte, ciò che ha scritto il registro d'emergenza, righe lette per tabella
// dello standalone e scritte per tabella Go, file e byte dei media.
func stampaRiassunto(r riassuntoImport) {
	for _, l := range righeRiassunto(r) {
		fmt.Println(l)
	}
}

func righeRiassunto(r riassuntoImport) []string {
	righe := []string{fmt.Sprintf("catena: import Go %s, %d avvisi, media %d file %d byte (%d in più dalla vendita)",
		r.Verdetto, r.Avvisi, r.FileMedia, r.ByteMedia, r.MediaVendita)}
	var k []string
	for _, t := range tabelleImprontaTesto {
		k = append(k, fmt.Sprintf("%s=%d", t, r.ImpronteTesto[t]))
	}
	righe = append(righe, "catena: avvisi "+codiceImprontaTesto+": "+strings.Join(k, " "))
	k = nil
	for c, n := range r.IstantiSenzaZona {
		k = append(k, fmt.Sprintf("%s=%d", c, n))
	}
	sort.Strings(k)
	righe = append(righe, "catena: avvisi "+codiceIstanteSenzaZona+": "+strings.Join(k, " "))
	k = nil
	for _, t := range tabelleVendita {
		k = append(k, fmt.Sprintf("%s=%d→%s=%d", t.standalone, r.Lette[t.standalone], t.goTab, r.Scritte[t.goTab]))
	}
	righe = append(righe, "catena: vendite lette→scritte: "+strings.Join(k, " "))
	righe = append(righe, fmt.Sprintf("catena: registro d'emergenza: %d righe (riga e storno), %d evento chiuso, %d busta scansionata",
		r.Emergenza.Righe, r.Emergenza.EventiChiusi, r.Emergenza.BusteScansionate))
	for _, m := range []struct {
		cosa string
		c    map[string]int64
	}{{"lette", r.Lette}, {"scritte", r.Scritte}} {
		var k []string
		for t, v := range m.c {
			if v > 0 {
				k = append(k, fmt.Sprintf("%s=%d", t, v))
			}
		}
		sort.Strings(k)
		righe = append(righe, fmt.Sprintf("catena: %s: %s", m.cosa, strings.Join(k, " ")))
	}
	return righe
}
