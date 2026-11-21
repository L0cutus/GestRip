// Package modellofinto è il server finto del modello di estrazione (slice 106, come il bot finto della
// 074b): risponde su POST /v1/chat/completions come un server compatibile OpenAI, con risposte
// programmabili (buona, errore HTTP, lenta, illeggibile, connessione chiusa). Dalla slice 109 è anche il
// trascrittore finto: POST /stt come il trascrittore della bottega (la clip nel corpo, il glossario in
// X-Gestrip-Glossario, la risposta {"text": …}), con una coda di risposte sua. Lo usano i test Go
// (internal/assist e, dalle slice 107-108, le pagine) con httptest, e il banco come
// `banco modello-finto -ascolta :8082` in un container della rete interna. Nessuna rete esterna, nessun
// modello vero, solo dati inventati.
//
// Ogni richiesta prende la prima risposta della sua coda; a coda vuota quella predefinita (una scheda
// buona, o TestoTrascritto). Le richieste ricevute si tengono in memoria (intestazione Authorization e
// corpo; della clip solo quanti byte erano e, per un WAV, formato e tono, mai i byte) per i controlli dei test; il
// server non scrive log.
//
// Rotte di prova, per il banco (slice 107-108):
//   - POST /_prova/risposte [{"stato": 500}, {"contenuto": "…"}, …]: sostituisce la coda;
//   - GET /_prova/richieste: {"totale": N, "ultima": {"autorizzazione": …, "corpo": …} o null};
//   - POST /_prova/risposte-stt [{"contenuto": "…"}, {"stato": 500}, …]: la coda di /stt (contenuto è il
//     testo trascritto);
//   - GET /_prova/richieste-stt: {"totale": N, "ultima": {…} o null}, le richieste a /stt;
//   - GET /_prova/salute: «ok».
package modellofinto

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// NomeModello è il nome che il server finto dà nel campo «model» della risposta, se la risposta non ne
// porta un altro.
const NomeModello = "modello-finto"

// SchedaBuona è la risposta predefinita: una scheda nel formato corto (D-103) con dati inventati.
const SchedaBuona = `{"n": "Mario Esempio", "d": "anello da lucidare", "p": "30"}`

// TestoTrascritto è il testo predefinito di /stt: una frase inventata.
const TestoTrascritto = "busta di prova, anello da lucidare"

// massimaClip è quanto si legge di una clip: il limite di GestRip (25 MiB) e un po'.
const massimaClip = 26 << 20

// Risposta è una risposta programmata.
type Risposta struct {
	Stato     int     `json:"stato,omitempty"`      // codice HTTP; 0 = 200
	Contenuto *string `json:"contenuto,omitempty"`  // content del messaggio; nil = SchedaBuona
	Modello   *string `json:"modello,omitempty"`    // campo «model»; nil = NomeModello
	Grezzo    *string `json:"grezzo,omitempty"`     // corpo intero al posto della risposta OpenAI
	RitardoMs int64   `json:"ritardo_ms,omitempty"` // attesa prima di rispondere, in ms (finisce se il chiamante se ne va)
	Chiudi    bool    `json:"chiudi,omitempty"`     // chiude la connessione senza rispondere (rete giù)
}

// Testo è un aiuto per Contenuto, Modello e Grezzo.
func Testo(s string) *string { return &s }

// Buona è una risposta 200 con il contenuto dato.
func Buona(contenuto string) Risposta { return Risposta{Contenuto: Testo(contenuto)} }

// Lenta è la scheda buona dopo d.
func Lenta(d time.Duration) Risposta { return Risposta{RitardoMs: d.Milliseconds()} }

// Richiesta è una richiesta ricevuta.
type Richiesta struct {
	Percorso       string         `json:"percorso"`
	Autorizzazione string         `json:"autorizzazione"`  // l'intestazione Authorization, vuota se manca
	ConAutorizz    bool           `json:"con_autorizz"`    // se l'intestazione c'era
	ContentType    string         `json:"content_type"`    // l'intestazione Content-Type
	Accept         string         `json:"accept"`          // l'intestazione Accept
	Corpo          map[string]any `json:"corpo,omitempty"` // il JSON ricevuto, nil se non lo era
}

// RichiestaSTT è una richiesta ricevuta su /stt: le intestazioni e quanti byte aveva la clip.
type RichiestaSTT struct {
	Autorizzazione string `json:"autorizzazione"`
	ConAutorizz    bool   `json:"con_autorizz"`
	ContentType    string `json:"content_type"`
	Accept         string `json:"accept"`
	Glossario      string `json:"glossario"`     // l'intestazione X-Gestrip-Glossario
	ConGlossario   bool   `json:"con_glossario"` // se l'intestazione c'era (anche vuota)
	Byte           int    `json:"byte"`          // la lunghezza della clip; i byte non si tengono
	Multipart      bool   `json:"multipart"`     // il corpo era un modulo invece della clip così com'è
	// WAV e TonoHz descrivono una clip WAV (DescriviWAV, slice 110): "" e 0 se non lo è.
	WAV    string `json:"wav"`
	TonoHz int    `json:"tono_hz"`
}

