package assist

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/tools/banco/modellofinto"
)

// La trascrizione, parte server (slice 109; RIP-H-44 parte server, RIP-H-46 parte trascrizione, RIP-H-47: voci
// parziali, la pagina arriva con la 110), come Assist.transcribe/3, Assist.Audio e Providers.LocalWhisper dello
// standalone a go-baseline (assist_test.exs «the dictation», providers_test.exs, assist/audio_test.exs). Con il
// trascrittore finto (tools/banco/modellofinto, /stt) e un'attesa corta. Clip sintetiche (byte inventati, nessuna
// voce), testi, glossario e chiave inventati.

// clipFinta sono n byte inventati.
func clipFinta(n int) []byte { return bytes.Repeat([]byte{0x1a}, n) }

// trascrittoreDiProva è il server finto con la configurazione che punta lì.
func trascrittoreDiProva(t *testing.T) (*modellofinto.Server, ConfigTrascrizione) {
	t.Helper()
	f := modellofinto.Nuovo()
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	return f, ConfigTrascrizione{Indirizzo: s.URL, Chiave: chiaveFinta, Glossario: "Poloni\nBottari"}
}

// La richiesta di LocalWhisper: POST <indirizzo>/stt (spazi e «/» finali tolti), la clip intera e nuda nel
// corpo (niente modulo), il tipo normalizzato, Accept JSON, il glossario a una riga, la chiave come Bearer.
func TestServerEsternoRichiesta(t *testing.T) {
	f, cfg := trascrittoreDiProva(t)
	cfg.Indirizzo = "  " + cfg.Indirizzo + "//  "
	cfg.Chiave = "  " + chiaveFinta + " "
	f.ProgrammaSTT(modellofinto.Buona("  anello da stringere \n"))
	testo, err := ServerEsterno{Attesa: 2 * time.Second}.Trascrivi(context.Background(), clipFinta(2345), " Audio/WebM;codecs=opus", cfg)
	if err != nil || testo != "  anello da stringere \n" { // com'è: lo giudica Trascrivi di Impostazioni
		t.Fatalf("%q %v", testo, err)
	}
	r := f.RichiesteSTT()
	if len(r) != 1 {
		t.Fatalf("richieste %+v", r)
	}
	want := modellofinto.RichiestaSTT{Autorizzazione: "Bearer " + chiaveFinta, ConAutorizz: true, ContentType: "audio/webm",
		Accept: "application/json", Glossario: "Poloni, Bottari", ConGlossario: true, Byte: 2345}
	if r[0] != want {
		t.Errorf("richiesta %+v, attesa %+v", r[0], want)
	}
	if len(f.Richieste()) != 0 {
		t.Error("una trascrizione è andata a /v1/chat/completions")
	}
}

// L'intestazione del glossario c'è sempre, anche vuota (il trascrittore della bottega sceglie il motore dalla sua
// presenza); senza chiave niente Authorization.
func TestServerEsternoSenzaGlossarioNeChiave(t *testing.T) {
	f, cfg := trascrittoreDiProva(t)
	cfg.Chiave, cfg.Glossario = "  ", " \n , "
	if _, err := (ServerEsterno{Attesa: 2 * time.Second}).Trascrivi(context.Background(), clipFinta(2000), "audio/ogg", cfg); err != nil {
		t.Fatal(err)
	}
	r := f.RichiesteSTT()[0]
	if !r.ConGlossario || r.Glossario != "" || r.ConAutorizz || r.ContentType != "audio/ogg" {
		t.Errorf("richiesta %+v", r)
	}
}

