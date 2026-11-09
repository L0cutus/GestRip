package web

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/imaging/sintetiche"
)

// Il preventivo e le foto della pagina del cliente (CLI-04…07) e il riquadro della scheda
// (CLI-09, RIP-S-22), come public_repair_controller_test.exs e repair_quote_live_test.exs dello
// standalone (go-baseline).

func (p provaCliente) inPreventivo(t *testing.T, preventivo string, f func(*buste.Accettazione)) buste.Creata {
	t.Helper()
	c := p.busta(t, func(a *buste.Accettazione) {
		a.Preventivo = preventivo
		if f != nil {
			f(a)
		}
	})
	if _, err := p.b.CambiaStato(context.Background(), c.ID, "preventivo", audit.Contesto{Attore: "titolare@esempio.it"}); err != nil {
		t.Fatal(err)
	}
	return c
}

// inventario: CLI-05
// Il blocco del preventivo: senza importo «in corso di definizione» e nessun pulsante; con importo
// la richiesta, e con ?rifiuta=1 il pannello di conferma; fuori da «preventivo» niente.
func TestPaginaClientePreventivoBlocco(t *testing.T) {
	p := paginaClienteProva(t)
	b := nuovoBrowser(t, p.h)
	senza := p.inPreventivo(t, "", nil)
	r := b.vai("GET", "/r/"+senza.Token, nil, nil)
	contiene(t, "senza importo", r.corpo, `<section id="public-quote" class="cliente-preventivo">`, `id="public-quote-pricing"`,
		"ℹ️ Preventivo in corso di definizione", "Le faremo sapere appena pronto.")
	nonContiene(t, "senza importo", r.corpo, "public-quote-approve", "public-quote-refuse", "/preventivo/")

	con := p.inPreventivo(t, "400/500", nil)
	r = b.vai("GET", "/r/"+con.Token, nil, nil)
	contiene(t, "con importo", r.corpo, `id="public-quote-pending"`, `<p class="cliente-preventivo-cifra">400/500</p>`,
		"Confermando autorizza l'avvio della lavorazione. Il prezzo finale potrà discostarsi dalla stima preventivata.",
		`<form action="/r/`+con.Token+`/preventivo/approva" method="post">`+"\n"+`<input name="_csrf_token" type="hidden" value="`+b.token+`">`,
		`<button type="submit" id="public-quote-approve">✓ Confermo il preventivo</button>`,
		`<a href="/r/`+con.Token+`?rifiuta=1" id="public-quote-refuse" class="cliente-preventivo-link">✗ Non procedo</a>`)
	nonContiene(t, "con importo", r.corpo, "public-quote-confirm", "public-quote-pricing")

	r = b.vai("GET", "/r/"+con.Token+"?rifiuta=1", nil, nil)
	contiene(t, "conferma", r.corpo, `id="public-quote-confirm"`, "Confermi di NON voler procedere con la riparazione?",
		`<form action="/r/`+con.Token+`/preventivo/rifiuta" method="post">`,
		`<textarea id="public-quote-reason" name="reason" rows="3" maxlength="300"></textarea>`,
		`<button type="submit" id="public-quote-reject">✗ Confermo: non procedo</button>`,
		`<a href="/r/`+con.Token+`" id="public-quote-cancel" class="cliente-preventivo-annulla">Annulla</a>`)
	nonContiene(t, "conferma", r.corpo, "public-quote-pending")
	for _, q := range []string{"?rifiuta=0", "?rifiuta=1x", "?rifiuta[]=1"} {
		contiene(t, q, b.vai("GET", "/r/"+con.Token+q, nil, nil).corpo, `id="public-quote-pending"`)
	}

	aperta := p.busta(t, func(a *buste.Accettazione) { a.Preventivo = "40" })
	nonContiene(t, "in lavorazione", b.vai("GET", "/r/"+aperta.Token, nil, nil).corpo, "public-quote", aperta.Token+"/preventivo")
}

