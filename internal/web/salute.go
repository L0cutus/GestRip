// Package web contiene le due facce HTTP di GestRip: interna (Tailscale) e pubblica (Funnel).
// Ognuna ha il suo elenco di rotte (PIANO §1.5).
package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// Sonda dice se l'app è pronta a servire (il database risponde).
type Sonda interface {
	Pronto(ctx context.Context) bool
}

// Salute è `GET /healthz` (ADM-20): 200 «ok» o 503 «unavailable», sempre no-store, nessuna
// sessione, nessun log.
func Salute(s Sonda) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Type", "text/plain; charset=utf-8")
		h.Set("Cache-Control", "no-store")
		if s.Pronto(r.Context()) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok")) // se il client se n'è andato non c'è niente da fare
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("unavailable"))
	})
}

// Dipendenze sono quello che le facce ricevono da chi le avvia. Le chiavi sono sottochiavi
// della chiave del server (db.SottoChiave), mai la chiave stessa.
type Dipendenze struct {
	Sonda          Sonda
	Registro       *slog.Logger
	Utenti         Utenti
	ChiaveSessione []byte                // db.SottoChiave(chiave, "sessione")
	ChiaveCSRF     []byte                // db.SottoChiave(chiave, "csrf")
	Orologio       clock.Orologio        // scadenza delle sessioni (D-31)
	Limiti         *ratelimit.Limitatore // tentativi falliti (login e rotte pubbliche), uno per processo
	Audit          Registro              // righe d'audit fuori da una transazione (inizio di un blocco)
	Recupero       RecuperoPassword      // password dimenticata del titolare (faccia interna)
	Inviti         Inviti                // inviti degli account (faccia interna)
	PrimoTitolare  PrimoTitolare         // il primo titolare su un database vuoto (faccia interna)
	Consultazione  Consultazione         // pagina del registro (faccia interna)
	Buste          Buste                 // la nuova busta (faccia interna)
	// IndirizzoInterno è l'indirizzo della faccia interna dal file dell'apparecchio (011b), su cui
	// si costruiscono i link d'invito; vuoto se non è configurato.
	IndirizzoInterno string
	// IndirizzoPubblico è l'indirizzo della faccia pubblica dallo stesso file (slice 031), su cui si
	// costruisce il link cliente /r/<token> di una busta nuova; vuoto se non è configurato.
	IndirizzoPubblico string
	// Configurazione è il percorso di quel file, citato dalle pagine quando l'indirizzo manca.
	Configurazione string
}

// facciaWeb tiene quello che serve alle rotte di una faccia.
type facciaWeb struct {
	utenti        Utenti
	sessioni      *sessioni
	csrf          *CSRF
	orologio      clock.Orologio
	limiti        *ratelimit.Limitatore
	audit         Registro
	recupero      RecuperoPassword
	inviti        Inviti
	primo         PrimoTitolare
	interno       string // indirizzo della faccia interna per i link, "" se non configurato
	config        string // percorso del file dell'apparecchio, citato quando interno manca
	consultazione Consultazione
	buste         Buste
	pubblico      string         // indirizzo della faccia pubblica per il link cliente, "" se non configurato
	esiti         *depositoEsiti // esiti e link delle buste appena create, e invii già fatti (esiti.go)
	registro      *slog.Logger
	rotte         []rottaDichiarata    // le rotte registrate con il loro permesso (ruoli.go)
	menu          []voceMenu           // le voci del menu che hanno la loro rotta (guscio.go)
	schede        []schedaImpostazioni // le schede delle impostazioni che hanno la loro rotta
}

func nuovaFaccia(d Dipendenze, nome string) (*facciaWeb, error) {
	if len(d.ChiaveSessione) < 32 {
		return nil, errors.New("web: chiave di sessione più corta di 32 byte")
	}
	if d.Orologio == nil {
		return nil, errors.New("web: manca l'orologio")
	}
	if d.Limiti == nil {
		return nil, errors.New("web: manca il limitatore dei tentativi")
	}
	if d.Audit == nil {
		return nil, errors.New("web: manca il registro delle azioni")
	}
	registro := d.Registro
	if registro == nil {
		registro = slog.New(slog.DiscardHandler)
	}
	c, err := NuovoCSRF(d.ChiaveCSRF, nome)
	if err != nil {
		return nil, err
	}
	return &facciaWeb{utenti: d.Utenti, sessioni: sessioniPer(d.ChiaveSessione, nome), csrf: c, orologio: d.Orologio, limiti: d.Limiti,
		audit: d.Audit, recupero: d.Recupero, inviti: d.Inviti, primo: d.PrimoTitolare, interno: d.IndirizzoInterno, config: d.Configurazione, consultazione: d.Consultazione, buste: d.Buste, pubblico: d.IndirizzoPubblico, esiti: nuovoDepositoEsiti(d.Orologio), registro: registro}, nil
}

// Interna è la faccia interna (gestione), solo nella tailnet: /login e il reset stanno solo qui
// (D-28). Ogni rotta si registra con chi la può usare (ruoli.go).
func Interna(d Dipendenze) (http.Handler, error) {
	h, _, err := costruisciInterna(d, nil)
	return h, err
}

