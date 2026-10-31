package importer

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
)

// I rifiuti delle opzioni e dei percorsi, prima di aprire la sorgente, non lasciano nessun file e
// non nominano la sorgente: dimensione negativa, sorgente, destinazione o report non dati, niente
// orologio, SHA-256 di 31 byte, sorgente che non c'è, cartella della destinazione o del report che
// non c'è, destinazione uguale al report.
func TestRifiutiDelleOpzioni(t *testing.T) {
	casi := []struct {
		nome   string
		cambia func(t *testing.T, dir string, o *Opzioni)
		errore string
	}{
		{"dimensione negativa", func(t *testing.T, dir string, o *Opzioni) { o.Dimensione = -1 }, "non può essere negativa"},
		{"senza sorgente", func(t *testing.T, dir string, o *Opzioni) { o.Sorgente = "" }, "servono -sorgente, -destinazione e -report"},
		{"senza destinazione", func(t *testing.T, dir string, o *Opzioni) { o.Destinazione = "" }, "servono -sorgente, -destinazione e -report"},
		{"senza report", func(t *testing.T, dir string, o *Opzioni) { o.Report = "" }, "servono -sorgente, -destinazione e -report"},
		{"senza orologio", func(t *testing.T, dir string, o *Opzioni) { o.Orologio = nil }, "manca l'orologio"},
		{"sha256 di 31 byte", func(t *testing.T, dir string, o *Opzioni) { o.Sha256 = o.Sha256[:62] }, "64 cifre"},
		{"sorgente che non c'è", func(t *testing.T, dir string, o *Opzioni) { o.Sorgente = filepath.Join(dir, "sorgente-assente.db") }, "sorgente: "},
		{"cartella della destinazione assente", func(t *testing.T, dir string, o *Opzioni) {
			o.Destinazione = filepath.Join(dir, "assente", "gestrip.db")
		}, "la cartella del database"},
		{"cartella del report assente", func(t *testing.T, dir string, o *Opzioni) {
			o.Report = filepath.Join(dir, "assente", "report.json")
		}, "la cartella del report"},
		{"destinazione e report uguali", func(t *testing.T, dir string, o *Opzioni) { o.Report = o.Destinazione }, "due file diversi"},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			dir, o := preparaImport(t, nil)
			c.cambia(t, dir, &o)
			prima := fileDi(t, dir)
			es, err := Importa(context.Background(), o)
			if err == nil || !strings.Contains(err.Error(), c.errore) {
				t.Errorf("errore %v, atteso con %q", err, c.errore)
			}
			if err != nil && strings.Contains(err.Error(), "sorgente-assente") {
				t.Errorf("l'errore nomina la sorgente: %v", err)
			}
			if es != (Esito{}) {
				t.Errorf("esito %+v", es)
			}
			if dopo := fileDi(t, dir); !reflect.DeepEqual(prima, dopo) {
				t.Errorf("file prima %v, dopo %v", prima, dopo)
			}
		})
	}
}

// La sorgente non è a riposo anche con un link pendente al posto del -wal, e un file accanto che
// non si riesce a guardare (qui un nome troppo lungo per il file system) è un rifiuto, non un via
// libera. Il messaggio dice quale dei due casi.
func TestFileAccantoLinkEIlleggibili(t *testing.T) {
	t.Run("link pendente", func(t *testing.T) {
		dir, o := preparaImport(t, nil)
		if err := os.Symlink("altrove", o.Sorgente+"-wal"); err != nil {
			t.Fatal(err)
		}
		prima := fileDi(t, dir)
		_, err := Importa(context.Background(), o)
		if err == nil || !strings.Contains(err.Error(), "file -wal: il database non è a riposo") {
			t.Errorf("errore %v", err)
		}
		if dopo := fileDi(t, dir); !reflect.DeepEqual(prima, dopo) {
			t.Errorf("file prima %v, dopo %v", prima, dopo)
		}
	})
	t.Run("non si legge", func(t *testing.T) {
		dir, o := preparaImport(t, nil)
		// 252 caratteri: il nome della sorgente c'è, quelli con -wal o -journal sono troppo lunghi.
		lungo := filepath.Join(dir, strings.Repeat("s", 252))
		if err := os.Rename(o.Sorgente, lungo); err != nil {
			t.Fatal(err)
		}
		o.Sorgente = lungo
		prima := fileDi(t, dir)
		_, err := Importa(context.Background(), o)
		if err == nil || !strings.Contains(err.Error(), "accanto alla sorgente non si legge") {
			t.Errorf("errore %v", err)
		}
		if err != nil && strings.Contains(err.Error(), strings.Repeat("s", 50)) {
			t.Errorf("l'errore nomina la sorgente: %v", err)
		}
		if dopo := fileDi(t, dir); !reflect.DeepEqual(prima, dopo) {
			t.Errorf("file prima %v, dopo %v", prima, dopo)
		}
	})
}

