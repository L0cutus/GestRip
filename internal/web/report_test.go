package web

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/pdf/lettura"
)

// Il report multi-scheda A4 (RIP-S-46, GET /repairs/report.pdf), come RepairReportController e i suoi
// test dello standalone a go-baseline; i testi e le intestazioni delle risposte sono quelli misurati
// sul banco (fab-220, 03/11/2026).

const percorsoReport = "/repairs/report.pdf"

// inventario: RIP-S-46
// Il valore di ids come lo decodifica Plug.Conn.Query (misurato nell'immagine dello standalone):
// vale l'ultima assegnazione, che decide anche il tipo (testo, lista con «[]», mappa con «[k]»); «+»
// è uno spazio, la chiave si decodifica prima («%69ds»); una chiave con parentesi non chiuse in fondo
// è un'altra chiave; «ids[][a]» mette tutto in una sola mappa dentro la lista. Un valore che non è un
// testo si scrive come inspect/1: virgolette e «\», «\#{», gli escape dei caratteri di controllo che
// Elixir conosce, \uXXXX per gli invisibili, <<…>> per un testo non stampabile, al più 50 elementi e
// 4096 caratteri, le chiavi delle mappe in ordine.
func TestIdsComePlug(t *testing.T) {
	for _, c := range []struct {
		query, testo, inspect string
		presente              bool
	}{
		{"ids=3,1,2", "3,1,2", "", true},
		{"ids=1&ids=2", "2", "", true},
		{"ids=1+2", "1 2", "", true},
		{"ids=1%2C2", "1,2", "", true},
		{"ids=1;2", "1;2", "", true},
		{"%69ds=1", "1", "", true},
		{"ids", "", "", true},
		{"ids=1&ids", "", "", true},
		{"", "", "", false},
		{"altro=1", "", "", false},
		{"ids[=1", "", "", false},
		{"ids]=1", "", "", false},
		{"ids[a=1", "", "", false},
		{"ids[a]b=1", "", "", false},
		{"ids[]a=1", "", "", false},
		{"ids+[]=1", "", "", false},
		{"ids[]=1&ids=2", "2", "", true},
		{"ids%5B%5D=1&ids%5B%5D=2", "", `["1", "2"]`, true},
		{"ids=1&ids[]=2", "", `["2"]`, true},
		{"ids&ids[]=1", "", `["1"]`, true},
		{"ids[]=", "", `[""]`, true},
		{"ids[]", "", `[""]`, true},
		{"ids[]=1&ids[]=&ids[]=3", "", `["1", "", "3"]`, true},
		{"ids[a]=1", "", `%{"a" => "1"}`, true},
		{"ids[a][b]=1", "", `%{"a" => %{"b" => "1"}}`, true},
		{"ids[b]=1&ids[a]=2", "", `%{"a" => "2", "b" => "1"}`, true},
		{"ids[a]=1&ids[a]=2", "", `%{"a" => "2"}`, true},
		{"ids[]=1&ids[a]=2", "", `%{"a" => "2"}`, true},
		{"ids[a]=1&ids[]=2", "", `["2"]`, true},
		{"ids[][a]=1", "", `[%{"a" => "1"}]`, true},
		{"ids[][a]=1&ids[][b]=2", "", `[%{"a" => "1", "b" => "2"}]`, true},
		{"ids[][a]=1&ids[][a]=2", "", `[%{"a" => "2"}]`, true},
		{"ids[a][]=1&ids[a][]=2", "", `%{"a" => ["1", "2"]}`, true},
		{"ids[b]=1&ids[a]=2&ids[c][]=x", "", `%{"a" => "2", "b" => "1", "c" => ["x"]}`, true},
		{"ids[1]=a&ids[0]=b", "", `%{"0" => "b", "1" => "a"}`, true},
		{"ids[%20a%20]=1", "", `%{" a " => "1"}`, true},
		{"ids[[a]]=1", "", `%{"[a]" => "1"}`, true},
		{"ids[]=%22a%22", "", `["\"a\""]`, true},
		{"ids[]=%5Cn", "", `["\\n"]`, true},
		{"ids[]=%23%7B", "", `["\#{"]`, true},
		{"ids[]=%23x", "", `["#x"]`, true},
		{"ids[]=%01", "", `[<<1>>]`, true},
		{"ids[]=%00", "", `[<<0>>]`, true},
		{"ids[]=1%01", "", `[<<49, 1>>]`, true},
		{"ids[]=%C2%85", "", `[<<194, 133>>]`, true},
		{"ids[]=%EF%BF%BE", "", `[<<239, 191, 190>>]`, true},
		{"ids[]=%7F", "", `["\d"]`, true},
		{"ids[]=%09", "", `["\t"]`, true},
		{"ids[]=%0D%0A", "", `["\r\n"]`, true},
		{"ids[]=%1B", "", `["\e"]`, true},
		{"ids[]=%0B%0C%08%07", "", `["\v\f\b\a"]`, true},
		{"ids[]=%E2%80%8B", "", `["\u200B"]`, true},
		{"ids[]=%E2%80%83", "", `["\u2003"]`, true},
		{"ids[]=%C2%A0", "", `["\u00A0"]`, true},
		{"ids[]=%EF%BB%BF", "", `["\uFEFF"]`, true},
		{"ids[]=%E2%80%A8", "", `["\u2028"]`, true},
		{"ids[]=%CD%8F%D8%9C%E2%81%A4%E2%81%A6%EF%BF%B9", "", `["\u034F\u061C\u2064\u2066\uFFF9"]`, true},
		{"ids[]=%E2%81%A5", "", "[\"⁥\"]", true},
		{"ids[]=%F0%9F%93%B7%C3%A8", "", `["📷è"]`, true},
	} {
		v, ok := idsDellaQuery(c.query)
		if ok != c.presente {
			t.Errorf("%q: presente %v", c.query, ok)
			continue
		}
		if !ok {
			continue
		}
		testo, eTesto := v.testo()
		if eTesto != (c.inspect == "") || testo != c.testo || (!eTesto && v.inspect() != c.inspect) {
			t.Errorf("%q: testo %v %q, inspect %q", c.query, eTesto, testo, v.inspect())
		}
	}

	// I limiti di inspect/1 (misurati): 50 elementi (un annidato ne ha uno in meno, più uno per ogni
	// elemento prima), 50 byte di un testo non stampabile (49 dentro una lista), 4096 caratteri di un testo.
	var q, attesi []string
	for i := 1; i <= 60; i++ {
		q = append(q, "ids[]="+strconv.Itoa(i))
		if i <= 50 {
			attesi = append(attesi, `"`+strconv.Itoa(i)+`"`)
		}
	}
	v, _ := idsDellaQuery(strings.Join(q, "&"))
	if got := v.inspect(); got != "["+strings.Join(attesi, ", ")+", ...]" {
		t.Errorf("60 elementi: %q", got)
	}
	v, _ = idsDellaQuery(strings.ReplaceAll(strings.Join(q, "&"), "ids[]", "ids[a][]"))
	if got := v.inspect(); got != `%{"a" => [`+strings.Join(attesi[:49], ", ")+", ...]}" {
		t.Errorf("annidati: %q", got)
	}
	// Due chiavi con 60 elementi (misurato): la prima voce della mappa ne mostra 49, la seconda 48.
	v, _ = idsDellaQuery(strings.ReplaceAll(strings.Join(q, "&"), "ids[]", "ids[a][]") + "&" +
		strings.ReplaceAll(strings.Join(q, "&"), "ids[]", "ids[b][]"))
	if got := v.inspect(); got != `%{"a" => [`+strings.Join(attesi[:49], ", ")+`, ...], "b" => [`+strings.Join(attesi[:48], ", ")+", ...]}" {
		t.Errorf("due chiavi: %q", got)
	}
	v, _ = idsDellaQuery("ids[]=" + strings.Repeat("%01", 60))
	if got := v.inspect(); got != "[<<"+strings.TrimSuffix(strings.Repeat("1, ", 48), ", ")+", 1, ...>>]" {
		t.Errorf("binario lungo: %q", got)
	}
	v, _ = idsDellaQuery("ids[]=" + strings.Repeat("a", 4100))
	if got := v.inspect(); got != `["`+strings.Repeat("a", 4096)+`" <> ...]` {
		t.Errorf("testo lungo: %d %q", len(got), got[len(got)-20:])
	}
	v, _ = idsDellaQuery("ids[]=" + strings.Repeat("%C3%A9", 3000))
	if got := v.inspect(); got != `["`+strings.Repeat("é", 3000)+`"]` {
		t.Errorf("3000 é: %d", len(got))
	}
}

