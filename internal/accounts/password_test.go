package accounts

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
)

// Hash generati dallo standalone (go-baseline) con Accounts.Password.hash/1, su un'istanza
// sintetica del banco, il 30/10/2026. Le password sono inventate.
var vettoriStandalone = []struct{ password, hash string }{
	{"password-sintetica-di-prova", "pbkdf2_sha256$600000$K9uSaX9dEHUZ1MGH/EC7rQ==$9K8s/4Ie/nMeKw1WAleOsdo0i5dqekagfkXykuGCxzY="},
	{"àèìòù-unicode-✓-prova", "pbkdf2_sha256$600000$pzT50rD2juUOvaVHGUzs8Q==$3m6qYUSgLsQJYHkN1eSiKbZeVPVhJB8GEduylFzPRUo="},
	{"", "pbkdf2_sha256$600000$wcyIBV9RL0k9BgOtcSENYw==$mr9rKcFFwwyHv579/5y+OGnQDxf++rtRRy6yHqHAwgU="},
}

// inventario: ACC-15
// Il Go verifica gli hash dello standalone: si importano senza far reimpostare le password.
func TestVerificaHashDelloStandalone(t *testing.T) {
	for _, v := range vettoriStandalone {
		if !Verifica(v.password, v.hash) {
			t.Errorf("%q: hash dello standalone rifiutato", v.password)
		}
		if Verifica(v.password+"x", v.hash) {
			t.Errorf("%q: password sbagliata accettata", v.password)
		}
	}
}

// inventario: ACC-15
// Le varianti, tutte misurate sullo standalone il 30/10/2026 (due giri): segno e zeri iniziali
// nelle iterazioni valgono, e anche il base64 con bit di coda non nulli; base64 senza padding,
// a capo o ritorno carrello ovunque, $ in più, maiuscole, spazio iniziale, derivata di 31 o 64
// byte no; sotto 600.000 iterazioni l'hash è rifiutato. Le iterazioni enormi nello standalone
// sollevano un'eccezione, nel Go danno no (divergenza dal lato sicuro, IterazioniMassime).
func TestVerificaVarianti(t *testing.T) {
	pw, h := vettoriStandalone[0].password, vettoriStandalone[0].hash
	parti := strings.Split(h, "$")
	unisci := func(p ...string) string { return strings.Join(p, "$") }
	casi := map[string]bool{
		unisci(parti[0], "+600000", parti[2], parti[3]):                        true,
		unisci(parti[0], "0600000", parti[2], parti[3]):                        true,
		unisci(parti[0], parti[1], strings.TrimRight(parti[2], "="), parti[3]): false,
		unisci(parti[0], parti[1], parti[2], strings.TrimRight(parti[3], "=")): false,
		h + "\n":   false,
		h + "\r\n": false,
		h + "\r":   false,
		unisci(parti[0], parti[1], parti[2][:10]+"\n"+parti[2][10:], parti[3]): false,
		h + "$x": false,
		"PBKDF2_SHA256" + strings.TrimPrefix(h, "pbkdf2_sha256"): false,
		" " + h: false,
		unisci(parti[0], "599999", parti[2], parti[3]):               false,
		unisci(parti[0], "-600000", parti[2], parti[3]):              false,
		unisci(parti[0], "600000x", parti[2], parti[3]):              false,
		unisci(parti[0], "99999999999999999999", parti[2], parti[3]): false,
		unisci(parti[0], parti[1], parti[2], parti[3][:40]+"AA=="):   false, // derivata di 31 byte
		unisci(parti[0], parti[1], parti[2], parti[3]+parti[3]):      false,
		"": false,
		unisci(parti[0], parti[1], strings.TrimSuffix(parti[2], "rQ==")+"rR==", parti[3]): true,
		unisci(parti[0], parti[1], parti[2], strings.TrimSuffix(parti[3], "zY=")+"zZ="):   true,
		unisci(parti[0], "6000001", parti[2], parti[3]):                                   false,
	}
	for hash, atteso := range casi {
		if got := Verifica(pw, hash); got != atteso {
			t.Errorf("Verifica(%q) = %v, atteso %v", hash, got, atteso)
		}
	}
}

