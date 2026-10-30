package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/clientip"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// Il limite ai tentativi falliti (CLI-21, ACC-20), come la slice 208 dello standalone; misurato
// sul banco contro go-baseline il 30/10/2026 (ricevuta della 009).

// famiglia è una famiglia di percorsi pubblici che conta i suoi 404, con il rifiuto delle sue
// rotte.
type famiglia struct {
	secchio ratelimit.Secchio
	testo   string // corpo del 404 delle sue rotte
	robots  bool   // X-Robots-Tag: le pagine delle vendite (/w e /g)
}

const (
	rifiutoRiparazione = "Riparazione non disponibile"
	rifiutoDocumento   = "Documento non disponibile"
	rifiutoLotto       = "Lotto non disponibile"
)

// famigliaDi è PublicAttempts.family/1: le pagine di /stato/<busta>/<anno> e tutto quello che sta
// sotto nel secchio corto; le pagine a token lungo nel secchio dei token; **le foto no** (un 404 di
// foto è normale, e alla foto si arriva da una pagina che ha già contato il suo token), né le
// conferme email (/stato/email-…, due segmenti).
func famigliaDi(percorso string) (famiglia, bool) {
	var s []string
	for _, x := range strings.Split(percorso, "/") {
		if x != "" {
			s = append(s, x)
		}
	}
	return famigliaDiSegmenti(s)
}

// famigliaDiRichiesta legge i segmenti come il mux: il percorso codificato diviso sulle «/», poi
// ogni segmento decodificato da solo, così un «%2F» resta dentro il suo segmento (come
// path_info nello standalone).
func famigliaDiRichiesta(r *http.Request) (famiglia, bool) {
	var s []string
	for _, x := range strings.Split(r.URL.EscapedPath(), "/") {
		if x == "" {
			continue
		}
		if d, err := url.PathUnescape(x); err == nil {
			x = d
		}
		s = append(s, x)
	}
	return famigliaDiSegmenti(s)
}

func famigliaDiSegmenti(s []string) (famiglia, bool) {
	if len(s) == 0 {
		return famiglia{}, false
	}
	n := len(s)
	riparazione := famiglia{ratelimit.Token, rifiutoRiparazione, false}
	documento := famiglia{ratelimit.Token, rifiutoDocumento, true}
	switch s[0] {
	case "stato":
		if n >= 3 {
			return famiglia{ratelimit.Corto, rifiutoRiparazione, false}, true
		}
	case "r":
		if n == 4 && s[2] == "foto" {
			return famiglia{}, false
		}
		if n >= 2 {
			return riparazione, true
		}
	case "lotto":
		if n == 2 {
			return famiglia{ratelimit.Token, rifiutoLotto, false}, true
		}
	case "artigiano":
		if (n == 4 && s[2] == "foto") || (n == 5 && s[3] == "foto") {
			return famiglia{}, false
		}
		return riparazione, true
	case "w":
		if (n == 4 && s[2] == "photos") || (n == 5 && s[2] == "current" && s[3] == "photos") {
			return famiglia{}, false
		}
		return documento, true
	case "g":
		if n == 4 && s[2] == "foto" {
			return famiglia{}, false
		}
		return documento, true
	}
	return famiglia{}, false
}

// RifiutoPubblico è il 404 di una rotta pubblica, misurato sullo standalone: il testo da solo,
// senza Content-Type, no-store e no-referrer con le intestazioni di sicurezza delle pagine;
// X-Robots-Tag sulle pagine delle vendite. Le rotte pubbliche rifiutano **solo** da qui, così il
// 404 di un indirizzo bloccato è per costruzione identico a quello di un token che non c'è (D-05).
func RifiutoPubblico(w http.ResponseWriter, r *http.Request, testo string, robots bool) {
	k := w.Header()
	k.Set("Cache-Control", "no-store")
	k.Set("Content-Security-Policy", "base-uri 'self'; frame-ancestors 'self';")
	k.Set("Referrer-Policy", "no-referrer")
	k.Set("X-Content-Type-Options", "nosniff")
	k.Set("X-Permitted-Cross-Domain-Policies", "none")
	if robots {
		k.Set("X-Robots-Tag", "noindex, nofollow")
	}
	k["Content-Type"] = nil // net/http non ne indovina uno: lo standalone non lo manda
	w.WriteHeader(http.StatusNotFound)
	if r.Method != http.MethodHead {
		_, _ = w.Write([]byte(testo))
	}
}

// limitePubblico è PublicAttempts: prima della scelta della rotta. Si prenota un posto prima che la
// rotta verifichi il codice o il token (slice 219): un indirizzo bloccato, o con i posti pieni,
// riceve il rifiuto della famiglia per ogni percorso della famiglia, buono o no, e la rotta non
// lavora; se no, ogni 404 della famiglia (anche di un percorso che nessuna rotta serve) rende il
// posto un fallito, e ogni altra risposta lo restituisce. La famiglia si legge dai segmenti
// decodificati, quelli della rotta instradata.
//
// Manca la riga d'audit `security.attempts_blocked` all'inizio del blocco: arriva con l'audit
// (ADM-16).
func limitePubblico(l *ratelimit.Limitatore) filtro {
	return func(w http.ResponseWriter, r *http.Request, avanti http.HandlerFunc) {
		f, ok := famigliaDiRichiesta(r)
		if !ok {
			avanti(w, r)
			return
		}
		ip := clientip.Di(r)
		b, attesa := l.Prenota(f.secchio, ip)
		if attesa > 0 {
			RifiutoPubblico(w, r, f.testo, f.robots)
			return
		}
		// Un gestore che va in panic restituisce il posto, come la pagina d'errore dello
		// standalone (before_send gira anche lì).
		sistemato := false
		defer func() {
			if !sistemato {
				l.Rilascia(f.secchio, ip, b)
			}
		}()
		c := &conCodice{ResponseWriter: w}
		avanti(c, r)
		sistemato = true
		if c.codice() == http.StatusNotFound {
			l.Fallito(f.secchio, ip, b)
		} else {
			l.Rilascia(f.secchio, ip, b)
		}
	}
}

// Login (ACC-20): 5 falliti per indirizzo e account e 20 per indirizzo in 15 minuti.

// chiaveAccount è l'indirizzo con l'email come la cerca il login (normalizzata, al massimo 254
// caratteri, come String.slice/3 nello standalone).
func chiaveAccount(ip, email string) string {
	e := []rune(accounts.NormalizzaEmail(email))
	if len(e) > 254 {
		e = e[:254]
	}
	return ip + "\x00" + string(e)
}

// attesaLogin è il blocco più lungo fra indirizzo e account; 0 se nessuno dei due è bloccato.
func (f *facciaWeb) attesaLogin(ip, account string) time.Duration {
	return max(f.limiti.Bloccato(ratelimit.LoginIndirizzo, ip), f.limiti.Bloccato(ratelimit.LoginAccount, account))
}

// msgTroppi è il 429 dello standalone: i minuti che mancano, arrotondati in su (div(left +
// 59_999, 60_000)), anche «1 minuti».
func msgTroppi(attesa time.Duration) string {
	ms := int64(attesa / time.Millisecond)
	return fmt.Sprintf("Troppi tentativi, riprova fra %d minuti.", (ms+59_999)/60_000)
}
