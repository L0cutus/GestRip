package buste

import (
	"errors"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/audit"
)

// L'editor dei testi dei messaggi (CLI-38, ADM-13), come Outgoing.Templates dello standalone
// (go-baseline): le quindici famiglie della pagina (WhatsApp, Telegram cliente, email cliente), i
// predefiniti, le regole delle email, put/3 con il suo audit e preview/2. I casi di
// templates_test.exs. Dati inventati.

var titolareTesti = audit.Contesto{Attore: "titolare@esempio.it"}

// inventario: CLI-38, ADM-13
// Le quindici famiglie nell'ordine della pagina: le nove WhatsApp, le due Telegram, le quattro
// email; ognuna con l'etichetta, la chiave, i campi e un predefinito valido; l'esempio dell'anteprima
// riempie ogni campo dichiarato.
func TestFamiglieDellEditor(t *testing.T) {
	ordine := []string{"wa_preventivo_con_importo", "wa_preventivo_senza_importo", "wa_pronta_compro_oro", "wa_pronta",
		"wa_link_stato", "wa_cliente_aggiornamento", "wa_oreficeria", "wa_oreficeria_lotto", "wa_artigiano",
		"tg_cliente_cambio_stato", "tg_cliente_nuovo_messaggio",
		"email_conferma_oggetto", "email_conferma_testo", "email_pronta_oggetto", "email_pronta_testo"}
	if len(Famiglie) != len(ordine) {
		t.Fatalf("famiglie %d", len(Famiglie))
	}
	for i, f := range Famiglie {
		if string(f) != ordine[i] {
			t.Errorf("%d: %s", i, f)
		}
		if g, ok := FamigliaDi(ordine[i]); !ok || g != f {
			t.Errorf("FamigliaDi(%s)", ordine[i])
		}
		if f.Etichetta() == "" || f.Predefinito() == "" || len(f.Campi()) == 0 || f.Chiave() != "outgoing.template."+ordine[i] {
			t.Errorf("%s: dati mancanti", f)
		}
		if e := ErroreModello(f, f.Predefinito()); e != "" {
			t.Errorf("%s: predefinito non valido: %s", f, e)
		}
		es := f.Esempio()
		for _, c := range f.Campi() {
			if _, ok := es[c]; !ok {
				t.Errorf("%s: l'esempio non ha %s", f, c)
			}
		}
	}
	for _, nome := range []string{"", "wa_portale_orefice_invito", "WA_PRONTA", "wa_pronta "} {
		if _, ok := FamigliaDi(nome); ok {
			t.Errorf("FamigliaDi(%q) accettata", nome)
		}
	}
	etichette := map[Famiglia]string{FamTGCambioStato: "Cambio stato riparazione", FamTGNuovoMessaggio: "Nuovo messaggio dal negozio",
		FamEmailConfermaOggetto: "Email di conferma — oggetto", FamEmailConfermaTesto: "Email di conferma — testo",
		FamEmailProntaOggetto: "Email di pronta — oggetto", FamEmailProntaTesto: "Email di pronta — testo"}
	for f, e := range etichette {
		if f.Etichetta() != e {
			t.Errorf("%s: %q", f, f.Etichetta())
		}
	}
	campi := map[Famiglia]string{
		FamTGCambioStato: "nome emoji titolo_stato numero descrizione stato negozio", FamTGNuovoMessaggio: "nome numero testo negozio",
		FamEmailConfermaOggetto: "nome numero negozio", FamEmailConfermaTesto: "nome numero descrizione negozio link_conferma",
		FamEmailProntaOggetto: "nome numero negozio", FamEmailProntaTesto: "nome numero descrizione negozio link_stato"}
	for f, c := range campi {
		if got := strings.Join(f.Campi(), " "); got != c {
			t.Errorf("%s: %q", f, got)
		}
	}
	predefiniti := map[Famiglia]string{
		FamTGCambioStato:        "Ciao <b>{nome}</b>! 👋\n\n{emoji} La tua riparazione è ora <b>{titolo_stato}</b>\n\n📦 Busta: <b>{numero}</b>\n💍 <i>{descrizione}</i>\n\n— {negozio}",
		FamTGNuovoMessaggio:     "Ciao <b>{nome}</b>! 👋\n\n💬 Hai un nuovo messaggio sulla busta <b>{numero}</b>:\n\n<blockquote>{testo}</blockquote>\n\n— {negozio}",
		FamEmailConfermaOggetto: "Conferma avviso email — busta {numero}",
		FamEmailConfermaTesto:   "Ciao {nome},\n\nper ricevere l'avviso via email quando la tua riparazione (busta {numero}) sarà pronta, conferma il tuo indirizzo cliccando questo link:\n\n{link_conferma}\n\nIl link vale 48 ore. Se non hai richiesto tu questo avviso, ignora questa email.\n\n— {negozio}",
		FamEmailProntaOggetto:   "La tua riparazione è pronta — busta {numero}",
		FamEmailProntaTesto:     "Ciao {nome},\n\nla tua riparazione (busta {numero}) è pronta per il ritiro!\n\n{descrizione}\n\nDettagli e foto: {link_stato}\n\nTi aspettiamo in negozio.\n\n— {negozio}",
	}
	for f, p := range predefiniti {
		if f.Predefinito() != p {
			t.Errorf("%s: %q", f, f.Predefinito())
		}
	}
}

