package importer

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
)

// inventario: OPS-07
// I resti di un import interrotto senza pulizia (SIGKILL, corrente) nella cartella della
// destinazione fermano l'import prima di qualsiasi lavoro: l'import non li cancella da solo.
func TestAvanziDiUnImportInterrotto(t *testing.T) {
	dir, o := preparaImport(t, nil)
	resti := filepath.Join(dir, ".gestrip-import-123456")
	if err := os.Mkdir(resti, 0o700); err != nil {
		t.Fatal(err)
	}
	scriviFile(t, filepath.Join(resti, "sorgente.db"))
	prima := fileDi(t, dir)
	_, err := Importa(context.Background(), o)
	if err == nil || !strings.Contains(err.Error(), "resti di un import interrotto") || !strings.Contains(err.Error(), resti) {
		t.Errorf("errore %v", err)
	}
	if dopo := fileDi(t, dir); !reflect.DeepEqual(prima, dopo) {
		t.Errorf("file prima %v, dopo %v", prima, dopo)
	}
	if _, err := os.Stat(filepath.Join(resti, "sorgente.db")); err != nil {
		t.Errorf("i resti sono stati toccati: %v", err)
	}
}

// cartelleConMetacaratteri sono nomi di cartella con i caratteri speciali di filepath.Glob, ognuno
// con una cartella vicina che il nome, letto come schema, prenderebbe al suo posto.
var cartelleConMetacaratteri = []struct{ nome, vicina string }{
	{"negozio[", "negozio1"},
	{"negozio[1]", "negozio1"},
	{"negozio*", "negozio-vicino"},
	{"negozio?", "negozioX"},
	{`nego\zio`, "negozio"},
}

// I resti si cercano nella cartella della destinazione com'è scritta, anche con i caratteri
// speciali di uno schema: quelli dentro fermano l'import, quelli di una cartella vicina no.
func TestRestiNellaCartellaConMetacaratteri(t *testing.T) {
	for _, c := range cartelleConMetacaratteri {
		t.Run(c.nome, func(t *testing.T) {
			t.Run("resti dentro", func(t *testing.T) {
				dir, o := preparaImport(t, nil)
				d := filepath.Join(dir, c.nome)
				resti := filepath.Join(d, ".gestrip-import-123456")
				if err := os.MkdirAll(resti, 0o700); err != nil {
					t.Fatal(err)
				}
				o.Destinazione, o.Report = filepath.Join(d, "gestrip.db"), filepath.Join(d, "report.json")
				_, err := Importa(context.Background(), o)
				if err == nil || !strings.Contains(err.Error(), "resti di un import interrotto in "+resti) {
					t.Errorf("errore %v", err)
				}
				if got := fileDi(t, d); !reflect.DeepEqual(got, []string{".gestrip-import-123456"}) {
					t.Errorf("nella cartella: %v", got)
				}
			})
			t.Run("resti nella cartella vicina", func(t *testing.T) {
				dir, o := preparaImport(t, nil)
				d := filepath.Join(dir, c.nome)
				if err := os.Mkdir(d, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(dir, c.vicina, ".gestrip-import-123456"), 0o700); err != nil {
					t.Fatal(err)
				}
				o.Destinazione, o.Report = filepath.Join(d, "gestrip.db"), filepath.Join(d, "report.json")
				if es, err := Importa(context.Background(), o); err != nil || es.Verdetto != Riconciliato {
					t.Errorf("esito %+v %v", es, err)
				}
			})
		})
	}
}

// RestiDiImport con una cartella che non c'è non trova resti (come prima, con lo schema); con una
// cartella che non si può elencare lo dice, invece di dire che i resti non ci sono.
func TestRestiDiImportCartellaAssenteOIlleggibile(t *testing.T) {
	t.Run("assente", func(t *testing.T) {
		resti, err := RestiDiImport(filepath.Join(t.TempDir(), "manca", "gestrip.db"))
		if err != nil || resti != nil {
			t.Errorf("resti %v, errore %v", resti, err)
		}
	})
	t.Run("non si elenca", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root elenca anche senza il permesso di lettura")
		}
		d := filepath.Join(t.TempDir(), "chiusa")
		if err := os.MkdirAll(filepath.Join(d, ".gestrip-import-123"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(d, 0o300); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(d, 0o700) })
		resti, err := RestiDiImport(filepath.Join(d, "gestrip.db"))
		if !errors.Is(err, fs.ErrPermission) {
			t.Errorf("resti %v, errore %v", resti, err)
		}
	})
}

