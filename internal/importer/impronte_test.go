package importer

import (
	"bytes"
	"database/sql"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Le impronte salvate come testo dallo standalone (D-42): solo nella colonna sha256 di
// historical_documents, sale_photos e sale_revision_photos un TEXT di esattamente 32 byte si
// accetta, si scrive nel Go come BLOB con gli stessi byte e il report lo dice con un avviso per
// tabella e il numero di righe. Lo standalone lo fa con un Repo.query e un binario semplice: qui
// lo stesso effetto con CAST(… AS TEXT), che tiene i byte.

// tabellaImpronta è una tabella di D-42 vista da un test: la tabella Go e come aggiungere nella
// sorgente una seconda riga con il file media n. Il nome del file della seconda riga è un TEXT di
// 32 byte: fuori dalla colonna sha256 resta testo.
type tabellaImpronta struct {
	standalone, goTab string
	seconda           func(c *sql.DB, n int)
}

var tabelleImpronta = []tabellaImpronta{
	{"historical_documents", "documenti_storici", func(c *sql.DB, n int) {
		f := (&sintetico{}).foto(n)
		esegui(c, `INSERT INTO historical_documents (source_filename, storage_key, byte_size, sha256, inserted_at)
			VALUES ('documento-inventato-numero-2.pdf', ?, ?, ?, '2024-02-02T00:00:00.000000Z')`, f["storage_key"], f["byte_size"], f["sha256"])
	}},
	{"sale_photos", "foto_vendite", func(c *sql.DB, n int) {
		f := (&sintetico{}).foto(n)
		esegui(c, `INSERT INTO sale_photos (sale_id, position, storage_key, original_filename, mime_type, byte_size, sha256, inserted_at)
			VALUES (1, 2, ?, 'foto-inventata-numero-due-xx.jpg', 'image/jpeg', ?, ?, '2024-02-02T00:00:00.000000Z')`, f["storage_key"], f["byte_size"], f["sha256"])
	}},
	{"sale_revision_photos", "foto_revisioni_vendite", func(c *sql.DB, n int) {
		f := (&sintetico{}).foto(n)
		esegui(c, `INSERT INTO sale_revision_photos (sale_revision_id, position, storage_key, original_filename, mime_type, byte_size, sha256, inserted_at)
			VALUES (1, 2, ?, 'foto-inventata-numero-due-xx.jpg', 'image/jpeg', ?, ?, '2024-02-02T00:00:00.000000Z')`, f["storage_key"], f["byte_size"], f["sha256"])
	}},
}

// improntaComeTesto riscrive come TEXT (gli stessi byte) lo sha256 delle righe di tab scelte da
// where.
func improntaComeTesto(c *sql.DB, tab, where string) {
	esegui(c, `UPDATE `+tab+` SET sha256 = CAST(sha256 AS TEXT) WHERE `+where)
}

// impronteGo dà per ogni riga di tabGo (in ordine di id) typeof(sha256) e i byte.
func impronteGo(t *testing.T, p, tabGo string) (tipi []string, valori [][]byte) {
	t.Helper()
	rows, err := apriLettura(t, p).Query(`SELECT typeof(sha256), CAST(sha256 AS BLOB) FROM ` + tabGo + ` ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var tipo string
		var b []byte
		if err := rows.Scan(&tipo, &b); err != nil {
			t.Fatal(err)
		}
		tipi, valori = append(tipi, tipo), append(valori, b)
	}
	chiudiRighe(t, rows)
	return tipi, valori
}

// Per ognuna delle tre tabelle: due righe con l'impronta come testo (file diversi) passano,
// riconciliate; l'avviso tipo/impronta_testo dice tabella, colonna e 2 righe; nel Go lo sha256
// è un BLOB con gli stessi 32 byte (lower(hex) = chiave) e i file sono copiati. Poi la tabella
// mista: una riga BLOB e una TEXT, avviso con 1 riga.
func TestImpronteComeTestoAccettate(t *testing.T) {
	for _, tab := range tabelleImpronta {
		for _, misto := range []bool{false, true} {
			nome := tab.standalone
			if misto {
				nome += " mista"
			}
			t.Run(nome, func(t *testing.T) {
				_, o := preparaImport(t, func(c *sql.DB) {
					tab.seconda(c, 1) // il file del logo, che c'è nella cartella media
					if misto {
						improntaComeTesto(c, tab.standalone, "id = 2")
					} else {
						improntaComeTesto(c, tab.standalone, "1")
					}
				})
				// La sorgente si apre solo dopo l'import: aprirla prima lascerebbe un -wal accanto.
				es := importa(t, o)
				r, b := leggiReport(t, o.Report)
				if es.Verdetto != Riconciliato {
					t.Fatalf("verdetto %s, differenze %s", es.Verdetto, differenzeDi(r))
				}
				src := apriLettura(t, o.Sorgente)
				var testi int64
				if err := src.QueryRow(`SELECT count(*) FROM ` + tab.standalone + ` WHERE typeof(sha256) = 'text'`).Scan(&testi); err != nil {
					t.Fatal(err)
				}
				attese := int64(2)
				if misto {
					attese = 1
				}
				if testi != attese {
					t.Fatalf("righe TEXT nella sorgente %d, attese %d", testi, attese)
				}
				want := []Voce{{Codice: "tipo/impronta_testo", Tabella: tab.standalone, Colonna: "sha256", Righe: ptr(attese)}}
				if !reflect.DeepEqual(r.Avvisi, want) {
					t.Errorf("avvisi %+v, attesi %+v", r.Avvisi, want)
				}
				tipi, valori := impronteGo(t, o.Destinazione, tab.goTab)
				if !reflect.DeepEqual(tipi, []string{"blob", "blob"}) {
					t.Errorf("typeof nel Go %v", tipi)
				}
				righeSrc := righe(t, src, `SELECT id, CAST(sha256 AS BLOB), storage_key FROM `+tab.standalone+` ORDER BY id`, 3)
				for i, v := range valori {
					s := righeSrc[fmt.Sprint(i+1)]
					if len(v) != 32 || !bytes.Equal(v, s[1].([]byte)) || hex.EncodeToString(v) != s[2] {
						t.Errorf("riga %d: sha256 nel Go %x, sorgente %x, chiave %v", i+1, v, s[1], s[2])
					}
				}
				var tipoNome string
				if err := apriLettura(t, o.Destinazione).QueryRow(`SELECT typeof(` + colonnaGoDi(mappaDi(tab.standalone), nomeFileDi(tab.standalone)) +
					`) FROM ` + tab.goTab + ` WHERE id = 2`).Scan(&tipoNome); err != nil || tipoNome != "text" {
					t.Errorf("nome del file nel Go: %q %v", tipoNome, err)
				}
				if f := famiglia(r, tab.standalone); f.Attesi != 2 || f.Copiati != 2 || f.HashDiversi != 0 {
					t.Errorf("media di %s: %+v", tab.standalone, f)
				}
				if bytes.Contains(b, []byte(chiaveMedia(1))) {
					t.Error("il report contiene un'impronta")
				}
			})
		}
	}
}

// Con l'impronta come testo il controllo del file resta lo stesso: un TEXT di 32 byte che non è lo
// SHA-256 del file (la chiave sì) blocca con media/hash_diverso sulla colonna sha256.
func TestImprontaComeTestoDiUnAltroFile(t *testing.T) {
	dir, o := preparaImport(t, func(c *sql.DB) {
		esegui(c, `UPDATE historical_documents SET sha256 = CAST(? AS TEXT)`, impronta(77))
	})
	r := controllaBloccato(t, dir, o, importa(t, o))
	if !ha(r.Differenze, "media/hash_diverso", "historical_documents", 1, "sha256") {
		t.Errorf("differenze %s, manca media/hash_diverso", differenzeDi(r))
	}
}

// Ogni altra forma blocca come prima, con tipo/blob_atteso su tabella, id e colonna e senza
// l'avviso: un TEXT di 31 o 33 byte, i 64 caratteri esadecimali, un INTEGER, un REAL nelle tre
// colonne di D-42; un TEXT di 32 byte in un'altra colonna BLOB (anche di una tabella delle
// vendite) o nello sha256 di un'altra tabella.
func TestImpronteInAltreFormeBloccano(t *testing.T) {
	type caso struct {
		nome, tab, col, valore string
		id                     int64
		// altro è un'altra assegnazione dello stesso UPDATE, per i CHECK della sorgente.
		altro string
	}
	var casi []caso
	for _, tab := range tabelleImpronta {
		for _, v := range []struct{ nome, valore string }{
			{"31 byte", `CAST(substr(sha256, 1, 31) AS TEXT)`},
			{"33 byte", `CAST(sha256 || X'00' AS TEXT)`},
			{"64 esadecimali", `lower(hex(sha256))`},
			// 32 caratteri ma 64 byte: si contano i byte, non i caratteri.
			{"32 caratteri non ASCII", `'` + strings.Repeat("è", 32) + `'`},
			{"intero", `1234`},
			{"reale", `12.5`},
		} {
			casi = append(casi, caso{tab.standalone + " " + v.nome, tab.standalone, "sha256", v.valore, 1, ""})
		}
	}
	trentadue := "'" + strings.Repeat("a", 32) + "'" // 32 byte ASCII: passa anche il CHECK length(sha256) = 32 del logo
	for _, c := range []struct{ tab, col, altro string }{
		{"repair_photos", "sha256", ""},
		// Il CHECK del logo vuole chiave = lower(hex(sha256)) anche nello standalone.
		{"branding_assets", "sha256", ", storage_key = lower(hex(" + trentadue + "))"},
		{"password_resets", "token_hash", ""},
		{"user_invitations", "token_hash", ""},
		{"telegram_link_tokens", "token_hash", ""},
		{"repairs", "public_token_hash", ""},
		{"sale_revisions", "public_token_hash", ""},
		{"legacy_import_captures", "source_sha256", ""},
	} {
		casi = append(casi, caso{c.tab + "." + c.col + " 32 byte", c.tab, c.col, trentadue, 1, c.altro})
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			dir, o := preparaImport(t, func(db *sql.DB) {
				esegui(db, `UPDATE `+c.tab+` SET `+c.col+` = `+c.valore+c.altro+` WHERE rowid = ?`, c.id)
				var tipo string
				var n int64
				if err := db.QueryRow(`SELECT typeof(`+c.col+`), length(CAST(`+c.col+` AS BLOB)) FROM `+c.tab+` WHERE rowid = ?`, c.id).Scan(&tipo, &n); err != nil {
					panic(err)
				}
				atteso := map[string]int64{"32 byte": 32, "31 byte": 31, "33 byte": 33, "32 caratteri non ASCII": 64}
				for suff, l := range atteso {
					if strings.HasSuffix(c.nome, suff) && (tipo != "text" || n != l) {
						panic(fmt.Sprintf("il caso non è un TEXT di %d byte: %s di %d", l, tipo, n))
					}
				}
			})
			r := controllaBloccato(t, dir, o, importa(t, o))
			if !ha(r.Differenze, "tipo/blob_atteso", c.tab, c.id, c.col) {
				t.Errorf("differenze %s, manca tipo/blob_atteso su %s.%s", differenzeDi(r), c.tab, c.col)
			}
			for _, a := range r.Avvisi {
				if a.Codice == "tipo/impronta_testo" {
					t.Errorf("avviso %+v per una forma che blocca", a)
				}
			}
		})
	}
}

// mappaDi dà la tabella della mappa di una tabella dello standalone.
func mappaDi(standalone string) Tabella {
	for _, t := range Mappa {
		if t.Standalone == standalone {
			return t
		}
	}
	return Tabella{}
}

// nomeFileDi è la colonna del nome del file originale nelle tabelle di D-42.
func nomeFileDi(standalone string) string {
	if standalone == "historical_documents" {
		return "source_filename"
	}
	return "original_filename"
}
