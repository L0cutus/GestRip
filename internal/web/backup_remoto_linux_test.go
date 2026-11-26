package web

import (
	"context"
	"errors"
	"html"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/backup"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/ratelimit"
	"github.com/L0cutus/gestrip/internal/sftpfinto"
)

// La copia remota nella pagina Impostazioni → Backup (backup_remoto.go, slice 129) contro il server SFTP finto
// (internal/sftpfinto, niente rete): tutto con moduli normali (senza JavaScript), solo il titolare, con il CSRF;
// impronta confermata riscrivendola, salvataggio solo con la prova riuscita, errori in parole del titolare, tempo
// massimo, una prova alla volta. Dati inventati.

// facciaRemota è una faccia con la pagina Backup (stato finto sano) e la copia remota vera sul server finto.
type facciaRemota struct {
	h       http.Handler
	d       *db.DB
	srv     *sftpfinto.Server
	cr      *backup.ConfigurazioneRemota
	prepara func(*backup.Remoto) // cambia ogni Remoto dopo il server finto
	conns   atomic.Int64         // quante connessioni al server
}

func nuovaFacciaRemota(t *testing.T) *facciaRemota {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(martediPromemoria)
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	inserisciConti(t, d, contiBase)
	f := &facciaRemota{d: d, srv: sftpfinto.Nuovo(t)}
	if err := os.Mkdir(f.srv.Locale("/backup"), 0o700); err != nil {
		t.Fatal(err)
	}
	f.cr = backup.NuovaConfigurazioneRemota(d, o).ConPreparazione(func(r *backup.Remoto) {
		r.Componi = func(ctx context.Context, ind string) (net.Conn, error) {
			f.conns.Add(1)
			return f.srv.Componi(ctx, ind)
		}
		if f.prepara != nil {
			f.prepara(r)
		}
	})
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti = o, ratelimit.Nuovo(o)
	dd.Backup, dd.CopiaRemota = &backupFinto{stato: statoSano()}, f.cr
	f.h = mustInterna(t, dd)
	return f
}

func (f *facciaRemota) titolare(t *testing.T) *browser {
	t.Helper()
	b := titolareDentro(t, f.h)
	b.vai("GET", rottaPaginaBackup, nil, nil)
	return b
}

// configurata dice se la configurazione è salvata.
func (f *facciaRemota) configurata(t *testing.T) (backup.ConfigRemota, bool) {
	t.Helper()
	c, ok, err := f.cr.Leggi(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return c, ok
}

// chiaveAutorizzata crea la chiave dell'apparecchio e la mette nel «pannello» del server finto.
func (f *facciaRemota) chiaveAutorizzata(t *testing.T) {
	t.Helper()
	pub, _, err := f.cr.CreaChiave()
	if err != nil {
		t.Fatal(err)
	}
	k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pub))
	if err != nil {
		t.Fatal(err)
	}
	f.srv.Autorizza(k)
}

func moduloRemotoProva(b *browser, cartella string) url.Values {
	return url.Values{"_csrf_token": {b.token}, "remoto[host]": {"u1.storage.invalid"}, "remoto[porta]": {"23"},
		"remoto[utente]": {"u1-sub1"}, "remoto[cartella]": {cartella}}
}

func conImpronta(v url.Values, impronta string) url.Values {
	c := cloneValues(v)
	c.Set("remoto[impronta]", impronta)
	return c
}

var reChiavePubblica = regexp.MustCompile(`<input type="text" id="backup-remote-pubkey" class="backup-chiave-pubblica" value="(ssh-ed25519 [A-Za-z0-9+/=&#;]+ gestrip-apparecchio)" readonly`)

// inPagina è s come lo scrive html/template (il «+» delle impronte e delle chiavi diventa &#43;).
func inPagina(s string) string { return strings.ReplaceAll(html.EscapeString(s), "+", "&#43;") }

// segreti sono i pezzi che non devono mai stare in una pagina: la chiave privata in qualunque forma.
func nessunSegreto(t *testing.T, cosa, corpo string, f *facciaRemota) {
	t.Helper()
	nonContiene(t, cosa, corpo, "PRIVATE KEY", "BEGIN OPENSSH", "openssh-key-v1")
	if b, err := os.ReadFile(backup.FileChiaveSFTP(filepath.Dir(f.d.Percorso()))); err == nil {
		righe := strings.Split(string(b), "\n")
		for _, r := range righe[1 : len(righe)-2] { // il corpo base64 della privata
			if len(r) > 20 && strings.Contains(corpo, r) {
				t.Errorf("%s: un pezzo della chiave privata nella pagina", cosa)
			}
		}
	}
}

