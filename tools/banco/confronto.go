package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"image"
	_ "image/jpeg" // dimensioni: le foto JPEG
	_ "image/png"  // dimensioni: le foto salvate come PNG
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	_ "golang.org/x/image/webp" // dimensioni: le miniature

	"github.com/L0cutus/gestrip/internal/pdf/lettura"
)

// dimensioniImmagine è «<larghezza>x<altezza>» dell'immagine nel corpo (dalla sola intestazione), o
// «(non un'immagine)». Per le foto (slice 037): le miniature del Go e di Pillow hanno byte diversi
// per scelta (tema H) e le stesse dimensioni.
func dimensioniImmagine(corpo string) string {
	c, _, err := image.DecodeConfig(strings.NewReader(corpo))
	if err != nil {
		return "(non un'immagine)"
	}
	return strconv.Itoa(c.Width) + "x" + strconv.Itoa(c.Height)
}

// I campi dei PDF (slice 042, D-24: «uguale» = stesso formato della pagina e stesso testo estratto
// normalizzato): il formato delle pagine in mm interi («80x297», una voce per pagina) e il testo di
// tutte le pagine con gli spazi ridotti e le legature sciolte, letti con internal/pdf/lettura, che
// legge i PDF di Chromium dello standalone e quelli del motore Go. La resa grafica non conta. Un
// corpo che il lettore non legge è «(non un PDF)».
//
// Dalla 043 anche il resto di D-24:
//   - «pdf:qr»: il contenuto dei QR, pagina per pagina (« | » fra le pagine), decodificato dai
//     rettangoli che il lettore estrae (lettura.CodiciQR: Chromium disegna l'SVG dello standalone
//     come un rettangolo per modulo, il motore Go uno per tratto). Il QR dello standalone è
//     trasposto e si legge come lo leggono i telefoni, specchiato: conta il testo. «(nessun QR)» per
//     una pagina che non ne ha, «(QR illeggibile)» per uno che ha la forma di un QR e non si
//     decodifica;
//   - «pdf:posizioni»: le posizioni chiave dell'etichetta busta (lettura.PosizioniEtichetta: il
//     riquadro dei moduli del QR, la riga «#numero», «Consegna:» e la casella FATTO), confrontate
//     **entro ±0,5 mm** fra le due app e con le attese (lettura.PosizioniVicine), non al centesimo:
//     D-24 vuole le posizioni chiave dove la misura è una regola, e Chromium arrotonda il layout ai
//     px (la pagina di 76 mm gli viene 75,86, il margine di 3 mm 2,91).
const (
	campoPDFFormato   = "pdf:formato"
	campoPDFTesto     = "pdf:testo"
	campoPDFQR        = "pdf:qr"
	campoPDFPosizioni = "pdf:posizioni"
	nonUnPDF          = "(non un PDF)"
	nessunQR          = "(nessun QR)"
	qrIllegibile      = "(QR illeggibile)"
	// tolleranzaPosizioni è il ±0,5 mm di D-24.
	tolleranzaPosizioni = 0.5
)

func valorePDF(corpo, campo string) string {
	d, err := lettura.Leggi([]byte(corpo))
	if err != nil {
		return nonUnPDF
	}
	switch campo {
	case campoPDFFormato:
		return lettura.Formato(d)
	case campoPDFQR, campoPDFPosizioni:
		parti := make([]string, len(d.Pagine))
		for i, p := range d.Pagine {
			parti[i] = valoreQRPagina(p, campo)
		}
		return strings.Join(parti, " | ")
	}
	return lettura.TestoNormalizzato(d)
}

func valoreQRPagina(p lettura.Pagina, campo string) string {
	if campo == campoPDFPosizioni {
		pp, err := lettura.PosizioniEtichetta(p)
		if err != nil {
			return qrIllegibile
		}
		return lettura.TestoPosizioni(pp)
	}
	qq, err := lettura.CodiciQR(p)
	switch {
	case err != nil:
		return qrIllegibile
	case len(qq) == 0:
		return nessunQR
	}
	testi := make([]string, len(qq))
	for i, q := range qq {
		testi[i] = q.Testo
	}
	return strings.Join(testi, " + ")
}

// uguali dice se due valori del campo sono la stessa cosa: esatti, tranne le posizioni (±0,5 mm).
func uguali(campo, a, b string) bool {
	if campo == campoPDFPosizioni {
		return lettura.PosizioniVicine(a, b, tolleranzaPosizioni)
	}
	return a == b
}

// scenario è una batteria di passi HTTP che gira uguale sullo standalone e sul Go. Solo dati
// inventati (PIANO §4.6).
type scenario struct {
	Nome       string   `json:"nome"`
	Inventario []string `json:"inventario"`
	// ProvaCon sono i dati con cui lo scenario prova davvero le sue voci (slice 040a): con gli altri
	// gira e si confronta, ma esce «non provato» e la copertura non lo conta. Vuoto: tutti i dati.
	ProvaCon []string `json:"prova_con,omitempty"`
	// InFondo: lo scenario cambia dati che altri scenari leggono (la firma consegna una busta e ne
	// azzera un'altra, slice 047), quindi gira dopo tutti gli altri, nell'ordine dei nomi fra quelli in
	// fondo. Gli altri restano nell'ordine dei nomi.
	InFondo bool    `json:"in_fondo,omitempty"`
	Passi   []passo `json:"passi"`
}

// nomiDati sono i nomi dei dati come li scrive runtime.json (datiDi): quelli ammessi in prova_con e
// nell'esito.
var nomiDati = []string{"account-seminati", datiSinteticiImport}

