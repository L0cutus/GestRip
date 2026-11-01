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

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/imaging"
	"github.com/L0cutus/gestrip/internal/imaging/sintetiche"
	"github.com/L0cutus/gestrip/internal/media"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// La rotta delle foto del personale (RIP-S-33, RIP-S-34), con i casi di
// repair_photo_controller_test.exs e photo_thumbnail_assertions.ex dello standalone a go-baseline,
// su account, buste, archivio e cache veri. La normalizzazione e la miniatura girano nel processo
// del test (in produzione nel figlio, imaging.Lavoratore).

type normalizzaQui struct{}

func (normalizzaQui) Normalizza(_ context.Context, in []byte) ([]byte, error) {
	return imaging.Normalizza(in)
}

type miniaturaQui struct{}

func (miniaturaQui) Miniatura(_ context.Context, in []byte) ([]byte, error) {
	return imaging.Miniatura(in)
}

// pngPixel è il PNG di un pixel dei test dello standalone (@png).
const pngPixel = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde\x00\x00\x00\x0cIDATx\x9cc\xf8\x7f\x9d\x01\x00\x04\xaf\x01\xd7\xc0\xd0\x10>\x00\x00\x00\x00IEND\xaeB`\x82"

type provaFotoWeb struct {
	h        http.Handler
	d        *db.DB
	o        *clock.Finto
	b        *buste.Buste
	archivio *media.Archivio
	dir      string
	busta    int64
	foto     buste.Foto // il PNG di un pixel, normalizzato (la foto della busta)
	grande   buste.Foto // una foto 1000×750 normalizzata, con la sua miniatura
}

func facciaConFoto(t *testing.T) *provaFotoWeb {
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
	p := &provaFotoWeb{d: d, o: o, dir: dir}
	p.archivio = media.Nuovo(media.CartellaDi(filepath.Join(dir, "gestrip.db")))
	p.b = buste.Nuove(d, o).ConFoto(p.archivio, normalizzaQui{}).
		ConMiniature(media.NuoveMiniature(p.archivio, media.MiniatureDi(filepath.Join(dir, "gestrip.db")), miniaturaQui{}))
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti, dd.Buste = o, ratelimit.Nuovo(o), p.b
	p.h = mustInterna(t, dd)
	p.busta = bustaScheda(t, d, o, nil)
	p.foto = p.allega(t, "x.png", []byte(pngPixel))
	p.grande = p.allega(t, "big.png", sintetiche.PNG(1000, 750, 1))
	return p
}

func (p *provaFotoWeb) allega(t *testing.T, nome string, b []byte) buste.Foto {
	t.Helper()
	in := filepath.Join(p.dir, "in-"+nome)
	if err := os.WriteFile(in, b, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := p.b.AllegaFoto(context.Background(), p.busta, []buste.Caricamento{{Percorso: in, NomeOriginale: nome}}, nil,
		audit.Contesto{Attore: "titolare@esempio.it"})
	if err != nil || len(f) != 1 {
		t.Fatalf("AllegaFoto: %v", err)
	}
	return f[0]
}

// rigaFoto scrive a mano una riga di foto_buste per una chiave già nell'archivio (le foto salvate
// prima della slice 191, o byte che non sono un'immagine).
func (p *provaFotoWeb) rigaFoto(t *testing.T, b []byte, tipo string, posizione int64) int64 {
	t.Helper()
	k, err := p.archivio.Salva(b)
	if err != nil {
		t.Fatal(err)
	}
	h := mustImpronta(t, k)
	r, err := p.d.Scrittura.Exec(`INSERT INTO foto_buste (busta_id, posizione, chiave, nome_file, tipo_mime, byte, sha256, foto_busta,
		visibile_cliente, visibile_artigiano, visibile_oreficeria, creato_il, aggiornato_il)
		VALUES (?, ?, ?, 'before.png', ?, ?, ?, 0, 0, 0, 0, '2026-10-31T09:00:00.000000Z', '2026-10-31T09:00:00.000000Z')`,
		p.busta, posizione, k, tipo, len(b), h)
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustImpronta(t *testing.T, k string) []byte {
	t.Helper()
	b := make([]byte, 32)
	for i := range b {
		n, err := strconv.ParseUint(k[2*i:2*i+2], 16, 8)
		if err != nil {
			t.Fatal(err)
		}
		b[i] = byte(n)
	}
	return b
}

func percorsoFoto(busta, foto int64) string {
	return "/repairs/" + strconv.FormatInt(busta, 10) + "/photos/" + strconv.FormatInt(foto, 10)
}

func miniatura(p string) string { return p + "?size=thumb" }

func (p *provaFotoWeb) visti(t *testing.T) int {
	t.Helper()
	return contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.staff_photo_viewed'`)
}

