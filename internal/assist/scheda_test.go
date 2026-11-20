package assist

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// Dalla risposta del modello alla scheda (slice 104; RIP-H-42 e RIP-H-43 in parte, D-19, D-20,
// D-103, D-104: le voci si chiudono con le slice 105-108, per questo qui niente segno di
// copertura). Atteso = quello che vede la busta in produzione: il traduttore di pc-fisso
// (traduttore.py) più il passaggio dello standalone (to_string dei campi, telefono normalizzato,
// compro oro ricalcolato), misurato una tantum su una copia del traduttore. Dettati e risposte
// sono inventati: in gran parte righe dei banchi bench/modello/, dati finti per costruzione.

// La lettura tollerante della risposta (Assist.decode/1 dello standalone) e la validità (D-19).
func TestLeggiRisposta(t *testing.T) {
	for _, c := range []struct {
		nome, risposta string
		valida         bool
		descrizione    string // della proposta, se valida
	}{
		{"oggetto semplice", `{"n": "Mario Rizzo", "d": "fede da lucidare"}`, true, "fede da lucidare"},
		{"spazi intorno", " \n\t{\"d\": \"fede\"}\n ", true, "fede"},
		{"markdown json", "```json\n{\"d\": \"fede\"}\n```", true, "fede"},
		{"markdown senza lingua", "```\n{\"d\": \"fede\"}\n```", true, "fede"},
		{"markdown su più righe", "```json\n{\n  \"n\": \"Mario\",\n  \"d\": \"fede\"\n}\n```", true, "fede"},
		{"testo prima e dopo", "Ecco la scheda: {\"d\": \"fede\"} spero vada bene.", true, "fede"},
		{"graffe nel testo", `{"d": "anello {oro} da lucidare"}`, true, "anello {oro} da lucidare"},
		{"lista con un oggetto: le graffe", `[{"d": "fede"}]`, true, "fede"},
		{"oggetto vuoto", `{}`, true, ""},
		{"chiave sconosciuta annidata si ignora", `{"d": "fede", "z": {"a": [1, 2]}}`, true, "fede"},
		{"chiave ripetuta: vince l'ultima", `{"d": "prima", "d": "seconda"}`, true, "seconda"},
		{"numeri, booleani e null sono semplici", `{"d": "fede", "n": 12, "p": 1.5, "o": true, "b": null}`, true, "fede"},
		{"c falsa si ignora", `{"d": "fede", "c": 0}`, true, "fede"},
		{"x falsa si ignora", `{"d": "fede", "x": false}`, true, "fede"},
		{"campo annidato (oggetto)", `{"n": {"nome": "Mario"}, "d": "fede"}`, false, ""},
		{"campo annidato (lista)", `{"d": ["fede", "anello"]}`, false, ""},
		{"busta annidata", `{"b": [12]}`, false, ""},
		{"acconto annidato", `{"a": {"euro": 50}}`, false, ""},
		{"annidato vuoto", `{"x": []}`, false, ""},
		{"JSON rotto", `{"n": "Mario", "d": "fede"`, false, ""},
		{"JSON rotto nel markdown", "```json\n{\"d\": \"fede\",}\n```", false, ""},
		{"vuota", "", false, ""},
		{"solo testo", "Non ho capito il dettato.", false, ""},
		{"lista", `["fede"]`, false, ""},
		{"stringa", `"fede"`, false, ""},
		{"null", `null`, false, ""},
		{"due oggetti", `{"d": "a"} e poi {"d": "b"}`, false, ""},
		{"consegna non testo", `{"d": "fede", "c": 5}`, false, ""},
		{"consegna vera", `{"d": "fede", "c": true}`, false, ""},
		{"note non testo", `{"d": "fede", "x": 12}`, false, ""},
		{"numero fuori misura", `{"p": 1e400}`, false, ""},
	} {
		r, ok := LeggiRisposta(c.risposta)
		if ok != c.valida {
			t.Errorf("%s: valida %v, attesa %v", c.nome, ok, c.valida)
			continue
		}
		if ok {
			p := PropostaDa(r, "dettato qualsiasi", giorno(t, "2026-10-23"))
			if p.Descrizione != c.descrizione {
				t.Errorf("%s: descrizione %q, attesa %q", c.nome, p.Descrizione, c.descrizione)
			}
		}
	}
}

