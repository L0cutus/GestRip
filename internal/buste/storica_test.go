package buste

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/firmeflask"
)

// I link storici /stato, /foto e /thumb (CLI-16…20), come public_repair_controller_test.exs,
// public_repair_legacy_revocation_test.exs, public_repair_thread_test.exs e
// public_repair_message_test.exs dello standalone (go-baseline). Segreto e codici inventati.

const segretoProva = "synthetic-legacy-secret"

// firmaStatoProva calcola qui, senza firmeflask, la firma «stato:<numero>:<exp>» troncata a 32.
func firmaStatoProva(numero string, exp int64) string {
	m := hmac.New(sha256.New, []byte(segretoProva))
	m.Write([]byte("stato:" + numero + ":" + strconv.FormatInt(exp, 10)))
	return hex.EncodeToString(m.Sum(nil))[:32]
}

func credCodice(c string) CredenzialeStorica { return CredenzialeStorica{Codice: c, ConCodice: true} }

func credFirma(f string, exp int64) CredenzialeStorica {
	return CredenzialeStorica{Firma: f, Scadenza: strconv.FormatInt(exp, 10), ConFirma: true, ConScadenza: true}
}

// storica crea una busta con il numero salvato e, se codiceSu non è vuoto, l'impronta del codice
// «CODICE» calcolata su quel numero (il numero com'era nello standalone quando l'ha importata).
func storica(t *testing.T, b *Buste, salvato, codiceSu string, f func(*Accettazione)) Creata {
	t.Helper()
	a := modulo()
	if f != nil {
		f(&a)
	}
	c := crea(t, b, a)
	esegui(t, b.d, `UPDATE buste SET numero = ? WHERE id = ?`, salvato, c.ID)
	if codiceSu != "" {
		esegui(t, b.d, `UPDATE buste SET hash_codice_flask = ? WHERE id = ?`, firmeflask.ImprontaCodice(codiceSu, "CODICE"), c.ID)
	}
	c.Numero = salvato
	return c
}

func contaAzione(t *testing.T, b *Buste, azione string) int {
	t.Helper()
	return conta(t, b.d, `SELECT count(*) FROM eventi_audit WHERE azione = ?`, azione)
}

// inventario: CLI-16
// D-77: la busta importata «0046/26» è salvata «46/26»; il codice è stato impresso sul numero com'era.
// Il link con il numero scritto («/stato/0046/26?pw=») si apre; lo stesso codice su «46/26» o
// «046/26» no. Ogni rifiuto è lo stesso ErrBustaNonTrovata, con la riga senza busta; l'apertura
// scrive repair.legacy_public_viewed con la busta.
func TestPaginaStoricaCodiceSulNumeroScritto(t *testing.T) {
	b, d, _ := prova(t)
	c := storica(t, b, "46/26", "0046/26", nil)
	p, err := b.PaginaStorica(ctx, "0046/26", credCodice(" codice "), chi)
	if err != nil || p.ID != c.ID {
		t.Fatalf("0046/26: %v %d", err, p.ID)
	}
	if g := ultimoAudit(t, d, "repair.legacy_public_viewed"); g != "repair.legacy_public_viewed|repair|"+itoa(c.ID)+"|ok|{}" {
		t.Errorf("audit: %q", g)
	}
	for _, x := range []struct {
		scritto string
		cred    CredenzialeStorica
	}{
		{"46/26", credCodice("CODICE")},
		{"046/26", credCodice("CODICE")},
		{"0046/26", credCodice("ALTRO")},
		{"0046/26", credCodice("")},
		{"0046/26", CredenzialeStorica{Codice: "CODICE"}}, // il parametro non è un testo
		{"0046/26", CredenzialeStorica{}},
		{"0047/26", credCodice("CODICE")},
		{"0046/25", credCodice("CODICE")},
	} {
		if _, err := b.PaginaStorica(ctx, x.scritto, x.cred, chi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("%s %+v: %v", x.scritto, x.cred, err)
		}
	}
	if n := contaAzione(t, b, "repair.legacy_public_denied"); n != 8 {
		t.Errorf("rifiuti scritti: %d", n)
	}
	if g := ultimoAudit(t, d, "repair.legacy_public_denied"); g != "repair.legacy_public_denied|repair|unknown|ok|{}" {
		t.Errorf("rifiuto: %q", g)
	}
	if n := contaAzione(t, b, "repair.legacy_public_viewed"); n != 1 {
		t.Errorf("aperture: %d", n)
	}
}

