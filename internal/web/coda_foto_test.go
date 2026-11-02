package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/imaging"
	"github.com/L0cutus/gestrip/internal/imaging/sintetiche"
	"github.com/L0cutus/gestrip/internal/media"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// La coda delle foto di «Nuova riparazione» (slice 039, RIP-S-35): POST /repairs/{id}/photos, una
// foto per richiesta, il corpo è la foto. I casi di repair_photo_upload_controller_test.exs dello
// standalone a go-baseline, uno per uno, più quelli del Go: D-48 (503), registro, file temporanei,
// CSRF senza leggere il corpo, dipendente, tempo per pezzo.

func percorsoCoda(busta int64) string {
	return "/repairs/" + strconv.FormatInt(busta, 10) + "/photos"
}

// inviaFoto manda una foto come photo_queue.js: il corpo è la foto, Content-Type image/png, il token
// della pagina in x-csrf-token. h aggiunge o cambia intestazioni; una chiave con «-» davanti la
// toglie.
func (b *browser) inviaFoto(percorso string, corpo []byte, h map[string]string) risposta {
	b.t.Helper()
	req, _ := http.NewRequest("POST", b.srv.URL+percorso, bytes.NewReader(corpo))
	intestazioniCoda(req, b.token, h)
	r, err := b.c.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	c, _ := io.ReadAll(r.Body) // un errore di lettura lascia il corpo a metà: i controlli lo vedono
	return risposta{r.StatusCode, r.Header.Get("Location"), string(c), r.Header}
}

func intestazioniCoda(req *http.Request, token string, h map[string]string) {
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("X-Csrf-Token", token)
	for k, v := range h {
		if nome, ok := strings.CutPrefix(k, "-"); ok {
			req.Header.Del(nome)
			continue
		}
		req.Header.Set(k, v)
	}
}

// nome è x-file-name come lo scrive la coda: encodeURIComponent del nome.
func conNome(n string) map[string]string {
	return map[string]string{"X-File-Name": url.PathEscape(n)}
}

// fotoDiretta manda la foto al gestore senza rete, con i cookie e il token di b, e conta i byte che
// il server ha letto; lunghezza < 0: corpo a pezzi.
func fotoDiretta(t *testing.T, h http.Handler, b *browser, percorso string, corpo io.Reader, lunghezza int64, intest map[string]string) (*httptest.ResponseRecorder, *lettoreContatoDa) {
	t.Helper()
	l := &lettoreContatoDa{r: corpo}
	req := httptest.NewRequest("POST", "https://localhost"+percorso, l)
	req.ContentLength = lunghezza
	intestazioniCoda(req, b.token, intest)
	u, _ := url.Parse(b.srv.URL)
	for _, k := range b.c.Jar.Cookies(u) {
		req.AddCookie(k)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w, l
}

// json dello standalone: %{ok: false, error: …} con le chiavi in ordine (mappa piccola di Erlang).
func rifiutoCoda(msg string) string {
	b, _ := json.Marshal(msg)
	return `{"error":` + string(b) + `,"ok":false}`
}

// rispostaJSON controlla codice, corpo e intestazioni di una risposta JSON della coda.
func rispostaJSON(t *testing.T, cosa string, r risposta, codice int, corpo string) {
	t.Helper()
	if r.codice != codice || r.corpo != corpo {
		t.Errorf("%s: %d %q, attesi %d %q", cosa, r.codice, r.corpo, codice, corpo)
	}
	for k, v := range map[string]string{
		"Content-Type": "application/json; charset=utf-8", "Cache-Control": cacheBase, "Location": "",
		"X-Content-Type-Options": "nosniff", "Referrer-Policy": "strict-origin-when-cross-origin",
		"Content-Security-Policy": "base-uri 'self'; frame-ancestors 'self';",
	} {
		if g := r.h.Get(k); g != v {
			t.Errorf("%s: %s = %q, atteso %q", cosa, k, g, v)
		}
	}
	if r.h.Get("X-Request-Id") == "" {
		t.Errorf("%s: manca x-request-id", cosa)
	}
}

// accolta: 201 con {"ok":true,"photo_id":<id>}; dà l'id.
func accolta(t *testing.T, cosa string, r risposta) int64 {
	t.Helper()
	var v struct {
		OK      bool  `json:"ok"`
		PhotoID int64 `json:"photo_id"`
	}
	if err := json.Unmarshal([]byte(r.corpo), &v); err != nil || !v.OK || v.PhotoID <= 0 {
		t.Fatalf("%s: %d %q", cosa, r.codice, r.corpo)
	}
	rispostaJSON(t, cosa, r, http.StatusCreated, `{"ok":true,"photo_id":`+strconv.FormatInt(v.PhotoID, 10)+`}`)
	return v.PhotoID
}

// pronto: il browser apre «Nuova riparazione» e prende il token del meta csrf-token, come la coda.
func pronto(b *browser) *browser {
	b.t.Helper()
	if r := b.vai("GET", "/nuova", nil, nil); r.codice != http.StatusOK {
		b.t.Fatalf("/nuova: %d", r.codice)
	}
	return b
}

// inventario: RIP-S-35
// La foto di chi ha fatto l'accesso va su quella busta, una volta, con la sua riga d'audit (attore,
// esito, id e posizione, indirizzo): 201 {"ok":true,"photo_id":…}; il nome tiene l'estensione di
// quello che si salva (un JPEG); la prima foto di una busta vuota è la foto della busta. La rotta non
// è la scheda: niente repair.viewed, niente redirect.
func TestCodaFotoAllegata(t *testing.T) {
	p := facciaCaricamento(t, nil)
	altra := bustaScheda(t, p.d, p.o, nil)
	b := pronto(titolareDentro(t, p.h))
	visteprima := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`)
	id := accolta(t, "titolare", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), conNome("anello del cliente.png")))
	f := p.foto(t)
	if len(f) != 1 || f[0].ID != id || f[0].NomeFile != "anello del cliente.jpg" || !f[0].FotoBusta || f[0].TipoMime != "image/jpeg" {
		t.Fatalf("foto: %+v", f)
	}
	if g, _ := p.b.ElencoFoto(t.Context(), altra); len(g) != 0 {
		t.Errorf("foto sull'altra busta: %d", len(g))
	}
	var attore, esito, dati, ip string
	if err := p.d.Lettura.QueryRow(`SELECT attore, esito, dati, ip FROM eventi_audit WHERE azione = 'repair.photo_uploaded'
		AND risorsa = 'repair' AND id_risorsa = ?`, strconv.FormatInt(p.busta, 10)).Scan(&attore, &esito, &dati, &ip); err != nil {
		t.Fatal(err)
	}
	var m map[string]int64
	if err := json.Unmarshal([]byte(dati), &m); err != nil {
		t.Fatal(err)
	}
	if attore != "titolare@esempio.it" || esito != "ok" || ip != "127.0.0.1" || len(m) != 2 || m["photo_id"] != id || m["position"] != 1 {
		t.Errorf("audit: %s %s %s %s", attore, esito, dati, ip)
	}
	if p.caricate(t) != 1 {
		t.Errorf("righe repair.photo_uploaded: %d", p.caricate(t))
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`); n != visteprima {
		t.Errorf("repair.viewed scritte: %d", n-visteprima)
	}
	if !p.caricamentiVuoti(t) {
		t.Error("resti nei caricamenti")
	}
}