// inventario: CLI-06, CLI-07
// Approvazione e rifiuto dalla pagina: CSRF, redirect alla pagina con il messaggio una volta, la
// doppia risposta e quella opposta con i testi dello standalone; un token che non apre niente è il
// 404 della pagina, e il limite lo conta.
func TestPaginaClientePreventivoRisposte(t *testing.T) {
	p := paginaClienteProva(t)
	b := nuovoBrowser(t, p.h)
	c := p.inPreventivo(t, "40", nil)
	pagina := "/r/" + c.Token
	b.vai("GET", pagina, nil, nil)
	// Senza token CSRF: 403, niente scritto.
	if r := b.vai("POST", pagina+"/preventivo/approva", url.Values{}, nil); r.codice != http.StatusForbidden {
		t.Fatalf("senza CSRF: %d", r.codice)
	}
	if contaSQL(t, p.d, `SELECT count(*) FROM eventi_preventivo`) != 0 {
		t.Fatal("scritto senza CSRF")
	}
	risponde := func(azione string, form url.Values, info, errore string) {
		t.Helper()
		b.vai("GET", pagina, nil, nil)
		if form == nil {
			form = url.Values{}
		}
		form.Set("_csrf_token", b.token)
		r := b.vai("POST", pagina+"/preventivo/"+azione, form, nil)
		if !redirectA(r, pagina) || r.h.Get("Cache-Control") != "no-store" || r.h.Get("Referrer-Policy") != "no-referrer" {
			t.Fatalf("%s: %d %q %v", azione, r.codice, r.location, r.h)
		}
		g := b.vai("GET", pagina, nil, nil)
		if info != "" {
			contiene(t, azione, g.corpo, `<div id="public-flash-info" class="cliente-esito">`+info+`</div>`)
		}
		if errore != "" {
			contiene(t, azione, g.corpo, `<div id="public-flash-error" class="cliente-esito cliente-esito-errore">`+errore+`</div>`)
		}
		if info == "" {
			nonContiene(t, azione, g.corpo, "public-flash-info")
		}
		if errore == "" {
			nonContiene(t, azione, g.corpo, "public-flash-error")
		}
		// Una volta sola.
		nonContiene(t, azione+" dopo", b.vai("GET", pagina, nil, nil).corpo, "public-flash")
	}
	risponde("approva", nil, "Grazie: il preventivo è stato approvato.", "")
	if got := colonna(t, p.d, c.ID, "stato"); got != "in_lavorazione" {
		t.Errorf("stato %s", got)
	}
	if n := contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'quote.approved' AND attore = 'public' AND ip = '127.0.0.1'`); n != 1 {
		t.Errorf("audit %d", n)
	}
	// La risposta data vince sullo stato: approvata e passata in lavorazione è «già approvato».
	risponde("approva", nil, "Questo preventivo era già stato approvato.", "")
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'preventivo' WHERE id = ?`, c.ID)
	risponde("approva", nil, "Questo preventivo era già stato approvato.", "")
	risponde("rifiuta", nil, "", "Ha già confermato il preventivo. Contatti il negozio se vuole cambiare scelta.")

	r := p.inPreventivo(t, "40", nil)
	pagina = "/r/" + r.Token
	risponde("rifiuta", url.Values{"reason": {"  Troppo <caro> & \"lento\"  "}}, "Abbiamo registrato che non vuole procedere.", "")
	g := b.vai("GET", pagina, nil, nil)
	contiene(t, "rifiutata", g.corpo, `id="public-quote-rejected"`, "🤝 Hai dichiarato di non procedere", "<p>il 31/10/2026 10:00</p>",
		`<p><em>"Troppo &lt;caro&gt; &amp; &#34;lento&#34;"</em></p>`, "Se vuole cambiare scelta, contatti il negozio.")
	nonContiene(t, "rifiutata", g.corpo, "public-quote-approve")
	risponde("rifiuta", nil, "Aveva già dichiarato di non procedere.", "")
	risponde("approva", nil, "", "Ha già dichiarato di non procedere. Contatti il negozio se vuole cambiare scelta.")
	if got := colonna(t, p.d, r.ID, "stato"); got != "preventivo" {
		t.Errorf("il rifiuto ha cambiato lo stato: %s", got)
	}

	// Approvata e rimessa in preventivo senza revoca: il blocco dice la risposta, con l'importo.
	contiene(t, "approvata", b.vai("GET", "/r/"+c.Token, nil, nil).corpo, `id="public-quote-approved"`, "🤝 Preventivo confermato",
		"<p>il 31/10/2026 10:00<span> · importo: 40</span></p>")

	// Token che non apre niente: il 404 della pagina, uguale al GET, senza scrivere.
	prima := contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit`)
	for _, tok := range []string{"NON-ESISTE-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", "a%2Fb"} {
		for _, az := range []string{"approva", "rifiuta"} {
			b.vai("GET", "/r/"+c.Token, nil, nil)
			x := b.vai("POST", "/r/"+tok+"/preventivo/"+az, url.Values{"_csrf_token": {b.token}}, nil)
			if x.codice != http.StatusNotFound || x.corpo != "Riparazione non disponibile" {
				t.Errorf("%s %s: %d %q", tok, az, x.codice, x.corpo)
			}
		}
	}
	if n := contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit`); n != prima {
		t.Errorf("audit su un token sbagliato: %d, prima %d", n, prima)
	}
	if strings.Contains(p.reg.String(), c.Token) || strings.Contains(p.reg.String(), r.Token) {
		t.Error("il token è nel registro")
	}
}

