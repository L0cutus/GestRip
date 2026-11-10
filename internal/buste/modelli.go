package buste

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// I testi WhatsApp (CLI-31, CLI-32, CLI-33), come Outgoing.Templates dello standalone (go-baseline)
// per le nove famiglie wa_*: nomi, etichette, campi dichiarati, predefiniti parola per parola, e la
// regola che non ripiega mai sul predefinito quando un testo salvato non si compone. I testi salvati
// stanno nelle impostazioni (outgoing.template.<famiglia>), importati dallo standalone; l'editor con
// il validatore è della slice 067 (CLI-38), le famiglie Telegram ed email arrivano con le loro tappe.

// FamigliaWA è una famiglia di testi WhatsApp.
type FamigliaWA string

// Le nove famiglie, con i nomi dello standalone.
const (
	FamPreventivoConImporto   FamigliaWA = "wa_preventivo_con_importo"
	FamPreventivoSenzaImporto FamigliaWA = "wa_preventivo_senza_importo"
	FamProntaCompraOro        FamigliaWA = "wa_pronta_compro_oro"
	FamPronta                 FamigliaWA = "wa_pronta"
	FamLinkStato              FamigliaWA = "wa_link_stato"
	FamClienteAggiornamento   FamigliaWA = "wa_cliente_aggiornamento"
	FamOreficeria             FamigliaWA = "wa_oreficeria"
	FamOreficeriaLotto        FamigliaWA = "wa_oreficeria_lotto"
	FamArtigiano              FamigliaWA = "wa_artigiano"
)

// FamiglieWhatsApp sono le famiglie nell'ordine della pagina dei testi (WA_TEMPLATES della sorgente).
var FamiglieWhatsApp = []FamigliaWA{FamPreventivoConImporto, FamPreventivoSenzaImporto, FamProntaCompraOro, FamPronta,
	FamLinkStato, FamClienteAggiornamento, FamOreficeria, FamOreficeriaLotto, FamArtigiano}

// FamiglieCliente sono le famiglie del cliente diretto, le sole che ammettono {telegram_optin}.
var FamiglieCliente = []FamigliaWA{FamPreventivoConImporto, FamPreventivoSenzaImporto, FamProntaCompraOro, FamPronta,
	FamLinkStato, FamClienteAggiornamento}

var etichetteFamiglie = map[FamigliaWA]string{
	FamPreventivoConImporto: "Preventivo (con importo)", FamPreventivoSenzaImporto: "Preventivo (senza importo)",
	FamProntaCompraOro: "Pronta — Compro oro", FamPronta: "Pronta — con link", FamLinkStato: "Invia link stato cliente",
	FamClienteAggiornamento: "Aggiornamento link al cliente", FamOreficeria: "Aggiornamento oreficeria",
	FamOreficeriaLotto: "Avviso lotto pronte oreficeria", FamArtigiano: "Sollecito artigiano",
}

// campiCliente è @customer_fields: il contratto comune dei testi del cliente, senza {password}.
var campiCliente = []string{"nome", "numero", "link", "negozio", "citta", "orari", "social", "telegram_optin"}

func piu(a []string, b ...string) []string { return append(append([]string{}, a...), b...) }

var campiFamiglie = map[FamigliaWA][]string{
	FamPreventivoConImporto:   piu(campiCliente, "preventivo", "descrizione"),
	FamPreventivoSenzaImporto: campiCliente,
	FamLinkStato:              piu(campiCliente, "stima"),
	FamClienteAggiornamento:   piu(campiCliente, "stima"),
	FamPronta:                 campiCliente,
	FamProntaCompraOro:        campiCliente,
	FamOreficeria:             {"oref_nome", "nome_cliente", "numero", "lavorazione", "stato", "importo", "consegna", "note_riga", "link", "negozio"},
	FamOreficeriaLotto:        {"oref_nome", "n_buste", "link", "negozio"},
	FamArtigiano:              {"artigiano_nome", "numero", "lavorazione", "stato", "consegna", "note_riga", "link", "negozio"},
}

// testoPreventivo è lo stesso testo per le due famiglie del preventivo (app.py:983-998).
const testoPreventivo = "Buongiorno {nome}, c'è un preventivo per la Sua riparazione n° {numero}.\n" +
	"Può visionare tutti i dettagli e confermare o rifiutare il preventivo da questo link:\n" +
	"{link}\n\n{social}👋 {negozio}{citta}\n{orari}"

var predefiniti = map[FamigliaWA]string{
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
}

// Etichetta è il nome che il titolare legge (WA_TEMPLATE_LABELS della sorgente).
func (f FamigliaWA) Etichetta() string { return etichetteFamiglie[f] }

// Campi sono i segnaposto che la famiglia può usare, e i soli.
func (f FamigliaWA) Campi() []string { return campiFamiglie[f] }

// Predefinito è il testo di fabbrica: quello di una casella vuota e di «Ripristina predefinito».
func (f FamigliaWA) Predefinito() string { return predefiniti[f] }

// Chiave è la chiave delle impostazioni del testo salvato.
func (f FamigliaWA) Chiave() string { return "outgoing.template." + string(f) }

// ModelloNonValido è un testo salvato che non si compone: la famiglia e l'avviso di ErroreModello,
// mai il testo.
type ModelloNonValido struct {
	Famiglia FamigliaWA
	Avviso   string
}

// erroreScansione è un errore di un testo: un campo sconosciuto (nome) o la sintassi (dettaglio).
type erroreScansione struct {
	sconosciuto bool
	nome        string // il campo sconosciuto
	dettaglio   string // la frase della sintassi
}

type pezzoModello struct {
	campo bool
	testo string // il testo letterale, o il nome del campo
}

// ErroreModello è format_error/2: "" se il testo usa solo i campi dichiarati della famiglia e si
// legge; altrimenti «Segnaposto non supportato: {x}» o «Sintassi non valida: …».
func ErroreModello(f FamigliaWA, testo string) string {
	vuoti := map[string]string{}
	for _, c := range f.Campi() {
		vuoti[c] = ""
	}
	if _, e := componi(testo, vuoti); e != nil {
		return e.avviso()
	}
	return ""
}

func (e *erroreScansione) avviso() string {
	if e.sconosciuto {
		return "Segnaposto non supportato: " + mostraNome(e.nome)
	}
	return "Sintassi non valida: " + e.dettaglio
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
func (b *Buste) testoInVigore(ctx context.Context, q lettore, f FamigliaWA) (string, error) {
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
func (b *Buste) componiFamiglia(ctx context.Context, f FamigliaWA, valori map[string]string) (string, *ModelloNonValido, error) {
	t, err := b.testoInVigore(ctx, b.d.Lettura, f)
	if err != nil {
		return "", nil, err
	}
	s, e := componi(t, valori)
	if e != nil {
		avviso := ErroreModello(f, t)
		if avviso == "" { // reason_text/1: un campo dichiarato che i valori non hanno
			avviso = e.dettaglio
			if e.sconosciuto {
				avviso = "segnaposto sconosciuto " + mostraNome(e.nome)
			}
		}
		return "", &ModelloNonValido{Famiglia: f, Avviso: avviso}, nil
	}
	return s, nil, nil
}
