package buste

import (
	"errors"
	"strings"
	"testing"
)

// Il popup «🔧 Sollecita artigiano» (slice 085), come il purpose :artisan di CustomerNotice e
// Outgoing.destination_for_artisan/1, compose_artisan/1, resolve_artisan_link/2 dello standalone
// (go-baseline; customer_notice_test.exs e outgoing_artisan_test.exs). Dati inventati.

// bustaSollecito crea una busta (dell'oreficeria 2 con b2b) affidata all'artigiano 1 con il telefono dato.
func bustaSollecito(t *testing.T, b *Buste, b2b bool, telefono any) int64 {
	t.Helper()
	esegui(t, b.d, `UPDATE artigiani SET telefono = ? WHERE id = 1`, telefono)
	return bustaAvviso(t, b, b2b, "artigiano_id = 1")
}

func disegnataSollecito(z BozzaAvviso) Disegnata {
	d := disegnata(z)
	d.ArtigianoID = z.ArtigianoID
	return d
}

// inventario: ART-23, CLI-29
// L'artigiano della busta con il suo numero: WhatsApp a lui, il testo wa_artigiano con il suo nome, il
// numero dei testi senza «N.» (D-82), la prima riga della lavorazione, lo stato in maiuscolo, consegna e
// note come righe, {link} letterale e il negozio; niente del cliente. Aprire non scrive niente. Anche su
// una busta dell'oreficeria il destinatario è l'artigiano.
func TestApriSollecitoArtigiano(t *testing.T) {
	b, _, _ := prova(t)
	profiloProva(t, b, "Città Prova", nil, nil, nil, nil)
	id := bustaSollecito(t, b, false, "348 555 6666")
	prima := statoAvvisi(t, b)
	z := apriAvviso(t, b, id, ScopoArtigiano)
	if statoAvvisi(t, b) != prima {
		t.Error("aprire ha scritto")
	}
	if z.Scopo != ScopoArtigiano || z.Tipo != TipoSollecito || z.Canale != "whatsapp" || z.Parte != ParteArtigiano ||
		z.Indirizzo != "+393485556666" || z.Nome != "Artigiano Esempio" || z.ArtigianoID == nil || *z.ArtigianoID != 1 ||
		z.Problema != "" || z.Modello != nil || z.Numero != "N. 1/26" || z.Chiusa {
		t.Fatalf("bozza %+v", z)
	}
	atteso := "Ciao Artigiano Esempio, sollecito riparazione n° 1/26\nLavorazione: Anello in oro giallo\nStato attuale: IN LAVORAZIONE\n" +
		"\nDettagli e foto: {link}\nBottega Prova"
	if z.Testo != atteso {
		t.Errorf("testo:\n%q\natteso\n%q", z.Testo, atteso)
	}
	if strings.Contains(z.Testo, "Ada Rossi") || strings.Contains(z.Testo, "333") {
		t.Error("dati del cliente nel testo")
	}
	// Consegna e note pubbliche come righe; il nome vuoto è «Collega»; l'etichetta dà il numero (D-64).
	esegui(t, b.d, `UPDATE buste SET data_prevista = '2026-11-20', note_pubbliche = ' da lucidare ', codice_qr = 'RIP-2026-0045',
		stato = 'in_attesa_materiali' WHERE id = ?`, id)
	esegui(t, b.d, `UPDATE artigiani SET nome = '   ' WHERE id = 1`)
	z = apriAvviso(t, b, id, ScopoArtigiano)
	atteso = "Ciao Collega, sollecito riparazione n° 0045/26\nLavorazione: Anello in oro giallo\nStato attuale: IN ATTESA MATERIALI\n" +
		"Consegna prevista: 20/11/2026\nNote: da lucidare\n\nDettagli e foto: {link}\nBottega Prova"
	if z.Testo != atteso || z.Numero != "0045/26" {
		t.Errorf("testo:\n%q\natteso\n%q (%s)", z.Testo, atteso, z.Numero)
	}
	// Busta dell'oreficeria: sempre l'artigiano.
	esegui(t, b.d, `UPDATE artigiani SET nome = 'Artigiano Esempio' WHERE id = 1`)
	b2b := bustaSollecito(t, b, true, "348 555 6666")
	if z := apriAvviso(t, b, b2b, ScopoArtigiano); z.Parte != ParteArtigiano || z.Indirizzo != "+393485556666" || z.Nome != "Artigiano Esempio" {
		t.Errorf("b2b %+v", z)
	}
}

