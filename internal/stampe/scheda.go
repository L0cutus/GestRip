package stampe

import (
	"unicode"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/pdf"
)

// La scheda PDF della busta (RIP-S-45, «⧉ Anteprima PDF»), da RepairSheet.render_html/2 e
// render_pdf/1 dello standalone a go-baseline (templates/stampa_pdf.html della sorgente). La pagina è
// 100×230 mm con 5 mm di margine (@page { size: 100mm 230mm; margin: 5mm }), il corpo largo 90 mm,
// 9 pt, colore #1a1a1a. Le misure del CSS:
//   - .header: il nome del negozio 15 pt grassetto con interlinea 1,1, sotto la riga vuota del
//     sottotitolo (1 px) e il badge dell'oreficeria (7 pt grassetto maiuscolo, 0,05 em fra le lettere,
//     bianco su #1a5c8a, padding 1 px × 5 px, 3 px sopra); a destra «N° BUSTA» 7 pt #666 e il numero
//     14 pt grassetto #b8960c, a destra e senza a capo; sotto 4 px, il filetto di 1,5 px #b8960c e 6 px;
//   - .dati: griglia di due colonne uguali con 10 px fra le colonne e 3 px fra le righe; ogni campo
//     ha l'etichetta 6 pt grassetto maiuscolo con 0,07 em fra le lettere, #888, e il valore 9 pt
//     grassetto alto almeno 12 px, 2 px di padding e il filetto di 0,5 px #ddd; l'artigiano occupa
//     tutta la riga; 5 px sotto;
//   - «Oggetti e lavorazioni» come le etichette, 2 px sotto; il riquadro della descrizione ha il bordo
//     di 1 px #ddd, padding 4 px × 5 px, alto almeno 28 mm, 9 pt con interlinea 1,4, spazi conservati
//     (pre-wrap), 4 px sotto;
//   - .note-box 7,5 pt #555, l'etichetta in grassetto in linea con il testo, 4 px sotto; le note
//     interne con lo sfondo #fffbe6;
//   - .firma-row: 6 px sopra, flex allineato in basso con 8 px fra le voci: «Firma artigiano» e
//     «Firma cliente» (6,5 pt #666, 2 px sotto, la riga di 14 px con il filetto di 1 px #333) e il
//     preventivo (largo almeno 60 px; etichetta come i campi, a destra; valore 11 pt grassetto
//     #b8960c, largo almeno 55 px, filetto di 1 px #333);
//   - compro oro: 0,8 cm sopra, padding 0,4 × 0,5 cm, bordo 1 px #b8860b, sfondo #fffaeb, 9 pt con
//     interlinea 1,4, #5c4400, il titolo in grassetto con 0,15 cm sotto;
//   - firma: 0,8 cm sopra, 0,3 cm di padding e il filetto di 1 px #333 sopra, 9 pt con interlinea 1,4;
//     il titolo in grassetto, la dichiarazione con 0,15 cm sopra e sotto, «Firmata …», l'immagine alta
//     al più 2,5 cm con 0,2 cm sopra e il filetto di 1 px in basso.
// Testo più lungo della pagina: si continua sulle pagine dopo, dello stesso formato (Chromium fa lo
// stesso); i riquadri si dividono fra le pagine.

const (
	larghezzaScheda = 100.0
	altezzaScheda   = 230.0
	margineScheda   = 5.0
	corpoScheda     = larghezzaScheda - 2*margineScheda
	cimaScheda      = margineScheda
	fondoScheda     = altezzaScheda - margineScheda
)

var (
	inchiostroScheda = pdf.Colore{R: 0x1a, G: 0x1a, B: 0x1a}
	oro              = pdf.Colore{R: 0xb8, G: 0x96, B: 0x0c}
	grigio888        = pdf.Colore{R: 0x88, G: 0x88, B: 0x88}
	grigio555        = pdf.Colore{R: 0x55, G: 0x55, B: 0x55}
	grigioDDD        = pdf.Colore{R: 0xdd, G: 0xdd, B: 0xdd}
	grigio333        = pdf.Colore{R: 0x33, G: 0x33, B: 0x33}
	bluBadge         = pdf.Colore{R: 0x1a, G: 0x5c, B: 0x8a}
	sfondoInterne    = pdf.Colore{R: 0xff, G: 0xfb, B: 0xe6}
	bordoDeposito    = pdf.Colore{R: 0xb8, G: 0x86, B: 0x0b}
	sfondoDeposito   = pdf.Colore{R: 0xff, G: 0xfa, B: 0xeb}
	testoDeposito    = pdf.Colore{R: 0x5c, G: 0x44, B: 0x00}
)

