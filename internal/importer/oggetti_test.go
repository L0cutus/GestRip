package importer

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// oggettoVero è una voce di testdata/standalone-go-baseline-oggetti.json: un trigger o una vista
// letti da sqlite_schema di un database vero dello standalone a go-baseline (quello decifrato della
// catena del banco, tools/schema-standalone/oggetti.py).
type oggettoVero struct {
	Tipo    string `json:"tipo"`
	Nome    string `json:"nome"`
	Tabella string `json:"tabella"`
	SQL     string `json:"sql"`
}

// oggettiVeri legge il file, letto qui a parte dall'import.
func oggettiVeri(t *testing.T) []oggettoVero {
	t.Helper()
	b, err := os.ReadFile("testdata/standalone-go-baseline-oggetti.json")
	if err != nil {
		t.Fatal(err)
	}
	var v []oggettoVero
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// sqlVero dà il testo vero dell'oggetto nome.
func sqlVero(t *testing.T, nome string) string {
	t.Helper()
	for _, o := range oggettiVeri(t) {
		if o.Nome == nome {
			return o.SQL
		}
	}
	t.Fatalf("%s non è tra gli oggetti veri", nome)
	return ""
}

// ritoccaDopo cambia la sorgente già finita (con i trigger), come farebbe una migrazione in più, e
// aggiorna l'impronta dichiarata: ritocca di preparaImport gira prima dei trigger.
func ritoccaDopo(t *testing.T, o *Opzioni, istruzioni ...string) {
	t.Helper()
	c, err := sql.Open("sqlite", "file:"+o.Sorgente)
	if err != nil {
		t.Fatal(err)
	}
	c.SetMaxOpenConns(1)
	for _, q := range istruzioni {
		if _, err := c.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	for _, s := range fileAccanto {
		if _, err := os.Lstat(o.Sorgente + s); err == nil {
			t.Fatalf("il ritocco ha lasciato %s", s)
		}
	}
	o.Sha256 = shaDi(t, o.Sorgente)
}

// inventario: OPS-01
// Trigger e viste della sorgente (nome e testo in sqlite_schema) devono essere quelli del
// riferimento di go-baseline: un trigger in più, uno tolto, uno con il testo cambiato (anche solo
// di uno spazio) o rifatto su un'altra tabella, una vista in più bloccano prima di copiare, con
// schema/oggetto_diverso che nomina l'oggetto e la sua tabella, mai il testo. La sorgente giusta
// passa.
func TestTriggerEVisteComeIlRiferimento(t *testing.T) {
	const segreto = "testo-del-trigger-da-non-copiare"
	casi := []struct {
		nome       string
		istruzioni []string
		voci       []Voce
	}{
		{"trigger in più", []string{`CREATE TRIGGER intruso AFTER INSERT ON artisans BEGIN SELECT '` + segreto + `'; END`},
			[]Voce{{Codice: "schema/oggetto_diverso", Tabella: "artisans", Oggetto: "intruso"}}},
		{"trigger tolto", []string{`DROP TRIGGER repair_status_versions_bump`},
			[]Voce{{Codice: "schema/oggetto_diverso", Tabella: "repairs", Oggetto: "repair_status_versions_bump"}}},
		{"trigger con il testo cambiato", []string{`DROP TRIGGER historical_documents_no_delete`,
			`CREATE TRIGGER historical_documents_no_delete BEFORE DELETE ON historical_documents BEGIN SELECT RAISE(ABORT, '` + segreto + `'); END`},
			[]Voce{{Codice: "schema/oggetto_diverso", Tabella: "historical_documents", Oggetto: "historical_documents_no_delete"}}},
		// Il testo vero con uno spazio prima di END.
		{"trigger con uno spazio in più", []string{`DROP TRIGGER historical_documents_no_delete`,
			strings.Replace(sqlVero(t, "historical_documents_no_delete"), "\nEND", "\n END", 1)},
			[]Voce{{Codice: "schema/oggetto_diverso", Tabella: "historical_documents", Oggetto: "historical_documents_no_delete"}}},
		{"due trigger in più", []string{`CREATE TRIGGER zeta AFTER INSERT ON artisans BEGIN SELECT 1; END`,
			`CREATE TRIGGER alfa AFTER INSERT ON customers BEGIN SELECT 1; END`},
			[]Voce{{Codice: "schema/oggetto_diverso", Tabella: "customers", Oggetto: "alfa"},
				{Codice: "schema/oggetto_diverso", Tabella: "artisans", Oggetto: "zeta"}}},
		{"vista in più", []string{`CREATE VIEW riassunto AS SELECT '` + segreto + `' AS x`},
			[]Voce{{Codice: "schema/oggetto_diverso", Tabella: "riassunto", Oggetto: "riassunto"}}},
		{"vista con il nome di un trigger", []string{`DROP TRIGGER repair_status_versions_bump`,
			`CREATE VIEW repair_status_versions_bump AS SELECT 1 AS x`},
			// Due differenze: il trigger che manca e la vista in più (nomi separati in SQLite).
			[]Voce{{Codice: "schema/oggetto_diverso", Tabella: "repairs", Oggetto: "repair_status_versions_bump"},
				{Codice: "schema/oggetto_diverso", Tabella: "repair_status_versions_bump", Oggetto: "repair_status_versions_bump"}}},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			dir, o := preparaImport(t, nil)
			ritoccaDopo(t, &o, c.istruzioni...)
			r := controllaBloccato(t, dir, o, importa(t, o))
			if !reflect.DeepEqual(r.Differenze, c.voci) {
				t.Errorf("differenze %s, attese %+v", differenzeDi(r), c.voci)
			}
			// Con lo schema diverso non si copia niente e i media non si guardano.
			for _, tr := range r.Tabelle {
				if len(tr.Scritte) != 0 {
					t.Errorf("%s: scritte %v con lo schema diverso", tr.Standalone, tr.Scritte)
				}
			}
			if r.Media != nil {
				t.Error("media guardati con lo schema diverso")
			}
			if _, b := leggiReport(t, o.Report); bytes.Contains(b, []byte(segreto)) || bytes.Contains(b, []byte("RAISE")) {
				t.Error("il report contiene il testo di un oggetto")
			}
		})
	}
	t.Run("sorgente giusta", func(t *testing.T) {
		_, o := preparaImport(t, nil)
		// Un ritocco che non tocca trigger e viste: lo schema resta quello del riferimento.
		ritoccaDopo(t, &o, `UPDATE artisans SET phone = '+39 000 0000009'`)
		if es := importa(t, o); es.Verdetto != Riconciliato {
			r, _ := leggiReport(t, o.Report)
			t.Fatalf("verdetto %s, differenze %s", es.Verdetto, differenzeDi(r))
		}
	})
}

