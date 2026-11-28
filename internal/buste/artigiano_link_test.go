package buste

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/firmeflask"
	"github.com/L0cutus/gestrip/internal/firmephoenix"
)

// I link e la pagina pubblica dell'artigiano (ART-23…26, RIP-S-25, D-87), come artisan_link_test.exs
// e public_artisan_controller_test.exs dello standalone (go-baseline). Chiavi e segreti inventati.

var (
	chiaveArtigianoProva = []byte("sottochiave-artigiano-di-prova-SINTETICA-32b")
	standaloneProva      = firmephoenix.Nuovo(strings.Repeat("SINTETICO-secret-key-base-", 3))
	pubblicoChi          = chi
)

// conLink dà le buste con la sottochiave del Go e il secret_key_base dello standalone.
func conLink(b *Buste) *Buste { return b.ConLinkArtigiano(chiaveArtigianoProva, standaloneProva) }

func tokDi(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return u.Query().Get("t")
}

// inventario: ART-23, ART-24
// Il gettone del Go apre la pagina della sua busta per 90 giorni compresi, poi no; non apre
// un'altra busta; una chiave diversa, nessuna chiave o un carattere toccato non aprono niente. Ogni
// rifiuto è lo stesso ErrBustaNonTrovata con la riga repair.public_artisan_denied senza busta;
// l'apertura scrive repair.public_artisan_viewed con la busta.
func TestPaginaArtigianoGettoneDelGo(t *testing.T) {
	b, d, o := prova(t)
	conLink(b)
	c := crea(t, b, modulo())
	altra := crea(t, b, modulo())
	g := b.GettoneArtigiano(c.ID)
	// Il formato fissato: calcolato fuori dal Go (HMAC-SHA256 della sottochiave su «gestrip link
	// artigiano\x00<id>\x00<firmato_ms>», base64url senza riempimento).
	if c.ID != 1 || g != "g1.1793437200000.cWhSaYalj8se-ipJldWXY3gXOl77G1GJxHGjWU-audA" {
		t.Fatalf("gettone: %d %q", c.ID, g)
	}
	p, err := b.PaginaArtigiano(ctx, itoa(c.ID), g, pubblicoChi)
	if err != nil || p.ID != c.ID {
		t.Fatalf("apertura: %v %+v", err, p)
	}
	if a := ultimoAudit(t, d, "repair.public_artisan_viewed"); a != "repair.public_artisan_viewed|repair|"+itoa(c.ID)+"|ok|{}" {
		t.Errorf("audit: %q", a)
	}
	toccato := []byte(g)
	toccato[len(toccato)-3] ^= 1
	rifiuti := map[string][2]string{
		"altra busta":  {itoa(altra.ID), g},
		"toccato":      {itoa(c.ID), string(toccato)},
		"vuoto":        {itoa(c.ID), ""},
		"id non cifre": {"abc", g},
		"id zero":      {"0", g},
		"id negativo":  {"-" + itoa(c.ID), g},
		"id spazio":    {itoa(c.ID) + " ", g},
		"prefisso":     {itoa(c.ID), strings.Replace(g, "g1.", "g2.", 1)},
		"senza firma":  {itoa(c.ID), g[:strings.LastIndex(g, ".")]},
		"ms con zero":  {itoa(c.ID), strings.Replace(g, "g1.", "g1.0", 1)},
	}
	for nome, x := range rifiuti {
		if _, err := b.PaginaArtigiano(ctx, x[0], x[1], pubblicoChi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("%s: %v", nome, err)
		}
	}
	if n := contaAzione(t, b, "repair.public_artisan_denied"); n != len(rifiuti) {
		t.Errorf("rifiuti scritti: %d", n)
	}
	if a := ultimoAudit(t, d, "repair.public_artisan_denied"); a != "repair.public_artisan_denied|repair|unknown|ok|{}" {
		t.Errorf("audit del rifiuto: %q", a)
	}
	o.Sposta(DurataLinkArtigiano)
	if _, err := b.PaginaArtigiano(ctx, itoa(c.ID), g, pubblicoChi); err != nil {
		t.Errorf("all'ultimo istante: %v", err)
	}
	o.Sposta(time.Millisecond)
	if _, err := b.PaginaArtigiano(ctx, itoa(c.ID), g, pubblicoChi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("scaduto: %v", err)
	}
	o.Sposta(-DurataLinkArtigiano)
	b.ConLinkArtigiano([]byte("altra-sottochiave-SINTETICA-di-prova-32-byte"), firmephoenix.Segreto{})
	if _, err := b.PaginaArtigiano(ctx, itoa(c.ID), g, pubblicoChi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("altra chiave: %v", err)
	}
	b.ConLinkArtigiano(nil, firmephoenix.Segreto{})
	if _, err := b.PaginaArtigiano(ctx, itoa(c.ID), g, pubblicoChi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("senza chiave: %v", err)
	}
	if b.GettoneArtigiano(c.ID) != "" {
		t.Error("gettone senza chiave")
	}
}

