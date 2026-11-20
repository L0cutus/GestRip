package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/L0cutus/gestrip/internal/assist"
)

// La clip del «🎤 Detta a voce» (slice 109, parte server; RIP-H-44, RIP-H-47), come l'upload :dictation e
// handle_dictation/3 di WorkshopLive dello standalone a go-baseline, senza LiveView: il pulsante e la
// registrazione nel browser arrivano con la 110.
//
// POST /nuova/trascrivi è un modulo multipart (csrf.ModuloConFile: il token in testa al corpo, o
// nell'intestazione x-csrf-token, prima che arrivi un byte della clip) per tutto il personale, come /nuova:
//   - una sola parte con un file, il campo «dictation» (max_entries: 1); il suo Content-Type è il tipo della
//     clip (client_type, «application/octet-stream» se manca). Un modulo senza nessun file (anche non
//     multipart) è una clip vuota, che Trascrivi rifiuta come le altre; un file di un altro campo, o altre parti
//     dopo la clip, sono il 400;
//   - oltre assist.ClipMassima (25 MiB, max_file_size) la lettura si ferma e la risposta è il 413: lo
//     standalone non dice niente nella pagina (l'errore dell'upload LiveView non si mostra);
//   - quando la clip è arrivata la sessione si ricontrolla (get_session_user): revocata, si va a /login e la
//     clip non arriva al trascrittore;
//   - poi assist.Impostazioni.Trascrivi (configurazione, 2000 byte, formati, motore, audit assist.transcribe).
//
// La risposta è JSON, per la pagina della 110: {"tipo": "ok", "messaggio": «✓ Trascritto…», "testo": …} (il
// testo senza spazi ai bordi, che la pagina aggiunge in fondo alla casella come append_dictated/2), oppure
// {"tipo": "error", "messaggio": …} con le frasi di assist_refusal/2 e «🔇 Non ho sentito nulla, riprova.»,
// sempre 200 come un evento LiveView; mai in cache.
//
// Riservatezza: la clip sta solo in memoria (niente file, nemmeno temporanei) e si azzera dopo l'uso, con i
// buffer lasciati indietro mentre cresce (leggiClip); restano i pochi KiB dei buffer di lettura di net/http e
// mime/multipart. Né lei né il testo vanno nel registro. Al più clipPerIstanza clip alla volta, clipPerUtente
// per account. Un guasto del database è la pagina d'errore con una frase fissa.

// campoClip è il nome dell'upload dello standalone (allow_upload(:dictation)).
const campoClip = "dictation"

// limiteClip è il corpo più grande di POST /nuova/trascrivi: la clip e un MiB per le intestazioni delle parti
// e i campi di testa.
const limiteClip = assist.ClipMassima + 1<<20

// errClipGrande: la clip supera assist.ClipMassima.
var errClipGrande = errors.New("caricamento: clip oltre il limite")

// esitoClip è la risposta JSON di POST /nuova/trascrivi.
type esitoClip struct {
	Tipo      string `json:"tipo"`
	Messaggio string `json:"messaggio"`
	Testo     string `json:"testo,omitempty"`
}

// Clip in corso (revisione della 109): ogni clip tiene in memoria fino a 25 MiB per fino a 35 s, quindi al più
// clipPerIstanza in tutto e clipPerUtente per account; oltre, «⚠ Trascrizione non disponibile…» senza leggere la
// clip.
const (
	clipPerIstanza = 2
	clipPerUtente  = 1
)

// clipInCorso conta le clip che si stanno ricevendo o trascrivendo.
type clipInCorso struct {
	mu        sync.Mutex
	tutte     int
	perUtente map[int64]int
}

// prendi occupa un posto per l'utente; false se non ce n'è.
func (c *clipInCorso) prendi(utente int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tutte >= clipPerIstanza || c.perUtente[utente] >= clipPerUtente {
		return false
	}
	if c.perUtente == nil {
		c.perUtente = map[int64]int{}
	}
	c.tutte++
	c.perUtente[utente]++
	return true
}

// lascia libera il posto preso da prendi.
func (c *clipInCorso) lascia(utente int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tutte--
	if c.perUtente[utente]--; c.perUtente[utente] <= 0 {
		delete(c.perUtente, utente)
	}
}

