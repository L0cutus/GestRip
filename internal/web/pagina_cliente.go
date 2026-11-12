package web

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clientip"
	"github.com/L0cutus/gestrip/internal/clock"
)

// La pagina del cliente GET /r/{token} sulla faccia pubblica (CLI-01, CLI-02, CLI-03, CLI-21), come
// show/2 e with_public_repair/3 di PublicRepairController dello standalone (go-baseline):
//   - il token è tutta l'autorizzazione: niente sessione né accesso (rottaPubblica, pipeline
//     :browser con le intestazioni delle pagine);
//   - token sbagliato, scaduto, revocato o busta nel cestino: lo stesso 404 «Riparazione non
//     disponibile» (RifiutoPubblico), che il limite ai tentativi falliti conta (limiti.go: 30 in un
//     minuto, poi 15 minuti di blocco con lo stesso 404, D-05);
//   - riuscita: no-store e no-referrer (public_headers/1), la pagina con quello che mostra lo
//     standalone (buste.PaginaCliente) e nient'altro; aprirla non scrive righe d'audit, come
//     show/2.
//
// Le parti della pagina delle slice dopo (062: foto e preventivo; 063: conversazione; 070: riquadro
// email, email_cliente.go; T4.7: riquadro Telegram) hanno il loro posto in pagina_cliente.html.

// datiPaginaCliente è quello che pagina_cliente.html riceve. Il token c'è solo per gli indirizzi
// che la pagina stessa chiama (le foto e i due moduli del preventivo), come lo standalone.
type datiPaginaCliente struct {
	buste.PaginaCliente
	Link       string // il token del link, solo per i percorsi /r/<token>/…
	Info       string // public-flash-info, una volta
	Errore     string // public-flash-error, una volta
	Conferma   bool   // ?rifiuta=1: il pannello che chiede conferma del rifiuto (confirming_refusal)
	FotoPagina []fotoDellaScheda
	RispostaIl string // l'istante della risposta data, Clock.stamp/1
	// La conversazione (CLI-10…12): le righe già scritte per la pagina, l'istante dell'ultima
	// (data-last-ts) e gli esiti del riquadro (thread_info, thread_error), una volta.
	Righe                []rigaConversazione
	UltimoTS             string
	InfoConv, ErroreConv string
	// Il riquadro dell'avviso email (CLI-39, slice 070): nil dove la pagina non lo mostra (email non
	// disponibile per la D-16, busta chiusa, link storico di una busta conto terzi); gli indirizzi dei
	// suoi due moduli ("" nell'anteprima) e i suoi esiti (email_info, email_error), una volta.
	Email                         *buste.RiquadroEmail
	IndirizzoEmail, IndirizzoStop string
	InfoEmail, ErroreEmail        string
	// Gli indirizzi del poll e del modulo del messaggio, e la credenziale che il modulo rimanda in
	// campi nascosti (solo la pagina storica /stato, legacy_credential/1).
	IndirizzoPoll, IndirizzoMessaggio string
	Credenziale                       []campoNascosto
	// L'anteprima del personale (anteprima_cliente.go): niente token né indirizzi, i controlli spenti,
	// la barra con «← Indietro» verso la scheda (Scheda) e l'informativa sull'indirizzo pubblico
	// (Privacy, "" = /privacy).
	Anteprima bool
	Scheda    string
	Privacy   string
}

// HrefPrivacy è l'indirizzo del link «Informativa privacy»: /privacy, o quello dato all'anteprima.
func (d datiPaginaCliente) HrefPrivacy() string {
	if d.Privacy != "" {
		return d.Privacy
	}
	return "/privacy"
}

// campoNascosto è un campo nascosto di un modulo, nell'ordine in cui va scritto.
type campoNascosto struct{ Nome, Valore string }

// rigaConversazione è una riga di thread_line/1.
type rigaConversazione struct {
	ID                     int64
	Parte, Lato, Il, Testo string
}

// righeConversazione scrive i messaggi come thread_line/1: lato, ora del negozio (Clock.stamp/1).
func righeConversazione(ms []buste.Messaggio) []rigaConversazione {
	r := make([]rigaConversazione, 0, len(ms))
	for _, m := range ms {
		r = append(r, rigaConversazione{ID: m.ID, Parte: m.Parte, Lato: m.Lato(), Il: clock.Timbro(m.InviatoIl), Testo: m.Testo})
	}
	return r
}

