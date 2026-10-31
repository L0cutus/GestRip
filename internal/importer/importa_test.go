package importer

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// apriLettura apre un database in sola lettura per i controlli dei test.
func apriLettura(t *testing.T, p string) *sql.DB {
	t.Helper()
	c, err := sql.Open("sqlite", "file:"+p+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() }) // sola lettura
	return c
}

func leggiReport(t *testing.T, p string) (Report, []byte) {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var r Report
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return r, b
}

func importa(t *testing.T, o Opzioni) Esito {
	t.Helper()
	es, err := Importa(context.Background(), o)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return es
}

// codici dà i codici delle voci, nell'ordine.
func codici(vs []Voce) []string {
	var cs []string
	for _, v := range vs {
		cs = append(cs, v.Codice)
	}
	return cs
}

// ha dice se tra le voci c'è quella con codice, tabella, id (0: qualunque) e colonna.
func ha(vs []Voce, codice, tab string, id int64, col string) bool {
	for _, v := range vs {
		if v.Codice == codice && v.Tabella == tab && v.Colonna == col && (id == 0 || (v.Id != nil && *v.Id == id)) {
			return true
		}
	}
	return false
}

// attesoGo è la traduzione che il test si aspetta, scritta qui a parte dall'import.
func attesoGo(t *testing.T, tr Traduzione, v any) any {
	t.Helper()
	if v == nil {
		return nil
	}
	switch tr {
	case Istante:
		x, err := time.Parse(time.RFC3339Nano, v.(string))
		if err != nil {
			t.Fatalf("istante sintetico %v: %v", v, err)
		}
		return x.UTC().Format(db.FormatoIstante)
	case Ruolo:
		return map[string]string{"owner": "titolare", "staff": "dipendente"}[v.(string)]
	case Email:
		return strings.ToLower(strings.TrimSpace(v.(string)))
	}
	return v
}

