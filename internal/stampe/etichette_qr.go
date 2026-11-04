package stampe

import (
	"math"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/pdf"
)

// Le etichette QR prenumerate (RIP-S-48): una pagina di L×H mm per etichetta, con il QR
// «RIP-<anno>-NNNN» e, sotto, il numero «0045/26» (D-21 aggiornata da D-64, Stefano 30/10 e
// 03/11/2026; lo standalone lo stampa a destra, «0045»). La disposizione, del Go:
//   - 2 mm di margine su ogni lato (lo standalone mette il QR a 2 mm dal bordo sinistro);
//   - il numero in grassetto, interlinea 1, centrato; il corpo parte da max(8, int(h × 0,55)) pt come
//     lo standalone e scende di mezzo punto finché il numero non sta nella larghezza (al minimo 4 pt);
//   - il QR (con il margine chiaro di 4 moduli dentro il suo lato, come lo standalone) è un quadrato
//     di lato min(L − 4, H − 4 − riga del numero), centrato in orizzontale; QR e numero, uno sotto
//     l'altro senza spazio in più (il margine chiaro del QR lo separa dal numero), sono centrati in
//     verticale. A 57×32 mm: numero 17 pt, QR di 22 mm; a 16×16 mm: numero 7,5 pt, QR di circa 9,35 mm (moduli
//     di 0,32 mm, 2,5 punti di una termica a 203 dpi).
const (
	margineEtichetteQR = 2.0
	corpoMinimoQR      = 4.0
	mmPerPunto         = 25.4 / 72
)

// DisposizioneEtichettaQR è dove vanno QR e numero su un'etichetta L×H mm con quel numero.
type DisposizioneEtichettaQR struct {
	QRX, QRY, Lato float64 // mm
	Corpo          float64 // pt
	TestoY         float64 // il bordo alto della riga del numero, mm
}

// DisponiEtichettaQR calcola la disposizione del numero testo su un'etichetta l×h mm di d.
func DisponiEtichettaQR(d *pdf.Documento, l, h float64, testo string) (DisposizioneEtichettaQR, error) {
	larga := l - 2*margineEtichetteQR
	corpo := math.Max(8, math.Trunc(h*0.55))
	for corpo > corpoMinimoQR {
		w, err := d.Larghezza(testo, corpo, true)
		if err != nil {
			return DisposizioneEtichettaQR{}, err
		}
		if w <= larga {
			break
		}
		corpo -= 0.5
	}
	riga := corpo * mmPerPunto
	lato := math.Min(larga, h-2*margineEtichetteQR-riga)
	y := (h - lato - riga) / 2
	return DisposizioneEtichettaQR{QRX: (l - lato) / 2, QRY: y, Lato: lato, Corpo: corpo, TestoY: y + lato}, nil
}

// EtichetteQR è il PDF del foglio f, già passato da pdf.Controlla.
func EtichetteQR(f buste.FoglioEtichette) ([]byte, error) {
	d, err := pdf.Nuovo(f.L, f.H)
	if err != nil {
		return nil, err
	}
	for n := f.Da; n <= f.A; n++ {
		if n > f.Da {
			d.Pagina()
		}
		testo := buste.NumeroEtichetta(f.Anno, n)
		p, err := DisponiEtichettaQR(d, f.L, f.H, testo)
		if err != nil {
			return nil, err
		}
		if err := d.QR(p.QRX, p.QRY, p.Lato, buste.CodiceEtichetta(f.Anno, n)); err != nil {
			return nil, err
		}
		c := pdf.Casella{X: margineEtichetteQR, Y: p.TestoY, Larghezza: f.L - 2*margineEtichetteQR, Testo: testo,
			Corpo: p.Corpo, Grassetto: true, Interlinea: 1, Allinea: pdf.Centro, Colore: nero}
		if _, _, err := d.Testo(c); err != nil {
			return nil, err
		}
	}
	return d.Byte()
}
