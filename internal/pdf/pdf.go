// Package pdf è il motore dei PDF del Go (RIP-S-41 sostituita, PIANO §2.1 scelta A): sopra
// github.com/signintech/gopdf, senza Chromium. Misure in millimetri con l'origine in alto a
// sinistra, corpo dei caratteri in punti tipografici; pagine di formato qualsiasi; font DejaVu Sans
// normale e grassetto incorporati nel binario (caratteri/, con la licenza); caselle di testo che vanno
// a capo sulle parole (e, se chiesto, dentro una parola lunga) e si tagliano a un numero di righe;
// rettangoli pieni, riquadri e QR vettoriali (github.com/boombuler/barcode).
//
// Prima di consegnare un PDF si controlla con Controlla, che lo rilegge con internal/pdf/lettura: un
// PDF che non si rilegge non esce (come lo standalone, che non consegna né registra un PDF che non
// comincia con «%PDF-»).
//
// Le regole di impaginazione copiano quelle del CSS dei documenti dello standalone, quanto basta per
// il confronto di D-24: interlinea «normal» = ascesa + discesa del font, spazi come l'HTML o
// conservati come white-space: pre-wrap, a capo sulle parole, overflow-wrap: anywhere.
package pdf

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"
	"unicode"

	"github.com/boombuler/barcode/qr"
	"github.com/signintech/gopdf"

	"github.com/L0cutus/gestrip/internal/pdf/lettura"
)

//go:embed caratteri/DejaVuSans.ttf caratteri/DejaVuSans-Bold.ttf
var caratteri embed.FS

const (
	// Ascesa e Discesa di DejaVu Sans (hhea: 1901 e 483 su 2048 unità per em), in em: la linea
	// di base sta Ascesa × corpo sotto il bordo alto della riga, e l'interlinea «normal» del CSS è
	// (Ascesa + Discesa) × corpo, come in Chromium.
	Ascesa  = 1901.0 / 2048
	Discesa = 483.0 / 2048

	mmPerPt = 25.4 / 72
	ptPerMM = 72 / 25.4

	// I nomi dei due font nel PDF (/BaseFont), come li scrive Chromium.
	normale   = "DejaVuSans"
	grassetto = "DejaVuSans-Bold"
)

// Colore è un colore RGB.
type Colore struct{ R, G, B uint8 }

// Nero è il nero pieno.
var Nero = Colore{}

// Allineamento è l'allineamento delle righe dentro la casella.
type Allineamento int

const (
	Sinistra Allineamento = iota
	Centro
	Destra
)

// Spazi dice come la casella tratta spazi e a capo del testo.
type Spazi int

const (
	// Collassati: come l'HTML (white-space: normal), ogni sequenza di spazi, tabulazioni e a capo è
	// uno spazio, e niente spazi ai bordi.
	Collassati Spazi = iota
	// Conservati: come white-space: pre-wrap, gli a capo restano (anche le righe vuote) e gli spazi
	// dentro la riga; gli spazi dove la riga va a capo spariscono.
	Conservati
)

// Casella è un testo da scrivere in un riquadro largo Larghezza mm, con il bordo alto a Y mm.
type Casella struct {
	X, Y, Larghezza float64 // mm
	Testo           string
	Corpo           float64 // pt
	Grassetto       bool
	Colore          Colore
	// Interlinea è il fattore dell'interlinea sul corpo (line-height del CSS: 1,45 per il p della
	// ricevuta); 0 è «normal», cioè Ascesa + Discesa.
	Interlinea   float64
	Allinea      Allineamento
	Spazi        Spazi
	SpezzaParole bool // overflow-wrap: anywhere: una parola più larga della casella si spezza
	MaxRighe     int  // il taglio: al più tante righe (0 = senza limite)
	// Spaziatura è la letter-spacing del CSS, in pt: si aggiunge dopo ogni carattere, e conta per l'a
	// capo e per l'allineamento (0 = normale).
	Spaziatura float64
}

// Documento è un PDF in costruzione. Tutte le pagine hanno il formato dato a Nuovo.
type Documento struct {
	gp                 *gopdf.GoPdf
	larghezza, altezza float64 // mm
}

