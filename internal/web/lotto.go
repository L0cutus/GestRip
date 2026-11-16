package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// La pagina di un lotto d'avviso (slice 089, ORE-06, ORE-08, ORE-09), come ReadyLotLive dello standalone
// (/ready-lots/:id), per tutto il personale. Senza LiveView gli eventi add, add_scanned, add_candidate e remove sono
// moduli POST:
//   - «Aggiungi» (numero), «Aggiungi dal QR» e «Aggiungi» di una candidata: riuscita → ritorno alla pagina con
//     «✓ Aggiunta busta #N» e il campo vuoto; «Busta già in questo lotto.» è un esito, non un errore (refuse/2), e
//     come ogni rifiuto ridisegna la pagina con il numero scritto nel campo (assign_form(typed)); dalla candidata il
//     campo resta vuoto (add/3 con ""), dal QR il codice resta nel suo campo e il numero è vuoto (il Go non sa
//     cosa c'era scritto nell'altro modulo);
//   - «Rimuovi»: sempre un redirect, con «Busta #N rimossa dal lotto.» o il rifiuto;
//   - un id di busta non valido (Id.positive/1) torna alla pagina senza messaggio, un lotto che non c'è (o un id non
//     valido) alla lista con «Lotto non trovato.» (mount);
//   - guasto del database → la pagina d'errore con una riga nel registro (nello standalone la LiveView cade).
// Il numero mostrato è quello visibile (D-64). Un lotto inviato o chiuso è in sola lettura: niente aggiunte, niente
// «Rimuovi», niente candidate, con «Questo lotto è partito il …» e il link pubblico (o «Link storico non riattivato
// durante l'importazione.»). Lo scanner continuo è della 090, «📤 Chiudi e invia» della 091.

const (
	msgLottoNonTrovato    = "Lotto non trovato."
	lunghezzaDescrLotto   = 50
	segnapostoNumeroLotto = "es. 0045/26"
)

// rigaLottoBusta è una busta della pagina del lotto (membro o candidata).
type rigaLottoBusta struct {
	ID                  int64
	Numero, Descrizione string
	GiaAvvisata         bool
}

type datiLotto struct {
	ID                 int64
	Oreficeria, Aperto string
	Modificabile       bool
	Scritto, ScrittoQR string // i valori dei campi «Numero busta» e del QR
	Segnaposto         string
	Buste, Candidate   []rigaLottoBusta
	Partito, Link      string // «Questo lotto è partito il …» e il link pubblico, solo per un lotto uscito
	AccessoSpento      bool
}

func (d datiLotto) titoloPagina() string { return "Lotto " + d.Oreficeria }

// troncaLotto è truncate/1 della pagina del lotto: oltre 50 grafemi i primi 50 e «…».
func troncaLotto(s string) string {
	if len(buste.Grafemi(s, lunghezzaDescrLotto+1)) <= lunghezzaDescrLotto {
		return s
	}
	return strings.Join(buste.Grafemi(s, lunghezzaDescrLotto), "") + "…"
}

func percorsoDelLotto(id int64) string { return rottaPaginaLotti + "/" + strconv.FormatInt(id, 10) }

// lottoDellaRotta legge il lotto di {id}; se l'id non è valido o il lotto non c'è torna alla lista con «Lotto non
// trovato.» (mount di ReadyLotLive) e dà false.
func (f *facciaWeb) lottoDellaRotta(w http.ResponseWriter, r *http.Request) (buste.LottoAvviso, bool) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, rottaPaginaLotti, msgLottoNonTrovato)
		return buste.LottoAvviso{}, false
	}
	l, err := f.buste.LottoAvviso(r.Context(), id)
	switch {
	case errors.Is(err, buste.ErrLottoNonTrovato):
		f.vaiAConErrore(w, r, rottaPaginaLotti, msgLottoNonTrovato)
		return l, false
	case err != nil:
		f.registro.Error("lotto non letto", "errore", err.Error())
		Errore(w, r)
		return l, false
	}
	return l, true
}

func righeLotto(xx []buste.BustaDelLotto) []rigaLottoBusta {
	var out []rigaLottoBusta
	for _, x := range xx {
		out = append(out, rigaLottoBusta{ID: x.ID, Numero: x.Visibile().Titolo(), Descrizione: troncaLotto(x.Descrizione), GiaAvvisata: x.GiaAvvisata})
	}
	return out
}