// inventario: RIP-S-35
// Una richiesta è una foto, nell'ordine d'invio: la prima di una busta vuota è la foto della busta.
// Su una busta di un'oreficeria: la prima visibile all'orefice e non al cliente (né all'artigiano),
// la seconda all'artigiano e all'orefice.
func TestCodaFotoUnaPerRichiesta(t *testing.T) {
	p := facciaCaricamento(t, nil)
	eseguiSQL(t, p.d, `INSERT INTO oreficerie (id, nome) VALUES (1, 'Il negozio di prova'), (2, 'Oreficeria sintetica')`)
	eseguiSQL(t, p.d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, p.busta)
	b := pronto(titolareDentro(t, p.h))
	prima := accolta(t, "prima", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), conNome("first.png")))
	seconda := accolta(t, "seconda", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), conNome("second.png")))
	f := p.foto(t)
	if len(f) != 2 {
		t.Fatalf("foto: %d", len(f))
	}
	if f[0].ID != prima || f[1].ID != seconda || prima == seconda {
		t.Errorf("photo_id %d e %d, salvate %d e %d", prima, seconda, f[0].ID, f[1].ID)
	}
	if f[0].Posizione != 1 || f[1].Posizione != 2 || f[0].NomeFile != "first.jpg" || f[1].NomeFile != "second.jpg" {
		t.Errorf("ordine: %+v", f)
	}
	if !f[0].FotoBusta || !f[0].Oreficeria || f[0].Cliente || f[0].Artigiano {
		t.Errorf("prima: %+v", f[0])
	}
	if f[1].FotoBusta || !f[1].Artigiano || !f[1].Oreficeria || f[1].Cliente {
		t.Errorf("seconda: %+v", f[1])
	}
}

// inventario: RIP-S-35
// Il nome: senza x-file-name «foto», senza estensione (o con una che non è delle foto) quella del
// tipo dichiarato; poi il nome salvato prende l'estensione del JPEG.
func TestCodaFotoNomeSenzaEstensione(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := pronto(titolareDentro(t, p.h))
	accolta(t, "senza nome", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), nil))
	accolta(t, "IMG_0001", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), conNome("IMG_0001")))
	h := conNome("scatto.gif")
	h["Content-Type"] = "image/webp"
	accolta(t, "scatto.gif", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), h))
	accolta(t, "vuoto", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), map[string]string{"X-File-Name": ""}))
	if g := p.nomiSalvati(t); strings.Join(g, "|") != "foto.jpg|IMG_0001.jpg|scatto.gif.jpg|foto.jpg" {
		t.Errorf("nomi: %q", g)
	}
}

// inventario: RIP-S-35
// Un nome forgiato si ripulisce: niente cartelle, niente caratteri di controllo, solo testo valido
// (x-file-name è codificato come encodeURIComponent; una codifica rotta è «foto»).
func TestCodaFotoNomeRipulito(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := pronto(titolareDentro(t, p.h))
	for _, n := range []string{"../../etc/anello.png", "an\nel\x00lo.png", "\xff\xfe.png", "  spazi.webp  "} {
		accolta(t, n, b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), conNome(n)))
	}
	accolta(t, "codifica rotta", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), map[string]string{"X-File-Name": "anello%zz.png"}))
	accolta(t, "più non è uno spazio", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), map[string]string{"X-File-Name": "a+b.png"}))
	if g := p.nomiSalvati(t); strings.Join(g, "|") != "anello.jpg|anello.jpg|foto.jpg|spazi.jpg|foto.jpg|a+b.jpg" {
		t.Errorf("nomi: %q", g)
	}
}