// controllaProvaCon vuole un elenco non vuoto di nomi di dati, senza ripetizioni; nil se il campo
// non c'è.
func controllaProvaCon(file string, l []string) error {
	if l == nil {
		return nil
	}
	if len(l) == 0 {
		return fmt.Errorf("%s: prova_con vuoto (togliere il campo per «tutti i dati»)", file)
	}
	for i, d := range l {
		if !slices.Contains(nomiDati, d) || slices.Contains(l[:i], d) {
			return fmt.Errorf("%s: prova_con %q non valido: %v", file, d, nomiDati)
		}
	}
	return nil
}

// provato dice se uno scenario con prova_con l prova qualcosa con i dati dati ("" = account
// seminati, come in banco su).
func provato(l []string, dati string) bool {
	return len(l) == 0 || slices.Contains(l, datiDi(dati != ""))
}

type passo struct {
	Metodo       string            `json:"metodo"`
	Percorso     string            `json:"percorso"` // arriva al server così com'è (//, /./, %74)
	Intestazioni map[string]string `json:"intestazioni,omitempty"`
	// Corpo è un modulo urlencoded; {csrf} è il token dell'ultima pagina di quell'istanza,
	// {email} e {password} le credenziali del titolare sintetico, {email_dipendente} e
	// {password_dipendente} quelle del dipendente (già codificati per il modulo).
	Corpo string `json:"corpo,omitempty"`
	// CorpoBase64 è un corpo binario (una foto inventata per la coda delle foto, slice 039), in
	// base64 standard, mandato così com'è: nessun Content-Type di suo (lo dicono le intestazioni del
	// passo). Mai insieme a Corpo.
	CorpoBase64 string `json:"corpo_base64,omitempty"`
	// Atteso sono valori che **tutte e due** le istanze devono dare (campo → valore): così un
	// passo non è verde solo perché le due sbagliano allo stesso modo (per esempio un login che
	// fallisce su entrambe).
	Atteso map[string]string `json:"atteso,omitempty"`
	// AttesoDati sono le attese che dipendono dai dati da cui è partito il banco (`banco su
	// -dati`, scritto in runtime.json e passato a confronta con -dati): per ogni modalità, i
	// campi che lì valgono al posto di quelli di Atteso o in più. Atteso resta l'attesa del
	// database con i soli due account seminati; una modalità qui dice esattamente cosa implicano
	// i suoi dati (slice 029), mai «indifferente».
	AttesoDati map[string]map[string]string `json:"atteso_dati,omitempty"`
	Confronta  []string                     `json:"confronta"`
	// SoloGo è una deviazione D-xx approvata: il passo gira solo sul Go, perché lì è un'azione che lo
	// standalone fa senza HTTP (un evento LiveView), e il Go deve dare esattamente le sue attese
	// (niente da confrontare, Confronta vuoto). Una differenza da un'attesa non si copre mai con le
	// deviazioni: è sempre inattesa. Solo un passo SoloGo può usare nel percorso {location} (la
	// Location della risposta di prima del Go) e {href:<id>} (l'href del link con quell'id nella
	// risposta di prima del Go), per seguire un redirect o un link con un segno casuale (slice 037b).
	//
	// Nel corpo di un passo SoloGo {mostra} è il segno della pagina della scheda di prima del Go
	// (il campo nascosto mostra dei moduli, slice 037b): il modulo mandato dalla pagina; vuoto se la
	// pagina non lo ha.
	SoloGo string `json:"solo_go,omitempty"`
	// SoloStandalone è come SoloGo per lo standalone: quello che lì è una richiesta e nel Go è dentro
	// un passo SoloGo (per esempio l'apertura della scheda che nel Go fa un rifiuto senza il segno
	// della pagina). Niente segnaposti.
	SoloStandalone string `json:"solo_standalone,omitempty"`
	// Normalizza è una deviazione D-xx approvata con un normalizzatore (normalizzatori): prima di
	// confrontare (e di guardare le attese), il corpo dello standalone passa dal normalizzatore. Un
	// campo diverso che normalizzato è uguale al Go è una differenza attesa di quella D-xx; un campo
	// uguale a quello grezzo dello standalone ma diverso dal normalizzato è inatteso («D-xx non
	// applicata dal Go»); tutto il resto si confronta come sempre. Solo per un passo che gira sulle due app.
	// Più normalizzatori si scrivono uniti da «+» («D-64+D-71») e si applicano in quell'ordine. Per i
	// campi del PDF (pdf:testo…) il normalizzatore passa sul valore letto dal PDF, non sul corpo.
	Normalizza string `json:"normalizza,omitempty"`
}

// normalizzatori sono le deviazioni che si dichiarano con una regola invece che con le righe di
// bench/deviazioni.json, perché toccano ogni busta di una pagina.
var normalizzatori = map[string]func(string) string{"D-64": normalizzaD64, "D-71": normalizzaD71, "D-72": normalizzaD72}

// idNormalizzatori sono le D-xx di un "normalizza" («D-64+D-71»).
func idNormalizzatori(nome string) []string {
	if nome == "" {
		return nil
	}
	return strings.Split(nome, "+")
}

// normalizzatore è la composizione dei normalizzatori di nome, nell'ordine; nil se nome è vuoto o una
// delle D-xx non ha un normalizzatore.
func normalizzatore(nome string) func(string) string {
	var fs []func(string) string
	for _, id := range idNormalizzatori(nome) {
		f := normalizzatori[id]
		if f == nil {
			return nil
		}
		fs = append(fs, f)
	}
	if len(fs) == 0 {
		return nil
	}
	return func(s string) string {
		for _, f := range fs {
			s = f(s)
		}
		return s
	}
}

