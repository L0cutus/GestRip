package web

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// La pagina utenti del titolare (ACC-06, ACC-10, ACC-11), su account veri in un database di prova.

const pwProva = "password-sintetica-di-prova"

type contoProva struct {
	email, ruolo string
	attivo       int
	password     bool
	nome         string
	ultimo       string
}

func facciaConAccountVeri(t *testing.T, conti []contoProva) (http.Handler, *db.DB) {
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
	h := accounts.Hash(pwProva)
	for _, c := range conti {
		var hash, nome, ultimo any
		if c.password {
			hash = h
		}
		if c.nome != "" {
			nome = c.nome
		}
		if c.ultimo != "" {
			ultimo = c.ultimo
		}
		if _, err := d.Scrittura.Exec(`INSERT INTO utenti (email, ruolo, attivo, hash_password, nome_visibile, ultimo_accesso_il, creato_il, aggiornato_il) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			c.email, c.ruolo, c.attivo, hash, nome, ultimo, "2026-10-30T09:00:00.000000Z", "2026-10-30T09:00:00.000000Z"); err != nil {
			t.Fatal(err)
		}
	}
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti, dd.Recupero = o, ratelimit.Nuovo(o), accounts.NuovoRecupero(d, o)
	return mustInterna(t, dd), d
}

var contiBase = []contoProva{
	{"titolare@esempio.it", "titolare", 1, true, "Anna", "2026-10-29T16:05:00.000000Z"},
	{"banco@esempio.it", "dipendente", 1, true, "", ""},
	{"invitato@esempio.it", "dipendente", 1, false, "", ""},
	{"uscito@esempio.it", "dipendente", 0, true, "", ""},
}

func titolareDentro(t *testing.T, h http.Handler) *browser {
	t.Helper()
	b := nuovoBrowser(t, h)
	if r := b.prova("titolare@esempio.it", pwProva); !redirectA(r, "/") {
		t.Fatalf("accesso del titolare: %d", r.codice)
	}
	return b
}

// inventario: ACC-06
// L'elenco degli account con email, nome, ruolo, ultimo accesso (quello di questo login, sull'ora
// del negozio) e stato, come UsersLive; i
// disattivati dietro «Mostra disattivati (N)»; nessun hash nella pagina.
func TestPaginaUtenti(t *testing.T) {
	h, _ := facciaConAccountVeri(t, contiBase)
	b := titolareDentro(t, h)
	r := b.vai("GET", "/settings/utenti", nil, nil)
	if r.codice != 200 {
		t.Fatalf("pagina: %d", r.codice)
	}
	for _, s := range []string{"<h1", ">Utenti<", "gestisce gli account", "Nessuno sceglie la password di un altro",
		"titolare@esempio.it", "banco@esempio.it", "invitato@esempio.it", ">Anna<", "Invito da consegnare", ">Attivo<",
		"30/10/2026 10:00", ">Titolare<", ">Staff<", "Disattiva", "Disattivare questo utente?", "Mostra disattivati (1)",
		"Email", "Nome", "Ruolo", "Ultimo accesso", "Stato", "Azioni"} {
		if !strings.Contains(r.corpo, s) {
			t.Errorf("la pagina non contiene %q", s)
		}
	}
	for _, s := range []string{"uscito@esempio.it", "pbkdf2", "Nascondi disattivati"} {
		if strings.Contains(r.corpo, s) {
			t.Errorf("la pagina contiene %q", s)
		}
	}
	r = b.vai("GET", "/settings/utenti?disattivati=1", nil, nil)
	for _, s := range []string{"uscito@esempio.it", "Nascondi disattivati (1)", ">Disattivato<"} {
		if !strings.Contains(r.corpo, s) {
			t.Errorf("con i disattivati la pagina non contiene %q", s)
		}
	}
	// Le righe dei disattivati non hanno né il menu del ruolo né «Disattiva».
	id := idDiPagina(r.corpo, "uscito@esempio.it")
	if strings.Contains(r.corpo, "user-"+id+"-role-form") || strings.Contains(r.corpo, "user-"+id+"-deactivate") ||
		!strings.Contains(r.corpo, `id="user-`+id+`-role-frozen">Staff<`) {
		t.Error("la riga del disattivato ha i comandi, o non ha il ruolo scritto")
	}
	// Ordine: attivi, dipendenti prima dei titolari, poi email.
	i, j, k := strings.Index(r.corpo, "banco@"), strings.Index(r.corpo, "invitato@"), strings.Index(r.corpo, "titolare@esempio.it")
	if i >= j || j >= k || k >= strings.Index(r.corpo, "uscito@") {
		t.Error("ordine dell'elenco sbagliato")
	}
}

// inventario: ACC-06
// Senza disattivati il pulsante non c'è.
func TestPaginaUtentiSenzaDisattivati(t *testing.T) {
	h, _ := facciaConAccountVeri(t, contiBase[:2])
	b := titolareDentro(t, h)
	if r := b.vai("GET", "/settings/utenti", nil, nil); strings.Contains(r.corpo, "disattivati") {
		t.Error("pulsante dei disattivati senza disattivati")
	}
}

func idDi(t *testing.T, d *db.DB, email string) string {
	t.Helper()
	var id string
	if err := d.Lettura.QueryRow(`SELECT id FROM utenti WHERE email = ?`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func ultimaRiga(t *testing.T, d *db.DB) string {
	t.Helper()
	var a, b, c, e, f string
	if err := d.Lettura.QueryRow(`SELECT attore, azione, id_risorsa, esito, dati FROM eventi_audit ORDER BY id DESC LIMIT 1`).Scan(&a, &b, &c, &e, &f); err != nil {
		return ""
	}
	return strings.Join([]string{a, b, c, e, f}, "|")
}

// inventario: ACC-10, ADM-17
// «Disattiva»: torna alla pagina con «Account disattivato. Le sessioni aperte cadono al primo
// gesto.», la riga d'audit ha il titolare come attore, e la sessione aperta di quell'account cade
// alla richiesta dopo. L'ultimo titolare si rifiuta con il messaggio dello standalone; un id che
// non va o un account già fuori: «Account non disattivato.».
func TestDisattivaDallaPagina(t *testing.T) {
	h, d := facciaConAccountVeri(t, contiBase)
	banco := nuovoBrowser(t, h)
	banco.prova("banco@esempio.it", pwProva)
	if r := banco.vai("GET", "/", nil, nil); r.codice != 200 {
		t.Fatalf("banco: %d", r.codice)
	}
	b := titolareDentro(t, h)
	b.vai("GET", "/settings/utenti", nil, nil)
	id := idDi(t, d, "banco@esempio.it")
	r := b.vai("POST", "/settings/utenti/"+id+"/disattiva", url.Values{"_csrf_token": {b.token}}, nil)
	if !redirectA(r, "/settings/utenti") {
		t.Fatalf("disattiva: %d %q", r.codice, r.location)
	}
	if r := b.vai("GET", "/settings/utenti", nil, nil); !strings.Contains(r.corpo, "Account disattivato. Le sessioni aperte cadono al primo gesto.") {
		t.Error("manca il messaggio")
	}
	if r := b.vai("GET", "/settings/utenti", nil, nil); strings.Contains(r.corpo, "Account disattivato.") {
		t.Error("il messaggio di esito resta anche alla pagina dopo")
	}
	if got := ultimaRiga(t, d); got != "titolare@esempio.it|user.deactivated|"+id+"|ok|{}" {
		t.Errorf("audit: %q", got)
	}
	if r := banco.vai("GET", "/", nil, nil); !redirectA(r, "/login") {
		t.Errorf("la sessione dell'account disattivato vale ancora: %d", r.codice)
	}
	for p, msg := range map[string]string{
		"/settings/utenti/" + idDi(t, d, "titolare@esempio.it") + "/disattiva": "È l&#39;ultimo titolare attivo: nomina prima un altro titolare.",
		"/settings/utenti/" + id + "/disattiva":                                "Account non disattivato.",
		"/settings/utenti/0/disattiva":                                         "Account non disattivato.",
		"/settings/utenti/abc/disattiva":                                       "Account non disattivato.",
		"/settings/utenti/999/disattiva":                                       "Account non disattivato.",
	} {
		b.vai("GET", "/settings/utenti", nil, nil)
		if r := b.vai("POST", p, url.Values{"_csrf_token": {b.token}}, nil); !redirectA(r, "/settings/utenti") {
			t.Errorf("%s: %d", p, r.codice)
		}
		if r := b.vai("GET", "/settings/utenti", nil, nil); !strings.Contains(r.corpo, msg) {
			t.Errorf("%s: manca %q", p, msg)
		}
	}
}

// inventario: ACC-11, ADM-17
// Il cambio di ruolo dal menu: «Ruolo aggiornato.», riga d'audit con il ruolo, il nuovo titolare
// trova le pagine del titolare senza rifare l'accesso; un ruolo che il menu non offre «Ruolo non
// valido.»; l'ultimo titolare non si declassa; un account che non c'è «Ruolo non aggiornato.».
func TestRuoloDallaPagina(t *testing.T) {
	h, d := facciaConAccountVeri(t, contiBase)
	banco := nuovoBrowser(t, h)
	banco.prova("banco@esempio.it", pwProva)
	if r := banco.vai("GET", "/settings/utenti", nil, nil); !redirectA(r, "/") {
		t.Fatalf("dipendente sulla pagina utenti: %d", r.codice)
	}
	b := titolareDentro(t, h)
	b.vai("GET", "/settings/utenti", nil, nil)
	id := idDi(t, d, "banco@esempio.it")
	if r := b.vai("POST", "/settings/utenti/"+id+"/ruolo", url.Values{"_csrf_token": {b.token}, "ruolo": {"titolare"}}, nil); !redirectA(r, "/settings/utenti") {
		t.Fatalf("ruolo: %d", r.codice)
	}
	if r := b.vai("GET", "/settings/utenti", nil, nil); !strings.Contains(r.corpo, "Ruolo aggiornato.") {
		t.Error("manca «Ruolo aggiornato.»")
	}
	if got := ultimaRiga(t, d); got != "titolare@esempio.it|user.role_changed|"+id+`|ok|{"role":"titolare"}` {
		t.Errorf("audit: %q", got)
	}
	if r := banco.vai("GET", "/settings/utenti", nil, nil); r.codice != 200 {
		t.Errorf("il promosso non vede la pagina del titolare: %d", r.codice)
	}
	tit := idDi(t, d, "titolare@esempio.it")
	// Ora i titolari sono due: il primo si può declassare, poi il secondo è l'ultimo.
	for _, c := range []struct{ id, ruolo, msg string }{
		{id, "owner", "Ruolo non valido."},
		{id, "-", "Ruolo non valido."},    // presente ma vuoto: invalid_role
		{id, "", "Ruolo non aggiornato."}, // mancante: nessun ruolo nel modulo
		{"999", "titolare", "Ruolo non aggiornato."},
		{"x", "titolare", "Ruolo non aggiornato."},
		{tit, "dipendente", "Ruolo aggiornato."},
	} {
		b2 := b
		if c.msg == "Ruolo aggiornato." {
			b2 = banco // il titolare declassa l'altro titolare dalla sua sessione
		}
		b2.vai("GET", "/settings/utenti", nil, nil)
		f := url.Values{"_csrf_token": {b2.token}}
		switch c.ruolo {
		case "":
		case "-":
			f.Set("ruolo", "")
		default:
			f.Set("ruolo", c.ruolo)
		}
		b2.vai("POST", "/settings/utenti/"+c.id+"/ruolo", f, nil)
		if r := b2.vai("GET", "/settings/utenti", nil, nil); !strings.Contains(r.corpo, c.msg) {
			t.Errorf("%s %q: manca %q", c.id, c.ruolo, c.msg)
		}
	}
	banco.vai("GET", "/settings/utenti", nil, nil)
	banco.vai("POST", "/settings/utenti/"+id+"/ruolo", url.Values{"_csrf_token": {banco.token}, "ruolo": {"dipendente"}}, nil)
	if r := banco.vai("GET", "/settings/utenti", nil, nil); !strings.Contains(r.corpo, "È l&#39;ultimo titolare attivo: nomina prima un altro titolare.") {
		t.Error("l'ultimo titolare si è declassato")
	}
	// Il titolare declassato non vede più la pagina.
	if r := b.vai("GET", "/settings/utenti", nil, nil); !redirectA(r, "/") {
		t.Errorf("il titolare declassato vede ancora la pagina: %d", r.codice)
	}
}

// idDiPagina trova l'id della riga di un'email nella pagina utenti.
func idDiPagina(corpo, email string) string {
	i := strings.Index(corpo, ">"+email+"<")
	j := strings.LastIndex(corpo[:i], `id="user-`)
	k := strings.Index(corpo[j+9:], "-")
	return corpo[j+9 : j+9+k]
}

// inventario: ACC-06, ACC-10, ACC-11
// Con i disattivati aperti, un'azione riuscita (o il rifiuto della disattivazione) li lascia
// aperti, come lo standalone, dove la pagina non si ricarica; i rifiuti del ruolo ricaricano la
// pagina (push_navigate) e li richiudono. Il campo del ruolo ripetuto vale per l'ultimo, come Plug.
func TestDisattivatiRestanoAperti(t *testing.T) {
	h, d := facciaConAccountVeri(t, contiBase)
	b := titolareDentro(t, h)
	r := b.vai("GET", "/settings/utenti?disattivati=1", nil, nil)
	if !strings.Contains(r.corpo, `name="disattivati" type="hidden" value="1"`) {
		t.Fatal("i moduli non portano lo stato dei disattivati")
	}
	id := idDi(t, d, "banco@esempio.it")
	f := url.Values{"_csrf_token": {b.token}, "disattivati": {"1"}, "ruolo": {"dipendente", "titolare"}}
	if r := b.vai("POST", "/settings/utenti/"+id+"/ruolo", f, nil); !redirectA(r, "/settings/utenti?disattivati=1") {
		t.Errorf("ruolo riuscito: %d %q", r.codice, r.location)
	}
	if got := ultimaRiga(t, d); !strings.Contains(got, `{"role":"titolare"}`) {
		t.Errorf("il ruolo ripetuto non vale per l'ultimo: %q", got)
	}
	b.vai("GET", "/settings/utenti?disattivati=1", nil, nil)
	f = url.Values{"_csrf_token": {b.token}, "disattivati": {"1"}, "ruolo": {"owner"}}
	if r := b.vai("POST", "/settings/utenti/"+id+"/ruolo", f, nil); !redirectA(r, "/settings/utenti") {
		t.Errorf("ruolo rifiutato: %d %q", r.codice, r.location)
	}
	for _, c := range []struct{ id, dove string }{
		{idDi(t, d, "uscito@esempio.it"), "/settings/utenti?disattivati=1"}, // rifiuto: resta aperto
		{id, "/settings/utenti?disattivati=1"},
	} {
		b.vai("GET", "/settings/utenti?disattivati=1", nil, nil)
		if r := b.vai("POST", "/settings/utenti/"+c.id+"/disattiva", url.Values{"_csrf_token": {b.token}, "disattivati": {"1"}}, nil); !redirectA(r, c.dove) {
			t.Errorf("disattiva %s: %d %q", c.id, r.codice, r.location)
		}
	}
	b.vai("GET", "/settings/utenti", nil, nil)
	if r := b.vai("POST", "/settings/utenti/"+idDi(t, d, "invitato@esempio.it")+"/disattiva", url.Values{"_csrf_token": {b.token}}, nil); !redirectA(r, "/settings/utenti") {
		t.Errorf("disattiva a disattivati chiusi: %d %q", r.codice, r.location)
	}
}
