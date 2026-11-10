package web

import (
	"encoding/base64"
	"html/template"
	"image/color"
	"net/http"
	"strconv"
	"strings"

	"github.com/boombuler/barcode/qr"

	"github.com/L0cutus/gestrip/internal/buste"
)

// Due pagine pubbliche senza busta, sulla faccia pubblica (PIANO §1.5), come lo standalone a
// go-baseline:
//   - il portale ritirato delle oreficerie (CLI-53, RetiredPortalController): ogni indirizzo
//     /m/<codice>/… risponde 410 «Portale non più attivo» con il nome e il telefono del negozio;
//     /m/<codice>/sw.js è il worker che svuota le cache «portale-*» del portale installato e si
//     toglie, fuori dal pipeline delle pagine (nessuna sessione). Il codice del percorso era tutta la
//     credenziale del portale: non si legge (nessuna rotta usa {codice} o {resto}), e nel registro va
//     solo il pattern della rotta, come per ogni rotta. Le pagine /m/ non contano nel limite ai
//     tentativi (PublicAttempts.family/1 non le ha);
//   - l'informativa privacy /privacy (CLI-54, PrivacyController e PrivacyHTML): l'intestazione del
//     titolare dal profilo del negozio, i paragrafi dello standalone, la data di aggiornamento e il QR
//     dell'indirizzo pubblico configurato (mai l'Host della richiesta), stampabile su un A4. Non scrive
//     righe d'audit e non legge nessuna busta.

// swPortaleRitirato è @service_worker di RetiredPortalController, byte per byte.
const swPortaleRitirato = `self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((key) => key.startsWith("portale-")).map((key) => caches.delete(key))))
      .then(() => self.registration.unregister())
  );
});
`

// aggiornamentoPrivacy è @updated_on di PrivacyHTML.
const aggiornamentoPrivacy = "2026-10-27"

// registraPortaleEPrivacy registra sulla faccia pubblica il portale ritirato e l'informativa.
func (f *facciaWeb) registraPortaleEPrivacy(mux *http.ServeMux) {
	f.rottaPubblica(mux, "GET /privacy", f.privacy)
	f.rottaPubblica(mux, "GET /m/{codice}", f.portaleRitirato)
	f.rottaPubblica(mux, "GET /m/{codice}/{resto...}", f.portaleRitirato)
	// Fuori da :browser, come lo standalone: uno script che il browser chiede da sé non riceve mai
	// una sessione né le intestazioni delle pagine.
	f.speciale(mux, "GET /m/{codice}/sw.js", aperta, http.HandlerFunc(serviWorkerPortale))
}

// nomeDelNegozio è OnlineSales.shop_name/1: il nome del profilo, «Negozio» senza profilo o con il
// nome vuoto.
func nomeDelNegozio(p buste.ProfiloNegozio) string {
	if p.Presente && p.Nome != "" {
		return p.Nome
	}
	return "Negozio"
}

// datiPortale è quello che portale_ritirato.html riceve.
type datiPortale struct {
	NomeNegozio string
	ConTelefono bool // profile && profile.phone: ogni telefono scritto, anche vuoto, come lo standalone
	Telefono    string
	HrefTel     template.HTMLAttr
}

func (datiPortale) titoloPagina() string { return "Portale non più attivo" }

// portaleRitirato è GET /m/{codice} e /m/{codice}/… (show/2).
func (f *facciaWeb) portaleRitirato(w http.ResponseWriter, r *http.Request) {
	p, err := f.buste.LeggiProfiloNegozio(r.Context())
	if err != nil {
		f.registro.Error("profilo del negozio non letto")
		Errore(w, r)
		return
	}
	d := datiPortale{NomeNegozio: nomeDelNegozio(p)}
	if p.Telefono != nil {
		d.ConTelefono, d.Telefono, d.HrefTel = true, *p.Telefono, attributoTel(*p.Telefono)
	}
	f.mostraPubblica(w, r, "portale_ritirato", http.StatusGone, d)
}

// serviWorkerPortale è GET /m/{codice}/sw.js (service_worker/2).
func serviWorkerPortale(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/javascript; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(swPortaleRitirato)) // se il client se n'è andato non c'è niente da fare
}