// inventario: CLI-16
// Una busta che nello standalone era già senza zeri: si apre anche con gli zeri scritti in più,
// come lo standalone che cerca senza zeri e verifica sul numero salvato.
func TestPaginaStoricaNumeroSalvato(t *testing.T) {
	b, _, _ := prova(t)
	c := storica(t, b, "47/26", "47/26", nil)
	for _, scritto := range []string{"47/26", "047/26", "00047/26"} {
		if p, err := b.PaginaStorica(ctx, scritto, credCodice("codice"), chi); err != nil || p.ID != c.ID {
			t.Errorf("%s: %v", scritto, err)
		}
	}
	// Spazi nel numero: non si tolgono (la rotta non li toglie), la busta non c'è.
	if _, err := b.PaginaStorica(ctx, " 47/26", credCodice("codice"), chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("con lo spazio: %v", err)
	}
}

// inventario: CLI-16
// La firma t/exp del segreto Flask: il vettore dello standalone su «42/26»; la firma di «0046/26»
// apre la busta riscritta «46/26» solo con il numero scritto così; scade dopo exp (exp ≥ adesso);
// senza segreto nessuna firma passa, il codice sì.
func TestPaginaStoricaFirma(t *testing.T) {
	b, _, o := prova(t)
	vettore := storica(t, b, "42/26", "", nil)
	riscritta := storica(t, b, "46/26", "46/26", nil)
	b.ConSegretoFlask(firmeflask.Nuovo(segretoProva))
	if p, err := b.PaginaStorica(ctx, "42/26", credFirma("dabbdeed7a356af74ce85ca0fd25b859", 2_000_000_000), chi); err != nil || p.ID != vettore.ID {
		t.Fatalf("vettore: %v", err)
	}
	f := firmaStatoProva("0046/26", 2_000_000_000)
	if p, err := b.PaginaStorica(ctx, "0046/26", credFirma(f, 2_000_000_000), chi); err != nil || p.ID != riscritta.ID {
		t.Errorf("0046/26: %v", err)
	}
	if _, err := b.PaginaStorica(ctx, "46/26", credFirma(f, 2_000_000_000), chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("la firma di 0046/26 su 46/26: %v", err)
	}
	adesso := o.Adesso().Unix()
	if _, err := b.PaginaStorica(ctx, "46/26", credFirma(firmaStatoProva("46/26", adesso), adesso), chi); err != nil {
		t.Errorf("alla scadenza esatta: %v", err)
	}
	for nome, cred := range map[string]CredenzialeStorica{
		"scaduta":       credFirma(firmaStatoProva("46/26", adesso-1), adesso-1),
		"exp cambiata":  credFirma(firmaStatoProva("46/26", adesso+10), adesso+11),
		"firma di zeri": credFirma(strings.Repeat("0", 32), 2_000_000_000),
		"senza exp":     {Firma: firmaStatoProva("46/26", 2_000_000_000), ConFirma: true},
	} {
		if _, err := b.PaginaStorica(ctx, "46/26", cred, chi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("%s: %v", nome, err)
		}
	}
	b.ConSegretoFlask(firmeflask.Segreto{})
	if _, err := b.PaginaStorica(ctx, "42/26", credFirma("dabbdeed7a356af74ce85ca0fd25b859", 2_000_000_000), chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("senza segreto: %v", err)
	}
	if _, err := b.PaginaStorica(ctx, "46/26", credCodice("codice"), chi); err != nil {
		t.Errorf("il codice senza segreto: %v", err)
	}
}

// inventario: CLI-16
// D-77 con il numero occupato: la busta rimasta «0046/26» e l'altra «46/26». Il link di «0046/26»
// apre la sua busta, mai l'altra; nel cestino è una busta che non c'è e non lascia cercare l'altra.
func TestPaginaStoricaNumeroComEScrittoPrima(t *testing.T) {
	b, d, _ := prova(t)
	b.ConSegretoFlask(firmeflask.Nuovo(segretoProva))
	zeri := storica(t, b, "0046/26", "", nil)
	altra := storica(t, b, "46/26", "", nil)
	f := firmaStatoProva("0046/26", 2_000_000_000)
	if p, err := b.PaginaStorica(ctx, "0046/26", credFirma(f, 2_000_000_000), chi); err != nil || p.ID != zeri.ID {
		t.Errorf("0046/26 apre %d (%v), attesa %d", p.ID, err, zeri.ID)
	}
	if p, err := b.PaginaStorica(ctx, "46/26", credFirma(firmaStatoProva("46/26", 2_000_000_000), 2_000_000_000), chi); err != nil || p.ID != altra.ID {
		t.Errorf("46/26: %v", err)
	}
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, zeri.ID)
	if p, err := b.PaginaStorica(ctx, "0046/26", credFirma(f, 2_000_000_000), chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("nel cestino: %v, apre %d", err, p.ID)
	}
}

