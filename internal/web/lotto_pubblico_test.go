package web

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/buste"
)

// La pagina pubblica del lotto /lotto/{token} e le sue foto (lotto_pubblico.go, slice 092, ORE-14, ORE-15, ORE-16),
// come public_lot_controller_test.exs dello standalone (go-baseline), sulla faccia pubblica vera con le buste, le
// foto e il profilo del negozio («Bottega di Prova») di facceAnteprima. L'oreficeria 2 è «Oreficeria Esempio»; le
// buste sono accettate il 01/10/2026. Nomi, gettoni e indirizzi inventati.

// gettoneFlask092 ha la forma di secrets.token_urlsafe(24) del Flask: 32 caratteri url-safe, come arriva con l'import.
const gettoneFlask092 = "SINTETICOflaskLotto_32caratteri-"

// lottoPubblico092 mette un lotto dell'oreficeria 2 inviato il 31/10/2026, aperto alle 09:05 del negozio, con il
// gettone e le buste.
func lottoPubblico092(t *testing.T, p *provaAnteprima, gettone string, buste ...int64) int64 {
	t.Helper()
	l := lottoProva(t, p.d, 2, "2026-10-31T08:05:00.000000Z")
	eseguiSQL(t, p.d, `UPDATE lotti_avviso SET token = ?, inviato_il = '2026-10-31T08:30:00.000000Z' WHERE id = ?`, gettone, l)
	for _, b := range buste {
		eseguiSQL(t, p.d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, l, b)
	}
	return l
}

