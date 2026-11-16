package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
)

// La pagina «Anagrafiche» (slice 076 e 086), come OreficerieLive dello standalone
// (/settings/anagrafiche, live_session del personale: titolare e dipendente): le oreficerie clienti
// (ORE-01…03, sopra) e gli artigiani (ART-01…04). Senza LiveView gli eventi create, update,
// create_artisan, update_artisan e delete_artisan sono moduli POST:
//   - riuscita: ritorno alla pagina (302) con il messaggio dello standalone, come stream reset +
//     put_flash;
//   - rifiuto della creazione o della modifica: la pagina ridisegnata (200) con il messaggio e i
//     valori scritti nel modulo, come LiveView che non tocca i campi del browser (il nome vuoto della
//     creazione ridisegna il modulo dal changeset: valori ripuliti ed errore sotto il nome);
//   - l'eliminazione torna sempre alla pagina con il messaggio.
// «Elimina» degli artigiani sta nel modulo della riga (formaction, con data-confirm «Eliminare <nome>?»);
// le oreficerie non si eliminano (ORE-03).

const (
	rottaPaginaAnagrafiche  = "/settings/anagrafiche"
	msgArtigianoNonAggiunto = "Artigiano non aggiunto."
	msgArtigianoAggiornato  = "Artigiano aggiornato."
	msgArtigianoEliminato   = "Artigiano eliminato."

	msgOreficeriaNonAggiunta = "Oreficeria non aggiunta."
	msgOreficeriaAggiornata  = "Oreficeria aggiornata."
)

// rigaAnagrafica è una riga dell'elenco (di un artigiano o di un'oreficeria) con i valori del suo modulo.
type rigaAnagrafica struct {
	ID             int64
	Nome, Telefono string // i valori dei campi: quelli salvati, o quelli scritti dopo un rifiuto
	NomeSalvato    string // per la domanda di «Elimina» degli artigiani
}

// moduloNuovo sono i valori del modulo «Nuovo artigiano» o «Nuova oreficeria» e l'errore sotto il nome.
type moduloNuovo struct {
	Nome, Telefono string
	ErroreNome     string
}

type datiAnagrafiche struct {
	Oreficerie      []rigaAnagrafica
	NuovaOreficeria moduloNuovo
	Artigiani       []rigaAnagrafica
	Nuovo           moduloNuovo
	ProfiloNegozio  bool // il link «Profilo negozio»: solo al titolare, e solo con la sua pagina
}