// Risposte finte prese dai banchi bench/modello/ (dettato e risposta di una riga, con il suo «oggi»),
// più casi scritti qui: la proposta intera.
func TestPropostaDalleRisposte(t *testing.T) {
	for _, c := range []struct {
		nome, oggi, dettato, risposta string
		attesa                        Proposta
	}{
		{"B correzione_busta", "2026-05-25",
			"oreficeria Bianchi Gioielli busta 7... correggo, 241 orologio Citizen cambio pila e guarnizione",
			`{"o": "Bianchi Gioielli", "b": "241", "d": "orologio Citizen cambio pila e guarnizione"}`,
			Proposta{DataAccettazione: "2026-05-25", Oreficeria: "Bianchi Gioielli",
				Descrizione: "orologio Citizen cambio pila e guarnizione", NotePubbliche: "Busta 241"}},
		{"B telefono_parlato", "2026-01-23",
			"Cliente Greco Loredana, il numero è 300 465 8065, anello con smeraldo argento misura 22 da rimpicciolire di mezza misura",
			`{"n": "Loredana Greco", "d": "anello con smeraldo argento misura 22 da rimpicciolire di mezza misura"}`,
			Proposta{DataAccettazione: "2026-01-23", NomeCliente: "Loredana Greco", TelefonoCliente: "3004658065",
				Descrizione: "anello con smeraldo argento misura 22 da rimpicciolire di mezza misura"}},
		{"B telefono_parlato, nome minuscolo grezzo (105)", "2027-06-29",
			"Cliente marisa zago, lo chiamate al 300 899 1069, braccialetto acciaio circa 25 grammi 21 cm maglie da aggiungere e da pulire",
			`{"n": "marisa zago", "d": "braccialetto acciaio circa 25 grammi 21 cm maglie da aggiungere e da pulire"}`,
			Proposta{DataAccettazione: "2027-06-29", NomeCliente: "marisa zago", TelefonoCliente: "3008991069",
				Descrizione: "braccialetto acciaio circa 25 grammi 21 cm maglie da aggiungere e da pulire"}},
		{"B acconto_parafrasi", "2027-02-24",
			"Cliente Arianna Giordano bracciale 3 gr da raddrizzare e da lucidare, ha versato 20 euro",
			`{"n": "Arianna Giordano", "d": "bracciale 3 gr da raddrizzare e da lucidare", "a": "20"}`,
			Proposta{DataAccettazione: "2027-02-24", NomeCliente: "Arianna Giordano",
				Descrizione: "bracciale 3 gr da raddrizzare e da lucidare", NotePubbliche: "Acconto 20 €"}},
		{"B riempitivi", "2026-09-28",
			"allora vediamo cliente Beatrice Mazzaro ah sì busta 167 orecchino argento 925 peso 25 grammi perno da sostituire",
			`{"n": "Beatrice Mazzaro", "b": "167", "d": "orecchino argento 925 peso 25 grammi perno da sostituire"}`,
			Proposta{DataAccettazione: "2026-09-28", NomeCliente: "Beatrice Mazzaro",
				Descrizione: "orecchino argento 925 peso 25 grammi perno da sostituire", NotePubbliche: "Busta 167"}},
		{"B correzione_prezzo", "2026-07-10",
			"Cliente Gabriele Zanatta orologio al quarzo Festina revisione completa, preventivo 80 euro, no anzi 60",
			`{"n": "Gabriele Zanatta", "d": "orologio al quarzo Festina revisione completa", "p": "60"}`,
			Proposta{DataAccettazione: "2026-07-10", NomeCliente: "Gabriele Zanatta",
				Descrizione: "orologio al quarzo Festina revisione completa", Preventivo: "60"}},
		{"C acconto giusta", "2027-05-28",
			"Cliente Mario Rizzo solitario oro 15gr taglia 19 chiusura a moschettone nuova, acconto 10 euro",
			`{"n": "Mario Rizzo", "d": "solitario oro 15gr taglia 19 chiusura a moschettone nuova", "a": "10"}`,
			Proposta{DataAccettazione: "2027-05-28", NomeCliente: "Mario Rizzo",
				Descrizione: "solitario oro 15gr taglia 19 chiusura a moschettone nuova", NotePubbliche: "Acconto 10 €"}},
		{"D prezzo con verbo", "2027-05-28",
			"Cliente Mario Rizzo solitario oro 15gr taglia 19 chiusura a moschettone nuova, gli ho detto 10 euro",
			`{"n": "Mario Rizzo", "d": "solitario oro 15gr taglia 19 chiusura a moschettone nuova", "p": "10"}`,
			Proposta{DataAccettazione: "2027-05-28", NomeCliente: "Mario Rizzo",
				Descrizione: "solitario oro 15gr taglia 19 chiusura a moschettone nuova", Preventivo: "10"}},
		{"tutte le chiavi, in markdown", "2026-10-23",
			"Cliente Giulia Furlan 340 111 2233 oreficeria Inventata Alfa busta 12 anello oro misura 14 da allargare preventivo 50€ acconto 20€ pronto martedì prossimo, lo ritira il marito",
			"```json\n{\"n\": \"Giulia Furlan\", \"o\": \"Oreficeria Inventata Alfa\", \"b\": \"12\", \"d\": \"anello oro misura 14 da allargare\", \"c\": \"pronto martedì prossimo\", \"p\": \"50\", \"a\": \"20\", \"x\": \"lo ritira il marito\"}\n```",
			Proposta{DataAccettazione: "2026-10-23", NomeCliente: "Giulia Furlan", TelefonoCliente: "3401112233",
				Oreficeria: "Oreficeria Inventata Alfa", Descrizione: "anello oro misura 14 da allargare",
				NotePubbliche: "Busta 12 — Acconto 20 € — lo ritira il marito", Preventivo: "50",
				DataPrevista: "2026-10-27"}},
		{"risposta vuota: solo oggi, telefono e compro oro", "2026-10-23",
			"Cliente Paolo Tonon 347 733 4567 oro vecchio da valutare due anelli 15 grammi", `{}`,
			Proposta{DataAccettazione: "2026-10-23", TelefonoCliente: "3477334567", CompraOro: true}},
	} {
		r, ok := LeggiRisposta(c.risposta)
		if !ok {
			t.Errorf("%s: risposta non valida", c.nome)
			continue
		}
		if p := PropostaDa(r, c.dettato, giorno(t, c.oggi)); p != c.attesa {
			t.Errorf("%s:\n ha    %+v\n atteso %+v", c.nome, p, c.attesa)
		}
	}
}

