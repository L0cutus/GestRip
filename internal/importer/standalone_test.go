package importer

import (
	"database/sql"
	_ "embed"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/db"
)

// Lo schema finale dello standalone a go-baseline (6eb93ed), come DDL: le 60 migrazioni di
// priv/repo/migrations tradotte in SQL e applicate in ordine da tools/schema-standalone
// (rigenera.sh, vedi il suo README), poi il dump di sqlite_schema, senza dati. I test lo caricano
// in un database vuoto e lo leggono con gli stessi pragma con cui leggono il Go, così il
// confronto è simmetrico. L'import (slice successiva) lo confronta anche con un database vero
// dello standalone, quello sintetico del banco (T3).
//
//go:embed testdata/standalone-go-baseline.sql
var ddlStandalone string

// Le chiavi segrete di settings, ricavate dal sorgente dello standalone dallo stesso strumento.
//
//go:embed testdata/chiavi-segrete-standalone.txt
var chiaviSegreteStandalone string

// colonnaSchema è una riga di pragma_table_info. nonNulla vale per NOT NULL o chiave primaria:
// pragma dà notnull 0 a una chiave primaria anche quando non può essere nulla, e così il confronto
// tra standalone e Go non dipende da come è scritta.
type colonnaSchema struct {
	nome, tipo  string
	nonNulla    bool
	chiave      bool
	predefinito string // "" se non c'è
}

type indiceSchema struct {
	tab      string
	cols     []string
	unico    bool
	parziale bool
	where    string
}

type esternaSchema struct{ tab, col, padre, a, azione string }

// schemaDB è uno schema letto da un database: tabelle (con le interne), indici, chiavi esterne,
// trigger.
type schemaDB struct {
	colonne map[string][]colonnaSchema
	sqlTab  map[string]string
	indici  []indiceSchema
	esterne []esternaSchema
	trigger map[string]string
}

func interroga(t *testing.T, q *sql.DB, s string, args ...any) *sql.Rows {
	t.Helper()
	rows, err := q.Query(s, args...)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func leggiSchema(t *testing.T, q *sql.DB) schemaDB {
	t.Helper()
	s := schemaDB{colonne: map[string][]colonnaSchema{}, sqlTab: map[string]string{}, trigger: map[string]string{}}
	rows := interroga(t, q, `SELECT m.name, coalesce(m.sql, ''), p.name, p.type, p."notnull", p.pk, coalesce(p.dflt_value, '')
		FROM sqlite_schema m, pragma_table_info(m.name) p WHERE m.type = 'table' ORDER BY m.name, p.cid`)
	for rows.Next() {
		var tab, ddl string
		var c colonnaSchema
		var nn, pk int
		if err := rows.Scan(&tab, &ddl, &c.nome, &c.tipo, &nn, &pk, &c.predefinito); err != nil {
			t.Fatal(err)
		}
		c.nonNulla, c.chiave = nn == 1 || pk > 0, pk > 0
		s.colonne[tab] = append(s.colonne[tab], c)
		s.sqlTab[tab] = ddl
	}
	chiudiRighe(t, rows)
	rows = interroga(t, q, `SELECT m.name, l.name, l."unique", l.partial, coalesce(i.sql, ''),
			(SELECT group_concat(name, ',') FROM (SELECT x.name FROM pragma_index_info(l.name) x ORDER BY x.seqno))
		FROM sqlite_schema m, pragma_index_list(m.name) l LEFT JOIN sqlite_schema i ON i.type = 'index' AND i.name = l.name
		WHERE m.type = 'table' AND l.origin <> 'pk'`)
	for rows.Next() {
		var ix indiceSchema
		var nome, ddl, cols string
		var u, p int
		if err := rows.Scan(&ix.tab, &nome, &u, &p, &ddl, &cols); err != nil {
			t.Fatal(err)
		}
		ix.cols, ix.unico, ix.parziale = strings.Split(cols, ","), u == 1, p == 1
		if i := strings.Index(strings.ToUpper(ddl), " WHERE "); i >= 0 {
			ix.where = ddl[i+7:]
		}
		s.indici = append(s.indici, ix)
	}
	chiudiRighe(t, rows)
	rows = interroga(t, q, `SELECT m.name, f."from", f."table", coalesce(f."to", 'id'), f.on_delete
		FROM sqlite_schema m, pragma_foreign_key_list(m.name) f WHERE m.type = 'table'`)
	for rows.Next() {
		var e esternaSchema
		if err := rows.Scan(&e.tab, &e.col, &e.padre, &e.a, &e.azione); err != nil {
			t.Fatal(err)
		}
		s.esterne = append(s.esterne, e)
	}
	chiudiRighe(t, rows)
	rows = interroga(t, q, `SELECT name, sql FROM sqlite_schema WHERE type = 'trigger'`)
	for rows.Next() {
		var n, ddl string
		if err := rows.Scan(&n, &ddl); err != nil {
			t.Fatal(err)
		}
		s.trigger[n] = ddl
	}
	chiudiRighe(t, rows)
	return s
}

func chiudiRighe(t *testing.T, rows *sql.Rows) {
	t.Helper()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
}

// schemaStandalone carica il DDL di riferimento in un database vuoto e lo legge.
func schemaStandalone(t *testing.T) schemaDB {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "standalone.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := d.Chiudi(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := d.Scrittura.Exec(ddlStandalone); err != nil {
		t.Fatalf("DDL dello standalone: %v", err)
	}
	return leggiSchema(t, d.Scrittura)
}

