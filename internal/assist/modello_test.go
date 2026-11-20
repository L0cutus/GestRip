package assist

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/tools/banco/modellofinto"
)

// Il collegamento al modello (slice 106; RIP-H-46 parte estrazione, RIP-H-47, RIP-H-45 in parte: voci
// parziali, niente segno di copertura). Con il server finto del modello (tools/banco/modellofinto) e
// un'attesa corta: nessun test aspetta 90 s. Dettati, nomi, telefoni e chiave sono inventati.

const chiaveFinta = "chiave-finta-di-prova"

// Il *audit.Registro vero va bene come Registratore.
var _ Registratore = (*audit.Registro)(nil)

// rigaAudit è una riga che il registratore finto ha ricevuto.
type rigaAudit struct {
	Chi                    audit.Contesto
	Azione, Risorsa, Esito string
	ID                     any
	Dati                   map[string]any
	Err                    error // ctx.Err() del contesto della scrittura: deve essere nil
}

type registroFinto struct {
	mu    sync.Mutex
	righe []rigaAudit
}

func (r *registroFinto) Registra(ctx context.Context, c audit.Contesto, azione, risorsa string, id any, esito string, dati map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.righe = append(r.righe, rigaAudit{c, azione, risorsa, esito, id, dati, ctx.Err()})
	return nil
}

// modelloDiProva è il server finto con un collegamento puntato lì (attesa 2 s, chiave finta).
func modelloDiProva(t *testing.T) (*modellofinto.Server, *Collegamento) {
	t.Helper()
	f := modellofinto.Nuovo()
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	return f, &Collegamento{
		Config: Configurazione{Indirizzo: s.URL, Modello: "gestrip-prova", Chiave: chiaveFinta},
		Attesa: 2 * time.Second,
	}
}

var chi = audit.Contesto{Attore: "titolare@esempio.invalid", IP: "127.0.0.1"}

const dettatoProva = "Cliente Mario Esempio 300 111 2222, anello da lucidare, preventivo 30 euro"

func estraiProva(t *testing.T, m *Collegamento, dettato string) (Estrazione, *registroFinto, error) {
	t.Helper()
	reg := &registroFinto{}
	e, err := m.Estrai(context.Background(), dettato, giorno(t, "2026-10-23"), Negozio{}, reg, chi)
	return e, reg, err
}

// La richiesta: POST <indirizzo>/v1/chat/completions, temperature 0, niente stream, il modello
// configurato, la sola domanda nel formato corto come messaggio dell'utente (D-103), la chiave Bearer.
func TestCollegamentoRichiesta(t *testing.T) {
	f, m := modelloDiProva(t)
	m.Config.Indirizzo = "  " + m.Config.Indirizzo + "// \n"
	m.Config.Chiave = "  " + chiaveFinta + "\t"
	m.Config.Modello = "gestrip-prova " // il nome si manda com'è salvato, come lo standalone
	e, reg, err := estraiProva(t, m, "\n  "+dettatoProva+"   ")
	if err != nil {
		t.Fatal(err)
	}
	if e.Proposta.Descrizione != "anello da lucidare" || e.Proposta.TelefonoCliente != "3001112222" || e.Modello != modellofinto.NomeModello {
		t.Errorf("estrazione %+v", e)
	}
	rr := f.Richieste()
	if len(rr) != 1 {
		t.Fatalf("%d richieste", len(rr))
	}
	r := rr[0]
	if r.Percorso != "/v1/chat/completions" || r.Autorizzazione != "Bearer "+chiaveFinta ||
		r.ContentType != "application/json" || r.Accept != "application/json" {
		t.Errorf("percorso %q, autorizzazione %q", r.Percorso, r.Autorizzazione)
	}
	attesi := map[string]any{
		"model": "gestrip-prova ", "temperature": float64(0), "stream": false,
		"messages": []any{map[string]any{"role": "user", "content": "Oggi: venerdì 23/10/2026\nDettato: " + dettatoProva}},
	}
	if g, a := fmt.Sprint(r.Corpo), fmt.Sprint(attesi); g != a {
		t.Errorf("corpo\n%s\natteso\n%s", g, a)
	}
	// Audit: una riga ok con i soli caratteri del dettato senza spazi ai bordi.
	if len(reg.righe) != 1 {
		t.Fatalf("righe di audit: %+v", reg.righe)
	}
	g := reg.righe[0]
	if g.Chi != chi || g.Azione != "assist.extract" || g.Risorsa != "assist" || g.ID != "ai" || g.Esito != "ok" ||
		fmt.Sprint(g.Dati) != fmt.Sprint(map[string]any{"characters": len([]rune(dettatoProva))}) {
		t.Errorf("riga di audit %+v", g)
	}
}

