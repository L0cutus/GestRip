package assist

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/telefono"
)

// La riga del file di addestramento (slice 117; RIP-H-52, OPS-09), come TrainingExport.export/5 dello standalone a
// go-baseline con Training.current/1 e Training.corrected_fields/2: un dettato raccolto unito ai dati attuali della sua
// busta, scartato con il motivo o scritto come un oggetto JSON su una riga:
//   - oggi, giorno: il giorno del dettato sull'orologio del negozio (le date relative ne hanno bisogno);
//   - testo: il dettato con i pseudonimi (Pseudonimizza);
//   - risposta: la busta com'è adesso (Prompt.fields), con nome e telefono come li ha dettati il cliente, inventati;
//   - proposta: quello che il modello aveva proposto (Prompt.fields), con gli stessi pseudonimi;
//   - modello, campi_corretti, nome_corretto_a_mano, nome_dall_anagrafica: per le misure.
//
// Il numero di busta non c'è mai: non entra nemmeno qui. La proposta si legge con UseNumber (nota 14 di
// docs/domande-aperte.md): nessun numero passa da float64. Puro: niente database né file.

// ScartoBustaNelCestino: la busta del dettato è nel cestino (export/5 la conta e la lascia fuori).
const ScartoBustaNelCestino MotivoScarto = "busta_nel_cestino"

// campiRisposta è Prompt.fields: le chiavi di risposta e proposta, nell'ordine dello standalone.
var campiRisposta = []string{"data", "nome", "telefono", "descrizione", "note", "preventivo", "data_consegna", "oreficeria", "compro_oro"}

// campiContati è @fields di Training: i campi di cui si contano le correzioni, nell'ordine della pagina.
var campiContati = []string{"nome", "telefono", "descrizione", "note", "preventivo", "data_consegna", "oreficeria"}

// giorniDelDettato sono @weekdays dell'esportazione, da lunedì.
var giorniDelDettato = [7]string{"lunedì", "martedì", "mercoledì", "giovedì", "venerdì", "sabato", "domenica"}

// BustaAttuale è la busta del dettato com'è adesso, nei testi della proposta (Training.current/1): chi la legge dal
// database scrive già le date in gg/mm/aaaa, il preventivo come price_input/2 e la consegna come date_source/2. Dati del
// cliente: non si stampa.
type BustaAttuale struct {
	Data, Nome, Telefono, Descrizione, Note, Preventivo, Consegna, Oreficeria string
	// OreficeriaID è l'oreficeria della busta; nil: nessuna.
	OreficeriaID *int64
	CompraOro    bool
	// NelCestino: la busta è nel cestino. ClienteEscluso: il suo cliente è in dettati_esclusioni.
	NelCestino, ClienteEscluso bool
}

// DettatoRaccolto è una riga di dettati_addestramento. Dati del cliente: non si stampa.
type DettatoRaccolto struct {
	ID    int64
	Testo string
	// Proposta è il JSON salvato.
	Proposta string
	// Modello è il modello che ha proposto; nil: non noto.
	Modello  *string
	CreatoIl time.Time
}

// Format nasconde la busta con ogni verbo di fmt.
func (BustaAttuale) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "{BustaAttuale:**redacted**}")
}

// LogValue la nasconde nel registro slog.
func (BustaAttuale) LogValue() slog.Value { return slog.StringValue("**redacted**") }

// Format nasconde il dettato con ogni verbo di fmt.
func (DettatoRaccolto) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "{DettatoRaccolto:**redacted**}")
}

// LogValue lo nasconde nel registro slog.
func (DettatoRaccolto) LogValue() slog.Value { return slog.StringValue("**redacted**") }

