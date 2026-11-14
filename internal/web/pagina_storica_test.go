package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/firmeflask"
	"github.com/L0cutus/gestrip/internal/imaging/sintetiche"
)

// I link storici /stato, /foto e /thumb sulla faccia pubblica (CLI-16…21), come
// public_repair_controller_test.exs, public_repair_legacy_revocation_test.exs,
// public_repair_thread_test.exs e public_repair_message_test.exs dello standalone (go-baseline).
// Segreto e codici inventati.

const segretoStorico = "synthetic-legacy-secret"

// storicaWeb prepara una busta «46/26» importata da «0046/26» (D-77) con il codice «CODICE»
// impresso sul numero com'era, e dà alle buste il segreto sintetico.
func storicaWeb(t *testing.T, p provaCliente, f func(*buste.Accettazione)) buste.Creata {
	t.Helper()
	p.b.ConSegretoFlask(firmeflask.Nuovo(segretoStorico))
	c := p.busta(t, f)
	eseguiSQL(t, p.d, `UPDATE buste SET numero = '46/26', codice_qr = NULL, hash_codice_flask = ? WHERE id = ?`,
		firmeflask.ImprontaCodice("0046/26", "CODICE"), c.ID)
	return c
}

func contaAzioneWeb(t *testing.T, p provaCliente, azione string) int {
	t.Helper()
	return contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = ?`, azione)
}

// inventario: CLI-16, CLI-17
// La pagina storica con il codice breve sul numero scritto: la pagina del cliente senza preventivo,
// numero visibile, intestazioni pubbliche, il poll e il modulo con la credenziale, le foto «cliente»
// firmate per un'ora con lo slug del numero scritto. Una riga repair.legacy_public_viewed.
func TestPaginaStoricaHTTP(t *testing.T) {
	p := paginaClienteProva(t)
	c := storicaWeb(t, p, func(a *buste.Accettazione) { a.NotePubbliche = "Pronta venerdì"; a.Preventivo = "40" })
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'preventivo' WHERE id = ?`, c.ID)
	f1 := p.allega(t, c.ID, "fronte.png", sintetiche.PNG(400, 300, 1))
	nascosta := p.allega(t, c.ID, "retro.png", sintetiche.PNG(300, 300, 2))
	eseguiSQL(t, p.d, `UPDATE foto_buste SET visibile_cliente = 0 WHERE id = ?`, nascosta.ID)
	b := nuovoBrowser(t, p.h)
	r := b.vai("GET", "/stato/0046/26?pw=+codice+", nil, nil)
	if r.codice != http.StatusOK {
		t.Fatalf("codice %d: %s", r.codice, r.corpo)
	}
	scad := strconv.FormatInt(p.o.Adesso().Unix()+3600, 10)
	firma := firmeflask.Nuovo(segretoStorico).FirmaFotoBusta("0046_26", f1.NomeFile, p.o.Adesso().Unix()+3600)
	src := "/foto/0046_26/" + f1.NomeFile + "?sig=" + firma + "&amp;exp=" + scad
	contiene(t, "pagina", r.corpo, ">La tua riparazione<", ">46/26<", ">Collana da saldare<", ">Pronta venerdì<",
		`<p class="cliente-prezzo">40</p>`, `data-poll-url="/stato/0046/26/poll?pw=&#43;codice&#43;"`,
		`<form id="public-thread-form" action="/stato/0046/26/messaggio" method="post" class="cliente-conv-modulo">`,
		`<input type="hidden" name="pw" value=" codice ">`,
		`<img id="public-repair-photo-`+strconv.FormatInt(f1.ID, 10)+`" src="`+src+`&amp;size=thumb"`, `<img src="`+src+`" alt="Foto 1"`)
	nonContiene(t, "pagina", r.corpo, "public-quote", "Confermo il preventivo", "Mario", "333", "Nota interna",
		"public-repair-photo-"+strconv.FormatInt(nascosta.ID, 10)+`"`, "0046/26<", "N. 46/26")
	for k, v := range map[string]string{"Cache-Control": "no-store", "Referrer-Policy": "no-referrer",
		"Content-Security-Policy": "base-uri 'self'; frame-ancestors 'self';", "X-Content-Type-Options": "nosniff",
		"Content-Type": "text/html; charset=utf-8"} {
		if g := r.h.Get(k); g != v {
			t.Errorf("%s: %q, atteso %q", k, g, v)
		}
	}
	if n := contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.legacy_public_viewed' AND id_risorsa = ?
		AND attore = 'public' AND dati = '{}'`, strconv.FormatInt(c.ID, 10)); n != 1 {
		t.Errorf("apertura scritta %d volte", n)
	}
	// La firma della pagina apre la foto e la miniatura, sulle due rotte.
	perc := "/foto/0046_26/" + f1.NomeFile + "?sig=" + firma + "&exp=" + scad
	if x := b.vai("GET", perc, nil, nil); x.codice != 200 || !unaVolta(x, "X-Photo-Variant", "full") || !unaVolta(x, "Content-Type", "image/jpeg") ||
		!unaVolta(x, "Cache-Control", "no-store") || !unaVolta(x, "Referrer-Policy", "no-referrer") {
		t.Errorf("foto: %d %v", x.codice, x.h)
	}
	for _, mini := range []string{perc + "&size=thumb", strings.Replace(perc, "/foto/", "/thumb/", 1)} {
		if x := b.vai("GET", mini, nil, nil); x.codice != 200 || !unaVolta(x, "X-Photo-Variant", "thumb") || !unaVolta(x, "Content-Type", "image/webp") {
			t.Errorf("miniatura %s: %d %v", mini, x.codice, x.h)
		}
	}
	if n := contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_photo_viewed' AND esito = 'ok'
		AND dati = '{"audience":"customer","photo_id":"filename","variant":"thumb"}'`); n != 2 {
		t.Errorf("audit delle miniature: %d", n)
	}
	// La firma ?t=&exp= della busta riscritta, sul numero scritto: il poll la rimanda (t, poi exp).
	m := regexp.MustCompile(`data-poll-url="([^"]*)"`)
	f := firmaStoricaWeb("0046/26", 2_000_000_000)
	r = b.vai("GET", "/stato/0046/26?exp=2000000000&t="+f, nil, nil)
	if r.codice != 200 {
		t.Fatalf("firma: %d", r.codice)
	}
	if g := m.FindStringSubmatch(r.corpo); g == nil || g[1] != "/stato/0046/26/poll?t="+f+"&amp;exp=2000000000" {
		t.Errorf("poll con la firma: %v", g)
	}
}

