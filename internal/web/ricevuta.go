package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/stampe"
)

// La ricevuta PDF della busta (RIP-S-42), come ReceiptController.show/2 dello standalone a
// go-baseline:
//   - GET /repairs/{id}/receipt.pdf, per titolare e dipendente, nella catena delle pagine
//     (:browser, :require_authenticated_user) senza guscio: sono byte;
//   - la busta come get_repair!/1 (buste.Ricevuta): 400 «Bad Request» per un id che non è un
//     intero, 404 «Not Found» se non c'è o è nel cestino, 500 oltre i 64 bit (misurato);
//   - il PDF si fa con stampe.Ricevuta e passa da pdf.Controlla; poi repair.receipt_generated;
//     se il PDF o la riga non riescono, 503 «PDF non disponibile» senza tipo (send_resp);
//   - la risposta: private, no-store, application/pdf, allegato «ricevuta-<numero>.pdf» con il nome
//     codificato come send_download/3 di Phoenix.
// La ricevuta scrive solo la sua riga: non è un'apertura della scheda (niente repair.viewed né segni).

const msgPDFNonDisponibile = "PDF non disponibile"

// ricevuta è GET /repairs/{id}/receipt.pdf.
func (f *facciaWeb) ricevuta(w http.ResponseWriter, r *http.Request) {
	pdf, dati, err := f.buste.Ricevuta(r.Context(), r.PathValue("id"), contestoFoto(r), stampe.Ricevuta)
	f.inviaPDF(w, r, pdf, err, "ricevuta", disposizioneAllegato(buste.NomeFileRicevuta(dati.Numero)))
}

// inviaPDF è la risposta dei controller dei PDF: gli errori di get_repair!/1, il 503 «PDF non
// disponibile» senza tipo (send_resp) se il PDF o la sua riga non riescono, altrimenti i byte con
// private, no-store, application/pdf e il Content-Disposition dato. cosa nomina il documento nel
// registro.
func (f *facciaWeb) inviaPDF(w http.ResponseWriter, r *http.Request, pdf []byte, err error, cosa, disp string) {
	h := w.Header()
	switch {
	case errors.Is(err, buste.ErrIdNonValido):
		paginaErrore(w, r, http.StatusBadRequest, "Bad Request")
		return
	case errors.Is(err, buste.ErrBustaNonTrovata):
		NonTrovata(w, r)
		return
	case errors.Is(err, buste.ErrRicevutaNonFatta), errors.Is(err, buste.ErrPDFNonFatto):
		f.registro.Error(cosa+" non fatta", "errore", err.Error())
		h.Set("Cache-Control", cacheBase)
		h["Content-Type"] = nil // send_resp senza tipo, e niente sniffing di net/http
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(msgPDFNonDisponibile)) // se il client se n'è andato non c'è niente da fare
		return
	case err != nil:
		f.registro.Error(cosa+" non letta", "errore", err.Error())
		Errore(w, r)
		return
	}
	h.Set("Cache-Control", "private, no-store")
	h.Set("Content-Type", "application/pdf")
	h.Set("Content-Disposition", disp)
	h.Set("Content-Length", strconv.Itoa(len(pdf)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdf) // come sopra
}

// disposizioneAllegato è disposizione per un allegato.
func disposizioneAllegato(nome string) string { return disposizione("attachment", nome) }

// disposizione è il Content-Disposition di send_download/3 (Phoenix 1.8.13, misurato nell'immagine
// fab-220) con la disposition data («attachment» o «inline»): il nome con URI.encode/2 e
// char_unreserved?/1, cioè tutto tranne lettere e cifre ASCII e «-._~» in %XX maiuscolo; se il nome
// cambia, anche filename* in UTF-8.
func disposizione(tipo, nome string) string {
	var b strings.Builder
	for i := 0; i < len(nome); i++ {
		c := nome[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte("0123456789ABCDEF"[c>>4])
		b.WriteByte("0123456789ABCDEF"[c&15])
	}
	cod := b.String()
	d := tipo + `; filename="` + cod + `"`
	if cod != nome {
		d += "; filename*=utf-8''" + cod
	}
	return d
}
