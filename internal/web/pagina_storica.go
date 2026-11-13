package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
)

// I link storici della vecchia app Flask sulla faccia pubblica (CLI-16…20), come legacy/2,
// legacy_poll/2, legacy_message/2, legacy_photo/2 e legacy_thumb/2 di PublicRepairController dello
// standalone (go-baseline). Le regole (busta, credenziale, revoca, contenuto, audit) stanno in
// internal/buste/storica.go; qui gli indirizzi, le risposte e i testi:
//   - GET /stato/{busta}/{anno} con ?pw= o ?t=&exp=: la pagina del cliente ridotta (CLI-17), no-store e
//     no-referrer; il 410 «Link non più attivo» per la credenziale giusta di una busta revocata
//     (CLI-18); ogni altro rifiuto il 404 «Riparazione non disponibile» (RifiutoPubblico), che il
//     limite conta nel secchio corto di /stato (10 in 15 minuti, limiti.go);
//   - GET /stato/{busta}/{anno}/poll e POST /stato/{busta}/{anno}/messaggio (CLI-19) con la stessa
//     credenziale (nella query il poll, nei campi del modulo il messaggio), il redirect alla pagina
//     con la credenziale; un rifiuto è il 404 della pagina;
//   - GET /foto/{slug}/{file} e GET /thumb/{slug}/{file} con ?sig=&exp= (CLI-20): la foto o la
//     miniatura, o il 404 «Foto non disponibile»; il limite non le conta.
// Le rotte email-optin/optout del link storico sono in email_cliente.go (slice 070); telegram-optin e
// telegram-optout in telegram_cliente.go (slice 072).

// Il 410 del link storico revocato (legacy_revoked/2).
const (
	titoloLinkNonAttivo = "Link non più attivo"
	msgLinkNonAttivo    = "Questo link non è più attivo. Chiedi a %s il link nuovo della tua riparazione."
)

// datiEsitoPubblico è quello che esito_pubblico.html riceve (email_outcome).
type datiEsitoPubblico struct {
	OK                bool
	Titolo, Messaggio string
	Link              string // «Torna alla tua riparazione», "" senza (slice 070)
}

// credenzialeStorica legge la credenziale come param/2: il valore di pw, t ed exp solo se è un
// testo (un pw[]=… non conta); campi è legacy_credential/1, quelli presenti nell'ordine pw, t, exp,
// da rimandare nel poll, nel modulo e nel redirect.
func credenzialeStorica(leggi func(nome string) (string, bool)) (buste.CredenzialeStorica, []campoNascosto) {
	var c buste.CredenzialeStorica
	var campi []campoNascosto
	for _, x := range []struct {
		nome   string
		valore *string
		c      *bool
	}{{"pw", &c.Codice, &c.ConCodice}, {"t", &c.Firma, &c.ConFirma}, {"exp", &c.Scadenza, &c.ConScadenza}} {
		if v, ok := leggi(x.nome); ok {
			*x.valore, *x.c = v, true
			campi = append(campi, campoNascosto{x.nome, v})
		}
	}
	return c, campi
}

// dallaQuery legge un parametro della query come Plug (parametroQuery: testo, l'ultimo vince).
func dallaQuery(r *http.Request) func(string) (string, bool) {
	return func(nome string) (string, bool) {
		v, testo := parametroQuery(r.URL.RawQuery, nome)
		return v, testo
	}
}

// dalModulo legge un campo del modulo (r.PostForm, l'ultimo vince): la query non conta.
func dalModulo(r *http.Request) func(string) (string, bool) {
	return func(nome string) (string, bool) { return ultimo(r, nome) }
}

// segmentoPhoenix scrive un segmento di percorso come i percorsi verificati di Phoenix
// (__encode_segment__/1, misurato nell'immagine dello standalone): tutto quello che non è una
// lettera o cifra ASCII o «-», «_», «.», «~» diventa %XX.
func segmentoPhoenix(s string) string {
	const esa = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(esa[c>>4])
		b.WriteByte(esa[c&15])
	}
	return b.String()
}

