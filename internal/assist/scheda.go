package assist

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/telefono"
)

// Dalla risposta del modello alla scheda (slice 104; RIP-H-42 e RIP-H-43 in parte, D-19, D-20,
// D-103, D-104). Con il formato corto dei modelli addestrati (D-103) il Go fa da sé il lavoro che
// in produzione fa il traduttore di pc-fisso (traduttore.py) prima di passare la scheda allo
// standalone: data di oggi, telefono dal dettato, data di consegna dalla frase «c», busta e
// acconto nelle note. Dove decide il traduttore il comportamento è il suo; dove decide lo
// standalone (lettura tollerante della risposta, campi come testo, telefono normalizzato, compro
// oro; nome del cliente, maiuscole, oreficeria e riconoscimento del cliente, slice 105 in nome.go)
// è quello dello standalone.

// Proposta è la proposta di scheda: i campi del modulo della busta nuova (repair[…] di
// internal/web/nuova.go), come testo pronto per il modulo. Il numero della busta non c'è (D-64):
// quello detto va nelle note (D-20). Artigiano, note interne e QR il modello non li compila.
type Proposta struct {
	DataAccettazione string // accepted_on, AAAA-MM-GG: sempre oggi
	NomeCliente      string // customer_name: «n» o l'apertura, maiuscole, solo una persona; corretto se riconosciuto (105)
	TelefonoCliente  string // customer_phone: dal dettato (D-104), normalizzato; senza, del cliente con quel nome (105)
	Oreficeria       string // il nome detto, «o» con le maiuscole (105)
	Descrizione      string // description: «d»
	NotePubbliche    string // public_notes: «Busta N — Acconto N € — x» (D-20)
	Preventivo       string // estimate_euros: «p»
	DataPrevista     string // expected_on, AAAA-MM-GG o vuota: dalla frase «c» (D-18)
	CompraOro        bool   // compro_oro: solo da «oro vecchio» / «scheda compro oro» nel dettato
	OreficeriaID     int64  // oreficeria_id: l'oreficeria nota che risponde al nome detto, 0 nessuna (105)
	NomeCorretto     bool   // il riconoscimento ha cambiato il nome (nome_corretto_da presente, 105)
	NomeCorrettoDa   string // il nome prima della correzione, anche vuoto (105)
}

// Risposta è una risposta del modello nel formato corto, già letta e valida: le chiavi
// n o b d c p a x (nome, oreficeria, busta, descrizione, consegna, preventivo, acconto, altre note).
type Risposta struct {
	n, o, b, d, c, p, a, x valore
}

// ErrRispostaIllegibile: il modello ha dato due risposte non valide di fila (D-19).
var ErrRispostaIllegibile = errors.New("assist: risposta del modello illeggibile")

// tipoValore è il tipo JSON di un campo semplice della risposta.
type tipoValore int

const (
	tipoNullo tipoValore = iota // campo assente o null
	tipoTesto
	tipoIntero
	tipoDecimale
	tipoBooleano
)

// valore è un campo semplice della risposta, con quello che serve a scriverlo come Python (nelle
// note, f-string del traduttore) e come lo standalone (to_string/1 sui campi copiati).
type valore struct {
	tipo  tipoValore
	testo string // il testo, o le cifre di un intero già nella forma di Python
	f     float64
	vero  bool
}

// pieno è la verità di Python: falso il testo vuoto, lo zero, false e null.
func (v valore) pieno() bool {
	switch v.tipo {
	case tipoTesto:
		return v.testo != ""
	case tipoIntero:
		return v.testo != "0"
	case tipoDecimale:
		return v.f != 0
	case tipoBooleano:
		return v.vero
	}
	return false
}

// python è str(v) di Python, come lo scrive l'f-string del traduttore nelle note.
func (v valore) python() string {
	switch v.tipo {
	case tipoDecimale:
		return reprPython(v.f)
	case tipoBooleano:
		if v.vero {
			return "True"
		}
		return "False"
	case tipoNullo:
		return "None"
	}
	return v.testo
}

// standalone è il campo come arriva nel modulo in produzione: il traduttore lo rimette nel JSON
// com'è (json.dumps) e lo standalone lo legge con to_string/1 (null vuoto, booleani minuscoli,
// decimali come Float.to_string/1 di Elixir: «1.0e3», non «1000.0»).
func (v valore) standalone() string {
	switch v.tipo {
	case tipoDecimale:
		return floatElixir(v.f)
	case tipoBooleano:
		return strconv.FormatBool(v.vero)
	case tipoNullo:
		return ""
	}
	return v.testo
}

// cifreDi sono le cifre più corte che tornano allo stesso numero (diverso da zero) e l'esponente
// della prima: 1500.0 è «15» ed e = 3.
func cifreDi(f float64) (segno, cifre string, e int) {
	s := strconv.FormatFloat(f, 'e', -1, 64) // «-1.2345e+02»
	if s[0] == '-' {
		segno, s = "-", s[1:]
	}
	mantissa, esp, _ := strings.Cut(s, "e")
	e, _ = strconv.Atoi(esp)
	return segno, strings.Replace(mantissa, ".", "", 1), e
}