// inventario: RIP-S-35
// Una busta consegnata prende la foto, come il caricamento della scheda.
func TestCodaFotoBustaConsegnata(t *testing.T) {
	p := facciaCaricamento(t, nil)
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'consegnata', data_pronta = '2026-10-27', data_consegna = '2026-10-28' WHERE id = ?`, p.busta)
	b := pronto(titolareDentro(t, p.h))
	accolta(t, "consegnata", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), nil))
	if len(p.foto(t)) != 1 {
		t.Error("foto non salvata")
	}
}

// inventario: RIP-S-35
// Nessuno dentro: con il token della pagina di accesso, a /login con «Devi accedere per continuare.»
// e niente scritto; senza nemmeno il cookie del token, il 403 del CSRF. Il corpo non si legge.
func TestCodaFotoSenzaAccesso(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := nuovoBrowser(t, p.h)
	b.vai("GET", "/login", nil, nil)
	w, l := fotoDiretta(t, p.h, b, percorsoCoda(p.busta), bytes.NewReader([]byte(pngPixel)), int64(len(pngPixel)), nil)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/login" || l.letti != 0 {
		t.Errorf("senza accesso: %d %q, letti %d", w.Code, w.Header().Get("Location"), l.letti)
	}
	r := b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), nil)
	if !redirectA(r, "/login") || !strings.Contains(b.vai("GET", "/login", nil, nil).corpo, "Devi accedere per continuare.") {
		t.Errorf("senza accesso: %d %q", r.codice, r.location)
	}
	vuoto := nuovoBrowser(t, p.h)
	if r := vuoto.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), nil); r.codice != http.StatusForbidden || r.corpo != "Forbidden" {
		t.Errorf("senza cookie: %d %q", r.codice, r.corpo)
	}
	p.nienteScritto(t, "senza accesso")
}

// inventario: RIP-S-35
// Un account disattivato, o una sessione chiusa (versione cambiata), è nessuno: a /login, niente
// scritto, e il corpo non si legge.
func TestCodaFotoAccountDisattivatoOSessioneChiusa(t *testing.T) {
	for _, c := range []struct{ nome, sql string }{
		{"disattivato", `UPDATE utenti SET attivo = 0 WHERE email = 'banco@esempio.it'`},
		{"chiudi tutte", `UPDATE utenti SET versione_sessione = versione_sessione + 1 WHERE email = 'banco@esempio.it'`},
	} {
		p := facciaCaricamento(t, nil)
		b := pronto(dipendenteDentro(t, p.h))
		eseguiSQL(t, p.d, c.sql)
		w, l := fotoDiretta(t, p.h, b, percorsoCoda(p.busta), bytes.NewReader([]byte(pngPixel)), int64(len(pngPixel)), nil)
		if w.Code != http.StatusFound || w.Header().Get("Location") != "/login" || l.letti != 0 {
			t.Errorf("%s: %d %q, letti %d", c.nome, w.Code, w.Header().Get("Location"), l.letti)
		}
		p.nienteScritto(t, c.nome)
	}
}

// inventario: RIP-S-35, ACC-17
// Il token CSRF della sessione è necessario e basta, nell'intestazione x-csrf-token (il corpo è la
// foto): senza, sbagliato, di un altro browser, solo nella query, o da un'altra origine, 403
// «Forbidden» senza leggere un byte del corpo; con il token della pagina, 201.
func TestCodaFotoCSRF(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := pronto(titolareDentro(t, p.h))
	altro := pronto(titolareDentro(t, p.h))
	for _, c := range []struct {
		nome, query string
		h           map[string]string
	}{
		{"senza token", "", map[string]string{"-X-Csrf-Token": ""}},
		{"token sbagliato", "", map[string]string{"X-Csrf-Token": "not-the-session-token"}},
		{"token di un altro browser", "", map[string]string{"X-Csrf-Token": altro.token}},
		{"token nella query", "?_csrf_token=" + url.QueryEscape(b.token), map[string]string{"-X-Csrf-Token": ""}},
		{"altra origine", "", map[string]string{"Sec-Fetch-Site": "cross-site"}},
	} {
		w, l := fotoDiretta(t, p.h, b, percorsoCoda(p.busta)+c.query, bytes.NewReader([]byte(pngPixel)), int64(len(pngPixel)), c.h)
		if w.Code != http.StatusForbidden || w.Body.String() != "Forbidden" || l.letti != 0 {
			t.Errorf("%s: %d %q, letti %d", c.nome, w.Code, w.Body.String(), l.letti)
		}
		p.nienteScritto(t, c.nome)
	}
	accolta(t, "token della pagina", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), nil))
	if len(p.foto(t)) != 1 {
		t.Error("foto non salvata")
	}
}

// inventario: RIP-S-35
// Una busta che non c'è, nel cestino o un id malformato: 404 «Busta non trovata.», prima di guardare
// il tipo e senza leggere il corpo.
func TestCodaFotoBustaNonTrovata(t *testing.T) {
	p := facciaCaricamento(t, nil)
	cestino := bustaScheda(t, p.d, p.o, nil)
	eseguiSQL(t, p.d, `UPDATE buste SET cancellata_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, cestino)
	b := pronto(titolareDentro(t, p.h))
	for _, q := range []string{percorsoCoda(999_999), "/repairs/abc/photos", "/repairs/0/photos", "/repairs/-1/photos", percorsoCoda(cestino)} {
		rispostaJSON(t, q, b.inviaFoto(q, []byte(pngPixel), nil), http.StatusNotFound, rifiutoCoda("Busta non trovata."))
		w, l := fotoDiretta(t, p.h, b, q, bytes.NewReader([]byte(pngPixel)), int64(len(pngPixel)), map[string]string{"Content-Type": "text/plain"})
		if w.Code != http.StatusNotFound || l.letti != 0 {
			t.Errorf("%s con text/plain: %d, letti %d", q, w.Code, l.letti)
		}
	}
	p.nienteScritto(t, "busta non trovata")
	if f, _ := p.b.ElencoFoto(t.Context(), cestino); len(f) != 0 {
		t.Error("foto sulla busta nel cestino")
	}
}

