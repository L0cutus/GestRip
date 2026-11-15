package firmephoenix

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// I vettori veri: generati il 15/11/2026 con Plug.Crypto 2.2.0 dell'immagine dello standalone
// (gestrip-standalone:fab-225, senza rete), Plug.Crypto.sign(kb, sale, dato, signed_at: s), che è
// quello che Phoenix.Token.sign/4 chiama con il secret_key_base dell'endpoint. Segreto inventato.
const (
	baseVettori = "SINTETICO-secret-key-base-di-prova-SINTETICO-secret-key-base-di-prova-SINTETICO-secret-key-base-di-prova-"
	saleVettori = "artisan bag projection"
	novantaG    = 90 * 24 * time.Hour
)

var vettori = []struct {
	nome    string
	gettone string
	dato    int64
	firmato int64 // millisecondi
}{
	{"piccolo", "SFMyNTY.g2gDYQRuBgAA2MMsuwNiAAFRgA.WqrRd0_Mf8eUNzBnPEJ9erquRzI0JkRAAk2FkWTuba8", 4, 4102444800000},
	{"intero", "SFMyNTY.g2gDYgAAASxuBgAAHv16mgFiAAFRgA.RYTofGbBfwA58-hdDno5jUw6bELA1jRy9MFw45aVHOM", 300, 1763000000000},
	{"millisecondi", "SFMyNTY.g2gDYgABEXBuBgD0H_16mgFiAAFRgA.1kmqkY2x0fm_sbCH-LGy0u8xRtfAzWQwW2yiudafPcE", 70000, 1763000000500},
	{"grande", "SFMyNTY.g2gDbgUAAPIFKgFuBgAAHv16mgFiAAFRgA.8iSMBsmqWx3weI9i81I1xbJIiQMrcUwGShRLqEgG8o4", 5000000000, 1763000000000},
	{"vecchio", "SFMyNTY.g2gDYQRuBgAAaOXPiwFiAAFRgA.y9ib1FtndLoi10_2b8nlmdDWHxOJo1_pE4AXme8e2vg", 4, 1700000000000},
	// max_age 1 scritto nel gettone: conta quello della verifica (opts vince), come Plug.Crypto.
	{"max_age nel gettone", "SFMyNTY.g2gDYQRuBgAAHv16mgFhAQ.kinUfq_UKCtPSCZBEzfHHmvzYOLg6fBpTkT1aQDkqOw", 4, 1763000000000},
}

const (
	altroSale  = "SFMyNTY.g2gDYQRuBgAA2MMsuwNiAAFRgA.raG-cgNP9aPfbYDWDhja0kHkfb0EsgwJlimBlHyT0UU"      // sale «altro sale»
	datoTesto  = "SFMyNTY.g2gDbQAAAAE0bgYAANjDLLsDYgABUYA.CSwQ8XBjG1R3dlMTT3_QMSZ7sEB8Q46ijEb8NXrQWho" // dato "4"
	chiaveVera = "936bc418f482b27f730406c8e43b4c495fe7947d4261cd9864a3fe0c73dbe37b"                    // KeyGenerator.generate/3
)

func ms(n int64) time.Time { return time.UnixMilli(n) }

// inventario: ART-23
// La chiave è quella di Plug.Crypto.KeyGenerator (PBKDF2-SHA256, 1000 giri, 32 byte) e ogni vettore
// vero si apre con il suo dato, dall'istante della firma fino a 90 giorni dopo compresi.
func TestVettoriVeri(t *testing.T) {
	s := Nuovo(baseVettori)
	if got := fmt.Sprintf("%x", s.chiave(saleVettori)); got != chiaveVera {
		t.Fatalf("chiave %s", got)
	}
	for _, v := range vettori {
		for _, adesso := range []int64{v.firmato, v.firmato + novantaG.Milliseconds()} {
			if d, ok := s.VerificaIntero(saleVettori, v.gettone, novantaG, ms(adesso)); !ok || d != v.dato {
				t.Errorf("%s a %d: %d %v", v.nome, adesso, d, ok)
			}
		}
		// Plug.Crypto 2.2.0: un gettone firmato dopo adesso è :invalid, uno scaduto :expired.
		for _, adesso := range []int64{v.firmato - 1, v.firmato + novantaG.Milliseconds() + 1} {
			if _, ok := s.VerificaIntero(saleVettori, v.gettone, novantaG, ms(adesso)); ok {
				t.Errorf("%s a %d: accettato", v.nome, adesso)
			}
		}
	}
}

// inventario: ART-23
// La firma del Go dà gli stessi byte di Plug.Crypto per lo stesso dato e lo stesso istante (max_age
// 86400 nel gettone, come Phoenix.Token.sign/4 senza opzioni).
func TestFirmaComePlug(t *testing.T) {
	s := Nuovo(baseVettori)
	for _, v := range vettori[:5] {
		if g := s.FirmaIntero(saleVettori, v.dato, ms(v.firmato)); g != v.gettone {
			t.Errorf("%s: %s", v.nome, g)
		}
	}
	if s.FirmaIntero(saleVettori, 4, ms(1)) == "" || (Segreto{}).FirmaIntero(saleVettori, 4, ms(1)) != "" {
		t.Error("firma senza segreto")
	}
}

