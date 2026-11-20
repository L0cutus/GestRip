package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/assist"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/ratelimit"
	"github.com/L0cutus/gestrip/tools/banco/modellofinto"
)

// La clip del «🎤 Detta a voce», parte server (trascrizione.go, slice 109; RIP-H-44, RIP-H-47: voci parziali, il
// microfono arriva con la 110), come l'upload :dictation e handle_dictation/3 di WorkshopLive dello standalone a
// go-baseline (workshop_assist_test.exs, «with a transcriber configured too»), senza LiveView. Il trascrittore è
// quello finto (tools/banco/modellofinto, /stt) su 127.0.0.1; clip sintetiche (byte inventati, nessuna voce).

const percorsoClip = "/nuova/trascrivi"

// clipWeb sono n byte inventati.
func clipWeb(n int) []byte { return bytes.Repeat([]byte{0x2b}, n) }

// nuovaClip è la faccia del dettato (titolare su /nuova); con configura la trascrizione salvata punta al
// trascrittore finto, con un glossario.
func nuovaClip(t *testing.T, configura bool) *dettatoProva {
	t.Helper()
	p := nuovoDettato(t, false)
	if configura {
		b := titolareNellAssistente(t, p.h)
		if r := b.vai("POST", "/settings/assistente/trascrizione", moduloAssistente(b, "stt", "endpoint", p.url, "glossary", "Poloni\nBottari"), nil); r.codice != 302 {
			t.Fatalf("configurazione %d %q", r.codice, r.corpo)
		}
	}
	return p
}

// mandaClip manda la clip come la pagina della 110: il token in testa, poi il file del campo dictation.
func mandaClip(b *browser, dati []byte, tipo string) risposta {
	b.t.Helper()
	corpo, ct := corpoMultipart(b.t, [][2]string{{"_csrf_token", b.token}}, []fileProva{{campo: "dictation", nome: "blob", tipo: tipo, dati: dati}}, nil)
	return b.inviaCorpo(percorsoClip, ct, bytes.NewReader(corpo), nil)
}

// esitoDi legge la risposta JSON di POST /nuova/trascrivi (200, mai in cache).
func esitoDi(t *testing.T, r risposta) esitoClip {
	t.Helper()
	if r.codice != 200 || r.h.Get("Content-Type") != "application/json; charset=utf-8" || r.h.Get("Cache-Control") != "no-store" {
		t.Fatalf("risposta %d %q %q %q", r.codice, r.h.Get("Content-Type"), r.h.Get("Cache-Control"), r.corpo)
	}
	var e esitoClip
	if err := json.Unmarshal([]byte(r.corpo), &e); err != nil {
		t.Fatalf("%q: %v", r.corpo, err)
	}
	return e
}

// Senza trascrizione configurata ogni clip è «Nessuna trascrizione è configurata…» (Assist.transcribe/3 la guarda
// per prima), senza audit e senza chiedere niente a nessuno.
func TestClipNonConfigurata(t *testing.T) {
	p := nuovaClip(t, false)
	for _, c := range []struct {
		n    int
		tipo string
	}{{100, "text/plain"}, {5000, "audio/webm"}} {
		e := esitoDi(t, mandaClip(p.b, clipWeb(c.n), c.tipo))
		if e != (esitoClip{Tipo: "error", Messaggio: "Nessuna trascrizione è configurata: scrivi a mano e premi Estrai."}) {
			t.Errorf("%+v", e)
		}
	}
	if righeAudit(t, p.d, "assist.transcribe") != 0 || len(p.m.RichiesteSTT()) != 0 {
		t.Error("audit o richieste senza configurazione")
	}
}

