package modellofinto

import (
	"encoding/binary"
	"math"
	"net/http/httptest"
	"strings"
	"testing"
)

// wavDiProva è un WAV inventato: PCM (formato 1) a 16 bit, il tono hz su tutti i canali (0 = silenzio), con un pezzo
// LIST di lunghezza dispari prima di data se lista; dati è la lunghezza scritta in data (-1 = quella vera).
func wavDiProva(frequenza, canali, hz int, secondi float64, lista bool, dati int) []byte {
	n := int(float64(frequenza) * secondi)
	pcm := make([]byte, 2*n*canali)
	for i := range n {
		v := int16(math.Round(9000 * math.Sin(2*math.Pi*float64(hz)*float64(i)/float64(frequenza))))
		for c := range canali {
			binary.LittleEndian.PutUint16(pcm[2*(i*canali+c):], uint16(v))
		}
	}
	var b []byte
	b = append(b, "RIFF\x00\x00\x00\x00WAVE"...)
	fmt := make([]byte, 24)
	copy(fmt, "fmt ")
	binary.LittleEndian.PutUint32(fmt[4:], 16)
	binary.LittleEndian.PutUint16(fmt[8:], 1)
	binary.LittleEndian.PutUint16(fmt[10:], uint16(canali))
	binary.LittleEndian.PutUint32(fmt[12:], uint32(frequenza))
	binary.LittleEndian.PutUint32(fmt[16:], uint32(2*frequenza*canali))
	binary.LittleEndian.PutUint16(fmt[20:], uint16(2*canali))
	binary.LittleEndian.PutUint16(fmt[22:], 16)
	b = append(b, fmt...)
	if lista {
		b = append(b, "LIST\x03\x00\x00\x00abc\x00"...)
	}
	if dati < 0 {
		dati = len(pcm)
	}
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(dati))
	b = append(b, pcm...)
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	return b
}

// DescriviWAV legge com'è fatta la clip del microfono del Go (slice 110, D-25) e ne stima il tono, senza tenerla: un
// WAV PCM a 16 bit con qualunque frequenza e numero di canali (il tono dal primo), anche con altri pezzi prima di
// data; ciò che non è un WAV leggibile è "" e 0, un formato che non è PCM a 16 bit si descrive ma non ha tono.
func TestDescriviWAV(t *testing.T) {
	for _, c := range []struct {
		nome  string
		clip  []byte
		descr string
		tono  int
	}{
		{"mono 16 kHz", wavDiProva(16000, 1, 440, 2, false, -1), "PCM 16000 Hz, 1 canale, 16 bit", 440},
		{"stereo 44,1 kHz con LIST", wavDiProva(44100, 2, 1000, 1, true, -1), "PCM 44100 Hz, 2 canali, 16 bit", 1000},
		{"silenzio", wavDiProva(16000, 1, 0, 1, false, -1), "PCM 16000 Hz, 1 canale, 16 bit", 0},
		{"data più lungo del file", wavDiProva(16000, 1, 440, 1, false, 1<<20), "PCM 16000 Hz, 1 canale, 16 bit, data incompleta", 440},
		{"non WAV", []byte(strings.Repeat("+", 3000)), "", 0},
		{"corto", []byte("RIFF"), "", 0},
		{"senza fmt", []byte("RIFF\x0c\x00\x00\x00WAVEdata\x00\x00\x00\x00"), "", 0},
	} {
		d, tono := DescriviWAV(c.clip)
		if d != c.descr || tono < c.tono-3 || tono > c.tono+3 {
			t.Errorf("%s: %q %d, atteso %q %d", c.nome, d, tono, c.descr, c.tono)
		}
	}
	// Un fruscio sotto la soglia sopra il tono (±1500 a campioni alterni) non conta come passaggi per lo zero: c'è l'isteresi.
	r := wavDiProva(16000, 1, 440, 1, false, -1)
	for i := 44; i+1 < len(r); i += 2 {
		v := int16(binary.LittleEndian.Uint16(r[i:])) + int16(1500*(1-2*((i/2)%2)))
		binary.LittleEndian.PutUint16(r[i:], uint16(v))
	}
	if _, tono := DescriviWAV(r); tono < 437 || tono > 443 {
		t.Errorf("tono con il fruscio: %d", tono)
	}
	// Un formato che non è PCM (3, virgola mobile a 32 bit): descritto, senza tono.
	f := wavDiProva(16000, 1, 440, 1, false, -1)
	binary.LittleEndian.PutUint16(f[20:], 3)
	binary.LittleEndian.PutUint16(f[34:], 32)
	if d, tono := DescriviWAV(f); d != "formato 3 16000 Hz, 1 canale, 32 bit" || tono != 0 {
		t.Errorf("virgola mobile: %q %d", d, tono)
	}
	// Un formato che non è PCM anche a 16 bit (3 con 16 bit): descritto, senza tono.
	binary.LittleEndian.PutUint16(f[34:], 16)
	if d, tono := DescriviWAV(f); d != "formato 3 16000 Hz, 1 canale, 16 bit" || tono != 0 {
		t.Errorf("formato 3 a 16 bit: %q %d", d, tono)
	}
	// Il pezzo fmt senza il pezzo data non è una clip leggibile.
	if d, tono := DescriviWAV(f[:44-8]); d != "" || tono != 0 {
		t.Errorf("senza data: %q %d", d, tono)
	}
	// Zero canali: niente divisione per zero.
	binary.LittleEndian.PutUint16(f[20:], 1)
	binary.LittleEndian.PutUint16(f[22:], 0)
	binary.LittleEndian.PutUint16(f[34:], 16)
	if d, tono := DescriviWAV(f); d != "PCM 16000 Hz, 0 canali, 16 bit" || tono != 0 {
		t.Errorf("zero canali: %q %d", d, tono)
	}
}

// Il trascrittore finto descrive la clip WAV che riceve (wav, tono_hz) nelle richieste, mai i byte; una clip che non
// è WAV (il webm dello standalone) ha wav vuoto.
func TestTrascrittoreFintoDescriveLaClip(t *testing.T) {
	f := Nuovo()
	s := httptest.NewServer(f)
	defer s.Close()
	clip := wavDiProva(16000, 1, 440, 2, false, -1)
	if codice, _ := chiama(t, s, "POST", "/stt", string(clip), map[string]string{"Content-Type": "audio/wav"}); codice != 200 {
		t.Fatalf("%d", codice)
	}
	if codice, _ := chiama(t, s, "POST", "/stt", "CLIP-WEBM-FINTA", map[string]string{"Content-Type": "audio/webm"}); codice != 200 {
		t.Fatalf("%d", codice)
	}
	rr := f.RichiesteSTT()
	if len(rr) != 2 || rr[0].WAV != "PCM 16000 Hz, 1 canale, 16 bit" || rr[0].TonoHz < 437 || rr[0].TonoHz > 443 || rr[0].Byte != len(clip) ||
		rr[1].WAV != "" || rr[1].TonoHz != 0 || rr[1].Byte != 15 {
		t.Errorf("richieste %+v", rr)
	}
	_, r := chiama(t, s, "GET", "/_prova/richieste-stt", "", nil)
	if !strings.Contains(r, `"wav":""`) || !strings.Contains(r, `"tono_hz":0`) || strings.Contains(r, "CLIP-WEBM") {
		t.Errorf("richieste %q", r)
	}
}