// Chiedi con istruzioni: prima il messaggio di sistema, poi quello dell'utente (LocalChat).
func TestChiediConIstruzioni(t *testing.T) {
	f, m := modelloDiProva(t)
	c, nome, err := m.Chiedi(context.Background(), "istruzioni di prova", "testo di prova")
	if err != nil || c != modellofinto.SchedaBuona || nome != modellofinto.NomeModello {
		t.Fatalf("%q %q %v", c, nome, err)
	}
	msg := f.Richieste()[0].Corpo["messages"]
	atteso := []any{map[string]any{"role": "system", "content": "istruzioni di prova"},
		map[string]any{"role": "user", "content": "testo di prova"}}
	if fmt.Sprint(msg) != fmt.Sprint(atteso) {
		t.Errorf("messaggi %v", msg)
	}
}

// La chiave come Bearer solo se c'è: vuota o di soli spazi, nessuna intestazione Authorization.
func TestChiaveAssente(t *testing.T) {
	for _, chiave := range []string{"", "   ", "\t\n"} {
		f, m := modelloDiProva(t)
		m.Config.Chiave = chiave
		if _, _, err := estraiProva(t, m, dettatoProva); err != nil {
			t.Fatal(err)
		}
		if r := f.Richieste()[0]; r.ConAutorizz || r.Autorizzazione != "" {
			t.Errorf("chiave %q: intestazione %q", chiave, r.Autorizzazione)
		}
	}
}

// Senza indirizzo o senza modello non si chiede niente a nessuno e non si scrive audit.
func TestModelloNonConfigurato(t *testing.T) {
	for _, c := range []Configurazione{
		{},
		{Indirizzo: "http://127.0.0.1:1", Modello: ""},
		{Indirizzo: "", Modello: "gestrip-prova"},
		{Indirizzo: "  \t", Modello: "gestrip-prova", Chiave: chiaveFinta},
		{Indirizzo: "http://127.0.0.1:1", Modello: " \n "},
	} {
		f, m := modelloDiProva(t)
		m.Config = c
		_, reg, err := estraiProva(t, m, dettatoProva)
		if !errors.Is(err, ErrNonConfigurato) || len(f.Richieste()) != 0 || len(reg.righe) != 0 {
			t.Errorf("%+v: %v, %d richieste, %d righe", c, err, len(f.Richieste()), len(reg.righe))
		}
		if MessaggioDiRifiuto(err) != "Nessun modello è configurato: non è stato chiesto niente a nessuno." {
			t.Errorf("messaggio %q", MessaggioDiRifiuto(err))
		}
	}
	if !(Configurazione{Indirizzo: "x", Modello: "y"}).Usabile() {
		t.Error("indirizzo e modello: usabile")
	}
}

