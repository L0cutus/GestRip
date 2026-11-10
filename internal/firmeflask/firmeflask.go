// Package firmeflask verifica le credenziali dei link che la vecchia app Flask ha mandato ai clienti
// e che il Go deve ancora aprire (PIANO §2.4; CLI-16, CLI-20; più avanti ART-26, VEN-30 e le
// garanzie /g/), come LegacyPublicLinks dello standalone (go-baseline):
//   - le firme: HMAC-SHA256 con il segreto Flask, in esadecimale minuscolo, troncato (32 caratteri;
//     16 per l'artigiano), sul messaggio fatto delle parti unite con «:» più la scadenza;
//   - la scadenza: solo cifre ASCII, letta come numero (gli zeri davanti non contano), valida se
//     non è prima di adesso (exp ≥ adesso, in secondi Unix);
//   - il confronto a tempo costante;
//   - senza segreto ogni firma è rifiutata;
//   - il codice breve dei vecchi /stato (?pw=): SHA-256 di «<busta>:<CODICE>», con il codice senza
//     spazi ai bordi e in maiuscolo, confrontato a tempo costante con l'impronta importata.
//
// Oggi il Go porta solo la pagina /stato e le foto /foto e /thumb (slice 064); le altre famiglie
// (artigiano, garanzia, foto delle vendite) useranno firma con le loro parti.
//
// Il segreto sta in un tipo che non si stampa: fmt, slog e JSON scrivono solo un segnaposto, e dentro
// un'altra struttura fmt ne vede solo l'indirizzo.
package firmeflask

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"strconv"
	"strings"
	"unicode"

	"github.com/L0cutus/gestrip/internal/segreti"
)

// MinimoSegreto e MassimoSegreto sono i limiti del file del segreto (PIANO §2.4).
const (
	MinimoSegreto  = 16
	MassimoSegreto = segreti.MassimoFile
)

// Le lunghezze delle firme: 32 caratteri esadecimali per lo stato e le foto, 16 per l'artigiano.
const (
	lunghezzaFirma = 32
)

// Segreto è il segreto Flask. Il valore zero è «assente»: ogni firma è rifiutata.
//
// I byte stanno solo dentro la funzione mac: fmt arriva ai campi di una struttura anche senza i suoi
// metodi (un Segreto in un campo non esportato), ma di una funzione stampa solo l'indirizzo.
type Segreto struct{ mac func(messaggio string) []byte }

const segnaposto = "[segreto Flask]"

// String, GoString, Format, LogValue e MarshalText non mostrano mai il segreto.
func (Segreto) String() string   { return segnaposto }
func (Segreto) GoString() string { return segnaposto }

// Format vale per ogni verbo di fmt (anche %x e %d, che altrimenti stamperebbero i byte).
func (Segreto) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, segnaposto) }

// LogValue è il segnaposto anche nel registro (slog).
func (Segreto) LogValue() slog.Value { return slog.StringValue(segnaposto) }

// MarshalText è il segnaposto anche in JSON.
func (Segreto) MarshalText() ([]byte, error) { return []byte(segnaposto), nil }

// Presente dice se c'è un segreto.
func (s Segreto) Presente() bool { return s.mac != nil }

// Nuovo fa un Segreto dal valore (i test e il banco, con valori inventati). Un valore vuoto è
// l'assenza.
func Nuovo(valore string) Segreto {
	if valore == "" {
		return Segreto{}
	}
	k := []byte(valore)
	return Segreto{mac: func(messaggio string) []byte {
		m := hmac.New(sha256.New, k)
		m.Write([]byte(messaggio))
		return m.Sum(nil)
	}}
}

// Leggi legge il segreto dal file privato percorso, con le regole di segreti.LeggiFile (file
// regolare, non un link, 0600 o 0400, al più 4096 byte, UTF-8) e senza gli spazi ai bordi come
// String.trim/1 di config/runtime.exs dello standalone, che ne vuole almeno MinimoSegreto byte. Un
// file che non c'è è l'assenza (nessun errore): lo standalone senza LEGACY_WARRANTY_SECRET_FILE
// rifiuta ogni firma. Ogni altro problema è un errore, che non riporta mai il contenuto.
func Leggi(percorso string) (Segreto, error) {
	v, err := segreti.LeggiFile(percorso, MinimoSegreto, MassimoSegreto)
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

// Scadenza è la scadenza di una firma letta come la legge decimal/1: solo cifre ASCII, almeno una.
// Testo è il numero senza gli zeri davanti (Integer.to_string/1), quello che entra nel messaggio
// firmato.
type scadenza struct{ testo string }

func leggiScadenza(s string) (scadenza, bool) {
	if s == "" {
		return scadenza{}, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return scadenza{}, false
		}
	}
	t := strings.TrimLeft(s, "0")
	if t == "" {
		t = "0"
	}
	return scadenza{testo: t}, true
}