// inventario: RIP-S-35
// Un tipo diverso da JPEG, PNG o WebP (o nessun tipo) è 415 «Formato non supportato.» senza leggere
// il corpo; JPEG o WebP dichiarati passano la porta e poi decidono i byte (qui un PNG). Il tipo si
// legge come lo standalone: prima del «;», senza spazi, minuscolo.
func TestCodaFotoFormato(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := pronto(titolareDentro(t, p.h))
	for _, tipo := range []string{"text/plain", "image/gif", "image/svg+xml", "application/octet-stream", "-", "multipart/form-data; boundary=x", "image/png-x", "image"} {
		h := map[string]string{"Content-Type": tipo}
		if tipo == "-" {
			h = map[string]string{"-Content-Type": ""}
		}
		rispostaJSON(t, tipo, b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), h), http.StatusUnsupportedMediaType, rifiutoCoda("Formato non supportato."))
		w, l := fotoDiretta(t, p.h, b, percorsoCoda(p.busta), bytes.NewReader([]byte(pngPixel)), int64(len(pngPixel)), h)
		if w.Code != http.StatusUnsupportedMediaType || l.letti != 0 {
			t.Errorf("%s: %d, letti %d", tipo, w.Code, l.letti)
		}
	}
	p.nienteScritto(t, "formato")
	for _, tipo := range []string{"image/jpeg", "image/webp", " IMAGE/PNG ; charset=x"} {
		accolta(t, tipo, b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), map[string]string{"Content-Type": tipo}))
	}
	if len(p.foto(t)) != 3 {
		t.Errorf("foto: %d", len(p.foto(t)))
	}
}

// inventario: RIP-S-35
// Byte che non sono un'immagine: 422 «Foto rifiutata.», niente scritto (righe, audit, archivio,
// caricamenti); anche un corpo vuoto.
func TestCodaFotoNonImmagine(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := pronto(titolareDentro(t, p.h))
	rispostaJSON(t, "non un'immagine", b.inviaFoto(percorsoCoda(p.busta), []byte("not an image"), conNome("bad.png")), http.StatusUnprocessableEntity, rifiutoCoda("Foto rifiutata."))
	rispostaJSON(t, "vuoto", b.inviaFoto(percorsoCoda(p.busta), nil, conNome("empty.png")), http.StatusUnprocessableEntity, rifiutoCoda("Foto rifiutata."))
	p.nienteScritto(t, "non un'immagine")
}

// pngRumore è un PNG di rumore (che non si comprime) di l × a, deterministico.
func pngRumore(l, a int) []byte {
	g := rand.New(rand.NewPCG(39, 35))
	return sintetiche.PNGRighe(l, a, 2, 8, func(_ int, r []byte) {
		for i := range r {
			r[i] = byte(g.Uint32())
		}
	})
}

// inventario: RIP-S-35
// Una foto più lunga di una lettura del corpo (1 MiB) si salva intera, alla sua misura, come JPEG di
// base con il nome giusto.
func TestCodaFotoPiuLungaDiUnPezzo(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := pronto(titolareDentro(t, p.h))
	foto := pngRumore(720, 720)
	if len(foto) <= 1<<20 {
		t.Fatalf("foto di %d byte", len(foto))
	}
	accolta(t, "grande", b.inviaFoto(percorsoCoda(p.busta), foto, conNome("grande.png")))
	f := p.foto(t)
	if len(f) != 1 || f[0].TipoMime != "image/jpeg" || f[0].NomeFile != "grande.jpg" {
		t.Fatalf("foto: %+v", f)
	}
	j, err := os.ReadFile(filepath.Join(p.archivio.Cartella(), f[0].Chiave))
	if err != nil {
		t.Fatal(err)
	}
	if i, ok := sintetiche.LeggiJPEG(j); !ok || i.Larghezza != 720 || i.Altezza != 720 || !i.Base {
		t.Errorf("salvata: %+v", i)
	}
}

// inventario: RIP-S-35
// Oltre 25 MiB è 413 «Foto oltre il limite di 25 MiB.»: per la lunghezza dichiarata (senza leggere
// il corpo) o per i byte (anche se la lunghezza manca o mente: la lettura si ferma poco oltre il
// limite). Esattamente 25 MiB passa la porta e si giudica come immagine: spazzatura 422, una foto
// di quel peso si salva.
func TestCodaFotoOltreIlLimite(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := pronto(titolareDentro(t, p.h))
	oltre := rifiutoCoda("Foto oltre il limite di 25 MiB.")
	w, l := fotoDiretta(t, p.h, b, percorsoCoda(p.busta), bytes.NewReader([]byte(pngPixel)), imaging.MaxByteIngresso+1, nil)
	if w.Code != http.StatusRequestEntityTooLarge || w.Body.String() != oltre || l.letti != 0 {
		t.Errorf("dichiarata: %d %q, letti %d", w.Code, w.Body.String(), l.letti)
	}
	if w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Errorf("dichiarata: %q", w.Header().Get("Content-Type"))
	}
	troppo := bytes.Repeat([]byte("a"), imaging.MaxByteIngresso+1)
	for _, n := range []int64{-1, 1000} {
		// Senza lunghezza, o con una lunghezza che mente (in difetto: il gestore riceve tutto).
		w, l := fotoDiretta(t, p.h, b, percorsoCoda(p.busta), bytes.NewReader(troppo), n, nil)
		if w.Code != http.StatusRequestEntityTooLarge || w.Body.String() != oltre || l.letti > imaging.MaxByteIngresso+2<<20 {
			t.Errorf("byte (lunghezza %d): %d %q, letti %d", n, w.Code, w.Body.String(), l.letti)
		}
	}
	rispostaJSON(t, "dal browser", b.inviaFoto(percorsoCoda(p.busta), troppo, nil), http.StatusRequestEntityTooLarge, oltre)
	rispostaJSON(t, "al limite", b.inviaFoto(percorsoCoda(p.busta), troppo[:imaging.MaxByteIngresso], nil), http.StatusUnprocessableEntity, rifiutoCoda("Foto rifiutata."))
	p.nienteScritto(t, "oltre il limite")
	pesante := sintetiche.JPEG(sintetiche.Gradiente(1600, 1200, 1), sintetiche.OpzioniJPEG{Pad: imaging.MaxByteIngresso})
	if len(pesante) != imaging.MaxByteIngresso {
		t.Fatalf("pesante: %d byte", len(pesante))
	}
	h := conNome("heavy.jpg")
	h["Content-Type"] = "image/jpeg"
	accolta(t, "pesante", b.inviaFoto(percorsoCoda(p.busta), pesante, h))
	if f := p.foto(t); len(f) != 1 || f[0].TipoMime != "image/jpeg" {
		t.Errorf("pesante: %+v", f)
	}
}