// firmaStoricaWeb è la firma t di un numero con il segreto sintetico, dai vettori calcolati fuori dal
// codice (python3, hmac).
func firmaStoricaWeb(numero string, exp int64) string {
	vettori := map[string]string{"0046/26:2000000000": "0613f5367396f9bbc157411baac1002c",
		"46/26:2000000000": "532beb68cf40230f5f2945678392a448", "42/26:2000000000": "dabbdeed7a356af74ce85ca0fd25b859"}
	return vettori[numero+":"+strconv.FormatInt(exp, 10)]
}

// inventario: CLI-16, CLI-21
// Ogni rifiuto (codice sbagliato, numero senza zeri, firma della forma sbagliata, nessuna
// credenziale, busta che non c'è, cestino) è lo stesso 404 della pagina del cliente, byte per byte,
// con una riga repair.legacy_public_denied senza busta.
func TestPaginaStoricaRifiutoUniforme(t *testing.T) {
	p := paginaClienteProva(t)
	c := storicaWeb(t, p, nil)
	b := nuovoBrowser(t, p.h)
	nulla := b.vai("GET", "/r/inventato", nil, nil)
	percorsi := []string{"/stato/0046/26?pw=ALTRO", "/stato/46/26?pw=CODICE", "/stato/046/26?pw=CODICE", "/stato/0046/26",
		"/stato/0046/26?pw[]=CODICE", "/stato/0046/26?t=" + strings.Repeat("0", 32) + "&exp=2000000000",
		"/stato/46/26?t=" + firmaStoricaWeb("0046/26", 2_000_000_000) + "&exp=2000000000", "/stato/0047/26?pw=CODICE",
		"/stato/a%2Fb/26?pw=CODICE"}
	for _, perc := range percorsi {
		r := b.vai("GET", perc, nil, nil)
		if r.codice != http.StatusNotFound || r.corpo != nulla.corpo || r.corpo != "Riparazione non disponibile" {
			t.Errorf("%s: %d %q", perc, r.codice, r.corpo)
		}
		for _, k := range []string{"Cache-Control", "Referrer-Policy", "Content-Security-Policy", "Content-Type", "X-Content-Type-Options"} {
			if r.h.Get(k) != nulla.h.Get(k) {
				t.Errorf("%s %s: %q, %q", perc, k, r.h.Get(k), nulla.h.Get(k))
			}
		}
	}
	eseguiSQL(t, p.d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteHome, c.ID)
	if r := b.vai("GET", "/stato/0046/26?pw=CODICE", nil, nil); r.codice != http.StatusNotFound {
		t.Errorf("cestino: %d", r.codice)
	}
	if n := contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.legacy_public_denied' AND id_risorsa = 'unknown'
		AND dati = '{}'`); n != len(percorsi)+1 {
		t.Errorf("rifiuti scritti: %d", n)
	}
	if contaAzioneWeb(t, p, "repair.legacy_public_viewed") != 0 {
		t.Error("un rifiuto scrive un'apertura")
	}
}

// inventario: CLI-18
// La revoca: la credenziale giusta dà il 410 «Link non più attivo» con il nome del negozio e nessun
// dato della busta, con le intestazioni pubbliche; il poll e il messaggio il 404; la credenziale
// sbagliata il 404 della busta che non c'è; le foto il 404 anche con la firma giusta.
func TestStoricaRevocataHTTP(t *testing.T) {
	p := paginaClienteProva(t)
	c := storicaWeb(t, p, nil)
	f1 := p.allega(t, c.ID, "fronte.png", sintetiche.PNG(400, 300, 1))
	b := nuovoBrowser(t, p.h)
	exp := p.o.Adesso().Unix() + 3600
	foto := "/foto/0046_26/" + f1.NomeFile + "?sig=" + firmeflask.Nuovo(segretoStorico).FirmaFotoBusta("0046_26", f1.NomeFile, exp) +
		"&exp=" + strconv.FormatInt(exp, 10)
	if r := b.vai("GET", foto, nil, nil); r.codice != 200 {
		t.Fatalf("foto prima della revoca: %d", r.codice)
	}
	if _, err := p.b.RevocaLink(t.Context(), c.ID, audit.Contesto{Attore: "titolare@esempio.it"}); err != nil {
		t.Fatal(err)
	}
	for _, cred := range []string{"pw=CODICE", "t=" + firmaStoricaWeb("0046/26", 2_000_000_000) + "&exp=2000000000"} {
		r := b.vai("GET", "/stato/0046/26?"+cred, nil, nil)
		if r.codice != http.StatusGone || r.h.Get("Cache-Control") != "no-store" || r.h.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s: %d %v", cred, r.codice, r.h)
		}
		contiene(t, "410", r.corpo, "<h1>Link non più attivo</h1>",
			`<p id="email-outcome-message">Questo link non è più attivo. Chiedi a Bottega di Prova il link nuovo della tua riparazione.</p>`)
		nonContiene(t, "410", r.corpo, "46/26", "Collana", "public-thread")
		if x := b.vai("GET", "/stato/0046/26/poll?"+cred, nil, nil); x.codice != http.StatusNotFound || x.corpo != "Riparazione non disponibile" {
			t.Errorf("poll: %d", x.codice)
		}
	}
	if n := contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.legacy_public_denied' AND id_risorsa = ?
		AND dati = '{"reason":"revoked"}'`, strconv.FormatInt(c.ID, 10)); n != 2 {
		t.Errorf("revoche scritte: %d", n)
	}
	if r := b.vai("GET", "/stato/0046/26?pw=SBAGLIATO", nil, nil); r.codice != http.StatusNotFound || r.corpo != "Riparazione non disponibile" {
		t.Errorf("sbagliato: %d", r.codice)
	}
	for _, perc := range []string{foto, strings.Replace(foto, "/foto/", "/thumb/", 1)} {
		if r := b.vai("GET", perc, nil, nil); r.codice != http.StatusNotFound || r.corpo != "Foto non disponibile" || r.h.Get("Content-Type") != "" {
			t.Errorf("%s: %d %q %q", perc, r.codice, r.corpo, r.h.Get("Content-Type"))
		}
	}
}

