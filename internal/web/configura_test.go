package web

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// Il primo titolare (ACC-16, «sostituita»): /configura/{codice} sulla faccia interna, con il codice
// stampato da «gestrip primo-titolare», su un database vero.

// primoFinto: nessun codice vale (la pagina si prova su un database vero).
type primoFinto struct{}

func (primoFinto) CodiceValido(context.Context, string) (bool, error) { return false, nil }
func (primoFinto) Crea(context.Context, string, string, string, string, string) (int64, string, error) {
	return 0, "", accounts.ErrCodiceNonValido
}

// facciaDaConfigurare è la faccia interna vera su un database con i conti dati (nessuno: vuoto).
func facciaDaConfigurare(t *testing.T, conti []contoProva) (http.Handler, *db.DB, *clock.Finto) {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC))
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	inserisciConti(t, d, conti)
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti, dd.Recupero = o, ratelimit.Nuovo(o), accounts.NuovoRecupero(d, o)
	dd.Inviti, dd.PrimoTitolare = accounts.NuoviInviti(d, o), accounts.NuovoPrimoTitolare(d, o)
	return mustInterna(t, dd), d, o
}

func codiceConsole(t *testing.T, d *db.DB, o clock.Orologio) string {
	t.Helper()
	c, _, err := accounts.NuovoPrimoTitolare(d, o).LinkDallaConsole(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// inviaConfigura apre il login per il token CSRF e manda il modulo della pagina.
func inviaConfigura(b *browser, codice, email, pw, conf string) risposta {
	b.t.Helper()
	b.vai("GET", "/login", nil, nil)
	return b.vai("POST", "/configura/"+codice, url.Values{"_csrf_token": {b.token}, "user[email]": {email},
		"user[password]": {pw}, "user[password_confirmation]": {conf}}, nil)
}

// rifiutoConfigura controlla la pagina unica dei rifiuti e la dà senza il token CSRF.
func rifiutoConfigura(t *testing.T, r risposta) string {
	t.Helper()
	if r.codice != 404 || !strings.Contains(r.corpo, `<h1 id="setup-refused">Link non valido</h1>`) ||
		!strings.Contains(r.corpo, "Questo link non è utilizzabile: è scaduto, è già stato usato, oppure il titolare esiste già.") ||
		strings.Contains(r.corpo, "@esempio.it") || strings.Contains(r.corpo, "<form") {
		t.Errorf("rifiuto: %d %s", r.codice, r.corpo)
	}
	return corpoSenzaCSRF(r.corpo)
}

const pwConfigura = "una password lunga del titolare"

// inventario: ACC-16
// /configura/{codice} sulla faccia interna (D-28), senza accesso: la pagina chiede email e due
// volte la password; aprirla non spende il codice. Email sbagliata, password diverse o corte: 422
// con il messaggio, l'email normalizzata resta nel modulo (la password mai), niente scritto e il
// codice resta. Riuscito: /login con «Titolare creato. Ora puoi accedere.», il titolare entra con
// la password scelta e apre le pagine del titolare; riga administrator.bootstrapped con attore
// installer e l'indirizzo di chi l'ha fatto.
func TestPaginaConfigura(t *testing.T) {
	h, d, o := facciaDaConfigurare(t, nil)
	codice := codiceConsole(t, d, o)
	b := nuovoBrowser(t, h)
	b.ip = "203.0.113.130"
	r := b.vai("GET", "/configura/"+codice, nil, nil)
	if r.codice != 200 {
		t.Fatalf("pagina: %d", r.codice)
	}
	for _, s := range []string{"<h1>Crea il titolare</h1>", "Il database è vuoto: crea l'account del titolare.", `id="setup-form"`,
		`action="/configura/` + codice + `"`, `name="user[email]"`, `type="email"`, `autocomplete="username"`, `name="user[password]"`,
		`name="user[password_confirmation]"`, "Password (almeno 12 caratteri)", "Ripeti la password", `autocomplete="new-password"`,
		"Crea il titolare</button>"} {
		if !strings.Contains(r.corpo, s) {
			t.Errorf("la pagina non contiene %q", s)
		}
	}
	if r.h.Get("Content-Security-Policy") == "" || r.h.Get("Cache-Control") != "no-store" {
		t.Errorf("intestazioni: %v", r.h)
	}
	if contaRighe(t, d, `SELECT count(*) FROM codici_primo_titolare WHERE usato_il IS NOT NULL`) != 0 {
		t.Error("aprire ha speso il codice")
	}
	for _, c := range []struct{ email, pw, conf, msg string }{
		{" Nuovo@Esempio.IT ", pwConfigura, "un'altra password lunga", "Le due password non coincidono."},
		{" Nuovo@Esempio.IT ", "corta", "corta", "La password deve avere almeno 12 caratteri."},
		{"non-un-indirizzo", pwConfigura, pwConfigura, "Email non valida."},
	} {
		r = inviaConfigura(b, codice, c.email, c.pw, c.conf)
		if r.codice != 422 || !strings.Contains(r.corpo, `<p id="setup-error">`+escapeHTML(c.msg)+`</p>`) || !strings.Contains(r.corpo, `id="setup-form"`) {
			t.Errorf("%q: %d %s", c.msg, r.codice, r.corpo)
		}
		if strings.Contains(r.corpo, c.pw) || strings.Contains(r.corpo, c.conf) {
			t.Errorf("%q: la password torna nel modulo", c.msg)
		}
		valore := `value="` + accounts.NormalizzaEmail(c.email) + `"`
		if !strings.Contains(r.corpo, valore) {
			t.Errorf("%q: il modulo non tiene l'email normalizzata", c.msg)
		}
	}
	if contaRighe(t, d, `SELECT count(*) FROM utenti`) != 0 || contaRighe(t, d, `SELECT count(*) FROM codici_primo_titolare WHERE usato_il IS NOT NULL`) != 0 ||
		contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'administrator.bootstrapped'`) != 0 {
		t.Error("un rifiuto ha scritto")
	}
	r = inviaConfigura(b, codice, " Titolare@Esempio.IT ", pwConfigura, pwConfigura)
	if !redirectA(r, "/login") {
		t.Fatalf("creazione: %d %q %s", r.codice, r.location, r.corpo)
	}
	if r := b.vai("GET", "/login", nil, nil); !strings.Contains(r.corpo, "Titolare creato. Ora puoi accedere.") {
		t.Error("manca «Titolare creato. Ora puoi accedere.»")
	}
	id := idDi(t, d, "titolare@esempio.it")
	var riga string
	if err := d.Lettura.QueryRow(`SELECT attore || '|' || coalesce(ip, '-') || '|' || azione || '|' || risorsa || '|' || id_risorsa || '|' || esito || '|' || dati
		FROM eventi_audit WHERE azione = 'administrator.bootstrapped'`).Scan(&riga); err != nil || riga != "installer|203.0.113.130|administrator.bootstrapped|user|"+id+"|ok|{}" {
		t.Errorf("registro: %q %v", riga, err)
	}
	if r := b.prova("titolare@esempio.it", pwConfigura); !redirectA(r, "/") {
		t.Fatalf("il titolare non entra: %d", r.codice)
	}
	if r := b.vai("GET", "/settings/utenti", nil, nil); r.codice != 200 {
		t.Errorf("il titolare creato non apre la pagina utenti: %d", r.codice)
	}
}

// inventario: ACC-16
// Ogni rifiuto è la stessa pagina «Link non valido», 404, senza modulo e senza email: codice
// inventato, troppo lungo, speso, scaduto, annullato da un codice nuovo, valido ma con un account
// nel database, modulo senza uno dei campi. Il POST di un codice morto, anche con un modulo
// sbagliato, è lo stesso rifiuto (non dice se il codice esiste).
func TestConfiguraRifiuti(t *testing.T) {
	h, d, o := facciaDaConfigurare(t, nil)
	inventato := rifiutoConfigura(t, nuovoBrowser(t, h).vai("GET", "/configura/codice-inventato", nil, nil))
	stesso := func(perche string, r risposta) {
		t.Helper()
		if got := rifiutoConfigura(t, r); got != inventato {
			t.Errorf("%s: non è lo stesso rifiuto", perche)
		}
	}
	stesso("troppo lungo", nuovoBrowser(t, h).vai("GET", "/configura/"+strings.Repeat("A", 65), nil, nil))
	for _, m := range [][3]string{
		{"t@esempio.it", pwConfigura, pwConfigura},
		{"t@esempio.it", pwConfigura, "diversa"},
		{"t@esempio.it", "corta", "corta"},
		{"non-un-indirizzo", pwConfigura, pwConfigura},
	} {
		stesso("POST inventato "+m[0]+" "+m[2], inviaConfigura(nuovoBrowser(t, h), "codice-inventato", m[0], m[1], m[2]))
	}
	vecchio := codiceConsole(t, d, o)
	codice := codiceConsole(t, d, o)
	stesso("annullato", nuovoBrowser(t, h).vai("GET", "/configura/"+vecchio, nil, nil))
	stesso("POST annullato", inviaConfigura(nuovoBrowser(t, h), vecchio, "t@esempio.it", pwConfigura, pwConfigura))
	for _, campo := range []string{"user[email]", "user[password]", "user[password_confirmation]"} {
		b := nuovoBrowser(t, h)
		b.vai("GET", "/login", nil, nil)
		f := url.Values{"_csrf_token": {b.token}, "user[email]": {"t@esempio.it"}, "user[password]": {pwConfigura}, "user[password_confirmation]": {pwConfigura}}
		f.Del(campo)
		stesso("senza "+campo, b.vai("POST", "/configura/"+codice, f, nil))
	}
	if contaRighe(t, d, `SELECT count(*) FROM utenti`) != 0 {
		t.Fatal("un rifiuto ha creato un account")
	}
	o.Sposta(time.Hour)
	stesso("scaduto", nuovoBrowser(t, h).vai("GET", "/configura/"+codice, nil, nil))
	stesso("POST scaduto", inviaConfigura(nuovoBrowser(t, h), codice, "t@esempio.it", pwConfigura, pwConfigura))
	codice = codiceConsole(t, d, o)
	if r := inviaConfigura(nuovoBrowser(t, h), codice, "t@esempio.it", pwConfigura, pwConfigura); !redirectA(r, "/login") {
		t.Fatalf("creazione: %d", r.codice)
	}
	stesso("speso", nuovoBrowser(t, h).vai("GET", "/configura/"+codice, nil, nil))
	stesso("POST speso", inviaConfigura(nuovoBrowser(t, h), codice, "altro@esempio.it", pwConfigura, pwConfigura))
	if contaRighe(t, d, `SELECT count(*) FROM utenti`) != 1 {
		t.Error("account dopo i rifiuti")
	}
}

// inventario: ACC-16
// Con un account già nel database (creato da un import, da un invito o in un altro modo), un
// codice rimasto aperto non apre la pagina e non crea un secondo titolare.
func TestConfiguraConAccountNelDatabase(t *testing.T) {
	h, d, o := facciaDaConfigurare(t, nil)
	codice := codiceConsole(t, d, o)
	inserisciConti(t, d, []contoProva{{"uscito@esempio.it", "dipendente", 0, true, "", ""}})
	rifiutoConfigura(t, nuovoBrowser(t, h).vai("GET", "/configura/"+codice, nil, nil))
	rifiutoConfigura(t, inviaConfigura(nuovoBrowser(t, h), codice, "t@esempio.it", pwConfigura, pwConfigura))
	if contaRighe(t, d, `SELECT count(*) FROM utenti`) != 1 || contaRighe(t, d, `SELECT count(*) FROM codici_primo_titolare WHERE usato_il IS NOT NULL`) != 0 {
		t.Error("creato un secondo account o speso il codice")
	}
}

// inventario: ACC-16
// Due moduli mandati insieme con lo stesso codice: un solo titolare, un solo redirect al login;
// l'altro è il rifiuto.
func TestConfiguraDueInviiInsieme(t *testing.T) {
	h, d, o := facciaDaConfigurare(t, nil)
	codice := codiceConsole(t, d, o)
	var bb []*browser
	for range 6 {
		b := nuovoBrowser(t, h)
		b.vai("GET", "/login", nil, nil)
		bb = append(bb, b)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	riuscite, rifiutate := 0, 0
	for n, b := range bb {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := b.vai("POST", "/configura/"+codice, url.Values{"_csrf_token": {b.token}, "user[email]": {"t" + itoa(int64(n)) + "@esempio.it"},
				"user[password]": {pwConfigura}, "user[password_confirmation]": {pwConfigura}}, nil)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case redirectA(r, "/login"):
				riuscite++
			case r.codice == 404:
				rifiutate++
			}
		}()
	}
	wg.Wait()
	if riuscite != 1 || rifiutate != len(bb)-1 || contaRighe(t, d, `SELECT count(*) FROM utenti`) != 1 {
		t.Errorf("riuscite %d, rifiutate %d", riuscite, rifiutate)
	}
}