// lettoreRotto dà i byte e poi un errore: un invio interrotto a metà.
type lettoreRotto struct{ r io.Reader }

func (l lettoreRotto) Read(p []byte) (int, error) {
	n, err := l.r.Read(p)
	if errors.Is(err, io.EOF) {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}

// inventario: RIP-S-35
// Un invio interrotto (il corpo finisce prima del dovuto): 400 «Invio interrotto.», niente scritto,
// il file a metà tolto.
func TestCodaFotoInvioInterrotto(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := pronto(titolareDentro(t, p.h))
	w, _ := fotoDiretta(t, p.h, b, percorsoCoda(p.busta), lettoreRotto{bytes.NewReader([]byte(pngPixel)[:20])}, -1, nil)
	if w.Code != http.StatusBadRequest || w.Body.String() != rifiutoCoda("Invio interrotto.") ||
		w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Errorf("interrotto: %d %q", w.Code, w.Body.String())
	}
	p.nienteScritto(t, "interrotto")
}

// inventario: RIP-S-35
// La sessione revocata mentre la foto si salva (dopo la normalizzazione, nella transazione): 401
// «Accesso scaduto.», nessuna riga, nessun resto nei caricamenti.
func TestCodaFotoSessioneRevocataDuranteIlSalvataggio(t *testing.T) {
	var p *provaCaricamento
	p = facciaCaricamento(t, normalizzaCon{prima: func() {
		eseguiSQL(t, p.d, `UPDATE utenti SET versione_sessione = versione_sessione + 1 WHERE email = 'banco@esempio.it'`)
	}})
	b := pronto(dipendenteDentro(t, p.h))
	rispostaJSON(t, "revocata", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), nil), http.StatusUnauthorized, rifiutoCoda("Accesso scaduto."))
	if len(p.foto(t)) != 0 || p.caricate(t) != 0 || !p.caricamentiVuoti(t) {
		t.Error("scritto qualcosa con la sessione revocata")
	}
}

// inventario: RIP-S-35
// Il figlio delle foto che non ce la fa adesso (D-48, domanda 7 della 037c): 503 con «Foto non
// elaborata adesso: riprova tra poco.», niente scritto; nel registro la causa, mai il nome, un
// percorso o l'indirizzo.
func TestCodaFotoErroreDiRisorse(t *testing.T) {
	p := facciaCaricamento(t, normalizzaCon{err: &imaging.ErroreRisorse{Causa: imaging.CausaTempo, Dettaglio: "10s"}})
	b := pronto(titolareDentro(t, p.h))
	rispostaJSON(t, "risorse", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), conNome("nome-riservato.png")),
		http.StatusServiceUnavailable, rifiutoCoda(buste.MsgFotoNonElaborata))
	p.nienteScritto(t, "risorse")
	reg := p.reg.String()
	if !strings.Contains(reg, "tempo") {
		t.Errorf("nel registro manca la causa: %s", reg)
	}
	for _, mai := range []string{"nome-riservato", p.caricamenti, p.dir, percorsoCoda(p.busta)} {
		if strings.Contains(reg, mai) {
			t.Errorf("nel registro c'è %q", mai)
		}
	}
}

// inventario: RIP-S-35
// Il registro non ha mai il nome della foto, un percorso dei caricamenti, l'indirizzo o la chiave
// salvata: né dopo un caricamento riuscito né dopo un rifiuto, un 413 o un invio interrotto.
func TestCodaFotoNienteNelRegistro(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := pronto(titolareDentro(t, p.h))
	accolta(t, "buona", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), conNome("nome-riservato.png")))
	b.inviaFoto(percorsoCoda(p.busta), []byte("x"), conNome("altro-riservato.png"))
	b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), map[string]string{"Content-Type": "image/gif", "X-File-Name": "terzo-riservato.gif"})
	fotoDiretta(t, p.h, b, percorsoCoda(p.busta), lettoreRotto{strings.NewReader("abc")}, -1, conNome("quarto-riservato.png"))
	f := p.foto(t)
	if len(f) != 1 {
		t.Fatalf("foto: %d", len(f))
	}
	reg := p.reg.String()
	for _, mai := range []string{"riservato", p.caricamenti, p.dir, f[0].Chiave, percorsoCoda(p.busta)} {
		if strings.Contains(reg, mai) {
			t.Errorf("nel registro c'è %q", mai)
		}
	}
}

// inventario: RIP-S-35
// Il file in arrivo sta nella cartella dei caricamenti, in una cartella privata della richiesta
// (0700), a 0600, con un nome che non è quello del browser, e si toglie sempre: riuscito, rifiutato,
// oltre il limite o interrotto.
func TestCodaFotoFileTemporanei(t *testing.T) {
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
	b := pronto(titolareDentro(t, p.h))
	accolta(t, "buona", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), conNome("nome-del-browser.png")))
	if len(visti) < 3 { // la cartella, quella della richiesta e il file
		t.Errorf("visti %v", visti)
	}
	if !p.caricamentiVuoti(t) {
		t.Error("resti dopo un caricamento riuscito")
	}
	b.inviaFoto(percorsoCoda(p.busta), []byte("x"), conNome("nome-del-browser.png"))
	b.inviaFoto(percorsoCoda(p.busta), bytes.Repeat([]byte("a"), imaging.MaxByteIngresso+1), nil)
	fotoDiretta(t, p.h, b, percorsoCoda(p.busta), lettoreRotto{strings.NewReader("abc")}, -1, nil)
	if !p.caricamentiVuoti(t) {
		t.Error("resti dopo i rifiuti")
	}
}

// inventario: RIP-S-35, ACC-05
// Il dipendente carica come il titolare (la rotta è di tutto il personale): 201 e la riga d'audit
// con il suo indirizzo email.
func TestCodaFotoDipendente(t *testing.T) {
	p := facciaCaricamento(t, nil)
	b := pronto(dipendenteDentro(t, p.h))
	accolta(t, "dipendente", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), nil))
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.photo_uploaded' AND attore = 'banco@esempio.it'`); n != 1 {
		t.Errorf("righe del dipendente: %d", n)
	}
}

