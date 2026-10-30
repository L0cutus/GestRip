package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// Il limite ai tentativi falliti (slice 009), misurato sullo standalone a go-baseline il
// 30/10/2026 (ricevuta della slice).

const msgTroppi15 = "Troppi tentativi, riprova fra 15 minuti."

func dipendenzeConLimiti(u *utentiFinti) (Dipendenze, *clock.Finto) {
	o := clock.NuovoFinto(time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC))
	d := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, u)
	d.Orologio, d.Limiti = o, ratelimit.Nuovo(o)
	return d, o
}

func (b *browser) prova(email, password string) risposta {
	b.t.Helper()
	b.vai("GET", "/login", nil, nil)
	return b.vai("POST", "/login", url.Values{"_csrf_token": {b.token}, "user[email]": {email}, "user[password]": {password}}, nil)
}

func rifiutato(r risposta) bool {
	return r.codice == 422 && strings.Contains(r.corpo, msgCredenziali) && !strings.Contains(r.corpo, "Troppi tentativi")
}

func troppi(r risposta, msg string) bool {
	return r.codice == 429 && strings.Contains(r.corpo, msg) && strings.Contains(r.corpo, "Accesso laboratorio") &&
		!strings.Contains(r.corpo, msgCredenziali)
}

// inventario: ACC-20
// Cinque password sbagliate per un account dallo stesso indirizzo lo bloccano: il quinto rifiuto è
// già il 429 «Troppi tentativi, riprova fra 15 minuti.» (misurato). Bloccato, la password non si
// controlla nemmeno, anche se è giusta; l'email si normalizza come al login. Un altro indirizzo o
// un altro account non sono bloccati.
func TestLoginBloccatoPerAccount(t *testing.T) {
	u := &utentiFinti{attivo: true}
	d, _ := dipendenzeConLimiti(u)
	h := mustInterna(t, d)
	b := nuovoBrowser(t, h)
	b.ip = "203.0.113.70"
	for i := 1; i <= 4; i++ {
		if r := b.prova(" "+strings.ToUpper(emailProva)+" ", "sbagliata"); !rifiutato(r) {
			t.Fatalf("tentativo %d: %d", i, r.codice)
		}
	}
	if r := b.prova(emailProva, "sbagliata"); !troppi(r, msgTroppi15) {
		t.Fatalf("quinto tentativo: %d %q", r.codice, r.corpo)
	}
	prima := u.autentiche
	if r := b.prova(emailProva, passwordProva); !troppi(r, msgTroppi15) {
		t.Errorf("password giusta da bloccato: %d", r.codice)
	}
	if u.autentiche != prima {
		t.Error("da bloccato la password è stata verificata")
	}
	if c := cookie(b, "__Host-gestrip_sessione_interna"); strings.Contains(c, `"u":`) {
		t.Error("sessione aperta da bloccato")
	}
	altroIP := nuovoBrowser(t, h)
	altroIP.ip = "203.0.113.71"
	if r := altroIP.prova(emailProva, passwordProva); !redirectA(r, "/") {
		t.Errorf("stesso account da un altro indirizzo: %d", r.codice)
	}
	if r := b.prova("altro@esempio.it", "sbagliata"); !rifiutato(r) {
		t.Errorf("altro account dallo stesso indirizzo: %d", r.codice)
	}
}

// inventario: ACC-20
// Venti login sbagliati da un indirizzo, su account qualunque, bloccano l'indirizzo: il ventesimo
// è già 429, e poi ogni account, anche nuovo o giusto.
func TestLoginBloccatoPerIndirizzo(t *testing.T) {
	u := &utentiFinti{attivo: true}
	d, _ := dipendenzeConLimiti(u)
	b := nuovoBrowser(t, mustInterna(t, d))
	b.ip = "203.0.113.72"
	for i := 1; i <= 19; i++ {
		if r := b.prova(fmt.Sprintf("a%d@esempio.it", i), "sbagliata"); !rifiutato(r) {
			t.Fatalf("tentativo %d: %d", i, r.codice)
		}
	}
	if r := b.prova("a20@esempio.it", "sbagliata"); !troppi(r, msgTroppi15) {
		t.Fatalf("ventesimo: %d", r.codice)
	}
	if r := b.prova(emailProva, passwordProva); !troppi(r, msgTroppi15) {
		t.Errorf("account giusto da indirizzo bloccato: %d", r.codice)
	}
}

