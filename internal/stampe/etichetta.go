package stampe

import (
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/pdf"
)

// L'etichetta busta (RIP-S-43), da BagLabel.document/1 e page/3 dello standalone a go-baseline
// (templates/etichetta_artigiano_pdf.html della sorgente). La pagina è 76×127 mm con 3 mm di margine
// (@page { size: 76mm 127mm; margin: 3mm }); il blocco .lab è largo 70 mm. Le misure del CSS:
//   - .header: flex, centrato in verticale, 2 mm fra testo e QR; il QR è un quadrato di 22 mm (l'SVG
//     con il margine di 4 moduli dentro); sotto, 1 mm di padding, filetto di 2 px, 1,5 mm di margine;
//   - .artigiano 9 pt maiuscolo con 0,5 pt fra le lettere; .busta 22 pt grassetto, interlinea 1,0,
//     0,5 mm sopra; il testo è centrato nei 46 mm accanto al QR;
//   - .meta 9 pt, una riga con «Consegna:» a sinistra e «Scad.:» a destra (space-between), 1,5 mm sotto;
//     nel Go «Consegna:» si chiama «All'artigiano:» (D-73, Stefano 06/11/2026, slice 058: l'etichetta
//     accompagna la busta che parte verso l'artigiano, la data resta quella di stampa);
//   - .desc 11 pt interlinea 1,25, filetti di 1 px sopra e sotto, 1,5 mm di padding, alta almeno
//     18 mm (box-sizing: border-box), parole spezzabili, 1,5 mm sotto; .desc-stato 8 pt maiuscolo
//     con 0,5 pt fra le lettere, bianco su nero, padding 0,5 mm × 1,5 mm, 1 mm sotto, poi un a capo;
//   - .fatto 11 pt grassetto con la casella di 4 mm (bordo 1,5 px, vertical-align −1 mm, 1 mm a
//     destra), 1 mm sotto; .note-label 9 pt, 1 mm sotto; tre .note-line alte 6 mm con il filetto di
//     1 px in basso e 0,8 mm fra l'una e l'altra.
// Il numero grande è quello visibile (D-64, slice 058): «#0045/26» con un'etichetta, «N. 45/26» senza,
// dove lo standalone scrive «#45/26»; il contenuto del QR non cambia (ValoreQR).
// Le altezze delle righe sono quelle del CSS (interlinea «normal» = ascesa + discesa di DejaVu): le
// posizioni chiave cadono entro ±0,5 mm da quelle di Chromium (misurate sul banco, D-24).

// EtichettaDataArtigiano è la voce della data di stampa sull'etichetta (D-73): nello standalone
// «Consegna:».
const EtichettaDataArtigiano = "All'artigiano:"

// Le misure della pagina dell'etichetta, in mm.
const (
	LarghezzaEtichetta = 76.0
	AltezzaEtichetta   = 127.0
	margineEtichetta   = 3.0
	largaEtichetta     = 70.0 // .lab
	latoQR             = 22.0
	testataTesto       = largaEtichetta - latoQR - 2 // .header-text: flex 1, gap 2 mm
)

var (
	nero   = pdf.Nero
	bianco = pdf.Colore{R: 0xff, G: 0xff, B: 0xff}
)

// Etichetta è il PDF dell'etichetta di una busta, già passato da pdf.Controlla.
func Etichetta(e buste.DatiEtichetta) ([]byte, error) {
	d, err := pdf.Nuovo(LarghezzaEtichetta, AltezzaEtichetta)
	if err != nil {
		return nil, err
	}
	if err := PaginaEtichetta(d, e); err != nil {
		return nil, err
	}
	return d.Byte()
}

// maiuscolo è text-transform: uppercase.
func maiuscolo(s string) string { return strings.ToUpper(s) }

// righeAlte scrive le righe già divise della casella a partire da y e dice dove finiscono.
func righeAlte(d *pdf.Documento, c pdf.Casella, righe []string, y float64) (float64, error) {
	for _, r := range righe {
		if err := d.Riga(c, r, y); err != nil {
			return 0, err
		}
		y += pdf.AltezzaRiga(c)
	}
	return y, nil
}