// inventario: RIP-S-35
// Il tempo di lettura è quello di read_body nel controller dello standalone (read_timeout di 30 s
// per ogni lettura di 1 MiB), non il ReadTimeout del server su tutto il corpo: con un server vero dal
// ReadTimeout corto, un corpo lento ma costante arriva intero (e si giudica: qui non è una foto); un
// corpo fermo oltre il tempo del pezzo no, e niente si scrive.
func TestCodaFotoPezziLenti(t *testing.T) {
	p := facciaCaricamento(t, nil)
	// Un secondo per MiB, almeno 5 volte la pausa del corpo lento ma costante; il ReadTimeout del
	// server (300 ms) è più corto di ogni invio.
	srv, b := serverCoda(t, p, 300*time.Millisecond, func(f *facciaWeb) { f.tempoCoda = time.Second })
	invia := func(pausa time.Duration, pezzi, misura int) (risposta, error) {
		corpo := make([]byte, pezzi<<20)
		pr, pw := io.Pipe()
		go func() {
			for len(corpo) > 0 {
				k := min(len(corpo), misura)
				if _, err := pw.Write(corpo[:k]); err != nil {
					return
				}
				corpo = corpo[k:]
				time.Sleep(pausa)
			}
			_ = pw.Close()
		}()
		defer func() { _ = pr.Close() }() // il server ha già risposto: lo scrittore si ferma
		req, _ := http.NewRequest("POST", srv.URL+percorsoCoda(p.busta), pr)
		intestazioniCoda(req, b.token, nil)
		res, err := b.c.Do(req)
		if err != nil {
			return risposta{}, err
		}
		defer func() { _ = res.Body.Close() }()
		c, _ := io.ReadAll(res.Body) // solo per chiudere bene
		return risposta{res.StatusCode, "", string(c), res.Header}, nil
	}
	interrotto := rifiutoCoda("Invio interrotto.")
	// 4 pezzi da 1 MiB ogni 200 ms: 0,8 s, oltre il ReadTimeout di 300 ms; arriva tutto e si giudica.
	r, err := invia(200*time.Millisecond, 4, 1<<20)
	if err != nil || r.codice != http.StatusUnprocessableEntity || r.corpo != rifiutoCoda("Foto rifiutata.") {
		t.Fatalf("lento e costante: %v %d %q", err, r.codice, r.corpo)
	}
	// Fermo per 2,5 s dopo il primo MiB: oltre il tempo del pezzo, «Invio interrotto.».
	r, err = invia(2500*time.Millisecond, 2, 1<<20)
	if err != nil || r.codice != http.StatusBadRequest || r.corpo != interrotto {
		t.Errorf("corpo fermo: %v %d %q", err, r.codice, r.corpo)
	}
	// Il tempo è per ogni MiB, non per ogni pezzo di 64 000 byte (quello di LiveView): un MiB a
	// pezzi di 64 000 byte ogni 100 ms arriva in circa 1,7 s, oltre il tempo del pezzo.
	r, err = invia(100*time.Millisecond, 1, 64_000)
	if err != nil || r.codice != http.StatusBadRequest || r.corpo != interrotto {
		t.Errorf("un MiB troppo lento: %v %d %q", err, r.codice, r.corpo)
	}
	p.nienteScritto(t, "pezzi lenti")
}

// serverCoda è la faccia di p su un server TLS vero con il ReadTimeout dato, modificata da cambia,
// e un titolare dentro con il token di «Nuova riparazione».
func serverCoda(t *testing.T, p *provaCaricamento, readTimeout time.Duration, cambia func(*facciaWeb)) (*httptest.Server, *browser) {
	t.Helper()
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(p.d, p.o))
	dd.Orologio, dd.Limiti, dd.Buste, dd.Caricamenti = p.o, ratelimit.Nuovo(p.o), p.b, p.caricamenti
	h, _, err := costruisciInterna(dd, func(f *facciaWeb, _ *http.ServeMux) { cambia(f) })
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(h)
	srv.Config.ReadTimeout = readTimeout
	srv.StartTLS()
	t.Cleanup(srv.Close)
	b := browserSu(t, srv)
	if r := b.prova("titolare@esempio.it", pwProva); !redirectA(r, "/") {
		t.Fatalf("accesso: %d", r.codice)
	}
	return srv, pronto(b)
}

// inventario: RIP-S-35, RIP-S-29
// La prima scadenza per pezzo vale già prima del primo byte del corpo (della foto per la coda, del
// primo file per la scheda): un corpo che non arriva mai finisce dopo il tempo del pezzo, non dopo
// il ReadTimeout del server (qui 20 s).
func TestPrimaScadenzaPrimaDelPrimoByte(t *testing.T) {
	p := facciaCaricamento(t, nil)
	srv, b := serverCoda(t, p, 20*time.Second, func(f *facciaWeb) { f.tempoCoda, f.tempoPezzo = 400*time.Millisecond, 400*time.Millisecond })
	manda := func(percorso string, testa []byte, intest func(*http.Request)) (risposta, time.Duration) {
		pr, pw := io.Pipe()
		go func() { _, _ = pw.Write(testa) }() // poi niente: il corpo resta aperto
		defer func() { _ = pw.Close() }()
		req, _ := http.NewRequest("POST", srv.URL+percorso, pr)
		intest(req)
		inizio := time.Now()
		res, err := b.c.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", percorso, err)
		}
		defer func() { _ = res.Body.Close() }()
		c, _ := io.ReadAll(res.Body) // solo per chiudere bene
		return risposta{res.StatusCode, res.Header.Get("Location"), string(c), res.Header}, time.Since(inizio)
	}
	r, d := manda(percorsoCoda(p.busta), nil, func(req *http.Request) { intestazioniCoda(req, b.token, nil) })
	if r.codice != http.StatusBadRequest || r.corpo != rifiutoCoda("Invio interrotto.") || d > 10*time.Second {
		t.Errorf("coda: %d %q dopo %v", r.codice, r.corpo, d)
	}
	// La scheda: token e segno in testa, poi l'inizio della parte del file e niente più.
	b.vai("GET", percorsoScheda(p.busta), nil, nil)
	corpo, tipo := corpoMultipart(t, b.testaDelModulo(), []fileProva{png("gallery", "a.png")}, nil)
	testa := corpo[:bytes.Index(corpo, []byte("\x89PNG"))]
	r, d = manda(percorsoCaricamento(p.busta), testa, func(req *http.Request) { req.Header.Set("Content-Type", tipo) })
	if r.codice != http.StatusBadRequest || d > 10*time.Second {
		t.Errorf("scheda: %d %q dopo %v", r.codice, r.location, d)
	}
	p.nienteScritto(t, "corpo che non arriva")
}