// D-20: busta, acconto e altre note nelle note per il cliente, nell'ordine e con le parole del
// traduttore («Busta N — Acconto N € — note»). Un valore «falso» per Python (vuoto, 0, false,
// null) non entra; un numero o un booleano si scrivono come li scrive Python.
func TestNoteD20(t *testing.T) {
	for _, c := range []struct{ risposta, note string }{
		{`{"b": "12"}`, "Busta 12"},
		{`{"a": "50"}`, "Acconto 50 €"},
		{`{"x": "lo ritira il marito"}`, "lo ritira il marito"},
		{`{"x": "pagato", "a": "50", "b": "12"}`, "Busta 12 — Acconto 50 € — pagato"},
		{`{"b": "12", "x": "pagato"}`, "Busta 12 — pagato"},
		{`{"b": 12, "a": 50}`, "Busta 12 — Acconto 50 €"},
		{`{"a": 50.5}`, "Acconto 50.5 €"},
		{`{"a": -50.5}`, "Acconto -50.5 €"},
		{`{"a": 50.0}`, "Acconto 50.0 €"},
		{`{"a": 12.0}`, "Acconto 12.0 €"},
		{`{"a": 1000.0}`, "Acconto 1000.0 €"}, // nelle note scrive Python, non Elixir
		{`{"a": true}`, "Acconto True €"},
		{`{"a": 1e-5}`, "Acconto 1e-05 €"},
		{`{"a": 12345678901234567.0}`, "Acconto 1.2345678901234568e+16 €"},
		{`{"a": 0.0001}`, "Acconto 0.0001 €"},
		{`{"a": -0}`, ""},
		{`{"a": 123456789012345678901234567890}`, "Acconto 123456789012345678901234567890 €"},
		{`{"b": true}`, "Busta True"},
		{`{"b": "0"}`, "Busta 0"},
		{`{"b": " "}`, "Busta  "},
		{`{"b": 0, "a": 0.0, "x": ""}`, ""},
		{`{"b": false, "a": null, "x": null}`, ""},
		{`{"b": "", "a": "", "x": false}`, ""},
		{`{"b": "busta 12"}`, "Busta busta 12"},
	} {
		r, ok := LeggiRisposta(c.risposta)
		if !ok {
			t.Errorf("%s: risposta non valida", c.risposta)
			continue
		}
		if p := PropostaDa(r, "dettato", giorno(t, "2026-10-23")); p.NotePubbliche != c.note {
			t.Errorf("%s: note %q, attese %q", c.risposta, p.NotePubbliche, c.note)
		}
	}
}

