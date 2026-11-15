// Package firmephoenix verifica i Phoenix.Token che lo standalone ha già mandato e che il Go deve
// ancora aprire: i link /artigiano/<id>?t= degli artigiani (D-87, Stefano 14/11/2026, eccezione
// alla D-02 solo per loro; slice 084). Il Go non li emette per i suoi link: firma solo i vettori
// dei test e del banco.
//
// Il formato è quello di Plug.Crypto 2.2.0 (misurato sull'immagine dello standalone, ricevuta della
// 084), che Phoenix.Token.sign/4 e verify/4 chiamano con il secret_key_base dell'endpoint:
//   - la chiave è Plug.Crypto.KeyGenerator.generate/3: PBKDF2-HMAC-SHA256 del secret_key_base con
//     il sale, 1000 giri, 32 byte;
//   - il gettone è MessageVerifier.sign/2: «SFMyNTY» (base64url di «HS256») «.» payload in base64url
//     senza riempimento «.» HMAC-SHA256 della chiave sui due primi pezzi così come sono scritti;
//   - il payload è :erlang.term_to_binary({dato, firmato_ms, max_age_s});
//   - verify/4 con max_age: rifiutato se firmato_ms è dopo adesso (:invalid) o se firmato_ms più
//     max_age in millisecondi è prima di adesso (:expired); il max_age scritto nel gettone non conta.
//
// Del formato di Erlang si legge solo quello che serve, e solo dopo la verifica dell'HMAC: versione
// 131, una tupla piccola di tre interi (SMALL_INTEGER_EXT, INTEGER_EXT, SMALL_BIG_EXT fino a 8 byte
// entro int64, anche in forma non canonica), e niente dopo. Ogni altra struttura è un rifiuto. Delle intestazioni solo HS256, l'unica che lo
// standalone scrive.
//
// Il segreto sta in un tipo che non si stampa, come firmeflask.Segreto.
package firmephoenix

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/L0cutus/gestrip/internal/segreti"
)

// MinimoSegreto è il minimo di SECRET_KEY_BASE_FILE in config/runtime.exs dello standalone: 64
// byte, senza gli spazi ai bordi.
const MinimoSegreto = 64

// intestazione è base64url(«HS256»): MessageVerifier con :sha256.
const intestazione = "SFMyNTY"

// Le opzioni di KeyGenerator che Phoenix.Token usa senza opzioni.
const (
	giri      = 1000
	lunghezza = 32
)

// maxAgeFirma è il max_age che Plug.Crypto scrive nel gettone senza opzioni (86400 secondi).
const maxAgeFirma = 86400

var b64 = base64.RawURLEncoding.Strict()

// Segreto è il secret_key_base dello standalone. Il valore zero è «assente»: ogni gettone è
// rifiutato. I byte stanno solo dentro la funzione chiave.
type Segreto struct{ chiave func(sale string) []byte }

const segnaposto = "[segreto dello standalone]"

// String, GoString, Format, LogValue e MarshalText non mostrano mai il segreto.
func (Segreto) String() string               { return segnaposto }
func (Segreto) GoString() string             { return segnaposto }
func (Segreto) Format(f fmt.State, _ rune)   { _, _ = io.WriteString(f, segnaposto) }
func (Segreto) LogValue() slog.Value         { return slog.StringValue(segnaposto) }
func (Segreto) MarshalText() ([]byte, error) { return []byte(segnaposto), nil }
func (s Segreto) Presente() bool             { return s.chiave != nil }

// Nuovo fa un Segreto dal secret_key_base (i test e il banco, con valori inventati). Vuoto è
// l'assenza. Le chiavi derivate restano in memoria, una per sale.
func Nuovo(base string) Segreto {
	if base == "" {
		return Segreto{}
	}
	var mu sync.Mutex
	chiavi := map[string][]byte{}
	return Segreto{chiave: func(sale string) []byte {
		mu.Lock()
		defer mu.Unlock()
		if k, ok := chiavi[sale]; ok {
			return k
		}
		k, err := pbkdf2.Key(sha256.New, base, []byte(sale), giri, lunghezza)
		if err != nil {
			return nil // solo con parametri fuori dai limiti FIPS: qui non succede, e nil rifiuta
		}
		chiavi[sale] = k
		return k
	}}
}

// Leggi legge il secret_key_base dal file privato percorso con le regole di segreti.LeggiFile e di
// runtime.exs dello standalone (al più 4096 byte, almeno 64 senza gli spazi ai bordi). Un file che
// non c'è è l'assenza (nessun errore): ogni Phoenix.Token è rifiutato. Ogni altro problema è un
// errore che non riporta mai il contenuto.
func Leggi(percorso string) (Segreto, error) {
	v, err := segreti.LeggiFile(percorso, 1, segreti.MassimoFile)
	if errors.Is(err, fs.ErrNotExist) {
		return Segreto{}, nil
	}
	if err != nil {
		return Segreto{}, err
	}
	v = strings.TrimFunc(v, unicode.IsSpace)
	if len(v) < MinimoSegreto {
		return Segreto{}, fmt.Errorf("segreto %s: deve avere almeno %d byte, senza gli spazi ai bordi", percorso, MinimoSegreto)
	}
	return Nuovo(v), nil
}

func (s Segreto) mac(sale, testo string) []byte {
	k := s.chiave(sale)
	if k == nil {
		return nil
	}
	m := hmac.New(sha256.New, k)
	m.Write([]byte(testo))
	return m.Sum(nil)
}

