package modellofinto

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func chiama(t *testing.T, s *httptest.Server, metodo, percorso, corpo string, intest map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(metodo, s.URL+percorso, strings.NewReader(corpo))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range intest {
		req.Header.Set(k, v)
	}
	res, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }() // letto qui sotto
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(b)
}

func contenutoDi(t *testing.T, corpo string) (string, string) {
	t.Helper()
	var r struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(corpo), &r); err != nil || len(r.Choices) != 1 {
		t.Fatalf("risposta %q: %v", corpo, err)
	}
	return r.Choices[0].Message.Content, r.Model
}

// Coda, predefinita, richieste registrate, errori HTTP e corpo grezzo.
func TestModelloFinto(t *testing.T) {
	f := Nuovo()
	s := httptest.NewServer(f)
	defer s.Close()
	f.Programma(Buona(`{"d": "fede"}`), Risposta{Stato: 500}, Risposta{Grezzo: Testo("non JSON"), Stato: 502},
		Risposta{Modello: Testo("altro")})
	corpo := `{"model": "m", "messages": [{"role": "user", "content": "x"}]}`
	codice, r := chiama(t, s, "POST", "/v1/chat/completions", corpo, map[string]string{"Authorization": "Bearer chiave-finta-di-prova",
		"Content-Type": "application/json", "Accept": "application/json"})
	if c, m := contenutoDi(t, r); codice != 200 || c != `{"d": "fede"}` || m != NomeModello {
		t.Errorf("prima: %d %q", codice, r)
	}
	if codice, r := chiama(t, s, "POST", "/v1/chat/completions", corpo, nil); codice != 500 || !strings.Contains(r, "errore finto") || strings.Contains(r, "choices") {
		t.Errorf("seconda: %d", codice)
	}
	if codice, r := chiama(t, s, "POST", "/v1/chat/completions", "rotto", nil); codice != 502 || r != "non JSON" {
		t.Errorf("terza: %d %q", codice, r)
	}
	if _, r := chiama(t, s, "POST", "/v1/chat/completions", corpo, nil); r != "" {
		if c, m := contenutoDi(t, r); c != SchedaBuona || m != "altro" {
			t.Errorf("quarta: %q", r)
		}
	}
	if _, r := chiama(t, s, "POST", "/v1/chat/completions", corpo, nil); r != "" {
		if c, m := contenutoDi(t, r); c != SchedaBuona || m != NomeModello {
			t.Errorf("predefinita: %q", r)
		}
	}
	rr := f.Richieste()
	if len(rr) != 5 || rr[0].Autorizzazione != "Bearer chiave-finta-di-prova" || !rr[0].ConAutorizz || rr[1].ConAutorizz ||
		rr[0].Corpo["model"] != "m" || rr[0].ContentType != "application/json" || rr[0].Accept != "application/json" || rr[1].Accept != "" || rr[2].Corpo != nil || rr[0].Percorso != "/v1/chat/completions" {
		t.Errorf("richieste %+v", rr)
	}
	if codice, _ := chiama(t, s, "GET", "/v1/chat/completions", "", nil); codice != 404 {
		t.Errorf("GET: %d", codice)
	}
}

