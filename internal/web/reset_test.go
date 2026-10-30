package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La password dimenticata del titolare (ACC-13, ACC-14, ACC-20), sulla faccia interna (D-28), con
// i testi e gli stati dello standalone. Nei test il lavoro dopo la risposta gira subito.
func init() { inDisparte = func(f func()) { f() } }

func contaRighe(t *testing.T, d *db.DB, q string) int {
	t.Helper()
	var n int
	if err := d.Lettura.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// inventario: ACC-13, ACC-20
// /reset-request: la pagina non chiede niente; la richiesta risponde sempre «Richiesta inviata»,
// con le stesse parole, che il link sia partito o no, che il limite sia stato superato o no.
func TestPaginaRichiestaReset(t *testing.T) {
	h, d := facciaConAccountVeri(t, contiBase)
	b := nuovoBrowser(t, h)
	b.ip = "203.0.113.120"
	r := b.vai("GET", "/reset-request", nil, nil)
	if r.codice != 200 {
		t.Fatalf("pagina: %d", r.codice)
	}
	for _, s := range []string{"Hai dimenticato la password?", "Invia link reset", "Validità 30 minuti, uso singolo.",
		`action="/reset-request"`, "Torna al login", `href="/login"`} {
		if !strings.Contains(r.corpo, s) {
			t.Errorf("la pagina non contiene %q", s)
		}
	}
	var risposte []string
	for range 5 {
		b.vai("GET", "/reset-request", nil, nil)
		r := b.vai("POST", "/reset-request", url.Values{"_csrf_token": {b.token}}, nil)
		if r.codice != 200 || !strings.Contains(r.corpo, "Richiesta inviata") || !strings.Contains(r.corpo, "Il link scade dopo 30 minuti ed è utilizzabile una sola volta.") {
			t.Fatalf("risposta: %d", r.codice)
		}
		risposte = append(risposte, r.corpo[strings.Index(r.corpo, "<main"):])
	}
	for _, x := range risposte[1:] {
		if x != risposte[0] {
			t.Error("le risposte non sono tutte uguali")
		}
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM reset_password WHERE ip = '203.0.113.120'`); n != 3 {
		t.Errorf("righe di reset dall'indirizzo: %d, attese 3", n)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'password_reset.requested' AND esito = 'rate_limited'`); n != 1 {
		t.Errorf("righe del limite: %d", n)
	}
	if r := b.vai("POST", "/reset-request", url.Values{"x": {"1"}}, nil); r.codice != 403 {
		t.Errorf("senza token CSRF: %d", r.codice)
	}
	p := nuovoBrowser(t, pubblicaProva(&sondaFinta{pronto: true}, nessunRegistro))
	if r := p.vai("GET", "/reset-request", nil, nil); r.codice != 404 {
		t.Errorf("/reset-request sulla faccia pubblica: %d (D-28)", r.codice)
	}
}

func linkConsole(t *testing.T, d *db.DB) string {
	t.Helper()
	tok, _, _, err := accounts.NuovoRecupero(d, clock.NuovoFinto(time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC))).LinkDallaConsole(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// inventario: ACC-14
// /reset/{token}: la pagina dice di chi è la password; le due password diverse o corte sono un 422
// che tiene il link; riuscito, si torna al login con «Password aggiornata. Accedi con la nuova
// password.», le sessioni aperte di quell'account cadono, la password nuova vale, il link no più.
func TestPaginaNuovaPassword(t *testing.T) {
	h, d := facciaConAccountVeri(t, contiBase)
	aperta := titolareDentro(t, h)
	tok := linkConsole(t, d)
	b := nuovoBrowser(t, h)
	r := b.vai("GET", "/reset/"+tok, nil, nil)
	if r.codice != 200 {
		t.Fatalf("pagina: %d", r.codice)
	}
	for _, s := range []string{"Scegli una nuova password", "Nuova password per <b>titolare@esempio.it</b>.",
		"Nuova password (almeno 12 caratteri)", "Conferma password", "Salva nuova password", `action="/reset/` + tok + `"`,
		`name="password[password]"`, `name="password[password_confirmation]"`, `autocomplete="new-password"`} {
		if !strings.Contains(r.corpo, s) {
			t.Errorf("la pagina non contiene %q", s)
		}
	}
	nuova := "una-password-nuova-lunga"
	for _, c := range []struct{ pw, conferma, msg string }{
		{nuova, nuova + "x", "Le due password non coincidono."},
		{"corta", "corta", "La password deve avere almeno 12 caratteri."},
	} {
		r := b.vai("POST", "/reset/"+tok, url.Values{"_csrf_token": {b.token}, "password[password]": {c.pw}, "password[password_confirmation]": {c.conferma}}, nil)
		if r.codice != 422 || !strings.Contains(r.corpo, c.msg) || !strings.Contains(r.corpo, "Scegli una nuova password") {
			t.Errorf("%q: %d", c.msg, r.codice)
		}
	}
	r = b.vai("POST", "/reset/"+tok, url.Values{"_csrf_token": {b.token}, "password[password]": {nuova}, "password[password_confirmation]": {nuova}}, nil)
	if !redirectA(r, "/login") {
		t.Fatalf("reset: %d %q", r.codice, r.location)
	}
	if r := b.vai("GET", "/login", nil, nil); !strings.Contains(r.corpo, "Password aggiornata. Accedi con la nuova password.") {
		t.Error("manca il messaggio sul login")
	}
	if r := aperta.vai("GET", "/", nil, nil); !redirectA(r, "/login") {
		t.Errorf("la sessione aperta prima vale ancora: %d", r.codice)
	}
	if r := b.prova("titolare@esempio.it", nuova); !redirectA(r, "/") {
		t.Errorf("la password nuova non entra: %d", r.codice)
	}
	if r := b.vai("GET", "/reset/"+tok, nil, nil); r.codice != 400 || !strings.Contains(r.corpo, "Link non valido") {
		t.Errorf("link già usato: %d", r.codice)
	}
}

// inventario: ACC-14
// Ogni link che non va ha la stessa pagina e lo stesso 400, anche in POST e con il modulo
// malformato; la pagina invita a chiederne uno nuovo.
func TestLinkResetNonValido(t *testing.T) {
	h, d := facciaConAccountVeri(t, contiBase)
	b := nuovoBrowser(t, h)
	for _, p := range []string{"/reset/inventato", "/reset/" + strings.Repeat("a", 65)} {
		r := b.vai("GET", p, nil, nil)
		if r.codice != 400 {
			t.Errorf("GET %s: %d", p, r.codice)
		}
		for _, s := range []string{"Link non valido", "Link non valido o scaduto.", "Il link può essere scaduto o già usato. Richiedine uno nuovo.",
			"Richiedi nuovo link", `href="/reset-request"`} {
			if !strings.Contains(r.corpo, s) {
				t.Errorf("%s: manca %q", p, s)
			}
		}
	}
	b.vai("GET", "/reset-request", nil, nil)
	tok := linkConsole(t, d)
	for _, f := range []url.Values{
		{"password[password]": {"x"}},
		{"password": {"una-password-nuova-lunga"}},
		{},
	} {
		f.Set("_csrf_token", b.token)
		if r := b.vai("POST", "/reset/"+tok, f, nil); r.codice != 400 || !strings.Contains(r.corpo, "Link non valido") {
			t.Errorf("modulo %v: %d", f, r.codice)
		}
	}
	if r := b.vai("POST", "/reset/inventato", url.Values{"_csrf_token": {b.token}, "password[password]": {"a"}, "password[password_confirmation]": {"b"}}, nil); r.codice != 400 {
		t.Errorf("password diverse su un link morto: %d", r.codice)
	}
	if r := b.vai("GET", "/reset/"+tok, nil, nil); r.codice != 200 {
		t.Errorf("i rifiuti hanno speso il link: %d", r.codice)
	}
}

// recuperoAttento rifiuta di lavorare con un contesto annullato, come il database.
type recuperoAttento struct {
	recuperoFinto
	chiamate []string
}

func (r *recuperoAttento) RichiestaDallaPagina(ctx context.Context, ip string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	r.chiamate = append(r.chiamate, ip)
	return "no_channel", nil
}

// inventario: ACC-13, ACC-20
// La richiesta lavora dopo la risposta e sopravvive alla fine della richiesta: se il lavoro usasse
// il contesto della richiesta, già chiuso, la riga non si scriverebbe e i limiti non scatterebbero
// mai (review della 011).
func TestRichiestaDopoLaRisposta(t *testing.T) {
	var lavoro func()
	prima := inDisparte
	inDisparte = func(f func()) { lavoro = f }
	defer func() { inDisparte = prima }()
	d := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, &utentiFinti{attivo: true})
	rec := &recuperoAttento{}
	d.Recupero = rec
	b := nuovoBrowser(t, mustInterna(t, d))
	b.ip = "203.0.113.121"
	b.vai("GET", "/reset-request", nil, nil)
	if r := b.vai("POST", "/reset-request", url.Values{"_csrf_token": {b.token}}, nil); r.codice != 200 {
		t.Fatalf("risposta: %d", r.codice)
	}
	if lavoro == nil || len(rec.chiamate) != 0 {
		t.Fatal("il lavoro non è stato messo da parte per dopo la risposta")
	}
	lavoro() // la richiesta è finita: il suo contesto è chiuso
	if len(rec.chiamate) != 1 || rec.chiamate[0] != "203.0.113.121" {
		t.Errorf("il lavoro dopo la risposta non è arrivato: %v", rec.chiamate)
	}
}

var _ = http.StatusOK