// inventario: CLI-19, CLI-17, CLI-28
// Il poll storico con la credenziale; su una busta B2B la pagina non ha prezzi, conversazione né
// foto, e il poll e il messaggio sono il 404 del codice sbagliato.
func TestPollStoricoEB2B(t *testing.T) {
	p := paginaClienteProva(t)
	c := storicaWeb(t, p, func(a *buste.Accettazione) { a.Preventivo = "123,45" })
	p.riga(t, c.ID, "shop", nil, "Pronta venerdì", istanteHome)
	b := nuovoBrowser(t, p.h)
	r := b.vai("GET", "/stato/0046/26/poll?pw=CODICE", nil, nil)
	if r.codice != 200 || !strings.Contains(r.corpo, `"body":"Pronta venerdì"`) || r.h.Get("Content-Type") != "application/json; charset=utf-8" ||
		r.h.Get("Cache-Control") != "no-store" {
		t.Errorf("poll: %d %s %v", r.codice, r.corpo, r.h)
	}
	if r := b.vai("GET", "/stato/0046/26/poll?pw=SBAGLIATO", nil, nil); r.codice != http.StatusNotFound || r.corpo != "Riparazione non disponibile" {
		t.Errorf("poll sbagliato: %d", r.codice)
	}
	eseguiSQL(t, p.d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, c.ID)
	g := b.vai("GET", "/stato/0046/26?pw=CODICE", nil, nil)
	if g.codice != 200 {
		t.Fatalf("b2b: %d", g.codice)
	}
	nonContiene(t, "b2b", g.corpo, "123,45", "public-thread", "Pronta venerdì", "Oreficeria Esempio", "public-repair-photos")
	if r := b.vai("GET", "/stato/0046/26/poll?pw=CODICE", nil, nil); r.codice != http.StatusNotFound {
		t.Errorf("poll b2b: %d", r.codice)
	}
	b.vai("GET", "/stato/0046/26?pw=CODICE", nil, nil)
	if r := b.vai("POST", "/stato/0046/26/messaggio", url.Values{"_csrf_token": {b.token}, "pw": {"CODICE"}, "testo": {"x"}}, nil); r.codice != http.StatusNotFound ||
		r.corpo != "Riparazione non disponibile" {
		t.Errorf("messaggio b2b: %d", r.codice)
	}
	if contaSQL(t, p.d, `SELECT count(*) FROM messaggi_buste WHERE autore = 'customer'`) != 0 {
		t.Error("b2b: messaggio scritto")
	}
}