// Le etichette della D-71 nello standalone: la riga «Data» della scheda (<dt>), l'etichetta «Data»
// del campo repair[accepted_on] nella modifica e «Data accettazione» in «Nuova riparazione»; nel
// testo dei PDF «DATA ACCETTAZIONE» (scheda PDF) e «DATA RIPARAZIONE» (report). Dalla slice 056
// (Stefano, 07/11/2026) anche la home: la scelta «Accettazione» del periodo nel cassetto
// (data_campo) e la pillola «📅 Accettazione: …».
var (
	reD71Riga    = regexp.MustCompile(`(<dt[^>]*>)Data(</dt>)`)
	reD71Campo   = regexp.MustCompile(`(<label[^>]*\bfor="repair_accepted_on"[^>]*>\s*<span[^>]*>)Data(?: accettazione)?(</span>)`)
	reD71Periodo = regexp.MustCompile(`(<option value="accettazione"[^>]*>\s*)Accettazione(\s*</option>)`)
	sostD71      = strings.NewReplacer("Data accettazione", "Data ricezione", "DATA ACCETTAZIONE", "DATA RICEZIONE",
		"DATA RIPARAZIONE", "DATA RICEZIONE", "📅 Accettazione: ", "📅 Ricezione: ")
)

// normalizzaD71 è la D-71 (Stefano, 06/11/2026, e 07/11/2026 per la home): la data di accettazione
// si chiama «Data ricezione» dappertutto la veda l'utente (scheda, modifica, «Nuova riparazione»,
// scheda PDF, report), e «Ricezione» nel filtro del periodo e nella sua pillola.
func normalizzaD71(s string) string {
	s = reD71Riga.ReplaceAllString(s, "${1}Data ricezione${2}")
	s = reD71Campo.ReplaceAllString(s, "${1}Data ricezione${2}")
	s = reD71Periodo.ReplaceAllString(s, "${1}Ricezione${2}")
	return sostD71.Replace(s)
}

// La data di ricezione e la riga «Registrata il» della scheda dello standalone (prima o dopo la D-71).
var (
	reD72Data       = regexp.MustCompile(`<dt[^>]*>Data(?: ricezione)?</dt>\s*<dd>\s*([0-9]{2}/[0-9]{2}/[0-9]{4})\s*</dd>`)
	reD72Registrata = regexp.MustCompile(`<div>\s*<dt[^>]*>Registrata il</dt>\s*<dd>\s*([0-9]{2}/[0-9]{2}/[0-9]{4}) [0-9]{2}:[0-9]{2}\s*</dd>\s*</div>\s*`)
)

// normalizzaD72 è la D-72 (Stefano, 06/11/2026): nella scheda «Registrata il» si vede solo quando il
// suo giorno è diverso dalla data di ricezione. Toglie dalla scheda dello standalone la riga
// «Registrata il» dello stesso giorno della data (lo standalone scrive l'ora del negozio, come il Go);
// una pagina senza la data, o con un altro giorno, resta com'è.
func normalizzaD72(s string) string {
	m := reD72Data.FindStringSubmatch(s)
	if m == nil {
		return s
	}
	return reD72Registrata.ReplaceAllStringFunc(s, func(r string) string {
		if reD72Registrata.FindStringSubmatch(r)[1] == m[1] {
			return ""
		}
		return r
	})
}

// reNumeroStandalone è il numero di busta come lo scrive la lista dello standalone: «#21/26» nel
// testo, «Apri busta 21/26» nell'aria-label dei link (interpolato, senza «#»); dalla slice 056 anche
// la conferma di «✓ Consegnata» («Segnare la busta 32/26 come consegnata?») e l'aria-label del menu
// dello stato («Stato 22/26»), interpolati allo stesso modo; dalla slice 057 il testo alternativo
// delle foto della galleria («Foto 21/26»).
var reNumeroStandalone = regexp.MustCompile(`(#|Apri busta |Segnare la busta |Stato |Foto )([0-9]{1,5}/[0-9]{2})\b`)

// normalizzaD64 è la D-64 (il numero visibile è quello dell'etichetta QR, slice 050): nel testo dello
// standalone (il corpo della risposta) ogni «#N/AA» (e «Apri busta N/AA», «Segnare la busta N/AA»,
// «Stato N/AA») di una busta dei dati del banco diventa il numero che il Go deve mostrare,
// scritto a mano in numeriVisibiliBanco (home.go). Un numero che la tabella non ha resta com'è, e la
// differenza resta inattesa.
func normalizzaD64(s string) string {
	return reNumeroStandalone.ReplaceAllStringFunc(s, func(m string) string {
		p := reNumeroStandalone.FindStringSubmatch(m)
		v, ok := numeriVisibiliBanco[p[2]]
		switch {
		case !ok:
			return m
		case p[1] == "#":
			return v
		}
		return p[1] + strings.TrimPrefix(v, "#")
	})
}

// reSegnoPagina è il segno della scheda nei moduli della pagina del Go.
var reSegnoPagina = regexp.MustCompile(`<input name="mostra" type="hidden" value="([A-Za-z0-9_-]+)">`)

// reSegnaposto sono i segnaposto del percorso di un passo SoloGo.
var reSegnaposto = regexp.MustCompile(`\{(location|href:[A-Za-z][A-Za-z0-9_-]*)\}`)