// Voce OPS-16 (destinazione remota), già chiusa dalla 128: niente segno «inventario:».
// Il giro intero con i soli moduli (senza JavaScript): chiave creata e mostrata (solo la pubblica), primo passo che
// mostra l'impronta e non scrive, conferma con un'impronta diversa rifiutata (resta il secondo passo, con il campo
// segnato), conferma giusta che salva, «Prova collegamento» della salvata, spegnimento solo con la spunta.
func TestPaginaCopiaRemota(t *testing.T) {
	f := nuovaFacciaRemota(t)
	b := f.titolare(t)
	r := b.vai("GET", rottaPaginaBackup, nil, nil)
	contiene(t, "pagina nuova", r.corpo, `<section id="backup-remote"`, `<form id="backup-remote-key" action="/settings/backup/chiave-sftp" method="post">`,
		`<form id="backup-remote-form" action="/settings/backup/remoto/impronta" method="post"`, `id="backup-remote-none"`,
		`<label for="backup-remote-host">`, `<label for="backup-remote-port">`, `value="22"`)
	nonContiene(t, "pagina nuova", r.corpo, `id="backup-remote-pubkey"`, `id="backup-remote-test"`, `id="backup-remote-off"`)

	if r := b.vai("POST", rottaChiaveSFTP, url.Values{"_csrf_token": {b.token}}, nil); !redirectA(r, rottaPaginaBackup) {
		t.Fatalf("crea chiave: %d %q", r.codice, r.location)
	}
	r = b.vai("GET", rottaPaginaBackup, nil, nil)
	contiene(t, "chiave creata", r.corpo, html.EscapeString(msgChiaveCreata))
	m := reChiavePubblica.FindStringSubmatch(r.corpo)
	if m == nil {
		t.Fatalf("chiave pubblica non mostrata")
	}
	nonContiene(t, "chiave creata", r.corpo, `id="backup-remote-key"`)
	nessunSegreto(t, "chiave creata", r.corpo, f)
	k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(html.UnescapeString(m[1])))
	if err != nil {
		t.Fatal(err)
	}
	f.srv.Autorizza(k) // il titolare la incolla nel pannello
	// Di nuovo: c'è già, non cambia.
	b.vai("POST", rottaChiaveSFTP, url.Values{"_csrf_token": {b.token}}, nil)
	r = b.vai("GET", rottaPaginaBackup, nil, nil)
	contiene(t, "chiave di nuovo", r.corpo, html.EscapeString(msgChiaveGiaPresente), m[1])

	v := moduloRemotoProva(b, "backup")
	r = b.vai("POST", rottaImprontaRemoto, v, nil)
	contiene(t, "primo passo", r.corpo, `<form id="backup-remote-confirm" action="/settings/backup/remoto" method="post"`,
		inPagina(f.srv.Impronta()), `id="backup-remote-seen-type">ssh-ed25519<`, `name="remoto[impronta]" value="" required`,
		`name="remoto[host]" type="hidden" value="u1.storage.invalid"`, html.EscapeString(msgVediImpronta))
	nonContiene(t, "primo passo", r.corpo, `aria-invalid`)
	if _, ok := f.configurata(t); ok || r.codice != 200 {
		t.Fatalf("il primo passo ha scritto (%d)", r.codice)
	}
	nascosti := url.Values{"_csrf_token": {b.token}}
	for k, x := range v {
		if k != "_csrf_token" {
			nascosti[k] = x
		}
	}

	r = b.vai("POST", rottaRemoto, conImpronta(nascosti, "SHA256:"+strings.Repeat("B", 43)), nil)
	contiene(t, "impronta diversa", r.corpo, "IMPRONTA DIVERSA", `id="backup-remote-confirm"`, `aria-invalid="true"`,
		`id="backup-remote-fingerprint-errore"`, inPagina(f.srv.Impronta()))
	if _, ok := f.configurata(t); ok {
		t.Fatal("salvata con un'impronta diversa")
	}
	r = b.vai("POST", rottaRemoto, conImpronta(nascosti, "non è un'impronta"), nil)
	// Mal scritta: niente da rileggere, si torna al primo passo con i campi scritti (revisione 129, B6).
	contiene(t, "impronta mal scritta", r.corpo, "43 caratteri", `id="backup-remote-form"`, `value="u1.storage.invalid"`)
	nonContiene(t, "impronta mal scritta", r.corpo, `id="backup-remote-confirm"`, inPagina(f.srv.Impronta()))
	// Il secondo passo senza impronta: il browser lo blocca (required), un POST forgiato no, ma non salva.
	r = b.vai("POST", rottaRemoto, nascosti, nil)
	contiene(t, "impronta vuota", r.corpo, "43 caratteri")
	if _, ok := f.configurata(t); ok {
		t.Fatal("salvata senza impronta")
	}

	if r := b.vai("POST", rottaRemoto, conImpronta(nascosti, " "+f.srv.Impronta()+" "), nil); !redirectA(r, rottaPaginaBackup) {
		t.Fatalf("conferma: %d %q %s", r.codice, r.location, r.corpo)
	}
	c, ok := f.configurata(t)
	if !ok || c.Cartella != "/backup" || c.Impronta != f.srv.Impronta() || c.Porta != 23 {
		t.Fatalf("salvata %+v %v", c, ok)
	}
	r = b.vai("GET", rottaPaginaBackup, nil, nil)
	contiene(t, "salvata", r.corpo, html.EscapeString(msgRemotoSalvato), `<dl id="backup-remote-saved"`, "u1.storage.invalid",
		"u1-sub1", "<dd>/backup</dd>", inPagina(f.srv.Impronta()), `<form id="backup-remote-test" action="/settings/backup/remoto/prova" method="post">`,
		`<form id="backup-remote-off" action="/settings/backup/remoto/togli" method="post">`, "Cambia la configurazione",
		`name="remoto[cartella]" value="/backup"`)
	nessunSegreto(t, "salvata", r.corpo, f)

	if r := b.vai("POST", rottaProvaRemoto, url.Values{"_csrf_token": {b.token}}, nil); !redirectA(r, rottaPaginaBackup) {
		t.Fatalf("prova: %d", r.codice)
	}
	contiene(t, "prova", b.vai("GET", rottaPaginaBackup, nil, nil).corpo, html.EscapeString(msgProvaRemotaOK))

	b.vai("POST", rottaTogliRemoto, url.Values{"_csrf_token": {b.token}}, nil)
	contiene(t, "togli senza spunta", b.vai("GET", rottaPaginaBackup, nil, nil).corpo, html.EscapeString(msgSpegniConferma))
	if _, ok := f.configurata(t); !ok {
		t.Fatal("spenta senza spunta")
	}
	if r := b.vai("POST", rottaTogliRemoto, url.Values{"_csrf_token": {b.token}, "remoto[conferma]": {"1"}}, nil); !redirectA(r, rottaPaginaBackup) {
		t.Fatalf("togli: %d", r.codice)
	}
	contiene(t, "togli", b.vai("GET", rottaPaginaBackup, nil, nil).corpo, html.EscapeString(msgRemotoSpento), `id="backup-remote-none"`)
	if _, ok := f.configurata(t); ok {
		t.Fatal("non spenta")
	}
	// Spenta, la prova non ha niente da provare.
	b.vai("POST", rottaProvaRemoto, url.Values{"_csrf_token": {b.token}}, nil)
	contiene(t, "prova da spenta", b.vai("GET", rottaPaginaBackup, nil, nil).corpo, "non è configurata: non c&#39;è niente da provare")
}