// prontaDell092 è una busta dell'oreficeria 2, pronta il 15/10/2026, con la descrizione e il codice dati.
func prontaDell092(t *testing.T, p *provaAnteprima, codice, descrizione string) int64 {
	t.Helper()
	c := p.busta(t, func(a *buste.Accettazione) {
		a.OreficeriaID, a.CodiceQR, a.Descrizione, a.Preventivo = "2", codice, descrizione, ""
	})
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'pronta', data_pronta = '2026-10-15' WHERE id = ?`, c.ID)
	return c.ID
}

// inventario: ORE-14
// La pagina di un lotto inviato, aperta con il gettone di 32 caratteri del Flask: no-store e no-referrer, niente
// X-Robots-Tag; il nome del negozio, «Buongiorno <oreficeria>», il conto con il giorno del lotto nel calendario del
// negozio, il totale dei soli importi esatti con «(N senza importo esatto)», «N ancora da ritirare su M», le buste
// dalla più recente con il numero visibile (D-64), la data di pronta, la descrizione (oltre 100 grafemi tagliata),
// «✓ Pronta» o «✓ Ritirata <data>» e il prezzo; in fondo il nome, l'indirizzo con CAP e città e il telefono del
// profilo del negozio. Una busta di un'altra oreficeria o nel cestino non c'è. Aprirla non scrive righe d'audit.
func TestPaginaLottoPubblico(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	eseguiSQL(t, p.d, `UPDATE profilo_negozio SET indirizzo = 'Via Inventata 1', cap = '00000', citta = 'Paese di Prova', telefono = '0490 000 111'`)
	eseguiSQL(t, p.d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Altra Oreficeria')`)
	lunga := strings.Repeat("é", 99) + "àb"
	uno := prontaDell092(t, p, "RIP-2026-0045", "Anello")
	due := prontaDell092(t, p, "", "Collana")
	tre := prontaDell092(t, p, "", lunga)
	altra := prontaDell092(t, p, "", "Segreto di un'altra")
	cestino := prontaDell092(t, p, "", "Nel cestino")
	eseguiSQL(t, p.d, `UPDATE buste SET prezzo_testo = '40,00', prezzo_centesimi = 4000 WHERE id = ?`, uno)
	eseguiSQL(t, p.d, `UPDATE buste SET prezzo_testo = '400/500', prezzo_centesimi = NULL WHERE id = ?`, due)
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'consegnata', data_consegna = '2026-10-30' WHERE id = ?`, tre)
	lottoPubblico092(t, p, gettoneFlask092, uno, due, tre, altra, cestino)
	eseguiSQL(t, p.d, `UPDATE buste SET oreficeria_id = 3 WHERE id = ?`, altra)
	eseguiSQL(t, p.d, `UPDATE buste SET cancellata_il = '2026-10-31T08:00:00.000000Z' WHERE id = ?`, cestino)
	audit := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit`)
	pub := nuovoBrowser(t, p.pubblica)
	r := pub.vai("GET", "/lotto/"+gettoneFlask092, nil, nil)
	if r.codice != 200 || r.h.Get("Cache-Control") != "no-store" || r.h.Get("Referrer-Policy") != "no-referrer" || r.h.Get("X-Robots-Tag") != "" ||
		r.h.Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("pagina: %d %v", r.codice, r.h)
	}
	contiene(t, "pagina", r.corpo,
		`<p class="lotto-pubblico-negozio">Bottega di Prova</p>`+"\n"+`<h1 id="public-lot-greeting">Buongiorno Oreficeria Esempio</h1>`+"\n"+
			`<p id="public-lot-count">3 riparazioni pronte al ritiro · lotto del 31/10/2026</p>`+"\n"+
			`<div id="public-lot-total" class="lotto-pubblico-totale"><span>Totale lotto</span> <strong>€ 40,00</strong></div>`+"\n"+
			`<p id="public-lot-unpriced" class="lotto-pubblico-senza">(2 senza importo esatto)</p>`+"\n"+`</header>`+"\n"+
			`<div id="public-lot-partial" class="lotto-pubblico-stato">2 ancora da ritirare su 3</div>`,
		`<li id="public-lot-bag-`+itoa(tre)+`" class="lotto-pubblico-ritirata">`+"\n"+
			`<div class="lotto-pubblico-riga"><strong class="font-mono">N. 3/26</strong> <span>pronta 15/10/2026</span></div>`+"\n"+
			`<div class="lotto-pubblico-corpo"><p>`+strings.Repeat("é", 99)+"à…"+`</p></div>`+"\n"+
			`<div class="lotto-pubblico-riga"><span class="lotto-pubblico-badge lotto-pubblico-badge-ritirata">✓ Ritirata 30/10/2026</span></div>`,
		`<li id="public-lot-bag-`+itoa(due)+`">`+"\n"+`<div class="lotto-pubblico-riga"><strong class="font-mono">N. 2/26</strong>`,
		`<span class="lotto-pubblico-badge">✓ Pronta</span> <span class="font-mono lotto-pubblico-prezzo">€ 400/500</span>`,
		`<strong class="font-mono">0045/26</strong>`, `<span class="font-mono lotto-pubblico-prezzo">€ 40,00</span>`,
		`<footer class="lotto-pubblico-piede">`+"\n"+`<strong>Bottega di Prova</strong>`+"\n"+`<p>Via Inventata 1 00000 Paese di Prova</p>`+"\n"+
			`<p><a href="tel:0490 000 111">0490 000 111</a></p>`+"\n"+`</footer>`)
	nonContiene(t, "pagina", r.corpo, "Segreto di un", "Nel cestino", "public-lot-complete", "public-lot-pending", "public-lot-empty",
		"public-lot-photo-", "Ada Inventata", "333 987 6543")
	if i, j, k := strings.Index(r.corpo, "public-lot-bag-"+itoa(tre)), strings.Index(r.corpo, "public-lot-bag-"+itoa(due)), strings.Index(r.corpo, "public-lot-bag-"+itoa(uno)); i > j || j > k {
		t.Error("le buste non sono dalla più recente")
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit`); n != audit {
		t.Errorf("righe d'audit: %d, prima %d", n, audit)
	}
	// Una busta sola e senza profilo del negozio: «1 riparazione pronta», «(1 senza importo)», niente totale, il piè
	// con «Negozio» e nient'altro.
	eseguiSQL(t, p.d, `DELETE FROM profilo_negozio`)
	quattro := prontaDell092(t, p, "", "Spilla")
	lottoPubblico092(t, p, "gettone-una-busta", quattro)
	r = pub.vai("GET", "/lotto/gettone-una-busta", nil, nil)
	contiene(t, "una busta", r.corpo, `<p id="public-lot-count">1 riparazione pronta al ritiro · lotto del 31/10/2026</p>`+"\n"+
		`<p id="public-lot-unpriced" class="lotto-pubblico-senza">(1 senza importo)</p>`,
		`<footer class="lotto-pubblico-piede">`+"\n"+`<strong>Negozio</strong>`+"\n"+`</footer>`)
	nonContiene(t, "una busta", r.corpo, "public-lot-total")
	// Un profilo con il telefono vuoto e l'indirizzo di soli campi vuoti: il piè ha solo il nome.
	eseguiSQL(t, p.d, `INSERT INTO profilo_negozio (nome, indirizzo, cap, citta, telefono, condizioni_garanzia, creato_il, aggiornato_il)
		VALUES ('Bottega di Prova', '', '', '', '', '', ?, ?)`, istanteHome, istanteHome)
	r = pub.vai("GET", "/lotto/gettone-una-busta", nil, nil)
	contiene(t, "telefono vuoto", r.corpo, `<footer class="lotto-pubblico-piede">`+"\n"+`<strong>Bottega di Prova</strong>`+"\n"+`</footer>`)
	nonContiene(t, "telefono vuoto", r.corpo, "tel:")
}

// inventario: ORE-14
// Il giorno del lotto è quello del negozio: le 22:30 UTC del 15 luglio sono già il 16 a Roma.
func TestLottoPubblicoGiornoDelNegozio(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	l := lottoPubblico092(t, p, "gettone-giorno", prontaDell092(t, p, "", "Anello"))
	eseguiSQL(t, p.d, `UPDATE lotti_avviso SET creato_il = '2026-07-15T22:30:00.000000Z' WHERE id = ?`, l)
	r := nuovoBrowser(t, p.pubblica).vai("GET", "/lotto/gettone-giorno", nil, nil)
	contiene(t, "giorno", r.corpo, "lotto del 16/07/2026")
}

// inventario: ORE-14
// Un gettone che nessuno ha coniato, un lotto non ancora inviato, un lotto importato con l'accesso spento
// («disabled-import-…») e un gettone quasi giusto sono la stessa risposta, byte per byte: il 404 «Lotto non
// disponibile» senza Content-Type, con no-store e no-referrer.
func TestLottoPubblicoStessoRifiuto(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	l := lottoPubblico092(t, p, gettoneFlask092, prontaDell092(t, p, "", "Anello"))
	aperto := lottoProva(t, p.d, 2, "2026-10-31T08:05:00.000000Z")
	eseguiSQL(t, p.d, `UPDATE lotti_avviso SET token = 'gettone-mai-inviato' WHERE id = ?`, aperto)
	spento := lottoPubblico092(t, p, "disabled-import-12", prontaDell092(t, p, "", "Collana"))
	eseguiSQL(t, p.d, `UPDATE lotti_avviso SET accesso_pubblico_spento_il = '2026-10-31T08:00:00.000000Z' WHERE id = ?`, spento)
	pub := nuovoBrowser(t, p.pubblica)
	if r := pub.vai("GET", "/lotto/"+gettoneFlask092, nil, nil); r.codice != 200 {
		t.Fatalf("il gettone del Flask: %d", r.codice)
	}
	var primo *risposta
	for _, g := range []string{"nessuno", "gettone-mai-inviato", "disabled-import-12", gettoneFlask092[:31], gettoneFlask092 + "x",
		strings.ToLower(gettoneFlask092), "abc%2Fdef", "a%20b"} {
		r := pub.vai("GET", "/lotto/"+g, nil, nil)
		if r.codice != 404 || r.corpo != "Lotto non disponibile" || r.h.Get("Cache-Control") != "no-store" ||
			r.h.Get("Referrer-Policy") != "no-referrer" || r.h.Values("Content-Type") != nil || r.h.Values("X-Robots-Tag") != nil {
			t.Errorf("%s: %d %q %v", g, r.codice, r.corpo, r.h)
		}
		if primo == nil {
			primo = &r
			continue
		}
		for _, k := range []string{"Cache-Control", "Referrer-Policy", "Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Content-Length"} {
			if r.h.Get(k) != primo.h.Get(k) {
				t.Errorf("%s: %s %q, il primo rifiuto %q", g, k, r.h.Get(k), primo.h.Get(k))
			}
		}
	}
	// Il lotto inviato con l'accesso spento dopo l'invio non risponde più.
	eseguiSQL(t, p.d, `UPDATE lotti_avviso SET accesso_pubblico_spento_il = '2026-10-31T08:00:00.000000Z' WHERE id = ?`, l)
	if r := pub.vai("GET", "/lotto/"+gettoneFlask092, nil, nil); r.codice != 404 || r.corpo != "Lotto non disponibile" {
		t.Errorf("spento: %d %q", r.codice, r.corpo)
	}
}

// inventario: ORE-14, CLI-21
// I rifiuti di /lotto contano nel limite dei tentativi falliti: dopo trenta da un indirizzo anche il gettone giusto
// riceve lo stesso 404, gli altri indirizzi no. Una pagina che risponde non conta, e i 404 delle foto non contano.
func TestLottoPubblicoLimiteDeiFalliti(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	uno := prontaDell092(t, p, "", "Anello")
	lottoPubblico092(t, p, gettoneFlask092, uno)
	foto := p.allega(t, uno, "busta.png")
	a := nuovoBrowser(t, p.pubblica)
	a.ip = "203.0.113.40"
	for range 40 {
		if r := a.vai("GET", "/lotto/"+gettoneFlask092, nil, nil); r.codice != 200 {
			t.Fatalf("pagina buona: %d", r.codice)
		}
		if r := a.vai("GET", "/lotto/"+gettoneFlask092+"/buste/"+itoa(uno)+"/foto/999", nil, nil); r.codice != 404 {
			t.Fatalf("foto che non c'è: %d", r.codice)
		}
	}
	for i := range 30 {
		a.vai("GET", fmt.Sprintf("/lotto/sbagliato-%d", i), nil, nil)
	}
	if r := a.vai("GET", "/lotto/"+gettoneFlask092, nil, nil); r.codice != 404 || r.corpo != "Lotto non disponibile" {
		t.Errorf("bloccato: %d %q", r.codice, r.corpo)
	}
	// Le foto non sono nella famiglia: il blocco non le tocca.
	if r := a.vai("GET", "/lotto/"+gettoneFlask092+"/buste/"+itoa(uno)+"/foto/"+itoa(foto.ID), nil, nil); r.codice != http.StatusOK {
		t.Errorf("foto dall'indirizzo bloccato: %d", r.codice)
	}
	b := nuovoBrowser(t, p.pubblica)
	b.ip = "203.0.113.41"
	if r := b.vai("GET", "/lotto/"+gettoneFlask092, nil, nil); r.codice != 200 {
		t.Errorf("altro indirizzo: %d", r.codice)
	}
}

// inventario: ORE-15
// Leggere la pagina chiude il lotto completo: «✓ Lotto completato» e chiuso_il scritto una volta; aggiornare la
// pagina non cambia la data. A metà «N ancora da ritirare su M»; con le visibili tutte ritirate ma una busta nel
// cestino ancora da consegnare «Il lotto non è ancora completato.» e niente chiusura; senza buste «Nessuna busta in
// questo lotto.». Una HEAD si comporta come la GET (Plug.Head).
func TestLottoPubblicoChiusuraAllaLettura(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	uno, due := prontaDell092(t, p, "", "Anello"), prontaDell092(t, p, "", "Collana")
	l := lottoPubblico092(t, p, "gettone-chiusura", uno, due)
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'consegnata', data_consegna = '2026-10-31' WHERE id = ?`, uno)
	pub := nuovoBrowser(t, p.pubblica)
	r := pub.vai("GET", "/lotto/gettone-chiusura", nil, nil)
	contiene(t, "a metà", r.corpo, `<div id="public-lot-partial" class="lotto-pubblico-stato">1 ancora da ritirare su 2</div>`)
	chiuso := func() string {
		t.Helper()
		var s *string
		if err := p.d.Lettura.QueryRow(`SELECT chiuso_il FROM lotti_avviso WHERE id = ?`, l).Scan(&s); err != nil {
			t.Fatal(err)
		}
		if s == nil {
			return ""
		}
		return *s
	}
	if chiuso() != "" {
		t.Fatal("chiuso a metà")
	}
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'consegnata', data_consegna = '2026-10-31' WHERE id = ?`, due)
	if r := pub.vai("HEAD", "/lotto/gettone-chiusura", nil, nil); r.codice != 200 || r.corpo != "" || chiuso() != "2026-10-31T09:00:00.000000Z" {
		t.Fatalf("HEAD: %d %q %q", r.codice, r.corpo, chiuso())
	}
	p.o.Sposta(3600 * 1e9)
	r = pub.vai("GET", "/lotto/gettone-chiusura", nil, nil)
	contiene(t, "completo", r.corpo, `</header>`+"\n"+`<div id="public-lot-complete" class="lotto-pubblico-stato lotto-pubblico-completo">✓ Lotto completato — tutte le riparazioni sono state ritirate</div>`+"\n"+`<ul`)
	nonContiene(t, "completo", r.corpo, "public-lot-partial", "public-lot-pending")
	if chiuso() != "2026-10-31T09:00:00.000000Z" {
		t.Errorf("la seconda lettura ha cambiato la data: %s", chiuso())
	}
	// Il cestino tiene aperto.
	tre, quattro := prontaDell092(t, p, "", "Spilla"), prontaDell092(t, p, "", "Orecchini")
	m := lottoPubblico092(t, p, "gettone-cestino", tre, quattro)
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, tre)
	eseguiSQL(t, p.d, `UPDATE buste SET cancellata_il = '2026-10-31T08:00:00.000000Z' WHERE id = ?`, quattro)
	r = pub.vai("GET", "/lotto/gettone-cestino", nil, nil)
	contiene(t, "cestino", r.corpo, `<div id="public-lot-pending" class="lotto-pubblico-stato">Il lotto non è ancora completato.</div>`)
	nonContiene(t, "cestino", r.corpo, "Lotto completato", "Orecchini")
	if n := contaRighe(t, p.d, `SELECT count(*) FROM lotti_avviso WHERE id = `+itoa(m)+` AND chiuso_il IS NOT NULL`); n != 0 {
		t.Error("chiuso con una busta nel cestino da consegnare")
	}
	lottoPubblico092(t, p, "gettone-vuoto")
	r = pub.vai("GET", "/lotto/gettone-vuoto", nil, nil)
	contiene(t, "vuoto", r.corpo, `<p id="public-lot-count">0 riparazioni pronte al ritiro · lotto del 31/10/2026</p>`,
		`<div id="public-lot-empty" class="lotto-pubblico-stato lotto-pubblico-vuoto">Nessuna busta in questo lotto.</div>`)
	nonContiene(t, "vuoto", r.corpo, "public-lot-total", "public-lot-unpriced")
}

// inventario: ORE-16
// Ogni busta mostra la prima foto che l'oreficeria può vedere, come miniatura (?size=thumb) sotto il gettone del
// lotto; la rotta serve la foto (no-store, no-referrer, X-Photo-Variant) e, con ?size=thumb esatto, la miniatura.
// Solo «size=thumb» è una miniatura. Una foto nascosta all'oreficeria, di un'altra busta del lotto, una busta fuori
// dal lotto o passata a un'altra oreficeria, un gettone che non c'è e un id finto sono lo stesso 404 «Foto non
// disponibile», per la foto e per la miniatura. Nascosta la prima, la pagina passa alla seconda.
func TestFotoLottoPubblico(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	eseguiSQL(t, p.d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Altra Oreficeria')`)
	uno, due, fuori := prontaDell092(t, p, "", "Anello"), prontaDell092(t, p, "", "Collana"), prontaDell092(t, p, "", "Spilla")
	prima, seconda := p.allega(t, uno, "busta.png"), p.allega(t, uno, "oggetto.png")
	diDue, diFuori := p.allega(t, due, "due.png"), p.allega(t, fuori, "fuori.png")
	lottoPubblico092(t, p, gettoneFlask092, uno, due)
	pub := nuovoBrowser(t, p.pubblica)
	base := "/lotto/" + gettoneFlask092 + "/buste/"
	src := func(b, f int64) string { return base + itoa(b) + "/foto/" + itoa(f) }
	r := pub.vai("GET", "/lotto/"+gettoneFlask092, nil, nil)
	contiene(t, "pagina", r.corpo,
		`<div class="lotto-pubblico-corpo"><img id="public-lot-photo-`+itoa(uno)+`" src="`+src(uno, prima.ID)+`?size=thumb" alt="Foto busta 1/26" loading="lazy"><p>Anello</p></div>`,
		`<img id="public-lot-photo-`+itoa(due)+`" src="`+src(due, diDue.ID)+`?size=thumb"`)
	nonContiene(t, "pagina", r.corpo, "/foto/"+itoa(diFuori.ID))
	f := pub.vai("GET", src(uno, prima.ID), nil, nil)
	if f.codice != 200 || f.h.Get("Content-Type") != "image/jpeg" || f.h.Get("X-Photo-Variant") != "full" || f.h.Get("Cache-Control") != "no-store" ||
		f.h.Get("Referrer-Policy") != "no-referrer" || f.h.Get("X-Robots-Tag") != "" {
		t.Fatalf("foto: %d %v", f.codice, f.h)
	}
	if m := pub.vai("GET", src(uno, prima.ID)+"?size=thumb", nil, nil); m.codice != 200 || m.h.Get("Content-Type") != "image/webp" || m.h.Get("X-Photo-Variant") != "thumb" {
		t.Errorf("miniatura: %d %v", m.codice, m.h)
	}
	for _, q := range []string{"", "?size=full", "?size=THUMB", "?size=", "?size[]=thumb", "?thumb=1"} {
		if m := pub.vai("GET", src(uno, prima.ID)+q, nil, nil); m.codice != 200 || m.h.Get("X-Photo-Variant") != "full" || m.h.Get("Content-Type") != "image/jpeg" {
			t.Errorf("%q: %d %v", q, m.codice, m.h)
		}
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_photo_viewed' AND esito = 'ok' AND attore = 'public' AND dati LIKE '%"audience":"orefice"%'`); n != 8 {
		t.Errorf("righe ok: %d", n)
	}
	rifiuti := []string{
		src(uno, diDue.ID), src(fuori, diFuori.ID), src(uno, 0), base + itoa(uno) + "/foto/zero", base + "abc/foto/" + itoa(prima.ID),
		"/lotto/nessuno/buste/" + itoa(uno) + "/foto/" + itoa(prima.ID),
	}
	controlla := func(perc string) {
		t.Helper()
		for _, q := range []string{"", "?size=thumb"} {
			m := pub.vai("GET", perc+q, nil, nil)
			if m.codice != 404 || m.corpo != "Foto non disponibile" || m.h.Values("Content-Type") != nil || m.h.Get("Cache-Control") != "no-store" ||
				m.h.Get("Referrer-Policy") != "no-referrer" || m.h.Get("X-Photo-Variant") != "" {
				t.Errorf("%s%s: %d %q %v", perc, q, m.codice, m.corpo, m.h)
			}
		}
	}
	for _, perc := range rifiuti {
		controlla(perc)
	}
	eseguiSQL(t, p.d, `UPDATE foto_buste SET visibile_oreficeria = 0 WHERE id = ?`, prima.ID)
	controlla(src(uno, prima.ID))
	r = pub.vai("GET", "/lotto/"+gettoneFlask092, nil, nil)
	contiene(t, "dopo", r.corpo, `src="`+src(uno, seconda.ID)+`?size=thumb"`)
	nonContiene(t, "dopo", r.corpo, "/foto/"+itoa(prima.ID))
	// Una busta di un secondo lotto inviato, della stessa oreficeria o di un'altra: con il gettone di questo lotto è il
	// 404 per la foto e la miniatura, senza righe «ok» in più; con il gettone del suo lotto la foto si vede.
	stessa, altra := prontaDell092(t, p, "", "Fermaglio"), prontaDell092(t, p, "", "Ciondolo")
	fStessa, fAltra := p.allega(t, stessa, "stessa.png"), p.allega(t, altra, "altra.png")
	lottoPubblico092(t, p, "gettone-secondo-092", stessa)
	terzo := lottoPubblico092(t, p, "gettone-terzo-092", altra)
	eseguiSQL(t, p.d, `UPDATE lotti_avviso SET oreficeria_id = 3 WHERE id = ?`, terzo)
	eseguiSQL(t, p.d, `UPDATE buste SET oreficeria_id = 3 WHERE id = ?`, altra)
	ok := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_photo_viewed' AND esito = 'ok'`)
	controlla(src(stessa, fStessa.ID))
	controlla(src(altra, fAltra.ID))
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_photo_viewed' AND esito = 'ok'`); n != ok {
		t.Errorf("righe ok per le buste di un altro lotto: %d, prima %d", n, ok)
	}
	if m := pub.vai("GET", "/lotto/gettone-terzo-092/buste/"+itoa(altra)+"/foto/"+itoa(fAltra.ID), nil, nil); m.codice != 200 {
		t.Errorf("la foto col gettone del suo lotto: %d", m.codice)
	}
	// Una busta passata a un'altra oreficeria esce dalla pagina e dalle sue foto.
	eseguiSQL(t, p.d, `UPDATE buste SET oreficeria_id = 3 WHERE id = ?`, due)
	controlla(src(due, diDue.ID))
	nonContiene(t, "spostata", pub.vai("GET", "/lotto/"+gettoneFlask092, nil, nil).corpo, "Collana", "public-lot-bag-"+itoa(due))
}