// Il riferimento dei trigger e delle viste è il file degli oggetti veri, voce per voce (tipo,
// tabella, testo esatto): 28 trigger, tutti nella mappa, nessuna vista.
func TestOggettiDelRiferimento(t *testing.T) {
	rif, err := oggettiDelRiferimento()
	if err != nil {
		t.Fatal(err)
	}
	veri := oggettiVeri(t)
	want := map[chiaveOggetto]oggettoSchema{}
	trigger := 0
	for _, o := range veri {
		want[chiaveOggetto{o.Tipo, o.Nome}] = oggettoSchema{tipo: o.Tipo, tabella: o.Tabella, sql: o.SQL}
		if o.Tipo == "trigger" {
			trigger++
			if _, ok := Trigger[o.Nome]; !ok {
				t.Errorf("trigger %s del riferimento non è nella mappa", o.Nome)
			}
		}
		if o.Tipo != "trigger" && o.Tipo != "view" || o.Nome == "" || o.Tabella == "" || o.SQL == "" {
			t.Errorf("voce %+v", o)
		}
	}
	if !reflect.DeepEqual(rif, want) {
		t.Errorf("riferimento %d oggetti, nel file %d (o con un testo diverso)", len(rif), len(want))
	}
	if len(want) != len(veri) || trigger != len(Trigger) || trigger != 28 || len(veri) != 28 {
		t.Errorf("voci %d, nomi diversi %d, trigger %d", len(veri), len(want), trigger)
	}
}

// riduciSpazi porta un testo SQL a una forma senza impaginazione: ogni sequenza di spazi e a capo
// diventa uno spazio, e gli spazi accanto a ( e ) si tolgono.
func riduciSpazi(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return regexp.MustCompile(` ?([()]) ?`).ReplaceAllString(s, "$1")
}