// RigaPerAddestramento è export_line/5: la riga del file (un oggetto JSON e l'a capo) o il motivo dello scarto, mai tutti e
// due. altri sono le parole dei nomi degli altri (ParoleDiAltriNomi), k la chiave dell'esportazione. Un caso imprevisto
// (un valore della proposta che non si legge come testo dove serve, un panic) è ScartoErrore, come l'eccezione che lo
// standalone conta come «errore» senza guardarla: il messaggio porterebbe il dettato.
func RigaPerAddestramento(d DettatoRaccolto, b BustaAttuale, altri map[string]bool, k Chiave) (riga []byte, m MotivoScarto) {
	defer func() {
		if recover() != nil {
			riga, m = nil, ScartoErrore
		}
	}()
	// export/5: prima il cliente escluso, poi il cestino, poi i pseudonimi.
	if b.ClienteEscluso {
		return nil, ScartoClienteEscluso
	}
	if b.NelCestino {
		return nil, ScartoBustaNelCestino
	}
	proposta, ok := leggiProposta(d.Proposta)
	if !ok {
		return nil, ScartoErrore
	}
	attuale := b.campi()
	nomi := make([]string, 0, 3)
	for _, v := range []any{attuale["nome"], proposta["nome_corretto_da"], proposta["nome"]} {
		s, ok := testoElixir(v)
		if !ok {
			return nil, ScartoErrore
		}
		nomi = append(nomi, s)
	}
	telefoni := make([]string, 0, 2)
	for _, v := range []any{attuale["telefono"], proposta["telefono"]} {
		s, ok := testoElixir(v)
		if !ok {
			return nil, ScartoErrore
		}
		telefoni = append(telefoni, s)
	}
	out, m := Pseudonimizza(RigaDaEsportare{
		ID: d.ID, Testo: d.Testo, Nomi: nomi, Telefoni: telefoni,
		Risposta: prendi(attuale, campiRisposta), Proposta: prendi(proposta, campiRisposta), Altri: altri,
	}, k)
	if m != "" {
		return nil, m
	}
	corretti, ok := campiCorretti(proposta, attuale, b.OreficeriaID)
	if !ok {
		return nil, ScartoErrore
	}
	giorno := clock.DataDi(d.CreatoIl)
	settimana := (int(time.Date(giorno.Anno, giorno.Mese, giorno.Giorno, 12, 0, 0, 0, time.UTC).Weekday()) + 6) % 7
	var modello any
	if d.Modello != nil {
		modello = *d.Modello
	}
	nomeCorretto, _ := proposta["nome_corretto_da"].(string)
	record := map[string]any{
		"oggi":                 giorno.String(),
		"giorno":               giorniDelDettato[settimana],
		"testo":                out.Testo,
		"risposta":             out.Risposta,
		"proposta":             out.Proposta,
		"modello":              modello,
		"campi_corretti":       corretti,
		"nome_corretto_a_mano": slices.Contains(corretti, "nome"),
		"nome_dall_anagrafica": trimElixir(nomeCorretto) != "",
	}
	// Jason non scrive «<», «>» e «&» come <…: qui nemmeno. Encode aggiunge l'a capo della riga.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(record); err != nil {
		return nil, ScartoErrore
	}
	return buf.Bytes(), ""
}

// campi è Training.current/1.
func (b BustaAttuale) campi() map[string]any {
	compro := 0
	if b.CompraOro {
		compro = 1
	}
	var ore any = nil
	if b.OreficeriaID != nil {
		ore = *b.OreficeriaID
	}
	return map[string]any{
		"data": b.Data, "nome": b.Nome, "telefono": b.Telefono, "descrizione": b.Descrizione, "note": b.Note,
		"preventivo": b.Preventivo, "data_consegna": b.Consegna, "oreficeria": b.Oreficeria, "oreficeria_id": ore,
		"compro_oro": compro,
	}
}

// leggiProposta è la proposta salvata (dettato.proposta || %{}) letta con UseNumber; ok false se non è un oggetto.
func leggiProposta(s string) (map[string]any, bool) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, false
	}
	switch p := v.(type) {
	case nil:
		return map[string]any{}, true
	case map[string]any:
		return p, true
	}
	return nil, false
}

