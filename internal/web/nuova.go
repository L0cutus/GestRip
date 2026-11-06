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
//   - GET /nuova disegna il modulo con la data di oggi in negozio; carica solo le scelte dei menu
//     (nessuna lettura della lista, nessun audit);
//   - POST /nuova legge solo i campi di @intake_fields (repair[…]) tolto bag_number: ogni altro campo
//     si scarta;
//   - D-64 (slice 053): niente campo «N° Busta». Il numero interno lo assegna sempre il contatore
//     dell'anno della data di accettazione, e un repair[bag_number] mandato da un modulo vecchio o
//     forgiato si ignora (risposta 8 di Stefano, 05/11/2026). Il campo del codice QR è «Etichetta
//     (0045/26)» con il 📷: il codice letto, o il numero scritto a mano che la creazione risolve
//     (buste.EtichettaScritta). Un'etichetta già su una busta aperta si avvisa una volta (campo
//     nascosto gemelle_avvisate con il codice risolto, come qr_avvisato) e poi si salva lo stesso;
//     dopo il salvataggio la home dice «Busta 0045/26 salvata» (studio D-64, punto (d)) con il numero
//     visibile (buste.NumeroVisibile), al posto di «Riparazione N creata.» dello standalone;
//   - l'avviso del QR mancante (slice 209) è uno stato della pagina nello standalone (qr_warned):
//     qui viaggia nel campo nascosto qr_avvisato, che il modulo porta solo dopo l'avviso; una
//     pagina nuova non lo ha, quindi l'avviso vale per una busta;
//   - gli errori ridisegnano il modulo con i valori scritti; un salvataggio riuscito torna alla
//     home con «Busta 0045/26 salvata» (push_patch a / nello standalone, con «Riparazione N creata.»).
//
// Il link pubblico della busta (<indirizzo_pubblico>/r/<token>, «Link cliente» o «Link
// dell'oreficeria») si mostra una volta sulla home, dal deposito in memoria (linkunavolta.go): mai
// nella risposta al POST, nel redirect, nel cookie di sessione (leggibile) o nel registro.
//
// Le foto scelte prima di generare la busta (RIP-H-40, slice 040b) non arrivano mai qui: restano nel
// browser e la coda di app.js le manda dopo a POST /repairs/{id}/photos (coda_foto.go). Il modulo
// porta la sua chiave casuale (intake_key, come la chiave nel socket dello standalone: nuova a ogni
// pagina, la stessa dopo un rifiuto o l'avviso del QR) e quante foto tiene il browser
// (intake_photos). Una busta salvata con delle foto mette nell'esito id, numero e chiave
// (fotoDaMandare): la home li disegna una volta, e il browser manda le foto solo se la chiave è
// quella del modulo da cui le ha scelte. La chiave non va in un indirizzo né nel registro.

