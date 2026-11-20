package web

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/L0cutus/gestrip/internal/assist"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
)

// Il «Dettato AI» della pagina «Nuova riparazione» (slice 108; RIP-H-42, RIP-H-45), come il riquadro
// #assist-card e l'evento assist_extract di WorkshopLive dello standalone a go-baseline, senza LiveView:
//   - con l'estrazione configurata (Assist.configured?(:ai), riletta a ogni pagina) il riquadro
//     «1 · Incolla il testo dettato» con «Descrizione libera» e «✦ Estrai con AI» prende il posto di
//     «🔒 Dettato AI», e il modulo diventa «Inserimento manuale senza AI», chiuso finché non serve;
//   - «✦ Estrai con AI» è POST /nuova/estrai, un modulo a parte (#assist-form) con il dettato. Con
//     JavaScript app.js ci aggiunge i campi del modulo della busta com'è in quel momento (invio, chiave
//     delle foto, avvisi) e se è aperto (intake_aperto), senza JavaScript arriva solo il dettato;
//   - estrazione riuscita: la pagina torna con il modulo riempito dalla proposta (repair_form/1 e
//     assign_customer_pair/3 dello standalone: i campi che l'estrazione non dà tornano vuoti, come in
//     un modulo nuovo), aperto, con «✓ Dati estratti. Verifica e correggi prima di salvare.» (o la
//     correzione del nome) sotto la casella. Niente si salva: la busta la crea solo «Genera busta», con
//     quello che la persona vede e può correggere. L'identificativo dell'invio e la chiave delle foto
//     restano quelli del modulo;
//   - estrazione rifiutata: la pagina torna con il messaggio dello standalone (in rosso) e il modulo
//     com'è arrivato, senza toccarlo; senza JavaScript non è arrivato niente e il modulo è quello nuovo.
//
// Riservatezza: il dettato, i nomi e i telefoni non vanno mai nel registro del server; un guasto si
// registra con una frase fissa. L'audit (assist.extract, soli conteggi) lo scrive assist.

// datiDettato è la casella del dettato (assist_text) e il messaggio sotto (assist_status).
type datiDettato struct {
	Testo  string
	Stato  string
	Errore bool // il messaggio è un rifiuto: classe text-error
}

// dettatoConfigurato è assist_ai?: senza la pagina dell'assistente (dipendenza assente) non c'è estrazione.
func (f *facciaWeb) dettatoConfigurato(ctx context.Context) (bool, error) {
	if f.assistente == nil {
		return false, nil
	}
	return f.assistente.EstrazioneConfigurata(ctx)
}

// negozioDelDettato è quello che il negozio conosce per l'estrazione: tutti i clienti (list_customers/0) e
// le oreficerie esterne (list_external_oreficerie/0), dal database.
func (f *facciaWeb) negozioDelDettato(ctx context.Context) (assist.Negozio, error) {
	s, err := f.buste.Scelte(ctx)
	if err != nil {
		return assist.Negozio{}, err
	}
	cc, err := f.buste.ClientiNoti(ctx)
	if err != nil {
		return assist.Negozio{}, err
	}
	n := assist.Negozio{Clienti: make([]assist.Cliente, 0, len(cc)), Oreficerie: make([]assist.Oreficeria, 0, len(s.Oreficerie))}
	for _, c := range cc {
		n.Clienti = append(n.Clienti, assist.Cliente{ID: c.ID, Nome: c.Nome, Telefono: c.Telefono})
	}
	for _, o := range s.Oreficerie {
		n.Oreficerie = append(n.Oreficerie, assist.Oreficeria{ID: o.ID, Nome: o.Nome})
	}
	return n, nil
}

// valoriDellaProposta sono i campi del modulo riempiti dalla proposta (repair_form/1 con
// assist_repair_attrs/1, e la coppia nome/telefono): artigiano, note interne e codice QR vuoti come in un
// modulo nuovo. Il secondo valore dice se l'oreficeria è da segnare nel menu (un id; con nil lo
// standalone non segna nessuna voce).
func valoriDellaProposta(p assist.Proposta) (map[string]string, map[string]bool) {
	v := map[string]string{
		"customer_name": p.NomeCliente, "customer_phone": p.TelefonoCliente,
		"oreficeria_id": "", "artisan_id": "",
		"accepted_on": p.DataAccettazione, "expected_on": p.DataPrevista,
		"description": p.Descrizione, "public_notes": p.NotePubbliche, "internal_notes": "",
		"estimate_euros": p.Preventivo, "compro_oro": "false", "qr_code": "",
	}
	if p.CompraOro {
		v["compro_oro"] = "true"
	}
	segnati := map[string]bool{}
	if p.OreficeriaID != 0 {
		v["oreficeria_id"] = strconv.FormatInt(p.OreficeriaID, 10)
		segnati["oreficeria_id"] = true
	}
	return v, segnati
}

