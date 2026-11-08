package stampe

import (
	"strconv"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/pdf"
)

// Il report multi-scheda A4 (RIP-S-46, GET /repairs/report.pdf), da RepairReport.render_html/3,
// print_to_pdf/1 e footer_template/1 dello standalone a go-baseline (templates/scheda_pdf.html della
// sorgente). A4 verticale con i margini 12 mm sopra, 15 sotto e 10 ai lati: il corpo è largo 190 mm,
// 9 pt, colore #1a1a1a. Le misure del CSS:
//   - .global-header, solo sulla prima pagina: a sinistra il negozio 15 pt grassetto (font-weight
//     900), 0,04 em fra le lettere, interlinea 1,1, sotto la riga vuota di .shop-sub (2 px); a destra
//     «Schede riparazioni» 13 pt grassetto #b8960c e .report-meta 7 pt #888 con interlinea 1,5, 3 px
//     sopra: «Generato il <data>» e «1 scheda» o «N schede»; le due parti centrate in altezza; sotto
//     6 px, il filetto di 2 px #b8960c e 10 px;
//   - .scheda, una per pagina (page-break-after: always, tranne l'ultima): .scheda-header con
//     «#numero» 18 pt grassetto #b8960c interlinea 1 (il numero visibile, D-64, slice 058: «#0045/26»
//     o «N. 45/26»), il cliente 13 pt grassetto 2 px sotto, a destra
//     il badge dello stato (8 pt grassetto maiuscolo, 0,05 em fra le lettere, padding 3 px × 8 px, 4 px
//     sopra, i colori della sua classe); sotto 5 px, il filetto di 2 px #b8960c e 8 px;
//   - .fields: griglia di due colonne uguali, 14 px fra le colonne e 5 px fra le righe, che si riempie
//     in ordine; un campo .full (lavorazione, note, note interne) prende tutta la riga e comincia la
//     sua. Ogni campo: l'etichetta 6,5 pt maiuscola #999 con 0,06 em fra le lettere, 1 px sotto; il
//     valore 8,5 pt con interlinea 1,4 (importi in grassetto #b8960c, note interne #555 in corsivo:
//     il corsivo non c'è fra i font incorporati, D-24 non conta la resa grafica); 3 px sotto e il
//     filetto di 0,5 px #e8e8e8. 7 px sotto la griglia;
//   - .timestamps: 7 pt #888, interlinea 1,6, «Pronta: …» e «Ritirata: …» in linea con 12 px dopo
//     ognuna; 8 px sotto;
//   - la sezione delle foto («📷 Foto», «Nessuna foto allegata.») nel Go non c'è (D-01);
//   - il piè di ogni pagina (footer_template/1, nel margine sotto): «Generato da <negozio> — uso
//     interno» a sinistra e «Pagina N / M» a destra, 7 pt #999, a 10 mm dai bordi, con la linea di base
//     dove la mette Chromium (291,31 mm, misurato).
// Una scheda più lunga della pagina continua sulla pagina dopo (Chromium fa lo stesso); i campi della
// stessa riga della griglia finiscono insieme.

const (
	larghezzaA4    = 210.0
	altezzaA4      = 297.0
	margineSuA4    = 12.0
	margineGiuA4   = 15.0
	margineLatoA4  = 10.0
	corpoA4        = larghezzaA4 - 2*margineLatoA4
	fondoA4        = altezzaA4 - margineGiuA4
	basePieA4      = 291.31 // la linea di base del piè di Chromium, misurata nel PDF dello standalone
	titoloReport   = "Schede riparazioni"
	colonneReport  = 14 * px
	righeReport    = 5 * px
	interlineaVal  = 1.4
	corpoEtichetta = 6.5
	corpoValore    = 8.5
)

var (
	grigio999 = pdf.Colore{R: 0x99, G: 0x99, B: 0x99}
	grigioE8  = pdf.Colore{R: 0xe8, G: 0xe8, B: 0xe8}
)