// Buste è quello che le pagine chiedono alle buste (internal/buste).
type Buste interface {
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
	// Le foto della scheda (foto_scheda.go).
	ElencoFoto(ctx context.Context, busta int64) ([]buste.Foto, error)
	AllegaFoto(ctx context.Context, busta int64, cs []buste.Caricamento, s *buste.Sessione, c audit.Contesto) ([]buste.Foto, error)
	ImpostaVisibilita(ctx context.Context, busta, foto int64, pubblico string, visibile bool, c audit.Contesto) (buste.Foto, error)
	RimuoviFoto(ctx context.Context, busta, foto int64, c audit.Contesto) error
	// La coda delle foto di «Nuova riparazione» (coda_foto.go), con l'identificativo d'invio (D-58).
	EsisteBusta(ctx context.Context, busta int64) (bool, error)
	AllegaFotoConInvio(ctx context.Context, busta int64, x buste.Caricamento, invio string, s *buste.Sessione, c audit.Contesto) (buste.Foto, bool, error)
	// La ricevuta PDF (ricevuta.go).
	Ricevuta(ctx context.Context, id string, c audit.Contesto, fai func(buste.DatiRicevuta) ([]byte, error)) ([]byte, buste.DatiRicevuta, error)
	// L'etichetta busta e la scheda PDF (documenti.go).
	Etichetta(ctx context.Context, id string, c audit.Contesto, fai func(buste.DatiEtichetta) ([]byte, error)) ([]byte, buste.DatiEtichetta, error)
	SchedaPDF(ctx context.Context, id string, c audit.Contesto, fai func(buste.DatiSchedaPDF) ([]byte, error)) ([]byte, buste.DatiSchedaPDF, error)
	// Il report multi-scheda A4 (report.go).
	Report(ctx context.Context, ids []int64, c audit.Contesto, fai func(buste.DatiReport) ([]byte, error)) ([]byte, buste.DatiReport, error)
	IdsDaiFiltri(ctx context.Context, f buste.Filtri) ([]int64, error)
	// Le etichette QR prenumerate (etichette_qr.go).
	ModuloEtichette(ctx context.Context) (buste.ModuloEtichette, error)
	GeneraEtichette(ctx context.Context, f buste.FoglioEtichette, c audit.Contesto, fai func(buste.FoglioEtichette) ([]byte, error)) ([]byte, error)
	AzzeraEtichette(ctx context.Context, nuovoAnno bool, anno int64, c audit.Contesto) error
	// La stampa diretta (stampanti.go).
	ScelteStampa(ctx context.Context) (buste.ScelteStampa, error)
	SalvaScelteStampa(ctx context.Context, s buste.ScelteStampa, c audit.Contesto) error
	StampaEtichetteAccettata(ctx context.Context, f buste.FoglioEtichette, coda, lavoro string, c audit.Contesto) (int64, error)
	EtichettaDaStampare(ctx context.Context, id string, fai func(buste.DatiEtichetta) ([]byte, error)) ([]byte, buste.DatiEtichetta, error)
	StampaEtichettaAccettata(ctx context.Context, e buste.DatiEtichetta, coda, lavoro string, c audit.Contesto) error
	StampaEtichetteNonSalvata(ctx context.Context, f buste.FoglioEtichette, coda, lavoro string, c audit.Contesto) error
	StampaEtichettaNonSalvata(ctx context.Context, e buste.DatiEtichetta, coda, lavoro string, c audit.Contesto) error
	// La firma di consegna (firma.go).
	ApriFirma(ctx context.Context, id string) (buste.PaginaFirma, error)
	CatturaFirma(ctx context.Context, id, campo, ritirata string, c audit.Contesto) error
	LeggiFirma(ctx context.Context, id string, c audit.Contesto) ([]byte, error)
	AzzeraFirma(ctx context.Context, id string, c audit.Contesto) error
	// La lista della home (home.go).
	Lista(ctx context.Context, f buste.Filtri) (buste.Lista, error)
	RegistraLista(ctx context.Context, chiavi []string, c audit.Contesto) error
	// La ricerca della home (slice 052): la selezione per smistare una ricerca scritta e la scansione.
	Selezione(ctx context.Context, f buste.Filtri, limite int) (int64, []int64, error)
	CercaPerQR(ctx context.Context, codice string, ancheNessuna bool, c audit.Contesto) ([]buste.RigaQR, error)
}

// campiBusta sono i campi del modulo (@intake_fields), con il nome di repair[…].
// bag_number non c'è (D-64, slice 053): il numero interno non si sceglie più dal modulo.
var campiBusta = []string{"customer_name", "customer_phone", "oreficeria_id", "artisan_id",
	"accepted_on", "expected_on", "description", "public_notes", "internal_notes", "estimate_euros", "compro_oro", "qr_code"}

// campoDelDominio dà il campo del modulo di ogni errore dei campi del dominio (buste.Campi).
var campoDelDominio = map[string]string{
	"data_accettazione": "accepted_on", "data_prevista": "expected_on",
	"descrizione": "description", "preventivo": "estimate_euros",
}

// opzione è una voce di un menu del modulo.
type opzione struct {
	Valore, Etichetta string
	Scelta            bool
}

