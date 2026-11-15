package web

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/firmeflask"
	"github.com/L0cutus/gestrip/internal/firmephoenix"
)

// Le pagine pubbliche dell'artigiano (ART-24…26) e il link sulla scheda (RIP-S-25), come
// public_artisan_controller_test.exs e repair_live_test.exs dello standalone (go-baseline). Chiavi e
// segreti inventati.

const segretoFlaskArtigiano = "segreto-flask-SINTETICO-di-prova"

var standaloneArtigiano = firmephoenix.Nuovo(strings.Repeat("SINTETICO-secret-key-base-", 3))

// facceArtigiano sono le facce di facceAnteprima con i link dell'artigiano e il segreto Flask; la
// busta 0045/26 con l'artigiano, la foto della busta (mai all'artigiano) e una seconda foto.
func facceArtigiano(t *testing.T) (*provaAnteprima, buste.Creata, buste.Foto, buste.Foto) {
	t.Helper()
	p := facceAnteprima(t, indirizzoPubblicoProva)
	p.b.ConLinkArtigiano([]byte("sottochiave-artigiano-SINTETICA-di-prova-32b"), standaloneArtigiano).
		ConSegretoFlask(firmeflask.Nuovo(segretoFlaskArtigiano)).ConIndirizzoPubblico(indirizzoPubblicoProva)
	eseguiSQL(t, p.d, `INSERT INTO artigiani (id, nome, telefono) VALUES (1, 'Laboratorio Esempio', '333 111 2222'), (2, 'Senza Numero', NULL)`)
	c := p.busta(t, nil)
	eseguiSQL(t, p.d, `UPDATE buste SET artigiano_id = 1 WHERE id = ?`, c.ID)
	busta := p.allega(t, c.ID, "busta.png")
	pezzo := p.allega(t, c.ID, "pezzo.png")
	return p, c, busta, pezzo
}

func linkArtigianoDi(corpo string) string {
	i := strings.Index(corpo, `id="repair-artisan-link-url" href="`)
	if i < 0 {
		return ""
	}
	s := corpo[i+len(`id="repair-artisan-link-url" href="`):]
	return html.UnescapeString(s[:strings.Index(s, `"`)])
}

