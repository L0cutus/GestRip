package web

import (
	"bytes"
	"context"
	"errors"
	"html"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/assist"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/ratelimit"
	"github.com/L0cutus/gestrip/tools/banco/modellofinto"
)

// Il «Dettato AI» della pagina «Nuova riparazione» (dettato.go, slice 108; RIP-H-42, RIP-H-45), come il
// riquadro #assist-card e l'evento assist_extract di WorkshopLive dello standalone a go-baseline
// (workshop_live_test.exs, «assisted intake»), senza LiveView. Il modello è quello finto della 106 su
// 127.0.0.1; clienti, telefoni e dettati inventati. Il giorno è venerdì 13/03/2026 (giornoDettato).

// dettatoProva è la faccia con l'assistente vero e il modello finto; se configura, l'estrazione è salvata.
type dettatoProva struct {
	h   http.Handler
	d   *db.DB
	m   *modellofinto.Server
	reg *bytes.Buffer
	b   *browser     // il titolare, sulla pagina /nuova
	url string       // l'indirizzo del modello finto (anche trascrittore finto, slice 109)
	o   *clock.Finto // l'orologio del negozio (slice 113: la scadenza della cattura)
}

// giornoDettato è venerdì 13/03/2026, 10:00 a Roma: un giorno diverso da quello in cui girano i test, così un «oggi»
// preso dall'orologio vero invece che da quello del negozio si vede.
var giornoDettato = time.Date(2026, 3, 13, 9, 0, 0, 0, time.UTC)

func nuovoDettato(t *testing.T, configura bool) *dettatoProva {
	t.Helper()
	return nuovoDettatoCon(t, configura, nil)
}

// quandoDettato è l'ora dell'orologio finto di nuovoDettatoCon (giornoDettato se zero).
var quandoDettato time.Time

// avvolgiAssistente, se c'è, avvolge l'assistente di nuovoDettatoCon (per far fallire una lettura, slice 110).
var avvolgiAssistente func(Assistente) Assistente