// Le risposte del trascrittore: solo un 200 con {"text": <testo>} vale; i rifiuti hanno un motivo fisso.
func TestServerEsternoRisposte(t *testing.T) {
	grande := `{"text": "` + strings.Repeat("a", massimoRispostaTrascrizione) + `"}`
	// JSON valido di massimo+1 byte: il limite lo rifiuta anche se letto per intero sarebbe un testo.
	alLimite := `{"text": "` + strings.Repeat("a", massimoRispostaTrascrizione+1-len(`{"text": ""}`)) + `"}`
	for _, c := range []struct {
		nome   string
		risp   modellofinto.Risposta
		testo  string
		motivo string
	}{
		{"testo", modellofinto.Buona("ciao"), "ciao", ""},
		{"testo vuoto", modellofinto.Buona(""), "", ""},
		{"500", modellofinto.Risposta{Stato: 500}, "", "stt:http:500"},
		{"201 con un testo", modellofinto.Risposta{Stato: 201, Grezzo: modellofinto.Testo(`{"text": "ciao"}`)}, "", "stt:http:201"},
		{"302", modellofinto.Risposta{Stato: 302, Grezzo: modellofinto.Testo(`{"text": "ciao"}`)}, "", "stt:http:302"},
		{"senza text", modellofinto.Risposta{Grezzo: modellofinto.Testo(`{"segments": []}`)}, "", "stt:risposta non riconosciuta"},
		{"Text con la maiuscola", modellofinto.Risposta{Grezzo: modellofinto.Testo(`{"Text": "ciao"}`)}, "", "stt:risposta non riconosciuta"},
		{"text numero", modellofinto.Risposta{Grezzo: modellofinto.Testo(`{"text": 3}`)}, "", "stt:risposta non riconosciuta"},
		{"text null", modellofinto.Risposta{Grezzo: modellofinto.Testo(`{"text": null}`)}, "", "stt:risposta non riconosciuta"},
		{"non JSON", modellofinto.Risposta{Grezzo: modellofinto.Testo(`ciao`)}, "", "stt:risposta non riconosciuta"},
		{"elenco", modellofinto.Risposta{Grezzo: modellofinto.Testo(`["ciao"]`)}, "", "stt:risposta non riconosciuta"},
		{"oltre 1 MiB", modellofinto.Risposta{Grezzo: &grande}, "", "stt:risposta non riconosciuta"},
		{"1 MiB e un byte", modellofinto.Risposta{Grezzo: &alLimite}, "", "stt:risposta non riconosciuta"},
		{"rete giù", modellofinto.Risposta{Chiudi: true}, "", "stt:transport::closed"},
		{"lento", modellofinto.Lenta(5 * time.Second), "", "stt:transport::timeout"},
	} {
		f, cfg := trascrittoreDiProva(t)
		f.ProgrammaSTT(c.risp)
		testo, err := ServerEsterno{Attesa: 300 * time.Millisecond}.Trascrivi(context.Background(), clipFinta(2000), "audio/webm", cfg)
		if c.motivo == "" {
			if err != nil || testo != c.testo {
				t.Errorf("%s: %q %v", c.nome, testo, err)
			}
			continue
		}
		var et ErrTrascrittore
		if !errors.As(err, &et) || et.Motivo != c.motivo || testo != "" {
			t.Errorf("%s: %q %v", c.nome, testo, err)
		}
	}
}