// inventario: RIP-S-35, RIP-S-29
// I valori di produzione: una faccia costruita normalmente dà 30 s per ogni MiB della coda
// (read_timeout del controller) e 10 s per ogni pezzo di 64 000 byte della scheda (chunk_timeout).
func TestTempiDeiPezziDiProduzione(t *testing.T) {
	p := facciaCaricamento(t, nil)
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(p.d, p.o))
	dd.Orologio, dd.Limiti, dd.Buste, dd.Caricamenti = p.o, ratelimit.Nuovo(p.o), p.b, p.caricamenti
	var coda, scheda time.Duration
	if _, _, err := costruisciInterna(dd, func(f *facciaWeb, _ *http.ServeMux) { coda, scheda = f.tempoCoda, f.tempoPezzo }); err != nil {
		t.Fatal(err)
	}
	if coda != 30*time.Second || scheda != 10*time.Second || pezzoCoda != 1<<20 || dimensionePezzo != 64_000 {
		t.Errorf("coda %v, scheda %v, pezzo della coda %d, della scheda %d", coda, scheda, pezzoCoda, dimensionePezzo)
	}
}

// inventario: RIP-S-35
// Il file della richiesta non si può creare (al posto della cartella dei caricamenti c'è un file):
// 500 «Foto non salvata.» in JSON, come random_file/1 che fallisce, senza leggere il corpo; nel
// registro la causa senza il percorso.
func TestCodaFotoDiscoNonDisponibile(t *testing.T) {
	p := facciaCaricamento(t, nil)
	if err := os.WriteFile(p.caricamenti, []byte("non una cartella"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := pronto(titolareDentro(t, p.h))
	rispostaJSON(t, "disco", b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), nil), http.StatusInternalServerError, rifiutoCoda("Foto non salvata."))
	w, l := fotoDiretta(t, p.h, b, percorsoCoda(p.busta), bytes.NewReader([]byte(pngPixel)), int64(len(pngPixel)), nil)
	if w.Code != http.StatusInternalServerError || l.letti != 0 {
		t.Errorf("disco: %d, letti %d", w.Code, l.letti)
	}
	if len(p.foto(t)) != 0 || p.caricate(t) != 0 {
		t.Error("scritto qualcosa")
	}
	reg := p.reg.String()
	if !strings.Contains(reg, "foto in arrivo non scritta") || strings.Contains(reg, p.dir) {
		t.Errorf("registro: %s", reg)
	}
}

// busteConAllega sono le buste vere, ma AllegaFoto dà sempre err (dopo aver controllato che il file
// del caricamento ci sia).
type busteConAllega struct {
	*buste.Buste
	err error
}

func (b busteConAllega) AllegaFoto(_ context.Context, _ int64, cs []buste.Caricamento, _ *buste.Sessione, _ audit.Contesto) ([]buste.Foto, error) {
	if len(cs) != 1 {
		return nil, errors.New("caricamenti sbagliati")
	}
	if _, err := os.Stat(cs[0].Percorso); err != nil {
		return nil, err
	}
	return nil, b.err
}

// facciaConAllega è la faccia di p con AllegaFoto che dà err, e il registro di p.
func facciaConAllega(t *testing.T, p *provaCaricamento, err error) http.Handler {
	t.Helper()
	l := slog.New(slog.NewTextHandler(p.reg, &slog.HandlerOptions{Level: slog.LevelDebug}))
	dd := dipendenzeProva(&sondaFinta{pronto: true}, l, accounts.NuoviUtenti(p.d, p.o))
	dd.Orologio, dd.Limiti, dd.Buste, dd.Caricamenti = p.o, ratelimit.Nuovo(p.o), busteConAllega{p.b, err}, p.caricamenti
	return mustInterna(t, dd)
}

