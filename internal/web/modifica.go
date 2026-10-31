package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/buste"
)

// La modifica della busta dalla scheda (RIP-S-04…12), come gli eventi edit, save, cancel e
// clear-expected-on di RepairLive e open_edit_from_query/3 dello standalone (go-baseline), senza
// LiveView:
//   - GET /repairs/{id}?modifica=1 è «Modifica»: la scheda con il modulo al posto del riquadro dei
//     dati; ?edit=prezzo e ?edit=preventivo (il popup del QR) lo aprono con il cursore nel prezzo
//     finale o nel preventivo; ogni altro valore apre la scheda. È un'apertura della scheda: una
//     riga repair.viewed, come ogni GET;
//   - POST /repairs/{id}/modifica è «Salva modifiche» (l'evento save): si leggono solo i campi di
//     @edit_fields; riuscito torna alla scheda con «Riparazione aggiornata.», un rifiuto ridisegna
//     il modulo con i valori scritti, gli errori sotto i campi o il messaggio dello standalone;
//   - POST /repairs/{id}/togli-data è «Togli data» della scheda (l'evento clear-expected-on);
//   - «Annulla» è un link alla scheda.
//
// Un doppio invio dello stesso modulo non ha bisogno dell'identificativo della nuova busta: la
// seconda modifica trova la busta già cambiata e non cambia niente (scrive solo un altro
// repair.updated vuoto); app.js spegne comunque il pulsante al primo invio, come phx-disable-with.

const (
	msgModificaNonSalvata = "Salvataggio non riuscito. Nessuna modifica applicata."
	msgModificaSalvata    = "Riparazione aggiornata."
	msgDataTolta          = "Data di consegna tolta."
	msgDataNonTolta       = "Data non tolta. Nessuna modifica applicata."
)

// campiModifica sono i campi del modulo di modifica (@edit_fields), con il nome di repair[…].
var campiModifica = []string{"accepted_on", "expected_on", "description", "public_notes", "internal_notes", "estimate_euros",
	"final_price_euros", "oreficeria_id", "artisan_id", "qr_code", "customer_name_snapshot", "customer_phone_snapshot",
	"compro_oro", "extra_days", "expected_on_text"}

// campoModificaDelDominio dà il campo del modulo di ogni errore dei campi della modifica.
var campoModificaDelDominio = map[string]string{
	"data_accettazione": "accepted_on", "descrizione": "description", "data_prevista": "expected_on",
	"data_prevista_testo": "expected_on_text", "preventivo": "estimate_euros", "prezzo_finale": "final_price_euros",
	"nome_cliente": "customer_name_snapshot", "telefono_cliente": "customer_phone_snapshot", "giorni_in_piu": "extra_days",
}

// datiModulo è il modulo «Modifica riparazione» come lo disegna la pagina.
type datiModulo struct {
	Valori           map[string]string   // i valori dei campi, per nome
	Nulli            map[string]bool     // i campi senza valore (NULL): niente attributo value, come Phoenix
	Errori           map[string][]string // gli errori sotto i campi, per nome
	Fuoco            string              // il campo con il cursore (?edit=), "" se nessuno
	TelefonoBloccato bool                // busta conto terzi: il telefono del cliente finale è spento
	TestoNascosto    bool                // la busta ha un testo di consegna: viaggia nel campo nascosto
	CompraOro        bool                // la casella è segnata
	TipoMostrato     string              // "true" o "false": il tipo della busta quando la pagina è stata disegnata
	Suggerimento     string              // cosa farebbe la casella segnata (kind_hint_text/1), "" se niente
	Oreficerie       []opzione           // vuoto: niente menu «Oreficeria cliente»
	Artigiani        []opzione
}

// valoriDellaBusta sono i valori del modulo per la busta com'è (Repair.edit_changeset(repair,
// %{})), e i campi che non ne hanno: Phoenix non scrive l'attributo value di un campo nil.
func valoriDellaBusta(s buste.Scheda) (map[string]string, map[string]bool) {
	nulli := map[string]bool{}
	testo := func(campo string, p *string) string {
		if p == nil {
			nulli[campo] = true
			return ""
		}
		return *p
	}
	id := func(p *int64) string {
		if p == nil {
			return ""
		}
		return strconv.FormatInt(*p, 10)
	}
	prezzo := func(campo string, p buste.Prezzo) string {
		v, ok := prezzoNelModulo(p)
		if !ok {
			nulli[campo] = true
		}
		return v
	}
	return map[string]string{
		"customer_name_snapshot": s.NomeCliente, "customer_phone_snapshot": s.TelefonoCliente,
		"accepted_on": s.DataAccettazione, "description": s.Descrizione,
		"public_notes": testo("public_notes", s.NotePubbliche), "internal_notes": testo("internal_notes", s.NoteInterne),
		"estimate_euros": prezzo("estimate_euros", s.Preventivo), "final_price_euros": prezzo("final_price_euros", s.PrezzoFinale),
		"expected_on": testo("expected_on", s.Consegna.Data), "expected_on_text": testo("expected_on_text", s.Consegna.Testo),
		"extra_days": strconv.FormatInt(s.GiorniInPiu, 10), "qr_code": testo("qr_code", s.CodiceQR),
		"oreficeria_id": id(s.OreficeriaID), "artisan_id": id(s.ArtigianoID),
	}, nulli
}