// PaginaEtichetta disegna l'etichetta e sulla pagina corrente di d, che deve essere di 76×127 mm (una
// pagina .lab). La usano l'etichetta della busta e, con l'artigiano della consegna, il foglio delle
// etichette di una consegna all'artigiano (T4.8).
func PaginaEtichetta(d *pdf.Documento, e buste.DatiEtichetta) error {
	x0, y0 := margineEtichetta, margineEtichetta

	// Intestazione: il testo centrato accanto al QR, il blocco alto quanto il più alto dei due.
	artigiano := pdf.Casella{X: x0, Larghezza: testataTesto, Testo: "→ " + maiuscolo(e.Artigiano), Corpo: 9, Spaziatura: 0.5,
		Allinea: pdf.Centro, Colore: nero}
	var rArtigiano []string
	if e.Artigiano != "" {
		var err error
		if rArtigiano, _, err = d.Righe(artigiano); err != nil {
			return err
		}
	}
	busta := pdf.Casella{X: x0, Larghezza: testataTesto, Testo: e.Visibile.Titolo(), Corpo: 22, Grassetto: true, Interlinea: 1,
		Allinea: pdf.Centro, Colore: nero}
	rBusta, _, err := d.Righe(busta)
	if err != nil {
		return err
	}
	altoTesto := float64(len(rArtigiano))*pdf.AltezzaRiga(artigiano) + 0.5 + float64(len(rBusta))*pdf.AltezzaRiga(busta)
	alto := max(latoQR, altoTesto)
	y, err := righeAlte(d, artigiano, rArtigiano, y0+(alto-altoTesto)/2)
	if err != nil {
		return err
	}
	if _, err := righeAlte(d, busta, rBusta, y+0.5); err != nil {
		return err
	}
	if err := d.QR(x0+testataTesto+2, y0+(alto-latoQR)/2, latoQR, e.QR); err != nil {
		return err
	}
	y = y0 + alto + 1
	d.Rettangolo(x0, y, largaEtichetta, 2*px, nero)
	y += 2*px + 1.5

	// Meta: «All'artigiano: …» (D-73) a sinistra, «Scad.: …» a destra.
	if y, err = meta(d, x0, y, e); err != nil {
		return err
	}
	y += 1.5

	// Descrizione con lo stato.
	if y, err = descrizione(d, x0, y, e); err != nil {
		return err
	}
	y += 1.5

	// FATTO: la linea ha la casella (4 mm, abbassata di 1 mm sotto la linea di base) e il testo
	// 11 pt grassetto; sopra la linea di base vale l'ascesa del testo, sotto il millimetro della
	// casella.
	fatto := pdf.Casella{X: x0 + 4 + 1, Larghezza: largaEtichetta - 5, Corpo: 11, Grassetto: true, Colore: nero}
	corpo := 11 * mmPerPt
	sopra, sotto := max(pdf.Ascesa*corpo, 3.0), max(pdf.Discesa*corpo, 1.0)
	base := y + sopra
	tratto := 1.5 * px
	d.Riquadro(x0+tratto/2, base+1-4+tratto/2, 4-tratto, 4-tratto, tratto, nero)
	// La riga si scrive con la sua linea di base: la casella senza mezza interlinea ha l'ascesa sopra.
	if err := d.Riga(fatto, " FATTO", base-pdf.Ascesa*corpo); err != nil {
		return err
	}
	y += sopra + sotto + 1

	nota := pdf.Casella{X: x0, Larghezza: largaEtichetta, Testo: "Note:", Corpo: 9, Colore: nero}
	if err := d.Riga(nota, "Note:", y); err != nil {
		return err
	}
	y += pdf.AltezzaRiga(nota) + 1
	for range 3 {
		d.Rettangolo(x0, y+6-px, largaEtichetta, px, nero)
		y += 6 + 0.8
	}
	return nil
}

const mmPerPt = 25.4 / 72