// I controlli che non chiedono di leggere la sorgente vengono prima di copiarla: con un -wal
// accanto, con il contesto già chiuso o con una destinazione che non si può guardare (nome troppo
// lungo), la sorgente non si copia nemmeno.
func TestRifiutoPrimaDellaCopia(t *testing.T) {
	for _, c := range []struct {
		nome    string
		prepara func(t *testing.T, o *Opzioni) context.Context
		errore  string
	}{
		{"wal accanto", func(t *testing.T, o *Opzioni) context.Context {
			scriviFile(t, o.Sorgente+"-wal")
			return context.Background()
		}, "file -wal"},
		{"contesto già chiuso", func(t *testing.T, o *Opzioni) context.Context {
			ctx, ferma := context.WithCancel(context.Background())
			ferma()
			return ctx
		}, "import interrotto"},
		{"destinazione con un nome troppo lungo", func(t *testing.T, o *Opzioni) context.Context {
			o.Destinazione = filepath.Join(filepath.Dir(o.Destinazione), strings.Repeat("d", 300)+".db")
			return context.Background()
		}, "file name too long"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			dir, o := preparaImport(t, nil)
			ctx := c.prepara(t, &o)
			copiata := false
			dopoLaCopia = func() { copiata = true }
			t.Cleanup(func() { dopoLaCopia = nil })
			prima := fileDi(t, dir)
			_, err := Importa(ctx, o)
			if err == nil || !strings.Contains(err.Error(), c.errore) {
				t.Errorf("errore %v, atteso con %q", err, c.errore)
			}
			if copiata {
				t.Error("la sorgente è stata copiata prima del rifiuto")
			}
			if dopo := fileDi(t, dir); !reflect.DeepEqual(prima, dopo) {
				t.Errorf("file prima %v, dopo %v", prima, dopo)
			}
		})
	}
}

// Durante l'import la copia della sorgente è un file 0600 nella cartella di lavoro 0700, e il
// report, prima di prendere il suo nome, è un temporaneo 0600 nella cartella del report.
func TestPermessiDeiTemporanei(t *testing.T) {
	dir, o := preparaImport(t, nil)
	dirRep := filepath.Join(t.TempDir(), "rapporti")
	if err := os.Mkdir(dirRep, 0o700); err != nil {
		t.Fatal(err)
	}
	o.Report = filepath.Join(dirRep, "report.json")
	dopoLaCopia = func() {
		copie, err := filepath.Glob(filepath.Join(dir, prefissoLavoro+"*", "sorgente.db"))
		if err != nil || len(copie) != 1 {
			t.Errorf("copia della sorgente: %v %v", copie, err)
			return
		}
		if fi, err := os.Stat(copie[0]); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("copia della sorgente: %v %v", fi, err)
		}
		if fi, err := os.Stat(filepath.Dir(copie[0])); err != nil || fi.Mode().Perm() != 0o700 {
			t.Errorf("cartella di lavoro: %v %v", fi, err)
		}
	}
	t.Cleanup(func() { dopoLaCopia = nil })
	visto := false
	o.PrimaDiAttivare = func(context.Context) {
		visto = true
		tmp, err := filepath.Glob(filepath.Join(dirRep, ".report.json.*.tmp"))
		if err != nil || len(tmp) != 1 {
			t.Errorf("temporaneo del report: %v %v", tmp, err)
			return
		}
		if fi, err := os.Stat(tmp[0]); err != nil || fi.Mode().Perm() != 0o600 || fi.Size() == 0 {
			t.Errorf("temporaneo del report: %v %v", fi, err)
		}
	}
	if es := importa(t, o); es.Verdetto != Riconciliato || !visto {
		t.Fatalf("verdetto %s, aggancio visto %v", es.Verdetto, visto)
	}
	if got := fileDi(t, dirRep); !reflect.DeepEqual(got, []string{"report.json"}) {
		t.Errorf("nella cartella del report: %v", got)
	}
}

// orologioInUnFuso dà sempre lo stesso istante, nel suo fuso (clock.Finto lo porterebbe in UTC).
type orologioInUnFuso struct{ t time.Time }

func (o orologioInUnFuso) Adesso() time.Time { return o.t }

// L'istante dell'import è in UTC anche con un orologio in un altro fuso: nel report e nel segno.
func TestIstanteDellImportInUTC(t *testing.T) {
	_, o := preparaImport(t, nil)
	o.Orologio = orologioInUnFuso{istanteImport.In(time.FixedZone("UTC+2", 2*3600))}
	importa(t, o)
	r, _ := leggiReport(t, o.Report)
	var il string
	if err := apriLettura(t, o.Destinazione).QueryRow(`SELECT importato_il FROM import_standalone`).Scan(&il); err != nil {
		t.Fatal(err)
	}
	if r.Istante != "2026-10-30T21:00:00.000000Z" || il != r.Istante {
		t.Errorf("istante del report %s, importato_il %s", r.Istante, il)
	}
}