// datiNuova è quello che la pagina riceve.
type datiNuova struct {
	Valori    map[string]string   // i valori dei campi, per nome
	Errori    map[string][]string // gli errori sotto i campi, per nome
	CompraOro bool                // la spunta è segnata
	AvvisoQR  bool                // l'avviso del QR mancante è già stato dato (qr_warned)
	// MostraAvvisoQR: l'avviso del QR mancante si vede. Non insieme all'avviso delle gemelle, che
	// vuole un QR (review della 053): lì resta solo il campo nascosto qr_avvisato.
	MostraAvvisoQR bool
	Gemelle        *avvisoGemelle // l'avviso dell'etichetta già su una busta aperta (D-64)
	// GemelleAvvisate è il codice già avvisato (gemelle_avvisate), che il modulo rimanda: quello
	// dell'avviso appena dato, o quello arrivato, così un errore dei campi non fa ripetere l'avviso.
	GemelleAvvisate string
	Invio           string    // l'identificativo monouso dell'invio (esiti.go)
	Chiave          string    // la chiave del modulo per le foto scelte prima (RIP-H-40)
	Oreficerie      []opzione // vuoto: niente menu «Oreficeria cliente»
	Artigiani       []opzione
	Bottega         string // il nome del negozio per l'intestazione della bottega (shop_header/1)
}

// avvisoGemelle è l'avviso dell'etichetta già usata da buste aperte (D-64, slice 053): il codice
// risolto, che il modulo rimanda in gemelle_avvisate, e le buste come «Cliente (0045/26)».
type avvisoGemelle struct {
	Codice string
	Buste  string // «Cliente A (0046/26) e Cliente B (0046/26)»
	Più    bool   // più di una: «dalle buste aperte»
}

// nuovoAvvisoGemelle scrive l'avviso delle gemelle aperte g.
func nuovoAvvisoGemelle(g *buste.GemelleAperte) *avvisoGemelle {
	nomi := make([]string, 0, len(g.Righe))
	for _, r := range g.Righe {
		n, _ := r.NumeroVisibile()
		nomi = append(nomi, r.NomeCliente+" ("+n+")")
	}
	testo := nomi[len(nomi)-1]
	if len(nomi) > 1 {
		testo = strings.Join(nomi[:len(nomi)-1], ", ") + " e " + testo
	}
	return &avvisoGemelle{Codice: g.Codice, Buste: testo, Più: len(nomi) > 1}
}

// bottega è il nome del negozio dell'intestazione della bottega (conBottega): lo standalone disegna
// shop_header/1 anche su «Nuova riparazione».
func (d datiNuova) bottega() string { return d.Bottega }