// Nuovo apre un documento con la prima pagina di larghezza × altezza mm.
func Nuovo(larghezza, altezza float64) (*Documento, error) {
	if !(larghezza > 0 && altezza > 0 && larghezza < 5000 && altezza < 5000) {
		return nil, fmt.Errorf("pdf: formato %v×%v mm non valido", larghezza, altezza)
	}
	gp := &gopdf.GoPdf{}
	gp.Start(gopdf.Config{Unit: gopdf.UnitPT, PageSize: gopdf.Rect{W: larghezza * ptPerMM, H: altezza * ptPerMM}})
	for _, f := range []struct{ file, nome string }{{"caratteri/DejaVuSans.ttf", normale}, {"caratteri/DejaVuSans-Bold.ttf", grassetto}} {
		b, err := caratteri.ReadFile(f.file)
		if err != nil {
			return nil, err
		}
		if err := gp.AddTTFFontDataWithOption(f.nome, b, gopdf.TtfOption{OnGlyphNotFoundSubstitute: sostituto}); err != nil {
			return nil, fmt.Errorf("pdf: font %s: %w", f.file, err)
		}
	}
	d := &Documento{gp: gp, larghezza: larghezza, altezza: altezza}
	d.Pagina()
	return d, nil
}

// sostituto è il carattere al posto di uno che DejaVu Sans non ha: «�», così si vede che manca
// (Chromium userebbe un altro font del sistema: è una differenza voluta, vedi la ricevuta della 042).
func sostituto(rune) rune { return unicode.ReplacementChar }

// Pagina aggiunge una pagina, che diventa quella su cui si disegna.
func (d *Documento) Pagina() { d.gp.AddPage() }

// Dimensioni sono larghezza e altezza della pagina in mm.
func (d *Documento) Dimensioni() (float64, float64) { return d.larghezza, d.altezza }

func (d *Documento) carattere(corpo float64, conGrassetto bool, spaziatura float64) error {
	f := normale
	if conGrassetto {
		f = grassetto
	}
	if !(corpo > 0 && corpo < 1000) {
		return fmt.Errorf("pdf: corpo %v non valido", corpo)
	}
	if !(spaziatura >= 0 && spaziatura < 100) {
		return fmt.Errorf("pdf: spaziatura %v non valida", spaziatura)
	}
	if err := d.gp.SetFont(f, "", corpo); err != nil {
		return err
	}
	return d.gp.SetCharSpacing(spaziatura)
}

// Larghezza è la larghezza di s in mm, con quel corpo (pt) e peso.
func (d *Documento) Larghezza(s string, corpo float64, grassetto bool) (float64, error) {
	return d.LarghezzaCasella(Casella{Testo: s, Corpo: corpo, Grassetto: grassetto})
}

// LarghezzaCasella è la larghezza del testo della casella su una riga sola, in mm, con il suo corpo,
// peso e spaziatura.
func (d *Documento) LarghezzaCasella(c Casella) (float64, error) {
	if err := d.carattere(c.Corpo, c.Grassetto, c.Spaziatura); err != nil {
		return 0, err
	}
	w, err := d.gp.MeasureTextWidth(stampabile(c.Testo))
	return w * mmPerPt, err
}

// stampabile toglie i caratteri di controllo (Chromium non li disegna) e mette uno spazio al posto
// della tabulazione; un byte che non è UTF-8 diventa «�» (strings.Map).
func stampabile(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		case r > 0xFFFF:
			return unicode.ReplacementChar // gopdf scrive solo i codici a 16 bit (piano di base)
		}
		return r
	}, s)
}

// altezzaRiga è l'interlinea della casella in mm.
func altezzaRiga(c Casella) float64 {
	f := c.Interlinea
	if f == 0 {
		f = Ascesa + Discesa
	}
	return f * c.Corpo * mmPerPt
}

// AltezzaRiga è l'altezza di una riga della casella in mm.
func AltezzaRiga(c Casella) float64 { return altezzaRiga(c) }

