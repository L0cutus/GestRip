package buste

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/audit"
)

// I testi dei messaggi (CLI-31, CLI-32, CLI-33, CLI-38, ADM-13), come Outgoing.Templates dello
// standalone (go-baseline): le quindici famiglie della pagina «Testi dei messaggi» (nove WhatsApp, due
// Telegram cliente, quattro email cliente), con nomi, etichette, campi dichiarati, predefiniti parola
// per parola, le due regole delle email (il link obbligatorio della conferma, l'oggetto su una riga) e
// la regola che non ripiega mai sul predefinito quando un testo salvato non si compone. I testi salvati
// stanno nelle impostazioni (outgoing.template.<famiglia>), importati dallo standalone o scritti
// dall'editor (SalvaModello, slice 067). Oggi si compongono solo i testi WhatsApp (avvisi.go); i testi
// Telegram li userà la T4.7, quelli email la slice 069: l'editor li ha già tutti, come lo standalone.

// Famiglia è una famiglia di testi.
type Famiglia string

// Le famiglie, con i nomi dello standalone.
const (
	FamPreventivoConImporto   Famiglia = "wa_preventivo_con_importo"
	FamPreventivoSenzaImporto Famiglia = "wa_preventivo_senza_importo"
	FamProntaCompraOro        Famiglia = "wa_pronta_compro_oro"
	FamPronta                 Famiglia = "wa_pronta"
	FamLinkStato              Famiglia = "wa_link_stato"
	FamClienteAggiornamento   Famiglia = "wa_cliente_aggiornamento"
	FamOreficeria             Famiglia = "wa_oreficeria"
	FamOreficeriaLotto        Famiglia = "wa_oreficeria_lotto"
	FamArtigiano              Famiglia = "wa_artigiano"
	FamTGCambioStato          Famiglia = "tg_cliente_cambio_stato"
	FamTGNuovoMessaggio       Famiglia = "tg_cliente_nuovo_messaggio"
	FamEmailConfermaOggetto   Famiglia = "email_conferma_oggetto"
	FamEmailConfermaTesto     Famiglia = "email_conferma_testo"
	FamEmailProntaOggetto     Famiglia = "email_pronta_oggetto"
	FamEmailProntaTesto       Famiglia = "email_pronta_testo"
)

// FamiglieWhatsApp sono le famiglie nell'ordine della pagina dei testi (WA_TEMPLATES della sorgente).
var FamiglieWhatsApp = []Famiglia{FamPreventivoConImporto, FamPreventivoSenzaImporto, FamProntaCompraOro, FamPronta,
	FamLinkStato, FamClienteAggiornamento, FamOreficeria, FamOreficeriaLotto, FamArtigiano}

// FamiglieTelegram sono le famiglie Telegram cliente (TG_TEMPLATES della sorgente).
var FamiglieTelegram = []Famiglia{FamTGCambioStato, FamTGNuovoMessaggio}

// FamiglieEmail sono le famiglie dell'email cliente (EMAIL_TEMPLATES della sorgente).
var FamiglieEmail = []Famiglia{FamEmailConfermaOggetto, FamEmailConfermaTesto, FamEmailProntaOggetto, FamEmailProntaTesto}

// Famiglie sono tutte le famiglie, nell'ordine della pagina dei testi (families/0): WhatsApp, Telegram,
// email.
var Famiglie = append(append(append([]Famiglia{}, FamiglieWhatsApp...), FamiglieTelegram...), FamiglieEmail...)

// FamigliaDi è la famiglia di un nome mandato dal browser, confrontato con l'elenco (draft/1 di
// TemplatesLive): false per ogni nome che non è esattamente quello di una famiglia.
func FamigliaDi(nome string) (Famiglia, bool) {
	for _, f := range Famiglie {
		if string(f) == nome {
			return f, true
		}
	}
	return "", false
}