// inventario: CLI-38
// Le regole delle email: il testo della conferma deve nominare {link_conferma}, l'oggetto sta su una
// riga (anche un «\r» da solo); un testo che lo scanner non legge dice perché, non che manca il link.
// Il testo della pronta può fare a meno del link. Telegram non ammette {link} né {telegram_optin}.
func TestRegoleDelleEmail(t *testing.T) {
	casi := []struct {
		f      Famiglia
		testo  string
		atteso string
	}{
		{FamEmailConfermaTesto, "Ciao {nome}", "Manca il segnaposto obbligatorio {link_conferma}"},
		{FamEmailConfermaTesto, "Ciao {nome} {link_conferma}", ""},
		{FamEmailConfermaTesto, "Ciao {nome", "Sintassi non valida: una { non chiusa"},
		{FamEmailConfermaTesto, "{link_conferma} {link}", "Segnaposto non supportato: {link}"},
		{FamEmailConfermaOggetto, "Conferma\nbusta {numero}", "L'oggetto deve stare su una riga"},
		{FamEmailConfermaOggetto, "Conferma\rbusta {numero}", "L'oggetto deve stare su una riga"},
		{FamEmailProntaOggetto, "Pronta\n{numero}", "L'oggetto deve stare su una riga"},
		{FamEmailProntaOggetto, "Pronta {link_stato}", "Segnaposto non supportato: {link_stato}"},
		{FamEmailProntaTesto, "Pronta, busta {numero}", ""},
		{FamEmailProntaTesto, "Pronta\n{numero}", ""},
		{FamTGNuovoMessaggio, "{testo} {emoji}", "Segnaposto non supportato: {emoji}"},
		{FamTGCambioStato, "{link}", "Segnaposto non supportato: {link}"},
		{FamTGCambioStato, "{telegram_optin}", "Segnaposto non supportato: {telegram_optin}"},
		{FamTGNuovoMessaggio, "<b>{numero}</b> <u>{testo}</u>", ""},
	}
	for _, c := range casi {
		if got := ErroreModello(c.f, c.testo); got != c.atteso {
			t.Errorf("%s %q: %q, atteso %q", c.f, c.testo, got, c.atteso)
		}
	}
}

// inventario: CLI-38, ADM-13
// preview/2: il testo che si sta scrivendo sull'esempio, non quello salvato; nessuna anteprima per
// un testo che non si legge. Le regole delle email non tolgono l'anteprima (come preview/2).
func TestAnteprimaModello(t *testing.T) {
	got, ok := AnteprimaModello(FamPronta, "Ciao {nome}, busta {numero}: {telegram_optin}")
	if !ok || got != "Ciao Maria Rossi, busta 12/26: https://negozio.example/r/abc123/telegram" {
		t.Errorf("%q %v", got, ok)
	}
	if got, ok := AnteprimaModello(FamPronta, "Ciao {password}"); ok || got != "" {
		t.Errorf("non valido: %q %v", got, ok)
	}
	if got, ok := AnteprimaModello(FamEmailConfermaTesto, "Ciao {nome}"); !ok || got != "Ciao Maria Rossi" {
		t.Errorf("senza link: %q %v", got, ok)
	}
	if got, _ := AnteprimaModello(FamLinkStato, FamLinkStato.Predefinito()); !strings.Contains(got, "\n\nPronta di solito in 5-8 giorni dal ritiro.\n\nInstagram: @negozio\n👋 Nome del negozio 💍 Città\nLun-Sab 9-19") {
		t.Errorf("predefinito: %q", got)
	}
}

