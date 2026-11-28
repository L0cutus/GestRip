package buste

import (
	"errors"
	"strconv"
	"testing"

	"github.com/L0cutus/gestrip/internal/firmeflask"
)

// Slice 135 (D-120, risposta F (a)): i link storici dopo un'eliminazione. La firma (o il codice) sul
// numero scritto nell'indirizzo vale solo per la busta il cui numero_import è esattamente quel numero;
// per ogni altra busta, anche con numero_import NULL, vale solo il numero salvato, come lo standalone.
//
// L'eliminazione definitiva non c'è ancora (139): la busta importata «0046/26» (salvata «46/26»)
// eliminata e poi un'altra busta che prende «46/26» si simulano con una sola busta «46/26» il cui
// numero_import non è «0046/26»: NULL (nata nel Go, o importata prima della 134), «46/26» (importata
// già così) o «046/26» (un altro numero con gli zeri). Il caso buono è numero_import «0046/26».

// variantiImport sono i numero_import della busta «46/26» e se il vecchio link di «0046/26» la apre.
var variantiImport = []struct {
	nome   string
	numImp any // nil è NULL
	apre   bool
}{
	{"nata nel go (NULL)", nil, false},
	{"importata già 46/26", "46/26", false},
	{"importata 046/26", "046/26", false},
	{"importata 0046/26", "0046/26", true},
}

// bustaDopoEliminazione è la busta «46/26» della variante, con il codice «CODICE» impresso su
// «0046/26» (il vecchio link della busta eliminata), due foto, il segreto Flask e la chiave degli
// artigiani.
func bustaDopoEliminazione(t *testing.T, numImp any) (*Buste, int64, []Foto) {
	t.Helper()
	b, p := conFoto(t)
	conLink(b)
	b.ConSegretoFlask(firmeflask.Nuovo(segretoProva))
	esegui(t, b.d, `UPDATE buste SET numero = '46/26', numero_import = ?, hash_codice_flask = ? WHERE id = ?`,
		numImp, firmeflask.ImprontaCodice("0046/26", "CODICE"), p.busta)
	return b, p.busta, allega(t, b, p.busta, p.due())
}

// inventario: CLI-16, CLI-19
// /stato (pagina, poll e messaggio): la firma e il codice di «0046/26» aprono la «46/26» solo se il
// suo numero_import è «0046/26»; la firma sul numero salvato apre sempre, con «46/26» o con gli zeri
// scritti in più; la firma di «046/26» solo per la busta importata «046/26». Ogni rifiuto è il 404
// della busta che non c'è (ErrBustaNonTrovata).
func TestLinkStoricoStatoDopoEliminazione(t *testing.T) {
	const exp = 2_000_000_000
	vecchia := credFirma(firmaStatoProva("0046/26", exp), exp)
	salvato := credFirma(firmaStatoProva("46/26", exp), exp)
	treZeri := credFirma(firmaStatoProva("046/26", exp), exp)
	for _, v := range variantiImport {
		t.Run(v.nome, func(t *testing.T) {
			b, id, _ := bustaDopoEliminazione(t, v.numImp)
			apre := func(scritto string, cred CredenzialeStorica, atteso bool) {
				t.Helper()
				p, err := b.PaginaStorica(ctx, scritto, cred, chi)
				switch {
				case atteso && (err != nil || p.ID != id):
					t.Errorf("pagina %s %+v: %v %d, attesa la busta %d", scritto, cred, err, p.ID, id)
				case !atteso && !errors.Is(err, ErrBustaNonTrovata):
					t.Errorf("pagina %s %+v: %v %d, atteso il 404", scritto, cred, err, p.ID)
				}
				if _, err := b.ConversazioneStorica(ctx, scritto, cred, chi); errors.Is(err, ErrBustaNonTrovata) == atteso {
					t.Errorf("poll %s %+v: %v", scritto, cred, err)
				}
				if err := b.ScriviMessaggioStorico(ctx, scritto, cred, "", chi); errors.Is(err, ErrBustaNonTrovata) == atteso {
					t.Errorf("messaggio %s %+v: %v", scritto, cred, err)
				}
			}
			apre("0046/26", vecchia, v.apre)
			apre("0046/26", credCodice("CODICE"), v.apre)
			apre("46/26", salvato, true)
			apre("0046/26", salvato, true)
			apre("046/26", treZeri, v.numImp == "046/26")
			// Il vecchio link non si apre nemmeno scritto senza zeri (com'era prima della 135), né il
			// codice impresso su «0046/26» con il numero salvato scritto.
			apre("46/26", vecchia, false)
			apre("46/26", credCodice("CODICE"), false)
			// Revocata: il 410 solo con una credenziale che apriva la busta (il vecchio link solo per
			// la busta importata «0046/26»), ogni altra il 404 della busta che non c'è.
			esegui(t, b.d, `UPDATE buste SET codice_flask_revocato_il = ? WHERE id = ?`, istanteProva, id)
			revoca := func(scritto string, cred CredenzialeStorica, atteso410 bool) {
				t.Helper()
				_, err := b.PaginaStorica(ctx, scritto, cred, chi)
				if want := map[bool]error{true: ErrLinkStoricoRevocato, false: ErrBustaNonTrovata}[atteso410]; !errors.Is(err, want) {
					t.Errorf("revocata %s %+v: %v, atteso %v", scritto, cred, err, want)
				}
			}
			revoca("0046/26", vecchia, v.apre)
			revoca("0046/26", credCodice("CODICE"), v.apre)
			revoca("046/26", treZeri, v.numImp == "046/26")
			revoca("0046/26", salvato, true)
			revoca("46/26", vecchia, false)
		})
	}
}

