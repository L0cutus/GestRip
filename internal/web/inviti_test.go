package web

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// Gli inviti (ACC-07, ACC-08, ACC-09) sulla pagina utenti e su /invito/{token}, su account veri.

// invitiFinti: nessun invito vale (gli inviti si provano su account veri).
type invitiFinti struct{}

func (invitiFinti) Invita(context.Context, string, string, string, audit.Contesto) (accounts.Invito, error) {
	return accounts.Invito{}, accounts.ErrEmailNonValida
}
func (invitiFinti) NuovoLink(context.Context, int64, audit.Contesto) (accounts.Invito, error) {
	return accounts.Invito{}, accounts.ErrNonTrovato
}
func (invitiFinti) Destinatario(context.Context, string) (string, bool, error) { return "", false, nil }
func (invitiFinti) Accetta(context.Context, string, string) error              { return accounts.ErrInvitoNonValido }

const indirizzoProva = "https://negozio.esempio.ts.net:8443"

var reLinkInvito = regexp.MustCompile(`https://negozio\.esempio\.ts\.net:8443/invito/([A-Za-z0-9_-]{43})`)

// facciaConInviti è facciaConAccountVeri con gli inviti veri, l'indirizzo interno dato (vuoto:
// non configurato) e l'orologio finto.
func facciaConInviti(t *testing.T, conti []contoProva, indirizzo string) (http.Handler, *db.DB, *clock.Finto) {
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
	dd.Inviti, dd.IndirizzoInterno = accounts.NuoviInviti(d, o), indirizzo
	return mustInterna(t, dd), d, o
}

func formInvito(b *browser, email, nome, ruolo string) url.Values {
	return url.Values{"_csrf_token": {b.token}, "user[email]": {email}, "user[display_name]": {nome}, "user[role]": {ruolo}}
}

// inventario: ACC-07, ADM-17
// «Aggiungi e genera il link»: l'account nasce senza password; la risposta mostra una volta il
// link /invito/<token> sull'indirizzo interno dell'apparecchio (mai sull'Host della richiesta), con
// «Account … aggiunto. Consegna il link una volta sola.»; il link vale davvero; riga user.invited
// con il titolare come attore. Alla pagina dopo il link non c'è più e l'account è «Invito da
// consegnare» con «Nuovo link».
func TestInvitaDallaPagina(t *testing.T) {
	h, d, o := facciaConInviti(t, contiBase, indirizzoProva)
	b := titolareDentro(t, h)
	r := b.vai("GET", "/settings/utenti", nil, nil)
	for _, s := range []string{">Aggiungi un account<", `id="user-invite-form"`, `action="/settings/utenti/invita"`, `name="user[email]"`, `type="email"`,
		`name="user[display_name]"`, "Nome (opzionale)", `name="user[role]"`, `<option value="dipendente" selected>Staff</option>`,
		`<option value="titolare">Titolare</option>`, "Aggiungi e genera il link"} {
		if !strings.Contains(r.corpo, s) {
			t.Errorf("la pagina non contiene %q", s)
		}
	}
	if strings.Contains(r.corpo, `id="invitation-link"`) {
		t.Error("riquadro del link senza invito")
	}
	r = b.vai("POST", "/settings/utenti/invita", formInvito(b, "Nuovo@Esempio.it", "Nuovo", "dipendente"), nil)
	if r.codice != 200 {
		t.Fatalf("invito: %d %q", r.codice, r.location)
	}
	m := reLinkInvito.FindStringSubmatch(r.corpo)
	if m == nil {
		t.Fatalf("link assente: %s", r.corpo)
	}
	if r.h.Get("Cache-Control") != "no-store" {
		t.Errorf("la risposta con il link si tiene in cache: %q", r.h.Get("Cache-Control"))
	}
	for _, s := range []string{"Account nuovo@esempio.it aggiunto. Consegna il link una volta sola.", `id="invitation-link"`,
		"Link di accesso per <b>nuovo@esempio.it</b>. Consegnalo adesso: non sarà mostrato di nuovo.", `id="invitation-link-dismiss"`,
		"Ho consegnato il link", ">Invito da consegnare<"} {
		if !strings.Contains(r.corpo, s) {
			t.Errorf("la risposta non contiene %q", s)
		}
	}
	if strings.Contains(r.corpo, "127.0.0.1") {
		t.Error("il link usa l'Host della richiesta")
	}
	// Il modulo torna vuoto.
	if strings.Contains(r.corpo, `value="Nuovo"`) || strings.Contains(r.corpo, `value="nuovo@esempio.it"`) || strings.Contains(r.corpo, `value="Nuovo@Esempio.it"`) {
		t.Error("il modulo tiene i valori dopo un invito riuscito")
	}
	id := idDi(t, d, "nuovo@esempio.it")
	if got := ultimaRiga(t, d); got != "titolare@esempio.it|user.invited|"+id+`|ok|{"role":"dipendente"}` {
		t.Errorf("audit: %q", got)
	}
	r = b.vai("GET", "/settings/utenti", nil, nil)
	if strings.Contains(r.corpo, `id="invitation-link"`) || strings.Contains(r.corpo, m[1]) || strings.Contains(r.corpo, "Consegna il link una volta sola") {
		t.Error("il link si vede una seconda volta")
	}
	if !strings.Contains(r.corpo, `id="user-`+id+`-reissue"`) {
		t.Error("manca «Nuovo link» per l'account in attesa")
	}
	if err := accounts.NuoviInviti(d, o).Accetta(context.Background(), m[1], "una password lunga"); err != nil {
		t.Errorf("il link mostrato non vale: %v", err)
	}
	// Il titolare invitato come titolare.
	b.vai("GET", "/settings/utenti", nil, nil)
	if r := b.vai("POST", "/settings/utenti/invita", formInvito(b, "socio@esempio.it", "", "titolare"), nil); r.codice != 200 || !reLinkInvito.MatchString(r.corpo) {
		t.Fatalf("invito del titolare: %d", r.codice)
	}
	if got := ultimaRiga(t, d); !strings.HasSuffix(got, `|ok|{"role":"titolare"}`) {
		t.Errorf("audit: %q", got)
	}
}