// L'import non sovrascrive mai: una destinazione o un report comparsi durante l'import restano
// quelli di chi li ha scritti e niente si attiva; con la cartella del report non scrivibile la
// destinazione non resta attivata.
func TestNonSovrascriveMai(t *testing.T) {
	altrui := []byte("file di un altro")
	t.Run("destinazione creata durante", func(t *testing.T) {
		dir, o := preparaImport(t, nil)
		o.DopoTabella = func(_ context.Context, tab string) {
			if tab == "buste" {
				if err := os.WriteFile(o.Destinazione, altrui, 0o600); err != nil {
					panic(err)
				}
			}
		}
		if _, err := Importa(context.Background(), o); err == nil {
			t.Error("nessun errore")
		}
		if b, err := os.ReadFile(o.Destinazione); err != nil || !bytes.Equal(b, altrui) {
			t.Errorf("destinazione altrui cambiata: %q %v", b, err)
		}
		if got := fileDi(t, dir); !reflect.DeepEqual(got, []string{"gestrip.db", "standalone.db"}) {
			t.Errorf("nella cartella: %v", got)
		}
	})
	t.Run("report creato durante", func(t *testing.T) {
		dir, o := preparaImport(t, nil)
		o.DopoTabella = func(_ context.Context, tab string) {
			if tab == "buste" {
				if err := os.WriteFile(o.Report, altrui, 0o600); err != nil {
					panic(err)
				}
			}
		}
		if _, err := Importa(context.Background(), o); err == nil {
			t.Error("nessun errore")
		}
		if b, err := os.ReadFile(o.Report); err != nil || !bytes.Equal(b, altrui) {
			t.Errorf("report altrui cambiato: %q %v", b, err)
		}
		if got := fileDi(t, dir); !reflect.DeepEqual(got, []string{"report.json", "standalone.db"}) {
			t.Errorf("nella cartella: %v (la destinazione deve essere tolta)", got)
		}
	})
	t.Run("cartella del report non scrivibile", func(t *testing.T) {
		dir, o := preparaImport(t, nil)
		cr := filepath.Join(dir, "report")
		if err := os.Mkdir(cr, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(cr, 0o700) }) // per la pulizia di TempDir
		o.Report = filepath.Join(cr, "report.json")
		o.DopoTabella = func(_ context.Context, tab string) {
			if tab == "buste" {
				if err := os.Chmod(cr, 0o500); err != nil {
					panic(err)
				}
			}
		}
		if _, err := Importa(context.Background(), o); err == nil {
			t.Error("nessun errore")
		}
		if got := fileDi(t, dir); !reflect.DeepEqual(got, []string{"report", "standalone.db"}) {
			t.Errorf("nella cartella: %v", got)
		}
		if got := fileDi(t, cr); len(got) != 0 {
			t.Errorf("nella cartella del report: %v", got)
		}
	})
}

// inventario: OPS-07
// Uno stop che arriva dopo il verdetto, subito prima dell'attivazione, è un'interruzione: niente
// attivato, niente report, nessun temporaneo.
func TestStopPrimaDellAttivazione(t *testing.T) {
	dir, o := preparaImport(t, nil)
	ctx, ferma := context.WithCancel(context.Background())
	defer ferma()
	o.PrimaDiAttivare = func(context.Context) { ferma() }
	_, err := Importa(ctx, o)
	if !errors.Is(err, ErrInterrotto) {
		t.Errorf("errore %v, attesa l'interruzione", err)
	}
	if got := fileDi(t, dir); !reflect.DeepEqual(got, []string{"standalone.db"}) {
		t.Errorf("nella cartella: %v", got)
	}
}

// orologioCheAvanza va avanti di un secondo a ogni lettura.
type orologioCheAvanza struct {
	mu sync.Mutex
	t  time.Time
}

func (o *orologioCheAvanza) Adesso() time.Time {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.t = o.t.Add(time.Second)
	return o.t
}

// Un solo istante dell'import: lo stesso nel report e in import_standalone.importato_il, anche
// con un orologio che va avanti durante l'import.
func TestUnSoloIstanteDellImport(t *testing.T) {
	_, o := preparaImport(t, nil)
	o.Orologio = &orologioCheAvanza{t: istanteImport}
	importa(t, o)
	r, _ := leggiReport(t, o.Report)
	var il string
	if err := apriLettura(t, o.Destinazione).QueryRow(`SELECT importato_il FROM import_standalone`).Scan(&il); err != nil {
		t.Fatal(err)
	}
	if il != r.Istante {
		t.Errorf("importato_il %s, istante del report %s", il, r.Istante)
	}
}

// inventario: OPS-01
// Una chiave di settings che usa solo il Go (lo stato della conservazione dell'audit) nella
// sorgente blocca: il report dice il nome della chiave, mai il valore.
func TestChiaviSoloGoNellaSorgente(t *testing.T) {
	dir, o := preparaImport(t, func(c *sql.DB) {
		esegui(c, `INSERT INTO settings (key, value, updated_at) VALUES (?, 'valore-da-non-copiare', '2026-01-01T00:00:00.000000Z')`, audit.ChiaveConservazione)
	})
	r := controllaBloccato(t, dir, o, importa(t, o))
	var trovata bool
	for _, v := range r.Differenze {
		if v.Codice == "settings/chiave_solo_go" && v.Tabella == "settings" && v.Chiave == audit.ChiaveConservazione && v.Id != nil {
			trovata = true
		}
	}
	if !trovata {
		t.Errorf("differenze %+v", r.Differenze)
	}
	if _, b := leggiReport(t, o.Report); bytes.Contains(b, []byte("valore-da-non-copiare")) {
		t.Error("il report contiene il valore")
	}
}

