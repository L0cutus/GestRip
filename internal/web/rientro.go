package web

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// Il riquadro «🔧 Fuori bottega» della scheda e il rientro dall'artigiano (slice 080, RIP-S-16, ART-13), come
// #repair-handed-over, #repair-artisan-notes e l'evento return-from-artisan di RepairLive (302439d), senza
// LiveView:
//   - il riquadro c'è finché la busta è fuori bottega (consegnata e non rientrata, Repair.out_of_shop?/1):
//     l'artigiano assegnato («artigiano» senza), «Consegnata il» nell'ora del negozio, «✓ Segna rientrata»
//     (un <details>, come lo standalone) con il modulo: l'avviso degli esiti non applicabili prima della
//     conferma (return_warning/1), «Stato lavoro» con «fatto» primo e scelto, la nota, «Conferma rientro»
//     con la conferma di app.js; accanto «🖨 Ristampa» e «📤 Condividi» dell'etichetta della busta (il
//     download con data-share-pdf, app.js della 046);
//   - dopo il rientro, con una nota, «📝 Note ultimo rientro da artigiano:»;
//   - POST /repairs/{id}/rientro (return[stato_lavoro], return[note_artigiano]; un campo assente è vuoto):
//     riuscito torna alla scheda con «Rientro registrato.», un rifiuto con il testo di
//     return_error_message/1, un errore con «Rientro non riuscito. Nessuna modifica applicata.» (la scheda
//     ridisegnata, il modulo aperto con l'esito e la nota mandati); un id
//     malformato, una busta che non c'è o nel cestino alla home con «Riparazione non trovata.», come le altre
//     azioni della scheda (lo standalone non ha questo ramo: la busta è quella montata). Titolare e
//     dipendente. Nessun popup Telegram dopo il rientro, come lo standalone; l'email di pronta parte nella
//     transazione del rientro (buste.RientraDallArtigiano).

const (
	msgRientroRegistrato  = "Rientro registrato."
	msgRientroNonRiuscito = "Rientro non riuscito. Nessuna modifica applicata."
)

// riquadroFuori è #repair-handed-over come lo disegna la scheda.
type riquadroFuori struct {
	Artigiano string // il nome dell'artigiano assegnato, «artigiano» senza
	Il        string // «Consegnata il», gg/mm/aaaa hh:mm in negozio
	Avviso    string // return_warning/1, "" se tutti gli esiti si possono dare
	// Il modulo: aperto e con l'esito e la nota mandati dopo un rifiuto (nello standalone il modulo resta com'era);
	// chiuso, con «fatto» scelto e la nota vuota altrimenti.
	Aperto bool
	Esito  string
	Nota   string
}

// riquadroFuoriDi è il riquadro per la busta fuori bottega, nil altrimenti.
func riquadroFuoriDi(s buste.Scheda) *riquadroFuori {
	if !s.FuoriBottega {
		return nil
	}
	f := &riquadroFuori{Artigiano: "artigiano", Avviso: avvisoDelRientro(s.Stato, s.CompraOro), Esito: buste.EsitoFatto}
	if s.Artigiano != nil {
		f.Artigiano = *s.Artigiano
	}
	if s.ConsegnataArtigianoIl != nil {
		f.Il = clock.Timbro(*s.ConsegnataArtigianoIl)
	}
	return f
}

// avvisoDelRientro è return_warning/1: tre forme sole, perché «da_rifare» è rifiutato dove lo è «fatto» e
// anche altrove.
func avvisoDelRientro(stato string, oro bool) string {
	switch esiti := buste.EsitiApplicabili(stato, oro); {
	case slices.Equal(esiti, []string{buste.EsitoDaRifare, ""}):
		return "Da «" + etichettaStato(stato) + "» l'esito «Fatto» non è applicabile: il rientro con quell'esito viene rifiutato per intero."
	case slices.Equal(esiti, []string{""}):
		return "Da «" + etichettaStato(stato) + "» nessun esito è applicabile: il rientro può solo lasciare lo stato invariato."
	}
	return ""
}

// noteDelRientro è #repair-artisan-notes: la nota dell'ultimo rientro, solo per una busta rientrata.
func noteDelRientro(s buste.Scheda) *string {
	if !s.RientrataArtigiano {
		return nil
	}
	return s.NoteArtigiano
}

// rientraBusta è POST /repairs/{id}/rientro (l'evento return-from-artisan).
func (f *facciaWeb) rientraBusta(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	}
	conSegno := f.segnoDelModulo(r, id)
	esito, _ := ultimo(r, "return[stato_lavoro]")
	nota, _ := ultimo(r, "return[note_artigiano]")
	nota = aCapoUnix(nota) // a_capo.go: la casella di LiveView nello standalone
	_, err := f.buste.RientraDallArtigiano(r.Context(), id, esito, nota, contestoDi(r))
	switch {
	case err == nil:
		st := statoDi(r)
		st.dati.Info, st.cambiata = msgRientroRegistrato, true
		f.vaiA(w, r, f.allaScheda(r, id, ""))
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
	case buste.ErroreDelRientro(err):
		f.rientroRifiutato(w, r, id, conSegno, err.Error(), esito, nota)
	default:
		f.registro.Error("rientro dall'artigiano non registrato")
		f.rientroRifiutato(w, r, id, conSegno, msgRientroNonRiuscito, esito, nota)
	}
}

// rientroRifiutato ridisegna la scheda dopo un rientro rifiutato, con il messaggio e, se la busta è ancora fuori
// bottega, il modulo aperto con l'esito e la nota mandati (un esito che il campo non può dare lascia «fatto»),
// come la modifica rifiutata (riapriBusta).
func (f *facciaWeb) rientroRifiutato(w http.ResponseWriter, r *http.Request, id int64, conSegno bool, errore, esito, nota string) {
	s, err := f.leggiDopoRifiuto(r, id, conSegno)
	switch {
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	case errors.Is(err, buste.ErrAperturaNonRiuscita):
		f.registro.Error("apertura della busta non registrata")
		f.vaiAConErrore(w, r, "/", msgBustaNonAperta)
		return
	case err != nil:
		f.registro.Error("busta non letta")
		Errore(w, r)
		return
	}
	d, err := datiDellaScheda(s, titolareDi(r))
	if err != nil {
		f.registro.Error("busta con una data illeggibile")
		Errore(w, r)
		return
	}
	if m := d.FuoriBottega; m != nil {
		m.Aperto, m.Nota = true, nota
		if e := strings.TrimSpace(esito); e == buste.EsitoFatto || e == buste.EsitoDaRifare || e == "" {
			m.Esito = e
		}
	}
	f.disegnaScheda(w, r, errore, "", d)
}