// moduloNuova prepara la pagina con i valori dati; nei menu è segnata la voce del valore solo se
// il valore c'è (segnati), come options_for_select con un valore nil.
func (f *facciaWeb) moduloNuova(ctx context.Context, valori map[string]string, segnati map[string]bool, invio, chiave string) (datiNuova, error) {
	s, err := f.buste.Scelte(ctx)
	if err != nil {
		return datiNuova{}, err
	}
	d := datiNuova{Valori: valori, Errori: map[string][]string{}, CompraOro: valori["compro_oro"] == "true", Invio: invio, Chiave: chiave, Bottega: s.NomeNegozio}
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

// reConteggio è un intero senza spazi, con il segno + facoltativo (Integer.parse/1 con il resto
// vuoto, senza il segno meno, che non è mai positivo).
var reConteggio = regexp.MustCompile(`^\+?[0-9]+$`)

// fotoScelte è intake_photo_count/1 > 0: il browser dice di avere delle foto per questa busta. Un
// intero di qualunque grandezza vale, come in Elixir.
func fotoScelte(s string) bool {
	return reConteggio.MatchString(s) && strings.Trim(strings.TrimPrefix(s, "+"), "0") != ""
}

// paginaNuova è GET /nuova: il modulo vuoto con la data di oggi in negozio (repair_form/0, senza il
// numero proposto: D-64).
func (f *facciaWeb) paginaNuova(w http.ResponseWriter, r *http.Request) {
	d, err := f.moduloNuova(r.Context(), map[string]string{"accepted_on": clock.Oggi(f.orologio).String()}, nil, casuale(), casuale())
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
	gemelleAvvisate, _ := ultimo(r, "gemelle_avvisate")
	invio, _ := ultimo(r, "invio")
	if !reInvio.MatchString(invio) {
		invio = casuale() // un modulo senza identificativo: da qui in poi ne ha uno
	}
	// La chiave del modulo (intake_key dello standalone): resta finché il modulo non è salvato. Una
	// chiave mancante o mandata male non si ripete: il modulo ne ha una nuova, e il browser toglie le
	// foto scelte sotto quella di prima.
	chiave, _ := ultimo(r, "intake_key")
	if !reInvio.MatchString(chiave) {
		chiave = casuale()
	}
	conteggio, _ := ultimo(r, "intake_photos")
	var gemelle *avvisoGemelle
	ridisegna := func(errore string, errori map[string][]string, avviso bool, segnati map[string]bool) {
		d, err := f.moduloNuova(r.Context(), valori, segnati, invio, chiave)
		if err != nil {
			Errore(w, r)
			return
		}
		d.AvvisoQR, d.Gemelle, d.GemelleAvvisate = avviso, gemelle, gemelleAvvisate
		d.MostraAvvisoQR = avviso && gemelle == nil
		if gemelle != nil {
			d.GemelleAvvisate = gemelle.Codice
		}
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
		OreficeriaID: valori["oreficeria_id"], ArtigianoID: valori["artisan_id"], // Numero vuoto: il contatore (D-64)
		DataAccettazione: valori["accepted_on"], DataPrevista: valori["expected_on"], Descrizione: valori["description"],
		NotePubbliche: valori["public_notes"], NoteInterne: valori["internal_notes"], Preventivo: valori["estimate_euros"],
		CompraOro: valori["compro_oro"], CodiceQR: valori["qr_code"], GemelleConfermate: gemelleAvvisate,
	}, contestoDi(r))
	var rifiutati *buste.CampiRifiutati
	var gemelleAperte *buste.GemelleAperte
	var rifiuto buste.Rifiuto
	switch {
	case err == nil:
		l := f.linkPubblico(creata.Token, creata.OreficeriaID != nil)       // Oreficeria.is_b2b/1
		visibile, _ := buste.NumeroVisibile(creata.CodiceQR, creata.Numero) // D-64
		e := esitoBusta{Numero: visibile, Link: l}
		if fotoScelte(conteggio) {
			// push_intake_photos/3: la busta e la chiave del modulo salvato, solo con delle foto.
			e.Foto = &fotoDaMandare{ID: creata.ID, Numero: visibile, Chiave: chiave}
		}
		esito = f.esiti.metti(st.dati.Utente, st.dati.Nonce, e)
		f.vaiA(w, r, "/?esito="+esito)
	case errors.As(err, &gemelleAperte):
		// L'etichetta è già su una busta aperta (D-64): si avvisa una volta, con il modulo com'è.
		gemelle = nuovoAvvisoGemelle(gemelleAperte)
		ridisegna("", nil, avvisato, inviati)
	case errors.As(err, &rifiutati):
		// Il changeset dello standalone porta i valori risolti dalla creazione (protected_attrs):
		// oreficeria e artigiano come id (nil la voce vuota), compro oro come booleano. Gli errori si
		// vedono solo per i campi arrivati (used_input?/1). Il numero non è più un campo (D-64): un
		// suo errore (il contatore dietro a un numero già usato) va in cima alla pagina.
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
		ridisegna(strings.Join(rifiutati.Campi["numero"], " "), errori, avvisato, segnati)
	case errors.As(err, &rifiuto):
		ridisegna(string(rifiuto), nil, avvisato, inviati)
	default:
		f.registro.Error("busta non salvata")
		ridisegna(buste.MessaggioNonSalvata, nil, avvisato, inviati)
	}
}
