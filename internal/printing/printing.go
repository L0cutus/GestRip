// Package printing è la stampa diretta (PIANO §2.3, slice 046): l'interfaccia Printer, la sua
// implementazione con i comandi di CUPS (lp, lpstat, lpoptions) e una stampante finta per i test e
// il banco.
//
// Le regole del PIANO (opzione A):
//   - i comandi si lanciano senza shell, con gli argomenti separati e LC_ALL=C (l'uscita è in
//     inglese fisso, qualunque sia la lingua del sistema);
//   - «a buon fine» vuol dire che lp esce con 0 e risponde «request id is <id> (…)»: l'id del
//     lavoro si legge da lì;
//   - PageSize=Custom.<L>x<H>mm si calcola dai millimetri del documento, mai dall'utente;
//   - le altre opzioni (MediaTracking=Gap, Darkness=8…) si scelgono per stampante e passano come
//     -o chiave=valore, una per argomento, dopo essere state validate (LeggiOpzioni): un valore non
//     può diventare un'opzione di lp;
//   - un solo lavoro lp per tutto il documento.
//
// Il PDF va a lp sullo standard input: nessun file temporaneo con i dati della busta, niente da
// togliere dopo un errore o un arresto a metà, nessuna cartella da proteggere. lp copia il documento
// nella coda di CUPS (/var/spool/cups) prima di rispondere.
package printing

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Queue è una coda di CUPS (un nome di lpstat -e).
type Queue struct {
	Nome string
}

// JobID è l'id del lavoro che CUPS ha accettato («Etichette-12»).
type JobID string

// Options sono le opzioni di un lavoro: il formato della pagina in millimetri (diventa PageSize),
// le opzioni della stampante e il titolo del lavoro (il nome del file, senza dati del cliente).
type Options struct {
	LarghezzaMM, AltezzaMM float64
	Extra                  []Opzione
	Titolo                 string
}

// Opzione è una coppia chiave=valore di lp -o.
type Opzione struct {
	Chiave, Valore string
}

// String è la forma di lp: «chiave=valore».
func (o Opzione) String() string { return o.Chiave + "=" + o.Valore }

// Printer è la stampa diretta: le code disponibili e un lavoro per documento.
type Printer interface {
	List(ctx context.Context) ([]Queue, error)
	Print(ctx context.Context, queue string, pdf []byte, opt Options) (JobID, error)
}

// OpzioneCoda è un'opzione che la stampante dichiara (lpoptions -p <coda> -l): la chiave, il nome
// leggibile, le scelte e quella predefinita.
type OpzioneCoda struct {
	Chiave, Nome string
	Scelte       []string
	Predefinita  string
}

// ConOpzioni è una Printer che sa dire le opzioni di una coda (lpoptions).
type ConOpzioni interface {
	Opzioni(ctx context.Context, queue string) ([]OpzioneCoda, error)
}

// Gli errori della stampa. ErrRifiutato porta il messaggio di CUPS (in inglese, LC_ALL=C).
var (
	ErrNonDisponibile = errors.New("CUPS non risponde")
	ErrTempoScaduto   = errors.New("CUPS non ha risposto in tempo")
	ErrRifiutato      = errors.New("CUPS ha rifiutato il lavoro")
	ErrCodaNonValida  = errors.New("nome della coda non valido")
	ErrOpzioneVietata = errors.New("opzione non permessa")
)

// Rifiuto è il rifiuto di CUPS con il suo messaggio, ripulito (Motivo).
type Rifiuto struct {
	Motivo string
}

func (r Rifiuto) Error() string {
	if r.Motivo == "" {
		return ErrRifiutato.Error()
	}
	return ErrRifiutato.Error() + ": " + r.Motivo
}

func (r Rifiuto) Unwrap() error { return ErrRifiutato }