// queryPlug è Plug.Conn.Query.encode/2 di una lista: le coppie nell'ordine dato (misurato), ogni
// chiave e valore con URI.encode_www_form/1 (lo spazio è «+»).
func queryPlug(campi []campoNascosto) string {
	parti := make([]string, len(campi))
	for i, c := range campi {
		parti[i] = url.QueryEscape(c.Nome) + "=" + url.QueryEscape(c.Valore)
	}
	return strings.Join(parti, "&")
}

// baseStorica è /stato/<busta>/<anno> con i segmenti come arrivano.
func baseStorica(r *http.Request) string {
	return "/stato/" + segmentoPhoenix(r.PathValue("busta")) + "/" + segmentoPhoenix(r.PathValue("anno"))
}

// paginaStoricaCon è legacy_page/3: la pagina con la credenziale, o senza query se non ce n'è.
func paginaStoricaCon(r *http.Request, campi []campoNascosto) string {
	if len(campi) == 0 {
		return baseStorica(r)
	}
	return baseStorica(r) + "?" + queryPlug(campi)
}

// scrittoStorico è «<busta>/<anno>» come arriva dall'indirizzo ("#{bag}/#{year}").
func scrittoStorico(r *http.Request) string { return r.PathValue("busta") + "/" + r.PathValue("anno") }

// paginaStorica è GET /stato/{busta}/{anno}.
func (f *facciaWeb) paginaStorica(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	cred, campi := credenzialeStorica(dallaQuery(r))
	p, err := f.buste.PaginaStorica(r.Context(), scrittoStorico(r), cred, contestoCliente(r))
	switch {
	case errors.Is(err, buste.ErrLinkStoricoRevocato):
		f.mostraPubblica(w, r, "esito_pubblico", http.StatusGone, datiEsitoPubblico{Titolo: titoloLinkNonAttivo,
			Messaggio: strings.Replace(msgLinkNonAttivo, "%s", p.NomeNegozio, 1)})
		return
	case errors.Is(err, buste.ErrBustaNonTrovata):
		RifiutoPubblico(w, r, rifiutoRiparazione, false)
		return
	case err != nil:
		f.registro.Error("pagina storica non letta")
		Errore(w, r)
		return
	}
	base := baseStorica(r)
	d := datiPaginaCliente{PaginaCliente: p, FotoPagina: fotoPerLaScheda(p.Foto), Credenziale: campi,
		IndirizzoPoll: base + "/poll?" + queryPlug(campi), IndirizzoMessaggio: base + "/messaggio"}
	slug := segmentoPhoenix(p.SlugFoto)
	for i := range d.FotoPagina {
		// legacy_photo_src/1: ?sig=…&exp=… (Plug tiene l'ordine della lista).
		d.FotoPagina[i].Src = "/foto/" + slug + "/" + segmentoPhoenix(p.Foto[i].NomeFile) + "?" +
			queryPlug([]campoNascosto{{"sig", p.FirmeFoto[i]}, {"exp", p.ScadenzaFoto}})
		d.FotoPagina[i].SrcMiniatura = indirizzoMiniatura(d.FotoPagina[i].Src)
	}
	d.Email = f.riquadroEmail(r, p.ID, true)
	d.IndirizzoEmail, d.IndirizzoStop = base+"/email-optin", base+"/email-optout"
	d.Telegram = f.riquadroTelegramCliente(r, p.ID, true)
	d.IndirizzoTelegram, d.IndirizzoStopTelegram = base+"/telegram-optin", base+"/telegram-optout"
	f.mostraPaginaCliente(w, r, d)
}

// pollStorico è GET /stato/{busta}/{anno}/poll.
func (f *facciaWeb) pollStorico(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	cred, _ := credenzialeStorica(dallaQuery(r))
	ms, err := f.buste.ConversazioneStorica(r.Context(), scrittoStorico(r), cred, contestoCliente(r))
	f.rispostaPoll(w, r, ms, err)
}