// bustaReport crea una busta con il numero dato.
func bustaReport(t *testing.T, d *db.DB, o clock.Orologio, numero string) int64 {
	t.Helper()
	return bustaScheda(t, d, o, func(a *buste.Accettazione) { a.Numero, a.Descrizione, a.CodiceQR = numero, "Lavoro "+numero, "" })
}

// inventario: RIP-S-46
// Titolare e dipendente scaricano il report delle buste ids=3,1,2 nell'ordine chiesto, i doppioni tolti:
// un PDF A4 con una pagina per busta, in linea («inline; filename="schede_3.pdf"»), application/pdf,
// private, no-store e la lunghezza; una riga repair.report_generated per busta e per richiesta, con
// l'attore di chi chiede; nessuna repair.viewed. La rotta non finisce nella scheda della busta
// (/repairs/{id}).
func TestReportScaricato(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	uno, due, tre := bustaReport(t, d, o, "1/26"), bustaReport(t, d, o, "2/26"), bustaReport(t, d, o, "3/26")
	ids := strconv.FormatInt(tre, 10) + "," + strconv.FormatInt(uno, 10) + ",%20" + strconv.FormatInt(due, 10) + "," +
		strconv.FormatInt(uno, 10) + ",0" + strconv.FormatInt(tre, 10)
	for _, b := range []*browser{titolareDentro(t, h), dipendenteDentro(t, h)} {
		for _, p := range []string{percorsoReport + "?ids=" + ids, percorsoReport + "/?ids=" + ids} {
			r := b.vai("GET", p, nil, nil)
			if r.codice != 200 || !unaVolta(r, "Content-Type", "application/pdf") || !unaVolta(r, "Cache-Control", "private, no-store") ||
				!unaVolta(r, "Content-Disposition", `inline; filename="schede_3.pdf"`) || r.h.Get("Content-Length") != strconv.Itoa(len(r.corpo)) {
				t.Fatalf("%s: %d %v %q", p, r.codice, r.h, r.corpo)
			}
			l, err := lettura.Leggi([]byte(r.corpo))
			if err != nil {
				t.Fatal(err)
			}
			testo := lettura.TestoNormalizzato(l)
			i3, i1, i2 := strings.Index(testo, "#3/26"), strings.Index(testo, "#1/26"), strings.Index(testo, "#2/26")
			if lettura.Formato(l) != "210x297,210x297,210x297" || !strings.HasPrefix(testo, "Negozio Schede riparazioni Generato il 31/10/2026 3 schede #3/26") ||
				!(i3 < i1 && i1 < i2) || !strings.HasSuffix(testo, "Generato da Negozio — uso interno Pagina 3 / 3") {
				t.Errorf("%s %q", lettura.Formato(l), testo)
			}
		}
	}
	for _, c := range []struct {
		email string
		id    int64
		n     int
	}{{"titolare@esempio.it", uno, 2}, {"titolare@esempio.it", tre, 2}, {"banco@esempio.it", due, 2}} {
		if n := contaCon(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.report_generated' AND attore = ? AND id_risorsa = ?
			AND esito = 'ok' AND dati = ?`, c.email, c.id, `{"bag_number":"`+map[int64]string{uno: "1/26", due: "2/26", tre: "3/26"}[c.id]+`"}`); n != c.n {
			t.Errorf("%s %d: %d righe", c.email, c.id, n)
		}
	}
	if n := contaCon(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.report_generated'`); n != 12 {
		t.Errorf("righe: %d", n)
	}
	if n := contaCon(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`); n != 0 {
		t.Errorf("repair.viewed: %d", n)
	}
}

// inventario: RIP-S-46
// I rifiuti di RepairReportController, con i testi, il tipo e le intestazioni misurati sullo
// standalone: refuse/3 è text/plain; charset=utf-8 con private, no-store, senza allegato. 422 per una
// voce che non è un id (la prima, senza spazi), per un ids che non è un testo (inspect/1) e oltre 50
// buste (anche se non ci sono: il limite viene prima); 404 per le buste che non ci sono o sono nel
// cestino, nell'ordine chiesto. Nessuno scrive righe. Senza accesso si va a /login.
func TestReportRifiuti(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	uno := strconv.FormatInt(bustaReport(t, d, o, "1/26"), 10)
	cestino := bustaReport(t, d, o, "2/26")
	eseguiSQL(t, d, `UPDATE buste SET cancellata_il = '2026-10-31T09:00:00.000000Z' WHERE id = ?`, cestino)
	if r := nuovoBrowser(t, h).vai("GET", percorsoReport+"?ids="+uno, nil, nil); !redirectA(r, "/login") {
		t.Errorf("senza accesso: %d %q", r.codice, r.location)
	}
	var cinquantuno []string
	for i := 1; i <= 51; i++ {
		cinquantuno = append(cinquantuno, strconv.Itoa(1000+i))
	}
	b := titolareDentro(t, h)
	for _, c := range []struct {
		query  string
		codice int
		corpo  string
	}{
		{"ids=abc", 422, "Selezione non valida: «abc» non è un numero di riparazione."},
		{"ids=" + uno + ",%20abc%20,zz", 422, "Selezione non valida: «abc» non è un numero di riparazione."},
		{"ids=0", 422, "Selezione non valida: «0» non è un numero di riparazione."},
		// Oltre i 64 bit: il 422 di Id.positive/1, come lo standalone (qui non c'è un 500: D-56 non vale).
		{"ids=99999999999999999999", 422, "Selezione non valida: «99999999999999999999» non è un numero di riparazione."},
		{"ids=" + uno + ",%2B0099999999999999999999", 422, "Selezione non valida: «+0099999999999999999999» non è un numero di riparazione."},
		{"ids=-99999999999999999999", 422, "Selezione non valida: «-99999999999999999999» non è un numero di riparazione."},
		{"ids=1+2", 422, "Selezione non valida: «1 2» non è un numero di riparazione."},
		{"ids%5B%5D=1&ids%5B%5D=2", 422, `Selezione non valida: «["1", "2"]» non è un numero di riparazione.`},
		{"ids[a]=1", 422, `Selezione non valida: «%{"a" => "1"}» non è un numero di riparazione.`},
		{"ids=" + strings.Join(cinquantuno, ","), 422,
			"Troppe riparazioni per un solo report: 51 selezionate, il massimo è 50. Restringi la selezione."},
		{"ids=" + uno + ",999999", 404, "Riparazione non trovata: 999999. Nessun report generato."},
		{"ids=999,998," + uno + ",999", 404, "Riparazione non trovata: 999, 998. Nessun report generato."},
		{"ids=" + strconv.FormatInt(cestino, 10), 404, "Riparazione non trovata: " + strconv.FormatInt(cestino, 10) + ". Nessun report generato."},
		{"ids=9223372036854775807", 404, "Riparazione non trovata: 9223372036854775807. Nessun report generato."},
	} {
		r := b.vai("GET", percorsoReport+"?"+c.query, nil, nil)
		if r.codice != c.codice || r.corpo != c.corpo || !unaVolta(r, "Content-Type", "text/plain; charset=utf-8") ||
			!unaVolta(r, "Cache-Control", "private, no-store") || r.h.Get("Content-Disposition") != "" {
			t.Errorf("%s: %d %q %v", c.query, r.codice, r.corpo, r.h)
		}
	}
	if n := contaCon(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.report_generated'`); n != 0 {
		t.Errorf("righe: %d", n)
	}
}

// inventario: RIP-S-46
// Senza ids (o con ids vuoto, di sole virgole o di soli spazi, o «ids» senza «=») il report è la
// selezione della home con i suoi filtri (from_filters/1, slice 051), quindi qui la sola busta che c'è;
// un ids che è una lista resta un 422. Una riga dell'audit che non si scrive dà 503 «PDF non
// disponibile» come refuse/3 (text/plain), senza PDF.
func TestReportSenzaIdsESenzaAudit(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	uno := strconv.FormatInt(bustaReport(t, d, o, "1/26"), 10)
	b := titolareDentro(t, h)
	for _, q := range []string{"", "?ids=", "?ids=,,", "?ids=%20", "?ids", "?ids=1&ids="} {
		r := b.vai("GET", percorsoReport+q, nil, nil)
		if r.codice != 200 || !unaVolta(r, "Content-Disposition", `inline; filename="schede_1.pdf"`) {
			t.Errorf("%q: %d %q", q, r.codice, r.corpo)
		}
	}
	if r := b.vai("GET", percorsoReport+"?ids[]=", nil, nil); r.codice != 422 {
		t.Errorf("ids[]=: %d %q", r.codice, r.corpo)
	}
	eseguiSQL(t, d, `CREATE TRIGGER no_report BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.report_generated'
		BEGIN SELECT RAISE(ABORT, 'no'); END`)
	for _, q := range []string{"?ids=" + uno, ""} {
		r := b.vai("GET", percorsoReport+q, nil, nil)
		if r.codice != 503 || r.corpo != "PDF non disponibile" || !unaVolta(r, "Content-Type", "text/plain; charset=utf-8") ||
			!unaVolta(r, "Cache-Control", "private, no-store") || r.h.Get("Content-Disposition") != "" {
			t.Errorf("503 %q: %d %q %v", q, r.codice, r.corpo, r.h)
		}
	}
	// La scheda della busta resta la scheda: /repairs/report.PDF non è il report.
	if r := b.vai("GET", "/repairs/report.PDF?ids="+uno, nil, nil); r.h.Get("Content-Type") == "application/pdf" {
		t.Errorf("report.PDF: %d %v", r.codice, r.h)
	}
}

// numeriDelReport sono i numeri «#N/AA» delle schede del PDF, nell'ordine.
func numeriDelReport(t *testing.T, r risposta) []string {
	t.Helper()
	l, err := lettura.Leggi([]byte(r.corpo))
	if err != nil {
		t.Fatalf("%d %q: %v", r.codice, r.corpo, err)
	}
	var out []string
	for _, p := range strings.Fields(lettura.TestoNormalizzato(l)) {
		if strings.HasPrefix(p, "#") && strings.Contains(p, "/") {
			out = append(out, p)
		}
	}
	return out
}

// inventario: RIP-S-46, RIP-H-19
// Senza ids il report è la selezione della home (RepairIndex.selection/2 e from_filters/1, come i test
// di repair_report_controller_test.exs): gli stessi filtri e lo stesso ordine della lista
// (fissate, poi anno e progressivo), la pagina ignorata, il nascondimento delle pronte senza filtri
// come la lista; ids vince sui filtri; 422 «Nessuna riparazione da stampare…» se non trova niente e
// «Troppe riparazioni…» con il conteggio intero oltre 50, senza righe; 31 buste (oltre una pagina)
// sono 31 schede, con una riga repair.report_generated per busta nell'ordine.
func TestReportDaiFiltriDellaHome(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := titolareDentro(t, h)
	uno, due := bustaReport(t, d, o, "1/26"), bustaReport(t, d, o, "2/26")
	tre := bustaReport(t, d, o, "3/26")
	eseguiSQL(t, d, `UPDATE buste SET stato = 'pronta' WHERE id = ?`, due)
	eseguiSQL(t, d, `UPDATE buste SET fissata_il = '2026-10-30T10:00:00.000000Z' WHERE id = ?`, uno)
	for _, c := range []struct {
		q      string
		voluti []string
	}{
		{"", []string{"#1/26", "#3/26", "#2/26"}},
		{"?stato=pronta", []string{"#2/26"}},
		{"?stato=pronta&p=2", []string{"#2/26"}},
		{"?p=9", []string{"#1/26", "#3/26", "#2/26"}},
		{"?cerca=Lavoro%203", []string{"#3/26"}},
		{"?cerca=03/26", []string{"#3/26"}},
		{"?ids=" + strconv.FormatInt(due, 10) + "," + strconv.FormatInt(tre, 10) + "&stato=pronta", []string{"#2/26", "#3/26"}},
	} {
		r := b.vai("GET", percorsoReport+c.q, nil, nil)
		if g := numeriDelReport(t, r); r.codice != 200 || !slices.Equal(g, c.voluti) {
			t.Errorf("%q: %d %v, voluti %v", c.q, r.codice, g, c.voluti)
		}
	}
	// Le righe del registro seguono l'ordine della selezione.
	esegui := func(q string, args ...any) { eseguiSQL(t, d, q, args...) }
	esegui(`DELETE FROM eventi_audit`)
	b.vai("GET", percorsoReport, nil, nil)
	var ordine []int64
	righe, err := d.Lettura.Query(`SELECT id_risorsa FROM eventi_audit WHERE azione = 'repair.report_generated' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for righe.Next() {
		var id int64
		if err := righe.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ordine = append(ordine, id)
	}
	_ = righe.Close()
	if !slices.Equal(ordine, []int64{uno, tre, due}) {
		t.Errorf("righe %v", ordine)
	}
	// Il nascondimento delle pronte vale anche per il report senza filtri; un filtro le rimostra.
	esegui(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('home_mostra_pronte', '0', '2026-10-30T10:00:00.000000Z')`)
	if g := numeriDelReport(t, b.vai("GET", percorsoReport, nil, nil)); !slices.Equal(g, []string{"#1/26", "#3/26"}) {
		t.Errorf("pronte nascoste: %v", g)
	}
	if g := numeriDelReport(t, b.vai("GET", percorsoReport+"?cerca=Lavoro", nil, nil)); len(g) != 3 {
		t.Errorf("pronte con un filtro: %v", g)
	}
	esegui(`DELETE FROM eventi_audit`)
	for _, q := range []string{"?cerca=nessuna", "?stato=chiusa", "?oreficeria=999", "?data_da=2030-01-01"} {
		r := b.vai("GET", percorsoReport+q, nil, nil)
		if r.codice != 422 || r.corpo != "Nessuna riparazione da stampare: il filtro selezionato non trova nessuna busta." ||
			!unaVolta(r, "Content-Type", "text/plain; charset=utf-8") || !unaVolta(r, "Cache-Control", "private, no-store") {
			t.Errorf("%s: %d %q", q, r.codice, r.corpo)
		}
	}
	for n := 4; n <= 31; n++ {
		bustaReport(t, d, o, strconv.Itoa(n)+"/26")
	}
	if g := numeriDelReport(t, b.vai("GET", percorsoReport+"?cerca=Lavoro", nil, nil)); len(g) != 31 || g[0] != "#1/26" || g[1] != "#31/26" {
		t.Errorf("31 buste: %d %v", len(g), g)
	}
	esegui(`DELETE FROM eventi_audit`)
	for n := 32; n <= 51; n++ {
		bustaReport(t, d, o, strconv.Itoa(n)+"/26")
	}
	// Senza filtri la pronta resta nascosta: 50 buste, il massimo, si stampano.
	if r := b.vai("GET", percorsoReport, nil, nil); r.codice != 200 || !unaVolta(r, "Content-Disposition", `inline; filename="schede_50.pdf"`) {
		t.Errorf("50: %d %v", r.codice, r.h)
	}
	esegui(`DELETE FROM eventi_audit`)
	// Oltre il limite il conteggio è quello intero, non le buste caricate (al più 51).
	bustaReport(t, d, o, "52/26")
	for q, testo := range map[string]string{
		"?cerca=Lavoro": "Troppe riparazioni per un solo report: 52 selezionate, il massimo è 50. Restringi la selezione.",
		"":              "Troppe riparazioni per un solo report: 51 selezionate, il massimo è 50. Restringi la selezione.",
	} {
		if r := b.vai("GET", percorsoReport+q, nil, nil); r.codice != 422 || r.corpo != testo {
			t.Errorf("%q: %d %q", q, r.codice, r.corpo)
		}
	}
	if n := contaCon(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.report_generated'`); n != 0 {
		t.Errorf("righe dopo i rifiuti: %d", n)
	}
}
