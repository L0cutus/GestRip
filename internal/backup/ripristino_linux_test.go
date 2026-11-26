package backup

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/media"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// Il ripristino isolato (slice 130). Tutto inventato: il database e le foto di nuovaProva, chiavi age generate
// nel test, una configurazione remota finta che non porta da nessuna parte.

// tabelleRegistri sono i cinque registri dei backup che il ripristino svuota (nota 13(a) e (c)).
var tabelleRegistri = []string{"verifiche_backup", "esecuzioni_backup", "prove_ripristino", "copie_remote", "prove_remote"}

var oraRipristino = time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)

// archivioDaRipristinare prepara una cartella dei dati inventata con i canali accesi, la copia remota configurata
// e righe nei registri dei backup, ne fa un backup e dà la prova, l'archivio e il contenuto del file della chiave.
func archivioDaRipristinare(t *testing.T) (*prova, string, []byte) {
	t.Helper()
	p := nuovaProva(t)
	conChiave(t, p)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO impostazioni VALUES ('` + uscita.ChiaveEmailAcceso + `', '1', '` + istanteProva + `'), ('` +
			uscita.ChiaveTelegramAcceso + `', '1', '` + istanteProva + `'), ('` + ChiaveRemoto + `', '{"host":"sftp.example.invalid"}', '` +
			istanteProva + `'), ('` + ChiaveAvvisi + `', '{}', '` + istanteProva + `')`,
		`INSERT INTO esecuzioni_backup (avvio, turno, iniziato_il, finito_il, esito) VALUES ('automatico', '2026-11-24', '` + istanteProva + `', '` + istanteProva + `', 'ok')`,
		`INSERT INTO verifiche_backup (esito, destinazione, archivio, sha256, byte, catturato_il, verificato_il, conteggi)
			VALUES ('ok', 'chiavetta', '/chiavetta/a.tar.age', '` + strings.Repeat("a", 64) + `', 1, '` + istanteProva + `', '` + istanteProva + `', '{}')`,
		`INSERT INTO prove_ripristino (avvio, settimana, iniziato_il, finito_il, esito, errore) VALUES ('a_mano', '2026-11-22', '` + istanteProva + `', '` + istanteProva + `', 'fallita', 'x')`,
		`INSERT INTO copie_remote (avvio, iniziato_il, finito_il, esito, codice, errore) VALUES ('automatico', '` + istanteProva + `', '` + istanteProva + `', 'fallita', 'rete', 'x')`,
		`INSERT INTO prove_remote (avvio, mese, turno, iniziato_il, finito_il, esito, codice, errore) VALUES ('automatico', '2026-11', '2026-11-22', '` + istanteProva + `', '` + istanteProva + `', 'fallita', 'rete', 'x')`,
	} {
		if _, err := p.s.DB.Scrittura.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.s.DB.ChiaveServer(ctx, p.s.Orologio); err != nil {
		t.Fatal(err)
	}
	r, err := Cifra(ctx, p.s, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chiave, err := os.ReadFile(FileChiave(p.dir))
	if err != nil {
		t.Fatal(err)
	}
	return p, r.File, chiave
}

// senzaRestiDelRipristino controlla che accanto alla destinazione non resti una cartella temporanea.
func senzaRestiDelRipristino(t *testing.T, padre string) {
	t.Helper()
	if m, _ := filepath.Glob(filepath.Join(padre, prefissoRipristino+"*")); len(m) != 0 {
		t.Errorf("cartelle temporanee del ripristino rimaste: %v", m)
	}
}

// Voce OPS-18: il ripristino su una cartella nuova, con la sola chiave dei backup.
// inventario: OPS-18
// gestrip restore in una cartella che non c'è: la cartella nasce 0700 con database, media e segreti uguali al
// backup (file 0600) e la chiave dei backup data; il database è isolato (canali e copia remota spenti e messi da
// parte, stato degli avvisi tolto, registri dei backup vuoti, chiave del server nuova); nessun temporaneo resta.
func TestRipristinoInCartellaNuova(t *testing.T) {
	p, archivio, chiave := archivioDaRipristinare(t)
	vecchia, err := p.s.DB.ChiaveServer(context.Background(), p.s.Orologio)
	if err != nil {
		t.Fatal(err)
	}
	padre := t.TempDir()
	dest := filepath.Join(padre, "dati")
	r, err := Ripristina(context.Background(), Ripristino{Archivio: archivio, Chiave: chiave, Cartella: dest, Orologio: clock.NuovoFinto(oraRipristino)})
	if err != nil {
		t.Fatal(err)
	}
	senzaRestiDelRipristino(t, padre)
	if r.Cartella != dest || r.DB != filepath.Join(dest, "gestrip.db") || r.Isolamento.Archivio != filepath.Base(archivio) || r.SHA256 == "" {
		t.Errorf("esito %+v", r)
	}
	if fi, err := os.Lstat(dest); err != nil || fi.Mode() != fs.ModeDir|0o700 {
		t.Fatalf("cartella: %v %v", fi, err)
	}
	for _, k := range p.foto {
		atteso, _ := os.ReadFile(filepath.Join(p.dir, media.Cartella, k))
		got, err := os.ReadFile(filepath.Join(dest, media.Cartella, k))
		if err != nil || !bytes.Equal(got, atteso) {
			t.Errorf("foto %s: %v", k, err)
		}
	}
	for n, atteso := range map[string][]byte{"segreto_flask": []byte(p.flask), "segreto_standalone": []byte("segreto-standalone-inventato"),
		NomeChiaveBackup: chiave} {
		f := filepath.Join(dest, CartellaSegreti, n)
		got, err := os.ReadFile(f)
		if err != nil || !bytes.Equal(got, atteso) {
			t.Errorf("segreto %s: %v", n, err)
		}
		if fi, err := os.Lstat(f); err != nil || fi.Mode() != 0o600 {
			t.Errorf("segreto %s: permessi %v %v", n, fi.Mode(), err)
		}
	}
	// La chiave copiata è leggibile come chiave dei backup della cartella nuova.
	if _, err := LeggiChiave(dest); err != nil {
		t.Errorf("chiave dei backup nella cartella nuova: %v", err)
	}
	d, err := db.Apri(r.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Chiudi() }()
	iso, ok, err := db.LeggiIsolamento(context.Background(), d.Lettura)
	if !ok || err != nil || iso.Spento[uscita.ChiaveEmailAcceso] != "1" || iso.Spento[uscita.ChiaveTelegramAcceso] != "1" ||
		iso.Spento[ChiaveRemoto] != `{"host":"sftp.example.invalid"}` || iso.RipristinatoIl != "2026-11-26T10:00:00.000000Z" {
		t.Fatalf("isolamento %+v %v %v", iso, ok, err)
	}
	var n int
	if err := d.Lettura.QueryRow(`SELECT count(*) FROM impostazioni WHERE chiave IN (?, ?, ?, ?)`, uscita.ChiaveEmailAcceso,
		uscita.ChiaveTelegramAcceso, ChiaveRemoto, ChiaveAvvisi).Scan(&n); err != nil || n != 0 {
		t.Errorf("impostazioni non spente: %d %v", n, err)
	}
	// Elenco fisso (non db.TabelleSvuotateDalRipristino): una tabella tolta dall'elenco del codice deve far cadere il test.
	for _, tab := range tabelleRegistri {
		if err := d.Lettura.QueryRow(`SELECT count(*) FROM ` + tab).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s: %d righe %v", tab, n, err)
		}
	}
	nuova, err := d.ChiaveServer(context.Background(), clock.NuovoFinto(oraRipristino))
	if err != nil || bytes.Equal(nuova, vecchia) {
		t.Errorf("chiave del server non rinnovata: %v", err)
	}
	// Il database di partenza non è stato toccato: una riga in ognuno dei registri.
	for _, tab := range tabelleRegistri {
		if err := p.s.DB.Lettura.QueryRow(`SELECT count(*) FROM ` + tab).Scan(&n); err != nil || n != 1 {
			t.Errorf("sorgente toccata: %s %d %v", tab, n, err)
		}
	}
}

// Una cartella che c'è ed è vuota (per esempio preparata a mano) va bene; una con dentro qualcosa, anche solo un
// file nascosto, un collegamento al posto della cartella, un file, o una cartella sopra che non c'è sono rifiutati
// prima di decifrare, e niente cambia.
func TestRipristinoCartellaDestinazione(t *testing.T) {
	_, archivio, chiave := archivioDaRipristinare(t)
	o := clock.NuovoFinto(oraRipristino)
	padre := t.TempDir()
	vuota := filepath.Join(padre, "vuota")
	if err := os.Mkdir(vuota, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Ripristina(context.Background(), Ripristino{Archivio: archivio, Chiave: chiave, Cartella: vuota, Orologio: o}); err != nil {
		t.Fatalf("cartella vuota: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vuota, "gestrip.db")); err != nil {
		t.Fatal(err)
	}
	piena := filepath.Join(padre, "piena")
	if err := os.Mkdir(piena, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(piena, ".nascosto"), []byte("dati di prima"), 0o600); err != nil {
		t.Fatal(err)
	}
	collegamento := filepath.Join(padre, "collegamento")
	if err := os.Symlink(filepath.Join(padre, "altrove"), collegamento); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(padre, "altrove"), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(padre, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for nome, c := range map[string]struct {
		cartella string
		atteso   error
	}{
		"piena":             {piena, ErrCartellaNonVuota},
		"sopra i dati":      {vuota, ErrCartellaNonVuota}, // ora ha il ripristino di prima
		"collegamento":      {collegamento, ErrCartellaRipristino},
		"file":              {file, ErrCartellaRipristino},
		"padre che manca":   {filepath.Join(padre, "manca", "dati"), ErrCartellaRipristino},
		"percorso vuoto":    {"", ErrCartellaRipristino},
		"radice":            {"/", ErrCartellaNonVuota},
		"percorso relativo": {"dati", ErrCartellaRipristino},
	} {
		_, err := Ripristina(context.Background(), Ripristino{Archivio: archivio, Chiave: chiave, Cartella: c.cartella, Orologio: o})
		if !errors.Is(err, c.atteso) {
			t.Errorf("%s: %v", nome, err)
		}
	}
	if b, err := os.ReadFile(filepath.Join(piena, ".nascosto")); err != nil || string(b) != "dati di prima" {
		t.Errorf("cartella piena toccata: %v", err)
	}
	if v, _ := os.ReadDir(filepath.Join(padre, "altrove")); len(v) != 0 {
		t.Errorf("scritto dietro il collegamento: %v", v)
	}
	senzaRestiDelRipristino(t, padre)
}

// Ogni controllo della verifica della 121 che fallisce (chiave sbagliata, archivio toccato, integrità del database
// estratto) ferma il ripristino senza lasciare né la cartella dei dati né la temporanea; così anche lo stop e una
// chiave che non è una chiave age.
func TestRipristinoFallitoNonLasciaNiente(t *testing.T) {
	p, archivio, chiave := archivioDaRipristinare(t)
	o := clock.NuovoFinto(oraRipristino)
	toccato := filepath.Join(t.TempDir(), "toccato.tar.age")
	b, err := os.ReadFile(archivio)
	if err != nil {
		t.Fatal(err)
	}
	b = slices.Clone(b)
	b[len(b)/2] ^= 1
	if err := os.WriteFile(toccato, b, 0o600); err != nil {
		t.Fatal(err)
	}
	altra := conChiaveAltra(t)
	fermo, ferma := context.WithCancel(context.Background())
	ferma()
	for nome, c := range map[string]struct {
		ctx      context.Context
		archivio string
		chiave   []byte
		rovina   bool
	}{
		"chiave di un'altra macchina": {context.Background(), archivio, altra, false},
		"chiave non age":              {context.Background(), archivio, []byte("non una chiave"), false},
		"archivio toccato":            {context.Background(), toccato, chiave, false},
		"archivio che non c'è":        {context.Background(), filepath.Join(p.dir, "manca.tar.age"), chiave, false},
		"database rovinato":           {context.Background(), archivio, chiave, true},
		"fermato":                     {fermo, archivio, chiave, false},
	} {
		padre := t.TempDir()
		dest := filepath.Join(padre, "dati")
		if c.rovina {
			dopoEstrazione = func(f string) {
				if err := os.WriteFile(f, []byte("non un database"), 0o600); err != nil {
					t.Error(err)
				}
			}
		}
		_, err := Ripristina(c.ctx, Ripristino{Archivio: c.archivio, Chiave: c.chiave, Cartella: dest, Orologio: o})
		dopoEstrazione = nil
		if err == nil {
			t.Errorf("%s: riuscito", nome)
		}
		if _, err := os.Lstat(dest); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: la cartella dei dati c'è: %v", nome, err)
		}
		senzaRestiDelRipristino(t, padre)
	}
}

// conChiaveAltra è il contenuto di un file della chiave dei backup di un'altra macchina (inventata).
func conChiaveAltra(t *testing.T) []byte {
	t.Helper()
	dir := t.TempDir()
	if _, err := CreaChiave(dir, istanteChiave); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(FileChiave(dir))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Lo spazio: il ripristino chiede nel volume della destinazione i byte dell'archivio più MargineSpazio, prima di
// decifrare.
func TestRipristinoSpazio(t *testing.T) {
	_, archivio, chiave := archivioDaRipristinare(t)
	fi, err := os.Stat(archivio)
	if err != nil {
		t.Fatal(err)
	}
	spazioLibero = func(string) (uint64, error) { return uint64(fi.Size()) + MargineSpazio - 1, nil }
	padre := t.TempDir()
	_, err = Ripristina(context.Background(), Ripristino{Archivio: archivio, Chiave: chiave, Cartella: filepath.Join(padre, "dati"),
		Orologio: clock.NuovoFinto(oraRipristino)})
	if !errors.Is(err, ErrSpazio) {
		t.Errorf("spazio: %v", err)
	}
	senzaRestiDelRipristino(t, padre)
}

// Un file arrivato nella destinazione vuota fra il controllo e la rinomina: rename(2) non la sostituisce e niente
// si perde, né lì né nella cartella preparata.
func TestPubblicaCartellaMaiSopraDati(t *testing.T) {
	padre := t.TempDir()
	tmp, dest := filepath.Join(padre, ".ripristino-1"), filepath.Join(padre, "dati")
	for _, c := range []string{tmp, dest} {
		if err := os.Mkdir(c, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(c, "f"), []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := pubblicaCartella(tmp, dest); !errors.Is(err, ErrCartellaNonVuota) {
		t.Fatalf("errore %v", err)
	}
	for _, c := range []string{tmp, dest} {
		if b, err := os.ReadFile(filepath.Join(c, "f")); err != nil || string(b) != c {
			t.Errorf("%s toccata: %v", c, err)
		}
	}
	// Una destinazione che non c'è: nasce dalla rinomina.
	nuova := filepath.Join(padre, "nuova")
	if err := pubblicaCartella(tmp, nuova); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(nuova, "f")); err != nil || string(b) != tmp {
		t.Errorf("nuova: %v", err)
	}
}

// Una cartella con un solo file nascosto è rifiutata prima di aprire l'archivio, di guardare lo spazio e di
// decifrare: qui l'archivio non c'è e lo spazio non si deve chiedere, quindi solo il controllo della cartella può
// dare ErrCartellaNonVuota.
func TestRipristinoCartellaPienaPrimaDiDecifrare(t *testing.T) {
	p := nuovaProva(t)
	conChiave(t, p)
	chiave, err := os.ReadFile(FileChiave(p.dir))
	if err != nil {
		t.Fatal(err)
	}
	spazioLibero = func(string) (uint64, error) {
		t.Error("spazio chiesto con la cartella piena")
		return 1 << 50, nil
	}
	padre := t.TempDir()
	piena := filepath.Join(padre, "piena")
	if err := os.Mkdir(piena, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(piena, ".solo"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Ripristina(context.Background(), Ripristino{Archivio: filepath.Join(padre, "manca.tar.age"), Chiave: chiave, Cartella: piena,
		Orologio: clock.NuovoFinto(oraRipristino)})
	if !errors.Is(err, ErrCartellaNonVuota) {
		t.Errorf("errore %v", err)
	}
	senzaRestiDelRipristino(t, padre)
}

// Il ripristino fa anche i controlli del database della 121 (controllaDatabase): un archivio coerente (inventario
// giusto, database integro che le migrazioni accettano) ma con un conteggio diverso da snapshot.json è rifiutato,
// e la cartella dei dati non nasce.
func TestRipristinoConteggiDiversi(t *testing.T) {
	p, r, v := provaConChiave(t)
	chiave, err := os.ReadFile(FileChiave(p.dir))
	if err != nil {
		t.Fatal(err)
	}
	v = cambiaFoto(t, v, func(f *Fotografia) { f.Conteggi["cancellazioni_media"]++ })
	archivio := filepath.Join(t.TempDir(), "gestrip-backup-20261125T023000Z.tar.age")
	if err := os.WriteFile(archivio, cifraPer(t, r, ricomponi(t, v)), 0o600); err != nil {
		t.Fatal(err)
	}
	padre := t.TempDir()
	dest := filepath.Join(padre, "dati")
	_, err = Ripristina(context.Background(), Ripristino{Archivio: archivio, Chiave: chiave, Cartella: dest, Orologio: clock.NuovoFinto(oraRipristino)})
	if !errors.Is(err, ErrFotografia) || !strings.Contains(err.Error(), "cancellazioni_media") {
		t.Errorf("errore %v", err)
	}
	if _, err := os.Lstat(dest); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("cartella dei dati nata: %v", err)
	}
	senzaRestiDelRipristino(t, padre)
}
