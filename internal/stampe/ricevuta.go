// Package stampe sono i documenti PDF del Go, impaginati con il motore internal/pdf: ognuno copia
// contenuti, ordine, testi e misure del documento HTML che lo standalone stampa con Chromium (D-24:
// stesso formato, stesso testo estratto; la resa grafica fine non conta).
package stampe

import (
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/pdf"
)

// La ricevuta della busta (RIP-S-42), da Receipt.render_html/2 e render_pdf/1 dello standalone a
// go-baseline. La pagina è 80×297 mm (paperWidth 3,1496", paperHeight 11,6929"). ChromicPDF passa
// anche i margini (0,2" e 0,16"), ma il foglio dice @page { margin: 0 } e Chromium usa quello: nel PDF
// dello standalone il contenuto parte dal bordo (misurato sul banco il 03/11/2026, filetto
// dell'intestazione largo 303 px = tutta la pagina). Qui lo stesso. Le misure del CSS:
//   - body 10 pt, colore #17140f; header con filetto in basso di 2 px e 4 mm sopra; h1 18 pt;
//     .subtitle #666 9 pt, 1 mm sopra; .bag 20 pt grassetto, 2 mm sopra;
//   - dl in due colonne uguali con 3 mm in mezzo, 5 mm sopra e sotto; dt #666 8 pt maiuscolo; dd
//     grassetto, 1 mm sopra;
//   - section con filetto in alto di 1 px #bbb, 4 mm sopra e 4 mm dentro; h2 9 pt maiuscolo, 2 mm
//     sotto; p con interlinea 1,45, spazi conservati (pre-wrap) e parole spezzabili (anywhere);
//   - footer con filetto in alto, 6 mm sopra e 3 mm dentro, #666 8 pt al centro.
// Il numero è quello visibile (D-64, slice 058): «Busta 0045/26» con un'etichetta, «Busta N. 45/26»
// senza, dove lo standalone scrive «Busta 45/26»; «ACCETTATA» e la data AAAA-MM-GG restano come nello
// standalone (Stefano, 07/11/2026: la D-71 non tocca la ricevuta del cliente).
// I margini verticali fra dl e section si fondono come nel CSS (vale il più grande, 5 mm).

const (
	larghezzaRicevuta = 80.0
	altezzaRicevuta   = 297.0
	px                = 25.4 / 96 // un px del CSS in mm
)

var (
	inchiostro = pdf.Colore{R: 0x17, G: 0x14, B: 0x0f}
	grigio     = pdf.Colore{R: 0x66, G: 0x66, B: 0x66}
	filetto    = pdf.Colore{R: 0xbb, G: 0xbb, B: 0xbb}
)

// flusso scrive dall'alto in basso e passa alla pagina dopo quando la riga non ci sta più.
type flusso struct {
	d *pdf.Documento
	y float64
}

func (f *flusso) aCapoPagina(alto float64) {
	if f.y+alto > altezzaRicevuta+1e-9 && f.y > 0 {
		f.d.Pagina()
		f.y = 0
	}
}

func (f *flusso) spazio(mm float64) {
	f.y += mm
	if f.y > altezzaRicevuta {
		f.d.Pagina()
		f.y = 0
	}
}

func (f *flusso) testo(c pdf.Casella) error {
	righe, _, err := f.d.Righe(c)
	if err != nil {
		return err
	}
	h := pdf.AltezzaRiga(c)
	for _, r := range righe {
		f.aCapoPagina(h)
		if err := f.d.Riga(c, r, f.y); err != nil {
			return err
		}
		f.y += h
	}
	return nil
}

func (f *flusso) filetto(alto float64, c pdf.Colore) {
	f.aCapoPagina(alto)
	f.d.Rettangolo(0, f.y, larghezzaRicevuta, alto, c)
	f.y += alto
}

func casella(testo string, corpo float64, grassetto bool, c pdf.Colore) pdf.Casella {
	return pdf.Casella{Larghezza: larghezzaRicevuta, Testo: testo, Corpo: corpo, Grassetto: grassetto, Colore: c}
}