// inventario: ART-24, ART-25, RIP-S-25
// Il link della scheda apre la pagina pubblica: numero, stato, lavorazione e solo la foto per
// l'artigiano (miniatura e foto intera con lo stesso gettone), no-store, no-referrer e
// noindex; niente del cliente. La foto della busta non si serve.
func TestPaginaArtigianoDalLinkDellaScheda(t *testing.T) {
	p, c, fotoBusta, pezzo := facceArtigiano(t)
	r := titolareDentro(t, p.interna).vai("GET", percorsoScheda(c.ID), nil, nil)
	contiene(t, "scheda", r.corpo, `<section id="repair-artisan-link"`, "🔧 Link per l'artigiano",
		"Busta, lavorazione, stato e le foto autorizzate all'artigiano. Nessun dato del cliente. Vale novanta giorni.")
	nonContiene(t, "scheda", r.corpo, "repair-artisan-send-missing")
	link := linkArtigianoDi(r.corpo)
	pref := indirizzoPubblicoProva + "/artigiano/" + strconv.FormatInt(c.ID, 10) + "?t="
	if !strings.HasPrefix(link, pref) {
		t.Fatalf("link: %q", link)
	}
	tok := strings.TrimPrefix(link, pref)
	pub := nuovoBrowser(t, p.pubblica)
	a := pub.vai("GET", strings.TrimPrefix(link, indirizzoPubblicoProva), nil, nil)
	if a.codice != 200 || a.h.Get("Cache-Control") != "no-store" || a.h.Get("Referrer-Policy") != "no-referrer" ||
		a.h.Get("X-Robots-Tag") != "noindex, nofollow" {
		t.Fatalf("pagina: %d %v", a.codice, a.h)
	}
	foto := "/artigiano/" + strconv.FormatInt(c.ID, 10) + "/foto/" + strconv.FormatInt(pezzo.ID, 10) + "?t=" + url.QueryEscape(tok)
	contiene(t, "pagina", a.corpo, `id="artisan-bag-number"`, "0045/26", `id="artisan-status"`, "In lavorazione",
		`id="artisan-description"`, "Ridurre anello", "Foto (1)", `src="`+html.EscapeString(foto+"&size=thumb")+`"`,
		`src="`+html.EscapeString(foto)+`"`, "Pagina di sola consultazione riservata all'artigiano.")
	nonContiene(t, "pagina", a.corpo, "Ada Inventata", "333 987 6543", "Pietra da ripulire", "80,00", "Laboratorio Esempio",
		"/foto/"+strconv.FormatInt(fotoBusta.ID, 10), "artisan-photos-empty")
	m := pub.vai("GET", foto+"&size=thumb", nil, nil)
	if m.codice != 200 || m.h.Get("Content-Type") != "image/webp" || m.h.Get("Cache-Control") != "no-store" || m.h.Get("X-Robots-Tag") != "" {
		t.Errorf("miniatura: %d %v", m.codice, m.h)
	}
	if m := pub.vai("GET", foto, nil, nil); m.codice != 200 || m.h.Get("X-Photo-Variant") != "full" {
		t.Errorf("foto intera: %d %v", m.codice, m.h)
	}
	for nome, perc := range map[string]string{
		"foto della busta": strings.Replace(foto, "/foto/"+strconv.FormatInt(pezzo.ID, 10), "/foto/"+strconv.FormatInt(fotoBusta.ID, 10), 1),
		"gettone toccato":  foto + "x",
		"senza gettone":    "/artigiano/" + strconv.FormatInt(c.ID, 10) + "/foto/" + strconv.FormatInt(pezzo.ID, 10),
	} {
		if m := pub.vai("GET", perc, nil, nil); m.codice != 404 || m.corpo != "Foto non disponibile" {
			t.Errorf("%s: %d %q", nome, m.codice, m.corpo)
		}
	}
	// Sull'interna la pagina non c'è: è solo della faccia pubblica.
	if r := titolareDentro(t, p.interna).vai("GET", strings.TrimPrefix(link, indirizzoPubblicoProva), nil, nil); r.codice == 200 {
		t.Error("pagina dell'artigiano sulla faccia interna")
	}
}