// 10–5000 caratteri (grafemi, come String.length/1) sul dettato senza spazi ai bordi; fuori, niente
// richiesta e niente audit.
func TestLimitiDelDettato(t *testing.T) {
	accento := "e\u0301" // un grafema, due rune
	for _, c := range []struct {
		nome, dettato string
		err           error
		caratteri     int // nella riga di audit, se la richiesta parte
	}{
		{"9", "123456789", ErrTestoCorto, 0},
		{"9 con spazi ai bordi", "  \n123456789  \t", ErrTestoCorto, 0},
		{"vuoto", "", ErrTestoCorto, 0},
		{"10", "1234567890", nil, 10},
		{"10 grafemi con l'accento", strings.Repeat(accento, 10), nil, 10},
		{"9 grafemi con l'accento (18 rune)", strings.Repeat(accento, 9), ErrTestoCorto, 0},
		{"5000", strings.Repeat("a", 5000), nil, 5000},
		{"5000 con spazi ai bordi", "   " + strings.Repeat("a", 5000) + "\n\n", nil, 5000},
		{"5000 grafemi con l'accento", strings.Repeat(accento, 5000), nil, 5000},
		{"5000 «é» (due byte)", strings.Repeat("\u00e9", 5000), nil, 5000},
		{"12 emoji con la pelle (8 byte)", strings.Repeat("\U0001F44D\U0001F3FD", 12), nil, 12},
		{"5001 «é»", strings.Repeat("\u00e9", 5001), ErrTestoLungo, 0},
		{"5001", strings.Repeat("a", 5001), ErrTestoLungo, 0},
	} {
		f, m := modelloDiProva(t)
		_, reg, err := estraiProva(t, m, c.dettato)
		if !errors.Is(err, c.err) {
			t.Errorf("%s: %v, atteso %v", c.nome, err, c.err)
		}
		chiamate := 1
		if c.err != nil {
			chiamate = 0
		}
		if len(f.Richieste()) != chiamate || len(reg.righe) != chiamate {
			t.Errorf("%s: %d richieste, %d righe di audit", c.nome, len(f.Richieste()), len(reg.righe))
		} else if chiamate == 1 && reg.righe[0].Dati["characters"] != c.caratteri {
			t.Errorf("%s: characters %v, attesi %d", c.nome, reg.righe[0].Dati["characters"], c.caratteri)
		}
	}
	if g := MessaggioDiRifiuto(ErrTestoCorto); g != "⚠ Testo troppo corto: servono almeno 10 caratteri." {
		t.Errorf("corto: %q", g)
	}
	if g := MessaggioDiRifiuto(ErrTestoLungo); g != "⚠ Testo troppo lungo: al massimo 5000 caratteri." {
		t.Errorf("lungo: %q", g)
	}
}

// D-19 e domanda F: un solo nuovo tentativo, dopo una risposta illeggibile o dopo un errore (rete,
// HTTP), dentro l'attesa unica. Vale l'esito del secondo.
func TestNuovoTentativo(t *testing.T) {
	buona := modellofinto.Buona(`{"d": "fede da lucidare"}`)
	illeggibile := modellofinto.Buona("non ho capito")
	for _, c := range []struct {
		nome      string
		risposte  []modellofinto.Risposta
		richieste int
		motivo    string // "" = riuscita
	}{
		{"buona", []modellofinto.Risposta{buona, buona}, 1, ""},
		{"errore 500 e poi buona", []modellofinto.Risposta{{Stato: 500}, buona}, 2, ""},
		{"errore 500 due volte", []modellofinto.Risposta{{Stato: 500}, {Stato: 500}, buona}, 2, "ai:http:500"},
		{"illeggibile e poi buona", []modellofinto.Risposta{illeggibile, buona}, 2, ""},
		{"illeggibile due volte", []modellofinto.Risposta{illeggibile, illeggibile, buona}, 2, "unreadable_answer"},
		{"rete giù e poi buona", []modellofinto.Risposta{{Chiudi: true}, buona}, 2, ""},
		{"rete giù due volte", []modellofinto.Risposta{{Chiudi: true}, {Chiudi: true}, buona}, 2, "ai:transport::closed"},
		{"illeggibile e poi 503", []modellofinto.Risposta{illeggibile, {Stato: 503}, buona}, 2, "ai:http:503"},
		{"401 e poi illeggibile", []modellofinto.Risposta{{Stato: 401}, illeggibile, buona}, 2, "unreadable_answer"},
		{"201 non è una risposta", []modellofinto.Risposta{{Stato: 201}, {Stato: 201}}, 2, "ai:http:201"},
		{"redirect non seguito", []modellofinto.Risposta{{Stato: 302}, {Stato: 302}}, 2, "ai:http:302"},
		{"busta non OpenAI", []modellofinto.Risposta{{Grezzo: modellofinto.Testo(`{"risposta": "x"}`)}, {Grezzo: modellofinto.Testo("non JSON")}}, 2, "ai:risposta non riconosciuta"},
		{"content non testo", []modellofinto.Risposta{{Grezzo: modellofinto.Testo(`{"choices": [{"message": {"content": 5}}]}`)},
			{Grezzo: modellofinto.Testo(`{"choices": []}`)}}, 2, "ai:risposta non riconosciuta"},
		{"content non testo due volte", []modellofinto.Risposta{{Grezzo: modellofinto.Testo(`{"choices": [{"message": {"content": 5}}]}`)},
			{Grezzo: modellofinto.Testo(`{"choices": [{"message": {}}]}`)}}, 2, "ai:risposta non riconosciuta"},
	} {
		f, m := modelloDiProva(t)
		f.Programma(c.risposte...)
		e, reg, err := estraiProva(t, m, dettatoProva)
		if n := len(f.Richieste()); n != c.richieste {
			t.Errorf("%s: %d richieste, attese %d", c.nome, n, c.richieste)
		}
		if len(reg.righe) != 1 {
			t.Errorf("%s: %d righe di audit", c.nome, len(reg.righe))
			continue
		}
		dati := reg.righe[0].Dati
		if c.motivo == "" {
			if err != nil || e.Proposta.Descrizione != "fede da lucidare" || reg.righe[0].Esito != "ok" || len(dati) != 1 {
				t.Errorf("%s: %+v, %v, audit %+v", c.nome, e, err, reg.righe[0])
			}
			continue
		}
		if err == nil || e != (Estrazione{}) {
			t.Errorf("%s: riuscita %+v", c.nome, e)
		}
		if reg.righe[0].Esito != "error" || dati["reason"] != c.motivo || dati["characters"] != len([]rune(dettatoProva)) || len(dati) != 2 {
			t.Errorf("%s: audit %+v", c.nome, reg.righe[0])
		}
		atteso := "⚠ AI temporaneamente non disponibile, riprova fra qualche minuto."
		if c.motivo == "unreadable_answer" {
			atteso = "⚠ Il modello non ha risposto con una scheda leggibile, riprova."
			if !errors.Is(err, ErrRispostaIllegibile) {
				t.Errorf("%s: %v", c.nome, err)
			}
		} else if em := (ErrModello{}); !errors.As(err, &em) || em.Motivo != c.motivo {
			t.Errorf("%s: %v", c.nome, err)
		}
		if g := MessaggioDiRifiuto(err); g != atteso {
			t.Errorf("%s: messaggio %q", c.nome, g)
		}
	}
}