// costruisciInterna fa la faccia interna; extra aggiunge rotte (solo i test, per provare i
// permessi su rotte che il Go non ha ancora).
func costruisciInterna(d Dipendenze, extra func(*facciaWeb, *http.ServeMux)) (http.Handler, *facciaWeb, error) {
	f, err := nuovaFaccia(d, "interna")
	if err != nil {
		return nil, nil, err
	}
	if d.Recupero == nil {
		return nil, nil, errors.New("web: manca il recupero della password")
	}
	if d.Inviti == nil {
		return nil, nil, errors.New("web: mancano gli inviti")
	}
	if d.PrimoTitolare == nil {
		return nil, nil, errors.New("web: manca il primo titolare")
	}
	if d.Buste == nil {
		return nil, nil, errors.New("web: mancano le buste")
	}
	mux := http.NewServeMux()
	f.speciale(mux, rottaSalute, aperta, Salute(d.Sonda))
	f.rotta(mux, "GET /login", aperta, func(w http.ResponseWriter, r *http.Request) {
		f.seFuori(http.HandlerFunc(f.paginaLogin)).ServeHTTP(w, r)
	})
	// Chi ha già una sessione valida va a / prima del controllo CSRF, come lo standalone (dove il
	// token non cambia al login): una seconda scheda con il modulo di prima non dà 403. È solo un
	// redirect, nessuna azione.
	f.speciale(mux, "POST /login", aperta, f.conSessione(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if statoDi(r).utente != nil {
			Pagina(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.vaiA(w, r, "/") })).ServeHTTP(w, r)
			return
		}
		f.csrf.Modulo(Pagina(http.HandlerFunc(f.entra))).ServeHTTP(w, r)
	})))
	f.rotta(mux, "POST /logout", aperta, f.esci)
	f.rottaConGuscio(mux, "GET /{$}", personale, f.home)
	f.rottaConGuscio(mux, "GET /nuova", personale, f.paginaNuova)
	f.rottaConGuscio(mux, "POST /nuova", personale, f.generaBusta)
	f.rottaConGuscio(mux, "GET /repairs/{id}", personale, f.paginaScheda)
	f.rottaConGuscio(mux, "POST /repairs/{id}/modifica", personale, f.salvaModifica)
	f.rotta(mux, "POST /repairs/{id}/togli-data", personale, f.togliData)
	f.rotta(mux, "POST /repairs/{id}/stato", personale, f.cambiaStato)
	f.rottaConGuscio(mux, "POST /repairs/{id}/riapri", personale, f.riapriBusta)
	f.rotta(mux, "POST /repairs/{id}/attesa", personale, f.attesaBusta)
	f.azioneDelTitolare(mux, "POST /repairs/{id}/revoca-link", msgSoloTitolareRevo, tornaAllaScheda, f.revocaLink)
	f.azioneDelTitolare(mux, "POST /repairs/{id}/nuovo-link", msgSoloTitolareNuov, tornaAllaScheda, f.nuovoLink)
	f.rotta(mux, "GET /reset-request", aperta, f.paginaRichiestaReset)
	f.rotta(mux, "POST /reset-request", aperta, f.richiediReset)
	f.rotta(mux, "GET /reset/{token}", aperta, f.paginaNuovaPassword)
	f.rotta(mux, "POST /reset/{token}", aperta, f.reimposta)
	f.rottaConGuscio(mux, "GET /audit", soloTitolare, f.paginaRegistro)
	f.rottaConGuscio(mux, "GET /settings/utenti", soloTitolare, f.paginaUtenti)
	f.rotta(mux, "POST /settings/utenti/{id}/disattiva", soloTitolare, f.disattivaUtente)
	f.rotta(mux, "POST /settings/utenti/{id}/ruolo", soloTitolare, f.ruoloUtente)
	f.rottaConGuscio(mux, "POST /settings/utenti/invita", soloTitolare, f.invitaUtente)
	f.rottaConGuscio(mux, "POST /settings/utenti/{id}/nuovo-link", soloTitolare, f.nuovoLinkUtente)
	f.rotta(mux, "GET /invito/{token}", aperta, f.paginaInvito)
	f.rotta(mux, "POST /invito/{token}", aperta, f.accettaInvito)
	f.rotta(mux, "GET /configura/{codice}", aperta, f.paginaConfigura)
	f.rotta(mux, "POST /configura/{codice}", aperta, f.configura)
	f.registraGuscio(mux)
	if extra != nil {
		extra(f, mux)
	}
	f.preparaGuscio()
	return faccia(mux, d.Registro), f, nil
}

// Pubblica è la faccia pubblica (clienti, artigiani, acquirenti), dietro Tailscale Funnel. Ogni
// richiesta passa dal limite ai tentativi falliti (limiti.go).
func Pubblica(d Dipendenze) (http.Handler, error) {
	h, _, err := costruisciPubblica(d, nil)
	return h, err
}

// costruisciPubblica fa la faccia pubblica; extra aggiunge rotte (solo i test, finché le rotte
// pubbliche vere non ci sono). Ogni rotta si registra con f.rottaPubblica, o f.speciale per quelle
// fuori dal pipeline :browser (/healthz), così sta nell'elenco f.rotte.
func costruisciPubblica(d Dipendenze, extra func(*facciaWeb, *http.ServeMux)) (http.Handler, *facciaWeb, error) {
	f, err := nuovaFaccia(d, "pubblica")
	if err != nil {
		return nil, nil, err
	}
	mux := http.NewServeMux()
	f.speciale(mux, rottaSalute, aperta, Salute(d.Sonda)) // nessuna sessione né intestazioni di pagina
	if extra != nil {
		extra(f, mux)
	}
	return facciaConFiltro(mux, d.Registro, limitePubblico(d.Limiti, f.registraBlocco)), f, nil
}
