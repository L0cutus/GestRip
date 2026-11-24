package assist

import (
	"cmp"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math"
	mrand "math/rand/v2"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// I pseudonimi dell'esportazione dei dettati (slice 116; RIP-H-52, solo la parte dei pseudonimi), come Assist.Pseudonym
// dello standalone a go-baseline (lib/gestrip_standalone/assist/pseudonym.ex, test pseudonym_test.exs) più il primo
// scarto di TrainingExport.export/5 (cliente escluso). Un dettato con la risposta attesa e la proposta del modello
// entra; esce lo stesso con nome, cognome e telefono del cliente sostituiti da valori inventati, oppure il motivo per cui
// la riga si butta. L'esportazione vera (file, cancellazione delle righe) è della 117.
//
// Regole dello standalone, nel suo ordine:
//   - si sostituiscono solo nome, cognome e telefono del cliente, ovunque compaiano (testo, descrizione, note,
//     proposta), più ogni altro numero con la forma di un telefono; date, oggetti, lavori, incisioni, oreficerie e
//     numeri di busta dettati restano;
//   - il nome atteso è quello trascritto, intero (la regola «Pelizari»): i candidati (nome attuale della busta, nome
//     scritto dal modello prima della correzione dall'anagrafica, nome proposto) si cercano nel testo, prima uguali a
//     parte accenti, maiuscole, spazi e apostrofi, poi con una grafia vicina (Jaro ≥ 0,9, stessa iniziale); vince il
//     primo uguale, una grafia vicina vale solo se è l'unica;
//   - un dubbio si butta: nome in nessuna forma nel testo, due grafie vicine diverse, nome di sole particelle, una parola
//     del nome altrove in minuscolo («oro rosa» o Rosa?), un numero con la forma di un telefono che non è di nessuno
//     dei noti, un valore che non è testo né numero, e la rete finale: una parola o un pezzo eliso («dell'Elisa») di un
//     nome vero, una seconda grafia con la maiuscola («Rosi» accanto a «Rossi»), un numero vero fra tutte le cifre di un
//     campo messe in fila («0423/52 1234»), ancora presenti dopo la sostituzione;
//   - i nomi degli altri si buttano, non si sostituiscono: una parola intera del nome di un altro cliente o di un nome
//     proprio comune (ParoleDeiNomi), che non sia del cliente stesso, butta la riga (altro_nome).
//
// La chiave (Chiave) nasce per una sola esportazione da crypto/rand e non si salva mai: è tutta la tabella delle
// sostituzioni. I valori inventati vengono da un generatore con il seme HMAC-SHA256(chiave, id della riga), quindi la
// stessa riga con la stessa chiave dà lo stesso risultato. Il generatore non è quello dello standalone (:rand exsss): i
// nomi e le cifre inventati sono altri, le regole le stesse.
//
// Puro: niente database, file o registro; non restituisce mai un originale.

// MotivoScarto è il perché una riga non si esporta: i nomi sono quelli dello standalone (le righe «scartati_…» del
// resoconto). Vuoto: la riga si esporta.
type MotivoScarto string

const (
	ScartoNomeNonTrovato    MotivoScarto = "nome_non_trovato"
	ScartoNomeAmbiguo       MotivoScarto = "nome_ambiguo"
	ScartoNomeResiduo       MotivoScarto = "nome_residuo"
	ScartoTelefonoDubbio    MotivoScarto = "telefono_dubbio"
	ScartoTelefonoResiduo   MotivoScarto = "telefono_residuo"
	ScartoValoreIlleggibile MotivoScarto = "valore_illeggibile"
	ScartoAltroNome         MotivoScarto = "altro_nome"
	ScartoClienteEscluso    MotivoScarto = "cliente_escluso"
	// ScartoErrore è un caso che non dovrebbe succedere (lo standalone andrebbe in eccezione e la sua esportazione
	// conterebbe la riga come «errore»): si butta la riga.
	ScartoErrore MotivoScarto = "errore"
)

// Chiave è la chiave di una sola esportazione. Non si stampa e non va nel registro: %v, %+v, %#v e slog la nascondono.
type Chiave struct{ b [32]byte }

// NuovaChiave dà una chiave nuova da crypto/rand (:crypto.strong_rand_bytes(32) dello standalone).
func NuovaChiave() (Chiave, error) {
	var c Chiave
	if _, err := rand.Read(c.b[:]); err != nil {
		return Chiave{}, fmt.Errorf("pseudonimi: chiave: %w", err)
	}
	return c, nil
}

// Format nasconde la chiave con ogni verbo di fmt (%d, %x, %q…), non solo con quelli che usano String.
func (c Chiave) Format(f fmt.State, verbo rune) {
	if verbo == 'v' && f.Flag('#') {
		_, _ = io.WriteString(f, c.GoString())
		return
	}
	_, _ = io.WriteString(f, c.String())
}

// String nasconde la chiave (%v, %+v, %s).
func (Chiave) String() string { return "**redacted**" }

// GoString la nasconde anche con %#v.
func (Chiave) GoString() string { return "assist.Chiave{**redacted**}" }

// LogValue la nasconde nel registro slog.
func (Chiave) LogValue() slog.Value { return slog.StringValue("**redacted**") }

// RigaDaEsportare è una riga raccolta unita ai dati attuali della busta, con i dati veri: non si stampa né va nel
// registro.
type RigaDaEsportare struct {
	ID    int64
	Testo string
	// Nomi sono i nomi candidati in ordine di fiducia: quello attuale della busta, quello che il modello aveva scritto
	// prima della correzione dall'anagrafica, quello proposto ("" se manca).
	Nomi []string
	// Telefoni sono i telefoni candidati: quello attuale della busta e quello proposto.
	Telefoni []string
	// Risposta (la busta com'è ora) e Proposta (del modello): testi, numeri, booleani o nil; un altro valore butta la
	// riga.
	Risposta map[string]any
	Proposta map[string]any
	// Altri sono le parole dei nomi degli altri (ParoleDiAltriNomi); nil: nessuna.
	Altri map[string]bool
	// ClienteEscluso: il cliente della busta si è opposto alla raccolta (dettati_esclusioni).
	ClienteEscluso bool
}

// Format nasconde la riga con ogni verbo di fmt.
func (r RigaDaEsportare) Format(f fmt.State, verbo rune) {
	if verbo == 'v' && f.Flag('#') {
		_, _ = io.WriteString(f, r.GoString())
		return
	}
	_, _ = io.WriteString(f, r.String())
}

// String nasconde la riga (%v, %+v).
func (RigaDaEsportare) String() string { return "{RigaDaEsportare:**redacted**}" }

// GoString la nasconde anche con %#v.
func (RigaDaEsportare) GoString() string { return "assist.RigaDaEsportare{**redacted**}" }

// LogValue la nasconde nel registro slog.
func (RigaDaEsportare) LogValue() slog.Value { return slog.StringValue("**redacted**") }

// RigaPseudonima è la riga con nome e telefono inventati. In Risposta, "nome" è il nome inventato scritto dove nel testo
// c'era quello vero ("" se il testo non nomina nessuno) e "telefono" il numero inventato scritto dove c'era uno noto (""
// se il testo non ne ha).
type RigaPseudonima struct {
	Testo    string
	Risposta map[string]any
	Proposta map[string]any
}

// Nomi inventati: nomi e cognomi italiani comuni scelti a mano (gli stessi dello standalone), nessuno preso da un
// cliente.
var (
	nomiInventati = strings.Fields(`Alba Bruna Carla Dora Elsa Fiora Gilda Ines Lia Mirta Nives Olga Pia Rita
		Selma Tea Aldo Bruno Carlo Dario Elio Fausto Guido Ivo Lino Nello Oreste Piero Renzo Sandro Tullio Ugo`)
	cognomiInventati = strings.Fields(`Albani Benetti Casoni Dalmonte Ermini Fabbris Galvani Ianni Lorenzon Marcon
		Nardin Orsetti Pavan Quaglia Rigon Salton Tonon Ugolini Vardanega Zanin
		Bortot Cecchin Dussin Favaro Gasparin Mazzon Righetto Sartor Visentin Zago`)
)

// Particelle: stanno dentro un nome senza nominare nessuno («in» di «Bianchi in Rossi», «Dalla» di «Dalla Lana»);
// restano come sono e non si cercano altrove.
var particelleNome = insieme(`d de da di del della dalla dal dalle dai dei degli delle dello e in
	van von der den du lo la le li`)

// Nomi propri italiani comuni (common_given_names/0 dello standalone, scritti a mano e da nessun cliente): prendono «per
// il figlio Giovanni» quando nessun Giovanni è cliente. Quelli che sono anche parole del mestiere o di tutti i giorni
// (Rosa, Gemma, Perla, Ambra, Stella…) non ci sono: li porta solo l'anagrafica.
var nomiPropriComuni = strings.Fields(`Giuseppe Giovanni Antonio Mario Luigi Francesco Vincenzo Pietro
	Salvatore Carlo Franco Domenico Bruno Paolo Michele Giorgio Aldo Sergio
	Luciano Marco Andrea Roberto Stefano Alessandro Luca Massimo Matteo
	Lorenzo Davide Simone Federico Riccardo Gabriele Nicola Daniele Fabio
	Claudio Maurizio Enrico Alberto Gianni Renato Emanuele Tommaso Filippo
	Edoardo Giacomo Leonardo Diego Alessio Mattia Samuele Raffaele Umberto
	Gino Valerio Massimiliano Gianluca Gianfranco Giancarlo Fabrizio Cristian
	Christian Ettore Elia Manuel Denis Ivan Mirco Mirko Enzo Renzo Silvano
	Maria Anna Giuseppina Angela Giovanna Teresa Lucia Carmela Caterina
	Francesca Antonietta Carla Elena Concetta Rita Franca Paola
	Laura Sara Giulia Chiara Martina Valentina Alessandra Silvia Elisa
	Federica Roberta Cristina Monica Barbara Daniela Simona Patrizia Luisa
	Sofia Aurora Alice Ginevra Emma Giorgia Greta Beatrice Noemi Matilde
	Elisabetta Claudia Michela Manuela Raffaella Stefania Annalisa Ilaria
	Giuliana Loredana Graziella Lidia Ornella Tiziana Antonella Nadia Debora
	Erika Veronica Irene Ida Eva Mariangela Rossella Vanessa Jessica Elisabeth
	Marta Agnese Camilla Arianna Benedetta Cinzia Donatella Emanuela Gabriella
	Lorena Mara Marisa Mirella Nicoletta Orietta Pamela Rosanna Sabrina Tatiana`)

// Parole che stanno nel campo nome di un cliente senza nominare nessuno: titoli, parentele, parole del negozio e i pezzi
// elisi che un nome scritto con lo spazio dopo l'apostrofo lascia («Dell' Orto» → «dell»).
var nonNomi = insieme(`sig sigra signor signore signora signorina sra dott dottor dottore dottoressa
	ing avv prof geom rag cav fam famiglia cliente clienti moglie marito figlio
	figlia figli madre padre mamma papa nonno nonna zio zia sorella fratello
	cognato cognata suocero suocera amico amica ditta negozio oreficeria
	gioielleria orologeria busta buste srl snc sas spa presso per con non che una uno
	del dei nel sul alla allo agli alle via piazza corso
	dell dall nell sull coll all dagl degl`)

// Parole del mestiere che sono anche cognomi o nomi (slice 185 dello standalone, Stefano 27/10): mai il nome di un
// altro, anche se un cliente le porta.
var paroleDelMestiere = strings.Fields(`oro argento platino bianco bianca rosa rosso rossa giallo gialla nero nera
	perla perle rubino rubini smeraldo smeraldi zaffiro zaffiri diamante diamanti
	brillante brillanti corallo coralli granato granati ambra gemma
	fede croce catena corona pila cassa maglia molla fiore stella cuore
	costa piccolo piccola grande`)

var mestiere = insieme(strings.Join(paroleDelMestiere, " "))

func insieme(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

// NomiInventati sono i nomi propri inventati.
func NomiInventati() []string { return slices.Clone(nomiInventati) }

// CognomiInventati sono i cognomi inventati.
func CognomiInventati() []string { return slices.Clone(cognomiInventati) }

// NomiPropriComuni sono i nomi propri comuni che contano come nome di un altro.
func NomiPropriComuni() []string { return slices.Clone(nomiPropriComuni) }

// ParoleDelMestiere sono le parole del mestiere che non contano mai come nome.
func ParoleDelMestiere() []string { return slices.Clone(paroleDelMestiere) }

const (
	// vicino: soglia di Jaro per una grafia vicina (con la stessa iniziale).
	vicino = 0.9
	// Un numero di nessuno dei noti è un telefono da sostituire con 9-11 cifre (fissi e cellulari italiani senza
	// prefisso del paese) che cominciano con 0 o 3.
	telefonoMin = 9
	telefonoMax = 11
	// Un telefono noto di meno cifre non si cerca.
	chiaveTelefonoMin = 6
)

var (
	// reParola è una parola: lettere, segni combinanti e apostrofi («D'Angelo»).
	// D-111 (1): anche U+02BC, U+02B9 e U+2032 valgono come apostrofi.
	reParola = regexp.MustCompile(`[\p{L}\p{M}'’\x{2bc}\x{2b9}\x{2032}]+`)
	// reFilaCifre è una fila di cifre. D-111 (2): le cifre sono unite anche attraverso «/», «,», punti, trattini, spazi
	// Unicode (lo spazio non separabile compreso), spazi multipli e (D-111 (3), 24/11/2026) gli a capo; non attraverso
	// un altro segno. Lo standalone ammetteva un solo spazio, punto o trattino, mai una barra (reFilaStandalone).
	reFilaCifre = regexp.MustCompile(`\+?[0-9](?:[\p{Zs}\t\n\r.,/\-]*[0-9])*`)
	// reFilaStandalone è la fila di cifre dello standalone (@digit_run): D-111 (1b) la usa per sapere quali numeri lo
	// standalone riconosceva da solo.
	reFilaStandalone = regexp.MustCompile(`\+?[0-9](?:[ .\-]?[0-9])*`)
	// reFila00 è l'inizio di una fila che comincia con «00»: D-111 (2), correzione del coordinatore del 24/11/2026, si
	// unisce solo attraverso gli spazi, così «0045/26 24/11/2026» (busta e data) non sembra un prefisso estero.
	reFila00 = regexp.MustCompile(`^00(?:[\p{Zs}\t]*[0-9])*`)
)

// ParoleDeiNomi è name_words/1: le parole dei nomi che nominano qualcuno, in forma confrontabile: parole intere, senza
// particelle, parole sotto le tre lettere, titoli e parole del negozio, e mai una parola del mestiere.
func ParoleDeiNomi(nomi []string) map[string]bool {
	m := map[string]bool{}
	for _, n := range nomi {
		for _, w := range reParola.FindAllString(n, -1) {
			k := confrontabile(w)
			if len(k) >= 3 && !particelleNome[k] && !nonNomi[k] && !mestiere[k] {
				m[k] = true
			}
		}
	}
	return m
}

// ParoleDiAltriNomi è other_names/0 di TrainingExport senza il database: le parole dei nomi di tutti i clienti e dei
// nomi propri comuni, meno quelle delle oreficerie, che restano nel file per scelta di Stefano (27/10/2026).
func ParoleDiAltriNomi(clienti, oreficerie []string) map[string]bool {
	m := ParoleDeiNomi(append(slices.Clone(clienti), nomiPropriComuni...))
	for k := range ParoleDeiNomi(oreficerie) {
		delete(m, k)
	}
	return m
}

// arco è un tratto del testo, in byte.
type arco struct{ da, lung int }

// Pseudonimizza è pseudonymize/2: la riga con nome e telefono del cliente sostituiti, o il motivo dello scarto (con la
// riga vuota). Un cliente escluso si scarta prima di tutto (TrainingExport.export/5).
func Pseudonimizza(r RigaDaEsportare, k Chiave) (RigaPseudonima, MotivoScarto) {
	if r.ClienteEscluso {
		return RigaPseudonima{}, ScartoClienteEscluso
	}
	p := pseudonimo{r: senzaFormato(r), rnd: generatore(k, r.ID)}
	out, m := p.fai()
	if m != "" {
		return RigaPseudonima{}, m
	}
	return out, ""
}

// senzaFormato è D-111 (1): prima di ogni altra cosa si tolgono i caratteri di formato (\p{Cf}: ZWJ, ZWNJ, trattino
// morbido, BOM…) dal testo, dai candidati e da ogni testo di risposta e proposta, così un nome non si nasconde dietro un
// carattere invisibile. La riga di chi chiama non si tocca.
func senzaFormato(r RigaDaEsportare) RigaDaEsportare {
	r.Testo = togliFormato(r.Testo)
	r.Nomi = slices.Clone(r.Nomi)
	for i := range r.Nomi {
		r.Nomi[i] = togliFormato(r.Nomi[i])
	}
	r.Telefoni = slices.Clone(r.Telefoni)
	for i := range r.Telefoni {
		r.Telefoni[i] = togliFormato(r.Telefoni[i])
	}
	r.Risposta, r.Proposta = campiSenzaFormato(r.Risposta), campiSenzaFormato(r.Proposta)
	return r
}

func campiSenzaFormato(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			v = togliFormato(s)
		}
		out[k] = v
	}
	return out
}