// VerificaIntero è Phoenix.Token.verify(endpoint, sale, gettone, max_age: durata) per un dato
// intero: il dato e true se il gettone è firmato con questo segreto e questo sale, non dopo adesso e
// non scaduto; altrimenti false. Il confronto della firma è a tempo costante.
func (s Segreto) VerificaIntero(sale, gettone string, durata time.Duration, adesso time.Time) (int64, bool) {
	if !s.Presente() {
		return 0, false
	}
	parti := strings.Split(gettone, ".")
	if len(parti) != 3 || parti[0] != intestazione {
		return 0, false
	}
	payload, err1 := b64.DecodeString(parti[1])
	firma, err2 := b64.DecodeString(parti[2])
	if err1 != nil || err2 != nil {
		return 0, false
	}
	atteso := s.mac(sale, parti[0]+"."+parti[1])
	if atteso == nil || !hmac.Equal(firma, atteso) {
		return 0, false
	}
	dato, firmato, _, ok := leggiTermine(payload)
	if !ok {
		return 0, false
	}
	ora := adesso.UnixMilli()
	if firmato > ora {
		return 0, false // :invalid di Plug.Crypto 2.2.0
	}
	// firmato + max_age < adesso è :expired; firmato ≤ ora qui, quindi la somma non trabocca se
	// la durata è ragionevole (90 giorni).
	if d := durata.Milliseconds(); firmato < math.MinInt64+d || firmato+d < ora {
		return 0, false
	}
	return dato, true
}

// FirmaIntero è Phoenix.Token.sign(endpoint, sale, dato, signed_at: firmatoIl) senza altre opzioni
// (max_age 86400 nel gettone): solo per i test e il banco. "" senza segreto.
func (s Segreto) FirmaIntero(sale string, dato int64, firmatoIl time.Time) string {
	if !s.Presente() {
		return ""
	}
	t := []byte{131, 104, 3}
	t = scriviIntero(t, dato)
	t = scriviIntero(t, firmatoIl.UnixMilli())
	t = scriviIntero(t, maxAgeFirma)
	return s.firmaTermine(sale, t)
}

func (s Segreto) firmaTermine(sale string, termine []byte) string {
	testo := intestazione + "." + b64.EncodeToString(termine)
	return testo + "." + b64.EncodeToString(s.mac(sale, testo))
}

// scriviIntero scrive n come term_to_binary: SMALL_INTEGER_EXT da 0 a 255, INTEGER_EXT entro 32 bit
// con segno, altrimenti SMALL_BIG_EXT con le cifre in byte dal meno significativo, senza zeri in cima.
func scriviIntero(b []byte, n int64) []byte {
	switch {
	case n >= 0 && n <= 255:
		return append(b, 97, byte(n))
	case n >= math.MinInt32 && n <= math.MaxInt32:
		return binary.BigEndian.AppendUint32(append(b, 98), uint32(int32(n)))
	}
	segno, m := byte(0), uint64(n)
	if n < 0 {
		segno, m = 1, uint64(-n) // -MinInt64 trabocca ma uint64 lo legge giusto
	}
	var cifre []byte
	for ; m > 0; m >>= 8 {
		cifre = append(cifre, byte(m))
	}
	return append(append(b, 110, byte(len(cifre)), segno), cifre...)
}

// leggiTermine legge {dato, firmato, durata}: versione 131, SMALL_TUPLE_EXT di tre interi, niente
// dopo.
func leggiTermine(b []byte) (dato, firmato, durata int64, ok bool) {
	if len(b) < 3 || b[0] != 131 || b[1] != 104 || b[2] != 3 {
		return 0, 0, 0, false
	}
	resto := b[3:]
	var n [3]int64
	for i := range n {
		if n[i], resto, ok = leggiIntero(resto); !ok {
			return 0, 0, 0, false
		}
	}
	if len(resto) != 0 {
		return 0, 0, 0, false
	}
	return n[0], n[1], n[2], true
}

// leggiIntero legge un intero all'inizio di b e dà il resto. Accetta anche forme non canoniche (un
// valore piccolo in INTEGER_EXT o SMALL_BIG_EXT, cifre zero in cima, zero col segno meno), che
// term_to_binary non scrive: è sicuro perché si legge solo un payload già autenticato dall'HMAC, che
// soltanto chi ha la chiave può produrre; qui conta solo che il valore stia in int64.
func leggiIntero(b []byte) (int64, []byte, bool) {
	if len(b) < 2 {
		return 0, nil, false
	}
	switch b[0] {
	case 97: // SMALL_INTEGER_EXT
		return int64(b[1]), b[2:], true
	case 98: // INTEGER_EXT
		if len(b) < 5 {
			return 0, nil, false
		}
		return int64(int32(binary.BigEndian.Uint32(b[1:5]))), b[5:], true
	case 110: // SMALL_BIG_EXT
		if len(b) < 3 {
			return 0, nil, false
		}
		n, segno := int(b[1]), b[2]
		if n > 8 || segno > 1 || len(b) < 3+n {
			return 0, nil, false
		}
		var m uint64
		for i := n - 1; i >= 0; i-- {
			m = m<<8 | uint64(b[3+i])
		}
		if m > math.MaxInt64 {
			return 0, nil, false
		}
		v := int64(m)
		if segno == 1 {
			v = -v
		}
		return v, b[3+n:], true
	}
	return 0, nil, false
}