// schemaGo legge lo schema del Go migrato, senza le tabelle interne.
func schemaGo(t *testing.T, d *db.DB) schemaDB {
	t.Helper()
	s := leggiSchema(t, d.Lettura)
	for tab := range s.colonne {
		if strings.HasPrefix(tab, "sqlite_") || tab == "schema_migrations" {
			delete(s.colonne, tab)
			delete(s.sqlTab, tab)
		}
	}
	return s
}

func (s schemaDB) colonna(tab, nome string) (colonnaSchema, bool) {
	for _, c := range s.colonne[tab] {
		if c.nome == nome {
			return c, true
		}
	}
	return colonnaSchema{}, false
}

// reIdentificatore trova parole e stringhe SQL: le stringhe restano come sono, le parole che sono
// colonne della tabella si traducono.
var reIdentificatore = regexp.MustCompile(`'[^']*'|[A-Za-z_][A-Za-z0-9_]*`)

// traduci riscrive un'espressione SQL di una tabella dello standalone con i nomi delle colonne
// del Go; per una colonna con la traduzione Ruolo anche i valori.
func traduci(tab, gtab, expr string) string {
	cols := colonneDi(tab)[gtab]
	ruolo := false
	for _, t := range Mappa {
		if t.Standalone == tab {
			for _, c := range t.Colonne {
				ruolo = ruolo || c.Traduzione == Ruolo
			}
		}
	}
	return reIdentificatore.ReplaceAllStringFunc(expr, func(w string) string {
		if strings.HasPrefix(w, "'") {
			if ruolo {
				if v, ok := map[string]string{"'owner'": "'titolare'", "'staff'": "'dipendente'"}[w]; ok {
					return v
				}
			}
			return w
		}
		if g, ok := cols[w]; ok {
			return g
		}
		return w
	})
}

// norma toglie spazi, virgolette doppie e maiuscole, per confrontare espressioni scritte in modo
// diverso; true e false diventano 1 e 0, come SQLite li salva.
func norma(s string) string {
	s = strings.ToLower(strings.NewReplacer(`"`, "", "`", "").Replace(s))
	s = regexp.MustCompile(`\btrue\b`).ReplaceAllString(s, "1")
	s = regexp.MustCompile(`\bfalse\b`).ReplaceAllString(s, "0")
	return strings.Join(strings.Fields(s), "")
}

// controlli dà le espressioni dei CHECK di un CREATE TABLE.
func controlli(ddl string) []string {
	var out []string
	for i := 0; i < len(ddl); i++ {
		switch {
		case ddl[i] == '\'':
			j := strings.IndexByte(ddl[i+1:], '\'')
			if j < 0 {
				return out
			}
			i += j + 1
		case strings.EqualFold(ddl[i:min(i+5, len(ddl))], "CHECK") && (i == 0 || !isParola(ddl[i-1])):
			j := i + 5
			for j < len(ddl) && (ddl[j] == ' ' || ddl[j] == '\n' || ddl[j] == '\t') {
				j++
			}
			if j >= len(ddl) || ddl[j] != '(' {
				continue
			}
			fondo, k := 0, j
			for ; k < len(ddl); k++ {
				if ddl[k] == '\'' {
					k += strings.IndexByte(ddl[k+1:], '\'') + 1
					continue
				}
				if ddl[k] == '(' {
					fondo++
				}
				if ddl[k] == ')' {
					fondo--
					if fondo == 0 {
						break
					}
				}
			}
			out = append(out, ddl[j+1:k])
			i = k
		}
	}
	return out
}