// righe legge tutte le righe di una query, per chiave (la prima colonna).
func righe(t *testing.T, c *sql.DB, q string, n int) map[string][]any {
	t.Helper()
	rows, err := c.Query(q)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	m := map[string][]any{}
	for rows.Next() {
		buf := make([]any, n)
		ptrs := make([]any, n)
		for i := range buf {
			ptrs[i] = &buf[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		m[fmt.Sprint(buf[0])] = buf
	}
	chiudiRighe(t, rows)
	return m
}

// controllaAndataERitorno confronta ogni colonna mappata di ogni riga della sorgente con la
// colonna Go della destinazione: valore tradotto atteso, stesso tipo.
func controllaAndataERitorno(t *testing.T, src, dst *sql.DB) {
	t.Helper()
	for _, tab := range Mappa {
		if tab.Go == "" {
			continue
		}
		var cs, gs []string
		for _, c := range tab.Colonne {
			cs = append(cs, `"`+c.Standalone+`"`)
			gs = append(gs, `"`+c.Go+`"`)
		}
		a := righe(t, src, fmt.Sprintf(`SELECT %s FROM %s`, strings.Join(cs, ", "), tab.Standalone), len(cs))
		b := righe(t, dst, fmt.Sprintf(`SELECT %s FROM %s`, strings.Join(gs, ", "), tab.Go), len(gs))
		attese := 0
		for k, r := range a {
			if tab.Standalone == "settings" && slices.Contains(ChiaviSegreteProvider, r[0].(string)) != (tab.Go == "segreti_provider") {
				continue
			}
			attese++
			g, ok := b[k]
			if !ok {
				t.Errorf("%s → %s: manca la riga %s", tab.Standalone, tab.Go, k)
				continue
			}
			for i, c := range tab.Colonne {
				if want := attesoGo(t, c.Traduzione, r[i]); !reflect.DeepEqual(g[i], want) {
					t.Errorf("%s.%s → %s.%s, riga %s: %#v, atteso %#v", tab.Standalone, c.Standalone, tab.Go, c.Go, k, g[i], want)
				}
			}
		}
		if attese == 0 {
			t.Errorf("%s → %s: nessuna riga sintetica", tab.Standalone, tab.Go)
		}
		if len(b) != attese {
			t.Errorf("%s: %d righe, attese %d", tab.Go, len(b), attese)
		}
	}
}

// Import completo del database sintetico (tutte le tabelle mappate, un valore diverso in ogni
// colonna): riconciliato a zero differenze e zero avvisi, e ogni colonna Go ha il valore tradotto
// atteso (istanti a sei e a tre decimali, date, booleani, JSON, ruoli, email). Poi:
// sqlite_sequence a max(seq dello standalone, max id), reset_password.origine = 'pagina', le chiavi
// segrete in segreti_provider e le altre (anche jobs.overdue.silenced_history) in impostazioni,
// token pubblici in chiaro e hash portati, codici_primo_titolare e segreti vuote, il segno
// dell'import, nessun temporaneo rimasto, destinazione e report 0600.
func TestImportCompletoRiconciliato(t *testing.T) {
	dir, o := preparaImport(t, nil)
	es := importa(t, o)
	if es.Verdetto != Riconciliato {
		r, b := leggiReport(t, o.Report)
		t.Fatalf("verdetto %s, differenze %v\n%s", es.Verdetto, codici(r.Differenze), b)
	}
	r, _ := leggiReport(t, o.Report)
	if len(r.Differenze) != 0 || len(r.Avvisi) != 0 || r.Verdetto != Riconciliato {
		t.Errorf("report: verdetto %s, differenze %v, avvisi %v", r.Verdetto, r.Differenze, r.Avvisi)
	}
	if es.Sha256 != o.Sha256 || r.Sorgente.Sha256 != o.Sha256 || r.Modalita != Sintetico || r.Riferimento != "" || r.Istante != "2026-10-30T21:00:00.000000Z" {
		t.Errorf("report: %+v %+v", r.Sorgente, r)
	}
	fi, err := os.Stat(o.Sorgente)
	if err != nil {
		t.Fatal(err)
	}
	if r.Sorgente.Byte != fi.Size() {
		t.Errorf("byte della sorgente %d, attesi %d", r.Sorgente.Byte, fi.Size())
	}
	for _, p := range []string{o.Destinazione, o.Report} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: permessi %v, attesi 0600", filepath.Base(p), fi.Mode().Perm())
		}
	}
	if got := fileDi(t, dir); !reflect.DeepEqual(got, []string{"gestrip.db", "media", "report.json", "standalone.db"}) {
		t.Errorf("nella cartella: %v", got)
	}
	// Conteggi e totali nel report: letti = scritti per ogni tabella, totali uguali.
	if len(r.Tabelle) != 40 {
		t.Errorf("tabelle nel report: %d", len(r.Tabelle))
	}
	for _, tr := range r.Tabelle {
		var s int64
		for _, n := range tr.Scritte {
			s += n
		}
		if tr.Lette == 0 || s != tr.Lette {
			t.Errorf("%s: lette %d, scritte %v", tr.Standalone, tr.Lette, tr.Scritte)
		}
	}
	if len(r.Totali) != 7 {
		t.Errorf("totali: %+v", r.Totali)
	}
	for _, tt := range r.Totali {
		if tt.Sorgente != tt.Destinazione || tt.Sorgente == 0 {
			t.Errorf("totale %+v", tt)
		}
	}
	if !reflect.DeepEqual(r.Scartate, []TabellaReport{{Standalone: "permanent_deletion_permits"}, {Standalone: "schema_migrations", Lette: 2}}) {
		t.Errorf("scartate: %+v", r.Scartate)
	}

	src, dst := apriLettura(t, o.Sorgente), apriLettura(t, o.Destinazione)
	controllaAndataERitorno(t, src, dst)
	var esito, modo string
	if err := dst.QueryRow(`PRAGMA integrity_check`).Scan(&esito); err != nil || esito != "ok" {
		t.Errorf("integrity_check %q %v", esito, err)
	}
	if err := dst.QueryRow(`PRAGMA journal_mode`).Scan(&modo); err != nil || modo != "delete" {
		t.Errorf("journal_mode %q %v", modo, err)
	}
	q := func(s string) any {
		t.Helper()
		var v any
		if err := dst.QueryRow(s).Scan(&v); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		return v
	}
	casi := []struct {
		q    string
		want any
	}{
		{`SELECT seq FROM sqlite_sequence WHERE name = 'utenti'`, int64(40)},
		{`SELECT seq FROM sqlite_sequence WHERE name = 'clienti'`, int64(2)},
		{`SELECT seq FROM sqlite_sequence WHERE name = 'buste'`, int64(2)},
		{`SELECT count(*) FROM sqlite_sequence WHERE name = 'cattura_import_flask'`, int64(0)},
		{`SELECT group_concat(origine) FROM reset_password`, "pagina"},
		{`SELECT group_concat(nome, ',') FROM (SELECT nome FROM segreti_provider ORDER BY nome)`,
			"assist.ai.api_key,assist.stt.api_key,outgoing.smtp.password,outgoing.telegram.bot_token"},
		{`SELECT valore FROM segreti_provider WHERE nome = 'outgoing.smtp.password'`, segretoSMTP},
		{`SELECT count(*) FROM impostazioni WHERE chiave LIKE '%password%' OR chiave LIKE '%token%' OR chiave LIKE '%api_key%'`, int64(0)},
		{`SELECT valore FROM impostazioni WHERE chiave = 'jobs.overdue.silenced_history'`, "2026-02-03T04:05:06.000000Z"},
		{`SELECT conservazione_iniziata_il FROM buste WHERE id = 1`, "2025-03-04T05:06:07.123000Z"},
		{`SELECT cancellata_il FROM buste WHERE id = 1`, "2025-03-05T05:06:07.500000Z"},
		{`SELECT token_pubblico FROM buste WHERE id = 1`, tokenPubblico},
		{`SELECT typeof(hash_token_pubblico) || length(hash_token_pubblico) FROM buste WHERE id = 1`, "blob32"},
		{`SELECT group_concat(ruolo, ',') FROM (SELECT ruolo FROM utenti ORDER BY id)`, "titolare,dipendente,dipendente"},
		{`SELECT stato FROM messaggi_in_uscita`, "queued"},
		{`SELECT count(*) FROM codici_primo_titolare`, int64(0)},
		{`SELECT count(*) FROM segreti`, int64(0)},
		{`SELECT modalita || ' ' || lower(hex(sha256_sorgente)) || ' ' || byte_sorgente || ' ' || importato_il FROM import_standalone`,
			fmt.Sprintf("sintetico %s %d 2026-10-30T21:00:00.000000Z", o.Sha256, fi.Size())},
		{`SELECT count(*) FROM pragma_foreign_key_check`, int64(0)},
	}
	for _, c := range casi {
		if got := q(c.q); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s = %#v, atteso %#v", c.q, got, c.want)
		}
	}
	// Il database importato si apre e si migra come quello di serve, senza migrazioni da applicare.
	d, err := db.Apri(o.Destinazione)
	if err != nil {
		t.Fatal(err)
	}
	if app, err := d.Migra(context.Background(), clock.NuovoFinto(istanteImport)); err != nil || len(app) != 0 {
		t.Errorf("migrazioni dopo l'import: %v %v", app, err)
	}
	if err := d.Chiudi(); err != nil {
		t.Fatal(err)
	}
}

