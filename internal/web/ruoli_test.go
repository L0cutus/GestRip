package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/clock"
)

// tabellaPermessi è la tabella dei permessi dello standalone a go-baseline = d3bab62 (ACC-05):
// router.ex (pipeline :require_owner_user, live_session :owner), user_auth.ex e gli eventi delle
// pagine comuni che chiedono Accounts.owner?/1. Ogni rotta della faccia interna del Go deve stare
// qui con lo stesso permesso (TestOgniRottaHaIlSuoPermesso); goRotta è vuota finché la funzione
// non c'è nel Go. Quando una slice porta una di queste funzioni, scrive qui la sua rotta e il test
// forgiato del dipendente.
//
// Le rotte del titolare rispondono al dipendente con un 302 a / e «Operazione riservata al
// titolare.» (require_owner/2, on_mount :ensure_owner). Le azioni dentro una pagina comune hanno
// invece il loro messaggio (rifiuto), sulla stessa pagina, senza scrivere niente: la slice che ne
// porta una le dà un permesso apposta, mai personale (TestUnaVoceUnPermesso).
var tabellaPermessi = []struct {
	standalone string // rotta o evento dello standalone
	goRotta    string // pattern del mux Go, "" se non c'è ancora
	chi        permesso
	voce       string
	rifiuto    string // per le azioni dentro una pagina comune: il messaggio dello standalone
}{
	{"GET /healthz", "GET /healthz", aperta, "ADM-20", ""},
	{"GET /login", "GET /login", aperta, "ACC-01", ""},
	{"POST /login", "POST /login", aperta, "ACC-01", ""},
	{"DELETE /logout", "POST /logout", aperta, "ACC-03", ""},
	{"live / (WorkshopLive)", "GET /{$}", personale, "RIP-H-01", ""},
	// Aperte, ma l'azione vale solo per il titolare attivo con password (password_recovery.ex):
	// un dipendente non riceve mai il link. La regola sta nel dominio, non nella rotta.
	{"GET /reset-request", "GET /reset-request", aperta, "ACC-13", ""},
	{"POST /reset-request", "POST /reset-request", aperta, "ACC-13", ""},
	{"GET /reset/:token", "GET /reset/{token}", aperta, "ACC-14", ""},
	{"POST /reset/:token", "POST /reset/{token}", aperta, "ACC-14", ""},
	// Aperte: il token nel percorso è tutta l'autorizzazione (InvitationController).
	{"GET /invito/:token", "GET /invito/{token}", aperta, "ACC-09", ""},
	{"POST /invito/:token", "POST /invito/{token}", aperta, "ACC-09", ""},
	// Solo Go (ACC-16 sostituita: lo standalone crea il primo titolare dall'installer, senza
	// rotta). Aperte: il codice nel percorso è tutta l'autorizzazione, e vale solo con utenti vuota.
	{"nessuna rotta (GET; Bootstrap.admin_from_files/2)", "GET /configura/{codice}", aperta, "ACC-16", ""},
	{"nessuna rotta (POST; Bootstrap.admin_from_files/2)", "POST /configura/{codice}", aperta, "ACC-16", ""},
	// Aperte e senza sessione: il browser le chiede da solo (PwaController, BrandingController e
	// Plug.Static nello standalone, fuori da ogni pipeline). Le icone sono quelle di ripiego finché
	// il logo (ADM-05) non c'è; gli asset hanno nel nome un pezzo dell'hash del contenuto.
	{"GET /manifest.webmanifest", "GET /manifest.webmanifest", aperta, "ADM-07", ""},
	{"GET /sw.js", "GET /sw.js", aperta, "ADM-07", ""},
	{"GET /branding/icon-192.png", "GET /branding/icon-192.png", aperta, "ADM-07", ""},
	{"GET /branding/icon-512.png", "GET /branding/icon-512.png", aperta, "ADM-07", ""},
	{"Plug.Static /assets e /settings-tabs.css", "GET /assets/{nome}", aperta, "ADM-22", ""},

	// Pagine del titolare (live_session :owner).
	{"live /audit", "GET /audit", soloTitolare, "ADM-16", ""},
	{"live /settings", "GET /settings", soloTitolare, "ADM-01", ""},
	{"live /settings/shop", "", soloTitolare, "ADM-02", ""},
	{"live /settings/providers", "", soloTitolare, "ADM-09", ""},
	{"live /settings/assistente", "", soloTitolare, "ADM-11", ""},
	{"live /settings/promemoria", "", soloTitolare, "JOB-05", ""},
	{"live /settings/templates", "", soloTitolare, "ADM-13", ""},
	{"live /settings/utenti", "GET /settings/utenti", soloTitolare, "ACC-06", ""},
	{"evento deactivate (UsersLive)", "POST /settings/utenti/{id}/disattiva", soloTitolare, "ACC-10", ""},
	{"evento set_role (UsersLive)", "POST /settings/utenti/{id}/ruolo", soloTitolare, "ACC-11", ""},
	{"evento invite (UsersLive)", "POST /settings/utenti/invita", soloTitolare, "ACC-07", ""},
	{"evento reissue (UsersLive)", "POST /settings/utenti/{id}/nuovo-link", soloTitolare, "ACC-08", ""},
	{"live /settings/cestino", "", soloTitolare, "RIP-S-54", ""},
	{"live /settings/retention-foto", "", soloTitolare, "ADM-15", ""},
	// Rotte del titolare (pipeline :require_owner_user).
	{"GET /audit/export.csv", "", soloTitolare, "ADM-18", ""},
	{"GET /export.zip", "", soloTitolare, "ADM-19", ""},
	{"POST /settings/sessions/terminate-all", "", soloTitolare, "ACC-12", ""},
	{"POST /settings/logo", "", soloTitolare, "ADM-05", ""},
	{"POST /repairs/:repair_id/firma/reset", "", soloTitolare, "RIP-S-40", ""},
	{"POST /handovers/history/:id/delete", "", soloTitolare, "ART-17", ""},
	{"POST /handovers/history/:id/detach", "", soloTitolare, "ART-18", ""},

	// Azioni del titolare dentro pagine comuni.
	{"evento revoke-link (RepairLive)", "", soloTitolare, "CLI-24", "Solo il titolare può revocare il link."},
	{"evento regenerate-link (RepairLive)", "", soloTitolare, "CLI-25", "Solo il titolare può generare un nuovo link."},
	{"evento training-exclude-customer (RepairLive)", "", soloTitolare, "RIP-H-50", "Solo il titolare può escludere un cliente."},
	{"evento close (EmergencyLive)", "", soloTitolare, "EME-14", "Solo il titolare può chiudere l'evento d'emergenza."},
	{"evento reverse (EmergencyLive)", "", soloTitolare, "EME-12", "Solo il titolare può stornare una riga."},
	{"evento delete (EmergencyLive)", "", soloTitolare, "EME-13", "Solo il titolare può eliminare una riga."},
	{"evento save di una vendita emessa (SalesLive, revisione)", "", soloTitolare, "VEN-14", "Operazione riservata al titolare."},
	{"evento trash (SalesLive)", "", soloTitolare, "VEN-18", "Operazione riservata al titolare."},
	{"evento delete di una bozza (SalesLive)", "", soloTitolare, "VEN-07", "Operazione riservata al titolare."},
	{"evento reopen (SalesLive)", "", soloTitolare, "VEN-17", "Operazione riservata al titolare."},
	{"evento revoke (SalesLive, revisione)", "", soloTitolare, "VEN-26", "Operazione riservata al titolare."},
	{"evento close con rimborso o annullamento (SalesLive)", "", soloTitolare, "VEN-16", "Operazione riservata al titolare."},
	{"evento restore di una vendita (TrashLive)", "", soloTitolare, "VEN-18", ""},
	{"evento purge (TrashLive)", "", soloTitolare, "RIP-S-53", ""},
}

