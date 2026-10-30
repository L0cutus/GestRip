package importer

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// Il database sintetico dello standalone: lo schema esatto della fixture (standalone-go-baseline.sql)
// e righe inventate in tutte le tabelle mappate, con un valore diverso in ogni colonna, così
// l'andata e ritorno vede uno scambio di colonne. I trigger dello standalone si creano dopo le
// righe: sale_revisions_validate_chain confronta le revisioni con la vendita e il profilo, e qui
// i valori sono tutti diversi apposta. Il file finisce come quelli dello standalone: intestazione
// in WAL, nessun -wal accanto.

type valori map[string]any

// sintetico genera i valori: un contatore unico per tutto il database.
type sintetico struct {
	c    *sql.DB
	n    int
	tipi map[string][]colonnaSintetica
}

type colonnaSintetica struct{ nome, tipo string }

// Valori sensibili inventati che il report non deve mai contenere (TestReportSenzaValori).
const (
	emailTitolare    = "titolare.sintetico@esempio.test"
	hashTitolare     = "pbkdf2-sha256$600000$c2FsZXNpbnRldGljbw$aGFzaHNpbnRldGljbw"
	segretoSMTP      = "password-smtp-inventata-7731"
	segretoBot       = "123456:token-bot-inventato-9921"
	tokenPubblico    = "tokenPubblicoInventato_abcdefghijklmnopqrstu"
	nomeCliente      = "Cliente Inventato Rossetti"
	tokenLotto       = "tokenLottoInventato_zyxwvutsrqponm"
	telefonoCliente  = "+39 000 0000001"
	emailNormalizzar = "  Persona.Maiuscola@Esempio.TEST "
)

func impronta(i int) []byte {
	h := sha256.Sum256([]byte(fmt.Sprint("sintetico-", i)))
	return h[:]
}

func (s *sintetico) prossimo() int { s.n++; return s.n }

// traduzioneStandalone dà la traduzione di una colonna dello standalone secondo la mappa.
func traduzioneStandalone(tab, col string) Traduzione {
	for _, t := range Mappa {
		if t.Standalone != tab {
			continue
		}
		for _, c := range t.Colonne {
			if c.Standalone == col {
				return c.Traduzione
			}
		}
	}
	return Uguale
}

// riga dà una riga di tab con un valore inventato e diverso in ogni colonna, poi i valori di over
// (nil: la colonna NULL).
func (s *sintetico) riga(tab string, over valori) valori {
	v := valori{}
	for _, c := range s.tipi[tab] {
		if c.nome == "id" {
			continue // 1, 2, … nell'ordine delle righe, come nello standalone (o dato in over)
		}
		n := s.prossimo()
		switch traduzioneStandalone(tab, c.nome) {
		case Istante:
			v[c.nome] = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(n)*time.Hour + time.Duration(n)*time.Microsecond).
				Format("2006-01-02T15:04:05.000000Z")
		case Data:
			v[c.nome] = time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n).Format(time.DateOnly)
		case Booleano:
			v[c.nome] = int64(n % 2)
		case JSON:
			v[c.nome] = fmt.Sprintf(`{"chiave":"json-%d"}`, n)
		case Ruolo:
			v[c.nome] = "staff"
		case Email:
			v[c.nome] = fmt.Sprintf("persona%d@esempio.test", n)
		default:
			switch c.tipo {
			case "INTEGER":
				v[c.nome] = int64(1000 + n)
			case "BLOB":
				v[c.nome] = impronta(n)
			default:
				v[c.nome] = fmt.Sprintf("v-%s-%d", c.nome, n)
			}
		}
	}
	for k, x := range over {
		v[k] = x
	}
	return v
}

// foto dà i valori di una foto con l'indirizzo del contenuto (chiave = hex dello sha256).
func (s *sintetico) foto(n int) valori {
	h := impronta(10000 + n)
	return valori{"sha256": h, "storage_key": hex.EncodeToString(h)}
}

type rigaSintetica struct {
	tab string
	v   valori
}

func unisci(a, b valori) valori {
	m := valori{}
	for k, x := range a {
		m[k] = x
	}
	for k, x := range b {
		m[k] = x
	}
	return m
}

