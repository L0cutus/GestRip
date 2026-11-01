package imaging

import (
	"image"
	"math"
)

// ridimensiona porta src a l × a con il filtro CatmullRom (tema H), in due passaggi separati
// (orizzontale, poi verticale) ma a righe: si tengono solo le righe della sorgente che servono alla
// riga d'uscita, già ridotte in orizzontale. golang.org/x/image/draw tiene invece tutto il passaggio
// intermedio in float64 (larghezza d'uscita × altezza della sorgente × 32 byte: quasi 1 GB per un
// 60 MP), che non sta nei 512 MiB del figlio.
func ridimensiona(src image.Image, l, a int) *image.RGBA {
	b := src.Bounds()
	ph, pv := calcolaPesi(b.Dx(), l), calcolaPesi(b.Dy(), a)
	dst := image.NewRGBA(image.Rect(0, 0, l, a))
	riga := make([]uint8, b.Dx()*3)
	// finestra: le righe ridotte in orizzontale, dalla sorgente prima in poi.
	prima := 0
	var finestra [][]float32
	ridotta := func(y int) []float32 {
		for prima+len(finestra) <= y {
			leggiRiga(src, b.Min.Y+prima+len(finestra), riga)
			r := make([]float32, l*3)
			for x := 0; x < l; x++ {
				var c0, c1, c2 float32
				i := ph.inizio[x] * 3
				for _, w := range ph.pesi[x] {
					c0 += w * float32(riga[i])
					c1 += w * float32(riga[i+1])
					c2 += w * float32(riga[i+2])
					i += 3
				}
				r[x*3], r[x*3+1], r[x*3+2] = c0, c1, c2
			}
			finestra = append(finestra, r)
		}
		return finestra[y-prima]
	}
	for y := 0; y < a; y++ {
		if salta := pv.inizio[y] - prima; salta > 0 {
			salta = min(salta, len(finestra))
			finestra = finestra[salta:]
			prima += salta
			if len(finestra) == 0 {
				prima = pv.inizio[y]
			}
		}
		out := dst.Pix[y*dst.Stride : y*dst.Stride+l*4]
		acc := make([]float32, l*3)
		for k, w := range pv.pesi[y] {
			r := ridotta(pv.inizio[y] + k)
			for i, v := range r {
				acc[i] += w * v
			}
		}
		for x := 0; x < l; x++ {
			out[x*4] = satura(acc[x*3])
			out[x*4+1] = satura(acc[x*3+1])
			out[x*4+2] = satura(acc[x*3+2])
			out[x*4+3] = 0xff
		}
	}
	return dst
}

func satura(v float32) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 255:
		return 255
	}
	return uint8(v + 0.5)
}

// pesiAsse sono, per ogni punto d'uscita di un asse, il primo punto della sorgente e i pesi
// normalizzati dei punti da lì in poi.
type pesiAsse struct {
	inizio []int
	pesi   [][]float32
}

// calcolaPesi è il campionamento di x/image/draw: il centro del punto d'uscita i cade in
// (i+0.5)·scala; in riduzione il filtro si allarga della scala.
func calcolaPesi(sn, dn int) pesiAsse {
	scala := float64(sn) / float64(dn)
	allarga := max(scala, 1)
	supporto := 2 * allarga
	p := pesiAsse{inizio: make([]int, dn), pesi: make([][]float32, dn)}
	for i := 0; i < dn; i++ {
		centro := (float64(i) + 0.5) * scala
		da := max(int(math.Floor(centro-supporto)), 0)
		a := min(int(math.Ceil(centro+supporto)), sn)
		w := make([]float64, 0, a-da)
		somma := 0.0
		for j := da; j < a; j++ {
			v := catmullRom((float64(j) + 0.5 - centro) / allarga)
			w = append(w, v)
			somma += v
		}
		pf := make([]float32, len(w))
		for k, v := range w {
			pf[k] = float32(v / somma)
		}
		p.inizio[i], p.pesi[i] = da, pf
	}
	return p
}

// catmullRom è il filtro cubico con B = 0, C = 0,5 (draw.CatmullRom).
func catmullRom(x float64) float64 {
	x = math.Abs(x)
	switch {
	case x < 1:
		return (1.5*x-2.5)*x*x + 1
	case x < 2:
		return ((-0.5*x+2.5)*x-4)*x + 2
	}
	return 0
}

// leggiRiga mette in r la riga y di m in RGB a 8 bit, con le vie veloci dei tipi che danno i
// decoder.
func leggiRiga(m image.Image, y int, r []uint8) {
	b := m.Bounds()
	switch x := m.(type) {
	case *image.YCbCr:
		for i := 0; i < b.Dx(); i++ {
			px := b.Min.X + i
			yi, ci := x.YOffset(px, y), x.COffset(px, y)
			r[i*3], r[i*3+1], r[i*3+2] = ycbcrRGB(x.Y[yi], x.Cb[ci], x.Cr[ci])
		}
	case *image.Gray:
		s := x.Pix[(y-b.Min.Y)*x.Stride:]
		for i := 0; i < b.Dx(); i++ {
			r[i*3], r[i*3+1], r[i*3+2] = s[i], s[i], s[i]
		}
	case *image.RGBA:
		s := x.Pix[(y-b.Min.Y)*x.Stride:]
		for i := 0; i < b.Dx(); i++ {
			r[i*3], r[i*3+1], r[i*3+2] = s[i*4], s[i*4+1], s[i*4+2]
		}
	case *image.NRGBA:
		s := x.Pix[(y-b.Min.Y)*x.Stride:]
		for i := 0; i < b.Dx(); i++ {
			r[i*3], r[i*3+1], r[i*3+2] = s[i*4], s[i*4+1], s[i*4+2]
		}
	default:
		for i := 0; i < b.Dx(); i++ {
			cr, cg, cb, _ := m.At(b.Min.X+i, y).RGBA()
			r[i*3], r[i*3+1], r[i*3+2] = uint8(cr>>8), uint8(cg>>8), uint8(cb>>8)
		}
	}
}

// ycbcrRGB è color.YCbCrToRGB (le stesse formule di JFIF dei decoder).
func ycbcrRGB(y, cb, cr uint8) (uint8, uint8, uint8) {
	yy := int32(y) * 0x10101
	cb1, cr1 := int32(cb)-128, int32(cr)-128
	r := yy + 91881*cr1
	g := yy - 22554*cb1 - 46802*cr1
	bb := yy + 116130*cb1
	return taglia(r), taglia(g), taglia(bb)
}

func taglia(v int32) uint8 {
	if uint32(v)&0xff000000 == 0 {
		return uint8(v >> 16)
	}
	if v < 0 {
		return 0
	}
	return 0xff
}