// meta scrive la riga .meta e dice dove finisce. Le due voci sono elementi flex: se insieme non ci
// stanno, si stringono in proporzione alla loro larghezza (flex-shrink: 1), non sotto la parola più
// lunga, e vanno a capo dentro la loro parte; «Scad.:» sta in fondo a destra.
func meta(d *pdf.Documento, x0, y float64, e buste.DatiEtichetta) (float64, error) {
	cons := pdf.Casella{X: x0, Larghezza: largaEtichetta, Testo: EtichettaDataArtigiano + " " + e.Oggi, Corpo: 9, Colore: nero}
	if e.Scadenza == "" {
		righe, _, err := d.Righe(cons)
		if err != nil {
			return 0, err
		}
		return righeAlte(d, cons, righe, y)
	}
	scad := pdf.Casella{Larghezza: largaEtichetta, Testo: "Scad.: " + e.Scadenza, Corpo: 9, Colore: nero}
	wc, err := d.LarghezzaCasella(cons)
	if err != nil {
		return 0, err
	}
	ws, err := d.LarghezzaCasella(scad)
	if err != nil {
		return 0, err
	}
	if tot := wc + ws; tot > largaEtichetta {
		mc, err := parolaPiuLarga(d, cons)
		if err != nil {
			return 0, err
		}
		ms, err := parolaPiuLarga(d, scad)
		if err != nil {
			return 0, err
		}
		nc := max(wc-(tot-largaEtichetta)*wc/tot, mc)
		ns := max(largaEtichetta-nc, ms)
		nc = max(largaEtichetta-ns, mc)
		// Una parola più larga della sua parte esce dal bordo, come in Chromium (lo span non ha
		// overflow-wrap): il testo resta intero, che è quello che D-24 confronta.
		wc, ws = nc, ns
	}
	cons.Larghezza, scad.Larghezza = wc+1e-6, ws+1e-6
	// In fondo a destra (space-between); se le due parti non ci stanno, una dopo l'altra da sinistra
	// (con lo spazio libero negativo space-between è flex-start) e la seconda esce a destra.
	scad.X = x0 + max(largaEtichetta-ws, wc)
	rc, _, err := d.Righe(cons)
	if err != nil {
		return 0, err
	}
	rs, _, err := d.Righe(scad)
	if err != nil {
		return 0, err
	}
	fc, err := righeAlte(d, cons, rc, y)
	if err != nil {
		return 0, err
	}
	fs, err := righeAlte(d, scad, rs, y)
	if err != nil {
		return 0, err
	}
	return max(fc, fs), nil
}

// parolaPiuLarga è la larghezza min-content della casella: la sua parola più larga.
func parolaPiuLarga(d *pdf.Documento, c pdf.Casella) (float64, error) {
	w := 0.0
	for _, p := range strings.Fields(c.Testo) {
		c.Testo = p
		l, err := d.LarghezzaCasella(c)
		if err != nil {
			return 0, err
		}
		w = max(w, l)
	}
	return w, nil
}

// descrizione scrive il riquadro .desc (filetti, stato, testo) a partire da y e dice dove finisce.
func descrizione(d *pdf.Documento, x0, y float64, e buste.DatiEtichetta) (float64, error) {
	testo := pdf.Casella{X: x0, Larghezza: largaEtichetta, Testo: e.Descrizione, Corpo: 11, Interlinea: 1.25, SpezzaParole: true, Colore: nero}
	h := pdf.AltezzaRiga(testo)
	// La linea di base nella riga di 11 pt con interlinea 1,25: mezza interlinea più l'ascesa.
	corpo := 11 * mmPerPt
	strutSopra := (h-(pdf.Ascesa+pdf.Discesa)*corpo)/2 + pdf.Ascesa*corpo
	interno := y + px + 1.5
	riga := interno
	if e.Stato != "" {
		// Lo stato è un inline-block sulla linea di base della prima riga: sopra il padding e
		// l'ascesa del suo testo, sotto la discesa, il padding e il margine di 1 mm.
		stato := pdf.Casella{Larghezza: largaEtichetta, Testo: maiuscolo(e.Stato), Corpo: 8, Spaziatura: 0.5, Colore: bianco}
		w, err := d.LarghezzaCasella(stato)
		if err != nil {
			return 0, err
		}
		c8 := 8 * mmPerPt
		sopra := 0.5 + pdf.Ascesa*c8
		sotto := pdf.Discesa*c8 + 0.5 + 1
		base := riga + max(strutSopra, sopra)
		d.Rettangolo(x0, base-sopra, w+3, (pdf.Ascesa+pdf.Discesa)*c8+1, nero)
		stato.X = x0 + 1.5
		if err := d.Riga(stato, stato.Testo, base-pdf.Ascesa*c8); err != nil {
			return 0, err
		}
		riga = base + max(h-strutSopra, sotto)
	}
	righe, _, err := d.Righe(testo)
	if err != nil {
		return 0, err
	}
	fine, err := righeAlte(d, testo, righe, riga)
	if err != nil {
		return 0, err
	}
	alto := max(18, fine+1.5+px-y)
	d.Rettangolo(x0, y, largaEtichetta, px, nero)
	d.Rettangolo(x0, y+alto-px, largaEtichetta, px, nero)
	return y + alto, nil
}
