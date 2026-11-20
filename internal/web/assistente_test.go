package web

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/assist"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/ratelimit"
	"github.com/L0cutus/gestrip/tools/banco/modellofinto"
)

// La pagina «Assistente e dettatura», parte configurazione (assistente.go, slice 107, ADM-11), come AssistLive
// dello standalone a go-baseline (assist_live_test.exs) senza LiveView. Il modello è quello finto della 106 su
// 127.0.0.1; chiavi inventate.

const chiaveAssistenteProva = "chiave-finta-di-prova"

// facciaConAssistente è la faccia interna con la pagina dell'assistente vera sul database; reg, se c'è, riceve il
// registro del server.
func facciaConAssistente(t *testing.T, reg *bytes.Buffer) (http.Handler, *db.DB) {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC))
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	inserisciConti(t, d, contiBase)
	l := nessunRegistro
	if reg != nil {
		l = slog.New(slog.NewTextHandler(reg, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	dd := dipendenzeProva(&sondaFinta{pronto: true}, l, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti = o, ratelimit.Nuovo(o)
	dd.Buste = buste.Nuove(d, o)
	a := assist.NuoveImpostazioni(d, o)
	a.Attesa = 2 * time.Second
	dd.Assistente = a
	return mustInterna(t, dd), d
}

func titolareNellAssistente(t *testing.T, h http.Handler) *browser {
	t.Helper()
	b := titolareDentro(t, h)
	if r := b.vai("GET", paginaAssistentePercorso, nil, nil); r.codice != 200 {
		t.Fatalf("pagina %d", r.codice)
	}
	return b
}

func moduloAssistente(b *browser, famiglia string, kv ...string) url.Values {
	v := url.Values{"_csrf_token": {b.token}}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(famiglia+"["+kv[i]+"]", kv[i+1])
	}
	return v
}

// La pagina vuota: i due riquadri «Non configurato», i campi come core_components (senza valore niente
// attributo value; la chiave sempre value="" con il segnaposto vuoto), i testi dello standalone, niente esito.
func TestAssistentePagina(t *testing.T) {
	h, _ := facciaConAssistente(t, nil)
	b := titolareDentro(t, h)
	r := b.vai("GET", paginaAssistentePercorso, nil, nil)
	if r.codice != 200 {
		t.Fatalf("%d", r.codice)
	}
	contiene(t, "pagina", r.corpo, "<title>Assistente e dettatura", "<h1>Assistente e dettatura</h1>", template.HTMLEscapeString(introAssistente),
		`<h2>Estrazione dal dettato</h2> <span id="ai-state" class="badge">Non configurato</span>`,
		`<h2>Trascrizione del parlato</h2> <span id="stt-state" class="badge">Non configurato</span>`,
		template.HTMLEscapeString(testoEstrazione), "quello che espone `POST /stt`: il clip esce da qui",
		`<form id="ai-form" action="/settings/assistente/estrazione" method="post"`,
		`<label for="ai_endpoint">Indirizzo del server</label> <input type="text" id="ai_endpoint" name="ai[endpoint]" placeholder="http://127.0.0.1:11434">`,
		`<label for="ai_model">Modello</label> <input type="text" id="ai_model" name="ai[model]" placeholder="qwen2.5:7b">`,
		`<label for="ai_api_key">Chiave (se il server la chiede)</label> <input type="password" id="ai_api_key" name="ai[api_key]" value="" autocomplete="new-password" placeholder="">`,
		`<input type="checkbox" name="ai[clear_secret]" value="1"> Cancella la chiave salvata`, "Salva modello",
		`<button id="ai-test" type="submit" class="pulsante-contorno">✦ Prova il modello</button>`,
		`<form id="stt-form" action="/settings/assistente/trascrizione" method="post"`,
		`<input type="text" id="stt_endpoint" name="stt[endpoint]" placeholder="http://127.0.0.1:8900">`,
		`<input type="password" id="stt_api_key" name="stt[api_key]" value="" autocomplete="new-password" placeholder="">`,
		`<input type="checkbox" name="stt[clear_secret]" value="1"> Cancella la chiave salvata`,
		`<label for="stt_glossary">Parole di questo negozio</label> <textarea id="stt_glossary" name="stt[glossary]" rows="10" placeholder="Una per riga: marche, cognomi, fornitori.">`+"\n</textarea>",
		"Una voce per riga, fino a 2000 caratteri una volta unite. Più voci non vuol dire meglio: ogni voce attira a sé anche parole che non sono lei.",
		"Salva trascrizione", `id="settings-tab-assistente" href="/settings/assistente"`)
	for _, id := range []string{"ai-form", "ai-test-form", "stt-form"} {
		if !regexp.MustCompile(`(?s)<form id="` + id + `"[^>]*>\s*<input name="_csrf_token" type="hidden" value="[^"]+">`).MatchString(r.corpo) {
			t.Errorf("%s senza _csrf_token", id)
		}
	}
	nonContiene(t, "pagina", r.corpo, `id="ai-probe"`, "Configurato<", "field-error", "training")
	if !regexp.MustCompile(`<a id="settings-tab-assistente" href="/settings/assistente"[^>]*aria-current="page"`).MatchString(r.corpo) {
		t.Error("la scheda «Assistente» non è quella aperta")
	}
}

// Solo il titolare: il dipendente torna alla home con «Operazione riservata al titolare.» da ogni rotta, senza
// scrivere niente e senza chiedere niente al modello.
func TestAssistenteSoloTitolare(t *testing.T) {
	h, d := facciaConAssistente(t, nil)
	f := modellofinto.Nuovo()
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	tb := titolareNellAssistente(t, h)
	if r := tb.vai("POST", "/settings/assistente/estrazione", moduloAssistente(tb, "ai", "endpoint", s.URL, "model", "gestrip-prova"), nil); r.codice != 302 {
		t.Fatalf("configurazione %d", r.codice)
	}
	prima := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'assist.%'`)
	b := dipendenteDentro(t, h)
	b.vai("GET", "/", nil, nil)
	for _, x := range []struct {
		metodo, percorso string
		form             url.Values
	}{
		{"GET", "/settings/assistente", nil},
		{"POST", "/settings/assistente/estrazione", moduloAssistente(b, "ai", "endpoint", "http://127.0.0.1:1", "model", "x")},
		{"POST", "/settings/assistente/trascrizione", moduloAssistente(b, "stt", "endpoint", "http://127.0.0.1:1")},
		{"POST", "/settings/assistente/prova", moduloAssistente(b, "ai")},
	} {
		r := b.vai(x.metodo, x.percorso, x.form, nil)
		if !redirectA(r, "/") {
			t.Errorf("%s %s: %d %q", x.metodo, x.percorso, r.codice, r.location)
		}
		contiene(t, x.percorso, b.vai("GET", "/", nil, nil).corpo, "Operazione riservata al titolare.")
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'assist.%'`); n != prima {
		t.Errorf("il dipendente ha scritto %d righe di audit", n-prima)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM impostazioni WHERE chiave LIKE 'assist.stt.%' OR (chiave = 'assist.ai.model' AND valore = 'x')`); n != 0 {
		t.Errorf("il dipendente ha salvato %d impostazioni", n)
	}
	if len(f.Richieste()) != 0 {
		t.Error("il dipendente ha fatto la prova")
	}
}

// «Salva modello»: «Impostazione salvata.», «Configurato», i valori salvati, la chiave mai nella pagina ma il
// segnaposto «salvata — …»; senza CSRF niente.
func TestAssistenteSalva(t *testing.T) {
	h, d := facciaConAssistente(t, nil)
	b := titolareNellAssistente(t, h)
	r := b.vai("POST", "/settings/assistente/estrazione", moduloAssistente(b, "ai", "endpoint", "http://127.0.0.1:11434", "model", "qwen2.5:7b",
		"api_key", chiaveAssistenteProva), nil)
	if !redirectA(r, paginaAssistentePercorso) {
		t.Fatalf("%d %q", r.codice, r.location)
	}
	c := b.vai("GET", paginaAssistentePercorso, nil, nil).corpo
	contiene(t, "salvato", c, `<div role="status" class="flash flash-info">Impostazione salvata.</div>`,
		`<span id="ai-state" class="badge badge-ok">Configurato</span>`, `<span id="stt-state" class="badge">Non configurato</span>`,
		`name="ai[endpoint]" value="http://127.0.0.1:11434"`, `name="ai[model]" value="qwen2.5:7b"`,
		`name="ai[api_key]" value="" autocomplete="new-password" placeholder="salvata — lascia vuoto per non cambiarla"`,
		`name="stt[api_key]" value="" autocomplete="new-password" placeholder="">`)
	nonContiene(t, "salvato", c, chiaveAssistenteProva)
	if n := contaRighe(t, d, `SELECT count(*) FROM segreti_provider WHERE nome = 'assist.ai.api_key' AND valore = '`+chiaveAssistenteProva+`'`); n != 1 {
		t.Error("chiave non salvata fra i segreti")
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.provider_configured' AND attore = 'titolare@esempio.it'`); n != 1 {
		t.Errorf("%d righe di audit", n)
	}
	// «Cancella la chiave salvata»: la chiave sparisce, il segnaposto torna vuoto.
	if r := b.vai("POST", "/settings/assistente/estrazione", moduloAssistente(b, "ai", "clear_secret", "1"), nil); !redirectA(r, paginaAssistentePercorso) {
		t.Fatalf("cancella %d", r.codice)
	}
	contiene(t, "cancellata", b.vai("GET", paginaAssistentePercorso, nil, nil).corpo, `name="ai[api_key]" value="" autocomplete="new-password" placeholder="">`)
	if n := contaRighe(t, d, `SELECT count(*) FROM segreti_provider WHERE nome = 'assist.ai.api_key'`); n != 0 {
		t.Error("chiave non cancellata")
	}
	// Il glossario: le righe con «\r\n» arrivano a «\n», salvate nella forma a righe; la pagina le rimostra.
	r = b.vai("POST", "/settings/assistente/trascrizione", moduloAssistente(b, "stt", "endpoint", "http://127.0.0.1:8900", "glossary", "Poloni,\r\n  Gioielli  Bottari\r\n"), nil)
	if !redirectA(r, paginaAssistentePercorso) {
		t.Fatalf("trascrizione %d", r.codice)
	}
	c = b.vai("GET", paginaAssistentePercorso, nil, nil).corpo
	contiene(t, "glossario", c, `<span id="stt-state" class="badge badge-ok">Configurato</span>`, "rows=\"10\" placeholder=\"Una per riga: marche, cognomi, fornitori.\">\nPoloni\nGioielli Bottari</textarea>")
	// Senza token: 403 come le altre forme, niente scritto.
	prima := contaRighe(t, d, `SELECT count(*) FROM eventi_audit`)
	if r := b.vai("POST", "/settings/assistente/estrazione", url.Values{"ai[model]": {"altro"}}, nil); r.codice != 403 {
		t.Errorf("senza CSRF %d", r.codice)
	}
	if contaRighe(t, d, `SELECT count(*) FROM eventi_audit`) != prima {
		t.Error("scritto senza CSRF")
	}
}

// TestAssistenteGlossarioConRighe: la casella del glossario va a «\n» prima del dominio (069c), anche quando il
// salvataggio è rifiutato e il testo torna nella pagina.
func TestAssistenteGlossarioConRighe(t *testing.T) {
	h, _ := facciaConAssistente(t, nil)
	b := titolareNellAssistente(t, h)
	r := b.vai("POST", "/settings/assistente/trascrizione", moduloAssistente(b, "stt", "endpoint", "ftp://x", "glossary", "Poloni\r\nBottari"), nil)
	if r.codice != 200 {
		t.Fatalf("%d", r.codice)
	}
	contiene(t, "rifiuto", r.corpo, "placeholder=\"Una per riga: marche, cognomi, fornitori.\">\nPoloni\nBottari</textarea>")
	nonContiene(t, "rifiuto", r.corpo, "\r")
}

// Un salvataggio rifiutato: la pagina (200) con gli errori sotto i campi e i valori mandati, mai la chiave (né
// quella scritta né quella salvata), l'altro riquadro com'è salvato; niente scritto.
func TestAssistenteRifiuto(t *testing.T) {
	h, d := facciaConAssistente(t, nil)
	b := titolareNellAssistente(t, h)
	if r := b.vai("POST", "/settings/assistente/trascrizione", moduloAssistente(b, "stt", "endpoint", "http://127.0.0.1:8900", "glossary", "Poloni"), nil); r.codice != 302 {
		t.Fatal(r.codice)
	}
	b.vai("GET", paginaAssistentePercorso, nil, nil) // il messaggio «Impostazione salvata.» si vede qui
	prima := contaRighe(t, d, `SELECT count(*) FROM eventi_audit`)
	r := b.vai("POST", "/settings/assistente/estrazione", moduloAssistente(b, "ai", "endpoint", "http://modelli.example.invalid", "model", "",
		"api_key", chiaveAssistenteProva), nil)
	if r.codice != 200 {
		t.Fatalf("%d", r.codice)
	}
	contiene(t, "rifiuto", r.corpo, `name="ai[endpoint]" value="http://modelli.example.invalid"`,
		`name="ai[model]" placeholder="qwen2.5:7b">`+"\n"+`<p class="field-error">non può essere vuoto</p>`,
		`name="ai[api_key]" value="" autocomplete="new-password" placeholder="">`+"\n"+`<p class="field-error">una chiave non viaggia in chiaro verso un host remoto</p>`,
		`<span id="ai-state" class="badge">Non configurato</span>`, `<span id="stt-state" class="badge badge-ok">Configurato</span>`,
		`name="stt[endpoint]" value="http://127.0.0.1:8900"`, ">\nPoloni</textarea>")
	nonContiene(t, "rifiuto", r.corpo, chiaveAssistenteProva, "Impostazione salvata.")
	if contaRighe(t, d, `SELECT count(*) FROM eventi_audit`) != prima || contaRighe(t, d, `SELECT count(*) FROM impostazioni WHERE chiave LIKE 'assist.ai.%'`) != 0 {
		t.Error("scritto da un rifiuto")
	}
	// La trascrizione rifiutata lascia l'estrazione com'è e mostra il glossario mandato, non quello salvato.
	r = b.vai("POST", "/settings/assistente/trascrizione", moduloAssistente(b, "stt", "endpoint", "", "glossary", "Rossi"), nil)
	// Il riquadro dell'estrazione resta quello salvato anche quando è la trascrizione a essere rifiutata.
	if r := b.vai("POST", "/settings/assistente/estrazione", moduloAssistente(b, "ai", "endpoint", "http://127.0.0.1:11434", "model", "qwen2.5:7b"), nil); r.codice != 302 {
		t.Fatal(r.codice)
	}
	b.vai("GET", paginaAssistentePercorso, nil, nil)
	r = b.vai("POST", "/settings/assistente/trascrizione", moduloAssistente(b, "stt", "endpoint", "", "glossary", "Rossi"), nil)
	contiene(t, "estrazione dopo il rifiuto stt", r.corpo, `name="ai[endpoint]" value="http://127.0.0.1:11434"`, `name="ai[model]" value="qwen2.5:7b"`,
		`<span id="ai-state" class="badge badge-ok">Configurato</span>`)
	b.vai("GET", paginaAssistentePercorso, nil, nil)
	if _, err := d.Scrittura.Exec(`DELETE FROM impostazioni WHERE chiave LIKE 'assist.ai.%'`); err != nil {
		t.Fatal(err)
	}
	r = b.vai("POST", "/settings/assistente/trascrizione", moduloAssistente(b, "stt", "endpoint", "", "glossary", "Rossi"), nil)
	contiene(t, "rifiuto stt", r.corpo, `name="stt[endpoint]" placeholder="http://127.0.0.1:8900">`+"\n"+`<p class="field-error">non può essere vuoto</p>`,
		">\nRossi</textarea>", `name="ai[model]" placeholder="qwen2.5:7b">`+"\n</div>")
}

// «✦ Prova il modello»: l'esito sotto il pulsante, come lo standalone: la descrizione (al più 80 caratteri), il
// motivo di un rifiuto, la risposta illeggibile, il modello non configurato; mai la chiave nel registro del server.
func TestAssistenteProva(t *testing.T) {
	var reg bytes.Buffer
	h, d := facciaConAssistente(t, &reg)
	b := titolareNellAssistente(t, h)
	prova := func() risposta {
		t.Helper()
		r := b.vai("POST", "/settings/assistente/prova", moduloAssistente(b, "ai"), nil)
		if r.codice != 200 {
			t.Fatalf("prova %d", r.codice)
		}
		return r
	}
	contiene(t, "non configurato", prova().corpo, `<p id="ai-probe" class="assistente-prova text-error">Il modello non è configurato: non è stato chiesto niente.</p>`)
	f := modellofinto.Nuovo()
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	if r := b.vai("POST", "/settings/assistente/estrazione", moduloAssistente(b, "ai", "endpoint", s.URL, "model", "gestrip-prova", "api_key", chiaveAssistenteProva), nil); r.codice != 302 {
		t.Fatal(r.codice)
	}
	b.vai("GET", paginaAssistentePercorso, nil, nil)
	r := prova()
	contiene(t, "buona", r.corpo, `<p id="ai-probe" class="assistente-prova">Il modello ha risposto: «anello da lucidare».</p>`,
		`<span id="ai-state" class="badge badge-ok">Configurato</span>`)
	nonContiene(t, "buona", r.corpo, "Impostazione salvata.")
	lunga := strings.Repeat("è", 79) + "🇮🇹" + "coda"
	f.Programma(modellofinto.Buona(`{"d": "` + lunga + `"}`))
	contiene(t, "lunga", prova().corpo, "Il modello ha risposto: «"+strings.Repeat("è", 79)+"🇮🇹».")
	f.Programma(modellofinto.Risposta{Stato: 500}, modellofinto.Risposta{Stato: 500})
	contiene(t, "500", prova().corpo, `<p id="ai-probe" class="assistente-prova text-error">Il modello ha rifiutato: ai:http:500.</p>`)
	f.Programma(modellofinto.Buona("niente"), modellofinto.Buona("niente"))
	contiene(t, "illeggibile", prova().corpo, `<p id="ai-probe" class="assistente-prova text-error">Il modello ha risposto qualcosa che non è un oggetto JSON.</p>`)
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.extract' AND attore = 'titolare@esempio.it'`); n != 4 {
		t.Errorf("%d righe assist.extract", n)
	}
	// La pagina dopo la prova non tiene l'esito.
	nonContiene(t, "dopo", b.vai("GET", paginaAssistentePercorso, nil, nil).corpo, `id="ai-probe"`)
	for _, x := range []string{chiaveAssistenteProva, s.URL, "anello"} {
		if strings.Contains(reg.String(), x) {
			t.Errorf("il registro contiene %q", x)
		}
	}
}