// L'attesa è una sola: un modello lento non riceve un nuovo tentativo a tempo finito, e un errore
// seguito da un modello lento finisce comunque entro l'attesa.
func TestAttesaUnica(t *testing.T) {
	for _, c := range []struct {
		nome      string
		risposte  []modellofinto.Risposta
		richieste int
	}{
		{"lento", []modellofinto.Risposta{modellofinto.Lenta(20 * time.Second), modellofinto.Buona(`{}`)}, 1},
		{"500 e poi lento", []modellofinto.Risposta{{Stato: 500}, modellofinto.Lenta(20 * time.Second), modellofinto.Buona(`{}`)}, 2},
		{"illeggibile e poi lento", []modellofinto.Risposta{modellofinto.Buona("no"), modellofinto.Lenta(20 * time.Second)}, 2},
	} {
		f, m := modelloDiProva(t)
		m.Attesa = 300 * time.Millisecond
		f.Programma(c.risposte...)
		inizio := time.Now()
		_, reg, err := estraiProva(t, m, dettatoProva)
		durata := time.Since(inizio)
		if em := (ErrModello{}); !errors.As(err, &em) || em.Motivo != "ai:transport::timeout" {
			t.Errorf("%s: %v", c.nome, err)
		}
		if durata > 5*time.Second {
			t.Errorf("%s: %v, oltre l'attesa", c.nome, durata)
		}
		if n := len(f.Richieste()); n != c.richieste {
			t.Errorf("%s: %d richieste, attese %d", c.nome, n, c.richieste)
		}
		if len(reg.righe) != 1 || reg.righe[0].Dati["reason"] != "ai:transport::timeout" || reg.righe[0].Err != nil {
			t.Errorf("%s: audit %+v", c.nome, reg.righe)
		}
	}
	// Il nuovo tentativo dopo un errore usa il tempo che resta: lento quanto basta per stare dentro.
	f, m := modelloDiProva(t)
	m.Attesa = 3 * time.Second
	f.Programma(modellofinto.Risposta{Stato: 500}, modellofinto.Risposta{RitardoMs: 100, Contenuto: modellofinto.Testo(`{"d": "fede"}`)})
	if e, _, err := estraiProva(t, m, dettatoProva); err != nil || e.Proposta.Descrizione != "fede" {
		t.Errorf("500 e poi buona un po' lenta: %+v %v", e, err)
	}
	// Senza Attesa vale AttesaModello.
	if (&Collegamento{}).attesa() != 90*time.Second || AttesaModello != 90*time.Second {
		t.Error("attesa predefinita diversa da 90 s")
	}
	// Un chiamante andato via (::closed) o con la scadenza già passata (::timeout): nessuna richiesta,
	// e la riga di audit si scrive lo stesso, con un contesto non finito.
	annullato, ferma := context.WithCancel(context.Background())
	ferma()
	scaduto, ferma2 := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer ferma2()
	for _, c := range []struct {
		ctx    context.Context
		motivo string
	}{{annullato, "ai:transport::closed"}, {scaduto, "ai:transport::timeout"}} {
		f, m = modelloDiProva(t)
		reg := &registroFinto{}
		_, err := m.Estrai(c.ctx, dettatoProva, giorno(t, "2026-10-23"), Negozio{}, reg, chi)
		if em := (ErrModello{}); !errors.As(err, &em) || em.Motivo != c.motivo || len(f.Richieste()) != 0 {
			t.Errorf("%s: %v, %d richieste", c.motivo, err, len(f.Richieste()))
		}
		if len(reg.righe) != 1 || reg.righe[0].Dati["reason"] != c.motivo || reg.righe[0].Err != nil {
			t.Errorf("%s: audit %+v", c.motivo, reg.righe)
		}
	}
}