// Server è il modello finto: un http.Handler.
type Server struct {
	mu           sync.Mutex
	coda         []Risposta
	richieste    []Richiesta
	codaSTT      []Risposta
	richiesteSTT []RichiestaSTT
}

// Nuovo dà un server finto con la coda vuota.
func Nuovo() *Server { return &Server{} }

// Programma sostituisce la coda delle risposte.
func (s *Server) Programma(r ...Risposta) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.coda = append([]Risposta(nil), r...)
}

// ProgrammaSTT sostituisce la coda delle risposte di /stt.
func (s *Server) ProgrammaSTT(r ...Risposta) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.codaSTT = append([]Risposta(nil), r...)
}

// RichiesteSTT dà le richieste ricevute su /stt, in ordine.
func (s *Server) RichiesteSTT() []RichiestaSTT {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]RichiestaSTT(nil), s.richiesteSTT...)
}

// Richieste dà le richieste ricevute su /v1/chat/completions, in ordine.
func (s *Server) Richieste() []Richiesta {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Richiesta(nil), s.richieste...)
}

// massimoCorpo è quanto si legge di una richiesta: un dettato è al più 5000 caratteri.
const massimoCorpo = 1 << 20

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/_prova/salute":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "ok") // un errore di scrittura non si può dire a nessuno
	case r.Method == http.MethodPost && r.URL.Path == "/_prova/risposte":
		s.programmaDaHTTP(w, r, s.Programma)
	case r.Method == http.MethodPost && r.URL.Path == "/_prova/risposte-stt":
		s.programmaDaHTTP(w, r, s.ProgrammaSTT)
	case r.Method == http.MethodGet && r.URL.Path == "/_prova/richieste-stt":
		s.mu.Lock()
		out := map[string]any{"totale": len(s.richiesteSTT), "ultima": nil}
		if n := len(s.richiesteSTT); n > 0 {
			out["ultima"] = s.richiesteSTT[n-1]
		}
		s.mu.Unlock()
		scriviJSON(w, http.StatusOK, out)
	case r.Method == http.MethodPost && r.URL.Path == "/stt":
		s.trascrivi(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/_prova/richieste":
		s.mu.Lock()
		out := map[string]any{"totale": len(s.richieste), "ultima": nil}
		if n := len(s.richieste); n > 0 {
			out["ultima"] = s.richieste[n-1]
		}
		s.mu.Unlock()
		scriviJSON(w, http.StatusOK, out)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions":
		s.completa(w, r)
	default:
		scriviJSON(w, http.StatusNotFound, map[string]any{"error": "non trovato"})
	}
}

func scriviJSON(w http.ResponseWriter, codice int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(codice)
	_ = json.NewEncoder(w).Encode(v) // la risposta è già partita
}

func (s *Server) programmaDaHTTP(w http.ResponseWriter, r *http.Request, programma func(...Risposta)) {
	var rr []Risposta
	letto, err := io.ReadAll(io.LimitReader(r.Body, massimoCorpo+1))
	if err != nil || len(letto) > massimoCorpo || json.Unmarshal(letto, &rr) != nil {
		scriviJSON(w, http.StatusBadRequest, map[string]any{"error": "atteso un elenco di risposte"})
		return
	}
	for _, x := range rr {
		// WriteHeader va in panic fuori da 100-999: si accetta solo uno stato vero (0 = 200).
		if x.Stato != 0 && (x.Stato < 200 || x.Stato > 599) {
			scriviJSON(w, http.StatusBadRequest, map[string]any{"error": "stato fuori da 200-599"})
			return
		}
	}
	programma(rr...)
	scriviJSON(w, http.StatusOK, map[string]any{"in_coda": len(rr)})
}

