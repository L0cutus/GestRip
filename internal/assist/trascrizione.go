package assist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
)

// La trascrizione, parte server (slice 109; RIP-H-44 parte server, RIP-H-46 parte trascrizione, RIP-H-47),
// come Assist.transcribe/3, Assist.Transcriber, Assist.Audio e Providers.LocalWhisper dello standalone a
// go-baseline: una clip registrata dal browser diventa testo. Il motore è un'interfaccia (Transcriber) con il
// «server esterno» di oggi (ServerEsterno: la clip così com'è a <indirizzo>/stt, il glossario del negozio in
// X-Gestrip-Glossario) e il motore finto dei test (TrascrittoreFinto); il motore locale (Parakeet, sidecar)
// arriva con la T5.1 (domanda G del piano T4.10), e per lui c'è già ConSilenzio.
//
// Riservatezza: la clip è la voce del cliente. Non si scrive mai su disco, né qui né nella pagina; il testo
// trascritto, il glossario, la chiave e i byte non vanno mai in un log, in un audit o in un errore restituito.
// La riga di audit assist.transcribe conta i byte e dice l'esito, come lo standalone.

// Limiti della clip.
const (
	// ClipMinima è @min_audio_bytes (app_riparazioni_bp.py:482): sotto è una registrazione vuota o per sbaglio.
	ClipMinima = 2_000
	// ClipMassima è max_file_size dell'upload :dictation (app.py:98): 25 MiB.
	ClipMassima = 26_214_400
)

// AttesaTrascrizione è @timeout di LocalWhisper (billing_client.py:206). Lo standalone la dà a hackney come
// recv_timeout; qui è la scadenza di tutta la richiesta.
const AttesaTrascrizione = 35 * time.Second

// massimoRispostaTrascrizione è quanto si legge al più della risposta del trascrittore: oltre è «risposta non
// riconosciuta».
const massimoRispostaTrascrizione = 1 << 20

// IntestazioneGlossario è l'intestazione con il glossario del negozio, sempre presente (anche vuota): il
// trascrittore della bottega sceglie il motore dalla sua presenza.
const IntestazioneGlossario = "X-Gestrip-Glossario"

// contenitori sono @containers di Assist.Audio: i formati che si possono passare al trascrittore.
var contenitori = []string{"audio/webm", "audio/ogg", "audio/mp4", "audio/mpeg", "audio/wav", "audio/x-wav"}

// TipoAudio è Audio.normalise/1: il tipo senza i parametri del registratore («;codecs=opus»), senza spazi ai
// bordi, in minuscolo.
func TipoAudio(tipo string) string {
	prima, _, _ := strings.Cut(tipo, ";")
	return strings.ToLower(trimElixir(prima))
}

// ContenitoreAmmesso è Audio.container?/1.
func ContenitoreAmmesso(tipo string) bool { return slices.Contains(contenitori, TipoAudio(tipo)) }

// Transcriber è Assist.Transcriber: la clip (i byte come li ha registrati il browser), il suo tipo e la
// configurazione; dà il testo come l'ha scritto il motore, senza togliere spazi e senza giudicarlo (se è
// vuoto lo decide Impostazioni.Trascrivi), oppure un ErrTrascrittore con un motivo fisso. Non scrive mai i
// byte, il testo o la chiave da nessuna parte.
type Transcriber interface {
	Trascrivi(ctx context.Context, audio []byte, tipo string, cfg ConfigTrascrizione) (string, error)
}

// Rifiuti della trascrizione, uno per nome come in Assist.transcribe/3.
var (
	ErrTrascrizioneNonConfigurata = errors.New("assist: nessuna trascrizione configurata")
	ErrClipCorta                  = errors.New("assist: registrazione troppo breve")
	ErrFormatoAudio               = errors.New("assist: formato della registrazione non ammesso")
)

// ErrTrascrittore è {:provider, reason}: il trascrittore non ha dato un testo. Motivo è un codice fisso:
// «stt:http:<stato>», «stt:transport:<genere>», «stt:risposta non riconosciuta» o «stt:provider non
// raggiungibile» (un motore andato in panic, o con un errore che non è un ErrTrascrittore).
type ErrTrascrittore struct{ Motivo string }

func (e ErrTrascrittore) Error() string {
	return "assist: trascrizione non disponibile (" + e.Motivo + ")"
}

// motivoIrraggiungibile è il motivo di un motore che va in panic (il rescue di ask/3).
const motivoIrraggiungibile = "stt:provider non raggiungibile"