// Server spento: rete giù, nessun nuovo tentativo riuscito.
func TestServerSpento(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	indirizzo := "http://" + l.Addr().String()
	_ = l.Close() // ora la porta rifiuta
	m := &Collegamento{Config: Configurazione{Indirizzo: indirizzo, Modello: "x"}, Attesa: 2 * time.Second}
	_, reg, err := estraiProva(t, m, dettatoProva)
	if em := (ErrModello{}); !errors.As(err, &em) || em.Motivo != "ai:transport::econnrefused" {
		t.Errorf("%v", err)
	}
	if len(reg.righe) != 1 || reg.righe[0].Dati["reason"] != "ai:transport::econnrefused" {
		t.Errorf("audit %+v", reg.righe)
	}
	// Indirizzo che non si legge: errore senza l'indirizzo.
	m.Config.Indirizzo = "http://[::1"
	_, _, err = estraiProva(t, m, dettatoProva)
	if em := (ErrModello{}); !errors.As(err, &em) || em.Motivo != "ai:transport::error" {
		t.Errorf("indirizzo rotto: %v", err)
	}
}

// Il nome del modello che ha risposto (with_model/2): quello del server se è testo corto e stampabile,
// senza spazi ai bordi; altrimenti quello configurato.
func TestNomeDelModello(t *testing.T) {
	lungo := strings.Repeat("m", 120)
	for _, c := range []struct {
		grezzo, atteso string
	}{
		{`"gestrip-e2b-v3"`, "gestrip-e2b-v3"},
		{`"  qwen 0.8b \n"`, "qwen 0.8b"},
		{`"` + lungo + `"`, lungo},
		{`"` + lungo + `x"`, "gestrip-prova"},
		{`""`, "gestrip-prova"},
		{`"   "`, "gestrip-prova"},
		{`"a\u0007b"`, "gestrip-prova"},
		{"\"a\u200bb\"", "gestrip-prova"},
		{`5`, "gestrip-prova"},
		{`null`, "gestrip-prova"},
	} {
		f, m := modelloDiProva(t)
		f.Programma(modellofinto.Risposta{Grezzo: modellofinto.Testo(`{"model": ` + c.grezzo +
			`, "choices": [{"message": {"content": "{\"d\": \"fede\"}"}}]}`)})
		e, _, err := estraiProva(t, m, dettatoProva)
		if err != nil || e.Modello != c.atteso {
			t.Errorf("%s: %q %v", c.grezzo, e.Modello, err)
		}
	}
	// Senza «model» nella risposta.
	f, m := modelloDiProva(t)
	f.Programma(modellofinto.Risposta{Grezzo: modellofinto.Testo(`{"choices": [{"message": {"content": "{}"}}]}`)})
	if e, _, err := estraiProva(t, m, dettatoProva); err != nil || e.Modello != "gestrip-prova" {
		t.Errorf("senza model: %q %v", e.Modello, err)
	}
}