// ConvErrore è il data-kind del riscontro del riquadro.
func (d datiPaginaCliente) ConvErrore() bool { return d.ErroreConv != "" }

// Le fasi del preventivo per il modello.
func (d datiPaginaCliente) PreventivoApprovato() bool {
	return d.Risposta.Fase == buste.RispostaApprovata
}
func (d datiPaginaCliente) PreventivoRifiutato() bool {
	return d.Risposta.Fase == buste.RispostaRifiutata
}
func (d datiPaginaCliente) PreventivoSenzaImporto() bool {
	return d.Risposta.Fase == buste.RispostaSenzaImporto
}
func (d datiPaginaCliente) PreventivoInAttesa() bool {
	return d.Risposta.Fase == buste.RispostaInAttesa
}
func (d datiPaginaCliente) ConPreventivo() bool {
	return d.Risposta.Fase != buste.RispostaNonApplicabile
}

// paginaCliente è GET /r/{token}.
func (f *facciaWeb) paginaCliente(w http.ResponseWriter, r *http.Request) {
	p, err := f.buste.PaginaCliente(r.Context(), r.PathValue("token"))
	switch {
	case errors.Is(err, buste.ErrBustaNonTrovata):
		RifiutoPubblico(w, r, rifiutoRiparazione, false)
		return
	case err != nil:
		f.registro.Error("pagina del cliente non letta")
		Errore(w, r)
		return
	}
	tok := r.PathValue("token")
	base := "/r/" + tok
	d := datiPaginaCliente{PaginaCliente: p, Link: tok, FotoPagina: fotoPerLaScheda(p.Foto),
		IndirizzoPoll: base + "/poll", IndirizzoMessaggio: base + "/messaggio"}
	for i := range d.FotoPagina {
		d.FotoPagina[i].Src = base + "/foto/" + strconv.FormatInt(d.FotoPagina[i].ID, 10)
		d.FotoPagina[i].SrcMiniatura = indirizzoMiniatura(d.FotoPagina[i].Src)
	}
	if v, testo := parametroQuery(r.URL.RawQuery, "rifiuta"); testo && v == "1" {
		d.Conferma = true
	}
	if p.Risposta.Fase == buste.RispostaApprovata || p.Risposta.Fase == buste.RispostaRifiutata {
		d.RispostaIl = clock.Timbro(p.Risposta.Il)
	}
	d.Email = f.riquadroEmail(r, p.ID, false)
	d.IndirizzoEmail, d.IndirizzoStop = base+"/email-optin", base+"/email-optout"
	f.mostraPaginaCliente(w, r, d)
}

// mostraPaginaCliente disegna la pagina del link della busta o di un link storico: la conversazione
// già scritta e gli esiti lasciati da un'azione (una volta).
func (f *facciaWeb) mostraPaginaCliente(w http.ResponseWriter, r *http.Request, d datiPaginaCliente) {
	if d.ConConversazione {
		d.Righe = righeConversazione(d.Conversazione)
		if n := len(d.Conversazione); n > 0 {
			d.UltimoTS = d.Conversazione[n-1].Inviato
		}
	}
	e := f.esitoCliente(w, r)
	d.Info, d.Errore, d.InfoConv, d.ErroreConv = e.Info, e.Errore, e.InfoConv, e.ErroreConv
	d.InfoEmail, d.ErroreEmail = e.InfoEmail, e.ErroreEmail
	intestazioniPubbliche(w)
	f.mostraPubblica(w, r, "pagina_cliente", http.StatusOK, d)
}

// indirizzoMiniatura è thumbnail_src/1: lo stesso indirizzo con size=thumb.
func indirizzoMiniatura(src string) string {
	if strings.Contains(src, "?") {
		return src + "&size=thumb"
	}
	return src + "?size=thumb"
}

// esitoCliente prende i messaggi di esito lasciati da un'azione della pagina (il flash della
// sessione della faccia pubblica, come put_flash/3 dello standalone) e li toglie: si vedono una volta.
func (f *facciaWeb) esitoCliente(w http.ResponseWriter, r *http.Request) datiSessione {
	s := f.sessioni.leggi(r)
	if s.Info == "" && s.Errore == "" && s.InfoConv == "" && s.ErroreConv == "" && s.InfoEmail == "" && s.ErroreEmail == "" {
		return datiSessione{}
	}
	f.sessioni.cancella(w)
	return datiSessione{Info: s.Info, Errore: s.Errore, InfoConv: s.InfoConv, ErroreConv: s.ErroreConv, InfoEmail: s.InfoEmail,
		ErroreEmail: s.ErroreEmail}
}