// Gli errori in parole del titolare, con il campo segnato quando è di un campo, e niente salvato: campi sbagliati
// (senza collegarsi), chiave non autorizzata, cartella che non c'è, porta chiusa, nome che non si trova, tempo
// scaduto, chiave dell'apparecchio mancante; un server che cambia chiave dopo il salvataggio.
func TestPaginaCopiaRemotaErrori(t *testing.T) {
	porta := func(err error) func(*backup.Remoto) {
		return func(r *backup.Remoto) {
			r.Componi = func(context.Context, string) (net.Conn, error) { return nil, err }
		}
	}
	for _, c := range []struct {
		nome      string
		campo     string // il campo cambiato del modulo
		valore    string
		chiave    bool // chiave dell'apparecchio creata e autorizzata
		prepara   func(*backup.Remoto)
		passo2    bool // l'errore arriva alla conferma (primo passo riuscito)
		frase     string
		segnato   string // l'id del campo con aria-invalid ("" nessuno)
		nessunaCx bool   // non si collega nemmeno
	}{
		{nome: "host con spazi", campo: "remoto[host]", valore: "u1 storage", chiave: true, frase: "Scrivi il nome del server",
			segnato: "backup-remote-host", nessunaCx: true},
		{nome: "porta non numero", campo: "remoto[porta]", valore: "ventitré", chiave: true, frase: "La porta è un numero",
			segnato: "backup-remote-port", nessunaCx: true},
		{nome: "utente vuoto", campo: "remoto[utente]", valore: " ", chiave: true, frase: "utente del sottoaccount",
			segnato: "backup-remote-user", nessunaCx: true},
		{nome: "chiave non autorizzata", passo2: true, frase: "ha rifiutato la chiave dell&#39;apparecchio", segnato: "backup-remote-user"},
		{nome: "chiave assente", passo2: true, frase: "Manca la chiave dell&#39;apparecchio"},
		{nome: "cartella che non c'è", campo: "remoto[cartella]", valore: "manca", chiave: true, passo2: true,
			frase: "La cartella non c&#39;è sul server", segnato: "backup-remote-dir"},
		{nome: "porta chiusa", chiave: true, prepara: porta(&net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}),
			frase: "porta chiusa", segnato: "backup-remote-port"},
		{nome: "nome che non si trova", chiave: true, prepara: porta(&net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "u1.storage.invalid"}}),
			frase: "Il nome del server non si trova", segnato: "backup-remote-host"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			f := nuovaFacciaRemota(t)
			if c.chiave {
				f.chiaveAutorizzata(t)
			} else if c.nome == "chiave non autorizzata" {
				if _, _, err := f.cr.CreaChiave(); err != nil {
					t.Fatal(err)
				}
			}
			f.prepara = c.prepara
			b := f.titolare(t)
			v := moduloRemotoProva(b, "backup")
			if c.campo != "" {
				v.Set(c.campo, c.valore)
			}
			rotta := rottaImprontaRemoto
			if c.passo2 {
				rotta = rottaRemoto
				v = conImpronta(v, f.srv.Impronta())
			}
			r := b.vai("POST", rotta, v, nil)
			if r.codice != 200 {
				t.Fatalf("%d", r.codice)
			}
			contiene(t, c.nome, r.corpo, `<div role="alert" class="flash flash-errore">`, c.frase)
			if c.segnato != "" {
				contiene(t, c.nome, r.corpo, `id="`+c.segnato+`" name=`, c.segnato+`-errore`)
				if !regexp.MustCompile(`id="` + c.segnato + `" [^>]*aria-invalid="true"`).MatchString(r.corpo) {
					t.Errorf("campo %s non segnato", c.segnato)
				}
			} else {
				nonContiene(t, c.nome, r.corpo, `aria-invalid`)
			}
			// Il modulo torna con i valori scritti (il campo sbagliato compreso), al primo passo.
			contiene(t, c.nome, r.corpo, `id="backup-remote-form"`)
			nonContiene(t, c.nome, r.corpo, "storage.invalid:", "connect:", "no such host")
			if _, ok := f.configurata(t); ok {
				t.Error("salvata")
			}
			if c.nessunaCx && f.conns.Load() != 0 {
				t.Errorf("collegato %d volte con un campo sbagliato", f.conns.Load())
			}
		})
	}

	// Tempo scaduto: il server resta fermo sulla cartella oltre il tempo della prova.
	t.Run("tempo scaduto", func(t *testing.T) {
		f := nuovaFacciaRemota(t)
		f.chiaveAutorizzata(t)
		ferma := make(chan struct{})
		var una sync.Once
		sblocca := func() { una.Do(func() { close(ferma) }) }
		t.Cleanup(sblocca)
		f.srv.Imposta(sftpfinto.Guasti{AlLstat: func(p string) {
			if p == "/backup" {
				<-ferma
			}
		}})
		f.prepara = func(r *backup.Remoto) { r.TempoProva = 300 * time.Millisecond }
		b := f.titolare(t)
		inizio := clock.Parti()
		r := b.vai("POST", rottaRemoto, conImpronta(moduloRemotoProva(b, "backup"), f.srv.Impronta()), nil)
		contiene(t, "tempo scaduto", r.corpo, "entro il tempo massimo")
		if d := inizio.Trascorso(); d > 10*time.Second {
			t.Errorf("durata %v", d)
		}
		if _, ok := f.configurata(t); ok {
			t.Error("salvata")
		}
	})

	// Impronta cambiata: salvata con un server, poi allo stesso indirizzo risponde un server con un'altra chiave.
	t.Run("impronta cambiata", func(t *testing.T) {
		f := nuovaFacciaRemota(t)
		f.chiaveAutorizzata(t)
		b := f.titolare(t)
		if r := b.vai("POST", rottaRemoto, conImpronta(moduloRemotoProva(b, "backup"), f.srv.Impronta()), nil); !redirectA(r, rottaPaginaBackup) {
			t.Fatalf("conferma: %d", r.codice)
		}
		b.vai("GET", rottaPaginaBackup, nil, nil)
		altro := sftpfinto.Nuovo(t)
		pub, _ := f.cr.ChiavePubblica()
		k, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(pub))
		altro.Autorizza(k)
		_ = os.Mkdir(altro.Locale("/backup"), 0o700)
		f.prepara = func(r *backup.Remoto) { r.Componi = altro.Componi }
		b.vai("POST", rottaProvaRemoto, url.Values{"_csrf_token": {b.token}}, nil)
		r := b.vai("GET", rottaPaginaBackup, nil, nil)
		contiene(t, "impronta cambiata", r.corpo, "impronta diversa da quella salvata", `role="alert"`)
		nonContiene(t, "impronta cambiata", r.corpo, inPagina(altro.Impronta()), html.EscapeString(msgProvaRemotaOK))
		if c, ok := f.configurata(t); !ok || c.Impronta != f.srv.Impronta() {
			t.Errorf("configurazione cambiata: %+v", c)
		}
	})
}