func isParola(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// reElenco trova «colonna IN ('a', 'b')» in un'espressione.
var reElenco = regexp.MustCompile(`(?:^|[^a-z0-9_])([a-z0-9_]+) IN \(('[^)]*)\)`)

// elenchi dà, per colonna, l'unione dei valori degli elenchi IN ('…') delle espressioni.
func elenchi(exprs []string) map[string]map[string]bool {
	m := map[string]map[string]bool{}
	for _, e := range exprs {
		for _, x := range reElenco.FindAllStringSubmatch(strings.Join(strings.Fields(e), " "), -1) {
			if m[x[1]] == nil {
				m[x[1]] = map[string]bool{}
			}
			for _, v := range strings.Split(x[2], ",") {
				m[x[1]][strings.Trim(strings.TrimSpace(v), "'")] = true
			}
		}
	}
	return m
}

func chiavi[V any](m map[string]V) []string {
	k := make([]string, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}

func elenco(xs []string) string { return fmt.Sprintf("%q", xs) }

// paroleNomi dice, per ogni parola dei nomi delle colonne dello standalone, con quali parole
// italiane può comparire nel nome Go. Serve a rendere visibile uno scambio tra due colonne dello
// stesso tipo (note pubbliche e interne, città e provincia, imponibile e IVA): il nome Go deve
// essere fatto solo di parole permesse dal nome dello standalone, e ogni parola dello standalone
// che ha delle traduzioni deve comparire con almeno una. Una parola senza traduzioni (final,
// snapshot…) non lascia segno nel nome Go.
var paroleNomi = map[string][]string{
	"accepted": {"accettato", "accettazione"}, "access": {"accesso"}, "action": {"azione"}, "active": {"attivo"},
	"actor": {"attore"}, "address": {"indirizzo"}, "agent": {"agent"}, "approved": {"accettato"},
	"artisan": {"artigiano"}, "at": {"il"}, "attempt": {"tentativo"}, "attempts": {"tentativi"}, "attesa": {"attesa"},
	"author": {"autore"}, "bag": {"busta", "foto"}, "body": {"testo"}, "buyer": {"acquirente"}, "by": {"da"},
	"byte": {"byte"}, "capture": {"cattura"}, "cents": {"centesimi"}, "channel": {"canale"}, "charge": {"addebito"},
	"chase": {"sollecito"}, "chat": {"chat"}, "city": {"citta"}, "cliente": {"cliente"}, "closed": {"chiuso", "chiusa", "chiusura"},
	"code": {"codice"}, "collected": {"ritirata"}, "compressed": {"compressa"}, "compro": {"compro"},
	"confirmed": {"confermata"}, "consent": {"consenso"}, "customer": {"cliente"}, "dal": {"dal"}, "days": {"giorni"},
	"deactivated": {"disattivato"}, "deleted": {"cancellata"}, "delivered": {"consegna"}, "description": {"descrizione"},
	"detail": {"dettaglio"}, "disabled": {"spento"}, "display": {"visibile"}, "document": {"documento"},
	"email": {"email"}, "emergency": {"emergenza"}, "end": {"al"}, "error": {"errore"}, "estimate": {"preventivo"},
	"event": {"evento"}, "expected": {"prevista"}, "expires": {"scade"}, "extra": {"in", "piu"},
	"facebook": {"facebook"}, "failed": {"fallito"}, "filename": {"nome", "file"}, "final": nil,
	"fiscal": {"partita", "iva", "fiscale"}, "for": nil, "from": {"dal"}, "gross": {"lordo"},
	"handed": {"consegnata", "artigiano"}, "handover": {"consegna", "artigiano"}, "hash": {"hash"}, "hours": {"orari"},
	"id": {"id"}, "imported": {"importata"}, "inserted": {"creato"}, "instagram": {"instagram"}, "internal": {"interne"},
	"inviato": {"inviato"}, "ip": {"ip"}, "is": nil, "issued": {"emessa"}, "item": nil, "job": {"lavoro"},
	"json": {"json"}, "key": {"chiave", "nome"}, "kind": {"tipo"}, "last": {"ultimo"}, "legacy": {"flask", "codice"},
	"legal": {"ragione", "sociale"}, "link": {"link"}, "linked": {"collegata"}, "lock": {"blocco"}, "login": {"accesso"},
	"lot": {"lotto"}, "markup": {"tastiera"}, "message": {"messaggio"}, "messages": {"messaggi"}, "metadata": {"dati"},
	"method": nil, "mime": {"mime"}, "mode": {"formato"}, "modello": {"modello"}, "months": {"mesi"}, "motivo": {"motivo"},
	"name": {"nome"}, "next": {"prossimo"}, "notes": {"note"}, "notices": {"avvisi"}, "notified": {"avviso", "avvisato"},
	"number": {"numero"}, "occurred": {"avvenuto"}, "of": {"di"}, "on": {"data"}, "open": {"aperto"}, "opened": {"aperto"},
	"opening": nil, "orefice": {"oreficeria"}, "oreficeria": {"oreficeria"}, "original": nil, "oro": {"oro"}, "out": {"fuori"},
	"outcome": {"esito"}, "outgoing": {"messaggio", "in", "uscita"}, "over": nil, "overdue": {"ritardo"}, "parse": nil,
	"party": {"destinatario", "parte"}, "password": {"password"}, "payment": {"pagamento"}, "percent": nil,
	"phone": {"telefono"}, "pinned": {"fissata"}, "position": {"posizione"}, "postal": {"cap"}, "previous": {"precedente"},
	"price": {"prezzo"}, "proposta": {"proposta"}, "provider": {"provider"}, "province": {"provincia"},
	"public": {"pubblico", "pubbliche"}, "qr": {"qr"}, "queued": {"accodato"}, "quote": {"preventivo"}, "ran": {"eseguito"},
	"rate": {"aliquota"}, "read": {"letto", "letti"}, "ready": {"pronta", "avviso"}, "reason": {"motivo"},
	"recorder": {"registratore"}, "reference": {"riferimento"}, "rejected": {"rifiutato", "rifiuto"}, "repair": {"busta"},
	"reply": nil, "resource": {"risorsa"}, "retention": {"conservazione"}, "returned": {"rientrata"},
	"reversal": {"storno"}, "revision": {"revisione"}, "revoked": {"revocato", "revocata"}, "role": {"ruolo"},
	"round": {"giro"}, "sale": {"vendita"}, "sales": {"vendite"}, "scheduled": {"giorno"}, "scope": {"ambito"},
	"sealed": {"sigillata"}, "seller": {"venditore"}, "sent": {"inviato", "inviata"}, "serial": {"matricola", "serie"},
	"session": {"sessione"}, "sha256": {"sha256"}, "shop": nil, "sidecars": {"file", "accanto"}, "signature": {"firma"},
	"size": {"byte"}, "snapshot": nil, "source": {"flask", "sorgente"}, "start": {"dal"}, "started": {"iniziato", "iniziata"},
	"status": {"stato"}, "stop": {"fermata"}, "stopped": {"fermata"}, "storage": nil, "subject": {"oggetto"},
	"subtitle": {"sottotitolo"}, "table": {"tabella"}, "target": {"standalone"}, "tax": {"fiscale"},
	"taxable": {"imponibile"}, "terms": {"condizioni"}, "testo": {"testo"}, "text": {"testo"}, "thread": {"messaggi"},
	"token": {"token"}, "trigger": {"avvio"}, "type": {"tipo"}, "updated": {"aggiornato", "aggiornata"}, "used": {"usato"},
	"user": {"utente", "user"}, "value": {"valore"}, "vat": {"iva"}, "version": {"versione"}, "vis": {"visibile"},
	"visible": {"visibili"}, "warranty": {"garanzia"}, "website": {"sito"}, "year": {"anno"},
}

// paroleFacoltative sono le parole dello standalone che possono non lasciare segno nel nome Go
// («il» di at, «data» di on, l'id di una partita IVA, il nome di una ragione sociale…).
var paroleFacoltative = map[string]bool{
	"at": true, "on": true, "from": true, "of": true, "by": true, "id": true, "public": true, "display": true,
	"code": true, "bag": true, "name": true, "retention": true,
}
