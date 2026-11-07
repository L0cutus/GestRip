package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/L0cutus/gestrip/internal/buste"
)

// La firma di consegna (RIP-S-36…40), come DeliverySignatureController e la sezione
// #repair-signature di RepairLive dello standalone a go-baseline. Le quattro rotte:
//   - GET /repairs/{id}/firma, la pagina con il riquadro (personale, con il guscio ma senza «🏠
//     Lista»): busta che non c'è → home con «Riparazione non trovata.»; già firmata → la scheda; che
//     non può arrivare allo stato «ritirata» → la scheda con il messaggio. Nessuna scrittura;
//   - POST /repairs/{id}/firma (personale): la firma e la consegna (buste.CatturaFirma); campo non
//     valido → di nuovo la pagina di firma con «Firma mancante o non valida.»;
//   - GET /repairs/{id}/firma.png (personale, senza guscio: sono byte): il PNG originale, no-store e
//     no-referrer; busta non firmata, che non c'è o con il file guasto: lo stesso 404 «Firma non
//     disponibile», senza tipo;
//   - POST /repairs/{id}/firma/reset (solo titolare, come la pipeline :require_owner_user: il
//     dipendente va a / con «Operazione riservata al titolare.»): la firma azzerata e di nuovo la
//     pagina di firma con «Firma azzerata: puoi rifirmare.».
// I redirect verso la scheda sono quelli dello standalone: una navigazione nuova alla scheda, senza
// segno (segni.go), quindi un'apertura vera con la sua riga repair.viewed, come il mount di
// RepairLive dopo il redirect di un controller. Nello standalone anche «Rifai firma» è un modulo
// POST vero, non un evento della pagina. Il riquadro di firma è in app.js (signature_pad.js).

// rottaFirmaPNG è il pattern della rotta dell'immagine della firma.
const rottaFirmaPNG = "GET /repairs/{id}/firma.png"

const (
	msgFirmaRegistrata = "Firma registrata: riparazione consegnata."
	// msgFirmaCompraOro è il messaggio dopo la firma di una scheda Compro Oro (D-75, Stefano
	// 06/11/2026, punto 4 della 049): lo standalone dice per tutte «Firma registrata: riparazione
	// consegnata.», ma una pratica Compro Oro non si consegna, si chiude.
	msgFirmaCompraOro      = "Firma registrata: pratica chiusa."
	msgFirmaAzzerata       = "Firma azzerata: puoi rifirmare."
	msgFirmaNonDisponibile = "Firma non disponibile"
	// confermaRifaiFirma è il data-confirm di «↺ Rifai firma».
	confermaRifaiFirma = "Cancellare la firma e rifarla? La consegna resta registrata."
)

// datiFirma è la pagina di firma (new.html.heex).
type datiFirma struct {
	buste.PaginaFirma
	Dichiarazione string
}

// titoloPagina è il page_title: «Firma busta #<numero>».
func (d datiFirma) titoloPagina() string { return "Firma busta #" + d.Numero }

// firmaDellaScheda è la sezione #repair-signature della scheda.
type firmaDellaScheda struct {
	Timbro   string // «Firmata …», "" senza firma
	Firmata  bool
	Offerta  bool // senza firma: il pulsante, se la busta può arrivare allo stato «ritirata»
	Aggiungi bool // il pulsante dice «Aggiungi firma» (busta già consegnata o chiusa)
	Titolare bool // «↺ Rifai firma», solo per il titolare
	Conferma string
}

// sezioneFirma scrive la sezione #repair-signature dalla busta: signature_stamp/1, signature_offered?
// (collection_changes/2 != :error) e Accounts.owner?/1.
func sezioneFirma(s buste.Scheda, titolare bool) firmaDellaScheda {
	f := firmaDellaScheda{Titolare: titolare, Conferma: confermaRifaiFirma}
	if s.Firma != nil {
		f.Firmata, f.Timbro = true, s.Firma.Timbro()
		return f
	}
	_, f.Offerta = buste.Ritirabile(s.Stato, s.CompraOro)
	f.Aggiungi = s.Stato == "consegnata" || s.Stato == "chiusa"
	return f
}

// percorsoDellaScheda è redirect(to: detail(repair_id)): l'id com'era nel percorso, scritto come lo
// scrive ~p (codificaURI): «01» resta «01», «+1» diventa «%2B1».
func percorsoDellaScheda(id string) string { return "/repairs/" + codificaURI(id) }