// prezzoNelModulo è price_input/2: il testo scritto, poi i centesimi come «123.45», poi niente
// (false).
func prezzoNelModulo(p buste.Prezzo) (string, bool) {
	switch {
	case p.Testo != nil && *p.Testo != "":
		return *p.Testo, true
	case p.Centesimi != nil:
		return buste.Euro(*p.Centesimi), true
	}
	return "", false
}

// valoreCompraOro è kind_param/2: "true" e "false" dicono il tipo; altro, o la casella che non
// arriva, lasciano quello della busta.
func valoreCompraOro(scritto string, arrivato, attuale bool) bool {
	switch {
	case arrivato && scritto == "true":
		return true
	case arrivato && scritto == "false":
		return false
	}
	return attuale
}

// suggerimentoTipo è kind_hint_text/1 per la casella com'è: niente se il tipo è quello della
// busta o se lo stato resta; l'annuncio del riallineamento; il rifiuto.
func suggerimentoTipo(s buste.Scheda, oro bool) string {
	switch c := buste.CambioDiTipo(s, oro); c.Esito {
	case buste.TipoRiallinea:
		return "Lo stato «" + etichettaStato(c.Da) + "» passerà a «" + etichettaStato(c.A) + "»."
	case buste.TipoRifiutato:
		return string(c.Rifiuto)
	}
	return ""
}

// moduloDellaBusta prepara il modulo con i valori dati (quelli della busta, o quelli scritti dopo
// un rifiuto); nei menu è segnata la voce del valore solo se il valore c'è.
func (f *facciaWeb) moduloDellaBusta(ctx context.Context, s buste.Scheda, valori map[string]string, nulli map[string]bool) (datiModulo, error) {
	sc, err := f.buste.Scelte(ctx)
	if err != nil {
		return datiModulo{}, err
	}
	m := datiModulo{Valori: valori, Nulli: nulli, Errori: map[string][]string{}, TelefonoBloccato: s.B2B(), TestoNascosto: s.Consegna.Testo != nil,
		CompraOro: s.CompraOro, TipoMostrato: strconv.FormatBool(s.CompraOro)}
	if len(sc.Oreficerie) > 0 {
		m.Oreficerie = opzioni("— Diretta "+sc.NomeNegozio, sc.Oreficerie, valori["oreficeria_id"], valori["oreficeria_id"] != "")
	}
	m.Artigiani = opzioni("— Nessuno —", sc.Artigiani, valori["artisan_id"], valori["artisan_id"] != "")
	return m, nil
}

// apriModulo dice se la GET della scheda apre il modulo, e con il cursore dove: ?edit=prezzo o
// ?edit=preventivo (l'ultimo valore, come i parametri di Phoenix) o ?modifica=1.
func apriModulo(r *http.Request) (bool, string) {
	q := r.URL.Query()
	switch ultimoDi(q, "edit") {
	case "prezzo":
		return true, "final_price_euros"
	case "preventivo":
		return true, "estimate_euros"
	}
	return ultimoDi(q, "modifica") == "1", ""
}

// salvaModifica è POST /repairs/{id}/modifica (l'evento save).
func (f *facciaWeb) salvaModifica(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	}
	valori, inviati := map[string]string{}, map[string]bool{}
	for _, c := range campiModifica {
		if v, ok := ultimo(r, "repair["+c+"]"); ok {
			valori[c], inviati[c] = v, true
		}
	}
	campo := func(c string) *string {
		if !inviati[c] {
			return nil
		}
		v := valori[c]
		return &v
	}
	m := buste.Modifica{NomeCliente: campo("customer_name_snapshot"), TelefonoCliente: campo("customer_phone_snapshot"),
		DataAccettazione: campo("accepted_on"), Descrizione: campo("description"), NotePubbliche: campo("public_notes"),
		NoteInterne: campo("internal_notes"), Preventivo: campo("estimate_euros"), PrezzoFinale: campo("final_price_euros"),
		DataPrevista: campo("expected_on"), DataPrevistaTesto: campo("expected_on_text"), GiorniInPiu: campo("extra_days"),
		CodiceQR: campo("qr_code"), OreficeriaID: campo("oreficeria_id"), ArtigianoID: campo("artisan_id")}
	// Solo un gesto cambia il tipo: la casella spostata dal tipo che la pagina mostrava. Senza il
	// tipo mostrato decide la busta salvata (la modifica confronta con quella).
	mostrato, conMostrato := tipoMostrato(r)
	var gesto *bool
	if v := valori["compro_oro"]; inviati["compro_oro"] && (v == "true" || v == "false") {
		oro := v == "true"
		if !conMostrato || oro != mostrato {
			gesto = &oro
		}
	}
	esito, err := f.buste.Modifica(r.Context(), id, m, gesto, contestoDi(r))
	var campi buste.Campi
	var rifiuto buste.Rifiuto
	switch {
	case err == nil:
		info := msgModificaSalvata
		if esito.TipoCambiato && esito.StatoPrima != esito.StatoDopo {
			info = "Riparazione aggiornata. Tipo busta cambiato: stato riallineato a «" + etichettaStato(esito.StatoDopo) + "»."
		}
		st := statoDi(r)
		st.dati.Info, st.cambiata = info, true
		f.vaiA(w, r, "/repairs/"+strconv.FormatInt(id, 10))
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
	case errors.As(err, &campi):
		errori := map[string][]string{}
		for k, mm := range campi {
			if c, ok := campoModificaDelDominio[k]; ok && inviati[c] {
				errori[c] = mm
			}
		}
		f.ridisegnaModifica(w, r, id, valori, inviati, "", errori)
	case errors.As(err, &rifiuto):
		f.ridisegnaModifica(w, r, id, valori, inviati, string(rifiuto), nil)
	default:
		if !errors.Is(err, buste.ErrOreficeriaNonValida) {
			f.registro.Error("busta non modificata")
		}
		f.ridisegnaModifica(w, r, id, valori, inviati, msgModificaNonSalvata, nil)
	}
}

