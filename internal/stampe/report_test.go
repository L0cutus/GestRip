package stampe

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/pdf"
	"github.com/L0cutus/gestrip/internal/pdf/lettura"
)

func report(t *testing.T, d buste.DatiReport) (*lettura.Documento, [][]string) {
	t.Helper()
	b, err := Report(d)
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
	pagine := make([][]string, len(l.Pagine))
	for i, p := range l.Pagine {
		for _, r := range p.Righe {
			pagine[i] = append(pagine[i], r.Testo)
		}
	}
	return l, pagine
}

func schedaReport(id int64, numero string) buste.SchedaReport {
	return buste.SchedaReport{ID: id, Numero: numero, NomeCliente: "Ada Rossi", Telefono: "333 111", Stato: "in lavorazione",
		ClasseStato: "in-lavorazione", DataRiparazione: "05/09/2026", Consegna: "—", Descrizione: "Anello"}
}

// inventario: RIP-S-46
// Il report è A4 (210×297 mm), una pagina per busta, nell'ordine dato. Sulla prima pagina
// l'intestazione: il negozio, «Schede riparazioni», «Generato il <data>» e «1 scheda» o «N schede». Ogni
// scheda nell'ordine di sheet/1: «#numero», il cliente, lo stato in maiuscolo (text-transform), poi le
// etichette in maiuscolo con i valori (data riparazione, consegna entro, telefono, oreficeria e
// artigiano solo se ci sono, preventivo / prezzo finale, lavorazione, note e note interne solo se ci
// sono), le date di pronta e ritiro. Niente sezione delle foto (D-01): né «📷 Foto» né «Nessuna foto
// allegata.». Il piè di ogni pagina: «Generato da <negozio> — uso interno» e «Pagina N / M».
func TestReportContenutiEOrdine(t *testing.T) {
	a := schedaReport(3, "12/26")
	a.Oreficeria, a.Artigiano, a.Importi = "Orafo <Verdi> & C.", "Cesellatore", "€ 400 → € 450,00"
	a.Note, a.NoteInterne, a.Pronta, a.Ritirata = "Pronta venerdì", "Cliente riservato", "08/09/2026", "10/09/2026"
	a.Stato, a.ClasseStato = "in attesa materiali", "in-attesa-materiali"
	b := schedaReport(1, "1/26")
	b.Artigiano, b.Importi, b.Pronta = "Cesellatore", "€ 123,45", "08/09/2026"
	l, pagine := report(t, buste.DatiReport{NomeNegozio: "Bottega Sintetica", Oggi: "11/09/2026", Schede: []buste.SchedaReport{a, b}})
	if lettura.Formato(l) != "210x297,210x297" {
		t.Fatalf("formato %q", lettura.Formato(l))
	}
	uguali(t, "prima pagina", pagine[0], []string{"Bottega Sintetica", "Schede riparazioni", "Generato il 11/09/2026", "2 schede",
		"#12/26", "Ada Rossi", "IN ATTESA MATERIALI", "DATA RIPARAZIONE", "05/09/2026", "CONSEGNA ENTRO", "—", "TELEFONO", "333 111",
		"OREFICERIA", "Orafo <Verdi> & C.", "ARTIGIANO", "Cesellatore", "PREVENTIVO / PREZZO FINALE", "€ 400 → € 450,00",
		"LAVORAZIONE", "Anello", "NOTE", "Pronta venerdì", "NOTE INTERNE", "Cliente riservato", "Pronta: 08/09/2026",
		"Ritirata: 10/09/2026", "Generato da Bottega Sintetica — uso interno", "Pagina 1 / 2"})
	uguali(t, "seconda pagina", pagine[1], []string{"#1/26", "Ada Rossi", "IN LAVORAZIONE", "DATA RIPARAZIONE", "05/09/2026",
		"CONSEGNA ENTRO", "—", "TELEFONO", "333 111", "ARTIGIANO", "Cesellatore", "PREVENTIVO / PREZZO FINALE", "€ 123,45",
		"LAVORAZIONE", "Anello", "Pronta: 08/09/2026", "Generato da Bottega Sintetica — uso interno", "Pagina 2 / 2"})
	testo := lettura.TestoNormalizzato(l)
	if strings.Contains(testo, "FOTO") || strings.Contains(testo, "Foto") || strings.Contains(testo, "Nessuna foto") ||
		strings.Contains(testo, "Avvisata") || strings.Contains(testo, "📷") {
		t.Errorf("sezione delle foto o «Avvisata»: %q", testo)
	}

	_, pagine = report(t, buste.DatiReport{NomeNegozio: "Negozio", Oggi: "11/09/2026", Schede: []buste.SchedaReport{schedaReport(1, "1/26")}})
	if len(pagine) != 1 || pagine[0][3] != "1 scheda" || pagine[0][len(pagine[0])-1] != "Pagina 1 / 1" {
		t.Errorf("una scheda: %q", pagine)
	}
}