// coloriStato sono le classi .stato-<classe> del foglio: sfondo, testo e bordo del badge. Uno stato
// senza classe (valutata, chiusa, …) non ha sfondo né bordo e ha il colore del corpo.
var coloriStato = map[string][3]pdf.Colore{
	"pronta":              {{R: 0xe8, G: 0xf8, B: 0xf0}, {R: 0x1e, G: 0x84, B: 0x49}, {R: 0x1e, G: 0x84, B: 0x49}},
	"consegnata":          {{R: 0xf0, G: 0xf0, B: 0xf0}, {R: 0x55, G: 0x55, B: 0x55}, {R: 0xaa, G: 0xaa, B: 0xaa}},
	"in-lavorazione":      {{R: 0xea, G: 0xf4, B: 0xfb}, {R: 0x1a, G: 0x6f, B: 0xa8}, {R: 0x1a, G: 0x6f, B: 0xa8}},
	"preventivo":          {{R: 0xfe, G: 0xf5, B: 0xe7}, {R: 0xb7, G: 0x77, B: 0x0d}, {R: 0xb7, G: 0x77, B: 0x0d}},
	"in-attesa-materiali": {{R: 0xff, G: 0xfb, B: 0xe6}, {R: 0x7d, G: 0x66, B: 0x08}, {R: 0xc9, G: 0xa5, B: 0x0a}},
	"annullata":           {{R: 0xfd, G: 0xed, B: 0xec}, {R: 0x92, G: 0x2b, B: 0x21}, {R: 0x92, G: 0x2b, B: 0x21}},
}

// flussoReport è il flusso delle pagine del report: come flussoScheda, i disegni si rimandano alla
// fine (prima gli sfondi, poi il resto), così il piè sa quante pagine ci sono.
type flussoReport struct {
	d      *pdf.Documento
	y      float64
	pagina int
	opere  []opera
}

func (f *flussoReport) disegna(strato int, fn func() error) {
	f.opere = append(f.opere, opera{f.pagina, strato, fn})
}

func (f *flussoReport) salta() {
	f.pagina++
	f.y = margineSuA4
}

// metti fa posto ad alto mm: se non ci stanno sulla pagina, si va alla pagina dopo.
func (f *flussoReport) metti(alto float64) {
	if f.y+alto > fondoA4+1e-9 && f.y > margineSuA4 {
		f.salta()
	}
}

func (f *flussoReport) riga(c pdf.Casella, testo string) {
	h := pdf.AltezzaRiga(c)
	f.metti(h)
	y := f.y
	f.disegna(1, func() error { return f.d.Riga(c, testo, y) })
	f.y += h
}

func (f *flussoReport) testo(c pdf.Casella) error {
	righe, _, err := f.d.Righe(c)
	if err != nil {
		return err
	}
	for _, r := range righe {
		f.riga(c, r)
	}
	return nil
}

// Report è il PDF del report, già passato da pdf.Controlla.
func Report(r buste.DatiReport) ([]byte, error) {
	d, err := pdf.Nuovo(larghezzaA4, altezzaA4)
	if err != nil {
		return nil, err
	}
	f := &flussoReport{d: d, y: margineSuA4}
	if err := intestazioneReport(f, r); err != nil {
		return nil, err
	}
	for i, s := range r.Schede {
		if i > 0 {
			f.salta()
		}
		if err := schedaDelReport(f, s); err != nil {
			return nil, err
		}
	}
	totale := strconv.Itoa(f.pagina + 1)
	for p := 0; p <= f.pagina; p++ {
		if p > 0 {
			d.Pagina()
		}
		for strato := range 2 {
			for _, o := range f.opere {
				if o.pagina == p && o.strato == strato {
					if err := o.f(); err != nil {
						return nil, err
					}
				}
			}
		}
		if err := pieReport(d, r.NomeNegozio, strconv.Itoa(p+1)+" / "+totale); err != nil {
			return nil, err
		}
	}
	return d.Byte()
}

func pieReport(d *pdf.Documento, negozio, pagine string) error {
	c := pdf.Casella{X: margineLatoA4, Larghezza: corpoA4, Corpo: 7, Colore: grigio999}
	y := basePieA4 - pdf.Ascesa*7*mmPerPt
	sinistra := c
	sinistra.Testo = "Generato da " + negozio + " — uso interno"
	if err := d.Riga(sinistra, sinistra.Testo, y); err != nil {
		return err
	}
	destra := c
	destra.Allinea, destra.Testo = pdf.Destra, "Pagina "+pagine
	return d.Riga(destra, destra.Testo, y)
}

func conteggioSchede(n int) string {
	if n == 1 {
		return "1 scheda"
	}
	return strconv.Itoa(n) + " schede"
}