// Le rotte di prova del banco.
func TestModelloFintoRotteDiProva(t *testing.T) {
	f := Nuovo()
	s := httptest.NewServer(f)
	defer s.Close()
	if codice, r := chiama(t, s, "GET", "/_prova/salute", "", nil); codice != 200 || r != "ok\n" {
		t.Errorf("salute %d %q", codice, r)
	}
	if codice, r := chiama(t, s, "GET", "/_prova/richieste", "", nil); codice != 200 || !strings.Contains(r, `"totale":0`) || !strings.Contains(r, `"ultima":null`) {
		t.Errorf("richieste vuote %d %q", codice, r)
	}
	if codice, _ := chiama(t, s, "POST", "/_prova/risposte", `{"stato": 500}`, nil); codice != 400 {
		t.Errorf("non un elenco: %d", codice)
	}
	for _, stato := range []string{"99", "199", "600", "1000", "-1"} {
		if codice, _ := chiama(t, s, "POST", "/_prova/risposte", `[{"stato": `+stato+`}]`, nil); codice != 400 {
			t.Errorf("stato %s: %d", stato, codice)
		}
	}
	for _, stato := range []string{"200", "599"} {
		if codice, _ := chiama(t, s, "POST", "/_prova/risposte", `[{"stato": `+stato+`}]`, nil); codice != 200 {
			t.Errorf("stato %s: %d", stato, codice)
		}
	}
	if codice, r := chiama(t, s, "POST", "/_prova/risposte", `[{"stato": 503}, {"contenuto": "{}", "ritardo_ms": 1}]`, nil); codice != 200 || !strings.Contains(r, `"in_coda":2`) {
		t.Errorf("programma %d %q", codice, r)
	}
	if codice, _ := chiama(t, s, "POST", "/v1/chat/completions", `{"model": "m"}`, nil); codice != 503 {
		t.Errorf("programmata 503: %d", codice)
	}
	if _, r := chiama(t, s, "POST", "/v1/chat/completions", `{"model": "m2"}`, nil); r != "" {
		if c, _ := contenutoDi(t, r); c != "{}" {
			t.Errorf("programmata buona: %q", r)
		}
	}
	if _, r := chiama(t, s, "GET", "/_prova/richieste", "", nil); !strings.Contains(r, `"totale":2`) || !strings.Contains(r, `"model":"m2"`) {
		t.Errorf("richieste %q", r)
	}
}

// Lenta: il chiamante che se ne va libera subito il server; Chiudi: la connessione cade.
func TestModelloFintoLentaEChiudi(t *testing.T) {
	f := Nuovo()
	s := httptest.NewServer(f)
	defer s.Close()
	f.Programma(Lenta(time.Minute), Risposta{Chiudi: true}, Lenta(10*time.Millisecond))
	ctx, ferma := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer ferma()
	req, _ := http.NewRequestWithContext(ctx, "POST", s.URL+"/v1/chat/completions", strings.NewReader("{}"))
	inizio := time.Now()
	if _, err := s.Client().Do(req); !errors.Is(err, context.DeadlineExceeded) || time.Since(inizio) > 5*time.Second {
		t.Errorf("lenta: %v", err)
	}
	if _, err := s.Client().Post(s.URL+"/v1/chat/completions", "application/json", strings.NewReader("{}")); err == nil {
		t.Error("chiudi: nessun errore")
	}
	if codice, _ := chiama(t, s, "POST", "/v1/chat/completions", "{}", nil); codice != 200 {
		t.Errorf("lenta poco: %d", codice)
	}
}

// Servi parte, risponde e si ferma con il contesto.
func TestServi(t *testing.T) {
	ctx, ferma := context.WithCancel(context.Background())
	pronto := make(chan string, 1)
	fatto := make(chan error, 1)
	go func() { fatto <- Servi(ctx, Nuovo(), "127.0.0.1:0", func(a string) { pronto <- a }) }()
	var a string
	select {
	case a = <-pronto:
	case err := <-fatto:
		t.Fatalf("non è partito: %v", err)
	case <-time.After(10 * time.Second):
		ferma()
		t.Fatal("non pronto dopo 10 s")
	}
	res, err := http.Get("http://" + a + "/_prova/salute")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close() // solo il codice
	if res.StatusCode != 200 {
		t.Errorf("salute %d", res.StatusCode)
	}
	ferma()
	if err := <-fatto; err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Errorf("fermato con %v", err)
	}
	if err := Servi(context.Background(), Nuovo(), "indirizzo-sbagliato", func(string) {}); err == nil {
		t.Error("indirizzo sbagliato accettato")
	}
}