// Righe divide il testo della casella nelle righe che scriverebbe, senza disegnare; tagliato dice se
// MaxRighe ne ha lasciate fuori.
func (d *Documento) Righe(c Casella) (righe []string, tagliato bool, err error) {
	if err := d.carattere(c.Corpo, c.Grassetto, c.Spaziatura); err != nil {
		return nil, false, err
	}
	larga := func(s string) (bool, error) {
		w, err := d.gp.MeasureTextWidth(s)
		return w*mmPerPt > c.Larghezza+1e-9, err
	}
	testo := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(c.Testo)
	var paragrafi []string
	if c.Spazi == Conservati {
		paragrafi = strings.Split(stampabileConservato(testo), "\n")
	} else {
		paragrafi = []string{strings.Join(strings.FieldsFunc(stampabileConservato(testo), unicode.IsSpace), " ")}
	}
	for _, p := range paragrafi {
		rr, err := aCapo(p, larga, c.SpezzaParole)
		if err != nil {
			return nil, false, err
		}
		righe = append(righe, rr...)
	}
	if c.MaxRighe > 0 && len(righe) > c.MaxRighe {
		return righe[:c.MaxRighe], true, nil
	}
	return righe, false, nil
}

// stampabileConservato è stampabile ma lascia gli a capo.
func stampabileConservato(s string) string {
	parti := strings.Split(s, "\n")
	for i, p := range parti {
		parti[i] = stampabile(p)
	}
	return strings.Join(parti, "\n")
}

// aCapo divide un paragrafo in righe: a capo fra le parole, gli spazi interni restano, quelli dove si
// va a capo spariscono; una parola troppo larga si spezza fra i caratteri se spezza, altrimenti sborda.
func aCapo(p string, larga func(string) (bool, error), spezza bool) ([]string, error) {
	if p == "" {
		return []string{""}, nil
	}
	var righe []string
	riga := ""
	resto := p
	for resto != "" {
		// Il prossimo pezzo: gli spazi davanti e la parola.
		i := strings.IndexFunc(resto, func(r rune) bool { return r != ' ' })
		if i < 0 {
			if riga == "" && len(righe) == 0 {
				riga = resto // un paragrafo di soli spazi resta (pre-wrap), ma non pesa sul taglio
			}
			break
		}
		j := strings.IndexByte(resto[i:], ' ')
		if j < 0 {
			j = len(resto) - i
		}
		spazi, parola := resto[:i], resto[i:i+j]
		resto = resto[i+j:]
		prova := riga + spazi + parola
		troppo, err := larga(prova)
		if err != nil {
			return nil, err
		}
		if !troppo {
			riga = prova
			continue
		}
		if riga != "" && strings.TrimLeft(riga, " ") != "" {
			righe = append(righe, strings.TrimRight(riga, " "))
			riga = ""
			spazi = ""
		} else if testa := riga + spazi; testa != "" {
			// Solo spazi in testa al paragrafo (pre-wrap): se la parola da sola ci sta, gli spazi
			// restano sulla loro riga e la parola va intera nella successiva, come Chromium.
			sola, err := larga(parola)
			if err != nil {
				return nil, err
			}
			if !sola {
				righe = append(righe, "")
				riga = parola
				continue
			}
		}
		prova = riga + spazi + parola
		if troppo, err = larga(prova); err != nil {
			return nil, err
		}
		if !troppo || !spezza {
			riga = prova
			continue
		}
		// La parola da sola non ci sta: si spezza fra i caratteri.
		testa := riga + spazi
		for _, r := range parola {
			t := testa + string(r)
			troppo, err := larga(t)
			if err != nil {
				return nil, err
			}
			if troppo && strings.TrimLeft(testa, " ") != "" {
				righe = append(righe, testa)
				testa = string(r)
				continue
			}
			testa = t
		}
		riga = testa
	}
	righe = append(righe, strings.TrimRight(riga, " "))
	if len(righe) > 1 && righe[len(righe)-1] == "" {
		righe = righe[:len(righe)-1]
	}
	return righe, nil
}

// Testo scrive la casella sulla pagina corrente e dice quanto è alta (righe × interlinea, in mm) e
// se il taglio ha lasciato fuori delle righe.
func (d *Documento) Testo(c Casella) (altezza float64, tagliato bool, err error) {
	righe, tagliato, err := d.Righe(c)
	if err != nil {
		return 0, false, err
	}
	y := c.Y
	for _, r := range righe {
		if err := d.Riga(c, r, y); err != nil {
			return 0, false, err
		}
		y += altezzaRiga(c)
	}
	return y - c.Y, tagliato, nil
}

