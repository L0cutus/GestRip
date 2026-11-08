package lettura

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// I QR di una pagina (slice 043, D-24: «uguale» vuol dire anche lo stesso contenuto dei QR). Il QR
// di un PDF è vettoriale: Chromium disegna l'SVG di QRCode.create/2 dello standalone come un
// rettangolo pieno (re … f) per modulo scuro (misurato sulle etichette di fab-220), il motore Go come
// un rettangolo per tratto scuro di una riga. I moduli si ritrovano dai rettangoli, senza immagini né
// soglie, e il codice si decodifica come dice la ISO/IEC 18004: formato, maschera, blocchi.
//
// I rettangoli esatti non hanno errori di lettura: la correzione d'errore non si applica, si
// controlla soltanto che ogni blocco sia una parola valida di Reed-Solomon. Un modulo diverso è un
// codice diverso, ed è un errore, non un contenuto corretto in silenzio.
//
// Limiti, voluti: un QR per gruppo di rettangoli della stessa altezza (due QR con i moduli uguali
// sulla stessa pagina non si separano); il riquadro dei moduli scuri deve essere il codice intero
// (vale sempre: i tre riquadri di posizione stanno negli angoli); modi numerico, alfanumerico, byte
// (testo UTF-8) ed ECI; il kanji è un errore.

// CodiceQR è un QR letto: il testo e il riquadro dei suoi moduli in mm (senza il margine chiaro),
// con l'angolo in alto a sinistra in (X, Y). Moduli è il lato in moduli (21 per la versione 1);
// Modo è il modo del primo segmento dei dati («numerico», «alfanumerico», «byte»).
//
// Specchiato dice che il codice si legge solo scambiando righe e colonne (lo specchio sulla
// diagonale, che i lettori dei telefoni provano quando la lettura diritta non riesce, come ZXing).
// È il caso dello standalone: l'SVG di QRCode 3.2.0 mette la riga della matrice sull'asse x, e il
// QR stampato è trasposto (misurato sulle etichette di fab-220: diritto non si legge, trasposto dà
// livello M, maschera 0, byte, il testo della busta). Il contenuto è lo stesso; il motore Go
// stampa il QR diritto.
type CodiceQR struct {
	Testo      string
	X, Y, L, A float64
	Moduli     int
	Modo       string
	Specchiato bool
}

// ErrQR è un gruppo di rettangoli con la forma di un QR (i tre riquadri di posizione) che non si
// decodifica.
var ErrQR = errors.New("lettura: QR illeggibile")

// CodiciQR dà i QR della pagina, nell'ordine dei loro rettangoli.
func CodiciQR(p Pagina) ([]CodiceQR, error) {
	// I rettangoli raggruppati per altezza (al centesimo di mm): i moduli di un QR sono tutti alti uguali.
	gruppi := map[int64][]Rettangolo{}
	var ordine []int64
	for _, r := range p.Rettangoli {
		if !(r.A > 0 && r.L > 0) {
			continue
		}
		k := int64(math.Round(r.A * 100))
		if _, ok := gruppi[k]; !ok {
			ordine = append(ordine, k)
		}
		gruppi[k] = append(gruppi[k], r)
	}
	var out []CodiceQR
	for _, k := range ordine {
		g := gruppi[k]
		if len(g) < 3*7 { // almeno i tre riquadri di posizione
			continue
		}
		m, x, y, l, a, ok := griglia(g)
		if !ok || !posizioniGiuste(m) {
			continue
		}
		specchiato := false
		testo, modo, err := decodifica(m)
		if err != nil {
			t, mo, err2 := decodifica(trasposta(m))
			if err2 != nil {
				return nil, fmt.Errorf("%w: %w", ErrQR, err)
			}
			testo, modo, specchiato = t, mo, true
		}
		n := len(m)
		out = append(out, CodiceQR{Testo: testo, X: x, Y: y, L: float64(n) * l, A: float64(n) * a, Moduli: n, Modo: modo, Specchiato: specchiato})
	}
	return out, nil
}