// inventario: ART-24
// Ogni gettone che non è un vettore giusto è rifiutato: un altro sale, un altro segreto, nessun
// segreto, un dato che non è un intero, un carattere toccato in ognuna delle tre parti, parti in più
// o in meno, l'intestazione di un altro algoritmo, il base64 con il riempimento.
func TestRifiuti(t *testing.T) {
	s := Nuovo(baseVettori)
	buono := vettori[1]
	adesso := ms(buono.firmato + 1000)
	rifiuta := func(nome string, s Segreto, sale, g string) {
		t.Helper()
		if d, ok := s.VerificaIntero(sale, g, novantaG, adesso); ok {
			t.Errorf("%s: accettato (%d)", nome, d)
		}
	}
	rifiuta("altro sale", s, saleVettori, strings.Replace(altroSale, "AA2MMsuwN", "AAHv16mgF", 1))
	rifiuta("altro sale vero", s, saleVettori, altroSale)
	rifiuta("dato testo", s, saleVettori, datoTesto)
	rifiuta("altro segreto", Nuovo(baseVettori+"x"), saleVettori, buono.gettone)
	rifiuta("nessun segreto", Segreto{}, saleVettori, buono.gettone)
	rifiuta("vuoto", s, saleVettori, "")
	rifiuta("sale sbagliato", s, "artisan bag projectio", buono.gettone)
	parti := strings.Split(buono.gettone, ".")
	for i := range parti {
		for _, c := range []byte{'A', 'B', '_', '-'} {
			p := append([]string(nil), parti...)
			b := []byte(p[i])
			if b[len(b)/2] == c {
				continue
			}
			b[len(b)/2] = c
			p[i] = string(b)
			rifiuta(fmt.Sprintf("parte %d toccata", i), s, saleVettori, strings.Join(p, "."))
		}
	}
	rifiuta("due parti", s, saleVettori, parti[0]+"."+parti[1])
	rifiuta("quattro parti", s, saleVettori, buono.gettone+".AA")
	rifiuta("riempimento", s, saleVettori, buono.gettone+"=")
	rifiuta("HS512", s, saleVettori, "SFM1MTI."+parti[1]+"."+parti[2])
	// Un'altra intestazione con l'HMAC-SHA256 giusto su quei byte: Plug.Crypto calcolerebbe l'HS512, qui è un rifiuto.
	altra := "SFM1MTI." + parti[1]
	rifiuta("HS512 firmato", s, saleVettori, altra+"."+base64.RawURLEncoding.EncodeToString(s.mac(saleVettori, altra)))
	rifiuta("spazio", s, saleVettori, " "+buono.gettone)
}

// inventario: ART-24
// Il termine Erlang si legge solo nella forma {intero, intero, intero}: tutto il resto (un'altra
// versione, una tupla di due o quattro, un dato testo, un intero troppo grande, byte in più o in
// meno) è rifiutato anche con la firma giusta.
func TestTermine(t *testing.T) {
	buoni := map[string]struct {
		b                     []byte
		dato, firmato, durata int64
	}{
		"vettore":  {[]byte{131, 104, 3, 98, 0, 0, 1, 44, 110, 6, 0, 0, 30, 253, 122, 154, 1, 98, 0, 1, 81, 128}, 300, 1763000000000, 86400},
		"negativo": {[]byte{131, 104, 3, 98, 255, 255, 255, 255, 110, 1, 1, 5, 97, 0}, -1, -5, 0},
		"grande 8": {[]byte{131, 104, 3, 110, 8, 0, 255, 255, 255, 255, 255, 255, 255, 127, 97, 1, 97, 1}, 1<<63 - 1, 1, 1},
	}
	for nome, x := range buoni {
		d, f, m, ok := leggiTermine(x.b)
		if !ok || d != x.dato || f != x.firmato || m != x.durata {
			t.Errorf("%s: %d %d %d %v", nome, d, f, m, ok)
		}
	}
	cattivi := map[string][]byte{
		"vuoto":            {},
		"versione":         {130, 104, 3, 97, 1, 97, 1, 97, 1},
		"tupla di due":     {131, 104, 2, 97, 1, 97, 1},
		"tupla di quattro": {131, 104, 4, 97, 1, 97, 1, 97, 1, 97, 1},
		"arità 4 con tre":  {131, 104, 4, 97, 1, 97, 1, 97, 1},
		"tupla grande":     {131, 105, 0, 0, 0, 3, 97, 1, 97, 1, 97, 1},
		"testo":            {131, 104, 3, 109, 0, 0, 0, 1, 52, 97, 1, 97, 1},
		"virgola mobile":   {131, 104, 3, 97, 1, 97, 1, 70, 64, 0, 0, 0, 0, 0, 0, 0},
		"byte in più":      {131, 104, 3, 97, 1, 97, 1, 97, 1, 0},
		"corto":            {131, 104, 3, 97, 1, 97, 1, 97},
		"intero corto":     {131, 104, 3, 98, 0, 0, 1, 97, 1, 97, 1},
		"grande 9":         {131, 104, 3, 110, 9, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 97, 1, 97, 1},
		"grande oltre":     {131, 104, 3, 110, 8, 0, 0, 0, 0, 0, 0, 0, 0, 128, 97, 1, 97, 1},
		"segno 2":          {131, 104, 3, 110, 1, 2, 1, 97, 1, 97, 1},
		"grande corto":     {131, 104, 3, 110, 4, 0, 1, 97, 1, 97, 1},
		"compresso":        {131, 80, 0, 0, 0, 9, 120, 156},
		"mappa":            {131, 116, 0, 0, 0, 0},
	}
	for nome, b := range cattivi {
		if _, _, _, ok := leggiTermine(b); ok {
			t.Errorf("%s: accettato", nome)
		}
	}
}