// inventario: ACC-20
// Un login riuscito azzera il conteggio dell'account (non quello dell'indirizzo).
func TestLoginRiuscitoAzzeraLAccount(t *testing.T) {
	u := &utentiFinti{attivo: true}
	d, _ := dipendenzeConLimiti(u)
	h := mustInterna(t, d)
	b := nuovoBrowser(t, h)
	b.ip = "203.0.113.73"
	for range 4 {
		b.prova(emailProva, "sbagliata")
	}
	if r := b.prova(emailProva, passwordProva); !redirectA(r, "/") {
		t.Fatalf("login giusto: %d", r.codice)
	}
	b2 := nuovoBrowser(t, h)
	b2.ip = b.ip
	for i := 1; i <= 4; i++ {
		if r := b2.prova(emailProva, "sbagliata"); !rifiutato(r) {
			t.Fatalf("dopo il login riuscito, tentativo %d: %d", i, r.codice)
		}
	}
	// L'indirizzo ha 8 falliti: altri 11 su account diversi non bloccano, il dodicesimo sì.
	for i := 1; i <= 11; i++ {
		if r := b2.prova(fmt.Sprintf("b%d@esempio.it", i), "sbagliata"); !rifiutato(r) {
			t.Fatalf("indirizzo, tentativo %d: %d", i, r.codice)
		}
	}
	if r := b2.prova("b12@esempio.it", "sbagliata"); !troppi(r, msgTroppi15) {
		t.Errorf("il conteggio dell'indirizzo è stato azzerato dal login riuscito: %d", r.codice)
	}
}

// inventario: ACC-20
// Il modulo malformato è il 422 di sempre e non conta, nemmeno da bloccato (misurato: risponde
// 422 senza guardare il blocco). Il messaggio dice i minuti che mancano, arrotondati in su; a 15
// minuti il blocco finisce.
func TestLoginMalformatoEMinuti(t *testing.T) {
	u := &utentiFinti{attivo: true}
	d, o := dipendenzeConLimiti(u)
	b := nuovoBrowser(t, mustInterna(t, d))
	b.ip = "203.0.113.74"
	for range 10 {
		b.vai("GET", "/login", nil, nil)
		if r := b.vai("POST", "/login", url.Values{"_csrf_token": {b.token}, "user[email]": {emailProva}}, nil); !rifiutato(r) {
			t.Fatalf("malformato: %d", r.codice)
		}
	}
	for i := 1; i <= 4; i++ {
		if r := b.prova(emailProva, "sbagliata"); !rifiutato(r) {
			t.Fatalf("i malformati hanno contato: tentativo %d → %d", i, r.codice)
		}
	}
	b.prova(emailProva, "sbagliata")
	b.vai("GET", "/login", nil, nil)
	if r := b.vai("POST", "/login", url.Values{"_csrf_token": {b.token}, "user[password]": {"x"}}, nil); !rifiutato(r) {
		t.Errorf("malformato da bloccato: %d", r.codice)
	}
	o.Sposta(14*time.Minute + 30*time.Second)
	if r := b.prova(emailProva, passwordProva); !troppi(r, "Troppi tentativi, riprova fra 1 minuti.") {
		t.Errorf("a 30 secondi dalla fine: %d", r.codice)
	}
	o.Imposta(time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC).Add(15 * time.Minute))
	if r := b.prova(emailProva, passwordProva); !redirectA(r, "/") {
		t.Errorf("a 15 minuti il blocco è finito: %d", r.codice)
	}
}

// inventario: ACC-20, CLI-21
// L'indirizzo che conta è quello della catena (clientip): dietro tailscaled (127.0.0.1) la voce più
// a destra non privata; quello che un cliente scrive più a sinistra non lo salva.
func TestLoginContaLIndirizzoDellaCatena(t *testing.T) {
	u := &utentiFinti{attivo: true}
	d, _ := dipendenzeConLimiti(u)
	b := nuovoBrowser(t, mustInterna(t, d))
	for i := 1; i <= 5; i++ {
		b.ip = fmt.Sprintf("198.51.100.%d, 203.0.113.75", i)
		b.prova(emailProva, "sbagliata")
	}
	b.ip = "198.51.100.99, 203.0.113.75"
	if r := b.prova(emailProva, passwordProva); !troppi(r, msgTroppi15) {
		t.Errorf("indirizzo inventato a sinistra: %d", r.codice)
	}
}