// inventario: CLI-38, ADM-13
// put/3: il testo salvato è quello che torna e che si compone; l'audit outgoing.template_saved dice
// la famiglia, se è il predefinito e se è valido, mai il testo. Una casella vuota rimette il
// predefinito (anche nell'audit, «default»: true).
func TestSalvaModello(t *testing.T) {
	b, d, _ := prova(t)
	testo := "Buongiorno {nome}, la busta {numero} è pronta. Frase privata"
	got, err := b.SalvaModello(ctx, FamPronta, testo, titolareTesti)
	if err != nil || got != testo {
		t.Fatalf("%q %v", got, err)
	}
	if v, err := b.TestoModello(ctx, FamPronta); err != nil || v != testo {
		t.Errorf("in vigore: %q %v", v, err)
	}
	a := ultimoAudit(t, d, "outgoing.template_saved")
	if a != `outgoing.template_saved|outgoing_template|wa_pronta|ok|{"default":false,"family":"wa_pronta","valid":true}` {
		t.Errorf("audit %q", a)
	}
	if strings.Contains(a, "Frase privata") {
		t.Error("l'audit porta il testo")
	}
	for _, vuoto := range []string{"", "  \n "} {
		got, err := b.SalvaModello(ctx, FamPronta, vuoto, titolareTesti)
		if err != nil || got != FamPronta.Predefinito() {
			t.Fatalf("vuoto: %q %v", got, err)
		}
		if v := valore(t, d, `SELECT valore FROM impostazioni WHERE chiave = 'outgoing.template.wa_pronta'`); v != FamPronta.Predefinito() {
			t.Errorf("salvato %q", v)
		}
		if a := ultimoAudit(t, d, "outgoing.template_saved"); !strings.Contains(a, `"default":true`) {
			t.Errorf("audit %q", a)
		}
	}
	// Le altre famiglie non cambiano.
	if n := conta(t, d, `SELECT count(*) FROM impostazioni WHERE chiave LIKE 'outgoing.template.%'`); n != 1 {
		t.Errorf("chiavi %d", n)
	}
	if v, _ := b.TestoModello(ctx, FamLinkStato); v != FamLinkStato.Predefinito() {
		t.Errorf("altra famiglia: %q", v)
	}
	// Un testo Telegram tiene l'HTML com'è scritto.
	html := "<b>{numero}</b> <u>{testo}</u>"
	if got, err := b.SalvaModello(ctx, FamTGNuovoMessaggio, html, titolareTesti); err != nil || got != html {
		t.Errorf("telegram: %q %v", got, err)
	}
}

// inventario: CLI-38, ADM-13
// Un testo che il validatore rifiuta, o troppo lungo (oltre 4000 caratteri, contati come grafemi),
// non scrive niente: né l'impostazione né l'audit. Il rifiuto porta l'avviso di format_error/2.
func TestSalvaModelloRifiuti(t *testing.T) {
	b, d, _ := prova(t)
	prima := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	casi := []struct {
		f      Famiglia
		testo  string
		avviso string
	}{
		{FamPronta, "Ciao {nome}, {password}", "Segnaposto non supportato: {password}"},
		{FamOreficeria, "Ciao {telegram_optin}", "Segnaposto non supportato: {telegram_optin}"},
		{FamPronta, "Ciao {0}", "Sintassi non valida: un segnaposto posizionale {0}"},
		{FamPronta, "Ciao {}", "Sintassi non valida: un segnaposto senza nome"},
		{FamPronta, "Ciao {a.b}", "Sintassi non valida: un attributo in {a.b}"},
		{FamPronta, "Ciao {a[0]}", "Sintassi non valida: un indice in {a[0]}"},
		{FamPronta, "Ciao {nome:>10}", "Sintassi non valida: un formato in {nome:>10}"},
		{FamPronta, "Ciao {nome!r}", "Sintassi non valida: una conversione in {nome!r}"},
		{FamPronta, "Ciao {nome", "Sintassi non valida: una { non chiusa"},
		{FamPronta, "Ciao } nome", "Sintassi non valida: una } da sola"},
		{FamEmailConfermaTesto, "Ciao {nome}", "Manca il segnaposto obbligatorio {link_conferma}"},
		{FamEmailConfermaOggetto, "a\nb", "L'oggetto deve stare su una riga"},
	}
	for _, c := range casi {
		_, err := b.SalvaModello(ctx, c.f, c.testo, titolareTesti)
		var r *ModelloRifiutato
		if !errors.As(err, &r) || r.Avviso != c.avviso {
			t.Errorf("%s %q: %v", c.f, c.testo, err)
		}
	}
	lungo := strings.Repeat("é", ModelloLunghezzaMassima+1) // 4001 grafemi, 8002 byte
	if _, err := b.SalvaModello(ctx, FamPronta, lungo, titolareTesti); !errors.Is(err, ErrModelloTroppoLungo) {
		t.Errorf("troppo lungo: %v", err)
	}
	// 4000 grafemi di due rune ciascuno («e» con l'accento combinato) vanno: si contano i grafemi.
	if _, err := b.SalvaModello(ctx, FamPronta, strings.Repeat("é", ModelloLunghezzaMassima), titolareTesti); err != nil {
		t.Errorf("4000 grafemi: %v", err)
	}
	if n := conta(t, d, `SELECT count(*) FROM impostazioni WHERE chiave LIKE 'outgoing.template.%'`); n != 1 {
		t.Errorf("chiavi %d", n)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit`); n != prima+1 {
		t.Errorf("audit %d → %d", prima, n)
	}
}

// inventario: CLI-38
// render/2 con le regole: un oggetto email salvato su due righe (scritto da altri) non si compone, e
// l'errore dice la famiglia e l'avviso, mai il testo.
func TestComponiConLeRegole(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.template.email_pronta_oggetto', 'Riga privata
{numero}', '2026-10-31T09:00:00.000000Z')`)
	got, m, err := b.componiFamiglia(ctx, FamEmailProntaOggetto, map[string]string{"nome": "A", "numero": "1/26", "negozio": "N"})
	if err != nil || got != "" || m == nil || m.Famiglia != FamEmailProntaOggetto || m.Avviso != "L'oggetto deve stare su una riga" {
		t.Errorf("%q %+v %v", got, m, err)
	}
}