// tornaAlCliente è back/4 del controller: il messaggio nella sessione della faccia pubblica e il
// redirect alla pagina del link.
func (f *facciaWeb) tornaAlCliente(w http.ResponseWriter, token, info, errore string) {
	f.tornaAlClienteCon(w, token, datiSessione{Info: info, Errore: errore}, "")
}

// tornaAlClienteCon è tornaAlCliente con gli esiti dati e un'ancora ("" senza).
func (f *facciaWeb) tornaAlClienteCon(w http.ResponseWriter, token string, esito datiSessione, ancora string) {
	f.tornaAllaPaginaCon(w, "/r/"+url.PathEscape(token)+ancora, esito)
}

// tornaAllaPaginaCon scrive gli esiti nella sessione della faccia pubblica e rimanda a dove.
func (f *facciaWeb) tornaAllaPaginaCon(w http.ResponseWriter, dove string, esito datiSessione) {
	f.sessioni.scrivi(w, esito, f.orologio.Adesso())
	rimanda(w, dove)
}

// I messaggi della risposta al preventivo, con i testi dello standalone (answered/4).
const (
	msgPreventivoApprovato   = "Grazie: il preventivo è stato approvato."
	msgPreventivoRifiutato   = "Abbiamo registrato che non vuole procedere."
	msgGiaApprovato          = "Questo preventivo era già stato approvato."
	msgGiaRifiutato          = "Aveva già dichiarato di non procedere."
	msgRifiutoDopoApprovato  = "Ha già confermato il preventivo. Contatti il negozio se vuole cambiare scelta."
	msgApprovatoDopoRifiuto  = "Ha già dichiarato di non procedere. Contatti il negozio se vuole cambiare scelta."
	msgNonPiuInPreventivo    = "Questa riparazione non è più in attesa di preventivo."
	msgRispostaNonRegistrata = "Non è stato possibile registrare la risposta."
)

// contestoCliente è context/1 del controller: chi agisce è il link («public») e l'indirizzo.
func contestoCliente(r *http.Request) audit.Contesto {
	return audit.Contesto{Attore: "public", IP: clientip.Di(r)}
}

// approvaPreventivo è POST /r/{token}/preventivo/approva (approve/2).
func (f *facciaWeb) approvaPreventivo(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	f.rispostaCliente(w, r, token, true, f.buste.ApprovaPreventivo(r.Context(), token, contestoCliente(r)))
}

// rifiutaPreventivo è POST /r/{token}/preventivo/rifiuta (reject/3), con il motivo nel campo reason.
func (f *facciaWeb) rifiutaPreventivo(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	motivo, _ := ultimo(r, "reason")
	f.rispostaCliente(w, r, token, false, f.buste.RifiutaPreventivo(r.Context(), token, motivo, contestoCliente(r)))
}

// rispostaCliente è answered/4: la stessa risposta due volte è un'informazione, quella opposta un
// errore che dice di chiedere al negozio; un link che non apre niente (anche una busta sparita nel
// frattempo) è il 404 di sempre, contato dal limite.
func (f *facciaWeb) rispostaCliente(w http.ResponseWriter, r *http.Request, token string, approva bool, err error) {
	intestazioniPubbliche(w)
	switch {
	case err == nil && approva:
		f.tornaAlCliente(w, token, msgPreventivoApprovato, "")
	case err == nil:
		f.tornaAlCliente(w, token, msgPreventivoRifiutato, "")
	case errors.Is(err, buste.ErrBustaNonTrovata):
		RifiutoPubblico(w, r, rifiutoRiparazione, false)
	case errors.Is(err, buste.ErrGiaApprovato) && approva:
		f.tornaAlCliente(w, token, msgGiaApprovato, "")
	case errors.Is(err, buste.ErrGiaRifiutato) && !approva:
		f.tornaAlCliente(w, token, msgGiaRifiutato, "")
	case errors.Is(err, buste.ErrGiaApprovato):
		f.tornaAlCliente(w, token, "", msgRifiutoDopoApprovato)
	case errors.Is(err, buste.ErrGiaRifiutato):
		f.tornaAlCliente(w, token, "", msgApprovatoDopoRifiuto)
	case errors.Is(err, buste.ErrNonInPreventivo):
		f.tornaAlCliente(w, token, "", msgNonPiuInPreventivo)
	default:
		f.registro.Error("risposta al preventivo non registrata")
		f.tornaAlCliente(w, token, "", msgRispostaNonRegistrata)
	}
}