// nonPrimaDi dice se la scadenza è adesso o dopo (expires_at >= now). Elixir non ha limiti sugli
// interi: un numero oltre int64 è sempre dopo.
func (e scadenza) nonPrimaDi(adesso int64) bool {
	n, err := strconv.ParseInt(e.testo, 10, 64)
	if err != nil {
		return true // solo cifre e oltre int64: più grande di ogni adesso
	}
	return n >= adesso
}

// firmaValida è valid_signature?/2: esattamente n caratteri esadecimali minuscoli.
func firmaValida(f string, n int) bool {
	if len(f) != n {
		return false
	}
	for i := 0; i < len(f); i++ {
		c := f[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// hmacTroncato è hmac/3: l'HMAC-SHA256 del messaggio in esadecimale minuscolo, primi n caratteri.
func (s Segreto) hmacTroncato(messaggio string, n int) string {
	return hex.EncodeToString(s.mac(messaggio))[:n]
}

// verifica è verify_expiring/6: segreto presente, scadenza fatta di cifre e non passata, firma
// della forma giusta e uguale (a tempo costante) a quella calcolata sulle parti e sulla scadenza.
func (s Segreto) verifica(parti []string, firma, scad string, adesso int64, n int) bool {
	if !s.Presente() {
		return false
	}
	e, ok := leggiScadenza(scad)
	if !ok || !e.nonPrimaDi(adesso) || !firmaValida(firma, n) {
		return false
	}
	atteso := s.hmacTroncato(strings.Join(append(parti, e.testo), ":"), n)
	return subtle.ConstantTimeCompare([]byte(firma), []byte(atteso)) == 1
}

// firma è sign_expiring/4: "" senza segreto o con una scadenza che non è fatta di cifre.
func (s Segreto) firma(parti []string, scad string, n int) string {
	if !s.Presente() {
		return ""
	}
	e, ok := leggiScadenza(scad)
	if !ok {
		return ""
	}
	return s.hmacTroncato(strings.Join(append(parti, e.testo), ":"), n)
}

// VerificaStato è verify_status/5: la firma t di /stato/<busta>/<anno>?t=&exp= sul numero di busta
// numero («stato:<numero>:<exp>»), che non può essere vuoto.
func (s Segreto) VerificaStato(numero, firma, scad string, adesso int64) bool {
	if numero == "" {
		return false
	}
	return s.verifica([]string{"stato", numero}, firma, scad, adesso, lunghezzaFirma)
}

// VerificaFotoBusta è verify_repair_photo/6: la firma sig di /foto/<slug>/<file>?sig=&exp= (e di
// /thumb) su slug e nome del file («<slug>:<file>:<exp>»), che non possono essere vuoti.
func (s Segreto) VerificaFotoBusta(slug, file, firma, scad string, adesso int64) bool {
	if slug == "" || file == "" {
		return false
	}
	return s.verifica([]string{slug, file}, firma, scad, adesso, lunghezzaFirma)
}

// FirmaFotoBusta è sign_repair_photo/4: la firma di una foto per la scadenza scad (in secondi
// Unix); "" senza segreto, o con slug o file vuoti (lo standalone scrive allora «sig=» vuoto).
func (s Segreto) FirmaFotoBusta(slug, file string, scad int64) string {
	if slug == "" || file == "" {
		return ""
	}
	return s.firma([]string{slug, file}, strconv.FormatInt(scad, 10), lunghezzaFirma)
}

// NormalizzaCodice è normalize_repair_code/1: il codice senza spazi ai bordi e in maiuscolo.
func NormalizzaCodice(codice string) string {
	return strings.ToUpper(strings.TrimFunc(codice, unicode.IsSpace))
}

// ImprontaCodice è repair_code_digest/2: SHA-256 di «<numero>:<CODICE>»; nil con il numero vuoto.
func ImprontaCodice(numero, codice string) []byte {
	if numero == "" {
		return nil
	}
	h := sha256.Sum256([]byte(numero + ":" + NormalizzaCodice(codice)))
	return h[:]
}

// VerificaCodice è verify_repair_code/3: l'impronta del codice per quel numero, confrontata a tempo
// costante con quella salvata, che deve essere di 32 byte.
func VerificaCodice(numero, codice string, impronta []byte) bool {
	if len(impronta) != sha256.Size {
		return false
	}
	h := ImprontaCodice(numero, codice)
	return h != nil && subtle.ConstantTimeCompare(h, impronta) == 1
}