// griglia mette i rettangoli in una griglia di moduli: il modulo è largo quanto il rettangolo più
// stretto e alto quanto il gruppo; ogni rettangolo deve coprire un numero intero di moduli e la
// griglia deve avere il lato di un QR (17 + 4·versione).
func griglia(g []Rettangolo) (m [][]bool, x0, y0, l, a float64, ok bool) {
	l = math.Inf(1)
	x0, y0 = math.Inf(1), math.Inf(1)
	x1, y1 := math.Inf(-1), math.Inf(-1)
	a = g[0].A
	for _, r := range g {
		l = math.Min(l, r.L)
		x0, y0 = math.Min(x0, r.X), math.Min(y0, r.Y)
		x1, y1 = math.Max(x1, r.X+r.L), math.Max(y1, r.Y+r.A)
	}
	n := int(math.Round((x1 - x0) / l))
	if n != int(math.Round((y1-y0)/a)) || n < 21 || n > 177 || (n-17)%4 != 0 {
		return nil, 0, 0, 0, 0, false
	}
	vicino := func(v float64) (int, bool) {
		i := math.Round(v)
		return int(i), math.Abs(v-i) < 0.2
	}
	m = make([][]bool, n)
	for i := range m {
		m[i] = make([]bool, n)
	}
	for _, r := range g {
		c, ok1 := vicino((r.X - x0) / l)
		riga, ok2 := vicino((r.Y - y0) / a)
		k, ok3 := vicino(r.L / l)
		if !ok1 || !ok2 || !ok3 || k < 1 || c+k > n || riga >= n {
			return nil, 0, 0, 0, 0, false
		}
		for j := range k {
			m[riga][c+j] = true
		}
	}
	return m, x0, y0, (x1 - x0) / float64(n), (y1 - y0) / float64(n), true
}

// trasposta scambia righe e colonne.
func trasposta(m [][]bool) [][]bool {
	t := make([][]bool, len(m))
	for i := range m {
		t[i] = make([]bool, len(m))
		for j := range m {
			t[i][j] = m[j][i]
		}
	}
	return t
}

