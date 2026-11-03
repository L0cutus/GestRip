package web

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/imaging"
	"github.com/L0cutus/gestrip/internal/media"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// Le foto sulla scheda della busta (slice 038): la galleria con il visualizzatore senza
// JavaScript (RIP-S-28), il caricamento da «Fotocamera» e «Galleria» (RIP-S-29), la visibilità per
// canale (RIP-S-31) ed «Elimina foto» (RIP-S-32), con i casi di repair_photo_live_test.exs,
// repair_live_test.exs («a thumbnail opens the viewer…», «the staff viewer draws what the staff
// gallery draws…») e photo_viewer_test.exs dello standalone a go-baseline. La normalizzazione gira
// nel processo del test (in produzione nel figlio, imaging.Lavoratore).

// normalizzaCon è il normalizzatore dei test con un aggancio: prima gira prima della
// normalizzazione (per esempio per disattivare l'account a metà), err sostituisce il risultato.
type normalizzaCon struct {
	prima func()
	err   error
}

func (n normalizzaCon) Normalizza(_ context.Context, in []byte) ([]byte, error) {
	if n.prima != nil {
		n.prima()
	}
	if n.err != nil {
		return nil, n.err
	}
	return imaging.Normalizza(in)
}

// registroSicuro è un registro in memoria che si può leggere mentre il server scrive.
type registroSicuro struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (r *registroSicuro) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.b.Write(p)
}

func (r *registroSicuro) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.b.String()
}

type provaCaricamento struct {
	h           http.Handler
	d           *db.DB
	o           *clock.Finto
	b           *buste.Buste
	archivio    *media.Archivio
	dir         string
	caricamenti string
	busta       int64
	reg         *registroSicuro
}

