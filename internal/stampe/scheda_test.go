package stampe

import (
	"bytes"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/pdf"
	"github.com/L0cutus/gestrip/internal/pdf/lettura"
)

func scheda(t *testing.T, s buste.DatiSchedaPDF) (*lettura.Documento, []string) {
	t.Helper()
	b, err := SchedaPDF(s)
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

func pngFirma(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		img.Set(x, h/2, color.Black)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func schedaBase() buste.DatiSchedaPDF {
	return buste.DatiSchedaPDF{ID: 1, Numero: "12/26", NomeNegozio: "Bottega Sintetica", DataAccettazione: "05/09/2026",
		Consegna: "20/09/2026", NomeCliente: "Ada Rossi", TelefonoCliente: "333 111", Descrizione: "Anello"}
}

// inventario: RIP-S-45
// La scheda è una pagina di 100×230 mm con, nell'ordine dello standalone: il nome del negozio, il
// badge dell'oreficeria (maiuscolo), «N° BUSTA» e il numero, le etichette in maiuscolo con i loro
// valori (accettazione, consegna prevista, cliente, telefono, artigiano assegnato), «OGGETTI E
// LAVORAZIONI» e la descrizione con i suoi a capo, «Note:» e «Note interne:» con il testo, le righe
// «Firma artigiano» e «Firma cliente», «PREVENTIVO» e il suo valore, e per la compro oro la ricevuta
// di deposito con il D.M. 26/2010.
func TestSchedaContenutiEOrdine(t *testing.T) {
	s := schedaBase()
	s.Oreficeria, s.Artigiano = "Orafo <Verdi> & C.", "Cesellatore"
	s.Descrizione = "riga uno\nriga <due> d'oro & pietre"
	s.NotePubbliche, s.NoteInterne, s.Preventivo = "Pronta venerdì", "Cliente <difficile>", "400/500 €"
	s.CompraOro = true
	l, righe := scheda(t, s)
	if lettura.Formato(l) != "100x230" {
		t.Fatalf("formato %q", lettura.Formato(l))
	}
	uguali(t, "righe", righe[:26], []string{"Bottega Sintetica", "ORAFO <VERDI> & C.", "N° BUSTA", "12/26",
		"DATA RICEZIONE", "05/09/2026", "DATA CONSEGNA PREVISTA", "20/09/2026", "CLIENTE", "Ada Rossi", "TELEFONO", "333 111",
		"ARTIGIANO ASSEGNATO", "Cesellatore", "OGGETTI E LAVORAZIONI", "riga uno", "riga <due> d'oro & pietre",
		"Note:", " Pronta venerdì", "Note interne:", " Cliente <difficile>", "Firma artigiano", "Firma cliente", "PREVENTIVO", "400/500 €",
		"Ricevuta di deposito per valutazione"})
	// La ricevuta di deposito, con il pezzo in grassetto in linea (D-24: conta il testo, non dove va a
	// capo).
	if got := lettura.TestoNormalizzato(l); !strings.HasSuffix(got, "Ricevuta di deposito per valutazione Gli oggetti consegnati sono "+
		"in deposito esclusivamente ai fini della valutazione tecnica e dell'eventuale proposta di acquisto. La presente non costituisce "+
		"contratto di compravendita. Il cliente può richiedere la restituzione degli oggetti in qualsiasi momento prima della firma della "+
		"scheda di compravendita ai sensi del D.M. 26/2010 (Compro Oro — registro Pubblica Sicurezza). La proposta di valore sarà "+
		"comunicata via messaggio al numero indicato.") {
		t.Errorf("deposito: %q", got)
	}
	if strings.Contains(strings.Join(righe, " "), "Consegna — Firma del cliente") {
		t.Error("blocco della firma senza firma")
	}
	// Senza le voci facoltative: niente badge, artigiano, note, ricevuta di deposito; la consegna
	// vuota lascia solo l'etichetta, il preventivo vuoto solo «PREVENTIVO».
	_, righe = scheda(t, buste.DatiSchedaPDF{ID: 1, Numero: "12/26", NomeNegozio: "Negozio", DataAccettazione: "05/09/2026",
		NomeCliente: "Ada Rossi", TelefonoCliente: "333 111", Descrizione: ""})
	uguali(t, "vuota", righe, []string{"Negozio", "N° BUSTA", "12/26", "DATA RICEZIONE", "05/09/2026", "DATA CONSEGNA PREVISTA",
		"CLIENTE", "Ada Rossi", "TELEFONO", "333 111", "OGGETTI E LAVORAZIONI", "Firma artigiano", "Firma cliente", "PREVENTIVO"})
}

// inventario: RIP-S-45
// La busta firmata ha il blocco «Consegna — Firma del cliente»: la dichiarazione, la riga «Firmata …»
// e la firma come immagine, alta al più 2,5 cm (un px del CSS per pixel, poi in proporzione). Un PNG
// che non si decodifica stampa al suo posto il testo alternativo «Firma», come Chromium per
// un'immagine rotta (misurato sulla firma sintetica della busta 3/26 del banco).
func TestSchedaFirma(t *testing.T) {
	s := schedaBase()
	s.Firma = &buste.FirmaScheda{Riga: "Firmata il 03/01/2026 00:04 — ritirata da Mario Rossi", PNG: pngFirma(t, 400, 150)}
	l, _ := scheda(t, s)
	if got := lettura.TestoNormalizzato(l); !strings.HasSuffix(got, "PREVENTIVO Consegna — Firma del cliente Dichiaro di aver ritirato "+
		"l'oggetto sopra indicato e di accettarne lo stato. Firmata il 03/01/2026 00:04 — ritirata da Mario Rossi") {
		t.Errorf("%q", got)
	}
	im := l.Pagine[0].Immagini
	// 150 px sono 39,7 mm: oltre i 25 mm si riduce in proporzione, 400 px → 66,7 mm.
	if len(im) != 1 || !vicinoMM(im[0].A, 25, 0.01) || !vicinoMM(im[0].L, 400.0/150*25, 0.01) || !vicinoMM(im[0].X, 5, 0.01) {
		t.Errorf("immagine %+v", im)
	}
	s.Firma.PNG = pngFirma(t, 120, 40) // 31,75 × 10,58 mm: resta com'è
	l, _ = scheda(t, s)
	if im := l.Pagine[0].Immagini; len(im) != 1 || !vicinoMM(im[0].L, 120*px, 0.01) || !vicinoMM(im[0].A, 40*px, 0.01) {
		t.Errorf("piccola %+v", im)
	}
	// Il PDF porta l'immagine con i suoi pixel, non ricampionata.
	b, err := SchedaPDF(s)
	if err != nil || !bytes.Contains(b, []byte("/Width 120")) || !bytes.Contains(b, []byte("/Height 40")) {
		t.Errorf("pixel dell'immagine: %v", err)
	}
	s.Firma.PNG = []byte("\x89PNG\r\n\x1a\nsynthetic-signature")
	l, righe := scheda(t, s)
	if len(l.Pagine[0].Immagini) != 0 || righe[len(righe)-1] != "Firma" {
		t.Errorf("PNG rotto: %q %+v", righe, l.Pagine[0].Immagini)
	}
}

// inventario: RIP-S-45
// Una descrizione molto lunga continua su altre pagine di 100×230 mm: niente testo perso.
func TestSchedaLunga(t *testing.T) {
	s := schedaBase()
	s.Descrizione = strings.Repeat("Una riga della lavorazione da fare con cura.\n", 60)
	s.NoteInterne = strings.Repeat("nota ", 200)
	l, _ := scheda(t, s)
	if len(l.Pagine) < 2 || !strings.HasPrefix(lettura.Formato(l), "100x230,100x230") {
		t.Fatalf("%q", lettura.Formato(l))
	}
	if got := lettura.TestoNormalizzato(l); strings.Count(got, "Una riga della lavorazione da fare con cura.") != 60 || strings.Count(got, "nota") != 200 ||
		!strings.HasSuffix(got, "Firma artigiano Firma cliente PREVENTIVO") {
		t.Errorf("testo perso: %d", strings.Count(got, "Una riga"))
	}
	for _, p := range l.Pagine {
		for _, r := range p.Righe {
			if r.Y > 230 || r.Y < 5 {
				t.Errorf("riga fuori: %+v", r)
			}
		}
	}
}

// inventario: RIP-S-45
// D-24 senza banco: la scheda del Go per i dati delle buste 2/26 (oreficeria), 3/26 (firma importata,
// con un PNG finto), 5/26 (note, note interne, preventivo a testo), 6/26 (artigiano) e 7/26 (compro
// oro, preventivo in centesimi) ha lo stesso formato e lo stesso testo normalizzato del PDF che
// Chromium ha fatto nello standalone (fab-220, internal/pdf/lettura/testdata).
func TestSchedaComeQuellaDiChromium(t *testing.T) {
	base := func(id int64, numero, data, nome, tel, desc string) buste.DatiSchedaPDF {
		return buste.DatiSchedaPDF{ID: id, Numero: numero, NomeNegozio: "Negozio", DataAccettazione: data, NomeCliente: nome,
			TelefonoCliente: tel, Descrizione: desc}
	}
	s2 := base(2, "2/26", "05/01/2026", "Cliente sintetico 2", "+390000000002", "Anello sintetico")
	s2.Oreficeria = "Oreficeria Sintetica"
	s3 := base(3, "3/26", "05/01/2026", "Cliente sintetico 3", "+390000000003", "Anello sintetico")
	s3.Firma = &buste.FirmaScheda{Riga: "Firmata il 09/01/2026 11:00 — ritirata da Cliente sintetico 3", PNG: []byte("\x89PNG\r\n\x1a\nsynthetic-signature")}
	s5 := base(5, "5/26", "02/03/2026", "Cliente <Sintetico> & 5", `+39 000 <5> & "5"`, "Lavoro sintetico")
	s5.Consegna, s5.Preventivo = "fine mese", "400/500 €"
	s5.NotePubbliche, s5.NoteInterne = "Prima riga\nSeconda <b>riga</b> & \"virgolette\"", "Nota interna\n\ncon una riga vuota"
	s6 := base(6, "6/26", "01/03/2026", "Cliente Sintetico 6", "+390000000006", "Lavoro sintetico")
	s6.Consegna, s6.Artigiano, s6.Preventivo = "15/04/2026", "Incastonatore Sintetico", "120,50 €"
	s7 := base(7, "7/26", "02/03/2026", "Cliente Sintetico 7", "+390000000007", "Bracciale da valutare")
	s7.Preventivo, s7.CompraOro = "€ 50,00", true
	for file, s := range map[string]buste.DatiSchedaPDF{
		"chromium-scheda-2.pdf": s2, "chromium-scheda-3.pdf": s3, "chromium-scheda-5.pdf": s5, "chromium-scheda-6.pdf": s6, "chromium-scheda-7.pdf": s7,
	} {
		b, err := os.ReadFile("../pdf/lettura/testdata/" + file)
		if err != nil {
			t.Fatal(err)
		}
		chromium, err := lettura.Leggi(b)
		if err != nil {
			t.Fatal(err)
		}
		l, _ := scheda(t, s)
		if lettura.Formato(l) != lettura.Formato(chromium) {
			t.Errorf("%s: formato %q, Chromium %q", file, lettura.Formato(l), lettura.Formato(chromium))
		}
		// D-71: «Data accettazione» dello standalone è «Data ricezione» nel Go.
		atteso := strings.ReplaceAll(lettura.TestoNormalizzato(chromium), "DATA ACCETTAZIONE", "DATA RICEZIONE")
		if got := lettura.TestoNormalizzato(l); got != atteso {
			t.Errorf("%s: testo:\n%q\nChromium:\n%q", file, got, atteso)
		}
	}
}

// inventario: RIP-S-45
// Una firma con l'intestazione PNG giusta e i dati guasti (lo standalone accetta una firma guardando
// solo i byte magici, quindi può arrivare dall'import) non rompe la scheda: si decodifica davvero
// prima di scegliere, e al posto dell'immagine va l'alt «Firma», come Chromium.
func TestSchedaFirmaConDatiGuasti(t *testing.T) {
	buona := pngFirma(t, 300, 100)
	// IHDR intatto (8 byte di firma + 25 dell'IHDR), poi un IDAT che non è zlib, poi IEND.
	idat := []byte("IDAT" + "non-zlib!")
	guasta := append([]byte{}, buona[:33]...)
	guasta = append(guasta, 0, 0, 0, 9)
	guasta = append(guasta, idat...)
	guasta = append(guasta, crcPNG(idat)...)
	guasta = append(guasta, 0, 0, 0, 0, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82)
	if w, h, err := pdf.DimensioniPNG(guasta); err != nil || w != 300 || h != 100 {
		t.Fatalf("l'intestazione deve essere buona: %d×%d %v", w, h, err)
	}
	s := schedaBase()
	s.Firma = &buste.FirmaScheda{Riga: "Firmata il 03/01/2026 00:04", PNG: guasta}
	l, righe := scheda(t, s)
	if len(l.Pagine[0].Immagini) != 0 || righe[len(righe)-1] != "Firma" {
		t.Errorf("%q %+v", righe, l.Pagine[0].Immagini)
	}
}

func crcPNG(b []byte) []byte {
	c := crc32.ChecksumIEEE(b)
	return []byte{byte(c >> 24), byte(c >> 16), byte(c >> 8), byte(c)}
}