// FamiglieCliente sono le famiglie del cliente diretto, le sole che ammettono {telegram_optin}.
var FamiglieCliente = []Famiglia{FamPreventivoConImporto, FamPreventivoSenzaImporto, FamProntaCompraOro, FamPronta,
	FamLinkStato, FamClienteAggiornamento}

var etichetteFamiglie = map[Famiglia]string{
	FamPreventivoConImporto: "Preventivo (con importo)", FamPreventivoSenzaImporto: "Preventivo (senza importo)",
	FamProntaCompraOro: "Pronta — Compro oro", FamPronta: "Pronta — con link", FamLinkStato: "Invia link stato cliente",
	FamClienteAggiornamento: "Aggiornamento link al cliente", FamOreficeria: "Aggiornamento oreficeria",
	FamOreficeriaLotto: "Avviso lotto pronte oreficeria", FamArtigiano: "Sollecito artigiano",
	FamTGCambioStato: "Cambio stato riparazione", FamTGNuovoMessaggio: "Nuovo messaggio dal negozio",
	FamEmailConfermaOggetto: "Email di conferma — oggetto", FamEmailConfermaTesto: "Email di conferma — testo",
	FamEmailProntaOggetto: "Email di pronta — oggetto", FamEmailProntaTesto: "Email di pronta — testo",
}

// campiCliente è @customer_fields: il contratto comune dei testi del cliente, senza {password}.
var campiCliente = []string{"nome", "numero", "link", "negozio", "citta", "orari", "social", "telegram_optin"}

func piu(a []string, b ...string) []string { return append(append([]string{}, a...), b...) }

var campiFamiglie = map[Famiglia][]string{
	FamPreventivoConImporto:   piu(campiCliente, "preventivo", "descrizione"),
	FamPreventivoSenzaImporto: campiCliente,
	FamLinkStato:              piu(campiCliente, "stima"),
	FamClienteAggiornamento:   piu(campiCliente, "stima"),
	FamPronta:                 campiCliente,
	FamProntaCompraOro:        campiCliente,
	FamOreficeria:             {"oref_nome", "nome_cliente", "numero", "lavorazione", "stato", "importo", "consegna", "note_riga", "link", "negozio"},
	FamOreficeriaLotto:        {"oref_nome", "n_buste", "link", "negozio"},
	FamArtigiano:              {"artigiano_nome", "numero", "lavorazione", "stato", "consegna", "note_riga", "link", "negozio"},
	FamTGCambioStato:          {"nome", "emoji", "titolo_stato", "numero", "descrizione", "stato", "negozio"},
	FamTGNuovoMessaggio:       {"nome", "numero", "testo", "negozio"},
	FamEmailConfermaOggetto:   {"nome", "numero", "negozio"},
	FamEmailConfermaTesto:     {"nome", "numero", "descrizione", "negozio", "link_conferma"},
	FamEmailProntaOggetto:     {"nome", "numero", "negozio"},
	FamEmailProntaTesto:       {"nome", "numero", "descrizione", "negozio", "link_stato"},
}

// obbligatori sono i campi che il testo deve nominare (@required): la conferma senza il suo link non
// conferma niente.
var obbligatori = map[Famiglia][]string{FamEmailConfermaTesto: {"link_conferma"}}

// suUnaRiga sono gli oggetti delle email (@single_line): una riga d'intestazione.
var suUnaRiga = map[Famiglia]bool{FamEmailConfermaOggetto: true, FamEmailProntaOggetto: true}

// testoPreventivo è lo stesso testo per le due famiglie del preventivo (app.py:983-998).
const testoPreventivo = "Buongiorno {nome}, c'è un preventivo per la Sua riparazione n° {numero}.\n" +
	"Può visionare tutti i dettagli e confermare o rifiutare il preventivo da questo link:\n" +
	"{link}\n\n{social}👋 {negozio}{citta}\n{orari}"