// inventario: CLI-02, CLI-21
// I POST con un token sbagliato contano nel limite come le pagine: al 31° anche la pagina buona è
// ferma.
func TestPaginaClientePreventivoLimite(t *testing.T) {
	p := paginaClienteProva(t)
	b := nuovoBrowser(t, p.h)
	c := p.inPreventivo(t, "40", nil)
	b.vai("GET", "/r/"+c.Token, nil, nil)
	tok := b.token
	for i := range 30 {
		if r := b.vai("POST", "/r/SBAGLIATO-"+strconv.Itoa(i)+"/preventivo/approva", url.Values{"_csrf_token": {tok}}, nil); r.codice != http.StatusNotFound {
			t.Fatalf("tentativo %d: %d", i, r.codice)
		}
	}
	if r := b.vai("POST", "/r/"+c.Token+"/preventivo/approva", url.Values{"_csrf_token": {tok}}, nil); r.codice != http.StatusNotFound {
		t.Errorf("dopo 30 falliti: %d", r.codice)
	}
	if got := colonna(t, p.d, c.ID, "stato"); got != "preventivo" {
		t.Errorf("stato %s", got)
	}
}

func (p provaCliente) allega(t *testing.T, busta int64, nome string, b []byte) buste.Foto {
	t.Helper()
	in := filepath.Join(p.dir, "in-"+nome)
	if err := os.WriteFile(in, b, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := p.b.AllegaFoto(context.Background(), busta, []buste.Caricamento{{Percorso: in, NomeOriginale: nome}}, nil,
		audit.Contesto{Attore: "titolare@esempio.it"})
	if err != nil || len(f) != 1 {
		t.Fatalf("AllegaFoto: %v", err)
	}
	return f[0]
}

// inventario: CLI-04
// Le foto della pagina: quelle visibili a chi ha il link («cliente» su una busta diretta, «orefice»
// su una B2B), miniature e visualizzatore; ogni lettura di una busta trovata scrive la sua riga;
// ogni rifiuto è lo stesso 404 «Foto non disponibile»; un link morto non scrive niente.
func TestPaginaClienteFoto(t *testing.T) {
	p := paginaClienteProva(t)
	b := nuovoBrowser(t, p.h)
	diretta := p.busta(t, nil)
	f1 := p.allega(t, diretta.ID, "uno.png", sintetiche.PNG(1000, 750, 1))
	f2 := p.allega(t, diretta.ID, "due.png", sintetiche.PNG(400, 300, 2))
	nascosta := p.allega(t, diretta.ID, "tre.png", sintetiche.PNG(300, 300, 3))
	eseguiSQL(t, p.d, `UPDATE foto_buste SET visibile_cliente = 0 WHERE id = ?`, nascosta.ID)
	b2b := p.busta(t, func(a *buste.Accettazione) { a.OreficeriaID = "2"; a.CodiceQR = "RIP-2026-0046" })
	fo := p.allega(t, b2b.ID, "oro.png", sintetiche.PNG(300, 300, 4))
	senzaFoto := p.busta(t, func(a *buste.Accettazione) { a.CodiceQR = "RIP-2026-0047" })

	tk := "/r/" + diretta.Token
	r := b.vai("GET", tk, nil, nil)
	id := func(f buste.Foto) string { return strconv.FormatInt(f.ID, 10) }
	contiene(t, "galleria", r.corpo, `<section id="public-repair-photos" class="cliente-foto">`,
		`<a href="#public-repair-photo-viewer-`+id(f1)+`"><img id="public-repair-photo-`+id(f1)+`" src="`+tk+`/foto/`+id(f1)+`?size=thumb" alt="Foto della riparazione" loading="lazy"></a>`,
		`<div class="gs-viewer-frame" id="public-repair-photo-viewer-`+id(f2)+`" role="dialog" aria-label="Foto 2 di 2">`,
		`<img src="`+tk+`/foto/`+id(f2)+`" alt="Foto 2" loading="lazy">`,
		`href="#public-repair-photo-viewer-`+id(f1)+`" aria-label="Foto precedente"`, `href="#public-repair-photos">← Indietro</a>`)
	nonContiene(t, "galleria", r.corpo, "public-repair-photo-"+id(nascosta)+`"`, "/foto/"+id(fo))
	nonContiene(t, "senza foto", b.vai("GET", "/r/"+senzaFoto.Token, nil, nil).corpo, "public-repair-photos")
	contiene(t, "b2b", b.vai("GET", "/r/"+b2b.Token, nil, nil).corpo, "/foto/"+id(fo)+"?size=thumb")

	visti := func() int {
		return contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_photo_viewed'`)
	}
	// La foto intera e la miniatura.
	x := b.vai("GET", tk+"/foto/"+id(f1), nil, nil)
	if x.codice != 200 || !unaVolta(x, "Content-Type", "image/jpeg") || !unaVolta(x, "X-Photo-Variant", "full") ||
		!unaVolta(x, "Cache-Control", "no-store") || !unaVolta(x, "Referrer-Policy", "no-referrer") {
		t.Errorf("intera: %d %v", x.codice, x.h)
	}
	x = b.vai("GET", tk+"/foto/"+id(f1)+"?size=thumb", nil, nil)
	if x.codice != 200 || !unaVolta(x, "Content-Type", "image/webp") || !unaVolta(x, "X-Photo-Variant", "thumb") ||
		!unaVolta(x, "Cache-Control", "no-store") {
		t.Errorf("miniatura: %d %v", x.codice, x.h)
	}
	if n := contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_photo_viewed' AND attore = 'public'
		AND esito = 'ok' AND id_risorsa = ? AND dati = ?`, strconv.FormatInt(diretta.ID, 10), `{"audience":"customer","photo_id":`+id(f1)+`,"variant":"thumb"}`); n != 1 {
		t.Errorf("audit della miniatura: %d", n)
	}
	if n := contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_photo_viewed' AND dati = ?`,
		`{"audience":"orefice","photo_id":`+id(fo)+`}`); n != 0 {
		t.Error("audit prima della lettura")
	}
	if x := b.vai("GET", "/r/"+b2b.Token+"/foto/"+id(fo), nil, nil); x.codice != 200 {
		t.Errorf("foto dell'oreficeria: %d", x.codice)
	}
	if n := contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_photo_viewed' AND dati = ?`,
		`{"audience":"orefice","photo_id":`+id(fo)+`}`); n != 1 {
		t.Errorf("audit dell'oreficeria: %d", n)
	}
	// Rifiuti di una busta trovata: «denied»; quelli di un link morto: niente.
	prima := visti()
	for nome, perc := range map[string]string{"nascosta": tk + "/foto/" + id(nascosta), "di un'altra busta": tk + "/foto/" + id(fo),
		"inesistente": tk + "/foto/99999", "malformata": tk + "/foto/abc", "orefice su diretta": "/r/" + b2b.Token + "/foto/" + id(f1)} {
		stessoRifiuto(t, nome, b.vai("GET", perc, nil, nil), b.vai("GET", perc+"?size=thumb", nil, nil))
	}
	if n := visti(); n != prima+10 {
		t.Errorf("righe denied: %d, attese %d", n-prima, 10)
	}
	if n := contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_photo_viewed' AND esito = 'denied' AND dati LIKE '%"photo_id":"invalid"%'`); n != 2 {
		t.Errorf("id malformato: %d", n)
	}
	if _, err := p.b.RevocaLink(context.Background(), diretta.ID, audit.Contesto{Attore: "titolare@esempio.it"}); err != nil {
		t.Fatal(err)
	}
	prima = visti()
	stessoRifiuto(t, "link revocato", b.vai("GET", tk+"/foto/"+id(f1), nil, nil), b.vai("GET", tk+"/foto/"+id(f1)+"?size=thumb", nil, nil))
	stessoRifiuto(t, "token inventato", b.vai("GET", "/r/NO/foto/"+id(f1), nil, nil), b.vai("GET", "/r/NO/foto/"+id(f1)+"?size=thumb", nil, nil))
	if n := visti(); n != prima {
		t.Errorf("audit su un link morto: %d", n-prima)
	}
	// Le foto non contano nel limite: 40 rifiuti e la pagina buona risponde ancora.
	for range 40 {
		b.vai("GET", "/r/NO/foto/1", nil, nil)
	}
	if r := b.vai("GET", "/r/"+b2b.Token, nil, nil); r.codice != 200 {
		t.Errorf("dopo 40 foto rifiutate: %d", r.codice)
	}
}