// inventario: ART-24, ART-26
// Ogni rifiuto è la stessa risposta byte per byte (codice, corpo, intestazioni): gettone assente, non
// testo, toccato, scaduto, firmato dopo adesso, di un'altra busta, del link del cliente, di un
// altro sale, il vecchio link Flask sulla rotta nuova e viceversa, busta che non c'è o nel cestino,
// id che non è un numero. Il Phoenix.Token dello standalone (D-87) e il vecchio link Flask aprono.
func TestPaginaArtigianoRifiutiUguali(t *testing.T) {
	p, c, fotoBusta, pezzo := facceArtigiano(t)
	altra := p.busta(t, func(a *buste.Accettazione) { a.CodiceQR = "RIP-2026-0046" })
	cestino := p.busta(t, func(a *buste.Accettazione) { a.CodiceQR = "RIP-2026-0047" })
	eseguiSQL(t, p.d, `UPDATE buste SET cancellata_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, cestino.ID)
	id := strconv.FormatInt(c.ID, 10)
	adesso := p.o.Adesso()
	numero := ""
	if err := p.d.Lettura.QueryRow(`SELECT numero FROM buste WHERE id = ?`, c.ID).Scan(&numero); err != nil {
		t.Fatal(err)
	}
	flask := firmeflask.Nuovo(segretoFlaskArtigiano).FirmaArtigiano(numero, adesso.Unix()+60)
	phoenix := standaloneArtigiano.FirmaIntero(buste.SaleArtigiano, c.ID, adesso.Add(-time.Hour))
	pub := nuovoBrowser(t, p.pubblica)
	for nome, perc := range map[string]string{
		"phoenix dello standalone": "/artigiano/" + id + "?t=" + phoenix,
		"vecchio link flask":       "/artigiano/" + numero + "?t=" + flask,
	} {
		a := pub.vai("GET", perc, nil, nil)
		if a.codice != 200 || !strings.Contains(a.corpo, `id="artisan-description"`) || a.h.Get("X-Robots-Tag") != "noindex, nofollow" {
			t.Errorf("%s: %d", nome, a.codice)
		}
		if nome == "vecchio link flask" && !strings.Contains(a.corpo, html.EscapeString("/artigiano/"+numero+"/foto/"+url.PathEscape(pezzo.NomeFile)+"?t="+url.QueryEscape(flask)+"&size=thumb")) {
			t.Errorf("foto del vecchio link: %s", a.corpo)
		}
	}
	if m := pub.vai("GET", "/artigiano/"+numero+"/foto/"+url.PathEscape(pezzo.NomeFile)+"?t="+flask+"&size=thumb", nil, nil); m.codice != 200 || m.h.Get("Content-Type") != "image/webp" {
		t.Errorf("foto per nome del vecchio link: %d", m.codice)
	}
	if m := pub.vai("GET", "/artigiano/"+numero+"/foto/"+url.PathEscape(fotoBusta.NomeFile)+"?t="+flask, nil, nil); m.codice != 404 {
		t.Errorf("foto della busta per nome: %d", m.codice)
	}
	if m := pub.vai("GET", "/artigiano/"+id+"/foto/"+strconv.FormatInt(pezzo.ID, 10)+"?t="+phoenix, nil, nil); m.codice != 200 {
		t.Errorf("foto con il phoenix: %d", m.codice)
	}
	rifiuti := map[string]string{
		"senza gettone":     "/artigiano/" + id,
		"gettone vuoto":     "/artigiano/" + id + "?t=",
		"non testo":         "/artigiano/" + id + "?t[a]=" + phoenix,
		"toccato":           "/artigiano/" + id + "?t=" + phoenix[:len(phoenix)-2] + "AA",
		"scaduto":           "/artigiano/" + id + "?t=" + standaloneArtigiano.FirmaIntero(buste.SaleArtigiano, c.ID, adesso.Add(-buste.DurataLinkArtigiano-time.Second)),
		"futuro":            "/artigiano/" + id + "?t=" + standaloneArtigiano.FirmaIntero(buste.SaleArtigiano, c.ID, adesso.Add(time.Second)),
		"altra busta":       "/artigiano/" + strconv.FormatInt(altra.ID, 10) + "?t=" + phoenix,
		"link del cliente":  "/artigiano/" + id + "?t=" + c.Token,
		"altro sale":        "/artigiano/" + id + "?t=" + standaloneArtigiano.FirmaIntero("gallery thumb", c.ID, adesso),
		"flask sulla nuova": "/artigiano/" + id + "?t=" + flask,
		"phoenix sul flask": "/artigiano/" + numero + "?t=" + phoenix,
		"flask altro anno":  "/artigiano/" + strings.Replace(numero, "/26", "/25", 1) + "?t=" + flask,
		"nel cestino":       "/artigiano/" + strconv.FormatInt(cestino.ID, 10) + "?t=" + standaloneArtigiano.FirmaIntero(buste.SaleArtigiano, cestino.ID, adesso),
		"non c'è":           "/artigiano/999999?t=" + standaloneArtigiano.FirmaIntero(buste.SaleArtigiano, 999999, adesso),
		"id non numero":     "/artigiano/abc?t=" + phoenix,
	}
	var primo *risposta
	for nome, perc := range rifiuti {
		// Ogni rifiuto da un indirizzo suo, così il limite dei falliti non entra nel confronto.
		pub.ip = "2001:db8:" + strconv.Itoa(len(nome)) + ":" + strconv.Itoa(int(nome[0])) + "::1"
		r := pub.vai("GET", perc, nil, nil)
		r.h.Del("Date")
		r.h.Del("X-Request-Id") // uno per richiesta
		if r.codice != 404 || r.corpo != "Riparazione non disponibile" {
			t.Errorf("%s: %d %q", nome, r.codice, r.corpo)
		}
		if primo == nil {
			primo = &r
			continue
		}
		if len(r.h) != len(primo.h) {
			t.Errorf("%s: intestazioni %v invece di %v", nome, r.h, primo.h)
		}
		for k := range primo.h {
			if r.h.Get(k) != primo.h.Get(k) {
				t.Errorf("%s: %s %q invece di %q", nome, k, r.h.Get(k), primo.h.Get(k))
			}
		}
	}
}

// inventario: RIP-S-25
// Il riquadro della scheda: con un artigiano senza numero dice che il sollecito non può partire (non su
// una busta annullata, chiusa); non c'è senza artigiano, su una busta consegnata o senza indirizzo pubblico.
func TestSchedaLinkArtigiano(t *testing.T) {
	p, c, _, _ := facceArtigiano(t)
	b := titolareDentro(t, p.interna)
	eseguiSQL(t, p.d, `UPDATE buste SET artigiano_id = 2 WHERE id = ?`, c.ID)
	r := b.vai("GET", percorsoScheda(c.ID), nil, nil)
	contiene(t, "senza numero", r.corpo, `id="repair-artisan-link-url"`,
		`<p id="repair-artisan-send-missing" class="artigiano-link-nota">Nessun telefono per l'artigiano: il sollecito su WhatsApp non può partire.</p>`)
	// Una busta chiusa ma non consegnata (annullata) ha il link, non la riga del telefono.
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'annullata' WHERE id = ?`, c.ID)
	r = b.vai("GET", percorsoScheda(c.ID), nil, nil)
	contiene(t, "annullata", r.corpo, `id="repair-artisan-link-url"`)
	nonContiene(t, "annullata", r.corpo, "repair-artisan-send-missing")
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'consegnata', data_consegna = '2026-10-31' WHERE id = ?`, c.ID)
	nonContiene(t, "consegnata", b.vai("GET", percorsoScheda(c.ID), nil, nil).corpo, "repair-artisan-link")
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'in_lavorazione', data_consegna = NULL, artigiano_id = NULL WHERE id = ?`, c.ID)
	nonContiene(t, "senza artigiano", b.vai("GET", percorsoScheda(c.ID), nil, nil).corpo, "repair-artisan-link")
	eseguiSQL(t, p.d, `UPDATE buste SET artigiano_id = 1 WHERE id = ?`, c.ID)
	p.b.ConIndirizzoPubblico("")
	nonContiene(t, "senza indirizzo pubblico", b.vai("GET", percorsoScheda(c.ID), nil, nil).corpo, "repair-artisan-link")
}

// inventario: ART-24, CLI-21
// Il 404 di una rotta pubblica parte con le intestazioni prima del corpo (Flush), senza Content-Type:
// come send_resp/3 dello standalone. Senza Flush il Caddy davanti mette «text/plain» (banco s084p).
func TestRifiutoPubblicoFlush(t *testing.T) {
	for _, metodo := range []string{http.MethodGet, http.MethodHead} {
		w := &scriviDopoFlush{ResponseRecorder: httptest.NewRecorder()}
		RifiutoPubblico(w, httptest.NewRequest(metodo, "/artigiano/4", nil), rifiutoRiparazione, false)
		if w.Code != 404 || !w.Flushed || w.Header().Get("Content-Type") != "" {
			t.Errorf("%s: %d flush %v tipo %q", metodo, w.Code, w.Flushed, w.Header().Get("Content-Type"))
		}
		if metodo == http.MethodGet && (!w.flushPrimaDelCorpo || w.Body.String() != rifiutoRiparazione) {
			t.Errorf("corpo prima del flush o diverso: %q", w.Body.String())
		}
	}
}