var predefiniti = map[Famiglia]string{
	FamPreventivoConImporto:   testoPreventivo,
	FamPreventivoSenzaImporto: testoPreventivo,
	FamLinkStato: "Buongiorno {nome}, può seguire in tempo reale lo stato della Sua riparazione n° {numero} da questo link:\n" +
		"{link}\n\n{stima}{social}👋 {negozio}{citta}\n{orari}",
	FamClienteAggiornamento: "Buongiorno {nome}, c'è stato un aggiornamento sulla Sua riparazione n° {numero}.\n" +
		"Può vedere i dettagli aggiornati da questo link:\n{link}\n\n👋 {negozio}{citta}\n{orari}",
	FamPronta: "Buongiorno {nome}, la Sua riparazione n° {numero} è pronta per il ritiro! 🎉\n" +
		"Può visionare tutti i dettagli da questo link:\n{link}\n\n{social}👋 {negozio}{citta}\n{orari}",
	FamProntaCompraOro: "Buongiorno {nome}, la Sua scheda compro oro n° {numero} è pronta per la firma! 🎉\n" +
		"Può visionare tutti i dettagli da questo link:\n{link}\n\n{social}👋 {negozio}{citta}\n{orari}",
	FamOreficeria: "Ciao {oref_nome}, aggiornamento sulla lavorazione:\nCliente: {nome_cliente} — busta n. {numero}\n" +
		"{lavorazione}Stato: {stato}\n{importo}{consegna}{note_riga}\nDettagli e foto: {link}\n{negozio}",
	// app.py:1043-1048 parola per parola, «ho 1 riparazioni» compreso.
	FamOreficeriaLotto: "Buongiorno {oref_nome},\nho {n_buste} riparazioni pronte da farvi ritirare.\nDettagli e foto: {link}\n\n{negozio}",
	FamArtigiano: "Ciao {artigiano_nome}, sollecito riparazione n° {numero}\n{lavorazione}Stato attuale: {stato}\n" +
		"{consegna}{note_riga}\nDettagli e foto: {link}\n{negozio}",
	// app.py:1112-1126 (TG_TEMPLATES), parola per parola.
	FamTGCambioStato: "Ciao <b>{nome}</b>! 👋\n\n{emoji} La tua riparazione è ora <b>{titolo_stato}</b>\n\n" +
		"📦 Busta: <b>{numero}</b>\n💍 <i>{descrizione}</i>\n\n— {negozio}",
	FamTGNuovoMessaggio: "Ciao <b>{nome}</b>! 👋\n\n💬 Hai un nuovo messaggio sulla busta <b>{numero}</b>:\n\n" +
		"<blockquote>{testo}</blockquote>\n\n— {negozio}",
	// app.py:1129-1147 (EMAIL_TEMPLATES), parola per parola.
	FamEmailConfermaOggetto: "Conferma avviso email — busta {numero}",
	FamEmailConfermaTesto: "Ciao {nome},\n\nper ricevere l'avviso via email quando la tua riparazione (busta {numero}) sarà pronta, " +
		"conferma il tuo indirizzo cliccando questo link:\n\n{link_conferma}\n\nIl link vale 48 ore. Se non hai richiesto tu " +
		"questo avviso, ignora questa email.\n\n— {negozio}",
	FamEmailProntaOggetto: "La tua riparazione è pronta — busta {numero}",
	FamEmailProntaTesto: "Ciao {nome},\n\nla tua riparazione (busta {numero}) è pronta per il ritiro!\n\n{descrizione}\n\n" +
		"Dettagli e foto: {link_stato}\n\nTi aspettiamo in negozio.\n\n— {negozio}",
}

// esempioCliente è la busta inventata dell'anteprima dei testi del cliente (@customer_sample).
var esempioCliente = map[string]string{"nome": "Maria Rossi", "numero": "12/26", "link": "https://negozio.example/r/abc123",
	"telegram_optin": "https://negozio.example/r/abc123/telegram", "negozio": "Nome del negozio", "citta": " 💍 Città",
	"orari": "Lun-Sab 9-19", "social": "Instagram: @negozio\n"}