// paginaFirma è GET /repairs/{id}/firma.
func (f *facciaWeb) paginaFirma(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := f.buste.ApriFirma(r.Context(), id)
	switch {
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
	case errors.Is(err, buste.ErrFirmaGiaPresente):
		f.vaiA(w, r, percorsoDellaScheda(id))
	case errors.Is(err, buste.ErrNonConsegnabile):
		f.vaiAConErrore(w, r, percorsoDellaScheda(id), buste.ErrNonConsegnabile.Error())
	case err != nil:
		f.registro.Error("pagina di firma non letta", "errore", err.Error())
		Errore(w, r)
	default:
		f.mostraCon(w, r, "firma", http.StatusOK, "", datiFirma{PaginaFirma: p, Dichiarazione: buste.DichiarazioneFirma})
	}
}

// salvaFirma è POST /repairs/{id}/firma.
func (f *facciaWeb) salvaFirma(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	campo, _ := ultimo(r, "firma_png")
	ritirata, _ := ultimo(r, "ritirato_da")
	err := f.buste.CatturaFirma(r.Context(), id, campo, ritirata, contestoFoto(r))
	switch {
	case err == nil:
		st := statoDi(r)
		st.dati.Info, st.cambiata = f.messaggioFirma(r.Context(), id), true
		f.vaiA(w, r, percorsoDellaScheda(id))
	case errors.Is(err, buste.ErrFirmaNonValida):
		f.vaiAConErrore(w, r, percorsoDellaScheda(id)+"/firma", buste.ErrFirmaNonValida.Error())
	case errors.Is(err, buste.ErrFirmaGiaPresente), errors.Is(err, buste.ErrNonConsegnabile):
		var rifiuto buste.Rifiuto
		errors.As(err, &rifiuto)
		f.vaiAConErrore(w, r, percorsoDellaScheda(id), rifiuto.Error())
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
	default:
		// Il ramo «_» del controller: ogni altro errore torna alla home con «Riparazione non trovata.».
		f.registro.Error("firma non salvata", "errore", err.Error())
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
	}
}

// immagineFirma è GET /repairs/{id}/firma.png.
func (f *facciaWeb) immagineFirma(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	png, err := f.buste.LeggiFirma(r.Context(), r.PathValue("id"), contestoFoto(r))
	if err != nil {
		// read/3 con la riga d'audit che non si scrive: lo standalone risponde lo stesso 404.
		f.registro.Error("lettura della firma non registrata", "errore", err.Error())
	}
	if png == nil || err != nil {
		// send_resp senza tipo, come il 404 delle foto (foto.go): niente Content-Type né sniffing, e
		// le intestazioni partono prima del corpo, senza lunghezza.
		h["Content-Type"] = nil
		w.WriteHeader(http.StatusNotFound)
		_ = http.NewResponseController(w).Flush()      // senza Flush il corpo parte lo stesso, con la lunghezza
		_, _ = w.Write([]byte(msgFirmaNonDisponibile)) // se il client se n'è andato non c'è niente da fare
		return
	}
	// put_resp_content_type("image/png", nil): senza charset.
	h.Set("Content-Type", "image/png")
	h.Set("Content-Length", strconv.Itoa(len(png)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png) // come sopra
}

// azzeraFirma è POST /repairs/{id}/firma/reset (il ruolo lo controlla la rotta).
func (f *facciaWeb) azzeraFirma(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := f.buste.AzzeraFirma(r.Context(), id, contestoFoto(r))
	switch {
	case err == nil:
		st := statoDi(r)
		st.dati.Info, st.cambiata = msgFirmaAzzerata, true
		f.vaiA(w, r, percorsoDellaScheda(id)+"/firma")
	case errors.Is(err, buste.ErrNessunaFirma):
		f.vaiAConErrore(w, r, percorsoDellaScheda(id), buste.ErrNessunaFirma.Error())
	default:
		if !errors.Is(err, buste.ErrBustaNonTrovata) {
			f.registro.Error("firma non azzerata", "errore", err.Error())
		}
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
	}
}

// messaggioFirma è il messaggio dopo la firma della busta id (D-75): «pratica chiusa» per una scheda
// Compro Oro, altrimenti quello dello standalone. Se la busta non si rilegge (la firma è già salvata)
// resta quello dello standalone.
func (f *facciaWeb) messaggioFirma(ctx context.Context, id string) string {
	n, ok := idPositivo(id)
	if !ok {
		return msgFirmaRegistrata
	}
	s, err := f.buste.Scheda(ctx, n)
	if err != nil || !s.CompraOro {
		return msgFirmaRegistrata
	}
	return msgFirmaCompraOro
}