// righeBase sono le righe del database sintetico completo.
func (s *sintetico) righeBase() []rigaSintetica {
	r := func(tab string, over valori) rigaSintetica { return rigaSintetica{tab, s.riga(tab, over)} }
	bustaComune := valori{"customer_id": int64(1), "oreficeria_id": int64(1), "artisan_id": int64(1), "handover_id": int64(1), "ready_lot_id": int64(1)}
	vendita := valori{"channel": "eBay", "warranty_months": int64(24)}
	return []rigaSintetica{
		r("users", valori{"id": int64(1), "email": emailTitolare, "password_hash": hashTitolare, "active": int64(1), "role": "owner", "deactivated_at": nil}),
		r("users", valori{"id": int64(2), "active": int64(0), "role": "staff"}),
		r("users", valori{"id": int64(3), "active": int64(1), "role": "staff", "password_hash": nil, "deactivated_at": nil}),
		r("user_invitations", valori{"user_id": int64(3)}),
		r("password_resets", valori{"user_id": int64(1), "channel": "email", "ip": "198.51.100.7"}),
		r("settings", valori{"key": "outgoing.smtp.password", "value": segretoSMTP}),
		r("settings", valori{"key": "outgoing.telegram.bot_token", "value": segretoBot}),
		r("settings", valori{"key": "assist.ai.api_key", "value": "chiave-ai-inventata-1"}),
		r("settings", valori{"key": "assist.stt.api_key", "value": "chiave-stt-inventata-2"}),
		r("settings", valori{"key": "outgoing.smtp.host", "value": "smtp.esempio.test"}),
		r("settings", valori{"key": "jobs.overdue.silenced_history", "value": "2026-02-03T04:05:06.000000Z"}),
		r("audit_events", valori{"inserted_at": "2015-05-06T07:08:09.123456Z"}),
		r("audit_events", valori{"inserted_at": "2016-01-02T03:04:05.654321Z"}),
		r("audit_events", nil),
		r("shop_profiles", valori{"scope": "default"}),
		r("branding_assets", unisci(s.foto(1), valori{"kind": "logo", "byte_size": int64(4321), "mime_type": "image/png"})),
		r("customers", valori{"id": int64(1), "name": nomeCliente, "phone": telefonoCliente}),
		r("customers", valori{"id": int64(2)}),
		r("bag_sequences", valori{"year": int64(2026)}),
		r("oreficerie", valori{"id": int64(1)}),
		r("oreficerie", valori{"id": int64(2)}),
		r("artisans", valori{"id": int64(1)}),
		r("handovers", valori{"artisan_id": int64(1)}),
		r("ready_lots", valori{"oreficeria_id": int64(1), "token": tokenLotto}),
		r("repairs", unisci(bustaComune, valori{"id": int64(1), "bag_number": "1/26", "status": "pronta", "compro_oro": int64(0),
			"public_token": tokenPubblico, "retention_started_at": "2025-03-04T05:06:07.123Z", "deleted_at": "2025-03-05T05:06:07.5Z"})),
		r("repairs", unisci(bustaComune, valori{"id": int64(2), "bag_number": "2/26", "status": "valutata", "compro_oro": int64(1),
			"customer_id": int64(2)})),
		r("repair_status_versions", valori{"repair_id": int64(1)}),
		r("repair_photos", unisci(s.foto(2), valori{"repair_id": int64(1), "is_bag": int64(1), "position": int64(1)})),
		r("repair_messages", valori{"repair_id": int64(1)}),
		r("artisan_reminders", valori{"repair_id": int64(1), "artisan_id": int64(1), "handover_id": int64(1)}),
		r("outgoing_messages", valori{"repair_id": int64(1), "channel": "telegram", "party": "customer", "status": "queued"}),
		r("ready_notice_events", valori{"repair_id": int64(1), "event": "notified", "outgoing_message_id": int64(1)}),
		r("quote_decision_events", valori{"repair_id": int64(1), "event": "rejected", "party": "customer"}),
		r("delivery_signature_events", valori{"repair_id": int64(1), "event": "captured", "storage_key": hex.EncodeToString(impronta(20000))}),
		r("telegram_subscriptions", valori{"repair_id": int64(1)}),
		r("telegram_link_tokens", valori{"repair_id": int64(1)}),
		r("dettati_addestramento", valori{"repair_id": int64(1)}),
		r("dettati_esclusioni", valori{"customer_id": int64(1)}),
		r("courtesy_document_sequences", valori{"year": int64(2026)}),
		r("sales", unisci(vendita, valori{"id": int64(1), "status": "returned", "document_year": int64(2026)})),
		r("sale_revisions", unisci(vendita, valori{"id": int64(1), "sale_id": int64(1), "previous_revision_id": nil, "revision_number": int64(1),
			"channel": "Etsy", "warranty_months": int64(12)})),
		r("sale_revisions", unisci(vendita, valori{"id": int64(2), "sale_id": int64(1), "previous_revision_id": int64(1), "revision_number": int64(2),
			"channel": "Vinted", "warranty_months": int64(6)})),
		r("sale_photos", unisci(s.foto(3), valori{"sale_id": int64(1), "position": int64(1)})),
		r("sale_revision_photos", unisci(s.foto(4), valori{"sale_revision_id": int64(1), "position": int64(1)})),
		r("sale_revision_seals", valori{"sale_revision_id": int64(1)}),
		r("sale_revision_revocations", valori{"sale_revision_id": int64(2)}),
		r("emergency_events", valori{"id": int64(1), "open_lock": int64(1)}),
		r("emergency_entries", valori{"id": int64(1), "emergency_event_id": int64(1), "document_year": int64(2026), "gross_cents": int64(12200),
			"taxable_cents": int64(10000), "vat_cents": int64(2200), "vat_rate_percent": int64(22), "repair_id": int64(1), "reversal_of_id": nil}),
		r("emergency_entries", valori{"id": int64(2), "emergency_event_id": int64(1), "document_year": int64(2026), "gross_cents": int64(-6100),
			"taxable_cents": int64(-5000), "vat_cents": int64(-1100), "vat_rate_percent": int64(22), "repair_id": int64(1), "reversal_of_id": int64(1)}),
		r("job_runs", valori{"job": "overdue", "trigger": "manual", "outcome": "ok"}),
		r("media_deletion_jobs", nil),
		r("historical_documents", unisci(s.foto(5), valori{"byte_size": int64(5555)})),
		r("legacy_import_mappings", nil),
		r("legacy_import_captures", valori{"id": int64(1)}),
	}
}