// inventario: CLI-18
// La revoca: la credenziale giusta (codice o firma) dà ErrLinkStoricoRevocato con la riga che nomina
// la busta e reason: revoked; una sbagliata il rifiuto di sempre senza busta. Poll e messaggio
// rifiutano anche la credenziale giusta, con la riga senza busta, e non scrivono niente.
func TestStoricaRevocata(t *testing.T) {
	b, d, _ := prova(t)
	b.ConSegretoFlask(firmeflask.Nuovo(segretoProva))
	c := storica(t, b, "42/26", "42/26", nil)
	if _, err := b.RevocaLink(ctx, c.ID, chi); err != nil {
		t.Fatal(err)
	}
	for _, cred := range []CredenzialeStorica{credCodice("codice"), credFirma("dabbdeed7a356af74ce85ca0fd25b859", 2_000_000_000)} {
		if _, err := b.PaginaStorica(ctx, "42/26", cred, chi); !errors.Is(err, ErrLinkStoricoRevocato) {
			t.Errorf("%+v: %v", cred, err)
		}
		if g := ultimoAudit(t, d, "repair.legacy_public_denied"); g != `repair.legacy_public_denied|repair|`+itoa(c.ID)+`|ok|{"reason":"revoked"}` {
			t.Errorf("audit: %q", g)
		}
		if _, err := b.ConversazioneStorica(ctx, "42/26", cred, chi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("poll: %v", err)
		}
		if g := ultimoAudit(t, d, "repair.legacy_public_denied"); g != "repair.legacy_public_denied|repair|unknown|ok|{}" {
			t.Errorf("audit del poll: %q", g)
		}
		if err := b.ScriviMessaggioStorico(ctx, "42/26", cred, "Ciao", chi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("messaggio: %v", err)
		}
	}
	for _, cred := range []CredenzialeStorica{credCodice("sbagliato"), credFirma(strings.Repeat("0", 32), 2_000_000_000), {}} {
		if _, err := b.PaginaStorica(ctx, "42/26", cred, chi); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("sbagliata %+v: %v", cred, err)
		}
		if g := ultimoAudit(t, d, "repair.legacy_public_denied"); g != "repair.legacy_public_denied|repair|unknown|ok|{}" {
			t.Errorf("audit del rifiuto: %q", g)
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM messaggi_buste`); n != 0 {
		t.Errorf("messaggi scritti: %d", n)
	}
	if n := contaAzione(t, b, "repair.legacy_public_viewed"); n != 0 {
		t.Errorf("aperture: %d", n)
	}
}

// inventario: CLI-17, CLI-28
// Contenuto ridotto: niente preventivo (anche in «preventivo»), le foto della colonna «cliente»;
// busta diretta con prezzi e conversazione; busta B2B senza prezzi, senza conversazione e senza
// foto (la classe esterna non ha la colonna «cliente»). Le foto hanno lo slug del numero scritto e
// la firma di un'ora.
func TestPaginaStoricaContenuto(t *testing.T) {
	b, p := conFoto(t)
	b.ConSegretoFlask(firmeflask.Nuovo(segretoProva))
	esegui(t, b.d, `UPDATE buste SET numero = '46/26', hash_codice_flask = ?, stato = 'preventivo', preventivo_testo = '40',
		prezzo_centesimi = 5000 WHERE id = ?`, firmeflask.ImprontaCodice("0046/26", "CODICE"), p.busta)
	ff := allega(t, b, p.busta, p.due())
	if _, err := b.ImpostaVisibilita(ctx, p.busta, ff[1].ID, "customer", false, chi); err != nil {
		t.Fatal(err)
	}
	rigaConv(t, b, p.busta, "shop", nil, "Dal negozio", istanteProva)
	pg, err := b.PaginaStorica(ctx, "0046/26", credCodice("codice"), chi)
	if err != nil {
		t.Fatal(err)
	}
	if pg.Risposta.Fase != RispostaNonApplicabile || pg.Preventivo != "40" || pg.PrezzoFinale != "€ 50,00" {
		t.Errorf("diretta: %+v %q %q", pg.Risposta, pg.Preventivo, pg.PrezzoFinale)
	}
	if !pg.ConConversazione || testi(pg.Conversazione) != "shop:negozio:Dal negozio" {
		t.Errorf("conversazione: %v %q", pg.ConConversazione, testi(pg.Conversazione))
	}
	if len(pg.Foto) != 1 || pg.Foto[0].ID != ff[0].ID {
		t.Fatalf("foto: %+v", pg.Foto)
	}
	scad := b.o.Adesso().Unix() + 3600
	if pg.SlugFoto != "0046_26" || pg.ScadenzaFoto != strconv.FormatInt(scad, 10) || len(pg.FirmeFoto) != 1 ||
		pg.FirmeFoto[0] != firmeflask.Nuovo(segretoProva).FirmaFotoBusta("0046_26", ff[0].NomeFile, scad) {
		t.Errorf("firme: %q %q %q", pg.SlugFoto, pg.ScadenzaFoto, pg.FirmeFoto)
	}
	if strings.HasPrefix(pg.Numero, "N.") || pg.Numero != "46/26" {
		t.Errorf("numero visibile: %q", pg.Numero)
	}
	// B2B: niente prezzi, niente conversazione, niente foto.
	esegui(t, b.d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, p.busta)
	if _, err := b.ImpostaVisibilita(ctx, p.busta, ff[0].ID, "orefice", true, chi); err != nil {
		t.Fatal(err)
	}
	pg, err = b.PaginaStorica(ctx, "0046/26", credCodice("codice"), chi)
	if err != nil {
		t.Fatal(err)
	}
	if pg.Preventivo != "" || pg.PrezzoFinale != "" || pg.ConConversazione || pg.Conversazione != nil || len(pg.Foto) != 0 || !pg.Oreficeria {
		t.Errorf("b2b: %+v", pg)
	}
	// Senza segreto le firme sono vuote (sig= vuoto nello standalone).
	esegui(t, b.d, `UPDATE buste SET oreficeria_id = NULL WHERE id = ?`, p.busta)
	b.ConSegretoFlask(firmeflask.Segreto{})
	if pg, err = b.PaginaStorica(ctx, "0046/26", credCodice("codice"), chi); err != nil || len(pg.FirmeFoto) != 1 || pg.FirmeFoto[0] != "" {
		t.Errorf("senza segreto: %q %v", pg.FirmeFoto, err)
	}
}

// inventario: CLI-19, CLI-11
// Il poll storico: le righe della conversazione con la credenziale giusta, senza audit; ogni rifiuto
// (credenziale, B2B, chiusa, cestino) è ErrBustaNonTrovata con la riga senza busta.
func TestConversazioneStorica(t *testing.T) {
	b, d, _ := prova(t)
	c := storica(t, b, "79/26", "79/26", nil)
	rigaConv(t, b, c.ID, "shop", nil, "Pronta venerdì", istanteProva)
	audit0 := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	ms, err := b.ConversazioneStorica(ctx, "79/26", credCodice("CODICE"), chi)
	if err != nil || testi(ms) != "shop:negozio:Pronta venerdì" {
		t.Fatalf("%q %v", testi(ms), err)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit`); n != audit0 {
		t.Errorf("il poll riuscito scrive: %d righe", n-audit0)
	}
	if _, err := b.ConversazioneStorica(ctx, "79/26", credCodice("SBAGLIATO"), chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("sbagliato: %v", err)
	}
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, c.ID)
	if _, err := b.ConversazioneStorica(ctx, "79/26", credCodice("CODICE"), chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("b2b: %v", err)
	}
	esegui(t, d, `UPDATE buste SET oreficeria_id = 1, stato = 'consegnata' WHERE id = ?`, c.ID)
	if _, err := b.ConversazioneStorica(ctx, "79/26", credCodice("CODICE"), chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("consegnata: %v", err)
	}
	esegui(t, d, `UPDATE buste SET stato = 'in_lavorazione', cancellata_il = ? WHERE id = ?`, istanteProva, c.ID)
	if _, err := b.ConversazioneStorica(ctx, "79/26", credCodice("CODICE"), chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("cestino: %v", err)
	}
	if n := contaAzione(t, b, "repair.legacy_public_denied"); n != 4 {
		t.Errorf("rifiuti: %d", n)
	}
}