// tipoMostrato è il campo nascosto tipo_mostrato, se è uno dei due valori che la pagina scrive.
func tipoMostrato(r *http.Request) (bool, bool) {
	switch v, _ := ultimo(r, "tipo_mostrato"); v {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// ridisegnaModifica rimostra il modulo dopo un rifiuto, come lo lascia lo standalone: i valori
// scritti (quelli che non sono arrivati dalla busta), il telefono spento se l'oreficeria scelta è
// esterna, la casella come l'ha lasciata chi scrive con il suo avviso, nessun cursore spostato. La
// busta si rilegge senza registrare un'apertura.
func (f *facciaWeb) ridisegnaModifica(w http.ResponseWriter, r *http.Request, id int64, scritti map[string]string, inviati map[string]bool, errore string, errori map[string][]string) {
	s, err := f.buste.Scheda(r.Context(), id)
	if errors.Is(err, buste.ErrBustaNonTrovata) {
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	}
	if err != nil {
		f.registro.Error("busta non letta")
		Errore(w, r)
		return
	}
	d, err := datiDellaScheda(s)
	if err != nil {
		f.registro.Error("busta con una data illeggibile")
		Errore(w, r)
		return
	}
	valori, nulli := valoriDellaBusta(s)
	for c := range inviati {
		valori[c] = scritti[c]
		delete(nulli, c)
	}
	for _, c := range []string{"oreficeria_id", "artisan_id"} {
		if inviati[c] {
			valori[c], _ = risolto(valori[c])
		}
	}
	m, err := f.moduloDellaBusta(r.Context(), s, valori, nulli)
	if err != nil {
		Errore(w, r)
		return
	}
	if inviati["oreficeria_id"] {
		// phone_locked?/2: la relazione come l'ha appena scelta chi scrive, con Integer.parse/1 senza
		// resto (niente spazi; un numero oltre i 64 bit è comunque un'oreficeria esterna).
		v := scritti["oreficeria_id"]
		n, err := strconv.ParseInt(v, 10, 64)
		m.TelefonoBloccato = (err == nil && n > 1) || (errors.Is(err, strconv.ErrRange) && !strings.HasPrefix(v, "-"))
	}
	if m.TelefonoBloccato {
		m.Valori["customer_phone_snapshot"] = s.TelefonoCliente
	}
	m.CompraOro = valoreCompraOro(scritti["compro_oro"], inviati["compro_oro"], s.CompraOro)
	m.Suggerimento = suggerimentoTipo(s, m.CompraOro)
	if v, ok := tipoMostrato(r); ok {
		m.TipoMostrato = strconv.FormatBool(v)
	}
	if errori != nil {
		m.Errori = errori
	}
	d.Modulo = &m
	f.mostraCon(w, r, "scheda", http.StatusOK, errore, d)
}

// togliData è POST /repairs/{id}/togli-data («Togli data» della scheda, clear-expected-on).
func (f *facciaWeb) togliData(w http.ResponseWriter, r *http.Request) {
	id, ok := idPositivo(r.PathValue("id"))
	if !ok {
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
		return
	}
	dove := "/repairs/" + strconv.FormatInt(id, 10)
	switch err := f.buste.TogliData(r.Context(), id, contestoDi(r)); {
	case err == nil:
		st := statoDi(r)
		st.dati.Info, st.cambiata = msgDataTolta, true
		f.vaiA(w, r, dove)
	case errors.Is(err, buste.ErrBustaNonTrovata):
		f.vaiAConErrore(w, r, "/", msgBustaNonTrovata)
	default:
		f.registro.Error("data di consegna non tolta")
		f.vaiAConErrore(w, r, dove, msgDataNonTolta)
	}
}
