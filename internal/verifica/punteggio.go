package verifica

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/assist"
	"github.com/L0cutus/gestrip/internal/telefono"
)

// Gli esiti di un caso.
const (
	EsitoOK           = "ok"          // il modello ha dato una scheda leggibile (giusta o no)
	EsitoIllegibile   = "illeggibile" // due risposte non leggibili (D-19)
	EsitoRifiutato    = "rifiutato"   // il server non ha dato una risposta (ErrModello)
	EsitoNonMandato   = "non mandato" // rifiutato prima della richiesta (troppo corto o troppo lungo)
	separatoreNote    = " — "
	prefissoBusta     = "Busta "
	prefissoAcconto   = "Acconto "
	suffissoAcconto   = " €"
	motivoCorto       = "testo troppo corto"
	motivoLungo       = "testo troppo lungo"
	motivoSconosciuto = "errore"
)

// Esito è un caso contato. Non contiene né il dettato né la risposta del modello.
type Esito struct {
	ID, File    string
	Esito       string          // EsitoOK, EsitoIllegibile, EsitoRifiutato, EsitoNonMandato
	Motivo      string          // per EsitoRifiutato e EsitoNonMandato: un codice fisso («ai:http:500»)
	Controlli   map[string]bool // campo → giusto; solo i campi del caso
	TutteGiuste bool
	Durata      time.Duration // dalla domanda alla proposta, nuovo tentativo compreso: l'attesa della busta
	Tentativi   int           // richieste mandate (0, 1 o 2)
	// AccontoInventato e BustaInventata: il caso non ha acconto (o busta) e la proposta sì (la riga «acconti
	// inventati» della tabella del PIANO, 03/11/2026). Solo con una proposta.
	AccontoInventato, BustaInventata bool
}

// Valuta conta un caso: e ed err sono quello che ha dato l'estrazione della nuova busta
// (assist.Collegamento.Estrai). Senza una proposta ogni campo del caso è sbagliato (evaluate/4).
func Valuta(c Caso, e assist.Estrazione, err error, durata time.Duration) Esito {
	es := Esito{ID: c.ID, File: c.File, Esito: EsitoOK, Controlli: map[string]bool{}, Durata: durata,
		Tentativi: e.Tentativi}
	var em assist.ErrModello
	switch {
	case err == nil:
	case errors.Is(err, assist.ErrRispostaIllegibile):
		es.Esito = EsitoIllegibile
	case errors.As(err, &em):
		es.Esito, es.Motivo = EsitoRifiutato, em.Motivo
	case errors.Is(err, assist.ErrTestoCorto):
		es.Esito, es.Motivo = EsitoNonMandato, motivoCorto
	case errors.Is(err, assist.ErrTestoLungo):
		es.Esito, es.Motivo = EsitoNonMandato, motivoLungo
	default: // non succede con il client della 106; il testo dell'errore non si scrive mai
		es.Esito, es.Motivo = EsitoRifiutato, motivoSconosciuto
	}
	valori := valoriDi(e.Proposta)
	if err == nil {
		es.AccontoInventato = inventato(c.Attesi["acconto"], valori["acconto"])
		es.BustaInventata = inventato(c.Attesi["busta"], valori["busta"])
	}
	es.TutteGiuste = true
	for campo, accettate := range c.Attesi {
		var giusto bool
		switch {
		case err != nil:
		case campo == "busta" && c.Standalone:
			giusto = bustaNelleNote(e.Proposta.NotePubbliche, accettate)
		default:
			giusto = giustoIl(campo, valori, accettate)
		}
		es.Controlli[campo] = giusto
		es.TutteGiuste = es.TutteGiuste && giusto
	}
	return es
}

// inventato dice se il campo è atteso vuoto (solo «») e la proposta ne ha uno.
func inventato(accettate []string, valore string) bool {
	return len(accettate) == 1 && accettate[0] == "" && strings.TrimSpace(valore) != ""
}