// inventario: RIP-S-46
// Le righe facoltative stanno al loro posto anche da sole: l'artigiano senza oreficeria è nella colonna
// accanto al telefono, il prezzo da solo pure; senza pronta né ritiro niente riga delle date; le note
// lunghe vanno a capo dentro la pagina (spazi e a capo come l'HTML).
func TestReportRigheFacoltative(t *testing.T) {
	s := schedaReport(1, "1/26")
	s.Note = strings.Repeat("parola ", 60) + "\n\nfine"
	l, pagine := report(t, buste.DatiReport{NomeNegozio: "Negozio", Oggi: "11/09/2026", Schede: []buste.SchedaReport{s}})
	if len(pagine) != 1 {
		t.Fatalf("pagine: %d", len(pagine))
	}
	testo := lettura.TestoNormalizzato(l)
	atteso := "Negozio Schede riparazioni Generato il 11/09/2026 1 scheda #1/26 Ada Rossi IN LAVORAZIONE DATA RIPARAZIONE 05/09/2026 " +
		"CONSEGNA ENTRO — TELEFONO 333 111 LAVORAZIONE Anello NOTE " + strings.Repeat("parola ", 60) + "fine " +
		"Generato da Negozio — uso interno Pagina 1 / 1"
	if testo != atteso {
		t.Errorf("testo:\n%q\natteso\n%q", testo, atteso)
	}
	var note int
	for _, r := range pagine[0] {
		if strings.HasPrefix(r, "parola") {
			note++
		}
	}
	if note < 2 {
		t.Errorf("le note non vanno a capo: %q", pagine[0])
	}
	for _, p := range l.Pagine {
		for _, r := range p.Righe {
			if r.X < 9.9 || r.X > 200 {
				t.Errorf("riga fuori dai margini: %+v", r)
			}
		}
	}
}

// inventario: RIP-S-46
// Una scheda più lunga della pagina continua sulla pagina dopo (Chromium fa lo stesso): l'intestazione
// del report resta solo sulla prima, ogni pagina ha il suo piè con il totale giusto, e la scheda dopo
// comincia su una pagina nuova.
func TestReportSchedaLungaSuPiuPagine(t *testing.T) {
	lunga := schedaReport(1, "1/26")
	lunga.Descrizione = strings.Repeat("riga lunga della lavorazione\n", 400)
	_, pagine := report(t, buste.DatiReport{NomeNegozio: "Negozio", Oggi: "11/09/2026",
		Schede: []buste.SchedaReport{lunga, schedaReport(2, "2/26")}})
	if len(pagine) < 3 {
		t.Fatalf("pagine: %d", len(pagine))
	}
	n := len(pagine)
	for i, p := range pagine {
		atteso := "Pagina " + strconv.Itoa(i+1) + " / " + strconv.Itoa(n)
		if p[len(p)-1] != atteso || p[len(p)-2] != "Generato da Negozio — uso interno" {
			t.Errorf("pagina %d: piè %q", i+1, p[len(p)-2:])
		}
		if i > 0 && p[0] == "Negozio" {
			t.Errorf("pagina %d con l'intestazione", i+1)
		}
	}
	if ultima := pagine[n-1]; ultima[0] != "#2/26" {
		t.Errorf("la seconda scheda non comincia la sua pagina: %q", ultima[:3])
	}
}