// scrittiAnagrafiche sono i valori scritti da rimettere nei moduli dopo un rifiuto, uno per modulo: la riga
// con quell'id (di artigiano o di oreficeria) e il modulo «nuovo» di una delle due anagrafiche.
type scrittiAnagrafiche struct {
	Artigiano, Oreficeria           *rigaAnagrafica
	NuovoArtigiano, NuovaOreficeria *moduloNuovo
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

// mostraAnagrafiche disegna la pagina con l'esito o l'errore e, se ci sono, i valori scritti da rimettere
// nel modulo che li ha ricevuti.
func (f *facciaWeb) mostraAnagrafiche(w http.ResponseWriter, r *http.Request, errore string, scritti scrittiAnagrafiche) {
	aa, err := f.buste.Artigiani(r.Context())
	if err != nil {
		f.registro.Error("anagrafica degli artigiani non letta", "errore", err.Error())
		Errore(w, r)
		return
	}
	oo, err := f.buste.Oreficerie(r.Context())
	if err != nil {
		f.registro.Error("anagrafica delle oreficerie non letta", "errore", err.Error())
		Errore(w, r)
		return
	}
	d := datiAnagrafiche{ProfiloNegozio: titolareDi(r) && f.haRotta("GET /settings/shop")}
	for _, o := range oo {
		riga := rigaAnagrafica{ID: o.ID, Nome: o.Nome, Telefono: o.Telefono, NomeSalvato: o.Nome}
		if scritti.Oreficeria != nil && scritti.Oreficeria.ID == o.ID {
			riga.Nome, riga.Telefono = scritti.Oreficeria.Nome, scritti.Oreficeria.Telefono
		}
		d.Oreficerie = append(d.Oreficerie, riga)
	}
	for _, a := range aa {
		riga := rigaAnagrafica{ID: a.ID, Nome: a.Nome, Telefono: a.Telefono, NomeSalvato: a.Nome}
		if scritti.Artigiano != nil && scritti.Artigiano.ID == a.ID {
			riga.Nome, riga.Telefono = scritti.Artigiano.Nome, scritti.Artigiano.Telefono
		}
		d.Artigiani = append(d.Artigiani, riga)
	}
	if scritti.NuovoArtigiano != nil {
		d.Nuovo = *scritti.NuovoArtigiano
	}
	if scritti.NuovaOreficeria != nil {
		d.NuovaOreficeria = *scritti.NuovaOreficeria
	}
	f.mostraCon(w, r, "anagrafiche", http.StatusOK, errore, d)
}

// paginaAnagrafiche è GET /settings/anagrafiche (mount di OreficerieLive).
func (f *facciaWeb) paginaAnagrafiche(w http.ResponseWriter, r *http.Request) {
	f.mostraAnagrafiche(w, r, "", scrittiAnagrafiche{})
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
		f.mostraAnagrafiche(w, r, msgArtigianoNonAggiunto, scrittiAnagrafiche{})
		return
	}
	a, err := f.buste.CreaArtigiano(r.Context(), dati, contestoDi(r))
	var doppio *buste.ArtigianoDoppio
	switch {
	case err == nil:
		f.tornaAlleAnagrafiche(w, r, `Artigiano "`+a.Nome+`" aggiunto.`, "")
	case errors.As(err, &doppio):
		f.mostraAnagrafiche(w, r, err.Error(), scrittiAnagrafiche{NuovoArtigiano: &moduloNuovo{Nome: valoreDi(dati.Nome), Telefono: valoreDi(dati.Telefono)}})
	case errors.Is(err, buste.ErrNomeArtigiano):
		// to_form(changeset): i valori ripuliti e normalizzati, l'errore di Ecto sotto il nome.
		f.mostraAnagrafiche(w, r, err.Error(), scrittiAnagrafiche{NuovoArtigiano: &moduloNuovo{Telefono: buste.NormalizzaTelefono(valoreDi(dati.Telefono)), ErroreNome: buste.MsgObbligatorio}})
	default:
		f.registro.Error("artigiano non aggiunto", "errore", err.Error())
		f.mostraAnagrafiche(w, r, msgArtigianoNonAggiunto, scrittiAnagrafiche{})
	}
}