// inventario: ART-23, ART-24
// D-87: il Phoenix.Token dello standalone (sale «artisan bag projection», dato l'id) apre la pagina con il
// suo secret_key_base fino a 90 giorni compresi; uno firmato dopo adesso, scaduto, di un'altra busta, di un
// altro sale (la galleria, l'email) o con un altro segreto no; senza il segreto nessuno.
func TestPaginaArtigianoPhoenix(t *testing.T) {
	b, _, o := prova(t)
	conLink(b)
	c := crea(t, b, modulo())
	adesso := o.Adesso()
	g := standaloneProva.FirmaIntero(SaleArtigiano, c.ID, adesso.Add(-DurataLinkArtigiano))
	if _, err := b.PaginaArtigiano(ctx, itoa(c.ID), g, pubblicoChi); err != nil {
		t.Fatalf("phoenix: %v", err)
	}
	for nome, tok := range map[string]string{
		"scaduto":         standaloneProva.FirmaIntero(SaleArtigiano, c.ID, adesso.Add(-DurataLinkArtigiano-time.Millisecond)),
		"dopo adesso":     standaloneProva.FirmaIntero(SaleArtigiano, c.ID, adesso.Add(time.Millisecond)),
		"altra busta":     standaloneProva.FirmaIntero(SaleArtigiano, c.ID+1, adesso),
		"sale galleria":   standaloneProva.FirmaIntero("gallery thumb", c.ID, adesso),
		"sale email":      standaloneProva.FirmaIntero("email optout", c.ID, adesso),
		"altro segreto":   firmephoenix.Nuovo(strings.Repeat("ALTRO-SINTETICO-", 5)).FirmaIntero(SaleArtigiano, c.ID, adesso),
		"gettone del go":  "SFMyNTY" + strings.TrimPrefix(b.GettoneArtigiano(c.ID), "g1"),
		"phoenix come go": "g1." + strings.TrimPrefix(g, "SFMyNTY."),
	} {
		if _, err := b.PaginaArtigiano(ctx, itoa(c.ID), tok, pubblicoChi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("%s: %v", nome, err)
		}
	}
	b.ConLinkArtigiano(chiaveArtigianoProva, firmephoenix.Segreto{})
	if _, err := b.PaginaArtigiano(ctx, itoa(c.ID), g, pubblicoChi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("senza segreto dello standalone: %v", err)
	}
}

