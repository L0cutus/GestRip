package web

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/L0cutus/gestrip/internal/firmeflask"
	"github.com/L0cutus/gestrip/internal/imaging/sintetiche"
)

// Slice 135 (D-120, risposta F (a)): dopo un'eliminazione il vecchio link di «0046/26» non apre la
// «46/26» di un altro cliente. L'eliminazione definitiva non c'è ancora: la busta «46/26» con
// numero_import NULL (nata nel Go) è l'altra busta; con numero_import «0046/26» è la busta giusta.

// inventario: CLI-16, CLI-20
// /stato, /foto e /thumb: il vecchio link (firma, codice, slug) dà il 404 sulla busta senza quel numero
// di prima e si apre sulla busta giusta; la firma sul numero salvato si apre sempre.
func TestLinkStoriciDopoEliminazioneHTTP(t *testing.T) {
	p := paginaClienteProva(t)
	c := storicaWeb(t, p, nil)
	f1 := p.allega(t, c.ID, "fronte.png", sintetiche.PNG(400, 300, 1))
	s := firmeflask.Nuovo(segretoStorico)
	exp := p.o.Adesso().Unix() + 3600
	e := strconv.FormatInt(exp, 10)
	foto := func(rotta, slug string) string {
		return "/" + rotta + "/" + slug + "/" + f1.NomeFile + "?sig=" + s.FirmaFotoBusta(slug, f1.NomeFile, exp) + "&exp=" + e
	}
	vecchi := []string{
		"/stato/0046/26?pw=CODICE",
		"/stato/0046/26?t=" + firmaStoricaWeb("0046/26", 2_000_000_000) + "&exp=2000000000",
		"/stato/0046/26/poll?pw=CODICE",
		foto("foto", "0046_26"),
		foto("thumb", "0046_26"),
	}
	salvati := []string{
		"/stato/46/26?t=" + firmaStoricaWeb("46/26", 2_000_000_000) + "&exp=2000000000",
		"/stato/0046/26?t=" + firmaStoricaWeb("46/26", 2_000_000_000) + "&exp=2000000000",
		foto("foto", "46_26"),
		foto("thumb", "46_26"),
	}
	for _, numImp := range []any{"0046/26", nil} {
		eseguiSQL(t, p.d, `UPDATE buste SET numero_import = ? WHERE id = ?`, numImp, c.ID)
		b := nuovoBrowser(t, p.h)
		atteso := http.StatusOK
		if numImp == nil {
			atteso = http.StatusNotFound
		}
		for _, perc := range vecchi {
			if r := b.vai("GET", perc, nil, nil); r.codice != atteso {
				t.Errorf("numero_import %v, %s: %d, atteso %d", numImp, perc, r.codice, atteso)
			}
		}
		for _, perc := range salvati {
			if r := b.vai("GET", perc, nil, nil); r.codice != http.StatusOK {
				t.Errorf("numero_import %v, %s: %d", numImp, perc, r.codice)
			}
		}
	}
}

// inventario: ART-24
// /artigiano/<busta>/<anno> e le sue foto: il gettone Flask di «0046/26» dà il 404 sulla «46/26» nata
// nel Go e si apre sulla busta importata «0046/26»; il gettone di «46/26» si apre sempre.
func TestLinkArtigianoStoricoDopoEliminazioneHTTP(t *testing.T) {
	p, c, _, pezzo := facceArtigiano(t)
	s := firmeflask.Nuovo(segretoFlaskArtigiano)
	adesso := p.o.Adesso().Unix() + 60
	vecchio, salvato := s.FirmaArtigiano("0046/26", adesso), s.FirmaArtigiano("46/26", adesso)
	nome := url.PathEscape(pezzo.NomeFile)
	pub := nuovoBrowser(t, p.pubblica)
	for _, numImp := range []any{"0046/26", nil} {
		eseguiSQL(t, p.d, `UPDATE buste SET numero = '46/26', numero_import = ? WHERE id = ?`, numImp, c.ID)
		atteso := http.StatusOK
		if numImp == nil {
			atteso = http.StatusNotFound
		}
		for _, perc := range []string{"/artigiano/0046/26?t=" + vecchio, "/artigiano/0046/26/foto/" + nome + "?t=" + vecchio + "&size=thumb"} {
			if r := pub.vai("GET", perc, nil, nil); r.codice != atteso {
				t.Errorf("numero_import %v, %s: %d, atteso %d", numImp, perc, r.codice, atteso)
			}
		}
		for _, perc := range []string{"/artigiano/46/26?t=" + salvato, "/artigiano/46/26/foto/" + nome + "?t=" + salvato} {
			if r := pub.vai("GET", perc, nil, nil); r.codice != http.StatusOK {
				t.Errorf("numero_import %v, %s: %d", numImp, perc, r.codice)
			}
		}
	}
}