// --- Faccia pubblica ---

// famiglieAttese: le famiglie di PublicAttempts.family/1 dello standalone.
var famiglieAttese = []struct {
	percorso string
	secchio  ratelimit.Secchio // -1: non conta
	testo    string
	robots   bool
}{
	{"/stato/42/26", ratelimit.Corto, "Riparazione non disponibile", false},
	{"/stato/42/26/poll", ratelimit.Corto, "Riparazione non disponibile", false},
	{"/stato/42/26/messaggio/x", ratelimit.Corto, "Riparazione non disponibile", false},
	{"/stato/email-conferma", -1, "", false},
	{"/stato", -1, "", false},
	{"/r/tok", ratelimit.Token, "Riparazione non disponibile", false},
	{"/r/tok/poll", ratelimit.Token, "Riparazione non disponibile", false},
	{"/r/tok/foto/7", -1, "", false},
	{"/r/tok/foto/7/x", ratelimit.Token, "Riparazione non disponibile", false},
	{"/r", -1, "", false},
	{"/lotto/tok", ratelimit.Token, "Lotto non disponibile", false},
	{"/lotto/tok/x", -1, "", false},
	{"/lotto", -1, "", false},
	{"/artigiano", ratelimit.Token, "Riparazione non disponibile", false},
	{"/artigiano/12", ratelimit.Token, "Riparazione non disponibile", false},
	{"/artigiano/12/foto/3", -1, "", false},
	{"/artigiano/42/26", ratelimit.Token, "Riparazione non disponibile", false},
	{"/artigiano/42/26/foto/a.jpg", -1, "", false},
	{"/w", ratelimit.Token, "Documento non disponibile", true},
	{"/w/tok", ratelimit.Token, "Documento non disponibile", true},
	{"/w/tok/current", ratelimit.Token, "Documento non disponibile", true},
	{"/w/tok/photos/1", -1, "", false},
	{"/w/tok/current/photos/1", -1, "", false},
	{"/g/9", ratelimit.Token, "Documento non disponibile", true},
	{"/g/9/foto/a.jpg", -1, "", false},
	{"/m/abc", -1, "", false},
	{"/privacy", -1, "", false},
	{"/healthz", -1, "", false},
}

// inventario: CLI-21
// I segmenti si leggono come il mux: ogni segmento decodificato da solo, quindi «%2F» resta dentro
// il token (/lotto/abc%2Fdef ha due segmenti e conta, come nello standalone).
func TestFamiglieConSegmentiCodificati(t *testing.T) {
	for p, ok := range map[string]bool{
		"/lotto/abc%2Fdef":    true,
		"/w/tok/photos%2F1":   true,
		"/r/tok/foto%2F7":     true,
		"/r/tok/foto/7":       false,
		"/artigiano/1/foto/2": false,
	} {
		r, _ := http.NewRequest("GET", "https://x"+p, nil)
		if _, got := famigliaDiRichiesta(r); got != ok {
			t.Errorf("%s conta = %v, atteso %v", p, got, ok)
		}
	}
}

// inventario: CLI-21
// Attraverso la faccia: trenta /lotto/<token con %2F> sconosciuti bloccano l'indirizzo sui lotti.
func TestSegmentiCodificatiAttraversoLaFaccia(t *testing.T) {
	h, _, _ := facciaPubblicaProva(t)
	b := nuovoBrowser(t, h)
	b.ip = "203.0.113.84"
	for i := range 30 {
		b.vai("GET", fmt.Sprintf("/lotto/abc%%2Fdef%d", i), nil, nil)
	}
	if r := b.vai("GET", "/lotto/qualunque", nil, nil); r.codice != 404 || r.corpo != "Lotto non disponibile" {
		t.Errorf("non bloccato: %d %q", r.codice, r.corpo)
	}
}