// Il motore non segue i redirect (la chiave e la clip vanno solo all'indirizzo configurato), e un server spento è
// :econnrefused; un indirizzo che non si legge non viene ripetuto nell'errore.
func TestServerEsternoRete(t *testing.T) {
	altrove := modellofinto.Nuovo()
	sa := httptest.NewServer(altrove)
	t.Cleanup(sa.Close)
	rimanda := httptest.NewServer(http.RedirectHandler(sa.URL+"/stt", http.StatusTemporaryRedirect))
	t.Cleanup(rimanda.Close)
	m := ServerEsterno{Attesa: 2 * time.Second}
	_, err := m.Trascrivi(context.Background(), clipFinta(2000), "audio/webm", ConfigTrascrizione{Indirizzo: rimanda.URL, Chiave: chiaveFinta})
	if fmt.Sprint(err) != fmt.Sprint(ErrTrascrittore{"stt:http:307"}) || len(altrove.RichiesteSTT()) != 0 {
		t.Errorf("redirect: %v, %d richieste altrove", err, len(altrove.RichiesteSTT()))
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	spento := "http://" + l.Addr().String()
	_ = l.Close() // porta chiusa
	if _, err := m.Trascrivi(context.Background(), clipFinta(2000), "audio/webm", ConfigTrascrizione{Indirizzo: spento}); fmt.Sprint(err) != fmt.Sprint(ErrTrascrittore{"stt:transport::econnrefused"}) {
		t.Errorf("spento: %v", err)
	}
	brutto := "http://utente:" + chiaveFinta + "@[::1"
	if _, err := m.Trascrivi(context.Background(), clipFinta(2000), "audio/webm", ConfigTrascrizione{Indirizzo: brutto}); fmt.Sprint(err) != fmt.Sprint(ErrTrascrittore{"stt:transport::error"}) {
		t.Errorf("indirizzo rotto: %v", err)
	}
	ctx, annulla := context.WithCancel(context.Background())
	annulla()
	f, cfg := trascrittoreDiProva(t)
	if _, err := m.Trascrivi(ctx, clipFinta(2000), "audio/webm", cfg); fmt.Sprint(err) != fmt.Sprint(ErrTrascrittore{"stt:transport::closed"}) || len(f.RichiesteSTT()) != 0 {
		t.Errorf("chiamante andato via: %v", err)
	}
}

// L'attesa predefinita è quella dello standalone: 35 s.
func TestAttesaTrascrizione(t *testing.T) {
	if AttesaTrascrizione != 35*time.Second || ClipMinima != 2000 || ClipMassima != 25<<20 {
		t.Errorf("%v %d %d", AttesaTrascrizione, ClipMinima, ClipMassima)
	}
	f, cfg := trascrittoreDiProva(t)
	f.ProgrammaSTT(modellofinto.Lenta(400 * time.Millisecond))
	// Senza Attesa vale AttesaTrascrizione: 400 ms ci stanno.
	if testo, err := (ServerEsterno{}).Trascrivi(context.Background(), clipFinta(2000), "audio/webm", cfg); err != nil || testo != modellofinto.TestoTrascritto {
		t.Errorf("%q %v", testo, err)
	}
}

// Audio.normalise/1 e Audio.container?/1: la lista dello standalone, senza i parametri del registratore.
func TestFormatiAmmessi(t *testing.T) {
	for _, tipo := range []string{"audio/webm", "audio/webm;codecs=opus", " AUDIO/OGG ; codecs=opus", "audio/mp4", "audio/mpeg", "audio/wav", "audio/x-wav"} {
		if !ContenitoreAmmesso(tipo) {
			t.Errorf("%q rifiutato", tipo)
		}
	}
	for _, tipo := range []string{"", "application/octet-stream", "audio/aac", "audio/flac", "video/webm", "audio/webm2", "audio/ webm", "audio", "text/plain"} {
		if ContenitoreAmmesso(tipo) {
			t.Errorf("%q ammesso", tipo)
		}
	}
	if g := TipoAudio("\tAudio/MP4;codecs=mp4a.40.2"); g != "audio/mp4" {
		t.Errorf("TipoAudio %q", g)
	}
}

// I messaggi di assist_refusal/2 per la famiglia «stt» e di handle_dictation/3.
func TestMessaggiDellaTrascrizione(t *testing.T) {
	for err, want := range map[error]string{
		ErrTrascrizioneNonConfigurata:             "Nessuna trascrizione è configurata: scrivi a mano e premi Estrai.",
		ErrClipCorta:                              "⚠ Registrazione troppo breve.",
		ErrFormatoAudio:                           "⚠ Questo browser ha registrato in un formato che non so inoltrare.",
		ErrTrascrittore{"stt:http:500"}:           "⚠ Trascrizione non disponibile, scrivi a mano e premi Estrai.",
		fmt.Errorf("x: %w", ErrClipCorta):         "⚠ Registrazione troppo breve.",
		errors.New("database chiuso"):             "",
		ErrNonConfigurato:                         "",
		ErrModello{"ai:http:500"}:                 "",
		fmt.Errorf("y: %w", ErrTrascrittore{"x"}): "⚠ Trascrizione non disponibile, scrivi a mano e premi Estrai.",
	} {
		if g := MessaggioDiTrascrizione(err); g != want {
			t.Errorf("%v: %q", err, g)
		}
	}
	if MessaggioDiTrascrizione(nil) != "" {
		t.Error("nil")
	}
	if g := (Trascrizione{Testo: "ciao"}).Messaggio(); g != "✓ Trascritto. Controlla e premi «✦ Estrai con AI»." {
		t.Error(g)
	}
	if g := (Trascrizione{NienteVoce: true}).Messaggio(); g != "🔇 Non ho sentito nulla, riprova." {
		t.Error(g)
	}
}

// configuraTrascrizione salva la trascrizione puntata al server finto, con chiave e glossario.
func configuraTrascrizione(t *testing.T, i *Impostazioni) *modellofinto.Server {
	t.Helper()
	f := modellofinto.Nuovo()
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	salvaBene(t, i, FamigliaTrascrizione, Modulo{"endpoint": s.URL, "api_key": chiaveFinta, "glossary": "Poloni\nBottari"})
	return f
}

func righeTrascrizione(t *testing.T, i *Impostazioni) int {
	t.Helper()
	return conta(t, i.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.transcribe'`)
}

// Assist.transcribe/3: prima, senza chiedere niente e senza audit, la trascrizione non configurata, poi la clip
// sotto i 2000 byte, poi il formato, in quest'ordine.
func TestTrascriviRifiutiPrima(t *testing.T) {
	i, _ := impostazioniDiProva(t)
	finto := &TrascrittoreFinto{Testo: "ciao"}
	i.Trascrittore = finto
	ctx := context.Background()
	if _, err := i.Trascrivi(ctx, clipFinta(1), "text/plain", chi); err != ErrTrascrizioneNonConfigurata {
		t.Errorf("non configurata: %v", err)
	}
	salvaBene(t, i, FamigliaTrascrizione, Modulo{"endpoint": "http://127.0.0.1:1"})
	if _, err := i.Trascrivi(ctx, clipFinta(ClipMinima-1), "text/plain", chi); err != ErrClipCorta {
		t.Errorf("corta: %v", err)
	}
	if _, err := i.Trascrivi(ctx, nil, "audio/webm", chi); err != ErrClipCorta {
		t.Errorf("vuota: %v", err)
	}
	if _, err := i.Trascrivi(ctx, clipFinta(ClipMinima), "application/octet-stream", chi); err != ErrFormatoAudio {
		t.Errorf("formato: %v", err)
	}
	if finto.Chiamate != 0 || righeTrascrizione(t, i) != 0 {
		t.Errorf("%d chiamate, %d righe", finto.Chiamate, righeTrascrizione(t, i))
	}
	tr, err := i.Trascrivi(ctx, clipFinta(ClipMinima), "audio/wav", chi)
	if err != nil || tr.Testo != "ciao" || finto.Chiamate != 1 || finto.UltimiByte != ClipMinima || finto.UltimoTipo != "audio/wav" ||
		finto.UltimaConfig.Indirizzo != "http://127.0.0.1:1" {
		t.Errorf("2000 byte: %+v %v %+v", tr, err, finto)
	}
}

// Una trascrizione riuscita: il testo senza spazi ai bordi; vuoto (a parte gli spazi) è «niente voce». La riga
// assist.transcribe ha l'attore, la risorsa assist con id stt, i byte e no_speech, mai il testo.
func TestTrascriviRiuscita(t *testing.T) {
	i, d := impostazioniDiProva(t)
	f := configuraTrascrizione(t, i)
	f.ProgrammaSTT(modellofinto.Buona(" \n anello da stringere\t"), modellofinto.Buona(" \n "))
	tr, err := i.Trascrivi(context.Background(), clipFinta(4321), "audio/webm;codecs=opus", chi)
	if err != nil || tr != (Trascrizione{Testo: "anello da stringere"}) {
		t.Fatalf("%+v %v", tr, err)
	}
	r := f.RichiesteSTT()
	if len(r) != 1 || r[0].Glossario != "Poloni, Bottari" || r[0].Autorizzazione != "Bearer "+chiaveFinta || r[0].Byte != 4321 || r[0].ContentType != "audio/webm" {
		t.Errorf("richiesta %+v", r)
	}
	ev := ultimoEvento(t, d, "assist.transcribe")
	if ev.Esito != "ok" || ev.Attore != chi.Attore || ev.Risorsa != "assist" || ev.ID != "stt" ||
		fmt.Sprint(ev.Dati) != fmt.Sprint(map[string]any{"bytes": float64(4321), "no_speech": false}) {
		t.Errorf("audit %+v", ev)
	}
	tr, err = i.Trascrivi(context.Background(), clipFinta(2000), "audio/mp4", chi)
	if err != nil || tr != (Trascrizione{NienteVoce: true}) {
		t.Fatalf("silenzio %+v %v", tr, err)
	}
	if ev := ultimoEvento(t, d, "assist.transcribe"); ev.Esito != "ok" || fmt.Sprint(ev.Dati) != fmt.Sprint(map[string]any{"bytes": float64(2000), "no_speech": true}) {
		t.Errorf("audit %+v", ev)
	}
}

// Un trascrittore che non risponde: ErrTrascrittore con il motivo, e la riga error con i byte e il motivo. Un motore
// che va in panic, o che dà un errore senza motivo, è «stt:provider non raggiungibile» (il rescue di ask/3).
func TestTrascriviRifiutoDelMotore(t *testing.T) {
	i, d := impostazioniDiProva(t)
	f := configuraTrascrizione(t, i)
	f.ProgrammaSTT(modellofinto.Risposta{Stato: 503})
	if _, err := i.Trascrivi(context.Background(), clipFinta(3000), "audio/webm", chi); fmt.Sprint(err) != fmt.Sprint(ErrTrascrittore{"stt:http:503"}) {
		t.Errorf("503: %v", err)
	}
	if ev := ultimoEvento(t, d, "assist.transcribe"); ev.Esito != "error" || fmt.Sprint(ev.Dati) != fmt.Sprint(map[string]any{"bytes": float64(3000), "reason": "stt:http:503"}) {
		t.Errorf("audit %+v", ev)
	}
	for _, m := range []*TrascrittoreFinto{{Panico: true, Testo: "segreto"}, {Err: errors.New("guasto con " + chiaveFinta)}} {
		i.Trascrittore = m
		_, err := i.Trascrivi(context.Background(), clipFinta(2500), "audio/webm", chi)
		if fmt.Sprint(err) != fmt.Sprint(ErrTrascrittore{"stt:provider non raggiungibile"}) {
			t.Errorf("%+v: %v", m, err)
		}
		if ev := ultimoEvento(t, d, "assist.transcribe"); ev.Esito != "error" || fmt.Sprint(ev.Dati) != fmt.Sprint(map[string]any{"bytes": float64(2500), "reason": "stt:provider non raggiungibile"}) {
			t.Errorf("audit %+v", ev)
		}
	}
	if n := righeTrascrizione(t, i); n != 3 {
		t.Errorf("%d righe", n)
	}
}

// L'attesa della trascrizione finisce anche se la richiesta del negozio è ancora viva, e la riga di audit si scrive
// lo stesso (senza la scadenza passata).
func TestTrascriviAttesaScaduta(t *testing.T) {
	i, d := impostazioniDiProva(t)
	i.AttesaTrascrizione = 200 * time.Millisecond
	f := configuraTrascrizione(t, i)
	f.ProgrammaSTT(modellofinto.Lenta(5 * time.Second))
	inizio := time.Now()
	if _, err := i.Trascrivi(context.Background(), clipFinta(2000), "audio/webm", chi); fmt.Sprint(err) != fmt.Sprint(ErrTrascrittore{"stt:transport::timeout"}) {
		t.Errorf("err %v", err)
	}
	if time.Since(inizio) > 3*time.Second {
		t.Error("l'attesa non è stata rispettata")
	}
	if ev := ultimoEvento(t, d, "assist.transcribe"); ev.Dati["reason"] != "stt:transport::timeout" {
		t.Errorf("audit %+v", ev)
	}
}

// Una lettura della configurazione che fallisce è l'errore del database, non un rifiuto.
func TestTrascriviDatabaseChiuso(t *testing.T) {
	i, d := impostazioniDiProva(t)
	_ = d.Chiudi()
	_, err := i.Trascrivi(context.Background(), clipFinta(2000), "audio/webm", chi)
	if err == nil || MessaggioDiTrascrizione(err) != "" {
		t.Errorf("err %v", err)
	}
}

// RIP-H-47: il testo trascritto, il glossario, la chiave e i byte della clip non finiscono mai in un errore, in un
// messaggio, nell'audit o in un log, qualunque cosa faccia il trascrittore (anche ripeterli).
func TestPrivacyTrascrizione(t *testing.T) {
	const (
		parole    = "Gianfilippo Inventatissimo collana"
		glossario = "Bottegaiofinto"
	)
	clip := append([]byte("CLIPSEGRETA-"), clipFinta(3000)...)
	segreti := []string{parole, "Inventatissimo", glossario, chiaveFinta, "CLIPSEGRETA"}
	eco := `{"error": "` + parole + ` ` + glossario + ` Bearer ` + chiaveFinta + `"}`

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
		nome   string
		risp   *modellofinto.Risposta
		motore Transcriber
		grezzo bool
	}{
		{nome: "riuscita", risp: &modellofinto.Risposta{Contenuto: modellofinto.Testo(parole)}},
		{nome: "500 con eco", risp: &modellofinto.Risposta{Stato: 500, Grezzo: &eco}},
		{nome: "200 illeggibile con eco", risp: &modellofinto.Risposta{Grezzo: &eco}},
		{nome: "lento", risp: &modellofinto.Risposta{RitardoMs: 5000}},
		{nome: "rete giù", risp: &modellofinto.Risposta{Chiudi: true}},
		{nome: "panic con le parole", motore: &TrascrittoreFinto{Panico: true, Testo: parole + " " + chiaveFinta}},
		{nome: "errore con le parole", motore: &TrascrittoreFinto{Err: errors.New(parole + " " + chiaveFinta)}},
		{nome: "byte oltre il Content-Length", grezzo: true},
	} {
		i, d := impostazioniDiProva(t)
		i.AttesaTrascrizione = 300 * time.Millisecond
		f := modellofinto.Nuovo()
		s := httptest.NewServer(f)
		indirizzo := s.URL
		if c.grezzo {
			indirizzo = serverGrezzo(t, func(conn net.Conn) {
				corpo := `{"text": "ok"}`
				_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: "+
					strconv.Itoa(len(corpo))+"\r\n\r\n"+corpo+"ECO "+parole+" "+chiaveFinta)
				time.Sleep(300 * time.Millisecond) // il tempo per il lettore di net/http
			})
		}
		salvaBene(t, i, FamigliaTrascrizione, Modulo{"endpoint": indirizzo, "api_key": chiaveFinta, "glossary": glossario})
		if c.risp != nil {
			f.ProgrammaSTT(*c.risp)
		}
		i.Trascrittore = c.motore
		tr, err := i.Trascrivi(context.Background(), clip, "audio/webm", chi)
		if c.grezzo {
			time.Sleep(500 * time.Millisecond) // il lettore di net/http scriverebbe nel log in questo tempo
		}
		s.Close()
		var visto strings.Builder
		fmt.Fprintf(&visto, "%v|%+v|%#v|%s|", err, err, err, MessaggioDiTrascrizione(err))
		if c.nome != "riuscita" { // riuscita, il testo è lo scopo
			fmt.Fprintf(&visto, "%+v|%s|", tr, tr.Messaggio())
		}
		for e := err; e != nil; e = errors.Unwrap(e) {
			visto.WriteString(e.Error() + "|")
		}
		var righe []string
		rr, qerr := d.Lettura.Query(`SELECT azione || risorsa || id_risorsa || esito || dati FROM eventi_audit`)
		if qerr != nil {
			t.Fatal(qerr)
		}
		for rr.Next() {
			var x string
			if err := rr.Scan(&x); err != nil {
				t.Fatal(err)
			}
			righe = append(righe, x)
		}
		_ = rr.Close()
		j, _ := json.Marshal(righe)
		visto.Write(j)
		visto.WriteString(bufLog.String())
		for _, s := range segreti {
			if strings.Contains(visto.String(), s) {
				t.Errorf("%s: %q trapela in %q", c.nome, s, visto.String())
			}
		}
		if n := righeTrascrizione(t, i); n != 1 {
			t.Errorf("%s: %d righe assist.transcribe", c.nome, n)
		}
	}
	// Una sola riga: il panic del motore, con la famiglia e il tipo del valore (RIP-H-47), nient'altro.
	if righe := strings.Split(strings.TrimSpace(bufLog.String()), "\n"); len(righe) != 1 ||
		!strings.Contains(righe[0], `msg="assist: il motore della trascrizione è andato in panic" famiglia=stt tipo=string`) {
		t.Errorf("registro %q", bufLog.String())
	}
}