// assistenteGuasto è la pagina dell'assistente con il database guasto; con soloProva solo la prova fallisce.
type assistenteGuasto struct{ soloProva bool }

var errGuasto = errors.New("database guasto: " + chiaveAssistenteProva)

func (a assistenteGuasto) Pagina(context.Context) (assist.PaginaAssistente, error) {
	if a.soloProva {
		return assist.PaginaAssistente{}, nil
	}
	return assist.PaginaAssistente{}, errGuasto
}
func (assistenteGuasto) Salva(context.Context, string, assist.Modulo, audit.Contesto) (*assist.Rifiuto, error) {
	return nil, errGuasto
}
func (assistenteGuasto) Prova(context.Context, audit.Contesto) (assist.Estrazione, error) {
	return assist.Estrazione{}, errGuasto
}
func (a assistenteGuasto) EstrazioneConfigurata(context.Context) (bool, error) {
	if a.soloProva {
		return true, nil
	}
	return false, errGuasto
}
func (assistenteGuasto) EstraiDettato(context.Context, string, func(context.Context) (assist.Negozio, error), audit.Contesto) (assist.Estrazione, error) {
	return assist.Estrazione{}, errGuasto
}
func (assistenteGuasto) Trascrivi(context.Context, []byte, string, audit.Contesto) (assist.Trascrizione, error) {
	return assist.Trascrizione{}, errGuasto
}