// inventario: CLI-19, CLI-12
// Il messaggio storico: la credenziale prima del testo; poi le regole del messaggio del cliente; una
// busta B2B rifiuta come il codice sbagliato (con la riga) dopo il controllo del testo, come
// PublicMessage.post/5; una consegnata è ErrConversazioneChiusa.
func TestScriviMessaggioStorico(t *testing.T) {
	b, d, _ := prova(t)
	c := storica(t, b, "46/26", "0046/26", nil)
	if err := b.ScriviMessaggioStorico(ctx, "0046/26", credCodice("ALTRO"), "", chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("credenziale sbagliata e testo vuoto: %v", err)
	}
	if err := b.ScriviMessaggioStorico(ctx, "0046/26", credCodice("CODICE"), "  ", chi); !errors.Is(err, ErrMessaggioVuoto) {
		t.Errorf("vuoto: %v", err)
	}
	if err := b.ScriviMessaggioStorico(ctx, "0046/26", credCodice("CODICE"), strings.Repeat("a", 1001), chi); !errors.Is(err, ErrMessaggioTroppoLungo) {
		t.Errorf("lungo: %v", err)
	}
	if err := b.ScriviMessaggioStorico(ctx, "0046/26", credCodice("CODICE"), " Dal vecchio link ", chi); err != nil {
		t.Fatal(err)
	}
	if g := conta(t, d, `SELECT count(*) FROM messaggi_buste WHERE busta_id = ? AND autore = 'customer' AND testo = 'Dal vecchio link'`, c.ID); g != 1 {
		t.Errorf("righe: %d", g)
	}
	if g := ultimoAudit(t, d, "repair.message_appended"); !strings.Contains(g, `"author":"customer"`) || !strings.Contains(g, `"bag_number":"46/26"`) {
		t.Errorf("audit: %q", g)
	}
	if contaAzione(t, b, "repair.message_notice_not_queued") != 1 {
		t.Error("manca l'avviso non accodato")
	}
	// B2B: il testo valido è rifiutato come il codice sbagliato; il testo vuoto prima dice vuoto.
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, c.ID)
	prima := contaAzione(t, b, "repair.legacy_public_denied")
	if err := b.ScriviMessaggioStorico(ctx, "0046/26", credCodice("CODICE"), "x", chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("b2b: %v", err)
	}
	if contaAzione(t, b, "repair.legacy_public_denied") != prima+1 {
		t.Error("b2b senza la riga del rifiuto")
	}
	if err := b.ScriviMessaggioStorico(ctx, "0046/26", credCodice("CODICE"), "", chi); !errors.Is(err, ErrMessaggioVuoto) {
		t.Errorf("b2b vuoto: %v", err)
	}
	esegui(t, d, `UPDATE buste SET oreficeria_id = NULL, stato = 'consegnata' WHERE id = ?`, c.ID)
	if err := b.ScriviMessaggioStorico(ctx, "0046/26", credCodice("CODICE"), "x", chi); !errors.Is(err, ErrConversazioneChiusa) {
		t.Errorf("consegnata: %v", err)
	}
	if g := conta(t, d, `SELECT count(*) FROM messaggi_buste`); g != 1 {
		t.Errorf("righe in tutto: %d", g)
	}
}