// MessaggioDiTrascrizione è assist_refusal/2 di WorkshopLive per la famiglia «stt»: la frase che vede il
// negozio; vuota per un errore che non è un rifiuto.
func MessaggioDiTrascrizione(err error) string {
	var et ErrTrascrittore
	switch {
	case errors.Is(err, ErrTrascrizioneNonConfigurata):
		return "Nessuna trascrizione è configurata: scrivi a mano e premi Estrai."
	case errors.Is(err, ErrClipCorta):
		return "⚠ Registrazione troppo breve."
	case errors.Is(err, ErrFormatoAudio):
		return "⚠ Questo browser ha registrato in un formato che non so inoltrare."
	case errors.As(err, &et):
		return "⚠ Trascrizione non disponibile, scrivi a mano e premi Estrai."
	}
	return ""
}

// Trascrizione è l'esito di Impostazioni.Trascrivi: il testo senza spazi ai bordi e se è vuoto (no_speech).
type Trascrizione struct {
	Testo      string
	NienteVoce bool
}

// Messaggio è quello di handle_dictation/3 dopo una trascrizione riuscita.
func (t Trascrizione) Messaggio() string {
	if t.NienteVoce {
		return "🔇 Non ho sentito nulla, riprova."
	}
	return "✓ Trascritto. Controlla e premi «✦ Estrai con AI»."
}

// Trascrivi è Assist.transcribe/3: la configurazione salvata (LeggiTrascrizione, riletta a ogni clip), poi i
// rifiuti senza chiedere niente a nessuno e senza audit (trascrizione non configurata, clip sotto ClipMinima
// byte, formato che non è fra quelli di Assist.Audio, in quest'ordine), poi il motore (i.Trascrittore, il
// server esterno se non c'è) e la riga assist.transcribe (risorsa assist, id stt) con i byte e no_speech, o i
// byte e il motivo. Il limite di ClipMassima lo tiene chi riceve la clip. Una lettura della configurazione che
// fallisce è l'errore del database, non un rifiuto.
func (i *Impostazioni) Trascrivi(ctx context.Context, audio []byte, tipo string, a audit.Contesto) (Trascrizione, error) {
	cfg, err := i.trascrizione(ctx)
	if err != nil {
		return Trascrizione{}, err
	}
	switch {
	case !cfg.Usabile():
		return Trascrizione{}, ErrTrascrizioneNonConfigurata
	case len(audio) < ClipMinima:
		return Trascrizione{}, ErrClipCorta
	case !ContenitoreAmmesso(tipo):
		return Trascrizione{}, ErrFormatoAudio
	}
	var motore Transcriber = ServerEsterno{Attesa: i.AttesaTrascrizione, SoloLocale: i.SoloLocale}
	if i.Trascrittore != nil {
		motore = i.Trascrittore
	}
	grezzo, err := chiediAlMotore(ctx, motore, audio, tipo, cfg, i.registro())
	dati := map[string]any{"bytes": len(audio)}
	if err != nil {
		var et ErrTrascrittore
		if !errors.As(err, &et) {
			et = ErrTrascrittore{motivoIrraggiungibile} // un motore che non dice il motivo come si deve
		}
		dati["reason"] = et.Motivo
		registraAudit(ctx, i.reg, a, "assist.transcribe", FamigliaTrascrizione, "error", dati)
		return Trascrizione{}, et
	}
	t := Trascrizione{Testo: trimElixir(grezzo)}
	t.NienteVoce = t.Testo == ""
	dati["no_speech"] = t.NienteVoce
	registraAudit(ctx, i.reg, a, "assist.transcribe", FamigliaTrascrizione, "ok", dati)
	return t, nil
}

// chiediAlMotore è ask/3: un motore che va in panic è un rifiuto come gli altri. Nel registro va solo la
// famiglia e il tipo del valore del panic («assist provider for stt raised …» dello standalone, RIP-H-47): il
// valore (che può portare la clip, il testo o la chiave) non va da nessuna parte.
func chiediAlMotore(ctx context.Context, m Transcriber, audio []byte, tipo string, cfg ConfigTrascrizione, reg *slog.Logger) (testo string, err error) {
	defer func() {
		if v := recover(); v != nil {
			reg.Error("assist: il motore della trascrizione è andato in panic", "famiglia", FamigliaTrascrizione, "tipo", fmt.Sprintf("%T", v))
			testo, err = "", ErrTrascrittore{motivoIrraggiungibile}
		}
	}()
	return m.Trascrivi(ctx, audio, tipo, cfg)
}