// Riga scrive una riga già divisa (da Righe) della casella con il bordo alto a y mm.
func (d *Documento) Riga(c Casella, riga string, y float64) error {
	if strings.TrimSpace(riga) == "" {
		return nil
	}
	if err := d.carattere(c.Corpo, c.Grassetto, c.Spaziatura); err != nil {
		return err
	}
	w, err := d.gp.MeasureTextWidth(riga)
	if err != nil {
		return err
	}
	x := c.X
	switch c.Allinea {
	case Centro:
		x += (c.Larghezza - w*mmPerPt) / 2
	case Destra:
		x += c.Larghezza - w*mmPerPt
	}
	// Mezza differenza fra l'interlinea e l'altezza del font sopra, come la «half-leading» del CSS.
	base := y + (altezzaRiga(c)-(Ascesa+Discesa)*c.Corpo*mmPerPt)/2 + Ascesa*c.Corpo*mmPerPt
	d.gp.SetTextColor(c.Colore.R, c.Colore.G, c.Colore.B)
	d.gp.SetXY(x*ptPerMM, base*ptPerMM)
	return d.gp.Text(riga)
}

// Rettangolo disegna un rettangolo pieno: angolo in alto a sinistra (x, y), larghezza l, altezza a, mm.
func (d *Documento) Rettangolo(x, y, l, a float64, c Colore) {
	d.gp.SetFillColor(c.R, c.G, c.B)
	d.gp.RectFromUpperLeftWithStyle(x*ptPerMM, y*ptPerMM, l*ptPerMM, a*ptPerMM, "F")
}

// Riquadro disegna il contorno di un rettangolo, con il tratto spesso s mm.
func (d *Documento) Riquadro(x, y, l, a, s float64, c Colore) {
	d.gp.SetLineWidth(s * ptPerMM)
	d.gp.SetStrokeColor(c.R, c.G, c.B)
	d.gp.RectFromUpperLeftWithStyle(x*ptPerMM, y*ptPerMM, l*ptPerMM, a*ptPerMM, "D")
}

// Linea disegna un segmento da (x1, y1) a (x2, y2), spesso s mm.
func (d *Documento) Linea(x1, y1, x2, y2, s float64, c Colore) {
	d.gp.SetLineWidth(s * ptPerMM)
	d.gp.SetStrokeColor(c.R, c.G, c.B)
	d.gp.Line(x1*ptPerMM, y1*ptPerMM, x2*ptPerMM, y2*ptPerMM)
}

// MargineQR sono i moduli chiari attorno al codice, dentro il lato (quiet zone di qrcode.make e della
// LabelSheet dello standalone).
const MargineQR = 4

// QR disegna il QR di contenuto nel quadrato di lato mm con l'angolo in alto a sinistra in (x, y):
// correzione M e modo byte (i byte UTF-8 del testo, senza ECI) come QRCode.create(:medium) dello
// standalone (misurato nella 043: anche «SINT-QR-0005», che starebbe in alfanumerico, esce in byte),
// così la versione, e con lei la grandezza dei moduli, è la stessa; margine di MargineQR moduli dentro
// il lato, moduli scuri come rettangoli pieni neri, uno per ogni tratto di una riga.
func (d *Documento) QR(x, y, lato float64, contenuto string) error {
	if contenuto == "" {
		return errors.New("pdf: QR senza contenuto")
	}
	if !(lato > 0) {
		return errors.New("pdf: QR senza lato")
	}
	codice, err := qr.Encode(contenuto, qr.M, qr.Unicode)
	if err != nil {
		return fmt.Errorf("pdf: QR: %w", err)
	}
	n := codice.Bounds().Dx()
	modulo := lato / float64(n+2*MargineQR)
	d.gp.SetFillColor(0, 0, 0)
	for r := range n {
		for c := 0; c < n; {
			if codice.At(c, r) != color.Black {
				c++
				continue
			}
			inizio := c
			for c < n && codice.At(c, r) == color.Black {
				c++
			}
			d.gp.RectFromUpperLeftWithStyle((x+float64(MargineQR+inizio)*modulo)*ptPerMM, (y+float64(MargineQR+r)*modulo)*ptPerMM,
				float64(c-inizio)*modulo*ptPerMM, modulo*ptPerMM, "F")
		}
	}
	return nil
}