// Una clip trascritta: {"tipo":"ok"} con «✓ Trascritto…» e il testo senza spazi ai bordi, per il titolare e per il
// dipendente (chi crea le buste). Al trascrittore va la clip intera e nuda con il suo tipo e il glossario; la riga
// assist.transcribe conta i byte. Nessun file scritto (nemmeno temporaneo), il testo mai nel registro.
func TestClipTrascritta(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	p := nuovaClip(t, true)
	dip := dipendenteDentro(t, p.h)
	dip.vai("GET", "/nuova", nil, nil)
	for _, b := range []*browser{p.b, dip} {
		p.m.ProgrammaSTT(modellofinto.Buona("  Busta 12, anello da stringere \n"))
		e := esitoDi(t, mandaClip(b, clipWeb(4321), "audio/webm;codecs=opus"))
		if e != (esitoClip{Tipo: "ok", Messaggio: "✓ Trascritto. Controlla e premi «✦ Estrai con AI».", Testo: "Busta 12, anello da stringere"}) {
			t.Errorf("%+v", e)
		}
	}
	r := p.m.RichiesteSTT()
	if len(r) != 2 || r[1] != (modellofinto.RichiestaSTT{ContentType: "audio/webm", Accept: "application/json", Glossario: "Poloni, Bottari", ConGlossario: true, Byte: 4321}) {
		t.Errorf("richieste %+v", r)
	}
	if g := ultimoAuditWeb(t, p.d, "assist.transcribe"); g != `stt|ok|{"bytes":4321,"no_speech":false}` {
		t.Errorf("audit %q", g)
	}
	if righeAudit(t, p.d, "assist.transcribe") != 2 {
		t.Error("righe di audit")
	}
	if strings.Contains(p.reg.String(), "anello") {
		t.Errorf("registro %q", p.reg.String())
	}
	if voci, err := os.ReadDir(tmp); err != nil || len(voci) != 0 {
		t.Errorf("file temporanei %v %v", voci, err)
	}
}

// I rifiuti dopo la configurazione, con le frasi dello standalone: clip sotto i 2000 byte, formato che non si sa
// inoltrare (anche senza tipo), niente voce, trascrittore che non risponde. Dove la clip non parte non c'è audit.
func TestClipRifiutata(t *testing.T) {
	p := nuovaClip(t, true)
	for _, c := range []struct {
		nome   string
		n      int
		tipo   string
		risp   *modellofinto.Risposta
		esito  esitoClip
		chiede bool
	}{
		{"corta", assist.ClipMinima - 1, "audio/webm", nil, esitoClip{"error", "⚠ Registrazione troppo breve.", ""}, false},
		{"corta e di un altro formato", 10, "image/png", nil, esitoClip{"error", "⚠ Registrazione troppo breve.", ""}, false},
		{"formato", 3000, "video/mp4", nil, esitoClip{"error", "⚠ Questo browser ha registrato in un formato che non so inoltrare.", ""}, false},
		{"senza tipo", 3000, "", nil, esitoClip{"error", "⚠ Questo browser ha registrato in un formato che non so inoltrare.", ""}, false},
		{"niente voce", assist.ClipMinima, "audio/wav", &modellofinto.Risposta{Contenuto: modellofinto.Testo(" \n ")}, esitoClip{"error", "🔇 Non ho sentito nulla, riprova.", ""}, true},
		{"trascrittore giù", 3000, "audio/ogg", &modellofinto.Risposta{Stato: 500}, esitoClip{"error", "⚠ Trascrizione non disponibile, scrivi a mano e premi Estrai.", ""}, true},
	} {
		prima, righe := len(p.m.RichiesteSTT()), righeAudit(t, p.d, "assist.transcribe")
		if c.risp != nil {
			p.m.ProgrammaSTT(*c.risp)
		}
		if e := esitoDi(t, mandaClip(p.b, clipWeb(c.n), c.tipo)); e != c.esito {
			t.Errorf("%s: %+v", c.nome, e)
		}
		chiesto := len(p.m.RichiesteSTT()) > prima
		if chiesto != c.chiede || (righeAudit(t, p.d, "assist.transcribe") > righe) != c.chiede {
			t.Errorf("%s: chiesto %v", c.nome, chiesto)
		}
	}
	if g := ultimoAuditWeb(t, p.d, "assist.transcribe"); g != `stt|error|{"bytes":3000,"reason":"stt:http:500"}` {
		t.Errorf("audit %q", g)
	}
}

