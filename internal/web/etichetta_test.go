package web

import (
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/buste"
)

// D-64 su «Nuova riparazione» e sulla scheda (slice 053): il campo «Etichetta (0045/26)», l'avviso
// delle gemelle aperte, il numero visibile nel titolo della scheda e le gemelle sotto il titolo.

// inventario: RIP-H-39, RIP-H-30
// Il numero scritto a mano diventa il codice dell'etichetta, con l'anno della data di accettazione
// del modulo (non di oggi) o quello scritto; il codice letto resta; «Busta 0045/26 salvata»
// dice il numero visibile, anche quello di un'etichetta dell'anno prima.
func TestNuovaEtichetta(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	for _, c := range []struct{ scritto, data, salvato, messaggio string }{
		{"45", "2026-10-31", "RIP-2026-0045", "Busta 0045/26 salvata"},
		{"46", "2025-12-31", "RIP-2025-0046", "Busta 0046/25 salvata"},
		{"0047/25", "2026-10-31", "RIP-2025-0047", "Busta 0047/25 salvata"},
		{" RIP-2026-0048 ", "2026-10-31", "RIP-2026-0048", "Busta 0048/26 salvata"},
		{"QR-ALTRO", "2026-10-31", "QR-ALTRO", "Busta N. 4/26 salvata"},
	} {
		b.vai("GET", "/nuova", nil, nil)
		r := b.vai("POST", "/nuova", con(con(bustaProva(b), "repair[qr_code]", c.scritto), "repair[accepted_on]", c.data), nil)
		if !creata(r) {
			t.Fatalf("%q: %d %s", c.scritto, r.codice, r.corpo)
		}
		var qr string
		if err := d.Lettura.QueryRow(`SELECT codice_qr FROM buste ORDER BY id DESC LIMIT 1`).Scan(&qr); err != nil || qr != c.salvato {
			t.Errorf("%q: salvato %q (%v), atteso %q", c.scritto, qr, err, c.salvato)
		}
		contiene(t, c.scritto, b.vai("GET", r.location, nil, nil).corpo, `<div role="status" class="flash flash-info">`+c.messaggio+`</div>`)
	}
}

