package assist

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// Il mezzo secondo di silenzio prima e dopo la clip (PIANO §1.8, decisione 3: con Parakeet gli errori sulle
// clip vere scendono dal 32,1% al 28,3%). Serve al motore locale della T5.1, che riceve il WAV mono 16 kHz
// registrato dal browser (D-25, slice 110); il server esterno di oggi riceve la clip com'è, come lo standalone,
// e il silenzio non lo tocca (domanda G del piano T4.10: qui solo la parte che serve ora).

// SilenzioAiBordi è il silenzio che ConSilenzio mette prima e dopo l'audio: mezzo secondo.
const SilenzioAiBordi = 500 // millisecondi

// Limiti dell'intestazione che ConSilenzio accetta: il silenzio si alloca da frequenza e canali, quindi
// un'intestazione fuori misura (1e9 Hz) non deve poter chiedere gigabyte (revisione della 109). Al più
// 48 kHz e 8 canali: mezzo secondo è al più 384 KB.
const (
	frequenzaMassimaWAV = 48_000
	canaliMassimiWAV    = 8
)

// ErrWAV: la clip non è un WAV PCM a 16 bit che ConSilenzio sa allungare.
var ErrWAV = errors.New("assist: la clip non è un WAV PCM a 16 bit")

// ConSilenzio dà la clip WAV (RIFF, PCM intero a 16 bit, qualunque frequenza e numero di canali) con
// SilenzioAiBordi di campioni a zero prima e dopo i campioni; il resto del file (gli altri pezzi del RIFF) non
// serve al motore e non si copia. Un pezzo «data» che dichiara più byte di quelli che ci sono (un registratore
// che non sa la lunghezza) vale per quelli che ci sono. Frequenza oltre 48 kHz, più di 8 canali o campioni oltre
// ClipMassima sono ErrWAV: il risultato è al più ClipMassima più i due silenzi. Non tocca la clip ricevuta.
func ConSilenzio(wav []byte) ([]byte, error) {
	if len(wav) < 12 || string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return nil, ErrWAV
	}
	var formato, campioni []byte
	for pos := 12; pos+8 <= len(wav); {
		id, dim := string(wav[pos:pos+4]), int64(binary.LittleEndian.Uint32(wav[pos+4:pos+8]))
		inizio := pos + 8
		fine := int64(inizio) + dim
		if fine > int64(len(wav)) {
			if id != "data" {
				return nil, ErrWAV
			}
			fine = int64(len(wav))
		}
		switch id {
		case "fmt ":
			formato = wav[inizio:fine]
		case "data":
			campioni = wav[inizio:fine]
		}
		if formato != nil && campioni != nil {
			break
		}
		pos = int(fine + dim%2) // i pezzi di lunghezza dispari hanno un byte in più
	}
	if len(formato) < 16 || campioni == nil {
		return nil, ErrWAV
	}
	tipo := binary.LittleEndian.Uint16(formato[0:2])
	canali := int64(binary.LittleEndian.Uint16(formato[2:4]))
	frequenza := int64(binary.LittleEndian.Uint32(formato[4:8]))
	blocco := int64(binary.LittleEndian.Uint16(formato[12:14]))
	bit := binary.LittleEndian.Uint16(formato[14:16])
	if tipo != 1 || bit != 16 || canali == 0 || canali > canaliMassimiWAV || frequenza == 0 || frequenza > frequenzaMassimaWAV || blocco != canali*2 ||
		int64(len(campioni)) > ClipMassima {
		return nil, ErrWAV
	}
	campioni = campioni[:int64(len(campioni))/blocco*blocco] // un campione a metà non si suona
	silenzio := frequenza * SilenzioAiBordi / 1000 * blocco
	dati := 2*silenzio + int64(len(campioni))
	pezzoFmt := int64(len(formato)) + int64(len(formato))%2
	totale := 4 + 8 + pezzoFmt + 8 + dati + dati%2 // con i limiti sopra resta lontano dai 4 GiB del RIFF
	var b bytes.Buffer
	b.Grow(int(8 + totale))
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(totale)) // un bytes.Buffer non fallisce
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(formato)))
	b.Write(formato)
	if len(formato)%2 == 1 {
		b.WriteByte(0)
	}
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(dati))
	b.Write(make([]byte, silenzio))
	b.Write(campioni)
	b.Write(make([]byte, silenzio))
	if dati%2 == 1 {
		b.WriteByte(0)
	}
	return b.Bytes(), nil
}