// La catena prima della clip: senza token il 403, senza accesso /login, e il trascrittore non sente niente. Un
// corpo senza clip, con un altro campo, con due clip o con altro dopo la clip è il 400; oltre i 25 MiB il 413, a
// 25 MiB esatti la clip passa.
func TestClipCatenaEForma(t *testing.T) {
	p := nuovaClip(t, true)
	corpo, ct := corpoMultipart(t, nil, []fileProva{{campo: "dictation", nome: "blob", tipo: "audio/webm", dati: clipWeb(3000)}}, nil)
	if r := p.b.inviaCorpo(percorsoClip, ct, bytes.NewReader(corpo), nil); r.codice != 403 {
		t.Errorf("senza token: %d", r.codice)
	}
	fuori := nuovoBrowser(t, p.h)
	fuori.vai("GET", "/login", nil, nil)
	if r := mandaClip(fuori, clipWeb(3000), "audio/webm"); !redirectA(r, "/login") {
		t.Errorf("senza accesso: %d %q", r.codice, r.location)
	}
	// Un modulo senza file è una clip vuota: troppo breve.
	if e := esitoDi(t, p.b.vai("POST", percorsoClip, url.Values{"_csrf_token": {p.b.token}}, nil)); e.Messaggio != "⚠ Registrazione troppo breve." {
		t.Errorf("modulo senza file: %+v", e)
	}
	testa := [][2]string{{"_csrf_token", p.b.token}}
	clip := fileProva{campo: "dictation", nome: "blob", tipo: "audio/webm", dati: clipWeb(3000)}
	corpo, ct = corpoMultipart(t, testa, nil, [][2]string{{"x", "y"}})
	if e := esitoDi(t, p.b.inviaCorpo(percorsoClip, ct, bytes.NewReader(corpo), nil)); e.Messaggio != "⚠ Registrazione troppo breve." {
		t.Errorf("multipart senza file: %+v", e)
	}
	for nome, c := range map[string]struct {
		file []fileProva
		coda [][2]string
	}{
		"un altro campo":     {[]fileProva{{campo: "audio", nome: "blob", tipo: "audio/webm", dati: clipWeb(3000)}}, nil},
		"due clip":           {[]fileProva{clip, clip}, nil},
		"un campo in coda":   {[]fileProva{clip}, [][2]string{{"x", "y"}}},
		"un file senza nome": {[]fileProva{{campo: "dictation", nome: "", tipo: "audio/webm", dati: clipWeb(3000)}, clip}, nil},
	} {
		corpo, ct := corpoMultipart(t, testa, c.file, c.coda)
		if r := p.b.inviaCorpo(percorsoClip, ct, bytes.NewReader(corpo), nil); r.codice != 400 {
			t.Errorf("%s: %d", nome, r.codice)
		}
	}
	if len(p.m.RichiesteSTT()) != 0 || righeAudit(t, p.d, "assist.transcribe") != 0 {
		t.Error("una clip rifiutata dalla catena è arrivata al trascrittore")
	}
	if r := mandaClip(p.b, clipWeb(assist.ClipMassima+1), "audio/webm"); r.codice != 413 {
		t.Errorf("oltre 25 MiB: %d", r.codice)
	}
	if len(p.m.RichiesteSTT()) != 0 {
		t.Error("la clip oltre il limite è arrivata al trascrittore")
	}
	if e := esitoDi(t, mandaClip(p.b, clipWeb(assist.ClipMassima), "audio/webm")); e.Tipo != "ok" {
		t.Errorf("25 MiB: %+v", e)
	}
	if r := p.m.RichiesteSTT(); len(r) != 1 || r[0].Byte != assist.ClipMassima {
		t.Errorf("richieste %+v", r)
	}
}