// wavProva è un WAV PCM a 16 bit con i campioni dati (canali intrecciati), con un pezzo LIST prima dei dati.
func wavProva(frequenza uint32, canali uint16, campioni []int16, extra bool) []byte {
	var corpo bytes.Buffer
	fmtc := new(bytes.Buffer)
	_ = binary.Write(fmtc, binary.LittleEndian, struct {
		Tipo, Canali         uint16
		Frequenza, ByteAlSec uint32
		Blocco, Bit          uint16
	}{1, canali, frequenza, frequenza * uint32(canali) * 2, canali * 2, 16})
	corpo.WriteString("WAVEfmt ")
	_ = binary.Write(&corpo, binary.LittleEndian, uint32(fmtc.Len()))
	corpo.Write(fmtc.Bytes())
	if extra {
		corpo.WriteString("LIST")
		_ = binary.Write(&corpo, binary.LittleEndian, uint32(3))
		corpo.WriteString("abc\x00") // dispari: un byte in più
	}
	corpo.WriteString("data")
	_ = binary.Write(&corpo, binary.LittleEndian, uint32(len(campioni)*2))
	_ = binary.Write(&corpo, binary.LittleEndian, campioni)
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(corpo.Len()))
	b.Write(corpo.Bytes())
	return b.Bytes()
}