// esempioStima è message_line/1 di una stima inventata: la frase e la riga vuota.
const esempioStima = "Pronta di solito in 5-8 giorni dal ritiro.\n\n"

func conEsempio(base map[string]string, altri ...string) map[string]string {
	m := map[string]string{}
	for k, v := range base {
		m[k] = v
	}
	for i := 0; i+1 < len(altri); i += 2 {
		m[altri[i]] = altri[i+1]
	}
	return m
}

// esempi sono i valori inventati dell'anteprima, per famiglia (@samples): mai una busta vera.
var esempi = map[Famiglia]map[string]string{
	FamPreventivoConImporto:   conEsempio(esempioCliente, "preventivo", "70,00", "descrizione", "Anello in oro giallo"),
	FamPreventivoSenzaImporto: esempioCliente,
	FamLinkStato:              conEsempio(esempioCliente, "stima", esempioStima),
	FamClienteAggiornamento:   conEsempio(esempioCliente, "stima", esempioStima),
	FamPronta:                 esempioCliente,
	FamProntaCompraOro:        esempioCliente,
	FamOreficeria: {"oref_nome": "Oreficeria Bianchi", "nome_cliente": "Maria Rossi", "numero": "12/26",
		"lavorazione": "Lavorazione: Anello in oro giallo\n", "stato": "IN LAVORAZIONE", "importo": "Totale: € 80,00\n",
		"consegna": "Consegna prevista: 20/09/2026\n", "note_riga": "Note: da lucidare\n", "link": "https://negozio.example/r/abc123",
		"negozio": "Nome del negozio"},
	FamOreficeriaLotto: {"oref_nome": "Oreficeria Bianchi", "n_buste": "3", "link": "https://negozio.example/lotto/abc123",
		"negozio": "Nome del negozio"},
	FamArtigiano: {"artigiano_nome": "Laboratorio Verdi", "numero": "12/26", "lavorazione": "Lavorazione: Anello in oro giallo\n",
		"stato": "IN LAVORAZIONE", "consegna": "Consegna prevista: 20/09/2026\n", "note_riga": "Note: da lucidare\n",
		"link": "https://negozio.example/artigiano/12?t=abc123", "negozio": "Nome del negozio"},
	FamTGCambioStato: {"nome": "Maria", "emoji": "✨", "titolo_stato": "Pronta per il ritiro", "numero": "12/26",
		"descrizione": "Anello in oro giallo", "stato": "pronta", "negozio": "Nome del negozio"},
	FamTGNuovoMessaggio: {"nome": "Maria", "numero": "12/26", "testo": "Il preventivo è pronto: può passare in negozio.",
		"negozio": "Nome del negozio"},
	FamEmailConfermaOggetto: {"nome": "Maria Rossi", "numero": "12/26", "negozio": "Nome del negozio"},
	FamEmailConfermaTesto: {"nome": "Maria Rossi", "numero": "12/26", "descrizione": "Anello in oro giallo",
		"negozio": "Nome del negozio", "link_conferma": "https://negozio.example/stato/email-conferma?t=abc123"},
	FamEmailProntaOggetto: {"nome": "Maria Rossi", "numero": "12/26", "negozio": "Nome del negozio"},
	FamEmailProntaTesto: {"nome": "Maria Rossi", "numero": "12/26", "descrizione": "Anello in oro giallo",
		"negozio": "Nome del negozio", "link_stato": "https://negozio.example/r/abc123"},
}

// ModelloLunghezzaMassima è max_length/0: un testo più lungo (in grafemi, String.length/1) non si
// salva.
const ModelloLunghezzaMassima = 4000

// Etichetta è il nome che il titolare legge (WA_TEMPLATE_LABELS della sorgente).
func (f Famiglia) Etichetta() string { return etichetteFamiglie[f] }

// Campi sono i segnaposto che la famiglia può usare, e i soli.
func (f Famiglia) Campi() []string { return campiFamiglie[f] }

