package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/media"
)

// Tutti i dati qui sono inventati: un database nuovo con le migrazioni, due «foto» di testo e
// segreti finti, in una cartella temporanea del test che fa da volume dei dati.

const istanteProva = "2026-11-25T02:30:00.000000Z"

type prova struct {
	s     Sorgente
	dir   string // la cartella dei dati: database, media/, secrets/
	foto  []string
	flask string
}

func nuovaProva(t *testing.T) *prova {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Apri(filepath.Join(dir, "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(time.Date(2026, 11, 25, 2, 30, 0, 0, time.UTC))
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	a := media.Nuovo(media.CartellaDi(d.Percorso()))
	if err := os.MkdirAll(a.Cartella(), 0o700); err != nil {
		t.Fatal(err)
	}
	p := &prova{s: Sorgente{DB: d, Archivio: a, Orologio: o}, dir: dir, flask: "segreto-flask-inventato"}
	for _, c := range []string{"foto inventata uno", "foto inventata due"} {
		k, err := a.Salva([]byte(c))
		if err != nil {
			t.Fatal(err)
		}
		p.foto = append(p.foto, k)
	}
	// Un temporaneo di un salvataggio a metà: non è un dato e non va nell'archivio.
	if err := os.WriteFile(filepath.Join(a.Cartella(), ".upload-abc"), []byte("a metà"), 0o600); err != nil {
		t.Fatal(err)
	}
	p.segreto(t, "segreto_flask", p.flask)
	p.segreto(t, "segreto_standalone", "segreto-standalone-inventato")
	// La chiave dei backup sta in secrets/ (D-113) ma mai nell'archivio.
	p.segreto(t, NomeChiaveBackup, "chiave finta dei backup, non una chiave age vera")
	ctx := context.Background()
	for i := range 3 {
		if _, err := d.Scrittura.ExecContext(ctx, `INSERT INTO cancellazioni_media (chiave, creato_il) VALUES (?, ?)`,
			strings.Repeat(string(rune('a'+i)), 64), istanteProva); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Scrittura.ExecContext(ctx, `INSERT INTO eventi_audit (attore, azione, risorsa, id_risorsa, esito, creato_il)
		VALUES ('prova', 'prova', 'prova', '1', 'ok', ?)`, istanteProva); err != nil {
		t.Fatal(err)
	}
	// Niente statfs vero nei test: spazio abbondante, salvo dove si prova il rifiuto.
	vecchio := spazioLibero
	spazioLibero = func(string) (uint64, error) { return 1 << 50, nil }
	t.Cleanup(func() { spazioLibero = vecchio })
	return p
}

func (p *prova) segreto(t *testing.T, nome, contenuto string) {
	t.Helper()
	sd := filepath.Join(p.dir, CartellaSegreti)
	if err := os.MkdirAll(sd, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sd, nome), []byte(contenuto), 0o600); err != nil {
		t.Fatal(err)
	}
}

// voci legge il tar: nome → contenuto, nell'ordine in cui compaiono.
func voci(t *testing.T, b []byte) (map[string][]byte, []string) {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(b))
	m := map[string][]byte{}
	var ordine []string
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return m, ordine
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag != tar.TypeReg {
			t.Fatalf("voce %q di tipo %c, attesi solo file", h.Name, h.Typeflag)
		}
		if _, ok := m[h.Name]; ok {
			t.Fatalf("voce %q ripetuta", h.Name)
		}
		c, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		m[h.Name] = c
		ordine = append(ordine, h.Name)
	}
}

