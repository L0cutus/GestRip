package web

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// La galleria della home (galleria.go), come i casi di «the gallery view (slice 202)» di
// workshop_live_test.exs dello standalone a go-baseline, su account, buste, archivio e miniature
// veri (facciaConFoto: la busta p.busta ha due foto, il PNG di un pixel in posizione 1 e una foto
// grande in posizione 2).

// fotoSu scrive una riga di foto_buste sulla busta con la chiave della foto grande, già
// nell'archivio, alla posizione data. Dà l'id.
func (p *provaFotoWeb) fotoSu(t *testing.T, busta, posizione int64) int64 {
	t.Helper()
	r, err := p.d.Scrittura.Exec(`INSERT INTO foto_buste (busta_id, posizione, chiave, nome_file, tipo_mime, byte, sha256, foto_busta,
		visibile_cliente, visibile_artigiano, visibile_oreficeria, creato_il, aggiornato_il)
		VALUES (?, ?, ?, 'g.png', 'image/png', ?, ?, 0, 0, 0, 0, '2026-10-31T09:00:00.000000Z', '2026-10-31T09:00:00.000000Z')`,
		busta, posizione, p.grande.Chiave, p.grande.Byte, mustImpronta(t, p.grande.Chiave))
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// righeGalleria sono le righe repair.staff_gallery_viewed: attore, esito, risorsa, id e dati.
func righeGalleria(t *testing.T, p *provaFotoWeb) []string {
	t.Helper()
	rr, err := p.d.Lettura.Query(`SELECT attore, esito, risorsa, id_risorsa, dati FROM eventi_audit
		WHERE azione = 'repair.staff_gallery_viewed' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rr.Close() }()
	var out []string
	for rr.Next() {
		var a, e, ri, id, dati string
		if err := rr.Scan(&a, &e, &ri, &id, &dati); err != nil {
			t.Fatal(err)
		}
		out = append(out, a+"|"+e+"|"+ri+"|"+id+"|"+dati)
	}
	return out
}

var reSrcGalleria = regexp.MustCompile(`<div id="gallery-([0-9]+)"[^>]*>\n<a href="([^"]*)" class="gs-galleria-foto" title="Apri foto"><img src="([^"]*)" data-retry alt="([^"]*)" loading="lazy" decoding="async">`)

// vociGalleria sono le celle della galleria nell'ordine della pagina: busta, link della foto,
// miniatura (decodificata dall'HTML) e testo alternativo.
func vociGalleria(corpo string) [][4]string {
	var out [][4]string
	for _, m := range reSrcGalleria.FindAllStringSubmatch(corpo, -1) {
		out = append(out, [4]string{m[1], html.UnescapeString(m[2]), html.UnescapeString(m[3]), html.UnescapeString(m[4])})
	}
	return out
}

// inventario: RIP-H-13
// La galleria: solo le buste della pagina con foto, la prima foto (posizione, poi id) come link al
// visualizzatore della scheda, «+N» per le altre, i dati come link alla scheda, «N schede con foto ·
// M senza foto nascoste», la linguetta della galleria scelta; la lista non c'è. Per titolare e
// dipendente.
func TestGalleriaHome(t *testing.T) {
	p := facciaConFoto(t)
	una := bustaHome(t, p.d, p.o, "40/26", "RIP-2026-0045", `oreficeria_id = NULL`)
	sola := p.fotoSu(t, una, 1)
	nuda := bustaHome(t, p.d, p.o, "41/26", "", "")
	for _, b := range []*browser{titolareDentro(t, p.h), dipendenteDentro(t, p.h)} {
		corpo := b.vai("GET", "/?vista=galleria", nil, nil).corpo
		contiene(t, "galleria", corpo, `<div id="repair-gallery" class="gs-galleria gallery-M">`,
			`<p id="gallery-summary" class="gs-galleria-riassunto">2 schede con foto<span> · 1 senza foto nascoste</span></p>`,
			`<a id="view-gallery" href="/?vista=galleria" aria-current="page" title="Vista galleria" class="gs-vista gs-vista-scelta">🖼 Galleria</a>`,
			`<a id="view-list" href="/" title="Vista lista" class="gs-vista">☰ Lista</a>`,
			`<span class="gallery-count">+1</span>`,
			`<a href="/repairs/`+idTesto(una)+`" class="gs-galleria-dati">
<div class="gs-galleria-riga"><span class="gs-galleria-numero">#0045/26</span>
<span class="gs-galleria-data">05/09/2026</span></div>
<p class="gs-galleria-cliente">Cliente 40/26</p>`)
		nonContiene(t, "galleria", corpo, `id="repairs"`, `id="gallery-`+idTesto(nuda)+`"`, `id="repair-gallery-empty"`, "rep-card")
		v := vociGalleria(corpo)
		// L'ordine della lista: la 41/26 (senza foto), la 40/26, poi la busta delle due foto.
		if len(v) != 2 || v[0][0] != idTesto(una) || v[1][0] != idTesto(p.busta) {
			t.Fatalf("voci: %v", v)
		}
		for i, x := range []struct {
			busta, foto int64
			alt         string
		}{{una, sola, "Foto 0045/26"}, {p.busta, p.foto.ID, "Foto N. 1/26"}} {
			if v[i][1] != "/repairs/"+idTesto(x.busta)+"#repair-photo-viewer-"+idTesto(x.foto) ||
				!strings.HasPrefix(v[i][2], "/repairs/"+idTesto(x.busta)+"/photos/"+idTesto(x.foto)+"?size=thumb&g=") || v[i][3] != x.alt {
				t.Errorf("voce %d: %v", i, v[i])
			}
		}
		if n := strings.Count(corpo, `class="gallery-count"`); n != 1 {
			t.Errorf("«+N»: %d", n)
		}
	}
}

// inventario: RIP-H-13, RIP-S-33
// Ogni visita della galleria scrive una riga repair.staff_gallery_viewed con le foto mostrate,
// nell'ordine della pagina, e ogni miniatura ha il gettone di quell'utente, busta e foto, firmato
// all'inizio del giorno: la rotta delle foto la serve senza una riga sua. La lista non scrive righe
// della galleria; una pagina senza foto nemmeno.
func TestGalleriaRigaEGettone(t *testing.T) {
	p := facciaConFoto(t)
	una := bustaHome(t, p.d, p.o, "40/26", "", "")
	sola := p.fotoSu(t, una, 1)
	b := titolareDentro(t, p.h)
	b.vai("GET", "/", nil, nil)
	if r := righeGalleria(t, p); len(r) != 0 {
		t.Fatalf("la lista ha scritto %v", r)
	}
	corpo := b.vai("GET", "/?vista=galleria", nil, nil).corpo
	voluta := fmt.Sprintf(`titolare@esempio.it|ok|repair|gallery|{"photo_ids":[%d,%d]}`, sola, p.foto.ID)
	if r := righeGalleria(t, p); len(r) != 1 || r[0] != voluta {
		t.Fatalf("righe: %v, voluta %s", r, voluta)
	}
	g := gettoneGalleria{chiave: dipendenzeProva(nil, nil, nil).ChiaveGalleria}
	io := p.idUtente(t, "titolare@esempio.it")
	v := vociGalleria(corpo)
	for i, x := range [][2]int64{{una, sola}, {p.busta, p.foto.ID}} {
		u, err := url.Parse(v[i][2])
		if err != nil {
			t.Fatal(err)
		}
		tok := u.Query().Get("g")
		if !g.valido(tok, io, x[0], x[1], p.o.Adesso()) || tok != g.firma(io, x[0], x[1], inizioGiorno(p.o.Adesso())) {
			t.Errorf("gettone %d non valido: %s", i, tok)
		}
		if g.valido(tok, io, x[0], p.grande.ID, p.o.Adesso()) || g.valido(tok, io+1, x[0], x[1], p.o.Adesso()) {
			t.Errorf("gettone %d valido per un'altra foto o un altro utente", i)
		}
		prima := p.visti(t)
		if r := b.vai("GET", v[i][2], nil, nil); r.codice != 200 || !unaVolta(r, "X-Photo-Variant", "thumb") {
			t.Errorf("miniatura %d: %d %v", i, r.codice, r.h)
		}
		if p.visti(t) != prima {
			t.Errorf("miniatura %d: una riga sua", i)
		}
	}
	// Un'altra visita, un'altra riga (una per visita, come repair.index_viewed); la stessa miniatura.
	if corpo2 := b.vai("GET", "/?vista=galleria", nil, nil).corpo; vociGalleria(corpo2)[0][2] != v[0][2] {
		t.Error("il gettone cambia nello stesso giorno")
	}
	if r := righeGalleria(t, p); len(r) != 2 {
		t.Errorf("righe dopo due visite: %d", len(r))
	}
	// Una pagina senza foto non scrive niente.
	b.vai("GET", "/?vista=galleria&stato=pronta", nil, nil)
	if r := righeGalleria(t, p); len(r) != 2 {
		t.Errorf("righe dopo una galleria vuota: %d", len(r))
	}
}

// inventario: RIP-H-13
// Senza la riga della galleria (l'audit rifiuta) la pagina si mostra lo stesso e le miniature non
// hanno il gettone: ognuna scriverà la sua riga.
func TestGalleriaSenzaRigaSenzaGettone(t *testing.T) {
	p := facciaConFoto(t)
	eseguiSQL(t, p.d, `CREATE TRIGGER galleria_rifiutata BEFORE INSERT ON eventi_audit
		WHEN NEW.azione = 'repair.staff_gallery_viewed' BEGIN SELECT RAISE(ABORT, 'audit sintetico rifiutato'); END`)
	b := titolareDentro(t, p.h)
	r := b.vai("GET", "/?vista=galleria", nil, nil)
	v := vociGalleria(r.corpo)
	if r.codice != 200 || len(v) != 1 || v[0][2] != percorsoFoto(p.busta, p.foto.ID)+"?size=thumb" {
		t.Fatalf("%d %v", r.codice, v)
	}
	if n := len(righeGalleria(t, p)); n != 0 {
		t.Errorf("righe: %d", n)
	}
	prima := p.visti(t)
	b.vai("GET", v[0][2], nil, nil)
	if p.visti(t) != prima+1 {
		t.Error("la miniatura senza gettone non ha scritto la sua riga")
	}
}

// inventario: RIP-H-13, RIP-S-33
// Review della 057: una busta nel cestino non è nella galleria né nella sua riga, e il gettone che
// la sua miniatura aveva prima non la apre più (404, nessun byte); un gettone non vale per la foto
// di un'altra busta, nemmeno cambiando solo il percorso.
func TestGalleriaCestinoEGettoneAltrove(t *testing.T) {
	p := facciaConFoto(t)
	una := bustaHome(t, p.d, p.o, "40/26", "", "")
	sola := p.fotoSu(t, una, 1)
	b := titolareDentro(t, p.h)
	v := vociGalleria(b.vai("GET", "/?vista=galleria", nil, nil).corpo)
	if len(v) != 2 || v[0][0] != idTesto(una) {
		t.Fatalf("voci: %v", v)
	}
	vecchio := v[0][2]
	tok := strings.SplitN(vecchio, "&g=", 2)[1]
	// Lo stesso gettone sulla foto dell'altra busta: non coperta, quindi una riga sua.
	prima := p.visti(t)
	if r := b.vai("GET", percorsoFoto(p.busta, p.foto.ID)+"?size=thumb&g="+tok, nil, nil); r.codice != 200 || p.visti(t) != prima+1 {
		t.Errorf("gettone di un'altra busta: %d, righe %d → %d", r.codice, prima, p.visti(t))
	}
	eseguiSQL(t, p.d, `UPDATE buste SET cancellata_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, una)
	corpo := b.vai("GET", "/?vista=galleria", nil, nil).corpo
	nonContiene(t, "cestino", corpo, `id="gallery-`+idTesto(una)+`"`)
	r := righeGalleria(t, p)
	if len(r) != 2 || !strings.HasSuffix(r[0], fmt.Sprintf(`{"photo_ids":[%d,%d]}`, sola, p.foto.ID)) ||
		!strings.HasSuffix(r[1], fmt.Sprintf(`{"photo_ids":[%d]}`, p.foto.ID)) {
		t.Errorf("righe: %v", r)
	}
	prima = p.visti(t)
	if r := b.vai("GET", vecchio, nil, nil); r.codice != 404 || r.corpo != msgFotoNonDisponibile {
		t.Errorf("miniatura nel cestino con il gettone: %d %q", r.codice, r.corpo)
	}
	if p.visti(t) != prima+1 {
		t.Error("il rifiuto della miniatura nel cestino non ha scritto la sua riga")
	}
}

// inventario: RIP-H-13
// Senza foto nella pagina: «Nessuna scheda con foto trovata.» con «N senza foto escluse dalla
// galleria.», senza il riassunto; senza buste, il solo messaggio.
func TestGalleriaVuota(t *testing.T) {
	p := facciaConFoto(t)
	b := titolareDentro(t, p.h)
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'pronta' WHERE id = ?`, p.busta)
	bustaHome(t, p.d, p.o, "40/26", "", "")
	bustaHome(t, p.d, p.o, "41/26", "", "")
	corpo := b.vai("GET", "/?vista=galleria&stato=in_lavorazione", nil, nil).corpo
	contiene(t, "senza foto", corpo, `<div id="repair-gallery" class="gs-galleria gallery-M">
<div id="repair-gallery-empty" class="gs-galleria-vuota">
Nessuna scheda con foto trovata.
<small>2 senza foto escluse dalla galleria.</small>
</div>
</div>`)
	nonContiene(t, "senza foto", corpo, "gallery-summary", `id="repairs"`)
	corpo = b.vai("GET", "/?vista=galleria&stato=preventivo", nil, nil).corpo
	contiene(t, "senza buste", corpo, `<div id="repair-gallery-empty" class="gs-galleria-vuota">
Nessuna scheda con foto trovata.
</div>`)
	nonContiene(t, "senza buste", corpo, "<small>", "gallery-summary")
	// Una busta con foto sola: «1 scheda con foto», senza le nascoste.
	contiene(t, "una", b.vai("GET", "/?vista=galleria&stato=pronta", nil, nil).corpo,
		`<p id="gallery-summary" class="gs-galleria-riassunto">1 scheda con foto</p>`)
}

// inventario: RIP-H-13
// La dimensione delle foto viene da galleria_dim: S e L com'è, ogni altro valore (o nessuno) M.
func TestGalleriaDimensione(t *testing.T) {
	p := facciaConFoto(t)
	b := titolareDentro(t, p.h)
	for v, classe := range map[string]string{"S": "gallery-S", "L": "gallery-L", "M": "gallery-M", "s": "gallery-M", " S": "gallery-M", "XL": "gallery-M", "": "gallery-M"} {
		eseguiSQL(t, p.d, `INSERT OR REPLACE INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('galleria_dim', ?, '2026-10-31T09:00:00.000000Z')`, v)
		contiene(t, v, b.vai("GET", "/?vista=galleria", nil, nil).corpo, `<div id="repair-gallery" class="gs-galleria `+classe+`">`)
	}
}

// inventario: RIP-H-13
// vista=galleria resta, come filter_query/3: nelle linguette (gli stessi filtri, senza la pagina),
// nella paginazione, in «⧉ Schede PDF», nei campi nascosti del campo unico e del cassetto, in
// #list-return, nel ritorno delle azioni del riquadro e della ricerca scritta, anche con un numero
// d'etichetta; non nei riquadri dei contatori e in «× Azzera filtro» (lo standalone li ha fissi).
// Solo il testo esatto «galleria» la sceglie.
func TestGalleriaVistaConservata(t *testing.T) {
	p := facciaConFoto(t)
	for i := 1; i <= 31; i++ {
		bustaHome(t, p.d, p.o, fmt.Sprintf("%d/25", i), "", `stato = 'pronta'`)
	}
	b := titolareDentro(t, p.h)
	corpo := b.vai("GET", "/?stato=pronta&vista=galleria&p=2", nil, nil).corpo
	contiene(t, "pagina 2", corpo,
		`<a id="view-list" href="/?stato=pronta" title="Vista lista" class="gs-vista">☰ Lista</a>`,
		`<a id="view-gallery" href="/?stato=pronta&amp;vista=galleria" aria-current="page"`,
		`<a href="/?stato=pronta&amp;vista=galleria&amp;p=1" class="pulsante-contorno">← Precedente</a>`,
		`<a id="repair-report-pdf" href="/repairs/report.pdf?stato=pronta&amp;vista=galleria" target="_blank"`,
		`<input type="hidden" name="stato" value="pronta">
<input type="hidden" name="vista" value="galleria">
<div class="gs-ricerca-riga">`,
		`<input type="hidden" name="vista" value="galleria">
<label class="gs-largo">`,
		`<div id="list-return" data-path="/?p=2&amp;stato=pronta&amp;vista=galleria" hidden></div>`,
		`<a id="clear-repair-filters" href="/" class="filter-pill pill-grigia">× Azzera filtro</a>`,
		`<a id="dashboard-pronta" href="/?stato=pronta" `)
	contiene(t, "pagina 1", b.vai("GET", "/?stato=pronta&vista=galleria", nil, nil).corpo,
		`<a href="/?stato=pronta&amp;vista=galleria&amp;p=2" class="pulsante-contorno">Successiva →</a>`,
		`<div id="list-return" data-path="/?stato=pronta&amp;vista=galleria" hidden></div>`)
	// La lista senza filtri: le linguette sono «/» e «/?vista=galleria».
	contiene(t, "lista", b.vai("GET", "/", nil, nil).corpo,
		`<a id="view-list" href="/" aria-current="page" title="Vista lista" class="gs-vista gs-vista-scelta">☰ Lista</a>`,
		`<a id="view-gallery" href="/?vista=galleria" title="Vista galleria" class="gs-vista">🖼 Galleria</a>`)
	// Solo «galleria» esatto: ogni altro valore è la lista, e nei link non c'è.
	for _, q := range []string{"vista=Galleria", "vista=galleria%20", "vista[]=galleria", "vista=lista", "vista=", "vist=galleria"} {
		corpo := b.vai("GET", "/?"+q, nil, nil).corpo
		contiene(t, q, corpo, `id="repairs"`, `<a id="view-list" href="/" aria-current="page"`)
		nonContiene(t, q, corpo, `id="repair-gallery"`, `name="vista"`)
	}
	// Le azioni del riquadro tornano alla galleria; la ricerca scritta resta nella galleria, anche
	// quella per numero d'etichetta, che toglie i filtri ma non la vista.
	b.vai("GET", "/", nil, nil)
	if r := b.vai("POST", "/repairs/"+idTesto(p.busta)+"/pin", formLista(b, "vista=galleria&stato=in_lavorazione&p=1"), nil); r.location != "/?stato=in_lavorazione&vista=galleria" {
		t.Errorf("pin: %q", r.location)
	}
	for q, voluto := range map[string]string{
		"/cerca?cerca=Cliente&stato=pronta&vista=galleria": "/?cerca=Cliente&stato=pronta&vista=galleria",
		"/cerca?stato=pronta&vista=galleria":               "/?stato=pronta&vista=galleria",
		"/cerca?cerca=0099&stato=pronta&vista=galleria":    "/?cerca=0099&vista=galleria",
	} {
		if r := b.vai("GET", q, nil, nil); r.codice != 302 || r.location != voluto {
			t.Errorf("%s: %d %q, voluto %q", q, r.codice, r.location, voluto)
		}
	}
	// Dal POST del campo unico la lista disegnata nella risposta è la galleria.
	b.vai("GET", "/", nil, nil)
	r := b.vai("POST", "/cerca-qr", url.Values{"_csrf_token": {b.token}, "cerca": {"Cliente"}, "vista": {"galleria"}}, nil)
	contiene(t, "cerca-qr", r.corpo, `id="repair-gallery"`, `<div id="list-return" data-path="/?cerca=Cliente&amp;vista=galleria" hidden></div>`)
}

// I conteggi delle schede del riassunto (sched{a|e}).
func TestSchedeDellaGalleria(t *testing.T) {
	for n, voluto := range map[int]string{1: "1 scheda", 2: "2 schede", 30: "30 schede"} {
		if got := (datiGalleria{Voci: make([]voceGalleria, n)}).Schede(); got != voluto {
			t.Errorf("%d: %q", n, got)
		}
	}
	for dim, voluto := range map[string]string{"S": "gallery-S", "M": "gallery-M", "L": "gallery-L", "": "gallery-M", "XL": "gallery-M"} {
		if got := classeGalleria(dim); got != voluto {
			t.Errorf("%q: %q", dim, got)
		}
	}
}

// inventario: RIP-H-13
// app.js: una miniatura con data-retry che non arriva si chiede una seconda volta, una sola; il
// pannello del QR va sopra la lista e sopra la galleria. Il comportamento lo prova il banco
// (home-galleria).
func TestScriptDellaGalleria(t *testing.T) {
	js := string(leggiStatico("app.js"))
	contiene(t, "app.js", js, `if (!img || img.tagName !== "IMG" || !img.hasAttribute("data-retry") || img.hasAttribute("data-retried")) return`,
		`img.setAttribute("data-retried", "")`, `img.src = src + (src.includes("?") ? "&" : "?") + "retry=1"`, `}, true)`,
		`const nellaLista = () => document.querySelector("#repairs, #repair-gallery") !== null`)
	css := string(leggiStatico("app.css"))
	contiene(t, "app.css", css, `.gs-galleria.gallery-S { grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); }`,
		`.gs-galleria.gallery-M { grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); }`,
		`.gs-galleria.gallery-L { grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); }`)
}