// Predefinito è il testo di fabbrica: quello di una casella vuota e di «Ripristina predefinito».
func (f Famiglia) Predefinito() string { return predefiniti[f] }

// Esempio sono i valori inventati dell'anteprima (sample_context/1).
func (f Famiglia) Esempio() map[string]string { return esempi[f] }

// Chiave è la chiave delle impostazioni del testo salvato.
func (f Famiglia) Chiave() string { return "outgoing.template." + string(f) }

// ModelloNonValido è un testo salvato che non si compone: la famiglia e l'avviso di ErroreModello,
// mai il testo.
type ModelloNonValido struct {
	Famiglia Famiglia
	Avviso   string
}

// erroreScansione è un errore di un testo: un campo sconosciuto (nome) o la sintassi (dettaglio).
type erroreScansione struct {
	sconosciuto bool
	nome        string // il campo sconosciuto
	dettaglio   string // la frase della sintassi
	mancante    string // il campo obbligatorio che il testo non nomina
	suPiuRighe  bool   // un oggetto email su più righe
}

type pezzoModello struct {
	campo bool
	testo string // il testo letterale, o il nome del campo
}

// ErroreModello è format_error/2: "" se il testo rispetta le regole della famiglia, usa solo i suoi
// campi dichiarati e si legge; altrimenti «L'oggetto deve stare su una riga», «Manca il segnaposto
// obbligatorio {x}», «Segnaposto non supportato: {x}» o «Sintassi non valida: …».
func ErroreModello(f Famiglia, testo string) string {
	if e := erroreRegola(f, testo); e != nil {
		return e.avviso()
	}
	vuoti := map[string]string{}
	for _, c := range f.Campi() {
		vuoti[c] = ""
	}
	if _, e := componi(testo, vuoti); e != nil {
		return e.avviso()
	}
	return ""
}

// erroreRegola è rule_error/2: l'oggetto su più righe, poi un campo obbligatorio che il testo non
// nomina, ma solo se il testo si legge (altrimenti lo dice la composizione).
func erroreRegola(f Famiglia, testo string) *erroreScansione {
	if suUnaRiga[f] && strings.ContainsAny(testo, "\n\r") {
		return &erroreScansione{suPiuRighe: true}
	}
	for _, c := range obbligatori[f] {
		pezzi, e := scansiona(testo)
		if e != nil {
			return nil
		}
		nominato := false
		for _, p := range pezzi {
			nominato = nominato || p.campo && p.testo == c
		}
		if !nominato {
			return &erroreScansione{mancante: c}
		}
	}
	return nil
}

func (e *erroreScansione) avviso() string {
	switch {
	case e.suPiuRighe:
		return "L'oggetto deve stare su una riga"
	case e.mancante != "":
		return "Manca il segnaposto obbligatorio " + mostraNome(e.mancante)
	case e.sconosciuto:
		return "Segnaposto non supportato: " + mostraNome(e.nome)
	}
	return "Sintassi non valida: " + e.dettaglio
}

// AnteprimaModello è preview/2: il testo che si sta scrivendo composto sull'esempio della famiglia;
// false se non si legge (né si salva né si manda: niente anteprima). Le regole delle email non
// contano, come nella sorgente.
func AnteprimaModello(f Famiglia, testo string) (string, bool) {
	s, e := componi(testo, f.Esempio())
	if e != nil {
		return "", false
	}
	return s, true
}

// reSpazioUnicode è \s con il flag u di una regex di Elixir.
var reSpazioUnicode = regexp.MustCompile(`[\s\p{Z}\x{85}]`)

// mostraNome è shown/1: «{nome}», o «{…}» per un nome con spazi o oltre 32 grafemi (sarebbe il testo
// del messaggio, che avvisi e registri non portano).
func mostraNome(nome string) string {
	if uniseg.GraphemeClusterCount(nome) <= 32 && !reSpazioUnicode.MatchString(nome) {
		return "{" + nome + "}"
	}
	return "{…}"
}

