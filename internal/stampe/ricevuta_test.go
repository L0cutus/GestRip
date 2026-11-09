package stampe

import (
	"math"
	"os"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/pdf"
	"github.com/L0cutus/gestrip/internal/pdf/lettura"
)

func testo(s string) *string { return &s }
func cent(c int64) *int64    { return &c }

func base() buste.DatiRicevuta {
	return buste.DatiRicevuta{
		ID: 1, Numero: "1/26", Visibile: buste.NumeroMostrato("", "1/26"), NomeNegozio: "Bottega Sintetica", NomeCliente: "Ada Rossi", TelefonoCliente: "333 111",
		DataAccettazione: "2026-09-05", Descrizione: "Anello",
	}
}

func leggiRicevuta(t *testing.T, d buste.DatiRicevuta) (*lettura.Documento, []string) {
	t.Helper()
	b, err := Ricevuta(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := pdf.Controlla(b); err != nil {
		t.Fatal(err)
	}
	l, err := lettura.Leggi(b)
	if err != nil {
		t.Fatal(err)
	}
	var righe []string
	for _, p := range l.Pagine {
		for _, r := range p.Righe {
			righe = append(righe, r.Testo)
		}
	}
	return l, righe
}

func uguali(t *testing.T, cosa string, got, attese []string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(attese, "|") {
		t.Errorf("%s:\n%q\nattese:\n%q", cosa, got, attese)
	}
}

// inventario: RIP-S-42
// La ricevuta è una pagina di 80×297 mm con, nell'ordine dello standalone: nome del negozio, «Busta
// N» (il numero visibile, D-64: qui un'etichetta, «Busta 0045/26»), Cliente e Telefono, Accettata e Prevista (le etichette in maiuscolo, come text-transform:
// uppercase), Lavorazione, Note, Importi e il piè «Conservare questa ricevuta per il ritiro.».
func TestRicevutaContenutiEOrdine(t *testing.T) {
	d := base()
	d.Sottotitolo = testo("Oreficeria dal 1952")
	d.NotePubbliche = testo("Prima riga\nSeconda <b>riga</b> & \"virgolette\"")
	d.Consegna = buste.Consegna{Testo: testo("fine mese")}
	d.Preventivo = buste.Prezzo{Testo: testo("400/500")}
	d.PrezzoFinale = buste.Prezzo{Testo: testo("35")}
	d.NomeCliente = "Èva <b>x</b>"
	d.Descrizione = "Riparazione collana d'oro & pietre"
	d.Visibile = buste.NumeroMostrato("RIP-2026-0045", "1/26")
	l, righe := leggiRicevuta(t, d)
	if lettura.Formato(l) != "80x297" {
		t.Errorf("formato %q", lettura.Formato(l))
	}
	p := l.Pagine[0]
	if math.Abs(p.Larghezza-80) > 0.004 || math.Abs(p.Altezza-297) > 0.004 {
		t.Errorf("%.4f×%.4f", p.Larghezza, p.Altezza)
	}
	uguali(t, "righe", righe, []string{
		"Bottega Sintetica", "Oreficeria dal 1952", "Busta 0045/26",
		"CLIENTE", "Èva <b>x</b>", "TELEFONO", "333 111",
		"ACCETTATA", "05/09/2026", "PREVISTA", "fine mese",
		"LAVORAZIONE", "Riparazione collana d'oro & pietre",
		"NOTE", "Prima riga", "Seconda <b>riga</b> & \"virgolette\"",
		"IMPORTI", "Preventivo: 400/500", "Prezzo finale: € 35",
		"Conservare questa ricevuta per il ritiro.",
	})
	// Il nome del negozio in alto a sinistra, 18 pt; «Busta N» 20 pt; il piè 8 pt al centro.
	if r := p.Righe[0]; r.X != 0 || math.Abs(r.Corpo-18) > 0.01 || r.Y > 7 {
		t.Errorf("nome %+v", r)
	}
	if r := p.Righe[2]; math.Abs(r.Corpo-20) > 0.01 {
		t.Errorf("busta %+v", r)
	}
	piè := p.Righe[len(p.Righe)-1]
	w, err := larghezza(piè.Testo, 8)
	if err != nil || math.Abs(piè.X-(80-w)/2) > 0.05 || math.Abs(piè.Corpo-8) > 0.01 {
		t.Errorf("piè %+v (larghezza %.3f)", piè, w)
	}
	// La seconda colonna dei dati (Telefono, Prevista) comincia a 41,5 mm: due colonne uguali con 3 mm in mezzo.
	for _, r := range p.Righe {
		if (r.Testo == "TELEFONO" || r.Testo == "fine mese") && math.Abs(r.X-41.5) > 0.01 {
			t.Errorf("%s a %.3f", r.Testo, r.X)
		}
	}
	// Il filetto sotto l'intestazione (2 px, tutta la larghezza) e quelli sopra le sezioni e il piè.
	if len(p.Rettangoli) != 5 || math.Abs(p.Rettangoli[0].L-80) > 0.01 || math.Abs(p.Rettangoli[0].A-2*25.4/96) > 0.01 {
		t.Errorf("filetti %+v", p.Rettangoli)
	}
}

func larghezza(s string, corpo float64) (float64, error) {
	d, err := pdf.Nuovo(80, 297)
	if err != nil {
		return 0, err
	}
	return d.Larghezza(s, corpo, false)
}

// inventario: RIP-S-42
// Senza note e senza prezzi non ci sono quelle sezioni; senza sottotitolo, o con uno di soli spazi,
// nessuna riga; senza prevista «Da concordare»; il nome di ripiego quando il dominio lo dà.
func TestRicevutaSezioniFacoltative(t *testing.T) {
	for _, sotto := range []*string{nil, testo(""), testo("   ")} {
		d := base()
		d.NomeNegozio = "Negozio"
		d.Sottotitolo = sotto
		d.NotePubbliche = testo("")
		_, righe := leggiRicevuta(t, d)
		uguali(t, "senza", righe, []string{
			"Negozio", "Busta N. 1/26", "CLIENTE", "Ada Rossi", "TELEFONO", "333 111", "ACCETTATA", "05/09/2026",
			"PREVISTA", "Da concordare", "LAVORAZIONE", "Anello", "Conservare questa ricevuta per il ritiro.",
		})
	}
	d := base()
	d.Preventivo = buste.Prezzo{Centesimi: cent(1250)}
	d.Consegna = buste.Consegna{Data: testo("2026-09-20")}
	_, righe := leggiRicevuta(t, d)
	uguali(t, "centesimi", righe, []string{
		"Bottega Sintetica", "Busta N. 1/26", "CLIENTE", "Ada Rossi", "TELEFONO", "333 111", "ACCETTATA", "05/09/2026",
		"PREVISTA", "20/09/2026", "LAVORAZIONE", "Anello", "IMPORTI", "Preventivo: € 12,50",
		"Conservare questa ricevuta per il ritiro.",
	})
}

// inventario: RIP-S-42
// Un sottotitolo di soli spazi non lascia nemmeno lo spazio della riga: «Busta N» sta dove sta senza
// sottotitolo.
func TestRicevutaSottotitoloDiSoliSpazi(t *testing.T) {
	y := func(sotto *string) float64 {
		d := base()
		d.Sottotitolo = sotto
		l, _ := leggiRicevuta(t, d)
		for _, r := range l.Pagine[0].Righe {
			if r.Testo == "Busta N. 1/26" {
				return r.Y
			}
		}
		t.Fatal("manca «Busta 1/26»")
		return 0
	}
	senza := y(nil)
	if got := y(testo(" \t ")); math.Abs(got-senza) > 0.001 {
		t.Errorf("con un sottotitolo di spazi «Busta» a %.3f, senza a %.3f", got, senza)
	}
	if got := y(testo("Oreficeria")); got <= senza+1 {
		t.Errorf("con il sottotitolo «Busta» a %.3f, senza a %.3f", got, senza)
	}
}

// inventario: RIP-S-42
// Le note di soli spazi fanno la sezione (optional_section/2 esclude solo nil e ""), vuota.
func TestRicevutaNoteDiSoliSpazi(t *testing.T) {
	d := base()
	d.NotePubbliche = testo("   ")
	_, righe := leggiRicevuta(t, d)
	uguali(t, "note", righe, []string{
		"Bottega Sintetica", "Busta N. 1/26", "CLIENTE", "Ada Rossi", "TELEFONO", "333 111", "ACCETTATA", "05/09/2026",
		"PREVISTA", "Da concordare", "LAVORAZIONE", "Anello", "NOTE", "Conservare questa ricevuta per il ritiro.",
	})
}

// inventario: RIP-S-42
// Una lavorazione lunga va a capo nella larghezza e, se non sta in una pagina, continua su una
// seconda pagina dello stesso formato (Chromium impagina allo stesso modo); il testo non si perde.
func TestRicevutaLungaSuPiuPagine(t *testing.T) {
	d := base()
	parole := strings.Repeat("saldatura della maglia rotta ", 160)
	d.Descrizione = parole + strings.Repeat("x", 300)
	l, righe := leggiRicevuta(t, d)
	if len(l.Pagine) < 2 || lettura.Formato(l) != strings.TrimSuffix(strings.Repeat("80x297,", len(l.Pagine)), ",") {
		t.Fatalf("pagine %q", lettura.Formato(l))
	}
	for _, p := range l.Pagine {
		for _, r := range p.Righe {
			if r.Y > 297 || r.Y < 0 {
				t.Errorf("riga fuori dalla pagina: %+v", r)
			}
			if w, _ := larghezza(r.Testo, r.Corpo); r.X+w > 80+0.01 && r.Corpo == 10 {
				t.Errorf("riga oltre il bordo: %q", r.Testo)
			}
		}
	}
	tutto := lettura.Normalizza(strings.Join(righe, " "))
	if !strings.Contains(strings.ReplaceAll(tutto, " ", ""), strings.ReplaceAll(d.Descrizione, " ", "")) {
		t.Error("lavorazione persa")
	}
	if righe[len(righe)-1] != "Conservare questa ricevuta per il ritiro." {
		t.Errorf("ultima riga %q", righe[len(righe)-1])
	}
}

// inventario: RIP-S-42
// D-24 senza banco: la ricevuta del Go per i dati della busta 5/26 del banco ha lo stesso formato e
// lo stesso testo normalizzato del PDF che Chromium ha fatto per la stessa busta nello standalone
// (fab-220, salvato in internal/pdf/lettura/testdata), con la D-64 e la D-80: la 5/26 non ha
// un'etichetta (SINT-QR-0005), quindi «Busta N. 5/26» dove Chromium scrive «Busta 5/26»; «ACCETTATA»
// è 02/03/2026 dove Chromium scrive 2026-03-02.
func TestRicevutaComeQuellaDiChromium(t *testing.T) {
	b, err := os.ReadFile("../pdf/lettura/testdata/chromium-ricevuta-5.pdf")
	if err != nil {
		t.Fatal(err)
	}
	chromium, err := lettura.Leggi(b)
	if err != nil {
		t.Fatal(err)
	}
	l, _ := leggiRicevuta(t, buste.DatiRicevuta{
		ID: 5, Numero: "5/26", Visibile: buste.NumeroMostrato("SINT-QR-0005", "5/26"), NomeNegozio: "Negozio",
		NomeCliente:     "Cliente <Sintetico> & 5",
		TelefonoCliente: `+39 000 <5> & "5"`, DataAccettazione: "2026-03-02", Descrizione: "Lavoro sintetico",
		NotePubbliche: testo("Prima riga\nSeconda <b>riga</b> & \"virgolette\""),
		Consegna:      buste.Consegna{Testo: testo("fine mese")},
		Preventivo:    buste.Prezzo{Testo: testo("400/500")}, PrezzoFinale: buste.Prezzo{Testo: testo("da definire")},
	})
	if lettura.Formato(l) != lettura.Formato(chromium) {
		t.Errorf("formato %q, Chromium %q", lettura.Formato(l), lettura.Formato(chromium))
	}
	atteso := strings.Replace(lettura.TestoNormalizzato(chromium), "Busta 5/26 ", "Busta N. 5/26 ", 1) // D-64
	atteso = strings.Replace(atteso, "ACCETTATA 2026-03-02 ", "ACCETTATA 02/03/2026 ", 1)              // D-80
	if got := lettura.TestoNormalizzato(l); got != atteso {
		t.Errorf("testo:\n%q\nChromium con la D-64 e la D-80:\n%q", got, atteso)
	}
}

// inventario: RIP-S-42
// D-64 (slice 058): la ricevuta scrive il numero visibile senza «#», come «Busta 45/26» dello
// standalone: l'etichetta «0045/26» (anche di un altro anno, «0045/25»), e il numero interno «N. 45/26»
// per una busta senza etichetta o con un codice di altro formato. «ACCETTATA» e la data AAAA-MM-GG non
// cambiano (D-71 non tocca la ricevuta, Stefano 07/11/2026).
func TestRicevutaNumeroVisibile(t *testing.T) {
	for _, c := range []struct{ qr, numero, riga string }{
		{"RIP-2026-0045", "21/26", "Busta 0045/26"},
		{"RIP-2025-0045", "25/26", "Busta 0045/25"},
		{" rip-2026- 0046 ", "23/26", "Busta 0046/26"},
		{"", "35/26", "Busta N. 35/26"},
		{"SINT-ALTRO-36", "36/26", "Busta N. 36/26"},
		{"RIP-2026-45", "37/26", "Busta N. 37/26"},
		{"", "10000/25", "Busta N. 10000/25"},
	} {
		d := base()
		d.Numero, d.Visibile = c.numero, buste.NumeroMostrato(c.qr, c.numero)
		_, righe := leggiRicevuta(t, d)
		uguali(t, c.qr, righe[:4], []string{"Bottega Sintetica", c.riga, "CLIENTE", "Ada Rossi"})
		if strings.Contains(strings.Join(righe, " "), "#") {
			t.Errorf("%q: «#» sulla ricevuta: %q", c.qr, righe)
		}
		if righe[6] != "ACCETTATA" || righe[7] != "05/09/2026" {
			t.Errorf("%q: data %q %q", c.qr, righe[6], righe[7])
		}
	}
}
