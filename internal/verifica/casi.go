// Package verifica è il banco del modello di estrazione (slice 112; RIP-H-54, OPS-08): `gestrip
// verifica-modello` manda ogni dettato inventato al server del modello come lo manda la nuova busta
// (lo stesso client e lo stesso messaggio corto della 106, la stessa lettura della risposta delle
// 104-105-108), confronta la proposta con le risposte attese, campo per campo, e misura i tempi.
//
// Chi decide: lo standalone (`mix assist.bench`, Assist.Bench) per il punteggio (tolleranze, campi,
// mediana/p90/max al rango più vicino, file rifiutato per una riga sbagliata), il Go per la richiesta
// (formato corto D-103, un solo nuovo tentativo in 90 s: domande B e F del piano T4.10).
//
// Riservatezza: il banco non stampa mai il dettato né la risposta del modello, nemmeno in un errore;
// non manda chiavi e non scrive audit né database. Nel resoconto ci sono solo numeri, nomi dei campi,
// identificativi dei casi e motivi fissi («ai:http:500»).
package verifica

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// Caso è un dettato del banco con le risposte accettate, campo per campo. Attesi ha solo i campi su
// cui il caso si conta: un campo che manca non si guarda.
type Caso struct {
	ID        string // l'id del caso, o <file>-<riga> per i banchi che non lo hanno
	File      string // il nome del file, senza cartella
	Categoria string // la categoria (cat dei banchi, prefisso dell'id dei casi dello standalone)
	Oggi      time.Time
	Dettato   string
	Attesi    map[string][]string
	// Standalone dice che il caso è nel formato dello standalone: la busta si cerca come right?("busta"),
	// «busta N» come parola in tutte le note.
	Standalone bool
}

// I campi del punteggio, nell'ordine del resoconto. Quelli dei casi dello standalone sono data,
// nome, telefono, preventivo, data_consegna, oreficeria, compro_oro (più busta e parola quando il caso
// le dice); quelli dei banchi B, C, D, D2 sono le chiavi della risposta corta (n o b d c p a x).
var Campi = []string{"data", "nome", "telefono", "oreficeria", "busta", "descrizione", "parola",
	"preventivo", "acconto", "altre_note", "data_consegna", "compro_oro"}

// campiStandalone sono i campi sempre contati di un caso dello standalone (@scored di Assist.Bench).
var campiStandalone = []string{"data", "nome", "telefono", "preventivo", "data_consegna", "oreficeria", "compro_oro"}

// chiaviBanco sono le chiavi della risposta corta (D-103) e il campo della proposta a cui vanno.
var chiaviBanco = map[string]string{"n": "nome", "o": "oreficeria", "b": "busta", "d": "descrizione",
	"c": "data_consegna", "p": "preventivo", "a": "acconto", "x": "altre_note"}

var giorni = []string{"lunedì", "martedì", "mercoledì", "giovedì", "venerdì", "sabato", "domenica"}

// Carica legge un file di casi (JSON lines), nei due formati che ci sono già:
//
//   - i casi dello standalone (test/fixtures/dettato/casi.jsonl, fuori dal repo: bench/modello/README.md):
//     {"id", "oggi" (AAAA-MM-GG), "testo", "attesi": {campo: [risposte accettate…]}, "busta", "parola"};
//   - i banchi B, C, D, D2 (bench/modello/): {"cat", "oggi" («giorno GG/MM/AAAA»), "dettato",
//     "risposta": {chiave corta: testo}}, con le chiavi vuote omesse (si contano come «vuoto»).
//
// Una riga che non è un caso rifiuta tutto il file con il suo numero, come lo standalone: un banco che
// ne saltasse una conterebbe casi diversi da quelli che dice. L'errore non ripete mai la riga.
func Carica(percorso string) ([]Caso, error) {
	dati, err := os.ReadFile(percorso)
	if err != nil {
		return nil, fmt.Errorf("casi non leggibili: %s", percorso)
	}
	nome := filepath.Base(percorso)
	var casi []Caso
	s := bufio.NewScanner(bytes.NewReader(dati))
	s.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for numero := 1; s.Scan(); numero++ {
		riga := s.Bytes()
		if len(bytes.TrimSpace(riga)) == 0 {
			continue
		}
		c, ok := leggiCaso(riga, nome, numero)
		if !ok {
			return nil, fmt.Errorf("%s, riga %d: caso non valido", nome, numero)
		}
		casi = append(casi, c)
	}
	if s.Err() != nil {
		return nil, fmt.Errorf("%s: riga troppo lunga o file non leggibile", nome)
	}
	if len(casi) == 0 {
		return nil, fmt.Errorf("%s: nessun caso", nome)
	}
	return casi, nil
}