// Il file degli oggetti veri e la fixture .sql vengono da due strade (il database della catena e le
// migrazioni tradotte): stessi nomi, tipi e tabelle, e lo stesso testo a meno dell'impaginazione.
// Se si rigenera uno dei due e non l'altro, questo test lo dice.
func TestOggettiVeriComeLaFixture(t *testing.T) {
	c, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	c.SetMaxOpenConns(1)
	if _, err := c.Exec(ddlStandalone); err != nil {
		t.Fatal(err)
	}
	rows, err := c.Query(`SELECT type, name, tbl_name, sql FROM sqlite_schema WHERE type IN ('trigger', 'view') ORDER BY type, name`)
	if err != nil {
		t.Fatal(err)
	}
	var fixture []oggettoVero
	for rows.Next() {
		var o oggettoVero
		if err := rows.Scan(&o.Tipo, &o.Nome, &o.Tabella, &o.SQL); err != nil {
			t.Fatal(err)
		}
		fixture = append(fixture, o)
	}
	chiudiRighe(t, rows)
	veri := oggettiVeri(t)
	if !slices.IsSortedFunc(veri, func(a, b oggettoVero) int { return strings.Compare(a.Tipo+" "+a.Nome, b.Tipo+" "+b.Nome) }) {
		t.Error("il file degli oggetti veri non è ordinato per tipo e nome")
	}
	if len(veri) != len(fixture) {
		t.Fatalf("oggetti veri %d, nella fixture %d", len(veri), len(fixture))
	}
	for i, v := range veri {
		f := fixture[i]
		if v.Tipo != f.Tipo || v.Nome != f.Nome || v.Tabella != f.Tabella {
			t.Errorf("voce %d: vera %s %s su %s, fixture %s %s su %s", i, v.Tipo, v.Nome, v.Tabella, f.Tipo, f.Nome, f.Tabella)
			continue
		}
		if riduciSpazi(v.SQL) != riduciSpazi(f.SQL) {
			t.Errorf("%s: testo diverso anche senza impaginazione\nvero:    %s\nfixture: %s", v.Nome, riduciSpazi(v.SQL), riduciSpazi(f.SQL))
		}
	}
}

// Il testo esatto conta: tra la fixture e il file vero solo l'impaginazione cambia, ma per l'import
// è un altro testo (la decisione dice testo esatto).
func TestRiduciSpazi(t *testing.T) {
	if got := riduciSpazi("CREATE  TRIGGER x\n  WHEN NOT EXISTS (\n SELECT 1 )\nBEGIN SELECT 1; END"); got != "CREATE TRIGGER x WHEN NOT EXISTS(SELECT 1)BEGIN SELECT 1; END" {
		t.Errorf("%q", got)
	}
}

// Un riferimento rovinato (non JSON, un nome ripetuto) è un errore, non un elenco a metà.
func TestRiferimentoRovinato(t *testing.T) {
	for _, b := range []string{`{`, `[{"tipo":"trigger","nome":"a","tabella":"x","sql":"s"},{"tipo":"trigger","nome":"a","tabella":"y","sql":"t"}]`} {
		if m, err := riferimentoDa([]byte(b)); err == nil {
			t.Errorf("%s: nessun errore, %v", b, m)
		}
	}
	m, err := riferimentoDa([]byte(`[{"tipo":"trigger","nome":"a","tabella":"x","sql":"s"},{"tipo":"view","nome":"v","tabella":"v","sql":"w"}]`))
	if err != nil || !reflect.DeepEqual(m, map[chiaveOggetto]oggettoSchema{{"trigger", "a"}: {"trigger", "x", "s"}, {"view", "v"}: {"view", "v", "w"}}) {
		t.Errorf("%v %v", m, err)
	}
}

// Trigger e viste hanno nomi separati in SQLite: una vista con il nome di un trigger del
// riferimento, con il trigger ricreato uguale, è un oggetto in più e blocca (review della 025).
// inventario: OPS-01
func TestVistaConIlNomeDiUnTriggerRicreato(t *testing.T) {
	_, o := preparaImport(t, nil)
	ritoccaDopo(t, &o,
		`DROP TRIGGER repair_status_versions_bump`,
		`CREATE VIEW repair_status_versions_bump AS SELECT 'vista-intrusa' AS x`,
		sqlVero(t, "repair_status_versions_bump"))
	es := importa(t, o)
	r, _ := leggiReport(t, o.Report)
	if es.Verdetto == Riconciliato {
		t.Fatal("vista in più non bloccata")
	}
	if d := differenzeDi(r); !strings.Contains(d, "schema/oggetto_diverso") {
		t.Errorf("differenze %s, attesa schema/oggetto_diverso", d)
	}
}
