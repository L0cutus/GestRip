package web

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/stampe"
)

// Il report multi-scheda A4 (RIP-S-46), come RepairReportController.show/2 dello standalone a
// go-baseline:
//   - GET /repairs/report.pdf, per titolare e dipendente, nella catena delle pagine senza guscio. Il
//     pattern letterale vince su /repairs/{id} nel mux (nel router dello standalone è scritto prima
//     della scheda per lo stesso motivo);
//   - ids si legge come Plug.Conn.Query (idsDellaQuery); un valore che non è un testo è una selezione
//     non valida con inspect/1 del valore; il testo passa da buste.IdsDelReport;
//   - senza ids (o vuoto) lo standalone stampa la selezione della home con i suoi filtri: nel Go la
//     lista della home non c'è ancora (T4.2), e la rotta risponde il 404 della faccia;
//   - i rifiuti come refuse/3: private, no-store, text/plain; charset=utf-8 e il testo; 422 per la
//     selezione, 404 per le buste che non ci sono, 503 «PDF non disponibile» se il PDF o una riga
//     d'audit non riescono (misurati sul banco, tranne il 503: dal codice);
//   - il PDF in linea: «inline; filename="schede_<N>.pdf"», application/pdf, private, no-store.

// report è GET /repairs/report.pdf.
func (f *facciaWeb) report(w http.ResponseWriter, r *http.Request) {
	var ids []int64
	var err error
	v, presente := idsDellaQuery(r.URL.RawQuery)
	testo, eTesto := "", true
	if presente {
		testo, eTesto = v.testo()
	}
	if eTesto {
		ids, err = buste.IdsDelReport(testo)
	} else {
		err = buste.SelezioneNonValida{Valore: v.inspect()}
	}
	var pdf []byte
	var dati buste.DatiReport
	if err == nil {
		pdf, dati, err = f.buste.Report(r.Context(), ids, contestoFoto(r), stampe.Report)
	}
	var sel buste.SelezioneNonValida
	var troppe buste.TroppeSchede
	var mancanti buste.SchedeNonTrovate
	switch {
	case errors.Is(err, buste.ErrSelezioneDaiFiltri):
		NonTrovata(w, r)
	case errors.As(err, &sel), errors.As(err, &troppe):
		rifiutaReport(w, http.StatusUnprocessableEntity, err.Error())
	case errors.As(err, &mancanti):
		rifiutaReport(w, http.StatusNotFound, err.Error())
	case errors.Is(err, buste.ErrPDFNonFatto):
		f.registro.Error("report non fatto", "errore", err.Error())
		rifiutaReport(w, http.StatusServiceUnavailable, msgPDFNonDisponibile)
	case err != nil:
		f.registro.Error("report non letto", "errore", err.Error())
		Errore(w, r)
	default:
		h := w.Header()
		h.Set("Cache-Control", "private, no-store")
		h.Set("Content-Type", "application/pdf")
		h.Set("Content-Disposition", disposizione("inline", buste.NomeFileReport(len(dati.Schede))))
		h.Set("Content-Length", strconv.Itoa(len(pdf)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(pdf) // se il client se n'è andato non c'è niente da fare
	}
}

// rifiutaReport è refuse/3 del controller.
func rifiutaReport(w http.ResponseWriter, codice int, testo string) {
	h := w.Header()
	h.Set("Cache-Control", "private, no-store")
	h.Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(codice)
	_, _ = w.Write([]byte(testo)) // come sopra
}

// valorePlug è un valore dei parametri come lo costruisce Plug.Conn.Query.decode/1: un testo, una
// lista («k[]=») o una mappa («k[x]=»).
type valorePlug struct {
	tipo  int // valoreTesto, valoreLista, valoreMappa
	s     string
	lista []*valorePlug
	mappa map[string]*valorePlug
}

const (
	valoreTesto = iota
	valoreLista
	valoreMappa
)

// testo dà il testo e true, o false se il valore è una lista o una mappa.
func (v *valorePlug) testo() (string, bool) { return v.s, v.tipo == valoreTesto }

// idsDellaQuery è il valore di «ids» nella query grezza come Plug.Conn.Query.decode/1 (misurato
// nell'immagine dello standalone): coppie separate solo da «&», chiave e valore decodificati («+» è
// uno spazio, un «%» illeggibile resta com'è); la chiave è la radice fino alla prima «[» e, se finisce
// con «]», le sottochiavi divise su «][»; altrimenti è un'altra chiave. Vale l'ultima assegnazione, che
// sostituisce un valore di un altro tipo. false se ids non c'è. Una query che non è UTF-8 valido non
// arriva qui: la faccia risponde 400 prima.
func idsDellaQuery(grezza string) (*valorePlug, bool) {
	var v *valorePlug
	for _, coppia := range strings.Split(grezza, "&") {
		if coppia == "" {
			continue
		}
		k, val, _ := strings.Cut(coppia, "=")
		k = decodificaForm(strings.ReplaceAll(k, "+", " "))
		val = decodificaForm(strings.ReplaceAll(val, "+", " "))
		radice, resto, conParentesi := strings.Cut(k, "[")
		var percorso []string
		if conParentesi {
			if !strings.HasSuffix(resto, "]") {
				continue // «ids[», «ids[a]b»: per Plug un'altra chiave
			}
			percorso = strings.Split(strings.TrimSuffix(resto, "]"), "][")
		}
		if radice != "ids" {
			continue
		}
		v = assegnaPlug(v, percorso, val)
	}
	return v, v != nil
}

// assegnaPlug mette val al percorso dentro v (nil = nessun valore) e dà il valore nuovo.
func assegnaPlug(v *valorePlug, percorso []string, val string) *valorePlug {
	switch {
	case len(percorso) == 0:
		return &valorePlug{tipo: valoreTesto, s: val}
	case percorso[0] == "":
		if v == nil || v.tipo != valoreLista {
			v = &valorePlug{tipo: valoreLista}
		}
		// «ids[][a]=1&ids[][b]=2» è una sola mappa nella lista (misurato).
		if n := len(v.lista); len(percorso) > 1 && n > 0 && v.lista[n-1].tipo != valoreTesto {
			v.lista[n-1] = assegnaPlug(v.lista[n-1], percorso[1:], val)
			return v
		}
		v.lista = append(v.lista, assegnaPlug(nil, percorso[1:], val))
		return v
	default:
		if v == nil || v.tipo != valoreMappa {
			v = &valorePlug{tipo: valoreMappa, mappa: map[string]*valorePlug{}}
		}
		v.mappa[percorso[0]] = assegnaPlug(v.mappa[percorso[0]], percorso[1:], val)
		return v
	}
}

// Le opzioni di Inspect.Opts che contano qui: limit e printable_limit.
const (
	limiteInspect           = 50
	limiteStampabileInspect = 4096
)

// inspect è inspect/1 di Elixir sul valore.
func (v *valorePlug) inspect() string {
	var b strings.Builder
	v.scrivi(&b, limiteInspect)
	return b.String()
}

// scrivi scrive il valore con il limite degli elementi dato: un contenitore ne mostra al più limite,
// poi «...», e l'elemento i (da 0) ha limite-1-i (misurato: gli annidati ne mostrano uno in meno per
// ogni elemento prima).
func (v *valorePlug) scrivi(b *strings.Builder, limite int) {
	switch v.tipo {
	case valoreTesto:
		b.WriteString(inspectTesto(v.s, limite))
	case valoreLista:
		b.WriteByte('[')
		for i, e := range v.lista {
			if i > 0 {
				b.WriteString(", ")
			}
			if i >= limite {
				b.WriteString("...")
				break
			}
			e.scrivi(b, limite-1-i)
		}
		b.WriteByte(']')
	case valoreMappa:
		// Le chiavi in ordine, come le mappe piccole di Erlang (fino a 32 chiavi; oltre l'ordine è quello
		// dell'hash e non si copia: una query che nessun modulo produce).
		chiavi := make([]string, 0, len(v.mappa))
		for k := range v.mappa {
			chiavi = append(chiavi, k)
		}
		sort.Strings(chiavi)
		b.WriteString("%{")
		for i, k := range chiavi {
			if i > 0 {
				b.WriteString(", ")
			}
			if i >= limite {
				b.WriteString("...")
				break
			}
			b.WriteString(inspectTesto(k, limiteInspect))
			b.WriteString(" => ")
			v.mappa[k].scrivi(b, limite-1-i)
		}
		b.WriteByte('}')
	}
}

// stampabile è String.printable?/1 per un carattere (misurato su tutti i codici nell'immagine dello
// standalone): non lo sono 0x00–0x06, 0x0E–0x1A, 0x1C–0x1F, 0x80–0x9F, U+FFFE e U+FFFF.
func stampabile(c rune) bool {
	switch {
	case c <= 0x06, c >= 0x0e && c <= 0x1a, c >= 0x1c && c <= 0x1f, c >= 0x80 && c <= 0x9f, c == 0xfffe, c == 0xffff:
		return false
	}
	return true
}

// invisibile dice se inspect/1 scrive il carattere come \uXXXX (misurato).
func invisibile(c rune) bool {
	switch {
	case c == 0xa0, c == 0x34f, c == 0x61c, c >= 0x2000 && c <= 0x200f, c >= 0x2028 && c <= 0x202e,
		c >= 0x205f && c <= 0x2064, c >= 0x2066 && c <= 0x2069, c == 0xfeff, c >= 0xfff9 && c <= 0xfffc:
		return true
	}
	return false
}

var escapeInspect = map[rune]string{'"': `\"`, '\\': `\\`, '\a': `\a`, '\b': `\b`, '\t': `\t`, '\n': `\n`, '\v': `\v`,
	'\f': `\f`, '\r': `\r`, 0x1b: `\e`, 0x7f: `\d`}

// inspectTesto è inspect/1 su un binario UTF-8: fra virgolette con gli escape se è stampabile (al più
// 4096 caratteri, poi «" <> ...»), altrimenti i byte «<<1, 2>>» (al più limite, poi «...»).
func inspectTesto(s string, limite int) string {
	for _, c := range s {
		if !stampabile(c) {
			var b strings.Builder
			b.WriteString("<<")
			for i := 0; i < len(s); i++ {
				if i > 0 {
					b.WriteString(", ")
				}
				if i >= limite {
					b.WriteString("...")
					break
				}
				b.WriteString(strconv.Itoa(int(s[i])))
			}
			b.WriteString(">>")
			return b.String()
		}
	}
	var b strings.Builder
	b.WriteByte('"')
	n := 0
	for i, c := range s {
		if n == limiteStampabileInspect {
			b.WriteString(`" <> ...`)
			return b.String()
		}
		n++
		switch e, ok := escapeInspect[c]; {
		case ok:
			b.WriteString(e)
		case c == '#' && strings.HasPrefix(s[i+utf8.RuneLen(c):], "{"):
			b.WriteString(`\#`)
		case invisibile(c):
			fmt.Fprintf(&b, `\u%04X`, c)
		default:
			b.WriteRune(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}