// inventario: ACC-05
// Ogni rotta della faccia interna sta nella tabella dei permessi dello standalone con lo stesso
// permesso, e ogni riga della tabella che nomina una rotta Go la trova registrata.
func TestOgniRottaHaIlSuoPermesso(t *testing.T) {
	_, f, err := costruisciInterna(dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, &utentiFinti{attivo: true}), nil)
	if err != nil {
		t.Fatal(err)
	}
	inTabella := map[string]permesso{}
	for _, r := range tabellaPermessi {
		if r.goRotta != "" {
			if _, doppia := inTabella[r.goRotta]; doppia {
				t.Errorf("rotta %q due volte nella tabella", r.goRotta)
			}
			inTabella[r.goRotta] = r.chi
		}
		if (r.rifiuto != "") && r.chi != soloTitolare {
			t.Errorf("%s: un messaggio di rifiuto su un'azione non del titolare", r.standalone)
		}
	}
	registrate := map[string]bool{}
	for _, r := range f.rotte {
		registrate[r.pattern] = true
		chi, ok := inTabella[r.pattern]
		if !ok {
			t.Errorf("rotta %q senza permesso nella tabella dello standalone", r.pattern)
		} else if chi != r.chi {
			t.Errorf("rotta %q: permesso %v, la tabella dice %v", r.pattern, r.chi, chi)
		}
	}
	for p := range inTabella {
		if !registrate[p] {
			t.Errorf("la tabella nomina %q, che la faccia interna non registra", p)
		}
	}
}