// inventario: ART-24, ART-25
// La proiezione: numero visibile senza «N.» (D-64, D-82 estesa nella 085), stato con le parole della pagina del cliente, lavorazione e
// le sole foto per l'artigiano; mai il cliente. La busta consegnata o senza più artigiano risponde
// ancora (lo standalone non guarda l'artigiano); quella nel cestino no. Il link del cliente non apre
// la pagina dell'artigiano e il gettone dell'artigiano non apre /r/.
func TestPaginaArtigianoProiezione(t *testing.T) {
	b, pf := conFoto(t)
	conLink(b)
	esegui(t, b.d, `UPDATE buste SET artigiano_id = 1, stato = 'consegnata', codice_qr = 'RIP-2026-0045' WHERE id = ?`, pf.busta)
	ff := allega(t, b, pf.busta, pf.due())
	// La prima foto è la foto della busta: mai all'artigiano. Nascondere la seconda al cliente non conta.
	if _, err := b.ImpostaVisibilita(ctx, pf.busta, ff[1].ID, "customer", false, chi); err != nil {
		t.Fatal(err)
	}
	g := b.GettoneArtigiano(pf.busta)
	p, err := b.PaginaArtigiano(ctx, itoa(pf.busta), g, pubblicoChi)
	if err != nil {
		t.Fatal(err)
	}
	if p.Numero != "0045/26" || p.Stato != "Consegnata" || p.Descrizione != "Anello da stringere" || len(p.Foto) != 1 || p.Foto[0].ID != ff[1].ID {
		t.Errorf("proiezione: %+v", p)
	}
	esegui(t, b.d, `UPDATE buste SET artigiano_id = NULL, codice_qr = NULL WHERE id = ?`, pf.busta)
	// D-82 estesa (slice 085): senza etichetta il numero dei testi, senza «N.» davanti.
	if p, err = b.PaginaArtigiano(ctx, itoa(pf.busta), g, pubblicoChi); err != nil || p.Numero == "" || strings.HasPrefix(p.Numero, "N.") ||
		p.Numero != NumeroNeiTesti("", p.numeroInterno) {
		t.Errorf("senza artigiano: %v %+v", err, p)
	}
	if _, err := b.PaginaCliente(ctx, g); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("il gettone dell'artigiano apre /r/: %v", err)
	}
	link, err := b.NuovoLink(ctx, pf.busta, chi)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.PaginaArtigiano(ctx, itoa(pf.busta), link.Token, pubblicoChi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("il link del cliente apre la pagina dell'artigiano: %v", err)
	}
	esegui(t, b.d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, pf.busta)
	if _, err := b.PaginaArtigiano(ctx, itoa(pf.busta), g, pubblicoChi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("nel cestino: %v", err)
	}
	if _, err := b.PaginaArtigiano(ctx, "999999", b.GettoneArtigiano(999999), pubblicoChi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("busta che non c'è: %v", err)
	}
}

// inventario: ART-25
// Le foto del canale artigiano per id: con il gettone della busta, la foto visibile all'artigiano
// (intera e miniatura) con la riga repair.public_photo_viewed «artisan»; una nascosta all'artigiano,
// di un'altra busta o un id non valido sono nil con la riga denied; un gettone che non apre la busta
// è nil senza righe.
func TestLeggiFotoArtigiano(t *testing.T) {
	b, pf := conFoto(t)
	conLink(b)
	ff := allega(t, b, pf.busta, pf.due())
	g := b.GettoneArtigiano(pf.busta)
	m, err := b.LeggiFotoArtigiano(ctx, itoa(pf.busta), g, itoa(ff[1].ID), VarianteIntera, chi)
	if err != nil || m == nil {
		t.Fatalf("foto: %v %v", m, err)
	}
	if a := ultimoAudit(t, b.d, "repair.public_photo_viewed"); a != `repair.public_photo_viewed|repair|`+itoa(pf.busta)+`|ok|{"audience":"artisan","photo_id":`+itoa(ff[1].ID)+`}` {
		t.Errorf("audit: %q", a)
	}
	if m, err := b.LeggiFotoArtigiano(ctx, itoa(pf.busta), g, itoa(ff[1].ID), VarianteMiniatura, chi); err != nil || m == nil {
		t.Errorf("miniatura: %v", err)
	}
	for nome, foto := range map[string]string{"foto della busta": itoa(ff[0].ID), "non c'è": "999999", "non valida": "x1"} {
		if m, err := b.LeggiFotoArtigiano(ctx, itoa(pf.busta), g, foto, VarianteIntera, chi); err != nil || m != nil {
			t.Errorf("%s: %v %v", nome, m, err)
		}
		if a := ultimoAudit(t, b.d, "repair.public_photo_viewed"); !strings.Contains(a, "|denied|") {
			t.Errorf("%s: audit %q", nome, a)
		}
	}
	righe := contaAzione(t, b, "repair.public_photo_viewed")
	for nome, x := range map[string][2]string{"gettone sbagliato": {itoa(pf.busta), g + "x"}, "altra busta": {"999999", g}} {
		if m, err := b.LeggiFotoArtigiano(ctx, x[0], x[1], itoa(ff[1].ID), VarianteIntera, chi); err != nil || m != nil {
			t.Errorf("%s: %v %v", nome, m, err)
		}
	}
	if contaAzione(t, b, "repair.public_photo_viewed") != righe {
		t.Error("un gettone che non apre scrive righe")
	}
}