// percorsoDopo dà il percorso del passo con i segnaposto presi dalla risposta prima: errore se uno
// non risponde, o se non dà un percorso relativo sull'istanza (/…, non //…).
func percorsoDopo(percorso string, prima risposta) (string, error) {
	var errore error
	out := reSegnaposto.ReplaceAllStringFunc(percorso, func(s string) string {
		nome := s[1 : len(s)-1]
		var v string
		if id, ok := strings.CutPrefix(nome, "href:"); ok {
			v = valoreAttributo(prima.Corpo, id, "href")
		} else {
			v = strings.Join(http.Header(prima.Intestazioni).Values("Location"), ", ")
		}
		if !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") {
			errore = fmt.Errorf("segnaposto %s senza un percorso relativo: %q", s, v)
		}
		return v
	})
	return out, errore
}

// attesePer dà le attese del passo con i dati della modalità dati ("" = account seminati),
// senza toccare il passo.
func attesePer(p passo, dati string) map[string]string {
	out := make(map[string]string, len(p.Atteso)+len(p.AttesoDati[dati]))
	for c, v := range p.Atteso {
		out[c] = v
	}
	if dati != "" {
		for c, v := range p.AttesoDati[dati] {
			out[c] = v
		}
	}
	return out
}

// intestazioniIgnorate sono quelle che il client Go ignora o riscrive in una richiesta.
var intestazioniIgnorate = []string{"Host", "Content-Length", "Connection", "Transfer-Encoding"}

var (
	reVoce = regexp.MustCompile(`^[A-Z]+(?:-[A-Z])?-\d+$`)
	reDev  = regexp.MustCompile(`^D-\d{2,}$`)
)

func campoValido(c string) bool {
	if c == "codice" || c == "corpo" || c == "impronta" || c == "lunghezza" || c == "dimensioni" || c == "forma-json" ||
		c == campoPDFFormato || c == campoPDFTesto || c == campoPDFQR || c == campoPDFPosizioni {
		return true
	}
	if f, ok := strings.CutPrefix(c, "testo:"); ok {
		return fraseValida(f)
	}
	if f, ok := strings.CutPrefix(c, "conteggio:"); ok {
		return fraseValida(f)
	}
	if loc, ok := strings.CutPrefix(c, "visibile:"); ok {
		return reLocalizzatore.MatchString(loc)
	}
	if loc, ok := strings.CutPrefix(c, "grezzo:"); ok {
		return reLocalizzatore.MatchString(loc)
	}
	if resto, ok := strings.CutPrefix(c, "attributo:"); ok {
		loc, nome, ok := tagliaAttributo(resto)
		return ok && reLocalizzatore.MatchString(loc) && reNomeAttributo.MatchString(nome)
	}
	n, ok := strings.CutPrefix(c, "intestazione:")
	if !ok {
		n, ok = strings.CutPrefix(c, "presenza:")
	}
	return ok && n != "" && n == strings.ToLower(n)
}

// reAttributo è un attributo HTML intero (href="/settings"): le uniche virgolette ammesse in una
// frase, perché testo: e conteggio: cercano nel corpo grezzo e gli scenari dei gusci controllano
// così il markup.
var reAttributo = regexp.MustCompile(`^[a-z][a-z-]*="[^"]*"$`)

// fraseValida rifiuta le frasi vuote e quelle con un carattere che l'HTML scappa nel testo
// (' & < > e le virgolette fuori da un attributo intero): nel corpo non comparirebbero mai, e
// un «assente» o uno «0» passerebbero sempre su tutte e due le parti.
func fraseValida(f string) bool {
	if f == "" || strings.ContainsAny(f, "'&<>") {
		return false
	}
	return !strings.Contains(f, `"`) || reAttributo.MatchString(f)
}