// Ricevuta è il PDF della ricevuta della busta, già passato da pdf.Controlla.
func Ricevuta(r buste.DatiRicevuta) ([]byte, error) {
	d, err := pdf.Nuovo(larghezzaRicevuta, altezzaRicevuta)
	if err != nil {
		return nil, err
	}
	f := &flusso{d: d}
	if err := f.testo(casella(r.NomeNegozio, 18, true, inchiostro)); err != nil {
		return nil, err
	}
	if s, ok := r.RigaDelSottotitolo(); ok {
		f.spazio(1)
		if err := f.testo(casella(s, 9, false, grigio)); err != nil {
			return nil, err
		}
	}
	f.spazio(2)
	if err := f.testo(casella("Busta "+r.Visibile.Testo, 20, true, inchiostro)); err != nil {
		return nil, err
	}
	f.spazio(4)
	f.filetto(2*px, inchiostro)
	f.spazio(5)
	for _, riga := range [][2][2]string{
		{{"CLIENTE", r.NomeCliente}, {"TELEFONO", r.TelefonoCliente}},
		{{"ACCETTATA", r.DataAccettazione}, {"PREVISTA", r.Prevista()}},
	} {
		if err := f.coppia(riga); err != nil {
			return nil, err
		}
	}
	f.spazio(5 - 3) // il gap dell'ultima riga non c'è: 5 mm sotto la dl, poi il margine della section si fonde
	if err := f.sezione("LAVORAZIONE", r.Descrizione); err != nil {
		return nil, err
	}
	if r.NotePubbliche != nil && *r.NotePubbliche != "" {
		f.spazio(4)
		if err := f.sezione("NOTE", *r.NotePubbliche); err != nil {
			return nil, err
		}
	}
	if importi := r.Importi(); len(importi) > 0 {
		f.spazio(4)
		testo := importi[0]
		for _, i := range importi[1:] {
			testo += "\n" + i
		}
		if err := f.sezione("IMPORTI", testo); err != nil {
			return nil, err
		}
	}
	f.spazio(6)
	f.filetto(px, filetto)
	f.spazio(3)
	piè := casella("Conservare questa ricevuta per il ritiro.", 8, false, grigio)
	piè.Allinea = pdf.Centro
	if err := f.testo(piè); err != nil {
		return nil, err
	}
	return d.Byte()
}

// coppia è una riga della dl: due celle (dt, dd) affiancate, alta quanto la più alta, più il gap.
func (f *flusso) coppia(celle [2][2]string) error {
	colonna := (larghezzaRicevuta - 3) / 2
	type cella struct {
		dt, dd   pdf.Casella
		rdt, rdd []string
	}
	var cc [2]cella
	alto := 0.0
	for i, c := range celle {
		x := float64(i) * (colonna + 3)
		dt := pdf.Casella{X: x, Larghezza: colonna, Testo: c[0], Corpo: 8, Colore: grigio}
		dd := pdf.Casella{X: x, Larghezza: colonna, Testo: c[1], Corpo: 10, Grassetto: true, Colore: inchiostro}
		rdt, _, err := f.d.Righe(dt)
		if err != nil {
			return err
		}
		rdd, _, err := f.d.Righe(dd)
		if err != nil {
			return err
		}
		cc[i] = cella{dt, dd, rdt, rdd}
		if h := float64(len(rdt))*pdf.AltezzaRiga(dt) + 1 + float64(len(rdd))*pdf.AltezzaRiga(dd); h > alto {
			alto = h
		}
	}
	f.aCapoPagina(alto)
	for _, c := range cc {
		y := f.y
		for _, r := range c.rdt {
			if err := f.d.Riga(c.dt, r, y); err != nil {
				return err
			}
			y += pdf.AltezzaRiga(c.dt)
		}
		y++
		for _, r := range c.rdd {
			if err := f.d.Riga(c.dd, r, y); err != nil {
				return err
			}
			y += pdf.AltezzaRiga(c.dd)
		}
	}
	f.y += alto
	f.spazio(3)
	return nil
}

// sezione è una section: filetto, titolo e paragrafo.
func (f *flusso) sezione(titolo, testo string) error {
	f.filetto(px, filetto)
	f.spazio(4)
	if err := f.testo(casella(titolo, 9, true, inchiostro)); err != nil {
		return err
	}
	f.spazio(2)
	p := casella(testo, 10, false, inchiostro)
	p.Interlinea, p.Spazi, p.SpezzaParole = 1.45, pdf.Conservati, true
	return f.testo(p)
}
