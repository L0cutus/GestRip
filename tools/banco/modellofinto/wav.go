package modellofinto

import (
	"encoding/binary"
	"fmt"
	"math"
)

// DescriviWAV dice com'è fatta una clip WAV (slice 110, la clip del microfono del Go, D-25), per lo scenario
// dettato-microfono del banco: «PCM 16000 Hz, 1 canale, 16 bit» (con «, data incompleta» se il pezzo data dichiara più
// byte di quelli che ci sono) e il tono in Hz, stimato contando i passaggi per lo zero del primo canale con
// un'isteresi del 20% del picco (0 per il silenzio o per un formato che non è PCM a 16 bit). Una clip che non è un WAV
// con il pezzo fmt e il pezzo data è "" e 0. La clip non si tiene: restano solo questi due valori.
func DescriviWAV(b []byte) (string, int) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return "", 0
	}
	var fmtPezzo, dati []byte
	incompleta := false
	for p := 12; p+8 <= len(b); {
		id, n := string(b[p:p+4]), int(binary.LittleEndian.Uint32(b[p+4:p+8]))
		p += 8
		if n > len(b)-p {
			n, incompleta = len(b)-p, id == "data"
		}
		switch id {
		case "fmt ":
			fmtPezzo = b[p : p+n]
		case "data":
			dati = b[p : p+n]
		}
		p += n + n%2
	}
	if len(fmtPezzo) < 16 || dati == nil {
		return "", 0
	}
	formato := binary.LittleEndian.Uint16(fmtPezzo[0:])
	canali := int(binary.LittleEndian.Uint16(fmtPezzo[2:]))
	hz := int(binary.LittleEndian.Uint32(fmtPezzo[4:]))
	bit := int(binary.LittleEndian.Uint16(fmtPezzo[14:]))
	nome := "PCM"
	if formato != 1 {
		nome = fmt.Sprintf("formato %d", formato)
	}
	parola := "canali"
	if canali == 1 {
		parola = "canale"
	}
	descr := fmt.Sprintf("%s %d Hz, %d %s, %d bit", nome, hz, canali, parola, bit)
	if incompleta {
		descr += ", data incompleta"
	}
	if formato != 1 || bit != 16 || canali == 0 || hz == 0 {
		return descr, 0
	}
	return descr, tono(dati, canali, hz)
}

// tono è la frequenza dominante del primo canale di campioni PCM a 16 bit: i passaggi da sotto -soglia a sopra
// +soglia (un periodo ciascuno) per secondo, con la soglia al 20% del picco.
func tono(dati []byte, canali, hz int) int {
	passo := 2 * canali
	campioni := len(dati) / passo
	if campioni == 0 {
		return 0
	}
	campione := func(i int) float64 { return float64(int16(binary.LittleEndian.Uint16(dati[i*passo:]))) }
	picco := 0.0
	for i := range campioni {
		picco = math.Max(picco, math.Abs(campione(i)))
	}
	if picco == 0 {
		return 0
	}
	soglia, su, periodi := picco/5, false, 0
	for i := range campioni {
		switch x := campione(i); {
		case !su && x > soglia:
			su = true
			periodi++
		case su && x < -soglia:
			su = false
		}
	}
	return int(math.Round(float64(periodi) * float64(hz) / float64(campioni)))
}