// inventario: CLI-21
// Le famiglie di percorsi che contano, con il testo del loro rifiuto: come PublicAttempts.
func TestFamiglieDeiPercorsi(t *testing.T) {
	for _, c := range famiglieAttese {
		f, ok := famigliaDi(c.percorso)
		if c.secchio == -1 {
			if ok {
				t.Errorf("%s conta (%+v), non dovrebbe", c.percorso, f)
			}
			continue
		}
		if !ok || f.secchio != c.secchio || f.testo != c.testo || f.robots != c.robots {
			t.Errorf("%s = %+v %v, atteso %v %q %v", c.percorso, f, ok, c.secchio, c.testo, c.robots)
		}
	}
}

// facciaPubblicaProva: la faccia pubblica vera più due rotte di prova che rifiutano con
// RifiutoPubblico, come faranno quelle vere (T4.5): «buono» vale, il resto no.
func facciaPubblicaProva(t *testing.T) (http.Handler, *clock.Finto, *int) {
	h, o, lavori, _ := facciaPubblicaProvaStato(t, 0)
	return h, o, lavori
}

// facciaPubblicaProvaStato aggiunge una rotta /stato/{busta}/{anno} lenta (ritardo) che conta le
// verifiche del codice, e una /r/esplode che va in panic.
func facciaPubblicaProvaStato(t *testing.T, ritardo time.Duration) (http.Handler, *clock.Finto, *int, *atomic.Int32) {
	t.Helper()
	d, o := dipendenzeConLimiti(&utentiFinti{attivo: true})
	lavori := 0
	var verifiche atomic.Int32
	h, err := costruisciPubblica(d, func(mux *http.ServeMux) {
		mux.HandleFunc("GET /stato/{busta}/{anno}", func(w http.ResponseWriter, r *http.Request) {
			verifiche.Add(1)
			time.Sleep(ritardo)
			if r.URL.Query().Get("pw") == "giusta" {
				_, _ = w.Write([]byte("stato"))
				return
			}
			RifiutoPubblico(w, r, "Riparazione non disponibile", false)
		})
		mux.HandleFunc("GET /r/{token}", func(w http.ResponseWriter, r *http.Request) {
			if r.PathValue("token") == "esplode" {
				panic("prova")
			}
			lavori++
			if r.PathValue("token") == "buono" {
				_, _ = w.Write([]byte("la tua riparazione"))
				return
			}
			RifiutoPubblico(w, r, "Riparazione non disponibile", false)
		})
		mux.HandleFunc("GET /w/{token}", func(w http.ResponseWriter, r *http.Request) {
			lavori++
			if r.PathValue("token") == "buono" {
				_, _ = w.Write([]byte("garanzia"))
				return
			}
			RifiutoPubblico(w, r, "Documento non disponibile", true)
		})
		mux.HandleFunc("GET /r/{token}/foto/{id}", func(w http.ResponseWriter, r *http.Request) {
			RifiutoPubblico(w, r, "Foto non disponibile", false)
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	return h, o, &lavori, &verifiche
}

// senzaVariabili: intestazioni e corpo di una risposta, senza quello che cambia a ogni richiesta.
func senzaVariabili(r risposta) string {
	h := r.h.Clone()
	h.Del("X-Request-Id")
	h.Del("Date")
	var sb strings.Builder
	_ = h.Write(&sb)
	return fmt.Sprintf("%d\n%s\n%s", r.codice, sb.String(), r.corpo)
}

// inventario: CLI-21
// Il 404 di una rotta pubblica è quello dello standalone (misurato): corpo di solo testo, nessun
// Content-Type, no-store, no-referrer e le intestazioni di sicurezza; X-Robots-Tag per /w e /g.
func TestRifiutoPubblico(t *testing.T) {
	h, _, _ := facciaPubblicaProva(t)
	b := nuovoBrowser(t, h)
	r := b.vai("GET", "/r/nessuno", nil, nil)
	if r.codice != 404 || r.corpo != "Riparazione non disponibile" || r.h.Get("Content-Type") != "" ||
		r.h.Get("Cache-Control") != "no-store" || r.h.Get("Referrer-Policy") != "no-referrer" ||
		r.h.Get("Content-Security-Policy") != "base-uri 'self'; frame-ancestors 'self';" ||
		r.h.Get("X-Content-Type-Options") != "nosniff" || r.h.Get("X-Permitted-Cross-Domain-Policies") != "none" ||
		r.h.Get("X-Request-Id") == "" || r.h.Get("X-Robots-Tag") != "" {
		t.Errorf("404 di /r:\n%s", senzaVariabili(r))
	}
	if r := b.vai("GET", "/w/nessuno", nil, nil); r.h.Get("X-Robots-Tag") != "noindex, nofollow" || r.corpo != "Documento non disponibile" {
		t.Errorf("404 di /w:\n%s", senzaVariabili(r))
	}
}

// inventario: CLI-21
// Trenta token sconosciuti in un minuto bloccano l'indirizzo sulle rotte a token lungo. Bloccato,
// anche un token buono riceve il 404 della rotta, **identico** a quello di un token che non c'è,
// intestazioni comprese (D-05), e la rotta non lavora. Un altro indirizzo no; le risposte buone
// non contano mai; dopo 15 minuti si riapre.
func TestBloccoDeiTokenIdenticoAl404(t *testing.T) {
	h, o, lavori := facciaPubblicaProva(t)
	b := nuovoBrowser(t, h)
	b.ip = "203.0.113.80"
	for range 100 {
		if r := b.vai("GET", "/r/buono", nil, nil); r.codice != 200 {
			t.Fatalf("le risposte buone hanno contato: %d", r.codice)
		}
	}
	normale := senzaVariabili(b.vai("GET", "/r/nessuno", nil, nil))
	normaleW := senzaVariabili(b.vai("GET", "/w/nessuno", nil, nil))
	for i := 3; i <= 29; i++ {
		b.vai("GET", fmt.Sprintf("/w/t%d", i), nil, nil)
	}
	if r := b.vai("GET", "/r/buono", nil, nil); r.codice != 200 {
		t.Fatalf("bloccato dopo 29 falliti: %d", r.codice)
	}
	b.vai("GET", "/r/t30", nil, nil)
	prima := *lavori
	for _, c := range []struct{ p, atteso string }{{"/r/buono", normale}, {"/r/nessuno", normale}, {"/w/buono", normaleW}} {
		if got := senzaVariabili(b.vai("GET", c.p, nil, nil)); got != c.atteso {
			t.Errorf("%s da bloccato:\n%s\natteso:\n%s", c.p, got, c.atteso)
		}
	}
	if *lavori != prima {
		t.Error("da bloccato la rotta ha lavorato")
	}
	altro := nuovoBrowser(t, h)
	altro.ip = "203.0.113.81"
	if r := altro.vai("GET", "/r/buono", nil, nil); r.codice != 200 {
		t.Errorf("altro indirizzo: %d", r.codice)
	}
	o.Sposta(15 * time.Minute)
	if r := b.vai("GET", "/r/buono", nil, nil); r.codice != 200 {
		t.Errorf("dopo 15 minuti: %d", r.codice)
	}
}

// inventario: CLI-21
// Le foto non contano e non sono bloccate; una rotta che non c'è dentro una famiglia (il 404 del
// router) conta come un rifiuto, come nello standalone. Il secchio di /stato è separato.
func TestFotoNonContanoERotteMancanti(t *testing.T) {
	h, _, _ := facciaPubblicaProva(t)
	b := nuovoBrowser(t, h)
	b.ip = "203.0.113.82"
	for i := range 40 {
		if r := b.vai("GET", fmt.Sprintf("/r/buono/foto/%d", i), nil, nil); r.corpo != "Foto non disponibile" {
			t.Fatalf("foto: %d %q", r.codice, r.corpo)
		}
	}
	if r := b.vai("GET", "/r/buono", nil, nil); r.codice != 200 {
		t.Fatalf("le foto hanno contato: %d", r.codice)
	}
	for i := range 30 {
		b.vai("GET", fmt.Sprintf("/r/a/b/c%d", i), nil, nil)
	}
	r := b.vai("GET", "/r/buono", nil, nil)
	if r.codice != 404 || r.corpo != "Riparazione non disponibile" {
		t.Errorf("i 404 del router non hanno contato: %d", r.codice)
	}
	if r := b.vai("GET", "/r/buono/foto/1", nil, nil); r.corpo != "Foto non disponibile" {
		t.Errorf("foto da bloccato: %q", r.corpo)
	}
	// /stato ha il suo secchio: dieci falliti lo bloccano, e il blocco non tocca /w.
	c := nuovoBrowser(t, h)
	c.ip = "203.0.113.83"
	for i := range 10 {
		c.vai("GET", fmt.Sprintf("/stato/%d/26", i), nil, nil)
	}
	if r := c.vai("GET", "/stato/1/26", nil, nil); r.corpo != "Riparazione non disponibile" || r.codice != 404 {
		t.Errorf("/stato bloccato: %d %q", r.codice, r.corpo)
	}
	if r := c.vai("GET", "/w/buono", nil, nil); r.codice != 200 {
		t.Errorf("/w da /stato bloccato: %d", r.codice)
	}
}

// inventario: CLI-21
// Manca la riga d'audit `security.attempts_blocked` all'inizio di ogni blocco (indirizzo e
// secchio, nient'altro): arriva con l'audit (ADM-16). Finché è saltato, la voce resta parziale.
func TestBloccoScriveAudit(t *testing.T) {
	t.Skip("audit non ancora nel Go (ADM-16): una riga security.attempts_blocked per blocco")
}

// inventario: CLI-21
// Manca la prova dal vivo dietro Tailscale Funnel (T1, PIANO §1.6): da dove arriva l'indirizzo
// vero del cliente. Finché è saltata, la voce resta parziale.
func TestIndirizzoDietroFunnel(t *testing.T) {
	t.Skip("prova dal vivo dietro Funnel su una macchina di prova (T1)")
}

// inventario: ACC-20
// Manca il reset della password con i suoi limiti (3 l'ora per indirizzo, 5 al giorno, ACC-13).
func TestLimitiDelReset(t *testing.T) {
	t.Skip("reset della password non ancora nel Go (ACC-13/14)")
}

// --- Slice 219 dello standalone: tentativi simultanei e percorsi in codice ---

// inventario: ACC-20
// Sessanta login sbagliati insieme sullo stesso account verificano cinque password, non sessanta
// (prenotazione prima della verifica). Chi trova i posti pieni riceve il 429 con un'attesa breve.
func TestLoginSimultanei(t *testing.T) {
	u := &utentiFinti{attivo: true, ritardo: 100 * time.Millisecond}
	d, _ := dipendenzeConLimiti(u)
	h := mustInterna(t, d)
	var wg sync.WaitGroup
	var mu sync.Mutex
	codici := map[int]int{}
	for range 60 {
		b := nuovoBrowser(t, h)
		b.ip = "203.0.113.90"
		b.vai("GET", "/login", nil, nil)
		wg.Go(func() {
			r := b.vai("POST", "/login", url.Values{"_csrf_token": {b.token}, "user[email]": {emailProva}, "user[password]": {"sbagliata"}}, nil)
			mu.Lock()
			codici[r.codice]++
			mu.Unlock()
		})
	}
	wg.Wait()
	if u.autentiche != 5 {
		t.Errorf("password verificate: %d, attese 5 (codici %v)", u.autentiche, codici)
	}
	if codici[422]+codici[429] != 60 || codici[429] < 55 {
		t.Errorf("codici %v", codici)
	}
}

// inventario: ACC-20
// Venti posti per indirizzo: sessanta login su account diversi verificano venti password.
func TestLoginSimultaneiSuAccountDiversi(t *testing.T) {
	u := &utentiFinti{attivo: true, ritardo: 100 * time.Millisecond}
	d, _ := dipendenzeConLimiti(u)
	h := mustInterna(t, d)
	var wg sync.WaitGroup
	for i := range 60 {
		b := nuovoBrowser(t, h)
		b.ip = "203.0.113.91"
		b.vai("GET", "/login", nil, nil)
		wg.Go(func() {
			b.vai("POST", "/login", url.Values{"_csrf_token": {b.token}, "user[email]": {fmt.Sprintf("c%d@esempio.it", i)}, "user[password]": {"x"}}, nil)
		})
	}
	wg.Wait()
	if u.autentiche != 20 {
		t.Errorf("password verificate: %d, attese 20", u.autentiche)
	}
}

// inventario: ACC-20
// Con i posti pieni di verifiche in corso l'attesa è breve: «riprova fra 1 minuti», non 15.
// L'altro posto prenotato si restituisce: chi è rifiutato non consuma il posto dell'indirizzo.
func TestLoginPostiPieni(t *testing.T) {
	u := &utentiFinti{attivo: true}
	d, _ := dipendenzeConLimiti(u)
	b := nuovoBrowser(t, mustInterna(t, d))
	b.ip = "203.0.113.92"
	acc := chiaveAccount(b.ip, emailProva)
	for range 5 {
		d.Limiti.Prenota(ratelimit.LoginAccount, acc)
	}
	for range 30 {
		if r := b.prova(emailProva, passwordProva); !troppi(r, "Troppi tentativi, riprova fra 1 minuti.") {
			t.Fatalf("posti pieni: %d", r.codice)
		}
	}
	if r := b.prova("altro@esempio.it", "sbagliata"); !rifiutato(r) {
		t.Errorf("i rifiuti hanno consumato i posti dell'indirizzo: %d", r.codice)
	}
	if u.autentiche != 1 {
		t.Errorf("password verificate: %d", u.autentiche)
	}
}

// inventario: CLI-21
// Sessanta codici /stato insieme: dieci verifiche, poi il blocco; una risposta buona restituisce il
// posto e non conta.
func TestStatoSimultanei(t *testing.T) {
	h, _, _, verifiche := facciaPubblicaProvaStato(t, 100*time.Millisecond)
	var wg sync.WaitGroup
	for i := range 60 {
		b := nuovoBrowser(t, h)
		b.ip = "203.0.113.93"
		wg.Go(func() { b.vai("GET", fmt.Sprintf("/stato/%d/26?pw=no", i), nil, nil) })
	}
	wg.Wait()
	if n := verifiche.Load(); n != 10 {
		t.Errorf("codici verificati: %d, attesi 10", n)
	}
	h2, _, _, verifiche2 := facciaPubblicaProvaStato(t, 0)
	b := nuovoBrowser(t, h2)
	b.ip = "203.0.113.94"
	for range 50 {
		if r := b.vai("GET", "/stato/1/26?pw=giusta", nil, nil); r.codice != 200 {
			t.Fatalf("le risposte buone tengono il posto: %d", r.codice)
		}
	}
	if verifiche2.Load() != 50 {
		t.Errorf("verifiche: %d", verifiche2.Load())
	}
}

// inventario: CLI-21
// Una rotta che va in errore (panic) restituisce il posto e non conta, come la pagina d'errore
// dello standalone (before_send gira anche lì).
func TestRottaInErroreRestituisceIlPosto(t *testing.T) {
	h, _, _ := facciaPubblicaProva(t)
	b := nuovoBrowser(t, h)
	b.ip = "203.0.113.95"
	b.c.Transport.(*http.Transport).DisableKeepAlives = true
	for range 40 {
		req, _ := http.NewRequest("GET", b.srv.URL+"/r/esplode", nil)
		req.Header.Set("X-Forwarded-For", b.ip)
		if res, err := b.c.Do(req); err == nil {
			_ = res.Body.Close()
		}
	}
	if r := b.vai("GET", "/r/buono", nil, nil); r.codice != 200 {
		t.Errorf("dopo 40 errori: %d", r.codice)
	}
}

// inventario: CLI-21
// /st%61to, /%72, /l%6Ftto, /%61rtigiano, /%77, /%67 contano come il percorso in chiaro (la rotta
// riconosciuta è quella instradata, slice 219).
func TestPercorsiInCodiceContano(t *testing.T) {
	for _, c := range []struct{ codificato, chiaro, testo string }{
		{"/st%%61to/%d/26", "/stato/1/26?pw=giusta", "Riparazione non disponibile"},
		{"/%%72/t%d", "/r/buono", "Riparazione non disponibile"},
		{"/l%%6Ftto/t%d", "/lotto/x", "Lotto non disponibile"},
		{"/%%61rtigiano/%d", "/artigiano/1", "Riparazione non disponibile"},
		{"/%%77/t%d", "/w/buono", "Documento non disponibile"},
		{"/%%67/%d", "/g/1", "Documento non disponibile"},
	} {
		h, _, _ := facciaPubblicaProva(t)
		b := nuovoBrowser(t, h)
		b.ip = "203.0.113.96"
		for i := range 30 {
			b.vai("GET", fmt.Sprintf(c.codificato, i), nil, nil)
		}
		if r := b.vai("GET", c.chiaro, nil, nil); r.codice != 404 || r.corpo != c.testo {
			t.Errorf("%s: dopo 30 in codice, %s = %d %q", c.codificato, c.chiaro, r.codice, r.corpo)
		}
	}
}