// inventario: CLI-16
// Il cestino, com'era: la busta importata «0046/26» nel cestino non si apre né con il suo vecchio
// link né con la firma sul numero salvato.
func TestLinkStoricoStatoNelCestino(t *testing.T) {
	const exp = 2_000_000_000
	b, id, _ := bustaDopoEliminazione(t, "0046/26")
	esegui(t, b.d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, id)
	for scritto, cred := range map[string]CredenzialeStorica{
		"0046/26": credFirma(firmaStatoProva("0046/26", exp), exp),
		"46/26":   credFirma(firmaStatoProva("46/26", exp), exp),
	} {
		if _, err := b.PaginaStorica(ctx, scritto, cred, chi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("%s nel cestino: %v", scritto, err)
		}
	}
}

// inventario: CLI-20
// /foto e /thumb: la firma sullo slug «0046_26» dà la foto della «46/26» solo se il suo numero_import
// è «0046/26»; altrimenti nil (il 404 «Foto non disponibile») senza la riga repair.public_photo_viewed.
// Lo slug del numero salvato apre sempre. Le foto della pagina /stato aperta con la firma sul numero
// salvato e un altro numero scritto («/stato/046/26») hanno lo slug del salvato, e si aprono.
func TestLinkStoricoFotoDopoEliminazione(t *testing.T) {
	s := firmeflask.Nuovo(segretoProva)
	for _, v := range variantiImport {
		t.Run(v.nome, func(t *testing.T) {
			b, id, ff := bustaDopoEliminazione(t, v.numImp)
			adesso := b.o.Adesso().Unix()
			e := strconv.FormatInt(adesso, 10)
			for _, va := range []Variante{VarianteIntera, VarianteMiniatura} {
				prima := contaAzione(t, b, "repair.public_photo_viewed")
				m, err := b.LeggiFotoStorica(ctx, "0046_26", ff[0].NomeFile, s.FirmaFotoBusta("0046_26", ff[0].NomeFile, adesso), e, va, chi)
				if err != nil || (m != nil) != v.apre {
					t.Errorf("0046_26 %s: foto %v, %v", va, m != nil, err)
				}
				righe := contaAzione(t, b, "repair.public_photo_viewed") - prima
				if want := map[bool]int{true: 1, false: 0}[v.apre]; righe != want {
					t.Errorf("0046_26 %s: righe %d, attese %d", va, righe, want)
				}
				if m, err := b.LeggiFotoStorica(ctx, "46_26", ff[0].NomeFile, s.FirmaFotoBusta("46_26", ff[0].NomeFile, adesso), e, va, chi); err != nil || m == nil {
					t.Errorf("46_26 %s: foto %v, %v", va, m != nil, err)
				}
			}
			// La pagina di «046/26» aperta con la firma del numero salvato: le sue foto si aprono.
			pg, err := b.PaginaStorica(ctx, "046/26", credFirma(firmaStatoProva("46/26", 2_000_000_000), 2_000_000_000), chi)
			if err != nil || pg.ID != id || len(pg.FirmeFoto) == 0 {
				t.Fatalf("pagina 046/26: %v %+v", err, pg)
			}
			slug := map[bool]string{true: "046_26", false: "46_26"}[v.numImp == "046/26"]
			if pg.SlugFoto != slug {
				t.Errorf("slug delle foto %q, atteso %q", pg.SlugFoto, slug)
			}
			if m, err := b.LeggiFotoStorica(ctx, pg.SlugFoto, pg.Foto[0].NomeFile, pg.FirmeFoto[0], pg.ScadenzaFoto, VarianteIntera, chi); err != nil || m == nil {
				t.Errorf("foto della pagina 046/26: foto %v, %v", m != nil, err)
			}
			// La pagina del vecchio link, quando si apre, firma le foto sullo slug com'è scritto.
			if v.apre {
				pg, err := b.PaginaStorica(ctx, "0046/26", credCodice("CODICE"), chi)
				if err != nil || pg.SlugFoto != "0046_26" {
					t.Errorf("pagina 0046/26: %v %q", err, pg.SlugFoto)
				}
			}
		})
	}
}