// contenuto legge tutte le tabelle di un database (anche sqlite_sequence), tranne gli istanti
// dell'import: schema_migrations.applicata_il e import_standalone.importato_il.
func contenuto(t *testing.T, p string) map[string][][]any {
	t.Helper()
	c := apriLettura(t, p)
	var tabelle []string
	rows, err := c.Query(`SELECT name FROM sqlite_schema WHERE type = 'table' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tabelle = append(tabelle, n)
	}
	chiudiRighe(t, rows)
	m := map[string][][]any{}
	for _, tab := range tabelle {
		r, err := c.Query(`SELECT * FROM "` + tab + `" ORDER BY rowid`)
		if tab == "sqlite_sequence" {
			r, err = c.Query(`SELECT * FROM sqlite_sequence ORDER BY name`)
		}
		if err != nil {
			t.Fatal(err)
		}
		cols, err := r.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for r.Next() {
			buf := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range buf {
				ptrs[i] = &buf[i]
			}
			if err := r.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			for i, col := range cols {
				if (tab == "schema_migrations" && col == "applicata_il") || (tab == "import_standalone" && col == "importato_il") {
					buf[i] = "ISTANTE DELL'IMPORT"
				}
			}
			m[tab] = append(m[tab], buf)
		}
		chiudiRighe(t, r)
	}
	return m
}

// inventario: OPS-04
// La destinazione è sempre nuova: due import della stessa sorgente in due destinazioni, a ore
// diverse, danno lo stesso contenuto in tutte le tabelle (anche sqlite_sequence), gli stessi media
// (nomi, byte, permessi) e lo stesso report, a parte l'istante dell'import.
func TestSecondoImportIdenticoAlPrimo(t *testing.T) {
	_, o := preparaImport(t, nil)
	importa(t, o)
	// La cartella media sta accanto alla destinazione: il secondo import va in un'altra cartella.
	dir2 := t.TempDir()
	o2 := o
	o2.Destinazione, o2.Report = filepath.Join(dir2, "secondo.db"), filepath.Join(dir2, "secondo.json")
	o2.Orologio = clock.NuovoFinto(istanteImport.Add(26*time.Hour + 3*time.Second))
	importa(t, o2)
	a, b := contenuto(t, o.Destinazione), contenuto(t, o2.Destinazione)
	if len(a) < 40 {
		t.Fatalf("tabelle lette: %d", len(a))
	}
	if !reflect.DeepEqual(a, b) {
		for tab := range a {
			if !reflect.DeepEqual(a[tab], b[tab]) {
				t.Errorf("%s diversa tra i due import", tab)
			}
		}
	}
	if m1, m2 := alberoMedia(t, CartellaMediaDi(o.Destinazione)), alberoMedia(t, CartellaMediaDi(o2.Destinazione)); len(m1) == 0 || !reflect.DeepEqual(m1, m2) {
		t.Errorf("media diversi tra i due import:\n%v\n%v", m1, m2)
	}
	r1, _ := leggiReport(t, o.Report)
	r2, _ := leggiReport(t, o2.Report)
	if r1.Istante == r2.Istante {
		t.Errorf("istante uguale nei due report: %s", r1.Istante)
	}
	r1.Istante, r2.Istante = "", ""
	if !reflect.DeepEqual(r1, r2) {
		t.Errorf("report diversi:\n%+v\n%+v", r1, r2)
	}
}

// controllaBloccato: verdetto bloccato, report scritto, nessuna destinazione e nessun temporaneo.
func controllaBloccato(t *testing.T, dir string, o Opzioni, es Esito) Report {
	t.Helper()
	if es.Verdetto != Bloccato {
		t.Errorf("verdetto %q, atteso bloccato", es.Verdetto)
	}
	if _, err := os.Lstat(o.Destinazione); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("destinazione: %v, attesa assente", err)
	}
	if got := fileDi(t, dir); !reflect.DeepEqual(got, []string{"report.json", "standalone.db"}) {
		t.Errorf("nella cartella: %v", got)
	}
	r, _ := leggiReport(t, o.Report)
	if r.Verdetto != Bloccato {
		t.Errorf("verdetto nel report %q", r.Verdetto)
	}
	return r
}

// inventario: OPS-01
// Database corrotto (byte rovinati nell'intestazione o in una pagina), troncato o incompleto
// (una tabella mancante): l'import non attiva niente e il report dice bloccato con il motivo.
func TestSorgenteRovinataNonAttivaNiente(t *testing.T) {
	rovina := func(t *testing.T, p string, f func(b []byte) []byte) {
		t.Helper()
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, f(b), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	casi := []struct {
		nome    string
		ritocca func(*sql.DB)
		file    func(b []byte) []byte
		codice  string
		tabella string
	}{
		{"intestazione rovinata", nil, func(b []byte) []byte { copy(b, "non è un database!"); return b }, "sorgente/illeggibile", ""},
		{"pagina rovinata", nil, func(b []byte) []byte {
			for i := 4096 * 3; i < 4096*6 && i < len(b); i++ {
				b[i] = byte(i * 7)
			}
			return b
		}, "sorgente/illeggibile", ""},
		{"troncato", nil, func(b []byte) []byte { return b[:len(b)/2] }, "sorgente/illeggibile", ""},
		{"tabella mancante", func(c *sql.DB) {
			if _, err := c.Exec(`DROP TABLE job_runs`); err != nil {
				panic(err)
			}
		}, nil, "schema/tabella_mancante", "job_runs"},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			dir, o := preparaImport(t, c.ritocca)
			if c.file != nil {
				rovina(t, o.Sorgente, c.file)
				o.Sha256 = shaDi(t, o.Sorgente)
			}
			es := importa(t, o)
			r := controllaBloccato(t, dir, o, es)
			if !ha(r.Differenze, c.codice, c.tabella, 0, "") {
				t.Errorf("differenze %+v, attesa %s %s", r.Differenze, c.codice, c.tabella)
			}
		})
	}
}

func esegui(c *sql.DB, q string, args ...any) {
	if _, err := c.Exec(q, args...); err != nil {
		panic(fmt.Sprintf("%s: %v", q, err))
	}
}

// inventario: OPS-01
// Lo schema della sorgente si confronta con la mappa: una tabella sconosciuta è bloccante con nome
// e righe (mai il contenuto), una colonna in più o in meno anche; un hash BLOB salvato come testo è
// bloccante con tabella, id e colonna; permanent_deletion_permits deve essere vuota.
func TestSchemaEDatiDellaSorgenteControllati(t *testing.T) {
	casi := []struct {
		nome    string
		ritocca func(*sql.DB)
		voce    Voce
	}{
		{"tabella sconosciuta", func(c *sql.DB) {
			esegui(c, `CREATE TABLE appunti_segreti (testo TEXT)`)
			esegui(c, `INSERT INTO appunti_segreti VALUES ('contenuto-da-non-copiare-1'), ('contenuto-da-non-copiare-2')`)
		}, Voce{Codice: "schema/tabella_sconosciuta", Tabella: "appunti_segreti", Righe: ptr(2)}},
		{"colonna in più", func(c *sql.DB) { esegui(c, `ALTER TABLE artisans ADD COLUMN email TEXT`) },
			Voce{Codice: "schema/colonna_sconosciuta", Tabella: "artisans", Colonna: "email"}},
		{"colonna mancante", func(c *sql.DB) { esegui(c, `ALTER TABLE artisans DROP COLUMN phone`) },
			Voce{Codice: "schema/colonna_mancante", Tabella: "artisans", Colonna: "phone"}},
		{"colonna in più in una tabella scartata", func(c *sql.DB) { esegui(c, `ALTER TABLE schema_migrations ADD COLUMN nota TEXT`) },
			Voce{Codice: "schema/colonna_sconosciuta", Tabella: "schema_migrations", Colonna: "nota"}},
		{"permessi di eliminazione presenti", func(c *sql.DB) { esegui(c, `INSERT INTO permanent_deletion_permits VALUES ('sale', 1)`) },
			Voce{Codice: "dati/permessi_eliminazione_presenti", Tabella: "permanent_deletion_permits", Righe: ptr(1)}},
		{"hash come testo", func(c *sql.DB) {
			esegui(c, `UPDATE repairs SET legacy_public_code_hash = ? WHERE id = 2`, strings.Repeat("a", 32))
		}, Voce{Codice: "tipo/blob_atteso", Tabella: "repairs", Id: ptr(2), Colonna: "legacy_public_code_hash"}},
		{"sha256 come testo", func(c *sql.DB) {
			esegui(c, `UPDATE user_invitations SET token_hash = 'testo' WHERE id = 1`)
		}, Voce{Codice: "tipo/blob_atteso", Tabella: "user_invitations", Id: ptr(1), Colonna: "token_hash"}},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			dir, o := preparaImport(t, c.ritocca)
			es := importa(t, o)
			r := controllaBloccato(t, dir, o, es)
			if len(r.Differenze) == 0 || !reflect.DeepEqual(r.Differenze[0], c.voce) {
				b, _ := json.Marshal(r.Differenze)
				t.Errorf("differenze %s, attesa per prima %+v", b, c.voce)
			}
			if _, b := leggiReport(t, o.Report); bytes.Contains(b, []byte("contenuto-da-non-copiare")) {
				t.Error("il report contiene il contenuto della tabella sconosciuta")
			}
			// Con una differenza di schema non si copia niente: il report ha solo le righe lette.
			if strings.HasPrefix(c.voce.Codice, "schema/") || strings.HasPrefix(c.voce.Codice, "dati/") {
				for _, tr := range r.Tabelle {
					if len(tr.Scritte) != 0 {
						t.Errorf("%s: scritte %v con lo schema diverso", tr.Standalone, tr.Scritte)
					}
				}
			}
		})
	}
}

// inventario: OPS-03
// Una riga che non si traduce o che il Go rifiuta è una differenza bloccante con tabella, id,
// colonna e codice, mai il valore: istante illeggibile, canale fuori dall'elenco del Go (CHECK),
// busta inesistente (chiave esterna), booleano 2. Nessuna riga sparisce in silenzio: il conteggio
// lo dice anche lui.
func TestRigheCheIlGoRifiuta(t *testing.T) {
	casi := []struct {
		nome    string
		ritocca func(*sql.DB)
		voci    []Voce
	}{
		{"istante illeggibile", func(c *sql.DB) { esegui(c, `UPDATE customers SET updated_at = 'ieri' WHERE id = 2`) },
			[]Voce{{Codice: "traduzione/istante", Tabella: "customers", Id: ptr(2), Colonna: "updated_at"},
				{Codice: "riconciliazione/conteggio", Tabella: "customers", Righe: ptr(1)}}},
		{"istante con nove decimali", func(c *sql.DB) {
			esegui(c, `UPDATE customers SET updated_at = '2025-01-02T03:04:05.123456789Z' WHERE id = 2`)
		},
			[]Voce{{Codice: "traduzione/istante", Tabella: "customers", Id: ptr(2), Colonna: "updated_at"}}},
		{"centesimi non interi", func(c *sql.DB) { esegui(c, `UPDATE repairs SET estimate_cents = 12.5 WHERE id = 2`) },
			[]Voce{{Codice: "vincolo/tipo", Tabella: "repairs", Id: ptr(2), Colonna: "estimate_cents"},
				{Codice: "riconciliazione/totale", Tabella: "repairs", Colonna: "estimate_cents"}}},
		{"data illeggibile", func(c *sql.DB) { esegui(c, `UPDATE repairs SET ready_on = '31/02/2026' WHERE id = 2`) },
			[]Voce{{Codice: "traduzione/data", Tabella: "repairs", Id: ptr(2), Colonna: "ready_on"}}},
		{"JSON non valido", func(c *sql.DB) { esegui(c, `UPDATE audit_events SET metadata = '{rotto' WHERE id = 3`) },
			[]Voce{{Codice: "traduzione/json", Tabella: "audit_events", Id: ptr(3), Colonna: "metadata"}}},
		{"booleano 2", func(c *sql.DB) { esegui(c, `UPDATE repair_photos SET vis_customer = 2`) },
			[]Voce{{Codice: "traduzione/booleano", Tabella: "repair_photos", Id: ptr(1), Colonna: "vis_customer"}}},
		{"canale fuori elenco", func(c *sql.DB) { esegui(c, `UPDATE password_resets SET channel = 'sms'`) },
			[]Voce{{Codice: "vincolo/check", Tabella: "password_resets", Id: ptr(1), Colonna: "channel"}}},
		{"tipo sbagliato", func(c *sql.DB) { esegui(c, `UPDATE artisan_reminders SET days_out = 'tanti'`) },
			[]Voce{{Codice: "vincolo/tipo", Tabella: "artisan_reminders", Id: ptr(1), Colonna: "days_out"}}},
		{"unicità", func(c *sql.DB) {
			esegui(c, `DROP INDEX artisans_name_index`)
			esegui(c, `UPDATE artisans SET name = 'Uguale'`)
			esegui(c, `INSERT INTO artisans (id, name) VALUES (7, 'Uguale')`)
		},
			[]Voce{{Codice: "vincolo/unico", Tabella: "artisans", Id: ptr(7), Colonna: "name"}}},
		{"busta inesistente", func(c *sql.DB) { esegui(c, `UPDATE repair_messages SET repair_id = 99`) },
			[]Voce{{Codice: "vincolo/chiave_esterna", Tabella: "repair_messages", Id: ptr(1), Colonna: "repair_id"}}},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			dir, o := preparaImport(t, c.ritocca)
			es := importa(t, o)
			r := controllaBloccato(t, dir, o, es)
			for _, v := range c.voci {
				if !slices.ContainsFunc(r.Differenze, func(d Voce) bool { return reflect.DeepEqual(d, v) }) {
					b, _ := json.Marshal(r.Differenze)
					t.Errorf("differenze %s, manca %+v", b, v)
				}
			}
		})
	}
}

// Email degli utenti (PIANO T2): con maiuscole o spazi ai lati si normalizza e si segnala come
// avviso, senza bloccare; due account attivi uguali dopo la normalizzazione bloccano («serve una
// scelta»), con tutti e due gli id; un attivo e un disattivato uguali passano, perché nel Go
// l'email è unica solo tra gli attivi (come nello standalone: chi è uscito si reinvita).
func TestEmailDegliUtenti(t *testing.T) {
	t.Run("da normalizzare", func(t *testing.T) {
		_, o := preparaImport(t, func(c *sql.DB) { esegui(c, `UPDATE users SET email = ? WHERE id = 3`, emailNormalizzar) })
		if es := importa(t, o); es.Verdetto != Riconciliato {
			t.Fatalf("verdetto %s", es.Verdetto)
		}
		r, b := leggiReport(t, o.Report)
		if !reflect.DeepEqual(r.Avvisi, []Voce{{Codice: "utenti/email_normalizzata", Tabella: "users", Id: ptr(3), Colonna: "email"}}) {
			t.Errorf("avvisi %+v", r.Avvisi)
		}
		if bytes.Contains(bytes.ToLower(b), []byte("persona.maiuscola")) {
			t.Error("il report contiene l'email")
		}
		var e string
		if err := apriLettura(t, o.Destinazione).QueryRow(`SELECT email FROM utenti WHERE id = 3`).Scan(&e); err != nil || e != "persona.maiuscola@esempio.test" {
			t.Errorf("email %q %v", e, err)
		}
	})
	t.Run("due attivi uguali", func(t *testing.T) {
		dir, o := preparaImport(t, func(c *sql.DB) {
			esegui(c, `UPDATE users SET email = ? WHERE id = 3`, " "+strings.ToUpper(emailTitolare))
		})
		r := controllaBloccato(t, dir, o, importa(t, o))
		for _, id := range []int64{1, 3} {
			if !ha(r.Differenze, "utenti/email_uguale_tra_attivi", "users", id, "email") {
				t.Errorf("differenze %+v, manca l'utente %d", r.Differenze, id)
			}
		}
	})
	t.Run("un attivo e un disattivato uguali", func(t *testing.T) {
		_, o := preparaImport(t, func(c *sql.DB) { esegui(c, `UPDATE users SET email = ? WHERE id = 2`, strings.ToUpper(emailTitolare)) })
		if es := importa(t, o); es.Verdetto != Riconciliato {
			r, b := leggiReport(t, o.Report)
			t.Fatalf("verdetto %s %v\n%s", es.Verdetto, r.Differenze, b)
		}
	})
}

// Il report non contiene mai valori dei dati: nessuno dei valori sensibili inventati (email,
// hash della password, segreti dei provider, token in chiaro, nomi, telefoni, hash in esadecimale,
// indirizzi e contenuto dei file media).
func TestReportSenzaValori(t *testing.T) {
	_, o := preparaImport(t, func(c *sql.DB) { esegui(c, `UPDATE users SET email = ? WHERE id = 3`, emailNormalizzar) })
	importa(t, o)
	_, b := leggiReport(t, o.Report)
	src := apriLettura(t, o.Sorgente)
	var hashPubblico []byte
	if err := src.QueryRow(`SELECT public_token_hash FROM repairs WHERE id = 1`).Scan(&hashPubblico); err != nil {
		t.Fatal(err)
	}
	vietati := []string{emailTitolare, "esempio.test", hashTitolare, "pbkdf2", segretoSMTP, segretoBot, "chiave-ai-inventata", tokenPubblico,
		tokenLotto, nomeCliente, telefonoCliente, "Maiuscola", hex.EncodeToString(hashPubblico), "v-description", "v-name", "json-",
		"byte inventati del file media"}
	for _, n := range append(mediaSintetici, 8) {
		vietati = append(vietati, chiaveMedia(n))
	}
	for _, v := range vietati {
		if bytes.Contains(bytes.ToLower(b), bytes.ToLower([]byte(v))) {
			t.Errorf("il report contiene %q", v)
		}
	}
	if strings.Contains(string(b), filepath.Dir(o.Sorgente)) {
		t.Error("il report contiene il percorso della sorgente")
	}
}

// inventario: OPS-01
// I rifiuti prima di aprire la sorgente non lasciano nessun file: modalità mancante, doppia,
// -riferimento vuoto o dato con -sintetico, SHA-256 sbagliato o malformato, dimensione sbagliata,
// -wal (o -journal, -shm) accanto alla sorgente, destinazione o report già esistenti.
func TestRifiutiSenzaFile(t *testing.T) {
	casi := []struct {
		nome   string
		cambia func(t *testing.T, o *Opzioni)
		errore string
	}{
		{"nessuna modalità", func(t *testing.T, o *Opzioni) { o.Sintetico = false }, "serve la modalità"},
		{"due modalità", func(t *testing.T, o *Opzioni) { o.Autorizzato, o.Riferimento = true, "Stefano, 30/10/2026" }, "una sola modalità"},
		{"riferimento vuoto", func(t *testing.T, o *Opzioni) { o.Sintetico, o.Autorizzato, o.Riferimento = false, true, "  " }, "chiede -riferimento"},
		{"riferimento con sintetico", func(t *testing.T, o *Opzioni) { o.Riferimento = "qualcuno" }, "solo con -autorizzato"},
		{"sha256 sbagliato", func(t *testing.T, o *Opzioni) { o.Sha256 = strings.Repeat("0", 64) }, "non quello dichiarato"},
		{"sha256 malformato", func(t *testing.T, o *Opzioni) { o.Sha256 = "abc" }, "64 cifre"},
		{"dimensione sbagliata", func(t *testing.T, o *Opzioni) { o.Dimensione = 12345 }, "non 12345 come dichiarato"},
		{"wal accanto", func(t *testing.T, o *Opzioni) { scriviFile(t, o.Sorgente+"-wal") }, "file -wal"},
		{"journal accanto", func(t *testing.T, o *Opzioni) { scriviFile(t, o.Sorgente+"-journal") }, "file -journal"},
		{"shm accanto", func(t *testing.T, o *Opzioni) { scriviFile(t, o.Sorgente+"-shm") }, "file -shm"},
		{"destinazione esistente", func(t *testing.T, o *Opzioni) { scriviFile(t, o.Destinazione) }, "esiste già"},
		{"destinazione link pendente", func(t *testing.T, o *Opzioni) {
			if err := os.Symlink("altrove.db", o.Destinazione); err != nil {
				t.Fatal(err)
			}
		}, "esiste già"},
		{"report esistente", func(t *testing.T, o *Opzioni) { scriviFile(t, o.Report) }, "esiste già: l'import non sovrascrive mai"},
		{"sorgente link", func(t *testing.T, o *Opzioni) {
			l := o.Sorgente + ".link"
			if err := os.Symlink(o.Sorgente, l); err != nil {
				t.Fatal(err)
			}
			o.Sorgente = l
		}, "non è un file regolare"},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			dir, o := preparaImport(t, nil)
			c.cambia(t, &o)
			prima := fileDi(t, dir)
			es, err := Importa(context.Background(), o)
			if err == nil || !strings.Contains(err.Error(), c.errore) {
				t.Errorf("errore %v, atteso con %q", err, c.errore)
			}
			if err != nil && strings.Contains(err.Error(), filepath.Base(o.Sorgente)) {
				t.Errorf("l'errore nomina la sorgente: %v", err)
			}
			if es.Report != "" || es.Verdetto != "" {
				t.Errorf("esito %+v", es)
			}
			if dopo := fileDi(t, dir); !reflect.DeepEqual(prima, dopo) {
				t.Errorf("file prima %v, dopo %v", prima, dopo)
			}
		})
	}
}

func scriviFile(t *testing.T, p string) {
	t.Helper()
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// inventario: OPS-01
// Modalità autorizzata: il riferimento (senza spazi ai lati) va nel report, e solo lì; la
// sorgente non cambia (si lavora su una copia in sola lettura).
func TestImportAutorizzato(t *testing.T) {
	_, o := preparaImport(t, nil)
	o.Sintetico, o.Autorizzato, o.Riferimento = false, true, "  autorizzato da prova, 30/10/2026 "
	prima := shaDi(t, o.Sorgente)
	fi, err := os.Stat(o.Sorgente)
	if err != nil {
		t.Fatal(err)
	}
	if es := importa(t, o); es.Verdetto != Riconciliato {
		t.Fatalf("verdetto %s", es.Verdetto)
	}
	r, _ := leggiReport(t, o.Report)
	if r.Modalita != Autorizzato || r.Riferimento != "autorizzato da prova, 30/10/2026" {
		t.Errorf("modalità %q, riferimento %q", r.Modalita, r.Riferimento)
	}
	dopo, err := os.Stat(o.Sorgente)
	if err != nil {
		t.Fatal(err)
	}
	if shaDi(t, o.Sorgente) != prima || !dopo.ModTime().Equal(fi.ModTime()) {
		t.Error("la sorgente è cambiata")
	}
	var m string
	if err := apriLettura(t, o.Destinazione).QueryRow(`SELECT modalita FROM import_standalone`).Scan(&m); err != nil || m != Autorizzato {
		t.Errorf("modalità nel segno %q %v", m, err)
	}
}

// inventario: OPS-07
// Un'interruzione a metà (il contesto finisce dopo la copia delle buste) non attiva niente: nessuna
// destinazione, nessun report, nessun temporaneo.
func TestImportInterrottoAMeta(t *testing.T) {
	dir, o := preparaImport(t, nil)
	ctx, ferma := context.WithCancel(context.Background())
	defer ferma()
	visti := 0
	o.DopoTabella = func(_ context.Context, tab string) {
		visti++
		if tab == "buste" {
			ferma()
		}
	}
	es, err := Importa(ctx, o)
	if !errors.Is(err, ErrInterrotto) || !errors.Is(err, context.Canceled) {
		t.Errorf("errore %v, atteso l'interruzione", err)
	}
	if es.Report != "" || visti < 5 {
		t.Errorf("esito %+v, tabelle viste %d", es, visti)
	}
	if got := fileDi(t, dir); !reflect.DeepEqual(got, []string{"standalone.db"}) {
		t.Errorf("nella cartella: %v", got)
	}
}

// inventario: ADM-17
// Dopo un import la conservazione dell'audit non cancella niente, anche con eventi più vecchi di
// 5 anni, finché non c'è un backup verificato (PIANO T2): il giro si rimanda e non scrive nulla.
func TestConservazioneFermaDopoLImport(t *testing.T) {
	_, o := preparaImport(t, nil)
	importa(t, o)
	d, err := db.Apri(o.Destinazione)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := d.Chiudi(); err != nil {
			t.Error(err)
		}
	}()
	var vecchi int
	if err := d.Lettura.QueryRow(`SELECT count(*) FROM eventi_audit WHERE creato_il < '2021-01-01'`).Scan(&vecchi); err != nil || vecchi != 2 {
		t.Fatalf("eventi vecchi %d %v", vecchi, err)
	}
	// L'orologio poco dopo l'evento più recente, così la guardia dell'orologio avanti non conta.
	var recente string
	if err := d.Lettura.QueryRow(`SELECT max(creato_il) FROM eventi_audit`).Scan(&recente); err != nil {
		t.Fatal(err)
	}
	adesso, err := time.Parse(db.FormatoIstante, recente)
	if err != nil {
		t.Fatal(err)
	}
	e, err := audit.NuovaConservazione(d, clock.NuovoFinto(adesso.Add(time.Hour))).Applica(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !e.Rimandata || !e.DopoImport || e.Girata || e.Cancellati != 0 {
		t.Errorf("esito %+v", e)
	}
	var n, stato int
	if err := d.Lettura.QueryRow(`SELECT count(*), (SELECT count(*) FROM impostazioni WHERE chiave = ?) FROM eventi_audit`, audit.ChiaveConservazione).Scan(&n, &stato); err != nil {
		t.Fatal(err)
	}
	if n != 3 || stato != 0 {
		t.Errorf("eventi %d (attesi 3), riga di stato %d", n, stato)
	}
	// Senza il segno (quello che farà il primo backup verificato) il giro cancella i due vecchi.
	if _, err := d.Scrittura.Exec(`DELETE FROM import_standalone`); err != nil {
		t.Fatal(err)
	}
	e, err = audit.NuovaConservazione(d, clock.NuovoFinto(adesso.Add(time.Hour))).Applica(context.Background())
	if err != nil || !e.Girata || e.Cancellati != 2 {
		t.Errorf("senza il segno: %+v %v", e, err)
	}
}