// La sessione si ricontrolla quando la clip è arrivata (get_session_user): «chiudi tutte» mentre arriva porta a
// /login, e la clip non arriva al trascrittore.
func TestClipSessioneRicontrollata(t *testing.T) {
	p := nuovaClip(t, true)
	b := dipendenteDentro(t, p.h)
	b.vai("GET", "/nuova", nil, nil)
	corpo, ct := corpoMultipart(t, [][2]string{{"_csrf_token", b.token}}, []fileProva{{campo: "dictation", nome: "blob", tipo: "audio/webm", dati: clipWeb(200_000)}}, nil)
	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write(corpo[:100_000])
		time.Sleep(300 * time.Millisecond) // la catena (con la sessione) è passata, la clip sta arrivando
		if _, err := p.d.Scrittura.Exec(`UPDATE utenti SET versione_sessione = versione_sessione + 1 WHERE email = 'banco@esempio.it'`); err != nil {
			t.Error(err)
		}
		_, _ = pw.Write(corpo[100_000:])
		_ = pw.Close()
	}()
	if r := b.inviaCorpo(percorsoClip, ct, pr, nil); !redirectA(r, "/login") {
		t.Errorf("%d %q", r.codice, r.location)
	}
	if len(p.m.RichiesteSTT()) != 0 || righeAudit(t, p.d, "assist.transcribe") != 0 {
		t.Error("la clip è arrivata al trascrittore con la sessione chiusa")
	}
}

// Senza la pagina dell'assistente non c'è trascrizione; con il database guasto la pagina d'errore, con una frase
// fissa nel registro senza l'errore.
func TestClipSenzaAssistente(t *testing.T) {
	for _, a := range []Assistente{nil, assistenteGuasto{}} {
		var reg bytes.Buffer
		u := &utentiFinti{attivo: true, ruolo: accounts.Titolare}
		dd := dipendenzeProva(&sondaFinta{pronto: true}, slog.New(slog.NewTextHandler(&reg, nil)), u)
		dd.Assistente = a
		b := nuovoBrowser(t, mustInterna(t, dd))
		if r := b.entra(emailProva, passwordProva); !redirectA(r, "/") {
			t.Fatalf("accesso: %d", r.codice)
		}
		b.vai("GET", "/", nil, nil)
		r := mandaClip(b, clipWeb(3000), "audio/webm")
		if a == nil {
			if e := esitoDi(t, r); e.Messaggio != "Nessuna trascrizione è configurata: scrivi a mano e premi Estrai." {
				t.Errorf("senza assistente: %+v", e)
			}
			continue
		}
		if r.codice != 500 || strings.Contains(reg.String(), chiaveAssistenteProva) || !strings.Contains(reg.String(), "dettatura: trascrizione non riuscita") {
			t.Errorf("guasto: %d %q", r.codice, reg.String())
		}
	}
}

// utentiCheCadono fa fallire DiSessione dalla chiamata numero cadi in poi (0 = mai).
type utentiCheCadono struct {
	Utenti
	mu       sync.Mutex
	chiamate int
	cadi     int
}

func (u *utentiCheCadono) DiSessione(ctx context.Context, id, versione int64) (accounts.Utente, bool, error) {
	u.mu.Lock()
	u.chiamate++
	cade := u.cadi > 0 && u.chiamate >= u.cadi
	u.mu.Unlock()
	if cade {
		return accounts.Utente{}, false, errors.New("database guasto")
	}
	return u.Utenti.DiSessione(ctx, id, versione)
}

// clipSuServer è la faccia di p (stesso database e trascrittore finto, trascrizione configurata) su un server TLS
// vero, con il tempo per pezzo, l'attesa del trascrittore e il ReadTimeout dati: per le scadenze della
// connessione, che il registratore dei test non ha. Dà il titolare su /nuova, gli utenti e il registro.
func clipSuServer(t *testing.T, p *dettatoProva, pezzo, attesa, letturaServer time.Duration) (*browser, *utentiCheCadono, *bytes.Buffer) {
	t.Helper()
	o := clock.NuovoFinto(giornoDettato)
	u := &utentiCheCadono{Utenti: accounts.NuoviUtenti(p.d, o)}
	reg := &bytes.Buffer{}
	dd := dipendenzeProva(&sondaFinta{pronto: true}, slog.New(slog.NewTextHandler(reg, nil)), u)
	dd.Orologio, dd.Limiti = o, ratelimit.Nuovo(o)
	dd.Buste = buste.Nuove(p.d, o)
	a := assist.NuoveImpostazioni(p.d, o)
	a.AttesaTrascrizione = attesa
	dd.Assistente = a
	h, _, err := costruisciInterna(dd, func(f *facciaWeb, _ *http.ServeMux) { f.tempoPezzo = pezzo })
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(h)
	srv.Config.ReadTimeout = letturaServer
	srv.StartTLS()
	t.Cleanup(srv.Close)
	b := browserSu(t, srv)
	if r := b.prova("titolare@esempio.it", pwProva); !redirectA(r, "/") {
		t.Fatalf("accesso: %d", r.codice)
	}
	b.vai("GET", "/nuova", nil, nil)
	return b, u, reg
}

