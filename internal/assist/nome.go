package assist

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/rivo/uniseg"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"

	"github.com/L0cutus/gestrip/internal/telefono"
)

// Nome del cliente e oreficeria dal dettato (slice 105; voce RIP-H-43 dell'inventario, che si
// chiude con la 108). Il traduttore di produzione passa «n» e «o» allo standalone come li ha
// scritti il modello: tutte le regole qui sono dello standalone, nel suo ordine (read_answer/3 e
// shape/3 di Assist, assign_customer_pair/3 e assist_summary/1 di WorkshopLive):
//
//  1. nome dall'apertura del dettato («Cliente/Signora/Signor/Sig.ra/Sig.» + nome), quando il
//     modello l'ha perso, ha cifre o ne ha preso solo la coda;
//  2. maiuscole come str.title() su nome e oreficeria;
//  3. il nome deve essere una persona: svuotato se è un segnaposto, un nome di negozio o uguale a
//     un'oreficeria;
//  4. l'oreficeria nota: la prima per nome e id che contiene il nome detto o vi è contenuta;
//  5. il cliente riconosciuto solo nei casi certi: un solo cliente con quel telefono (almeno 7
//     cifre, 0039/39 tolti), altrimenti un solo nome uguale a parte accenti, spazi, apostrofi e
//     maiuscole. Nessuna somiglianza approssimata;
//  6. la coppia nome/telefono del modulo: vince il telefono dettato; se manca, quello dei clienti
//     con lo stesso nome confrontabile solo se hanno tutti lo stesso telefono (D-106, Stefano
//     20/11/2026: lo standalone prende il primo per nome e id anche fra omonimi, e la busta può
//     finire sul cliente sbagliato; con telefoni diversi il campo resta vuoto e lo sceglie il negozio).
//
// Il riconoscimento sbagliato metterebbe la busta sul cliente di un altro: per questo solo i due
// casi certi e mai un'approssimazione.

// Cliente è un cliente dell'anagrafica, come serve al riconoscimento.
type Cliente struct {
	ID       int64
	Nome     string
	Telefono string
}

// Oreficeria è un'oreficeria esterna (id > 1), come la propone il menu della busta nuova.
type Oreficeria struct {
	ID   int64
	Nome string
}

// Negozio è quello che il negozio conosce già: tutti i clienti (list_customers/0) e le oreficerie
// esterne (list_external_oreficerie/0). Lo legge dal database il chiamante (la pagina della busta
// nuova, 108), così assist non dipende da buste. L'ordine non conta: qui si mettono per nome e id,
// come l'ORDER BY nome, id di SQLite (confronto dei byte).
type Negozio struct {
	Clienti    []Cliente
	Oreficerie []Oreficeria
}

// Il messaggio di assist_summary/1 sotto la casella del dettato quando nessun nome è corretto.
const messaggioEstratti = "✓ Dati estratti. Verifica e correggi prima di salvare."

// Messaggio è il messaggio da mostrare dopo l'estrazione: la correzione del nome solo se il nome
// di prima non era vuoto (assist_summary/1 guarda il valore: un nome svuotato dalla regola della
// persona e poi trovato dal telefono non è una correzione da annunciare).
func (p Proposta) Messaggio() string {
	if p.NomeCorretto && p.NomeCorrettoDa != "" {
		// Concatenato, mai con segnaposto da sostituire: un nome può contenere «%s».
		return "✓ Nome cliente corretto da «" + p.NomeCorrettoDa + "» a «" + p.NomeCliente + "». Verifica i dati prima di salvare."
	}
	return messaggioEstratti
}

// regoleDelNome applica alla proposta le regole del nome, dell'oreficeria e del cliente (read_answer/3
// dopo i campi semplici, poi shape/3 e la coppia del modulo). nome e oreficeria sono come li dà il
// modello (to_string/1); il telefono della proposta è già quello del dettato, normalizzato.
func regoleDelNome(p *Proposta, nome, oreficeria, dettato string, n Negozio) {
	orefs := slices.Clone(n.Oreficerie)
	slices.SortStableFunc(orefs, func(a, b Oreficeria) int {
		return cmp.Or(strings.Compare(a.Nome, b.Nome), cmp.Compare(a.ID, b.ID))
	})
	clienti := slices.Clone(n.Clienti)
	slices.SortStableFunc(clienti, func(a, b Cliente) int {
		return cmp.Or(strings.Compare(a.Nome, b.Nome), cmp.Compare(a.ID, b.ID))
	})
	note := make([]string, len(orefs))
	for i, o := range orefs {
		note[i] = o.Nome
	}

	p.Oreficeria = titolo(oreficeria)
	p.NomeCliente = nomePersona(titolo(conApertura(nome, dettato, note)), p.Oreficeria, note)
	p.OreficeriaID = oreficeriaDi(p.Oreficeria, orefs)
	if r, ok := riconosci(p.NomeCliente, p.TelefonoCliente, clienti); ok && r != p.NomeCliente {
		p.NomeCorretto, p.NomeCorrettoDa, p.NomeCliente = true, p.NomeCliente, r
	}
	if p.TelefonoCliente == "" {
		p.TelefonoCliente = telefonoDellUnico(clienti, p.NomeCliente)
	}
}