// leggiWAV dà frequenza, canali e campioni di un WAV scritto da ConSilenzio (forma canonica).
func leggiWAV(t *testing.T, b []byte) (uint32, uint16, []int16) {
	t.Helper()
	if string(b[0:4]) != "RIFF" || string(b[8:16]) != "WAVEfmt " || binary.LittleEndian.Uint32(b[4:8]) != uint32(len(b)-8) {
		t.Fatalf("intestazione %q", b[:16])
	}
	n := binary.LittleEndian.Uint32(b[16:20])
	f := b[20 : 20+n]
	dati := b[20+n+n%2:]
	if string(dati[0:4]) != "data" || binary.LittleEndian.Uint32(dati[4:8]) != uint32(len(dati)-8) {
		t.Fatalf("data %q", dati[:8])
	}
	c := make([]int16, (len(dati)-8)/2)
	_ = binary.Read(bytes.NewReader(dati[8:]), binary.LittleEndian, c)
	return binary.LittleEndian.Uint32(f[4:8]), binary.LittleEndian.Uint16(f[2:4]), c
}

// Mezzo secondo di silenzio prima e dopo (PIANO §1.8): 8000 campioni a zero ai due lati di un WAV mono a 16 kHz
// (D-25), i campioni in mezzo come erano; con due canali a 44,1 kHz 22050 campioni per canale.
func TestConSilenzio(t *testing.T) {
	campioni := []int16{1, -2, 3, 32767, -32768}
	for _, extra := range []bool{false, true} {
		orig := wavProva(16000, 1, campioni, extra)
		copia := append([]byte(nil), orig...)
		out, err := ConSilenzio(orig)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(orig, copia) {
			t.Error("la clip ricevuta è cambiata")
		}
		fr, can, c := leggiWAV(t, out)
		if fr != 16000 || can != 1 || len(c) != 8000+5+8000 {
			t.Fatalf("%d %d %d", fr, can, len(c))
		}
		for k, v := range c {
			if (k < 8000 || k >= 8005) && v != 0 || k >= 8000 && k < 8005 && v != campioni[k-8000] {
				t.Fatalf("campione %d = %d", k, v)
			}
		}
	}
	out, err := ConSilenzio(wavProva(44100, 2, []int16{5, 6}, false))
	if err != nil {
		t.Fatal(err)
	}
	if _, can, c := leggiWAV(t, out); can != 2 || len(c) != 2*22050*2+2 || c[44100] != 5 || c[44101] != 6 {
		t.Errorf("stereo: %d canali, %d campioni", can, len(c))
	}
}

