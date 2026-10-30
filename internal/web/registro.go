package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
)

// La pagina del registro del titolare (ADM-16), come AuditLive dello standalone. Senza LiveView il
// modulo dei filtri è un GET su /audit con actor, action e giorni (l'evento filter faceva
// push_patch allo stesso indirizzo), e «Precedente» e «Successiva» sono link con i filtri, la
// pagina e la fotografia. Il permesso lo controlla f.rotta a ogni richiesta.

const msgAuditNonDisponibile = "Audit non disponibile."

// Consultazione legge una pagina del registro e scrive la riga dell'apertura (ADM-16).
type Consultazione interface {
	Consulta(ctx context.Context, q audit.Richiesta, c audit.Contesto) (audit.Pagina, error)
}

type rigaRegistro struct {
	ID                                     int64
	Quando, Attore, Azione, Risorsa, Esito string
}

type datiRegistro struct {
	Pagina                 audit.Pagina
	Righe                  []rigaRegistro
	Periodi                []int
	Precedente, Successiva string // indirizzi delle pagine vicine, "" se non ci sono
}

func (f *facciaWeb) paginaRegistro(w http.ResponseWriter, r *http.Request) {
	v := campiQuery(r.URL.RawQuery)
	q := audit.Richiesta{Attore: ultimoDi(v, "actor"), Azione: ultimoDi(v, "action"), Giorni: ultimoDi(v, "giorni"),
		Pagina: ultimoDi(v, "pagina"), FinoA: ultimoDi(v, "fino_a")}
	errore := ""
	p, err := f.consultazione.Consulta(r.Context(), q, contestoDi(r))
	if err != nil {
		f.registro.Error("registro non disponibile", "errore", err)
		errore = msgAuditNonDisponibile
		p = audit.Pagina{Pagina: 1, Pagine: 1, Filtri: audit.Filtri{Giorni: 30}}
	}
	d := datiRegistro{Pagina: p, Periodi: []int{7, 30, 90, 365}}
	for _, e := range p.Eventi {
		d.Righe = append(d.Righe, rigaRegistro{ID: e.ID, Quando: clock.Formatta(e.Quando, "02/01/2006 15:04:05"),
			Attore: e.Attore, Azione: e.Azione, Risorsa: e.Risorsa + " #" + e.IDRisorsa, Esito: e.Esito})
	}
	if p.Pagina > 1 {
		d.Precedente = indirizzoRegistro(p, p.Pagina-1)
	}
	if p.Pagina < p.Pagine {
		d.Successiva = indirizzoRegistro(p, p.Pagina+1)
	}
	f.mostraCon(w, r, "registro", http.StatusOK, errore, d)
}

// indirizzoRegistro è query/2 di AuditLive: filtri, pagina e fotografia, senza i valori vuoti.
func indirizzoRegistro(p audit.Pagina, pagina int) string {
	v := url.Values{"giorni": {strconv.Itoa(p.Filtri.Giorni)}, "pagina": {strconv.Itoa(pagina)},
		"fino_a": {strconv.FormatInt(p.FinoA, 10)}}
	if p.Filtri.Attore != "" {
		v.Set("actor", p.Filtri.Attore)
	}
	if p.Filtri.Azione != "" {
		v.Set("action", p.Filtri.Azione)
	}
	return "/audit?" + v.Encode()
}

// campiQuery legge la query come Plug.Conn.Query (come formatoQuery): coppie separate solo da «&»
// (il «;» fa parte del valore), «+» è uno spazio, un «%» illeggibile resta com'è. url.ParseQuery
// invece scarterebbe la coppia, e il filtro sparirebbe. La validità UTF-8 la controlla già la
// faccia (400 come Plug).
func campiQuery(grezza string) url.Values {
	v := url.Values{}
	for _, coppia := range strings.Split(grezza, "&") {
		if coppia == "" {
			continue
		}
		k, x, _ := strings.Cut(coppia, "=")
		v.Add(decodificaForm(strings.ReplaceAll(k, "+", " ")), decodificaForm(strings.ReplaceAll(x, "+", " ")))
	}
	return v
}

// ultimoDi è l'ultimo valore di un campo ripetuto, come Plug.
func ultimoDi(v url.Values, campo string) string {
	vv := v[campo]
	if len(vv) == 0 {
		return ""
	}
	return vv[len(vv)-1]
}