// inventario: ACC-05
// Una voce con un'azione del titolare non può avere una rotta Go di tutti: chi porta CLI-24
// registrando la revoca come personale (con una riga nuova) fa fallire il test, invece di lasciare
// la riga del titolare vuota accanto.
func TestUnaVoceUnPermesso(t *testing.T) {
	titolare := map[string]bool{}
	for _, r := range tabellaPermessi {
		if r.chi == soloTitolare {
			titolare[r.voce] = true
		}
	}
	for _, r := range tabellaPermessi {
		if r.chi != soloTitolare && titolare[r.voce] {
			t.Errorf("%s (%s): la voce ha azioni del titolare, ma questa riga è %v", r.standalone, r.voce, r.chi)
		}
	}
	visti := map[string]bool{}
	for _, r := range tabellaPermessi {
		if visti[r.standalone] {
			t.Errorf("%s due volte nella tabella", r.standalone)
		}
		visti[r.standalone] = true
	}
}

// inventario: ACC-05, ACC-17
// Nelle due facce le rotte passano tutte da f.rotta, f.rottaPubblica o f.speciale, così finiscono
// nell'elenco della faccia (la tabella dei permessi per l'interna) e hanno la loro catena (CSRF e
// intestazioni): un mux.Handle diretto sfuggirebbe ai controlli e ai test che iterano sull'elenco.
func TestNessunaRottaFuoriDallaTabella(t *testing.T) {
	file, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range file {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, riga := range strings.Split(string(b), "\n") {
			if !strings.Contains(riga, ".Handle(") && !strings.Contains(riga, ".HandleFunc(") {
				continue
			}
			// Ammessa solo la registrazione dentro registra.
			if f == "ruoli.go" && strings.Contains(riga, "mux.Handle(pattern, h)") {
				continue
			}
			t.Errorf("%s:%d: rotta registrata fuori da f.rotta/f.rottaPubblica/f.speciale: %s", f, i+1, strings.TrimSpace(riga))
		}
	}
}

var reSegnaposto = regexp.MustCompile(`\{[^}]*\}`)

// percorsoDi dà un percorso concreto per un pattern del mux: i segnaposto diventano «1».
func percorsoDi(pattern string) (metodo, percorso string) {
	metodo, percorso, _ = strings.Cut(pattern, " ")
	return metodo, reSegnaposto.ReplaceAllStringFunc(percorso, func(s string) string {
		if s == "{$}" {
			return ""
		}
		return "1"
	})
}

