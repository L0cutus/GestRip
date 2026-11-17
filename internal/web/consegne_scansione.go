package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
)

// Lo scanner continuo della sessione di consegna (slice 079, ART-09; quello del lotto, ORE-07, è in lotto_scansione.go): «📷 Scansiona» della pagina della
// sessione apre il lettore QR di app.js (quello della home e di «Nuova riparazione», nel modo continuo) e
// ogni codice letto arriva qui, come l'evento add_scanned di HandoverSessionLive. Si prova prima come QR e
// poi come numero (buste.LetturaScansione, l'ordine opposto al campo scritto); solo «non trovata» passa alla
// seconda lettura. La risposta è la reply dello standalone, {ok, message, count}, con info in più: ok solo per
// una busta appena aggiunta (il suono di successo), message il testo della pagina («✓ Aggiunta busta …», «↻
// Busta … già in lista» o il rifiuto con il codice), count le buste in consegna rilette adesso (anche dopo un
// rifiuto); info dice se il messaggio è d'informazione (put_flash :info: aggiunta e «già in lista») o un
// errore: alla chiusura della finestra la pagina mostra solo l'ultimo errore (il flash verde dello standalone
// si chiude da sé), e senza info «già in lista» (ok falso) non si distinguerebbe da un rifiuto. «Già in lista» si
// decide sulla consegna riletta nella transazione: una busta aggiunta da un altro telefono è una ripetizione.
// Niente messaggio nella sessione (la finestra copre la pagina: lo mette app.js quando la rilegge) e
// nessuna riga d'audit, come l'aggiunta dal campo. Il campo «Numero busta» non si tocca: la pagina lo tiene.

// esitoDellaScansione è la risposta di POST /handovers/{id}/scansione.
type esitoDellaScansione struct {
	Ok      bool   `json:"ok"`
	Message string `json:"message"`
	Count   int    `json:"count"`
	Info    bool   `json:"info"`
}

// codiceLetto è il parametro qr di add_scanned: senza spazi ai bordi, "" se manca o se non è un testo (un
// «qr[…]» che Plug farebbe mappa o lista).
func codiceLetto(r *http.Request) string {
	for k := range r.PostForm {
		if strings.HasPrefix(k, "qr[") {
			return ""
		}
	}
	v, _ := ultimo(r, "qr")
	return strings.TrimSpace(v)
}

// scansionaBusta è l'evento add_scanned. Un id che non è un numero positivo è 404 (la pagina non lo
// manda mai); una consegna che non c'è o chiusa è il rifiuto del dominio, come lo standalone.
func (f *facciaWeb) scansionaBusta(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	codice := codiceLetto(r)
	a, err := f.buste.AggiungiAllaConsegna(r.Context(), id, codice, buste.LetturaScansione)
	var e esitoDellaScansione
	var rifiuto *buste.RifiutoAggiunta
	switch {
	case err == nil && a.GiaInLista:
		e = esitoDellaScansione{Message: "↻ Busta " + a.Busta.Visibile().Titolo() + " già in lista", Info: true}
	case err == nil:
		e = esitoDellaScansione{Ok: true, Message: "✓ Aggiunta busta " + a.Busta.Visibile().Titolo(), Info: true}
	case errors.As(err, &rifiuto):
		e = esitoDellaScansione{Message: err.Error()}
	default:
		f.registro.Error("busta scansionata non aggiunta alla consegna", "errore", err.Error())
		Errore(w, r)
		return
	}
	bb, err := f.buste.BusteDellaConsegna(r.Context(), id)
	if err != nil {
		f.registro.Error("buste della consegna non lette", "errore", err.Error())
		Errore(w, r)
		return
	}
	e.Count = len(bb)
	f.rispondiEsito(w, r, e)
}

// rispondiEsito è la risposta JSON di ogni scanner continuo (la consegna, il lotto): l'esito, mai in cache.
func (f *facciaWeb) rispondiEsito(w http.ResponseWriter, r *http.Request, e esitoDellaScansione) {
	corpo, _ := json.Marshal(e) // una struttura fissa: Marshal non fallisce
	w.Header().Set("Cache-Control", "no-store")
	f.jsonCoda(w, r, http.StatusOK, corpo)
}