// Una cartella della destinazione con ?, # o % nel nome: la copia della sorgente e il database
// nuovo stanno lì dentro e si aprono lo stesso (il nome va nell'URI di SQLite codificato), e
// accanto alla cartella non nasce niente.
func TestCartellaConCaratteriDellURI(t *testing.T) {
	for _, c := range []struct{ caso, nome string }{
		{"punto di domanda", "con?punto"}, {"cancelletto", "con#cancelletto"},
		{"percento", "con%41percento"}, {"percento solo", "con%percento"},
	} {
		nome := c.nome
		t.Run(c.caso, func(t *testing.T) {
			dir, o := preparaImport(t, nil)
			accanto := append(fileDi(t, dir), nome)
			slices.Sort(accanto)
			d := filepath.Join(dir, nome)
			if err := os.Mkdir(d, 0o700); err != nil {
				t.Fatal(err)
			}
			o.Destinazione, o.Report = filepath.Join(d, "gestrip.db"), filepath.Join(d, "report.json")
			es, err := Importa(context.Background(), o)
			if err != nil || es.Verdetto != Riconciliato {
				r, _ := leggiReport(t, o.Report)
				t.Fatalf("esito %+v %v, differenze %v", es, err, r.Differenze)
			}
			if got := fileDi(t, d); !reflect.DeepEqual(got, []string{"gestrip.db", "media", "report.json"}) {
				t.Errorf("nella cartella: %v", got)
			}
			if got := fileDi(t, dir); !reflect.DeepEqual(got, accanto) {
				t.Errorf("accanto alla cartella: %v, attesi %v", got, accanto)
			}
		})
	}
}

// La verifica finale guarda anche le chiavi esterne del database finito: una riga orfana (qui
// scritta apposta prima della verifica) blocca con il numero di righe.
func TestVerificaFinaleChiaviEsterne(t *testing.T) {
	dir, o := preparaImport(t, nil)
	primaDellaVerifica = func(p string) {
		c, err := sql.Open("sqlite", "file:"+p)
		if err != nil {
			panic(err)
		}
		esegui(c, `UPDATE messaggi_buste SET busta_id = 99`)
		if err := c.Close(); err != nil {
			panic(err)
		}
	}
	t.Cleanup(func() { primaDellaVerifica = nil })
	es, err := Importa(context.Background(), o)
	r := controllaBloccato(t, dir, o, es)
	if err != nil || !reflect.DeepEqual(r.Differenze, []Voce{{Codice: "destinazione/chiavi_esterne", Righe: ptr(1)}}) {
		t.Errorf("errore %v, differenze %+v", err, r.Differenze)
	}
}