// inventario: CLI-20
// Le foto storiche: firma su slug com'è scritto e nome del file; la busta riscritta si trova senza
// gli zeri; solo le foto visibili al cliente; la riga repair.public_photo_viewed solo con la firma
// giusta; revocata, firma sbagliata, scaduta o di un altro file: nil senza righe.
func TestLeggiFotoStorica(t *testing.T) {
	b, p := conFoto(t)
	b.ConSegretoFlask(firmeflask.Nuovo(segretoProva))
	esegui(t, b.d, `UPDATE buste SET numero = '46/26' WHERE id = ?`, p.busta)
	ff := allega(t, b, p.busta, p.due())
	adesso := b.o.Adesso().Unix()
	s := firmeflask.Nuovo(segretoProva)
	firma := func(slug, file string, exp int64) (string, string) {
		return s.FirmaFotoBusta(slug, file, exp), strconv.FormatInt(exp, 10)
	}
	f, e := firma("0046_26", ff[0].NomeFile, adesso)
	m, err := b.LeggiFotoStorica(ctx, "0046_26", ff[0].NomeFile, f, e, VarianteIntera, chi)
	if err != nil || m == nil || m.Variante != VarianteIntera {
		t.Fatalf("foto: %v %v", m, err)
	}
	if g := ultimoAudit(t, b.d, "repair.public_photo_viewed"); g != `repair.public_photo_viewed|repair|`+itoa(p.busta)+`|ok|{"audience":"customer","photo_id":"filename"}` {
		t.Errorf("audit: %q", g)
	}
	if m, err = b.LeggiFotoStorica(ctx, "0046_26", ff[0].NomeFile, f, e, VarianteMiniatura, chi); err != nil || m == nil {
		t.Errorf("miniatura: %v", err)
	}
	if g := ultimoAudit(t, b.d, "repair.public_photo_viewed"); !strings.Contains(g, `"variant":"full"`) {
		t.Errorf("audit della miniatura (senza cache ripiega sulla foto): %q", g)
	}
	righe := contaAzione(t, b, "repair.public_photo_viewed")
	// Rifiuti senza righe: slug com'è salvato con la firma dello slug scritto, altro file con la
	// stessa firma, firma di zeri, scaduta, altra busta.
	for nome, x := range map[string][4]string{
		"slug salvato":  {"46_26", ff[0].NomeFile, f, e},
		"altro file":    {"0046_26", "altro.jpg", f, e},
		"firma di zeri": {"0046_26", ff[0].NomeFile, strings.Repeat("0", 32), e},
		"exp diversa":   {"0046_26", ff[0].NomeFile, f, strconv.FormatInt(adesso+1, 10)},
		"altra busta":   {"0047_26", ff[0].NomeFile, f, e},
	} {
		if m, err := b.LeggiFotoStorica(ctx, x[0], x[1], x[2], x[3], VarianteIntera, chi); err != nil || m != nil {
			t.Errorf("%s: %v %v", nome, m, err)
		}
	}
	sf, se := firma("0046_26", ff[0].NomeFile, adesso-1)
	if m, _ := b.LeggiFotoStorica(ctx, "0046_26", ff[0].NomeFile, sf, se, VarianteIntera, chi); m != nil {
		t.Error("scaduta servita")
	}
	if n := contaAzione(t, b, "repair.public_photo_viewed"); n != righe {
		t.Errorf("righe dei rifiuti senza firma: %d", n-righe)
	}
	// Firma giusta di un file che non c'è, o di una foto nascosta al cliente: nil con la riga denied.
	nf, ne := firma("0046_26", "manca.jpg", adesso)
	if m, err := b.LeggiFotoStorica(ctx, "0046_26", "manca.jpg", nf, ne, VarianteIntera, chi); err != nil || m != nil {
		t.Errorf("file che non c'è: %v %v", m, err)
	}
	if g := ultimoAudit(t, b.d, "repair.public_photo_viewed"); !strings.Contains(g, "|denied|") {
		t.Errorf("audit del file che non c'è: %q", g)
	}
	if _, err := b.ImpostaVisibilita(ctx, p.busta, ff[0].ID, "customer", false, chi); err != nil {
		t.Fatal(err)
	}
	if m, _ := b.LeggiFotoStorica(ctx, "0046_26", ff[0].NomeFile, f, e, VarianteIntera, chi); m != nil {
		t.Error("foto nascosta al cliente servita")
	}
	if _, err := b.ImpostaVisibilita(ctx, p.busta, ff[0].ID, "customer", true, chi); err != nil {
		t.Fatal(err)
	}
	// B2B: la colonna «cliente» non c'è.
	esegui(t, b.d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, p.busta)
	if m, _ := b.LeggiFotoStorica(ctx, "0046_26", ff[0].NomeFile, f, e, VarianteIntera, chi); m != nil {
		t.Error("b2b servita")
	}
	esegui(t, b.d, `UPDATE buste SET oreficeria_id = NULL WHERE id = ?`, p.busta)
	// Revocata: nil anche con la firma giusta, senza righe.
	if _, err := b.RevocaLink(ctx, p.busta, chi); err != nil {
		t.Fatal(err)
	}
	righe = contaAzione(t, b, "repair.public_photo_viewed")
	if m, err := b.LeggiFotoStorica(ctx, "0046_26", ff[0].NomeFile, f, e, VarianteIntera, chi); err != nil || m != nil {
		t.Errorf("revocata: %v %v", m, err)
	}
	if contaAzione(t, b, "repair.public_photo_viewed") != righe {
		t.Error("la revocata scrive una riga")
	}
	// Senza segreto nessuna firma passa.
	esegui(t, b.d, `UPDATE buste SET codice_flask_revocato_il = NULL WHERE id = ?`, p.busta)
	b.ConSegretoFlask(firmeflask.Segreto{})
	if m, _ := b.LeggiFotoStorica(ctx, "0046_26", ff[0].NomeFile, f, e, VarianteIntera, chi); m != nil {
		t.Error("senza segreto servita")
	}
}