// modificaArtigiano è l'evento update_artisan.
func (f *facciaWeb) modificaArtigiano(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	dati, mappa := datiDelModuloArtigiano(r)
	if !ok || !mappa {
		f.mostraAnagrafiche(w, r, buste.ErrArtigianoNonTrovato.Error(), scrittiAnagrafiche{})
		return
	}
	_, err := f.buste.ModificaArtigiano(r.Context(), id, dati, contestoDi(r))
	var doppio *buste.ArtigianoDoppio
	switch {
	case err == nil:
		f.tornaAlleAnagrafiche(w, r, msgArtigianoAggiornato, "")
	case errors.As(err, &doppio), errors.Is(err, buste.ErrNomeArtigiano):
		f.mostraAnagrafiche(w, r, err.Error(), scrittiAnagrafiche{Artigiano: &rigaAnagrafica{ID: id, Nome: valoreDi(dati.Nome), Telefono: valoreDi(dati.Telefono)}})
	default:
		if !errors.Is(err, buste.ErrArtigianoNonTrovato) {
			f.registro.Error("artigiano non modificato", "errore", err.Error())
		}
		f.mostraAnagrafiche(w, r, buste.ErrArtigianoNonTrovato.Error(), scrittiAnagrafiche{})
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

// datiDelModuloOreficeria legge oreficeria[name] e oreficeria[phone] (l'ultimo valore, come Plug). Il
// secondo valore dice se il modulo è la mappa oreficeria (%{"oreficeria" => attrs} when is_map(attrs)):
// almeno un campo oreficeria[…], e nessun «oreficeria» o «oreficeria[]», che Plug farebbe testo o lista.
func datiDelModuloOreficeria(r *http.Request) (buste.DatiOreficeria, bool) {
	mappa := false
	for k := range r.PostForm {
		if k == "oreficeria" || k == "oreficeria[]" {
			return buste.DatiOreficeria{}, false
		}
		if strings.HasPrefix(k, "oreficeria[") {
			mappa = true
		}
	}
	var d buste.DatiOreficeria
	if v, ok := ultimo(r, "oreficeria[name]"); ok {
		d.Nome = &v
	}
	if v, ok := ultimo(r, "oreficeria[phone]"); ok {
		d.Telefono = &v
	}
	return d, mappa
}

// creaOreficeria è l'evento create.
func (f *facciaWeb) creaOreficeria(w http.ResponseWriter, r *http.Request) {
	dati, mappa := datiDelModuloOreficeria(r)
	if !mappa {
		f.mostraAnagrafiche(w, r, msgOreficeriaNonAggiunta, scrittiAnagrafiche{})
		return
	}
	o, err := f.buste.CreaOreficeria(r.Context(), dati, contestoDi(r))
	var doppia *buste.OreficeriaDoppia
	switch {
	case err == nil:
		f.tornaAlleAnagrafiche(w, r, `Oreficeria "`+o.Nome+`" aggiunta.`, "")
	case errors.As(err, &doppia):
		f.mostraAnagrafiche(w, r, err.Error(), scrittiAnagrafiche{NuovaOreficeria: &moduloNuovo{Nome: valoreDi(dati.Nome), Telefono: valoreDi(dati.Telefono)}})
	case errors.Is(err, buste.ErrNomeOreficeria):
		// to_form(changeset): i valori ripuliti e normalizzati, l'errore di Ecto sotto il nome.
		f.mostraAnagrafiche(w, r, err.Error(), scrittiAnagrafiche{NuovaOreficeria: &moduloNuovo{Telefono: buste.NormalizzaTelefono(valoreDi(dati.Telefono)), ErroreNome: buste.MsgObbligatorio}})
	default:
		f.registro.Error("oreficeria non aggiunta", "errore", err.Error())
		f.mostraAnagrafiche(w, r, msgOreficeriaNonAggiunta, scrittiAnagrafiche{})
	}
}

// modificaOreficeria è l'evento update.
func (f *facciaWeb) modificaOreficeria(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	dati, mappa := datiDelModuloOreficeria(r)
	if !ok || !mappa {
		f.mostraAnagrafiche(w, r, buste.ErrOreficeriaNonTrovata.Error(), scrittiAnagrafiche{})
		return
	}
	_, err := f.buste.ModificaOreficeria(r.Context(), id, dati, contestoDi(r))
	var doppia *buste.OreficeriaDoppia
	switch {
	case err == nil:
		f.tornaAlleAnagrafiche(w, r, msgOreficeriaAggiornata, "")
	case errors.As(err, &doppia), errors.Is(err, buste.ErrNomeOreficeria):
		f.mostraAnagrafiche(w, r, err.Error(), scrittiAnagrafiche{Oreficeria: &rigaAnagrafica{ID: id, Nome: valoreDi(dati.Nome), Telefono: valoreDi(dati.Telefono)}})
	default:
		if !errors.Is(err, buste.ErrOreficeriaNonTrovata) {
			f.registro.Error("oreficeria non modificata", "errore", err.Error())
		}
		f.mostraAnagrafiche(w, r, buste.ErrOreficeriaNonTrovata.Error(), scrittiAnagrafiche{})
	}
}

// registraAnagrafiche registra le rotte della pagina (tutte del personale, come la live_session
// dello standalone).
func (f *facciaWeb) registraAnagrafiche(mux *http.ServeMux) {
	f.rottaConGuscio(mux, "GET /settings/anagrafiche", personale, f.paginaAnagrafiche)
	f.rottaConGuscio(mux, "POST /settings/anagrafiche/oreficerie", personale, f.creaOreficeria)          // ridisegna dopo un rifiuto
	f.rottaConGuscio(mux, "POST /settings/anagrafiche/oreficerie/{id}", personale, f.modificaOreficeria) // ridisegna dopo un rifiuto
	f.rottaConGuscio(mux, "POST /settings/anagrafiche/artigiani", personale, f.creaArtigiano)            // ridisegna dopo un rifiuto
	f.rottaConGuscio(mux, "POST /settings/anagrafiche/artigiani/{id}", personale, f.modificaArtigiano)   // ridisegna dopo un rifiuto
	f.rotta(mux, "POST /settings/anagrafiche/artigiani/{id}/elimina", personale, f.eliminaArtigiano)
}