// apostrofo dice se r vale come apostrofo (D-111 (1)).
func apostrofo(r rune) bool {
	return r == '\'' || r == '’' || r == 0x2bc || r == 0x2b9 || r == 0x2032
}

// copiaDiRicerca è D-111 (iv) (coordinatore, 24/11/2026): la copia di s in cui si cercano nomi nascosti. Gli apostrofi
// diventano «'» e restano; ogni altro segno non ASCII (\p{P} o \p{S}) che sta fra due lettere si toglie («Ros‧si» →
// «Rossi»). Il testo esportato non cambia: un nome che si vede solo in questa copia fa scartare la riga (altroNome e la
// rete finale guardano anche qui).
func copiaDiRicerca(s string) string {
	r := []rune(s)
	for i, c := range r {
		if apostrofo(c) {
			r[i] = '\''
		}
	}
	segno := func(c rune) bool { return c > unicode.MaxASCII && (unicode.IsPunct(c) || unicode.IsSymbol(c)) }
	var out []rune
	for i := 0; i < len(r); i++ {
		if segno(r[i]) && i > 0 && unicode.IsLetter(r[i-1]) {
			j := i
			for j < len(r) && segno(r[j]) {
				j++
			}
			if j < len(r) && unicode.IsLetter(r[j]) {
				i = j - 1
				continue
			}
		}
		out = append(out, r[i])
	}
	return string(out)
}