// Solo il titolare, con il CSRF: il dipendente torna a / senza che niente parta (nessuna chiave, nessuna
// connessione, niente scritto né spento); il titolare senza token ha il 403; senza la copia remota nelle dipendenze
// la pagina non ha la sezione e le azioni rispondono con l'errore.
func TestPaginaCopiaRemotaRuoloECSRF(t *testing.T) {
	f := nuovaFacciaRemota(t)
	modulo := func(tok string) url.Values {
		v := url.Values{"_csrf_token": {tok}, "remoto[host]": {"u1.storage.invalid"}, "remoto[porta]": {"23"},
			"remoto[utente]": {"u1-sub1"}, "remoto[cartella]": {"backup"}, "remoto[impronta]": {f.srv.Impronta()},
			"remoto[conferma]": {"1"}}
		return v
	}
	rotte := []string{rottaChiaveSFTP, rottaImprontaRemoto, rottaRemoto, rottaProvaRemoto, rottaTogliRemoto}
	dip := dipendenteDentro(t, f.h)
	dip.vai("GET", "/", nil, nil)
	for _, p := range rotte {
		if r := dip.vai("POST", p, modulo(dip.token), nil); !redirectA(r, "/") {
			t.Errorf("dipendente %s: %d %q", p, r.codice, r.location)
		}
	}
	tit := titolareDentro(t, f.h)
	tit.vai("GET", "/", nil, nil)
	for _, p := range rotte {
		for nome, tok := range map[string]string{"senza token": "", "token sbagliato": "inventato"} {
			v := modulo(tok)
			if tok == "" {
				v.Del("_csrf_token")
			}
			if r := tit.vai("POST", p, v, nil); r.codice != 403 {
				t.Errorf("titolare %s %s: %d", nome, p, r.codice)
			}
		}
	}
	if _, err := f.cr.ChiavePubblica(); err == nil {
		t.Error("chiave creata senza permesso")
	}
	if _, ok := f.configurata(t); ok || f.conns.Load() != 0 {
		t.Errorf("configurata %v, connessioni %d", ok, f.conns.Load())
	}

	// Configurata dal titolare, il dipendente non la spegne.
	f.chiaveAutorizzata(t)
	tit.vai("GET", rottaPaginaBackup, nil, nil)
	if r := tit.vai("POST", rottaRemoto, modulo(tit.token), nil); !redirectA(r, rottaPaginaBackup) {
		t.Fatalf("conferma: %d", r.codice)
	}
	dip.vai("POST", rottaTogliRemoto, modulo(dip.token), nil)
	if _, ok := f.configurata(t); !ok {
		t.Error("spenta dal dipendente")
	}

	// Senza la copia remota nelle dipendenze.
	h, _ := facciaBackup(t, &backupFinto{stato: statoSano()})
	b := titolareDentro(t, h)
	r := b.vai("GET", rottaPaginaBackup, nil, nil)
	nonContiene(t, "senza copia remota", r.corpo, `id="backup-remote"`)
	for _, p := range rotte {
		if r := b.vai("POST", p, modulo(b.token), nil); r.codice < 400 {
			t.Errorf("senza copia remota %s: %d", p, r.codice)
		}
	}
}

