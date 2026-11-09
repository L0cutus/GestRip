// Package web contiene le due facce HTTP di GestRip: interna (Tailscale) e pubblica (Funnel).
// Ognuna ha il suo elenco di rotte (PIANO §1.5).
package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/printing"
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
	ChiaveGalleria []byte                // db.SottoChiave(chiave, "galleria"), gettoni delle miniature (faccia interna)
	Orologio       clock.Orologio        // scadenza delle sessioni (D-31)
	Limiti         *ratelimit.Limitatore // tentativi falliti (login e rotte pubbliche), uno per processo
	Audit          Registro              // righe d'audit fuori da una transazione (inizio di un blocco)
	Recupero       RecuperoPassword      // password dimenticata del titolare (faccia interna)
	Inviti         Inviti                // inviti degli account (faccia interna)
	PrimoTitolare  PrimoTitolare         // il primo titolare su un database vuoto (faccia interna)
	Consultazione  Consultazione         // pagina del registro (faccia interna)
	Buste          Buste                 // le buste (faccia interna) e la pagina del cliente (faccia pubblica)
	// IndirizzoInterno è l'indirizzo della faccia interna dal file dell'apparecchio (011b), su cui
	// si costruiscono i link d'invito; vuoto se non è configurato.
	IndirizzoInterno string
	// IndirizzoPubblico è l'indirizzo della faccia pubblica dallo stesso file (slice 031), su cui si
	// costruisce il link cliente /r/<token> di una busta nuova; vuoto se non è configurato.
	IndirizzoPubblico string
	// Configurazione è il percorso di quel file, citato dalle pagine quando l'indirizzo manca.
	Configurazione string
	// Caricamenti è la cartella privata dei file delle foto in arrivo dalla scheda (slice 038),
	// accanto al database (sul volume cifrato); vuota: le foto non si caricano dalla scheda.
	Caricamenti string
	// Stampanti è la stampa diretta (slice 046): CUPS in serve, la finta nei test; nil: niente
	// stampa diretta (le pagine non mostrano i pulsanti, le impostazioni lo dicono).
	Stampanti printing.Printer
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
	pubblico      string          // indirizzo della faccia pubblica per il link cliente, "" se non configurato
	tempoPezzo    time.Duration   // il tempo di ogni pezzo di un caricamento (foto_scheda.go)
	tempoCoda     time.Duration   // il tempo di ogni pezzo di una foto della coda (coda_foto.go)
	caricamenti   string          // cartella dei file delle foto in arrivo, "" se non configurata (foto_scheda.go)
	esiti         *depositoEsiti  // esiti e link delle buste appena create, e invii già fatti (esiti.go)
	segni         *depositoSegni  // i segni della scheda mostrata senza un'apertura (segni.go)
	galleria      gettoneGalleria // i gettoni delle miniature della galleria (foto.go)
	registro      *slog.Logger
	rotte         []rottaDichiarata    // le rotte registrate con il loro permesso (ruoli.go)
	menu          []voceMenu           // le voci del menu che hanno la loro rotta (guscio.go)
	schede        []schedaImpostazioni // le schede delle impostazioni che hanno la loro rotta
	stampanti     printing.Printer     // la stampa diretta, nil se non c'è (stampanti.go)
	inCorso       *stampeInCorso       // i lavori di stampa che non hanno ancora risposto
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
		audit: d.Audit, recupero: d.Recupero, inviti: d.Inviti, primo: d.PrimoTitolare, interno: d.IndirizzoInterno, config: d.Configurazione, consultazione: d.Consultazione, buste: d.Buste, pubblico: d.IndirizzoPubblico, caricamenti: d.Caricamenti, tempoPezzo: tempoPezzo, tempoCoda: tempoPezzoCoda, esiti: nuovoDepositoEsiti(d.Orologio), segni: nuovoDepositoSegni(d.Orologio), galleria: gettoneGalleria{chiave: d.ChiaveGalleria}, registro: registro,
		stampanti: d.Stampanti, inCorso: nuoveStampeInCorso()}, nil
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
	if len(d.ChiaveGalleria) < 32 {
		return nil, nil, errors.New("web: chiave della galleria più corta di 32 byte")
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
	f.rotta(mux, "GET /cerca", personale, f.cerca)
	f.rottaConGuscio(mux, "POST /cerca-qr", personale, f.cercaQR)
	f.rottaConGuscio(mux, "POST /repairs/{id}/qr-avanza", personale, f.qrAvanza)
	f.rottaConGuscio(mux, "POST /repairs/{id}/qr-preventivo", personale, f.qrPreventivo)
	f.rottaConGuscio(mux, "POST /repairs/{id}/rapido", personale, f.statoRapido) // la conferma senza JavaScript (slice 060)
	f.rotta(mux, "POST /repairs/{id}/stato-lista", personale, f.statoDallaLista)
	f.rotta(mux, "POST /repairs/{id}/pin", personale, f.cambiaPin)
	f.rottaConGuscio(mux, "POST /nuova", personale, f.generaBusta)
	f.rottaConGuscio(mux, "GET /repairs/{id}", personale, f.paginaScheda)
	f.rottaConGuscio(mux, "POST /repairs/{id}/modifica", personale, f.salvaModifica)
	f.rotta(mux, "POST /repairs/{id}/togli-data", personale, f.togliData)
	f.rotta(mux, "POST /repairs/{id}/stato", personale, f.cambiaStato)
	f.rottaConGuscio(mux, "POST /repairs/{id}/riapri", personale, f.riapriBusta)
	f.rotta(mux, "POST /repairs/{id}/attesa", personale, f.attesaBusta)
	f.rotta(mux, rottaFoto, personale, f.paginaFoto)
	f.rottaConFile(mux, "POST /repairs/{id}/foto", personale, limiteCaricamento, f.caricaFoto)
	f.rotta(mux, "POST /repairs/{id}/foto/{foto}/visibilita", personale, f.visibilitaFoto)
	f.rotta(mux, "POST /repairs/{id}/foto/{foto}/elimina", personale, f.eliminaFoto)
	f.rotta(mux, "POST /repairs/{id}/photos", personale, f.caricaFotoCoda)
	f.rotta(mux, "GET /repairs/{id}/receipt.pdf", personale, f.ricevuta)
	f.rotta(mux, "GET /repairs/{id}/label.pdf", personale, f.etichetta)
	f.rotta(mux, "GET /repairs/{id}/scheda.pdf", personale, f.schedaPDF)
	f.rotta(mux, "GET /repairs/report.pdf", personale, f.report)
	f.rottaConGuscio(mux, "GET /repairs/{id}/firma", personale, f.paginaFirma)
	f.rotta(mux, "POST /repairs/{id}/firma", personale, f.salvaFirma)
	f.rotta(mux, rottaFirmaPNG, personale, f.immagineFirma)
	f.rotta(mux, "POST /repairs/{id}/firma/reset", soloTitolare, f.azzeraFirma)
	f.azioneDelTitolare(mux, "POST /repairs/{id}/revoca-link", msgSoloTitolareRevo, f.tornaAllaScheda, f.revocaLink)
	f.azioneDelTitolare(mux, "POST /repairs/{id}/nuovo-link", msgSoloTitolareNuov, f.tornaAllaScheda, f.nuovoLink)
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
	f.registraEtichette(mux)
	f.registraStampanti(mux)
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

// costruisciPubblica fa la faccia pubblica; extra aggiunge rotte (solo i test, per le rotte
// pubbliche che il Go non ha ancora). Ogni rotta si registra con f.rottaPubblica, o f.speciale per quelle
// fuori dal pipeline :browser (/healthz), così sta nell'elenco f.rotte.
func costruisciPubblica(d Dipendenze, extra func(*facciaWeb, *http.ServeMux)) (http.Handler, *facciaWeb, error) {
	f, err := nuovaFaccia(d, "pubblica")
	if err != nil {
		return nil, nil, err
	}
	if d.Buste == nil {
		return nil, nil, errors.New("web: mancano le buste")
	}
	mux := http.NewServeMux()
	f.speciale(mux, rottaSalute, aperta, Salute(d.Sonda)) // nessuna sessione né intestazioni di pagina
	if extra != nil {
		// I test del limite e della sicurezza mettono le loro rotte /r/ di prova al posto della
		// pagina del cliente.
		extra(f, mux)
	} else {
		f.registraCliente(mux)
	}
	return facciaConFiltro(mux, d.Registro, limitePubblico(d.Limiti, f.registraBlocco)), f, nil
}