func (p *provaFotoWeb) idUtente(t *testing.T, email string) int64 {
	t.Helper()
	var id int64
	if err := p.d.Lettura.QueryRow(`SELECT id FROM utenti WHERE email = ?`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// unaVolta dice se l'intestazione k c'è una volta sola con il valore v.
func unaVolta(r risposta, k, v string) bool {
	got := r.h.Values(k)
	return len(got) == 1 && got[0] == v
}

// controllaIntera: la foto intera è il JPEG salvato, no-store, x-photo-variant full, con le
// intestazioni delle pagine e no-referrer.
func controllaIntera(t *testing.T, cosa string, r risposta) {
	t.Helper()
	if r.codice != 200 || !unaVolta(r, "Content-Type", "image/jpeg") || !unaVolta(r, "X-Photo-Variant", "full") ||
		!unaVolta(r, "Cache-Control", "no-store") || !unaVolta(r, "Referrer-Policy", "no-referrer") ||
		!unaVolta(r, "X-Content-Type-Options", "nosniff") || !strings.HasPrefix(r.corpo, "\xff\xd8\xff") {
		t.Errorf("%s: %d %v", cosa, r.codice, r.h)
	}
}

// stessoRifiuto è assert_same_refusal/2: il rifiuto della miniatura è quello della foto, byte per
// byte: 404 «Foto non disponibile», no-store, no-referrer, niente x-photo-variant, nessun tipo.
func stessoRifiuto(t *testing.T, cosa string, intera, mini risposta) {
	t.Helper()
	for _, r := range []risposta{intera, mini} {
		if r.codice != 404 || r.corpo != "Foto non disponibile" || !unaVolta(r, "Cache-Control", "no-store") ||
			!unaVolta(r, "Referrer-Policy", "no-referrer") || len(r.h.Values("X-Photo-Variant")) != 0 ||
			!unaVolta(r, "X-Content-Type-Options", "nosniff") {
			t.Errorf("%s: %d %q %v", cosa, r.codice, r.corpo, r.h)
		}
	}
	// Come lo standalone (misurato sul banco): né tipo né lunghezza, il corpo parte a pezzi.
	for _, r := range []risposta{intera, mini} {
		if len(r.h.Values("Content-Type")) != 0 || len(r.h.Values("Content-Length")) != 0 {
			t.Errorf("%s: Content-Type %v, Content-Length %v", cosa, r.h.Values("Content-Type"), r.h.Values("Content-Length"))
		}
	}
}

// inventario: RIP-S-33
// Il personale riceve la foto intera, verificata con la sua impronta, no-store e no-referrer, con
// una riga d'audit senza la chiave; titolare e dipendente allo stesso modo.
func TestFotoInteraDelPersonale(t *testing.T) {
	p := facciaConFoto(t)
	salvati, err := os.ReadFile(filepath.Join(p.archivio.Cartella(), p.foto.Chiave))
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []*browser{titolareDentro(t, p.h), dipendenteDentro(t, p.h)} {
		r := b.vai("GET", percorsoFoto(p.busta, p.foto.ID), nil, nil)
		controllaIntera(t, "foto", r)
		if r.corpo != string(salvati) {
			t.Error("byte diversi da quelli salvati")
		}
	}
	if n := contaCon(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.staff_photo_viewed' AND esito = 'ok'
		AND risorsa = 'repair' AND id_risorsa = ? AND dati = ?`, strconv.FormatInt(p.busta, 10), `{"photo_id":`+strconv.FormatInt(p.foto.ID, 10)+`}`); n != 2 {
		t.Errorf("righe d'audit: %d", n)
	}
	if n := contaCon(t, p.d, `SELECT count(*) FROM eventi_audit WHERE dati LIKE ?`, "%"+p.foto.Chiave+"%"); n != 0 {
		t.Error("la chiave è nell'audit")
	}
}

// inventario: RIP-S-33
// Una foto salvata come PNG prima della slice 191 dello standalone si serve ancora come PNG.
func TestFotoPNGDiPrima(t *testing.T) {
	p := facciaConFoto(t)
	id := p.rigaFoto(t, []byte(pngPixel), "image/png", 9)
	r := titolareDentro(t, p.h).vai("GET", percorsoFoto(p.busta, id), nil, nil)
	if r.codice != 200 || r.corpo != pngPixel || !unaVolta(r, "Content-Type", "image/png") {
		t.Errorf("%d %v", r.codice, r.h)
	}
}

// inventario: RIP-S-33
// Serve una sessione del personale attiva: senza accesso, o con l'account disattivato, si va a
// /login, per la foto e per la miniatura; nessuna rotta pubblica delle foto delle buste.
func TestFotoSoloConLaSessione(t *testing.T) {
	p := facciaConFoto(t)
	percorso := percorsoFoto(p.busta, p.grande.ID)
	for _, q := range []string{percorso, miniatura(percorso)} {
		if r := nuovoBrowser(t, p.h).vai("GET", q, nil, nil); !redirectA(r, "/login") {
			t.Errorf("%s senza accesso: %d %q", q, r.codice, r.location)
		}
	}
	b := dipendenteDentro(t, p.h)
	eseguiSQL(t, p.d, `UPDATE utenti SET attivo = 0 WHERE email = 'banco@esempio.it'`)
	for _, q := range []string{percorso, miniatura(percorso)} {
		if r := b.vai("GET", q, nil, nil); !redirectA(r, "/login") {
			t.Errorf("%s da disattivato: %d %q", q, r.codice, r.location)
		}
	}
	if p.visti(t) != 0 {
		t.Error("audit senza sessione")
	}
}

// inventario: RIP-S-33
// Una foto manomessa (impronta diversa) e una foto tolta danno il rifiuto, senza byte; una busta
// nel cestino anche (Repo.get dello standalone la esclude).
func TestFotoManomessaToltaONelCestino(t *testing.T) {
	p := facciaConFoto(t)
	b := titolareDentro(t, p.h)
	percorso := percorsoFoto(p.busta, p.foto.ID)
	file := filepath.Join(p.archivio.Cartella(), p.foto.Chiave)
	if err := os.Chmod(file, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	stessoRifiuto(t, "manomessa", b.vai("GET", percorso, nil, nil), b.vai("GET", miniatura(percorso), nil, nil))
	if err := p.b.RimuoviFoto(context.Background(), p.busta, p.foto.ID, audit.Contesto{Attore: "titolare@esempio.it"}); err != nil {
		t.Fatal(err)
	}
	stessoRifiuto(t, "tolta", b.vai("GET", percorso, nil, nil), b.vai("GET", miniatura(percorso), nil, nil))
	grande := percorsoFoto(p.busta, p.grande.ID)
	eseguiSQL(t, p.d, `UPDATE buste SET cancellata_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, p.busta)
	stessoRifiuto(t, "cestino", b.vai("GET", grande, nil, nil), b.vai("GET", miniatura(grande), nil, nil))
	if n := contaCon(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.staff_photo_viewed' AND esito = 'denied'
		AND id_risorsa = ?`, strconv.FormatInt(p.busta, 10)); n != 6 {
		t.Errorf("rifiuti nell'audit: %d", n)
	}
}

// inventario: RIP-S-33
// Id malformati, di un'altra busta o che non ci sono: lo stesso rifiuto per foto e miniatura, ognuno
// con la sua riga «denied» con l'id della busta o «invalid», mai l'input ostile.
func TestFotoIdMalformati(t *testing.T) {
	p := facciaConFoto(t)
	b := titolareDentro(t, p.h)
	ostile := "%3Cscript%3Ealert(1)%3C%2Fscript%3E"
	busta := strconv.FormatInt(p.busta, 10)
	foto := strconv.FormatInt(p.grande.ID, 10)
	percorsi := []string{
		"/repairs/" + busta + "/photos/0",
		"/repairs/" + busta + "/photos/-1",
		"/repairs/" + busta + "/photos/99999999999999999999999999999",
		"/repairs/" + busta + "/photos/" + ostile,
		"/repairs/" + busta + "/photos/invalid",
		"/repairs/" + busta + "/photos/" + strconv.FormatInt(p.grande.ID+100, 10),
		"/repairs/0/photos/" + foto,
		"/repairs/" + ostile + "/photos/" + foto,
		"/repairs/" + strconv.FormatInt(p.busta+1, 10) + "/photos/" + foto,
	}
	for _, q := range percorsi {
		stessoRifiuto(t, q, b.vai("GET", q, nil, nil), b.vai("GET", miniatura(q), nil, nil))
	}
	righe, err := p.d.Lettura.Query(`SELECT id_risorsa, dati FROM eventi_audit WHERE azione = 'repair.staff_photo_viewed' AND esito = 'denied'`)
	if err != nil {
		t.Fatal(err)
	}
	defer righe.Close()
	n := 0
	for righe.Next() {
		var id, dati string
		if err := righe.Scan(&id, &dati); err != nil {
			t.Fatal(err)
		}
		n++
		if id != busta && id != "invalid" && id != strconv.FormatInt(p.busta+1, 10) {
			t.Errorf("id_risorsa %q", id)
		}
		for _, mai := range []string{"script", "99999999999999999999999999999", "-1"} {
			if strings.Contains(dati, mai) {
				t.Errorf("dati con l'input: %s", dati)
			}
		}
	}
	if n != 2*len(percorsi) {
		t.Errorf("righe denied %d, attese %d", n, 2*len(percorsi))
	}
	if got := ultimoAuditWeb(t, p.d, "repair.staff_photo_viewed"); got != `invalid|denied|{"photo_id":`+foto+`,"variant":"thumb"}` &&
		got != strconv.FormatInt(p.busta+1, 10)+`|denied|{"photo_id":`+foto+`,"variant":"thumb"}` {
		t.Errorf("ultima riga: %s", got)
	}
}

func ultimoAuditWeb(t *testing.T, d *db.DB, azione string) string {
	t.Helper()
	var id, esito, dati string
	if err := d.Lettura.QueryRow(`SELECT id_risorsa, esito, dati FROM eventi_audit WHERE azione = ? ORDER BY id DESC LIMIT 1`, azione).
		Scan(&id, &esito, &dati); err != nil {
		t.Fatal(err)
	}
	return id + "|" + esito + "|" + dati
}

// inventario: RIP-S-33
// Con ?size=thumb il personale riceve un WebP molto più piccolo che sta in 400 px, in cache nel
// browser per un giorno (private, max-age=86400); la foto salvata non si tocca e l'archivio non
// cambia.
func TestMiniaturaDelPersonale(t *testing.T) {
	p := facciaConFoto(t)
	salvati, err := os.ReadFile(filepath.Join(p.archivio.Cartella(), p.grande.Chiave))
	if err != nil {
		t.Fatal(err)
	}
	b := titolareDentro(t, p.h)
	percorso := percorsoFoto(p.busta, p.grande.ID)
	intera := b.vai("GET", percorso, nil, nil)
	mini := b.vai("GET", miniatura(percorso), nil, nil)
	controllaIntera(t, "intera", intera)
	if mini.codice != 200 || !unaVolta(mini, "Content-Type", "image/webp") || !unaVolta(mini, "X-Photo-Variant", "thumb") ||
		!unaVolta(mini, "Cache-Control", "private, max-age=86400") || !unaVolta(mini, "Referrer-Policy", "no-referrer") ||
		!unaVolta(mini, "X-Content-Type-Options", "nosniff") {
		t.Errorf("miniatura: %d %v", mini.codice, mini.h)
	}
	if l, a, ok := sintetiche.DimensioniWebP([]byte(mini.corpo)); !ok || max(l, a) > 400 || len(mini.corpo)*10 >= len(intera.corpo) {
		t.Errorf("miniatura %d×%d di %d byte, foto di %d", l, a, len(mini.corpo), len(intera.corpo))
	}
	if intera.corpo != string(salvati) {
		t.Error("foto intera diversa da quella salvata")
	}
	dopo, err := os.ReadFile(filepath.Join(p.archivio.Cartella(), p.grande.Chiave))
	if err != nil || string(dopo) != string(salvati) {
		t.Error("la foto salvata è cambiata")
	}
	voci, err := os.ReadDir(p.archivio.Cartella())
	if err != nil || len(voci) != 2 {
		t.Errorf("archivio: %d voci %v", len(voci), err)
	}
	if _, err := os.Lstat(filepath.Join(p.dir, media.CartellaMiniature, p.grande.Chiave+".webp")); err != nil {
		t.Errorf("miniatura non in cache accanto al database: %v", err)
	}
}

// inventario: RIP-S-33
// Solo size=thumb esatto chiede la miniatura (come i parametri di Plug: vale l'ultima
// assegnazione, chiave e valore decodificati); ogni altra richiesta è la foto intera.
func TestSoloSizeThumbEsatto(t *testing.T) {
	p := facciaConFoto(t)
	b := titolareDentro(t, p.h)
	percorso := percorsoFoto(p.busta, p.grande.ID)
	for _, q := range []string{"", "?size=full", "?size=THUMB", "?size=thumb%20", "?size=thumb+", "?size=", "?size[]=thumb",
		"?thumb=1", "?size=thumb&size=full", "?Size=thumb", "?size", "?size=thumb;"} {
		r := b.vai("GET", percorso+q, nil, nil)
		if r.codice != 200 || !unaVolta(r, "X-Photo-Variant", "full") || !unaVolta(r, "Content-Type", "image/jpeg") {
			t.Errorf("%q: %d %v", q, r.codice, r.h)
		}
	}
	for _, q := range []string{"?size=thumb", "?size=full&size=thumb", "?si%7Ae=thumb", "?size=th%75mb", "?a=1&size=thumb"} {
		r := b.vai("GET", percorso+q, nil, nil)
		if r.codice != 200 || !unaVolta(r, "X-Photo-Variant", "thumb") {
			t.Errorf("%q: %d %v", q, r.codice, r.h)
		}
	}
}

// inventario: RIP-S-33
// Una foto da cui non si fa la miniatura risponde con la foto intera e lo dice: x-photo-variant
// full, il tipo della riga per byte senza firma, no-store, e nell'audit «variant: full».
func TestMiniaturaCheNonSiFa(t *testing.T) {
	p := facciaConFoto(t)
	id := p.rigaFoto(t, []byte("these bytes are not an image"), "image/png", 9)
	r := titolareDentro(t, p.h).vai("GET", miniatura(percorsoFoto(p.busta, id)), nil, nil)
	if r.codice != 200 || r.corpo != "these bytes are not an image" || !unaVolta(r, "X-Photo-Variant", "full") ||
		!unaVolta(r, "Content-Type", "image/png") || !unaVolta(r, "Cache-Control", "no-store") {
		t.Errorf("%d %q %v", r.codice, r.corpo, r.h)
	}
	if got := ultimoAuditWeb(t, p.d, "repair.staff_photo_viewed"); got != strconv.FormatInt(p.busta, 10)+`|ok|{"photo_id":`+strconv.FormatInt(id, 10)+`,"variant":"full"}` {
		t.Errorf("audit: %s", got)
	}
}

// inventario: RIP-S-33
// Ogni lettura scrive la sua riga: la miniatura con «variant: thumb», la foto intera come prima;
// il rifiuto di una miniatura dice «variant: thumb».
func TestAuditDelleLetture(t *testing.T) {
	p := facciaConFoto(t)
	b := titolareDentro(t, p.h)
	percorso := percorsoFoto(p.busta, p.grande.ID)
	b.vai("GET", miniatura(percorso), nil, nil)
	b.vai("GET", percorso, nil, nil)
	foto := strconv.FormatInt(p.grande.ID, 10)
	busta := strconv.FormatInt(p.busta, 10)
	for _, dati := range []string{`{"photo_id":` + foto + `,"variant":"thumb"}`, `{"photo_id":` + foto + `}`} {
		if n := contaCon(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.staff_photo_viewed' AND esito = 'ok'
			AND attore = 'titolare@esempio.it' AND id_risorsa = ? AND dati = ?`, busta, dati); n != 1 {
			t.Errorf("%s: %d righe", dati, n)
		}
	}
	b.vai("GET", "/repairs/"+busta+"/photos/invalid?size=thumb", nil, nil)
	if got := ultimoAuditWeb(t, p.d, "repair.staff_photo_viewed"); got != busta+`|denied|{"photo_id":"invalid","variant":"thumb"}` {
		t.Errorf("rifiuto: %s", got)
	}
}

// inventario: RIP-S-33
// Una miniatura con il gettone della galleria (utente, busta e foto giusti, firmato all'inizio del
// giorno UTC) si serve senza una riga sua: la galleria ne ha scritta una per tutte.
func TestMiniaturaConIlGettoneDellaGalleria(t *testing.T) {
	p := facciaConFoto(t)
	b := titolareDentro(t, p.h)
	g := gettoneGalleria{chiave: dipendenzeProva(nil, nil, nil).ChiaveGalleria}
	tok := g.firma(p.idUtente(t, "titolare@esempio.it"), p.busta, p.grande.ID, inizioGiorno(p.o.Adesso()))
	prima := p.visti(t)
	r := b.vai("GET", miniatura(percorsoFoto(p.busta, p.grande.ID))+"&g="+url.QueryEscape(tok), nil, nil)
	if r.codice != 200 || !unaVolta(r, "X-Photo-Variant", "thumb") || !unaVolta(r, "Content-Type", "image/webp") ||
		!unaVolta(r, "Cache-Control", "private, max-age=86400") {
		t.Errorf("%d %v", r.codice, r.h)
	}
	if p.visti(t) != prima {
		t.Error("una riga d'audit per una miniatura coperta")
	}
}

// inventario: RIP-S-33
// Ogni altro gettone (di un altro utente, di un'altra foto o busta, scaduto, falso, vuoto,
// ripetuto come lista) e la foto intera con un gettone buono scrivono la riga come prima.
func TestAltriGettoniScrivonoLaRiga(t *testing.T) {
	p := facciaConFoto(t)
	b := titolareDentro(t, p.h)
	g := gettoneGalleria{chiave: dipendenzeProva(nil, nil, nil).ChiaveGalleria}
	io, altro := p.idUtente(t, "titolare@esempio.it"), p.idUtente(t, "banco@esempio.it")
	oggi := inizioGiorno(p.o.Adesso())
	percorso := miniatura(percorsoFoto(p.busta, p.grande.ID))
	altraChiave := gettoneGalleria{chiave: []byte("un'altra-chiave-della-galleria-lunga-32-byte")}
	for _, q := range []string{
		"&g=" + url.QueryEscape(g.firma(altro, p.busta, p.grande.ID, oggi)),
		"&g=" + url.QueryEscape(g.firma(io, p.busta, p.foto.ID, oggi)),
		"&g=" + url.QueryEscape(g.firma(io, p.busta+1, p.grande.ID, oggi)),
		"&g=" + url.QueryEscape(g.firma(io, p.busta, p.grande.ID, p.o.Adesso().Add(-3*24*time.Hour))),
		"&g=" + url.QueryEscape(altraChiave.firma(io, p.busta, p.grande.ID, oggi)),
		"&g[]=" + url.QueryEscape(g.firma(io, p.busta, p.grande.ID, oggi)),
		"&g=forged",
		"&g=",
	} {
		prima := p.visti(t)
		if r := b.vai("GET", percorso+q, nil, nil); r.codice != 200 {
			t.Errorf("%s: %d", q, r.codice)
		}
		if p.visti(t) != prima+1 {
			t.Errorf("%s: nessuna riga", q)
		}
	}
	prima := p.visti(t)
	r := b.vai("GET", percorsoFoto(p.busta, p.grande.ID)+"?g="+url.QueryEscape(g.firma(io, p.busta, p.grande.ID, oggi)), nil, nil)
	if !unaVolta(r, "X-Photo-Variant", "full") || p.visti(t) != prima+1 {
		t.Errorf("intera con il gettone: %v, righe %d", r.h, p.visti(t)-prima)
	}
}

// inventario: RIP-S-33
// La miniatura che non si fa, con un gettone buono, ripiega sulla foto intera e scrive la riga;
// un gettone non apre una foto sotto un'altra busta.
func TestGettoneConRipiegoOAltraBusta(t *testing.T) {
	p := facciaConFoto(t)
	b := titolareDentro(t, p.h)
	g := gettoneGalleria{chiave: dipendenzeProva(nil, nil, nil).ChiaveGalleria}
	io := p.idUtente(t, "titolare@esempio.it")
	oggi := inizioGiorno(p.o.Adesso())
	id := p.rigaFoto(t, []byte("not an image, no preview"), "image/png", 8)
	prima := p.visti(t)
	r := b.vai("GET", miniatura(percorsoFoto(p.busta, id))+"&g="+url.QueryEscape(g.firma(io, p.busta, id, oggi)), nil, nil)
	if !unaVolta(r, "X-Photo-Variant", "full") || !unaVolta(r, "Cache-Control", "no-store") || p.visti(t) != prima+1 {
		t.Errorf("ripiego: %v, righe %d", r.h, p.visti(t)-prima)
	}
	altra := p.busta + 1
	r = b.vai("GET", miniatura(percorsoFoto(altra, p.grande.ID))+"&g="+url.QueryEscape(g.firma(io, altra, p.grande.ID, oggi)), nil, nil)
	if r.codice != 404 || !unaVolta(r, "Cache-Control", "no-store") {
		t.Errorf("altra busta: %d %v", r.codice, r.h)
	}
}

// inventario: RIP-S-33
// Il gettone vale due giorni dalla firma (la giornata e quella della cache del browser), firmato
// all'inizio del giorno UTC così l'indirizzo della miniatura resta lo stesso tutto il giorno.
func TestGettoneDueGiorni(t *testing.T) {
	g := gettoneGalleria{chiave: []byte("sottochiave-galleria-di-prova-lunga-32-byte")}
	firma := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	if got := inizioGiorno(time.Date(2026, 10, 31, 23, 59, 59, 0, time.FixedZone("CET", 3600))); !got.Equal(time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("inizio del giorno %v", got)
	}
	tok := g.firma(1, 2, 3, firma)
	if tok != g.firma(1, 2, 3, firma) {
		t.Error("lo stesso gettone non è stabile")
	}
	for _, c := range []struct {
		adesso time.Time
		ok     bool
	}{{firma, true}, {firma.Add(48 * time.Hour), true}, {firma.Add(48*time.Hour + time.Millisecond), false}, {firma.Add(-time.Hour), true}} {
		if got := g.valido(tok, 1, 2, 3, c.adesso); got != c.ok {
			t.Errorf("%v: %v", c.adesso, got)
		}
	}
	for _, c := range [][3]int64{{2, 2, 3}, {1, 3, 3}, {1, 2, 4}} {
		if g.valido(tok, c[0], c[1], c[2], firma) {
			t.Errorf("%v: valido", c)
		}
	}
	if g.valido(tok[:len(tok)-1], 1, 2, 3, firma) || g.valido("", 1, 2, 3, firma) || g.valido(tok+"x", 1, 2, 3, firma) {
		t.Error("gettone guasto valido")
	}
}

// inventario: RIP-S-33
// Una lettura che non si registra nell'audit non dà byte (un errore SQL è un 500, come
// l'eccezione dello standalone).
func TestFotoSenzaAuditNonSiServe(t *testing.T) {
	p := facciaConFoto(t)
	b := titolareDentro(t, p.h)
	eseguiSQL(t, p.d, `CREATE TRIGGER ferma_foto BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.staff_photo_viewed'
		BEGIN SELECT RAISE(ABORT, 'no'); END`)
	r := b.vai("GET", percorsoFoto(p.busta, p.grande.ID), nil, nil)
	if r.codice != 500 || strings.HasPrefix(r.corpo, "\xff\xd8") || len(r.h.Values("X-Photo-Variant")) != 0 {
		t.Errorf("%d %v", r.codice, r.h)
	}
}

// contaCon è contaRighe con gli argomenti della query.
func contaCon(t *testing.T, d *db.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := d.Lettura.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// inventario: RIP-S-33
// Una foto sostituita sul disco con un'altra immagine valida (impronta diversa dal nome), senza
// miniatura in cache: anche la miniatura è il rifiuto «denied», come media_for/2 dello standalone
// (thumbnail/1 legge con read_object/1, poi la foto intera fallisce allo stesso modo).
func TestMiniaturaDiUnaFotoSostituita(t *testing.T) {
	p := facciaConFoto(t)
	b := titolareDentro(t, p.h)
	file := filepath.Join(p.archivio.Cartella(), p.grande.Chiave)
	if err := os.Chmod(file, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, sintetiche.PNG(300, 200, 7), 0o600); err != nil {
		t.Fatal(err)
	}
	percorso := percorsoFoto(p.busta, p.grande.ID)
	stessoRifiuto(t, "sostituita", b.vai("GET", percorso, nil, nil), b.vai("GET", miniatura(percorso), nil, nil))
	if got := ultimoAuditWeb(t, p.d, "repair.staff_photo_viewed"); got != strconv.FormatInt(p.busta, 10)+`|denied|{"photo_id":`+strconv.FormatInt(p.grande.ID, 10)+`,"variant":"thumb"}` {
		t.Errorf("audit: %s", got)
	}
	if _, err := os.Lstat(filepath.Join(p.dir, media.CartellaMiniature, p.grande.Chiave+".webp")); err == nil {
		t.Error("miniatura in cache di una foto non verificata")
	}
}

// inventario: RIP-S-33
// Il tipo si legge dai byte (image_type/2): una riga «image/png» con byte JPEG si serve image/jpeg.
func TestTipoDaiByteNonDallaRiga(t *testing.T) {
	p := facciaConFoto(t)
	j := sintetiche.JPEG(sintetiche.Gradiente(40, 30, 2), sintetiche.OpzioniJPEG{})
	id := p.rigaFoto(t, j, "image/png", 9)
	r := titolareDentro(t, p.h).vai("GET", percorsoFoto(p.busta, id), nil, nil)
	if r.codice != 200 || !unaVolta(r, "Content-Type", "image/jpeg") || r.corpo != string(j) {
		t.Errorf("%d %v", r.codice, r.h)
	}
}