// Il testo della ricevuta di deposito della compro oro (deposit_receipt/1), con il pezzo in grassetto.
const (
	titoloDeposito = "Ricevuta di deposito per valutazione"
	deposito1      = "Gli oggetti consegnati sono in deposito esclusivamente ai fini della valutazione tecnica e dell'eventuale proposta di acquisto. "
	deposito2      = "La presente non costituisce contratto di compravendita."
	deposito3      = " Il cliente può richiedere la restituzione degli oggetti in qualsiasi momento prima della firma della scheda di compravendita ai sensi del D.M. 26/2010 (Compro Oro — registro Pubblica Sicurezza). La proposta di valore sarà comunicata via messaggio al numero indicato."
	// altFirma è l'alt dell'immagine della firma: Chromium lo scrive al posto di un'immagine che non
	// si decodifica, dopo l'icona di 16 px.
	altFirma = "Firma"
)

// opera è un disegno rimandato: le pagine si disegnano alla fine, prima gli sfondi (strato 0) poi il
// resto, così uno sfondo non copre il testo che gli sta sopra.
type opera struct {
	pagina, strato int
	f              func() error
}

// scatola è un riquadro aperto che si divide fra le pagine: sfondo e bordo si disegnano per pezzi.
type scatola struct {
	x, l, inizio float64
	sfondo       *pdf.Colore
	bordo        *pdf.Colore
	pagina       int // la pagina dove si è aperta
}

type flussoScheda struct {
	d      *pdf.Documento
	y      float64
	pagina int
	opere  []opera
	aperte []*scatola
}

func (f *flussoScheda) disegna(strato int, fn func() error) {
	f.opere = append(f.opere, opera{f.pagina, strato, fn})
}

// pezzo chiude il pezzo della scatola s sulla pagina corrente, fino a fine.
func (f *flussoScheda) pezzo(s *scatola, fine float64, ultimo bool) {
	x, l, y, a := s.x, s.l, s.inizio, fine-s.inizio
	if a <= 0 {
		return
	}
	if s.sfondo != nil {
		c := *s.sfondo
		f.disegna(0, func() error { f.d.Rettangolo(x, y, l, a, c); return nil })
	}
	if s.bordo != nil {
		c := *s.bordo
		f.disegna(0, func() error {
			f.d.Rettangolo(x, y, l, px, c)
			f.d.Rettangolo(x, y, px, a, c)
			f.d.Rettangolo(x+l-px, y, px, a, c)
			if ultimo {
				f.d.Rettangolo(x, y+a-px, l, px, c)
			}
			return nil
		})
	}
}

func (f *flussoScheda) salta() {
	for _, s := range f.aperte {
		f.pezzo(s, fondoScheda, false)
		s.inizio = cimaScheda
	}
	f.pagina++
	f.y = cimaScheda
}

// metti fa posto ad alto mm: se non ci stanno sulla pagina, si va alla pagina dopo.
func (f *flussoScheda) metti(alto float64) {
	if f.y+alto > fondoScheda+1e-9 && f.y > cimaScheda {
		f.salta()
	}
}

func (f *flussoScheda) spazio(mm float64) {
	f.y += mm
	if f.y > fondoScheda {
		f.salta()
	}
}

func (f *flussoScheda) apri(s *scatola) *scatola {
	s.inizio, s.pagina = f.y, f.pagina
	f.aperte = append(f.aperte, s)
	return s
}

func (f *flussoScheda) chiudi(s *scatola) {
	f.pezzo(s, f.y, true)
	for i, a := range f.aperte {
		if a == s {
			f.aperte = append(f.aperte[:i], f.aperte[i+1:]...)
			break
		}
	}
}

// riga scrive una riga già divisa della casella sulla y corrente.
func (f *flussoScheda) riga(c pdf.Casella, testo string) {
	h := pdf.AltezzaRiga(c)
	f.metti(h)
	y := f.y
	f.disegna(1, func() error { return f.d.Riga(c, testo, y) })
	f.y += h
}

func (f *flussoScheda) testo(c pdf.Casella) error {
	righe, _, err := f.d.Righe(c)
	if err != nil {
		return err
	}
	for _, r := range righe {
		f.riga(c, r)
	}
	return nil
}