// mostraLotto disegna la pagina del lotto con l'errore o l'esito e i valori dei due campi.
func (f *facciaWeb) mostraLotto(w http.ResponseWriter, r *http.Request, l buste.LottoAvviso, errore, esito, scritto, scrittoQR string) {
	bb, err := f.buste.BusteDelLotto(r.Context(), l.ID)
	if err != nil {
		f.registro.Error("buste del lotto non lette", "errore", err.Error())
		Errore(w, r)
		return
	}
	d := datiLotto{ID: l.ID, Oreficeria: l.Oreficeria, Aperto: clock.Timbro(l.Aperto), Modificabile: l.Modificabile(), Scritto: scritto,
		ScrittoQR: scrittoQR, Segnaposto: segnapostoNumeroLotto, Buste: righeLotto(bb), AccessoSpento: l.AccessoSpento}
	if d.Modificabile {
		cc, err := f.buste.CandidateDelLotto(r.Context(), l)
		if err != nil {
			f.registro.Error("candidate del lotto non lette", "errore", err.Error())
			Errore(w, r)
			return
		}
		d.Candidate = righeLotto(cc)
	} else if !l.AccessoSpento && l.Inviato != nil {
		d.Partito, d.Link = clock.Timbro(*l.Inviato), f.pubblico+"/lotto/"+l.Gettone
	}
	f.mostraConInfo(w, r, "lotto", http.StatusOK, errore, esito, d)
}

// paginaLotto è GET /ready-lots/{id}.
func (f *facciaWeb) paginaLotto(w http.ResponseWriter, r *http.Request) {
	if l, ok := f.lottoDellaRotta(w, r); ok {
		f.mostraLotto(w, r, l, "", "", "", "")
	}
}

// tornaAlLotto è il ritorno alla pagina del lotto con un messaggio di esito o di errore.
func (f *facciaWeb) tornaAlLotto(w http.ResponseWriter, r *http.Request, id int64, info, errore string) {
	st := statoDi(r)
	st.dati.Info, st.dati.Errore, st.cambiata = info, errore, true
	f.vaiA(w, r, percorsoDelLotto(id))
}

// testoDelModulo è string_param/2: il campo (l'ultimo valore, come Plug) senza spazi ai bordi; "" se manca o se il
// valore non è un testo (campo[…] o campo[], che Plug farebbe mappa o lista).
func testoDelModulo(r *http.Request, campo string) string {
	for k := range r.PostForm {
		if strings.HasPrefix(k, campo+"[") {
			return ""
		}
	}
	v, _ := ultimo(r, campo)
	return strings.TrimSpace(v)
}

// numeroDelLotto è bag_param/1: bag[numero_busta]; "" se manca, se bag non è la mappa («bag» o «bag[]») o se il
// valore non è un testo.
func numeroDelLotto(r *http.Request) string {
	for k := range r.PostForm {
		if k == "bag" || k == "bag[]" {
			return ""
		}
	}
	return testoDelModulo(r, "bag[numero_busta]")
}

// aggiungiAlLotto fa l'aggiunta e risponde: scritto e scrittoQR sono i valori da rimettere nei campi dopo un rifiuto.
func (f *facciaWeb) aggiungiAlLotto(w http.ResponseWriter, r *http.Request, l buste.LottoAvviso, q buste.RichiestaLotto, scritto, scrittoQR string) {
	x, err := f.buste.AggiungiAlLotto(r.Context(), l.ID, q, contestoDi(r))
	var rifiuto buste.Rifiuto
	var altro *buste.InAltroLotto // non è un Rifiuto; *RifiutoPiuBuste sì (Unwrap: ErrPiuBuste)
	switch {
	case err == nil:
		f.tornaAlLotto(w, r, l.ID, "✓ Aggiunta busta "+x.Visibile().Titolo(), "")
		return
	case errors.Is(err, buste.ErrGiaInQuestoLotto):
		f.ridisegnaLotto(w, r, l.ID, "", err.Error(), scritto, scrittoQR)
	case errors.As(err, &rifiuto), errors.As(err, &altro):
		f.ridisegnaLotto(w, r, l.ID, err.Error(), "", scritto, scrittoQR)
	default:
		f.registro.Error("busta non aggiunta al lotto", "errore", err.Error())
		Errore(w, r)
	}
}