// CaricaTutti legge più file di casi, nell'ordine dato; due casi con lo stesso id lo rifiutano.
func CaricaTutti(percorsi []string) ([]Caso, error) {
	var tutti []Caso
	visti := map[string]bool{}
	for _, p := range percorsi {
		casi, err := Carica(p)
		if err != nil {
			return nil, err
		}
		for _, c := range casi {
			if visti[c.ID] {
				return nil, errors.New("id ripetuto: " + c.ID)
			}
			visti[c.ID] = true
		}
		tutti = append(tutti, casi...)
	}
	return tutti, nil
}

func leggiCaso(riga []byte, file string, numero int) (Caso, bool) {
	var grezzo map[string]json.RawMessage
	if json.Unmarshal(riga, &grezzo) != nil {
		return Caso{}, false
	}
	if _, ok := grezzo["attesi"]; ok {
		return casoStandalone(grezzo, file)
	}
	if _, ok := grezzo["risposta"]; ok {
		return casoBanco(grezzo, file, numero)
	}
	return Caso{}, false
}

// casoStandalone è parse/1 di Assist.Bench: id, oggi e testo sono testi, ogni campo contato ha una
// lista non vuota di testi o interi, busta e parola sono null o un testo non vuoto.
func casoStandalone(g map[string]json.RawMessage, file string) (Caso, bool) {
	var id, oggi, testo string
	if !testo1(g["id"], &id) || id == "" || !testo1(g["oggi"], &oggi) || !testo1(g["testo"], &testo) {
		return Caso{}, false
	}
	giorno, err := time.ParseInLocation("2006-01-02", oggi, clock.Roma)
	if err != nil {
		return Caso{}, false
	}
	var attesi map[string]json.RawMessage
	if json.Unmarshal(g["attesi"], &attesi) != nil || attesi == nil {
		return Caso{}, false
	}
	c := Caso{ID: id, File: file, Categoria: categoriaDellID(id), Oggi: mezzogiorno(giorno), Dettato: testo,
		Attesi: map[string][]string{}, Standalone: true}
	for _, campo := range campiStandalone {
		v, ok := accettati(attesi[campo])
		if !ok {
			return Caso{}, false
		}
		c.Attesi[campo] = v
	}
	for _, campo := range []string{"busta", "parola"} {
		v, ok := facoltativo(g[campo])
		if !ok {
			return Caso{}, false
		}
		if v != "" {
			c.Attesi[campo] = []string{v}
		}
	}
	return c, true
}