// estraiDettato è POST /nuova/estrai (l'evento assist_extract).
func (f *facciaWeb) estraiDettato(w http.ResponseWriter, r *http.Request) {
	m := leggiModuloMandato(r)
	testo, _ := ultimo(r, "assist[text]")
	testo = aCapoUnix(testo) // come la casella di LiveView (a_capo.go)
	var e assist.Estrazione
	err := assist.ErrNonConfigurato
	if f.assistente != nil {
		// Il negozio si legge solo dopo i rifiuti che non costano (non configurata, testo corto o lungo).
		e, err = f.assistente.EstraiDettato(r.Context(), testo, f.negozioDelDettato, contestoDi(r))
	}
	messaggio := assist.MessaggioDiRifiuto(err)
	if err != nil && messaggio == "" {
		// Non è un rifiuto dell'estrazione: il database (configurazione, negozio o audit) non risponde.
		switch {
		case errors.Is(err, assist.ErrNegozio):
			f.registro.Error("dettato: clienti e oreficerie non letti")
		case !errors.Is(err, context.Canceled):
			f.registro.Error("dettato: estrazione non riuscita")
		}
		Errore(w, r)
		return
	}
	if err != nil {
		f.rifiutoDelDettato(w, r, m, testo, messaggio)
		return
	}
	// Riuscita: il modulo della proposta, aperto (intake-reveal); l'avviso del QR mancante si azzera
	// (qr_warned false), come gli avvisi delle etichette: il codice QR torna vuoto.
	valori, segnati := valoriDellaProposta(e.Proposta)
	d, errM := f.moduloNuova(r.Context(), valori, segnati, m.invio, m.chiave)
	if errM != nil {
		Errore(w, r)
		return
	}
	d.Aperto = true
	d.Dettato = datiDettato{Testo: testo, Stato: e.Proposta.Messaggio()}
	f.mostraCon(w, r, "nuova", http.StatusOK, "", d)
}

// rifiutoDelDettato ridisegna la pagina dopo un rifiuto: il messaggio in rosso e il modulo com'è arrivato, senza
// toccarlo (valori, avvisi, invio, chiave), aperto se lo era (intake_aperto, da app.js). Senza nessun campo del modulo
// (senza JavaScript) è il modulo nuovo. Gli avvisi delle etichette si rimettono con la regola di «Genera busta»
// (ridisegna): quello del codice simile a un'etichetta (forma_avvisata) e quello delle buste aperte con lo stesso codice
// (gemelle_avvisate, rilette), e l'avviso del QR mancante solo senza di loro. Con l'estrazione tolta dopo aver aperto
// la pagina la pagina resta com'era all'apertura, come lo standalone che decide al mount: il riquadro, «Inserimento
// manuale senza AI», il modulo chiuso se non era aperto.
func (f *facciaWeb) rifiutoDelDettato(w http.ResponseWriter, r *http.Request, m moduloMandato, testo, messaggio string) {
	valori, segnati := m.valori, m.inviati
	if !slices.ContainsFunc(campiBusta, func(c string) bool { return m.inviati[c] }) {
		valori, segnati = map[string]string{"accepted_on": clock.Oggi(f.orologio).String()}, nil
	}
	d, err := f.moduloNuova(r.Context(), valori, segnati, m.invio, m.chiave)
	if err != nil {
		Errore(w, r)
		return
	}
	var gemelle *buste.GemelleAperte
	if m.gemelleAvvisate != "" {
		if gemelle, err = f.buste.GemelleAperteDi(r.Context(), m.gemelleAvvisate); err != nil {
			f.registro.Error("dettato: avviso dell'etichetta non letto")
			Errore(w, r)
			return
		}
	}
	if gemelle != nil {
		d.Gemelle = nuovoAvvisoGemelle(gemelle)
	}
	if m.formaAvvisata != "" && buste.NormalizzaCodice(m.valori["qr_code"]) == buste.NormalizzaCodice(m.formaAvvisata) {
		d.Forma = m.formaAvvisata // l'avviso vale per il codice ancora nel campo
	}
	d.AvvisoQR = m.avvisato
	d.MostraAvvisoQR = m.avvisato && d.Gemelle == nil && d.Forma == ""
	d.GemelleAvvisate, d.FormaAvvisata = m.gemelleAvvisate, m.formaAvvisata
	// Un rifiuto arriva sempre dal riquadro: la pagina è quella del mount con l'estrazione.
	d.Assistito = true
	d.Aperto = r.PostForm.Get("intake_aperto") == "1"
	d.Dettato = datiDettato{Testo: testo, Stato: messaggio, Errore: true}
	f.mostraCon(w, r, "nuova", http.StatusOK, "", d)
}