// Spazi ai bordi, parole e minuscole come le funzioni di String di Elixir 1.18.

// trimElixir è String.trim/1: via gli spazi Unicode ai bordi (White_Space, come unicode.IsSpace).
func trimElixir(s string) string { return strings.TrimFunc(s, unicode.IsSpace) }

// spazioDivide dice se r divide le parole per String.split/1: gli spazi Unicode tranne quelli non
// separabili (U+00A0, U+2007, U+202F). Misurato con Elixir 1.18 su tutti i caratteri.
func spazioDivide(r rune) bool {
	return unicode.IsSpace(r) && r != 0xa0 && r != 0x2007 && r != 0x202f
}

// paroleElixir è String.split/1: le parole fra gli spazi che dividono, senza pezzi vuoti.
func paroleElixir(s string) []string { return strings.FieldsFunc(s, spazioDivide) }

// minuscoleElixir è String.downcase/1 (modalità predefinita): la tabella semplice più «İ» →
// «i» + punto combinante, l'unica mappatura senza condizioni di SpecialCasing per le minuscole
// (la stessa regola di buste.minuscole).
func minuscoleElixir(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "\u0130", "i\u0307"))
}

// maiuscoleElixir è String.upcase/1: con le mappature complete di SpecialCasing («ß» → «SS»).
// Si può condividere fra goroutine: per language.Und il trasformatore è undUpperCaser, senza stato
// (transform.NopResetter, il contesto vive sullo stack di Transform).
var maiuscoleElixir = cases.Upper(language.Und)

// titolo è title_case/1 dello standalone, la sua resa di str.title() di Python (che è del
// sorgente Flask): tolti gli spazi ai bordi, un grafema per volta, una lettera che non segue una
// lettera va in maiuscolo e tutto il resto in minuscolo. È lettera solo un grafema di un solo
// carattere \p{L}: «é» scritta come «e» + accento combinante non lo è, e la lettera dopo torna
// maiuscola («e\u0301Lodie»), come nello standalone.
func titolo(s string) string {
	s = trimElixir(s)
	var b strings.Builder
	dopoLettera := false
	for g := uniseg.NewGraphemes(s); g.Next(); {
		r := g.Runes()
		lettera := len(r) == 1 && unicode.IsLetter(r[0])
		if lettera && !dopoLettera {
			b.WriteString(maiuscoleElixir.String(g.Str()))
		} else {
			b.WriteString(minuscoleElixir(g.Str()))
		}
		dopoLettera = lettera
	}
	return b.String()
}

// confrontabile è comparable_name/1: senza accenti (NFKD), minuscolo e con le sole lettere e cifre
// ASCII, così «Luca D'Angelo» e «luca dangelo» sono la stessa persona. Lo standalone toglie anche i
// segni combinanti (\p{Mn}) prima delle minuscole: qui li toglie già il filtro finale, perché nessuno
// diventa una lettera o una cifra ASCII.
func confrontabile(s string) string {
	s = minuscoleElixir(norm.NFKD.String(s))
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// telefonoConfrontabile è comparable_phone/1: solo le cifre (\d Unicode), senza «0039» davanti, o
// senza «39» davanti a un numero di più di 10 cifre.
func telefonoConfrontabile(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if unicode.Is(unicode.Nd, r) {
			b.WriteRune(r)
			n++
		}
	}
	c := b.String()
	switch {
	case strings.HasPrefix(c, "0039"):
		return c[4:]
	case strings.HasPrefix(c, "39") && n > 10:
		return c[2:]
	}
	return c
}

// segnaposti sono @placeholders dello standalone (SEGNAPOSTI_NOME del Flask), già confrontabili:
// quello che i modelli scrivono quando nessuno è nominato.
var segnaposti = []string{"cliente", "clientenonidentificato", "clientesconosciuto", "clientenonindicato",
	"clientenonspecificato", "sconosciuto", "nonindicato", "nonspecificato", "nd"}