func leggiScenario(file string, r io.Reader) (scenario, error) {
	var s scenario
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return s, fmt.Errorf("%s: %w", file, err)
	}
	if s.Nome == "" || len(s.Passi) == 0 {
		return s, fmt.Errorf("%s: servono nome e almeno un passo", file)
	}
	for _, id := range s.Inventario {
		if !reVoce.MatchString(id) {
			return s, fmt.Errorf("%s: voce d'inventario malformata %q", file, id)
		}
	}
	if err := controllaProvaCon(file, s.ProvaCon); err != nil {
		return s, err
	}
	for i, p := range s.Passi {
		// Un passo solo_go può cominciare con un segnaposto, che dà sempre un percorso relativo.
		if p.Metodo == "" || !strings.HasPrefix(p.Percorso, "/") && (p.SoloGo == "" || !strings.HasPrefix(p.Percorso, "{")) {
			return s, fmt.Errorf("%s: passo %d: metodo e percorso relativo (/…) obbligatori", file, i)
		}
		if p.CorpoBase64 != "" {
			if p.Corpo != "" {
				return s, fmt.Errorf("%s: passo %d: corpo e corpo_base64 insieme", file, i)
			}
			if _, err := base64.StdEncoding.Strict().DecodeString(p.CorpoBase64); err != nil {
				return s, fmt.Errorf("%s: passo %d: corpo_base64 non valido: %w", file, i, err)
			}
		}
		for k := range p.Intestazioni {
			if slices.Contains(intestazioniIgnorate, http.CanonicalHeaderKey(k)) {
				return s, fmt.Errorf("%s: passo %d: l'intestazione %s la decide il client Go, non lo scenario", file, i, k)
			}
		}
		switch {
		case p.SoloGo != "" && p.SoloStandalone != "":
			return s, fmt.Errorf("%s: passo %d: solo_go e solo_standalone insieme", file, i)
		case p.Normalizza != "" && (p.SoloGo != "" || p.SoloStandalone != ""):
			return s, fmt.Errorf("%s: passo %d: normalizza solo in un passo che gira sulle due app", file, i)
		case p.SoloStandalone != "":
			if !reDev.MatchString(p.SoloStandalone) || len(p.Confronta) > 0 || len(p.Atteso) == 0 ||
				reSegnaposto.MatchString(p.Percorso) || strings.Contains(p.Corpo, "{mostra}") {
				return s, fmt.Errorf("%s: passo %d: solo_standalone vuole una D-xx, attese, niente confronta né segnaposti", file, i)
			}
		case p.SoloGo != "":
			if !reDev.MatchString(p.SoloGo) {
				return s, fmt.Errorf("%s: passo %d: solo_go vuole una deviazione D-xx, non %q", file, i, p.SoloGo)
			}
			if len(p.Confronta) > 0 {
				return s, fmt.Errorf("%s: passo %d: un passo solo_go non confronta, ha solo attese", file, i)
			}
			if len(p.Atteso) == 0 {
				return s, fmt.Errorf("%s: passo %d: un passo solo_go senza attese", file, i)
			}
		case len(p.Confronta) == 0:
			return s, fmt.Errorf("%s: passo %d: niente da confrontare", file, i)
		case p.Normalizza != "" && normalizzatore(p.Normalizza) == nil:
			return s, fmt.Errorf("%s: passo %d: nessun normalizzatore per %q", file, i, p.Normalizza)
		case reSegnaposto.MatchString(p.Percorso), strings.Contains(p.Corpo, "{mostra}"):
			return s, fmt.Errorf("%s: passo %d: {location} e {href:…} solo in un passo solo_go", file, i)
		}
		for _, c := range p.Confronta {
			if !campoValido(c) {
				return s, fmt.Errorf("%s: passo %d: campo %q non valido", file, i, c)
			}
		}
		if err := attesaValida(p.Atteso); err != nil {
			return s, fmt.Errorf("%s: passo %d: %w", file, i, err)
		}
		for dati, att := range p.AttesoDati {
			// La modalità di base è Atteso: qui solo le modalità con dati diversi.
			if con, err := modalitaDati(dati); err != nil || !con {
				return s, fmt.Errorf("%s: passo %d: atteso_dati per la modalità %q: vale solo %s", file, i, dati, datiSinteticiImport)
			}
			if len(att) == 0 {
				return s, fmt.Errorf("%s: passo %d: atteso_dati %s senza campi", file, i, dati)
			}
			if err := attesaValida(att); err != nil {
				return s, fmt.Errorf("%s: passo %d: atteso_dati %s: %w", file, i, dati, err)
			}
		}
	}
	return s, nil
}

// attesaValida vuole campi validi e, per un conteggio, un numero intero non negativo.
func attesaValida(att map[string]string) error {
	for c, v := range att {
		if !campoValido(c) {
			return fmt.Errorf("campo atteso %q non valido", c)
		}
		if strings.HasPrefix(c, "conteggio:") {
			if n, err := strconv.Atoi(v); err != nil || n < 0 || strconv.Itoa(n) != v {
				return fmt.Errorf("conteggio atteso %q non è un numero: %q", c, v)
			}
		}
	}
	return nil
}

// risposta è ciò che il banco ha visto da un'istanza per un passo.
type risposta struct {
	Codice       int
	Corpo        string
	Intestazioni map[string][]string
}

func (r risposta) valore(campo string) string {
	switch campo {
	case "codice":
		return fmt.Sprint(r.Codice)
	case "corpo":
		return r.Corpo
	case "impronta":
		h := sha256.Sum256([]byte(r.Corpo))
		return hex.EncodeToString(h[:])
	case "lunghezza":
		return strconv.Itoa(len(r.Corpo))
	case "dimensioni":
		return dimensioniImmagine(r.Corpo)
	case "forma-json":
		return formaJSON(r.Corpo)
	case campoPDFFormato, campoPDFTesto, campoPDFQR, campoPDFPosizioni:
		return valorePDF(r.Corpo, campo)
	}
	if frase, ok := strings.CutPrefix(campo, "testo:"); ok {
		if strings.Contains(r.Corpo, frase) {
			return "presente"
		}
		return "assente"
	}
	// conteggio: quante volte la frase compare nel corpo, per le attese esatte sui dati.
	if frase, ok := strings.CutPrefix(campo, "conteggio:"); ok {
		return strconv.Itoa(strings.Count(r.Corpo, frase))
	}
	if loc, ok := strings.CutPrefix(campo, "visibile:"); ok {
		return testoVisibile(r.Corpo, loc)
	}
	if loc, ok := strings.CutPrefix(campo, "grezzo:"); ok {
		return testoGrezzo(r.Corpo, loc)
	}
	if resto, ok := strings.CutPrefix(campo, "attributo:"); ok {
		loc, nome, _ := tagliaAttributo(resto)
		return valoreAttributo(r.Corpo, loc, nome)
	}
	if nome, ok := strings.CutPrefix(campo, "presenza:"); ok {
		if len(http.Header(r.Intestazioni).Values(nome)) > 0 {
			return "presente"
		}
		return "assente"
	}
	nome := strings.TrimPrefix(campo, "intestazione:")
	return strings.Join(http.Header(r.Intestazioni).Values(nome), ", ")
}