// inventario: CLI-18, CLI-19
// La revoca arrivata dopo il controllo della rotta: la scrittura storica la ricontrolla nella sua
// transazione e non scrive niente; il link della busta (:token) scrive lo stesso.
func TestScriviMessaggioStoricoRevocatoNelFrattempo(t *testing.T) {
	b, d, _ := prova(t)
	c := storica(t, b, "46/26", "0046/26", nil)
	esegui(t, d, `UPDATE buste SET codice_flask_revocato_il = ? WHERE id = ?`, istanteProva, c.ID)
	if err := b.scriviMessaggioPubblico(ctx, c.ID, true, "Dopo la revoca", chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("storico: %v", err)
	}
	if n := conta(t, d, `SELECT count(*) FROM messaggi_buste`); n != 0 {
		t.Errorf("righe scritte: %d", n)
	}
	if err := b.scriviMessaggioPubblico(ctx, c.ID, false, "Dal link della busta", chi); err != nil {
		t.Errorf("token: %v", err)
	}
}

// inventario: CLI-16
// Se la riga repair.legacy_public_viewed non si scrive, la pagina non si apre: è il rifiuto, come il
// with di legacy/2.
func TestPaginaStoricaAperturaNonScritta(t *testing.T) {
	b, d, _ := prova(t)
	storica(t, b, "46/26", "0046/26", nil)
	esegui(t, d, `CREATE TRIGGER no_apertura BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.legacy_public_viewed'
		BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if p, err := b.PaginaStorica(ctx, "0046/26", credCodice("CODICE"), chi); !errors.Is(err, ErrBustaNonTrovata) || p.ID != 0 {
		t.Errorf("apertura non scritta: %v %d", err, p.ID)
	}
	if n := contaAzione(t, b, "repair.legacy_public_denied"); n != 1 {
		t.Errorf("rifiuti: %d", n)
	}
}

// inventario: CLI-20
// Lo slug «0046_26» di una busta nel cestino ferma la ricerca: la foto con lo stesso nome della busta
// viva «46/26» non si serve, e nessuna riga si scrive.
func TestFotoStoricaSlugNelCestino(t *testing.T) {
	b, p := conFoto(t)
	b.ConSegretoFlask(firmeflask.Nuovo(segretoProva))
	esegui(t, b.d, `UPDATE buste SET numero = '46/26' WHERE id = ?`, p.busta)
	ff := allega(t, b, p.busta, p.due())
	zeri := storica(t, b, "0046/26", "", nil)
	esegui(t, b.d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, zeri.ID)
	adesso := b.o.Adesso().Unix()
	f := firmeflask.Nuovo(segretoProva).FirmaFotoBusta("0046_26", ff[0].NomeFile, adesso)
	righe := contaAzione(t, b, "repair.public_photo_viewed")
	if m, err := b.LeggiFotoStorica(ctx, "0046_26", ff[0].NomeFile, f, strconv.FormatInt(adesso, 10), VarianteIntera, chi); err != nil || m != nil {
		t.Errorf("servita dall'altra busta: %v %v", m, err)
	}
	if contaAzione(t, b, "repair.public_photo_viewed") != righe {
		t.Error("riga scritta")
	}
	// Tolta dal cestino, la stessa firma la trova lei (senza foto: denied), mai la «46/26».
	esegui(t, b.d, `UPDATE buste SET cancellata_il = NULL WHERE id = ?`, zeri.ID)
	if m, _ := b.LeggiFotoStorica(ctx, "0046_26", ff[0].NomeFile, f, strconv.FormatInt(adesso, 10), VarianteIntera, chi); m != nil {
		t.Error("servita la foto della 46/26")
	}
	if g := ultimoAudit(t, b.d, "repair.public_photo_viewed"); !strings.Contains(g, "|"+itoa(zeri.ID)+"|denied|") {
		t.Errorf("audit: %q", g)
	}
}