// ridisegnaLotto ridisegna la pagina dopo un rifiuto, con il lotto riletto: un lotto inviato nel frattempo smette di
// offrire quello che ha rifiutato.
func (f *facciaWeb) ridisegnaLotto(w http.ResponseWriter, r *http.Request, id int64, errore, esito, scritto, scrittoQR string) {
	l, err := f.buste.LottoAvviso(r.Context(), id)
	if err != nil {
		if errors.Is(err, buste.ErrLottoNonTrovato) {
			f.vaiAConErrore(w, r, rottaPaginaLotti, msgLottoNonTrovato)
			return
		}
		f.registro.Error("lotto non letto", "errore", err.Error())
		Errore(w, r)
		return
	}
	f.mostraLotto(w, r, l, errore, esito, scritto, scrittoQR)
}

// aggiungiPerNumero è l'evento add: il campo «Numero busta».
func (f *facciaWeb) aggiungiPerNumero(w http.ResponseWriter, r *http.Request) {
	if l, ok := f.lottoDellaRotta(w, r); ok {
		n := numeroDelLotto(r)
		f.aggiungiAlLotto(w, r, l, buste.RichiestaLotto{Numero: n}, n, "")
	}
}

// aggiungiPerQR è l'evento add_scanned: il campo del QR.
func (f *facciaWeb) aggiungiPerQR(w http.ResponseWriter, r *http.Request) {
	if l, ok := f.lottoDellaRotta(w, r); ok {
		qr := testoDelModulo(r, "qr")
		f.aggiungiAlLotto(w, r, l, buste.RichiestaLotto{QR: qr}, "", qr)
	}
}

// aggiungiCandidata è l'evento add_candidate: «Aggiungi» di una candidata, per id.
func (f *facciaWeb) aggiungiCandidata(w http.ResponseWriter, r *http.Request) {
	l, ok := f.lottoDellaRotta(w, r)
	if !ok {
		return
	}
	busta, ok := idPositivo(r.PathValue("busta"))
	if !ok {
		f.vaiA(w, r, percorsoDelLotto(l.ID))
		return
	}
	f.aggiungiAlLotto(w, r, l, buste.RichiestaLotto{ID: strconv.FormatInt(busta, 10)}, "", "")
}

// togliDalLotto è l'evento remove: «Rimuovi».
func (f *facciaWeb) togliDalLotto(w http.ResponseWriter, r *http.Request) {
	l, ok := f.lottoDellaRotta(w, r)
	if !ok {
		return
	}
	busta, ok := idPositivo(r.PathValue("busta"))
	if !ok {
		f.vaiA(w, r, percorsoDelLotto(l.ID))
		return
	}
	x, err := f.buste.TogliDalLotto(r.Context(), l.ID, busta, contestoDi(r))
	var rifiuto buste.Rifiuto
	switch {
	case err == nil:
		f.tornaAlLotto(w, r, l.ID, "Busta "+x.Visibile().Titolo()+" rimossa dal lotto.", "")
	case errors.As(err, &rifiuto):
		f.tornaAlLotto(w, r, l.ID, "", err.Error())
	default:
		f.registro.Error("busta non tolta dal lotto", "errore", err.Error())
		Errore(w, r)
	}
}

// registraLotto registra le rotte della pagina del lotto (del personale, come la live dello standalone).
func (f *facciaWeb) registraLotto(mux *http.ServeMux) {
	f.rottaConGuscio(mux, "GET /ready-lots/{id}", personale, f.paginaLotto)
	f.rottaConGuscio(mux, "POST /ready-lots/{id}/aggiungi", personale, f.aggiungiPerNumero)               // ridisegna dopo un rifiuto
	f.rottaConGuscio(mux, "POST /ready-lots/{id}/qr", personale, f.aggiungiPerQR)                         // ridisegna dopo un rifiuto
	f.rottaConGuscio(mux, "POST /ready-lots/{id}/buste/{busta}/aggiungi", personale, f.aggiungiCandidata) // ridisegna dopo un rifiuto
	f.rotta(mux, "POST /ready-lots/{id}/buste/{busta}/togli", personale, f.togliDalLotto)
}
