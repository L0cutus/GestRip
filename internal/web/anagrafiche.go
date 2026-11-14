package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
)

// La pagina «Anagrafiche» (slice 076), come OreficerieLive dello standalone
// (/settings/anagrafiche, live_session del personale: titolare e dipendente), per ora con la sola
// parte degli artigiani (ART-01…04); le oreficerie clienti (ORE-01…03) arrivano con la T4.9.
// Senza LiveView gli eventi create_artisan, update_artisan e delete_artisan sono moduli POST:
//   - riuscita: ritorno alla pagina (302) con il messaggio dello standalone, come stream reset +
//     put_flash;
//   - rifiuto della creazione o della modifica: la pagina ridisegnata (200) con il messaggio e i
//     valori scritti nel modulo, come LiveView che non tocca i campi del browser (il nome vuoto della
//     creazione ridisegna il modulo dal changeset: valori ripuliti ed errore sotto il nome);
//   - l'eliminazione torna sempre alla pagina con il messaggio.
// «Elimina» sta nel modulo della riga (formaction, con data-confirm «Eliminare <nome>?»).

const (
	rottaPaginaAnagrafiche  = "/settings/anagrafiche"
	msgArtigianoNonAggiunto = "Artigiano non aggiunto."
	msgArtigianoAggiornato  = "Artigiano aggiornato."
	msgArtigianoEliminato   = "Artigiano eliminato."
)

// rigaArtigiano è una riga dell'elenco con i valori del suo modulo.
type rigaArtigiano struct {
	ID             int64
	Nome, Telefono string // i valori dei campi: quelli salvati, o quelli scritti dopo un rifiuto
	NomeSalvato    string // per la domanda di «Elimina»
}

// moduloNuovoArtigiano sono i valori del modulo «Nuovo artigiano» e l'errore sotto il nome.
type moduloNuovoArtigiano struct {
	Nome, Telefono string
	ErroreNome     string
}

type datiAnagrafiche struct {
	Artigiani      []rigaArtigiano
	Nuovo          moduloNuovoArtigiano
	ProfiloNegozio bool // il link «Profilo negozio»: solo al titolare, e solo con la sua pagina
}

// datiDelModuloArtigiano legge artisan[name] e artisan[phone] (l'ultimo valore, come Plug). Il
// secondo valore dice se il modulo è la mappa artisan (%{"artisan" => attrs} when is_map(attrs)):
// almeno un campo artisan[…], e nessun «artisan» o «artisan[]», che Plug farebbe testo o lista.
func datiDelModuloArtigiano(r *http.Request) (buste.DatiArtigiano, bool) {
	mappa := false
	for k := range r.PostForm {
		if k == "artisan" || k == "artisan[]" {
			return buste.DatiArtigiano{}, false
		}
		if strings.HasPrefix(k, "artisan[") {
			mappa = true
		}
	}
	var d buste.DatiArtigiano
	if v, ok := ultimo(r, "artisan[name]"); ok {
		d.Nome = &v
	}
	if v, ok := ultimo(r, "artisan[phone]"); ok {
		d.Telefono = &v
	}
	return d, mappa
}