// I campi copiati dalla risposta come li scrive lo standalone (to_string/1 di quello che il
// traduttore gli manda): numeri come Python li rimette nel JSON, booleani in minuscolo, null vuoto.
func TestCampiSemplici(t *testing.T) {
	for _, c := range []struct {
		risposta                string
		nome, ore, descr, preve string
	}{
		{`{"n": 123, "o": true, "d": false, "p": 50}`, "123", "true", "false", "50"},
		{`{"p": 50.0}`, "", "", "", "50.0"},
		{`{"p": 1.50}`, "", "", "", "1.5"},
		{`{"p": -0.0}`, "", "", "", "-0.0"},
		{`{"p": 1E2}`, "", "", "", "100.0"},
		// I decimali come Float.to_string/1 di Elixir (misurato con Elixir 1.18/OTP 27): la forma
		// più corta fra quella con e senza esponente, a parità senza; da 2^53 sempre con l'esponente.
		{`{"p": 1000.0, "n": 1500.0, "o": 1e-5, "d": 1e16}`, "1.5e3", "1.0e-5", "1.0e16", "1.0e3"},
		{`{"p": 0.1, "n": 100.0, "o": 123456789.0, "d": 1.0e-4}`, "100.0", "123456789.0", "0.0001", "0.1"},
		{`{"p": -1500.0, "n": 9007199254740992.0, "o": 120.0, "d": -0.00001}`, "9.007199254740992e15", "120.0", "-1.0e-5", "-1.5e3"},
		{`{"p": -9007199254740992.0, "n": -1.5e300}`, "-1.5e300", "", "", "-9.007199254740992e15"},
		{`{"p": "400/500", "d": null, "n": null}`, "", "", "", "400/500"},
		{`{"p": "  50  ", "d": "  fede  "}`, "", "", "  fede  ", "  50  "},
		{`{"n": "MARIO ROSSI", "o": "bianchi gioielli"}`, "MARIO ROSSI", "bianchi gioielli", "", ""},
	} {
		r, ok := LeggiRisposta(c.risposta)
		if !ok {
			t.Errorf("%s: risposta non valida", c.risposta)
			continue
		}
		p := PropostaDa(r, "dettato", giorno(t, "2026-10-23"))
		if p.NomeCliente != c.nome || p.Oreficeria != c.ore || p.Descrizione != c.descr || p.Preventivo != c.preve {
			t.Errorf("%s: %q %q %q %q", c.risposta, p.NomeCliente, p.Oreficeria, p.Descrizione, p.Preventivo)
		}
	}
}

// Compro oro solo da «oro vecchio» o «scheda compro oro» nel dettato (con qualsiasi maiuscola e
// spazi), qualunque cosa dica la risposta.
func TestCompraOroDalDettato(t *testing.T) {
	for _, c := range []struct {
		dettato string
		atteso  bool
	}{
		{"Cliente Paolo Tonon oro vecchio da valutare", true},
		{"ORO   VECCHIO due anelli", true},
		{"oro\nvecchio", true},
		{"oro\u00a0vecchio", true},
		{"oro\u180evecchio", true}, // U+180E è uno spazio per PCRE (lo standalone), non per Python
		{"fare la Scheda Compro Oro per la signora", true},
		{"anello d'oro vecchio stile", true},
		{"orovecchio", false},
		{"compro oro", false},
		{"oro usato da valutare", false},
		{"scheda compro-oro", false},
		{"anello oro giallo", false},
	} {
		for _, risposta := range []string{`{}`, `{"d": "oro vecchio", "x": "scheda compro oro"}`} {
			r, _ := LeggiRisposta(risposta)
			if p := PropostaDa(r, c.dettato, giorno(t, "2026-10-23")); p.CompraOro != c.atteso {
				t.Errorf("%q con %s: compro oro %v, atteso %v", c.dettato, risposta, p.CompraOro, c.atteso)
			}
		}
	}
}