func intestazioneReport(f *flussoReport, r buste.DatiReport) error {
	d, x0 := f.d, margineLatoA4
	titolo := pdf.Casella{Larghezza: corpoA4, Testo: titoloReport, Corpo: 13, Grassetto: true, Colore: oro, Allinea: pdf.Destra}
	meta := pdf.Casella{Larghezza: corpoA4, Corpo: 7, Interlinea: 1.5, Colore: grigio888, Allinea: pdf.Destra}
	metaRighe := []string{"Generato il " + r.Oggi, conteggioSchede(len(r.Schede))}
	destra, err := d.LarghezzaCasella(titolo)
	if err != nil {
		return err
	}
	for _, t := range metaRighe {
		c := meta
		c.Testo = t
		w, err := d.LarghezzaCasella(c)
		if err != nil {
			return err
		}
		destra = max(destra, w)
	}
	negozio := pdf.Casella{X: x0, Larghezza: max(corpoA4-destra, 1), Testo: r.NomeNegozio, Corpo: 15, Grassetto: true,
		Interlinea: 1.1, Spaziatura: 0.04 * 15, Colore: inchiostroScheda}
	righe, _, err := d.Righe(negozio)
	if err != nil {
		return err
	}
	alteSinistra := float64(len(righe))*pdf.AltezzaRiga(negozio) + 2*px
	alteDestra := pdf.AltezzaRiga(titolo) + 3*px + 2*pdf.AltezzaRiga(meta)
	alto := max(alteSinistra, alteDestra)
	top := f.y
	f.y = top + (alto-alteSinistra)/2
	for _, t := range righe {
		f.riga(negozio, t)
	}
	f.y = top + (alto-alteDestra)/2
	titolo.X = x0
	f.riga(titolo, titolo.Testo)
	f.y += 3 * px
	meta.X = x0
	for _, t := range metaRighe {
		f.riga(meta, t)
	}
	f.y = top + alto + 6*px
	yf := f.y
	f.disegna(1, func() error { d.Rettangolo(x0, yf, corpoA4, 2*px, oro); return nil })
	f.y += 2*px + 10*px
	return nil
}

// campoReport è un .field della griglia.
type campoReport struct {
	etichetta, valore string
	pieno             bool // .field.full
	importo, interne  bool
}

func campiReport(s buste.SchedaReport) []campoReport {
	campi := []campoReport{{etichetta: "Data ricezione", valore: s.DataRiparazione}, {etichetta: "Consegna entro", valore: s.Consegna},
		{etichetta: "Telefono", valore: s.Telefono}}
	if s.Oreficeria != "" {
		campi = append(campi, campoReport{etichetta: "Oreficeria", valore: s.Oreficeria})
	}
	if s.Artigiano != "" {
		campi = append(campi, campoReport{etichetta: "Artigiano", valore: s.Artigiano})
	}
	if s.Importi != "" {
		campi = append(campi, campoReport{etichetta: "Preventivo / Prezzo finale", valore: s.Importi, importo: true})
	}
	campi = append(campi, campoReport{etichetta: "Lavorazione", valore: s.Descrizione, pieno: true})
	if s.Note != "" {
		campi = append(campi, campoReport{etichetta: "Note", valore: s.Note, pieno: true})
	}
	if s.NoteInterne != "" {
		campi = append(campi, campoReport{etichetta: "Note interne", valore: s.NoteInterne, pieno: true, interne: true})
	}
	return campi
}

// righeGriglia mette i campi nelle righe della griglia: due per riga, un campo pieno da solo nella
// sua (il posto lasciato accanto a un campo prima resta vuoto, come l'auto-placement del CSS).
func righeGriglia(campi []campoReport) [][]campoReport {
	var righe [][]campoReport
	var aperta []campoReport
	for _, c := range campi {
		if c.pieno {
			if aperta != nil {
				righe, aperta = append(righe, aperta), nil
			}
			righe = append(righe, []campoReport{c})
			continue
		}
		aperta = append(aperta, c)
		if len(aperta) == 2 {
			righe, aperta = append(righe, aperta), nil
		}
	}
	if aperta != nil {
		righe = append(righe, aperta)
	}
	return righe
}