// inventario: ART-23
// Un gettone con il termine giusto ma il dato che non è un intero positivo per la busta non apre: lo
// decide il chiamante; qui il dato torna com'è (anche 0 o negativo).
func TestDatoComE(t *testing.T) {
	s := Nuovo(baseVettori)
	g := s.firmaTermine(saleVettori, []byte{131, 104, 3, 97, 0, 110, 6, 0, 0, 30, 253, 122, 154, 1, 98, 0, 1, 81, 128})
	if d, ok := s.VerificaIntero(saleVettori, g, novantaG, ms(1763000000000)); !ok || d != 0 {
		t.Errorf("%d %v", d, ok)
	}
	if _, err := base64.RawURLEncoding.DecodeString(strings.Split(g, ".")[1]); err != nil {
		t.Error(err)
	}
}

// inventario: ART-23
// Il segreto non esce mai: fmt con ogni verbo, slog, JSON, anche dentro un'altra struttura.
func TestNonSiStampa(t *testing.T) {
	s := Nuovo(baseVettori)
	var b bytes.Buffer
	slog.New(slog.NewTextHandler(&b, nil)).Info("x", "s", s)
	j, _ := json.Marshal(struct{ S Segreto }{s})
	for _, out := range []string{fmt.Sprint(s), fmt.Sprintf("%v %+v %#v %x %s %d", s, s, s, s, s, s),
		fmt.Sprintf("%+v %#v", struct{ s Segreto }{s}, struct{ s Segreto }{s}), b.String(), string(j)} {
		if strings.Contains(out, "SINTETICO") || strings.Contains(out, "secret-key") {
			t.Errorf("stampato: %s", out)
		}
	}
}

// inventario: ART-23
// Leggi ha le regole di SECRET_KEY_BASE_FILE dello standalone (file privato, 64…4096 byte, UTF-8,
// almeno 64 byte senza gli spazi ai bordi): un file che non c'è è l'assenza, ogni altro problema un
// errore che non riporta il contenuto.
func TestLeggi(t *testing.T) {
	dir := t.TempDir()
	scrivi := func(nome, v string, perm os.FileMode) string {
		p := filepath.Join(dir, nome)
		if err := os.WriteFile(p, []byte(v), perm); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, perm); err != nil {
			t.Fatal(err)
		}
		return p
	}
	s, err := Leggi(filepath.Join(dir, "manca"))
	if err != nil || s.Presente() {
		t.Errorf("assente: %v %v", s.Presente(), err)
	}
	s, err = Leggi(scrivi("buono", "  "+baseVettori+"\n", 0o600))
	if err != nil || !s.Presente() {
		t.Fatalf("buono: %v", err)
	}
	if d, ok := s.VerificaIntero(saleVettori, vettori[1].gettone, novantaG, ms(vettori[1].firmato)); !ok || d != 300 {
		t.Error("letto con gli spazi ai bordi")
	}
	for nome, x := range map[string]struct {
		v    string
		perm os.FileMode
	}{
		"corto":           {"SINTETICO" + strings.Repeat("a", 54), 0o600},
		"corto con spazi": {"SINTETICO" + strings.Repeat("a", 50) + "          ", 0o600},
		"permessi":        {baseVettori, 0o640},
		"non UTF-8":       {baseVettori + "\xff", 0o600},
		"lungo":           {strings.Repeat("SINTETICO", 456), 0o600},
	} {
		_, err := Leggi(scrivi(nome, x.v, x.perm))
		if err == nil || strings.Contains(err.Error(), "SINTETICO") {
			t.Errorf("%s: %v", nome, err)
		}
	}
}