// prendi è Map.take/2: solo le chiavi date che ci sono.
func prendi(m map[string]any, chiavi []string) map[string]any {
	out := make(map[string]any, len(chiavi))
	for _, k := range chiavi {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}

// testoElixir è to_string/1 di un valore letto dal JSON (nil è ""): ok false per una lista o una mappa, dove lo
// standalone solleverebbe.
func testoElixir(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", true
	case string:
		return x, true
	case json.Number:
		return string(x), true
	case bool:
		return strconv.FormatBool(x), true
	case int:
		return strconv.Itoa(x), true
	case int64:
		return strconv.FormatInt(x, 10), true
	}
	return "", false
}

// testoPulito è text/1 di Training: to_string e senza spazi ai bordi.
func testoPulito(v any) (string, bool) {
	s, ok := testoElixir(v)
	return trimElixir(s), ok
}

// campiCorretti è Training.corrected_fields/2: i campi contati in cui la busta di adesso è diversa dalla proposta,
// confrontati come li ha presi il modulo. ok false per un valore che non si legge come testo.
func campiCorretti(proposta, attuale map[string]any, oreficeria *int64) (out []string, ok bool) {
	out = []string{}
	for _, campo := range campiContati {
		p, ok := testoPulito(proposta[campo])
		if !ok {
			return nil, false
		}
		a, _ := testoPulito(attuale[campo])
		var diverso bool
		switch campo {
		case "telefono":
			// Un dettato senza telefono lascia la casella all'anagrafica: quello che mette non è una correzione.
			diverso = p != "" && cifreTelefono(p) != cifreTelefono(a)
		case "data_consegna":
			dp, okp := dataGiornoMeseAnno(p)
			da, oka := dataGiornoMeseAnno(a)
			if okp && oka {
				diverso = !dp.Equal(da)
			} else {
				diverso = p != a
			}
		case "oreficeria":
			id, ok := idOreficeria(proposta["oreficeria_id"])
			if !ok {
				return nil, false
			}
			diverso = !stessoID(id, oreficeria)
		case "nome":
			diverso = confrontabile(p) != confrontabile(a)
		default:
			diverso = p != a
		}
		if diverso {
			out = append(out, campo)
		}
	}
	return out, true
}

// cifreTelefono è phone_digits/1 di Training: le cifre ASCII di Phone.normalize, senza «0039» davanti o senza «39»
// davanti a più di 10 cifre.
func cifreTelefono(s string) string {
	var b strings.Builder
	for _, r := range telefono.Normalizza(s) {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	c := b.String()
	switch {
	case strings.HasPrefix(c, "0039"):
		return c[4:]
	case strings.HasPrefix(c, "39") && len(c) > 10:
		return c[2:]
	}
	return c
}

// dataGiornoMeseAnno è parse_date/1 di Training: gg/mm/aaaa (cifre ASCII) e una data che esiste.
func dataGiornoMeseAnno(s string) (time.Time, bool) {
	if len(s) != 10 || s[2] != '/' || s[5] != '/' {
		return time.Time{}, false
	}
	for i, c := range []byte(s) {
		if i != 2 && i != 5 && (c < '0' || c > '9') {
			return time.Time{}, false
		}
	}
	g, _ := strconv.Atoi(s[0:2])
	m, _ := strconv.Atoi(s[3:5])
	a, _ := strconv.Atoi(s[6:10])
	t := time.Date(a, time.Month(m), g, 0, 0, 0, 0, time.UTC)
	if t.Day() != g || int(t.Month()) != m || t.Year() != a {
		return time.Time{}, false
	}
	return t, true
}

// idOreficeria è oreficeria_id/1 di Training: un intero, o un testo che è tutto un intero (Integer.parse/1, segno
// compreso, dopo text/1); nil altrimenti. ok false per un valore che text/1 non legge.
func idOreficeria(v any) (*int64, bool) {
	s, ok := testoPulito(v)
	if !ok {
		return nil, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, true
	}
	return &n, true
}

func stessoID(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