// bustaNelleNote è right?("busta") di Assist.Bench per i casi dello standalone: ~r/\bbusta\s+N\b/iu su tutte le
// note, in qualsiasi punto e con qualsiasi maiuscola. \b e \s Unicode come PCRE con «u», scritti per esteso.
func bustaNelleNote(note string, accettate []string) bool {
	for _, n := range accettate {
		re := regexp.MustCompile(`(?i)(?:\A|[^\p{L}\p{N}_])busta[\s\p{Z}\x{85}]+` + regexp.QuoteMeta(n) + `(?:\z|[^\p{L}\p{N}_])`)
		if re.MatchString(note) {
			return true
		}
	}
	return false
}

// valoriDi sono i campi della proposta come li vede il negozio nel modulo, sotto i nomi del banco.
// Busta, acconto e altre note sono i pezzi delle note per il cliente (D-20), che la proposta scrive
// «Busta N — Acconto N € — x»: si rileggono in quell'ordine.
func valoriDi(p assist.Proposta) map[string]string {
	busta, acconto, altre := separaNote(p.NotePubbliche)
	compro := "0"
	if p.CompraOro {
		compro = "1"
	}
	return map[string]string{
		"data": giornoDi(p.DataAccettazione), "nome": p.NomeCliente, "telefono": p.TelefonoCliente,
		"oreficeria": p.Oreficeria, "busta": busta, "descrizione": p.Descrizione, "preventivo": p.Preventivo,
		"acconto": acconto, "altre_note": altre, "data_consegna": giornoDi(p.DataPrevista), "compro_oro": compro,
	}
}

// separaNote rilegge le note della proposta: il primo pezzo «Busta N», poi «Acconto N €», poi il resto.
func separaNote(note string) (busta, acconto, altre string) {
	if note == "" {
		return "", "", ""
	}
	pezzi := strings.Split(note, separatoreNote)
	if strings.HasPrefix(pezzi[0], prefissoBusta) {
		busta, pezzi = strings.TrimPrefix(pezzi[0], prefissoBusta), pezzi[1:]
	}
	if len(pezzi) > 0 && strings.HasPrefix(pezzi[0], prefissoAcconto) && strings.HasSuffix(pezzi[0], suffissoAcconto) {
		acconto, pezzi = strings.TrimSuffix(strings.TrimPrefix(pezzi[0], prefissoAcconto), suffissoAcconto), pezzi[1:]
	}
	return busta, acconto, strings.Join(pezzi, separatoreNote)
}

// giornoDi porta una data AAAA-MM-GG della proposta nella forma GG/MM/AAAA degli attesi.
func giornoDi(iso string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	return t.Format("02/01/2006")
}

// giustoIl è right?/3 di Assist.Bench: la parola è contenuta nella descrizione (minuscole); ogni altro
// campo è uguale a una delle risposte accettate dopo confrontabile.
func giustoIl(campo string, valori map[string]string, accettate []string) bool {
	if campo == "parola" {
		descrizione := strings.ToLower(valori["descrizione"])
		for _, p := range accettate {
			if strings.Contains(descrizione, strings.ToLower(p)) {
				return true
			}
		}
		return false
	}
	v := confrontabile(campo, valori[campo])
	for _, a := range accettate {
		if confrontabile(campo, a) == v {
			return true
		}
	}
	return false
}

// confrontabile è comparable/2 di Assist.Bench: si perdonano solo maiuscole e spazi (ai bordi e
// ripetuti), il telefono passa per la normalizzazione dell'app e nel preventivo «400-500» vale
// «400/500». Nient'altro: «35 €» non è «35».
func confrontabile(campo, v string) string {
	switch campo {
	case "telefono":
		v = telefono.Normalizza(v)
	case "preventivo":
		v = strings.ReplaceAll(v, "-", "/")
	}
	return strings.Join(strings.Fields(strings.ToLower(v)), " ")
}