// reCoda è il nome di una coda che il Go accetta: lettere e cifre ASCII, poi anche «_», «.», «-» e
// «@», al più 127 caratteri (il limite di CUPS). Mai un primo carattere «-», che lp leggerebbe come
// un'opzione; niente spazi, «/» (le istanze di lpoptions), virgolette o caratteri di controllo. Una
// coda di CUPS con un nome fuori da questa regola non si può scegliere (la pagina lo dice).
var reCoda = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{0,126}$`)

// CodaValida dice se il nome si può passare a lp -d.
func CodaValida(nome string) bool { return reCoda.MatchString(nome) }

// Le opzioni: la chiave comincia con una lettera ASCII e ha lettere, cifre, «_» e «-» (come le
// opzioni dei PPD e di IPP); il valore lettere, cifre e «_», «.», «-», «+». Niente «=» nel valore,
// niente spazi, niente virgolette: lp non le separa mai in altre opzioni.
var (
	reChiave = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
	reValore = regexp.MustCompile(`^[A-Za-z0-9_.+-]{1,64}$`)
)

// MaxOpzioni è quante opzioni si accettano per una stampante.
const MaxOpzioni = 16

// chiaviVietate sono le opzioni che il Go sceglie da sé dal documento (PageSize e i suoi sinonimi),
// o che cambierebbero la pagina rispetto al PDF: nessuna si scrive a mano. Confronto senza
// maiuscole, come CUPS per i nomi delle opzioni.
var chiaviVietate = []string{"pagesize", "pageregion", "media", "media-col", "fit-to-page", "print-scaling",
	"scaling", "number-up", "page-ranges", "document-format"}

// LeggiOpzioni legge le opzioni scritte nelle impostazioni: coppie chiave=valore separate da spazi
// («MediaTracking=Gap Darkness=8»). Un testo vuoto non ha opzioni. Una chiave ripetuta, una coppia
// senza «=», una chiave o un valore fuori dalle regole, una delle chiaviVietate o più di MaxOpzioni:
// errore, con la coppia sbagliata nel messaggio.
func LeggiOpzioni(testo string) ([]Opzione, error) {
	var out []Opzione
	for _, coppia := range strings.Fields(testo) {
		k, v, ok := strings.Cut(coppia, "=")
		switch {
		case !ok:
			return nil, fmt.Errorf("%w: «%s» non è nella forma chiave=valore", ErrOpzioneVietata, coppia)
		case !reChiave.MatchString(k):
			return nil, fmt.Errorf("%w: «%s» non è un nome di opzione valido", ErrOpzioneVietata, k)
		case slices.Contains(chiaviVietate, strings.ToLower(k)):
			return nil, fmt.Errorf("%w: «%s» la sceglie GestRip dal formato del documento", ErrOpzioneVietata, k)
		case !reValore.MatchString(v):
			return nil, fmt.Errorf("%w: il valore di «%s» non è valido", ErrOpzioneVietata, k)
		case slices.ContainsFunc(out, func(o Opzione) bool { return strings.EqualFold(o.Chiave, k) }):
			return nil, fmt.Errorf("%w: «%s» è ripetuta", ErrOpzioneVietata, k)
		}
		out = append(out, Opzione{k, v})
	}
	if len(out) > MaxOpzioni {
		return nil, fmt.Errorf("%w: al più %d opzioni", ErrOpzioneVietata, MaxOpzioni)
	}
	return out, nil
}

// ScriviOpzioni è la forma salvata: le coppie separate da uno spazio.
func ScriviOpzioni(oo []Opzione) string {
	s := make([]string, len(oo))
	for i, o := range oo {
		s[i] = o.String()
	}
	return strings.Join(s, " ")
}

// ControllaOpzioni confronta le opzioni con quelle che la stampante dichiara (lpoptions): ogni
// chiave deve esserci (senza maiuscole) e il valore deve essere una delle scelte, o «Custom.…» se la
// stampante ne ha una «Custom.…». Errore con la prima che non va.
func ControllaOpzioni(oo []Opzione, dichiarate []OpzioneCoda) error {
	for _, o := range oo {
		i := slices.IndexFunc(dichiarate, func(d OpzioneCoda) bool { return strings.EqualFold(d.Chiave, o.Chiave) })
		if i < 0 {
			return fmt.Errorf("%w: la stampante non ha l'opzione «%s»", ErrOpzioneVietata, o.Chiave)
		}
		d := dichiarate[i]
		if slices.Contains(d.Scelte, o.Valore) {
			continue
		}
		if strings.HasPrefix(o.Valore, "Custom.") && slices.ContainsFunc(d.Scelte, func(s string) bool { return strings.HasPrefix(s, "Custom.") }) {
			continue
		}
		return fmt.Errorf("%w: «%s» non è una scelta di «%s» (%s)", ErrOpzioneVietata, o.Valore, d.Chiave, strings.Join(d.Scelte, " "))
	}
	return nil
}

// FormatoPagina è il PageSize del documento: «Custom.<L>x<H>mm» con i millimetri scritti con il punto
// e senza zeri inutili (57x32, 57.5x32). Al più due decimali: CUPS li legge come numeri decimali.
func FormatoPagina(l, h float64) (string, error) {
	if !(l > 0 && h > 0 && l <= 1000 && h <= 1000) {
		return "", fmt.Errorf("formato della pagina non valido: %v×%v mm", l, h)
	}
	mm := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	l, h = arrotonda(l), arrotonda(h)
	return "Custom." + mm(l) + "x" + mm(h) + "mm", nil
}

func arrotonda(v float64) float64 {
	n, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'f', 2, 64), 64)
	return n
}

// argomentiLp sono gli argomenti di lp per un lavoro: la coda, il titolo, il PageSize e le
// opzioni, poi nessun file (lp legge lo standard input). Errore se la coda, il titolo, il formato o
// un'opzione non vanno.
func argomentiLp(queue string, opt Options) ([]string, error) {
	if !CodaValida(queue) {
		return nil, ErrCodaNonValida
	}
	ps, err := FormatoPagina(opt.LarghezzaMM, opt.AltezzaMM)
	if err != nil {
		return nil, err
	}
	a := []string{"-d", queue}
	if t := titoloPulito(opt.Titolo); t != "" {
		a = append(a, "-t", t)
	}
	a = append(a, "-o", "PageSize="+ps)
	for _, o := range opt.Extra {
		if !reChiave.MatchString(o.Chiave) || !reValore.MatchString(o.Valore) || slices.Contains(chiaviVietate, strings.ToLower(o.Chiave)) {
			return nil, fmt.Errorf("%w: %q", ErrOpzioneVietata, o.Chiave)
		}
		a = append(a, "-o", o.String())
	}
	return a, nil
}

// titoloPulito tiene del titolo solo lettere, cifre e «_.-» ASCII, al più 64 caratteri, e non lo fa
// cominciare con «-».
func titoloPulito(t string) string {
	var b strings.Builder
	for _, c := range t {
		if b.Len() >= 64 {
			break
		}
		if c < 128 && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-') {
			b.WriteRune(c)
		}
	}
	return strings.TrimLeft(b.String(), "-")
}