// Un registratore che non sa la lunghezza («data» di 0xFFFFFFFF) vale per i byte che ci sono; un campione a metà
// si lascia.
func TestConSilenzioLunghezzaIgnota(t *testing.T) {
	w := wavProva(16000, 1, []int16{7, 8}, false)
	binary.LittleEndian.PutUint32(w[40:44], 0xFFFFFFFF)
	w = append(w, 0x09) // mezzo campione
	out, err := ConSilenzio(w)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, c := leggiWAV(t, out); len(c) != 16002 || c[8000] != 7 || c[8001] != 8 {
		t.Errorf("%d campioni", len(c))
	}
}

// Quello che non è un WAV PCM a 16 bit non si tocca.
func TestConSilenzioRifiuti(t *testing.T) {
	buono := wavProva(16000, 1, []int16{1}, false)
	cambia := func(pos int, v uint16) []byte {
		b := append([]byte(nil), buono...)
		binary.LittleEndian.PutUint16(b[pos:pos+2], v)
		return b
	}
	senzaData := append([]byte(nil), buono[:36]...)
	binary.LittleEndian.PutUint32(senzaData[4:8], 28)
	fmtCorto := append([]byte(nil), buono...)
	binary.LittleEndian.PutUint32(fmtCorto[16:20], 1000) // un pezzo fmt che va oltre la fine
	for nome, b := range map[string][]byte{
		"vuoto":                     nil,
		"webm":                      append([]byte{0x1a, 0x45, 0xdf, 0xa3}, clipFinta(3000)...),
		"RIFF non WAVE":             append([]byte("RIFF\x00\x00\x00\x00AVI "), clipFinta(40)...),
		"float":                     cambia(20, 3),
		"8 bit":                     cambia(34, 8),
		"zero canali":               cambia(22, 0),
		"zero canali e blocco zero": func() []byte { b := cambia(22, 0); binary.LittleEndian.PutUint16(b[32:34], 0); return b }(),
		"blocco sbagliato":          cambia(32, 4),
		"senza data":                senzaData,
		"fmt oltre":                 fmtCorto,
	} {
		if _, err := ConSilenzio(b); err != ErrWAV {
			t.Errorf("%s: %v", nome, err)
		}
	}
}

