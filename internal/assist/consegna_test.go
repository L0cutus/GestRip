package assist

import (
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// Le date di consegna dal dettato (slice 103, D-18; RIP-H-42 parte date, RIP-H-46 per la D-18,
// tutte e due parziali: il resto arriva con le slice 104-108, e il segno «inventario:» si mette
// quando la voce si chiude). Atteso = quello che dà consegna.py, il traduttore di produzione di
// pc-fisso, sulla stessa frase e lo stesso «oggi» (confronto una tantum, slice 103): ogni
// stranezza è copiata com'è. "" è la data vuota (frase non riconosciuta).

var frasiConsegna = []struct{ oggi, frase, atteso string }{
	// Le 32 prove scritte in fondo a consegna.py (oggi venerdì 23/10/2026).
	{"2026-10-23", "pronto per venerdì", "2026-10-30"},
	{"2026-10-23", "la ritira sabato", "2026-10-24"},
	{"2026-10-23", "pronto fra due settimane", "2026-11-06"},
	{"2026-10-23", "entro fine mese", "2026-10-31"},
	{"2026-10-23", "martedì prossimo", "2026-10-27"},
	{"2026-10-23", "lo vuole per domani", "2026-10-24"},
	{"2026-10-23", "pronta il 5 novembre", "2026-11-05"},
	{"2026-10-23", "per giovedì", "2026-10-29"},
	{"2026-10-23", "pronto mercoledì", "2026-10-28"},
	{"2026-10-23", "urgente per lunedì", "2026-10-26"},
	{"2026-10-23", "li ritira il 2 novembre", "2026-11-02"},
	{"2026-10-23", "pronto per il 30", "2026-10-30"},
	{"2026-10-23", "pronta fra 10 giorni", "2026-11-02"},
	{"2026-10-23", "lo aspetta", ""},
	{"2026-10-23", "sabato prossimo", "2026-10-24"},
	{"2026-10-23", "consegna entro il 15 dicembre", "2026-12-15"},
	{"2026-10-23", "pronto venerdì prossimo", "2026-10-30"},
	{"2026-10-23", "per il 12 novembre", "2026-11-12"},
	{"2026-10-23", "pronta dopodomani", "2026-10-25"},
	{"2026-10-23", "entro la fine della prossima settimana", "2026-10-31"},
	{"2026-10-23", "oggi pomeriggio", "2026-10-23"},
	{"2026-10-23", "li riprende tra tre giorni", "2026-10-26"},
	{"2026-10-23", "pronto il 3 novembre", "2026-11-03"},
	{"2026-10-23", "prima di Natale", "2026-12-24"},
	{"2026-10-23", "giovedì 5 novembre", "2026-11-05"},
	{"2026-10-23", "tra una settimana", "2026-10-30"},
	{"2026-10-23", "pronto per mercoledì 28", "2026-10-28"},
	{"2026-10-23", "pronto domattina", "2026-10-24"},
	{"2026-10-23", "pronta lunedì 2", "2026-11-02"},
	{"2026-10-23", "tempo un mese e mezzo", "2026-12-08"},
	{"2026-10-23", "pronto tra un mese", "2026-11-23"},
	{"2026-10-23", "entro il 14 novembre", "2026-11-14"},

	// Altre frasi dette venerdì 23/10/2026: orari, maiuscole e accenti, date passate, «il N», casi negativi.
	{"2026-10-23", "VENERDÌ alle 17", "2026-10-30"},
	{"2026-10-23", "Domani alle 10:30", "2026-10-24"},
	{"2026-10-23", "stasera dopo le 18", "2026-10-23"},
	{"2026-10-23", "il 23 ottobre", "2026-10-23"},
	{"2026-10-23", "il 22 ottobre", "2027-10-22"},
	{"2026-10-23", "15 dicembre 2025", "2025-12-15"},
	{"2026-10-23", "il 31 febbraio", ""},
	{"2026-10-23", "il 23", "2026-10-23"},
	{"2026-10-23", "il 22", "2026-11-22"},
	{"2026-10-23", "il 31", "2026-10-31"},
	{"2026-10-23", "per domenica", "2026-10-25"},
	{"2026-10-23", "fra 3 settimane e mezzo", "2026-11-16"},
	{"2026-10-23", "fra due giorni e mezzo", "2026-10-25"},
	{"2026-10-23", "fra 0 giorni", ""},
	{"2026-10-23", "fra pochi giorni", ""},
	{"2026-10-23", "una settimana o due", "2026-10-30"},
	{"2026-10-23", "entro fine settimana", "2026-10-24"},
	{"2026-10-23", "fine della settimana prossima", "2026-10-31"},
	{"2026-10-23", "per la fine del mese prossimo", "2026-10-31"},
	{"2026-10-23", "lunedì 40", ""},
	{"2026-10-23", "il 2026", ""},
	{"2026-10-23", "lunedi o martedi", "2026-10-26"},
	{"2026-10-23", "martedì o lunedì", "2026-10-26"},
	{"2026-10-23", "pronta lunedí", ""},
	{"2026-10-23", "pronto per l'11 novembre", "2026-11-11"},
	{"2026-10-23", "pronto per il 5/11", "2026-11-05"},
	{"2026-10-23", "quando può", ""},
	{"2026-10-23", "", ""},
	{"2026-10-23", "   ", ""},
	{"2026-10-23", "pronto a gennaio", ""},
	{"2026-10-23", "fra 10 mesi", "2027-08-23"},
	{"2026-10-23", "tra dodici mesi", "2027-10-23"},
	{"2026-10-23", "fra quindici giorni", "2026-11-07"},
	{"2026-10-23", "PRIMA DI NATALE", "2026-12-24"},
	{"2026-10-23", "domani o dopodomani", "2026-10-25"},
	{"2026-10-23", "oggi o domani", "2026-10-24"},
	{"2026-10-23", "il 5 novembre o il 30", "2026-11-05"},
	{"2026-10-23", "il 30 o il 5 novembre", "2026-11-05"},
	{"2026-10-23", "entro il 115 dicembre", ""},
	{"2026-10-23", "entro tre mesi", "2027-01-23"},
	{"2026-10-23", "fra 2 giorni, cioè lunedì", "2026-10-25"},

	// Fine anno: giovedì 31/12/2026, poi Natale.
	{"2026-12-31", "domani", "2027-01-01"},
	{"2026-12-31", "lunedì", "2027-01-04"},
	{"2026-12-31", "il 5", "2027-01-05"},
	{"2026-12-31", "il 5 gennaio", "2027-01-05"},
	{"2026-12-31", "fra una settimana", "2027-01-07"},
	{"2026-12-31", "tra un mese", "2027-01-31"},
	{"2026-12-31", "prima di natale", "2027-12-24"},
	{"2026-12-31", "entro fine mese", "2026-12-31"},
	{"2026-12-31", "fine settimana", "2027-01-02"},
	{"2026-12-31", "la prossima settimana per la fine", "2027-01-09"},
	{"2026-12-25", "prima di natale", "2027-12-24"},
	{"2026-12-25", "venerdì", "2027-01-01"},
	{"2026-12-24", "natale", "2026-12-24"},

	// Fine mese: domenica 31/01/2027 (febbraio corto).
	{"2027-01-31", "tra un mese", "2027-02-28"},
	{"2027-01-31", "il 30", "2027-03-30"},
	{"2027-01-31", "il 31", "2027-01-31"},
	{"2027-01-31", "fra due mesi", "2027-03-31"},
	{"2027-01-31", "fine settimana", "2027-01-30"},
	{"2027-01-31", "entro fine mese", "2027-01-31"},

	// Bisestile: 2028 sì, 2027 no.
	{"2028-02-27", "entro fine mese", "2028-02-29"},
	{"2028-02-27", "il 29 febbraio", "2028-02-29"},
	{"2028-02-27", "il 30", "2028-03-30"},
	{"2028-02-27", "fra 2 giorni", "2028-02-29"},
	{"2028-03-01", "il 29 febbraio", ""}, // consegna.py: ValueError
	{"2028-03-01", "il 29 febbraio 2028", "2028-02-29"},
	{"2027-02-27", "il 29 febbraio", ""},

	// Fine novembre e «fine settimana» detto di domenica.
	{"2026-11-30", "il 31", "2026-12-31"},
	{"2026-11-30", "tra un mese", "2026-12-30"},
	{"2026-10-25", "fine settimana", "2026-10-24"},
	{"2026-10-25", "fine della prossima settimana", "2026-10-31"},

	// Limiti: dove consegna.py solleva un'eccezione il Go dà vuota (vedi DataConsegna); İ, tab, «mezzogiorno», numeri con zeri.
	{"2026-10-23", "lunedì 0", ""}, // consegna.py: ValueError
	{"2026-10-23", "il 00", ""},    // consegna.py: ValueError
	{"2026-10-23", "il 0 novembre", ""},
	{"2026-10-23", "fra 99999999999999999999 giorni", ""}, // consegna.py: OverflowError
	{"2026-10-23", "fra 99999 mesi", ""},                  // consegna.py: ValueError
	{"2026-10-23", "15 dicembre 0000", ""},
	{"2026-10-23", "İl 30", ""},
	{"2026-10-23", "È pronto per DOMANI", "2026-10-24"},
	{"2026-10-23", "pronto mercoledi\tsera", "2026-10-28"},
	{"2026-10-23", "fra 2 settimane e mezzogiorno", "2026-11-09"},
	{"2026-10-23", "fra tre giorni oppure il 5 novembre", "2026-11-05"},
	{"2026-10-23", "trenta giorni", "2026-11-22"},
	{"2026-10-23", "un mesetto", ""},
	{"2026-10-23", "fra 007 giorni", "2026-10-30"},
	{"2026-10-23", "fra due settimane è mezzo", "2026-11-09"},
	{"2026-10-23", "un mese è mezzo", "2026-12-08"},
	{"2026-10-23", "pronto venerdi'", "2026-10-30"},

	// Dalla revisione della 103 (attesi ricontrollati con consegna.py).
	{"2026-10-23", "fra 2\tgiorni", "2026-10-25"},
	{"2026-10-23", "fra undici giorni", "2026-11-03"},
	{"2026-10-23", "domenica o giovedì", "2026-10-29"},
	{"2026-10-23", "il 16 marzolino, giovedì", "2026-10-29"},
	{"2026-10-23", "il 007 o martedì", "2026-10-27"},
	{"2026-10-23", "un mese é mezzo", "2026-12-08"},
	{"2026-10-23", "fra 99999999999999999999 giorni, martedì", ""}, // consegna.py: OverflowError
	{"2026-10-23", "fra 2635249153387078803 settimane", ""},        // consegna.py: OverflowError
	{"9999-12-31", "domani", ""},                                   // consegna.py: OverflowError
	{"9999-12-31", "fra due giorni", ""},                           // consegna.py: OverflowError
	{"9999-12-25", "natale", ""},                                   // consegna.py: ValueError
	{"9999-12-30", "il 5 gennaio", ""},                             // consegna.py: ValueError
	{"2026-10-23", "il 007", ""},
	// il massimo di un int: senza i limiti di piuGiorni e piuMesi i conti del Go traboccano e
	// tornano nel 2026
	{"2026-10-23", "fra 9223372036854775807 giorni", ""}, // consegna.py: OverflowError
	{"2026-10-23", "fra 9223372036854775807 mesi", ""},   // consegna.py: OverflowError
}

func giorno(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.ParseInLocation("2006-01-02", s, clock.Roma)
	if err != nil {
		t.Fatalf("data di prova %q: %v", s, err)
	}
	return d
}

func TestDataConsegnaComeConsegnaPy(t *testing.T) {
	if len(frasiConsegna) < 30 {
		t.Fatalf("banco di frasi troppo corto: %d", len(frasiConsegna))
	}
	for _, c := range frasiConsegna {
		d, ok := DataConsegna(c.frase, giorno(t, c.oggi))
		got := ""
		if ok {
			got = d.Format("2006-01-02")
		}
		if got != c.atteso {
			t.Errorf("oggi %s, %q: %q, atteso %q", c.oggi, c.frase, got, c.atteso)
		}
		if !ok && !d.IsZero() {
			t.Errorf("oggi %s, %q: data vuota ma non zero (%v)", c.oggi, c.frase, d)
		}
	}
}

// Conta solo il giorno di «oggi» nel suo fuso: un istante a tarda sera di Roma resta quel giorno,
// e la data che esce è la mezzanotte di quel fuso.
func TestDataConsegnaGiornoDiOggiNelSuoFuso(t *testing.T) {
	sera := time.Date(2026, 10, 23, 23, 59, 0, 0, clock.Roma) // in UTC è ancora il 23, ma 21:59
	notte := time.Date(2026, 10, 24, 0, 30, 0, 0, clock.Roma) // in UTC è il 23 alle 22:30
	for _, c := range []struct {
		oggi   time.Time
		atteso time.Time
	}{
		{sera, time.Date(2026, 10, 24, 0, 0, 0, 0, clock.Roma)},
		{notte, time.Date(2026, 10, 25, 0, 0, 0, 0, clock.Roma)},
	} {
		d, ok := DataConsegna("domani", c.oggi)
		if !ok || !d.Equal(c.atteso) || d.Location() != clock.Roma {
			t.Errorf("domani da %v: %v %v, atteso %v", c.oggi, d, ok, c.atteso)
		}
	}
	// «Fra un mese» attraversa il cambio dell'ora (25/10/2026): resta mezzanotte.
	d, ok := DataConsegna("fra un mese", time.Date(2026, 10, 23, 18, 0, 0, 0, clock.Roma))
	if !ok || !d.Equal(time.Date(2026, 11, 23, 0, 0, 0, 0, clock.Roma)) {
		t.Errorf("fra un mese: %v %v", d, ok)
	}
}