// reNegozio è @shop_name (~r/^\s*(oreficeria|gioielleria)\b/iu) senza il \b, che con il flag u è
// Unicode e si controlla a mano (parolaPCRE).
var reNegozio = regexp.MustCompile(`(?i)\A` + spazioPCRE + `*(?:oreficeria|gioielleria)`)

// parolaPCRE dice se r è un carattere di \w per PCRE con UCP: lettera, numero o «_».
func parolaPCRE(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r) }

// nomeDiNegozio dice se il testo comincia con la parola intera «oreficeria» o «gioielleria».
func nomeDiNegozio(s string) bool {
	m := reNegozio.FindStringIndex(s)
	if m == nil {
		return false
	}
	for _, r := range s[m[1]:] {
		return !parolaPCRE(r)
	}
	return true
}

// nomePersona è person_name/3 (nome_solo_persona del Flask): il nome senza spazi ai bordi, o ""
// quando non è una persona: un segnaposto («Cliente», «Cliente non identificato», «n/d»…), un nome
// che comincia con «Oreficeria»/«Gioielleria», o uguale (confrontabile, mai contenuto)
// all'oreficeria estratta o a una del negozio.
func nomePersona(nome, oreficeria string, note []string) string {
	testo := trimElixir(nome)
	chiave := confrontabile(testo)
	switch {
	case chiave == "", slices.Contains(segnaposti, chiave), nomeDiNegozio(testo):
		return ""
	case confrontabile(oreficeria) == chiave:
		return ""
	}
	for _, o := range note {
		if confrontabile(o) == chiave {
			return ""
		}
	}
	return testo
}

// reApertura è @opening (~r/^\s*(?:cliente|signora|signor|sig\.ra|sig\.)\s+([^.,;:!?\n\d]+)/iu),
// con lo spazio e le cifre Unicode di PCRE scritti per esteso: il pezzo dopo l'apertura, fino alla
// prima punteggiatura o cifra.
var reApertura = regexp.MustCompile(`(?i)\A` + spazioPCRE + `*(?:cliente|signora|signor|sig\.ra|sig\.)` +
	spazioPCRE + `+([^.,;:!?\n\p{Nd}]+)`)

// paroleNonNome sono @not_name_words: parole che un nome non ha, quindi il pezzo è una frase.
var paroleNonNome = strings.Fields(`tel telefono cel cell cellulare n preventivo non ha hanno e è e' vuole
	chiede porta portato lascia ritira sconosciuto sconosciuta presente anonimo anonima busta numero per
	con oro argento anello anelli fede fedi collana collane catena catenina bracciale orecchini orecchino
	orologio ciondolo pila cinturino spilla`)

// particelle sono @leading_particles, già confrontabili: quello che un modello scrive prima del
// pezzo di cognome che ha tenuto («il monte» di «del monte»).
var particelle = strings.Fields(`il lo la i gli le l d de da di del dello della dei degli delle dal dallo
	dalla dai dagli dalle`)

// haLettera dice se s ha almeno una lettera (\p{L}).
func haLettera(s string) bool { return strings.IndexFunc(s, unicode.IsLetter) >= 0 }

// haCifra dice se s ha almeno una cifra (\d Unicode).
func haCifra(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return unicode.Is(unicode.Nd, r) }) >= 0
}

// nomeDallApertura è opening_name/2 (nome_da_apertura del Flask): il nome dopo «Cliente»,
// «Signora», «Signor», «Sig.ra» o «Sig.» in testa al dettato, o "". Del pezzo restano le parole
// con una lettera («+39», «(», «/» no); si butta se ha più di quattro parole, una parola che un nome
// non ha, o se contiene (confrontabile) il nome di un'oreficeria del negozio.
func nomeDallApertura(testo string, note []string) string {
	m := reApertura.FindStringSubmatch(testo)
	if m == nil {
		return ""
	}
	var parole []string
	for _, p := range paroleElixir(m[1]) {
		if haLettera(p) {
			parole = append(parole, p)
		}
	}
	candidato := strings.Join(parole, " ")
	minuscole := paroleElixir(minuscoleElixir(candidato))
	if len(minuscole) < 1 || len(minuscole) > 4 {
		return ""
	}
	for _, p := range minuscole {
		if slices.Contains(paroleNonNome, p) {
			return ""
		}
	}
	chiave := confrontabile(candidato)
	for _, o := range note {
		if c := confrontabile(o); c != "" && strings.Contains(chiave, c) {
			return ""
		}
	}
	return candidato
}

// conApertura è with_opening/3: il nome dell'apertura vince su quello del modello quando questo è
// vuoto, ha una cifra o, senza gli articoli e le particelle davanti, è la coda dell'apertura
// («Lana» di «dalla lana»). Un altro nome (un'altra persona, o un nome di battesimo che l'apertura
// non ha) resta.
func conApertura(nome, testo string, note []string) string {
	apertura := nomeDallApertura(testo, note)
	if apertura == "" || restaIlModello(nome, apertura) {
		return nome
	}
	return apertura
}