// componiTesto è render_text/2 senza il panico: il testo con i valori al posto dei campi.
func componiTesto(testo string, valori map[string]string) (string, error) {
	s, e := componi(testo, valori)
	if e != nil {
		return "", errors.New(e.avviso())
	}
	return s, nil
}

// componi è compile/2: legge il testo (scan/1) e mette i valori; un campo che i valori non hanno è
// sconosciuto. Un valore non si rilegge.
func componi(testo string, valori map[string]string) (string, *erroreScansione) {
	pezzi, e := scansiona(testo)
	if e != nil {
		return "", e
	}
	var b strings.Builder
	for _, p := range pezzi {
		if !p.campo {
			b.WriteString(p.testo)
			continue
		}
		v, ok := valori[p.testo]
		if !ok {
			return "", &erroreScansione{sconosciuto: true, nome: p.testo}
		}
		b.WriteString(v)
	}
	return b.String(), nil
}

// scansiona è scan/1, string.Formatter.parse della sorgente: «{{» e «}}» sono una graffa, una «}» da
// sola è un errore, un campo va fino alla prima «}» e il suo nome passa da campoValido.
func scansiona(testo string) ([]pezzoModello, *erroreScansione) {
	var pezzi []pezzoModello
	var lett strings.Builder
	spingi := func() {
		if lett.Len() > 0 {
			pezzi = append(pezzi, pezzoModello{testo: lett.String()})
			lett.Reset()
		}
	}
	for i := 0; i < len(testo); {
		switch {
		case strings.HasPrefix(testo[i:], "{{"):
			lett.WriteByte('{')
			i += 2
		case strings.HasPrefix(testo[i:], "}}"):
			lett.WriteByte('}')
			i += 2
		case testo[i] == '}':
			return nil, &erroreScansione{dettaglio: "una } da sola"}
		case testo[i] == '{':
			fine := strings.IndexByte(testo[i+1:], '}')
			if fine < 0 {
				return nil, &erroreScansione{dettaglio: "una { non chiusa"}
			}
			nome := testo[i+1 : i+1+fine]
			if e := campoValido(nome); e != nil {
				return nil, e
			}
			spingi()
			pezzi = append(pezzi, pezzoModello{campo: true, testo: nome})
			i += fine + 2
		default:
			r, n := utf8.DecodeRuneInString(testo[i:])
			if r == utf8.RuneError && n <= 1 {
				return nil, &erroreScansione{dettaglio: "un byte non valido"}
			}
			lett.WriteString(testo[i : i+n])
			i += n
		}
	}
	spingi()
	return pezzi, nil
}

var reSoloCifre = regexp.MustCompile(`^[0-9]+$`)

// campoValido è valid_field/1, il controllo get_field della sorgente.
func campoValido(nome string) *erroreScansione {
	switch {
	case nome == "":
		return &erroreScansione{dettaglio: "un segnaposto senza nome"}
	case reSoloCifre.MatchString(nome):
		return &erroreScansione{dettaglio: "un segnaposto posizionale " + mostraNome(nome)}
	case strings.Contains(nome, "."):
		return &erroreScansione{dettaglio: "un attributo in " + mostraNome(nome)}
	case strings.Contains(nome, "["):
		return &erroreScansione{dettaglio: "un indice in " + mostraNome(nome)}
	case strings.Contains(nome, "{"):
		return &erroreScansione{dettaglio: "una { dentro un segnaposto"}
	case strings.Contains(nome, ":"):
		return &erroreScansione{dettaglio: "un formato in " + mostraNome(nome)}
	case strings.Contains(nome, "!"):
		return &erroreScansione{dettaglio: "una conversione in " + mostraNome(nome)}
	}
	return nil
}