// copiaDiRicercaDi è la riga con ogni testo nella sua copia di ricerca (solo per la rete finale).
func copiaDiRicercaDi(o RigaPseudonima) RigaPseudonima {
	c := RigaPseudonima{Testo: copiaDiRicerca(o.Testo), Risposta: map[string]any{}, Proposta: map[string]any{}}
	for _, lato := range []struct{ da, a map[string]any }{{o.Risposta, c.Risposta}, {o.Proposta, c.Proposta}} {
		for k, v := range lato.da {
			if t, ok := v.(string); ok {
				v = copiaDiRicerca(t)
			}
			lato.a[k] = v
		}
	}
	return c
}

func togliFormato(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}

// generatore è seed/2: un ChaCha8 con il seme HMAC-SHA256(chiave, id).
func generatore(k Chiave, id int64) *mrand.Rand {
	h := hmac.New(sha256.New, k.b[:])
	h.Write([]byte(strconv.FormatInt(id, 10)))
	var seme [32]byte
	copy(seme[:], h.Sum(nil))
	return mrand.New(mrand.NewChaCha8(seme))
}

type pseudonimo struct {
	r   RigaDaEsportare
	rnd *mrand.Rand
}

func (p *pseudonimo) fai() (RigaPseudonima, MotivoScarto) {
	testo := p.r.Testo
	var nomi []string
	for _, n := range p.r.Nomi {
		if n = trimElixir(n); n != "" {
			nomi = append(nomi, n)
		}
	}
	var chiaviTel []string
	for _, t := range p.r.Telefoni {
		if c, ok := chiaveTelefono(t); ok && !slices.Contains(chiaviTel, c) {
			chiaviTel = append(chiaviTel, c)
		}
	}
	if !leggibile(p.r.Risposta) || !leggibile(p.r.Proposta) {
		return RigaPseudonima{}, ScartoValoreIlleggibile
	}
	if p.altroNome(testo, nomi) {
		return RigaPseudonima{}, ScartoAltroNome
	}
	archi, m := trovaNome(nomi, testo)
	if m != "" {
		return RigaPseudonima{}, m
	}
	if len(archi) > 0 && len(identificanti(taglio(testo, archi[0]))) == 0 {
		return RigaPseudonima{}, ScartoNomeAmbiguo
	}
	nomiFinti, ok := p.tabellaNomi(archi, testo, nomi)
	if !ok {
		return RigaPseudonima{}, ScartoNomeAmbiguo
	}
	trascritto, m := telefonoTrascritto(testo, chiaviTel)
	if m != "" {
		return RigaPseudonima{}, m
	}
	if m := soloFormeNuove(append(append([]string{testo}, testi(p.r.Risposta)...), testi(p.r.Proposta)...), chiaviTel); m != "" {
		return RigaPseudonima{}, m
	}
	telFinti := p.tabellaTelefoni(testo, chiaviTel)
	nuovo, ok := sostituisciParole(testo, nomiFinti, archi)
	if !ok {
		return RigaPseudonima{}, ScartoNomeAmbiguo
	}
	nuovo = sostituisciTelefoni(nuovo, telFinti)
	risposta, ok := sostituisciCampi(p.r.Risposta, nomiFinti, telFinti)
	if !ok {
		return RigaPseudonima{}, ScartoNomeAmbiguo
	}
	proposta, ok := sostituisciCampi(p.r.Proposta, nomiFinti, telFinti)
	if !ok {
		return RigaPseudonima{}, ScartoNomeAmbiguo
	}
	detto, ok := nomeDetto(nuovo, archi, testo)
	if !ok {
		return RigaPseudonima{}, ScartoErrore
	}
	risposta["nome"] = detto
	risposta["telefono"] = ""
	if trascritto != "" {
		risposta["telefono"] = telFinti[trascritto]
	}
	out := RigaPseudonima{Testo: nuovo, Risposta: risposta, Proposta: proposta}
	if m := controllaResidui(out, nomi, nomiFinti, chiaviTel); m != "" {
		return RigaPseudonima{}, m
	}
	// D-111 (iv): la rete anche sulla copia di ricerca del risultato; un nome che si vede solo lì fa scartare la riga.
	if m := controllaResidui(copiaDiRicercaDi(out), nomi, nomiFinti, chiaviTel); m != "" {
		return RigaPseudonima{}, m
	}
	return out, ""
}