// Senza la pagina dell'assistente (Dipendenze.Assistente nil) ogni rotta è la pagina d'errore; con il database
// guasto pure, con una riga nel registro senza l'errore (che potrebbe portare un valore).
func TestAssistenteSenzaDipendenza(t *testing.T) {
	for _, a := range []Assistente{nil, assistenteGuasto{}, assistenteGuasto{soloProva: true}} {
		var reg bytes.Buffer
		u := &utentiFinti{attivo: true, ruolo: accounts.Titolare}
		dd := dipendenzeProva(&sondaFinta{pronto: true}, slog.New(slog.NewTextHandler(&reg, nil)), u)
		dd.Assistente = a
		h := mustInterna(t, dd)
		b := nuovoBrowser(t, h)
		if r := b.entra(emailProva, passwordProva); !redirectA(r, "/") {
			t.Fatalf("accesso: %d", r.codice)
		}
		b.vai("GET", "/", nil, nil)
		for _, x := range []struct{ metodo, percorso string }{{"GET", paginaAssistentePercorso}, {"POST", "/settings/assistente/estrazione"},
			{"POST", "/settings/assistente/trascrizione"}, {"POST", "/settings/assistente/prova"}} {
			if g, ok := a.(assistenteGuasto); ok && g.soloProva && x.percorso != "/settings/assistente/prova" {
				continue
			}
			if r := b.vai(x.metodo, x.percorso, moduloAssistente(b, "ai", "endpoint", "http://127.0.0.1:1", "model", "m"), nil); r.codice != 500 {
				t.Errorf("%T %s %s: %d", a, x.metodo, x.percorso, r.codice)
			}
		}
		if strings.Contains(reg.String(), chiaveAssistenteProva) || (a != nil && !strings.Contains(reg.String(), "assistente")) {
			t.Errorf("registro %q", reg.String())
		}
	}
}
