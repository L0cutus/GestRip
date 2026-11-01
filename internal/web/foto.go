package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clientip"
)

// La rotta delle foto del personale (RIP-S-33), come RepairPhotoController.show/2, PhotoMedia e
// GalleryToken dello standalone a go-baseline:
//   - GET /repairs/{id}/photos/{foto}, lo stesso percorso, per titolare e dipendente, nella catena
//     delle pagine (pipeline :browser e :require_authenticated_user) ma senza guscio: sono byte;
//   - ogni risposta ha no-store e no-referrer; la foto intera ha il tipo dei suoi byte e
//     x-photo-variant full; con esattamente size=thumb una miniatura WebP con x-photo-variant thumb,
//     che il browser del negozio tiene un giorno (private, max-age=86400); se la miniatura non si
//     fa, la foto intera, no-store, e l'intestazione lo dice;
//   - ogni rifiuto è il 404 «Foto non disponibile» (busta o foto che non c'è, malformata, di
//     un'altra busta, nel cestino, file guasto), con la riga d'audit «denied»;
//   - ogni lettura scrive la sua riga (buste.LeggiFotoPersonale), tranne la miniatura coperta dal
//     gettone della galleria (parametro g): la galleria ha scritto una riga per tutte.
// La miniatura si fa nel processo figlio (media.Miniature con imaging.Lavoratore), mai qui.

// rottaFoto è il pattern della rotta delle foto del personale.
const rottaFoto = "GET /repairs/{id}/photos/{foto}"

// msgFotoNonDisponibile è il corpo del rifiuto (send_resp(conn, :not_found, …)).
const msgFotoNonDisponibile = "Foto non disponibile"

// paginaFoto è GET /repairs/{id}/photos/{foto}.
func (f *facciaWeb) paginaFoto(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	busta, foto := r.PathValue("id"), r.PathValue("foto")
	c := contestoFoto(r)
	var m *buste.MediaFoto
	var err error
	switch {
	case variante(r) != buste.VarianteMiniatura:
		m, err = f.buste.LeggiFotoPersonale(r.Context(), busta, foto, buste.VarianteIntera, c)
	case f.coperta(r, busta, foto):
		m, err = f.buste.LeggiMiniaturaCoperta(r.Context(), busta, foto, c)
	default:
		m, err = f.buste.LeggiFotoPersonale(r.Context(), busta, foto, buste.VarianteMiniatura, c)
	}
	if err != nil {
		f.registro.Error("foto non letta")
		Errore(w, r)
		return
	}
	if m == nil {
		// send_resp senza tipo: nessun Content-Type, e niente sniffing di net/http. Le intestazioni
		// partono prima del corpo, senza lunghezza, come la risposta dello standalone (misurato sul
		// banco): così nemmeno il proxy davanti (Caddy) ne indovina uno.
		h["Content-Type"] = nil
		w.WriteHeader(http.StatusNotFound)
		_ = http.NewResponseController(w).Flush()     // senza Flush il corpo parte lo stesso, con la lunghezza
		_, _ = w.Write([]byte(msgFotoNonDisponibile)) // se il client se n'è andato non c'è niente da fare
		return
	}
	if m.Variante == buste.VarianteMiniatura {
		h.Set("Cache-Control", "private, max-age=86400")
	}
	h.Set("X-Photo-Variant", string(m.Variante))
	h.Set("Content-Type", m.Tipo)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(m.Byte) // come sopra
}

// contestoFoto è audit_context/1 del controller: email, indirizzo del client e il primo user agent.
func contestoFoto(r *http.Request) audit.Contesto {
	c := audit.Contesto{Attore: statoDi(r).utente.Email, IP: clientip.Di(r)}
	if ua := r.Header.Values("User-Agent"); len(ua) > 0 {
		c.UserAgent = ua[0]
	}
	return c
}

// variante è PhotoMedia.variant/1: la miniatura solo per esattamente size=thumb.
func variante(r *http.Request) buste.Variante {
	if v, testo := parametroQuery(r.URL.RawQuery, "size"); testo && v == "thumb" {
		return buste.VarianteMiniatura
	}
	return buste.VarianteIntera
}