// pezzoRIFF è un pezzo di un RIFF di prova: id, byte e la lunghezza dichiarata (-1 = quella vera).
type pezzoRIFF struct {
	id   string
	dati []byte
	dim  int64
}

// riffProva scrive «RIFF», la dimensione vera, forma («WAVE») e i pezzi, con il byte di riempimento dei dispari.
func riffProva(forma string, pezzi ...pezzoRIFF) []byte {
	var c bytes.Buffer
	c.WriteString(forma)
	for _, p := range pezzi {
		c.WriteString(p.id)
		dim := p.dim
		if dim < 0 {
			dim = int64(len(p.dati))
		}
		_ = binary.Write(&c, binary.LittleEndian, uint32(dim))
		c.Write(p.dati)
		if len(p.dati)%2 == 1 {
			c.WriteByte(0)
		}
	}
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(c.Len()))
	b.Write(c.Bytes())
	return b.Bytes()
}

// fmtProva è il pezzo fmt di un PCM a 16 bit.
func fmtProva(frequenza uint32, canali uint16) []byte {
	var f bytes.Buffer
	_ = binary.Write(&f, binary.LittleEndian, struct {
		Tipo, Canali         uint16
		Frequenza, ByteAlSec uint32
		Blocco, Bit          uint16
	}{1, canali, frequenza, frequenza * uint32(canali) * 2, canali * 2, 16})
	return f.Bytes()
}