// inventario: RIP-H-39
// D-64, busta di carta riusata: un'etichetta già su una busta aperta si avvisa una volta, con il
// modulo com'è e il codice risolto nel campo gemelle_avvisate, senza salvare; premendo di nuovo si
// salva. Un altro codice dopo l'avviso si controlla da capo. Solo le gemelle chiuse: nessun avviso.
func TestNuovaGemelleAperte(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	busta := func(nome, codice, stato string) int64 {
		id := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.NomeCliente, a.CodiceQR = nome, codice })
		eseguiSQL(t, d, `UPDATE buste SET stato = ? WHERE id = ?`, stato, id)
		return id
	}
	busta("Cliente chiusa", "RIP-2026-0045", "consegnata")
	b := titolareDentro(t, h)
	b.vai("GET", "/nuova", nil, nil)
	modulo := func(qr string) url.Values {
		v := con(con(bustaProva(b), "repair[qr_code]", qr), "repair[description]", "Riusata <busta>")
		v.Del("gemelle_avvisate")
		return v
	}
	if r := b.vai("POST", "/nuova", modulo("45"), nil); !creata(r) {
		t.Fatalf("solo una gemella chiusa: %d %s", r.codice, r.corpo)
	}
	busta("Cliente <aperta>", "RIP-2026-0046", "pronta")
	prima := contaBuste(t, d)
	r := b.vai("POST", "/nuova", modulo("46"), nil)
	if r.codice != 200 {
		t.Fatalf("avviso: %d %q", r.codice, r.location)
	}
	contiene(t, "avviso", r.corpo, `<div id="intake-qr-code-row" class="intake-qr-row intake-qr-missing">`,
		`<p id="intake-qr-twins" role="alert">`+"\n"+`⚠ Etichetta già usata dalla busta aperta di Cliente &lt;aperta&gt; (0046/26): controlla`+
			"\nl'etichetta, oppure premi di nuovo «Genera busta» per salvarla lo stesso.\n</p>",
		`<input name="gemelle_avvisate" type="hidden" value="RIP-2026-0046">`, `value="46"`, ">\nRiusata &lt;busta&gt;</textarea>",
		`<button type="submit">Genera busta</button>`)
	nonContiene(t, "avviso", r.corpo, `id="intake-qr-warning"`, "field-error")
	if n := contaBuste(t, d); n != prima {
		t.Errorf("l'avviso ha salvato: %d", n-prima)
	}
	// Un altro numero dopo l'avviso: l'avviso del suo codice.
	busta("Prima", "RIP-2026-0047", "in_lavorazione")
	busta("Seconda", "RIP-2026-0047", "in_attesa_materiali")
	r = b.vai("POST", "/nuova", con(modulo("0047/26"), "gemelle_avvisate", "RIP-2026-0046"), nil)
	contiene(t, "altro codice", r.corpo, "⚠ Etichetta già usata dalle buste aperte di Seconda (0047/26) e Prima (0047/26): controlla",
		`<input name="gemelle_avvisate" type="hidden" value="RIP-2026-0047">`)
	// Premuto di nuovo, con il campo dell'avviso: si salva lo stesso.
	if r := b.vai("POST", "/nuova", con(modulo("46"), "gemelle_avvisate", "RIP-2026-0046"), nil); !creata(r) {
		t.Fatalf("salva lo stesso: %d %s", r.codice, r.corpo)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM buste WHERE codice_qr = 'RIP-2026-0046'`); n != 2 {
		t.Errorf("buste con 0046/26: %d", n)
	}
	// Un errore dei campi dopo l'avviso rimanda il codice avvisato: nessun nuovo avviso dopo.
	r = b.vai("POST", "/nuova", con(con(modulo("47"), "gemelle_avvisate", "RIP-2026-0047"), "repair[description]", ""), nil)
	contiene(t, "errore dopo l'avviso", r.corpo, `<input name="gemelle_avvisate" type="hidden" value="RIP-2026-0047">`, "Campo obbligatorio.")
	nonContiene(t, "errore dopo l'avviso", r.corpo, `id="intake-qr-twins"`)
	// Dopo l'avviso del QR mancante (qr_avvisato) arriva un'etichetta già usata: solo l'avviso delle
	// gemelle, con il campo nascosto qr_avvisato che resta (review della 053).
	r = b.vai("POST", "/nuova", con(modulo("46"), "qr_avvisato", "1"), nil)
	contiene(t, "gemelle dopo il QR mancante", r.corpo, `id="intake-qr-twins"`, `<input name="qr_avvisato" type="hidden" value="1">`,
		`<button type="submit">Genera busta</button>`)
	nonContiene(t, "gemelle dopo il QR mancante", r.corpo, `id="intake-qr-warning"`, "Manca il codice QR", "Genera busta senza QR")
	// Senza QR c'è l'avviso del QR mancante, non quello delle gemelle.
	r = b.vai("POST", "/nuova", modulo(" "), nil)
	contiene(t, "senza QR", r.corpo, `id="intake-qr-warning"`)
	nonContiene(t, "senza QR", r.corpo, `id="intake-qr-twins"`)
}

// inventario: RIP-S-01, RIP-S-02, RIP-H-56
// Il titolo della scheda ha il numero visibile della lista (D-64): «Busta #0045/26» con il numero
// interno in piccolo, l'anno di un'etichetta riusata («#0045/25»), «Busta N. 1/26» senza etichetta o
// con un codice di altro formato; «⚠ senza QR» per una busta aperta senza codice e, D-79 estesa (slice
// 060), per una aperta con un codice di altro formato, con il suo titolo.
func TestSchedaNumeroVisibile(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := titolareDentro(t, h)
	for _, c := range []struct {
		qr, stato, titolo, h1 string
		senza                 bool
	}{
		{"RIP-2026-0045", "in_lavorazione", "Busta #0045/26", `<h1 class="titolo-busta">Busta #0045/26` + "\n" + `<small id="repair-internal-number" class="numero-interno">N. 1/26</small></h1>`, false},
		{"RIP-2025-0045", "in_lavorazione", "Busta #0045/25", `<h1 class="titolo-busta">Busta #0045/25` + "\n" + `<small id="repair-internal-number" class="numero-interno">N. 2/26</small></h1>`, false},
		{"QR-ALTRO", "in_lavorazione", "Busta N. 3/26", `<h1 class="titolo-busta">Busta N. 3/26` + "\n" + `<span id="repair-noqr" class="segno segno-noqr" title="Busta senza etichetta RIP: il suo QR è di un altro formato">⚠ senza QR</span></h1>`, true},
		{"", "in_lavorazione", "Busta N. 4/26", `<h1 class="titolo-busta">Busta N. 4/26` + "\n" + `<span id="repair-noqr" class="segno segno-noqr"`, true},
		{"", "consegnata", "Busta N. 5/26", `<h1 class="titolo-busta">Busta N. 5/26</h1>`, false},
		{"", "in_lavorazione", "Busta N. 6/26", `<span id="repair-noqr" class="segno segno-noqr" title="Busta senza codice QR: aggiungilo da «Modifica»">⚠ senza QR</span>`, true},
		{"QR-ALTRO-7", "consegnata", "Busta N. 7/26", `<h1 class="titolo-busta">Busta N. 7/26</h1>`, false},
	} {
		id := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.CodiceQR = c.qr })
		eseguiSQL(t, d, `UPDATE buste SET stato = ? WHERE id = ?`, c.stato, id)
		r := b.vai("GET", percorsoScheda(id), nil, nil)
		contiene(t, c.titolo, r.corpo, "<title>"+c.titolo+" · Laboratorio</title>", c.h1)
		if strings.Contains(r.corpo, "⚠ senza QR") != c.senza {
			t.Errorf("%s: senza QR %v", c.titolo, !c.senza)
		}
		nonContiene(t, c.titolo, r.corpo, `id="repair-label-twins"`)
		if c.qr != "RIP-2026-0045" && c.qr != "RIP-2025-0045" {
			nonContiene(t, c.titolo, r.corpo, `id="repair-internal-number"`)
		}
		// Anche con il modulo della modifica aperto.
		r = b.vai("GET", percorsoScheda(id)+"?modifica=1", nil, nil)
		contiene(t, c.titolo+" modifica", r.corpo, "<title>"+c.titolo+" · Laboratorio</title>", c.h1, `id="repair-form"`)
	}
}

