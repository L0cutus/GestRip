// Package assist è l'assistente della busta nuova (T4.10): estrazione dei campi dal dettato,
// date relative, trascrizione. Per ora contiene le date di consegna (slice 103).
package assist

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Le date di consegna dal dettato (D-18): porting parola per parola di consegna.py, il
// traduttore di produzione di pc-fisso (decisione di Stefano del 19/11/2026: «uguale a
// consegna.py; ogni ritocco va fra le idee»). Il modello copia la frase di consegna così come è
// stata dettata («pronto martedì prossimo», «tra un mese»); qui si fa il conto. Le date da
// incidere non si escludono qui: consegna.py riceve solo la frase di consegna che il modello ha
// già separato dal resto del dettato, e così fa DataConsegna.

var giorniSettimana = []string{"lunedi", "martedi", "mercoledi", "giovedi", "venerdi", "sabato", "domenica"}

var mesi = []string{"gennaio", "febbraio", "marzo", "aprile", "maggio", "giugno", "luglio", "agosto",
	"settembre", "ottobre", "novembre", "dicembre"}

var numeriInParole = map[string]int{"un": 1, "una": 1, "uno": 1, "due": 2, "tre": 3, "quattro": 4, "cinque": 5,
	"sei": 6, "sette": 7, "otto": 8, "nove": 9, "dieci": 10, "undici": 11, "dodici": 12, "quindici": 15,
	"venti": 20, "trenta": 30}

var (
	// «il 5 novembre», «giovedì 5 novembre», «15 dicembre 2026»
	reDataConMese = regexp.MustCompile(`\b(\d{1,2}) (` + strings.Join(mesi, "|") + `)(?: (\d{4}))?\b`)
	// «fra/tra N giorni|settimane|mesi», «un mese e mezzo», «tempo un mese»
	reFraN = regexp.MustCompile(`\b(\w+) (giorni|giorno|settimane|settimana|mesi|mese)\b( e mezzo)?`)
	// «per il 30», «entro il 15»
	reIlN        = regexp.MustCompile(`\bil (\d{1,2})\b`)
	reNonLettera = regexp.MustCompile(`[^a-z0-9 ]`)
	accenti      = strings.NewReplacer("à", "a", "è", "e", "é", "e", "ì", "i", "ò", "o", "ù", "u")
)

// normalizza è _norm: minuscole come str.lower() di Python (la İ diventa «i» più il punto
// combinante, come in Python), sei vocali accentate senza accento, ogni altro carattere uno spazio.
func normalizza(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "İ", "i\u0307"))
	return reNonLettera.ReplaceAllString(accenti.Replace(s), " ")
}

// data è un giorno di calendario; i conti si fanno in UTC, dove ogni giorno ha 24 ore.
func data(anno int, mese time.Month, g int) time.Time {
	return time.Date(anno, mese, g, 0, 0, 0, 0, time.UTC)
}

// valida dice se un giorno esiste in Python (datetime.date: anni 1-9999). Fuori, consegna.py
// solleva un'eccezione; qui la data resta vuota.
func valida(d time.Time) bool { return d.Year() >= 1 && d.Year() <= 9999 }

// giorniDelMese è calendar.monthrange(anno, mese)[1].
func giorniDelMese(anno int, mese time.Month) int { return data(anno, mese+1, 0).Day() }

// piuGiorni è d + timedelta(n). Python va al più all'anno 9999, oltre solleva un'eccezione
// e qui la data resta vuota (valida). 3_660_000 giorni sono più di 10 000 anni: oltre, il
// risultato è comunque dopo il 9999, e il limite tiene AddDate lontano da numeri che traboccano.
func piuGiorni(d time.Time, n int) (time.Time, bool) {
	if n > 3_660_000 {
		return time.Time{}, false
	}
	r := d.AddDate(0, 0, n)
	return r, valida(r)
}

// piuMesi è _piu_mesi: n mesi dopo, con il giorno ridotto all'ultimo del mese se non c'è.
// 120_000 mesi sono 10 000 anni: oltre, il risultato è comunque dopo l'anno 9999 (eccezione in
// Python), e il limite tiene i conti sugli anni lontani dal trabocco.
func piuMesi(d time.Time, n int) (time.Time, bool) {
	if n > 120_000 {
		return time.Time{}, false
	}
	m := int(d.Month()) - 1 + n
	anno, mese := d.Year()+m/12, time.Month(m%12+1)
	r := data(anno, mese, min(d.Day(), giorniDelMese(anno, mese)))
	return r, valida(r)
}

// giornoDelMese è _giorno_del_mese: «il 30», «lunedì 2»: il giorno n di questo mese se non è
// passato, altrimenti del mese dopo (fino a due mesi dopo). n = 0 è un'eccezione in Python.
func giornoDelMese(oggi time.Time, n int) (time.Time, bool) {
	for k := range 3 {
		base, ok := piuMesi(data(oggi.Year(), oggi.Month(), 1), k)
		if !ok {
			return time.Time{}, false
		}
		if n <= giorniDelMese(base.Year(), base.Month()) {
			if n < 1 {
				return time.Time{}, false
			}
			if d := data(base.Year(), base.Month(), n); !d.Before(oggi) {
				return d, true
			}
		}
	}
	return time.Time{}, false
}