// Il panic del motore va nel registro di Impostazioni, se c'è, con la famiglia e il tipo soltanto.
func TestTrascriviPanicNelRegistro(t *testing.T) {
	i, _ := impostazioniDiProva(t)
	var reg bytes.Buffer
	i.Registro = slog.New(slog.NewTextHandler(&reg, nil))
	salvaBene(t, i, FamigliaTrascrizione, Modulo{"endpoint": "http://127.0.0.1:1"})
	i.Trascrittore = &TrascrittoreFinto{Panico: true, Testo: "parole segrete"}
	if _, err := i.Trascrivi(context.Background(), clipFinta(2000), "audio/webm", chi); err == nil {
		t.Fatal("nessun errore")
	}
	if g := reg.String(); !strings.Contains(g, "famiglia=stt tipo=string") || strings.Contains(g, "segrete") {
		t.Errorf("registro %q", g)
	}
}

// I limiti dell'intestazione (revisione della 109): il silenzio si alloca da frequenza e canali, quindi oltre 48 kHz
// o 8 canali è ErrWAV, anche in un file di pochi byte (un 1e9 Hz chiedeva gigabyte); 48 kHz e 8 canali vanno.
func TestConSilenzioLimiti(t *testing.T) {
	dati := pezzoRIFF{"data", []byte{1, 0, 2, 0, 3, 0, 4, 0, 5, 0, 6, 0, 7, 0, 8, 0}, -1}
	for nome, c := range map[string]struct {
		frequenza uint32
		canali    uint16
		ok        bool
	}{
		"1 MHz": {1_000_000, 1, false}, "48001 Hz": {48_001, 1, false}, "48 kHz": {48_000, 1, true},
		"9 canali": {16_000, 9, false}, "8 canali": {48_000, 8, true},
	} {
		w := riffProva("WAVE", pezzoRIFF{"fmt ", fmtProva(c.frequenza, c.canali), -1}, dati)
		out, err := ConSilenzio(w)
		if c.ok != (err == nil) {
			t.Errorf("%s: %v", nome, err)
		}
		if c.ok && len(out) > len(w)+2*int(c.frequenza/2)*int(c.canali)*2 {
			t.Errorf("%s: %d byte", nome, len(out))
		}
	} // Campioni oltre ClipMassima: il risultato non supera mai ClipMassima più i due silenzi.
	troppi := riffProva("WAVE", pezzoRIFF{"fmt ", fmtProva(16000, 1), -1}, pezzoRIFF{"data", make([]byte, ClipMassima+2), -1})
	if _, err := ConSilenzio(troppi); err != ErrWAV {
		t.Errorf("oltre ClipMassima: %v", err)
	}
	if _, err := ConSilenzio(riffProva("WAVE", pezzoRIFF{"fmt ", fmtProva(16000, 1), -1}, pezzoRIFF{"data", make([]byte, ClipMassima), -1})); err != nil {
		t.Errorf("ClipMassima: %v", err)
	}
}

// La forma del RIFF: «WAVE» (non un altro RIFF con gli stessi pezzi); un fmt dopo i dati e troncato non vale; un fmt di
// 14 byte non basta; un fmt di lunghezza dispari si copia con il suo riempimento, contato nella dimensione del RIFF;
// trovati fmt e data, quello che segue non si guarda (anche un pezzo troncato).
func TestConSilenzioForma(t *testing.T) {
	f := pezzoRIFF{"fmt ", fmtProva(16000, 1), -1}
	d := pezzoRIFF{"data", []byte{9, 0, 8, 0}, -1}
	if _, err := ConSilenzio(riffProva("WAVX", f, d)); err != ErrWAV {
		t.Errorf("WAVX: %v", err)
	}
	if _, err := ConSilenzio(riffProva("WAVE", d, pezzoRIFF{"fmt ", fmtProva(16000, 1), 1000})); err != ErrWAV {
		t.Errorf("fmt troncato dopo i dati: %v", err)
	}
	// Il fmt di 14 byte è seguito da un pezzo il cui id comincia con 16 (0x10 0x00): leggere i bit oltre il pezzo
	// li troverebbe giusti.
	if _, err := ConSilenzio(riffProva("WAVE", pezzoRIFF{"fmt ", fmtProva(16000, 1)[:14], -1}, pezzoRIFF{"\x10\x00xy", nil, -1}, d)); err != ErrWAV {
		t.Errorf("fmt di 14 byte: %v", err)
	}
	dispari := pezzoRIFF{"fmt ", append(fmtProva(16000, 1), 0x77), -1}
	out, err := ConSilenzio(riffProva("WAVE", dispari, d))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, c := leggiWAV(t, out); len(c) != 16002 || c[8000] != 9 || c[8001] != 8 {
		t.Errorf("fmt dispari: %d campioni", len(c))
	}
	coda := append(riffProva("WAVE", f, d), []byte("LIST\xff\xff\x00\x00ab")...)
	binary.LittleEndian.PutUint32(coda[4:8], uint32(len(coda)-8))
	if _, err := ConSilenzio(coda); err != nil {
		t.Errorf("pezzo troncato dopo fmt e data: %v", err)
	}
}