// inventario: ART-26
// I vecchi link Flask /artigiano/<busta>/<anno>?t=<exp>.<16hex>: la firma sul numero scritto apre la
// pagina (D-77: «0046/26» salvata «46/26»; e «46/26» col suo), con repair.legacy_artisan_viewed;
// ogni rifiuto (scaduto, toccato, la firma di /stato, altro numero, nel cestino, senza segreto) è lo
// stesso ErrBustaNonTrovata con repair.legacy_artisan_denied senza busta. Le foto per nome file con
// lo stesso gettone, solo quelle dell'artigiano.
func TestPaginaArtigianoStorica(t *testing.T) {
	b, pf := conFoto(t)
	conLink(b)
	s := firmeflask.Nuovo(segretoProva)
	b.ConSegretoFlask(s)
	esegui(t, b.d, `UPDATE buste SET numero = '46/26', numero_import = '0046/26' WHERE id = ?`, pf.busta)
	ff := allega(t, b, pf.busta, pf.due())
	adesso := b.o.Adesso().Unix()
	scritto := s.FirmaArtigiano("0046/26", adesso)
	for nome, x := range map[string][2]string{
		"scritto": {"0046/26", scritto},
		"salvato": {"46/26", s.FirmaArtigiano("46/26", adesso)},
	} {
		p, err := b.PaginaArtigianoStorica(ctx, x[0], x[1], pubblicoChi)
		if err != nil || p.ID != pf.busta || len(p.Foto) != 1 {
			t.Errorf("%s: %v %+v", nome, err, p)
		}
	}
	if a := ultimoAudit(t, b.d, "repair.legacy_artisan_viewed"); a != "repair.legacy_artisan_viewed|repair|"+itoa(pf.busta)+"|ok|{}" {
		t.Errorf("audit: %q", a)
	}
	rifiuti := map[string][2]string{
		"scaduto":        {"0046/26", s.FirmaArtigiano("0046/26", adesso-1)},
		"toccato":        {"0046/26", scritto[:len(scritto)-1] + "0"},
		"altro numero":   {"0047/26", scritto},
		"firma di stato": {"0046/26", itoa(adesso) + "." + s.FirmaFotoBusta("x", "y", adesso)[:16]},
		"gettone del go": {"0046/26", b.GettoneArtigiano(pf.busta)},
		"vuoto":          {"0046/26", ""},
		// Lo standalone cerca il numero esatto: la firma del numero salvato con un altro scritto no.
		"firma del salvato": {"046/26", s.FirmaArtigiano("46/26", adesso)},
	}
	for nome, x := range rifiuti {
		if _, err := b.PaginaArtigianoStorica(ctx, x[0], x[1], pubblicoChi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("%s: %v", nome, err)
		}
	}
	if n := contaAzione(t, b, "repair.legacy_artisan_denied"); n != len(rifiuti) {
		t.Errorf("rifiuti scritti: %d", n)
	}
	if a := ultimoAudit(t, b.d, "repair.legacy_artisan_denied"); a != "repair.legacy_artisan_denied|repair|unknown|ok|{}" {
		t.Errorf("audit del rifiuto: %q", a)
	}
	// Il vecchio link non apre la pagina nuova, né il contrario.
	if _, err := b.PaginaArtigiano(ctx, itoa(pf.busta), scritto, pubblicoChi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("il gettone Flask apre la pagina nuova: %v", err)
	}
	m, err := b.LeggiFotoArtigianoStorica(ctx, "0046/26", scritto, ff[1].NomeFile, VarianteMiniatura, chi)
	if err != nil || m == nil {
		t.Fatalf("foto storica: %v %v", m, err)
	}
	if a := ultimoAudit(t, b.d, "repair.public_photo_viewed"); !strings.Contains(a, `"audience":"artisan","photo_id":"filename"`) {
		t.Errorf("audit della foto: %q", a)
	}
	if m, _ := b.LeggiFotoArtigianoStorica(ctx, "0046/26", scritto, ff[0].NomeFile, VarianteIntera, chi); m != nil {
		t.Error("foto della busta servita all'artigiano")
	}
	// Nomi ostili: solo il nome esatto di una foto della busta, mai un percorso.
	for _, nome := range []string{"../" + ff[1].NomeFile, "..%2F" + ff[1].NomeFile, "/" + ff[1].NomeFile, "../../secrets/segreto_flask", "%2E%2E"} {
		if m, err := b.LeggiFotoArtigianoStorica(ctx, "0046/26", scritto, nome, VarianteIntera, chi); err != nil || m != nil {
			t.Errorf("nome ostile %q: %v %v", nome, m, err)
		}
	}
	righe := contaAzione(t, b, "repair.public_photo_viewed")
	if m, err := b.LeggiFotoArtigianoStorica(ctx, "0046/26", rifiuti["scaduto"][1], ff[1].NomeFile, VarianteIntera, chi); err != nil || m != nil || contaAzione(t, b, "repair.public_photo_viewed") != righe {
		t.Errorf("foto con il gettone scaduto: %v %v", m, err)
	}
	esegui(t, b.d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, pf.busta)
	if _, err := b.PaginaArtigianoStorica(ctx, "0046/26", scritto, pubblicoChi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("nel cestino: %v", err)
	}
	if m, _ := b.LeggiFotoArtigianoStorica(ctx, "0046/26", scritto, ff[1].NomeFile, VarianteIntera, chi); m != nil {
		t.Error("foto della busta nel cestino servita")
	}
	esegui(t, b.d, `UPDATE buste SET cancellata_il = NULL WHERE id = ?`, pf.busta)
	b.ConSegretoFlask(firmeflask.Segreto{})
	if _, err := b.PaginaArtigianoStorica(ctx, "0046/26", scritto, pubblicoChi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("senza segreto: %v", err)
	}
}