// Una risposta oltre 1 MiB non si legge.
func TestRispostaTroppoGrande(t *testing.T) {
	f, m := modelloDiProva(t)
	enorme := `{"choices": [{"message": {"content": "{}"}}], "z": "` + strings.Repeat("a", massimoRispostaModello) + `"}`
	f.Programma(modellofinto.Risposta{Grezzo: &enorme}, modellofinto.Risposta{Grezzo: &enorme})
	_, _, err := estraiProva(t, m, dettatoProva)
	if em := (ErrModello{}); !errors.As(err, &em) || em.Motivo != "ai:risposta non riconosciuta" {
		t.Errorf("%v", err)
	}
	// Al limite si legge ancora.
	// Al limite si legge ancora; un byte in più (uno spazio, JSON ancora valido) no. Due volte, per il
	// nuovo tentativo.
	giusta := `{"choices": [{"message": {"content": "{}"}}]}`
	giusta += strings.Repeat(" ", massimoRispostaModello-len(giusta))
	f.Programma(modellofinto.Risposta{Grezzo: &giusta}, modellofinto.Risposta{Grezzo: &giusta})
	if _, _, err := estraiProva(t, m, dettatoProva); err != nil || len(f.Richieste()) != 3 {
		t.Errorf("al limite: %v, %d richieste", err, len(f.Richieste()))
	}
	oltre := giusta + " "
	f.Programma(modellofinto.Risposta{Grezzo: &oltre}, modellofinto.Risposta{Grezzo: &oltre})
	if _, _, err := estraiProva(t, m, dettatoProva); err == nil {
		t.Error("un byte oltre il limite: letta")
	}
}

// Niente redirect: la chiave va solo all'indirizzo configurato.
func TestNienteRedirect(t *testing.T) {
	var altrove int
	dest := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { altrove++ }))
	defer dest.Close()
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dest.URL+"/v1/chat/completions", http.StatusTemporaryRedirect)
	}))
	defer src.Close()
	m := &Collegamento{Config: Configurazione{Indirizzo: src.URL, Modello: "x", Chiave: chiaveFinta}, Attesa: 2 * time.Second}
	_, _, err := estraiProva(t, m, dettatoProva)
	if em := (ErrModello{}); !errors.As(err, &em) || em.Motivo != "ai:http:307" || altrove != 0 {
		t.Errorf("%v, %d richieste altrove", err, altrove)
	}
	if trasportoModello.Proxy != nil {
		t.Error("il proxy dall'ambiente non si usa")
	}
}

// Il genere di un errore di rete, come inspect/1 di hackney: mai il testo.
func TestGenereRete(t *testing.T) {
	op := func(e error) error {
		return &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", e)}
	}
	for _, c := range []struct {
		err    error
		atteso string
	}{
		{context.DeadlineExceeded, ":timeout"},
		{fmt.Errorf("x: %w", os.ErrDeadlineExceeded), ":timeout"},
		{&net.DNSError{Err: "no such host", Name: "modello.invalid", IsNotFound: true}, ":nxdomain"},
		{op(syscall.ECONNREFUSED), ":econnrefused"},
		{op(syscall.ECONNRESET), ":econnreset"},
		{op(syscall.EHOSTUNREACH), ":ehostunreach"},
		{op(syscall.ENETUNREACH), ":enetunreach"},
		{op(syscall.EPERM), ":error"},
		{io.EOF, ":closed"},
		{fmt.Errorf("x: %w", io.ErrUnexpectedEOF), ":closed"},
		{context.Canceled, ":closed"},
		{errors.New("altro"), ":error"},
	} {
		if g := genereRete(c.err); g != c.atteso {
			t.Errorf("%v: %q, atteso %q", c.err, g, c.atteso)
		}
	}
	if MessaggioDiRifiuto(errors.New("altro")) != "" || MessaggioDiRifiuto(nil) != "" {
		t.Error("un errore qualsiasi non è un rifiuto")
	}
}