// apriSorgente e verificaNuovo codificano %, ? e # del percorso nell'URI di SQLite: aprono
// proprio quel file.
func TestPercorsiConCaratteriDellURI(t *testing.T) {
	for _, c := range []struct{ caso, nome string }{{"punto di domanda", "a?b"}, {"cancelletto", "a#b"}, {"percento", "a%41b"}} {
		t.Run(c.caso, func(t *testing.T) {
			d := filepath.Join(t.TempDir(), c.nome)
			if err := os.Mkdir(d, 0o700); err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(d, "prova.db")
			r := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23")
			w, err := sql.Open("sqlite", "file:"+r.Replace(p))
			if err != nil {
				t.Fatal(err)
			}
			esegui(w, `CREATE TABLE t (x INTEGER)`)
			esegui(w, `INSERT INTO t VALUES (7)`)
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			s, err := apriSorgente(p)
			if err != nil {
				t.Fatal(err)
			}
			var x int
			if err := s.QueryRow(`SELECT x FROM t`).Scan(&x); err != nil || x != 7 {
				t.Errorf("apriSorgente: %d %v", x, err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			var rep Report
			if err := verificaNuovo(context.Background(), p, &rep); err != nil || len(rep.Differenze) != 0 {
				t.Errorf("verificaNuovo: %v %+v", err, rep.Differenze)
			}
			if got := fileDi(t, d); !reflect.DeepEqual(got, []string{"prova.db"}) {
				t.Errorf("nella cartella: %v", got)
			}
		})
	}
}

// differenzeDi dà le differenze del report come JSON compatto, per i messaggi.
func differenzeDi(r Report) string {
	b, _ := json.Marshal(r.Differenze)
	return string(b)
}

// Un nome di tabella che comincia per «sqlite» senza essere interno a SQLite (sqlite_…) è una
// tabella sconosciuta come le altre.
func TestTabellaCheSembraInterna(t *testing.T) {
	dir, o := preparaImport(t, func(c *sql.DB) { esegui(c, `CREATE TABLE sqliteXappunti (testo TEXT)`) })
	r := controllaBloccato(t, dir, o, importa(t, o))
	if !reflect.DeepEqual(r.Differenze, []Voce{{Codice: "schema/tabella_sconosciuta", Tabella: "sqliteXappunti", Righe: ptr(0)}}) {
		t.Errorf("differenze %s", differenzeDi(r))
	}
}

// Un hash facoltativo NULL è un BLOB accettabile: passa, e nel Go resta NULL.
func TestHashFacoltativoNullo(t *testing.T) {
	_, o := preparaImport(t, func(c *sql.DB) { esegui(c, `UPDATE repairs SET legacy_public_code_hash = NULL WHERE id = 2`) })
	if es := importa(t, o); es.Verdetto != Riconciliato {
		r, _ := leggiReport(t, o.Report)
		t.Fatalf("verdetto %s, differenze %s", es.Verdetto, differenzeDi(r))
	}
	var tipo string
	if err := apriLettura(t, o.Destinazione).QueryRow(`SELECT typeof(hash_codice_flask) FROM buste WHERE id = 2`).Scan(&tipo); err != nil || tipo != "null" {
		t.Errorf("hash_codice_flask: %q %v", tipo, err)
	}
}

// Una riga con più problemi è una differenza sola (la prima trovata), e non si prova a scriverla:
// il conteggio la dice mancante una volta.
func TestUnaDifferenzaPerRiga(t *testing.T) {
	dir, o := preparaImport(t, func(c *sql.DB) {
		esegui(c, `UPDATE repairs SET public_token_hash = 'testo', legacy_public_code_hash = 'altro testo' WHERE id = 2`)
	})
	r := controllaBloccato(t, dir, o, importa(t, o))
	var dellaRiga []Voce
	for _, v := range r.Differenze {
		if v.Tabella == "repairs" && v.Id != nil && *v.Id == 2 {
			dellaRiga = append(dellaRiga, v)
		}
	}
	if len(dellaRiga) != 1 || dellaRiga[0].Codice != "tipo/blob_atteso" {
		t.Errorf("differenze %s", differenzeDi(r))
	}
	if !ha(r.Differenze, "riconciliazione/conteggio", "repairs", 0, "") {
		t.Errorf("differenze %s, manca il conteggio", differenzeDi(r))
	}
	for _, v := range r.Differenze {
		if strings.HasPrefix(v.Codice, "vincolo/") {
			t.Errorf("la riga scartata è stata scritta: %+v", v)
		}
	}
}

// Una chiave solo Go nella sorgente non si scrive: la sua differenza e il conteggio di settings,
// nient'altro.
func TestChiaveSoloGoNonScritta(t *testing.T) {
	dir, o := preparaImport(t, func(c *sql.DB) {
		esegui(c, `INSERT INTO settings (key, value, updated_at) VALUES (?, 'x', '2026-01-01T00:00:00.000000Z')`, audit.ChiaveConservazione)
	})
	r := controllaBloccato(t, dir, o, importa(t, o))
	if len(r.Differenze) != 2 || r.Differenze[0].Codice != "settings/chiave_solo_go" ||
		!reflect.DeepEqual(r.Differenze[1], Voce{Codice: "riconciliazione/conteggio", Tabella: "settings", Righe: ptr(1)}) {
		t.Errorf("differenze %s", differenzeDi(r))
	}
}

// Due attivi con la stessa email dopo la normalizzazione: nessuno dei due si scrive, il conteggio
// ne dice mancanti due.
func TestEmailUgualiNessunaScritta(t *testing.T) {
	dir, o := preparaImport(t, func(c *sql.DB) {
		esegui(c, `UPDATE users SET email = ? WHERE id = 3`, strings.ToUpper(emailTitolare))
	})
	r := controllaBloccato(t, dir, o, importa(t, o))
	if !slices.ContainsFunc(r.Differenze, func(v Voce) bool {
		return reflect.DeepEqual(v, Voce{Codice: "riconciliazione/conteggio", Tabella: "users", Righe: ptr(2)})
	}) {
		t.Errorf("differenze %s", differenzeDi(r))
	}
}

// Valori che non si traducono anche se la forma sembra giusta: un istante con la forma ma un
// giorno che non esiste, un istante con il fuso invece di Z, un ruolo fuori elenco (CHECK dello
// standalone spento), un'email che non è testo.
func TestValoriCheNonSiTraducono(t *testing.T) {
	casi := []struct {
		nome    string
		ritocca func(*sql.DB)
		voce    Voce
	}{
		{"istante di un giorno che non c'è", func(c *sql.DB) { esegui(c, `UPDATE customers SET updated_at = '2025-02-30T03:04:05Z' WHERE id = 2`) },
			Voce{Codice: "traduzione/istante", Tabella: "customers", Id: ptr(2), Colonna: "updated_at"}},
		{"istante con il fuso", func(c *sql.DB) {
			esegui(c, `UPDATE customers SET updated_at = '2025-01-02T03:04:05+02:00' WHERE id = 2`)
		},
			Voce{Codice: "traduzione/istante", Tabella: "customers", Id: ptr(2), Colonna: "updated_at"}},
		{"ruolo fuori elenco", func(c *sql.DB) {
			esegui(c, `PRAGMA ignore_check_constraints = ON`)
			esegui(c, `UPDATE users SET role = 'admin' WHERE id = 2`)
			esegui(c, `PRAGMA ignore_check_constraints = OFF`)
		}, Voce{Codice: "traduzione/ruolo", Tabella: "users", Id: ptr(2), Colonna: "role"}},
		{"email non testo", func(c *sql.DB) { esegui(c, `UPDATE users SET email = X'41' WHERE id = 2`) },
			Voce{Codice: "traduzione/email", Tabella: "users", Id: ptr(2), Colonna: "email"}},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			dir, o := preparaImport(t, c.ritocca)
			r := controllaBloccato(t, dir, o, importa(t, o))
			if len(r.Differenze) == 0 || !reflect.DeepEqual(r.Differenze[0], c.voce) {
				t.Errorf("differenze %s, attesa per prima %+v", differenzeDi(r), c.voce)
			}
		})
	}
}