// inventario: ART-23
// {link} del testo wa_artigiano diventa il link dell'artigiano (indirizzo_pubblico + /artigiano/<id>?t=),
// che apre la pagina; un testo senza {link} torna com'è; senza indirizzo pubblico il testo con {link} non
// parte (D-46 estesa).
func TestRisolviLinkArtigiano(t *testing.T) {
	b, _, _ := prova(t)
	conLink(b)
	c := crea(t, b, modulo())
	if s, err := b.RisolviLinkArtigiano(c.ID, "Ciao"); err != nil || s != "Ciao" {
		t.Errorf("senza segnaposto: %q %v", s, err)
	}
	if _, err := b.RisolviLinkArtigiano(c.ID, "Vedi {link}"); !errors.Is(err, ErrSenzaIndirizzoPubblico) {
		t.Errorf("senza indirizzo: %v", err)
	}
	if b.LinkArtigiano(c.ID) != "" {
		t.Error("link senza indirizzo pubblico")
	}
	b.ConIndirizzoPubblico("https://negozio.example")
	s, err := b.RisolviLinkArtigiano(c.ID, "Vedi {link} e {link}")
	if err != nil || !strings.HasPrefix(s, "Vedi https://negozio.example/artigiano/"+itoa(c.ID)+"?t=g1.") || strings.Contains(s, "{link}") {
		t.Fatalf("%q %v", s, err)
	}
	link := strings.Fields(s)[1]
	if _, err := b.PaginaArtigiano(ctx, itoa(c.ID), tokDi(link), pubblicoChi); err != nil {
		t.Errorf("il link non apre: %v", err)
	}
}