// ChiaviSoloGo nomina tutte le chiavi di impostazioni che il Go scrive da sé, e solo i file che
// conosce toccano impostazioni: un file nuovo che la legge o la scrive deve aggiungere qui la sua
// chiave (o dire che è dello standalone).
func TestChiaviSoloGoComplete(t *testing.T) {
	if ChiaviSoloGo[audit.ChiaveConservazione] == "" {
		t.Errorf("manca %s", audit.ChiaveConservazione)
	}
	conosciuti := []string{"internal/audit/conservazione.go"}
	re := regexp.MustCompile(`(?i)(FROM|INTO|UPDATE)\s+impostazioni\b`)
	var trovati []string
	radice := filepath.Join("..", "..")
	for _, d := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(radice, d), func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.Contains(p, "importer") {
				return err
			}
			b, err := os.ReadFile(p)
			if err == nil && re.Match(b) {
				rel, _ := filepath.Rel(radice, p)
				trovati = append(trovati, filepath.ToSlash(rel))
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	slices.Sort(trovati)
	if !reflect.DeepEqual(trovati, conosciuti) {
		t.Errorf("file che toccano impostazioni %v, conosciuti %v: aggiorna ChiaviSoloGo", trovati, conosciuti)
	}
}

// inventario: OPS-01
// I file accanto alla sorgente si ricontrollano dopo la copia: un -wal comparso mentre si copiava
// ferma l'import, senza file.
func TestFileAccantoDopoLaCopia(t *testing.T) {
	dir, o := preparaImport(t, nil)
	dopoLaCopia = func() { scriviFile(t, o.Sorgente+"-wal") }
	t.Cleanup(func() { dopoLaCopia = nil })
	_, err := Importa(context.Background(), o)
	if err == nil || !strings.Contains(err.Error(), "file -wal") {
		t.Errorf("errore %v", err)
	}
	if got := fileDi(t, dir); !reflect.DeepEqual(got, []string{"standalone.db", "standalone.db-wal"}) {
		t.Errorf("nella cartella: %v", got)
	}
}

// inventario: OPS-01
// Destinazione e report non possono essere la sorgente né uno dei suoi file accanto.
func TestDestinazioneNonELaSorgente(t *testing.T) {
	for _, c := range []struct {
		nome   string
		cambia func(o *Opzioni)
	}{
		{"destinazione = -wal della sorgente", func(o *Opzioni) { o.Destinazione = o.Sorgente + "-wal" }},
		{"destinazione = -journal con un giro nel percorso", func(o *Opzioni) {
			// Scritto a mano: filepath.Join pulirebbe già il percorso.
			o.Destinazione = filepath.Dir(o.Sorgente) + "/./x/../" + filepath.Base(o.Sorgente) + "-journal"
		}},
		{"report = -shm della sorgente", func(o *Opzioni) { o.Report = o.Sorgente + "-shm" }},
	} {
		t.Run(c.nome, func(t *testing.T) {
			dir, o := preparaImport(t, nil)
			if err := os.Mkdir(filepath.Join(dir, "x"), 0o700); err != nil {
				t.Fatal(err)
			}
			c.cambia(&o)
			prima := fileDi(t, dir)
			if _, err := Importa(context.Background(), o); err == nil || !strings.Contains(err.Error(), "sorgente") {
				t.Errorf("errore %v", err)
			}
			if dopo := fileDi(t, dir); !reflect.DeepEqual(prima, dopo) {
				t.Errorf("file prima %v, dopo %v", prima, dopo)
			}
		})
	}
}

// Il database costruito si ricontrolla prima di attivarlo: se la verifica d'integrità fallisce
// (qui il file rovinato apposta), niente si attiva e il report dice bloccato.
func TestVerificaFinaleDelDatabase(t *testing.T) {
	dir, o := preparaImport(t, nil)
	primaDellaVerifica = func(p string) {
		f, err := os.OpenFile(p, os.O_WRONLY, 0)
		if err != nil {
			panic(err)
		}
		fi, err := f.Stat()
		if err != nil {
			panic(err)
		}
		// Il contenuto dell'ultima pagina, lasciando la sua intestazione: integrity_check risponde, e
		// non dice ok.
		if _, err := f.WriteAt(bytes.Repeat([]byte{0x5a}, 3000), fi.Size()-3000); err != nil {
			panic(err)
		}
		if err := f.Close(); err != nil {
			panic(err)
		}
	}
	t.Cleanup(func() { primaDellaVerifica = nil })
	es, err := Importa(context.Background(), o)
	r := controllaBloccato(t, dir, o, es)
	if err != nil || !ha(r.Differenze, "destinazione/integrita", "", 0, "") {
		t.Errorf("errore %v, differenze %+v", err, r.Differenze)
	}
}