// trascriviClip è POST /nuova/trascrivi.
func (f *facciaWeb) trascriviClip(w http.ResponseWriter, r *http.Request) {
	st := statoDi(r)
	if !f.clip.prendi(st.dati.Utente) {
		f.esitoDellaClip(w, r, esitoClip{Tipo: "error", Messaggio: assist.MessaggioDiTrascrizione(assist.ErrTrascrittore{})})
		return
	}
	defer f.clip.lascia(st.dati.Utente)
	audio, tipo, err := []byte(nil), "application/octet-stream", error(nil)
	if corpo := fileDelModulo(r); corpo != nil {
		// Token e sessione sono a posto: da qui il tempo è per pezzo, non per tutto il corpo.
		corpo.lettore.alPezzo = allungaLettura(w, f.tempoPezzo)
		audio, tipo, err = riceviClip(corpo)
	}
	defer clear(audio) // la voce del cliente non resta in memoria più del necessario
	if err != nil {
		if errors.Is(err, errClipGrande) {
			TroppoGrande(w, r)
			return
		}
		erroreDelCorpo(w, r, err)
		return
	}
	// Il corpo è finito: la scadenza dell'ultimo pezzo non vale più. Lasciata lì, a lettura di fondo del server la
	// vedrebbe scadere durante l'attesa del trascrittore e cancellerebbe la richiesta (revisione della 109): l'attesa
	// la tiene assist (35 s), un browser che se ne va si vede ancora dalla connessione chiusa.
	_ = http.NewResponseController(w).SetReadDeadline(time.Time{}) // un registratore dei test non ha scadenze
	// La clip è arrivata: la sessione si ricontrolla prima di mandarla al trascrittore (get_session_user).
	_, ok, err := f.utenti.DiSessione(r.Context(), st.dati.Utente, st.dati.Versione)
	if err != nil {
		f.registro.Error("sessione non riletta")
		Errore(w, r)
		return
	}
	if !ok {
		f.fuoriPerLeFoto(w, r)
		return
	}
	var t assist.Trascrizione
	err = assist.ErrTrascrizioneNonConfigurata
	if f.assistente != nil {
		t, err = f.assistente.Trascrivi(r.Context(), audio, tipo, contestoDi(r))
	}
	esito := esitoClip{Tipo: "ok", Messaggio: t.Messaggio(), Testo: t.Testo}
	switch messaggio := assist.MessaggioDiTrascrizione(err); {
	case err != nil && messaggio == "":
		// Non è un rifiuto della trascrizione: il database (configurazione) non risponde.
		if !errors.Is(err, context.Canceled) {
			f.registro.Error("dettatura: trascrizione non riuscita")
		}
		Errore(w, r)
		return
	case err != nil:
		esito = esitoClip{Tipo: "error", Messaggio: messaggio}
	case t.NienteVoce:
		esito.Tipo = "error"
	}
	f.esitoDellaClip(w, r, esito)
}

// esitoDellaClip scrive la risposta JSON, 200 e mai in cache.
func (f *facciaWeb) esitoDellaClip(w http.ResponseWriter, r *http.Request, esito esitoClip) {
	b, _ := json.Marshal(esito) // una struttura di testi: Marshal non fallisce
	w.Header().Set("Cache-Control", "no-store")
	f.jsonCoda(w, r, http.StatusOK, b)
}

// leggiClip legge la parte della clip in un buffer che cresce raddoppiando fino a assist.ClipMassima+1 byte:
// ogni buffer lasciato indietro si azzera (io.ReadAll lascerebbe le sue copie intermedie piene di voce). Oltre
// assist.ClipMassima è errClipGrande; con un errore torna quello che ha letto, da azzerare.
func leggiClip(p io.Reader) ([]byte, error) {
	buf := make([]byte, 0, 64<<10)
	for {
		if len(buf) == cap(buf) {
			if len(buf) > assist.ClipMassima {
				return buf, errClipGrande
			}
			nuovo := make([]byte, len(buf), min(2*cap(buf), assist.ClipMassima+1))
			copy(nuovo, buf)
			clear(buf)
			buf = nuovo
		}
		n, err := p.Read(buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]
		switch {
		case len(buf) > assist.ClipMassima:
			return buf, errClipGrande
		case errors.Is(err, io.EOF):
			return buf, nil
		case err != nil:
			return buf, err
		}
	}
}

// riceviClip legge la clip del modulo: la prima parte con un file dev'essere il campo «dictation» e l'ultima del
// corpo. Dà i byte e il tipo dichiarato; senza nessun file una clip vuota.
func riceviClip(c *corpoConFile) ([]byte, string, error) {
	p := c.primo
	if p == nil {
		return nil, "application/octet-stream", nil
	}
	if p.FormName() != campoClip {
		return nil, "", errCorpoMalformato
	}
	audio, err := leggiClip(p)
	if err != nil {
		return audio, "", err
	}
	tipo := p.Header.Get("Content-Type")
	if tipo == "" {
		tipo = "application/octet-stream"
	}
	if _, err := c.mr.NextPart(); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errCorpoMalformato
		}
		return audio, "", err
	}
	return audio, tipo, nil
}