// Revisione della 109 (ALTA): finita la clip, l'attesa del trascrittore è quella di assist (35 s) e non il tempo
// dell'ultimo pezzo (10 s), che altrimenti la connessione fa scadere a metà attesa e cancella la richiesta. In
// proporzione: pezzo 300 ms, attesa 1050 ms; un trascrittore che risponde dopo 800 ms va bene, uno che ne mette
// 1500 è l'attesa scaduta.
func TestClipAttesaOltreIlPezzo(t *testing.T) {
	p := nuovaClip(t, true)
	b, _, _ := clipSuServer(t, p, 300*time.Millisecond, 1050*time.Millisecond, 60*time.Second)
	p.m.ProgrammaSTT(modellofinto.Risposta{RitardoMs: 800, Contenuto: modellofinto.Testo("anello da stringere")})
	if e := esitoDi(t, mandaClip(b, clipWeb(3000), "audio/webm")); e.Tipo != "ok" || e.Testo != "anello da stringere" {
		t.Errorf("800 ms: %+v %s", e, ultimoAuditWeb(t, p.d, "assist.transcribe"))
	}
	p.m.ProgrammaSTT(modellofinto.Risposta{RitardoMs: 1500})
	if e := esitoDi(t, mandaClip(b, clipWeb(3000), "audio/webm")); e.Messaggio != "⚠ Trascrizione non disponibile, scrivi a mano e premi Estrai." {
		t.Errorf("1500 ms: %+v", e)
	}
	if g := ultimoAuditWeb(t, p.d, "assist.transcribe"); g != `stt|error|{"bytes":3000,"reason":"stt:transport::timeout"}` {
		t.Errorf("audit %q", g)
	}
}

// Una clip che arriva lenta, a pezzi, oltre il ReadTimeout del server ma con ogni pezzo nel suo tempo, passa: il
// tempo vale per pezzo (allungaLettura), come per le foto.
func TestClipCaricamentoLento(t *testing.T) {
	p := nuovaClip(t, true)
	b, _, _ := clipSuServer(t, p, 2*time.Second, 2*time.Second, 500*time.Millisecond)
	corpo, ct := corpoMultipart(t, [][2]string{{"_csrf_token", b.token}}, []fileProva{{campo: "dictation", nome: "blob", tipo: "audio/webm", dati: clipWeb(256_000)}}, nil)
	pr, pw := io.Pipe()
	go func() {
		for i := 0; i < len(corpo); i += 64_000 {
			_, _ = pw.Write(corpo[i:min(i+64_000, len(corpo))])
			time.Sleep(250 * time.Millisecond) // in tutto oltre il secondo, ogni pezzo sotto i 2 s
		}
		_ = pw.Close()
	}()
	if e := esitoDi(t, b.inviaCorpo(percorsoClip, ct, pr, nil)); e.Tipo != "ok" {
		t.Errorf("%+v", e)
	}
	if r := p.m.RichiesteSTT(); len(r) != 1 || r[0].Byte != 256_000 {
		t.Errorf("richieste %+v", r)
	}
}

// Revisione della 109 (BASSA): la sessione non riletta quando la clip è arrivata è la pagina d'errore, con una
// frase fissa, e la clip non parte.
func TestClipSessioneNonRiletta(t *testing.T) {
	p := nuovaClip(t, true)
	b, u, reg := clipSuServer(t, p, 2*time.Second, 2*time.Second, 60*time.Second)
	u.mu.Lock()
	u.cadi = u.chiamate + 2 // la prima è della catena, la seconda del gestore
	u.mu.Unlock()
	if r := mandaClip(b, clipWeb(3000), "audio/webm"); r.codice != 500 || !strings.Contains(reg.String(), "sessione non riletta") {
		t.Errorf("%d %q", r.codice, reg.String())
	}
	if len(p.m.RichiesteSTT()) != 0 {
		t.Error("la clip è partita senza la sessione riletta")
	}
}

