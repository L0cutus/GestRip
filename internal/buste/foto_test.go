package buste

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/imaging"
	"github.com/L0cutus/gestrip/internal/imaging/sintetiche"
	"github.com/L0cutus/gestrip/internal/media"
)

// I casi di repair_photos_test.exs dello standalone (go-baseline) per il dominio delle foto.

// png1 è il PNG di un pixel dei test dello standalone (@png).
var png1, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGP4f50BAASvAdfA0BA+AAAAAElFTkSuQmCC")

// nelProcesso è imaging.Normalizza senza il figlio, per i test.
type nelProcesso struct{}

func (nelProcesso) Normalizza(_ context.Context, in []byte) ([]byte, error) {
	return imaging.Normalizza(in)
}

type provaFoto struct {
	b       *Buste
	dir     string
	mediaDi string
	input   string
	busta   int64
}

func conFoto(t *testing.T) (*Buste, *provaFoto) {
	t.Helper()
	b, _, _ := prova(t)
	dir := t.TempDir()
	p := &provaFoto{b: b, dir: dir, mediaDi: filepath.Join(dir, media.Cartella)}
	b.ConFoto(media.Nuovo(p.mediaDi), nelProcesso{})
	p.input = scriviFile(t, dir, "input.png", png1)
	p.busta = crea(t, b, modulo()).ID
	return b, p
}