// inventario: RIP-S-01, RIP-S-02
// Le gemelle della scheda (D-64): «Un'altra busta ha la stessa etichetta» con il link a ognuna solo
// per le gemelle aperte; il link «storico di questa etichetta» (la ricerca della home per il numero
// visibile) solo se una gemella è chiusa e il codice è un'etichetta. Il cestino non conta.
func TestSchedaGemelleWeb(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := titolareDentro(t, h)
	busta := func(nome, codice, stato string) int64 {
		id := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.NomeCliente, a.CodiceQR = nome, codice })
		eseguiSQL(t, d, `UPDATE buste SET stato = ? WHERE id = ?`, stato, id)
		return id
	}
	chiusa := busta("Chi ha ritirato", "RIP-2026-0045", "consegnata")
	aperta := busta("Chi ha riusato", "RIP-2026-0045", "in_lavorazione")
	cestino := busta("Nel cestino", "RIP-2026-0045", "in_lavorazione")
	eseguiSQL(t, d, `UPDATE buste SET cancellata_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, cestino)
	storico := `<p><a id="repair-label-history" href="/cerca?cerca=0045%2F26">storico di questa etichetta</a></p>`
	sid := func(id int64) string { return strconv.FormatInt(id, 10) }
	// Dall'aperta: la gemella è chiusa, solo lo storico.
	r := b.vai("GET", percorsoScheda(aperta), nil, nil)
	contiene(t, "aperta", r.corpo, `<div id="repair-label-twins" class="gemelle-etichetta">`, storico)
	nonContiene(t, "aperta", r.corpo, "la stessa etichetta", `id="repair-twin-`)
	// Dalla chiusa: la gemella è aperta, il suo link; nessuno storico (l'altra non è chiusa).
	r = b.vai("GET", percorsoScheda(chiusa), nil, nil)
	contiene(t, "chiusa", r.corpo, `<p id="repair-label-twins-open">⚠ Un'altra busta ha la stessa etichetta:`+"\n"+
		`<a id="repair-twin-`+sid(aperta)+`" href="/repairs/`+sid(aperta)+`">Chi ha riusato · 05/09/2026 · In lavorazione</a></p>`)
	nonContiene(t, "chiusa", r.corpo, "repair-label-history", `id="repair-twin-`+sid(cestino))
	// Una terza, aperta: dalla chiusa due link, dalla seconda aperta l'altra aperta e lo storico.
	terza := busta("Terza <b>", "RIP-2026-0045", "pronta")
	r = b.vai("GET", percorsoScheda(chiusa), nil, nil)
	contiene(t, "due aperte", r.corpo, "⚠ Altre 2 buste hanno la stessa etichetta:\n"+`<a id="repair-twin-`+sid(terza)+`" href="/repairs/`+sid(terza)+
		`">Terza &lt;b&gt; · 05/09/2026 · Pronta</a>, <a id="repair-twin-`+sid(aperta)+`"`)
	r = b.vai("GET", percorsoScheda(aperta), nil, nil)
	contiene(t, "aperta con due", r.corpo, `<a id="repair-twin-`+sid(terza)+`"`, storico, "Un'altra busta ha la stessa etichetta")
	// Lo storico porta alla lista con tutte e tre (il cestino no).
	r = b.vai("GET", "/cerca?cerca=0045%2F26", nil, nil)
	if r.codice != 302 || r.location != "/?cerca=0045%2F26" {
		t.Errorf("storico: %d %q", r.codice, r.location)
	}
	lista := b.vai("GET", r.location, nil, nil).corpo
	for _, id := range []int64{chiusa, aperta, terza} {
		contiene(t, "storico", lista, `id="repair-`+sid(id)+`"`)
	}
	nonContiene(t, "storico", lista, `id="repair-`+sid(cestino)+`"`)
	// Un codice di altro formato: le gemelle aperte sì, lo storico no (il codice non va in un indirizzo).
	libera := busta("Libera chiusa", "QR-LIBERO", "annullata")
	altra := busta("Libera aperta", "QR-LIBERO", "in_lavorazione")
	r = b.vai("GET", percorsoScheda(altra), nil, nil)
	nonContiene(t, "codice libero aperta", r.corpo, `id="repair-label-twins"`, "QR-LIBERO\"", "/cerca?")
	r = b.vai("GET", percorsoScheda(libera), nil, nil)
	contiene(t, "codice libero chiusa", r.corpo, `<a id="repair-twin-`+sid(altra)+`"`)
	nonContiene(t, "codice libero chiusa", r.corpo, "repair-label-history")
	// Una gemella aperta con una data illeggibile (solo da import): la data com'è, non un 500.
	strana := busta("Data strana", "QR-LIBERO", "pronta")
	eseguiSQL(t, d, `UPDATE buste SET data_accettazione = '2026-02-30' WHERE id = ?`, strana)
	r = b.vai("GET", percorsoScheda(libera), nil, nil)
	contiene(t, "data illeggibile", r.corpo, `<a id="repair-twin-`+sid(strana)+`" href="/repairs/`+sid(strana)+`">Data strana · 2026-02-30 · Pronta</a>`)
	// Una busta senza gemelle: niente.
	sola := busta("Sola", "RIP-2026-0099", "in_lavorazione")
	nonContiene(t, "sola", b.vai("GET", percorsoScheda(sola), nil, nil).corpo, `id="repair-label-twins"`)
}