// sqlite_sequence del Go, tabella per tabella con AUTOINCREMENT: una riga sola con max(seq dello
// standalone, max id), nessuna riga dove tutti e due sono 0, e nessuna riga per le tabelle senza
// AUTOINCREMENT. Una tabella svuotata nello standalone (seq rimasto) tiene il suo seq; una vuota e
// senza seq non ha la riga.
func TestSequenzeDiTutteLeTabelle(t *testing.T) {
	_, o := preparaImport(t, func(c *sql.DB) {
		esegui(c, `DELETE FROM job_runs`)
		esegui(c, `DELETE FROM telegram_link_tokens`)
		esegui(c, `DELETE FROM sqlite_sequence WHERE name = 'telegram_link_tokens'`)
	})
	if es := importa(t, o); es.Verdetto != Riconciliato {
		r, _ := leggiReport(t, o.Report)
		t.Fatalf("verdetto %s, differenze %s", es.Verdetto, differenzeDi(r))
	}
	src, dst := apriLettura(t, o.Sorgente), apriLettura(t, o.Destinazione)
	seqSrc := map[string]int64{}
	for k, r := range righe(t, src, `SELECT name, seq FROM sqlite_sequence`, 2) {
		seqSrc[k] = r[1].(int64)
	}
	if seqSrc["job_runs"] == 0 {
		t.Fatal("la sorgente non ha il seq di job_runs")
	}
	seqGo := map[string][]any{}
	rows, err := dst.Query(`SELECT name, seq FROM sqlite_sequence`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n string
		var s int64
		if err := rows.Scan(&n, &s); err != nil {
			t.Fatal(err)
		}
		seqGo[n] = append(seqGo[n], s)
	}
	chiudiRighe(t, rows)
	conAuto := map[string]bool{}
	for k, r := range righe(t, dst, `SELECT name, sql FROM sqlite_schema WHERE type = 'table' AND sql LIKE '%AUTOINCREMENT%'`, 2) {
		conAuto[k] = strings.Contains(r[1].(string), "AUTOINCREMENT")
	}
	for n := range seqGo {
		if !conAuto[n] {
			t.Errorf("sqlite_sequence ha %s, che non ha AUTOINCREMENT", n)
		}
	}
	for _, tab := range Mappa {
		if tab.Go == "" || !conAuto[tab.Go] {
			continue
		}
		var maxId sql.NullInt64
		if err := dst.QueryRow(`SELECT max(rowid) FROM "` + tab.Go + `"`).Scan(&maxId); err != nil {
			t.Fatal(err)
		}
		atteso := max(seqSrc[tab.Standalone], maxId.Int64)
		switch got := seqGo[tab.Go]; {
		case atteso == 0 && len(got) != 0:
			t.Errorf("%s: sqlite_sequence %v, attesa nessuna riga", tab.Go, got)
		case atteso > 0 && !reflect.DeepEqual(got, []any{atteso}):
			t.Errorf("%s: sqlite_sequence %v, atteso %d", tab.Go, got, atteso)
		}
	}
	if got := seqGo["token_telegram"]; len(got) != 0 {
		t.Errorf("token_telegram vuota e senza seq: %v", got)
	}
	if !reflect.DeepEqual(seqGo["esecuzioni_lavori"], []any{seqSrc["job_runs"]}) {
		t.Errorf("esecuzioni_lavori svuotata: %v, atteso %d", seqGo["esecuzioni_lavori"], seqSrc["job_runs"])
	}
}

// chiaviJSON dà le chiavi di un oggetto JSON, in ordine.
func chiaviJSON(t *testing.T, b []byte) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return chiavi(m)
}