// Revisione della 109 (MEDIA): una clip alla volta per account e due per istanza; oltre, «⚠ Trascrizione non
// disponibile…» subito, senza audit e senza chiedere niente. Finita la prima, il posto torna libero.
func TestClipInParallelo(t *testing.T) {
	p := nuovaClip(t, true)
	dip := dipendenteDentro(t, p.h)
	dip.vai("GET", "/nuova", nil, nil)
	p.m.ProgrammaSTT(modellofinto.Risposta{RitardoMs: 1500, Contenuto: modellofinto.Testo("prima")})
	fatto := make(chan esitoClip, 1)
	go func() { fatto <- esitoDi(t, mandaClip(p.b, clipWeb(3000), "audio/webm")) }()
	for fine := time.Now().Add(5 * time.Second); len(p.m.RichiesteSTT()) == 0 && time.Now().Before(fine); {
		time.Sleep(5 * time.Millisecond)
	}
	if e := esitoDi(t, mandaClip(p.b, clipWeb(3000), "audio/webm")); e != (esitoClip{Tipo: "error", Messaggio: "⚠ Trascrizione non disponibile, scrivi a mano e premi Estrai."}) {
		t.Errorf("seconda dello stesso account: %+v", e)
	}
	if e := esitoDi(t, mandaClip(dip, clipWeb(3000), "audio/webm")); e.Tipo != "ok" {
		t.Errorf("un altro account: %+v", e)
	}
	if e := <-fatto; e.Testo != "prima" {
		t.Errorf("prima: %+v", e)
	}
	if n := len(p.m.RichiesteSTT()); n != 2 || righeAudit(t, p.d, "assist.transcribe") != 2 {
		t.Errorf("%d richieste", n)
	}
	if e := esitoDi(t, mandaClip(p.b, clipWeb(3000), "audio/webm")); e.Tipo != "ok" {
		t.Errorf("dopo: %+v", e)
	}
}

// Il conto delle clip: due per istanza, una per account, e i posti tornano.
func TestClipInCorso(t *testing.T) {
	var c clipInCorso
	if !c.prendi(1) || c.prendi(1) || !c.prendi(2) || c.prendi(3) {
		t.Fatal("limiti")
	}
	c.lascia(1)
	if c.prendi(2) || !c.prendi(3) {
		t.Fatal("dopo lascia(1)")
	}
	c.lascia(2)
	c.lascia(3)
	if c.tutte != 0 || len(c.perUtente) != 0 || !c.prendi(1) {
		t.Fatalf("%+v", c.perUtente)
	}
}

// leggiClip: i byte come arrivano, a pezzi piccoli e grandi, fino a ClipMassima; oltre è errClipGrande.
func TestLeggiClip(t *testing.T) {
	for _, n := range []int{0, 1, 64 << 10, 64<<10 + 1, 3_000_000} {
		dati := clipWeb(n)
		dati = append(dati[:0:0], dati...)
		for i := range dati {
			dati[i] = byte(i * 7)
		}
		got, err := leggiClip(iotest.OneByteReader(bytes.NewReader(dati[:min(n, 1000)])))
		if err != nil || !bytes.Equal(got, dati[:min(n, 1000)]) {
			t.Errorf("%d a un byte: %v", n, err)
		}
		got, err = leggiClip(bytes.NewReader(dati))
		if err != nil || !bytes.Equal(got, dati) {
			t.Errorf("%d: %d %v", n, len(got), err)
		}
	}
	if _, err := leggiClip(bytes.NewReader(make([]byte, assist.ClipMassima+1))); err != errClipGrande {
		t.Errorf("oltre: %v", err)
	}
	if got, err := leggiClip(iotest.TimeoutReader(bytes.NewReader(make([]byte, 100_000)))); err == nil || len(got) == 0 {
		t.Errorf("errore a metà: %d %v", len(got), err)
	}
}