// messaggioStorico è POST /stato/{busta}/{anno}/messaggio, con la credenziale e il testo nei campi
// del modulo; torna alla pagina storica con la credenziale (written/3).
func (f *facciaWeb) messaggioStorico(w http.ResponseWriter, r *http.Request) {
	intestazioniPubbliche(w)
	cred, campi := credenzialeStorica(dalModulo(r))
	testo, _ := ultimo(r, "testo")
	err := f.buste.ScriviMessaggioStorico(r.Context(), scrittoStorico(r), cred, testo, contestoCliente(r))
	pagina := paginaStoricaCon(r, campi)
	f.esitoMessaggio(w, r, err, pagina)
}

// esitoMessaggio è written/3: riuscito, «Messaggio inviato.» nel riquadro; busta chiusa, la frase in
// cima alla pagina; link morto, il 404; gli altri rifiuti nel riquadro.
func (f *facciaWeb) esitoMessaggio(w http.ResponseWriter, r *http.Request, err error, pagina string) {
	conv := func(info, errore string) {
		f.tornaAllaPaginaCon(w, pagina+"#public-thread", datiSessione{InfoConv: info, ErroreConv: errore})
	}
	switch {
	case err == nil:
		conv(msgMessaggioInviato, "")
	case errors.Is(err, buste.ErrBustaNonTrovata):
		RifiutoPubblico(w, r, rifiutoRiparazione, false)
	case errors.Is(err, buste.ErrConversazioneChiusa):
		f.tornaAllaPaginaCon(w, pagina, datiSessione{Errore: msgNonRiceveMessaggi})
	case errors.Is(err, buste.ErrMessaggioVuoto):
		conv("", msgMessaggioVuoto)
	case errors.Is(err, buste.ErrMessaggioTroppoLungo):
		conv("", msgMessaggioLungo)
	case errors.Is(err, buste.ErrTroppiMessaggi):
		conv("", msgTroppiMessaggi)
	default:
		f.registro.Error("messaggio del cliente non scritto")
		conv("", msgMessaggioNonInviato)
	}
}

// fotoStorica è GET /foto/{slug}/{file}: la foto, o la miniatura con ?size=thumb.
func (f *facciaWeb) fotoStorica(w http.ResponseWriter, r *http.Request) {
	f.serviFotoStorica(w, r, variante(r))
}

// miniaturaStorica è GET /thumb/{slug}/{file}: sempre la miniatura (legacy_thumb/2).
func (f *facciaWeb) miniaturaStorica(w http.ResponseWriter, r *http.Request) {
	f.serviFotoStorica(w, r, buste.VarianteMiniatura)
}

func (f *facciaWeb) serviFotoStorica(w http.ResponseWriter, r *http.Request, v buste.Variante) {
	intestazioniPubbliche(w)
	firma, _ := parametroQuery(r.URL.RawQuery, "sig")
	scad, _ := parametroQuery(r.URL.RawQuery, "exp")
	m, err := f.buste.LeggiFotoStorica(r.Context(), r.PathValue("slug"), r.PathValue("file"), firma, scad, v, contestoCliente(r))
	if err != nil {
		f.registro.Error("foto storica non letta")
		Errore(w, r)
		return
	}
	rispostaFotoPubblica(w, m)
}

// registraStoriche registra le rotte dei link storici sulla faccia pubblica.
func (f *facciaWeb) registraStoriche(mux *http.ServeMux) {
	f.rottaPubblica(mux, "GET /stato/{busta}/{anno}", f.paginaStorica)
	f.rottaPubblica(mux, "GET /stato/{busta}/{anno}/poll", f.pollStorico)
	f.rottaPubblica(mux, "POST /stato/{busta}/{anno}/messaggio", f.messaggioStorico)
	f.rottaPubblica(mux, "GET /foto/{slug}/{file}", f.fotoStorica)
	f.rottaPubblica(mux, "GET /thumb/{slug}/{file}", f.miniaturaStorica)
}