// inventario: ACC-07
// I rifiuti del modulo, con i messaggi di invite_error/1 e niente scritto: email sbagliata
// («Email non valida.», anche per un nome troppo lungo), già presa («Questa email è già registrata
// su un account.»), ruolo che il menu non offre («Ruolo non valido.»), modulo senza i campi
// («Account non aggiunto.»). Il modulo tiene i valori scritti, come il changeset. Un campo
// ripetuto vale per l'ultimo; il ruolo che manca è dipendente (il default dello schema).
func TestInvitaRifiutiDallaPagina(t *testing.T) {
	h, d, _ := facciaConInviti(t, contiBase, indirizzoProva)
	b := titolareDentro(t, h)
	for _, c := range []struct {
		form url.Values
		msg  string
	}{
		{formInvito(b, "non-un-indirizzo", "", "dipendente"), "Email non valida."},
		{formInvito(b, "ok@esempio.it", strings.Repeat("n", 121), "dipendente"), "Email non valida."},
		{formInvito(b, "banco@esempio.it", "", "dipendente"), "Questa email è già registrata su un account."},
		{formInvito(b, "ok@esempio.it", "", "admin"), "Ruolo non valido."},
		{formInvito(b, "ok@esempio.it", "", ""), "Ruolo non valido."},
		{url.Values{"_csrf_token": {b.token}}, "Account non aggiunto."},
		{url.Values{"_csrf_token": {b.token}, "email": {"ok@esempio.it"}}, "Account non aggiunto."},
		{url.Values{"_csrf_token": {b.token}, "user[email]": {""}}, "Email non valida."},
		{url.Values{"_csrf_token": {b.token}, "user[]": {"x"}}, "Account non aggiunto."},
		{url.Values{"_csrf_token": {b.token}, "user[]": {"x"}, "user[email]": {"ok@esempio.it"}}, "Account non aggiunto."},
		{url.Values{"_csrf_token": {b.token}, "user": {"una stringa"}, "user[email]": {"ok@esempio.it"}}, "Account non aggiunto."},
	} {
		b.vai("GET", "/settings/utenti", nil, nil)
		c.form.Set("_csrf_token", b.token)
		r := b.vai("POST", "/settings/utenti/invita", c.form, nil)
		if r.codice != 200 || !strings.Contains(r.corpo, `role="alert" class="flash flash-errore">`+escapeHTML(c.msg)) {
			t.Errorf("%v: %d, manca %q", c.form, r.codice, c.msg)
		}
		if strings.Contains(r.corpo, `id="invitation-link"`) {
			t.Errorf("%v: riquadro del link in un rifiuto", c.form)
		}
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM utenti`); n != len(contiBase) {
		t.Errorf("account scritti dai rifiuti: %d", n)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM inviti_utenti`); n != 0 {
		t.Errorf("inviti scritti dai rifiuti: %d", n)
	}
	// I valori scritti restano nel modulo.
	b.vai("GET", "/settings/utenti", nil, nil)
	r := b.vai("POST", "/settings/utenti/invita", formInvito(b, "non-un-indirizzo", "Gino", "titolare"), nil)
	if !strings.Contains(r.corpo, `value="non-un-indirizzo"`) || !strings.Contains(r.corpo, `value="Gino"`) ||
		!strings.Contains(r.corpo, `<option value="titolare" selected>Titolare</option>`) {
		t.Error("il modulo non tiene i valori scritti")
	}
	// Come il changeset: l'email torna normalizzata e il nome senza spazi ai lati.
	b.vai("GET", "/settings/utenti", nil, nil)
	r = b.vai("POST", "/settings/utenti/invita", formInvito(b, "  Banco@Esempio.IT ", "  Gino  ", "dipendente"), nil)
	if !strings.Contains(r.corpo, "Questa email è già registrata su un account.") || !strings.Contains(r.corpo, `value="banco@esempio.it"`) ||
		!strings.Contains(r.corpo, `value="Gino"`) {
		t.Error("il modulo non ripropone i valori normalizzati")
	}
	// Ripetuto: vale l'ultimo. Ruolo mancante: dipendente.
	b.vai("GET", "/settings/utenti", nil, nil)
	f := url.Values{"_csrf_token": {b.token}, "user[email]": {"primo@esempio.it", "ultimo@esempio.it"}}
	if r := b.vai("POST", "/settings/utenti/invita", f, nil); !strings.Contains(r.corpo, "Account ultimo@esempio.it aggiunto.") {
		t.Errorf("campo ripetuto: %d", r.codice)
	}
	var ruolo string
	_ = d.Lettura.QueryRow(`SELECT ruolo FROM utenti WHERE email = 'ultimo@esempio.it'`).Scan(&ruolo)
	if ruolo != accounts.Dipendente {
		t.Errorf("ruolo mancante: %q", ruolo)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func escapeHTML(s string) string { return strings.ReplaceAll(s, "'", "&#39;") }

// inventario: ACC-08, ADM-17
// «Nuovo link» per un account in attesa: «Nuovo link pronto. Il precedente non vale più.», il link
// nuovo mostrato una volta, il vecchio morto, riga user.invitation_reissued. Il pulsante non c'è per
// chi ha già una password né per un disattivato; il POST forzato per loro, o con un id che non va,
// dice «Nessun nuovo link: questo account ha già una password.» senza scrivere niente.
func TestNuovoLinkDallaPagina(t *testing.T) {
	h, d, o := facciaConInviti(t, contiBase, indirizzoProva)
	b := titolareDentro(t, h)
	b.vai("GET", "/settings/utenti", nil, nil)
	r := b.vai("POST", "/settings/utenti/invita", formInvito(b, "atteso@esempio.it", "", "dipendente"), nil)
	primo := reLinkInvito.FindStringSubmatch(r.corpo)
	if primo == nil {
		t.Fatal("primo link assente")
	}
	id := idDi(t, d, "atteso@esempio.it")
	// Un account invitato e poi disattivato non ha «Nuovo link», anche se non ha mai avuto password.
	fuori, err := accounts.NuoviInviti(d, o).Invita(context.Background(), "fuori@esempio.it", "", "dipendente", audit.Contesto{Attore: "titolare@esempio.it"})
	if err != nil {
		t.Fatal(err)
	}
	if err := accounts.NuoviUtenti(d, o).Disattiva(context.Background(), fuori.UtenteID, audit.Contesto{Attore: "titolare@esempio.it"}); err != nil {
		t.Fatal(err)
	}
	r = b.vai("GET", "/settings/utenti?disattivati=1", nil, nil)
	if !strings.Contains(r.corpo, "fuori@esempio.it") || strings.Contains(r.corpo, `id="user-`+itoa(fuori.UtenteID)+`-reissue"`) {
		t.Error("«Nuovo link» per un invitato disattivato")
	}
	for _, e := range []string{"titolare@esempio.it", "banco@esempio.it", "uscito@esempio.it"} {
		if strings.Contains(r.corpo, `id="user-`+idDi(t, d, e)+`-reissue"`) {
			t.Errorf("«Nuovo link» per %s", e)
		}
	}
	for _, e := range []string{"atteso@esempio.it", "invitato@esempio.it"} {
		if !strings.Contains(r.corpo, `action="/settings/utenti/`+idDi(t, d, e)+`/nuovo-link"`) || !strings.Contains(r.corpo, `id="user-`+idDi(t, d, e)+`-reissue"`) {
			t.Errorf("manca «Nuovo link» per %s", e)
		}
	}
	r = b.vai("POST", "/settings/utenti/"+id+"/nuovo-link", url.Values{"_csrf_token": {b.token}}, nil)
	secondo := reLinkInvito.FindStringSubmatch(r.corpo)
	if r.h.Get("Cache-Control") != "no-store" {
		t.Errorf("la risposta con il nuovo link si tiene in cache: %q", r.h.Get("Cache-Control"))
	}
	if r.codice != 200 || secondo == nil || secondo[1] == primo[1] ||
		!strings.Contains(r.corpo, "Nuovo link pronto. Il precedente non vale più.") || !strings.Contains(r.corpo, "Link di accesso per <b>atteso@esempio.it</b>.") {
		t.Fatalf("nuovo link: %d", r.codice)
	}
	if got := ultimaRiga(t, d); got != "titolare@esempio.it|user.invitation_reissued|"+id+"|ok|{}" {
		t.Errorf("audit: %q", got)
	}
	inv := accounts.NuoviInviti(d, o)
	if err := inv.Accetta(context.Background(), primo[1], "una password lunga"); err == nil {
		t.Error("il primo link vale ancora")
	}
	// L'account importato senza password e senza inviti.
	b.vai("GET", "/settings/utenti", nil, nil)
	if r := b.vai("POST", "/settings/utenti/"+idDi(t, d, "invitato@esempio.it")+"/nuovo-link", url.Values{"_csrf_token": {b.token}}, nil); !reLinkInvito.MatchString(r.corpo) {
		t.Errorf("nessun link per l'account senza inviti: %d", r.codice)
	}
	prima := contaRighe(t, d, `SELECT count(*) FROM inviti_utenti`) + contaRighe(t, d, `SELECT count(*) FROM eventi_audit`)
	for _, p := range []string{idDi(t, d, "titolare@esempio.it"), idDi(t, d, "uscito@esempio.it"), "0", "abc", "999"} {
		b.vai("GET", "/settings/utenti", nil, nil)
		r := b.vai("POST", "/settings/utenti/"+p+"/nuovo-link", url.Values{"_csrf_token": {b.token}}, nil)
		if r.codice != 200 || !strings.Contains(r.corpo, "Nessun nuovo link: questo account ha già una password.") || strings.Contains(r.corpo, `id="invitation-link"`) {
			t.Errorf("%s: %d", p, r.codice)
		}
	}
	if contaRighe(t, d, `SELECT count(*) FROM inviti_utenti`)+contaRighe(t, d, `SELECT count(*) FROM eventi_audit`) != prima {
		t.Error("un rifiuto ha scritto")
	}
	if err := inv.Accetta(context.Background(), secondo[1], "una password lunga"); err != nil {
		t.Errorf("il secondo link non vale: %v", err)
	}
}

// inventario: ACC-07, ACC-08
// Con i disattivati aperti, l'invito e il nuovo link li lasciano aperti (show_deactivated resta).
func TestInvitiConDisattivatiAperti(t *testing.T) {
	h, d, _ := facciaConInviti(t, contiBase, indirizzoProva)
	b := titolareDentro(t, h)
	r := b.vai("GET", "/settings/utenti?disattivati=1", nil, nil)
	f := formInvito(b, "nuovo@esempio.it", "", "dipendente")
	f.Set("disattivati", "1")
	if r = b.vai("POST", "/settings/utenti/invita", f, nil); !strings.Contains(r.corpo, "uscito@esempio.it") || !strings.Contains(r.corpo, "Nascondi disattivati") {
		t.Error("l'invito ha chiuso i disattivati")
	}
	g := url.Values{"_csrf_token": {b.token}, "disattivati": {"1"}}
	if r = b.vai("POST", "/settings/utenti/"+idDi(t, d, "invitato@esempio.it")+"/nuovo-link", g, nil); !strings.Contains(r.corpo, "uscito@esempio.it") {
		t.Error("il nuovo link ha chiuso i disattivati")
	}
	if r = b.vai("POST", "/settings/utenti/invita", formInvito(b, "altro@esempio.it", "", "dipendente"), nil); strings.Contains(r.corpo, "uscito@esempio.it") {
		t.Error("i disattivati si aprono senza chiederlo")
	}
}

// inventario: ACC-07, ACC-08
// Senza l'indirizzo interno nel file dell'apparecchio (D-32) l'invito e il nuovo link non si
// creano: il titolare legge che cosa manca e non resta nessun account né link senza indirizzo.
func TestInvitiSenzaIndirizzoInterno(t *testing.T) {
	h, d, _ := facciaConInviti(t, contiBase, "")
	b := titolareDentro(t, h)
	b.vai("GET", "/settings/utenti", nil, nil)
	r := b.vai("POST", "/settings/utenti/invita", formInvito(b, "nuovo@esempio.it", "", "dipendente"), nil)
	if !strings.Contains(r.corpo, escapeHTML(msgSenzaIndirizzoAdd)) || strings.Contains(r.corpo, "/invito/") {
		t.Errorf("invito senza indirizzo: %d", r.codice)
	}
	b.vai("GET", "/settings/utenti", nil, nil)
	r = b.vai("POST", "/settings/utenti/"+idDi(t, d, "invitato@esempio.it")+"/nuovo-link", url.Values{"_csrf_token": {b.token}}, nil)
	if !strings.Contains(r.corpo, escapeHTML(msgSenzaIndirizzoLink)) || strings.Contains(r.corpo, "/invito/") {
		t.Errorf("nuovo link senza indirizzo: %d", r.codice)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM inviti_utenti`) + contaRighe(t, d, `SELECT count(*) FROM eventi_audit`); n != 0 ||
		contaRighe(t, d, `SELECT count(*) FROM utenti`) != len(contiBase) {
		t.Error("scritto qualcosa senza indirizzo")
	}
}

// inventario: ACC-05, ACC-07, ACC-08
// Il dipendente che forza l'invito o il nuovo link con un token valido va a / con «Operazione
// riservata al titolare.» e non nasce niente: nessun account, nessun link, nessuna riga.
func TestDipendenteForzaGliInviti(t *testing.T) {
	h, d, _ := facciaConInviti(t, contiBase, indirizzoProva)
	banco := nuovoBrowser(t, h)
	if r := banco.prova("banco@esempio.it", pwProva); !redirectA(r, "/") {
		t.Fatalf("accesso del dipendente: %d", r.codice)
	}
	banco.vai("GET", "/", nil, nil)
	if r := banco.vai("POST", "/settings/utenti/invita", formInvito(banco, "forgiato@esempio.it", "", "titolare"), nil); !redirectA(r, "/") || strings.Contains(r.corpo, "/invito/") {
		t.Errorf("invito forzato: %d %q", r.codice, r.location)
	}
	if r := banco.vai("GET", "/", nil, nil); !strings.Contains(r.corpo, "Operazione riservata al titolare.") {
		t.Error("manca il messaggio del rifiuto")
	}
	if r := banco.vai("POST", "/settings/utenti/"+idDi(t, d, "invitato@esempio.it")+"/nuovo-link", url.Values{"_csrf_token": {banco.token}}, nil); !redirectA(r, "/") ||
		strings.Contains(r.corpo, "/invito/") {
		t.Errorf("nuovo link forzato: %d %q", r.codice, r.location)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM inviti_utenti`) + contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'user.invit%'`); n != 0 ||
		contaRighe(t, d, `SELECT count(*) FROM utenti`) != len(contiBase) {
		t.Error("il dipendente ha creato un invito")
	}
}

// corpoSenzaCSRF toglie dalla pagina l'unica parte che può cambiare tra due rifiuti: il token
// CSRF della sessione.
var reMetaCSRF = regexp.MustCompile(`<meta name="csrf-token" content="[^"]*">`)

func corpoSenzaCSRF(s string) string { return reMetaCSRF.ReplaceAllString(s, "") }

// inventario: ACC-09
// /invito/{token}, sulla faccia interna (D-28) e senza accesso: la pagina dice di chi è l'accesso e
// chiede la password due volte; aprirla non spende il link. Ogni rifiuto (speso, scaduto,
// inventato, di un account disattivato, modulo senza i campi) è la stessa pagina «Invito non
// valido», 404, senza l'email. Password diverse o corte: 422 con il messaggio, il link resta.
// Riuscito: /login con «Password impostata. Ora puoi accedere.», e si entra con la password scelta.
func TestPaginaInvito(t *testing.T) {
	h, d, o := facciaConInviti(t, contiBase, indirizzoProva)
	inv := accounts.NuoviInviti(d, o)
	chi := audit.Contesto{Attore: "titolare@esempio.it"}
	ospite, err := inv.Invita(context.Background(), "ospite@esempio.it", "", "dipendente", chi)
	if err != nil {
		t.Fatal(err)
	}
	b := nuovoBrowser(t, h)
	r := b.vai("GET", "/invito/"+ospite.Token, nil, nil)
	if r.codice != 200 {
		t.Fatalf("pagina: %d", r.codice)
	}
	for _, s := range []string{"Scegli la tua password", "Stai attivando l'accesso per <b>ospite@esempio.it</b>.", "nessun altro la conosce",
		`action="/invito/` + ospite.Token + `"`, `name="password[password]"`, `name="password[password_confirmation]"`,
		"Password (almeno 12 caratteri)", "Ripeti la password", `autocomplete="new-password"`, "Attiva l'accesso"} {
		if !strings.Contains(r.corpo, s) {
			t.Errorf("la pagina non contiene %q", s)
		}
	}
	if r.h.Get("Content-Security-Policy") == "" {
		t.Error("la pagina non ha le intestazioni delle pagine")
	}
	if contaRighe(t, d, `SELECT count(*) FROM inviti_utenti WHERE accettato_il IS NOT NULL`) != 0 {
		t.Error("aprire ha speso il link")
	}
	invia := func(b *browser, token, pw, conf string) risposta {
		b.vai("GET", "/login", nil, nil)
		return b.vai("POST", "/invito/"+token, url.Values{"_csrf_token": {b.token}, "password[password]": {pw}, "password[password_confirmation]": {conf}}, nil)
	}
	r = invia(b, ospite.Token, "una password lunga", "un'altra password")
	if r.codice != 422 || !regexp.MustCompile(`<p id="invitation-error">Le due password non coincidono\.</p>`).MatchString(r.corpo) ||
		!strings.Contains(r.corpo, "ospite@esempio.it") {
		t.Errorf("password diverse: %d", r.codice)
	}
	r = invia(b, ospite.Token, "corta", "corta")
	if r.codice != 422 || !strings.Contains(r.corpo, `<p id="invitation-error">La password deve avere almeno 12 caratteri.</p>`) {
		t.Errorf("password corta: %d", r.codice)
	}
	var hash any
	_ = d.Lettura.QueryRow(`SELECT hash_password FROM utenti WHERE email = 'ospite@esempio.it'`).Scan(&hash)
	if hash != nil || contaRighe(t, d, `SELECT count(*) FROM inviti_utenti WHERE accettato_il IS NOT NULL`) != 0 {
		t.Error("un rifiuto ha scritto la password o speso il link")
	}
	// Il rifiuto di riferimento: un token inventato.
	rifiuto := func(r risposta) string {
		t.Helper()
		if r.codice != 404 || !strings.Contains(r.corpo, `id="invitation-refused"`) || !strings.Contains(r.corpo, "Invito non valido") ||
			!strings.Contains(r.corpo, "Questo link non è utilizzabile. Chiedi al titolare di generarne uno nuovo dalla pagina degli utenti.") ||
			strings.Contains(r.corpo, "@esempio.it") {
			t.Errorf("rifiuto: %d %s", r.codice, r.corpo)
		}
		return corpoSenzaCSRF(r.corpo)
	}
	inventato := rifiuto(nuovoBrowser(t, h).vai("GET", "/invito/token-inventato", nil, nil))
	if got := rifiuto(invia(nuovoBrowser(t, h), "token-inventato", "una password lunga", "una password lunga")); got != inventato {
		t.Error("il POST di un token inventato non è lo stesso rifiuto")
	}
	if got := rifiuto(invia(nuovoBrowser(t, h), "token-inventato", "una password lunga", "diversa")); got != inventato {
		t.Error("password diverse su un token inventato non sono lo stesso rifiuto")
	}
	for _, campo := range []string{"password[password]", "password[password_confirmation]"} {
		una := nuovoBrowser(t, h)
		una.vai("GET", "/login", nil, nil)
		if got := rifiuto(una.vai("POST", "/invito/"+ospite.Token, url.Values{"_csrf_token": {una.token}, campo: {"una password lunga"}}, nil)); got != inventato {
			t.Errorf("il modulo con il solo %s non è lo stesso rifiuto", campo)
		}
	}
	senzaCampi := nuovoBrowser(t, h)
	senzaCampi.vai("GET", "/login", nil, nil)
	if got := rifiuto(senzaCampi.vai("POST", "/invito/"+ospite.Token, url.Values{"_csrf_token": {senzaCampi.token}}, nil)); got != inventato {
		t.Error("il modulo senza campi non è lo stesso rifiuto")
	}
	// Riuscito.
	r = invia(b, ospite.Token, "una password lunga", "una password lunga")
	if !redirectA(r, "/login") {
		t.Fatalf("accettazione: %d %q", r.codice, r.location)
	}
	if r := b.vai("GET", "/login", nil, nil); !strings.Contains(r.corpo, "Password impostata. Ora puoi accedere.") {
		t.Error("manca «Password impostata. Ora puoi accedere.»")
	}
	if r := b.prova("ospite@esempio.it", "una password lunga"); !redirectA(r, "/") {
		t.Errorf("l'invitato non entra: %d", r.codice)
	}
	// Speso.
	if got := rifiuto(nuovoBrowser(t, h).vai("GET", "/invito/"+ospite.Token, nil, nil)); got != inventato {
		t.Error("il link speso non è lo stesso rifiuto")
	}
	if got := rifiuto(invia(nuovoBrowser(t, h), ospite.Token, "un'altra password lunga", "un'altra password lunga")); got != inventato {
		t.Error("il POST di un link speso non è lo stesso rifiuto")
	}
	// Scaduto e di un account disattivato.
	scaduto, _ := inv.Invita(context.Background(), "scaduto@esempio.it", "", "dipendente", chi)
	uscito, _ := inv.Invita(context.Background(), "uscira@esempio.it", "", "dipendente", chi)
	if err := accounts.NuoviUtenti(d, o).Disattiva(context.Background(), uscito.UtenteID, chi); err != nil {
		t.Fatal(err)
	}
	if got := rifiuto(nuovoBrowser(t, h).vai("GET", "/invito/"+uscito.Token, nil, nil)); got != inventato {
		t.Error("il link di un account disattivato non è lo stesso rifiuto")
	}
	o.Sposta(15 * time.Minute)
	if got := rifiuto(nuovoBrowser(t, h).vai("GET", "/invito/"+scaduto.Token, nil, nil)); got != inventato {
		t.Error("il link scaduto non è lo stesso rifiuto")
	}
}

// inventario: ACC-05, ACC-09
// /invito è aperta come /reset: un titolare già dentro la apre anche lui (la pagina non chiede
// l'accesso e non lo toglie).
func TestPaginaInvitoConAccesso(t *testing.T) {
	h, d, o := facciaConInviti(t, contiBase, indirizzoProva)
	ospite, err := accounts.NuoviInviti(d, o).Invita(context.Background(), "ospite@esempio.it", "", "dipendente", audit.Contesto{Attore: "titolare@esempio.it"})
	if err != nil {
		t.Fatal(err)
	}
	b := titolareDentro(t, h)
	if r := b.vai("GET", "/invito/"+ospite.Token, nil, nil); r.codice != 200 || !strings.Contains(r.corpo, "ospite@esempio.it") {
		t.Errorf("con accesso: %d", r.codice)
	}
	if r := b.vai("GET", "/settings/utenti", nil, nil); r.codice != 200 {
		t.Errorf("la pagina dell'invito ha tolto l'accesso: %d", r.codice)
	}
}