// D-104: il telefono dal dettato, come trova_telefono di traduttore.py (atteso preso dalla copia del
// traduttore), poi normalizzato come lo standalone.
func TestTelefonoDalDettato(t *testing.T) {
	for _, c := range []struct{ dettato, atteso string }{
		{"347 123 4567", "3471234567"},
		{"Cliente Mario, 347-123-4567, anello", "3471234567"},
		{"cell 347, 123, 45, 67 grazie", "3471234567"},
		{"3 4 7 1 2 3 4 5 6 7", "3471234567"},
		{"tel 347/1234567", "3471234567"},
		{"347 . 123 . 4567", "3471234567"},
		{"busta 12 3471234567", "3471234567"},
		{"busta 12, 347 123 4567", "3471234567"},
		{"N. 12 3471234567", "3471234567"},
		{"acconto 50 347 1234567", "3471234567"},
		{"preventivo 80 euro 347 123 4567", "3471234567"},
		{"50 euro 347 1234567", "3471234567"},
		{"20 eur 3471234567", "3471234567"},
		{"20 europei 3471234567", "3471234567"},
		{"PREVENTİVO 3471234567", "3471234567"},
		{"Busta 7, 12 euro, 347 1234567 e 0423 123456", "3471234567"},
		{"incidere 12/03/2026 tel 3471234567", "3471234567"},
		{"il 12/03/26 3471234567", "3471234567"},
		{"incidere 12/03/2026", ""},
		{"incidere 12 12 2026", "12122026"}, // senza barre la data non si toglie: copiato com'è
		{"è12/12/2026", "12122026"},         // lettera attaccata: per Python non è una data
		{"busta 45/26 anello 15 18 kt", ""},
		{"+39 347 1234567", "393471234567"},
		{"0039 347 1234567", "3471234567"},
		{"tel 123456", ""},
		{"tel 12345678901234", ""},
		{"preventivo 1234567", "1234567"},
		{"347 -- 1234567", "1234567"},
		{"347 e 1234567", "1234567"},
		{"tel ٣٤٧١٢٣٤٥٦٧", "٣٤٧١٢٣٤٥٦٧"},
		{"anello misura 14 da allargare, 3 grammi", ""},
		{"incidere 12.03.2026", ""},
		{"incidere 12\x1c.03.2026", ""}, // \x1c è uno spazio per Python
		{"12 .\x1c03.2026", ""},
		{"347 \x1c123 4567", "3471234567"},
		{"12-03-26 3471234567", "3471234567"},
		{"busta 12345/26 3471234567", "3471234567"},
		{"347 12/03/2026 1234567", "1234567"}, // la data tolta separa i gruppi
		{"numero 12 3471234567", "3471234567"},
		{"PREVENTİVO 50 3471234567", "3471234567"},
		{"347 123 4567 20 eur", "3471234567"},
		{"347 123 4567 20 eura", "347123456720"}, // «eura» non è «eur» che chiude una parola
		{"3471234567 1234567 euro", "3471234567"},
		{"0039 123456", "0039123456"},
		{"n° 12 3471234567", "3471234567"},
		{"N° 12 3471234567", "3471234567"},
		{"_12/03/2026 3471234567", ""}, // «_» è una lettera per \b: la data resta e si unisce
		{"347 +123 4567", "3471234567"},
		{"31/12/2026 3471234567", "3471234567"},
		{"15/12/2026 3471234567", "3471234567"},
		{"25/12/2026 3471234567", "3471234567"},
		{"", ""},
	} {
		if got := TelefonoDalDettato(c.dettato); got != c.atteso {
			t.Errorf("%q: %q, atteso %q", c.dettato, got, c.atteso)
		}
		r, _ := LeggiRisposta(`{}`)
		if p := PropostaDa(r, c.dettato, giorno(t, "2026-10-23")); p.TelefonoCliente != c.atteso {
			t.Errorf("%q: telefono della proposta %q, atteso %q", c.dettato, p.TelefonoCliente, c.atteso)
		}
	}
}