// posizioniGiuste dice se nei tre angoli ci sono i riquadri di posizione 7×7 con il loro separatore.
func posizioniGiuste(m [][]bool) bool {
	n := len(m)
	for _, o := range [][2]int{{0, 0}, {n - 7, 0}, {0, n - 7}} {
		for dy := -1; dy <= 7; dy++ {
			for dx := -1; dx <= 7; dx++ {
				x, y := o[0]+dx, o[1]+dy
				if x < 0 || y < 0 || x >= n || y >= n {
					continue
				}
				d := max(abs(dx-3), abs(dy-3))
				if m[y][x] != (d != 2 && d != 4) {
					return false
				}
			}
		}
	}
	return true
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Le tabelle dei blocchi della ISO/IEC 18004 (tabella 9), per livello (L, M, Q, H) e versione:
// codici di correzione per blocco e numero dei blocchi.
var (
	correzionePerBlocco = [4][41]int{
		{0, 7, 10, 15, 20, 26, 18, 20, 24, 30, 18, 20, 24, 26, 30, 22, 24, 28, 30, 28, 28, 28, 28, 30, 30, 26, 28, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30},
		{0, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26, 30, 22, 22, 24, 24, 28, 28, 26, 26, 26, 26, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28},
		{0, 13, 22, 18, 26, 18, 24, 18, 22, 20, 24, 28, 26, 24, 20, 30, 24, 28, 28, 26, 30, 28, 30, 30, 30, 30, 28, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30},
		{0, 17, 28, 22, 16, 22, 28, 26, 26, 24, 28, 24, 28, 22, 24, 24, 30, 28, 28, 26, 28, 30, 24, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30},
	}
	numeroBlocchi = [4][41]int{
		{0, 1, 1, 1, 1, 1, 2, 2, 2, 2, 4, 4, 4, 4, 4, 6, 6, 6, 6, 7, 8, 8, 9, 9, 10, 12, 12, 12, 13, 14, 15, 16, 17, 18, 19, 19, 20, 21, 22, 24, 25},
		{0, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5, 5, 8, 9, 9, 10, 10, 11, 13, 14, 16, 17, 17, 18, 20, 21, 23, 25, 26, 28, 29, 31, 33, 35, 37, 38, 40, 43, 45, 47, 49},
		{0, 1, 1, 2, 2, 4, 4, 6, 6, 8, 8, 8, 10, 12, 16, 12, 17, 16, 18, 21, 20, 23, 23, 25, 27, 29, 34, 34, 35, 38, 40, 43, 45, 48, 51, 53, 56, 59, 62, 65, 68},
		{0, 1, 1, 2, 4, 4, 4, 5, 6, 8, 8, 11, 11, 16, 16, 18, 16, 19, 21, 25, 25, 25, 34, 30, 32, 35, 37, 40, 42, 45, 48, 51, 54, 57, 60, 63, 66, 70, 74, 77, 81},
	}
)

// allineamenti sono i centri dei riquadri di allineamento della versione v (allegato E).
func allineamenti(v int) []int {
	if v == 1 {
		return nil
	}
	k := v/7 + 2
	passo := (v*4 + k*2 + 1) / (k*2 - 2) * 2
	if v == 32 {
		passo = 26
	}
	out := make([]int, k)
	out[0] = 6
	for i, pos := k-1, v*4+10; i >= 1; i, pos = i-1, pos-passo {
		out[i] = pos
	}
	return out
}

// funzioni sono i moduli che non portano dati: posizione, separatori, sincronismo, allineamento,
// formato, versione e il modulo scuro.
func funzioni(n, v int) [][]bool {
	f := make([][]bool, n)
	for i := range f {
		f[i] = make([]bool, n)
	}
	segna := func(x, y int) {
		if x >= 0 && y >= 0 && x < n && y < n {
			f[y][x] = true
		}
	}
	for i := range n {
		segna(6, i)
		segna(i, 6)
	}
	for _, c := range [][2]int{{3, 3}, {n - 4, 3}, {3, n - 4}} {
		for dy := -4; dy <= 4; dy++ {
			for dx := -4; dx <= 4; dx++ {
				segna(c[0]+dx, c[1]+dy)
			}
		}
	}
	al := allineamenti(v)
	for i, ax := range al {
		for j, ay := range al {
			if (i == 0 && j == 0) || (i == 0 && j == len(al)-1) || (i == len(al)-1 && j == 0) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					segna(ax+dx, ay+dy)
				}
			}
		}
	}
	for i := range 9 {
		segna(8, i)
		segna(i, 8)
	}
	for i := range 8 {
		segna(n-1-i, 8)
		segna(8, n-1-i)
	}
	if v >= 7 {
		for i := range 18 {
			a, b := n-11+i%3, i/3
			segna(a, b)
			segna(b, a)
		}
	}
	return f
}

// formato legge livello e maschera dai bit di formato: almeno una delle due copie deve essere
// esattamente una parola BCH (nessuna correzione: i rettangoli sono esatti, un bit diverso è un altro
// codice). I livelli nell'ordine delle tabelle (L, M, Q, H).
func formato(m [][]bool) (livello, maschera int, err error) {
	n := len(m)
	bit := func(x, y int) int {
		if m[y][x] {
			return 1
		}
		return 0
	}
	prima, seconda := 0, 0
	for i := range 6 {
		prima |= bit(8, i) << i
	}
	prima |= bit(8, 7) << 6
	prima |= bit(8, 8) << 7
	prima |= bit(7, 8) << 8
	for i := 9; i < 15; i++ {
		prima |= bit(14-i, 8) << i
	}
	for i := range 8 {
		seconda |= bit(n-1-i, 8) << i
	}
	for i := 8; i < 15; i++ {
		seconda |= bit(8, n-15+i) << i
	}
	migliore, distanza := -1, 99
	for dati := range 32 {
		r := dati
		for range 10 {
			r = r<<1 ^ (r>>9)*0x537
		}
		parola := (dati<<10 | r) ^ 0x5412
		for _, letto := range []int{prima, seconda} {
			d := 0
			for x := parola ^ letto; x != 0; x &= x - 1 {
				d++
			}
			if d < distanza {
				migliore, distanza = dati, d
			}
		}
	}
	if distanza != 0 {
		return 0, 0, errors.New("bit di formato non validi")
	}
	// Nei bit di formato L=01, M=00, Q=11, H=10.
	return [4]int{1, 0, 3, 2}[migliore>>3], migliore & 7, nil
}