func inserisci(t *testing.T, c *sql.DB, tab string, v valori) {
	t.Helper()
	var cols, segni []string
	var args []any
	for _, k := range chiavi(v) {
		cols = append(cols, `"`+k+`"`)
		segni = append(segni, "?")
		args = append(args, v[k])
	}
	if _, err := c.Exec(fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`, tab, strings.Join(cols, ", "), strings.Join(segni, ", ")), args...); err != nil {
		t.Fatalf("riga sintetica in %s: %v", tab, err)
	}
}

// creaSorgente scrive in p il database sintetico dello standalone; ritocca, se c'è, lo cambia prima
// dei trigger (un caso da provare). vuoto: solo lo schema, senza righe.
func creaSorgente(t *testing.T, p string, vuoto bool, ritocca func(c *sql.DB)) {
	t.Helper()
	c, err := sql.Open("sqlite", "file:"+p)
	if err != nil {
		t.Fatal(err)
	}
	c.SetMaxOpenConns(1)
	esegui := func(q string, args ...any) {
		t.Helper()
		if _, err := c.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", strings.SplitN(q, "\n", 2)[0], err)
		}
	}
	esegui(`PRAGMA journal_mode = WAL`)
	var trigger []string
	for i, st := range strings.Split(ddlStandalone, "\n\nCREATE ") {
		if i > 0 {
			st = "CREATE " + st
		}
		if strings.HasPrefix(st, "CREATE TRIGGER") {
			trigger = append(trigger, st)
			continue
		}
		esegui(st)
	}
	if len(trigger) != len(Trigger) {
		t.Fatalf("trigger della fixture %d, nella mappa %d", len(trigger), len(Trigger))
	}
	if !vuoto {
		s := &sintetico{c: c, tipi: map[string][]colonnaSintetica{}}
		rows, err := c.Query(`SELECT m.name, p.name, p.type FROM sqlite_schema m, pragma_table_info(m.name) p WHERE m.type = 'table' ORDER BY m.name, p.cid`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var tab, col, tipo string
			if err := rows.Scan(&tab, &col, &tipo); err != nil {
				t.Fatal(err)
			}
			s.tipi[tab] = append(s.tipi[tab], colonnaSintetica{col, tipo})
		}
		chiudiRighe(t, rows)
		for _, r := range s.righeBase() {
			inserisci(t, c, r.tab, r.v)
		}
		inserisci(t, c, "schema_migrations", valori{"version": int64(20260905070000), "inserted_at": "2026-09-05T07:00:00"})
		inserisci(t, c, "schema_migrations", valori{"version": int64(20261027200100), "inserted_at": "2026-10-27T20:01:00"})
		// Un seq più alto del max id (righe cancellate nello standalone) e uno più basso (seq
		// rovinato): nel Go vale il più alto dei due.
		esegui(`UPDATE sqlite_sequence SET seq = 40 WHERE name = 'users'`)
		esegui(`UPDATE sqlite_sequence SET seq = 1 WHERE name = 'customers'`)
	}
	if ritocca != nil {
		ritocca(c)
	}
	for _, tr := range trigger {
		esegui(tr)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	for _, s := range fileAccanto {
		if _, err := os.Lstat(p + s); err == nil {
			t.Fatalf("il database sintetico ha lasciato %s", s)
		}
	}
}

func shaDi(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// istanteImport è l'ora dell'orologio finto dell'import.
var istanteImport = time.Date(2026, 10, 30, 21, 0, 0, 0, time.UTC)

// preparaImport crea in una cartella nuova la sorgente sintetica (con ritocca) e dà le opzioni di
// un import sintetico verso destinazione e report nella stessa cartella.
func preparaImport(t *testing.T, ritocca func(*sql.DB)) (string, Opzioni) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "standalone.db")
	creaSorgente(t, src, false, ritocca)
	return dir, Opzioni{Sorgente: src, Destinazione: filepath.Join(dir, "gestrip.db"), Report: filepath.Join(dir, "report.json"),
		Sha256: shaDi(t, src), Sintetico: true, Orologio: clock.NuovoFinto(istanteImport)}
}

// fileDi elenca i nomi nella cartella.
func fileDi(t *testing.T, dir string) []string {
	t.Helper()
	voci, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var nomi []string
	for _, v := range voci {
		nomi = append(nomi, v.Name())
	}
	return nomi
}