// completa è /v1/chat/completions: registra la richiesta e dà la prossima risposta della coda.
func (s *Server) completa(w http.ResponseWriter, r *http.Request) {
	ric := Richiesta{Percorso: r.URL.Path, ContentType: r.Header.Get("Content-Type"), Accept: r.Header.Get("Accept")}
	if a, ok := r.Header["Authorization"]; ok {
		ric.ConAutorizz = true
		if len(a) > 0 {
			ric.Autorizzazione = a[0]
		}
	}
	if letto, err := io.ReadAll(io.LimitReader(r.Body, massimoCorpo)); err == nil {
		var corpo map[string]any
		if json.Unmarshal(letto, &corpo) == nil {
			ric.Corpo = corpo
		}
	}
	s.mu.Lock()
	s.richieste = append(s.richieste, ric)
	var risp Risposta
	if len(s.coda) > 0 {
		risp, s.coda = s.coda[0], s.coda[1:]
	}
	s.mu.Unlock()

	if risp.RitardoMs > 0 && !aspetta(r.Context(), time.Duration(risp.RitardoMs)*time.Millisecond) {
		return // il chiamante non aspetta più
	}
	if risp.Chiudi {
		chiudi(w)
		return
	}
	stato := risp.Stato
	if stato == 0 {
		stato = http.StatusOK
	}
	if risp.Grezzo != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(stato)
		_, _ = io.WriteString(w, *risp.Grezzo) // la risposta è già partita
		return
	}
	if stato != http.StatusOK {
		scriviJSON(w, stato, map[string]any{"error": map[string]any{"message": "errore finto", "code": stato}})
		return
	}
	contenuto, modello := SchedaBuona, NomeModello
	if risp.Contenuto != nil {
		contenuto = *risp.Contenuto
	}
	if risp.Modello != nil {
		modello = *risp.Modello
	}
	scriviJSON(w, stato, map[string]any{
		"id": "finto", "object": "chat.completion", "model": modello,
		"choices": []map[string]any{{"index": 0, "finish_reason": "stop",
			"message": map[string]any{"role": "assistant", "content": contenuto}}},
	})
}

// trascrivi è /stt: registra la richiesta (senza la clip) e dà la prossima risposta della sua coda: {"text":
// contenuto} (TestoTrascritto se nil), un errore, un corpo grezzo, un ritardo o la connessione chiusa.
func (s *Server) trascrivi(w http.ResponseWriter, r *http.Request) {
	ric := RichiestaSTT{ContentType: r.Header.Get("Content-Type"), Accept: r.Header.Get("Accept")}
	if a, ok := r.Header["Authorization"]; ok {
		ric.ConAutorizz = true
		if len(a) > 0 {
			ric.Autorizzazione = a[0]
		}
	}
	if g, ok := r.Header["X-Gestrip-Glossario"]; ok {
		ric.ConGlossario = true
		if len(g) > 0 {
			ric.Glossario = g[0]
		}
	}
	ric.Multipart = strings.HasPrefix(strings.ToLower(ric.ContentType), "multipart/")
	// Contano solo i byte arrivati; della clip restano la lunghezza e, se è un WAV, com'è fatta e il suo tono.
	clip, _ := io.ReadAll(io.LimitReader(r.Body, massimaClip))
	ric.Byte = len(clip)
	ric.WAV, ric.TonoHz = DescriviWAV(clip)
	clear(clip)
	s.mu.Lock()
	s.richiesteSTT = append(s.richiesteSTT, ric)
	var risp Risposta
	if len(s.codaSTT) > 0 {
		risp, s.codaSTT = s.codaSTT[0], s.codaSTT[1:]
	}
	s.mu.Unlock()

	if risp.RitardoMs > 0 && !aspetta(r.Context(), time.Duration(risp.RitardoMs)*time.Millisecond) {
		return
	}
	if risp.Chiudi {
		chiudi(w)
		return
	}
	stato := risp.Stato
	if stato == 0 {
		stato = http.StatusOK
	}
	switch {
	case risp.Grezzo != nil:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(stato)
		_, _ = io.WriteString(w, *risp.Grezzo) // la risposta è già partita
	case stato != http.StatusOK:
		scriviJSON(w, stato, map[string]any{"error": "errore finto"})
	default:
		testo := TestoTrascritto
		if risp.Contenuto != nil {
			testo = *risp.Contenuto
		}
		scriviJSON(w, stato, map[string]any{"text": testo})
	}
}

// aspetta attende d o la fine di ctx; false se ctx è finito prima.
func aspetta(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// chiudi chiude la connessione senza scrivere niente: per il chiamante la rete è caduta.
func chiudi(w http.ResponseWriter) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		panic(http.ErrAbortHandler) // HTTP/2: il server chiude il flusso
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	_ = conn.Close() // la chiusura è lo scopo
}

// Servi ascolta su ascolta finché ctx non finisce; pronto riceve l'indirizzo vero.
func Servi(ctx context.Context, s *Server, ascolta string, pronto func(string)) error {
	l, err := net.Listen("tcp", ascolta)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		IdleTimeout: 2 * time.Minute}
	go func() {
		<-ctx.Done()
		fine, annulla := context.WithTimeout(context.Background(), 5*time.Second)
		defer annulla()
		_ = srv.Shutdown(fine) // all'arresto conta solo che il server si fermi
	}()
	pronto(l.Addr().String())
	return srv.Serve(l)
}