// facciaConRotteDelTitolare: la faccia interna vera più una pagina e un modulo del titolare di
// prova, per provare il meccanismo finché il Go non ha rotte del titolare sue. chiamate conta le
// volte che i gestori di prova sono arrivati a lavorare.
func facciaConRotteDelTitolare(t *testing.T, u *utentiFinti, o clock.Orologio) (http.Handler, *facciaWeb, *atomic.Int32) {
	t.Helper()
	d := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, u)
	if o != nil {
		d.Orologio = o
	}
	var chiamate atomic.Int32
	lavora := func(w http.ResponseWriter, r *http.Request) {
		chiamate.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("riservato"))
	}
	h, f, err := costruisciInterna(d, func(f *facciaWeb, mux *http.ServeMux) {
		f.rotta(mux, "GET /prova/titolare", soloTitolare, lavora)
		f.rotta(mux, "POST /prova/titolare/{id}", soloTitolare, lavora)
	})
	if err != nil {
		t.Fatal(err)
	}
	return h, f, &chiamate
}

const msgSoloTitolareAtteso = "Operazione riservata al titolare."

// inventario: ACC-05
// Il dipendente che forza un indirizzo del titolare, pagina o modulo con un token valido, va a /
// con «Operazione riservata al titolare.» (non un 403) e il gestore non lavora; un modulo senza
// token è il 403 del CSRF, controllato prima come nello standalone (protect_from_forgery prima di
// require_owner). Senza accesso si va a /login. Il titolare passa. Vale per ogni rotta del
// titolare registrata, comprese quelle che arriveranno.
func TestDipendenteForzaLeRotteDelTitolare(t *testing.T) {
	u := &utentiFinti{attivo: true, ruolo: accounts.Dipendente}
	h, f, chiamate := facciaConRotteDelTitolare(t, u, nil)
	var delTitolare []string
	for _, r := range f.rotte {
		if r.chi == soloTitolare {
			delTitolare = append(delTitolare, r.pattern)
		}
	}
	if len(delTitolare) < 2 {
		t.Fatalf("rotte del titolare: %v", delTitolare)
	}
	for _, pattern := range delTitolare {
		metodo, percorso := percorsoDi(pattern)
		fuori := nuovoBrowser(t, h)
		fuori.vai("GET", "/login", nil, nil)
		var form url.Values
		if metodo == "POST" {
			form = url.Values{"_csrf_token": {fuori.token}}
		}
		if r := fuori.vai(metodo, percorso, form, nil); !redirectA(r, "/login") {
			t.Errorf("%s senza accesso: %d %q", pattern, r.codice, r.location)
		}

		b := nuovoBrowser(t, h)
		b.entra(emailProva, passwordProva)
		b.vai("GET", "/", nil, nil)
		if metodo == "POST" {
			if r := b.vai("POST", percorso, url.Values{"x": {"1"}}, nil); r.codice != 403 {
				t.Errorf("%s del dipendente senza token: %d", pattern, r.codice)
			}
			form = url.Values{"_csrf_token": {b.token}}
		} else {
			form = nil
		}
		r := b.vai(metodo, percorso, form, map[string]string{"Sec-Fetch-Dest": "document"})
		if !redirectA(r, "/") {
			t.Errorf("%s del dipendente: %d %q", pattern, r.codice, r.location)
		}
		if r.h.Get("Content-Security-Policy") == "" {
			t.Errorf("%s: il redirect non ha le intestazioni delle pagine", pattern)
		}
		if r := b.vai("GET", "/", nil, nil); r.codice != 200 || !strings.Contains(r.corpo, msgSoloTitolareAtteso) {
			t.Errorf("%s: la pagina dopo non dice %q", pattern, msgSoloTitolareAtteso)
		}
		if r := b.vai("GET", "/", nil, nil); strings.Contains(r.corpo, msgSoloTitolareAtteso) {
			t.Errorf("%s: il messaggio resta anche alla seconda pagina", pattern)
		}
	}
	if n := chiamate.Load(); n != 0 {
		t.Fatalf("il gestore del titolare ha lavorato %d volte per il dipendente", n)
	}
	// Il titolare passa, con il token per i moduli.
	u.cambiaRuolo(accounts.Titolare)
	b := nuovoBrowser(t, h)
	b.entra(emailProva, passwordProva)
	b.vai("GET", "/", nil, nil)
	if r := b.vai("GET", "/prova/titolare", nil, nil); r.codice != 200 || r.corpo != "riservato" {
		t.Errorf("pagina del titolare: %d", r.codice)
	}
	if r := b.vai("POST", "/prova/titolare/7", url.Values{"_csrf_token": {b.token}}, nil); r.codice != 200 {
		t.Errorf("modulo del titolare: %d", r.codice)
	}
	if n := chiamate.Load(); n != 2 {
		t.Errorf("chiamate del titolare: %d", n)
	}
}