// leggibile è readable/1: solo testi, numeri, booleani e nil; una lista o una mappa passerebbe la sostituzione e la rete
// senza essere vista.
func leggibile(m map[string]any) bool {
	for _, v := range m {
		switch v.(type) {
		case nil, string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, json.Number:
		case float32, float64:
			// Un float si legge solo se è un intero esatto (correzione della revisione della 116): un telefono arriva
			// come float64 da json.Unmarshal; un decimale o un numero troppo grande non si può controllare.
			if _, ok := testoDelValore(v); !ok {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// testi sono i valori di testo di m (strings/1), nell'ordine delle chiavi come Map.values/1 di Elixir: l'ordine decide
// quali cifre inventate tocchino a quale numero, e la stessa riga con la stessa chiave deve dare lo stesso risultato.
// Un numero intero o un json.Number conta come il suo testo (correzione della revisione della 116): un telefono salvato come
// numero è un telefono lo stesso.
func testi(m map[string]any) []string {
	var v []string
	for _, k := range slices.Sorted(maps.Keys(m)) {
		if s, ok := testoDelValore(m[k]); ok {
			v = append(v, s)
		}
	}
	return v
}

// testoDelFloat è il testo di un float intero esatto, cioè non oltre limite (2^53 per float64, 2^24 per float32); ok
// false per un decimale, un numero troppo grande, NaN o infinito.
func testoDelFloat(x, limite float64) (string, bool) {
	if x != math.Trunc(x) || math.Abs(x) > limite || math.IsNaN(x) {
		return "", false
	}
	return strconv.FormatFloat(x, 'f', -1, 64), true
}

// testoDelValore è il testo di un valore che si legge come testo: una stringa, un json.Number o un intero (in decimale). Un
// float, un booleano o nil no.
func testoDelValore(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case json.Number:
		return string(x), true
	case int:
		return strconv.FormatInt(int64(x), 10), true
	case int8:
		return strconv.FormatInt(int64(x), 10), true
	case int16:
		return strconv.FormatInt(int64(x), 10), true
	case int32:
		return strconv.FormatInt(int64(x), 10), true
	case int64:
		return strconv.FormatInt(x, 10), true
	case uint:
		return strconv.FormatUint(uint64(x), 10), true
	case uint8:
		return strconv.FormatUint(uint64(x), 10), true
	case uint16:
		return strconv.FormatUint(uint64(x), 10), true
	case uint32:
		return strconv.FormatUint(uint64(x), 10), true
	case uint64:
		return strconv.FormatUint(x, 10), true
	case float64:
		return testoDelFloat(x, 1<<53)
	case float32:
		return testoDelFloat(float64(x), 1<<24)
	}
	return "", false
}

// altroNome è no_other_name/3: una parola del nome di un altro, ovunque, è un dubbio; le parole del cliente e i pezzi
// delle sue parole elise no.
func (p *pseudonimo) altroNome(testo string, nomi []string) bool {
	if len(p.r.Altri) == 0 {
		return false
	}
	propri := map[string]bool{}
	for _, n := range nomi {
		for _, w := range pezzi(n) {
			propri[confrontabile(w)] = true
		}
	}
	tutti := append(append([]string{testo}, testi(p.r.Risposta)...), testi(p.r.Proposta)...)
	for _, s := range tutti {
		// D-111 (iv): anche nella copia di ricerca.
		for _, w := range append(pezzi(s), pezzi(copiaDiRicerca(s))...) {
			if k := confrontabile(w); p.r.Altri[k] && !propri[k] {
				return true
			}
		}
	}
	return false
}

// ---- nomi ----

type trovato int

const (
	nessuno trovato = iota
	uguale
	vicinoUnico
	ambiguo
)

// trovaNome è locate_name/2: i tratti del testo che sono il nome trascritto (nessuno se la riga non nomina nessuno).
func trovaNome(nomi []string, testo string) ([]arco, MotivoScarto) {
	if len(nomi) == 0 {
		return nil, ""
	}
	parole := archiParole(testo)
	type esito struct {
		t     trovato
		archi []arco
	}
	es := make([]esito, len(nomi))
	for i, n := range nomi {
		es[i].t, es[i].archi = cerca(n, parole, testo)
	}
	for _, e := range es {
		if e.t == uguale {
			return e.archi, ""
		}
	}
	for _, e := range es {
		if e.t == ambiguo {
			return nil, ScartoNomeAmbiguo
		}
	}
	for _, e := range es {
		if e.t == vicinoUnico {
			return e.archi, ""
		}
	}
	return nil, ScartoNomeNonTrovato
}

func archiParole(s string) []arco {
	var v []arco
	for _, ix := range reParola.FindAllStringIndex(s, -1) {
		v = append(v, arco{ix[0], ix[1] - ix[0]})
	}
	return v
}

// cerca è find/3 e find/4: prima le finestre di una parola in meno fino a una in più uguali al nome confrontabile, poi le
// grafie vicine con lo stesso numero di parole.
func cerca(nome string, parole []arco, testo string) (trovato, []arco) {
	bersaglio := confrontabile(nome)
	quante := len(reParola.FindAllStringIndex(nome, -1))
	// Un «nome» senza lettere («.», «-», cifre) non nomina nessuno da cercare.
	if quante == 0 || bersaglio == "" {
		return nessuno, nil
	}
	var parolaUguale []arco
	for larg := max(quante-1, 1); larg <= quante+1; larg++ {
		for _, f := range finestre(parole, larg, testo) {
			if confrontabile(taglio(testo, f)) == bersaglio {
				parolaUguale = append(parolaUguale, f)
			}
		}
	}
	if len(parolaUguale) > 0 {
		return uguale, disgiunti(parolaUguale)
	}
	var vicine []arco
	var forme []string
	for _, f := range finestre(parole, quante, testo) {
		if c := confrontabile(taglio(testo, f)); simile(c, bersaglio) {
			vicine = append(vicine, f)
			if !slices.Contains(forme, c) {
				forme = append(forme, c)
			}
		}
	}
	switch len(forme) {
	case 0:
		return nessuno, nil
	case 1:
		return vicinoUnico, disgiunti(vicine)
	default:
		return ambiguo, nil
	}
}

// finestre sono windows/3: file di larg parole consecutive con solo spazi fra loro, come tratti del testo.
func finestre(parole []arco, larg int, testo string) []arco {
	var v []arco
	for i := 0; i+larg <= len(parole); i++ {
		ok := true
		for j := i; j < i+larg-1; j++ {
			a, b := parole[j], parole[j+1]
			if !soloSpazi(testo[a.da+a.lung : b.da]) {
				ok = false
				break
			}
		}
		if ok {
			ultima := parole[i+larg-1]
			v = append(v, arco{parole[i].da, ultima.da + ultima.lung - parole[i].da})
		}
	}
	return v
}

// soloSpazi è ~r/^\s+$/u: almeno uno spazio Unicode e nient'altro. Con /u il \s di PCRE è \p{Z}, \h e \v: gli spazi di
// unicode.IsSpace più U+180E (misurato con Elixir 1.18 dell'immagine dello standalone).
func soloSpazi(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsSpace(r) && r != 0x180e {
			return false
		}
	}
	return true
}

// simile è close?/2: stessa iniziale e Jaro almeno vicino.
func simile(c, bersaglio string) bool {
	return c != "" && bersaglio != "" && c[0] == bersaglio[0] && jaro(c, bersaglio) >= vicino
}

// jaro è String.jaro_distance/2 su testi confrontabili (solo ASCII).
func jaro(a, b string) float64 {
	if a == b {
		return 1
	}
	if a == "" || b == "" {
		return 0
	}
	finestra := max(len(a), len(b))/2 - 1
	finestra = max(finestra, 0)
	presiA := make([]bool, len(a))
	presiB := make([]bool, len(b))
	comuni := 0
	for i := range len(a) {
		da, a2 := max(0, i-finestra), min(len(b), i+finestra+1)
		for j := da; j < a2; j++ {
			if !presiB[j] && a[i] == b[j] {
				presiA[i], presiB[j] = true, true
				comuni++
				break
			}
		}
	}
	if comuni == 0 {
		return 0
	}
	trasp, j := 0, 0
	for i := range len(a) {
		if !presiA[i] {
			continue
		}
		for !presiB[j] {
			j++
		}
		if a[i] != b[j] {
			trasp++
		}
		j++
	}
	m := float64(comuni)
	return (m/float64(len(a)) + m/float64(len(b)) + (m-float64(trasp)/2)/m) / 3
}

// disgiunti è disjoint/1: fra finestre che si sovrappongono resta la più a sinistra.
func disgiunti(v []arco) []arco {
	v = slices.Clone(v)
	slices.SortFunc(v, func(x, y arco) int { return cmp.Or(cmp.Compare(x.da, y.da), cmp.Compare(x.lung, y.lung)) })
	var out []arco
	for _, a := range v {
		if n := len(out); n > 0 && a.da < out[n-1].da+out[n-1].lung {
			continue
		}
		out = append(out, a)
	}
	return out
}

func taglio(s string, a arco) string { return s[a.da : a.da+a.lung] }

// identificanti è identifying/1: le parole del nome senza particelle e senza quelle di meno di due lettere confrontabili.
func identificanti(nome string) []string {
	var v []string
	for _, w := range reParola.FindAllString(nome, -1) {
		if k := confrontabile(w); !particelleNome[k] && len(k) >= 2 {
			v = append(v, w)
		}
	}
	return v
}

// tabellaNomi è name_table/4: ogni parola di un nome vero, per forma confrontabile, a una parola inventata. Prima le
// parole del tratto trascritto, nel loro ordine (un nome proprio, poi cognomi), poi ogni altra parola dei candidati.
// ok false: più parole che nomi inventati liberi (un dubbio).
func (p *pseudonimo) tabellaNomi(archi []arco, testo string, nomi []string) (map[string]string, bool) {
	var detto []string
	if len(archi) > 0 {
		detto = identificanti(taglio(testo, archi[0]))
	}
	var altri []string
	for _, n := range nomi {
		altri = append(altri, identificanti(n)...)
	}
	riservati := map[string]bool{}
	for _, w := range append(slices.Clone(detto), altri...) {
		riservati[confrontabile(w)] = true
	}
	tabella := map[string]string{}
	usati := map[string]bool{}
	aggiungi := func(w string, serbatoio []string) bool {
		k := confrontabile(w)
		if _, c := tabella[k]; c {
			return true
		}
		var scelte []string
		for _, f := range serbatoio {
			if c := confrontabile(f); !riservati[c] && !usati[c] {
				scelte = append(scelte, f)
			}
		}
		if len(scelte) == 0 {
			return false
		}
		f := scelte[p.rnd.IntN(len(scelte))]
		tabella[k] = f
		usati[confrontabile(f)] = true
		return true
	}
	for i, w := range detto {
		serbatoio := cognomiInventati
		if i == 0 && len(detto) > 1 {
			serbatoio = nomiInventati
		}
		if !aggiungi(w, serbatoio) {
			return nil, false
		}
	}
	for _, w := range altri {
		if !aggiungi(w, cognomiInventati) {
			return nil, false
		}
	}
	return tabella, true
}

// nomeDetto è spoken_name/3: il nome inventato letto dove c'era quello vero. Ogni parola si sostituisce con una parola
// sola, quindi sono le stesse posizioni di parola nel testo nuovo. ok false: le parole non tornano (non dovrebbe
// succedere).
func nomeDetto(nuovo string, archi []arco, testo string) (string, bool) {
	if len(archi) == 0 {
		return "", true
	}
	a := archi[0]
	prima := len(reParola.FindAllStringIndex(testo[:a.da], -1))
	quante := len(reParola.FindAllStringIndex(taglio(testo, a), -1))
	parole := reParola.FindAllStringIndex(nuovo, -1)
	if quante == 0 || prima+quante > len(parole) {
		return "", false
	}
	return nuovo[parole[prima][0]:parole[prima+quante-1][1]], true
}

// ---- telefoni ----

// cifre sono le sole cifre ASCII di s.
func soloCifre(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// chiaveTelefono è phone_key/1: le cifre significative, senza «0039» davanti o senza «39» davanti a più di 10 cifre;
// ok false sotto le chiaveTelefonoMin cifre. Lo standalone passa prima da Phone.normalize, che tiene anche cifre non
// ASCII, ma poi tiene solo 0-9: il risultato è lo stesso.
func chiaveTelefono(s string) (string, bool) {
	v := soloCifre(s)
	switch {
	case strings.HasPrefix(v, "0039"):
		v = v[4:]
	case strings.HasPrefix(v, "39") && len(v) > 10:
		v = v[2:]
	}
	return v, len(v) >= chiaveTelefonoMin
}

// fileDiCifre sono le file di cifre di s, come scritte (il «+» davanti compreso).
func fileDiCifre(s string) []string {
	var v []string
	for _, ix := range indiciFile(s) {
		v = append(v, s[ix[0]:ix[1]])
	}
	return v
}

// indiciFile sono le file di cifre di s (inizio e fine in byte). D-111 (1a): un gruppo con la forma di una data è una fila
// a sé e non si unisce mai alle cifre intorno; fra le date valgono le file di indiciTraLeDate.
func indiciFile(s string) [][2]int {
	var v [][2]int
	da := 0
	for _, d := range date(s) {
		v = append(v, indiciTraLeDate(s, da, d[0])...)
		v = append(v, d)
		da = d[1]
	}
	return append(v, indiciTraLeDate(s, da, len(s))...)
}

// date sono le date di s che restano separate dalle altre cifre (D-111 (i)-(iii), coordinatore 24/11/2026): giorno 1-31,
// mese 1-12, anno a 4 cifre, lo stesso separatore («/», «.» o «-») due volte. Si prova ogni inizio possibile da sinistra,
// senza dipendere dall'ordine di una regex; una data non deve avere cifre attaccate prima o dopo, nemmeno attraverso un
// solo separatore («0423.12.11.2026» o «11/11/2026/1» non sono date). Una data con l'anno a 2 cifre non conta: si unisce
// alle altre cifre come qualunque gruppo.
func date(s string) [][2]int {
	var v [][2]int
	for i := 0; i < len(s); i++ {
		if !cifra(s[i]) || attaccata(s, i, -1) {
			continue
		}
		if e, ok := dataDa(s, i); ok && !attaccata(s, e-1, +1) {
			v = append(v, [2]int{i, e})
			i = e - 1
		}
	}
	return v
}

// attaccata dice se dalla cifra in posizione i, nella direzione verso (-1 prima, +1 dopo), c'è un'altra cifra subito o
// dopo un solo separatore.
func attaccata(s string, i, verso int) bool {
	j := i + verso
	if j < 0 || j >= len(s) {
		return false
	}
	if cifra(s[j]) {
		return true
	}
	k := j + verso
	return strings.IndexByte("./-,", s[j]) >= 0 && k >= 0 && k < len(s) && cifra(s[k])
}

// dataDa è la fine della data plausibile che comincia in i, se c'è.
func dataDa(s string, i int) (int, bool) {
	g, j := numeroDa(s, i, 2)
	if g < 1 || g > 31 || j >= len(s) || strings.IndexByte("./-", s[j]) < 0 {
		return 0, false
	}
	sep := s[j]
	m, k := numeroDa(s, j+1, 2)
	if m < 1 || m > 12 || k >= len(s) || s[k] != sep {
		return 0, false
	}
	if _, e := numeroDa(s, k+1, 4); e-(k+1) == 4 {
		return e, true
	}
	return 0, false
}

// numeroDa legge da i al massimo quante cifre; -1 se non ce n'è nessuna.
func numeroDa(s string, i, quante int) (int, int) {
	n, j := 0, i
	for j < len(s) && j-i < quante && cifra(s[j]) {
		n = n*10 + int(s[j]-'0')
		j++
	}
	if j == i {
		return -1, i
	}
	return n, j
}

func cifra(c byte) bool { return c >= '0' && c <= '9' }

// indiciTraLeDate sono le file di reFilaCifre in s[da:a]. Una fila che comincia con «00» si unisce solo attraverso gli
// spazi (reFila00, D-111 (2), correzione del coordinatore del 24/11/2026); se però si ferma a un separatore che non è uno
// spazio, «00NN» si rilegge con la fila che segue (estensioneEstera), così «0041/79 123 45 67» resta un telefono estero.
func indiciTraLeDate(s string, da, a int) [][2]int {
	var v [][2]int
	for da < a {
		ix := reFilaCifre.FindStringIndex(s[da:a])
		if ix == nil {
			break
		}
		x, y := da+ix[0], da+ix[1]
		if strings.HasPrefix(s[x:], "00") {
			pieno := y
			y = x + len(reFila00.FindString(s[x:pieno]))
			if y < pieno {
				y = estensioneEstera(s, x, y, pieno)
			}
		}
		v = append(v, [2]int{x, y})
		da = y
	}
	return v
}

// estensioneEstera è la fine della fila «00…» s[x:y] tagliata a un separatore che non è uno spazio (la fila intera finiva
// a pieno): la fila che segue si legge unita dai soli spazi, oppure dal solo separatore del taglio; se con una delle due
// ci sono almeno telefonoMin cifre dopo il «00», la fila arriva fin lì (la più lunga). Altrimenti resta y. Così «0045/26
// 24/11/2026» (busta e data) resta fuori: «26 24» o «26» danno troppe poche cifre.
func estensioneEstera(s string, x, y, pieno int) int {
	n := y
	for n < pieno && !cifra(s[n]) {
		n++
	}
	sep, _ := utf8.DecodeRuneInString(strings.TrimLeftFunc(s[y:n], spazioDiFila))
	fine := y
	for _, ammesso := range []func(rune) bool{spazioDiFila, func(r rune) bool { return r == sep }} {
		e := n + prefissoUnito(s[n:pieno], ammesso)
		if len(soloCifre(s[x:e]))-2 >= telefonoMin && e > fine {
			fine = e
		}
	}
	return fine
}

func spazioDiFila(r rune) bool { return r == '\t' || unicode.Is(unicode.Zs, r) }

// prefissoUnito è la lunghezza di [0-9](?:ammesso*[0-9])* all'inizio di s.
func prefissoUnito(s string, ammesso func(rune) bool) int {
	fine, i := 0, 0
	for i < len(s) {
		r, w := utf8.DecodeRuneInString(s[i:])
		switch {
		case cifra(s[i]):
			i++
			fine = i
		case i > 0 && ammesso(r):
			i += w
		default:
			return fine
		}
	}
	return fine
}

// formaDiTelefono è phone_shaped?/1: 9-11 cifre significative che cominciano con 0 o 3. D-111 (2): in più un numero con
// un prefisso estero («+NN» o «00NN», 0039 compreso) e almeno 9 cifre dopo il «+» o il «00».
func formaDiTelefono(fila string) bool {
	if formaStandalone(fila) {
		return true
	}
	c := soloCifre(fila)
	return strings.HasPrefix(fila, "+") && len(c) >= telefonoMin || strings.HasPrefix(c, "00") && len(c)-2 >= telefonoMin
}

// formaStandalone è phone_shaped?/1 com'era nello standalone: 9-11 cifre significative che cominciano con 0 o 3.
func formaStandalone(fila string) bool {
	k, ok := chiaveTelefono(fila)
	return ok && len(k) >= telefonoMin && len(k) <= telefonoMax && (k[0] == '0' || k[0] == '3')
}

// telefonoTrascritto è transcribed_phone/2: il numero noto scritto nel testo, se c'è; un numero con la forma di un
// telefono che non è di nessuno dei noti, senza uno noto accanto, è un dubbio.
func telefonoTrascritto(testo string, chiavi []string) (string, MotivoScarto) {
	file := fileDiCifre(testo)
	for _, k := range chiavi {
		for _, f := range file {
			if strings.Contains(soloCifre(f), k) {
				return k, ""
			}
		}
	}
	for _, f := range append(file, fileStandalone(testo)...) {
		if formaDiTelefono(f) {
			return "", ScartoTelefonoDubbio
		}
	}
	return "", ""
}

// restiAttornoAiNoti sono i pezzi della fila f (com'è scritta) fuori dai numeri noti che contiene.
func restiAttornoAiNoti(f string, chiavi []string) []string {
	var pos []int // posizione in f di ogni cifra
	for i := 0; i < len(f); i++ {
		if cifra(f[i]) {
			pos = append(pos, i)
		}
	}
	c := soloCifre(f)
	coperta := make([]bool, len(c))
	for _, k := range chiavi {
		for da := 0; ; {
			i := strings.Index(c[da:], k)
			if i < 0 {
				break
			}
			for j := da + i; j < da+i+len(k); j++ {
				coperta[j] = true
			}
			da += i + len(k)
		}
	}
	var v []string
	inizio := -1
	for j := range c {
		switch {
		case !coperta[j] && inizio < 0:
			inizio = j
		case coperta[j] && inizio >= 0:
			v = append(v, f[pos[inizio]:pos[j-1]+1])
			inizio = -1
		}
	}
	if inizio >= 0 {
		v = append(v, f[pos[inizio]:])
	}
	return v
}

func contieneChiave(c string, chiavi []string) bool {
	for _, k := range chiavi {
		if strings.Contains(c, k) {
			return true
		}
	}
	return false
}

// fileStandalone sono le file di cifre dello standalone (reFilaStandalone), ma senza unire una data alle cifre intorno
// (D-111 (1a)).
func fileStandalone(s string) []string {
	var v []string
	da := 0
	for _, d := range append(date(s), [2]int{len(s), len(s)}) {
		v = append(v, reFilaStandalone.FindAllString(s[da:d[0]], -1)...)
		v = append(v, s[d[0]:d[1]])
		da = d[1]
	}
	return v
}

// soloFormeNuove è D-111 (1b) (coordinatore, 24/11/2026): un numero con la forma di un telefono, che non contiene un
// numero noto e che le file dello standalone non riconoscono da sole (lo riconoscono solo separatori, spazi Unicode o
// multipli, «00»/«+» esteri, a capo), non si riscrive: la riga si scarta. Vale per il testo e per i campi.
func soloFormeNuove(stringhe, chiavi []string) MotivoScarto {
	for _, s := range stringhe {
		vecchie := map[string]bool{}
		for _, f := range fileStandalone(s) {
			if formaStandalone(f) {
				k, _ := chiaveTelefono(f)
				vecchie[k] = true
			}
		}
	fila:
		for _, f := range fileDiCifre(s) {
			c := soloCifre(f)
			if !formaDiTelefono(f) && !contieneChiave(c, chiavi) {
				continue
			}
			if contieneChiave(c, chiavi) {
				// La fila contiene un numero noto: quello che resta attorno, riletto come altre file, se ha la forma di
				// un telefono è un altro numero attaccato al noto («333 000 1234 0041/79 123 45 67»), che non si
				// riscriverebbe: un dubbio.
				for _, r := range restiAttornoAiNoti(f, chiavi) {
					for _, g := range fileDiCifre(r) {
						if formaDiTelefono(g) {
							return ScartoTelefonoDubbio
						}
					}
				}
				continue fila
			}
			if k, _ := chiaveTelefono(f); !vecchie[k] {
				return ScartoTelefonoDubbio
			}
		}
	}
	return ""
}

// tabellaTelefoni è phone_table/4: ogni numero noto e ogni numero con la forma di un telefono (nel testo e nei campi) a
// cifre inventate.
func (p *pseudonimo) tabellaTelefoni(testo string, chiavi []string) map[string]string {
	tabella := map[string]string{}
	tutte := slices.Clone(chiavi)
	for _, s := range append(append([]string{testo}, testi(p.r.Risposta)...), testi(p.r.Proposta)...) {
		// Anche le file dello standalone: un numero che le file nuove uniscono ad altre cifre («busta 45, 0400 123456»)
		// si sostituisce lo stesso dentro la fila più lunga.
		for _, f := range append(fileDiCifre(s), fileStandalone(s)...) {
			if formaDiTelefono(f) {
				k, _ := chiaveTelefono(f)
				tutte = append(tutte, k)
			}
		}
	}
	for _, k := range tutte {
		if _, c := tabella[k]; !c {
			tabella[k] = p.cifreFinte(k)
		}
	}
	return tabella
}

// cifreFinte è fake_digits/2: la prima cifra resta, le altre a caso, mai uguale all'originale.
func (p *pseudonimo) cifreFinte(k string) string {
	for {
		b := []byte(k)
		for i := 1; i < len(b); i++ {
			b[i] = byte('0' + p.rnd.IntN(10))
		}
		if f := string(b); f != k {
			return f
		}
	}
}

// sostituisciTelefoni è replace_phones/2: dentro una fila, ogni numero della tabella si riscrive cifra per cifra, così
// spazi, punti, trattini e prefisso del paese restano dove sono.
func sostituisciTelefoni(s string, tabella map[string]string) string {
	chiavi := make([]string, 0, len(tabella))
	for k := range tabella {
		chiavi = append(chiavi, k)
	}
	// Prima le più lunghe; a parità l'ordine delle chiavi (le mappe piccole di Elixir sono ordinate).
	slices.SortFunc(chiavi, func(a, b string) int { return cmp.Or(cmp.Compare(len(b), len(a)), strings.Compare(a, b)) })
	riscrivi := func(fila string) string {
		c := soloCifre(fila)
		nuove := c
		for _, k := range chiavi {
			nuove = strings.ReplaceAll(nuove, k, tabella[k])
		}
		if nuove == c {
			return fila
		}
		b := []byte(fila)
		j := 0
		for i := range b {
			if b[i] >= '0' && b[i] <= '9' && j < len(nuove) {
				b[i] = nuove[j]
				j++
			}
		}
		return string(b)
	}
	var out strings.Builder
	da := 0
	for _, ix := range indiciFile(s) {
		out.WriteString(s[da:ix[0]])
		out.WriteString(riscrivi(s[ix[0]:ix[1]]))
		da = ix[1]
	}
	out.WriteString(s[da:])
	return out.String()
}

// ---- sostituzione ----

// sostituisciCampi è replace_fields/3; ok false: una parola del nome in minuscolo fuori dal campo nome (un dubbio).
func sostituisciCampi(campi map[string]any, nomi, tel map[string]string) (map[string]any, bool) {
	out := make(map[string]any, len(campi)+2)
	for k, v := range campi {
		s, testo := testoDelValore(v)
		if !testo {
			out[k] = v
			continue
		}
		if _, numero := v.(string); !numero {
			// Un numero si riscrive solo se contiene un telefono, e resta un numero (json.Number, sole cifre).
			if n := sostituisciTelefoni(s, tel); n != s {
				out[k] = json.Number(n)
			} else {
				out[k] = v
			}
			continue
		}
		if k == "nome" {
			s = sostituisciCampoNome(s, nomi)
		} else {
			var ok bool
			if s, ok = sostituisciParole(s, nomi, nil); !ok {
				return nil, false
			}
		}
		out[k] = sostituisciTelefoni(s, tel)
	}
	return out, true
}

// sostituisciCampoNome è replace_name_field/2: un campo nome è un nome dal primo all'ultimo carattere, ogni parola della
// tabella se ne va, in qualunque forma.
func sostituisciCampoNome(s string, nomi map[string]string) string {
	return reParola.ReplaceAllStringFunc(s, func(w string) string {
		if r, esito := sostituisciParola(w, nomi, true); esito == parolaCambiata {
			return r
		}
		return w
	})
}

// sostituisciParole è replace_words/3: nel testo libero una parola del nome si sostituisce dentro il nome trascritto o se
// è scritta con la maiuscola; in minuscolo altrove può essere una parola comune («oro rosa») o il nome: un dubbio (ok
// false).
func sostituisciParole(s string, nomi map[string]string, archi []arco) (string, bool) {
	var b strings.Builder
	cursore := 0
	for _, ix := range reParola.FindAllStringIndex(s, -1) {
		w := s[ix[0]:ix[1]]
		r, esito := sostituisciParola(w, nomi, dentro(ix[0], archi))
		switch esito {
		case parolaDubbia:
			return "", false
		case parolaCambiata:
			b.WriteString(s[cursore:ix[0]])
			b.WriteString(r)
			cursore = ix[1]
		}
	}
	b.WriteString(s[cursore:])
	return b.String(), true
}

type esitoParola int

const (
	parolaUguale esitoParola = iota
	parolaCambiata
	parolaDubbia
)

// pezziConApostrofi è Regex.split(~r/['’]/u, w, include_captures: true): i pezzi e gli apostrofi in mezzo.
func pezziConApostrofi(w string) []string {
	var v []string
	inizio := 0
	for i, r := range w {
		if apostrofo(r) { // D-111 (1)
			v = append(v, w[inizio:i], string(r))
			inizio = i + len(string(r))
		}
	}
	return append(v, w[inizio:])
}

// sostituisciParola è replace_word/3: una parola intera della tabella o, per una parola elisa come «dell'Elisa», un suo
// pezzo; un pezzo segue la regola della parola.
func sostituisciParola(w string, nomi map[string]string, dentro bool) (string, esitoParola) {
	pz := pezziConApostrofi(w)
	esito := parolaUguale
	out := make([]string, len(pz))
	for i, x := range pz {
		out[i] = x
		f, c := nomi[confrontabile(x)]
		if !c {
			continue
		}
		if dentro || maiuscola(x) {
			out[i] = comeLe(f, x)
			if esito != parolaDubbia {
				esito = parolaCambiata
			}
		} else {
			esito = parolaDubbia
		}
	}
	if f, c := nomi[confrontabile(w)]; c && len(pz) > 1 {
		if dentro || maiuscola(w) {
			return comeLe(f, w), parolaCambiata
		}
		return "", parolaDubbia
	}
	if esito == parolaCambiata {
		return strings.Join(out, ""), parolaCambiata
	}
	return "", esito
}

func dentro(da int, archi []arco) bool {
	for _, a := range archi {
		if da >= a.da && da < a.da+a.lung {
			return true
		}
	}
	return false
}

// primoGrafema è String.first/1.
func primoGrafema(s string) string {
	g := uniseg.NewGraphemes(s)
	if g.Next() {
		return g.Str()
	}
	return ""
}

// maiuscola è capitalised?/1: il primo grafema cambia con le minuscole.
func maiuscola(w string) bool {
	p := primoGrafema(w)
	return p != minuscoleElixir(p)
}

// comeLe è case_like/2: un originale tutto maiuscolo (più di un grafema) dà il finto in maiuscolo, uno tutto minuscolo in
// minuscolo, altrimenti il finto com'è.
func comeLe(finto, originale string) string {
	switch {
	case originale == maiuscoleElixir.String(originale) && originale != minuscoleElixir(originale) &&
		uniseg.GraphemeClusterCount(originale) > 1:
		return maiuscoleElixir.String(finto)
	case originale == minuscoleElixir(originale):
		return minuscoleElixir(finto)
	default:
		return finto
	}
}

// ---- la rete ----

// controllaResidui è check_residue/4.
func controllaResidui(out RigaPseudonima, nomi []string, tabella map[string]string, chiaviTel []string) MotivoScarto {
	tutti := append(append([]string{out.Testo}, testi(out.Risposta)...), testi(out.Proposta)...)
	veri := map[string]bool{}
	for k := range tabella {
		veri[k] = true
	}
	for _, n := range nomi {
		for _, w := range identificanti(n) {
			veri[confrontabile(w)] = true
		}
	}
	finti := map[string]bool{}
	for _, f := range tabella {
		finti[confrontabile(f)] = true
	}
	for _, s := range tutti {
		for _, x := range pezzi(s) {
			if veri[confrontabile(x)] {
				return ScartoNomeResiduo
			}
		}
	}
	// Una seconda grafia di un nome vero («Rosi» accanto a «Rossi»), con la maiuscola, che nessuna regola ha sostituito.
	for _, s := range tutti {
		for _, x := range pezzi(s) {
			if !maiuscola(x) {
				continue
			}
			c := confrontabile(x)
			if finti[c] || len(c) < 3 {
				continue
			}
			for v := range veri {
				if simile(c, v) {
					return ScartoNomeResiduo
				}
			}
		}
	}
	// Tutte le cifre di un campo in fila: un numero diviso da una barra, una virgola o due spazi è ancora il numero.
	for _, s := range tutti {
		c := soloCifre(s)
		for _, k := range chiaviTel {
			if strings.Contains(c, k) {
				return ScartoTelefonoResiduo
			}
		}
	}
	return ""
}

// pezzi è pieces/1: ogni parola e ogni pezzo non vuoto di una parola elisa.
func pezzi(s string) []string {
	var v []string
	for _, w := range reParola.FindAllString(s, -1) {
		v = append(v, w)
		for i, x := range pezziConApostrofi(w) {
			if i%2 == 0 && x != "" {
				v = append(v, x)
			}
		}
	}
	return v
}