// registro è il registro di Impostazioni (quello predefinito se non c'è).
func (i *Impostazioni) registro() *slog.Logger {
	if i.Registro != nil {
		return i.Registro
	}
	return slog.Default()
}

// ServerEsterno è Providers.LocalWhisper: il trascrittore che il titolare ha indicato nella pagina
// dell'assistente. Attesa la accorciano i test (0 = AttesaTrascrizione).
type ServerEsterno struct {
	Attesa time.Duration
	// SoloLocale: la connessione va solo a un indirizzo IP di loopback (trasportoSoloLocale, slice 131).
	SoloLocale bool
}

// Trascrivi è transcribe/3 di LocalWhisper: POST a <indirizzo senza spazi e senza «/» finali>/stt con la clip
// così com'è (niente modulo intorno), Content-Type il tipo normalizzato, Accept application/json,
// X-Gestrip-Glossario con il glossario nella forma a una riga (sempre, anche vuoto), Authorization Bearer solo
// con una chiave; niente proxy, niente redirect, niente keep-alive (trasportoModello, revisione della 106).
// Solo il 200 con {"text": <testo>} vale.
func (s ServerEsterno) Trascrivi(ctx context.Context, audio []byte, tipo string, cfg ConfigTrascrizione) (string, error) {
	attesa := s.Attesa
	if attesa <= 0 {
		attesa = AttesaTrascrizione
	}
	ctx, ferma := context.WithTimeout(ctx, attesa)
	defer ferma()
	indirizzo := strings.TrimRight(trimElixir(cfg.Indirizzo), "/") + "/stt"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, indirizzo, bytes.NewReader(audio))
	if err != nil {
		// L'indirizzo non si legge: il messaggio di url.Parse lo ripeterebbe.
		return "", ErrTrascrittore{"stt:transport::error"}
	}
	req.Header.Set(IntestazioneGlossario, GlossarioPerLaRete(cfg.Glossario))
	req.Header.Set("Content-Type", TipoAudio(tipo))
	req.Header.Set("Accept", "application/json")
	if chiave := trimElixir(cfg.Chiave); chiave != "" {
		req.Header.Set("Authorization", "Bearer "+chiave)
	}
	cliente := &http.Client{Transport: trasportoPer(s.SoloLocale), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := cliente.Do(req)
	if err != nil {
		return "", ErrTrascrittore{"stt:transport:" + genereRete(err)}
	}
	defer func() { _ = resp.Body.Close() }() // letto qui sotto quanto serve
	letto, err := io.ReadAll(io.LimitReader(resp.Body, massimoRispostaTrascrizione+1))
	if err != nil {
		return "", ErrTrascrittore{"stt:transport:" + genereRete(err)}
	}
	if resp.StatusCode != http.StatusOK {
		return "", ErrTrascrittore{"stt:http:" + strconv.Itoa(resp.StatusCode)}
	}
	var radice map[string]json.RawMessage
	if len(letto) > massimoRispostaTrascrizione || json.Unmarshal(letto, &radice) != nil {
		return "", ErrTrascrittore{"stt:risposta non riconosciuta"}
	}
	testo, ok := testoJSON(radice["text"])
	if !ok {
		return "", ErrTrascrittore{"stt:risposta non riconosciuta"}
	}
	return testo, nil
}

// TrascrittoreFinto è il motore finto (come il provider finto dei test dello standalone): dà Testo, o Err, o va
// in panic con Panico; conta le chiamate e tiene i byte e il tipo dell'ultima, per i controlli dei test.
type TrascrittoreFinto struct {
	Testo  string
	Err    error
	Panico bool

	Chiamate     int
	UltimiByte   int
	UltimoTipo   string
	UltimaConfig ConfigTrascrizione
}

// Trascrivi è il motore finto.
func (f *TrascrittoreFinto) Trascrivi(_ context.Context, audio []byte, tipo string, cfg ConfigTrascrizione) (string, error) {
	f.Chiamate++
	f.UltimiByte, f.UltimoTipo, f.UltimaConfig = len(audio), tipo, cfg
	if f.Panico {
		panic("trascrittore finto: " + f.Testo)
	}
	return f.Testo, f.Err
}

// registraAudit è audit/5 dello standalone, che non guarda l'esito della scrittura. La scrittura non usa la
// scadenza del motore, che può essere passata.
func registraAudit(ctx context.Context, reg Registratore, chi audit.Contesto, azione, famiglia, esito string, dati map[string]any) {
	if reg == nil {
		return
	}
	_ = reg.Registra(context.WithoutCancel(ctx), chi, azione, "assist", famiglia, esito, dati) // come lo standalone
}