// inventario: CLI-19, CLI-12
// Il messaggio dalla pagina storica: CSRF della faccia pubblica, la credenziale nei campi del modulo,
// il redirect alla pagina con la credenziale e l'ancora, l'esito una volta; consegnata, la frase in
// cima e niente ancora; credenziale sbagliata, il 404 senza scrivere.
func TestMessaggioStoricoHTTP(t *testing.T) {
	p := paginaClienteProva(t)
	c := storicaWeb(t, p, nil)
	b := nuovoBrowser(t, p.h)
	pagina := "/stato/0046/26?pw=CODICE"
	b.vai("GET", pagina, nil, nil)
	if r := b.vai("POST", "/stato/0046/26/messaggio", url.Values{"pw": {"CODICE"}, "testo": {"ciao"}}, nil); r.codice != http.StatusForbidden {
		t.Fatalf("senza CSRF: %d", r.codice)
	}
	manda := func(campi url.Values) risposta {
		t.Helper()
		b.vai("GET", pagina, nil, nil)
		campi.Set("_csrf_token", b.token)
		return b.vai("POST", "/stato/0046/26/messaggio", campi, nil)
	}
	r := manda(url.Values{"pw": {"CODICE"}, "testo": {" Dal vecchio link "}})
	if !redirectA(r, pagina+"#public-thread") || r.h.Get("Cache-Control") != "no-store" {
		t.Fatalf("inviato: %d %q", r.codice, r.location)
	}
	g := b.vai("GET", pagina, nil, nil)
	contiene(t, "esito", g.corpo, `data-kind="info" class="cliente-conv-esito">Messaggio inviato.</p>`, "Dal vecchio link")
	contiene(t, "una volta", b.vai("GET", pagina, nil, nil).corpo, `data-kind="info" class="cliente-conv-esito"></p>`)
	if contaSQL(t, p.d, `SELECT count(*) FROM messaggi_buste WHERE busta_id = ? AND autore = 'customer'`, c.ID) != 1 {
		t.Error("messaggio non scritto")
	}
	// La firma: torna con t ed exp, nell'ordine di legacy_credential/1.
	f := firmaStoricaWeb("0046/26", 2_000_000_000)
	if r := manda(url.Values{"exp": {"2000000000"}, "t": {f}, "testo": {""}}); !redirectA(r, "/stato/0046/26?t="+f+"&exp=2000000000#public-thread") {
		t.Errorf("vuoto con la firma: %d %q", r.codice, r.location)
	}
	// La credenziale nella query del POST non conta (i campi del modulo sì).
	b.vai("GET", pagina, nil, nil)
	if r := b.vai("POST", "/stato/0046/26/messaggio?pw=CODICE", url.Values{"_csrf_token": {b.token}, "testo": {"x"}}, nil); r.codice != http.StatusNotFound {
		t.Errorf("credenziale nella query: %d", r.codice)
	}
	if r := manda(url.Values{"pw": {"SBAGLIATO"}, "testo": {""}}); r.codice != http.StatusNotFound || r.corpo != "Riparazione non disponibile" {
		t.Errorf("sbagliato: %d", r.codice)
	}
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, c.ID)
	if r := manda(url.Values{"pw": {"CODICE"}, "testo": {"x"}}); !redirectA(r, pagina) {
		t.Errorf("consegnata: %d %q", r.codice, r.location)
	}
	contiene(t, "consegnata", b.vai("GET", pagina, nil, nil).corpo,
		`<div id="public-flash-error" class="cliente-esito cliente-esito-errore">Questa riparazione non riceve più messaggi.</div>`)
	if contaSQL(t, p.d, `SELECT count(*) FROM messaggi_buste`) != 1 {
		t.Error("righe in più")
	}
}