func valoreDi(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// haRotta dice se la faccia ha registrato la rotta (le voci che compaiono solo quando la loro
// pagina c'è, come il menu).
func (f *facciaWeb) haRotta(pattern string) bool {
	for _, r := range f.rotte {
		if r.pattern == pattern {
			return true
		}
	}
	return false
}

// mostraAnagrafiche disegna la pagina con l'esito o l'errore; scritta, se c'è, mette nella riga id
// i valori scritti; nuovo, se c'è, è il modulo «Nuovo artigiano» da ridisegnare.
func (f *facciaWeb) mostraAnagrafiche(w http.ResponseWriter, r *http.Request, errore string, scritta *rigaArtigiano, nuovo *moduloNuovoArtigiano) {
	aa, err := f.buste.Artigiani(r.Context())
	if err != nil {
		f.registro.Error("anagrafica degli artigiani non letta", "errore", err.Error())
		Errore(w, r)
		return
	}
	d := datiAnagrafiche{ProfiloNegozio: titolareDi(r) && f.haRotta("GET /settings/shop")}
	for _, a := range aa {
		riga := rigaArtigiano{ID: a.ID, Nome: a.Nome, Telefono: a.Telefono, NomeSalvato: a.Nome}
		if scritta != nil && scritta.ID == a.ID {
			riga.Nome, riga.Telefono = scritta.Nome, scritta.Telefono
		}
		d.Artigiani = append(d.Artigiani, riga)
	}
	if nuovo != nil {
		d.Nuovo = *nuovo
	}
	f.mostraCon(w, r, "anagrafiche", http.StatusOK, errore, d)
}

// paginaAnagrafiche è GET /settings/anagrafiche (mount di OreficerieLive).
func (f *facciaWeb) paginaAnagrafiche(w http.ResponseWriter, r *http.Request) {
	f.mostraAnagrafiche(w, r, "", nil, nil)
}

// tornaAlleAnagrafiche è il ritorno alla pagina con il messaggio di esito o di errore.
func (f *facciaWeb) tornaAlleAnagrafiche(w http.ResponseWriter, r *http.Request, info, errore string) {
	st := statoDi(r)
	st.dati.Info, st.dati.Errore, st.cambiata = info, errore, true
	f.vaiA(w, r, rottaPaginaAnagrafiche)
}

// creaArtigiano è l'evento create_artisan.
func (f *facciaWeb) creaArtigiano(w http.ResponseWriter, r *http.Request) {
	dati, mappa := datiDelModuloArtigiano(r)
	if !mappa {
		f.mostraAnagrafiche(w, r, msgArtigianoNonAggiunto, nil, nil)
		return
	}
	a, err := f.buste.CreaArtigiano(r.Context(), dati, contestoDi(r))
	var doppio *buste.ArtigianoDoppio
	switch {
	case err == nil:
		f.tornaAlleAnagrafiche(w, r, `Artigiano "`+a.Nome+`" aggiunto.`, "")
	case errors.As(err, &doppio):
		f.mostraAnagrafiche(w, r, err.Error(), nil, &moduloNuovoArtigiano{Nome: valoreDi(dati.Nome), Telefono: valoreDi(dati.Telefono)})
	case errors.Is(err, buste.ErrNomeArtigiano):
		// to_form(changeset): i valori ripuliti e normalizzati, l'errore di Ecto sotto il nome.
		f.mostraAnagrafiche(w, r, err.Error(), nil, &moduloNuovoArtigiano{Telefono: buste.NormalizzaTelefono(valoreDi(dati.Telefono)), ErroreNome: buste.MsgObbligatorio})
	default:
		f.registro.Error("artigiano non aggiunto", "errore", err.Error())
		f.mostraAnagrafiche(w, r, msgArtigianoNonAggiunto, nil, nil)
	}
}

// modificaArtigiano è l'evento update_artisan.
func (f *facciaWeb) modificaArtigiano(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	dati, mappa := datiDelModuloArtigiano(r)
	if !ok || !mappa {
		f.mostraAnagrafiche(w, r, buste.ErrArtigianoNonTrovato.Error(), nil, nil)
		return
	}
	_, err := f.buste.ModificaArtigiano(r.Context(), id, dati, contestoDi(r))
	var doppio *buste.ArtigianoDoppio
	switch {
	case err == nil:
		f.tornaAlleAnagrafiche(w, r, msgArtigianoAggiornato, "")
	case errors.As(err, &doppio), errors.Is(err, buste.ErrNomeArtigiano):
		f.mostraAnagrafiche(w, r, err.Error(), &rigaArtigiano{ID: id, Nome: valoreDi(dati.Nome), Telefono: valoreDi(dati.Telefono)}, nil)
	default:
		if !errors.Is(err, buste.ErrArtigianoNonTrovato) {
			f.registro.Error("artigiano non modificato", "errore", err.Error())
		}
		f.mostraAnagrafiche(w, r, buste.ErrArtigianoNonTrovato.Error(), nil, nil)
	}
}

// eliminaArtigiano è l'evento delete_artisan.
func (f *facciaWeb) eliminaArtigiano(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.tornaAlleAnagrafiche(w, r, "", buste.ErrArtigianoNonTrovato.Error())
		return
	}
	err := f.buste.EliminaArtigiano(r.Context(), id, contestoDi(r))
	var inUso *buste.ArtigianoInUso
	var conConsegne *buste.ArtigianoConConsegne
	switch {
	case err == nil:
		f.tornaAlleAnagrafiche(w, r, msgArtigianoEliminato, "")
	case errors.As(err, &inUso), errors.As(err, &conConsegne):
		f.tornaAlleAnagrafiche(w, r, "", err.Error())
	case errors.Is(err, buste.ErrArtigianoNonTrovato):
		f.tornaAlleAnagrafiche(w, r, "", err.Error())
	default:
		f.registro.Error("artigiano non eliminato", "errore", err.Error())
		Errore(w, r)
	}
}

// registraAnagrafiche registra le rotte della pagina (tutte del personale, come la live_session
// dello standalone).
func (f *facciaWeb) registraAnagrafiche(mux *http.ServeMux) {
	f.rottaConGuscio(mux, "GET /settings/anagrafiche", personale, f.paginaAnagrafiche)
	f.rottaConGuscio(mux, "POST /settings/anagrafiche/artigiani", personale, f.creaArtigiano)          // ridisegna dopo un rifiuto
	f.rottaConGuscio(mux, "POST /settings/anagrafiche/artigiani/{id}", personale, f.modificaArtigiano) // ridisegna dopo un rifiuto
	f.rotta(mux, "POST /settings/anagrafiche/artigiani/{id}/elimina", personale, f.eliminaArtigiano)
}