// Il testo e gli attributi di un elemento della pagina (slice 032), per confrontare due pagine con
// markup diverso (Phoenix e html/template). Non è un parser HTML: gli elementi si trovano con
// espressioni regolari sul corpo, e la chiusura contando solo i tag con lo stesso nome.
//
// Il localizzatore è un id («repair-status») oppure «.classe@n», l'n-esimo elemento (da 1) che ha
// quella classe fra le sue; dopo, facoltativo, «/tag» o «/tag@n»: il primo o l'n-esimo tag con
// quel nome dentro l'elemento. Id e classi si leggono solo scritti con le virgolette doppie, come
// li scrivono le due app.
var (
	reLocalizzatore = regexp.MustCompile(`^(?:[A-Za-z][A-Za-z0-9_-]*|\.[A-Za-z0-9_-]+@[1-9][0-9]*)(?:/[a-z][a-z0-9]*(?:@[1-9][0-9]*)?)?$`)
	reNomeAttributo = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	reApertura      = regexp.MustCompile(`<([A-Za-z][A-Za-z0-9-]*)\b[^>]*>`)
	reClasse        = regexp.MustCompile(`\sclass="([^"]*)"`)
	reCommentoHTML  = regexp.MustCompile(`(?s)<!--.*?-->`)
	reTagHTML       = regexp.MustCompile(`</?([A-Za-z][A-Za-z0-9-]*)?[^>]*>`)
	// tagVuoti sono gli elementi senza contenuto né chiusura.
	tagVuoti = map[string]bool{"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
		"input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true}
	// tagInLinea sono gli elementi che non vanno a capo: togliendoli non si aggiunge spazio.
	tagInLinea = map[string]bool{"a": true, "abbr": true, "b": true, "bdi": true, "bdo": true, "button": true, "cite": true,
		"code": true, "data": true, "dfn": true, "em": true, "i": true, "img": true, "input": true, "kbd": true, "label": true,
		"mark": true, "q": true, "s": true, "samp": true, "small": true, "span": true, "strong": true, "sub": true, "sup": true,
		"time": true, "u": true, "var": true, "wbr": true}
)

const (
	// assenteVisibile: nessun elemento risponde al localizzatore.
	assenteVisibile = "(assente)"
	// senzaAttributo: l'elemento c'è, l'attributo no.
	senzaAttributo = "(senza attributo)"
)

// tagliaAttributo divide «localizzatore:nome» sull'ultimo «:».
func tagliaAttributo(s string) (loc, nome string, ok bool) {
	i := strings.LastIndex(s, ":")
	if i <= 0 || i == len(s)-1 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

// elemento trova l'elemento del localizzatore: il tag di apertura e il contenuto fino al tag che lo
// chiude (tutto il resto del corpo se non si chiude; vuoto per un elemento vuoto).
func elemento(corpo, loc string) (apertura, dentro string, ok bool) {
	base, sotto, _ := strings.Cut(loc, "/")
	var m []int
	if classe, n, conClasse := strings.Cut(strings.TrimPrefix(base, "."), "@"); strings.HasPrefix(base, ".") && conClasse {
		m = ennesimo(corpo, n, func(tag, apertura string) bool {
			c := reClasse.FindStringSubmatch(apertura)
			return c != nil && slices.Contains(strings.Fields(c[1]), classe)
		})
	} else {
		m = regexp.MustCompile(`<([A-Za-z][A-Za-z0-9-]*)\b[^>]*\sid="` + regexp.QuoteMeta(base) + `"[^>]*>`).FindStringSubmatchIndex(corpo)
	}
	if m == nil {
		return "", "", false
	}
	apertura, dentro = corpo[m[0]:m[1]], contenuto(corpo[m[1]:], strings.ToLower(corpo[m[2]:m[3]]))
	if sotto == "" {
		return apertura, dentro, true
	}
	nome, n, conN := strings.Cut(sotto, "@")
	if !conN {
		n = "1"
	}
	m = ennesimo(dentro, n, func(tag, _ string) bool { return tag == nome })
	if m == nil {
		return "", "", false
	}
	return dentro[m[0]:m[1]], contenuto(dentro[m[1]:], nome), true
}

// ennesimo è l'n-esimo tag di apertura che piace a scegli: gli indici del tag e del nome.
func ennesimo(corpo, n string, scegli func(tag, apertura string) bool) []int {
	k, err := strconv.Atoi(n)
	if err != nil {
		return nil
	}
	for _, m := range reApertura.FindAllStringSubmatchIndex(corpo, -1) {
		if scegli(strings.ToLower(corpo[m[2]:m[3]]), corpo[m[0]:m[1]]) {
			if k--; k == 0 {
				return m
			}
		}
	}
	return nil
}

// contenuto è resto fino al tag che chiude tag, contando quelli con lo stesso nome aperti dentro.
func contenuto(resto, tag string) string {
	if tagVuoti[tag] {
		return ""
	}
	livello := 1
	for _, t := range regexp.MustCompile(`(?i)<(/?)`+regexp.QuoteMeta(tag)+`\b[^>]*>`).FindAllStringSubmatchIndex(resto, -1) {
		switch {
		case t[3] > t[2]: // chiusura
			livello--
		case !strings.HasSuffix(resto[t[0]:t[1]], "/>"):
			livello++
		}
		if livello == 0 {
			return resto[:t[0]]
		}
	}
	return resto
}

// testoVisibile è il testo che si legge: commenti tolti, tag tolti (quelli di blocco e <br>
// valgono uno spazio, quelli in linea niente), entità decodificate, spazi compattati. Non sa
// niente di CSS: un elemento in linea reso a blocco dal foglio di stile conta come in linea.
func testoVisibile(corpo, loc string) string {
	_, dentro, ok := elemento(corpo, loc)
	if !ok {
		return assenteVisibile
	}
	dentro = reCommentoHTML.ReplaceAllString(dentro, "")
	dentro = reTagHTML.ReplaceAllStringFunc(dentro, func(t string) string {
		if m := reTagHTML.FindStringSubmatch(t); m != nil && tagInLinea[strings.ToLower(m[1])] {
			return ""
		}
		return " "
	})
	return strings.Join(strings.Fields(html.UnescapeString(dentro)), " ")
}

// testoGrezzo è il testo con a capo e spazi intatti: commenti e tag tolti senza spazio, entità
// decodificate. Per i riquadri pre-wrap, dove una riga vuota in più si vede.
func testoGrezzo(corpo, loc string) string {
	_, dentro, ok := elemento(corpo, loc)
	if !ok {
		return assenteVisibile
	}
	return html.UnescapeString(reTagHTML.ReplaceAllString(reCommentoHTML.ReplaceAllString(dentro, ""), ""))
}

// valoreAttributo è il valore dell'attributo nome nel tag di apertura, con le entità decodificate;
// «» per un attributo senza valore. Solo valori fra virgolette doppie.
func valoreAttributo(corpo, loc, nome string) string {
	apertura, _, ok := elemento(corpo, loc)
	if !ok {
		return assenteVisibile
	}
	m := regexp.MustCompile(`\s` + regexp.QuoteMeta(nome) + `(?:="([^"]*)"|[\s/>])`).FindStringSubmatch(apertura)
	if m == nil {
		return senzaAttributo
	}
	return html.UnescapeString(m[1])
}

// nonJSON è forma-json di un corpo che non è un solo valore JSON.
const nonJSON = "(non JSON)"

// formaJSON è il corpo JSON com'è scritto, con le chiavi nel loro ordine, senza spazi e con ogni
// numero al posto di «#»: per confrontare una risposta che porta un id nuovo, diverso da un'istanza
// all'altra (photo_id della coda delle foto, slice 039), senza perdere la forma (chiavi, ordine,
// tipi, testi). nonJSON se il corpo non è esattamente un valore JSON.
func formaJSON(corpo string) string {
	dec := json.NewDecoder(strings.NewReader(corpo))
	dec.UseNumber()
	type livello struct{ oggetto, primo, chiave bool }
	var pila []*livello
	var b strings.Builder
	finito := false
	scrivi := func(v string) {
		if n := len(pila); n > 0 {
			l := pila[n-1]
			switch {
			case l.oggetto && !l.chiave:
				b.WriteByte(':')
			case !l.primo:
				b.WriteByte(',')
			}
			l.primo = false
			if l.oggetto {
				l.chiave = !l.chiave
			}
		}
		b.WriteString(v)
	}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || finito {
			return nonJSON
		}
		switch v := tok.(type) {
		case json.Delim:
			if v == '{' || v == '[' {
				scrivi(string(v))
				pila = append(pila, &livello{oggetto: v == '{', primo: true, chiave: true})
				continue
			}
			pila = pila[:len(pila)-1]
			b.WriteString(string(v))
		case json.Number:
			scrivi("#")
		case string:
			q, _ := json.Marshal(v) // una stringa: Marshal non fallisce
			scrivi(string(q))
		case bool:
			scrivi(strconv.FormatBool(v))
		default: // nil
			scrivi("null")
		}
		finito = len(pila) == 0
	}
	if !finito {
		return nonJSON
	}
	return b.String()
}

type differenza struct {
	Scenario       string
	Passo          int
	Campo          string
	Standalone, Go string
	// Normalizzata è la D-xx del normalizzatore del passo quando il valore dello standalone,
	// normalizzato, è quello del Go: una differenza attesa.
	Normalizzata string
}

func (d differenza) String() string {
	return fmt.Sprintf("%s[%d] %s: standalone %q, go %q", d.Scenario, d.Passo, d.Campo, d.Standalone, d.Go)
}

// confronta restituisce i campi del passo che differiscono fra standalone (a) e Go (b), e quelli
// in cui una delle due non dà l'atteso del database con i soli account seminati.
func confronta(p passo, a, b risposta) []differenza {
	return confrontaCon(p, "", a, b)
}

// confrontaCon è confronta con le attese dei dati della modalità dati (attesePer).
func confrontaCon(p passo, dati string, a, b risposta) []differenza {
	var out []differenza
	// an è la risposta dello standalone con il corpo normalizzato (normalizza del passo): le attese la
	// guardano così, e un campo diverso che lì è uguale al Go è una differenza della D-xx.
	an := a
	norm := normalizzatore(p.Normalizza)
	if norm != nil {
		an.Corpo = norm(a.Corpo)
	}
	// normalizzato è il valore dello standalone normalizzato: per un campo del PDF il normalizzatore
	// passa sul valore letto (il corpo è il PDF), per gli altri sul corpo (an).
	normalizzato := func(c string) string {
		if norm != nil && strings.HasPrefix(c, "pdf:") {
			return norm(a.valore(c))
		}
		return an.valore(c)
	}
	for c, v := range attesePer(p, dati) {
		if va, vb := normalizzato(c), b.valore(c); !uguali(c, va, v) || !uguali(c, vb, v) {
			out = append(out, differenza{Campo: "atteso " + c + "=" + v, Standalone: va, Go: vb})
		}
	}
	for _, c := range p.Confronta {
		va, vb := a.valore(c), b.valore(c)
		vn := normalizzato(c)
		switch {
		case p.Normalizza == "" && uguali(c, va, vb), p.Normalizza != "" && uguali(c, va, vb) && uguali(c, vn, vb):
		case p.Normalizza != "" && uguali(c, va, vb):
			// Il Go dà il valore grezzo dello standalone dove avrebbe dovuto applicare la deviazione.
			out = append(out, differenza{Campo: c + " (" + p.Normalizza + " non applicata dal Go)", Standalone: vn, Go: vb})
		case p.Normalizza != "" && uguali(c, vn, vb):
			out = append(out, differenza{Campo: c, Standalone: va, Go: vb, Normalizzata: p.Normalizza})
		case p.Normalizza != "":
			// Diversa anche normalizzata: per le righe di deviazioni.json conta lo standalone
			// normalizzato (per esempio la D-01 del report, che toglie le foto, sopra la D-71).
			out = append(out, differenza{Campo: c, Standalone: vn, Go: vb})
		default:
			out = append(out, differenza{Campo: c, Standalone: va, Go: vb})
		}
	}
	return out
}

// deviazione è una differenza attesa, legata a una riga D-xx approvata dell'inventario.
type deviazione struct {
	ID string `json:"id"`
	// Livello è "" per il livello HTTP, "browser" per il livello browser (browser.go): lì il campo è
	// uno del browser e il passo è la vista (0 desktop, 1 telefono).
	Livello    string `json:"livello,omitempty"`
	Scenario   string `json:"scenario"`
	Passo      int    `json:"passo"`
	Campo      string `json:"campo"`
	Standalone string `json:"standalone"`
	Go         string `json:"go"`
	// Togli (slice 044, solo per pdf:testo del livello HTTP, con standalone e go vuoti) è il testo che lo
	// standalone ha in più e il Go no, per una D-xx che toglie una parte di un documento (D-01: la
	// sezione delle foto del report); Copie è quante volte la frase c'è nel testo dello standalone (una
	// per scheda del passo). La differenza è coperta se la frase c'è esattamente Copie volte e, tolte
	// quelle copie e normalizzati gli spazi, il testo è esattamente quello del Go: una nota della busta
	// uguale alla frase, che il Go non stampasse, sarebbe una copia in più e la differenza resta
	// inattesa. Così il resto del testo si confronta ancora tutto, anche dove cambia ogni giorno.
	Togli  string `json:"togli,omitempty"`
	Copie  int    `json:"copie,omitempty"`
	Motivo string `json:"motivo"`
}

type deviazioni []deviazione

func leggiDeviazioni(r io.Reader) (deviazioni, error) {
	var ds deviazioni
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ds); err != nil {
		return nil, fmt.Errorf("deviazioni: %w", err)
	}
	for _, d := range ds {
		if !reDev.MatchString(d.ID) || d.Scenario == "" || d.Motivo == "" {
			return nil, fmt.Errorf("deviazione %+v: servono id D-xx, scenario, campo valido e motivo", d)
		}
		if d.Togli != "" && (strings.TrimSpace(d.Togli) == "" || d.Copie < 1 || d.Standalone != "" || d.Go != "" ||
			d.Campo != campoPDFTesto || d.Livello != "") {
			return nil, fmt.Errorf("deviazione %+v: togli vuole copie (almeno 1), campo pdf:testo, livello HTTP, standalone e go vuoti", d)
		}
		if d.Togli == "" && d.Copie != 0 {
			return nil, fmt.Errorf("deviazione %+v: copie senza togli", d)
		}
		switch d.Livello {
		case "":
			if !campoValido(d.Campo) {
				return nil, fmt.Errorf("deviazione %+v: campo non valido per il livello HTTP", d)
			}
		case livelloBrowser:
			if !reCampoBrowser.MatchString(d.Campo) || d.Passo < 0 || d.Passo >= len(visteBrowser) {
				return nil, fmt.Errorf("deviazione %+v: campo del browser e passo 0 (desktop) o 1 (telefono)", d)
			}
		default:
			return nil, fmt.Errorf("deviazione %+v: livello sconosciuto", d)
		}
	}
	return ds, nil
}