// inventario: CLI-20, CLI-21
// Le foto storiche: firma sbagliata, scaduta, di un altro file o senza: lo stesso 404 «Foto non
// disponibile» senza tipo; i 404 delle foto non contano nel limite. Dieci 404 di /stato (anche del
// poll) bloccano lo stesso indirizzo per 15 minuti, anche con il codice giusto.
func TestFotoStoricheELimite(t *testing.T) {
	p := paginaClienteProva(t)
	c := storicaWeb(t, p, nil)
	f1 := p.allega(t, c.ID, "fronte.png", sintetiche.PNG(400, 300, 1))
	b := nuovoBrowser(t, p.h)
	b.ip = "198.51.100.31"
	exp := p.o.Adesso().Unix() + 60
	s := firmeflask.Nuovo(segretoStorico)
	buona := "/foto/0046_26/" + f1.NomeFile + "?sig=" + s.FirmaFotoBusta("0046_26", f1.NomeFile, exp) + "&exp=" + strconv.FormatInt(exp, 10)
	for _, perc := range []string{
		"/foto/0046_26/" + f1.NomeFile,
		"/foto/0046_26/" + f1.NomeFile + "?sig=" + strings.Repeat("0", 32) + "&exp=" + strconv.FormatInt(exp, 10),
		"/foto/0046_26/altro.jpg?sig=" + s.FirmaFotoBusta("0046_26", f1.NomeFile, exp) + "&exp=" + strconv.FormatInt(exp, 10),
		"/foto/46_26/" + f1.NomeFile + "?sig=" + s.FirmaFotoBusta("0046_26", f1.NomeFile, exp) + "&exp=" + strconv.FormatInt(exp, 10),
		"/thumb/0046_26/" + f1.NomeFile + "?sig=x&exp=1",
	} {
		for range 3 {
			if r := b.vai("GET", perc, nil, nil); r.codice != http.StatusNotFound || r.corpo != "Foto non disponibile" || r.h.Get("Content-Type") != "" {
				t.Fatalf("%s: %d %q", perc, r.codice, r.corpo)
			}
		}
	}
	if r := b.vai("GET", buona, nil, nil); r.codice != 200 {
		t.Fatalf("foto buona dopo 15 rifiuti: %d", r.codice)
	}
	p.o.Sposta(61 * time.Second)
	if r := b.vai("GET", buona, nil, nil); r.codice != http.StatusNotFound {
		t.Errorf("scaduta: %d", r.codice)
	}
	for i := range 10 {
		perc := "/stato/0046/26?pw=SBAGLIATO"
		if i%2 == 1 {
			perc = "/stato/0046/26/poll?pw=SBAGLIATO"
		}
		if r := b.vai("GET", perc, nil, nil); r.codice != http.StatusNotFound {
			t.Fatalf("fallito %d: %d", i, r.codice)
		}
	}
	if r := b.vai("GET", "/stato/0046/26?pw=CODICE", nil, nil); r.codice != http.StatusNotFound || r.corpo != "Riparazione non disponibile" {
		t.Errorf("bloccato: %d", r.codice)
	}
	b.ip = "198.51.100.32"
	if r := b.vai("GET", "/stato/0046/26?pw=CODICE", nil, nil); r.codice != http.StatusOK {
		t.Errorf("altro indirizzo: %d", r.codice)
	}
}