// La consegna si calcola solo dalla frase «c» della risposta, mai dal dettato né da «d» o «x», come
// fa il traduttore: una data da incidere nel dettato o nella descrizione lascia la consegna vuota, o
// quella detta in «c» (obbligo del coordinatore del 20/11/2026). Una data da incidere messa dal
// modello proprio in «c» diventerebbe la consegna, come oggi in produzione: la regola che la
// escluderebbe è fra le idee (coordinatore, 20/11/2026).
func TestConsegnaSoloDallaFraseC(t *testing.T) {
	for _, c := range []struct{ dettato, risposta, consegna string }{
		{"busta cliente 12, fede da incidere con la data 12 dicembre 2026, preventivo 25",
			`{"n": "busta cliente 12", "d": "fede da incidere con la data 12 dicembre 2026", "p": "25"}`, ""},
		{"anello in oro giallo da allargare alla misura 28 e incidere data 12 dicembre 2026",
			`{"d": "anello in oro giallo da allargare alla misura 28 e incidere data 12 dicembre 2026"}`, ""},
		{"ciondolo da incidere sul retro con 'Laura 12/03/1990', 25 euro",
			`{"d": "ciondolo da incidere sul retro con 'Laura 12/03/1990'", "p": "25"}`, ""},
		{"fede da incidere 'per sempre 5 novembre', pronta venerdì",
			`{"d": "fede da incidere 'per sempre 5 novembre'", "c": "pronta venerdì"}`, "2026-10-30"},
		{"fede da incidere con la data di domani, pronta fra due settimane",
			`{"d": "fede da incidere con la data di domani", "c": "pronta fra due settimane"}`, "2026-11-06"},
		{"incidere il 12 dicembre 2026 sulla fede", `{"d": "incidere il 12 dicembre 2026 sulla fede", "x": "data 12 dicembre"}`, ""},
	} {
		r, ok := LeggiRisposta(c.risposta)
		if !ok {
			t.Errorf("%q: risposta non valida", c.dettato)
			continue
		}
		if p := PropostaDa(r, c.dettato, giorno(t, "2026-10-23")); p.DataPrevista != c.consegna {
			t.Errorf("%q: consegna %q, attesa %q", c.dettato, p.DataPrevista, c.consegna)
		}
	}
}

// La consegna da «c» con DataConsegna (D-18), nel formato del selettore del modulo; una frase non
// riconosciuta o non testo lascia la data vuota.
func TestConsegnaDallaFrase(t *testing.T) {
	for _, c := range []struct{ risposta, consegna string }{
		{`{"c": "pronto per venerdì"}`, "2026-10-30"},
		{`{"c": "entro fine mese"}`, "2026-10-31"},
		{`{"c": "lo aspetta"}`, ""},
		{`{"c": ""}`, ""},
		{`{"c": null}`, ""},
		{`{"c": false}`, ""},
		{`{"c": "il 31 febbraio"}`, ""},
	} {
		r, ok := LeggiRisposta(c.risposta)
		if !ok {
			t.Errorf("%s: risposta non valida", c.risposta)
			continue
		}
		if p := PropostaDa(r, "dettato", giorno(t, "2026-10-23")); p.DataPrevista != c.consegna {
			t.Errorf("%s: consegna %q, attesa %q", c.risposta, p.DataPrevista, c.consegna)
		}
	}
}

