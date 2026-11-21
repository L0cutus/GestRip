package main

import (
	"encoding/binary"
	"math"
)

// Il microfono finto del livello browser (slice 110, scenario dettato-microfono: RIP-H-44, D-25). Chromium lo
// legge da un file WAV (--use-file-for-fake-audio-capture, con --use-fake-device-for-media-stream e
// --use-fake-ui-for-media-stream), che scriviFotocamere mette accanto ai video delle fotocamere finte, nella cartella
// montata in sola lettura nel container del browser: microfono.wav, tre secondi di un tono a tonoMicrofono Hz, PCM
// mono a 16 bit e 16 kHz. Nessuna voce, solo un suono inventato; Chromium lo ripete da capo. Il trascrittore finto ne
// riconosce il tono nella clip che riceve (modellofinto.DescriviWAV), così lo scenario vede che la clip del Go è il
// suono del microfono ricampionato a 16 kHz, e non un'altra frequenza o del silenzio.
const (
	nomeMicrofono      = "microfono.wav"
	tonoMicrofono      = 440
	frequenzaMicrofono = 16000
	secondiMicrofono   = 3
	// ampiezzaMicrofono è circa un terzo del massimo: un suono chiaro che non satura.
	ampiezzaMicrofono = 12000
)

// wavMicrofono è il file del microfono finto.
func wavMicrofono() []byte {
	n := frequenzaMicrofono * secondiMicrofono
	b := make([]byte, 44+2*n)
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+2*n))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1) // PCM
	binary.LittleEndian.PutUint16(b[22:], 1) // un canale
	binary.LittleEndian.PutUint32(b[24:], frequenzaMicrofono)
	binary.LittleEndian.PutUint32(b[28:], 2*frequenzaMicrofono)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(2*n))
	for i := range n {
		v := math.Round(ampiezzaMicrofono * math.Sin(2*math.Pi*tonoMicrofono*float64(i)/frequenzaMicrofono))
		binary.LittleEndian.PutUint16(b[44+2*i:], uint16(int16(v)))
	}
	return b
}