// cifre dice se t è fatta solo di cifre (str.isdigit() dopo normalizza).
func cifre(t string) bool {
	if t == "" {
		return false
	}
	for _, c := range t {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// intero è int(t) per una parola di sole cifre; troppo grande è -1 (un conto impossibile).
func intero(t string) int {
	n, err := strconv.Atoi(t)
	if err != nil {
		return -1
	}
	return n
}

// numero è _numero: cifre o numero in parole; 0 (anche «000») e le parole sconosciute non sono un
// numero.
func numero(t string) int {
	if cifre(t) {
		return intero(t)
	}
	return numeriInParole[t]
}

// contiene è «p in w» per una lista di parole.
func contiene(w []string, p string) bool { return indice(w, p) >= 0 }

// DataConsegna è data_consegna(frase, oggi) di consegna.py: la data di consegna detta nella
// frase, contata da oggi. Di oggi conta solo il giorno nel suo fuso (il chiamante passa l'ora del
// negozio, clock.Roma); la data esce a mezzanotte dello stesso fuso. false (e la data zero) quando
// la frase non è riconosciuta: allora la data resta vuota. Dove consegna.py solleva un'eccezione
// («lunedì 0», «il 00», «il 29 febbraio» detto dopo il 29 di un bisestile, numeri enormi) la
// data resta vuota.
func DataConsegna(frase string, oggi time.Time) (time.Time, bool) {
	o := data(oggi.Date())
	d, ok := dataConsegna(frase, o)
	if !ok {
		return time.Time{}, false
	}
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, oggi.Location()), true
}

func dataConsegna(frase string, oggi time.Time) (time.Time, bool) {
	if frase == "" {
		return time.Time{}, false
	}
	s := normalizza(frase)
	w := strings.Fields(s)
	// data esplicita con mese
	if m := reDataConMese.FindStringSubmatch(s); m != nil {
		g := intero(m[1])
		mese := time.Month(indice(mesi, m[2]) + 1)
		anno := oggi.Year()
		if m[3] != "" {
			anno = intero(m[3])
		}
		if anno < 1 || g < 1 || g > giorniDelMese(anno, mese) {
			return time.Time{}, false
		}
		d := data(anno, mese, g)
		if m[3] == "" && d.Before(oggi) {
			// date.replace(year=anno+1): il 29 febbraio senza il bisestile dopo è un'eccezione
			if g > giorniDelMese(anno+1, mese) {
				return time.Time{}, false
			}
			d = data(anno+1, mese, g)
			return d, valida(d)
		}
		return d, true
	}
	if contiene(w, "dopodomani") {
		return piuGiorni(oggi, 2)
	}
	if contiene(w, "domani") || contiene(w, "domattina") {
		return piuGiorni(oggi, 1)
	}
	if contiene(w, "oggi") || contiene(w, "stasera") {
		return oggi, true
	}
	if contiene(w, "natale") {
		anno := oggi.Year()
		if oggi.After(data(anno, time.December, 24)) {
			anno++
		}
		d := data(anno, time.December, 24)
		return d, valida(d)
	}
	if m := reFraN.FindStringSubmatch(s); m != nil && numero(m[1]) != 0 {
		n, mezzo := numero(m[1]), m[3] != ""
		if n < 0 {
			return time.Time{}, false // int(t) enorme: timedelta o date.replace vanno fuori
		}
		switch {
		case strings.HasPrefix(m[2], "giorn"):
			return piuGiorni(oggi, n)
		case strings.HasPrefix(m[2], "settiman"):
			// lo stesso limite di piuGiorni contato in settimane: va guardato prima di 7*n,
			// che con n enorme (fino al massimo di un int) trabocca e diventerebbe piccolo o negativo
			if n > 3_660_000/7 {
				return time.Time{}, false
			}
			extra := 0
			if mezzo {
				extra = 3
			}
			return piuGiorni(oggi, 7*n+extra)
		}
		d, ok := piuMesi(oggi, n)
		if ok && mezzo {
			return piuGiorni(d, 15)
		}
		return d, ok
	}
	if contiene(w, "fine") && contiene(w, "settimana") {
		// fine della (prossima) settimana: il sabato
		gs := settimana(oggi)
		lun := oggi.AddDate(0, 0, -gs)
		if contiene(w, "prossima") {
			lun = oggi.AddDate(0, 0, 7-gs)
		}
		return piuGiorni(lun, 5)
	}
	if contiene(w, "fine") && contiene(w, "mese") {
		return data(oggi.Year(), oggi.Month(), giorniDelMese(oggi.Year(), oggi.Month())), true
	}
	// giorno della settimana, con o senza numero: «martedì prossimo», «lunedì 2», «mercoledì 28»;
	// vince il primo nell'ordine lunedì…domenica, non nella frase
	for i, g := range giorniSettimana {
		k := indice(w, g)
		if k < 0 {
			continue
		}
		if k+1 < len(w) && cifre(w[k+1]) {
			n := intero(w[k+1])
			if n < 0 {
				return time.Time{}, false // più grande di ogni mese
			}
			return giornoDelMese(oggi, n)
		}
		avanti := ((i-settimana(oggi))%7 + 7) % 7
		if avanti == 0 {
			avanti = 7
		}
		return piuGiorni(oggi, avanti)
	}
	if m := reIlN.FindStringSubmatch(s); m != nil {
		return giornoDelMese(oggi, intero(m[1]))
	}
	return time.Time{}, false
}

// settimana è date.weekday() di Python: lunedì 0 … domenica 6.
func settimana(d time.Time) int { return (int(d.Weekday()) + 6) % 7 }

// indice è list.index: la prima posizione di p in l, -1 se non c'è.
func indice(l []string, p string) int {
	for i, x := range l {
		if x == p {
			return i
		}
	}
	return -1
}