// casoBanco è una riga dei banchi B, C, D, D2: la risposta corta attesa, tutte le chiavi contate
// (vuote quando mancano). La data di «oggi» deve avere il suo giorno della settimana: la domanda al
// modello lo ricalcola dalla data, e un caso che dicesse un altro giorno non sarebbe quello del banco.
func casoBanco(g map[string]json.RawMessage, file string, numero int) (Caso, bool) {
	var cat, oggi, dettato string
	if !testo1(g["cat"], &cat) || !testo1(g["oggi"], &oggi) || !testo1(g["dettato"], &dettato) {
		return Caso{}, false
	}
	parti := strings.Fields(oggi)
	if len(parti) != 2 {
		return Caso{}, false
	}
	giorno, err := time.ParseInLocation("02/01/2006", parti[1], clock.Roma)
	if err != nil || giorni[(int(giorno.Weekday())+6)%7] != parti[0] {
		return Caso{}, false
	}
	var risposta map[string]json.RawMessage
	if json.Unmarshal(g["risposta"], &risposta) != nil || risposta == nil {
		return Caso{}, false
	}
	c := Caso{ID: strings.TrimSuffix(file, filepath.Ext(file)) + "-" + strconv.Itoa(numero), File: file,
		Categoria: cat, Oggi: mezzogiorno(giorno), Dettato: dettato, Attesi: map[string][]string{}}
	for _, campo := range chiaviBanco {
		c.Attesi[campo] = []string{""}
	}
	for chiave, grezzo := range risposta {
		campo, ok := chiaviBanco[chiave]
		var v string
		if !ok || !testo1(grezzo, &v) {
			return Caso{}, false
		}
		c.Attesi[campo] = []string{v}
	}
	return c, true
}

// mezzogiorno è il giorno del caso a mezzogiorno, ora del negozio: la domanda e la proposta ne usano
// solo la data.
func mezzogiorno(g time.Time) time.Time {
	return time.Date(g.Year(), g.Month(), g.Day(), 12, 0, 0, 0, clock.Roma)
}

// categoriaDellID è l'id senza il numero finale («prova-07» → «prova»).
func categoriaDellID(id string) string {
	if i := strings.LastIndex(id, "-"); i > 0 {
		return id[:i]
	}
	return id
}

func testo1(grezzo json.RawMessage, s *string) bool {
	return len(grezzo) > 0 && grezzo[0] == '"' && json.Unmarshal(grezzo, s) == nil
}

// accettati è una lista non vuota di testi o interi (accepted?/1), come testi.
func accettati(grezzo json.RawMessage) ([]string, bool) {
	var lista []json.RawMessage
	if json.Unmarshal(grezzo, &lista) != nil || len(lista) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(lista))
	for _, v := range lista {
		var s string
		var n int64
		switch {
		case testo1(v, &s):
			out = append(out, s)
		case json.Unmarshal(v, &n) == nil && !bytes.ContainsAny(v, ".eE"):
			out = append(out, strconv.FormatInt(n, 10))
		default:
			return nil, false
		}
	}
	return out, true
}

// facoltativo è optional?/1: assente o null danno "", un testo non vuoto sé stesso, il resto no.
func facoltativo(grezzo json.RawMessage) (string, bool) {
	if len(grezzo) == 0 || string(grezzo) == "null" {
		return "", true
	}
	var s string
	if !testo1(grezzo, &s) || s == "" {
		return "", false
	}
	return s, true
}

// Campione tiene al più n casi per categoria di ogni file, distribuiti in modo uniforme lungo la
// categoria (gli indici i·len/n), nell'ordine di partenza. n <= 0 li tiene tutti. Serve alla corsa
// vera su CPU, dove tutti i casi prenderebbero mezz'ora.
func Campione(casi []Caso, n int) []Caso {
	if n <= 0 {
		return casi
	}
	gruppi := map[[2]string][]int{}
	for i, c := range casi {
		k := [2]string{c.File, c.Categoria}
		gruppi[k] = append(gruppi[k], i)
	}
	tieni := make([]bool, len(casi))
	for _, indici := range gruppi {
		if len(indici) <= n {
			for _, i := range indici {
				tieni[i] = true
			}
			continue
		}
		for j := range n {
			tieni[indici[j*len(indici)/n]] = true
		}
	}
	var out []Caso
	for i, c := range casi {
		if tieni[i] {
			out = append(out, c)
		}
	}
	return out
}
