package assist

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/db"
)

// La configurazione dell'assistente (slice 107; ADM-11, RIP-H-46 parte impostazioni), come Assist.Config e
// Assist.Glossary dello standalone a go-baseline: due famiglie, l'estrazione («ai»: indirizzo, modello, chiave)
// e la trascrizione («stt»: indirizzo, chiave, glossario). Indirizzi, modello e glossario stanno in impostazioni
// con le chiavi dello standalone (assist.ai.*, assist.stt.*), che l'import porta come sono; le due chiavi in
// segreti_provider (D-03, migrazione 0006, importer.ChiaviSegreteProvider), mai tra le impostazioni. Niente è
// configurato di partenza: nessun indirizzo predefinito, centrale o no.
//
// LeggiEstrazione è il punto da cui il client del modello (Collegamento, slice 106) prende la configurazione
// salvata: la usa la prova della pagina, la userà la busta nuova (108). LeggiTrascrizione è per il trascrittore
// (109). Si rileggono a ogni uso: un salvataggio vale subito, senza riavvii.

// Le famiglie, come gli atomi :ai e :stt dello standalone (anche nell'audit).
const (
	FamigliaEstrazione   = "ai"
	FamigliaTrascrizione = "stt"
)

// I campi del modulo di una famiglia (Config.keys/1), nell'ordine in cui la pagina li mostra; clear_secret è
// la casella «Cancella la chiave salvata».
const (
	campoIndirizzo = "endpoint"
	campoModello   = "model"
	campoChiave    = "api_key"
	campoGlossario = "glossary"
	campoCancella  = "clear_secret"
)

// CampiDi sono i campi di una famiglia (keys/1); nil per una famiglia sconosciuta.
func CampiDi(famiglia string) []string {
	switch famiglia {
	case FamigliaEstrazione:
		return []string{campoIndirizzo, campoModello, campoChiave}
	case FamigliaTrascrizione:
		return []string{campoIndirizzo, campoChiave, campoGlossario}
	}
	return nil
}

// chiaveDi è il nome dell'impostazione (o del segreto) di un campo: «assist.<famiglia>.<campo>».
func chiaveDi(famiglia, campo string) string { return "assist." + famiglia + "." + campo }

// lunghezzeMassime sono @max_lengths di Assist.Config, in grafemi come String.length/1.
var lunghezzeMassime = map[string]int{campoIndirizzo: 255, campoModello: 120, campoChiave: 255, campoGlossario: 4000}

// MassimoGlossario è Glossary.cap/0: i caratteri della forma a una riga, quella che va al trascrittore.
const MassimoGlossario = 2000

// ConfigTrascrizione è la famiglia «stt» (Config.Stt): indirizzo del trascrittore, chiave (anche vuota) e
// glossario nella forma salvata (una voce per riga).
type ConfigTrascrizione struct {
	Indirizzo, Chiave, Glossario string
}

// Usabile è usable?/1 per la trascrizione: basta l'indirizzo (il suo percorso unico non vuole un modello).
func (c ConfigTrascrizione) Usabile() bool { return trimElixir(c.Indirizzo) != "" }

// La chiave non si stampa mai, come @derive {Inspect, except: [:api_key]}: Format (ogni verbo di fmt) e
// LogValue (slog) mostrano «***» al posto della chiave, e al posto dell'indirizzo, che può portare un utente
// e una password (http://utente:password@…) e che gli errori del collegamento già non ripetono.
func (c Configurazione) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprintf(f, "assist.Configurazione{Indirizzo:%s Modello:%q Chiave:%s}", nascosto(c.Indirizzo), c.Modello, nascosto(c.Chiave))
}

// LogValue è Format per slog.
func (c Configurazione) LogValue() slog.Value {
	return slog.GroupValue(slog.String("indirizzo", nascosto(c.Indirizzo)), slog.String("modello", c.Modello), slog.String("chiave", nascosto(c.Chiave)))
}

// Format è quello di Configurazione: il glossario non è un segreto (redact/1 lo lascia), indirizzo e chiave sì.
func (c ConfigTrascrizione) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprintf(f, "assist.ConfigTrascrizione{Indirizzo:%s Chiave:%s Glossario:%q}", nascosto(c.Indirizzo), nascosto(c.Chiave), c.Glossario)
}

// LogValue è Format per slog.
func (c ConfigTrascrizione) LogValue() slog.Value {
	return slog.GroupValue(slog.String("indirizzo", nascosto(c.Indirizzo)), slog.String("chiave", nascosto(c.Chiave)), slog.String("glossario", c.Glossario))
}

// nascosto è mask/1: «***» se c'è qualcosa, vuoto altrimenti.
func nascosto(s string) string {
	if s == "" {
		return ""
	}
	return "***"
}

// LeggiEstrazione è Config.get(:ai): la configurazione dell'estrazione salvata, quella che il client del
// modello usa. Un valore che manca o di soli spazi è "" (setting/1); gli altri restano come sono.
func LeggiEstrazione(ctx context.Context, q db.Interrogatore) (Configurazione, error) {
	v, err := leggiFamiglia(ctx, q, FamigliaEstrazione)
	if err != nil {
		return Configurazione{}, err
	}
	return Configurazione{Indirizzo: testoDi(v[campoIndirizzo]), Modello: testoDi(v[campoModello]), Chiave: testoDi(v[campoChiave])}, nil
}

