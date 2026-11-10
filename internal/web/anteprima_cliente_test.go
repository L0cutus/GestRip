package web

import (
	"context"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/media"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// L'anteprima del cliente dalla scheda (CLI-27, RIP-S-27), come repair_customer_preview_test.exs dello
// standalone (go-baseline): GET /repairs/{id}/anteprima-cliente sulla faccia interna, la pagina che il
// link della busta disegna sulla faccia pubblica, senza token e con i controlli spenti.

type provaAnteprima struct {
	interna, pubblica http.Handler
	d                 *db.DB
	b                 *buste.Buste
	o                 *clock.Finto
	dir               string
}

// facceAnteprima sono le due facce vere sullo stesso database, con account, archivio delle foto,
// profilo del negozio e l'indirizzo pubblico dato ("" = non configurato).
func facceAnteprima(t *testing.T, pubblico string) *provaAnteprima {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Apri(filepath.Join(dir, "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	inserisciConti(t, d, contiBase)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (1, 'Il negozio'), (2, 'Oreficeria Esempio')`)
	eseguiSQL(t, d, `INSERT INTO profilo_negozio (nome, condizioni_garanzia, creato_il, aggiornato_il) VALUES ('Bottega di Prova', '', ?, ?)`,
		istanteHome, istanteHome)
	archivio := media.Nuovo(media.CartellaDi(filepath.Join(dir, "gestrip.db")))
	b := buste.Nuove(d, o).ConFoto(archivio, normalizzaQui{}).
		ConMiniature(media.NuoveMiniature(archivio, media.MiniatureDi(filepath.Join(dir, "gestrip.db")), miniaturaQui{}))
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti, dd.Buste, dd.Audit = o, ratelimit.Nuovo(o), b, audit.Nuovo(d, o)
	dd.IndirizzoPubblico, dd.Configurazione = pubblico, configProva
	pub, err := Pubblica(dd)
	if err != nil {
		t.Fatal(err)
	}
	return &provaAnteprima{interna: mustInterna(t, dd), pubblica: pub, d: d, b: b, o: o, dir: dir}
}

func (p *provaAnteprima) busta(t *testing.T, f func(*buste.Accettazione)) buste.Creata {
	t.Helper()
	a := buste.Accettazione{NomeCliente: "Ada Inventata", TelefonoCliente: "333 987 6543", DataAccettazione: "2026-10-01",
		Descrizione: "Ridurre anello", NotePubbliche: "Pietra da ripulire", Preventivo: "80,00", CodiceQR: "RIP-2026-0045"}
	if f != nil {
		f(&a)
	}
	a.GemelleConfermate = buste.EtichettaScritta(a.CodiceQR, buste.AnnoDiAccettazione(a.DataAccettazione, p.o.Adesso()))
	c, err := p.b.Crea(context.Background(), a, audit.Contesto{Attore: "titolare@esempio.it"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// inPreventivo è quoted/2 del test dello standalone: la busta in «preventivo».
func (p *provaAnteprima) inPreventivo(t *testing.T, f func(*buste.Accettazione)) buste.Creata {
	t.Helper()
	c := p.busta(t, f)
	if _, err := p.b.CambiaStato(context.Background(), c.ID, "preventivo", audit.Contesto{Attore: "titolare@esempio.it"}); err != nil {
		t.Fatal(err)
	}
	return c
}

func (p *provaAnteprima) dice(t *testing.T, c buste.Creata, testo string) {
	t.Helper()
	if err := p.b.ScriviMessaggioCliente(context.Background(), c.Token, testo, audit.Contesto{Attore: "public"}); err != nil {
		t.Fatal(err)
	}
}

func (p *provaAnteprima) allega(t *testing.T, busta int64, nome string) buste.Foto {
	t.Helper()
	in := filepath.Join(p.dir, "in-"+nome)
	if err := os.WriteFile(in, []byte(pngPixel), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := p.b.AllegaFoto(context.Background(), busta, []buste.Caricamento{{Percorso: in, NomeOriginale: nome}}, nil,
		audit.Contesto{Attore: "titolare@esempio.it"})
	if err != nil || len(f) != 1 {
		t.Fatalf("AllegaFoto: %v", err)
	}
	return f[0]
}

func percorsoAnteprima(id int64) string {
	return "/repairs/" + strconv.FormatInt(id, 10) + "/anteprima-cliente"
}

var (
	reArticolo  = regexp.MustCompile(`(?s)<article\b.*?</article>`)
	reTag       = regexp.MustCompile(`(?s)<[^>]*>`)
	reTemplateH = regexp.MustCompile(`(?s)<template\b.*?</template>`)
)

// articolo è l'<article> della pagina (la scheda del cliente, senza la barra dell'anteprima).
func articolo(corpo string) string { return reArticolo.FindString(corpo) }

// testoArticolo è il testo dell'<article>, spazi compattati (text/1 del test dello standalone).
func testoArticolo(corpo string) string {
	a := reTemplateH.ReplaceAllString(articolo(corpo), " ")
	return strings.Join(strings.Fields(html.UnescapeString(reTag.ReplaceAllString(a, " "))), " ")
}

func contaAnteprime(t *testing.T, d *db.DB) int {
	t.Helper()
	return contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.customer_preview_viewed'`)
}

// inventario: CLI-27, RIP-S-27
// Senza accesso si va al login, senza pagina né riga; il dipendente la apre come il titolare, con
// private, no-store e no-referrer, e la riga repair.customer_preview_viewed porta chi guarda e la busta.
func TestAnteprimaClienteChiLaApre(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	c := p.inPreventivo(t, nil)
	r := nuovoBrowser(t, p.interna).vai("GET", percorsoAnteprima(c.ID), nil, nil)
	if !redirectA(r, "/login") || contaAnteprime(t, p.d) != 0 {
		t.Fatalf("senza accesso: %d %q, righe %d", r.codice, r.location, contaAnteprime(t, p.d))
	}
	for i, x := range []struct {
		b     *browser
		email string
	}{{dipendenteDentro(t, p.interna), "banco@esempio.it"}, {titolareDentro(t, p.interna), "titolare@esempio.it"}} {
		r := x.b.vai("GET", percorsoAnteprima(c.ID), nil, nil)
		if r.codice != http.StatusOK {
			t.Fatalf("%s: %d %q", x.email, r.codice, r.location)
		}
		contiene(t, x.email, r.corpo, "👁 Anteprima: così la vede il cliente. Nulla qui viene inviato.")
		if r.h.Get("Cache-Control") != "private, no-store" || r.h.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s: cache %q, referrer %q", x.email, r.h.Get("Cache-Control"), r.h.Get("Referrer-Policy"))
		}
		if n := contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.customer_preview_viewed' AND attore = ?
			AND risorsa = 'repair' AND id_risorsa = ? AND esito = 'ok' AND dati = '{"bag_number":"1/26"}'`, x.email, strconv.FormatInt(c.ID, 10)); n != 1 {
			t.Errorf("%s: righe %d", x.email, n)
		}
		if n := contaAnteprime(t, p.d); n != i+1 {
			t.Errorf("righe in tutto %d", n)
		}
	}
}

// inventario: CLI-27, RIP-S-27
// Busta che non c'è, id malformato o nel cestino: 404 «Riparazione non disponibile» senza tipo, e
// nessuna riga; una riga che non si scrive: 503 «Anteprima non disponibile». Una POST non c'è (404).
func TestAnteprimaClienteRifiuti(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	c := p.inPreventivo(t, nil)
	cestino := p.inPreventivo(t, nil)
	eseguiSQL(t, p.d, `UPDATE buste SET cancellata_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, cestino.ID)
	b := titolareDentro(t, p.interna)
	prima := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit`)
	for _, q := range []string{percorsoAnteprima(cestino.ID), "/repairs/999999/anteprima-cliente", "/repairs/abc/anteprima-cliente",
		"/repairs/0/anteprima-cliente"} {
		r := b.vai("GET", q, nil, nil)
		if r.codice != http.StatusNotFound || r.corpo != "Riparazione non disponibile" || r.h.Get("Content-Type") != "" {
			t.Errorf("%s: %d %q %q", q, r.codice, r.corpo, r.h.Get("Content-Type"))
		}
		if r.h.Get("Cache-Control") != "private, no-store" || r.h.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s: %v", q, r.h)
		}
	}
	r := b.vai("POST", percorsoAnteprima(c.ID), map[string][]string{"_csrf_token": {b.token}}, nil)
	if r.codice != http.StatusNotFound {
		t.Errorf("POST: %d", r.codice)
	}
	if dopo := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit`); dopo != prima {
		t.Errorf("righe d'audit: %d", dopo-prima)
	}
	// Il messaggio del personale lasciato nella sessione se ne va anche con il 404 (clear_flash/1 vale
	// per ogni risposta): dopo, la home non lo mostra.
	if r := b.vai("GET", "/repairs/999999", nil, nil); !redirectA(r, "/") {
		t.Fatalf("%d", r.codice)
	}
	if r := b.vai("GET", "/repairs/999999/anteprima-cliente", nil, nil); r.codice != http.StatusNotFound {
		t.Fatalf("%d", r.codice)
	}
	nonContiene(t, "home dopo il 404", b.vai("GET", "/", nil, nil).corpo, "Riparazione non trovata.")
	eseguiSQL(t, p.d, `CREATE TEMP TRIGGER no_anteprima BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.customer_preview_viewed'
		BEGIN SELECT RAISE(ABORT, 'no'); END`)
	r = b.vai("GET", percorsoAnteprima(c.ID), nil, nil)
	if r.codice != http.StatusServiceUnavailable || r.corpo != "Anteprima non disponibile" || strings.Contains(r.corpo, "Ridurre") {
		t.Errorf("riga non scritta: %d %q", r.codice, r.corpo)
	}
}

// inventario: CLI-27, RIP-S-27, CLI-28
// È la pagina che il link disegna, parola per parola (dentro <article>), sotto la barra con «←
// Indietro» verso la scheda: cliente, oreficeria (B2B, con la conversazione dell'oreficeria) e busta
// con il link revocato, scaduto o mai emesso. La pagina del cliente non ha la barra.
func TestAnteprimaClienteComeLaPagina(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	diretta := p.inPreventivo(t, nil)
	p.dice(t, diretta, "Quando è pronto?")
	b2b := p.inPreventivo(t, func(a *buste.Accettazione) { a.OreficeriaID = "2" })
	p.dice(t, b2b, "Per il nostro cliente")
	staff := titolareDentro(t, p.interna)
	cliente := nuovoBrowser(t, p.pubblica)
	for _, c := range []buste.Creata{diretta, b2b} {
		link := cliente.vai("GET", "/r/"+c.Token, nil, nil)
		a := staff.vai("GET", percorsoAnteprima(c.ID), nil, nil)
		if link.codice != 200 || a.codice != 200 {
			t.Fatalf("%d %d", link.codice, a.codice)
		}
		if testoArticolo(a.corpo) != testoArticolo(link.corpo) {
			t.Errorf("busta %d: testo diverso\n%s\n%s", c.ID, testoArticolo(a.corpo), testoArticolo(link.corpo))
		}
		contiene(t, "anteprima", testoArticolo(a.corpo), "Approva il preventivo", "Pietra da ripulire")
		contiene(t, "anteprima", a.corpo, `<nav id="customer-preview-banner" class="gs-back-bar gs-back-bar-preview" aria-label="Ritorno">`+
			`<a id="customer-preview-back" href="/repairs/`+strconv.FormatInt(c.ID, 10)+`" class="gs-back-btn">← Indietro</a>`)
		nonContiene(t, "pagina del link", link.corpo, "customer-preview-banner")
	}
	contiene(t, "diretta", testoArticolo(staff.vai("GET", percorsoAnteprima(diretta.ID), nil, nil).corpo), "Quando è pronto?")
	contiene(t, "B2B", testoArticolo(staff.vai("GET", percorsoAnteprima(b2b.ID), nil, nil).corpo), "Per il nostro cliente")
	vivo := testoArticolo(cliente.vai("GET", "/r/"+diretta.Token, nil, nil).corpo)
	for _, q := range []string{
		`UPDATE buste SET token_pubblico_revocato_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`,
		`UPDATE buste SET token_pubblico_revocato_il = NULL, token_pubblico_scade_il = '2026-10-01T00:00:00.000000Z' WHERE id = ?`,
		`UPDATE buste SET token_pubblico = NULL WHERE id = ?`,
	} {
		eseguiSQL(t, p.d, q, diretta.ID)
		if g := testoArticolo(staff.vai("GET", percorsoAnteprima(diretta.ID), nil, nil).corpo); g != vivo {
			t.Errorf("%s: testo diverso\n%s\n%s", q, g, vivo)
		}
	}
}

// inventario: CLI-27, RIP-S-27
// Non cambia niente: la riga della busta uguale, il messaggio resta da leggere, nessun token, nessuna
// riga in nessuna tabella tranne quella dell'anteprima.
func TestAnteprimaClienteNonCambiaNiente(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	c := p.inPreventivo(t, nil)
	p.dice(t, c, "Quando è pronto?")
	eseguiSQL(t, p.d, `UPDATE buste SET token_pubblico = NULL WHERE id = ?`, c.ID)
	staff := titolareDentro(t, p.interna)
	tabelle := func() string {
		var s strings.Builder
		rr, err := p.d.Lettura.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name != 'eventi_audit' ORDER BY name`)
		if err != nil {
			t.Fatal(err)
		}
		var nomi []string
		for rr.Next() {
			var n string
			_ = rr.Scan(&n)
			nomi = append(nomi, n)
		}
		_ = rr.Close()
		for _, n := range nomi {
			s.WriteString(n + ":" + strconv.Itoa(contaRighe(t, p.d, `SELECT count(*) FROM "`+n+`"`)) + " ")
		}
		var riga string
		if err := p.d.Lettura.QueryRow(`SELECT quote(messaggi_letti_il) || quote(ultimo_messaggio_il) || quote(token_pubblico) ||
			quote(hash_token_pubblico) || quote(aggiornato_il) || quote(stato) || quote(preventivo_accettato_letto_il) || quote(preventivo_rifiutato_letto_il) FROM buste WHERE id = ?`, c.ID).Scan(&riga); err != nil {
			t.Fatal(err)
		}
		return s.String() + riga
	}
	prima, audit0 := tabelle(), contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit`)
	r := staff.vai("GET", percorsoAnteprima(c.ID), nil, nil)
	contiene(t, "anteprima", r.corpo, "Quando è pronto?")
	if dopo := tabelle(); dopo != prima {
		t.Errorf("cambiato:\n%s\n%s", prima, dopo)
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit`); n != audit0+1 {
		t.Errorf("righe d'audit: %d", n-audit0)
	}
	if contaSQL(t, p.d, `SELECT count(*) FROM buste WHERE id = ? AND messaggi_letti_il IS NULL AND ultimo_messaggio_il IS NOT NULL`, c.ID) != 1 {
		t.Error("il messaggio risulta letto")
	}
}

var (
	reControllo       = regexp.MustCompile(`<(button|textarea|select)\b|<input\b[^>]*>`)
	reFieldsetSpento  = regexp.MustCompile(`(?s)<fieldset\b[^>]*\bdisabled\b[^>]*>.*?</fieldset>`)
	reFieldsetAcceso  = regexp.MustCompile(`<fieldset\b[^>]*>`)
	reModuloConAzione = regexp.MustCompile(`<form\b[^>]*\saction=`)
)

// controlli conta i controlli dell'<article> (button, textarea, select, input non nascosti), fuori
// dai <template>.
func controlli(s string) int {
	n := 0
	for _, m := range reControllo.FindAllString(reTemplateH.ReplaceAllString(s, ""), -1) {
		if !strings.Contains(m, `type="hidden"`) {
			n++
		}
	}
	return n
}

// inventario: CLI-27, RIP-S-27
// Ogni controllo è dentro un fieldset spento, nessun modulo ha un indirizzo, «✗ Non procedo» porta a
// «#», nessun indirizzo del link (niente /r/, niente token, niente data-poll-url): niente si può
// mandare. La pagina del cliente tiene i suoi controlli vivi.
func TestAnteprimaClienteControlliSpenti(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	c := p.inPreventivo(t, nil)
	p.dice(t, c, "Quando è pronto?")
	r := titolareDentro(t, p.interna).vai("GET", percorsoAnteprima(c.ID), nil, nil)
	a := articolo(r.corpo)
	tutti := controlli(a)
	spenti := 0
	for _, f := range reFieldsetSpento.FindAllString(a, -1) {
		spenti += controlli(f)
	}
	if tutti < 3 || spenti != tutti {
		t.Errorf("controlli %d, spenti %d", tutti, spenti)
	}
	nonContiene(t, "anteprima", r.corpo, "/r/", c.Token, "data-poll-url", "_csrf_token")
	if reModuloConAzione.MatchString(a) {
		t.Error("un modulo con un indirizzo")
	}
	contiene(t, "anteprima", a, `<a href="#" id="public-quote-refuse" class="cliente-preventivo-link">✗ Non procedo</a>`)
	link := nuovoBrowser(t, p.pubblica).vai("GET", "/r/"+c.Token, nil, nil)
	la := articolo(link.corpo)
	if reFieldsetSpento.MatchString(la) || !reFieldsetAcceso.MatchString(la) {
		t.Error("la pagina del cliente ha i controlli spenti")
	}
	contiene(t, "link", link.corpo, `data-poll-url="/r/`+c.Token+`/poll"`)
	if n := len(reModuloConAzione.FindAllString(la, -1)); n < 2 {
		t.Errorf("moduli della pagina del cliente: %d", n)
	}
}

// inventario: CLI-27, RIP-S-27
// Il messaggio del personale (un rifiuto della scheda lasciato nella sessione) non compare
// sull'anteprima e se ne va, come clear_flash/1; la pagina è pubblica (niente manifest, menu,
// tavolozze), con 🏠 e ← del personale (public_fabs/1) e l'informativa sull'indirizzo pubblico.
func TestAnteprimaClienteSenzaMessaggiDelPersonale(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	c := p.inPreventivo(t, nil)
	b := titolareDentro(t, p.interna)
	if r := b.vai("GET", "/repairs/999999", nil, nil); !redirectA(r, "/") {
		t.Fatalf("%d", r.codice)
	}
	r := b.vai("GET", percorsoAnteprima(c.ID), nil, nil)
	nonContiene(t, "anteprima", r.corpo, "Riparazione non trovata.", "public-flash-error", "flash-errore", `rel="manifest"`,
		"app-mobile-navigation", "theme-picker", "photo-queue")
	contiene(t, "anteprima", r.corpo, "<title>GestRip</title>",
		`<a href="/" id="fab-home" class="gs-fab gs-fab-right" title="Lista riparazioni" aria-label="Lista riparazioni">`,
		`<button type="button" id="fab-back" class="gs-fab gs-fab-left" onclick="history.back()" title="Indietro" aria-label="Indietro">`,
		`<a id="public-privacy-link" href="`+indirizzoPubblicoProva+`/privacy">Informativa privacy</a>`)
	r = b.vai("GET", "/", nil, nil)
	nonContiene(t, "home dopo l'anteprima", r.corpo, "Riparazione non trovata.")
	// Senza indirizzo pubblico l'informativa resta /privacy, come lo standalone.
	q := facceAnteprima(t, "")
	c2 := q.inPreventivo(t, nil)
	r = titolareDentro(t, q.interna).vai("GET", percorsoAnteprima(c2.ID), nil, nil)
	contiene(t, "senza indirizzo", r.corpo, `<a id="public-privacy-link" href="/privacy">Informativa privacy</a>`)
}

// inventario: CLI-27, RIP-S-27
// Le foto sono quelle che vede chi ha il link, servite dalla rotta del personale (che scrive la sua
// riga per ogni foto); una foto nascosta al cliente non c'è.
func TestAnteprimaClienteFoto(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	c := p.inPreventivo(t, nil)
	mostrata := p.allega(t, c.ID, "busta.png")
	nascosta := p.allega(t, c.ID, "oggetto.png")
	if _, err := p.b.ImpostaVisibilita(context.Background(), c.ID, nascosta.ID, string(buste.PubblicoCliente), false,
		audit.Contesto{Attore: "titolare@esempio.it"}); err != nil {
		t.Fatal(err)
	}
	b := titolareDentro(t, p.interna)
	r := b.vai("GET", percorsoAnteprima(c.ID), nil, nil)
	base := "/repairs/" + strconv.FormatInt(c.ID, 10) + "/photos/"
	contiene(t, "anteprima", r.corpo, `src="`+base+strconv.FormatInt(mostrata.ID, 10)+`?size=thumb"`,
		`src="`+base+strconv.FormatInt(mostrata.ID, 10)+`"`)
	nonContiene(t, "anteprima", r.corpo, base+strconv.FormatInt(nascosta.ID, 10))
	prima := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.staff_photo_viewed'`)
	f := b.vai("GET", base+strconv.FormatInt(mostrata.ID, 10)+"?size=thumb", nil, nil)
	if f.codice != 200 || f.h.Get("Content-Type") != "image/webp" {
		t.Errorf("miniatura: %d %q", f.codice, f.h.Get("Content-Type"))
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.staff_photo_viewed'`); n != prima+1 {
		t.Errorf("righe della foto: %d", n-prima)
	}
}

// inventario: RIP-S-27
// «👁 Anteprima cliente» nella scheda, ultimo delle azioni, con il link revocato anche; non c'è su una
// busta consegnata.
func TestAnteprimaClientePulsanteDellaScheda(t *testing.T) {
	p := facceAnteprima(t, indirizzoPubblicoProva)
	c := p.inPreventivo(t, nil)
	eseguiSQL(t, p.d, `UPDATE buste SET token_pubblico_revocato_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, c.ID)
	b := titolareDentro(t, p.interna)
	r := b.vai("GET", percorsoScheda(c.ID), nil, nil)
	contiene(t, "scheda", r.corpo, `<a id="repair-customer-preview" href="`+percorsoAnteprima(c.ID)+`" class="pulsante-contorno">👁 Anteprima cliente</a>`)
	consegnata := p.busta(t, nil)
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'consegnata', data_consegna = '2026-10-31' WHERE id = ?`, consegnata.ID)
	r = dipendenteDentro(t, p.interna).vai("GET", percorsoScheda(consegnata.ID), nil, nil)
	if r.codice != 200 {
		t.Fatalf("%d", r.codice)
	}
	nonContiene(t, "consegnata", r.corpo, "repair-customer-preview", "anteprima-cliente")
}