// inventario: CLI-09, RIP-S-22
// Il riquadro della scheda: la risposta, il motivo, la storia e «Revoca», che revoca la risposta
// mostrata; una pagina vecchia dice «Questa risposta non risulta più registrata.».
func TestSchedaRiquadroPreventivo(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.Preventivo = "40" })
	bb := buste.Nuove(d, o)
	b := dipendenteDentro(t, h)
	nonContiene(t, "senza storia", b.vai("GET", percorsoScheda(id), nil, nil).corpo, "repair-quote")

	const tok = "SINTETICO-token-della-scheda-xxxxxxxxxxxxxx"
	eseguiSQL(t, d, `UPDATE buste SET stato = 'preventivo', hash_token_pubblico = ?, token_pubblico_scade_il = '2027-01-01T00:00:00.000000Z',
		token_pubblico_revocato_il = NULL WHERE id = ?`, buste.ImprontaToken(tok), id)
	o.Sposta(time.Hour)
	if err := bb.RifiutaPreventivo(context.Background(), tok, "Troppo <caro>", audit.Contesto{Attore: "public"}); err != nil {
		t.Fatal(err)
	}
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	sid := strconv.FormatInt(id, 10)
	contiene(t, "rifiutata", r.corpo, `<div id="repair-quote" class="riquadro-preventivo">`, "<strong>🤝 Preventivo</strong>",
		`<span class="segno-risposta segno-rifiutato">Il cliente ha dichiarato di NON procedere il 31/10/2026 11:00</span>`,
		`<form action="/repairs/`+sid+`/preventivo/revoca" method="post"`, `<input type="hidden" name="answer" value="rejected">`,
		`data-confirm="Revocare il rifiuto? Il cliente rivedrà i pulsanti per confermare o rifiutare, e una busta annullata torna a preventivo. Lo storico resta."`,
		`<p id="repair-quote-reason" class="motivo-rifiuto">Motivo: "Troppo &lt;caro&gt;"</p>`,
		`>Preventivo rifiutato · 31/10/2026 11:00 · cliente</li>`)
	nonContiene(t, "rifiutata", r.corpo, "repair-quote-approved")

	// Una pagina vecchia che mostrava un'approvazione: niente da revocare.
	form := func(risposta string) url.Values {
		return url.Values{"_csrf_token": {b.token}, "answer": {risposta}}
	}
	if x := b.vai("POST", percorsoScheda(id)+"/preventivo/revoca", form("approved"), nil); !redirectAllaScheda(x, id) {
		t.Fatalf("revoca vecchia: %d %q", x.codice, x.location)
	}
	contiene(t, "vecchia", b.vai("GET", percorsoScheda(id), nil, nil).corpo, `<div role="alert" class="flash flash-errore">Questa risposta non risulta più registrata.</div>`)
	if x := b.vai("POST", percorsoScheda(id)+"/preventivo/revoca", form("boh"), nil); !redirectAllaScheda(x, id) {
		t.Fatalf("revoca boh: %d", x.codice)
	}
	contiene(t, "boh", b.vai("GET", percorsoScheda(id), nil, nil).corpo, "Richiesta non valida.")
	if n := contaSQL(t, d, `SELECT count(*) FROM eventi_preventivo WHERE busta_id = ?`, id); n != 1 {
		t.Fatalf("storia %d", n)
	}

	o.Sposta(time.Hour)
	if x := b.vai("POST", percorsoScheda(id)+"/preventivo/revoca", form("rejected"), nil); !redirectAllaScheda(x, id) {
		t.Fatalf("revoca: %d %q", x.codice, x.location)
	}
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "revocata", r.corpo, `<div role="status" class="flash flash-info">Fatto: la risposta al preventivo è stata revocata.</div>`,
		`>Preventivo rifiutato · 31/10/2026 11:00 · cliente</li>`, `>Rifiuto revocato · 31/10/2026 12:00 · negozio</li>`)
	nonContiene(t, "revocata", r.corpo, `name="answer"`, "repair-quote-reason")
	if n := contaSQL(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'quote.rejection_revoked' AND attore = 'banco@esempio.it'`); n != 1 {
		t.Errorf("audit %d", n)
	}
	// Il cliente approva; il riquadro mostra l'approvazione e la sua revoca.
	if err := bb.ApprovaPreventivo(context.Background(), tok, audit.Contesto{Attore: "public"}); err != nil {
		t.Fatal(err)
	}
	contiene(t, "approvata", b.vai("GET", percorsoScheda(id), nil, nil).corpo,
		`<span class="segno-risposta segno-approvato">Confermato dal cliente il 31/10/2026 12:00</span>`, `<input type="hidden" name="answer" value="approved">`,
		`data-confirm="Revocare l'approvazione? La busta resta nello stato in cui si trova: riportala a preventivo se vuoi che il cliente decida di nuovo. Lo storico resta."`)
	for nome, perc := range map[string]string{"id malformato": "/repairs/abc/preventivo/revoca", "busta che non c'è": "/repairs/999/preventivo/revoca"} {
		if x := b.vai("POST", perc, form("approved"), nil); !redirectA(x, "/") {
			t.Errorf("%s: %d %q", nome, x.codice, x.location)
		}
	}
}