// La data di oggi: è la data di accettazione (il giorno del negozio, anche a tarda sera) e va nella
// domanda al modello nel formato dei modelli addestrati, come la scrive il traduttore.
func TestOggi(t *testing.T) {
	sera := time.Date(2026, 10, 23, 23, 59, 0, 0, clock.Roma) // in UTC è ancora il 23
	notte := time.Date(2026, 10, 24, 0, 30, 0, 0, clock.Roma) // in UTC è il 23
	r, _ := LeggiRisposta(`{"c": "domani"}`)
	if p := PropostaDa(r, "dettato", sera); p.DataAccettazione != "2026-10-23" || p.DataPrevista != "2026-10-24" {
		t.Errorf("sera: %q %q", p.DataAccettazione, p.DataPrevista)
	}
	if p := PropostaDa(r, "dettato", notte); p.DataAccettazione != "2026-10-24" || p.DataPrevista != "2026-10-25" {
		t.Errorf("notte: %q %q", p.DataAccettazione, p.DataPrevista)
	}
	for _, c := range []struct {
		oggi    time.Time
		dettato string
		attesa  string
	}{
		{sera, "anello da lucidare", "Oggi: venerdì 23/10/2026\nDettato: anello da lucidare"},
		{notte, "fede", "Oggi: sabato 24/10/2026\nDettato: fede"},
		{giorno(t, "2026-10-25"), "x", "Oggi: domenica 25/10/2026\nDettato: x"},
		{giorno(t, "2026-10-26"), "x", "Oggi: lunedì 26/10/2026\nDettato: x"},
		{giorno(t, "2026-10-27"), "x", "Oggi: martedì 27/10/2026\nDettato: x"},
		{giorno(t, "2026-10-28"), "x", "Oggi: mercoledì 28/10/2026\nDettato: x"},
		{giorno(t, "2026-10-29"), "x", "Oggi: giovedì 29/10/2026\nDettato: x"},
		{giorno(t, "2027-01-05"), "a\nb", "Oggi: martedì 05/01/2027\nDettato: a\nb"},
	} {
		if got := Domanda(c.dettato, c.oggi); got != c.attesa {
			t.Errorf("%v: %q, attesa %q", c.oggi, got, c.attesa)
		}
	}
}

// D-19 e domanda F: un solo nuovo tentativo se la risposta non è valida, con un chiamante finto.
func TestEstraiUnSoloNuovoTentativo(t *testing.T) {
	errRete := errors.New("rete giù")
	buona := `{"d": "fede da lucidare"}`
	for _, c := range []struct {
		nome      string
		risposte  []string // "!" = errore del chiamante
		chiamate  int
		err       error
		descrizio string
	}{
		{"buona subito", []string{buona}, 1, nil, "fede da lucidare"},
		{"illeggibile e poi buona", []string{"non so", buona}, 2, nil, "fede da lucidare"},
		{"annidata e poi buona", []string{`{"n": {"a": 1}}`, buona}, 2, nil, "fede da lucidare"},
		{"due illeggibili", []string{"non so", "```json\n{rotto\n```", buona}, 2, ErrRispostaIllegibile, ""},
		{"errore subito: nessun nuovo tentativo", []string{"!", buona}, 1, errRete, ""},
		{"illeggibile e poi errore", []string{"non so", "!", buona}, 2, errRete, ""},
	} {
		n := 0
		chiedi := func() (string, error) {
			r := c.risposte[n]
			n++
			if r == "!" {
				return "", errRete
			}
			return r, nil
		}
		p, err := Estrai(chiedi, "Cliente Mario 347 123 4567 fede", giorno(t, "2026-10-23"))
		if n != c.chiamate {
			t.Errorf("%s: %d chiamate, attese %d", c.nome, n, c.chiamate)
		}
		if !errors.Is(err, c.err) {
			t.Errorf("%s: errore %v, atteso %v", c.nome, err, c.err)
		}
		if err == nil && (p.Descrizione != c.descrizio || p.TelefonoCliente != "3471234567") {
			t.Errorf("%s: proposta %+v", c.nome, p)
		}
		if err != nil && p != (Proposta{}) {
			t.Errorf("%s: con l'errore la proposta deve essere vuota: %+v", c.nome, p)
		}
	}
}

// Un dettato lungo pieno di numeri non rallenta la ricerca del telefono (5000 caratteri, il
// massimo del dettato).
func TestTelefonoDettatoLungo(t *testing.T) {
	testo := strings.Repeat("busta 1 ", 625)
	if got := TelefonoDalDettato(testo + "347 123 4567"); got != "3471234567" {
		t.Errorf("dettato lungo: %q", got)
	}
}