// inventario: RIP-S-04
// Nella modifica il campo «Etichetta» mostra «0045/26» (D-64, studio punto (d)), e rimandato com'è
// non cambia il codice; un'etichetta che così non tornerebbe uguale resta il codice.
func TestModificaEtichettaMostrata(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := titolareDentro(t, h)
	for _, c := range []struct{ codice, campo string }{
		{"RIP-2026-0045", "0045/26"}, {"RIP-2025-0046", "0046/25"}, {"RIP-2026-10000", "RIP-2026-10000"}, {"QR-LIBERO", "QR-LIBERO"},
	} {
		id := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.CodiceQR = c.codice })
		r := b.vai("GET", percorsoModifica(id), nil, nil)
		contiene(t, c.codice, r.corpo, `name="repair[qr_code]" id="repair_qr_code" value="`+c.campo+`" placeholder=`)
		// Rimandato com'è (con un'altra modifica): il codice non cambia.
		if r := salva(t, b, id, map[string]string{"repair[internal_notes]": "Altra nota"}); r.codice != 302 {
			t.Fatalf("%s: salvataggio %d", c.codice, r.codice)
		}
		if qr := colonna(t, d, id, "codice_qr"); qr != c.codice || colonna(t, d, id, "note_interne") != "Altra nota" {
			t.Errorf("%s: dopo il salvataggio %q", c.codice, qr)
		}
	}
	// Una etichetta con gli spazi salvati: il codice com'è.
	id := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `UPDATE buste SET codice_qr = ' RIP-2026-0047 ' WHERE id = ?`, id)
	contiene(t, "spazi", b.vai("GET", percorsoModifica(id), nil, nil).corpo, `id="repair_qr_code" value=" RIP-2026-0047 "`)
}