// nuovoDettatoCon è nuovoDettato con le buste avvolte da avvolgi (per far fallire una lettura).
func nuovoDettatoCon(t *testing.T, configura bool, avvolgi func(Buste) Buste) *dettatoProva {
	t.Helper()
	p := &dettatoProva{reg: &bytes.Buffer{}, m: modellofinto.Nuovo()}
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	quando := giornoDettato
	if !quandoDettato.IsZero() {
		quando = quandoDettato
	}
	o := clock.NuovoFinto(quando)
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	inserisciConti(t, d, contiBase)
	dd := dipendenzeProva(&sondaFinta{pronto: true}, slog.New(slog.NewTextHandler(p.reg, &slog.HandlerOptions{Level: slog.LevelDebug})), accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti = o, ratelimit.Nuovo(o)
	dd.Buste = buste.Nuove(d, o)
	if avvolgi != nil {
		dd.Buste = avvolgi(dd.Buste)
	}
	a := assist.NuoveImpostazioni(d, o)
	a.Attesa = 2 * time.Second
	dd.Assistente = a
	if avvolgiAssistente != nil {
		dd.Assistente = avvolgiAssistente(a)
	}
	p.h, p.d, p.o = mustInterna(t, dd), d, o
	s := httptest.NewServer(p.m)
	t.Cleanup(s.Close)
	p.url = s.URL
	p.b = titolareDentro(t, p.h)
	if configura {
		b := titolareNellAssistente(t, p.h)
		if r := b.vai("POST", "/settings/assistente/estrazione", moduloAssistente(b, "ai", "endpoint", s.URL, "model", "gestrip-prova"), nil); r.codice != 302 {
			t.Fatalf("configurazione %d", r.codice)
		}
	}
	if r := p.b.vai("GET", "/nuova", nil, nil); r.codice != 200 {
		t.Fatalf("GET /nuova %d", r.codice)
	}
	return p
}

// clienti e oreficerie inventati del negozio.
func (p *dettatoProva) semina(t *testing.T) {
	t.Helper()
	ora := "2026-11-01T09:00:00.000000Z"
	for _, c := range [][2]string{{"Mario Rossi", "3331234567"}, {"Anna Verdi", "3479876543"}, {"Lia Neri", "3201112233"}, {"Lia Neri", "3204445566"},
		{"Ugo Bassi", "333 777 8888"}, {"Ugo Bassi", "+393337778888"}} {
		eseguiSQL(t, p.d, `INSERT INTO clienti (nome, telefono, creato_il, aggiornato_il) VALUES (?, ?, ?, ?)`, c[0], c[1], ora, ora)
	}
	eseguiSQL(t, p.d, `INSERT INTO oreficerie (id, nome) VALUES (7, 'Oreficeria Bottari'), (8, 'Gioielli Zeta')`)
}

// estrai manda «✦ Estrai con AI» con il dettato e, se ci sono, i campi del modulo della busta.
func (p *dettatoProva) estrai(testo string, modulo url.Values) risposta {
	f := url.Values{"_csrf_token": {p.b.token}, "assist[text]": {testo}}
	for k, v := range modulo {
		if k != "_csrf_token" {
			f[k] = v
		}
	}
	return p.b.vai("POST", "/nuova/estrai", f, nil)
}

var (
	reValoreCampo   = regexp.MustCompile(`name="repair\[([a-z_]+)\]" value="([^"]*)"`)
	reValoreData    = regexp.MustCompile(`name="repair\[([a-z_]+)\]" id="[a-z_]+" value="([^"]*)"`)
	reCasellaModulo = regexp.MustCompile(`(?s)<textarea id="[a-z_]+" name="repair\[([a-z_]+)\]"[^>]*>\n(.*?)</textarea>`)
	reScelta        = regexp.MustCompile(`(?s)<select id="repair_([a-z_]+)" name="repair\[[a-z_]+\]">(.*?)</select>`)
	reSegnata       = regexp.MustCompile(`<option value="([^"]*)" selected>`)
)

// valoriModulo legge dalla pagina i valori dei campi del modulo della busta, come li vede il browser:
// caselle, date, testi, la voce segnata dei menu («» nessuna), la spunta del compro oro.
func valoriModulo(corpo string) map[string]string {
	v := map[string]string{}
	for _, m := range reValoreCampo.FindAllStringSubmatch(corpo, -1) {
		v[m[1]] = html.UnescapeString(m[2])
	}
	for _, m := range reValoreData.FindAllStringSubmatch(corpo, -1) {
		v[m[1]] = html.UnescapeString(m[2])
	}
	for _, m := range reCasellaModulo.FindAllStringSubmatch(corpo, -1) {
		v[m[1]] = html.UnescapeString(m[2])
	}
	for _, m := range reScelta.FindAllStringSubmatch(corpo, -1) {
		v[m[1]] = ""
		if s := reSegnata.FindStringSubmatch(m[2]); s != nil {
			v[m[1]] = s[1]
		}
	}
	v["compro_oro"] = "false"
	if strings.Contains(corpo, `id="repair_compro_oro" name="repair[compro_oro]" value="true" checked>`) {
		v["compro_oro"] = "true"
	}
	return v
}

var reStatoDettato = regexp.MustCompile(`<p id="assist-status" class="([^"]*)">([^<]*)</p>`)

// statoDettato è il messaggio sotto la casella e se è in rosso.
func statoDettato(t *testing.T, corpo string) (string, bool) {
	t.Helper()
	m := reStatoDettato.FindStringSubmatch(corpo)
	if m == nil {
		t.Fatal("manca #assist-status")
	}
	return html.UnescapeString(m[2]), strings.Contains(m[1], "text-error")
}

var reCasellaDettato = regexp.MustCompile(`(?s)<textarea id="assist-text" name="assist\[text\]"[^>]*>\n(.*?)</textarea>`)

func casellaDettato(corpo string) string {
	m := reCasellaDettato.FindStringSubmatch(corpo)
	if m == nil {
		return "(assente)"
	}
	return html.UnescapeString(m[1])
}

func moduloAperto(corpo string) bool {
	return strings.Contains(corpo, `<details id="manual-intake" open>`)
}

// Il riquadro si vede solo con l'estrazione configurata (Assist.configured?(:ai), riletto a ogni pagina): senza,
// «🔒 Dettato AI» e il modulo «Inserimento riparazione» aperto; con, «1 · Incolla il testo dettato» con
// «Descrizione libera», il segnaposto dello standalone e «✦ Estrai con AI» in un modulo verso /nuova/estrai con
// il CSRF, e il modulo «Inserimento manuale senza AI» chiuso. Aprire la pagina non chiede niente al modello.
func TestDettatoRiquadro(t *testing.T) {
	p := nuovoDettato(t, false)
	r := p.b.vai("GET", "/nuova", nil, nil)
	contiene(t, "senza estrazione", r.corpo, `<article id="assist-disabled" class="riquadro">`, "🔒 Dettato AI",
		`<details id="manual-intake" open>`, "<span>Inserimento riparazione</span>")
	nonContiene(t, "senza estrazione", r.corpo, "assist-card", "assist-form", "Estrai con AI", "Inserimento manuale senza AI")

	p = nuovoDettato(t, true)
	r = p.b.vai("GET", "/nuova", nil, nil)
	contiene(t, "con estrazione", r.corpo,
		`<article id="assist-card" class="riquadro">`, "<h2>1 · Incolla il testo dettato</h2>",
		`<form id="assist-form" action="/nuova/estrai" method="post" data-dettato data-invio-unico>`,
		`<input name="_csrf_token" type="hidden" value="`+p.b.token+`">`,
		`<label class="label" for="assist-text">Descrizione libera</label>`,
		`<textarea id="assist-text" name="assist[text]" rows="4" placeholder="Es.: Busta 42, cliente Mario Rossi 3331234567, anello oro con pietra da ridimensionare taglia 14, anche pulizia. Consegna entro fine mese.">`,
		`<button id="assist-estrai" type="submit">✦ Estrai con AI</button>`,
		`<p id="assist-status" class="stato-dettato"></p>`,
		`<details id="manual-intake">`, "<span>Inserimento manuale senza AI</span>")
	if m := regexp.MustCompile(`(?s)<form id="assist-form"[^>]*>(.*?)</form>`).FindStringSubmatch(r.corpo); m == nil ||
		!strings.Contains(m[1], `<input name="_csrf_token" type="hidden" value="`+p.b.token+`">`) {
		t.Error("il modulo del dettato non ha il CSRF")
	}
	nonContiene(t, "con estrazione", r.corpo, "assist-disabled", "🔒 Dettato AI", `<details id="manual-intake" open>`, "Inserimento riparazione")
	if casellaDettato(r.corpo) != "" {
		t.Errorf("casella del dettato non vuota: %q", casellaDettato(r.corpo))
	}
	if n := len(p.m.Richieste()); n != 0 {
		t.Errorf("la pagina ha chiesto %d volte al modello", n)
	}
	// Anche il dipendente lo vede (la pagina è del personale).
	contiene(t, "dipendente", dipendenteDentro(t, p.h).vai("GET", "/nuova", nil, nil).corpo, `<article id="assist-card" class="riquadro">`)
}

// inventario: RIP-H-42
// «✦ Estrai con AI» riempie il modulo dalla proposta (assist_extract → repair_form/1 e assign_customer_pair/3):
// nome dall'apertura riconosciuto fra i clienti, telefono dal dettato (D-104), oreficeria per contenimento,
// data di oggi, consegna calcolata dalla frase «c» (D-18), «Busta N — Acconto N € — x» nelle note (D-20),
// preventivo, compro oro dal dettato; artigiano, note interne e codice QR vuoti come in un modulo nuovo. Il
// modulo si apre, la casella tiene il dettato, sotto «✓ Dati estratti. …». Niente si salva; l'invio e la
// chiave delle foto restano quelli del modulo; l'audit assist.extract ha solo il numero dei caratteri.
func TestDettatoRiempieIlModulo(t *testing.T) {
	p := nuovoDettato(t, true)
	p.semina(t)
	p.m.Programma(modellofinto.Buona(`{"n": "mario rossi", "o": "bottari", "b": 12, "d": "anello da stringere di una misura", "c": "fra 2 settimane", "p": "40", "a": 10, "x": "lucidare anche la fede"}`))
	dettato := "Cliente Mario Rossi 333 123 4567, anello da stringere di una misura, oro vecchio, busta 12 acconto 10 euro, consegna fra due settimane"
	invio, chiave := casuale(), casuale()
	modulo := con(con(con(bustaProva(p.b), "invio", invio), "intake_key", chiave), "qr_avvisato", "1")
	modulo.Set("repair[artisan_id]", "")
	modulo.Set("repair[internal_notes]", "nota scritta prima")
	prima := contaBuste(t, p.d)
	r := p.estrai(dettato, modulo)
	if r.codice != 200 {
		t.Fatalf("estrai %d", r.codice)
	}
	atteso := map[string]string{
		"customer_name": "Mario Rossi", "customer_phone": "3331234567", "oreficeria_id": "7", "artisan_id": "",
		"accepted_on": "2026-03-13", "expected_on": "2026-03-27", "qr_code": "", "description": "anello da stringere di una misura",
		"public_notes": "Busta 12 — Acconto 10 € — lucidare anche la fede", "internal_notes": "", "estimate_euros": "40", "compro_oro": "true",
	}
	if got := valoriModulo(r.corpo); !mappeUguali(got, atteso) {
		t.Errorf("modulo:\n%v\natteso\n%v", got, atteso)
	}
	if s, rosso := statoDettato(t, r.corpo); s != "✓ Dati estratti. Verifica e correggi prima di salvare." || rosso {
		t.Errorf("stato %q rosso=%v", s, rosso)
	}
	if casellaDettato(r.corpo) != dettato || !moduloAperto(r.corpo) {
		t.Errorf("casella %q, aperto %v", casellaDettato(r.corpo), moduloAperto(r.corpo))
	}
	contiene(t, "invio e chiave", r.corpo, `<input name="invio" type="hidden" value="`+invio+`">`, `<input type="hidden" name="intake_key" value="`+chiave+`">`,
		`<button type="submit">Genera busta</button>`)
	nonContiene(t, "avvisi azzerati", r.corpo, `name="qr_avvisato"`, "intake-qr-warning", "Genera busta senza QR")
	if n := contaBuste(t, p.d); n != prima {
		t.Errorf("l'estrazione ha salvato %d buste", n-prima)
	}
	if a := ultimoAuditWeb(t, p.d, "assist.extract"); a != `ai|ok|{"characters":`+strconv.Itoa(len([]rune(dettato)))+`}` {
		t.Errorf("audit %q", a)
	}
	// Al modello è andato il formato corto con il giorno del negozio e il dettato.
	rr := p.m.Richieste()
	if len(rr) != 1 || !strings.Contains(contenutoUtente(rr[0]), "Oggi: venerdì 13/03/2026\nDettato: "+dettato) {
		t.Errorf("richieste al modello: %+v", rr)
	}
	// «Genera busta» con quello che la pagina mostra salva la busta della proposta.
	salva := url.Values{"_csrf_token": {p.b.token}, "invio": {invio}, "intake_key": {chiave}, "repair[compro_oro]": {"true"}, "senza_foto": {"1"}}
	for k, v := range atteso {
		if k != "compro_oro" {
			salva.Set("repair["+k+"]", v)
		}
	}
	salva.Set("repair[qr_code]", "0045/26")
	if r := p.b.vai("POST", "/nuova", salva, nil); r.codice != 302 {
		t.Fatalf("salvataggio %d", r.codice)
	}
	var nome, tel, note, prevista string
	var oref int64
	if err := p.d.Lettura.QueryRow(`SELECT c.nome, c.telefono, b.note_pubbliche, coalesce(b.data_prevista, ''), coalesce(b.oreficeria_id, 0) FROM buste b JOIN clienti c ON c.id = b.cliente_id ORDER BY b.id DESC LIMIT 1`).
		Scan(&nome, &tel, &note, &prevista, &oref); err != nil {
		t.Fatal(err)
	}
	if nome != "Mario Rossi" || tel != "3331234567" || note != atteso["public_notes"] || prevista != "2026-03-27" || oref != 7 {
		t.Errorf("busta salvata: %q %q %q %q %d", nome, tel, note, prevista, oref)
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM clienti WHERE nome = 'Mario Rossi'`); n != 1 {
		t.Errorf("clienti Mario Rossi: %d", n)
	}
}

func mappeUguali(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// contenutoUtente è il messaggio dell'utente di una richiesta al modello finto.
func contenutoUtente(r modellofinto.Richiesta) string {
	mm, _ := r.Corpo["messages"].([]any)
	for _, m := range mm {
		if mappa, ok := m.(map[string]any); ok && mappa["role"] == "user" {
			s, _ := mappa["content"].(string)
			return s
		}
	}
	return ""
}

// inventario: RIP-H-43
// Le regole che correggono il modello, sulla pagina (le tabelle sono in internal/assist): compro oro e nome
// dall'apertura in TestDettatoRiempieIlModulo; qui la coppia nome/telefono con la D-106 (Stefano, 20/11/2026): con due clienti con lo stesso nome e nessun telefono dettato il
// telefono resta vuoto (lo standalone propone quello del primo per nome e id); con un solo cliente con quel
// nome, o con omonimi con lo stesso numero scritto in due modi, si propone il suo. Il riconoscimento del nome con la correzione: «✓ Nome cliente corretto da …».
func TestDettatoOmonimiECorrezione(t *testing.T) {
	p := nuovoDettato(t, true)
	p.semina(t)
	p.m.Programma(modellofinto.Buona(`{"n": "lia neri", "d": "collana da saldare"}`))
	r := p.estrai("Signora Lia Neri, collana da saldare", nil)
	if v := valoriModulo(r.corpo); v["customer_name"] != "Lia Neri" || v["customer_phone"] != "" {
		t.Errorf("omonimi: %q %q", v["customer_name"], v["customer_phone"])
	}
	p.m.Programma(modellofinto.Buona(`{"n": "ugo bassi", "d": "catena da saldare"}`))
	r = p.estrai("Signor Ugo Bassi, catena da saldare", nil)
	if v := valoriModulo(r.corpo); v["customer_name"] != "Ugo Bassi" || v["customer_phone"] != "333 777 8888" {
		t.Errorf("omonimi con lo stesso numero: %q %q", v["customer_name"], v["customer_phone"])
	}
	p.m.Programma(modellofinto.Buona(`{"n": "anna verdi", "d": "orecchini da pulire"}`))
	r = p.estrai("Cliente Anna Verdi, orecchini da pulire", nil)
	if v := valoriModulo(r.corpo); v["customer_name"] != "Anna Verdi" || v["customer_phone"] != "3479876543" {
		t.Errorf("nome unico: %q %q", v["customer_name"], v["customer_phone"])
	}
	p.m.Programma(modellofinto.Buona(`{"n": "mario rosi", "d": "anello"}`))
	r = p.estrai("mario rosi 333 1234567 anello da lucidare", nil)
	if s, _ := statoDettato(t, r.corpo); s != "✓ Nome cliente corretto da «Mario Rosi» a «Mario Rossi». Verifica i dati prima di salvare." {
		t.Errorf("correzione: %q", s)
	}
	if v := valoriModulo(r.corpo); v["customer_name"] != "Mario Rossi" || v["customer_phone"] != "3331234567" {
		t.Errorf("correzione: %q %q", v["customer_name"], v["customer_phone"])
	}
}

// I rifiuti con i messaggi dello standalone (assist_refusal/2), in rosso, e il modulo com'è arrivato: un errore
// non tocca il modulo (valori, avvisi, invio, chiave). Testo corto o lungo non chiede niente al modello; con il
// modulo chiuso resta chiuso, con intake_aperto (app.js) resta aperto; senza i campi del modulo (senza
// JavaScript) torna il modulo nuovo con la data di oggi.
func TestDettatoRifiuti(t *testing.T) {
	p := nuovoDettato(t, true)
	p.semina(t)
	invio, chiave := casuale(), casuale()
	modulo := con(con(con(con(bustaProva(p.b), "invio", invio), "intake_key", chiave), "qr_avvisato", "1"), "forma_avvisata", "45/2026")
	modulo.Set("repair[qr_code]", "")
	modulo.Set("repair[expected_on]", "2026-12-01")
	modulo.Set("repair[oreficeria_id]", "8") // l'oreficeria scelta resta segnata
	atteso := valoriDi(modulo)
	for _, c := range []struct {
		nome, testo string
		risposte    []modellofinto.Risposta
		messaggio   string
		richieste   int
	}{
		{"corto", "  anello  ", nil, "⚠ Testo troppo corto: servono almeno 10 caratteri.", 0},
		{"lungo", strings.Repeat("a", 5001), nil, "⚠ Testo troppo lungo: al massimo 5000 caratteri.", 0},
		{"illeggibile", "anello da lucidare", []modellofinto.Risposta{modellofinto.Buona("niente"), modellofinto.Buona("[]")},
			"⚠ Il modello non ha risposto con una scheda leggibile, riprova.", 2},
		{"500", "anello da lucidare", []modellofinto.Risposta{{Stato: 500}, {Stato: 503}},
			"⚠ AI temporaneamente non disponibile, riprova fra qualche minuto.", 2},
	} {
		prima := len(p.m.Richieste())
		p.m.Programma(c.risposte...)
		r := p.estrai(c.testo, modulo)
		if r.codice != 200 {
			t.Fatalf("%s: %d", c.nome, r.codice)
		}
		if s, rosso := statoDettato(t, r.corpo); s != c.messaggio || !rosso {
			t.Errorf("%s: %q rosso=%v", c.nome, s, rosso)
		}
		if got := valoriModulo(r.corpo); !mappeUguali(got, atteso) {
			t.Errorf("%s: il modulo è cambiato:\n%v\natteso\n%v", c.nome, got, atteso)
		}
		contiene(t, c.nome, r.corpo, `<input name="invio" type="hidden" value="`+invio+`">`, `name="intake_key" value="`+chiave+`"`,
			`<input name="qr_avvisato" type="hidden" value="1">`, `<input name="gemelle_avvisate" type="hidden" value="QR-PROVA">`,
			`<input name="forma_avvisata" type="hidden" value="45/2026">`,
			"intake-qr-warning", "Genera busta senza QR", `<details id="manual-intake">`)
		if casellaDettato(r.corpo) != c.testo {
			t.Errorf("%s: la casella non tiene il dettato", c.nome)
		}
		if n := len(p.m.Richieste()) - prima; n != c.richieste {
			t.Errorf("%s: %d richieste al modello, attese %d", c.nome, n, c.richieste)
		}
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.extract'`); n != 2 {
		t.Errorf("righe assist.extract: %d (solo le due che hanno chiesto)", n)
	}
	if a := ultimoAuditWeb(t, p.d, "assist.extract"); a != `ai|error|{"characters":18,"reason":"ai:http:503"}` {
		t.Errorf("audit %q", a)
	}
	// Gli avvisi delle etichette con la regola di «Genera busta»: il codice simile a un'etichetta ancora nel campo e le
	// buste aperte con lo stesso codice si rimostrano, e allora non l'avviso del QR mancante.
	r0 := p.estrai("corto", con(con(modulo, "repair[qr_code]", "45/2026"), "repair[customer_name]", "Ada Esempio"))
	contiene(t, "forma", r0.corpo, `id="intake-qr-format"`, "«45/2026» non è un'etichetta riconosciuta", `<input name="forma_avvisata" type="hidden" value="45/2026">`)
	nonContiene(t, "forma", r0.corpo, `id="intake-qr-warning"`)
	if r := p.b.vai("POST", "/nuova", con(con(bustaProva(p.b), "repair[qr_code]", "QR-GEMELLA"), "gemelle_avvisate", ""), nil); r.codice != 302 {
		t.Fatalf("busta gemella %d", r.codice)
	}
	r0 = p.estrai("corto", con(con(modulo, "repair[qr_code]", "QR-GEMELLA"), "gemelle_avvisate", "QR-GEMELLA"))
	contiene(t, "gemelle", r0.corpo, `id="intake-qr-twins"`, "dalla busta aperta di Ada Esempio", `<input name="gemelle_avvisate" type="hidden" value="QR-GEMELLA">`)
	nonContiene(t, "gemelle", r0.corpo, `id="intake-qr-warning"`, `id="intake-qr-format"`)
	// Il modulo aperto (intake_aperto da app.js) resta aperto; solo «1» vale.
	r := p.estrai("corto", con(modulo, "intake_aperto", "1"))
	if !moduloAperto(r.corpo) {
		t.Error("il modulo aperto si è chiuso")
	}
	if r := p.estrai("corto", con(modulo, "intake_aperto", "0")); moduloAperto(r.corpo) {
		t.Error("intake_aperto=0 ha aperto il modulo")
	}
	// Solo qr_avvisato=1 è l'avviso già dato.
	if r := p.estrai("corto", con(modulo, "qr_avvisato", "0")); strings.Contains(r.corpo, `name="qr_avvisato"`) || strings.Contains(r.corpo, "intake-qr-warning") {
		t.Error("qr_avvisato=0 vale come avviso dato")
	}
	// «Salva senza foto» già detto (D-117, slice 133) resta nel modulo dopo un rifiuto; senza la risposta, o con un
	// valore che non è «1», non c'è, e il rifiuto non fa mai la domanda delle foto.
	ricordata := `<input name="senza_foto" type="hidden" value="1">`
	if !strings.Contains(modulo.Get("senza_foto"), "1") {
		t.Fatal("il modulo di prova non ha la risposta")
	}
	r = p.estrai("corto", modulo)
	contiene(t, "senza foto ricordato", r.corpo, ricordata)
	nonContiene(t, "senza foto ricordato", r.corpo, `id="intake-no-photos"`)
	for _, v := range []string{"", "0"} {
		m := con(modulo, "senza_foto", v)
		if v == "" {
			m.Del("senza_foto")
		}
		nonContiene(t, "senza foto "+v, p.estrai("corto", m).corpo, ricordata, `id="intake-no-photos"`)
	}
	// Senza JavaScript arriva solo il dettato: il modulo è quello nuovo, con la data di oggi.
	r = p.estrai("corto", nil)
	v := valoriModulo(r.corpo)
	if v["accepted_on"] != "2026-03-13" || v["customer_name"] != "" || moduloAperto(r.corpo) || strings.Contains(r.corpo, `name="qr_avvisato"`) {
		t.Errorf("senza JavaScript: %v aperto=%v", v, moduloAperto(r.corpo))
	}
	if s, _ := statoDettato(t, r.corpo); s != "⚠ Testo troppo corto: servono almeno 10 caratteri." {
		t.Errorf("senza JavaScript: %q", s)
	}
}

// valoriDi sono i valori del modulo come la pagina li rimostra.
func valoriDi(v url.Values) map[string]string {
	out := map[string]string{}
	for _, c := range campiBusta {
		out[c] = v.Get("repair[" + c + "]")
	}
	return out
}

// Estrazione non configurata (tolta dopo aver aperto la pagina, o senza la pagina dell'assistente): il riquadro
// dice «Nessun modello è configurato: …» in rosso, senza chiedere niente e senza audit, e il modulo resta com'è. Anche
// tolta davvero dal database dopo aver aperto la pagina.
func TestDettatoNonConfigurato(t *testing.T) {
	p := nuovoDettato(t, false)
	r := p.estrai("anello da lucidare", bustaProva(p.b))
	if r.codice != 200 {
		t.Fatalf("estrai %d %s", r.codice, r.corpo)
	}
	if s, rosso := statoDettato(t, r.corpo); s != "Nessun modello è configurato: non è stato chiesto niente a nessuno." || !rosso {
		t.Errorf("stato %q rosso=%v", s, rosso)
	}
	// La pagina resta com'era all'apertura con il riquadro (lo standalone decide al mount): «Inserimento manuale senza AI»,
	// chiuso, o aperto se lo era.
	contiene(t, "non configurato", r.corpo, `<article id="assist-card" class="riquadro">`, "<span>Inserimento manuale senza AI</span>", `<details id="manual-intake">`)
	if r := p.estrai("anello da lucidare", con(bustaProva(p.b), "intake_aperto", "1")); !moduloAperto(r.corpo) {
		t.Error("non configurato: il modulo aperto si è chiuso")
	}
	nonContiene(t, "non configurato", r.corpo, "assist-disabled")
	if v := valoriModulo(r.corpo); v["customer_name"] != "Ada Esempio" {
		t.Errorf("modulo %v", v)
	}
	if len(p.m.Richieste()) != 0 || contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.extract'`) != 0 {
		t.Error("ha chiesto o scritto l'audit")
	}
	// Tolta dal database dopo aver aperto la pagina.
	q := nuovoDettato(t, true)
	eseguiSQL(t, q.d, `DELETE FROM impostazioni WHERE chiave = 'assist.ai.model'`)
	if r := q.estrai("anello da lucidare", bustaProva(q.b)); !strings.Contains(r.corpo, "Nessun modello è configurato") ||
		!strings.Contains(r.corpo, "<span>Inserimento manuale senza AI</span>") || moduloAperto(r.corpo) || len(q.m.Richieste()) != 0 {
		t.Error("estrazione tolta dopo l'apertura")
	}
	// Senza la pagina dell'assistente (Dipendenze.Assistente nil): lo stesso rifiuto.
	h, _ := facciaConBuste(t, nil)
	b := titolareDentro(t, h)
	b.vai("GET", "/nuova", nil, nil)
	r = b.vai("POST", "/nuova/estrai", url.Values{"_csrf_token": {b.token}, "assist[text]": {"anello da lucidare"}}, nil)
	if s, _ := statoDettato(t, r.corpo); r.codice != 200 || s != "Nessun modello è configurato: non è stato chiesto niente a nessuno." {
		t.Errorf("senza assistente: %d %q", r.codice, s)
	}
	// Senza CSRF niente.
	if r := p.b.vai("POST", "/nuova/estrai", url.Values{"assist[text]": {"anello da lucidare"}}, nil); r.codice != 403 {
		t.Errorf("senza CSRF: %d", r.codice)
	}
}