func sha(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Voci OPS-16 e OPS-27, in parte: niente segno «inventario:», che chiuderebbe le voci (lo mette la slice che le chiude).
// Il tar ha il database, tutte le foto, il segreto Flask e quello dello standalone, inventory.json
// e snapshot.json; non ha i temporanei dei media né la chiave dei backup.
func TestArchivioContenuto(t *testing.T) {
	p := nuovaProva(t)
	var buf bytes.Buffer
	e, err := Scrivi(context.Background(), p.s, &buf)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := voci(t, buf.Bytes())
	attesi := []string{"data/gestrip.db", "inventory.json", "snapshot.json",
		"secrets/segreto_flask", "secrets/segreto_standalone"}
	for _, k := range p.foto {
		attesi = append(attesi, "media/"+k)
	}
	slices.Sort(attesi)
	if got := slices.Sorted(maps.Keys(m)); !slices.Equal(got, attesi) {
		t.Fatalf("voci del tar\n%q\nattese\n%q", got, attesi)
	}
	if string(m["secrets/segreto_flask"]) != p.flask {
		t.Errorf("segreto Flask %q", m["secrets/segreto_flask"])
	}
	if string(m["media/"+p.foto[0]]) != "foto inventata uno" {
		t.Errorf("foto %q", m["media/"+p.foto[0]])
	}
	if !bytes.HasPrefix(m["data/gestrip.db"], []byte("SQLite format 3\x00")) {
		t.Error("data/gestrip.db non è un database SQLite")
	}
	if e.Byte != int64(buf.Len()) {
		t.Errorf("byte scritti %d, nel buffer %d", e.Byte, buf.Len())
	}
}

// Voce OPS-16, in parte (senza segno «inventario:», vedi TestArchivioContenuto).
// inventory.json ha lo SHA-256 di ogni file del tar tranne i due json, e nient'altro; è lo stesso
// inventario che Scrivi restituisce.
func TestArchivioInventario(t *testing.T) {
	p := nuovaProva(t)
	var buf bytes.Buffer
	e, err := Scrivi(context.Background(), p.s, &buf)
	if err != nil {
		t.Fatal(err)
	}
	m, ordine := voci(t, buf.Bytes())
	var inv map[string]string
	if err := json.Unmarshal(m["inventory.json"], &inv); err != nil {
		t.Fatal(err)
	}
	calcolato := map[string]string{}
	for n, c := range m {
		if n != "inventory.json" && n != "snapshot.json" {
			calcolato[n] = sha(c)
		}
	}
	if !maps.Equal(inv, calcolato) {
		t.Fatalf("inventory.json\n%v\ncalcolato\n%v", inv, calcolato)
	}
	if !maps.Equal(e.Inventario, inv) {
		t.Errorf("inventario restituito\n%v\nnel tar\n%v", e.Inventario, inv)
	}
	// Le foto hanno per nome il loro SHA-256.
	for _, k := range p.foto {
		if inv["media/"+k] != k {
			t.Errorf("media/%s con impronta %s", k, inv["media/"+k])
		}
	}
	// I due json chiudono il tar, come nello standalone.
	if n := len(ordine); n < 2 || ordine[n-2] != "inventory.json" || ordine[n-1] != "snapshot.json" {
		t.Errorf("ordine delle voci %q", ordine)
	}
}

// Voce OPS-16, in parte (senza segno «inventario:», vedi TestArchivioContenuto).
// snapshot.json: formato, versione del programma, migrazioni applicate e conteggi delle righe di
// ogni tabella della copia, senza il registro (eventi_audit) né le tabelle interne di SQLite.
func TestArchivioFotografia(t *testing.T) {
	p := nuovaProva(t)
	vecchia := versioneProgramma
	versioneProgramma = func() string { return "v0.0.0-prova" }
	t.Cleanup(func() { versioneProgramma = vecchia })
	var buf bytes.Buffer
	e, err := Scrivi(context.Background(), p.s, &buf)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := voci(t, buf.Bytes())
	var f Fotografia
	if err := json.Unmarshal(m["snapshot.json"], &f); err != nil {
		t.Fatal(err)
	}
	if f.Formato != Formato || f.Versione != "v0.0.0-prova" || f.CatturatoIl != istanteProva {
		t.Errorf("fotografia %+v", f)
	}
	ctx := context.Background()
	var migr []int
	r, err := p.s.DB.Lettura.QueryContext(ctx, `SELECT versione FROM schema_migrations ORDER BY versione`)
	if err != nil {
		t.Fatal(err)
	}
	for r.Next() {
		var v int
		if err := r.Scan(&v); err != nil {
			t.Fatal(err)
		}
		migr = append(migr, v)
	}
	if err := errors.Join(r.Err(), r.Close()); err != nil {
		t.Fatal(err)
	}
	if len(migr) == 0 || !slices.Equal(f.Migrazioni, migr) {
		t.Errorf("migrazioni %v, attese %v", f.Migrazioni, migr)
	}
	if _, ok := f.Conteggi["eventi_audit"]; ok {
		t.Error("eventi_audit nei conteggi: lo standalone lo esclude (ogni verifica aggiunge una riga)")
	}
	for n := range f.Conteggi {
		if strings.HasPrefix(n, "sqlite_") {
			t.Errorf("tabella interna %q nei conteggi", n)
		}
	}
	if f.Conteggi["cancellazioni_media"] != 3 || f.Conteggi["schema_migrations"] != int64(len(migr)) {
		t.Errorf("conteggi %v", f.Conteggi)
	}
	// Tutte le tabelle dello schema, tranne quelle escluse, hanno il loro conteggio.
	var tabelle int
	if err := p.s.DB.Lettura.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type = 'table'
		AND name NOT LIKE 'sqlite_%' AND name <> 'eventi_audit'`).Scan(&tabelle); err != nil {
		t.Fatal(err)
	}
	if len(f.Conteggi) != tabelle {
		t.Errorf("%d conteggi, %d tabelle", len(f.Conteggi), tabelle)
	}
	if !slices.Equal(e.Migrazioni, f.Migrazioni) || !maps.Equal(e.Conteggi, f.Conteggi) {
		t.Error("la fotografia restituita non è quella del tar")
	}
}

// I conteggi si leggono sulla copia, non sul database acceso: una riga scritta dopo la copia non
// c'è né nel database del tar né nei conteggi.
func TestArchivioConteggiSullaCopia(t *testing.T) {
	p := nuovaProva(t)
	w := &scrittoreFermo{fermo: make(chan struct{}), primo: make(chan struct{})}
	fatto := make(chan error, 1)
	var e Esito
	go func() {
		var err error
		e, err = Scrivi(context.Background(), p.s, w)
		fatto <- err
	}()
	w.aspetta(t, fatto) // la copia c'è già: si sta scrivendo il tar
	if _, err := p.s.DB.Scrittura.Exec(`INSERT INTO cancellazioni_media (chiave, creato_il) VALUES (?, ?)`,
		strings.Repeat("f", 64), istanteProva); err != nil {
		t.Fatal(err)
	}
	close(w.fermo)
	if err := <-fatto; err != nil {
		t.Fatal(err)
	}
	if e.Conteggi["cancellazioni_media"] != 3 {
		t.Errorf("cancellazioni_media %d, attese 3 (le righe della copia)", e.Conteggi["cancellazioni_media"])
	}
}

// Voce OPS-16, in parte (senza segno «inventario:», vedi TestArchivioContenuto).
// Con meno spazio libero del database più il margine il backup non parte: niente scritto, niente
// copia lasciata nel volume dei dati.
func TestArchivioSpazioInsufficiente(t *testing.T) {
	p := nuovaProva(t)
	// Il database e il suo WAL (VACUUM INTO copia anche le pagine del WAL).
	var dimensione uint64
	for _, n := range []string{p.s.DB.Percorso(), p.s.DB.Percorso() + "-wal"} {
		fi, err := os.Stat(n)
		if err != nil {
			t.Fatal(err)
		}
		dimensione += uint64(fi.Size())
	}
	var chiesto string
	spazioLibero = func(dir string) (uint64, error) {
		chiesto = dir
		return dimensione + MargineSpazio - 1, nil
	}
	var buf bytes.Buffer
	_, err := Scrivi(context.Background(), p.s, &buf)
	if !errors.Is(err, ErrSpazio) {
		t.Fatalf("errore %v, atteso ErrSpazio", err)
	}
	if chiesto != p.dir {
		t.Errorf("spazio chiesto per %q, atteso il volume dei dati %q", chiesto, p.dir)
	}
	if buf.Len() != 0 {
		t.Errorf("%d byte scritti", buf.Len())
	}
	restano(t, p.dir)

	spazioLibero = func(string) (uint64, error) { return 0, errors.New("statfs finto non riuscito") }
	if _, err := Scrivi(context.Background(), p.s, &buf); err == nil {
		t.Fatal("spazio illeggibile: il backup è partito lo stesso")
	}
	restano(t, p.dir)

	// Con lo spazio giusto (il database, il WAL e il margine, al byte) parte.
	spazioLibero = func(string) (uint64, error) { return dimensione + MargineSpazio, nil }
	if _, err := Scrivi(context.Background(), p.s, &buf); err != nil {
		t.Fatal(err)
	}
}

// restano controlla che nel volume dei dati non resti una cartella temporanea del backup.
func restano(t *testing.T, dir string) {
	t.Helper()
	v, err := filepath.Glob(filepath.Join(dir, prefissoTemporanea+"*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 0 {
		t.Errorf("restano %q", v)
	}
}

// La copia del database sta in una cartella temporanea dentro il volume dei dati (mai /tmp), 0700, e
// sparisce a backup finito.
func TestArchivioCopiaNelVolumeDeiDati(t *testing.T) {
	p := nuovaProva(t)
	w := &scrittoreFermo{fermo: make(chan struct{}), primo: make(chan struct{})}
	fatto := make(chan error, 1)
	go func() {
		_, err := Scrivi(context.Background(), p.s, w)
		fatto <- err
	}()
	w.aspetta(t, fatto)
	v, err := filepath.Glob(filepath.Join(p.dir, prefissoTemporanea+"*"))
	if err != nil || len(v) != 1 {
		t.Fatalf("cartelle temporanee %q (%v), attesa una", v, err)
	}
	fi, err := os.Stat(v[0])
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("cartella temporanea %v (%v), attesa 0700", fi.Mode(), err)
	}
	if _, err := os.Stat(filepath.Join(v[0], "gestrip.db")); err != nil {
		t.Errorf("la copia non è nella cartella temporanea: %v", err)
	}
	close(w.fermo)
	if err := <-fatto; err != nil {
		t.Fatal(err)
	}
	restano(t, p.dir)
}

// Un errore a metà (lo scrittore si rompe) toglie la copia e lo dice.
func TestArchivioErroreDelloScrittore(t *testing.T) {
	p := nuovaProva(t)
	if _, err := Scrivi(context.Background(), p.s, scrittoreRotto{}); err == nil {
		t.Fatal("scrittore rotto: nessun errore")
	}
	restano(t, p.dir)
}

type scrittoreRotto struct{}

func (scrittoreRotto) Write([]byte) (int, error) { return 0, errors.New("destinazione finta rotta") }

// scrittoreFermo si ferma alla prima scrittura (segnala primo) finché fermo non si chiude.
type scrittoreFermo struct {
	fermo, primo chan struct{}
	visto        bool
	buf          bytes.Buffer
}

// aspetta la prima scrittura; se Scrivi finisce prima (fatto) o non scrive entro 10 s, il test
// cade invece di restare appeso.
func (w *scrittoreFermo) aspetta(t *testing.T, fatto <-chan error) {
	t.Helper()
	select {
	case <-w.primo:
	case err := <-fatto:
		t.Fatalf("Scrivi finito prima di scrivere: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("Scrivi non scrive")
	}
}

func (w *scrittoreFermo) Write(b []byte) (int, error) {
	if !w.visto {
		w.visto = true
		close(w.primo)
		<-w.fermo
	}
	return w.buf.Write(b)
}

// Voce OPS-16, in parte (senza segno «inventario:», vedi TestArchivioContenuto).
// Mentre il backup legge l'archivio la coda delle cancellazioni (D-55) aspetta: una foto in coda e
// senza righe resta finché il tar non è scritto, poi la pulizia la toglie.
func TestArchivioLaPuliziaAspetta(t *testing.T) {
	p := nuovaProva(t)
	k := p.foto[1]
	if _, err := p.s.DB.Scrittura.Exec(`INSERT INTO cancellazioni_media (chiave, creato_il) VALUES (?, ?)`, k, istanteProva); err != nil {
		t.Fatal(err)
	}
	pul := media.NuovaPulizia(p.s.DB, p.s.Archivio, nil)
	w := &scrittoreFermo{fermo: make(chan struct{}), primo: make(chan struct{})}
	fatto := make(chan error, 1)
	go func() {
		_, err := Scrivi(context.Background(), p.s, w)
		fatto <- err
	}()
	w.aspetta(t, fatto)
	pulita := make(chan error, 1)
	go func() { pulita <- pul.CancellaInCoda(context.Background()) }()
	select {
	case err := <-pulita:
		t.Fatalf("la pulizia è passata durante il backup (%v)", err)
	case <-time.After(300 * time.Millisecond):
	}
	// Intanto i salvataggi vanno avanti: aspetta solo la coda.
	salvato := make(chan error, 1)
	go func() {
		_, err := p.s.Archivio.Salva([]byte("foto nuova durante il backup"))
		salvato <- err
	}()
	select {
	case err := <-salvato:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("un salvataggio aspetta il backup")
	}
	if _, err := os.Stat(filepath.Join(p.s.Archivio.Cartella(), k)); err != nil {
		t.Fatalf("la foto in coda è sparita durante il backup: %v", err)
	}
	close(w.fermo)
	if err := <-fatto; err != nil {
		t.Fatal(err)
	}
	if err := <-pulita; err != nil {
		t.Fatal(err)
	}
	m, _ := voci(t, w.buf.Bytes())
	if string(m["media/"+k]) != "foto inventata due" {
		t.Error("la foto in coda non è nel tar")
	}
	if _, err := os.Stat(filepath.Join(p.s.Archivio.Cartella(), k)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dopo il backup la pulizia non ha tolto la foto in coda: %v", err)
	}
}

// Voce OPS-16, in parte (senza segno «inventario:», vedi TestArchivioContenuto).
// Nessun nome del tar esce dall'archivio: solo nomi relativi, puliti, sotto data/, media/, secrets/
// o i due json.
func TestNomeSicuro(t *testing.T) {
	buoni := []string{"data/gestrip.db", "media/" + strings.Repeat("0", 64), "secrets/segreto_flask",
		"inventory.json", "snapshot.json"}
	for _, n := range buoni {
		if err := nomeSicuro(n); err != nil {
			t.Errorf("%q rifiutato: %v", n, err)
		}
	}
	cattivi := []string{"", "/etc/passwd", "../fuori", "media/../../fuori", "media/./x", "media//x",
		"media/", "altro/x", "data/../secrets/x", "media/a b", "media/x\x00", `media\x`, "media/sotto/x",
		"inventory.json/x", "secrets", "data"}
	for _, n := range cattivi {
		if err := nomeSicuro(n); !errors.Is(err, ErrFileInatteso) {
			t.Errorf("%q accettato (%v)", n, err)
		}
	}
}

// Voce OPS-16, in parte (senza segno «inventario:», vedi TestArchivioContenuto).
// Nella cartella media un collegamento simbolico, una sottocartella o un file con un nome che non è
// un indirizzo di contenuto fermano il backup (il tar non segue mai un collegamento fuori), e la
// copia del database non resta.
func TestArchivioMediaInattesi(t *testing.T) {
	casi := map[string]func(t *testing.T, dir string){
		"collegamento": func(t *testing.T, dir string) {
			fuori := filepath.Join(t.TempDir(), "fuori")
			if err := os.WriteFile(fuori, []byte("fuori dall'archivio"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(fuori, filepath.Join(dir, strings.Repeat("1", 64))); err != nil {
				t.Fatal(err)
			}
		},
		"sottocartella": func(t *testing.T, dir string) {
			if err := os.Mkdir(filepath.Join(dir, strings.Repeat("2", 64)), 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"nome strano": func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "appunti.txt"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for nome, prepara := range casi {
		t.Run(nome, func(t *testing.T) {
			p := nuovaProva(t)
			prepara(t, p.s.Archivio.Cartella())
			_, err := Scrivi(context.Background(), p.s, io.Discard)
			if !errors.Is(err, ErrFileInatteso) {
				t.Fatalf("errore %v, atteso ErrFileInatteso", err)
			}
			restano(t, p.dir)
		})
	}
}

// Voce OPS-16, in parte (senza segno «inventario:», vedi TestArchivioContenuto).
// La chiave dei backup non entra mai nell'archivio (D-113): con il suo nome resta fuori; un file di
// secrets/ che contiene una chiave privata age, con qualunque nome, ferma il backup; un file
// sconosciuto in secrets/ lo ferma (va deciso se entra o no).
func TestArchivioChiaveDeiBackupFuori(t *testing.T) {
	t.Run("con il suo nome resta fuori", func(t *testing.T) {
		p := nuovaProva(t)
		p.segreto(t, NomeChiaveBackup, "AGE-SECRET-KEY-1INVENTATA")
		var buf bytes.Buffer
		if _, err := Scrivi(context.Background(), p.s, &buf); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(buf.Bytes(), []byte("AGE-SECRET-KEY-")) {
			t.Error("la chiave dei backup è nel tar")
		}
	})
	t.Run("una chiave age in un segreto ammesso", func(t *testing.T) {
		p := nuovaProva(t)
		p.segreto(t, "segreto_standalone", "prima\nAGE-SECRET-KEY-1INVENTATA\n")
		var buf bytes.Buffer
		_, err := Scrivi(context.Background(), p.s, &buf)
		if !errors.Is(err, ErrChiaveBackupNellArchivio) {
			t.Fatalf("errore %v, atteso ErrChiaveBackupNellArchivio", err)
		}
		if bytes.Contains(buf.Bytes(), []byte("AGE-SECRET-KEY-")) {
			t.Error("la chiave è finita nel tar prima del rifiuto")
		}
		restano(t, p.dir)
	})
	t.Run("un file sconosciuto", func(t *testing.T) {
		p := nuovaProva(t)
		p.segreto(t, "chiave_age_copia", "AGE-SECRET-KEY-1INVENTATA")
		_, err := Scrivi(context.Background(), p.s, io.Discard)
		if !errors.Is(err, ErrFileInatteso) {
			t.Fatalf("errore %v, atteso ErrFileInatteso", err)
		}
		restano(t, p.dir)
	})
	t.Run("secrets assente", func(t *testing.T) {
		p := nuovaProva(t)
		if err := os.RemoveAll(filepath.Join(p.dir, CartellaSegreti)); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if _, err := Scrivi(context.Background(), p.s, &buf); err != nil {
			t.Fatal(err)
		}
		m, _ := voci(t, buf.Bytes())
		for n := range m {
			if strings.HasPrefix(n, "secrets/") {
				t.Errorf("voce %q senza la cartella secrets", n)
			}
		}
	})
}