// inventario: ACC-15
// Un hash nuovo ha la forma dello standalone: 600.000 iterazioni, sale di 16 byte, derivata di
// 32 byte, base64 con padding; sale nuovo ogni volta.
func TestHash(t *testing.T) {
	h1, h2 := Hash("password-sintetica-di-prova"), Hash("password-sintetica-di-prova")
	if h1 == h2 {
		t.Error("due hash uguali: il sale non cambia")
	}
	p := strings.Split(h1, "$")
	if len(p) != 4 || p[0] != "pbkdf2_sha256" || p[1] != "600000" || len(p[2]) != 24 || len(p[3]) != 44 {
		t.Errorf("forma dell'hash: %q", h1)
	}
	if !Verifica("password-sintetica-di-prova", h1) || Verifica("altra-password-sintetica", h1) {
		t.Error("l'hash nuovo non si verifica come atteso")
	}
	if Iterazioni != 600_000 {
		t.Errorf("Iterazioni = %d: in produzione devono restare 600.000", Iterazioni)
	}
}

// inventario: ACC-15
// Minimo 12 caratteri contati come String.length di Elixir (grafemi), misurato sullo
// standalone: 11 «é» scomposte sono 11, 12 emoji famiglia sono 12, 12 «\r\n» sono 12.
func TestLunghezzaPassword(t *testing.T) {
	casi := map[string]bool{
		strings.Repeat("a", 11):     false,
		strings.Repeat("a", 12):     true,
		strings.Repeat("é", 11):    false, // 22 rune, 11 grafemi
		strings.Repeat("é", 12):    true,
		strings.Repeat("👨‍👩‍👧", 11): false,
		strings.Repeat("👨‍👩‍👧", 12): true,
		strings.Repeat("\r\n", 11):  false,
		strings.Repeat("\r\n", 12):  true,
		strings.Repeat("à", 12):     true,
		strings.Repeat("क्क", 6):    true, // misurato: Elixir dà 12
		strings.Repeat("\xff", 12):  true, // misurato: 12
	}
	for pw, atteso := range casi {
		if got := PasswordAbbastanzaLunga(pw); got != atteso {
			t.Errorf("PasswordAbbastanzaLunga(%q) = %v, atteso %v", pw, got, atteso)
		}
	}
	if LunghezzaMinima != 12 {
		t.Errorf("LunghezzaMinima = %d", LunghezzaMinima)
	}
}

// conIterazioni costruisce un hash valido con un numero di iterazioni scelto, con crypto/pbkdf2
// direttamente (non con deriva, che è sotto prova).
func conIterazioni(password string, iter int) string {
	sale := []byte("sale-di-16-byte!")
	k, err := pbkdf2.Key(sha256.New, password, sale, iter, 32)
	if err != nil {
		panic(err)
	}
	return strings.Join([]string{"pbkdf2_sha256", strconv.Itoa(iter), base64.StdEncoding.EncodeToString(sale),
		base64.StdEncoding.EncodeToString(k)}, "$")
}

// inventario: ACC-15
// Un hash corretto ma con meno di 600.000 iterazioni è rifiutato (non accettato e aggiornato);
// uno con più iterazioni vale, con le sue iterazioni.
func TestVerificaIterazioni(t *testing.T) {
	pw := "password-sintetica-di-prova"
	if Verifica(pw, conIterazioni(pw, 1000)) {
		t.Error("hash con 1000 iterazioni accettato")
	}
	if !Verifica(pw, conIterazioni(pw, 700_000)) {
		t.Error("hash con 700.000 iterazioni rifiutato")
	}
}

// inventario: ACC-15
// Il sale vuoto vale anche nello standalone (misurato): l'hash con sale vuoto si verifica. Oltre
// IterazioniMassime l'hash è rifiutato senza calcolare niente.
func TestVerificaSaleVuotoETetto(t *testing.T) {
	k, err := pbkdf2.Key(sha256.New, "x", nil, Iterazioni, 32)
	if err != nil {
		t.Fatal(err)
	}
	if !Verifica("x", "pbkdf2_sha256$600000$$"+base64.StdEncoding.EncodeToString(k)) {
		t.Error("hash con sale vuoto rifiutato")
	}
	if IterazioniMassime != 6_000_000 {
		t.Errorf("IterazioniMassime = %d", IterazioniMassime)
	}
	if Verifica("password-sintetica-di-prova", conIterazioni("password-sintetica-di-prova", IterazioniMassime+1)) {
		t.Error("hash oltre il tetto accettato")
	}
}