// inventario: RIP-S-46
// Il report delle buste 3, 5, 6 e 2 dei dati sintetici del banco (ritiro, note con a capo e caratteri da
// scappare, preventivo e prezzo a testo, artigiano, pronta, oreficeria) ha lo stesso formato e lo stesso
// testo normalizzato del PDF che Chromium ha fatto nello standalone (fab-220,
// internal/pdf/lettura/testdata/chromium-report-3-5-6-2.pdf), tolta la sezione delle foto: «📷 FOTO
// Nessuna foto allegata.» su ogni scheda dello standalone, che nel Go non c'è (D-01).
func TestReportComeQuelloDiChromium(t *testing.T) {
	base := func(id int64, numero, data, nome, tel, stato, desc string) buste.SchedaReport {
		return buste.SchedaReport{ID: id, Numero: numero, NomeCliente: nome, Telefono: tel, Stato: stato,
			ClasseStato: strings.ReplaceAll(stato, " ", "-"), DataRiparazione: data, Consegna: "—", Descrizione: desc}
	}
	s3 := base(3, "3/26", "05/01/2026", "Cliente sintetico 3", "+390000000003", "consegnata", "Anello sintetico")
	s3.Ritirata = "09/01/2026"
	s5 := base(5, "5/26", "02/03/2026", "Cliente <Sintetico> & 5", `+39 000 <5> & "5"`, "in lavorazione", "Lavoro sintetico")
	s5.Consegna, s5.Importi = "fine mese", "€ 400/500 → € da definire"
	s5.Note, s5.NoteInterne = "Prima riga\nSeconda <b>riga</b> & \"virgolette\"", "Nota interna\n\ncon una riga vuota"
	s6 := base(6, "6/26", "01/03/2026", "Cliente Sintetico 6", "+390000000006", "pronta", "Lavoro sintetico")
	s6.Consegna, s6.Artigiano, s6.Importi, s6.Pronta = "15/04/2026", "Incastonatore Sintetico", "€ 120,50 → € 85", "03/11/2026"
	s2 := base(2, "2/26", "05/01/2026", "Cliente sintetico 2", "+390000000002", "consegnata", "Anello sintetico")
	s2.Oreficeria, s2.Pronta, s2.Ritirata = "Oreficeria Sintetica", "08/01/2026", "03/11/2026"
	b, err := os.ReadFile("../pdf/lettura/testdata/chromium-report-3-5-6-2.pdf")
	if err != nil {
		t.Fatal(err)
	}
	chromium, err := lettura.Leggi(b)
	if err != nil {
		t.Fatal(err)
	}
	l, _ := report(t, buste.DatiReport{NomeNegozio: "Negozio", Oggi: "03/11/2026", Schede: []buste.SchedaReport{s3, s5, s6, s2}})
	if lettura.Formato(l) != lettura.Formato(chromium) || lettura.Formato(l) != "210x297,210x297,210x297,210x297" {
		t.Errorf("formato %q, Chromium %q", lettura.Formato(l), lettura.Formato(chromium))
	}
	foto := "📷 FOTO Nessuna foto allegata."
	ch := lettura.TestoNormalizzato(chromium)
	if strings.Count(ch, foto) != 4 {
		t.Fatalf("la fixture non ha la sezione delle foto su ogni scheda: %q", ch)
	}
	if got, atteso := lettura.TestoNormalizzato(l), lettura.Normalizza(strings.ReplaceAll(ch, foto, " ")); got != atteso {
		t.Errorf("testo:\n%q\nChromium senza le foto:\n%q", got, atteso)
	}
}

// inventario: RIP-S-46
// Le posizioni che decidono la forma della pagina: un campo pieno (Lavorazione) comincia sempre una riga
// sua dalla prima colonna, anche dopo un numero dispari di campi a metà (il posto accanto al telefono
// resta vuoto); la seconda colonna comincia a 10 mm + metà del corpo + 7 px (Chromium: 107,02 mm); il
// piè ha la linea di base a 291,31 mm, a sinistra a 10 mm e a destra che finisce a 200 mm.
func TestReportPosizioni(t *testing.T) {
	for _, n := range []int{0, 1, 2, 3} {
		s := schedaReport(1, "1/26")
		s.Oreficeria, s.Artigiano, s.Importi = []string{"", "Orafo", "Orafo", "Orafo"}[n], []string{"", "", "Cesellatore", "Cesellatore"}[n],
			[]string{"", "", "", "€ 5,00"}[n]
		l, _ := report(t, buste.DatiReport{NomeNegozio: "Negozio", Oggi: "11/09/2026", Schede: []buste.SchedaReport{s}})
		righe := map[string]lettura.Riga{}
		for _, r := range l.Pagine[0].Righe {
			righe[r.Testo] = r
		}
		tel, lav, cons := righe["TELEFONO"], righe["LAVORAZIONE"], righe["CONSEGNA ENTRO"]
		ultima := tel
		for _, k := range []string{"OREFICERIA", "ARTIGIANO", "PREVENTIVO / PREZZO FINALE"} {
			if r, ok := righe[k]; ok && r.Y > ultima.Y {
				ultima = r
			}
		}
		if lav.X < 9.9 || lav.X > 10.2 || lav.Y <= ultima.Y+1 {
			t.Errorf("%d campi in più: LAVORAZIONE %+v, ultima riga a metà %+v", n, lav, ultima)
		}
		if cons.X < 106.8 || cons.X > 107.3 {
			t.Errorf("seconda colonna: %+v", cons)
		}
		if n > 0 {
			// L'oreficeria sta accanto al telefono, nella seconda colonna; l'artigiano va a capo.
			if r := righe["OREFICERIA"]; r.X < 106.8 || r.Y != tel.Y {
				t.Errorf("%d: accanto al telefono %+v", n, r)
			}
		}
		if r, ok := righe["ARTIGIANO"]; ok && (r.X > 10.2 || r.Y <= tel.Y) {
			t.Errorf("%d: artigiano %+v", n, r)
		}
		sinistra, destra := righe["Generato da Negozio — uso interno"], righe["Pagina 1 / 1"]
		if sinistra.X < 9.95 || sinistra.X > 10.05 || sinistra.Y < 291.26 || sinistra.Y > 291.36 || destra.Y != sinistra.Y ||
			destra.X < 185 || destra.X > 195 || sinistra.Corpo != 7 {
			t.Errorf("piè: %+v %+v", sinistra, destra)
		}
	}
}