// inventario: ART-23
// Nessun artigiano, un artigiano senza telefono o con un telefono senza cifre: «nessun recapito», testo
// vuoto. Una busta chiusa (consegnata, annullata, chiusa): «chiusa», anche con il numero.
func TestSollecitoSenzaRecapito(t *testing.T) {
	b, _, _ := prova(t)
	senza := bustaAvviso(t, b, false, "")
	if z := apriAvviso(t, b, senza, ScopoArtigiano); z.Problema != ProblemaNessunRecapito || z.Testo != "" || z.Indirizzo != "" || z.ArtigianoID != nil {
		t.Errorf("senza artigiano %+v", z)
	}
	for _, tel := range []any{nil, "", "  ", "abc"} {
		id := bustaSollecito(t, b, false, tel)
		if z := apriAvviso(t, b, id, ScopoArtigiano); z.Problema != ProblemaNessunRecapito || z.Testo != "" || z.Indirizzo != "" {
			t.Errorf("telefono %v: %+v", tel, z)
		}
	}
	for _, stato := range statiChiusi {
		id := bustaSollecito(t, b, false, "348 555 6666")
		oro := 0
		if stato == "chiusa" { // «chiusa» è solo della compro oro
			oro = 1
		}
		esegui(t, b.d, `UPDATE buste SET compro_oro = ?, stato = ? WHERE id = ?`, oro, stato, id)
		if z := apriAvviso(t, b, id, ScopoArtigiano); z.Problema != ProblemaChiusa || z.Testo != "" || !z.Chiusa {
			t.Errorf("%s: %+v", stato, z)
		}
	}
	// Chiusa e senza numero: «chiusa» (destination_for_artisan guarda prima la chiusura).
	id := bustaSollecito(t, b, false, nil)
	esegui(t, b.d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, id)
	if z := apriAvviso(t, b, id, ScopoArtigiano); z.Problema != ProblemaChiusa {
		t.Errorf("chiusa senza numero: %+v", z)
	}
	// Chiusa e senza artigiano: «nessun recapito» (:no_artisan viene prima della chiusura).
	esegui(t, b.d, `UPDATE buste SET artigiano_id = NULL WHERE id = ?`, id)
	if z := apriAvviso(t, b, id, ScopoArtigiano); z.Problema != ProblemaNessunRecapito {
		t.Errorf("chiusa senza artigiano: %+v", z)
	}
}