// fine disegna le pagine: per ognuna prima gli sfondi, poi il resto, nell'ordine in cui sono venuti.
func (f *flussoScheda) fine() error {
	for p := 0; p <= f.pagina; p++ {
		if p > 0 {
			f.d.Pagina()
		}
		for strato := range 2 {
			for _, o := range f.opere {
				if o.pagina == p && o.strato == strato {
					if err := o.f(); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// pezzoDiTesto è un pezzo di un paragrafo con il suo peso: <strong> dentro il testo.
type pezzoDiTesto struct {
	testo     string
	grassetto bool
}

// paragrafo scrive pezzi di testo con pesi diversi come un paragrafo HTML (spazi collassati, a capo
// fra le parole), largo l mm, dalla x data: ogni riga è uno o più pezzi sulla stessa linea di base.
func (f *flussoScheda) paragrafo(pezzi []pezzoDiTesto, x, l, corpo, interlinea float64, colore pdf.Colore) error {
	type parola struct {
		testo     string
		grassetto bool
		spazio    bool // preceduta da uno spazio
	}
	var parole []parola
	spazio := false
	for _, p := range pezzi {
		for i, r := 0, []rune(p.testo); i < len(r); {
			if unicode.IsSpace(r[i]) {
				spazio = len(parole) > 0
				i++
				continue
			}
			j := i
			for j < len(r) && !unicode.IsSpace(r[j]) {
				j++
			}
			parole = append(parole, parola{string(r[i:j]), p.grassetto, spazio})
			spazio = false
			i = j
		}
	}
	base := pdf.Casella{Corpo: corpo, Interlinea: interlinea, Colore: colore}
	larga := func(s string, g bool) (float64, error) {
		c := base
		c.Testo, c.Grassetto = s, g
		return f.d.LarghezzaCasella(c)
	}
	type tratto struct {
		testo     string
		grassetto bool
		x         float64
	}
	var righe [][]tratto
	var riga []tratto
	w := 0.0
	for _, p := range parole {
		t := p.testo
		if p.spazio && len(riga) > 0 {
			t = " " + t
		}
		tw, err := larga(t, p.grassetto)
		if err != nil {
			return err
		}
		if len(riga) > 0 && w+tw > l+1e-9 {
			righe = append(righe, riga)
			riga, w, t = nil, 0, p.testo
			if tw, err = larga(t, p.grassetto); err != nil {
				return err
			}
		}
		if n := len(riga); n > 0 && riga[n-1].grassetto == p.grassetto {
			riga[n-1].testo += t
		} else {
			riga = append(riga, tratto{t, p.grassetto, x + w})
		}
		w += tw
	}
	if len(riga) > 0 {
		righe = append(righe, riga)
	}
	h := pdf.AltezzaRiga(base)
	for _, r := range righe {
		f.metti(h)
		y := f.y
		for _, t := range r {
			c := base
			c.X, c.Larghezza, c.Grassetto = t.x, l, t.grassetto
			testo := t.testo
			f.disegna(1, func() error { return f.d.Riga(c, testo, y) })
		}
		f.y += h
	}
	return nil
}

// SchedaPDF è il PDF della scheda della busta, già passato da pdf.Controlla.
func SchedaPDF(s buste.DatiSchedaPDF) ([]byte, error) {
	d, err := pdf.Nuovo(larghezzaScheda, altezzaScheda)
	if err != nil {
		return nil, err
	}
	f := &flussoScheda{d: d, y: cimaScheda}
	x0 := margineScheda
	if err := intestazione(f, s); err != nil {
		return nil, err
	}
	if err := dati(f, s); err != nil {
		return nil, err
	}
	f.spazio(5 * px)

	// Oggetti e lavorazioni.
	if err := f.testo(etichettaCampo("Oggetti e lavorazioni", corpoScheda)); err != nil {
		return nil, err
	}
	f.spazio(2 * px)
	box := f.apri(&scatola{x: x0, l: corpoScheda, bordo: &grigioDDD})
	inizio := f.y
	f.spazio(px + 4*px)
	// Senza overflow-wrap, come il CSS: una parola più larga del riquadro esce dal bordo e resta intera.
	desc := pdf.Casella{X: x0 + px + 5*px, Larghezza: corpoScheda - 2*px - 10*px, Testo: s.Descrizione, Corpo: 9, Interlinea: 1.4,
		Spazi: pdf.Conservati, Colore: inchiostroScheda}
	if s.Descrizione != "" {
		if err := f.testo(desc); err != nil {
			return nil, err
		}
	}
	f.spazio(4*px + px)
	if f.pagina == box.pagina && f.y-inizio < 28 { // min-height: 28mm, se il riquadro sta su una pagina
		f.spazio(28 - (f.y - inizio))
	}
	f.chiudi(box)
	f.spazio(4 * px)

	// Note e note interne.
	for _, n := range []struct {
		etichetta, testo string
		sfondo           *pdf.Colore
	}{{"Note", s.NotePubbliche, nil}, {"Note interne", s.NoteInterne, &sfondoInterne}} {
		if n.testo == "" {
			continue
		}
		var sc *scatola
		if n.sfondo != nil {
			sc = f.apri(&scatola{x: x0, l: corpoScheda, sfondo: n.sfondo})
		}
		if err := f.paragrafo([]pezzoDiTesto{{n.etichetta + ":", true}, {" " + n.testo, false}}, x0, corpoScheda, 7.5, 0, grigio555); err != nil {
			return nil, err
		}
		if sc != nil {
			f.chiudi(sc)
		}
		f.spazio(4 * px)
	}

	if err := firme(f, s); err != nil {
		return nil, err
	}
	if s.CompraOro {
		if err := ricevutaDiDeposito(f); err != nil {
			return nil, err
		}
	}
	if s.Firma != nil {
		if err := bloccoFirma(f, *s.Firma); err != nil {
			return nil, err
		}
	}
	if err := f.fine(); err != nil {
		return nil, err
	}
	return d.Byte()
}

// etichettaCampo è .campo-lbl e .desc-lbl: 6 pt grassetto maiuscolo, 0,07 em fra le lettere, #888.
func etichettaCampo(testo string, l float64) pdf.Casella {
	return pdf.Casella{Larghezza: l, Testo: maiuscolo(testo), Corpo: 6, Grassetto: true, Spaziatura: 0.07 * 6, Colore: grigio888}
}

func intestazione(f *flussoScheda, s buste.DatiSchedaPDF) error {
	d, x0, y0 := f.d, margineScheda, cimaScheda
	nb := pdf.Casella{Larghezza: corpoScheda, Testo: "N° BUSTA", Corpo: 7, Colore: pdf.Colore{R: 0x66, G: 0x66, B: 0x66}, Allinea: pdf.Destra, X: x0}
	num := pdf.Casella{Larghezza: corpoScheda, Testo: s.Numero, Corpo: 14, Grassetto: true, Colore: oro, Allinea: pdf.Destra, X: x0}
	wnb, err := d.LarghezzaCasella(nb)
	if err != nil {
		return err
	}
	wnum, err := d.LarghezzaCasella(num)
	if err != nil {
		return err
	}
	destra := max(wnb, wnum)
	logo := pdf.Casella{X: x0, Larghezza: max(corpoScheda-destra, 1), Testo: s.NomeNegozio, Corpo: 15, Grassetto: true, Interlinea: 1.1,
		Colore: inchiostroScheda}
	righe, _, err := d.Righe(logo)
	if err != nil {
		return err
	}
	for _, r := range righe {
		f.riga(logo, r)
	}
	y := f.y + px // .logo-sub vuoto: solo il suo margine
	if s.Oreficeria != "" {
		// Il badge è un inline-block nella riga del div (9 pt): sopra la linea di base il margine,
		// il padding e l'ascesa del suo testo; sotto il resto.
		badge := pdf.Casella{Larghezza: corpoScheda, Testo: maiuscolo(s.Oreficeria), Corpo: 7, Grassetto: true, Spaziatura: 0.05 * 7,
			Colore: bianco}
		w, err := d.LarghezzaCasella(badge)
		if err != nil {
			return err
		}
		c7, c9 := 7*mmPerPt, 9*mmPerPt
		sopra := 3*px + px + pdf.Ascesa*c7
		sotto := px + pdf.Discesa*c7
		base := y + max(pdf.Ascesa*c9, sopra)
		alto := (pdf.Ascesa+pdf.Discesa)*c7 + 2*px
		bx, by := x0, base-pdf.Ascesa*c7-px
		f.disegna(0, func() error { d.Rettangolo(bx, by, w+10*px, alto, bluBadge); return nil })
		badge.X = x0 + 5*px
		ty := base - pdf.Ascesa*c7
		f.disegna(1, func() error { return d.Riga(badge, badge.Testo, ty) })
		y = base + max(pdf.Discesa*c9, sotto)
	}
	f.y = y0
	f.riga(nb, nb.Testo)
	f.riga(num, num.Testo)
	f.y = max(y, f.y) + 4*px
	yf := f.y
	f.disegna(1, func() error { d.Rettangolo(x0, yf, corpoScheda, 1.5*px, oro); return nil })
	f.y += 1.5*px + 6*px
	return nil
}

// cella è un .campo della griglia: l'etichetta e il valore.
type cella struct{ etichetta, valore string }

func dati(f *flussoScheda, s buste.DatiSchedaPDF) error {
	d, x0 := f.d, margineScheda
	colonna := (corpoScheda - 10*px) / 2
	file := [][]cella{
		{{"Data ricezione", s.DataAccettazione}, {"Data consegna prevista", s.Consegna}},
		{{"Cliente", s.NomeCliente}, {"Telefono", s.TelefonoCliente}},
	}
	if s.Artigiano != "" {
		file = append(file, []cella{{"Artigiano assegnato", s.Artigiano}})
	}
	for i, riga := range file {
		if i > 0 {
			f.spazio(3 * px)
		}
		l := colonna
		if len(riga) == 1 {
			l = corpoScheda
		}
		type pronta struct {
			lbl, val   pdf.Casella
			rlbl, rval []string
		}
		var pp []pronta
		alto := 0.0
		for j, c := range riga {
			x := x0 + float64(j)*(colonna+10*px)
			lbl := etichettaCampo(c.etichetta, l)
			lbl.X = x
			val := pdf.Casella{X: x, Larghezza: l, Testo: c.valore, Corpo: 9, Grassetto: true, Colore: inchiostroScheda}
			rlbl, _, err := d.Righe(lbl)
			if err != nil {
				return err
			}
			var rval []string
			if c.valore != "" {
				if rval, _, err = d.Righe(val); err != nil {
					return err
				}
			}
			h := float64(len(rlbl))*pdf.AltezzaRiga(lbl) + max(12*px, float64(len(rval))*pdf.AltezzaRiga(val)) + 2*px + 0.5*px
			alto = max(alto, h)
			pp = append(pp, pronta{lbl, val, rlbl, rval})
		}
		f.metti(alto)
		top := f.y
		for _, p := range pp {
			f.y = top
			for _, r := range p.rlbl {
				f.riga(p.lbl, r)
			}
			for _, r := range p.rval {
				f.riga(p.val, r)
			}
			x, lw, yb := p.lbl.X, p.lbl.Larghezza, top+alto-0.5*px
			f.disegna(1, func() error { d.Rettangolo(x, yb, lw, 0.5*px, grigioDDD); return nil })
		}
		f.y = top + alto
	}
	return nil
}

// firme è .firma-row: le due righe da firmare e il preventivo, allineate in basso.
func firme(f *flussoScheda, s buste.DatiSchedaPDF) error {
	d, x0 := f.d, margineScheda
	f.spazio(6 * px)
	val := pdf.Casella{Larghezza: corpoScheda, Testo: s.Preventivo, Corpo: 11, Grassetto: true, Colore: oro, Allinea: pdf.Destra}
	wv := 55 * px
	if s.Preventivo != "" {
		w, err := d.LarghezzaCasella(val)
		if err != nil {
			return err
		}
		wv = max(wv, w)
	}
	lp := max(60*px, wv)
	voce := (corpoScheda - lp - 2*8*px) / 2
	lbl := pdf.Casella{Corpo: 6.5, Colore: pdf.Colore{R: 0x66, G: 0x66, B: 0x66}, Larghezza: voce}
	hl := pdf.AltezzaRiga(lbl)
	altoFirma := hl + 2*px + 14*px + px
	c11, c9 := 11*mmPerPt, 9*mmPerPt
	lblPrev := etichettaCampo("Preventivo", lp)
	hp := pdf.AltezzaRiga(lblPrev)
	sopra := max(pdf.Ascesa*c11, pdf.Ascesa*c9)
	sotto := max(pdf.Discesa*c11+2*px, pdf.Discesa*c9)
	altoPrev := hp + sopra + sotto
	alto := max(altoFirma, altoPrev)
	f.metti(alto)
	top := f.y
	for i, t := range []string{"Firma artigiano", "Firma cliente"} {
		c := lbl
		c.X, c.Testo = x0+float64(i)*(voce+8*px), t
		f.y = top + alto - altoFirma
		f.riga(c, t)
		yb := f.y + 2*px + 14*px
		x := c.X
		f.disegna(1, func() error { d.Rettangolo(x, yb, voce, px, grigio333); return nil })
	}
	xp := x0 + corpoScheda - lp
	lblPrev.X, lblPrev.Allinea = xp, pdf.Destra
	f.y = top + alto - altoPrev
	f.riga(lblPrev, lblPrev.Testo)
	base := f.y + sopra
	if s.Preventivo != "" {
		val.X, val.Larghezza = xp, lp
		yv := base - pdf.Ascesa*c11
		f.disegna(1, func() error { return d.Riga(val, val.Testo, yv) })
	}
	yb := base + pdf.Discesa*c11 + px
	xv := x0 + corpoScheda - wv
	f.disegna(1, func() error { d.Rettangolo(xv, yb, wv, px, grigio333); return nil })
	f.y = top + alto
	return nil
}

func ricevutaDiDeposito(f *flussoScheda) error {
	x0 := margineScheda
	f.spazio(8)
	sc := f.apri(&scatola{x: x0, l: corpoScheda, sfondo: &sfondoDeposito, bordo: &bordoDeposito})
	f.spazio(px + 4)
	x, l := x0+px+5, corpoScheda-2*px-10
	titolo := pdf.Casella{X: x, Larghezza: l, Testo: titoloDeposito, Corpo: 9, Grassetto: true, Interlinea: 1.4, Colore: testoDeposito}
	if err := f.testo(titolo); err != nil {
		return err
	}
	f.spazio(1.5)
	if err := f.paragrafo([]pezzoDiTesto{{deposito1, false}, {deposito2, true}, {deposito3, false}}, x, l, 9, 1.4, testoDeposito); err != nil {
		return err
	}
	f.spazio(4 + px)
	f.chiudi(sc)
	return nil
}

func bloccoFirma(f *flussoScheda, firma buste.FirmaScheda) error {
	d, x0 := f.d, margineScheda
	f.spazio(8)
	f.metti(px + 3 + 3*pdf.AltezzaRiga(pdf.Casella{Corpo: 9, Interlinea: 1.4}))
	yf := f.y
	f.disegna(1, func() error { d.Rettangolo(x0, yf, corpoScheda, px, grigio333); return nil })
	f.spazio(px + 3)
	c := pdf.Casella{X: x0, Larghezza: corpoScheda, Corpo: 9, Interlinea: 1.4, Colore: inchiostroScheda}
	titolo := c
	titolo.Testo, titolo.Grassetto = "Consegna — Firma del cliente", true
	if err := f.testo(titolo); err != nil {
		return err
	}
	f.spazio(1.5)
	dich := c
	dich.Testo = buste.DichiarazioneFirma
	if err := f.testo(dich); err != nil {
		return err
	}
	f.spazio(1.5)
	riga := c
	riga.Testo = firma.Riga
	if err := f.testo(riga); err != nil {
		return err
	}
	// L'immagine è in linea: un px del CSS per pixel, alta al più 2,5 cm (in proporzione), mai più
	// larga della pagina; sopra 0,2 cm, sotto il filetto di 1 px.
	h := pdf.AltezzaRiga(c)
	// Si decodifica davvero adesso, non solo l'intestazione: un IHDR giusto con i dati guasti (la
	// firma si accetta guardando solo i byte magici, e può arrivare dall'import) non deve rompere la
	// scheda quando si disegna.
	img, err := pdf.LeggiPNG(firma.PNG)
	if err != nil {
		// Chromium, per un'immagine che non si decodifica, scrive l'alt dopo l'icona di 16 px.
		f.spazio(2)
		alt := c
		alt.X, alt.Testo = x0+16*px, altFirma
		f.riga(alt, altFirma)
		return nil
	}
	w, a := img.Larghezza, img.Altezza
	lw, la := float64(w)*px, float64(a)*px
	if la > 25 {
		lw, la = lw*25/la, 25
	}
	if lw > corpoScheda {
		lw, la = corpoScheda, la*corpoScheda/lw
	}
	f.spazio(2)
	f.metti(la + px)
	y := f.y
	f.disegna(1, func() error {
		if err := d.ImmaginePNG(x0, y, lw, la, img); err != nil {
			return err
		}
		d.Rettangolo(x0, y+la, lw, px, pdf.Nero)
		return nil
	})
	f.y += max(la+px, h)
	return nil
}
