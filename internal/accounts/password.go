// Package accounts contiene utenti, ruoli e password (PIANO §1, `internal/accounts`).
package accounts

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/rivo/uniseg"
)

// Password come lo standalone (ACC-15, Accounts.Password): «pbkdf2_sha256$<iterazioni>$<sale
// base64>$<derivata base64>», PBKDF2-HMAC-SHA256, sale di 16 byte, derivata di 32 byte. Gli hash
// importati dallo standalone si verificano così come sono.
const (
	// Iterazioni è il lavoro di PBKDF2. Un hash con meno iterazioni è rifiutato, non aggiornato.
	Iterazioni = 600_000
	// LunghezzaMinima è la lunghezza minima di una password, in caratteri come li conta
	// String.length di Elixir (grafemi).
	LunghezzaMinima = 12

	// IterazioniMassime è un tetto contro un hash importato manipolato (con 10^12 iterazioni ogni
	// tentativo di login terrebbe un processore per ore). Lo standalone non ha tetto: con un
	// numero enorme solleva un'eccezione (misurato), con uno grande ma valido lavora per ore.
	// Divergenza dal lato sicuro: oltre il tetto l'hash è rifiutato.
	IterazioniMassime = 10 * Iterazioni

	prefisso        = "pbkdf2_sha256"
	lunghezzaSale   = 16
	lunghezzaChiave = 32
)

// Hash calcola l'hash di una password nuova.
func Hash(password string) string {
	sale := make([]byte, lunghezzaSale)
	_, _ = rand.Read(sale) // crypto/rand.Read non fallisce (in caso estremo il processo si ferma)
	return strings.Join([]string{
		prefisso, strconv.Itoa(Iterazioni),
		base64.StdEncoding.EncodeToString(sale), base64.StdEncoding.EncodeToString(derivaONiente(password, sale, Iterazioni)),
	}, "$")
}

// Verifica dice se password corrisponde all'hash, con un confronto a tempo costante. Come lo
// standalone (misurato il 30/10/2026): le iterazioni sono un intero (anche con «+» o zeri
// iniziali) non sotto Iterazioni; sale e derivata in base64 standard con padding, anche con bit
// di coda non nulli (Elixir li accetta), anche il sale vuoto. Il decoder di Go ignora gli a capo
// dentro il base64, Elixir no: un hash con un a capo è rifiutato. Oltre IterazioniMassime è
// rifiutato. Esce subito su un hash malformato: il login deve fare comunque una derivazione
// fittizia (come @dummy_hash dello standalone), per non rivelare quali account hanno un hash
// rifiutato.
func Verifica(password, hash string) bool {
	parti := strings.Split(hash, "$")
	if len(parti) != 4 || parti[0] != prefisso || strings.ContainsAny(hash, "\r\n") {
		return false
	}
	iter, err := strconv.Atoi(parti[1])
	if err != nil || iter < Iterazioni || iter > IterazioniMassime {
		return false
	}
	sale, err1 := base64.StdEncoding.DecodeString(parti[2])
	atteso, err2 := base64.StdEncoding.DecodeString(parti[3])
	if err1 != nil || err2 != nil {
		return false
	}
	calcolato, err := deriva(password, sale, iter)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(calcolato, atteso) == 1 // 0 anche se le lunghezze sono diverse
}

// deriva fallisce solo in modalità FIPS (GODEBUG=fips140=only) con parametri che FIPS non
// ammette, per esempio un sale importato più corto di 16 byte: la verifica dà no.
func deriva(password string, sale []byte, iter int) ([]byte, error) {
	return pbkdf2.Key(sha256.New, password, sale, iter, lunghezzaChiave)
}

// derivaONiente è deriva per un hash nuovo, con il sale di 16 byte che FIPS ammette: un errore
// qui è un difetto del programma.
func derivaONiente(password string, sale []byte, iter int) []byte {
	k, err := deriva(password, sale, iter)
	if err != nil {
		panic("pbkdf2 con un sale di 16 byte: " + err.Error())
	}
	return k
}

// PasswordAbbastanzaLunga dice se la password ha almeno LunghezzaMinima caratteri, contati come
// grafemi (String.length di Elixir): «é» scomposta in due rune conta uno. Misurato sullo
// standalone anche «क्क»×6 → 12 (Elixir non applica GB9c, come uniseg) e un byte non UTF-8 →
// 1. Elixir usa Unicode 16, uniseg v0.4.7 Unicode 15: possono differire solo i caratteri nuovi
// di Unicode 16. I moduli rifiutano comunque l'UTF-8 non valido (400).
func PasswordAbbastanzaLunga(password string) bool {
	return uniseg.GraphemeClusterCount(password) >= LunghezzaMinima
}