// La forma del report.json (formato 1): le chiavi di primo livello, il riferimento solo con
// -autorizzato, media null quando l'import si ferma prima di guardarli, nelle voci solo i campi
// che servono, nelle tabelle «scritte» solo dopo la copia.
func TestFormaDelReport(t *testing.T) {
	base := []string{"avvisi", "differenze", "formato", "istante", "media", "modalita", "scartate", "sorgente", "tabelle", "totali_centesimi", "verdetto"}
	t.Run("riconciliato", func(t *testing.T) {
		_, o := preparaImport(t, nil)
		importa(t, o)
		_, b := leggiReport(t, o.Report)
		if got := chiaviJSON(t, b); !reflect.DeepEqual(got, base) {
			t.Errorf("chiavi %v", got)
		}
		var r struct {
			Formato json.RawMessage   `json:"formato"`
			Tabelle []json.RawMessage `json:"tabelle"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatal(err)
		}
		if string(r.Formato) != "1" || len(r.Tabelle) == 0 {
			t.Fatalf("formato %s, tabelle %d", r.Formato, len(r.Tabelle))
		}
		if got := chiaviJSON(t, r.Tabelle[0]); !reflect.DeepEqual(got, []string{"lette", "scritte", "standalone"}) {
			t.Errorf("chiavi di una tabella %v", got)
		}
		if !bytes.HasSuffix(b, []byte("}\n")) || !bytes.Contains(b, []byte("\n  \"formato\": 1,\n")) {
			t.Errorf("report non indentato o senza a capo finale:\n%.200s", b)
		}
	})
	t.Run("autorizzato", func(t *testing.T) {
		_, o := preparaImport(t, nil)
		o.Sintetico, o.Autorizzato, o.Riferimento = false, true, "prova"
		importa(t, o)
		_, b := leggiReport(t, o.Report)
		if got := chiaviJSON(t, b); !slices.Contains(got, "riferimento") || len(got) != len(base)+1 {
			t.Errorf("chiavi %v", got)
		}
	})
	t.Run("bloccato dallo schema", func(t *testing.T) {
		_, o := preparaImport(t, func(c *sql.DB) { esegui(c, `CREATE TABLE intrusa (x TEXT)`) })
		importa(t, o)
		_, b := leggiReport(t, o.Report)
		var r struct {
			Media      json.RawMessage   `json:"media"`
			Differenze []json.RawMessage `json:"differenze"`
			Tabelle    []json.RawMessage `json:"tabelle"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatal(err)
		}
		if got := chiaviJSON(t, b); !reflect.DeepEqual(got, base) || string(r.Media) != "null" {
			t.Errorf("chiavi %v, media %s", got, r.Media)
		}
		if len(r.Differenze) != 1 || len(r.Tabelle) == 0 {
			t.Fatalf("differenze %d, tabelle %d", len(r.Differenze), len(r.Tabelle))
		}
		if got := chiaviJSON(t, r.Differenze[0]); !reflect.DeepEqual(got, []string{"codice", "righe", "tabella"}) {
			t.Errorf("chiavi della voce %v", got)
		}
		if got := chiaviJSON(t, r.Tabelle[0]); !reflect.DeepEqual(got, []string{"lette", "standalone"}) {
			t.Errorf("chiavi di una tabella %v", got)
		}
	})
	t.Run("voce con id e colonna", func(t *testing.T) {
		_, o := preparaImport(t, func(c *sql.DB) { esegui(c, `UPDATE user_invitations SET token_hash = 'testo' WHERE id = 1`) })
		importa(t, o)
		_, b := leggiReport(t, o.Report)
		var r struct {
			Differenze []json.RawMessage `json:"differenze"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatal(err)
		}
		if len(r.Differenze) == 0 {
			t.Fatal("nessuna differenza")
		}
		if got := chiaviJSON(t, r.Differenze[0]); !reflect.DeepEqual(got, []string{"codice", "colonna", "id", "tabella"}) {
			t.Errorf("chiavi della voce %v", got)
		}
	})
}

// I vincoli di import_standalone: una riga sola (id 1), modalità dell'elenco, impronta di 32 byte
// in un BLOB, dimensione positiva, istante nella forma del Go; nessun campo NULL. La riga giusta
// passa.
func TestVincoliDelSegnoDellImport(t *testing.T) {
	giusta := map[string]any{"id": int64(1), "modalita": Sintetico, "sha256_sorgente": bytes.Repeat([]byte{7}, 32),
		"byte_sorgente": int64(4096), "importato_il": istante}
	for _, c := range []struct {
		nome string
		v    map[string]any
	}{
		{"id 2", con(giusta, "id", int64(2))},
		{"modalità fuori elenco", con(giusta, "modalita", "prova")},
		{"modalità NULL", con(giusta, "modalita", nil)},
		{"impronta di 33 byte", con(giusta, "sha256_sorgente", bytes.Repeat([]byte{7}, 33))},
		{"impronta di 31 byte", con(giusta, "sha256_sorgente", bytes.Repeat([]byte{7}, 31))},
		{"impronta come testo", con(giusta, "sha256_sorgente", strings.Repeat("a", 32))},
		{"impronta NULL", con(giusta, "sha256_sorgente", nil)},
		{"dimensione 0", con(giusta, "byte_sorgente", int64(0))},
		{"dimensione negativa", con(giusta, "byte_sorgente", int64(-1))},
		{"dimensione NULL", con(giusta, "byte_sorgente", nil)},
		{"dimensione come testo", con(giusta, "byte_sorgente", "molti")},
		{"istante senza microsecondi", con(giusta, "importato_il", "2026-10-30T09:15:07Z")},
		{"istante con un prefisso", con(giusta, "importato_il", "x"+istante)},
		{"istante NULL", con(giusta, "importato_il", nil)},
	} {
		t.Run(c.nome, func(t *testing.T) {
			d := migrato(t)
			if err := riga(d, "import_standalone", c.v); err == nil {
				t.Errorf("riga accettata: %v", c.v)
			}
		})
	}
	d := migrato(t)
	if err := riga(d, "import_standalone", giusta); err != nil {
		t.Fatalf("riga giusta: %v", err)
	}
	if err := riga(d, "import_standalone", giusta); err == nil {
		t.Error("seconda riga accettata")
	}
}

// Un errore che non è dei dati (qui la cartella delle miniature che non si legge): l'import non
// attiva niente, scrive il report bloccato con import/errore e restituisce l'errore insieme
// all'esito.
func TestErroreCheNonEDeiDati(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root legge anche senza il permesso di lettura")
	}
	dir, o := preparaImport(t, nil)
	o.Miniature = filepath.Join(t.TempDir(), "thumbnails")
	if err := os.Mkdir(o.Miniature, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(o.Miniature, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(o.Miniature, 0o700) }) // per la pulizia di TempDir
	es, err := Importa(context.Background(), o)
	if err == nil || !strings.Contains(err.Error(), "import non riuscito, niente attivato") || !strings.Contains(err.Error(), "cartella delle miniature") {
		t.Errorf("errore %v", err)
	}
	if es.Report != o.Report || es.Sha256 != o.Sha256 || es.Modalita != Sintetico {
		t.Errorf("esito %+v", es)
	}
	r := controllaBloccato(t, dir, o, es)
	if !reflect.DeepEqual(r.Differenze, []Voce{{Codice: "import/errore"}}) {
		t.Errorf("differenze %s", differenzeDi(r))
	}
}

// Le righe lette e scritte dell'esito sono le somme del report.
func TestEsitoConteggi(t *testing.T) {
	_, o := preparaImport(t, nil)
	es := importa(t, o)
	r, _ := leggiReport(t, o.Report)
	var lette, scritte int64
	for _, tr := range r.Tabelle {
		lette += tr.Lette
		for _, n := range tr.Scritte {
			scritte += n
		}
	}
	if es.Lette != lette || es.Scritte != scritte || lette < 50 {
		t.Errorf("esito: lette %d, scritte %d; nel report %d e %d", es.Lette, es.Scritte, lette, scritte)
	}
}

// Un report comparso durante l'import: l'errore lo dice, non lo sovrascrive.
func TestReportComparsoMessaggio(t *testing.T) {
	_, o := preparaImport(t, nil)
	o.PrimaDiAttivare = func(context.Context) { scriviFile(t, o.Report) }
	_, err := Importa(context.Background(), o)
	if err == nil || !strings.Contains(err.Error(), "il report "+o.Report+" esiste già: non lo sovrascrivo") {
		t.Errorf("errore %v", err)
	}
}

// La copia della sorgente si apre in sola lettura e immutabile: durante l'import accanto alla
// copia non nasce niente (né -wal né -shm), anche se la sua intestazione dice WAL.
func TestCopiaDellaSorgenteSenzaFileAccanto(t *testing.T) {
	dir, o := preparaImport(t, nil)
	visti := 0
	o.DopoTabella = func(context.Context, string) {
		visti++
		accanto, err := filepath.Glob(filepath.Join(dir, prefissoLavoro+"*", "sorgente.db?*"))
		if err != nil || len(accanto) != 0 {
			t.Errorf("accanto alla copia: %v %v", accanto, err)
		}
	}
	if es := importa(t, o); es.Verdetto != Riconciliato || visti == 0 {
		t.Fatalf("verdetto %s, tabelle viste %d", es.Verdetto, visti)
	}
}

// Un database finito che non risponde nemmeno a integrity_check (intestazione rovinata) è una
// differenza della verifica, non un errore dell'import.
func TestVerificaFinaleDatabaseIlleggibile(t *testing.T) {
	dir, o := preparaImport(t, nil)
	primaDellaVerifica = func(p string) {
		f, err := os.OpenFile(p, os.O_WRONLY, 0)
		if err != nil {
			panic(err)
		}
		if _, err := f.WriteAt(bytes.Repeat([]byte("rovinato"), 512), 0); err != nil {
			panic(err)
		}
		if err := f.Close(); err != nil {
			panic(err)
		}
	}
	t.Cleanup(func() { primaDellaVerifica = nil })
	es, err := Importa(context.Background(), o)
	r := controllaBloccato(t, dir, o, es)
	if err != nil || !reflect.DeepEqual(r.Differenze, []Voce{{Codice: "destinazione/integrita"}}) {
		t.Errorf("errore %v, differenze %s", err, differenzeDi(r))
	}
}

// Un'email che non è testo non entra nel confronto delle email uguali: anche se i suoi byte
// sono quelli di un'altra email attiva, la differenza è solo la sua traduzione.
func TestEmailNonTestoFuoriDalConfronto(t *testing.T) {
	dir, o := preparaImport(t, func(c *sql.DB) { esegui(c, `UPDATE users SET email = CAST(? AS BLOB) WHERE id = 3`, emailTitolare) })
	r := controllaBloccato(t, dir, o, importa(t, o))
	for _, v := range r.Differenze {
		if v.Tabella == "users" && v.Codice != "riconciliazione/conteggio" &&
			!reflect.DeepEqual(v, Voce{Codice: "traduzione/email", Tabella: "users", Id: ptr(3), Colonna: "email"}) {
			t.Errorf("differenze %s", differenzeDi(r))
		}
	}
}

// Un istante senza decimali, con la Z, è valido: passa con sei zeri.
func TestIstanteSenzaDecimali(t *testing.T) {
	_, o := preparaImport(t, func(c *sql.DB) { esegui(c, `UPDATE customers SET updated_at = '2025-01-02T03:04:05Z' WHERE id = 2`) })
	if es := importa(t, o); es.Verdetto != Riconciliato {
		r, _ := leggiReport(t, o.Report)
		t.Fatalf("verdetto %s, differenze %s", es.Verdetto, differenzeDi(r))
	}
	var v string
	if err := apriLettura(t, o.Destinazione).QueryRow(`SELECT aggiornato_il FROM clienti WHERE id = 2`).Scan(&v); err != nil || v != "2025-01-02T03:04:05.000000Z" {
		t.Errorf("aggiornato_il %q %v", v, err)
	}
}

// Rifiuti del Go con la colonna giusta anche dove il messaggio di SQLite non la nomina per prima:
// un CHECK che comincia con una funzione (length), uno su una colonna con cifre nel nome (CHECK
// dello standalone spento), un NOT NULL (una chiave di settings NULL, che
// lo standalone accetta), una chiave esterna orfana in ognuna delle tre chiavi di una tabella;
// e con una riga rifiutata anche il totale dei centesimi, anche quando la somma della sorgente
// non è intera e quella del Go è zero.
func TestColonnaDeiRifiuti(t *testing.T) {
	casi := []struct {
		nome    string
		ritocca func(*sql.DB)
		voce    Voce
	}{
		{"CHECK con una funzione", func(c *sql.DB) { esegui(c, `UPDATE password_resets SET token_hash = zeroblob(31)`) },
			Voce{Codice: "vincolo/check", Tabella: "password_resets", Id: ptr(1), Colonna: "token_hash"}},
		{"CHECK su una colonna con cifre nel nome", func(c *sql.DB) {
			esegui(c, `PRAGMA ignore_check_constraints = ON`)
			esegui(c, `UPDATE legacy_import_captures SET source_sha256 = zeroblob(31)`)
			esegui(c, `PRAGMA ignore_check_constraints = OFF`)
		},
			Voce{Codice: "vincolo/check", Tabella: "legacy_import_captures", Id: ptr(1), Colonna: "source_sha256"}},
		{"NOT NULL", func(c *sql.DB) {
			esegui(c, `INSERT INTO settings (key, value, updated_at) VALUES (NULL, 'x', '2026-01-01T00:00:00.000000Z')`)
		}, Voce{Codice: "vincolo/non_nullo", Tabella: "settings", Id: ptr(7), Colonna: "key"}},
		{"orfana nella busta", func(c *sql.DB) { esegui(c, `UPDATE artisan_reminders SET repair_id = 99`) },
			Voce{Codice: "vincolo/chiave_esterna", Tabella: "artisan_reminders", Id: ptr(1), Colonna: "repair_id"}},
		{"orfana nell'artigiano", func(c *sql.DB) { esegui(c, `UPDATE artisan_reminders SET artisan_id = 99`) },
			Voce{Codice: "vincolo/chiave_esterna", Tabella: "artisan_reminders", Id: ptr(1), Colonna: "artisan_id"}},
		{"orfana nella consegna", func(c *sql.DB) { esegui(c, `UPDATE artisan_reminders SET handover_id = 99`) },
			Voce{Codice: "vincolo/chiave_esterna", Tabella: "artisan_reminders", Id: ptr(1), Colonna: "handover_id"}},
		{"totale di una riga rifiutata", func(c *sql.DB) { esegui(c, `UPDATE repairs SET ready_on = '31/02/2026' WHERE id = 2`) },
			Voce{Codice: "riconciliazione/totale", Tabella: "repairs", Colonna: "estimate_cents"}},
		{"totale non intero che sembra zero", func(c *sql.DB) {
			esegui(c, `UPDATE repairs SET estimate_cents = 0 WHERE id = 1`)
			esegui(c, `UPDATE repairs SET estimate_cents = 12.5 WHERE id = 2`)
		}, Voce{Codice: "riconciliazione/totale", Tabella: "repairs", Colonna: "estimate_cents"}},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			dir, o := preparaImport(t, c.ritocca)
			r := controllaBloccato(t, dir, o, importa(t, o))
			if !slices.ContainsFunc(r.Differenze, func(d Voce) bool { return reflect.DeepEqual(d, c.voce) }) {
				t.Errorf("differenze %s, manca %+v", differenzeDi(r), c.voce)
			}
		})
	}
}