// datiPrivacy è quello che privacy.html riceve, già scritto come PrivacyHTML.
type datiPrivacy struct {
	NomeNegozio string // per il titolo: «Privacy — <nome>»
	Titolare    string // controller_name/1
	PartitaIVA  string // fiscal_id/1, "" senza
	Sede        string // seat/1, "" senza
	Contatti    string // contacts/1, "" senza
	Aggiornata  string // @updated_on
	QR          template.URL
}

func (d datiPrivacy) titoloPagina() string { return "Privacy — " + d.NomeNegozio }

// campoProfilo è field/2 di PrivacyHTML: un testo non vuoto, altrimenti assente.
func campoProfilo(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// privacy è GET /privacy (show/2).
func (f *facciaWeb) privacy(w http.ResponseWriter, r *http.Request) {
	p, err := f.buste.LeggiProfiloNegozio(r.Context())
	if err != nil {
		f.registro.Error("profilo del negozio non letto")
		Errore(w, r)
		return
	}
	d := datiPrivacy{NomeNegozio: nomeDelNegozio(p), Aggiornata: aggiornamentoPrivacy}
	nome := ""
	if p.Presente {
		nome = p.Nome
	}
	// controller_name/1: la ragione sociale, poi il nome del negozio, poi «Il negozio».
	d.Titolare = "Il negozio"
	for _, v := range []string{campoProfilo(p.RagioneSociale), nome} {
		if v != "" {
			d.Titolare = v
			break
		}
	}
	d.PartitaIVA = campoProfilo(p.PartitaIVA)
	var sede []string // seat/1: indirizzo, poi città, unite da «, »
	for _, v := range []string{campoProfilo(p.Indirizzo), campoProfilo(p.Citta)} {
		if v != "" {
			sede = append(sede, v)
		}
	}
	d.Sede = strings.Join(sede, ", ")
	var contatti []string // contacts/1: «tel. …» e «email …», uniti da « · »
	if v := campoProfilo(p.Telefono); v != "" {
		contatti = append(contatti, "tel. "+v)
	}
	if v := campoProfilo(p.Email); v != "" {
		contatti = append(contatti, "email "+v)
	}
	d.Contatti = strings.Join(contatti, " · ")
	// qr_src/0: il QR dell'indirizzo pubblico configurato, mai l'Host della richiesta; senza indirizzo
	// configurato non c'è (lo standalone ha sempre Endpoint.url/0).
	if f.pubblico != "" {
		if d.QR, err = qrSVG(f.pubblico + "/privacy"); err != nil {
			f.registro.Error("QR dell'informativa non fatto")
			Errore(w, r)
			return
		}
	}
	f.mostraPubblica(w, r, "privacy", http.StatusOK, d)
}

// margineQRSVG sono i moduli chiari attorno al codice (quiet zone, come pdf.MargineQR).
const margineQRSVG = 4

// qrSVG è il QR di contenuto come immagine SVG in un indirizzo data: (la forma di qr_src/0): correzione
// M e modo byte come QRCode.create(:medium) dello standalone e il QR dei PDF (pdf.Documento.QR), un
// rettangolo nero per ogni tratto scuro di una riga, su fondo bianco con il margine.
func qrSVG(contenuto string) (template.URL, error) {
	codice, err := qr.Encode(contenuto, qr.M, qr.Unicode)
	if err != nil {
		return "", err
	}
	n := codice.Bounds().Dx()
	lato := strconv.Itoa(n + 2*margineQRSVG)
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ` + lato + " " + lato + `" shape-rendering="crispEdges">`)
	b.WriteString(`<rect width="` + lato + `" height="` + lato + `" fill="#fff"/>`)
	for r := range n {
		for c := 0; c < n; {
			if codice.At(c, r) != color.Black {
				c++
				continue
			}
			inizio := c
			for c < n && codice.At(c, r) == color.Black {
				c++
			}
			b.WriteString(`<rect x="` + strconv.Itoa(inizio+margineQRSVG) + `" y="` + strconv.Itoa(r+margineQRSVG) +
				`" width="` + strconv.Itoa(c-inizio) + `" height="1"/>`)
		}
	}
	b.WriteString(`</svg>`)
	return template.URL("data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(b.String()))), nil
}