// inventario: CLI-16
// Senza segreto Flask le firme sono rifiutate, il codice breve funziona, e le foto della pagina hanno
// la firma vuota (sig=), come lo standalone senza LEGACY_WARRANTY_SECRET_FILE.
func TestStoricaSenzaSegreto(t *testing.T) {
	p := paginaClienteProva(t)
	c := storicaWeb(t, p, nil)
	f1 := p.allega(t, c.ID, "fronte.png", sintetiche.PNG(400, 300, 1))
	p.b.ConSegretoFlask(firmeflask.Segreto{})
	b := nuovoBrowser(t, p.h)
	if r := b.vai("GET", "/stato/0046/26?t="+firmaStoricaWeb("0046/26", 2_000_000_000)+"&exp=2000000000", nil, nil); r.codice != http.StatusNotFound {
		t.Errorf("firma senza segreto: %d", r.codice)
	}
	r := b.vai("GET", "/stato/0046/26?pw=CODICE", nil, nil)
	if r.codice != 200 {
		t.Fatalf("codice: %d", r.codice)
	}
	contiene(t, "foto", r.corpo, `src="/foto/0046_26/`+f1.NomeFile+`?sig=&amp;exp=`)
}

// Le parole e la codifica degli indirizzi come Phoenix e Plug (misurate nell'immagine dello
// standalone): segmenti con i soli caratteri non riservati, query nell'ordine dato.
func TestIndirizziComePhoenix(t *testing.T) {
	if g := segmentoPhoenix("a b+c:d@e&f=g(h)~_-.jpg"); g != "a%20b%2Bc%3Ad%40e%26f%3Dg%28h%29~_-.jpg" {
		t.Errorf("segmento: %q", g)
	}
	if g := queryPlug([]campoNascosto{{"pw", "A B"}, {"t", "x"}, {"exp", "2"}}); g != "pw=A+B&t=x&exp=2" {
		t.Errorf("query: %q", g)
	}
}