// Privacy (rischio della riga 106): un dettato con nome e telefono inventati e una chiave finta;
// in ogni modo di fallire (500 con il dettato e la chiave ripetuti nel corpo, attesa scaduta, risposta
// illeggibile che ripete il dettato, rete giù, server spento) né il log catturato, né l'audit, né
// l'errore (anche con %+v e %#v) né il messaggio per il negozio contengono il nome, il telefono, il
// dettato o la chiave.
func TestPrivacyDettatoEChiave(t *testing.T) {
	const (
		nome     = "Gianfilippo Inventatissimo"
		telefono = "300 987 6543"
	)
	dettato := "Cliente " + nome + " " + telefono + ", collana da riparare, consegna venerdì"
	segreti := []string{nome, "Inventatissimo", telefono, "3009876543", "987 6543", chiaveFinta, "collana da riparare"}
	eco := `{"error": "` + dettato + ` Bearer ` + chiaveFinta + `"}`
	ecoIlleggibile := "Non capisco: " + dettato + " " + chiaveFinta

	var bufLog bytes.Buffer
	vecchioSlog := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&bufLog, &slog.HandlerOptions{Level: slog.LevelDebug})))
	vecchioLog := log.Writer()
	log.SetOutput(&bufLog)
	defer func() {
		slog.SetDefault(vecchioSlog)
		log.SetOutput(vecchioLog)
	}()

	for _, c := range []struct {
		nome     string
		risposte []modellofinto.Risposta
		attesa   time.Duration
		spento   bool
	}{
		{"500 con eco", []modellofinto.Risposta{{Stato: 500, Grezzo: &eco}, {Stato: 500, Grezzo: &eco}}, 0, false},
		{"attesa scaduta", []modellofinto.Risposta{modellofinto.Lenta(20 * time.Second)}, 200 * time.Millisecond, false},
		{"illeggibile con eco", []modellofinto.Risposta{modellofinto.Buona(ecoIlleggibile), modellofinto.Buona(ecoIlleggibile)}, 0, false},
		{"busta rotta con eco", []modellofinto.Risposta{{Grezzo: &eco}, {Grezzo: &eco}}, 0, false},
		{"rete giù", []modellofinto.Risposta{{Chiudi: true}, {Chiudi: true}}, 0, false},
		{"server spento", nil, 0, true},
		{"byte oltre il Content-Length", nil, 0, false},
	} {
		f, m := modelloDiProva(t)
		f.Programma(c.risposte...)
		if c.attesa > 0 {
			m.Attesa = c.attesa
		}
		if c.nome == "byte oltre il Content-Length" {
			m.Config.Indirizzo = serverGrezzo(t, func(conn net.Conn) {
				corpo := `{"choices": [{"message": {"content": "{}"}}]}`
				_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: "+
					strconv.Itoa(len(corpo))+"\r\n\r\n"+corpo+"ECO "+dettato+" "+chiaveFinta)
				time.Sleep(300 * time.Millisecond) // il tempo per il lettore di net/http
			})
		}
		if c.spento {
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			m.Config.Indirizzo = "http://" + l.Addr().String()
			_ = l.Close() // porta chiusa
		}
		e, reg, err := estraiProva(t, m, dettato)
		if c.nome == "byte oltre il Content-Length" {
			time.Sleep(500 * time.Millisecond) // il lettore di net/http scriverebbe nel log in questo tempo
		}
		var visto strings.Builder
		fmt.Fprintf(&visto, "%v|%+v|%#v|%s|", err, err, err, MessaggioDiRifiuto(err))
		if err != nil { // riuscita, la proposta porta il nome e il telefono: è il suo scopo
			fmt.Fprintf(&visto, "%+v|", e)
		}
		for err != nil {
			visto.WriteString(err.Error() + "|")
			err = errors.Unwrap(err)
		}
		dati, jerr := json.Marshal(reg.righe)
		if jerr != nil {
			t.Fatal(jerr)
		}
		visto.Write(dati)
		visto.WriteString(bufLog.String())
		for _, s := range segreti {
			if strings.Contains(visto.String(), s) {
				t.Errorf("%s: %q trapela in %q", c.nome, s, visto.String())
			}
		}
		if len(reg.righe) != 1 {
			t.Errorf("%s: %d righe di audit", c.nome, len(reg.righe))
		}
	}
	if bufLog.Len() != 0 {
		t.Errorf("il collegamento non scrive log: %q", bufLog.String())
	}
}

