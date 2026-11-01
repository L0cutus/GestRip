package imaging

import (
	"image"
	"math"
)

// dentroDelRiquadro sono le dimensioni di thumbnail((lato, lato)) di Pillow 9.4
// (preserve_aspect_ratio con round_aspect): niente da fare se la foto sta già nel riquadro;
// altrimenti il lato che tocca il riquadro resta lato e l'altro è l'intero vicino (sotto o sopra)
// che tiene meglio le proporzioni, a parità quello sotto, mai meno di 1.
func dentroDelRiquadro(l, a, lato int) (int, int) {
	x, y := lato, lato
	if x >= l && y >= a {
		return l, a
	}
	proporzione := float64(l) / float64(a)
	if float64(x)/float64(y) >= proporzione {
		x = arrotondaProporzione(float64(y)*proporzione, func(n float64) float64 {
			return math.Abs(proporzione - n/float64(y))
		})
	} else {
		y = arrotondaProporzione(float64(x)/proporzione, func(n float64) float64 {
			if n == 0 {
				return 0
			}
			return math.Abs(proporzione - float64(x)/n)
		})
	}
	return x, y
}

// arrotondaProporzione è round_aspect: max(min(floor, ceil, key=chiave), 1); min dà il primo a
// parità.
func arrotondaProporzione(n float64, chiave func(float64) float64) int {
	giu, su := math.Floor(n), math.Ceil(n)
	v := giu
	if chiave(su) < chiave(giu) {
		v = su
	}
	return max(int(v), 1)
}

// dopoIlDraft sono le dimensioni di un JPEG dopo draft("RGB", richiesta) di Pillow 9.4, con la
// richiesta di normalize_image.py (il lato lungo a Pavimento): la riduzione più forte fra 1/8,
// 1/4, 1/2 e 1 che lascia almeno le dimensioni chieste, arrotondando per eccesso.
func dopoIlDraft(l, a int) (int, int) {
	r := float64(Pavimento) / float64(max(l, a))
	rl, ra := max(1, int(float64(l)*r)), max(1, int(float64(a)*r))
	riduzione := min(l/rl, a/ra)
	s := 1
	for _, v := range []int{8, 4, 2, 1} {
		if riduzione >= v {
			s = v
			break
		}
	}
	return (l + s - 1) / s, (a + s - 1) / s
}

// orienta applica l'orientamento EXIF come ImageOps.exif_transpose: 2 specchio orizzontale, 3
// 180°, 4 specchio verticale, 5 trasposta, 6 90° in senso orario, 7 trasversa, 8 90° in senso
// antiorario; ogni altro valore lascia la foto com'è.
func orienta(m *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return m
	}
	l, a := m.Bounds().Dx(), m.Bounds().Dy()
	nl, na := l, a
	if o >= 5 {
		nl, na = a, l
	}
	d := image.NewRGBA(image.Rect(0, 0, nl, na))
	for y := 0; y < a; y++ {
		for x := 0; x < l; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = l-1-x, y
			case 3:
				dx, dy = l-1-x, a-1-y
			case 4:
				dx, dy = x, a-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = a-1-y, x
			case 7:
				dx, dy = a-1-y, l-1-x
			case 8:
				dx, dy = y, l-1-x
			}
			s, t := m.PixOffset(x, y), d.PixOffset(dx, dy)
			copy(d.Pix[t:t+4], m.Pix[s:s+4])
		}
	}
	return d
}