// Una prova alla volta, e intanto le altre pagine rispondono: mentre una conferma è ferma sul server, un'altra
// richiesta di prova (anche da un'altra sessione del titolare) risponde subito «in corso» senza collegarsi, e la
// pagina Backup e la home rispondono; finita la prima, è salvata.
func TestPaginaCopiaRemotaProvaConcorrente(t *testing.T) {
	f := nuovaFacciaRemota(t)
	f.chiaveAutorizzata(t)
	ferma, arrivato := make(chan struct{}), make(chan struct{})
	var una, unaArrivo sync.Once
	sblocca := func() { una.Do(func() { close(ferma) }) }
	t.Cleanup(sblocca)
	f.srv.Imposta(sftpfinto.Guasti{AlLstat: func(p string) {
		if p == "/backup" {
			unaArrivo.Do(func() { close(arrivato) })
			<-ferma
		}
	}})
	primo, secondo := f.titolare(t), f.titolare(t)
	v := conImpronta(moduloRemotoProva(primo, "backup"), f.srv.Impronta())
	fatto := make(chan risposta, 1)
	go func() { fatto <- primo.vai("POST", rottaRemoto, v, nil) }()
	select {
	case <-arrivato:
	case <-time.After(10 * time.Second):
		t.Fatal("la prima prova non arriva al server")
	}
	connPrima := f.conns.Load()
	inizio := clock.Parti()
	r := secondo.vai("POST", rottaImprontaRemoto, moduloRemotoProva(secondo, "backup"), nil)
	contiene(t, "seconda", r.corpo, "Un&#39;altra prova del collegamento è in corso")
	secondo.vai("POST", rottaProvaRemoto, url.Values{"_csrf_token": {secondo.token}}, nil)
	contiene(t, "seconda prova", secondo.vai("GET", rottaPaginaBackup, nil, nil).corpo, "Un&#39;altra prova del collegamento è in corso")
	if r := secondo.vai("GET", "/", nil, nil); r.codice != 200 {
		t.Errorf("home durante la prova: %d", r.codice)
	}
	if d := inizio.Trascorso(); d > 3*time.Second {
		t.Errorf("le altre richieste hanno aspettato %v", d)
	}
	if f.conns.Load() != connPrima {
		t.Errorf("la seconda si è collegata")
	}
	sblocca()
	select {
	case r := <-fatto:
		if !redirectA(r, rottaPaginaBackup) {
			t.Fatalf("prima: %d %s", r.codice, r.corpo)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("la prima non finisce")
	}
	if _, ok := f.configurata(t); !ok {
		t.Error("non salvata")
	}
}

// Revisione 129, B6: il modulo non porta più l'impronta vista; campi forgiati con quel nome non si mostrano mai. Il
// secondo passo dopo un errore mostra solo l'impronta riletta dal server (impronta diversa).
func TestPaginaCopiaRemotaImprontaVistaForgiata(t *testing.T) {
	f := nuovaFacciaRemota(t)
	f.chiaveAutorizzata(t)
	b := f.titolare(t)
	finta := "SHA256:" + strings.Repeat("C", 43)
	v := conImpronta(moduloRemotoProva(b, "backup"), finta)
	v.Set("remoto[tipo_visto]", "ssh-ed25519")
	v.Set("remoto[impronta_vista]", finta)
	r := b.vai("POST", rottaRemoto, v, nil)
	contiene(t, "forgiata", r.corpo, "IMPRONTA DIVERSA", `id="backup-remote-seen-fingerprint"><code>`+inPagina(f.srv.Impronta()))
	if strings.Count(r.corpo, inPagina(finta)) != 0 {
		t.Error("impronta forgiata nella pagina")
	}
	if _, ok := f.configurata(t); ok {
		t.Fatal("salvata con l'impronta forgiata")
	}
	v.Set("remoto[impronta]", "x")
	r = b.vai("POST", rottaRemoto, v, nil)
	nonContiene(t, "mal scritta", r.corpo, inPagina(finta), `id="backup-remote-confirm"`)
	contiene(t, "mal scritta", r.corpo, `id="backup-remote-form"`)
}

// Revisione 129, M2: una configurazione salvata che non si legge si può spegnere dalla pagina.
func TestPaginaCopiaRemotaIlleggibileSiSpegne(t *testing.T) {
	f := nuovaFacciaRemota(t)
	if _, err := f.d.Scrittura.ExecContext(context.Background(), `INSERT INTO impostazioni (chiave, valore, aggiornata_il)
		VALUES (?, '{rotta', '2026-11-26T10:00:00Z')`, backup.ChiaveRemoto); err != nil {
		t.Fatal(err)
	}
	b := f.titolare(t)
	r := b.vai("GET", rottaPaginaBackup, nil, nil)
	contiene(t, "illeggibile", r.corpo, `id="backup-remote-unreadable"`, `<form id="backup-remote-off" action="/settings/backup/remoto/togli" method="post">`)
	nonContiene(t, "illeggibile", r.corpo, `id="backup-remote-test"`, `id="backup-remote-saved"`)
	if r := b.vai("POST", rottaTogliRemoto, url.Values{"_csrf_token": {b.token}, "remoto[conferma]": {"1"}}, nil); !redirectA(r, rottaPaginaBackup) {
		t.Fatalf("togli: %d", r.codice)
	}
	contiene(t, "spenta", b.vai("GET", rottaPaginaBackup, nil, nil).corpo, html.EscapeString(msgRemotoSpento), `id="backup-remote-none"`)
	if _, ok, _ := f.cr.Leggi(context.Background()); ok {
		t.Error("ancora configurata")
	}
}

// togliGuasto è la copia remota vera con uno spegnimento che fallisce.
type togliGuasto struct {
	*backup.ConfigurazioneRemota
	err error
}

func (g togliGuasto) Togli(context.Context) error { return g.err }

// Revisione 129 (M14 del revisore, B4): se lo spegnimento non riesce la pagina lo dice, e niente «spenta»; durante
// una prova in corso dice «in corso».
func TestPaginaCopiaRemotaTogliNonRiuscito(t *testing.T) {
	for _, c := range []struct {
		nome, frase string
		err         error
	}{
		{"guasto", msgRemotoNonSpento, errors.New("database inventato occupato")},
		{"prova in corso", "Un'altra prova del collegamento è in corso", backup.ErrProvaInCorso},
	} {
		t.Run(c.nome, func(t *testing.T) {
			f := nuovaFacciaRemota(t)
			dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(f.d, clock.NuovoFinto(martediPromemoria)))
			dd.Orologio = clock.NuovoFinto(martediPromemoria)
			dd.Limiti = ratelimit.Nuovo(dd.Orologio)
			dd.Backup, dd.CopiaRemota = &backupFinto{stato: statoSano()}, togliGuasto{f.cr, c.err}
			b := titolareDentro(t, mustInterna(t, dd))
			b.vai("GET", rottaPaginaBackup, nil, nil)
			b.vai("POST", rottaTogliRemoto, url.Values{"_csrf_token": {b.token}, "remoto[conferma]": {"1"}}, nil)
			r := b.vai("GET", rottaPaginaBackup, nil, nil)
			contiene(t, c.nome, r.corpo, `<div role="alert" class="flash flash-errore">`, html.EscapeString(c.frase))
			nonContiene(t, c.nome, r.corpo, html.EscapeString(msgRemotoSpento))
		})
	}
}

// Revisione 129, B3: ogni campo ha un testo, anche quelli senza testo suo (ripiego), mai un messaggio muto.
func TestTestoCampoRemotoMaiMuto(t *testing.T) {
	for _, campo := range []string{backup.CampoHost, backup.CampoPorta, backup.CampoUtente, backup.CampoCartella,
		backup.CampoTipo, backup.CampoImpronta, "inventato"} {
		if testo, c := testoErroreRemoto(&backup.ErrCampoRemoto{Campo: campo}); strings.TrimSpace(testo) == "" || c != campo {
			t.Errorf("%s: %q %q", campo, testo, c)
		}
	}
	if testo, _ := testoErroreRemoto(&backup.ErrCampoRemoto{Campo: "inventato"}); testo != msgCampoRemotoSbagliato {
		t.Errorf("ripiego: %q", testo)
	}
}