func schedaDelReport(f *flussoReport, s buste.SchedaReport) error {
	d, x0 := f.d, margineLatoA4

	// Il badge dello stato: a destra, 4 px sotto la cima.
	colori, conClasse := coloriStato[s.ClasseStato]
	testoBadge := inchiostroScheda
	if conClasse {
		testoBadge = colori[1]
	}
	badge := pdf.Casella{Larghezza: corpoA4, Testo: maiuscolo(s.Stato), Corpo: 8, Grassetto: true, Spaziatura: 0.05 * 8, Colore: testoBadge}
	wb, err := d.LarghezzaCasella(badge)
	if err != nil {
		return err
	}
	lb, ab := wb+16*px, pdf.AltezzaRiga(badge)+6*px
	num := pdf.Casella{X: x0, Larghezza: max(corpoA4-lb, 1), Testo: s.Visibile.Titolo(), Corpo: 18, Grassetto: true, Interlinea: 1, Colore: oro}
	cliente := pdf.Casella{X: x0, Larghezza: num.Larghezza, Testo: s.NomeCliente, Corpo: 13, Grassetto: true, Colore: inchiostroScheda}
	f.metti(pdf.AltezzaRiga(num) + 2*px + pdf.AltezzaRiga(cliente))
	top, pagina := f.y, f.pagina
	if err := f.testo(num); err != nil {
		return err
	}
	f.y += 2 * px
	if err := f.testo(cliente); err != nil {
		return err
	}
	fine, fineP := f.y, f.pagina
	f.y, f.pagina = top+4*px, pagina
	bx, by := x0+corpoA4-lb, f.y
	if conClasse {
		f.disegna(0, func() error {
			d.Rettangolo(bx, by, lb, ab, colori[0])
			d.Riquadro(bx+px/2, by+px/2, lb-px, ab-px, px, colori[2])
			return nil
		})
	}
	badge.X, badge.Larghezza = bx+8*px, wb
	yb := by + 3*px
	f.disegna(1, func() error { return d.Riga(badge, badge.Testo, yb) })
	if fineP == pagina {
		f.y = max(fine, by+ab)
	} else {
		f.y, f.pagina = fine, fineP
	}
	f.y += 5 * px
	yf := f.y
	f.disegna(1, func() error { d.Rettangolo(x0, yf, corpoA4, 2*px, oro); return nil })
	f.y += 2*px + 8*px

	if err := grigliaReport(f, campiReport(s)); err != nil {
		return err
	}
	f.y += 7 * px

	var date []string
	if s.Pronta != "" {
		date = append(date, "Pronta: "+s.Pronta)
	}
	if s.Ritirata != "" {
		date = append(date, "Ritirata: "+s.Ritirata)
	}
	if len(date) > 0 {
		c := pdf.Casella{Corpo: 7, Interlinea: 1.6, Colore: grigio888, Larghezza: corpoA4}
		h := pdf.AltezzaRiga(c)
		f.metti(h)
		y, x := f.y, x0
		for _, t := range date {
			v := c
			v.X, v.Testo = x, t
			w, err := d.LarghezzaCasella(v)
			if err != nil {
				return err
			}
			f.disegna(1, func() error { return d.Riga(v, v.Testo, y) })
			x += w + 12*px
		}
		f.y += h + 8*px
	}
	return nil
}

func grigliaReport(f *flussoReport, campi []campoReport) error {
	d, x0 := f.d, margineLatoA4
	colonna := (corpoA4 - colonneReport) / 2
	for i, riga := range righeGriglia(campi) {
		if i > 0 {
			f.y += righeReport
		}
		top, pagina := f.y, f.pagina
		// La prima riga di ogni campo (etichetta e prima riga del valore) sta sulla stessa pagina.
		f.metti(pdf.AltezzaRiga(pdf.Casella{Corpo: corpoEtichetta}) + px + pdf.AltezzaRiga(pdf.Casella{Corpo: corpoValore, Interlinea: interlineaVal}))
		if f.pagina != pagina {
			top, pagina = f.y, f.pagina
		}
		fine, fineP := top, pagina
		type bordo struct{ x, l float64 }
		var bordi []bordo
		for j, c := range riga {
			l := colonna
			if c.pieno {
				l = corpoA4
			}
			x := x0 + float64(j)*(colonna+colonneReport)
			f.y, f.pagina = top, pagina
			lbl := pdf.Casella{X: x, Larghezza: l, Testo: maiuscolo(c.etichetta), Corpo: corpoEtichetta, Spaziatura: 0.06 * corpoEtichetta,
				Colore: grigio999}
			if err := f.testo(lbl); err != nil {
				return err
			}
			f.y += px
			val := pdf.Casella{X: x, Larghezza: l, Testo: c.valore, Corpo: corpoValore, Interlinea: interlineaVal, Colore: inchiostroScheda}
			switch {
			case c.importo:
				val.Grassetto, val.Colore = true, oro
			case c.interne:
				val.Colore = grigio555
			}
			if err := f.testo(val); err != nil {
				return err
			}
			if f.pagina > fineP || f.pagina == fineP && f.y > fine {
				fine, fineP = f.y, f.pagina
			}
			bordi = append(bordi, bordo{x, l})
		}
		f.y, f.pagina = fine+3*px, fineP
		yb := f.y
		for _, b := range bordi {
			f.disegna(1, func() error { d.Rettangolo(b.x, yb, b.l, 0.5*px, grigioE8); return nil })
		}
		f.y += 0.5 * px
	}
	return nil
}