// inventario: ACC-04, ACC-05
// Il ruolo si rilegge a ogni richiesta: un titolare declassato perde le pagine del titolare al
// clic dopo, un dipendente promosso le trova, senza rifare l'accesso.
func TestRuoloRilettoAOgniRichiesta(t *testing.T) {
	u := &utentiFinti{attivo: true}
	h, _, _ := facciaConRotteDelTitolare(t, u, nil)
	b := nuovoBrowser(t, h)
	b.entra(emailProva, passwordProva)
	if r := b.vai("GET", "/prova/titolare", nil, nil); r.codice != 200 {
		t.Fatalf("titolare: %d", r.codice)
	}
	u.cambiaRuolo(accounts.Dipendente)
	if r := b.vai("GET", "/prova/titolare", nil, nil); !redirectA(r, "/") {
		t.Errorf("titolare declassato: %d %q", r.codice, r.location)
	}
	if r := b.vai("GET", "/", nil, nil); r.codice != 200 || !strings.Contains(r.corpo, msgSoloTitolareAtteso) {
		t.Errorf("declassato: la home non lo dice (%d)", r.codice)
	}
	u.cambiaRuolo(accounts.Titolare)
	if r := b.vai("GET", "/prova/titolare", nil, nil); r.codice != 200 {
		t.Errorf("dipendente promosso: %d", r.codice)
	}
}

// inventario: ACC-05
// Il dipendente usa le rotte di tutti (la home) come il titolare.
func TestDipendenteUsaLeRotteDiTutti(t *testing.T) {
	u := &utentiFinti{attivo: true, ruolo: accounts.Dipendente}
	b := nuovoBrowser(t, mustInterna(t, dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, u)))
	if r := b.entra(emailProva, passwordProva); !redirectA(r, "/") {
		t.Fatalf("accesso del dipendente: %d", r.codice)
	}
	if r := b.vai("GET", "/", nil, nil); r.codice != 200 || strings.Contains(r.corpo, msgSoloTitolareAtteso) {
		t.Errorf("home del dipendente: %d", r.codice)
	}
	if r := b.vai("POST", "/logout", url.Values{"_csrf_token": {b.token}}, nil); !redirectA(r, "/login") {
		t.Errorf("uscita del dipendente: %d", r.codice)
	}
}

