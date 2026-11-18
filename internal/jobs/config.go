package jobs

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/db"
)

// ChiaveGiorniApertura è la chiave di impostazioni, solo del Go (D-17), dei giorni di apertura del
// negozio per i lavori automatici: i giorni della settimana ISO (1 lunedì … 7 domenica) separati da
// virgole, per esempio «2,3,4,5,6». Senza la riga, vuota o con un valore illeggibile: martedì–sabato
// (Stefano, 15/11/2026). La scrive la pagina «Promemoria e job» (slice 098).
const ChiaveGiorniApertura = "jobs.giorni_apertura"

// giorniPredefiniti sono martedì–sabato.
var giorniPredefiniti = [7]bool{time.Tuesday: true, time.Wednesday: true, time.Thursday: true, time.Friday: true, time.Saturday: true}

// predefiniti sono quelli dello standalone (@defaults di Jobs.Config): ritardi acceso alle 09:00,
// solleciti acceso alle 10:00, briefing spento alle 08:00.
var predefiniti = map[string]struct {
	attivo bool
	ora    time.Duration
}{
	Ritardi:   {true, 9 * time.Hour},
	Solleciti: {true, 10 * time.Hour},
	Briefing:  {false, 8 * time.Hour},
}

// Config è quello che il titolare ha deciso sui lavori (Jobs.Config), letto da impostazioni con le
// chiavi dello standalone (jobs.<lavoro>_enabled, jobs.<lavoro>_hour). Le soglie in giorni le leggono i
// lavori (095–097).
type Config struct {
	attivo map[string]bool
	ora    map[string]time.Duration
	aperto [7]bool // per time.Weekday
}

// Attivo dice se il titolare lascia acceso il lavoro (enabled?/2); un nome sconosciuto è spento.
func (c Config) Attivo(nome string) bool { return c.attivo[nome] }

// Ora è l'ora del negozio a cui il lavoro è dovuto, come tempo dalla mezzanotte (time/2).
func (c Config) Ora(nome string) time.Duration { return c.ora[nome] }

// Aperto dice se il negozio è aperto nel giorno della settimana g (D-17).
func (c Config) Aperto(g time.Weekday) bool { return g >= 0 && int(g) < len(c.aperto) && c.aperto[g] }

// LeggiConfig è Config.get/0.
func LeggiConfig(ctx context.Context, q db.Interrogatore) (Config, error) {
	c := Config{attivo: map[string]bool{}, ora: map[string]time.Duration{}, aperto: giorniPredefiniti}
	for _, n := range Nomi {
		pred := predefiniti[n]
		v, ok, err := impostazione(ctx, q, "jobs."+n+"_enabled")
		if err != nil {
			return Config{}, err
		}
		c.attivo[n] = pred.attivo
		if ok {
			c.attivo[n] = acceso(pred.attivo, strings.TrimSpace(v))
		}
		v, ok, err = impostazione(ctx, q, "jobs."+n+"_hour")
		if err != nil {
			return Config{}, err
		}
		c.ora[n] = pred.ora
		if ok && strings.TrimSpace(v) != "" {
			if o, letta := leggiOra(v); letta {
				c.ora[n] = o
			}
		}
	}
	v, ok, err := impostazione(ctx, q, ChiaveGiorniApertura)
	if err != nil {
		return Config{}, err
	}
	if ok {
		if g, letti := leggiGiorni(v); letti {
			c.aperto = g
		}
	}
	return c, nil
}

// impostazione è Settings.get/1: il valore com'è e se la riga c'è.
func impostazione(ctx context.Context, q db.Interrogatore, chiave string) (string, bool, error) {
	var v string
	err := q.QueryRowContext(ctx, `SELECT valore FROM impostazioni WHERE chiave = ?`, chiave).Scan(&v)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, err
	}
	return v, true, nil
}

// acceso è on?/2: un lavoro acceso di default si spegne solo con le quattro parole dello standalone, uno
// spento di default si accende solo con le parole che scrive il modulo, così una riga sporca non accende un
// lavoro che nessuno ha chiesto.
func acceso(predefinito bool, v string) bool {
	if predefinito {
		switch v {
		case "0", "false", "False", "no":
			return false
		}
		return true
	}
	switch v {
	case "1", "true", "True", "yes":
		return true
	}
	return false
}

var reOra = regexp.MustCompile(`\A([01][0-9]|2[0-3]):([0-5][0-9])(?::([0-5][0-9]))?\z`)

// leggiOra è parse_hour/1: «HH:MM» o «HH:MM:SS»; ogni altra cosa non è un'ora (e il chiamante usa la
// predefinita: un valore salvato illeggibile non deve fermare il pianificatore).
func leggiOra(v string) (time.Duration, bool) {
	m := reOra.FindStringSubmatch(v)
	if m == nil {
		return 0, false
	}
	h, _ := strconv.Atoi(m[1]) // due cifre: la regex le garantisce
	mi, _ := strconv.Atoi(m[2])
	s := 0
	if m[3] != "" {
		s, _ = strconv.Atoi(m[3])
	}
	return time.Duration(h)*time.Hour + time.Duration(mi)*time.Minute + time.Duration(s)*time.Second, true
}

// leggiGiorni legge «2,3,4,5,6» (giorni ISO, spazi ai bordi permessi); vuoto o con un pezzo che non è un
// giorno da 1 a 7 non si legge.
func leggiGiorni(v string) ([7]bool, bool) {
	var g [7]bool
	if strings.TrimSpace(v) == "" {
		return g, false
	}
	for _, pezzo := range strings.Split(v, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(pezzo))
		if err != nil || n < 1 || n > 7 {
			return [7]bool{}, false
		}
		g[time.Weekday(n%7)] = true // 7 domenica = time.Sunday (0)
	}
	return g, true
}
