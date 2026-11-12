package web

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// Le righe dei moduli senza JavaScript (slice 068, Dubbio 3 della 067): il browser manda il testo di una
// casella con «\r\n», LiveView manda allo standalone il valore della casella con «\n». Le caselle che
// nello standalone sono di LiveView si salvano con «\n»: la risposta della scheda, descrizione e note
// della modifica e di «Nuova riparazione» (come già il popup e i testi dei messaggi, slice 067). Dati
// inventati.

func TestACapoUnix(t *testing.T) {
	for dentro, fuori := range map[string]string{
		"": "", "una riga": "una riga", "a\r\nb": "a\nb", "a\r\n\r\nb\r\n": "a\n\nb\n", "a\nb": "a\nb",
		"a\rb": "a\rb", // un «\r» da solo non viene da una casella: resta com'è, come nel popup
	} {
		if got := aCapoUnix(dentro); got != fuori {
			t.Errorf("aCapoUnix(%q) = %q, atteso %q", dentro, got, fuori)
		}
	}
}

// inventario: RIP-S-04
// La modifica senza JavaScript: descrizione, note per il cliente e note interne con «\r\n» si salvano con
// «\n».
func TestModificaRigheDelModulo(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := dipendenteDentro(t, h)
	r := salva(t, b, id, map[string]string{"repair[description]": "Collana\r\nLucidatura\r\n", "repair[public_notes]": "Prima\r\nseconda",
		"repair[internal_notes]": "Riservata\r\n\r\naltro"})
	if !redirectAllaScheda(r, id) {
		t.Fatalf("%d %q", r.codice, r.location)
	}
	for c, v := range map[string]string{"descrizione": "Collana\nLucidatura\n", "note_pubbliche": "Prima\nseconda", "note_interne": "Riservata\n\naltro"} {
		if got := colonna(t, d, id, c); got != v {
			t.Errorf("%s = %q, atteso %q", c, got, v)
		}
	}
}

// inventario: RIP-H-30
// «Nuova riparazione» senza JavaScript: descrizione e note con «\r\n» si salvano con «\n».
func TestNuovaRigheDelModulo(t *testing.T) {
	h, d := facciaConBuste(t, nil)
	b := dipendenteDentro(t, h)
	b.vai("GET", "/nuova", nil, nil)
	v := bustaProva(b)
	v.Set("repair[description]", "Ridurre anello\r\nLucidare")
	v.Set("repair[public_notes]", "Per il cliente\r\nsu due righe")
	v.Set("repair[internal_notes]", "Interna\r\n\r\nfine")
	if r := b.vai("POST", "/nuova", v, nil); !creata(r) {
		t.Fatalf("salvataggio: %d %q", r.codice, r.location)
	}
	if n := contaSQL(t, d, `SELECT count(*) FROM buste WHERE descrizione = ? AND note_pubbliche = ? AND note_interne = ?`,
		"Ridurre anello\nLucidare", "Per il cliente\nsu due righe", "Interna\n\nfine"); n != 1 {
		t.Errorf("righe salvate con \\r\\n (%d buste giuste)", n)
	}
}

// inventario: CLI-14
// «Rispondi al cliente» senza JavaScript: la risposta con «\r\n» si scrive con «\n».
func TestRispostaRigheDelModulo(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := dipendenteDentro(t, h)
	b.vai("GET", percorsoScheda(id), nil, nil)
	r := b.vai("POST", percorsoScheda(id)+"/risposta", url.Values{"_csrf_token": {b.token}, "reply[body]": {"Pronta\r\nda lunedì"}}, nil)
	if r.codice != 302 {
		t.Fatalf("%d %q", r.codice, r.location)
	}
	if n := contaSQL(t, d, `SELECT count(*) FROM messaggi_buste WHERE busta_id = ? AND testo = ?`, id, "Pronta\nda lunedì"); n != 1 {
		t.Errorf("risposta salvata con \\r\\n (%d)", n)
	}
}

// Le caselle di più righe (<textarea>) di tutte le pagine (069c, decisione dell'11/11): quelle interne
// sono tutte fra le normalizzate a «\n» (le prove sopra, TestInviaTestoConRigheDelModulo e
// TestSalvaERipristinaTesti); quelle
// della pagina pubblica del cliente (/r/…, /m/…, /stato/…) restano come lo standalone, con «\r\n». Una
// casella nuova fa cadere il test: va normalizzata con aCapoUnix (e provata) o messa fra le pubbliche.
func TestCaselleDiPiuRigheNormalizzate(t *testing.T) {
	// Chiave «file nome»: una casella con lo stesso nome in un'altra pagina manda a un altro gestore.
	interne := map[string]bool{
		"scheda.html repair[description]": true, "scheda.html repair[public_notes]": true,
		"scheda.html repair[internal_notes]": true, "scheda.html reply[body]": true,
		"nuova.html repair[description]": true, "nuova.html repair[public_notes]": true,
		"nuova.html repair[internal_notes]": true,
		"avviso.html notice[body]":          true, "testi.html template[text]": true}
	pubbliche := map[string]bool{"pagina_cliente.html reason": true, "pagina_cliente.html testo": true}
	file, err := filepath.Glob("pagine/*.html")
	if err != nil || len(file) == 0 {
		t.Fatalf("pagine: %v", err)
	}
	reCasella := regexp.MustCompile(`(?i)<textarea[^>]*\sname=["']?([^"'\s>]+)`)
	reApertura := regexp.MustCompile(`(?i)<textarea\b`)
	visti := map[string]bool{}
	for _, f := range file {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		caselle := reCasella.FindAllStringSubmatch(string(b), -1)
		if n := len(reApertura.FindAllString(string(b), -1)); n != len(caselle) {
			t.Errorf("%s: %d <textarea> ma %d con un nome riconosciuto", filepath.Base(f), n, len(caselle))
		}
		for _, m := range caselle {
			chiave := filepath.Base(f) + " " + m[1]
			switch {
			case pubbliche[chiave], interne[chiave]:
				visti[chiave] = true
			default:
				t.Errorf("casella di più righe %q: né normalizzata né pubblica", chiave)
			}
		}
	}
	for nome := range interne {
		if !visti[nome] {
			t.Errorf("casella %q non più nelle pagine: toglila dall'elenco", nome)
		}
	}
	for nome := range pubbliche {
		if !visti[nome] {
			t.Errorf("casella pubblica %q non più nelle pagine", nome)
		}
	}
}