// inventario: ART-23
// Un testo wa_artigiano salvato che non si compone: nessuna proposta, il riquadro vuoto e l'avviso.
func TestSollecitoModelloNonValido(t *testing.T) {
	b, _, _ := prova(t)
	esegui(t, b.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.template.wa_artigiano', 'Ciao {password}', ?)`, istanteProva)
	id := bustaSollecito(t, b, false, "348 555 6666")
	z := apriAvviso(t, b, id, ScopoArtigiano)
	if z.Modello == nil || z.Modello.Famiglia != FamArtigiano || z.Testo != "" || z.Indirizzo == "" {
		t.Errorf("bozza %+v", z)
	}
}

// inventario: ART-23, CLI-29
// «Invia»: {link} diventa il link dell'artigiano (firmato, niente scritto per il link, nessun link della
// busta emesso), il testo senza spazi ai bordi, la riga WhatsApp «accepted» verso «artisan» al suo numero,
// outgoing.handed_off con party artisan; nessun timbro link_inviato_il.
func TestInviaSollecito(t *testing.T) {
	b, _, _ := prova(t)
	conLink(b)
	b.ConIndirizzoPubblico(baseProva)
	id := bustaSollecito(t, b, false, "348 555 6666")
	token := tokenSalvato(t, b, id)
	z := apriAvviso(t, b, id, ScopoArtigiano)
	inv, err := b.InviaAvviso(ctx, id, disegnataSollecito(z), " \nSollecito: {link}\n\n", "", chi)
	if err != nil {
		t.Fatal(err)
	}
	testo := valore(t, b.d, `SELECT testo FROM messaggi_in_uscita`).(string)
	link := strings.TrimPrefix(testo, "Sollecito: ")
	if !strings.HasPrefix(link, baseProva+"/artigiano/"+itoa(id)+"?t=g1.") || strings.ContainsAny(link, " \n") {
		t.Fatalf("testo %q", testo)
	}
	if _, err := b.PaginaArtigiano(ctx, itoa(id), tokDi(link), pubblicoChi); err != nil {
		t.Errorf("il link non apre: %v", err)
	}
	if inv.URL != IndirizzoWaMe("+393485556666", testo) || inv.Tipo != TipoSollecito {
		t.Errorf("invio %+v", inv)
	}
	riga := valore(t, b.d, `SELECT busta_id || '|' || canale || '|' || destinatario || '|' || indirizzo || '|' || stato || '|' || accettato_il FROM messaggi_in_uscita`)
	if riga != itoa(id)+"|whatsapp|artisan|+393485556666|accepted|"+istanteProva {
		t.Errorf("riga %v", riga)
	}
	mid := valore(t, b.d, `SELECT id FROM messaggi_in_uscita`).(int64)
	if a := ultimoAudit(t, b.d, "outgoing.handed_off"); a != "outgoing.handed_off|outgoing_message|"+itoa(mid)+`|ok|{"channel":"whatsapp","party":"artisan","repair_id":`+itoa(id)+`}` {
		t.Errorf("audit %s", a)
	}
	if v := colonnaBusta(t, b, id, "link_inviato_il"); v != nil {
		t.Errorf("timbro %v", v)
	}
	if tokenSalvato(t, b, id) != token || quanteAzioni(t, b, "repair.public_link_issued") != 0 {
		t.Error("il link della busta è cambiato")
	}
	// Senza {link} il testo parte com'è, anche senza indirizzo pubblico.
	b.ConIndirizzoPubblico("")
	if inv, err := b.InviaAvviso(ctx, id, disegnataSollecito(z), "Solo testo", "", chi); err != nil || inv.URL != IndirizzoWaMe("+393485556666", "Solo testo") {
		t.Errorf("senza link: %+v %v", inv, err)
	}
}

// inventario: ART-23
// I rifiuti dell'«Invia», senza scrivere niente: vuoto, tipo sbagliato, artigiano cambiato (un altro,
// tolto, messo), numero cambiato o tolto, canale, busta chiusa, {link} senza indirizzo pubblico.
func TestInviaSollecitoRifiuti(t *testing.T) {
	b, _, _ := prova(t)
	conLink(b)
	esegui(t, b.d, `INSERT INTO artigiani (id, nome, telefono) VALUES (2, 'Altro Artigiano', '348 555 6666')`)
	id := bustaSollecito(t, b, false, "348 555 6666")
	z := apriAvviso(t, b, id, ScopoArtigiano)
	prova := func(nome string, d Disegnata, testo string, atteso error) {
		t.Helper()
		prima := statoAvvisi(t, b)
		if _, err := b.InviaAvviso(ctx, id, d, testo, baseProva, chi); !errors.Is(err, atteso) {
			t.Errorf("%s: %v, atteso %v", nome, err, atteso)
		}
		if statoAvvisi(t, b) != prima {
			t.Errorf("%s: ha scritto", nome)
		}
	}
	prova("vuoto", disegnataSollecito(z), " \n", ErrAvvisoVuoto)
	d := disegnataSollecito(z)
	d.Tipo = TipoAggiornamento
	prova("tipo", d, "Ciao", ErrTipoCambiato)
	// Il tipo del sollecito con lo scopo della controparte non diventa un sollecito.
	d = disegnata(apriAvviso(t, b, id, ScopoControparte))
	d.Tipo = TipoSollecito
	prova("tipo artigiano alla controparte", d, "Ciao", ErrTipoCambiato)
	d = disegnataSollecito(z)
	due := int64(2)
	d.ArtigianoID = &due
	prova("altro artigiano", d, "Ciao", ErrArtigianoCambiato)
	d.ArtigianoID = nil
	prova("artigiano tolto nel modulo", d, "Ciao", ErrArtigianoCambiato)
	esegui(t, b.d, `UPDATE buste SET artigiano_id = 2 WHERE id = ?`, id)
	prova("artigiano cambiato", disegnataSollecito(z), "Ciao", ErrArtigianoCambiato)
	esegui(t, b.d, `UPDATE buste SET artigiano_id = NULL WHERE id = ?`, id)
	prova("artigiano tolto", disegnataSollecito(z), "Ciao", ErrArtigianoCambiato)
	esegui(t, b.d, `UPDATE buste SET artigiano_id = 1 WHERE id = ?`, id)
	d = disegnataSollecito(z)
	d.Indirizzo = "+390000000000"
	prova("numero", d, "Ciao", ErrRecapitoCambiato)
	d = disegnataSollecito(z)
	d.Canale = "telegram"
	prova("canale", d, "Ciao", ErrCanaleCambiato)
	esegui(t, b.d, `UPDATE artigiani SET telefono = NULL WHERE id = 1`)
	prova("numero tolto", disegnataSollecito(z), "Ciao", ErrRecapitoCambiato)
	esegui(t, b.d, `UPDATE artigiani SET telefono = '348 555 6666' WHERE id = 1`)
	esegui(t, b.d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, id)
	prova("chiusa", disegnataSollecito(z), "Ciao", ErrBustaChiusa)
	esegui(t, b.d, `UPDATE buste SET stato = 'in_lavorazione' WHERE id = ?`, id)
	prova("senza indirizzo pubblico", disegnataSollecito(z), "Vedi {link}", ErrSenzaIndirizzoPubblico)
	if _, err := b.InviaAvviso(ctx, 999, disegnataSollecito(z), "Ciao", baseProva, chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("busta assente: %v", err)
	}
	// Una busta senza artigiano e un modulo senza artigiano: nessun recapito, niente scritto.
	senza := bustaAvviso(t, b, false, "")
	zs := apriAvviso(t, b, senza, ScopoArtigiano)
	prima := statoAvvisi(t, b)
	if _, err := b.InviaAvviso(ctx, senza, disegnataSollecito(zs), "Ciao", baseProva, chi); !errors.Is(err, ErrNessunRecapito) {
		t.Errorf("senza artigiano: %v", err)
	}
	if statoAvvisi(t, b) != prima {
		t.Error("senza artigiano: ha scritto")
	}
}