func scriviFile(t *testing.T, dir, nome string, b []byte) string {
	t.Helper()
	p := filepath.Join(dir, nome)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func (p *provaFoto) due() []Caricamento {
	return []Caricamento{{p.input, "busta.png"}, {p.input, "oggetto.png"}}
}

func allega(t *testing.T, b *Buste, id int64, cs []Caricamento) []Foto {
	t.Helper()
	f, err := b.AllegaFoto(ctx, id, cs, nil, chi)
	if err != nil {
		t.Fatalf("AllegaFoto: %v", err)
	}
	return f
}

func vis(f Foto) [3]bool { return [3]bool{f.Cliente, f.Artigiano, f.Oreficeria} }

// inventario: RIP-S-29, RIP-S-31
// Al più due foto per volta, in ordine stabile; la prima di una busta senza foto è la foto della
// busta; le visibilità iniziali di una busta diretta; stesso contenuto, stesso oggetto. Con tre
// foto niente si scrive.
func TestAllegaDueFotoConIValoriIniziali(t *testing.T) {
	b, p := conFoto(t)
	cs := []Caricamento{{p.input, "camera.png"}, {p.input, "gallery.PNG"}}
	f := allega(t, b, p.busta, cs)
	if len(f) != 2 || f[0].ID == f[1].ID {
		t.Fatalf("%+v", f)
	}
	borsa, dopo := f[0], f[1]
	if borsa.Posizione != 1 || dopo.Posizione != 2 || !borsa.FotoBusta || dopo.FotoBusta {
		t.Errorf("posizioni e foto della busta: %+v %+v", borsa, dopo)
	}
	if vis(borsa) != [3]bool{true, false, false} || vis(dopo) != [3]bool{true, true, false} {
		t.Errorf("visibilità %v %v", vis(borsa), vis(dopo))
	}
	if borsa.Chiave != dopo.Chiave || !regexp.MustCompile(`\A[0-9a-f]{64}\z`).MatchString(borsa.Chiave) {
		t.Errorf("chiavi %q %q", borsa.Chiave, dopo.Chiave)
	}
	if _, err := b.AllegaFoto(ctx, p.busta, append(cs, cs[0]), nil, chi); !errors.Is(err, ErrTroppeFoto) {
		t.Errorf("tre foto: %v", err)
	}
	el, err := b.ElencoFoto(ctx, p.busta)
	if err != nil || len(el) != 2 || el[0].ID != borsa.ID || el[1].ID != dopo.ID {
		t.Errorf("elenco %+v %v", el, err)
	}
	// Una busta con foto: la prossima non è la foto della busta, e la posizione continua.
	f = allega(t, b, p.busta, cs[:1])
	if f[0].FotoBusta || f[0].Posizione != 3 || vis(f[0]) != [3]bool{true, true, false} {
		t.Errorf("terza: %+v", f[0])
	}
	if a := ultimoAudit(t, p.b.d, "repair.photo_uploaded"); a != "repair.photo_uploaded|repair|"+itoa(p.busta)+"|ok|{\"photo_id\":"+itoa(f[0].ID)+",\"position\":3}" {
		t.Errorf("audit %q", a)
	}
}

// inventario: RIP-S-31
// Busta esterna: oreficeria sì, artigiano sì (la foto della busta no), cliente mai; orefice e
// artigiano si cambiano, «customer» è rifiutato.
func TestVisibilitaDellaBustaEsterna(t *testing.T) {
	b, p := conFoto(t)
	esegui(t, p.b.d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, p.busta)
	f := allega(t, b, p.busta, p.due())
	if vis(f[0]) != [3]bool{false, false, true} || vis(f[1]) != [3]bool{false, true, true} {
		t.Errorf("visibilità %v %v", vis(f[0]), vis(f[1]))
	}
	if Classificazione(ptr(2)) != ClasseEsterna {
		t.Error("classificazione")
	}
	u, err := b.ImpostaVisibilita(ctx, p.busta, f[0].ID, "orefice", false, chi)
	if err != nil || u.Oreficeria {
		t.Errorf("orefice: %+v %v", u, err)
	}
	u, err = b.ImpostaVisibilita(ctx, p.busta, f[0].ID, "artisan", true, chi)
	if err != nil || !u.Artigiano {
		t.Errorf("artisan: %+v %v", u, err)
	}
	if _, err := b.ImpostaVisibilita(ctx, p.busta, f[0].ID, "customer", true, chi); !errors.Is(err, ErrPubblicoNonValido) {
		t.Errorf("customer: %v", err)
	}
	if a := ultimoAudit(t, p.b.d, "repair.photo_visibility_changed"); a != "repair.photo_visibility_changed|repair|"+itoa(p.busta)+"|ok|{\"audience\":\"artisan\",\"photo_id\":"+itoa(f[0].ID)+",\"visible\":true}" {
		t.Errorf("audit %q", a)
	}
}

func ptr(n int64) *int64 { return &n }

// inventario: RIP-S-31
// Senza oreficeria e con l'oreficeria 1 (il negozio) la busta è diretta; cambiare oreficeria non
// riscrive i segni delle foto già caricate.
func TestCambioDiOreficeriaNonRiscriveISegni(t *testing.T) {
	b, p := conFoto(t)
	if Classificazione(nil) != ClasseDiretta || Classificazione(ptr(1)) != ClasseDiretta || Classificazione(ptr(0)) != ClasseDiretta {
		t.Error("classificazione diretta")
	}
	f := allega(t, b, p.busta, []Caricamento{{p.input, "direct.png"}})
	esegui(t, p.b.d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, p.busta)
	g, ok, err := b.FotoDi(ctx, p.busta, f[0].ID)
	if err != nil || !ok || vis(g) != [3]bool{true, false, false} {
		t.Errorf("%+v %v %v", g, ok, err)
	}
}

// inventario: RIP-S-29
// Le quattro estensioni dichiarate (senza maiuscole) passano; un'altra, un file che manca, uno
// oltre 25 MiB o vuoto sono rifiutati e niente si scrive; una foto da 25 MiB passa.
func TestAllegaEstensioniEDimensioni(t *testing.T) {
	b, p := conFoto(t)
	for _, e := range []string{".jpg", ".jpeg", ".png", ".webp", ".JPG"} {
		allega(t, b, p.busta, []Caricamento{{p.input, "image" + e}})
	}
	prima := len(fotoDellaBusta(t, b, p.busta))
	grande := scriviFile(t, p.dir, "oversized.png", make([]byte, imaging.MaxByteIngresso+1))
	vuoto := scriviFile(t, p.dir, "vuoto.png", nil)
	if err := os.Mkdir(filepath.Join(p.dir, "cartella.png"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, c := range []Caricamento{
		{p.input, "image.gif"},
		{p.input, "image"},
		{p.input, ".png"},
		{filepath.Join(p.dir, "missing.png"), "missing.png"},
		{grande, "oversized.png"},
		{vuoto, "vuoto.png"},
		{filepath.Join(p.dir, "cartella.png"), "cartella.png"},
	} {
		if _, err := b.AllegaFoto(ctx, p.busta, []Caricamento{c}, nil, chi); !errors.Is(err, ErrCaricamento) {
			t.Errorf("%+v: %v", c, err)
		}
	}
	// Una foto non valida nel gruppo ferma anche l'altra.
	if _, err := b.AllegaFoto(ctx, p.busta, []Caricamento{{p.input, "ok.png"}, {p.input, "no.gif"}}, nil, chi); !errors.Is(err, ErrCaricamento) {
		t.Errorf("gruppo: %v", err)
	}
	if _, err := b.AllegaFoto(ctx, p.busta, nil, nil, chi); !errors.Is(err, ErrCaricamento) {
		t.Errorf("nessuna: %v", err)
	}
	if n := len(fotoDellaBusta(t, b, p.busta)); n != prima {
		t.Errorf("scritte %d foto", n-prima)
	}
	pesante := scriviFile(t, p.dir, "heavy.jpg", sintetiche.JPEG(sintetiche.Gradiente(800, 600, 1), sintetiche.OpzioniJPEG{Pad: imaging.MaxByteIngresso}))
	allega(t, b, p.busta, []Caricamento{{pesante, "heavy.jpg"}})
}

func fotoDellaBusta(t *testing.T, b *Buste, id int64) []Foto {
	t.Helper()
	f, err := b.ElencoFoto(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// inventario: RIP-S-29, RIP-S-30
// Una foto nuova si salva JPEG con il nome .jpg (IMG_0001.JPEG → IMG_0001.jpg, screenshot.png →
// screenshot.jpg); tipo e byte della riga sono quelli del file salvato, alla risoluzione del
// telefono.
func TestAllegaSalvaJPEGConIlNomeGiusto(t *testing.T) {
	b, p := conFoto(t)
	tel := scriviFile(t, p.dir, "phone.jpg", sintetiche.JPEG(sintetiche.Liscio(4032, 3024), sintetiche.OpzioniJPEG{}))
	f := allega(t, b, p.busta, []Caricamento{{tel, "IMG_0001.JPEG"}, {p.input, "screenshot.png"}})
	if f[0].TipoMime != "image/jpeg" || f[0].NomeFile != "IMG_0001.jpg" || f[1].TipoMime != "image/jpeg" || f[1].NomeFile != "screenshot.jpg" {
		t.Errorf("%+v %+v", f[0], f[1])
	}
	pieno, err := media.Nuovo(p.mediaDi).Leggi(f[0].Chiave)
	if err != nil {
		t.Fatal(err)
	}
	if i, _ := sintetiche.LeggiJPEG(pieno); i.Larghezza != 4032 || i.Altezza != 3024 || !i.Base {
		t.Errorf("%+v", i)
	}
	if f[0].Byte != int64(len(pieno)) || len(f[0].Sha256) != 32 {
		t.Errorf("byte %d, impronta %d", f[0].Byte, len(f[0].Sha256))
	}
}

// inventario: RIP-S-29
// stored_filename/2: basename e rootname di Elixir (misurati sull'immagine dello standalone),
// massimo 255 grafemi con l'estensione del file salvato.
func TestNomeSalvato(t *testing.T) {
	j := []byte("\xFF\xD8\xFF")
	for _, c := range []struct{ in, out string }{
		{"IMG_0001.JPEG", "IMG_0001.jpg"},
		{"dir/a.png", "a.jpg"},
		{"a.tar.png", "a.tar.jpg"},
		{"..jpg", "..jpg"},
		{" .png", " .jpg"},
		{`\a.png`, `\a.jpg`},
		{"dir/a.png//", "a.jpg"},
		{"//", ".jpg"},
		{strings.Repeat("à", 300) + ".png", strings.Repeat("à", 251) + ".jpg"},
		{strings.Repeat("é", 300) + ".png", strings.Repeat("é", 251) + ".jpg"},
	} {
		if g := NomeSalvato(c.in, j); g != c.out {
			t.Errorf("%q: %q, atteso %q", c.in, g, c.out)
		}
	}
	if g := NomeSalvato("x.png", []byte("\x89PNG\r\n\x1a\n")); g != "x.png" {
		t.Errorf("PNG: %q", g)
	}
}

// inventario: RIP-S-31, RIP-S-32
// Visibilità ed eliminazione stanno nella busta, scrivono l'audit e non cancellano mai i byte in
// comune; l'audit non porta né il nome del file né la chiave.
func TestVisibilitaEliminazioneEAudit(t *testing.T) {
	b, p := conFoto(t)
	f := allega(t, b, p.busta, []Caricamento{{p.input, "bag.png"}, {p.input, "object.png"}})
	borsa, comune := f[0], f[1]
	if u, err := b.ImpostaVisibilita(ctx, p.busta, borsa.ID, "artisan", true, chi); err != nil || !u.Artigiano {
		t.Errorf("artisan: %+v %v", u, err)
	}
	if _, err := b.ImpostaVisibilita(ctx, p.busta, borsa.ID, "orefice", true, chi); !errors.Is(err, ErrPubblicoNonValido) {
		t.Errorf("orefice su diretta: %v", err)
	}
	if err := b.RimuoviFoto(ctx, p.busta, borsa.ID, chi); err != nil {
		t.Fatal(err)
	}
	if a := ultimoAudit(t, p.b.d, "repair.photo_removed"); a != "repair.photo_removed|repair|"+itoa(p.busta)+"|ok|{\"photo_id\":"+itoa(borsa.ID)+",\"position\":1}" {
		t.Errorf("audit %q", a)
	}
	if err := b.RimuoviFoto(ctx, p.busta, borsa.ID, chi); !errors.Is(err, ErrFotoNonTrovata) {
		t.Errorf("di nuovo: %v", err)
	}
	if _, err := b.ImpostaVisibilita(ctx, p.busta, borsa.ID, "customer", false, chi); !errors.Is(err, ErrFotoNonTrovata) {
		t.Errorf("visibilità della foto tolta: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(p.mediaDi, comune.Chiave)); err != nil {
		t.Errorf("il file in comune è sparito: %v", err)
	}
	var dati string
	righe, err := p.b.d.Lettura.Query(`SELECT dati FROM eventi_audit WHERE azione LIKE 'repair.photo%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer righe.Close()
	n := 0
	for righe.Next() {
		if err := righe.Scan(&dati); err != nil {
			t.Fatal(err)
		}
		n++
		if strings.Contains(dati, "bag.png") || strings.Contains(dati, comune.Chiave) {
			t.Errorf("audit con nome o chiave: %s", dati)
		}
	}
	if n < 4 {
		t.Errorf("righe d'audit delle foto: %d", n)
	}
}

// inventario: RIP-S-31, RIP-S-32
// La foto di un'altra busta, una foto o una busta che non c'è: ErrFotoNonTrovata (busta:
// ErrBustaNonTrovata); un id non positivo per la visibilità è «pubblico non valido», come
// Id.positive dello standalone; niente si scrive.
func TestFotoDiUnAltraBusta(t *testing.T) {
	b, p := conFoto(t)
	f := allega(t, b, p.busta, []Caricamento{{p.input, "a.png"}})
	altra := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-ALTRA" })).ID
	prima := scatta(t, p.b.d)
	if _, err := b.ImpostaVisibilita(ctx, altra, f[0].ID, "customer", false, chi); !errors.Is(err, ErrFotoNonTrovata) {
		t.Errorf("visibilità da un'altra busta: %v", err)
	}
	if err := b.RimuoviFoto(ctx, altra, f[0].ID, chi); !errors.Is(err, ErrFotoNonTrovata) {
		t.Errorf("rimozione da un'altra busta: %v", err)
	}
	if _, err := b.ImpostaVisibilita(ctx, 999999, f[0].ID, "customer", false, chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("busta che non c'è: %v", err)
	}
	if err := b.RimuoviFoto(ctx, 999999, f[0].ID, chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("rimozione da una busta che non c'è: %v", err)
	}
	if _, err := b.ImpostaVisibilita(ctx, p.busta, 0, "customer", false, chi); !errors.Is(err, ErrPubblicoNonValido) {
		t.Errorf("id 0: %v", err)
	}
	if err := b.RimuoviFoto(ctx, p.busta, -1, chi); !errors.Is(err, ErrFotoNonTrovata) {
		t.Errorf("rimozione id -1: %v", err)
	}
	uguali(t, "rifiuti", prima, scatta(t, p.b.d))
	if g, ok, _ := b.FotoDi(ctx, altra, f[0].ID); ok {
		t.Errorf("FotoDi da un'altra busta: %+v", g)
	}
}

// inventario: RIP-S-29, RIP-S-30
// Busta che non c'è, foto non valida, archivio guasto, audit che fallisce, sessione revocata dopo
// la normalizzazione: nessuna riga. I file già pubblicati restano (orfani, li raccoglie la
// pulizia), come nello standalone.
func TestAllegaRifiutiSenzaRighe(t *testing.T) {
	b, p := conFoto(t)
	uno := []Caricamento{{p.input, "image.png"}}
	if _, err := b.AllegaFoto(ctx, 999999, uno, nil, chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("busta: %v", err)
	}
	invalida := scriviFile(t, p.dir, "invalid.png", []byte("not an image"))
	if _, err := b.AllegaFoto(ctx, p.busta, []Caricamento{{invalida, "invalid.png"}}, nil, chi); !errors.Is(err, imaging.ErrImmagine) {
		t.Errorf("immagine: %v", err)
	}
	bloccata := scriviFile(t, p.dir, "not-a-directory", []byte("blocked"))
	b.ConFoto(media.Nuovo(bloccata), nelProcesso{})
	if _, err := b.AllegaFoto(ctx, p.busta, uno, nil, chi); !errors.Is(err, media.ErrSalvataggio) {
		t.Errorf("archivio: %v", err)
	}
	b.ConFoto(media.Nuovo(p.mediaDi), nelProcesso{})
	esegui(t, p.b.d, `CREATE TRIGGER audit_guasto BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.photo_uploaded' BEGIN SELECT RAISE(ABORT, 'guasto'); END`)
	if _, err := b.AllegaFoto(ctx, p.busta, uno, nil, chi); err == nil {
		t.Error("audit guasto: nessun errore")
	}
	esegui(t, p.b.d, `DROP TRIGGER audit_guasto`)
	esegui(t, p.b.d, `INSERT INTO utenti (id, email, ruolo, attivo, versione_sessione, creato_il, aggiornato_il) VALUES (7, 'dip@esempio.test', 'dipendente', 1, 3, ?1, ?1)`, istanteProva)
	if _, err := b.AllegaFoto(ctx, p.busta, uno, &Sessione{ID: 7, Versione: 2}, chi); !errors.Is(err, ErrSessioneRevocata) {
		t.Errorf("versione vecchia: %v", err)
	}
	esegui(t, p.b.d, `UPDATE utenti SET attivo = 0 WHERE id = 7`)
	if _, err := b.AllegaFoto(ctx, p.busta, uno, &Sessione{ID: 7, Versione: 3}, chi); !errors.Is(err, ErrSessioneRevocata) {
		t.Errorf("disattivato: %v", err)
	}
	if n := len(fotoDellaBusta(t, b, p.busta)); n != 0 {
		t.Errorf("%d righe", n)
	}
	// Il file pubblicato prima del rifiuto resta nella cartella.
	ok := false
	v, _ := os.ReadDir(p.mediaDi)
	for _, e := range v {
		ok = ok || media.ChiaveValida(e.Name())
	}
	if !ok {
		t.Error("nessun oggetto pubblicato")
	}
	esegui(t, p.b.d, `UPDATE utenti SET attivo = 1 WHERE id = 7`)
	if _, err := b.AllegaFoto(ctx, p.busta, uno, &Sessione{ID: 7, Versione: 3}, chi); err != nil {
		t.Errorf("sessione valida: %v", err)
	}
	if _, err := Nuove(p.b.d, orologio()).AllegaFoto(ctx, p.busta, uno, nil, chi); !errors.Is(err, ErrSenzaArchivio) {
		t.Errorf("senza archivio: %v", err)
	}
}

// inventario: RIP-S-31
// L'unico punto che decide: un pubblico che la classe non ha è rifiutato, non dirottato su un'altra
// colonna; spostare la busta non rimette in gioco un link vecchio; il personale vede tutto; un
// pubblico sconosciuto o la foto di un'altra busta sono un no.
func TestUnicoPuntoDellaVisibilita(t *testing.T) {
	b, p := conFoto(t)
	f := allega(t, b, p.busta, p.due())
	diretta := BustaDellaFoto{ID: p.busta}
	casi := []struct {
		f    Foto
		p    Pubblico
		vede bool
	}{
		{f[0], PubblicoCliente, true}, {f[1], PubblicoCliente, true},
		{f[0], PubblicoArtigiano, false}, {f[1], PubblicoArtigiano, true},
		{f[0], PubblicoOreficeria, false}, {f[1], PubblicoOreficeria, false},
		{f[0], PubblicoPersonale, true}, {f[0], "cliente", false}, {f[0], "", false},
	}
	for _, c := range casi {
		if Visibile(diretta, c.f, c.p) != c.vede {
			t.Errorf("diretta %v %s: atteso %v", c.f.FotoBusta, c.p, c.vede)
		}
	}
	esterna := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-EST"; a.OreficeriaID = "2" })).ID
	g := allega(t, b, esterna, p.due())
	e := BustaDellaFoto{ID: esterna, OreficeriaID: ptr(2)}
	if !Visibile(e, g[0], PubblicoOreficeria) || !Visibile(e, g[1], PubblicoOreficeria) || Visibile(e, g[0], PubblicoCliente) || Visibile(e, g[1], PubblicoCliente) {
		t.Error("esterna")
	}
	spostata := BustaDellaFoto{ID: esterna}
	if Visibile(spostata, g[0], PubblicoOreficeria) || Visibile(spostata, g[0], PubblicoCliente) {
		t.Error("spostata: il vecchio pubblico risponde ancora")
	}
	altra := BustaDellaFoto{ID: esterna + 100}
	if Visibile(altra, g[0], PubblicoPersonale) || Visibile(altra, g[0], PubblicoOreficeria) {
		t.Error("foto di un'altra busta")
	}
	if VisibilitaIniziale(ClasseEsterna, true) != (Visibilita{Oreficeria: true}) || VisibilitaIniziale(ClasseDiretta, false) != (Visibilita{Cliente: true, Artigiano: true}) {
		t.Error("VisibilitaIniziale")
	}
}

// inventario: RIP-S-31
// Una foto invisibile esce insieme dall'elenco di quel pubblico; chiudere una colonna non chiude le
// altre.
func TestVisibiliPer(t *testing.T) {
	b, p := conFoto(t)
	f := allega(t, b, p.busta, p.due())
	d := BustaDellaFoto{ID: p.busta}
	ids := func(pub Pubblico) []int64 {
		v, err := b.VisibiliPer(ctx, d, pub)
		if err != nil {
			t.Fatal(err)
		}
		var n []int64
		for _, x := range v {
			n = append(n, x.ID)
		}
		return n
	}
	if g := ids(PubblicoCliente); !reflect.DeepEqual(g, []int64{f[0].ID, f[1].ID}) {
		t.Errorf("cliente %v", g)
	}
	if g := ids(PubblicoArtigiano); !reflect.DeepEqual(g, []int64{f[1].ID}) {
		t.Errorf("artigiano %v", g)
	}
	if _, err := b.ImpostaVisibilita(ctx, p.busta, f[1].ID, "customer", false, chi); err != nil {
		t.Fatal(err)
	}
	if g := ids(PubblicoCliente); !reflect.DeepEqual(g, []int64{f[0].ID}) {
		t.Errorf("cliente dopo %v", g)
	}
	if g := ids(PubblicoArtigiano); !reflect.DeepEqual(g, []int64{f[1].ID}) {
		t.Errorf("artigiano dopo %v", g)
	}
	if g := ids(PubblicoPersonale); len(g) != 2 {
		t.Errorf("personale %v", g)
	}
}

// inventario: RIP-S-31
// Lo stesso valore non riscrive la riga (Ecto non fa l'UPDATE senza cambi) ma l'audit c'è.
func TestVisibilitaUgualeNonRiscrive(t *testing.T) {
	b, p := conFoto(t)
	f := allega(t, b, p.busta, []Caricamento{{p.input, "a.png"}})
	esegui(t, p.b.d, `UPDATE foto_buste SET aggiornato_il = '2000-01-01T00:00:00.000000Z'`)
	if _, err := b.ImpostaVisibilita(ctx, p.busta, f[0].ID, "customer", true, chi); err != nil {
		t.Fatal(err)
	}
	if v := valore(t, p.b.d, `SELECT aggiornato_il FROM foto_buste`); v != "2000-01-01T00:00:00.000000Z" {
		t.Errorf("riscritta: %v", v)
	}
	if conta(t, p.b.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.photo_visibility_changed'`) != 1 {
		t.Error("audit mancante")
	}
	if _, err := b.ImpostaVisibilita(ctx, p.busta, f[0].ID, "customer", false, chi); err != nil {
		t.Fatal(err)
	}
	if v := valore(t, p.b.d, `SELECT aggiornato_il FROM foto_buste`); v != istanteProva {
		t.Errorf("non aggiornata: %v", v)
	}
}

// inventario: RIP-S-29
// Un caricamento che non è un file regolare è rifiutato prima di leggerlo (File.lstat con type
// :regular): un link a una foto buona e una cartella con dentro qualcosa (dimensione non zero).
func TestAllegaSoloFileRegolari(t *testing.T) {
	b, p := conFoto(t)
	link := filepath.Join(p.dir, "link.png")
	if err := os.Symlink(p.input, link); err != nil {
		t.Fatal(err)
	}
	cartella := filepath.Join(p.dir, "piena.png")
	if err := os.Mkdir(cartella, 0o700); err != nil {
		t.Fatal(err)
	}
	scriviFile(t, cartella, strings.Repeat("n", 200), []byte("x"))
	for _, c := range []Caricamento{{link, "link.png"}, {cartella, "piena.png"}} {
		if _, err := b.AllegaFoto(ctx, p.busta, []Caricamento{c}, nil, chi); !errors.Is(err, ErrCaricamento) {
			t.Errorf("%s: %v", c.NomeOriginale, err)
		}
	}
}

// inventario: RIP-S-29
// Dopo una rimozione la posizione riparte dall'ultima rimasta più uno; la foto della busta è la
// prima solo di una busta che non ha più foto.
func TestPosizioneDopoUnaRimozione(t *testing.T) {
	b, p := conFoto(t)
	f := allega(t, b, p.busta, p.due())
	if err := b.RimuoviFoto(ctx, p.busta, f[0].ID, chi); err != nil {
		t.Fatal(err)
	}
	g := allega(t, b, p.busta, []Caricamento{{p.input, "c.png"}})
	if g[0].Posizione != 3 || g[0].FotoBusta {
		t.Errorf("dopo aver tolto la prima: %+v", g[0])
	}
	for _, x := range []int64{f[1].ID, g[0].ID} {
		if err := b.RimuoviFoto(ctx, p.busta, x, chi); err != nil {
			t.Fatal(err)
		}
	}
	h := allega(t, b, p.busta, []Caricamento{{p.input, "d.png"}})
	if h[0].Posizione != 1 || !h[0].FotoBusta {
		t.Errorf("busta di nuovo vuota: %+v", h[0])
	}
}

// inventario: RIP-S-29, RIP-S-31, RIP-S-32
// Una busta nel cestino non c'è per le foto, come Repo.get dello standalone (il filtro del cestino
// di Repo.prepare_query/3: Repair è in Trash.families/0): allegare, cambiare visibilità e togliere
// danno ErrBustaNonTrovata senza righe né audit. Il file normalizzato resta nell'archivio, orfano,
// perché attach_many/3 lo salva prima della transazione che rilegge la busta.
func TestFotoSuUnaBustaNelCestino(t *testing.T) {
	b, p := conFoto(t)
	f := allega(t, b, p.busta, []Caricamento{{p.input, "a.png"}})[0]
	esegui(t, p.b.d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, p.busta)
	prima := scatta(t, p.b.d)
	righe := func() any {
		return valore(t, p.b.d, `SELECT group_concat(id || ':' || posizione || ':' || visibile_cliente, ',') FROM foto_buste`)
	}
	primaRighe := righe()
	altro := scriviFile(t, p.dir, "altro.png", sintetiche.PNG(20, 10, 4))
	if _, err := b.AllegaFoto(ctx, p.busta, []Caricamento{{altro, "b.png"}}, nil, chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("allega: %v", err)
	}
	if _, err := b.ImpostaVisibilita(ctx, p.busta, f.ID, "customer", false, chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("visibilità: %v", err)
	}
	if err := b.RimuoviFoto(ctx, p.busta, f.ID, chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("rimozione: %v", err)
	}
	uguali(t, "cestino", prima, scatta(t, p.b.d))
	if dopo := righe(); dopo != primaRighe {
		t.Errorf("foto_buste: %v, prima %v", dopo, primaRighe)
	}
	if v, err := os.ReadDir(p.mediaDi); err != nil || len(v) != 2 {
		t.Errorf("archivio: %d file %v (attesi la foto e l'orfano)", len(v), err)
	}
}