// MaxPixelPNG è il limite dei pixel di un'immagine PNG da disegnare (16 milioni, 64 MiB decodificata
// in RGBA): oltre è un errore, come un PNG che non si decodifica. La firma della consegna sta molto
// sotto (il riquadro del telefono, al più 500 KiB).
const MaxPixelPNG = 16_000_000

// DimensioniPNG sono larghezza e altezza in pixel del PNG b, lette senza decodificarlo.
func DimensioniPNG(b []byte) (int, int, error) {
	c, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return 0, 0, fmt.Errorf("pdf: PNG: %w", err)
	}
	if c.Width <= 0 || c.Height <= 0 || int64(c.Width)*int64(c.Height) > MaxPixelPNG {
		return 0, 0, fmt.Errorf("pdf: PNG di %d×%d pixel", c.Width, c.Height)
	}
	return c.Width, c.Height, nil
}

// PNG è un'immagine PNG già decodificata per intero e riscritta in RGBA a 8 bit (così gopdf riceve
// sempre la stessa forma, qualunque sia quella del file): Larghezza e Altezza sono i suoi pixel.
type PNG struct {
	Larghezza, Altezza int
	rgba               *image.NRGBA
}

// LeggiPNG decodifica davvero il PNG b (non solo l'intestazione: un IHDR giusto con dati guasti è un
// errore), entro MaxPixelPNG.
func LeggiPNG(b []byte) (*PNG, error) {
	if _, _, err := DimensioniPNG(b); err != nil {
		return nil, err
	}
	src, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("pdf: PNG: %w", err)
	}
	rgba := image.NewNRGBA(image.Rect(0, 0, src.Bounds().Dx(), src.Bounds().Dy()))
	draw.Draw(rgba, rgba.Bounds(), src, src.Bounds().Min, draw.Src)
	return &PNG{Larghezza: rgba.Bounds().Dx(), Altezza: rgba.Bounds().Dy(), rgba: rgba}, nil
}

// Immagine disegna il PNG b nel riquadro con l'angolo in alto a sinistra in (x, y), largo l e alto a
// mm, con la sua trasparenza. Un PNG che non si decodifica (o oltre MaxPixelPNG) è un errore e non
// disegna niente.
func (d *Documento) Immagine(x, y, l, a float64, b []byte) error {
	if !(l > 0 && a > 0) {
		return errors.New("pdf: immagine senza misure")
	}
	p, err := LeggiPNG(b)
	if err != nil {
		return err
	}
	return d.ImmaginePNG(x, y, l, a, p)
}

// ImmaginePNG è Immagine per un PNG già letto con LeggiPNG.
func (d *Documento) ImmaginePNG(x, y, l, a float64, p *PNG) error {
	if !(l > 0 && a > 0) {
		return errors.New("pdf: immagine senza misure")
	}
	if p == nil || p.rgba == nil {
		return errors.New("pdf: immagine senza PNG")
	}
	return d.gp.ImageFrom(p.rgba, x*ptPerMM, y*ptPerMM, &gopdf.Rect{W: l * ptPerMM, H: a * ptPerMM})
}

// Byte chiude il documento e dà il PDF, solo se passa Controlla.
func (d *Documento) Byte() ([]byte, error) {
	var b bytes.Buffer
	if _, err := d.gp.WriteTo(&b); err != nil {
		return nil, fmt.Errorf("pdf: %w", err)
	}
	if err := Controlla(b.Bytes()); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Controlla dice se b è un PDF valido, rileggendolo: intestazione, xref, oggetti dove la xref dice,
// stream lunghi quanto dicono, almeno una pagina con il suo formato, contenuto e font che si leggono.
// Un PDF che non passa non si consegna (RIP-S-41).
func Controlla(b []byte) error {
	if _, err := lettura.Leggi(b); err != nil {
		return fmt.Errorf("pdf: il documento non si rilegge: %w", err)
	}
	return nil
}