// fisso è la forma senza esponente delle cifre: «1500.0», «0.0001», «50.5».
func fisso(cifre string, e int) string {
	switch {
	case e < 0:
		return "0." + strings.Repeat("0", -e-1) + cifre
	case len(cifre) <= e+1:
		return cifre + strings.Repeat("0", e+1-len(cifre)) + ".0"
	}
	return cifre[:e+1] + "." + cifre[e+1:]
}

// mantissa è «d.ddd» delle cifre, con almeno una cifra dopo il punto in Elixir («1.0»), nessuna in
// Python («1»).
func mantissa(cifre string, zero bool) string {
	if len(cifre) > 1 {
		return cifre[:1] + "." + cifre[1:]
	}
	if zero {
		return cifre + ".0"
	}
	return cifre
}

// reprPython è repr(float) di Python: le cifre più corte che tornano allo stesso numero, con «.0»
// se è intero, in forma esponenziale («1e-05», «1.5e+16») sotto 1e-4 e da 1e16 in su.
func reprPython(f float64) string {
	if f == 0 {
		if math.Signbit(f) {
			return "-0.0"
		}
		return "0.0"
	}
	segno, cifre, e := cifreDi(f)
	if e >= -4 && e < 16 {
		return segno + fisso(cifre, e)
	}
	se := "+"
	if e < 0 {
		se, e = "-", -e
	}
	return segno + mantissa(cifre, false) + "e" + se + fmt.Sprintf("%02d", e)
}

// floatElixir è Float.to_string/1 di Elixir (:erlang.float_to_binary(f, [:short])): le cifre più
// corte che tornano allo stesso numero, nella forma più corta fra quella senza esponente e quella
// con l'esponente («1.0e3», «1.5e-7»); a parità vince quella senza (100.0, 0.0001, 120.0). Da 2^53
// in su sempre con l'esponente («9.007199254740992e15»): lì non tutti gli interi sono un float.
// Misurato con Elixir 1.18/OTP 27 su 40 386 numeri (slice 104).
func floatElixir(f float64) string {
	if f == 0 {
		if math.Signbit(f) {
			return "-0.0"
		}
		return "0.0"
	}
	segno, cifre, e := cifreDi(f)
	decimale := fisso(cifre, e)
	esponente := mantissa(cifre, true) + "e" + strconv.Itoa(e)
	if len(esponente) < len(decimale) || math.Abs(f) >= 1<<53 {
		return segno + esponente
	}
	return segno + decimale
}

// leggiValore legge un campo della risposta; false se non è un valore semplice (oggetto o lista)
// o se è un numero che non sta in un float64.
func leggiValore(grezzo json.RawMessage) (valore, bool) {
	grezzo = bytes.TrimSpace(grezzo)
	if len(grezzo) == 0 {
		return valore{}, true
	}
	switch grezzo[0] {
	case '"':
		var s string
		if err := json.Unmarshal(grezzo, &s); err != nil {
			return valore{}, false
		}
		return valore{tipo: tipoTesto, testo: s}, true
	case 't', 'f':
		return valore{tipo: tipoBooleano, vero: grezzo[0] == 't'}, true
	case 'n':
		return valore{}, true
	case '{', '[':
		return valore{}, false
	}
	lit := string(grezzo)
	if strings.ContainsAny(lit, ".eE") {
		f, err := strconv.ParseFloat(lit, 64)
		if err != nil {
			return valore{}, false
		}
		return valore{tipo: tipoDecimale, f: f}, true
	}
	i, ok := new(big.Int).SetString(lit, 10)
	if !ok {
		return valore{}, false
	}
	return valore{tipo: tipoIntero, testo: i.String()}, true // «-0» diventa «0», come int() di Python
}

// reGraffe è @braces di Assist.decode/1: dalla prima «{» all'ultima «}».
var reGraffe = regexp.MustCompile(`(?s)\{.*\}`)

// LeggiRisposta legge la risposta del modello e dice se è valida (D-19). Come Assist.decode/1
// dello standalone: prima il testo intero senza spazi ai bordi; se non è un oggetto JSON, il pezzo
// dalla prima «{» all'ultima «}» (il blocco ```json di markdown, o del testo prima e dopo). Non è
// valida una risposta senza oggetto, con JSON rotto, o con una delle chiavi n o b d c p a x che non
// è un valore semplice (oggetto o lista); le altre chiavi si ignorano. Non sono valide nemmeno «c»
// e «x» vere che non sono testo: in produzione il traduttore lì si ferma con un errore.
func LeggiRisposta(risposta string) (Risposta, bool) {
	if r, oggetto, ok := decodifica(strings.TrimSpace(risposta)); oggetto {
		return r, ok
	}
	if pezzo := reGraffe.FindString(risposta); pezzo != "" {
		if r, oggetto, ok := decodifica(pezzo); oggetto {
			return r, ok
		}
	}
	return Risposta{}, false
}