// testoInVigore è get/1: il testo salvato della famiglia, o il predefinito se manca o è di soli
// spazi.
func (b *Buste) testoInVigore(ctx context.Context, q lettore, f Famiglia) (string, error) {
	var v string
	err := q.QueryRowContext(ctx, `SELECT valore FROM impostazioni WHERE chiave = ?`, f.Chiave()).Scan(&v)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return f.Predefinito(), nil
	case err != nil:
		return "", err
	case strings.TrimSpace(v) == "":
		return f.Predefinito(), nil
	}
	return v, nil
}

// componiFamiglia è render/2: il testo in vigore con i valori. Un testo salvato che non si compone
// dà ModelloNonValido (famiglia e avviso) e nessun testo: niente ripiego sul predefinito.
func (b *Buste) componiFamiglia(ctx context.Context, f Famiglia, valori map[string]string) (string, *ModelloNonValido, error) {
	t, err := b.testoInVigore(ctx, b.d.Lettura, f)
	if err != nil {
		return "", nil, err
	}
	e := erroreRegola(f, t)
	var s string
	if e == nil {
		s, e = componi(t, valori)
	}
	if e != nil {
		avviso := ErroreModello(f, t)
		if avviso == "" { // reason_text/1: un campo dichiarato che i valori non hanno
			avviso = e.dettaglio
			switch {
			case e.sconosciuto:
				avviso = "segnaposto sconosciuto " + mostraNome(e.nome)
			case e.mancante != "":
				avviso = "manca il segnaposto " + mostraNome(e.mancante)
			case e.suPiuRighe:
				avviso = "oggetto su più righe"
			}
		}
		return "", &ModelloNonValido{Famiglia: f, Avviso: avviso}, nil
	}
	return s, nil, nil
}

// TestoModello è get/1: il testo in vigore della famiglia, quello salvato o il predefinito.
func (b *Buste) TestoModello(ctx context.Context, f Famiglia) (string, error) {
	return b.testoInVigore(ctx, b.d.Lettura, f)
}

// ErrModelloTroppoLungo è {:error, :too_long}: oltre ModelloLunghezzaMassima grafemi.
var ErrModelloTroppoLungo = errors.New("buste: testo del messaggio troppo lungo")

// ModelloRifiutato è {:error, {:invalid, avviso}}: il testo non rispetta le regole della famiglia;
// l'avviso è quello di ErroreModello, mai il testo.
type ModelloRifiutato struct{ Avviso string }

func (e *ModelloRifiutato) Error() string { return "buste: testo non valido: " + e.Avviso }

// SalvaModello è put/3 («Salva il testo» e «Ripristina predefinito», che manda una casella vuota): una
// casella vuota o di soli spazi rimette il predefinito; un testo troppo lungo (ErrModelloTroppoLungo) o
// che ErroreModello rifiuta (*ModelloRifiutato) non scrive niente. Altrimenti, in una transazione,
// l'impostazione outgoing.template.<famiglia> e outgoing.template_saved con la famiglia, se il testo è
// il predefinito e se è valido: mai il testo, che è un messaggio e non il nome di un'impostazione. Dà
// il testo salvato.
func (b *Buste) SalvaModello(ctx context.Context, f Famiglia, testo string, c audit.Contesto) (string, error) {
	if strings.TrimSpace(testo) == "" {
		testo = f.Predefinito()
	}
	if uniseg.GraphemeClusterCount(testo) > ModelloLunghezzaMassima {
		return "", ErrModelloTroppoLungo
	}
	if avviso := ErroreModello(f, testo); avviso != "" {
		return "", &ModelloRifiutato{Avviso: avviso}
	}
	err := b.inTx(ctx, func(tx *sql.Tx, ora string) error {
		if err := b.salvaImpostazione(ctx, tx, f.Chiave(), testo, ora); err != nil {
			return err
		}
		return b.reg.Scrivi(ctx, tx, c, "outgoing.template_saved", "outgoing_template", string(f), "ok", map[string]any{
			"family": string(f), "default": testo == f.Predefinito(), "valid": ErroreModello(f, testo) == ""})
	})
	if err != nil {
		return "", err
	}
	return testo, nil
}