func mascherato(maschera, x, y int) bool {
	switch maschera {
	case 0:
		return (x+y)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (x+y)%3 == 0
	case 4:
		return (x/3+y/2)%2 == 0
	case 5:
		return x*y%2+x*y%3 == 0
	case 6:
		return (x*y%2+x*y%3)%2 == 0
	}
	return ((x+y)%2+x*y%3)%2 == 0
}

// decodifica legge il testo della griglia m.
func decodifica(m [][]bool) (testo, modo string, err error) {
	n := len(m)
	v := (n - 17) / 4
	livello, maschera, err := formato(m)
	if err != nil {
		return "", "", err
	}
	f := funzioni(n, v)
	moduli := (16*v+128)*v + 64
	if v >= 2 {
		k := v/7 + 2
		moduli -= (25*k-10)*k - 55
		if v >= 7 {
			moduli -= 36
		}
	}
	grezzi := make([]byte, moduli/8)
	i := 0
	for destra := n - 1; destra >= 1; destra -= 2 {
		if destra == 6 {
			destra = 5
		}
		for verticale := range n {
			for j := range 2 {
				x := destra - j
				y := verticale
				if (destra+1)&2 == 0 {
					y = n - 1 - verticale
				}
				if f[y][x] || i >= len(grezzi)*8 {
					continue
				}
				if m[y][x] != mascherato(maschera, x, y) {
					grezzi[i>>3] |= 0x80 >> (i & 7)
				}
				i++
			}
		}
	}
	dati, err := blocchi(grezzi, v, livello)
	if err != nil {
		return "", "", err
	}
	return segmenti(dati, v)
}

// blocchi separa i blocchi intercalati, controlla che ognuno sia una parola di Reed-Solomon e dà i
// codici dei dati in fila.
func blocchi(grezzi []byte, v, livello int) ([]byte, error) {
	nb, ec := numeroBlocchi[livello][v], correzionePerBlocco[livello][v]
	corti := nb - len(grezzi)%nb
	lungCorto := len(grezzi) / nb
	bb := make([][]byte, nb)
	k := 0
	for i := range lungCorto - ec + 1 {
		for b := range nb {
			if i == lungCorto-ec && b < corti {
				continue // i blocchi corti hanno un codice dei dati in meno
			}
			bb[b] = append(bb[b], grezzi[k])
			k++
		}
	}
	for range ec {
		for b := range nb {
			bb[b] = append(bb[b], grezzi[k])
			k++
		}
	}
	var dati []byte
	for b, blocco := range bb {
		if !parolaRS(blocco, ec) {
			return nil, fmt.Errorf("blocco %d non valido (un modulo diverso)", b)
		}
		dati = append(dati, blocco[:len(blocco)-ec]...)
	}
	return dati, nil
}

// Il campo GF(256) del QR (polinomio 0x11D, generatore 2).
var esp, logaritmo = func() ([512]byte, [256]int) {
	var e [512]byte
	var l [256]int
	x := 1
	for i := range 255 {
		e[i] = byte(x)
		l[x] = i
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D
		}
	}
	for i := 255; i < 512; i++ {
		e[i] = e[i-255]
	}
	return e, l
}()