// Le righe della casella arrivano al modello come «\n» (LiveView), non «\r\n» del modulo senza JavaScript, e la
// casella le rimostra così.
func TestDettatoConRighe(t *testing.T) {
	p := nuovoDettato(t, true)
	p.m.Programma(modellofinto.Buona(`{"d": "anello"}`))
	r := p.estrai("anello da lucidare\r\nconsegna domani", nil)
	if c := contenutoUtente(p.m.Richieste()[0]); !strings.HasSuffix(c, "Dettato: anello da lucidare\nconsegna domani") {
		t.Errorf("al modello: %q", c)
	}
	if casellaDettato(r.corpo) != "anello da lucidare\nconsegna domani" {
		t.Errorf("casella %q", casellaDettato(r.corpo))
	}
}

// Riservatezza (RIP-H-47 per la pagina): il dettato, i nomi, i telefoni e la risposta non vanno mai nel registro
// del server, né dopo una riuscita né dopo un rifiuto; un guasto del database è la pagina d'errore con una
// frase fissa nel registro.
func TestDettatoPrivacy(t *testing.T) {
	p := nuovoDettato(t, true)
	p.semina(t)
	segreti := []string{"Zelinda", "3339990001", "spilla di Zelinda", "Rossi", "3331234567"}
	p.m.Programma(modellofinto.Buona(`{"n": "Zelinda Quarta", "d": "spilla di Zelinda"}`))
	p.estrai("Cliente Zelinda Quarta 333 999 0001 spilla di Zelinda", nil)
	p.m.Programma(modellofinto.Risposta{Stato: 500, Contenuto: modellofinto.Testo("Zelinda 3339990001")}, modellofinto.Risposta{Chiudi: true})
	p.estrai("Cliente Zelinda Quarta 333 999 0001 spilla di Zelinda", bustaProva(p.b))
	reg := p.reg.String()
	for _, s := range segreti {
		if strings.Contains(reg, s) {
			t.Errorf("il registro contiene %q", s)
		}
	}
	for _, a := range []string{"assist.extract"} {
		dati := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = '`+a+`' AND (dati LIKE '%Zelinda%' OR dati LIKE '%333%')`)
		if dati != 0 {
			t.Errorf("audit con il dettato: %d", dati)
		}
	}

	// Il database guasto: la pagina d'errore, una riga fissa nel registro, mai l'errore.
	var reg2 bytes.Buffer
	u := &utentiFinti{attivo: true, ruolo: accounts.Titolare}
	dd := dipendenzeProva(&sondaFinta{pronto: true}, slog.New(slog.NewTextHandler(&reg2, nil)), u)
	dd.Assistente = assistenteGuasto{soloProva: true}
	h := mustInterna(t, dd)
	b := nuovoBrowser(t, h)
	if r := b.entra(emailProva, passwordProva); !redirectA(r, "/") {
		t.Fatalf("accesso: %d", r.codice)
	}
	b.vai("GET", "/", nil, nil)
	r := b.vai("POST", "/nuova/estrai", url.Values{"_csrf_token": {b.token}, "assist[text]": {"Cliente Zelinda Quarta anello"}}, nil)
	if r.codice != 500 {
		t.Errorf("guasto: %d", r.codice)
	}
	if strings.Contains(reg2.String(), "Zelinda") || strings.Contains(reg2.String(), chiaveAssistenteProva) || !strings.Contains(reg2.String(), "dettato: estrazione non riuscita") {
		t.Errorf("registro del guasto: %s", reg2.String())
	}
	// La configurazione guasta: anche GET /nuova è la pagina d'errore.
	dd.Assistente = assistenteGuasto{}
	b = nuovoBrowser(t, mustInterna(t, dd))
	if r := b.entra(emailProva, passwordProva); !redirectA(r, "/") {
		t.Fatalf("accesso: %d", r.codice)
	}
	if r := b.vai("GET", "/nuova", nil, nil); r.codice != 500 {
		t.Errorf("GET con la configurazione guasta: %d", r.codice)
	}
}

// Il negozio passato all'estrazione è quello del database: tutti i clienti e le sole oreficerie esterne (id > 1).
func TestNegozioDelDettato(t *testing.T) {
	d, err := db.Apri(t.TempDir() + "/gestrip.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC))
	if _, err := d.Migra(t.Context(), o); err != nil {
		t.Fatal(err)
	}
	eseguiSQL(t, d, `INSERT OR IGNORE INTO oreficerie (id, nome) VALUES (1, 'Il negozio')`)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (5, 'Zeta'), (4, 'Alfa')`)
	ora := "2026-11-01T09:00:00.000000Z"
	eseguiSQL(t, d, `INSERT INTO clienti (id, nome, telefono, creato_il, aggiornato_il) VALUES (2, 'Bea', '1', ?, ?), (3, 'Ada', '', ?, ?)`, ora, ora, ora, ora)
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti, dd.Buste = o, ratelimit.Nuovo(o), buste.Nuove(d, o)
	f, err := nuovaFaccia(dd, "interna")
	if err != nil {
		t.Fatal(err)
	}
	n, err := f.negozioDelDettato(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(n.Clienti) != 2 || n.Clienti[0] != (assist.Cliente{ID: 3, Nome: "Ada"}) || n.Clienti[1] != (assist.Cliente{ID: 2, Nome: "Bea", Telefono: "1"}) {
		t.Errorf("clienti %+v", n.Clienti)
	}
	if len(n.Oreficerie) != 2 || n.Oreficerie[0] != (assist.Oreficeria{ID: 4, Nome: "Alfa"}) || n.Oreficerie[1] != (assist.Oreficeria{ID: 5, Nome: "Zeta"}) {
		t.Errorf("oreficerie %+v", n.Oreficerie)
	}
}

// busteSenzaClienti fa fallire la lettura dei clienti del dettato.
type busteSenzaClienti struct{ Buste }

func (busteSenzaClienti) ClientiNoti(context.Context) ([]buste.ClienteNoto, error) {
	return nil, errors.New("database guasto: Zelinda 3339990001")
}

// Clienti non letti: la pagina d'errore, nessuna domanda al modello, una riga fissa nel registro. «Genera busta»
// rifiutata con l'estrazione configurata ridisegna il modulo aperto (era aperto per premerla) e il riquadro vuoto.
func TestDettatoGuastiEModuloAperto(t *testing.T) {
	p := nuovoDettatoCon(t, true, func(b Buste) Buste { return busteSenzaClienti{b} })
	// I rifiuti che non costano vengono prima della lettura dei clienti.
	if r := p.estrai("corto", nil); r.codice != 200 || !strings.Contains(r.corpo, "Testo troppo corto") {
		t.Errorf("testo corto con i clienti guasti: %d", r.codice)
	}
	r := p.estrai("Cliente Zelinda Quarta anello da lucidare", nil)
	if r.codice != 500 || len(p.m.Richieste()) != 0 {
		t.Errorf("clienti non letti: %d, %d richieste", r.codice, len(p.m.Richieste()))
	}
	if reg := p.reg.String(); strings.Contains(reg, "Zelinda") || !strings.Contains(reg, "dettato: clienti e oreficerie non letti") {
		t.Errorf("registro: %s", reg)
	}
	r = p.b.vai("POST", "/nuova", con(con(bustaProva(p.b), "repair[customer_name]", ""), "assist[text]", "anello da lucidare\r\nper sabato"), nil)
	if r.codice != 200 || !moduloAperto(r.corpo) {
		t.Errorf("salvataggio rifiutato: %d aperto=%v", r.codice, moduloAperto(r.corpo))
	}
	// La casella del dettato resta piena (assist_text dello standalone; app.js la copia nel modulo).
	if c := casellaDettato(r.corpo); c != "anello da lucidare\nper sabato" {
		t.Errorf("casella dopo il salvataggio rifiutato: %q", c)
	}
	contiene(t, "salvataggio rifiutato", r.corpo, `<article id="assist-card" class="riquadro">`, `<p id="assist-status" class="stato-dettato"></p>`,
		"<span>Inserimento manuale senza AI</span>")
}

// «Oggi» è il giorno del negozio (Roma), non quello UTC: alle 23:30 UTC del 13/03/2026 a Roma è già sabato 14.
func TestDettatoOggiDelNegozio(t *testing.T) {
	quandoDettato = time.Date(2026, 3, 13, 23, 30, 0, 0, time.UTC)
	t.Cleanup(func() { quandoDettato = time.Time{} })
	p := nuovoDettato(t, true)
	p.m.Programma(modellofinto.Buona(`{"d": "anello", "c": "domani"}`))
	r := p.estrai("anello da lucidare per domani", nil)
	if v := valoriModulo(r.corpo); v["accepted_on"] != "2026-03-14" || v["expected_on"] != "2026-03-15" {
		t.Errorf("date: %q %q", v["accepted_on"], v["expected_on"])
	}
	if c := contenutoUtente(p.m.Richieste()[0]); !strings.HasPrefix(c, "Oggi: sabato 14/03/2026\n") {
		t.Errorf("al modello: %q", c)
	}
}

// Il browser che se ne va (contesto della richiesta annullato) ferma la domanda al modello: niente nuovo tentativo, la
// riga d'audit dice «closed» (non la scadenza dell'attesa).
func TestDettatoBrowserVaVia(t *testing.T) {
	p := nuovoDettato(t, true)
	p.m.Programma(modellofinto.Lenta(10*time.Second), modellofinto.Lenta(10*time.Second))
	ctx, ferma := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer ferma()
	f := url.Values{"_csrf_token": {p.b.token}, "assist[text]": {"anello da lucidare"}}
	req, _ := http.NewRequestWithContext(ctx, "POST", p.b.srv.URL+"/nuova/estrai", strings.NewReader(f.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if r, err := p.b.c.Do(req); err == nil {
		_ = r.Body.Close()
		t.Fatal("la richiesta non è stata annullata")
	}
	fine := time.Now().Add(1500 * time.Millisecond)
	for contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.extract'`) == 0 && time.Now().Before(fine) {
		time.Sleep(20 * time.Millisecond)
	}
	if a := ultimoAuditWeb(t, p.d, "assist.extract"); a != `ai|error|{"characters":18,"reason":"ai:transport::closed"}` {
		t.Errorf("audit %q", a)
	}
	if n := len(p.m.Richieste()); n != 1 {
		t.Errorf("richieste al modello: %d", n)
	}
}