// decodifica legge un oggetto JSON: oggetto dice se lo è, ok se è anche una risposta valida.
func decodifica(s string) (r Risposta, oggetto, ok bool) {
	if !strings.HasPrefix(s, "{") {
		return Risposta{}, false, false
	}
	var campi map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &campi); err != nil {
		return Risposta{}, false, false
	}
	for chiave, dove := range map[string]*valore{"n": &r.n, "o": &r.o, "b": &r.b, "d": &r.d,
		"c": &r.c, "p": &r.p, "a": &r.a, "x": &r.x} {
		v, semplice := leggiValore(campi[chiave])
		if !semplice {
			return Risposta{}, true, false
		}
		*dove = v
	}
	for _, v := range []valore{r.c, r.x} {
		if v.pieno() && v.tipo != tipoTesto {
			return Risposta{}, true, false
		}
	}
	return r, true, true
}

// reCompraOro è @compro_oro dello standalone (~r/oro\s+vecchio|scheda\s+compro\s+oro/iu): con
// «u» lo spazio è quello Unicode di PCRE (\p{Z}, \h, \v), scritto per esteso perché \s di Go è
// solo ASCII. Lo standalone lo ricalcola dal dettato sopra quello del traduttore, che usa la
// stessa regola con lo spazio di Python.
var reCompraOro = regexp.MustCompile(`(?i)oro` + spazioPCRE + `+vecchio|scheda` + spazioPCRE + `+compro` + spazioPCRE + `+oro`)

const spazioPCRE = `[\t\n\v\f\r \x{85}\x{a0}\x{1680}\x{180e}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\p{Z}]`

// PropostaDa è la proposta di scheda di una risposta valida per il dettato detto oggi (l'ora del
// negozio, clock.Roma: conta il giorno nel suo fuso), con i clienti e le oreficerie che il negozio
// conosce. È scheda() di traduttore.py seguita da read_answer/3 e shape/3 dello standalone (senza il
// numero della busta, D-64) e dalla coppia nome/telefono del modulo (regoleDelNome, slice 105).
func PropostaDa(r Risposta, dettato string, oggi time.Time, negozio Negozio) Proposta {
	p := Proposta{
		DataAccettazione: oggi.Format("2006-01-02"),
		TelefonoCliente:  telefono.Normalizza(TelefonoDalDettato(dettato)),
		Descrizione:      r.d.standalone(),
		NotePubbliche:    note(r),
		Preventivo:       r.p.standalone(),
		CompraOro:        reCompraOro.MatchString(dettato),
	}
	regoleDelNome(&p, r.n.standalone(), r.o.standalone(), dettato, negozio)
	// La consegna solo dalla frase «c», mai dal dettato o dalla descrizione: una data da incidere
	// («fede da incidere con la data 12 dicembre 2026») non diventa la consegna.
	if r.c.pieno() {
		if d, ok := DataConsegna(r.c.testo, oggi); ok {
			p.DataPrevista = d.Format("2006-01-02")
		}
	}
	return p
}

// note sono le note per il cliente (D-20), come le scrive il traduttore: «Busta N», «Acconto N €»
// e le altre note «x», quelle che ci sono, separate da « — ».
func note(r Risposta) string {
	var n []string
	if r.b.pieno() {
		n = append(n, "Busta "+r.b.python())
	}
	if r.a.pieno() {
		n = append(n, "Acconto "+r.a.python()+" €")
	}
	if r.x.pieno() {
		n = append(n, r.x.testo)
	}
	return strings.Join(n, " — ")
}

var giorniDellaSettimana = []string{"lunedì", "martedì", "mercoledì", "giovedì", "venerdì", "sabato", "domenica"}

// Domanda è il messaggio per un modello addestrato nel formato corto (D-103), come lo scrive il
// traduttore: «Oggi: <giorno> GG/MM/AAAA» e il dettato. Lo manda il collegamento al modello (106).
func Domanda(dettato string, oggi time.Time) string {
	return "Oggi: " + giorniDellaSettimana[settimana(oggi)] + " " + oggi.Format("02/01/2006") + "\nDettato: " + dettato
}

// Estrai chiede la risposta al modello con chiedi e ne fa la proposta di scheda per il negozio. Una risposta non
// valida si chiede una seconda volta, e una sola (D-19; domanda F del piano T4.10, perché con
// temperature 0 la seconda è quasi sempre uguale): due non valide sono ErrRispostaIllegibile. Un
// errore di chiedi esce subito, senza nuovo tentativo; la rete e l'attesa di 90 s in tutto sono
// del collegamento al modello (106).
func Estrai(chiedi func() (string, error), dettato string, oggi time.Time, negozio Negozio) (Proposta, error) {
	for range 2 {
		risposta, err := chiedi()
		if err != nil {
			return Proposta{}, err
		}
		if r, ok := LeggiRisposta(risposta); ok {
			return PropostaDa(r, dettato, oggi, negozio), nil
		}
	}
	return Proposta{}, ErrRispostaIllegibile
}