// restaIlModello è model_name_stays?/2.
func restaIlModello(nome, apertura string) bool {
	sentito := paroleDelNome(nome)
	for len(sentito) > 0 && slices.Contains(particelle, sentito[0]) {
		sentito = sentito[1:]
	}
	detto := paroleDelNome(apertura)
	switch {
	case len(sentito) == 0, haCifra(nome):
		return false
	case len(sentito) <= len(detto) && slices.Equal(detto[len(detto)-len(sentito):], sentito):
		return false // (con più parole dell'apertura Enum.take/2 le dà tutte: mai uguali)
	}
	return true
}

// paroleDelNome è name_words/1: le parole confrontabili, senza quelle che restano vuote.
func paroleDelNome(s string) []string {
	var parole []string
	for _, p := range paroleElixir(s) {
		if c := confrontabile(p); c != "" {
			parole = append(parole, c)
		}
	}
	return parole
}

// oreficeriaDi è match_oreficeria/2: l'id della prima oreficeria (per nome e id) il cui nome, in
// minuscolo, contiene il nome detto o vi è contenuto; 0 se nessuna o se il nome detto è vuoto.
func oreficeriaDi(nome string, orefs []Oreficeria) int64 {
	cercato := minuscoleElixir(trimElixir(nome))
	if cercato == "" {
		return 0
	}
	for _, o := range orefs {
		if n := minuscoleElixir(o.Nome); strings.Contains(n, cercato) || strings.Contains(cercato, n) {
			return o.ID
		}
	}
	return 0
}

// riconosci è recognise/2: il nome del cliente quando è certo chi è, false altrimenti. Prima il
// telefono (almeno 7 cifre dopo i prefissi): un solo nome fra i clienti con quel telefono; poi il
// nome: un solo nome fra i clienti con lo stesso nome confrontabile. Più clienti con lo stesso nome
// scritto uguale contano come un nome solo. I clienti senza nome non contano.
func riconosci(nome, telefono string, clienti []Cliente) (string, bool) {
	cifre := telefonoConfrontabile(telefono)
	if len([]rune(cifre)) >= 7 {
		if n, ok := unSoloNome(clienti, func(c Cliente) bool { return telefonoConfrontabile(c.Telefono) == cifre }); ok {
			return n, true
		}
	}
	chiave := confrontabile(nome)
	if chiave == "" {
		return "", false
	}
	return unSoloNome(clienti, func(c Cliente) bool { return confrontabile(c.Nome) == chiave })
}

// unSoloNome è sole_name/2: il nome (senza spazi ai bordi) se i clienti con un nome che rispondono
// alla condizione hanno tutti lo stesso.
func unSoloNome(clienti []Cliente, condizione func(Cliente) bool) (string, bool) {
	trovato := ""
	for _, c := range clienti {
		n := trimElixir(c.Nome)
		if n == "" || !condizione(c) {
			continue
		}
		if trovato != "" && trovato != n {
			return "", false
		}
		trovato = n
	}
	return trovato, trovato != ""
}

// telefonoDellUnico è il telefono che la coppia del modulo propone quando il dettato non ne ha uno (D-106, Stefano
// 20/11/2026, aggiornata lo stesso giorno). Contano i clienti con lo stesso nome confrontabile (confrontabile, la chiave
// di riconosci: a parte accenti, spazi, apostrofi e maiuscole): se tutti hanno lo stesso telefono (telefono.Normalizza e
// poi telefonoConfrontabile, così «333 1234567» e «+393331234567» sono lo stesso numero)
// si propone quello del primo per nome e id, com'è salvato, e il salvataggio troverà la scheda con quel telefono;
// con telefoni diversi, o senza nessun cliente con quel nome, il campo resta vuoto. Lo standalone
// (assign_customer_pair/3) dà il telefono del primo con lo stesso nome a parte maiuscole e spazi, anche fra omonimi.
// clienti sono già per nome e id.
func telefonoDellUnico(clienti []Cliente, nome string) string {
	chiave := confrontabile(nome)
	if chiave == "" {
		return ""
	}
	trovato, proposto, confronto := false, "", ""
	for _, c := range clienti {
		if confrontabile(c.Nome) != chiave {
			continue
		}
		t := telefonoConfrontabile(telefono.Normalizza(c.Telefono))
		if !trovato {
			trovato, proposto, confronto = true, c.Telefono, t
			continue
		}
		if t != confronto {
			return ""
		}
	}
	return proposto
}