// parolaRS dice se il blocco (dati poi correzione, il primo byte è il grado più alto) ha tutte le
// sindromi nulle sulle radici α^0…α^(ec−1) del generatore del QR.
func parolaRS(blocco []byte, ec int) bool {
	for i := range ec {
		s := byte(0)
		for _, c := range blocco {
			if s != 0 {
				s = esp[logaritmo[s]+i]
			}
			s ^= c
		}
		if s != 0 {
			return false
		}
	}
	return true
}

const alfanumerici = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ $%*+-./:"

// segmenti legge i segmenti dei dati: modo, lunghezza e contenuto, fino al terminatore.
func segmenti(dati []byte, v int) (string, string, error) {
	pos := 0
	leggi := func(n int) (int, bool) {
		if pos+n > len(dati)*8 {
			return 0, false
		}
		x := 0
		for range n {
			x = x<<1 | int(dati[pos>>3]>>(7-pos&7)&1)
			pos++
		}
		return x, true
	}
	classe := 0
	if v >= 27 {
		classe = 2
	} else if v >= 10 {
		classe = 1
	}
	var b strings.Builder
	primo := ""
	for {
		modo, ok := leggi(4)
		if !ok || modo == 0 {
			break
		}
		var nome string
		switch modo {
		case 1:
			nome = "numerico"
			lung, ok := leggi([3]int{10, 12, 14}[classe])
			if !ok {
				return "", "", errors.New("segmento numerico troncato")
			}
			for lung > 0 {
				cifre := min(lung, 3)
				x, ok := leggi([4]int{0, 4, 7, 10}[cifre])
				if !ok || x >= [4]int{0, 10, 100, 1000}[cifre] {
					return "", "", errors.New("segmento numerico non valido")
				}
				fmt.Fprintf(&b, "%0*d", cifre, x)
				lung -= cifre
			}
		case 2:
			nome = "alfanumerico"
			lung, ok := leggi([3]int{9, 11, 13}[classe])
			if !ok {
				return "", "", errors.New("segmento alfanumerico troncato")
			}
			for ; lung >= 2; lung -= 2 {
				x, ok := leggi(11)
				if !ok || x >= 45*45 {
					return "", "", errors.New("segmento alfanumerico non valido")
				}
				b.WriteByte(alfanumerici[x/45])
				b.WriteByte(alfanumerici[x%45])
			}
			if lung == 1 {
				x, ok := leggi(6)
				if !ok || x >= 45 {
					return "", "", errors.New("segmento alfanumerico non valido")
				}
				b.WriteByte(alfanumerici[x])
			}
		case 4:
			nome = "byte"
			lung, ok := leggi([3]int{8, 16, 16}[classe])
			if !ok {
				return "", "", errors.New("segmento byte troncato")
			}
			for range lung {
				x, ok := leggi(8)
				if !ok {
					return "", "", errors.New("segmento byte troncato")
				}
				b.WriteByte(byte(x))
			}
		case 7:
			// ECI: l'indicatore si salta, il testo resta in byte (UTF-8 per i due generatori).
			x, ok := leggi(8)
			if ok && x&0x80 != 0 {
				_, ok = leggi(8 * (1 + boolInt(x&0x40 != 0)))
			}
			if !ok {
				return "", "", errors.New("ECI troncato")
			}
			continue
		default:
			return "", "", fmt.Errorf("modo %d non letto", modo)
		}
		if primo == "" {
			primo = nome
		}
	}
	return b.String(), primo, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Le posizioni chiave dell'etichetta busta 76×127 (D-24: «posizioni chiave entro ±0,5 mm dove la
// misura è una regola, cioè nelle etichette»). Sono le cose che chi usa l'etichetta trova a colpo
// d'occhio o con lo scanner, e che il CSS dello standalone fissa in millimetri:
//   - qr: il riquadro dei moduli del QR (x, y, larghezza, altezza): lo scanner lo cerca lì, e il
//     lato dice anche la versione del codice;
//   - numero: la linea di base (y) della riga del numero grande, «#<numero>» o, nel Go senza
//     etichetta, «N. <numero>» (D-64; «N.» da solo quando il numero va a capo, da «N. 1000/26» in su
//     nei 46 mm accanto al QR). Solo la y dalla slice 058: la riga è centrata accanto al QR e il
//     suo inizio dipende dal testo, che con la D-64 nelle due app è diverso («#5/26» e «N. 5/26»);
//     dove il testo è lo stesso l'inizio lo confrontano i test di internal/stampe;
//   - consegna: l'inizio della linea di base della data di stampa (x, y), la prima riga sotto il
//     filetto: «Consegna: …» nello standalone, «All'artigiano: …» nel Go (D-73, slice 058);
//   - fatto: il riquadro della casella «FATTO» (il primo rettangolo disegnato con il solo tratto),
//     dove l'artigiano mette la crocetta.
// Le altre righe dipendono dalla lunghezza dei testi e non sono una regola.

// Posizione è una posizione chiave: il nome e le misure in mm (nil se sulla pagina non c'è).
type Posizione struct {
	Nome   string
	Valori []float64
}

// PosizioniEtichetta dà le posizioni chiave della pagina, sempre tutte e quattro e nello stesso
// ordine; un QR che non si legge è un errore.
func PosizioniEtichetta(p Pagina) ([]Posizione, error) {
	out := []Posizione{{Nome: "qr"}, {Nome: "numero"}, {Nome: "consegna"}, {Nome: "fatto"}}
	qq, err := CodiciQR(p)
	if err != nil {
		return nil, err
	}
	if len(qq) > 0 {
		out[0].Valori = []float64{qq[0].X, qq[0].Y, qq[0].L, qq[0].A}
	}
	for _, r := range p.Righe {
		t := strings.TrimSpace(r.Testo)
		if out[1].Valori == nil && (strings.HasPrefix(t, "#") || strings.HasPrefix(t, "N. ") || t == "N.") {
			out[1].Valori = []float64{r.Y}
		}
		if out[2].Valori == nil && (strings.HasPrefix(t, "Consegna:") || strings.HasPrefix(t, "All'artigiano:")) {
			out[2].Valori = []float64{r.X, r.Y}
		}
	}
	if len(p.Contorni) > 0 {
		c := p.Contorni[0]
		out[3].Valori = []float64{c.X, c.Y, c.L, c.A}
	}
	return out, nil
}

// TestoPosizioni scrive le posizioni come le confronta il banco: «qr 53.87 5.94 16.09 15.90;
// numero 16.67; …», al centesimo di mm, «assente» per una che manca.
func TestoPosizioni(pp []Posizione) string {
	parti := make([]string, len(pp))
	for i, p := range pp {
		s := p.Nome
		if p.Valori == nil {
			s += " assente"
		}
		for _, v := range p.Valori {
			s += fmt.Sprintf(" %.2f", v)
		}
		parti[i] = s
	}
	return strings.Join(parti, "; ")
}

// PosizioniVicine dice se due testi di TestoPosizioni (anche di più pagine, separate da « | ») hanno
// le stesse voci e ogni misura entro tolleranza mm. Un testo che non è di posizioni (per esempio
// «(non un PDF)») è vicino solo a sé stesso.
func PosizioniVicine(a, b string, tolleranza float64) bool {
	if a == b {
		return true
	}
	pa, pb := strings.Fields(a), strings.Fields(b)
	if len(pa) != len(pb) {
		return false
	}
	for i := range pa {
		// Un numero può chiudere la voce con «;»: il separatore deve essere lo stesso.
		na, sa := strings.CutSuffix(pa[i], ";")
		nb, sb := strings.CutSuffix(pb[i], ";")
		x, ex := strconv.ParseFloat(na, 64)
		y, ey := strconv.ParseFloat(nb, 64)
		if ex != nil || ey != nil || sa != sb {
			if pa[i] != pb[i] {
				return false
			}
			continue
		}
		if math.IsNaN(x) || math.IsNaN(y) || math.Abs(x-y) > tolleranza+1e-9 {
			return false
		}
	}
	return true
}