// fotoCliente è GET /r/{token}/foto/{foto} (photo/2): no-store e no-referrer per le due varianti
// (una foto nascosta un attimo fa non torna da una cache), il tipo dei byte (D-47 come la rotta del
// personale), x-photo-variant; ogni rifiuto (link morto, id falso, foto di un'altra busta o non
// visibile a chi ha il link) è lo stesso 404 «Foto non disponibile», che il limite non conta.
func (f *facciaWeb) fotoCliente(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	v := buste.VarianteIntera
	if variante(r) == buste.VarianteMiniatura {
		v = buste.VarianteMiniatura
	}
	m, err := f.buste.LeggiFotoCliente(r.Context(), r.PathValue("token"), r.PathValue("foto"), v, contestoCliente(r))
	if err != nil {
		f.registro.Error("foto del cliente non letta")
		Errore(w, r)
		return
	}
	rispostaFotoPubblica(w, m)
}

// rispostaFotoPubblica è PhotoMedia.send_media/2 per le rotte pubbliche, o il loro 404 «Foto non
// disponibile» senza tipo (le intestazioni pubbliche le ha già messe il gestore).
func rispostaFotoPubblica(w http.ResponseWriter, m *buste.MediaFoto) {
	h := w.Header()
	if m == nil {
		h["Content-Type"] = nil
		w.WriteHeader(http.StatusNotFound)
		_ = http.NewResponseController(w).Flush()
		_, _ = w.Write([]byte(msgFotoNonDisponibile))
		return
	}
	h.Set("X-Photo-Variant", string(m.Variante))
	h.Set("Content-Type", tipoServito(m.Tipo))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(m.Byte)
}

// intestazioniPubbliche è public_headers/1: la pagina non resta in nessuna cache e non manda
// l'indirizzo (con il token) a nessun altro sito.
func intestazioniPubbliche(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

// mostraPubblica disegna una pagina della faccia pubblica: nessuna sessione, nessun messaggio di
// esito del personale, il token CSRF della faccia pubblica nella testa (get_csrf_token/0 del
// layout radice).
func (f *facciaWeb) mostraPubblica(w http.ResponseWriter, r *http.Request, nome string, codice int, dati any) {
	d := datiPagina{Token: f.csrf.Token(w, r), Dati: dati, Guscio: f.guscioPer(r, nome)}
	// Il page_title di una pagina pubblica che lo dà (/m/, /privacy): live_title/1 lo scrive anche
	// fuori dal guscio interno, con « · Laboratorio».
	if t, ok := dati.(conTitolo); ok {
		d.Guscio.Titolo = t.titoloPagina()
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(codice)
	_ = pagine.ExecuteTemplate(w, nome, d) // un errore qui è un client andato via: niente da fare
}

// registraCliente registra sulla faccia pubblica la pagina del cliente e quello che la sua testa
// chiede (asset con l'hash e icona), come lo standalone che li serve su ogni indirizzo.
func (f *facciaWeb) registraCliente(mux *http.ServeMux) {
	f.rottaPubblica(mux, "GET /r/{token}", f.paginaCliente)
	f.rottaPubblica(mux, "GET /r/{token}/foto/{foto}", f.fotoCliente)
	f.rottaPubblica(mux, "POST /r/{token}/preventivo/approva", f.approvaPreventivo)
	f.rottaPubblica(mux, "POST /r/{token}/preventivo/rifiuta", f.rifiutaPreventivo)
	f.rottaPubblica(mux, "GET /r/{token}/poll", f.pollCliente)
	f.rottaPubblica(mux, "POST /r/{token}/messaggio", f.messaggioCliente)
	f.rottaPubblica(mux, "GET /r/{token}/telegram", f.linkTelegram) // {telegram_optin}, slice 066
	f.registraStoriche(mux)
	f.registraEmail(mux)
	f.registraPortaleEPrivacy(mux)
	f.speciale(mux, "GET "+indirizzoIcona192, aperta, serviIcona("icon-192.png"))
	f.speciale(mux, "GET /assets/{nome}", aperta, http.HandlerFunc(serviAsset))
}