// facciaCaricamento è la faccia interna con account, buste e archivio veri, la cartella dei
// caricamenti e una busta senza foto; n è il normalizzatore (nil: quello vero, nel processo).
func facciaCaricamento(t *testing.T, n buste.Normalizzatore) *provaCaricamento {
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
	if n == nil {
		n = normalizzaQui{}
	}
	p := &provaCaricamento{d: d, o: o, dir: dir, reg: &registroSicuro{}, caricamenti: filepath.Join(dir, "caricamenti")}
	p.archivio = media.Nuovo(media.CartellaDi(filepath.Join(dir, "gestrip.db")))
	p.b = buste.Nuove(d, o).ConFoto(p.archivio, n)
	l := slog.New(slog.NewTextHandler(p.reg, &slog.HandlerOptions{Level: slog.LevelDebug}))
	dd := dipendenzeProva(&sondaFinta{pronto: true}, l, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti, dd.Buste, dd.Caricamenti = o, ratelimit.Nuovo(o), p.b, p.caricamenti
	p.h = mustInterna(t, dd)
	p.busta = bustaScheda(t, d, o, nil)
	return p
}

func (p *provaCaricamento) foto(t *testing.T) []buste.Foto {
	t.Helper()
	f, err := p.b.ElencoFoto(context.Background(), p.busta)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// fileNellArchivio sono i file con il nome di una chiave nell'archivio delle foto.
func (p *provaCaricamento) fileNellArchivio(t *testing.T) int {
	t.Helper()
	voci, err := os.ReadDir(p.archivio.Cartella())
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, v := range voci {
		if media.ChiaveValida(v.Name()) {
			n++
		}
	}
	return n
}

// caricamentiVuoti dice se nella cartella dei caricamenti non è rimasto niente (la cartella può
// non esserci ancora).
func (p *provaCaricamento) caricamentiVuoti(t *testing.T) bool {
	t.Helper()
	var resti []string
	err := filepath.WalkDir(p.caricamenti, func(q string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if q != p.caricamenti {
			resti = append(resti, q)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(resti) > 0 {
		t.Logf("resti nei caricamenti: %v", resti)
	}
	return len(resti) == 0
}

func (p *provaCaricamento) caricate(t *testing.T) int {
	t.Helper()
	return contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.photo_uploaded'`)
}

// nienteScritto: nessuna riga di foto, nessuna riga d'audit del caricamento, nessun file
// nell'archivio, niente nei caricamenti.
func (p *provaCaricamento) nienteScritto(t *testing.T, cosa string) {
	t.Helper()
	if n := len(p.foto(t)); n != 0 {
		t.Errorf("%s: %d foto", cosa, n)
	}
	if n := p.caricate(t); n != 0 {
		t.Errorf("%s: %d righe repair.photo_uploaded", cosa, n)
	}
	if n := p.fileNellArchivio(t); n != 0 {
		t.Errorf("%s: %d file nell'archivio", cosa, n)
	}
	if !p.caricamentiVuoti(t) {
		t.Errorf("%s: resti nella cartella dei caricamenti", cosa)
	}
}

func percorsoCaricamento(busta int64) string {
	return "/repairs/" + strconv.FormatInt(busta, 10) + "/foto"
}

func percorsoVisibilita(busta, foto int64) string {
	return "/repairs/" + strconv.FormatInt(busta, 10) + "/foto/" + strconv.FormatInt(foto, 10) + "/visibilita"
}

func percorsoElimina(busta, foto int64) string {
	return "/repairs/" + strconv.FormatInt(busta, 10) + "/foto/" + strconv.FormatInt(foto, 10) + "/elimina"
}

// fileProva è un file del modulo: il campo (camera o gallery), il nome che dà il browser, il tipo
// che dichiara e i byte.
type fileProva struct {
	campo, nome, tipo string
	dati              []byte
}

func png(campo, nome string) fileProva {
	return fileProva{campo: campo, nome: nome, tipo: "image/png", dati: []byte(pngPixel)}
}

// corpoMultipart scrive il modulo come lo manda il browser: i campi di testa nell'ordine, poi i
// file. Dà il corpo e il Content-Type con il boundary.
func corpoMultipart(t *testing.T, testa [][2]string, file []fileProva, coda [][2]string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, c := range testa {
		if err := w.WriteField(c[0], c[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range file {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", `form-data; name="`+f.campo+`"; filename="`+f.nome+`"`)
		h.Set("Content-Type", f.tipo)
		part, err := w.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(f.dati); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range coda {
		if err := w.WriteField(c[0], c[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), w.FormDataContentType()
}

// testaDelModulo sono il token e il segno della pagina, come i campi nascosti del modulo.
func (b *browser) testaDelModulo() [][2]string {
	testa := [][2]string{{"_csrf_token", b.token}}
	if !b.senzaSegno && b.segno != "" {
		testa = append(testa, [2]string{"mostra", b.segno})
	}
	return testa
}

// carica manda i file al modulo della scheda come il browser, dopo il token e il segno della
// pagina.
func (b *browser) carica(percorso string, file ...fileProva) risposta {
	b.t.Helper()
	corpo, tipo := corpoMultipart(b.t, b.testaDelModulo(), file, nil)
	return b.inviaCorpo(percorso, tipo, bytes.NewReader(corpo), nil)
}

// inviaCorpo manda un POST con il corpo e il tipo dati e legge la risposta come vai.
func (b *browser) inviaCorpo(percorso, tipo string, corpo io.Reader, h map[string]string) risposta {
	b.t.Helper()
	req, _ := http.NewRequest("POST", b.srv.URL+percorso, corpo)
	req.Header.Set("Content-Type", tipo)
	for k, v := range h {
		req.Header.Set(k, v)
	}
	r, err := b.c.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	c, _ := io.ReadAll(r.Body) // un errore di lettura lascia il corpo a metà: i controlli lo vedono
	return risposta{r.StatusCode, r.Header.Get("Location"), string(c), r.Header}
}

// dopo segue il redirect e dà la pagina dove porta.
func (b *browser) dopo(r risposta) risposta {
	b.t.Helper()
	if r.codice != http.StatusFound {
		b.t.Fatalf("atteso un redirect: %d %q", r.codice, r.corpo)
	}
	return b.vai("GET", r.location, nil, nil)
}

// tornaAllaSchedaConSegno: un redirect alla scheda della busta con il segno della pagina.
func tornaAllaSchedaConSegno(r risposta, busta int64) bool {
	return r.codice == http.StatusFound && strings.HasPrefix(r.location, percorsoScheda(busta)+"?mostra=")
}

// inventario: RIP-S-28
// Una busta senza foto: «Foto (0)», «Nessuna foto caricata.», nessun visualizzatore; sotto, il
// contratto del caricamento e i due moduli «Fotocamera» (fotocamera posteriore) e «Galleria».
func TestGalleriaDellaSchedaVuota(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	for _, b := range []*browser{titolareDentro(t, h), dipendenteDentro(t, h)} {
		r := b.vai("GET", percorsoScheda(id), nil, nil)
		contiene(t, "vuota", r.corpo, `<section id="repair-photos"`, "<h2>Foto (0)</h2>", "Nessuna foto caricata.",
			"Massimo 2 foto per volta, JPEG/PNG/WebP, 25 MiB ciascuna. I metadati vengono rimossi.",
			`<form id="camera-upload" action="/repairs/`+strconv.FormatInt(id, 10)+`/foto" method="post" enctype="multipart/form-data"`,
			`<form id="gallery-upload" action="/repairs/`+strconv.FormatInt(id, 10)+`/foto" method="post" enctype="multipart/form-data"`,
			`id="camera-control"`, `id="gallery-control"`, `<div id="repair-photo-upload-errors" class="errori-foto">`)
		nonContiene(t, "vuota", r.corpo, "gs-viewer-frame", `id="repair-photo-viewer"`, "data-audience")
		camera := moduloConId(t, r.corpo, "camera-upload")
		galleria := moduloConId(t, r.corpo, "gallery-upload")
		contiene(t, "fotocamera", camera, "Fotocamera", `type="file"`, `name="camera"`, `accept=".jpg,.jpeg,.png,.webp"`, `capture="environment"`)
		contiene(t, "galleria", galleria, "Galleria", `type="file"`, `name="gallery"`, `accept=".jpg,.jpeg,.png,.webp"`, " multiple")
		nonContiene(t, "galleria", galleria, "capture")
		// Il token prima dei file: il server lo legge prima di ricevere un byte delle foto.
		for nome, m := range map[string]string{"fotocamera": camera, "galleria": galleria} {
			tok := strings.Index(m, `name="_csrf_token"`)
			segno := strings.Index(m, `name="mostra"`)
			file := strings.Index(m, `type="file"`)
			if tok < 0 || segno < 0 || tok > file || segno > file {
				t.Errorf("%s: token e segno non vengono prima del file: %q", nome, m)
			}
		}
	}
}

// moduloConId è il modulo con quell'id, dal tag di apertura alla chiusura.
func moduloConId(t *testing.T, corpo, id string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<form id="` + regexp.QuoteMeta(id) + `".*?</form>`).FindString(corpo)
	if m == "" {
		t.Fatalf("modulo %s non trovato", id)
	}
	return m
}

// cornice è il markup di un riquadro del visualizzatore: dal suo id al riquadro dopo
// (photo_viewer_test.exs, frame/2).
func cornice(corpo, id string) string {
	_, resto, ok := strings.Cut(corpo, `id="`+id+`"`)
	if !ok {
		return ""
	}
	pezzo, _, _ := strings.Cut(resto, `class="gs-viewer-frame"`)
	return pezzo
}

// cella è il riquadro della galleria di una foto.
func cella(corpo string, foto int64) string {
	return regexp.MustCompile(`(?s)<article id="repair-photo-` + strconv.FormatInt(foto, 10) + `".*?</article>`).FindString(corpo)
}

// inventario: RIP-S-28
// La galleria: «Foto (N)», una cella per foto nell'ordine di caricamento (posizione, poi id), la
// miniatura (size=thumb) nella cella e la foto intera solo nel visualizzatore; ogni cella apre il
// suo riquadro (#repair-photo-viewer-<id>), che ha il contatore, la foto precedente e la
// successiva (nessuna ai due capi) e «← Indietro» verso #repair-photos; nessun link alla foto
// nuda. Il personale vede tutte le foto, anche chiuse a ogni canale; titolare e dipendente uguali.
func TestGalleriaDellaSchedaConFoto(t *testing.T) {
	p := facciaConFoto(t)
	// Chiusa a ogni canale: per il personale resta nella galleria (la lista non è un pubblico).
	for _, pub := range []string{"customer", "artisan"} {
		if _, err := p.b.ImpostaVisibilita(context.Background(), p.busta, p.foto.ID, pub, false, audit.Contesto{Attore: "titolare@esempio.it"}); err != nil {
			t.Fatal(err)
		}
	}
	// Le due foto vanno alle posizioni 11 e 12; una riga nuova alla 5 viene prima di tutte anche con
	// l'id più alto.
	eseguiSQL(t, p.d, `UPDATE foto_buste SET posizione = posizione + 10 WHERE busta_id = ?`, p.busta)
	primaDiTutte := p.rigaFoto(t, []byte(pngPixel), "image/png", 5)
	ordine := []int64{primaDiTutte, p.foto.ID, p.grande.ID}
	busta := strconv.FormatInt(p.busta, 10)
	for _, b := range []*browser{titolareDentro(t, p.h), dipendenteDentro(t, p.h)} {
		for _, q := range []string{percorsoScheda(p.busta), percorsoScheda(p.busta) + "?modifica=1"} {
			r := b.vai("GET", q, nil, nil)
			contiene(t, q, r.corpo, "<h2>Foto (3)</h2>", `<div id="repair-photo-viewer">`)
			nonContiene(t, q, r.corpo, "Nessuna foto caricata.", `href="/repairs/`+busta+`/photos/`)
			celle := regexp.MustCompile(`<article id="repair-photo-(\d+)"`).FindAllStringSubmatch(r.corpo, -1)
			riquadri := regexp.MustCompile(`class="gs-viewer-frame" id="repair-photo-viewer-(\d+)"`).FindAllStringSubmatch(r.corpo, -1)
			if len(celle) != 3 || len(riquadri) != 3 {
				t.Fatalf("%s: %d celle, %d riquadri", q, len(celle), len(riquadri))
			}
			for i, id := range ordine {
				s := strconv.FormatInt(id, 10)
				if celle[i][1] != s || riquadri[i][1] != s {
					t.Errorf("%s: posto %d: cella %s, riquadro %s, attesa %s", q, i+1, celle[i][1], riquadri[i][1], s)
				}
				foto := "/repairs/" + busta + "/photos/" + s
				c := cella(r.corpo, id)
				contiene(t, "cella "+s, c, `<a href="#repair-photo-viewer-`+s+`"`, `title="Apri foto `+strconv.Itoa(i+1)+`"`,
					`<img src="`+foto+`?size=thumb" alt="Foto `+strconv.Itoa(i+1)+`" loading="lazy"`)
				f := cornice(r.corpo, "repair-photo-viewer-"+s)
				contiene(t, "riquadro "+s, f, `role="dialog"`, `aria-label="Foto `+strconv.Itoa(i+1)+` di 3"`,
					`<img src="`+foto+`" alt="Foto `+strconv.Itoa(i+1)+`" loading="lazy"`,
					`<span class="gs-viewer-counter">`+strconv.Itoa(i+1)+` / 3</span>`,
					`<a class="gs-viewer-btn" data-gs-nav="close" href="#repair-photos">← Indietro</a>`)
				nonContiene(t, "riquadro "+s, f, "size=thumb")
				if i > 0 {
					contiene(t, "riquadro "+s, f, `data-gs-nav="prev" href="#repair-photo-viewer-`+strconv.FormatInt(ordine[i-1], 10)+`" aria-label="Foto precedente"`)
				} else {
					nonContiene(t, "riquadro "+s, f, `data-gs-nav="prev"`)
					contiene(t, "riquadro "+s, f, `<span class="gs-viewer-btn gs-viewer-nav gs-viewer-off" aria-hidden="true">‹</span>`)
				}
				if i < 2 {
					contiene(t, "riquadro "+s, f, `data-gs-nav="next" href="#repair-photo-viewer-`+strconv.FormatInt(ordine[i+1], 10)+`" aria-label="Foto successiva"`)
				} else {
					nonContiene(t, "riquadro "+s, f, `data-gs-nav="next"`)
					contiene(t, "riquadro "+s, f, `<span class="gs-viewer-btn gs-viewer-nav gs-viewer-off" aria-hidden="true">›</span>`)
				}
			}
		}
	}
	// La pagina non legge i byte delle foto: nessuna riga repair.staff_photo_viewed per la scheda.
	if p.visti(t) != 0 {
		t.Errorf("la scheda ha letto le foto: %d righe", p.visti(t))
	}
}

// inventario: RIP-S-28
// Una foto sola: «1 / 1», né precedente né successiva.
func TestGalleriaConUnaFotoSola(t *testing.T) {
	p := facciaCaricamento(t, nil)
	in := filepath.Join(p.dir, "in.png")
	if err := os.WriteFile(in, []byte(pngPixel), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := p.b.AllegaFoto(context.Background(), p.busta, []buste.Caricamento{{Percorso: in, NomeOriginale: "a.png"}}, nil, audit.Contesto{Attore: "x"})
	if err != nil {
		t.Fatal(err)
	}
	r := titolareDentro(t, p.h).vai("GET", percorsoScheda(p.busta), nil, nil)
	c := cornice(r.corpo, "repair-photo-viewer-"+strconv.FormatInt(f[0].ID, 10))
	contiene(t, "sola", c, `<span class="gs-viewer-counter">1 / 1</span>`)
	nonContiene(t, "sola", c, `data-gs-nav="prev"`, `data-gs-nav="next"`)
}

// inventario: RIP-S-28
// Lo stile del visualizzatore è nel foglio (il riquadro si vede solo come :target, la pagina sotto
// non scorre) e app.js aggiunge le comodità di photo_viewer.js: i comandi della barra sostituiscono
// la voce della cronologia, frecce ed Esc, lo scorrimento di almeno 50 px più orizzontale che
// verticale; e «Foto caricata.» sparisce dopo 3 s. Senza script il visualizzatore funziona lo
// stesso (le ancore sopra).
func TestVisualizzatoreFoglioEScript(t *testing.T) {
	css, err := fileStatici.ReadFile("statici/app.css")
	if err != nil {
		t.Fatal(err)
	}
	contiene(t, "app.css", string(css), ".gs-viewer-frame{position:fixed;inset:0;z-index:60;display:none;background:#000;}",
		".gs-viewer-frame:target{display:block;}", "body:has(.gs-viewer-frame:target){overflow:hidden;}")
	js, err := fileStatici.ReadFile("statici/app.js")
	if err != nil {
		t.Fatal(err)
	}
	contiene(t, "app.js", string(js), `a[data-gs-nav]`, "window.location.replace(link.getAttribute(\"href\"))",
		`"ArrowLeft"`, `"ArrowRight"`, `"Escape"`, `".gs-viewer-frame:target"`, "touchstart", "touchend", "const SWIPE_PX = 50",
		`"Foto caricata."`, "3000", "data-invio-automatico", "requestSubmit", "new DataTransfer()", `"anteprima"`,
		"25 * 1024 * 1024", `"troppe"`, `"misura"`, `"formato"`, "files.slice(0, MAX_FOTO)")
}

// inventario: RIP-S-29
// «Galleria» con due foto: tornano alla scheda con il segno e «Foto caricata.», due righe nelle
// posizioni 1 e 2, la prima è la foto della busta, ognuna con repair.photo_uploaded; la galleria
// mostra «Foto (2)» con le miniature. Niente resta nella cartella dei caricamenti. Lo stesso per il
// dipendente, su un'altra busta.
func TestPaginaCaricamentoFotoDallaScheda(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := titolareDentro(t, p.h)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	r := b.carica(percorsoCaricamento(p.busta), png("gallery", "one.png"), png("gallery", "two.png"))
	if !tornaAllaSchedaConSegno(r, p.busta) {
		t.Fatalf("caricamento: %d %q", r.codice, r.location)
	}
	pagina := b.dopo(r)
	contiene(t, "dopo", pagina.corpo, `<div role="status" class="flash flash-info">Foto caricata.</div>`, "<h2>Foto (2)</h2>")
	nonContiene(t, "dopo", pagina.corpo, "flash-errore")
	f := p.foto(t)
	if len(f) != 2 || f[0].Posizione != 1 || f[1].Posizione != 2 || !f[0].FotoBusta || f[1].FotoBusta {
		t.Fatalf("foto: %+v", f)
	}
	if f[0].NomeFile != "one.jpg" || f[1].NomeFile != "two.jpg" {
		t.Errorf("nomi: %q %q", f[0].NomeFile, f[1].NomeFile)
	}
	for _, x := range f {
		contiene(t, "miniatura", pagina.corpo, `src="`+miniatura(percorsoFoto(p.busta, x.ID))+`"`)
	}
	if n := contaCon(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.photo_uploaded' AND attore = 'titolare@esempio.it'
		AND id_risorsa = ? AND esito = 'ok'`, strconv.FormatInt(p.busta, 10)); n != 2 {
		t.Errorf("audit: %d", n)
	}
	// La scheda mostrata dopo il caricamento non è un'apertura (D-44): una sola repair.viewed.
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`); n != 1 {
		t.Errorf("repair.viewed: %d", n)
	}
	if !p.caricamentiVuoti(t) {
		t.Error("resti nei caricamenti")
	}
	// Il dipendente carica dalla fotocamera su un'altra busta: la sua prima foto è la foto della busta.
	altra := bustaScheda(t, p.d, p.o, nil)
	dip := dipendenteDentro(t, p.h)
	dip.vai("GET", percorsoScheda(altra), nil, nil)
	if r := dip.carica(percorsoCaricamento(altra), fileProva{campo: "camera", nome: "camera.jpg", tipo: "image/jpeg", dati: []byte(pngPixel)}); !tornaAllaSchedaConSegno(r, altra) {
		t.Fatalf("dipendente: %d %q", r.codice, r.location)
	}
	g, err := p.b.ElencoFoto(context.Background(), altra)
	if err != nil || len(g) != 1 || !g[0].FotoBusta {
		t.Fatalf("fotocamera: %+v %v", g, err)
	}
	if n := contaCon(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.photo_uploaded' AND attore = 'banco@esempio.it'`); n != 1 {
		t.Errorf("audit del dipendente: %d", n)
	}
}

// inventario: RIP-S-29
// Una busta che ha già foto: le nuove vanno dopo l'ultima posizione e nessuna è la foto della busta.
// Una busta consegnata accetta foto come le altre.
func TestCaricamentoDopoLeAltreESuBustaConsegnata(t *testing.T) {
	p := facciaCaricamento(t, nil)
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'consegnata', data_consegna = '2026-10-30' WHERE id = ?`, p.busta)
	b := titolareDentro(t, p.h)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	if r := b.carica(percorsoCaricamento(p.busta), png("camera", "a.png")); !tornaAllaSchedaConSegno(r, p.busta) {
		t.Fatalf("primo: %d %q", r.codice, r.location)
	}
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	if r := b.carica(percorsoCaricamento(p.busta), png("gallery", "b.png"), png("gallery", "c.png")); !tornaAllaSchedaConSegno(r, p.busta) {
		t.Fatalf("secondo: %d %q", r.codice, r.location)
	}
	f := p.foto(t)
	if len(f) != 3 || !f[0].FotoBusta || f[1].FotoBusta || f[2].FotoBusta || f[1].Posizione != 2 || f[2].Posizione != 3 {
		t.Fatalf("foto: %+v", f)
	}
}

// riquadroErrori sono i messaggi del riquadro #repair-photo-upload-errors della pagina, in ordine.
func riquadroErrori(t *testing.T, corpo string) []string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<div id="repair-photo-upload-errors"[^>]*>(.*?)</div>`).FindStringSubmatch(corpo)
	if m == nil {
		t.Fatal("riquadro degli errori del caricamento non trovato")
	}
	var v []string
	for _, p := range regexp.MustCompile(`<p>(.*?)</p>`).FindAllStringSubmatch(m[1], -1) {
		v = append(v, p[1])
	}
	return v
}

func stessiErrori(t *testing.T, cosa string, got, attesi []string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(attesi, "|") {
		t.Errorf("%s: riquadro %q, attesi %q", cosa, got, attesi)
	}
}

func (p *provaCaricamento) nomiSalvati(t *testing.T) []string {
	t.Helper()
	var v []string
	for _, f := range p.foto(t) {
		v = append(v, f.NomeFile)
	}
	return v
}

func gif(campo, nome string) fileProva {
	return fileProva{campo: campo, nome: nome, tipo: "image/gif", dati: []byte("GIF89a")}
}

func enorme(campo, nome string) fileProva {
	return fileProva{campo: campo, nome: nome, tipo: "image/jpeg", dati: make([]byte, imaging.MaxByteIngresso+1)}
}

// inventario: RIP-S-29
// Più di 2 foto insieme, come lo standalone con auto_upload (put_entries/2 risponde :ok con
// :too_many_files, generate_preflight_response/4 autorizza le prime max_entries voci per indice, il
// client annulla le altre): le prime 2 di ogni campo (camera e gallery sono due allow_upload) si
// validano e si caricano, anche quando una delle due è rifiutata (occupa comunque il suo posto, come
// put_invalid_entry/3); le altre non si scrivono; nel riquadro «Massimo 2 foto per volta.» per
// campo, poi gli errori di ogni voce (anche oltre la seconda), nell'ordine di repair_live.ex.
func TestCaricamentoTroppeFoto(t *testing.T) {
	for _, c := range []struct {
		nome    string
		file    []fileProva
		salvate []string
		errori  []string
	}{
		{"tre buone", []fileProva{png("gallery", "one.png"), png("gallery", "two.png"), png("gallery", "three.png")}, []string{"one.jpg", "two.jpg"}, []string{msgTroppeFoto}},
		{"quattro buone", []fileProva{png("gallery", "1.png"), png("gallery", "2.png"), png("gallery", "3.png"), png("gallery", "4.png")}, []string{"1.jpg", "2.jpg"}, []string{msgTroppeFoto}},
		{"terza gif", []fileProva{png("gallery", "one.png"), png("gallery", "two.png"), gif("gallery", "x.gif")}, []string{"one.jpg", "two.jpg"}, []string{msgTroppeFoto, msgFormatoFoto}},
		{"prima gif", []fileProva{gif("gallery", "x.gif"), png("gallery", "one.png"), png("gallery", "two.png")}, []string{"one.jpg"}, []string{msgTroppeFoto, msgFormatoFoto}},
		{"prima enorme", []fileProva{enorme("gallery", "big.jpg"), png("gallery", "one.png"), png("gallery", "two.png")}, []string{"one.jpg"}, []string{msgTroppeFoto, msgFotoOltreLimite}},
		{"due campi", []fileProva{png("camera", "one.png"), png("gallery", "two.png"), png("gallery", "three.png")}, []string{"one.jpg", "two.jpg", "three.jpg"}, nil},
		{"tre per campo", []fileProva{png("camera", "a.png"), png("camera", "b.png"), png("camera", "c.png"), png("gallery", "d.png"), png("gallery", "e.png"), gif("gallery", "f.gif")},
			[]string{"a.jpg", "b.jpg", "d.jpg", "e.jpg"}, []string{msgTroppeFoto, msgTroppeFoto, msgFormatoFoto}},
	} {
		p := facciaCaricamento(t, nil)
		b := titolareDentro(t, p.h)
		b.vai("GET", percorsoScheda(p.busta), nil, nil)
		r := b.carica(percorsoCaricamento(p.busta), c.file...)
		if !tornaAllaSchedaConSegno(r, p.busta) {
			t.Fatalf("%s: %d %q", c.nome, r.codice, r.location)
		}
		pagina := b.dopo(r)
		stessiErrori(t, c.nome, riquadroErrori(t, pagina.corpo), c.errori)
		contiene(t, c.nome, pagina.corpo, `class="flash flash-info">Foto caricata.</div>`)
		nonContiene(t, c.nome, pagina.corpo, "flash-errore")
		if got := p.nomiSalvati(t); strings.Join(got, "|") != strings.Join(c.salvate, "|") {
			t.Errorf("%s: salvate %q, attese %q", c.nome, got, c.salvate)
		}
		if p.caricate(t) != len(c.salvate) || !p.caricamentiVuoti(t) {
			t.Errorf("%s: audit %d o resti", c.nome, p.caricate(t))
		}
		// Il riquadro si vede una volta: la scheda dopo è pulita.
		if v := riquadroErrori(t, b.vai("GET", percorsoScheda(p.busta), nil, nil).corpo); len(v) != 0 {
			t.Errorf("%s: riquadro ancora pieno: %q", c.nome, v)
		}
	}
}

// inventario: RIP-S-29
// Il formato: solo .jpg .jpeg .png .webp (senza badare alle maiuscole), oppure il tipo dichiarato
// image/jpeg, image/png o image/webp (accepted?/2 di LiveView: estensione o tipo). Fuori,
// «Formato non supportato.» nel riquadro (upload_errors) e la foto non si scrive; un'altra foto
// buona dello stesso invio si carica. Un nome senza estensione buona ma con il tipo buono passa il
// formato e il dominio lo rifiuta (valid_upload?/1): «Foto rifiutata o non salvata.» nel flash.
func TestCaricamentoFormato(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := titolareDentro(t, p.h)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	pagina := b.dopo(b.carica(percorsoCaricamento(p.busta), gif("gallery", "x.gif")))
	stessiErrori(t, "gif", riquadroErrori(t, pagina.corpo), []string{msgFormatoFoto})
	nonContiene(t, "gif", pagina.corpo, "Foto caricata.", "flash-errore")
	p.nienteScritto(t, "gif")

	heic := fileProva{campo: "gallery", nome: "x.heic", tipo: "image/jpeg", dati: []byte(pngPixel)}
	pagina = b.dopo(b.carica(percorsoCaricamento(p.busta), heic))
	contiene(t, "heic", pagina.corpo, `class="flash flash-errore">Foto rifiutata o non salvata.</div>`)
	stessiErrori(t, "heic", riquadroErrori(t, pagina.corpo), nil)
	p.nienteScritto(t, "heic")

	// Una gif e una foto buona: la buona si carica, la gif no.
	r := b.carica(percorsoCaricamento(p.busta), gif("gallery", "x.gif"), fileProva{campo: "gallery", nome: "MAIUSCOLA.PNG", tipo: "application/octet-stream", dati: []byte(pngPixel)})
	if !tornaAllaSchedaConSegno(r, p.busta) {
		t.Fatalf("misto: %d %q", r.codice, r.location)
	}
	pagina = b.dopo(r)
	contiene(t, "misto", pagina.corpo, `class="flash flash-info">Foto caricata.</div>`)
	stessiErrori(t, "misto", riquadroErrori(t, pagina.corpo), []string{msgFormatoFoto})
	if f := p.foto(t); len(f) != 1 || !f[0].FotoBusta || f[0].NomeFile != "MAIUSCOLA.jpg" {
		t.Errorf("misto: %+v", f)
	}
}

// inventario: RIP-S-29
// 25 MiB passano la misura (poi il dominio rifiuta byte che non sono un'immagine: «Foto rifiutata o
// non salvata.» nel flash); un byte in più è «Foto oltre il limite di 25 MiB.» nel riquadro, anche
// con un formato sbagliato (validate_max_file_size viene prima di validate_accepted), e il file non
// resta. Un'immagine non valida o vuota è «Foto rifiutata o non salvata.».
func TestCaricamentoMisuraEImmagineNonValida(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := titolareDentro(t, p.h)
	for _, c := range []struct {
		nome     string
		file     fileProva
		flash    string
		riquadro []string
	}{
		{"25 MiB", fileProva{campo: "gallery", nome: "a.jpg", tipo: "image/jpeg", dati: make([]byte, imaging.MaxByteIngresso)}, msgFotoRifiutata, nil},
		{"un byte in più", enorme("camera", "b.jpg"), "", []string{msgFotoOltreLimite}},
		{"un byte in più e gif", fileProva{campo: "gallery", nome: "b.gif", tipo: "image/gif", dati: make([]byte, imaging.MaxByteIngresso+1)}, "", []string{msgFotoOltreLimite}},
		{"non un'immagine", fileProva{campo: "gallery", nome: "c.jpg", tipo: "image/jpeg", dati: []byte("non sono una foto")}, msgFotoRifiutata, nil},
		{"vuoto", fileProva{campo: "gallery", nome: "d.png", tipo: "image/png", dati: nil}, msgFotoRifiutata, nil},
	} {
		b.vai("GET", percorsoScheda(p.busta), nil, nil)
		pagina := b.dopo(b.carica(percorsoCaricamento(p.busta), c.file))
		if c.flash != "" {
			contiene(t, c.nome, pagina.corpo, `class="flash flash-errore">`+c.flash+`</div>`)
		} else {
			nonContiene(t, c.nome, pagina.corpo, "flash-errore")
		}
		stessiErrori(t, c.nome, riquadroErrori(t, pagina.corpo), c.riquadro)
		nonContiene(t, c.nome, pagina.corpo, "Foto caricata.")
		p.nienteScritto(t, c.nome)
	}
}

// sequenzaErrori dà, a ogni normalizzazione, l'errore successivo della lista (nil: quella vera).
type sequenzaErrori struct {
	mu   sync.Mutex
	errs []error
}

func (s *sequenzaErrori) Normalizza(ctx context.Context, in []byte) ([]byte, error) {
	s.mu.Lock()
	var err error
	if len(s.errs) > 0 {
		err, s.errs = s.errs[0], s.errs[1:]
	}
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return imaging.Normalizza(in)
}

// inventario: RIP-S-29
// Gli errori del dominio vanno nel flash come put_flash(:error, …) per ogni foto: si vede l'ultimo;
// «Foto caricata.» se almeno una è salvata (put_flash(:info, …)).
func TestCaricamentoErroriDelDominioNelFlash(t *testing.T) {
	risorse := &imaging.ErroreRisorse{Causa: imaging.CausaTempo}
	for _, c := range []struct {
		nome    string
		errs    []error
		flash   string
		salvate int
	}{
		{"risorse poi rifiuto", []error{risorse, imaging.ErrImmagine}, msgFotoRifiutata, 0},
		{"rifiuto poi risorse", []error{imaging.ErrImmagine, risorse}, buste.MsgFotoNonElaborata, 0},
		{"rifiuto poi buona", []error{imaging.ErrImmagine, nil}, msgFotoRifiutata, 1},
	} {
		p := facciaCaricamento(t, &sequenzaErrori{errs: c.errs})
		b := titolareDentro(t, p.h)
		b.vai("GET", percorsoScheda(p.busta), nil, nil)
		pagina := b.dopo(b.carica(percorsoCaricamento(p.busta), png("gallery", "a.png"), png("gallery", "b.png")))
		contiene(t, c.nome, pagina.corpo, `class="flash flash-errore">`+c.flash+`</div>`)
		if strings.Count(pagina.corpo, "flash-errore") != 1 {
			t.Errorf("%s: più di un flash d'errore", c.nome)
		}
		if (c.salvate > 0) != strings.Contains(pagina.corpo, `class="flash flash-info">Foto caricata.</div>`) || len(p.foto(t)) != c.salvate {
			t.Errorf("%s: salvate %d", c.nome, len(p.foto(t)))
		}
		stessiErrori(t, c.nome, riquadroErrori(t, pagina.corpo), nil)
	}
}

// richiestaDiretta manda il corpo al gestore senza rete (httptest.NewRecorder), con i cookie di
// b, e conta i byte che il server ha letto; lunghezza < 0: corpo a pezzi.
func richiestaDiretta(t *testing.T, h http.Handler, b *browser, percorso, tipo string, corpo io.Reader, lunghezza int64) (*httptest.ResponseRecorder, *lettoreContatoDa) {
	t.Helper()
	l := &lettoreContatoDa{r: corpo}
	req := httptest.NewRequest("POST", "https://localhost"+percorso, l)
	req.ContentLength = lunghezza
	req.Header.Set("Content-Type", tipo)
	u, _ := url.Parse(b.srv.URL)
	for _, k := range b.c.Jar.Cookies(u) {
		req.AddCookie(k)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w, l
}

// lettoreContatoDa conta i byte letti da r.
type lettoreContatoDa struct {
	r     io.Reader
	letti int64
}

func (l *lettoreContatoDa) Read(p []byte) (int, error) {
	n, err := l.r.Read(p)
	l.letti += int64(n)
	return n, err
}

// inventario: RIP-S-29
// Oltre il limite senza JavaScript: il Content-Length dichiarato non basta per rifiutare; il corpo
// si legge e si decide alla prima violazione. Due foto da 30 MiB (60 MiB, oltre il limite): tutte e
// due «Foto oltre il limite di 25 MiB.», un redirect alla scheda, niente scritto, mai più del limite
// letto; lo stesso a pezzi. Una foto da 30 MiB dal browser vero: il redirect con il messaggio. Il
// 413 resta solo per un corpo abusivo (oltre il limite senza nessuna violazione).
func TestCaricamentoOltreIlLimiteSenzaJavaScript(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := titolareDentro(t, p.h)
	trenta := func(nome string) fileProva {
		return fileProva{campo: "gallery", nome: nome, tipo: "image/jpeg", dati: make([]byte, 30<<20)}
	}
	for _, lunghezza := range []bool{true, false} {
		b.vai("GET", percorsoScheda(p.busta), nil, nil)
		corpo, tipo := corpoMultipart(t, b.testaDelModulo(), []fileProva{trenta("a.jpg"), trenta("b.jpg")}, nil)
		n := int64(-1)
		if lunghezza {
			n = int64(len(corpo))
		}
		w, l := richiestaDiretta(t, p.h, b, percorsoCaricamento(p.busta), tipo, bytes.NewReader(corpo), n)
		if w.Code != http.StatusFound || !strings.HasPrefix(w.Header().Get("Location"), percorsoScheda(p.busta)+"?mostra=") {
			t.Fatalf("due da 30 MiB (lunghezza %v): %d %q", lunghezza, w.Code, w.Header().Get("Location"))
		}
		if l.letti > limiteCaricamento+64<<10 {
			t.Errorf("letti %d byte", l.letti)
		}
		for _, k := range w.Result().Cookies() {
			b.c.Jar.SetCookies(mustURL(t, b.srv.URL), []*http.Cookie{k})
		}
		pagina := b.vai("GET", w.Header().Get("Location"), nil, nil)
		stessiErrori(t, "due da 30 MiB", riquadroErrori(t, pagina.corpo), []string{msgFotoOltreLimite, msgFotoOltreLimite})
		p.nienteScritto(t, "due da 30 MiB")
	}
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	pagina := b.dopo(b.carica(percorsoCaricamento(p.busta), trenta("c.jpg")))
	stessiErrori(t, "una da 30 MiB", riquadroErrori(t, pagina.corpo), []string{msgFotoOltreLimite})
	p.nienteScritto(t, "una da 30 MiB")
	// Abusivo: una foto buona e poi un campo che porta il corpo oltre il limite.
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	corpo, tipo := corpoMultipart(t, b.testaDelModulo(), []fileProva{png("gallery", "a.png")}, [][2]string{{"riempitivo", strings.Repeat("x", limiteCaricamento)}})
	w, _ := richiestaDiretta(t, p.h, b, percorsoCaricamento(p.busta), tipo, bytes.NewReader(corpo), int64(len(corpo)))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("abusivo: %d", w.Code)
	}
	p.nienteScritto(t, "abusivo")
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// inventario: RIP-S-29, ACC-17
// Finché il token non è valido il corpo si legge al più fino a LimiteModulo: una testa enorme (una
// parte con un'intestazione di 5 MiB, o un corpo dichiarato di 60 MiB senza token) si rifiuta senza
// leggerne di più.
func TestCaricamentoTestaPrimaDelTokenLimitata(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := titolareDentro(t, p.h)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	intestazione := "--xyz\r\nContent-Disposition: form-data; name=\"_csrf_token\"; x=\"" + strings.Repeat("a", 5<<20) + "\"\r\n\r\n" + b.token + "\r\n--xyz--\r\n"
	w, l := richiestaDiretta(t, p.h, b, percorsoCaricamento(p.busta), "multipart/form-data; boundary=xyz", strings.NewReader(intestazione), int64(len(intestazione)))
	if w.Code != http.StatusRequestEntityTooLarge && w.Code != http.StatusBadRequest {
		t.Errorf("intestazione enorme: %d", w.Code)
	}
	if l.letti > LimiteModulo+64<<10 {
		t.Errorf("intestazione enorme: letti %d byte", l.letti)
	}
	corpo, tipo := corpoMultipart(t, [][2]string{{"mostra", b.segno}}, []fileProva{trenta2()}, nil)
	w, l = richiestaDiretta(t, p.h, b, percorsoCaricamento(p.busta), tipo, bytes.NewReader(corpo), int64(len(corpo)))
	if w.Code != http.StatusForbidden || l.letti > LimiteModulo+64<<10 {
		t.Errorf("senza token: %d, letti %d", w.Code, l.letti)
	}
	p.nienteScritto(t, "testa enorme")
}

func trenta2() fileProva {
	return fileProva{campo: "gallery", nome: "a.jpg", tipo: "image/jpeg", dati: make([]byte, 30<<20)}
}

// inventario: RIP-S-29
// Le scelte già fatte nel browser (app.js, come il preflight di LiveView: più di 2 foto, o una oltre
// 25 MiB, o un formato fuori) arrivano come campi «anteprima» in testa al modulo, con le foto
// valide fra le prime 2: il riquadro le mostra nell'ordine, un codice sconosciuto non conta. Senza
// foto da mandare, il riquadro e basta.
func TestCaricamentoAnteprimaDelBrowser(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := titolareDentro(t, p.h)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	testa := append(b.testaDelModulo(), [2]string{"anteprima", "troppe"}, [2]string{"anteprima", "misura"}, [2]string{"anteprima", "boh"}, [2]string{"anteprima", "formato"})
	corpo, tipo := corpoMultipart(t, testa, []fileProva{png("gallery", "a.png")}, nil)
	r := b.inviaCorpo(percorsoCaricamento(p.busta), tipo, bytes.NewReader(corpo), nil)
	pagina := b.dopo(r)
	stessiErrori(t, "anteprima", riquadroErrori(t, pagina.corpo), []string{msgTroppeFoto, msgFotoOltreLimite, msgFormatoFoto})
	contiene(t, "anteprima", pagina.corpo, "Foto caricata.")
	if len(p.foto(t)) != 1 {
		t.Error("foto non salvata")
	}
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	testa = append(b.testaDelModulo(), [2]string{"anteprima", "misura"})
	corpo, tipo = corpoMultipart(t, testa, []fileProva{{campo: "gallery", nome: "", tipo: "application/octet-stream"}}, nil)
	r = b.inviaCorpo(percorsoCaricamento(p.busta), tipo, bytes.NewReader(corpo), nil)
	if !tornaAllaSchedaConSegno(r, p.busta) {
		t.Fatalf("senza foto: %d %q", r.codice, r.location)
	}
	pagina = b.dopo(r)
	stessiErrori(t, "senza foto", riquadroErrori(t, pagina.corpo), []string{msgFotoOltreLimite})
	nonContiene(t, "senza foto", pagina.corpo, "Foto caricata.")
}

// inventario: RIP-S-29
// Il tempo di lettura del caricamento è quello di chunk_timeout di LiveView (10 s di base) per ogni
// pezzo di 64 000 byte, non il ReadTimeout del server su tutto il corpo: con un server vero dal
// ReadTimeout corto, un corpo lento ma costante passa; un corpo fermo oltre il tempo del pezzo no,
// e niente si scrive.
func TestCaricamentoPezziLenti(t *testing.T) {
	p := facciaCaricamento(t, nil)
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(p.d, p.o))
	dd.Orologio, dd.Limiti, dd.Buste, dd.Caricamenti = p.o, ratelimit.Nuovo(p.o), p.b, p.caricamenti
	h, _, err := costruisciInterna(dd, func(f *facciaWeb, _ *http.ServeMux) { f.tempoPezzo = 400 * time.Millisecond })
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(h)
	srv.Config.ReadTimeout = 300 * time.Millisecond
	srv.StartTLS()
	t.Cleanup(srv.Close)
	b := browserSu(t, srv)
	if r := b.prova("titolare@esempio.it", pwProva); !redirectA(r, "/") {
		t.Fatalf("accesso: %d", r.codice)
	}
	invia := func(pausa time.Duration, pezzi int) (risposta, error) {
		b.vai("GET", percorsoScheda(p.busta), nil, nil)
		corpo, tipo := corpoMultipart(t, b.testaDelModulo(), []fileProva{{campo: "gallery", nome: "a.jpg", tipo: "image/jpeg", dati: make([]byte, pezzi*64_000)}}, nil)
		pr, pw := io.Pipe()
		go func() {
			for len(corpo) > 0 {
				k := min(len(corpo), 64_000)
				if _, err := pw.Write(corpo[:k]); err != nil {
					return
				}
				corpo = corpo[k:]
				time.Sleep(pausa)
			}
			_ = pw.Close()
		}()
		req, _ := http.NewRequest("POST", srv.URL+percorsoCaricamento(p.busta), pr)
		req.Header.Set("Content-Type", tipo)
		res, err := b.c.Do(req)
		if err != nil {
			return risposta{}, err
		}
		defer func() { _ = res.Body.Close() }()
		c, _ := io.ReadAll(res.Body) // solo per chiudere bene
		return risposta{res.StatusCode, res.Header.Get("Location"), string(c), res.Header}, nil
	}
	// 12 pezzi ogni 100 ms: 1,2 s, oltre il ReadTimeout di 300 ms.
	r, err := invia(100*time.Millisecond, 12)
	if err != nil || !tornaAllaSchedaConSegno(r, p.busta) {
		t.Fatalf("lento e costante: %v %d %q", err, r.codice, r.location)
	}
	contiene(t, "lento", b.dopo(r).corpo, `class="flash flash-errore">Foto rifiutata o non salvata.</div>`)
	// Fermo per 1 s fra un pezzo e l'altro: oltre il tempo del pezzo.
	r, err = invia(time.Second, 3)
	if err == nil && r.codice == http.StatusFound {
		t.Errorf("corpo fermo: %d %q", r.codice, r.location)
	}
	p.nienteScritto(t, "pezzi lenti")
}

// browserSu è nuovoBrowser su un server già avviato.
func browserSu(t *testing.T, srv *httptest.Server) *browser {
	jar, _ := cookiejar.New(nil)
	c := srv.Client()
	c.Jar = jar
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &browser{t: t, srv: srv, c: c}
}

// inventario: RIP-S-29
// Nessun file scelto: la scheda di nuovo, senza messaggi e senza scrivere niente.
func TestCaricamentoSenzaFile(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := titolareDentro(t, p.h)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	r := b.carica(percorsoCaricamento(p.busta), fileProva{campo: "gallery", nome: "", tipo: "application/octet-stream"})
	if !tornaAllaSchedaConSegno(r, p.busta) {
		t.Fatalf("%d %q", r.codice, r.location)
	}
	pagina := b.dopo(r)
	nonContiene(t, "senza file", pagina.corpo, "flash-errore", "flash-info")
	p.nienteScritto(t, "senza file")
}

// inventario: RIP-S-29
// Il figlio delle foto che non ce la fa adesso (D-48): «Foto non elaborata adesso: riprova tra
// poco.», niente scritto; nel registro la causa, mai il nome della foto, un percorso o una chiave.
func TestCaricamentoErroreDiRisorse(t *testing.T) {
	p := facciaCaricamento(t, normalizzaCon{err: &imaging.ErroreRisorse{Causa: imaging.CausaTempo, Dettaglio: "10s"}})
	b := titolareDentro(t, p.h)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	pagina := b.dopo(b.carica(percorsoCaricamento(p.busta), png("gallery", "nome-riservato.png")))
	contiene(t, "risorse", pagina.corpo, `class="flash flash-errore">`+buste.MsgFotoNonElaborata+`</div>`)
	p.nienteScritto(t, "risorse")
	reg := p.reg.String()
	if !strings.Contains(reg, "tempo") {
		t.Errorf("nel registro manca la causa: %s", reg)
	}
	for _, mai := range []string{"nome-riservato", p.caricamenti, p.dir} {
		if strings.Contains(reg, mai) {
			t.Errorf("nel registro c'è %q: %s", mai, reg)
		}
	}
}

// inventario: RIP-S-29
// Il registro non ha mai il nome della foto, un percorso dei caricamenti o la chiave salvata, né
// dopo un caricamento riuscito né dopo un rifiuto.
func TestCaricamentoNienteNelRegistro(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := titolareDentro(t, p.h)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	b.dopo(b.carica(percorsoCaricamento(p.busta), png("gallery", "nome-riservato.png")))
	b.dopo(b.carica(percorsoCaricamento(p.busta), fileProva{campo: "gallery", nome: "altro-riservato.gif", tipo: "image/gif", dati: []byte("x")}))
	b.dopo(b.carica(percorsoCaricamento(p.busta), fileProva{campo: "gallery", nome: "terzo-riservato.jpg", tipo: "image/jpeg", dati: []byte("x")}))
	f := p.foto(t)
	if len(f) != 1 {
		t.Fatalf("foto: %d", len(f))
	}
	reg := p.reg.String()
	for _, mai := range []string{"riservato", p.caricamenti, p.dir, f[0].Chiave, percorsoCaricamento(p.busta)} {
		if strings.Contains(reg, mai) {
			t.Errorf("nel registro c'è %q", mai)
		}
	}
}

// inventario: RIP-S-29
// I file del caricamento stanno in una cartella privata della richiesta (0700), a 0600, con un nome
// che non è quello del browser, e si tolgono sempre, riuscito o no.
func TestCaricamentoFileTemporanei(t *testing.T) {
	var p *provaCaricamento
	var visti []string
	controlla := func() {
		_ = filepath.WalkDir(p.caricamenti, func(q string, e os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			fi, err := os.Lstat(q)
			if err != nil {
				return err
			}
			visti = append(visti, q)
			switch {
			case q == p.caricamenti || e.IsDir():
				if fi.Mode().Perm() != 0o700 || fi.Mode()&os.ModeSymlink != 0 {
					t.Errorf("cartella %s: %v", q, fi.Mode())
				}
			default:
				if !fi.Mode().IsRegular() || fi.Mode().Perm() != 0o600 {
					t.Errorf("file %s: %v", q, fi.Mode())
				}
				if strings.Contains(q, "nome-del-browser") {
					t.Errorf("il file ha il nome del browser: %s", q)
				}
			}
			return nil
		})
	}
	p = facciaCaricamento(t, normalizzaCon{prima: controlla})
	b := titolareDentro(t, p.h)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	b.dopo(b.carica(percorsoCaricamento(p.busta), png("gallery", "nome-del-browser.png")))
	if len(visti) < 3 { // la cartella, quella della richiesta e il file
		t.Errorf("visti %v", visti)
	}
	if !p.caricamentiVuoti(t) {
		t.Error("resti dopo un caricamento riuscito")
	}
	visti = nil
	p2 := facciaCaricamento(t, normalizzaCon{err: imaging.ErrImmagine})
	p = p2
	b = titolareDentro(t, p.h)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	b.dopo(b.carica(percorsoCaricamento(p.busta), png("gallery", "nome-del-browser.png")))
	if !p.caricamentiVuoti(t) {
		t.Error("resti dopo un rifiuto")
	}
}

// inventario: RIP-S-29
// La sessione si ricontrolla prima di salvare: l'account disattivato, o la sessione chiusa da
// «chiudi tutte», mentre le foto arrivano porta a /login senza scrivere niente, nemmeno nell'archivio.
// E si ricontrolla dopo la normalizzazione, nella transazione: disattivato in quel momento, /login
// e nessuna riga (il file normalizzato resta nell'archivio, orfano, come lo standalone).
func TestCaricamentoSessioneRicontrollata(t *testing.T) {
	for _, c := range []struct {
		nome, sql string
	}{
		{"disattivato", `UPDATE utenti SET attivo = 0 WHERE email = 'banco@esempio.it'`},
		{"chiudi tutte", `UPDATE utenti SET versione_sessione = versione_sessione + 1 WHERE email = 'banco@esempio.it'`},
	} {
		p := facciaCaricamento(t, nil)
		b := dipendenteDentro(t, p.h)
		b.vai("GET", percorsoScheda(p.busta), nil, nil)
		corpo, tipo := corpoMultipart(t, b.testaDelModulo(), []fileProva{png("gallery", "a.png")}, nil)
		meta := bytes.Index(corpo, []byte("\x89PNG")) + 4
		// La prima metà del corpo; quando il file del caricamento c'è (la catena, con il controllo
		// della sessione all'inizio, è passata e le foto arrivano), l'account cambia; poi il resto.
		pr, pw := io.Pipe()
		go func() {
			_, _ = pw.Write(corpo[:meta])
			for fine := time.Now().Add(10 * time.Second); time.Now().Before(fine) && !p.fileInArrivo(); {
				time.Sleep(5 * time.Millisecond)
			}
			if _, err := p.d.Scrittura.Exec(c.sql); err != nil {
				t.Error(err)
			}
			_, _ = pw.Write(corpo[meta:])
			_ = pw.Close()
		}()
		r := b.inviaCorpo(percorsoCaricamento(p.busta), tipo, pr, nil)
		if !redirectA(r, "/login") {
			t.Errorf("%s durante l'invio: %d %q", c.nome, r.codice, r.location)
		}
		if b.dopo(r).corpo == "" {
			t.Error("pagina di login vuota")
		}
		p.nienteScritto(t, c.nome+" durante l'invio")
	}
	var p *provaCaricamento
	p = facciaCaricamento(t, normalizzaCon{prima: func() {
		eseguiSQL(t, p.d, `UPDATE utenti SET attivo = 0 WHERE email = 'banco@esempio.it'`)
	}})
	b := dipendenteDentro(t, p.h)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	r := b.carica(percorsoCaricamento(p.busta), png("gallery", "a.png"))
	if !redirectA(r, "/login") {
		t.Errorf("dopo la normalizzazione: %d %q", r.codice, r.location)
	}
	if len(p.foto(t)) != 0 || p.caricate(t) != 0 {
		t.Error("righe scritte con la sessione revocata")
	}
	if !p.caricamentiVuoti(t) {
		t.Error("resti nei caricamenti")
	}
}

// fileInArrivo dice se nella cartella dei caricamenti c'è già il file di una foto in arrivo.
func (p *provaCaricamento) fileInArrivo() bool {
	trovato := false
	_ = filepath.WalkDir(p.caricamenti, func(_ string, e os.DirEntry, err error) error {
		if err == nil && e.Type().IsRegular() {
			trovato = true
		}
		return nil
	})
	return trovato
}

// inventario: RIP-S-29
// Una busta nel cestino o che non c'è: la home con «Riparazione non trovata.», niente righe (come
// le altre azioni della scheda); un id malformato anche.
func TestCaricamentoBustaNelCestino(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := titolareDentro(t, p.h)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	eseguiSQL(t, p.d, `UPDATE buste SET cancellata_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, p.busta)
	for _, q := range []string{percorsoCaricamento(p.busta), percorsoCaricamento(p.busta + 100), "/repairs/abc/foto"} {
		r := b.carica(q, png("gallery", "a.png"))
		if !redirectA(r, "/") {
			t.Errorf("%s: %d %q", q, r.codice, r.location)
			continue
		}
		contiene(t, q, b.dopo(r).corpo, "Riparazione non trovata.")
	}
	if len(p.foto(t)) != 0 || p.caricate(t) != 0 || !p.caricamentiVuoti(t) {
		t.Error("scritto qualcosa su una busta nel cestino")
	}
}

// inventario: RIP-S-29, ACC-17
// Il token CSRF del modulo con le foto: nel campo _csrf_token in testa al corpo (prima dei file),
// o nell'intestazione x-csrf-token se il campo non c'è. Senza token, con un token sbagliato, di un
// altro browser, solo nella query o arrivato dopo i file: 403 «Forbidden», e nessun byte delle foto
// arriva su disco.
func TestCaricamentoCSRF(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := titolareDentro(t, p.h)
	altro := titolareDentro(t, p.h)
	altro.vai("GET", percorsoScheda(p.busta), nil, nil)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	file := []fileProva{png("gallery", "a.png")}
	for _, c := range []struct {
		nome        string
		testa, coda [][2]string
		h           map[string]string
		query       string
	}{
		{"senza token", [][2]string{{"mostra", b.segno}}, nil, nil, ""},
		{"token sbagliato", [][2]string{{"_csrf_token", "sbagliato"}}, nil, nil, ""},
		{"token vuoto e intestazione giusta", [][2]string{{"_csrf_token", ""}}, nil, map[string]string{"X-Csrf-Token": b.token}, ""},
		{"token di un altro browser", [][2]string{{"_csrf_token", altro.token}}, nil, nil, ""},
		{"token nella query", nil, nil, nil, "?_csrf_token=" + url.QueryEscape(b.token)},
		{"token dopo i file", nil, [][2]string{{"_csrf_token", b.token}}, nil, ""},
		{"intestazione sbagliata", nil, nil, map[string]string{"X-Csrf-Token": "sbagliato"}, ""},
	} {
		corpo, tipo := corpoMultipart(t, c.testa, file, c.coda)
		r := b.inviaCorpo(percorsoCaricamento(p.busta)+c.query, tipo, bytes.NewReader(corpo), c.h)
		if r.codice != http.StatusForbidden || r.corpo != "Forbidden" {
			t.Errorf("%s: %d %q", c.nome, r.codice, r.corpo)
		}
		p.nienteScritto(t, c.nome)
	}
	// Solo l'intestazione (come una pagina con JavaScript): passa.
	corpo, tipo := corpoMultipart(t, [][2]string{{"mostra", b.segno}}, file, nil)
	r := b.inviaCorpo(percorsoCaricamento(p.busta), tipo, bytes.NewReader(corpo), map[string]string{"X-Csrf-Token": b.token})
	if !tornaAllaSchedaConSegno(r, p.busta) || len(p.foto(t)) != 1 {
		t.Errorf("intestazione: %d %q", r.codice, r.location)
	}
	// Da un'altra origine: 403 anche con il token giusto.
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	corpo, tipo = corpoMultipart(t, b.testaDelModulo(), file, nil)
	r = b.inviaCorpo(percorsoCaricamento(p.busta), tipo, bytes.NewReader(corpo), map[string]string{"Sec-Fetch-Site": "cross-site"})
	if r.codice != http.StatusForbidden || len(p.foto(t)) != 1 {
		t.Errorf("altra origine: %d", r.codice)
	}
}

// inventario: RIP-S-29
// Un modulo malformato (senza boundary, una parte con un campo enorme in testa) è il 400 dei moduli,
// senza scrivere niente.
func TestCaricamentoModuloMalformato(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := titolareDentro(t, p.h)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	r := b.inviaCorpo(percorsoCaricamento(p.busta), "multipart/form-data", strings.NewReader("niente"), map[string]string{"X-Csrf-Token": b.token})
	if r.codice != http.StatusBadRequest {
		t.Errorf("senza boundary: %d", r.codice)
	}
	corpo, tipo := corpoMultipart(t, [][2]string{{"_csrf_token", b.token}, {"enorme", strings.Repeat("x", 64<<10)}}, []fileProva{png("gallery", "a.png")}, nil)
	if r := b.inviaCorpo(percorsoCaricamento(p.busta), tipo, bytes.NewReader(corpo), nil); r.codice != http.StatusBadRequest {
		t.Errorf("campo enorme: %d", r.codice)
	}
	corpo, tipo = corpoMultipart(t, [][2]string{{"_csrf_token", b.token}, {"mostra", "\xff\xfe"}}, []fileProva{png("gallery", "a.png")}, nil)
	if r := b.inviaCorpo(percorsoCaricamento(p.busta), tipo, bytes.NewReader(corpo), nil); r.codice != http.StatusBadRequest {
		t.Errorf("UTF-8 non valido: %d", r.codice)
	}
	p.nienteScritto(t, "malformato")
}

// provaVisibilita è facciaConFoto con una busta B2B (oreficeria 2) e la sua foto, oltre a quella
// diretta di facciaConFoto.
func provaVisibilita(t *testing.T) (*provaFotoWeb, int64, buste.Foto) {
	t.Helper()
	p := facciaConFoto(t)
	eseguiSQL(t, p.d, `INSERT INTO oreficerie (id, nome) VALUES (1, 'Il negozio di prova'), (2, 'Gioielleria inventata')`)
	b2b := bustaScheda(t, p.d, p.o, nil)
	eseguiSQL(t, p.d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, b2b)
	in := filepath.Join(p.dir, "in-b2b.png")
	if err := os.WriteFile(in, []byte(pngPixel), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := p.b.AllegaFoto(context.Background(), b2b, []buste.Caricamento{{Percorso: in, NomeOriginale: "b2b.png"}}, nil, audit.Contesto{Attore: "x"})
	if err != nil {
		t.Fatal(err)
	}
	return p, b2b, f[0]
}

func (p *provaFotoWeb) fotoDi(t *testing.T, busta, foto int64) (buste.Foto, bool) {
	t.Helper()
	f, ok, err := p.b.FotoDi(context.Background(), busta, foto)
	if err != nil {
		t.Fatal(err)
	}
	return f, ok
}

// inventario: RIP-S-31, RIP-S-32
// Sotto ogni foto i canali della sua busta: diretta «Cliente» e «Artigiano», B2B «Orefice» e
// «Artigiano», con ✓ o ⛔ e un modulo per pulsante (POST con audience e visible, i valori di
// phx-value); «Elimina foto» con la conferma «Eliminare la foto?». Un clic cambia la visibilità,
// scrive repair.photo_visibility_changed e torna alla scheda con il segno; «Elimina foto» toglie la
// riga (il file resta), scrive repair.photo_removed e torna con «Foto eliminata.». Il dipendente
// come il titolare.
func TestPaginaVisibilitaEdEliminazioneFoto(t *testing.T) {
	p, b2b, fb2b := provaVisibilita(t)
	for i, b := range []*browser{titolareDentro(t, p.h), dipendenteDentro(t, p.h)} {
		r := b.vai("GET", percorsoScheda(p.busta), nil, nil)
		s := strconv.FormatInt(p.grande.ID, 10)
		c := cella(r.corpo, p.grande.ID)
		contiene(t, "diretta", c,
			`<form action="/repairs/`+strconv.FormatInt(p.busta, 10)+`/foto/`+s+`/visibilita" method="post"`,
			`<input type="hidden" name="audience" value="customer">`, `<input type="hidden" name="audience" value="artisan">`,
			`<button id="customer-photo-`+s+`" type="submit" data-audience="customer"`, `title="Visibile al cliente">Cliente ✓</button>`,
			`<button id="artisan-photo-`+s+`" type="submit" data-audience="artisan"`, `title="Visibile all'artigiano">Artigiano ✓</button>`,
			`<form action="/repairs/`+strconv.FormatInt(p.busta, 10)+`/foto/`+s+`/elimina" method="post"`,
			`<button id="remove-repair-photo-`+s+`" type="submit" data-confirm="Eliminare la foto?"`, `>Elimina foto</button>`)
		nonContiene(t, "diretta", c, "orefice", "Orefice")
		// La foto della busta non va all'artigiano: «Artigiano ⛔», e il clic manda visible=true.
		fb := cella(r.corpo, p.foto.ID)
		contiene(t, "foto della busta", fb, `title="Visibile all'artigiano">Artigiano ⛔</button>`)
		if i == 0 {
			v := regexp.MustCompile(`(?s)<form action="[^"]*/foto/`+strconv.FormatInt(p.foto.ID, 10)+`/visibilita"[^>]*>.*?</form>`).FindAllString(fb, -1)
			if len(v) != 2 || !strings.Contains(v[1], `name="visible" value="true"`) || !strings.Contains(v[0], `name="visible" value="false"`) {
				t.Errorf("moduli della visibilità: %q", v)
			}
		}
		r = b.vai("GET", percorsoScheda(b2b), nil, nil)
		s = strconv.FormatInt(fb2b.ID, 10)
		c = cella(r.corpo, fb2b.ID)
		contiene(t, "b2b", c, `<input type="hidden" name="audience" value="orefice">`, `<button id="orefice-photo-`+s+`" type="submit" data-audience="orefice"`,
			`title="Visibile all'orefice">Orefice ✓</button>`, `title="Visibile all'artigiano">Artigiano ⛔</button>`)
		nonContiene(t, "b2b", c, "customer", "Cliente")
	}
	// Il clic: «Artigiano ⛔» della foto della busta diventa ✓.
	b := dipendenteDentro(t, p.h)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	r := b.vai("POST", percorsoVisibilita(p.busta, p.foto.ID), url.Values{"_csrf_token": {b.token}, "audience": {"artisan"}, "visible": {"true"}}, nil)
	if !tornaAllaSchedaConSegno(r, p.busta) {
		t.Fatalf("visibilità: %d %q", r.codice, r.location)
	}
	pagina := b.dopo(r)
	contiene(t, "dopo il clic", cella(pagina.corpo, p.foto.ID), `Artigiano ✓</button>`)
	nonContiene(t, "dopo il clic", pagina.corpo, "flash-errore", "flash-info")
	if f, _ := p.fotoDi(t, p.busta, p.foto.ID); !f.Artigiano {
		t.Error("artigiano non aggiornato")
	}
	if got := ultimoAuditWeb(t, p.d, "repair.photo_visibility_changed"); got != strconv.FormatInt(p.busta, 10)+`|ok|{"audience":"artisan","photo_id":`+strconv.FormatInt(p.foto.ID, 10)+`,"visible":true}` {
		t.Errorf("audit: %s", got)
	}
	// Oreficeria della B2B chiusa dal titolare.
	tit := titolareDentro(t, p.h)
	tit.vai("GET", percorsoScheda(b2b), nil, nil)
	if r := tit.vai("POST", percorsoVisibilita(b2b, fb2b.ID), url.Values{"_csrf_token": {tit.token}, "audience": {"orefice"}, "visible": {"false"}}, nil); !tornaAllaSchedaConSegno(r, b2b) {
		t.Fatalf("orefice: %d %q", r.codice, r.location)
	}
	if f, _ := p.fotoDi(t, b2b, fb2b.ID); f.Oreficeria {
		t.Error("orefice non aggiornato")
	}
	// Elimina: la riga va, il file resta.
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	r = b.vai("POST", percorsoElimina(p.busta, p.grande.ID), url.Values{"_csrf_token": {b.token}}, nil)
	if !tornaAllaSchedaConSegno(r, p.busta) {
		t.Fatalf("elimina: %d %q", r.codice, r.location)
	}
	pagina = b.dopo(r)
	contiene(t, "eliminata", pagina.corpo, `class="flash flash-info">Foto eliminata.</div>`, "<h2>Foto (1)</h2>")
	nonContiene(t, "eliminata", pagina.corpo, `id="repair-photo-`+strconv.FormatInt(p.grande.ID, 10)+`"`)
	if _, ok := p.fotoDi(t, p.busta, p.grande.ID); ok {
		t.Error("riga non tolta")
	}
	if fi, err := os.Stat(filepath.Join(p.archivio.Cartella(), p.grande.Chiave)); err != nil || !fi.Mode().IsRegular() {
		t.Errorf("il file è sparito: %v", err)
	}
	if got := ultimoAuditWeb(t, p.d, "repair.photo_removed"); got != strconv.FormatInt(p.busta, 10)+`|ok|{"photo_id":`+strconv.FormatInt(p.grande.ID, 10)+`,"position":2}` {
		t.Errorf("audit: %s", got)
	}
	// Nessuna di queste mostre è un'apertura (D-44): le righe repair.viewed sono le GET della scheda.
}

// inventario: RIP-S-31
// «Visibilità non aggiornata.» per un pubblico che la busta non ha (cliente forzato su una B2B,
// orefice su una diretta), un pubblico inventato, visible che non è «true» o «false», una foto di
// un'altra busta, che non c'è o con l'id malformato; niente cambia e nessuna riga d'audit. Con il
// segno della pagina il ritorno è senza riga; senza, un'apertura vera.
func TestVisibilitaNonAggiornata(t *testing.T) {
	p, b2b, fb2b := provaVisibilita(t)
	b := titolareDentro(t, p.h)
	for _, c := range []struct {
		nome, percorso string
		campi          url.Values
		busta          int64
	}{
		{"cliente su B2B", percorsoVisibilita(b2b, fb2b.ID), url.Values{"audience": {"customer"}, "visible": {"true"}}, b2b},
		{"orefice su diretta", percorsoVisibilita(p.busta, p.foto.ID), url.Values{"audience": {"orefice"}, "visible": {"true"}}, p.busta},
		{"staff", percorsoVisibilita(p.busta, p.foto.ID), url.Values{"audience": {"staff"}, "visible": {"false"}}, p.busta},
		{"inventato", percorsoVisibilita(p.busta, p.foto.ID), url.Values{"audience": {"vicino"}, "visible": {"true"}}, p.busta},
		{"senza audience", percorsoVisibilita(p.busta, p.foto.ID), url.Values{"visible": {"true"}}, p.busta},
		{"visible sì", percorsoVisibilita(p.busta, p.foto.ID), url.Values{"audience": {"artisan"}, "visible": {"sì"}}, p.busta},
		{"visible on", percorsoVisibilita(p.busta, p.foto.ID), url.Values{"audience": {"artisan"}, "visible": {"on"}}, p.busta},
		{"visible TRUE", percorsoVisibilita(p.busta, p.foto.ID), url.Values{"audience": {"artisan"}, "visible": {"TRUE"}}, p.busta},
		{"senza visible", percorsoVisibilita(p.busta, p.foto.ID), url.Values{"audience": {"artisan"}}, p.busta},
		{"foto di un'altra busta", percorsoVisibilita(b2b, p.foto.ID), url.Values{"audience": {"artisan"}, "visible": {"true"}}, b2b},
		{"foto che non c'è", percorsoVisibilita(p.busta, 9999), url.Values{"audience": {"artisan"}, "visible": {"true"}}, p.busta},
		{"id malformato", "/repairs/" + strconv.FormatInt(p.busta, 10) + "/foto/abc/visibilita", url.Values{"audience": {"artisan"}, "visible": {"true"}}, p.busta},
		{"id zero", "/repairs/" + strconv.FormatInt(p.busta, 10) + "/foto/0/visibilita", url.Values{"audience": {"artisan"}, "visible": {"true"}}, p.busta},
	} {
		for _, conSegno := range []bool{true, false} {
			b.vai("GET", percorsoScheda(c.busta), nil, nil)
			b.senzaSegno = !conSegno
			c.campi.Set("_csrf_token", b.token)
			prima := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`)
			r := b.vai("POST", c.percorso, c.campi, nil)
			b.senzaSegno = false
			if conSegno && !tornaAllaSchedaConSegno(r, c.busta) || !conSegno && !redirectA(r, percorsoScheda(c.busta)) {
				t.Errorf("%s (segno %v): %d %q", c.nome, conSegno, r.codice, r.location)
				continue
			}
			contiene(t, c.nome, b.dopo(r).corpo, `class="flash flash-errore">Visibilità non aggiornata.</div>`)
			atteso := prima
			if !conSegno {
				atteso++
			}
			if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`); n != atteso {
				t.Errorf("%s (segno %v): repair.viewed %d, attese %d", c.nome, conSegno, n, atteso)
			}
		}
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.photo_visibility_changed'`); n != 0 {
		t.Errorf("audit: %d", n)
	}
	if f, _ := p.fotoDi(t, b2b, fb2b.ID); f.Cliente || !f.Oreficeria {
		t.Errorf("B2B cambiata: %+v", f)
	}
}

// inventario: RIP-S-32
// «Foto non disponibile.» per una foto di un'altra busta, che non c'è, già tolta o con l'id
// malformato: niente cambia e nessuna riga d'audit.
func TestEliminaFotoNonDisponibile(t *testing.T) {
	p, b2b, _ := provaVisibilita(t)
	b := dipendenteDentro(t, p.h)
	if err := p.b.RimuoviFoto(context.Background(), p.busta, p.grande.ID, audit.Contesto{Attore: "x"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		nome, percorso string
		busta          int64
	}{
		{"di un'altra busta", percorsoElimina(b2b, p.foto.ID), b2b},
		{"che non c'è", percorsoElimina(p.busta, 9999), p.busta},
		{"già tolta", percorsoElimina(p.busta, p.grande.ID), p.busta},
		{"id malformato", "/repairs/" + strconv.FormatInt(p.busta, 10) + "/foto/x1/elimina", p.busta},
		{"id negativo", "/repairs/" + strconv.FormatInt(p.busta, 10) + "/foto/-1/elimina", p.busta},
	} {
		b.vai("GET", percorsoScheda(c.busta), nil, nil)
		r := b.vai("POST", c.percorso, url.Values{"_csrf_token": {b.token}}, nil)
		if !tornaAllaSchedaConSegno(r, c.busta) {
			t.Errorf("%s: %d %q", c.nome, r.codice, r.location)
			continue
		}
		contiene(t, c.nome, b.dopo(r).corpo, `class="flash flash-errore">Foto non disponibile.</div>`)
	}
	if _, ok := p.fotoDi(t, p.busta, p.foto.ID); !ok {
		t.Error("foto tolta")
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.photo_removed'`); n != 1 {
		t.Errorf("audit: %d", n)
	}
}

// inventario: RIP-S-31, RIP-S-32
// Busta nel cestino, che non c'è o con l'id malformato: la home con «Riparazione non trovata.», come
// le altre azioni della scheda; niente cambia.
func TestVisibilitaEdEliminaSuBustaNelCestino(t *testing.T) {
	p := facciaConFoto(t)
	b := titolareDentro(t, p.h)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	eseguiSQL(t, p.d, `UPDATE buste SET cancellata_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, p.busta)
	for _, q := range []string{percorsoVisibilita(p.busta, p.foto.ID), percorsoElimina(p.busta, p.foto.ID),
		percorsoVisibilita(p.busta+100, p.foto.ID), percorsoElimina(p.busta+100, p.foto.ID),
		"/repairs/abc/foto/1/visibilita", "/repairs/abc/foto/1/elimina"} {
		r := b.vai("POST", q, url.Values{"_csrf_token": {b.token}, "audience": {"artisan"}, "visible": {"true"}}, nil)
		if !redirectA(r, "/") {
			t.Errorf("%s: %d %q", q, r.codice, r.location)
			continue
		}
		contiene(t, q, b.dopo(r).corpo, "Riparazione non trovata.")
	}
	if _, ok := p.fotoDi(t, p.busta, p.foto.ID); !ok {
		t.Error("foto tolta da una busta nel cestino")
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione IN ('repair.photo_removed', 'repair.photo_visibility_changed')`); n != 0 {
		t.Errorf("audit: %d", n)
	}
}

// inventario: RIP-S-31, RIP-S-32, ACC-17
// Visibilità ed elimina vogliono il token: senza, sbagliato o di un altro browser, 403 «Forbidden» e
// niente cambia.
func TestVisibilitaEdEliminaCSRF(t *testing.T) {
	p := facciaConFoto(t)
	b := titolareDentro(t, p.h)
	altro := titolareDentro(t, p.h)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	for _, q := range []string{percorsoVisibilita(p.busta, p.foto.ID), percorsoElimina(p.busta, p.foto.ID)} {
		for _, tok := range []string{"", "sbagliato", altro.token} {
			campi := url.Values{"audience": {"artisan"}, "visible": {"true"}}
			if tok != "" {
				campi.Set("_csrf_token", tok)
			}
			if r := b.vai("POST", q, campi, nil); r.codice != http.StatusForbidden || r.corpo != "Forbidden" {
				t.Errorf("%s %q: %d", q, tok, r.codice)
			}
		}
	}
	if f, ok := p.fotoDi(t, p.busta, p.foto.ID); !ok || f.Artigiano {
		t.Error("cambiata senza token")
	}
}

// inventario: RIP-S-29, RIP-S-31, RIP-S-32
// Senza accesso, o con l'account disattivato, le tre azioni vanno a /login senza cambiare niente.
func TestAzioniDelleFotoSoloConLaSessione(t *testing.T) {
	p := facciaConFoto(t)
	fuori := nuovoBrowser(t, p.h)
	fuori.vai("GET", "/login", nil, nil)
	b := dipendenteDentro(t, p.h)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	eseguiSQL(t, p.d, `UPDATE utenti SET attivo = 0 WHERE email = 'banco@esempio.it'`)
	for _, x := range []*browser{fuori, b} {
		for _, q := range []string{percorsoVisibilita(p.busta, p.foto.ID), percorsoElimina(p.busta, p.foto.ID), percorsoCaricamento(p.busta)} {
			r := x.vai("POST", q, url.Values{"_csrf_token": {x.token}, "audience": {"artisan"}, "visible": {"true"}}, nil)
			if !redirectA(r, "/login") {
				t.Errorf("%s: %d %q", q, r.codice, r.location)
			}
		}
	}
	if f, ok := p.fotoDi(t, p.busta, p.foto.ID); !ok || f.Artigiano {
		t.Error("cambiata senza sessione")
	}
}

// inventario: RIP-S-01, RIP-S-29, RIP-S-31, RIP-S-32
// Ogni modulo delle foto ha il segno della pagina (D-44); un caricamento rifiutato senza il segno
// (un POST forgiato) mostra la scheda come un'apertura vera, con la riga.
func TestModuliDelleFotoConIlSegno(t *testing.T) {
	p := facciaConFoto(t)
	b := titolareDentro(t, p.h)
	r := b.vai("GET", percorsoScheda(p.busta), nil, nil)
	segno := reSegnoPagina.FindStringSubmatch(r.corpo)
	if segno == nil {
		t.Fatal("nessun segno")
	}
	moduli := regexp.MustCompile(`(?s)<form [^>]*action="/repairs/\d+/foto[^"]*" method="post"[^>]*>.*?</form>`).FindAllString(r.corpo, -1)
	// 2 caricamenti, 2 foto con 2 pulsanti di visibilità e «Elimina foto».
	if len(moduli) != 2+2*3 {
		t.Fatalf("moduli delle foto: %d", len(moduli))
	}
	for _, m := range moduli {
		if !strings.Contains(m, `<input name="mostra" type="hidden" value="`+segno[1]+`">`) || !strings.Contains(m, `<input name="_csrf_token" type="hidden" value="`) {
			t.Errorf("modulo senza segno o token: %q", m)
		}
	}
	prima := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`)
	b.senzaSegno = true
	r = b.carica(percorsoCaricamento(p.busta), fileProva{campo: "gallery", nome: "x.gif", tipo: "image/gif", dati: []byte("x")})
	b.senzaSegno = false
	if !redirectA(r, percorsoScheda(p.busta)) {
		t.Fatalf("forgiato: %d %q", r.codice, r.location)
	}
	b.dopo(r)
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`); n != prima+1 {
		t.Errorf("repair.viewed %d, attese %d", n, prima+1)
	}
}

// inventario: RIP-S-29
// Il nome che dà il browser si ripulisce come clean_name/1 dello standalone prima di NomeSalvato:
// niente caratteri di controllo (qui una tabulazione: gli altri net/textproto li rifiuta già
// nell'intestazione della parte, 400), niente cartelle, niente spazi ai bordi; UTF-8 non valido
// diventa vuoto (e allora il dominio rifiuta per l'estensione).
func TestCaricamentoNomeRipulito(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := titolareDentro(t, p.h)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	b.dopo(b.carica(percorsoCaricamento(p.busta), png("gallery", "  cartella/no\tme.png ")))
	if got := p.nomiSalvati(t); len(got) != 1 || got[0] != "nome.jpg" {
		t.Errorf("nome %q", got)
	}
	pagina := b.dopo(b.carica(percorsoCaricamento(p.busta), png("gallery", "\xff\xfe.png")))
	contiene(t, "UTF-8 non valido", pagina.corpo, `class="flash flash-errore">Foto rifiutata o non salvata.</div>`)
	if len(p.foto(t)) != 1 {
		t.Error("salvata una foto con un nome non valido")
	}
}

// inventario: RIP-S-29
// All'avvio la cartella dei caricamenti è una cartella vera a 0700: se manca si crea, a 0750 si
// corregge, un link o un file al suo posto è un errore; le cartelle di richieste rimaste si tolgono.
func TestPulisciCaricamenti(t *testing.T) {
	dir := t.TempDir()
	c := filepath.Join(dir, "caricamenti")
	if err := PulisciCaricamenti(c); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(c); err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		t.Fatalf("creata: %v %v", fi, err)
	}
	if err := os.Chmod(c, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(c, "richiesta-vecchia"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := PulisciCaricamenti(c); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(c); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("permessi non corretti: %v", fi.Mode())
	}
	if _, err := os.Stat(filepath.Join(c, "richiesta-vecchia")); !os.IsNotExist(err) {
		t.Error("richiesta vecchia rimasta")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(c, link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{link, file} {
		if err := PulisciCaricamenti(q); err == nil {
			t.Errorf("%s accettato", q)
		}
	}
}

// inventario: RIP-S-29
// Un link al posto della cartella dei caricamenti (anche verso una cartella 0700) non si segue: la
// foto non si riceve, risposta 500, niente scritto né lì né nella cartella vera.
func TestCaricamentiNonSeguonoUnLink(t *testing.T) {
	p := facciaCaricamento(t, nil)
	vera := t.TempDir()
	if err := os.Chmod(vera, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(vera, p.caricamenti); err != nil {
		t.Fatal(err)
	}
	b := titolareDentro(t, p.h)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	if r := b.carica(percorsoCaricamento(p.busta), png("gallery", "a.png")); r.codice != http.StatusInternalServerError {
		t.Errorf("%d %q", r.codice, r.location)
	}
	if voci, _ := os.ReadDir(vera); len(voci) != 0 || len(p.foto(t)) != 0 {
		t.Errorf("scritto attraverso il link: %v", voci)
	}
}

// inventario: RIP-S-29
// I tetti della lettura: al più 16 campi di testo prima del primo file (17: 400) e al più 16 parti
// dopo (17: 400, niente scritto); un file in un campo che non è camera né gallery (un upload che lo
// standalone non ha) è il 400, niente scritto.
func TestCaricamentoTettiDelleParti(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := titolareDentro(t, p.h)
	invia := func(testa [][2]string, file []fileProva) risposta {
		corpo, tipo := corpoMultipart(t, testa, file, nil)
		return b.inviaCorpo(percorsoCaricamento(p.busta), tipo, bytes.NewReader(corpo), nil)
	}
	campi := func(n int) [][2]string {
		v := [][2]string{{"_csrf_token", b.token}}
		for len(v) < n {
			v = append(v, [2]string{"altro", "x"})
		}
		return v
	}
	parti := func(n int) []fileProva {
		v := []fileProva{png("gallery", "a.png")}
		for len(v) < n {
			v = append(v, fileProva{campo: "gallery", nome: "", tipo: "application/octet-stream"})
		}
		return v
	}
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	if r := invia(campi(17), parti(1)); r.codice != http.StatusBadRequest {
		t.Errorf("17 campi: %d", r.codice)
	}
	if r := invia(campi(16), parti(17)); r.codice != http.StatusBadRequest {
		t.Errorf("17 parti: %d", r.codice)
	}
	if r := invia(campi(1), []fileProva{png("documento", "a.png")}); r.codice != http.StatusBadRequest {
		t.Errorf("campo sconosciuto: %d", r.codice)
	}
	p.nienteScritto(t, "tetti")
	if r := invia(campi(16), parti(16)); r.codice != http.StatusFound || len(p.foto(t)) != 1 {
		t.Errorf("16 campi e 16 parti: %d", r.codice)
	}
}

// inventario: RIP-S-01, RIP-S-29, RIP-S-32
// Un caricamento o un'eliminazione riusciti scrivono la loro riga e fanno ripartire la catena dei
// segni (allaScheda, non dopoRifiuto): 50 minuti dopo l'apertura l'azione, 50 dopo «Modifica» non
// scrive una riga (sarebbero 100 dall'apertura).
func TestAzioniDelleFotoFannoRipartireLaCatena(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := titolareDentro(t, p.h)
	viste := func() int {
		return contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`)
	}
	for _, azione := range []string{"caricamento", "eliminazione"} {
		b.vai("GET", percorsoScheda(p.busta), nil, nil)
		p.o.Sposta(50 * time.Minute)
		var r risposta
		if azione == "caricamento" {
			r = b.carica(percorsoCaricamento(p.busta), png("gallery", "a.png"))
		} else {
			r = b.vai("POST", percorsoElimina(p.busta, p.foto(t)[0].ID), url.Values{"_csrf_token": {b.token}}, nil)
		}
		pagina := b.dopo(r)
		p.o.Sposta(50 * time.Minute)
		prima := viste()
		b.vai("GET", hrefDi(t, pagina.corpo, "edit-repair"), nil, nil)
		if n := viste() - prima; n != 0 {
			t.Errorf("%s: «Modifica» 50 minuti dopo ha scritto %d righe", azione, n)
		}
	}
}

// busteConElencoGuasto sono le buste vere con ElencoFoto che non riesce.
type busteConElencoGuasto struct{ *buste.Buste }

func (busteConElencoGuasto) ElencoFoto(context.Context, int64) ([]buste.Foto, error) {
	return nil, errors.New("database guasto")
}

// inventario: RIP-S-28
// Se le foto della busta non si leggono, la scheda non si mostra a metà: 500 e una riga nel
// registro.
func TestSchedaConLeFotoNonLette(t *testing.T) {
	p := facciaCaricamento(t, nil)
	dd := dipendenzeProva(&sondaFinta{pronto: true}, slog.New(slog.NewTextHandler(p.reg, nil)), accounts.NuoviUtenti(p.d, p.o))
	dd.Orologio, dd.Limiti, dd.Buste = p.o, ratelimit.Nuovo(p.o), busteConElencoGuasto{p.b}
	h := mustInterna(t, dd)
	r := titolareDentro(t, h).vai("GET", percorsoScheda(p.busta), nil, nil)
	if r.codice != http.StatusInternalServerError || !strings.Contains(p.reg.String(), "foto della busta non lette") {
		t.Errorf("%d", r.codice)
	}
}

// inventario: RIP-S-31, RIP-S-32
// I rifiuti attesi (foto che non c'è, pubblico che la busta non ha) non vanno nel registro come
// guasti.
func TestRifiutiDelleFotoNonSonoGuasti(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := titolareDentro(t, p.h)
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	b.dopo(b.carica(percorsoCaricamento(p.busta), png("gallery", "a.png")))
	foto := p.foto(t)[0].ID
	b.vai("POST", percorsoElimina(p.busta, foto+100), url.Values{"_csrf_token": {b.token}}, nil)
	b.vai("POST", percorsoVisibilita(p.busta, foto+100), url.Values{"_csrf_token": {b.token}, "audience": {"artisan"}, "visible": {"true"}}, nil)
	b.vai("POST", percorsoVisibilita(p.busta, foto), url.Values{"_csrf_token": {b.token}, "audience": {"orefice"}, "visible": {"true"}}, nil)
	for _, mai := range []string{"foto non tolta", "visibilità della foto non cambiata", "level=ERROR"} {
		if strings.Contains(p.reg.String(), mai) {
			t.Errorf("nel registro: %q", mai)
		}
	}
}

// inventario: RIP-S-29, ACC-17
// Nessun gestore di internal/web legge un modulo con i file a modo suo: ParseMultipartForm,
// FormValue, PostFormValue, FormFile e MultipartReader leggerebbero il corpo intero senza i limiti e
// senza il controllo del token in testa (csrf.ModuloConFile).
func TestNessunaLetturaDelModuloFuoriDalCSRF(t *testing.T) {
	file, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range file {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, vietata := range []string{".ParseMultipartForm(", ".FormValue(", ".PostFormValue(", ".FormFile(", ".MultipartReader(", ".ParseForm("} {
			if strings.Contains(string(b), vietata) {
				t.Errorf("%s usa %s", f, vietata)
			}
		}
	}
}

// inventario: RIP-S-29
// Anteprima e barra di avanzamento del caricamento dalla scheda (slice 040b), come il riquadro
// #repair-photo-upload-progress di RepairLive (live_img_preview ed entry.progress): la scheda ha il
// riquadro vuoto (role=status, aria-live) prima di quello degli errori; con JavaScript app.js manda il
// modulo con XHR, mette per ogni foto l'anteprima («Anteprima di <nome>», un indirizzo blob: del
// browser), «<nome> · Caricamento foto: N%» e la barra «Avanzamento foto», e poi mostra la pagina che
// il server dà dopo il redirect, senza chiederla una seconda volta. Senza JavaScript il modulo resta
// quello della 038. Nel browser lo prova lo scenario caricamento del banco.
func TestCaricamentoAnteprimaEAvanzamento(t *testing.T) {
	p := facciaCaricamento(t, nil)
	r := titolareDentro(t, p.h).vai("GET", percorsoScheda(p.busta), nil, nil)
	riquadro := `<div id="repair-photo-upload-progress" role="status" aria-live="polite" class="avanzamento-foto"></div>`
	contiene(t, "scheda", r.corpo, riquadro)
	if i, j := strings.Index(r.corpo, riquadro), strings.Index(r.corpo, `<div id="repair-photo-upload-errors"`); i < 0 || j < i {
		t.Errorf("il riquadro dell'avanzamento non sta prima di quello degli errori (%d, %d)", i, j)
	}
	js := string(leggiStatico("app.js"))
	contiene(t, "app.js", js, `"#repair-photo-upload-progress"`, "`Anteprima di ${file.name}`", "URL.createObjectURL(file)",
		"`${file.name} · Caricamento foto: ${percento}%`", `"aria-label": "Avanzamento foto"`, "xhr.upload.onprogress",
		"new FormData(form)", "xhr.responseURL", "history.replaceState")
}
