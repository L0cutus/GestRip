package verifica

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Conto è quanti giusti su quanti contati.
type Conto struct{ Giusti, Contati int }

// Riassunto sono gli esiti sommati (summarize/1 di Assist.Bench).
type Riassunto struct {
	Casi, TutteGiuste int
	PerCampo          map[string]Conto
	PerFile           map[string]Conto // schede tutte giuste per file
	File              []string         // i file nell'ordine in cui compaiono
	Illeggibili       int
	Rifiutati         map[string]int // motivo → casi (anche quelli non mandati)
	NuoviTentativi    int            // casi con una seconda richiesta
	AccontiInventati  int
	BusteInventate    int
	Mediana, P90, Max time.Duration
}

// Riassumi somma gli esiti; i tempi sono al rango più vicino (il più piccolo con almeno la
// percentuale dei tempi minori o uguali), come rank/2 dello standalone.
func Riassumi(esiti []Esito) Riassunto {
	r := Riassunto{Casi: len(esiti), PerCampo: map[string]Conto{}, PerFile: map[string]Conto{}, Rifiutati: map[string]int{}}
	tempi := make([]time.Duration, 0, len(esiti))
	for _, e := range esiti {
		if _, ok := r.PerFile[e.File]; !ok {
			r.File = append(r.File, e.File)
		}
		f := r.PerFile[e.File]
		f.Contati++
		if e.TutteGiuste {
			r.TutteGiuste++
			f.Giusti++
		}
		r.PerFile[e.File] = f
		for campo, ok := range e.Controlli {
			c := r.PerCampo[campo]
			c.Contati++
			if ok {
				c.Giusti++
			}
			r.PerCampo[campo] = c
		}
		switch e.Esito {
		case EsitoIllegibile:
			r.Illeggibili++
		case EsitoRifiutato, EsitoNonMandato:
			r.Rifiutati[e.Motivo]++
		}
		if e.AccontoInventato {
			r.AccontiInventati++
		}
		if e.BustaInventata {
			r.BusteInventate++
		}
		if e.Tentativi > 1 {
			r.NuoviTentativi++
		}
		tempi = append(tempi, e.Durata)
	}
	slices.Sort(tempi)
	r.Mediana, r.P90 = rango(tempi, 50), rango(tempi, 90)
	if len(tempi) > 0 {
		r.Max = tempi[len(tempi)-1]
	}
	return r
}

// rango è il tempo al rango più vicino: l'elemento ceil(n·p/100) (contando da 1) dei tempi ordinati.
func rango(ordinati []time.Duration, percento int) time.Duration {
	if len(ordinati) == 0 {
		return 0
	}
	return ordinati[(len(ordinati)*percento+99)/100-1]
}

// Rapporto è il riassunto in righe di testo: solo numeri, nomi dei campi e dei file, motivi fissi.
func Rapporto(r Riassunto) []string {
	righe := []string{fmt.Sprintf("schede tutte giuste: %d/%d", r.TutteGiuste, r.Casi)}
	for _, f := range r.File {
		righe = append(righe, fmt.Sprintf("  %s: %d/%d", f, r.PerFile[f].Giusti, r.PerFile[f].Contati))
	}
	righe = append(righe, "per campo:")
	for _, campo := range Campi {
		if c, ok := r.PerCampo[campo]; ok && c.Contati > 0 {
			nota := ""
			if campo == "compro_oro" {
				nota = " (dal testo, non dal modello)"
			}
			righe = append(righe, fmt.Sprintf("  %s: %d/%d%s", campo, c.Giusti, c.Contati, nota))
		}
	}
	righe = append(righe, fmt.Sprintf("acconti inventati: %d", r.AccontiInventati),
		fmt.Sprintf("buste inventate: %d", r.BusteInventate),
		fmt.Sprintf("risposte illeggibili: %d", r.Illeggibili))
	motivi := make([]string, 0, len(r.Rifiutati))
	totale := 0
	for m, n := range r.Rifiutati {
		motivi = append(motivi, fmt.Sprintf("%s %d", m, n))
		totale += n
	}
	slices.Sort(motivi)
	riga := fmt.Sprintf("rifiuti: %d", totale)
	if totale > 0 {
		riga += " (" + strings.Join(motivi, ", ") + ")"
	}
	righe = append(righe, riga,
		fmt.Sprintf("casi con un nuovo tentativo: %d", r.NuoviTentativi),
		fmt.Sprintf("tempo per scheda (ms): mediana %d, p90 %d, max %d", r.Mediana.Milliseconds(), r.P90.Milliseconds(), r.Max.Milliseconds()))
	return righe
}

// RigaDelCaso è la riga di un caso sbagliato: l'id e i campi sbagliati, o l'esito; "" per un caso
// tutto giusto.
func RigaDelCaso(e Esito) string {
	switch {
	case e.TutteGiuste:
		return ""
	case e.Esito == EsitoIllegibile:
		return e.ID + ": risposta illeggibile"
	case e.Esito == EsitoRifiutato || e.Esito == EsitoNonMandato:
		return e.ID + ": " + e.Esito + " (" + e.Motivo + ")"
	}
	var sbagliati []string
	for _, campo := range Campi {
		if ok, contato := e.Controlli[campo]; contato && !ok {
			sbagliati = append(sbagliati, campo)
		}
	}
	return e.ID + ": " + strings.Join(sbagliati, ", ")
}