// delLivello sono le deviazioni di un livello ("" HTTP, livelloBrowser).
func (ds deviazioni) delLivello(l string) deviazioni {
	var out deviazioni
	for _, d := range ds {
		if d.Livello == l {
			out = append(out, d)
		}
	}
	return out
}

// copre dice se la differenza è esattamente una deviazione dichiarata (stesso scenario, passo,
// campo e gli stessi due valori, o il testo dello standalone con Copie copie della frase di Togli e,
// senza, uguale al Go).
func (ds deviazioni) copre(x differenza) (string, bool) {
	for _, d := range ds {
		if d.Scenario != x.Scenario || d.Passo != x.Passo || d.Campo != x.Campo {
			continue
		}
		if d.Togli != "" {
			if strings.Count(x.Standalone, d.Togli) == d.Copie && lettura.Normalizza(strings.ReplaceAll(x.Standalone, d.Togli, " ")) == x.Go {
				return d.ID, true
			}
			continue
		}
		if d.Standalone == x.Standalone && d.Go == x.Go {
			return d.ID, true
		}
	}
	return "", false
}

// deviazioniApprovate legge gli ID della tabella «Deviazioni volute» dell'inventario.
func deviazioniApprovate(r io.Reader) (map[string]bool, error) {
	ok := map[string]bool{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		campi := strings.Split(sc.Text(), "|")
		if len(campi) > 2 {
			if id := strings.TrimSpace(campi[1]); reDev.MatchString(id) {
				ok[id] = true
			}
		}
	}
	if len(ok) == 0 {
		return nil, errors.New("nessuna deviazione D-xx trovata nell'inventario")
	}
	return ok, sc.Err()
}
