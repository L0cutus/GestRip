package web

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
)

// La sessione sta in un cookie per faccia, firmato con la sottochiave «sessione» e con il nome
// della faccia (lo stesso host ha le due facce su porte diverse, e i cookie non distinguono le
// porte). Attributi come il cookie dello standalone: Path=/, Secure, HttpOnly, SameSite=Lax,
// senza scadenza nel browser. La chiudono l'uscita, la disattivazione dell'account e «chiudi
// tutte» (versione_sessione), ricontrollati a ogni richiesta (ACC-04); in più, solo nel Go, la
// sessione vale al massimo 30 giorni dall'accesso, una durata assoluta che l'uso non rinnova
// (D-31, Stefano 30/10/2026). La scadenza la controlla il server: il cookie resta senza Max-Age,
// come quello dello standalone.

// datiSessione è il contenuto del cookie.
type datiSessione struct {
	Utente   int64  `json:"u,omitempty"`
	Versione int64  `json:"v,omitempty"`
	Ritorno  string `json:"r,omitempty"` // la pagina chiesta prima del login (ACC-02)
	Errore   string `json:"e,omitempty"` // messaggio da mostrare una volta, dopo un redirect
	Info     string `json:"i,omitempty"` // messaggio di esito (flash :info), una volta
	// Casuale a ogni accesso: due accessi, anche dello stesso account, non danno mai lo stesso
	// cookie.
	Nonce string `json:"n,omitempty"`
	// Istante dell'accesso, in secondi Unix (D-31).
	Accesso int64 `json:"a,omitempty"`
}

// durataSessione è la durata massima di una sessione dall'accesso (D-31): 30 giorni di 24 ore,
// anche a cavallo del cambio dell'ora.
const durataSessione = 30 * 24 * time.Hour

type sessioni struct {
	chiave []byte
	faccia string
	cookie string
}

func sessioniPer(chiave []byte, faccia string) *sessioni {
	return &sessioni{chiave: chiave, faccia: faccia, cookie: "__Host-gestrip_sessione_" + faccia}
}

func (s *sessioni) firma(dati string) string {
	m := hmac.New(sha256.New, s.chiave)
	m.Write([]byte("gestrip sessione " + s.faccia + "\x00" + dati))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// leggi dà il contenuto del cookie; vuoto se manca, è malformato o la firma non è di questa
// faccia.
func (s *sessioni) leggi(r *http.Request) datiSessione {
	c, err := r.Cookie(s.cookie)
	if err != nil {
		return datiSessione{}
	}
	dati, firma, ok := strings.Cut(c.Value, ".")
	if !ok || !hmac.Equal([]byte(firma), []byte(s.firma(dati))) {
		return datiSessione{}
	}
	j, err := base64.RawURLEncoding.DecodeString(dati)
	var d datiSessione
	if err != nil || json.Unmarshal(j, &d) != nil {
		return datiSessione{}
	}
	return d
}

func (s *sessioni) scrivi(w http.ResponseWriter, d datiSessione) {
	// Senza escape HTML: json.Marshal porterebbe ogni «&» del ritorno a sei byte, e un cookie
	// oltre 4096 byte il browser lo scarta.
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(d) // una struttura di campi semplici: non fallisce
	dati := base64.RawURLEncoding.EncodeToString(bytes.TrimSuffix(b.Bytes(), []byte("\n")))
	http.SetCookie(w, &http.Cookie{Name: s.cookie, Value: dati + "." + s.firma(dati),
		Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func (s *sessioni) cancella(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: s.cookie, Value: "", Path: "/", MaxAge: -1,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

// stato è la sessione della richiesta in corso: i dati del cookie e l'account, se c'è ed è
// ancora valido.
type stato struct {
	dati     datiSessione
	utente   *accounts.Utente
	cambiata bool
	// inizioCatena è l'inizio della catena dei segni della scheda di questa richiesta (segni.go):
	// l'istante dell'ultima riga, ereditato dal segno consumato o adesso per una riga appena
	// scritta; zero se non c'è.
	inizioCatena time.Time
}

type chiaveStato struct{}

func statoDi(r *http.Request) *stato {
	st, _ := r.Context().Value(chiaveStato{}).(*stato)
	return st
}

// conSessione legge la sessione e ricontrolla l'account a ogni richiesta (ACC-04): deve essere
// ancora attivo, con la stessa versione di sessione e un accesso di meno di 30 giorni fa (D-31),
// altrimenti la richiesta è come senza accesso.
func (f *facciaWeb) conSessione(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := &stato{dati: f.sessioni.leggi(r)}
		if st.dati.Utente > 0 {
			ok := f.inTempo(st.dati.Accesso)
			var u accounts.Utente
			if ok {
				var err error
				u, ok, err = f.utenti.DiSessione(r.Context(), st.dati.Utente, st.dati.Versione)
				if err != nil {
					Errore(w, r)
					return
				}
			}
			if ok {
				st.utente = &u
			} else {
				st.dati.Utente, st.dati.Versione, st.dati.Accesso, st.cambiata = 0, 0, 0, true
			}
		}
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), chiaveStato{}, st)))
	})
}

// tolleranzaOrologio: un accesso fino a 5 minuti nel futuro vale ancora (l'orologio ritoccato da
// NTP); oltre, l'orologio era avanti all'accesso e la sessione durerebbe 30 giorni più l'errore.
const tolleranzaOrologio = 5 * time.Minute

// inTempo dice se una sessione aperta all'istante accesso (secondi Unix) vale ancora (D-31). Un
// cookie senza istante (0) è scaduto da sé: vale come un accesso del 1970.
func (f *facciaWeb) inTempo(accesso int64) bool {
	adesso := f.orologio.Adesso().Unix()
	return adesso < accesso+int64(durataSessione/time.Second) && accesso <= adesso+int64(tolleranzaOrologio/time.Second)
}

// salva scrive il cookie se la sessione è cambiata; va chiamato prima di scrivere la risposta.
func (f *facciaWeb) salva(w http.ResponseWriter, st *stato) {
	if st.cambiata {
		f.sessioni.scrivi(w, st.dati)
		st.cambiata = false
	}
}

// Errore è il 500 dello standalone: «Internal Server Error».
func Errore(w http.ResponseWriter, r *http.Request) {
	paginaErrore(w, r, http.StatusInternalServerError, "Internal Server Error")
}