// LeggiTrascrizione è Config.get(:stt).
func LeggiTrascrizione(ctx context.Context, q db.Interrogatore) (ConfigTrascrizione, error) {
	v, err := leggiFamiglia(ctx, q, FamigliaTrascrizione)
	if err != nil {
		return ConfigTrascrizione{}, err
	}
	return ConfigTrascrizione{Indirizzo: testoDi(v[campoIndirizzo]), Chiave: testoDi(v[campoChiave]), Glossario: testoDi(v[campoGlossario])}, nil
}

// leggiFamiglia legge i campi di una famiglia: nil per un valore che manca o di soli spazi (setting/1). La
// chiave da segreti_provider, il resto da impostazioni.
func leggiFamiglia(ctx context.Context, q db.Interrogatore, famiglia string) (map[string]*string, error) {
	out := map[string]*string{}
	for _, campo := range CampiDi(famiglia) {
		dove := `impostazioni WHERE chiave = ?`
		if campo == campoChiave {
			dove = `segreti_provider WHERE nome = ?`
		}
		var v string
		err := q.QueryRowContext(ctx, `SELECT valore FROM `+dove, chiaveDi(famiglia, campo)).Scan(&v)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return nil, err
		case trimElixir(v) != "":
			out[campo] = &v
		}
	}
	return out, nil
}

func testoDi(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Il glossario del negozio (Assist.Glossary, porting di services/stt_glossario.py): la forma salvata è una
// voce per riga, quella che va al trascrittore una riga sola con le voci separate da «, » (un'intestazione
// HTTP non può andare a capo). Il limite di MassimoGlossario si misura sulla forma a una riga.

// vociGlossario è entries/1: la virgola separa come l'a capo; in ogni voce gli spazi interni diventano uno solo
// (String.split/1 e Enum.join(" ")); le voci vuote si tolgono.
func vociGlossario(testo string) []string {
	var voci []string
	for _, riga := range strings.Split(strings.ReplaceAll(testo, ",", "\n"), "\n") {
		if v := strings.Join(paroleElixir(riga), " "); v != "" {
			voci = append(voci, v)
		}
	}
	return voci
}

// GlossarioSalvato è normalize/1: una voce per riga.
func GlossarioSalvato(testo string) string { return strings.Join(vociGlossario(testo), "\n") }

// GlossarioPerLaRete è for_wire/1: le voci su una riga, separate da «, ».
func GlossarioPerLaRete(testo string) string { return strings.Join(vociGlossario(testo), ", ") }

// EccessoGlossario è excess/1: i caratteri (grafemi) della forma a una riga oltre MassimoGlossario; 0 se ci sta.
func EccessoGlossario(testo string) int {
	return max(0, uniseg.GraphemeClusterCount(GlossarioPerLaRete(testo))-MassimoGlossario)
}

// L'indirizzo come lo legge URI.parse/1 di Elixir 1.18 (il regex dell'appendice B della RFC 3986, poi
// split_authority/1), solo lo schema (in minuscolo) e l'host (senza le quadre): un indirizzo è accettato con
// schema http o https e un host non vuoto. Le espressioni sono quelle di Elixir, lette come PCRE senza «u»:
// [a-z] con /i è solo ASCII (qui scritto per esteso: (?i) di Go piegherebbe anche «ſ» e il segno del kelvin),
// \d solo cifre ASCII, il punto non prende l'a capo.
var (
	reURI      = regexp.MustCompile(`^(([a-zA-Z][a-zA-Z0-9+\-.]*):)?(//([^/?#]*))?([^?#]*)(\?([^#]*))?(#(.*))?`)
	reAutorita = regexp.MustCompile(`^((.*)@)?(\[[a-zA-Z0-9:.]*\]|[^:]*)(:(\d*))?`)
)

// partiIndirizzo dà lo schema in minuscolo ("" senza schema) e l'host ("" se manca o è vuoto: per le regole
// dello standalone nil e "" valgono uguale) di un indirizzo già senza spazi ai bordi.
func partiIndirizzo(s string) (schema, host string) {
	m := reURI.FindStringSubmatchIndex(s) // tutto è facoltativo: combacia sempre
	if m[4] >= 0 {
		schema = strings.ToLower(s[m[4]:m[5]])
	}
	if m[8] < 0 {
		return schema, ""
	}
	autorita := s[m[8]:m[9]]
	if autorita == "" {
		return schema, ""
	}
	a := reAutorita.FindStringSubmatch(autorita)
	return schema, strings.TrimRight(strings.TrimLeft(a[3], "["), "]")
}

// questaMacchina sono gli host di @loopback: una chiave può viaggiare in chiaro solo verso di loro.
var questaMacchina = []string{"127.0.0.1", "localhost", "::1", "[::1]", "0:0:0:0:0:0:0:1"}

// inChiaroAltrove è remote_plaintext?/1: un indirizzo http verso un host che non è questa macchina (anche
// senza host).
func inChiaroAltrove(indirizzo string) bool {
	schema, host := partiIndirizzo(trimElixir(indirizzo))
	if schema != "http" {
		return false
	}
	for _, h := range questaMacchina {
		if host == h {
			return false
		}
	}
	return true
}