// inventario: CLI-20
// Le foto storiche nel cestino, com'era: nil con tutti e due gli slug.
func TestLinkStoricoFotoNelCestino(t *testing.T) {
	s := firmeflask.Nuovo(segretoProva)
	b, id, ff := bustaDopoEliminazione(t, "0046/26")
	esegui(t, b.d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, id)
	adesso := b.o.Adesso().Unix()
	for _, slug := range []string{"0046_26", "46_26"} {
		if m, err := b.LeggiFotoStorica(ctx, slug, ff[0].NomeFile, s.FirmaFotoBusta(slug, ff[0].NomeFile, adesso), strconv.FormatInt(adesso, 10), VarianteIntera, chi); err != nil || m != nil {
			t.Errorf("%s nel cestino: foto %v, %v", slug, m != nil, err)
		}
	}
}

// inventario: ART-24
// /artigiano/<busta>/<anno> e le sue foto: il gettone Flask di «0046/26» apre la «46/26» solo se il suo
// numero_import è «0046/26»; il gettone del numero salvato sul numero salvato apre sempre. Il link
// nuovo per id (gettone g1. della chiave del server, D-118) non guarda il numero e apre sempre.
func TestLinkStoricoArtigianoDopoEliminazione(t *testing.T) {
	s := firmeflask.Nuovo(segretoProva)
	for _, v := range variantiImport {
		t.Run(v.nome, func(t *testing.T) {
			b, id, _ := bustaDopoEliminazione(t, v.numImp)
			adesso := b.o.Adesso().Unix()
			buona, err := b.PaginaArtigianoStorica(ctx, "46/26", s.FirmaArtigiano("46/26", adesso), pubblicoChi)
			if err != nil || buona.ID != id || len(buona.Foto) == 0 {
				t.Fatalf("46/26: %v %+v", err, buona)
			}
			file := buona.Foto[0].NomeFile
			vecchio := s.FirmaArtigiano("0046/26", adesso)
			p, err := b.PaginaArtigianoStorica(ctx, "0046/26", vecchio, pubblicoChi)
			if v.apre != (err == nil && p.ID == id) || (!v.apre && !errors.Is(err, ErrBustaNonTrovata)) {
				t.Errorf("pagina 0046/26: %v %d", err, p.ID)
			}
			for _, va := range []Variante{VarianteIntera, VarianteMiniatura} {
				if m, err := b.LeggiFotoArtigianoStorica(ctx, "0046/26", vecchio, file, va, pubblicoChi); err != nil || (m != nil) != v.apre {
					t.Errorf("foto 0046/26 %s: foto %v, %v", va, m != nil, err)
				}
				if m, err := b.LeggiFotoArtigianoStorica(ctx, "46/26", s.FirmaArtigiano("46/26", adesso), file, va, pubblicoChi); err != nil || m == nil {
					t.Errorf("foto 46/26 %s: foto %v, %v", va, m != nil, err)
				}
			}
			if p, err := b.PaginaArtigiano(ctx, itoa(id), b.GettoneArtigiano(id), pubblicoChi); err != nil || p.ID != id {
				t.Errorf("gettone g1.: %v %d", err, p.ID)
			}
		})
	}
}

// inventario: ART-24
// Il cestino, com'era: il vecchio link dell'artigiano della busta nel cestino è il 404.
func TestLinkStoricoArtigianoNelCestino(t *testing.T) {
	s := firmeflask.Nuovo(segretoProva)
	b, id, _ := bustaDopoEliminazione(t, "0046/26")
	esegui(t, b.d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, id)
	adesso := b.o.Adesso().Unix()
	for _, n := range []string{"0046/26", "46/26"} {
		if _, err := b.PaginaArtigianoStorica(ctx, n, s.FirmaArtigiano(n, adesso), pubblicoChi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("%s nel cestino: %v", n, err)
		}
	}
}
