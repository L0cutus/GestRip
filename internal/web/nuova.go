package web

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// La pagina «Nuova riparazione» (RIP-H-29…39, 41), come render(%{live_action: :new}) e
// save_repair/3 di WorkshopLive dello standalone, senza LiveView e senza JavaScript obbligatorio:
//   - GET /nuova disegna il modulo con il numero proposto e la data di oggi in negozio; carica solo
//     le scelte dei menu (nessuna lettura della lista, nessun audit);
//   - POST /nuova legge solo i campi di @intake_fields (repair[…]): ogni altro campo si scarta;
//   - l'avviso del QR mancante (slice 209) è uno stato della pagina nello standalone (qr_warned):
//     qui viaggia nel campo nascosto qr_avvisato, che il modulo porta solo dopo l'avviso; una
//     pagina nuova non lo ha, quindi l'avviso vale per una busta;
//   - gli errori ridisegnano il modulo con i valori scritti; un salvataggio riuscito torna alla
//     home con «Riparazione N creata.» (push_patch a / nello standalone).
//
// Il link pubblico della busta (<indirizzo_pubblico>/r/<token>, «Link cliente» o «Link
// dell'oreficeria») si mostra una volta sulla home, dal deposito in memoria (linkunavolta.go): mai
// nella risposta al POST, nel redirect, nel cookie di sessione (leggibile) o nel registro.

// Buste è quello che le pagine chiedono alle buste (internal/buste).
type Buste interface {
	ProssimoNumero(ctx context.Context) (string, error)
	Scelte(ctx context.Context) (buste.Scelte, error)
	Crea(ctx context.Context, a buste.Accettazione, c audit.Contesto) (buste.Creata, error)
	ApriScheda(ctx context.Context, id string, c audit.Contesto) (buste.Scheda, error)
	Scheda(ctx context.Context, id int64) (buste.Scheda, error)
	Modifica(ctx context.Context, id int64, m buste.Modifica, compraOro *bool, c audit.Contesto) (buste.Modificata, error)
	TogliData(ctx context.Context, id int64, c audit.Contesto) error
	CambiaStato(ctx context.Context, id int64, stato string, c audit.Contesto) (buste.Cambio, error)
	Riapri(ctx context.Context, id int64, stato, motivo string, c audit.Contesto) (buste.Cambio, error)
	Attesa(ctx context.Context, id int64, azione, motivo string, c audit.Contesto) (bool, error)
	RevocaLink(ctx context.Context, id int64, c audit.Contesto) (bool, error)
	NuovoLink(ctx context.Context, id int64, c audit.Contesto) (buste.LinkNuovo, error)
	LeggiFotoPersonale(ctx context.Context, busta, foto string, v buste.Variante, c audit.Contesto) (*buste.MediaFoto, error)
	LeggiMiniaturaCoperta(ctx context.Context, busta, foto string, c audit.Contesto) (*buste.MediaFoto, error)
}

// campiBusta sono i campi del modulo (@intake_fields), con il nome di repair[…].
var campiBusta = []string{"customer_name", "customer_phone", "oreficeria_id", "artisan_id", "bag_number",
	"accepted_on", "expected_on", "description", "public_notes", "internal_notes", "estimate_euros", "compro_oro", "qr_code"}

// campoDelDominio dà il campo del modulo di ogni errore dei campi del dominio (buste.Campi).
var campoDelDominio = map[string]string{
	"numero": "bag_number", "data_accettazione": "accepted_on", "data_prevista": "expected_on",
	"descrizione": "description", "preventivo": "estimate_euros",
}

// opzione è una voce di un menu del modulo.
type opzione struct {
	Valore, Etichetta string
	Scelta            bool
}

// datiNuova è quello che la pagina riceve.
type datiNuova struct {
	Valori     map[string]string   // i valori dei campi, per nome
	Errori     map[string][]string // gli errori sotto i campi, per nome
	CompraOro  bool                // la spunta è segnata
	AvvisoQR   bool                // l'avviso del QR mancante è già stato dato (qr_warned)
	Invio      string              // l'identificativo monouso dell'invio (esiti.go)
	Oreficerie []opzione           // vuoto: niente menu «Oreficeria cliente»
	Artigiani  []opzione
}

// moduloNuova prepara la pagina con i valori dati; nei menu è segnata la voce del valore solo se
// il valore c'è (segnati), come options_for_select con un valore nil.
func (f *facciaWeb) moduloNuova(ctx context.Context, valori map[string]string, segnati map[string]bool, invio string) (datiNuova, error) {
	s, err := f.buste.Scelte(ctx)
	if err != nil {
		return datiNuova{}, err
	}
	d := datiNuova{Valori: valori, Errori: map[string][]string{}, CompraOro: valori["compro_oro"] == "true", Invio: invio}
	if len(s.Oreficerie) > 0 {
		d.Oreficerie = opzioni("— Diretta "+s.NomeNegozio, s.Oreficerie, valori["oreficeria_id"], segnati["oreficeria_id"])
	}
	d.Artigiani = opzioni("— Nessuno —", s.Artigiani, valori["artisan_id"], segnati["artisan_id"])
	return d, nil
}

// opzioni è options_for_select: la prima voce vuota, poi le voci; se segna, è segnata quella
// uguale al valore, anche la vuota.
func opzioni(prima string, voci []buste.Voce, valore string, segna bool) []opzione {
	o := []opzione{{Valore: "", Etichetta: prima}}
	for _, v := range voci {
		o = append(o, opzione{Valore: strconv.FormatInt(v.ID, 10), Etichetta: v.Nome})
	}
	if segna {
		for i := range o {
			o[i].Scelta = o[i].Valore == valore
		}
	}
	return o
}