func mustInterna(t *testing.T, d Dipendenze) http.Handler {
	t.Helper()
	h, err := Interna(d)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// inventario: ACC-01, ACC-04
// D-31 (Stefano, 30/10/2026): la sessione vale al massimo 30 giorni dall'accesso, e l'uso non la
// rinnova. Scaduta, la richiesta è come senza accesso: /login con «Devi accedere per continuare.»
// e la pagina chiesta ricordata.
func TestSessioneDuraTrentaGiorni(t *testing.T) {
	o := clock.NuovoFinto(time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC))
	h, _, _ := facciaConRotteDelTitolare(t, &utentiFinti{attivo: true}, o)
	b := nuovoBrowser(t, h)
	b.entra(emailProva, passwordProva)
	// Ventinove giorni di uso, uno al giorno (passa anche il cambio dell'ora di marzo).
	for range 29 {
		o.Sposta(24 * time.Hour)
		if r := b.vai("GET", "/prova/titolare", nil, nil); r.codice != 200 {
			t.Fatalf("giorno %v: %d", o.Adesso(), r.codice)
		}
	}
	o.Imposta(time.Date(2026, 3, 31, 7, 59, 59, 0, time.UTC))
	if r := b.vai("GET", "/", nil, nil); r.codice != 200 {
		t.Fatalf("un secondo prima dei 30 giorni: %d", r.codice)
	}
	o.Sposta(time.Second)
	if r := b.vai("GET", "/prova/titolare", nil, map[string]string{"Sec-Fetch-Dest": "document"}); !redirectA(r, "/login") {
		t.Fatalf("a 30 giorni esatti: %d %q", r.codice, r.location)
	}
	if r := b.vai("GET", "/login", nil, nil); r.codice != 200 || !strings.Contains(r.corpo, msgAccedi) {
		t.Errorf("dopo la scadenza: %d", r.codice)
	}
	if r := b.entra(emailProva, passwordProva); !redirectA(r, "/prova/titolare") {
		t.Errorf("nuovo accesso: %d %q", r.codice, r.location)
	}
	o.Sposta(29 * 24 * time.Hour)
	if r := b.vai("GET", "/", nil, nil); r.codice != 200 {
		t.Errorf("il nuovo accesso conta da capo: %d", r.codice)
	}
}

// inventario: ACC-04
// Un cookie firmato senza l'istante dell'accesso, o con un istante oltre i 30 giorni, non vale
// (D-31); nemmeno se l'account è attivo e la versione giusta.
func TestSessioneSenzaAccessoValido(t *testing.T) {
	o := clock.NuovoFinto(time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC))
	d := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, &utentiFinti{attivo: true})
	d.Orologio = o
	b := nuovoBrowser(t, mustInterna(t, d))
	s := sessioniPer(d.ChiaveSessione, "interna")
	for nome, accesso := range map[string]int64{
		"senza istante": 0,
		"negativo":      -1,
		"oltre 30 gg":   o.Adesso().Add(-30 * 24 * time.Hour).Unix(),
		// Accesso registrato con l'orologio avanti, poi corretto: non deve durare 30 giorni più
		// l'errore (review della 008c). Oltre i 5 minuti di tolleranza la sessione non vale.
		"accesso nel futuro": o.Adesso().Add(5*time.Minute + time.Second).Unix(),
	} {
		rec := httptest.NewRecorder()
		s.scrivi(rec, datiSessione{Utente: 1, Accesso: accesso})
		imposta(b, "__Host-gestrip_sessione_interna", rec.Result().Cookies()[0].Value)
		if r := b.vai("GET", "/", nil, nil); !redirectA(r, "/login") {
			t.Errorf("%s: %d", nome, r.codice)
		}
	}
	rec := httptest.NewRecorder()
	s.scrivi(rec, datiSessione{Utente: 1, Accesso: o.Adesso().Add(-time.Hour).Unix()})
	imposta(b, "__Host-gestrip_sessione_interna", rec.Result().Cookies()[0].Value)
	if r := b.vai("GET", "/", nil, nil); r.codice != 200 {
		t.Errorf("accesso di un'ora fa: %d", r.codice)
	}
	rec = httptest.NewRecorder()
	s.scrivi(rec, datiSessione{Utente: 1, Accesso: o.Adesso().Add(5 * time.Minute).Unix()})
	imposta(b, "__Host-gestrip_sessione_interna", rec.Result().Cookies()[0].Value)
	if r := b.vai("GET", "/", nil, nil); r.codice != 200 {
		t.Errorf("accesso 5 minuti avanti (orologio ritoccato da NTP): %d", r.codice)
	}
}