// serverGrezzo è un server TCP che legge una richiesta e poi fa quello che dice scrivi sulla connessione:
// per le risposte che un server HTTP vero non manda.
func serverGrezzo(t *testing.T, scrivi func(c net.Conn)) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() }) // fine del test
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }() // fine della risposta
				req, err := http.ReadRequest(bufio.NewReader(c))
				if err != nil {
					return
				}
				_, _ = io.Copy(io.Discard, req.Body) // la richiesta intera, poi la risposta
				scrivi(c)
			}()
		}
	}()
	return "http://" + l.Addr().String()
}

// Una risposta con le intestazioni e poi il corpo a metà: il server si blocca (attesa scaduta) o chiude.
func TestCorpoAMeta(t *testing.T) {
	testa := "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 500\r\n\r\n{\"choices\": ["
	blocca := make(chan struct{})
	defer close(blocca)
	for _, c := range []struct {
		nome, motivo string
		scrivi       func(net.Conn)
	}{
		{"si blocca", "ai:transport::timeout", func(c net.Conn) { _, _ = io.WriteString(c, testa); <-blocca }},
		{"chiude", "ai:transport::closed", func(c net.Conn) { _, _ = io.WriteString(c, testa) }},
	} {
		m := &Collegamento{Config: Configurazione{Indirizzo: serverGrezzo(t, c.scrivi), Modello: "x"}, Attesa: 300 * time.Millisecond}
		_, reg, err := estraiProva(t, m, dettatoProva)
		if em := (ErrModello{}); !errors.As(err, &em) || em.Motivo != c.motivo {
			t.Errorf("%s: %v", c.nome, err)
		}
		if len(reg.righe) != 1 || reg.righe[0].Dati["reason"] != c.motivo {
			t.Errorf("%s: audit %+v", c.nome, reg.righe)
		}
	}
}

// Le chiavi della risposta contano con le loro maiuscole, come il pattern di Jason; content e model
// valgono solo se sono testo.
func TestInterpretaModello(t *testing.T) {
	for _, c := range []struct {
		corpo, contenuto, modello string
		ok                        bool
	}{
		{`{"choices": [{"message": {"content": "x"}}], "model": "m"}`, "x", "m", true},
		{`{"choices": [{"message": {"content": ""}}, {"message": 5}]}`, "", "", true},
		{`{"Choices": [{"message": {"content": "x"}}]}`, "", "", false},
		{`{"choices": [{"Message": {"content": "x"}}]}`, "", "", false},
		{`{"choices": [{"message": {"Content": "x"}}]}`, "", "", false},
		{`{"choices": [{"message": {"content": "x"}}], "Model": "m"}`, "x", "", true},
		{`{"choices": [{"message": {"content": "x"}}], "model": 5}`, "x", "", true},
		{`{"choices": [{"message": {"content": null}}]}`, "", "", false},
		{`{"choices": [{"message": {"content": ["x"]}}]}`, "", "", false},
		{`{"choices": [{"message": null}]}`, "", "", false},
		{`{"choices": [null]}`, "", "", false},
		{`{"choices": [5]}`, "", "", false},
		{`{"choices": null}`, "", "", false},
		{`{"choices": {}}`, "", "", false},
		{`{}`, "", "", false},
		{`null`, "", "", false},
		{`[]`, "", "", false},
		{`"x"`, "", "", false},
	} {
		g, mod, ok := interpretaModello([]byte(c.corpo))
		if g != c.contenuto || mod != c.modello || ok != c.ok {
			t.Errorf("%s: %q %q %v", c.corpo, g, mod, ok)
		}
	}
}