// reInvio è la forma dell'identificativo di un invio (casuale(): 22 caratteri base64url).
var reInvio = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)

// risolto è il valore di un menu come lo rimostra il changeset dopo la creazione: l'id risolto
// (nil per la voce vuota) e se c'è.
func risolto(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return s, true
	}
	return strconv.FormatInt(n, 10), true
}

// paginaNuova è GET /nuova: il modulo vuoto con il numero proposto e la data di oggi in negozio
// (repair_form/0).
func (f *facciaWeb) paginaNuova(w http.ResponseWriter, r *http.Request) {
	numero, err := f.buste.ProssimoNumero(r.Context())
	if err != nil {
		Errore(w, r)
		return
	}
	d, err := f.moduloNuova(r.Context(), map[string]string{"bag_number": numero, "accepted_on": clock.Oggi(f.orologio).String()}, nil, casuale())
	if err != nil {
		Errore(w, r)
		return
	}
	f.mostraCon(w, r, "nuova", http.StatusOK, "", d)
}

// generaBusta è POST /nuova (l'evento save_repair).
func (f *facciaWeb) generaBusta(w http.ResponseWriter, r *http.Request) {
	valori := map[string]string{}
	inviati := map[string]bool{}
	for _, c := range campiBusta {
		valori[c], inviati[c] = ultimo(r, "repair["+c+"]")
	}
	avvisato := r.PostForm.Get("qr_avvisato") == "1"
	invio, _ := ultimo(r, "invio")
	if !reInvio.MatchString(invio) {
		invio = casuale() // un modulo senza identificativo: da qui in poi ne ha uno
	}
	ridisegna := func(errore string, errori map[string][]string, avviso bool, segnati map[string]bool) {
		d, err := f.moduloNuova(r.Context(), valori, segnati, invio)
		if err != nil {
			Errore(w, r)
			return
		}
		d.AvvisoQR = avviso
		if errori != nil {
			d.Errori = errori
		}
		f.mostraCon(w, r, "nuova", http.StatusOK, errore, d)
	}
	// Repair.blank_qr?/1: un QR vuoto o di soli spazi chiede una volta, senza salvare né validare.
	if strings.TrimSpace(valori["qr_code"]) == "" && !avvisato {
		ridisegna("", nil, true, inviati)
		return
	}
	// Lo stesso invio una seconda volta (anche insieme alla prima) non crea un'altra busta: aspetta
	// la prima e porta al suo esito.
	st := statoDi(r)
	a, primo := f.esiti.prenota(invio, st.dati.Utente, st.dati.Nonce)
	for !primo {
		esito, err := f.esiti.aspetta(r.Context(), a)
		if err != nil {
			Errore(w, r) // il client se n'è andato
			return
		}
		if esito != "" {
			f.vaiA(w, r, "/?esito="+esito)
			return
		}
		a, primo = f.esiti.prenota(invio, st.dati.Utente, st.dati.Nonce)
	}
	esito := ""
	defer func() { f.esiti.chiudi(a, esito) }() // anche con un panic: chi aspetta non resta appeso
	creata, err := f.buste.Crea(r.Context(), buste.Accettazione{
		NomeCliente: valori["customer_name"], TelefonoCliente: valori["customer_phone"],
		OreficeriaID: valori["oreficeria_id"], ArtigianoID: valori["artisan_id"], Numero: valori["bag_number"],
		DataAccettazione: valori["accepted_on"], DataPrevista: valori["expected_on"], Descrizione: valori["description"],
		NotePubbliche: valori["public_notes"], NoteInterne: valori["internal_notes"], Preventivo: valori["estimate_euros"],
		CompraOro: valori["compro_oro"], CodiceQR: valori["qr_code"],
	}, contestoDi(r))
	var rifiutati *buste.CampiRifiutati
	var rifiuto buste.Rifiuto
	switch {
	case err == nil:
		l := f.linkPubblico(creata.Token, creata.OreficeriaID != nil) // Oreficeria.is_b2b/1
		esito = f.esiti.metti(st.dati.Utente, st.dati.Nonce, esitoBusta{Numero: creata.Numero, Link: l})
		f.vaiA(w, r, "/?esito="+esito)
	case errors.As(err, &rifiutati):
		// Il changeset dello standalone porta i valori risolti dalla creazione (protected_attrs): il
		// numero assegnato o scritto senza spazi, oreficeria e artigiano come id (nil la voce
		// vuota), compro oro come booleano. Gli errori si vedono solo per i campi arrivati
		// (used_input?/1); il numero c'è sempre.
		valori["bag_number"], inviati["bag_number"] = rifiutati.Numero, true
		segnati := map[string]bool{}
		for _, c := range []string{"oreficeria_id", "artisan_id"} {
			valori[c], segnati[c] = risolto(valori[c])
		}
		if buste.CompraOroDalModulo(valori["compro_oro"]) {
			valori["compro_oro"] = "true"
		} else {
			valori["compro_oro"] = "false"
		}
		errori := map[string][]string{}
		for k, mm := range rifiutati.Campi {
			if c, ok := campoDelDominio[k]; ok && inviati[c] {
				errori[c] = mm
			}
		}
		ridisegna("", errori, avvisato, segnati)
	case errors.As(err, &rifiuto):
		ridisegna(string(rifiuto), nil, avvisato, inviati)
	default:
		f.registro.Error("busta non salvata")
		ridisegna(buste.MessaggioNonSalvata, nil, avvisato, inviati)
	}
}