// inventario: RIP-S-35
// Gli esiti di AllegaFoto (D-50, D-51, Stefano 02/11/2026): 422 «Foto rifiutata.» solo per una foto
// non valida (byte che non sono un'immagine, caricamento non valido); un guasto del server
// (database, archivio che non scrive, archivio non configurato, contesto finito) è 500 «Foto non
// salvata.», che la coda tiene per «Riprova», con la causa nel registro e mai un percorso; la busta
// cestinata nel frattempo è 422 come lo standalone (il 404 è solo della porta), il file del
// caricamento che non si rilegge è un guasto (500), il figlio che non ce la fa 503, la sessione
// revocata 401.
func TestCodaFotoEsitiDelSalvataggio(t *testing.T) {
	guasto := errors.New("guasto inventato del database")
	for _, c := range []struct {
		nome   string
		err    error
		codice int
		msg    string
		causa  string // nel registro, "" se nessuna riga
	}{
		{"immagine", imaging.ErrImmagine, http.StatusUnprocessableEntity, "Foto rifiutata.", ""},
		{"caricamento", buste.ErrCaricamento, http.StatusUnprocessableEntity, "Foto rifiutata.", ""},
		{"troppe", buste.ErrTroppeFoto, http.StatusUnprocessableEntity, "Foto rifiutata.", ""},
		{"database", fmt.Errorf("riga: %w", guasto), http.StatusInternalServerError, "Foto non salvata.", "guasto inventato del database"},
		{"archivio", media.ErrSalvataggio, http.StatusInternalServerError, "Foto non salvata.", "salvataggio non riuscito"},
		{"senza archivio", buste.ErrSenzaArchivio, http.StatusInternalServerError, "Foto non salvata.", "archivio delle foto non configurato"},
		{"contesto", context.Canceled, http.StatusInternalServerError, "Foto non salvata.", "context canceled"},
		{"percorso", &os.PathError{Op: "open", Path: "/percorso/riservato/x", Err: os.ErrPermission}, http.StatusInternalServerError, "Foto non salvata.", "open: permission denied"},
		// La busta cestinata dopo il controllo della porta: answer(_refused) dello standalone.
		{"busta sparita", buste.ErrBustaNonTrovata, http.StatusUnprocessableEntity, "Foto rifiutata.", ""},
		{"file non riletto", errors.Join(buste.ErrFileCaricamento, &os.PathError{Op: "open", Path: "/percorso/riservato/0", Err: os.ErrPermission}),
			http.StatusInternalServerError, "Foto non salvata.", "open: permission denied"},
		{"risorse", &imaging.ErroreRisorse{Causa: imaging.CausaMemoria}, http.StatusServiceUnavailable, buste.MsgFotoNonElaborata, "memoria"},
		{"revocata", buste.ErrSessioneRevocata, http.StatusUnauthorized, "Accesso scaduto.", ""},
	} {
		p := facciaCaricamento(t, nil)
		h := facciaConAllega(t, p, c.err)
		b := pronto(titolareDentro(t, h))
		rispostaJSON(t, c.nome, b.inviaFoto(percorsoCoda(p.busta), []byte(pngPixel), conNome("nome-riservato.png")), c.codice, rifiutoCoda(c.msg))
		reg := p.reg.String()
		if c.causa == "" {
			if strings.Contains(reg, "level=ERROR") {
				t.Errorf("%s: riga d'errore nel registro per un rifiuto: %s", c.nome, reg)
			}
		} else if !strings.Contains(reg, c.causa) {
			t.Errorf("%s: nel registro manca la causa %q: %s", c.nome, c.causa, reg)
		}
		if c.codice == http.StatusInternalServerError && !strings.Contains(reg, `msg="foto non salvata"`) {
			t.Errorf("%s: nel registro manca la riga: %s", c.nome, reg)
		}
		for _, mai := range []string{"riservato", p.caricamenti, p.dir, percorsoCoda(p.busta)} {
			if strings.Contains(reg, mai) {
				t.Errorf("%s: nel registro c'è %q", c.nome, mai)
			}
		}
		if !p.caricamentiVuoti(t) {
			t.Errorf("%s: resti nei caricamenti", c.nome)
		}
	}
}

// normalizzaAnnullato annulla la richiesta durante la normalizzazione e dà l'errore del contesto,
// come imaging.Lavoratore quando il chiamante se ne va (il figlio si ferma).
type normalizzaAnnullato struct{ annulla func() }

func (n normalizzaAnnullato) Normalizza(ctx context.Context, _ []byte) ([]byte, error) {
	n.annulla()
	<-ctx.Done()
	return nil, ctx.Err()
}

// inventario: RIP-S-35
// Il client si scollega mentre la foto si normalizza: il Go non salva (lo standalone finisce il
// salvataggio; differenza dichiarata nella ricevuta). Non resta niente: né righe, né audit, né file
// nell'archivio, né nei caricamenti.
func TestCodaFotoClienteScollegato(t *testing.T) {
	ctx, annulla := context.WithCancel(context.Background())
	defer annulla()
	p := facciaCaricamento(t, normalizzaAnnullato{annulla: annulla})
	b := pronto(titolareDentro(t, p.h))
	req := httptest.NewRequestWithContext(ctx, "POST", "https://localhost"+percorsoCoda(p.busta), bytes.NewReader([]byte(pngPixel)))
	intestazioniCoda(req, b.token, conNome("scollegato.png"))
	u, _ := url.Parse(b.srv.URL)
	for _, k := range b.c.Jar.Cookies(u) {
		req.AddCookie(k)
	}
	w := httptest.NewRecorder()
	p.h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError || w.Body.String() != rifiutoCoda("Foto non salvata.") {
		t.Errorf("scollegato: %d %q", w.Code, w.Body.String())
	}
	p.nienteScritto(t, "scollegato")
}

// inventario: RIP-S-29
// Nella scheda (038) il file del caricamento che non si rilegge (D-51) e una foto che non è
// un'immagine danno lo stesso messaggio di sempre, «Foto rifiutata o non salvata.»; il guasto va nel
// registro, il rifiuto no.
func TestCaricamentoSchedaFileNonRiletto(t *testing.T) {
	for _, c := range []struct {
		nome string
		err  error
		riga bool
	}{
		{"file non riletto", errors.Join(buste.ErrFileCaricamento, &os.PathError{Op: "open", Path: "/percorso/riservato/0", Err: os.ErrPermission}), true},
		{"immagine", imaging.ErrImmagine, false},
	} {
		p := facciaCaricamento(t, nil)
		h := facciaConAllega(t, p, c.err)
		b := titolareDentro(t, h)
		b.vai("GET", percorsoScheda(p.busta), nil, nil)
		pagina := b.dopo(b.carica(percorsoCaricamento(p.busta), png("gallery", "a.png")))
		contiene(t, c.nome, pagina.corpo, `class="flash flash-errore">Foto rifiutata o non salvata.</div>`)
		reg := p.reg.String()
		if strings.Contains(reg, `msg="foto non salvata"`) != c.riga || strings.Contains(reg, "riservato") {
			t.Errorf("%s: registro %s", c.nome, reg)
		}
	}
}