// coperta è covered?/4: la richiesta porta un gettone g (testo) valido per chi chiede, la busta e
// la foto, tutti e due interi positivi.
func (f *facciaWeb) coperta(r *http.Request, busta, foto string) bool {
	g, testo := parametroQuery(r.URL.RawQuery, "g")
	if !testo {
		return false
	}
	bid, ok1 := idPositivo(busta)
	fid, ok2 := idPositivo(foto)
	return ok1 && ok2 && f.galleria.valido(g, statoDi(r).utente.ID, bid, fid, f.orologio.Adesso())
}

// parametroQuery dà il valore di nome nella query come i parametri di Plug (Plug.Conn.Query):
// coppie separate da «&», chiave e valore decodificati («+» è uno spazio, «%XX» un byte); vale
// l'ultima assegnazione. testo è falso se nome non c'è o se l'ultima assegnazione ha le parentesi
// («size[]=…», «size[x]=…»): allora per Plug non è un testo.
func parametroQuery(grezza, nome string) (valore string, testo bool) {
	for _, coppia := range strings.Split(grezza, "&") {
		if coppia == "" {
			continue
		}
		k, v, _ := strings.Cut(coppia, "=")
		k = decodificaForm(strings.ReplaceAll(k, "+", " "))
		radice, _, conParentesi := strings.Cut(k, "[")
		if radice != nome {
			continue
		}
		valore, testo = decodificaForm(strings.ReplaceAll(v, "+", " ")), !conParentesi
		if conParentesi {
			valore = ""
		}
	}
	return valore, testo
}

// gettoneGalleria è la firma della galleria su ogni miniatura che mostra (GalleryToken, slice 205
// dello standalone): nomina l'utente, la busta e la foto, quindi non apre altro (un altro utente,
// un'altra foto, la foto intera o un gettone scaduto si registrano come prima). Si firma
// all'inizio del giorno UTC, così l'indirizzo della miniatura resta lo stesso tutto il giorno e il
// browser la trova nella sua cache; vale due giorni, la giornata e quella della cache.
//
// Lo standalone usa Phoenix.Token; qui un HMAC-SHA256 con una sottochiave della chiave del server
// («galleria»). La forma del gettone è del Go: lo firma solo la galleria del Go (T4.2).
type gettoneGalleria struct{ chiave []byte }

// durataGettone è @max_age del gettone.
const durataGettone = 2 * 24 * time.Hour

// firma dà il gettone di utente, busta e foto firmato al momento firmatoIl.
func (g gettoneGalleria) firma(utente, busta, foto int64, firmatoIl time.Time) string {
	dati := fmt.Sprintf("%d.%d.%d.%d", utente, busta, foto, firmatoIl.UnixMilli())
	return base64.RawURLEncoding.EncodeToString([]byte(dati)) + "." + base64.RawURLEncoding.EncodeToString(g.mac(dati))
}

func (g gettoneGalleria) mac(dati string) []byte {
	h := hmac.New(sha256.New, g.chiave)
	h.Write([]byte("gallery-thumb\x00" + dati)) // hash.Hash.Write non fallisce
	return h.Sum(nil)
}

// valido dice se tok è il gettone di utente, busta e foto, firmato con questa chiave e non
// scaduto adesso (Phoenix.Token.verify con max_age: scaduto solo dopo la firma più la durata).
func (g gettoneGalleria) valido(tok string, utente, busta, foto int64, adesso time.Time) bool {
	if len(g.chiave) == 0 {
		return false
	}
	d64, m64, ok := strings.Cut(tok, ".")
	if !ok {
		return false
	}
	dati, err1 := base64.RawURLEncoding.DecodeString(d64)
	mac, err2 := base64.RawURLEncoding.DecodeString(m64)
	if err1 != nil || err2 != nil || !hmac.Equal(mac, g.mac(string(dati))) {
		return false
	}
	parti := strings.Split(string(dati), ".")
	if len(parti) != 4 {
		return false
	}
	var n [4]int64
	for i, p := range parti {
		v, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return false
		}
		n[i] = v
	}
	if n[0] != utente || n[1] != busta || n[2] != foto {
		return false
	}
	return adesso.UnixMilli() <= n[3]+durataGettone.Milliseconds()
}

// inizioGiorno è l'inizio del giorno UTC di t (day_start/0).
func inizioGiorno(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}
